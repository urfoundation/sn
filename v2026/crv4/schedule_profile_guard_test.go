// The new scheduler is explicit and fresh-production-only. Retained legacy
// records keep their original wire bytes, nonce, epoch and timelock target.
package crv4

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// Existing callers with no selected profile retain the old drand-v2 result;
// an unknown profile must fail rather than silently choose either scheduler.
func TestTempoDriftProfilePreservesLegacyAndRejectsUnknown(t *testing.T) {
	state := EpochScheduleState{LastEpochBlock: 1000, SubnetEpochIndex: 7, Tempo: 100, BlocksSinceLastStep: 100, CurrentBlock: 1000}
	legacy, err := PredictFirstRevealBlock(&state, 1)
	if err != nil || legacy != 1100 {
		t.Fatalf("legacy scheduler was retargeted: %d %v", legacy, err)
	}
	state.EpochScheduleProfile = "unknown"
	if _, err := PredictFirstRevealBlock(&state, 1); err == nil {
		t.Fatal("unknown schedule profile silently acquired legacy semantics")
	}
	state.EpochScheduleProfile, state.Tempo = TempoDriftEpochScheduleProfile, 0
	if _, err := PredictFirstRevealBlock(&state, 1); err != ErrTempoZero {
		t.Fatalf("tempo-drift profile ignored disabled epochs: %v", err)
	}
}

// Root-set tempo may exceed the owner limit. Keep the search conservative for
// the actual u16 value without changing legacy callers' original bound.
func TestTempoDriftProfileBoundsRootTempoPrediction(t *testing.T) {
	state := tempoDriftScheduleFixture(t, 65535)
	state.Tempo = 65535
	state.CurrentBlock, state.LastEpochBlock = 100000, 100000
	block, err := PredictFirstRevealBlock(&state, 4)
	if err != nil || block != 362141 {
		t.Fatalf("root tempo lost its bounded reveal prediction: %d %v", block, err)
	}
}

// Finite source bounds stop invalid authenticated snapshots before simulation;
// they cannot turn a large period or saturated epoch into unbounded local work.
func TestTempoDriftProfileRejectsUnboundedPredictionInputs(t *testing.T) {
	for _, fault := range []string{"zero-period", "large-period", "block-overflow", "future-anchor", "future-trigger", "counter", "epoch-overflow"} {
		state := tempoDriftScheduleFixture(t, 100)
		period := uint64(1)
		switch fault {
		case "zero-period":
			period = 0
		case "large-period":
			period = math.MaxUint64
		case "block-overflow":
			state.CurrentBlock = math.MaxUint32
		case "future-anchor":
			state.LastEpochBlock = 1001
		case "future-trigger":
			state.PendingEpochAt = math.MaxUint64
		case "counter":
			state.BlocksSinceLastStep = 1001
		case "epoch-overflow":
			state.SubnetEpochIndex = math.MaxUint64
		}
		if _, err := PredictFirstRevealBlock(&state, period); err == nil {
			t.Fatalf("unbounded %s prediction was accepted", fault)
		}
	}
}

// Missing/wrong profile and revoked exact-purpose authority fail before nonce
// allocation, encryption, or any RPC. Generic exact metadata is insufficient.
func TestTempoDriftProfileGuardsFreshProductionPreparation(t *testing.T) {
	for _, fault := range []string{"missing", "unknown", "generic-binding", "missing-profile-generic", "revoked", "mutated-view"} {
		fixture := validatorProducerRuntimeFixture(t)
		artifact := fixture.bind(t)
		if fault != "generic-binding" && fault != "missing-profile-generic" {
			if err := fixture.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
				t.Fatal(err)
			}
		}
		options := SubmitOptions{EpochScheduleProfile: TempoDriftEpochScheduleProfile}
		switch fault {
		case "missing":
			options.EpochScheduleProfile = ""
		case "missing-profile-generic":
			options.EpochScheduleProfile, options.RequireProductionRuntime = "", true
		case "unknown":
			options.EpochScheduleProfile = "unknown"
		case "revoked":
			if err := fixture.chain.BindRuntimeArtifact(artifact); err != nil {
				t.Fatal(err)
			}
		case "mutated-view":
			fixture.chain.Runtime.SpecVersion++
		}
		state := tempoDriftScheduleFixture(t, 100)
		before := fixture.calls
		prepared, err := prepareWeightsU16(t.Context(), fixture.chain, nil, 25, []uint16{1}, []uint16{65535}, 4, options, &state, fixture.block)
		if err == nil || prepared != nil || fixture.calls != before {
			t.Fatalf("%s schedule authority reached preparation: prepared=%v calls=%d err=%v", fault, prepared != nil, fixture.calls-before, err)
		}
	}
}

// The legacy signed fixture carries no new field. Cold decoding and signature
// validation do not migrate it to a different epoch, nonce or drand target.
func TestTempoDriftProfilePreservesLegacyPreparedReplay(t *testing.T) {
	prepared, _ := sourcePreparedTest(t)
	before, err := json.Marshal(prepared)
	if err != nil || bytes.Contains(before, []byte("epoch_schedule_profile")) {
		t.Fatalf("legacy prepared JSON changed: %v", err)
	}
	var recovered PreparedSubmission
	if err := json.Unmarshal(before, &recovered); err != nil {
		t.Fatal(err)
	}
	raw, err := recovered.Validate()
	if err != nil || len(raw) == 0 || recovered.ExtrinsicHex != prepared.ExtrinsicHex || recovered.AccountNonce != 129 || recovered.SubnetEpoch != 23 || recovered.RevealRound != 2200 || recovered.RevealBlock != 900 || recovered.EpochScheduleProfile != "" {
		t.Fatalf("legacy signed record was reinterpreted: %v", err)
	}
	after, err := json.Marshal(&recovered)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("legacy rehydration changed its exact JSON: %v", err)
	}
	recovered.EpochScheduleProfile = "unknown"
	if _, err := recovered.Validate(); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("unreviewed durable schedule was accepted: %v", err)
	}
}
