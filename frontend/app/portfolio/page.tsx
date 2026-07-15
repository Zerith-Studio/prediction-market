"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePitchWallet } from "@/lib/wallet";
import { api, explorerTx } from "@/lib/api";
import type { Portfolio, Position } from "@/lib/types";
import { usd, shares as fmtShares, shortHash } from "@/lib/format";
import { TopBar } from "@/components/TopBar";
import { VerifyLink } from "@/components/VerifyLink";

interface PosCalc {
  p: Position;
  side: "YES" | "NO";
  qty: number;
  entry: number;
  cur: number;
  valueMicro: number;
  costMicro: number;
  pnlMicro: number;
}

function calc(p: Position): PosCalc {
  const side: "YES" | "NO" = p.yes > 0 ? "YES" : "NO";
  const qty = side === "YES" ? p.yes : p.no;
  const cur = side === "YES" ? p.current : 100 - p.current;
  const valueMicro = qty * cur * 10_000;
  const costMicro = qty * p.avg_cost * 10_000;
  return { p, side, qty, entry: p.avg_cost, cur, valueMicro, costMicro, pnlMicro: valueMicro - costMicro };
}

export default function PortfolioPage() {
  const wallet = usePitchWallet();
  const [pf, setPf] = useState<Portfolio | null>(null);

  useEffect(() => {
    let alive = true;
    api.getPortfolio(wallet.address).then((r) => alive && setPf(r));
    return () => {
      alive = false;
    };
  }, [wallet.address]);

  const positions = pf?.positions.map(calc) ?? [];
  const totalValue = positions.reduce((a, x) => a + x.valueMicro, 0);
  const totalPnl = positions.reduce((a, x) => a + x.pnlMicro, 0);

  return (
    <div className="min-h-screen">
      <TopBar balanceMicro={pf?.balance_micro ?? 0} />
      <main className="mx-auto max-w-[900px] px-5 sm:px-8">
        {!pf && <div className="py-24 font-mono text-[13px] text-dim">Loading…</div>}

        {pf && (
          <>
            {/* summary */}
            <section className="grid grid-cols-2 gap-8 py-10 sm:grid-cols-4">
              <Stat label="Vault" value={usd(pf.balance_micro)} />
              <Stat label="Position value" value={usd(totalValue)} />
              <Stat
                label="Unrealised P&L"
                value={`${totalPnl >= 0 ? "+" : "−"}${usd(Math.abs(totalPnl))}`}
                tone={totalPnl >= 0 ? "up" : "down"}
              />
              <Stat label="Open orders" value={String(pf.orders.length)} />
            </section>

            {/* positions */}
            <Section title="Positions">
              <Grid cols="grid-cols-[1fr_auto_auto_auto] sm:grid-cols-[1fr_repeat(4,minmax(84px,auto))]">
                <Head>Market</Head>
                <Head className="text-right">Size</Head>
                <Head className="hidden text-right sm:block">Avg</Head>
                <Head className="text-right">Value</Head>
                <Head className="text-right">P&L</Head>
              </Grid>
              {positions.map((x) => (
                <Grid
                  key={x.p.market_id}
                  cols="grid-cols-[1fr_auto_auto_auto] sm:grid-cols-[1fr_repeat(4,minmax(84px,auto))]"
                  className="border-b border-line py-3.5 last:border-b-0"
                >
                  <div className="min-w-0">
                    <Link
                      href={`/market/${x.p.market_id}`}
                      className="block truncate text-[13.5px] text-ink hover:text-accent"
                    >
                      {x.p.title}
                    </Link>
                    <span className={`font-mono text-[11px] ${x.side === "YES" ? "text-accent" : "text-down"}`}>
                      {x.side}
                    </span>
                  </div>
                  <Num className="text-right">{fmtShares(x.qty)}</Num>
                  <Num className="hidden text-right sm:block">{x.entry}¢</Num>
                  <Num className="text-right text-ink">{usd(x.valueMicro)}</Num>
                  <Num className={`text-right ${x.pnlMicro >= 0 ? "text-accent" : "text-down"}`}>
                    {x.pnlMicro >= 0 ? "+" : "−"}
                    {usd(Math.abs(x.pnlMicro))}
                  </Num>
                </Grid>
              ))}
            </Section>

            {/* open orders */}
            <Section title="Open orders">
              {pf.orders.length === 0 ? (
                <Empty>No resting orders. Signed limit orders show up here.</Empty>
              ) : (
                pf.orders.map((o) => {
                  const filled = o.size - o.remaining;
                  return (
                    <div
                      key={o.order_hash}
                      className="flex items-center justify-between border-b border-line py-3.5 last:border-b-0"
                    >
                      <div className="min-w-0">
                        <p className="truncate text-[13.5px] text-ink">{o.title}</p>
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
                        <button className="font-mono text-[11px] text-dim transition-colors hover:text-down">
                          cancel
                        </button>
                      </div>
                    </div>
                  );
                })
              )}
            </Section>

            {/* history */}
            <Section title="History">
              {pf.history.map((h, i) => (
                <div
                  key={i}
                  className="flex items-center justify-between border-b border-line py-3.5 last:border-b-0"
                >
                  <div className="min-w-0">
                    <p className="truncate text-[13.5px] text-ink">{h.title}</p>
                    <p className="font-mono text-[11px] text-dim">
                      <span className={h.outcome === "YES" ? "text-accent" : "text-down"}>
                        {h.side} {h.outcome}
                      </span>{" "}
                      · {fmtShares(h.size)} @ {h.price}¢
                    </p>
                  </div>
                  <VerifyLink href={explorerTx(h.tx)}>settle tx</VerifyLink>
                </div>
              ))}
            </Section>

            <div className="py-10" />
          </>
        )}
      </main>
    </div>
  );
}

function Stat({ label, value, tone }: { label: string; value: string; tone?: "up" | "down" }) {
  return (
    <div>
      <p className="mb-2 eyebrow">{label}</p>
      <p
        className={`font-mono text-[20px] font-light tnum sm:text-[24px] ${
          tone === "up" ? "text-accent" : tone === "down" ? "text-down" : "text-ink"
        }`}
      >
        {value}
      </p>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="rule-t py-8">
      <h2 className="mb-5 text-[13px] font-semibold text-ink">{title}</h2>
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

function Empty({ children }: { children: React.ReactNode }) {
  return <p className="py-6 text-[13px] text-muted">{children}</p>;
}
