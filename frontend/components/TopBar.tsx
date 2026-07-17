"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { CommandPalette, SearchIcon } from "@/components/CommandPalette";
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
  const [searchOpen, setSearchOpen] = useState(false);
  const [modKey, setModKey] = useState("⌘");
  const isActive = (href: string) =>
    href === "/" ? path === "/" || path.startsWith("/market") : path.startsWith(href) && href !== "#";

  useEffect(() => {
    if (!/Mac|iPhone|iPad/.test(navigator.platform)) setModKey("Ctrl");
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setSearchOpen((v) => !v);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

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
          <button
            onClick={() => setSearchOpen(true)}
            aria-label="Search markets"
            className="flex items-center gap-2 font-mono text-[12.5px] text-dim transition-colors hover:text-muted"
          >
            <SearchIcon />
            <span className="hidden md:inline">Search</span>
            <kbd className="hidden rounded-[2px] border border-line2 px-1.5 py-0.5 text-[10px] tracking-[0.08em] md:inline">
              {modKey} K
            </kbd>
          </button>
          {wallet.address && (
            <div className="hidden font-mono text-[12.5px] text-muted tnum sm:block">
              <span className="text-dim">vault</span>{" "}
              <span className="text-ink">{usd(balanceMicro)}</span>
            </div>
          )}
          {wallet.address ? (
            <WalletMenu
              address={wallet.address}
              isDemo={wallet.isDemo}
              onDisconnect={wallet.disconnect}
            />
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
      <CommandPalette open={searchOpen} onClose={() => setSearchOpen(false)} />
    </header>
  );
}

function short(addr: string): string {
  return `${addr.slice(0, 4)}…${addr.slice(-4)}`;
}

function WalletMenu({
  address,
  isDemo,
  onDisconnect,
}: {
  address: string;
  isDemo: boolean;
  onDisconnect: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    window.addEventListener("mousedown", onDown);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(address);
      setCopied(true);
      setTimeout(() => setCopied(false), 1400);
    } catch {
      /* clipboard unavailable — ignore */
    }
  };

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        title="Wallet"
        className="font-mono text-[12.5px] text-muted transition-colors hover:text-ink"
      >
        <span className="mr-1.5 inline-block h-1.5 w-1.5 rounded-full bg-accent align-middle" />
        {short(address)}
        {isDemo && <span className="ml-1.5 text-dim">demo</span>}
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-full z-50 mt-2 w-[248px] rounded-[3px] border border-line2 bg-bg shadow-2xl"
        >
          <div className="rule-b px-3 py-2.5">
            <div className="eyebrow mb-1">Wallet</div>
            <div className="break-all font-mono text-[11.5px] leading-relaxed text-muted">
              {address}
            </div>
          </div>
          <button
            role="menuitem"
            onClick={copy}
            className="flex w-full items-center justify-between px-3 py-2.5 text-left font-mono text-[12.5px] text-muted transition-colors hover:bg-line/70 hover:text-ink"
          >
            <span>Copy address</span>
            {copied && <span className="text-accent">copied</span>}
          </button>
          <button
            role="menuitem"
            onClick={() => {
              setOpen(false);
              onDisconnect();
            }}
            className="flex w-full items-center px-3 py-2.5 text-left font-mono text-[12.5px] text-muted transition-colors hover:bg-line/70 hover:text-down"
          >
            Log out
          </button>
        </div>
      )}
    </div>
  );
}
