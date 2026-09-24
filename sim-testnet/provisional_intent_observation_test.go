//go:build linux || darwin

package main

import (
	"context"
	"testing"
)

// Both observation entry points reject absent ownership without dereferencing
// a nil context or a partially constructed admitted configuration.
func TestProvisionalIntentObservationRejectsIncompleteOwner(t *testing.T) {
	for _, cfg := range []*ResolvedConfig{nil, {}, {provisionalResume: &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true}}}} {
		observed, generation := observeProvisionalValidatorIntent(context.Background(), cfg, t.TempDir(), 1)
		if observed.Error == "" || generation != "" || observed.LocalRuntimeIntents == nil || observed.LocalRuntimeIntents.State != "unknown" {
			t.Fatalf("missing owner gained observation authority: %+v", observed)
		}
	}
	cfg := testResolvedConfig(t)
	cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true}}
	observed, generation := observeProvisionalValidatorIntent(nil, cfg, t.TempDir(), 1)
	if observed.Error == "" || generation != "" || observed.LocalRuntimeIntents == nil {
		t.Fatal("nil context gained observation authority")
	}
	if observation, err := observeProvisionalValidatorSourceV2(nil, cfg, t.TempDir(), 1, ProcessSpec{ID: "validator-1", Role: "validator", Args: []string{"__validator"}}, [32]byte{}); err == nil || observation != nil {
		t.Fatal("nil source context gained observation authority")
	}
	calls := 0
	projected := inspectProvisionalValidatorIntentObserved(t.Context(), cfg, 1, func() (ValidatorObservation, string) {
		calls++
		return ValidatorObservation{ValidatorID: 1}, "synthetic-generation"
	})
	if calls != 1 || projected.Error == "" || projected.LocalRuntimeIntents == nil || projected.LocalRuntimeIntents.State != "unknown" || projected.FinalizedIntents != 0 || projected.AppliedIntents != 0 {
		t.Fatalf("nil successful source gained projection authority: %+v", projected)
	}
}
