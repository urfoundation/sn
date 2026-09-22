//go:build linux || darwin

// Real completed public authentication seeds these adversarial private-file
// cases. No test-created proof is used to substitute for the initial reader.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/protocol"
	validatorcomponent "github.com/urfoundation/sn/validator"
	"golang.org/x/sys/unix"
)

// Retain one actual complete result, then reopen its independently located
// authenticated entry so mutation tests have no synthetic verification verdict.
func (self *evidenceRelayColdCensusTestFixture) checkpoint(t *testing.T) (*evidenceRelayColdCensusEntry, *validatorcomponent.ValidatorEvidenceCensusV2Publication) {
	t.Helper()
	census := self.census(t, 1)
	requests, err := self.runtime.readClosedPublication(t.Context(), &self.runtime.sources[0], &self.closed[0], self.finalized, self.finalizedHash, census)
	if err != nil || len(requests) != len(self.runtime.sources[0].activations) || census.checkpointed != 1 {
		t.Fatal("could not seed actual complete public checkpoint", err)
	}
	census = self.census(t, 1)
	entry := census.entry(t.Context(), &self.closed[0], requests[0].Window)
	publication := entry.read(t.Context())
	if entry == nil || publication == nil {
		t.Fatal("complete checkpoint could not be authenticated after reopen")
	}
	return entry, publication
}

// Every identity field is exact. The full semantic context changes for plans,
// source bounds, origins and original activations; a runner rebuild is incidental.
func TestEvidenceRelayColdCensusRejectsContextSourceVersionAndGeometrySubstitutions(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	entry, _ := fixture.checkpoint(t)
	for _, change := range []struct {
		name   string
		mutate func(*evidenceRelayColdCensusEntry)
	}{
		{name: "schema", mutate: func(value *evidenceRelayColdCensusEntry) { value.identity.Schema += "-other" }},
		{name: "verifier", mutate: func(value *evidenceRelayColdCensusEntry) { value.identity.VerifierVersion += "-other" }},
		{name: "context", mutate: func(value *evidenceRelayColdCensusEntry) {
			value.identity.ContextHash = "0x" + strings.Repeat("ab", 32)
		}},
		{name: "source", mutate: func(value *evidenceRelayColdCensusEntry) { value.identity.SourceHash = "0x" + strings.Repeat("ab", 32) }},
		{name: "manifest", mutate: func(value *evidenceRelayColdCensusEntry) {
			value.identity.ManifestHash = "0x" + strings.Repeat("ab", 32)
		}},
		{name: "epoch geometry", mutate: func(value *evidenceRelayColdCensusEntry) { value.identity.Window.EndBlock++ }},
		{name: "file witness", mutate: func(value *evidenceRelayColdCensusEntry) { value.identity.File.Inode++ }},
	} {
		candidate := *entry
		change.mutate(&candidate)
		if candidate.read(t.Context()) != nil {
			t.Errorf("%s substitution borrowed the original proof", change.name)
		}
	}
	for _, change := range []struct {
		name   string
		mutate func() func()
	}{
		{name: "plan", mutate: func() func() {
			fixture.runtime.executor.plan.Schema += "-other"
			return func() {
				fixture.runtime.executor.plan.Schema = strings.TrimSuffix(fixture.runtime.executor.plan.Schema, "-other")
			}
		}},
		{name: "source bound", mutate: func() func() {
			fixture.runtime.sources[0].bounds.MaxClosureBytes++
			return func() { fixture.runtime.sources[0].bounds.MaxClosureBytes-- }
		}},
		{name: "original activation", mutate: func() func() {
			fixture.runtime.sources[0].activations[0].NativeBlock++
			return func() { fixture.runtime.sources[0].activations[0].NativeBlock-- }
		}},
		{name: "origin", mutate: func() func() {
			original := fixture.runtime.origins[1]
			fixture.runtime.origins[1] = "https://changed.example"
			return func() { fixture.runtime.origins[1] = original }
		}},
	} {
		restore := change.mutate()
		candidate := fixture.census(t, 1).entry(t.Context(), &fixture.closed[0], entry.window)
		if candidate == nil || candidate.identity.ContextHash == entry.identity.ContextHash || candidate.read(t.Context()) != nil {
			t.Errorf("%s did not invalidate the exact semantic context", change.name)
		}
		restore()
	}
}

// Even a correctly retagged malformed candidate cannot drop a configured member
// or replace its original consent. Ordinary corruption fails Hmac authentication.
func TestEvidenceRelayColdCensusRejectsTamperedIncompleteAndUnboundedEntries(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	entry, _ := fixture.checkpoint(t)
	path := filepath.Join(fixture.runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName, entry.name)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		name   string
		mutate func(*evidenceRelayColdCensusEnvelope)
		retag  bool
	}{
		{name: "invalid tag", mutate: func(value *evidenceRelayColdCensusEnvelope) { value.Mac = strings.Repeat("00", 32) }},
		{name: "header mutation", mutate: func(value *evidenceRelayColdCensusEnvelope) { value.Proof.Members[0].Header.PayloadBytes++ }},
		{name: "missing member", mutate: func(value *evidenceRelayColdCensusEnvelope) { value.Proof.Members = value.Proof.Members[:1] }, retag: true},
		{name: "reordered members", mutate: func(value *evidenceRelayColdCensusEnvelope) {
			value.Proof.Members[0], value.Proof.Members[1] = value.Proof.Members[1], value.Proof.Members[0]
		}, retag: true},
		{name: "invalid consent", mutate: func(value *evidenceRelayColdCensusEnvelope) { value.Proof.Members[0].HotkeySignature[0] ^= 1 }, retag: true},
	} {
		var envelope evidenceRelayColdCensusEnvelope
		if err := json.Unmarshal(original, &envelope); err != nil {
			t.Fatal(err)
		}
		change.mutate(&envelope)
		if change.retag {
			envelope.Mac = hex.EncodeToString(entry.authenticationTag(envelope.Proof))
		}
		raw, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		if entry.read(t.Context()) != nil {
			t.Errorf("%s produced a reusable publication", change.name)
		}
	}
	for _, raw := range [][]byte{nil, original[:len(original)/2], append(bytes.Clone(original), []byte("{}\n")...), bytes.Replace(original, []byte("{\"proof\":"), []byte("{\"unknown\":true,\"proof\":"), 1), bytes.Replace(original, []byte("{\"proof\":"), []byte("{\"proof\":null,\"proof\":"), 1)} {
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if entry.read(t.Context()) != nil {
			t.Fatal("noncanonical or incomplete entry was accepted")
		}
	}
	if err := os.Truncate(path, entry.maximumBytes+1); err != nil {
		t.Fatal(err)
	}
	if entry.read(t.Context()) != nil {
		t.Fatal("oversized entry exceeded the signed-object byte allowance")
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	entry.source.session.key[0] ^= 1
	if entry.read(t.Context()) != nil {
		t.Fatal("another wallet key authenticated the prior proof")
	}
	entry.source.session.key[0] ^= 1
	if entry.read(t.Context()) == nil {
		t.Fatal("restored exact entry was not independently readable")
	}
}

// The descriptor-owned cache never follows a substituted entry or directory.
// Replacing even identical source bytes invalidates the original file witness.
func TestEvidenceRelayColdCensusRejectsUnsafeFilesAndLocatorReplacement(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	entry, publication := fixture.checkpoint(t)
	path := filepath.Join(fixture.runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName, entry.name)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if entry.read(t.Context()) != nil {
		t.Fatal("nonprivate entry was accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	locator := evidenceRelayStartupWitnessPath(fixture.runtime.executor.stateDir, entry.identity.File)
	original, err := os.ReadFile(locator)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(locator, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if entry.read(t.Context()) != nil || entry.save(t.Context(), publication) || entry.source.session.revalidate() == nil {
		t.Fatal("replaced locator retained an earlier verification witness")
	}
	entry, publication = fixture.checkpoint(t)
	path = filepath.Join(fixture.runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName, entry.name)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(locator, path); err != nil {
		t.Fatal(err)
	}
	if entry.read(t.Context()) != nil {
		t.Fatal("linked entry was accepted")
	}
	cacheDir := filepath.Dir(path)
	if err := os.Rename(cacheDir, cacheDir+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cacheDir+".retained", cacheDir); err != nil {
		t.Fatal(err)
	}
	if entry.read(t.Context()) != nil || entry.save(t.Context(), publication) {
		t.Fatal("linked cache directory redirected access")
	}
}

// Aborting before atomic replacement preserves the old complete entry. Once
// replacement happens, cancellation cannot erase that already completed object.
func TestEvidenceRelayColdCensusAtomicCommitPreservesProgressAcrossCancellation(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	entry, publication := fixture.checkpoint(t)
	path := filepath.Join(fixture.runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName, entry.name)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	entry.beforeCommit = cancel
	if entry.save(ctx, publication) {
		t.Fatal("cancellation before commit published an entry")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) || entry.read(t.Context()) == nil {
		t.Fatal("cancellation before replacement erased completed work", err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	entry.beforeCommit, entry.afterCommit = nil, cancel
	if !entry.save(ctx, publication) || ctx.Err() == nil || entry.read(t.Context()) == nil {
		t.Fatal("cancellation after replacement erased a complete authenticated object")
	}
	paths, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".tmp-*"))
	if err != nil || len(paths) != 0 {
		t.Fatal("cancellation leaked an uncommitted temporary entry", paths, err)
	}
}

// Strict execution and stopped capture do not even construct a cache reader.
// Disabling private persistence is an optimization miss, never public authority.
func TestEvidenceRelayColdCensusStrictAndRetainedReadersRejectProvisionalAuthority(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	_, _ = fixture.checkpoint(t)
	cfg := fixture.runtime.executor.cfg
	provisional := cfg.provisionalResume
	for _, mode := range []string{"strict", "read-only", "retained", "no wallet"} {
		cfg.provisionalResume, cfg.readOnlyAudit, fixture.runtime.retainedPublications = provisional, false, nil
		wallet := cfg.WalletMaterial
		switch mode {
		case "strict":
			cfg.provisionalResume = nil
		case "read-only":
			cfg.readOnlyAudit = true
		case "retained":
			fixture.runtime.retainedPublications = &evidenceRelayRetainedPublications{}
		case "no wallet":
			cfg.WalletMaterial = ""
		}
		session, err := fixture.runtime.newEvidenceRelayColdCensusSession(t.Context())
		cfg.WalletMaterial = wallet
		if err != nil || session != nil {
			t.Fatalf("%s acquired provisional public authentication: %v", mode, err)
		}
	}
}

// A cache hit still checks the actual current audit upper block. Forward heads
// share immutable geometry; earlier heads cannot borrow later signed decisions.
func TestEvidenceRelayColdCensusAuditChecksFreshUpperBlockAndOriginalSubject(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	census := fixture.census(t, 1)
	if _, err := fixture.runtime.readAuditPublication(t.Context(), &fixture.runtime.sources[0], &fixture.audit, fixture.finalized, fixture.finalizedHash, census); err != nil {
		t.Fatal(err)
	}
	census = fixture.census(t, 1)
	window := protocol.ValidatorEvidenceWindow{Epoch: fixture.audit.Epoch, StartBlock: 210, EndBlock: 510, FinalizedBlock: 829, Subject: fixture.audit.Subject}
	entry := census.entry(t.Context(), &fixture.audit, window)
	if entry == nil || entry.read(t.Context()) != nil {
		t.Fatal("earlier finalized head borrowed a future audit decision")
	}
	window.FinalizedBlock = 900
	if census.entry(t.Context(), &fixture.audit, window).read(t.Context()) == nil {
		t.Fatal("later finalized head erased the exact original audit")
	}
	census = fixture.census(t, 1)
	window.Subject.NativeEpoch++
	if census.entry(t.Context(), &fixture.audit, window).read(t.Context()) != nil {
		t.Fatal("another native audit subject borrowed the original consent")
	}
}

// Persistence failure cannot invent resumable progress or discard an otherwise
// complete public result. The obstructing private path remains untouched.
func TestEvidenceRelayColdCensusPersistenceFailureReportsOnlyVerifiedProgress(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	path := filepath.Join(fixture.runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName)
	marker := []byte("synthetic cache obstruction\n")
	if err := os.WriteFile(path, marker, 0o600); err != nil {
		t.Fatal(err)
	}
	census := fixture.census(t, 1)
	requests, err := fixture.runtime.readClosedPublication(t.Context(), &fixture.runtime.sources[0], &fixture.closed[0], fixture.finalized, fixture.finalizedHash, census)
	if err != nil || len(requests) != 2 || census.completed != 1 || census.resumed != 0 || census.checkpointed != 0 || census.checkpointFailures != 1 {
		t.Fatal("persistence failure lost authentication or claimed durability", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, marker) {
		t.Fatal("cache writer replaced an unrelated existing path", err)
	}
}

// The explicit preparation capability belongs to one source and runtime. A
// caller passing another source still enters its ordinary public authentication.
func TestEvidenceRelayColdCensusCannotRouteOneSourcesProofToAnotherSource(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	_, _ = fixture.checkpoint(t)
	census := fixture.census(t, 1)
	requests, err := fixture.runtime.readClosedPublication(t.Context(), &fixture.runtime.sources[1], &fixture.closed[0], fixture.finalized, fixture.finalizedHash, census)
	if err == nil || requests != nil || census.completed != 0 {
		t.Fatal("another source borrowed a completed public census", err)
	}
}

// The retained entry budget spans all contexts, including leftover files from
// interrupted writers. A full directory preserves prior authenticated progress.
func TestEvidenceRelayColdCensusRetainedEntryBudgetSurvivesSessionRestart(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	entry, publication := fixture.checkpoint(t)
	directory := filepath.Join(fixture.runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName)
	for index := uint64(1); index < evidenceRelayColdCensusMaximumEntries; index++ {
		if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("retained-%04d.json", index)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	census := fixture.census(t, 1)
	requests, err := fixture.runtime.readClosedPublication(t.Context(), &fixture.runtime.sources[0], &fixture.closed[1], fixture.finalized, fixture.finalizedHash, census)
	if err != nil || len(requests) != 2 || census.checkpointFailures != 1 || census.checkpointed != 0 {
		t.Fatal("new session exceeded aggregate retained-entry capacity", err)
	}
	if !entry.save(t.Context(), publication) || entry.read(t.Context()) == nil {
		t.Fatal("entry capacity erased or prevented replacement of existing progress")
	}
	files, err := os.ReadDir(directory)
	if err != nil || uint64(len(files)) != evidenceRelayColdCensusMaximumEntries {
		t.Fatal("cache file count escaped its aggregate cap", len(files), err)
	}
}

// The writer accounts for the live old entry and its new temporary bytes,
// refreshes after directory changes and refuses another session's active flock.
func TestEvidenceRelayColdCensusRetainedByteBudgetAndConcurrentWriterAreBounded(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	entry, publication := fixture.checkpoint(t)
	directory := filepath.Join(fixture.runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName)
	info, err := os.Stat(filepath.Join(directory, entry.name))
	if err != nil {
		t.Fatal(err)
	}
	filler := filepath.Join(directory, "retained-large.json")
	file, err := os.OpenFile(filler, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(evidenceRelayColdCensusMaximumBytes - info.Size()); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if entry.save(t.Context(), publication) || entry.read(t.Context()) == nil {
		t.Fatal("cache exceeded total bytes or lost its existing authenticated entry")
	}
	if err := os.Remove(filler); err != nil {
		t.Fatal(err)
	}
	if !entry.save(t.Context(), publication) {
		t.Fatal("changed directory did not refresh its bounded byte census")
	}
	owned, err := openEvidenceRelayPrivateCacheDirectory(fixture.runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName, false)
	if err != nil {
		t.Fatal(err)
	}
	defer owned.Close()
	if err := unix.Flock(int(owned.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(int(owned.Fd()), unix.LOCK_UN)
	if entry.save(t.Context(), publication) || entry.read(t.Context()) == nil {
		t.Fatal("cache writer crossed another session's disk reservation")
	}
}
