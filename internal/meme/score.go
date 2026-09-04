package meme

import (
	"math"
	"math/big"
	"time"
)

type ScoreInput struct {
	LPLocked       bool
	LPLockPct      float64
	OwnerRenounced bool
	HasLiquidity   bool
	Age            time.Duration
	SwapCount      int
	UniqueTraders  int
	VolumeWei      *big.Int
	QuoteIsWETH    bool
}

type ScoreResult struct {
	Score   float64
	Flags   []string
	AlertOK bool // locked LP required
}

// Score applies non-scam green flags. Locked LP is a hard gate for alerts.
func Score(in ScoreInput) ScoreResult {
	var flags []string
	score := 0.0
	add := func(pts float64, flag string) {
		score += pts
		if flag != "" {
			flags = append(flags, flag)
		}
	}

	if !in.HasLiquidity {
		return ScoreResult{Score: 0, Flags: []string{"no-liquidity"}, AlertOK: false}
	}
	if in.Age > 30*24*time.Hour {
		return ScoreResult{Score: 0, Flags: []string{"too-old"}, AlertOK: false}
	}

	if in.LPLocked {
		add(40, "lp-locked")
		if in.LPLockPct >= 99 {
			add(10, "lp-fully-burned")
		}
	} else {
		flags = append(flags, "lp-unlocked")
	}

	if in.OwnerRenounced {
		add(15, "owner-renounced")
	} else {
		flags = append(flags, "owner-active")
	}

	if in.QuoteIsWETH {
		add(5, "weth-pair")
	}

	if in.SwapCount >= 5 {
		add(10, "early-volume")
	}
	if in.SwapCount >= 25 {
		add(10, "sustained-swaps")
	}
	if in.UniqueTraders >= 5 {
		add(10, "unique-traders")
	}
	if in.UniqueTraders >= 20 {
		add(5, "broad-traders")
	}

	if in.VolumeWei != nil && in.VolumeWei.Cmp(big.NewInt(0)) > 0 {
		// Soft boost once volume crosses ~0.05 ETH equivalent units (raw wei).
		threshold := new(big.Int).Mul(big.NewInt(5), big.NewInt(1e16))
		if in.VolumeWei.Cmp(threshold) >= 0 {
			add(10, "volume-floor")
		}
	}

	if in.Age > 0 && in.Age < 15*time.Minute {
		add(5, "fresh-launch")
	} else if in.Age >= 1*time.Hour && in.Age <= 7*24*time.Hour {
		add(5, "seasoned")
	}

	if score > 99 {
		score = 99
	}
	score = math.Round(score*10) / 10

	return ScoreResult{
		Score:   score,
		Flags:   flags,
		AlertOK: in.LPLocked && score >= AlertMinScore,
	}
}
