package discover

import "testing"

func TestScoreAndBotFilter(t *testing.T) {
	good := walletStats{
		wallet:      "0xabc",
		trades:      12,
		collections: 4,
		buyCount:    6,
		sellCount:   4,
		mintCount:   2,
		paidTrades:  8,
	}
	if IsBotLike(good) {
		t.Fatal("good wallet should not be botlike")
	}
	if reason := WashReason(good); reason != "" {
		t.Fatalf("good wallet wash reason %s", reason)
	}
	if Score(good) < 70 {
		t.Fatalf("expected high score got %f", Score(good))
	}
	bot := walletStats{wallet: "0xbot", trades: 80, collections: 1, buyCount: 40, sellCount: 40}
	if !IsBotLike(bot) {
		t.Fatal("expected botlike")
	}
}

func TestWashHeuristics(t *testing.T) {
	recip := walletStats{
		wallet:         "0xwash",
		trades:         20,
		collections:    2,
		buyCount:       10,
		sellCount:      10,
		reciprocalHits: 12,
		uniqueCounter:  1,
	}
	if got := WashReason(recip); got != "reciprocal-pair" && got != "single-counterparty" {
		t.Fatalf("expected reciprocal wash, got %q", got)
	}
	flips := walletStats{
		wallet:         "0xflip",
		trades:         10,
		collections:    2,
		buyCount:       5,
		sellCount:      5,
		sameTokenFlip:  6,
		zeroValueRatio: 0.9,
		paidTrades:     1,
	}
	if got := WashReason(flips); got != "zero-value-flips" {
		t.Fatalf("expected zero-value-flips, got %q", got)
	}
	churn := walletStats{
		wallet:      "0xchurn",
		trades:      25,
		collections: 2,
		buyCount:    12,
		sellCount:   13,
		paidTrades:  0,
	}
	if got := WashReason(churn); got != "unpriced-churn" {
		t.Fatalf("expected unpriced-churn, got %q", got)
	}
}
