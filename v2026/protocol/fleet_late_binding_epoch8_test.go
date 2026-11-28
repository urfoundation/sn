package protocol

import "testing"

// The window's edges, from chain data: epoch 8's first block is 9,600,028 and
// block 9,615,061 is the first at or after 2026-11-29 20:40:00Z.
func TestEpoch8LateBindingWindowEdges(t *testing.T) {
	mainnet, coordinator := Epoch8LateBindingChainID, Epoch8LateBindingCoordinator
	cases := []struct {
		name      string
		validFrom uint64
		landed    uint64
		first     uint64
	}{
		{"epoch 8's first block counts for epoch 8", 9, 9_600_028, 8},
		{"the block after counts for epoch 8", 9, 9_600_029, 8},
		{"the window's last block counts for epoch 8", 9, 9_615_060, 8},
		{"the deadline block waits for epoch 9", 9, 9_615_061, 9},
		{"after the deadline waits for epoch 9", 9, 9_615_062, 9},
		{"late in epoch 8 waits for epoch 9", 9, 9_650_427, 9},
		{"a binding before epoch 8 signed from epoch 9 starts at epoch 9", 9, 9_600_027, 9},
		{"a binding before epoch 8 signed from epoch 8 starts at epoch 8", 8, 9_600_027, 8},
		{"a binding in the window signed from epoch 10 starts at epoch 10", 10, 9_600_029, 10},
		{"epoch 7's late binding starts at epoch 8", 8, 9_549_700, 8},
		{"epoch 9's late binding starts at epoch 10", 10, 9_650_500, 10},
	}
	for _, c := range cases {
		if got := FleetBindingFirstEpoch(mainnet, coordinator, c.validFrom, c.landed); got != c.first {
			t.Errorf("%s: first epoch %d, want %d", c.name, got, c.first)
		}
		if got, want := Epoch8LateBindingLanded(mainnet, coordinator, c.validFrom, c.landed), c.first == 8 && c.validFrom == 9; got != want {
			t.Errorf("%s: landed in the window %t, want %t", c.name, got, want)
		}
	}
}

// The window belongs to the SN25 mainnet coordinator alone.
func TestEpoch8LateBindingOnlyOnTheMainnetCoordinator(t *testing.T) {
	other := Epoch8LateBindingCoordinator
	other[19] ^= 1
	for _, c := range []struct {
		chainID     uint64
		coordinator [20]byte
	}{{945, Epoch8LateBindingCoordinator}, {Epoch8LateBindingChainID, other}, {0, [20]byte{}}} {
		if FleetBindingFirstEpoch(c.chainID, c.coordinator, 9, 9_600_029) != 9 || Epoch8LateBindingAppliesAt(c.chainID, c.coordinator, 8, 9_600_029) {
			t.Fatalf("chain %d coordinator %x has the window", c.chainID, c.coordinator)
		}
	}
}

// A FleetBound event in the window covers epoch 8 as well as its signed
// epochs; outside the window coverage is the signed validity.
func TestFleetBindingCoversEpochWithTheWindow(t *testing.T) {
	mainnet, coordinator := Epoch8LateBindingChainID, Epoch8LateBindingCoordinator
	if !FleetBindingCoversEpoch(mainnet, coordinator, 9, 20, 9_600_029, 8) || !FleetBindingCoversEpoch(mainnet, coordinator, 9, 20, 9_600_029, 9) {
		t.Fatal("a window binding does not cover epochs 8 and 9")
	}
	if FleetBindingCoversEpoch(mainnet, coordinator, 9, 20, 9_615_061, 8) || FleetBindingCoversEpoch(mainnet, coordinator, 9, 20, 9_600_027, 8) {
		t.Fatal("a binding outside the window covers epoch 8")
	}
	if FleetBindingCoversEpoch(mainnet, coordinator, 9, 20, 9_600_029, 7) || FleetBindingCoversEpoch(mainnet, coordinator, 9, 20, 9_600_029, 21) {
		t.Fatal("a window binding covers an epoch outside its validity")
	}
}

// The window applies only to reads of epoch 8 at blocks in or after epoch 8.
func TestEpoch8LateBindingAppliesAt(t *testing.T) {
	mainnet, coordinator := Epoch8LateBindingChainID, Epoch8LateBindingCoordinator
	for _, c := range []struct {
		epoch, block uint64
		want         bool
	}{{8, 9_600_027, false}, {8, 9_600_028, true}, {8, 9_650_428, true}, {7, 9_600_029, false}, {9, 9_600_029, false}, {9, 9_650_428, false}} {
		if got := Epoch8LateBindingAppliesAt(mainnet, coordinator, c.epoch, c.block); got != c.want {
			t.Errorf("epoch %d at block %d: applies %t, want %t", c.epoch, c.block, got, c.want)
		}
	}
}

// A window binding counts for epoch 8 while it is active for epoch 9, after a
// revocation that ends it with epoch 8, and after a later binding replaced it;
// it stops when it is cleaned in epoch 8 or loses its UID.
func TestEpoch8LateBindingInForce(t *testing.T) {
	for _, c := range []struct {
		name        string
		activeFrom9 bool
		validTo     uint64
		cleaned     bool
		cleanedAt   uint64
		replaced    bool
		want        bool
	}{
		{"active for epoch 9", true, 20, false, 0, false, true},
		{"revoked to end with epoch 8", false, 8, false, 0, false, true},
		{"revoked, then cleaned in epoch 9", false, 8, true, 9, false, true},
		{"replaced after the window", false, 8, false, 0, true, true},
		{"its hotkey lost its UID", false, 20, false, 0, false, false},
		{"cleaned in epoch 8", false, 20, true, 8, false, false},
		{"replaced after a cleanup in epoch 8", false, 20, true, 8, true, false},
	} {
		if got := Epoch8LateBindingInForce(c.activeFrom9, c.validTo, c.cleaned, c.cleanedAt, c.replaced); got != c.want {
			t.Errorf("%s: in force %t, want %t", c.name, got, c.want)
		}
	}
}
