//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/crv4"
)

func TestReleaseNativeObservationV2PreservesAbsenceAndUnresolvedPublication(t *testing.T) {
	store := intentV2PublicationTest(t)
	cfg := store.v2.runtime.cfg
	hotkey := [32]byte{7}
	got, err := readReleaseNativeSourceReferencesV2(t.Context(), &cfg, hotkey, nil)
	if err != nil || got == nil || len(got.References) != 0 || got.StoreSHA256 != ReleaseMeasurementContentHash(nil) {
		t.Fatalf("read-only fresh observation changed actual absence: %+v %v", got, err)
	}
	if _, err := os.Lstat(store.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only observation created an intent store", err)
	}
	marker := filepath.Join(cfg.StateDir, releaseIntentV2Marker)
	want := []byte("actual incomplete publication\n")
	if err := os.WriteFile(marker, want, 0o600); err != nil { t.Fatal(err) }
	if got, err := readReleaseNativeSourceReferencesV2(t.Context(), &cfg, hotkey, nil); err == nil || got != nil {
		t.Fatal("unresolved publication became a successful empty observation")
	}
	if got, err := os.ReadFile(marker); err != nil || !bytes.Equal(got, want) {
		t.Fatal("source observation changed the unresolved publication", err)
	}
	if err := os.Remove(marker); err != nil { t.Fatal(err) }
	if err := os.WriteFile(store.path, []byte(`{"schema":"legacy","current":null,"history":[]}`), 0o600); err != nil { t.Fatal(err) }
	if got, err := readReleaseNativeSourceReferencesV2(t.Context(), &cfg, hotkey, nil); err == nil || got != nil {
		t.Fatal("legacy bytes became strict V2 progress")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := readReleaseNativeSourceReferencesV2(ctx, &cfg, hotkey, nil); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("canceled local source retained partial progress", err)
	}
}

// The native source verifier remains separate. This boundary specifically
// verifies the real V2 sr25519 envelope and all configured source links; it
// does not fabricate a positive native inclusion or a scoring verdict.
func TestReleaseNativeObservationV2BindsRealSignedSourceIdentity(t *testing.T) {
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 1)
	encoded, envelope := fixture.seal(t)
	decoded, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), encoded, fixture.options().MaxControlBytes)
	if err != nil { t.Fatal(err) }
	a := fixture.artifact
	cfg := ReleaseConfig{DeploymentID: a.DeploymentID, ChainID: a.ChainID, GenesisHash: a.GenesisHash, Coordinator: a.Coordinator, SettlementVault: a.SettlementVault, ValidatorID: a.ValidatorID, Netuid: a.Netuid, PolicyHash: a.PolicyHash}
	intent := SteeringIntent{ValidatorID: a.ValidatorID, Netuid: a.Netuid, SubnetEpoch: a.SubnetEpoch, NativeSnapshotBlock: a.NativeSnapshotBlock, NativeSnapshotHash: a.NativeSnapshotHash, EVMSnapshotBlock: a.EVMSnapshotBlock, EVMSnapshotHash: a.EVMSnapshotHash, SettlementEpoch: a.SettlementEpoch, PolicyHash: a.PolicyHash, SelfUID: a.SelfUID, DepositAudits: a.DepositAudits, MeasurementArtifactHash: envelope.MeasurementArtifactHash, MeasurementArtifactSize: envelope.MeasurementArtifactSize,
		Prepared: &crv4.PreparedSubmission{HotkeyHex: envelope.ValidatorHotkey, ExtrinsicHash: envelope.PreparedExtrinsicHash, Netuid: a.Netuid, SubnetEpoch: a.SubnetEpoch}}
	if err := matchObservedNativeSourceV2(&cfg, fixture.hotkey.PublicKey(), &intent, a, decoded); err != nil { t.Fatal(err) }
	for _, problem := range []string{"source-hash", "prepared-hash", "hotkey", "deployment", "snapshot", "values"} {
		t.Run(problem, func(t *testing.T) {
			changed, config := intent, cfg
			prepared := *intent.Prepared
			changed.Prepared = &prepared
			key := fixture.hotkey.PublicKey()
			switch problem {
			case "source-hash": changed.MeasurementArtifactHash = ReleaseMeasurementContentHash([]byte("other"))
			case "prepared-hash": prepared.ExtrinsicHash = releaseHex32([32]byte{9})
			case "hotkey": key[0] ^= 1
			case "deployment": config.DeploymentID += "-other"
			case "snapshot": changed.NativeSnapshotBlock++
			case "values": changed.Status, changed.Values = "applied", []uint16{1}
			}
			if err := matchObservedNativeSourceV2(&config, key, &changed, a, decoded); err == nil { t.Fatal("changed source identity was admitted") }
		})
	}
	changed := bytes.Clone(encoded)
	signatureAt := bytes.Index(changed, []byte(decoded.Signature))
	if signatureAt < 0 || len(decoded.Signature) < 3 { t.Fatal("original signature is absent") }
	changed[signatureAt+2] = '0'
	if decoded.Signature[2] == '0' { changed[signatureAt+2] = '1' }
	if _, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), changed, fixture.options().MaxControlBytes); err == nil {
		t.Fatal("changed original signature was accepted")
	}
}

func TestReleaseNativeObservationV2RejectsUnapprovedNamespaceBeforeProgress(t *testing.T) {
	store := intentV2PublicationTest(t)
	cfg := store.v2.runtime.cfg
	cfg.DeploymentID, cfg.ValidatorID = "deployment", 1
	request := &ReleaseHistoryAdoptionV2{DeploymentID: cfg.DeploymentID, ValidatorID: cfg.ValidatorID, CoordinatorStateDir: filepath.Join(cfg.StateDir, "foreign")}
	if got, err := readReleaseNativeSourceReferencesV2(t.Context(), &cfg, [32]byte{7}, request); err == nil || got != nil {
		t.Fatal("foreign coordinator namespace acquired progress authority")
	}
	request.CoordinatorStateDir, request.IntentPrefixCount, request.LastNativeEpoch = cfg.StateDir, 1, 1405
	if got, err := readReleaseNativeSourceReferencesV2(t.Context(), &cfg, [32]byte{7}, request); err == nil || got != nil {
		t.Fatal("missing retained prefix acquired progress authority")
	}
}
