//go:build linux || darwin

// Real canonical Rpc reads cross deterministic transport interruptions. The
// fixtures control failures and deadlines, never identity or replay verdicts.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/stabi"
)

// The real Http transport receives every admitted request. Only a chosen
// physical refusal is injected before forwarding to the independent server.
type releaseStartupRpcTestTransport struct {
	inspect func(*http.Request, []chainBatchRPCRequest) error
}

func (self *releaseStartupRpcTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(request.Body)
	if err := errors.Join(err, request.Body.Close()); err != nil {
		return nil, err
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	var calls []chainBatchRPCRequest
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		err = json.Unmarshal(raw, &calls)
	} else {
		var call chainBatchRPCRequest
		err = json.Unmarshal(raw, &call)
		calls = []chainBatchRPCRequest{call}
	}
	if err != nil {
		return nil, err
	}
	if err := self.inspect(request, calls); err != nil {
		return nil, err
	}
	return http.DefaultTransport.RoundTrip(request)
}

// Swap only the transport of one test-owned reader; all actual release
// methods, signatures, canonical selectors and response decoding are kept.
func installReleaseStartupRpcTestTransport(t *testing.T, chain *ChainClient, endpoint string, inspect func(*http.Request, []chainBatchRPCRequest) error) {
	t.Helper()
	client, err := gethrpc.DialOptions(t.Context(), endpoint, gethrpc.WithHTTPClient(&http.Client{Transport: &releaseStartupRpcTestTransport{inspect: inspect}}))
	if err != nil {
		t.Fatal(err)
	}
	prior := chain.client
	chain.client = ethclient.NewClient(client)
	t.Cleanup(func() { client.Close(); chain.client = prior })
}

// Clock hooks alone grant no retry authority: the real startup entry point
// must opt its derived read context in before using them.
func releaseStartupRpcTestContext(ctx context.Context, wait releaseSnapshotRetryWait) context.Context {
	return context.WithValue(ctx, releaseStartupRpcReadKey{}, releaseStartupRpcReadHooks{wait: wait})
}

func TestReleaseStartupRpcUidPreservesCanonicalBatchPrefix(t *testing.T) {
	fixture := newChainBatchRPCFixture(t, 111, chainBatchRPCFaults{})
	server := httptest.NewServer(fixture)
	t.Cleanup(server.Close)
	chain, closeChain := chainBatchTestClient(t, fixture)
	t.Cleanup(closeChain)
	var ranges [][2]int
	countReads, waits := 0, 0
	installReleaseStartupRpcTestTransport(t, chain, server.URL, func(request *http.Request, calls []chainBatchRPCRequest) error {
		if calls[0].Method != "eth_call" {
			return nil
		}
		for _, call := range calls {
			var selector gethrpc.BlockNumberOrHash
			if err := json.Unmarshal(call.Params[1], &selector); err != nil || selector.BlockHash == nil || *selector.BlockHash != common.Hash(chainBatchTestBlockHash) || !selector.RequireCanonical {
				return errors.New("retry changed its independently pinned canonical snapshot")
			}
		}
		var input struct {
			Input hexutil.Bytes `json:"input"`
		}
		if err := json.Unmarshal(calls[0].Params[0], &input); err != nil {
			return err
		}
		if bytes.Equal(input.Input[:4], fixture.countSelector[:]) {
			countReads++
			return nil
		}
		first := int(input.Input[66])*256 + int(input.Input[67])
		ranges = append(ranges, [2]int{first, len(calls)})
		if first == 50 && len(calls) > 13 {
			return releaseStartupRpcTestDeadline(request)
		}
		return nil
	})
	ctx := releaseStartupRpcTestContext(t.Context(), func(ctx context.Context, delay time.Duration) error { waits++; return ctx.Err() })
	uid, err := loadReleaseStartupValidatorUid(ctx, chain, &ReleaseSnapshot{BlockNumber: 123, BlockHash: chainBatchTestBlockHash}, 521, chainBatchHotkey(110), nil, false)
	if err != nil || uid != 110 || countReads != 1 || waits != 2 {
		t.Fatalf("one interrupted suffix restarted Uid discovery: uid=%d counts=%d waits=%d ranges=%v err=%v", uid, countReads, waits, ranges, err)
	}
	want := [][2]int{{0, 50}, {50, 50}, {50, 25}, {50, 13}, {63, 13}, {76, 13}, {89, 13}, {102, 9}}
	if !reflect.DeepEqual(ranges, want) {
		t.Fatalf("completed prefix or reduced width was lost: got=%v want=%v", ranges, want)
	}
}

// A real request owns a normal finite call deadline, independent of the
// larger startup retry budget. Returning the typed failure avoids sleeps.
func releaseStartupRpcTestDeadline(request *http.Request) error {
	deadline, present := request.Context().Deadline()
	if !present || time.Until(deadline) > chainCallTimeout {
		return errors.New("startup request lost its bounded per-call deadline")
	}
	return context.DeadlineExceeded
}

func TestReleaseStartupRpcUidUnavailableDoesNotClaimMissing(t *testing.T) {
	fixture := newChainBatchRPCFixture(t, 2, chainBatchRPCFaults{})
	server := httptest.NewServer(fixture)
	t.Cleanup(server.Close)
	chain, closeChain := chainBatchTestClient(t, fixture)
	t.Cleanup(closeChain)
	calls := 0
	installReleaseStartupRpcTestTransport(t, chain, server.URL, func(request *http.Request, values []chainBatchRPCRequest) error {
		calls++
		return releaseStartupRpcTestDeadline(request)
	})
	ctx := releaseStartupRpcTestContext(t.Context(), func(context.Context, time.Duration) error { return context.DeadlineExceeded })
	_, err := loadReleaseStartupValidatorUid(ctx, chain, &ReleaseSnapshot{BlockNumber: 123, BlockHash: chainBatchTestBlockHash}, 521, chainBatchHotkey(1), nil, false)
	if calls != 1 || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "read release validator UID") || strings.Contains(err.Error(), "has no UID") {
		t.Fatalf("unavailable observation invented missing identity: calls=%d err=%v", calls, err)
	}
}

func TestReleaseStartupRpcUidProvenAbsenceRemainsFatal(t *testing.T) {
	fixture := newChainBatchRPCFixture(t, 2, chainBatchRPCFaults{})
	chain, closeChain := chainBatchTestClient(t, fixture)
	t.Cleanup(closeChain)
	ctx := releaseStartupRpcTestContext(t.Context(), func(context.Context, time.Duration) error { t.Fatal("proven absence retried"); return nil })
	_, err := loadReleaseStartupValidatorUid(ctx, chain, &ReleaseSnapshot{BlockNumber: 123, BlockHash: chainBatchTestBlockHash}, 521, chainBatchHotkey(9), nil, false)
	if err == nil || !strings.Contains(err.Error(), "has no UID") || strings.Contains(err.Error(), "%!w") || RetryableEvidenceTransportError(err) {
		t.Fatalf("proven absent identity lost its permanent verdict: %v", err)
	}
}

func TestReleaseStartupRpcCanonicalHeaderRetainsIndependentIdentity(t *testing.T) {
	fixture := newChainBatchRPCFixture(t, 2, chainBatchRPCFaults{})
	server := httptest.NewServer(fixture)
	t.Cleanup(server.Close)
	chain, closeChain := chainBatchTestClient(t, fixture)
	t.Cleanup(closeChain)
	headers, waits := 0, 0
	installReleaseStartupRpcTestTransport(t, chain, server.URL, func(request *http.Request, calls []chainBatchRPCRequest) error {
		if calls[0].Method == "eth_getBlockByHash" {
			headers++
			if headers == 1 {
				return releaseStartupRpcTestDeadline(request)
			}
		}
		return nil
	})
	ctx := withReleaseStartupRpcReads(releaseStartupRpcTestContext(t.Context(), func(ctx context.Context, delay time.Duration) error { waits++; return ctx.Err() }))
	if err := chain.validateBlockIdentityContext(ctx, 123, chainBatchTestBlockHash); err != nil || headers != 2 || waits != 1 {
		t.Fatalf("canonical header was not recovered: headers=%d waits=%d err=%v", headers, waits, err)
	}
	if err := chain.validateBlockIdentityContext(ctx, 124, chainBatchTestBlockHash); err == nil || headers != 2 || waits != 1 {
		t.Fatalf("retry accepted a changed height/hash owner: headers=%d waits=%d err=%v", headers, waits, err)
	}
}

func TestReleaseStartupRpcUidRejectsMalformedCompletedSnapshot(t *testing.T) {
	fixture := newChainBatchRPCFixture(t, 2, chainBatchRPCFaults{duplicateHotkeys: true})
	chain, closeChain := chainBatchTestClient(t, fixture)
	t.Cleanup(closeChain)
	ctx := releaseStartupRpcTestContext(t.Context(), func(context.Context, time.Duration) error { t.Fatal("completed malformed census retried"); return nil })
	_, err := loadReleaseStartupValidatorUid(ctx, chain, &ReleaseSnapshot{BlockNumber: 123, BlockHash: chainBatchTestBlockHash}, 521, chainBatchHotkey(1), nil, false)
	if err == nil || !strings.Contains(err.Error(), "duplicated at uid") || strings.Contains(err.Error(), "has no UID") || RetryableEvidenceTransportError(err) {
		t.Fatalf("completed invalid census was treated as transient or absent: %v", err)
	}
}

func TestReleaseStartupRpcBudgetPreservesCauseAndCancellation(t *testing.T) {
	var elapsed time.Duration
	hooks := releaseHttpGetTestDeadlineHooks(t, &elapsed, 75*time.Second)
	ctx := context.WithValue(t.Context(), releaseStartupRpcReadKey{}, releaseStartupRpcReadHooks{withTimeout: hooks.withTimeout, wait: hooks.wait})
	calls := 0
	err := retryReleaseStartupRpcRead(withReleaseStartupRpcReads(ctx), func(context.Context) error { calls++; return context.DeadlineExceeded })
	if calls != 4 || elapsed != 300*time.Second || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("startup read lost its one full deadline: calls=%d elapsed=%s err=%v", calls, elapsed, err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	waits := 0
	canceled = releaseStartupRpcTestContext(canceled, func(context.Context, time.Duration) error { waits++; cancel(); return nil })
	calls = 0
	err = retryReleaseStartupRpcRead(withReleaseStartupRpcReads(canceled), func(context.Context) error { calls++; return context.DeadlineExceeded })
	if calls != 1 || waits != 1 || !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("owner cancellation issued another read or lost original failure: calls=%d waits=%d err=%v", calls, waits, err)
	}
}

func TestReleaseStartupRpcMixedIntegrityAndLiveReadsDoNotRetry(t *testing.T) {
	for _, failure := range []error{
		errors.New("synthetic canonical hash differs"),
		errors.Join(context.DeadlineExceeded, errors.New("synthetic signature differs")),
		&gethrpc.HTTPError{StatusCode: http.StatusForbidden},
		errors.New("context deadline exceeded"),
	} {
		calls := 0
		ctx := releaseStartupRpcTestContext(t.Context(), func(context.Context, time.Duration) error { t.Fatal("nontransport failure retried"); return nil })
		err := retryReleaseStartupRpcRead(withReleaseStartupRpcReads(ctx), func(context.Context) error { calls++; return failure })
		if calls != 1 || !errors.Is(err, failure) {
			t.Fatalf("permanent sibling was discarded: calls=%d err=%v", calls, err)
		}
	}
	calls := 0
	ctx := releaseStartupRpcTestContext(t.Context(), func(context.Context, time.Duration) error { t.Fatal("unmarked live read retried"); return nil })
	err := retryReleaseStartupRpcRead(ctx, func(context.Context) error { calls++; return context.DeadlineExceeded })
	if calls != 1 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("clock hooks enabled live retries: calls=%d err=%v", calls, err)
	}
}

func TestReleaseStartupRpcSemanticReplayRetainsCompletedNativeCut(t *testing.T) {
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.trail(t, 0)
	fixture.ordinary(t, 0, 1, false)
	fixture.trail(t, 0)
	if err := fixture.disk.participants[0].Stats.Save(fixture.disk.participants[0].StateDir); err != nil {
		t.Fatal(err)
	}
	before := releaseStartupV2TestDiskImages(t, fixture.disk)
	fixture.reopen(t)
	var stateLock sync.Mutex
	var refused []chainBatchRPCRequest
	interrupted, reconciled, unrelated := false, false, false
	var waits atomic.Int64
	var retryReleased atomic.Bool
	retryEntered := make(chan struct{}, 1)
	allowRetry := make(chan struct{})
	var allowOnce sync.Once
	defer allowOnce.Do(func() { close(allowRetry) })
	installReleaseStartupRpcTestTransport(t, fixture.chain, fixture.chain.rpcUrl, func(request *http.Request, calls []chainBatchRPCRequest) error {
		stateLock.Lock()
		defer stateLock.Unlock()
		if calls[0].Method != "eth_call" || reconciled || !interrupted && len(calls) < 2 {
			return nil
		}
		if !interrupted {
			interrupted = true
			refused = calls
			return releaseStartupRpcTestDeadline(request)
		}
		// Different operator batches can share currentEpoch/policy prefixes.
		// Every member of this reduced batch must match the refused prefix.
		same := len(calls) <= len(refused)
		for index := range calls {
			if index >= len(refused) || calls[index].Method != refused[index].Method || !reflect.DeepEqual(calls[index].Params, refused[index].Params) {
				same = false
			}
		}
		if !same && reflect.DeepEqual(calls[0].Params, refused[0].Params) {
			unrelated = true
		}
		if same && retryReleased.Load() {
			reconciled = true
		}
		return nil
	})
	ctx := releaseStartupRpcTestContext(t.Context(), func(ctx context.Context, delay time.Duration) error {
		waits.Add(1)
		retryEntered <- struct{}{}
		select {
		case <-allowRetry:
			return ctx.Err()
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	startupCtx, cancelStartup := context.WithCancel(ctx)
	done := make(chan error, 1)
	joined := false
	go func() { done <- fixture.start(startupCtx, attemptSettlementV2PhysicalIO()) }()
	defer func() {
		cancelStartup()
		allowOnce.Do(func() { close(allowRetry) })
		if !joined {
			<-done
		}
	}()
	select {
	case <-retryEntered:
	case err := <-done:
		joined = true
		t.Fatalf("semantic startup did not admit interrupted-read recovery: %v", err)
	}
	// Force the same-prefix/different-suffix sibling while recovery is parked.
	// It is a real independently decoded Rpc read, with no Stats mutation.
	stateLock.Lock()
	first := refused[0]
	stateLock.Unlock()
	var selector gethrpc.BlockNumberOrHash
	var input struct {
		To    common.Address `json:"to"`
		Input hexutil.Bytes  `json:"input"`
	}
	if err := errors.Join(json.Unmarshal(first.Params[0], &input), json.Unmarshal(first.Params[1], &selector)); err != nil || selector.BlockHash == nil {
		t.Fatalf("captured startup request is invalid: %v", err)
	}
	block := uint64(0)
	for number, hash := range fixture.blocks {
		if common.Hash(hash) == *selector.BlockHash {
			block = number
		}
	}
	// A longer complete batch cannot be the reduced retry prefix.
	stateLock.Lock()
	sibling := make([]chainBatchCall, len(refused)+1)
	stateLock.Unlock()
	for index := range sibling {
		sibling[index] = chainBatchCall{address: input.To, calldata: stabi.NewSTCoordinator().PackCurrentEpoch()}
	}
	sibling[0].calldata = bytes.Clone(input.Input)
	if _, err := fixture.chain.batchCallsAtHashContext(t.Context(), block, [32]byte(*selector.BlockHash), sibling); err != nil {
		t.Fatalf("unrelated concurrent canonical read was mistaken for retry: %v", err)
	}
	stateLock.Lock()
	observedSibling := unrelated
	stateLock.Unlock()
	if !observedSibling {
		t.Fatal("fixture did not force the different-suffix sibling")
	}
	retryReleased.Store(true)
	allowOnce.Do(func() { close(allowRetry) })
	err := <-done
	joined = true
	if err != nil {
		t.Fatalf("semantic startup abandoned retained state after a transient Rpc read: %v", err)
	}
	if !interrupted || !reconciled || waits.Load() != 1 {
		t.Fatalf("actual semantic replay missed interrupted read: interrupted=%v reconciled=%v waits=%d", interrupted, reconciled, waits.Load())
	}
	if after := releaseStartupV2TestDiskImages(t, fixture.disk); !reflect.DeepEqual(before, after) {
		t.Fatal("retry changed the completed native cut or exact real suffix")
	}
	stats := fixture.disk.participants[0].Stats
	if stats.egressGeneration != 2 || stats.attemptLastAppliedSequence != 16 || len(stats.egress) == 0 {
		t.Fatal("retry duplicated native rotation or discarded admitted suffix")
	}
	if hooks, _ := ctx.Value(releaseStartupRpcReadKey{}).(releaseStartupRpcReadHooks); hooks.enabled {
		t.Fatal("semantic startup leaked read permission into caller context")
	}
}
