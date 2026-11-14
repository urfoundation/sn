// Synthetic canonical bodies and exact event bundles exercise original-call
// reconciliation. Helper seals are test data, never production chain proofs.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Independent event construction associates every outcome with index1; index0
// remains another transaction and cannot donate its registration or fee.
func rootRegisterTestEvents(t *testing.T, f *rootRegisterTestFixture, success bool) []byte {
	t.Helper()
	operator, _ := hex.DecodeString(f.config.Action.Policy.Operator[2:])
	hotkey, _ := hex.DecodeString(f.config.Action.Policy.Hotkey[2:])
	count := uint64(2)
	if success {
		count++
	}
	events := rootCompact(count)
	if success {
		events = append(events, rootReceiptEventFixture(t, f.metadata, "SubtensorModule.NeuronRegistered", 1, []byte{0, 0}, []byte{0, 0}, hotkey)...)
	}
	events = append(events, rootReceiptEventFixture(t, f.metadata, "TransactionPayment.TransactionFeePaid", 1, operator, binary.LittleEndian.AppendUint64(nil, 12), make([]byte, 8))...)
	terminal := "System.ExtrinsicFailed"
	if success {
		terminal = "System.ExtrinsicSuccess"
	}
	events = append(events, rootReceiptEventFixture(t, f.metadata, terminal, 1)...)
	return events
}

// Failure deliberately retains newly created ownership lists to prevent tests
// from silently assuming that every failed registration has no state effects.
func rootRegisterTestInclusionObservation(t *testing.T, f *rootRegisterTestFixture, success bool, hash string, number uint64) rootRegisterObservation {
	t.Helper()
	raw, err := json.Marshal(f.input.Observation)
	if err != nil {
		t.Fatal(err)
	}
	var observation rootRegisterObservation
	if err := json.Unmarshal(raw, &observation); err != nil {
		t.Fatal(err)
	}
	a := f.config.Action
	observation.FinalizedNumber, observation.FinalizedHash = number, hash
	operator, _ := hex.DecodeString(a.Policy.Operator[2:])
	hotkey, _ := hex.DecodeString(a.Policy.Hotkey[2:])
	rootRegisterTestSet(t, &observation, f.metadata, "SubtensorModule", "Owner", operator, hotkey)
	for _, name := range []string{"OwnedHotkeys", "StakingHotkeys"} {
		rootRegisterTestSet(t, &observation, f.metadata, "SubtensorModule", name, append([]byte{4}, hotkey...), operator)
	}
	if success {
		rootRegisterTestSeat(t, &observation, f.metadata, 0, a.Policy.Hotkey, a.Policy.Operator, number, 0)
		rootRegisterTestSet(t, &observation, f.metadata, "SubtensorModule", "Delegates", binary.LittleEndian.AppendUint16(nil, 11796), hotkey)
	}
	account := make([]byte, 56)
	binary.LittleEndian.PutUint32(account, a.Nonce+1)
	binary.LittleEndian.PutUint64(account[16:24], 99000)
	rootRegisterTestSet(t, &observation, f.metadata, "System", "Account", account, operator)
	return observation
}

// This retained-evidence helper exercises replay validation without an RPC
// route. The separate canonical fixture below exercises header/body acquisition.
func rootRegisterTestReconciliation(t *testing.T, f *rootRegisterTestFixture, success bool) (rootRegisterReconciliation, []byte) {
	t.Helper()
	a := f.config.Action
	signed, err := a.signed(f.signature(t))
	if err != nil {
		t.Fatal(err)
	}
	events := rootRegisterTestEvents(t, f, success)
	receipt, uid, err := rootDecodeRegisterReceiptEvents(f.metadata, events, 1, 2, a.Policy)
	if err != nil {
		t.Fatal(err)
	}
	block, hash := a.BirthBlock+1, "0x"+strings.Repeat("99", 32)
	receipt.BlockNumber, receipt.BlockHash, receipt.ExtrinsicIndex, receipt.RawExtrinsic = block, hash, 1, "0x"+hex.EncodeToString(signed)
	receipt.ExecutionRuntimeVersion, receipt.ExecutionCodeHash, receipt.ExecutionMetadataHash = a.Policy.RuntimeVersion, a.Policy.RuntimeCodeHash, a.Policy.RuntimeMetadataHash
	nonce := a.Nonce + 1
	readback := rootRegisterTestInclusionObservation(t, f, success, hash, block)
	evidence := rootRegisterReconciliation{FinalizedNumber: block, FinalizedHash: hash, AnchorHash: a.BirthHash, CheckedFrom: a.BirthBlock + 1, CheckedThrough: block, AccountNonce: &nonce, Receipt: &receipt, BodyCount: 2, RawEvents: "0x" + hex.EncodeToString(events), Readback: &readback}
	if uid != nil {
		evidence.Registration = &rootSeatExpectation{Uid: *uid, RegistrationBlock: block}
	}
	request, err := newRootRegisterSigningRequest(f.config, f.key, f.input.Metadata, f.input.LedgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.validate(request, signed); err != nil {
		t.Fatal(err)
	}
	return evidence, signed
}

// Canonical synthetic body/header chains share the existing read-only fixture.
// The base fault supplies exact paged key enumeration; test-specific faults must
// delegate to it for unrelated methods instead of losing census behavior.
func rootRegisterTestChain(t *testing.T, f *rootRegisterTestFixture, count int, included bool) (*rootRegisterCanonicalChain, *rootReceiptFixture, rootRegisterSigningRequest, []byte) {
	t.Helper()
	a := f.config.Action
	fixture := &rootReceiptFixture{metadata: f.metadata, metadataHex: f.input.Metadata, profile: a.Policy.runtime(), headers: map[string]rootReceiptHeader{}, byHeight: map[uint64]string{}, bodies: map[string][]string{}, storageKVs: map[string]string{}, evmHex: "0x3c4", runtimeKVs: map[string]rootReceiptProfile{}, metadataKVs: map[string]string{}, counts: map[string]int{}}
	fixture.action.Scope.NativeChain, fixture.action.Scope.GenesisHash = a.Policy.NativeChain, a.Policy.GenesisHash
	anchor, hash := rootReceiptHeaderFixture(t, "0x"+strings.Repeat("9a", 32), a.BirthBlock, nil, false)
	fixture.headers[hash], fixture.byHeight[a.BirthBlock] = anchor, hash
	f.input.Action.BirthHash, f.input.Observation.FinalizedHash = hash, hash
	f.input.Observation.ContentHash = ""
	f.input.Observation.ContentHash = rootObjectHash(f.input.Observation)
	f.input.Action.ObservationHash = f.input.Observation.ContentHash
	var err error
	f.config, err = prepareRootRegisterPlan(f.input)
	if err != nil {
		t.Fatal(err)
	}
	f.approve()
	a = f.config.Action
	signed, err := a.signed(f.signature(t))
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= count; index++ {
		var body [][]byte
		if index == 1 && included {
			body = [][]byte{{8, 4, 0}, signed}
		}
		header, next := rootReceiptHeaderFixture(t, hash, a.BirthBlock+uint64(index), body, false)
		fixture.headers[next], fixture.byHeight[a.BirthBlock+uint64(index)] = header, next
		fixture.bodies[next] = []string{}
		for _, raw := range body {
			fixture.bodies[next] = append(fixture.bodies[next], "0x"+hex.EncodeToString(raw))
		}
		hash = next
	}
	fixture.finalized = hash
	observation := f.input.Observation
	if included {
		observation = rootRegisterTestInclusionObservation(t, f, true, fixture.byHeight[a.BirthBlock+1], a.BirthBlock+1)
	}
	for _, row := range observation.Storage {
		if row.RawStorage != nil {
			fixture.storageKVs[row.Key] = *row.RawStorage
		}
	}
	eventKey, err := types.CreateStorageKey(f.metadata, "System", "Events")
	if err != nil {
		t.Fatal(err)
	}
	fixture.storageKVs[eventKey.Hex()] = "0x" + hex.EncodeToString(rootRegisterTestEvents(t, f, true))
	fixture.fault = func(method string, params []json.RawMessage, _ int) (any, bool) {
		if method != "state_getKeysPaged" {
			return nil, false
		}
		var prefix, start string
		var size int
		if len(params) != 4 || json.Unmarshal(params[0], &prefix) != nil || json.Unmarshal(params[1], &size) != nil || json.Unmarshal(params[2], &start) != nil && string(params[2]) != "null" || size < 1 || size > 128 {
			return nil, true
		}
		keys := []string{}
		for key := range fixture.storageKVs {
			if strings.HasPrefix(key, prefix) && key > start {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		return keys[:min(size, len(keys))], true
	}
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	t.Cleanup(server.Close)
	client, err := newRpcClient(server.URL, 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	native, err := newRootCanonicalChain(client, a.Policy.identity(), []rootReceiptProfile{a.Policy.runtime()})
	if err != nil {
		t.Fatal(err)
	}
	request, err := newRootRegisterSigningRequest(f.config, f.key, f.input.Metadata, f.input.LedgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	return &rootRegisterCanonicalChain{rootCanonicalChain: native, config: f.config, key: f.key}, fixture, request, signed
}

// Exact original body/event replay proves a successful registration generation
// independently of the later observed burn quote; it performs no submission.
func TestRootRegisterCanonicalReceiptAndExactGeneration(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	chain, fixture, request, signed := rootRegisterTestChain(t, f, 3, true)
	evidence, err := chain.reconcile(t.Context(), request, signed)
	if err != nil || evidence.Receipt == nil || evidence.Readback == nil || evidence.Receipt.ActualFeeRao != 12 || evidence.ActualBurnRao != nil || rootRegisterPhase(request, evidence) != "finalized" {
		t.Fatalf("exact root registration not recovered: %+v %v", evidence, err)
	}
	seat := rootRegisterObservedSeat(request, evidence)
	if seat == nil || seat.Uid != 0 || seat.RegistrationBlock != f.config.Action.BirthBlock+1 {
		t.Fatal("event did not join exact inclusion generation")
	}
	fixture.stateLock.Lock()
	sends := fixture.counts["author_submitExtrinsic"]
	fixture.stateLock.Unlock()
	if sends != 0 {
		t.Fatal("receipt recovery submitted a transaction")
	}
}

// A failed call retains account creation and its actual fee, without acquiring
// a root seat or claiming that its zero/missing burn was established by events.
func TestRootRegisterFailureRetainsOwnershipSideEffects(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	evidence, signed := rootRegisterTestReconciliation(t, f, false)
	request, err := newRootRegisterSigningRequest(f.config, f.key, f.input.Metadata, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.validate(request, signed); err != nil {
		t.Fatal(err)
	}
	facts, err := evidence.Readback.eligibility(f.config.Action.Policy, f.metadata)
	if err != nil || !facts.OwnerPresent || facts.ExistingOwner != f.config.Action.Policy.Operator || len(facts.OwnedHotkeys) != 1 || len(facts.StakingHotkeys) != 1 || facts.ExistingSeat != nil || evidence.Registration != nil || evidence.ActualBurnRao != nil || rootRegisterObservedSeat(request, evidence) != nil || rootRegisterPhase(request, evidence) != "dispatch-failed" {
		t.Fatalf("failed registration lost effects or became success: %+v %v", facts, err)
	}
}

// Retained checksums cannot transplant another event phase/generation, invent a
// numeric burn, or turn a later slot reuse into the original registration seat.
func TestRootRegisterReceiptRejectsEventAndGenerationForgery(t *testing.T) {
	for _, change := range []string{"other-phase", "other-netuid", "other-hotkey", "duplicate", "forged-uid", "numeric-burn", "later-generation"} {
		f := newRootRegisterTestFixture(t, false)
		evidence, signed := rootRegisterTestReconciliation(t, f, true)
		request, err := newRootRegisterSigningRequest(f.config, f.key, f.input.Metadata, "")
		if err != nil {
			t.Fatal(err)
		}
		events, _ := hex.DecodeString(evidence.RawEvents[2:])
		switch change {
		case "other-phase":
			binary.LittleEndian.PutUint32(events[2:6], 0)
		case "other-netuid":
			events[8] = 25
		case "other-hotkey":
			events[12] ^= 1
		case "duplicate":
			hotkey, _ := hex.DecodeString(f.config.Action.Policy.Hotkey[2:])
			events[0] = 16
			events = append(events, rootReceiptEventFixture(t, f.metadata, "SubtensorModule.NeuronRegistered", 1, []byte{0, 0}, []byte{0, 0}, hotkey)...)
		case "forged-uid":
			evidence.Registration.Uid++
		case "numeric-burn":
			burn := uint64(10)
			evidence.ActualBurnRao = &burn
		case "later-generation":
			rootRegisterTestSet(t, evidence.Readback, f.metadata, "SubtensorModule", "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, f.config.Action.BirthBlock), []byte{0, 0}, []byte{0, 0})
		}
		evidence.RawEvents, evidence.Receipt.EventHash = "0x"+hex.EncodeToString(events), rootExtrinsicHash(events)
		err = evidence.validate(request, signed)
		if change == "later-generation" {
			if err != nil || rootRegisterPhase(request, evidence) != "finalized-state-conflict" || rootRegisterObservedSeat(request, evidence) != nil {
				t.Fatal("another generation was promoted to original seat", err)
			}
		} else if err == nil {
			t.Fatalf("%s event forgery accepted", change)
		}
	}
}

// A later registration can reuse the same hotkey/UID/block tuple. Its original
// financial receipt remains valid while the bootstrap handoff is withheld.
func TestRootRegisterSameBlockReregistrationCannotReuseGeneration(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	evidence, signed := rootRegisterTestReconciliation(t, f, true)
	request, err := newRootRegisterSigningRequest(f.config, f.key, f.input.Metadata, "")
	if err != nil {
		t.Fatal(err)
	}
	events, _ := hex.DecodeString(evidence.RawEvents[2:])
	hotkey, _ := hex.DecodeString(f.config.Action.Policy.Hotkey[2:])
	events[0] = 16
	events = append(events, rootReceiptEventFixture(t, f.metadata, "SubtensorModule.NeuronRegistered", 2, []byte{0, 0}, []byte{0, 0}, hotkey)...)
	evidence.BodyCount, evidence.RawEvents, evidence.Receipt.EventHash = 3, "0x"+hex.EncodeToString(events), rootExtrinsicHash(events)
	if err := evidence.validate(request, signed); err != nil {
		t.Fatal("a later registration erased the original financial receipt", err)
	}
	if rootRegisterObservedSeat(request, evidence) != nil || rootRegisterPhase(request, evidence) != "finalized-state-conflict" {
		t.Fatal("same-block re-registration became the original exact generation")
	}
}

// Complete absence distinguishes expiry from foreign nonce consumption, while
// unavailable state, a changed body or a regressing finality tag fail closed.
func TestRootRegisterCanonicalAbsenceAndReadbackGaps(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	chain, fixture, request, signed := rootRegisterTestChain(t, f, 8, false)
	evidence, err := chain.reconcile(context.Background(), request, signed)
	if err != nil || rootRegisterPhase(request, evidence) != "expired" || evidence.CheckedThrough != 107 {
		t.Fatal("full absence did not expire original mortal action", err)
	}
	operator, _ := hex.DecodeString(f.config.Action.Policy.Operator[2:])
	key, _ := types.CreateStorageKey(f.metadata, "System", "Account", operator)
	fixture.stateLock.Lock()
	account, _ := hex.DecodeString(fixture.storageKVs[key.Hex()][2:])
	binary.LittleEndian.PutUint32(account, f.config.Action.Nonce+1)
	fixture.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(account)
	fixture.stateLock.Unlock()
	evidence, err = chain.reconcile(t.Context(), request, signed)
	if err != nil || rootRegisterPhase(request, evidence) != "nonce-conflict" {
		t.Fatal("foreign nonce consumption became root registration", err)
	}
	fixture.stateLock.Lock()
	fixture.bodies[fixture.byHeight[101]] = []string{"0x080400"}
	fixture.stateLock.Unlock()
	if _, err := chain.reconcile(t.Context(), request, signed); err == nil {
		t.Fatal("changed body was skipped during mortal coverage")
	}
}

// A lagging closing tag retries only coverage under the original deadline.
// Original bodies, events and inclusion readback are neither restarted nor moved.
func TestRootRegisterClosingFinalityRetriesWithoutReplayingBodies(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	chain, fixture, request, signed := rootRegisterTestChain(t, f, 3, true)
	baseFault := fixture.fault
	fixture.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
		if method == "chain_getFinalizedHead" && count == 2 {
			return fixture.byHeight[f.config.Action.BirthBlock+2], true
		}
		return baseFault(method, params, count)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	waits, storageAtWait := 0, 0
	chain.client.retryWait = func(waitCtx context.Context, _ time.Duration) error {
		waits++
		if actual, ok := waitCtx.Deadline(); !ok || actual != deadline {
			t.Fatal("closing coverage reset the original read budget")
		}
		fixture.stateLock.Lock()
		storageAtWait = fixture.counts["state_getStorage"]
		fixture.stateLock.Unlock()
		return nil
	}
	evidence, err := chain.reconcile(ctx, request, signed)
	fixture.stateLock.Lock()
	bodies, storage := fixture.counts["chain_getBlock"], fixture.counts["state_getStorage"]
	fixture.stateLock.Unlock()
	if err != nil || waits != 1 || bodies != 3 || storage != storageAtWait || evidence.FinalizedNumber != f.config.Action.BirthBlock+3 || rootRegisterPhase(request, evidence) != "finalized" {
		t.Fatalf("lagging closure repeated or changed original work: waits=%d bodies=%d storage=%d/%d evidence=%+v err=%v", waits, bodies, storageAtWait, storage, evidence, err)
	}
}

// A lower tag never masks an actual retained-hash contradiction, and canceled
// unavailable coverage preserves its original cause without publishing evidence.
func TestRootRegisterClosingFinalityConflictAndCancellation(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		f := newRootRegisterTestFixture(t, false)
		chain, fixture, request, signed := rootRegisterTestChain(t, f, 3, true)
		baseFault := fixture.fault
		fixture.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
			if method == "chain_getFinalizedHead" && count >= 2 {
				return fixture.byHeight[f.config.Action.BirthBlock+2], true
			}
			if conflict && method == "chain_getBlockHash" {
				var height uint64
				json.Unmarshal(params[0], &height)
				if height == f.config.Action.BirthBlock+3 {
					return f.config.Action.Policy.GenesisHash, true
				}
			}
			return baseFault(method, params, count)
		}
		cause := errors.New("synthetic root registration read owner canceled")
		ctx, cancel := context.WithCancelCause(t.Context())
		waits := 0
		chain.client.retryWait = func(waitCtx context.Context, _ time.Duration) error {
			waits++
			cancel(cause)
			return waitCtx.Err()
		}
		evidence, err := chain.reconcile(ctx, request, signed)
		cancel(nil)
		if evidence.Receipt != nil || err == nil {
			t.Fatal("unclosed finality published a receipt")
		}
		if conflict {
			if waits != 0 || !errors.Is(err, errRpcIntegrity) {
				t.Fatal("actual canonical conflict retried as lower-tag availability", waits, err)
			}
		} else if waits != 1 || !errors.Is(err, errRpcObservationUnavailable) || !errors.Is(err, context.Canceled) || !errors.Is(err, cause) || errors.Is(err, errRpcIntegrity) {
			t.Fatal("lower tag lost original unavailable/cancellation cause", waits, err)
		}
	}
}

// A post-state/runtime outage preserves an exact original financial outcome.
// Recovery enriches its same inclusion hash and cannot change the registration.
func TestRootRegisterReceiptSurvivesReadbackOutage(t *testing.T) {
	f := newRootRegisterTestFixture(t, true)
	chain, fixture, request, signed := rootRegisterTestChain(t, f, 3, true)
	baseFault := fixture.fault
	ownerKey, _ := types.CreateStorageKey(f.metadata, "SubtensorModule", "SubnetOwner", []byte{25, 0})
	fixture.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
		if method == "state_getStorage" {
			var key string
			json.Unmarshal(params[0], &key)
			if key == ownerKey.Hex() {
				return "0x00", true
			}
		}
		return baseFault(method, params, count)
	}
	first, err := chain.reconcile(t.Context(), request, signed)
	if err != nil || first.Receipt == nil || first.Readback != nil || first.ReadbackIssue == "" || first.Registration == nil || rootRegisterPhase(request, first) != "finalized-readback-pending" {
		t.Fatalf("readback outage erased original receipt: %+v %v", first, err)
	}
	fixture.fault = baseFault
	second, err := chain.reconcile(t.Context(), request, signed)
	if err != nil || second.Receipt == nil || second.Readback == nil || rootObjectHash(*first.Receipt) != rootObjectHash(*second.Receipt) || first.RawEvents != second.RawEvents || rootObjectHash(first.Registration) != rootObjectHash(second.Registration) || rootRegisterPhase(request, second) != "finalized" {
		t.Fatal("readback recovery replaced its original event or financial receipt", err)
	}
}
