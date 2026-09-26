//go:build linux || darwin

// Recovery handoffs must be admitted after generation selection; arbitrary
// child arguments are never an alternative to a signed deployment plan.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	validatorcomponent "github.com/urfoundation/sn/validator"
	"gopkg.in/yaml.v3"
)

// An existing rollover must reject a recovery argument without its separately
// authenticated receipt, including arguments carried by a stopped supervisor.
func TestNativeHistoryRecoveryV2RejectsUnapprovedLauncherArguments(t *testing.T) {
	g, _, _ := newPolicyRolloverSourceRoleTestV2(t)
	f := g.fixture
	specs := []ProcessSpec{
		{ID: "validator-1", Role: "validator", Args: []string{"__validator", "--native-history-recovery=/unapproved.json"}},
		{ID: "validator-2", Role: "validator", Args: []string{"__validator"}},
	}
	if err := attachPolicyRolloverProcessConfigsV2(t.Context(), f.cfg, f.stateDir, f.plan, specs); err == nil {
		t.Fatal("generation projection retained an unapproved native recovery argument")
	}
}

// nativeHistoryRecoveryTestV2 supplies real file custody and deployment owner
// signatures. Its inert intent values are only local binding fixtures; the
// injected native boundary never claims a real source or chain application.
type nativeHistoryRecoveryTestV2 struct {
	f        *runtimeEvidenceProvisionV2TestFixture
	h        *policyRolloverHandoffV2
	p        *nativeHistoryRecoveryPlanV2
	original map[string][]byte
}

// newNativeHistoryRecoveryTestV2 models the generation-2 config and separate
// source-role path with unchanged seeds, operator settings and evidence inputs.
func newNativeHistoryRecoveryTestV2(t *testing.T) *nativeHistoryRecoveryTestV2 {
	t.Helper()
	return newNativeHistoryRecoveryConfiguredTestV2(t, nil)
}

// Configured fixtures preserve genuine historical config hashes and signatures.
func newNativeHistoryRecoveryConfiguredTestV2(t *testing.T, configure func(*ResolvedConfig)) *nativeHistoryRecoveryTestV2 {
	t.Helper()
	f := newRuntimeEvidenceProvisionV2ConfiguredTestFixture(t, configure)
	renderFinalValidatorFixtureTest(t, f)
	f.cfg.provisionalResume = &provisionalResumeState{Driver: provisionalDriverProvenance{ExecutablePath: "/synthetic/driver", ExecutableSHA256: "sha256:" + strings.Repeat("31", 32), Build: releaseExecutableBuildIdentity{PackagePath: "github.com/urfoundation/sn/sim-testnet", ModulePath: "github.com/urfoundation/sn", Revision: strings.Repeat("4", 40)}}, Record: &provisionalResumeRecord{Schema: "urnetwork-sim-provisional-resume-v1", Provisional: true, PlanHash: f.plan.PlanHash, ConfigHash: f.cfg.ConfigHash, DeploymentID: f.plan.DeploymentID}}
	h := &policyRolloverHandoffV2{Generation: 2, PlanHash: "0x" + strings.Repeat("51", 32), sourceSHA256: bytesSHA256([]byte("synthetic-generation-authority"))}
	overlay := []byte("synthetic-overlay-reference-boundary")
	overlayPath := filepath.Join(f.stateDir, "policy-rollover", "source-role", "generation-00000000000000000002", "handoff.evidence.json")
	if _, err := validatorcomponent.WriteReleaseEvidenceV2File(t.Context(), overlayPath, overlay, 1024); err != nil {
		t.Fatal(err)
	}
	reference := policyRolloverFile(overlayPath, overlay)
	h.SourceRoleOverlay = &reference
	runtime := crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: crv4.ReviewedRuntimeSpecVersion + 1, TransactionVersion: 1, StateVersion: 1}, CodeHash: "0x" + strings.Repeat("61", 32), MetadataHash: "0x" + strings.Repeat("62", 32)}
	p := &nativeHistoryRecoveryPlanV2{Schema: nativeHistoryRecoverySchemaV2, BasePlanHash: f.plan.PlanHash, DeploymentId: f.plan.DeploymentID, StateDir: f.stateDir, ConfigHash: f.cfg.ConfigHash, PolicyHash: f.cfg.PolicyHash, Generation: 2, RolloverPlanHash: h.PlanHash, RolloverHandoffSha256: h.sourceSHA256, SourceRole: reference, Driver: f.cfg.provisionalResume.Driver, Native: ChainHead{Number: 150, Hash: "0x" + strings.Repeat("71", 32)}, NativeEpoch: 23, FirstNativeEpoch: 25, Runtime: runtime, Provisional: true}
	original := map[string][]byte{}
	for _, id := range []int{1, 2} {
		config, err := validatorcomponent.LoadReleaseConfig(filepath.Join(f.stateDir, "runtime", fmt.Sprintf("validator-%d", id), "validator.yml"))
		if err != nil {
			t.Fatal(err)
		}
		config.StateDir = filepath.Join(f.stateDir, "runtime", fmt.Sprintf("validator-%d", id), "evidence-generations", "generation-00000000000000000002", "coordinator-state-v2")
		config.ProvisionalRuntimeCompatibility = crv4.ProvisionalRuntimeCompatibilityProfile
		configPath := filepath.Join(filepath.Dir(config.StateDir), "validator.yml")
		if id == 2 {
			configPath = filepath.Join(filepath.Dir(overlayPath), "validator-2", "validator.yml")
		}
		raw, err := yaml.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := validatorcomponent.WriteReleaseEvidenceV2File(t.Context(), configPath, raw, 128*1024); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(config.StateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		intent := struct {
			Schema  string                              `json:"schema"`
			Current *validatorcomponent.SteeringIntent  `json:"current,omitempty"`
			History []validatorcomponent.SteeringIntent `json:"history"`
		}{Schema: validatorcomponent.SteeringIntentSchema, Current: &validatorcomponent.SteeringIntent{SubnetEpoch: 20, SettlementEpoch: 10, Status: "applied", MeasurementArtifactHash: bytesSHA256([]byte(fmt.Sprintf("synthetic-measurement-%d", id))), Prepared: &crv4.PreparedSubmission{SourceCommitment: &crv4.PreparedSourceCommitment{Hash: "inert-local-prefix-test"}}}}
		intentBytes, err := json.MarshalIndent(intent, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		intentBytes = append(intentBytes, '\n')
		intentPath := filepath.Join(config.StateDir, "steering-intents.json")
		if err := os.WriteFile(intentPath, intentBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		ema, err := validatorcomponent.NewHeadEMAStore(config.StateDir)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := ema.FoldForEpoch(20, map[validatorcomponent.FleetScoreKey]*big.Rat{}, protocol.Rational{Numerator: 1, Denominator: 2}); err != nil {
			t.Fatal(err)
		}
		requestBytes, err := validatorcomponent.CaptureReleaseNativeHistoryRecoveryV2(t.Context(), f.stateDir, configPath, raw, f.plan.PlanHash, h.PlanHash, 25, runtime)
		if err != nil {
			t.Fatal(err)
		}
		request, err := validatorcomponent.DecodeReleaseNativeHistoryRecoveryV2(requestBytes, bytesSHA256(requestBytes))
		if err != nil {
			t.Fatal(err)
		}
		p.Validators = append(p.Validators, *request)
		h.Validators = append(h.Validators, policyRolloverValidatorHandoffV2{ValidatorID: uint64(id), StateDir: config.StateDir, Config: policyRolloverFile(configPath, raw)})
		original[configPath], original[intentPath] = raw, intentBytes
		original[filepath.Join(config.StateDir, "head-ema.json")], err = os.ReadFile(filepath.Join(config.StateDir, "head-ema.json"))
		if err != nil {
			t.Fatal(err)
		}
	}
	boundary := &scenarioCampaignAcceptanceBoundary{LastObservationHead: ChainHead{Number: 900, Hash: "0x" + strings.Repeat("81", 32)}, LastObservationEpoch: 15, AcceptanceWindow: ScenarioAcceptanceWindow{TerminalBlock: 899}}
	source := scenarioCampaignAttemptPayload{Schema: "urnetwork-sim-scenario-attempt-v2", Phase: "release-1.0", RunID: "synthetic-sealed-release-1.0", StartedAt: "2026-01-01T00:00:00Z", PreparationComplete: true, PlanHash: f.plan.PlanHash, ConfigHash: f.cfg.ConfigHash, PolicyHash: f.cfg.PolicyHash, AcceptanceBoundary: boundary, AcceptanceInvalidation: "synthetic retained failed interval", AcceptanceInvalidatedAt: "2026-01-01T02:00:00Z"}
	signed, err := signEvidence(f.cfg, scenarioCampaignAttemptEvidenceKind, source.RunID, source, f.roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(f.stateDir, "campaign-attempts", "release-1.0.recovery.2.evidence.json")
	if _, err := writeRuntimeEvidenceSetupV2(t.Context(), sourcePath, signed, nativeHistoryRecoveryMaximumBytesV2); err != nil {
		t.Fatal(err)
	}
	accepted := false
	outcome := &ScenarioResult{Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", RunID: source.RunID, Name: source.Phase, StartedAt: source.StartedAt, CompletedAt: "2026-01-01T01:00:00Z", Result: "fail", ConfigHash: f.cfg.ConfigHash, PolicyHash: f.cfg.PolicyHash, DeploymentID: f.plan.DeploymentID, ChainID: f.cfg.ChainID, Netuid: f.cfg.Netuid, GenesisHash: f.cfg.Public.Chain.GenesisHash, Provisional: true, FinalAcceptance: &accepted, EndHead: boundary.LastObservationHead, EndEpoch: boundary.LastObservationEpoch, AcceptanceWindow: &boundary.AcceptanceWindow, AssertionCount: 1, FailedAssertionCount: 1, Assertions: []AssertionRecord{{ID: "synthetic-native-gap", Passed: false}}}
	outcome.EvidenceHash, err = canonicalScenarioResultHash(outcome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeRuntimeEvidenceSetupV2(t.Context(), filepath.Join(f.stateDir, "runs", source.RunID, "result.json"), outcome, nativeHistoryRecoveryMaximumBytesV2); err != nil {
		t.Fatal(err)
	}
	p.Terminal, p.Result, p.RunId, err = authenticateNativeRecoveryTerminalV2(t.Context(), f.cfg, f.stateDir, f.plan, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	p.PlanHash, err = p.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateNativeHistoryRecoveryPlanV2(t.Context(), f.cfg, f.stateDir, f.plan, h, p, true); err != nil {
		t.Fatal(err)
	}
	return &nativeHistoryRecoveryTestV2{f: f, h: h, p: p, original: original}
}

// TestNativeHistoryRecoveryV2SignsAndRestartsExactSelection exercises real
// signed plan/child publication and a retained restart with no new selection.
func TestNativeHistoryRecoveryV2SignsAndRestartsExactSelection(t *testing.T) {
	x := newNativeHistoryRecoveryTestV2(t)
	f := x.f
	verified := 0
	receipt, err := publishNativeHistoryRecoveryV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, x.h, x.p, func(context.Context, *nativeHistoryRecoveryPlanV2) error { verified++; return nil })
	if err != nil || verified != 1 {
		t.Fatal("signed recovery publication failed", verified, err)
	}
	f.cfg.nativeHistoryRecoveryV2 = &nativeHistoryRecoverySelectionV2{path: receipt.Path, sha256: receipt.SHA256}
	specs := []ProcessSpec{{ID: "validator-1", Role: "validator", Args: []string{"__validator", "--config=" + x.p.Validators[0].ConfigPath}}, {ID: "validator-2", Role: "validator", Args: []string{"__validator", "--config=" + x.p.Validators[1].ConfigPath}}}
	if err := attachNativeHistoryRecoveryV2(t.Context(), f.cfg, f.stateDir, f.plan, x.h, specs); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(specs)
	f.cfg.nativeHistoryRecoveryV2 = nil
	if err := attachNativeHistoryRecoveryV2(t.Context(), f.cfg, f.stateDir, f.plan, x.h, specs); err != nil {
		t.Fatal("retained restart lost signed recovery", err)
	}
	after, _ := json.Marshal(specs)
	if !bytes.Equal(before, after) {
		t.Fatal("retained restart changed exact selected argv")
	}
	for _, change := range []func([]ProcessSpec){
		func(s []ProcessSpec) { s[0].Args = s[0].Args[:len(s[0].Args)-1] },
		func(s []ProcessSpec) {
			s[0].Args[len(s[0].Args)-1] = "--native-history-recovery-sha256=" + bytesSHA256([]byte("changed"))
		},
		func(s []ProcessSpec) { s[0].Args = append(s[0].Args, s[0].Args[len(s[0].Args)-1]) },
		func(s []ProcessSpec) { s[0].Args = append(s[0].Args, "--native-history-recovery-unknown=1") },
		func(s []ProcessSpec) { s[1].Args = s[1].Args[:2] },
	} {
		var changed []ProcessSpec
		if err := json.Unmarshal(before, &changed); err != nil {
			t.Fatal(err)
		}
		change(changed)
		if err := attachNativeHistoryRecoveryV2(t.Context(), f.cfg, f.stateDir, f.plan, x.h, changed); err == nil {
			t.Fatal("retained restart repaired altered child authority")
		}
	}
	for path, want := range x.original {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(want, got) {
			t.Fatal("recovery changed original config, prefix or compact EMA", path, err)
		}
	}
	if !strings.Contains(specs[1].Args[1], "source-role") {
		t.Fatal("recovery lost validator 2 source-role overlay")
	}
}

// TestNativeHistoryRecoveryV2RejectsRehashedAuthority checks the parent approval
// against independently retained source, generation, runtime and driver owners.
func TestNativeHistoryRecoveryV2RejectsRehashedAuthority(t *testing.T) {
	x := newNativeHistoryRecoveryTestV2(t)
	for _, change := range []func(*nativeHistoryRecoveryPlanV2){
		func(p *nativeHistoryRecoveryPlanV2) { p.Generation++ },
		func(p *nativeHistoryRecoveryPlanV2) { p.FinalAcceptance = true },
		func(p *nativeHistoryRecoveryPlanV2) { p.Provisional = false },
		func(p *nativeHistoryRecoveryPlanV2) { p.StateImported = true },
		func(p *nativeHistoryRecoveryPlanV2) { p.NativeEpoch = p.FirstNativeEpoch },
		func(p *nativeHistoryRecoveryPlanV2) { p.SourceRole.SHA256 = bytesSHA256([]byte("other")) },
		func(p *nativeHistoryRecoveryPlanV2) { p.Driver.ExecutableSHA256 = strings.Repeat("f", 64) },
		func(p *nativeHistoryRecoveryPlanV2) { p.Terminal.SHA256 = bytesSHA256([]byte("other")) },
		func(p *nativeHistoryRecoveryPlanV2) {
			p.Validators[1].Adoption.ConfigSHA256 = p.Validators[0].Adoption.ConfigSHA256
		},
		func(p *nativeHistoryRecoveryPlanV2) { p.Runtime.Version.SpecVersion++ },
	} {
		wire, _ := json.Marshal(x.p)
		var changed nativeHistoryRecoveryPlanV2
		if err := json.Unmarshal(wire, &changed); err != nil {
			t.Fatal(err)
		}
		change(&changed)
		changed.PlanHash, _ = changed.hash()
		if err := validateNativeHistoryRecoveryPlanV2(t.Context(), x.f.cfg, x.f.stateDir, x.f.plan, x.h, &changed, true); err == nil {
			t.Fatal("rehashing substituted independent recovery authority")
		}
	}
}

// TestNativeHistoryRecoveryV2RejectsWrongSignerAndLateSourceChange prevents
// review publication from overriding native refusal or a changed local prefix.
func TestNativeHistoryRecoveryV2RejectsWrongSignerAndLateSourceChange(t *testing.T) {
	x := newNativeHistoryRecoveryTestV2(t)
	f := x.f
	refusal := errors.New("synthetic native application does not match")
	if _, err := publishNativeHistoryRecoveryV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, x.h, x.p, func(context.Context, *nativeHistoryRecoveryPlanV2) error { return refusal }); !errors.Is(err, refusal) {
		t.Fatal("native verification refusal was lost", err)
	}
	if _, err := os.Stat(nativeHistoryRecoveryRootV2(f.stateDir, x.p.PlanHash)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed native verification published recovery files", err)
	}
	receipt, err := publishNativeHistoryRecoveryV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, x.h, x.p, func(context.Context, *nativeHistoryRecoveryPlanV2) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := signEvidence(f.cfg, nativeHistoryRecoveryKindV2, x.p.PlanHash, x.p, f.roles.EVM["keeper"])
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(wrong)
	raw = append(raw, '\n')
	if err := os.WriteFile(receipt.Path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeHistoryRecoveryHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan, x.h, receipt.Path, policyRolloverFile(receipt.Path, raw).SHA256); err == nil {
		t.Fatal("another valid signer granted recovery")
	}
	if err := os.WriteFile(filepath.Join(x.p.Validators[0].Adoption.CoordinatorStateDir, "head-ema.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateNativeHistoryRecoveryPlanV2(t.Context(), f.cfg, f.stateDir, f.plan, x.h, x.p, true); err == nil {
		t.Fatal("changed EMA retained old approval")
	}
}

// TestNativeHistoryRecoveryV2CliAuthority is a table of expected refusals; the
// sole accepted capture carries no apply hash or process side effect.
func TestNativeHistoryRecoveryV2CliAuthority(t *testing.T) {
	base := []string{"native-history-recovery", "--provisional-resume", "--plan-hash", "0x" + strings.Repeat("11", 32), "--first-native-epoch", "25", "--native-history-recovery-source", "/reviewed/campaign-attempts/source.json", "--owned-rpc-authority", "192.0.2.1:9944"}
	// The command's owned-route test uses a private documentation fixture below.
	base[len(base)-1] = "10.0.0.1:9944"
	if _, _, err := parseCLI(base); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range [][]string{{"--apply"}, {"--detach"}, {"--then-release-candidate"}, {"--prepare-only"}, {"--native-history-recovery-plan-hash", "0x" + strings.Repeat("22", 32)}} {
		if _, _, err := parseCLI(append(append([]string(nil), base...), suffix...)); err == nil {
			t.Fatal("mixed native recovery CLI authority accepted", suffix)
		}
	}
}

// TestNativeHistoryRecoveryV2DryRunAuthorityDoesNotWrite ensures the common
// provisional setup does not persist invocation state during recovery capture.
func TestNativeHistoryRecoveryV2DryRunAuthorityDoesNotWrite(t *testing.T) {
	x := newNativeHistoryRecoveryTestV2(t)
	f := x.f
	before, err := os.ReadDir(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	o := cliOptions{ProvisionalResume: true, PlanHash: f.plan.PlanHash, FirstNativeEpoch: 25, NativeRecoverySource: x.p.Terminal.Path}
	if err := prepareProvisionalResume(t.Context(), f.cfg, f.stateDir, "native-history-recovery", o, f.plan); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadDir(f.stateDir)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("read-only capture wrote live provenance", err)
	}
	if !f.cfg.provisionalResume.Record.ReadOnly || f.cfg.provisionalResume.RecordPath != "" {
		t.Fatal("dry-run acquired a writable authority")
	}
	var printed bytes.Buffer
	if err := writeJSONResult(&printed, x.p); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "review-plan.json")
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, printed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	var reloaded nativeHistoryRecoveryPlanV2
	if _, err := readRuntimeEvidenceSetupV2(t.Context(), path, nativeHistoryRecoveryMaximumBytesV2, &reloaded); err != nil || !reflect.DeepEqual(reloaded, *x.p) {
		t.Fatal("printed dry-run plan cannot be applied byte-for-byte", err)
	}
}

// TestNativeHistoryRecoveryV2AuthenticatesRealSealedPredecessor reuses a full
// signed terminal fixture, preserving its failures and rejecting changed bytes.
func TestNativeHistoryRecoveryV2AuthenticatesRealSealedPredecessor(t *testing.T) {
	x := newProvisionalProductionTestFixture(t)
	c := x.history.campaign
	terminal, result, runId, err := authenticateNativeRecoveryTerminalV2(t.Context(), c.cfg, c.stateDir, c.current, x.history.attempt.path())
	if err != nil || terminal.SHA256 != policyRolloverFile(terminal.Path, x.source[x.history.attempt.path()]).SHA256 || result.SHA256 != policyRolloverFile(result.Path, x.source[filepath.Join(x.runDir, "result.json")]).SHA256 || runId != x.result.RunID {
		t.Fatal("real sealed predecessor lost its identity", err)
	}
	if err := os.WriteFile(result.Path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := authenticateNativeRecoveryTerminalV2(t.Context(), c.cfg, c.stateDir, c.current, terminal.Path); err == nil {
		t.Fatal("changed terminal result was accepted")
	}
}

// TestNativeHistoryRecoveryV2ReadOnlyLockRequiresExistingCustody verifies real
// exclusion and no file creation without relying on timing or fake callbacks.
func TestNativeHistoryRecoveryV2ReadOnlyLockRequiresExistingCustody(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if close, err := lockNativeHistoryRecoveryV2(root); err == nil {
		_ = close()
		t.Fatal("read-only capture created a missing deployment lock")
	}
	path := filepath.Join(root, "deployment.lock")
	original := []byte("synthetic-existing-owner")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockNativeHistoryRecoveryV2(root)
	if err != nil {
		t.Fatal(err)
	}
	if close, err := lockNativeHistoryRecoveryV2(root); err == nil {
		_ = close()
		t.Fatal("second owner entered capture exclusion")
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, original) {
		t.Fatal("read-only exclusion changed the lock file", err)
	}
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".retained", path); err != nil {
		t.Fatal(err)
	}
	if close, err := lockNativeHistoryRecoveryV2(root); err == nil {
		_ = close()
		t.Fatal("capture accepted a symlinked deployment owner")
	}
}

// TestNativeHistoryRecoveryV2CannotRetargetApprovalDuringVerification keeps
// the exact approved digest immutable across the external native boundary.
func TestNativeHistoryRecoveryV2CannotRetargetApprovalDuringVerification(t *testing.T) {
	x := newNativeHistoryRecoveryTestV2(t)
	f := x.f
	originalHash := x.p.PlanHash
	_, err := publishNativeHistoryRecoveryV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, x.h, x.p, func(_ context.Context, p *nativeHistoryRecoveryPlanV2) error {
		p.FirstNativeEpoch++
		for index := range p.Validators {
			p.Validators[index].Adoption.FirstNativeEpoch = p.FirstNativeEpoch
		}
		p.PlanHash, _ = p.hash()
		return nil
	})
	if err == nil {
		t.Fatal("native verification retargeted an already approved first epoch")
	}
	for _, hash := range []string{originalHash, x.p.PlanHash} {
		if _, err := os.Stat(nativeHistoryRecoveryRootV2(f.stateDir, hash)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("changed approval published recovery files", err)
		}
	}
}
