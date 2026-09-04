package meme

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
	"github.com/xlaez/bot33/internal/chain"
	"github.com/xlaez/bot33/internal/store"
	"github.com/xlaez/bot33/internal/wallet"
)

type BuyJob struct {
	Source   string // copy|manual
	Token    common.Address
	SignalTx string
	Label    string
}

type Buyer struct {
	client *ethclient.Client
	store  *store.Store
	log    *slog.Logger
	tg     *alert.Telegram
	key    *ecdsa.PrivateKey
	from   common.Address
	chain  *big.Int

	mu       sync.Mutex
	queue    chan BuyJob
	inflight map[string]struct{}
}

func NewBuyer(client *ethclient.Client, st *store.Store, log *slog.Logger, tg *alert.Telegram, privateKeyHex string, chainID int64) (*Buyer, error) {
	b := &Buyer{
		client:   client,
		store:    st,
		log:      log,
		tg:       tg,
		chain:    big.NewInt(chainID),
		queue:    make(chan BuyJob, 64),
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
		b.key = key
		b.from = crypto.PubkeyToAddress(key.PublicKey)
	}
	return b, nil
}

func (b *Buyer) HasSigner() bool { return b.key != nil }

func (b *Buyer) SignerAddress() string {
	if b.key == nil {
		return ""
	}
	return b.from.Hex()
}

func (b *Buyer) Enqueue(job BuyJob) {
	select {
	case b.queue <- job:
	default:
		b.log.Warn("meme buy queue full", "token", job.Token.Hex())
	}
}

// BuyNow runs a buy synchronously and returns a user-visible error.
func (b *Buyer) BuyNow(ctx context.Context, job BuyJob) error {
	return b.handle(ctx, job)
}

func (b *Buyer) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case job := <-b.queue:
			if err := b.handle(ctx, job); err != nil {
				b.log.Error("meme buy failed", "err", err, "token", job.Token.Hex(), "source", job.Source)
				b.notify(fmt.Sprintf("[MEME BUY FAILED] %s\ntoken: %s\nsource: %s\nerr: %s",
					job.Label, job.Token.Hex(), job.Source, err.Error()))
			}
		}
	}
}

func (b *Buyer) handle(ctx context.Context, job BuyJob) error {
	settings, err := b.store.GetSettings(ctx)
	if err != nil {
		return err
	}
	token := wallet.NormalizeAddress(job.Token.Hex())

	if settings.MemeExecuteLive && b.key == nil {
		_ = b.store.InsertMemeOrder(ctx, store.MemeOrder{
			Source: job.Source, Token: token, Status: "rejected",
			Error: "meme_execute_live but EXECUTOR_PRIVATE_KEY missing on this process",
			DryRun: true, SignalTx: job.SignalTx,
		})
		return fmt.Errorf("no executor key loaded (set EXECUTOR_PRIVATE_KEY on api and meme-watcher)")
	}

	if !b.tryLock(token) {
		_ = b.store.InsertMemeOrder(ctx, store.MemeOrder{
			Source: job.Source, Token: token, Status: "skipped_duplicate",
			Error: "buy already in flight", DryRun: !settings.MemeExecuteLive, SignalTx: job.SignalTx,
		})
		return fmt.Errorf("buy already in flight")
	}
	defer b.unlock(token)

	if settings.MemeExecuteLive {
		done, err := b.store.HasLiveMemeBuy(ctx, token)
		if err != nil {
			return err
		}
		if done {
			_ = b.store.InsertMemeOrder(ctx, store.MemeOrder{
				Source: job.Source, Token: token, Status: "skipped_duplicate",
				Error: "token already bought live", DryRun: false, SignalTx: job.SignalTx,
			})
			return fmt.Errorf("token already bought live")
		}
	}

	tok, err := b.store.GetMemeToken(ctx, token)
	if err != nil {
		_ = b.store.InsertMemeOrder(ctx, store.MemeOrder{
			Source: job.Source, Token: token, Status: "rejected", Error: "token not tracked: " + err.Error(),
			DryRun: !settings.MemeExecuteLive, SignalTx: job.SignalTx,
		})
		return err
	}
	if !tok.LPLocked {
		_ = b.store.InsertMemeOrder(ctx, store.MemeOrder{
			Source: job.Source, Token: token, PoolAddress: tok.PoolAddress, Dex: tok.Dex,
			Status: "rejected", Error: "lp not locked", DryRun: !settings.MemeExecuteLive, SignalTx: job.SignalTx,
		})
		return fmt.Errorf("lp not locked")
	}
	if tok.FirstLiquidityAt != nil && time.Since(*tok.FirstLiquidityAt) > 30*24*time.Hour {
		_ = b.store.InsertMemeOrder(ctx, store.MemeOrder{
			Source: job.Source, Token: token, Status: "rejected", Error: "token older than 30 days",
			DryRun: !settings.MemeExecuteLive, SignalTx: job.SignalTx,
		})
		return fmt.Errorf("too old")
	}

	spend, ok := new(big.Int).SetString(settings.MemeMaxSpendWei, 10)
	if !ok || spend.Sign() <= 0 {
		return fmt.Errorf("invalid meme_max_spend_wei")
	}

	recipient := b.from
	if b.key == nil {
		recipient = common.HexToAddress("0x0000000000000000000000000000000000000001")
	}
	pool := common.HexToAddress(tok.PoolAddress)
	plan, err := BuildBuyPlanWithPool(ctx, b.client, job.Token, tok.Dex, tok.FeeTier, pool, spend, settings.MemeSlippageBps, recipient)
	if err != nil {
		_ = b.store.InsertMemeOrder(ctx, store.MemeOrder{
			Source: job.Source, Token: token, PoolAddress: tok.PoolAddress, Dex: tok.Dex,
			SpendWei: spend.String(), Status: "rejected", Error: err.Error(),
			DryRun: !settings.MemeExecuteLive, SignalTx: job.SignalTx,
		})
		return err
	}

	live := settings.MemeExecuteLive && b.key != nil
	order := store.MemeOrder{
		Source: job.Source, Token: token, PoolAddress: tok.PoolAddress, Dex: plan.Dex,
		SpendWei: spend.String(), MinOutWei: plan.MinOut.String(),
		DryRun: !live, SignalTx: job.SignalTx, Status: "planned",
	}

	if !live {
		msg := ethereum.CallMsg{From: recipient, To: &plan.To, Value: plan.Value, Data: plan.Data}
		_, callErr := callWithBalanceOverride(ctx, b.client, msg)
		if callErr != nil {
			order.Status = "dry_run_failed"
			order.Error = callErr.Error()
			_ = b.store.InsertMemeOrder(ctx, order)
			return callErr
		}
		order.Status = "dry_run_ok"
		_ = b.store.InsertMemeOrder(ctx, order)
		b.log.Info("meme dry-run buy ok", "token", token, "spend_wei", spend.String(), "dex", plan.Dex, "fee", plan.FeeTier)
		b.notify(fmt.Sprintf("[MEME DRY-RUN OK] %s\ntoken: %s\nspend: %s wei\ndex: %s fee=%d\nmin_out: %s\n(note: live needs meme_execute_live + key on this process)",
			tok.Symbol, chain.ExplorerAddress(token), spend.String(), plan.Dex, plan.FeeTier, plan.MinOut.String()))
		return nil
	}

	bal, err := b.client.BalanceAt(ctx, b.from, nil)
	if err == nil && bal.Cmp(spend) < 0 {
		order.Status = "rejected"
		order.Error = fmt.Sprintf("insufficient ETH: have %s wei need %s wei", bal.String(), spend.String())
		_ = b.store.InsertMemeOrder(ctx, order)
		return fmt.Errorf("%s", order.Error)
	}

	txHash, err := b.send(ctx, plan)
	if err != nil {
		order.Status = "broadcast_failed"
		order.Error = err.Error()
		_ = b.store.InsertMemeOrder(ctx, order)
		return err
	}
	order.Status = "broadcast"
	order.TxHash = txHash
	_ = b.store.InsertMemeOrder(ctx, order)
	b.log.Info("meme buy broadcast", "token", token, "tx", txHash, "spend_wei", spend.String(), "fee", plan.FeeTier)
	b.notify(fmt.Sprintf("[MEME LIVE BUY] %s\ntoken: %s\ntx: %s\nspend: %s wei",
		tok.Symbol, chain.ExplorerAddress(token), chain.ExplorerTx(txHash), spend.String()))
	return nil
}

func (b *Buyer) send(ctx context.Context, plan *SwapPlan) (string, error) {
	nonce, err := b.client.PendingNonceAt(ctx, b.from)
	if err != nil {
		return "", err
	}
	tip, err := b.client.SuggestGasTipCap(ctx)
	if err != nil {
		tip = big.NewInt(1_000_000)
	}
	header, err := b.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return "", err
	}
	base := header.BaseFee
	if base == nil {
		base = big.NewInt(0)
	}
	maxFee := new(big.Int).Add(new(big.Int).Mul(base, big.NewInt(2)), tip)
	gasLimit := uint64(400_000)
	if est, err := b.client.EstimateGas(ctx, ethereum.CallMsg{
		From: b.from, To: &plan.To, Value: plan.Value, Data: plan.Data,
	}); err == nil && est > 0 {
		gasLimit = est + est/5
	}
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID: b.chain, Nonce: nonce, GasTipCap: tip, GasFeeCap: maxFee,
		Gas: gasLimit, To: &plan.To, Value: plan.Value, Data: plan.Data,
	})
	signer := types.LatestSignerForChainID(b.chain)
	signed, err := types.SignTx(tx, signer, b.key)
	if err != nil {
		return "", err
	}
	if err := b.client.SendTransaction(ctx, signed); err != nil {
		return "", err
	}
	return signed.Hash().Hex(), nil
}

func (b *Buyer) tryLock(token string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.inflight[token]; ok {
		return false
	}
	b.inflight[token] = struct{}{}
	return true
}

func (b *Buyer) unlock(token string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.inflight, token)
}

func (b *Buyer) notify(msg string) {
	if b.tg == nil || !b.tg.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = b.tg.SendText(ctx, msg)
}
