package main

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSupervisorChild(t *testing.T) {
	if os.Getenv("SIM_TEST_CHILD") != "1" {
		return
	}
	counter := os.Getenv("SIM_TEST_COUNTER")
	if _, err := os.Stat(counter); os.IsNotExist(err) {
		_ = os.WriteFile(counter, []byte("crashed once\n"), 0o600)
		os.Exit(17)
	}
	for {
		time.Sleep(time.Second)
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
	spec := ProcessSpec{
		ID: "restart-child", Role: "test", Identity: "test-child", Command: executable,
		Args: []string{"-test.run=TestSupervisorChild"}, WorkDir: dir,
		Env:        map[string]string{"SIM_TEST_CHILD": "1", "SIM_TEST_COUNTER": counter},
		StdoutPath: filepath.Join(dir, "child.stdout"), StderrPath: filepath.Join(dir, "child.stderr"), RestartLimit: 2,
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
	if err := waitSupervisorReady(ctx, dir, manifest, 12*time.Second); err != nil {
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

func TestSupervisorServiceNamesAndArgumentsAreSafe(t *testing.T) {
	if got := serviceToken("Testnet / Release_1.0"); got != "testnet---release-1-0" {
		t.Fatalf("service token = %q", got)
	}
	if _, err := systemdQuote("bad\npath"); err == nil {
		t.Fatal("systemd argument accepted a newline")
	}
	if got, err := systemdQuote("/tmp/path with spaces"); err != nil || got != `"/tmp/path with spaces"` {
		t.Fatalf("quoted argument = %q, %v", got, err)
	}
}

func TestRestartBackoffIsBounded(t *testing.T) {
	if restartBackoff(1) != time.Second || restartBackoff(100) != 30*time.Second {
		t.Fatalf("unexpected restart backoff: %s, %s", restartBackoff(1), restartBackoff(100))
	}
}

func TestProvisioningStartsOnlyOperatorAPIs(t *testing.T) {
	specs := []ProcessSpec{
		{ID: "operator-1-api", Role: "operator-api"},
		{ID: "operator-1-connect", Role: "operator-connect"},
		{ID: "operator-1-taskworker", Role: "operator-taskworker"},
		{ID: "operator-2-api", Role: "operator-api"},
	}
	got := selectProvisioningServerSpecs(specs)
	if len(got) != 2 || got[0].ID != "operator-1-api" || got[1].ID != "operator-2-api" {
		t.Fatalf("provisioning specs = %+v", got)
	}
}

func TestClientSpecsGiveExactAddressesOneSharedHeadPrefix(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	specs := buildClientSpecs(cfg, t.TempDir(), map[string]string{"miner": "/bin/miner", "validator": "/bin/validator"}, roles)
	sources := map[string]bool{}
	var headPrefix netip.Prefix
	miners := 0
	for _, spec := range specs {
		if spec.Role != "miner" {
			continue
		}
		miners++
		var source string
		for _, arg := range spec.Args {
			if strings.HasPrefix(arg, "--test-egress-source-ip=") {
				source = strings.TrimPrefix(arg, "--test-egress-source-ip=")
			}
		}
		addr, err := netip.ParseAddr(source)
		if err != nil || !addr.Is4() || !addr.IsLoopback() || sources[source] {
			t.Fatalf("%s source identity = %q, err=%v duplicate=%t", spec.ID, source, err, sources[source])
		}
		sources[source] = true
		if miners <= cfg.Config.Topology.HeadFleets*cfg.Config.Topology.ClientsPerHeadFleet {
			prefix := netip.PrefixFrom(addr, cfg.Policy.Verify.EgressIPv4Prefix).Masked()
			if !headPrefix.IsValid() {
				headPrefix = prefix
			} else if prefix != headPrefix {
				t.Fatalf("head miner %s prefix = %s, want shared %s", spec.ID, prefix, headPrefix)
			}
		} else if netip.PrefixFrom(addr, cfg.Policy.Verify.EgressIPv4Prefix).Masked() == headPrefix {
			t.Fatalf("tail miner %s leaked into shared head prefix", spec.ID)
		}
	}
	if miners != cfg.Config.Topology.Miners || len(sources) != miners {
		t.Fatalf("miner/source count = %d/%d, want %d", miners, len(sources), cfg.Config.Topology.Miners)
	}
}
