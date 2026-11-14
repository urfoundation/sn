package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestQualificationWaitUsesOwnedChildDespiteIdenticalCommandText(t *testing.T) {
	const childEnvironment = "QUALIFICATION_SYNTHETIC_WAIT_CHILD"
	if os.Getenv(childEnvironment) == "1" {
		fmt.Fprintln(os.Stdout, "ready")
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(7)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	start := func() (*exec.Cmd, io.WriteCloser) {
		command := exec.Command(binary, "-test.run=^TestQualificationWaitUsesOwnedChildDespiteIdenticalCommandText$")
		command.Env = append(os.Environ(), childEnvironment+"=1")
		input, err := command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := command.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = input.Close(); _ = command.Process.Kill(); _, _ = command.Process.Wait() })
		if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || line != "ready\n" {
			t.Fatalf("child readiness: %q %v", line, err)
		}
		return command, input
	}
	owned, ownedInput := start()
	decoy, decoyInput := start()
	if !reflect.DeepEqual(owned.Args, decoy.Args) || owned.Process.Pid == decoy.Process.Pid {
		t.Fatal("fixture does not force identical command text with separate identities")
	}
	// The decoy is deliberately alive until after the owned child is reaped.
	// This handshake, rather than a race with pgrep, forces the incident shape.
	if err := ownedInput.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := waitOwnedCommand(ctx, owned); exitCode(err) != 7 || ctx.Err() != nil {
		t.Fatalf("wait lost exact completed child: %v / %v", err, ctx.Err())
	}
	if err := decoy.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("wait consumed or signaled command-text decoy: %v", err)
	}
	_ = decoyInput.Close()
	if err := waitOwnedCommand(ctx, decoy); exitCode(err) != 7 {
		t.Fatalf("decoy no longer independently joinable: %v", err)
	}
}

func TestQualificationCaptureLockRejectsLiveOwnerAndReopensAfterRelease(t *testing.T) {
	capture := t.TempDir()
	owner, err := lockCapture(capture, true)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if contender, err := lockCapture(capture, false); err == nil {
		contender.Close()
		t.Fatal("resume acquired an active capture")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	resumed, err := lockCapture(capture, false)
	if err != nil {
		t.Fatalf("released owner still appears alive: %v", err)
	}
	resumed.Close()
	if err := os.Remove(filepath.Join(capture, "run.lock")); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "foreign")
	testWrite(t, foreign, "")
	if err := os.Symlink(foreign, filepath.Join(capture, "run.lock")); err != nil {
		t.Fatal(err)
	}
	if lock, err := lockCapture(capture, false); err == nil {
		lock.Close()
		t.Fatal("capture lock followed an unowned symlink")
	}
}

func TestQualificationCaptureLockSurvivesLauncherDescriptorCloseUntilChildJoined(t *testing.T) {
	const childEnvironment = "QUALIFICATION_SYNTHETIC_LOCK_CHILD"
	if os.Getenv(childEnvironment) == "1" {
		fmt.Fprintln(os.Stdout, "ready")
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	capture := t.TempDir()
	lock, err := lockCapture(capture, true)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-test.run=^TestQualificationCaptureLockSurvivesLauncherDescriptorCloseUntilChildJoined$")
	command.Env = append(os.Environ(), childEnvironment+"=1")
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := startOwnedCommand(command, []*os.File{lock}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close(); _ = command.Process.Kill(); _, _ = command.Process.Wait() })
	if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("child readiness: %q %v", line, err)
	}
	// Closing the launcher's last descriptor is the kernel effect of its death.
	// The child stays alive behind the pipe handshake, with no timing assumption.
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if contender, err := lockCapture(capture, false); err == nil {
		contender.Close()
		t.Fatal("capture was reusable before the surviving child owner joined")
	}
	_ = input.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := waitOwnedCommand(ctx, command); err != nil || ctx.Err() != nil {
		t.Fatalf("child join: %v / %v", err, ctx.Err())
	}
	resumed, err := lockCapture(capture, false)
	if err != nil {
		t.Fatalf("joined owner retained capture lock: %v", err)
	}
	resumed.Close()
}

type resumeFixture struct {
	plan       planSpec
	old        string
	current    string
	nodes      map[string]stage
	packages   map[string]packageSpec
	checkpoint matrixCheckpoint
}

func newResumeFixture(t *testing.T) resumeFixture {
	t.Helper()
	module := newQualificationModuleAliasFixture(t)
	plan := module.plan
	modules, err := captureModuleSources(module.graph(t), plan.Sources)
	if err != nil {
		t.Fatal(err)
	}
	old, current := t.TempDir(), t.TempDir()
	buildName, suiteName := "build-validator-normal", "suite-normal"
	binary := filepath.Join(old, buildName+".testbin")
	testWrite(t, binary, "synthetic compiled bytes")
	proof, err := regularProof(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(binary+".json", proof); err != nil {
		t.Fatal(err)
	}
	wanted, err := expectedInputs(plan.Suites[0].Outcomes, plan.Suites[0].FailureLiterals)
	if err != nil {
		t.Fatal(err)
	}
	events := []map[string]any{testEvent("start", "", "")}
	for _, root := range wanted.Roots {
		events = append(events, testEvent("run", root, ""), testEvent("pass", root, ""))
	}
	events = append(events, testEvent("output", "", "PASS\n"), testEvent("pass", "", ""))
	for _, event := range events {
		event["Package"] = plan.Packages[0].ImportPath
	}
	encoded := testEncodedEvents(t, events)
	checked, err := verifyEvents(bytes.NewReader(encoded), wanted, plan.Packages[0].ImportPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	stages := map[string]stageReceipt{}
	for _, name := range []string{buildName, suiteName} {
		commands := []string{name, name + "-list", name + "-events"}
		if name == buildName {
			commands = []string{name, name + "-identity", name + "-modules"}
		}
		var body commandResult
		for _, command := range commands {
			base, exit := filepath.Join(old, command), 0
			result := commandResult{Exit: &exit, Joined: true, Stdout: base + ".stdout", Stderr: base + ".stderr", Request: base + ".request.json", OwnerLog: base + ".owner.log"}
			for _, suffix := range []string{".request.json", ".command.json", ".stdout", ".stderr", ".owner.log"} {
				testWrite(t, base+suffix, "")
			}
			testWrite(t, base+".joined", "joined\n")
			if err := writeJSON(base+".result.json", result); err != nil {
				t.Fatal(err)
			}
			if command == name {
				body = result
			}
		}
		result := stageResult{Status: "passed", Command: &body, BinarySHA256: proof.SHA256, BinaryMode: proof.Mode, ModuleSources: modules}
		if name == suiteName {
			result.Verification, result.Events = &checked, filepath.Join(old, name+"-events.stdout")
			testWrite(t, result.Events, string(encoded))
		}
		stages[name], err = retainStage(old, name, result)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, capture := range []string{old, current} {
		testWrite(t, filepath.Join(capture, "runner"), "same synthetic runner")
	}
	runnerProof, err := regularProof(filepath.Join(old, "runner"))
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := matrixCheckpoint{Version: 1, Source: fileProof{SHA256: strings.Repeat("a", 64), Mode: 0600}, Plan: plan, Environment: map[string]string{"same": "environment digest"}, Inputs: map[string]fileProof{filepath.Join(old, "runner"): runnerProof}, Stages: stages}
	if err := writeJSON(filepath.Join(old, "checkpoint.json"), checkpoint); err != nil {
		t.Fatal(err)
	}
	checkpoint.Inputs = map[string]fileProof{filepath.Join(current, "runner"): runnerProof}
	checkpoint.Stages = nil
	nodes := map[string]stage{buildName: {Build: &plan.Suites[0]}, suiteName: {Dependencies: []string{buildName}, Suite: &plan.Suites[0], Binary: buildName}}
	return resumeFixture{plan: plan, old: old, current: current, nodes: nodes, packages: map[string]packageSpec{plan.Packages[0].Id: plan.Packages[0]}, checkpoint: checkpoint}
}

func TestQualificationResumeReplaysEvidenceAndReusesExactBuild(t *testing.T) {
	fixture := newResumeFixture(t)
	results, receipts, err := loadResume(t.Context(), fixture.old, fixture.checkpoint, fixture.nodes, fixture.packages, fixture.current)
	if err != nil || len(results) != 2 || len(receipts) != 2 {
		t.Fatalf("unchanged completed work was not reusable: %d/%d %v", len(results), len(receipts), err)
	}
	proof, err := regularProof(filepath.Join(fixture.current, "build-validator-normal.testbin"))
	if err != nil || proof.SHA256 != results["build-validator-normal"].BinarySHA256 {
		t.Fatalf("resumed compiler bytes changed: %+v %v", proof, err)
	}
	if !strings.HasPrefix(results["suite-normal"].Events, fixture.old+string(filepath.Separator)) {
		t.Fatal("resumption erased original event evidence locator")
	}
}

func TestQualificationResumeRejectsChangedEvidenceAndAuthority(t *testing.T) {
	for _, fault := range []string{"source", "environment", "budget", "runner", "binary", "events", "receipt", "orphan", "summary", "canceled"} {
		fixture := newResumeFixture(t)
		var checkpoint matrixCheckpoint
		if err := readJSON(filepath.Join(fixture.old, "checkpoint.json"), &checkpoint); err != nil {
			t.Fatal(err)
		}
		ctx := t.Context()
		switch fault {
		case "source":
			fixture.checkpoint.Source.SHA256 = strings.Repeat("b", 64)
		case "environment":
			fixture.checkpoint.Environment["same"] = "different"
		case "budget":
			fixture.checkpoint.Plan.Limits.TestSeconds++
		case "runner":
			fixture.checkpoint.Inputs[filepath.Join(fixture.current, "runner")] = fileProof{SHA256: strings.Repeat("b", 64)}
		case "binary":
			testWrite(t, filepath.Join(fixture.old, "build-validator-normal.testbin"), "replacement")
		case "events":
			testWrite(t, filepath.Join(fixture.old, "suite-normal-events.stdout"), "missing roots")
		case "receipt":
			testWrite(t, checkpoint.Stages["suite-normal"].Path, "{}\n")
		case "orphan":
			delete(checkpoint.Stages, "build-validator-normal")
		case "summary":
			ref := checkpoint.Stages["suite-normal"]
			var retained retainedStage
			if err := readJSON(ref.Path, &retained); err != nil {
				t.Fatal(err)
			}
			retained.Result.Command.Joined = false
			if err := writeJSON(ref.Path, retained); err != nil {
				t.Fatal(err)
			}
			ref.Proof, _ = regularProof(ref.Path)
			checkpoint.Stages["suite-normal"] = ref
		case "canceled":
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		if err := writeJSON(filepath.Join(fixture.old, "checkpoint.json"), checkpoint); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadResume(ctx, fixture.old, fixture.checkpoint, fixture.nodes, fixture.packages, fixture.current); err == nil {
			t.Errorf("accepted changed %s", fault)
		}
		if _, err := os.Stat(filepath.Join(fixture.current, "build-validator-normal.testbin")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s copied bytes before all evidence passed: %v", fault, err)
		}
	}
}

func TestQualificationResumeScratchRootsMayChangeWithoutExposingEnvironment(t *testing.T) {
	owner := &stageOwner{Environment: map[string]string{"GOMAXPROCS": "4"}}
	t.Setenv("QUALIFICATION_SYNTHETIC_SECRET", "synthetic-private-value")
	first := resumeEnvironment(owner)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("GOTMPDIR", t.TempDir())
	t.Setenv("TMP", t.TempDir())
	t.Setenv("TEMP", t.TempDir())
	t.Setenv("SN_TEST_COMMAND_ROOT", t.TempDir())
	t.Setenv("CARGO_TARGET_DIR", t.TempDir())
	if second := resumeEnvironment(owner); !reflect.DeepEqual(first, second) {
		t.Fatal("private per-invocation scratch allocation invalidated completed work")
	}
	if strings.Contains(first["QUALIFICATION_SYNTHETIC_SECRET"], "synthetic-private-value") || len(first["QUALIFICATION_SYNTHETIC_SECRET"]) != 64 {
		t.Fatal("resume evidence exposed inherited environment contents")
	}
	t.Setenv("QUALIFICATION_SYNTHETIC_SECRET", "changed-private-value")
	if reflect.DeepEqual(first, resumeEnvironment(owner)) {
		t.Fatal("changed semantic environment was silently reused")
	}
}
