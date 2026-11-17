package main

// A composed runtime root owns only its explicit journals. Exported signing
// inputs remain alongside the independently prepared fixture configuration.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The old fixture writes an unowned sidecar into the relocated runtime root.
// This forces the real composition transition without replaying eight actions.
func TestEvmActionFixtureKeepsSignedInputOutsideComposedRuntime(t *testing.T) {
	f := newEvmCreateFixture(t)
	configDirectory := filepath.Dir(f.configPath)
	runtimeDirectory := mainnetPrivateTestDir(t)
	f.config.Plan.RunDirectory = runtimeDirectory
	f.signSelectedAction()
	entries, err := os.ReadDir(runtimeDirectory)
	if err != nil || len(entries) != 0 || filepath.Dir(f.signedPath) != configDirectory {
		t.Fatal("fixture signing created unowned runtime sidecar", f.signedPath, len(entries), err)
	}
	raw, err := os.ReadFile(f.signedPath)
	if err != nil || !bytes.Equal(raw, f.raw) || safeReleaseHash(raw) != f.signedHash {
		t.Fatal("external fixture input lost its exact bytes or digest", err)
	}
	tx, err := f.config.Plan.Actions[f.plan.ActionIndex].signed(raw)
	if err != nil || tx.Hash() != f.tx.Hash() {
		t.Fatal("input placement changed original transaction authority", err)
	}
}
