"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { Check } from "lucide-react";
import { api, explorerAddr, explorerTx } from "@/lib/api";
import { demoBalanceMicro } from "@/lib/fixtures";
import type { Settlement } from "@/lib/types";
import { usd } from "@/lib/format";
import { TopBar } from "@/components/TopBar";
import { VerifyLink } from "@/components/VerifyLink";

export default function SettlementPage({ params }: { params: { id: string } }) {
  const [s, setS] = useState<Settlement | null>(null);
  const [err, setErr] = useState(false);

  useEffect(() => {
    let alive = true;
    api
      .getSettlement(params.id)
      .then((r) => alive && setS(r))
      .catch(() => alive && setErr(true));
    return () => {
      alive = false;
    };
  }, [params.id]);

  return (
    <div className="min-h-screen">
      <TopBar balanceMicro={demoBalanceMicro} />
      <main className="mx-auto max-w-[760px] px-5 sm:px-8">
        {!s && !err && <div className="py-24 font-mono text-[13px] text-dim">Loading…</div>}
        {err && <div className="py-24 font-mono text-[13px] text-down">Couldn’t load settlement.</div>}

        {s && (
          <>
            <section className="py-10 sm:py-14">
              <p className="mb-6 eyebrow">Settled · {s.resolved_by}</p>
              <h1 className="mb-8 text-[15px] font-semibold text-ink">{s.title}</h1>

              <div className="flex items-baseline gap-4">
                <span
                  className={`font-mono text-[56px] font-light leading-none tracking-tight sm:text-[76px] ${
                    s.winner === "YES" ? "text-accent" : "text-down"
                  }`}
                >
                  {s.winner}
                </span>
                <span className="text-[18px] text-muted">resolved</span>
              </div>
              <p className="mt-4 font-mono text-[13px] text-muted tnum">{s.scoreline}</p>
            </section>

            {/* trust moment */}
            <div className="flex flex-wrap items-center justify-between gap-4 rule-t rule-b py-6">
              <div>
                <p className="flex items-center gap-2 text-[14px] font-semibold text-ink">
                  <Check size={15} className="text-accent" /> Verified on Solana
                </p>
                <p className="mt-1 font-mono text-[12px] text-dim">
                  Outcome and settlement are on devnet — check them yourself, don’t trust us.
                </p>
              </div>
              <VerifyLink href={explorerTx(s.timeline[2].tx!)}>View resolution</VerifyLink>
            </div>

            {/* your result */}
            <section className="py-8">
              <p className="mb-4 eyebrow">Your position</p>
              <div className="flex items-baseline justify-between">
                <span className="font-mono text-[15px] text-ink tnum">
                  {s.your_shares} YES
                </span>
                <span className="font-mono text-[22px] font-light text-accent tnum">
                  {usd(s.your_payout_micro)}
                </span>
              </div>
              <p className="mt-1.5 font-mono text-[12px] text-dim">
                Winning shares burned, paid 1:1 from the pool.
              </p>
            </section>

            {/* settlement trail */}
            <section className="rule-t py-8">
              <p className="mb-6 eyebrow">Settlement trail</p>
              <ol>
                {s.timeline.map((step, i) => (
                  <li
                    key={i}
                    className="flex items-start justify-between gap-4 border-b border-line py-4 last:border-b-0"
                  >
                    <div className="flex gap-4">
                      <span className="mt-1 font-mono text-[11px] text-dim tnum">
                        {String(i + 1).padStart(2, "0")}
                      </span>
                      <div>
                        <p className="text-[13.5px] font-medium text-ink">{step.label}</p>
                        <p className="mt-0.5 font-mono text-[11.5px] text-dim">{step.detail}</p>
                      </div>
                    </div>
                    {step.tx ? (
                      <VerifyLink href={explorerTx(step.tx)}>tx</VerifyLink>
                    ) : (
                      <span className="font-mono text-[11px] text-dim">off-chain</span>
                    )}
                  </li>
                ))}
              </ol>
            </section>

            <footer className="flex flex-wrap items-center justify-between gap-3 rule-t py-6 font-mono text-[11px] text-dim">
              <span>
                Program{" "}
                <a
                  href={explorerAddr(s.program_id)}
                  target="_blank"
                  rel="noreferrer"
                  className="text-muted transition-colors hover:text-ink"
                >
                  {s.program_id.slice(0, 6)}…{s.program_id.slice(-6)}
                </a>
              </span>
              <Link href="/" className="text-accent hover:brightness-125">
                ← Back to markets
              </Link>
            </footer>
          </>
        )}
      </main>
    </div>
  );
}
