// These retained-record arithmetic tests do not construct an owned producer
// result. The actual original-Wasm consumer is qualified separately below.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
)

// SCALE bytes are built independently of the production availability parser.
func nativeTreasuryTestObservation(query historicalPrincipalQuery, stock, total, locked, available uint64) historicalPrincipalObservation {
	stakeRaw := append([]byte{1}, query.Hotkey[:]...)
	stakeRaw = append(stakeRaw, query.Coldkey[:]...)
	for _, value := range []uint64{uint64(query.Netuid), stock, 0, 0, 0, 0} {
		stakeRaw = append(stakeRaw, rootCompact(value)...)
	}
	stakeRaw = append(stakeRaw, 1)
	availabilityRaw := append([]byte{4}, query.Coldkey[:]...)
	availabilityRaw = append(availabilityRaw, 4, byte(query.Netuid), byte(query.Netuid>>8))
	for _, value := range []uint64{total, locked, available} {
		availabilityRaw = append(availabilityRaw, rootCompact(value)...)
	}
	stockText, totalText, lockedText, availableText, registered := fmt.Sprint(stock), fmt.Sprint(total), fmt.Sprint(locked), fmt.Sprint(available), true
	return historicalPrincipalObservation{Query: query, ResultHex: nativeExecutionTestHex(stakeRaw), OpeningStakeAlpha: &stockText, Registered: &registered, Availability: &historicalStakeAvailabilityObservation{ResultHex: nativeExecutionTestHex(availabilityRaw), TotalAlpha: &totalText, LockedAlpha: &lockedText, AvailableAlpha: &availableText}}
}

func nativeTreasuryTestArchivedState(t *testing.T) (economicConservationState, economicConservationPolicy) {
	t.Helper()
	authority, emission := nativeTreasuryTestAuthority(t)
	emission.Execution = &nativeExecutionPolicy{Treasury: authority, Principal: nativeTreasuryTestPrincipal(authority, emission.From)}
	policy := economicConservationPolicy{Native: monitorEconomicNativePolicy{Observation: emission}}
	through := economicEmissionBoundary{Number: emission.From.Number + 1, Hash: "0x" + strings.Repeat("47", 32)}
	window := nativeExecutionEmpty(emission.From)
	window.Treasury = newNativeTreasuryAmounts(authority)
	window.Treasury.Gross, window.Treasury.Liquid, window.Treasury.Collateral = "98", "95", "3"
	window.Blocks, window.Through, window.MinerAllocation, window.CollateralCapture, window.AllocationDifference = 1, through, "100", "3", "2"
	if err := window.references(); err != nil {
		t.Fatal(err)
	}
	archive := &economicConservationPrincipalArchive{Blocks: 1, Through: through}
	for index, query := range emission.Execution.Principal.Queries {
		amount, liquid, captured := uint64(9), "6", "3"
		if index == 1 {
			amount, liquid, captured = 89, "89", "0"
		}
		before, after, residual := "0", fmt.Sprint(amount), "0"
		archive.After = append(archive.After, nativeTreasuryTestObservation(query, amount, 98, 0, 95))
		archive.Pools = append(archive.Pools, nativePrincipalPoolEffects{Query: query, Before: &before, After: &after, StockChange: &after, Deposits: "0", Withdrawals: "0", Refunds: "0", VaultCaptures: "0", NativeEarnings: liquid, CapturedEarnings: &captured, Residual: &residual, Complete: true})
	}
	return economicConservationState{Native: monitorEconomicNativeState{Cursor: through, ExecutionAccounting: &window}, Archive: &economicConservationArchive{PrincipalEffects: archive}}, policy
}

func TestEconomicTreasuryAvailabilityCountsSharedColdkeyOnce(t *testing.T) {
	state, policy := nativeTreasuryTestArchivedState(t)
	result, err := state.treasurySummary(policy)
	if err != nil || result == nil || !result.CauseCensusComplete || !result.StockCovered || !result.AvailabilityKnown || *result.PositionStockAlpha != "98" || *result.Availability.AvailableAlpha != "95" || *result.SpendableIncomeLowerAlpha != "95" || *result.SpendableIncomeUpperAlpha != "95" || result.IncomeUnlockedOrSpent == nil || *result.IncomeUnlockedOrSpent {
		t.Fatal("shared coldkey was counted twice or captured earnings became spendable", result, err)
	}
	*result.Availability.AvailableAlpha = "999"
	*result.Positions[0].After = "999"
	if *state.Archive.PrincipalEffects.After[0].Availability.AvailableAlpha != "95" || *state.Archive.PrincipalEffects.Pools[0].After != "9" {
		t.Fatal("summary mutated retained original custody")
	}
}

func TestEconomicTreasuryUnknownAvailabilityAndUncoveredStockStayUnknown(t *testing.T) {
	for _, mode := range []string{"absent", "uncovered", "disagreement"} {
		state, policy := nativeTreasuryTestArchivedState(t)
		for index := range state.Archive.PrincipalEffects.After {
			value := &state.Archive.PrincipalEffects.After[index]
			switch mode {
			case "absent":
				raw := append([]byte{4}, value.Query.Coldkey[:]...)
				raw = append(raw, 0)
				value.Availability = &historicalStakeAvailabilityObservation{ResultHex: nativeExecutionTestHex(raw)}
			case "uncovered":
				stock := uint64(9)
				if index == 1 {
					stock = 89
				}
				*value = nativeTreasuryTestObservation(value.Query, stock, 99, 0, 96)
			case "disagreement":
				if index == 1 {
					*value = nativeTreasuryTestObservation(value.Query, 89, 98, 0, 94)
				}
			}
		}
		result, err := state.treasurySummary(policy)
		if mode == "disagreement" {
			if err == nil {
				t.Fatal("contradictory original coldkey results accepted")
			}
			continue
		}
		if err != nil || result.SpendableIncomeLowerAlpha != nil || result.IncomeUnlockedOrSpent != nil {
			t.Fatal("unknown original availability became spendable income", mode, result, err)
		}
	}
}

func TestEconomicTreasuryOriginalUnlockMakesAvailabilityKnownWithoutNewIncome(t *testing.T) {
	state, policy := nativeTreasuryTestArchivedState(t)
	for index := range state.Archive.PrincipalEffects.After {
		query := state.Archive.PrincipalEffects.After[index].Query
		amount := uint64(9)
		if index == 1 {
			amount = 89
		}
		state.Archive.PrincipalEffects.After[index] = nativeTreasuryTestObservation(query, amount, 98, 0, 98)
	}
	result, err := state.treasurySummary(policy)
	if err != nil || result.IncomeUnlockedOrSpent == nil || !*result.IncomeUnlockedOrSpent || result.Income.Gross != "98" || result.Income.Collateral != "3" || *result.Availability.AvailableAlpha != "98" {
		t.Fatal("original availability was replaced by historical capture or new income", result, err)
	}
}

// The durable metrics owner accepts the explicit successor sample, including
// unknown availability; none of its new gauges can turn unknown into success.
func TestEconomicTreasuryMetricsPublishesSuccessorWithoutUnknownCredit(t *testing.T) {
	directory := monitorMetricsTestDir(t)
	path := filepath.Join(directory, "treasury.prom")
	// A real owner must reject a bare context before touching the textfile or
	// lock. The same path succeeds only with its explicit fixture declaration.
	unguarded, err := openMonitorMetrics(path, t.Context())
	if unguarded != nil {
		closeErr := unguarded.close()
		t.Fatal("treasury metrics acquired storage without a declaration", closeErr)
	}
	if err == nil || !strings.Contains(err.Error(), "explicit durable-volume declaration and hash") {
		t.Fatal("treasury metrics did not enforce the original volume admission", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("refused treasury metrics owner changed its undeclared directory", entries, err)
	}
	storage := durablefixture.New(t, t.Context(), directory)
	owner, err := openMonitorMetrics(path, storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.close(); err != nil {
			t.Error(err)
		}
	})
	for _, mode := range []string{"locked", "unknown", "unlocked"} {
		state, policy := nativeTreasuryTestArchivedState(t)
		if mode != "locked" {
			for index := range state.Archive.PrincipalEffects.After {
				observation := &state.Archive.PrincipalEffects.After[index]
				if mode == "unknown" {
					raw := append([]byte{4}, observation.Query.Coldkey[:]...)
					observation.Availability = &historicalStakeAvailabilityObservation{ResultHex: nativeExecutionTestHex(append(raw, 0))}
				} else {
					amount := uint64(9)
					if index == 1 {
						amount = 89
					}
					*observation = nativeTreasuryTestObservation(observation.Query, amount, 98, 0, 98)
				}
			}
		}
		treasury, err := state.treasurySummary(policy)
		if err != nil {
			t.Fatal(err)
		}
		summary := economicConservationSummary{Schema: "urnetwork-economic-conservation-sample-v2", Treasury: treasury, Execution: state.Native.ExecutionAccounting, PolicyHash: monitorReadDigest([]byte("metrics policy")), CheckpointHash: monitorReadDigest([]byte("metrics original checkpoint")), SampleAt: time.Date(2026, 10, 5, 1, 2, 3, 0, time.UTC)}
		raw, err := renderEconomicConservationMetrics(&summary)
		if err != nil || len(raw) > 16*1024 || bytes.Count(raw, []byte("# TYPE ")) != 78 {
			t.Fatal("successor metrics exceeded the existing owner or changed census", len(raw), err)
		}
		known, spendable := 1, 0
		if mode == "unknown" {
			known = 0
		}
		if mode == "unlocked" {
			spendable = 1
		}
		for _, line := range []string{
			fmt.Sprintf("sn_mainnet_conservation_treasury_availability_known %d\n", known),
			fmt.Sprintf("sn_mainnet_conservation_treasury_income_spendability_known %d\n", known),
			fmt.Sprintf("sn_mainnet_conservation_treasury_income_unlocked_or_spent %d\n", spendable),
			"sn_mainnet_conservation_treasury_split_known 0\n", "sn_mainnet_conservation_owner_recycle_known 0\n", "sn_mainnet_conservation_target_known 0\n",
		} {
			if !bytes.Contains(raw, []byte(line)) {
				t.Fatal("treasury metric invented known credit or old owner conformance", mode, line)
			}
		}
		if err := saveEconomicConservationMetrics(owner, &summary); err != nil {
			t.Fatal("public metrics publication refused treasury sample", err)
		}
		retained, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(retained, raw) {
			t.Fatal("durable treasury metrics differ from original sample", err)
		}
		summary.Schema = "urnetwork-economic-conservation-sample-v1"
		if _, err := renderEconomicConservationMetrics(&summary); err == nil {
			t.Fatal("treasury was admitted through legacy sample schema")
		}
		summary.Treasury = nil
		if _, err := renderEconomicConservationMetrics(&summary); err == nil {
			t.Fatal("treasury execution omitted its original custody summary")
		}
		summary.Execution = nil
		legacy, err := renderEconomicConservationMetrics(&summary)
		if err != nil {
			t.Fatal(err)
		}
		readEconomicMetricsTest(t, legacy)
		if bytes.Contains(legacy, []byte("conservation_treasury_")) {
			t.Fatal("legacy metrics bytes gained successor gauges")
		}
	}
}
