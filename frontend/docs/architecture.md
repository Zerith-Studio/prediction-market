# PitchMarket frontend architecture

This diagram describes the code in `frontend/`. The Go exchange API, its data
stores and workers, TxLINE, and Solana are external to this directory, so they
are shown as system boundaries rather than frontend-owned components.

```mermaid
flowchart TB
  trader[Trader]
  maker[Market maker]
  operator[Operator]

  subgraph browser[Browser — Next.js 14 App Router]
    direction TB

    subgraph routes[Route layer — app/]
      direction LR
      discovery["Discovery<br/><code>/</code>"]
      market["Live market<br/><code>/market/[id]</code>"]
      account["Account<br/><code>/portfolio</code>"]
      products["Other products<br/><code>/precision/[id]</code><br/><code>/combos</code><br/><code>/settlement/[id]</code>"]
      desks["Restricted desks<br/><code>/mm</code><br/><code>/admin</code>"]
    end

    subgraph view[UI composition — components/]
      direction LR
      shell["Shell<br/>TopBar · Providers · CommandPalette"]
      marketUI["Market UI<br/>MatchHero · MatchCentre · PriceChart<br/>OrderBook · RecentFills · Comments"]
      actionUI["Action UI<br/>TradePanel · MarketPositions<br/>StarButton · AdminComments"]
    end

    subgraph client[Client state and domain layer — lib/]
      direction LR
      live["Realtime hooks<br/>useLiveMarket · useComments"]
      actions["Actions and state<br/>usePositionActions · watchlist"]
      models["Domain model<br/>types · kinds · format · teams"]
      signing["Signing codec<br/>borsh order · combo quote"]
      wallet["Wallet context<br/>Privy embedded wallet<br/>or local Ed25519 demo wallet"]
      rest["Public REST client<br/>api.ts"]
      admin["Admin REST client<br/>adminApi.ts"]
    end

    storage[("Browser localStorage<br/>demo wallet seed<br/>admin session<br/>chart history")]
  end

  subgraph exchange[External — Go exchange API · NEXT_PUBLIC_API_URL]
    direction LR
    publicAPI["Public REST<br/>markets · matches · books · fills<br/>orders · balances · portfolio<br/>comments · watchlist · precision · combos"]
    adminAPI["Admin REST<br/>wallet challenge/session<br/>fixtures · market lifecycle · moderation"]
    ws["WebSocket /ws<br/>book_update · fill · oneliner<br/>match_state · comment · combo_quote"]
    exchangeCore["Exchange service<br/>matching · RFQ · settlement orchestration"]
  end

  privy["External — Privy<br/>authentication + embedded Solana wallet"]
  txline["External — TxLINE<br/>football fixtures, odds, and live state"]
  solana["External — Solana devnet<br/>settlement program + USDC vault"]
  explorer["External — Solana Explorer"]

  trader --> discovery
  trader --> market
  trader --> account
  trader --> products
  maker --> desks
  operator --> desks

  routes --> view
  view --> live
  view --> actions
  view --> models
  actions --> signing
  actions --> rest
  signing --> wallet
  live --> rest
  live <-->|"live subscriptions"| ws
  desks --> admin
  desks --> signing
  products --> rest
  wallet <-->|"production wallet mode"| privy
  wallet <-->|"seed + session"| storage
  live <-->|"per-market chart cache"| storage
  admin <-->|"admin session token"| storage

  rest -->|"HTTP JSON"| publicAPI
  admin -->|"X-Admin-Session"| adminAPI
  publicAPI --> exchangeCore
  adminAPI --> exchangeCore
  exchangeCore --> ws
  txline --> exchangeCore
  exchangeCore --> solana
  routes -. "verification links" .-> explorer
  explorer --> solana

  classDef person fill:#f4f0e6,stroke:#816b3a,color:#17130b;
  classDef route fill:#eaf2ff,stroke:#4777ad,color:#10233b;
  classDef ui fill:#eef7f0,stroke:#4e8460,color:#10291a;
  classDef domain fill:#f5edff,stroke:#8060a8,color:#251438;
  classDef external fill:#fff2e8,stroke:#b66a39,color:#381a0c;
  class trader,maker,operator person;
  class discovery,market,account,products,desks route;
  class shell,marketUI,actionUI ui;
  class live,actions,models,signing,wallet,rest,admin domain;
  class publicAPI,adminAPI,ws,exchangeCore,privy,txline,solana,explorer external;
```

## Main runtime flows

1. **Read/live market:** a route gets its initial snapshot through `api.ts`, then
   `useLiveMarket` merges `/ws` events into the book, fills, match state, ticker,
   and chart history.
2. **Trade/exit:** the UI serializes an order with the Borsh codec, asks the
   selected wallet implementation for an Ed25519 signature, and posts the signed
   order to the exchange. The resulting book and fills return over WebSocket.
3. **Combo RFQ:** a taker creates an RFQ and polls for a quote; a market maker
   signs a Borsh combo quote; the taker accepts it through REST. The backend
   also exposes `combo_quote` on the shared WebSocket surface, although this
   frontend currently uses polling for that step.
4. **Administration:** an operator signs a server challenge, stores the returned
   session token locally, and sends it as `X-Admin-Session` for lifecycle and
   moderation calls.

## Deployment configuration

| Variable | Purpose |
| --- | --- |
| `NEXT_PUBLIC_API_URL` | Base URL for both REST and the derived `/ws` URL |
| `NEXT_PUBLIC_PRIVY_APP_ID` | Enables Privy; without it, the local demo wallet is used |
