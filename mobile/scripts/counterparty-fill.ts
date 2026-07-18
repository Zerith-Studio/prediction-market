// Demo counterparty: crosses a target wallet's resting orders so they fill.
// A fresh wallet deposits, then for each live order of TARGET it submits the
// crossing order — BUYs are met with the opposite outcome at 100−price (MINT
// match, both sides only need USDC); SELLs are met with a same-outcome BUY at
// their limit (NORMAL match). Everything goes through the real exchange
// (sig verify → soft-lock → match → crank), exactly like app traffic.
//
// Run: EXPO_PUBLIC_API_URL=http://localhost:8080 npx tsx scripts/counterparty-fill.ts <TARGET_WALLET>
import nacl from "tweetnacl";
import bs58 from "bs58";
import { api, configured } from "../src/lib/api";
import { borshOrder, fromHex, randomSalt, toHex } from "../src/lib/borsh";
import { b64ToBytes } from "../src/lib/base64";

async function main() {
  const target = process.argv[2];
  if (!configured() || !target) {
    console.error("usage: EXPO_PUBLIC_API_URL=… npx tsx scripts/counterparty-fill.ts <TARGET_WALLET>");
    process.exit(1);
  }

  const kp = nacl.sign.keyPair();
  const addr = bs58.encode(kp.publicKey);
  console.log(`counterparty wallet ${addr}`);

  const init = await api.depositInit(addr, 1_000_000_000);
  if (init) {
    const sig = nacl.sign.detached(b64ToBytes(init.message_b64), kp.secretKey);
    await api.depositComplete(init.deposit_id, addr, 1_000_000_000, toHex(sig));
  } else {
    await api.depositMirror(addr, 1_000_000_000);
  }
  console.log(`deposited; balance ${await api.getBalance(addr)}`);

  const pf = await api.getPortfolio(target);
  // Highest-priced BUYs first so each exact-complement counter order can only
  // MINT-match its intended target order (sum == 100 with it, < 100 below it).
  const live = pf.orders
    .filter((o) => o.status === "live" && o.remaining > 0)
    .sort((a, b) => b.price - a.price);
  if (live.length === 0) {
    console.log("target has no live orders — nothing to cross");
    return;
  }

  for (const o of live) {
    const isBuy = o.side === "buy";
    const targetOutcome = o.outcome === "YES" ? 1 : 0;
    const outcome = isBuy ? 1 - targetOutcome : targetOutcome;
    const price = isBuy ? Math.max(1, 100 - o.price) : o.price;
    const salt = randomSalt();
    const msg = borshOrder({
      maker: bs58.decode(addr),
      marketId: fromHex(o.market_id),
      outcome,
      side: 0, // always a BUY from our side
      price,
      size: BigInt(o.remaining),
      feeBps: 0,
      expiry: 0n,
      salt,
    });
    const sig = nacl.sign.detached(msg, kp.secretKey);
    const res = await api.postOrder({
      maker: addr,
      market_id: o.market_id,
      outcome,
      side: 0,
      price,
      size: o.remaining,
      fee_bps: 0,
      expiry: 0,
      salt: Number(salt),
      sig: toHex(sig),
    });
    console.log(
      `${o.title}: countered ${o.side.toUpperCase()} ${o.outcome} ${o.remaining} @ ${o.price}¢ ` +
        `with BUY ${outcome === 1 ? "YES" : "NO"} @ ${price}¢ → ${res.fills.length} fill(s)`
    );
  }

  const after = await api.getPortfolio(target);
  const open = after.positions.filter((p) => p.yes > 0 || p.no > 0);
  console.log(`target now holds ${open.length} position(s):`);
  for (const p of open) console.log(`  ${p.title}: yes=${p.yes} no=${p.no} avg=${p.avg_cost}¢`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
