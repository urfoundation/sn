package main

// Provisional startup retention tests use the test process as an inert,
// authenticated supervisor generation. They never signal or start a process.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type provisionalStartupRetentionTestFixture struct {
	cfg      *ResolvedConfig
	stateDir string
	manifest SupervisorFile
	service  *SupervisorService
	state    SupervisorState
}

// Builds a durable installed generation with one stopped, recoverable child.
func newProvisionalStartupRetentionTestFixture(t *testing.T) provisionalStartupRetentionTestFixture {
	t.Helper()
	cfg, plan, stateDir, options := provisionalResumeTestContext(t)
	if err := prepareProvisionalResume(t.Context(), cfg, stateDir, "resume", options, plan); err != nil {
		t.Fatal(err)
	}
	binaryHash, err := fileSHA256("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	manifest := SupervisorFile{
		Schema: "urnetwork-sim-supervisor-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, BinaryHash: binaryHash,
		Specs: []ProcessSpec{{ID: "validator-synthetic", Role: "validator", Identity: "synthetic-validator"}},
	}
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	startTimeTicks, err := processStartTimeTicks(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", UpdatedAt: "2030-01-01T00:00:02Z",
		SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: startTimeTicks, ManifestHash: manifestHash,
		ContractCleanupCutoff: "2030-01-01T00:00:00Z",
		Processes: []ProcessState{{
			ID: "validator-synthetic", Role: "validator", Identity: "synthetic-validator", PID: 0,
			StartedAt: "2030-01-01T00:00:01Z", Restarts: 1, Healthy: false, ExitError: "exit status 1",
		}},
	}
	serviceName, err := persistentSupervisorServiceName(manifest.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	service := &SupervisorService{
		Schema: "urnetwork-sim-supervisor-service-v1", Name: serviceName,
		Unit: filepath.Join(stateDir, "synthetic.service"), Binary: "/proc/self/exe", StateDir: stateDir,
	}
	for name, value := range map[string]any{
		"supervisor.json":         manifest,
		"supervisor.service.json": service,
		"supervisor.state.json":   state,
	} {
		if err := writePublicJSON(filepath.Join(stateDir, name), value); err != nil {
			t.Fatal(err)
		}
	}
	return provisionalStartupRetentionTestFixture{cfg: cfg, stateDir: stateDir, manifest: manifest, service: service, state: state}
}

// Returns the nonterminal state used by every successful handoff.
func provisionalStartupRetentionActiveService(context.Context, SupervisorService) (supervisorServiceStatus, error) {
	return supervisorServiceStatus{ActiveState: "active", SubState: "running", Result: "success", ExecMainCode: "0", ExecMainStatus: "0"}, nil
}

// A readiness failure preserves the exact installed generation, leaves the
// stopped child to its supervisor, and remains adoptable by the next resume.
func TestProvisionalStartupRetainsAuthenticatedRecoveringSupervisor(t *testing.T) {
	fixture := newProvisionalStartupRetentionTestFixture(t)
	launchErr := errors.New("supervisor readiness timeout")
	preserved, err := preserveRecoverableProvisionalStartup(
		t.Context(), fixture.cfg, fixture.stateDir, fixture.manifest, fixture.service, launchErr, false,
		liveRecordedSupervisor, provisionalStartupRetentionActiveService,
	)
	if err != nil || !preserved {
		t.Fatalf("recovering provisional supervisor preservation = %t, %v", preserved, err)
	}
	stopCalls := 0
	stop := func(context.Context, string) (map[string]any, error) {
		stopCalls++
		return map[string]any{"on_chain_state_preserved": true}, nil
	}
	if got := cleanupFailedPersistentLaunch(fixture.stateDir, !preserved, launchErr, stop); !errors.Is(got, launchErr) || stopCalls != 0 {
		t.Fatalf("preserved generation cleanup = %v calls=%d", got, stopCalls)
	}
	adoption, err := prepareProvisionalLiveTopology(t.Context(), fixture.cfg, fixture.stateDir, "resume", nil)
	if err != nil || adoption == nil {
		t.Fatalf("next resume cannot authenticate retained generation: %+v %v", adoption, err)
	}
	if adoption.ManifestHash != fixture.state.ManifestHash || adoption.SupervisorPID != fixture.state.SupervisorPID || adoption.SupervisorStartTimeTicks != fixture.state.SupervisorStartTimeTicks || !adoption.Provisional || adoption.FinalAcceptance {
		t.Fatalf("retained generation adoption changed identity or acceptance: %+v", adoption)
	}
}

// Policy boundaries return before inspecting live state, so strict launch,
// cancellation, and a mutation that needs rollback always retain cleanup.
func TestProvisionalStartupRetentionKeepsRollbackBoundaries(t *testing.T) {
	launchErr := errors.New("supervisor readiness timeout")
	type testCase struct {
		name             string
		strict           bool
		cancel           bool
		returnedError    error
		rollbackRequired bool
	}
	cases := []testCase{
		{name: "successful startup", strict: false, returnedError: nil},
		{name: "strict launch", strict: true, returnedError: launchErr},
		{name: "explicit cancellation", cancel: true, returnedError: context.Canceled},
		{name: "expired owner deadline", returnedError: context.DeadlineExceeded},
		{name: "rollback required mutation", returnedError: launchErr, rollbackRequired: true},
	}
	for _, current := range cases {
		fixture := newProvisionalStartupRetentionTestFixture(t)
		if current.strict {
			fixture.cfg.provisionalResume = nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		if current.cancel {
			cancel()
		} else {
			defer cancel()
		}
		probeCalls := 0
		readLive := func(string) (*SupervisorState, error) {
			probeCalls++
			return &fixture.state, nil
		}
		readService := func(context.Context, SupervisorService) (supervisorServiceStatus, error) {
			probeCalls++
			return provisionalStartupRetentionActiveService(t.Context(), *fixture.service)
		}
		preserved, err := preserveRecoverableProvisionalStartup(ctx, fixture.cfg, fixture.stateDir, fixture.manifest, fixture.service, current.returnedError, current.rollbackRequired, readLive, readService)
		if err != nil || preserved || probeCalls != 0 {
			t.Errorf("%s preservation = %t, %v probes=%d", current.name, preserved, err, probeCalls)
		}
	}
}

// Every identity or liveness ambiguity fails closed. The production caller
// consequently leaves its owned-launch cleanup armed.
func TestProvisionalStartupRetentionRejectsUnauthenticatedInstallation(t *testing.T) {
	type testCase struct {
		name   string
		change func(*provisionalStartupRetentionTestFixture, *liveRecordedSupervisorReader, *supervisorServiceStatusReader)
		want   string
	}
	cases := []testCase{
		{
			name: "changed provisional approval", want: "approval identity is incomplete",
			change: func(fixture *provisionalStartupRetentionTestFixture, _ *liveRecordedSupervisorReader, _ *supervisorServiceStatusReader) {
				fixture.cfg.provisionalResume.Record.DeploymentID = "different-synthetic-deployment"
			},
		},
		{
			name: "incomplete service", want: "service identity differs",
			change: func(fixture *provisionalStartupRetentionTestFixture, _ *liveRecordedSupervisorReader, _ *supervisorServiceStatusReader) {
				fixture.service.Unit = "relative.service"
			},
		},
		{
			name: "changed service metadata", want: "service metadata changed",
			change: func(fixture *provisionalStartupRetentionTestFixture, _ *liveRecordedSupervisorReader, _ *supervisorServiceStatusReader) {
				fixture.service.Binary = "/synthetic/replacement"
			},
		},
		{
			name: "changed manifest", want: "manifest changed",
			change: func(fixture *provisionalStartupRetentionTestFixture, _ *liveRecordedSupervisorReader, _ *supervisorServiceStatusReader) {
				fixture.manifest.ProviderStartupWaveSize = 1
			},
		},
		{
			name: "dead supervisor", want: "is not live",
			change: func(_ *provisionalStartupRetentionTestFixture, readLive *liveRecordedSupervisorReader, _ *supervisorServiceStatusReader) {
				*readLive = func(string) (*SupervisorState, error) { return nil, nil }
			},
		},
		{
			name: "reused supervisor pid", want: "start time changed",
			change: func(fixture *provisionalStartupRetentionTestFixture, readLive *liveRecordedSupervisorReader, _ *supervisorServiceStatusReader) {
				fixture.state.SupervisorStartTimeTicks++
				*readLive = func(string) (*SupervisorState, error) { return &fixture.state, nil }
			},
		},
		{
			name: "manifest hash mismatch", want: "installation identity is incomplete",
			change: func(fixture *provisionalStartupRetentionTestFixture, readLive *liveRecordedSupervisorReader, _ *supervisorServiceStatusReader) {
				fixture.state.ManifestHash = "0x" + strings.Repeat("12", 32)
				*readLive = func(string) (*SupervisorState, error) { return &fixture.state, nil }
			},
		},
		{
			name: "incomplete pre-start checkpoint", want: "installation identity is incomplete",
			change: func(fixture *provisionalStartupRetentionTestFixture, readLive *liveRecordedSupervisorReader, _ *supervisorServiceStatusReader) {
				fixture.state.ContractCleanupCutoff = ""
				*readLive = func(string) (*SupervisorState, error) { return &fixture.state, nil }
			},
		},
		{
			name: "changed child identity", want: "differs from the installed manifest",
			change: func(fixture *provisionalStartupRetentionTestFixture, readLive *liveRecordedSupervisorReader, _ *supervisorServiceStatusReader) {
				fixture.state.Processes[0].Identity = "other-synthetic-validator"
				*readLive = func(string) (*SupervisorState, error) { return &fixture.state, nil }
			},
		},
		{
			name: "unrecoverable stopped child", want: "has no recoverable exit",
			change: func(fixture *provisionalStartupRetentionTestFixture, readLive *liveRecordedSupervisorReader, _ *supervisorServiceStatusReader) {
				fixture.state.Processes[0].ExitError = ""
				*readLive = func(string) (*SupervisorState, error) { return &fixture.state, nil }
			},
		},
		{
			name: "terminal service", want: "is terminal",
			change: func(_ *provisionalStartupRetentionTestFixture, _ *liveRecordedSupervisorReader, readService *supervisorServiceStatusReader) {
				*readService = func(context.Context, SupervisorService) (supervisorServiceStatus, error) {
					return supervisorServiceStatus{ActiveState: "failed", SubState: "failed"}, nil
				}
			},
		},
		{
			name: "unobservable service", want: "authenticate installed provisional supervisor service",
			change: func(_ *provisionalStartupRetentionTestFixture, _ *liveRecordedSupervisorReader, readService *supervisorServiceStatusReader) {
				*readService = func(context.Context, SupervisorService) (supervisorServiceStatus, error) {
					return supervisorServiceStatus{}, errors.New("synthetic status failure")
				}
			},
		},
		{
			name: "changed generation during handoff", want: "start time changed",
			change: func(fixture *provisionalStartupRetentionTestFixture, readLive *liveRecordedSupervisorReader, _ *supervisorServiceStatusReader) {
				reads := 0
				*readLive = func(string) (*SupervisorState, error) {
					reads++
					state := fixture.state
					if reads > 1 {
						state.SupervisorStartTimeTicks++
					}
					return &state, nil
				}
			},
		},
	}
	for _, current := range cases {
		fixture := newProvisionalStartupRetentionTestFixture(t)
		readLive := liveRecordedSupervisorReader(liveRecordedSupervisor)
		readService := supervisorServiceStatusReader(provisionalStartupRetentionActiveService)
		current.change(&fixture, &readLive, &readService)
		preserved, err := preserveRecoverableProvisionalStartup(t.Context(), fixture.cfg, fixture.stateDir, fixture.manifest, fixture.service, errors.New("supervisor readiness timeout"), false, readLive, readService)
		if err == nil || preserved || !strings.Contains(err.Error(), current.want) {
			t.Errorf("%s preservation = %t, %v; want %q", current.name, preserved, err, current.want)
		}
	}
}

// Matching on-disk metadata cannot substitute for the running executable
// named by the manifest.
func TestProvisionalStartupRetentionRejectsChangedExecutable(t *testing.T) {
	fixture := newProvisionalStartupRetentionTestFixture(t)
	fixture.manifest.BinaryHash = "sha256:" + strings.Repeat("34", 32)
	manifestHash, err := canonicalHashHex(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	fixture.state.ManifestHash = manifestHash
	if err := writePublicJSON(filepath.Join(fixture.stateDir, "supervisor.json"), fixture.manifest); err != nil {
		t.Fatal(err)
	}
	readLive := func(string) (*SupervisorState, error) { return &fixture.state, nil }
	preserved, err := preserveRecoverableProvisionalStartup(
		t.Context(), fixture.cfg, fixture.stateDir, fixture.manifest, fixture.service, errors.New("supervisor readiness timeout"), false,
		readLive, provisionalStartupRetentionActiveService,
	)
	if err == nil || preserved || !strings.Contains(err.Error(), "executable differs") {
		t.Fatalf("changed executable preservation = %t, %v", preserved, err)
	}
}
