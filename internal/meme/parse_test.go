package meme

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestParseV3PoolCreatedIndexedFee(t *testing.T) {
	token0 := common.HexToAddress("0xbbd09f72b025360fee5c928053dca6248d35be54")
	token1 := WETH
	fee := big.NewInt(3000)
	pool := common.HexToAddress("0x0dbfebbeaf88e0c32e93ea5b35efc3735b5ebab4")
	tickSpacing := make([]byte, 32)
	tickSpacing[31] = 60
	poolWord := common.LeftPadBytes(pool.Bytes(), 32)

	lg := types.Log{
		Topics: []common.Hash{
			PoolCreatedTopic,
			common.BytesToHash(token0.Bytes()),
			common.BytesToHash(token1.Bytes()),
			common.BigToHash(fee),
		},
		Data: append(tickSpacing, poolWord...),
	}
	if len(lg.Topics) < 4 || len(lg.Data) < 64 {
		t.Fatal("fixture invalid")
	}
	got0 := common.BytesToAddress(lg.Topics[1].Bytes())
	got1 := common.BytesToAddress(lg.Topics[2].Bytes())
	gotFee := new(big.Int).SetBytes(lg.Topics[3].Bytes())
	gotPool := common.BytesToAddress(lg.Data[32:64])
	if got0 != token0 || got1 != token1 || gotFee.Cmp(fee) != 0 || gotPool != pool {
		t.Fatalf("parse mismatch: %s %s fee=%s pool=%s", got0, got1, gotFee, gotPool)
	}
	meme, quote, ok := classifyPair(got0, got1)
	if !ok || meme != token0 || quote != WETH {
		t.Fatalf("classify: meme=%s quote=%s ok=%v", meme, quote, ok)
	}
}

func TestParseV4InitializeIndexedCurrencies(t *testing.T) {
	id := common.HexToHash("0xf53c0917b1a634943a3a189ccde1e71f1a8a14a942b2e3cce5f48fb3bd85c8fa")
	c0 := common.HexToAddress("0x323a9f0d03736a6a4853989d87666a724f055601")
	c1 := ZeroAddress // native ETH quote
	fee := big.NewInt(0)
	data := make([]byte, 160)
	copy(data[0:32], common.LeftPadBytes(fee.Bytes(), 32))

	lg := types.Log{
		Topics: []common.Hash{
			V4InitializeTopic,
			id,
			common.BytesToHash(c0.Bytes()),
			common.BytesToHash(c1.Bytes()),
		},
		Data: data,
	}
	if len(lg.Topics) < 4 || len(lg.Data) < 96 {
		t.Fatal("fixture would be rejected")
	}
	gotID := lg.Topics[1].Hex()
	got0 := common.BytesToAddress(lg.Topics[2].Bytes())
	got1 := common.BytesToAddress(lg.Topics[3].Bytes())
	gotFee := new(big.Int).SetBytes(lg.Data[0:32])
	if gotID != id.Hex() || got0 != c0 || got1 != c1 || gotFee.Cmp(fee) != 0 {
		t.Fatalf("parse mismatch")
	}
	meme, quote, ok := classifyPair(got0, got1)
	if !ok || meme != c0 || quote != ZeroAddress {
		t.Fatalf("classify native: meme=%s quote=%s ok=%v", meme, quote, ok)
	}
}
