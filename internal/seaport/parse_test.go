package seaport

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestOrderFulfilledTopic(t *testing.T) {
	want := common.HexToHash("0x9d9af8e38d66c62e2c12f0225249fd9d721c54b83f48d9352c97c6cacdcb6f31")
	if OrderFulfilledTopic != want {
		t.Fatalf("topic mismatch got %s", OrderFulfilledTopic.Hex())
	}
}

func TestParseOrderFulfilledFixture(t *testing.T) {
	path := os.Getenv("SEAPORT_FIXTURE")
	if path == "" {
		path = "/tmp/seaport_sample.json"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skip("no fixture:", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	var topics []common.Hash
	for _, x := range raw["topics"].([]any) {
		topics = append(topics, common.HexToHash(x.(string)))
	}
	li := new(big.Int)
	li.SetString(raw["logIndex"].(string)[2:], 16)
	bn := new(big.Int)
	bn.SetString(raw["blockNumber"].(string)[2:], 16)
	lg := types.Log{
		Address:     common.HexToAddress(raw["address"].(string)),
		Topics:      topics,
		Data:        common.FromHex(raw["data"].(string)),
		TxHash:      common.HexToHash(raw["transactionHash"].(string)),
		BlockNumber: bn.Uint64(),
		Index:       uint(li.Uint64()),
	}
	sale, ok, err := ParseOrderFulfilled(lg)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || sale == nil {
		t.Fatal("expected sale")
	}
	if sale.Collection == (common.Address{}) {
		t.Fatal("missing collection")
	}
	if sale.Buyer == (common.Address{}) && sale.Seller == (common.Address{}) {
		t.Fatal("missing parties")
	}
	t.Logf("buyer=%s seller=%s coll=%s id=%s price=%s",
		sale.Buyer.Hex(), sale.Seller.Hex(), sale.Collection.Hex(), sale.TokenID.String(), sale.PriceWei.String())
}
