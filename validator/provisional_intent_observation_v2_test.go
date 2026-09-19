//go:build linux || darwin

package validator

import (
	"strings"
	"testing"
)

func provisionalIntentPlanHash(byteValue string) string {
	return "0x" + strings.Repeat(byteValue, 32)
}

func TestProvisionalIntentObservationAcceptsOnlyAuthenticatedPlanLineage(t *testing.T) {
	current := provisionalIntentPlanHash("51")
	predecessor := provisionalIntentPlanHash("52")
	foreign := provisionalIntentPlanHash("53")
	if err := validateProvisionalIntentPlanLineage(current, []string{predecessor, current}, predecessor); err != nil {
		t.Fatalf("approved predecessor rejected: %v", err)
	}
	for name, test := range map[string]struct {
		accepted []string
		handoff  string
	}{
		"missing-current": {accepted: []string{predecessor}, handoff: predecessor},
		"foreign-handoff": {accepted: []string{current, predecessor}, handoff: foreign},
		"noncanonical":    {accepted: []string{current, "not-a-hash"}, handoff: predecessor},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateProvisionalIntentPlanLineage(current, test.accepted, test.handoff); err == nil {
				t.Fatal("unapproved lineage was accepted")
			}
		})
	}
}

func TestProvisionalActivationObservationLoadsOnlyReviewedRetainedRuntime(t *testing.T) {
	cfg := validReleaseConfig(t)
	cfg.RuntimeSpec = 461
	cfg.RuntimeCodeHash = "0x15cf19d2f4f8e2a8a6f46cb735db8f9f03ba3775866188fa93799ad3a040da2e"
	cfg.RuntimeMetadataHash = "0x98b2cfd0d6633488dfe5b3b70b869d5753aa3c42396533013df131e4e0e5ca68"
	cfg.ProvisionalRuntimeCompatibility = "urnetwork-subtensor-consumed-interface-v1"
	path := writeReleaseConfig(t, cfg)
	if _, err := LoadReleaseConfig(path); err == nil {
		t.Fatal("current producer loader admitted retained runtime")
	}
	loaded, err := LoadProvisionalActivationObservationConfig(path)
	if err != nil || loaded.RuntimeSpec != 461 || loaded.ProvisionalRuntimeCompatibility == "" {
		t.Fatalf("retained observation loader=%+v err=%v", loaded, err)
	}
	cfg.RuntimeCodeHash = "0x" + strings.Repeat("aa", 32)
	if _, err := LoadProvisionalActivationObservationConfig(writeReleaseConfig(t, cfg)); err == nil {
		t.Fatal("unreviewed retained runtime was admitted")
	}
}
