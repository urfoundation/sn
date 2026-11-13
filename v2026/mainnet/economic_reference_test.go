// Integer controls distinguish native target accounting from observed rewards.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Small native intervals retain their cumulative tenth instead of losing it
// through independent per-interval floors.
func TestEconomicReferenceCarriesRoundingAcrossIntervals(t *testing.T) {
	input := economicReferenceInput{Schema: economicReferenceInputSchema, NativeMinerAllocations: []string{"9", "1", "0", "9", "1"}}
	reference, err := calculateEconomicReference(input, "synthetic-input-hash")
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []string{"0", "1", "0", "0", "1"} {
		if reference.Intervals[index].ProviderAlpha != want {
			t.Fatalf("interval %d provider reference=%s, want %s", index, reference.Intervals[index].ProviderAlpha, want)
		}
	}
	last := reference.Intervals[len(reference.Intervals)-1]
	if last.NativeMinerTotalAlpha != "20" || last.ProviderTotalAlpha != "2" || last.OwnerRecycleTotalAlpha != "18" {
		t.Fatalf("cumulative reference differs: %+v", last)
	}
}

// Equal native allocation totals give equal cumulative targets regardless of
// interval partitioning; this does not authenticate a caller's interval list.
func TestEconomicReferencePartitionConservesTarget(t *testing.T) {
	var final economicReferenceInterval
	for _, amounts := range [][]string{{"10"}, {"3", "3", "4"}, {"1", "1", "1", "1", "1", "1", "1", "1", "1", "1"}} {
		reference, err := calculateEconomicReference(economicReferenceInput{Schema: economicReferenceInputSchema, NativeMinerAllocations: amounts}, "synthetic-input-hash")
		if err != nil {
			t.Fatal(err)
		}
		last := reference.Intervals[len(reference.Intervals)-1]
		if final.ProviderTotalAlpha != "" && (last.ProviderTotalAlpha != final.ProviderTotalAlpha || last.OwnerRecycleTotalAlpha != final.OwnerRecycleTotalAlpha) {
			t.Fatalf("partition changed cumulative target: %+v vs %+v", last, final)
		}
		final = last
	}
}

// Native per-interval u64 amounts may sum beyond u64 and beyond float64's exact
// integer domain without truncating or saturating the cumulative reference.
func TestEconomicReferenceDoesNotOverflowCumulativeAmounts(t *testing.T) {
	reference, err := calculateEconomicReference(economicReferenceInput{Schema: economicReferenceInputSchema, NativeMinerAllocations: []string{"18446744073709551615", "18446744073709551615"}}, "synthetic-input-hash")
	if err != nil {
		t.Fatal(err)
	}
	last := reference.Intervals[1]
	if last.NativeMinerTotalAlpha != "36893488147419103230" || last.ProviderTotalAlpha != "3689348814741910323" || last.OwnerRecycleTotalAlpha != "33204139332677192907" || last.ProviderAlpha != "1844674407370955162" {
		t.Fatalf("large integer reference differs: %+v", last)
	}
}

// Every interval and cumulative row conserve alpha, and the integer tenth is
// bounded by a residual of zero through nine atomic units.
func TestEconomicReferenceConservesAlphaWithoutReserveCredit(t *testing.T) {
	amounts := []string{"0", "1", "19", "9007199254740993", "18446744073709551615", "7"}
	reference, err := calculateEconomicReference(economicReferenceInput{Schema: economicReferenceInputSchema, NativeMinerAllocations: amounts}, "synthetic-input-hash")
	if err != nil {
		t.Fatal(err)
	}
	parse := func(raw string) *big.Int {
		value, ok := new(big.Int).SetString(raw, 10)
		if !ok || value.Sign() < 0 {
			t.Fatalf("invalid reference integer %q", raw)
		}
		return value
	}
	for _, interval := range reference.Intervals {
		if new(big.Int).Add(parse(interval.ProviderAlpha), parse(interval.OwnerRecycleAlpha)).Cmp(parse(interval.NativeMinerAlpha)) != 0 || new(big.Int).Add(parse(interval.ProviderTotalAlpha), parse(interval.OwnerRecycleTotalAlpha)).Cmp(parse(interval.NativeMinerTotalAlpha)) != 0 {
			t.Fatalf("alpha not conserved: %+v", interval)
		}
		residual := new(big.Int).Sub(parse(interval.NativeMinerTotalAlpha), new(big.Int).Mul(parse(interval.ProviderTotalAlpha), big.NewInt(10)))
		if residual.Sign() < 0 || residual.Cmp(big.NewInt(9)) > 0 {
			t.Fatalf("reference tenth outside integer bound: %+v", interval)
		}
	}
	if reference.RemainderPolicy != "owner-recycle" || reference.ReserveCreditAlpha != "0" || reference.DeferredProviderLiabilityAlpha != "0" || reference.ActualNativeOutcomeVerified || reference.QuantizationToleranceKnown || reference.ActivationReady {
		t.Fatalf("reference acquired unintended financial authority: %+v", reference)
	}
}

// No decimal rounding, exponent parsing, signed value, leading zero or amount
// wider than the runtime's u64 interval type enters reference accounting.
func TestEconomicReferenceRejectsAmbiguousAmounts(t *testing.T) {
	for _, amount := range []string{"", "-1", "+1", "01", "1.0", "1e3", " 1", "18446744073709551616"} {
		if _, err := calculateEconomicReference(economicReferenceInput{Schema: economicReferenceInputSchema, NativeMinerAllocations: []string{amount}}, "synthetic-input-hash"); err == nil {
			t.Fatalf("ambiguous amount %q was accepted", amount)
		}
	}
}

// Input completeness and total work are checked before materializing outputs.
func TestEconomicReferenceRejectsMissingOrUnboundedIntervals(t *testing.T) {
	for _, input := range []economicReferenceInput{
		{NativeMinerAllocations: []string{"10"}},
		{Schema: economicReferenceInputSchema},
		{Schema: economicReferenceInputSchema, NativeMinerAllocations: make([]string, maximumReferenceIntervals+1)},
	} {
		if _, err := calculateEconomicReference(input, "synthetic-input-hash"); err == nil {
			t.Fatal("incomplete or unbounded reference input was accepted")
		}
	}
}

// Fixture files contain synthetic protocol inputs and no signer material.
func writeEconomicTestInput(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "economic-input.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The offline command records its exact input hash and remains non-activating.
func TestEconomicReferenceCommandRemainsReferenceOnly(t *testing.T) {
	path := writeEconomicTestInput(t, economicReferenceInput{Schema: economicReferenceInputSchema, NativeMinerAllocations: []string{"9", "1"}})
	var stdout, stderr bytes.Buffer
	if result := runMain(context.Background(), []string{"economic-reference", "--input", path}, &stdout, &stderr); result != 0 {
		t.Fatalf("reference command exit=%d error=%s", result, stderr.String())
	}
	var reference economicReference
	if err := json.Unmarshal(stdout.Bytes(), &reference); err != nil || len(reference.Intervals) != 2 || reference.Intervals[1].ProviderTotalAlpha != "1" || !strings.HasPrefix(reference.InputHash, "sha256:") || reference.ActivationReady {
		t.Fatalf("reference command result: %+v %v", reference, err)
	}
}

// Burn returns a failed precondition exit while preserving the sealed evidence;
// Recycle passes only the named mode check, never overall activation.
func TestRecycleModeCommandPreservesFailedAndPassingEvidence(t *testing.T) {
	for _, mode := range []string{"0x00", "0x01"} {
		client, policy, _ := newRecycleTestClient(t, &mode, nil)
		path := writeEconomicTestInput(t, policy)
		var stdout, stderr bytes.Buffer
		result := runMain(context.Background(), []string{"check-recycle-mode", "--rpc", client.url, "--policy", path}, &stdout, &stderr)
		want := 3
		if mode == "0x01" {
			want = 0
		}
		var snapshot recycleModeEnvelope
		if result != want || json.Unmarshal(stdout.Bytes(), &snapshot) != nil || snapshot.Observation.ActivationReady || snapshot.Observation.ModeGatePassed != (mode == "0x01") || !strings.HasPrefix(snapshot.Observation.PolicyHash, "sha256:") {
			t.Fatalf("mode=%s exit=%d stdout=%s stderr=%s", mode, result, stdout.String(), stderr.String())
		}
	}
}

// Duplicate and unknown fields cannot change the meaning of a reviewed file.
func TestEconomicCommandsRejectAmbiguousJson(t *testing.T) {
	for _, raw := range []string{
		`{"schema":"urnetwork-native-miner-reference-input-v1","schema":"other","native_miner_allocations_alpha":["10"]}`,
		`{"schema":"urnetwork-native-miner-reference-input-v1","native_miner_allocations_alpha":["10"],"activate":true}`,
		`{"schema":"urnetwork-native-miner-reference-input-v1","native_miner_allocations_alpha":["10"]} {}`,
	} {
		path := filepath.Join(t.TempDir(), "ambiguous.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if result := runMain(context.Background(), []string{"economic-reference", "--input", path}, &stdout, &stderr); result != 2 || stdout.Len() != 0 {
			t.Fatalf("ambiguous input exit=%d stdout=%s stderr=%s", result, stdout.String(), stderr.String())
		}
	}
}
