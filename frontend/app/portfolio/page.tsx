"use client";

// Portfolio: positions marked at the best bid (BBP — the price you could exit
// at NOW), realized + unrealized P&L, one-click exit (signs a SELL at the best
// bid), and cancellable open orders.

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import bs58 from "bs58";
import { usePitchWallet } from "@/lib/wallet";
import { api, explorerTx } from "@/lib/api";
import { borshOrder, fromHex, randomSalt, toHex } from "@/lib/borsh";
import type { Portfolio, Position, Market } from "@/lib/types";
import { usd, shares as fmtShares, shortHash } from "@/lib/format";
import { TopBar } from "@/components/TopBar";
import { VerifyLink } from "@/components/VerifyLink";
import { useWatchlist } from "@/lib/watchlist";
import { kindOf } from "@/lib/kinds";

interface PosCalc {
  p: Position;
  side: "YES" | "NO";
  qty: number;
  entry: number;
  cur: number; // exit mark (BBP) in the held side's terms
  valueMicro: number;
  unrealizedMicro: number;
}

function calc(p: Position): PosCalc {
  const side: "YES" | "NO" = p.yes > 0 ? "YES" : "NO";
  const qty = side === "YES" ? p.yes : p.no;
  const cur = p.current > 0 ? (side === "YES" ? p.current : 100 - p.current) : p.avg_cost;
  const valueMicro = qty * cur * 10_000;
  const costMicro = qty * p.avg_cost * 10_000;
  return { p, side, qty, entry: p.avg_cost, cur, valueMicro, unrealizedMicro: valueMicro - costMicro };
}

export default function PortfolioPage() {
  const wallet = usePitchWallet();
  const [pf, setPf] = useState<Portfolio | null>(null);
  const [busy, setBusy] = useState<string | null>(null); // market_id or order_hash in flight
  const [error, setError] = useState<string | null>(null);
  const { isWatched } = useWatchlist();
  const [markets, setMarkets] = useState<Market[]>([]);
  useEffect(() => {
    api.listMarkets().then(setMarkets).catch(() => {});
  }, []);
  const watched = markets.filter((m) => isWatched(m.market_id));

  const load = useCallback(() => {
    api
      .getPortfolio(wallet.address)
      .then(setPf)
      .catch((e) => setError(e.message));
  }, [wallet.address]);

  useEffect(() => {
    load();
    const t = setInterval(load, 6000);
    return () => clearInterval(t);
  }, [load]);

  // Exit: sign a SELL of the full position at the current best bid. Same
  // signing path as the trade panel — the backend can't tell them apart.
  async function exit(x: PosCalc) {
    if (!wallet.address || x.cur <= 0 || busy) return;
    setError(null);
    setBusy(x.p.market_id);
    try {
      const salt = randomSalt();
      const outcome = x.side === "YES" ? 1 : 0;
      const msg = borshOrder({
        maker: bs58.decode(wallet.address),
        marketId: fromHex(x.p.market_id),
        outcome,
        side: 1, // SELL
        price: Math.max(1, x.side === "YES" ? x.cur : 100 - x.cur),
        size: BigInt(x.qty),
        feeBps: 0,
        expiry: 0n,
        salt,
      });
      const sig = await wallet.signMessage(msg);
      await api.postOrder({
        maker: wallet.address,
        market_id: x.p.market_id,
        outcome,
        side: 1,
        price: Math.max(1, x.side === "YES" ? x.cur : 100 - x.cur),
        size: x.qty,
        fee_bps: 0,
        expiry: 0,
        salt: Number(salt),
        sig: toHex(sig),
      });
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "exit failed");
    } finally {
      setBusy(null);
    }
  }

  async function cancel(orderHash: string) {
    if (!wallet.address || busy) return;
    setError(null);
    setBusy(orderHash);
    try {
      await api.cancelOrder(orderHash, wallet.address);
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "cancel failed");
    } finally {
      setBusy(null);
    }
  }

  const positions = pf?.positions.filter((p) => p.yes > 0 || p.no > 0).map(calc) ?? [];
  const totalValue = positions.reduce((a, x) => a + x.valueMicro, 0);
  const totalUnrealized = positions.reduce((a, x) => a + x.unrealizedMicro, 0);
  const totalRealized = pf?.positions.reduce((a, p) => a + p.realized, 0) ?? 0;
  const totalPnl = totalUnrealized + totalRealized;
  const noActivity =
    !!pf &&
    positions.length === 0 &&
    pf.orders.length === 0 &&
    pf.precision.length === 0 &&
    pf.combos.length === 0 &&
    pf.history.length === 0;

  return (
    <div className="min-h-screen">
      <TopBar balanceMicro={pf?.balance_micro ?? 0} />
      <main className="mx-auto max-w-[900px] px-5 sm:px-8">
        {!wallet.address && (
          <div className="py-24 text-center">
            <p className="mb-4 text-[14px] text-muted">Connect a wallet to see your portfolio.</p>
            <button
              onClick={wallet.connect}
              className="bg-accent px-6 py-3 text-[14px] font-semibold text-bg transition-[transform,filter] duration-150 ease-out-strong hover:brightness-110 active:scale-[0.98]"
            >
              Connect wallet
            </button>
          </div>
        )}

        {wallet.address && !pf && (
          <div className="py-24 font-mono text-[13px] text-dim">Loading…</div>
        )}

        {wallet.address && pf && (
          <>
            {/* summary — two focal numbers: vault + total P&L, each with a muted breakdown */}
            <section className="rule-b py-10 sm:py-12">
              <div className="grid gap-8 sm:grid-cols-2">
                <div>
                  <p className="eyebrow mb-3">Vault</p>
                  <p className="font-mono text-[38px] font-light leading-none text-ink tnum sm:text-[48px]">
                    {usd(pf.balance_micro)}
                  </p>
                  <p className="mt-3 font-mono text-[11.5px] text-dim">
                    + {usd(totalValue)} in positions · BBP
                  </p>
                </div>
                <div className="sm:text-right">
                  <p className="eyebrow mb-3">Total P&amp;L</p>
                  <p
                    className={`font-mono text-[38px] font-light leading-none tnum sm:text-[48px] ${
                      totalPnl > 0 ? "text-accent" : totalPnl < 0 ? "text-down" : "text-ink"
                    }`}
                  >
                    {totalPnl >= 0 ? "+" : "−"}
                    {usd(Math.abs(totalPnl))}
                  </p>
                  <p className="mt-3 font-mono text-[11.5px] text-dim">
                    unrealised{" "}
                    <span className={totalUnrealized < 0 ? "text-down" : "text-muted"}>
                      {totalUnrealized >= 0 ? "+" : "−"}
                      {usd(Math.abs(totalUnrealized))}
                    </span>{" "}
                    · realised{" "}
                    <span className={totalRealized < 0 ? "text-down" : "text-muted"}>
                      {totalRealized >= 0 ? "+" : "−"}
                      {usd(Math.abs(totalRealized))}
                    </span>
                  </p>
                </div>
              </div>
            </section>

            {watched.length > 0 && (
              <section className="rule-b py-6">
                <h2 className="eyebrow mb-4">Watchlist</h2>
                <div className="-mx-1 flex snap-x snap-mandatory gap-3 overflow-x-auto px-1 pb-1">
                  {watched.map((m) => (
                    <Link
                      key={m.market_id}
                      href={m.type === "precision" ? `/precision/${m.market_id}` : `/market/${m.market_id}`}
                      className="group flex min-h-[76px] w-[220px] shrink-0 snap-start flex-col justify-between rounded-[3px] border border-line bg-line/40 p-3.5 transition-[transform,border-color,background-color] duration-150 ease-out-strong hover:-translate-y-px hover:border-line2 hover:bg-line/70"
                    >
                      <span className="line-clamp-2 text-[13px] leading-snug text-ink transition-colors group-hover:text-accent">
                        {m.title}
                      </span>
                      <span className="mt-2 flex items-center justify-between font-mono text-[10px] uppercase tracking-[0.12em] text-dim">
                        <span className="truncate">{kindOf(m)}</span>
                        <span className={m.status === "settled" ? "text-accent/70" : ""}>
                          {m.status !== "open" ? m.status : ""}
                        </span>
                      </span>
                    </Link>
                  ))}
                </div>
              </section>
            )}

            {error && (
              <p className="mb-4 font-mono text-[12px] text-down" role="alert">
                {error}
              </p>
            )}

            {/* positions */}
            {positions.length > 0 && (
              <Section title="Positions" count={positions.length}>
                <Grid cols="grid-cols-[1fr_auto_auto] sm:grid-cols-[1fr_repeat(5,minmax(72px,auto))]">
                    <Head>Market</Head>
                    <Head className="hidden text-right sm:block">Size</Head>
                    <Head className="hidden text-right sm:block">Avg → BBP</Head>
                    <Head className="text-right">Value</Head>
                    <Head className="text-right">P&L</Head>
                    <Head className="text-right"> </Head>
                  </Grid>
                  {positions.map((x) => (
                    <Grid
                      key={x.p.market_id}
                      cols="grid-cols-[1fr_auto_auto] sm:grid-cols-[1fr_repeat(5,minmax(72px,auto))]"
                      className="border-b border-line py-3.5 last:border-b-0"
                    >
                      <div className="min-w-0">
                        <Link
                          href={`/market/${x.p.market_id}`}
                          className="block truncate text-[13.5px] text-ink hover:text-accent"
                        >
                          {x.p.title}
                        </Link>
                        <span
                          className={`font-mono text-[11px] ${x.side === "YES" ? "text-accent" : "text-down"}`}
                        >
                          {x.side} · {fmtShares(x.qty)}
                        </span>
                      </div>
                      <Num className="hidden text-right sm:block">{fmtShares(x.qty)}</Num>
                      <Num className="hidden text-right sm:block">
                        {x.entry}¢ → {x.cur}¢
                      </Num>
                      <Num className="text-right text-ink">{usd(x.valueMicro)}</Num>
                      <Num
                        className={`text-right ${x.unrealizedMicro >= 0 ? "text-accent" : "text-down"}`}
                      >
                        {x.unrealizedMicro >= 0 ? "+" : "−"}
                        {usd(Math.abs(x.unrealizedMicro))}
                      </Num>
                      <div className="text-right">
                        <button
                          onClick={() => exit(x)}
                          disabled={busy !== null || x.cur <= 0}
                          title={x.cur <= 0 ? "No bid to exit into" : `Sell ${fmtShares(x.qty)} @ ${x.cur}¢`}
                          className="font-mono text-[11px] text-dim transition-colors hover:text-down disabled:opacity-40"
                        >
                          {busy === x.p.market_id ? "exiting…" : "exit"}
                        </button>
                      </div>
                    </Grid>
                  ))}
              </Section>
            )}

            {/* open orders */}
            {pf.orders.length > 0 && (
              <Section title="Open orders" count={pf.orders.length}>
                {pf.orders.map((o) => {
                  const filled = o.size - o.remaining;
                  return (
                    <div
                      key={o.order_hash}
                      className="flex items-center justify-between border-b border-line py-3.5 last:border-b-0"
                    >
                      <div className="min-w-0">
                        <Link
                          href={`/market/${o.market_id}`}
                          className="block truncate text-[13.5px] text-ink hover:text-accent"
                        >
                          {o.title}
                        </Link>
                        <p className="font-mono text-[11px] text-dim">
                          <span className={o.outcome === "YES" ? "text-accent" : "text-down"}>
                            {o.side} {o.outcome}
                          </span>{" "}
                          · {o.price}¢ · {shortHash(o.order_hash, 6, 4)}
                        </p>
                      </div>
                      <div className="flex items-center gap-5 text-right">
                        <div className="font-mono text-[12px] tnum">
                          <div className="text-muted">{fmtShares(o.remaining)}</div>
                          <div className="text-dim">
                            {filled > 0 ? `${fmtShares(filled)} filled` : `of ${fmtShares(o.size)}`}
                          </div>
                        </div>
                        <button
                          onClick={() => cancel(o.order_hash)}
                          disabled={busy !== null}
                          className="font-mono text-[11px] text-dim transition-colors hover:text-down disabled:opacity-40"
                        >
                          {busy === o.order_hash ? "cancelling…" : "cancel"}
                        </button>
                      </div>
                    </div>
                  );
                })}
              </Section>
            )}

            {/* precision pools */}
            {pf.precision.length > 0 && (
              <Section title="Precision pools" count={pf.precision.length}>
                {pf.precision.map((p) => {
                  const settled = p.status === "settled" || p.status === "void";
                  const payout = p.payout_micro ?? 0;
                  const pnl = settled ? payout - p.stake_micro : 0;
                  return (
                    <div
                      key={p.market_id}
                      className="flex items-center justify-between border-b border-line py-3.5 last:border-b-0"
                    >
                      <div className="min-w-0">
                        <Link
                          href={`/precision/${p.market_id}`}
                          className="block truncate text-[13.5px] text-ink hover:text-accent"
                        >
                          {p.title}
                        </Link>
                        <p className="font-mono text-[11px] text-dim">
                          guess {p.guess} · stake {usd(p.stake_micro)}
                          {p.score != null && <> · score {p.score.toFixed(2)}</>}
                        </p>
                      </div>
                      <div className="text-right font-mono text-[12px] tnum">
                        {settled ? (
                          <>
                            <div className={pnl >= 0 ? "text-accent" : "text-down"}>
                              {pnl >= 0 ? "+" : "−"}
                              {usd(Math.abs(pnl))}
                            </div>
                            <div className="text-dim">
                              {p.status === "void" ? "void · refunded" : `paid ${usd(payout)}`}
                            </div>
                          </>
                        ) : (
                          <span className="uppercase text-dim">
                            {p.status === "closed" ? "locked" : "open"}
                          </span>
                        )}
                      </div>
                    </div>
                  );
                })}
              </Section>
            )}

            {/* combos */}
            {pf.combos.length > 0 && (
              <Section title="Combos" count={pf.combos.length}>
                {pf.combos.map((c) => {
                  const pnl =
                    c.status === "won"
                      ? c.payout_micro - c.stake_micro
                      : c.status === "lost"
                        ? -c.stake_micro
                        : 0;
                  const done = c.status === "won" || c.status === "lost";
                  return (
                    <div
                      key={c.quote_hash}
                      className="flex items-start justify-between gap-4 border-b border-line py-4 last:border-b-0"
                    >
                      <div className="min-w-0 flex-1">
                        <div className="mb-2 flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                          <span className="text-[13px] font-medium text-ink">
                            {c.legs}-leg combo
                          </span>
                          <span className="font-mono text-[11px] text-dim tnum">
                            stake {usd(c.stake_micro)} → pays {usd(c.payout_micro)} ·{" "}
                            {shortHash(c.quote_hash, 4, 4)}
                          </span>
                        </div>
                        <ul className="space-y-1">
                          {c.legDetails.map((l, i) => (
                            <li
                              key={i}
                              className="flex items-baseline gap-2 font-mono text-[11.5px]"
                            >
                              <span
                                className={`w-7 shrink-0 ${l.outcome === 1 ? "text-accent" : "text-down"}`}
                              >
                                {l.outcome === 1 ? "YES" : "NO"}
                              </span>
                              <span className="min-w-0 truncate text-muted">{l.title}</span>
                            </li>
                          ))}
                        </ul>
                      </div>
                      <div className="flex items-center gap-4 text-right font-mono text-[12px] tnum">
                        <div>
                          <div
                            className={`uppercase ${
                              c.status === "won"
                                ? "text-accent"
                                : c.status === "lost"
                                  ? "text-down"
                                  : "text-dim"
                            }`}
                          >
                            {c.status === "accepted" ? "pending" : c.status}
                          </div>
                          {done && (
                            <div className={pnl >= 0 ? "text-accent" : "text-down"}>
                              {pnl >= 0 ? "+" : "−"}
                              {usd(Math.abs(pnl))}
                            </div>
                          )}
                        </div>
                        {c.resolve_tx && <VerifyLink href={explorerTx(c.resolve_tx)}>tx</VerifyLink>}
                      </div>
                    </div>
                  );
                })}
              </Section>
            )}

            {/* history — demoted to a secondary, muted section (reference) */}
            {pf.history.length > 0 && (
              <Section title="History" count={pf.history.length} secondary>
                {pf.history.map((h, i) => (
                  <div
                    key={i}
                    className="flex items-center justify-between border-b border-line py-3.5 last:border-b-0"
                  >
                    <div className="min-w-0">
                      <p className="truncate text-[13.5px] text-ink">{h.title}</p>
                      <p className="font-mono text-[11px] text-dim">
                        {fmtShares(h.size)} @ {h.price}¢
                      </p>
                    </div>
                    {h.tx ? (
                      <VerifyLink href={explorerTx(h.tx)}>settle tx</VerifyLink>
                    ) : (
                      <span className="font-mono text-[11px] text-dim">settling…</span>
                    )}
                  </div>
                ))}
              </Section>
            )}

            {noActivity && (
              <div className="rule-t py-20 text-center">
                <p className="text-[14px] text-muted">Nothing here yet.</p>
                <p className="mt-2 text-[13px] leading-relaxed text-dim">
                  Fund your wallet and place a trade — positions, pools, combos, and
                  history will show up here.
                </p>
              </div>
            )}

            <div className="py-10" />
          </>
        )}
      </main>
    </div>
  );
}

function Section({
  title,
  count,
  secondary,
  children,
}: {
  title: string;
  count?: number;
  secondary?: boolean;
  children: React.ReactNode;
}) {
  return (
    <section className="rule-t py-8">
      <h2 className="mb-5 flex items-baseline gap-2">
        <span className={secondary ? "eyebrow" : "text-[13px] font-semibold text-ink"}>{title}</span>
        {count ? <span className="font-mono text-[11px] text-dim tnum">{count}</span> : null}
      </h2>
      {children}
    </section>
  );
}

function Grid({
  cols,
  className = "",
  children,
}: {
  cols: string;
  className?: string;
  children: React.ReactNode;
}) {
  return <div className={`grid ${cols} items-center gap-3 ${className}`}>{children}</div>;
}

function Head({ children, className = "" }: { children: React.ReactNode; className?: string }) {
  return <span className={`eyebrow ${className}`}>{children}</span>;
}

function Num({ children, className = "" }: { children: React.ReactNode; className?: string }) {
  return <span className={`font-mono text-[12.5px] text-muted tnum ${className}`}>{children}</span>;
}
