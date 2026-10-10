//go:build linux || darwin

// A signed activation-pending config names a drained activation epoch before
// its contracts exist; its rendered successor is built at least one coordinator
// epoch later. The re-approval may move only that drained boundary forward,
// inside the original window, and stays bound to the original through history.
package validator

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// A later finalized drained boundary inside the fixture's signed window
// (first epoch 20 at block 101, window through block 200 and epoch 30).
func productionRenderingTestBoundary(epoch, block uint64) productionSuccessorBoundary {
	return productionSuccessorBoundary{Epoch: epoch, Block: block, Hash: [32]byte(mainnetRuntimeTestBlock(block))}
}

// Builds and publishes one successor whose approval re-selects boundary. The
// inputs are rendered once per fixture: their declared paths are immutable.
func productionRenderingTestPublish(t *testing.T, f *productionPendingTestFixture, rendered []ReleaseEvidenceV2OperatorConfig, boundary *productionSuccessorBoundary) (*releaseProductionSuccessor, ReleaseProductionSuccessorOptions) {
	t.Helper()
	if rendered == nil {
		rendered = productionPendingTestRender(t, f.cfg)
	}
	options := productionPendingTestOptions(t)
	successor, err := buildReleaseProductionSuccessor(t.Context(), f.cfg, f.path, rendered, options, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := successor.publish(t.Context(), hex.EncodeToString(ed25519.Sign(f.private, successor.message))); err != nil {
		t.Fatal(err)
	}
	return successor, options
}

// The launch shape that could not activate: the original's first decision
// epoch passed long before rendering. Its successor re-selects a later drained
// boundary, loads in the producer, admits exactly that first intent, keeps the
// original authority as history and leaves the bootstrap-pinned bytes intact.
func TestProductionRenderedSuccessorAdvancesDrainedActivation(t *testing.T) {
	for _, treasury := range []bool{false, true} {
		f := newProductionPendingTestFixture(t, treasury)
		originalBytes, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
		original := productionPendingTestClone(t, f.approval)
		later := original.FirstNativeEpoch + 3
		if err := requireOwnerRecycleProductionFirstIntent(f.cfg, nil, later); err == nil || !strings.Contains(err.Error(), "missed its signed drained activation epoch") {
			t.Fatal("control: the original no longer refuses a late first intent", treasury, err)
		}
		boundary := productionRenderingTestBoundary(later, 150)
		successor, options := productionRenderingTestPublish(t, f, nil, &boundary)
		if !reflect.DeepEqual(f.approval, original) {
			t.Fatal("building the successor changed the retained original approval", treasury)
		}
		want := productionPendingTestClone(t, original)
		want.ConfigHash, want.FirstNativeEpoch, want.ValidFromNativeBlock = successor.approval.ConfigHash, later, 150
		want.Production.ActivationNativeHash = boundary.Hash
		if !reflect.DeepEqual(successor.approval, want) || want.ValidThroughNativeBlock != original.ValidThroughNativeBlock ||
			want.Production.ValidThroughNativeEpoch != original.Production.ValidThroughNativeEpoch || want.Production.MaximumLastUpdateAge != original.Production.MaximumLastUpdateAge {
			t.Fatal("successor approval moved more than its drained activation", treasury)
		}
		var printed bytes.Buffer
		successor.print(&printed)
		if !strings.Contains(printed.String(), fmt.Sprintf("re-selects the drained activation at native epoch %d, block 150", later)) ||
			!strings.Contains(printed.String(), fmt.Sprintf("first decision must be made in native epoch %d", later)) {
			t.Fatal("successor request does not name its re-selected drained activation", printed.String())
		}
		loaded, err := LoadReleaseConfig(options.RenderedConfigPath)
		if err != nil {
			t.Fatal("producer refused the re-selected successor", treasury, err)
		}
		approved, err := ownerRecycleProductionApproval(loaded)
		if err != nil || !reflect.DeepEqual(approved.Approval, successor.approval) {
			t.Fatal("producer lost the re-selected approval", treasury, err)
		}
		if err := requireOwnerRecycleProductionFirstIntent(loaded, nil, later); err != nil {
			t.Fatal("successor refused its own first decision epoch", treasury, err)
		}
		for _, epoch := range []uint64{original.FirstNativeEpoch, later - 1, later + 1} {
			if err := requireOwnerRecycleProductionFirstIntent(loaded, nil, epoch); err == nil {
				t.Fatal("successor admitted a first intent outside its signed drained epoch", treasury, epoch)
			}
		}
		// The original stays the immutable history entry: its approval and
		// envelope are exactly what bootstrap inspection pinned.
		history := loaded.productionAuthorityHistory.entries[0].config
		prior, err := ownerRecycleProductionApproval(history)
		if err != nil || !reflect.DeepEqual(prior.Approval, original) || productionEconomicSelection(history).Approval != productionEconomicSelection(f.cfg).Approval {
			t.Fatal("successor history is not the original approval", treasury, err)
		}
		inspection, err := InspectProductionBootstrapConfigPreActivation(t.Context(), f.path, originalBytes)
		if err != nil || !reflect.DeepEqual(inspection.Approval, prior.Approval) || inspection.ApprovalReference != productionEconomicSelection(history).Approval {
			t.Fatal("bootstrap inspection no longer names the successor's original authority", treasury, err)
		}
		if after, err := os.ReadFile(f.path); err != nil || !bytes.Equal(after, originalBytes) {
			t.Fatal("rendering changed the bootstrap-pinned original", treasury, err)
		}
		// Native reads between the two activations, like the activation's own
		// prepared snapshot, keep the original's approved runtime window.
		for _, block := range []uint64{101, 120, 149} {
			artifacts, err := releaseHistoricalRuntimeArtifactsAt(loaded, block)
			if err != nil || len(artifacts) != 1 || artifacts[0] != releaseNativeRuntimeIdentity(f.cfg) {
				t.Fatal("original window no longer authenticates its native reads", treasury, block, err)
			}
			if _, err := releaseProductionRuntimeAt(loaded, block, false); err == nil {
				t.Fatal("a pre-activation block acquired current producer authority", treasury, block)
			}
		}
		if _, err := releaseProductionRuntimeAt(loaded, 150, false); err != nil {
			t.Fatal("successor window does not begin at its drained activation", treasury, err)
		}
		if _, err := releaseHistoricalRuntimeArtifactsAt(loaded, 100); err == nil {
			t.Fatal("a block before the original window was admitted", treasury)
		}
	}
}

// Every successor below moves the drained activation correctly and carries a
// valid approval-key signature, yet also moves another signed term. Each term
// stays the original's; the producer refuses the successor.
func TestProductionRenderedSuccessorRefusesMovingOtherSignedTerms(t *testing.T) {
	f := newProductionPendingTestFixture(t, true)
	boundary := productionRenderingTestBoundary(f.approval.FirstNativeEpoch+3, 150)
	successor, _ := productionRenderingTestPublish(t, f, nil, &boundary)
	resign := func(mutate func(*OwnerRecycleApproval)) string {
		cfg, approval := productionPendingTestClone(t, *successor.config), productionPendingTestClone(t, successor.approval)
		cfg.TreasuryApproval.Approval = ReleaseEvidenceV2File{Path: filepath.Join(identityTestStateDir(t), "approval.json")}
		mutate(&approval)
		path, _ := productionPendingTestSign(t, cfg, approval, f.private)
		return path
	}
	if _, err := LoadReleaseConfig(resign(func(*OwnerRecycleApproval) {})); err != nil {
		t.Fatal("faithful re-signed successor control refused", err)
	}
	for name, mutate := range map[string]func(*OwnerRecycleApproval){
		"window end":         func(approval *OwnerRecycleApproval) { approval.ValidThroughNativeBlock-- },
		"epoch window end":   func(approval *OwnerRecycleApproval) { approval.Production.ValidThroughNativeEpoch-- },
		"last update age":    func(approval *OwnerRecycleApproval) { approval.Production.MaximumLastUpdateAge++ },
		"runtime review":     func(approval *OwnerRecycleApproval) { approval.RuntimeReviewHash[0] ^= 1 },
		"native chain":       func(approval *OwnerRecycleApproval) { approval.NativeChain += " other" },
		"subnet owner":       func(approval *OwnerRecycleApproval) { approval.SubnetOwner[0] ^= 1 },
		"subnet uid bound":   func(approval *OwnerRecycleApproval) { approval.MaximumSubnetUids++ },
		"owned hotkey bound": func(approval *OwnerRecycleApproval) { approval.MaximumOwnedHotkeys-- },
		"owner hotkeys":      func(approval *OwnerRecycleApproval) { approval.OwnerHotkeys = approval.OwnerHotkeys[:1] },
		"validator census": func(approval *OwnerRecycleApproval) {
			approval.Production.ValidatorHotkeys = append(approval.Production.ValidatorHotkeys, [32]byte(bytes.Repeat([]byte{0xee}, 32)))
		},
		"treasury recipient": func(approval *OwnerRecycleApproval) { approval.Proposal.Treasury.Recipients[1].RegistrationBlock++ },
	} {
		if _, err := LoadReleaseConfig(resign(mutate)); err == nil || !strings.Contains(err.Error(), "original economic approval") {
			t.Errorf("%s: signed successor admitted or refused for another cause: %v", name, err)
		}
	}
}

// A re-selected boundary must be a strictly later epoch at a strictly later
// block, keep the activation at the new valid_from in the original's wire
// form, name a new nonzero block hash and fit the original window. The builder
// refuses each such request before any successor file exists, and the producer
// refuses each one even when the approval key signed it.
func TestProductionRenderedSuccessorRefusesNonAdvancingBoundary(t *testing.T) {
	f := newProductionPendingTestFixture(t, true)
	first, from := f.approval.FirstNativeEpoch, f.approval.ValidFromNativeBlock
	options := productionPendingTestOptions(t)
	rendered := productionPendingTestRender(t, f.cfg)
	for name, boundary := range map[string]productionSuccessorBoundary{
		"earlier epoch":      productionRenderingTestBoundary(first-1, 150),
		"equal epoch":        productionRenderingTestBoundary(first, 150),
		"equal block":        {Epoch: first + 3, Block: from, Hash: [32]byte(mainnetRuntimeTestBlock(150))},
		"earlier block":      productionRenderingTestBoundary(first+3, from-1),
		"original hash":      {Epoch: first + 3, Block: 150, Hash: f.approval.Production.ActivationNativeHash},
		"zero hash":          {Epoch: first + 3, Block: 150},
		"block after window": productionRenderingTestBoundary(first+3, f.approval.ValidThroughNativeBlock+1),
		"epoch after window": productionRenderingTestBoundary(f.approval.Production.ValidThroughNativeEpoch+1, 150),
	} {
		if _, err := buildReleaseProductionSuccessor(t.Context(), f.cfg, f.path, rendered, options, &boundary); err == nil || !strings.Contains(err.Error(), "may only advance") {
			t.Errorf("%s: successor request built or refused for another cause: %v", name, err)
		}
	}
	for _, path := range []string{options.RenderedConfigPath, options.SuccessorApprovalPath, options.OriginalAuthorityPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("a refused boundary wrote successor custody", path, err)
		}
	}
	boundary := productionRenderingTestBoundary(first+3, 150)
	successor, _ := productionRenderingTestPublish(t, f, rendered, &boundary)
	resign := func(mutate func(*OwnerRecycleApproval)) string {
		cfg, approval := productionPendingTestClone(t, *successor.config), productionPendingTestClone(t, successor.approval)
		cfg.TreasuryApproval.Approval = ReleaseEvidenceV2File{Path: filepath.Join(identityTestStateDir(t), "approval.json")}
		mutate(&approval)
		path, _ := productionPendingTestSign(t, cfg, approval, f.private)
		return path
	}
	for name, item := range map[string]struct {
		mutate     func(*OwnerRecycleApproval)
		diagnostic string
	}{
		"earlier epoch":  {mutate: func(approval *OwnerRecycleApproval) { approval.FirstNativeEpoch = first - 1 }, diagnostic: "may only advance"},
		"equal epoch":    {mutate: func(approval *OwnerRecycleApproval) { approval.FirstNativeEpoch = first }, diagnostic: "may only advance"},
		"original block": {mutate: func(approval *OwnerRecycleApproval) { approval.ValidFromNativeBlock = from }, diagnostic: "may only advance"},
		"original hash": {mutate: func(approval *OwnerRecycleApproval) {
			approval.Production.ActivationNativeHash = f.approval.Production.ActivationNativeHash
		}, diagnostic: "may only advance"},
		"activation before valid_from": {mutate: func(approval *OwnerRecycleApproval) { approval.Production.ActivationNativeBlock = 140 }, diagnostic: "may only advance"},
		"explicit activation wire":     {mutate: func(approval *OwnerRecycleApproval) { approval.Production.ActivationNativeBlock = 150 }, diagnostic: "may only advance"},
		"epoch after window": {mutate: func(approval *OwnerRecycleApproval) {
			approval.FirstNativeEpoch = approval.Production.ValidThroughNativeEpoch + 1
		}, diagnostic: "finite epoch window"},
	} {
		if _, err := LoadReleaseConfig(resign(item.mutate)); err == nil || !strings.Contains(err.Error(), item.diagnostic) {
			t.Errorf("%s: signed successor admitted or refused for another cause: %v", name, err)
		}
	}
}

// Activate reads the latest drained native boundary from finalized history
// with the same readers every decision uses. The original's own boundary is
// kept while current; a later one is re-selected only at or after the common
// activation boundary, and an undrained boundary is refused.
func TestProductionSuccessorBoundaryReadsLatestDrainedNativeEpoch(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	admission := fixture.operator.measurement.admission
	approved, err := ownerRecycleProductionApproval(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	original := approved.Approval
	put := func(name string, value uint64) {
		admission.storage[fixture.storageNameKVs[name]] = codec.HexEncodeToString(binary.LittleEndian.AppendUint64(nil, value))
	}
	var output bytes.Buffer
	setup := &releaseActivationSetup{cfg: fixture.cfg, native: admission.chain, output: &output}
	completed := func(block uint64) *ReleaseActivationSetupCompletedV2 {
		return &ReleaseActivationSetupCompletedV2{Schema: ReleaseActivationSetupCompletedSchemaV2, Boundary: ReleaseActivationSetupHeadV2{Number: block}}
	}
	put("LastEpochBlock", original.ValidFromNativeBlock)
	selected, ready, err := setup.productionSuccessorBoundary(t.Context(), completed(original.ValidFromNativeBlock))
	if err != nil || !ready || selected != nil {
		t.Fatal("the original's current drained boundary was not kept", selected, ready, err)
	}
	facts, err := readOwnerRecycleDrainedBoundaryAt(t.Context(), admission.chain, fixture.cfg, types.Hash(original.Production.ActivationNativeHash))
	if err != nil || facts.Block != original.ValidFromNativeBlock || facts.Epoch != original.FirstNativeEpoch || facts.PendingServerEmission != 0 {
		t.Fatal("drained boundary reader disagrees with the signed activation", facts, err)
	}
	fixture.head, fixture.epoch = original.ValidFromNativeBlock+10, original.FirstNativeEpoch+1
	put("LastEpochBlock", original.ValidFromNativeBlock+5)
	output.Reset()
	if selected, ready, err := setup.productionSuccessorBoundary(t.Context(), completed(original.ValidFromNativeBlock+7)); err != nil || ready || selected != nil || !strings.Contains(output.String(), "waiting: no native epoch has drained") {
		t.Fatal("a boundary before the activation boundary was selected", selected, ready, err, output.String())
	}
	selected, ready, err = setup.productionSuccessorBoundary(t.Context(), completed(original.ValidFromNativeBlock+3))
	want := productionSuccessorBoundary{Epoch: original.FirstNativeEpoch + 1, Block: original.ValidFromNativeBlock + 5, Hash: [32]byte(fixture.block(original.ValidFromNativeBlock + 5))}
	if err != nil || !ready || selected == nil || *selected != want {
		t.Fatal("the latest drained boundary was not re-selected", selected, ready, err)
	}
	put("PendingServerEmission", 1)
	if _, _, err := setup.productionSuccessorBoundary(t.Context(), completed(original.ValidFromNativeBlock+3)); err == nil || !strings.Contains(err.Error(), "is not drained") {
		t.Fatal("an undrained native epoch boundary was selected", err)
	}
}
