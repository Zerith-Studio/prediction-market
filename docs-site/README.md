# PitchMarket docs site

Technical documentation for PitchMarket, built with [Mintlify](https://mintlify.com).
Content is derived from the repo's pinned documents (`docs/interface-contract.md`,
`docs/adr/`, `docs/HANDOFF.md`, `PROJECT_PLAN.md`, `progress.md`) — those stay the
source of truth; edit them first, then reflect changes here.

## Run locally

```sh
cd docs-site
npx mint dev
```

Serves at http://localhost:3000 (pass `--port` to change). Requires Node ≥ 20; the
first run downloads the Mintlify CLI.

To check for broken internal links:

```sh
npx mint broken-links
```

## Layout

```
docs.json                 site config + navigation
index.mdx                 overview
architecture.mdx          E1/E2/frontend/mobile map
setup.mdx                 toolchain, build & verify commands
concepts/                 matching engine, trust model
onchain/                  program accounts/instructions, signed messages & tx layout
api/                      REST + WebSocket surface
reference/                ADR index, status snapshot
```
