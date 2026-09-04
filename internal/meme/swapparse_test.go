package meme

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestSwapRecipientAndMemeBuyV2(t *testing.T) {
	meme := common.HexToAddress("0x1111111111111111111111111111111111111111")
	weth := WETH
	buyer := common.HexToAddress("0x2222222222222222222222222222222222222222")
	data := make([]byte, 128)
	// amount0Out = 1000 (meme is token0)
	data[95] = 0xe8
	data[94] = 0x03 // 1000
	lg := types.Log{
		Topics: []common.Hash{
			V2SwapTopic,
			common.BytesToHash(common.HexToAddress("0x3333333333333333333333333333333333333333").Bytes()),
			common.BytesToHash(buyer.Bytes()),
		},
		Data: data,
	}
	got, ok := swapRecipient(lg)
	if !ok || got != buyer {
		t.Fatalf("recipient %s ok=%v", got, ok)
	}
	if !isMemeTokenBuy(lg, meme, meme, weth) {
		t.Fatal("expected meme buy")
	}
	if isMemeTokenBuy(lg, meme, weth, meme) {
		t.Fatal("meme as token1 should not count amount0Out")
	}
}

func TestMemeBuyV3NegativeAmount(t *testing.T) {
	meme := common.HexToAddress("0x1111111111111111111111111111111111111111")
	weth := WETH
	buyer := common.HexToAddress("0x2222222222222222222222222222222222222222")
	data := make([]byte, 160)
	// amount0 = -100 (meme out of pool)
	neg := big.NewInt(-100)
	max := new(big.Int).Lsh(big.NewInt(1), 256)
	twos := new(big.Int).Add(neg, max)
	copy(data[0:32], common.LeftPadBytes(twos.Bytes(), 32))
	lg := types.Log{
		Topics: []common.Hash{
			V3SwapTopic,
			common.BytesToHash(common.HexToAddress("0x3333333333333333333333333333333333333333").Bytes()),
			common.BytesToHash(buyer.Bytes()),
		},
		Data: data,
	}
	if !isMemeTokenBuy(lg, meme, meme, weth) {
		t.Fatal("expected v3 meme buy")
	}
}
