# bot33 — Robinhood Chain NFT smart-wallet watcher

Go watcher that alerts on Telegram when curated or auto-discovered smart wallets mint/buy NFTs on Robinhood Chain (`4663`), plus a minimal ops UI.

## Stack

- Go + Fiber (API + static UI)
- React (Vite) console at `/`
- `go-ethereum` log polling
- PostgreSQL
- Telegram alerts

## Quick start

```bash
cp .env.example .env
# fill TELEGRAM_BOT_TOKEN + TELEGRAM_CHAT_ID

docker compose up -d postgres
make web-build
go run ./cmd/api
go run ./cmd/watcher
```

Open **http://127.0.0.1:8080**

Dev UI with hot reload:

```bash
go run ./cmd/api      # :8080
cd web && npm run dev # :5173 proxies /api
```

### UI
- **Wallets** — list, add, pause/resume, remove, reload seed
- **NFT activity** — mint/buy/sell feed from watched wallets
- **Collections** — manage known NFT contracts

### API
- `GET /api/status` · `GET /api/wallets` · `POST /api/wallets`
- `PATCH|DELETE /api/wallets/:address`
- `GET /api/trades` · `GET|POST /api/collections`

## Tests

```bash
go test ./...
cd web && npm run build
```
