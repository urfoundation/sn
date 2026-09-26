//go:build linux || darwin

// Real durable folds and the live collector reproduce a lost native observation
// without inventing a submitted intent, an intervening sample or a chain receipt.
package validator

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

// A preview followed by an interrupted preparation never advances durable state.
// Every strict entry point must report the same missing native observation.
func TestHeadEmaNativeEpochGapRetainsActualObservations(t *testing.T) {
	t.Parallel()
	limits := headEMAStoreV2TestLimits()
	store, err := NewHeadEMAStoreV2(t.Context(), newReleaseHeadV2TestStateDir(t), limits)
	if err != nil {
		t.Fatal(err)
	}
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	alpha := protocol.Rational{Numerator: 1, Denominator: 2}
	if _, _, err := store.FoldForEpochV2(t.Context(), 40, map[FleetScoreKey]*big.Rat{key: big.NewRat(4, 1)}, alpha); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	raw := map[FleetScoreKey]*big.Rat{key: big.NewRat(2, 1)}
	_, records, err := store.PreviewForEpochV2(t.Context(), 41, raw, alpha)
	if err != nil {
		t.Fatal(err)
	}
	// Preparation ended before intent/ema publication. A later observation
	// cannot silently turn this speculative preview into a committed sample.
	cases := []struct {
		name string
		run  func() error
	}{
		{name: "bounded preview", run: func() error { _, _, err := store.PreviewForEpochV2(t.Context(), 42, raw, alpha); return err }},
		{name: "compatibility preview", run: func() error { _, _, err := store.PreviewForEpoch(42, raw, alpha); return err }},
		{name: "compact preview", run: func() error {
			_, _, err := store.previewForEpochV2(t.Context(), 42, raw, alpha, limits.MaxEntries, limits.MaxControlBytes)
			return err
		}},
		{name: "bounded commit", run: func() error { return store.CommitForEpochV2(t.Context(), 42, records, alpha) }},
		{name: "compatibility commit", run: func() error { return store.CommitForEpoch(42, records, alpha) }},
	}
	for _, c := range cases {
		var gap *nativeEpochGapError
		err := c.run()
		if !errors.As(err, &gap) || gap.previousEpoch != 40 || gap.currentEpoch != 42 || !strings.Contains(err.Error(), "no retained commit for native epochs 41 through 41") {
			t.Errorf("%s lost the exact missing observation: %v", c.name, err)
		}
		if RetryableEvidenceTransportError(err) {
			t.Errorf("%s classified a history gap as a transport retry", c.name)
		}
		mixed := errors.Join(err, context.DeadlineExceeded)
		if retryable, _ := classifyReleasePreparationRetry(mixed); retryable || RetryableEvidenceTransportError(mixed) {
			t.Errorf("%s let a transport deadline hide the history gap", c.name)
		}
	}
	after, err := os.ReadFile(store.path)
	finalIdentity, statErr := os.Stat(store.path)
	if err != nil || statErr != nil || !bytes.Equal(before, after) || !os.SameFile(identity, finalIdentity) || *store.lastSubnetEpoch != 40 {
		t.Fatalf("interrupted or skipped epoch advanced durable state: %v %v", err, statErr)
	}
	reloaded, err := NewHeadEMAStoreV2(t.Context(), filepath.Dir(store.path), limits)
	if err != nil {
		t.Fatal(err)
	}
	var gap *nativeEpochGapError
	if _, _, err := reloaded.PreviewForEpochV2(t.Context(), 42, raw, alpha); !errors.As(err, &gap) || gap.previousEpoch != 40 || gap.currentEpoch != 42 {
		t.Fatalf("restart lost the original native gap: %v", err)
	}
}

// Policy, transcript and regression conflicts have different causes. A gap is
// never a replacement for cancellation, file custody or same-epoch integrity.
func TestHeadEmaNativeEpochGapKeepsIntegrityFailuresDistinct(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	bad := append([]HeadEMAMeasurement(nil), fixture.records...)
	bad[0].Next = RationalJSON{Numerator: "999", Denominator: "1"}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	cases := []struct {
		name string
		run  func() error
	}{
		{name: "regression", run: func() error {
			_, _, err := store.PreviewForEpochV2(t.Context(), 9, fixture.raw, fixture.alpha)
			return err
		}},
		{name: "same epoch policy", run: func() error {
			_, _, err := store.PreviewForEpochV2(t.Context(), 10, fixture.raw, protocol.Rational{Numerator: 1, Denominator: 7})
			return err
		}},
		{name: "same epoch transcript", run: func() error { return store.CommitForEpochV2(t.Context(), 10, bad, fixture.alpha) }},
		{name: "new epoch transcript", run: func() error { return store.CommitForEpochV2(t.Context(), 11, bad, fixture.alpha) }},
		{name: "cancelled gap", run: func() error {
			_, _, err := store.PreviewForEpochV2(cancelled, 12, fixture.raw, fixture.alpha)
			return err
		}},
	}
	for _, c := range cases {
		var gap *nativeEpochGapError
		if err := c.run(); err == nil || errors.As(err, &gap) {
			t.Errorf("%s was reclassified as missing history: %v", c.name, err)
		}
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	if err := os.Rename(fixture.path, fixture.path+".held"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.path, fixture.encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	var gap *nativeEpochGapError
	if err := store.CommitForEpochV2(t.Context(), 10, fixture.records, fixture.alpha); err == nil || errors.As(err, &gap) {
		t.Fatalf("replacement custody was reclassified as missing history: %v", err)
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
}

// Lifecycle refusal takes precedence over a forward history gap. Only the
// existing explicit gap policy may allow the otherwise terminal successor.
func TestSteeringIntentNativeEpochGapRetainsLifecycleAndAuthority(t *testing.T) {
	t.Parallel()
	previous := &SteeringIntent{SubnetEpoch: 40, SettlementEpoch: 12, Status: "applied"}
	current := &SteeringIntent{SubnetEpoch: 42, SettlementEpoch: 13}
	for _, status := range []string{"applied", "failed"} {
		previous.Status = status
		var gap *nativeEpochGapError
		err := validateSteeringIntentSuccessorWithGapsV2(previous, current, false)
		if !errors.As(err, &gap) || gap.previousEpoch != 40 || gap.currentEpoch != 42 {
			t.Errorf("terminal %s did not retain the missing epoch: %v", status, err)
		}
		if err := validateSteeringIntentSuccessorWithGapsV2(previous, current, true); err != nil {
			t.Errorf("explicit %s gap authority was lost: %v", status, err)
		}
	}
	for _, status := range []string{"pending", "finalized"} {
		previous.Status = status
		var gap *nativeEpochGapError
		if err := validateSteeringIntentSuccessorWithGapsV2(previous, current, false); err == nil || errors.As(err, &gap) || !strings.Contains(err.Error(), "unfinished intent") {
			t.Errorf("unresolved %s was described as missing history: %v", status, err)
		}
	}
	previous.Status = "applied"
	for _, epoch := range []uint64{0, 39, 40} {
		current.SubnetEpoch = epoch
		var gap *nativeEpochGapError
		if err := validateSteeringIntentSuccessorWithGapsV2(previous, current, false); err == nil || errors.As(err, &gap) {
			t.Errorf("non-forward epoch %d was described as a gap: %v", epoch, err)
		}
	}
}

// The authenticated runtime's negative admission must run before signed native
// input collection. Synthetic terminal metadata is used only to demand refusal.
func TestReleaseRuntimeNativeEpochGapRefusesBeforeInputPublication(t *testing.T) {
	fixture, steerer := newReleaseStartupOwnerV2TestFixture(t)
	boundary := fixture.startup.boundary
	hash, err := parseReleaseHex32("synthetic native gap Evm hash", boundary.EVMBlockHash, false)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &ReleaseSnapshot{Epoch: new(big.Int).SetUint64(boundary.SettlementEpoch), BlockNumber: boundary.EVMBlock, BlockHash: hash}
	posts := [2]map[uint64]int{fixture.stores[0].counts(), fixture.stores[1].counts()}
	previous := &SteeringIntent{SubnetEpoch: 40, SettlementEpoch: boundary.SettlementEpoch, Status: "applied"}
	native := fixture.startup.nativeFixture
	inputs, options, err := fixture.runtime.collect(t.Context(), steerer, previous, snapshot, 42, native.blockNumber, native.block.Hex(), map[[32]byte]uint16{fixture.hotkey.PublicKey(): 2})
	var gap *nativeEpochGapError
	if !errors.As(err, &gap) || gap.previousEpoch != 40 || gap.currentEpoch != 42 || inputs != nil || !reflect.DeepEqual(options, ReleaseMeasurementV2Options{}) {
		t.Fatalf("native gap reached input collection: inputs=%d error=%v", len(inputs), err)
	}
	for _, participant := range fixture.runtime.history.participants {
		path := releaseMeasurementInputV2Path(fixture.runtime.cfg.StateDir, 42, participant.NoID)
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("gap published an unusable signed input: %v", err)
		}
	}
	for index, store := range fixture.stores {
		if !reflect.DeepEqual(posts[index], store.counts()) {
			t.Fatal("gap uploaded an unusable native cut")
		}
	}
}

// The preflight inherits only the permissions already granted to the durable
// intent owner; runtime compatibility alone is not skipped-epoch authority.
func TestReleaseNativeEpochPreflightKeepsExistingGapAuthority(t *testing.T) {
	t.Parallel()
	previous := &SteeringIntent{SubnetEpoch: 40, SettlementEpoch: 12, Status: "applied", MeasurementArtifactHash: ReleaseMeasurementContentHash([]byte("synthetic prior artifact"))}
	runtime := &releaseRuntimeV2{cfg: ReleaseConfig{ChainID: 945, ProvisionalRuntimeCompatibility: crv4.ProvisionalRuntimeCompatibilityProfile}, history: &releaseEvidenceV2StartupHistory{}}
	runtime.cfg.Policy.NetworkProfile = "testnet"
	var gap *nativeEpochGapError
	if err := runtime.admitNativeIntentSuccessorV2(previous, 42, 13); !errors.As(err, &gap) {
		t.Fatalf("runtime compatibility granted skipped-epoch authority: %v", err)
	}
	runtime.cfg.ProvisionalDeferClosedNativeInput = true
	if err := runtime.admitNativeIntentSuccessorV2(previous, 42, 13); !errors.As(err, &gap) {
		t.Fatalf("fresh deferral granted retained-history authority: %v", err)
	}
	runtime.history.retainedStartup = true
	if err := runtime.admitNativeIntentSuccessorV2(previous, 42, 13); err != nil {
		t.Fatalf("existing retained provisional authority was lost: %v", err)
	}
	runtime.history.retainedStartup = false
	runtime.cfg.ProvisionalDeferClosedNativeInput = false
	runtime.history.historyAdoption = &releaseHistoryAdoptionV2{request: ReleaseHistoryAdoptionV2{LastNativeEpoch: 40, FirstNativeEpoch: 42, LastArtifactHash: previous.MeasurementArtifactHash}}
	if err := runtime.admitNativeIntentSuccessorV2(previous, 42, 13); err != nil {
		t.Fatalf("approved adoption edge was lost: %v", err)
	}
	if err := runtime.admitNativeIntentSuccessorV2(previous, 43, 13); !errors.As(err, &gap) {
		t.Fatalf("adoption edge widened to another epoch: %v", err)
	}
	previous.SubnetEpoch = 42
	if err := runtime.admitNativeIntentSuccessorV2(previous, 43, 13); err != nil {
		t.Fatalf("consecutive native successor was refused: %v", err)
	}
	if err := runtime.admitNativeIntentSuccessorV2(previous, 44, 13); !errors.As(err, &gap) {
		t.Fatalf("consumed adoption edge admitted another gap: %v", err)
	}
}

// Legacy stores keep their rejection and report the same range, including the
// last representable native epoch. Regression never wraps into a forward gap.
func TestHeadEmaNativeEpochGapLegacyRangeDoesNotOverflow(t *testing.T) {
	t.Parallel()
	alpha := protocol.Rational{Numerator: 1, Denominator: 2}
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	raw := map[FleetScoreKey]*big.Rat{key: big.NewRat(4, 1)}
	for _, epoch := range []uint64{0, ^uint64(0) - 2} {
		store, err := NewHeadEMAStore(newReleaseHeadV2TestStateDir(t))
		if err != nil {
			t.Fatal(err)
		}
		_, records, err := store.FoldForEpoch(epoch, raw, alpha)
		if err != nil {
			t.Fatal(err)
		}
		var gap *nativeEpochGapError
		if _, _, err := store.PreviewForEpoch(epoch+2, raw, alpha); !errors.As(err, &gap) || gap.previousEpoch != epoch || gap.currentEpoch != epoch+2 {
			t.Errorf("legacy preview lost range from %d: %v", epoch, err)
		}
		if err := store.CommitForEpoch(epoch+2, records, alpha); !errors.As(err, &gap) || gap.previousEpoch != epoch || gap.currentEpoch != epoch+2 {
			t.Errorf("legacy commit lost range from %d: %v", epoch, err)
		}
		if _, _, err := store.FoldForEpoch(^uint64(0), raw, alpha); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.PreviewForEpoch(0, raw, alpha); err == nil || errors.As(err, &gap) {
			t.Errorf("legacy terminal epoch wrapped into a gap: %v", err)
		}
	}
}
