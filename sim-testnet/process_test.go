package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/urnetwork/connect"
	servercontroller "github.com/urnetwork/server/controller"
)

// Returns this test process's exact kernel generation for supervisor fixtures.
func currentProcessStartTimeTicks(t *testing.T) uint64 {
	t.Helper()
	ticks, err := processStartTimeTicks(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	return ticks
}

// Reproduce the launch race by presenting clients before their services in the
// manifest and proving no dependent start crosses the explicit health barrier.
func TestSupervisorStartupWaitsForEveryServiceBeforeClients(t *testing.T) {
	specs := []ProcessSpec{
		{ID: "miner", Role: "miner-swarm", HealthURL: "http://miner/status"},
		{ID: "api", Role: "operator-api", HealthURL: "http://api/status"},
		{ID: "claim", Role: "claim-relayer"},
		{ID: "connect", Role: "operator-connect", HealthURL: "http://connect/status"},
		{ID: "worker", Role: "operator-taskworker"},
		{ID: "rpc", Role: "dependency-rpc-proxy", HealthURL: "http://rpc/healthz"},
		{ID: "validator", Role: "validator"},
	}
	serviceBarrierCrossed := false
	providerBarrierCrossed := false
	events := []string{}
	err := startSupervisorSpecsWithReadiness(
		specs,
		func(spec ProcessSpec) error {
			if supervisorStartupProvider(spec) && !serviceBarrierCrossed {
				return fmt.Errorf("provider %s started before service readiness", spec.ID)
			}
			if !supervisorStartupPrerequisite(spec) && !supervisorStartupProvider(spec) && !providerBarrierCrossed {
				return fmt.Errorf("dependent %s started before provider readiness", spec.ID)
			}
			events = append(events, "start:"+spec.ID)
			return nil
		},
		func(phase []ProcessSpec) error {
			if len(phase) == 0 {
				t.Fatal("empty startup readiness phase")
			}
			if supervisorStartupPrerequisite(phase[0]) {
				for _, spec := range phase {
					if !supervisorStartupPrerequisite(spec) || spec.HealthURL == "" {
						t.Fatalf("invalid prerequisite at barrier: %+v", spec)
					}
				}
				events = append(events, "ready:services")
				serviceBarrierCrossed = true
				return nil
			}
			for _, spec := range phase {
				if !supervisorStartupProvider(spec) || spec.HealthURL == "" || !serviceBarrierCrossed {
					t.Fatalf("invalid provider at barrier: %+v", spec)
				}
			}
			events = append(events, "ready:providers")
			providerBarrierCrossed = true
			return nil
		},
	)
	want := []string{"start:api", "start:connect", "start:rpc", "ready:services", "start:miner", "ready:providers", "start:claim", "start:worker", "start:validator"}
	if err != nil || strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("startup events=%v want=%v error=%v", events, want, err)
	}
}

// A failed or unverifiable service barrier must leave every client and
// background worker unstarted, including adjacent claim and validator roles.
func TestSupervisorStartupReadinessFailureStartsNoDependents(t *testing.T) {
	sentinel := errors.New("API not ready")
	started := []string{}
	specs := []ProcessSpec{
		{ID: "api", Role: "operator-api", HealthURL: "http://api/status"},
		{ID: "miner", Role: "miner-swarm", HealthURL: "http://miner/status"},
		{ID: "claim", Role: "claim-relayer"},
		{ID: "validator", Role: "validator"},
	}
	err := startSupervisorSpecsWithReadiness(
		specs,
		func(spec ProcessSpec) error {
			started = append(started, spec.ID)
			return nil
		},
		func([]ProcessSpec) error { return sentinel },
	)
	if !errors.Is(err, sentinel) || len(started) != 1 || started[0] != "api" {
		t.Fatalf("failed-barrier starts=%v error=%v", started, err)
	}
}

// Reject an incomplete prerequisite declaration before creating even one
// child, so a future service role cannot silently turn the barrier into sleep.
func TestSupervisorStartupRejectsPrerequisiteWithoutHealthEndpoint(t *testing.T) {
	starts := 0
	err := startSupervisorSpecsWithReadiness(
		[]ProcessSpec{{ID: "api", Role: "operator-api"}, {ID: "miner", Role: "miner-swarm"}},
		func(ProcessSpec) error {
			starts++
			return nil
		},
		func([]ProcessSpec) error { return nil },
	)
	if err == nil || !strings.Contains(err.Error(), "api has no health endpoint") || starts != 0 {
		t.Fatalf("missing-health starts=%d error=%v", starts, err)
	}
}

// A disconnected miner population may start after services, but validators
// and relayers cannot cross the second barrier until all provider carriers are
// live. The injected callback makes the failed transition exact and sleep-free.
func TestSupervisorStartupProviderReadinessFailureStartsNoValidators(t *testing.T) {
	sentinel := errors.New("provider carriers are disconnected")
	started := []string{}
	waits := 0
	err := startSupervisorSpecsWithReadiness(
		[]ProcessSpec{
			{ID: "validator", Role: "validator"},
			{ID: "miner", Role: "miner-swarm", HealthURL: "http://miner/status"},
			{ID: "api", Role: "operator-api", HealthURL: "http://api/status"},
			{ID: "claim", Role: "claim-relayer"},
		},
		func(spec ProcessSpec) error {
			started = append(started, spec.ID)
			return nil
		},
		func(phase []ProcessSpec) error {
			waits++
			if supervisorStartupProvider(phase[0]) {
				return sentinel
			}
			return nil
		},
	)
	if !errors.Is(err, sentinel) || waits != 2 || strings.Join(started, ",") != "api,miner" {
		t.Fatalf("failed provider barrier starts=%v waits=%d error=%v", started, waits, err)
	}
}

// Provider health metadata is validated before any child starts, just like
// service prerequisites, so the second barrier can never degrade into delay.
func TestSupervisorStartupRejectsProviderWithoutHealthEndpoint(t *testing.T) {
	starts := 0
	err := startSupervisorSpecsWithReadiness(
		[]ProcessSpec{
			{ID: "api", Role: "operator-api", HealthURL: "http://api/status"},
			{ID: "miner", Role: "miner-swarm"},
		},
		func(ProcessSpec) error {
			starts++
			return nil
		},
		func([]ProcessSpec) error { return nil },
	)
	if err == nil || !strings.Contains(err.Error(), "miner has no health endpoint") || starts != 0 {
		t.Fatalf("missing provider health starts=%d error=%v", starts, err)
	}
}

// The shared readiness primitive also rejects missing health metadata instead
// of silently declaring a partial process set ready.
func TestWaitSpecsReadyRejectsMissingHealthEndpoint(t *testing.T) {
	err := waitSpecsReady(context.Background(), []ProcessSpec{{ID: "validator"}}, 0)
	if err == nil || !strings.Contains(err.Error(), "validator has no health endpoint") {
		t.Fatalf("missing-health readiness error=%v", err)
	}
}

// An HTTP-ready Connect process is not ready when its exact UDP/TLS ingress is
// unavailable. The callback is the transport barrier, so no timeout or
// scheduler ordering participates in this regression.
func TestProcessSpecReadinessRequiresVerifiedH3AfterHTTPHealth(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer health.Close()
	sentinel := errors.New("direct H3 ingress dropped the QUIC Initial")
	called := false
	spec := ProcessSpec{
		ID:                "operator-1-connect",
		HealthURL:         health.URL,
		H3ProbeAddress:    "127.0.1.1:443",
		H3ProbeServerName: "127.0.1.1",
		H3ProbeCAFile:     "/release/connect-ca.crt",
	}
	err := processSpecReadinessWithH3Probe(
		context.Background(),
		&http.Client{},
		spec,
		func(_ context.Context, address, serverName, caFile string) error {
			called = true
			if address != spec.H3ProbeAddress || serverName != spec.H3ProbeServerName || caFile != spec.H3ProbeCAFile {
				t.Fatalf("H3 probe arguments = %q %q %q", address, serverName, caFile)
			}
			return sentinel
		},
	)
	if !called || !errors.Is(err, sentinel) {
		t.Fatalf("HTTP-only readiness called=%t error=%v", called, err)
	}
}

// A partial transport declaration must fail closed instead of degrading back
// to the HTTP-only check that missed the live blackhole.
func TestProcessSpecReadinessRejectsIncompleteH3Identity(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer health.Close()
	err := processSpecReadinessWithH3Probe(
		context.Background(),
		&http.Client{},
		ProcessSpec{ID: "operator-1-connect", HealthURL: health.URL, H3ProbeAddress: "127.0.1.1:443"},
		func(context.Context, string, string, string) error {
			t.Fatal("incomplete H3 identity reached the transport probe")
			return nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "H3 readiness probe is incomplete") {
		t.Fatalf("incomplete H3 readiness error=%v", err)
	}
}

// Exercises the production probe over an actual QUIC socket with the exact
// deterministic CA and IP-SAN identity rendered for simulator clients.
func TestConnectH3ReadinessProbeVerifiesRenderedOperatorIdentity(t *testing.T) {
	cfg := testResolvedConfig(t)
	caCertificatePem, leafCertificatePem, leafPrivateKeyPem, err := operatorConnectTLSArtifacts(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(leafCertificatePem, leafPrivateKeyPem)
	if err != nil {
		t.Fatal(err)
	}
	packetConn, err := net.ListenPacket("udp4", net.JoinHostPort(operatorConnectHostIP(1), "0"))
	if err != nil {
		t.Fatal(err)
	}
	transport := &quic.Transport{Conn: packetConn}
	listener, err := transport.ListenEarly(
		&tls.Config{Certificates: []tls.Certificate{certificate}},
		&quic.Config{
			HandshakeIdleTimeout: 30 * time.Second,
			MaxIdleTimeout:       60 * time.Second,
			KeepAlivePeriod:      15 * time.Second,
			Allow0RTT:            true,
			InitialPacketSize:    connect.H3InitialPacketByteCount,
			EnableDatagrams:      true,
		},
	)
	if err != nil {
		packetConn.Close()
		t.Fatal(err)
	}
	defer listener.Close()
	defer transport.Close()
	caFile := filepath.Join(t.TempDir(), "connect-ca.crt")
	if err := atomicWrite(caFile, caCertificatePem, 0o644); err != nil {
		t.Fatal(err)
	}
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer probeCancel()
	if err := probeConnectH3Readiness(probeCtx, packetConn.LocalAddr().String(), operatorConnectHostIP(1), caFile); err != nil {
		t.Fatal(err)
	}
}

// Reproduces production's SNI callback path instead of installing the
// certificate directly on the listener's outer TLS config.
func TestConnectH3ReadinessProbeVerifiesDynamicOperatorIdentity(t *testing.T) {
	cfg := testResolvedConfig(t)
	caCertificatePem, leafCertificatePem, leafPrivateKeyPem, err := operatorConnectTLSArtifacts(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(leafCertificatePem, leafPrivateKeyPem)
	if err != nil {
		t.Fatal(err)
	}
	packetConn, err := net.ListenPacket("udp4", net.JoinHostPort(operatorConnectHostIP(1), "0"))
	if err != nil {
		t.Fatal(err)
	}
	transport := &quic.Transport{Conn: packetConn}
	listener, err := transport.Listen(
		&tls.Config{GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
			return &tls.Config{Certificates: []tls.Certificate{certificate}}, nil
		}},
		&quic.Config{HandshakeIdleTimeout: time.Second},
	)
	if err != nil {
		packetConn.Close()
		t.Fatal(err)
	}
	defer listener.Close()
	defer transport.Close()
	caFile := filepath.Join(t.TempDir(), "connect-ca.crt")
	if err := atomicWrite(caFile, caCertificatePem, 0o644); err != nil {
		t.Fatal(err)
	}
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer probeCancel()
	if err := probeConnectH3Readiness(probeCtx, packetConn.LocalAddr().String(), operatorConnectHostIP(1), caFile); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorRestartsOncePublishesReadyAndStopsChildren(t *testing.T) {
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binaryHash, err := fileSHA256(executable)
	if err != nil {
		t.Fatal(err)
	}
	counter := filepath.Join(dir, "child.counter")
	readyPath := filepath.Join(dir, "child.ready")
	health := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if _, err := os.Stat(readyPath); err != nil {
			http.Error(writer, "not ready", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer health.Close()
	spec := ProcessSpec{
		ID: "restart-child", Role: "test", Identity: "test-child", Command: "/bin/sh",
		Args: []string{"-c", `if [ ! -e "$SIM_TEST_COUNTER" ]; then printf 'crashed once\n' >"$SIM_TEST_COUNTER"; exit 17; fi; printf 'ready\n' >"$SIM_TEST_READY"; exec /bin/sleep 300`}, WorkDir: dir,
		Env:        map[string]string{"SIM_TEST_COUNTER": counter, "SIM_TEST_READY": readyPath},
		StdoutPath: filepath.Join(dir, "child.stdout"), StderrPath: filepath.Join(dir, "child.stderr"), RestartLimit: 2,
		HealthURL: health.URL,
	}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "test", BinaryHash: binaryHash, Specs: []ProcessSpec{spec}}
	b, _ := json.Marshal(manifest)
	manifestPath := filepath.Join(dir, "supervisor.json")
	if err := os.WriteFile(manifestPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- supervise(ctx, dir, manifestPath) }()
	if _, err := waitSupervisorReady(ctx, dir, manifest, 30*time.Second); err != nil {
		cancel()
		<-done
		t.Fatal(err)
	}
	ready, err := supervisorReadyNow(dir, manifest)
	if err != nil || !ready {
		t.Fatalf("supervisor ready = %v, %v", ready, err)
	}
	stateBytes, err := os.ReadFile(filepath.Join(dir, "supervisor.state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state SupervisorState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Processes) != 1 || state.Processes[0].Restarts != 1 || state.Processes[0].PID <= 1 {
		t.Fatalf("unexpected restart state: %+v", state)
	}
	childPID := state.Processes[0].PID
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && syscall.Kill(childPID, syscall.Signal(0)) == nil {
		time.Sleep(50 * time.Millisecond)
	}
	if syscall.Kill(childPID, syscall.Signal(0)) == nil {
		t.Fatalf("child process %d survived supervisor cancellation", childPID)
	}
}

func TestSupervisorReadinessRejectsDuplicateIdentityAndReportsMalformedState(t *testing.T) {
	dir := t.TempDir()
	want := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "test", BinaryHash: "hash", Specs: []ProcessSpec{
		{ID: "one", Role: "miner", Identity: "miner-1"},
		{ID: "two", Role: "validator", Identity: "validator-1"},
	}}
	manifestHash, err := canonicalHashHex(want)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: currentProcessStartTimeTicks(t), ManifestHash: manifestHash,
		Processes: []ProcessState{
			{ID: "one", Role: "miner", Identity: "miner-1", PID: os.Getpid(), Healthy: true},
			{ID: "one", Role: "miner", Identity: "miner-1", PID: os.Getpid(), Healthy: true},
		},
	}
	encoded, err := json.Marshal(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "supervisor.state.json")
	if err := os.WriteFile(statePath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if ready, err := supervisorReadyNow(dir, want); err != nil || ready {
		t.Fatalf("duplicate process identity readiness = %v, %v", ready, err)
	}
	if err := os.WriteFile(statePath, []byte(`{"schema":`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = waitSupervisorReady(ctx, dir, want, 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "decode supervisor state") {
		t.Fatalf("malformed supervisor readiness error = %v", err)
	}
}

// A process-only health snapshot cannot advance the release boundary. Every
// validator/operator proof domain must move after the launch baseline, and a
// single supervised restart permanently rejects that launch attempt.
func TestReleaseTopologyReadinessRequiresFreshProofsAndRejectsRestart(t *testing.T) {
	specs := []ProcessSpec{
		{ID: "validator-1", Role: "validator", Identity: "first"},
		{ID: "validator-2", Role: "validator", Identity: "second"},
	}
	want := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "test", BinaryHash: "hash", Specs: specs}
	wantHash, err := canonicalHashHex(want)
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: currentProcessStartTimeTicks(t), ManifestHash: wantHash,
		Processes: []ProcessState{
			{ID: "validator-1", Role: "validator", Identity: "first", PID: os.Getpid(), Healthy: true},
			{ID: "validator-2", Role: "validator", Identity: "second", PID: os.Getpid(), Healthy: true},
		},
	}
	supervisorPID := state.SupervisorPID
	supervisorStartTimeTicks := state.SupervisorStartTimeTicks
	baseline := map[string]int{"validator-1/no-1": 4, "validator-1/no-2": 8, "validator-2/no-1": 3, "validator-2/no-2": 7}
	unchanged := map[string]int{"validator-1/no-1": 4, "validator-1/no-2": 8, "validator-2/no-1": 3, "validator-2/no-2": 7}
	if ready, err := releaseTopologyReady(state, wantHash, specs, supervisorPID, supervisorStartTimeTicks, baseline, unchanged); err != nil || ready {
		t.Fatalf("stale proof readiness = %t, %v", ready, err)
	}
	fresh := map[string]int{"validator-1/no-1": 5, "validator-1/no-2": 9, "validator-2/no-1": 4, "validator-2/no-2": 8}
	if ready, err := releaseTopologyReady(state, wantHash, specs, supervisorPID, supervisorStartTimeTicks, baseline, fresh); err != nil || !ready {
		t.Fatalf("fresh proof readiness = %t, %v", ready, err)
	}
	state.Processes[1].Restarts = 1
	if ready, err := releaseTopologyReady(state, wantHash, specs, supervisorPID, supervisorStartTimeTicks, baseline, fresh); err == nil || ready || !strings.Contains(err.Error(), "validator-2 restarted 1") {
		t.Fatalf("restarted topology readiness = %t, %v", ready, err)
	}
	state.Processes[1].Restarts = 0
	state.SupervisorStartTimeTicks++
	if ready, err := releaseTopologyReady(state, wantHash, specs, supervisorPID, supervisorStartTimeTicks, baseline, fresh); err == nil || ready || !strings.Contains(err.Error(), "supervisor generation changed") {
		t.Fatalf("restarted supervisor readiness = %t, %v", ready, err)
	}
}

// Concurrent readers may see an append before its final newline or closing
// brace. Only that final fragment is tolerated; corruption at a durable JSONL
// boundary fails closed.
func releaseProofTestLine(t *testing.T, version int, trailID connect.Id, coverage, completeTimeMS uint64) string {
	t.Helper()
	value := struct {
		Version        int        `json:"v"`
		TrailID        connect.Id `json:"trail_id"`
		Coverage       uint64     `json:"coverage"`
		CompleteTimeMS uint64     `json:"complete_time_ms"`
	}{Version: version, TrailID: trailID, Coverage: coverage, CompleteTimeMS: completeTimeMS}
	line, err := json.Marshal(&value)
	if err != nil {
		t.Fatal(err)
	}
	return string(line)
}

func TestCompletedReleaseProofCountIgnoresOnlyTrailingFragment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proofs.jsonl")
	first, trailing := connect.Id{1}, connect.Id{2}
	contents := strings.Join([]string{
		releaseProofTestLine(t, 1, first, 3, 1),
		strings.TrimSuffix(releaseProofTestLine(t, 1, trailing, 3, 2), "}"),
	}, "\n")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	count, err := completedReleaseProofCount(path)
	if err != nil || count != 1 {
		t.Fatalf("complete release proof count = %d, %v, want 1", count, err)
	}
	unterminatedPath := filepath.Join(t.TempDir(), "proofs.jsonl")
	if err := os.WriteFile(unterminatedPath, []byte(releaseProofTestLine(t, 1, connect.Id{3}, 3, 4)), 0o600); err != nil {
		t.Fatal(err)
	}
	count, err = completedReleaseProofCount(unterminatedPath)
	if err != nil || count != 0 {
		t.Fatalf("unterminated release proof count = %d, %v, want 0", count, err)
	}
}

func TestCompletedReleaseProofCountRejectsDurableCorruption(t *testing.T) {
	trailID := connect.Id{1}
	tests := []struct {
		name     string
		contents string
		want     string
	}{
		{name: "malformed", contents: "{\n", want: "line 1 is malformed"},
		{name: "wrong version", contents: releaseProofTestLine(t, 2, trailID, 3, 1) + "\n", want: "line 1 has an incomplete release identity"},
		{name: "missing trail", contents: releaseProofTestLine(t, 1, connect.Id{}, 3, 1) + "\n", want: "line 1 has an incomplete release identity"},
		{name: "missing coverage", contents: releaseProofTestLine(t, 1, trailID, 0, 1) + "\n", want: "line 1 has an incomplete release identity"},
		{name: "missing completion", contents: releaseProofTestLine(t, 1, trailID, 3, 0) + "\n", want: "line 1 has an incomplete release identity"},
		{name: "duplicate", contents: strings.Join([]string{
			releaseProofTestLine(t, 1, trailID, 3, 1),
			releaseProofTestLine(t, 1, trailID, 3, 2),
			"",
		}, "\n"), want: "line 2 duplicates trail_id"},
	}
	for _, test := range tests {
		path := filepath.Join(t.TempDir(), "proofs.jsonl")
		if err := os.WriteFile(path, []byte(test.contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if count, err := completedReleaseProofCount(path); err == nil || count != 0 || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: complete release proof count = %d, %v, want error containing %q", test.name, count, err, test.want)
		}
	}
}

func TestCurrentSupervisorTreatsAnUnstartedManifestAsNotReady(t *testing.T) {
	dir := t.TempDir()
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "test", BinaryHash: "hash"}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "supervisor.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	ready, err := currentSupervisorReady(dir)
	if err != nil || ready {
		t.Fatalf("unstarted supervisor ready=%t error=%v", ready, err)
	}
}

func TestSupervisorFailsWhenItCannotPublishState(t *testing.T) {
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binaryHash, err := fileSHA256(executable)
	if err != nil {
		t.Fatal(err)
	}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: "test", BinaryHash: binaryHash}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "supervisor.json")
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "supervisor.state.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := supervise(context.Background(), dir, manifestPath); err == nil || !strings.Contains(err.Error(), "publish supervisor state") {
		t.Fatalf("unpublishable supervisor state error = %v", err)
	}
}

func TestSupervisorServiceNamesAndArgumentsAreSafe(t *testing.T) {
	if got := serviceToken("Testnet / Release_1.0"); got != "testnet---release-1-0" {
		t.Fatalf("service token = %q", got)
	}
	for _, value := range []string{"bad\npath", "bad\tpath", "bad\x7fpath"} {
		if _, err := systemdExecArgument(value); err == nil {
			t.Fatalf("systemd argument accepted a control character in %q", value)
		}
	}
	if got, err := systemdExecArgument("/tmp/path with spaces"); err != nil || got != `"/tmp/path with spaces"` {
		t.Fatalf("quoted argument = %q, %v", got, err)
	}
	if got, err := systemdExecArgument(`/tmp/%n/$HOME/"quoted"`); err != nil || got != `"/tmp/%%n/$$HOME/\"quoted\""` {
		t.Fatalf("expansion-safe argument = %q, %v", got, err)
	}
}

func TestPersistentSupervisorRequiresExplicitResumeAfterReboot(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.ConfigPath = "/release path/%n/$HOME/testnet.yml"
	cfg.Config.Deployment.DeploymentID = "Testnet / Release_1.0"
	unit, err := persistentSupervisorUnit(cfg, "/release path/sim-testnet", "/release path/state", "/release path/supervisor.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(unit, "WorkingDirectory=") {
		t.Fatalf("supervisor unit contains a redundant directive with incompatible path quoting:\n%s", unit)
	}
	if !strings.Contains(unit, "Description=UR Network real-testnet simulation testnet---release-1-0") || !strings.Contains(unit, `--config "/release path/%%n/$$HOME/testnet.yml"`) {
		t.Fatalf("supervisor unit did not sanitize metadata and preserve literal path characters:\n%s", unit)
	}
	if strings.Contains(unit, "[Install]") || strings.Contains(unit, "WantedBy=") {
		t.Fatalf("supervisor unit is boot-installable:\n%s", unit)
	}
	actions := persistentSupervisorSystemctlActions("urnetwork-sim-test.service")
	joined := ""
	for _, action := range actions {
		joined += strings.Join(action, " ") + "\n"
	}
	if strings.Contains(joined, "enable") || strings.Contains(joined, "--now") || !strings.Contains(joined, "disable") || !strings.Contains(joined, "start") {
		t.Fatalf("supervisor actions can persist across reboot:\n%s", joined)
	}
}

func TestReleaseHostPreflightRejectsFullDiskAndOccupiedPort(t *testing.T) {
	if err := validateReleaseStateFreeBytes(minimumReleaseStateFreeBytes); err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseStateFreeBytes(minimumReleaseStateFreeBytes - 1); err == nil {
		t.Fatal("undersized release filesystem was accepted")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := validateAvailableListenAddresses([]string{address}); err == nil || !strings.Contains(err.Error(), address) {
		t.Fatalf("occupied simulator listener was accepted: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateAvailableListenAddresses([]string{address}); err != nil {
		t.Fatal(err)
	}
	if err := validateAvailableListenAddresses([]string{address, address}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate simulator listeners were accepted: %v", err)
	}
}

func TestReleaseHostPreflightRejectsOccupiedPacketPort(t *testing.T) {
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.LocalAddr().String()
	if err := validateAvailablePacketListenAddresses([]string{address}); err == nil || !strings.Contains(err.Error(), address) {
		t.Fatalf("occupied simulator packet listener was accepted: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateAvailablePacketListenAddresses([]string{address}); err != nil {
		t.Fatal(err)
	}
	if err := validateAvailablePacketListenAddresses([]string{address, address}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate simulator packet listeners were accepted: %v", err)
	}
}

func TestBindServiceCapabilityCommandScopesOnlyThePrivateConnectBinary(t *testing.T) {
	cases := []struct {
		effectiveUserID int
		sudoPath        string
		setcapPath      string
		binary          string
		want            string
		wantError       bool
	}{
		{effectiveUserID: 0, setcapPath: "/usr/sbin/setcap", binary: "/release/sim-testnet-connect", want: "/usr/sbin/setcap\x00cap_net_bind_service=+ep\x00/release/sim-testnet-connect"},
		{effectiveUserID: 1_000, sudoPath: "/usr/bin/sudo", setcapPath: "/usr/sbin/setcap", binary: "/release/sim-testnet-connect", want: "/usr/bin/sudo\x00-n\x00/usr/sbin/setcap\x00cap_net_bind_service=+ep\x00/release/sim-testnet-connect"},
		{effectiveUserID: 0, setcapPath: "setcap", binary: "/release/sim-testnet-connect", wantError: true},
		{effectiveUserID: 1_000, sudoPath: "sudo", setcapPath: "/usr/sbin/setcap", binary: "/release/sim-testnet-connect", wantError: true},
		{effectiveUserID: 0, setcapPath: "/usr/sbin/setcap", binary: "sim-testnet-connect", wantError: true},
	}
	for _, test := range cases {
		command, err := bindServiceCapabilityCommand(test.effectiveUserID, test.sudoPath, test.setcapPath, test.binary)
		if test.wantError {
			if err == nil {
				t.Errorf("capability command unexpectedly accepted uid=%d sudo=%q setcap=%q binary=%q", test.effectiveUserID, test.sudoPath, test.setcapPath, test.binary)
			}
			continue
		}
		if err != nil || strings.Join(command, "\x00") != test.want {
			t.Errorf("capability command = %q, %v; want %q", command, err, test.want)
		}
	}
	binary := "/release path/sim-testnet-connect"
	if err := validateConnectBindServiceCapability(binary, []byte(binary+" cap_net_bind_service=ep\n")); err != nil {
		t.Fatalf("exact capability readback: %v", err)
	}
	for _, output := range []string{
		"",
		binary + " cap_net_bind_service=p",
		binary + " cap_net_bind_service=ep extra",
		"/other/sim-testnet-connect cap_net_bind_service=ep",
	} {
		if err := validateConnectBindServiceCapability(binary, []byte(output)); err == nil {
			t.Errorf("inexact capability readback accepted: %q", output)
		}
	}
}

func TestPacketListenerProbePreservesEveryExactPrivilegedAddressAndFailure(t *testing.T) {
	addresses := []string{"127.0.1.1:443", "127.0.1.1:53", "127.0.1.2:443", "127.0.1.2:53"}
	wantArguments := "__listener_probe\x00--network=udp\x00--address=127.0.1.1:443\x00--address=127.0.1.1:53\x00--address=127.0.1.2:443\x00--address=127.0.1.2:53"
	called := false
	err := runPacketListenerProbe(context.Background(), "/release/sim-testnet-connect", addresses, func(_ context.Context, binary string, arguments ...string) ([]byte, error) {
		called = true
		if binary != "/release/sim-testnet-connect" || strings.Join(arguments, "\x00") != wantArguments {
			t.Fatalf("probe invocation binary=%q arguments=%q", binary, arguments)
		}
		return nil, nil
	})
	if err != nil || !called {
		t.Fatalf("exact packet probe error=%v called=%t", err, called)
	}

	probeError := errors.New("occupied")
	err = runPacketListenerProbe(context.Background(), "/release/sim-testnet-connect", addresses, func(context.Context, string, ...string) ([]byte, error) {
		return []byte("udp/53 belongs to another process\n"), probeError
	})
	if !errors.Is(err, probeError) || !strings.Contains(err.Error(), "udp/53 belongs") {
		t.Fatalf("packet probe failure = %v", err)
	}
	for _, invalid := range [][]string{nil, {""}, {"127.0.1.1:53", "127.0.1.1:53"}} {
		if _, err := packetListenerProbeArguments(invalid); err == nil {
			t.Errorf("invalid packet addresses accepted: %q", invalid)
		}
	}
	if err := runPacketListenerProbe(context.Background(), "relative", addresses, func(context.Context, string, ...string) ([]byte, error) { return nil, nil }); err == nil {
		t.Fatal("relative capability binary was accepted")
	}
	if err := runPacketListenerProbe(context.Background(), "/release/sim-testnet-connect", addresses, nil); err == nil {
		t.Fatal("nil packet probe runner was accepted")
	}
}

func TestReleaseProcessListenAddressesCoverTopologyWithoutDuplicates(t *testing.T) {
	cfg := testResolvedConfig(t)
	addresses := releaseProcessListenAddresses(cfg)
	want := 6 + (4+operatorConnectExchangePortCount)*cfg.Config.Topology.Operators + cfg.Config.Topology.MinerSwarmProcesses
	if len(addresses) != want {
		t.Fatalf("release listener count=%d, want %d: %v", len(addresses), want, addresses)
	}
	seen := map[string]bool{}
	for _, address := range addresses {
		if seen[address] {
			t.Fatalf("duplicate release listener %s", address)
		}
		seen[address] = true
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			t.Fatalf("release listener is not loopback-confined: %q (%v)", address, err)
		}
	}
}

func TestReleaseProcessPacketListenAddressesCoverTopologyWithoutDuplicates(t *testing.T) {
	cfg := testResolvedConfig(t)
	addresses := releaseProcessPacketListenAddresses(cfg)
	want := 3 * cfg.Config.Topology.Operators
	if len(addresses) != want {
		t.Fatalf("release packet listener count=%d, want %d: %v", len(addresses), want, addresses)
	}
	seenAddresses := map[string]bool{}
	for _, address := range addresses {
		if seenAddresses[address] {
			t.Fatalf("duplicate release packet listener %s", address)
		}
		seenAddresses[address] = true
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			t.Fatalf("release packet listener is not loopback-confined: %q (%v)", address, err)
		}
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		host := operatorConnectHostIP(operator)
		for _, port := range []int{443, 53, 8053} {
			address := net.JoinHostPort(host, fmt.Sprint(port))
			if !seenAddresses[address] {
				t.Errorf("production packet ingress %s is absent from %v", address, addresses)
			}
		}
	}
}

func TestRestartBackoffIsBounded(t *testing.T) {
	if restartBackoff(1) != time.Second || restartBackoff(100) != 30*time.Second {
		t.Fatalf("unexpected restart backoff: %s, %s", restartBackoff(1), restartBackoff(100))
	}
}

func TestProvisioningStartsOnlyOperatorAPIs(t *testing.T) {
	specs := []ProcessSpec{
		{ID: publicEVMEgressProcessID, Role: "dependency-rpc-proxy"},
		{ID: workloadRPCProxyProcessID, Role: "dependency-rpc-proxy"},
		{ID: workloadSubstrateProcessID, Role: "dependency-rpc-proxy"},
		{ID: "operator-1-api", Role: "operator-api"},
		{ID: "operator-1-connect", Role: "operator-connect"},
		{ID: "operator-1-taskworker", Role: "operator-taskworker"},
		{ID: "operator-2-api", Role: "operator-api"},
	}
	got := selectProvisioningServerSpecs(specs)
	if len(got) != 5 || got[0].ID != publicEVMEgressProcessID || got[1].ID != workloadRPCProxyProcessID || got[2].ID != workloadSubstrateProcessID || got[3].ID != "operator-1-api" || got[4].ID != "operator-2-api" {
		t.Fatalf("provisioning specs = %+v", got)
	}
}

func TestInterruptedProvisioningRecoveryStopsOnlyTheRecordedKernelIdentity(t *testing.T) {
	dir := t.TempDir()
	spec := ProcessSpec{
		ID: "temporary-test", Role: "operator-api", Identity: "no:1", Command: "/bin/sleep", Args: []string{"300"}, WorkDir: dir,
		StdoutPath: filepath.Join(dir, "temporary.stdout"), StderrPath: filepath.Join(dir, "temporary.stderr"),
	}
	command, err := startSpec(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL) })
	identity, err := temporaryProcessIdentity(spec, command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	state := TemporaryProcessFile{Schema: temporaryProcessFileSchema, DeploymentID: "test", Processes: []TemporaryProcessIdentity{identity}}
	if err := writeTemporaryProcessFile(dir, state); err != nil {
		t.Fatal(err)
	}
	if err := recoverStaleTemporaryProcesses(context.Background(), dir, "test", []ProcessSpec{spec}); err != nil {
		t.Fatal(err)
	}
	if syscall.Kill(command.Process.Pid, syscall.Signal(0)) == nil {
		t.Fatalf("recorded temporary process %d survived recovery", command.Process.Pid)
	}
	if _, err := os.Stat(temporaryProcessFilePath(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary ownership file survived recovery: %v", err)
	}
}

func TestInterruptedProvisioningRecoveryNeverSignalsAReusedPID(t *testing.T) {
	dir := t.TempDir()
	spec := ProcessSpec{
		ID: "temporary-test", Role: "operator-api", Identity: "no:1", Command: "/bin/sleep", Args: []string{"300"}, WorkDir: dir,
		StdoutPath: filepath.Join(dir, "temporary.stdout"), StderrPath: filepath.Join(dir, "temporary.stderr"),
	}
	command, err := startSpec(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL) })
	identity, err := temporaryProcessIdentity(spec, command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	identity.StartTimeTicks++
	state := TemporaryProcessFile{Schema: temporaryProcessFileSchema, DeploymentID: "test", Processes: []TemporaryProcessIdentity{identity}}
	if err := writeTemporaryProcessFile(dir, state); err != nil {
		t.Fatal(err)
	}
	if err := recoverStaleTemporaryProcesses(context.Background(), dir, "test", []ProcessSpec{spec}); err != nil {
		t.Fatal(err)
	}
	if syscall.Kill(command.Process.Pid, syscall.Signal(0)) != nil {
		t.Fatalf("PID-reuse mismatch process %d was signalled", command.Process.Pid)
	}
	if _, err := os.Stat(temporaryProcessFilePath(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("obsolete ownership file survived PID reuse: %v", err)
	}
}

func TestPostTopologyTournamentSelectionIsExactAndBounded(t *testing.T) {
	plan := &SetupPlan{Actions: []Action{
		{ID: "accounts.provision"},
		{ID: "topology.launch"},
		{ID: "fleet.register.201"},
		{ID: "fleet.commitment.201"},
		{ID: "fleet.mirror.201"},
		{ID: "fleet.bind.201.1"},
		{ID: "churn.tournament-complete"},
		{ID: "precompile.commitment-write"},
	}}
	actions, err := postTopologyTournamentActions(plan)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"fleet.register.201", "fleet.commitment.201", "fleet.mirror.201", "fleet.bind.201.1", "churn.tournament-complete"}
	if len(actions) != len(want) {
		t.Fatalf("selected %d tournament actions, want %d: %+v", len(actions), len(want), actions)
	}
	for i := range want {
		if actions[i].ID != want[i] {
			t.Fatalf("selected action %d=%s, want %s", i, actions[i].ID, want[i])
		}
	}
}

func TestPostTopologyTournamentSelectionRejectsMalformedBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		actions []Action
		want    string
	}{
		{name: "nil plan", want: "unavailable"},
		{name: "missing topology", actions: []Action{{ID: "churn.tournament-complete"}}, want: "topology.launch"},
		{name: "missing barrier", actions: []Action{{ID: "topology.launch"}, {ID: "fleet.register.201"}}, want: "churn.tournament-complete"},
		{name: "unexpected action", actions: []Action{{ID: "topology.launch"}, {ID: "precompile.commitment-write"}, {ID: "churn.tournament-complete"}}, want: "unexpected post-topology action"},
	}
	for _, test := range tests {
		var plan *SetupPlan
		if test.actions != nil {
			plan = &SetupPlan{Actions: test.actions}
		}
		_, err := postTopologyTournamentActions(plan)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: error=%v, want substring %q", test.name, err, test.want)
		}
	}
}

// Proofs completed before the challenger writes cannot certify that the live
// topology survived them. A second proof generation is a mandatory boundary,
// and its failure must remain visible to launch.
func TestPostTopologyTournamentGateRequiresFreshPostTournamentProof(t *testing.T) {
	current := map[string]int{"validator-1/no-1": 4}
	staleProof := errors.New("post-tournament proof did not advance")
	tournamentRan := false
	waited := false
	err := runPostTopologyTournamentGate(
		context.Background(),
		func() (map[string]int, error) {
			return map[string]int{"validator-1/no-1": current["validator-1/no-1"]}, nil
		},
		func(context.Context) error {
			tournamentRan = true
			return nil
		},
		func(_ context.Context, baseline map[string]int) error {
			waited = true
			if current["validator-1/no-1"] <= baseline["validator-1/no-1"] {
				return staleProof
			}
			return nil
		},
	)
	if !errors.Is(err, staleProof) || !tournamentRan || !waited {
		t.Fatalf("stale post-tournament gate error=%v tournament=%t waited=%t", err, tournamentRan, waited)
	}

	tournamentRan = false
	waited = false
	err = runPostTopologyTournamentGate(
		context.Background(),
		func() (map[string]int, error) {
			return map[string]int{"validator-1/no-1": current["validator-1/no-1"]}, nil
		},
		func(context.Context) error {
			tournamentRan = true
			current["validator-1/no-1"]++
			return nil
		},
		func(_ context.Context, baseline map[string]int) error {
			waited = true
			if current["validator-1/no-1"] <= baseline["validator-1/no-1"] {
				return staleProof
			}
			return nil
		},
	)
	if err != nil || !tournamentRan || !waited {
		t.Fatalf("fresh post-tournament gate error=%v tournament=%t waited=%t", err, tournamentRan, waited)
	}
}

// A read failure or challenger-action failure stops the boundary at that exact
// stage; later callbacks cannot hide or replace the root error.
func TestPostTopologyTournamentGatePropagatesBoundaryFailure(t *testing.T) {
	baselineError := errors.New("baseline unavailable")
	tournamentCalled := false
	waitCalled := false
	err := runPostTopologyTournamentGate(
		context.Background(),
		func() (map[string]int, error) { return nil, baselineError },
		func(context.Context) error { tournamentCalled = true; return nil },
		func(context.Context, map[string]int) error { waitCalled = true; return nil },
	)
	if !errors.Is(err, baselineError) || tournamentCalled || waitCalled {
		t.Fatalf("baseline failure error=%v tournament=%t wait=%t", err, tournamentCalled, waitCalled)
	}

	tournamentError := errors.New("challenger action failed")
	tournamentCalled = false
	waitCalled = false
	err = runPostTopologyTournamentGate(
		context.Background(),
		func() (map[string]int, error) { return map[string]int{"validator-1/no-1": 1}, nil },
		func(context.Context) error { tournamentCalled = true; return tournamentError },
		func(context.Context, map[string]int) error { waitCalled = true; return nil },
	)
	if !errors.Is(err, tournamentError) || !tournamentCalled || waitCalled {
		t.Fatalf("tournament failure error=%v tournament=%t wait=%t", err, tournamentCalled, waitCalled)
	}
}

// Cover the release builders as one inventory: listener-bearing services are
// prerequisites, while every worker and client waits behind their readiness.
func TestReleaseProcessInventoryClassifiesEveryStartupDependency(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	serverSpecs, err := buildServerSpecs(cfg, stateDir, map[string]string{
		"sim-testnet":           "/release/sim-testnet",
		connectServerBinaryName: "/release/sim-testnet-connect",
	})
	if err != nil {
		t.Fatal(err)
	}
	clientSpecs := buildClientSpecs(cfg, stateDir, map[string]string{"sim-testnet": "/release/sim-testnet"}, roles)
	prerequisiteCount := 0
	dependentCount := 0
	for _, spec := range append(serverSpecs, clientSpecs...) {
		switch spec.Role {
		case "dependency-rpc-proxy", "operator-api", "operator-connect":
			if !supervisorStartupPrerequisite(spec) || spec.HealthURL == "" {
				t.Fatalf("release prerequisite is not health-gated: %+v", spec)
			}
			prerequisiteCount++
		case "operator-taskworker", "miner-swarm", "claim-relayer", "validator":
			if supervisorStartupPrerequisite(spec) {
				t.Fatalf("release dependent was classified as prerequisite: %+v", spec)
			}
			dependentCount++
		default:
			t.Fatalf("release process role has no startup classification: %+v", spec)
		}
	}
	wantPrerequisites := 3 + 2*cfg.Config.Topology.Operators
	wantDependents := cfg.Config.Topology.Operators + cfg.Config.Topology.MinerSwarmProcesses + cfg.Config.Topology.Operators + cfg.Config.Topology.Validators
	if prerequisiteCount != wantPrerequisites || dependentCount != wantDependents {
		t.Fatalf("release startup prerequisites/dependents=%d/%d, want %d/%d", prerequisiteCount, dependentCount, wantPrerequisites, wantDependents)
	}
}

func TestOperatorConnectSpecsAllocateEveryProductionListener(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	specs, err := buildServerSpecs(cfg, stateDir, map[string]string{
		"sim-testnet":           "/release/sim-testnet",
		connectServerBinaryName: "/release/sim-testnet-connect",
	})
	if err != nil {
		t.Fatal(err)
	}
	parseHostPorts := func(value string) map[int]int {
		hostPorts := map[int]int{}
		for _, portPair := range strings.Split(value, ",") {
			servicePort := 0
			hostPort := 0
			if _, err := fmt.Sscanf(portPair, "%d:%d", &servicePort, &hostPort); err != nil {
				t.Fatalf("invalid WARP_PORTS pair %q: %v", portPair, err)
			}
			if hostPorts[servicePort] != 0 {
				t.Fatalf("duplicate service port %d in %q", servicePort, value)
			}
			hostPorts[servicePort] = hostPort
		}
		return hostPorts
	}
	seenListenAddresses := map[string]string{}
	connectCount := 0
	for _, spec := range specs {
		if spec.Role != "operator-connect" {
			if spec.Role == "operator-api" || spec.Role == "operator-taskworker" {
				port := 0
				if len(spec.Args) != 2 {
					t.Fatalf("%s has invalid server arguments: %v", spec.ID, spec.Args)
				}
				if _, err := fmt.Sscanf(spec.Args[1], "--port=%d", &port); err != nil || port == 0 {
					t.Fatalf("%s has invalid server port: %v", spec.ID, spec.Args)
				}
				if spec.Env["WARP_HOST"] != "127.0.0.1" || spec.Env["WARP_HOST_IPV4"] != "127.0.0.1" || spec.Env["WARP_PORTS"] != fmt.Sprintf("%d:%d", port, port) || spec.HealthURL != fmt.Sprintf("http://127.0.0.1:%d/status", port) {
					t.Fatalf("%s is not directly loopback-confined: env=%+v health=%q", spec.ID, spec.Env, spec.HealthURL)
				}
			}
			continue
		}
		connectCount++
		operator := 0
		if _, err := fmt.Sscanf(spec.ID, "operator-%d-connect", &operator); err != nil || operator < 1 {
			t.Fatalf("invalid connect process identity %q: %v", spec.ID, err)
		}
		statusPort := 19080 + operator
		connectIP := operatorConnectHostIP(operator)
		if spec.Env["WARP_HOST"] != connectIP || spec.Env["WARP_HOST_IPV4"] != connectIP || spec.HealthURL != fmt.Sprintf("http://%s:%d/status", connectIP, statusPort) {
			t.Fatalf("%s is not loopback-confined: env=%+v health=%q", spec.ID, spec.Env, spec.HealthURL)
		}
		if spec.Command != "/release/sim-testnet-connect" || len(spec.Args) != 4 || spec.Args[0] != "__server_connect" || spec.Args[2] != "--tls-default-host="+connectIP || spec.Args[3] != "--direct-h3-loopback" {
			t.Fatalf("%s does not use the capability-scoped binary and exact IP TLS fallback: %+v", spec.ID, spec)
		}
		if spec.H3ProbeAddress != net.JoinHostPort(connectIP, "443") || spec.H3ProbeServerName != connectIP || spec.H3ProbeCAFile != operatorConnectCAFile(stateDir) {
			t.Fatalf("%s does not probe its exact verified H3 ingress: %+v", spec.ID, spec)
		}
		hostPorts := parseHostPorts(spec.Env["WARP_PORTS"])
		wantHostPorts := map[int]int{
			443:        443,
			4053:       53,
			5080:       5080,
			5081:       5081,
			8053:       8053,
			statusPort: statusPort,
		}
		if len(hostPorts) != len(wantHostPorts) {
			t.Fatalf("%s WARP_PORTS=%q parsed=%v, want %v", spec.ID, spec.Env["WARP_PORTS"], hostPorts, wantHostPorts)
		}
		for servicePort, wantHostPort := range wantHostPorts {
			if hostPorts[servicePort] != wantHostPort {
				t.Fatalf("%s service port %d maps to %d, want %d", spec.ID, servicePort, hostPorts[servicePort], wantHostPort)
			}
			listenAddress := net.JoinHostPort(connectIP, fmt.Sprint(wantHostPort))
			if owner := seenListenAddresses[listenAddress]; owner != "" {
				t.Fatalf("connect listener %s is shared by %s and %s", listenAddress, owner, spec.ID)
			}
			seenListenAddresses[listenAddress] = spec.ID
		}
		for _, ingress := range []struct{ servicePort, publicPort int }{{443, 443}, {4053, 53}, {8053, 8053}} {
			advertised := net.JoinHostPort(spec.Env["WARP_HOST"], fmt.Sprint(ingress.publicPort))
			bound := net.JoinHostPort(spec.Env["WARP_HOST_IPV4"], fmt.Sprint(hostPorts[ingress.servicePort]))
			if advertised != bound {
				t.Fatalf("%s advertises unreachable %s but binds %s", spec.ID, advertised, bound)
			}
		}
	}
	if connectCount != cfg.Config.Topology.Operators {
		t.Fatalf("connect process count=%d, want %d", connectCount, cfg.Config.Topology.Operators)
	}
}

func TestServerSpecsRouteWorkloadsThroughSimulatorOwnedRPCProxy(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.OperationalEVM = "https://evm-rpc.example"
	cfg.Public.Chain.EVMPublicReadEndpoint = cfg.OperationalEVM
	cfg.OperationalRPCMode = rpcModePublicOverride
	cfg.OperationalSubstrate = "wss://substrate-rpc.example:443"
	specs, err := buildServerSpecs(cfg, t.TempDir(), map[string]string{
		"sim-testnet":           "/release/sim-testnet",
		connectServerBinaryName: "/release/sim-testnet-connect",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) < 3 || specs[0].ID != publicEVMEgressProcessID || specs[0].HealthURL != "http://"+publicEVMEgressHealthAddress+"/healthz" || specs[1].ID != workloadRPCProxyProcessID || specs[1].HealthURL != "http://"+workloadRPCProxyHealthAddress+"/healthz" || specs[2].ID != workloadSubstrateProcessID || specs[2].HealthURL != "http://"+workloadSubstrateHealthAddress+"/healthz" {
		t.Fatalf("missing workload RPC proxy spec: %+v", specs)
	}
	egressArgs := strings.Join(specs[0].Args, " ")
	workloadArgs := strings.Join(specs[1].Args, " ")
	substrateArgs := strings.Join(specs[2].Args, " ")
	if !strings.Contains(egressArgs, "--upstream=evm-rpc.example:443") || !strings.Contains(egressArgs, "--tls-server-name=evm-rpc.example") || !strings.Contains(egressArgs, "--http") || !strings.Contains(egressArgs, "--maximum-requests-per-minute=40") || !strings.Contains(workloadArgs, "--upstream="+campaignEVMAuthority()) || !strings.Contains(workloadArgs, "--http") || strings.Contains(workloadArgs, "--tls-server-name") || strings.Contains(workloadArgs, "--maximum-requests-per-minute") || !strings.Contains(substrateArgs, "--upstream=substrate-rpc.example:443") || !strings.Contains(substrateArgs, "--tls-server-name=substrate-rpc.example") || strings.Contains(substrateArgs, "--http") || strings.Contains(substrateArgs, "--maximum-requests-per-minute") {
		t.Fatalf("RPC proxies lost protocol-specific quota/TLS chaining: egress=%q workload=%q substrate=%q", egressArgs, workloadArgs, substrateArgs)
	}
	for _, spec := range specs[3:] {
		if spec.Env["BRINGYOUR_SUBTENSOR_HOSTNAME"] != workloadRPCAuthority() {
			t.Fatalf("%s bypasses workload RPC proxy: %q", spec.ID, spec.Env["BRINGYOUR_SUBTENSOR_HOSTNAME"])
		}
		switch spec.Role {
		case "operator-api":
			if spec.Command != "/release/sim-testnet" || len(spec.Args) != 2 || spec.Args[0] != "__server_api" {
				t.Fatalf("%s bypasses the production API module runner: %+v", spec.ID, spec)
			}
			var operator int
			if _, err := fmt.Sscanf(spec.ID, "operator-%d-api", &operator); err != nil || spec.Env[servercontroller.VerifySimulationAssignmentFilterFileEnv] != verifyAssignmentFilterPath(filepath.Dir(filepath.Dir(spec.StdoutPath)), operator) {
				t.Fatalf("%s has no exact validator-view fault input: %+v", spec.ID, spec.Env)
			}
		case "operator-connect":
			if _, ok := spec.Env[servercontroller.VerifySimulationAssignmentFilterFileEnv]; ok {
				t.Fatalf("%s received API-only validator-view fault authority", spec.ID)
			}
			if spec.Command != "/release/sim-testnet-connect" || len(spec.Args) != 4 || spec.Args[0] != "__server_connect" || !strings.HasPrefix(spec.Args[2], "--tls-default-host=127.0.1.") || spec.Args[3] != "--direct-h3-loopback" || spec.H3ProbeAddress == "" || spec.H3ProbeServerName == "" || spec.H3ProbeCAFile == "" {
				t.Fatalf("%s bypasses the production connect module runner: %+v", spec.ID, spec)
			}
		case "operator-taskworker":
			if _, ok := spec.Env[servercontroller.VerifySimulationAssignmentFilterFileEnv]; ok {
				t.Fatalf("%s received API-only validator-view fault authority", spec.ID)
			}
			if spec.Command != "/release/sim-testnet" || len(spec.Args) != 2 || spec.Args[0] != "__server_taskworker" {
				t.Fatalf("%s bypasses the production taskworker module runner: %+v", spec.ID, spec)
			}
		}
	}
}

func TestClientSpecsUseProductionModuleSwarmsAndDistinctPrefixes(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	specs := buildClientSpecs(cfg, stateDir, map[string]string{"sim-testnet": "/bin/sim-testnet"}, roles)
	providerSwarms := 0
	claimSwarms := 0
	validators := 0
	wantCAFile := operatorConnectCAFile(stateDir)
	if !filepath.IsAbs(wantCAFile) {
		t.Fatalf("operator Connect CA path is not absolute: %q", wantCAFile)
	}
	for _, spec := range specs {
		switch spec.Role {
		case "miner-swarm":
			providerSwarms++
			if spec.Command != "/bin/sim-testnet" || len(spec.Args) != 2 || spec.Args[0] != "__miner_swarm" {
				t.Fatalf("provider swarm %s bypasses the production miner module wrapper: %+v", spec.ID, spec)
			}
			if spec.Env[connect.ExtraRootCAFileEnv] != wantCAFile {
				t.Fatalf("provider swarm %s private transport root = %q, want %q", spec.ID, spec.Env[connect.ExtraRootCAFileEnv], wantCAFile)
			}
		case "claim-relayer":
			claimSwarms++
			if spec.Command != "/bin/sim-testnet" || len(spec.Args) != 2 || spec.Args[0] != "__claim_swarm" {
				t.Fatalf("claim swarm %s bypasses the production miner module wrapper: %+v", spec.ID, spec)
			}
			if spec.Env[connect.ExtraRootCAFileEnv] != "" {
				t.Fatalf("claim swarm %s unnecessarily inherited the private transport root", spec.ID)
			}
		case "validator":
			validators++
			if spec.Env[connect.ExtraRootCAFileEnv] != wantCAFile {
				t.Fatalf("validator %s private transport root = %q, want %q", spec.ID, spec.Env[connect.ExtraRootCAFileEnv], wantCAFile)
			}
		}
	}
	if providerSwarms != cfg.Config.Topology.MinerSwarmProcesses || claimSwarms != cfg.Config.Topology.Operators || validators != cfg.Config.Topology.Validators {
		t.Fatalf("provider/claim/validator count = %d/%d/%d, want %d/%d/%d", providerSwarms, claimSwarms, validators, cfg.Config.Topology.MinerSwarmProcesses, cfg.Config.Topology.Operators, cfg.Config.Topology.Validators)
	}
	sources := map[string]bool{}
	prefixes := map[netip.Prefix][]int{}
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		source := minerTestEgressSourceIP(miner)
		addr, err := netip.ParseAddr(source)
		if err != nil || !addr.Is4() || !addr.IsLoopback() || sources[source] {
			t.Fatalf("miner-%d source identity = %q, err=%v duplicate=%t", miner, source, err, sources[source])
		}
		sources[source] = true
		prefix := netip.PrefixFrom(addr, cfg.Policy.Verify.EgressIPv4Prefix).Masked()
		prefixes[prefix] = append(prefixes[prefix], miner)
	}
	wantPrefixes := cfg.Config.Topology.Miners - 3
	if len(sources) != cfg.Config.Topology.Miners || len(prefixes) != wantPrefixes {
		t.Fatalf("source/prefix count = %d/%d, want %d/%d", len(sources), len(prefixes), cfg.Config.Topology.Miners, wantPrefixes)
	}
	multi := map[string]bool{"1,5": true, "803,804": true, "807,808": true}
	for prefix, miners := range prefixes {
		if len(miners) == 1 {
			continue
		}
		parts := make([]string, len(miners))
		for index, miner := range miners {
			parts[index] = fmt.Sprint(miner)
		}
		key := strings.Join(parts, ",")
		if !multi[key] {
			t.Fatalf("unexpected shared prefix %s miners=%v", prefix, miners)
		}
		delete(multi, key)
	}
	if len(multi) != 0 {
		t.Fatalf("expected shared-prefix cohorts were absent: %v", multi)
	}
}

func TestBalancedOperatorAssignmentKeepsHeadFleetsIsolated(t *testing.T) {
	cfg := testResolvedConfig(t)
	want := map[int]int{1: 1, 4: 1, 5: 2, 8: 2, 801: 1, 804: 1, 805: 2, 808: 2, 809: 1, 810: 2, 1_000: 2}
	for miner, operator := range want {
		if got := operatorForMiner(cfg, miner); got != operator {
			t.Fatalf("miner %d operator = %d, want %d", miner, got, operator)
		}
	}
	if operatorForMiner(cfg, 0) != 0 || operatorForMiner(cfg, cfg.Config.Topology.Miners+1) != 0 {
		t.Fatal("invalid miner index was assigned to a live operator")
	}
}

func TestTestEgressGeometryKeepsSharedFleetsAboveChallengers(t *testing.T) {
	cfg := testResolvedConfig(t)
	fleetPrefixes := map[int]map[netip.Prefix]bool{}
	claims := map[netip.Prefix]int{}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		fleetPrefixes[fleet] = map[netip.Prefix]bool{}
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			miner := fleetMemberMinerIndex(cfg, fleet, member)
			address, err := netip.ParseAddr(minerTestEgressSourceIP(miner))
			if err != nil {
				t.Fatal(err)
			}
			fleetPrefixes[fleet][netip.PrefixFrom(address, cfg.Policy.Verify.EgressIPv4Prefix).Masked()] = true
		}
		for prefix := range fleetPrefixes[fleet] {
			claims[prefix]++
		}
	}
	type rankedFleet struct {
		fleet int
		score *big.Rat
	}
	ranked := make([]rankedFleet, 0, len(fleetPrefixes))
	for fleet, prefixes := range fleetPrefixes {
		score := new(big.Rat)
		for prefix := range prefixes {
			score.Add(score, new(big.Rat).SetFrac64(1, int64(claims[prefix])))
		}
		ranked = append(ranked, rankedFleet{fleet: fleet, score: score})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if comparison := ranked[i].score.Cmp(ranked[j].score); comparison != 0 {
			return comparison > 0
		}
		return ranked[i].fleet < ranked[j].fleet
	})
	selected := map[int]bool{}
	for _, fleet := range ranked[:cfg.Config.Topology.HeadSlots] {
		selected[fleet.fleet] = true
	}
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		if !selected[fleet] {
			t.Fatalf("initial fleet %d fell below the top-200 boundary", fleet)
		}
	}
	if ranked[0].score.Cmp(big.NewRat(4, 1)) != 0 || ranked[198].score.Cmp(big.NewRat(7, 2)) != 0 || ranked[199].score.Cmp(big.NewRat(7, 2)) != 0 || ranked[200].fleet != 201 || ranked[200].score.Cmp(big.NewRat(3, 1)) != 0 || ranked[201].fleet != 202 || ranked[201].score.Cmp(big.NewRat(3, 1)) != 0 {
		t.Fatalf("unexpected boundary geometry: top=%+v boundary=%+v", ranked[0], ranked[198:])
	}
}

func TestManagedDependencySpecsMirrorServerLocalAndIsolateOperators(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Release = &ReleaseLock{Dependencies: map[string]string{
		"postgres": "postgres:18@sha256:" + strings.Repeat("1", 64),
		"redis":    "redis:8-alpine@sha256:" + strings.Repeat("2", 64),
	}}
	specs, err := dependencyContainerSpecs(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 4 {
		t.Fatalf("dependency spec count = %d, want 4", len(specs))
	}
	postgresOne := specs[0]
	redisOne := specs[1]
	postgresTwo := specs[2]
	redisTwo := specs[3]
	if postgresOne.Name != "unit-test-deployment-pg-1" || postgresTwo.Name != "unit-test-deployment-pg-2" || redisOne.Name != "unit-test-deployment-redis-1" || redisTwo.Name != "unit-test-deployment-redis-2" {
		t.Fatalf("dependency names are not isolated: %+v", []string{postgresOne.Name, redisOne.Name, postgresTwo.Name, redisTwo.Name})
	}
	postgresArgs := strings.Join(postgresOne.RunArgs, " ")
	if !strings.Contains(postgresArgs, "127.0.0.11:5432:5432") || !strings.Contains(strings.Join(postgresTwo.RunArgs, " "), "127.0.0.12:5432:5432") {
		t.Fatalf("PostgreSQL operator bindings are not isolated: %q / %q", postgresArgs, strings.Join(postgresTwo.RunArgs, " "))
	}
	if !strings.Contains(postgresArgs, "/server/local/postgres/initdb") || !strings.Contains(postgresArgs, "POSTGRES_INITDB_ARGS=--locale=en_US.UTF-8") || !strings.Contains(postgresArgs, "APP_DB_USER=bringyour") || !strings.Contains(postgresArgs, "APP_DB_NAME=bringyour") {
		t.Fatalf("PostgreSQL spec drifted from server/local: %q", postgresArgs)
	}
	if strings.Join(postgresOne.Command, " ") != "postgres -c max_connections=512 -c shared_buffers=256MB" {
		t.Fatalf("PostgreSQL server settings drifted: %q", postgresOne.Command)
	}
	if len(postgresOne.DataVolumes) != 1 || postgresOne.DataVolumes[0] != "unit-test-deployment-pg-1-data" || postgresOne.ReadyExpected != "512:256MB:en_US.UTF-8" {
		t.Fatalf("PostgreSQL durable/readiness contract drifted: volumes=%v expected=%q", postgresOne.DataVolumes, postgresOne.ReadyExpected)
	}
	if postgresOne.RestartPolicy != "no" || redisOne.RestartPolicy != "no" || postgresTwo.RestartPolicy != "no" || redisTwo.RestartPolicy != "no" {
		t.Fatalf("managed dependencies can restart after reboot: %q/%q/%q/%q", postgresOne.RestartPolicy, redisOne.RestartPolicy, postgresTwo.RestartPolicy, redisTwo.RestartPolicy)
	}
	postgresProbe := strings.Join(postgresOne.ReadyProbe, " ")
	if !strings.Contains(postgresProbe, "PGPASSWORD=") || !strings.Contains(postgresProbe, "psql -h 127.0.0.1 -U bringyour -d bringyour") || !strings.Contains(postgresProbe, "max_connections") {
		t.Fatalf("PostgreSQL readiness is not an authenticated settings query: %q", postgresProbe)
	}
	redisArgs := strings.Join(redisOne.RunArgs, " ")
	redisCommand := strings.Join(redisOne.Command, " ")
	if !strings.Contains(redisArgs, "127.0.0.11:6379:6379") || !strings.Contains(strings.Join(redisTwo.RunArgs, " "), "127.0.0.12:6379:6379") || !strings.Contains(redisArgs, "nofile=65536:65536") {
		t.Fatalf("Redis operator bindings/settings are not isolated: %q / %q", redisArgs, strings.Join(redisTwo.RunArgs, " "))
	}
	for _, required := range []string{"--io-threads 8", "--maxclients 32768", "--tcp-backlog 65535", "--save  --appendonly no"} {
		if !strings.Contains(redisCommand, required) {
			t.Fatalf("Redis command %q lacks server/local setting %q", redisCommand, required)
		}
	}
	if redisOne.ReadyExpected != "PONG" || len(redisOne.DataVolumes) != 0 {
		t.Fatalf("Redis readiness/durability contract drifted: expected=%q volumes=%v", redisOne.ReadyExpected, redisOne.DataVolumes)
	}
}

func TestMutationPreflightRunsBeforeWriteCapableExecution(t *testing.T) {
	for command, want := range map[string]struct {
		dependencies bool
		binaries     bool
	}{
		"setup":    {},
		"launch":   {dependencies: true, binaries: true},
		"resume":   {dependencies: true, binaries: true},
		"scenario": {dependencies: true},
	} {
		if got := requiresManagedDependencies(command); got != want.dependencies {
			t.Fatalf("%s managed-dependency preflight = %t, want %t", command, got, want.dependencies)
		}
		if got := requiresReleaseBinaries(command); got != want.binaries {
			t.Fatalf("%s binary preflight = %t, want %t", command, got, want.binaries)
		}
	}
}

func TestManagedContainerSpecHashCoversCreationSettings(t *testing.T) {
	spec := managedContainerSpec{
		Name:              "one",
		Image:             "image@sha256:" + strings.Repeat("a", 64),
		ConfigurationHash: "sha256:" + strings.Repeat("b", 64),
		RestartPolicy:     "no",
		RunArgs:           []string{"-p", "127.0.0.11:5432:5432"},
		Command:           []string{"server", "--limit", "1"},
		DataVolumes:       []string{"one-data"},
		ReadyProbe:        []string{"ready"},
		ReadyExpected:     "ready",
		ReadyTimeout:      time.Second,
	}
	first, err := managedContainerSpecHash(spec)
	if err != nil {
		t.Fatal(err)
	}
	second, err := managedContainerSpecHash(spec)
	if err != nil || first != second {
		t.Fatalf("container spec hash is not deterministic: %q %q %v", first, second, err)
	}
	changed := spec
	changed.Command = append([]string(nil), spec.Command...)
	changed.Command[len(changed.Command)-1] = "2"
	third, err := managedContainerSpecHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("container creation command drift did not change the spec hash")
	}
	changed = spec
	changed.DataVolumes = []string{"stale-data"}
	third, err = managedContainerSpecHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("container data-volume drift did not change the spec hash")
	}
	changed = spec
	changed.ConfigurationHash = "sha256:" + strings.Repeat("c", 64)
	third, err = managedContainerSpecHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("server/local configuration drift did not change the spec hash")
	}
	changed = spec
	changed.RestartPolicy = "unless-stopped"
	third, err = managedContainerSpecHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("container restart-policy drift did not change the spec hash")
	}
	dataFirst, err := managedContainerDataSpecHash(spec)
	if err != nil {
		t.Fatal(err)
	}
	dataThird, err := managedContainerDataSpecHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if dataThird != dataFirst {
		t.Fatal("container-only restart policy invalidated compatible persistent data")
	}
	changed.ConfigurationHash = "sha256:" + strings.Repeat("c", 64)
	dataThird, err = managedContainerDataSpecHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if dataThird == dataFirst {
		t.Fatal("data-affecting configuration drift preserved the volume hash")
	}
}

func TestManagedContainerReadinessRequiresExactSemanticOutput(t *testing.T) {
	docker := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(docker, []byte("#!/bin/sh\nprintf 'PONG\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	spec := managedContainerSpec{Name: "redis", ReadyProbe: []string{"redis-cli", "ping"}, ReadyExpected: "PONG", ReadyTimeout: time.Second}
	cli := dockerCLI{Executable: docker}
	if err := waitContainerReady(context.Background(), cli, spec); err != nil {
		t.Fatal(err)
	}
	spec.ReadyExpected = "AUTHENTICATED"
	spec.ReadyTimeout = 20 * time.Millisecond
	if err := waitContainerReady(context.Background(), cli, spec); err == nil || !strings.Contains(err.Error(), "want \"AUTHENTICATED\"") {
		t.Fatalf("incorrect readiness output was accepted: %v", err)
	}
}

func TestDockerResolutionUsesOnlyPasswordlessSudoFallback(t *testing.T) {
	bin := t.TempDir()
	docker := filepath.Join(bin, "docker")
	sudo := filepath.Join(bin, "sudo")
	if err := os.WriteFile(docker, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sudo, []byte("#!/bin/sh\nprintf '29.7.2\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	cli, err := resolveDockerCLI(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cli.Executable != sudo || len(cli.Prefix) != 2 || cli.Prefix[0] != "-n" || cli.Prefix[1] != docker || cli.ServerVersion != "29.7.2" {
		t.Fatalf("Docker sudo fallback = %+v", cli)
	}
}

func TestLiveManagedDependenciesMirrorServerLocal(t *testing.T) {
	if os.Getenv("SIM_TESTNET_LIVE_DEPENDENCIES") != "1" {
		t.Skip("set SIM_TESTNET_LIVE_DEPENDENCIES=1 to create/check the digest-pinned local PG/Redis pairs")
	}
	cfg, err := LoadResolved(LoadOptions{ConfigPath: "testnet.yml", RequireSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := startDependencies(ctx, cfg); err != nil {
		t.Fatal(err)
	}
}
