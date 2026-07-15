# Design

Visual system for the PitchMarket frontend, as built in `frontend/`. Direction:
**flat, sharp, box-less minimal** — a premium near-monochrome trading surface where
structure comes from hairlines, whitespace, and typography, not cards.

> This replaced an earlier "broadcast/matchday" panel-heavy direction (git history has it)
> which read as templated. The rule now: **no boxes.** If you reach for a card, use a
> hairline and space instead.

## Theme

**Committed dark, single theme (no light mode).** One flat near-black surface —
no gradients, no textures, no panels. Depth and grouping are carried entirely by
hairline rules (`border-line`) and generous vertical space.

Color strategy: **near-monochrome + one accent.** Off-white text on flat black, with a
single emerald accent that only ever means *up / YES / positive / on-chain*. A quiet rose
means *down / NO*, used sparingly. Everything else is greyscale. This restraint is the
whole premium: bright color is a signal, never decoration.

## Color

Tokens (`tailwind.config.ts` → `theme.extend.colors`):

| Role | Token | Hex | Use |
|---|---|---|---|
| Base | `bg` | `#0a0a0b` | the single flat surface — no elevated panels |
| Ink | `ink` | `#f4f5f7` | primary text |
| Muted | `muted` | `#9297a0` | secondary text (AA) |
| Dim | `dim` | `#565b63` | labels, eyebrows, tertiary, chart guides |
| Hairline | `line` | `#1b1c20` | separators — the primary structural device |
| Hairline+ | `line2` | `#292b30` | input underlines, stronger dividers |
| **Accent** | `accent` | `#34d399` | up / YES / bid / positive / on-chain / primary action — the ONE accent |
| **Down** | `down` | `#f2637e` | down / NO / ask — used sparingly |

Radius is **0** by default (sharp); `sm` = 1px, `md` = 2px exist but are rarely used.
No shadows, no glow (except the chart marker's faint halo). Bans honored: no gradient
text, no glassmorphism (the only blur is the sticky nav), no side-stripe borders, no cards.

## Typography

Inter (sans) + JetBrains Mono. **One idea: numbers and system chrome are mono; prose is
sans.**

- **Display price** — big and *thin* (`font-light`, ~46–64px mono). Weight, not size, reads
  premium here. The current YES price is the page's typographic anchor.
- **Headings** — sans, `font-semibold`, 13–15px. Small and quiet; the data is the hero.
- **Eyebrows** (`.eyebrow`) — mono, 10.5px, uppercase, `tracking-[0.18em]`, `dim`. Used for
  column headers and section tags where a terminal earns them — not on every section.
- **All numerics** — mono + `.tnum` (tabular). Prices, sizes, balances, hashes, clocks.
- Fixed rem scale (no fluid clamp in app UI); prose capped ~65–75ch.

## Motion

State, not decoration; 150–300ms ease-out. Reserved moments:

1. **Live pulse** — the LIVE dot (rose, 1.8s loop).
2. **Trade flash** — a book row / trade briefly tints accent/down when it lands, then decays.
3. **Price chart** — updates as trades stream; **hover shows a crosshair + price/time readout**.
4. **AI ticker** — one-liner crossfades (`AnimatePresence initial={false}`, so it never
   ships blank — content is never gated on the animation).

Library: Framer Motion. Full `prefers-reduced-motion` fallbacks (globals zeroes durations).
No page-load choreography.

## Components (`frontend/components/`)

Box-less by construction — sections are separated by `rule-t` / `rule-b` / `rule-l`.

- **PriceChart** — SVG line chart of the YES token value; dashed baseline at the window's
  open, hi/lo guides, live marker, hover crosshair. `preserveAspectRatio="none"` +
  `vector-effect="non-scaling-stroke"` keeps strokes crisp at any width.
- **MatchHero** — airy match line: team names + score, national-color **dots** (not crests).
- **OrderBook** — box-less rows, subtle depth tint (accent/down at ~7% opacity), MID/SPREAD rule.
- **RecentFills** — minimal trades list.
- **TradePanel** — underline inputs (border-bottom only), segmented Buy/Sell via underline,
  one sharp solid accent button. Full state coverage (error, disabled, loading, locked).
- **VerifySeal / settlement** — the "Verified on Solana ↗" trust moment; first-class, real
  devnet explorer links.
- **PitchTicker**, **TopBar** (mono wordmark, text nav), **Skeletons** (box-less bars).

States: skeleton loading (bars, no spinners), empty states that teach, `focus-visible`
accent outline on every interactive element.

## Layout

`max-w-[1200px]`, generous `px`. Structural responsiveness (not fluid type):
market page is `lg:grid-cols-[1fr_300px]` (book+trades | sticky trade panel, split by a
vertical hairline) collapsing to a single column; hero/score grid uses `min-w-0` + truncate
so nothing overflows (verified at true 390px). Semantic z-index scale.

## Stack

Next.js 14 (App Router) on Vercel · Privy embedded wallet (ed25519 order signing, encoder
lifted from `tests/helpers.ts`) · Tailwind · Framer Motion · lucide icons · typed
`apiClient` + `useLiveMarket` hook against the Go REST/WS surface. Fixture-driven until
`NEXT_PUBLIC_API_URL` is set (backend REST mux then needs CORS for the origin).
