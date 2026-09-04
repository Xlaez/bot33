package meme

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
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

type SwapPlan struct {
	To       common.Address
	Data     []byte
	Value    *big.Int
	MinOut   *big.Int
	Dex      string
	Deadline uint64
}

// BuildBuyPlan builds a native-ETH → meme token swap plan (V2 preferred, then V3).
func BuildBuyPlan(ctx context.Context, client *ethclient.Client, token common.Address, dex string, feeTier int, amountIn *big.Int, slippageBps int, recipient common.Address) (*SwapPlan, error) {
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
		return buildV3Buy(ctx, client, token, feeTier, amountIn, slippageBps, recipient)
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
		// fallback non-supporting
		data, err = v2RouterABI.Pack("swapExactETHForTokens", minOut, path, recipient, new(big.Int).SetUint64(deadline))
		if err != nil {
			return nil, err
		}
	}
	return &SwapPlan{
		To: V2Router, Data: data, Value: amountIn, MinOut: minOut, Dex: "uniswap-v2", Deadline: deadline,
	}, nil
}

func buildV3Buy(ctx context.Context, client *ethclient.Client, token common.Address, feeTier int, amountIn *big.Int, slippageBps int, recipient common.Address) (*SwapPlan, error) {
	if feeTier <= 0 {
		feeTier = 3000
	}
	// Quote via eth_call with amountOutMinimum=0 then apply slippage to returned amount.
	probe := exactInputSingleArgs{
		TokenIn:           WETH,
		TokenOut:          token,
		Fee:               uint32(feeTier),
		Recipient:         recipient,
		AmountIn:          amountIn,
		AmountOutMinimum:  big.NewInt(0),
		SqrtPriceLimitX96: big.NewInt(0),
	}
	data, err := v3RouterABI.Pack("exactInputSingle", probe)
	if err != nil {
		return nil, err
	}
	to := V3SwapRouter02
	out, err := client.CallContract(ctx, ethereum.CallMsg{From: recipient, To: &to, Value: amountIn, Data: data}, nil)
	if err != nil {
		return nil, fmt.Errorf("v3 quote: %w", err)
	}
	vals, err := v3RouterABI.Unpack("exactInputSingle", out)
	if err != nil || len(vals) == 0 {
		return nil, fmt.Errorf("v3 unpack quote")
	}
	expected, ok := vals[0].(*big.Int)
	if !ok || expected.Sign() <= 0 {
		return nil, fmt.Errorf("v3 zero quote")
	}
	minOut := applySlippage(expected, slippageBps)
	live := exactInputSingleArgs{
		TokenIn: WETH, TokenOut: token, Fee: uint32(feeTier), Recipient: recipient,
		AmountIn: amountIn, AmountOutMinimum: minOut, SqrtPriceLimitX96: big.NewInt(0),
	}
	data, err = v3RouterABI.Pack("exactInputSingle", live)
	if err != nil {
		return nil, err
	}
	return &SwapPlan{
		To: V3SwapRouter02, Data: data, Value: amountIn, MinOut: minOut, Dex: "uniswap-v3",
	}, nil
}

type exactInputSingleArgs struct {
	TokenIn           common.Address
	TokenOut          common.Address
	Fee               uint32
	Recipient         common.Address
	AmountIn          *big.Int
	AmountOutMinimum  *big.Int
	SqrtPriceLimitX96 *big.Int
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
		// some ABIs decode as []interface{}
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
