// Lib-level e2e: drives the mobile lib against a LIVE backend.
// Run: EXPO_PUBLIC_API_URL=http://localhost:8080 npm run e2e
import nacl from "tweetnacl";
import bs58 from "bs58";
import { api, configured } from "../src/lib/api";
import { borshOrder, fromHex, randomSalt, toHex } from "../src/lib/borsh";
import { b64ToBytes } from "../src/lib/base64";

function assert(cond: unknown, msg: string): asserts cond {
  if (!cond) {
    console.error(`✗ ${msg}`);
    process.exit(1);
  }
  console.log(`✓ ${msg}`);
}

async function main() {
  assert(configured(), "EXPO_PUBLIC_API_URL configured");

  const kp = nacl.sign.keyPair();
  const addr = bs58.encode(kp.publicKey);
  console.log(`wallet ${addr}`);

  // deposit (two-step if on-chain, mirror otherwise)
  const init = await api.depositInit(addr, 1_000_000_000);
  if (init) {
    const msg = b64ToBytes(init.message_b64);
    const sig = nacl.sign.detached(msg, kp.secretKey);
    await api.depositComplete(init.deposit_id, addr, 1_000_000_000, toHex(sig));
  } else {
    await api.depositMirror(addr, 1_000_000_000);
  }
  assert((await api.getBalance(addr)) === 1_000_000_000, "deposit credited 1,000 USDC");

  // pick an open binary market
  const market = (await api.listMarkets("open")).find((m) => m.type === "binary");
  assert(market, "an open binary market exists");

  // place a deep bid (won't cross), see it on the book, then cancel it
  const salt = randomSalt();
  const order = {
    maker: bs58.decode(addr),
    marketId: fromHex(market!.market_id),
    outcome: 1,
    side: 0,
    price: 2,
    size: 10n,
    feeBps: 0,
    expiry: 0n,
    salt,
  };
  const sig = nacl.sign.detached(borshOrder(order), kp.secretKey);
  const res = await api.postOrder({
    maker: addr,
    market_id: market!.market_id,
    outcome: 1,
    side: 0,
    price: 2,
    size: 10,
    fee_bps: 0,
    expiry: 0,
    salt: Number(salt),
    sig: toHex(sig),
  });
  assert(res.order_hash, "order accepted");

  const book = await api.getBook(market!.market_id);
  assert(book.bids.some((l) => l.price === 2), "bid visible on the unified ladder");

  const pf = await api.getPortfolio(addr);
  assert(pf.orders.some((o) => o.order_hash === res.order_hash), "order in portfolio");

  await api.cancelOrder(res.order_hash, addr);
  const pf2 = await api.getPortfolio(addr);
  assert(!pf2.orders.some((o) => o.order_hash === res.order_hash), "cancel removed it");

  console.log("\nmobile lib e2e: ALL GREEN");
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
