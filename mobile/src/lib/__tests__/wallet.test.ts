import nacl from "tweetnacl";
import bs58 from "bs58";
import { keypairFromSeed } from "../wallet";

test("seed-derived keypair signs verifiably (same scheme the backend checks)", () => {
  const seed = new Uint8Array(32).map((_, i) => (i * 7) % 251);
  const kp = keypairFromSeed(seed);
  const msg = new Uint8Array([1, 2, 3, 4]);
  const sig = nacl.sign.detached(msg, kp.secretKey);
  expect(nacl.sign.detached.verify(msg, sig, kp.publicKey)).toBe(true);
  expect(bs58.decode(bs58.encode(kp.publicKey))).toEqual(kp.publicKey);
});
