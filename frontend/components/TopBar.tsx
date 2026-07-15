"use client";

import { usd } from "@/lib/format";
import { Wallet } from "lucide-react";

const NAV = ["Markets", "Combos", "Precision", "Portfolio"];

export function TopBar({ balanceMicro }: { balanceMicro: number }) {
  return (
    <header className="sticky top-0 z-40 border-b border-line bg-bg/80 backdrop-blur-lg">
      <div className="mx-auto flex max-w-[1180px] items-center justify-between gap-4 px-5 py-3.5">
        <div className="flex items-center gap-2.5">
          <span
            className="grid h-8 w-8 place-items-center rounded-lg text-[15px] shadow-[0_0_20px_rgba(255,210,30,.32)]"
            style={{
              background:
                "conic-gradient(from 210deg,#ffd21e,#ff7a1e,#ff2e4d,#ffd21e)",
            }}
            aria-hidden
          >
            ⚽
          </span>
          <span className="text-[19px] font-extrabold tracking-tight">
            PitchMarket
          </span>
        </div>

        <nav className="hidden items-center gap-1 md:flex" aria-label="Primary">
          {NAV.map((item, i) => (
            <a
              key={item}
              href="#"
              aria-current={i === 0 ? "page" : undefined}
              className={`rounded-lg px-3.5 py-2 text-[13.5px] font-semibold transition-colors ${
                i === 0
                  ? "bg-panel2 text-ink"
                  : "text-muted hover:text-ink"
              }`}
            >
              {item}
            </a>
          ))}
        </nav>

        <div className="flex items-center gap-3">
          <div className="hidden font-mono text-[13px] text-muted sm:block tnum">
            Vault <span className="font-bold text-yes">{usd(balanceMicro)}</span>
          </div>
          <button
            className="flex items-center gap-2 rounded-lg border border-line2 bg-gradient-to-b from-panel2 to-panel px-3.5 py-2 text-[13px] font-bold text-ink transition-colors hover:border-dim"
            aria-label="Wallet: 7xQp…4mF2"
          >
            <Wallet size={14} className="text-verify" />
            <span className="font-mono">7xQp…4mF2</span>
          </button>
        </div>
      </div>
    </header>
  );
}
