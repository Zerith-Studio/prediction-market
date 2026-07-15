import * as anchor from "@coral-xyz/anchor";
import { Keypair, SystemProgram } from "@solana/web3.js";
import { TOKEN_PROGRAM_ID } from "@solana/spl-token";
import { assert } from "chai";
import {
  Env,
  Order,
  OUTCOME_YES,
  OUTCOME_NO,
  SIDE_BUY,
  SIDE_SELL,
  MATCH_MINT,
  MATCH_NORMAL,
  randomMarketId,
  futureExpiry,
} from "./helpers";

describe("pitchmarket lifecycle", () => {
  const env = new Env();
  const alice = Keypair.generate();
  const bob = Keypair.generate();
  const carol = Keypair.generate();
  const marketId = randomMarketId();
  let market: Awaited<ReturnType<Env["createMarket"]>>;

  before(async () => {
    await Promise.all([env.fund(alice), env.fund(bob), env.fund(carol)]);
    await env.setupUsdc([alice, bob, carol], 1_000_000_000n); // 1000 USDC each
  });

  it("initialize_market creates the market, mints and pool", async () => {
    market = await env.createMarket(marketId, env.operator.publicKey);
    const m = await (env.program.account as any).market.fetch(market.market);
    assert.equal(m.yesMint.toBase58(), market.yesMint.toBase58());
    assert.equal(m.usdcMint.toBase58(), env.usdcMint.toBase58());
    assert.equal(m.oracleTier, 0);
    assert.ok(m.outcome.unresolved !== undefined);
  });

  it("deposit moves USDC into vault-owned ATAs", async () => {
    await env.initVaultAndDeposit(alice, 100_000_000n);
    await env.initVaultAndDeposit(bob, 100_000_000n);
    await env.initVaultAndDeposit(carol, 100_000_000n);
    assert.equal(await env.bal(env.ata(env.usdcMint, env.vaultPda(alice.publicKey))), 100_000_000n);
    assert.equal(await env.bal(env.ata(env.usdcMint, env.vaultPda(bob.publicKey))), 100_000_000n);
  });

  it("settle_match MINT: opposite-outcome buys mint a complete set into the pool", async () => {
    const aliceVault = env.vaultPda(alice.publicKey);
    const bobVault = env.vaultPda(bob.publicKey);
    await env.ensureOutcomeAta(market.yesMint, aliceVault);
    await env.ensureOutcomeAta(market.noMint, bobVault);

    const expiry = futureExpiry();
    const aliceOrder: Order = { maker: alice.publicKey, marketId, outcome: OUTCOME_YES, side: SIDE_BUY, price: 60, size: 100n, feeBps: 0, expiry, salt: 1n };
    const bobOrder: Order = { maker: bob.publicKey, marketId, outcome: OUTCOME_NO, side: SIDE_BUY, price: 40, size: 100n, feeBps: 0, expiry, salt: 2n };

    await env.settle(market, alice, aliceOrder, bob, bobOrder, MATCH_MINT, 60, 100n);

    assert.equal(await env.bal(env.ata(env.usdcMint, aliceVault)), 40_000_000n, "alice vault usdc");
    assert.equal(await env.bal(env.ata(env.usdcMint, bobVault)), 60_000_000n, "bob vault usdc");
    assert.equal(await env.bal(market.poolUsdc), 100_000_000n, "pool collateral");
    assert.equal(await env.bal(env.ata(market.yesMint, aliceVault)), 100n, "alice YES shares");
    assert.equal(await env.bal(env.ata(market.noMint, bobVault)), 100n, "bob NO shares");
  });

  it("settle_match NORMAL: Alice sells 40 YES to Carol peer-to-peer", async () => {
    const aliceVault = env.vaultPda(alice.publicKey);
    const carolVault = env.vaultPda(carol.publicKey);
    await env.ensureOutcomeAta(market.yesMint, carolVault);

    const expiry = futureExpiry();
    const carolOrder: Order = { maker: carol.publicKey, marketId, outcome: OUTCOME_YES, side: SIDE_BUY, price: 70, size: 40n, feeBps: 0, expiry, salt: 3n };
    const aliceSell: Order = { maker: alice.publicKey, marketId, outcome: OUTCOME_YES, side: SIDE_SELL, price: 70, size: 40n, feeBps: 0, expiry, salt: 4n };

    const carolBefore = await env.bal(env.ata(env.usdcMint, carolVault));
    const aliceBefore = await env.bal(env.ata(env.usdcMint, aliceVault));

    await env.settle(market, carol, carolOrder, alice, aliceSell, MATCH_NORMAL, 70, 40n);

    assert.equal(await env.bal(env.ata(env.usdcMint, carolVault)), carolBefore - 28_000_000n, "carol paid");
    assert.equal(await env.bal(env.ata(env.usdcMint, aliceVault)), aliceBefore + 28_000_000n, "alice received");
    assert.equal(await env.bal(env.ata(market.yesMint, carolVault)), 40n, "carol YES shares");
    assert.equal(await env.bal(env.ata(market.yesMint, aliceVault)), 60n, "alice YES shares left");
  });

  it("resolve_market + redeem: YES wins, Alice redeems shares 1:1 for USDC", async () => {
    await env.program.methods
      .resolveMarket(OUTCOME_YES)
      .accountsPartial({ market: market.market, resolver: env.operator.publicKey })
      .rpc();

    const aliceVault = env.vaultPda(alice.publicKey);
    const aliceWalletUsdc = env.ata(env.usdcMint, alice.publicKey);
    const walletBefore = await env.bal(aliceWalletUsdc);

    await env.program.methods
      .redeem(OUTCOME_YES, new anchor.BN(60))
      .accountsPartial({
        market: market.market,
        vault: aliceVault,
        outcomeMint: market.yesMint,
        userOutcomeAta: env.ata(market.yesMint, aliceVault),
        poolUsdc: market.poolUsdc,
        userUsdcAta: aliceWalletUsdc,
        user: alice.publicKey,
        tokenProgram: TOKEN_PROGRAM_ID,
      })
      .signers([alice])
      .rpc();

    assert.equal(await env.bal(aliceWalletUsdc), walletBefore + 60_000_000n, "alice redeemed 60 USDC");
    assert.equal(await env.bal(env.ata(market.yesMint, aliceVault)), 0n, "alice YES burned");
    assert.equal(await env.bal(market.poolUsdc), 40_000_000n, "pool after alice redeem");
  });
});
