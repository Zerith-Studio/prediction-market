import * as anchor from "@coral-xyz/anchor";
import { Keypair, SystemProgram } from "@solana/web3.js";
import { assert } from "chai";
import {
  Env,
  Order,
  OUTCOME_YES,
  OUTCOME_NO,
  SIDE_BUY,
  SIDE_SELL,
  MATCH_MINT,
  MATCH_MERGE,
  orderHash,
  randomMarketId,
  futureExpiry,
} from "./helpers";

describe("settle_match MERGE", () => {
  const env = new Env();
  const dave = Keypair.generate();
  const erin = Keypair.generate();
  const marketId = randomMarketId();
  let market: Awaited<ReturnType<Env["createMarket"]>>;

  before(async () => {
    await Promise.all([env.fund(dave), env.fund(erin)]);
    await env.setupUsdc([dave, erin], 1_000_000_000n);
    market = await env.createMarket(marketId, env.operator.publicKey);
    await env.initVaultAndDeposit(dave, 100_000_000n);
    await env.initVaultAndDeposit(erin, 100_000_000n);
    await env.ensureOutcomeAta(market.yesMint, env.vaultPda(dave.publicKey));
    await env.ensureOutcomeAta(market.noMint, env.vaultPda(erin.publicKey));

    // Seed opposite shares: MINT one complete set. Dave gets 100 YES, Erin 100 NO,
    // pool holds 100 USDC. (Dave pays 60, Erin pays 40.)
    const expiry = futureExpiry();
    const daveBuy: Order = { maker: dave.publicKey, marketId, outcome: OUTCOME_YES, side: SIDE_BUY, price: 60, size: 100n, feeBps: 0, expiry, salt: 10n };
    const erinBuy: Order = { maker: erin.publicKey, marketId, outcome: OUTCOME_NO, side: SIDE_BUY, price: 40, size: 100n, feeBps: 0, expiry, salt: 11n };
    await env.settle(market, dave, daveBuy, erin, erinBuy, MATCH_MINT, 60, 100n);
  });

  it("two opposite-outcome sells burn a complete set and release the pooled collateral", async () => {
    const daveVault = env.vaultPda(dave.publicKey);
    const erinVault = env.vaultPda(erin.publicKey);

    const daveUsdcBefore = await env.bal(env.ata(env.usdcMint, daveVault));
    const erinUsdcBefore = await env.bal(env.ata(env.usdcMint, erinVault));
    assert.equal(await env.bal(market.poolUsdc), 100_000_000n, "pool seeded by MINT");

    // taker = Dave SELL YES @55 ; maker = Erin SELL NO @45 (prices sum to 100).
    const expiry = futureExpiry();
    const daveSell: Order = { maker: dave.publicKey, marketId, outcome: OUTCOME_YES, side: SIDE_SELL, price: 55, size: 100n, feeBps: 0, expiry, salt: 12n };
    const erinSell: Order = { maker: erin.publicKey, marketId, outcome: OUTCOME_NO, side: SIDE_SELL, price: 45, size: 100n, feeBps: 0, expiry, salt: 13n };

    await env.settle(market, dave, daveSell, erin, erinSell, MATCH_MERGE, 55, 100n);

    // Shares burned to zero, collateral returned per each seller's price.
    assert.equal(await env.bal(env.ata(market.yesMint, daveVault)), 0n, "dave YES burned");
    assert.equal(await env.bal(env.ata(market.noMint, erinVault)), 0n, "erin NO burned");
    assert.equal(await env.bal(env.ata(env.usdcMint, daveVault)), daveUsdcBefore + 55_000_000n, "dave got 55 USDC");
    assert.equal(await env.bal(env.ata(env.usdcMint, erinVault)), erinUsdcBefore + 45_000_000n, "erin got 45 USDC");
    assert.equal(await env.bal(market.poolUsdc), 0n, "pool fully released");
  });
});

describe("cancel_order", () => {
  const env = new Env();
  const grace = Keypair.generate();
  const frank = Keypair.generate();
  const marketId = randomMarketId();
  let market: Awaited<ReturnType<Env["createMarket"]>>;

  before(async () => {
    await Promise.all([env.fund(grace), env.fund(frank)]);
    await env.setupUsdc([grace, frank], 1_000_000_000n);
    market = await env.createMarket(marketId, env.operator.publicKey);
    await env.initVaultAndDeposit(grace, 100_000_000n);
    await env.initVaultAndDeposit(frank, 100_000_000n);
    await env.ensureOutcomeAta(market.yesMint, env.vaultPda(grace.publicKey));
    await env.ensureOutcomeAta(market.noMint, env.vaultPda(frank.publicKey));
  });

  const expiry = futureExpiry();
  const frankOrder: Order = { maker: frank.publicKey, marketId, outcome: OUTCOME_NO, side: SIDE_BUY, price: 40, size: 100n, feeBps: 0, expiry, salt: 20n };

  it("maker cancels their own order and the status flips to closed", async () => {
    await env.program.methods
      .cancelOrder(Array.from(orderHash(frankOrder)))
      .accountsPartial({ orderStatus: env.ostatusPda(frankOrder), maker: frank.publicKey, systemProgram: SystemProgram.programId })
      .signers([frank])
      .rpc();

    const st = await (env.program.account as any).orderStatus.fetch(env.ostatusPda(frankOrder));
    assert.equal(st.isFilledOrCancelled, true, "order marked cancelled");
  });

  it("a cancelled order cannot be settled (fails closed as OrderClosed)", async () => {
    const graceOrder: Order = { maker: grace.publicKey, marketId, outcome: OUTCOME_YES, side: SIDE_BUY, price: 60, size: 100n, feeBps: 0, expiry, salt: 21n };

    let threw = false;
    try {
      // Grace (BUY YES) crosses Frank (BUY NO) as a MINT — but Frank's order is cancelled.
      await env.settle(market, grace, graceOrder, frank, frankOrder, MATCH_MINT, 60, 100n);
    } catch (e: any) {
      threw = true;
      const blob = JSON.stringify(e.logs ?? e.message ?? e);
      assert.match(blob, /OrderClosed|order already filled or cancelled|6002/, "expected OrderClosed");
    }
    assert.isTrue(threw, "settle of a cancelled order must revert");
  });
});
