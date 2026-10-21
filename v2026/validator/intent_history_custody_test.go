//go:build linux || darwin

// A new segment is part of the reference set as soon as a candidate pins it;
// its custody must survive the same publication boundary as older segments.
package validator

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestIntentV2HistoryPublicationRefusesRemovedNewSegment(t *testing.T) {
	history := newIntentV2SegmentTestHistory(t)
	history.growToSeal()
	history.trust()
	read, err := history.store.readV2(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer read.custody.close()
	before := bytes.Clone(read.encoded)
	failed := *read.file.Current
	failed.Status, failed.Error = "failed", "synthetic retained failure"
	read.file.History = append(read.file.History, failed)
	successor := history.intent
	read.file.Current = &successor
	if err := history.store.sealHistoryV2(t.Context(), read, history.limit()); err != nil {
		t.Fatal(err)
	}
	if len(read.file.Segments) != 1 {
		t.Fatal("fixture did not create one new segment")
	}
	path := filepath.Join(history.store.stateDir, filepath.FromSlash(steeringIntentSegmentName(read.file.Segments[0].SHA256)))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := history.store.writeV2(t.Context(), read); err == nil {
		t.Error("published an intent whose newly sealed segment was removed")
	}
	after, err := os.ReadFile(history.store.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("missing new segment changed the live intent: err=%v", err)
	}
}

// A preexisting segment namespace is evidence, never permission to repair a
// foreign directory or write through an alias outside the state directory.
func TestIntentV2SegmentRefusesSymlinkDirectoryWithoutChangingTarget(t *testing.T) {
	state := t.TempDir()
	target := t.TempDir()
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(state, steeringIntentSegmentDirectory)
	if err := os.Symlink(target, directory); err != nil {
		t.Fatal(err)
	}
	if err := persistSteeringIntentSegment(t.Context(), filepath.Join(directory, "synthetic.json"), []byte("synthetic segment")); err == nil {
		t.Error("published a segment through a symlink directory")
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("symlink target permissions changed: %v %v", info, err)
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Errorf("symlink target was modified: %v %v", entries, err)
	}
}

func TestIntentV2SegmentRefusesPermissiveDirectoryWithoutRepair(t *testing.T) {
	directory := filepath.Join(t.TempDir(), steeringIntentSegmentDirectory)
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := persistSteeringIntentSegment(t.Context(), filepath.Join(directory, "synthetic.json"), []byte("synthetic segment")); err == nil {
		t.Error("published a segment in a permissive directory")
	}
	info, err := os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("existing permissions were repaired: %v %v", info, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Errorf("permissive directory was modified: %v %v", entries, err)
	}
}
