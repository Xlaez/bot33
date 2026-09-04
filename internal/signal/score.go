package signal

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/xlaez/bot33/internal/store"
	"github.com/xlaez/bot33/internal/wallet"
)

// Input is everything needed to decide whether a Seaport sale is worth notifying.
type Input struct {
	Side            string
	PriceWei        *big.Int
	BuyerWatched    bool
	SellerWatched   bool
	BuyerSource     wallet.Source
	SellerSource    wallet.Source
	TrackedColl     bool // in active collections table (curated or hot)
	CollStats       store.CollectionSaleStats
	MinPriceWei     *big.Int // absolute floor; below this market alerts never fire
	NotifyMinScore  float64
	HeatMinSales    int
	PremiumMultiple float64 // price / median to count as premium print
}

type Decision struct {
	Notify  bool
	Score   float64
	Reasons []string
	Tag     string // CURATED / DISCOVERED / COLLECTION / HEAT / PREMIUM
}

func Evaluate(in Input) Decision {
	if in.PriceWei == nil {
		in.PriceWei = big.NewInt(0)
	}
	if in.MinPriceWei == nil {
		in.MinPriceWei = big.NewInt(0)
	}
	if in.NotifyMinScore <= 0 {
		in.NotifyMinScore = 60
	}
	if in.HeatMinSales <= 0 {
		in.HeatMinSales = 3
	}
	if in.PremiumMultiple <= 0 {
		in.PremiumMultiple = 1.5
	}

	var d Decision
	add := func(pts float64, reason, tag string) {
		d.Score += pts
		d.Reasons = append(d.Reasons, reason)
		if tag != "" && d.Tag == "" {
			d.Tag = tag
		}
	}

	// Wallet-centric: always worth a ping for watched counterparties on the relevant side.
	if in.Side == "buy" && in.BuyerWatched {
		tag := "CURATED"
		if in.BuyerSource == wallet.SourceDiscovered {
			tag = "DISCOVERED"
		}
		add(100, "watched-buyer", tag)
	}
	if in.Side == "sell" && in.SellerWatched {
		tag := "CURATED"
		if in.SellerSource == wallet.SourceDiscovered {
			tag = "DISCOVERED"
		}
		add(100, "watched-seller", tag)
	}

	// Collection-centric signals (buys only — sells are noisy unless watched).
	if in.Side == "buy" {
		aboveFloor := in.MinPriceWei.Sign() == 0 || in.PriceWei.Cmp(in.MinPriceWei) >= 0

		if in.TrackedColl && aboveFloor {
			add(55, "tracked-collection", "COLLECTION")
		}

		sales := in.CollStats.Sales
		buyers := in.CollStats.UniqueBuyers
		if sales >= in.HeatMinSales && buyers >= 2 && aboveFloor {
			add(30, fmt.Sprintf("heat sales=%d buyers=%d", sales, buyers), "HEAT")
		}
		if sales >= in.HeatMinSales*2 && aboveFloor {
			add(15, "heat-surge", "HEAT")
		}

		med := in.CollStats.MedianWei
		if med != nil && med.Sign() > 0 && in.PriceWei.Sign() > 0 {
			// price / median as float
			pf := new(big.Float).SetInt(in.PriceWei)
			mf := new(big.Float).SetInt(med)
			ratio, _ := new(big.Float).Quo(pf, mf).Float64()
			if ratio >= in.PremiumMultiple && aboveFloor {
				add(25, fmt.Sprintf("premium x%.2f median", ratio), "PREMIUM")
			}
			if ratio >= in.PremiumMultiple*2 && aboveFloor {
				add(15, "outlier-print", "PREMIUM")
			}
		}

		// First/early priced activity on a collection we have almost no history for.
		if sales <= 1 && aboveFloor && in.PriceWei.Cmp(in.MinPriceWei) >= 0 && in.MinPriceWei.Sign() > 0 {
			add(20, "early-collection-print", "COLLECTION")
		}

		// Absolute size still matters as a backstop, but alone is weaker than heat/premium.
		if aboveFloor && in.MinPriceWei.Sign() > 0 && in.PriceWei.Cmp(in.MinPriceWei) >= 0 {
			add(10, "above-min-price", "")
		}
	}

	if d.Tag == "" {
		d.Tag = "MARKET"
	}
	d.Notify = d.Score >= in.NotifyMinScore
	return d
}

func FormatReasons(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	return strings.Join(reasons, ", ")
}
