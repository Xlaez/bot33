package execute

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/xlaez/bot33/internal/alert"
	"github.com/xlaez/bot33/internal/seadrop"
	"github.com/xlaez/bot33/internal/store"
)

type SettingsView struct {
	MaxSpendWei          string   `json:"max_spend_wei"`
	MaxSpendETH          string   `json:"max_spend_eth"`
	ExecuteLive          bool     `json:"execute_live"`
	AutoCopyMint         bool     `json:"auto_copy_mint"`
	MintQuantity         uint64   `json:"mint_quantity"`
	MintMaxWallets       int      `json:"mint_max_wallets"`
	MintMaxTotal         int      `json:"mint_max_total"`
	SmartWalletMin       int      `json:"smart_wallet_min"`
	SmartMintWindowMin   int      `json:"smart_mint_window_min"`
	SmartBuyWindowMin    int      `json:"smart_buy_window_min"`
	NewCollectionMaxAgeH int      `json:"new_collection_max_age_h"`
	MemeMaxSpendWei      string   `json:"meme_max_spend_wei"`
	MemeMaxSpendETH      string   `json:"meme_max_spend_eth"`
	MemeExecuteLive      bool     `json:"meme_execute_live"`
	MemeAutoBuy          bool     `json:"meme_auto_buy"`
	MemeSlippageBps      int      `json:"meme_slippage_bps"`
	HasSigner            bool     `json:"has_signer"`
	SignerAddress        string   `json:"signer_address,omitempty"`
	SignerAddresses      []string `json:"signer_addresses,omitempty"`
	SignerCount          int      `json:"signer_count"`
}

type signer struct {
	key  *ecdsa.PrivateKey
	from common.Address
}

type Engine struct {
	client  *ethclient.Client
	store   *store.Store
	log     *slog.Logger
	tg      *alert.Telegram
	signers []signer
	chain   *big.Int

	mu       sync.Mutex
	queue    chan Job
	inflight map[string]struct{}
}

type Job struct {
	Source      string // copy|manual|sweep
	Collection  common.Address
	Quantity    uint64
	WalletCount int
	SignalTx    string
	Label       string
	FreeOnly    bool
}

func New(client *ethclient.Client, st *store.Store, log *slog.Logger, tg *alert.Telegram, privateKeyHex string, chainID int64) (*Engine, error) {
	return NewMulti(client, st, log, tg, []string{privateKeyHex}, chainID)
}

func NewMulti(client *ethclient.Client, st *store.Store, log *slog.Logger, tg *alert.Telegram, privateKeyHexes []string, chainID int64) (*Engine, error) {
	e := &Engine{
		client:   client,
		store:    st,
		log:      log,
		tg:       tg,
		chain:    big.NewInt(chainID),
		queue:    make(chan Job, 64),
		inflight: map[string]struct{}{},
	}
	seen := map[string]struct{}{}
	for _, raw := range privateKeyHexes {
		pk := strings.TrimSpace(raw)
		if pk == "" {
			continue
		}
		if strings.HasPrefix(pk, "0x") || strings.HasPrefix(pk, "0X") {
			pk = pk[2:]
		}
		key, err := crypto.HexToECDSA(pk)
		if err != nil {
			return nil, fmt.Errorf("executor private key: %w", err)
		}
		from := crypto.PubkeyToAddress(key.PublicKey)
		addr := strings.ToLower(from.Hex())
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		e.signers = append(e.signers, signer{key: key, from: from})
	}
	return e, nil
}

func (e *Engine) HasSigner() bool {
	return len(e.signers) > 0
}

func (e *Engine) SignerCount() int {
	return len(e.signers)
}

func (e *Engine) SignerAddress() string {
	if len(e.signers) == 0 {
		return ""
	}
	return e.signers[0].from.Hex()
}

func (e *Engine) SignerAddresses() []string {
	out := make([]string, 0, len(e.signers))
	for _, s := range e.signers {
		out = append(out, s.from.Hex())
	}
	return out
}

func (e *Engine) Enqueue(job Job) {
	select {
	case e.queue <- job:
	default:
		e.log.Warn("execution queue full, dropping job", "collection", job.Collection.Hex(), "source", job.Source)
	}
}

func (e *Engine) EnqueueSweep(collection common.Address, quantity uint64, walletCount int, signalTx, label string) {
	e.Enqueue(Job{
		Source:      "sweep",
		Collection:  collection,
		Quantity:    quantity,
		WalletCount: walletCount,
		SignalTx:    signalTx,
		Label:       label,
		FreeOnly:    true,
	})
}

func (e *Engine) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case job := <-e.queue:
			if err := e.handle(ctx, job); err != nil {
				e.log.Error("execution failed", "err", err, "collection", job.Collection.Hex(), "source", job.Source)
			}
		}
	}
}

func (e *Engine) handle(ctx context.Context, job Job) error {
	settings, err := e.store.GetSettings(ctx)
	if err != nil {
		return err
	}
	qty := job.Quantity
	if qty == 0 {
		qty = settings.MintQuantity
	}
	if qty == 0 {
		qty = 1
	}
	collection := strings.ToLower(job.Collection.Hex())

	if job.SignalTx != "" {
		done, err := e.store.HasProcessedSignalTx(ctx, job.SignalTx)
		if err != nil {
			return err
		}
		if done {
			_ = e.store.InsertOrder(ctx, store.MintOrder{
				Source: job.Source, Collection: collection, Quantity: qty,
				Status: "skipped_duplicate", Error: "signal tx already processed",
				SignalTx: job.SignalTx, DryRun: !settings.ExecuteLive,
			})
			return nil
		}
	}

	lockKey := collection + ":" + job.Source
	if !e.tryLockCollection(lockKey) {
		_ = e.store.InsertOrder(ctx, store.MintOrder{
			Source: job.Source, Collection: collection, Quantity: qty,
			Status: "skipped_duplicate", Error: "collection mint already in flight",
			SignalTx: job.SignalTx, DryRun: !settings.ExecuteLive,
		})
		return nil
	}
	defer e.unlockCollection(lockKey)

	planProbe, err := seadrop.BuildPlan(ctx, e.client, job.Collection, qty)
	if err != nil {
		_ = e.store.InsertOrder(ctx, store.MintOrder{
			Source: job.Source, Collection: collection, Quantity: qty,
			Status: "rejected", Error: err.Error(), SignalTx: job.SignalTx, DryRun: !settings.ExecuteLive,
		})
		return err
	}
	if job.FreeOnly || job.Source == "copy" || job.Source == "sweep" {
		if !planProbe.Drop.IsFree() || !planProbe.Drop.IsOpen(0) {
			_ = e.store.InsertOrder(ctx, store.MintOrder{
				Source: job.Source, Collection: collection, Quantity: qty,
				Status: "rejected", Error: "not an open free SeaDrop mint",
				SignalTx: job.SignalTx, DryRun: !settings.ExecuteLive,
			})
			return fmt.Errorf("not an open free SeaDrop mint")
		}
	}

	maxSpend, ok := new(big.Int).SetString(settings.MaxSpendWei, 10)
	if !ok || maxSpend.Sign() <= 0 {
		return fmt.Errorf("invalid max_spend_wei")
	}
	if planProbe.Value.Cmp(maxSpend) > 0 {
		msg := fmt.Sprintf("mint value %s wei exceeds max spend %s wei", planProbe.Value.String(), maxSpend.String())
		_ = e.store.InsertOrder(ctx, store.MintOrder{
			Source: job.Source, Collection: collection, Quantity: qty, ValueWei: planProbe.Value.String(),
			Status: "capped", Error: msg, SignalTx: job.SignalTx, DryRun: !settings.ExecuteLive,
		})
		return fmt.Errorf("%s", msg)
	}

	walletCount := job.WalletCount
	if walletCount <= 0 {
		walletCount = settings.MintMaxWallets
	}
	if walletCount <= 0 {
		walletCount = 1
	}
	if walletCount > len(e.signers) {
		if len(e.signers) == 0 {
			walletCount = 1
		} else {
			walletCount = len(e.signers)
		}
	}
	maxTotal := settings.MintMaxTotal
	if maxTotal <= 0 {
		maxTotal = 20
	}
	alreadyQty, _ := e.store.SumMintedQuantityForCollection(ctx, collection)
	remaining := maxTotal - alreadyQty
	if remaining <= 0 {
		_ = e.store.InsertOrder(ctx, store.MintOrder{
			Source: job.Source, Collection: collection, Quantity: qty,
			Status: "capped", Error: fmt.Sprintf("mint_max_total %d reached", maxTotal),
			SignalTx: job.SignalTx, DryRun: !settings.ExecuteLive,
		})
		return fmt.Errorf("mint_max_total reached")
	}

	signers := e.signers
	if len(signers) == 0 {
		signers = []signer{{}} // dry-run placeholder
	}
	if walletCount > len(signers) {
		walletCount = len(signers)
	}

	var lastErr error
	minted := 0
	for i := 0; i < walletCount && remaining > 0; i++ {
		sg := signers[i]
		useQty := qty
		if int(useQty) > remaining {
			useQty = uint64(remaining)
		}
		if useQty == 0 {
			break
		}
		if settings.ExecuteLive && sg.key != nil {
			done, err := e.store.HasLiveMintForCollectionWallet(ctx, collection, sg.from.Hex())
			if err != nil {
				return err
			}
			if done {
				_ = e.store.InsertOrder(ctx, store.MintOrder{
					Source: job.Source, Collection: collection, Quantity: useQty,
					Status: "skipped_duplicate", Error: "signer already minted live",
					SignalTx: job.SignalTx, DryRun: false, Signer: sg.from.Hex(),
				})
				continue
			}
		}
		if err := e.mintOne(ctx, job, settings, sg, useQty, planProbe); err != nil {
			lastErr = err
			e.log.Error("mint wallet failed", "err", err, "signer", sg.from.Hex())
			continue
		}
		minted++
		remaining -= int(useQty)
	}
	if minted == 0 && lastErr != nil {
		return lastErr
	}
	return nil
}

func (e *Engine) mintOne(ctx context.Context, job Job, settings store.BotSettings, sg signer, qty uint64, probe *seadrop.Plan) error {
	plan := probe
	if qty != probe.Quantity {
		var err error
		plan, err = seadrop.BuildPlan(ctx, e.client, job.Collection, qty)
		if err != nil {
			return err
		}
	}
	live := settings.ExecuteLive && sg.key != nil
	signerAddr := ""
	if sg.key != nil {
		signerAddr = sg.from.Hex()
	}
	order := store.MintOrder{
		Source: job.Source, Collection: strings.ToLower(job.Collection.Hex()), Quantity: qty,
		ValueWei: plan.Value.String(), FeeRecipient: strings.ToLower(plan.FeeRecipient.Hex()),
		SignalTx: job.SignalTx, DryRun: !live, Status: "planned", Signer: signerAddr,
	}
	from := sg.from
	if sg.key == nil {
		from = common.HexToAddress("0x0000000000000000000000000000000000000001")
	}
	if !live {
		msg := ethereum.CallMsg{From: from, To: &plan.To, Value: plan.Value, Data: plan.Data}
		_, callErr := e.client.CallContract(ctx, msg, nil)
		if callErr != nil {
			order.Status = "dry_run_failed"
			order.Error = callErr.Error()
			_ = e.store.InsertOrder(ctx, order)
			return callErr
		}
		order.Status = "dry_run_ok"
		_ = e.store.InsertOrder(ctx, order)
		e.log.Info("dry-run mint ok", "collection", job.Collection.Hex(), "qty", qty, "signer", signerAddr, "source", job.Source)
		e.notify(fmt.Sprintf("[DRY-RUN OK] %s mint x%d\ncollection: %s\nsigner: %s\nsource: %s", job.Label, qty, job.Collection.Hex(), signerAddr, job.Source))
		return nil
	}
	txHash, err := e.sendMint(ctx, sg, plan)
	if err != nil {
		order.Status = "broadcast_failed"
		order.Error = err.Error()
		_ = e.store.InsertOrder(ctx, order)
		return err
	}
	order.Status = "broadcast"
	order.TxHash = txHash
	_ = e.store.InsertOrder(ctx, order)
	e.log.Info("mint broadcast", "tx", txHash, "collection", job.Collection.Hex(), "signer", signerAddr)
	e.notify(fmt.Sprintf("[LIVE] mint broadcast x%d\ncollection: %s\nsigner: %s\ntx: https://robinhoodchain.blockscout.com/tx/%s", qty, job.Collection.Hex(), signerAddr, txHash))
	return nil
}

func (e *Engine) sendMint(ctx context.Context, sg signer, plan *seadrop.Plan) (string, error) {
	nonce, err := e.client.PendingNonceAt(ctx, sg.from)
	if err != nil {
		return "", err
	}
	tip, err := e.client.SuggestGasTipCap(ctx)
	if err != nil {
		tip = big.NewInt(1_000_000)
	}
	header, err := e.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return "", err
	}
	base := header.BaseFee
	if base == nil {
		base = big.NewInt(0)
	}
	maxFee := new(big.Int).Add(new(big.Int).Mul(base, big.NewInt(2)), tip)
	gasLimit := uint64(300_000)
	if est, err := e.client.EstimateGas(ctx, ethereum.CallMsg{
		From: sg.from, To: &plan.To, Value: plan.Value, Data: plan.Data,
	}); err == nil && est > 0 {
		gasLimit = est + est/5
	}
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID: e.chain, Nonce: nonce, GasTipCap: tip, GasFeeCap: maxFee,
		Gas: gasLimit, To: &plan.To, Value: plan.Value, Data: plan.Data,
	})
	signer := types.LatestSignerForChainID(e.chain)
	signed, err := types.SignTx(tx, signer, sg.key)
	if err != nil {
		return "", err
	}
	if err := e.client.SendTransaction(ctx, signed); err != nil {
		return "", err
	}
	return signed.Hash().Hex(), nil
}

func (e *Engine) tryLockCollection(collection string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.inflight[collection]; ok {
		return false
	}
	e.inflight[collection] = struct{}{}
	return true
}

func (e *Engine) unlockCollection(collection string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.inflight, collection)
}

func (e *Engine) notify(msg string) {
	if e.tg == nil || !e.tg.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = e.tg.SendText(ctx, msg)
}

func (e *Engine) ManualMint(ctx context.Context, collection string, quantity uint64) error {
	addr := common.HexToAddress(collection)
	e.Enqueue(Job{Source: "manual", Collection: addr, Quantity: quantity, WalletCount: 1, Label: "manual"})
	return nil
}

func (e *Engine) ManualSweep(ctx context.Context, collection string, quantity uint64, walletCount int) error {
	addr := common.HexToAddress(collection)
	e.Enqueue(Job{Source: "sweep", Collection: addr, Quantity: quantity, WalletCount: walletCount, Label: "manual-sweep", FreeOnly: true})
	return nil
}

func (e *Engine) SettingsView(ctx context.Context) (SettingsView, error) {
	s, err := e.store.GetSettings(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	wei, _ := new(big.Int).SetString(s.MaxSpendWei, 10)
	eth := "0"
	if wei != nil {
		f := new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(1e18))
		eth = f.Text('f', 6)
	}
	mwei, _ := new(big.Int).SetString(s.MemeMaxSpendWei, 10)
	meth := "0"
	if mwei != nil {
		f := new(big.Float).Quo(new(big.Float).SetInt(mwei), big.NewFloat(1e18))
		meth = f.Text('f', 6)
	}
	addrs := e.SignerAddresses()
	return SettingsView{
		MaxSpendWei: s.MaxSpendWei, MaxSpendETH: eth, ExecuteLive: s.ExecuteLive,
		AutoCopyMint: s.AutoCopyMint, MintQuantity: s.MintQuantity,
		MintMaxWallets: s.MintMaxWallets, MintMaxTotal: s.MintMaxTotal,
		SmartWalletMin: s.SmartWalletMin, SmartMintWindowMin: s.SmartMintWindowMin,
		SmartBuyWindowMin: s.SmartBuyWindowMin, NewCollectionMaxAgeH: s.NewCollectionMaxAgeH,
		MemeMaxSpendWei: s.MemeMaxSpendWei, MemeMaxSpendETH: meth,
		MemeExecuteLive: s.MemeExecuteLive, MemeAutoBuy: s.MemeAutoBuy, MemeSlippageBps: s.MemeSlippageBps,
		HasSigner: e.HasSigner(), SignerAddress: e.SignerAddress(), SignerAddresses: addrs, SignerCount: len(addrs),
	}, nil
}
