//go:build linux || darwin

// Slot headroom, source lifetime and upload quotas are separate bounded
// resources. Real approvals retain their original money and source identity.
package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Apply the same signed v6 capacity transformation as runtime resolution to
// the actual checked-in launch profile, without opening a wallet or network.
func TestRuntimeEvidenceSourceCapacityApprovedProfileHasTwofoldMargin(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	cfg.OperationalRPCMode = rpcModeOwnedNode
	before, err := json.Marshal(cfg.Config)
	if err != nil {
		t.Fatal(err)
	}
	continuation := &EvidenceRelayContinuation{Schema: evidenceRelayContinuationSourceExpansionSchema}
	for _, source := range cfg.Config.ValidatorEvidenceV2 {
		approved, err := doubledEvidenceRelaySourceBounds(source.Evidence.Bounds)
		if err != nil {
			t.Fatal(err)
		}
		continuation.SourceBounds = append(continuation.SourceBounds, evidenceRelaySourceBounds{ValidatorId: source.ValidatorID, Original: source.Evidence.Bounds, Approved: approved})
	}
	resolved, err := evidenceRelaySourceCapacityConfig(cfg, &SetupPlan{EvidenceRelayContinuation: continuation})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Config.ValidatorEvidenceRelay.MaxSlots != 2048 || resolved.Config.ValidatorEvidenceRelay.SourceHorizonBlocks != 10080 {
		t.Fatal("funded subjects were confused with the independent source lifetime")
	}
	rows := 0
	for _, source := range resolved.Config.ValidatorEvidenceV2 {
		minimum, err := requiredRuntimeEvidenceSourceCapacity(resolved, source.Evidence.Bounds)
		if err != nil {
			t.Fatal(err)
		}
		if minimum.span != 10080 || minimum.objectsPerHour != 39929 || minimum.bytesPerHour != 27904081920 || minimum.retryRequestsPerHour != 12646364 {
			t.Fatalf("expanded source plus full subject census changed: %+v", minimum)
		}
		for _, replica := range resolved.Config.Artifacts.ReservedAttemptUploads {
			if replica.Budget.ObjectsPerHour < 2*minimum.objectsPerHour || replica.Budget.BytesPerHour < 2*minimum.bytesPerHour || replica.Budget.RetryRequestsPerHour < 2*minimum.retryRequestsPerHour {
				t.Fatalf("validator %d replica %d lost its twofold upload margin: budget=%+v minimum=%+v", source.ValidatorID, replica.Admission.ReplicaNoID, replica.Budget, minimum)
			}
			rows++
		}
	}
	var diagnostic bytes.Buffer
	if err := validateRuntimeEvidenceSourceCapacityWithDiagnostics(resolved, &diagnostic); err != nil || diagnostic.Len() != 0 || rows != 4 {
		t.Fatalf("strict four-owner admission requires an advisory: rows=%d diagnostics=%q error=%v", rows, diagnostic.String(), err)
	}
	after, err := json.Marshal(cfg.Config)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("source overlay rewrote the configured predecessor", err)
	}
}

// The actual successor builder must carry the funded continuation byte for
// byte. A source-only capacity repair cannot submit another expansion or add
// money, and the real dynamic admission must accept exactly its funded slots.
func TestEvidenceRelaySourceExpansionCapacityRevisionPreservesFundedApproval(t *testing.T) {
	fixture, executor := newEvidenceRelayExpansionTest(t)
	retainEvidenceRelaySourceExpansionSetupTest(t, fixture, executor)
	continuation := evidenceRelaySourceExpansionRequestTest(t, fixture, executor)
	prior, err := appendEvidenceRelayContinuationPlan(executor.plan, continuation)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRunInputs(fixture.cfg, fixture.stateDir, prior, fixture.roles); err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	launch := runtimeEvidenceLaunchConfigTest(t)
	fixture.cfg.Config.ValidatorEvidenceRelay = launch.Config.ValidatorEvidenceRelay
	fixture.cfg.Config.Artifacts.ReservedAttemptUploads = launch.Config.Artifacts.ReservedAttemptUploads
	fixture.cfg.Config.EvidenceArchiveMetadata = launch.Config.EvidenceArchiveMetadata
	fixture.cfg.ConfigHash, err = releaseConfigHash(fixture.cfg.Config, fixture.cfg.Public, fixture.cfg.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	current := prior.LiveFacts
	revised, err := buildPlanRevisionFromFacts(fixture.cfg, fixture.stateDir, prior, &current, executor.journal.Entries(), time.Unix(3, 0))
	if err != nil {
		t.Fatalf("funded capacity successor failed: %v", err)
	}
	if revised.ConfigHash == prior.ConfigHash || revised.PlanHash == prior.PlanHash || !revised.allowedPlanHashes()[prior.PlanHash] || !reflect.DeepEqual(revised.EvidenceRelayContinuation, prior.EvidenceRelayContinuation) {
		t.Fatal("capacity revision changed its immutable funding history or omitted current approval")
	}
	oldReserve := actionByID(t, prior, evidenceRelayReserveId)
	newReserve := actionByID(t, revised, evidenceRelayReserveId)
	if !reflect.DeepEqual(oldReserve, newReserve) || newReserve.Spend.EVMGasWei != "51200000000000000000" || !reflect.DeepEqual(revised.Limits, prior.Limits) {
		t.Fatal("capacity revision created another reserve or spending approval")
	}
	for _, pair := range [][2]Spend{{revised.MaximumSpend, prior.MaximumSpend}, {revised.SupersededSpend, prior.SupersededSpend}} {
		if same, err := equalSpend(pair[0], pair[1]); err != nil || !same {
			t.Fatalf("source capacity changed action economics: prior=%+v revised=%+v error=%v", pair[1], pair[0], err)
		}
	}
	after, err := json.Marshal(prior)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("successor rewrote its prior approval", err)
	}
	if err := writeRunInputs(fixture.cfg, fixture.stateDir, revised, fixture.roles); err != nil {
		t.Fatal(err)
	}
	if err := validateEvidenceRelayContinuationSource(fixture.stateDir, revised, executor.journal.Entries()); err != nil {
		t.Fatal("successor lost exact original source ancestry", err)
	}
	executor.plan = revised
	request := evidenceRelayLaunchRequestTest(t, fixture, fixture.prepared.Members[0], fixture.prepared.Epoch+1, false, 0)
	action, owner, err := executor.admitOwnedEvidenceRelayAction(t.Context(), request)
	if err != nil || owner != revised.PlanHash || action.Spend.EVMGasWei != "25000000000000000" {
		t.Fatal("exact funded v6 configuration stranded real relay admission", err)
	}
	configured := fixture.cfg.Config.ValidatorEvidenceRelay
	for _, slots := range []uint64{0, 257, 1024, 2047, 2049} {
		fixture.cfg.Config.ValidatorEvidenceRelay.MaxSlots = slots
		if err := validateEvidenceRelayContinuationConfig(fixture.cfg, revised); err == nil {
			t.Fatalf("unapproved configured %d slots acquired funding authority", slots)
		}
	}
	fixture.cfg.Config.ValidatorEvidenceRelay = configured
	fixture.cfg.Config.ValidatorEvidenceRelay.MaxSlots = 256
	if err := validateEvidenceRelayContinuationConfig(fixture.cfg, revised); err != nil {
		t.Fatal("immutable original template lost compatibility", err)
	}
	fixture.cfg.Config.ValidatorEvidenceRelay.GasUnits++
	if err := validateEvidenceRelayContinuationConfig(fixture.cfg, revised); err == nil {
		t.Fatal("different original per-call gas gained admission")
	}
}

// An omitted optional horizon preserves legacy hashes and original behavior.
// An explicit source horizon cannot claim more blocks than the funded slots.
func TestEvidenceRelayHorizonSeparatesSourceLifetimeFromSlotMargin(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	span, closed, native, err := evidenceRelayConfiguredHorizon(cfg)
	if err != nil || span != 10080 || closed != 35 || native != 29 {
		t.Fatal("explicit source cutoff changed", span, closed, native, err)
	}
	cfg.Config.ValidatorEvidenceRelay.SourceHorizonBlocks = 0
	legacy, _, _, err := evidenceRelayConfiguredHorizon(cfg)
	if err != nil || legacy <= span {
		t.Fatal("omitted cutoff changed the legacy slot-derived horizon", legacy, err)
	}
	raw, err := json.Marshal(cfg.Config.ValidatorEvidenceRelay)
	if err != nil || strings.Contains(string(raw), "source_horizon_blocks") {
		t.Fatal("absent optional bound changed persisted legacy bytes", string(raw), err)
	}
	cfg.Config.ValidatorEvidenceRelay.SourceHorizonBlocks = legacy + 1
	if _, _, _, err := evidenceRelayConfiguredHorizon(cfg); err == nil {
		t.Fatal("source lifetime exceeded its actual funded slot horizon")
	}
	fixture, horizon := newEvidenceRelayHorizonTestFixture(t)
	horizon.maximum = 2048
	horizon.sourceHorizon = span
	end, epoch, nativeEnd, err := horizon.ceilings(nil)
	if err != nil || end != horizon.anchorBlock+span || epoch != horizon.anchorEpoch+closed-1 || nativeEnd != horizon.anchorNativeEpoch+native-1 {
		t.Fatal("runtime failed to enforce the configured original cutoff", end, epoch, nativeEnd, err)
	}
	if err := horizon.requireRemaining(end, nativeEnd, 1); err == nil {
		t.Fatal("runtime extended source lifetime using spare funded slots")
	}
	if fixture.cfg.Config.ValidatorEvidenceRelay.MaxSlots != 256 {
		t.Fatal("new forecast mutated the historical original fixture")
	}
}
