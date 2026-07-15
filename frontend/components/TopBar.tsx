"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { usd } from "@/lib/format";
import { usePitchWallet } from "@/lib/wallet";

const NAV: { label: string; href: string }[] = [
  { label: "Markets", href: "/" },
  { label: "Combos", href: "/combos" },
  { label: "Portfolio", href: "/portfolio" },
];

export function TopBar({ balanceMicro }: { balanceMicro: number }) {
  const wallet = usePitchWallet();
  const path = usePathname();
  const isActive = (href: string) =>
    href === "/" ? path === "/" || path.startsWith("/market") : path.startsWith(href) && href !== "#";

  return (
    <header className="sticky top-0 z-40 rule-b bg-bg/85 backdrop-blur-md">
      <div className="mx-auto flex h-14 max-w-[1200px] items-center justify-between px-5 sm:px-8">
        <div className="flex items-center gap-8">
          <Link
            href="/"
            className="font-mono text-[13px] font-bold uppercase tracking-[0.24em] text-ink"
          >
            Pitch<span className="text-accent">market</span>
          </Link>
          <nav className="hidden items-center gap-6 md:flex" aria-label="Primary">
            {NAV.map((item) => {
              const active = isActive(item.href);
              return (
                <Link
                  key={item.label}
                  href={item.href}
                  aria-current={active ? "page" : undefined}
                  className={`text-[13px] transition-colors ${
                    active ? "text-ink" : "text-dim hover:text-muted"
                  }`}
                >
                  {item.label}
                </Link>
              );
            })}
          </nav>
        </div>

        <div className="flex items-center gap-5">
          {wallet.address && (
            <div className="hidden font-mono text-[12.5px] text-muted tnum sm:block">
              <span className="text-dim">vault</span>{" "}
              <span className="text-ink">{usd(balanceMicro)}</span>
            </div>
          )}
          {wallet.address ? (
            <button
              onClick={wallet.disconnect}
              title="Disconnect"
              className="font-mono text-[12.5px] text-muted transition-colors hover:text-ink"
            >
              <span className="mr-1.5 inline-block h-1.5 w-1.5 rounded-full bg-accent align-middle" />
              {short(wallet.address)}
              {wallet.isDemo && <span className="ml-1.5 text-dim">demo</span>}
            </button>
          ) : (
            <button
              onClick={wallet.connect}
              disabled={!wallet.ready}
              className="font-mono text-[12.5px] text-accent transition-[filter] hover:brightness-110 disabled:text-dim"
            >
              Connect wallet
            </button>
          )}
        </div>
      </div>
    </header>
  );
}

function short(addr: string): string {
  return `${addr.slice(0, 4)}…${addr.slice(-4)}`;
}
