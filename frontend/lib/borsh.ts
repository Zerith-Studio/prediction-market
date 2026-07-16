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
  // Cap the salt at 53 bits. It is *signed* over borsh as a u64 but *posted* as
  // a JSON number (float64, no bigint), so anything above 2^53 would round-trip
  // lossily and break signature verification. 53 bits is ample uniqueness and
  // still fits the backend's signed BIGINT column.
  b[6] &= 0x1f; // keep low 5 bits of byte 6 → 6*8 + 5 = 53 bits
  b[7] = 0x00;
  return new DataView(b.buffer).getBigUint64(0, true);
}

export const toHex = (b: Uint8Array): string =>
  Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");

export function fromHex(hex: string): Uint8Array {
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  return out;
}
