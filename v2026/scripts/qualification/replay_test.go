// Standalone replay is a bounded read-only verifier of retained evidence, not
// another compile, test body, converter or source-qualification attempt.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Build physical replay inputs through the same canonical metadata contract
// used by suite execution. No observed event determines expected membership.
func replayInputsTest(t *testing.T, expected expectedSuite, events []map[string]any, actualExit int) []string {
	t.Helper()
	var outcomes, literals []string
	for name, outcome := range expected.Outcomes {
		outcomes = append(outcomes, name+"\t"+strings.ToUpper(outcome))
	}
	for name, literal := range expected.Markers {
		literals = append(literals, name+"\t"+literal)
	}
	sort.Strings(outcomes)
	sort.Strings(literals)
	markers := ""
	if len(literals) > 0 {
		markers = strings.Join(literals, "\n") + "\n"
	}
	outcomePath, markerPath := descendantInputFilesTest(t, strings.Join(outcomes, "\n")+"\n", markers)
	directory := filepath.Dir(outcomePath)
	eventPath, exitPath := filepath.Join(directory, "events.json"), filepath.Join(directory, "body.exit")
	testWrite(t, eventPath, string(testEncodedEvents(t, events)))
	testWrite(t, exitPath, strconv.Itoa(actualExit)+"\n")
	return []string{eventPath, outcomePath, markerPath, "example.com/validator", exitPath}
}

// This is main's exact replay dispatch. Empty executable lookup and unchanged
// input bytes/modes prove replay owns no body, converter or receipt mutation.
func TestQualificationReplayChecksDescendantsWithoutReexecution(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	expected, events := descendantTreeTest()
	args := replayInputsTest(t, expected, events, 0)
	beforeBytes := map[string][]byte{}
	beforeInfos := map[string]os.FileInfo{}
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
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if status != 0 || stderr.Len() != 0 || result.Status != "matched" || result.Package != args[3] || result.Error != "" || result.Verification == nil {
		t.Fatalf("replay dispatch: exit=%d result=%+v stderr=%q", status, result, stderr.String())
	}
	checked := result.Verification
	if checked.Roots != 1 || checked.Passed != 1 || checked.Subtests != 3 || checked.SubtestsPassed != 3 || checked.ExpectedFailures != 0 || checked.ExpectedSubtestFailures != 0 || checked.BinaryExit != 0 || checked.Events != len(events) || checked.Bytes != len(beforeBytes[args[0]]) {
		t.Fatalf("root/subtest summary: %+v", checked)
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
			t.Fatalf("replay mutated retained input %s", path)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(args[0]))
	if err != nil || len(entries) != 4 {
		t.Fatalf("replay created another capture artifact: %d %v", len(entries), err)
	}
}

// Legacy top-level causal assertions retain exact exit/literal ownership and
// the original root counts; no subtest is invented by the new summary fields.
func TestQualificationReplayPreservesTopLevelCausalEvidence(t *testing.T) {
	args := replayInputsTest(t, testExpectedEvents(), testEvents(), 1)
	var stdout, stderr bytes.Buffer
	if status := runReplay(args, &stdout, &stderr); status != 0 || stderr.Len() != 0 {
		t.Fatalf("top-level replay: %d %q", status, stderr.String())
	}
	var result replayResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Verification == nil {
		t.Fatalf("replay result: %+v %v", result, err)
	}
	checked := result.Verification
	if result.Status != "matched" || checked.Roots != 2 || checked.Passed != 1 || checked.ExpectedFailures != 1 || checked.Subtests != 0 || checked.SubtestsPassed != 0 || checked.ExpectedSubtestFailures != 0 || checked.BinaryExit != 1 {
		t.Fatalf("legacy root census changed: %+v", result)
	}
}

// A refusal is explicit machine-readable output and a nonzero command status,
// never an empty or partial matched summary.
func replayRefusalTest(t *testing.T, args []string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if status := runReplay(args, &stdout, &stderr); status != 1 {
		t.Fatalf("invalid replay status %d", status)
	}
	var result replayResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Status != "refused" || result.Verification != nil || result.Error == "" || stderr.Len() == 0 {
		t.Fatalf("refusal lost its evidence: %+v %v %q", result, err, stderr.String())
	}
}

// Binary exits remain canonical bounded receipts. Timeouts, cancellations and
// wrong declared exits cannot be relabeled as an assertion or a passing replay.
func TestQualificationReplayRejectsArgumentsAndInvalidExitReceipts(t *testing.T) {
	expected, events := descendantTreeTest()
	args := replayInputsTest(t, expected, events, 0)
	for _, count := range []int{0, 1, 4, 6} {
		input := append([]string(nil), args...)
		if count == 6 {
			input = append(input, "unowned")
		} else {
			input = input[:count]
		}
		replayRefusalTest(t, input)
	}
	for _, packagePath := range []string{"", "/absolute", "../escape", "example.com/../validator", "example.com//validator", ".", "..", "example.com/validator/", "example.com/validator\n", "example.com/foreign", strings.Repeat("x", metadataLimit+1)} {
		input := append([]string(nil), args...)
		input[3] = packagePath
		replayRefusalTest(t, input)
	}
	for _, receipt := range []string{"", "0", "00\n", " 0\n", "0 \n", "0\r\n", "-1\n", "256\n", "124\n", "137\n", "2\n", "1\n", "0\n0\n", strings.Repeat("0", 17)} {
		testWrite(t, args[4], receipt)
		replayRefusalTest(t, args)
	}
}

// Every supplied file is a physical, bounded regular owner. Sparse oversized
// events and a fifo are refused before any data read can allocate or block.
func TestQualificationReplayRequiresPhysicalBoundedInputs(t *testing.T) {
	expected, events := descendantTreeTest()
	args := replayInputsTest(t, expected, events, 0)
	for _, index := range []int{0, 1, 2, 4} {
		input := append([]string(nil), args...)
		alias := args[index] + ".alias"
		if err := os.Symlink(args[index], alias); err != nil {
			t.Fatal(err)
		}
		input[index] = alias
		replayRefusalTest(t, input)
		input[index] = filepath.Dir(args[index])
		replayRefusalTest(t, input)
		input[index] = args[index] + ".missing"
		replayRefusalTest(t, input)
	}
	fifo := filepath.Join(filepath.Dir(args[0]), "events.fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	input := append([]string(nil), args...)
	input[0] = fifo
	replayRefusalTest(t, input)
	file, err := os.OpenFile(args[0], os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(file.Truncate(capturedJSONLimit+1), file.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err := replayEventsFiles(args); err == nil || !strings.Contains(err.Error(), "captured Json bound") {
		t.Fatalf("event file bound did not precede reading: %v", err)
	}
	replayRefusalTest(t, args)
}

// Replay applies the same complete event grammar and byte bounds. It does not
// filter malformed, extra, undeclared or incomplete descendants from evidence.
func TestQualificationReplayRetainsStrictEventRefusals(t *testing.T) {
	for _, fault := range []string{"empty", "partial", "oversized-record", "duplicate-key", "null-test", "foreign-child", "missing-child", "duplicate-child", "extra-terminal"} {
		expected, events := descendantTreeTest()
		args := replayInputsTest(t, expected, events, 0)
		raw := testEncodedEvents(t, events)
		switch fault {
		case "empty":
			raw = nil
		case "partial":
			raw = raw[:len(raw)-1]
		case "oversized-record":
			raw = []byte(strings.Repeat("x", metadataLimit) + "\n")
		case "duplicate-key":
			raw = bytes.Replace(raw, []byte("\"Test\":\"TestTree/child\""), []byte("\"Test\":\"TestTree/peer\",\"Test\":\"TestTree/child\""), 1)
		case "null-test":
			raw = bytes.Replace(raw, []byte("\"Test\":\"TestTree/child\""), []byte("\"Test\":null"), 1)
		case "foreign-child":
			events[6] = testEvent("run", "TestTree/child/foreign", "")
			raw = testEncodedEvents(t, events)
		case "missing-child":
			events[9], events[10] = testEvent("output", "TestTree", ""), testEvent("output", "TestTree", "")
			raw = testEncodedEvents(t, events)
		case "duplicate-child":
			events[9] = testEvent("run", "TestTree/child", "")
			raw = testEncodedEvents(t, events)
		case "extra-terminal":
			raw = append(raw, testEncodedEvents(t, []map[string]any{testEvent("pass", "", "")})...)
		}
		testWrite(t, args[0], string(raw))
		replayRefusalTest(t, args)
	}
}

// Return malformed counts or independent write errors to the actual command
// output owner; a valid stream cannot turn failed receipt publication into pass.
type replayOutputWriterTest struct {
	Count int
	Err   error
	Calls int
}

// No retained input or matched result may authorize a second output attempt.
func (self *replayOutputWriterTest) Write(data []byte) (int, error) {
	self.Calls++
	return self.Count, self.Err
}

// Short, negative and too-large counts are all invalid io.Writer outcomes.
func TestQualificationReplayRetainsOutputOwnerFailure(t *testing.T) {
	expected, events := descendantTreeTest()
	args := replayInputsTest(t, expected, events, 0)
	beforeArgs := append([]string(nil), args...)
	for _, output := range []*replayOutputWriterTest{
		{Count: 0}, {Count: -1}, {Count: 65536}, {Count: 0, Err: errors.New("synthetic replay output failure")},
	} {
		var stderr bytes.Buffer
		if status := runReplay(args, output, &stderr); status != 1 || output.Calls != 1 || stderr.Len() == 0 {
			t.Fatalf("lost output error: %+v status=%d stderr=%q", output, status, stderr.String())
		}
	}
	var output bytes.Buffer
	if runReplay(args, nil, &output) != 1 || runReplay(args, &output, nil) != 1 || output.Len() != 0 {
		t.Fatal("nil output owner admitted replay")
	}
	if !reflect.DeepEqual(args, beforeArgs) {
		t.Fatal("replay mutated its arguments")
	}
}

// A private test-binary child invokes the actual main branch and exits before
// its own test driver can emit a terminal record. No retained body is rerun.
func TestQualificationReplayStandaloneCommandDispatch(t *testing.T) {
	const inputEnvironment = "QUALIFICATION_SYNTHETIC_REPLAY_INPUTS"
	if directory := os.Getenv(inputEnvironment); directory != "" {
		os.Args = []string{"qualification", "replay", filepath.Join(directory, "events.json"), filepath.Join(directory, "outcomes.tsv"), filepath.Join(directory, "literals.tsv"), "example.com/validator", filepath.Join(directory, "body.exit")}
		main()
		t.Fatal("standalone replay dispatch returned")
	}
	expected, events := descendantTreeTest()
	args := replayInputsTest(t, expected, events, 0)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []int{0, 1} {
		if wanted == 1 {
			testWrite(t, args[0], "not a Json event\n")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		command := exec.CommandContext(ctx, binary, "-test.run=^TestQualificationReplayStandaloneCommandDispatch$")
		command.Env = append(os.Environ(), inputEnvironment+"="+filepath.Dir(args[0]))
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		runErr := command.Run()
		cancel()
		if command.ProcessState == nil || command.ProcessState.ExitCode() != wanted || (runErr != nil) != (wanted != 0) {
			t.Fatalf("standalone dispatch lost command exit %d: %v %q", wanted, runErr, stderr.String())
		}
		var result replayResult
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("standalone command emitted non-result output: %q %v", stdout.String(), err)
		}
		if wanted == 0 {
			if result.Status != "matched" || result.Verification == nil || result.Verification.Roots != 1 || result.Verification.Subtests != 3 || stderr.Len() != 0 {
				t.Fatalf("standalone matched census: %+v %q", result, stderr.String())
			}
		} else if result.Status != "refused" || result.Verification != nil || result.Error == "" || stderr.Len() == 0 {
			t.Fatalf("standalone refusal: %+v %q", result, stderr.String())
		}
	}
}
