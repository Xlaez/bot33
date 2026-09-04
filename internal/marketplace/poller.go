package marketplace

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/xlaez/bot33/internal/alert"
	"github.com/xlaez/bot33/internal/chain"
	"github.com/xlaez/bot33/internal/enrich"
	"github.com/xlaez/bot33/internal/seaport"
	"github.com/xlaez/bot33/internal/store"
	"github.com/xlaez/bot33/internal/wallet"
)

const cursorName = "seaport_sales"

type Poller struct {
	client      *ethclient.Client
	store       *store.Store
	log         *slog.Logger
	telegram    *alert.Telegram
	enricher    *enrich.Enricher
	address     common.Address
	interval    time.Duration
	startLag    uint64
	enabled     bool
	alertOnSell bool
	minAlertWei *big.Int

	mu    sync.RWMutex
	watch map[string]wallet.Record
}

func New(
	client *ethclient.Client,
	st *store.Store,
	log *slog.Logger,
	tg *alert.Telegram,
	en *enrich.Enricher,
	enabled bool,
	interval time.Duration,
	startLag uint64,
	alertOnSell bool,
	minAlertWei string,
) *Poller {
	minWei := big.NewInt(0)
	if minAlertWei != "" && minAlertWei != "0" {
		if v, ok := new(big.Int).SetString(minAlertWei, 10); ok {
			minWei = v
		}
	}
	return &Poller{
		client:      client,
		store:       st,
		log:         log,
		telegram:    tg,
		enricher:    en,
		address:     seaport.Address,
		interval:    interval,
		startLag:    startLag,
		enabled:     enabled,
		alertOnSell: alertOnSell,
		minAlertWei: minWei,
		watch:       map[string]wallet.Record{},
	}
}

func (p *Poller) reloadWatch(ctx context.Context) error {
	rows, err := p.store.ListActiveWatchSet(ctx)
	if err != nil {
		return err
	}
	next := make(map[string]wallet.Record, len(rows))
	for _, r := range rows {
		next[wallet.NormalizeAddress(r.Address)] = r
	}
	p.mu.Lock()
	p.watch = next
	p.mu.Unlock()
	return nil
}

func (p *Poller) lookup(addr common.Address) (wallet.Record, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	r, ok := p.watch[wallet.NormalizeAddress(addr.Hex())]
	return r, ok
}

func (p *Poller) Run(ctx context.Context) error {
	if !p.enabled {
		p.log.Info("marketplace poller disabled")
		<-ctx.Done()
		return ctx.Err()
	}
	_ = p.reloadWatch(ctx)
	p.log.Info("marketplace poller started", "seaport", p.address.Hex())
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	reload := time.NewTicker(2 * time.Minute)
	defer reload.Stop()
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
		case <-reload.C:
			if err := p.reloadWatch(ctx); err != nil {
				p.log.Error("marketplace reload watch set", "err", err)
			}
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
	alerted := 0
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
			if p.maybeAlert(ctx, sale, "buy") {
				alerted++
			}
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
			if p.alertOnSell && p.maybeAlert(ctx, sale, "sell") {
				alerted++
			}
		}
	}
	if written > 0 {
		p.log.Info("marketplace sales ingested", "rows", written, "alerts", alerted, "from", from, "to", to)
	}
	return p.store.SetCursor(ctx, cursorName, to)
}

func (p *Poller) maybeAlert(ctx context.Context, sale *seaport.Sale, side string) bool {
	if p.telegram == nil || !p.telegram.Enabled() {
		return false
	}
	var party common.Address
	switch side {
	case "buy":
		party = sale.Buyer
	case "sell":
		party = sale.Seller
	default:
		return false
	}
	rec, watched := p.lookup(party)
	priceOk := sale.PriceWei != nil && p.minAlertWei.Sign() > 0 && sale.PriceWei.Cmp(p.minAlertWei) >= 0
	if !watched {
		if side != "buy" || !priceOk {
			return false
		}
		rec = wallet.Record{
			Address: wallet.NormalizeAddress(party.Hex()),
			Label:   "market",
			Source:  wallet.SourceDiscovered,
			Score:   0,
		}
	} else if side == "sell" && !p.alertOnSell {
		return false
	}

	name := sale.Collection.Hex()
	if p.enricher != nil {
		name = p.enricher.CollectionName(ctx, sale.Collection)
	}
	tokenID := "0"
	if sale.TokenID != nil {
		tokenID = sale.TokenID.String()
	}
	priceEth := "0"
	if sale.PriceWei != nil && sale.PriceWei.Sign() > 0 {
		f := new(big.Float).Quo(new(big.Float).SetInt(sale.PriceWei), big.NewFloat(1e18))
		priceEth = f.Text('f', 6)
	}
	tag := "[CURATED]"
	switch {
	case !watched:
		tag = "[MARKET]"
	case rec.Source == wallet.SourceDiscovered:
		tag = fmt.Sprintf("[DISCOVERED score=%.0f]", rec.Score)
	}
	label := rec.Label
	if label == "" {
		label = rec.Address
	}
	msg := fmt.Sprintf(
		"%s %s %s NFT (seaport)\ncollection: %s #%s\nprice: %s ETH\nwallet: %s\ntx: %s\nwallet link: %s",
		tag,
		label,
		side,
		name,
		tokenID,
		priceEth,
		rec.Address,
		chain.ExplorerTx(sale.TxHash.Hex()),
		chain.ExplorerAddress(rec.Address),
	)
	if err := p.telegram.SendText(ctx, msg); err != nil {
		p.log.Error("telegram send failed", "err", err, "tx", sale.TxHash.Hex(), "side", side)
		return false
	}
	p.log.Info("telegram alert sent", "side", side, "wallet", rec.Address, "watched", watched, "tx", sale.TxHash.Hex())
	return true
}
