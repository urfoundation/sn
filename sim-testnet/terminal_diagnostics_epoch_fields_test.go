// Diagnostic attribution retains legacy bytes and cannot close missing epochs.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTerminalDiagnosticEpochFieldsPreserveLegacyUnavailableEvidence(t *testing.T) {
	e := scenarioClaimWindowTestEvaluation()
	e.Current.Operators = []OperatorObservation{{NoID: 1, LatestArtifactEpoch: 102}, {NoID: 2, LatestArtifactEpoch: 102}}
	for index := range e.Current.Claims {
		e.Current.Claims[index].LastDiscovered = 0
		e.Current.Claims[index].EpochOutcomes = nil
	}
	before, _ := json.Marshal(e.Current)
	collector := &terminalDiagnosticCollector{ctx: t.Context(), report: &terminalDiagnosticReport{ReadOnly: true}}
	collector.check("original-failure", "", time.Minute, func(context.Context) (any, error) { return nil, errors.New("retained original failure") })
	collectTerminalDiagnosticEpochObservationFields(collector, e.Current, "")
	check := collector.report.Checks[1]
	if check.Status != "unavailable" || !strings.Contains(check.Detail, "default last_discovered value is not observed") || !strings.Contains(check.Detail, "latest epoch 102 alone") || collector.report.Checks[0].Status != "fail" || collector.report.FinalAcceptance {
		t.Fatalf("legacy producer mismatch became measured failure or success: %+v", collector.report.Checks)
	}
	if claims, err := scenarioClaimsForAcceptance(e); err == nil || claims != nil {
		t.Fatal("unavailable diagnostic fields authorized strict coverage")
	}
	after, _ := json.Marshal(e.Current)
	if !bytes.Equal(before, after) {
		t.Fatal("diagnostic rewrote signed legacy observation")
	}
}

func TestTerminalDiagnosticEpochFieldsDoNotWaiveIncompleteCurrentCoverage(t *testing.T) {
	e := scenarioClaimWindowTestEvaluation()
	for index := range e.Current.Claims {
		e.Current.Claims[index].LastDiscovered = 100
		e.Current.Claims[index].EpochOutcomes = []ClaimEpochObservation{{Epoch: 100, Status: "finalized"}}
	}
	e.Current.Operators = []OperatorObservation{{NoID: 1, PayoutTierArtifacts: []OperatorPayoutTierArtifactObservation{{Epoch: 100}}}, {NoID: 2, PayoutTierArtifacts: []OperatorPayoutTierArtifactObservation{{Epoch: 100}}}}
	collector := &terminalDiagnosticCollector{ctx: t.Context(), report: &terminalDiagnosticReport{ReadOnly: true}}
	collectTerminalDiagnosticEpochObservationFields(collector, e.Current, "")
	if collector.report.Checks[0].Status != "pass" || collector.report.FinalAcceptance {
		t.Fatal("available fields were not distinguished from acceptance")
	}
	if _, err := scenarioClaimsForAcceptance(e); err == nil || !strings.Contains(err.Error(), "last=100") {
		t.Fatalf("actual incomplete current discovery was waived: %v", err)
	}
}
