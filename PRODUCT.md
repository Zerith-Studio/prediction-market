# Product

## Register

product

## Users

Football fans and prediction-market traders on Solana, using the app on matchday —
often live, during a game, on desktop or mobile. Two overlapping jobs:

- **Trade an opinion into a position.** Sign a limit order on a match/goal market, see
  it rest or fill on a real order book, manage exposure, redeem when the match resolves.
- **Play a match, not just a market.** Enter a precision pool (guess the number), build a
  combo/parlay via RFQ, and follow the live one-liner commentary — lighter-touch formats
  that pull fans into the exchange.

Context: the user is mid-match, semi-distracted, and skeptical of crypto UIs. The interface
must make a signed non-custodial order feel as fast and safe as tapping a betting slip,
and make "your money is settled on Solana, not held by us" legible without a lecture.

## Product Purpose

PitchMarket is Polymarket's CTF + CLOB faithfully rebuilt on Solana, plus two net-new
formats (precision pools, RFQ combos). Users sign orders off-chain (silent, gasless); an
operator crank settles each match **on-chain, non-custodially** — it can't forge or
over-fill. TxODDS/TxLINE drives market creation, live pricing, and — the headline — is the
signed on-chain root of settlement trust. Success = a fan who has never touched a wallet
places a trade, watches it settle on-chain, and clicks "Verified on Solana ↗" believing it.

## Brand Personality

**Pro exchange with a matchday soul.** Three words: **credible, kinetic, trustworthy.**
It is a serious trading terminal first — precise, dense where it needs to be, calm under
the hand of someone moving real money — with football-broadcast energy spent deliberately
as *moments*, not wallpaper. Voice: confident and plain. Never hype, never "🚀 to the
moon," never a casino barker. The trust story is stated once, structurally, and then the
tool disappears into the task.

## Anti-references

Confirmed by the team — avoid all four:

- **Generic dark SaaS dashboard.** The interchangeable slate-gray crypto dashboard with a
  lone accent color. The default every hackathon frontend reaches for; the thing to beat.
- **Garish casino / sportsbook.** Flashing neon, coin animations, gold-and-red "BET NOW."
  Cheapens the non-custodial trust story we're selling.
- **Polymarket clone.** Faithful mechanics, distinct look. No copying its blue-card layout.
- **Crypto-degen neon.** Purple/cyan gradients-on-black, glassmorphism everywhere, degen vibe.

## Design Principles

1. **Product discipline, broadcast moments.** Trading surfaces (order book, trade panel,
   portfolio) obey product-UI rigor: earned familiarity, full component states, motion that
   conveys state in 150–250ms. Broadcast delight is a *budget* spent on four moments only:
   the live match hero, the fill flash, the settlement seal, the one-liner ticker.
2. **Trust is shown, not claimed.** "Non-custodial," "gasless," "settled on Solana" are
   proven in-context — a real explorer link, a signed-order affordance — not badges. The
   settlement "Verified on Solana ↗" is the product's closing argument; treat it as a
   first-class screen, not a footnote.
3. **The match is the anchor.** Every market lives under a live scoreline and clock. This
   is what makes it football, not a generic exchange, and it's the single biggest thing
   separating us from the four anti-references.
4. **Fast, safe, silent.** Signing an order must feel like tapping a betting slip: one
   action, instant feedback, no gas anxiety, no wallet jargon. Latency is a design surface.
5. **Money UI honesty.** YES/NO, prices, sizes, and balances never mislead. YES/NO carry
   text labels and position (not color alone); numbers use one monospace treatment; every
   destructive or irreversible action states its consequence before it happens.

## Accessibility & Inclusion

WCAG 2.1 AA. Body text ≥4.5:1, large text ≥3:1, focus-visible on every interactive
element, full keyboard paths for trading. Reduced-motion fallbacks for all four broadcast
moments (crossfade/instant, never gated content). YES/NO encoded with label + position in
addition to green/red as a matter of money-UI honesty (Principle 5), even though full
colorblind-safe encoding was not a hard requirement.
