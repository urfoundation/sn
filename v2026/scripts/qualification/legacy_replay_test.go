// Retained events survive correction of source-declared legacy descendants.
package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// A root-only census can omit legacy t.Run cases even when the body passed.
// Correct only the source-declared expectations and replay the retained bytes;
// never turn an unknown descendant into an implicit acceptance or rerun a body.
func TestQualificationShardsReplayOmittedLegacyDescendantsWithoutChangingBody(t *testing.T) {
	plan := testPlan(t)
	plan.Suites[0].RootsPerShard = 1
	testWrite(t, plan.Suites[0].Outcomes, "TestLegacy\tPASS\n")
	initial, _, err := partitionSuites(plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	events := []map[string]any{testEvent("start", "", ""), testEvent("run", "TestLegacy", ""), testEvent("run", "TestLegacy/retained-case", ""), testEvent("pass", "TestLegacy/retained-case", ""), testEvent("pass", "TestLegacy", ""), testEvent("output", "", "PASS\n"), testEvent("pass", "", "")}
	for _, event := range events {
		event["Package"] = plan.Packages[0].ImportPath
	}
	capture := t.TempDir()
	eventPath, exitPath := filepath.Join(capture, "original.events"), filepath.Join(capture, "original.exit")
	testWrite(t, eventPath, string(testEncodedEvents(t, events)))
	testWrite(t, exitPath, "0\n")
	proof, err := regularProof(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	exitProof, err := regularProof(exitPath)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{eventPath, initial.Suites[0].Outcomes, initial.Suites[0].FailureLiterals, plan.Packages[0].ImportPath, exitPath}
	if _, err := replayEventsFiles(args); err == nil || !strings.Contains(err.Error(), "unexpected root or subtest TestLegacy/retained-case") {
		t.Fatalf("omitted legacy descendant was silently accepted: %v", err)
	}
	// This is a separate corrected declaration. Original metadata and body
	// evidence stay intact, including the earlier checker's honest refusal.
	plan.Suites[0].Outcomes = filepath.Join(capture, "corrected.outcomes.tsv")
	testWrite(t, plan.Suites[0].Outcomes, "TestLegacy\tPASS\nTestLegacy/retained-case\tPASS\n")
	corrected, _, err := partitionSuites(plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	args[1], args[2] = corrected.Suites[0].Outcomes, corrected.Suites[0].FailureLiterals
	result, err := replayEventsFiles(args)
	if err != nil || result.Status != "matched" || result.Verification == nil || result.Verification.Roots != 1 || result.Verification.Subtests != 1 || result.Verification.Passed != 1 || result.Verification.SubtestsPassed != 1 {
		t.Fatalf("corrected source-declared descendants did not replay: %+v %v", result, err)
	}
	if err := checkProofs(map[string]fileProof{eventPath: proof, exitPath: exitProof}); err != nil {
		t.Fatalf("checker repair changed original body evidence: %v", err)
	}
	if _, err := replayEventsFiles([]string{eventPath, initial.Suites[0].Outcomes, initial.Suites[0].FailureLiterals, plan.Packages[0].ImportPath, exitPath}); err == nil {
		t.Fatal("corrected replay overwrote the original refusal's metadata")
	}
}
