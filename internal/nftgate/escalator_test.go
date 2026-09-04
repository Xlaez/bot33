package nftgate

import (
	"testing"
	"time"

	"github.com/xlaez/bot33/internal/seadrop"
	"math/big"
)

func TestDefaults(t *testing.T) {
	if DefaultSmartWalletMin != 2 {
		t.Fatalf("smart wallet min want 2 got %d", DefaultSmartWalletMin)
	}
	if DefaultMintMaxTotal != 20 {
		t.Fatalf("max total want 20 got %d", DefaultMintMaxTotal)
	}
	if DefaultMintWindow != 2*time.Hour || DefaultBuyWindow != 6*time.Hour {
		t.Fatal("unexpected windows")
	}
}

func TestFreeOpenGates(t *testing.T) {
	now := uint64(time.Now().Unix())
	d := &seadrop.PublicDrop{
		MintPrice: big.NewInt(0), StartTime: now - 10, EndTime: now + 1000, MaxTotalMintableByWallet: 10,
	}
	if !d.IsFree() || !d.IsOpen(now) {
		t.Fatal("free open gate")
	}
}
