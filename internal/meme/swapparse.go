package meme

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// swapRecipient returns the token recipient for a Uniswap V2/V3 Swap log.
func swapRecipient(lg types.Log) (common.Address, bool) {
	if len(lg.Topics) < 3 {
		return common.Address{}, false
	}
	switch lg.Topics[0] {
	case V2SwapTopic, V3SwapTopic:
		return common.BytesToAddress(lg.Topics[2].Bytes()), true
	default:
		return common.Address{}, false
	}
}

// isMemeTokenBuy reports whether the swap delivers meme tokens to the recipient.
func isMemeTokenBuy(lg types.Log, memeToken, token0, token1 common.Address) bool {
	memeIs0 := memeToken == token0
	memeIs1 := memeToken == token1
	if !memeIs0 && !memeIs1 {
		return false
	}
	switch lg.Topics[0] {
	case V2SwapTopic:
		// data: amount0In, amount1In, amount0Out, amount1Out
		if len(lg.Data) < 128 {
			return false
		}
		amount0Out := new(big.Int).SetBytes(lg.Data[64:96])
		amount1Out := new(big.Int).SetBytes(lg.Data[96:128])
		if memeIs0 {
			return amount0Out.Sign() > 0
		}
		return amount1Out.Sign() > 0
	case V3SwapTopic:
		// data: amount0, amount1, sqrtPriceX96, liquidity, tick (int256/int24 packed)
		if len(lg.Data) < 64 {
			return false
		}
		amount0 := signed256(lg.Data[0:32])
		amount1 := signed256(lg.Data[32:64])
		// Negative amount means tokens leaving the pool to the recipient.
		if memeIs0 {
			return amount0.Sign() < 0
		}
		return amount1.Sign() < 0
	default:
		return false
	}
}

func signed256(b []byte) *big.Int {
	v := new(big.Int).SetBytes(b)
	if len(b) > 0 && b[0]&0x80 != 0 {
		// two's complement negative
		max := new(big.Int).Lsh(big.NewInt(1), 256)
		v.Sub(v, max)
	}
	return v
}
