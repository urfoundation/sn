//go:build linux

package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/ss58"
)

// A real test-owned child passes the kernel argv/generation checks before the
// actual source reader returns nil plus an error. No chain or live tree is used.
func TestProvisionalIntentObservationRetainsUnknownAfterSourceFailure(t *testing.T) {
	cfg := testResolvedConfig(t)
	planHash := "0x" + strings.Repeat("36", 32)
	cfg.provisionalResume = &provisionalResumeState{AcceptedPlanHashes: []string{planHash}, Record: &provisionalResumeRecord{Provisional: true, PlanHash: planHash}}
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	childRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(childRoot, "__validator"), []byte("printf 'ready\\n'\nread -r input\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sh", "__validator")
	command.Dir = childRoot
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
	t.Cleanup(func() {
		_, _ = input.Write([]byte("done\n"))
		_ = input.Close()
		_ = command.Wait()
	})
	if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("test child failed to enter its owned wait: %q %v", line, err)
	}
	identity, err := ss58.Encode([32]byte{0x35}, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	spec := ProcessSpec{ID: "validator-1", Role: "validator", Identity: identity, Args: []string{"__validator"}}
	manifest := SupervisorFile{DeploymentID: cfg.Config.Deployment.DeploymentID, Specs: []ProcessSpec{spec}}
	if err := writePublicJSON(filepath.Join(stateDir, "supervisor.json"), manifest); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, "supervisor.json"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	ticks, err := processStartTimeTicks(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: ticks, ManifestHash: hash,
		Processes: []ProcessState{{ID: spec.ID, Role: spec.Role, Identity: spec.Identity, PID: command.Process.Pid}}}
	adoption := provisionalLiveTopology{Schema: "urnetwork-sim-provisional-live-topology-v1", Provisional: true, PlanHash: planHash,
		CompletedAt: "2000-01-02T03:04:05Z", ManifestHash: hash, ManifestBytesSHA256: bytesSHA256(raw), SupervisorPID: state.SupervisorPID, SupervisorStartTimeTicks: ticks}
	for path, value := range map[string]any{"supervisor.state.json": state, "provisional-resumes/live-topology.json": adoption} {
		if err := writePublicJSON(filepath.Join(stateDir, path), value); err != nil {
			t.Fatal(err)
		}
	}
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	observed, generation := observeProvisionalValidatorIntent(t.Context(), cfg, stateDir, 1)
	if generation != "" || observed.LocalRuntimeIntents == nil || observed.LocalRuntimeIntents.State != "unknown" || observed.LocalRuntimeIntents.FinalAcceptance ||
		!strings.Contains(observed.Error, "lacks its explicit activation handoff") || observed.LocalRuntimeIntents.Error == "" {
		t.Fatalf("source failure gained observation authority or lost its cause: %+v generation=%q", observed, generation)
	}
	projected := inspectProvisionalValidatorIntent(t.Context(), cfg, stateDir, 1)
	if projected.LocalRuntimeIntents == nil || projected.LocalRuntimeIntents.State != "unknown" || projected.FinalizedIntents != 0 || projected.AppliedIntents != 0 || !strings.Contains(projected.Error, "lacks its explicit activation handoff") {
		t.Fatalf("source failure was not retained as a non-authorizing diagnostic: %+v", projected)
	}
	if after := validatorNamespaceTreeSnapshot(t, stateDir); !reflect.DeepEqual(before, after) {
		t.Fatal("failed observation modified retained state")
	}
}
