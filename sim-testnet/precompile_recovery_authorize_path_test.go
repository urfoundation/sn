package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrecompileRecoveryAuthorizationPathAllowsFirstPublicationAndRejectsSymlink(t *testing.T) {
	stateDir := t.TempDir()
	publicDir := filepath.Join(stateDir, "public")
	if err := os.Mkdir(publicDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(publicDir, "precompile-recovery-authorization.json")
	if err := validatePrecompileRecoveryArtifactPath(stateDir, path); err != nil {
		t.Fatalf("first publication should allow an absent leaf: %v", err)
	}
	if err := os.Symlink(filepath.Join(stateDir, "elsewhere"), path); err != nil {
		t.Fatal(err)
	}
	if err := validatePrecompileRecoveryArtifactPath(stateDir, path); err == nil {
		t.Fatal("symlink leaf was accepted")
	}
}
