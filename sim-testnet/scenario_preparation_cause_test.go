//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Parent cancellation after preparation must retain the relay's exact cause
// without advancing observations or claiming that native readiness expired.
func TestScenarioPreparationPreservesParentCancellationCause(t *testing.T) {
	cfg := testResolvedConfig(t)
	relayFailure := errors.New("synthetic relay publication identity differs")
	for _, cause := range []error{relayFailure, context.Canceled, context.DeadlineExceeded, errors.Join(relayFailure, context.DeadlineExceeded)} {
		dir := t.TempDir()
		ctx, cancel := context.WithCancelCause(t.Context())
		campaign := &scenarioAdversaryStub{evidence: healthyAdversaryEvidence()}
		definition := scenarioDefinition{
			Name: "unit-preparation-cause", AdversarialMatrixHash: campaign.evidence.MatrixHash,
			Checks: []scenarioCheck{{ID: "unused", Check: func(*scenarioEvaluation) (bool, string) { return true, "" }}},
		}
		probe := &staticScenarioProbe{observations: []*ScenarioObservation{testScenarioObservation(cfg, 7)}}
		result, err := runScenarioWithProbe(ctx, cfg, dir, definition, probe, scenarioRunOptions{
			Adversaries: campaign,
			Prepare: func(context.Context) error {
				cancel(cause)
				return nil
			},
		})
		cancel(nil)
		if !errors.Is(err, cause) || result == nil || result.Result != "fail" || probe.calls != 1 || campaign.startCalls != 1 || campaign.stopCalls != 1 {
			t.Fatalf("cause=%v result=%+v err=%v calls=%d campaign=%+v", cause, result, err, probe.calls, campaign)
		}
		if !strings.Contains(err.Error(), "scenario preparation canceled by parent") || strings.Contains(err.Error(), "native readiness deadline") {
			t.Fatalf("parent cause was mislabeled: %v", err)
		}
		data, readErr := os.ReadFile(filepath.Join(dir, "runs", result.RunID, "result.json"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		var persisted ScenarioResult
		if err := json.Unmarshal(data, &persisted); err != nil {
			t.Fatal(err)
		}
		causeRetained := false
		for _, assertion := range persisted.Assertions {
			if assertion.ID == "initial_observation" {
				causeRetained = !assertion.Passed && strings.Contains(assertion.Message, cause.Error()) && !strings.Contains(assertion.Message, "native readiness deadline")
			}
		}
		if !causeRetained {
			t.Fatalf("persisted parent cause differs: %+v", persisted.Assertions)
		}
		if _, err := os.Stat(filepath.Join(dir, "runs", result.RunID, "complete.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("canceled preparation published completion: %v", err)
		}
	}
}
