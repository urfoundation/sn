// Deterministic control transports force ambiguous replies and cancellation at
// the mutation boundary without relying on network timing or sleeps.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type minerControlTestTransport func(*http.Request) (*http.Response, error)

func (self minerControlTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

// The fixture shares state only through its lock, so explicit transport hooks
// can also coordinate concurrent request tests without introducing fixture races.
type minerControlTestFixture struct {
	driver    *liveScenarioFaultDriver
	fault     scenarioFaultSpec
	stateLock sync.Mutex
	states    map[string]string
	posts     []string
}

func newMinerControlTestFixture(t *testing.T, targets ...string) *minerControlTestFixture {
	t.Helper()
	dir := t.TempDir()
	cfg := testResolvedConfig(t)
	process := ProcessSpec{ID: "miner-swarm-1", Role: "miner-swarm", Identity: "synthetic-miner-members"}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "synthetic-control-test", BinaryHash: "synthetic-binary-hash", Specs: []ProcessSpec{process}}
	hash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), ManifestHash: hash, Processes: []ProcessState{{ID: process.ID, Role: process.Role, Identity: process.Identity, PID: 1234, Healthy: true}}}
	if err := writePublicJSON(filepath.Join(dir, "supervisor.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(dir, "supervisor.state.json"), state); err != nil {
		t.Fatal(err)
	}
	fixture := &minerControlTestFixture{
		fault:  scenarioFaultSpec{ID: "synthetic-cohort", Kind: "miner-control", Targets: targets},
		states: map[string]string{},
		driver: &liveScenarioFaultDriver{
			stateDir: dir, cfg: cfg,
			minerControlURL: func(_ int, target, action string) string {
				return "http://swarm.example/control/" + target + "/" + action
			},
			minerControlWait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
		},
	}
	for _, target := range targets {
		fixture.states[target] = "running"
	}
	fixture.driver.minerControlClient = &http.Client{Transport: minerControlTestTransport(fixture.roundTrip)}
	return fixture
}

func minerControlTestResponse(status int, value any) *http.Response {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}
}

func (self *minerControlTestFixture) roundTrip(request *http.Request) (*http.Response, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if request.Method == http.MethodGet && len(parts) == 3 && parts[2] == "status" {
		state, exists := self.states[parts[1]]
		if !exists {
			return minerControlTestResponse(http.StatusNotFound, "unknown synthetic member"), nil
		}
		return minerControlTestResponse(http.StatusOK, minerControlObservation{Schema: "urnetwork-provider-swarm-member-v1", Id: parts[1], State: state}), nil
	}
	if request.Method == http.MethodPost && len(parts) == 3 {
		self.posts = append(self.posts, request.URL.Path)
		switch parts[2] {
		case "enable":
			self.states[parts[1]] = "running"
		case "disable":
			self.states[parts[1]] = "disabled"
		default:
			return minerControlTestResponse(http.StatusNotFound, "unknown synthetic action"), nil
		}
	}
	status := minerControlSwarmStatus{Schema: "urnetwork-provider-swarm-v1", Configured: self.driver.cfg.Config.Topology.Miners / self.driver.cfg.Config.Topology.MinerSwarmProcesses}
	status.Running = status.Configured
	for id, state := range self.states {
		if state != "running" {
			status.Running--
		}
		if state == "disabled" {
			status.Disabled = append(status.Disabled, id)
		}
	}
	sort.Strings(status.Disabled)
	return minerControlTestResponse(http.StatusOK, status), nil
}

func (self *minerControlTestFixture) apply(t *testing.T) {
	t.Helper()
	if _, err := self.driver.Apply(context.Background(), self.fault); err != nil {
		t.Fatal(err)
	}
	self.stateLock.Lock()
	self.posts = nil
	self.stateLock.Unlock()
}

func (self *minerControlTestFixture) progress(t *testing.T) minerFaultControlProgress {
	t.Helper()
	active, err := readActiveFaultFile(self.driver.activePath())
	if err != nil || len(active.MinerControls) != 1 {
		t.Fatalf("active progress = %+v, %v", active, err)
	}
	return active.MinerControls[0]
}

func (self *minerControlTestFixture) requirePosts(t *testing.T, want ...string) {
	t.Helper()
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if !reflect.DeepEqual(self.posts, want) {
		t.Fatalf("control mutations = %v, want %v", self.posts, want)
	}
}

func TestMinerControlPersistsCompleteIntentBeforeFirstMutation(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1", "miner-2")
	observed := false
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost && !observed {
			progress := fixture.progress(t)
			if progress.Phase != "applying" || progress.Total != 2 || progress.CompletedCount != 0 || progress.Pending != "miner-1" || progress.Attempts != 1 {
				t.Fatalf("mutation preceded its complete durable intent: %+v", progress)
			}
			observed = true
		}
		return fixture.roundTrip(request)
	})
	fixture.apply(t)
	progress := fixture.progress(t)
	if !observed || progress.Phase != "active" || progress.CompletedCount != 2 || progress.Pending != "" {
		t.Fatalf("completed activation = %+v, intent observed=%t", progress, observed)
	}
}

func TestMinerControlCanceledRestoreRetainsPrefixAndReconcilesLostReply(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1", "miner-2")
	fixture.apply(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		response, err := fixture.roundTrip(request)
		if request.Method == http.MethodPost && request.URL.Path == "/control/miner-2/enable" {
			_ = response.Body.Close()
			cancel()
			return nil, context.Canceled
		}
		return response, err
	})
	if _, err := fixture.driver.Restore(ctx, fixture.fault); !errors.Is(err, context.Canceled) {
		t.Fatalf("restore result = %v", err)
	}
	fixture.requirePosts(t, "/control/miner-1/enable", "/control/miner-2/enable")
	progress := fixture.progress(t)
	if progress.Phase != "restoring" || progress.CompletedCount != 1 || progress.Completed[0].ID != "miner-1" || progress.Pending != "miner-2" || progress.LastError == "" {
		t.Fatalf("canceled restore lost its completed prefix: %+v", progress)
	}
	// A new driver has no invocation memory. Live observations complete the
	// ambiguous second target without replaying either successful enable.
	reopened := *fixture.driver
	reopened.minerControlClient = &http.Client{Transport: minerControlTestTransport(fixture.roundTrip)}
	if err := reopened.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.requirePosts(t, "/control/miner-1/enable", "/control/miner-2/enable")
	if _, err := os.Stat(reopened.activePath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful recovery retained ledger: %v", err)
	}
}

func TestMinerControlReconcilesAppliedTimeoutWithoutDuplicateEnable(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.apply(t)
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		response, err := fixture.roundTrip(request)
		if request.Method == http.MethodPost {
			_ = response.Body.Close()
			return nil, context.DeadlineExceeded
		}
		return response, err
	})
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err != nil {
		t.Fatal(err)
	}
	fixture.requirePosts(t, "/control/miner-1/enable")
}

func TestMinerControlRetriesUnappliedTransportFailureWithSameAction(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.apply(t)
	attempts := 0
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			attempts++
			progress := fixture.progress(t)
			if progress.Pending != "miner-1" || progress.Attempts != uint64(attempts) {
				t.Fatalf("retry preceded its durable attempt: %+v", progress)
			}
			if attempts == 1 {
				return nil, io.ErrUnexpectedEOF
			}
		}
		return fixture.roundTrip(request)
	})
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	fixture.requirePosts(t, "/control/miner-1/enable")
}

func TestMinerControlWaitsForAcceptedStartupWithoutReposting(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.apply(t)
	attempts, waits := 0, 0
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			attempts++
			fixture.stateLock.Lock()
			fixture.states["miner-1"] = "starting"
			fixture.stateLock.Unlock()
			return nil, context.DeadlineExceeded
		}
		return fixture.roundTrip(request)
	})
	fixture.driver.minerControlWait = func(ctx context.Context, _ time.Duration) error {
		waits++
		if waits == 2 {
			fixture.stateLock.Lock()
			fixture.states["miner-1"] = "running"
			fixture.stateLock.Unlock()
		}
		return ctx.Err()
	}
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || waits != 2 {
		t.Fatalf("accepted startup attempts/waits = %d/%d, want 1/2", attempts, waits)
	}
}

func TestMinerControlPermanentTimeoutTextDoesNotRetry(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.apply(t)
	attempts := 0
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			attempts++
			return minerControlTestResponse(http.StatusConflict, "synthetic wallet rejection mentioning timeout"), nil
		}
		return fixture.roundTrip(request)
	})
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err == nil || !strings.Contains(err.Error(), "HTTP 409") {
		t.Fatalf("permanent rejection = %v", err)
	}
	if attempts != 1 || fixture.progress(t).CompletedCount != 0 {
		t.Fatalf("permanent rejection retried or completed: attempts=%d", attempts)
	}
}

func TestMinerControlRejectsForeignMemberStatusBeforeMutation(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.apply(t)
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(*http.Request) (*http.Response, error) {
		return minerControlTestResponse(http.StatusOK, minerControlObservation{Schema: "urnetwork-provider-swarm-member-v1", Id: "miner-2", State: "running"}), nil
	})
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err == nil || !strings.Contains(err.Error(), "different identity") {
		t.Fatalf("foreign status result = %v", err)
	}
	fixture.requirePosts(t)
	if fixture.progress(t).CompletedCount != 0 {
		t.Fatal("foreign identity completed restoration")
	}
}

func TestMinerControlMalformedSuccessDoesNotBecomeCompletion(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.apply(t)
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		response, err := fixture.roundTrip(request)
		if request.Method == http.MethodPost {
			_ = response.Body.Close()
			return minerControlTestResponse(http.StatusOK, map[string]string{}), nil
		}
		return response, err
	})
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err == nil {
		t.Fatal("malformed success was admitted")
	}
	fixture.requirePosts(t, "/control/miner-1/enable")
	if fixture.progress(t).CompletedCount != 0 {
		t.Fatal("malformed reply became durable completion")
	}
}

func TestMinerControlInterruptedApplyCannotBeAdoptedAndRestoresAmbiguousTarget(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1", "miner-2")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		response, err := fixture.roundTrip(request)
		if request.Method == http.MethodPost && request.URL.Path == "/control/miner-2/disable" {
			_ = response.Body.Close()
			cancel()
			return nil, context.Canceled
		}
		return response, err
	})
	if _, err := fixture.driver.Apply(ctx, fixture.fault); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted apply = %v", err)
	}
	progress := fixture.progress(t)
	if progress.Phase != "applying" || progress.Total != 2 || progress.CompletedCount != 1 || progress.Pending != "miner-2" {
		t.Fatalf("interrupted apply progress = %+v", progress)
	}
	if _, err := fixture.driver.Apply(context.Background(), fixture.fault); err == nil || !strings.Contains(err.Error(), "unfinished miner control") {
		t.Fatalf("partial activation was adopted: %v", err)
	}
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(fixture.roundTrip)
	if err := fixture.driver.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.requirePosts(t, "/control/miner-1/disable", "/control/miner-2/disable", "/control/miner-1/enable", "/control/miner-2/enable")
}

func TestMinerControlPrevalidatesEveryTargetBeforeMutation(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1", "synthetic-invalid-target")
	if _, err := fixture.driver.Apply(context.Background(), fixture.fault); err == nil {
		t.Fatal("malformed later target was accepted")
	}
	fixture.requirePosts(t)
	if _, err := os.Stat(fixture.driver.activePath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid target created recovery intent: %v", err)
	}
}

func TestMinerControlBoundsTransientAttemptsAndRetainsProgress(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.apply(t)
	attempts := 0
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			attempts++
			return minerControlTestResponse(http.StatusServiceUnavailable, "synthetic transient startup failure"), nil
		}
		return fixture.roundTrip(request)
	})
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err == nil || !strings.Contains(err.Error(), "retry budget exhausted") {
		t.Fatalf("retry exhaustion = %v", err)
	}
	progress := fixture.progress(t)
	if attempts != minerControlMaximumAttempts || progress.Attempts != uint64(attempts) || progress.CompletedCount != 0 || progress.Pending != "miner-1" {
		t.Fatalf("unbounded or lost attempts: calls=%d, progress=%+v", attempts, progress)
	}
}

func TestMinerControlCompletedProgressRequiresFreshStateAfterRestart(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1", "miner-2")
	fixture.apply(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost && request.URL.Path == "/control/miner-2/enable" {
			cancel()
			return nil, context.Canceled
		}
		return fixture.roundTrip(request)
	})
	if _, err := fixture.driver.Restore(ctx, fixture.fault); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if fixture.progress(t).CompletedCount != 1 {
		t.Fatal("missing successful prefix")
	}
	fixture.stateLock.Lock()
	fixture.states["miner-1"] = "disabled"
	fixture.stateLock.Unlock()
	reopened := *fixture.driver
	reopened.minerControlClient = &http.Client{Transport: minerControlTestTransport(fixture.roundTrip)}
	if err := reopened.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.requirePosts(t, "/control/miner-1/enable", "/control/miner-1/enable", "/control/miner-2/enable")
}

func TestMinerControlLegacyReadyStatusReconcilesLostEnableReply(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.apply(t)
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet && request.URL.Path != "/status" {
			return minerControlTestResponse(http.StatusNotFound, "synthetic legacy status endpoint"), nil
		}
		response, err := fixture.roundTrip(request)
		if request.Method == http.MethodPost {
			_ = response.Body.Close()
			return nil, io.ErrUnexpectedEOF
		}
		return response, err
	})
	if _, err := fixture.driver.Restore(context.Background(), fixture.fault); err != nil {
		t.Fatal(err)
	}
	fixture.requirePosts(t, "/control/miner-1/enable")
}

func TestMinerControlLegacyUnreadyPopulationDoesNotProveEnableCompletion(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1")
	fixture.apply(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.driver.minerControlClient.Transport = minerControlTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet && request.URL.Path != "/status" {
			return minerControlTestResponse(http.StatusNotFound, "synthetic legacy status endpoint"), nil
		}
		response, err := fixture.roundTrip(request)
		if request.Method == http.MethodPost {
			_ = response.Body.Close()
			fixture.stateLock.Lock()
			fixture.states["miner-2"] = "starting"
			fixture.stateLock.Unlock()
			return nil, io.ErrUnexpectedEOF
		}
		return response, err
	})
	waits := 0
	fixture.driver.minerControlWait = func(waitCtx context.Context, _ time.Duration) error {
		waits++
		if waits == 2 {
			cancel()
		}
		return waitCtx.Err()
	}
	if _, err := fixture.driver.Restore(ctx, fixture.fault); !errors.Is(err, context.Canceled) {
		t.Fatalf("unready legacy status result = %v", err)
	}
	fixture.requirePosts(t, "/control/miner-1/enable")
	if fixture.progress(t).CompletedCount != 0 {
		t.Fatal("legacy readiness omission certified a completed enable")
	}
}

func TestMinerControlRecoveryTimeoutUsesPendingCohortAndCapsBudget(t *testing.T) {
	fixture := newMinerControlTestFixture(t, "miner-1", "miner-2")
	if timeout := fixture.driver.RecoveryTimeout(); timeout != 30*time.Second {
		t.Fatalf("empty recovery timeout = %s", timeout)
	}
	fixture.apply(t)
	if timeout := fixture.driver.RecoveryTimeout(); timeout != 30*time.Second+2*minerControlTargetTimeout {
		t.Fatalf("active recovery timeout = %s", timeout)
	}
	active, err := readActiveFaultFile(fixture.driver.activePath())
	if err != nil {
		t.Fatal(err)
	}
	active.MinerControls[0].Phase = "restoring"
	active.MinerControls[0].Completed = active.MinerControls[0].Completed[:1]
	active.MinerControls[0].CompletedCount = 1
	if err := writePublicJSON(fixture.driver.activePath(), active); err != nil {
		t.Fatal(err)
	}
	if timeout := fixture.driver.RecoveryTimeout(); timeout != 30*time.Second+minerControlTargetTimeout+2*minerControlRequestTimeout {
		t.Fatalf("partial recovery timeout = %s", timeout)
	}
	large := newMinerControlTestFixture(t, "miner-1", "miner-2", "miner-3", "miner-4", "miner-5", "miner-6", "miner-7", "miner-8", "miner-9", "miner-10")
	large.apply(t)
	if timeout := large.driver.RecoveryTimeout(); timeout != 30*time.Minute {
		t.Fatalf("large recovery timeout = %s", timeout)
	}
}
