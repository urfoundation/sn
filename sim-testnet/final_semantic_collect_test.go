package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/connect"
)

func TestVerifyFinalCollectedAttemptRecordsAcceptsRecoveredPendingLifecycle(t *testing.T) {
	firstTrail := connect.Id{1}
	secondTrail := connect.Id{2}
	records := &FinalCollectedAttemptRecords{
		Schema: finalCollectedAttemptRecordsSchema, ValidatorID: 1, NoID: 1, FirstSequence: 1, LastSequence: 4,
		DispositionCounts: map[string]uint64{
			validatorpkg.AttemptDispositionPending:        2,
			validatorpkg.AttemptDispositionValidatorError: 1,
			validatorpkg.AttemptDispositionComplete:       1,
		},
		PendingRecoveredCount: 1,
		Records: []validatorpkg.AttemptRecord{
			{Sequence: 1, TrailID: firstTrail, Disposition: validatorpkg.AttemptDispositionPending},
			{Sequence: 2, TrailID: firstTrail, Disposition: validatorpkg.AttemptDispositionValidatorError},
			{Sequence: 3, TrailID: secondTrail, Disposition: validatorpkg.AttemptDispositionPending},
			{Sequence: 4, TrailID: secondTrail, Disposition: validatorpkg.AttemptDispositionComplete},
		},
	}
	summary := &FinalCollectedValidatorAttempts{
		NoID: 1, RecordCount: 4, CheckpointCount: 2, CompleteCount: 1, FailedCount: 1,
		PendingCount: 0, PendingRecoveredCount: 1,
	}
	if err := verifyFinalCollectedAttemptRecords(records, summary); err != nil {
		t.Fatal(err)
	}
	bad := *summary
	bad.PendingCount = 1
	if err := verifyFinalCollectedAttemptRecords(records, &bad); err == nil {
		t.Fatal("unresolved pending summary was accepted")
	}
	bad = *summary
	bad.PendingRecoveredCount = 0
	if err := verifyFinalCollectedAttemptRecords(records, &bad); err == nil {
		t.Fatal("recovered pending record was omitted from summary")
	}
}

func TestFinalLifecycleIntentRequirementsKeepSettlementAndNativeClocksDistinct(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	census, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("lifecycle fixture ready=%t error=%v", ready, err)
	}
	census.Phase = "release-1.0"
	observation.FleetLifecycle = &FleetLifecycleEvidence{
		FirstAcceptedEpoch:              census.Validators[0].SettlementEpoch,
		AcceptanceStartBlock:            1,
		AcceptanceEndBlock:              1_501,
		AcceptanceTerminalBlock:         1_651,
		ReleaseHandoffSchedule:          &FleetLifecycleNativeSchedule{ApplicationDeadlineBlock: 1_651},
		ReleaseEVMEvidenceDeadlineBlock: 1_651,
		CandidateCensuses:               []FleetLifecycleCandidateCensus{census},
	}
	requirements, err := finalLifecycleIntentRequirements(observation, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(requirements) != 2 || len(requirements[1]) != 1 || requirements[1][0].SettlementEpoch == requirements[1][0].SubnetEpoch {
		t.Fatalf("dual-clock lifecycle requirements=%+v", requirements)
	}
	expected := requirements[1][0]
	intent := &validatorpkg.SteeringIntent{
		ValidatorID:             uint64(expected.ValidatorID),
		Status:                  "applied",
		SettlementEpoch:         expected.SettlementEpoch,
		SubnetEpoch:             expected.SubnetEpoch,
		NativeSnapshotBlock:     expected.NativeSnapshot.Number,
		NativeSnapshotHash:      expected.NativeSnapshot.Hash,
		EVMSnapshotBlock:        expected.EVMSnapshot.Number,
		EVMSnapshotHash:         expected.EVMSnapshot.Hash,
		MeasurementArtifactHash: expected.MeasurementArtifactHash,
		VectorHash:              expected.VectorHash,
		ExtrinsicHash:           expected.ExtrinsicHash,
		FinalizedBlock:          expected.Commit.Number,
		FinalizedBlockHash:      expected.Commit.Hash,
		RevealBlock:             expected.RevealBlock,
		ApplicationBlock:        expected.Application.Number,
		ApplicationBlockHash:    expected.Application.Hash,
		EligibleHeadUIDs:        slices.Clone(expected.EligibleUIDs),
		SelectedHeadUIDs:        slices.Clone(expected.SelectedUIDs),
		RejectedHeadUIDs:        slices.Clone(expected.RejectedUIDs),
	}
	for _, weight := range expected.AppliedWeights {
		intent.UIDs = append(intent.UIDs, weight.UID)
		intent.Values = append(intent.Values, weight.Value)
	}
	if !finalLifecycleIntentMatches(intent, expected) {
		t.Fatal("exact dual-clock lifecycle intent did not match")
	}
	for index, uid := range intent.UIDs {
		if slices.Contains(expected.RejectedUIDs, uid) {
			intent.UIDs = append(intent.UIDs[:index], intent.UIDs[index+1:]...)
			intent.Values = append(intent.Values[:index], intent.Values[index+1:]...)
			break
		}
	}
	if !finalLifecycleIntentMatches(intent, expected) {
		t.Fatal("sparse lifecycle intent did not preserve an implicit rejected zero")
	}
	missingPositive := *intent
	missingPositive.UIDs = append([]uint16(nil), intent.UIDs...)
	missingPositive.Values = append([]uint16(nil), intent.Values...)
	for index, uid := range missingPositive.UIDs {
		if slices.Contains(expected.SelectedUIDs, uid) {
			missingPositive.UIDs = append(missingPositive.UIDs[:index], missingPositive.UIDs[index+1:]...)
			missingPositive.Values = append(missingPositive.Values[:index], missingPositive.Values[index+1:]...)
			break
		}
	}
	if finalLifecycleIntentMatches(&missingPositive, expected) {
		t.Fatal("sparse lifecycle intent omitted a positive selected weight")
	}
	intent.SubnetEpoch = intent.SettlementEpoch
	if finalLifecycleIntentMatches(intent, expected) {
		t.Fatal("settlement epoch substituted for native subnet epoch")
	}
	if other, err := finalLifecycleIntentRequirements(observation, "production-soak"); err != nil || len(other) != 0 {
		t.Fatalf("release lifecycle intents leaked into production collection: %v %+v", err, other)
	}
}

func TestVerifyFinalCollectedPriorManifestUsesPublishedCampaignKind(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	owner := roles.EVM["testnet-owner"]
	key, err := crypto.HexToECDSA(strings.TrimPrefix(owner.PrivateKeyHex, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	const runID = "prior-release-run"
	envelope, err := signEvidence(cfg, campaignEvidenceManifestKind, runID, map[string]any{"schema": campaignEvidenceManifestSchema}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalCollectedPriorManifestEnvelope(envelope, &key.PublicKey, runID, envelope.ContentHash); err != nil {
		t.Fatalf("published campaign manifest kind was rejected: %v", err)
	}
	oldLiteral, err := signEvidence(cfg, "campaign-evidence-manifest", runID, map[string]any{"schema": campaignEvidenceManifestSchema}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalCollectedPriorManifestEnvelope(oldLiteral, &key.PublicKey, runID, oldLiteral.ContentHash); err == nil {
		t.Fatal("obsolete campaign manifest literal was accepted")
	}
}
