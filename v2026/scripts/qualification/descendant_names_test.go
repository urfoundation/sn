// Literal Go descendant names keep exact declaration, ancestry and replay
// ownership without becoming regular-expression selectors or filesystem paths.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Explicit synthetic children produce a complete one-parent event fixture.
func descendantNamedEventsTest(children []string) (expectedSuite, []map[string]any) {
	root := "TestSyntheticNames"
	expected := expectedSuite{Roots: []string{root}, Outcomes: map[string]string{root: "pass"}, Markers: map[string]string{}}
	events := []map[string]any{testEvent("start", "", ""), testEvent("run", root, "")}
	for _, child := range children {
		name := root + "/" + child
		expected.Outcomes[name] = "pass"
		events = append(events, testEvent("run", name, ""), testEvent("pass", name, ""))
	}
	events = append(events, testEvent("pass", root, ""), testEvent("pass", "", ""))
	return expected, events
}

// These four literals are source-declared in sim-testnet/evm_test.go and
// plan_revision_test.go. Go rewrites their spaces, not their hyphens.
func TestQualificationDescendantNamesAdmitSourceDeclaredHyphens(t *testing.T) {
	declarations := []struct {
		root     string
		children []string
	}{
		{
			root:     "TestEVMFundingDeltaAccountsForRuntimeExistentialDeposit",
			children: []string{"deposit-only_mirror", "failed-run_incident_state", "sub-rao_EVM_remainder"},
		},
		{
			root:     "TestPlanRevisionNeverReconcilesVerifiedOrNonAlphaTransactions",
			children: []string{"non-alpha"},
		},
	}
	expected := expectedSuite{Outcomes: map[string]string{}, Markers: map[string]string{}}
	events := []map[string]any{testEvent("start", "", "")}
	for _, declaration := range declarations {
		expected.Roots = append(expected.Roots, declaration.root)
		expected.Outcomes[declaration.root] = "pass"
		events = append(events, testEvent("run", declaration.root, ""))
		for _, child := range declaration.children {
			name := declaration.root + "/" + child
			expected.Outcomes[name] = "pass"
			events = append(events, testEvent("run", name, ""), testEvent("pass", name, ""))
		}
		events = append(events, testEvent("pass", declaration.root, ""))
	}
	events = append(events, testEvent("pass", "", ""))
	sort.Strings(expected.Roots)
	args := replayInputsTest(t, expected, events, 0)
	admitted, err := expectedInputs(args[1], args[2])
	if err != nil {
		t.Fatalf("source-declared hyphen admission: %v", err)
	}
	if !reflect.DeepEqual(admitted, expected) || len(admitted.Roots) != 2 || len(admitted.Outcomes) != 6 {
		t.Fatalf("source-declared membership changed: %+v", admitted)
	}
	result, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, events)), admitted, args[3], 0)
	if err != nil || result.Roots != 2 || result.Passed != 2 || result.Subtests != 4 || result.SubtestsPassed != 4 || result.ExpectedFailures != 0 || result.ExpectedSubtestFailures != 0 {
		t.Fatalf("source-declared hyphen event census: %+v %v", result, err)
	}
}

// testing.rewrite retains printable punctuation and Unicode verbatim. Every
// admitted spelling remains an exact key, including regex-like and dot names.
func TestQualificationDescendantNamesPreservePrintableLiteralIdentity(t *testing.T) {
	var children []string
	for r := rune('!'); r <= '~'; r++ {
		if r != '/' {
			children = append(children, string(r))
		}
	}
	children = append(children, "..", ".*", "[ab]", "case#01", "café", "e\u0301", "λ", "東京", "🙂", "\ufffd", "a_b", "\\x00", "\\t", "\\u200b")
	expected, events := descendantNamedEventsTest(children)
	args := replayInputsTest(t, expected, events, 0)
	admitted, err := expectedInputs(args[1], args[2])
	if err != nil || !reflect.DeepEqual(admitted, expected) {
		t.Fatalf("literal descendant admission: %+v %v", admitted, err)
	}
	result, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, events)), admitted, args[3], 0)
	if err != nil || result.Roots != 1 || result.Passed != 1 || result.Subtests != len(children) || result.SubtestsPassed != len(children) {
		t.Fatalf("literal identity census: %+v %v", result, err)
	}
	for _, change := range []struct {
		declared string
		foreign  string
	}{
		{declared: "*", foreign: "unlisted"},
		{declared: ".*", foreign: "also-unlisted"},
		{declared: "..", foreign: "../foreign"},
		{declared: "[ab]", foreign: "ab"},
		{declared: "case#01", foreign: "case#1"},
		{declared: "café", foreign: "cafe\u0301"},
		{declared: "λ", foreign: "Λ"},
		{declared: "a_b", foreign: "a b"},
		{declared: "\\x00", foreign: "\x00"},
	} {
		_, changed := descendantNamedEventsTest(children)
		original, replacement := "TestSyntheticNames/"+change.declared, "TestSyntheticNames/"+change.foreign
		if admitted.Outcomes[replacement] != "" {
			t.Fatalf("foreign control is already declared: %q", replacement)
		}
		replaced := 0
		for _, event := range changed {
			if event["Test"] == original {
				event["Test"] = replacement
				replaced++
			}
		}
		if replaced != 2 {
			t.Fatalf("control did not replace the complete child: %q %d", original, replaced)
		}
		if _, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, changed)), admitted, args[3], 0); err == nil {
			t.Fatalf("literal descendant acquired matching or normalization authority: %q -> %q", original, replacement)
		}
	}
}

// Inputs declare names after Go's rewrite, never raw whitespace or unprintable
// runes. Unicode uses the same inclusive byte and depth owners as ascii.
func TestQualificationDescendantNamesKeepCanonicalAndFiniteAdmission(t *testing.T) {
	root := "TestSyntheticNames"
	var invalid []string
	for _, r := range []rune{0, '\t', '\n', '\v', '\f', '\r', ' ', 0x7f, 0x85, 0xa0, 0xad, 0x1680, 0x2000, 0x200a, 0x200b, 0x200d, 0x2028, 0x2029, 0x202f, 0x205f, 0x2060, 0x3000} {
		invalid = append(invalid, root+"/raw"+string(r)+"name")
	}
	invalid = append(invalid, root+"/\xff", root+"/\xce", root+"/", root+"//child", root+"/child/", "Test-name/child", "TestÉ/child", "TestName#01/child")
	for _, name := range invalid {
		if validTestIdentity(name) {
			t.Fatalf("unrewritten or malformed name admitted: %q", name)
		}
		outcomes, markers := descendantInputFilesTest(t, root+"\tPASS\n"+name+"\tPASS\n", "")
		if _, err := expectedInputs(outcomes, markers); err == nil {
			t.Fatalf("metadata admitted malformed descendant: %q", name)
		}
	}
	for _, name := range []string{"Test-name", "TestÉ", "TestName#01", "Test.name", "Test*"} {
		if validTestIdentity(name) {
			t.Fatalf("top-level selector grammar widened: %q", name)
		}
	}
	prefix := root + "/"
	name := prefix + strings.Repeat("λ", (4096-len(prefix))/len("λ"))
	name += strings.Repeat("x", 4096-len(name))
	if len(name) != 4096 {
		t.Fatal("exact Unicode byte-bound fixture is incorrect")
	}
	for _, identity := range []string{name, name + "x"} {
		outcomes, markers := descendantInputFilesTest(t, root+"\tPASS\n"+identity+"\tPASS\n", "")
		_, err := expectedInputs(outcomes, markers)
		if (err == nil) != (len(identity) == 4096) {
			t.Fatalf("Unicode byte owner %d: %v", len(identity), err)
		}
	}
	for _, depth := range []int{64, 65} {
		name, rows := root, root+"\tPASS\n"
		for index := 0; index < depth; index++ {
			name += "/λ-"
			rows += name + "\tPASS\n"
		}
		outcomes, markers := descendantInputFilesTest(t, rows, "")
		if _, err := expectedInputs(outcomes, markers); (err == nil) != (depth == 64) {
			t.Fatalf("Unicode depth owner %d: %v", depth, err)
		}
	}
}

// Even a literal dot parent is a separately declared, live identity. It cannot
// be cleaned away, synthesized by an event, or completed before its child.
func TestQualificationDescendantNamesRetainLiteralParentLifecycle(t *testing.T) {
	root, parent, leaf := "TestSyntheticNames", "TestSyntheticNames/..", "TestSyntheticNames/../λ+[x]"
	expected := expectedSuite{Roots: []string{root}, Outcomes: map[string]string{root: "pass", parent: "pass", leaf: "pass"}, Markers: map[string]string{}}
	makeEvents := func() []map[string]any {
		return []map[string]any{
			testEvent("start", "", ""), testEvent("run", root, ""), testEvent("run", parent, ""),
			testEvent("pause", parent, ""), testEvent("run", leaf, ""), testEvent("pass", leaf, ""),
			testEvent("cont", parent, ""), testEvent("pass", parent, ""), testEvent("pass", root, ""), testEvent("pass", "", ""),
		}
	}
	args := replayInputsTest(t, expected, makeEvents(), 0)
	admitted, err := expectedInputs(args[1], args[2])
	if err != nil {
		t.Fatal(err)
	}
	result, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, makeEvents())), admitted, args[3], 0)
	if err != nil || result.Passed != 1 || result.SubtestsPassed != 2 {
		t.Fatalf("literal parent lifecycle: %+v %v", result, err)
	}
	for _, fault := range []string{"missing-parent-declaration", "missing-parent-start", "missing-child", "parent-first-terminal", "duplicate-child", "cleaned-leaf", "late-child"} {
		events := makeEvents()
		caseExpected := expectedSuite{Roots: expected.Roots, Outcomes: map[string]string{}, Markers: expected.Markers}
		for name, outcome := range expected.Outcomes {
			caseExpected.Outcomes[name] = outcome
		}
		switch fault {
		case "missing-parent-declaration":
			delete(caseExpected.Outcomes, parent)
		case "missing-parent-start":
			events[2], events[3] = testEvent("output", root, ""), testEvent("output", root, "")
		case "missing-child":
			events[4], events[5] = testEvent("output", parent, ""), testEvent("output", parent, "")
		case "parent-first-terminal":
			events[3], events[5] = testEvent("output", parent, ""), testEvent("pass", parent, "")
		case "duplicate-child":
			events[5] = testEvent("run", leaf, "")
		case "cleaned-leaf":
			events[4]["Test"] = "λ+[x]"
		case "late-child":
			events = append(events[:9], testEvent("output", leaf, ""), events[9])
		}
		if _, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, events)), caseExpected, args[3], 0); err == nil {
			t.Fatalf("literal hierarchy admitted %s", fault)
		}
	}
}

// New name spellings do not change the exact causal literal owner, propagated
// ancestor outcome, or original binary-exit requirements of standalone replay.
func TestQualificationDescendantNamesKeepFailureLiteralAttribution(t *testing.T) {
	root, child := "TestSyntheticNames", "TestSyntheticNames/λ-case[1]"
	expected := expectedSuite{Roots: []string{root}, Outcomes: map[string]string{root: "fail", child: "fail"}, Markers: map[string]string{root: "parent propagated failure", child: "literal child causal assertion"}}
	makeEvents := func() []map[string]any {
		return []map[string]any{
			testEvent("start", "", ""), testEvent("run", root, ""), testEvent("run", child, ""),
			testEvent("output", child, "literal child causal "), testEvent("output", child, "assertion\n"),
			testEvent("fail", child, ""), testEvent("output", root, "parent propagated failure\n"),
			testEvent("fail", root, ""), testEvent("fail", "", ""),
		}
	}
	args := replayInputsTest(t, expected, makeEvents(), 1)
	var stdout, stderr bytes.Buffer
	if status := runReplay(args, &stdout, &stderr); status != 0 || stderr.Len() != 0 {
		t.Fatalf("literal causal replay: %d %q", status, stderr.String())
	}
	var result replayResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Verification == nil || result.Status != "matched" {
		t.Fatalf("literal causal summary: %+v %v", result, err)
	}
	if checked := result.Verification; checked.Roots != 1 || checked.Passed != 0 || checked.Subtests != 1 || checked.SubtestsPassed != 0 || checked.ExpectedFailures != 1 || checked.ExpectedSubtestFailures != 1 || checked.BinaryExit != 1 {
		t.Fatalf("literal causal ownership: %+v", checked)
	}
	for _, fault := range []string{"borrow-parent-literal", "borrow-child-literal", "missing-child-literal", "undeclared-spelling", "passing-exit"} {
		events, actualExit := makeEvents(), 1
		switch fault {
		case "borrow-parent-literal":
			events[3]["Output"], events[4]["Output"], events[6]["Output"] = "different", "\n", "parent propagated failure literal child causal assertion\n"
		case "borrow-child-literal":
			events[4]["Output"], events[6]["Output"] = "assertion parent propagated failure\n", "different\n"
		case "missing-child-literal":
			events[4]["Output"] = "different\n"
		case "undeclared-spelling":
			events[3]["Test"] = "TestSyntheticNames/λ-case1"
		case "passing-exit":
			actualExit = 0
		}
		replayRefusalTest(t, replayInputsTest(t, expected, events, actualExit))
	}
}

// This is the actual read-only command dispatch, with no available executable
// lookup. Literal event/metadata bytes and file owners remain unchanged.
func TestQualificationDescendantNamesReplayWithoutRewritingEvidence(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	expected, events := descendantNamedEventsTest([]string{"deposit-only_mirror", "non-alpha", "λ-case[1]", ".."})
	args := replayInputsTest(t, expected, events, 0)
	beforeBytes, beforeInfos := map[string][]byte{}, map[string]os.FileInfo{}
	for _, index := range []int{0, 1, 2, 4} {
		path := args[index]
		var err error
		beforeBytes[path], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		beforeInfos[path], err = os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	status := runReplay(args, &stdout, &stderr)
	var result replayResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || status != 0 || stderr.Len() != 0 || result.Status != "matched" || result.Package != args[3] || result.Error != "" || result.Verification == nil {
		t.Fatalf("literal replay dispatch: %d %+v %v %q", status, result, err, stderr.String())
	}
	if checked := result.Verification; checked.Roots != 1 || checked.Passed != 1 || checked.Subtests != 4 || checked.SubtestsPassed != 4 || checked.ExpectedFailures != 0 || checked.ExpectedSubtestFailures != 0 || checked.BinaryExit != 0 || checked.Events != len(events) || checked.Bytes != len(beforeBytes[args[0]]) {
		t.Fatalf("literal replay census: %+v", checked)
	}
	for path, before := range beforeBytes {
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) || !os.SameFile(beforeInfos[path], info) || beforeInfos[path].Mode() != info.Mode() || !beforeInfos[path].ModTime().Equal(info.ModTime()) {
			t.Fatalf("literal replay rewrote retained input: %s", path)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(args[0]))
	if err != nil || len(entries) != 4 {
		t.Fatalf("literal replay created another capture artifact: %d %v", len(entries), err)
	}
}
