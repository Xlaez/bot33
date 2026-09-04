package nftgate

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/xlaez/bot33/internal/alert"
	"github.com/xlaez/bot33/internal/chain"
	"github.com/xlaez/bot33/internal/enrich"
	"github.com/xlaez/bot33/internal/seadrop"
	"github.com/xlaez/bot33/internal/store"
	"github.com/xlaez/bot33/internal/wallet"
)

const (
	DefaultSmartWalletMin = 2
	DefaultMintWindow     = 2 * time.Hour
	DefaultBuyWindow      = 6 * time.Hour
	DefaultMaxCollectionAge = 24 * time.Hour
	DefaultMintMaxTotal   = 20
)

type SweepFn func(collection common.Address, quantity uint64, walletCount int, signalTx, label string)

type Escalator struct {
	client   *ethclient.Client
	store    *store.Store
	log      *slog.Logger
	telegram *alert.Telegram
	enricher *enrich.Enricher
	onSweep  SweepFn
}

func New(client *ethclient.Client, st *store.Store, log *slog.Logger, tg *alert.Telegram, en *enrich.Enricher, onSweep SweepFn) *Escalator {
	return &Escalator{client: client, store: st, log: log, telegram: tg, enricher: en, onSweep: onSweep}
}

type Decision struct {
	Escalate   bool
	FreeMint   bool
	Tag        string // FREE_MINT | PRIORITY | SECONDARY_SMART
	WalletCount int
	DropOpen   bool
	Reason     string
}

func (e *Escalator) EvaluateMint(ctx context.Context, collection common.Address) (Decision, error) {
	settings, err := e.store.GetSettings(ctx)
	if err != nil {
		return Decision{}, err
	}
	minW := settings.SmartWalletMin
	if minW <= 0 {
		minW = DefaultSmartWalletMin
	}
	window := time.Duration(settings.SmartMintWindowMin) * time.Minute
	if window <= 0 {
		window = DefaultMintWindow
	}
	maxAge := time.Duration(settings.NewCollectionMaxAgeH) * time.Hour
	if maxAge <= 0 {
		maxAge = DefaultMaxCollectionAge
	}

	coll := wallet.NormalizeAddress(collection.Hex())
	n, err := e.store.CountDistinctWatchedActors(ctx, coll, []string{"mint", "buy"}, window)
	if err != nil {
		return Decision{}, err
	}
	if n < minW {
		return Decision{WalletCount: n, Reason: "below_smart_wallet_min"}, nil
	}
	first, err := e.store.FirstWatchedActivityAt(ctx, coll, []string{"mint", "buy"})
	if err != nil {
		return Decision{}, err
	}
	if first != nil && time.Since(*first) > maxAge {
		return Decision{WalletCount: n, Reason: "collection_too_old"}, nil
	}

	drop, err := seadrop.FetchPublicDrop(ctx, e.client, collection)
	if err != nil {
		e.log.Warn("seadrop fetch", "err", err, "collection", coll)
	}
	now := uint64(time.Now().Unix())
	free := drop != nil && drop.IsFree() && drop.IsOpen(now)
	d := Decision{Escalate: true, WalletCount: n, FreeMint: free, DropOpen: drop != nil && drop.IsOpen(now)}
	if free {
		d.Tag = "FREE_MINT"
		d.Reason = "free_seadrop_consensus"
	} else {
		d.Tag = "PRIORITY"
		d.Reason = "smart_wallet_consensus"
	}
	return d, nil
}

func (e *Escalator) EvaluateSecondaryBuy(ctx context.Context, collection common.Address) (Decision, error) {
	settings, err := e.store.GetSettings(ctx)
	if err != nil {
		return Decision{}, err
	}
	minW := settings.SmartWalletMin
	if minW <= 0 {
		minW = DefaultSmartWalletMin
	}
	window := time.Duration(settings.SmartBuyWindowMin) * time.Minute
	if window <= 0 {
		window = DefaultBuyWindow
	}
	coll := wallet.NormalizeAddress(collection.Hex())
	n, err := e.store.CountDistinctWatchedActors(ctx, coll, []string{"buy", "mint"}, window)
	if err != nil {
		return Decision{}, err
	}
	if n < minW {
		return Decision{WalletCount: n, Reason: "below_smart_wallet_min"}, nil
	}
	drop, err := seadrop.FetchPublicDrop(ctx, e.client, collection)
	if err != nil {
		e.log.Warn("seadrop fetch", "err", err, "collection", coll)
	}
	now := uint64(time.Now().Unix())
	freeOpen := drop != nil && drop.IsFree() && drop.IsOpen(now)
	if freeOpen {
		// Prefer free-mint path; secondary tag not used while free drop is open.
		return Decision{Escalate: true, WalletCount: n, FreeMint: true, DropOpen: true, Tag: "FREE_MINT", Reason: "free_open_consensus"}, nil
	}
	tag := "SECONDARY_SMART"
	if drop != nil && drop.IsOpen(now) && !drop.IsFree() {
		tag = "PRIORITY"
	}
	return Decision{Escalate: true, WalletCount: n, FreeMint: false, DropOpen: drop != nil && drop.IsOpen(now), Tag: tag, Reason: "smart_buy_consensus"}, nil
}

func (e *Escalator) HandleMint(ctx context.Context, collection common.Address, walletAddr, label, txHash string) {
	dec, err := e.EvaluateMint(ctx, collection)
	if err != nil {
		e.log.Error("nftgate mint eval", "err", err)
		return
	}
	if !dec.Escalate {
		e.log.Info("nft event tracked", "collection", collection.Hex(), "wallets", dec.WalletCount, "reason", dec.Reason)
		return
	}
	kind := strings.ToLower(dec.Tag)
	ok, err := e.store.TryMarkCollectionAlert(ctx, collection.Hex(), kind)
	if err != nil {
		e.log.Error("collection alert mark", "err", err)
		return
	}
	if !ok {
		return
	}
	e.promoteHot(ctx, collection, dec.Tag)
	e.alert(ctx, collection, walletAddr, label, txHash, dec)

	settings, err := e.store.GetSettings(ctx)
	if err != nil {
		return
	}
	if dec.FreeMint && settings.AutoCopyMint && e.onSweep != nil {
		qty := settings.MintQuantity
		if qty == 0 {
			qty = 1
		}
		wc := settings.MintMaxWallets
		e.log.Info("auto free-mint sweep", "collection", collection.Hex(), "qty", qty, "wallets", wc)
		e.onSweep(collection, qty, wc, txHash, label)
	}
}

func (e *Escalator) HandleSecondary(ctx context.Context, collection common.Address, walletAddr, label, txHash string) bool {
	dec, err := e.EvaluateSecondaryBuy(ctx, collection)
	if err != nil {
		e.log.Error("nftgate secondary eval", "err", err)
		return false
	}
	if !dec.Escalate {
		return false
	}
	kind := strings.ToLower(dec.Tag)
	ok, err := e.store.TryMarkCollectionAlert(ctx, collection.Hex(), kind)
	if err != nil || !ok {
		return false
	}
	e.promoteHot(ctx, collection, dec.Tag)
	e.alert(ctx, collection, walletAddr, label, txHash, dec)
	if dec.FreeMint && e.onSweep != nil {
		settings, err := e.store.GetSettings(ctx)
		if err == nil && settings.AutoCopyMint {
			qty := settings.MintQuantity
			if qty == 0 {
				qty = 1
			}
			e.onSweep(collection, qty, settings.MintMaxWallets, txHash, label)
		}
	}
	return true
}

func (e *Escalator) promoteHot(ctx context.Context, collection common.Address, tag string) {
	name := ""
	if e.enricher != nil {
		name = e.enricher.CollectionName(ctx, collection)
	}
	_ = e.store.UpsertCollection(ctx, store.Collection{
		Address: collection.Hex(),
		Name:    name,
		Notes:   "priority from " + tag,
		Source:  "hot",
		Active:  true,
	})
}

func (e *Escalator) alert(ctx context.Context, collection common.Address, walletAddr, label, txHash string, dec Decision) {
	if e.telegram == nil || !e.telegram.Enabled() {
		return
	}
	name := collection.Hex()
	if e.enricher != nil {
		if n := e.enricher.CollectionName(ctx, collection); n != "" {
			name = n
		}
	}
	if label == "" {
		label = walletAddr
	}
	msg := fmt.Sprintf(
		"[%s] %d smart wallets on %s\ntrigger: %s\nwhy: %s\ncollection: %s\ntx: %s",
		dec.Tag,
		dec.WalletCount,
		name,
		label,
		dec.Reason,
		chain.ExplorerAddress(collection.Hex()),
		chain.ExplorerTx(txHash),
	)
	if err := e.telegram.SendText(ctx, msg); err != nil {
		e.log.Error("telegram priority alert", "err", err)
	}
}
