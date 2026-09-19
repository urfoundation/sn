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
