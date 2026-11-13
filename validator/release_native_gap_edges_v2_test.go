//go:build linux || darwin

package validator

import (
	"bytes"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

const (
	nativeGapOwnTestLast   = uint64(26073)
	nativeGapOwnTestNative = uint64(26105)
	nativeGapOwnTestBlock  = uint64(9410050)
)

// The retained applied intent and the source slot its finalized write left.
func nativeGapOwnTestCurrent() *SteeringIntent {
	intent := nativeGapBridgeTestIntent(nativeGapOwnTestLast, "applied")
	intent.Prepared = &crv4.PreparedSubmission{SourceCommitment: &crv4.PreparedSourceCommitment{Hash: releaseHex32([32]byte{0x5c})}}
	intent.FinalizedBlock = nativeGapOwnTestBlock
	return intent
}

func nativeGapOwnTestSlot() *crv4.FinalizedCommitment {
	return &crv4.FinalizedCommitment{Hash: [32]byte{0x5c}, CommitmentBlock: nativeGapOwnTestBlock}
}

func nativeGapOwnTestApproved(through uint64) func(uint64) bool {
	return func(epoch uint64) bool { return epoch <= through }
}

func nativeGapOwnTestConfig(t *testing.T) *ReleaseConfig {
	t.Helper()
	return &ReleaseConfig{ChainID: 964, DeploymentID: "sn25-mainnet-20261007-01", ValidatorID: 1, StateDir: newReleaseHeadV2TestStateDir(t)}
}

// The rule refuses everything that needs a person: anything but a plain production
// run, an unfinished predecessor, a state whose applied intent is not the hotkey's
// latest public decision, a very long gap and an approval about to end.
func TestNativeGapOwnEdgeRule(t *testing.T) {
	t.Parallel()
	approved := nativeGapOwnTestApproved(26990)
	if refusal := releaseNativeGapOwnEdgeRefusal(true, nativeGapOwnTestCurrent(), nativeGapOwnTestNative, approved, nativeGapOwnTestSlot()); refusal != "" {
		t.Fatalf("the ordinary gap was refused: %s", refusal)
	}
	if refusal := releaseNativeGapOwnEdgeRefusal(true, nativeGapOwnTestCurrent(), nativeGapOwnTestLast+releaseNativeGapOwnEdgeMaximumEpochs, approved, nativeGapOwnTestSlot()); refusal != "" {
		t.Fatalf("the longest allowed gap was refused: %s", refusal)
	}
	status := func(value string) *SteeringIntent {
		intent := nativeGapOwnTestCurrent()
		intent.Status = value
		return intent
	}
	unprepared := nativeGapOwnTestCurrent()
	unprepared.Prepared = nil
	noSource := nativeGapOwnTestCurrent()
	noSource.Prepared.SourceCommitment = nil
	unfinalized := nativeGapOwnTestCurrent()
	unfinalized.FinalizedBlock = 0
	laterDecision := nativeGapOwnTestSlot()
	laterDecision.Hash = [32]byte{0x5d}
	laterBlock := nativeGapOwnTestSlot()
	laterBlock.CommitmentBlock++
	for _, test := range []struct {
		name     string
		plain    bool
		current  *SteeringIntent
		native   uint64
		approved func(uint64) bool
		observed *crv4.FinalizedCommitment
		want     string
	}{
		{"provisional or adopted history", false, nativeGapOwnTestCurrent(), nativeGapOwnTestNative, approved, nativeGapOwnTestSlot(), nativeGapEdgeNotPlainProduction},
		{"no approval to read", true, nativeGapOwnTestCurrent(), nativeGapOwnTestNative, nil, nativeGapOwnTestSlot(), nativeGapEdgeNotPlainProduction},
		{"no intent", true, nil, nativeGapOwnTestNative, approved, nativeGapOwnTestSlot(), nativeGapEdgePredecessorStatus},
		{"pending may still land", true, status("pending"), nativeGapOwnTestNative, approved, nativeGapOwnTestSlot(), nativeGapEdgePredecessorStatus},
		{"finalized not yet applied", true, status("finalized"), nativeGapOwnTestNative, approved, nativeGapOwnTestSlot(), nativeGapEdgePredecessorStatus},
		{"failed", true, status("failed"), nativeGapOwnTestNative, approved, nativeGapOwnTestSlot(), nativeGapEdgePredecessorStatus},
		{"no prepared commitment", true, unprepared, nativeGapOwnTestNative, approved, nativeGapOwnTestSlot(), nativeGapEdgePredecessorStatus},
		{"no source commitment", true, noSource, nativeGapOwnTestNative, approved, nativeGapOwnTestSlot(), nativeGapEdgePredecessorStatus},
		{"never finalized", true, unfinalized, nativeGapOwnTestNative, approved, nativeGapOwnTestSlot(), nativeGapEdgePredecessorStatus},
		{"no gap at all", true, nativeGapOwnTestCurrent(), nativeGapOwnTestLast + 1, approved, nativeGapOwnTestSlot(), nativeGapEdgeTooLong},
		{"longer than the bound", true, nativeGapOwnTestCurrent(), nativeGapOwnTestLast + releaseNativeGapOwnEdgeMaximumEpochs + 1, approved, nativeGapOwnTestSlot(), nativeGapEdgeTooLong},
		{"approval ends at this epoch", true, nativeGapOwnTestCurrent(), nativeGapOwnTestNative, nativeGapOwnTestApproved(nativeGapOwnTestNative), nativeGapOwnTestSlot(), nativeGapEdgeApprovalWindow},
		{"slot read empty", true, nativeGapOwnTestCurrent(), nativeGapOwnTestNative, approved, nil, nativeGapEdgeSourceSlot},
		{"restored state: a later decision is public", true, nativeGapOwnTestCurrent(), nativeGapOwnTestNative, approved, laterDecision, nativeGapEdgeSourceSlot},
		{"same hash from another block", true, nativeGapOwnTestCurrent(), nativeGapOwnTestNative, approved, laterBlock, nativeGapEdgeSourceSlot},
	} {
		if refusal := releaseNativeGapOwnEdgeRefusal(test.plain, test.current, test.native, test.approved, test.observed); refusal != test.want {
			t.Fatalf("%s: got %q, want %q", test.name, refusal, test.want)
		}
	}
}

// The recorded file is exactly the pinned file's canonical wire, private to the
// validator, and anything else at that path refuses startup.
func TestNativeGapOwnEdgesFile(t *testing.T) {
	t.Parallel()
	cfg := nativeGapOwnTestConfig(t)
	if bridge, err := readReleaseNativeGapOwnEdges(cfg.StateDir); bridge != nil || err != nil {
		t.Fatalf("no file must mean no edges: %v %v", bridge, err)
	}
	first := ReleaseNativeGapBridgeEdge{LastNativeEpoch: nativeGapOwnTestLast, LastArtifactHash: nativeGapBridgeTestArtifactHash(nativeGapOwnTestLast), FirstNativeEpoch: nativeGapOwnTestNative + 1}
	if err := recordReleaseNativeGapOwnEdge(cfg, first); err != nil {
		t.Fatal(err)
	}
	second := ReleaseNativeGapBridgeEdge{LastNativeEpoch: nativeGapOwnTestNative + 9, LastArtifactHash: nativeGapBridgeTestArtifactHash(nativeGapOwnTestNative + 9), FirstNativeEpoch: nativeGapOwnTestNative + 12}
	if err := recordReleaseNativeGapOwnEdge(cfg, second); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.StateDir, releaseNativeGapOwnEdgesName)
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the edges file is not private: %v %v", info, err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, encodeNativeGapBridgeEdgesTest(t, nativeGapBridgeEdgesTestWire([]ReleaseNativeGapBridgeEdge{first, second}))) {
		t.Fatalf("the edges file is not the pinned file's canonical wire: %s", encoded)
	}
	if _, err := DecodeReleaseNativeGapBridge(encoded, ReleaseMeasurementContentHash(encoded)); err != nil {
		t.Fatalf("the pinned decoder refuses the recorded wire: %v", err)
	}
	bridge, err := readReleaseNativeGapOwnEdges(cfg.StateDir)
	if err != nil || !slices.Equal(bridge.Edges(), []ReleaseNativeGapBridgeEdge{first, second}) {
		t.Fatalf("the edges did not read back: %v %v", bridge, err)
	}
	// An edge that overlaps the last one would be refused by the next start, so
	// it is never written.
	overlapping := ReleaseNativeGapBridgeEdge{LastNativeEpoch: nativeGapOwnTestNative + 10, LastArtifactHash: nativeGapBridgeTestArtifactHash(1), FirstNativeEpoch: nativeGapOwnTestNative + 14}
	if err := recordReleaseNativeGapOwnEdge(cfg, overlapping); err == nil {
		t.Fatal("an overlapping edge was recorded")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, encoded) {
		t.Fatalf("a refused edge changed the file: %v", err)
	}
	if err := os.WriteFile(path, append(bytes.Clone(encoded), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readReleaseNativeGapOwnEdges(cfg.StateDir); err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("a non-canonical file was read: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(cfg.StateDir, "elsewhere")
	if err := os.WriteFile(target, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := readReleaseNativeGapOwnEdges(cfg.StateDir); err == nil {
		t.Fatal("a link was read as the edges file")
	}
}

// Pinned and recorded edges join in history order. Without recorded edges the
// pinned bridge is untouched, which is what keeps every older history's replay
// exactly as it was.
func TestNativeGapOwnEdgesMerge(t *testing.T) {
	t.Parallel()
	list := nativeGapBridgeEdgesTestList()
	pinned := nativeGapBridgeEdgesTestBridge(list...)
	if merged, err := mergeReleaseNativeGapBridges(&pinned, nil); err != nil || merged != &pinned {
		t.Fatalf("no recorded edges changed the pinned bridge: %v", err)
	}
	if merged, err := mergeReleaseNativeGapBridges(nil, nil); err != nil || merged != nil {
		t.Fatalf("no edges at all invented a bridge: %v", err)
	}
	later := ReleaseNativeGapBridgeEdge{LastNativeEpoch: 25800, LastArtifactHash: nativeGapBridgeTestArtifactHash(25800), FirstNativeEpoch: 25803}
	own := nativeGapBridgeEdgesTestBridge(later)
	if merged, err := mergeReleaseNativeGapBridges(nil, &own); err != nil || merged != &own {
		t.Fatalf("recorded edges alone were changed: %v", err)
	}
	merged, err := mergeReleaseNativeGapBridges(&pinned, &own)
	if err != nil || !slices.Equal(merged.Edges(), append(slices.Clone(list), later)) {
		t.Fatalf("merge: %v %v", merged, err)
	}
	// The startup record's rule: its edges stay the leading edges of the merge.
	if !slices.Equal(merged.Edges()[:len(list)], list) {
		t.Fatal("recording an edge moved a pinned one")
	}
	// An operator bridging the same gap by hand is not a conflict.
	sameByHand := later
	sameByHand.FirstNativeEpoch = 25802
	hand := nativeGapBridgeEdgesTestBridge(sameByHand)
	merged, err = mergeReleaseNativeGapBridges(&hand, &own)
	if err != nil || !slices.Equal(merged.Edges(), []ReleaseNativeGapBridgeEdge{sameByHand}) {
		t.Fatalf("the same predecessor twice: %v %v", merged, err)
	}
	conflict := later
	conflict.LastArtifactHash = nativeGapBridgeTestArtifactHash(1)
	other := nativeGapBridgeEdgesTestBridge(conflict)
	if _, err := mergeReleaseNativeGapBridges(&other, &own); err == nil {
		t.Fatal("two artifacts for one predecessor were merged")
	}
	overlapping := nativeGapBridgeEdgesTestBridge(ReleaseNativeGapBridgeEdge{LastNativeEpoch: 25801, LastArtifactHash: nativeGapBridgeTestArtifactHash(25801), FirstNativeEpoch: 25804})
	if _, err := mergeReleaseNativeGapBridges(&overlapping, &own); err == nil {
		t.Fatal("overlapping edges were merged")
	}
	foreign := own
	foreign.ValidatorID = 2
	if _, err := mergeReleaseNativeGapBridges(&pinned, &foreign); err == nil {
		t.Fatal("another validator's edges were merged")
	}
}

// The whole recovery on the edge rules. The gap refuses and holds; the recorded
// edge, read back at the next start beside the pinned file, idles until its first
// epoch and then admits exactly what a hand-made edge admits: the next input folds
// once with no decay, and a second gap refuses again.
func TestNativeGapOwnEdgeResumesLikeAPinnedEdge(t *testing.T) {
	t.Parallel()
	limits := headEMAStoreV2TestLimits()
	budget := releaseHeadV2Budget{limit: limits.MaxControlBytes}
	alpha := protocol.Rational{Numerator: 1, Denominator: 4}
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	store, err := NewHeadEMAStoreV2(t.Context(), newReleaseHeadV2TestStateDir(t), limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.FoldForEpochV2(t.Context(), nativeGapOwnTestLast, map[FleetScoreKey]*big.Rat{key: big.NewRat(8, 1)}, alpha); err != nil {
		t.Fatal(err)
	}
	E := nativeGapOwnTestNative
	if !store.refusesNativeEpochGap(E) {
		t.Fatal("the gap must refuse before any edge exists")
	}
	if err := store.admitReleaseHeadV2Known(t.Context(), E, alpha, limits.MaxEntries, budget); !errors.Is(err, errReleaseHeadEMANativeGap) {
		t.Fatalf("head admission without an edge: %v", err)
	}
	current := nativeGapOwnTestCurrent()
	if refusal := releaseNativeGapOwnEdgeRefusal(true, current, E, nativeGapOwnTestApproved(26990), nativeGapOwnTestSlot()); refusal != "" {
		t.Fatal(refusal)
	}
	cfg := nativeGapOwnTestConfig(t)
	F := E + 1
	edge := ReleaseNativeGapBridgeEdge{LastNativeEpoch: current.SubnetEpoch, LastArtifactHash: current.MeasurementArtifactHash, FirstNativeEpoch: F}
	if err := recordReleaseNativeGapOwnEdge(cfg, edge); err != nil {
		t.Fatal(err)
	}
	// The next start: the operator's earlier crossed edges plus the recorded one.
	crossedLast, crossedNext := nativeGapBridgeTestIntent(25893, "applied"), nativeGapBridgeTestIntent(25947, "applied")
	pinned := nativeGapBridgeEdgesTestBridge(ReleaseNativeGapBridgeEdge{LastNativeEpoch: 25893, LastArtifactHash: crossedLast.MeasurementArtifactHash, FirstNativeEpoch: 25947})
	own, err := readReleaseNativeGapOwnEdges(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := mergeReleaseNativeGapBridges(&pinned, own)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := merged.matchHistory(t.Context(), []*SteeringIntent{crossedLast, crossedNext, current})
	if err != nil || owner.crossed {
		t.Fatalf("the next start refused its own pending edge: %v", err)
	}
	if err := owner.requireFirstEpoch(current, E); err != ErrSteeringAlreadyFinal {
		t.Fatalf("the rest of the recording epoch must idle: %v", err)
	}
	if err := owner.requireFirstEpoch(current, F); err != nil {
		t.Fatalf("the first epoch stayed idle: %v", err)
	}
	store.v2.nativeGapBridge = owner
	if store.refusesNativeEpochGap(F) || store.refusesNativeEpochGap(F+3) {
		t.Fatal("the recorded edge did not admit its first epoch or a later one")
	}
	if err := store.admitReleaseHeadV2Known(t.Context(), F, alpha, limits.MaxEntries, budget); err != nil {
		t.Fatalf("head admission with the recorded edge: %v", err)
	}
	_, records, err := store.FoldForEpochV2(t.Context(), F, map[FleetScoreKey]*big.Rat{key: big.NewRat(4, 1)}, alpha)
	if err != nil || len(records) != 1 {
		t.Fatalf("the crossing fold: %d records: %v", len(records), err)
	}
	// α·raw + (1 − α)·prior = 4/4 + 3·8/4 = 7, with prior exactly the last Next.
	if records[0].Prior.Numerator != "8" || records[0].Prior.Denominator != "1" || records[0].Next.Numerator != "7" || records[0].Next.Denominator != "1" {
		t.Fatalf("the missed epochs changed the fold: %+v", records[0])
	}
	crossing := nativeGapBridgeTestIntent(F, "applied")
	if err := validateSteeringIntentSuccessorWithGapsV2(current, crossing, (&releaseIntentV2Owner{nativeGapBridge: owner}).allowsIntentGap(current, crossing)); err != nil {
		t.Fatalf("the crossing intent was refused: %v", err)
	}
	if !owner.allowsArtifactEdge(edge.LastArtifactHash, &ReleaseMeasurementArtifact{SubnetEpoch: current.SubnetEpoch}, &ReleaseMeasurementArtifact{SubnetEpoch: F, PreviousArtifactHash: edge.LastArtifactHash}) {
		t.Fatal("the crossing artifact was refused")
	}
	if crossed, err := merged.matchHistory(t.Context(), []*SteeringIntent{crossedLast, crossedNext, current, crossing}); err != nil || !crossed.crossed {
		t.Fatalf("a restart refused the crossed history: %v", err)
	}
	if !store.refusesNativeEpochGap(F + 2) {
		t.Fatal("the edge admitted a second gap")
	}
}

// Recording ends the process once, so the next start reads the edge. A failed
// slot read holds and retries instead, and spends no failures.
func TestProductionSteeringLoopRestartsOnceForARecordedEdge(t *testing.T) {
	t.Parallel()
	recorded := &productionNativeGapEdgeRecorded{edge: ReleaseNativeGapBridgeEdge{LastNativeEpoch: nativeGapOwnTestLast, LastArtifactHash: nativeGapBridgeTestArtifactHash(nativeGapOwnTestLast), FirstNativeEpoch: nativeGapOwnTestNative + 1}}
	polls, waits := 0, 0
	err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
		polls++
		return recorded
	}, func() bool {
		waits++
		return true
	}, nativeGapHoldTestProgress())
	var ended *productionNativeGapEdgeRecorded
	if !errors.As(err, &ended) || ended.edge != recorded.edge || polls != 1 || waits != 0 {
		t.Fatalf("a recorded edge must end the loop at once: %d polls, %d waits: %v", polls, waits, err)
	}
	if !strings.Contains(err.Error(), "restarting to admit it") {
		t.Fatalf("the exit does not say why: %v", err)
	}
	// With something else in the graph it is an ordinary hard failure.
	polls = 0
	err = runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
		polls++
		return errors.Join(recorded, errors.New("independent cause"))
	}, func() bool { return true }, nativeGapHoldTestProgress())
	var limit *releaseSteeringFailureLimitError
	if !errors.As(err, &limit) || polls != releaseSteeringFailureLimit {
		t.Fatalf("a mixed graph ended the loop early: %d polls: %v", polls, err)
	}
}

// An operator reading a hold can see why the validator did not bridge it itself.
func TestProductionNativeGapHoldNamesTheRefusedEdge(t *testing.T) {
	output := &releaseDiagnosticCapture{written: make(chan struct{}, 8)}
	ctx, owner, err := newReleaseDiagnostics(t.Context(), output, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	hold := nativeGapHoldTestRefused()
	hold.edgeRefused = nativeGapEdgeSourceSlot
	err = runReleaseProductionSteeringLoopWithWait(ctx, func() error { return hold }, func() bool {
		<-output.written
		<-output.written
		return false
	}, nativeGapHoldTestProgress())
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "automatic edge refused: source_slot_differs") {
		t.Fatalf("the refusal's reason is missing: %q", output.String())
	}
}

// The rule's chain read, through the real slot decoder on a retained applied
// intent: the slot its finalized write left admits the edge, and the record ends
// the process. A slot that cannot be read holds, retries and records nothing.
func TestRecordNativeGapEdgeReadsTheRealSourceSlot(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	native.receiptNumber, fixture.production.head = 101, 102
	fixture.production.extrinsicsKVs = map[uint64][]string{101: {pending.Prepared.ExtrinsicHex}}
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("the original receipt did not reconcile: %v", err)
	}
	native.applied = true
	fixture.production.head = max(uint64(102), pending.Prepared.RevealBlock)
	var transition *productionSteeringTransition
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &transition) || transition.revealWait {
		t.Fatalf("the row did not apply: %v", err)
	}
	current, err := fixture.steerer.intents.currentV2(t.Context())
	if err != nil || current == nil || current.Status != "applied" {
		t.Fatalf("no applied intent: %v", err)
	}
	path := filepath.Join(fixture.steerer.cfg.StateDir, releaseNativeGapOwnEdgesName)
	hold := &productionNativeGapHold{lastEpoch: current.SubnetEpoch, lastStatus: current.Status, lastArtifactHash: current.MeasurementArtifactHash, nativeEpoch: current.SubnetEpoch + 2}
	var failed *productionNativeGapHold
	if err := fixture.steerer.recordNativeGapEdge(t.Context(), current, hold, fixture.production.block(current.FinalizedBlock+1)); !errors.As(err, &failed) || failed.cause == nil || failed.nativeEpoch != hold.nativeEpoch {
		t.Fatalf("a slot that cannot be read must hold and retry: %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an edge was recorded without a slot read: %v", err)
	}
	err = fixture.steerer.recordNativeGapEdge(t.Context(), current, hold, fixture.production.block(current.FinalizedBlock))
	var recorded *productionNativeGapEdgeRecorded
	if !errors.As(err, &recorded) || hold.edgeRefused != "" {
		t.Fatalf("the slot its own write left did not admit the edge: refused %q: %v", hold.edgeRefused, err)
	}
	want := ReleaseNativeGapBridgeEdge{LastNativeEpoch: current.SubnetEpoch, LastArtifactHash: current.MeasurementArtifactHash, FirstNativeEpoch: current.SubnetEpoch + 3}
	if recorded.edge != want {
		t.Fatalf("recorded %+v, want %+v", recorded.edge, want)
	}
	own, err := readReleaseNativeGapOwnEdges(fixture.steerer.cfg.StateDir)
	if err != nil || !slices.Equal(own.Edges(), []ReleaseNativeGapBridgeEdge{want}) {
		t.Fatalf("the file does not hold the edge: %v %v", own, err)
	}
	if own.ChainID != fixture.steerer.cfg.ChainID || own.DeploymentID != fixture.steerer.cfg.DeploymentID || own.ValidatorID != fixture.steerer.cfg.ValidatorID {
		t.Fatal("the file names another owner than its config, which the next start's configure refuses")
	}
}

// idle-fleet-fix's shape: an idle fleet's row refuses a whole tempo, which writes
// no intent and no fold, so the next tempo is a gap. One recorded edge covers it,
// and every further refused tempo too: it admits any epoch from its first on until
// an intent crosses, so the hold does not return and no second edge is written.
// When the fleet comes back, the next fold takes the applied predecessor's state.
func TestNativeGapOwnEdgeOutlastsRefusedTempos(t *testing.T) {
	t.Parallel()
	limits := headEMAStoreV2TestLimits()
	budget := releaseHeadV2Budget{limit: limits.MaxControlBytes}
	alpha := protocol.Rational{Numerator: 1, Denominator: 4}
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 145}
	store, err := NewHeadEMAStoreV2(t.Context(), newReleaseHeadV2TestStateDir(t), limits)
	if err != nil {
		t.Fatal(err)
	}
	L := nativeGapOwnTestLast
	if _, _, err := store.FoldForEpochV2(t.Context(), L, map[FleetScoreKey]*big.Rat{key: big.NewRat(8, 1)}, alpha); err != nil {
		t.Fatal(err)
	}
	// L+1 refused its row: nothing was written. L+2 is the gap.
	current := nativeGapOwnTestCurrent()
	if !store.refusesNativeEpochGap(L + 2) {
		t.Fatal("a refused tempo must leave a gap")
	}
	if refusal := releaseNativeGapOwnEdgeRefusal(true, current, L+2, nativeGapOwnTestApproved(26990), nativeGapOwnTestSlot()); refusal != "" {
		t.Fatalf("a refused tempo's gap was not bridged: %s", refusal)
	}
	cfg := nativeGapOwnTestConfig(t)
	edge := ReleaseNativeGapBridgeEdge{LastNativeEpoch: L, LastArtifactHash: current.MeasurementArtifactHash, FirstNativeEpoch: L + 3}
	if err := recordReleaseNativeGapOwnEdge(cfg, edge); err != nil {
		t.Fatal(err)
	}
	own, err := readReleaseNativeGapOwnEdges(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	// The fleet stays idle through L+3 to L+8: each start sees the edge pending,
	// and each epoch is admitted without another hold or edge.
	for epoch := L + 3; epoch <= L+8; epoch++ {
		owner, err := own.matchHistory(t.Context(), []*SteeringIntent{current})
		if err != nil || owner.crossed {
			t.Fatalf("epoch %d: a start refused the pending edge: %v", epoch, err)
		}
		if err := owner.requireFirstEpoch(current, epoch); err != nil {
			t.Fatalf("epoch %d idled: %v", epoch, err)
		}
		store.v2.nativeGapBridge = owner
		if store.refusesNativeEpochGap(epoch) {
			t.Fatalf("epoch %d needs another edge", epoch)
		}
		if err := store.admitReleaseHeadV2Known(t.Context(), epoch, alpha, limits.MaxEntries, budget); err != nil {
			t.Fatalf("epoch %d head admission: %v", epoch, err)
		}
	}
	// The fleet returns at L+9.
	_, records, err := store.FoldForEpochV2(t.Context(), L+9, map[FleetScoreKey]*big.Rat{key: big.NewRat(4, 1)}, alpha)
	if err != nil || len(records) != 1 || records[0].Prior.Numerator != "8" || records[0].Next.Numerator != "7" {
		t.Fatalf("the returning fleet's fold: %+v %v", records, err)
	}
	if !slices.Equal(own.Edges(), []ReleaseNativeGapBridgeEdge{edge}) {
		t.Fatalf("refused tempos wrote more edges: %v", own.Edges())
	}
}
