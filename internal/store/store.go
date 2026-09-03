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
CREATE INDEX IF NOT EXISTS idx_wallets_active_source ON wallets(active, source);
`
	_, err := s.db.ExecContext(ctx, q)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
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
