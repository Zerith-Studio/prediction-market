# Design

Seed visual system for the PitchMarket frontend. Direction: **Broadcast / matchday** —
a committed dark trading terminal with football-broadcast moments. Re-run
`/impeccable document` once real tokens exist in code to capture the built system.

## Theme

**Committed dark, single theme (no light mode).** Scene sentence that forces it: *a fan
on the sofa at night, phone in hand, a World Cup match live on the TV in front of them* —
matchday is an evening, big-screen, dark-room experience, and the order book / odds read
best as light-on-dark. Light mode is not built; broadcast energy dies in it.

Color strategy: **Restrained + semantic.** Tinted-neutral surfaces carry the workbench;
saturated color is reserved for *meaning* (YES/NO, live, verified), never decoration. This
is what keeps us out of "crypto-degen neon" — the bright colors are a vocabulary, not a mood.

## Color

OKLCH values are the source of truth; hex is the fallback shipped in the mockup.

| Role | Token | Hex | Use |
|---|---|---|---|
| Base bg | `--bg` | `#07090d` | app background (near-black, faintly cool) |
| Surface | `--panel` | `#11161f` | panels, cards, book |
| Surface 2 | `--panel-2` | `#141b26` | nav, toolbars, insets (cooler second layer) |
| Hairline | `--line` | `#1e2735` | borders, dividers |
| Ink | `--txt` | `#eef2f8` | primary text (≥4.5:1 on all surfaces) |
| Muted | `--muted` | `#8b98ad` | secondary text — verified AA, not lighter |
| Dim | `--dim` | `#5c6a7e` | tertiary / meta labels only (large/non-body) |
| **YES** | `--yes` | `#22e08a` | buy/yes/bid/up — **always paired with a "YES" label + position, never color alone** |
| **NO** | `--no` | `#ff4d6a` | sell/no/ask/down — same rule |
| Live / AI | `--accent` | `#ffd21e` | live tag, match clock, one-liner AI voice |
| **Verified** | `--verify` | `#00e0ff` | on-chain / settled / "Verified on Solana" ONLY — this cyan is a trust semantic, never a decorative accent |

Semantic states (standardize across every interactive element): hover, focus-visible,
active, disabled, selected, loading, error, warning, success. Accent/verify colors are
full-saturation only on active states; inactive states desaturate toward the neutral ramp.

**Bans honored:** no gradient text, no glassmorphism beyond the single sticky nav blur, no
side-stripe borders, no gradients-on-black hero. Depth comes from the hairline + surface
layering, not glow.

## Typography

One workhorse sans + one monospace. No display/body pairing (product register).

- **Sans:** Inter (or system-ui fallback). Headings, labels, buttons, body. Weights 500/700/800.
- **Mono:** JetBrains Mono / SF Mono / ui-monospace. **All numerics** — prices, sizes,
  balances, odds, order hashes, tx sigs, clocks. One monospace treatment everywhere numbers
  appear (Principle 5: money-UI honesty).
- **Scale:** fixed rem, ratio ~1.2. No fluid clamp() in the app UI (product register). The
  match-hero scoreline is the one permitted oversized element (~40px), a broadcast moment.
- Letter-spacing ≥ -0.02em on the few large elements; prose capped 65–75ch; tables may run denser.

## Motion

Motion conveys **state**, not decoration (product register: 150–250ms on most transitions).
Broadcast energy is a strict four-moment budget:

1. **Live pulse** — the LIVE dot on the match hero (2s loop). Ambient, low-cost.
2. **Fill flash** — an order-book row flashes YES/NO tint for ~0.9s when a fill lands, then
   settles. This is the single most important animation; it makes the book feel alive.
3. **Odds tick** — the YES/NO odds meter animates width + a ▲/▼ delta when price moves.
4. **Settlement seal** — the "Verified on Solana" card reveals with a cyan glow-in on resolve.

Everything else (nav, panels, forms, tabs) uses plain 150–250ms ease-out (quart/expo, no
bounce). Library: Framer Motion. **Every one of the four moments has a
`prefers-reduced-motion` fallback** (crossfade or instant; content is never gated on the
animation). No orchestrated page-load sequence — the app loads into a task.

## Components

Base: **shadcn/ui + Tailwind**, restyled to these tokens (don't ship default shadcn slate).
Every interactive component ships all states: default, hover, focus-visible, active,
disabled, loading (skeleton, not center-spinner), error.

Signature components (the ones worth crafting first):
- **MatchHero** — live tag, crests in national colors, scoreline, clock. Anchors every market page.
- **OrderBook** — depth-shaded rows (YES bid / NO ask), spread marker, fill-flash. The pro layer.
- **OddsMeter** — the green/red YES-NO split bar; the instant read above the book.
- **TradePanel** — buy/sell tabs, limit price + size, cost/payout, "Sign & Buy" with the
  "gasless · settled by crank" trust line.
- **VerifySeal** — the settlement "Verified on Solana ↗" card. First-class, not a footnote.
- **PitchTicker** — the Claude one-liner ticker (amber AI voice).

Empty states teach (an unopened book explains how signed orders rest); they never say
"nothing here." Modals are a last resort — prefer inline/progressive (product register).

## Layout

Responsive is structural, not fluid type: the market page is a two-column
book + trade-panel grid on desktop that stacks on mobile; nav collapses; tables become
scroll containers. Format landing uses `repeat(auto-fit, minmax(280px,1fr))`. Semantic
z-index scale (dropdown → sticky nav → modal-backdrop → modal → toast → tooltip).

## Stack

Next.js 14 (App Router) on Vercel · Privy embedded wallet (ed25519 order signing, encoder
lifted from `tests/helpers.ts`) · Tailwind + shadcn/ui · Framer Motion · typed `apiClient`
+ single `useSocket` hook against the Go REST/WS surface. Requires CORS added to the Go
REST mux for the Vercel/localhost origin.
