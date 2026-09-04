package meme

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
)

var v2RouterABI = mustABI(`[
  {"inputs":[{"name":"amountIn","type":"uint256"},{"name":"path","type":"address[]"}],"name":"getAmountsOut","outputs":[{"name":"amounts","type":"uint256[]"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"amountOutMin","type":"uint256"},{"name":"path","type":"address[]"},{"name":"to","type":"address"},{"name":"deadline","type":"uint256"}],"name":"swapExactETHForTokensSupportingFeeOnTransferTokens","outputs":[],"stateMutability":"payable","type":"function"},
  {"inputs":[{"name":"amountOutMin","type":"uint256"},{"name":"path","type":"address[]"},{"name":"to","type":"address"},{"name":"deadline","type":"uint256"}],"name":"swapExactETHForTokens","outputs":[{"name":"amounts","type":"uint256[]"}],"stateMutability":"payable","type":"function"}
]`)

var v3RouterABI = mustABI(`[
  {"inputs":[{"components":[
    {"name":"tokenIn","type":"address"},
    {"name":"tokenOut","type":"address"},
    {"name":"fee","type":"uint24"},
    {"name":"recipient","type":"address"},
    {"name":"amountIn","type":"uint256"},
    {"name":"amountOutMinimum","type":"uint256"},
    {"name":"sqrtPriceLimitX96","type":"uint160"}
  ],"name":"params","type":"tuple"}],"name":"exactInputSingle","outputs":[{"name":"amountOut","type":"uint256"}],"stateMutability":"payable","type":"function"}
]`)

var v3PoolFeeABI = mustABI(`[{"inputs":[],"name":"fee","outputs":[{"type":"uint24"}],"stateMutability":"view","type":"function"}]`)

type SwapPlan struct {
	To       common.Address
	Data     []byte
	Value    *big.Int
	MinOut   *big.Int
	Dex      string
	Deadline uint64
	FeeTier  int
}

// BuildBuyPlan builds a native-ETH → meme token swap plan (V2 preferred, then V3).
func BuildBuyPlan(ctx context.Context, client *ethclient.Client, token common.Address, dex string, feeTier int, amountIn *big.Int, slippageBps int, recipient common.Address) (*SwapPlan, error) {
	return BuildBuyPlanWithPool(ctx, client, token, dex, feeTier, common.Address{}, amountIn, slippageBps, recipient)
}

func BuildBuyPlanWithPool(ctx context.Context, client *ethclient.Client, token common.Address, dex string, feeTier int, pool common.Address, amountIn *big.Int, slippageBps int, recipient common.Address) (*SwapPlan, error) {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return nil, fmt.Errorf("spend amount must be > 0")
	}
	if slippageBps <= 0 {
		slippageBps = 1000
	}
	if slippageBps > 5000 {
		slippageBps = 5000
	}
	deadline := uint64(time.Now().Add(3 * time.Minute).Unix())

	switch strings.ToLower(dex) {
	case "uniswap-v2", "":
		return buildV2Buy(ctx, client, token, amountIn, slippageBps, recipient, deadline)
	case "uniswap-v3":
		return buildV3Buy(ctx, client, token, feeTier, pool, amountIn, slippageBps, recipient)
	default:
		return nil, fmt.Errorf("auto-buy not supported for dex %s yet", dex)
	}
}

func buildV2Buy(ctx context.Context, client *ethclient.Client, token common.Address, amountIn *big.Int, slippageBps int, recipient common.Address, deadline uint64) (*SwapPlan, error) {
	path := []common.Address{WETH, token}
	expected, err := v2GetAmountsOut(ctx, client, amountIn, path)
	if err != nil {
		return nil, fmt.Errorf("getAmountsOut: %w", err)
	}
	minOut := applySlippage(expected, slippageBps)
	data, err := v2RouterABI.Pack("swapExactETHForTokensSupportingFeeOnTransferTokens", minOut, path, recipient, new(big.Int).SetUint64(deadline))
	if err != nil {
		data, err = v2RouterABI.Pack("swapExactETHForTokens", minOut, path, recipient, new(big.Int).SetUint64(deadline))
		if err != nil {
			return nil, err
		}
	}
	return &SwapPlan{
		To: V2Router, Data: data, Value: amountIn, MinOut: minOut, Dex: "uniswap-v2", Deadline: deadline,
	}, nil
}

func buildV3Buy(ctx context.Context, client *ethclient.Client, token common.Address, feeTier int, pool common.Address, amountIn *big.Int, slippageBps int, recipient common.Address) (*SwapPlan, error) {
	fees := v3FeeCandidates(feeTier, pool, client, ctx)
	var lastErr error
	for _, fee := range fees {
		plan, err := buildV3BuyFee(ctx, client, token, fee, amountIn, slippageBps, recipient)
		if err == nil {
			return plan, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no v3 fee tier worked")
	}
	return nil, fmt.Errorf("v3 buy: %w (tried fees %v)", lastErr, fees)
}

func v3FeeCandidates(feeTier int, pool common.Address, client *ethclient.Client, ctx context.Context) []int {
	seen := map[int]bool{}
	var out []int
	add := func(f int) {
		if f <= 0 || seen[f] {
			return
		}
		seen[f] = true
		out = append(out, f)
	}
	add(feeTier)
	if pool != (common.Address{}) {
		if f, err := fetchV3PoolFee(ctx, client, pool); err == nil {
			add(f)
		}
	}
	for _, f := range []int{100, 500, 3000, 10000} {
		add(f)
	}
	return out
}

func fetchV3PoolFee(ctx context.Context, client *ethclient.Client, pool common.Address) (int, error) {
	data, err := v3PoolFeeABI.Pack("fee")
	if err != nil {
		return 0, err
	}
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &pool, Data: data}, nil)
	if err != nil {
		return 0, err
	}
	vals, err := v3PoolFeeABI.Unpack("fee", out)
	if err != nil || len(vals) == 0 {
		return 0, fmt.Errorf("unpack fee")
	}
	switch v := vals[0].(type) {
	case *big.Int:
		return int(v.Int64()), nil
	case uint64:
		return int(v), nil
	case uint32:
		return int(v), nil
	case uint16:
		return int(v), nil
	default:
		return 0, fmt.Errorf("bad fee type %T", vals[0])
	}
}

func buildV3BuyFee(ctx context.Context, client *ethclient.Client, token common.Address, feeTier int, amountIn *big.Int, slippageBps int, recipient common.Address) (*SwapPlan, error) {
	probe := exactInputSingleArgs{
		TokenIn: WETH, TokenOut: token, Fee: big.NewInt(int64(feeTier)), Recipient: recipient,
		AmountIn: amountIn, AmountOutMinimum: big.NewInt(0), SqrtPriceLimitX96: big.NewInt(0),
	}
	data, err := v3RouterABI.Pack("exactInputSingle", probe)
	if err != nil {
		return nil, err
	}
	to := V3SwapRouter02
	out, err := callWithBalanceOverride(ctx, client, ethereum.CallMsg{
		From: recipient, To: &to, Value: amountIn, Data: data,
	})
	if err != nil {
		return nil, fmt.Errorf("fee %d quote: %w", feeTier, err)
	}
	vals, err := v3RouterABI.Unpack("exactInputSingle", out)
	if err != nil || len(vals) == 0 {
		return nil, fmt.Errorf("fee %d unpack quote", feeTier)
	}
	expected, ok := vals[0].(*big.Int)
	if !ok || expected.Sign() <= 0 {
		return nil, fmt.Errorf("fee %d zero quote", feeTier)
	}
	minOut := applySlippage(expected, slippageBps)
	live := exactInputSingleArgs{
		TokenIn: WETH, TokenOut: token, Fee: big.NewInt(int64(feeTier)), Recipient: recipient,
		AmountIn: amountIn, AmountOutMinimum: minOut, SqrtPriceLimitX96: big.NewInt(0),
	}
	data, err = v3RouterABI.Pack("exactInputSingle", live)
	if err != nil {
		return nil, err
	}
	return &SwapPlan{
		To: V3SwapRouter02, Data: data, Value: amountIn, MinOut: minOut, Dex: "uniswap-v3", FeeTier: feeTier,
	}, nil
}

type exactInputSingleArgs struct {
	TokenIn           common.Address
	TokenOut          common.Address
	Fee               *big.Int
	Recipient         common.Address
	AmountIn          *big.Int
	AmountOutMinimum  *big.Int
	SqrtPriceLimitX96 *big.Int
}

// callWithBalanceOverride simulates a payable call as if From has huge ETH (public RPCs often
// revert eth_call when the from wallet has insufficient balance for msg.value).
func callWithBalanceOverride(ctx context.Context, client *ethclient.Client, msg ethereum.CallMsg) ([]byte, error) {
	from := msg.From
	if from == (common.Address{}) {
		from = common.HexToAddress("0x0000000000000000000000000000000000000001")
		msg.From = from
	}
	arg := map[string]interface{}{
		"from": from,
		"to":   msg.To,
		"data": hexutil.Bytes(msg.Data),
	}
	if msg.Value != nil {
		arg["value"] = (*hexutil.Big)(msg.Value)
	}
	overrides := map[string]interface{}{
		from.Hex(): map[string]interface{}{
			"balance": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		},
	}
	var out hexutil.Bytes
	err := client.Client().CallContext(ctx, &out, "eth_call", arg, "latest", overrides)
	if err != nil {
		// Fallback without overrides (some RPCs reject the 5th arg).
		return client.CallContract(ctx, msg, nil)
	}
	return out, nil
}

func v2GetAmountsOut(ctx context.Context, client *ethclient.Client, amountIn *big.Int, path []common.Address) (*big.Int, error) {
	data, err := v2RouterABI.Pack("getAmountsOut", amountIn, path)
	if err != nil {
		return nil, err
	}
	to := V2Router
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil {
		return nil, err
	}
	vals, err := v2RouterABI.Unpack("getAmountsOut", out)
	if err != nil || len(vals) == 0 {
		return nil, fmt.Errorf("unpack getAmountsOut")
	}
	amounts, ok := vals[0].([]*big.Int)
	if !ok || len(amounts) < 2 {
		raw, ok2 := vals[0].([]interface{})
		if !ok2 || len(raw) < 2 {
			return nil, fmt.Errorf("bad amounts length")
		}
		last, ok3 := raw[len(raw)-1].(*big.Int)
		if !ok3 {
			return nil, fmt.Errorf("bad amount type")
		}
		return last, nil
	}
	return amounts[len(amounts)-1], nil
}

func applySlippage(expected *big.Int, bps int) *big.Int {
	if expected == nil {
		return big.NewInt(0)
	}
	num := new(big.Int).Mul(expected, big.NewInt(int64(10000-bps)))
	return num.Div(num, big.NewInt(10000))
}
