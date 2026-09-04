# bot33 — Robinhood Chain NFT smart-wallet watcher + mint execution

Watch curated/discovered wallets on Robinhood Chain (`4663`), alert via Telegram, and optionally copy/manual SeaDrop **public** mints with a UI spend cap.

## Quick start

```bash
cp .env.example .env
# TELEGRAM_* and optional EXECUTOR_PRIVATE_KEY

docker compose up -d postgres
make web-build
go run ./cmd/api        # UI http://127.0.0.1:8080
go run ./cmd/watcher    # from repo root
```

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
