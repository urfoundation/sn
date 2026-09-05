package main

// Namespace safety is independent of database replay. A real qualified store
// fixture must never be renamed into an unsigned archive, including recovery
// from journals written before disk authority was recognized.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const validatorNamespaceStoreFixtureHash = "62c642eaa728a7c0fc3191c5b3273e1638d6fda87c6a7168a843bf76a4f1daba"

// Exact bytes came from a close/reopen/replay-qualified private store, not a
// hand-authored LevelDB layout or a marker that merely claims signed authority.
type validatorNamespaceStoreFixture struct {
	Schema string `json:"schema"`
	Head   struct {
		LastSequence uint64 `json:"last_sequence"`
		Root         string `json:"root"`
		RecordBytes  uint64 `json:"record_bytes"`
		TrailCount   uint64 `json:"trail_count"`
	} `json:"head"`
	RecordsJSONL []byte `json:"records_jsonl"`
	Files        []struct {
		Name string `json:"name"`
		Data []byte `json:"data"`
	} `json:"files"`
}

// The fixture digest is pinned so an edited opaque store cannot weaken tests.
func readValidatorNamespaceStoreFixture(t *testing.T) validatorNamespaceStoreFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "attempt-record-store-namespace-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	if len(raw) > 1024*1024 || hex.EncodeToString(hash[:]) != validatorNamespaceStoreFixtureHash {
		t.Fatal("namespace private-store fixture differs from its qualified exact bytes")
	}
	var fixture validatorNamespaceStoreFixture
	if err := decodeStrictJSONBytes(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Schema != "urnetwork-test-attempt-namespace-store-v1" || fixture.Head.LastSequence != 8 || fixture.Head.TrailCount != 1 || fixture.Head.RecordBytes == 0 || len(fixture.Files) == 0 || bytes.Count(fixture.RecordsJSONL, []byte{'\n'}) != 8 {
		t.Fatal("namespace private-store fixture has an incomplete census")
	}
	return fixture
}

// Install only into a new test-owned directory; names cannot escape that root.
func installValidatorNamespaceStoreFixture(t *testing.T, storePath string, fixture validatorNamespaceStoreFixture) {
	t.Helper()
	if err := os.MkdirAll(storePath, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range fixture.Files {
		if filepath.Base(file.Name) != file.Name || file.Name == "." {
			t.Fatal("fixture has an invalid storage filename")
		}
		output, err := os.OpenFile(filepath.Join(storePath, file.Name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if written, err := output.Write(file.Data); err != nil || written != len(file.Data) {
			_ = output.Close()
			t.Fatalf("fixture copy %s: %d/%d: %v", file.Name, written, len(file.Data), err)
		}
		if err := output.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// Capture paths, modes, exact bytes and symlink targets without following aliases.
func validatorNamespaceTreeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		value := info.Mode().String()
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			value += ":" + target
		} else if !entry.IsDir() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += fmt.Sprintf(":%x", sha256.Sum256(raw))
		}
		result[filepath.ToSlash(relative)] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// Build a formerly accepted pending reset from the exact current source, as
// an interrupted pre-fix binary could have done before the archive rename.
func prepareValidatorNamespacePendingJournal(t *testing.T, stateDir string) validatorAttemptStateResetJournal {
	t.Helper()
	root := filepath.Join(stateDir, "runtime", "validator-1")
	hash, count, err := hashValidatorStateTree(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	journal := validatorAttemptStateResetJournal{
		Schema: validatorAttemptStateResetJournalSchema, DeploymentID: "deployment",
		ValidatorID: 1, Operators: 2, SourceHash: hash, FileCount: count,
		ArchiveName: validatorLegacyArchiveName(hash), Status: "pending",
	}
	if err := persistValidatorAttemptStateResetJournal(filepath.Join(root, validatorAttemptStateResetJournalName), &journal); err != nil {
		t.Fatal(err)
	}
	return journal
}

// Both absent and preserved-empty v1 sources can accompany signed disk rows.
func TestValidatorStateNamespaceProtectsDiskWithoutLegacyJSONL(t *testing.T) {
	fixture := readValidatorNamespaceStoreFixture(t)
	for _, emptyLegacy := range []bool{false, true} {
		stateDir := t.TempDir()
		state := filepath.Join(stateDir, "runtime", "validator-1", "state")
		for noID := 1; noID <= 2; noID++ {
			operator := filepath.Join(state, "operators", fmt.Sprintf("no-%d", noID))
			installValidatorNamespaceStoreFixture(t, filepath.Join(operator, "attempt-ledger.records"), fixture)
			if emptyLegacy {
				writeValidatorStateTestFile(t, filepath.Join(operator, "attempt-ledger.jsonl"), "")
			}
		}
		before := validatorNamespaceTreeSnapshot(t, stateDir)
		err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2)
		if err == nil {
			t.Fatalf("emptyLegacy=%t: signed disk rows were reset as unsigned legacy state", emptyLegacy)
		}
		if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
			t.Fatalf("emptyLegacy=%t: disk refusal changed namespace bytes or paths", emptyLegacy)
		}
	}
}

// Classification itself must not advertise a disk namespace as resettable.
func TestValidatorStateNamespaceClassifiesDiskAsProtected(t *testing.T) {
	fixture := readValidatorNamespaceStoreFixture(t)
	state := filepath.Join(t.TempDir(), "state")
	installValidatorNamespaceStoreFixture(t, filepath.Join(state, "operators", "no-1", "attempt-ledger.records"), fixture)
	legacy, _, err := classifyValidatorAttemptState(state, 2)
	if legacy || err == nil {
		t.Fatalf("disk namespace was classified as resettable: legacy=%t err=%v", legacy, err)
	}
}

// One signed disk operator cannot be silently reset with another legacy one.
func TestValidatorStateNamespaceRejectsMixedDiskAndLegacy(t *testing.T) {
	fixture := readValidatorNamespaceStoreFixture(t)
	stateDir := t.TempDir()
	state := filepath.Join(stateDir, "runtime", "validator-1", "state")
	installValidatorNamespaceStoreFixture(t, filepath.Join(state, "operators", "no-1", "attempt-ledger.records"), fixture)
	writeValidatorStateTestFile(t, filepath.Join(state, "operators", "no-2", "stats.json"), `{"assignments":1}`)
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err == nil {
		t.Fatal("mixed disk and legacy authorities were archived as unsigned")
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
		t.Fatal("mixed namespace refusal changed preserved state")
	}
}

// An old journal must not bypass present-day classification before rename.
func TestValidatorStateNamespacePendingResetProtectsDiskSource(t *testing.T) {
	fixture := readValidatorNamespaceStoreFixture(t)
	stateDir := t.TempDir()
	state := filepath.Join(stateDir, "runtime", "validator-1", "state")
	installValidatorNamespaceStoreFixture(t, filepath.Join(state, "operators", "no-1", "attempt-ledger.records"), fixture)
	prepareValidatorNamespacePendingJournal(t, stateDir)
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err == nil {
		t.Fatal("pending reset journal archived a signed disk source")
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
		t.Fatal("pending disk-source refusal changed preserved state")
	}
}

// A pre-fix archive containing disk authority cannot acquire a completion
// receipt or create fresh state, regardless of the old journal status.
func TestValidatorStateNamespaceResetRejectsDiskArchive(t *testing.T) {
	fixture := readValidatorNamespaceStoreFixture(t)
	for _, status := range []string{"pending", "complete"} {
		stateDir := t.TempDir()
		root := filepath.Join(stateDir, "runtime", "validator-1")
		state := filepath.Join(root, "state")
		installValidatorNamespaceStoreFixture(t, filepath.Join(state, "operators", "no-1", "attempt-ledger.records"), fixture)
		journal := prepareValidatorNamespacePendingJournal(t, stateDir)
		if err := os.Rename(state, filepath.Join(root, journal.ArchiveName)); err != nil {
			t.Fatal(err)
		}
		journal.Status = status
		if err := persistValidatorAttemptStateResetJournal(filepath.Join(root, validatorAttemptStateResetJournalName), &journal); err != nil {
			t.Fatal(err)
		}
		before := validatorNamespaceTreeSnapshot(t, stateDir)
		if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err == nil {
			t.Fatalf("%s journal blessed a disk archive as unsigned", status)
		}
		if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
			t.Fatalf("%s disk-archive refusal changed preserved state", status)
		}
	}
}

// Completed legacy archival cannot authorize a second reset after disk starts.
func TestValidatorStateNamespaceCompletedResetProtectsNewDiskState(t *testing.T) {
	fixture := readValidatorNamespaceStoreFixture(t)
	stateDir := t.TempDir()
	state := filepath.Join(stateDir, "runtime", "validator-1", "state")
	writeValidatorStateTestFile(t, filepath.Join(state, "operators", "no-1", "stats.json"), `{"assignments":1}`)
	if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err != nil {
		t.Fatal(err)
	}
	installValidatorNamespaceStoreFixture(t, filepath.Join(state, "operators", "no-1", "attempt-ledger.records"), fixture)
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err == nil {
		t.Fatal("complete old reset journal authorized resetting a new disk namespace")
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
		t.Fatal("completed-reset refusal changed current state or old evidence")
	}
}

// Corruption and missing metadata cannot turn possible authority into a reset
// permission. Aliases and out-of-place reserved names also remain untouched.
func TestValidatorStateNamespaceRejectsAmbiguousDiskArtifacts(t *testing.T) {
	fixture := readValidatorNamespaceStoreFixture(t)
	for _, variation := range []string{"corrupt-store", "import", "import-temporary", "ready", "ready-temporary", "empty-store", "misplaced-store", "symlink", "hardlink", "store-file"} {
		stateDir := t.TempDir()
		state := filepath.Join(stateDir, "runtime", "validator-1", "state")
		operator := filepath.Join(state, "operators", "no-1")
		writeValidatorStateTestFile(t, filepath.Join(operator, "stats.json"), `{"assignments":1}`)
		store := filepath.Join(operator, "attempt-ledger.records")
		switch variation {
		case "corrupt-store":
			installValidatorNamespaceStoreFixture(t, store, fixture)
			writeValidatorStateTestFile(t, filepath.Join(store, "CURRENT"), "corrupt retained fixture\n")
		case "import", "import-temporary", "ready", "ready-temporary":
			name := "attempt-ledger-import.json"
			if strings.HasPrefix(variation, "ready") {
				name = "attempt-ledger-ready.json"
			}
			if strings.HasSuffix(variation, "temporary") {
				name += ".tmp"
			}
			writeValidatorStateTestFile(t, filepath.Join(operator, name), "{")
		case "empty-store":
			if err := os.Mkdir(store, 0o700); err != nil {
				t.Fatal(err)
			}
		case "misplaced-store":
			installValidatorNamespaceStoreFixture(t, filepath.Join(state, "unknown", "attempt-ledger.records"), fixture)
		case "symlink":
			outside := filepath.Join(stateDir, "outside-store")
			installValidatorNamespaceStoreFixture(t, outside, fixture)
			if err := os.Symlink(outside, store); err != nil {
				t.Fatal(err)
			}
		case "hardlink":
			outside := filepath.Join(stateDir, "outside-file")
			writeValidatorStateTestFile(t, outside, "retained outside fixture")
			if err := os.Link(outside, filepath.Join(operator, "attempt-ledger-ready.json")); err != nil {
				t.Fatal(err)
			}
		case "store-file":
			writeValidatorStateTestFile(t, store, "wrong storage type")
		}
		before := validatorNamespaceTreeSnapshot(t, stateDir)
		if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err == nil {
			t.Fatalf("%s was accepted as resettable unsigned state", variation)
		}
		if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
			t.Fatalf("%s refusal changed preserved state", variation)
		}
	}
}

// Recheck v1 authority too: an exact source hash does not authorize resetting
// signed state when an earlier or damaged journal names that source.
func TestValidatorStateNamespacePendingResetProtectsSignedV1(t *testing.T) {
	fixture := readValidatorNamespaceStoreFixture(t)
	stateDir := t.TempDir()
	state := filepath.Join(stateDir, "runtime", "validator-1", "state")
	for noID := 1; noID <= 2; noID++ {
		writeValidatorStateTestFile(t, filepath.Join(state, "operators", fmt.Sprintf("no-%d", noID), "attempt-ledger.jsonl"), string(fixture.RecordsJSONL))
	}
	prepareValidatorNamespacePendingJournal(t, stateDir)
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err == nil {
		t.Fatal("pending reset journal archived signed v1 authority")
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
		t.Fatal("signed-v1 recovery refusal changed preserved state")
	}
}

// Ordinary legacy archival retains every credential and byte; complete v1
// namespaces still rerender without mutation after a completed reset.
func TestValidatorStateNamespacePreservesLegacyAndSignedControls(t *testing.T) {
	fixture := readValidatorNamespaceStoreFixture(t)
	stateDir := t.TempDir()
	root := filepath.Join(stateDir, "runtime", "validator-1")
	state := filepath.Join(root, "state")
	writeValidatorStateTestFile(t, filepath.Join(state, "operators", "no-1", "stats.json"), `{"assignments":1}`)
	writeValidatorStateTestFile(t, filepath.Join(state, "operators", "no-1", "client.key"), "retained fixture credential")
	writeValidatorStateTestFile(t, filepath.Join(state, "operators", "no-2", "proofs.jsonl"), string(fixture.RecordsJSONL))
	before := validatorNamespaceTreeSnapshot(t, state)
	if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err != nil {
		t.Fatal(err)
	}
	var journal validatorAttemptStateResetJournal
	if err := decodeStrictJSONFile(filepath.Join(root, validatorAttemptStateResetJournalName), &journal); err != nil || journal.Status != "complete" {
		t.Fatalf("legacy archive journal: %+v/%v", journal, err)
	}
	archive := filepath.Join(root, journal.ArchiveName)
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, archive)) {
		t.Fatal("legacy archive lost or changed fixture artifacts")
	}
	for noID := 1; noID <= 2; noID++ {
		writeValidatorStateTestFile(t, filepath.Join(state, "operators", fmt.Sprintf("no-%d", noID), "attempt-ledger.jsonl"), string(fixture.RecordsJSONL))
	}
	complete := validatorNamespaceTreeSnapshot(t, stateDir)
	if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(complete, validatorNamespaceTreeSnapshot(t, stateDir)) {
		t.Fatal("signed namespace rerender changed current or archived state")
	}
}

// A signed ledger outside the configured operator census is ambiguous, not
// permission to erase an otherwise unrecognized source or recovery archive.
func TestValidatorStateNamespaceRejectsUnmappedSignedLedger(t *testing.T) {
	fixture := readValidatorNamespaceStoreFixture(t)
	for _, misplaced := range []bool{false, true} {
		stateDir := t.TempDir()
		state := filepath.Join(stateDir, "runtime", "validator-1", "state")
		path := filepath.Join(state, "operators", "no-3", "attempt-ledger.jsonl")
		if misplaced {
			path = filepath.Join(state, "unmapped", "attempt-ledger.jsonl")
		}
		writeValidatorStateTestFile(t, path, string(fixture.RecordsJSONL))
		before := validatorNamespaceTreeSnapshot(t, stateDir)
		if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err == nil {
			t.Fatalf("misplaced=%t: unmapped signed ledger was treated as unsigned", misplaced)
		}
		if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
			t.Fatalf("misplaced=%t: unmapped ledger refusal changed retained artifacts", misplaced)
		}
	}
}

// A later protected namespace or recovery archive must be found before an
// earlier validator's legacy state is renamed or its journal is created.
func TestValidatorStateNamespacePreflightsEveryConfiguredValidator(t *testing.T) {
	fixture := readValidatorNamespaceStoreFixture(t)
	for _, variation := range []string{"current", "pending-archive", "complete-archive"} {
		stateDir := t.TempDir()
		first := filepath.Join(stateDir, "runtime", "validator-1", "state")
		writeValidatorStateTestFile(t, filepath.Join(first, "operators", "no-1", "stats.json"), `{"assignments":1}`)
		secondRoot := filepath.Join(stateDir, "runtime", "validator-2")
		second := filepath.Join(secondRoot, "state")
		installValidatorNamespaceStoreFixture(t, filepath.Join(second, "operators", "no-1", "attempt-ledger.records"), fixture)
		if variation != "current" {
			hash, count, err := hashValidatorStateTree(second)
			if err != nil {
				t.Fatal(err)
			}
			journal := validatorAttemptStateResetJournal{
				Schema: validatorAttemptStateResetJournalSchema, DeploymentID: "deployment",
				ValidatorID: 2, Operators: 2, SourceHash: hash, FileCount: count,
				ArchiveName: validatorLegacyArchiveName(hash), Status: strings.TrimSuffix(variation, "-archive"),
			}
			if err := os.Rename(second, filepath.Join(secondRoot, journal.ArchiveName)); err != nil {
				t.Fatal(err)
			}
			if err := persistValidatorAttemptStateResetJournal(filepath.Join(secondRoot, validatorAttemptStateResetJournalName), &journal); err != nil {
				t.Fatal(err)
			}
		}
		cfg := &ResolvedConfig{Config: &HarnessConfig{
			Deployment: DeploymentConfig{DeploymentID: "deployment"},
			Topology:   TopologyConfig{Validators: 2, Operators: 2},
		}}
		before := validatorNamespaceTreeSnapshot(t, stateDir)
		if err := prepareSignedAttemptStateNamespaces(cfg, stateDir); err == nil {
			t.Fatalf("%s: configured disk namespace was accepted for legacy reset", variation)
		}
		if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
			t.Fatalf("%s: earlier validator changed before later protected authority was rejected", variation)
		}
	}
}
