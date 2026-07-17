# Watchlist Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user favourite ("watch") any market; watched markets appear as a horizontal strip on the portfolio page and as a top group in the search palette.

**Architecture:** Per-wallet watchlist persisted in Postgres via three REST routes. The Next.js frontend loads the watchlist once into a shared `WatchlistProvider` React context that all surfaces (star buttons, search, portfolio strip) read from, with optimistic toggles.

**Tech Stack:** Go (net/http, pgx v5, Postgres/Neon), Next.js App Router, React context, Tailwind.

## Global Constraints

- **pgx simple query protocol** — pass `[32]byte` as `marketID[:]` (bytea); no prepared-statement-only features.
- **Wallet is base58**, market_id is 64-hex. Validate wallet with `models.ParsePubkey`, parse market_id with `models.ParseHash`, render hashes with `models.HashString`.
- **Chain is authoritative for money** — the watchlist is UI preference state only; never gate financial logic on it.
- **Commit messages:** `type(scope): summary`, imperative, no `Co-Authored-By` / tool-attribution trailers.
- **progress.md rule:** the final task updates `progress.md` (component table + changelog row) in the same commit.
- Go tests run with `go test -p 1 ./internal/store/` and need `DATABASE_URL` (auto-read from repo-root `.env`).

## File Structure

**Backend**
- `backend/db/schema.sql` — append `watchlists` table (idempotent).
- `backend/internal/store/watchlist.go` (new) — `AddWatch`, `RemoveWatch`, `Watchlist`, plus `isForeignKeyViolation` helper.
- `backend/internal/store/watchlist_test.go` (new) — store round-trip tests.
- `backend/internal/api/api.go` — three routes + handlers.
- `docs/interface-contract.md` — document the three routes.

**Frontend**
- `frontend/lib/api.ts` — `getWatchlist`, `addWatch`, `removeWatch` + a `del` helper.
- `frontend/lib/watchlist.tsx` (new) — `WatchlistProvider` + `useWatchlist` hook.
- `frontend/components/Providers.tsx` — wrap children in `WatchlistProvider`.
- `frontend/components/StarButton.tsx` (new) — the toggle.
- `frontend/app/page.tsx` — star on home cards.
- `frontend/app/market/[id]/page.tsx` — star in price header.
- `frontend/components/CommandPalette.tsx` — "Watchlist" group on top.
- `frontend/app/portfolio/page.tsx` — horizontal watchlist strip under the summary.

---

### Task 1: Backend store — watchlist table + methods

**Files:**
- Modify: `backend/db/schema.sql` (append at end)
- Create: `backend/internal/store/watchlist.go`
- Test: `backend/internal/store/watchlist_test.go`

**Interfaces:**
- Consumes: existing `storetest.Open`, `seedMarket`, `wallet` test helpers; `store.ErrNotFound`.
- Produces:
  - `func (s *Store) AddWatch(ctx context.Context, wallet string, marketID [32]byte) error`
  - `func (s *Store) RemoveWatch(ctx context.Context, wallet string, marketID [32]byte) error`
  - `func (s *Store) Watchlist(ctx context.Context, wallet string) ([][32]byte, error)`

- [ ] **Step 1: Append the table to `backend/db/schema.sql`**

Add at the end of the file (after the `oneliners` table / migrations block):

```sql
-- Per-wallet market watchlist (UI preference; chain stays authoritative for money).
CREATE TABLE IF NOT EXISTS watchlists (
    wallet     TEXT  NOT NULL,
    market_id  BYTEA NOT NULL REFERENCES markets(market_id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (wallet, market_id)
);
```

- [ ] **Step 2: Write the failing test** — create `backend/internal/store/watchlist_test.go`

```go
package store_test

import (
	"testing"

	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store/storetest"
)

func TestWatchlistRoundTrip(t *testing.T) {
	s := storetest.Open(t)
	_, w := wallet(1)

	var m1, m2 [32]byte
	m1[0], m1[31] = 0xA1, 0x01
	m2[0], m2[31] = 0xB2, 0x02
	seedMarket(t, s, m1)
	seedMarket(t, s, m2)

	// Empty to start.
	got, err := s.Watchlist(ctx, w)
	if err != nil {
		t.Fatalf("Watchlist: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("fresh watchlist = %d entries, want 0", len(got))
	}

	// Add two; most-recent-first ordering means m2 comes before m1.
	if err := s.AddWatch(ctx, w, m1); err != nil {
		t.Fatalf("AddWatch m1: %v", err)
	}
	if err := s.AddWatch(ctx, w, m2); err != nil {
		t.Fatalf("AddWatch m2: %v", err)
	}
	// Adding a duplicate is a no-op, not an error.
	if err := s.AddWatch(ctx, w, m1); err != nil {
		t.Fatalf("AddWatch duplicate: %v", err)
	}

	got, err = s.Watchlist(ctx, w)
	if err != nil {
		t.Fatalf("Watchlist: %v", err)
	}
	if len(got) != 2 || got[0] != m2 || got[1] != m1 {
		t.Fatalf("watchlist = %v, want [m2 m1]", got)
	}

	// Remove one.
	if err := s.RemoveWatch(ctx, w, m2); err != nil {
		t.Fatalf("RemoveWatch: %v", err)
	}
	got, _ = s.Watchlist(ctx, w)
	if len(got) != 1 || got[0] != m1 {
		t.Fatalf("after remove = %v, want [m1]", got)
	}
}

func TestWatchUnknownMarket(t *testing.T) {
	s := storetest.Open(t)
	_, w := wallet(2)
	var unknown [32]byte
	unknown[0], unknown[31] = 0xEE, 0xEE
	if err := s.AddWatch(ctx, w, unknown); err != store.ErrNotFound {
		t.Fatalf("AddWatch unknown market = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd backend && go test -p 1 ./internal/store/ -run TestWatch`
Expected: FAIL — `s.Watchlist`, `s.AddWatch`, `s.RemoveWatch` undefined (build error).

- [ ] **Step 4: Implement `backend/internal/store/watchlist.go`**

```go
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// AddWatch favourites a market for a wallet. Idempotent (ON CONFLICT DO NOTHING);
// an unknown market_id trips the FK and surfaces as ErrNotFound.
func (s *Store) AddWatch(ctx context.Context, wallet string, marketID [32]byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO watchlists (wallet, market_id) VALUES ($1, $2)
		ON CONFLICT (wallet, market_id) DO NOTHING`, wallet, marketID[:])
	if isForeignKeyViolation(err) {
		return ErrNotFound
	}
	return err
}

// RemoveWatch unfavourites a market. Removing an absent entry is a no-op.
func (s *Store) RemoveWatch(ctx context.Context, wallet string, marketID [32]byte) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM watchlists WHERE wallet = $1 AND market_id = $2`, wallet, marketID[:])
	return err
}

// Watchlist returns a wallet's watched market ids, most-recently-added first.
func (s *Store) Watchlist(ctx context.Context, wallet string) ([][32]byte, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT market_id FROM watchlists WHERE wallet = $1
		ORDER BY created_at DESC`, wallet)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][32]byte
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var id [32]byte
		copy(id[:], b)
		out = append(out, id)
	}
	return out, rows.Err()
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd backend && go test -p 1 ./internal/store/ -run TestWatch`
Expected: PASS (both `TestWatchlistRoundTrip` and `TestWatchUnknownMarket`).

- [ ] **Step 6: Vet + commit**

```bash
cd backend && go build ./... && go vet ./...
cd .. && git add backend/db/schema.sql backend/internal/store/watchlist.go backend/internal/store/watchlist_test.go
git commit -m "feat(store): per-wallet watchlist table and methods"
```

---

### Task 2: Backend API — three watchlist routes

**Files:**
- Modify: `backend/internal/api/api.go` (route registration near line 137; handlers near the `handleBalance`/`handlePortfolio` block ~line 758)
- Modify: `docs/interface-contract.md`

**Interfaces:**
- Consumes: `s.store.AddWatch`, `s.store.RemoveWatch`, `s.store.Watchlist` (Task 1); `models.ParsePubkey`, `models.ParseHash`, `models.HashString`; helpers `writeJSON`, `httpError`; `store.ErrNotFound`.
- Produces: routes `GET /watchlist`, `POST /watchlist`, `DELETE /watchlist/{market_id}`.

- [ ] **Step 1: Register the routes** — in `backend/internal/api/api.go`, after the `GET /portfolio` line (~137):

```go
	mux.HandleFunc("GET /watchlist", s.handleGetWatchlist)
	mux.HandleFunc("POST /watchlist", s.handleAddWatch)
	mux.HandleFunc("DELETE /watchlist/{market_id}", s.handleRemoveWatch)
```

- [ ] **Step 2: Add the handlers** — append after `handlePortfolio` (before the `// ---- helpers ----` block ~line 890):

```go
// handleGetWatchlist returns a wallet's watched market ids (64-hex), newest first.
func (s *Server) handleGetWatchlist(w http.ResponseWriter, r *http.Request) {
	wallet := r.URL.Query().Get("wallet")
	if _, err := models.ParsePubkey(wallet); err != nil {
		httpError(w, http.StatusBadRequest, "wallet query param: "+err.Error())
		return
	}
	ids, err := s.store.Watchlist(r.Context(), wallet)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = models.HashString(id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"market_ids": out})
}

// handleAddWatch favourites a market for a wallet. Body: {wallet, market_id}.
func (s *Server) handleAddWatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Wallet   string `json:"wallet"`
		MarketID string `json:"market_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	if _, err := models.ParsePubkey(body.Wallet); err != nil {
		httpError(w, http.StatusBadRequest, "wallet: "+err.Error())
		return
	}
	marketID, err := models.ParseHash(body.MarketID)
	if err != nil {
		httpError(w, http.StatusBadRequest, "market_id: "+err.Error())
		return
	}
	if err := s.store.AddWatch(r.Context(), body.Wallet, marketID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusBadRequest, "unknown market")
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleRemoveWatch unfavourites a market. Path: market_id; query: wallet.
func (s *Server) handleRemoveWatch(w http.ResponseWriter, r *http.Request) {
	wallet := r.URL.Query().Get("wallet")
	if _, err := models.ParsePubkey(wallet); err != nil {
		httpError(w, http.StatusBadRequest, "wallet query param: "+err.Error())
		return
	}
	marketID, err := models.ParseHash(r.PathValue("market_id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "market_id: "+err.Error())
		return
	}
	if err := s.store.RemoveWatch(r.Context(), wallet, marketID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
```

- [ ] **Step 3: Verify `store` and `errors` are imported** in `api.go`

Run: `cd backend && grep -nE '"errors"|internal/store"' internal/api/api.go`
Expected: both present. If `"errors"` is missing, add it to the import block. (The `store` package import is already used by existing handlers; confirm it appears.)

- [ ] **Step 4: Build + vet**

Run: `cd backend && go build ./... && go vet ./...`
Expected: no errors.

- [ ] **Step 5: Smoke test the routes against a running backend**

Assuming the backend runs locally on `:8080` and a real market id `MID` exists and a base58 wallet `W`:

```bash
curl -s "http://localhost:8080/watchlist?wallet=$W"
curl -s -X POST http://localhost:8080/watchlist -H 'content-type: application/json' -d "{\"wallet\":\"$W\",\"market_id\":\"$MID\"}"
curl -s "http://localhost:8080/watchlist?wallet=$W"          # -> {"market_ids":["MID"]}
curl -s -X DELETE "http://localhost:8080/watchlist/$MID?wallet=$W"
```
Expected: the GET after POST lists `MID`; after DELETE it is empty. (If no backend is running, this step is deferred to the final integration smoke in Task 8; note it as unverified.)

- [ ] **Step 6: Document routes in `docs/interface-contract.md`**

Add a subsection (matching the file's existing REST-route style) covering:
`GET /watchlist?wallet=` → `{ market_ids: string[] }`; `POST /watchlist` body `{ wallet, market_id }` → `{ ok: true }`; `DELETE /watchlist/{market_id}?wallet=` → `{ ok: true }`. Note wallet is base58, market_id 64-hex, and that the list is newest-first.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/api.go docs/interface-contract.md
git commit -m "feat(api): watchlist REST routes"
```

---

### Task 3: Frontend api.ts — watchlist client methods

**Files:**
- Modify: `frontend/lib/api.ts` (add `del` helper near `post` ~line 45; add methods to the `api` object ~line 243)

**Interfaces:**
- Consumes: existing `get`, `post`, `BASE`, `ApiError`, `safeText`.
- Produces (on `api`):
  - `getWatchlist(wallet: string | null): Promise<string[]>`
  - `addWatch(wallet: string, marketId: string): Promise<void>`
  - `removeWatch(wallet: string, marketId: string): Promise<void>`

- [ ] **Step 1: Add a `del` helper** after the `post` function (~line 54 in `frontend/lib/api.ts`):

```ts
async function del(path: string): Promise<void> {
  if (!BASE) throw new ApiError(0, "NEXT_PUBLIC_API_URL is not configured");
  const res = await fetch(`${BASE}${path}`, { method: "DELETE" });
  if (!res.ok) throw new ApiError(res.status, await safeText(res));
}
```

- [ ] **Step 2: Add the three methods** to the `api` object (place after `getPortfolio`, keeping trailing comma style):

```ts
  async getWatchlist(wallet: string | null): Promise<string[]> {
    if (!wallet) return [];
    return get<string[], { market_ids: string[] | null }>(
      `/watchlist?wallet=${encodeURIComponent(wallet)}`,
      (w) => w.market_ids ?? []
    );
  },

  async addWatch(wallet: string, marketId: string): Promise<void> {
    await post<{ ok: boolean }>(`/watchlist`, { wallet, market_id: marketId });
  },

  async removeWatch(wallet: string, marketId: string): Promise<void> {
    await del(`/watchlist/${marketId}?wallet=${encodeURIComponent(wallet)}`);
  },
```

- [ ] **Step 3: Type-check**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/lib/api.ts
git commit -m "feat(web): watchlist api client methods"
```

---

### Task 4: Frontend — WatchlistProvider context + hook

**Files:**
- Create: `frontend/lib/watchlist.tsx`
- Modify: `frontend/components/Providers.tsx`

**Interfaces:**
- Consumes: `usePitchWallet` (`{ address: string | null }`), `api.getWatchlist/addWatch/removeWatch` (Task 3).
- Produces:
  - `WatchlistProvider({ children })`
  - `useWatchlist(): { isWatched(id: string): boolean; toggle(id: string): void; ready: boolean }`

- [ ] **Step 1: Create `frontend/lib/watchlist.tsx`**

```tsx
"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { usePitchWallet } from "@/lib/wallet";

interface WatchlistCtx {
  isWatched: (id: string) => boolean;
  toggle: (id: string) => void;
  ready: boolean;
}

const Ctx = createContext<WatchlistCtx | null>(null);

export function useWatchlist(): WatchlistCtx {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useWatchlist must be used within WatchlistProvider");
  return ctx;
}

export function WatchlistProvider({ children }: { children: React.ReactNode }) {
  const wallet = usePitchWallet();
  const [ids, setIds] = useState<Set<string>>(new Set());
  const [ready, setReady] = useState(false);

  // Load once per connected wallet; clear on disconnect.
  useEffect(() => {
    let alive = true;
    setReady(false);
    if (!wallet.address) {
      setIds(new Set());
      setReady(true);
      return;
    }
    api
      .getWatchlist(wallet.address)
      .then((list) => {
        if (alive) {
          setIds(new Set(list));
          setReady(true);
        }
      })
      .catch(() => {
        if (alive) setReady(true);
      });
    return () => {
      alive = false;
    };
  }, [wallet.address]);

  const isWatched = useCallback((id: string) => ids.has(id), [ids]);

  // Optimistic: flip the set now, call the API in the background, revert on error.
  const toggle = useCallback(
    (id: string) => {
      const addr = wallet.address;
      if (!addr) return;
      const currentlyWatched = ids.has(id);
      setIds((prev) => {
        const next = new Set(prev);
        if (currentlyWatched) next.delete(id);
        else next.add(id);
        return next;
      });
      const call = currentlyWatched
        ? api.removeWatch(addr, id)
        : api.addWatch(addr, id);
      call.catch(() => {
        setIds((prev) => {
          const next = new Set(prev);
          if (currentlyWatched) next.add(id);
          else next.delete(id);
          return next;
        });
      });
    },
    [ids, wallet.address]
  );

  return <Ctx.Provider value={{ isWatched, toggle, ready }}>{children}</Ctx.Provider>;
}
```

- [ ] **Step 2: Wire it into `frontend/components/Providers.tsx`**

Replace the body so `WatchlistProvider` wraps children inside the wallet provider:

```tsx
"use client";

import { MotionConfig } from "framer-motion";
import { PitchWalletProvider } from "@/lib/wallet";
import { WatchlistProvider } from "@/lib/watchlist";

/**
 * MotionConfig reducedMotion="user": the globals.css kill-switch only zeroes
 * CSS transitions/animations — Framer Motion drives values from JS and ignores
 * it. This makes every motion.* component honor prefers-reduced-motion
 * (transforms disabled, opacity crossfades kept — which is the right fallback:
 * fewer and gentler, not zero).
 */
export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <MotionConfig reducedMotion="user">
      <PitchWalletProvider>
        <WatchlistProvider>{children}</WatchlistProvider>
      </PitchWalletProvider>
    </MotionConfig>
  );
}
```

- [ ] **Step 3: Type-check**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/lib/watchlist.tsx frontend/components/Providers.tsx
git commit -m "feat(web): watchlist context provider"
```

---

### Task 5: Frontend — StarButton component

**Files:**
- Create: `frontend/components/StarButton.tsx`

**Interfaces:**
- Consumes: `useWatchlist` (Task 4), `usePitchWallet`.
- Produces: `StarButton({ marketId, className }: { marketId: string; className?: string })` — renders `null` when no wallet connected.

- [ ] **Step 1: Create `frontend/components/StarButton.tsx`**

```tsx
"use client";

import { usePitchWallet } from "@/lib/wallet";
import { useWatchlist } from "@/lib/watchlist";

// Star toggle for a market's watchlist membership. Hidden when no wallet is
// connected (there is nowhere to persist the preference). Stops click
// propagation so it can sit inside link/card surfaces.
export function StarButton({
  marketId,
  className = "",
}: {
  marketId: string;
  className?: string;
}) {
  const wallet = usePitchWallet();
  const { isWatched, toggle } = useWatchlist();
  if (!wallet.address) return null;

  const watched = isWatched(marketId);
  return (
    <button
      type="button"
      aria-pressed={watched}
      aria-label={watched ? "Remove from watchlist" : "Add to watchlist"}
      title={watched ? "In watchlist" : "Add to watchlist"}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        toggle(marketId);
      }}
      className={`shrink-0 transition-colors duration-150 ${
        watched ? "text-accent" : "text-dim hover:text-muted"
      } ${className}`}
    >
      <svg
        width="15"
        height="15"
        viewBox="0 0 24 24"
        fill={watched ? "currentColor" : "none"}
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M12 17.3 6.16 20.5l1.12-6.53L2.5 9.36l6.56-.95L12 2.5l2.94 5.91 6.56.95-4.78 4.61 1.12 6.53z" />
      </svg>
    </button>
  );
}
```

- [ ] **Step 2: Type-check**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors (unused-import warnings are fine; component is wired up in Tasks 6–8).

- [ ] **Step 3: Commit**

```bash
git add frontend/components/StarButton.tsx
git commit -m "feat(web): StarButton watchlist toggle"
```

---

### Task 6: Frontend — stars on home cards + market page

**Files:**
- Modify: `frontend/app/page.tsx` (`BinaryCard` open + resolved branches, and the precision card `<Link>`)
- Modify: `frontend/app/market/[id]/page.tsx` (price header block)

**Interfaces:**
- Consumes: `StarButton` (Task 5). Each market exposes `m.market_id`.

- [ ] **Step 1: Import `StarButton` in `frontend/app/page.tsx`**

Add to the imports at the top:

```tsx
import { StarButton } from "@/components/StarButton";
```

- [ ] **Step 2: Add the star to the open `BinaryCard` branch**

In the open-status card's action row (`frontend/app/page.tsx`, the `<div className="flex shrink-0 items-center gap-1.5">` holding Yes/No), add the star as the first child:

```tsx
          <div className="flex shrink-0 items-center gap-1.5">
            <StarButton marketId={m.market_id} />
            <Link href={`/market/${m.market_id}?o=yes`} className={yesBtn}>
              Yes
            </Link>
            <Link href={`/market/${m.market_id}?o=no`} className={noBtn}>
              No
            </Link>
          </div>
```

- [ ] **Step 3: Add the star to the resolved `BinaryCard` branch**

In the resolved-market `<Link className={cardLink}>` return, change the bottom row to include the star before `MarketState`:

```tsx
      <div className="mt-3 flex items-center justify-between">
        <span className="font-mono text-[10px] uppercase tracking-[0.12em] text-dim">
          {kindOf(m)}
        </span>
        <span className="flex items-center gap-2">
          <StarButton marketId={m.market_id} />
          <MarketState market={m} />
        </span>
      </div>
```

- [ ] **Step 4: Add the star to the precision card**

In `MatchSection`, the precision `<Link>`'s bottom row, add the star before `PrecisionState`:

```tsx
            <div className="mt-3 flex items-center justify-between">
              <span className="font-mono text-[10px] uppercase tracking-[0.12em] text-dim">
                {kindOf(m)}
              </span>
              <span className="flex items-center gap-2">
                <StarButton marketId={m.market_id} />
                <PrecisionState market={m} />
              </span>
            </div>
```

- [ ] **Step 5: Add the star to the market detail price header**

In `frontend/app/market/[id]/page.tsx`, import it:

```tsx
import { StarButton } from "@/components/StarButton";
```

Then in the price-header block (the `<div className="mb-8 flex items-end justify-between gap-6">` region around lines 49–?), place a `StarButton marketId={params.id}` next to the title. Read the surrounding markup first and insert it as a sibling of the title element so it aligns on the header's trailing edge, e.g. wrap the title and star in a `flex items-center gap-2` container.

- [ ] **Step 6: Type-check + run**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

Run the dev server (`npm run dev`), connect the demo wallet, and confirm: a star appears on each home card and the market page; clicking it fills/empties instantly and survives a reload.

- [ ] **Step 7: Commit**

```bash
git add frontend/app/page.tsx "frontend/app/market/[id]/page.tsx"
git commit -m "feat(web): watchlist star on market cards and detail page"
```

---

### Task 7: Frontend — "Watchlist" group atop search

**Files:**
- Modify: `frontend/components/CommandPalette.tsx`

**Interfaces:**
- Consumes: `useWatchlist` (Task 4), existing `results`/`Group`/`flat`/keyboard-nav machinery.

- [ ] **Step 1: Import and read the watchlist**

Add import:

```tsx
import { useWatchlist } from "@/lib/watchlist";
```

Inside `CommandPalette`, near the other hooks:

```tsx
  const { isWatched } = useWatchlist();
```

- [ ] **Step 2: Derive a watchlist result group**

Extend the `results` `useMemo` to also compute watched hits. Change its return to include a `watch` array (watched + query-matching, in either type), and keep `binary`/`precision` **excluding** watched entries so nothing is listed twice:

```tsx
    const all = markets.filter(hit);
    const watch = all.filter((m) => isWatched(m.market_id)).map(toResult);
    const rest = all.filter((m) => !isWatched(m.market_id));
    return {
      watch,
      binary: rest.filter((m) => m.type === "binary").map(toResult),
      precision: rest.filter((m) => m.type === "precision").map(toResult),
    };
```

Add `isWatched` to the `useMemo` dependency array.

- [ ] **Step 3: Include watch results in flat nav order (first)**

Update `flat`:

```tsx
  const flat = useMemo(
    () => [...results.watch, ...results.binary, ...results.precision],
    [results]
  );
```

- [ ] **Step 4: Render the Watchlist group first and shift start indices**

In the results render block, add the group above "Markets", and offset the existing groups' `startIdx` by `results.watch.length`:

```tsx
              <Group
                label="Watchlist"
                items={results.watch}
                startIdx={0}
                sel={sel}
                setSelected={setSelected}
                go={go}
              />
              <Group
                label="Markets"
                items={results.binary}
                startIdx={results.watch.length}
                sel={sel}
                setSelected={setSelected}
                go={go}
              />
              <Group
                label="Precision"
                items={results.precision}
                startIdx={results.watch.length + results.binary.length}
                sel={sel}
                setSelected={setSelected}
                go={go}
              />
```

- [ ] **Step 5: Type-check + run**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

Open search (⌘K) with a wallet that has watched markets: the "Watchlist" group appears first, those markets are not duplicated under Markets/Precision, and arrow-key nav walks watchlist rows first.

- [ ] **Step 6: Commit**

```bash
git add frontend/components/CommandPalette.tsx
git commit -m "feat(web): watchlist group atop market search"
```

---

### Task 8: Frontend — watchlist strip on portfolio + docs

**Files:**
- Modify: `frontend/app/portfolio/page.tsx` (add a strip under the Vault/P&L `<section>`)
- Modify: `progress.md`

**Interfaces:**
- Consumes: `useWatchlist` (Task 4), `api.listMarkets`, `Market`/`Match` types, `FlagPair` (optional), `kindOf`.

- [ ] **Step 1: Add imports to `frontend/app/portfolio/page.tsx`**

```tsx
import { useWatchlist } from "@/lib/watchlist";
import type { Market } from "@/lib/types";
import { kindOf } from "@/lib/kinds";
```

(`api` and `Link` are already imported.)

- [ ] **Step 2: Load markets and derive watched ones**

Inside `PortfolioPage`, after the existing state, add:

```tsx
  const { isWatched } = useWatchlist();
  const [markets, setMarkets] = useState<Market[]>([]);
  useEffect(() => {
    api.listMarkets().then(setMarkets).catch(() => {});
  }, []);
  const watched = markets.filter((m) => isWatched(m.market_id));
```

- [ ] **Step 3: Render the strip under the summary section**

Immediately after the closing `</section>` of the Vault/P&L summary (the one with `className="rule-b py-10 sm:py-12"`), insert:

```tsx
            {watched.length > 0 && (
              <section className="rule-b py-6">
                <h2 className="eyebrow mb-4">Watchlist</h2>
                <div className="-mx-1 flex snap-x snap-mandatory gap-3 overflow-x-auto px-1 pb-1">
                  {watched.map((m) => (
                    <Link
                      key={m.market_id}
                      href={m.type === "precision" ? `/precision/${m.market_id}` : `/market/${m.market_id}`}
                      className="group flex min-h-[76px] w-[220px] shrink-0 snap-start flex-col justify-between rounded-[3px] border border-line bg-line/40 p-3.5 transition-[transform,border-color,background-color] duration-150 ease-out-strong hover:-translate-y-px hover:border-line2 hover:bg-line/70"
                    >
                      <span className="line-clamp-2 text-[13px] leading-snug text-ink transition-colors group-hover:text-accent">
                        {m.title}
                      </span>
                      <span className="mt-2 flex items-center justify-between font-mono text-[10px] uppercase tracking-[0.12em] text-dim">
                        <span className="truncate">{kindOf(m)}</span>
                        <span className={m.status === "settled" ? "text-accent/70" : ""}>
                          {m.status !== "open" ? m.status : ""}
                        </span>
                      </span>
                    </Link>
                  ))}
                </div>
              </section>
            )}
```

- [ ] **Step 4: Type-check + run**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

Run the dev server, connect the demo wallet, favourite a couple of markets from the home page, then open `/portfolio`: a horizontally scrollable "Watchlist" strip shows under the Vault/P&L numbers, cards link to the right market, and the strip is absent when nothing is watched.

- [ ] **Step 5: Update `progress.md`**

Per the project's one-rule: add/adjust a component-table row for the watchlist feature (mark 🟡 unverified for anything not yet run on a live backend, ✅ for what you executed), and append a Changelog row dated 2026-07-18 describing the watchlist (backend routes + web UI).

- [ ] **Step 6: Full verification pass**

Run:
```bash
cd backend && go build ./... && go vet ./... && go test -p 1 ./internal/store/ -run TestWatch
cd ../frontend && npx tsc --noEmit && npm run build
```
Expected: store tests PASS; both builds succeed. Then, against a live backend + web dev server, run the end-to-end smoke: favourite from a card → appears in search group and portfolio strip → survives reload → unfavourite removes it everywhere.

- [ ] **Step 7: Commit**

```bash
git add frontend/app/portfolio/page.tsx progress.md
git commit -m "feat(web): watchlist strip on portfolio page"
```

---

## Self-Review Notes

- **Spec coverage:** table + store (T1), routes + contract (T2), api client (T3), context (T4), StarButton (T5), home + market stars (T6), search group (T7), portfolio strip + progress.md (T8). All spec sections mapped.
- **No-duplication in search:** T7 excludes watched markets from the Markets/Precision groups so watched entries appear only in the Watchlist group.
- **Type consistency:** `AddWatch/RemoveWatch/Watchlist` signatures identical across T1→T2; `getWatchlist/addWatch/removeWatch` identical across T3→T4; `useWatchlist()` shape `{ isWatched, toggle, ready }` identical across T4→T5/T6/T7/T8.
- **Edge cases:** no-wallet star hidden (T5), empty strip/group hidden (T7/T8), stale watched markets naturally skipped because rendering is driven by the intersection with `listMarkets()` (T7/T8), optimistic revert on API error (T4).
