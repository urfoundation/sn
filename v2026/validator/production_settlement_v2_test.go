//go:build linux || darwin

// Settlement v2 adoption is an exact successor class: every successor below is
// signed by the real approval key, so each refusal comes from the class rule.
package validator

import (
	"crypto/ed25519"
	"encoding/hex"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A signed predecessor loaded by the producer loader, with its approval key.
type settlementV2TestPredecessor struct {
	cfg      *ReleaseConfig
	approval OwnerRecycleApproval
	private  ed25519.PrivateKey
}

// Today's shape is a treasury production config; the owner-recycle original
// precedes it.
func newSettlementV2TestPredecessor(t *testing.T, treasury bool) settlementV2TestPredecessor {
	t.Helper()
	runtime := newProductionRuntimeTestFixture(t, false)
	cfg, approval := productionPendingTestClone(t, *runtime.cfg), productionPendingTestClone(t, runtime.approval)
	if treasury {
		productionPendingTestTreasury(&cfg, &approval)
	}
	productionEconomicSelection(&cfg).Approval = ReleaseEvidenceV2File{Path: filepath.Join(identityTestStateDir(t), "approval.json")}
	path, approved := productionPendingTestSign(t, cfg, approval, runtime.private)
	loaded, err := LoadReleaseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	return settlementV2TestPredecessor{cfg: loaded, approval: approved, private: runtime.private}
}

// The successor appends the predecessor's exact bundle to its history and
// nominates a new approval file; mutate makes the change under test.
func (self settlementV2TestPredecessor) successor(t *testing.T, mutate func(*ReleaseConfig, *OwnerRecycleApproval)) (string, OwnerRecycleApproval) {
	t.Helper()
	bundle, err := BuildOwnerRecycleProductionAuthority(t.Context(), self.cfg)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := WriteReleaseEvidenceV2File(t.Context(), filepath.Join(identityTestStateDir(t), "original-authority.json"), bundle, maximumProductionAuthorityBundleBytes)
	if err != nil {
		t.Fatal(err)
	}
	cfg, approval := productionPendingTestClone(t, *self.cfg), productionPendingTestClone(t, self.approval)
	cfg.ProductionAuthorityHistory = append(cfg.ProductionAuthorityHistory, reference)
	productionEconomicSelection(&cfg).Approval = ReleaseEvidenceV2File{Path: filepath.Join(identityTestStateDir(t), "approval.json")}
	mutate(&cfg, &approval)
	return productionPendingTestSign(t, cfg, approval, self.private)
}

// Adds the block and nothing else.
func settlementV2TestAdopt(cfg *ReleaseConfig, _ *OwnerRecycleApproval) {
	block := settlementV2TestBlock
	cfg.SettlementV2 = &block
}

// The adopted successor, loaded, as the predecessor of a later change.
func newSettlementV2TestAdopted(t *testing.T) settlementV2TestPredecessor {
	t.Helper()
	predecessor := newSettlementV2TestPredecessor(t, true)
	path, approval := predecessor.successor(t, settlementV2TestAdopt)
	loaded, err := LoadReleaseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	return settlementV2TestPredecessor{cfg: loaded, approval: approval, private: predecessor.private}
}

// VA-6: the successor that adds exactly the block, appends exactly the
// predecessor's bundle and re-signs the old approval for its new config hash
// loads; its history is the exact signed predecessor.
func TestProductionSettlementV2AdoptionAdmitsExactlyTheBlock(t *testing.T) {
	predecessor := newSettlementV2TestPredecessor(t, true)
	path, approval := predecessor.successor(t, settlementV2TestAdopt)
	loaded, err := LoadReleaseConfig(path)
	if err != nil {
		t.Fatal("adoption successor refused", err)
	}
	approved, err := ownerRecycleProductionApproval(loaded)
	if err != nil {
		t.Fatal(err)
	}
	want := predecessor.approval
	want.ConfigHash = approval.ConfigHash
	if approval.ConfigHash == predecessor.approval.ConfigHash || !reflect.DeepEqual(approved.Approval, want) || *loaded.SettlementV2 != settlementV2TestBlock {
		t.Fatal("adopted approval is not the old one re-signed for the new config hash")
	}
	history := loaded.productionAuthorityHistory.entries
	if len(history) != 1 || history[0].config.SettlementV2 != nil || !reflect.DeepEqual(history[0].config.ownerRecycleProduction.encoded, predecessor.cfg.ownerRecycleProduction.encoded) {
		t.Fatal("adopted history is not the exact signed predecessor")
	}
}

// VA-6: once adopted, the block stays through a later runtime renewal.
func TestProductionSettlementV2AdoptedBlockSurvivesRenewal(t *testing.T) {
	adopted := newSettlementV2TestAdopted(t)
	path, _ := adopted.successor(t, func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) { cfg.PollSeconds++ })
	loaded, err := LoadReleaseConfig(path)
	if err != nil {
		t.Fatal("renewal of an adopted config refused", err)
	}
	if *loaded.SettlementV2 != settlementV2TestBlock || len(loaded.productionAuthorityHistory.entries) != 2 {
		t.Fatal("renewal lost the adopted block or its lineage")
	}
}

// VA-6: an adopted block can be neither re-pointed nor removed.
func TestProductionSettlementV2AdoptedBlockIsFixed(t *testing.T) {
	adopted := newSettlementV2TestAdopted(t)
	for name, block := range map[string]*ReleaseSettlementV2Config{
		"re-pointed settlement": {Settlement: "0x5e77000000000000000000000000000000000003", Vault: settlementV2TestBlock.Vault},
		"re-pointed vault":      {Settlement: settlementV2TestBlock.Settlement, Vault: "0x5e77000000000000000000000000000000000003"},
		"removed":               nil,
	} {
		path, _ := adopted.successor(t, func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) { cfg.SettlementV2 = block })
		if _, err := LoadReleaseConfig(path); err == nil || !strings.Contains(err.Error(), "continuity changes policy, identity, transport or custody") {
			t.Errorf("%s: signed successor admitted or refused for another cause: %v", name, err)
		}
	}
}

// VA-6: adoption is exclusive. Each signed successor adds the block together
// with one change another class would admit, or any other config change.
func TestProductionSettlementV2AdoptionRefusesEveryOtherChange(t *testing.T) {
	predecessor := newSettlementV2TestPredecessor(t, true)
	beyondBlock := "settlement v2 adoption changes signed configuration beyond the added settlement_v2 block"
	beyondHash := "settlement v2 adoption changes the approval beyond its config hash"
	for name, item := range map[string]struct {
		mutate     func(*ReleaseConfig, *OwnerRecycleApproval)
		diagnostic string
	}{
		"poll interval": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) { cfg.PollSeconds++ }, diagnostic: beyondBlock},
		"runtime pin": {mutate: func(cfg *ReleaseConfig, approval *OwnerRecycleApproval) {
			cfg.RuntimeSpec++
			cfg.RuntimeCodeHash = releaseHex32([32]byte{0x95})
			approval.Proposal.Runtime.Version.SpecVersion = cfg.RuntimeSpec
			approval.Proposal.Runtime.CodeHash = [32]byte{0x95}
			approval.RuntimeReviewHash = recycleTestId(4201)
		}, diagnostic: beyondBlock},
		"approval renewal": {mutate: func(_ *ReleaseConfig, approval *OwnerRecycleApproval) { approval.ValidThroughNativeBlock++ }, diagnostic: beyondHash},
		"capacity revision": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) {
			cfg.EvidenceV2.Bounds.MaxHistoryBytes *= 2
			cfg.ProductionCapacityRevision = &ProductionCapacityRevision{Schema: ProductionCapacityRevisionSchema, Margin: 2}
		}, diagnostic: beyondBlock},
		"operator route": {mutate: func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) { cfg.Operators[0].APIURL = "https://other.example" }, diagnostic: beyondBlock},
	} {
		path, _ := predecessor.successor(t, func(cfg *ReleaseConfig, approval *OwnerRecycleApproval) {
			settlementV2TestAdopt(cfg, approval)
			item.mutate(cfg, approval)
		})
		if _, err := LoadReleaseConfig(path); err == nil || !strings.Contains(err.Error(), item.diagnostic) {
			t.Errorf("%s: signed adoption successor admitted or refused for another cause: %v", name, err)
		}
	}
}

// VA-6: adoption cannot carry the owner-recycle to treasury transition.
func TestProductionSettlementV2AdoptionRefusesTreasuryTransition(t *testing.T) {
	predecessor := newSettlementV2TestPredecessor(t, false)
	path, _ := predecessor.successor(t, func(cfg *ReleaseConfig, approval *OwnerRecycleApproval) {
		settlementV2TestAdopt(cfg, approval)
		productionPendingTestTreasury(cfg, approval)
		approval.Proposal.PolicyId++
		approval.Proposal.EffectiveEpoch++
		approval.FirstNativeEpoch++
		approval.ValidFromNativeBlock = 150
		approval.Production.ActivationNativeBlock = 150
		approval.Production.ActivationNativeHash = [32]byte(mainnetRuntimeTestBlock(150))
	})
	if _, err := LoadReleaseConfig(path); err == nil || !strings.Contains(err.Error(), "settlement v2 adoption changes signed configuration beyond the added settlement_v2 block") {
		t.Fatal("adoption carried the treasury transition or refused it for another cause", err)
	}
}

// VA-6: adoption cannot render an activation-pending census.
func TestProductionSettlementV2AdoptionRefusesRendering(t *testing.T) {
	pending := newProductionPendingTestFixture(t, true)
	rendered := productionPendingTestRender(t, pending.cfg)
	predecessor := settlementV2TestPredecessor{cfg: pending.cfg, approval: pending.approval, private: pending.private}
	path, _ := predecessor.successor(t, func(cfg *ReleaseConfig, approval *OwnerRecycleApproval) {
		settlementV2TestAdopt(cfg, approval)
		cfg.EvidenceV2.Operators = rendered
	})
	if _, err := LoadReleaseConfig(path); err == nil || !strings.Contains(err.Error(), "settlement v2 adoption changes signed configuration beyond the added settlement_v2 block") {
		t.Fatal("adoption carried a rendering or refused it for another cause", err)
	}
}

// VA-6: the successor's history is the predecessor's plus exactly one entry.
func TestProductionSettlementV2AdoptionRequiresOneAppendedEntry(t *testing.T) {
	entry := func(name string) ReleaseEvidenceV2File {
		return ReleaseEvidenceV2File{Path: filepath.Join("/srv", name), Bytes: 1, SHA256: releaseHex32([32]byte{name[0]})}
	}
	block := settlementV2TestBlock
	for name, histories := range map[string][2][]ReleaseEvidenceV2File{
		"none appended":  {{entry("a")}, {entry("a")}},
		"two appended":   {nil, {entry("a"), entry("b")}},
		"changed prefix": {{entry("a")}, {entry("c"), entry("b")}},
	} {
		original := &ReleaseConfig{ProductionAuthorityHistory: histories[0]}
		current := &ReleaseConfig{ProductionAuthorityHistory: histories[1], SettlementV2: &block}
		if err := validateProductionSettlementV2Adoption(original, current, OwnerRecycleApproval{}, OwnerRecycleApproval{}); err == nil ||
			!strings.Contains(err.Error(), "must append exactly one authority history entry") {
			t.Errorf("%s: history admitted or refused for another cause: %v", name, err)
		}
	}
}

// VA-6: the class compares the two signed approvals, so it refuses a
// predecessor whose authority was never loaded and a successor that differs
// from the config its approval names.
func TestProductionSettlementV2AdoptionRequiresBothLoadedApprovals(t *testing.T) {
	block := settlementV2TestBlock
	if err := validateProductionAuthorityContinuity(&ReleaseConfig{}, &ReleaseConfig{SettlementV2: &block}); err == nil ||
		!strings.Contains(err.Error(), "owner-recycle production configuration lacks independently loaded authority") {
		t.Fatal("adoption admitted an unloaded predecessor or refused it for another cause", err)
	}
	predecessor := newSettlementV2TestPredecessor(t, true)
	unsigned := *predecessor.cfg
	unsigned.SettlementV2 = &block
	if err := validateProductionAuthorityContinuity(predecessor.cfg, &unsigned); err == nil ||
		!strings.Contains(err.Error(), "owner-recycle production configuration changed after approval") {
		t.Fatal("adoption admitted a block its approval never signed or refused it for another cause", err)
	}
}

// VA-9: activating a pending config that carries settlement_v2 renders a
// successor that keeps the block and loads under the rendering class.
func TestProductionRenderedSuccessorKeepsSettlementV2(t *testing.T) {
	runtime := newProductionRuntimeTestFixture(t, false)
	cfg, approval := productionPendingTestClone(t, *runtime.cfg), productionPendingTestClone(t, runtime.approval)
	cfg.EvidenceV2.Operators = productionPendingTestEvidence(cfg.EvidenceV2.Operators)
	productionPendingTestTreasury(&cfg, &approval)
	settlementV2TestAdopt(&cfg, &approval)
	path, _ := productionPendingTestSign(t, cfg, approval, runtime.private)
	pending, err := LoadReleaseProductionConfigPreActivation(path)
	if err != nil {
		t.Fatal(err)
	}
	rendered := productionPendingTestRender(t, pending)
	options := productionPendingTestOptions(t)
	successor, err := buildReleaseProductionSuccessor(t.Context(), pending, path, rendered, options, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := successor.publish(t.Context(), hex.EncodeToString(ed25519.Sign(runtime.private, successor.message))); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadReleaseConfig(options.RenderedConfigPath)
	if err != nil {
		t.Fatal("rendered successor of a settlement v2 config refused", err)
	}
	if *loaded.SettlementV2 != settlementV2TestBlock || *loaded.productionAuthorityHistory.entries[0].config.SettlementV2 != settlementV2TestBlock {
		t.Fatal("rendering lost the settlement_v2 block")
	}
}

// VA-6: an adopted block stays fixed through a capacity revision. The
// revision that keeps it loads; with the block removed or re-pointed the same
// signed revision is refused by the continuity rule that keeps the block.
func TestProductionSettlementV2CapacityRevisionKeepsTheBlock(t *testing.T) {
	predecessor := newSettlementV2TestPredecessor(t, false)
	path, approval := predecessor.successor(t, settlementV2TestAdopt)
	adopted, err := LoadReleaseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	kept := settlementV2TestBlock
	for name, block := range map[string]*ReleaseSettlementV2Config{
		"kept":       &kept,
		"removed":    nil,
		"re-pointed": {Settlement: settlementV2TestBlock.Settlement, Vault: "0x5e77000000000000000000000000000000000003"},
	} {
		cfg, next := productionCapacityTestDraft(t, adopted, approval)
		cfg.SettlementV2 = block
		_, err := LoadReleaseConfig(productionCapacityTestSign(t, cfg, next, predecessor.private).path)
		if name == "kept" {
			if err != nil {
				t.Fatal("capacity revision of an adopted config refused", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "continuity changes policy, identity, transport or custody") {
			t.Errorf("%s: signed capacity revision admitted or refused for another cause: %v", name, err)
		}
	}
}

// VA-6: rendering a pending config that carries the block keeps it. The
// rendering that keeps it loads; one that removes or re-points it is refused
// by the rendering class's exact projection.
func TestProductionSettlementV2RenderingKeepsTheBlock(t *testing.T) {
	runtime := newProductionRuntimeTestFixture(t, false)
	cfg, approval := productionPendingTestClone(t, *runtime.cfg), productionPendingTestClone(t, runtime.approval)
	cfg.EvidenceV2.Operators = productionPendingTestEvidence(cfg.EvidenceV2.Operators)
	productionPendingTestTreasury(&cfg, &approval)
	settlementV2TestAdopt(&cfg, &approval)
	path, approved := productionPendingTestSign(t, cfg, approval, runtime.private)
	pending, err := LoadReleaseProductionConfigPreActivation(path)
	if err != nil {
		t.Fatal(err)
	}
	rendered := productionPendingTestRender(t, pending)
	predecessor := settlementV2TestPredecessor{cfg: pending, approval: approved, private: runtime.private}
	kept := settlementV2TestBlock
	for name, block := range map[string]*ReleaseSettlementV2Config{
		"kept":       &kept,
		"removed":    nil,
		"re-pointed": {Settlement: "0x5e77000000000000000000000000000000000003", Vault: settlementV2TestBlock.Vault},
	} {
		path, _ := predecessor.successor(t, func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) {
			cfg.EvidenceV2.Operators = rendered
			cfg.SettlementV2 = block
		})
		_, err := LoadReleaseConfig(path)
		if name == "kept" {
			if err != nil {
				t.Fatal("rendering that keeps the block refused", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "evidence rendering changes signed configuration beyond its rendered references") {
			t.Errorf("%s: signed rendering admitted or refused for another cause: %v", name, err)
		}
	}
}
