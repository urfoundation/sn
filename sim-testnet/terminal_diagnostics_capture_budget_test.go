//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestTerminalDiagnosticCaptureBudgetAllowsCompleteTapeWithinOwner(t *testing.T) {
	collector := &terminalDiagnosticCollector{ctx: t.Context(), report: &terminalDiagnosticReport{ReadOnly: true}}
	collector.check("prior-integrity", "", time.Minute, func(context.Context) (any, error) { return nil, errors.New("original failure") })
	started := time.Now()
	collector.check("validator-1/signed-source-capture", "", terminalDiagnosticValidatorCaptureTimeout, func(ctx context.Context) (any, error) {
		deadline, bounded := ctx.Deadline()
		if !bounded || deadline.Before(started.Add(59*time.Minute)) || deadline.After(time.Now().Add(time.Hour)) {
			t.Fatalf("complete tape retained the old short allowance or became unbounded: %s", deadline)
		}
		return "complete original tape", nil
	})
	if len(collector.report.Checks) != 2 || collector.report.Checks[0].Status != "fail" || collector.report.Checks[1].Status != "pass" || collector.report.FinalAcceptance {
		t.Fatal("diagnostic duration changed strict acceptance or original failure")
	}
	ownerDeadline := time.Now().Add(time.Minute)
	owner, cancel := context.WithDeadline(t.Context(), ownerDeadline)
	defer cancel()
	collector.ctx = owner
	collector.check("validator-2/signed-source-capture", "", terminalDiagnosticValidatorCaptureTimeout, func(ctx context.Context) (any, error) {
		deadline, bounded := ctx.Deadline()
		if !bounded || deadline != ownerDeadline {
			t.Fatal("capture extended its owner's remaining allowance")
		}
		return nil, nil
	})
	cancel()
	collector.check("canceled-capture", "", terminalDiagnosticValidatorCaptureTimeout, func(context.Context) (any, error) { t.Fatal("canceled owner entered capture"); return nil, nil })
	if collector.report.Checks[3].Status != "unavailable" {
		t.Fatal("owner cancellation was waived")
	}
}

// A timed-out source remains failed even if its reader returns partial evidence
// with nil; later independent checks retain the live parent and still execute.
func TestTerminalDiagnosticCaptureTimeoutKeepsLaterChecks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		collector := &terminalDiagnosticCollector{ctx: t.Context(), report: &terminalDiagnosticReport{ReadOnly: true}}
		passed := collector.check("validator-1/signed-source-capture", "", terminalDiagnosticValidatorCaptureTimeout, func(ctx context.Context) (any, error) {
			<-ctx.Done()
			return "retained authenticated prefix", nil
		})
		continued := collector.check("independent-terminal-check", "", time.Minute, func(ctx context.Context) (any, error) {
			return "independent terminal evidence", ctx.Err()
		})
		if passed || !continued || len(collector.report.Checks) != 2 || collector.report.Checks[0].Status != "fail" || collector.report.Checks[0].Evidence != "retained authenticated prefix" || collector.report.Checks[1].Status != "pass" || collector.report.FinalAcceptance {
			t.Fatalf("capture timeout changed later evidence or acquired acceptance: %+v", collector.report.Checks)
		}
	})
}
