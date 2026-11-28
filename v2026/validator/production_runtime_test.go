// Independently signed synthetic configs exercise the actual production loader,
// exact runtime/history gate and signing-view capability without chain writes.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
	"gopkg.in/yaml.v3"
)

// The shared transcript fixture counts only read calls and preserves explicit
// old/current artifact windows. Approval keys are generated fixture material.
type productionRuntimeTestFixture struct {
	cfg      *ReleaseConfig
	rpc      *mainnetRuntimeTestFixture
	approval OwnerRecycleApproval
	private  ed25519.PrivateKey
	path     string
}

// An initial deployment may have no prior production history. Adding a prior
// window is a separate signed config input, never an observation promotion.
func newProductionRuntimeTestFixture(t *testing.T, historical bool) *productionRuntimeTestFixture {
	t.Helper()
	rpc := newMainnetRuntimeTestFixture(t)
	input := recycleTestInput(t)
	rpc.cfg.SchemaVersion = ReleaseMainnetProductionSchemaVersion
	rpc.cfg.Netuid = 25
	rpc.cfg.Policy = input.ParentPolicy
	rpc.cfg.PolicyHash, _ = rpc.cfg.Policy.HashHex()
	rpc.cfg.MainnetRuntimeApprovals = nil
	_, rpc.metadata = provisionalValidatorMetadataTest(t, "../crv4/runtime-profile-v1.scale.gz.base64", crv4.ReviewedRuntimeMetadataHash)
	rpc.cfg.RuntimeMetadataHash = crv4.ReviewedRuntimeMetadataHash
	if historical {
		prior := releaseProductionRuntimeApproval(rpc.approvals[0])
		prior.Schema = releaseProductionRuntimeApprovalSchema
		prior.Netuid, prior.PolicyHash, prior.RuntimeReviewScope = rpc.cfg.Netuid, rpc.cfg.PolicyHash, crv4.ValidatorProducerRuntimeProfile
		prior.RuntimeMetadataHash = rpc.cfg.RuntimeMetadataHash
		reference := mainnetRuntimeTestWriteApproval(t, filepath.Join(identityTestStateDir(t), "production-history.json"), prior)
		rpc.cfg.ProductionRuntimeApprovals = []ReleaseEvidenceV2File{reference}
	}
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x53}, ed25519.SeedSize))
	rpc.cfg.OwnerRecycleApproval = &ReleaseOwnerRecycleApprovalConfig{
		Approval: ReleaseEvidenceV2File{Path: filepath.Join(identityTestStateDir(t), "production-approval.json")},
		Signer:   "0x" + hex.EncodeToString(private.Public().(ed25519.PublicKey)),
	}
	proposal := input.Proposal
	proposal.Runtime.GenesisHash = [32]byte(rpc.genesis)
	proposal.Runtime.Version = rpc.versions[1]
	proposal.Runtime.CodeHash, _ = parseHash32("synthetic code", rpc.cfg.RuntimeCodeHash)
	proposal.Runtime.MetadataHash, _ = parseHash32("synthetic metadata", rpc.cfg.RuntimeMetadataHash)
	self := &productionRuntimeTestFixture{cfg: &rpc.cfg, rpc: rpc, private: private,
		approval: OwnerRecycleApproval{Schema: ownerRecycleProductionApprovalSchema, Proposal: proposal,
			NativeChain: rpc.nativeChain, RuntimeReviewHash: recycleTestId(302), ValidatorHotkey: recycleTestId(4), SubnetOwner: recycleTestId(303),
			OwnerHotkeys: [][32]byte{recycleTestId(2), recycleTestId(3)}, FirstNativeEpoch: 20,
			ValidFromNativeBlock: 101, ValidThroughNativeBlock: 200, MaximumSubnetUids: 6, MaximumOwnedHotkeys: 16,
			Production: &OwnerRecycleProductionApproval{Schema: ownerRecycleProductionScope, RuntimeCapability: crv4.ValidatorProducerRuntimeProfile,
				ValidatorHotkeys: [][32]byte{recycleTestId(4), recycleTestId(5)}, MaximumLastUpdateAge: 100,
				ValidThroughNativeEpoch: 30, ActivationNativeHash: [32]byte(mainnetRuntimeTestBlock(101))},
		},
	}
	rpc.client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if err := rpc.callContext(ctx, result, method, args...); err != nil {
			return err
		}
		if method == "state_getRuntimeVersion" {
			hash, err := types.NewHashFromHexString(fmt.Sprint(args[0]))
			if err != nil {
				return err
			}
			index := 0
			number, err := mainnetRuntimeTestNumber(hash)
			if err != nil {
				return err
			}
			if number > 100 {
				index = 1
			}
			version := rpc.versions[index]
			return setValidatorRuntimeIdentityTestResult(result, map[string]any{"specName": version.SpecName, "specVersion": version.SpecVersion,
				"transactionVersion": version.TransactionVersion, "stateVersion": version.StateVersion, "apis": []any{[]any{"0x8375104b299b74c5", 2}}})
		}
		return nil
	}
	self.resignAndLoad(t)
	return self
}

// The synthetic external approver signs the normalized YAML representation;
// only the public production loader installs the private authority capsule.
func (self *productionRuntimeTestFixture) resignAndLoad(t *testing.T) {
	t.Helper()
	self.signConfig(t)
	var err error
	self.cfg, err = LoadReleaseConfig(self.path)
	if err != nil {
		t.Fatal(err)
	}
}

// Negative histories are signed deliberately to distinguish signature refusal
// from the independent history schema, scope and interval checks.
func (self *productionRuntimeTestFixture) signConfig(t *testing.T) {
	t.Helper()
	raw, err := yaml.Marshal(self.cfg)
	if err != nil {
		t.Fatal(err)
	}
	var resolved ReleaseConfig
	if err := yaml.Unmarshal(raw, &resolved); err != nil {
		t.Fatal(err)
	}
	if err := resolved.normalize(filepath.Dir(resolved.StateDir)); err != nil {
		t.Fatal(err)
	}
	self.cfg = &resolved
	self.approval.ConfigHash, err = OwnerRecycleConfigHash(self.cfg)
	if err != nil {
		t.Fatal(err)
	}
	message, err := self.approval.SigningMessage()
	if err != nil {
		t.Fatal(err)
	}
	envelope := OwnerRecycleApprovalEnvelope{Approval: self.approval, Signature: hex.EncodeToString(ed25519.Sign(self.private, message))}
	raw, err = json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	self.cfg.OwnerRecycleApproval.Approval = mainnetRuntimeTestWriteBytes(t, self.cfg.OwnerRecycleApproval.Approval.Path, append(raw, '\n'))
	self.path = writeReleaseConfig(t, *self.cfg)
}

// The default producer loader admits an independently signed current artifact
// without expanding schema 2 or creating signer/state files while parsing.
func TestProductionRuntimeConfigAuthenticatesExactCurrentArtifact(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	if fixture.cfg.RuntimeSpec == crv4.ReviewedRuntimeSpecVersion {
		t.Fatal("fixture accidentally uses the compiled current artifact")
	}
	if _, err := os.Stat(fixture.cfg.HotkeySeedFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config load touched signer state: %v", err)
	}
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), fixture.rpc.native, fixture.cfg, mainnetRuntimeTestBlock(150)); err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseNativeSigningRuntime(fixture.rpc.native, fixture.cfg); err != nil {
		t.Fatal(err)
	}
	if fixture.rpc.native.CurrentRuntimeCompatibilityProfile() != "" || fixture.rpc.native.ProvisionalRuntimeCompatibilityEnabled() {
		t.Fatal("production authority imported testnet compatibility")
	}
	if err := os.Remove(fixture.cfg.OwnerRecycleApproval.Approval.Path); err != nil {
		t.Fatal(err)
	}
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), fixture.rpc.native, fixture.cfg, mainnetRuntimeTestBlock(150)); err != nil {
		t.Fatalf("loaded immutable authority reopened the source path: %v", err)
	}
}

// Earlier approved bytes are usable only at their original block interval.
// An old artifact neither signs now nor becomes current by relabeling history.
func TestProductionRuntimePreservesApprovedHistoricalDomain(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, true)
	prior := mainnetRuntimeTestBlock(100)
	if err := authenticateHistoricalNativeRuntimeAtContext(t.Context(), fixture.rpc.native, fixture.cfg, prior); err != nil {
		t.Fatal(err)
	}
	if uint32(fixture.rpc.native.Runtime.SpecVersion) != fixture.rpc.versions[0].SpecVersion || validateReleaseNativeSigningRuntime(fixture.rpc.native, fixture.cfg) == nil {
		t.Fatal("historical runtime gained current producer authority")
	}
	oldMeta, oldRuntime := fixture.rpc.native.Meta, fixture.rpc.native.Runtime
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), fixture.rpc.native, fixture.cfg, prior); err == nil {
		t.Fatal("fresh production admitted a historical-only interval")
	}
	if fixture.rpc.native.Meta != oldMeta || fixture.rpc.native.Runtime != oldRuntime {
		t.Fatal("failed current authentication changed historical evidence")
	}
	if err := validatePreparedNativeRuntimeContext(t.Context(), fixture.rpc.native, fixture.cfg, prior, mainnetRuntimeTestBlock(150)); err == nil {
		t.Fatal("prepared old-runtime bytes gained current replay authority")
	}
	fixture.rpc.versions[0], fixture.rpc.codes[0] = fixture.rpc.versions[1], fixture.rpc.codes[1]
	if err := authenticateHistoricalNativeRuntimeAtContext(t.Context(), fixture.rpc.native, fixture.cfg, prior); err == nil {
		t.Fatal("current runtime relabeled the prior production interval")
	}
}

// Even an approved artifact with the right exported version must pass the
// producer storage/call/event/API purpose before its mutable view may sign.
func TestProductionRuntimeSigningRejectsReadOnlyExactArtifact(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	artifact, err := crv4.AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.rpc.native, mainnetRuntimeTestBlock(150), releaseNativeRuntimeIdentity(fixture.cfg))
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.rpc.native.BindRuntimeArtifact(artifact); err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseNativeSigningRuntime(fixture.rpc.native, fixture.cfg); err == nil {
		t.Fatal("read-only exact artifact acquired production signing authority")
	}
}

// Changing network, routes, artifact or finite approval windows always fails
// before any new view is published, with both warm and cold metadata caches.
func TestProductionRuntimeRejectsNetworkArtifactAndWindowDrift(t *testing.T) {
	for _, fault := range []string{"chain", "genesis", "EVM", "version", "code", "metadata", "window", "canonical"} {
		fixture := newProductionRuntimeTestFixture(t, false)
		switch fault {
		case "chain":
			fixture.rpc.nativeChain = "Other Synthetic Network"
		case "genesis":
			fixture.rpc.genesis = types.Hash{0x45}
		case "EVM":
			fixture.rpc.evmChainId = "0x3b1"
		case "version":
			fixture.rpc.versions[1].SpecVersion++
		case "code":
			fixture.rpc.codes[1] = types.Hash{0x56}.Hex()
		case "metadata":
			fixture.rpc.metadata = "0x00"
		case "window":
			fixture.rpc.head = 201
		case "canonical":
			fixture.rpc.canonicalHashKVs[150] = types.Hash{0x67}
		}
		oldMeta, oldRuntime := fixture.rpc.native.Meta, fixture.rpc.native.Runtime
		if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), fixture.rpc.native, fixture.cfg, mainnetRuntimeTestBlock(fixture.rpc.head)); err == nil {
			t.Errorf("%s acquired production runtime authority", fault)
		}
		if fixture.rpc.native.Meta != oldMeta || fixture.rpc.native.Runtime != oldRuntime {
			t.Errorf("%s changed the retained view", fault)
		}
	}
}

// Public schema changes and copied pointers cannot expand loaded authority;
// bootstrap/observation parsers remain separate even for valid signed configs.
func TestProductionRuntimeConfigRefusesRelabelingAndOtherPurposes(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, true)
	for _, load := range []func(string) (*ReleaseConfig, error){LoadReleaseConfigPreActivation, LoadMainnetRuntimeObservationConfig, LoadProvisionalActivationObservationConfig, LoadOwnerRecycleAdmissionConfig} {
		if _, err := load(fixture.path); err == nil {
			t.Fatal("production config acquired another load purpose")
		}
	}
	for _, fault := range []string{"schema", "version", "history", "approval"} {
		changed := *fixture.cfg
		switch fault {
		case "schema":
			changed.SchemaVersion = 1
		case "version":
			changed.RuntimeSpec++
		case "history":
			changed.ProductionRuntimeApprovals = nil
		case "approval":
			changed.OwnerRecycleApproval = nil
		}
		if err := validateReleaseNativeRuntimeConfig(&changed); err == nil {
			t.Errorf("%s acquired runtime authority", fault)
		}
		if _, err := loadReleaseHotkey(&changed); err == nil {
			t.Errorf("%s passed key admission", fault)
		}
		if _, err := openReleaseNativeJournal(&changed); err == nil {
			t.Errorf("%s opened a native journal", fault)
		}
	}
}

// Cancellation is forced inside the exact runtime read and joined. The
// operation does not publish a late success or partially replace the old view.
func TestProductionRuntimeCancellationJoinsBeforeBinding(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	started := make(chan struct{})
	client := fixture.rpc.client
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if method == "state_getRuntimeVersion" {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}
		return original(ctx, result, method, args...)
	}
	oldMeta, oldRuntime := fixture.rpc.native.Meta, fixture.rpc.native.Runtime
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- authenticatePinnedNativeRuntimeAtContext(ctx, fixture.rpc.native, fixture.cfg, mainnetRuntimeTestBlock(150))
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation cause: %v", err)
	}
	if fixture.rpc.native.Meta != oldMeta || fixture.rpc.native.Runtime != oldRuntime {
		t.Fatal("canceled production authentication changed the old view")
	}
}

// An independently signed history still must name production scope, preserve
// prior intervals and keep the current signing window separate.
func TestProductionRuntimeHistoryRejectsObservationScopeAndOverlap(t *testing.T) {
	for _, fault := range []string{"schema", "scope", "overlap", "predecessor", "trailing", "duplicate"} {
		fixture := newProductionRuntimeTestFixture(t, true)
		reference := fixture.cfg.ProductionRuntimeApprovals[0]
		raw, err := os.ReadFile(reference.Path)
		if err != nil {
			t.Fatal(err)
		}
		var approval releaseProductionRuntimeApproval
		if err := json.Unmarshal(raw, &approval); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "schema":
			approval.Schema = releaseMainnetRuntimeApprovalSchema
		case "scope":
			approval.RuntimeReviewScope = releaseMainnetRuntimeObservationScope
		case "overlap":
			approval.ValidThroughBlock = fixture.approval.ValidFromNativeBlock
		case "predecessor":
			approval.PreviousSha256 = reference.SHA256
		}
		raw, err = json.Marshal(approval)
		if err != nil {
			t.Fatal(err)
		}
		if fault == "trailing" {
			raw = append(raw, []byte("\n{}")...)
		} else if fault == "duplicate" {
			raw = append([]byte(`{"revision":1,`), raw[1:]...)
		}
		fixture.cfg.ProductionRuntimeApprovals[0] = mainnetRuntimeTestWriteBytes(t, reference.Path, raw)
		fixture.signConfig(t)
		if _, err := LoadReleaseConfig(fixture.path); err == nil {
			t.Errorf("signed %s history acquired production authority", fault)
		}
	}
}

// A final canonical change after every artifact and purpose read cannot
// replace the previous mutable signing view with partially accepted evidence.
func TestProductionRuntimeClosingCanonicalCheckPreservesPriorView(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	client := fixture.rpc.client
	original := client.callContext
	finalityReads, canonicalReads := 0, 0
	faulted := false
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if method == "chain_getFinalizedHead" {
			finalityReads++
		}
		if method == "chain_getBlockHash" && args[0] == uint64(150) {
			canonicalReads++
			if finalityReads == 3 {
				faulted = true
				return setReleaseHistoricalTestResult(result, (types.Hash{0x98}).Hex())
			}
		}
		return original(ctx, result, method, args...)
	}
	oldMeta, oldRuntime := fixture.rpc.native.Meta, fixture.rpc.native.Runtime
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), fixture.rpc.native, fixture.cfg, mainnetRuntimeTestBlock(150)); err == nil || !faulted {
		t.Fatalf("closing canonical change acquired authority: %v reads=%d", err, canonicalReads)
	}
	if fixture.rpc.native.Meta != oldMeta || fixture.rpc.native.Runtime != oldRuntime {
		t.Fatal("closing canonical failure replaced the old view")
	}
}
