// The typed catalog is the only runtime policy for classified process facts.
// Tests exercise real classification and full terminal evaluation, not a
// persisted permission bit or a second controller-specific class allowlist.
package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// Adding a new producer class without its execution policy must fail here,
// before a long interval discovers a missing controller allowlist entry.
func TestProcessLogRuntimeCatalogRequiresEveryTypedClass(t *testing.T) {
	seen := map[string]bool{}
	for class := processLogClassNone + 1; class < processLogClassCount; class++ {
		definition := class.definition()
		if definition.name == "" || seen[definition.name] || definition.category <= processLogRuntimeInvalid || definition.category > processLogRuntimeCatastrophic {
			t.Fatalf("class %d has an absent, duplicate or invalid policy: %+v", class, definition)
		}
		seen[definition.name] = true
		if processLogRuntimeCategoryForName(definition.name) != definition.category {
			t.Fatalf("wire classifier lost policy for %s", definition.name)
		}
		if processLogFindingsError([]ProcessLogFinding{{Class: definition.name, Blocking: true, Count: 1}}) == nil {
			t.Fatalf("runtime policy waived the final severity of %s", definition.name)
		}
	}
	for _, unknown := range []string{"", "unknown-class", "operational", "tls-handshake-timeout ", "TLS-HANDSHAKE-TIMEOUT"} {
		if processLogRuntimeCategoryForName(unknown) != processLogRuntimeUnknown {
			t.Fatalf("unknown wire name inherited execution authority: %q", unknown)
		}
	}
}

// A batch of known operational outcomes must reach the terminal check in one
// pass, including classes which were missing from the old ad-hoc list.
func TestProcessLogRuntimeOperationalBatchReachesTerminalWithoutWaiver(t *testing.T) {
	t.Parallel()
	cfg, definition, window, observations := newScenarioIntervalFixture(t)
	fixture := newProcessLogGateFixture(t, "", "")
	definition.Checks = []scenarioCheck{{ID: "terminal-only", Check: func(evaluation *scenarioEvaluation) (bool, string) {
		return scenarioAcceptanceIntervalObserved(evaluation.Window, evaluation.Current), "terminal checkpoint observed"
	}}}
	lines := []string{
		"E0801 12:00:00 [api-token]failed to refresh JWT: Timeout.",
		"E0801 12:00:00 seed pick: no seed providers",
		"E0801 12:00:00 exit could not create contract",
		"E0801 12:00:00 completeHandshake failed: context canceled",
		"E0801 12:00:00 h3 connect err = tls: internal error",
		"E0801 12:00:00 failed to sufficiently increase receive buffer size",
	}
	probe := &scenarioIntervalProbe{observations: observations, before: func(_ context.Context, index int) error {
		if index == 4 {
			return os.WriteFile(fixture.stderrPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
		}
		return nil
	}}
	result, err := runScenarioWithProbe(t.Context(), cfg, fixture.dir, definition, probe, scenarioRunOptions{PollInterval: time.Microsecond, Timeout: time.Hour, ProcessLogs: fixture.gate, FaultDriver: &fakeFaultDriver{}})
	if err == nil || result == nil || result.Result != "fail" || result.EndHead.Number != window.TerminalBlock || probe.calls.Load() != int64(len(observations)) {
		t.Fatalf("operational batch stopped before terminal: result=%+v reads=%d error=%v", result, probe.calls.Load(), err)
	}
	for _, assertion := range result.Assertions {
		if assertion.ID == "scenario_context" || assertion.ID == "terminal-only" && !assertion.Passed {
			t.Fatalf("known operational finding interrupted terminal evaluation: %+v", assertion)
		}
	}
	scan, err := fixture.gate.Scan(true)
	if err != nil || len(blockingProcessLogFindings(scan.Findings)) != len(lines) || fixture.gate.RequireClean(true) == nil {
		t.Fatalf("operational classification changed final severity: %+v error=%v", scan, err)
	}
	for _, finding := range scan.Findings {
		if finding.Disposition != "unexplained" || len(finding.FaultIDs) != 0 || finding.Count != 1 || finding.FirstLineSHA256 == "" {
			t.Fatalf("operational evidence was relabeled or dropped: %+v", finding)
		}
	}
}

// Availability can be deferred only as its classified outcome. It cannot
// authorize unknown warnings, a rejected public signature or an evidence gap.
func TestProcessLogRuntimeIntegrityAndUnknownRemainTerminal(t *testing.T) {
	t.Parallel()
	cfg, _, _, _ := newScenarioIntervalFixture(t)
	known := processLogFindingsError([]ProcessLogFinding{{Blocking: true, Class: "api-token-refresh-timeout", Count: 1}})
	if !scenarioProcessLogFailureDeferred(cfg, "release-1.0", known) {
		t.Fatal("typed availability outcome was not eligible")
	}
	for _, line := range []string{
		"panic: tls handshake timeout",
		"fatal error: service unavailable",
		"E0801 12:00:00 unknown transport timeout",
		"W0801 12:00:00 unknown retry warning",
		"E0801 12:00:00 invalid byte sequence for encoding UTF8: 0x00",
		"E0801 12:00:00 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000000) exit contract verification failed (Public)",
		"E0801 12:00:00 transfer.go:1] [r]receiver<-sender s(00000000-0000-0000-0000-000000000001) exit contract verification failed (Network)",
	} {
		classification, matched := classifyProcessLogLine([]byte(line))
		failure := processLogFindingsError([]ProcessLogFinding{{Blocking: true, Class: classification.class.definition().name, Count: 1}})
		if !matched || scenarioProcessLogFailureDeferred(cfg, "release-1.0", errors.Join(known, failure)) {
			t.Fatalf("operational outcome masked a hard class: %s (%+v)", line, classification)
		}
	}
	for _, class := range []processLogClass{processLogClassLogIntegrity, processLogClassLogOverrun, processLogClassArtifactFailure} {
		failure := processLogFindingsError([]ProcessLogFinding{{Blocking: true, Class: class.definition().name, Count: 1}})
		if scenarioProcessLogFailureDeferred(cfg, "release-1.0", errors.Join(known, failure)) {
			t.Fatalf("operational outcome masked evidence integrity: %v", failure)
		}
	}
	if scenarioProcessLogFailureDeferred(cfg, "release-1.0", errors.Join(known, errors.New("synthetic evidence write failed"))) {
		t.Fatal("operational outcome masked a persistence failure")
	}
}
