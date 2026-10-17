// Signed original-config controls exercise real provider proof, source and
// runtime readers across renewal. No history bool or simulated verdict is used.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
	"gopkg.in/yaml.v3"
)

// A separately provisioned source reference can disappear after durable
// retention. The next complete config and approval are signed independently.
func productionAuthorityTestSuccessor(t *testing.T, original *ReleaseConfig, approved OwnerRecycleApproval, private ed25519.PrivateKey, changeRuntime bool) (*ReleaseConfig, OwnerRecycleApproval) {
	t.Helper()
	raw, err := BuildOwnerRecycleProductionAuthority(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := WriteReleaseEvidenceV2File(t.Context(), filepath.Join(identityTestStateDir(t), "original-authority.json"), raw, maximumProductionAuthorityBundleBytes)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var cfg ReleaseConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(approved)
	if err != nil {
		t.Fatal(err)
	}
	var approval OwnerRecycleApproval
	if err := json.Unmarshal(raw, &approval); err != nil {
		t.Fatal(err)
	}
	cfg.ProductionAuthorityHistory = append(cfg.ProductionAuthorityHistory, reference)
	cfg.OwnerRecycleApproval.Approval = ReleaseEvidenceV2File{Path: filepath.Join(identityTestStateDir(t), "successor-approval.json")}
	cfg.PollSeconds++
	approval.Production.ActivationNativeBlock = ownerRecycleActivationBlock(&approval)
	approval.ValidFromNativeBlock++
	if changeRuntime {
		cfg.RuntimeSpec++
		cfg.RuntimeCodeHash = releaseHex32([32]byte{0x95, byte(len(cfg.ProductionAuthorityHistory))})
		approval.Proposal.Runtime.Version.SpecVersion = cfg.RuntimeSpec
		approval.Proposal.Runtime.CodeHash, _ = parseHash32("synthetic successor code", cfg.RuntimeCodeHash)
		approval.RuntimeReviewHash = recycleTestId(4200 + uint16(len(cfg.ProductionAuthorityHistory)))
	}
	approval.ConfigHash, err = OwnerRecycleConfigHash(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	approver := recycleAdmissionFixture{cfg: &cfg, approval: approval, private: private}
	approver.sign(t)
	if err := loadOwnerRecycleProductionConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionRuntimeHistory(&cfg); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionAuthorityHistory(&cfg); err != nil {
		t.Fatal(err)
	}
	return &cfg, approval
}

// Runtime results depend on the exact requested block. Current accumulated
// emissions deliberately remain nonzero while the original drain stays zero.
func productionAuthorityTestUpgrade(t *testing.T, fixture *ownerRecycleProductionTestFixture, current *ReleaseConfig) {
	t.Helper()
	admission := fixture.operator.measurement.admission
	client := admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	original := client.callContext
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		assign := func(value any) error {
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, target)
		}
		if method == "state_getRuntimeVersion" && len(args) == 1 && args[0] != fixture.block(100).Hex() {
			return assign(map[string]any{"specName": "node-subtensor", "specVersion": current.RuntimeSpec, "transactionVersion": 1, "stateVersion": 1,
				"apis": []any{[]any{"0x8375104b299b74c5", 2}}})
		}
		if method == "state_getStorageHash" && len(args) == 2 && args[0] == "0x3a636f6465" && args[1] != fixture.block(100).Hex() {
			return assign(current.RuntimeCodeHash)
		}
		if method == "state_getStorage" && len(args) == 2 && args[0] == fixture.storageNameKVs["PendingServerEmission"] && args[1] != fixture.block(100).Hex() {
			return assign(codec.HexEncodeToString(binary.LittleEndian.AppendUint64(nil, 500)))
		}
		return original(ctx, target, method, args...)
	}
	fixture.head = 101
}

// The old real signed sidecar verifies under its retained complete authority
// after the current artifact changes. It never becomes a fresh signing config.
func TestProductionAuthorityHistoryReplaysOriginalAfterRuntimeUpgrade(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	measurement := fixture.operator.measurement
	current, _ := productionAuthorityTestSuccessor(t, fixture.cfg, measurement.admission.approval, measurement.admission.private, true)
	productionAuthorityTestUpgrade(t, fixture, current)
	original, err := productionConfigForIntent(current, intent)
	if err != nil {
		t.Fatal(err)
	}
	if original == current || !original.ownerRecycleProduction.historicalOnly || original.ownerRecycleProduction.configHash != fixture.cfg.ownerRecycleProduction.configHash {
		t.Fatal("history did not select the exact original read-only authority")
	}
	native := &crv4.Chain{API: measurement.admission.chain.API, GenesisHash: measurement.admission.chain.GenesisHash}
	replayed, err := prepareOwnerRecycleProductionDecision(t.Context(), original, native, fixture.operator.chain,
		measurement.encoded, measurement.provider.artifact, provider, measurement.provider.options(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(replayed.encoded, stage.encoded) {
		t.Fatal("historical authority changed the original source proof")
	}
	if _, err := verifyOwnerRecycleProductionIntent(t.Context(), original, replayed, intent, measurement.encoded, measurement.provider.artifact, provider); err != nil {
		t.Fatal(err)
	}
	if err := authenticateReleaseNativeSourceReferenceV2(t.Context(), native, current, intent, measurement.provider.artifact); err != nil {
		t.Fatal(err)
	}
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, current, fixture.block(101)); err != nil {
		t.Fatal(err)
	}
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, original, fixture.block(100)); err == nil {
		t.Fatal("old config acquired a fresh production purpose")
	}
	if ownerRecycleProductionBoundary(original) == nil {
		t.Fatal("original authority opened a current writer")
	}
	if _, err := sealOwnerRecycleProductionIntent(t.Context(), replayed, fixture.hotkey, intent.Prepared, intent.MeasurementEnvelopeHash); err == nil {
		t.Fatal("historical source authority signed another sidecar")
	}
	store := &IntentStore{v2: &releaseIntentV2Owner{ctx: t.Context(), runtime: &releaseRuntimeV2{cfg: *current}}}
	if err := store.retainOwnerRecyclePreparedAuthorization(intent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ownerRecyclePreparedConfig(t.Context(), current, intent.Prepared); err == nil {
		t.Fatal("old prepared grant was reissued under current approval")
	}
}

// Renewal preserves the existing drained activation and cumulative proof
// progress. Nonzero current pending emissions are not a reason to start over.
func TestProductionAuthorityHistoryContinuesWithoutAnotherDrain(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	first, _ := fixture.stage(t)
	measurement := fixture.operator.measurement
	current, _ := productionAuthorityTestSuccessor(t, fixture.cfg, measurement.admission.approval, measurement.admission.private, true)
	productionAuthorityTestUpgrade(t, fixture, current)
	fixture.cfg = current
	artifact := measurement.provider.artifact
	fixture.epoch = artifact.SubnetEpoch + 1
	artifact.PreviousArtifactHash = ReleaseMeasurementContentHash(measurement.encoded)
	artifact.SubnetEpoch, artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash = fixture.epoch, 101, fixture.block(101).Hex()
	for index := range artifact.Inputs {
		artifact.Inputs[index].CutNativeBlock, artifact.Inputs[index].CutNativeBlockHash = 101, fixture.block(101).Hex()
	}
	var err error
	measurement.encoded, _, err = SealReleaseMeasurementArtifactV2(t.Context(), artifact, measurement.provider.options(t))
	if err != nil {
		t.Fatal(err)
	}
	stage, provider := fixture.stage(t)
	if stage.proof.Eligibility.ActivationBlock != 100 || stage.proof.Eligibility.ActivationHash != first.proof.Eligibility.ActivationHash || stage.proof.Eligibility.PendingServerEmission != 0 {
		t.Fatal("renewal discarded the original drained activation")
	}
	intent := fixture.intent(t, stage, provider)
	if _, err := verifyOwnerRecycleProductionIntent(t.Context(), current, stage, intent, measurement.encoded, artifact, provider); err != nil {
		t.Fatal(err)
	}
	if len(current.ProductionRuntimeApprovals) != 0 {
		t.Fatal("fixture silently supplied duplicate runtime history")
	}
	actual, err := releaseProductionRuntimeAt(current, 100, true)
	if err != nil || actual.Version.SpecVersion+1 != current.RuntimeSpec {
		t.Fatalf("original runtime was not projected from signed bundle: %v", err)
	}
}

// The public loader authenticates both signatures after all original source
// paths disappear. Retention is repeatable and preserves the old content file.
func TestProductionAuthorityHistoryDefaultLoaderSurvivesSourceLoss(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, true)
	original := fixture.cfg
	current, _ := productionAuthorityTestSuccessor(t, original, fixture.approval, fixture.private, false)
	retained, err := RetainOwnerRecycleApproval(t.Context(), current)
	if err != nil {
		t.Fatal(err)
	}
	if repeated, err := RetainOwnerRecycleApproval(t.Context(), current); err != nil || repeated != retained {
		t.Fatalf("retention is not idempotent: %v", err)
	}
	for _, path := range []string{original.OwnerRecycleApproval.Approval.Path, current.OwnerRecycleApproval.Approval.Path,
		current.ProductionAuthorityHistory[0].Path, current.ProductionRuntimeApprovals[0].Path} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := yaml.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(identityTestStateDir(t), "current.yml")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadReleaseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.productionAuthorityHistory.entries) != 1 || !bytes.Equal(loaded.productionAuthorityHistory.entries[0].config.ownerRecycleProduction.encoded, original.ownerRecycleProduction.encoded) {
		t.Fatal("restart replaced or omitted original authority")
	}
	if _, err := os.Stat(retainedProductionAuthorityPath(current, current.ProductionAuthorityHistory[0].SHA256)); err != nil {
		t.Fatal(err)
	}
}

// Altered original bytes, current history selection or an unapproved economic
// change cannot create historical authority through a schema or hash alone.
func TestProductionAuthorityHistoryRejectsSubstitutionAndScopeChanges(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	current, _ := productionAuthorityTestSuccessor(t, fixture.cfg, fixture.approval, fixture.private, false)
	reference := current.ProductionAuthorityHistory[0]
	raw, err := os.ReadFile(reference.Path)
	if err != nil {
		t.Fatal(err)
	}
	var bundle releaseProductionAuthorityBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"approval", "config", "prefix", "schema"} {
		candidate := bundle
		candidate.Approval = bytes.Clone(bundle.Approval)
		candidate.Config = bytes.Clone(bundle.Config)
		switch fault {
		case "approval":
			candidate.Approval[len(candidate.Approval)/2] ^= 1
		case "config":
			var cfg ReleaseConfig
			if err := json.Unmarshal(candidate.Config, &cfg); err != nil {
				t.Fatal(err)
			}
			cfg.PollSeconds++
			candidate.Config, _ = json.Marshal(&cfg)
		case "prefix":
			var cfg ReleaseConfig
			if err := json.Unmarshal(candidate.Config, &cfg); err != nil {
				t.Fatal(err)
			}
			cfg.ProductionAuthorityHistory = []ReleaseEvidenceV2File{reference}
			candidate.Config, _ = json.Marshal(&cfg)
		case "schema":
			candidate.Schema = releaseProductionRuntimeApprovalSchema
		}
		encoded, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeProductionAuthorityBundle(append(encoded, '\n'), current, nil, 0); err == nil {
			t.Errorf("%s original bundle substitution admitted", fault)
		}
	}
	changed := *current
	changed.ProductionAuthorityHistory = nil
	if validateReleaseProductionAuthorityHistory(&changed) == nil {
		t.Fatal("edited history selection kept its private seal")
	}
	changed = *current
	changed.Policy.Steering.MaxWeightLimitU16--
	if validateProductionAuthorityContinuity(fixture.cfg, &changed) == nil {
		t.Fatal("runtime continuity changed economic policy")
	}
	if _, err := productionConfigForIntent(current, &SteeringIntent{OwnerRecycle: &OwnerRecycleProductionIntent{Proof: OwnerRecycleProductionProof{Approval: []byte("synthetic missing authority")}}}); err == nil {
		t.Fatal("unselected original approval was admitted")
	}
}

// Actual recovery reaches the canonical receipt reader using the original
// runtime. Its transport error is retained instead of becoming a config failure.
func TestProductionAuthorityHistoryPendingRecoveryUsesOriginalReader(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	intent.Status = "pending"
	measurement := fixture.operator.measurement
	current, _ := productionAuthorityTestSuccessor(t, fixture.cfg, measurement.admission.approval, measurement.admission.private, true)
	productionAuthorityTestUpgrade(t, fixture, current)
	client := measurement.admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	original := client.callContext
	canary := errors.New("synthetic retained receipt temporarily unavailable")
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if method == "chain_getBlock" {
			return canary
		}
		if strings.HasPrefix(method, "author_") {
			t.Fatal("receipt recovery attempted submission")
		}
		return original(ctx, target, method, args...)
	}
	steerer := &ReleaseSteerer{cfg: current, native: measurement.admission.chain, hotkey: fixture.hotkey}
	if _, err := steerer.reconcilePendingV2(t.Context(), intent, &crv4.EpochScheduleState{SubnetEpochIndex: intent.SubnetEpoch}); !errors.Is(err, canary) {
		t.Fatalf("old pending reader rejected original config or lost receipt failure: %v", err)
	}
	if intent.Status != "pending" || intent.Prepared.ExtrinsicHash == "" {
		t.Fatal("failed receipt read discarded original pending bytes")
	}
}

// Receipt authentication may finish after the original finite signing window.
// The source remains original; the independently approved later read window
// must reach actual inclusion evidence instead of rewriting its signed config.
func TestProductionAuthorityHistoryFinalityUsesApprovedLaterWindow(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	measurement := fixture.operator.measurement
	admission := measurement.admission
	admission.approval.ValidThroughNativeBlock = 100
	admission.sign(t)
	if err := loadOwnerRecycleProductionConfig(fixture.cfg); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionRuntimeHistory(fixture.cfg); err != nil {
		t.Fatal(err)
	}
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	approved := admission.approval
	approved.ValidThroughNativeBlock = 200
	current, _ := productionAuthorityTestSuccessor(t, fixture.cfg, approved, admission.private, false)
	fixture.head = 101
	intent.FinalizedBlock, intent.FinalizedBlockHash = 101, fixture.block(101).Hex()
	client := admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	original := client.callContext
	canary := errors.New("synthetic actual finalized source body unavailable")
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if method == "chain_getBlock" {
			if len(args) != 1 || args[0] != fixture.block(101).Hex() {
				t.Fatal("finalized source body escaped its exact receipt block")
			}
			return canary
		}
		if strings.HasPrefix(method, "author_") {
			t.Fatal("finalized source observation attempted a write")
		}
		return original(ctx, target, method, args...)
	}
	if err := authenticateReleaseNativeSourceReferenceV2(t.Context(), admission.chain, current, intent, measurement.provider.artifact); !errors.Is(err, canary) {
		t.Fatalf("original signing interval stranded later receipt evidence: %v", err)
	}
}

// A candidate's claimed application is not accepted from a status field. Its
// historical read reaches actual weight storage under the original artifact,
// and an unavailable row stays an error instead of becoming native success.
func TestProductionAuthorityHistoryApplicationReachesOriginalWeightReader(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	measurement := fixture.operator.measurement
	current, _ := productionAuthorityTestSuccessor(t, fixture.cfg, measurement.admission.approval, measurement.admission.private, true)
	productionAuthorityTestUpgrade(t, fixture, current)
	intent.Status, intent.RevealBlock, intent.ApplicationBlock, intent.ApplicationBlockHash = "applied", 100, 100, fixture.block(100).Hex()
	key, err := types.CreateStorageKey(fixture.metadata, crv4.PalletName, "Weights", binary.LittleEndian.AppendUint16(nil, current.Netuid), binary.LittleEndian.AppendUint16(nil, intent.SelfUID))
	if err != nil {
		t.Fatal(err)
	}
	client := measurement.admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	original := client.callContext
	canary := errors.New("synthetic original application row unavailable")
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if method == "state_getStorage" && len(args) == 2 && args[0] == key.Hex() {
			if args[1] != fixture.block(100).Hex() {
				t.Fatal("historical application row escaped its exact block")
			}
			return canary
		}
		if strings.HasPrefix(method, "author_") {
			t.Fatal("application observation attempted a write")
		}
		return original(ctx, target, method, args...)
	}
	if err := authenticateAdoptedIntentApplicationV2(t.Context(), measurement.admission.chain, current, intent); !errors.Is(err, canary) {
		t.Fatalf("runtime renewal rejected the original application reader or swallowed its error: %v", err)
	}
}

// Metadata and drain capture use original authority while the caller retains
// the successor config. Capture has no write-capable method and keeps exact data.
func TestProductionAuthorityHistoryCaptureRoutesOriginalSidecar(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	measurement := fixture.operator.measurement
	current, _ := productionAuthorityTestSuccessor(t, fixture.cfg, measurement.admission.approval, measurement.admission.private, true)
	productionAuthorityTestUpgrade(t, fixture, current)
	client := measurement.admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	original := client.callContext
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		var raw json.RawMessage
		if err := original(ctx, &raw, method, args...); err != nil {
			return err
		}
		return json.Unmarshal(raw, target)
	}
	reads := 0
	native := &crv4.Chain{API: measurement.admission.chain.API, GenesisHash: measurement.admission.chain.GenesisHash}
	if err := CaptureReleaseNativeSourceV2(t.Context(), native, current, intent, measurement.encoded, func(_ context.Context, value ReleaseEvidenceV2NativeRead) error {
		if value.Method == "state_getStorage" {
			reads++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if reads == 0 {
		t.Fatal("historical capture skipped the actual native evidence")
	}
}

// Multiple renewals keep the exact signed prefix and derive disjoint effective
// runtime windows without replacing any original config or approval bytes.
func TestProductionAuthorityHistoryAppendsAndRejectsReorderedPrefix(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	second, secondApproval := productionAuthorityTestSuccessor(t, fixture.cfg, fixture.approval, fixture.private, true)
	third, thirdApproval := productionAuthorityTestSuccessor(t, second, secondApproval, fixture.private, true)
	windows, err := productionHistoricalRuntimeWindows(third)
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 2 || windows[0].from != fixture.approval.ValidFromNativeBlock || windows[0].through != windows[0].from ||
		windows[1].from != secondApproval.ValidFromNativeBlock || windows[1].through != windows[1].from || windows[0].artifact.Version.SpecVersion+1 != windows[1].artifact.Version.SpecVersion {
		t.Fatalf("renewal runtime projection differs: %+v", windows)
	}
	windows[0].artifact.CodeHash = "synthetic external copy mutation"
	again, err := productionHistoricalRuntimeWindows(third)
	if err != nil || again[0].artifact.CodeHash != fixture.cfg.RuntimeCodeHash {
		t.Fatal("runtime-only projection escaped its immutable owner")
	}
	if len(third.productionAuthorityHistory.entries[1].config.ProductionAuthorityHistory) != 1 {
		t.Fatal("second original lost its exact signed prefix")
	}
	third.ProductionAuthorityHistory = slices.Clone(third.ProductionAuthorityHistory)
	third.ProductionAuthorityHistory[0], third.ProductionAuthorityHistory[1] = third.ProductionAuthorityHistory[1], third.ProductionAuthorityHistory[0]
	thirdApproval.ConfigHash, err = OwnerRecycleConfigHash(third)
	if err != nil {
		t.Fatal(err)
	}
	approver := recycleAdmissionFixture{cfg: third, approval: thirdApproval, private: fixture.private}
	approver.sign(t)
	if err := loadOwnerRecycleProductionConfig(third); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionRuntimeHistory(third); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionAuthorityHistory(third); err == nil || !strings.Contains(err.Error(), "predecessor prefix") {
		t.Fatalf("even a newly selected reordered history must preserve original signatures: %v", err)
	}
}

// An explicit runtime document cannot silently override the different exact
// artifact already signed in the retained original authority for that block.
func TestProductionAuthorityHistoryRejectsConflictingRuntimeProjection(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	current, approval := productionAuthorityTestSuccessor(t, fixture.cfg, fixture.approval, fixture.private, true)
	document := releaseProductionRuntimeApproval{
		Schema: releaseProductionRuntimeApprovalSchema, Revision: 1,
		NativeChain: approval.NativeChain, GenesisHash: current.GenesisHash, EvmChainId: current.ChainID,
		DeploymentId: current.DeploymentID, ValidatorId: current.ValidatorID, Netuid: current.Netuid,
		Coordinator: current.Coordinator, PolicyHash: current.PolicyHash,
		ValidFromBlock: fixture.approval.ValidFromNativeBlock, ValidThroughBlock: fixture.approval.ValidFromNativeBlock,
		RuntimeVersion: releaseNativeRuntimeIdentity(current).Version, RuntimeCodeHash: current.RuntimeCodeHash, RuntimeMetadataHash: current.RuntimeMetadataHash,
		RuntimeSourceCommit: strings.Repeat("a", 40), RuntimeReviewSha256: strings.Repeat("b", 64), RuntimeReviewScope: crv4.ValidatorProducerRuntimeProfile,
	}
	current.ProductionRuntimeApprovals = []ReleaseEvidenceV2File{mainnetRuntimeTestWriteApproval(t, filepath.Join(identityTestStateDir(t), "conflicting-runtime.json"), document)}
	var err error
	approval.ConfigHash, err = OwnerRecycleConfigHash(current)
	if err != nil {
		t.Fatal(err)
	}
	approver := recycleAdmissionFixture{cfg: current, approval: approval, private: fixture.private}
	approver.sign(t)
	if err := loadOwnerRecycleProductionConfig(current); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionRuntimeHistory(current); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionAuthorityHistory(current); err == nil || !strings.Contains(err.Error(), "conflicting exact artifacts") {
		t.Fatalf("conflicting original runtime was projected downstream: %v", err)
	}
}

// Complete count/byte admission precedes filesystem reads; cancellation grants
// neither retained bytes nor a partially authenticated output bundle.
func TestProductionAuthorityHistoryBoundsAndCancelledExport(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	current, approval := productionAuthorityTestSuccessor(t, fixture.cfg, fixture.approval, fixture.private, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if raw, err := BuildOwnerRecycleProductionAuthority(ctx, current); !errors.Is(err, context.Canceled) || raw != nil {
		t.Fatal("cancelled export published authority")
	}
	for _, count := range []int{maximumProductionAuthorityHistory + 1, maximumProductionAuthorityHistory} {
		current.ProductionAuthorityHistory = make([]ReleaseEvidenceV2File, count)
		for index := range current.ProductionAuthorityHistory {
			current.ProductionAuthorityHistory[index] = ReleaseEvidenceV2File{Path: filepath.Join(current.StateDir, "synthetic-unread-bundle.json"),
				Bytes: maximumProductionAuthorityBundleBytes, SHA256: releaseHex32([32]byte{0x21})}
		}
		var err error
		approval.ConfigHash, err = OwnerRecycleConfigHash(current)
		if err != nil {
			t.Fatal(err)
		}
		approver := recycleAdmissionFixture{cfg: current, approval: approval, private: fixture.private}
		approver.sign(t)
		if err := loadOwnerRecycleProductionConfig(current); err != nil {
			t.Fatal(err)
		}
		if err := loadReleaseProductionRuntimeHistory(current); err != nil {
			t.Fatal(err)
		}
		err = loadReleaseProductionAuthorityHistory(current)
		if err == nil || !strings.Contains(err.Error(), "exceeds its") {
			t.Fatalf("%d oversized history read a source or escaped its bound: %v", count, err)
		}
	}
}
