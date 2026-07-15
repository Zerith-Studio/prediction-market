// Borsh encoding of the canonical Order message (interface-contract.md §1).
// MUST stay byte-identical to backend/internal/models/hash.go BorshOrder,
// programs/pitchmarket/src/sig_verify.rs borsh_order, and tests/helpers.ts —
// all pinned by the same golden vector (see scripts/check-borsh.mjs).
// The user signs exactly these bytes; drift fails closed on-chain as BadSignature.

export interface OrderMsg {
  maker: Uint8Array; // 32-byte ed25519 pubkey
  marketId: Uint8Array; // 32 bytes
  outcome: number; // 0 = NO, 1 = YES
  side: number; // 0 = BUY, 1 = SELL
  price: number; // cents 1..99
  size: bigint; // shares
  feeBps: number;
  expiry: bigint; // unix seconds; 0 = GTC
  salt: bigint;
}

export function borshOrder(o: OrderMsg): Uint8Array {
  if (o.maker.length !== 32 || o.marketId.length !== 32) {
    throw new Error("borshOrder: maker and marketId must be 32 bytes");
  }
  const buf = new Uint8Array(32 + 32 + 1 + 1 + 2 + 8 + 2 + 8 + 8);
  const dv = new DataView(buf.buffer);
  let off = 0;
  buf.set(o.maker, off);
  off += 32;
  buf.set(o.marketId, off);
  off += 32;
  buf[off++] = o.outcome;
  buf[off++] = o.side;
  dv.setUint16(off, o.price, true);
  off += 2;
  dv.setBigUint64(off, o.size, true);
  off += 8;
  dv.setUint16(off, o.feeBps, true);
  off += 2;
  dv.setBigInt64(off, o.expiry, true);
  off += 8;
  dv.setBigUint64(off, o.salt, true);
  return buf;
}

export function randomSalt(): bigint {
  const b = new Uint8Array(8);
  crypto.getRandomValues(b);
  // Clear the top bit: the backend stores salts in a signed BIGINT column.
  b[7] &= 0x7f;
  return new DataView(b.buffer).getBigUint64(0, true);
}

export const toHex = (b: Uint8Array): string =>
  Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");

export function fromHex(hex: string): Uint8Array {
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  return out;
}
