use crate::errors::PitchMarketError;
use crate::state::OrderArgs;
use anchor_lang::prelude::*;
use anchor_lang::solana_program::ed25519_program;
use anchor_lang::solana_program::sysvar::instructions::load_instruction_at_checked;

/// Solana's Ed25519 precompile marks an offset field as "point into this same
/// instruction's data" with this sentinel (solana-web3.js Ed25519Program always
/// builds self-contained instructions this way — see
/// solana-web3.js/src/ed25519-program.ts, `signatureInstructionIndex` etc.).
const ED25519_IX_SELF: u16 = u16::MAX;

/// Byte layout the Ed25519 native program (`Ed25519SigVerify111...`) requires for
/// its instruction data — stable since Solana v1.6, documented at
/// https://docs.rs/solana-program/latest/solana_program/ed25519_program/index.html
/// and mirrored by solana-web3.js's Ed25519Program.createInstructionWithPublicKey:
///
/// ```text
/// [0]      num_signatures: u8            (must be 1 — we verify one order per ix)
/// [1]      padding: u8
/// [2..4]   signature_offset: u16 LE
/// [4..6]   signature_instruction_index: u16 LE  (0xFFFF = this instruction)
/// [6..8]   public_key_offset: u16 LE
/// [8..10]  public_key_instruction_index: u16 LE
/// [10..12] message_data_offset: u16 LE
/// [12..14] message_data_size: u16 LE
/// [14..16] message_instruction_index: u16 LE
/// [16..]   signature (64B) | pubkey (32B) | message (N B)   — for the default
///          self-contained layout client libraries produce.
/// ```
///
/// The Ed25519 native program itself is what actually checks the signature
/// cryptographically, and it runs as its own instruction earlier in the same
/// transaction — if that check fails, the whole transaction aborts before our
/// instruction ever executes. Our job here is narrower: confirm the (pubkey,
/// message) pair the precompile verified is actually `(order.maker,
/// borsh_order(order))` and not some other, unrelated but validly-signed
/// Ed25519 instruction the client slipped into the same transaction.
///
/// Caller contract: `ix_index` must be the index, within this transaction, of
/// the Ed25519Program instruction that signs this specific order — the client
/// is responsible for ordering instructions so each settle_match's two orders
/// have their signature-verification instructions at known, agreed indices
/// (interface-contract.md should pin this ordering once E2's crank is updated
/// to emit it — currently crank.go does not yet build these instructions, see
/// backend/internal/crank/crank.go TODO).
pub fn verify_order_signature(
    instructions_sysvar: &AccountInfo,
    ix_index: u16,
    order: &OrderArgs,
    sig: &[u8; 64],
) -> Result<()> {
    let ix = load_instruction_at_checked(ix_index as usize, instructions_sysvar)
        .map_err(|_| PitchMarketError::BadSignature)?;

    require_keys_eq!(ix.program_id, ed25519_program::ID, PitchMarketError::BadSignature);

    let expected_msg = borsh_order(order);
    let expected_len = 16usize
        .checked_add(64)
        .and_then(|v| v.checked_add(32))
        .and_then(|v| v.checked_add(expected_msg.len()))
        .ok_or(PitchMarketError::BadSignature)?;
    require!(ix.data.len() == expected_len, PitchMarketError::BadSignature);

    require!(ix.data[0] == 1, PitchMarketError::BadSignature); // num_signatures

    let sig_off = u16::from_le_bytes([ix.data[2], ix.data[3]]);
    let sig_ix_idx = u16::from_le_bytes([ix.data[4], ix.data[5]]);
    let pk_off = u16::from_le_bytes([ix.data[6], ix.data[7]]);
    let pk_ix_idx = u16::from_le_bytes([ix.data[8], ix.data[9]]);
    let msg_off = u16::from_le_bytes([ix.data[10], ix.data[11]]);
    let msg_len = u16::from_le_bytes([ix.data[12], ix.data[13]]);
    let msg_ix_idx = u16::from_le_bytes([ix.data[14], ix.data[15]]);

    require!(
        sig_ix_idx == ED25519_IX_SELF && pk_ix_idx == ED25519_IX_SELF && msg_ix_idx == ED25519_IX_SELF,
        PitchMarketError::BadSignature
    );

    let got_sig = slice_checked(&ix.data, sig_off, 64)?;
    let got_pk = slice_checked(&ix.data, pk_off, 32)?;
    let got_msg = slice_checked(&ix.data, msg_off, msg_len)?;

    require!(got_sig == sig.as_slice(), PitchMarketError::BadSignature);
    require!(got_pk == order.maker.as_ref(), PitchMarketError::BadSignature);
    require!(got_msg == expected_msg.as_slice(), PitchMarketError::BadSignature);

    Ok(())
}

fn slice_checked(data: &[u8], offset: u16, len: u16) -> Result<&[u8]> {
    let start = offset as usize;
    let end = start.checked_add(len as usize).ok_or(PitchMarketError::BadSignature)?;
    data.get(start..end).ok_or(PitchMarketError::BadSignature.into())
}

/// Borsh-encodes an OrderArgs identically to backend/internal/models/hash.go
/// borshOrder() — field order and widths must never drift between the two.
pub fn borsh_order(o: &OrderArgs) -> Vec<u8> {
    let mut buf = Vec::with_capacity(32 + 32 + 1 + 1 + 2 + 8 + 2 + 8 + 8);
    buf.extend_from_slice(o.maker.as_ref());
    buf.extend_from_slice(&o.market_id);
    buf.push(o.outcome);
    buf.push(o.side);
    buf.extend_from_slice(&o.price.to_le_bytes());
    buf.extend_from_slice(&o.size.to_le_bytes());
    buf.extend_from_slice(&o.fee_bps.to_le_bytes());
    buf.extend_from_slice(&o.expiry.to_le_bytes());
    buf.extend_from_slice(&o.salt.to_le_bytes());
    buf
}

pub fn order_hash(o: &OrderArgs) -> [u8; 32] {
    anchor_lang::solana_program::hash::hash(&borsh_order(o)).to_bytes()
}

#[cfg(test)]
mod tests {
    use super::*;
    use anchor_lang::AnchorSerialize;

    /// Must stay byte-identical to goldenOrder() in
    /// backend/internal/models/hash_conformance_test.go. If either side's
    /// encoder drifts, its golden test fails — a silent drift would fail closed
    /// on-chain as BadSignature with no useful error.
    fn golden_order() -> OrderArgs {
        let mut maker = [0u8; 32];
        let mut market_id = [0u8; 32];
        for i in 0..32 {
            maker[i] = (i + 1) as u8;
            market_id[i] = (i + 33) as u8;
        }
        OrderArgs {
            maker: Pubkey::new_from_array(maker),
            market_id,
            outcome: 1, // YES
            side: 0,    // BUY
            price: 61,
            size: 1_000_000,
            fee_bps: 0,
            expiry: 1_700_000_000,
            salt: 0xDEAD_BEEF,
        }
    }

    const GOLDEN_BORSH_HEX: &str =
        "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20\
         2122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f40\
         01003d0040420f0000000000000000f1536500000000efbeadde00000000";
    const GOLDEN_HASH_HEX: &str =
        "92d9ef2fb291b8dda3f183d40973d85be3bb73398f2b3d4db9d12511bbccba7e";

    fn to_hex(bytes: &[u8]) -> String {
        bytes.iter().map(|b| format!("{b:02x}")).collect()
    }

    #[test]
    fn test_borsh_order_golden_vector() {
        let encoded = borsh_order(&golden_order());
        assert_eq!(encoded.len(), 94, "borsh(Order) must be 94 bytes");
        assert_eq!(
            to_hex(&encoded),
            GOLDEN_BORSH_HEX.replace(char::is_whitespace, ""),
            "borsh encoding drifted from the golden vector pinned with hash.go"
        );
    }

    #[test]
    fn test_order_hash_golden_vector() {
        assert_eq!(
            to_hex(&order_hash(&golden_order())),
            GOLDEN_HASH_HEX,
            "order hash drifted from the golden vector pinned with hash.go"
        );
    }

    /// borsh_order must also agree with AnchorSerialize (how OrderArgs travels
    /// in settle_match instruction data) — all fields are fixed-width, so the
    /// hand-rolled encoding and derive(AnchorSerialize) must coincide.
    #[test]
    fn test_borsh_order_matches_anchor_serialize() {
        let order = golden_order();
        let mut anchor_encoded = Vec::new();
        order.serialize(&mut anchor_encoded).unwrap();
        assert_eq!(borsh_order(&order), anchor_encoded);
    }
}
