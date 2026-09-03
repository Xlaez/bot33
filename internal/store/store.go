package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
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

CREATE TABLE IF NOT EXISTS collections (
  address TEXT PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  active BOOLEAN NOT NULL DEFAULT TRUE,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`
	_, err := s.db.ExecContext(ctx, q)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

type Trade struct {
	ID          int64     `json:"id"`
	Wallet      string    `json:"wallet"`
	Collection  string    `json:"collection"`
	TokenID     string    `json:"token_id"`
	Side        string    `json:"side"`
	TxHash      string    `json:"tx_hash"`
	BlockNumber uint64    `json:"block_number"`
	ValueWei    string    `json:"value_wei"`
	CreatedAt   time.Time `json:"created_at"`
}

type Collection struct {
	Address   string    `json:"address"`
	Name      string    `json:"name"`
	Notes     string    `json:"notes"`
	Active    bool      `json:"active"`
	UpdatedAt time.Time `json:"updated_at"`
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

func (s *Store) ListTrades(ctx context.Context, limit int) ([]Trade, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, wallet, collection, token_id, side, tx_hash, block_number, value_wei::text, created_at
FROM nft_trades
ORDER BY created_at DESC
LIMIT $1
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Trade
	for rows.Next() {
		var t Trade
		if err := rows.Scan(&t.ID, &t.Wallet, &t.Collection, &t.TokenID, &t.Side, &t.TxHash, &t.BlockNumber, &t.ValueWei, &t.CreatedAt); err != nil {
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
	_, err := s.db.ExecContext(ctx, `
INSERT INTO collections(address, name, notes, active, updated_at)
VALUES ($1,$2,$3,$4,NOW())
ON CONFLICT (address) DO UPDATE SET
  name = CASE WHEN EXCLUDED.name <> '' THEN EXCLUDED.name ELSE collections.name END,
  notes = EXCLUDED.notes,
  active = EXCLUDED.active,
  updated_at = NOW()
`, c.Address, c.Name, c.Notes, c.Active)
	return err
}

func (s *Store) ListCollections(ctx context.Context) ([]Collection, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT address, name, notes, active, updated_at FROM collections ORDER BY name ASC, address ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collection
	for rows.Next() {
		var c Collection
		if err := rows.Scan(&c.Address, &c.Name, &c.Notes, &c.Active, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if out == nil {
		out = []Collection{}
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
    WHEN wallets.source = 'curated' THEN wallets.source
    WHEN wallets.source = 'blocked' THEN wallets.source
    ELSE EXCLUDED.source
  END,
  tags = EXCLUDED.tags,
  collections = EXCLUDED.collections,
  evidence = EXCLUDED.evidence,
  score = GREATEST(wallets.score, EXCLUDED.score),
  active = EXCLUDED.active,
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

func (s *Store) InsertTrade(ctx context.Context, walletAddr, collection, tokenID, side, txHash string, block uint64, valueWei string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO nft_trades(wallet, collection, token_id, side, tx_hash, block_number, value_wei)
VALUES ($1,$2,$3,$4,$5,$6,$7)
`, wallet.NormalizeAddress(walletAddr), wallet.NormalizeAddress(collection), tokenID, side, txHash, block, valueWei)
	return err
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
