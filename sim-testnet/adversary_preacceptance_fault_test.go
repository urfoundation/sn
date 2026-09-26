// Preparation filters must be visible to valid walks before the fault driver
// installs them, including the lifecycle path before any acceptance boundary.
package main

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

// Keep scenario lifecycle evidence while recording exact fault publication.
type preAcceptanceAdversaryFixture struct {
	scenarioAdversaryStub
	window *adversaryFaultWindow
}

// An already armed filter remains installed before its first accepted trigger
// block; the first heartbeat must retain that scope rather than age it out.
func TestAdversaryPreAcceptanceFaultSurvivesFirstHeartbeat(t *testing.T) {
	specs := []scenarioFaultSpec{
		{ID: "prune-target", Kind: "validator-view-filter", Targets: []string{lifecycleValidatorViewFaultTarget(1)}, PreAcceptance: true, TriggerOffsetBlocks: 1, DurationBlocks: 10},
		{ID: "later-api", Kind: "process-restart", Targets: []string{"operator-2-api"}, TriggerOffsetBlocks: 2, DurationBlocks: 1},
	}
	records, err := initializeFaultRecords(100, specs)
	if err != nil {
		t.Fatal(err)
	}
	if targets := scenarioFaultTargets(records, 100, true); len(targets) != 0 {
		t.Fatalf("unarmed preparation fault changed selection: %v", targets)
	}
	records[0].ArmedBlock = 99
	if targets := scenarioFaultTargets(records, 100, true); len(targets) != 0 {
		t.Fatalf("incomplete arming boundary changed selection: %v", targets)
	}
	records[0].ArmedBlockHash = "0x" + strings.Repeat("ab", 32)
	want := specs[0].Targets
	for _, includeDue := range []bool{false, true} {
		if targets := scenarioFaultTargets(records, 100, includeDue); !slices.Equal(targets, want) {
			t.Fatalf("armed fault disappeared before accepted trigger: include_due=%t targets=%v want=%v", includeDue, targets, want)
		}
	}
	records[0].Status = "restored"
	if targets := scenarioFaultTargets(records, 100, false); len(targets) != 0 {
		t.Fatalf("restored preparation fault changed selection: %v", targets)
	}
}

// Route availability uses the same window as a live adversarial actor.
func (self *preAcceptanceAdversaryFixture) SetExpectedFaultTargets(targets []string) {
	self.window.Update(targets)
}

// Observe the real driver's entry before the filter becomes active.
type preAcceptanceAdversaryFaultDriver struct {
	preAcceptanceOrderingFaultDriver
	window             *adversaryFaultWindow
	targetPublished    bool
	unrelatedPublished bool
}

// Fault intent must precede installation; later publication is too late for
// a concurrent signed walk to avoid a deliberately unavailable seed.
func (self *preAcceptanceAdversaryFaultDriver) Apply(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	self.targetPublished = self.window.Expected(spec.Targets[0])
	self.unrelatedPublished = self.window.Expected("operator-2-api")
	return self.preAcceptanceOrderingFaultDriver.Apply(ctx, spec)
}

// Reproduce arming during preparation, when no heartbeat fault window exists.
func TestAdversaryPreAcceptanceFaultPublishedBeforeLifecycleApply(t *testing.T) {
	cfg := testResolvedConfig(t)
	window := newAdversaryFaultWindow(time.Second)
	campaign := &preAcceptanceAdversaryFixture{scenarioAdversaryStub: scenarioAdversaryStub{evidence: healthyAdversaryEvidence()}, window: window}
	driver := &preAcceptanceAdversaryFaultDriver{window: window}
	lifecycle := &preAcceptanceOrderingLifecycle{driver: &driver.preAcceptanceOrderingFaultDriver}
	definition := scenarioDefinition{
		Name: "unit-pre-acceptance-adversary", AdversarialMatrixHash: campaign.evidence.MatrixHash,
		Checks: []scenarioCheck{{ID: "unused", Check: func(*scenarioEvaluation) (bool, string) { return true, "" }}},
		Faults: []scenarioFaultSpec{
			{ID: "prune-target", Kind: "validator-view-filter", Targets: []string{lifecycleValidatorViewFaultTarget(1)}, PreAcceptance: true, TriggerOffsetBlocks: 1, DurationBlocks: 1},
			{ID: "later-api", Kind: "process-restart", Targets: []string{"operator-2-api"}, TriggerOffsetBlocks: 2, DurationBlocks: 1},
		},
	}
	result, err := runScenarioWithProbe(t.Context(), cfg, t.TempDir(), definition,
		&staticScenarioProbe{observations: []*ScenarioObservation{testScenarioObservation(cfg, 1)}},
		scenarioRunOptions{Adversaries: campaign, FaultDriver: driver, FleetLifecycle: lifecycle},
	)
	if err == nil || result == nil || !strings.Contains(err.Error(), "stop after lifecycle ordering probe") {
		t.Fatalf("ordering probe did not reach lifecycle: result=%+v err=%v", result, err)
	}
	if !driver.targetPublished || driver.unrelatedPublished || !lifecycle.armedAtBegin {
		t.Fatalf("preparation fault window was absent or overbroad: target=%t unrelated=%t armed=%t", driver.targetPublished, driver.unrelatedPublished, lifecycle.armedAtBegin)
	}
}
