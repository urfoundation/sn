//go:build linux

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

func nativeProducerRestoreLongPath(t *testing.T, root, name string) string {
	t.Helper()
	path := root
	for range 6 {
		path = filepath.Join(path, strings.Repeat("\x01", 240))
	}
	path = filepath.Join(path, name)
	if len(path) <= maximumMonitorHistoryPath || !bootstrapRootAbsolutePath(path) {
		t.Fatal("original native namespace did not exceed monitor-only limits")
	}
	return path
}

func nativeProducerRestoreLongNamespaceContinues(t *testing.T, profile string) {
	t.Helper()
	f := newNativeProducerRestoreNamespaceFixture(t, 2, false, profile)
	policy := f.combined.archive.source.policy
	original := policy.Native.Observation.Execution.Producer.Authority
	if profile == "approval" && (len(original.Path) <= maximumMonitorHistoryPath || len(filepath.Base(original.Path)) != 255) {
		t.Fatal("original signed native approval omitted its accepted filename and path")
	}
	f.combined.apply(t, f.combined.plan(t), true)
	raw, err := os.ReadFile(original.Path)
	if err != nil || monitorReadDigest(raw) != original.Sha256 || f.combined.archive.source.policy.identityHash() != policy.identityHash() {
		t.Fatal("native namespace restore changed its original signed approval", err)
	}
	p := f.producer
	p.ctx = context.WithValue(f.combined.archive.ctx, nativeProducerStateKey{}, f.combined.state.Native.ExecutionProducer)
	before := mainnetNamespaceTest(t, p.source.policy.Execution.Directory)
	observation, code, issue := p.command(t)
	if code != 0 || !observation.Complete || observation.ExecutionProducer == nil || observation.ExecutionProducer.Completed != 3 || observation.ExecutionProducer.Cursor.Number != 103 || observation.ExecutionWindow == nil || observation.ExecutionWindow.MinerAllocation != "0" || observation.TargetMet != nil || observation.ActivationReady || observation.ActualNativeOutcomeVerified {
		t.Fatal("long native namespace lost exact original pending continuation", code, issue)
	}
	if p.proofs.Load() != f.proofs || p.blocks.Load()-f.blocks != 1 || !reflect.DeepEqual(before, mainnetNamespaceTest(t, p.source.policy.Execution.Directory)) {
		t.Fatal("native namespace restore recaptured evidence or rewrote pending custody")
	}
	if raw, err := os.ReadFile(f.pendingPath); err != nil || !bytes.Equal(raw, []byte{1, 2, 3}) {
		t.Fatal("native namespace restore discarded unknown pending trie bytes", err)
	}
}

func TestNativeProducerRestoreLongApprovalNamespaceContinuesOriginalJob(t *testing.T) {
	nativeProducerRestoreLongNamespaceContinues(t, "approval")
}

func TestNativeProducerRestoreLongRuntimeNamespaceContinuesOriginalJob(t *testing.T) {
	nativeProducerRestoreLongNamespaceContinues(t, "runtime")
}

// The real copied member reader admits a long native path while the monitor
// snapshot reader retains its own bound. A symlink cannot borrow those bytes.
func TestNativeProducerRestoreLongCopiedMemberKeepsPhysicalAncestry(t *testing.T) {
	root := t.TempDir()
	protectFreshEconomicConservationTestRoot(t, root)
	path := nativeProducerRestoreLongPath(t, root, strings.Repeat("r", 255))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("synthetic exact original signed-input bytes\n")
	if err := os.WriteFile(path, raw, 0400); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	member := durablevolume.PreparationFile{Path: relative, Kind: "file", Mode: 0400, Bytes: uint64(len(raw)), Sha256: monitorReadDigest(raw)}
	if actual, err := readStorageNativeProducerMember(t.Context(), directory, member); err != nil || !bytes.Equal(actual, raw) {
		t.Fatal("original long copied native member could not be read", err)
	}
	if owner, err := openStorageMonitorTreeTarget(t.Context(), directory, filepath.Dir(relative)); err == nil || owner != nil {
		t.Fatal("native copied namespace silently enlarged the monitor snapshot profile", err)
	}
	if err := os.Symlink(filepath.Dir(path), filepath.Join(root, "redirected")); err != nil {
		t.Fatal(err)
	}
	member.Path = filepath.Join("redirected", filepath.Base(path))
	if actual, err := readStorageNativeProducerMember(t.Context(), directory, member); err == nil || actual != nil {
		t.Fatal("native copied reader followed a replacement ancestor to original bytes", err)
	}
}
