//go:build linux

// A signed generation and real test-owned validator children exercise the
// observer's complete local evidence path without contacting a chain.
package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func scenarioValidatorAuthorityFixture(t *testing.T) (*policyRolloverGenerationTestV2, *policyRolloverHandoffV2, *liveScenarioProbe, []OperatorObservation) {
	t.Helper()
	g, handoff, specs := policyRolloverObservationFixtureV2(t)
	f := g.fixture
	childRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(childRoot, "__validator"), []byte("printf 'ready\\n'\nread -r input\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := SupervisorFile{DeploymentID: f.cfg.Config.Deployment.DeploymentID}
	ticks, err := processStartTimeTicks(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: ticks}
	for _, spec := range specs {
		if spec.Role != "validator" {
			continue
		}
		command := exec.CommandContext(t.Context(), "/bin/sh", spec.Args...)
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
			t.Fatalf("synthetic validator did not reach owned barrier: %q %v", line, err)
		}
		manifest.Specs = append(manifest.Specs, spec)
		state.Processes = append(state.Processes, ProcessState{ID: spec.ID, Role: spec.Role, Identity: spec.Identity, PID: command.Process.Pid})
	}
	if len(manifest.Specs) != f.cfg.Config.Topology.Validators {
		t.Fatal("fixture lacks the complete validator process census")
	}
	if err := writePublicJSON(filepath.Join(f.stateDir, "supervisor.json"), manifest); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(f.stateDir, "supervisor.json"))
	if err != nil {
		t.Fatal(err)
	}
	state.ManifestHash, err = canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	adoption := provisionalLiveTopology{Schema: "urnetwork-sim-provisional-live-topology-v1", Provisional: true, PlanHash: f.plan.PlanHash,
		CompletedAt: "2000-01-02T03:04:05Z", ManifestHash: state.ManifestHash, ManifestBytesSHA256: bytesSHA256(raw), SupervisorPID: state.SupervisorPID, SupervisorStartTimeTicks: ticks}
	for path, value := range map[string]any{"supervisor.state.json": state, "provisional-resumes/live-topology.json": adoption} {
		if err := writePublicJSON(filepath.Join(f.stateDir, path), value); err != nil {
			t.Fatal(err)
		}
	}
	server := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x73}, ed25519.SeedSize))
	var operators []OperatorObservation
	for noId := 1; noId <= f.cfg.Config.Topology.Operators; noId++ {
		operators = append(operators, OperatorObservation{NoID: noId, VerifyKeys: []VerifyKeyObservation{{ServerKeyID: 1, PublicKey: server.Public().(ed25519.PublicKey)}}})
		for _, selected := range handoff.Validators {
			path := filepath.Join(selected.ClientStateDir, "operators", fmt.Sprintf("no-%d", noId), "proofs.jsonl")
			policyRolloverSignedProofV2(t, path, server, noId, f.cfg.Policy.Verify.TrailDepth)
		}
	}
	runtime, err := campaignRPCConfig(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	return g, handoff, &liveScenarioProbe{cfg: runtime, authorizedCfg: f.cfg, stateDir: f.stateDir}, operators
}

func TestScenarioObserverSeparatesApprovedInputsFromCampaignTransport(t *testing.T) {
	g, handoff, probe, operators := scenarioValidatorAuthorityFixture(t)
	f := g.fixture
	authorizedHash, err := resolvedInputsHash(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	runtimeHash, err := resolvedInputsHash(probe.cfg)
	if err != nil || runtimeHash == authorizedHash || authorizedHash != f.plan.ResolvedInputsHash {
		t.Fatal("fixture did not reproduce the actual transport derivative", err)
	}
	if _, err := loadRuntimePersistedPlan(probe.cfg, f.stateDir); !errors.Is(err, errPersistedPlanIdentityMismatch) || !strings.Contains(err.Error(), "resolved_inputs_hash") {
		t.Fatalf("transport unexpectedly became approved plan authority: %v", err)
	}
	planBefore, err := os.ReadFile(filepath.Join(f.stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		observations, err := probe.inspectValidators(t.Context(), operators)
		if err != nil || len(observations) != len(handoff.Validators) {
			t.Fatalf("complete local observer: observations=%+v error=%v", observations, err)
		}
		for index, observation := range observations {
			local := observation.LocalRuntimeIntents
			if observation.Error != "" || local == nil || local.State != "absent" || local.FinalAcceptance || local.StateDirectory != handoff.Validators[index].StateDir || !validSHA256ContentHash(local.HandoffSHA256) {
				t.Fatalf("approved fresh source was lost through the proxy: %+v", observation)
			}
			if !reflect.DeepEqual(observation.PathProofCounts, map[int]int{1: 1, 2: 1}) || observation.FinalizedIntents != 0 || observation.AppliedIntents != 0 {
				t.Fatalf("fresh proof namespace/counters differ: %+v", observation)
			}
		}
	}
	planAfter, err := os.ReadFile(filepath.Join(f.stateDir, "plan.json"))
	if err != nil || !bytes.Equal(planBefore, planAfter) {
		t.Fatal("local observation rewrote the approved plan", err)
	}
	if current, err := resolvedInputsHash(f.cfg); err != nil || current != authorizedHash {
		t.Fatal("observation mutated canonical inputs", err)
	}
	if current, err := resolvedInputsHash(probe.cfg); err != nil || current != runtimeHash || probe.cfg.OperationalEVM != "http://"+campaignEVMAuthority() {
		t.Fatal("local authentication changed the campaign I/O route", err)
	}
}

func TestScenarioObserverRefusesTransportAndApprovalSubstitution(t *testing.T) {
	g, _, probe, operators := scenarioValidatorAuthorityFixture(t)
	original := probe.cfg
	changed := *original
	changed.OperationalEVM = "https://unapproved-rpc.example"
	probe.cfg = &changed
	if got, err := probe.inspectValidators(t.Context(), operators); err == nil || got != nil || !strings.Contains(err.Error(), "campaign RPC transport") || probe.pathProofs != nil {
		t.Fatalf("unapproved transport entered the local reader: observations=%+v error=%v", got, err)
	}
	authorized := *g.fixture.cfg
	authorized.ObjectStoreHost = "changed-approval.example"
	probe.authorizedCfg = &authorized
	var err error
	probe.cfg, err = campaignRPCConfig(&authorized)
	if err != nil {
		t.Fatal(err)
	}
	got, err := probe.inspectValidators(t.Context(), operators)
	if err != nil || len(got) != authorized.Config.Topology.Validators {
		t.Fatalf("invalid source diagnostic was lost: observations=%+v error=%v", got, err)
	}
	for _, observation := range got {
		if !strings.Contains(observation.Error, "resolved_inputs_hash differs") || len(observation.PathProofCounts) != 0 || observation.LocalRuntimeIntents == nil || observation.LocalRuntimeIntents.State != "unknown" {
			t.Fatalf("foreign canonical inputs gained source authority: %+v", observation)
		}
	}
}

func TestScenarioObserverCanonicalInputsKeepGenerationCorruptionBlocking(t *testing.T) {
	_, handoff, probe, operators := scenarioValidatorAuthorityFixture(t)
	if err := os.WriteFile(handoff.Validators[0].Evidence.Operators[0].Context.Path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	observations, err := probe.inspectValidators(t.Context(), operators)
	if err != nil || len(observations) != 2 {
		t.Fatalf("corruption diagnostic was lost: observations=%+v error=%v", observations, err)
	}
	for _, observation := range observations {
		if observation.Error == "" || len(observation.PathProofCounts) != 0 || observation.LocalRuntimeIntents == nil || observation.LocalRuntimeIntents.State != "unknown" || observation.FinalizedIntents != 0 || observation.AppliedIntents != 0 {
			t.Fatalf("changed signed generation contributed observation authority: %+v", observation)
		}
	}
}
