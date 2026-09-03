package ingest

import (
	"context"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/xlaez/bot33/internal/alert"
	"github.com/xlaez/bot33/internal/classify"
	"github.com/xlaez/bot33/internal/enrich"
	"github.com/xlaez/bot33/internal/store"
	"github.com/xlaez/bot33/internal/wallet"
)

const cursorName = "nft_logs"

type Watcher struct {
	client       *ethclient.Client
	store        *store.Store
	enricher     *enrich.Enricher
	telegram     *alert.Telegram
	log          *slog.Logger
	alertOnSell  bool
	pollInterval time.Duration
	startLag     uint64

	mu    sync.RWMutex
	watch map[string]wallet.Record
}

func New(
	client *ethclient.Client,
	st *store.Store,
	en *enrich.Enricher,
	tg *alert.Telegram,
	log *slog.Logger,
	alertOnSell bool,
	pollInterval time.Duration,
	startLag uint64,
) *Watcher {
	return &Watcher{
		client:       client,
		store:        st,
		enricher:     en,
		telegram:     tg,
		log:          log,
		alertOnSell:  alertOnSell,
		pollInterval: pollInterval,
		startLag:     startLag,
		watch:        map[string]wallet.Record{},
	}
}

func (w *Watcher) ReloadWatchSet(ctx context.Context) error {
	rows, err := w.store.ListActiveWatchSet(ctx)
	if err != nil {
		return err
	}
	next := make(map[string]wallet.Record, len(rows))
	for _, r := range rows {
		next[wallet.NormalizeAddress(r.Address)] = r
	}
	w.mu.Lock()
	w.watch = next
	w.mu.Unlock()
	w.log.Info("watch set loaded", "count", len(next))
	return nil
}

func (w *Watcher) snapshotWatch() map[string]struct{} {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make(map[string]struct{}, len(w.watch))
	for a := range w.watch {
		out[a] = struct{}{}
	}
	return out
}

func (w *Watcher) watchTopicHashes() []common.Hash {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]common.Hash, 0, len(w.watch))
	for a := range w.watch {
		out = append(out, common.HexToHash(a))
	}
	return out
}

func (w *Watcher) lookup(addr string) (wallet.Record, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	r, ok := w.watch[wallet.NormalizeAddress(addr)]
	return r, ok
}

func (w *Watcher) Run(ctx context.Context) error {
	if err := w.ReloadWatchSet(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	reload := time.NewTicker(2 * time.Minute)
	defer reload.Stop()

	for {
		if err := w.pollOnce(ctx); err != nil {
			w.log.Error("poll failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-reload.C:
			if err := w.ReloadWatchSet(ctx); err != nil {
				w.log.Error("reload watch set", "err", err)
			}
		case <-ticker.C:
		}
	}
}

func (w *Watcher) pollOnce(ctx context.Context) error {
	addrs := w.watchTopicHashes()
	if len(addrs) == 0 {
		return nil
	}

	head, err := w.client.BlockNumber(ctx)
	if err != nil {
		return err
	}
	safe := head
	if w.startLag > 0 && head > w.startLag {
		safe = head - w.startLag
	}
	from, err := w.store.GetCursor(ctx, cursorName)
	if err != nil {
		return err
	}
	if from == 0 {
		from = safe
		if from > 5 {
			from = from - 5
		}
	} else {
		from = from + 1
	}
	if from > safe {
		return nil
	}

	const maxSpan = uint64(500)
	to := from + maxSpan
	if to > safe {
		to = safe
	}

	logs, err := w.fetchWatchedLogs(ctx, from, to, addrs)
	if err != nil {
		if to > from {
			mid := from + (to-from)/2
			if mid == from {
				return err
			}
			if err1 := w.processRange(ctx, from, mid, addrs); err1 != nil {
				return err1
			}
			return w.processRange(ctx, mid+1, to, addrs)
		}
		return err
	}
	if err := w.handleLogs(ctx, logs); err != nil {
		return err
	}
	if err := w.store.SetCursor(ctx, cursorName, to); err != nil {
		return err
	}
	w.log.Debug("polled", "from", from, "to", to, "logs", len(logs))
	return nil
}

func (w *Watcher) processRange(ctx context.Context, from, to uint64, addrs []common.Hash) error {
	logs, err := w.fetchWatchedLogs(ctx, from, to, addrs)
	if err != nil {
		return err
	}
	if err := w.handleLogs(ctx, logs); err != nil {
		return err
	}
	return w.store.SetCursor(ctx, cursorName, to)
}

func (w *Watcher) fetchWatchedLogs(ctx context.Context, from, to uint64, addrs []common.Hash) ([]types.Log, error) {
	var all []types.Log

	// ERC-721 Transfer: topics[0]=sig, [1]=from, [2]=to, [3]=tokenId
	recv721 := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(from),
		ToBlock:   new(big.Int).SetUint64(to),
		Topics: [][]common.Hash{
			{classify.TransferTopic},
			nil,
			addrs,
		},
	}
	logs, err := w.client.FilterLogs(ctx, recv721)
	if err != nil {
		return nil, err
	}
	all = append(all, logs...)

	if w.alertOnSell {
		sent721 := ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(from),
			ToBlock:   new(big.Int).SetUint64(to),
			Topics: [][]common.Hash{
				{classify.TransferTopic},
				addrs,
			},
		}
		logs, err = w.client.FilterLogs(ctx, sent721)
		if err != nil {
			return nil, err
		}
		all = append(all, logs...)
	}

	// ERC-1155 TransferSingle: [0]=sig, [1]=operator, [2]=from, [3]=to
	recv1155 := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(from),
		ToBlock:   new(big.Int).SetUint64(to),
		Topics: [][]common.Hash{
			{classify.TransferSingleTopic},
			nil,
			nil,
			addrs,
		},
	}
	logs, err = w.client.FilterLogs(ctx, recv1155)
	if err != nil {
		return nil, err
	}
	all = append(all, logs...)

	return all, nil
}

func (w *Watcher) handleLogs(ctx context.Context, logs []types.Log) error {
	watch := w.snapshotWatch()
	if len(watch) == 0 {
		return nil
	}
	for _, lg := range logs {
		ev, ok := classify.ParseLog(lg)
		if !ok {
			continue
		}
		matched, action, ok := classify.MatchWatch(ev, watch, w.alertOnSell)
		if !ok {
			continue
		}
		first, err := w.store.MarkSeen(ctx, strings.ToLower(ev.TxHash.Hex()), ev.LogIndex)
		if err != nil {
			return err
		}
		if !first {
			continue
		}
		rec, ok := w.lookup(matched)
		if !ok {
			continue
		}
		name := w.enricher.CollectionName(ctx, ev.Collection)
		tokenID := "0"
		if ev.TokenID != nil {
			tokenID = ev.TokenID.String()
		}
		side := string(action)
		_ = w.store.InsertTrade(ctx, matched, ev.Collection.Hex(), tokenID, side, strings.ToLower(ev.TxHash.Hex()), ev.BlockNumber, "0")

		w.log.Info("nft event",
			"action", action,
			"label", rec.Label,
			"wallet", matched,
			"collection", name,
			"token_id", tokenID,
			"tx", ev.TxHash.Hex(),
		)

		if w.telegram != nil && w.telegram.Enabled() {
			if err := w.telegram.Send(ctx, alert.Payload{
				Wallet:     rec,
				Event:      ev,
				Action:     action,
				Collection: name,
			}); err != nil {
				w.log.Error("telegram send failed", "err", err, "tx", ev.TxHash.Hex())
			}
		}
	}
	return nil
}
