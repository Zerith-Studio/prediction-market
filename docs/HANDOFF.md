# Engineer handoff — state of PitchMarket as of 2026-07-15

Audience: whoever picks this up next (E1 or E2). This is the "everything you need to
continue" document. `progress.md` stays the live status board; this is the deeper tour.
For the pre-demo adversarial test report + residual risks, see
`docs/DEMO-READINESS.md`.

---

## 1. TL;DR — what is DONE and PROVEN

**The never-cut demo floor works end-to-end on devnet.** One match, one binary market,
fully trustless: user-signed order → real matching engine → operator crank → on-chain
settlement with ed25519 verification → resolution → 1:1 redemption.

| Proven | Where |
|---|---|
| Program deployed at pinned ID `3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs` | [deploy tx](https://explorer.solana.com/tx/5Ayf6cLmSpqFue5odVvTVSBQSPMyJjyV6ndhp9FPu6F46CYDSkJucuDyPTpKMQvbpfv4XzC33v4bnfnaj4xXgVqa?cluster=devnet) |
| Go crank settles a real MINT match (v0 tx + lookup table, ed25519 introspection, fill accounting) | [settle tx](https://explorer.solana.com/tx/3zNVPQJqLZhAuRpEmCzGxVfA9aqQe3mm3qT1yFzcN34rrNqM1Eu2oyuagxvdcT51xTjW86ggzjNGhrbvYoKzvdXS?cluster=devnet) |
| resolve_market (tier-a) → redeem pays winners 1:1 from the pool | [resolve](https://explorer.solana.com/tx/5oNcWKQBin6atteQcvAAtEkdivE5q9hXKmYXWeNiKzrXrS7X2VJN2SvSe7pxQ8oCMvjrSjBMr2T9i1uWtVJfXiK8?cluster=devnet) / [redeem](https://explorer.solana.com/tx/4qKCYL4G1VzsPighcWLQ6wgEfYBggHnFCkpHfomXBkWdCfVzrEHv4Ju3dLAwtKHNx62WEyV7Tvi2VqxeRSMWkMku?cluster=devnet) |
| Full E2 backend (matching, Postgres mirror, API/WS, RFQ, precision, MM bot, lifecycle) | HTTP e2e suite vs real Postgres — `backend/internal/api/api_e2e_test.go` |
| Program lifecycle on localnet, all 3 settle paths + cancel fail-closed | `tests/` TS suite, 8/8 (`npm test` vs `solana-test-validator`) |

Rerun the devnet proof anytime: `cd backend && go run ./cmd/devnet-e2e` (needs the
operator keypair + ~0.15 SOL per run; prints explorer links and asserts every balance).

**The single biggest missing piece is the frontend.** Everything it needs is served.

---

## 2. Repo tour (60 seconds)

```
programs/pitchmarket/     Anchor program (Rust) — deployed to devnet
  src/lib.rs              all instructions; SettleMatch accounts are Boxed (BPF stack)
  src/sig_verify.rs       ed25519 sysvar introspection + borsh_order + GOLDEN VECTORS
tests/                    TS localnet suite (mocha); helpers.ts = reference borsh + v0+ALT
backend/
  cmd/server/             the real server — env config in the doc comment + .env.example
  cmd/devnet-e2e/         one-shot devnet floor proof (this is your integration debugger)
  internal/exchange/      THE trading core — all order flow goes through here
  internal/matching/      in-memory book: NORMAL/MINT/MERGE, price-time priority
  internal/store/         Postgres mirror: soft-locks, fills, balances, combos, precision
  internal/crank/         tx builders (settle v0+ALT, market/vault/resolve/redeem), LUT
                          manager, RPC submitter, revert hooks
  internal/api/           REST + /ws surface (frontend contract — see §5)
  internal/{rfq,mmbot,lifecycle,feed,oneliner,index,ws,templates,models}
docs/interface-contract.md   the frozen E1↔E2 boundary (§6.5 = settle tx layout, v0+ALT)
progress.md               live status board — UPDATE IT IN THE SAME COMMIT AS CODE
```

---

## 3. Invariants that must not break

1. **Borsh order encoding is triplicated** (Go `models.BorshOrder`, Rust
   `sig_verify::borsh_order`, TS `tests/helpers.ts borshOrder`). Golden vectors pin
   Go↔Rust (`hash_conformance_test.go` ↔ `sig_verify.rs` tests); TS↔Rust was proven at
   runtime on devnet. Touch one encoder → update the same vector in both test suites.
2. **settle_match tx layout is pinned** (interface-contract §6.5): exactly
   `[ed25519(taker), ed25519(maker), settle_match]`, compiled as a **v0 tx with a
   per-market lookup table** (legacy is 1421+ B > 1232 limit). `crank.TxBuilder` is the
   only producer; `builder_test.go` re-implements the on-chain byte checks.
3. **MINT/MERGE money moves at each order's OWN limit price** on-chain; only NORMAL uses
   `fill_price`. `store.legDeltaFor` mirrors this exactly — keep them in lockstep.
4. **All order flow goes through `internal/exchange`** — API and MM bot alike. Anything
   else skips sig verification, soft-locks, the crank, or WS events.
5. **Chain is authoritative for money.** Postgres is a mirror + soft-lock store; on a
   settle revert the crank hooks unwind everything (proven in
   `TestRevertReconcilesEverywhere`).

---

## 4. Runbooks

### Tests
```sh
cd backend && go test -p 1 ./...     # -p 1: parallel packages contend on Neon and flake
cargo test -p pitchmarket            # host tests incl. golden vectors
npm test                             # TS localnet suite (needs solana-test-validator + IDL, see §6 gotchas)
```
DATABASE_URL comes from the repo-root `.env` (copy `.env.example`). DB tests create and
drop a scratch database per run. A `dial error`/`no such host` failure is Neon network
flake — re-run the package.

### Demo server (off-chain mirror mode — no chain needed)
```sh
cd backend && DEMO_FIXTURE=demo-final go run ./cmd/server
```
Auto-creates 7 template markets for the fixture, seeds precision pools with 25 personas,
streams the recorded match (`backend/fixtures/demo-final.json`), MM bot quotes both
sides, resolution fires at full time. REST+WS on :8080.

### On-chain mode (against the deployed devnet program)
```sh
SOLANA_RPC_URL=https://api.devnet.solana.com \
OPERATOR_KEYPAIR=$HOME/.config/solana/id.json \
DEMO_FIXTURE=demo-final go run ./cmd/server
```
This flips the crank to real v0+ALT settlement and starts the chain index poller.
**Known gap:** market creation & resolution are not yet wired on-chain from the server —
see punch list #2. The devnet-e2e harness is the working reference for those calls.

### Devnet floor proof
```sh
cd backend && go run ./cmd/devnet-e2e   # ~2 min; prints explorer links
```

---

## 5. Punch list — what remains, in priority order

### #1 Frontend (unstarted; the demo needs it)
Next.js + Privy per PROJECT_PLAN §6. The backend contract:
- REST: `POST /orders`, `DELETE /orders/{hash}?maker=`, `GET /matches`, `GET /markets[?status=]`,
  `GET /markets/{id}`, `/book`, `/fills`, `/settlement`, `/oneliners`,
  `POST /combos`, `GET /combos/{id}`, `POST /combos/{id}/quotes`, `POST /combos/{id}/accept`,
  `POST /markets/{id}/precision`, `GET /markets/{id}/precision/leaderboard`,
  `POST /wallet/deposit`, `GET /balance?wallet=`, `GET /portfolio?wallet=`.
- WS `/ws`: `book_update`, `fill`, `order_update`, `combo_quote`, `match_state`, `oneliner`
  (broadcast-all; filter client-side by `market_id`/`fixture_id`).
- Wire format: pubkeys base58, hashes/market ids 64-hex, sigs 128-hex, money integer
  micro-USDC, prices integer cents 1..99. Client signs `borshOrder` bytes ed25519
  (`tests/helpers.ts` has the reference TS encoder — lift it as-is).
- Error semantics: 401 bad sig, 402 insufficient funds, 409 replay/double-accept,
  410 precision after kickoff.

### #2 Wire on-chain market lifecycle into the server (E2, ~half day)
The seams exist; the harness proves the calls work:
- `lifecycle.ChainResolver` (interface in `lifecycle.go`) — implement with
  `crank.TxBuilder.ResolveMarketIx` + the send/confirm pattern from `crank/rpc.go`;
  wire in `cmd/server/main.go` when `SOLANA_RPC_URL` is set (currently Noop).
- On-chain `initialize_market` when `lifecycle.RegisterFixture` creates markets —
  builder exists (`crank.TxBuilder.InitializeMarketIx`), call it per market in on-chain mode.
- Vault outcome-ATA creation before first settle per (user, market) — see
  `devnet-e2e ensureVaultOutcomeATAs`; the crank could create them lazily instead.

### #3 combo_accept / resolve_combo (E1) + crank submitter (E2)
Program instructions are typed stubs in `lib.rs` (accounts sketched in the contexts).
ADR 0004 has the full spec. On the Go side implement `rfq.ComboSubmitter` (interface in
`rfq.go`, currently Noop = combos settle off-chain, which is the sanctioned cut position).

### #4 Oracle tier d — TxODDS-signed resolution (E1, gated on TxODDS reply)
`resolve_market` currently tier-a only. Feed plumbing already carries
`SignedProof` bytes per event (`feed.MatchEvent`); decision #3 (was the TxODDS email
sent?) is STILL unconfirmed — chase it, the trust headline depends on it.

### #5 Demo & submission
Record the ~3-min demo (PROJECT_PLAN §8 script), swap `REPLAY_SPEED` to taste,
`ANTHROPIC_API_KEY` enables the one-liner ticker. Judged by 2026-07-29.

---

## 6. Operational notes & gotchas

- **Operator wallet** `2rRndZBMURYnyZNY4b7Kvsmugn77dXchTmrzMub6A2fQ` lives at
  `~/.config/solana/id.json` on Ashish's machine ONLY. It is the program's **upgrade
  authority**, the tier-a resolver for e2e markets, and holds the devnet SOL. To let the
  other engineer redeploy/resolve: share that file out of band, or
  `solana program set-upgrade-authority`. (Program *keypair* is committed — identity
  only; the operator key is deliberately not.)
- **Toolchain trap:** any `anchor` CLI invocation silently re-installs old
  platform-tools and re-breaks `cargo build-sbf`. Build with
  `cd programs/pitchmarket && cargo build-sbf` (needs Agave ≥ 4.x / platform-tools
  ≥ v1.54; see progress.md §4 for the repoint incantation after using anchor).
- **IDL generation** chokes on the hash-derived `ostatus` seeds — progress.md §4 has
  the temporary-seed-swap workaround (only needed for the TS client).
- **Devnet RPC is rate-limited:** poll gently (the crank/harness use 1.5–2.5s
  intervals). A "not confirmed in time" often LANDED — `solana confirm <sig>` first.
- **Neon DATABASE_URL** is in the gitignored `.env` and was shared in chat — rotate it
  after the hackathon.
- The CLI devnet **faucet** rate-limits per IP; use faucet.solana.com when the operator
  runs dry (deploys cost ~3 SOL; e2e runs ~0.15 SOL).
