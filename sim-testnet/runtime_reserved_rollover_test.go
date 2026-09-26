//go:build linux || darwin

// A provisional successor can observe the exact previous staging contexts
// without admitting changed capacity or treating them as current acceptance.
package main

import (
	"slices"
	"testing"

	validatorcomponent "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/server/controller"
)

// Only an authenticated predecessor's complete context census may explain a
// retained operator config after the selected generation rotates.
func TestRetainedRolloverReservedStagingMatchesExactPredecessor(t *testing.T) {
	previous := []validatorcomponent.ReleaseEvidenceV2File{
		{Path: "previous-1-1", Bytes: 1, SHA256: "one"},
		{Path: "previous-1-2", Bytes: 2, SHA256: "two"},
		{Path: "previous-2-1", Bytes: 3, SHA256: "three"},
		{Path: "previous-2-2", Bytes: 4, SHA256: "four"},
	}
	current := slices.Clone(previous)
	for index := range current {
		current[index].Path = "current-" + current[index].Path
	}
	predecessor := &policyRolloverHandoffV2{Validators: []policyRolloverValidatorHandoffV2{
		{Evidence: validatorcomponent.ReleaseEvidenceV2Config{Operators: []validatorcomponent.ReleaseEvidenceV2OperatorConfig{{Context: previous[0]}, {Context: previous[1]}}}},
		{Evidence: validatorcomponent.ReleaseEvidenceV2Config{Operators: []validatorcomponent.ReleaseEvidenceV2OperatorConfig{{Context: previous[2]}, {Context: previous[3]}}}},
	}}
	expected := &controller.StReservedAttemptUploadConfig{NativeRPCURLs: []string{"ws://127.0.0.1:9944"}}
	expected.Admission.ActivationContexts = current
	actual := *expected
	actual.Admission.ActivationContexts = slices.Clone(previous)
	if !retainedRolloverReservedStagingMatches(&actual, expected, predecessor, 2) {
		t.Fatal("exact predecessor contexts were not recognized")
	}
	changed := actual
	changed.NativeRPCURLs = []string{"ws://127.0.0.1:9945"}
	if retainedRolloverReservedStagingMatches(&changed, expected, predecessor, 2) {
		t.Fatal("changed endpoint passed the retained-context exception")
	}
	changed = actual
	changed.Admission.ActivationContexts = slices.Clone(previous)
	changed.Admission.ActivationContexts[3].Path = "unbound"
	if retainedRolloverReservedStagingMatches(&changed, expected, predecessor, 2) {
		t.Fatal("unbound context passed the predecessor census")
	}
	if retainedRolloverReservedStagingMatches(&actual, expected, nil, 2) || retainedRolloverReservedStagingMatches(&actual, expected, predecessor, 3) {
		t.Fatal("missing or incomplete predecessor passed")
	}
}
