package discover

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/xlaez/bot33/internal/store"
	"github.com/xlaez/bot33/internal/wallet"
)

type Options struct {
	MinScore  float64
	MinTrades int
	TopN      int
	Window    time.Duration
	Interval  time.Duration
}

type Scorer struct {
	store *store.Store
	log   *slog.Logger
	opts  Options
}

func New(st *store.Store, log *slog.Logger, opts Options) *Scorer {
	if opts.Window <= 0 {
		opts.Window = 30 * 24 * time.Hour
	}
	if opts.TopN <= 0 {
		opts.TopN = 40
	}
	if opts.MinTrades <= 0 {
		opts.MinTrades = 5
	}
	if opts.Interval <= 0 {
		opts.Interval = 5 * time.Minute
	}
	return &Scorer{store: st, log: log, opts: opts}
}

func (s *Scorer) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.opts.Interval)
	defer ticker.Stop()
	if err := s.tick(ctx); err != nil {
		s.log.Error("discovery tick failed", "err", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.tick(ctx); err != nil {
				s.log.Error("discovery tick failed", "err", err)
			}
		}
	}
}

type walletStats struct {
	wallet         string
	trades         int
	collections    int
	buyCount       int
	sellCount      int
	mintCount      int
	paidTrades     int
	volumeWei      *big.Int
	uniqueCounter  int
	reciprocalHits int
	zeroValueRatio float64
	sameTokenFlip  int
	lastActivity   time.Time
}

func (s *Scorer) tick(ctx context.Context) error {
	stats, err := s.loadWindowStats(ctx)
	if err != nil {
		return err
	}
	wash, err := s.loadWashSignals(ctx)
	if err != nil {
		return err
	}
	for addr, w := range wash {
		st, ok := stats[addr]
		if !ok {
			continue
		}
		st.reciprocalHits = w.reciprocal
		st.sameTokenFlip = w.flips
		st.uniqueCounter = w.counterparties
		if st.trades > 0 {
			st.zeroValueRatio = float64(st.trades-st.paidTrades) / float64(st.trades)
		}
		stats[addr] = st
	}

	type ranked struct {
		addr  string
		st    walletStats
		score float64
	}
	var candidates []ranked
	blocked := 0
	for addr, st := range stats {
		if reason := WashReason(st); reason != "" {
			_ = s.store.UpsertWallet(ctx, wallet.Record{
				Address:  addr,
				Label:    "blocked-wash",
				Source:   wallet.SourceBlocked,
				Tags:     []string{"wash-filter", reason},
				Active:   false,
				Score:    0,
				Evidence: []string{reason},
			})
			blocked++
			continue
		}
		if IsBotLike(st) {
			_ = s.store.UpsertWallet(ctx, wallet.Record{
				Address:  addr,
				Label:    "blocked-botlike",
				Source:   wallet.SourceBlocked,
				Tags:     []string{"bot-filter"},
				Active:   false,
				Score:    0,
			})
			blocked++
			continue
		}
		score := Score(st)
		if st.trades < s.opts.MinTrades || score < s.opts.MinScore {
			continue
		}
		candidates = append(candidates, ranked{addr: addr, st: st, score: score})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			vi := candidates[i].st.volumeWei
			vj := candidates[j].st.volumeWei
			if vi == nil {
				vi = big.NewInt(0)
			}
			if vj == nil {
				vj = big.NewInt(0)
			}
			return vi.Cmp(vj) > 0
		}
		return candidates[i].score > candidates[j].score
	})

	if len(candidates) > s.opts.TopN {
		candidates = candidates[:s.opts.TopN]
	}

	keep := make([]wallet.Record, 0, len(candidates))
	for _, c := range candidates {
		vol := "0"
		if c.st.volumeWei != nil {
			vol = c.st.volumeWei.String()
		}
		keep = append(keep, wallet.Record{
			Address: c.addr,
			Label:   fmt.Sprintf("discovered-%.0f", c.score),
			Source:  wallet.SourceDiscovered,
			Tags:    []string{"auto-discovered", "rhc-nft", "windowed"},
			Score:   c.score,
			Active:  true,
			Evidence: []string{
				fmt.Sprintf("window=%s trades=%d collections=%d buys=%d mints=%d paid=%d vol_wei=%s",
					s.opts.Window, c.st.trades, c.st.collections, c.st.buyCount, c.st.mintCount, c.st.paidTrades, vol),
			},
		})
	}

	promoted, demoted, err := s.store.SyncDiscoveredWatchSet(ctx, keep)
	if err != nil {
		return err
	}
	s.log.Info("discovery tick complete",
		"window", s.opts.Window.String(),
		"top_n", s.opts.TopN,
		"wallets_scored", len(stats),
		"candidates", len(candidates),
		"promoted_or_updated", promoted,
		"demoted", demoted,
		"blocked", blocked,
	)
	return nil
}

func (s *Scorer) loadWindowStats(ctx context.Context) (map[string]walletStats, error) {
	rows, err := s.store.DB().QueryContext(ctx, `
SELECT wallet,
       COUNT(DISTINCT tx_hash) AS trades,
       COUNT(DISTINCT collection) AS collections,
       COUNT(*) FILTER (WHERE side = 'buy') AS buys,
       COUNT(*) FILTER (WHERE side = 'sell') AS sells,
       COUNT(*) FILTER (WHERE side = 'mint') AS mints,
       COUNT(*) FILTER (WHERE value_wei > 0) AS paid,
       COALESCE(SUM(value_wei), 0)::text AS volume_wei,
       MAX(created_at) AS last_activity
FROM nft_trades
WHERE created_at >= NOW() - ($1::text)::interval
  AND side IN ('buy', 'sell', 'mint')
GROUP BY wallet
`, formatInterval(s.opts.Window))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]walletStats)
	for rows.Next() {
		var st walletStats
		var last sql.NullTime
		var vol string
		if err := rows.Scan(&st.wallet, &st.trades, &st.collections, &st.buyCount, &st.sellCount, &st.mintCount, &st.paidTrades, &vol, &last); err != nil {
			return nil, err
		}
		st.volumeWei, _ = new(big.Int).SetString(vol, 10)
		if st.volumeWei == nil {
			st.volumeWei = big.NewInt(0)
		}
		if last.Valid {
			st.lastActivity = last.Time
		}
		if st.trades > 0 {
			st.zeroValueRatio = float64(st.trades-st.paidTrades) / float64(st.trades)
		}
		out[st.wallet] = st
	}
	return out, rows.Err()
}

type washSignals struct {
	reciprocal    int
	flips         int
	counterparties int
}

func (s *Scorer) loadWashSignals(ctx context.Context) (map[string]washSignals, error) {
	out := make(map[string]washSignals)

	rows, err := s.store.DB().QueryContext(ctx, `
WITH ranked AS (
  SELECT LEAST(wallet, counterparty) AS a,
         GREATEST(wallet, counterparty) AS b,
         COUNT(*) AS n
  FROM nft_trades
  WHERE created_at >= NOW() - ($1::text)::interval
    AND counterparty <> ''
    AND side IN ('buy', 'sell')
  GROUP BY 1, 2
  HAVING COUNT(*) >= 6
)
SELECT a, b, n FROM ranked
`, formatInterval(s.opts.Window))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a, b string
		var n int
		if err := rows.Scan(&a, &b, &n); err != nil {
			rows.Close()
			return nil, err
		}
		sa := out[a]
		sa.reciprocal += n
		out[a] = sa
		sb := out[b]
		sb.reciprocal += n
		out[b] = sb
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	flipRows, err := s.store.DB().QueryContext(ctx, `
SELECT wallet, COUNT(*) AS flips
FROM (
  SELECT wallet, collection, token_id
  FROM nft_trades
  WHERE created_at >= NOW() - ($1::text)::interval
    AND side IN ('buy', 'sell')
  GROUP BY wallet, collection, token_id
  HAVING COUNT(*) FILTER (WHERE side = 'buy') >= 1
     AND COUNT(*) FILTER (WHERE side = 'sell') >= 1
     AND COALESCE(SUM(value_wei), 0) = 0
) t
GROUP BY wallet
`, formatInterval(s.opts.Window))
	if err != nil {
		return nil, err
	}
	defer flipRows.Close()
	for flipRows.Next() {
		var addr string
		var flips int
		if err := flipRows.Scan(&addr, &flips); err != nil {
			return nil, err
		}
		st := out[addr]
		st.flips = flips
		out[addr] = st
	}

	cpRows, err := s.store.DB().QueryContext(ctx, `
SELECT wallet, COUNT(DISTINCT counterparty) AS cps
FROM nft_trades
WHERE created_at >= NOW() - ($1::text)::interval
  AND counterparty <> ''
GROUP BY wallet
`, formatInterval(s.opts.Window))
	if err != nil {
		return nil, err
	}
	defer cpRows.Close()
	for cpRows.Next() {
		var addr string
		var cps int
		if err := cpRows.Scan(&addr, &cps); err != nil {
			return nil, err
		}
		st := out[addr]
		st.counterparties = cps
		out[addr] = st
	}
	return out, nil
}

func formatInterval(d time.Duration) string {
	hours := int(d.Hours())
	if hours < 1 {
		hours = 1
	}
	return fmt.Sprintf("%d hours", hours)
}

func IsBotLike(st walletStats) bool {
	if st.trades >= 50 && st.collections <= 1 {
		return true
	}
	if st.buyCount > 0 && st.sellCount > 0 {
		ratio := float64(st.sellCount) / float64(st.buyCount+st.mintCount+1)
		if st.trades >= 30 && ratio > 0.95 && st.collections <= 2 {
			return true
		}
	}
	addr := strings.ToLower(st.wallet)
	if strings.HasPrefix(addr, "0x000000000000000000000000") {
		return true
	}
	return false
}

// WashReason returns a non-empty tag when the wallet looks like wash trading.
func WashReason(st walletStats) string {
	if st.reciprocalHits >= 8 && st.uniqueCounter > 0 && st.uniqueCounter <= 2 {
		return "reciprocal-pair"
	}
	if st.sameTokenFlip >= 5 && st.zeroValueRatio >= 0.8 {
		return "zero-value-flips"
	}
	if st.trades >= 20 && st.paidTrades == 0 && st.buyCount > 0 && st.sellCount > 0 && st.collections <= 3 {
		return "unpriced-churn"
	}
	if st.trades >= 15 && st.uniqueCounter == 1 && st.reciprocalHits >= 6 {
		return "single-counterparty"
	}
	return ""
}

func Score(st walletStats) float64 {
	if st.trades == 0 {
		return 0
	}
	base := 40.0
	base += math.Min(30, float64(st.collections)*8)
	base += math.Min(20, float64(st.mintCount)*3)
	closed := math.Min(float64(st.sellCount), float64(st.buyCount+st.mintCount))
	if st.buyCount+st.mintCount > 0 {
		hit := closed / float64(st.buyCount+st.mintCount)
		base += hit * 15
	}
	if st.paidTrades > 0 {
		base += math.Min(15, float64(st.paidTrades)*2)
	}
	if !st.lastActivity.IsZero() {
		days := time.Since(st.lastActivity).Hours() / 24
		if days <= 14 {
			base += 10
		} else if days <= 30 {
			base += 5
		}
	}
	if base > 99 {
		return 99
	}
	return base
}
