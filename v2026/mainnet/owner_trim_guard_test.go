// Two immutable synthetic blocks exercise historical reconstruction, current
// drift and interrupted rechecks through the real bounded RPC census reader.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Configuration is immutable during a call; counters alone need the lock.
type ownerTrimGuardFixture struct {
	before, after *rootRpcFixture
	afterHeader   rootReceiptHeader
	afterHash     string
	afterNumber   uint64
	stateLock     sync.Mutex
	methodCounts  map[string]int
	faultMethod   string
	faultCount    int
	faultBlock    string
	fault         string
	cancel        context.CancelFunc
	inflight      int
}

// Retain canonical JSON exactly as the command does; private decoded fields
// disappear, so a guard cannot accidentally trust their in-memory values.
func newOwnerTrimGuardFixture(t *testing.T, partial bool) (*rpcClient, *ownerTrimGuardFixture, subnetCensusPolicy, string, ownerTrimPlan) {
	t.Helper()
	client, before, policy := newSubnetFixture(t)
	if partial {
		born := uint64(95)
		policy.Remove[0].RegistrationBlock = &born
		before.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, born), []byte{25, 0}, []byte{4, 0})
	}
	policyRaw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(policyRaw)
	policyHash := "sha256:" + hex.EncodeToString(digest[:])
	preview, err := client.readSubnetPreview(t.Context(), policy, policyHash)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil || plan.Best == nil {
		t.Fatalf("fixture plan: %v %+v", err, plan)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := decodePlanJson(raw, &plan); err != nil {
		t.Fatal(err)
	}
	_, after, _ := newSubnetFixture(t)
	after.storageKVs = maps.Clone(before.storageKVs)
	header, hash := rootReceiptHeaderFixture(t, testFinalizedHash, 101, nil, false)
	fixture := &ownerTrimGuardFixture{before: before, after: after, afterHeader: header, afterHash: hash, afterNumber: 101, methodCounts: map[string]int{}}
	client.httpClient.Transport = roundTripFunc(fixture.roundTrip)
	return client, fixture, policy, policyHash, plan
}

// Replies retain the request ID and refuse any method absent from the read-only
// base fixture. The test transport contains no transaction endpoint.
func ownerTrimTestReply(result any) (*http.Response, error) {
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(raw))}, err
}

// Block-specific storage is immutable; translating the second block hash lets
// the original fixture keep enforcing pinned requests and census integrity.
func (self *ownerTrimGuardFixture) roundTrip(request *http.Request) (*http.Response, error) {
	var call struct {
		JsonRpc string            `json:"jsonrpc"`
		Id      int               `json:"id"`
		Method  string            `json:"method"`
		Params  []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		return nil, err
	}
	block := ""
	if len(call.Params) != 0 {
		_ = json.Unmarshal(call.Params[len(call.Params)-1], &block)
	}
	key := call.Method
	if call.Method == "chain_getBlockHash" {
		key += ":" + string(call.Params[0])
	}
	self.stateLock.Lock()
	self.methodCounts[key]++
	count := self.methodCounts[key]
	self.inflight++
	self.stateLock.Unlock()
	defer func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.inflight--
	}()
	if key == self.faultMethod && (self.faultCount == 0 || count == self.faultCount) && (self.faultBlock == "" || block == self.faultBlock) {
		if self.fault == "cancel" {
			self.cancel()
			return nil, request.Context().Err()
		}
		if strings.HasPrefix(self.fault, "missing-header-") {
			raw, _ := json.Marshal(identityTestHeader())
			var header map[string]any
			_ = json.Unmarshal(raw, &header)
			field := strings.TrimPrefix(self.fault, "missing-header-")
			if field == "logs" {
				delete(header["digest"].(map[string]any), "logs")
			} else {
				delete(header, field)
			}
			return ownerTrimTestReply(header)
		}
		return ownerTrimTestReply("0x" + strings.Repeat("ab", 32))
	}
	switch call.Method {
	case "chain_getFinalizedHead":
		return ownerTrimTestReply(self.afterHash)
	case "chain_getHeader":
		if block == self.afterHash {
			return ownerTrimTestReply(self.afterHeader)
		}
		if block == testFinalizedHash {
			return ownerTrimTestReply(identityTestHeader())
		}
		return nil, errors.New("unexpected guard fixture header")
	case "chain_getBlockHash":
		switch string(call.Params[0]) {
		case "0":
			return ownerTrimTestReply(testGenesisHash)
		case "100":
			return ownerTrimTestReply(testFinalizedHash)
		case fmt.Sprint(self.afterNumber):
			return ownerTrimTestReply(self.afterHash)
		default:
			return nil, errors.New("unexpected guard fixture height")
		}
	}
	selected := self.before
	if strings.HasPrefix(call.Method, "state_") {
		if block == self.afterHash {
			selected = self.after
			call.Params[len(call.Params)-1], _ = json.Marshal(testFinalizedHash)
		} else if block != testFinalizedHash {
			return nil, fmt.Errorf("%s escaped the guard's block pair", call.Method)
		}
	}
	raw, err := json.Marshal(call)
	if err != nil {
		return nil, err
	}
	forwarded := request.Clone(request.Context())
	forwarded.Body = io.NopCloser(bytes.NewReader(raw))
	return selected.roundTrip(forwarded)
}

// All workers must have returned before a canceled observation is discarded.
func (self *ownerTrimGuardFixture) counts() (int, map[string]int) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.inflight, maps.Clone(self.methodCounts)
}

// A stable partial selection retains its immune old miner without claiming
// completion, atomically protected dispatch or any source/custody authority.
func TestOwnerTrimGuardRechecksExactPartialPlanWithoutExecutionAuthority(t *testing.T) {
	client, fixture, policy, policyHash, plan := newOwnerTrimGuardFixture(t, true)
	result, err := client.readOwnerTrimGuard(t.Context(), policy, policyHash, plan, "recheck")
	if err != nil || !result.ObservationMatches || result.ResetReady || result.ApplyAuthority || result.FullResetCompleted ||
		len(result.ExpectedResidual) != 1 || result.ExpectedResidual[0].Hotkey != policy.Remove[0].Hotkey ||
		result.PlanContentHash != plan.ContentHash || result.BaselineCensusHash != plan.Census.ContentHash ||
		result.CurrentCensus.Observation.Identity.FinalizedHash != fixture.afterHash ||
		!slices.Contains(result.ExecutionBlockers, "OWNER_TRIM_EXECUTION_TIME_SELECTION_SAFETY_NOT_ESTABLISHED") {
		t.Fatalf("partial recheck acquired authority or lost residuals: %+v %v", result, err)
	}
	_, counts := fixture.counts()
	if counts["chain_getFinalizedHead"] != 9 || counts["chain_getBlockHash:100"] != 5 || counts["chain_getBlockHash:101"] != 15 {
		t.Fatalf("baseline/current/final checks missing: %v", counts)
	}
}

// Rechecking detects unsafe drift even when the ranked capacity happens to stay
// unchanged. The retained plan is never replaced with a newly favorable plan.
func TestOwnerTrimGuardRefusesSelectionInputsThatChangeAfterPlanning(t *testing.T) {
	for _, change := range []string{"emission-safe-order", "emission-protected", "immunity", "permit", "owner-custody", "generation", "registration", "pow-registration", "capacity", "root", "subnet-generation", "cooldown", "admin-window"} {
		t.Run(change, func(t *testing.T) {
			client, fixture, policy, policyHash, plan := newOwnerTrimGuardFixture(t, false)
			want := ""
			switch change {
			case "emission-safe-order", "emission-protected":
				emissions := []uint64{100, 90, 80, 70, 2, 1}
				want = "OWNER_TRIM_EMISSIONS_CHANGED"
				if change == "emission-protected" {
					emissions[3] = 0
				}
				data := []byte{}
				for _, emission := range emissions {
					data = binary.LittleEndian.AppendUint64(data, emission)
				}
				fixture.after.set(t, "Emission", subnetTestVector(data, 8), []byte{25, 0})
			case "immunity":
				fixture.after.set(t, "ImmunityPeriod", []byte{60, 0}, []byte{25, 0})
				want = "OWNER_TRIM_IMMUNITY_CHANGED"
			case "permit":
				fixture.after.set(t, "ValidatorPermit", subnetTestVector([]byte{0, 0, 1, 0, 1, 0}, 1), []byte{25, 0})
				want = "OWNER_TRIM_PROTECTED_ROLE_OR_CUSTODY_CHANGED"
			case "owner-custody":
				fixture.after.set(t, "Owner", bytes.Repeat([]byte{0x77}, 32), bytes.Repeat([]byte{0x45}, 32))
				want = "OWNER_TRIM_PROTECTED_ROLE_OR_CUSTODY_CHANGED"
			case "generation":
				fixture.after.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 101), []byte{25, 0}, []byte{4, 0})
				want = "OWNER_TRIM_MEMBERSHIP_OR_GENERATION_CHANGED"
			case "registration", "pow-registration":
				name := "NetworkRegistrationAllowed"
				if change == "pow-registration" {
					name = "NetworkPowRegistrationAllowed"
				}
				fixture.after.set(t, name, []byte{1}, []byte{25, 0})
				want = "OWNER_TRIM_COMPETING_REGISTRATION_OR_REENTRY_NOT_FENCED"
			case "capacity":
				fixture.after.set(t, "MaxAllowedUids", []byte{7, 0}, []byte{25, 0})
				want = "OWNER_TRIM_CAPACITY_CHANGED"
			case "root":
				fixture.after.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 42), []byte{0, 0}, []byte{1, 0})
				want = "OWNER_TRIM_EXCLUDED_ROOT_GENERATIONS_CHANGED"
			case "subnet-generation":
				fixture.after.set(t, "RegisteredSubnetCounter", binary.LittleEndian.AppendUint64(nil, 4), []byte{25, 0})
				want = "OWNER_TRIM_SUBNET_OWNER_OR_GENERATION_CHANGED"
			case "cooldown":
				fixture.after.set(t, "TransactionKeyLastBlock", binary.LittleEndian.AppendUint64(nil, 100), bytes.Repeat([]byte{0x61}, 32), []byte{25, 0}, []byte{9, 0})
				want = "OWNER_TRIM_TIMING_CHANGED"
			case "admin-window":
				fixture.after.set(t, "PendingEpochAt", binary.LittleEndian.AppendUint64(nil, 102), []byte{25, 0})
				want = "OWNER_TRIM_TIMING_CHANGED"
			}
			result, err := client.readOwnerTrimGuard(t.Context(), policy, policyHash, plan, "recheck")
			if err != nil || result.ObservationMatches || result.ApplyAuthority || !slices.Contains(result.ComparisonBlockers, want) {
				t.Fatalf("%s escaped fixed-plan recheck: %+v %v", change, result, err)
			}
		})
	}
}

// An expiry can make an additional old miner removable. The original one-miner
// approval still cannot silently expand to that two-miner selection.
func TestOwnerTrimGuardDoesNotExpandPartialSelectionAfterImmunityExpiry(t *testing.T) {
	client, fixture, policy, policyHash, plan := newOwnerTrimGuardFixture(t, true)
	fixture.afterNumber = 105
	fixture.afterHeader, fixture.afterHash = rootReceiptHeaderFixture(t, testFinalizedHash, 105, nil, false)
	result, err := client.readOwnerTrimGuard(t.Context(), policy, policyHash, plan, "recheck")
	if err != nil || result.ObservationMatches || len(result.ExpectedResidual) != 1 ||
		!slices.Contains(result.ComparisonBlockers, "OWNER_TRIM_REVIEWED_SELECTION_OR_RESIDUAL_CHANGED") {
		t.Fatalf("expired partial plan expanded: %+v %v", result, err)
	}
}

// A historical read cannot treat a canonical best-chain block as finalized
// merely because the supplied plan names it or its header hash is valid.
func TestOwnerTrimGuardRejectsBaselineBeyondFinalizedHead(t *testing.T) {
	client, fixture, policy, policyHash, plan := newOwnerTrimGuardFixture(t, false)
	fixture.afterNumber = 99
	fixture.afterHeader, fixture.afterHash = rootReceiptHeaderFixture(t, testGenesisHash, 99, nil, false)
	result, err := client.readOwnerTrimGuard(t.Context(), policy, policyHash, plan, "recheck")
	if !errors.Is(err, errRpcIntegrity) || result.ContentHash != "" || fixture.after.count("state_getStorage") != 0 {
		t.Fatalf("nonfinalized baseline admitted: %+v %v", result, err)
	}
}

// The two real headers deliberately share roots and an empty digest. Missing
// historical fields must not inherit those values from the already-read head.
func TestOwnerTrimGuardRejectsIncompleteHistoricalHeader(t *testing.T) {
	for _, field := range []string{"stateRoot", "extrinsicsRoot", "digest", "logs"} {
		t.Run(field, func(t *testing.T) {
			client, fixture, policy, policyHash, plan := newOwnerTrimGuardFixture(t, false)
			fixture.faultMethod, fixture.faultBlock, fixture.fault = "chain_getHeader", testFinalizedHash, "missing-header-"+field
			result, err := client.readOwnerTrimGuard(t.Context(), policy, policyHash, plan, "recheck")
			if !errors.Is(err, errRpcIntegrity) || result.ContentHash != "" || fixture.after.count("state_getStorage") != 0 {
				t.Fatalf("omitted historical %s inherited current evidence: matches=%t err=%v", field, result.ObservationMatches, err)
			}
		})
	}
}

// A canonical anchor and a stable latest finalized head are independent gates.
// Exact runtime/code/metadata changes refuse before reading changed storage.
func TestOwnerTrimGuardRejectsStaleForkedAndChangedRuntimeEvidence(t *testing.T) {
	for _, change := range []string{"latest", "old-canonical", "current-canonical", "metadata", "code", "runtime", "header"} {
		t.Run(change, func(t *testing.T) {
			client, fixture, policy, policyHash, plan := newOwnerTrimGuardFixture(t, false)
			switch change {
			case "latest":
				fixture.faultMethod, fixture.faultCount = "chain_getFinalizedHead", 9
			case "old-canonical":
				fixture.faultMethod, fixture.faultCount = "chain_getBlockHash:100", 5
			case "current-canonical":
				fixture.faultMethod, fixture.faultCount = "chain_getBlockHash:101", 15
			case "metadata":
				fixture.after.metadataHex = "0x00"
			case "code":
				fixture.after.policy.RuntimeCodeHash = "0x" + strings.Repeat("ab", 32)
			case "runtime":
				fixture.after.version.StateVersion++
			case "header":
				fixture.afterHeader.StateRoot = testGenesisHash
			}
			result, err := client.readOwnerTrimGuard(t.Context(), policy, policyHash, plan, "recheck")
			if !errors.Is(err, errRpcIntegrity) || result.ContentHash != "" || result.CurrentCensus.ContentHash != "" {
				t.Fatalf("%s published incomplete evidence: %+v %v", change, result, err)
			}
			if (change == "metadata" || change == "code" || change == "runtime") && fixture.after.count("state_getStorage") != 0 {
				t.Fatal("changed runtime reached current census storage")
			}
		})
	}
}

// Rehashing an altered residual or historical fact cannot create a new baseline.
func TestOwnerTrimGuardRebuildsImportedPlanInsteadOfTrustingItsHashes(t *testing.T) {
	for _, change := range []string{"unsigned-call", "historical-census", "residual", "policy", "authority"} {
		t.Run(change, func(t *testing.T) {
			client, fixture, policy, policyHash, plan := newOwnerTrimGuardFixture(t, true)
			switch change {
			case "unsigned-call":
				plan.Best.UnsignedCallHex = "0x000000000000"
			case "historical-census":
				plan.Census.Observation.Seats[5].EmissionRao = "0"
				plan.Census, _ = sealSubnetPreview(plan.Census.Observation)
			case "residual":
				plan.Residual = nil
			case "policy":
				policyHash = "sha256:" + strings.Repeat("ab", 32)
			case "authority":
				plan.ApplyAuthority = true
			}
			plan.ContentHash, _ = ownerTrimPlanHash(plan)
			result, err := client.readOwnerTrimGuard(t.Context(), policy, policyHash, plan, "recheck")
			if err == nil || result.ContentHash != "" {
				t.Fatalf("%s rehash accepted: %+v %v", change, result, err)
			}
			_, counts := fixture.counts()
			if (change == "policy" || change == "authority") && len(counts) != 0 {
				t.Fatal("invalid plan reached RPC")
			}
		})
	}
}

// Interrupt each useful phase after work has started; no cancellation returns a
// previously good partial observation, and every storage worker is joined.
func TestOwnerTrimGuardInterruptedRecheckPublishesNothing(t *testing.T) {
	for _, phase := range []string{"baseline-storage", "current-storage", "final-head"} {
		t.Run(phase, func(t *testing.T) {
			client, fixture, policy, policyHash, plan := newOwnerTrimGuardFixture(t, false)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fixture.fault, fixture.cancel = "cancel", cancel
			fixture.faultMethod, fixture.faultBlock = "state_getStorage", testFinalizedHash
			if phase == "current-storage" {
				fixture.faultBlock = fixture.afterHash
			} else if phase == "final-head" {
				fixture.faultMethod, fixture.faultBlock, fixture.faultCount = "chain_getFinalizedHead", "", 3
			}
			result, err := client.readOwnerTrimGuard(ctx, policy, policyHash, plan, "recheck")
			inflight, _ := fixture.counts()
			if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(result, ownerTrimGuard{}) || inflight != 0 {
				t.Fatalf("%s leaked result/workers: %+v inflight=%d %v", phase, result, inflight, err)
			}
		})
	}
}

// Rewrite explicit fixture memberships, preserving each supplied registration.
// This models post-state only; no trim implementation is invoked by the test.
func ownerTrimSetPostCensus(t *testing.T, fixture *rootRpcFixture, seats []subnetSeat, capacity uint16) {
	t.Helper()
	netuidArg := []byte{25, 0}
	for index := 0; index < 6; index++ {
		uidArg := binary.LittleEndian.AppendUint16(nil, uint16(index))
		hotkey := bytes.Repeat([]byte{byte(0x41 + index)}, 32)
		for _, name := range []string{"Keys", "BlockAtRegistration"} {
			subnetTestDelete(t, fixture, name, netuidArg, uidArg)
		}
		subnetTestDelete(t, fixture, "Uids", netuidArg, hotkey)
		subnetTestDelete(t, fixture, "IsNetworkMember", hotkey, netuidArg)
	}
	active, permits, emissions := []byte{}, []byte{}, []byte{}
	for index, seat := range seats {
		uidArg := binary.LittleEndian.AppendUint16(nil, uint16(index))
		hotkey, _ := hex.DecodeString(seat.Hotkey[2:])
		coldkey, _ := hex.DecodeString(seat.Coldkey[2:])
		fixture.set(t, "Keys", hotkey, netuidArg, uidArg)
		fixture.set(t, "Uids", uidArg, netuidArg, hotkey)
		fixture.set(t, "Owner", coldkey, hotkey)
		fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, seat.RegistrationBlock), netuidArg, uidArg)
		fixture.set(t, "IsNetworkMember", []byte{1}, hotkey, netuidArg)
		active, permits = append(active, 1), append(permits, 0)
		if seat.ValidatorPermit {
			permits[len(permits)-1] = 1
		}
		emissions = binary.LittleEndian.AppendUint64(emissions, uint64(100-index))
	}
	fixture.set(t, "SubnetworkN", binary.LittleEndian.AppendUint16(nil, uint16(len(seats))), netuidArg)
	fixture.set(t, "MaxAllowedUids", binary.LittleEndian.AppendUint16(nil, capacity), netuidArg)
	fixture.set(t, "Active", subnetTestVector(active, 1), netuidArg)
	fixture.set(t, "ValidatorPermit", subnetTestVector(permits, 1), netuidArg)
	fixture.set(t, "Emission", subnetTestVector(emissions, 8), netuidArg)
}

// Old requested miners that remain after a safe partial trim stay explicit.
// Exact state correspondence cannot establish a transaction or full reset.
func TestOwnerTrimGuardReconcilesPartialPostStateWithExplicitResidual(t *testing.T) {
	client, fixture, policy, policyHash, plan := newOwnerTrimGuardFixture(t, true)
	ownerTrimSetPostCensus(t, fixture.after, plan.Census.Observation.Seats[:5], 5)
	result, err := client.readOwnerTrimGuard(t.Context(), policy, policyHash, plan, "reconcile")
	if err != nil || !result.ObservationMatches || result.ApplyAuthority || result.FullResetCompleted || result.ResetReady ||
		len(result.ExpectedResidual) != 1 || result.Reconciliation == nil {
		t.Fatalf("partial reconciliation: %+v %v", result, err)
	}
	rows := result.Reconciliation.RequestedGenerations
	if len(rows) != 2 || rows[0].Hotkey != policy.Remove[0].Hotkey || rows[0].ObservedDisposition != "old-generation-retained" ||
		rows[1].Hotkey != policy.Remove[1].Hotkey || rows[1].ObservedDisposition != "old-generation-absent" ||
		len(result.Reconciliation.AbsentOldGenerations) != 1 || !result.Reconciliation.RootUnchanged ||
		!slices.Contains(result.ExecutionBlockers, "EXACT_TRIM_RECEIPT_AND_DISPATCH_PHASE_NOT_VERIFIED") {
		t.Fatalf("residual or attribution collapsed: %+v", result)
	}
}

// Removing UID 4 compresses the immune original UID 5 to UID 4 while keeping
// its exact old generation. A numeric-UID-only reconciliation would lose it.
func TestOwnerTrimGuardReconcilesCompressedResidualGeneration(t *testing.T) {
	client, fixture, policy, _, _ := newOwnerTrimGuardFixture(t, false)
	born := uint64(95)
	policy.Remove[1].RegistrationBlock = &born
	fixture.before.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, born), []byte{25, 0}, []byte{5, 0})
	policyRaw, _ := json.Marshal(policy)
	digest := sha256.Sum256(policyRaw)
	policyHash := "sha256:" + hex.EncodeToString(digest[:])
	baselineClient, _ := newRpcClient("http://root-rpc.example", client.retryWindow)
	baselineClient.httpClient.Transport = roundTripFunc(fixture.before.roundTrip)
	preview, err := baselineClient.readSubnetPreview(t.Context(), policy, policyHash)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil || plan.Best == nil || len(plan.Residual) != 1 {
		t.Fatalf("compressed residual plan: %+v %v", plan, err)
	}
	seats := append(slices.Clone(preview.Seats[:4]), preview.Seats[5])
	ownerTrimSetPostCensus(t, fixture.after, seats, 5)
	result, err := client.readOwnerTrimGuard(t.Context(), policy, policyHash, plan, "reconcile")
	if err != nil || !result.ObservationMatches || result.FullResetCompleted || result.Reconciliation == nil {
		t.Fatalf("compressed residual: %+v %v", result, err)
	}
	row := result.Reconciliation.RequestedGenerations[1]
	if row.Uid != 5 || row.CurrentRegistration == nil || row.CurrentRegistration.Uid != 4 ||
		row.RegistrationBlock != 95 || row.CurrentRegistration.RegistrationBlock != 95 || row.ObservedDisposition != "old-generation-retained" {
		t.Fatalf("residual identity lost during compression: %+v", row)
	}
}

// A smaller subnet may have removed the wrong identity. Custody and unpermitted
// validators remain protected, and UID compression must match every survivor.
func TestOwnerTrimGuardReconciliationRejectsWrongSurvivorsAndReentry(t *testing.T) {
	for _, change := range []string{"protected-validator", "custody", "owner-outside-immunity", "reentry", "coldkey-change", "uid-swap", "residual-missing", "root", "count-only"} {
		t.Run(change, func(t *testing.T) {
			client, fixture, policy, policyHash, plan := newOwnerTrimGuardFixture(t, true)
			seats := slices.Clone(plan.Census.Observation.Seats[:5])
			want := ""
			switch change {
			case "protected-validator", "custody":
				seats = append(slices.Clone(plan.Census.Observation.Seats[:3]), plan.Census.Observation.Seats[4:]...)
				want = "OWNER_TRIM_PROTECTED_GENERATION_MISSING"
				if change == "custody" {
					// Both old and current reads now apply the independently changed
					// role policy; the retained plan must be rebuilt for those bytes.
					policy.Preserve[1].Roles = []string{"escrow", "custody"}
					raw, _ := json.Marshal(policy)
					digest := sha256.Sum256(raw)
					policyHash = "sha256:" + hex.EncodeToString(digest[:])
					baselineClient, _ := newRpcClient("http://root-rpc.example", client.retryWindow)
					baselineClient.httpClient.Transport = roundTripFunc(fixture.before.roundTrip)
					preview, err := baselineClient.readSubnetPreview(t.Context(), policy, policyHash)
					if err != nil {
						t.Fatal(err)
					}
					plan, err = buildOwnerTrimPlan(t.Context(), policy, preview)
					if err != nil {
						t.Fatal(err)
					}
				}
			case "owner-outside-immunity":
				seats = append(slices.Clone(plan.Census.Observation.Seats[:1]), plan.Census.Observation.Seats[2:]...)
				want = "OWNER_TRIM_PROTECTED_GENERATION_MISSING"
			case "reentry":
				seats = append(seats, plan.Census.Observation.Seats[5])
				seats[5].RegistrationBlock = 101
				want = "OWNER_TRIM_UNEXPECTED_REGISTRATION_OR_REENTRY"
			case "coldkey-change":
				seats[4].Coldkey = "0x" + strings.Repeat("77", 32)
				want = "OWNER_TRIM_UNEXPECTED_REGISTRATION_OR_REENTRY"
			case "uid-swap":
				seats[2], seats[3] = seats[3], seats[2]
				want = "OWNER_TRIM_SURVIVOR_GENERATION_OR_UID_MAPPING_DIFFERS"
			case "residual-missing":
				seats = seats[:4]
				want = "OWNER_TRIM_SURVIVOR_GENERATION_OR_UID_MAPPING_DIFFERS"
			case "root":
				fixture.after.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 42), []byte{0, 0}, []byte{1, 0})
				want = "OWNER_TRIM_EXCLUDED_ROOT_GENERATIONS_CHANGED"
			case "count-only":
				seats = append(slices.Clone(plan.Census.Observation.Seats[:4]), plan.Census.Observation.Seats[5])
				want = "OWNER_TRIM_EXPECTED_REMOVAL_STILL_PRESENT"
			}
			ownerTrimSetPostCensus(t, fixture.after, seats, uint16(max(5, len(seats))))
			result, err := client.readOwnerTrimGuard(t.Context(), policy, policyHash, plan, "reconcile")
			if err != nil || result.ObservationMatches || !slices.Contains(result.ComparisonBlockers, want) || result.FullResetCompleted {
				t.Fatalf("%s acquired matching post-state: %+v %v", change, result, err)
			}
			if change == "reentry" {
				rows := result.Reconciliation.RequestedGenerations
				if rows[1].ObservedDisposition != "hotkey-has-different-generation" || rows[1].CurrentRegistration == nil ||
					rows[1].CurrentRegistration.RegistrationBlock != 101 || len(result.Reconciliation.UnexpectedRegistrations) != 1 {
					t.Fatalf("reentry collapsed into old residual: %+v", result.Reconciliation)
				}
			}
		})
	}
}

// A newly registered UID appearing while the reviewed set is pending forces a
// new reviewed plan even if emissions would currently keep that new entrant.
func TestOwnerTrimGuardRefusesConcurrentRegistration(t *testing.T) {
	client, fixture, policy, policyHash, plan := newOwnerTrimGuardFixture(t, false)
	seats := slices.Clone(plan.Census.Observation.Seats)
	seats = append(seats, subnetSeat{subnetRegistration: subnetRegistration{Uid: 6, Hotkey: "0x" + strings.Repeat("78", 32), Coldkey: "0x" + strings.Repeat("79", 32), RegistrationBlock: 101}})
	ownerTrimSetPostCensus(t, fixture.after, seats, 8)
	result, err := client.readOwnerTrimGuard(t.Context(), policy, policyHash, plan, "recheck")
	if err != nil || result.ObservationMatches || !slices.Contains(result.ComparisonBlockers, "OWNER_TRIM_MEMBERSHIP_OR_GENERATION_CHANGED") {
		t.Fatalf("concurrent registration escaped guard: %+v %v", result, err)
	}
}

// The CLI validates exact local bytes before any RPC and emits one complete
// result only after the final recheck. There is no apply or signing flag.
func TestOwnerTrimGuardCommandsRequireExactPolicyPlanAndNoSigning(t *testing.T) {
	_, fixture, policy, _, plan := newOwnerTrimGuardFixture(t, true)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		response, err := fixture.roundTrip(request)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		defer response.Body.Close()
		_, _ = io.Copy(writer, response.Body)
	}))
	defer server.Close()
	policyRaw, _ := json.Marshal(policy)
	planRaw, _ := json.Marshal(plan)
	dir := t.TempDir()
	policyPath, planPath := filepath.Join(dir, "policy.json"), filepath.Join(dir, "plan.json")
	if err := os.WriteFile(policyPath, policyRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, planRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"owner-trim-recheck", "--rpc", server.URL, "--policy", policyPath, "--plan", planPath, "--plan-hash", plan.ContentHash}
	var stdout, stderr bytes.Buffer
	if code := runMain(t.Context(), args, &stdout, &stderr); code != 0 || !bytes.Contains(stdout.Bytes(), []byte(`"apply_authority":false`)) {
		t.Fatalf("recheck command: %d %s %s", code, stdout.String(), stderr.String())
	}
	for _, extra := range [][]string{{"--apply"}, {"--signing-key", "never"}, {"--plan-hash", "sha256:" + strings.Repeat("ab", 32)}} {
		stdout.Reset()
		stderr.Reset()
		_, before := fixture.counts()
		if code := runMain(t.Context(), append(slices.Clone(args), extra...), &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Fatalf("bad input published evidence: %v %d %s", extra, code, stdout.String())
		}
		_, after := fixture.counts()
		if !reflect.DeepEqual(before, after) {
			t.Fatal("bad input reached network")
		}
	}
	// A role change in the supplied policy does not silently change the scope.
	policy.Preserve[0].Roles = []string{"custody"}
	policyRaw, _ = json.Marshal(policy)
	if err := os.WriteFile(policyPath, policyRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runMain(t.Context(), args, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
		t.Fatalf("changed role policy accepted: %d %s", code, stdout.String())
	}
}
