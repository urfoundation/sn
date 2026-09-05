// Runtime 454 regressions deterministically reproduce each changed decision
// boundary before the live campaign relies on the deployed Wasm behavior.
package main

import (
	"math"
	"strings"
	"testing"
)

// The v453 inherited-filter repair made the inner call face the contracts
// filter, so v454 must restore transfer_stake without widening any sibling call.
func TestRuntime454ContractFilterRestoresOnlyTransferStake(t *testing.T) {
	if !runtime454ContractProxyDispatchAllowed(runtime454ContractTransferStake, true) {
		t.Fatal("delegated transfer_stake was rejected by the inherited contract filter")
	}
	for _, call := range []runtime454ContractCallClass{
		runtime454ContractCallUnknown,
		runtime454ContractMoveStake,
		runtime454ContractAddStake,
		runtime454ContractUtilityBatch,
	} {
		if runtime454ContractProxyDispatchAllowed(call, true) {
			t.Errorf("contract proxy admitted sibling call class %d", call)
		}
	}
	if runtime454ContractProxyDispatchAllowed(runtime454ContractTransferStake, false) {
		t.Fatal("contract filter bypassed the user's proxy delegation")
	}
}

// Subnet-only and stale relationships must not consume the post-selection row
// budget, while root stake and a negative watermark remain independently live.
func TestRuntime454RootClaimSelectsOnlyRootRelevantRelationships(t *testing.T) {
	relationships := []runtime454RootRelationship{
		{HasRootStake: true},
		{RawAlphaRows: 200},
		{BasketWatermark: -1},
		{},
	}
	selectedIndexes, err := runtime454SelectRootRelationships(relationships, 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(selectedIndexes) != 2 || selectedIndexes[0] != 0 || selectedIndexes[1] != 2 {
		t.Fatalf("selected relationships=%v, want root stake and outstanding watermark", selectedIndexes)
	}
}

// Candidate classification and raw Alpha/AlphaV2 rows have separate exact
// admission edges; zero or duplicate raw rows still represent storage work.
func TestRuntime454RootClaimBoundsCandidateScanAndRawRows(t *testing.T) {
	relationships := make([]runtime454RootRelationship, 256)
	if _, err := runtime454SelectRootRelationships(relationships, 256); err != nil {
		t.Fatalf("exact candidate scan boundary rejected: %v", err)
	}
	if _, err := runtime454SelectRootRelationships(append(relationships, runtime454RootRelationship{}), 256); err == nil || !strings.Contains(err.Error(), "candidate scan") {
		t.Fatalf("over-limit candidate scan error=%v", err)
	}
	exactRows := []runtime454RootRelationship{{HasRootStake: true, RawAlphaRows: 127, RawAlphaV2Rows: 128}}
	if _, err := runtime454SelectRootRelationships(exactRows, 256); err != nil {
		t.Fatalf("exact raw-row boundary rejected: %v", err)
	}
	overRows := []runtime454RootRelationship{{HasRootStake: true, RawAlphaRows: 128, RawAlphaV2Rows: 128}}
	if _, err := runtime454SelectRootRelationships(overRows, 256); err == nil || !strings.Contains(err.Error(), "raw basket rows") {
		t.Fatalf("over-limit raw-row error=%v", err)
	}
}

// A positive marked claim must sell one unit rather than burn owed shares for
// zero, and its nonfinal payout cannot capture the sale surplus.
func TestRuntime454RootClaimMinimumUnitPreservesEntitlement(t *testing.T) {
	if take := runtime454MinimumAlphaTake(0, 91, false, false); take != 1 {
		t.Fatalf("minimum alpha take=%d, want 1", take)
	}
	if payout := runtime454ClaimPayout(1_000, 91, false); payout != 91 {
		t.Fatalf("nonfinal minimum-unit payout=%d, want marked entitlement 91", payout)
	}
	for _, input := range []struct {
		entitlement uint64
		root        bool
		terminal    bool
	}{
		{entitlement: 0},
		{entitlement: 91, root: true},
		{entitlement: 91, terminal: true},
	} {
		if take := runtime454MinimumAlphaTake(0, input.entitlement, input.root, input.terminal); take != 0 {
			t.Errorf("ineligible minimum-unit input %+v took %d alpha", input, take)
		}
	}
	if payout := runtime454ClaimPayout(1_000, 91, true); payout != 1_000 {
		t.Fatalf("final shareholder left realized cash behind: %d", payout)
	}
}

// Removing fractional residue prevents a drained position from reviving and
// reduces the denominator by exactly the extinguished share.
func TestRuntime454ShareWithdrawalCanonicalizesSubRaoDust(t *testing.T) {
	share, denominator, removed, err := runtime454CanonicalizeWithdrawalDust(runtime454ShareWithdrawal{
		UpdatedSharedValue: 3,
		CurrentShare:       1,
		Denominator:        4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !removed || share != 0 || denominator != 3 {
		t.Fatalf("canonical withdrawal share=%d denominator=%d removed=%t", share, denominator, removed)
	}
	share, denominator, removed, err = runtime454CanonicalizeWithdrawalDust(runtime454ShareWithdrawal{
		UpdatedSharedValue: 8,
		CurrentShare:       1,
		Denominator:        4,
	})
	if err != nil || removed || share != 1 || denominator != 4 {
		t.Fatalf("whole-rao share was changed: share=%d denominator=%d removed=%t error=%v", share, denominator, removed, err)
	}
	if _, _, removed, err = runtime454CanonicalizeWithdrawalDust(runtime454ShareWithdrawal{CurrentShare: 1}); err == nil || removed {
		t.Fatalf("invalid valuation was interpreted as removable zero: %v", err)
	}
}

// An absent cursor can mean never-started or corrupt state, so only the fresh
// positive migration marker may release the relationship cleanup.
func TestRuntime454MigrationDependencyRequiresCompletionMarker(t *testing.T) {
	if runtime454DependentCleanupAllowed(false, false) {
		t.Fatal("missing progress cursor was treated as completed storage cleanup")
	}
	if !runtime454DependentCleanupAllowed(false, true) {
		t.Fatal("positive storage cleanup marker did not release dependent cleanup")
	}
	if runtime454DependentCleanupAllowed(true, true) {
		t.Fatal("seed migration overlap was admitted despite the completion marker")
	}
}

// Charging only the full-claim dimension underdeclares candidate selection;
// the combined envelope must also saturate instead of wrapping.
func TestRuntime454RootClaimDeclaredWeightIncludesSelectionScan(t *testing.T) {
	if weight := runtime454DeclaredClaimWeight(10, 7); weight != 17 {
		t.Fatalf("declared root-claim weight=%d, want 17", weight)
	}
	if weight := runtime454DeclaredClaimWeight(math.MaxUint64-2, 7); weight != math.MaxUint64 {
		t.Fatalf("overflowing root-claim weight=%d, want saturation", weight)
	}
}

// The live custody actor must execute every local v454 boundary during both
// control and attack phases rather than relying on pre-launch unit tests alone.
func TestRuntime454AdversaryBoundaryMetricsCoverEveryChangedDomain(t *testing.T) {
	for _, sequence := range []uint64{0, 1, math.MaxUint16, math.MaxUint64} {
		passed, err := runtime454AdversaryBoundaryMetrics(sequence)
		if err != nil || passed != 7 {
			t.Errorf("sequence=%d passed=%d, want 7: %v", sequence, passed, err)
		}
	}
}
