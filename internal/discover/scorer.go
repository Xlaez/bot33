package discover

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/xlaez/bot33/internal/store"
	"github.com/xlaez/bot33/internal/wallet"
)

type Scorer struct {
	store     *store.Store
	log       *slog.Logger
	minScore  float64
	minTrades int
	interval  time.Duration
}

func New(st *store.Store, log *slog.Logger, minScore float64, minTrades int, interval time.Duration) *Scorer {
	return &Scorer{store: st, log: log, minScore: minScore, minTrades: minTrades, interval: interval}
}

func (s *Scorer) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
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
	wallet       string
	trades       int
	collections  int
	buyCount     int
	sellCount    int
	mintCount    int
	lastActivity time.Time
}

func (s *Scorer) tick(ctx context.Context) error {
	rows, err := s.store.DB().QueryContext(ctx, `
SELECT wallet,
       COUNT(*) AS trades,
       COUNT(DISTINCT collection) AS collections,
       COUNT(*) FILTER (WHERE side = 'buy') AS buys,
       COUNT(*) FILTER (WHERE side = 'sell') AS sells,
       COUNT(*) FILTER (WHERE side = 'mint') AS mints,
       MAX(created_at) AS last_activity
FROM nft_trades
GROUP BY wallet
`)
	if err != nil {
		return err
	}
	defer rows.Close()

	promoted := 0
	for rows.Next() {
		var st walletStats
		var last sql.NullTime
		if err := rows.Scan(&st.wallet, &st.trades, &st.collections, &st.buyCount, &st.sellCount, &st.mintCount, &last); err != nil {
			return err
		}
		if last.Valid {
			st.lastActivity = last.Time
		}
		if IsBotLike(st) {
			_ = s.store.UpsertWallet(ctx, wallet.Record{
				Address: st.wallet,
				Label:   "blocked-botlike",
				Source:  wallet.SourceBlocked,
				Tags:    []string{"bot-filter"},
				Active:  false,
				Score:   0,
			})
			continue
		}
		score := Score(st)
		if st.trades < s.minTrades || score < s.minScore {
			continue
		}
		rec := wallet.Record{
			Address: st.wallet,
			Label:   fmt.Sprintf("discovered-%.0f", score),
			Source:  wallet.SourceDiscovered,
			Tags:    []string{"auto-discovered", "rhc-nft"},
			Score:   score,
			Active:  true,
			Evidence: []string{
				fmt.Sprintf("trades=%d collections=%d buys=%d mints=%d", st.trades, st.collections, st.buyCount, st.mintCount),
			},
		}
		if err := s.store.UpsertWallet(ctx, rec); err != nil {
			return err
		}
		promoted++
	}
	s.log.Info("discovery tick complete", "promoted_or_updated", promoted)
	return rows.Err()
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
		base += hit * 20
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
