package protocol

import (
	"os"
	"path/filepath"
	"testing"
)

func testPolicyPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "deploy", "testnet", "policy-v1.yml")
}

func TestLoadPolicyCanonicalHash(t *testing.T) {
	p, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	h1, err := p.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 || h1 == "" {
		t.Fatal("empty canonical policy/hash")
	}
	p2, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := p2.HashHex()
	if h1 != h2 {
		t.Fatalf("non-deterministic policy hash: %s != %s", h1, h2)
	}
}

func TestPolicyStrictAndFailClosed(t *testing.T) {
	b, err := os.ReadFile(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	bad := append(append([]byte(nil), b...), []byte("\n  unknown_field: true\n")...)
	if _, err := ParsePolicy(bad); err == nil {
		t.Fatal("unknown policy field accepted")
	}
	p, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	p.Deposit.Tiers[0].RateDenominator = 0
	if err := p.Validate(); err == nil {
		t.Fatal("zero rate accepted")
	}
}

func TestPolicyCadenceWindowsFailClosed(t *testing.T) {
	p, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if p.Settlement.CloseGraceBlocks != 5 || p.ProductionCadence.EpochBlocks != 50_400 || p.ProductionCadence.AfterAcceleratedEpochs != 20 {
		t.Fatalf("unexpected release cadence: settlement=%+v production=%+v", p.Settlement, p.ProductionCadence)
	}
	tests := []func(*Policy){
		func(v *Policy) { v.Settlement.CloseGraceBlocks = 0 },
		func(v *Policy) { v.Settlement.CloseGraceBlocks = v.Settlement.RootCommitWindowBlocks + 1 },
		func(v *Policy) { v.ProductionCadence.AfterAcceleratedEpochs = 0 },
		func(v *Policy) { v.ProductionCadence.EpochBlocks = v.Settlement.EpochBlocks },
		func(v *Policy) {
			v.ProductionCadence.RootCommitWindowBlocks = v.ProductionCadence.FinalizeOffsetBlocks + 1
		},
	}
	for index, mutate := range tests {
		copy := *p
		mutate(&copy)
		if err := copy.Validate(); err == nil {
			t.Fatalf("invalid cadence mutation %d accepted", index)
		}
	}
}
