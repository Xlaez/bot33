package seadrop

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

// Canonical OpenSea SeaDrop singleton (same address on supported EVM chains).
var Address = common.HexToAddress("0x00005EA00Ac477B1030CE78506496e8C2dE24bf5")

var OpenSeaFeeRecipient = common.HexToAddress("0x0000a26b00c1F0DF003000390027140000fAa719")

const publicABIJSON = `[
  {"inputs":[{"name":"nftContract","type":"address"},{"name":"feeRecipient","type":"address"},{"name":"minterIfNotPayer","type":"address"},{"name":"quantity","type":"uint256"}],"name":"mintPublic","outputs":[],"stateMutability":"payable","type":"function"},
  {"inputs":[{"name":"nftContract","type":"address"}],"name":"getPublicDrop","outputs":[{"components":[{"name":"mintPrice","type":"uint80"},{"name":"startTime","type":"uint48"},{"name":"endTime","type":"uint48"},{"name":"maxTotalMintableByWallet","type":"uint16"},{"name":"feeBps","type":"uint16"},{"name":"restrictFeeRecipients","type":"bool"}],"name":"","type":"tuple"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"nftContract","type":"address"}],"name":"getAllowedFeeRecipients","outputs":[{"name":"","type":"address[]"}],"stateMutability":"view","type":"function"}
]`

var publicABI = mustABI(publicABIJSON)

type PublicDrop struct {
	MintPrice                *big.Int
	StartTime                uint64
	EndTime                  uint64
	MaxTotalMintableByWallet uint16
	FeeBps                   uint16
	RestrictFeeRecipients    bool
}

type Plan struct {
	To           common.Address
	Data         []byte
	Value        *big.Int
	Drop         PublicDrop
	FeeRecipient common.Address
	NFT          common.Address
	Quantity     uint64
}

func FetchPublicDrop(ctx context.Context, client *ethclient.Client, nft common.Address) (*PublicDrop, error) {
	data, err := publicABI.Pack("getPublicDrop", nft)
	if err != nil {
		return nil, err
	}
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &Address, Data: data}, nil)
	if err != nil {
		return nil, err
	}
	if len(out) < 192 {
		return nil, fmt.Errorf("getPublicDrop short response")
	}
	price := new(big.Int).SetBytes(out[0:32])
	start := new(big.Int).SetBytes(out[32:64]).Uint64()
	end := new(big.Int).SetBytes(out[64:96]).Uint64()
	maxW := uint16(new(big.Int).SetBytes(out[96:128]).Uint64())
	fee := uint16(new(big.Int).SetBytes(out[128:160]).Uint64())
	restricted := new(big.Int).SetBytes(out[160:192]).Sign() != 0
	if start == 0 && end == 0 && maxW == 0 {
		return nil, nil
	}
	return &PublicDrop{
		MintPrice:                price,
		StartTime:                start,
		EndTime:                  end,
		MaxTotalMintableByWallet: maxW,
		FeeBps:                   fee,
		RestrictFeeRecipients:    restricted,
	}, nil
}

func ResolveFeeRecipient(ctx context.Context, client *ethclient.Client, nft common.Address, restricted bool) (common.Address, error) {
	data, err := publicABI.Pack("getAllowedFeeRecipients", nft)
	if err != nil {
		return common.Address{}, err
	}
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &Address, Data: data}, nil)
	if err == nil && len(out) > 0 {
		vals, err := publicABI.Unpack("getAllowedFeeRecipients", out)
		if err == nil && len(vals) > 0 {
			if addrs, ok := vals[0].([]common.Address); ok && len(addrs) > 0 {
				return addrs[0], nil
			}
		}
	}
	if restricted {
		return common.Address{}, fmt.Errorf("drop restricts fee recipients but none allowed")
	}
	return OpenSeaFeeRecipient, nil
}

func BuildPlan(ctx context.Context, client *ethclient.Client, nft common.Address, quantity uint64) (*Plan, error) {
	if quantity == 0 {
		return nil, fmt.Errorf("quantity must be > 0")
	}
	drop, err := FetchPublicDrop(ctx, client, nft)
	if err != nil {
		return nil, err
	}
	if drop == nil {
		return nil, fmt.Errorf("no public SeaDrop configured for %s", nft.Hex())
	}
	fee, err := ResolveFeeRecipient(ctx, client, nft, drop.RestrictFeeRecipients)
	if err != nil {
		return nil, err
	}
	if drop.MaxTotalMintableByWallet > 0 && quantity > uint64(drop.MaxTotalMintableByWallet) {
		return nil, fmt.Errorf("quantity %d exceeds per-wallet max %d", quantity, drop.MaxTotalMintableByWallet)
	}
	data, err := publicABI.Pack(
		"mintPublic",
		nft,
		fee,
		common.Address{},
		new(big.Int).SetUint64(quantity),
	)
	if err != nil {
		return nil, err
	}
	value := new(big.Int).Mul(drop.MintPrice, new(big.Int).SetUint64(quantity))
	return &Plan{
		To:           Address,
		Data:         data,
		Value:        value,
		Drop:         *drop,
		FeeRecipient: fee,
		NFT:          nft,
		Quantity:     quantity,
	}, nil
}

func mustABI(raw string) abi.ABI {
	a, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		panic(err)
	}
	return a
}
