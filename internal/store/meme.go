package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xlaez/bot33/internal/wallet"
)

type MemeToken struct {
	Address          string     `json:"address"`
	Symbol           string     `json:"symbol"`
	Name             string     `json:"name"`
	Decimals         int        `json:"decimals"`
	PairedWith       string     `json:"paired_with"`
	PoolAddress      string     `json:"pool_address"`
	Dex              string     `json:"dex"`
	FeeTier          int        `json:"fee_tier"`
	LaunchTx         string     `json:"launch_tx"`
	LaunchBlock      uint64     `json:"launch_block"`
	FirstLiquidityAt *time.Time `json:"first_liquidity_at,omitempty"`
	LPLocked         bool       `json:"lp_locked"`
	LPLockPct        float64    `json:"lp_lock_pct"`
	LPLockEvidence   string     `json:"lp_lock_evidence"`
	OwnerRenounced   bool       `json:"owner_renounced"`
	Score            float64    `json:"score"`
	Flags            []string   `json:"flags"`
	Status           string     `json:"status"`
	VolumeWei        string     `json:"volume_wei"`
	SwapCount        int        `json:"swap_count"`
	UniqueTraders    int        `json:"unique_traders"`
	LastSwapAt       *time.Time `json:"last_swap_at,omitempty"`
	AlertedAt        *time.Time `json:"alerted_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
	CreatedAt        time.Time  `json:"created_at"`
}

type MemePool struct {
	PoolAddress  string    `json:"pool_address"`
	Token0       string    `json:"token0"`
	Token1       string    `json:"token1"`
	MemeToken    string    `json:"meme_token"`
	QuoteToken   string    `json:"quote_token"`
	Dex          string    `json:"dex"`
	FeeTier      int       `json:"fee_tier"`
	CreatedTx    string    `json:"created_tx"`
	CreatedBlock uint64    `json:"created_block"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Store) UpsertMemePool(ctx context.Context, p MemePool) error {
	p.PoolAddress = wallet.NormalizeAddress(p.PoolAddress)
	p.Token0 = wallet.NormalizeAddress(p.Token0)
	p.Token1 = wallet.NormalizeAddress(p.Token1)
	p.MemeToken = wallet.NormalizeAddress(p.MemeToken)
	p.QuoteToken = wallet.NormalizeAddress(p.QuoteToken)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO meme_pools(pool_address, token0, token1, meme_token, quote_token, dex, fee_tier, created_tx, created_block)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (pool_address) DO NOTHING
`, p.PoolAddress, p.Token0, p.Token1, p.MemeToken, p.QuoteToken, p.Dex, p.FeeTier, strings.ToLower(p.CreatedTx), p.CreatedBlock)
	return err
}

func (s *Store) UpsertMemeToken(ctx context.Context, t MemeToken) error {
	t.Address = wallet.NormalizeAddress(t.Address)
	t.PairedWith = wallet.NormalizeAddress(t.PairedWith)
	t.PoolAddress = wallet.NormalizeAddress(t.PoolAddress)
	if t.Status == "" {
		t.Status = "watching"
	}
	if t.VolumeWei == "" {
		t.VolumeWei = "0"
	}
	flags, _ := json.Marshal(t.Flags)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO meme_tokens(
  address, symbol, name, decimals, paired_with, pool_address, dex, fee_tier,
  launch_tx, launch_block, first_liquidity_at, lp_locked, lp_lock_pct, lp_lock_evidence,
  owner_renounced, score, flags, status, volume_wei, swap_count, unique_traders,
  last_swap_at, alerted_at, updated_at
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,
  $9,$10,$11,$12,$13,$14,
  $15,$16,$17,$18,$19,$20,$21,
  $22,$23,NOW()
)
ON CONFLICT (address) DO UPDATE SET
  symbol = CASE WHEN EXCLUDED.symbol <> '' THEN EXCLUDED.symbol ELSE meme_tokens.symbol END,
  name = CASE WHEN EXCLUDED.name <> '' THEN EXCLUDED.name ELSE meme_tokens.name END,
  paired_with = CASE WHEN EXCLUDED.paired_with <> '' THEN EXCLUDED.paired_with ELSE meme_tokens.paired_with END,
  pool_address = CASE WHEN EXCLUDED.pool_address <> '' THEN EXCLUDED.pool_address ELSE meme_tokens.pool_address END,
  dex = CASE WHEN EXCLUDED.dex <> '' THEN EXCLUDED.dex ELSE meme_tokens.dex END,
  fee_tier = CASE WHEN EXCLUDED.fee_tier > 0 THEN EXCLUDED.fee_tier ELSE meme_tokens.fee_tier END,
  launch_tx = CASE WHEN meme_tokens.launch_tx = '' THEN EXCLUDED.launch_tx ELSE meme_tokens.launch_tx END,
  launch_block = CASE WHEN meme_tokens.launch_block = 0 THEN EXCLUDED.launch_block ELSE meme_tokens.launch_block END,
  first_liquidity_at = COALESCE(meme_tokens.first_liquidity_at, EXCLUDED.first_liquidity_at),
  lp_locked = EXCLUDED.lp_locked OR meme_tokens.lp_locked,
  lp_lock_pct = GREATEST(meme_tokens.lp_lock_pct, EXCLUDED.lp_lock_pct),
  lp_lock_evidence = CASE WHEN EXCLUDED.lp_lock_evidence <> '' THEN EXCLUDED.lp_lock_evidence ELSE meme_tokens.lp_lock_evidence END,
  owner_renounced = EXCLUDED.owner_renounced OR meme_tokens.owner_renounced,
  score = EXCLUDED.score,
  flags = EXCLUDED.flags,
  status = CASE
    WHEN meme_tokens.alerted_at IS NOT NULL THEN meme_tokens.status
    ELSE EXCLUDED.status
  END,
  volume_wei = GREATEST(meme_tokens.volume_wei, EXCLUDED.volume_wei),
  swap_count = GREATEST(meme_tokens.swap_count, EXCLUDED.swap_count),
  unique_traders = GREATEST(meme_tokens.unique_traders, EXCLUDED.unique_traders),
  last_swap_at = COALESCE(EXCLUDED.last_swap_at, meme_tokens.last_swap_at),
  alerted_at = COALESCE(meme_tokens.alerted_at, EXCLUDED.alerted_at),
  updated_at = NOW()
`, t.Address, t.Symbol, t.Name, t.Decimals, t.PairedWith, t.PoolAddress, t.Dex, t.FeeTier,
		strings.ToLower(t.LaunchTx), t.LaunchBlock, t.FirstLiquidityAt, t.LPLocked, t.LPLockPct, t.LPLockEvidence,
		t.OwnerRenounced, t.Score, flags, t.Status, t.VolumeWei, t.SwapCount, t.UniqueTraders,
		t.LastSwapAt, t.AlertedAt)
	return err
}

func (s *Store) MarkMemeAlerted(ctx context.Context, address string) error {
	address = wallet.NormalizeAddress(address)
	_, err := s.db.ExecContext(ctx, `
UPDATE meme_tokens SET alerted_at = NOW(), status = 'alerted', updated_at = NOW()
WHERE address = $1 AND alerted_at IS NULL
`, address)
	return err
}

func (s *Store) ListMemeTokens(ctx context.Context, maxAge time.Duration, limit int) ([]MemeToken, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if maxAge <= 0 {
		maxAge = 30 * 24 * time.Hour
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT address, symbol, name, decimals, paired_with, pool_address, dex, fee_tier,
       launch_tx, launch_block, first_liquidity_at, lp_locked, lp_lock_pct, lp_lock_evidence,
       owner_renounced, score, flags, status, volume_wei::text, swap_count, unique_traders,
       last_swap_at, alerted_at, updated_at, created_at
FROM meme_tokens
WHERE first_liquidity_at IS NULL
   OR first_liquidity_at >= NOW() - ($1::text)::interval
ORDER BY score DESC, COALESCE(first_liquidity_at, created_at) DESC
LIMIT $2
`, formatPGInterval(maxAge), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemeTokens(rows)
}

func (s *Store) ListActiveMemePools(ctx context.Context, maxAge time.Duration) ([]MemePool, error) {
	if maxAge <= 0 {
		maxAge = 30 * 24 * time.Hour
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT p.pool_address, p.token0, p.token1, p.meme_token, p.quote_token, p.dex, p.fee_tier,
       p.created_tx, p.created_block, p.created_at
FROM meme_pools p
JOIN meme_tokens t ON t.address = p.meme_token
WHERE t.first_liquidity_at IS NULL
   OR t.first_liquidity_at >= NOW() - ($1::text)::interval
`, formatPGInterval(maxAge))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemePool
	for rows.Next() {
		var p MemePool
		if err := rows.Scan(&p.PoolAddress, &p.Token0, &p.Token1, &p.MemeToken, &p.QuoteToken, &p.Dex, &p.FeeTier, &p.CreatedTx, &p.CreatedBlock, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if out == nil {
		out = []MemePool{}
	}
	return out, rows.Err()
}

func (s *Store) GetMemeToken(ctx context.Context, address string) (MemeToken, error) {
	address = wallet.NormalizeAddress(address)
	row := s.db.QueryRowContext(ctx, `
SELECT address, symbol, name, decimals, paired_with, pool_address, dex, fee_tier,
       launch_tx, launch_block, first_liquidity_at, lp_locked, lp_lock_pct, lp_lock_evidence,
       owner_renounced, score, flags, status, volume_wei::text, swap_count, unique_traders,
       last_swap_at, alerted_at, updated_at, created_at
FROM meme_tokens WHERE address = $1
`, address)
	var t MemeToken
	var flags []byte
	var first, last, alerted sql.NullTime
	err := row.Scan(
		&t.Address, &t.Symbol, &t.Name, &t.Decimals, &t.PairedWith, &t.PoolAddress, &t.Dex, &t.FeeTier,
		&t.LaunchTx, &t.LaunchBlock, &first, &t.LPLocked, &t.LPLockPct, &t.LPLockEvidence,
		&t.OwnerRenounced, &t.Score, &flags, &t.Status, &t.VolumeWei, &t.SwapCount, &t.UniqueTraders,
		&last, &alerted, &t.UpdatedAt, &t.CreatedAt,
	)
	if err != nil {
		return t, err
	}
	_ = json.Unmarshal(flags, &t.Flags)
	if first.Valid {
		t.FirstLiquidityAt = &first.Time
	}
	if last.Valid {
		t.LastSwapAt = &last.Time
	}
	if alerted.Valid {
		t.AlertedAt = &alerted.Time
	}
	return t, nil
}

func (s *Store) RecordMemeSwap(ctx context.Context, token string, volumeDelta string, at time.Time) error {
	token = wallet.NormalizeAddress(token)
	if volumeDelta == "" {
		volumeDelta = "0"
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE meme_tokens SET
  volume_wei = volume_wei + ($2::numeric),
  swap_count = swap_count + 1,
  last_swap_at = $3,
  updated_at = NOW()
WHERE address = $1
`, token, volumeDelta, at)
	return err
}

func (s *Store) MemeStats(ctx context.Context) (total, locked, alerted int, err error) {
	err = s.db.QueryRowContext(ctx, `
SELECT
  COUNT(*)::int,
  COUNT(*) FILTER (WHERE lp_locked)::int,
  COUNT(*) FILTER (WHERE alerted_at IS NOT NULL)::int
FROM meme_tokens
WHERE first_liquidity_at IS NULL OR first_liquidity_at >= NOW() - INTERVAL '30 days'
`).Scan(&total, &locked, &alerted)
	return
}

// RecordMemeSmartBuy records a watched-wallet buy. Returns true if this wallet is new for the token.
func (s *Store) RecordMemeSmartBuy(ctx context.Context, token, walletAddr, txHash, pool string) (bool, error) {
	token = wallet.NormalizeAddress(token)
	walletAddr = wallet.NormalizeAddress(walletAddr)
	if token == "" || walletAddr == "" {
		return false, nil
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO meme_smart_buys(token, wallet, tx_hash, pool_address)
VALUES ($1,$2,$3,$4)
ON CONFLICT (token, wallet) DO NOTHING
`, token, walletAddr, strings.ToLower(txHash), wallet.NormalizeAddress(pool))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) CountMemeSmartBuyers(ctx context.Context, token string, window time.Duration) (int, error) {
	token = wallet.NormalizeAddress(token)
	if window <= 0 {
		window = 6 * time.Hour
	}
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM meme_smart_buys
WHERE token = $1 AND created_at >= NOW() - ($2 * INTERVAL '1 second')
`, token, int64(window.Seconds())).Scan(&n)
	return n, err
}

func (s *Store) ListMemeSmartBuyers(ctx context.Context, token string, window time.Duration, limit int) ([]string, error) {
	token = wallet.NormalizeAddress(token)
	if window <= 0 {
		window = 6 * time.Hour
	}
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT wallet FROM meme_smart_buys
WHERE token = $1 AND created_at >= NOW() - ($2 * INTERVAL '1 second')
ORDER BY created_at ASC LIMIT $3
`, token, int64(window.Seconds()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func scanMemeTokens(rows *sql.Rows) ([]MemeToken, error) {
	var out []MemeToken
	for rows.Next() {
		var t MemeToken
		var flags []byte
		var first, last, alerted sql.NullTime
		if err := rows.Scan(
			&t.Address, &t.Symbol, &t.Name, &t.Decimals, &t.PairedWith, &t.PoolAddress, &t.Dex, &t.FeeTier,
			&t.LaunchTx, &t.LaunchBlock, &first, &t.LPLocked, &t.LPLockPct, &t.LPLockEvidence,
			&t.OwnerRenounced, &t.Score, &flags, &t.Status, &t.VolumeWei, &t.SwapCount, &t.UniqueTraders,
			&last, &alerted, &t.UpdatedAt, &t.CreatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(flags, &t.Flags)
		if t.Flags == nil {
			t.Flags = []string{}
		}
		if first.Valid {
			t.FirstLiquidityAt = &first.Time
		}
		if last.Valid {
			t.LastSwapAt = &last.Time
		}
		if alerted.Valid {
			t.AlertedAt = &alerted.Time
		}
		out = append(out, t)
	}
	if out == nil {
		out = []MemeToken{}
	}
	return out, rows.Err()
}

func formatPGInterval(d time.Duration) string {
	if d < time.Hour {
		m := int(d.Minutes())
		if m < 1 {
			m = 1
		}
		return fmt.Sprintf("%d minutes", m)
	}
	h := int(d.Hours())
	if h < 1 {
		h = 1
	}
	return fmt.Sprintf("%d hours", h)
}
