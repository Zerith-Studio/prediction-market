import { borshOrder, randomSalt, toHex } from "../borsh";
import { b64ToBytes } from "../base64";

// Same golden vector as scripts/check-borsh.mjs, hash_conformance_test.go,
// sig_verify.rs tests, and frontend/scripts/check-borsh.mjs.
const GOLDEN =
  "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20" +
  "2122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f40" +
  "01003d0040420f0000000000000000f1536500000000efbeadde00000000";

test("borshOrder matches the pinned golden vector", () => {
  const maker = new Uint8Array(32).map((_, i) => i + 1);
  const marketId = new Uint8Array(32).map((_, i) => i + 33);
  const bytes = borshOrder({
    maker, marketId,
    outcome: 1, side: 0, price: 61,
    size: 1_000_000n, feeBps: 0,
    expiry: 1_700_000_000n, salt: 0xdeadbeefn,
  });
  expect(bytes.length).toBe(94);
  expect(toHex(bytes)).toBe(GOLDEN);
});

test("randomSalt clears the top bit (signed BIGINT column)", () => {
  for (let i = 0; i < 32; i++) expect(randomSalt() < 2n ** 63n).toBe(true);
});

test("b64ToBytes decodes standard base64", () => {
  expect(Array.from(b64ToBytes("AQIDBA=="))).toEqual([1, 2, 3, 4]);
  expect(Array.from(b64ToBytes("aGVsbG8="))).toEqual([104, 101, 108, 108, 111]);
});
