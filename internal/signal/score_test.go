package signal

import (
	"math/big"
	"testing"

	"github.com/xlaez/bot33/internal/store"
	"github.com/xlaez/bot33/internal/wallet"
)

func wei(eth float64) *big.Int {
	f := new(big.Float).Mul(big.NewFloat(eth), big.NewFloat(1e18))
	out, _ := f.Int(nil)
	return out
}

func TestEvaluateWatchedBuyerAlways(t *testing.T) {
	d := Evaluate(Input{
		Side:           "buy",
		PriceWei:       wei(0.001),
		BuyerWatched:   true,
		BuyerSource:    wallet.SourceCurated,
		MinPriceWei:    wei(0.01),
		NotifyMinScore: 60,
	})
	if !d.Notify || d.Tag != "CURATED" {
		t.Fatalf("expected curated notify got %+v", d)
	}
}

func TestEvaluateHeatAndTracked(t *testing.T) {
	d := Evaluate(Input{
		Side:         "buy",
		PriceWei:     wei(0.02),
		TrackedColl:  true,
		MinPriceWei:  wei(0.01),
		HeatMinSales: 3,
		CollStats: store.CollectionSaleStats{
			Sales:        5,
			UniqueBuyers: 4,
			MedianWei:    wei(0.015),
			AvgWei:       wei(0.015),
			MaxWei:       wei(0.03),
		},
		NotifyMinScore:  60,
		PremiumMultiple: 1.5,
	})
	// tracked 55 + above-min 10 = 65
	if !d.Notify {
		t.Fatalf("expected notify score=%v reasons=%v", d.Score, d.Reasons)
	}
	if d.Score < 60 {
		t.Fatalf("score too low %v", d.Score)
	}
}

func TestEvaluateIgnoresCheapNoise(t *testing.T) {
	d := Evaluate(Input{
		Side:         "buy",
		PriceWei:     wei(0.0001),
		TrackedColl:  true,
		MinPriceWei:  wei(0.01),
		HeatMinSales: 3,
		CollStats: store.CollectionSaleStats{
			Sales:        20,
			UniqueBuyers: 10,
			MedianWei:    wei(0.0002),
		},
		NotifyMinScore: 60,
	})
	if d.Notify {
		t.Fatalf("cheap noise should not notify: %+v", d)
	}
}

func TestEvaluatePremiumPrint(t *testing.T) {
	d := Evaluate(Input{
		Side:        "buy",
		PriceWei:    wei(0.05),
		MinPriceWei: wei(0.01),
		CollStats: store.CollectionSaleStats{
			Sales:        4,
			UniqueBuyers: 3,
			MedianWei:    wei(0.02),
		},
		NotifyMinScore:  60,
		PremiumMultiple: 1.5,
		HeatMinSales:    3,
	})
	// heat 30 + premium 25 + above-min 10 = 65
	if !d.Notify {
		t.Fatalf("expected premium/heat notify got %+v", d)
	}
}
