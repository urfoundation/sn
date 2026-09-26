//go:build linux || darwin

// Recovery tests use synthetic local prefix values and real private files.
// Native runtime tests use the existing independently authenticated RPC fixture.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"gopkg.in/yaml.v3"
)

// nativeRecoveryTestV2 owns an inert applied prefix and a genuine rational EMA
// fold. The prefix is never presented as an authenticated native observation.
type nativeRecoveryTestV2 struct {
	request *ReleaseNativeHistoryRecoveryV2
	config  *ReleaseConfig
	wire    []byte
	intent  steeringIntentFile
}

// newNativeRecoveryTestV2 selects a config in the source-role overlay and a
// distinct existing generation-2 coordinator namespace, reproducing V2 custody.
func newNativeRecoveryTestV2(t *testing.T) *nativeRecoveryTestV2 {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := validReleaseConfig(t)
	cfg.StateDir = filepath.Join(root, "runtime", "validator-1", "evidence-generations", "generation-00000000000000000002", "coordinator-state-v2")
	cfg.ProvisionalRuntimeCompatibility = crv4.ProvisionalRuntimeCompatibilityProfile
	path := filepath.Join(root, "policy-rollover", "source-role", "generation-00000000000000000002", "validator-1", "validator.yml")
	for _, dir := range []string{cfg.StateDir, filepath.Dir(path)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	config, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, config, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewHeadEMAStoreV2(t.Context(), cfg.StateDir, headEMAStoreV2TestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.FoldForEpochV2(t.Context(), 20, map[FleetScoreKey]*big.Rat{{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 2, UID: 7}: big.NewRat(4, 1)}, protocol.Rational{Numerator: 1, Denominator: 2}); err != nil {
		t.Fatal(err)
	}
	intent := steeringIntentFile{Schema: steeringIntentSchema, Current: &SteeringIntent{SubnetEpoch: 20, SettlementEpoch: 10, Status: "applied", MeasurementArtifactHash: ReleaseMeasurementContentHash([]byte("synthetic-retained-measurement")), Prepared: &crv4.PreparedSubmission{SourceCommitment: &crv4.PreparedSourceCommitment{Hash: "inert-local-prefix-test"}}}}
	intents, err := marshalAttemptSettlementV2JSON(t.Context(), &intent, cfg.EvidenceV2.Bounds.IntentFileLimit(), true, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.StateDir, "steering-intents.json"), intents, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: crv4.ReviewedRuntimeSpecVersion + 1, TransactionVersion: 1, StateVersion: 1}, CodeHash: "0x" + strings.Repeat("31", 32), MetadataHash: "0x" + strings.Repeat("32", 32)}
	wire, err := CaptureReleaseNativeHistoryRecoveryV2(t.Context(), root, path, config, "0x"+strings.Repeat("11", 32), "0x"+strings.Repeat("22", 32), 25, runtime)
	if err != nil {
		t.Fatal(err)
	}
	request, err := DecodeReleaseNativeHistoryRecoveryV2(wire, ReleaseMeasurementContentHash(wire))
	if err != nil {
		t.Fatal(err)
	}
	return &nativeRecoveryTestV2{request: request, config: &cfg, wire: wire, intent: intent}
}

// TestReleaseNativeHistoryRecoveryV2PreservesOverlayAndOneEdge exercises the
// new admission while retaining all existing strict gap and final-archive gates.
func TestReleaseNativeHistoryRecoveryV2PreservesOverlayAndOneEdge(t *testing.T) {
	t.Parallel()
	f := newNativeRecoveryTestV2(t)
	before, err := os.ReadFile(f.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.request.configure(t.Context(), f.config, f.request.ConfigPath); err != nil {
		t.Fatal(err)
	}
	if f.config.historyAdoptionV2 == nil || f.config.nativeHistoryRecoveryV2 == nil || f.config.ProvisionalDeferClosedNativeInput {
		t.Fatal("recovery lost its exact owner or enabled general deferral")
	}
	if err := f.config.ValidateHistorical(); err == nil {
		t.Fatal("provisional recovery entered final historical admission")
	}
	owner, err := f.config.historyAdoptionV2.matchPrefix(t.Context(), &f.intent, nil, f.config.EvidenceV2.Bounds.IntentFileLimit())
	if err != nil {
		t.Fatal(err)
	}
	for _, epoch := range []uint64{21, 24, 26, 40} {
		if owner.allowsIntentEdge(f.intent.Current, &SteeringIntent{SubnetEpoch: epoch}) || owner.requireFirstEpoch(f.intent.Current, epoch) == nil {
			t.Fatalf("recovery admitted unapproved first native epoch %d", epoch)
		}
	}
	if !owner.allowsIntentEdge(f.intent.Current, &SteeringIntent{SubnetEpoch: 25}) {
		t.Fatal("exact authorized edge was lost")
	}
	strict := f.request.Adoption
	if err := strict.configure(f.config, f.request.ConfigPath); err == nil {
		t.Fatal("existing strict adoption inherited provisional recovery authority")
	}
	f.request.Adoption.FirstNativeEpoch++
	if f.config.historyAdoptionV2.FirstNativeEpoch != 25 {
		t.Fatal("borrowed recovery request mutated admitted authority")
	}
	after, err := os.ReadFile(f.request.ConfigPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("recovery rewrote the selected config", err)
	}
}

// TestReleaseNativeHistoryRecoveryV2RejectsChangedScope checks rehashed foreign
// data as well as exact-byte tampering; decoder admission alone is insufficient.
func TestReleaseNativeHistoryRecoveryV2RejectsChangedScope(t *testing.T) {
	t.Parallel()
	f := newNativeRecoveryTestV2(t)
	for _, change := range []func(*ReleaseNativeHistoryRecoveryV2){
		func(r *ReleaseNativeHistoryRecoveryV2) { r.Generation++ },
		func(r *ReleaseNativeHistoryRecoveryV2) { r.Provisional = false },
		func(r *ReleaseNativeHistoryRecoveryV2) { r.FinalAcceptance = true },
		func(r *ReleaseNativeHistoryRecoveryV2) { r.CompatibilityProfile = "other" },
		func(r *ReleaseNativeHistoryRecoveryV2) { r.Runtime.Version.TransactionVersion++ },
		func(r *ReleaseNativeHistoryRecoveryV2) { r.Adoption.FirstNativeEpoch = r.Adoption.LastNativeEpoch + 1 },
		func(r *ReleaseNativeHistoryRecoveryV2) { r.Adoption.CoordinatorStateDir += "-other" },
	} {
		changed := *f.request
		change(&changed)
		raw, _ := json.MarshalIndent(changed, "", "  ")
		raw = append(raw, '\n')
		if _, err := DecodeReleaseNativeHistoryRecoveryV2(raw, ReleaseMeasurementContentHash(raw)); err == nil {
			t.Fatalf("rehashed foreign recovery scope admitted: %+v", changed)
		}
	}
	changed := append(bytes.Clone(f.wire), ' ')
	if _, err := DecodeReleaseNativeHistoryRecoveryV2(changed, ReleaseMeasurementContentHash(changed)); err == nil {
		t.Fatal("noncanonical recovery accepted")
	}
	if _, err := DecodeReleaseNativeHistoryRecoveryV2(changed, ReleaseMeasurementContentHash(f.wire)); err == nil {
		t.Fatal("changed bytes retained original approval")
	}
}

// TestReleaseNativeHistoryRecoveryV2RejectsChangedSource tests file custody,
// publication ambiguity, exact compact EMA bytes and a pending original intent.
func TestReleaseNativeHistoryRecoveryV2RejectsChangedSource(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"config", "ema", "pending", "mode", releaseIntentV2Marker, releaseIntentV2Candidate, headEMAStoreV2Marker, headEMAStoreV2Candidate} {
		t.Run(name, func(t *testing.T) {
			f := newNativeRecoveryTestV2(t)
			var err error
			switch name {
			case "config":
				err = os.WriteFile(f.request.ConfigPath, []byte("changed"), 0o600)
			case "ema":
				err = os.WriteFile(filepath.Join(f.config.StateDir, "head-ema.json"), []byte("{}"), 0o600)
			case "pending":
				f.intent.Current.Status = "pending"
				raw, encodeErr := marshalAttemptSettlementV2JSON(t.Context(), &f.intent, f.config.EvidenceV2.Bounds.IntentFileLimit(), true, true)
				if encodeErr != nil {
					t.Fatal(encodeErr)
				}
				err = os.WriteFile(filepath.Join(f.config.StateDir, "steering-intents.json"), raw, 0o600)
			case "mode":
				err = os.Chmod(f.config.StateDir, 0o755)
			default:
				err = os.WriteFile(filepath.Join(f.config.StateDir, name), nil, 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := CheckReleaseNativeHistoryRecoveryV2Source(t.Context(), f.wire, ReleaseMeasurementContentHash(f.wire), false); err == nil {
				t.Fatal("changed native recovery source accepted")
			}
		})
	}
}

// TestReleaseNativeHistoryRecoveryV2PinsActualRuntime rejects another compatible
// successor artifact before any native signing or subscription can happen.
func TestReleaseNativeHistoryRecoveryV2PinsActualRuntime(t *testing.T) {
	fixture := newProvisionalValidatorRuntimeFixture(t)
	if err := enableReleaseProvisionalRuntimeCompatibility(fixture.chain, &fixture.cfg); err != nil {
		t.Fatal(err)
	}
	runtime := crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: fixture.version, TransactionVersion: 1, StateVersion: 1}, CodeHash: fixture.code, MetadataHash: "0xb0fae6d022b74faf948e3b98463b98b46c4738e87348e24340f91146ededa4bf"}
	fixture.cfg.nativeHistoryRecoveryV2 = &ReleaseNativeHistoryRecoveryV2{Runtime: runtime, CompatibilityProfile: crv4.ProvisionalRuntimeCompatibilityProfile}
	if _, err := authenticatePinnedNativeRuntimeContext(t.Context(), fixture.chain, &fixture.cfg); err != nil {
		t.Fatal(err)
	}
	fixture.cfg.nativeHistoryRecoveryV2.Runtime.CodeHash = "0x" + strings.Repeat("11", 32)
	if _, err := authenticatePinnedNativeRuntimeContext(t.Context(), fixture.chain, &fixture.cfg); err == nil {
		t.Fatal("compatible unapproved runtime passed recovery pin")
	}
	if fixture.submissions != 0 {
		t.Fatal("runtime rejection reached signing transport")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := authenticatePinnedNativeRuntimeContext(cancelled, fixture.chain, &fixture.cfg); err == nil {
		t.Fatal("recovery runtime ignored cancellation")
	}
}

// TestReleaseNativeHistoryRecoveryV2RetainsAppendOnlyRestart verifies the local
// admission after a real-epoch append without claiming native authentication of
// these inert fixture intents. Normal startup still owns the appended replay.
func TestReleaseNativeHistoryRecoveryV2RetainsAppendOnlyRestart(t *testing.T) {
	t.Parallel()
	f := newNativeRecoveryTestV2(t)
	previous := *f.intent.Current
	next := previous
	next.SubnetEpoch, next.SettlementEpoch = 25, 15
	next.MeasurementArtifactHash = ReleaseMeasurementContentHash([]byte("synthetic-first-real-epoch"))
	f.intent.History, f.intent.Current = []SteeringIntent{previous}, &next
	raw, err := marshalAttemptSettlementV2JSON(t.Context(), &f.intent, f.config.EvidenceV2.Bounds.IntentFileLimit(), true, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.config.StateDir, "steering-intents.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckReleaseNativeHistoryRecoveryV2Source(t.Context(), f.wire, ReleaseMeasurementContentHash(f.wire), false); err != nil {
		t.Fatal("append-only restart lost original source pin", err)
	}
	if err := CheckReleaseNativeHistoryRecoveryV2Source(t.Context(), f.wire, ReleaseMeasurementContentHash(f.wire), true); err == nil {
		t.Fatal("a new capture/apply reused a changed full prefix")
	}
	owner, err := f.request.Adoption.matchPrefix(t.Context(), &f.intent, raw, f.config.EvidenceV2.Bounds.IntentFileLimit())
	if err != nil {
		t.Fatal(err)
	}
	if owner.allowsIntentEdge(&next, &SteeringIntent{SubnetEpoch: 27}) {
		t.Fatal("restart granted a second missing native edge")
	}
	f.intent.History[0].ApplicationBlock++
	raw, err = marshalAttemptSettlementV2JSON(t.Context(), &f.intent, f.config.EvidenceV2.Bounds.IntentFileLimit(), true, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.config.StateDir, "steering-intents.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckReleaseNativeHistoryRecoveryV2Source(t.Context(), f.wire, ReleaseMeasurementContentHash(f.wire), false); err == nil {
		t.Fatal("restart blessed a changed original application receipt")
	}
}
