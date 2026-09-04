package marketplace

import (
	"context"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/xlaez/bot33/internal/seaport"
	"github.com/xlaez/bot33/internal/store"
)

const cursorName = "seaport_sales"

type Poller struct {
	client   *ethclient.Client
	store    *store.Store
	log      *slog.Logger
	address  common.Address
	interval time.Duration
	startLag uint64
	enabled  bool
}

func New(client *ethclient.Client, st *store.Store, log *slog.Logger, enabled bool, interval time.Duration, startLag uint64) *Poller {
	return &Poller{
		client:   client,
		store:    st,
		log:      log,
		address:  seaport.Address,
		interval: interval,
		startLag: startLag,
		enabled:  enabled,
	}
}

func (p *Poller) Run(ctx context.Context) error {
	if !p.enabled {
		p.log.Info("marketplace poller disabled")
		<-ctx.Done()
		return ctx.Err()
	}
	p.log.Info("marketplace poller started", "seaport", p.address.Hex())
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	backoff := p.interval
	for {
		if err := p.pollOnce(ctx); err != nil {
			p.log.Error("marketplace poll failed", "err", err, "backoff", backoff.String())
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			if backoff < 60*time.Second {
				backoff *= 2
			}
		} else {
			backoff = p.interval
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (p *Poller) pollOnce(ctx context.Context) error {
	head, err := p.client.BlockNumber(ctx)
	if err != nil {
		return err
	}
	safe := head
	if p.startLag > 0 && head > p.startLag {
		safe = head - p.startLag
	}
	from, err := p.store.GetCursor(ctx, cursorName)
	if err != nil {
		return err
	}
	if from == 0 {
		from = safe
		if from > 64 {
			from -= 64
		}
	} else {
		from++
	}
	const maxLag = uint64(5_000)
	if from+maxLag < safe {
		jump := safe - 128
		p.log.Warn("marketplace cursor far behind, jumping", "was", from-1, "jump_to", jump)
		from = jump
		_ = p.store.SetCursor(ctx, cursorName, from-1)
	}
	if from > safe {
		return nil
	}
	const maxSpan = uint64(400)
	to := from + maxSpan
	if to > safe {
		to = safe
	}
	q := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(from),
		ToBlock:   new(big.Int).SetUint64(to),
		Addresses: []common.Address{p.address},
		Topics:    [][]common.Hash{{seaport.OrderFulfilledTopic}},
	}
	logs, err := p.client.FilterLogs(ctx, q)
	if err != nil {
		return err
	}
	written := 0
	for _, lg := range logs {
		sale, ok, err := seaport.ParseOrderFulfilled(lg)
		if err != nil {
			p.log.Warn("seaport decode", "err", err, "tx", lg.TxHash.Hex())
			continue
		}
		if !ok {
			continue
		}
		first, err := p.store.MarkSeen(ctx, strings.ToLower(sale.TxHash.Hex()), sale.LogIndex)
		if err != nil {
			return err
		}
		if !first {
			continue
		}
		tokenID := "0"
		if sale.TokenID != nil {
			tokenID = sale.TokenID.String()
		}
		price := "0"
		if sale.PriceWei != nil {
			price = sale.PriceWei.String()
		}
		if sale.Buyer != (common.Address{}) {
			cp := ""
			if sale.Seller != (common.Address{}) {
				cp = sale.Seller.Hex()
			}
			if err := p.store.InsertTrade(ctx, sale.Buyer.Hex(), sale.Collection.Hex(), tokenID, "buy", strings.ToLower(sale.TxHash.Hex()), sale.BlockNumber, price, cp, "seaport"); err != nil {
				return err
			}
			written++
		}
		if sale.Seller != (common.Address{}) {
			cp := ""
			if sale.Buyer != (common.Address{}) {
				cp = sale.Buyer.Hex()
			}
			if err := p.store.InsertTrade(ctx, sale.Seller.Hex(), sale.Collection.Hex(), tokenID, "sell", strings.ToLower(sale.TxHash.Hex()), sale.BlockNumber, price, cp, "seaport"); err != nil {
				return err
			}
			written++
		}
	}
	if written > 0 {
		p.log.Info("marketplace sales ingested", "rows", written, "from", from, "to", to)
	}
	return p.store.SetCursor(ctx, cursorName, to)
}
