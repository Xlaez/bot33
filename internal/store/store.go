package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/xlaez/bot33/internal/wallet"
	_ "github.com/jackc/pgx/v5/stdlib"
	"gopkg.in/yaml.v3"
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) migrate(ctx context.Context) error {
	const q = `
CREATE TABLE IF NOT EXISTS wallets (
  address TEXT PRIMARY KEY,
  label TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL,
  tags JSONB NOT NULL DEFAULT '[]',
  collections JSONB NOT NULL DEFAULT '[]',
  evidence JSONB NOT NULL DEFAULT '[]',
  score DOUBLE PRECISION NOT NULL DEFAULT 0,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS seen_events (
  tx_hash TEXT NOT NULL,
  log_index INTEGER NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (tx_hash, log_index)
);

CREATE TABLE IF NOT EXISTS cursor_state (
  name TEXT PRIMARY KEY,
  block_number BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS nft_trades (
  id BIGSERIAL PRIMARY KEY,
  wallet TEXT NOT NULL,
  collection TEXT NOT NULL,
  token_id TEXT NOT NULL,
  side TEXT NOT NULL,
  tx_hash TEXT NOT NULL,
  block_number BIGINT NOT NULL,
  value_wei NUMERIC NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_nft_trades_wallet ON nft_trades(wallet);
CREATE INDEX IF NOT EXISTS idx_nft_trades_created ON nft_trades(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_wallets_active_source ON wallets(active, source);

ALTER TABLE nft_trades ADD COLUMN IF NOT EXISTS counterparty TEXT NOT NULL DEFAULT '';
ALTER TABLE nft_trades ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'transfer';
CREATE INDEX IF NOT EXISTS idx_nft_trades_wallet_created ON nft_trades(wallet, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_nft_trades_counterparty ON nft_trades(counterparty) WHERE counterparty <> '';

CREATE TABLE IF NOT EXISTS collections (
  address TEXT PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  active BOOLEAN NOT NULL DEFAULT TRUE,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE collections ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'curated';
CREATE INDEX IF NOT EXISTS idx_collections_active ON collections(active);

CREATE TABLE IF NOT EXISTS bot_settings (
  id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  max_spend_wei NUMERIC NOT NULL DEFAULT 50000000000000000,
  execute_live BOOLEAN NOT NULL DEFAULT FALSE,
  auto_copy_mint BOOLEAN NOT NULL DEFAULT FALSE,
  mint_quantity INTEGER NOT NULL DEFAULT 1,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO bot_settings (id) VALUES (1) ON CONFLICT DO NOTHING;

ALTER TABLE bot_settings ADD COLUMN IF NOT EXISTS meme_max_spend_wei NUMERIC NOT NULL DEFAULT 20000000000000000;
ALTER TABLE bot_settings ADD COLUMN IF NOT EXISTS meme_execute_live BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE bot_settings ADD COLUMN IF NOT EXISTS meme_auto_buy BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE bot_settings ADD COLUMN IF NOT EXISTS meme_slippage_bps INTEGER NOT NULL DEFAULT 1000;
ALTER TABLE bot_settings ADD COLUMN IF NOT EXISTS mint_max_wallets INTEGER NOT NULL DEFAULT 3;
ALTER TABLE bot_settings ADD COLUMN IF NOT EXISTS mint_max_total INTEGER NOT NULL DEFAULT 20;
ALTER TABLE bot_settings ADD COLUMN IF NOT EXISTS smart_wallet_min INTEGER NOT NULL DEFAULT 2;
ALTER TABLE bot_settings ADD COLUMN IF NOT EXISTS smart_mint_window_min INTEGER NOT NULL DEFAULT 120;
ALTER TABLE bot_settings ADD COLUMN IF NOT EXISTS smart_buy_window_min INTEGER NOT NULL DEFAULT 360;
ALTER TABLE bot_settings ADD COLUMN IF NOT EXISTS new_collection_max_age_h INTEGER NOT NULL DEFAULT 24;

ALTER TABLE mint_orders ADD COLUMN IF NOT EXISTS signer TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS collection_alerts (
  collection TEXT NOT NULL,
  kind TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (collection, kind)
);
CREATE INDEX IF NOT EXISTS idx_collection_alerts_created ON collection_alerts(created_at DESC);

CREATE TABLE IF NOT EXISTS meme_orders (
  id BIGSERIAL PRIMARY KEY,
  source TEXT NOT NULL,
  token TEXT NOT NULL,
  pool_address TEXT NOT NULL DEFAULT '',
  dex TEXT NOT NULL DEFAULT '',
  spend_wei NUMERIC NOT NULL DEFAULT 0,
  min_out_wei NUMERIC NOT NULL DEFAULT 0,
  tx_hash TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  dry_run BOOLEAN NOT NULL DEFAULT TRUE,
  signal_tx TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_meme_orders_created ON meme_orders(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_meme_orders_token ON meme_orders(token);

CREATE TABLE IF NOT EXISTS mint_orders (
  id BIGSERIAL PRIMARY KEY,
  source TEXT NOT NULL,
  collection TEXT NOT NULL,
  quantity INTEGER NOT NULL DEFAULT 1,
  value_wei NUMERIC NOT NULL DEFAULT 0,
  fee_recipient TEXT NOT NULL DEFAULT '',
  signal_tx TEXT NOT NULL DEFAULT '',
  tx_hash TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  dry_run BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mint_orders_created ON mint_orders(created_at DESC);

CREATE TABLE IF NOT EXISTS meme_tokens (
  address TEXT PRIMARY KEY,
  symbol TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  decimals INTEGER NOT NULL DEFAULT 18,
  paired_with TEXT NOT NULL DEFAULT '',
  pool_address TEXT NOT NULL DEFAULT '',
  dex TEXT NOT NULL DEFAULT '',
  fee_tier INTEGER NOT NULL DEFAULT 0,
  launch_tx TEXT NOT NULL DEFAULT '',
  launch_block BIGINT NOT NULL DEFAULT 0,
  first_liquidity_at TIMESTAMPTZ,
  lp_locked BOOLEAN NOT NULL DEFAULT FALSE,
  lp_lock_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
  lp_lock_evidence TEXT NOT NULL DEFAULT '',
  owner_renounced BOOLEAN NOT NULL DEFAULT FALSE,
  score DOUBLE PRECISION NOT NULL DEFAULT 0,
  flags JSONB NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'watching',
  volume_wei NUMERIC NOT NULL DEFAULT 0,
  swap_count INTEGER NOT NULL DEFAULT 0,
  unique_traders INTEGER NOT NULL DEFAULT 0,
  last_swap_at TIMESTAMPTZ,
  alerted_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_meme_tokens_liquidity ON meme_tokens(first_liquidity_at DESC NULLS LAST);
CREATE INDEX IF NOT EXISTS idx_meme_tokens_score ON meme_tokens(score DESC);
CREATE INDEX IF NOT EXISTS idx_meme_tokens_status ON meme_tokens(status);

CREATE TABLE IF NOT EXISTS meme_pools (
  pool_address TEXT PRIMARY KEY,
  token0 TEXT NOT NULL,
  token1 TEXT NOT NULL,
  meme_token TEXT NOT NULL,
  quote_token TEXT NOT NULL,
  dex TEXT NOT NULL,
  fee_tier INTEGER NOT NULL DEFAULT 0,
  created_tx TEXT NOT NULL DEFAULT '',
  created_block BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_meme_pools_token ON meme_pools(meme_token);

CREATE TABLE IF NOT EXISTS meme_smart_buys (
  token TEXT NOT NULL,
  wallet TEXT NOT NULL,
  tx_hash TEXT NOT NULL DEFAULT '',
  pool_address TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (token, wallet)
);
CREATE INDEX IF NOT EXISTS idx_meme_smart_buys_token_created ON meme_smart_buys(token, created_at DESC);
`
	_, err := s.db.ExecContext(ctx, q)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

type Trade struct {
	ID           int64     `json:"id"`
	Wallet       string    `json:"wallet"`
	Collection   string    `json:"collection"`
	TokenID      string    `json:"token_id"`
	Side         string    `json:"side"`
	TxHash       string    `json:"tx_hash"`
	BlockNumber  uint64    `json:"block_number"`
	ValueWei     string    `json:"value_wei"`
	Counterparty string    `json:"counterparty,omitempty"`
	Source       string    `json:"source,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Collection struct {
	Address   string    `json:"address"`
	Name      string    `json:"name"`
	Notes     string    `json:"notes"`
	Source    string    `json:"source,omitempty"`
	Active    bool      `json:"active"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CollectionSaleStats is a short-window view of secondary sales for one NFT contract.
type CollectionSaleStats struct {
	Collection   string
	Sales        int
	UniqueBuyers int
	AvgWei       *big.Int
	MaxWei       *big.Int
	MedianWei    *big.Int
}

type Stats struct {
	WalletsTotal   int    `json:"wallets_total"`
	WalletsWatching int   `json:"wallets_watching"`
	TradesTotal    int    `json:"trades_total"`
	Collections    int    `json:"collections"`
	CursorBlock    uint64 `json:"cursor_block"`
	ChainID        int64  `json:"chain_id,omitempty"`
}

func (s *Store) SetWalletActive(ctx context.Context, address string, active bool) error {
	address = wallet.NormalizeAddress(address)
	res, err := s.db.ExecContext(ctx, `UPDATE wallets SET active=$2, updated_at=NOW() WHERE address=$1`, address, active)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("wallet not found")
	}
	return nil
}

func (s *Store) DeleteWallet(ctx context.Context, address string) error {
	address = wallet.NormalizeAddress(address)
	res, err := s.db.ExecContext(ctx, `DELETE FROM wallets WHERE address=$1`, address)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("wallet not found")
	}
	return nil
}

func (s *Store) ListTrades(ctx context.Context, limit int, watchedOnly bool) ([]Trade, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `
SELECT id, wallet, collection, token_id, side, tx_hash, block_number, value_wei::text,
       COALESCE(counterparty,''), COALESCE(source,'transfer'), created_at
FROM nft_trades
`
	if watchedOnly {
		q += `
WHERE wallet IN (
  SELECT address FROM wallets WHERE active = TRUE AND source IN ('curated','discovered')
)
`
	}
	q += `
ORDER BY created_at DESC
LIMIT $1
`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Trade
	for rows.Next() {
		var t Trade
		if err := rows.Scan(&t.ID, &t.Wallet, &t.Collection, &t.TokenID, &t.Side, &t.TxHash, &t.BlockNumber, &t.ValueWei, &t.Counterparty, &t.Source, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if out == nil {
		out = []Trade{}
	}
	return out, rows.Err()
}

func (s *Store) UpsertCollection(ctx context.Context, c Collection) error {
	c.Address = wallet.NormalizeAddress(c.Address)
	if c.Address == "" {
		return fmt.Errorf("empty address")
	}
	if c.Source == "" {
		c.Source = "curated"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO collections(address, name, notes, active, source, updated_at)
VALUES ($1,$2,$3,$4,$5,NOW())
ON CONFLICT (address) DO UPDATE SET
  name = CASE WHEN EXCLUDED.name <> '' THEN EXCLUDED.name ELSE collections.name END,
  notes = CASE WHEN EXCLUDED.notes <> '' THEN EXCLUDED.notes ELSE collections.notes END,
  active = EXCLUDED.active,
  source = CASE
    WHEN collections.source = 'curated' THEN collections.source
    ELSE EXCLUDED.source
  END,
  updated_at = NOW()
`, c.Address, c.Name, c.Notes, c.Active, c.Source)
	return err
}

func (s *Store) ListCollections(ctx context.Context) ([]Collection, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT address, name, notes, active, COALESCE(source,'curated'), updated_at
FROM collections ORDER BY name ASC, address ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collection
	for rows.Next() {
		var c Collection
		if err := rows.Scan(&c.Address, &c.Name, &c.Notes, &c.Active, &c.Source, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if out == nil {
		out = []Collection{}
	}
	return out, rows.Err()
}

func (s *Store) ListActiveCollections(ctx context.Context) ([]Collection, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT address, name, notes, active, COALESCE(source,'curated'), updated_at
FROM collections WHERE active = TRUE
ORDER BY name ASC, address ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collection
	for rows.Next() {
		var c Collection
		if err := rows.Scan(&c.Address, &c.Name, &c.Notes, &c.Active, &c.Source, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if out == nil {
		out = []Collection{}
	}
	return out, rows.Err()
}

// CollectionSaleStats returns priced buy-side seaport stats for a collection over window.
func (s *Store) CollectionSaleStats(ctx context.Context, collection string, window time.Duration) (CollectionSaleStats, error) {
	collection = wallet.NormalizeAddress(collection)
	out := CollectionSaleStats{
		Collection: collection,
		AvgWei:     big.NewInt(0),
		MaxWei:     big.NewInt(0),
		MedianWei:  big.NewInt(0),
	}
	if collection == "" {
		return out, nil
	}
	hours := int(window.Hours())
	mins := int(window.Minutes())
	interval := fmt.Sprintf("%d hours", hours)
	if window < time.Hour {
		if mins < 1 {
			mins = 1
		}
		interval = fmt.Sprintf("%d minutes", mins)
	} else if hours < 1 {
		interval = "1 hours"
	}
	var avg, maxx string
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*)::int,
       COUNT(DISTINCT wallet)::int,
       COALESCE(AVG(value_wei), 0)::text,
       COALESCE(MAX(value_wei), 0)::text
FROM nft_trades
WHERE collection = $1
  AND side = 'buy'
  AND source = 'seaport'
  AND value_wei > 0
  AND created_at >= NOW() - ($2::text)::interval
`, collection, interval).Scan(&out.Sales, &out.UniqueBuyers, &avg, &maxx)
	if err != nil {
		return out, err
	}
	if v, ok := new(big.Int).SetString(avg, 10); ok {
		out.AvgWei = v
	}
	if v, ok := new(big.Int).SetString(maxx, 10); ok {
		out.MaxWei = v
	}

	// Approximate median via ordered prices.
	rows, err := s.db.QueryContext(ctx, `
SELECT value_wei::text
FROM nft_trades
WHERE collection = $1
  AND side = 'buy'
  AND source = 'seaport'
  AND value_wei > 0
  AND created_at >= NOW() - ($2::text)::interval
ORDER BY value_wei::numeric
`, collection, interval)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	var prices []*big.Int
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return out, err
		}
		if v, ok := new(big.Int).SetString(raw, 10); ok {
			prices = append(prices, v)
		}
	}
	if n := len(prices); n > 0 {
		out.MedianWei = prices[n/2]
	}
	return out, rows.Err()
}

func (s *Store) DeleteCollection(ctx context.Context, address string) error {
	address = wallet.NormalizeAddress(address)
	res, err := s.db.ExecContext(ctx, `DELETE FROM collections WHERE address=$1`, address)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("collection not found")
	}
	return nil
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wallets`).Scan(&st.WalletsTotal)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wallets WHERE active AND source IN ('curated','discovered')`).Scan(&st.WalletsWatching)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nft_trades`).Scan(&st.TradesTotal)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM collections`).Scan(&st.Collections)
	cur, err := s.GetCursor(ctx, "nft_logs")
	if err != nil {
		return st, err
	}
	st.CursorBlock = cur
	return st, nil
}

func (s *Store) LoadCollectionsFile(ctx context.Context, path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var file struct {
		Collections []struct {
			Name    string `yaml:"name"`
			Address string `yaml:"address"`
		} `yaml:"collections"`
	}
	if err := yaml.Unmarshal(b, &file); err != nil {
		return 0, err
	}
	n := 0
	for _, c := range file.Collections {
		if err := s.UpsertCollection(ctx, Collection{
			Address: c.Address,
			Name:    c.Name,
			Source:  "curated",
			Active:  true,
		}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Store) UpsertWallet(ctx context.Context, w wallet.Record) error {
	w.Address = wallet.NormalizeAddress(w.Address)
	if w.Address == "" {
		return fmt.Errorf("empty address")
	}
	if w.Source == "" {
		w.Source = wallet.SourceCurated
	}
	tags, _ := json.Marshal(w.Tags)
	cols, _ := json.Marshal(w.Collections)
	ev, _ := json.Marshal(w.Evidence)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO wallets(address, label, source, tags, collections, evidence, score, active, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())
ON CONFLICT (address) DO UPDATE SET
  label = CASE
    WHEN wallets.source = 'curated' AND EXCLUDED.source = 'discovered' THEN wallets.label
    WHEN EXCLUDED.label <> '' THEN EXCLUDED.label
    ELSE wallets.label
  END,
  source = CASE
    WHEN EXCLUDED.source = 'blocked' THEN EXCLUDED.source
    WHEN wallets.source = 'curated' THEN wallets.source
    WHEN wallets.source = 'blocked' THEN wallets.source
    ELSE EXCLUDED.source
  END,
  tags = EXCLUDED.tags,
  collections = EXCLUDED.collections,
  evidence = EXCLUDED.evidence,
  score = CASE
    WHEN EXCLUDED.source = 'discovered' THEN EXCLUDED.score
    ELSE GREATEST(wallets.score, EXCLUDED.score)
  END,
  active = CASE
    WHEN wallets.source = 'blocked' OR EXCLUDED.source = 'blocked' THEN FALSE
    ELSE EXCLUDED.active
  END,
  updated_at = NOW()
`, w.Address, w.Label, string(w.Source), tags, cols, ev, w.Score, w.Active)
	if err != nil {
		return fmt.Errorf("upsert wallet: %w", err)
	}
	return nil
}

func (s *Store) ListActiveWatchSet(ctx context.Context) ([]wallet.Record, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT address, label, source, tags, collections, evidence, score, active, updated_at
FROM wallets
WHERE active = TRUE AND source IN ('curated','discovered')
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWallets(rows)
}

func (s *Store) ListWallets(ctx context.Context) ([]wallet.Record, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT address, label, source, tags, collections, evidence, score, active, updated_at
FROM wallets ORDER BY updated_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWallets(rows)
}

func scanWallets(rows *sql.Rows) ([]wallet.Record, error) {
	var out []wallet.Record
	for rows.Next() {
		var w wallet.Record
		var source string
		var tags, cols, ev []byte
		if err := rows.Scan(&w.Address, &w.Label, &source, &tags, &cols, &ev, &w.Score, &w.Active, &w.UpdatedAt); err != nil {
			return nil, err
		}
		w.Source = wallet.Source(source)
		_ = json.Unmarshal(tags, &w.Tags)
		_ = json.Unmarshal(cols, &w.Collections)
		_ = json.Unmarshal(ev, &w.Evidence)
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) MarkSeen(ctx context.Context, txHash string, logIndex uint) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO seen_events(tx_hash, log_index) VALUES ($1,$2)
ON CONFLICT DO NOTHING
`, txHash, logIndex)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *Store) GetCursor(ctx context.Context, name string) (uint64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT block_number FROM cursor_state WHERE name=$1`, name).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, nil
	}
	return uint64(n), nil
}

func (s *Store) SetCursor(ctx context.Context, name string, block uint64) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO cursor_state(name, block_number, updated_at) VALUES ($1,$2,NOW())
ON CONFLICT (name) DO UPDATE SET block_number=EXCLUDED.block_number, updated_at=NOW()
`, name, block)
	return err
}

func (s *Store) InsertTrade(ctx context.Context, walletAddr, collection, tokenID, side, txHash string, block uint64, valueWei, counterparty, source string) error {
	if valueWei == "" {
		valueWei = "0"
	}
	if source == "" {
		source = "transfer"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO nft_trades(wallet, collection, token_id, side, tx_hash, block_number, value_wei, counterparty, source)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
`, wallet.NormalizeAddress(walletAddr), wallet.NormalizeAddress(collection), tokenID, side, txHash, block, valueWei, wallet.NormalizeAddress(counterparty), source)
	return err
}

// SyncDiscoveredWatchSet upserts the top-N discovered set and deactivates prior discovered wallets not kept.
// Curated wallets are never demoted by this path.
func (s *Store) SyncDiscoveredWatchSet(ctx context.Context, keep []wallet.Record) (promoted, demoted int, err error) {
	keepSet := make(map[string]struct{}, len(keep))
	for _, w := range keep {
		w.Source = wallet.SourceDiscovered
		w.Active = true
		if err := s.UpsertWallet(ctx, w); err != nil {
			return promoted, demoted, err
		}
		keepSet[wallet.NormalizeAddress(w.Address)] = struct{}{}
		promoted++
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT address FROM wallets WHERE source = 'discovered' AND active = TRUE
`)
	if err != nil {
		return promoted, demoted, err
	}
	defer rows.Close()
	var drop []string
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			return promoted, demoted, err
		}
		if _, ok := keepSet[wallet.NormalizeAddress(addr)]; !ok {
			drop = append(drop, addr)
		}
	}
	if err := rows.Err(); err != nil {
		return promoted, demoted, err
	}
	for _, addr := range drop {
		if err := s.SetWalletActive(ctx, addr, false); err != nil {
			return promoted, demoted, err
		}
		demoted++
	}
	return promoted, demoted, nil
}

func (s *Store) LoadSeedFile(ctx context.Context, path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read seed: %w", err)
	}
	var seed wallet.SeedFile
	if err := yaml.Unmarshal(b, &seed); err != nil {
		return 0, fmt.Errorf("parse seed: %w", err)
	}
	n := 0
	for _, w := range seed.Wallets {
		w.Source = wallet.SourceCurated
		w.Active = true
		if err := s.UpsertWallet(ctx, w); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

type BotSettings struct {
	MaxSpendWei          string `json:"max_spend_wei"`
	ExecuteLive          bool   `json:"execute_live"`
	AutoCopyMint         bool   `json:"auto_copy_mint"`
	MintQuantity         uint64 `json:"mint_quantity"`
	MintMaxWallets       int    `json:"mint_max_wallets"`
	MintMaxTotal         int    `json:"mint_max_total"`
	SmartWalletMin       int    `json:"smart_wallet_min"`
	SmartMintWindowMin   int    `json:"smart_mint_window_min"`
	SmartBuyWindowMin    int    `json:"smart_buy_window_min"`
	NewCollectionMaxAgeH int    `json:"new_collection_max_age_h"`
	MemeMaxSpendWei      string `json:"meme_max_spend_wei"`
	MemeExecuteLive      bool   `json:"meme_execute_live"`
	MemeAutoBuy          bool   `json:"meme_auto_buy"`
	MemeSlippageBps      int    `json:"meme_slippage_bps"`
}

type MintOrder struct {
	ID           int64     `json:"id"`
	Source       string    `json:"source"`
	Collection   string    `json:"collection"`
	Quantity     uint64    `json:"quantity"`
	ValueWei     string    `json:"value_wei"`
	FeeRecipient string    `json:"fee_recipient"`
	SignalTx     string    `json:"signal_tx"`
	TxHash       string    `json:"tx_hash"`
	Status       string    `json:"status"`
	Error        string    `json:"error"`
	DryRun       bool      `json:"dry_run"`
	Signer       string    `json:"signer"`
	CreatedAt    time.Time `json:"created_at"`
}

type MemeOrder struct {
	ID          int64     `json:"id"`
	Source      string    `json:"source"`
	Token       string    `json:"token"`
	PoolAddress string    `json:"pool_address"`
	Dex         string    `json:"dex"`
	SpendWei    string    `json:"spend_wei"`
	MinOutWei   string    `json:"min_out_wei"`
	TxHash      string    `json:"tx_hash"`
	Status      string    `json:"status"`
	Error       string    `json:"error"`
	DryRun      bool      `json:"dry_run"`
	SignalTx    string    `json:"signal_tx"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Store) GetSettings(ctx context.Context) (BotSettings, error) {
	var st BotSettings
	var qty int64
	var slip int64
	var maxWallets, maxTotal, smartMin, mintWin, buyWin, maxAge int64
	err := s.db.QueryRowContext(ctx, `
SELECT max_spend_wei::text, execute_live, auto_copy_mint, mint_quantity,
       mint_max_wallets, mint_max_total, smart_wallet_min, smart_mint_window_min, smart_buy_window_min, new_collection_max_age_h,
       meme_max_spend_wei::text, meme_execute_live, meme_auto_buy, meme_slippage_bps
FROM bot_settings WHERE id=1
`).Scan(&st.MaxSpendWei, &st.ExecuteLive, &st.AutoCopyMint, &qty,
		&maxWallets, &maxTotal, &smartMin, &mintWin, &buyWin, &maxAge,
		&st.MemeMaxSpendWei, &st.MemeExecuteLive, &st.MemeAutoBuy, &slip)
	if err != nil {
		return st, err
	}
	if qty < 1 {
		qty = 1
	}
	st.MintQuantity = uint64(qty)
	if slip <= 0 {
		slip = 1000
	}
	st.MemeSlippageBps = int(slip)
	if st.MemeMaxSpendWei == "" {
		st.MemeMaxSpendWei = "20000000000000000"
	}
	if maxWallets <= 0 {
		maxWallets = 3
	}
	if maxTotal <= 0 {
		maxTotal = 20
	}
	if smartMin <= 0 {
		smartMin = 2
	}
	if mintWin <= 0 {
		mintWin = 120
	}
	if buyWin <= 0 {
		buyWin = 360
	}
	if maxAge <= 0 {
		maxAge = 24
	}
	st.MintMaxWallets = int(maxWallets)
	st.MintMaxTotal = int(maxTotal)
	st.SmartWalletMin = int(smartMin)
	st.SmartMintWindowMin = int(mintWin)
	st.SmartBuyWindowMin = int(buyWin)
	st.NewCollectionMaxAgeH = int(maxAge)
	return st, nil
}

func (s *Store) UpdateSettings(ctx context.Context, st BotSettings) error {
	if st.MintQuantity == 0 {
		st.MintQuantity = 1
	}
	if st.MaxSpendWei == "" {
		st.MaxSpendWei = "50000000000000000"
	}
	if st.MemeMaxSpendWei == "" {
		st.MemeMaxSpendWei = "20000000000000000"
	}
	if st.MemeSlippageBps <= 0 {
		st.MemeSlippageBps = 1000
	}
	if st.MintMaxWallets <= 0 {
		st.MintMaxWallets = 3
	}
	if st.MintMaxTotal <= 0 {
		st.MintMaxTotal = 20
	}
	if st.SmartWalletMin <= 0 {
		st.SmartWalletMin = 2
	}
	if st.SmartMintWindowMin <= 0 {
		st.SmartMintWindowMin = 120
	}
	if st.SmartBuyWindowMin <= 0 {
		st.SmartBuyWindowMin = 360
	}
	if st.NewCollectionMaxAgeH <= 0 {
		st.NewCollectionMaxAgeH = 24
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE bot_settings SET
  max_spend_wei=$1,
  execute_live=$2,
  auto_copy_mint=$3,
  mint_quantity=$4,
  mint_max_wallets=$5,
  mint_max_total=$6,
  smart_wallet_min=$7,
  smart_mint_window_min=$8,
  smart_buy_window_min=$9,
  new_collection_max_age_h=$10,
  meme_max_spend_wei=$11,
  meme_execute_live=$12,
  meme_auto_buy=$13,
  meme_slippage_bps=$14,
  updated_at=NOW()
WHERE id=1
`, st.MaxSpendWei, st.ExecuteLive, st.AutoCopyMint, st.MintQuantity,
		st.MintMaxWallets, st.MintMaxTotal, st.SmartWalletMin, st.SmartMintWindowMin, st.SmartBuyWindowMin, st.NewCollectionMaxAgeH,
		st.MemeMaxSpendWei, st.MemeExecuteLive, st.MemeAutoBuy, st.MemeSlippageBps)
	return err
}

func (s *Store) InsertMemeOrder(ctx context.Context, o MemeOrder) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO meme_orders(source, token, pool_address, dex, spend_wei, min_out_wei, tx_hash, status, error, dry_run, signal_tx)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
`, o.Source, wallet.NormalizeAddress(o.Token), wallet.NormalizeAddress(o.PoolAddress), o.Dex,
		nullWei(o.SpendWei), nullWei(o.MinOutWei), o.TxHash, o.Status, o.Error, o.DryRun, o.SignalTx)
	return err
}

func (s *Store) ListMemeOrders(ctx context.Context, limit int) ([]MemeOrder, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, source, token, pool_address, dex, spend_wei::text, min_out_wei::text,
       tx_hash, status, error, dry_run, signal_tx, created_at
FROM meme_orders ORDER BY created_at DESC LIMIT $1
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemeOrder
	for rows.Next() {
		var o MemeOrder
		if err := rows.Scan(&o.ID, &o.Source, &o.Token, &o.PoolAddress, &o.Dex, &o.SpendWei, &o.MinOutWei,
			&o.TxHash, &o.Status, &o.Error, &o.DryRun, &o.SignalTx, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if out == nil {
		out = []MemeOrder{}
	}
	return out, rows.Err()
}

func (s *Store) HasLiveMemeBuy(ctx context.Context, token string) (bool, error) {
	token = wallet.NormalizeAddress(token)
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM meme_orders
WHERE token = $1 AND dry_run = FALSE AND status IN ('broadcast', 'confirmed')
`, token).Scan(&n)
	return n > 0, err
}

func (s *Store) InsertOrder(ctx context.Context, o MintOrder) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO mint_orders(source, collection, quantity, value_wei, fee_recipient, signal_tx, tx_hash, status, error, dry_run, signer)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
`, o.Source, o.Collection, o.Quantity, nullWei(o.ValueWei), o.FeeRecipient, o.SignalTx, o.TxHash, o.Status, o.Error, o.DryRun, wallet.NormalizeAddress(o.Signer))
	return err
}

func (s *Store) HasLiveMintForCollectionWallet(ctx context.Context, collection, signer string) (bool, error) {
	collection = wallet.NormalizeAddress(collection)
	signer = wallet.NormalizeAddress(signer)
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM mint_orders
WHERE collection = $1 AND signer = $2
  AND dry_run = FALSE
  AND status IN ('broadcast', 'confirmed')
`, collection, signer).Scan(&n)
	return n > 0, err
}

func (s *Store) SumMintedQuantityForCollection(ctx context.Context, collection string) (int, error) {
	collection = wallet.NormalizeAddress(collection)
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(quantity), 0) FROM mint_orders
WHERE collection = $1
  AND status IN ('broadcast', 'confirmed', 'dry_run_ok')
`, collection).Scan(&n)
	return n, err
}

// HasLiveMintForCollection reports whether we already broadcast a live mint for this collection (any signer).
func (s *Store) HasLiveMintForCollection(ctx context.Context, collection string) (bool, error) {
	collection = wallet.NormalizeAddress(collection)
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM mint_orders
WHERE collection = $1
  AND dry_run = FALSE
  AND status IN ('broadcast', 'confirmed')
`, collection).Scan(&n)
	return n > 0, err
}

func (s *Store) CountDistinctWatchedActors(ctx context.Context, collection string, sides []string, window time.Duration) (int, error) {
	collection = wallet.NormalizeAddress(collection)
	if window <= 0 {
		window = 2 * time.Hour
	}
	_ = sides // always mint+buy consensus across both paths
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT t.wallet) FROM nft_trades t
INNER JOIN wallets w ON w.address = t.wallet AND w.active = TRUE AND w.source IN ('curated','discovered')
WHERE t.collection = $1
  AND t.side IN ('mint', 'buy')
  AND t.created_at >= NOW() - ($2 * INTERVAL '1 second')
`, collection, int64(window.Seconds())).Scan(&n)
	return n, err
}

func (s *Store) FirstWatchedActivityAt(ctx context.Context, collection string, sides []string) (*time.Time, error) {
	collection = wallet.NormalizeAddress(collection)
	_ = sides
	var t sql.NullTime
	err := s.db.QueryRowContext(ctx, `
SELECT MIN(t.created_at) FROM nft_trades t
INNER JOIN wallets w ON w.address = t.wallet AND w.active = TRUE AND w.source IN ('curated','discovered')
WHERE t.collection = $1 AND t.side IN ('mint', 'buy')
`, collection).Scan(&t)
	if err != nil {
		return nil, err
	}
	if !t.Valid {
		return nil, nil
	}
	tt := t.Time
	return &tt, nil
}

func (s *Store) TryMarkCollectionAlert(ctx context.Context, collection, kind string) (bool, error) {
	collection = wallet.NormalizeAddress(collection)
	kind = strings.ToLower(strings.TrimSpace(kind))
	if collection == "" || kind == "" {
		return false, nil
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO collection_alerts(collection, kind) VALUES ($1,$2)
ON CONFLICT (collection, kind) DO NOTHING
`, collection, kind)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// HasProcessedSignalTx reports whether a copy-mint signal was already handled (any outcome that consumed it).
func (s *Store) HasProcessedSignalTx(ctx context.Context, signalTx string) (bool, error) {
	signalTx = strings.ToLower(strings.TrimSpace(signalTx))
	if signalTx == "" {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM mint_orders
WHERE signal_tx = $1
  AND status NOT IN ('rejected', 'skipped_duplicate')
`, signalTx).Scan(&n)
	return n > 0, err
}

func nullWei(v string) string {
	if v == "" {
		return "0"
	}
	return v
}

func (s *Store) ListOrders(ctx context.Context, limit int) ([]MintOrder, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, source, collection, quantity, value_wei::text, fee_recipient, signal_tx, tx_hash, status, error, dry_run, COALESCE(signer,''), created_at
FROM mint_orders ORDER BY created_at DESC LIMIT $1
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MintOrder
	for rows.Next() {
		var o MintOrder
		if err := rows.Scan(&o.ID, &o.Source, &o.Collection, &o.Quantity, &o.ValueWei, &o.FeeRecipient, &o.SignalTx, &o.TxHash, &o.Status, &o.Error, &o.DryRun, &o.Signer, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if out == nil {
		out = []MintOrder{}
	}
	return out, rows.Err()
}
