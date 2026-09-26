//go:build linux || darwin

package validator

// The setup journal is discovery only, but its absence semantics gate the
// whole flow: a never-created setup directory must read as initially missing
// so the first `validator activate` can prepare, while a retained receipt is
// admitted only when it is canonical.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseActivationSetupReadsAbsentDirectoryAsInitiallyMissing(t *testing.T) {
	cfg := unrenderedReleaseConfig(t)
	preparedPath, completedPath := ReleaseActivationSetupPaths(&cfg)
	if filepath.Dir(preparedPath) != filepath.Join(cfg.StateDir, "evidence-v2-setup") || filepath.Base(completedPath) != "completed.json" {
		t.Fatalf("setup paths = %s %s", preparedPath, completedPath)
	}
	if _, err := os.Lstat(cfg.StateDir); !os.IsNotExist(err) {
		t.Fatalf("fixture state dir exists: %v", err)
	}
	var prepared ReleaseActivationSetupPreparedV2
	_, err := readReleaseActivationSetupV2(context.Background(), preparedPath, cfg.EvidenceV2.Bounds.MaxControlBytes, &prepared)
	if !ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		t.Fatalf("absent setup directory did not read as initially missing: %v", err)
	}
	// A written receipt round-trips canonically and is no longer missing.
	value := ReleaseActivationSetupCompletedV2{Schema: ReleaseActivationSetupCompletedSchemaV2, PreparedHash: attemptHex32([32]byte{1}), Boundary: ReleaseActivationSetupHeadV2{Number: 7, Hash: attemptHex32([32]byte{2})}}
	if _, err := writeReleaseActivationSetupV2(context.Background(), completedPath, &value, cfg.EvidenceV2.Bounds.MaxControlBytes); err != nil {
		t.Fatal(err)
	}
	var retained ReleaseActivationSetupCompletedV2
	if _, err := readReleaseActivationSetupV2(context.Background(), completedPath, cfg.EvidenceV2.Bounds.MaxControlBytes, &retained); err != nil || retained != value {
		t.Fatalf("retained completion = %+v, %v", retained, err)
	}
	if info, err := os.Stat(completedPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("completion mode = %v, %v", info, err)
	}
	// Noncanonical or unknown-field bytes are refused even though they parse.
	if err := os.WriteFile(preparedPath, []byte(`{"schema":"x","extra":1}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readReleaseActivationSetupV2(context.Background(), preparedPath, cfg.EvidenceV2.Bounds.MaxControlBytes, &prepared); err == nil || ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		t.Fatalf("unknown-field preparation admitted: %v", err)
	}
}
