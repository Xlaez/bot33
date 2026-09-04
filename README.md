# bot33 — Robinhood Chain NFT smart-wallet watcher + mint execution

Watch curated/discovered wallets on Robinhood Chain (`4663`), alert via Telegram, and optionally copy/manual SeaDrop **public** mints with a UI spend cap.

## Quick start

```bash
cp .env.example .env
# TELEGRAM_* and optional EXECUTOR_PRIVATE_KEY
# Production: point DATABASE_URL/REDIS_URL at the host Postgres/Redis (127.0.0.1)

# Local only (dedicated Postgres :5434 / Redis :6381):
docker compose -f docker-compose.dev.yml up -d postgres redis

make web-build
go run ./cmd/api        # UI http://127.0.0.1:8080
go run ./cmd/watcher    # from repo root
```

## Deploy (Hetzner)

GitHub Actions (`.github/workflows/deploy.yml`) rsyncs to `/opt/bot33` and runs `docker compose up -d`.

Required repo secrets: `SERVER_HOST`, `SERVER_USERNAME`, `SERVER_KEY`.

On the server once:
1. Create `/opt/bot33` and a `.env` (never committed; deploy will not overwrite it)
2. Create DB/user on the existing Postgres if needed
3. Ensure Docker is installed and the deploy user can run `docker compose`

Production compose uses `network_mode: host` so containers reach host Postgres/Redis on `127.0.0.1`.

## Discovery

- Windowed scoring over `DISCOVERY_WINDOW` (default 30d)
- Promote at most `DISCOVERY_TOP_N` (default 40) discovered wallets; demote the rest
- Wash/bot filters block reciprocal pairs, zero-value flips, and unpriced churn
- Secondary sales: Seaport 1.6 (`OrderFulfilled`) → `nft_trades` with `value_wei` (`MARKETPLACE_ENABLED`)

## Execute tab

- **Max spend / NFT** — blocks any mint whose SeaDrop value exceeds the cap
- **Auto-copy** — when a watched wallet *mints*, queue the same collection
- **Dry-run (default)** vs **LIVE** — live needs `EXECUTOR_PRIVATE_KEY` + toggle
- **Manual mint** — paste collection address and queue

Orders appear under Execute with status (`dry_run_ok`, `capped`, `rejected`, `broadcast`, …).

## Safety

- Default is dry-run (`execute_live=false`)
- Private key never exposed in the UI — only in `.env`
- Only SeaDrop **public** mint path (no WL/FCFS OpenSea signatures)
