package protocol

import "testing"

func TestAllocateSharesLargestRemainder(t *testing.T) {
	var a, b, c [16]byte
	a[15], b[15], c[15] = 1, 2, 3
	var ca, cb, cc [32]byte
	ca[31], cb[31], cc[31] = 1, 2, 3
	shares, err := AllocateShares([]ProviderAllocation{
		{ClientID: c, Coldkey: cc, UsageBytes: 1, ReliabilityPPM: 1_000_000, Eligible: true},
		{ClientID: b, Coldkey: cb, UsageBytes: 1, ReliabilityPPM: 1_000_000, Eligible: true},
		{ClientID: a, Coldkey: ca, UsageBytes: 1, ReliabilityPPM: 1_000_000, Eligible: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 3 || shares[0].ClientID != a || shares[0].ShareBPS != 3334 || shares[1].ShareBPS != 3333 || shares[2].ShareBPS != 3333 {
		t.Fatalf("unexpected allocation: %#v", shares)
	}
}

func TestAllocateSharesExcludesHeadAndRequiresWeight(t *testing.T) {
	var id [16]byte
	id[0] = 1
	var cold [32]byte
	cold[0] = 1
	if _, err := AllocateShares([]ProviderAllocation{{ClientID: id, Coldkey: cold, UsageBytes: 1, ReliabilityPPM: 1, Eligible: true, HeadExcluded: true}}); err == nil {
		t.Fatal("head-only input accepted")
	}
	shares, err := AllocateShares([]ProviderAllocation{
		{ClientID: id, Coldkey: cold, UsageBytes: 10, ReliabilityPPM: 500_000, Eligible: true},
		{ClientID: [16]byte{2}, Coldkey: [32]byte{2}, UsageBytes: 10, ReliabilityPPM: 1_000_000, Eligible: false},
	})
	if err != nil || len(shares) != 1 || shares[0].ShareBPS != 10_000 {
		t.Fatalf("unexpected: %#v %v", shares, err)
	}
}

func TestAllocateSharesAggregatesSharedColdkey(t *testing.T) {
	shared := [32]byte{9}
	other := [32]byte{8}
	shares, err := AllocateShares([]ProviderAllocation{
		{ClientID: [16]byte{2}, Coldkey: shared, UsageBytes: 1, ReliabilityPPM: 1_000_000, Eligible: true},
		{ClientID: [16]byte{1}, Coldkey: shared, UsageBytes: 1, ReliabilityPPM: 1_000_000, Eligible: true},
		{ClientID: [16]byte{3}, Coldkey: other, UsageBytes: 1, ReliabilityPPM: 1_000_000, Eligible: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 2 {
		t.Fatalf("got %d leaves, want one per coldkey", len(shares))
	}
	if shares[0].Coldkey != shared || shares[0].ClientID != ([16]byte{1}) || shares[0].ShareBPS != 6667 {
		t.Fatalf("unexpected shared-coldkey allocation: %#v", shares)
	}
}
