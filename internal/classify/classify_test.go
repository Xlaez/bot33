package classify

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestClassifyAction(t *testing.T) {
	if got := classifyAction(ZeroAddress, common.HexToAddress("0x1111111111111111111111111111111111111111")); got != ActionMint {
		t.Fatalf("want mint got %s", got)
	}
	if got := classifyAction(common.HexToAddress("0x1111111111111111111111111111111111111111"), ZeroAddress); got != ActionTransfer {
		t.Fatalf("want transfer got %s", got)
	}
}

func TestMatchWatch(t *testing.T) {
	watch := map[string]struct{}{
		"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": {},
	}
	ev := &Event{
		From: ZeroAddress,
		To:   common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}
	addr, action, ok := MatchWatch(ev, watch, false)
	if !ok || action != ActionMint || addr != "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("unexpected match %#v %s %v", addr, action, ok)
	}

	ev2 := &Event{
		From: common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		To:   common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	}
	_, _, ok = MatchWatch(ev2, watch, false)
	if ok {
		t.Fatal("sell should be ignored when alertOnSell=false")
	}
	_, action, ok = MatchWatch(ev2, watch, true)
	if !ok || action != ActionSell {
		t.Fatalf("want sell got %s ok=%v", action, ok)
	}
}
