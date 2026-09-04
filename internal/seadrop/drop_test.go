package seadrop

import (
	"math/big"
	"testing"
	"time"
)

func TestPublicDropIsFreeAndOpen(t *testing.T) {
	now := uint64(time.Now().Unix())
	free := &PublicDrop{
		MintPrice: big.NewInt(0), StartTime: now - 60, EndTime: now + 3600, MaxTotalMintableByWallet: 10,
	}
	if !free.IsFree() || !free.IsOpen(now) {
		t.Fatal("expected free+open")
	}
	paid := &PublicDrop{
		MintPrice: big.NewInt(1e15), StartTime: now - 60, EndTime: now + 3600, MaxTotalMintableByWallet: 5,
	}
	if paid.IsFree() {
		t.Fatal("expected paid")
	}
	closed := &PublicDrop{
		MintPrice: big.NewInt(0), StartTime: now - 7200, EndTime: now - 60, MaxTotalMintableByWallet: 10,
	}
	if closed.IsOpen(now) {
		t.Fatal("expected closed")
	}
}
