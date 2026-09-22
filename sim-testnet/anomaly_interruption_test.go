// Reporting distinguishes an early campaign stop from tests that never ran;
// the failed interval and all original evidence remain required for acceptance.
package main

import (
	"reflect"
	"testing"
	"time"
)

// An explicit interrupted window ends before its first ordinary fault trigger.
func interruptedScenarioAnomalyFixture(t *testing.T) (*ScenarioResult, *ScenarioObservation) {
	t.Helper()
	_, definition, window, observations := newScenarioIntervalFixture(t)
	faults, err := initializeFaultRecords(window.StartBlock, definition.Faults)
	if err != nil {
		t.Fatal(err)
	}
	current := observations[0]
	started := time.Now().UTC()
	return &ScenarioResult{RunID: "interrupted-synthetic-campaign", StartedAt: started.Format(time.RFC3339Nano), EndHead: current.Status.Contracts.FinalizedHead, AcceptanceWindow: window, Faults: faults,
		Assertions: []AssertionRecord{
			scenarioInterruptedIntervalAssertion(window, current, started, started),
			{ID: "scenario_context", Passed: false, Message: "synthetic process log integrity failure"},
		},
		Adversaries: &AdversaryCampaignEvidence{
			Actors:  []AdversaryActorEvidence{{ID: "unstarted-actor", Status: "stopped"}},
			Vectors: []AdversaryVectorEvidence{{ID: "unstarted-vector", ActorIDs: []string{"unstarted-actor"}, Status: "fail"}},
		},
	}, current
}

// Only consequence metadata changes; the actual cause and final verdict stay
// blocking, and no scheduled fault or adversary result is fabricated as passed.
func TestScenarioAnomalyInterruptedLabelsOnlyUnexercisedConsequences(t *testing.T) {
	t.Parallel()
	result, current := interruptedScenarioAnomalyFixture(t)
	faults := cloneScenarioFaultRecords(result.Faults)
	vector := result.Adversaries.Vectors[0]
	attachScenarioAnomalyGate(result, time.Now().UTC(), current, current)
	derived := 0
	for _, entry := range result.Anomalies.Entries {
		if entry.DerivedFrom != "" {
			derived++
			if entry.Status != "derived" || entry.Severity != "info" || entry.Disposition != "not-exercised-before-interruption" {
				t.Fatal("unexercised consequence lost its explicit disposition", entry)
			}
		}
	}
	if derived != 2 || result.Result != "fail" || result.Anomalies.Status != "open" || !reflect.DeepEqual(faults, result.Faults) || !reflect.DeepEqual(vector, result.Adversaries.Vectors[0]) {
		t.Fatalf("derived report erased evidence or accepted an incomplete interval: %+v", result)
	}
}

// An attempted fault, elapsed trigger, missing stop cause or actual actor
// error cannot be reclassified as an unexercised consequence of interruption.
func TestScenarioAnomalyInterruptedPreservesAttemptedAndUnknownFailures(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		mutate func(*ScenarioResult)
		source string
	}{
		{name: "fault-applied", mutate: func(result *ScenarioResult) { result.Faults[0].AppliedBlock = 1 }, source: "fault:synthetic-pause"},
		{name: "fault-armed", mutate: func(result *ScenarioResult) { result.Faults[0].ArmedBlock = 1 }, source: "fault:synthetic-pause"},
		{name: "pre-acceptance", mutate: func(result *ScenarioResult) { result.Faults[0].PreAcceptance = true }, source: "fault:synthetic-pause"},
		{name: "trigger-observed", mutate: func(result *ScenarioResult) { result.EndHead.Number = result.Faults[0].TriggerBlock }, source: "fault:synthetic-pause"},
		{name: "fault-error", mutate: func(result *ScenarioResult) { result.Faults[0].Error = "synthetic scheduler failure" }, source: "fault:synthetic-pause"},
		{name: "missing-cause", mutate: func(result *ScenarioResult) { result.Assertions = result.Assertions[:1] }, source: "fault:synthetic-pause"},
		{name: "not-interrupted", mutate: func(result *ScenarioResult) { result.Assertions = result.Assertions[1:] }, source: "fault:synthetic-pause"},
		{name: "actor-error", mutate: func(result *ScenarioResult) { result.Adversaries.Actors[0].Errors = 1 }, source: "adversary-vector:unstarted-vector"},
		{name: "actor-request", mutate: func(result *ScenarioResult) { result.Adversaries.Actors[0].Requests = 1 }, source: "adversary-vector:unstarted-vector"},
		{name: "actor-sample", mutate: func(result *ScenarioResult) { result.Adversaries.Actors[0].Samples = 1 }, source: "adversary-vector:unstarted-vector"},
		{name: "actor-missing", mutate: func(result *ScenarioResult) { result.Adversaries.Actors = nil }, source: "adversary-vector:unstarted-vector"},
	} {
		result, current := interruptedScenarioAnomalyFixture(t)
		test.mutate(result)
		attachScenarioAnomalyGate(result, time.Now().UTC(), current, current)
		found := false
		for _, entry := range result.Anomalies.Entries {
			if entry.Source == test.source {
				found = true
				if entry.DerivedFrom != "" || entry.Status != "open" || entry.Severity != "critical" {
					t.Fatalf("%s actual failure was downgraded: %+v", test.name, entry)
				}
			}
		}
		if !found || result.Result != "fail" {
			t.Fatalf("%s failure disappeared", test.name)
		}
	}
}
