# Deploying PitchMarket

Two pieces: the Go backend (a long-running process — REST/WS, TxLINE SSE feeds,
the crank) and the Next.js frontend. Serverless does NOT fit the backend; it
needs a host that keeps one machine awake.

## Backend → Fly.io (config included)

```sh
cd backend
fly launch --no-deploy                # accept the included fly.toml
fly volumes create pitchdata -s 1     # persists TxLINE credentials + USDC mint id
fly secrets set \
  DATABASE_URL='postgresql://…' \
  SOLANA_RPC_URL='https://solana-devnet.g.alchemy.com/v2/…' \
  GEMINI_API_KEY='…' \
  CORS_ORIGIN='https://<your-frontend>.vercel.app' \
  OPERATOR_KEYPAIR_JSON="$(cat ~/.config/solana/id.json)"
fly deploy
```

Any Docker host works the same way (Railway, Render, a VPS): build
`backend/Dockerfile`, mount a volume at `/data`, set the same env. The
entrypoint materializes `OPERATOR_KEYPAIR_JSON` into a key file.

Notes:
- The operator wallet is the program upgrade authority, tier-a resolver, fee
  payer, and USDC mint authority — treat `OPERATOR_KEYPAIR_JSON` as the crown
  jewels of the demo.
- First boot on an empty volume self-provisions TxLINE credentials (an
  on-chain subscribe tx, ~0.001 SOL) and creates/caches the demo USDC mint.
- Keep `min_machines_running = 1` — if the machine sleeps, feeds and the crank
  stop with it.

## Frontend → Vercel

```sh
cd frontend
vercel link
vercel env add NEXT_PUBLIC_API_URL      # https://pitchmarket-backend.fly.dev
vercel env add NEXT_PUBLIC_PRIVY_APP_ID # from dashboard.privy.io (optional:
                                        # without it users get the local demo wallet)
vercel deploy --prod
```

Then pin the backend's `CORS_ORIGIN` to the exact Vercel URL.

## Smoke test after deploy

1. `curl https://<backend>/healthz` → 200
2. `curl https://<backend>/markets?status=open` → real TxLINE fixtures
3. Load the frontend → markets index shows the live match; connect wallet →
   Fund 1,000 demo USDC (a real devnet tx) → place an order → watch it fill
   against the bot and gain a `settle tx` explorer link in the portfolio.
