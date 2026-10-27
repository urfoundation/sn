// Live custody checks must retain the exact attempt floor and outcome through
// the final send/result boundary, independently of otherwise healthy chain reads.
package main

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Observation two runs after the durable attempt. Each fault therefore proves
// that a previously successful admission cannot spend from missing custody.
func TestBootstrapSuccessorExecutionStopsWhenCountedCustodyChangesBeforeSend(t *testing.T) {
	for _, fault := range []string{"attempt-intent", "attempt-record", "adoption-intent", "adoption-record", "claim", "ready", "changed-attempt", "changed-adoption", "nonprivate-attempt", "hardlinked-attempt", "extra-event", "extra-stage"} {
		f := newBootstrapSuccessorExecutionFixture(t)
		owner := f.open(true, nil)
		directory := f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory
		nonces := bootstrapSuccessorPreparationTestFiles(t, f.approval.Plan.Request.RegistryDirectory)
		var retained map[string]string
		f.observeHook = func(number int, _ *bootstrapSuccessorExecutionObservation) {
			if number != 2 {
				return
			}
			name := bootstrapSuccessorExecutionEventName(1) + ".json"
			switch fault {
			case "attempt-intent":
				name = bootstrapSuccessorExecutionEventName(1) + ".intent"
			case "adoption-intent":
				name = bootstrapSuccessorExecutionEventName(0) + ".intent"
			case "adoption-record", "changed-adoption":
				name = bootstrapSuccessorExecutionEventName(0) + ".json"
			case "claim", "ready":
				name = bootstrapSuccessorExecutionPrefix + "." + fault
			case "extra-event":
				name = bootstrapSuccessorExecutionEventName(2) + ".intent"
			case "extra-stage":
				name = owner.local.stageName(bootstrapSuccessorExecutionEventName(2)+".intent", "attempt-reserved")
			}
			path := filepath.Join(directory, name)
			var err error
			switch {
			case strings.HasPrefix(fault, "changed-"), strings.HasPrefix(fault, "extra-"):
				err = os.WriteFile(path, []byte("synthetic conflicting execution custody"), 0600)
			case fault == "nonprivate-attempt":
				err = os.Chmod(path, 0644)
			case fault == "hardlinked-attempt":
				err = os.Link(path, filepath.Join(t.TempDir(), "synthetic-attempt-link"))
			default:
				err = os.Remove(path)
			}
			if err != nil {
				t.Fatal(fault, err)
			}
			retained = bootstrapSuccessorPreparationTestFiles(t, directory)
		}
		result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true)
		if retained == nil || err == nil || result.SubmissionAttempted || len(f.writes) != 0 || !owner.closed || owner.last.CumulativeAttempts != 9 {
			t.Fatal("successor sent from changed counted custody", fault, result, err)
		}
		if !maps.Equal(retained, bootstrapSuccessorPreparationTestFiles(t, directory)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, f.approval.Plan.Request.RegistryDirectory)) {
			t.Fatal("live custody refusal repaired evidence or released a nonce", fault)
		}
	}
}

// A full staged outcome can survive interrupted publication. Its reopened
// owner must preserve those exact bytes until canonical recovery completes;
// even truncation to an otherwise recoverable prefix is a new custody change.
func TestBootstrapSuccessorExecutionFreezesInterruptedOutcomeDuringOwnership(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err != nil {
		t.Fatal(err)
	}
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: f.receipt()}
	interrupted := errors.New("synthetic interruption after terminal intent bytes")
	owner.local.hook = func(stage string) error {
		if stage == bootstrapSuccessorExecutionEventName(2)+".intent:stage-written" {
			return interrupted
		}
		return nil
	}
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, false); !errors.Is(err, interrupted) {
		t.Fatal("terminal publication did not reach the intended interruption", err)
	}
	owner = f.open(false, nil)
	if owner.pending != "installed" {
		t.Fatal("interrupted terminal intent was not retained")
	}
	directory := f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory
	stageName := owner.local.stageName(bootstrapSuccessorExecutionEventName(2)+".intent", "installed")
	var retained map[string]string
	owner.local.hook = func(stage string) error {
		if stage == "event-admission" {
			if err := os.WriteFile(filepath.Join(directory, stageName), nil, 0600); err != nil {
				return err
			}
			retained = bootstrapSuccessorPreparationTestFiles(t, directory)
		}
		return nil
	}
	result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, false)
	if retained == nil || err == nil || result.InstallationComplete || len(f.writes) != 1 || !owner.closed || owner.last.CumulativeAttempts != 9 ||
		!maps.Equal(retained, bootstrapSuccessorPreparationTestFiles(t, directory)) {
		t.Fatal("successor replaced a changed interrupted outcome during ownership", result, err)
	}
}

// A result boundary follows canonical inclusion and durable terminal append.
// Losing that receipt before output must not turn in-memory success into a
// recoverable installation claim.
func TestBootstrapSuccessorExecutionStopsWhenTerminalCustodyChangesBeforeResult(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err != nil {
		t.Fatal(err)
	}
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: f.receipt()}
	directory := f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory
	var retained map[string]string
	owner.local.hook = func(stage string) error {
		if stage == "canonical-result-retained" {
			if err := os.Remove(filepath.Join(directory, bootstrapSuccessorExecutionEventName(2)+".json")); err != nil {
				return err
			}
			retained = bootstrapSuccessorPreparationTestFiles(t, directory)
		}
		return nil
	}
	result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, false)
	if retained == nil || err == nil || result.InstallationComplete || result.ActivationReady || len(f.writes) != 1 || !owner.closed || owner.last.CumulativeAttempts != 9 {
		t.Fatal("successor reported installation after losing its terminal custody", result, err)
	}
	if !maps.Equal(retained, bootstrapSuccessorPreparationTestFiles(t, directory)) {
		t.Fatal("terminal custody refusal recreated the missing receipt")
	}
}
