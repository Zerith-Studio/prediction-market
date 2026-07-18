# Watchlist — design

**Date:** 2026-07-18
**Scope:** Frontend (Next.js) + backend (Go/Postgres). Web only — no mobile.

## Goal

Let a user favourite ("watch") any market. Watched markets appear in two places for a
unified experience:

1. **Portfolio page** — a horizontal, scrollable strip of watchlist cards directly under
   the Vault/P&L summary section.
2. **Search** — a "Watchlist" group at the top of the `CommandPalette`, above Markets and
   Precision.

The favourite toggle (a star) lives on the home-page market cards and on the market detail
page header.

## Persistence: backend + Postgres

Watchlist is per-wallet and syncs across devices. It is UI/preference state, not money —
the chain stays authoritative for everything financial (interface-contract §6.2). This
adds a new table and REST surface.

### Table (`backend/db/schema.sql`, appended; idempotent)

```sql
CREATE TABLE IF NOT EXISTS watchlists (
    wallet     TEXT  NOT NULL,
    market_id  BYTEA NOT NULL REFERENCES markets(market_id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (wallet, market_id)
);
```

`created_at DESC` gives most-recently-watched-first ordering. The FK to `markets` prevents
orphan rows and means an unknown `market_id` is rejected at insert.

### Store (`backend/internal/store/watchlist.go`, new)

- `AddWatch(ctx, wallet string, marketID [32]byte) error` — `INSERT … ON CONFLICT
  (wallet, market_id) DO NOTHING`. Surfaces FK violation on unknown market as a typed
  not-found error.
- `RemoveWatch(ctx, wallet string, marketID [32]byte) error` — `DELETE`.
- `Watchlist(ctx, wallet string) ([][32]byte, error)` — `SELECT market_id … ORDER BY
  created_at DESC`.

Uses simple query protocol conventions already in the store (explicit param handling,
`marketID[:]` bytea).

### Endpoints (`backend/internal/api/api.go`)

Mirror the wallet-keyed `/balance` handler and the RESTful `DELETE /orders/{hash}` route.

| Method + path                     | Body / query                     | Response               |
|-----------------------------------|----------------------------------|------------------------|
| `GET /watchlist?wallet=…`         | wallet query param               | `{ "market_ids": [] }` |
| `POST /watchlist`                 | `{ wallet, market_id }`          | `200` (idempotent)     |
| `DELETE /watchlist/{market_id}?wallet=…` | market_id path, wallet query | `200`                  |

- `wallet` validated with `models.ParsePubkey` → 400 on bad input.
- `market_id` parsed from 64-hex → `[32]byte`; unknown market (FK) → 400.
- `market_ids` in the GET response are 64-hex strings (via `models.HashString`).

### Interface contract

Add a short subsection to `docs/interface-contract.md` documenting the three routes and the
`market_ids` shape, consistent with the existing REST surface documentation.

## Frontend

### Data layer (`frontend/lib/api.ts`)

- `getWatchlist(wallet: string | null): Promise<string[]>` — returns `[]` when no wallet;
  maps `{ market_ids }`.
- `addWatch(wallet, marketId): Promise<void>` — `POST /watchlist`.
- `removeWatch(wallet, marketId): Promise<void>` — `DELETE /watchlist/{id}?wallet=…`.
  (Add a `del` helper alongside `get`/`post`, or inline a `fetch` with `method: "DELETE"`.)

### State: `WatchlistProvider` context

New provider wired into `frontend/components/Providers.tsx`, sibling to the wallet context.

- Holds `Set<string>` of watched `market_id`s; loads once via `getWatchlist` when
  `wallet.address` becomes available; clears on disconnect.
- Exposes `isWatched(id): boolean` and `toggle(id): void`.
- `toggle` is **optimistic**: mutate the set immediately, fire `addWatch`/`removeWatch` in
  the background, and revert the set on API error.
- One source of truth — star buttons, the search group, and the portfolio strip all read
  the same set, so a toggle anywhere updates everywhere with no refetch.

### `StarButton` component (`frontend/components/StarButton.tsx`, new)

- Props: `marketId: string` (and optional `className` / size).
- Outline star when not watched; filled accent star when watched. Uses `useWatchlist()`.
- **Hidden when no wallet is connected** (chosen over disabled-with-tooltip).
- `onClick` stops propagation (cards are links) and calls `toggle`.

### Surfaces

- **Home page (`frontend/app/page.tsx`)** — `StarButton` in the top-right of `BinaryCard`
  and the precision card. Placed so it doesn't collide with the Yes/No buttons or status
  labels.
- **Market detail (`frontend/app/market/[id]/page.tsx`)** — `StarButton` in the price
  header near the title.
- **Search (`frontend/components/CommandPalette.tsx`)** — a new "Watchlist" `Group`
  rendered above "Markets" and "Precision". It filters the watched markets by the current
  query (same `hit()` predicate) and is omitted when empty. Keyboard nav index ordering
  updated so the watchlist rows come first.
- **Portfolio (`frontend/app/portfolio/page.tsx`)** — a horizontal scroll-snap strip of
  compact watchlist cards directly under the Vault/P&L summary `<section>`. Resolves
  titles/status from `listMarkets()` (already fetched elsewhere; fetch here too). Links to
  `/market/[id]` or `/precision/[id]` by market type. Section hidden when the watchlist is
  empty.

## Edge cases

- **No wallet:** star hidden; portfolio page already gates on wallet connect.
- **Empty watchlist:** portfolio strip and search group are both omitted (no empty-state
  clutter).
- **Settled/void watched market:** still rendered, with its status label.
- **Stale watch (market no longer in `listMarkets()`):** skipped when rendering the strip
  and search group; the row stays in the DB harmlessly.
- **Optimistic toggle failure:** set reverts; a non-blocking console warn is acceptable for
  the hackathon scope (no toast system exists).

## Out of scope

- Mobile app.
- Reordering / manual sorting of the watchlist (fixed most-recent-first).
- Notifications or price alerts on watched markets.

## Verification

- Backend: `go build ./... && go vet ./...`; `go test ./internal/store/` for the new store
  methods (scratch DB, per CLAUDE.md).
- Frontend: `npm run build` (or `next lint`) in `frontend/`; manual smoke — favourite a
  market from a card, confirm it appears in the search group and the portfolio strip, and
  survives a reload (cross-device persistence).
- Update `progress.md`: component table + changelog row, per the one-rule.
