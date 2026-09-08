// Build-once, dependency-driven offline qualification with compact status.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// The already-qualified Linux owner remains a temporary migration dependency.
// Only the direct Bash child is signaled; its pidfd/subreaper owner joins all
// descendants before its completion/ACK/wait protocol releases the stage.
const ownerScript = `set -euo pipefail
sn_repo=$1
workspace=$2
export QUALIFICATION_RUNNER=$3 QUALIFICATION_REQUEST=$4
source "$5"
release_gate_jobs_init
qualification_phase() {
  "$QUALIFICATION_RUNNER" worker "$QUALIFICATION_REQUEST"
}
release_gate_start qualification qualification_phase
qualification_status=0
release_gate_complete || qualification_status=$?
if (( release_gate_active == 0 )) && [[ "$release_gate_services_cleaned" == 1 ]]; then
  (set -o noclobber; printf 'joined\n' > "$6") || exit 125
else
  exit 125
fi
exit "$qualification_status"
`

type commandRequest struct {
	Argv        []string          `json:"argv"`
	Directory   string            `json:"directory"`
	Seconds     int               `json:"seconds"`
	Environment map[string]string `json:"environment"`
	Stdin       string            `json:"stdin"`
	Stdout      string            `json:"stdout"`
	Stderr      string            `json:"stderr"`
	Result      string            `json:"result"`
}
type commandResult struct {
	Exit      *int    `json:"exit"`
	Joined    bool    `json:"joined"`
	OwnerExit int     `json:"owner_exit"`
	Seconds   float64 `json:"seconds"`
	Stdout    string  `json:"stdout"`
	Stderr    string  `json:"stderr"`
	Request   string  `json:"request"`
	OwnerLog  string  `json:"owner_log"`
	Error     string  `json:"error,omitempty"`
}
type stageResult struct {
	Status        string              `json:"status"`
	Error         string              `json:"error,omitempty"`
	Command       *commandResult      `json:"command,omitempty"`
	BinarySHA256  string              `json:"binary_sha256,omitempty"`
	BinaryMode    uint32              `json:"binary_mode,omitempty"`
	Seconds       float64             `json:"seconds,omitempty"`
	Verification  *eventSummary       `json:"verification,omitempty"`
	Events        string              `json:"events,omitempty"`
	ModuleSources []moduleSourceProof `json:"module_sources,omitempty"`
}
type stage struct {
	Dependencies  []string
	Build         *suiteSpec
	Suite         *suiteSpec
	Binary        string
	BinarySHA256  string
	BinaryMode    uint32
	ModuleSources []moduleSourceProof
}
type matrixStatus struct {
	State           string   `json:"state"`
	Finished        int      `json:"finished"`
	Total           int      `json:"total"`
	Running         []string `json:"running,omitempty"`
	Pending         int      `json:"pending,omitempty"`
	Failed          []string `json:"failed,omitempty"`
	SourceUnchanged bool     `json:"source_unchanged"`
	Error           string   `json:"error,omitempty"`
	Report          string   `json:"report"`
}
type matrixReport struct {
	Status  matrixStatus           `json:"status"`
	Results map[string]stageResult `json:"results"`
}

// A scheduler failure still cancels and joins every admitted worker before
// returning. No global lock is held across an external command or callback.
func runDAG(ctx context.Context, nodes map[string]stage, jobs int, execute func(context.Context, string, stage) stageResult, publish func(map[string]stageResult, []string, int) error) (map[string]stageResult, error) {
	if jobs < 1 || jobs > 256 || len(nodes) > 2048 || execute == nil || publish == nil {
		return nil, errors.New("bounded jobs, nodes and callbacks required")
	}
	// Validate the entire graph before any independent stage can mutate capture.
	visited := map[string]int{}
	var visit func(string) error
	visit = func(name string) error {
		if visited[name] == 1 {
			return errors.New("cyclic dependency")
		}
		if visited[name] == 2 {
			return nil
		}
		node, ok := nodes[name]
		if !ok {
			return errors.New("missing dependency")
		}
		visited[name] = 1
		for _, dependency := range node.Dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		visited[name] = 2
		return nil
	}
	for name := range nodes {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type completion struct {
		Name   string
		Result stageResult
		Err    error
	}
	complete := make(chan completion, jobs)
	pending := map[string]stage{}
	for name, node := range nodes {
		pending[name] = node
	}
	results := map[string]stageResult{}
	running := map[string]bool{}
	var scheduleErr error
	for len(pending)+len(running) > 0 {
		for _, name := range sortedKeys(pending) {
			node := pending[name]
			blocked, ready := false, true
			for _, dependency := range node.Dependencies {
				result, done := results[dependency]
				ready = ready && done
				blocked = blocked || (done && result.Status != "passed")
			}
			if ctx.Err() != nil || blocked {
				status := "blocked"
				if ctx.Err() != nil {
					status = "canceled"
				}
				results[name] = stageResult{Status: status}
				delete(pending, name)
			} else if ready && len(running) < jobs {
				running[name] = true
				delete(pending, name)
				if node.Binary != "" {
					node.BinarySHA256 = results[node.Binary].BinarySHA256
					node.BinaryMode = results[node.Binary].BinaryMode
					node.ModuleSources = append([]moduleSourceProof(nil), results[node.Binary].ModuleSources...)
				}
				go func() {
					finished := completion{Name: name}
					returned := false
					defer func() {
						recovered := recover()
						if !returned {
							finished.Err = errors.New("stage exited without returning")
							if recovered != nil {
								finished.Err = fmt.Errorf("stage %s panicked: %v", name, recovered)
							}
							finished.Result = stageResult{Status: "failed", Error: finished.Err.Error()}
						}
						complete <- finished
					}()
					finished.Result = execute(ctx, name, node)
					returned = true
					if finished.Result.Status != "passed" && finished.Result.Status != "failed" && finished.Result.Status != "canceled" {
						finished.Result = stageResult{Status: "failed", Error: "stage returned an invalid terminal status"}
					}
				}()
			}
		}
		if scheduleErr == nil {
			if err := publishSnapshot(publish, results, sortedKeys(running), len(pending)); err != nil {
				scheduleErr = err
				cancel()
			}
		}
		if len(running) == 0 {
			if len(pending) != 0 {
				scheduleErr = errors.Join(scheduleErr, errors.New("cycle or missing dependency"))
			}
			break
		}
		var finished completion
		if ctx.Err() != nil {
			finished = <-complete
		} else {
			select {
			case finished = <-complete:
			case <-ctx.Done():
				continue
			}
		}
		delete(running, finished.Name)
		results[finished.Name] = finished.Result
		if finished.Err != nil {
			scheduleErr = errors.Join(scheduleErr, finished.Err)
			cancel()
		}
	}
	return results, scheduleErr
}

// One joined publisher invocation also covers runtime.Goexit, whose defers run
// without a panic. The callback receives no mutable scheduler-owned storage.
func publishSnapshot(publish func(map[string]stageResult, []string, int) error, results map[string]stageResult, running []string, pending int) error {
	snapshot := map[string]stageResult{}
	for name, result := range results {
		result.ModuleSources = append([]moduleSourceProof(nil), result.ModuleSources...)
		if result.Command != nil {
			command := *result.Command
			if command.Exit != nil {
				exit := *command.Exit
				command.Exit = &exit
			}
			result.Command = &command
		}
		if result.Verification != nil {
			verification := *result.Verification
			result.Verification = &verification
		}
		snapshot[name] = result
	}
	finished := make(chan error, 1)
	go func() {
		var err error
		returned := false
		defer func() {
			recovered := recover()
			if !returned {
				err = errors.New("publisher exited without returning")
				if recovered != nil {
					err = fmt.Errorf("publisher panicked: %v", recovered)
				}
			}
			finished <- err
		}()
		err = publish(snapshot, append([]string(nil), running...), pending)
		returned = true
	}()
	return <-finished
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		return exited.ExitCode()
	}
	return 125
}

// Child execution is deliberately outside CommandContext: the timeout's result
// is recorded independently and the enclosing owner proves descendant joins.
func runWorker(path string) int {
	var request commandRequest
	if err := readJSON(path, &request); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 125
	}
	if err := validateRequest(path, request); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 125
	}
	output, err := os.OpenFile(request.Stdout, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 125
	}
	errorsFile, err := os.OpenFile(request.Stderr, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		output.Close()
		return 125
	}
	inputPath := request.Stdin
	if inputPath == "" {
		inputPath = os.DevNull
	}
	input, err := os.Open(inputPath)
	if err != nil {
		output.Close()
		errorsFile.Close()
		return 125
	}
	argv := append([]string{"--signal=TERM", "--kill-after=10s", strconv.Itoa(request.Seconds)}, request.Argv...)
	command := exec.Command("timeout", argv...)
	command.Dir = request.Directory
	environment := executionEnvironment(os.Environ(), request.Environment)
	for _, key := range sortedKeys(environment) {
		command.Env = append(command.Env, key+"="+environment[key])
	}
	capturedOutput, capturedErrors := &boundedCapture{File: output, Remaining: capturedJSONLimit}, &boundedCapture{File: errorsFile, Remaining: capturedJSONLimit}
	command.Stdin, command.Stdout, command.Stderr = input, capturedOutput, capturedErrors
	command.WaitDelay = 10 * time.Second
	started := time.Now()
	runErr := command.Run()
	code := exitCode(runErr)
	result := commandResult{Exit: &code, Seconds: time.Since(started).Seconds()}
	captureErr := errors.Join(capturedOutput.Err, capturedErrors.Err, input.Close(), output.Close(), errorsFile.Close())
	if captureErr != nil {
		result.Error = captureErr.Error()
	}
	if err := writeJSON(request.Result, result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 125
	}
	return code
}

// Requests are private capture-local values, not a general arbitrary-file API.
func validateRequest(path string, request commandRequest) error {
	if len(request.Argv) == 0 || request.Argv[0] == "" || request.Seconds < 1 || request.Seconds > 86400 || request.Environment == nil {
		return errors.New("invalid bounded command request")
	}
	if err := physicalPath(request.Directory, true); err != nil {
		return err
	}
	root := filepath.Dir(path)
	if err := physicalPath(root, true); err != nil {
		return err
	}
	paths := map[string]bool{path: true}
	for _, output := range []string{request.Stdout, request.Stderr, request.Result} {
		if !filepath.IsAbs(output) || filepath.Clean(output) != output || filepath.Dir(output) != root || paths[output] || strings.ContainsAny(output, "\x00\r\n") {
			return errors.New("command output is not an independent capture-local path")
		}
		paths[output] = true
		if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
			return errors.New("command output already exists or cannot be inspected")
		}
	}
	if request.Stdin != "" {
		if paths[request.Stdin] {
			return errors.New("command input aliases an output")
		}
		if err := physicalPath(request.Stdin, false); err != nil {
			return err
		}
	}
	for _, argument := range request.Argv {
		if strings.ContainsRune(argument, '\x00') {
			return errors.New("invalid command argument")
		}
	}
	for key, value := range request.Environment {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, '\x00') {
			return errors.New("invalid command environment")
		}
	}
	return nil
}

// Capture each command stream within the existing 64 MiB event-stream ceiling.
// Separate stdout/stderr owners are each written by one exec copier goroutine.
type boundedCapture struct {
	File      io.Writer
	Remaining int
	Err       error
}

func (self *boundedCapture) Write(data []byte) (int, error) {
	if self.Err != nil {
		return 0, self.Err
	}
	if len(data) > self.Remaining {
		self.Err = errors.New("command output exceeds byte bound")
		return 0, self.Err
	}
	count, err := self.File.Write(data)
	self.Remaining -= count
	if count != len(data) && err == nil {
		err = io.ErrShortWrite
	}
	self.Err = err
	return count, err
}

// Immutable maps support concurrent stage commands. Unproven cleanup is sticky
// and forbids all final source-fence claims, including after cancellation.
type stageOwner struct {
	Source      string
	Capture     string
	Environment map[string]string
	Go          string
	Bash        string
	Inputs      map[string]fileProof
	Tools       map[string]string
	Unproven    atomic.Bool
}

func (self *stageOwner) command(ctx context.Context, name string, argv []string, directory string, seconds int, stdin string) (result commandResult, resultErr error) {
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if self.Unproven.Load() {
		return result, errors.New("an earlier owner did not prove cleanup")
	}
	if err := checkProofs(self.Inputs); err != nil {
		return result, err
	}
	for name, expected := range self.Tools {
		actual, err := executablePath(name)
		if err != nil || actual != expected {
			return result, fmt.Errorf("tool lookup changed: %s", name)
		}
	}
	base := filepath.Join(self.Capture, name)
	request := commandRequest{Argv: argv, Directory: directory, Seconds: seconds, Environment: self.Environment,
		Stdin: stdin, Stdout: base + ".stdout", Stderr: base + ".stderr", Result: base + ".command.json"}
	path := base + ".request.json"
	if err := writeJSON(path, request); err != nil {
		return result, err
	}
	requestProof, err := regularProof(path)
	if err != nil {
		return result, err
	}
	log, err := os.OpenFile(base+".owner.log", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	// The Bash child inherits only this fixed environment plus the private values
	// established by the qualified helper; plan limits are reapplied by the worker.
	bash := self.Bash
	if bash == "" {
		bash = "bash"
	}
	command := exec.Command(bash, "-c", ownerScript, "qualification-owner", self.Source, filepath.Dir(self.Source), filepath.Join(self.Capture, "runner"), path, filepath.Join(self.Capture, "release-gate-jobs.sh"), base+".joined")
	environment := executionEnvironment(os.Environ(), self.Environment)
	for _, key := range sortedKeys(environment) {
		command.Env = append(command.Env, key+"="+environment[key])
	}
	command.Env = append(command.Env, "RELEASE_GATE_JOBS=1")
	command.Stdout, command.Stderr = log, log
	started, proven := false, false
	defer func() {
		if started && !proven {
			self.Unproven.Store(true)
		}
		resultErr = errors.Join(resultErr, log.Close())
	}()
	if err := ctx.Err(); err != nil {
		return commandResult{}, err
	}
	if err := command.Start(); err != nil {
		return commandResult{}, err
	}
	started = true
	joined := make(chan error, 1)
	go func() { joined <- command.Wait() }()
	var waitErr error
	select {
	case waitErr = <-joined:
	case <-ctx.Done():
		signalErr := command.Process.Signal(syscall.SIGTERM)
		waitErr = <-joined
		if signalErr != nil && !errors.Is(signalErr, os.ErrProcessDone) {
			waitErr = errors.Join(waitErr, signalErr)
		}
	}
	readErr := readJSON(request.Result, &result)
	joinedProof, joinedErr := readBounded(base+".joined", int64(len("joined\n")))
	proven = joinedErr == nil && string(joinedProof) == "joined\n"
	if !proven {
		joinedErr = errors.Join(joinedErr, errors.New("owner did not prove every descendant joined"))
	}
	result.Joined = proven
	result.OwnerExit, result.Stdout, result.Stderr = exitCode(waitErr), request.Stdout, request.Stderr
	result.Request, result.OwnerLog = path, base+".owner.log"
	custodyErr := checkProofs(map[string]fileProof{path: requestProof})
	if result.Error != "" {
		readErr = errors.Join(readErr, errors.New(result.Error))
	}
	return result, errors.Join(readErr, joinedErr, custodyErr, ctx.Err(), writeJSON(base+".result.json", result))
}

func commandMatches(result commandResult, expected int) bool {
	return result.Joined && result.Error == "" && result.Exit != nil && *result.Exit == expected && result.OwnerExit == expected
}

// Do not execute inherited shell/Python startup or loader injection before the
// qualified child owner exists. Explicit Go controls override its CPU defaults.
func executionEnvironment(inherited []string, overrides map[string]string) map[string]string {
	result := map[string]string{}
	for _, pair := range inherited {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "BASH_FUNC_") || strings.HasPrefix(key, "PYTHON") {
			continue
		}
		switch key {
		case "BASH_ENV", "ENV", "SHELLOPTS", "BASHOPTS", "LD_PRELOAD", "LD_LIBRARY_PATH":
			continue
		}
		result[key] = value
	}
	for key, value := range overrides {
		result[key] = value
	}
	return result
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, info, err := openRegular(source)
	if err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return errors.Join(err, input.Close())
	}
	count, writeErr := io.Copy(output, io.LimitReader(input, info.Size()+1))
	if count != info.Size() {
		writeErr = errors.Join(writeErr, errors.New("copy source changed size"))
	}
	return errors.Join(writeErr, sameRegular(source, input, info), input.Close(), output.Close())
}

// Compare both mode and content, retaining an explicit error for every mismatch.
func checkProofs(expected map[string]fileProof) error {
	for _, path := range sortedKeys(expected) {
		actual, err := regularProof(path)
		if err != nil {
			return fmt.Errorf("input fence %s: %w", path, err)
		}
		if actual != expected[path] {
			return fmt.Errorf("input fence changed: %s", path)
		}
	}
	return nil
}

func executeStage(ctx context.Context, name string, node stage, owner *stageOwner, plan planSpec, packages map[string]packageSpec) (stageOutcome stageResult) {
	moduleSources := append([]moduleSourceProof(nil), node.ModuleSources...)
	defer func() {
		stageOutcome.ModuleSources = append([]moduleSourceProof(nil), moduleSources...)
		if len(moduleSources) != 0 && !owner.Unproven.Load() {
			if err := checkModuleSourceProofs(moduleSources); err != nil {
				stageOutcome.Status = "failed"
				stageOutcome.Error = shortError(errors.Join(errors.New(stageOutcome.Error), err))
			}
		}
	}()
	failed := func(err error, result *commandResult) stageResult {
		text := "command outcome differs"
		if err != nil {
			text = shortError(err)
		}
		return stageResult{Status: "failed", Error: text, Command: result}
	}
	if node.Build != nil {
		item := node.Build
		pkg := packages[item.Package]
		identity, err := owner.command(ctx, name+"-identity", []string{owner.Go, "list", "-f", "{{.ImportPath}}\n{{.Dir}}", "."}, pkg.Directory, plan.Limits.BuildSeconds, "")
		if err != nil || !commandMatches(identity, 0) {
			return failed(err, &identity)
		}
		actual, err := readBounded(identity.Stdout, metadataLimit)
		if err != nil || string(actual) != pkg.ImportPath+"\n"+pkg.Directory+"\n" {
			return failed(errors.New("actual Go import path or directory differs"), &identity)
		}
		graph, err := owner.command(ctx, name+"-modules", []string{owner.Go, "list", "-m", "-json", "all"}, pkg.Directory, plan.Limits.BuildSeconds, "")
		if err != nil || !commandMatches(graph, 0) {
			return failed(err, &graph)
		}
		graphBytes, err := readBounded(graph.Stdout, sourceManifestLimit)
		if err != nil {
			return failed(err, &graph)
		}
		moduleSources, err = captureModuleSources(graphBytes, plan.Sources)
		if err != nil {
			return failed(err, &graph)
		}
		binary := filepath.Join(owner.Capture, name+".testbin")
		argv := []string{owner.Go, "test", "-c", "-p=" + strconv.Itoa(plan.Limits.Parallel)}
		if item.Mode == "race" {
			argv = append(argv, "-race")
		}
		argv = append(argv, "-o", binary, ".")
		result, err := owner.command(ctx, name, argv, pkg.Directory, plan.Limits.BuildSeconds, "")
		if err != nil || !commandMatches(result, 0) {
			return failed(err, &result)
		}
		proof, err := regularProof(binary)
		if err != nil {
			return failed(err, &result)
		}
		if err := writeJSON(binary+".json", proof); err != nil {
			return failed(err, &result)
		}
		return stageResult{Status: "passed", BinarySHA256: proof.SHA256, BinaryMode: proof.Mode, Seconds: result.Seconds, Command: &result}
	}
	item := node.Suite
	pkg := packages[item.Package]
	if len(moduleSources) == 0 {
		return failed(errors.New("test binary has no parent-owned module source proof"), nil)
	}
	if err := checkModuleSourceProofs(moduleSources); err != nil {
		return failed(err, nil)
	}
	expected, err := expectedInputs(item.Outcomes, item.FailureLiterals)
	if err != nil {
		return failed(err, nil)
	}
	selector := "^(" + strings.Join(expected.Roots, "|") + ")$"
	binary := filepath.Join(owner.Capture, node.Binary+".testbin")
	var proof fileProof
	if err := readJSON(binary+".json", &proof); err != nil {
		return failed(err, nil)
	}
	if node.BinarySHA256 == "" || proof.SHA256 != node.BinarySHA256 || proof.Mode != node.BinaryMode {
		return failed(errors.New("binary proof differs from the parent-owned build result"), nil)
	}
	hash, err := fileHash(binary)
	if err != nil || hash != proof.SHA256 || checkProofs(map[string]fileProof{binary: proof}) != nil {
		return failed(errors.New("test binary changed before execution"), nil)
	}
	listed, err := owner.command(ctx, name+"-list", []string{binary, "-test.list=" + selector}, pkg.Directory, plan.Limits.BuildSeconds, "")
	if err != nil || !commandMatches(listed, 0) {
		return failed(err, &listed)
	}
	actual, err := metadataRows(listed.Stdout, false)
	sort.Strings(actual)
	if err != nil || !reflect.DeepEqual(actual, expected.Roots) {
		return failed(errors.New("compiled root census differs"), &listed)
	}
	if err := checkProofs(map[string]fileProof{binary: proof}); err != nil {
		return failed(err, &listed)
	}
	result, err := owner.command(ctx, name, []string{binary, "-test.run=" + selector, "-test.v=test2json", "-test.count=1", "-test.parallel=" + strconv.Itoa(plan.Limits.Parallel), "-test.timeout=" + strconv.Itoa(plan.Limits.TestSeconds) + "s"}, pkg.Directory, plan.Limits.OuterSeconds, "")
	wanted := 0
	if len(expected.Markers) != 0 {
		wanted = 1
	}
	if err != nil || !commandMatches(result, wanted) {
		return failed(err, &result)
	}
	converted, err := owner.command(ctx, name+"-events", []string{owner.Go, "tool", "test2json", "-t", "-p", pkg.ImportPath}, pkg.Directory, plan.Limits.BuildSeconds, result.Stdout)
	if err != nil || !commandMatches(converted, 0) {
		return failed(err, &converted)
	}
	events, eventInfo, err := openRegular(converted.Stdout)
	if err != nil {
		return failed(err, &result)
	}
	checked, checkErr := verifyEvents(events, expected, pkg.ImportPath, *result.Exit)
	closeErr := errors.Join(sameRegular(converted.Stdout, events, eventInfo), events.Close())
	if err := errors.Join(checkErr, closeErr); err != nil {
		return failed(err, &result)
	}
	hash, err = fileHash(binary)
	if err != nil || hash != proof.SHA256 || checkProofs(map[string]fileProof{binary: proof}) != nil {
		return failed(errors.New("test binary changed during execution"), &result)
	}
	return stageResult{Status: "passed", Seconds: result.Seconds, BinarySHA256: hash, BinaryMode: proof.Mode, Verification: &checked, Events: converted.Stdout, Command: &result}
}

func runMatrix(ctx context.Context, planPath, capture string) (returnedStatus matrixStatus, returnedErr error) {
	planProof, err := regularProof(planPath)
	if err != nil {
		return matrixStatus{}, err
	}
	var plan planSpec
	if err := readJSON(planPath, &plan); err != nil {
		return matrixStatus{}, err
	}
	if err := validatePlan(plan); err != nil {
		return matrixStatus{}, err
	}
	originalInputs := map[string]fileProof{planPath: planProof}
	for _, source := range plan.Sources {
		proof, err := regularProof(source.Manifest)
		if err != nil {
			return matrixStatus{}, err
		}
		originalInputs[source.Manifest] = proof
	}
	for _, suite := range plan.Suites {
		for _, path := range []string{suite.Outcomes, suite.FailureLiterals} {
			proof, err := regularProof(path)
			if err != nil {
				return matrixStatus{}, err
			}
			originalInputs[path] = proof
		}
	}
	if err := checkProofs(originalInputs); err != nil {
		return matrixStatus{}, err
	}
	before, err := sourceFenceContext(ctx, plan.Sources)
	if err != nil {
		return matrixStatus{}, err
	}
	if !filepath.IsAbs(capture) || filepath.Clean(capture) != capture || strings.ContainsAny(capture, "\x00\r\n") {
		return matrixStatus{}, errors.New("capture path must be absolute and canonical")
	}
	if err := physicalPath(filepath.Dir(capture), true); err != nil {
		return matrixStatus{}, err
	}
	for _, source := range plan.Sources {
		if inside(capture, source.Root) {
			return matrixStatus{}, errors.New("capture must not mutate a source tree")
		}
	}
	helperPaths := []string{}
	for _, name := range []string{"release-gate-jobs.sh", "release-gate-child.py"} {
		path := filepath.Join(plan.SourceRoot, "scripts", name)
		if err := physicalPath(path, false); err != nil {
			return matrixStatus{}, err
		}
		helperPaths = append(helperPaths, path)
	}
	// jobs_init sources this sibling helper even when no service is started.
	// It is an actual dependency, not an implicit trusted workspace fallback.
	serviceHelper := filepath.Join(filepath.Dir(plan.SourceRoot), "server", "local", "release-gate-services.sh")
	for _, path := range append(append([]string(nil), helperPaths...), serviceHelper) {
		owned := false
		for _, source := range before {
			if !inside(path, source.Root) {
				continue
			}
			relative, err := filepath.Rel(source.Root, path)
			if err == nil && source.Files[relative].SHA256 != "" {
				owned = true
				originalInputs[path] = source.Files[relative]
			}
		}
		if !owned {
			return matrixStatus{}, fmt.Errorf("ownership adapter is absent from source manifest: %s", path)
		}
	}
	if err := os.Mkdir(capture, 0700); err != nil {
		return matrixStatus{}, err
	}
	status := matrixStatus{State: "running", Report: filepath.Join(capture, "report.json")}
	results := map[string]stageResult{}
	// Every later return, including setup and fence failures, leaves a compact
	// terminal result. Incomplete evidence is retained, never converted to pass.
	defer func() { returnedStatus, returnedErr = finishCapture(capture, status, results, returnedErr) }()
	if err := writeJSON(filepath.Join(capture, "status.json"), status); err != nil {
		return status, err
	}
	executable, err := os.Executable()
	if err != nil {
		return matrixStatus{}, err
	}
	if err := copyFile(executable, filepath.Join(capture, "runner"), 0700); err != nil {
		return matrixStatus{}, err
	}
	for _, path := range helperPaths {
		if err := copyFile(path, filepath.Join(capture, filepath.Base(path)), 0600); err != nil {
			return matrixStatus{}, err
		}
	}
	if err := writeJSON(filepath.Join(capture, "source.before.json"), before); err != nil {
		return matrixStatus{}, err
	}
	for index := range plan.Suites {
		suite := &plan.Suites[index]
		for suffix, source := range map[string]*string{"outcomes": &suite.Outcomes, "literals": &suite.FailureLiterals} {
			destination := filepath.Join(capture, suite.Id+"."+suffix+".tsv")
			if err := copyFile(*source, destination, 0600); err != nil {
				return matrixStatus{}, err
			}
			*source = destination
		}
	}
	if err := writeJSON(filepath.Join(capture, "plan.json"), plan); err != nil {
		return matrixStatus{}, err
	}
	if err := validatePlan(plan); err != nil {
		return status, err
	}
	if err := checkProofs(originalInputs); err != nil {
		return status, err
	}
	inputs := map[string]fileProof{}
	entries, err := os.ReadDir(capture)
	if err != nil {
		return matrixStatus{}, err
	}
	for _, entry := range entries {
		if entry.Name() == "status.json" {
			continue
		}
		path := filepath.Join(capture, entry.Name())
		proof, err := regularProof(path)
		if err != nil {
			return matrixStatus{}, err
		}
		inputs[path] = proof
	}
	if err := writeJSON(filepath.Join(capture, "inputs.json"), inputs); err != nil {
		return matrixStatus{}, err
	}
	inputManifest := filepath.Join(capture, "inputs.json")
	inputManifestProof, err := regularProof(inputManifest)
	if err != nil {
		return status, err
	}
	inputs[inputManifest] = inputManifestProof
	for path, proof := range originalInputs {
		inputs[path] = proof
	}
	tools := map[string]string{}
	for _, name := range []string{"go", "bash", "git", "timeout", "python3", "setsid", "nproc", "stat", "mkfifo", "mktemp", "awk", "ln", "mkdir", "sleep"} {
		path, err := executablePath(name)
		if err != nil {
			return status, err
		}
		proof, err := regularProof(path)
		if err != nil {
			return status, err
		}
		tools[name], inputs[path] = path, proof
	}
	owner := &stageOwner{Source: plan.SourceRoot, Capture: capture, Inputs: inputs, Tools: tools, Go: tools["go"], Bash: tools["bash"], Environment: map[string]string{"PATH": os.Getenv("PATH"), "GOMAXPROCS": strconv.Itoa(plan.Limits.GOMAXPROCS), "GOFLAGS": "-mod=readonly", "GOPROXY": "off", "GOSUMDB": "off", "GONOPROXY": "none", "GONOSUMDB": "none", "GOPRIVATE": "", "GOVCS": "*:off", "GOWORK": "off", "GOENV": "off", "GOTOOLCHAIN": "local", "WARP_TEST_ENV_FAIL_FAST": "1"}}
	toolchain, err := owner.command(ctx, "toolchain", []string{owner.Go, "env", "-json", "GOROOT", "GOTOOLDIR", "GOVERSION", "GOOS", "GOARCH", "CGO_ENABLED", "CC", "CXX", "GOPATH", "GOCACHE", "GOMODCACHE"}, plan.SourceRoot, plan.Limits.BuildSeconds, "")
	if err != nil || !commandMatches(toolchain, 0) {
		return status, errors.Join(err, errors.New("actual Go toolchain discovery failed"))
	}
	var toolchainValues map[string]string
	if err := readJSON(toolchain.Stdout, &toolchainValues); err != nil {
		return status, err
	}
	if toolchainValues["GOVERSION"] == "" {
		return status, errors.New("missing actual Go version")
	}
	toolDirectory := toolchainValues["GOTOOLDIR"]
	if err := physicalPath(toolDirectory, true); err != nil {
		return status, err
	}
	for _, name := range []string{"asm", "compile", "link", "cgo", "pack", "test2json"} {
		path := filepath.Join(toolDirectory, name)
		proof, err := regularProof(path)
		if err != nil {
			return status, err
		}
		inputs[path] = proof
	}
	for _, key := range []string{"GOROOT", "GOOS", "GOARCH", "CGO_ENABLED", "CC", "CXX", "GOPATH", "GOCACHE", "GOMODCACHE"} {
		value, ok := toolchainValues[key]
		if !ok || value == "" {
			return status, fmt.Errorf("missing actual toolchain setting: %s", key)
		}
		owner.Environment[key] = value
	}
	if owner.Environment["CGO_ENABLED"] == "1" {
		for _, key := range []string{"CC", "CXX"} {
			path, err := executablePath(owner.Environment[key])
			if err != nil {
				return status, err
			}
			proof, err := regularProof(path)
			if err != nil {
				return status, err
			}
			inputs[path] = proof
			owner.Environment[key] = path
		}
	} else if owner.Environment["CGO_ENABLED"] != "0" {
		return status, errors.New("invalid actual cgo setting")
	}
	toolchainPath := filepath.Join(capture, "toolchain.json")
	if err := writeJSON(toolchainPath, struct {
		Values map[string]string    `json:"values"`
		Tools  map[string]string    `json:"tools"`
		Files  map[string]fileProof `json:"files"`
	}{Values: toolchainValues, Tools: tools, Files: inputs}); err != nil {
		return status, err
	}
	toolchainProof, err := regularProof(toolchainPath)
	if err != nil {
		return status, err
	}
	inputs[toolchainPath] = toolchainProof
	packages := map[string]packageSpec{}
	for _, pkg := range plan.Packages {
		packages[pkg.Id] = pkg
	}
	nodes := map[string]stage{}
	for index := range plan.Suites {
		suite := &plan.Suites[index]
		build := "build-" + suite.Package + "-" + suite.Mode
		nodes[build] = stage{Build: suite}
		nodes["suite-"+suite.Id] = stage{Dependencies: []string{build}, Suite: suite, Binary: build}
	}
	status.Total = len(nodes)
	publish := func(results map[string]stageResult, running []string, pending int) error {
		status.Finished, status.Running, status.Pending = len(results), running, pending
		status.Failed = nil
		for _, name := range sortedKeys(results) {
			if results[name].Status != "passed" {
				status.Failed = append(status.Failed, name)
			}
		}
		return writeJSON(filepath.Join(capture, "status.json"), status)
	}
	results, runErr := runDAG(ctx, nodes, plan.Limits.Jobs, func(ctx context.Context, name string, node stage) stageResult {
		return executeStage(ctx, name, node, owner, plan, packages)
	}, publish)
	var after []sourceProof
	var fenceErr, afterErr error
	if owner.Unproven.Load() {
		fenceErr = errors.New("source fence forbidden: at least one child owner did not prove cleanup")
	} else {
		after, fenceErr = sourceFenceContext(ctx, plan.Sources)
		afterErr = writeJSON(filepath.Join(capture, "source.after.json"), after)
		if fenceErr == nil && !reflect.DeepEqual(before, after) {
			fenceErr = errors.New("source identity changed during qualification")
		}
		for _, result := range results {
			fenceErr = errors.Join(fenceErr, checkModuleSourceProofs(result.ModuleSources))
		}
		fenceErr = errors.Join(fenceErr, checkProofs(inputs), checkProofs(originalInputs))
	}
	unchanged := fenceErr == nil
	status.State, status.SourceUnchanged, status.Finished, status.Running, status.Pending = "failed", unchanged, len(results), nil, 0
	status.Failed = nil
	for _, name := range sortedKeys(results) {
		if results[name].Status != "passed" {
			status.Failed = append(status.Failed, name)
		}
	}
	finalErr := errors.Join(runErr, fenceErr, afterErr, ctx.Err())
	if len(results) != len(nodes) {
		finalErr = errors.Join(finalErr, errors.New("incomplete terminal stage census"))
	}
	if finalErr == nil && unchanged && len(status.Failed) == 0 {
		status.State = "passed"
	}
	return status, finalErr
}

// The final status is authoritative only after report publication succeeds.
// A failed status publication retains the old status and a separate failure
// artifact; neither the returned value nor process exit may announce success.
func finishCapture(capture string, status matrixStatus, results map[string]stageResult, cause error) (matrixStatus, error) {
	status.Report, status.Finished, status.Running, status.Pending = filepath.Join(capture, "report.json"), len(results), nil, 0
	status.Failed = nil
	failures := map[string]stageResult{}
	for _, name := range sortedKeys(results) {
		if results[name].Status != "passed" {
			status.Failed = append(status.Failed, name)
			failures[name] = results[name]
		}
	}
	if status.State != "passed" || !status.SourceUnchanged || len(status.Failed) != 0 || status.Finished != status.Total {
		cause = errors.Join(cause, errors.New("qualification did not complete with exact passing evidence"))
	}
	record := func(err error) {
		cause = errors.Join(cause, err)
		if cause != nil {
			status.State = "failed"
			status.Error = shortError(cause)
		}
	}
	record(nil)
	record(writeJSON(filepath.Join(capture, "failures.json"), matrixReport{Status: status, Results: failures}))
	record(writeJSON(status.Report, matrixReport{Status: status, Results: results}))
	if err := writeJSON(filepath.Join(capture, "status.json"), status); err != nil {
		record(err)
		record(writeJSON(filepath.Join(capture, "status.failure.json"), status))
	}
	return status, cause
}

// Full command evidence remains in raw captures; compact status stays bounded.
func shortError(err error) string {
	message := err.Error()
	if len(message) > 16*1024 {
		return message[:16*1024] + " [error text truncated; inspect capture]"
	}
	return message
}

// A failed final status publication outranks a retained earlier progress row.
// Neither a malformed failure marker nor an incomplete passed row is ignored.
func readCaptureStatus(capture string) (matrixStatus, error) {
	if err := physicalPath(capture, true); err != nil {
		return matrixStatus{}, err
	}
	var status matrixStatus
	err := readJSON(filepath.Join(capture, "status.failure.json"), &status)
	if err == nil {
		if status.State != "failed" || status.Error == "" {
			return matrixStatus{}, errors.New("invalid terminal publication failure marker")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := readJSON(filepath.Join(capture, "status.json"), &status); err != nil {
			return matrixStatus{}, err
		}
	} else {
		return matrixStatus{}, err
	}
	if status.Report != filepath.Join(capture, "report.json") || status.Total < 0 || status.Total > 2048 || status.Finished < 0 || status.Finished > status.Total || status.Pending < 0 || status.Pending > status.Total {
		return matrixStatus{}, errors.New("invalid capture status identity or census")
	}
	switch status.State {
	case "running":
	case "failed":
		if status.Error == "" {
			return matrixStatus{}, errors.New("failed status has no cause")
		}
	case "passed":
		if !status.SourceUnchanged || status.Finished != status.Total || status.Total == 0 || status.Pending != 0 || len(status.Running) != 0 || len(status.Failed) != 0 || status.Error != "" {
			return matrixStatus{}, errors.New("incomplete passing status")
		}
	default:
		return matrixStatus{}, errors.New("unknown capture status")
	}
	return status, nil
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "worker" {
		os.Exit(runWorker(os.Args[2]))
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var result any
	var err error
	if len(os.Args) == 3 && os.Args[1] == "status" {
		var status matrixStatus
		status, err = readCaptureStatus(os.Args[2])
		result = status
	} else if len(os.Args) == 4 && os.Args[1] == "fence" {
		var proofs []sourceProof
		proofs, err = sourceFenceContext(ctx, []sourceSpec{{Root: os.Args[2], Manifest: os.Args[3]}})
		if err == nil {
			result = struct {
				Status         string `json:"status"`
				Root           string `json:"root"`
				Files          int    `json:"files"`
				ManifestSHA256 string `json:"manifest_sha256"`
			}{Status: "matched", Root: proofs[0].Root, Files: len(proofs[0].Files), ManifestSHA256: proofs[0].ManifestSHA256}
		}
	} else if len(os.Args) == 4 && os.Args[1] == "run" {
		var status matrixStatus
		status, err = runMatrix(ctx, os.Args[2], os.Args[3])
		result = status
		if err == nil && status.State != "passed" {
			err = errors.New("qualification did not pass")
		}
	} else {
		err = errors.New("usage: qualification run PLAN.json NEW_CAPTURE | status CAPTURE | fence SOURCE_ROOT MANIFEST")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if status, ok := result.(matrixStatus); !ok || status.State == "" {
			result = matrixStatus{State: "refused", Error: err.Error()}
		}
	}
	encoded, encodeErr := json.Marshal(result)
	if encodeErr == nil {
		fmt.Println(string(bytes.TrimSpace(encoded)))
	}
	if err != nil || encodeErr != nil {
		os.Exit(1)
	}
}
