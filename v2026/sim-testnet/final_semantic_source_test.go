package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalSemanticCampaignArtifactLoaderRejectsParentSymlinkEscape(t *testing.T) {
	stateDir := t.TempDir()
	runDir := filepath.Join(stateDir, "runs", "candidate")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "proof.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(runDir, "escaped")); err != nil {
		t.Fatal(err)
	}
	load, err := NewFinalSemanticCampaignArtifactLoader(stateDir, runDir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = load(context.Background(), FinalArtifactLocator{Kind: "test", URI: "escaped/proof.json", ContentHash: bytesSHA256([]byte("{}")), SizeBytes: 2})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("parent symlink escape was not rejected: %v", err)
	}
}

func TestFinalSemanticCampaignArtifactLoaderReadsOnlyClosedRoots(t *testing.T) {
	stateDir := t.TempDir()
	runDir := filepath.Join(stateDir, "runs", "candidate")
	if err := os.MkdirAll(filepath.Join(runDir, "artifacts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "public"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string][]byte{
		filepath.Join(runDir, "artifacts", "run.json"):  []byte(`{"run":true}`),
		filepath.Join(stateDir, "public", "setup.json"): []byte(`{"public":true}`),
	} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	load, err := NewFinalSemanticCampaignArtifactLoader(stateDir, runDir)
	if err != nil {
		t.Fatal(err)
	}
	for uri, want := range map[string]string{"artifacts/run.json": `{"run":true}`, "public/setup.json": `{"public":true}`} {
		got, err := load(context.Background(), FinalArtifactLocator{Kind: "test", URI: uri, ContentHash: bytesSHA256([]byte(want)), SizeBytes: uint64(len(want))})
		if err != nil || string(got) != want {
			t.Fatalf("load %s=%q error=%v", uri, got, err)
		}
	}
	if _, err := load(context.Background(), FinalArtifactLocator{Kind: "test", URI: "https://example.test/object", ContentHash: "sha256:" + strings.Repeat("11", 32), SizeBytes: 1}); err == nil || !strings.Contains(err.Error(), "network") {
		t.Fatalf("network locator was not rejected: %v", err)
	}
}

func TestFinalSemanticNativeHeadUsesOnlyCapturedSignedCheckpoint(t *testing.T) {
	chain := &FinalCollectedChainSnapshot{
		NativeHead:  ChainHead{Number: 500, Hash: finalTestHex(0x50)},
		NativeHeads: []ChainHead{{Number: 100, Hash: finalTestHex(0x10)}, {Number: 500, Hash: finalTestHex(0x50)}},
	}
	signed := finalTestHex(0x30)
	got, err := finalSemanticNativeHead(chain, 300, signed)
	if err != nil || got != (ChainHead{Number: 300, Hash: signed}) {
		t.Fatalf("signed closed checkpoint=(%+v,%v), want exact block 300", got, err)
	}
	if _, err := finalSemanticNativeHead(chain, 300, ""); err == nil {
		t.Fatal("uncaptured checkpoint without a signed hash was accepted")
	}
	if _, err := finalSemanticNativeHead(chain, 501, finalTestHex(0x51)); err == nil {
		t.Fatal("signed checkpoint beyond the captured terminal was accepted")
	}
	if _, err := finalSemanticNativeHead(chain, 300, "0x1234"); err == nil {
		t.Fatal("malformed signed checkpoint hash was accepted")
	}
}
