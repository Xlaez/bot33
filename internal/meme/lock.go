package meme

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

var erc20ABI = mustABI(`[
  {"inputs":[],"name":"totalSupply","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"account","type":"address"}],"name":"balanceOf","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[],"name":"owner","outputs":[{"type":"address"}],"stateMutability":"view","type":"function"},
  {"inputs":[],"name":"getOwner","outputs":[{"type":"address"}],"stateMutability":"view","type":"function"},
  {"inputs":[],"name":"symbol","outputs":[{"type":"string"}],"stateMutability":"view","type":"function"},
  {"inputs":[],"name":"name","outputs":[{"type":"string"}],"stateMutability":"view","type":"function"},
  {"inputs":[],"name":"decimals","outputs":[{"type":"uint8"}],"stateMutability":"view","type":"function"}
]`)

type LockResult struct {
	Locked   bool
	Pct      float64
	Evidence string
}

// CheckV2LPLock treats LP as locked when >= threshold of pair totalSupply sits in burn/lock sinks.
func CheckV2LPLock(ctx context.Context, client *ethclient.Client, pair common.Address) (LockResult, error) {
	supply, err := callUint(ctx, client, pair, "totalSupply")
	if err != nil {
		return LockResult{}, err
	}
	if supply.Sign() == 0 {
		return LockResult{Locked: false, Evidence: "zero-lp-supply"}, nil
	}
	locked := big.NewInt(0)
	parts := make([]string, 0, len(LPLockAddresses))
	for _, sink := range LPLockAddresses {
		bal, err := callBalance(ctx, client, pair, sink)
		if err != nil {
			continue
		}
		if bal.Sign() > 0 {
			locked.Add(locked, bal)
			parts = append(parts, fmt.Sprintf("%s=%s", short(sink), bal.String()))
		}
	}
	pct := percent(locked, supply)
	ev := strings.Join(parts, ",")
	if ev == "" {
		ev = "no-lp-in-burn-addresses"
	}
	return LockResult{
		Locked:   pct >= LPLockThresholdPct,
		Pct:      pct,
		Evidence: fmt.Sprintf("v2_lp_lock_pct=%.2f [%s]", pct, ev),
	}, nil
}

// CheckV3LPLock approximates lock by counting position NFTs held by burn addresses.
// True V3 "lock" often means the position NFT is in a locker or dead address.
func CheckV3LPLock(ctx context.Context, client *ethclient.Client, positionMgr common.Address) (LockResult, error) {
	deadBal, err := callBalance(ctx, client, positionMgr, DeadAddress)
	if err != nil {
		return LockResult{}, err
	}
	zeroBal, _ := callBalance(ctx, client, positionMgr, ZeroAddress)
	total := new(big.Int).Add(deadBal, zeroBal)
	if total.Sign() > 0 {
		return LockResult{
			Locked:   true,
			Pct:      100,
			Evidence: fmt.Sprintf("v3_position_nft_burned dead=%s zero=%s", deadBal.String(), zeroBal.String()),
		}, nil
	}
	return LockResult{Locked: false, Pct: 0, Evidence: "v3_no_burned_position_nft"}, nil
}

func OwnerRenounced(ctx context.Context, client *ethclient.Client, token common.Address) bool {
	for _, fn := range []string{"owner", "getOwner"} {
		addr, err := callAddress(ctx, client, token, fn)
		if err != nil {
			continue
		}
		if addr == ZeroAddress || addr == DeadAddress {
			return true
		}
		return false
	}
	// No owner() — treat as renounced/immutable for meme scoring purposes.
	return true
}

type TokenMeta struct {
	Symbol   string
	Name     string
	Decimals int
}

func FetchTokenMeta(ctx context.Context, client *ethclient.Client, token common.Address) TokenMeta {
	meta := TokenMeta{Symbol: short(token), Name: short(token), Decimals: 18}
	if s, err := callString(ctx, client, token, "symbol"); err == nil && s != "" {
		meta.Symbol = s
	}
	if n, err := callString(ctx, client, token, "name"); err == nil && n != "" {
		meta.Name = n
	}
	if d, err := callUint(ctx, client, token, "decimals"); err == nil {
		meta.Decimals = int(d.Int64())
	}
	return meta
}

func callUint(ctx context.Context, client *ethclient.Client, to common.Address, method string) (*big.Int, error) {
	data, err := erc20ABI.Pack(method)
	if err != nil {
		return nil, err
	}
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil {
		return nil, err
	}
	vals, err := erc20ABI.Unpack(method, out)
	if err != nil || len(vals) == 0 {
		return nil, fmt.Errorf("unpack %s", method)
	}
	switch v := vals[0].(type) {
	case *big.Int:
		return v, nil
	case uint8:
		return big.NewInt(int64(v)), nil
	default:
		return nil, fmt.Errorf("unexpected type for %s", method)
	}
}

func callBalance(ctx context.Context, client *ethclient.Client, token, account common.Address) (*big.Int, error) {
	data, err := erc20ABI.Pack("balanceOf", account)
	if err != nil {
		return nil, err
	}
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &token, Data: data}, nil)
	if err != nil {
		return nil, err
	}
	vals, err := erc20ABI.Unpack("balanceOf", out)
	if err != nil || len(vals) == 0 {
		return nil, fmt.Errorf("unpack balanceOf")
	}
	v, ok := vals[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("bad balanceOf type")
	}
	return v, nil
}

func callAddress(ctx context.Context, client *ethclient.Client, to common.Address, method string) (common.Address, error) {
	data, err := erc20ABI.Pack(method)
	if err != nil {
		return common.Address{}, err
	}
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil {
		return common.Address{}, err
	}
	vals, err := erc20ABI.Unpack(method, out)
	if err != nil || len(vals) == 0 {
		return common.Address{}, fmt.Errorf("unpack %s", method)
	}
	addr, ok := vals[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("bad address type")
	}
	return addr, nil
}

func callString(ctx context.Context, client *ethclient.Client, to common.Address, method string) (string, error) {
	data, err := erc20ABI.Pack(method)
	if err != nil {
		return "", err
	}
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil {
		return "", err
	}
	vals, err := erc20ABI.Unpack(method, out)
	if err != nil || len(vals) == 0 {
		return "", fmt.Errorf("unpack %s", method)
	}
	s, ok := vals[0].(string)
	if !ok {
		return "", fmt.Errorf("bad string type")
	}
	return s, nil
}

func percent(part, whole *big.Int) float64 {
	if whole == nil || whole.Sign() == 0 {
		return 0
	}
	// (part * 10000 / whole) / 100
	scaled := new(big.Int).Mul(part, big.NewInt(10000))
	scaled.Div(scaled, whole)
	return float64(scaled.Int64()) / 100.0
}

func short(a common.Address) string {
	h := a.Hex()
	if len(h) < 10 {
		return h
	}
	return h[:6] + "…" + h[len(h)-4:]
}

func mustABI(raw string) abi.ABI {
	a, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		panic(err)
	}
	return a
}
