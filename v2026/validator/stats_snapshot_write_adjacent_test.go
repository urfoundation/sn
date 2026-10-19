//go:build linux || darwin

package validator

// Legacy API compatibility is deliberate and separate from v2 physical
// authority. These controls exercise real paths/bytes, not fake snapshots.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// Relative paths and stable aliases may name an existing physical ancestor;
// finite missing children are created only through its retained descriptor.
func TestStatsSnapshotWriteLegacyRelativeAliasAndMissingCompatibility(t *testing.T) {
	for _, kind := range []string{"relative", "alias", "alias-missing"} {
		stats, physical, _, expected := newStatsSnapshotWriteTest(t)
		dir := physical
		if kind == "relative" {
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			dir, err = filepath.Rel(cwd, physical)
			if err != nil || filepath.IsAbs(dir) {
				t.Fatalf("actual relative path prerequisite: %v", err)
			}
		} else {
			alias := physical + "-alias"
			if err := os.Symlink(filepath.Base(physical), alias); err != nil {
				t.Fatal(err)
			}
			dir = alias
			if kind == "alias-missing" {
				dir, physical = filepath.Join(alias, "one", "two"), filepath.Join(physical, "one", "two")
			}
		}
		if err := stats.Save(dir); err != nil {
			t.Fatalf("legacy %s admission failed: %v", kind, err)
		}
		if !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(physical, "stats.json")), expected) {
			t.Fatalf("legacy %s changed encoding/routing", kind)
		}
	}
}

// Existing mode repair remains legacy-only and changes the admitted inode,
// while strict v2 refuses without chmod-ing an existing publication directory.
func TestStatsSnapshotWriteLegacyModeRepairAndV2NoRepair(t *testing.T) {
	for _, physical := range []bool{false, true} {
		stats, dir, before, expected := newStatsSnapshotWriteTest(t)
		if err := os.Chmod(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		owner, err := stats.acquireStatsWrite(t.Context(), "snapshot-mode-control")
		if err != nil {
			t.Fatal(err)
		}
		err = owner.prepareSnapshot(dir, physical)
		if err == nil {
			err = stats.saveOwned(dir, owner.persist)
		}
		err = errors.Join(err, owner.finishSnapshot())
		owner.release()
		info, statErr := os.Stat(dir)
		if statErr != nil {
			t.Fatal(statErr)
		}
		actual := statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json"))
		if physical {
			if err == nil || info.Mode().Perm() != 0o750 || !bytes.Equal(actual, before) {
				t.Fatalf("v2 repaired an existing non-private directory: %v", err)
			}
		} else if err != nil || info.Mode().Perm() != 0o700 || !bytes.Equal(actual, expected) {
			t.Fatalf("legacy owned mode repair changed: %v", err)
		}
	}
}

// The physical root stays unchanged, so only the original logical-name check
// can reject a moved legacy alias before writing the replacement directory.
func TestStatsSnapshotWriteLegacyAliasRetargetRefused(t *testing.T) {
	stats, dir, before, _ := newStatsSnapshotWriteTest(t)
	alias, other := dir+"-alias", ordinaryInputCommitTestDir(t)
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	marker := []byte("other alias target\n")
	if err := os.WriteFile(filepath.Join(other, "stats.json"), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	steps := 0
	stats.writeHooks.step = func(operation, stage string) {
		if operation != "save" || stage != "before-snapshot" {
			return
		}
		steps++
		if err := os.Rename(alias, alias+"-preserved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(other, alias); err != nil {
			t.Fatal(err)
		}
	}
	err := stats.Save(alias)
	if steps != 1 || err == nil || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), before) || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(other, "stats.json")), marker) {
		t.Fatalf("legacy alias retarget acquired a new write authority: steps=%d error=%v", steps, err)
	}
}

// Actual activation supplies the v6 snapshot; transport cannot synthesize its
// authority by changing a legacy version header. Physical admission remains
// independent of whether the encoded bytes are otherwise valid.
func TestStatsSnapshotWriteEncodedVersionPinsPhysicalAuthority(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	participant := fixture.participants[0]
	stats, dir := participant.Stats, participant.StateDir
	before := statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json"))
	snapshot := stats.snapshotStats()
	if snapshot.Version != 6 || snapshot.AttemptV2 == nil || snapshot.AttemptV2.Activation != fixture.fixtures[0].expected.Activation {
		t.Fatal("real activation did not establish the exact v6 snapshot authority")
	}
	encoded, err := encodeStatsSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, before) {
		t.Fatal("real activated snapshot differs from its durable physical bytes")
	}
	alias := dir + "-alias"
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	if err := writeEncodedStatsSnapshot(filepath.Join(alias, "stats.json"), encoded); err == nil || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), before) {
		t.Fatalf("encoded v6 inherited legacy alias admission: %v", err)
	}
	if err := writeEncodedStatsSnapshot(filepath.Join(dir, "stats.json"), encoded); err != nil || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), encoded) {
		t.Fatalf("actual physical v6 bytes did not survive writer transport: %v", err)
	}
	write, err := encodedStatsSnapshotWrite(filepath.Join(dir, "stats.json"), encoded)
	if err != nil || write.version != 6 || !bytes.Equal(write.data, encoded) {
		t.Fatalf("snapshot payload lost actual version: %v", err)
	}
	for _, invalid := range []string{`{}`, `{"v":0}`, `{"v":7}`, `{"ema":{},"v":5}`, `{"v":"6"}`, `[]`} {
		if err := writeEncodedStatsSnapshot(filepath.Join(dir, "stats.json"), []byte(invalid)); err == nil || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), encoded) {
			t.Fatalf("invalid private version header reached mutation: %s/%v", invalid, err)
		}
	}
}

// Invalid-byte authority is rejected, not normalized into another JSON/path
// identity. A v2 alias is also refused before any temporary or chmod operation.
func TestStatsSnapshotWriteV2InvalidPathAndAliasRefusedBeforeMutation(t *testing.T) {
	stats, dir, before, _ := newStatsSnapshotWriteTest(t)
	alias := dir + "-alias"
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{alias, dir + string([]byte{0xff}), dir + "/.", string(filepath.Separator)} {
		mutations := 0
		stats.writeHooks.snapshotIO.after = func(stage string, _ *os.File) error {
			if stage != "directory-admitted" && stage != "directory-closed" {
				mutations++
			}
			return nil
		}
		owner, err := stats.acquireStatsWrite(t.Context(), "snapshot-physical-control")
		if err != nil {
			t.Fatal(err)
		}
		err = owner.prepareSnapshot(candidate, true)
		err = errors.Join(err, owner.finishSnapshot())
		owner.release()
		if err == nil || mutations != 0 || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), before) {
			t.Fatalf("invalid physical snapshot authority mutated state: path=%q mutations=%d error=%v", candidate, mutations, err)
		}
	}
}

// Destination replacement never opens/follows the old leaf. Existing symlinks
// and FIFOs retain legacy atomic-rename semantics, without blocking reads.
func TestStatsSnapshotWriteLegacyLeafReplacementDoesNotFollowOrBlock(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo", "hardlink"} {
		stats, dir, before, expected := newStatsSnapshotWriteTest(t)
		path, preserved := filepath.Join(dir, "stats.json"), filepath.Join(dir, "old-stats.json")
		if err := os.Rename(path, preserved); err != nil {
			t.Fatal(err)
		}
		var err error
		switch kind {
		case "symlink":
			err = os.Symlink(filepath.Base(preserved), path)
		case "fifo":
			err = unix.Mkfifo(path, 0o600)
		default:
			err = os.Link(preserved, path)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := stats.Save(dir); err != nil || !bytes.Equal(statsSnapshotWriteTestRead(t, path), expected) || !bytes.Equal(statsSnapshotWriteTestRead(t, preserved), before) {
			t.Fatalf("legacy %s destination was followed or changed: %v", kind, err)
		}
	}
}

// A generic journal callback changes the parent before Save. The prior live
// egress window and both physical namespaces must remain untouched.
func TestStatsSnapshotWriteLegacyNativeJournalRetargetKeepsWindow(t *testing.T) {
	stats, dir, before, _ := newStatsSnapshotWriteTest(t)
	live, calls := stats.snapshotStats(), 0
	marker := []byte("foreign native journal target\n")
	measurement, err := stats.detachReleaseStatsMeasurement(dir, func(ReleaseStatsMeasurement, uint64) error {
		calls++
		return statsSnapshotWriteTestRetarget(dir, marker)
	})
	if calls != 1 || err == nil || !reflect.DeepEqual(measurement, ReleaseStatsMeasurement{}) || !reflect.DeepEqual(stats.snapshotStats(), live) || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir, "stats.json")), marker) || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(dir+"-preserved", "stats.json")), before) {
		t.Fatalf("legacy native callback retarget lost its snapshot/window: calls=%d error=%v", calls, err)
	}
}

// Real legacy batch generation and its original complete signed verifier
// exercise the adjacent public default snapshot callback without replacing it.
func TestStatsSnapshotWritePublicBatchDefaultKeepsExactSnapshots(t *testing.T) {
	first, firstLedger := newAttemptSettlementTestParticipant(t, 1)
	second, secondLedger := newAttemptSettlementTestParticipant(t, 2)
	t.Cleanup(func() { _ = firstLedger.Close(); _ = secondLedger.Close() })
	participants := []AttemptSettlementParticipant{second, first}
	if err := AdvanceAttemptSettlementEpoch(t.TempDir(), 43, attemptLedgerTestBoundary(), participants); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAttemptSettlementBatch([]*AttemptSettlementTransition{first.Stats.settlementTransition, second.Stats.settlementTransition}); err != nil {
		t.Fatal(err)
	}
	for _, participant := range participants {
		expected, err := encodeStatsSnapshot(participant.Stats.snapshotStats())
		if err != nil || !bytes.Equal(statsSnapshotWriteTestRead(t, filepath.Join(participant.StateDir, "stats.json")), expected) {
			t.Fatalf("public default snapshot writer changed canonical batch bytes: %v", err)
		}
	}
}
