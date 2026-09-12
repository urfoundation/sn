//go:build linux || darwin

// These capture-boundary tests use the real renderer/private descriptors and
// immutable archive. Structural custody is never labeled semantic acceptance.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	validatorpkg "github.com/urfoundation/sn/validator"
)

// Bounded actual archive entries exercise only structural/raw custody. The
// intentionally incomplete source payloads cannot pass any semantic builder.
func newFinalCaptureV2ShapeTestFixture(t *testing.T) (*ResolvedConfig, *FinalSemanticCollectedInputs, string) {
	t.Helper()
	cfg := testResolvedConfig(t)
	configureRuntimeEvidenceV2Test(t, cfg, t.TempDir())
	runRoot := t.TempDir()
	put := func(kind, name string) FinalArtifactLocator {
		raw := []byte(name + "\n")
		locator, err := persistFinalCollectedArtifact(runRoot, kind, "final-inputs/"+name+".bin", raw)
		if err != nil {
			t.Fatal(err)
		}
		return locator
	}
	paths := []FinalOperatorPathIdentity{{NoID: 1, PathVPK: fmt.Sprintf("0x%x", [32]byte{1})}, {NoID: 2, PathVPK: fmt.Sprintf("0x%x", [32]byte{2})}}
	collected := FinalCollectedValidatorInputs{ValidatorID: 1, PathVPK: paths[0].PathVPK, OperatorPaths: paths,
		IntentStore: put("validator-steering-intent-store", "store"),
		EvidenceV2:  &FinalCollectedValidatorEvidenceV2{Schema: finalCollectedValidatorEvidenceV2Schema, SemanticStatus: "pending_offline_verification", Hotkey: fmt.Sprintf("0x%x", [32]byte{3}), Origins: [2]string{cfg.OperatorAPIOrigins[0], cfg.OperatorAPIOrigins[1]}}}
	for _, kind := range []string{"setup", "private", "native-rpc", "closed-census", "signed-evidence", "terminal-payload", "relay-journal", "relay-request", "relay-result", "relay-winner"} {
		collected.EvidenceV2.Sources = append(collected.EvidenceV2.Sources, FinalCollectedValidatorSourceV2{Source: validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: kind, Name: "original"}, Artifact: put("validator-evidence-v2-source", kind)})
	}
	sort.Slice(collected.EvidenceV2.Sources, func(i, j int) bool {
		return finalCaptureSourceV2Key(collected.EvidenceV2.Sources[i].Source) < finalCaptureSourceV2Key(collected.EvidenceV2.Sources[j].Source)
	})
	collected.Intents = []FinalCollectedValidatorIntent{{Sequence: 1, SettlementEpoch: 9, SubnetEpoch: 20, Status: "applied", VectorHash: common.Hash{4}.Hex(), Artifact: put("steering-intent", "intent"), Measurement: put("validator-release-measurement", "measurement"), Envelope: put("validator-release-measurement-envelope", "envelope")}}
	collected.EvidenceV2.Closures = []FinalCollectedSettlementClosure{{Epoch: 9, Boundary: ChainHead{Number: 109, Hash: common.Hash{5}.Hex()}, Artifact: put("validator-evidence-v2-source", "closure")}}
	value := &FinalSemanticCollectedInputs{Schema: finalSemanticCollectedInputsSchema, Phase: "release-1.0", RunID: "capture-v2", ResultHash: common.Hash{6}.Hex(), EvidenceHash: common.Hash{7}.Hex(), Window: ScenarioAcceptanceWindow{FirstEpoch: 9, EpochCount: 1, EpochBlocks: 10, StartBlock: 100}, Validators: []FinalCollectedValidatorInputs{collected}}
	if err := verifyFinalCollectedValidatorEvidenceV2(cfg, value, collected); err != nil {
		t.Fatal(err)
	}
	return cfg, value, runRoot
}

// The exact immutable setup and real renderer produce the source references;
// replacing a rendered public origin cannot redefine the approved census.
func TestFinalCaptureV2ReadsActualRenderedSetupAndRejectsChangedSource(t *testing.T) {
	t.Parallel()
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	// The source origins were selected before the fixture signed its plan.
	// Replacing resolved inputs afterward would test plan drift, not capture.
	approved, err := loadPersistedPlan(fixture.cfg, fixture.stateDir)
	if err != nil || approved.PlanHash != fixture.plan.PlanHash {
		t.Fatalf("original approved setup plan: %v", err)
	}
	executor := &Executor{cfg: fixture.cfg, plan: fixture.plan, roles: fixture.roles, stateDir: fixture.stateDir}
	if err := executor.retainRuntimeEvidenceInputsV2(t.Context(), fixture.prepared, fixture.preparedBytes, fixture.completed); err != nil {
		t.Fatal(err)
	}
	deployment := fixture.plan.Deployment
	deployment.DeployBlock = 100
	deployment.DeployBlockHash = common.Hash{8}.Hex()
	deployment.CoordinatorEventStartBlock = 100
	deployment.CoordinatorEventStartBlockHash = deployment.DeployBlockHash
	if err := saveContractDeployment(fixture.stateDir, deployment); err != nil {
		t.Fatal(err)
	}
	if err := renderValidatorMinerConfigs(fixture.cfg, fixture.stateDir, fixture.roles, &deployment); err != nil {
		t.Fatal(err)
	}
	supervisor := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: fixture.plan.DeploymentID}
	for id := 1; id <= 2; id++ {
		supervisor.Specs = append(supervisor.Specs, ProcessSpec{ID: fmt.Sprintf("validator-%d", id), Role: "validator"})
	}
	supervisorBytes, err := json.Marshal(supervisor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.stateDir, "supervisor.json"), supervisorBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	for validatorId := uint64(1); validatorId <= 2; validatorId++ {
		release, raw, err := finalReleaseCaptureConfigV2(t.Context(), fixture.cfg, fixture.stateDir, validatorId)
		if err != nil || release == nil || release.ValidatorID != validatorId {
			t.Fatalf("actual rendered source %d: %v", validatorId, err)
		}
		path := filepath.Join(fixture.stateDir, "runtime", fmt.Sprintf("validator-%d", validatorId), "validator.yml")
		original, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(original, raw) {
			t.Fatal("capture normalized the original rendered bytes")
		}
		changed := bytes.Replace(raw, []byte(fixture.cfg.OperatorAPIOrigins[0]), []byte("https://unapproved-capture.example"), 1)
		if bytes.Equal(raw, changed) {
			t.Fatal("actual renderer did not contain its configured source")
		}
		if err := os.WriteFile(path, changed, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := finalReleaseCaptureConfigV2(t.Context(), fixture.cfg, fixture.stateDir, validatorId); err == nil {
			t.Fatal("changed rendered origin redefined source custody")
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// A changed resolved origin cannot repair an already approved plan identity.
// The actual disk loader refuses it without rewriting any retained source.
func TestFinalCaptureV2ApprovedPlanRejectsChangedResolvedOrigin(t *testing.T) {
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	before := validatorNamespaceTreeSnapshot(t, fixture.stateDir)
	approved, err := loadPersistedPlan(fixture.cfg, fixture.stateDir)
	if err != nil || approved.PlanHash != fixture.plan.PlanHash {
		t.Fatalf("original approved plan: %v", err)
	}
	fixture.cfg.OperatorAPIOrigins = append([]string(nil), fixture.cfg.OperatorAPIOrigins...)
	fixture.cfg.OperatorAPIOrigins[0] = "https://unapproved-capture.example"
	if plan, err := loadPersistedPlan(fixture.cfg, fixture.stateDir); plan != nil || !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatalf("changed origin redefined approved setup: %v %v", plan, err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, fixture.stateDir)) {
		t.Fatal("origin refusal rewrote the plan or retained setup")
	}
}

// A real archived shape counts sources, not candidate trail/proof summaries.
func TestFinalCaptureV2PendingStatusCountsRawSourcesOnly(t *testing.T) {
	_, value, runRoot := newFinalCaptureV2ShapeTestFixture(t)
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := persistFinalCollectedArtifact(runRoot, "final-semantic-input-manifest", "final-inputs/manifest.json", raw)
	if err != nil {
		t.Fatal(err)
	}
	result := &ScenarioResult{Name: value.Phase, RunID: value.RunID, EvidenceHash: value.ResultHash, CompletedAt: time.Unix(100, 0).UTC().Format(time.RFC3339Nano)}
	status := finalSemanticCaptureStatus(result, value, manifest)
	status.EvidenceHash, err = finalSemanticCaptureStatusHash(status)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalSemanticCaptureStatus(status, value); err != nil {
		t.Fatal(err)
	}
	if status.CompactSourceCount != 10 || status.AttemptRecordCount != 0 || status.PathProofCount != 0 || status.SemanticStatus != "pending_offline_verification" {
		t.Fatalf("raw custody was projected into legacy acceptance: %+v", status)
	}
	status.AttemptRecordCount = 1
	status.EvidenceHash, err = finalSemanticCaptureStatusHash(status)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalSemanticCaptureStatus(status, value); err == nil {
		t.Fatal("rehashing a fabricated legacy count changed raw capture into scoring")
	}
}

// Mixed representations cannot silently route one validator around V2.
func TestFinalCaptureV2RejectsMixedLegacyStatus(t *testing.T) {
	_, value, runRoot := newFinalCaptureV2ShapeTestFixture(t)
	legacy := value.Validators[0]
	legacy.ValidatorID = 2
	legacy.EvidenceV2 = nil
	value.Validators = append(value.Validators, legacy)
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := persistFinalCollectedArtifact(runRoot, "final-semantic-input-manifest", "final-inputs/manifest.json", raw)
	if err != nil {
		t.Fatal(err)
	}
	status := finalSemanticCaptureStatus(&ScenarioResult{Name: value.Phase, RunID: value.RunID, EvidenceHash: value.ResultHash, CompletedAt: time.Unix(100, 0).UTC().Format(time.RFC3339Nano)}, value, manifest)
	status.EvidenceHash, err = finalSemanticCaptureStatusHash(status)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalSemanticCaptureStatus(status, value); err == nil {
		t.Fatal("mixed legacy and compact capture was admitted")
	}
}

// Positive shape is established first; the overflowing range would otherwise
// wrap to an unrelated retained boundary with entirely valid content hashes.
func TestFinalCaptureV2RejectsOverflowingTerminalBoundary(t *testing.T) {
	cfg, value, _ := newFinalCaptureV2ShapeTestFixture(t)
	value.Window.StartBlock = math.MaxUint64 - 4
	value.Validators[0].EvidenceV2.Closures[0].Boundary.Number = 4
	if err := verifyFinalCollectedValidatorEvidenceV2(cfg, value, value.Validators[0]); err == nil || !strings.Contains(err.Error(), "overflows") {
		t.Fatalf("wrapped terminal boundary was admitted: %v", err)
	}
}

// Successful public slots require their actual request/result/winner custody.
func TestFinalCaptureV2RequiresRelaySourceCensus(t *testing.T) {
	cfg, value, _ := newFinalCaptureV2ShapeTestFixture(t)
	for index, source := range value.Validators[0].EvidenceV2.Sources {
		if source.Source.Kind == "relay-result" {
			value.Validators[0].EvidenceV2.Sources = append(value.Validators[0].EvidenceV2.Sources[:index], value.Validators[0].EvidenceV2.Sources[index+1:]...)
			break
		}
	}
	if err := verifyFinalCollectedValidatorEvidenceV2(cfg, value, value.Validators[0]); err == nil || !strings.Contains(err.Error(), "relay-result") {
		t.Fatalf("raw capture omitted its original result: %v", err)
	}
}

// A capture-only source is rejected by the actual downstream builder before
// missing materialized inputs can be used to invent a legacy representation.
func TestFinalCaptureV2LegacyBuilderRejectsPendingCompactSources(t *testing.T) {
	cfg, value, _ := newFinalCaptureV2ShapeTestFixture(t)
	archive := &finalSemanticArchive{collected: value}
	result, err := buildFinalSemanticSourceFromArchive(t.Context(), cfg, archive, &ScenarioResult{}, &ScenarioObservation{}, []*ScenarioObservation{{}})
	if err == nil || result != nil || !strings.Contains(err.Error(), "independent V2 final semantic replay") {
		t.Fatalf("raw capture bypassed final semantic acceptance: %v", err)
	}
}

// Cancellation precedes any private authority read or public availability poll.
func TestFinalCaptureV2TerminalWaitCancellationPrecedesReads(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.ValidatorEvidenceV2 = []validatorpkg.ReleaseValidatorEvidenceV2Config{{ValidatorID: 1}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := waitFinalValidatorSettlementClosuresWithWait(ctx, cfg, filepath.Join(t.TempDir(), "absent"), &ScenarioObservation{}, &ScenarioAcceptanceWindow{}, time.Now().Add(time.Hour), time.Second, func(context.Context, time.Duration) error { t.Fatal("canceled wait polled"); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled collector touched unavailable source state: %v", err)
	}
}

// Companion capture cannot replace missing finalized action receipts with
// the deployment manifest's candidate address or a generic six-contract view.
func TestFinalCaptureV2CompanionRequiresOriginalJournalBeforeRpc(t *testing.T) {
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	terminal := &ScenarioObservation{Status: &DeploymentStatus{Contracts: &ContractView{}}}
	_, err := captureFinalCompanionInputsV2(t.Context(), fixture.cfg, fixture.stateDir, t.TempDir(), terminal)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("companion capture invented missing original journal/receipts: %v", err)
	}
}

// Chunked companion files must keep the same closed archive namespace.
func TestFinalCaptureV2CompanionChunkClassPreservesSourceIdentity(t *testing.T) {
	for _, name := range []string{"validator-evidence-companion", "validator-evidence-companion-001-of-002", "validator-evidence-companion-002-of-002"} {
		if got := finalSemanticBundleClass(name); got != "validator-evidence-companion" {
			t.Errorf("companion chunk %s changed source identity to %s", name, got)
		}
	}
}

// The small structural fixtures use the same real config/lock/key factories
// as the parallel roots. Mutating one owner cannot change another's inputs.
func TestFinalCaptureV2PrivateFixtureInputsAreDetached(t *testing.T) {
	t.Parallel()
	left, leftInputs, leftRoot := newFinalCaptureV2ShapeTestFixture(t)
	right, rightInputs, rightRoot := newFinalCaptureV2ShapeTestFixture(t)
	if leftRoot == rightRoot || left == right || left.Config == right.Config || left.Public == right.Public || left.Policy == right.Policy || left.Release == right.Release || left.Hyperparameters == right.Hyperparameters {
		t.Fatal("capture fixtures share a writable owner")
	}
	leftRoles, err := BuildRoleSecrets(left)
	if err != nil {
		t.Fatal(err)
	}
	rightRoles, err := BuildRoleSecrets(right)
	if err != nil {
		t.Fatal(err)
	}
	client := rightRoles.Clients["miner-1"]
	artifact := rightRoles.EVM["testnet-owner"]
	hotkey := rightRoles.Substrate[validatorHotkeyLabel(1)]
	delete(leftRoles.Clients, "miner-1")
	delete(leftRoles.EVM, "testnet-owner")
	delete(leftRoles.Substrate, validatorHotkeyLabel(1))
	freshRoles, err := BuildRoleSecrets(right)
	if err != nil || rightRoles.Clients["miner-1"] != client || freshRoles.Clients["miner-1"] != client || rightRoles.EVM["testnet-owner"] != artifact || freshRoles.EVM["testnet-owner"] != artifact || rightRoles.Substrate[validatorHotkeyLabel(1)] != hotkey || freshRoles.Substrate[validatorHotkeyLabel(1)] != hotkey {
		t.Fatalf("private fixture mutation reached a sibling or cached signer: %v", err)
	}
	miners, epochBlocks := right.Config.Topology.Miners, right.Policy.Settlement.EpochBlocks
	origin := right.OperatorAPIOrigins[0]
	buildHash := right.Release.EVMBuild["abi_hash"]
	tempo := right.Hyperparameters.OwnerControlled["tempo"]
	noId := right.Config.ValidatorEvidenceV2[0].Evidence.Operators[0].NoID
	left.Config.Topology.Miners = 0
	left.Policy.Settlement.EpochBlocks++
	left.OperatorAPIOrigins[0] = "https://changed-owned.example"
	left.Release.EVMBuild["abi_hash"] = "changed-owned-build"
	left.Hyperparameters.OwnerControlled["tempo"] = -1
	left.Config.ValidatorEvidenceV2[0].Evidence.Operators[0].NoID = 0
	if right.Config.Topology.Miners != miners || right.Policy.Settlement.EpochBlocks != epochBlocks || right.OperatorAPIOrigins[0] != origin || right.Release.EVMBuild["abi_hash"] != buildHash || right.Hyperparameters.OwnerControlled["tempo"] != tempo || right.Config.ValidatorEvidenceV2[0].Evidence.Operators[0].NoID != noId {
		t.Fatal("private fixture mutation reached a sibling configuration")
	}
	for _, paths := range [][2]string{
		{filepath.Join(leftRoot, filepath.FromSlash(leftInputs.Validators[0].IntentStore.URI)), filepath.Join(rightRoot, filepath.FromSlash(rightInputs.Validators[0].IntentStore.URI))},
		{left.Config.ValidatorEvidenceV2[0].Evidence.Operators[0].Activation.Path, right.Config.ValidatorEvidenceV2[0].Evidence.Operators[0].Activation.Path},
	} {
		leftInfo, err := os.Stat(paths[0])
		if err != nil {
			t.Fatal(err)
		}
		rightInfo, err := os.Stat(paths[1])
		if err != nil || os.SameFile(leftInfo, rightInfo) {
			t.Fatalf("capture fixtures alias a retained input: %v", err)
		}
		before, err := os.ReadFile(paths[1])
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths[0], []byte("changed-private-owner\n"), leftInfo.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(paths[1])
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("private capture mutation changed sibling retained bytes: %v", err)
		}
	}
}
