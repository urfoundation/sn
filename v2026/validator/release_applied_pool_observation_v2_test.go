//go:build linux || darwin

// Synthetic retained receipts exercise normal custody, not a mirror decoder.
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
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Existing constructors create only generated test submissions and a bounded
// synthetic artifact. No production identity or private source is a fixture.
type appliedPoolObservationFixture struct {
	options ProvisionalConfiguredIntentObservationV2Options
	query   AppliedPoolObservationV2Query
	intent  SteeringIntent
	files   map[string][]byte
}

func newAppliedPoolObservationFixture(t *testing.T) appliedPoolObservationFixture {
	t.Helper()
	cfg := validReleaseConfig(t)
	cfg.DeploymentID = "synthetic-applied-pool"
	cfg.StateDir = newReleaseHeadV2TestStateDir(t)
	policyHash, err := cfg.Policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	prepared := testPreparedSubmission(t, 12, []uint16{2, 9})
	poolHotkey := [32]byte{0x52}
	artifact := ReleaseMeasurementArtifact{Schema: ReleaseMeasurementSchemaV2, DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID,
		GenesisHash: cfg.GenesisHash, Coordinator: cfg.Coordinator, SettlementVault: cfg.SettlementVault, ValidatorID: cfg.ValidatorID, Netuid: cfg.Netuid,
		SubnetEpoch: 12, SettlementEpoch: 4, PolicyHash: releaseHex32(policyHash), Policy: cfg.Policy, NativeSnapshotBlock: 100,
		NativeSnapshotHash: releaseHex32([32]byte{0x13}), EVMSnapshotBlock: 98, EVMSnapshotHash: releaseHex32([32]byte{0x14}), SelfUID: 5,
		Inputs: []ReleaseMeasurementInput{}, Bindings: []ReleaseBindingMeasurement{}, HeadEMA: []HeadEMAMeasurement{},
		Pools: []ReleasePoolMeasurement{{NoID: 1, UID: 2, PoolHotkey: releaseHex32(poolHotkey)}}, DepositAudits: []DepositAudit{}, ControlledNOIDs: []uint64{}}
	raw, err := canonicalReleaseMeasurementBytes(&artifact)
	if err != nil {
		t.Fatal(err)
	}
	hash := ReleaseMeasurementContentHash(raw)
	name, size, err := persistReleaseMeasurementArtifact(cfg.StateDir, raw, hash)
	if err != nil {
		t.Fatal(err)
	}
	intent := SteeringIntent{Schema: steeringIntentSchema, ValidatorID: cfg.ValidatorID, Netuid: cfg.Netuid, SubnetEpoch: artifact.SubnetEpoch,
		NativeSnapshotBlock: artifact.NativeSnapshotBlock, NativeSnapshotHash: artifact.NativeSnapshotHash, EVMSnapshotBlock: artifact.EVMSnapshotBlock,
		EVMSnapshotHash: artifact.EVMSnapshotHash, SettlementEpoch: artifact.SettlementEpoch, PolicyHash: artifact.PolicyHash,
		MeasurementArtifactPath: name, MeasurementArtifactHash: hash, MeasurementArtifactSize: size, SelfUID: artifact.SelfUID,
		UIDs: []uint16{2, 9}, Prepared: prepared, Values: prepared.Values, Status: "applied", CreatedAt: "2030-01-01T00:00:00Z", UpdatedAt: "2030-01-01T00:00:00Z",
		ExtrinsicHash: prepared.ExtrinsicHash, FinalizedBlock: 110, FinalizedBlockHash: releaseHex32([32]byte{0x15}), RevealBlock: prepared.RevealBlock,
		ApplicationBlock: 121, ApplicationBlockHash: releaseHex32([32]byte{0x16})}
	intent.VectorHash, err = intent.computeVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	writeIntentHistoryFixture(t, filepath.Join(cfg.StateDir, "steering-intents.json"), steeringIntentFile{Schema: steeringIntentSchema, Current: &intent, History: []SteeringIntent{}})
	configPath := writeReleaseConfig(t, cfg)
	if err := os.Chmod(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture := appliedPoolObservationFixture{options: ProvisionalConfiguredIntentObservationV2Options{
		Config:        ReleaseEvidenceV2File{Path: configPath, Bytes: uint64(len(configBytes)), SHA256: fmt.Sprintf("0x%x", sha256.Sum256(configBytes))},
		HandoffSHA256: ReleaseMeasurementContentHash([]byte("synthetic-public-approval")), DeploymentID: cfg.DeploymentID, StateDir: cfg.StateDir,
		ValidatorID: cfg.ValidatorID, Netuid: cfg.Netuid, Hotkey: testIntentHotkey(t).PublicKey()},
		query: AppliedPoolObservationV2Query{NativeEpoch: intent.SubnetEpoch, MinimumSettlementEpoch: 4, MeasurementHash: hash, MeasurementBytes: size,
			VectorHash: intent.VectorHash, PoolNoId: 1, PoolHotkey: poolHotkey}, intent: intent, files: map[string][]byte{}}
	if err := filepath.WalkDir(cfg.StateDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		fixture.files[path], err = os.ReadFile(path)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestAppliedPoolObservationProjectsOnlyPublicVector(t *testing.T) {
	fixture := newAppliedPoolObservationFixture(t)
	got, err := ObserveConfiguredAppliedPoolV2(t.Context(), fixture.options, fixture.query)
	if err != nil || got == nil || got.Receipt.Status != "applied" || got.Receipt.SettlementEpoch != 4 || got.DecisionPoolUid != 2 || got.PoolHotkey != releaseHex32(fixture.query.PoolHotkey) || !reflect.DeepEqual(got.Uids, fixture.intent.UIDs) || !reflect.DeepEqual(got.Values, fixture.intent.Values) || got.SourceCompletedAt.Before(got.SourceStartedAt) || !got.NormalHistoryVerified || !got.MeasurementContextVerified || got.NativeChainAcceptance || got.FullMathematicalReplayPerformed || got.PrivateFieldsEmitted || got.SignersConstructed || got.StateWrites {
		t.Fatalf("normal applied vector projection: %+v %v", got, err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"payload_hex", "ciphertext", "extrinsic_hex", "account_nonce", `"prepared":`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("private field %s reached public projection", forbidden)
		}
	}
	for path, before := range fixture.files {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("read-only projection changed synthetic source: %v", err)
		}
	}
}

func TestAppliedPoolObservationRefusesWrongIndependentSelection(t *testing.T) {
	for _, mode := range []string{"epoch", "settlement", "hash", "bytes", "vector", "operator", "hotkey"} {
		fixture := newAppliedPoolObservationFixture(t)
		query := fixture.query
		switch mode {
		case "epoch":
			query.NativeEpoch++
		case "settlement":
			query.MinimumSettlementEpoch++
		case "hash":
			query.MeasurementHash = ReleaseMeasurementContentHash([]byte("synthetic-foreign-artifact"))
		case "bytes":
			query.MeasurementBytes++
		case "vector":
			query.VectorHash = releaseHex32([32]byte{0x71})
		case "operator":
			query.PoolNoId++
		case "hotkey":
			query.PoolHotkey[0]++
		}
		got, err := ObserveConfiguredAppliedPoolV2(t.Context(), fixture.options, query)
		if err == nil || got != nil {
			t.Fatalf("%s returned an unbound vector: %+v %v", mode, got, err)
		}
	}
}

func TestAppliedPoolObservationRefusesSourceMutationAndAliases(t *testing.T) {
	for _, mode := range []string{"values", "vector", "measurement-bytes", "measurement-alias", "publication"} {
		fixture := newAppliedPoolObservationFixture(t)
		path := filepath.Join(fixture.options.StateDir, "steering-intents.json")
		switch mode {
		case "values", "vector":
			intent := fixture.intent
			intent.Values = slices.Clone(intent.Values)
			if mode == "values" {
				intent.Values[0]++
			} else {
				intent.VectorHash = releaseHex32([32]byte{0x72})
			}
			writeIntentHistoryFixture(t, path, steeringIntentFile{Schema: steeringIntentSchema, Current: &intent, History: []SteeringIntent{}})
		case "measurement-bytes":
			path = filepath.Join(fixture.options.StateDir, filepath.FromSlash(fixture.intent.MeasurementArtifactPath))
			if err := os.WriteFile(path, []byte("synthetic-corrupt-public-measurement"), 0o600); err != nil {
				t.Fatal(err)
			}
		case "measurement-alias":
			path = filepath.Join(fixture.options.StateDir, filepath.FromSlash(fixture.intent.MeasurementArtifactPath))
			if err := os.Rename(path, path+".original"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path+".original", path); err != nil {
				t.Fatal(err)
			}
		case "publication":
			if err := os.WriteFile(filepath.Join(fixture.options.StateDir, releaseIntentV2Marker), []byte("synthetic-unresolved"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		got, err := ObserveConfiguredAppliedPoolV2(t.Context(), fixture.options, fixture.query)
		if err == nil || got != nil {
			t.Fatalf("%s returned source mutation as a vector: %+v %v", mode, got, err)
		}
	}
}

func TestAppliedPoolObservationSuppressesLateCloseAndCancellation(t *testing.T) {
	for _, mode := range []string{"close", "cancel"} {
		fixture := newAppliedPoolObservationFixture(t)
		ctx, cancel := context.WithCancel(t.Context())
		want := errors.New("synthetic-late-close")
		hooks := releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
			if filepath.Base(file.Name()) == "steering-intents.json" {
				if mode == "cancel" {
					cancel()
					return nil
				}
				return want
			}
			return nil
		}}
		got, err := observeConfiguredAppliedPoolV2(ctx, fixture.options, fixture.query, hooks)
		cancel()
		if err == nil || got != nil || mode == "close" && !errors.Is(err, want) || mode == "cancel" && !errors.Is(err, context.Canceled) {
			t.Fatalf("%s published a false complete vector: %+v %v", mode, got, err)
		}
	}
}

func TestAppliedPoolReferenceDiscoveryCannotReplaceIndependentVectorPin(t *testing.T) {
	fixture := newAppliedPoolObservationFixture(t)
	query := AppliedPoolReferenceV2Query{NativeEpoch: fixture.query.NativeEpoch, MinimumSettlementEpoch: fixture.query.MinimumSettlementEpoch,
		VectorHash: fixture.query.VectorHash, PoolNoId: fixture.query.PoolNoId, PoolHotkey: fixture.query.PoolHotkey}
	got, err := DiscoverConfiguredAppliedPoolReferenceV2(t.Context(), fixture.options, query)
	if err != nil || got == nil || got.HandoffSha256 != fixture.options.HandoffSHA256 || got.MeasurementHash != fixture.query.MeasurementHash || got.MeasurementBytes != fixture.query.MeasurementBytes || !got.ReferenceMustBeIndependentlyPinned || got.NativeChainAcceptance {
		t.Fatalf("normal reference discovery failed: %+v %v", got, err)
	}
	raw, err := json.Marshal(got)
	if err != nil || bytes.Contains(raw, []byte(`"uids"`)) || bytes.Contains(raw, []byte(`"values"`)) {
		t.Fatalf("reference-only discovery exported a vector: %s %v", raw, err)
	}
	missingPin := fixture.query
	missingPin.MeasurementHash, missingPin.MeasurementBytes = "", 0
	if vector, err := ObserveConfiguredAppliedPoolV2(t.Context(), fixture.options, missingPin); err == nil || vector != nil {
		t.Fatalf("discovery silently authorized an unpinned vector: %+v %v", vector, err)
	}
	query.VectorHash = releaseHex32([32]byte{0x73})
	if reference, err := DiscoverConfiguredAppliedPoolReferenceV2(t.Context(), fixture.options, query); err == nil || reference != nil {
		t.Fatalf("foreign public progress selected a reference: %+v %v", reference, err)
	}
}
