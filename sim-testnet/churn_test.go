package main

import (
	"encoding/json"
	"math"
	"testing"
)

func TestChallengerChurnPreStateRejectsEveryUnsafeAdjacentState(t *testing.T) {
	canonical := challengerChurnState{ExpectedUID: 7, RuntimePruneUID: 7, ChurnUID: 7, ChurnFound: true, UIDCount: 256, MaximumUIDs: 256}
	if err := validateChallengerChurnPreState(canonical); err != nil {
		t.Fatal(err)
	}
	tests := []challengerChurnState{
		{ExpectedUID: 7, RuntimePruneUID: 7, ChurnUID: 7, ChurnFound: true, UIDCount: 255, MaximumUIDs: 256},
		{ExpectedUID: 7, RuntimePruneUID: 7, ChurnUID: 7, ChurnFound: true, UIDCount: 256, MaximumUIDs: 0},
		{ExpectedUID: 7, RuntimePruneUID: 7, ChallengerUID: 8, ChallengerFound: true, ChurnUID: 7, ChurnFound: true, UIDCount: 256, MaximumUIDs: 256},
		{ExpectedUID: 7, RuntimePruneUID: 7, ChurnUID: 8, ChurnFound: true, UIDCount: 256, MaximumUIDs: 256},
		{ExpectedUID: 7, RuntimePruneUID: 7, ChurnUID: 7, ChurnFound: false, UIDCount: 256, MaximumUIDs: 256},
		{ExpectedUID: 7, RuntimePruneUID: 8, ChurnUID: 7, ChurnFound: true, UIDCount: 256, MaximumUIDs: 256},
	}
	for index, state := range tests {
		if err := validateChallengerChurnPreState(state); err == nil {
			t.Fatalf("unsafe pre-state %d was accepted: %+v", index, state)
		}
	}
}

func TestChallengerChurnPostStateRequiresExactInPlaceReplacement(t *testing.T) {
	canonical := challengerChurnState{ExpectedUID: 7, ChallengerUID: 7, ChallengerFound: true, ChurnFound: false, UIDCount: 256, MaximumUIDs: 256}
	if err := validateChallengerChurnPostState(canonical); err != nil {
		t.Fatal(err)
	}
	tests := []challengerChurnState{
		{ExpectedUID: 7, ChallengerUID: 8, ChallengerFound: true, ChurnFound: false, UIDCount: 256, MaximumUIDs: 256},
		{ExpectedUID: 7, ChallengerUID: 7, ChallengerFound: false, ChurnFound: false, UIDCount: 256, MaximumUIDs: 256},
		{ExpectedUID: 7, ChallengerUID: 7, ChallengerFound: true, ChurnUID: 7, ChurnFound: true, UIDCount: 256, MaximumUIDs: 256},
		{ExpectedUID: 7, ChallengerUID: 7, ChallengerFound: true, ChurnFound: false, UIDCount: 255, MaximumUIDs: 256},
		{ExpectedUID: 7, ChallengerUID: 7, ChallengerFound: true, ChurnFound: false, UIDCount: 256, MaximumUIDs: 0},
	}
	for index, state := range tests {
		if err := validateChallengerChurnPostState(state); err == nil {
			t.Fatalf("unsafe post-state %d was accepted: %+v", index, state)
		}
	}
}

func TestContractRegistrationReplacementStatesRejectEveryAdjacentMutation(t *testing.T) {
	pre := registrationReplacementState{ExpectedUID: 7, RuntimePruneUID: 7, ChurnUID: 7, ChurnFound: true, UIDCount: 256, MaximumUIDs: 256}
	if err := validateRegistrationReplacementPreState(pre); err != nil {
		t.Fatal(err)
	}
	preMutations := []func(*registrationReplacementState){
		func(value *registrationReplacementState) { value.UIDCount-- },
		func(value *registrationReplacementState) { value.MaximumUIDs = 0 },
		func(value *registrationReplacementState) { value.ReplacementFound = true },
		func(value *registrationReplacementState) { value.ChurnFound = false },
		func(value *registrationReplacementState) { value.ChurnUID++ },
		func(value *registrationReplacementState) { value.RuntimePruneUID++ },
	}
	for index, mutate := range preMutations {
		changed := pre
		mutate(&changed)
		if err := validateRegistrationReplacementPreState(changed); err == nil {
			t.Fatalf("unsafe contract replacement pre-state %d was accepted: %+v", index, changed)
		}
	}
	post := registrationReplacementState{ExpectedUID: 7, ReplacementUID: 7, ReplacementFound: true, ChurnFound: false, UIDCount: 256, MaximumUIDs: 256}
	if err := validateRegistrationReplacementPostState(post); err != nil {
		t.Fatal(err)
	}
	postMutations := []func(*registrationReplacementState){
		func(value *registrationReplacementState) { value.UIDCount-- },
		func(value *registrationReplacementState) { value.MaximumUIDs = 0 },
		func(value *registrationReplacementState) { value.ReplacementFound = false },
		func(value *registrationReplacementState) { value.ReplacementUID++ },
		func(value *registrationReplacementState) { value.ChurnFound = true },
	}
	for index, mutate := range postMutations {
		changed := post
		mutate(&changed)
		if err := validateRegistrationReplacementPostState(changed); err == nil {
			t.Fatalf("unsafe contract replacement post-state %d was accepted: %+v", index, changed)
		}
	}
}

func TestObservedPostconditionUIDAcceptsOnlyExactUint16Values(t *testing.T) {
	valid := []any{float64(12), json.Number("12"), uint16(12), uint64(12)}
	for index, value := range valid {
		uid, err := observedPostconditionUID(value)
		if err != nil || uid != 12 {
			t.Fatalf("valid UID %d (%T) decoded as %d, err=%v", index, value, uid, err)
		}
	}
	invalid := []any{float64(-1), float64(1.5), float64(65536), math.NaN(), math.Inf(1), json.Number("-1"), json.Number("65536"), uint64(65536), int(12), nil}
	for index, value := range invalid {
		if _, err := observedPostconditionUID(value); err == nil {
			t.Fatalf("invalid UID %d (%T=%v) was accepted", index, value, value)
		}
	}
}

func TestRuntime453PruneCandidateMirrorsImmunityFloorAndTieBreakers(t *testing.T) {
	allImmune := []runtime453PruneNeuron{
		{UID: 0, Immortal: true, Immune: true, RegistrationBlock: 1},
		{UID: 4, Immune: true, EmissionRao: 0, RegistrationBlock: 20},
		{UID: 3, Immune: true, EmissionRao: 0, RegistrationBlock: 20},
		{UID: 2, Immune: true, EmissionRao: 1, RegistrationBlock: 10},
	}
	uid, err := runtime453PruneCandidate(allImmune, 10)
	if err != nil || uid != 3 {
		t.Fatalf("all-immune candidate=%d err=%v, want UID 3", uid, err)
	}

	nonImmune := []runtime453PruneNeuron{
		{UID: 0, Immortal: true},
		{UID: 1, EmissionRao: 2, RegistrationBlock: 1},
		{UID: 2, EmissionRao: 1, RegistrationBlock: 3},
		{UID: 3, EmissionRao: 1, RegistrationBlock: 2},
		{UID: 4, Immune: true, EmissionRao: 0, RegistrationBlock: 1},
	}
	uid, err = runtime453PruneCandidate(nonImmune, 2)
	if err != nil || uid != 3 {
		t.Fatalf("non-immune candidate=%d err=%v, want UID 3", uid, err)
	}
	if uid, err = runtime453PruneCandidate(nonImmune[:3], 2); err == nil {
		// There is no immune non-owner fallback in this slice, so the runtime
		// must reject instead of violating its minimum-free floor.
		t.Fatalf("minimum-free floor returned UID %d without an immune fallback", uid)
	}
	uid, err = runtime453PruneCandidate([]runtime453PruneNeuron{
		{UID: 0, Immortal: true},
		{UID: 1, EmissionRao: 1},
		{UID: 2, EmissionRao: 1},
		{UID: 9, Immune: true, EmissionRao: 100},
	}, 2)
	if err != nil || uid != 9 {
		t.Fatalf("exact minimum-free floor candidate=%d err=%v, want immune UID 9", uid, err)
	}
}

// Reproduces the live netuid-521 failure: once enough controlled registrations
// age out of immunity, the older zero-emission external UID becomes the runtime
// candidate even though it is not part of the approved churn floor.
func TestRuntime453PruneCandidateSelectsExternalBootstrapAfterRecoveryWindowExpires(t *testing.T) {
	neurons := []runtime453PruneNeuron{
		{UID: 0, Immortal: true, RegistrationBlock: 1},
		{UID: 1, RegistrationBlock: 100},
	}
	for uid := uint16(2); uid <= 12; uid++ {
		neurons = append(neurons, runtime453PruneNeuron{UID: uid, RegistrationBlock: 1_000 + uint64(uid)})
	}
	uid, err := runtime453PruneCandidate(neurons, 10)
	if err != nil || uid != 1 {
		t.Fatalf("expired recovery window candidate=%d err=%v, want external bootstrap UID 1", uid, err)
	}
}

// The reviewed bootstrap period puts only the old external UID outside
// immunity. Runtime 453 preserves its minimum non-immune floor and therefore
// selects the oldest controlled immune identity for bounded replacement.
func TestRuntime453PruneCandidatePreservesExternalBootstrapInsideRecoveryWindow(t *testing.T) {
	neurons := []runtime453PruneNeuron{
		{UID: 0, Immortal: true, RegistrationBlock: 1},
		{UID: 1, RegistrationBlock: 100},
	}
	for uid := uint16(2); uid <= 12; uid++ {
		neurons = append(neurons, runtime453PruneNeuron{UID: uid, Immune: true, RegistrationBlock: 1_000 + uint64(uid)})
	}
	uid, err := runtime453PruneCandidate(neurons, 10)
	if err != nil || uid != 2 {
		t.Fatalf("active recovery window candidate=%d err=%v, want controlled churn UID 2", uid, err)
	}
}

// A period that also covers the older external registration is unsafe: the
// immune fallback again chooses it by registration age. This is the adjacent
// upper boundary to the expired-window failure.
func TestRuntime453PruneCandidateSelectsExternalBootstrapWhenRecoveryWindowIsOverwide(t *testing.T) {
	neurons := []runtime453PruneNeuron{
		{UID: 0, Immortal: true, Immune: true, RegistrationBlock: 1},
		{UID: 1, Immune: true, RegistrationBlock: 100},
		{UID: 2, Immune: true, RegistrationBlock: 1_002},
	}
	uid, err := runtime453PruneCandidate(neurons, 10)
	if err != nil || uid != 1 {
		t.Fatalf("overwide recovery window candidate=%d err=%v, want external bootstrap UID 1", uid, err)
	}
}
