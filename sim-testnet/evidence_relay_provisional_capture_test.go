//go:build linux || darwin

package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/signature"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

func TestProvisionalRelayCaptureRequiresExactReadOnlyOwnedApproval(t *testing.T) {
	cfg, plan, stateDir, _, original := provisionalRuntimePlanFixtureWithOwnedRpc(t, "192.168.50.20:9944")
	options := cliOptions{ProvisionalCapture: true, PlanHash: plan.PlanHash, OwnedRPCAuthority: cfg.ownedRPCAuthority, RelayEndBlock: 10000}
	for _, change := range []struct {
		name string
		edit func(*cliOptions)
	}{
		{name: "source", edit: func(o *cliOptions) { o.PlanHash = "0x" + strings.Repeat("ef", 32) }},
		{name: "apply", edit: func(o *cliOptions) { o.Apply = true }},
		{name: "resume", edit: func(o *cliOptions) { o.ProvisionalResume = true }},
		{name: "public", edit: func(o *cliOptions) { o.OwnedRPCAuthority = "" }},
		{name: "other node", edit: func(o *cliOptions) { o.OwnedRPCAuthority = "192.168.50.21:9944" }},
		{name: "import", edit: func(o *cliOptions) { o.RelayContinuationPlan = "/synthetic/other.json" }},
	} {
		changed := options
		change.edit(&changed)
		if _, _, err := prepareProvisionalRelayCapture(t.Context(), cfg, stateDir, changed); err == nil {
			t.Fatalf("capture admitted %s", change.name)
		}
	}
	if _, err := os.Stat(filepath.Join(stateDir, "provisional-resumes")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected capture wrote provenance", err)
	}
	reader, retained, err := prepareProvisionalRelayCapture(t.Context(), cfg, stateDir, options)
	if err != nil {
		t.Fatal(err)
	}
	if !reader.readOnlyAudit || !reader.provisionalResume.Record.ReadOnly || reader.provisionalResume.Record.FinalAcceptance || cfg.readOnlyAudit || cfg.provisionalResume.Record != nil || retained.PlanHash != plan.PlanHash || retained.ReleaseLockHash != plan.ReleaseLockHash {
		t.Fatal("capture changed source approval or granted execution authority")
	}
	if err := validateProvisionalRelayCaptureContext(reader, retained); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPersistedPlan(reader, stateDir); !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatal("capture granted strict current release authority", err)
	}
	current, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil || !bytes.Equal(original, current) {
		t.Fatal("capture mutated active plan", err)
	}
	for _, name := range []string{"journal.jsonl", "deployment.lock", "supervisor.state.json"} {
		if _, err := os.Stat(filepath.Join(stateDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("capture created %s", name)
		}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := prepareProvisionalRelayCapture(cancelled, cfg, stateDir, options); !errors.Is(err, context.Canceled) {
		t.Fatal("capture lost cancellation", err)
	}
}

func TestProvisionalRelayCaptureCLIAndMutationEntrypointsStayReadOnly(t *testing.T) {
	args := []string{"relay-continuation", "--provisional-capture", "--plan-hash", "0x" + strings.Repeat("45", 32), "--owned-rpc-authority", "192.168.50.20:9944", "--relay-end-block", "10000", "--relay-slots", "2048"}
	_, options, err := parseCLI(args)
	if err != nil || executableAttestationModeForCommand("relay-continuation", options) != executableAttestationProvisionalResume {
		t.Fatal("capture did not require actual driver attestation", err)
	}
	for _, command := range []string{"setup", "resume", "launch", "scenario", "plan", "audit"} {
		changed := append([]string(nil), args...)
		changed[0] = command
		if _, _, err := parseCLI(changed); err == nil {
			t.Fatalf("capture flag granted %s authority", command)
		}
	}
	cfg := &ResolvedConfig{readOnlyAudit: true}
	executor := &Executor{cfg: cfg}
	if err := executor.Execute(t.Context(), Action{}); err == nil {
		t.Fatal("read-only capture executed an action")
	}
	if _, err := executor.admitEvidenceRelayAction(t.Context(), validatorcomponent.ValidatorEvidenceTransactionV2Expected{}); err == nil {
		t.Fatal("capture admitted a new relay debit")
	}
	if _, _, err := executor.admitOwnedEvidenceRelayAction(t.Context(), validatorcomponent.ValidatorEvidenceTransactionV2Expected{}); err == nil {
		t.Fatal("capture admitted a retained relay debit")
	}
	native := &SubstrateManager{cfg: cfg}
	if _, _, err := native.SendAsWithRecoveryPrecondition(t.Context(), "", Action{}, types.Call{}, signature.KeyringPair{}, nil); err == nil {
		t.Fatal("capture reached native signing or replay")
	}
	evm := &EvmTxManager{readOnly: true}
	if _, err := evm.prepareOwnedEVMTransaction(t.Context(), "", Action{}, nil, nil, nil, 0); err == nil {
		t.Fatal("capture reached EVM signing or replay")
	}
	if _, err := evm.waitExactTransaction(t.Context(), "", Action{}, nil); err == nil {
		t.Fatal("capture reached EVM broadcast")
	}
}

// The captured metadata bytes use the complete reviewed consumed profile; the
// successor's new runtime identity must be checked through real HTTP reads.
func newProvisionalRelayRuntime468Test(t *testing.T) (*runtimeEvidenceActivationRpcV2TestFixture, *evidenceRelayRuntime, protocol.ValidatorEvidenceActivation) {
	t.Helper()
	fixture := newRuntimeEvidenceActivationRpcV2TestFixture(t)
	encoded, err := os.ReadFile("../crv4/runtime-profile-v1.scale.gz.base64")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(io.LimitReader(reader, 347305))
	if err := errors.Join(err, reader.Close()); err != nil || len(raw) != 347304 {
		t.Fatal("profile fixture", err)
	}
	metadataHex := hexutil.Encode(raw)
	_, metadataHash, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	fixture.metadataHex = metadataHex
	fixture.nativeRuntime = crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 468, TransactionVersion: 1, StateVersion: 1}, CodeHash: types.Hash{0x68}.Hex(), MetadataHash: metadataHash}
	fixture.provisionalRuntimeAPIs = true
	fixture.stateLock.Unlock()
	executor := fixture.executor
	cfg, err := prepareOwnedRPCConfiguration(executor.cfg, "192.168.50.20:9944")
	if err != nil {
		t.Fatal(err)
	}
	executor.cfg = cfg
	cfg.Release = testReleaseLockFixture(t)
	cfg.Public.Chain.ExpectedRuntimeSpec = reviewedRuntimeSpecVersion
	executor.plan.OwnedRPCAuthority = cfg.ownedRPCAuthority
	provisional, _, _, _ := provisionalResumeTestContext(t)
	cfg.provisionalResume = provisional.provisionalResume
	cfg.readOnlyAudit = true
	options := cliOptions{ProvisionalCapture: true, PlanHash: executor.plan.PlanHash, OwnedRPCAuthority: cfg.ownedRPCAuthority, RelayEndBlock: 10000}
	if err := prepareProvisionalResume(t.Context(), cfg, executor.stateDir, "relay-continuation", options, executor.plan); err != nil {
		t.Fatal(err)
	}
	if err := enableProvisionalRuntimeCompatibility(executor.substrate.chain, cfg); err != nil {
		t.Fatal(err)
	}
	activation := protocol.ValidatorEvidenceActivation{NativeBlock: fixture.nativeNumber, NativeHash: [32]byte(fixture.nativeHash), Hotkey: fixture.hotkeys[1]}
	return fixture, &evidenceRelayRuntime{executor: executor}, activation
}

func TestProvisionalRelayCaptureRuntime468SnapshotCannotAuthorizeCurrentOperation(t *testing.T) {
	fixture, relay, activation := newProvisionalRelayRuntime468Test(t)
	chain := fixture.executor.substrate.chain
	metadata, runtime, current := chain.Meta, chain.Runtime, relay.executor.runtimeEvidenceNativeIdentityV2()
	if epoch, err := relay.readHorizonNative(t.Context(), activation, evidenceRelayNativePreviewSnapshot); err != nil || epoch != 77 {
		t.Fatalf("compatible468 preview epoch=%d error=%v", epoch, err)
	}
	if fixture.callCount("state_call") == 0 {
		t.Fatal("preview skipped independent non-self stake and schedule")
	}
	for _, mode := range []evidenceRelayNativeReadMode{evidenceRelayNativeCurrentHead, evidenceRelayNativeCurrentSnapshot} {
		if _, err := relay.readHorizonNative(t.Context(), activation, mode); err == nil {
			t.Fatalf("preview granted current-operation mode%d", mode)
		}
	}
	if chain.Meta != metadata || chain.Runtime != runtime || relay.executor.runtimeEvidenceNativeIdentityV2() != current || chain.CurrentRuntimeCompatibilityProfile() != "" {
		t.Fatal("preview changed current signing identity")
	}
	files, err := os.ReadDir(filepath.Join(filepath.Dir(relay.executor.cfg.provisionalResume.RecordPath), "runtime-compatibility"))
	if err != nil || len(files) != 1 {
		t.Fatal("preview did not durably record its exact runtime", err)
	}
	for _, change := range []struct {
		name string
		edit func()
	}{
		{name: "transaction", edit: func() { fixture.nativeRuntime.Version.TransactionVersion = 2 }},
		{name: "state", edit: func() { fixture.nativeRuntime.Version.StateVersion = 2 }},
		{name: "foreign runtime", edit: func() { fixture.nativeRuntime.Version.SpecName = "synthetic-other" }},
		{name: "permit", edit: func() { fixture.permits[1] = false }},
		{name: "stake", edit: func() { fixture.totalStake[1] = 0 }},
	} {
		fixture.stateLock.Lock()
		fixture.nativeRuntime.Version = crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 468, TransactionVersion: 1, StateVersion: 1}
		fixture.permits[1], fixture.totalStake[1] = true, 150
		change.edit()
		fixture.stateLock.Unlock()
		if _, err := relay.readHorizonNative(t.Context(), activation, evidenceRelayNativePreviewSnapshot); err == nil {
			t.Fatalf("warm preview admitted %s", change.name)
		}
	}
}

func TestProvisionalRelayCaptureMarkerRejectsAcceptanceAndTamperedProvenance(t *testing.T) {
	cfg, plan, stateDir, _, _ := provisionalRuntimePlanFixtureWithOwnedRpc(t, "192.168.50.20:9944")
	options := cliOptions{ProvisionalCapture: true, PlanHash: plan.PlanHash, OwnedRPCAuthority: cfg.ownedRPCAuthority, RelayEndBlock: 10000}
	reader, _, err := prepareProvisionalRelayCapture(t.Context(), cfg, stateDir, options)
	if err != nil {
		t.Fatal(err)
	}
	preview := *plan
	preview.PriorPlanHashes = append(append([]string(nil), plan.PriorPlanHashes...), plan.PlanHash)
	preview.EvidenceRelayContinuation = &EvidenceRelayContinuation{SourcePlanHash: plan.PlanHash, ProvisionalCapture: &EvidenceRelayProvisionalCapture{Record: *reader.provisionalResume.Record, RecordPath: reader.provisionalResume.RecordPath, RecordSHA256: reader.provisionalResume.RecordHash}}
	if err := validateProvisionalRelayCaptureSource(stateDir, &preview); err != nil {
		t.Fatal(err)
	}
	capture := preview.EvidenceRelayContinuation.ProvisionalCapture
	capture.Record.FinalAcceptance = true
	if err := validateProvisionalRelayCaptureMarker(&preview); err == nil {
		t.Fatal("preview claimed strict acceptance")
	}
	capture.Record.FinalAcceptance = false
	raw, err := os.ReadFile(capture.RecordPath)
	if err != nil {
		t.Fatal(err)
	}
	var changed provisionalResumeRecord
	if err := json.Unmarshal(raw, &changed); err != nil {
		t.Fatal(err)
	}
	changed.Driver.Build.Revision = strings.Repeat("cd", 20)
	tampered, err := json.MarshalIndent(changed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(capture.RecordPath, append(tampered, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateProvisionalRelayCaptureSource(stateDir, &preview); err == nil {
		t.Fatal("warm capture trusted modified actual-driver provenance")
	}
}

func TestProvisionalRelayCaptureStrictReconciliationDoesNotGrantFinalAcceptance(t *testing.T) {
	fixture, relay := provisionalRelayContinuationRuntimeTest(t)
	executor := fixture.executor
	cfg, err := prepareOwnedRPCConfiguration(executor.cfg, "192.168.50.20:9944")
	if err != nil {
		t.Fatal(err)
	}
	cfg.provisionalResume = nil
	plan := *executor.plan
	plan.OwnedRPCAuthority = cfg.ownedRPCAuthority
	plan.ResolvedInputsHash, err = resolvedInputsHash(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan.ReleaseLockHash, err = canonicalHashHex(cfg.Release)
	if err != nil {
		t.Fatal(err)
	}
	provenance, _, _, _ := provisionalResumeTestContext(t)
	provenance.ConfigHash = cfg.ConfigHash
	provenance.Release = cfg.Release
	provenance.ChainID = cfg.ChainID
	options := cliOptions{ProvisionalCapture: true, PlanHash: plan.EvidenceRelayContinuation.SourcePlanHash, OwnedRPCAuthority: cfg.ownedRPCAuthority, RelayEndBlock: 10000}
	source := plan
	source.PlanHash = options.PlanHash
	if err := prepareProvisionalResume(t.Context(), provenance, executor.stateDir, "relay-continuation", options, &source); err != nil {
		t.Fatal(err)
	}
	c := *plan.EvidenceRelayContinuation
	c.ProvisionalCapture = &EvidenceRelayProvisionalCapture{Record: *provenance.provisionalResume.Record, RecordPath: provenance.provisionalResume.RecordPath, RecordSHA256: provenance.provisionalResume.RecordHash}
	plan.EvidenceRelayContinuation = &c
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(&plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(executor.stateDir, "plan.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPersistedPlan(cfg, executor.stateDir); !errors.Is(err, errPersistedPlanIdentityMismatch) || !strings.Contains(err.Error(), "relay_capture_requires_strict_reconciliation") {
		t.Fatal("pending preview granted ordinary strict acceptance", err)
	}
	reader, retained, err := prepareStrictRelayCapture(cfg, executor.stateDir)
	if err != nil {
		t.Fatal("strict read-only reconciliation could not retain the exact source", err)
	}
	if retained.PlanHash != plan.PlanHash || !reader.readOnlyAudit || reader.provisionalResume != nil || cfg.readOnlyAudit || cfg.relayCapturePlanHash != "" {
		t.Fatal("strict reconciliation escaped its read-only source scope")
	}
	if _, err := loadPlanIdentityBytes(cfg, raw, true); err != nil {
		t.Fatal("exact provisional adoption lost a valid pending capture", err)
	}
	strict := *reader
	strict.relayCapturePlanHash = "0x" + strings.Repeat("ab", 32)
	if _, err := loadPersistedPlan(&strict, executor.stateDir); err == nil {
		t.Fatal("reconciliation admitted another plan")
	}
	changedRelease := *cfg.Release
	changedRelease.SchemaVersion++
	strict.Release = &changedRelease
	strict.relayCapturePlanHash = plan.PlanHash
	if _, err := loadPersistedPlan(&strict, executor.stateDir); err == nil {
		t.Fatal("strict reconciliation borrowed a mismatched release")
	}
	if relay.executor.journal == nil {
		t.Fatal("fixture lost its authenticated source journal")
	}
}
