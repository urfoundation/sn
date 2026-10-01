// Provisional diagnostics must reproduce continuation admission without
// executing actions, accepting source drift as a release, or changing history.
package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// A read-only command still names an exact approval and authenticates its
// driver; no apply, process control, or alternative manifest is admitted.
func TestProvisionalDoctorRequiresReadOnlyExactApproval(t *testing.T) {
	planHash := "0x" + strings.Repeat("ad", 32)
	args := []string{"doctor", "--provisional-resume", "--plan-hash", planHash}
	command, options, err := parseCLI(args)
	if err != nil || command != "doctor" || !options.ProvisionalResume || options.Apply || executableAttestationModeForCommand(command, options) != executableAttestationProvisionalResume {
		t.Fatalf("read-only retained doctor admission failed: %+v %v", options, err)
	}
	for _, extra := range [][]string{
		{"--apply"}, {"--detach"}, {"--prepare-only"}, {"--then-release-candidate"},
		{"--name", "release-candidate"}, {"--manifest", "https://operator.example/manifest"},
		{"--strict-history-adoption", "/fixture/history.json"},
		{"--provisional-rpc-authority", "192.0.2.8:19944"},
		{"--plan-hash", ""}, {"--plan-hash", "approximate"},
	} {
		if _, _, err := parseCLI(append(append([]string{}, args...), extra...)); err == nil {
			t.Errorf("provisional doctor accepted incompatible flags %v", extra)
		}
	}
}

// The real plan loader retains original inputs across a driver hotfix. A
// generated private route meets the LAN-only contract without naming a node.
func TestProvisionalDoctorRetainsApprovedRouteAndAuthenticatesSuccessor(t *testing.T) {
	authority := net.JoinHostPort(net.IPv4(10, 203, 0, 19).String(), "23456")
	cfg, plan, stateDir, options, before := provisionalRuntimePlanFixtureWithOwnedRpc(t, authority)
	options.Apply, options.Name = false, ""
	if _, _, err := parseCLI([]string{"doctor", "--provisional-resume", "--plan-hash", plan.PlanHash, "--owned-rpc-authority", authority}); err != nil {
		t.Fatal("doctor rejected the retained route", err)
	}
	reader, err := prepareProvisionalDoctor(t.Context(), cfg, stateDir, options)
	if err != nil {
		t.Fatal(err)
	}
	if reader == cfg || reader.provisionalResume == cfg.provisionalResume || provisionalResumeEnabled(cfg) || !reader.readOnlyAudit {
		t.Fatal("diagnostic admission changed its strict caller's authority")
	}
	record := reader.provisionalResume.Record
	if record.Command != "doctor" || !record.ReadOnly || !record.Provisional || record.FinalAcceptance || record.PlanHash != plan.PlanHash || record.ReleaseLockHash != plan.ReleaseLockHash || record.ConfigHash != plan.ConfigHash || record.Driver != cfg.provisionalResume.Driver {
		t.Fatalf("doctor lost retained plan or actual driver provenance: %+v", record)
	}
	if err := validateOwnedRPCPlan(reader, plan); err != nil {
		t.Fatal("diagnostics substituted approved operational inputs", err)
	}
	if verificationSubstrateEndpoint(reader) != "ws://"+authority || verificationEVMEndpoint(reader) != "http://"+authority || configuredEVMRequestsPerMinute(reader, reader.OperationalEVM) != 0 {
		t.Fatal("diagnostics changed the unpaced approved route")
	}
	chain := provisionalRuntimeChainTest(t, reader)
	current, err := readAuthenticatedRuntimeMetadataAtContext(t.Context(), chain, reader, types.Hash{2})
	if err != nil || current.CompatibilityProfile != crv4.ProvisionalRuntimeCompatibilityProfile || current.Version.SpecVersion != provisionalRuntimeSuccessorTestSpec {
		t.Fatalf("diagnostics failed continuation's capability admission: %+v %v", current, err)
	}
	prior, err := readReleaseHistoryRuntimeMetadataAtContext(t.Context(), chain, reader, types.Hash{1})
	if err != nil || prior.CompatibilityProfile != "" || prior.CodeHash != cfg.Release.Runtime.CodeHash {
		t.Fatalf("diagnostics relabeled reviewed history: %+v %v", prior, err)
	}
	if _, err := readAuthenticatedRuntimeMetadataAtContext(t.Context(), chain, cfg, types.Hash{2}); err == nil {
		t.Fatal("strict caller inherited a diagnostic compatibility observation")
	}
	after, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("doctor rewrote retained plan bytes", err)
	}
	for _, name := range []string{"journal.jsonl", "deployment.lock", "supervisor.json", "supervisor.state.json", "runtime-config-manifest.json", "secrets"} {
		if _, err := os.Lstat(filepath.Join(stateDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("doctor mutated deployment path %s: %v", name, err)
		}
	}
	var persisted provisionalResumeRecord
	if err := readJSONFile(reader.provisionalResume.RecordPath, &persisted); err != nil || persisted != *record {
		t.Fatalf("diagnostic provenance was not retained exactly: %v", err)
	}
}

// Missing or altered operational authority fails before any observation is
// persisted. The strict doctor default retains its existing runtime checks.
func TestProvisionalDoctorRejectsApprovalDriftBeforeWriting(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ResolvedConfig, *cliOptions)
	}{
		{name: "config", change: func(cfg *ResolvedConfig, _ *cliOptions) { cfg.ConfigHash += "-changed" }},
		{name: "plan", change: func(_ *ResolvedConfig, options *cliOptions) { options.PlanHash = "0x" + strings.Repeat("ef", 32) }},
		{name: "route", change: func(cfg *ResolvedConfig, _ *cliOptions) { cfg.OperationalEVM += "/other" }},
		{name: "driver", change: func(cfg *ResolvedConfig, _ *cliOptions) { cfg.provisionalResume = nil }},
		{name: "apply", change: func(_ *ResolvedConfig, options *cliOptions) { options.Apply = true }},
	}
	for _, test := range tests {
		cfg, _, stateDir, options, before := provisionalRuntimePlanFixture(t)
		options.Apply, options.Name = false, ""
		test.change(cfg, &options)
		if _, err := prepareProvisionalDoctor(t.Context(), cfg, stateDir, options); err == nil {
			t.Errorf("%s admitted altered doctor authority", test.name)
		}
		entries, err := os.ReadDir(stateDir)
		if err != nil || len(entries) != 1 || entries[0].Name() != "plan.json" {
			t.Fatalf("%s wrote state before admission: %v", test.name, err)
		}
		after, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("%s changed retained plan bytes: %v", test.name, err)
		}
	}
}

// An ordinary doctor must not infer provisional authority from retained
// observations or from the testnet identity alone.
func TestProvisionalDoctorStrictDefaultKeepsAuthority(t *testing.T) {
	cfg, _, stateDir, _, before := provisionalRuntimePlanFixture(t)
	reader, err := prepareProvisionalDoctor(t.Context(), cfg, stateDir, cliOptions{})
	if err != nil || reader != cfg || provisionalResumeEnabled(reader) {
		t.Fatalf("ordinary doctor gained provisional authority: %v", err)
	}
	if _, err := readAuthenticatedRuntimeMetadataAtContext(t.Context(), provisionalRuntimeChainTest(t, cfg), reader, types.Hash{2}); err == nil {
		t.Fatal("ordinary doctor accepted a successor runtime")
	}
	after, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("ordinary doctor changed retained plan", err)
	}
	if _, err := os.Lstat(filepath.Join(stateDir, "provisional-resumes")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("ordinary doctor persisted provisional authority", err)
	}
}

// Skipping version numbers never demands another source release when the
// consumed interface is unchanged. Each actual artifact still gets its own
// immutable observation, with historical pins remaining exact.
func TestProvisionalDoctorAuthenticatesNonAdjacentSuccessors(t *testing.T) {
	cfg, _, stateDir, options, _ := provisionalRuntimePlanFixture(t)
	options.Apply, options.Name = false, ""
	reader, err := prepareProvisionalDoctor(t.Context(), cfg, stateDir, options)
	if err != nil {
		t.Fatal(err)
	}
	runtimeSpecs := []uint32{reviewedRuntimeSpecVersion + 1, reviewedRuntimeSpecVersion + 2, reviewedRuntimeSpecVersion + 100}
	for _, runtimeSpec := range runtimeSpecs {
		chain := provisionalRuntimeChainTest(t, reader)
		client := chain.API.Client.(*releaseRuntimeTestClient)
		read := client.callContext
		client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
			if method == "state_getRuntimeVersion" && len(args) == 1 && args[0] == (types.Hash{2}).Hex() {
				return setReleaseRuntimeTestResult(target, map[string]any{"specName": "node-subtensor", "specVersion": runtimeSpec, "transactionVersion": 1, "stateVersion": 1, "apis": []any{[]any{"0x8375104b299b74c5", 2}}})
			}
			return read(ctx, target, method, args...)
		}
		current, err := readAuthenticatedRuntimeMetadataAtContext(t.Context(), chain, reader, types.Hash{2})
		if err != nil || current.Version.SpecVersion != runtimeSpec || current.CompatibilityProfile != crv4.ProvisionalRuntimeCompatibilityProfile {
			t.Fatalf("compatible future spec %d required a source update: %+v %v", runtimeSpec, current, err)
		}
	}
	observations, err := os.ReadDir(filepath.Join(filepath.Dir(reader.provisionalResume.RecordPath), "runtime-compatibility"))
	if err != nil || len(observations) != len(runtimeSpecs) {
		t.Fatalf("successor observations were conflated: %d %v", len(observations), err)
	}
}
