// Deterministic controls for exact event, bounded input and joined failure edges.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// A failed decode leaves the previous complete document untouched.
func TestQualificationReviewJSONRejectsNullAndCaseAliases(t *testing.T) {
	for _, data := range []string{`null`, `{"version":null}`, `{"source_root":null}`, `{"limits":null}`, `{"limits":{"jobs":null}}`, `{"Version":1}`, `{"version":1,"VERSION":2}`, `{"sources":[null]}`} {
		value := planSpec{Version: 19, SourceRoot: "unchanged"}
		before := value
		if err := decodeJSON([]byte(data), &value); err == nil || !reflect.DeepEqual(before, value) {
			t.Fatalf("accepted or partially installed %s: %+v/%v", data, value, err)
		}
	}
}

// Missing fields cannot inherit a previous successful command's authority.
func TestQualificationReviewJSONReusedDestinationIsFresh(t *testing.T) {
	exit := 0
	result := commandResult{Exit: &exit, Joined: true, Request: "old-request", Error: "old-error"}
	if err := decodeJSON([]byte(`{}`), &result); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, commandResult{}) || commandMatches(result, 0) {
		t.Fatalf("reused result retained authority: %+v", result)
	}
	if err := decodeJSON([]byte(`{"exit":null}`), &result); err != nil || result.Exit != nil {
		t.Fatalf("missing independent exit was not retained as absence: %+v/%v", result, err)
	}
}

// Nested small documents need an independent depth ceiling, not only bytes.
func TestQualificationReviewJSONBoundsDepthAndRejectsInvalidUTF8(t *testing.T) {
	var value any
	if err := decodeJSON([]byte(strings.Repeat("[", 64)+"0"+strings.Repeat("]", 64)), &value); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{[]byte(strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65)), {'"', 0xff, '"'}} {
		if err := decodeJSON(data, &value); err == nil {
			t.Fatal("accepted out-of-bound or non-UTF-8 JSON")
		}
	}
}

// Null is not the optional absence of Test, nor empty Output or FailedBuild.
func TestQualificationReviewEventsRejectNullTextFields(t *testing.T) {
	for _, control := range []struct {
		index int
		field string
	}{{index: 0, field: "Test"}, {index: 0, field: "Action"}, {index: 0, field: "Package"}, {index: 0, field: "FailedBuild"}, {index: 5, field: "Output"}, {index: 10, field: "Output"}} {
		events := testEvents()
		events[control.index][control.field] = nil
		if _, err := verifyEvents(bytes.NewReader(testEncodedEvents(t, events)), testExpectedEvents(), "example.com/validator", 1); err == nil {
			t.Fatalf("accepted null %s at %d", control.field, control.index)
		}
	}
}

// A complete JSON object without its final newline is still a truncated row.
func TestQualificationReviewEventsRequireCompleteFinalRecord(t *testing.T) {
	data := testEncodedEvents(t, testEvents())
	if _, err := verifyEvents(bytes.NewReader(data[:len(data)-1]), testExpectedEvents(), "example.com/validator", 1); err == nil {
		t.Fatal("accepted unterminated package terminal record")
	}
	crlf := bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
	checked, err := verifyEvents(bytes.NewReader(crlf), testExpectedEvents(), "example.com/validator", 1)
	if err != nil || checked.Bytes != len(crlf) {
		t.Fatalf("lost actual CRLF stream byte accounting: %+v/%v", checked, err)
	}
}

// The one-megabyte row bound includes its newline, matching the original checker.
func TestQualificationReviewEventsIncludeNewlineInExactByteBound(t *testing.T) {
	for _, extra := range []int{0, 1} {
		events := testEvents()
		empty, err := json.Marshal(testEvent("output", "", ""))
		if err != nil {
			t.Fatal(err)
		}
		events[10] = testEvent("output", "", strings.Repeat("x", metadataLimit-len(empty)-1+extra))
		_, err = verifyEvents(bytes.NewReader(testEncodedEvents(t, events)), testExpectedEvents(), "example.com/validator", 1)
		if (err != nil) != (extra != 0) {
			t.Fatalf("wrong exact event row admission for extra=%d: %v", extra, err)
		}
	}
}

// Hide the source's WriterTo so io.Copy must exercise the destination contract.
func TestQualificationReviewMetadataCopyCannotBypassByteBound(t *testing.T) {
	bounded := &boundedOutput{Maximum: 4}
	if count, err := io.Copy(bounded, struct{ io.Reader }{Reader: strings.NewReader("12345")}); err == nil || count > 4 || len(bounded.Bytes()) > 4 {
		t.Fatalf("promoted ReadFrom bypassed bound: %d/%d/%v", count, len(bounded.Bytes()), err)
	}
	if _, err := bounded.Write([]byte("1")); err == nil {
		t.Fatal("overflowed metadata owner resumed publication")
	}
	exact := &boundedOutput{Maximum: 4}
	if _, err := io.Copy(exact, struct{ io.Reader }{Reader: strings.NewReader("1234")}); err != nil || string(exact.Bytes()) != "1234" {
		t.Fatalf("exact bound refused: %q/%v", exact.Bytes(), err)
	}
}

// A short successful Write is an I/O failure, and that failure stays sticky.
type reviewShortWriter struct{ calls int }

func (self *reviewShortWriter) Write(data []byte) (int, error) {
	self.calls++
	return len(data) - 1, nil
}

func TestQualificationReviewCaptureRetainsShortWriteAndOverflowFailure(t *testing.T) {
	short := &reviewShortWriter{}
	owner := &boundedCapture{File: short, Remaining: 4}
	if _, err := owner.Write([]byte("12")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write accepted: %v", err)
	}
	if _, err := owner.Write([]byte("1")); !errors.Is(err, io.ErrShortWrite) || short.calls != 1 {
		t.Fatal("failed capture wrote again")
	}
	var buffer bytes.Buffer
	owner = &boundedCapture{File: &buffer, Remaining: 4}
	if _, err := owner.Write([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Write([]byte("5")); err == nil || buffer.String() != "1234" {
		t.Fatal("capture overflow changed retained evidence")
	}
}

// Pre-open type rejection must not block on a FIFO, follow an alias or allocate
// from an invalid declared limit.
func TestQualificationReviewBoundedReadsRejectFIFOAliasAndInvalidLimits(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, fifo, alias := filepath.Join(root, "file"), filepath.Join(root, "fifo"), filepath.Join(root, "alias")
	testWrite(t, file, "bytes")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fifo, alias} {
		if _, err := readBounded(path, 5); err == nil {
			t.Fatalf("accepted nonphysical regular input %s", path)
		}
	}
	for _, maximum := range []int64{-1, regularFileLimit + 1, 4} {
		if _, err := readBounded(file, maximum); err == nil {
			t.Fatalf("accepted input bound %d", maximum)
		}
	}
	if data, err := readBounded(file, 5); err != nil || string(data) != "bytes" {
		t.Fatalf("exact bounded input refused: %q/%v", data, err)
	}
}

// Same-byte replacement cannot supply the terminal witness of an older open.
func TestQualificationReviewRetainedFileRejectsSameByteReplacement(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "input")
	testWrite(t, path, "same")
	file, before, err := openRegular(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	testWrite(t, filepath.Join(root, "replacement"), "same")
	if err := os.Rename(filepath.Join(root, "replacement"), path); err != nil {
		t.Fatal(err)
	}
	if err := sameRegular(path, file, before); err == nil {
		t.Fatal("retained file adopted a same-byte replacement inode")
	}
}

// Two labels cannot evade same-package/mode root uniqueness.
func TestQualificationReviewPlanRejectsPhysicalAndImportAliases(t *testing.T) {
	for _, kind := range []string{"directory", "import"} {
		plan := testPlan(t)
		duplicate := plan.Packages[0]
		duplicate.Id = "alias"
		if kind == "directory" {
			duplicate.ImportPath += "/alias"
		} else {
			duplicate.Directory = plan.SourceRoot
		}
		plan.Packages = append(plan.Packages, duplicate)
		if err := validatePlan(plan); err == nil {
			t.Fatalf("accepted package %s alias", kind)
		}
	}
}

// Invalid graphs are rejected before even unrelated ready work is admitted.
func TestQualificationReviewInvalidDAGDoesNotStartIndependentWork(t *testing.T) {
	for _, dependency := range []string{"missing", "broken"} {
		var calls atomic.Int32
		_, err := runDAG(context.Background(), map[string]stage{"independent": {}, "broken": {Dependencies: []string{dependency}}}, 2,
			func(context.Context, string, stage) stageResult { calls.Add(1); return stageResult{Status: "passed"} }, func(map[string]stageResult, []string, int) error { return nil })
		if err == nil || calls.Load() != 0 {
			t.Fatalf("invalid graph admitted %d stages: %v", calls.Load(), err)
		}
	}
}

// Enter both workers before forcing nonreturn; the surviving peer must observe
// cancellation and finish before the scheduler returns either failure.
func reviewAbortedWorker(t *testing.T, abort func()) {
	t.Helper()
	entered, joined := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	results, err := runDAG(ctx, map[string]stage{"abort": {}, "peer": {}, "pending": {Dependencies: []string{"abort"}}}, 2,
		func(ctx context.Context, name string, _ stage) stageResult {
			if name == "abort" {
				<-entered
				abort()
				return stageResult{Status: "passed"}
			}
			if name != "peer" {
				return stageResult{Status: "failed", Error: "admitted failed dependent"}
			}
			close(entered)
			<-ctx.Done()
			close(joined)
			return stageResult{Status: "canceled"}
		}, func(map[string]stageResult, []string, int) error { return nil })
	if err == nil || ctx.Err() != nil || results["abort"].Status != "failed" || results["peer"].Status != "canceled" || results["pending"].Status != "canceled" {
		t.Fatalf("lost nonreturning worker failure: %+v/%v/%v", results, err, ctx.Err())
	}
	select {
	case <-joined:
	default:
		t.Fatal("returned before admitted peer joined")
	}
}

func TestQualificationReviewDAGPanicCancelsAndJoinsPeer(t *testing.T) {
	reviewAbortedWorker(t, func() { panic("deterministic worker panic") })
}

func TestQualificationReviewDAGGoexitCancelsAndJoinsPeer(t *testing.T) {
	reviewAbortedWorker(t, runtime.Goexit)
}

// Publisher failure occurs only after the worker owns its live context.
func reviewAbortedPublisher(t *testing.T, abort func()) {
	t.Helper()
	entered, joined := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	results, err := runDAG(ctx, map[string]stage{"peer": {}}, 1,
		func(ctx context.Context, _ string, _ stage) stageResult {
			close(entered)
			<-ctx.Done()
			close(joined)
			return stageResult{Status: "canceled"}
		},
		func(map[string]stageResult, []string, int) error { <-entered; abort(); return nil })
	if err == nil || ctx.Err() != nil || results["peer"].Status != "canceled" {
		t.Fatalf("publisher failure did not cancel work: %+v/%v/%v", results, err, ctx.Err())
	}
	select {
	case <-joined:
	default:
		t.Fatal("publisher failure returned before peer joined")
	}
}

func TestQualificationReviewPublisherPanicCancelsAndJoinsPeer(t *testing.T) {
	reviewAbortedPublisher(t, func() { panic("deterministic publisher panic") })
}

func TestQualificationReviewPublisherGoexitCancelsAndJoinsPeer(t *testing.T) {
	reviewAbortedPublisher(t, runtime.Goexit)
}

// Presentation callbacks cannot rewrite the scheduler's retained verdict.
func TestQualificationReviewPublisherCannotMutateRetainedResults(t *testing.T) {
	exit := 0
	results, err := runDAG(t.Context(), map[string]stage{"build": {}, "suite": {Dependencies: []string{"build"}}}, 1,
		func(context.Context, string, stage) stageResult {
			return stageResult{Status: "passed", Command: &commandResult{Exit: &exit, Joined: true}}
		},
		func(snapshot map[string]stageResult, _ []string, _ int) error {
			for name, result := range snapshot {
				result.Status = "failed"
				*result.Command.Exit = 1
				result.Command.Joined = false
				snapshot[name] = result
			}
			return nil
		})
	if err != nil || len(results) != 2 || exit != 0 {
		t.Fatalf("publisher mutated retained owner: %+v/%v", results, err)
	}
	for _, result := range results {
		if result.Status != "passed" || !commandMatches(*result.Command, 0) {
			t.Fatalf("publisher rewrote terminal result: %+v", result)
		}
	}
}

// A matching assertion exit is not proof that the process owner joined.
func TestQualificationReviewEqualFailureExitsNeedJoinedOwner(t *testing.T) {
	exit := 1
	result := commandResult{Exit: &exit, OwnerExit: 1}
	if commandMatches(result, 1) {
		t.Fatal("unproven cleanup became expected assertion failure")
	}
	result.Joined = true
	if !commandMatches(result, 1) {
		t.Fatal("genuine joined assertion exit refused")
	}
	result.Error = "stdout close failed"
	if commandMatches(result, 1) {
		t.Fatal("capture failure became expected assertion failure")
	}
}

// Frozen inputs and sticky cleanup refusal are checked before request creation.
func TestQualificationReviewOwnerRefusesMutationBeforeCommandAdmission(t *testing.T) {
	for _, kind := range []string{"input", "unproven", "tool"} {
		capture, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(capture, "input")
		testWrite(t, path, "before")
		proof, err := regularProof(path)
		if err != nil {
			t.Fatal(err)
		}
		owner := &stageOwner{Capture: capture, Inputs: map[string]fileProof{path: proof}}
		switch kind {
		case "input":
			testWrite(t, path, "after!")
		case "unproven":
			owner.Unproven.Store(true)
		case "tool":
			owner.Tools = map[string]string{"bash": filepath.Join(capture, "wrong-bash")}
		}
		if _, err := owner.command(t.Context(), "refused", []string{"unused"}, capture, 1, ""); err == nil {
			t.Fatalf("admitted %s refusal", kind)
		}
		if _, err := os.Lstat(filepath.Join(capture, "refused.request.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s refusal created command request: %v", kind, err)
		}
	}
}

// Replacing both a test binary and its adjacent hash cannot replace the build
// result retained in the scheduler's own memory.
func TestQualificationReviewBinaryProofCannotSelfAuthorizeReplacement(t *testing.T) {
	fixture := newQualificationModuleAliasFixture(t)
	plan := fixture.plan
	moduleSources, err := captureModuleSources(fixture.graph(t), plan.Sources)
	if err != nil || len(moduleSources) != 2 {
		t.Fatalf("original parent module proof: %d/%v", len(moduleSources), err)
	}
	capture := filepath.Dir(plan.SourceRoot)
	binary := filepath.Join(capture, "build.testbin")
	testWrite(t, binary, "original parent-owned build")
	parentProof, err := regularProof(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(binary+".json", parentProof); err != nil {
		t.Fatal(err)
	}
	testWrite(t, binary, "replacement")
	proof, err := regularProof(binary)
	if err != nil {
		t.Fatal(err)
	}
	if proof.SHA256 == parentProof.SHA256 || proof.Mode != parentProof.Mode {
		t.Fatal("binary replacement did not isolate the parent-owned digest")
	}
	if err := writeJSON(binary+".json", proof); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name          string
		moduleSources []moduleSourceProof
		refusal       string
	}{
		{name: "missing-module-owner", refusal: "parent-owned module source proof"},
		{name: "captured-module-owner", moduleSources: moduleSources, refusal: "parent-owned build"},
	} {
		node := stage{Suite: &plan.Suites[0], Binary: "build", BinarySHA256: parentProof.SHA256, BinaryMode: parentProof.Mode, ModuleSources: item.moduleSources}
		result := executeStage(t.Context(), "suite", node, &stageOwner{Capture: capture}, plan, map[string]packageSpec{plan.Packages[0].Id: plan.Packages[0]})
		if result.Status != "failed" || result.Command != nil || !strings.Contains(result.Error, item.refusal) {
			t.Fatalf("%s replacement binary self-authorized: %+v", item.name, result)
		}
		if _, err := os.Lstat(filepath.Join(capture, "suite-list.request.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s replacement reached actual binary admission", item.name)
		}
	}
}

// A setup error after mkdir must produce a readable terminal failure record.
func TestQualificationReviewCaptureSetupFailurePublishesTerminalStatus(t *testing.T) {
	capture := t.TempDir()
	cause := errors.New("deterministic setup refusal")
	status, err := finishCapture(capture, matrixStatus{State: "running"}, map[string]stageResult{}, cause)
	if !errors.Is(err, cause) || status.State != "failed" || !strings.Contains(status.Error, cause.Error()) {
		t.Fatalf("lost setup failure: %+v/%v", status, err)
	}
	var saved matrixStatus
	if err := readJSON(filepath.Join(capture, "status.json"), &saved); err != nil || !reflect.DeepEqual(saved, status) {
		t.Fatalf("missing durable terminal status: %+v/%v", saved, err)
	}
}

// Report refusal cannot leave either the returned or committed status passed.
func TestQualificationReviewReportFailureCannotPublishPassedStatus(t *testing.T) {
	capture := t.TempDir()
	incomplete := filepath.Join(capture, "report.json.new")
	testWrite(t, incomplete, "retained")
	status, err := finishCapture(capture, matrixStatus{State: "passed", SourceUnchanged: true, Total: 1}, map[string]stageResult{"stage": {Status: "passed"}}, nil)
	if err == nil || status.State != "failed" {
		t.Fatalf("report failure announced success: %+v/%v", status, err)
	}
	var saved matrixStatus
	if err := readJSON(filepath.Join(capture, "status.json"), &saved); err != nil || saved.State != "failed" {
		t.Fatalf("committed status hid report failure: %+v/%v", saved, err)
	}
	if retained, err := os.ReadFile(incomplete); err != nil || string(retained) != "retained" {
		t.Fatal("overwrote incomplete report evidence")
	}
}

// A blocked status rename retains old progress and a distinct failure artifact.
func TestQualificationReviewStatusFailureRetainsExplicitFailureArtifact(t *testing.T) {
	capture := t.TempDir()
	path := filepath.Join(capture, "status.json")
	if err := writeJSON(path, matrixStatus{State: "running"}); err != nil {
		t.Fatal(err)
	}
	testWrite(t, path+".new", "retained")
	status, err := finishCapture(capture, matrixStatus{State: "passed", SourceUnchanged: true, Total: 1}, map[string]stageResult{"stage": {Status: "passed"}}, nil)
	if err == nil || status.State != "failed" {
		t.Fatalf("status failure announced success: %+v/%v", status, err)
	}
	var saved matrixStatus
	if err := readJSON(path, &saved); err != nil || saved.State != "running" {
		t.Fatal("overwrote previously committed progress")
	}
	if err := readJSON(filepath.Join(capture, "status.failure.json"), &saved); err != nil || saved.State != "failed" || saved.Error == "" {
		t.Fatalf("lost explicit final publication failure: %+v/%v", saved, err)
	}
	if current, err := readCaptureStatus(capture); err != nil || current.State != "failed" {
		t.Fatalf("status lookup reported stale running state: %+v/%v", current, err)
	}
}

// Local replacement paths are independently declared, even when go list exits 0.
func TestQualificationReviewModuleGraphRequiresOwnedLocalReplacement(t *testing.T) {
	plan := testPlan(t)
	testWrite(t, filepath.Join(plan.SourceRoot, "go.mod"), "module example.com/sn\n")
	main := map[string]any{"Path": "example.com/sn", "Main": true, "Dir": plan.SourceRoot, "GoMod": filepath.Join(plan.SourceRoot, "go.mod")}
	mainBytes, err := json.Marshal(main)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyModuleSources(mainBytes, plan.Sources); err != nil {
		t.Fatal(err)
	}
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	testWrite(t, filepath.Join(outside, "go.mod"), "module example.com/dependency\n")
	for _, directory := range []any{outside, nil, "", plan.SourceRoot + "/.."} {
		replacement, err := json.Marshal(map[string]any{"Path": "example.com/dependency", "Replace": map[string]any{"Path": "../dependency", "Dir": directory, "GoMod": filepath.Join(outside, "go.mod")}})
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyModuleSources(append(append(append([]byte(nil), mainBytes...), '\n'), replacement...), plan.Sources); err == nil {
			t.Fatalf("accepted unowned replacement directory %v", directory)
		}
	}
	for _, invalid := range []string{`null`, `{"Path":"example.com/no-main","Version":"v1.0.0"}`, `{"Path":"example.com/sn","Main":null}`, string(mainBytes) + "\n" + string(mainBytes)} {
		if err := verifyModuleSources([]byte(invalid), plan.Sources); err == nil {
			t.Fatalf("accepted malformed module graph %s", invalid)
		}
	}
}

// Inherited startup hooks must not execute before the actual child owner exists.
func TestQualificationReviewEnvironmentDropsUnownedStartupHooks(t *testing.T) {
	inherited := []string{"PATH=/pinned", "BASH_ENV=/bad", "ENV=/bad", "BASH_FUNC_bad%%=bad", "PYTHONPATH=/bad", "LD_PRELOAD=/bad", "GOFLAGS=-overlay=/bad", "WARP_TEST_ENV_FAIL_FAST=0"}
	overrides := map[string]string{"GOFLAGS": "-mod=readonly", "WARP_TEST_ENV_FAIL_FAST": "1"}
	actual := executionEnvironment(inherited, overrides)
	expected := map[string]string{"PATH": "/pinned", "GOFLAGS": "-mod=readonly", "WARP_TEST_ENV_FAIL_FAST": "1"}
	if !reflect.DeepEqual(actual, expected) || len(overrides) != 2 {
		t.Fatalf("unowned startup environment survived: %+v", actual)
	}
}

// Output identity is private, exact and checked before any output is opened.
func TestQualificationReviewRequestRejectsAliasesAndUnboundedTimeout(t *testing.T) {
	capture, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(capture, "request.json")
	request := commandRequest{Argv: []string{"unused"}, Directory: capture, Seconds: 1, Environment: map[string]string{}, Stdout: filepath.Join(capture, "stdout"), Stderr: filepath.Join(capture, "stderr"), Result: filepath.Join(capture, "result")}
	if err := validateRequest(path, request); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"same-output", "outside-output", "input-output", "zero", "overflow"} {
		invalid := request
		switch kind {
		case "same-output":
			invalid.Stderr = invalid.Stdout
		case "outside-output":
			invalid.Stdout = filepath.Join(filepath.Dir(capture), "outside")
		case "input-output":
			invalid.Stdin = invalid.Stdout
		case "zero":
			invalid.Seconds = 0
		case "overflow":
			invalid.Seconds = 86401
		}
		if err := validateRequest(path, invalid); err == nil {
			t.Fatalf("accepted %s request", kind)
		}
	}
}

// Source manifests are rooted at the candidate, while a compiled test binary
// reads package-local fixtures. Both fences must retain the explicit root even
// when the caller remains in the package directory between them.
func TestQualificationReviewSourceFenceBeforeAndAfterPackageCWD(t *testing.T) {
	plan := testGitSource(t)
	packageFile := filepath.Join(plan.Packages[0].Directory, "fixture.input")
	testWrite(t, packageFile, "package-local fixture")
	rootHash, err := fileHash(filepath.Join(plan.SourceRoot, "fixture.go"))
	if err != nil {
		t.Fatal(err)
	}
	packageHash, err := fileHash(packageFile)
	if err != nil {
		t.Fatal(err)
	}
	testWrite(t, plan.Sources[0].Manifest, rootHash+"  fixture.go\n"+packageHash+"  validator/fixture.input\n")
	t.Chdir(plan.Packages[0].Directory)
	before, err := sourceFenceContext(t.Context(), plan.Sources)
	if err != nil {
		t.Fatalf("candidate-root before fence inherited package cwd: %v", err)
	}
	actual, err := os.ReadFile("fixture.input")
	if err != nil || string(actual) != "package-local fixture" {
		t.Fatalf("execution did not retain package fixture cwd: %q/%v", actual, err)
	}
	after, err := sourceFenceContext(t.Context(), plan.Sources)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("candidate-root after fence inherited package cwd: %v", err)
	}
	if _, err := sourceFenceContext(t.Context(), []sourceSpec{{Root: plan.Packages[0].Directory, Manifest: plan.Sources[0].Manifest}}); err == nil {
		t.Fatal("wrong explicit source root was guessed or normalized to its parent")
	}
	testWrite(t, filepath.Join(plan.SourceRoot, "fixture.go"), "package changed\n")
	if _, err := sourceFenceContext(t.Context(), plan.Sources); err == nil {
		t.Fatal("post-execution source mutation escaped the exact root fence")
	}
}

// The ordinary Go graph reports workspace aliases, while authority is the
// separately declared physical repository and its exact go.mod contents.
type qualificationModuleAliasFixture struct {
	plan       planSpec
	main       map[string]any
	dependency map[string]any
	alias      string
	physical   string
}

func newQualificationModuleAliasFixture(t *testing.T) qualificationModuleAliasFixture {
	t.Helper()
	plan := testPlan(t)
	physical := filepath.Join(filepath.Dir(plan.SourceRoot), "physical-dependency")
	if err := os.MkdirAll(physical, 0700); err != nil {
		t.Fatal(err)
	}
	testWrite(t, filepath.Join(plan.SourceRoot, "go.mod"), "module example.com/sn\n\nreplace example.com/dependency => ../workspace-dependency\n")
	testWrite(t, filepath.Join(physical, "go.mod"), "module example.com/dependency\n")
	alias := filepath.Join(filepath.Dir(plan.SourceRoot), "workspace-dependency")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(filepath.Dir(plan.SourceRoot), "dependency.sha256")
	testWrite(t, manifest, strings.Repeat("b", 64)+"  go.mod\n")
	plan.Sources = append(plan.Sources, sourceSpec{Root: physical, Manifest: manifest})
	main := map[string]any{"Path": "example.com/sn", "Main": true, "Dir": plan.SourceRoot, "GoMod": filepath.Join(plan.SourceRoot, "go.mod")}
	replacement := map[string]any{"Path": "../workspace-dependency", "Dir": alias, "GoMod": filepath.Join(alias, "go.mod")}
	dependency := map[string]any{"Path": "example.com/dependency", "Version": "v0.0.0", "Replace": replacement, "Dir": alias, "GoMod": filepath.Join(alias, "go.mod")}
	return qualificationModuleAliasFixture{plan: plan, main: main, dependency: dependency, alias: alias, physical: physical}
}

func (self qualificationModuleAliasFixture) graph(t *testing.T) []byte {
	t.Helper()
	main, err := json.Marshal(self.main)
	if err != nil {
		t.Fatal(err)
	}
	dependency, err := json.Marshal(self.dependency)
	if err != nil {
		t.Fatal(err)
	}
	return append(append(append([]byte(nil), main...), '\n'), dependency...)
}

func TestQualificationReviewModuleGraphAcceptsDeclaredReplacementAlias(t *testing.T) {
	fixture := newQualificationModuleAliasFixture(t)
	t.Chdir(fixture.plan.Packages[0].Directory)
	if err := verifyModuleSources(fixture.graph(t), fixture.plan.Sources); err != nil {
		t.Fatalf("declared physical module behind Go workspace alias was rejected: %v", err)
	}
	proofs, err := captureModuleSources(fixture.graph(t), fixture.plan.Sources)
	if err != nil || len(proofs) != 2 {
		t.Fatalf("module proof census=%d: %v", len(proofs), err)
	}
	if proofs[0].Module != "example.com/dependency" || proofs[0].Directory != fixture.alias || proofs[0].PhysicalDirectory != fixture.physical || proofs[0].Root != fixture.physical {
		t.Fatalf("replacement lost lexical and physical identities: %+v", proofs)
	}
	if err := checkModuleSourceProofs(proofs); err != nil {
		t.Fatalf("unchanged exact routes failed after package-local work: %v", err)
	}
}

func TestQualificationReviewModuleGraphRejectsCompetingAndOmittedReplacement(t *testing.T) {
	for _, fault := range []string{"omitted-replacement", "omitted-replacement-and-version", "missing-path", "outer-directory", "outer-metadata", "noncanonical-directory", "foreign-metadata", "main-alias"} {
		fixture := newQualificationModuleAliasFixture(t)
		replacement := fixture.dependency["Replace"].(map[string]any)
		switch fault {
		case "omitted-replacement":
			delete(fixture.dependency, "Replace")
		case "omitted-replacement-and-version":
			delete(fixture.dependency, "Replace")
			delete(fixture.dependency, "Version")
		case "missing-path":
			delete(replacement, "Path")
		case "outer-directory":
			fixture.dependency["Dir"] = fixture.plan.SourceRoot
		case "outer-metadata":
			fixture.dependency["GoMod"] = filepath.Join(fixture.plan.SourceRoot, "go.mod")
		case "noncanonical-directory":
			replacement["Dir"] = fixture.alias + "/../workspace-dependency"
			delete(fixture.dependency, "Dir")
		case "foreign-metadata":
			replacement["GoMod"] = filepath.Join(fixture.plan.SourceRoot, "go.mod")
			delete(fixture.dependency, "GoMod")
		case "main-alias":
			fixture.main["Dir"] = fixture.alias
			fixture.main["GoMod"] = filepath.Join(fixture.alias, "go.mod")
		}
		if err := verifyModuleSources(fixture.graph(t), fixture.plan.Sources); err == nil {
			t.Errorf("%s crossed local module authority", fault)
		}
	}
}

func TestQualificationReviewModuleSourceProofRejectsRerouteAndMetadataChange(t *testing.T) {
	for _, fault := range []string{"undeclared-root", "different-declared-root", "missing-route", "metadata-bytes", "metadata-mode", "metadata-symlink"} {
		fixture := newQualificationModuleAliasFixture(t)
		other := filepath.Join(filepath.Dir(fixture.plan.SourceRoot), "other-dependency")
		if err := os.MkdirAll(other, 0700); err != nil {
			t.Fatal(err)
		}
		testWrite(t, filepath.Join(other, "go.mod"), "module example.com/dependency\n")
		if fault == "different-declared-root" {
			fixture.plan.Sources = append(fixture.plan.Sources, sourceSpec{Root: other, Manifest: fixture.plan.Sources[1].Manifest})
		}
		proofs, err := captureModuleSources(fixture.graph(t), fixture.plan.Sources)
		if err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "undeclared-root", "different-declared-root", "missing-route":
			if err := os.Remove(fixture.alias); err != nil {
				t.Fatal(err)
			}
			if fault != "missing-route" {
				if err := os.Symlink(other, fixture.alias); err != nil {
					t.Fatal(err)
				}
			}
		case "metadata-bytes":
			testWrite(t, filepath.Join(fixture.physical, "go.mod"), "module example.com/replaced\n")
		case "metadata-mode":
			if err := os.Chmod(filepath.Join(fixture.physical, "go.mod"), 0640); err != nil {
				t.Fatal(err)
			}
		case "metadata-symlink":
			if err := os.Remove(filepath.Join(fixture.physical, "go.mod")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(other, "go.mod"), filepath.Join(fixture.physical, "go.mod")); err != nil {
				t.Fatal(err)
			}
		}
		if err := checkModuleSourceProofs(proofs); err == nil {
			t.Errorf("%s crossed the post-command module fence", fault)
		}
	}
}

func TestQualificationReviewModuleAliasesDoNotRelaxConfiguredRoots(t *testing.T) {
	fixture := newQualificationModuleAliasFixture(t)
	if err := validatePlan(fixture.plan); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"source", "package"} {
		plan := fixture.plan
		plan.Sources = append([]sourceSpec(nil), fixture.plan.Sources...)
		plan.Packages = append([]packageSpec(nil), fixture.plan.Packages...)
		if fault == "source" {
			plan.Sources[1].Root = fixture.alias
		} else {
			plan.Packages[0].Directory = fixture.alias
		}
		if err := validatePlan(plan); err == nil {
			t.Errorf("reported module alias relaxed configured %s authority", fault)
		}
	}
}

func TestQualificationReviewModuleProofOwnershipSurvivesDAGAndPublisher(t *testing.T) {
	fixture := newQualificationModuleAliasFixture(t)
	proofs, err := captureModuleSources(fixture.graph(t), fixture.plan.Sources)
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[string]stage{"build": {}, "suite": {Dependencies: []string{"build"}, Binary: "build"}}
	results, err := runDAG(t.Context(), nodes, 2, func(ctx context.Context, name string, node stage) stageResult {
		if name == "build" {
			return stageResult{Status: "passed", BinarySHA256: "owned", ModuleSources: proofs}
		}
		if !reflect.DeepEqual(node.ModuleSources, proofs) {
			return stageResult{Status: "failed", Error: "parent module proof was not carried exactly"}
		}
		node.ModuleSources[0].Directory = "/child-mutation"
		return stageResult{Status: "passed"}
	}, func(snapshot map[string]stageResult, running []string, pending int) error {
		if build, ok := snapshot["build"]; ok && len(build.ModuleSources) != 0 {
			build.ModuleSources[0].Directory = "/publisher-mutation"
		}
		return nil
	})
	if err != nil || results["suite"].Status != "passed" || !reflect.DeepEqual(results["build"].ModuleSources, proofs) || proofs[0].Directory != fixture.alias {
		t.Fatalf("module ownership crossed scheduler callback boundary: %+v/%v", results, err)
	}
}

func TestQualificationReviewSuiteRejectsChangedModuleRouteBeforeCommand(t *testing.T) {
	fixture := newQualificationModuleAliasFixture(t)
	proofs, err := captureModuleSources(fixture.graph(t), fixture.plan.Sources)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fixture.plan.SourceRoot, fixture.alias); err != nil {
		t.Fatal(err)
	}
	node := stage{Suite: &fixture.plan.Suites[0], ModuleSources: proofs}
	result := executeStage(t.Context(), "suite", node, &stageOwner{}, fixture.plan, map[string]packageSpec{fixture.plan.Packages[0].Id: fixture.plan.Packages[0]})
	if result.Status != "failed" || result.Command != nil || !strings.Contains(result.Error, "local module route") {
		t.Fatalf("rerouted module admitted a list/test command: %+v", result)
	}
}
