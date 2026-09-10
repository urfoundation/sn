// Exact descendant declarations preserve top-level binary selection and prove
// every observed child belongs to a live, fully declared parent hierarchy.
package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Physical private metadata files exercise the real admission path.
func descendantInputFilesTest(t *testing.T, outcomes, markers string) (string, string) {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outcomePath, markerPath := filepath.Join(directory, "outcomes.tsv"), filepath.Join(directory, "literals.tsv")
	testWrite(t, outcomePath, outcomes)
	testWrite(t, markerPath, markers)
	return outcomePath, markerPath
}

// The five child names are derived from the source-declared names in
// server/controller/connect_controller_test.go, not from an observed pass set.
func TestQualificationDescendantsAdmitExactControllerCensus(t *testing.T) {
	parent := "TestContractResultErrorSeparatesReliabilityFromAccountFailures"
	children := []string{"missing_companion_origin", "inactive_destination_at_write_boundary", "inactive_source", "insufficient_balance", "unknown_legacy_failure"}
	roots := []string{parent}
	for index := 1; index < 20; index++ {
		roots = append(roots, fmt.Sprintf("TestSyntheticController%02d", index))
	}
	sort.Strings(roots)
	identities := append([]string(nil), roots...)
	for _, child := range children {
		name := parent + "/" + child
		if rootPattern.MatchString(name) {
			t.Fatal("original top-level-only refusal was not reproduced")
		}
		identities = append(identities, name)
	}
	sort.Strings(identities)
	var rows []string
	for _, name := range identities {
		rows = append(rows, name+"\tPASS")
	}
	outcomes, markers := descendantInputFilesTest(t, strings.Join(rows, "\n")+"\n", "")
	expected, err := expectedInputs(outcomes, markers)
	if err != nil {
		t.Fatalf("declared descendant admission: %v", err)
	}
	if !reflect.DeepEqual(expected.Roots, roots) || len(expected.Outcomes) != 25 {
		t.Fatalf("binary root selection acquired descendants: %+v", expected)
	}
	events := []map[string]any{testEvent("start", "", "")}
	for _, root := range roots {
		events = append(events, testEvent("run", root, ""))
		if root == parent {
			for _, child := range children {
				name := parent + "/" + child
				events = append(events, testEvent("run", name, ""), testEvent("pass", name, ""))
			}
		}
		events = append(events, testEvent("pass", root, ""))
	}
	events = append(events, testEvent("pass", "", ""))
	raw := testEncodedEvents(t, events)
	before := bytes.Clone(raw)
	result, err := verifyEvents(bytes.NewReader(raw), expected, "example.com/validator", 0)
	if err != nil || result.Roots != 20 || result.Passed != 20 || result.Subtests != 5 || result.SubtestsPassed != 5 || result.ExpectedFailures != 0 || result.ExpectedSubtestFailures != 0 || result.Events != len(events) || result.Bytes != len(raw) || !bytes.Equal(before, raw) {
		t.Fatalf("exact root/child event census: %+v %v", result, err)
	}
}

// Every nested ancestor must be declared before descendants. Duplicate,
// foreign, malformed or over-bound identities do not become selectors.
func TestQualificationDescendantsRejectInvalidDeclarations(t *testing.T) {
	for _, rows := range []string{
		"TestTree/child\tPASS\n",
		"TestTree\tPASS\nTestTree/child/leaf\tPASS\n",
		"TestTree\tPASS\nTestTree/child\tPASS\nTestTree/child\tPASS\n",
		"TestTree\tPASS\nTestOther/child\tPASS\n",
		"TestTree\tPASS\nTestTree/\tPASS\n",
		"TestTree\tPASS\nTestTree//child\tPASS\n",
		"TestTree\tPASS\nTestTree/raw space\tPASS\n",
		"TestTree\tPASS\nTestTree/raw\u200bcontrol\tPASS\n",
		"TestTree\tPASS\nTestTree/child\tpass\n",
		"TestTree\tPASS\nTestTree/child\tPASS",
		"TestTree\tPASS\nTestTree/child\tPASS\r\n",
		"TestTree\tPASS\nTestTree/\xff\tPASS\n",
		"TestTree\tPASS\nTestTree/" + strings.Repeat("x", 4096) + "\tPASS\n",
		strings.Repeat("x", metadataLimit+1),
	} {
		outcomes, markers := descendantInputFilesTest(t, rows, "")
		if _, err := expectedInputs(outcomes, markers); err == nil {
			t.Fatalf("invalid declaration admitted: %.120q", rows)
		}
	}
	for _, depth := range []int{64, 65} {
		name, rows := "TestTree", "TestTree\tPASS\n"
		for index := 0; index < depth; index++ {
			name += "/child"
			rows += name + "\tPASS\n"
		}
		outcomes, markers := descendantInputFilesTest(t, rows, "")
		if _, err := expectedInputs(outcomes, markers); (err == nil) != (depth == 64) {
			t.Fatalf("descendant depth=%d: %v", depth, err)
		}
	}
	exact := "TestTree/" + strings.Repeat("x", 4096-len("TestTree/"))
	outcomes, markers := descendantInputFilesTest(t, "TestTree\tPASS\n"+exact+"\tPASS\n", "")
	if _, err := expectedInputs(outcomes, markers); err != nil {
		t.Fatalf("exact descendant byte bound: %v", err)
	}
	longRoot := "Test" + strings.Repeat("x", 4097)
	outcomes, markers = descendantInputFilesTest(t, longRoot+"\tPASS\n", "")
	if expected, err := expectedInputs(outcomes, markers); err != nil || !reflect.DeepEqual(expected.Roots, []string{longRoot}) {
		t.Fatalf("unchanged legacy top-level grammar: %v", err)
	}
}

// One package/mode cannot run the same top-level body twice by assigning each
// invocation a disjoint descendant set. Separate modes retain separate owners.
func TestQualificationDescendantsKeepTopLevelSuiteOwnership(t *testing.T) {
	plan := testPlan(t)
	first, firstMarkers := descendantInputFilesTest(t, "TestTree\tPASS\nTestTree/first\tPASS\n", "")
	second, secondMarkers := descendantInputFilesTest(t, "TestTree\tPASS\nTestTree/second\tPASS\n", "")
	plan.Suites[0].Outcomes, plan.Suites[0].FailureLiterals = first, firstMarkers
	plan.Suites = append(plan.Suites, suiteSpec{Id: "second", Package: plan.Suites[0].Package, Mode: "normal", Outcomes: second, FailureLiterals: secondMarkers})
	if err := validatePlan(plan); err == nil {
		t.Fatal("two descendant selections duplicated one binary root")
	}
	plan.Suites[1].Mode = "race"
	if err := validatePlan(plan); err != nil {
		t.Fatal(err)
	}
}

// A paused ancestor is still live; descendants nevertheless require every
// named ancestor and cannot complete any parent while a child is unfinished.
func descendantTreeTest() (expectedSuite, []map[string]any) {
	expected := expectedSuite{Roots: []string{"TestTree"}, Outcomes: map[string]string{"TestTree": "pass", "TestTree/child": "pass", "TestTree/child/leaf": "pass", "TestTree/peer": "pass"}, Markers: map[string]string{}}
	events := []map[string]any{
		testEvent("start", "", ""), testEvent("run", "TestTree", ""), testEvent("run", "TestTree/child", ""),
		testEvent("pause", "TestTree/child", ""), testEvent("output", "TestTree", "parent remains live\n"),
		testEvent("cont", "TestTree/child", ""), testEvent("run", "TestTree/child/leaf", ""),
		testEvent("pass", "TestTree/child/leaf", ""), testEvent("pass", "TestTree/child", ""),
		testEvent("run", "TestTree/peer", ""), testEvent("pass", "TestTree/peer", ""), testEvent("pass", "TestTree", ""), testEvent("pass", "", ""),
	}
	return expected, events
}

// Each mutation changes a real admitted event sequence, not the verifier's
// expected metadata. Foreign and duplicated identities cannot hide in totals.
func TestQualificationDescendantsRequireCompleteLiveHierarchy(t *testing.T) {
	expected, events := descendantTreeTest()
	if result, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, events)), expected, "example.com/validator", 0); err != nil || result.Roots != 1 || result.Subtests != 3 || result.Passed != 1 || result.SubtestsPassed != 3 {
		t.Fatalf("valid nested hierarchy: %+v %v", result, err)
	}
	for _, fault := range []string{"missing-root-start", "missing-child-start", "missing-leaf", "foreign-child", "duplicate-child", "duplicate-terminal", "parent-before-child", "root-before-children", "output-after-parent", "missing-terminal", "skip-child", "wrong-child-outcome"} {
		expected, events := descendantTreeTest()
		switch fault {
		case "missing-root-start":
			events = append(events[:1], events[2:]...)
		case "missing-child-start":
			events[2], events[3], events[5] = testEvent("output", "TestTree", ""), testEvent("output", "TestTree", ""), testEvent("output", "TestTree", "")
		case "missing-leaf":
			events[6], events[7] = testEvent("output", "TestTree", ""), testEvent("output", "TestTree", "")
		case "foreign-child":
			events[6] = testEvent("run", "TestTree/child/foreign", "")
		case "duplicate-child":
			events[4] = testEvent("run", "TestTree/child", "")
		case "duplicate-terminal":
			events[9] = testEvent("pass", "TestTree/child", "")
		case "parent-before-child":
			events[7] = testEvent("pass", "TestTree/child", "")
		case "root-before-children":
			events[4] = testEvent("pass", "TestTree", "")
		case "output-after-parent":
			events = append(events[:12], testEvent("output", "TestTree/peer", ""), events[12])
		case "missing-terminal":
			events = events[:len(events)-1]
		case "skip-child":
			events[7] = testEvent("skip", "TestTree/child/leaf", "")
		case "wrong-child-outcome":
			events[7] = testEvent("fail", "TestTree/child/leaf", "")
		}
		_, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, events)), expected, "example.com/validator", 0)
		if err == nil {
			t.Fatalf("admitted %s", fault)
		}
		if (fault == "missing-root-start" || fault == "missing-child-start" || fault == "output-after-parent") && !strings.Contains(err.Error(), "inactive ancestor") {
			t.Fatalf("%s bypassed live ancestor admission: %v", fault, err)
		}
	}
}

// An ancestor's failure summary and a child's causal assertion are independent
// identities. No package, sibling, parent or child can lend the other's literal.
func TestQualificationDescendantsKeepFailureLiteralAttribution(t *testing.T) {
	expected := expectedSuite{Roots: []string{"TestTree"}, Outcomes: map[string]string{"TestTree": "fail", "TestTree/child": "fail"}, Markers: map[string]string{"TestTree": "parent propagated failure", "TestTree/child": "child causal assertion"}}
	makeEvents := func() []map[string]any {
		return []map[string]any{testEvent("start", "", ""), testEvent("run", "TestTree", ""), testEvent("run", "TestTree/child", ""), testEvent("output", "TestTree/child", "child causal "), testEvent("output", "TestTree/child", "assertion\n"), testEvent("fail", "TestTree/child", ""), testEvent("output", "TestTree", "parent propagated failure\n"), testEvent("fail", "TestTree", ""), testEvent("fail", "", "")}
	}
	if result, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, makeEvents())), expected, "example.com/validator", 1); err != nil || result.ExpectedFailures != 1 || result.ExpectedSubtestFailures != 1 || result.Passed != 0 || result.SubtestsPassed != 0 {
		t.Fatalf("exact descendant causal ownership: %+v %v", result, err)
	}
	for _, substitute := range []string{"parent", "package", "child", "split", "sibling"} {
		events := makeEvents()
		caseExpected := expectedSuite{Roots: expected.Roots, Outcomes: map[string]string{}, Markers: expected.Markers}
		for name, outcome := range expected.Outcomes {
			caseExpected.Outcomes[name] = outcome
		}
		switch substitute {
		case "parent":
			events[3]["Output"], events[4]["Output"], events[6]["Output"] = "different", "\n", "parent propagated failure child causal assertion\n"
		case "package":
			events[3]["Output"], events[4]["Output"] = "different", "\n"
			events = append(events[:8], testEvent("output", "", "child causal assertion\n"), events[8])
		case "child":
			events[4]["Output"], events[6]["Output"] = "assertion parent propagated failure\n", "different\n"
		case "split":
			events[4]["Output"], events[6]["Output"] = "different\n", "assertion parent propagated failure\n"
		case "sibling":
			caseExpected.Outcomes["TestTree/peer"] = "pass"
			events[3]["Output"], events[4]["Output"] = "different", "\n"
			tail := append([]map[string]any(nil), events[6:]...)
			events = append(events[:6], testEvent("run", "TestTree/peer", ""), testEvent("output", "TestTree/peer", "child causal assertion\n"), testEvent("pass", "TestTree/peer", ""))
			events = append(events, tail...)
		}
		if _, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, events)), caseExpected, "example.com/validator", 1); err == nil {
			t.Fatalf("borrowed a %s failure literal", substitute)
		}
	}
	outcomes, markers := descendantInputFilesTest(t, "TestTree\tPASS\nTestTree/child\tFAIL\n", "TestTree/child\tchild causal assertion\n")
	if _, err := expectedInputs(outcomes, markers); err == nil {
		t.Fatal("failing child inherited a passing parent")
	}
}
