//go:build linux || darwin

// Real signed locators, both public Http origins and the production worker
// exercise audit/start separation. Channels establish every failure boundary.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The public read is held while the real relay constructor loop authenticates
// and services RequirePrepared. Neither replica completion nor replay progress
// is allowed to impersonate the other, and cancellation joins both readers.
func TestEvidenceRelayPublicAuditStartsReleaseBeforeBlockedReplica(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	runtime := fixture.runtime
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	runtime.ctx, runtime.cancel = ctx, cancel
	runtime.ready, runtime.done, runtime.changed = make(chan struct{}), make(chan struct{}), make(chan struct{})
	runtime.remainingRequests = make(chan evidenceRelayRemainingRequest, 1)
	runtime.through, runtime.completed = map[uint64]uint64{}, map[uint64]bool{}
	runtime.poll = time.Hour
	failures := make(chan error, 1)
	runtime.fail = func(err error) { failures <- err }
	for index := range runtime.sources {
		runtime.sources[index].nextEpoch = runtime.sources[index].activations[0].Domain.Epoch
		runtime.completed[runtime.sources[index].validatorId] = false
	}
	fixture.blockHash = fmt.Sprintf("0x%x", fixture.closed[0].CensusHash)
	fixture.blockStarted = make(chan struct{})
	before := runtime.executor.journal.Entries()
	go runtime.run()
	t.Cleanup(func() { _ = runtime.Close() })
	if err := runtime.WaitReady(t.Context()); err != nil {
		t.Fatal("provisional admission waited for the public audit", err)
	}
	select {
	case <-fixture.blockStarted:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	observer, cancelObserver := context.WithCancel(t.Context())
	cancelObserver()
	if err := runtime.WaitPublicAudit(observer); !errors.Is(err, context.Canceled) || ctx.Err() != nil {
		t.Fatal("canceled audit observer completed the audit or canceled runtime", err)
	}
	if err := runtime.RequirePrepared(t.Context()); err != nil {
		t.Fatal("blocked read-only audit prevented real phase admission", err)
	}
	select {
	case <-runtime.publicAudit.done:
		t.Fatal("blocked replica produced a terminal audit")
	default:
	}
	if !reflect.DeepEqual(before, runtime.executor.journal.Entries()) {
		t.Fatal("parallel census manufactured a transaction")
	}
	cancel()
	if err := runtime.Close(); err != nil {
		t.Fatal("joining owned audit/relay cancellation became a runtime failure", err)
	}
	select {
	case failure := <-failures:
		t.Fatal("owned cancellation failed the campaign", failure)
	default:
	}
}

// A previous complete object survives the refactor's private reader identity.
// The audit completes only after the untouched suffix is read at both origins.
func TestEvidenceRelayPublicAuditReusesCheckpointWithoutGrantingLiveProgress(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	fixture.checkpoint(t)
	runtime := fixture.runtime
	beforeReads := fixture.counts()
	beforeJournal := runtime.executor.journal.Entries()
	beforeSources := append([]evidenceRelaySource(nil), runtime.sources...)
	if err := runtime.prepareHorizon(); err != nil {
		t.Fatal(err)
	}
	if runtime.pendingPublicCensus == nil || fixture.counts() != beforeReads || len(runtime.horizon.headerKVs) != 0 {
		t.Fatal("provisional preparation read replicas or fabricated slot progress")
	}
	if err := runtime.WaitPublicAudit(t.Context()); err == nil {
		t.Fatal("unstarted audit was accepted")
	}
	runtime.publicAudit = newEvidenceRelayPublicAudit(t.Context(), runtime.pendingPublicCensus)
	t.Cleanup(func() { _ = runtime.publicAudit.Close() })
	if err := runtime.WaitPublicAudit(t.Context()); err != nil {
		t.Fatal("real retained-prefix audit could not complete", err)
	}
	if err := runtime.publicAudit.Close(); err != nil {
		t.Fatal(err)
	}
	afterReads := fixture.counts()
	for index := range afterReads {
		if afterReads[index]-beforeReads[index] != 10 {
			t.Fatalf("origin %d replayed the complete prefix or omitted suffix: before=%v after=%v", index, beforeReads, afterReads)
		}
	}
	if !reflect.DeepEqual(beforeJournal, runtime.executor.journal.Entries()) || !reflect.DeepEqual(beforeSources, runtime.sources) || len(runtime.horizon.headerKVs) != 0 || len(runtime.pendingPublicCensus.horizon.headerKVs) != 6 {
		t.Fatal("audit mutated the journal, live horizon or relay cursors")
	}
	entries, err := filepath.Glob(filepath.Join(runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName, "*.json"))
	if err != nil || len(entries) != 3 {
		t.Fatal("parallel verification did not preserve all complete checkpoints", err)
	}
}

// Integrity failures remain visible to final acceptance while the provisional
// controller retains its independent lifetime and authenticated local inputs.
func TestEvidenceRelayPublicAuditFailureCannotPassOrCancelProvisionalRuntime(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	runtime := fixture.runtime
	if err := runtime.prepareHorizon(); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("0x%x", fixture.closed[0].CensusHash)
	fixture.stateLock.Lock()
	fixture.objectKVs[1][hash] = []byte("changed immutable census\n")
	fixture.stateLock.Unlock()
	runtime.publicAudit = newEvidenceRelayPublicAudit(t.Context(), runtime.pendingPublicCensus)
	t.Cleanup(func() { _ = runtime.publicAudit.Close() })
	if err := runtime.WaitPublicAudit(t.Context()); err == nil || runtime.ctx.Err() != nil {
		t.Fatal("failed census became passing or canceled the provisional owner", err)
	}
	if err := runtime.publicAudit.Close(); err == nil {
		t.Fatal("cleanup discarded the independently failed audit")
	}
	entries, err := filepath.Glob(filepath.Join(runtime.executor.stateDir, evidenceRelayColdCensusDirectoryName, "*.json"))
	if err != nil || len(entries) != 0 || len(runtime.horizon.headerKVs) != 0 {
		t.Fatal("failed public authentication created durable success or live admission", err)
	}
}

// Strict mode always uses the complete inline public read. A warm provisional
// checkpoint never lets strict startup borrow lower-assurance admission.
func TestEvidenceRelayPublicAuditStrictPreparationStillReadsBothReplicas(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	fixture.checkpoint(t)
	runtime := fixture.runtime
	runtime.executor.cfg.provisionalResume = nil
	before := fixture.counts()
	if err := runtime.prepareHorizon(); err != nil {
		t.Fatal("strict actual public census could not complete", err)
	}
	after := fixture.counts()
	if runtime.pendingPublicCensus != nil || runtime.publicAudit != nil || len(runtime.horizon.headerKVs) != 6 || after[0]-before[0] != 15 || after[1]-before[1] != 15 {
		t.Fatal("strict preparation deferred or cached a required public read", before, after)
	}
}

// A file replaced after local admission cannot use its parsed predecessor as
// proof that the current fixed census still exists when final audit completes.
func TestEvidenceRelayPublicAuditRejectsLocatorReplacementDuringRead(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	runtime := fixture.runtime
	fixture.blockHash = fmt.Sprintf("0x%x", fixture.closed[0].CensusHash)
	fixture.blockStarted, fixture.blockRelease = make(chan struct{}), make(chan struct{})
	if err := runtime.prepareHorizon(); err != nil {
		t.Fatal(err)
	}
	runtime.publicAudit = newEvidenceRelayPublicAudit(t.Context(), runtime.pendingPublicCensus)
	t.Cleanup(func() { _ = runtime.publicAudit.Close() })
	select {
	case <-fixture.blockStarted:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	witness := runtime.pendingPublicCensus.witnesses[0]
	path := evidenceRelayStartupWitnessPath(runtime.executor.stateDir, witness)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	close(fixture.blockRelease)
	if err := runtime.WaitPublicAudit(t.Context()); err == nil || !strings.Contains(err.Error(), "changed during audit") {
		t.Fatal("replaced manifest borrowed prior public verification", err)
	}
}

// A pre-canceled first request cannot release the initial replay barrier.
// Its replacement performs fresh chain and native checks before starting work.
func TestEvidenceRelayPublicAuditInitialPreparationRetriesAbandonedRequest(t *testing.T) {
	fixture := newEvidenceRelayColdCensusTestFixture(t)
	runtime := fixture.runtime
	runtime.ready, runtime.done = make(chan struct{}), make(chan struct{})
	runtime.remainingRequests = make(chan evidenceRelayRemainingRequest, 1)
	if err := runtime.prepareHorizon(); err != nil {
		t.Fatal(err)
	}
	close(runtime.ready)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	abandoned := evidenceRelayRemainingRequest{ctx: ctx, result: make(chan error, 1)}
	runtime.remainingRequests <- abandoned
	completed := make(chan error, 1)
	go func() { completed <- runtime.awaitInitialReleasePreparation() }()
	if err := <-abandoned.result; !errors.Is(err, context.Canceled) {
		t.Fatal("abandoned first request obtained preparation", err)
	}
	select {
	case err := <-completed:
		t.Fatal("abandoned request started retained replay", err)
	default:
	}
	if err := runtime.RequirePrepared(t.Context()); err != nil {
		t.Fatal("replacement phase admission could not proceed", err)
	}
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
}
