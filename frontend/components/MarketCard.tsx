"use client";

import Link from "next/link";
import type { Market, Match } from "@/lib/types";
import { kindOf } from "@/lib/kinds";
import { FlagPair } from "@/components/TeamFlag";
import { StarButton } from "@/components/StarButton";

// Market-card shells: bordered tile, subtle elevation. cardLink is the fully
// clickable variant (pools, settled markets) with a hover-lift + press feedback;
// cardBox is the static container used when the card holds its own Yes/No buttons.
const cardBase =
  "flex min-h-[68px] flex-col justify-between rounded-[3px] border border-line bg-line/40 p-4";
export const cardLink = `group ${cardBase} transition-[transform,border-color,background-color] duration-150 ease-out-strong hover:-translate-y-px hover:border-line2 hover:bg-line/70 active:translate-y-0`;
const cardBox = `${cardBase} transition-colors duration-150 hover:border-line2`;

// Yes/No quick-trade buttons — accent for YES, down for NO, with press feedback.
const yesBtn =
  "rounded-[2px] border border-accent/30 bg-accent/10 px-2.5 py-1 font-mono text-[11px] font-semibold uppercase tracking-wide text-accent transition-[transform,filter,background-color] duration-150 ease-out-strong hover:bg-accent/20 hover:brightness-110 active:scale-[0.96]";
const noBtn =
  "rounded-[2px] border border-down/30 bg-down/10 px-2.5 py-1 font-mono text-[11px] font-semibold uppercase tracking-wide text-down transition-[transform,filter,background-color] duration-150 ease-out-strong hover:bg-down/20 hover:brightness-110 active:scale-[0.96]";

// BinaryCard: open markets carry Yes/No buttons that deep-link into the trade
// panel with the outcome preselected; resolved markets show the result instead.
export function BinaryCard({ m, match }: { m: Market; match: Match }) {
  if (m.status === "open") {
    return (
      <div className={cardBox}>
        <Link
          href={`/market/${m.market_id}`}
          className="flex items-start gap-2.5 text-ink transition-colors hover:text-accent"
        >
          <FlagPair home={match.home} away={match.away} size={18} />
          <span className="line-clamp-2 min-w-0 text-[13.5px] leading-snug">{m.title}</span>
        </Link>
        <div className="mt-3 flex items-center justify-between gap-2">
          <span className="min-w-0 truncate font-mono text-[10px] uppercase tracking-[0.12em] text-dim">
            {kindOf(m)}
          </span>
          <div className="flex shrink-0 items-center gap-1.5">
            <StarButton marketId={m.market_id} />
            <Link href={`/market/${m.market_id}?o=yes`} className={yesBtn}>
              Yes
            </Link>
            <Link href={`/market/${m.market_id}?o=no`} className={noBtn}>
              No
            </Link>
          </div>
        </div>
      </div>
    );
  }
  return (
    <Link href={`/market/${m.market_id}`} className={cardLink}>
      <span className="flex items-start gap-2.5">
        <FlagPair home={match.home} away={match.away} size={18} />
        <span className="line-clamp-2 min-w-0 text-[13.5px] leading-snug text-ink transition-colors group-hover:text-accent">
          {m.title}
        </span>
      </span>
      <div className="mt-3 flex items-center justify-between gap-2">
        <span className="min-w-0 truncate font-mono text-[10px] uppercase tracking-[0.12em] text-dim">
          {kindOf(m)}
        </span>
        <span className="flex items-center gap-2">
          <StarButton marketId={m.market_id} />
          <MarketState market={m} />
        </span>
      </div>
    </Link>
  );
}

export function GlobalBinaryCard({ m }: { m: Market }) {
  if (m.status === "open") {
    return (
      <div className={cardBox}>
        <Link
          href={`/market/${m.market_id}`}
          className="line-clamp-2 min-w-0 text-[13.5px] leading-snug text-ink transition-colors hover:text-accent"
        >
          {m.title}
        </Link>
        <div className="mt-3 flex items-center justify-between gap-2">
          <span className="min-w-0 truncate font-mono text-[10px] uppercase tracking-[0.12em] text-dim">
            {m.scope ?? kindOf(m)}
            {m.subject_id ? ` · ${m.subject_id}` : ""}
          </span>
          <div className="flex shrink-0 items-center gap-1.5">
            <StarButton marketId={m.market_id} />
            <Link href={`/market/${m.market_id}?o=yes`} className={yesBtn}>
              Yes
            </Link>
            <Link href={`/market/${m.market_id}?o=no`} className={noBtn}>
              No
            </Link>
          </div>
        </div>
      </div>
    );
  }
  return (
    <Link href={`/market/${m.market_id}`} className={cardLink}>
      <span className="line-clamp-2 min-w-0 text-[13.5px] leading-snug text-ink transition-colors group-hover:text-accent">
        {m.title}
      </span>
      <div className="mt-3 flex items-center justify-between gap-2">
        <span className="min-w-0 truncate font-mono text-[10px] uppercase tracking-[0.12em] text-dim">
          {m.scope ?? kindOf(m)}
        </span>
        <span className="flex items-center gap-2">
          <StarButton marketId={m.market_id} />
          <MarketState market={m} />
        </span>
      </div>
    </Link>
  );
}

// PrecisionCard: a pool tile linking to the precision page. Pass `match` for a
// fixture pool (renders team flags); omit it for a global/competition pool.
export function PrecisionCard({ m, match }: { m: Market; match?: Match }) {
  return (
    <Link href={`/precision/${m.market_id}`} className={cardLink}>
      {match ? (
        <span className="flex items-start gap-2.5">
          <FlagPair home={match.home} away={match.away} size={18} />
          <span className="line-clamp-2 min-w-0 text-[13.5px] leading-snug text-ink transition-colors group-hover:text-accent">
            {m.title}
          </span>
        </span>
      ) : (
        <span className="line-clamp-2 min-w-0 text-[13.5px] leading-snug text-ink transition-colors group-hover:text-accent">
          {m.title}
        </span>
      )}
      <div className="mt-3 flex items-center justify-between gap-2">
        <span className="min-w-0 truncate font-mono text-[10px] uppercase tracking-[0.12em] text-dim">
          {match ? kindOf(m) : m.scope ?? kindOf(m)}
        </span>
        <span className="flex items-center gap-2">
          <StarButton marketId={m.market_id} />
          <PrecisionState market={m} />
        </span>
      </div>
    </Link>
  );
}

function MarketState({ market }: { market: Market }) {
  if (market.status === "settled") {
    const w = market.outcome?.winner;
    return (
      <span className={`shrink-0 font-mono text-[11px] ${w === "YES" ? "text-accent" : "text-down"}`}>
        {w}
      </span>
    );
  }
  if (market.status === "void") {
    return <span className="shrink-0 font-mono text-[11px] text-dim">VOID</span>;
  }
  return (
    <span className="shrink-0 font-mono text-[10.5px] uppercase tracking-[0.12em] text-dim">
      {market.status}
    </span>
  );
}

// PrecisionState mirrors MarketState for pools: a settled pool shows its winning
// value, a kickoff-locked pool shows "locked", an open pool shows "pool".
function PrecisionState({ market }: { market: Market }) {
  if (market.status === "settled" && market.outcome?.value != null) {
    return (
      <span className="shrink-0 font-mono text-[11px] text-accent tnum">
        {market.outcome.value}
      </span>
    );
  }
  if (market.status === "void") {
    return <span className="shrink-0 font-mono text-[11px] text-dim">VOID</span>;
  }
  if (market.status === "closed") {
    return (
      <span className="shrink-0 font-mono text-[10.5px] uppercase tracking-[0.12em] text-dim">
        locked
      </span>
    );
  }
  return (
    <span className="shrink-0 font-mono text-[10.5px] uppercase tracking-[0.12em] text-accent/70">
      pool
    </span>
  );
}
