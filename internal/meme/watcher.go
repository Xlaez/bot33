package meme

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/xlaez/bot33/internal/alert"
	"github.com/xlaez/bot33/internal/chain"
	"github.com/xlaez/bot33/internal/store"
	"github.com/xlaez/bot33/internal/wallet"
)

const (
	cursorLaunches = "meme_launches"
	cursorSwaps    = "meme_swaps"
)

type Watcher struct {
	client   *ethclient.Client
	store    *store.Store
	log      *slog.Logger
	telegram *alert.Telegram
	interval time.Duration
	startLag uint64
	maxAge   time.Duration
}

func NewWatcher(client *ethclient.Client, st *store.Store, log *slog.Logger, tg *alert.Telegram, interval time.Duration, startLag uint64) *Watcher {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Watcher{
		client:   client,
		store:    st,
		log:      log,
		telegram: tg,
		interval: interval,
		startLag: startLag,
		maxAge:   30 * 24 * time.Hour,
	}
}

func (w *Watcher) Run(ctx context.Context) error {
	w.log.Info("meme watcher started",
		"v2_factory", V2Factory.Hex(),
		"v3_factory", V3Factory.Hex(),
		"v4_pool_manager", V4PoolManager.Hex(),
		"telegram", w.telegram != nil && w.telegram.Enabled(),
	)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	rescore := time.NewTicker(2 * time.Minute)
	defer rescore.Stop()
	backoff := w.interval
	for {
		if err := w.pollLaunches(ctx); err != nil {
			w.log.Error("meme launch poll failed", "err", err, "backoff", backoff.String())
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			if backoff < 60*time.Second {
				backoff *= 2
			}
		} else {
			backoff = w.interval
		}
		if err := w.pollSwaps(ctx); err != nil {
			w.log.Error("meme swap poll failed", "err", err)
		}
		if err := w.pollV3PositionBurns(ctx); err != nil {
			w.log.Error("meme v3 lock poll failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-rescore.C:
			if err := w.rescoreYoung(ctx); err != nil {
				w.log.Error("meme rescore failed", "err", err)
			}
		case <-ticker.C:
		}
	}
}

func (w *Watcher) pollRange(ctx context.Context, cursor string) (from, to uint64, err error) {
	head, err := w.client.BlockNumber(ctx)
	if err != nil {
		return 0, 0, err
	}
	safe := head
	if w.startLag > 0 && head > w.startLag {
		safe = head - w.startLag
	}
	from, err = w.store.GetCursor(ctx, cursor)
	if err != nil {
		return 0, 0, err
	}
	if from == 0 {
		from = safe
		if from > 128 {
			from -= 128
		}
	} else {
		from++
	}
	const maxLag = uint64(8_000)
	if from+maxLag < safe {
		jump := safe - 256
		w.log.Warn("meme cursor far behind, jumping", "cursor", cursor, "was", from-1, "jump_to", jump)
		from = jump
		_ = w.store.SetCursor(ctx, cursor, from-1)
	}
	if from > safe {
		return from, safe, nil
	}
	to = from + 400
	if to > safe {
		to = safe
	}
	return from, to, nil
}

func (w *Watcher) pollLaunches(ctx context.Context) error {
	from, to, err := w.pollRange(ctx, cursorLaunches)
	if err != nil {
		return err
	}
	if from > to {
		return nil
	}

	q := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(from),
		ToBlock:   new(big.Int).SetUint64(to),
		Addresses: []common.Address{V2Factory, V3Factory, V4PoolManager},
		Topics: [][]common.Hash{{
			PairCreatedTopic,
			PoolCreatedTopic,
			V4InitializeTopic,
		}},
	}
	logs, err := w.client.FilterLogs(ctx, q)
	if err != nil {
		return err
	}
	n := 0
	for _, lg := range logs {
		first, err := w.store.MarkSeen(ctx, strings.ToLower(lg.TxHash.Hex()), lg.Index)
		if err != nil {
			return err
		}
		if !first {
			continue
		}
		if err := w.handleLaunchLog(ctx, lg); err != nil {
			w.log.Warn("meme launch handle", "err", err, "tx", lg.TxHash.Hex())
			continue
		}
		n++
	}
	if n > 0 {
		w.log.Info("meme launches processed", "count", n, "from", from, "to", to)
	}
	return w.store.SetCursor(ctx, cursorLaunches, to)
}

func (w *Watcher) handleLaunchLog(ctx context.Context, lg types.Log) error {
	switch lg.Topics[0] {
	case PairCreatedTopic:
		return w.handleV2Pair(ctx, lg)
	case PoolCreatedTopic:
		return w.handleV3Pool(ctx, lg)
	case V4InitializeTopic:
		return w.handleV4Init(ctx, lg)
	default:
		return nil
	}
}

func (w *Watcher) handleV2Pair(ctx context.Context, lg types.Log) error {
	if len(lg.Topics) < 3 || len(lg.Data) < 64 {
		return fmt.Errorf("bad PairCreated")
	}
	token0 := common.BytesToAddress(lg.Topics[1].Bytes())
	token1 := common.BytesToAddress(lg.Topics[2].Bytes())
	pair := common.BytesToAddress(lg.Data[12:32])
	meme, quote, ok := classifyPair(token0, token1)
	if !ok {
		return nil // ignore non-quote pairs (token/token)
	}
	now := time.Now().UTC()
	meta := FetchTokenMeta(ctx, w.client, meme)
	lock, _ := CheckV2LPLock(ctx, w.client, pair)
	renounced := OwnerRenounced(ctx, w.client, meme)
	_ = w.store.UpsertMemePool(ctx, store.MemePool{
		PoolAddress: pair.Hex(), Token0: token0.Hex(), Token1: token1.Hex(),
		MemeToken: meme.Hex(), QuoteToken: quote.Hex(), Dex: "uniswap-v2",
		CreatedTx: lg.TxHash.Hex(), CreatedBlock: lg.BlockNumber,
	})
	sc := Score(ScoreInput{
		LPLocked: lock.Locked, LPLockPct: lock.Pct, OwnerRenounced: renounced,
		HasLiquidity: true, Age: 0, QuoteIsWETH: quote == WETH,
	})
	// First liquidity = pair creation time as proxy; refined when Mint seen via swaps path.
	t := store.MemeToken{
		Address: meme.Hex(), Symbol: meta.Symbol, Name: meta.Name, Decimals: meta.Decimals,
		PairedWith: quote.Hex(), PoolAddress: pair.Hex(), Dex: "uniswap-v2",
		LaunchTx: lg.TxHash.Hex(), LaunchBlock: lg.BlockNumber, FirstLiquidityAt: &now,
		LPLocked: lock.Locked, LPLockPct: lock.Pct, LPLockEvidence: lock.Evidence,
		OwnerRenounced: renounced, Score: sc.Score, Flags: sc.Flags, Status: "watching",
		VolumeWei: "0",
	}
	if err := w.store.UpsertMemeToken(ctx, t); err != nil {
		return err
	}
	return w.maybeAlert(ctx, t.Address, sc)
}

func (w *Watcher) handleV3Pool(ctx context.Context, lg types.Log) error {
	if len(lg.Topics) < 3 || len(lg.Data) < 96 {
		return fmt.Errorf("bad PoolCreated")
	}
	token0 := common.BytesToAddress(lg.Topics[1].Bytes())
	token1 := common.BytesToAddress(lg.Topics[2].Bytes())
	fee := new(big.Int).SetBytes(lg.Data[0:32])
	pool := common.BytesToAddress(lg.Data[64:96])
	meme, quote, ok := classifyPair(token0, token1)
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	meta := FetchTokenMeta(ctx, w.client, meme)
	lock, _ := CheckV3LPLock(ctx, w.client, V3PositionMgr)
	// V3 lock is not inferred from global NPM burns; wait for position NFT burn events.
	_ = lock
	lock = LockResult{Locked: false, Evidence: "v3_awaiting_position_burn"}
	renounced := OwnerRenounced(ctx, w.client, meme)
	_ = w.store.UpsertMemePool(ctx, store.MemePool{
		PoolAddress: pool.Hex(), Token0: token0.Hex(), Token1: token1.Hex(),
		MemeToken: meme.Hex(), QuoteToken: quote.Hex(), Dex: "uniswap-v3", FeeTier: int(fee.Int64()),
		CreatedTx: lg.TxHash.Hex(), CreatedBlock: lg.BlockNumber,
	})
	sc := Score(ScoreInput{
		LPLocked: lock.Locked, LPLockPct: lock.Pct, OwnerRenounced: renounced,
		HasLiquidity: true, Age: 0, QuoteIsWETH: quote == WETH,
	})
	t := store.MemeToken{
		Address: meme.Hex(), Symbol: meta.Symbol, Name: meta.Name, Decimals: meta.Decimals,
		PairedWith: quote.Hex(), PoolAddress: pool.Hex(), Dex: "uniswap-v3", FeeTier: int(fee.Int64()),
		LaunchTx: lg.TxHash.Hex(), LaunchBlock: lg.BlockNumber, FirstLiquidityAt: &now,
		LPLocked: lock.Locked, LPLockPct: lock.Pct, LPLockEvidence: lock.Evidence,
		OwnerRenounced: renounced, Score: sc.Score, Flags: sc.Flags, Status: "watching",
		VolumeWei: "0",
	}
	if err := w.store.UpsertMemeToken(ctx, t); err != nil {
		return err
	}
	return w.maybeAlert(ctx, t.Address, sc)
}

func (w *Watcher) handleV4Init(ctx context.Context, lg types.Log) error {
	// Initialize(bytes32 id, address currency0, address currency1, uint24 fee, int24 tickSpacing, address hooks, uint160 sqrtPriceX96, int24 tick)
	if len(lg.Data) < 256 {
		return fmt.Errorf("bad V4 Initialize")
	}
	currency0 := common.BytesToAddress(lg.Data[32:64])
	currency1 := common.BytesToAddress(lg.Data[64:96])
	fee := new(big.Int).SetBytes(lg.Data[96:128])
	meme, quote, ok := classifyPair(currency0, currency1)
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	meta := FetchTokenMeta(ctx, w.client, meme)
	renounced := OwnerRenounced(ctx, w.client, meme)
	poolKey := common.BytesToHash(lg.Data[0:32]).Hex()
	_ = w.store.UpsertMemePool(ctx, store.MemePool{
		PoolAddress: poolKey, Token0: currency0.Hex(), Token1: currency1.Hex(),
		MemeToken: meme.Hex(), QuoteToken: quote.Hex(), Dex: "uniswap-v4", FeeTier: int(fee.Int64()),
		CreatedTx: lg.TxHash.Hex(), CreatedBlock: lg.BlockNumber,
	})
	// V4 LP lock detection is limited without locker indexing — require later rescore / manual evidence.
	lock := LockResult{Locked: false, Evidence: "v4_lock_pending_indexer"}
	sc := Score(ScoreInput{
		LPLocked: false, OwnerRenounced: renounced, HasLiquidity: true, Age: 0, QuoteIsWETH: quote == WETH,
	})
	t := store.MemeToken{
		Address: meme.Hex(), Symbol: meta.Symbol, Name: meta.Name, Decimals: meta.Decimals,
		PairedWith: quote.Hex(), PoolAddress: poolKey, Dex: "uniswap-v4", FeeTier: int(fee.Int64()),
		LaunchTx: lg.TxHash.Hex(), LaunchBlock: lg.BlockNumber, FirstLiquidityAt: &now,
		LPLocked: lock.Locked, LPLockPct: lock.Pct, LPLockEvidence: lock.Evidence,
		OwnerRenounced: renounced, Score: sc.Score, Flags: sc.Flags, Status: "watching",
		VolumeWei: "0",
	}
	if err := w.store.UpsertMemeToken(ctx, t); err != nil {
		return err
	}
	return w.maybeAlert(ctx, t.Address, sc)
}

func classifyPair(a, b common.Address) (meme, quote common.Address, ok bool) {
	if _, isQuote := QuoteTokens[a]; isQuote {
		return b, a, true
	}
	if _, isQuote := QuoteTokens[b]; isQuote {
		return a, b, true
	}
	return common.Address{}, common.Address{}, false
}

func (w *Watcher) pollV3PositionBurns(ctx context.Context) error {
	from, to, err := w.pollRange(ctx, "meme_v3_locks")
	if err != nil {
		return err
	}
	if from > to {
		return nil
	}
	q := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(from),
		ToBlock:   new(big.Int).SetUint64(to),
		Addresses: []common.Address{V3PositionMgr},
		Topics: [][]common.Hash{
			{TransferTopic},
			nil,
			{common.BytesToHash(DeadAddress.Bytes()), common.BytesToHash(ZeroAddress.Bytes())},
		},
	}
	logs, err := w.client.FilterLogs(ctx, q)
	if err != nil {
		return err
	}
	for _, lg := range logs {
		if len(lg.Topics) < 4 {
			continue
		}
		tokenID := new(big.Int).SetBytes(lg.Topics[3].Bytes())
		token0, token1, ok := positionTokens(ctx, w.client, tokenID)
		if !ok {
			continue
		}
		meme, _, ok := classifyPair(token0, token1)
		if !ok {
			continue
		}
		tok, err := w.store.GetMemeToken(ctx, meme.Hex())
		if err != nil {
			continue
		}
		tok.LPLocked = true
		tok.LPLockPct = 100
		tok.LPLockEvidence = fmt.Sprintf("v3_position_nft_to_burn id=%s", tokenID.String())
		sc := Score(scoreInputFromToken(tok))
		tok.Score = sc.Score
		tok.Flags = sc.Flags
		_ = w.store.UpsertMemeToken(ctx, tok)
		_ = w.maybeAlert(ctx, tok.Address, sc)
	}
	return w.store.SetCursor(ctx, "meme_v3_locks", to)
}

var npmABI = mustABI(`[
  {"inputs":[{"name":"tokenId","type":"uint256"}],"name":"positions","outputs":[
    {"name":"nonce","type":"uint96"},
    {"name":"operator","type":"address"},
    {"name":"token0","type":"address"},
    {"name":"token1","type":"address"},
    {"name":"fee","type":"uint24"},
    {"name":"tickLower","type":"int24"},
    {"name":"tickUpper","type":"int24"},
    {"name":"liquidity","type":"uint128"},
    {"name":"feeGrowthInside0LastX128","type":"uint256"},
    {"name":"feeGrowthInside1LastX128","type":"uint256"},
    {"name":"tokensOwed0","type":"uint128"},
    {"name":"tokensOwed1","type":"uint128"}
  ],"stateMutability":"view","type":"function"}
]`)

func positionTokens(ctx context.Context, client *ethclient.Client, tokenID *big.Int) (common.Address, common.Address, bool) {
	data, err := npmABI.Pack("positions", tokenID)
	if err != nil {
		return common.Address{}, common.Address{}, false
	}
	to := V3PositionMgr
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil || len(out) == 0 {
		return common.Address{}, common.Address{}, false
	}
	vals, err := npmABI.Unpack("positions", out)
	if err != nil || len(vals) < 5 {
		return common.Address{}, common.Address{}, false
	}
	t0, _ := vals[2].(common.Address)
	t1, _ := vals[3].(common.Address)
	return t0, t1, true
}

func (w *Watcher) pollSwaps(ctx context.Context) error {
	pools, err := w.store.ListActiveMemePools(ctx, w.maxAge)
	if err != nil {
		return err
	}
	if len(pools) == 0 {
		return nil
	}
	from, to, err := w.pollRange(ctx, cursorSwaps)
	if err != nil {
		return err
	}
	if from > to {
		return nil
	}
	addrs := make([]common.Address, 0, len(pools))
	poolToToken := map[string]string{}
	for _, p := range pools {
		if p.Dex == "uniswap-v4" {
			continue // pool id is not a contract address
		}
		addrs = append(addrs, common.HexToAddress(p.PoolAddress))
		poolToToken[wallet.NormalizeAddress(p.PoolAddress)] = p.MemeToken
	}
	if len(addrs) == 0 {
		return w.store.SetCursor(ctx, cursorSwaps, to)
	}
	// Chunk addresses to avoid oversized filters.
	const chunk = 40
	now := time.Now().UTC()
	for i := 0; i < len(addrs); i += chunk {
		end := i + chunk
		if end > len(addrs) {
			end = len(addrs)
		}
		q := ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(from),
			ToBlock:   new(big.Int).SetUint64(to),
			Addresses: addrs[i:end],
			Topics:    [][]common.Hash{{V2SwapTopic, V3SwapTopic, V2MintTopic}},
		}
		logs, err := w.client.FilterLogs(ctx, q)
		if err != nil {
			return err
		}
		for _, lg := range logs {
			token := poolToToken[wallet.NormalizeAddress(lg.Address.Hex())]
			if token == "" {
				continue
			}
			first, err := w.store.MarkSeen(ctx, "s:"+strings.ToLower(lg.TxHash.Hex()), lg.Index)
			if err != nil {
				return err
			}
			if !first {
				continue
			}
			vol := estimateSwapVolumeWei(lg)
			if lg.Topics[0] == V2MintTopic {
				// Liquidity add — re-check V2 lock.
				pair := lg.Address
				lock, err := CheckV2LPLock(ctx, w.client, pair)
				if err == nil {
					tok, err := w.store.GetMemeToken(ctx, token)
					if err == nil {
						tok.LPLocked = lock.Locked
						tok.LPLockPct = lock.Pct
						tok.LPLockEvidence = lock.Evidence
						sc := Score(scoreInputFromToken(tok))
						tok.Score = sc.Score
						tok.Flags = sc.Flags
						_ = w.store.UpsertMemeToken(ctx, tok)
						_ = w.maybeAlert(ctx, token, sc)
					}
				}
			}
			_ = w.store.RecordMemeSwap(ctx, token, vol.String(), now)
		}
	}
	return w.store.SetCursor(ctx, cursorSwaps, to)
}

func estimateSwapVolumeWei(lg types.Log) *big.Int {
	// Best-effort: take max of amount fields in data for volume proxy.
	max := big.NewInt(0)
	for i := 0; i+32 <= len(lg.Data); i += 32 {
		v := new(big.Int).SetBytes(lg.Data[i : i+32])
		// Ignore clearly huge int256 negatives by capping bit length.
		if v.BitLen() > 128 {
			continue
		}
		if v.Cmp(max) > 0 {
			max = v
		}
	}
	return max
}

func (w *Watcher) rescoreYoung(ctx context.Context) error {
	tokens, err := w.store.ListMemeTokens(ctx, w.maxAge, 200)
	if err != nil {
		return err
	}
	for _, tok := range tokens {
		if tok.Dex == "uniswap-v2" && tok.PoolAddress != "" {
			lock, err := CheckV2LPLock(ctx, w.client, common.HexToAddress(tok.PoolAddress))
			if err == nil {
				tok.LPLocked = lock.Locked
				tok.LPLockPct = lock.Pct
				tok.LPLockEvidence = lock.Evidence
			}
		}
		tok.OwnerRenounced = OwnerRenounced(ctx, w.client, common.HexToAddress(tok.Address)) || tok.OwnerRenounced
		sc := Score(scoreInputFromToken(tok))
		tok.Score = sc.Score
		tok.Flags = sc.Flags
		if err := w.store.UpsertMemeToken(ctx, tok); err != nil {
			return err
		}
		_ = w.maybeAlert(ctx, tok.Address, sc)
	}
	return nil
}

func scoreInputFromToken(tok store.MemeToken) ScoreInput {
	age := time.Duration(0)
	if tok.FirstLiquidityAt != nil {
		age = time.Since(*tok.FirstLiquidityAt)
	}
	vol := big.NewInt(0)
	if tok.VolumeWei != "" {
		if v, ok := new(big.Int).SetString(tok.VolumeWei, 10); ok {
			vol = v
		}
	}
	return ScoreInput{
		LPLocked:       tok.LPLocked,
		LPLockPct:      tok.LPLockPct,
		OwnerRenounced: tok.OwnerRenounced,
		HasLiquidity:   tok.FirstLiquidityAt != nil,
		Age:            age,
		SwapCount:      tok.SwapCount,
		UniqueTraders:  tok.UniqueTraders,
		VolumeWei:      vol,
		QuoteIsWETH:    strings.EqualFold(tok.PairedWith, WETH.Hex()),
	}
}

func (w *Watcher) maybeAlert(ctx context.Context, address string, sc ScoreResult) error {
	if !sc.AlertOK || w.telegram == nil || !w.telegram.Enabled() {
		return nil
	}
	tok, err := w.store.GetMemeToken(ctx, address)
	if err != nil {
		return err
	}
	if tok.AlertedAt != nil {
		return nil
	}
	if tok.FirstLiquidityAt != nil && time.Since(*tok.FirstLiquidityAt) > w.maxAge {
		return nil
	}
	msg := fmt.Sprintf(
		"[MEME score=%.0f] %s (%s)\n"+
			"dex: %s\npair: %s\nlp_locked: %v (%.1f%%)\n"+
			"flags: %s\nvolume_wei: %s swaps: %d\n"+
			"token: %s\npool: %s\ntx: %s",
		sc.Score,
		tok.Symbol,
		tok.Name,
		tok.Dex,
		tok.PairedWith,
		tok.LPLocked,
		tok.LPLockPct,
		strings.Join(sc.Flags, ", "),
		tok.VolumeWei,
		tok.SwapCount,
		chain.ExplorerAddress(tok.Address),
		chain.ExplorerAddress(tok.PoolAddress),
		chain.ExplorerTx(tok.LaunchTx),
	)
	if err := w.telegram.SendText(ctx, msg); err != nil {
		return err
	}
	if err := w.store.MarkMemeAlerted(ctx, address); err != nil {
		return err
	}
	w.log.Info("meme alert sent", "token", address, "score", sc.Score, "symbol", tok.Symbol)
	return nil
}
