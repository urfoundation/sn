package main

// Recovery tests force stopped, replaced, fault-owned, and canceled transitions
// without a live Docker daemon, chain, or scheduler-dependent waiting.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Calls are serialized by the recovery cycle; callbacks force exact boundaries.
type dependencyRecoveryTestRuntime struct {
	observations map[string]supervisorDependencyObservation
	inspectError map[string]error
	startError   map[string]error
	inspectedIds []string
	startedIds   []string
	afterInspect func(string)
}

// Provides an exact observation or an injected ephemeral daemon failure.
func (self *dependencyRecoveryTestRuntime) inspect(_ context.Context, id string) (supervisorDependencyObservation, error) {
	self.inspectedIds = append(self.inspectedIds, id)
	if self.afterInspect != nil {
		self.afterInspect(id)
	}
	return self.observations[id], self.inspectError[id]
}

// Successful start changes only the lifecycle of the original fixture ID.
func (self *dependencyRecoveryTestRuntime) start(_ context.Context, id string) error {
	self.startedIds = append(self.startedIds, id)
	if err := self.startError[id]; err != nil {
		return err
	}
	observation := self.observations[id]
	observation.running = true
	self.observations[id] = observation
	return nil
}

// Uses generated container identities and never imports a live deployment.
func dependencyRecoveryTestFixture() ([]supervisorDependency, *dependencyRecoveryTestRuntime) {
	dependencies := []supervisorDependency{}
	runtime := &dependencyRecoveryTestRuntime{observations: map[string]supervisorDependencyObservation{}, inspectError: map[string]error{}, startError: map[string]error{}}
	for index, role := range []string{"postgres", "redis"} {
		dependency := supervisorDependency{TargetId: "operator-1-" + role, ContainerId: fmt.Sprintf("%064x", index+1), Name: "example-deployment-" + role, Image: "example-image@sha256:" + strings.Repeat("b", 64), SpecHash: strings.Repeat("c", 64)}
		dependencies = append(dependencies, dependency)
		runtime.observations[dependency.ContainerId] = supervisorDependencyObservation{containerId: dependency.ContainerId, name: dependency.Name, image: dependency.Image, specHash: dependency.SpecHash}
	}
	return dependencies, runtime
}

// A daemon outage affects every dependency; recovery must retry in place while
// still repairing independent containers when just one start remains transient.
func TestSupervisorDependencyRecoveryRetriesAllStoppedContainers(t *testing.T) {
	stateDir := t.TempDir()
	dependencies, runtime := dependencyRecoveryTestFixture()
	unavailable := errors.New("daemon unavailable")
	for _, dependency := range dependencies {
		runtime.inspectError[dependency.ContainerId] = unavailable
	}
	if repaired, err := recoverSupervisorDependencies(context.Background(), stateDir, dependencies, runtime); !errors.Is(err, unavailable) || len(repaired) != 0 || len(runtime.inspectedIds) != len(dependencies) || len(runtime.startedIds) != 0 {
		t.Fatalf("daemon outage: repaired=%v inspected=%v starts=%v err=%v", repaired, runtime.inspectedIds, runtime.startedIds, err)
	}
	runtime.inspectError = map[string]error{}
	runtime.startError[dependencies[0].ContainerId] = unavailable
	if repaired, err := recoverSupervisorDependencies(context.Background(), stateDir, dependencies, runtime); !errors.Is(err, unavailable) || !reflect.DeepEqual(repaired, []string{dependencies[1].TargetId}) {
		t.Fatalf("independent recovery: repaired=%v err=%v", repaired, err)
	}
	runtime.startError = map[string]error{}
	if repaired, err := recoverSupervisorDependencies(context.Background(), stateDir, dependencies, runtime); err != nil || !reflect.DeepEqual(repaired, []string{dependencies[0].TargetId}) {
		t.Fatalf("retry recovery: repaired=%v err=%v", repaired, err)
	}
	starts := append([]string(nil), runtime.startedIds...)
	if repaired, err := recoverSupervisorDependencies(context.Background(), stateDir, dependencies, runtime); err != nil || len(repaired) != 0 || !reflect.DeepEqual(starts, runtime.startedIds) {
		t.Fatalf("healthy containers restarted: repaired=%v starts=%v err=%v", repaired, runtime.startedIds, err)
	}
}

// Neither a same-name replacement nor a foreign image/spec can acquire the
// captured generation's authority, even if Docker returned it for the query.
func TestSupervisorDependencyRecoveryRejectsReplacedOrForeignContainers(t *testing.T) {
	for _, field := range []string{"id", "name", "image", "spec"} {
		dependencies, runtime := dependencyRecoveryTestFixture()
		first := dependencies[0]
		observation := runtime.observations[first.ContainerId]
		switch field {
		case "id":
			observation.containerId = strings.Repeat("f", 64)
		case "name":
			observation.name = "foreign-deployment"
		case "image":
			observation.image = "foreign-image"
		case "spec":
			observation.specHash = strings.Repeat("d", 64)
		}
		runtime.observations[first.ContainerId] = observation
		if repaired, err := recoverSupervisorDependencies(context.Background(), t.TempDir(), dependencies, runtime); err == nil || !strings.Contains(err.Error(), "captured container identity") || !reflect.DeepEqual(repaired, []string{dependencies[1].TargetId}) || !reflect.DeepEqual(runtime.startedIds, []string{dependencies[1].ContainerId}) {
			t.Fatalf("changed %s: repaired=%v starts=%v err=%v", field, repaired, runtime.startedIds, err)
		}
	}
}

// The recovery lock covers the gap between Docker stop and fault-ledger commit.
// It then honors the committed fault until Restore explicitly removes it.
func TestSupervisorDependencyRecoveryPreservesFaultMutationAndLedger(t *testing.T) {
	stateDir := t.TempDir()
	dependencies, runtime := dependencyRecoveryTestFixture()
	unlock, err := lockSupervisorDependencyFault(context.Background(), stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if repaired, err := recoverSupervisorDependencies(context.Background(), stateDir, dependencies, runtime); err != nil || len(repaired) != 0 || len(runtime.inspectedIds) != 0 {
		unlock()
		t.Fatalf("recovery raced in-flight fault: repaired=%v inspections=%v err=%v", repaired, runtime.inspectedIds, err)
	}
	active := activeFaultFile{Schema: "urnetwork-sim-active-faults-v1"}
	fault := scenarioFaultSpec{ID: "deliberate-database-outage", Kind: "container-restart", Targets: []string{dependencies[0].TargetId}}
	path := filepath.Join(stateDir, "active-faults.json")
	if err := appendActiveFault(path, active, fault, []FaultProcessEvidence{{ID: dependencies[0].TargetId, PID: 1234}}); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()
	if repaired, err := recoverSupervisorDependencies(context.Background(), stateDir, dependencies, runtime); err != nil || !reflect.DeepEqual(repaired, []string{dependencies[1].TargetId}) || !reflect.DeepEqual(runtime.inspectedIds, []string{dependencies[1].ContainerId}) {
		t.Fatalf("recovery undid deliberate outage: repaired=%v inspections=%v err=%v", repaired, runtime.inspectedIds, err)
	}
	active, err = readActiveFaultFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := removeActiveFault(path, active, 0); err != nil {
		t.Fatal(err)
	}
	if repaired, err := recoverSupervisorDependencies(context.Background(), stateDir, dependencies, runtime); err != nil || !reflect.DeepEqual(repaired, []string{dependencies[0].TargetId}) {
		t.Fatalf("cleared fault remained stale: repaired=%v err=%v", repaired, err)
	}
}

// An unreadable or incomplete fault ledger never permits an assumed repair.
func TestSupervisorDependencyRecoveryDefersMalformedFaultLedger(t *testing.T) {
	stateDir := t.TempDir()
	dependencies, runtime := dependencyRecoveryTestFixture()
	if err := os.WriteFile(filepath.Join(stateDir, "active-faults.json"), []byte(`{"schema":"urnetwork-sim-active-faults-v1","faults":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverSupervisorDependencies(context.Background(), stateDir, dependencies, runtime); err == nil || len(runtime.inspectedIds) != 0 || len(runtime.startedIds) != 0 {
		t.Fatalf("ambiguous fault state permitted recovery: starts=%v err=%v", runtime.startedIds, err)
	}
}

// Cancellation after observation but before mutation cannot wake a dependency
// after its supervisor owner has shut down.
func TestSupervisorDependencyRecoveryCancellationBeforeStart(t *testing.T) {
	dependencies, runtime := dependencyRecoveryTestFixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.afterInspect = func(string) { cancel() }
	if _, err := recoverSupervisorDependencies(ctx, t.TempDir(), dependencies, runtime); !errors.Is(err, context.Canceled) || len(runtime.startedIds) != 0 {
		t.Fatalf("canceled owner started containers: starts=%v err=%v", runtime.startedIds, err)
	}
	if _, err := lockSupervisorDependencyFault(ctx, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled fault lock: %v", err)
	}
}

// Paused and Docker-owned restarts are observations, not permission to interfere.
func TestSupervisorDependencyRecoveryLeavesPausedAndRestartingContainersAlone(t *testing.T) {
	dependencies, runtime := dependencyRecoveryTestFixture()
	for index, dependency := range dependencies {
		observation := runtime.observations[dependency.ContainerId]
		observation.paused, observation.restarting = index == 0, index == 1
		runtime.observations[dependency.ContainerId] = observation
	}
	if repaired, err := recoverSupervisorDependencies(context.Background(), t.TempDir(), dependencies, runtime); err != nil || len(repaired) != 0 || len(runtime.startedIds) != 0 {
		t.Fatalf("paused/restarting containers mutated: repaired=%v err=%v", repaired, err)
	}
}

// Invalid manifest ownership cannot reach any Docker operation.
func TestSupervisorDependencyRecoveryRejectsMalformedOrDuplicateOwnership(t *testing.T) {
	for _, field := range []string{"empty-id", "nonhex-id", "spec-hash", "duplicate-id", "duplicate-name", "duplicate-target"} {
		dependencies, runtime := dependencyRecoveryTestFixture()
		switch field {
		case "empty-id":
			dependencies[0].ContainerId = ""
		case "nonhex-id":
			dependencies[0].ContainerId = strings.Repeat("g", 64)
		case "spec-hash":
			dependencies[0].SpecHash = ""
		case "duplicate-id":
			dependencies[1].ContainerId = dependencies[0].ContainerId
		case "duplicate-name":
			dependencies[1].Name = dependencies[0].Name
		case "duplicate-target":
			dependencies[1].TargetId = dependencies[0].TargetId
		}
		if _, err := recoverSupervisorDependencies(context.Background(), t.TempDir(), dependencies, runtime); err == nil || len(runtime.inspectedIds) != 0 {
			t.Fatalf("invalid %s reached Docker: inspections=%v err=%v", field, runtime.inspectedIds, err)
		}
	}
}

// Exercise the actual Docker adapter with retained fixture state: only inspect
// and start of the original ID are allowed, never create/remove/name-based start.
func TestSupervisorDependencyRecoveryDockerCommandsRetainContainerId(t *testing.T) {
	stateDir := t.TempDir()
	dependencies, _ := dependencyRecoveryTestFixture()
	dependency := dependencies[0]
	logPath := filepath.Join(stateDir, "docker.log")
	path := filepath.Join(stateDir, "docker")
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$RECOVERY_DOCKER_LOG"
if [ "$1" = container ] && [ "$2" = inspect ] && [ "$5" = "$RECOVERY_CONTAINER_ID" ]; then
  printf '%s\n' "$RECOVERY_INSPECT"
elif [ "$1" = start ] && [ "$2" = "$RECOVERY_CONTAINER_ID" ]; then
  printf '%s\n' "$2"
else
  exit 97
fi
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RECOVERY_DOCKER_LOG", logPath)
	t.Setenv("RECOVERY_CONTAINER_ID", dependency.ContainerId)
	t.Setenv("RECOVERY_INSPECT", strings.Join([]string{dependency.ContainerId, "/" + dependency.Name, dependency.Image, dependency.SpecHash, "no", "false", "false", "false"}, "|"))
	runtime := &dockerSupervisorDependencyRuntime{docker: dockerCLI{Executable: path}}
	if repaired, err := recoverSupervisorDependencies(context.Background(), stateDir, dependencies[:1], runtime); err != nil || !reflect.DeepEqual(repaired, []string{dependency.TargetId}) {
		t.Fatalf("existing generation recovery: repaired=%v err=%v", repaired, err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	commands := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(commands) != 2 || !strings.HasPrefix(commands[0], "container inspect --format ") || commands[1] != "start "+dependency.ContainerId {
		t.Fatalf("recovery commands: %v", commands)
	}
}

// A subprocess helper exposes the Docker protocol without ever contacting a
// daemon. The callback gives the owning test an explicit successful-start edge.
func TestSupervisorDependencyDockerHelper(t *testing.T) {
	if os.Getenv("RECOVERY_DOCKER_HELPER") != "1" {
		return
	}
	arguments := []string{}
	for index, arg := range os.Args {
		if arg == "--" {
			arguments = os.Args[index+1:]
			break
		}
	}
	if len(arguments) >= 1 && arguments[0] == "version" {
		fmt.Println("29.0.0")
		os.Exit(0)
	}
	if len(arguments) == 5 && arguments[0] == "container" && arguments[1] == "inspect" && arguments[4] == os.Getenv("RECOVERY_CONTAINER_ID") {
		fmt.Println(os.Getenv("RECOVERY_INSPECT"))
		os.Exit(0)
	}
	if len(arguments) == 2 && arguments[0] == "start" && arguments[1] == os.Getenv("RECOVERY_CONTAINER_ID") {
		response, err := http.Get(os.Getenv("RECOVERY_STARTED_CALLBACK") + "/" + arguments[1])
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusNoContent {
				fmt.Println(arguments[1])
				os.Exit(0)
			}
		}
	}
	os.Exit(97)
}

// This exercises the real supervisor entry point, Docker adapter and manifest
// so deleting its recovery wiring deterministically loses the expected start.
// No child, data, setup, or funding action participates in the repair.
func TestSupervisorRecoversDependencyWhileKeepingGeneration(t *testing.T) {
	stateDir := t.TempDir()
	dependencies, _ := dependencyRecoveryTestFixture()
	dependency := dependencies[0]
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binaryHash, err := fileSHA256(executable)
	if err != nil {
		t.Fatal(err)
	}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "example-deployment", BinaryHash: binaryHash, Dependencies: dependencies[:1]}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(stateDir, "supervisor.json")
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 1)
	callback := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started <- strings.TrimPrefix(request.URL.Path, "/")
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer callback.Close()
	path := filepath.Join(stateDir, "docker")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\nresult=$(\"$RECOVERY_TEST_BINARY\" -test.run=^TestSupervisorDependencyDockerHelper$ -- \"$@\" 2>/dev/null)\nprintf '%s\\n' \"$result\" | /usr/bin/tail -n 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stateDir)
	t.Setenv("RECOVERY_TEST_BINARY", executable)
	t.Setenv("RECOVERY_DOCKER_HELPER", "1")
	t.Setenv("RECOVERY_CONTAINER_ID", dependency.ContainerId)
	t.Setenv("RECOVERY_INSPECT", strings.Join([]string{dependency.ContainerId, "/" + dependency.Name, dependency.Image, dependency.SpecHash, "no", "false", "false", "false"}, "|"))
	t.Setenv("RECOVERY_STARTED_CALLBACK", callback.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- superviseWithContractCleanup(ctx, stateDir, manifestPath, func(context.Context, string, []ProcessSpec, time.Time) error { return nil })
	}()
	select {
	case id := <-started:
		if id != dependency.ContainerId {
			cancel()
			<-done
			t.Fatalf("supervisor started a different generation: %s", id)
		}
	case err := <-done:
		t.Fatalf("supervisor exited before dependency recovery: %v", err)
	case <-time.After(20 * time.Second):
		cancel()
		<-done
		t.Fatal("supervisor did not recover its stopped dependency")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	state, err := readSupervisorRestartTestState(filepath.Join(stateDir, "supervisor.state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.SupervisorPID != os.Getpid() || state.SupervisorStartTimeTicks != currentProcessStartTimeTicks(t) || len(state.Processes) != 0 {
		t.Fatalf("recovery changed supervisor identity or children: %+v", state)
	}
}

// Runs recovery at the exact mutation boundary before Apply records the fault.
type dependencyFaultRecoveryTestRuntime struct {
	fakeContainerRuntime
	duringMutation func()
}

// The explicit callback forces the unsafe interleaving without scheduling luck.
func (self *dependencyFaultRecoveryTestRuntime) Stop(ctx context.Context, spec managedContainerSpec) (int, error) {
	pid, err := self.fakeContainerRuntime.Stop(ctx, spec)
	self.duringMutation()
	return pid, err
}

// Restore must hold the same lock until its ledger removal has committed.
func (self *dependencyFaultRecoveryTestRuntime) Start(ctx context.Context, spec managedContainerSpec) (int, error) {
	pid, err := self.fakeContainerRuntime.Start(ctx, spec)
	self.duringMutation()
	return pid, err
}

// Exercises actual Apply/Restore wiring, so dropping either mutation lock lets
// the callback inspect a container inside the intentionally protected window.
func TestSupervisorDependencyRecoveryCannotRaceFaultDriver(t *testing.T) {
	stateDir := t.TempDir()
	dependencies, recoveryRuntime := dependencyRecoveryTestFixture()
	mutationChecks := 0
	runtime := &dependencyFaultRecoveryTestRuntime{duringMutation: func() {
		mutationChecks++
		if repaired, err := recoverSupervisorDependencies(context.Background(), stateDir, dependencies, recoveryRuntime); err != nil || len(repaired) != 0 || len(recoveryRuntime.inspectedIds) != 0 {
			t.Fatalf("recovery entered fault mutation: repaired=%v inspected=%v err=%v", repaired, recoveryRuntime.inspectedIds, err)
		}
	}}
	driver := &liveScenarioFaultDriver{stateDir: stateDir, cfg: testResolvedConfig(t), containers: runtime}
	fault := scenarioFaultSpec{ID: "example-database-outage", Kind: "container-restart", Targets: []string{dependencies[0].TargetId}}
	if _, err := driver.Apply(context.Background(), fault); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Restore(context.Background(), fault); err != nil {
		t.Fatal(err)
	}
	if mutationChecks != 2 {
		t.Fatalf("fault mutation checks=%d, want apply and restore", mutationChecks)
	}
}
