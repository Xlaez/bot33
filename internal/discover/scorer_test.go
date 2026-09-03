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
	}
	if IsBotLike(good) {
		t.Fatal("good wallet should not be botlike")
	}
	if Score(good) < 70 {
		t.Fatalf("expected high score got %f", Score(good))
	}
	bot := walletStats{wallet: "0xbot", trades: 80, collections: 1, buyCount: 40, sellCount: 40}
	if !IsBotLike(bot) {
		t.Fatal("expected botlike")
	}
}
