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
	"github.com/xlaez/bot33/internal/signal"
	"github.com/xlaez/bot33/internal/store"
	"github.com/xlaez/bot33/internal/wallet"
)

const cursorName = "seaport_sales"

type Options struct {
	Enabled         bool
	Interval        time.Duration
	StartLag        uint64
	AlertOnSell     bool
	MinPriceWei     string
	NotifyMinScore  float64
	HeatWindow      time.Duration
	HeatMinSales    int
	PremiumMultiple float64
}

type Poller struct {
	client   *ethclient.Client
	store    *store.Store
	log      *slog.Logger
	telegram *alert.Telegram
	enricher *enrich.Enricher
	address  common.Address
	opts     Options
	minWei   *big.Int

	mu     sync.RWMutex
	watch  map[string]wallet.Record
	colls  map[string]store.Collection
}

func New(
	client *ethclient.Client,
	st *store.Store,
	log *slog.Logger,
	tg *alert.Telegram,
	en *enrich.Enricher,
	opts Options,
) *Poller {
	minWei := big.NewInt(0)
	if opts.MinPriceWei != "" && opts.MinPriceWei != "0" {
		if v, ok := new(big.Int).SetString(opts.MinPriceWei, 10); ok {
			minWei = v
		}
	}
	if opts.Interval <= 0 {
		opts.Interval = 8 * time.Second
	}
	if opts.HeatWindow <= 0 {
		opts.HeatWindow = 30 * time.Minute
	}
	if opts.HeatMinSales <= 0 {
		opts.HeatMinSales = 3
	}
	if opts.NotifyMinScore <= 0 {
		opts.NotifyMinScore = 60
	}
	if opts.PremiumMultiple <= 0 {
		opts.PremiumMultiple = 1.5
	}
	return &Poller{
		client:   client,
		store:    st,
		log:      log,
		telegram: tg,
		enricher: en,
		address:  seaport.Address,
		opts:     opts,
		minWei:   minWei,
		watch:    map[string]wallet.Record{},
		colls:    map[string]store.Collection{},
	}
}

func (p *Poller) reload(ctx context.Context) error {
	wallets, err := p.store.ListActiveWatchSet(ctx)
	if err != nil {
		return err
	}
	nextW := make(map[string]wallet.Record, len(wallets))
	for _, r := range wallets {
		nextW[wallet.NormalizeAddress(r.Address)] = r
	}
	colls, err := p.store.ListActiveCollections(ctx)
	if err != nil {
		return err
	}
	nextC := make(map[string]store.Collection, len(colls))
	for _, c := range colls {
		nextC[wallet.NormalizeAddress(c.Address)] = c
	}
	p.mu.Lock()
	p.watch = nextW
	p.colls = nextC
	p.mu.Unlock()
	p.log.Info("marketplace watch reloaded", "wallets", len(nextW), "collections", len(nextC))
	return nil
}

func (p *Poller) lookupWallet(addr common.Address) (wallet.Record, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	r, ok := p.watch[wallet.NormalizeAddress(addr.Hex())]
	return r, ok
}

func (p *Poller) trackedCollection(addr common.Address) (store.Collection, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	c, ok := p.colls[wallet.NormalizeAddress(addr.Hex())]
	return c, ok
}

func (p *Poller) Run(ctx context.Context) error {
	if !p.opts.Enabled {
		p.log.Info("marketplace poller disabled")
		<-ctx.Done()
		return ctx.Err()
	}
	_ = p.reload(ctx)
	p.log.Info("marketplace poller started",
		"seaport", p.address.Hex(),
		"notify_min_score", p.opts.NotifyMinScore,
		"heat_window", p.opts.HeatWindow.String(),
		"heat_min_sales", p.opts.HeatMinSales,
	)
	ticker := time.NewTicker(p.opts.Interval)
	defer ticker.Stop()
	reload := time.NewTicker(2 * time.Minute)
	defer reload.Stop()
	backoff := p.opts.Interval
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
			backoff = p.opts.Interval
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-reload.C:
			if err := p.reload(ctx); err != nil {
				p.log.Error("marketplace reload", "err", err)
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
	if p.opts.StartLag > 0 && head > p.opts.StartLag {
		safe = head - p.opts.StartLag
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
		n, err := p.decideAndAlert(ctx, sale)
		if err != nil {
			p.log.Warn("signal evaluate", "err", err, "tx", sale.TxHash.Hex())
		}
		alerted += n
	}
	if written > 0 {
		p.log.Info("marketplace sales ingested", "rows", written, "alerts", alerted, "from", from, "to", to)
	}
	return p.store.SetCursor(ctx, cursorName, to)
}

func (p *Poller) decideAndAlert(ctx context.Context, sale *seaport.Sale) (int, error) {
	if p.telegram == nil || !p.telegram.Enabled() {
		return 0, nil
	}
	stats, err := p.store.CollectionSaleStats(ctx, sale.Collection.Hex(), p.opts.HeatWindow)
	if err != nil {
		return 0, err
	}
	_, tracked := p.trackedCollection(sale.Collection)
	buyer, buyerWatched := p.lookupWallet(sale.Buyer)
	seller, sellerWatched := p.lookupWallet(sale.Seller)

	sent := 0
	buyDec := signal.Evaluate(signal.Input{
		Side:            "buy",
		PriceWei:        sale.PriceWei,
		BuyerWatched:    buyerWatched,
		BuyerSource:     buyer.Source,
		TrackedColl:     tracked,
		CollStats:       stats,
		MinPriceWei:     p.minWei,
		NotifyMinScore:  p.opts.NotifyMinScore,
		HeatMinSales:    p.opts.HeatMinSales,
		PremiumMultiple: p.opts.PremiumMultiple,
	})
	if buyDec.Notify {
		if err := p.sendSaleAlert(ctx, sale, "buy", buyDec, buyer, buyerWatched); err != nil {
			p.log.Error("telegram send failed", "err", err, "tx", sale.TxHash.Hex(), "side", "buy")
		} else {
			sent++
		}
		// Auto-track hot collections so future sales inherit COLLECTION boost.
		if !tracked && (containsReason(buyDec.Reasons, "heat") || containsReason(buyDec.Reasons, "heat-surge")) {
			name := ""
			if p.enricher != nil {
				name = p.enricher.CollectionName(ctx, sale.Collection)
			}
			_ = p.store.UpsertCollection(ctx, store.Collection{
				Address: sale.Collection.Hex(),
				Name:    name,
				Notes:   "auto-tracked from marketplace heat",
				Source:  "hot",
				Active:  true,
			})
			p.mu.Lock()
			p.colls[wallet.NormalizeAddress(sale.Collection.Hex())] = store.Collection{
				Address: wallet.NormalizeAddress(sale.Collection.Hex()),
				Name:    name,
				Source:  "hot",
				Active:  true,
			}
			p.mu.Unlock()
		}
	}

	if p.opts.AlertOnSell && sellerWatched {
		sellDec := signal.Evaluate(signal.Input{
			Side:           "sell",
			PriceWei:       sale.PriceWei,
			SellerWatched:  true,
			SellerSource:   seller.Source,
			MinPriceWei:    p.minWei,
			NotifyMinScore: p.opts.NotifyMinScore,
		})
		if sellDec.Notify {
			if err := p.sendSaleAlert(ctx, sale, "sell", sellDec, seller, true); err != nil {
				p.log.Error("telegram send failed", "err", err, "tx", sale.TxHash.Hex(), "side", "sell")
			} else {
				sent++
			}
		}
	}
	return sent, nil
}

func containsReason(reasons []string, needle string) bool {
	for _, r := range reasons {
		if r == needle || strings.HasPrefix(r, needle) {
			return true
		}
	}
	return false
}

func (p *Poller) sendSaleAlert(ctx context.Context, sale *seaport.Sale, side string, dec signal.Decision, rec wallet.Record, watched bool) error {
	name := sale.Collection.Hex()
	if p.enricher != nil {
		name = p.enricher.CollectionName(ctx, sale.Collection)
	}
	if c, ok := p.trackedCollection(sale.Collection); ok && c.Name != "" {
		name = c.Name
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
	party := sale.Buyer
	if side == "sell" {
		party = sale.Seller
	}
	label := rec.Label
	if !watched || label == "" {
		label = wallet.NormalizeAddress(party.Hex())
	}
	msg := fmt.Sprintf(
		"[%s score=%.0f] %s %s NFT (seaport)\ncollection: %s #%s\nprice: %s ETH\nwhy: %s\nwallet: %s\ntx: %s",
		dec.Tag,
		dec.Score,
		label,
		side,
		name,
		tokenID,
		priceEth,
		signal.FormatReasons(dec.Reasons),
		wallet.NormalizeAddress(party.Hex()),
		chain.ExplorerTx(sale.TxHash.Hex()),
	)
	if err := p.telegram.SendText(ctx, msg); err != nil {
		return err
	}
	p.log.Info("telegram alert sent",
		"side", side,
		"tag", dec.Tag,
		"score", dec.Score,
		"collection", wallet.NormalizeAddress(sale.Collection.Hex()),
		"tx", sale.TxHash.Hex(),
	)
	return nil
}
