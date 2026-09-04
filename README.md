# bot33 — Robinhood Chain NFT + memecoin bot

Watch curated/discovered wallets on Robinhood Chain (`4663`). Prioritize collections when **≥2 smart wallets** mint or buy. Free SeaDrop sweeps across multiple wallets. Memecoin alerts/buys with locked-LP gates.

## Quick start

```bash
cp .env.example .env
# TELEGRAM_* and EXECUTOR_PRIVATE_KEYS (comma-separated) or EXECUTOR_PRIVATE_KEY

docker compose up -d --build
# UI http://SERVER:8080
```

For host-side `go run`, keep `.env` on published ports (`127.0.0.1:5434` / `:6381`).  
Compose **overrides** `DATABASE_URL`/`REDIS_URL` inside services to `postgres:5432` and `redis:6379`.

## NFT priority + free mints

- **Silent until consensus:** first smart-wallet mint/buy is tracked only; **second** distinct watched wallet escalates.
- **FREE_MINT** — SeaDrop public drop, `mintPrice=0`, drop open, collection ≤24h from first watched activity → Telegram + optional auto-sweep.
- **PRIORITY / SECONDARY_SMART** — same 2-wallet rule on paid mint or Seaport buys (alert only; no Seaport auto-buy yet).
- **Sell alerts off** by default (`ALERT_ON_SELL=false`).
- Ungated per-mint Telegram spam removed.

### Multi-wallet sweep

- `EXECUTOR_PRIVATE_KEYS=0xabc...,0xdef...` (falls back to `EXECUTOR_PRIVATE_KEY`)
- Settings: qty/wallet, max wallets (default 3), max total NFTs/collection (default 20)
- UI Execute → **Sweep free mint**; auto-sweep when `auto_copy_mint` is on and free-mint gates pass
- Live needs `execute_live` + keys; dry-run is default
- Per-(collection, signer) live dedup (not one mint for the whole collection)

## Memecoins

Separate `meme-watcher` (same Postgres) tracks Uniswap V2/V3/V4 launches.

- Age from **first liquidity**, max **30 days**
- Telegram: `TELEGRAM_MEME_CHAT_ID`
- Alerts/buys only with **locked LP** + score ≥70, **or** ≥2 smart wallets buying the same token (`SMART_WALLET_WATCH`)
- Smart-wallet path uses the same curated/discovered watch set as NFTs; Telegram says `source: smart wallets watch`
- Auto-buy on smart-wallet consensus only if LP is also locked
- UI Memecoins tab: max spend, auto-buy, dry-run/LIVE, orders
- Live meme buys: first executor key + `meme_execute_live`

```bash
go run ./cmd/meme-watcher
```

## Discovery

- Windowed scoring over `DISCOVERY_WINDOW` (default 30d)
- Promote at most `DISCOVERY_TOP_N` discovered wallets
- Seaport 1.6 sales → `nft_trades` for scoring + secondary consensus

## Safety

- Default dry-run for NFT mint and meme buy
- Keys only in `.env`, never in UI
- Only SeaDrop **public** mint path (no WL/FCFS signatures)
- Floor protection via `mint_max_total`
