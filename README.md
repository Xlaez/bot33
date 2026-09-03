# bot33 — Robinhood Chain NFT smart-wallet watcher

Go watcher that alerts on Telegram when curated or auto-discovered smart wallets mint/buy NFTs on Robinhood Chain (`4663`).

## Stack

- Go + Fiber (ops API)
- `go-ethereum` log polling
- PostgreSQL (wallets, dedupe, trades, cursor)
- Telegram alerts
- Hybrid wallets: curated seed + discovery scorer

## Quick start

```bash
cp .env.example .env
# fill TELEGRAM_BOT_TOKEN + TELEGRAM_CHAT_ID

docker compose up -d postgres redis
go mod tidy
go run ./cmd/api &
go run ./cmd/watcher
```

API:
- `GET /health`
- `GET /wallets` / `GET /wallets/watch`
- `POST /wallets` — add curated wallet
- `POST /wallets/seed` — reload `configs/wallets.seed.yaml`

## Refresh curated seed

```bash
python3 scripts/research_seed_fast.py
# or
go run ./cmd/research-seed
```

Seed is built from OpenSea collection owners + early `ownerOf` holders across major RHC NFT contracts.

## Tests

```bash
go test ./...
```
