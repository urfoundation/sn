//go:build linux || darwin

// Restart custody controls use the real atomic intent/EMA writers. Inert
// intent values isolate local admission; they are never native replay proof.
package validator

import (
	"bytes"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/protocol"
)

// Publish the pending N file through the ordinary atomic writer after the
// synthetic local boundary. Production calls this writer only after replay.
func publishNativeRecoveryPendingTestV2(t *testing.T, f *nativeRecoveryTestV2) {
	t.Helper()
	previous := *f.intent.Current
	next := previous
	next.SubnetEpoch, next.SettlementEpoch, next.Status = 25, 15, "pending"
	next.MeasurementArtifactHash = ReleaseMeasurementContentHash([]byte("synthetic-pending-N"))
	f.intent.History, f.intent.Current = []SteeringIntent{previous}, &next
	custody := &releaseEvidenceV2StartupReferences{remaining: f.config.EvidenceV2.Bounds.MaxHistoryBytes}
	path := filepath.Join(f.config.StateDir, "steering-intents.json")
	encoded, err := custody.read(t.Context(), path, f.config.EvidenceV2.Bounds.IntentFileLimit(), false)
	if err != nil {
		t.Fatal(err)
	}
	store := &IntentStore{path: path, stateDir: f.config.StateDir, v2: &releaseIntentV2Owner{ctx: t.Context(), runtime: &releaseRuntimeV2{cfg: *f.config}}}
	err = store.writeV2(t.Context(), &releaseIntentV2Read{file: &f.intent, custody: custody, encoded: encoded})
	if err := errors.Join(err, custody.close()); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseNativeHistoryRecoveryV2DurablePendingRestart(t *testing.T) {
	f := newNativeRecoveryTestV2(t)
	publishNativeRecoveryPendingTestV2(t, f)
	if err := f.request.configure(t.Context(), f.config, f.request.ConfigPath); err != nil {
		t.Fatal("durably published pending N was rejected before ordinary semantic replay", err)
	}
	store, err := NewHeadEMAStoreV2(t.Context(), f.config.StateDir, headEMAStoreV2TestLimits())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := f.request.Adoption.matchPrefix(t.Context(), &f.intent, nil, f.config.EvidenceV2.Bounds.IntentFileLimit())
	if err != nil {
		t.Fatal(err)
	}
	store.v2.historyAdoption = owner
	alpha := protocol.Rational{Numerator: 1, Denominator: 2}
	raw := map[FleetScoreKey]*big.Rat{{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 2, UID: 7}: big.NewRat(6, 1)}
	_, records, err := store.PreviewForEpochV2(t.Context(), 25, raw, alpha)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitForEpochV2(t.Context(), 25, records, alpha); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.request.configure(t.Context(), f.config, f.request.ConfigPath); err != nil {
		t.Fatal("pending N with committed EMA was rejected before ordinary semantic replay", err)
	}
	reloaded, err := NewHeadEMAStoreV2(t.Context(), f.config.StateDir, headEMAStoreV2TestLimits())
	if err != nil {
		t.Fatal(err)
	}
	reloaded.v2.historyAdoption = owner
	if err := reloaded.CommitForEpochV2(t.Context(), 25, records, alpha); err != nil {
		t.Fatal("same-epoch retry could not retain its actual committed fold", err)
	}
	if _, _, err := reloaded.PreviewForEpochV2(t.Context(), 27, raw, alpha); err == nil {
		t.Fatal("pending restart granted a second gap")
	}
	after, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("same-epoch retry repeated or changed the fold", err)
	}
	if err := CheckReleaseNativeHistoryRecoveryV2Source(t.Context(), f.wire, ReleaseMeasurementContentHash(f.wire), true); err == nil {
		t.Fatal("progressed pending state reused capture approval")
	}
}

func TestReleaseNativeHistoryRecoveryV2InterruptedPublicationStaysUnresolved(t *testing.T) {
	f := newNativeRecoveryTestV2(t)
	publishNativeRecoveryPendingTestV2(t, f)
	store, err := NewHeadEMAStoreV2(t.Context(), f.config.StateDir, headEMAStoreV2TestLimits())
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("synthetic interruption immediately after actual native exchange")
	published := false
	_, _, err = store.runHeadEMAStoreV2(t.Context(), headEMAStoreV2FoldEpoch, 25, map[FleetScoreKey]*big.Rat{}, nil, protocol.Rational{Numerator: 1, Denominator: 2}, headEMAStoreV2RuntimeHooks{step: func(step string, _ *os.File) error {
		if step == "published" {
			published = true
			return stop
		}
		return nil
	}})
	if !published || !errors.Is(err, stop) {
		t.Fatal("test did not reach actual interrupted publication", published, err)
	}
	before := map[string][]byte{}
	for _, name := range []string{"steering-intents.json", "head-ema.json", headEMAStoreV2Marker, headEMAStoreV2Candidate} {
		before[name], err = os.ReadFile(filepath.Join(f.config.StateDir, name))
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := f.request.configure(t.Context(), f.config, f.request.ConfigPath); err == nil {
		t.Fatal("native recovery silently reconciled an uncertain publication")
	}
	if _, err := NewHeadEMAStoreV2(t.Context(), f.config.StateDir, headEMAStoreV2TestLimits()); err == nil {
		t.Fatal("ordinary loader accepted uncertain publication")
	}
	for name, want := range before {
		got, err := os.ReadFile(filepath.Join(f.config.StateDir, name))
		if err != nil || !bytes.Equal(want, got) {
			t.Fatal("refusal changed retained publication evidence", name, err)
		}
	}
}
