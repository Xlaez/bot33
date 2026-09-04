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
	MaxSpendWei     string `json:"max_spend_wei"`
	MaxSpendETH     string `json:"max_spend_eth"`
	ExecuteLive     bool   `json:"execute_live"`
	AutoCopyMint    bool   `json:"auto_copy_mint"`
	MintQuantity    uint64 `json:"mint_quantity"`
	MemeMaxSpendWei string `json:"meme_max_spend_wei"`
	MemeMaxSpendETH string `json:"meme_max_spend_eth"`
	MemeExecuteLive bool   `json:"meme_execute_live"`
	MemeAutoBuy     bool   `json:"meme_auto_buy"`
	MemeSlippageBps int    `json:"meme_slippage_bps"`
	HasSigner       bool   `json:"has_signer"`
	SignerAddress   string `json:"signer_address,omitempty"`
}

type Engine struct {
	client *ethclient.Client
	store  *store.Store
	log    *slog.Logger
	tg     *alert.Telegram
	key    *ecdsa.PrivateKey
	from   common.Address
	chain  *big.Int

	mu       sync.Mutex
	queue    chan Job
	inflight map[string]struct{}
}

type Job struct {
	Source     string // copy|manual
	Collection common.Address
	Quantity   uint64
	SignalTx   string
	Label      string
}

func New(client *ethclient.Client, st *store.Store, log *slog.Logger, tg *alert.Telegram, privateKeyHex string, chainID int64) (*Engine, error) {
	e := &Engine{
		client:   client,
		store:    st,
		log:      log,
		tg:       tg,
		chain:    big.NewInt(chainID),
		queue:    make(chan Job, 64),
		inflight: map[string]struct{}{},
	}
	pk := strings.TrimSpace(privateKeyHex)
	if pk != "" {
		if strings.HasPrefix(pk, "0x") || strings.HasPrefix(pk, "0X") {
			pk = pk[2:]
		}
		key, err := crypto.HexToECDSA(pk)
		if err != nil {
			return nil, fmt.Errorf("EXECUTOR_PRIVATE_KEY: %w", err)
		}
		e.key = key
		e.from = crypto.PubkeyToAddress(key.PublicKey)
	}
	return e, nil
}

func (e *Engine) HasSigner() bool {
	return e.key != nil
}

func (e *Engine) SignerAddress() string {
	if e.key == nil {
		return ""
	}
	return e.from.Hex()
}

func (e *Engine) Enqueue(job Job) {
	select {
	case e.queue <- job:
	default:
		e.log.Warn("execution queue full, dropping job", "collection", job.Collection.Hex(), "source", job.Source)
	}
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

	// Same signal tx (copy) must only run once.
	if job.SignalTx != "" {
		done, err := e.store.HasProcessedSignalTx(ctx, job.SignalTx)
		if err != nil {
			return err
		}
		if done {
			_ = e.store.InsertOrder(ctx, store.MintOrder{
				Source:     job.Source,
				Collection: collection,
				Quantity:   qty,
				Status:     "skipped_duplicate",
				Error:      "signal tx already processed",
				SignalTx:   job.SignalTx,
				DryRun:     !settings.ExecuteLive,
			})
			e.log.Info("skip duplicate signal", "tx", job.SignalTx, "collection", collection)
			return nil
		}
	}

	// Never live-mint the same collection twice.
	if settings.ExecuteLive {
		already, err := e.store.HasLiveMintForCollection(ctx, collection)
		if err != nil {
			return err
		}
		if already {
			_ = e.store.InsertOrder(ctx, store.MintOrder{
				Source:     job.Source,
				Collection: collection,
				Quantity:   qty,
				Status:     "skipped_duplicate",
				Error:      "collection already minted live",
				SignalTx:   job.SignalTx,
				DryRun:     false,
			})
			e.log.Warn("skip duplicate collection mint", "collection", collection)
			return nil
		}
	}

	// In-process lock so concurrent queue jobs cannot double-broadcast one collection.
	if !e.tryLockCollection(collection) {
		_ = e.store.InsertOrder(ctx, store.MintOrder{
			Source:     job.Source,
			Collection: collection,
			Quantity:   qty,
			Status:     "skipped_duplicate",
			Error:      "collection mint already in flight",
			SignalTx:   job.SignalTx,
			DryRun:     !settings.ExecuteLive,
		})
		return nil
	}
	defer e.unlockCollection(collection)

	plan, err := seadrop.BuildPlan(ctx, e.client, job.Collection, qty)
	if err != nil {
		_ = e.store.InsertOrder(ctx, store.MintOrder{
			Source:     job.Source,
			Collection: strings.ToLower(job.Collection.Hex()),
			Quantity:   qty,
			Status:     "rejected",
			Error:      err.Error(),
			SignalTx:   job.SignalTx,
			DryRun:     !settings.ExecuteLive,
		})
		return err
	}

	maxSpend, ok := new(big.Int).SetString(settings.MaxSpendWei, 10)
	if !ok || maxSpend.Sign() <= 0 {
		return fmt.Errorf("invalid max_spend_wei")
	}
	if plan.Value.Cmp(maxSpend) > 0 {
		msg := fmt.Sprintf("mint value %s wei exceeds max spend %s wei", plan.Value.String(), maxSpend.String())
		_ = e.store.InsertOrder(ctx, store.MintOrder{
			Source:     job.Source,
			Collection: strings.ToLower(job.Collection.Hex()),
			Quantity:   qty,
			ValueWei:   plan.Value.String(),
			Status:     "capped",
			Error:      msg,
			SignalTx:   job.SignalTx,
			DryRun:     !settings.ExecuteLive,
		})
		e.log.Warn("spend cap blocked mint", "value", plan.Value.String(), "cap", maxSpend.String())
		return fmt.Errorf("%s", msg)
	}

	live := settings.ExecuteLive && e.key != nil
	order := store.MintOrder{
		Source:       job.Source,
		Collection:   strings.ToLower(job.Collection.Hex()),
		Quantity:     qty,
		ValueWei:     plan.Value.String(),
		FeeRecipient: strings.ToLower(plan.FeeRecipient.Hex()),
		SignalTx:     job.SignalTx,
		DryRun:       !live,
		Status:       "planned",
	}

	if !live {
		// eth_call simulation
		msg := ethereum.CallMsg{
			From:  e.from,
			To:    &plan.To,
			Value: plan.Value,
			Data:  plan.Data,
		}
		if e.key == nil {
			msg.From = common.HexToAddress("0x0000000000000000000000000000000000000001")
		}
		_, callErr := e.client.CallContract(ctx, msg, nil)
		if callErr != nil {
			order.Status = "dry_run_failed"
			order.Error = callErr.Error()
			_ = e.store.InsertOrder(ctx, order)
			return callErr
		}
		order.Status = "dry_run_ok"
		_ = e.store.InsertOrder(ctx, order)
		e.log.Info("dry-run mint ok", "collection", job.Collection.Hex(), "value_wei", plan.Value.String(), "qty", qty, "source", job.Source)
		e.notify(fmt.Sprintf("[DRY-RUN OK] %s mint x%d\ncollection: %s\nvalue: %s wei\nsource: %s", job.Label, qty, job.Collection.Hex(), plan.Value.String(), job.Source))
		return nil
	}

	txHash, err := e.sendMint(ctx, plan)
	if err != nil {
		order.Status = "broadcast_failed"
		order.Error = err.Error()
		_ = e.store.InsertOrder(ctx, order)
		return err
	}
	order.Status = "broadcast"
	order.TxHash = txHash
	_ = e.store.InsertOrder(ctx, order)
	e.log.Info("mint broadcast", "tx", txHash, "collection", job.Collection.Hex(), "value_wei", plan.Value.String())
	e.notify(fmt.Sprintf("[LIVE] mint broadcast x%d\ncollection: %s\ntx: https://robinhoodchain.blockscout.com/tx/%s", qty, job.Collection.Hex(), txHash))
	return nil
}

func (e *Engine) sendMint(ctx context.Context, plan *seadrop.Plan) (string, error) {
	nonce, err := e.client.PendingNonceAt(ctx, e.from)
	if err != nil {
		return "", err
	}
	tip, err := e.client.SuggestGasTipCap(ctx)
	if err != nil {
		tip = big.NewInt(1_000_000) // 0.001 gwei fallback
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
		From: e.from, To: &plan.To, Value: plan.Value, Data: plan.Data,
	}); err == nil && est > 0 {
		gasLimit = est + est/5
	}
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   e.chain,
		Nonce:     nonce,
		GasTipCap: tip,
		GasFeeCap: maxFee,
		Gas:       gasLimit,
		To:        &plan.To,
		Value:     plan.Value,
		Data:      plan.Data,
	})
	signer := types.LatestSignerForChainID(e.chain)
	signed, err := types.SignTx(tx, signer, e.key)
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
	e.Enqueue(Job{Source: "manual", Collection: addr, Quantity: quantity, Label: "manual"})
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
	return SettingsView{
		MaxSpendWei:     s.MaxSpendWei,
		MaxSpendETH:     eth,
		ExecuteLive:     s.ExecuteLive,
		AutoCopyMint:    s.AutoCopyMint,
		MintQuantity:    s.MintQuantity,
		MemeMaxSpendWei: s.MemeMaxSpendWei,
		MemeMaxSpendETH: meth,
		MemeExecuteLive: s.MemeExecuteLive,
		MemeAutoBuy:     s.MemeAutoBuy,
		MemeSlippageBps: s.MemeSlippageBps,
		HasSigner:       e.HasSigner(),
		SignerAddress:   e.SignerAddress(),
	}, nil
}
