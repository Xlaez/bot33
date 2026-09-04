package meme

import (
	"math/big"
	"testing"
	"time"
)

func TestScoreRequiresLockedLPForAlert(t *testing.T) {
	unlocked := Score(ScoreInput{
		LPLocked: false, HasLiquidity: true, OwnerRenounced: true,
		SwapCount: 50, UniqueTraders: 30, QuoteIsWETH: true,
		VolumeWei: new(big.Int).Mul(big.NewInt(1), big.NewInt(1e18)),
		Age:       time.Hour,
	})
	if unlocked.AlertOK {
		t.Fatal("unlocked LP must not alert")
	}
	locked := Score(ScoreInput{
		LPLocked: true, LPLockPct: 100, HasLiquidity: true, OwnerRenounced: true,
		SwapCount: 30, UniqueTraders: 20, QuoteIsWETH: true,
		VolumeWei: new(big.Int).Mul(big.NewInt(1), big.NewInt(1e18)),
		Age:       2 * time.Hour,
	})
	if !locked.AlertOK {
		t.Fatalf("expected alert ok score=%v flags=%v", locked.Score, locked.Flags)
	}
}

func TestScoreRejectsOld(t *testing.T) {
	got := Score(ScoreInput{
		LPLocked: true, HasLiquidity: true, Age: 40 * 24 * time.Hour,
	})
	if got.AlertOK || got.Score != 0 {
		t.Fatalf("old token should be rejected: %+v", got)
	}
}
