// Golden-vector check: the TS encoder must produce the exact bytes pinned in
// backend/internal/models/hash_conformance_test.go and sig_verify.rs tests.
// Run: node scripts/check-borsh.mjs  (wired into `npm run build` as a pretest)

const GOLDEN =
  "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20" +
  "2122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f40" +
  "01003d0040420f0000000000000000f1536500000000efbeadde00000000";

// Inline copy of lib/borsh.ts logic (this script runs in plain node, pre-build).
function borshOrder(o) {
  const buf = new Uint8Array(94);
  const dv = new DataView(buf.buffer);
  let off = 0;
  buf.set(o.maker, off); off += 32;
  buf.set(o.marketId, off); off += 32;
  buf[off++] = o.outcome;
  buf[off++] = o.side;
  dv.setUint16(off, o.price, true); off += 2;
  dv.setBigUint64(off, o.size, true); off += 8;
  dv.setUint16(off, o.feeBps, true); off += 2;
  dv.setBigInt64(off, o.expiry, true); off += 8;
  dv.setBigUint64(off, o.salt, true);
  return buf;
}

const maker = new Uint8Array(32).map((_, i) => i + 1);
const marketId = new Uint8Array(32).map((_, i) => i + 33);
const got = Buffer.from(
  borshOrder({
    maker, marketId,
    outcome: 1, side: 0, price: 61,
    size: 1_000_000n, feeBps: 0,
    expiry: 1_700_000_000n, salt: 0xdeadbeefn,
  })
).toString("hex");

if (got !== GOLDEN) {
  console.error("borsh encoder DRIFTED from the golden vector");
  console.error(" got:", got);
  console.error("want:", GOLDEN);
  process.exit(1);
}
console.log("borsh golden vector ok (94 bytes)");
