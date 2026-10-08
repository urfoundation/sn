// Signed scheduler selection changes fresh production timing only. Original
// approvals and retained transactions remain independently verifiable custody.
package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The actual production reader, signed approval and encrypted source batch
// carry the corrected post-initialization commit epoch through cold replay.
func TestProductionScheduleApprovedProfileReachesExactPreparedIntent(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixtureWithInputs(t, newRecycleOperatorFixtureWithHotkey, func(fixture *ownerRecycleProductionTestFixture) {
		admission := fixture.operator.measurement.admission
		for name, value := range map[string]uint64{"LastEpochBlock": 100, "BlocksSinceLastStep": 12} {
			admission.storage[fixture.storageNameKVs[name]] = codec.HexEncodeToString(binary.LittleEndian.AppendUint64(nil, value))
		}
	})
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	if intent.Prepared.EpochScheduleProfile != crv4.TempoDriftEpochScheduleProfile || intent.Prepared.PreparedAtBlock != 100 || intent.Prepared.RevealBlock != 113 || releaseSubmitOptions(fixture.cfg).EpochScheduleProfile != crv4.TempoDriftEpochScheduleProfile {
		t.Fatalf("production schedule was not selected from signed authority: %+v", intent.Prepared)
	}
	before, err := json.Marshal(intent.Prepared)
	if err != nil {
		t.Fatal(err)
	}
	fixture.head = 105
	admission := fixture.operator.measurement.admission
	admission.chain = &crv4.Chain{API: admission.chain.API, GenesisHash: admission.chain.GenesisHash}
	recovered, recoveredProvider := fixture.stage(t)
	measurement := fixture.operator.measurement
	if _, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, recovered, intent, measurement.encoded, measurement.provider.artifact, recoveredProvider); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(intent.Prepared)
	if !bytes.Equal(before, after) {
		t.Fatal("pending original intent acquired a different nonce, epoch, round or signed bytes")
	}
	intent.Prepared.EpochScheduleProfile = ""
	if _, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, recovered, intent, measurement.encoded, measurement.provider.artifact, recoveredProvider); err == nil {
		t.Fatal("new production approval reinterpreted an unprofiled prepared intent")
	}
}

// Omission preserves old signed bytes during decoding, but cannot select the
// new production scheduler or reach a fresh nonce allocation.
func TestProductionScheduleLegacyApprovalRehydratesWithoutFreshAuthority(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixtureWithInputs(t, newRecycleOperatorFixtureWithHotkey, func(fixture *ownerRecycleProductionTestFixture) {
		fixture.operator.measurement.admission.approval.Production.EpochScheduleProfile = ""
	})
	original := bytes.Clone(fixture.cfg.ownerRecycleProduction.encoded)
	if bytes.Contains(original, []byte("epoch_schedule_profile")) {
		t.Fatal("legacy signed approval acquired a new JSON field")
	}
	fixture.cfg.ownerRecycleProduction = nil
	if err := loadOwnerRecycleProductionConfig(fixture.cfg); err != nil || !bytes.Equal(original, fixture.cfg.ownerRecycleProduction.encoded) {
		t.Fatalf("legacy signed approval was reinterpreted on restart: %v", err)
	}
	approved, err := ownerRecycleProductionApproval(fixture.cfg)
	if err != nil || approved.Approval.Production.EpochScheduleProfile != "" || releaseSubmitOptions(fixture.cfg).EpochScheduleProfile != "" {
		t.Fatalf("legacy approval inferred a new schedule: %v", err)
	}
	stage, provider := fixture.stage(t)
	native := fixture.operator.measurement.admission.chain
	selected, err := types.NewHashFromHexString(stage.proof.Decision.NativeSnapshotHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, fixture.cfg, selected); err != nil {
		t.Fatal(err)
	}
	client := native.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	originalCall := client.callContext
	nonces := 0
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if method == "system_accountNextIndex" {
			nonces++
		}
		return originalCall(ctx, target, method, args...)
	}
	options := releaseSubmitOptions(fixture.cfg)
	options.SourceHash = stage.sourceHash
	row, err := ownerRecycleProductionRowDecision(provider, stage.proof.Row)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := crv4.PrepareWeightsCRv4ExactAtContext(t.Context(), native, fixture.hotkey, fixture.cfg.Netuid, row.UIDs, row.Scores, options, selected)
	if err == nil || prepared != nil || nonces != 0 || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("legacy approval reached fresh production preparation: prepared=%t nonces=%d err=%v", prepared != nil, nonces, err)
	}
}

// An independently valid signature does not make an unknown profile reviewed.
func TestProductionScheduleRejectsIndependentlySignedUnknownProfile(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	admission := fixture.operator.measurement.admission
	admission.approval.Production.EpochScheduleProfile = "unknown"
	admission.sign(t)
	fixture.cfg.ownerRecycleProduction = nil
	if err := loadOwnerRecycleProductionConfig(fixture.cfg); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("independently signed unknown scheduler was admitted: %v", err)
	}
}

// A policy renewal cannot silently relabel the original activation's timing.
// Switching profiles requires separately reviewed activation authority.
func TestProductionScheduleRenewalCannotRetargetOriginalApproval(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	current, approval := productionAuthorityTestSuccessor(t, fixture.cfg, fixture.approval, fixture.private, false)
	approval.Production.EpochScheduleProfile = crv4.TempoDriftEpochScheduleProfile
	approver := recycleAdmissionFixture{cfg: current, approval: approval, private: fixture.private}
	approver.sign(t)
	current.ownerRecycleProduction = nil
	if err := loadOwnerRecycleProductionConfig(current); err != nil {
		t.Fatal(err)
	}
	if err := validateProductionAuthorityContinuity(fixture.cfg, current); err == nil {
		t.Fatal("renewal retargeted legacy timing without a separate activation")
	}
}

// Old production signatures and pending intent metadata omit the new profile.
// A cold exact-runtime/provider replay can still grant only those retained bytes.
func TestProductionScheduleLegacyPendingIntentReplaysOriginalBytes(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixtureWithInputs(t, newRecycleOperatorFixtureWithHotkey, func(fixture *ownerRecycleProductionTestFixture) {
		fixture.operator.measurement.admission.approval.Production.EpochScheduleProfile = ""
	})
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	before, err := json.Marshal(intent.Prepared)
	if err != nil || bytes.Contains(before, []byte("epoch_schedule_profile")) {
		t.Fatalf("legacy pending fixture changed its wire fields: %v", err)
	}
	fixture.head = 105
	measurement := fixture.operator.measurement
	measurement.admission.chain = &crv4.Chain{API: measurement.admission.chain.API, GenesisHash: measurement.admission.chain.GenesisHash}
	recovered, recoveredProvider := fixture.stage(t)
	verified, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, recovered, intent, measurement.encoded, measurement.provider.artifact, recoveredProvider)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyReleaseMeasurementIntent(intent, measurement.provider.artifact, verified); err != nil {
		t.Fatal(err)
	}
	preparedHash, err := types.NewHashFromHexString(intent.Prepared.PreparedAtBlockHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := authenticateHistoricalNativeRuntimeAtContext(t.Context(), measurement.admission.chain, fixture.cfg, preparedHash); err != nil {
		t.Fatal(err)
	}
	if err := measurement.admission.chain.ValidatePreparedSourceWeightsContext(t.Context(), intent.Prepared, verified.UIDs, verified.Scores, releaseSubmitOptions(fixture.cfg)); err != nil {
		t.Fatal(err)
	}
	store := &IntentStore{v2: &releaseIntentV2Owner{runtime: &releaseRuntimeV2{cfg: *fixture.cfg}}}
	if err := store.retainOwnerRecyclePreparedAuthorization(intent); err != nil {
		t.Fatal(err)
	}
	granted, err := store.ownerRecyclePreparedConfig(t.Context(), fixture.cfg, intent.Prepared)
	if err != nil || validateOwnerRecyclePreparedAuthorization(granted, intent.Prepared) != nil {
		t.Fatalf("legacy pending bytes lost their exact retained-intent grant: %v", err)
	}
	after, _ := json.Marshal(intent.Prepared)
	if !bytes.Equal(before, after) {
		t.Fatal("legacy pending replay changed original nonce, epoch, round or signature")
	}
}
