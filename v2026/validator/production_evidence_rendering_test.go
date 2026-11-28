//go:build linux || darwin

// A signed activation-pending production config never runs or changes. Its
// rendered successor runs only with the approval key's re-approval and the
// exact signed original as authenticated history. All keys are generated.
package validator

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"gopkg.in/yaml.v3"
)

// The signed original, its approval key and its exact approved body.
type productionPendingTestFixture struct {
	path     string
	cfg      *ReleaseConfig
	approval OwnerRecycleApproval
	private  ed25519.PrivateKey
}

// Pending keeps every pre-declared path and clears only lengths and digests.
func productionPendingTestEvidence(operators []ReleaseEvidenceV2OperatorConfig) []ReleaseEvidenceV2OperatorConfig {
	operators = slices.Clone(operators)
	for index := range operators {
		operator := &operators[index]
		for _, file := range []*ReleaseEvidenceV2File{&operator.Activation, &operator.VPKSignature, &operator.HotkeySignature, &operator.Context, &operator.History} {
			file.Bytes, file.SHA256 = 0, ""
		}
	}
	return operators
}

// The approval key signs the normalized document under either selector.
func productionPendingTestSign(t *testing.T, cfg ReleaseConfig, approval OwnerRecycleApproval, private ed25519.PrivateKey) (string, OwnerRecycleApproval) {
	t.Helper()
	raw, err := yaml.Marshal(cfg)
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
	approval.ConfigHash, err = OwnerRecycleConfigHash(&resolved)
	if err != nil {
		t.Fatal(err)
	}
	message, err := approval.SigningMessage()
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(OwnerRecycleApprovalEnvelope{Approval: approval, Signature: hex.EncodeToString(ed25519.Sign(private, message))})
	if err != nil {
		t.Fatal(err)
	}
	selection := productionEconomicSelection(&resolved)
	selection.Approval = mainnetRuntimeTestWriteBytes(t, selection.Approval.Path, append(raw, '\n'))
	return writeReleaseConfig(t, resolved), approval
}

// Exact deep copies keep each mutation local to its variant.
func productionPendingTestClone[T any](t *testing.T, value T) T {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// The launch selector: a signed receiving policy for two reserve recipients.
func productionPendingTestTreasury(cfg *ReleaseConfig, approval *OwnerRecycleApproval) {
	cfg.TreasuryApproval, cfg.OwnerRecycleApproval = cfg.OwnerRecycleApproval, nil
	approval.Schema, approval.Proposal.Schema, approval.Production.Schema = TreasuryApprovalSchema, TreasuryProposalSchema, TreasuryProductionScope
	approval.Proposal.Remainder, approval.Proposal.OwnerAllocation = "ordinary_treasury_credit", "equal_exact_registered"
	approval.Proposal.Runtime.SourceCommit = crv4.NativeOwnerSource470
	approval.Proposal.Treasury = &TreasuryPolicy{Schema: TreasuryReceivePolicySchema, MultisigAccount: [32]byte(bytes.Repeat([]byte{0x77}, 32)),
		Recipients: []TreasuryRecipient{
			{Uid: 4, Hotkey: [32]byte(bytes.Repeat([]byte{0x45}, 32)), RegistrationBlock: 44},
			{Uid: 5, Hotkey: [32]byte(bytes.Repeat([]byte{0x46}, 32)), RegistrationBlock: 45},
		}, ProviderShare: protocol.Rational{Numerator: 1, Denominator: 10}, TreasuryShare: protocol.Rational{Numerator: 9, Denominator: 10}, MaxWeightLimitU16: 32768}
}

// One signed schema-3 config whose census names every path and pins no bytes.
func newProductionPendingTestFixture(t *testing.T, treasury bool) *productionPendingTestFixture {
	t.Helper()
	runtime := newProductionRuntimeTestFixture(t, false)
	cfg := productionPendingTestClone(t, *runtime.cfg)
	approval := productionPendingTestClone(t, runtime.approval)
	cfg.EvidenceV2.Operators = productionPendingTestEvidence(cfg.EvidenceV2.Operators)
	if treasury {
		productionPendingTestTreasury(&cfg, &approval)
	}
	path, approved := productionPendingTestSign(t, cfg, approval, runtime.private)
	loaded, err := LoadReleaseProductionConfigPreActivation(path)
	if err != nil {
		t.Fatal("signed activation-pending config refused for activation", err)
	}
	return &productionPendingTestFixture{path: path, cfg: loaded, approval: approved, private: runtime.private}
}

// A pending variant signed by the real approval key, so the refusal comes
// from the rule under test rather than a missing or stale signature.
func (self *productionPendingTestFixture) variant(t *testing.T, mutate func(*ReleaseConfig, *OwnerRecycleApproval)) string {
	t.Helper()
	cfg, approval := productionPendingTestClone(t, *self.cfg), productionPendingTestClone(t, self.approval)
	productionEconomicSelection(&cfg).Approval = ReleaseEvidenceV2File{Path: filepath.Join(identityTestStateDir(t), "approval.json")}
	mutate(&cfg, &approval)
	path, _ := productionPendingTestSign(t, cfg, approval, self.private)
	return path
}

// Real activation inputs rendered at exactly the pre-declared paths.
func productionPendingTestRender(t *testing.T, cfg *ReleaseConfig) []ReleaseEvidenceV2OperatorConfig {
	t.Helper()
	hotkey, err := crv4.KeypairFromSeed([32]byte{4})
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := parseHash32("synthetic genesis", cfg.GenesisHash)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := parseHash32("synthetic policy", cfg.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	deployment := ReleaseActivationDeploymentV2{DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, GenesisHash: genesis, Netuid: cfg.Netuid,
		Coordinator: [20]byte(common.HexToAddress(cfg.Coordinator)), SettlementVault: [20]byte(common.HexToAddress(cfg.SettlementVault)), PolicyHash: policy}
	snapshot := ReleaseActivationSnapshotV2{Epoch: 9, NativeBlock: 120, NativeHash: [32]byte{5}, EVMBlock: 200, EVMHash: [32]byte{6}}
	ledger := ReleaseActivationLedgerV2{DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, GenesisHash: strings.ToLower(cfg.GenesisHash), Netuid: cfg.Netuid, ValidatorID: cfg.ValidatorID}
	var rendered []ReleaseEvidenceV2OperatorConfig
	for index, operator := range cfg.EvidenceV2.Operators {
		clientKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(0x61 + index)}, ed25519.SeedSize))
		activation, err := BuildFreshReleaseActivationV2(deployment, snapshot, hotkey.PublicKey(), operator.NoID, [32]byte(clientKey[ed25519.SeedSize:]))
		if err != nil {
			t.Fatal(err)
		}
		vpkSignature, hotkeySignature, err := SignReleaseActivationV2(activation, hotkey, clientKey)
		if err != nil {
			t.Fatal(err)
		}
		member := ReleaseActivationMemberV2{NoID: operator.NoID, ValidatorUID: 3, Activation: activation, VPKSignature: vpkSignature, HotkeySignature: hotkeySignature}
		paths := ReleaseEvidenceV2OperatorPaths{ReplayScratchRoot: operator.ReplayScratchRoot, SealScratchRoot: operator.SealScratchRoot}
		for fileIndex, file := range operator.Files() {
			paths.Files[fileIndex] = file.Path
		}
		entry, inputs, err := RenderReleaseActivationInputsV2(ledger, member, [20]byte{0x33}, [32]byte{8}, ReleaseActivationBoundaryV2{Block: 260, Hash: [32]byte{7}}, cfg.EvidenceV2.Bounds, paths)
		if err != nil {
			t.Fatal(err)
		}
		for fileIndex, reference := range entry.Files() {
			if _, err := WriteReleaseEvidenceV2File(t.Context(), reference.Path, inputs[reference.Path], ReleaseEvidenceV2ReferenceLimit(cfg.EvidenceV2.Bounds, fileIndex)); err != nil {
				t.Fatal(err)
			}
		}
		rendered = append(rendered, entry)
	}
	return rendered
}

// Three successor files in their own owner-private directory.
func productionPendingTestOptions(t *testing.T) ReleaseProductionSuccessorOptions {
	t.Helper()
	root := identityTestStateDir(t)
	return ReleaseProductionSuccessorOptions{RenderedConfigPath: filepath.Join(root, "validator-rendered.yml"),
		SuccessorApprovalPath: filepath.Join(root, "successor-approval.json"), OriginalAuthorityPath: filepath.Join(root, "original-authority.json")}
}

// The producer and every existing lifecycle loader refuse the signed pending
// config with a distinct cause, before state exists and without completing an
// activation in place; only the explicit pre-activation loader admits it.
func TestProductionPendingConfigNeverRunsButLoadsForActivation(t *testing.T) {
	f := newProductionPendingTestFixture(t, false)
	original, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReleaseConfig(f.path); !errors.Is(err, ErrReleaseProductionActivationPending) || errors.Is(err, ErrReleaseEvidenceV2ActivationPending) {
		t.Fatalf("producer loader error = %v, want the distinct production activation-pending refusal", err)
	}
	if err := RunRelease(t.Context(), f.path); !errors.Is(err, ErrReleaseProductionActivationPending) || !strings.Contains(err.Error(), "--rendered-config") {
		t.Fatalf("producer lifecycle error = %v", err)
	}
	if _, err := os.Lstat(f.cfg.StateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused producer created state or began an in-place activation", err)
	}
	if after, err := os.ReadFile(f.path); err != nil || !bytes.Equal(after, original) {
		t.Fatal("refused producer rewrote the signed config", err)
	}
	if _, err := LoadReleaseConfigPreActivation(f.path); err == nil {
		t.Fatal("legacy pre-activation loader acquired production authority")
	}
	approved, err := ownerRecycleProductionApproval(f.cfg)
	if err != nil || !reflect.DeepEqual(approved.Approval, f.approval) || len(f.cfg.EvidenceV2.Operators) != 2 {
		t.Fatal("pre-activation loader lost the exact signed approval", err)
	}
	for _, operator := range f.cfg.EvidenceV2.Operators {
		if !operator.Unrendered() || slices.Contains(releaseEvidenceV2DeclaredPaths(operator), "") {
			t.Fatal("pre-activation loader changed the pending census", operator)
		}
	}
	if _, err := loadReleaseActivationConfig(f.path, false); err == nil || !strings.Contains(err.Error(), "never rewritten in place") {
		t.Fatal("legacy activation would rewrite the signed config", err)
	}
	if cfg, err := loadReleaseActivationConfig(f.path, true); err != nil || cfg.EvidenceV2.Operators[0] != f.cfg.EvidenceV2.Operators[0] {
		t.Fatal("successor activation did not route to the production pre-activation loader", err)
	}
	legacy := writeReleaseConfig(t, unrenderedReleaseConfig(t))
	if _, err := loadReleaseActivationConfig(legacy, true); err == nil || !strings.Contains(err.Error(), "apply only to a signed schema 3") {
		t.Fatal("successor files were accepted for a legacy config", err)
	}
	if _, err := LoadReleaseProductionConfigPreActivation(legacy); err == nil || !strings.Contains(err.Error(), "signed schema 3") {
		t.Fatal("legacy config acquired production pre-activation", err)
	}
	rendered := newProductionRuntimeTestFixture(t, false)
	if _, err := LoadReleaseProductionConfigPreActivation(rendered.path); err == nil || !strings.Contains(err.Error(), "already rendered") {
		t.Fatal("rendered production config acquired a pre-activation purpose", err)
	}
	for name, item := range map[string]struct {
		mutate     func(*ReleaseConfig, *OwnerRecycleApproval)
		diagnostic string
	}{
		"undeclared path": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) { cfg.EvidenceV2.Operators[1].History.Path = "" }, diagnostic: "pre-declare every input path"},
		"undeclared root": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) { cfg.EvidenceV2.Operators[0].SealScratchRoot = "" }, diagnostic: "pre-declare every input path"},
		"mixed census": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) {
			cfg.EvidenceV2.Operators[0] = rendered.cfg.EvidenceV2.Operators[0]
		}, diagnostic: "mixes rendered and activation-pending"},
		"overlapping path": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) {
			cfg.EvidenceV2.Operators[0].Context.Path = filepath.Join(cfg.StateDir, "context.json")
		}, diagnostic: "overlaps protected state"},
	} {
		path := f.variant(t, item.mutate)
		if _, err := LoadReleaseProductionConfigPreActivation(path); err == nil || !strings.Contains(err.Error(), item.diagnostic) {
			t.Errorf("%s: signed pending variant admitted or refused for another cause: %v", name, err)
		}
		if _, err := LoadReleaseConfig(path); !errors.Is(err, ErrReleaseProductionActivationPending) {
			t.Errorf("%s: producer did not refuse the signed pending variant as pending: %v", name, err)
		}
	}
}

// Bootstrap admits a pending census only through the explicit inspector, with
// every other check unchanged; rendered configs inspect identically either way.
func TestProductionBootstrapPreActivationInspectsOnlyPendingCensus(t *testing.T) {
	f := newProductionPendingTestFixture(t, true)
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := InspectProductionBootstrapConfig(t.Context(), f.path, raw); got != nil || !errors.Is(err, ErrReleaseEvidenceV2ActivationPending) {
		t.Fatal("strict bootstrap inspection admitted a pending census", err)
	}
	got, err := InspectProductionBootstrapConfigPreActivation(t.Context(), f.path, raw)
	if err != nil || got == nil || !got.EvidenceActivationPending || !reflect.DeepEqual(got.Approval, f.approval) || got.Approval.Proposal.Treasury == nil ||
		got.ApprovalReference != f.cfg.TreasuryApproval.Approval || got.ApprovalSigner != f.cfg.TreasuryApproval.Signer {
		t.Fatalf("pending treasury config inspection = %+v, %v", got, err)
	}
	for _, operator := range f.cfg.EvidenceV2.Operators {
		for _, path := range releaseEvidenceV2DeclaredPaths(operator) {
			if !slices.Contains(got.DeclaredPaths, path) {
				t.Fatal("pending inspection omitted a reserved evidence path", path)
			}
		}
	}
	for _, path := range []string{f.cfg.HotkeySeedFile, f.cfg.EvidenceV2.Operators[0].Activation.Path} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("pending inspection opened or created producer input", path, err)
		}
	}
	rendered := newProductionRuntimeTestFixture(t, false)
	renderedRaw, err := os.ReadFile(rendered.path)
	if err != nil {
		t.Fatal(err)
	}
	strict, strictErr := InspectProductionBootstrapConfig(t.Context(), rendered.path, renderedRaw)
	pre, preErr := InspectProductionBootstrapConfigPreActivation(t.Context(), rendered.path, renderedRaw)
	if strictErr != nil || preErr != nil || !reflect.DeepEqual(strict, pre) || pre.EvidenceActivationPending {
		t.Fatal("rendered config inspection differs between inspectors", strictErr, preErr)
	}
	if encoded, err := json.Marshal(strict); err != nil || bytes.Contains(encoded, []byte("evidence_activation_pending")) {
		t.Fatal("strict inspection encoding changed", err)
	}
	for name, item := range map[string]struct {
		mutate     func(*ReleaseConfig, *OwnerRecycleApproval)
		diagnostic string
	}{
		"undeclared path": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) { cfg.EvidenceV2.Operators[0].Context.Path = "" }, diagnostic: "pre-declare every input path"},
		"mixed census": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) {
			cfg.EvidenceV2.Operators[1] = rendered.cfg.EvidenceV2.Operators[1]
		}, diagnostic: "mixes rendered and activation-pending"},
		"overlapping path": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) {
			cfg.EvidenceV2.Operators[0].History.Path = cfg.HotkeySeedFile
		}, diagnostic: "overlaps protected state"},
		"runtime pin": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) { cfg.RuntimeSpec++ }, diagnostic: "approved runtime and configured mainnet tuple differ"},
		"census": {mutate: func(_ *ReleaseConfig, approval *OwnerRecycleApproval) {
			approval.Production.ValidatorHotkeys = approval.Production.ValidatorHotkeys[1:]
		}, diagnostic: "validator census"},
	} {
		path := f.variant(t, item.mutate)
		variant, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := InspectProductionBootstrapConfigPreActivation(t.Context(), path, variant); got != nil || err == nil || !strings.Contains(err.Error(), item.diagnostic) {
			t.Errorf("%s: pre-activation inspection admitted an invalid signed config or refused it for another cause: %v", name, err)
		}
	}
	stale := bytes.Replace(raw, []byte("poll_seconds: 2"), []byte("poll_seconds: 3"), 1)
	if bytes.Equal(stale, raw) {
		t.Fatal("stale-approval control did not change the signed document")
	}
	if got, err := InspectProductionBootstrapConfigPreActivation(t.Context(), f.path, stale); got != nil || err == nil || !strings.Contains(err.Error(), "different complete configuration") {
		t.Fatal("pending inspection admitted bytes its approval does not name", err)
	}
}

// Rendering yields one exact successor: the original approval re-signed for
// the successor's complete-config hash, which commits the rendered references
// and the original authority bundle. Only that authority admits it to run.
func TestProductionRenderedSuccessorRunsOnlyWithAuthenticatedLineage(t *testing.T) {
	for _, treasury := range []bool{false, true} {
		f := newProductionPendingTestFixture(t, treasury)
		original, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
		rendered := productionPendingTestRender(t, f.cfg)
		options := productionPendingTestOptions(t)
		successor, err := buildReleaseProductionSuccessor(t.Context(), f.cfg, f.path, rendered, options, nil)
		if err != nil {
			t.Fatal(treasury, err)
		}
		want := f.approval
		want.ConfigHash = successor.approval.ConfigHash
		hash, err := OwnerRecycleConfigHash(successor.config)
		message, messageErr := want.SigningMessage()
		if err != nil || messageErr != nil || hash != successor.approval.ConfigHash || successor.approval.ConfigHash == f.approval.ConfigHash ||
			!reflect.DeepEqual(successor.approval, want) || !bytes.Equal(message, successor.message) {
			t.Fatal("successor approval is not the original re-signed for its own complete config", treasury, err, messageErr)
		}
		var printed bytes.Buffer
		successor.print(&printed)
		if !strings.Contains(printed.String(), "0x"+hex.EncodeToString(message)) || !strings.Contains(printed.String(), successor.reference.SHA256) {
			t.Fatal("printed request omits the exact signing message or original authority", printed.String())
		}
		foreign := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x5a}, ed25519.SeedSize))
		if _, err := successor.publish(t.Context(), hex.EncodeToString(ed25519.Sign(foreign, message))); err == nil || !strings.Contains(err.Error(), "pinned approval key") {
			t.Fatal("a foreign key completed the successor", err)
		}
		for _, path := range []string{options.RenderedConfigPath, options.SuccessorApprovalPath, options.OriginalAuthorityPath} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("a refused signature wrote successor custody", path, err)
			}
		}
		signature := hex.EncodeToString(ed25519.Sign(f.private, message))
		if _, err := successor.publish(t.Context(), strings.ToUpper(signature)); err == nil {
			t.Fatal("a noncanonical signature spelling was admitted")
		}
		document, err := successor.publish(t.Context(), signature)
		if err != nil {
			t.Fatal(treasury, err)
		}
		loaded, err := LoadReleaseConfig(options.RenderedConfigPath)
		if err != nil {
			t.Fatal("authenticated successor refused by the producer loader", treasury, err)
		}
		approved, err := ownerRecycleProductionApproval(loaded)
		if err != nil || !reflect.DeepEqual(approved.Approval, successor.approval) || !reflect.DeepEqual(loaded.EvidenceV2.Operators, rendered) ||
			len(loaded.ProductionAuthorityHistory) != 1 || loaded.ProductionAuthorityHistory[0] != successor.reference || ownerRecycleProductionBoundary(loaded) != nil {
			t.Fatal("loaded successor lost its rendered inputs, approval or current writer authority", err)
		}
		history := loaded.productionAuthorityHistory.entries[0].config
		prior, err := ownerRecycleProductionApproval(history)
		if err != nil || !history.ownerRecycleProduction.historicalOnly || !reflect.DeepEqual(prior.Approval, f.approval) || !reflect.DeepEqual(history.EvidenceV2, f.cfg.EvidenceV2) {
			t.Fatal("successor history is not the exact signed pending original", err)
		}
		if after, err := os.ReadFile(f.path); err != nil || !bytes.Equal(after, original) {
			t.Fatal("rendering changed the signed original", err)
		}
		if again, err := successor.publish(t.Context(), signature); err != nil || !bytes.Equal(again, document) {
			t.Fatal("exact successor retry was not idempotent", err)
		}
	}
}

// Every successor below carries a valid approval-key signature over its own
// complete config, yet changes what rendering may change. The producer loader
// refuses each; an unsigned edit of the accepted successor is refused too.
func TestProductionRenderedSuccessorRefusesUnauthorizedRendering(t *testing.T) {
	f := newProductionPendingTestFixture(t, true)
	rendered := productionPendingTestRender(t, f.cfg)
	options := productionPendingTestOptions(t)
	successor, err := buildReleaseProductionSuccessor(t.Context(), f.cfg, f.path, rendered, options, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := successor.publish(t.Context(), hex.EncodeToString(ed25519.Sign(f.private, successor.message))); err != nil {
		t.Fatal(err)
	}
	foreign := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x5b}, ed25519.SeedSize))
	resign := func(mutate func(*ReleaseConfig, *OwnerRecycleApproval), private ed25519.PrivateKey) string {
		cfg, approval := productionPendingTestClone(t, *successor.config), productionPendingTestClone(t, successor.approval)
		cfg.TreasuryApproval.Approval = ReleaseEvidenceV2File{Path: filepath.Join(identityTestStateDir(t), "approval.json")}
		mutate(&cfg, &approval)
		path, _ := productionPendingTestSign(t, cfg, approval, private)
		return path
	}
	if _, err := LoadReleaseConfig(resign(func(*ReleaseConfig, *OwnerRecycleApproval) {}, f.private)); err != nil {
		t.Fatal("faithful re-signed successor control refused", err)
	}
	moved := filepath.Join(identityTestStateDir(t), "context.json")
	for name, item := range map[string]struct {
		mutate     func(*ReleaseConfig, *OwnerRecycleApproval)
		private    ed25519.PrivateKey
		diagnostic string
	}{
		"moved reference": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) { cfg.EvidenceV2.Operators[0].Context.Path = moved },
			private: f.private, diagnostic: "pre-declared path"},
		"moved scratch root": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) {
			cfg.EvidenceV2.Operators[1].ReplayScratchRoot = filepath.Join(filepath.Dir(moved), "replay")
		}, private: f.private, diagnostic: "scratch roots"},
		"changed route": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) { cfg.Operators[0].APIURL = "https://other.example" },
			private: f.private, diagnostic: "beyond its rendered references"},
		"changed window": {mutate: func(_ *ReleaseConfig, approval *OwnerRecycleApproval) { approval.ValidThroughNativeBlock++ },
			private: f.private, diagnostic: "original economic approval"},
		"changed treasury": {mutate: func(_ *ReleaseConfig, approval *OwnerRecycleApproval) {
			approval.Proposal.Treasury.Recipients[1].RegistrationBlock++
		}, private: f.private, diagnostic: "original economic approval"},
		"foreign signer": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) {
			cfg.TreasuryApproval.Signer = "0x" + hex.EncodeToString(foreign.Public().(ed25519.PublicKey))
		}, private: foreign, diagnostic: "beyond its rendered references"},
		"partial rendering": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) {
			cfg.EvidenceV2.Operators[1].History.Bytes, cfg.EvidenceV2.Operators[1].History.SHA256 = 0, ""
		}, private: f.private, diagnostic: "evidence_v2 reference"},
	} {
		if _, err := LoadReleaseConfig(resign(item.mutate, item.private)); err == nil || !strings.Contains(err.Error(), item.diagnostic) {
			t.Errorf("%s: signed successor admitted or refused for another cause: %v", name, err)
		}
	}
	var accepted ReleaseConfig
	raw, err := os.ReadFile(options.RenderedConfigPath)
	if err != nil || yaml.Unmarshal(raw, &accepted) != nil {
		t.Fatal("accepted successor unreadable", err)
	}
	for name, mutate := range map[string]func(*ReleaseConfig){
		"digest":  func(cfg *ReleaseConfig) { cfg.EvidenceV2.Operators[0].Context.SHA256 = "0x" + strings.Repeat("cd", 32) },
		"lineage": func(cfg *ReleaseConfig) { cfg.ProductionAuthorityHistory = nil },
	} {
		changed := productionPendingTestClone(t, accepted)
		mutate(&changed)
		if _, err := LoadReleaseConfig(writeReleaseConfig(t, changed)); err == nil || !strings.Contains(err.Error(), "different complete configuration") {
			t.Errorf("%s: unsigned successor edit admitted or refused for another cause: %v", name, err)
		}
	}
}

// The rendering class exists only for a pending predecessor. A rendered
// original still cannot change its references through a re-signed successor.
func TestProductionAuthorityContinuityKeepsRenderedReferencesFixed(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	bundle, err := BuildOwnerRecycleProductionAuthority(t.Context(), fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := WriteReleaseEvidenceV2File(t.Context(), filepath.Join(identityTestStateDir(t), "original-authority.json"), bundle, maximumProductionAuthorityBundleBytes)
	if err != nil {
		t.Fatal(err)
	}
	cfg, approval := productionPendingTestClone(t, *fixture.cfg), productionPendingTestClone(t, fixture.approval)
	cfg.ProductionAuthorityHistory = []ReleaseEvidenceV2File{reference}
	cfg.OwnerRecycleApproval.Approval = ReleaseEvidenceV2File{Path: filepath.Join(identityTestStateDir(t), "approval.json")}
	path, _ := productionPendingTestSign(t, cfg, approval, fixture.private)
	if _, err := LoadReleaseConfig(path); err != nil {
		t.Fatal("unchanged rendered successor control refused", err)
	}
	cfg.EvidenceV2.Operators = slices.Clone(cfg.EvidenceV2.Operators)
	cfg.EvidenceV2.Operators[0].Context.SHA256 = "0x" + strings.Repeat("cd", 32)
	path, _ = productionPendingTestSign(t, cfg, approval, fixture.private)
	if _, err := LoadReleaseConfig(path); err == nil || !strings.Contains(err.Error(), "continuity changes policy, identity, transport or custody") {
		t.Fatal("a rendered predecessor's references changed through a re-signed successor", err)
	}
}

// The successor files are named explicitly and never alias the signed
// original, its approval, declared custody or each other.
func TestProductionSuccessorOptionsAndUsage(t *testing.T) {
	opts, err := parseValidatorArgsForTest(t, []string{"activate", "--config=/srv/validator.yml", "--relayer_key_file=relay.key", "--apply",
		"--rendered-config=/srv/out/rendered.yml", "--successor-approval=/srv/out/approval.json", "--original-authority=/srv/out/original.json",
		"--approval-signature=" + strings.Repeat("ab", 64)})
	if err != nil {
		t.Fatal(err)
	}
	successor := releaseProductionSuccessorOptionsFromOpts(opts)
	if selected, _ := opts.Bool("activate"); !selected || successor == nil || successor.RenderedConfigPath != "/srv/out/rendered.yml" ||
		successor.SuccessorApprovalPath != "/srv/out/approval.json" || successor.OriginalAuthorityPath != "/srv/out/original.json" || successor.ApprovalSignature != strings.Repeat("ab", 64) {
		t.Fatal("successor options were not parsed", opts)
	}
	if opts, err := parseValidatorArgsForTest(t, []string{"activate", "--config=r.yml", "--dry-run"}); err != nil || releaseProductionSuccessorOptionsFromOpts(opts) != nil {
		t.Fatal("legacy activation acquired successor options", err)
	}
	f := newProductionPendingTestFixture(t, false)
	options := productionPendingTestOptions(t)
	if err := validateReleaseProductionSuccessorPaths(f.cfg, f.path, options); err != nil {
		t.Fatal(err)
	}
	for name, item := range map[string]struct {
		mutate     func(*ReleaseProductionSuccessorOptions)
		diagnostic string
	}{
		"missing":  {mutate: func(o *ReleaseProductionSuccessorOptions) { o.OriginalAuthorityPath = "" }, diagnostic: "requires --rendered-config"},
		"relative": {mutate: func(o *ReleaseProductionSuccessorOptions) { o.RenderedConfigPath = "rendered.yml" }, diagnostic: "canonical absolute"},
		"aliased":  {mutate: func(o *ReleaseProductionSuccessorOptions) { o.SuccessorApprovalPath = o.OriginalAuthorityPath }, diagnostic: "distinct and unnested"},
		"nested": {mutate: func(o *ReleaseProductionSuccessorOptions) {
			o.RenderedConfigPath = filepath.Join(o.OriginalAuthorityPath, "rendered.yml")
		}, diagnostic: "distinct and unnested"},
		"original": {mutate: func(o *ReleaseProductionSuccessorOptions) { o.RenderedConfigPath = f.path }, diagnostic: "overlaps the signed original"},
		"approval": {mutate: func(o *ReleaseProductionSuccessorOptions) {
			o.SuccessorApprovalPath = f.cfg.OwnerRecycleApproval.Approval.Path
		}, diagnostic: "overlaps the signed original"},
		"state": {mutate: func(o *ReleaseProductionSuccessorOptions) {
			o.OriginalAuthorityPath = filepath.Join(f.cfg.StateDir, "original.json")
		}, diagnostic: "overlaps the signed original"},
		"evidence": {mutate: func(o *ReleaseProductionSuccessorOptions) {
			o.RenderedConfigPath = f.cfg.EvidenceV2.Operators[0].History.Path
		}, diagnostic: "overlaps the signed original"},
		"signature": {mutate: func(o *ReleaseProductionSuccessorOptions) { o.ApprovalSignature = "0x" + strings.Repeat("ab", 64) }, diagnostic: "128 lowercase hex"},
	} {
		changed := options
		item.mutate(&changed)
		if err := validateReleaseProductionSuccessorPaths(f.cfg, f.path, changed); err == nil || !strings.Contains(err.Error(), item.diagnostic) {
			t.Errorf("%s: successor options admitted or refused for another cause: %v", name, err)
		}
	}
}
