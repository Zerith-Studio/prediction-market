import * as anchor from "@coral-xyz/anchor";
import { Program } from "@coral-xyz/anchor";
import {
  PublicKey,
  Keypair,
  SystemProgram,
  Transaction,
  TransactionMessage,
  VersionedTransaction,
  AddressLookupTableProgram,
  Ed25519Program,
  SYSVAR_INSTRUCTIONS_PUBKEY,
  LAMPORTS_PER_SOL,
} from "@solana/web3.js";
import {
  TOKEN_PROGRAM_ID,
  ASSOCIATED_TOKEN_PROGRAM_ID,
  createMint,
  getOrCreateAssociatedTokenAccount,
  getAssociatedTokenAddressSync,
  mintTo,
  getAccount,
} from "@solana/spl-token";
import { createHash } from "crypto";
import { readFileSync } from "fs";
import nacl from "tweetnacl";

export const OUTCOME_NO = 0,
  OUTCOME_YES = 1;
export const SIDE_BUY = 0,
  SIDE_SELL = 1;
export const MATCH_NORMAL = 0,
  MATCH_MINT = 1,
  MATCH_MERGE = 2;
export const USDC_DECIMALS = 6;

export const idl = JSON.parse(readFileSync("target/idl/pitchmarket.json", "utf8"));

export type Order = {
  maker: PublicKey;
  marketId: Buffer; // 32
  outcome: number;
  side: number;
  price: number;
  size: bigint;
  feeBps: number;
  expiry: bigint;
  salt: bigint;
};

// Byte-identical to sig_verify.rs::borsh_order (and backend hash.go borshOrder).
export function borshOrder(o: Order): Buffer {
  const b = Buffer.alloc(32 + 32 + 1 + 1 + 2 + 8 + 2 + 8 + 8);
  let off = 0;
  o.maker.toBuffer().copy(b, off); off += 32;
  o.marketId.copy(b, off); off += 32;
  b.writeUInt8(o.outcome, off); off += 1;
  b.writeUInt8(o.side, off); off += 1;
  b.writeUInt16LE(o.price, off); off += 2;
  b.writeBigUInt64LE(o.size, off); off += 8;
  b.writeUInt16LE(o.feeBps, off); off += 2;
  b.writeBigInt64LE(o.expiry, off); off += 8;
  b.writeBigUInt64LE(o.salt, off); off += 8;
  return b;
}

// solana_program::hash::hash == sha256
export function orderHash(o: Order): Buffer {
  return createHash("sha256").update(borshOrder(o)).digest();
}

export function toArg(o: Order) {
  return {
    maker: o.maker,
    marketId: Array.from(o.marketId),
    outcome: o.outcome,
    side: o.side,
    price: o.price,
    size: new anchor.BN(o.size.toString()),
    feeBps: o.feeBps,
    expiry: new anchor.BN(o.expiry.toString()),
    salt: new anchor.BN(o.salt.toString()),
  };
}

export function ed25519Sign(kp: Keypair, msg: Buffer): Buffer {
  return Buffer.from(nacl.sign.detached(msg, kp.secretKey));
}

// A market + its derived accounts, plus the instruction drivers used by tests.
export class Env {
  provider = anchor.AnchorProvider.env();
  program: Program;
  conn: anchor.web3.Connection;
  programId: PublicKey;
  operator: Keypair;
  usdcMint!: PublicKey;

  constructor() {
    anchor.setProvider(this.provider);
    this.program = new Program(idl as anchor.Idl, this.provider);
    this.conn = this.provider.connection;
    this.programId = this.program.programId;
    this.operator = (this.provider.wallet as anchor.Wallet).payer;
  }

  vaultPda(owner: PublicKey) {
    return PublicKey.findProgramAddressSync([Buffer.from("vault"), owner.toBuffer()], this.programId)[0];
  }
  ostatusPda(o: Order) {
    return PublicKey.findProgramAddressSync([Buffer.from("ostatus"), orderHash(o)], this.programId)[0];
  }
  ata(mint: PublicKey, owner: PublicKey) {
    return getAssociatedTokenAddressSync(mint, owner, true);
  }

  async bal(acc: PublicKey): Promise<bigint> {
    try {
      return (await getAccount(this.conn, acc)).amount;
    } catch {
      return 0n;
    }
  }

  async fund(kp: Keypair) {
    const sig = await this.conn.requestAirdrop(kp.publicKey, 2 * LAMPORTS_PER_SOL);
    await this.conn.confirmTransaction(sig, "confirmed");
  }

  // Create the mock USDC mint once, fund each user's wallet ATA.
  async setupUsdc(users: Keypair[], perUser: bigint) {
    this.usdcMint = await createMint(this.conn, this.operator, this.operator.publicKey, null, USDC_DECIMALS);
    for (const kp of users) {
      const acc = await getOrCreateAssociatedTokenAccount(this.conn, this.operator, this.usdcMint, kp.publicKey);
      await mintTo(this.conn, this.operator, this.usdcMint, acc.address, this.operator, perUser);
    }
  }

  async initVaultAndDeposit(user: Keypair, amount: bigint) {
    const vault = this.vaultPda(user.publicKey);
    await this.program.methods
      .initVault()
      .accountsPartial({ vault, user: user.publicKey, systemProgram: SystemProgram.programId })
      .signers([user])
      .rpc();
    await this.program.methods
      .deposit(new anchor.BN(amount.toString()))
      .accountsPartial({
        vault,
        owner: user.publicKey,
        userUsdcAta: this.ata(this.usdcMint, user.publicKey),
        vaultUsdcAta: this.ata(this.usdcMint, vault),
        usdcMint: this.usdcMint,
        user: user.publicKey,
        tokenProgram: TOKEN_PROGRAM_ID,
        associatedTokenProgram: ASSOCIATED_TOKEN_PROGRAM_ID,
        systemProgram: SystemProgram.programId,
      })
      .signers([user])
      .rpc();
  }

  // Vault-owned outcome ATAs must exist before settle (no init_if_needed on them).
  async ensureOutcomeAta(mint: PublicKey, owner: PublicKey) {
    await getOrCreateAssociatedTokenAccount(this.conn, this.operator, mint, owner, true);
  }

  // A single binary market: PDAs + initialize_market.
  async createMarket(marketId: Buffer, resolver: PublicKey) {
    const m = {
      marketId,
      market: PublicKey.findProgramAddressSync([Buffer.from("market"), marketId], this.programId)[0],
      yesMint: PublicKey.findProgramAddressSync([Buffer.from("yes"), marketId], this.programId)[0],
      noMint: PublicKey.findProgramAddressSync([Buffer.from("no"), marketId], this.programId)[0],
      poolUsdc: PublicKey.findProgramAddressSync([Buffer.from("pool"), marketId], this.programId)[0],
    };
    await this.program.methods
      .initializeMarket(Array.from(marketId), 0, resolver)
      .accountsPartial({
        market: m.market,
        yesMint: m.yesMint,
        noMint: m.noMint,
        poolUsdc: m.poolUsdc,
        usdcMint: this.usdcMint,
        operator: this.operator.publicKey,
        tokenProgram: TOKEN_PROGRAM_ID,
        systemProgram: SystemProgram.programId,
        rent: anchor.web3.SYSVAR_RENT_PUBKEY,
      })
      .rpc();
    return m;
  }

  // Build & send the exact 3-instruction settle tx (ed25519 taker, ed25519 maker,
  // settle_match) as a v0 tx with an Address Lookup Table — the 3-ix tx exceeds the
  // 1232-byte legacy limit, so the crank must build it this way too.
  async settle(
    market: { yesMint: PublicKey; noMint: PublicKey; market: PublicKey; poolUsdc: PublicKey },
    taker: Keypair,
    takerOrder: Order,
    maker: Keypair,
    makerOrder: Order,
    matchType: number,
    fillPrice: number,
    fillSize: bigint
  ) {
    const takerMsg = borshOrder(takerOrder);
    const makerMsg = borshOrder(makerOrder);
    const takerSig = ed25519Sign(taker, takerMsg);
    const makerSig = ed25519Sign(maker, makerMsg);
    const ed0 = Ed25519Program.createInstructionWithPrivateKey({ privateKey: taker.secretKey, message: takerMsg });
    const ed1 = Ed25519Program.createInstructionWithPrivateKey({ privateKey: maker.secretKey, message: makerMsg });

    const takerVault = this.vaultPda(takerOrder.maker);
    const makerVault = this.vaultPda(makerOrder.maker);
    const takerOutcomeMint = takerOrder.outcome === OUTCOME_YES ? market.yesMint : market.noMint;
    const makerOutcomeMint = makerOrder.outcome === OUTCOME_YES ? market.yesMint : market.noMint;

    const settleIx = await this.program.methods
      .settleMatch(
        toArg(takerOrder),
        Array.from(takerSig),
        toArg(makerOrder),
        Array.from(makerSig),
        matchType,
        fillPrice,
        new anchor.BN(fillSize.toString())
      )
      .accountsPartial({
        market: market.market,
        takerOutcomeMint,
        makerOutcomeMint,
        poolUsdc: market.poolUsdc,
        takerOrderStatus: this.ostatusPda(takerOrder),
        makerOrderStatus: this.ostatusPda(makerOrder),
        takerVault,
        makerVault,
        takerUsdcAta: this.ata(this.usdcMint, takerVault),
        makerUsdcAta: this.ata(this.usdcMint, makerVault),
        takerOutcomeAta: this.ata(takerOutcomeMint, takerVault),
        makerOutcomeAta: this.ata(makerOutcomeMint, makerVault),
        operator: this.operator.publicKey,
        instructionsSysvar: SYSVAR_INSTRUCTIONS_PUBKEY,
        tokenProgram: TOKEN_PROGRAM_ID,
        systemProgram: SystemProgram.programId,
      })
      .instruction();

    const lutAddresses = [
      ...new Map(
        settleIx.keys
          .map((k) => k.pubkey)
          .filter((k) => !k.equals(this.operator.publicKey))
          .map((k) => [k.toBase58(), k] as const)
      ).values(),
    ];
    const slot = await this.conn.getSlot("finalized");
    const [createLutIx, lut] = AddressLookupTableProgram.createLookupTable({
      authority: this.operator.publicKey,
      payer: this.operator.publicKey,
      recentSlot: slot,
    });
    const extendIx = AddressLookupTableProgram.extendLookupTable({
      payer: this.operator.publicKey,
      authority: this.operator.publicKey,
      lookupTable: lut,
      addresses: lutAddresses,
    });
    await this.provider.sendAndConfirm(new Transaction().add(createLutIx, extendIx), [this.operator]);
    await new Promise((r) => setTimeout(r, 1200));
    const lutAccount = (await this.conn.getAddressLookupTable(lut)).value!;

    const msg = new TransactionMessage({
      payerKey: this.operator.publicKey,
      recentBlockhash: (await this.conn.getLatestBlockhash()).blockhash,
      instructions: [ed0, ed1, settleIx],
    }).compileToV0Message([lutAccount]);
    const vtx = new VersionedTransaction(msg);
    vtx.sign([this.operator]);
    const sig = await this.conn.sendTransaction(vtx);
    await this.conn.confirmTransaction(sig, "confirmed");
    return sig;
  }
}

export function randomMarketId(): Buffer {
  return Buffer.from(Keypair.generate().publicKey.toBuffer());
}

export function futureExpiry(): bigint {
  return BigInt(Math.floor(Date.now() / 1000) + 3600);
}
