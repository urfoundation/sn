//go:build linux || darwin

// Sealed history segments over the fixture's real measurement, through the
// store's own seal, exchange and readers.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A growing synthetic history over the fixture's one real measurement. Each
// begin moves the current intent into the history as failed and makes a new
// pending vector current, as beginV2 does once it has verified the new intent,
// through the store's own seal and exchange. The memo trusts the whole logical
// history, as after a complete read, so every read takes the reused path.
type intentV2SegmentTestHistory struct {
	t       *testing.T
	store   *IntentStore
	intent  SteeringIntent
	options ReleaseMeasurementV2Options
	all     []SteeringIntent
	// When set, begins seal against this limit instead of the store's own.
	sealLimit uint64
}

func newIntentV2SegmentTestHistory(t *testing.T) *intentV2SegmentTestHistory {
	t.Helper()
	fixture := newProductionContinuationTestFixture(t)
	return newIntentV2SegmentTestHistoryFor(t, fixture.steerer.intents, fixture.intent)
}

func newIntentV2SegmentTestHistoryFor(t *testing.T, store *IntentStore, signed *SteeringIntent) *intentV2SegmentTestHistory {
	t.Helper()
	intent, err := store.beginV2(t.Context(), *signed)
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
	return &intentV2SegmentTestHistory{t: t, store: store, intent: *intent, options: options, all: []SteeringIntent{*intent}}
}

func (self *intentV2SegmentTestHistory) limit() uint64 {
	return self.store.v2.runtime.cfg.EvidenceV2.Bounds.IntentFileLimit()
}

// The memo trusts exactly the expected logical history.
func (self *intentV2SegmentTestHistory) trust() [][32]byte {
	self.t.Helper()
	all := make([]*SteeringIntent, len(self.all))
	for index := range self.all {
		all[index] = &self.all[index]
	}
	digests, digestible := releaseIntentV2EntryDigests(all)
	if !digestible {
		self.t.Fatal("the synthetic history has no digest chain")
	}
	self.store.v2.verified = &releaseIntentV2VerifiedHistory{authority: self.store.v2.verifiedAuthority(), digests: digests, options: map[int]ReleaseMeasurementV2Options{len(self.all) - 1: self.options}}
	return digests
}

// Appends one entry as begin does, stopping before the exchange when publish
// is false, as a crash there would.
func (self *intentV2SegmentTestHistory) beginThrough(publish bool) error {
	self.t.Helper()
	self.trust()
	read, err := self.store.readV2(self.t.Context())
	if err != nil {
		return err
	}
	failed := *read.file.Current
	failed.Status, failed.Error = "failed", fmt.Sprintf("synthetic retained failure %d", len(self.all))
	successor := self.intent
	successor.DepositAudits = slices.Clone(self.intent.DepositAudits)
	successor.DepositAudits[0].UsageBytes += uint64(len(self.all))
	if successor.VectorHash, err = successor.computeVectorHash(); err != nil {
		self.t.Fatal(err)
	}
	read.file.History = append(read.file.History, failed)
	read.file.Current = &successor
	sealLimit := self.limit()
	if self.sealLimit != 0 {
		sealLimit = self.sealLimit
	}
	err = self.store.sealHistoryV2(self.t.Context(), read, sealLimit)
	if err == nil && publish {
		err = self.store.writeV2(self.t.Context(), read)
	}
	if err := errors.Join(err, read.custody.close()); err != nil || !publish {
		return err
	}
	self.all[len(self.all)-1] = failed
	self.all = append(self.all, successor)
	return nil
}

func (self *intentV2SegmentTestHistory) begin() {
	self.t.Helper()
	if err := self.beginThrough(true); err != nil {
		self.t.Fatalf("synthetic begin %d: %v", len(self.all), err)
	}
}

// The live file as published.
func (self *intentV2SegmentTestHistory) live() *steeringIntentFile {
	self.t.Helper()
	raw, err := os.ReadFile(self.store.path)
	if err != nil {
		self.t.Fatal(err)
	}
	file, err := decodeSteeringIntentFileV2(self.t.Context(), raw, self.limit())
	if err != nil {
		self.t.Fatal(err)
	}
	return file
}

// Whether the next begin seals, by the documented rule: the live file it
// would publish passes half its limit and keeps more history than the live
// tail. Nothing is written.
func (self *intentV2SegmentTestHistory) wouldSeal() bool {
	self.t.Helper()
	file := *self.live()
	failed := *file.Current
	failed.Status, failed.Error = "failed", "synthetic retained failure"
	file.History = append(slices.Clone(file.History), failed)
	if len(file.History) <= steeringIntentLiveHistory {
		return false
	}
	size, err := countAttemptSettlementV2JSON(self.t.Context(), &file, self.limit(), true, true)
	return err != nil || size > self.limit()/2
}

// Begins until the next begin would seal, then returns the live file.
func (self *intentV2SegmentTestHistory) growToSeal() *steeringIntentFile {
	self.t.Helper()
	for range 256 {
		if self.wouldSeal() {
			return self.live()
		}
		self.begin()
	}
	self.t.Fatal("the synthetic history never reached its seal")
	return nil
}

// A fresh store's read, as at startup, of the logical history.
func (self *intentV2SegmentTestHistory) freshRead() ([]SteeringIntent, [][32]byte, error) {
	self.t.Helper()
	owner, err := newReleaseIntentStoreV2(self.store.v2.runtime)
	if err != nil {
		self.t.Fatal(err)
	}
	startup := newReleaseStartupVerified()
	startup.intents = self.trust()
	read, err := owner.readV2(withReleaseStartupVerified(self.t.Context(), startup))
	if err != nil {
		return nil, nil, err
	}
	all := make([]SteeringIntent, len(read.all))
	for index, intent := range read.all {
		all[index] = *intent
	}
	return all, read.digests, read.custody.close()
}

// The segment names in the state directory, besides any temporaries.
func intentV2SegmentTestNames(t *testing.T, stateDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(stateDir, steeringIntentSegmentDirectory))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// A begin seals once the live file it would publish passes half its limit:
// all but the last eight history entries move, in order, into one segment
// that the live file pins by its content hash, size, entry count and the
// digest chain through its last entry, and the current intent stays live. The
// segment is the canonical census of exactly those entries, so the same
// published entries always seal to the same bytes and name.
func TestIntentV2HistorySealsDeterministically(t *testing.T) {
	history := newIntentV2SegmentTestHistory(t)
	before := history.growToSeal()
	if len(before.Segments) != 0 || before.Schema != steeringIntentSchema {
		t.Fatal("the history sealed before its live file passed half its limit")
	}
	history.begin()
	live := history.live()
	if len(live.Segments) != 1 || live.Schema != SteeringIntentSealedSchema || len(live.History) != steeringIntentLiveHistory || live.Current == nil || live.Current.VectorHash != history.all[len(history.all)-1].VectorHash {
		t.Fatalf("the seal left %d live history entries and %d segments", len(live.History), len(live.Segments))
	}
	pin := live.Segments[0]
	raw, err := os.ReadFile(filepath.Join(history.store.stateDir, filepath.FromSlash(steeringIntentSegmentName(pin.SHA256))))
	if err != nil {
		t.Fatal(err)
	}
	sealed := len(history.all) - 1 - steeringIntentLiveHistory
	want := steeringIntentSegmentFile{Schema: steeringIntentSegmentSchema, History: history.all[:sealed]}
	encoded, err := marshalAttemptSettlementV2JSON(t.Context(), &want, history.limit(), true, true)
	if err != nil || !bytes.Equal(raw, encoded) || pin.SHA256 != ReleaseMeasurementContentHash(raw) || pin.Bytes != uint64(len(raw)) || pin.Count != uint64(sealed) {
		t.Fatal("the segment is not the canonical census of the oldest entries")
	}
	if digests := history.trust(); pin.Head != attemptHex32(digests[sealed-1]) {
		t.Fatal("the segment head is not the digest chain through its last entry")
	}
	if names := intentV2SegmentTestNames(t, history.store.stateDir); len(names) != 1 || names[0] != filepath.Base(steeringIntentSegmentName(pin.SHA256)) {
		t.Fatalf("the seal left other files beside its segment: %v", names)
	}
}

// A crash after the segment is published but before the exchange leaves the
// previous live file, still holding every entry, and an unpinned segment. A
// fresh store reads the complete history from the live file alone, and the
// next seal of the same entries reuses the segment byte for byte.
func TestIntentV2HistorySealCrashBeforeTheExchangeKeepsEveryEntry(t *testing.T) {
	history := newIntentV2SegmentTestHistory(t)
	history.growToSeal()
	before, err := os.ReadFile(history.store.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := history.beginThrough(false); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(history.store.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("the live file changed before its exchange")
	}
	orphans := intentV2SegmentTestNames(t, history.store.stateDir)
	if len(orphans) != 1 {
		t.Fatalf("the interrupted seal published %d segments", len(orphans))
	}
	orphan, err := os.Stat(filepath.Join(history.store.stateDir, steeringIntentSegmentDirectory, orphans[0]))
	if err != nil {
		t.Fatal(err)
	}
	all, _, err := history.freshRead()
	if err != nil || len(all) != len(history.all) {
		t.Fatalf("the previous live file lost entries after the interrupted seal: %d of %d, %v", len(all), len(history.all), err)
	}
	history.begin()
	live := history.live()
	reused, err := os.Stat(filepath.Join(history.store.stateDir, steeringIntentSegmentDirectory, orphans[0]))
	if err != nil || len(live.Segments) != 1 || filepath.Base(steeringIntentSegmentName(live.Segments[0].SHA256)) != orphans[0] || !os.SameFile(orphan, reused) || len(intentV2SegmentTestNames(t, history.store.stateDir)) != 1 {
		t.Fatal("the retried seal did not pin the interrupted seal's segment as it was")
	}
}

// A failure inside writeV2, after its marker, leaves the previous live file,
// the marker and the published segment. Like any uncertain publication the
// store then refuses every read until the marker is resolved. Resolved toward
// the previous live file, the complete history reads again and the next seal
// reuses the segment.
func TestIntentV2HistorySealCrashInsideTheExchangeAwaitsRecovery(t *testing.T) {
	history := newIntentV2SegmentTestHistory(t)
	history.growToSeal()
	before, err := os.ReadFile(history.store.path)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(history.store.path)
	candidate, marker := filepath.Join(directory, releaseIntentV2Candidate), filepath.Join(directory, releaseIntentV2Marker)
	// The read checks for a pending publication first; an occupied candidate
	// created after that check fails the exchange once its marker is written.
	admitted := 0
	history.store.v2.referenceReadHooks.step = func(operation string, _ *os.File, _ int) error {
		if operation == "path-admitted" {
			if admitted++; admitted == 2 {
				return os.WriteFile(candidate, []byte("synthetic occupied candidate\n"), 0o600)
			}
		}
		return nil
	}
	err = history.beginThrough(true)
	history.store.v2.referenceReadHooks.step = nil
	if err == nil {
		t.Fatal("the exchange published over an occupied candidate")
	}
	after, err := os.ReadFile(history.store.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("the failed exchange changed the previous live file")
	}
	if _, err := os.Stat(marker); err != nil || len(intentV2SegmentTestNames(t, history.store.stateDir)) != 1 {
		t.Fatalf("the failed exchange lost its marker or its segment: %v", err)
	}
	if _, _, err := history.freshRead(); err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("a read passed the unresolved publication: %v", err)
	}
	if err := errors.Join(os.Remove(marker), os.Remove(candidate)); err != nil {
		t.Fatal(err)
	}
	all, _, err := history.freshRead()
	if err != nil || len(all) != len(history.all) {
		t.Fatalf("the resolved previous live file lost entries: %d of %d, %v", len(all), len(history.all), err)
	}
	if history.store, err = newReleaseIntentStoreV2(history.store.v2.runtime); err != nil {
		t.Fatal(err)
	}
	history.begin()
	if live := history.live(); len(live.Segments) != 1 || len(intentV2SegmentTestNames(t, history.store.stateDir)) != 1 {
		t.Fatal("the retried seal did not reuse the published segment")
	}
}

// Every reader refuses a sealed history whose segment is missing, or whose
// bytes, entry count or digest chain head differ from the live file's pin.
// Restored, the history reads again.
func TestIntentV2HistoryRefusesAMissingOrChangedSegment(t *testing.T) {
	history := newIntentV2SegmentTestHistory(t)
	history.growToSeal()
	history.begin()
	live := history.live()
	path := filepath.Join(history.store.stateDir, filepath.FromSlash(steeringIntentSegmentName(live.Segments[0].SHA256)))
	segment, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(history.store.path)
	if err != nil {
		t.Fatal(err)
	}
	repin := func(change func(*SteeringIntentSegment)) func() {
		return func() {
			file := *live
			file.Segments = slices.Clone(live.Segments)
			change(&file.Segments[0])
			encoded, err := marshalAttemptSettlementV2JSON(t.Context(), &file, history.limit(), true, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(history.store.path, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	read := func(name string, maximum uint64) ([]byte, error) {
		return os.ReadFile(filepath.Join(history.store.stateDir, filepath.FromSlash(name)))
	}
	for _, fault := range []struct {
		name  string
		apply func()
	}{
		{name: "missing", apply: func() {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "changed bytes", apply: func() {
			changed := bytes.Clone(segment)
			changed[len(changed)-3] ^= 1
			if err := os.WriteFile(path, changed, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "count", apply: repin(func(pin *SteeringIntentSegment) { pin.Count++ })},
		{name: "head", apply: repin(func(pin *SteeringIntentSegment) { pin.Head = attemptHex32([32]byte{0x51}) })},
	} {
		fault.apply()
		if _, _, err := history.freshRead(); err == nil {
			t.Errorf("%s: the intent store read a refused segment", fault.name)
		}
		encoded, err := os.ReadFile(history.store.path)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := ReadSteeringIntentHistory(t.Context(), encoded, history.limit(), read); err == nil {
			t.Errorf("%s: the shared history reader read a refused segment", fault.name)
		}
		if err := errors.Join(os.WriteFile(path, segment, 0o600), os.WriteFile(history.store.path, original, 0o600)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := history.freshRead(); err != nil {
		t.Fatalf("the restored history did not read: %v", err)
	}
}

// Sealing changes only where entries are stored. A fresh store reads the
// same logical history from the sealed and the unsealed file, entry for
// entry, with the same digest chain, so the verify-once memo and the startup
// verified record accept it alike, and the shared reader returns the same
// stored bytes for every entry.
func TestIntentV2SealedHistoryVerifiesAsTheUnsealedOne(t *testing.T) {
	history := newIntentV2SegmentTestHistory(t)
	history.growToSeal()
	history.begin()
	sealedFile, err := os.ReadFile(history.store.path)
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string, maximum uint64) ([]byte, error) {
		return os.ReadFile(filepath.Join(history.store.stateDir, filepath.FromSlash(name)))
	}
	sealedAll, sealedDigests, sealedErr := history.freshRead()
	_, sealedRaw, sealedRetained, rawErr := ReadSteeringIntentHistory(t.Context(), sealedFile, history.limit(), read)
	unsealed := steeringIntentFile{Schema: steeringIntentSchema, History: history.all[:len(history.all)-1], Current: &history.all[len(history.all)-1]}
	encoded, err := marshalAttemptSettlementV2JSON(t.Context(), &unsealed, history.limit(), true, true)
	if err := errors.Join(sealedErr, rawErr, err); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(history.store.path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	unsealedAll, unsealedDigests, err := history.freshRead()
	if err != nil {
		t.Fatal(err)
	}
	_, unsealedRaw, unsealedRetained, err := ReadSteeringIntentHistory(t.Context(), encoded, history.limit(), read)
	if err != nil {
		t.Fatal(err)
	}
	if len(sealedAll) != len(history.all) || len(unsealedAll) != len(sealedAll) || !slices.Equal(sealedDigests, unsealedDigests) || sealedRetained != unsealedRetained {
		t.Fatalf("the sealed history read %d entries, the unsealed %d", len(sealedAll), len(unsealedAll))
	}
	for index := range sealedAll {
		sealedEntry, sealedErr := json.Marshal(&sealedAll[index])
		unsealedEntry, unsealedErr := json.Marshal(&unsealedAll[index])
		if sealedErr != nil || unsealedErr != nil || !bytes.Equal(sealedEntry, unsealedEntry) || !bytes.Equal(sealedRaw[index], unsealedRaw[index]) {
			t.Fatalf("entry %d differs between the sealed and unsealed history", index)
		}
	}
	authority := history.store.v2.verifiedAuthority()
	memo := &releaseIntentV2VerifiedHistory{authority: authority, digests: unsealedDigests, options: map[int]ReleaseMeasurementV2Options{len(unsealedDigests) - 1: history.options}}
	startup := newReleaseStartupVerified()
	startup.intents = unsealedDigests
	if memo.reusablePrefix(authority, sealedDigests) != len(sealedDigests) || startup.intentPrefix(sealedDigests) != len(sealedDigests) {
		t.Fatal("the memo or startup verified record does not accept the sealed history as the unsealed one")
	}
}

// A sealed live file carries its own schema and a segments field, so a
// reader that knows no segments refuses it instead of taking its live entries
// for the whole history. The decoder also refuses either schema with the
// other's census.
func TestIntentV2SealedHistoryRefusesAnOlderReader(t *testing.T) {
	history := newIntentV2SegmentTestHistory(t)
	history.growToSeal()
	history.begin()
	sealed, err := os.ReadFile(history.store.path)
	if err != nil {
		t.Fatal(err)
	}
	var previous struct {
		Schema  string           `json:"schema"`
		Current *SteeringIntent  `json:"current,omitempty"`
		History []SteeringIntent `json:"history"`
	}
	if err := decodeAttemptStreamV2JSON(sealed, history.limit(), &previous); err == nil {
		t.Error("a strict reader without segments decoded a sealed file")
	}
	if err := json.Unmarshal(sealed, &previous); err != nil || previous.Schema == SteeringIntentSchema {
		t.Errorf("a sealed file carries the unsealed schema: %v", err)
	}
	legacy, err := NewIntentStore(history.store.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Current(); err == nil {
		t.Error("the legacy intent store read a sealed file")
	}
	live := history.live()
	for _, mixed := range []steeringIntentFile{
		{Schema: SteeringIntentSealedSchema, Current: live.Current, History: live.History},
		{Schema: steeringIntentSchema, Segments: live.Segments, Current: live.Current, History: live.History},
	} {
		encoded, err := marshalAttemptSettlementV2JSON(t.Context(), &mixed, history.limit(), true, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeSteeringIntentFileV2(context.Background(), encoded, history.limit()); err == nil {
			t.Errorf("schema %s with %d segments was admitted", mixed.Schema, len(mixed.Segments))
		}
	}
}

// Unsealed, this history would not fit the fixture's intent file limit, and
// the begin that passed it would fail and stop steering. Sealed, every begin
// succeeds, the live file stays within its limit, and a fresh store reads the
// whole history back with its complete digest chain.
func TestIntentV2LongHistoryStaysWithinTheFileLimit(t *testing.T) {
	history := newIntentV2SegmentTestHistory(t)
	for {
		unsealed := steeringIntentFile{Schema: steeringIntentSchema, History: history.all[:len(history.all)-1], Current: &history.all[len(history.all)-1]}
		if _, err := countAttemptSettlementV2JSON(t.Context(), &unsealed, history.limit(), true, true); err != nil && len(history.live().Segments) >= 2 {
			break
		}
		history.begin()
		if info, err := os.Stat(history.store.path); err != nil || uint64(info.Size()) > history.limit() {
			t.Fatalf("the live file passed its limit at %d entries: %v", len(history.all), err)
		}
		if len(history.all) > 512 {
			t.Fatal("the history never outgrew its file limit")
		}
	}
	all, digests, err := history.freshRead()
	if err != nil || len(all) != len(history.all) || !slices.Equal(digests, history.trust()) {
		t.Fatalf("the long sealed history did not read back whole: %d of %d, %v", len(all), len(history.all), err)
	}
}

// Startup's own pass over the retained history refuses a sealed history whose
// segment is missing, as it refuses any other missing retained reference,
// before it verifies an entry or reaches current preparation. The segment is
// sealed against a small limit only so that a short history seals at all.
func TestProductionStartupRefusesAMissingIntentSegment(t *testing.T) {
	fixture := newProductionStartupTestFixture(t)
	history := newIntentV2SegmentTestHistoryFor(t, fixture.continuation.steerer.intents, fixture.continuation.intent)
	history.sealLimit = 128 * 1024
	for len(history.live().Segments) == 0 {
		history.begin()
		if len(history.all) > 64 {
			t.Fatal("the short history never sealed")
		}
	}
	pin := history.live().Segments[0]
	if err := os.Remove(filepath.Join(history.store.stateDir, filepath.FromSlash(steeringIntentSegmentName(pin.SHA256)))); err != nil {
		t.Fatal(err)
	}
	fixture.closePreparation(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunRelease(fixture.storageContext(ctx), fixture.configPath) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "V2 intent segment 0") {
			t.Fatalf("startup did not refuse the missing segment: %v", err)
		}
	case <-fixture.evm.freshRead:
		cancel()
		t.Fatalf("startup reached current preparation over a missing segment: %v", <-done)
	case <-t.Context().Done():
		cancel()
		t.Fatalf("startup neither refused nor progressed: %v", <-done)
	}
}
