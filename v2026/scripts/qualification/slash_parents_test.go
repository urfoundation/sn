// Actual Go test subprocesses distinguish a slash inside one child name from
// separately executed parents. Expected identities come only from this source.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

const slashFixtureEnvironment = "QUALIFICATION_SYNTHETIC_SLASH_FIXTURE"
const slashFixtureRoot = "TestQualificationSlashParentsActualGoEvents"

// Commands are finite, capped and joined. Cancellation kills the owned process
// group, including compiler descendants if Go resolves a cache-built converter.
func slashCommandTest(t *testing.T, arguments []string, environment []string, input []byte, wantedExit int) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, arguments[0], arguments[1:]...)
	command.Env = append(os.Environ(), environment...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 5 * time.Second
	command.Stdin = bytes.NewReader(input)
	stdout, stderr := &boundedOutput{Maximum: metadataLimit}, &boundedOutput{Maximum: metadataLimit}
	command.Stdout, command.Stderr = stdout, stderr
	err := command.Run()
	if ctx.Err() != nil || command.ProcessState == nil || command.ProcessState.ExitCode() != wantedExit || (err != nil) != (wantedExit != 0) || stderr.buffer.Len() != 0 {
		t.Fatalf("owned slash fixture command did not complete with exit %d: %v %q", wantedExit, err, stderr.Bytes())
	}
	return bytes.Clone(stdout.Bytes())
}

// Resolve and retain the actual converter, then collect this test executable's
// real verbose bytes. No event or outcome is constructed from the observed set.
func slashGoEventsTest(t *testing.T, failing bool) (expectedSuite, []byte, []map[string]any, int) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	mode, actualExit := "pass", 0
	expected := expectedSuite{Roots: []string{slashFixtureRoot}, Outcomes: map[string]string{slashFixtureRoot: "pass"}, Markers: map[string]string{}}
	for _, child := range []string{"rtt_500ms/preferred_true/legacy_false", "genuine/parent", "genuine/parent/leaf/literal", "genuine/parent/peer/paused", "ordinary"} {
		expected.Outcomes[slashFixtureRoot+"/"+child] = "pass"
	}
	if failing {
		mode, actualExit = "fail", 1
		parent, leaf := slashFixtureRoot+"/parent/with/slash", slashFixtureRoot+"/parent/with/slash/child/with/slash"
		expected.Outcomes = map[string]string{slashFixtureRoot: "fail", parent: "fail", leaf: "fail"}
		expected.Markers = map[string]string{slashFixtureRoot: "root propagated slash failure", parent: "parent propagated slash failure", leaf: "actual slash child assertion"}
	}
	raw := slashCommandTest(t, []string{binary, "-test.run=^" + slashFixtureRoot + "$", "-test.v=test2json", "-test.count=1", "-test.parallel=2", "-test.timeout=20s"}, []string{slashFixtureEnvironment + "=" + mode}, nil, actualExit)
	resolved := slashCommandTest(t, []string{filepath.Join(runtime.GOROOT(), "bin", "go"), "tool", "-n", "test2json"}, []string{"GOTOOLCHAIN=local", "GOWORK=off"}, nil, 0)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resolutionPath := filepath.Join(directory, "converter-path.txt")
	testWrite(t, resolutionPath, string(resolved))
	converter, err := retainResolvedGoTool(resolutionPath, filepath.Join(directory, "test2json"))
	if err != nil {
		t.Fatal(err)
	}
	encoded := slashCommandTest(t, []string{converter.Path, "-p", "example.com/validator", "-t"}, nil, raw, 0)
	var events []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	for {
		var event map[string]any
		if err := decoder.Decode(&event); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return expected, encoded, events, actualExit
}

// t.Run is intentional: the behavior under test is Go's actual descendant
// event boundary, captured in an owned child rather than the outer test stream.
func TestQualificationSlashParentsActualGoEvents(t *testing.T) {
	if mode := os.Getenv(slashFixtureEnvironment); mode != "" {
		switch mode {
		case "pass":
			t.Run("rtt_500ms/preferred_true/legacy_false", func(t *testing.T) { t.Log("literal slash child") })
			t.Run("genuine/parent", func(t *testing.T) {
				t.Run("leaf/literal", func(t *testing.T) { t.Log("nested slash child") })
				t.Run("peer/paused", func(t *testing.T) { t.Parallel() })
			})
			t.Run("ordinary", func(t *testing.T) { t.Log("ordinary child") })
		case "fail":
			t.Run("parent/with/slash", func(t *testing.T) {
				t.Run("child/with/slash", func(t *testing.T) { t.Error("actual slash child assertion") })
				t.Log("parent propagated slash failure")
			})
			t.Log("root propagated slash failure")
		default:
			t.Fatal("unknown synthetic slash fixture mode")
		}
		return
	}
	expected, encoded, events, actualExit := slashGoEventsTest(t, false)
	args := replayInputsTest(t, expected, events, actualExit)
	// Replay receives the converter's original bytes, not reserialized events.
	testWrite(t, args[0], string(encoded))
	admitted, err := expectedInputs(args[1], args[2])
	if err != nil || !reflect.DeepEqual(admitted, expected) {
		t.Fatalf("literal slash source declarations were refused: %v", err)
	}
	parents, err := expectedParents(admitted)
	if err != nil || parents[slashFixtureRoot+"/rtt_500ms/preferred_true/legacy_false"] != slashFixtureRoot || parents[slashFixtureRoot+"/genuine/parent/leaf/literal"] != slashFixtureRoot+"/genuine/parent" {
		t.Fatalf("literal slash declaration selected the wrong real parent: %+v %v", parents, err)
	}
	paused, continued := false, false
	for _, event := range events {
		paused = paused || event["Action"] == "pause"
		continued = continued || event["Action"] == "cont"
		for _, implicit := range []string{"/rtt_500ms", "/rtt_500ms/preferred_true", "/genuine", "/genuine/parent/leaf"} {
			if event["Test"] == slashFixtureRoot+implicit {
				t.Fatal("Go emitted an assumed intermediate test identity")
			}
		}
	}
	if !paused || !continued {
		t.Fatal("actual Go fixture did not exercise the parallel lifecycle")
	}
	before := map[string]fileProof{}
	for _, index := range []int{0, 1, 2, 4} {
		proof, err := regularProof(args[index])
		if err != nil {
			t.Fatal(err)
		}
		before[args[index]] = proof
	}
	// No executable can be found once actual generation is done. Replay must
	// consume only retained bytes and preserve all four input owners.
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	status := runReplay(args, &stdout, &stderr)
	var result replayResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || status != 0 || stderr.Len() != 0 || result.Status != "matched" || result.Verification == nil {
		t.Fatalf("actual literal slash events were refused: %d %+v %v %q", status, result, err, stderr.String())
	}
	checked := result.Verification
	if checked.Roots != 1 || checked.Passed != 1 || checked.Subtests != 5 || checked.SubtestsPassed != 5 || checked.BinaryExit != 0 || checked.Events != len(events) || checked.Bytes != len(encoded) {
		t.Fatalf("actual literal slash census differs: %+v", checked)
	}
	for path, proof := range before {
		after, err := regularProof(path)
		if err != nil || after != proof {
			t.Fatalf("slash replay changed retained input: %s %v", path, err)
		}
	}
}

// Events cannot fill omitted declarations or bypass a true declared parent.
// Mutations retain the actual source census except the explicit orphan case.
func TestQualificationSlashParentsRejectUnknownAndMisorderedParents(t *testing.T) {
	expected, encoded, _, actualExit := slashGoEventsTest(t, false)
	parent, leaf := slashFixtureRoot+"/genuine/parent", slashFixtureRoot+"/genuine/parent/leaf/literal"
	if _, err := verifyEvents(bytes.NewReader(encoded), expected, "example.com/validator", actualExit); err != nil {
		t.Fatalf("actual slash parent baseline refused: %v", err)
	}
	for _, fault := range []string{"unknown-parent", "missing-parent-start", "late-parent-start", "parent-first-terminal", "unknown-literal-part", "missing-child-terminal", "duplicate-child", "late-child"} {
		caseExpected := expectedSuite{Roots: expected.Roots, Outcomes: map[string]string{}, Markers: expected.Markers}
		for name, outcome := range expected.Outcomes {
			caseExpected.Outcomes[name] = outcome
		}
		if fault == "unknown-parent" {
			delete(caseExpected.Outcomes, parent)
		}
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		var changed []map[string]any
		var delayedParentEvents []map[string]any
		mutated := false
		for {
			var event map[string]any
			if err := decoder.Decode(&event); err == io.EOF {
				break
			} else if err != nil {
				t.Fatal(err)
			}
			if !mutated {
				switch {
				case fault == "late-parent-start" && event["Test"] == parent:
					delayedParentEvents = append(delayedParentEvents, event)
					continue
				case fault == "late-parent-start" && event["Action"] == "pass" && event["Test"] == leaf:
					changed = append(changed, event)
					changed = append(changed, delayedParentEvents...)
					mutated = true
					continue
				case fault == "missing-parent-start" && event["Action"] == "run" && event["Test"] == parent:
					mutated = true
					continue
				case fault == "parent-first-terminal" && event["Action"] == "run" && event["Test"] == leaf:
					changed = append(changed, testEvent("pass", parent, ""))
					mutated = true
				case fault == "unknown-literal-part" && event["Action"] == "run" && event["Test"] == leaf:
					event["Test"] = parent + "/leaf"
					mutated = true
				case fault == "missing-child-terminal" && event["Action"] == "pass" && event["Test"] == leaf:
					mutated = true
					continue
				case fault == "duplicate-child" && event["Action"] == "run" && event["Test"] == leaf:
					changed = append(changed, testEvent("run", leaf, ""))
					mutated = true
				case fault == "late-child" && event["Action"] == "pass" && event["Test"] == slashFixtureRoot:
					changed = append(changed, event, testEvent("run", leaf, ""))
					mutated = true
					continue
				}
			}
			changed = append(changed, event)
		}
		if fault != "unknown-parent" && !mutated {
			t.Fatalf("slash parent mutation did not reach actual event: %s", fault)
		}
		_, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, changed)), caseExpected, "example.com/validator", actualExit)
		if err == nil {
			t.Fatalf("literal slash events bypassed true parent custody: %s", fault)
		}
		if fault == "late-parent-start" && !strings.Contains(err.Error(), "inactive ancestor") {
			t.Fatalf("late slash parent bypassed the actual ancestor check: %v", err)
		}
	}
}

// Original failed child and ancestor assertions retain separate literal and
// exit owners even when multiple slash components belong to one test name.
func TestQualificationSlashParentsKeepOriginalFailureOwnership(t *testing.T) {
	expected, encoded, _, actualExit := slashGoEventsTest(t, true)
	checked, err := verifyEvents(bytes.NewReader(encoded), expected, "example.com/validator", actualExit)
	if err != nil || checked.Roots != 1 || checked.ExpectedFailures != 1 || checked.Subtests != 2 || checked.ExpectedSubtestFailures != 2 || checked.Passed != 0 || checked.SubtestsPassed != 0 {
		t.Fatalf("actual slash failure lost exact ownership: %+v %v", checked, err)
	}
	parent, leaf := slashFixtureRoot+"/parent/with/slash", slashFixtureRoot+"/parent/with/slash/child/with/slash"
	for _, fault := range []string{"missing-parent-literal", "borrowed-child-literal", "passing-parent", "wrong-binary-exit"} {
		caseExpected := expectedSuite{Roots: expected.Roots, Outcomes: map[string]string{}, Markers: map[string]string{}}
		for name, outcome := range expected.Outcomes {
			caseExpected.Outcomes[name] = outcome
		}
		for name, literal := range expected.Markers {
			caseExpected.Markers[name] = literal
		}
		caseExit := actualExit
		switch fault {
		case "missing-parent-literal":
			delete(caseExpected.Markers, parent)
		case "borrowed-child-literal":
			caseExpected.Markers[leaf] = "root propagated slash failure"
		case "passing-parent":
			caseExpected.Outcomes[parent] = "pass"
			delete(caseExpected.Markers, parent)
		case "wrong-binary-exit":
			caseExit = 0
		}
		if _, err := verifyEvents(bytes.NewReader(encoded), caseExpected, "example.com/validator", caseExit); err == nil {
			t.Fatalf("literal slash failure borrowed authority: %s", fault)
		}
	}
}

// The finite declaration grammar keeps the actual root together with all
// exact descendants, including a single child at the existing depth limit.
func TestQualificationSlashParentsKeepBoundedDeclarationAndShardOwnership(t *testing.T) {
	plan := testPlan(t)
	first, parent, leaf := "TestFirst", "TestFirst/parent/literal", "TestFirst/parent/literal/child/literal"
	deep := "TestSecond" + strings.Repeat("/part", 64)
	rows := first + "\tFAIL\n" + parent + "\tFAIL\n" + leaf + "\tFAIL\nTestSecond\tPASS\n" + deep + "\tPASS\n"
	testWrite(t, plan.Suites[0].Outcomes, rows)
	testWrite(t, plan.Suites[0].FailureLiterals, first+"\troot assertion\n"+parent+"\tparent assertion\n"+leaf+"\tchild assertion\n")
	plan.Suites[0].RootsPerShard = 1
	expanded, partitions, err := partitionSuites(plan, t.TempDir())
	if err != nil || len(expanded.Suites) != 2 || len(partitions) != 1 || len(partitions[0].Roots) != 2 {
		t.Fatalf("literal slash roots did not retain shard ownership: %+v %v", partitions, err)
	}
	for index, suite := range expanded.Suites {
		expected, err := expectedInputs(suite.Outcomes, suite.FailureLiterals)
		if err != nil || len(expected.Roots) != 1 {
			t.Fatalf("slash shard admission: %+v %v", expected, err)
		}
		parents, err := expectedParents(expected)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			if len(expected.Outcomes) != 3 || len(expected.Markers) != 3 || parents[parent] != first || parents[leaf] != parent {
				t.Fatalf("slash shard changed exact failure owners: %+v %+v", expected, parents)
			}
		} else if len(expected.Outcomes) != 2 || len(expected.Markers) != 0 || parents[deep] != "TestSecond" {
			t.Fatalf("deep literal child changed root ownership: %+v %+v", expected, parents)
		}
	}
	for _, name := range []string{deep + "/extra", "TestOrphan/parent/literal", "TestSecond//part", "TestSecond/part/", "TestSecond/" + strings.Repeat("x", 4096)} {
		outcomes, literals := descendantInputFilesTest(t, "TestSecond\tPASS\n"+name+"\tPASS\n", "")
		if _, err := expectedInputs(outcomes, literals); err == nil {
			t.Fatalf("slash declaration escaped existing bounds or root owner: %s", name)
		}
	}
}
