// Deterministic journal reuse tests observe semantic work, not elapsed time.
// Every source is synthetic and mutations are made at explicit read boundaries.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// An invocation-local record enables the reader without introducing authority
// from a real deployment or requiring a live chain in these filesystem tests.
func campaignJournalCheckpointFixture(t *testing.T) (*ResolvedConfig, string, *atomic.Int64) {
	t.Helper()
	dir := t.TempDir()
	calls := &atomic.Int64{}
	cfg := &ResolvedConfig{
		Config:  &HarnessConfig{Deployment: DeploymentConfig{DeploymentID: "synthetic-prefix-deployment"}},
		ChainID: 1, Netuid: 2,
		provisionalResume: &provisionalResumeState{
			Record:     &provisionalResumeRecord{Provisional: true, Command: "scenario", DeploymentID: "synthetic-prefix-deployment", PlanHash: "synthetic-prefix-plan"},
			RecordPath: filepath.Join(dir, "provenance.json"), RecordHash: bytesSHA256([]byte("synthetic invocation")),
			recoveryJournal: &scenarioCampaignJournalCache{validated: func() { calls.Add(1) }},
		},
	}
	return cfg, dir, calls
}

// Replace contents in the same inode so retained-byte verification, rather than
// pathname identity alone, must distinguish valid appends from rewritten history.
func writeCampaignJournalCheckpoint(t *testing.T, dir string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "journal.jsonl"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Re-encode one synthetic record after changing its action semantics.
func campaignJournalCheckpointLine(t *testing.T, entry JournalEntry) ([]byte, string) {
	t.Helper()
	entry.EntryHash = ""
	hash, err := canonicalHashHex(entry)
	if err != nil {
		t.Fatal(err)
	}
	entry.EntryHash = hash
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n'), hash
}

// Repeated historical cuts decode only the new suffix, retain full visitor
// diagnostics, and never give that suffix authority over an older signed cut.
func TestScenarioCampaignJournalCheckpointReusesPrefixAndAppends(t *testing.T) {
	cfg, dir, calls := campaignJournalCheckpointFixture(t)
	first, hash := campaignRecoveryPrefixTestLine(t, 1, "", "complete diagnostic")
	second, hash := campaignRecoveryPrefixTestLine(t, 2, hash, "later diagnostic")
	third, _ := campaignRecoveryPrefixTestLine(t, 3, hash, "current diagnostic")
	writeCampaignJournalCheckpoint(t, dir, append(bytes.Clone(first), second...))
	_, before, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, nil)
	if err != nil || calls.Load() != 2 {
		t.Fatalf("initial proof: calls=%d error=%v", calls.Load(), err)
	}
	for range 3 {
		if _, snapshot, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, nil); err != nil || snapshot != before {
			t.Fatalf("unchanged proof: snapshot=%+v error=%v", snapshot, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("unchanged journal was semantically replayed: calls=%d", calls.Load())
	}
	writeCampaignJournalCheckpoint(t, dir, append(append(bytes.Clone(first), second...), third...))
	expected := scenarioCampaignJournalCut{Bytes: uint64(len(first)), SHA256: bytesSHA256(first)}
	var entries []JournalEntry
	cut, snapshot, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, &expected, func(entry JournalEntry) {
		entries = append(entries, entry)
		entry.IntentHash = "caller mutation"
	})
	if err != nil || calls.Load() != 3 || cut != expected || snapshot.Bytes != uint64(len(first)+len(second)+len(third)) || len(entries) != 1 || entries[0].Error != "complete diagnostic" {
		t.Fatalf("appended proof leaked authority or replayed history: calls=%d cut=%+v snapshot=%+v entries=%+v error=%v", calls.Load(), cut, snapshot, entries, err)
	}
	for _, witnesses := range cfg.provisionalResume.recoveryJournal.checkpoint.journal.validationKVs {
		for _, entry := range witnesses {
			if entry.Error != "" || entry.IntentHash == "caller mutation" {
				t.Fatal("checkpoint retained diagnostic text or caller-owned mutation")
			}
		}
	}
}

// Hashes are checked even when an attacker supplies a new valid chain with the
// same length and restores mtime; inode, truncation and symlink checks are extra.
func TestScenarioCampaignJournalCheckpointRejectsChangedSources(t *testing.T) {
	for _, kind := range []string{"valid-rewrite", "truncate", "replacement", "symlink"} {
		cfg, dir, calls := campaignJournalCheckpointFixture(t)
		first, _ := campaignRecoveryPrefixTestLine(t, 1, "", "")
		writeCampaignJournalCheckpoint(t, dir, first)
		if _, _, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, nil); err != nil {
			t.Fatal(err)
		}
		checkpoint := cfg.provisionalResume.recoveryJournal.checkpoint
		path := filepath.Join(dir, "journal.jsonl")
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "valid-rewrite":
			changed, _ := campaignRecoveryPrefixTestLine(t, 2, "", "")
			if len(changed) != len(first) {
				t.Fatal("replacement fixture changed size")
			}
			writeCampaignJournalCheckpoint(t, dir, changed)
			if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
		case "truncate":
			writeCampaignJournalCheckpoint(t, dir, first[:len(first)-1])
		case "replacement", "symlink":
			target := filepath.Join(dir, "replacement.jsonl")
			if err := os.WriteFile(target, first, 0o600); err != nil {
				t.Fatal(err)
			}
			if kind == "replacement" {
				err = os.Rename(target, path)
			} else if err = os.Remove(path); err == nil {
				err = os.Symlink(target, path)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		cut, snapshot, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, nil)
		if err == nil || cut != (scenarioCampaignJournalCut{}) || snapshot != (scenarioCampaignJournalCut{}) || calls.Load() != 1 || cfg.provisionalResume.recoveryJournal.checkpoint != checkpoint {
			t.Fatalf("%s replaced authenticated progress: calls=%d cut=%+v snapshot=%+v error=%v", kind, calls.Load(), cut, snapshot, err)
		}
	}
}

// An invalid suffix must not leave a terminal witness in the successful prefix.
// After repairing only the suffix, validation continues from the unchanged cut.
func TestScenarioCampaignJournalCheckpointFailureDoesNotPoisonRecovery(t *testing.T) {
	cfg, dir, calls := campaignJournalCheckpointFixture(t)
	first, hash := campaignRecoveryPrefixTestLine(t, 1, "", "")
	var initial JournalEntry
	if err := json.Unmarshal(first, &initial); err != nil {
		t.Fatal(err)
	}
	writeCampaignJournalCheckpoint(t, dir, first)
	if _, _, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, nil); err != nil {
		t.Fatal(err)
	}
	retained := cfg.provisionalResume.recoveryJournal.checkpoint
	verified := initial
	verified.Sequence, verified.PreviousHash, verified.Stage = 2, hash, StageVerified
	verified.PostconditionHash = "synthetic-postcondition"
	path, err := legacyPostconditionRelativePath(initial.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	verified.PostconditionPath = path
	second, hash := campaignJournalCheckpointLine(t, verified)
	repeated := initial
	repeated.Sequence, repeated.PreviousHash = 3, hash
	third, _ := campaignJournalCheckpointLine(t, repeated)
	writeCampaignJournalCheckpoint(t, dir, append(append(bytes.Clone(first), second...), third...))
	if _, _, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, nil); err == nil || !strings.Contains(err.Error(), "verification is terminal") {
		t.Fatalf("new suffix lost retained action semantics: %v", err)
	}
	if cfg.provisionalResume.recoveryJournal.checkpoint != retained || retained.count != 1 || len(retained.journal.validationKVs[journalActionKey{planHash: initial.PlanHash, actionId: initial.ActionID}]) != 1 {
		t.Fatal("failed suffix modified the successful checkpoint")
	}
	writeCampaignJournalCheckpoint(t, dir, append(bytes.Clone(first), second...))
	if _, _, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, nil); err != nil || calls.Load() != 4 {
		t.Fatalf("repair replayed old prefix or inherited failed state: calls=%d error=%v", calls.Load(), err)
	}
	writeCampaignJournalCheckpoint(t, dir, append(append(bytes.Clone(first), second...), third...))
	if _, _, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, nil); err == nil || !strings.Contains(err.Error(), "verification is terminal") {
		t.Fatalf("successful terminal witness was lost on the next append: %v", err)
	}
}

// Warm and cold readers both reject mutation or replacement while visiting;
// appends remain a new finite snapshot for the next observation.
func TestScenarioCampaignJournalCheckpointConcurrentMutationAndAppend(t *testing.T) {
	for _, warm := range []bool{false, true} {
		for _, kind := range []string{"rewrite", "replace", "append"} {
			cfg, dir, calls := campaignJournalCheckpointFixture(t)
			first, hash := campaignRecoveryPrefixTestLine(t, 1, "", "")
			second, _ := campaignRecoveryPrefixTestLine(t, 2, hash, "")
			writeCampaignJournalCheckpoint(t, dir, first)
			if warm {
				if _, _, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			cut, snapshot, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, func(JournalEntry) {
				switch kind {
				case "rewrite":
					changed, _ := campaignRecoveryPrefixTestLine(t, 2, "", "")
					writeCampaignJournalCheckpoint(t, dir, changed)
				case "replace":
					path := filepath.Join(dir, "replacement.jsonl")
					if err := os.WriteFile(path, first, 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(path, filepath.Join(dir, "journal.jsonl")); err != nil {
						t.Fatal(err)
					}
				case "append":
					file, err := os.OpenFile(filepath.Join(dir, "journal.jsonl"), os.O_WRONLY|os.O_APPEND, 0)
					if err != nil {
						t.Fatal(err)
					}
					_, writeErr := file.Write(second)
					if err := errors.Join(writeErr, file.Close()); err != nil {
						t.Fatal(err)
					}
				}
			})
			if kind != "append" {
				if err == nil || cut.Bytes != 0 || snapshot.Bytes != 0 {
					t.Fatalf("warm=%t kind=%s trusted a changed source: %v", warm, kind, err)
				}
				continue
			}
			if err != nil || cut != snapshot || snapshot.Bytes != uint64(len(first)) || calls.Load() != 1 {
				t.Fatalf("warm=%t append moved finite cut: cut=%+v snapshot=%+v calls=%d error=%v", warm, cut, snapshot, calls.Load(), err)
			}
			if _, current, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, &cut, nil); err != nil || current.Bytes != uint64(len(first)+len(second)) || calls.Load() != 2 {
				t.Fatalf("warm=%t next snapshot lost append: current=%+v calls=%d error=%v", warm, current, calls.Load(), err)
			}
		}
	}
}

// Strict readers and new invocations revalidate every record. A historical
// policy projection in the same invocation may share the authenticated journal.
func TestScenarioCampaignJournalCheckpointScopesInvocationAndStrictReplay(t *testing.T) {
	cfg, dir, calls := campaignJournalCheckpointFixture(t)
	first, _ := campaignRecoveryPrefixTestLine(t, 1, "", "")
	writeCampaignJournalCheckpoint(t, dir, first)
	if _, _, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, nil); err != nil {
		t.Fatal(err)
	}
	historical := *cfg
	historical.readOnlyAudit, historical.ConfigHash, historical.PolicyHash = true, "old configuration", "old policy"
	if _, _, err := readScenarioCampaignJournalSnapshotMemo(&historical, dir, nil, nil); err != nil || calls.Load() != 1 {
		t.Fatalf("historical projection replayed invocation journal: %d %v", calls.Load(), err)
	}
	cfg.provisionalResume.RecordHash = bytesSHA256([]byte("new invocation"))
	if _, _, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, nil); err != nil || calls.Load() != 2 {
		t.Fatalf("new invocation reused old proof: %d %v", calls.Load(), err)
	}
	strictCalls := 0
	for range 2 {
		if _, _, next, err := readScenarioCampaignJournalCheckpoint(dir, nil, nil, nil, false, func() { strictCalls++ }); err != nil || next != nil {
			t.Fatalf("strict replay memoized a journal: %v", err)
		}
	}
	if strictCalls != 2 {
		t.Fatalf("strict replay skipped history: %d", strictCalls)
	}
	for _, disable := range []func(*ResolvedConfig){
		func(cfg *ResolvedConfig) { cfg.provisionalResume.Record.FinalAcceptance = true },
		func(cfg *ResolvedConfig) { cfg.provisionalResume.Record.ReadOnly = true },
		func(cfg *ResolvedConfig) { cfg.provisionalResume.Record.Provisional = false },
		func(cfg *ResolvedConfig) { cfg.strictHistoryAdoption = &strictHistoryAdoptionState{} },
		func(cfg *ResolvedConfig) { cfg.relayCapturePlanHash = "synthetic capture" },
		func(cfg *ResolvedConfig) { cfg.provisionalResume = nil },
	} {
		view := *cfg
		invocation := *cfg.provisionalResume
		record := *invocation.Record
		invocation.Record = &record
		view.provisionalResume = &invocation
		disable(&view)
		if _, enabled := scenarioCampaignJournalContext(&view, dir); enabled {
			t.Fatal("strict or unauthenticated context admitted incremental replay")
		}
	}
	cut, snapshot, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, func(JournalEntry) {
		cfg.Netuid++
	})
	if err == nil || cut.Bytes != 0 || snapshot.Bytes != 0 || !strings.Contains(err.Error(), "invocation changed") {
		t.Fatalf("visitor changed authority without rejection: %v", err)
	}
}

// Shared configuration copies join one successful semantic pass. Callbacks own
// detached values, while a failed suffix cannot race publication of its state.
func TestScenarioCampaignJournalCheckpointConcurrentReaders(t *testing.T) {
	cfg, dir, calls := campaignJournalCheckpointFixture(t)
	first, hash := campaignRecoveryPrefixTestLine(t, 1, "", "")
	second, _ := campaignRecoveryPrefixTestLine(t, 2, hash, "")
	writeCampaignJournalCheckpoint(t, dir, append(bytes.Clone(first), second...))
	start := make(chan struct{})
	results := make(chan error, 8)
	var joined sync.WaitGroup
	for range 8 {
		joined.Add(1)
		go func() {
			defer joined.Done()
			<-start
			view := *cfg
			_, _, err := readScenarioCampaignJournalSnapshotMemo(&view, dir, nil, nil)
			results <- err
		}()
	}
	close(start)
	joined.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("concurrent readers repeated authenticated work: %d", calls.Load())
	}
}

// Compact witnesses have an explicit memory bound, independent of diagnostic
// payload size; detached map/slice ownership prevents untrusted suffix mutation.
func TestScenarioCampaignJournalCheckpointWitnessBudgetAndIsolation(t *testing.T) {
	entry := JournalEntry{PlanHash: "synthetic plan", ActionID: "synthetic action", IntentHash: "synthetic intent", Stage: StageIntent}
	key := journalActionKey{planHash: entry.PlanHash, actionId: entry.ActionID}
	source := &Journal{lastHash: "synthetic hash", deploymentID: "synthetic deployment", validationKVs: map[journalActionKey][]JournalEntry{key: {entry}}}
	cloned := cloneScenarioCampaignJournalWitnesses(source)
	if !reflect.DeepEqual(source.validationKVs, cloned.validationKVs) || scenarioCampaignJournalWitnessBytes(source) >= scenarioCampaignJournalCheckpointBytes {
		t.Fatal("small witness inventory did not retain its exact bounded state")
	}
	cloned.validationKVs[key][0].IntentHash = "changed"
	delete(cloned.validationKVs, key)
	if source.validationKVs[key][0] != entry {
		t.Fatal("cloned witnesses share mutable storage")
	}
	large := strings.Repeat("x", 1024*1024)
	for i := range 33 {
		key := journalActionKey{planHash: "synthetic plan", actionId: string(rune('a' + i))}
		entry.ActionID = large
		source.validationKVs[key] = []JournalEntry{entry}
	}
	if scenarioCampaignJournalWitnessBytes(source) <= scenarioCampaignJournalCheckpointBytes {
		t.Fatal("witness memory accounting missed oversized immutable strings")
	}
}

// A temporary rewrite can be restored before the final range hash, with an
// append making changed timestamps legitimate. Bind the actual visitor bytes
// as well, so neither read-ahead nor later restoration hides an unproved record.
func TestScenarioCampaignJournalCheckpointVisitorCannotObserveTemporaryRewrite(t *testing.T) {
	cfg, dir, calls := campaignJournalCheckpointFixture(t)
	first, hash := campaignRecoveryPrefixTestLine(t, 1, "", strings.Repeat("a", 80*1024))
	second, hash := campaignRecoveryPrefixTestLine(t, 2, hash, strings.Repeat("b", 80*1024)+"original-marker")
	third, _ := campaignRecoveryPrefixTestLine(t, 3, hash, "")
	original := append(bytes.Clone(first), second...)
	changed := bytes.Replace(original, []byte("original-marker"), []byte("modified-marker"), 1)
	writeCampaignJournalCheckpoint(t, dir, original)
	if _, _, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, nil); err != nil {
		t.Fatal(err)
	}
	visits := 0
	cut, snapshot, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, func(entry JournalEntry) {
		visits++
		if visits == 1 {
			writeCampaignJournalCheckpoint(t, dir, changed)
		} else if visits == 2 {
			if !strings.HasSuffix(entry.Error, "modified-marker") {
				t.Fatal("fixture did not place the mutation beyond scanner read-ahead")
			}
			writeCampaignJournalCheckpoint(t, dir, append(bytes.Clone(original), third...))
		}
	})
	if err == nil || !strings.Contains(err.Error(), "visitor source changed") || visits != 2 || cut.Bytes != 0 || snapshot.Bytes != 0 {
		t.Fatalf("visitor accepted temporarily rewritten bytes: visits=%d cut=%+v snapshot=%+v error=%v", visits, cut, snapshot, err)
	}
	if _, _, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, nil); err != nil || calls.Load() != 3 {
		t.Fatalf("repaired prefix did not retain its proof and validate the append: calls=%d error=%v", calls.Load(), err)
	}
}

// Incomplete in-memory state and malformed signed cuts fail before they can
// dereference missing proof or turn a partial record into predecessor authority.
func TestScenarioCampaignJournalCheckpointRejectsIncompleteProofAndCuts(t *testing.T) {
	cfg, dir, _ := campaignJournalCheckpointFixture(t)
	first, _ := campaignRecoveryPrefixTestLine(t, 1, "", "")
	writeCampaignJournalCheckpoint(t, dir, first)
	if _, _, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, nil, nil); err != nil {
		t.Fatal(err)
	}
	base := cfg.provisionalResume.recoveryJournal.checkpoint
	for _, modify := range []func(*scenarioCampaignJournalCheckpoint){
		func(proof *scenarioCampaignJournalCheckpoint) { proof.info = nil },
		func(proof *scenarioCampaignJournalCheckpoint) { proof.journal = nil },
		func(proof *scenarioCampaignJournalCheckpoint) { proof.journal = &Journal{} },
		func(proof *scenarioCampaignJournalCheckpoint) { proof.count = 0 },
		func(proof *scenarioCampaignJournalCheckpoint) { proof.cut.Bytes = 0 },
		func(proof *scenarioCampaignJournalCheckpoint) { proof.cut.SHA256 = "malformed" },
	} {
		proof := *base
		modify(&proof)
		cut, snapshot, next, err := readScenarioCampaignJournalCheckpoint(dir, nil, nil, &proof, true, nil)
		if err == nil || cut.Bytes != 0 || snapshot.Bytes != 0 || next != nil {
			t.Fatalf("incomplete checkpoint was accepted: %v", err)
		}
	}
	for _, expected := range []scenarioCampaignJournalCut{
		{Bytes: 0, SHA256: bytesSHA256(nil)},
		{Bytes: ^uint64(0), SHA256: bytesSHA256(first)},
		{Bytes: uint64(len(first)), SHA256: "malformed"},
		{Bytes: uint64(len(first) - 1), SHA256: bytesSHA256(first[:len(first)-1])},
		{Bytes: uint64(len(first)), SHA256: bytesSHA256([]byte("different"))},
	} {
		cut, snapshot, err := readScenarioCampaignJournalSnapshotMemo(cfg, dir, &expected, nil)
		if err == nil || cut.Bytes != 0 || snapshot.Bytes != 0 {
			t.Fatalf("cached history admitted malformed signed cut %+v: %v", expected, err)
		}
	}
}
