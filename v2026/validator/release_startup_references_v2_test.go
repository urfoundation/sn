//go:build linux || darwin

// References exercise genuine compact artifact/envelope bytes. They prove
// custody and input linkage only, never independent head/deposit observations.
package validator

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Actual absence consumes no content bytes. The complete-history exact limit
// cannot become either an extra byte allowance or an implicit empty-file read.
func TestReleaseStartupV2ExhaustedReferenceAllowanceProvesOnlyAbsence(t *testing.T) {
	t.Parallel()
	root := newAttemptSettlementRuntimeV2TestStateDir(t)
	path := filepath.Join(root, "steering-intents.json")
	owned := &releaseEvidenceV2StartupReferences{}
	encoded, err := owned.read(t.Context(), path, 1024, true)
	if err != nil || encoded != nil || owned.remaining != 0 {
		t.Fatalf("actual zero-byte absence was refused: %v", err)
	}
	if err := owned.close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	occupied := &releaseEvidenceV2StartupReferences{}
	if _, err := occupied.read(context.Background(), path, 1024, true); err == nil {
		t.Fatal("zero-byte occupied file became authenticated absence")
	}
	if err := occupied.close(); err != nil {
		t.Fatal(err)
	}
}

// The complete empty canonical intent file is distinct from missing history;
// both permit actual pristine Stats startup without manufacturing a decision.
func TestReleaseStartupV2ReadsCanonicalEmptyIntentReferenceFile(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, false)
	encoded, err := marshalAttemptSettlementV2JSON(t.Context(), &steeringIntentFile{Schema: steeringIntentSchema, History: []SteeringIntent{}}, fixture.cfg.EvidenceV2.Bounds.MaxHistoryBytes, true, true)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.cfg.StateDir, "steering-intents.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO()); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, encoded) {
		t.Fatalf("startup changed the canonical empty intent history: %v", err)
	}
}

// Exact existing signed content is read through retained descriptors; neither
// a candidate pathname nor its declared length can enlarge the byte allowance.
func TestReleaseStartupV2ContentReferenceUsesExactBoundedPhysicalBytes(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 1)
	root := newAttemptSettlementRuntimeV2TestStateDir(t)
	path, size, err := persistReleaseMeasurementArtifact(root, fixture.measurement, ReleaseMeasurementContentHash(fixture.measurement))
	if err != nil {
		t.Fatal(err)
	}
	history := &releaseEvidenceV2StartupHistory{cfg: ReleaseConfig{StateDir: root}}
	owned := &releaseEvidenceV2StartupReferences{remaining: size}
	actual, err := history.readContentReference(t.Context(), owned, path, ReleaseMeasurementContentHash(fixture.measurement), size, size, false)
	if err != nil || !bytes.Equal(actual, fixture.measurement) || owned.remaining != 0 {
		t.Fatalf("exact physical reference read differs: %v", err)
	}
	if err := owned.close(); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"path", "aggregate", "file"} {
		limit, aggregate, supplied := size, size, path
		switch fault {
		case "path":
			supplied = "measurements/../foreign.json"
		case "aggregate":
			aggregate--
		case "file":
			limit--
		}
		refused := &releaseEvidenceV2StartupReferences{remaining: aggregate}
		if _, err := history.readContentReference(t.Context(), refused, supplied, ReleaseMeasurementContentHash(fixture.measurement), size, limit, false); err == nil || len(refused.owners) != 0 {
			t.Fatalf("%s reference reached filesystem without finite path/size admission: %v", fault, err)
		}
		if err := refused.close(); err != nil {
			t.Fatal(err)
		}
	}
}

// The configured independent fixture decision is established before candidate
// mutation. The genuine V2 sealer and sr25519 decoder precede this private
// link-only comparison; no fabricated replay callback is installed.
func TestReleaseStartupV2IntentReferenceMatchesActualSignedInputBytes(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 1)
	options := fixture.options()
	encoded, envelope := fixture.seal(t)
	decoded, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), encoded, options.MaxControlBytes)
	if err != nil {
		t.Fatal(err)
	}
	expected := options.Expected
	history := &releaseEvidenceV2StartupHistory{cfg: ReleaseConfig{DeploymentID: expected.DeploymentID, ChainID: expected.ChainID, GenesisHash: expected.GenesisHash, Coordinator: expected.Coordinator, SettlementVault: expected.SettlementVault, ValidatorID: expected.ValidatorID, Netuid: expected.Netuid, PolicyHash: expected.PolicyHash, Policy: options.Policy, EvidenceV2: ReleaseEvidenceV2Config{Bounds: ReleaseEvidenceV2Bounds{MaxInputJournalBytes: options.MaxArtifactBytes, MaxHeadEntries: options.MaxHeadEntries}}}, initial: map[uint64]ReleaseEvidenceV2ActivationContext{}, inputByEpoch: map[uint64]map[uint64]*releaseMeasurementInputJournal{expected.SubnetEpoch: {}}}
	ownedArtifact := cloneReleaseMeasurementArtifact(t, fixture.artifact)
	for _, input := range ownedArtifact.Inputs {
		operator := options.Operators[input.NoID]
		history.initial[input.NoID] = ReleaseEvidenceV2ActivationContext{InitialCut: operator.Expected}
		history.participants = append(history.participants, AttemptSettlementRuntimeV2Participant{NoID: input.NoID})
		history.inputByEpoch[expected.SubnetEpoch][input.NoID] = &releaseMeasurementInputJournal{Schema: releaseMeasurementInputV2Schema, SubnetEpoch: expected.SubnetEpoch, MeasurementInput: input}
	}
	intent := &SteeringIntent{ValidatorID: expected.ValidatorID, Netuid: expected.Netuid, SubnetEpoch: expected.SubnetEpoch, NativeSnapshotBlock: expected.NativeSnapshotBlock, NativeSnapshotHash: expected.NativeSnapshotHash, EVMSnapshotBlock: expected.EVMSnapshotBlock, EVMSnapshotHash: expected.EVMSnapshotHash, SettlementEpoch: expected.SettlementEpoch, PolicyHash: expected.PolicyHash, SelfUID: expected.SelfUID, MeasurementArtifactHash: envelope.MeasurementArtifactHash, MeasurementArtifactSize: envelope.MeasurementArtifactSize, Prepared: &crv4.PreparedSubmission{HotkeyHex: envelope.ValidatorHotkey, ExtrinsicHash: envelope.PreparedExtrinsicHash, Netuid: expected.Netuid, SubnetEpoch: expected.SubnetEpoch, PreparedAtBlock: expected.NativeSnapshotBlock, PreparedAtBlockHash: expected.NativeSnapshotHash}}
	if err := history.matchIntentReference(t.Context(), intent, fixture.artifact, decoded); err != nil {
		t.Fatalf("actual signed input reference differs: %v", err)
	}
	mutated := cloneReleaseMeasurementArtifact(t, fixture.artifact)
	mutated.Inputs[0].Stats.Providers[0].Assignments++
	if err := history.matchIntentReference(t.Context(), intent, mutated, decoded); err == nil || !strings.Contains(err.Error(), "immutable journal") {
		t.Fatalf("reference accepted altered genuine input bytes: %v", err)
	}
	delete(history.inputByEpoch[expected.SubnetEpoch], fixture.artifact.Inputs[0].NoID)
	if err := history.matchIntentReference(t.Context(), intent, fixture.artifact, decoded); err == nil || !strings.Contains(err.Error(), "missing immutable native input") {
		t.Fatalf("reference accepted removed input before Begin: %v", err)
	}
}
