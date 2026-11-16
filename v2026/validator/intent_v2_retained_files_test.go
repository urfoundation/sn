//go:build linux || darwin

// A reused entry's retained files are owned by custody identity between the
// complete reads that hash them, and any change to that identity reads them
// again.
package validator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The fixture's one real entry behind n-1 failed same-epoch predecessors over
// the same measurement. Each predecessor is its own vector through a synthetic
// deposit audit usage, which the reused read never re-derives, and the memo
// trusts every entry, as after a complete read of this history in this
// process. The returned count is the custody's byte reads of any file.
func intentV2RetainedFilesTestHistory(t *testing.T, n int) (*IntentStore, *SteeringIntent, *int) {
	t.Helper()
	fixture := newProductionContinuationTestFixture(t)
	store := fixture.steerer.intents
	intent, err := store.beginV2(t.Context(), *fixture.intent)
	if err != nil {
		t.Fatalf("actual nonempty V2 begin: %v", err)
	}
	if _, err := store.currentV2(t.Context()); err != nil {
		t.Fatal(err)
	}
	options, found := store.v2.verified.optionsAt(0)
	if !found {
		t.Fatal("the complete read kept no measurement options")
	}
	file := steeringIntentFile{Schema: steeringIntentSchema, Current: intent}
	for index := 0; index < n-1; index++ {
		failed := *intent
		failed.DepositAudits = slices.Clone(intent.DepositAudits)
		failed.DepositAudits[0].UsageBytes += uint64(index + 1)
		failed.Status, failed.Error = "failed", fmt.Sprintf("synthetic retained failure %d", index)
		if failed.VectorHash, err = failed.computeVectorHash(); err != nil {
			t.Fatal(err)
		}
		file.History = append(file.History, failed)
	}
	encoded, err := marshalAttemptSettlementV2JSON(t.Context(), &file, fixture.runtime.cfg.EvidenceV2.Bounds.IntentFileLimit(), true, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	all := make([]*SteeringIntent, 0, n)
	for index := range file.History {
		all = append(all, &file.History[index])
	}
	digests, digestible := releaseIntentV2EntryDigests(append(all, intent))
	if !digestible {
		t.Fatal("the synthetic history has no digest chain")
	}
	store.v2.verified = &releaseIntentV2VerifiedHistory{authority: store.v2.verifiedAuthority(), digests: digests, options: map[int]ReleaseMeasurementV2Options{n - 1: options}}
	reads := 0
	store.v2.referenceReadHooks.step = func(operation string, _ *os.File, _ int) error {
		if operation == "leaf-read" {
			reads++
		}
		return nil
	}
	return store, intent, &reads
}

// One steering poll's byte reads: the intent file and each retained file it
// reads in full.
func intentV2RetainedFilesTestPoll(t *testing.T, store *IntentStore, intent *SteeringIntent, reads *int) int {
	t.Helper()
	before := *reads
	current, err := store.currentV2(t.Context())
	if err != nil || current == nil || current.VectorHash != intent.VectorHash {
		t.Fatalf("actual reused V2 read: %v", err)
	}
	return *reads - before
}

// Each steering poll used to read and hash every retained entry's artifact and
// envelope again, a cost that grows with the whole history. A reused entry
// whose files keep the custody identity they were hashed under is now owned
// without its bytes. The first entry, which is decoded, and the last, whose
// bytes the next entry's lineage needs, are still read in full.
func TestIntentV2ReusedReadSkipsUnchangedRetainedFiles(t *testing.T) {
	const n = 6
	store, intent, reads := intentV2RetainedFilesTestHistory(t, n)
	if got := intentV2RetainedFilesTestPoll(t, store, intent, reads); got != 1+2*n {
		t.Fatalf("the first reused read hashed %d files, not the intent file and all %d entries' %d", got, n, 2*n)
	}
	for range 2 {
		if got := intentV2RetainedFilesTestPoll(t, store, intent, reads); got != 1+2*2 {
			t.Errorf("a later reused read read %d files, not only the intent file and its first and last entries' 4", got)
		}
	}
}

// Replacing a retained file, even by identical bytes, gives it another custody
// identity, so every entry over it is read and hashed again before the new
// identity is trusted.
func TestIntentV2ReusedReadRereadsAReplacedRetainedFile(t *testing.T) {
	const n = 4
	store, intent, reads := intentV2RetainedFilesTestHistory(t, n)
	intentV2RetainedFilesTestPoll(t, store, intent, reads)
	if got := intentV2RetainedFilesTestPoll(t, store, intent, reads); got != 1+2*2 {
		t.Fatalf("the unchanged reused read read %d files", got)
	}
	path := filepath.Join(store.v2.runtime.cfg.StateDir, filepath.FromSlash(intent.MeasurementArtifactPath))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := path + ".synthetic-replacement"
	if err := os.WriteFile(replacement, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if got := intentV2RetainedFilesTestPoll(t, store, intent, reads); got != 1+2*n {
		t.Fatalf("the read after a replacement hashed %d files, not every entry's %d again", got, 2*n)
	}
	if got := intentV2RetainedFilesTestPoll(t, store, intent, reads); got != 1+2*2 {
		t.Errorf("the replaced file's new identity was not kept: %d files read", got)
	}
}

// The identity a reused entry trusts is the one its last complete read hashed.
// Unchanged, its files are owned without a byte read. Replaced by identical
// bytes, they are read and hashed again and the new identity is returned.
// Rewritten in place with its modification time restored, the change time
// still differs, so the changed bytes are read and refused. Either way each
// file is charged against the history allowance once, as by a complete read.
func TestIntentV2RetainedFileIdentitySkipsOnlyAnUnchangedFile(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	store := fixture.steerer.intents
	intent, err := store.beginV2(t.Context(), *fixture.intent)
	if err != nil {
		t.Fatalf("actual nonempty V2 begin: %v", err)
	}
	path := filepath.Join(fixture.runtime.cfg.StateDir, filepath.FromSlash(intent.MeasurementArtifactPath))
	reads := 0
	hooks := releaseMeasurementInputV2ReadHooks{step: func(operation string, _ *os.File, _ int) error {
		if operation == "leaf-read" {
			reads++
		}
		return nil
	}}
	allowance := fixture.runtime.cfg.EvidenceV2.Bounds.MaxHistoryBytes
	read := func(retained *releaseIntentV2RetainedFiles) (*releaseIntentV2RetainedFiles, int, uint64, error) {
		t.Helper()
		custody := &releaseEvidenceV2StartupReferences{remaining: allowance, readHooks: hooks}
		before := reads
		_, _, kept, err := store.readVerifiedMeasurementV2(t.Context(), custody, intent, retained, false, false)
		return kept, reads - before, allowance - custody.remaining, errors.Join(err, custody.close())
	}
	replace := func() {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".synthetic-replacement", raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".synthetic-replacement", path); err != nil {
			t.Fatal(err)
		}
	}
	rewrite := func() {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		raw[len(raw)-2] ^= 1
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteAt(raw, 0); err != nil {
			t.Fatal(errors.Join(err, file.Close()))
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
	}
	// The rewrite corrupts the artifact, so it runs last.
	for _, scenario := range []struct {
		name    string
		change  func()
		reads   int
		refused bool
	}{
		{name: "unchanged", change: func() {}, reads: 0},
		{name: "replaced by identical bytes", change: replace, reads: 2},
		{name: "rewritten in place with its modification time restored", change: rewrite, reads: 1, refused: true},
	} {
		hashed, _, complete, err := read(nil)
		if err != nil || hashed == nil {
			t.Fatalf("%s: the complete read kept no identities: %v", scenario.name, err)
		}
		scenario.change()
		kept, got, charged, err := read(hashed)
		if got != scenario.reads || (err != nil) != scenario.refused {
			t.Errorf("%s: %d byte reads and error %v", scenario.name, got, err)
			continue
		}
		switch {
		case scenario.refused:
		case charged != complete:
			t.Errorf("%s: charged %d bytes against the history allowance, not the complete read's %d", scenario.name, charged, complete)
		case scenario.reads == 0 && (kept == nil || *kept != *hashed):
			t.Errorf("%s: the trusted identities changed", scenario.name)
		case scenario.reads != 0 && (kept == nil || kept.artifact == hashed.artifact || kept.envelope != hashed.envelope):
			t.Errorf("%s: the read kept no new artifact identity", scenario.name)
		}
	}
}
