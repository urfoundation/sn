//go:build linux || darwin

// Explicit bridge observation preserves authenticated local application history.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type configuredIntentBridgeFixture struct {
	cfg     ReleaseConfig
	bridge  ReleaseNativeGapBridge
	path    string
	encoded []byte
	target  SteeringIntent
}

// Synthetic gaps retain every applied predecessor and a newer finalized head.
func newConfiguredIntentBridgeFixture(t *testing.T) configuredIntentBridgeFixture {
	t.Helper()
	store, path := authenticatedIntentHistoryFixture(t)
	state, err := store.readLocked()
	if err != nil {
		t.Fatal(err)
	}
	all := []SteeringIntent{state.History[0]}
	for _, epoch := range []uint64{8, 13, 14} {
		intent := testSteeringIntent(t, filepath.Dir(path), epoch, all[len(all)-1].MeasurementArtifactHash)
		intent.Schema, intent.Status = steeringIntentSchema, "applied"
		intent.CreatedAt, intent.UpdatedAt = "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"
		intent.ExtrinsicHash, intent.FinalizedBlock, intent.FinalizedBlockHash = intent.Prepared.ExtrinsicHash, 110, releaseHex32([32]byte{4})
		intent.RevealBlock, intent.Values = intent.Prepared.RevealBlock, intent.Prepared.Values
		intent.ApplicationBlock, intent.ApplicationBlockHash = intent.Prepared.RevealBlock, releaseHex32([32]byte{5})
		if epoch == 14 {
			intent.Status, intent.ApplicationBlock, intent.ApplicationBlockHash = "finalized", 0, ""
		}
		intent.VectorHash, err = intent.computeVectorHash()
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, intent)
	}
	writeIntentHistoryFixture(t, path, steeringIntentFile{Schema: steeringIntentSchema, History: all[:len(all)-1], Current: &all[len(all)-1]})
	cfg := validReleaseConfig(t)
	cfg.StateDir = filepath.Dir(path)
	bridge := ReleaseNativeGapBridge{Schema: ReleaseNativeGapBridgeEdgesSchema, ChainID: cfg.ChainID, DeploymentID: cfg.DeploymentID, ValidatorID: cfg.ValidatorID,
		edges: []ReleaseNativeGapBridgeEdge{
			{LastNativeEpoch: all[0].SubnetEpoch, LastArtifactHash: all[0].MeasurementArtifactHash, FirstNativeEpoch: all[1].SubnetEpoch},
			{LastNativeEpoch: all[1].SubnetEpoch, LastArtifactHash: all[1].MeasurementArtifactHash, FirstNativeEpoch: all[2].SubnetEpoch},
		}}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return configuredIntentBridgeFixture{cfg: cfg, bridge: bridge, path: path, encoded: encoded, target: all[2]}
}

func TestProvisionalConfiguredIntentObservationRetainsBridgedApplication(t *testing.T) {
	fixture := newConfiguredIntentBridgeFixture(t)
	if provisionalClosedNativeInputEnabled(&fixture.cfg) {
		t.Fatal("fixture unexpectedly permits unlisted provisional gaps")
	}
	if err := fixture.bridge.configure(&fixture.cfg); err != nil {
		t.Fatal(err)
	}
	result, err := observeProvisionalIntentStoreV2(t.Context(), &fixture.cfg, testIntentHotkey(t).PublicKey(), &ProvisionalIntentObservationV2{})
	if err != nil || result.State != "observed" || len(result.Receipts) != 4 || result.RecordedAppliedIntents == nil || *result.RecordedAppliedIntents != 3 || result.FinalAcceptance {
		t.Fatalf("explicit bridge lost retained application: %+v, %v", result, err)
	}
	got := result.Receipts[2]
	if got.SubnetEpoch != fixture.target.SubnetEpoch || got.VectorHash != fixture.target.VectorHash || got.Status != "applied" || got.ApplicationBlock != fixture.target.ApplicationBlock || got.ApplicationBlockHash != fixture.target.ApplicationBlockHash || result.Receipts[3].Status != "finalized" {
		t.Fatalf("historical application differs from its retained receipt: %+v", got)
	}
	after, err := os.ReadFile(fixture.path)
	if err != nil || !bytes.Equal(after, fixture.encoded) {
		t.Fatalf("observation mutated its source: %v", err)
	}
}

// Canonical public bridge files bind the observer to independently pinned bytes.
func writeConfiguredIntentBridgeFixture(t *testing.T, fixture configuredIntentBridgeFixture) (string, string) {
	t.Helper()
	wire := releaseNativeGapBridgeEdgesWire{Schema: fixture.bridge.Schema, ChainID: fixture.bridge.ChainID, DeploymentID: fixture.bridge.DeploymentID, ValidatorID: fixture.bridge.ValidatorID, Edges: fixture.bridge.edgeList()}
	encoded, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	path := filepath.Join(t.TempDir(), "native-gap-bridge.json")
	if err := os.WriteFile(path, encoded, 0o444); err != nil {
		t.Fatal(err)
	}
	return path, ReleaseMeasurementContentHash(encoded)
}

// The normal configured entry point owns the config and public bridge byte pins.
func configuredIntentBridgeOptions(t *testing.T, fixture configuredIntentBridgeFixture) ProvisionalConfiguredIntentObservationV2Options {
	t.Helper()
	configPath := writeReleaseConfig(t, fixture.cfg)
	if err := os.Chmod(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	bridgePath, bridgeHash := writeConfiguredIntentBridgeFixture(t, fixture)
	return ProvisionalConfiguredIntentObservationV2Options{
		Config:        ReleaseEvidenceV2File{Path: configPath, Bytes: uint64(len(encoded)), SHA256: fmt.Sprintf("0x%x", sha256.Sum256(encoded))},
		HandoffSHA256: ReleaseMeasurementContentHash([]byte("synthetic-owner-approved-generation")), DeploymentID: fixture.cfg.DeploymentID, StateDir: fixture.cfg.StateDir,
		ValidatorID: fixture.cfg.ValidatorID, Netuid: fixture.cfg.Netuid, Hotkey: testIntentHotkey(t).PublicKey(), NativeGapBridgePath: bridgePath, NativeGapBridgeSha256: bridgeHash,
	}
}

func TestProvisionalConfiguredIntentObservationPinsCompleteBridge(t *testing.T) {
	fixture := newConfiguredIntentBridgeFixture(t)
	options := configuredIntentBridgeOptions(t, fixture)
	result, err := ObserveProvisionalConfiguredIntentsV2(t.Context(), options)
	if err != nil || result.State != "observed" || len(result.Receipts) != 4 || result.Receipts[2].VectorHash != fixture.target.VectorHash || result.Receipts[2].ApplicationBlockHash != fixture.target.ApplicationBlockHash || result.Receipts[3].Status != "finalized" || result.Scope != "local-runtime-observation" || result.FinalAcceptance {
		t.Fatalf("pinned bridge observation lost older application: %+v, %v", result, err)
	}
	after, err := os.ReadFile(fixture.path)
	if err != nil || !bytes.Equal(after, fixture.encoded) {
		t.Fatalf("configured observation mutated its source: %v", err)
	}
}

func TestProvisionalConfiguredIntentObservationRefusesUnboundBridge(t *testing.T) {
	fixture := newConfiguredIntentBridgeFixture(t)
	options := configuredIntentBridgeOptions(t, fixture)
	for _, test := range []struct {
		name   string
		mutate func(*ProvisionalConfiguredIntentObservationV2Options)
	}{
		{name: "no bridge", mutate: func(o *ProvisionalConfiguredIntentObservationV2Options) {
			o.NativeGapBridgePath, o.NativeGapBridgeSha256 = "", ""
		}},
		{name: "missing path", mutate: func(o *ProvisionalConfiguredIntentObservationV2Options) { o.NativeGapBridgePath = "" }},
		{name: "missing pin", mutate: func(o *ProvisionalConfiguredIntentObservationV2Options) { o.NativeGapBridgeSha256 = "" }},
		{name: "wrong pin", mutate: func(o *ProvisionalConfiguredIntentObservationV2Options) {
			o.NativeGapBridgeSha256 = ReleaseMeasurementContentHash([]byte("different synthetic bridge"))
		}},
		{name: "wrong validator", mutate: func(o *ProvisionalConfiguredIntentObservationV2Options) { o.ValidatorID++ }},
		{name: "wrong hotkey", mutate: func(o *ProvisionalConfiguredIntentObservationV2Options) { o.Hotkey[0] ^= 1 }},
		{name: "relative path", mutate: func(o *ProvisionalConfiguredIntentObservationV2Options) {
			o.NativeGapBridgePath = filepath.Base(o.NativeGapBridgePath)
		}},
	} {
		changed := options
		test.mutate(&changed)
		result, err := ObserveProvisionalConfiguredIntentsV2(t.Context(), changed)
		if err == nil || result.State != "unknown" || result.RecordedAppliedIntents != nil || result.RecordedFinalizedIntents != nil || len(result.Receipts) != 0 || result.FinalAcceptance {
			t.Fatalf("%s returned observation credit: %+v, %v", test.name, result, err)
		}
	}
	after, err := os.ReadFile(fixture.path)
	if err != nil || !bytes.Equal(after, fixture.encoded) {
		t.Fatalf("refused bridge observation mutated its source: %v", err)
	}
}

func TestProvisionalConfiguredIntentObservationRefusesDifferentBridgeHistory(t *testing.T) {
	for _, mode := range []string{"scope", "first predecessor", "last predecessor", "omitted first edge", "omitted last edge", "wrong first epoch"} {
		fixture := newConfiguredIntentBridgeFixture(t)
		switch mode {
		case "scope":
			fixture.bridge.DeploymentID += "-different"
		case "first predecessor":
			fixture.bridge.edges[0].LastArtifactHash = ReleaseMeasurementContentHash([]byte("different first predecessor"))
		case "last predecessor":
			fixture.bridge.edges[1].LastArtifactHash = ReleaseMeasurementContentHash([]byte("different last predecessor"))
		case "omitted first edge":
			fixture.bridge.edges = fixture.bridge.edges[1:]
		case "omitted last edge":
			fixture.bridge.edges = fixture.bridge.edges[:1]
		case "wrong first epoch":
			fixture.bridge.edges[1].FirstNativeEpoch++
		}
		result, err := ObserveProvisionalConfiguredIntentsV2(t.Context(), configuredIntentBridgeOptions(t, fixture))
		if err == nil || result.State != "unknown" || result.RecordedAppliedIntents != nil || len(result.Receipts) != 0 || result.FinalAcceptance {
			t.Fatalf("%s authenticated a different history: %+v, %v", mode, result, err)
		}
		after, err := os.ReadFile(fixture.path)
		if err != nil || !bytes.Equal(after, fixture.encoded) {
			t.Fatalf("%s changed its refused history: %v", mode, err)
		}
	}
}

func TestProvisionalConfiguredIntentBridgeObservationRefusesPublication(t *testing.T) {
	fixture := newConfiguredIntentBridgeFixture(t)
	options := configuredIntentBridgeOptions(t, fixture)
	marker := filepath.Join(fixture.cfg.StateDir, releaseIntentV2Marker)
	encoded := []byte("synthetic unresolved publication\n")
	if err := os.WriteFile(marker, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ObserveProvisionalConfiguredIntentsV2(t.Context(), options)
	if err == nil || result.State != "unknown" || len(result.Receipts) != 0 {
		t.Fatalf("unresolved publication returned receipts: %+v, %v", result, err)
	}
	after, err := os.ReadFile(fixture.path)
	if err != nil || !bytes.Equal(after, fixture.encoded) {
		t.Fatalf("refusal changed the intent source: %v", err)
	}
	after, err = os.ReadFile(marker)
	if err != nil || !bytes.Equal(after, encoded) {
		t.Fatalf("refusal changed the publication marker: %v", err)
	}
}

func TestProvisionalConfiguredIntentBridgeObservationRefusesCancellation(t *testing.T) {
	fixture := newConfiguredIntentBridgeFixture(t)
	options := configuredIntentBridgeOptions(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := ObserveProvisionalConfiguredIntentsV2(ctx, options)
	if !errors.Is(err, context.Canceled) || result.State != "unknown" || len(result.Receipts) != 0 {
		t.Fatalf("canceled observation returned receipts: %+v, %v", result, err)
	}
	after, err := os.ReadFile(fixture.path)
	if err != nil || !bytes.Equal(after, fixture.encoded) {
		t.Fatalf("cancellation changed the intent source: %v", err)
	}
}

func TestProvisionalConfiguredIntentBridgeObservationRefusesMissingHistory(t *testing.T) {
	fixture := newConfiguredIntentBridgeFixture(t)
	options := configuredIntentBridgeOptions(t, fixture)
	if err := os.Remove(fixture.path); err != nil {
		t.Fatal(err)
	}
	result, err := ObserveProvisionalConfiguredIntentsV2(t.Context(), options)
	if err == nil || result.State != "unknown" || len(result.Receipts) != 0 || result.RecordedAppliedIntents != nil {
		t.Fatalf("missing bridge predecessors became an absent success: %+v, %v", result, err)
	}
	if _, err := os.Lstat(fixture.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("observer recreated the missing source: %v", err)
	}
}
