// Deterministic scheduler, configuration and source-custody controls.
package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func testPlan(t *testing.T) planSpec {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "sn")
	directory := filepath.Join(source, "validator")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	outcomes, markers, manifest := filepath.Join(root, "outcomes.tsv"), filepath.Join(root, "markers.tsv"), filepath.Join(root, "source.sha256")
	testWrite(t, outcomes, "TestFirst\tPASS\nTestSecond\tPASS\n")
	testWrite(t, markers, "")
	testWrite(t, manifest, strings.Repeat("a", 64)+"  fixture.go\n")
	return planSpec{Version: 1, SourceRoot: source, Sources: []sourceSpec{{Root: source, Manifest: manifest}},
		Limits:   limitsSpec{Jobs: 4, BuildSeconds: 180, TestSeconds: 180, OuterSeconds: 240, Parallel: 4, GOMAXPROCS: 24},
		Packages: []packageSpec{{Id: "validator", Directory: directory, ImportPath: "example.com/sn/validator"}},
		Suites:   []suiteSpec{{Id: "normal", Package: "validator", Mode: "normal", Outcomes: outcomes, FailureLiterals: markers}}}
}

func TestQualificationPlanRequiresExactNonemptyMembership(t *testing.T) {
	plan := testPlan(t)
	if err := validatePlan(plan); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"", "TestSecond\tPASS\nTestFirst\tPASS\n", "TestFirst\tFAIL\n", "TestFirst\tPASS\nTestFirst\tPASS\n", "TestFirst\tPASS"} {
		testWrite(t, plan.Suites[0].Outcomes, content)
		if err := validatePlan(plan); err == nil {
			t.Fatalf("accepted invalid outcome input %q", content)
		}
	}
}

func TestQualificationPlanRejectsDuplicateIdentityAndSameModeRoots(t *testing.T) {
	for _, field := range []string{"sources", "packages", "suites", "roots"} {
		plan := testPlan(t)
		switch field {
		case "sources":
			plan.Sources = append(plan.Sources, plan.Sources[0])
		case "packages":
			plan.Packages = append(plan.Packages, plan.Packages[0])
		case "suites":
			plan.Suites = append(plan.Suites, plan.Suites[0])
		case "roots":
			duplicate := plan.Suites[0]
			duplicate.Id = "duplicate"
			plan.Suites = append(plan.Suites, duplicate)
		}
		if err := validatePlan(plan); err == nil {
			t.Fatalf("accepted duplicate %s", field)
		}
	}
	plan := testPlan(t)
	race := plan.Suites[0]
	race.Id, race.Mode = "race", "race"
	plan.Suites = append(plan.Suites, race)
	if err := validatePlan(plan); err != nil {
		t.Fatal(err)
	}
}

func TestQualificationPlanRejectsWrongModeAndLimits(t *testing.T) {
	for _, value := range []int{0, -1, 86401} {
		plan := testPlan(t)
		plan.Limits.Jobs = value
		if err := validatePlan(plan); err == nil {
			t.Fatalf("accepted job count %d", value)
		}
	}
	plan := testPlan(t)
	plan.Limits.OuterSeconds = plan.Limits.TestSeconds
	if err := validatePlan(plan); err == nil {
		t.Fatal("accepted outer timeout inside Go test timeout")
	}
	plan = testPlan(t)
	plan.Suites[0].Mode = "ordinary"
	if err := validatePlan(plan); err == nil {
		t.Fatal("accepted unknown mode")
	}
}

func TestQualificationPlanRequiresPhysicalPackageOwnership(t *testing.T) {
	plan := testPlan(t)
	alias := filepath.Join(filepath.Dir(plan.SourceRoot), "alias")
	if err := os.Symlink(plan.Packages[0].Directory, alias); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{alias, filepath.Dir(plan.SourceRoot), "validator"} {
		plan.Packages[0].Directory = directory
		if err := validatePlan(plan); err == nil {
			t.Fatalf("accepted unowned or aliased directory %s", directory)
		}
	}
}

func TestQualificationJSONRejectsDuplicateUnknownAndTrailingFields(t *testing.T) {
	for _, data := range []string{`{"version":1,"version":2}`, `{"extra":1}`, `{"version":1} {}`, `{"limits":{"jobs":true}}`, `{"limits":{"jobs":1.5}}`} {
		var plan planSpec
		if err := decodeJSON([]byte(data), &plan); err == nil {
			t.Fatalf("accepted ambiguous input %s", data)
		}
	}
	var plan planSpec
	if err := decodeJSON([]byte(`{"version":1}`), &plan); err != nil {
		t.Fatal(err)
	}
	if err := validatePlan(plan); err == nil {
		t.Fatal("accepted incomplete plan")
	}
}

func TestQualificationReadySuiteDoesNotWaitForUnrelatedBuild(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseBuild := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseBuild()
	var buildCalls, suiteCalls atomic.Int32
	nodes := map[string]stage{"build-a": {}, "build-b": {}, "suite-a": {Dependencies: []string{"build-a"}}, "suite-b": {Dependencies: []string{"build-a"}}}
	execute := func(ctx context.Context, name string, _ stage) stageResult {
		switch name {
		case "build-a":
			buildCalls.Add(1)
			select {
			case <-entered:
			case <-ctx.Done():
				return stageResult{Status: "failed", Error: ctx.Err().Error()}
			}
		case "build-b":
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return stageResult{Status: "failed", Error: ctx.Err().Error()}
			}
		case "suite-a":
			suiteCalls.Add(1)
			releaseBuild()
		case "suite-b":
			suiteCalls.Add(1)
		}
		return stageResult{Status: "passed"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results, err := runDAG(ctx, nodes, 2, execute, func(map[string]stageResult, []string, int) error { return nil })
	if err != nil || ctx.Err() != nil {
		t.Fatalf("scheduler did not release independent work: %v / %v", err, ctx.Err())
	}
	if buildCalls.Load() != 1 || suiteCalls.Load() != 2 || len(results) != 4 {
		t.Fatalf("wrong exact build/suite census: %d/%d/%d", buildCalls.Load(), suiteCalls.Load(), len(results))
	}
	for name, result := range results {
		if result.Status != "passed" {
			t.Fatalf("%s: %+v", name, result)
		}
	}
}

func TestQualificationBuildFailureBlocksOnlyDependentsWithoutRetry(t *testing.T) {
	nodes := map[string]stage{"build": {}, "suite": {Dependencies: []string{"build"}}, "independent": {}}
	var calls atomic.Int32
	results, err := runDAG(context.Background(), nodes, 2, func(_ context.Context, name string, _ stage) stageResult {
		calls.Add(1)
		if name == "build" {
			return stageResult{Status: "failed"}
		}
		return stageResult{Status: "passed"}
	}, func(map[string]stageResult, []string, int) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || results["suite"].Status != "blocked" || results["independent"].Status != "passed" {
		t.Fatalf("failure was retried or spread to unrelated work: %+v", results)
	}
}

func TestQualificationCancellationDoesNotAdmitPendingStages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	result, err := runDAG(ctx, map[string]stage{"stage": {}}, 1, func(context.Context, string, stage) stageResult { calls.Add(1); return stageResult{Status: "passed"} }, func(map[string]stageResult, []string, int) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 || result["stage"].Status != "canceled" {
		t.Fatalf("admitted canceled work: %+v", result)
	}
}

func TestQualificationPublishFailureCancelsAndJoinsAdmittedWorker(t *testing.T) {
	finished := make(chan struct{})
	writeErr := errors.New("status output refused")
	results, err := runDAG(context.Background(), map[string]stage{"stage": {}}, 1, func(ctx context.Context, _ string, _ stage) stageResult {
		<-ctx.Done()
		close(finished)
		return stageResult{Status: "canceled"}
	}, func(map[string]stageResult, []string, int) error { return writeErr })
	if !errors.Is(err, writeErr) || results["stage"].Status != "canceled" {
		t.Fatalf("lost publishing failure: %v / %+v", err, results)
	}
	select {
	case <-finished:
	default:
		t.Fatal("scheduler returned before worker completion")
	}
}

func TestQualificationRejectsMissingAndCyclicDependencies(t *testing.T) {
	for _, dependency := range []string{"missing", "stage"} {
		_, err := runDAG(context.Background(), map[string]stage{"stage": {Dependencies: []string{dependency}}}, 1,
			func(context.Context, string, stage) stageResult { return stageResult{Status: "passed"} }, func(map[string]stageResult, []string, int) error { return nil })
		if err == nil {
			t.Fatalf("accepted dependency %s", dependency)
		}
	}
}

func TestQualificationStatusPreservesIncompleteWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	if err := writeJSON(path, matrixStatus{State: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(path, matrixStatus{State: "passed"}); err != nil {
		t.Fatal(err)
	}
	testWrite(t, path+".new", "retained incomplete evidence")
	if err := writeJSON(path, matrixStatus{State: "replaced"}); err == nil {
		t.Fatal("overwrote pending write")
	}
	var status matrixStatus
	if err := readJSON(path, &status); err != nil {
		t.Fatal(err)
	}
	if status.State != "passed" {
		t.Fatal("changed committed status")
	}
}

func testGitSource(t *testing.T) planSpec {
	t.Helper()
	plan := testPlan(t)
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", plan.SourceRoot}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git: %v: %s", err, output)
		}
	}
	git("init", "-q")
	testWrite(t, filepath.Join(plan.SourceRoot, "fixture.go"), "package fixture\n")
	git("add", "fixture.go")
	git("-c", "user.name=qualification-test", "-c", "user.email=qualification@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	hash, err := fileHash(filepath.Join(plan.SourceRoot, "fixture.go"))
	if err != nil {
		t.Fatal(err)
	}
	testWrite(t, plan.Sources[0].Manifest, hash+"  fixture.go\n")
	return plan
}

func TestQualificationSourceFenceChecksHashesModesAndGitInputs(t *testing.T) {
	plan := testGitSource(t)
	before, err := sourceFence(plan.Sources)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(plan.SourceRoot, "fixture.go")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	after, err := sourceFence(plan.Sources)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(before, after) {
		t.Fatal("lost source mode change")
	}
	testWrite(t, path, "package changed\n")
	if _, err := sourceFence(plan.Sources); err == nil {
		t.Fatal("accepted source hash mismatch")
	}
}

func TestQualificationSourceFenceRejectsUnlistedNewInputs(t *testing.T) {
	plan := testGitSource(t)
	testWrite(t, filepath.Join(plan.SourceRoot, "unexpected.go"), "package fixture\n")
	if _, err := sourceFence(plan.Sources); err == nil {
		t.Fatal("accepted new unlisted source")
	}
}

func TestQualificationSourceFenceRejectsEscapingAndDuplicatePaths(t *testing.T) {
	plan := testGitSource(t)
	hash, err := fileHash(filepath.Join(plan.SourceRoot, "fixture.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{hash + "  ../fixture.go\n", hash + "  ./fixture.go\n", strings.Repeat(hash+"  fixture.go\n", 2), hash + "  fixture.go"} {
		testWrite(t, plan.Sources[0].Manifest, content)
		if _, err := sourceFence(plan.Sources); err == nil {
			t.Fatalf("accepted invalid source manifest %q", content)
		}
	}
}

func TestQualificationCommandRequiresActualBinaryAndOwnerOutcome(t *testing.T) {
	for _, actual := range []int{0, 1, 124, 125, 137, 143} {
		for _, owner := range []int{0, 1, 124, 125, 137, 143} {
			for _, expected := range []int{0, 1} {
				result := commandResult{Exit: &actual, OwnerExit: owner, Joined: true}
				if commandMatches(result, expected) != (actual == expected && owner == expected) {
					t.Fatalf("lost binary/owner mismatch: %+v", result)
				}
			}
		}
	}
	if commandMatches(commandResult{}, 0) {
		t.Fatal("missing binary exit became success")
	}
}
