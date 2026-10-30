//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

type nativeBridgeObservationTestFixture struct {
	cfg     ReleaseConfig
	bridge  ReleaseNativeGapBridge
	current SteeringIntent
	files   map[string][]byte
}

func newNativeBridgeObservationTestFixture(t *testing.T) nativeBridgeObservationTestFixture {
	t.Helper()
	state := newReleaseHeadV2TestStateDir(t)
	cfg := ReleaseConfig{StateDir: state, DeploymentID: "native-bridge-projection-test", ChainID: 945, ValidatorID: 1, Netuid: 7,
		EvidenceV2: ReleaseEvidenceV2Config{Bounds: ReleaseEvidenceV2Bounds{MaxControlBytes: 1024 * 1024, MaxHistoryBytes: 8 * 1024 * 1024, MaxArtifactBytes: 1024 * 1024, MaxOperators: 1}}}
	all := make([]*SteeringIntent, 0, 3)
	for _, epoch := range []uint64{10, 20, 21} {
		artifact := ReleaseMeasurementArtifact{Schema: ReleaseMeasurementSchemaV2, DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, ValidatorID: cfg.ValidatorID, Netuid: cfg.Netuid, SubnetEpoch: epoch, SettlementEpoch: 1,
			PolicyHash: releaseHex32([32]byte{7}), Policy: protocol.Policy{}}
		if len(all) > 0 {
			artifact.PreviousArtifactHash = all[len(all)-1].MeasurementArtifactHash
		}
		raw, err := canonicalReleaseMeasurementBytes(&artifact)
		if err != nil {
			t.Fatal(err)
		}
		hash := ReleaseMeasurementContentHash(raw)
		name, size, err := persistReleaseMeasurementArtifact(state, raw, hash)
		if err != nil {
			t.Fatal(err)
		}
		prepared := testPreparedSubmission(t, epoch, []uint16{2, 9})
		intent := &SteeringIntent{Schema: steeringIntentSchema, ValidatorID: cfg.ValidatorID, Netuid: cfg.Netuid, SubnetEpoch: epoch, SettlementEpoch: 1, PolicyHash: artifact.PolicyHash,
			MeasurementArtifactHash: hash, MeasurementArtifactPath: name, MeasurementArtifactSize: size, Prepared: prepared,
			Status: "applied", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z", ExtrinsicHash: prepared.ExtrinsicHash, FinalizedBlock: 110, FinalizedBlockHash: releaseHex32([32]byte{4}), RevealBlock: prepared.RevealBlock, Values: prepared.Values, ApplicationBlock: 120, ApplicationBlockHash: releaseHex32([32]byte{5})}
		intent.VectorHash, err = intent.computeVectorHash()
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, intent)
	}
	writeIntentHistoryFixture(t, filepath.Join(state, "steering-intents.json"), steeringIntentFile{Schema: steeringIntentSchema, History: []SteeringIntent{*all[0], *all[1]}, Current: all[2]})
	fixture := nativeBridgeObservationTestFixture{cfg: cfg, bridge: ReleaseNativeGapBridge{Schema: ReleaseNativeGapBridgeEdgesSchema, ChainID: cfg.ChainID, DeploymentID: cfg.DeploymentID, ValidatorID: cfg.ValidatorID,
		edges: []ReleaseNativeGapBridgeEdge{{LastNativeEpoch: 10, LastArtifactHash: all[0].MeasurementArtifactHash, FirstNativeEpoch: 20}}}, current: *all[2], files: map[string][]byte{}}
	if err := filepath.WalkDir(state, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fixture.files[path] = raw
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (self nativeBridgeObservationTestFixture) unchanged(t *testing.T) {
	t.Helper()
	seen := map[string][]byte{}
	if err := filepath.WalkDir(self.cfg.StateDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		seen[path] = raw
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, self.files) {
		t.Fatal("read-only predecessor observation changed or created state")
	}
}

func TestNativeBridgePredecessorObservationBindsAppliedPublicArtifact(t *testing.T) {
	t.Parallel()
	fixture := newNativeBridgeObservationTestFixture(t)
	got, err := ObserveReleaseNativeGapBridgePredecessorV2(t.Context(), &fixture.cfg, &fixture.bridge, fixture.current.SubnetEpoch, fixture.current.MeasurementArtifactHash, fixture.current.MeasurementArtifactSize)
	if err != nil || got == nil || got.NativeEpoch != 21 || got.Status != "applied" || got.ArtifactHash != fixture.current.MeasurementArtifactHash || got.VectorHash != fixture.current.VectorHash || !got.ExistingBridgeMatches || got.RetainedIntents != 3 {
		t.Fatalf("normal bridge-aware predecessor projection failed: %v, %+v", err, got)
	}
	fixture.unchanged(t)
}

func TestNativeBridgePredecessorObservationRefusesWrongAuthority(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"nil bridge", "invalid bridge", "bridge scope", "bridge", "epoch", "hash", "bytes", "identity", "publication marker", "public bytes", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newNativeBridgeObservationTestFixture(t)
			epoch, hash, size := fixture.current.SubnetEpoch, fixture.current.MeasurementArtifactHash, fixture.current.MeasurementArtifactSize
			bridge := &fixture.bridge
			switch mode {
			case "nil bridge":
				bridge = nil
			case "invalid bridge":
				fixture.bridge.Schema = "unsupported-test-schema"
			case "bridge scope":
				fixture.bridge.ChainID++
			case "bridge":
				fixture.bridge.edges[0].LastArtifactHash = ReleaseMeasurementContentHash([]byte("wrong predecessor"))
			case "epoch":
				epoch++
			case "hash":
				hash = ReleaseMeasurementContentHash([]byte("wrong public artifact"))
			case "bytes":
				size++
			case "identity":
				fixture.cfg.DeploymentID = "different-test-deployment"
			case "publication marker":
				path := filepath.Join(fixture.cfg.StateDir, releaseIntentV2Marker)
				if err := os.WriteFile(path, []byte("unresolved synthetic publication\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				fixture.files[path] = []byte("unresolved synthetic publication\n")
			case "public bytes":
				path := filepath.Join(fixture.cfg.StateDir, filepath.FromSlash(fixture.current.MeasurementArtifactPath))
				raw := bytes.Clone(fixture.files[path])
				raw[0] = '['
				if err := os.WriteFile(path, raw, 0o600); err != nil {
					t.Fatal(err)
				}
				fixture.files[path] = raw
			case "symlink":
				path := filepath.Join(fixture.cfg.StateDir, "steering-intents.json")
				original := fixture.files[path]
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".original", path); err != nil {
					t.Fatal(err)
				}
				fixture.files[path+".original"] = original
			}
			got, err := ObserveReleaseNativeGapBridgePredecessorV2(t.Context(), &fixture.cfg, bridge, epoch, hash, size)
			if err == nil || got != nil {
				t.Fatalf("%s returned a usable predecessor: %+v, %v", mode, got, err)
			}
			fixture.unchanged(t)
		})
	}
}

func TestNativeBridgePredecessorObservationLateCloseReturnsNoReceipt(t *testing.T) {
	t.Parallel()
	fixture := newNativeBridgeObservationTestFixture(t)
	want := errors.New("synthetic late descriptor close failure")
	hooks := releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
		if filepath.Base(file.Name()) == "steering-intents.json" {
			return want
		}
		return nil
	}}
	got, err := observeReleaseNativeGapBridgePredecessorV2(t.Context(), &fixture.cfg, &fixture.bridge, fixture.current.SubnetEpoch, fixture.current.MeasurementArtifactHash, fixture.current.MeasurementArtifactSize, hooks)
	if !errors.Is(err, want) || got != nil {
		t.Fatalf("late descriptor close published a receipt: %+v, %v", got, err)
	}
	fixture.unchanged(t)
}

func TestNativeBridgePredecessorObservationLateCancellationReturnsNoReceipt(t *testing.T) {
	t.Parallel()
	fixture := newNativeBridgeObservationTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	hooks := releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
		if filepath.Base(file.Name()) == "steering-intents.json" {
			cancel()
		}
		return nil
	}}
	got, err := observeReleaseNativeGapBridgePredecessorV2(ctx, &fixture.cfg, &fixture.bridge, fixture.current.SubnetEpoch, fixture.current.MeasurementArtifactHash, fixture.current.MeasurementArtifactSize, hooks)
	if !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("late cancellation published a receipt: %+v, %v", got, err)
	}
	fixture.unchanged(t)
}

func TestNativeBridgePredecessorObservationCancellationReturnsNoReceipt(t *testing.T) {
	t.Parallel()
	fixture := newNativeBridgeObservationTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := ObserveReleaseNativeGapBridgePredecessorV2(ctx, &fixture.cfg, &fixture.bridge, fixture.current.SubnetEpoch, fixture.current.MeasurementArtifactHash, fixture.current.MeasurementArtifactSize)
	if err == nil || got != nil {
		t.Fatalf("cancelled predecessor observation returned a receipt: %+v, %v", got, err)
	}
	fixture.unchanged(t)
}
