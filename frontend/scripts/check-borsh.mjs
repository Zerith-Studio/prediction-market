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

// ---- ComboQuote (market-maker quote signing) --------------------------------
// Must match backend/internal/models/hash.go BorshComboQuote, pinned by
// TestBorshComboQuoteGoldenVector.
const GOLDEN_QUOTE =
  "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20" +
  "02000000" +
  "2122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f4001" +
  "4142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f6000" +
  "404b4c0000000000" +
  "002d310100000000" +
  "00f1536500000000" +
  "efbeadde00000000";

function borshComboQuote(q) {
  const buf = new Uint8Array(32 + 4 + q.legs.length * 33 + 32);
  const dv = new DataView(buf.buffer);
  let o = 0;
  buf.set(q.maker, o); o += 32;
  dv.setUint32(o, q.legs.length, true); o += 4;
  for (const leg of q.legs) {
    buf.set(leg.marketId, o); o += 32;
    buf[o++] = leg.outcome;
  }
  dv.setBigUint64(o, q.stake, true); o += 8;
  dv.setBigUint64(o, q.payout, true); o += 8;
  dv.setBigUint64(o, q.expiry, true); o += 8;
  dv.setBigUint64(o, q.salt, true);
  return buf;
}

const gotQuote = Buffer.from(
  borshComboQuote({
    maker: new Uint8Array(32).map((_, i) => i + 1),
    legs: [
      { marketId: new Uint8Array(32).map((_, i) => i + 33), outcome: 1 },
      { marketId: new Uint8Array(32).map((_, i) => i + 65), outcome: 0 },
    ],
    stake: 5_000_000n,
    payout: 20_000_000n,
    expiry: 1_700_000_000n,
    salt: 0xdeadbeefn,
  })
).toString("hex");

if (gotQuote !== GOLDEN_QUOTE) {
  console.error("borsh ComboQuote encoder DRIFTED from the golden vector");
  console.error(" got:", gotQuote);
  console.error("want:", GOLDEN_QUOTE);
  process.exit(1);
}
console.log("borsh combo-quote golden vector ok");
