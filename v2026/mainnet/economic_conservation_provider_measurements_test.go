// Original independent fixture rows remain fixed while separately signed payout
// artifacts vary. These tests cover the leaf conjunction; original witness
// acquisition and public checkpoint admission are exercised by their consumers.
package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
)

// The four independently observed providers include paid/free traffic, failed
// trials and an idle identity excluded by zero exposure. Neither may disappear.
func economicProviderMeasurementFixtureRows() []payoutartifact.ProviderInput {
	return []payoutartifact.ProviderInput{
		{ClientID: [16]byte{1}, NetworkID: [16]byte{11}, Coldkey: [32]byte{21}, UsageBytes: 100, Assignments: 10, Confirmations: 8, Eligible: true, BindingGeneration: 1},
		{ClientID: [16]byte{2}, NetworkID: [16]byte{12}, Coldkey: [32]byte{22}, UsageBytes: 300, Assignments: 10, Confirmations: 8, Eligible: true, BindingGeneration: 2},
		{ClientID: [16]byte{3}, NetworkID: [16]byte{13}, Coldkey: [32]byte{23}, Assignments: 10, ExclusionReason: "reliability_exposure_floor", BindingGeneration: 3},
		{ClientID: [16]byte{4}, NetworkID: [16]byte{14}, Coldkey: [32]byte{24}, ExclusionReason: "reliability_exposure_floor", BindingGeneration: 4},
	}
}

// The actual shared builder/signature verifier constructs a mathematically valid
// operator artifact even for the independently contradicted variants below.
func economicProviderMeasurementTestArtifact(t *testing.T, rows []payoutartifact.ProviderInput) *payoutartifact.Artifact {
	t.Helper()
	artifact, err := payoutartifact.BuildWithContext(t.Context(), payoutartifact.BuildInput{DeploymentID: "synthetic-provider-measurement", GenesisHash: "0x" + strings.Repeat("1", 64), PolicyHash: "0x" + strings.Repeat("2", 64), ChainID: 964, Netuid: 77, Coordinator: common.BytesToAddress([]byte{31}), SettlementVault: common.BytesToAddress([]byte{32}), Epoch: 9, NoID: 1, Start: payoutartifact.Boundary{Number: 100, Hash: "0x" + strings.Repeat("3", 64)}, End: payoutartifact.Boundary{Number: 200, Hash: "0x" + strings.Repeat("4", 64)}, OperatorSnapshotHash: "sha256:" + strings.Repeat("5", 64), FleetSnapshotHash: "sha256:" + strings.Repeat("6", 64), Providers: rows, ReliabilityAMin: 10, CreatedAt: time.Unix(1900000000, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(strings.Repeat("17", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(artifact, key); err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.VerifyWithContext(t.Context(), artifact); err != nil {
		t.Fatal(err)
	}
	return artifact
}

// This helper projects the original fixture inputs, never the candidate's
// possibly changed ProviderInput or leaf values.
func economicProviderMeasurementTestOriginal(artifactHash string, rows []payoutartifact.ProviderInput) (*economicProviderWorkValues, *economicProviderTrialValues) {
	work := &economicProviderWorkValues{artifactHash: artifactHash, authorityHash: "sha256:" + strings.Repeat("7", 64), windowHash: "sha256:" + strings.Repeat("8", 64), complete: true}
	trials := &economicProviderTrialValues{artifactHash: artifactHash, registryHash: "sha256:" + strings.Repeat("9", 64), windowHash: "sha256:" + strings.Repeat("a", 64), minimumAssignments: 10, complete: true}
	for _, row := range rows {
		work.providers = append(work.providers, economicProviderWorkValue{clientId: row.ClientID, networkId: row.NetworkID, usageBytes: row.UsageBytes})
		trials.providers = append(trials.providers, economicProviderTrialValue{clientId: row.ClientID, networkId: row.NetworkID, coldkey: row.Coldkey, assignments: row.Assignments, confirmations: row.Confirmations, eligible: row.Eligible, headExcluded: row.HeadExcluded, exclusionReason: row.ExclusionReason, bindingGeneration: row.BindingGeneration})
	}
	return work, trials
}

// Complete independent inputs retain both non-earning providers and the exact
// shared allocator's recipient vector, with paid/free bytes weighted equally.
func TestEconomicProviderMeasurementsCompleteRowsKeepFailedAndIdleProviders(t *testing.T) {
	rows := economicProviderMeasurementFixtureRows()
	artifact := economicProviderMeasurementTestArtifact(t, rows)
	work, trials := economicProviderMeasurementTestOriginal(artifact.ContentHash, rows)
	value, err := reconcileEconomicProviderMeasurements(t.Context(), artifact, work, trials)
	if err != nil || value == nil || value.Providers != 4 || value.CompletedBytes != 400 || value.Assignments != 30 || value.Confirmations != 16 || value.ProviderHash != artifact.ProviderSnapshotHash || len(artifact.Leaves) != 2 || artifact.Leaves[0].ShareBPS != 2500 || artifact.Leaves[1].ShareBPS != 7500 {
		t.Fatalf("complete original measurement conjunction differs: %+v %v", value, err)
	}
}

// Re-signing a different allocation with the same complete-byte total cannot
// replace independently observed per-provider work.
func TestEconomicProviderMeasurementsEqualTotalCannotMoveCompletedBytes(t *testing.T) {
	rows := economicProviderMeasurementFixtureRows()
	changed := append([]payoutartifact.ProviderInput(nil), rows...)
	changed[0].UsageBytes, changed[1].UsageBytes = 200, 200
	artifact := economicProviderMeasurementTestArtifact(t, changed)
	work, trials := economicProviderMeasurementTestOriginal(artifact.ContentHash, rows)
	value, err := reconcileEconomicProviderMeasurements(t.Context(), artifact, work, trials)
	if err == nil || value != nil || !strings.Contains(err.Error(), "completed bytes differ") {
		t.Fatalf("equal total authorized a different original provider allocation: %+v %v", value, err)
	}
}

// An extra zero row leaves amounts unchanged but still changes the independently
// expected identity census; omission of an idle row has the same consequence.
func TestEconomicProviderMeasurementsRejectMissingAndExtraZeroProviders(t *testing.T) {
	rows := economicProviderMeasurementFixtureRows()
	variants := [][]payoutartifact.ProviderInput{
		append([]payoutartifact.ProviderInput(nil), rows[:3]...),
		append(append([]payoutartifact.ProviderInput(nil), rows...), payoutartifact.ProviderInput{ClientID: [16]byte{5}, NetworkID: [16]byte{15}, Coldkey: [32]byte{25}, Eligible: true, BindingGeneration: 5}),
	}
	for index, changed := range variants {
		artifact := economicProviderMeasurementTestArtifact(t, changed)
		work, trials := economicProviderMeasurementTestOriginal(artifact.ContentHash, rows)
		value, err := reconcileEconomicProviderMeasurements(t.Context(), artifact, work, trials)
		if err == nil || value != nil || !strings.Contains(err.Error(), "expected provider") {
			t.Fatalf("provider census variant %d accepted: %+v %v", index, value, err)
		}
	}
}

// A zero confirmation count is a verified failed trial, not permission to turn
// the provider into a successful or unmeasured identity.
func TestEconomicProviderMeasurementsFailedTrialsCannotBecomeSuccess(t *testing.T) {
	rows := economicProviderMeasurementFixtureRows()
	changed := append([]payoutartifact.ProviderInput(nil), rows...)
	changed[2].Confirmations = 1
	artifact := economicProviderMeasurementTestArtifact(t, changed)
	work, trials := economicProviderMeasurementTestOriginal(artifact.ContentHash, rows)
	value, err := reconcileEconomicProviderMeasurements(t.Context(), artifact, work, trials)
	if err == nil || value != nil || !strings.Contains(err.Error(), "trial or eligibility differs") {
		t.Fatalf("original failed trial was replaced: %+v %v", value, err)
	}
}

// Missing independent components remain unknown, while a complete contradiction
// in the other component remains visible instead of being masked by absence.
func TestEconomicProviderMeasurementsMissingWindowCannotHideKnownContradiction(t *testing.T) {
	rows := economicProviderMeasurementFixtureRows()
	artifact := economicProviderMeasurementTestArtifact(t, rows)
	work, trials := economicProviderMeasurementTestOriginal(artifact.ContentHash, rows)
	for index, pair := range []struct {
		work   *economicProviderWorkValues
		trials *economicProviderTrialValues
	}{{work: work}, {trials: trials}, {work: &economicProviderWorkValues{}, trials: trials}} {
		value, err := reconcileEconomicProviderMeasurements(t.Context(), artifact, pair.work, pair.trials)
		if err != nil || value != nil {
			t.Fatalf("missing original window %d manufactured complete evidence: %+v %v", index, value, err)
		}
	}
	work.providers[0].usageBytes++
	if value, err := reconcileEconomicProviderMeasurements(t.Context(), artifact, work, nil); err == nil || value != nil {
		t.Fatalf("missing trials hid independently contradicted work: %+v %v", value, err)
	}
}

// A signed complete empty source has different semantics from no source. This
// measurement predicate does not invent a finalized zero-amount entitlement.
func TestEconomicProviderMeasurementsKnownEmptyWindowIsNotMissingEvidence(t *testing.T) {
	artifact := economicProviderMeasurementTestArtifact(t, nil)
	work, trials := economicProviderMeasurementTestOriginal(artifact.ContentHash, nil)
	value, err := reconcileEconomicProviderMeasurements(t.Context(), artifact, work, trials)
	if err != nil || value == nil || value.Providers != 0 || value.CompletedBytes != 0 || value.Assignments != 0 || value.Confirmations != 0 {
		t.Fatalf("complete known-empty measurement became missing or nonzero: %+v %v", value, err)
	}
	if value, err := reconcileEconomicProviderMeasurements(t.Context(), artifact, work, nil); err != nil || value != nil {
		t.Fatalf("missing empty validator census was inferred complete: %+v %v", value, err)
	}
}

// An unchanged payout root does not authorize changes to otherwise zero-value
// eligibility, registered identity, binding generation or exclusion history.
func TestEconomicProviderMeasurementsBindOriginalEligibilityAndIdentity(t *testing.T) {
	rows := economicProviderMeasurementFixtureRows()
	changes := []func(*payoutartifact.ProviderInput){
		func(row *payoutartifact.ProviderInput) { row.NetworkID = [16]byte{99} },
		func(row *payoutartifact.ProviderInput) { row.Coldkey = [32]byte{99} },
		func(row *payoutartifact.ProviderInput) { row.Eligible = !row.Eligible },
		func(row *payoutartifact.ProviderInput) { row.HeadExcluded = true },
		func(row *payoutartifact.ProviderInput) { row.ExclusionReason = "synthetic-foreign-exclusion" },
		func(row *payoutartifact.ProviderInput) { row.BindingGeneration++ },
	}
	for index, change := range changes {
		changed := append([]payoutartifact.ProviderInput(nil), rows...)
		change(&changed[3])
		artifact := economicProviderMeasurementTestArtifact(t, changed)
		work, trials := economicProviderMeasurementTestOriginal(artifact.ContentHash, rows)
		value, err := reconcileEconomicProviderMeasurements(t.Context(), artifact, work, trials)
		cause := "trial or eligibility differs"
		if index == 0 {
			cause = "completed bytes differ"
		}
		if err == nil || value != nil || !strings.Contains(err.Error(), cause) {
			t.Fatalf("zero-value identity variant %d accepted: %+v %v", index, value, err)
		}
	}
}

// The measurement owner cancels before allocation and cannot publish a retained
// result from a previous successful invocation.
func TestEconomicProviderMeasurementsCanceledOwnerPublishesNoProjection(t *testing.T) {
	rows := economicProviderMeasurementFixtureRows()
	artifact := economicProviderMeasurementTestArtifact(t, rows)
	work, trials := economicProviderMeasurementTestOriginal(artifact.ContentHash, rows)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	value, err := reconcileEconomicProviderMeasurements(ctx, artifact, work, trials)
	if value != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled measurement owner published evidence: %+v %v", value, err)
	}
}
