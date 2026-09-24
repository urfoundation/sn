//go:build linux || darwin

// Read-only generation observation uses real canonical intent history and pins.
package validator

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestProvisionalConfiguredIntentObservationPinsExactReadOnlySource(t *testing.T) {
	_, path := authenticatedIntentHistoryFixture(t)
	cfg := validReleaseConfig(t)
	cfg.StateDir = filepath.Dir(path)
	configPath := writeReleaseConfig(t, cfg)
	if err := os.Chmod(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	options := ProvisionalConfiguredIntentObservationV2Options{
		Config:        ReleaseEvidenceV2File{Path: configPath, Bytes: uint64(len(config)), SHA256: fmt.Sprintf("0x%x", sha256.Sum256(config))},
		HandoffSHA256: ReleaseMeasurementContentHash([]byte("owner-authenticated-generation")), StateDir: cfg.StateDir,
		DeploymentID: cfg.DeploymentID, ValidatorID: cfg.ValidatorID, Netuid: cfg.Netuid, Hotkey: testIntentHotkey(t).PublicKey(),
	}
	result, err := ObserveProvisionalConfiguredIntentsV2(t.Context(), options)
	if err != nil || result.State != "observed" || result.RecordedAppliedIntents == nil || *result.RecordedAppliedIntents != 2 || result.FinalAcceptance || result.StateDirectory != cfg.StateDir || result.HandoffSHA256 != options.HandoffSHA256 {
		t.Fatalf("exact generation observation: %+v %v", result, err)
	}
	for _, mutate := range []func(*ProvisionalConfiguredIntentObservationV2Options){
		func(o *ProvisionalConfiguredIntentObservationV2Options) {
			o.Config.SHA256 = releaseHex32([32]byte{0x78})
		},
		func(o *ProvisionalConfiguredIntentObservationV2Options) { o.Config.Bytes++ },
		func(o *ProvisionalConfiguredIntentObservationV2Options) { o.StateDir = filepath.Dir(o.StateDir) },
		func(o *ProvisionalConfiguredIntentObservationV2Options) { o.DeploymentID += "-foreign" },
		func(o *ProvisionalConfiguredIntentObservationV2Options) { o.ValidatorID++ },
		func(o *ProvisionalConfiguredIntentObservationV2Options) { o.Netuid++ },
		func(o *ProvisionalConfiguredIntentObservationV2Options) { o.Hotkey[0] ^= 1 },
		func(o *ProvisionalConfiguredIntentObservationV2Options) { o.HandoffSHA256 = "unbound" },
	} {
		changed := options
		mutate(&changed)
		result, err := ObserveProvisionalConfiguredIntentsV2(t.Context(), changed)
		if err == nil || result.State != "unknown" || result.RecordedAppliedIntents != nil || len(result.Receipts) != 0 || result.FinalAcceptance {
			t.Fatalf("changed source became observation credit: %+v %v", result, err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("observation changed the intent store: %v", err)
	}
}
