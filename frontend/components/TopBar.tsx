"use client";

import { usd } from "@/lib/format";

const NAV = ["Markets", "Combos", "Precision", "Portfolio"];

export function TopBar({ balanceMicro }: { balanceMicro: number }) {
  return (
    <header className="sticky top-0 z-40 rule-b bg-bg/85 backdrop-blur-md">
      <div className="mx-auto flex h-14 max-w-[1200px] items-center justify-between px-5 sm:px-8">
        <div className="flex items-center gap-8">
          <span className="font-mono text-[13px] font-bold uppercase tracking-[0.24em] text-ink">
            Pitch<span className="text-accent">market</span>
          </span>
          <nav className="hidden items-center gap-6 md:flex" aria-label="Primary">
            {NAV.map((item, i) => (
              <a
                key={item}
                href="#"
                aria-current={i === 0 ? "page" : undefined}
                className={`text-[13px] transition-colors ${
                  i === 0
                    ? "text-ink"
                    : "text-dim hover:text-muted"
                }`}
              >
                {item}
              </a>
            ))}
          </nav>
        </div>

        <div className="flex items-center gap-5">
          <div className="hidden font-mono text-[12.5px] text-muted tnum sm:block">
            <span className="text-dim">vault</span>{" "}
            <span className="text-ink">{usd(balanceMicro)}</span>
          </div>
          <button className="font-mono text-[12.5px] text-muted transition-colors hover:text-ink">
            <span className="mr-1.5 inline-block h-1.5 w-1.5 rounded-full bg-accent align-middle" />
            7xQp…4mF2
          </button>
        </div>
      </div>
    </header>
  );
}
