//go:build linux || darwin

// Actual signed producer databases, detached replay and deterministic path
// mutations exercise the liability boundary without a source/verdict mock.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// Replay only committed controls first, preserving the live producer's exact
// ledger and import receipts for the separate unsealed observation.
func productionBootstrapUnsealedTestFixture(t *testing.T) (releaseArchiveV2TestFixture, *ReleaseEvidenceV2Archive, *ProductionBootstrapCommittedObservation) {
	t.Helper()
	f, observed, approved, _ := productionBootstrapCommittedTestFixture(t)
	history, err := readReleaseEvidenceV2HistoryFilesForUid(t.Context(), f.options.Config.StateDir, f.options.Config.EvidenceV2.Bounds, uint32(os.Geteuid()), 8192, true, releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	options, err := captureProductionBootstrapCommitted(t.Context(), f.options.Config, observed, history, f.options.ScratchRoot)
	if err := errors.Join(err, history.close()); err != nil {
		t.Fatal(err)
	}
	archiveCtx, cancel := context.WithCancel(context.Background())
	archive, err := openReleaseEvidenceV2ArchiveHistory(archiveCtx, options)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer cancel()
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	})
	current, err := projectProductionBootstrapCommitted(t.Context(), archive, options.Sources, observed, approved, nil, uint32(os.Geteuid()))
	if err != nil {
		t.Fatal(err)
	}
	return f, archive, current
}

// A real new server assignment becomes an unfinished signed producer record.
// A temporary engine creates its signature; the actual disk owner appends it.
func productionBootstrapUnsealedTestPending(t *testing.T, f releaseArchiveV2TestFixture, index int) AttemptRecord {
	t.Helper()
	server, key := f.startup.servers[index], f.startup.inputs[index].PrivateKey
	engine, stats, _ := newTestEngine(t, server, key, server.validatorClientId, 8, nil)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, newAttemptLedgerDiskTestStateDir(t), &generation)
	t.Cleanup(func() { _ = ledger.Close() })
	if _, err := engine.RunTrail(t.Context()); err != nil {
		t.Fatal(err)
	}
	records, err := ledger.RecordsAfter(0)
	if err != nil || len(records) != 8 {
		t.Fatal("actual signed pending source", err)
	}
	pending := records[0]
	pending.Boundary = f.startup.boundary
	actual, err := f.startup.disk.participants[index].Ledger.Append(pending)
	if err != nil {
		t.Fatal(err)
	}
	return *actual
}

// Pending work beyond a complete cut is retained as liability, not silently
// discarded because it has no public stream manifest or completed proof.
func TestProductionBootstrapUnsealedRetainsPendingAndCompleteTail(t *testing.T) {
	f, archive, current := productionBootstrapUnsealedTestFixture(t)
	productionBootstrapUnsealedTestPending(t, f, 0)
	f.startup.trail(t, 1)
	before := map[string]map[string]string{}
	paths := []string{f.options.Config.StateDir}
	for _, operator := range f.options.Config.Operators {
		paths = append(paths, operator.StateDir)
	}
	for _, path := range paths {
		before[path] = releaseArchiveV2TestPrivateFiles(t, path)
	}
	got, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uint32(os.Geteuid()), releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if got.Ledgers[0].UnsealedRecords != 1 || got.Ledgers[0].PendingTrails != 1 || got.Ledgers[1].UnsealedRecords != 8 || got.Ledgers[1].PendingTrails != 0 || got.Ledgers[0].PendingHash == got.Ledgers[1].PendingHash || got.IntentFilePresent || got.IntentFileHash != "" {
		t.Fatal("unsealed tail or pending liability disappeared", got)
	}
	for _, path := range paths {
		if !reflect.DeepEqual(before[path], releaseArchiveV2TestPrivateFiles(t, path)) {
			t.Fatal("read-only unsealed observation mutated producer state", path)
		}
	}
}

// An append may finish a pending trail, but replay must prove the retained
// prior unsealed head as an actual signed ancestor, even when it is not a cut.
func TestProductionBootstrapUnsealedRetainedAncestryAndResolution(t *testing.T) {
	f, archive, current := productionBootstrapUnsealedTestFixture(t)
	pending := productionBootstrapUnsealedTestPending(t, f, 0)
	first, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uint32(os.Geteuid()), releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	previous := *current
	previous.Unsealed = first
	pending.Disposition = AttemptDispositionValidatorError
	if _, err := f.startup.disk.participants[0].Ledger.Append(pending); err != nil {
		t.Fatal(err)
	}
	got, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, &previous, f.options.ScratchRoot, uint32(os.Geteuid()), releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if got.Ledgers[0].Head.LastSequence != first.Ledgers[0].Head.LastSequence+1 || got.Ledgers[0].PendingTrails != 0 {
		t.Fatal("signed resolution lost lifetime head or retained stale pending work")
	}
	first.Ledgers[0].Head.Root = "0x" + strings.Repeat("a", 64)
	if got, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, &previous, f.options.ScratchRoot, uint32(os.Geteuid()), releaseMeasurementInputV2ReadHooks{}); got != nil || owner != nil || err == nil || !strings.Contains(err.Error(), "ancestor") {
		t.Fatal("rewritten prior tail was accepted", err)
	}
}

// An explicitly encoded empty file is distinct from absence. Nonempty or
// malformed content refuses before any ledger snapshot is constructed.
func TestProductionBootstrapUnsealedIntentBoundary(t *testing.T) {
	f, archive, current := productionBootstrapUnsealedTestFixture(t)
	path := filepath.Join(f.options.Config.StateDir, "steering-intents.json")
	empty, err := marshalAttemptSettlementV2JSON(t.Context(), &steeringIntentFile{Schema: steeringIntentSchema}, f.options.Config.EvidenceV2.Bounds.IntentFileLimit(), true, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{empty, []byte("{}\n"), []byte(""), []byte(`{"schema":"urnetwork-validator-steering-intent-v6","current":{},"history":null}` + "\n")} {
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		got, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uint32(os.Geteuid()), releaseMeasurementInputV2ReadHooks{})
		if bytes.Equal(raw, empty) {
			if err != nil || !got.IntentFilePresent || got.IntentFileHash != ReleaseMeasurementContentHash(empty) {
				t.Fatal("explicit empty boundary", err)
			}
			if err := owner.close(); err != nil {
				t.Fatal(err)
			}
		} else if err == nil || got != nil || owner != nil {
			t.Fatal("unreplayed steering intent liability admitted", string(raw), err)
		}
	}
}

// Canonical encoding cannot turn occupied current or historical intent slots
// into the supported empty boundary. Refusal precedes ledger copy/replay.
func TestProductionBootstrapUnsealedRejectsCanonicalNonemptyIntent(t *testing.T) {
	f, archive, current := productionBootstrapUnsealedTestFixture(t)
	for _, file := range []steeringIntentFile{
		{Schema: steeringIntentSchema, Current: &SteeringIntent{Schema: steeringIntentSchema, Status: "pending"}},
		{Schema: steeringIntentSchema, Current: &SteeringIntent{Schema: steeringIntentSchema, Status: "applied"}},
		{Schema: steeringIntentSchema, History: []SteeringIntent{{Schema: steeringIntentSchema, Status: "failed"}}},
	} {
		raw, err := marshalAttemptSettlementV2JSON(t.Context(), &file, f.options.Config.EvidenceV2.Bounds.IntentFileLimit(), true, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(f.options.Config.StateDir, "steering-intents.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadDir(f.options.ScratchRoot)
		if err != nil {
			t.Fatal(err)
		}
		got, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uint32(os.Geteuid()), releaseMeasurementInputV2ReadHooks{})
		if owner != nil {
			_ = owner.close()
		}
		if err == nil || got != nil || owner != nil || !strings.Contains(err.Error(), "nonempty or noncanonical steering intent liability") {
			t.Fatal("canonical occupied intent escaped the empty-boundary guard", err)
		}
		after, err := os.ReadDir(f.options.ScratchRoot)
		if err != nil || len(after) != len(before) {
			t.Fatal("occupied intent reached ledger copying before refusal", err)
		}
	}
}

// Removing even an empty retained intent file cannot reset its durable
// observed boundary to a pristine, never-observed state.
func TestProductionBootstrapUnsealedIntentCannotDisappear(t *testing.T) {
	f, archive, current := productionBootstrapUnsealedTestFixture(t)
	path := filepath.Join(f.options.Config.StateDir, "steering-intents.json")
	raw, err := marshalAttemptSettlementV2JSON(t.Context(), &steeringIntentFile{Schema: steeringIntentSchema}, f.options.Config.EvidenceV2.Bounds.IntentFileLimit(), true, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	prior, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uint32(os.Geteuid()), releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	previous := *current
	previous.Unsealed = prior
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, &previous, f.options.ScratchRoot, uint32(os.Geteuid()), releaseMeasurementInputV2ReadHooks{}); err == nil || got != nil || owner != nil || !strings.Contains(err.Error(), "retained empty intent") {
		t.Fatal("intent deletion reset observed custody", err)
	}
}

// Source replacement, extra aliases, removal and late publication are forced
// after the held owner completes replay, including after its actual Close.
func TestProductionBootstrapUnsealedClosingCustody(t *testing.T) {
	for _, fault := range []string{"append", "intent", "links", "remove", "close", "later-close"} {
		f, archive, current := productionBootstrapUnsealedTestFixture(t)
		armed, changed := false, false
		cause := errors.New("synthetic unsealed close failure")
		hooks := releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
			if armed && !changed && fault == "later-close" && file.Name() == filepath.Join(f.options.Config.Operators[1].StateDir, attemptLedgerStoreName) {
				changed = true
				return os.WriteFile(filepath.Join(f.options.Config.StateDir, "steering-intents.json"), []byte("{}\n"), 0o600)
			}
			if armed && !changed && fault == "close" {
				changed = true
				if _, err := file.Stat(); err == nil {
					t.Fatal("observer preceded actual Close")
				}
				return cause
			}
			return nil
		}}
		_, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uint32(os.Geteuid()), hooks)
		if err != nil {
			t.Fatal("positive held census", err)
		}
		armed = true
		path := filepath.Join(f.options.Config.Operators[0].StateDir, attemptLedgerReadyName)
		switch fault {
		case "append":
			productionBootstrapUnsealedTestPending(t, f, 0)
		case "intent":
			err = os.WriteFile(filepath.Join(f.options.Config.StateDir, "steering-intents.json"), []byte("{}\n"), 0o600)
		case "links":
			err = os.Link(path, filepath.Join(f.options.Config.Operators[0].StateDir, "alias"))
		case "remove":
			err = os.Remove(path)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := owner.close(); err == nil || fault == "close" && !errors.Is(err, cause) {
			t.Fatal("closing source contradiction admitted", fault, err)
		}
		if fault == "later-close" && !changed {
			t.Fatal("later owner did not mutate the already-closed intent boundary")
		}
	}
}

// A real signed/VPK-valid record can still have a forged server assignment.
// Changing it and every local index does not promote it to trusted evidence.
func TestProductionBootstrapUnsealedRejectsForgedServerTail(t *testing.T) {
	f, archive, current := productionBootstrapUnsealedTestFixture(t)
	pending := productionBootstrapUnsealedTestPending(t, f, 0)
	if err := f.startup.disk.close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.options.Config.Operators[0].StateDir, attemptLedgerStoreName)
	store, err := openAttemptRecordStore(t.Context(), path, pending.Identity, strings.ToLower(f.options.Config.Coordinator), f.startup.inputs[0].PrivateKey.Public().(ed25519.PublicKey), attemptRecordStoreTestBounds())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	db := store.db
	pending.Assignments[0].AssignSignature[0] ^= 1
	pending = resignAttemptRecordStoreTest(t, pending, f.startup.inputs[0].PrivateKey)
	encoded, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := db.Get([]byte("head"), nil)
	var head AttemptLedgerHead
	if err != nil || json.Unmarshal(raw, &head) != nil {
		t.Fatal(err)
	}
	head.Root = pending.RecordHash
	headBytes, _ := json.Marshal(head)
	trailBytes, _ := json.Marshal(attemptRecordStoreTrail{LastSequence: pending.Sequence, RecordHash: pending.RecordHash})
	batch := new(leveldb.Batch)
	batch.Put(attemptStoreRecordKey(pending.Sequence), encoded)
	batch.Put(attemptStoreTrailRecordKey(pending.TrailID, pending.Sequence), []byte(pending.RecordHash))
	batch.Put(attemptStoreTrailKey(pending.TrailID), trailBytes)
	batch.Put([]byte("head"), headBytes)
	if err := errors.Join(db.Write(batch, &opt.WriteOptions{Sync: true}), store.Close()); err != nil {
		t.Fatal(err)
	}
	if err := verifyAttemptRecord(&pending, pending.Identity, f.startup.inputs[0].PrivateKey.Public().(ed25519.PublicKey), nil, false); err != nil {
		t.Fatal("forged fixture must retain valid local VPK authority", err)
	}
	if got, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uint32(os.Geteuid()), releaseMeasurementInputV2ReadHooks{}); err == nil || got != nil || owner != nil || !strings.Contains(err.Error(), "server signature") {
		t.Fatal("forged server assignment admitted", err)
	}
}

// Cancellation does not suppress an actual later close failure or return a
// successful partial inventory. No sleeps select the transition.
func TestProductionBootstrapUnsealedCancellationJoinsClose(t *testing.T) {
	f, archive, current := productionBootstrapUnsealedTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	armed := false
	cause := errors.New("synthetic cancelled close")
	hooks := releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
		if armed {
			return cause
		}
		return nil
	}}
	_, owner, err := readProductionBootstrapUnsealed(ctx, f.options.Config, archive, current, nil, f.options.ScratchRoot, uint32(os.Geteuid()), hooks)
	if err != nil {
		t.Fatal(err)
	}
	armed = true
	cancel()
	if err := owner.close(); !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
		t.Fatal("cancel/close cause disappeared", err)
	}
}

// Neither missing state nor incomplete migration receipts can stand in for
// an original empty ledger. Aliases and owner mismatches fail at acquisition.
func TestProductionBootstrapUnsealedRejectsIncompleteSource(t *testing.T) {
	for _, fault := range []string{"ledger", "ready", "marker", "legacy", "uid", "symlink", "unknown", "mode"} {
		f, archive, current := productionBootstrapUnsealedTestFixture(t)
		root := f.options.Config.Operators[0].StateDir
		uid := uint32(os.Geteuid())
		var err error
		switch fault {
		case "ledger":
			err = os.Rename(filepath.Join(root, attemptLedgerStoreName), filepath.Join(root, "retained-ledger"))
		case "ready":
			err = os.Remove(filepath.Join(root, attemptLedgerReadyName))
		case "marker":
			err = os.WriteFile(filepath.Join(root, attemptLedgerImportName), []byte("{}"), 0o600)
		case "legacy":
			err = os.WriteFile(filepath.Join(root, attemptLedgerLegacyName), []byte{}, 0o600)
		case "uid":
			uid ^= 1
		case "symlink":
			path := filepath.Join(root, attemptLedgerReadyName)
			if err := os.Rename(path, path+"-original"); err != nil {
				t.Fatal(err)
			}
			err = os.Symlink(path+"-original", path)
		case "unknown":
			err = os.WriteFile(filepath.Join(root, attemptLedgerStoreName, "unexpected"), []byte("synthetic"), 0o600)
		case "mode":
			err = os.Chmod(filepath.Join(root, attemptLedgerReadyName), 0o640)
		}
		if err != nil {
			t.Fatal(err)
		}
		got, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uid, releaseMeasurementInputV2ReadHooks{})
		if err == nil || got != nil || owner != nil {
			t.Fatal("unproven local boundary admitted", fault, err)
		}
		if fault == "ledger" {
			if err := os.Rename(filepath.Join(root, "retained-ledger"), filepath.Join(root, attemptLedgerStoreName)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Both entry and byte bounds apply to real immutable copy input. Cancellation
// and byte exhaustion never authorize treating an occupied file as absent.
func TestProductionBootstrapUnsealedSourceLimits(t *testing.T) {
	root := productionBootstrapCommittedTestDirectory(t)
	path := filepath.Join(root, "source")
	if err := os.WriteFile(path, []byte("bounded"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"bytes", "objects", "cancel"} {
		ctx, cancel := context.WithCancel(t.Context())
		owner := &productionBootstrapUnsealedOwner{ctx: ctx, uid: uint32(os.Geteuid()), remaining: 64}
		switch fault {
		case "bytes":
			owner.remaining = 1
		case "objects":
			owner.sources = make([]ReleaseEvidenceV2ArchiveSource, productionBootstrapCommittedMaximumObjects)
		case "cancel":
			cancel()
		}
		if raw, err := owner.read(path, "source", 64, true); err == nil || raw != nil {
			t.Fatal("bounded source admitted", fault, err)
		}
		_ = owner.close()
		cancel()
	}
}

// A retained owner catches an actual source inode change scheduled after the
// read descriptor's Close, even when replacement bytes are exactly the same.
func TestProductionBootstrapUnsealedSameBytesReplacement(t *testing.T) {
	root := productionBootstrapCommittedTestDirectory(t)
	path := filepath.Join(root, "source")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed := false
	owner := &productionBootstrapUnsealedOwner{ctx: t.Context(), uid: uint32(os.Geteuid()), remaining: 64, hooks: releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
		if !changed && file.Name() == path {
			changed = true
			if _, err := file.Stat(); err == nil {
				t.Fatal("replacement preceded actual Close")
			}
			return atomicStateWrite(path, []byte("original"), 0o600)
		}
		return nil
	}}}
	raw, err := owner.read(path, "source", 64, false)
	closeErr := owner.close()
	if !changed || raw != nil || errors.Join(err, closeErr) == nil {
		t.Fatal("same-byte replacement erased physical custody", changed, err, closeErr)
	}
}

// Root provisions only synthetic fixture namespaces after their writer joins.
// No production state or ancestor permission is changed by these tests.
func productionBootstrapUnsealedTestChown(t *testing.T, f releaseArchiveV2TestFixture, uid uint32) []string {
	t.Helper()
	if err := f.startup.disk.close(); err != nil {
		t.Fatal(err)
	}
	paths := []string{f.options.Config.StateDir}
	for _, operator := range f.options.Config.Operators {
		paths = append(paths, operator.StateDir)
	}
	for _, root := range paths {
		if err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Chown(path, int(uid), -1)
		}); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

// Root provisions only this fixture; admission must read a genuinely foreign
// service uid without chown, permissions repair or opening the source ledger.
func TestProductionBootstrapUnsealedForeignUidReadOnly(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires isolated root-owned fixture provisioning")
	}
	f, archive, current := productionBootstrapUnsealedTestFixture(t)
	productionBootstrapUnsealedTestPending(t, f, 0)
	const uid = uint32(65534)
	paths := productionBootstrapUnsealedTestChown(t, f, uid)
	got, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uid, releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if got.Ledgers[0].PendingTrails != 1 {
		t.Fatal("foreign owner lost pending liability")
	}
	for _, root := range paths {
		directory, err := openAttemptPrivateDirectory(root)
		if err != nil {
			t.Fatal(err)
		}
		if directory.anchor.uid != uid || directory.anchor.mode&0o077 != 0 {
			t.Fatal("read-only observer changed service ownership")
		}
		if err := directory.close(); err != nil {
			t.Fatal(err)
		}
	}
}

// A correct foreign service parent does not authorize an individual root-owned
// intent/import leaf. Root's ability to read it must not bypass leaf custody.
func TestProductionBootstrapUnsealedRejectsForeignUidLeaf(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires isolated root-owned fixture provisioning")
	}
	f, archive, current := productionBootstrapUnsealedTestFixture(t)
	intentPath := filepath.Join(f.options.Config.StateDir, "steering-intents.json")
	raw, err := marshalAttemptSettlementV2JSON(t.Context(), &steeringIntentFile{Schema: steeringIntentSchema}, f.options.Config.EvidenceV2.Bounds.IntentFileLimit(), true, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(intentPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	const uid = uint32(65534)
	productionBootstrapUnsealedTestChown(t, f, uid)
	if _, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uid, releaseMeasurementInputV2ReadHooks{}); err != nil {
		t.Fatal("positive foreign-owner boundary", err)
	} else if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{intentPath, filepath.Join(f.options.Config.Operators[0].StateDir, attemptLedgerImportName), filepath.Join(f.options.Config.Operators[0].StateDir, attemptLedgerReadyName)} {
		if err := os.Chown(path, 0, -1); err != nil {
			t.Fatal(err)
		}
		got, owner, readErr := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uid, releaseMeasurementInputV2ReadHooks{})
		if owner != nil {
			_ = owner.close()
		}
		directory, err := openAttemptPrivateDirectory(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		state, err := directory.stat(filepath.Base(path))
		if err := errors.Join(err, directory.close()); err != nil {
			t.Fatal(err)
		}
		if state.uid != 0 {
			t.Fatal("read-only admission repaired the wrong leaf owner")
		}
		if err := os.Chown(path, int(uid), -1); err != nil {
			t.Fatal(err)
		}
		if readErr == nil || got != nil || owner != nil || !strings.Contains(readErr.Error(), "unsealed source ownership") {
			t.Fatal("foreign service directory hid an individual wrong-owner leaf", filepath.Base(path), readErr)
		}
	}
}

// Validator/server signatures cannot authorize a later epoch or finalized
// block than the current observation, or a different hash at that same block.
func TestProductionBootstrapUnsealedRejectsTailBoundary(t *testing.T) {
	for _, fault := range []string{"epoch", "future", "hash"} {
		f, archive, current := productionBootstrapUnsealedTestFixture(t)
		switch fault {
		case "epoch":
			f.startup.boundary.SettlementEpoch++
		case "future":
			f.startup.boundary.EVMBlock++
		case "hash":
			f.startup.boundary.EVMBlockHash = "0x" + strings.Repeat("f", 64)
		}
		productionBootstrapUnsealedTestPending(t, f, 0)
		if got, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uint32(os.Geteuid()), releaseMeasurementInputV2ReadHooks{}); got != nil || owner != nil || err == nil || !strings.Contains(err.Error(), "boundary") {
			t.Fatal("signed tail escaped the authenticated current ceiling", fault, err)
		}
	}
}

// The generic database reader may recover CURRENT from a backup. Admission
// uses the strict producer storage selector and refuses that same ambiguity.
func TestProductionBootstrapUnsealedRejectsMissingCurrentWithBackup(t *testing.T) {
	f, archive, current := productionBootstrapUnsealedTestFixture(t)
	if err := f.startup.disk.close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.options.Config.Operators[0].StateDir, attemptLedgerStoreName)
	if err := os.Rename(filepath.Join(path, "CURRENT"), filepath.Join(path, "CURRENT.bak")); err != nil {
		t.Fatal(err)
	}
	if got, owner, err := readProductionBootstrapUnsealed(t.Context(), f.options.Config, archive, current, nil, f.options.ScratchRoot, uint32(os.Geteuid()), releaseMeasurementInputV2ReadHooks{}); got != nil || owner != nil || err == nil || !strings.Contains(err.Error(), "CURRENT") {
		t.Fatal("missing selector was repaired into authority", err)
	}
	if _, err := os.Lstat(filepath.Join(path, "CURRENT")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only observer repaired producer storage", err)
	}
}
