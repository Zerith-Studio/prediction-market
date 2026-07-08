# Day-1 email to TxODDS — the highest-leverage hour in the build

Send via the Telegram/contact on the Superteam Earn listing. Two asks: **feed format** (needed
regardless) and **signed data** (gates the whole oracle architecture — ADR 0005 (d) vs (b)).

---

**Subject: Feed format + signed-data question for our Prediction Markets & Settlement build**

Hi — we're building on the Prediction Markets & Settlement (World Cup) track and want to wire
against TxODDS/TxLINE properly. Two questions:

**1. Feed format & coverage.** For live World Cup matches, what's the delivery + shape of TxLINE?
Specifically:
- Protocol: HTTP/JSON, WebSocket, FIX, other?
- A sample fixtures payload + a sample live-state payload + a sample final-result payload.
- Stat granularity: do you provide **per-player** stats (goals, shots on target, assists) and
  **per-team** stats (shots, corners, cards, fouls, possession %) live and at full-time?
- Do you emit an explicit **`final` / settled flag** per match/stat, and do stats get **revised**
  post-full-time (and if so, typical revision latency)?

**2. Signed / attested data — the important one.** Do you offer **cryptographically signed or
attested** data? Concretely:
- A **publishable signing key**, and a **signature over each payload** (ideally over the
  final-flagged result)?
- Ed25519 would be ideal — it lets us **verify your signature on-chain (Solana)** and make TxODDS
  the cryptographic root of settlement trust, so our operator can only *relay* your signed data,
  never forge an outcome. For a "Settlement" track that's the strongest possible trust story, and
  it puts you at the root of it.

If signed feeds aren't available, no problem — we'll fall back to a bonded challenge-window
resolver. But if they are, we'd build directly against them. Thanks!

---

**Why this gates the architecture (internal note):**
- **Yes, signed** → build ADR 0005 tier **(d)**: `resolve` ix does on-chain ed25519 verify of the
  TxODDS signature; operator = pure relay. Plus the finality rule (signed `final=true` + T+X delay).
- **No** → build tier **(b)**: commit-reveal + challenge window. ~1 extra day.
- Either way, tier **(a)** single-key resolver ships first as the floor so the demo is never dead.
