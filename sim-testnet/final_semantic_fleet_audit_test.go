package main

// Proves that public ordinary-fleet replay rejects each identity boundary
// independently of lifecycle replay and keeps concurrent collection deterministic.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/ss58"
)

// Wraps the established semantic fake and mutates one requested response.
// stateLock protects only counters; base-reader calls occur after release.
type finalFleetAuditTestReader struct {
	*finalTestChainReader

	stateLock         sync.Mutex
	concurrency       int
	mutation          string
	callCounts        map[string]int
	inFlight          int
	maximumInFlight   int
	commitmentEntered chan struct{}
	commitmentRelease <-chan struct{}
}

// Supplies the test-controlled bounded worker count.
func (self *finalFleetAuditTestReader) FleetAuditConcurrency() int {
	return self.concurrency
}

// Records one in-flight fake RPC without holding the lock across a barrier or
// delegated reader call.
func (self *finalFleetAuditTestReader) begin(kind string) func() {
	self.stateLock.Lock()
	if self.callCounts == nil {
		self.callCounts = map[string]int{}
	}
	self.callCounts[kind]++
	self.inFlight++
	if self.inFlight > self.maximumInFlight {
		self.maximumInFlight = self.inFlight
	}
	entered, release := self.commitmentEntered, self.commitmentRelease
	self.stateLock.Unlock()
	if kind == "native-commitment" && entered != nil && release != nil {
		entered <- struct{}{}
		<-release
	}
	return func() {
		self.stateLock.Lock()
		self.inFlight--
		self.stateLock.Unlock()
	}
}

// Returns a stable test-only snapshot after collection has joined.
func (self *finalFleetAuditTestReader) counts() (map[string]int, int) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	result := make(map[string]int, len(self.callCounts))
	for key, value := range self.callCounts {
		result[key] = value
	}
	return result, self.maximumInFlight
}

// Models an absent or replaced native commitment.
func (self *finalFleetAuditTestReader) NativeFleetCommitment(ctx context.Context, netuid uint16, hotkey string, head ChainHead) (FinalNativeFleetCommitmentState, []FinalRPCExchange, error) {
	defer self.begin("native-commitment")()
	if self.mutation == "absent-commitment" {
		return FinalNativeFleetCommitmentState{}, nil, errors.New("commitment absent")
	}
	state, exchanges, err := self.finalTestChainReader.NativeFleetCommitment(ctx, netuid, hotkey, head)
	if err != nil {
		return state, exchanges, err
	}
	if self.mutation == "wrong-native-hash" || self.mutation == "replaced-commitment" {
		state.CommitmentHash = finalTestHex(0xe1)
	}
	return state, exchanges, nil
}

// Models a coordinator state that no longer mirrors native.
func (self *finalFleetAuditTestReader) FleetMirror(ctx context.Context, hotkey string, head ChainHead) (FinalFleetMirrorChainState, []FinalRPCExchange, error) {
	defer self.begin("mirror")()
	state, exchanges, err := self.finalTestChainReader.FleetMirror(ctx, hotkey, head)
	if err != nil {
		return state, exchanges, err
	}
	if self.mutation == "wrong-mirror-hash" || self.mutation == "replaced-commitment" {
		state.CommitmentHash = finalTestHex(0xe1)
	}
	return state, exchanges, nil
}

// Models a UID reassigned since the claimed fleet.
func (self *finalFleetAuditTestReader) NativeUID(ctx context.Context, netuid, uid uint16, head ChainHead) (FinalNativeUIDState, []FinalRPCExchange, error) {
	defer self.begin("native-uid")()
	state, exchanges, err := self.finalTestChainReader.NativeUID(ctx, netuid, uid, head)
	if err != nil {
		return state, exchanges, err
	}
	if self.mutation == "uid-reassigned" {
		state.Hotkey = self.evidence.HeadFleets[1].Hotkey
	}
	return state, exchanges, nil
}

// Models one-field binding changes while retaining every other response field,
// making each regression a direct semantic boundary.
func (self *finalFleetAuditTestReader) FleetBinding(ctx context.Context, clientID string, epoch uint64, head ChainHead) (FinalFleetBindingChainState, []FinalRPCExchange, error) {
	defer self.begin("binding-at")()
	if self.mutation == "absent-binding" {
		return FinalFleetBindingChainState{}, nil, errors.New("binding absent")
	}
	state, exchanges, err := self.finalTestChainReader.FleetBinding(ctx, clientID, epoch, head)
	if err != nil {
		return state, exchanges, err
	}
	switch self.mutation {
	case "revoked-binding":
		state.Active, state.Cleaned, state.CleanedAtEpoch = false, true, epoch
	case "stale-generation":
		state.Generation++
	case "swapped-member":
		state.ClientKey = finalTestHex(0xe2)
	case "terminal-valid-cycle-invalid":
		state.Active = true
	}
	return state, exchanges, nil
}

// Constructs a tiny complete cross-product. Its snapshots share neither native
// nor EVM heads, so omitted jobs cannot hide behind another transcript.
func finalFleetAuditFixture(t *testing.T, fleetCount, cycleCount int) *FinalSemanticEvidence {
	t.Helper()
	if fleetCount < 2 || cycleCount < 1 {
		t.Fatal("fleet audit fixture needs at least two fleets and one cycle")
	}
	encodeKey := func(seed byte) string {
		var key [32]byte
		key[0], key[31] = seed, seed^0xff
		encoded, err := ss58.Encode(key, ss58.BittensorPrefix)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	evidence := &FinalSemanticEvidence{Netuid: 521}
	for fleetIndex := 0; fleetIndex < fleetCount; fleetIndex++ {
		fleetID := uint64(fleetIndex + 1)
		members := make([]FinalHeadFleetMemberEvidence, 0, 4)
		for memberIndex := 0; memberIndex < 4; memberIndex++ {
			members = append(members, FinalHeadFleetMemberEvidence{
				ClientID:       fmt.Sprintf("0x%032x", fleetIndex*16+memberIndex+1),
				ClientKey:      finalTestHex(byte(0x80 + fleetIndex*4 + memberIndex)),
				ValidFromEpoch: 10,
				ValidToEpoch:   uint64(10 + cycleCount),
			})
		}
		evidence.HeadFleets = append(evidence.HeadFleets, FinalHeadFleetEvidence{
			FleetID: fleetID, UID: uint16(100 + fleetIndex), Hotkey: encodeKey(byte(0x20 + fleetIndex)), Coldkey: encodeKey(byte(0x40 + fleetIndex)),
			FleetKey: finalTestHex(byte(0x60 + fleetIndex)), CommitmentHash: finalTestHex(byte(0x70 + fleetIndex)), Members: members,
			Generation: 1, MemberCount: len(members), Registered: true, Snapshot: ChainHead{Number: 99, Hash: finalTestHex(99)},
		})
	}
	cycles := make([]FinalCRv4Cycle, 0, cycleCount)
	for cycleIndex := 0; cycleIndex < cycleCount; cycleIndex++ {
		cycles = append(cycles, FinalCRv4Cycle{SettlementEpoch: uint64(10 + cycleIndex), NativeSnapshot: ChainHead{Number: uint64(20 + cycleIndex), Hash: finalTestHex(byte(20 + cycleIndex))}, EVMSnapshot: ChainHead{Number: uint64(120 + cycleIndex), Hash: finalTestHex(byte(120 + cycleIndex))}})
	}
	evidence.Validators = []FinalValidatorIdentityEvidence{{ValidatorID: 1, Cycles: cycles}, {ValidatorID: 2, Cycles: append([]FinalCRv4Cycle(nil), cycles...)}}
	return evidence
}

// Builds a fake retaining established non-fleet methods while exposing fleet
// RPC work to these regressions.
func newFinalFleetAuditTestReader(evidence *FinalSemanticEvidence) *finalFleetAuditTestReader {
	return &finalFleetAuditTestReader{finalTestChainReader: &finalTestChainReader{evidence: evidence}, concurrency: 4}
}

// Proves exact fleet/cycle coverage, deterministic transcript order, bounded
// fanout, and canonical-head mirror confirmation.
func TestFinalSemanticFleetAuditReplaysEveryOrdinaryGeneration(t *testing.T) {
	evidence := finalFleetAuditFixture(t, 2, 2)
	reader := newFinalFleetAuditTestReader(evidence)
	reader.commitmentEntered = make(chan struct{}, reader.concurrency)
	release := make(chan struct{})
	reader.commitmentRelease = release
	type auditResult struct {
		groups []finalFleetAuditExchangeGroup
		err    error
	}
	done := make(chan auditResult, 1)
	go func() {
		groups, err := executeFinalSemanticFleetAudit(context.Background(), evidence, reader)
		done <- auditResult{groups: groups, err: err}
	}()
	for entered := 0; entered < reader.concurrency; entered++ {
		select {
		case <-reader.commitmentEntered:
		case <-time.After(10 * time.Second):
			t.Fatal("fleet audit workers did not reach the explicit commitment barrier")
		}
	}
	close(release)
	var first auditResult
	select {
	case first = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("fleet audit did not finish after releasing its explicit barrier")
	}
	if first.err != nil {
		t.Fatal(first.err)
	}
	counts, maximumInFlight := reader.counts()
	if maximumInFlight != reader.concurrency || counts["native-commitment"] != 4 || counts["mirror"] != 4 || counts["native-uid"] != 4 || counts["binding-at"] != 16 {
		t.Fatalf("fleet audit bounded calls/in-flight=%v/%d, want 4/4/4/16 and %d", counts, maximumInFlight, reader.concurrency)
	}
	if len(first.groups) != 29 {
		t.Fatalf("fleet audit transcript groups=%d, want 29", len(first.groups))
	}
	secondReader := newFinalFleetAuditTestReader(evidence)
	second, err := executeFinalSemanticFleetAudit(context.Background(), evidence, secondReader)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := json.Marshal(first.groups)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("fleet audit transcript ordering depends on worker scheduling")
	}
}

// Changes one state field at a time and proves fail-closed replay at each
// identity and validity boundary.
func TestFinalSemanticFleetAuditRejectsIdentityAndValidityDrift(t *testing.T) {
	cases := []struct {
		name     string
		mutation string
		prepare  func(*FinalSemanticEvidence)
	}{
		{name: "absent binding", mutation: "absent-binding"},
		{name: "revoked binding", mutation: "revoked-binding"},
		{name: "stale generation", mutation: "stale-generation"},
		{name: "swapped member key", mutation: "swapped-member"},
		{name: "wrong native commitment", mutation: "wrong-native-hash"},
		{name: "wrong coordinator mirror", mutation: "wrong-mirror-hash"},
		{name: "replaced commitment", mutation: "replaced-commitment"},
		{name: "UID reassignment", mutation: "uid-reassigned"},
		{name: "terminal valid but cycle invalid", mutation: "terminal-valid-cycle-invalid", prepare: func(evidence *FinalSemanticEvidence) {
			evidence.HeadFleets[0].Members[0].ValidFromEpoch = 11
		}},
	}
	for _, testCase := range cases {
		evidence := finalFleetAuditFixture(t, 2, 1)
		if testCase.prepare != nil {
			testCase.prepare(evidence)
		}
		reader := newFinalFleetAuditTestReader(evidence)
		reader.mutation = testCase.mutation
		if _, err := executeFinalSemanticFleetAudit(context.Background(), evidence, reader); err == nil {
			t.Errorf("%s did not fail closed", testCase.name)
		}
	}
}

// Proves duplicate snapshots collapse before the cross-product and each cache
// key is fetched once under concurrent callers.
func TestFinalSemanticFleetAuditDeduplicatesCyclesAndCachedReads(t *testing.T) {
	evidence := finalFleetAuditFixture(t, 2, 1)
	cycles, err := finalFleetAuditCycles(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 1 {
		t.Fatalf("duplicate validator cycles=%d, want 1", len(cycles))
	}
	cache := &finalFleetAuditReadCache{}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	results := make(chan error, 8)
	var queries atomic.Int32
	for index := 0; index < cap(results); index++ {
		go func() {
			_, _, err := cache.read(context.Background(), "same-exact-head-and-key", func(context.Context) (any, []FinalRPCExchange, error) {
				queries.Add(1)
				started <- struct{}{}
				<-release
				return "cached", nil, nil
			})
			results <- err
		}()
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("cache owner did not enter explicit barrier")
	}
	close(release)
	for index := 0; index < cap(results); index++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	cache.stateLock.Lock()
	entry := cache.entries["same-exact-head-and-key"]
	cache.stateLock.Unlock()
	if entry == nil || entry.value != "cached" {
		t.Fatal("cache did not retain one exact head/key result")
	}
	if queries.Load() != 1 {
		t.Fatalf("exact cache query executions=%d, want 1", queries.Load())
	}
}

// Proves a worker cannot disappear after another concurrent job succeeds.
func TestFinalSemanticFleetAuditRejectsMissingWorkerResult(t *testing.T) {
	err := finalFleetAuditResultsError(context.Background(), []finalFleetAuditJobResult{{Done: true}, {}})
	if err == nil || !strings.Contains(err.Error(), "has no result") {
		t.Fatalf("missing fleet audit worker result was accepted: %v", err)
	}
}

// Proves a v5 public transcript cannot substitute a count-equivalent but
// different ordinary fleet/cycle scope before public-chain reads begin.
func TestFinalPublicFleetAuditSealsExactReplayScope(t *testing.T) {
	evidence := finalFleetAuditFixture(t, 2, 2)
	baseline, err := finalPublicFleetAuditForEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Schema != finalPublicFleetAuditSchema || baseline.OrdinaryFleetGenerations != 2 || baseline.CycleSnapshots != 2 || baseline.ReplayJobs != 4 || baseline.MemberBindings != 16 {
		t.Fatalf("fleet audit summary=%+v, want 2 fleets/2 cycles/4 jobs/16 bindings", baseline)
	}
	if err := verifyFinalPublicFleetAudit(evidence, baseline); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		name string
		edit func(*FinalSemanticEvidence)
		want string
	}{
		{name: "fleet key", edit: func(value *FinalSemanticEvidence) { value.HeadFleets[0].FleetKey = finalTestHex(0xc1) }, want: "differs from sealed projection"},
		{name: "commitment", edit: func(value *FinalSemanticEvidence) { value.HeadFleets[0].CommitmentHash = finalTestHex(0xc2) }, want: "differs from sealed projection"},
		{name: "member key", edit: func(value *FinalSemanticEvidence) { value.HeadFleets[0].Members[0].ClientKey = finalTestHex(0xc3) }, want: "differs from sealed projection"},
		{name: "member validity", edit: func(value *FinalSemanticEvidence) { value.HeadFleets[0].Members[0].ValidToEpoch++ }, want: "differs from sealed projection"},
		{name: "native snapshot", edit: func(value *FinalSemanticEvidence) {
			value.Validators[0].Cycles[0].NativeSnapshot.Hash = finalTestHex(0xc4)
		}, want: "differs from sealed projection"},
		{name: "EVM snapshot", edit: func(value *FinalSemanticEvidence) {
			value.Validators[0].Cycles[0].EVMSnapshot.Hash = finalTestHex(0xc5)
		}, want: "differs from sealed projection"},
		{name: "netuid", edit: func(value *FinalSemanticEvidence) {
			value.Netuid++
		}, want: "differs from sealed projection"},
		{name: "cross-fleet client reuse", edit: func(value *FinalSemanticEvidence) {
			value.HeadFleets[1].Members[0].ClientID = value.HeadFleets[0].Members[0].ClientID
		}, want: "reuses member client"},
	} {
		candidate := finalSemanticClone(t, evidence)
		mutation.edit(candidate)
		if err := verifyFinalPublicFleetAudit(candidate, baseline); err == nil || !strings.Contains(err.Error(), mutation.want) {
			t.Errorf("%s replay-scope mutation was accepted or had wrong error: %v", mutation.name, err)
		}
	}
}

// Covers offline failure: retaining a generic hash cannot rescue a missing or
// tampered scope summary from before P0-2.
func TestFinalPublicFleetAuditFailsClosedOnMissingOrTamperedSummary(t *testing.T) {
	evidence := finalFleetAuditFixture(t, 2, 2)
	baseline, err := finalPublicFleetAuditForEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []FinalPublicFleetAudit{
		{},
		{Schema: finalPublicFleetAuditSchema, OrdinaryFleetGenerations: 2, CycleSnapshots: 2, ReplayJobs: 4, MemberBindings: 16, ProjectionHash: finalTestHex(0xd1)},
		{Schema: finalPublicFleetAuditSchema, OrdinaryFleetGenerations: 2, CycleSnapshots: 2, ReplayJobs: 4, MemberBindings: 20, ProjectionHash: baseline.ProjectionHash},
	} {
		if err := verifyFinalPublicFleetAudit(evidence, candidate); err == nil {
			t.Fatalf("invalid public fleet audit summary was accepted: %+v", candidate)
		}
	}
}

// Proves sealed replay fields cannot be substituted after capture: each is
// exact-equal to the canonical manifest and binding census.
func TestFinalSemanticFleetAuditProjectionBindsTheExistingArtifact(t *testing.T) {
	t.Parallel()
	source, artifacts := finalSemanticFixture(t)
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	load := func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		data, ok := artifacts[locator.URI]
		if !ok {
			return nil, fmt.Errorf("missing fixture artifact %s", locator.URI)
		}
		return append([]byte(nil), data...), nil
	}
	if err := VerifyFinalSemanticArtifacts(context.Background(), draft, load); err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name string
		edit func(*FinalSemanticEvidence)
	}{
		{name: "fleet key", edit: func(evidence *FinalSemanticEvidence) { evidence.HeadFleets[0].FleetKey = finalTestHex(0xef) }},
		{name: "commitment", edit: func(evidence *FinalSemanticEvidence) { evidence.HeadFleets[0].CommitmentHash = finalTestHex(0xee) }},
		{name: "member client key", edit: func(evidence *FinalSemanticEvidence) {
			evidence.HeadFleets[0].Members[0].ClientKey = finalTestHex(0xed)
		}},
		{name: "member validity", edit: func(evidence *FinalSemanticEvidence) { evidence.HeadFleets[0].Members[0].ValidToEpoch++ }},
		{name: "missing member", edit: func(evidence *FinalSemanticEvidence) {
			evidence.HeadFleets[0].Members = evidence.HeadFleets[0].Members[:3]
		}},
	}
	for _, mutation := range mutations {
		tampered := finalSemanticClone(t, draft)
		mutation.edit(tampered)
		resignFinalSemantic(t, tampered)
		if err := VerifyFinalSemanticArtifacts(context.Background(), tampered, load); err == nil || !strings.Contains(err.Error(), "projection") {
			t.Errorf("artifact projection mutation %s was accepted or had wrong error: %v", mutation.name, err)
		}
	}
}
