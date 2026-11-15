//go:build linux || darwin

// Real public operator admission retains original proof and financial reads
// while controlled geth HTTP header replies exercise closing finality recovery.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// Only serialized headers and original transport errors are changed. The
// signed approval, proof replay, coordinator views and approved route stay real.
type releaseDecisionFinalityTestTransport struct {
	original  *http.Transport
	fixture   *recycleOperatorFixture
	mode      string
	opening   uint64
	decision  uint64
	cause     error
	stateLock sync.Mutex
	finalized int
	closing   int
	waits     int
	owners    int
	canonical map[uint64]int
	methods   map[string]int
}

// Every request body is bounded and closed before forwarding an owned copy.
func (self *releaseDecisionFinalityTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(io.LimitReader(request.Body, 1024*1024+1))
	err = errors.Join(err, request.Body.Close())
	if err != nil || len(raw) > 1024*1024 {
		return nil, errors.Join(errors.New("synthetic finality request exceeded its bound"), err)
	}
	var calls []chainBatchRPCRequest
	if len(raw) > 0 && raw[0] == '[' {
		err = json.Unmarshal(raw, &calls)
	} else {
		var call chainBatchRPCRequest
		err = json.Unmarshal(raw, &call)
		calls = []chainBatchRPCRequest{call}
	}
	if err != nil {
		return nil, err
	}
	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		for _, call := range calls {
			self.methods[call.Method]++
		}
	}()
	if len(calls) != 1 || calls[0].Method != "eth_getBlockByNumber" {
		forwarded := request.Clone(request.Context())
		forwarded.Body = io.NopCloser(bytes.NewReader(raw))
		return self.original.RoundTrip(forwarded)
	}
	call := calls[0]
	var selector string
	if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &selector) != nil {
		return nil, errors.New("synthetic finality header selector changed")
	}
	closing := self.fixture.count("currentEpoch") >= 2
	value, err := self.header(selector, closing)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": value})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), ContentLength: int64(len(encoded)), Body: io.NopCloser(bytes.NewReader(encoded))}, nil
}

// State transitions follow completed ABI observations and explicit request
// counts. No sleep, selected hash cache or injected verifier result is involved.
func (self *releaseDecisionFinalityTestTransport) header(selector string, closing bool) (any, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	var number uint64
	changed := false
	if selector == "finalized" {
		self.finalized++
		number = self.opening
		if !closing && self.mode == "opening-lag" && self.finalized == 1 {
			number = self.decision - 1
		}
		if closing {
			self.closing++
			switch self.mode {
			case "lag":
				if self.closing == 1 {
					number--
				}
			case "persistent-lag":
				number--
			case "null":
				if self.closing == 1 {
					return nil, nil
				}
			case "genesis":
				if self.closing == 1 {
					number = 0
				}
			case "same-height":
				if self.closing > 1 {
					return nil, nil
				}
				changed = true
			case "original-fork", "decision-fork":
				if self.closing > 1 {
					return nil, nil
				}
				number--
			case "advanced-original-fork":
				if self.closing > 1 {
					return nil, nil
				}
				number++
			case "canonical-null":
				if self.closing == 1 {
					number--
				}
			case "advance":
				number++
			case "transport":
				if self.closing == 1 {
					return nil, self.cause
				}
			case "empty-object":
				return map[string]any{}, nil
			case "zero-hash":
				return map[string]any{"number": "0x0", "hash": common.Hash{}}, nil
			case "missing-number":
				return map[string]any{"hash": common.Hash(self.fixture.blocks[number])}, nil
			}
		}
	} else {
		var err error
		number, err = hexutil.DecodeUint64(selector)
		if err != nil {
			return nil, err
		}
		if closing {
			self.canonical[number]++
			if self.mode == "same-height" {
				return nil, nil
			}
			if self.mode == "original-fork" || self.mode == "advanced-original-fork" {
				if number == self.decision || number == self.opening && self.canonical[number] > 1 {
					return nil, nil
				}
				changed = number == self.opening
			}
			if self.mode == "decision-fork" && number == self.decision {
				if self.canonical[number] > 1 {
					return nil, nil
				}
				changed = true
			}
			if self.mode == "canonical-null" && number == self.opening && self.canonical[number] == 1 {
				return nil, nil
			}
		}
	}
	hash, found := self.fixture.blocks[number]
	if !found {
		return nil, errors.New("synthetic finality requested an unowned block")
	}
	if changed {
		// Fixture identities occupy the first byte. A fork changes an unused
		// byte so it stays nonzero and cannot alias another retained height.
		hash[len(hash)-1] ^= 1
	}
	return map[string]any{"number": hexutil.EncodeUint64(number), "hash": common.Hash(hash)}, nil
}

// The original signed route is retained while its private HTTP client gets
// deterministic physical response faults and the existing logical owner clock.
func newOwnerRecycleFinalityFixture(t *testing.T, mode string) (*recycleOperatorFixture, *releaseDecisionFinalityTestTransport) {
	t.Helper()
	fixture := newRecycleOperatorFixture(t)
	block := fixture.query.boundary.EVMBlock
	fixture.blocks[block+1], fixture.blocks[block+2], fixture.blocks[block+3], fixture.blocks[0] = [32]byte{0xd1}, [32]byte{0xd2}, [32]byte{0xd3}, [32]byte{0xd4}
	if _, found := fixture.blocks[block-1]; !found {
		fixture.blocks[block-1] = [32]byte{0xd5}
	}
	transport := &releaseDecisionFinalityTestTransport{original: http.DefaultTransport.(*http.Transport).Clone(), fixture: fixture, mode: mode, opening: block + 2, decision: block, canonical: map[uint64]int{}, methods: map[string]int{}}
	t.Cleanup(transport.original.CloseIdleConnections)
	client, err := gethrpc.DialOptions(t.Context(), fixture.chain.rpcUrl, gethrpc.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	fixture.chain.client = ethclient.NewClient(client)
	hooks := chainReadRetryTestHooks(4)
	own, wait := hooks.withTimeout, hooks.wait
	hooks.withTimeout = func(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		if timeout != 300*time.Second {
			t.Fatal("decision observation changed the original operation budget")
		}
		transport.stateLock.Lock()
		transport.owners++
		transport.stateLock.Unlock()
		return own(ctx, timeout)
	}
	hooks.wait = func(ctx context.Context, delay time.Duration) error {
		transport.stateLock.Lock()
		transport.waits++
		transport.stateLock.Unlock()
		return wait(ctx, delay)
	}
	fixture.chain.readRetryHooks = hooks
	return fixture, transport
}

// Actual proof/financial reads run once, and the immutable input owner and
// transaction-write authority remain unchanged even when coverage is retried.
func assertOwnerRecycleFinalityReadOnly(t *testing.T, fixture *recycleOperatorFixture, transport *releaseDecisionFinalityTestTransport) {
	t.Helper()
	if len(fixture.measurement.authority.operatorEvidence) != 0 || fixture.count("rootCommitments") != 2 || fixture.count("epochDeposits") != 2 || fixture.count("cumulativeConviction") != 2 || fixture.count("currentEpoch") != 2 {
		t.Fatal("finality retry changed the input owner or repeated financial observation")
	}
	transport.stateLock.Lock()
	defer transport.stateLock.Unlock()
	if transport.owners != 1 {
		t.Fatal("closing finality received a fresh operation owner", transport.owners)
	}
	for method := range transport.methods {
		switch method {
		case "eth_chainId", "chain_getBlockHash", "eth_getBlockByNumber", "eth_getBlockByHash", "eth_call":
		default:
			t.Fatal("decision finality issued a non-read RPC", method)
		}
	}
}

// Lower closing coverage remains above the decision block, but still must
// recover the stronger original opening witness before publishing authority.
func TestOwnerRecycleOperatorsFinalityLagRecoversOriginalWitness(t *testing.T) {
	fixture, transport := newOwnerRecycleFinalityFixture(t, "lag")
	owner, err := fixture.observe(t)
	if err != nil || owner == nil || owner == fixture.measurement.authority || transport.waits != 1 || transport.finalized != 3 || transport.canonical[transport.opening] != 2 || transport.canonical[transport.decision] != 3 {
		t.Fatal("public operator admission did not recover its original finality witness", err, transport.waits, transport.finalized)
	}
	var evidence OwnerRecycleOperatorEvidence
	if err := json.Unmarshal(owner.operatorEvidence, &evidence); err != nil || evidence.Decision != fixture.measurement.authority.expected {
		t.Fatal("finality recovery retargeted the independently pinned decision", err)
	}
	assertOwnerRecycleFinalityReadOnly(t, fixture, transport)
}

// Initial coverage lag cannot turn the independently pinned decision into a
// different block or begin its financial census before finality is available.
func TestOwnerRecycleOperatorsOpeningFinalityLagRecoversPinnedDecision(t *testing.T) {
	fixture, transport := newOwnerRecycleFinalityFixture(t, "opening-lag")
	wait := fixture.chain.readRetryHooks.wait
	fixture.chain.readRetryHooks.wait = func(ctx context.Context, delay time.Duration) error {
		if fixture.count("netuid") != 0 {
			t.Fatal("financial views began before initial finalized coverage")
		}
		return wait(ctx, delay)
	}
	if owner, err := fixture.observe(t); err != nil || owner == nil || transport.waits != 1 || transport.finalized != 3 {
		t.Fatal("initial finality lag did not recover the pinned decision", err)
	}
	assertOwnerRecycleFinalityReadOnly(t, fixture, transport)
}

// Persistent lag reaches the existing logical owner deadline and retains the
// actual original witness alongside that deadline, with no partial result.
func TestOwnerRecycleOperatorsFinalityLagExpiresOriginalBudget(t *testing.T) {
	fixture, transport := newOwnerRecycleFinalityFixture(t, "persistent-lag")
	owner, err := fixture.observe(t)
	var unavailable *releaseDecisionFinalityUnavailableError
	if owner != nil || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &unavailable) || unavailable.requiredBlock != transport.opening || unavailable.requiredHash != fixture.blocks[transport.opening] || unavailable.observedBlock != transport.opening-1 || transport.waits != 4 || transport.finalized != 5 || !RetryableEvidenceTransportError(err) {
		t.Fatal("persistent finality lag lost its bounded original availability cause", err)
	}
	assertOwnerRecycleFinalityReadOnly(t, fixture, transport)
}

// Literal null is distinguishable from every present malformed header.
func TestOwnerRecycleOperatorsMissingClosingFinalityRecovers(t *testing.T) {
	fixture, transport := newOwnerRecycleFinalityFixture(t, "null")
	if owner, err := fixture.observe(t); err != nil || owner == nil || transport.waits != 1 || transport.finalized != 3 {
		t.Fatal("literal missing closing finality became a permanent conflict", err)
	}
	assertOwnerRecycleFinalityReadOnly(t, fixture, transport)
}

// A genuine finalized genesis header is lower coverage, with its own actual
// numbered canonical confirmation; zero height does not mean an absent header.
func TestOwnerRecycleOperatorsGenesisFinalityLagRecovers(t *testing.T) {
	fixture, transport := newOwnerRecycleFinalityFixture(t, "genesis")
	if owner, err := fixture.observe(t); err != nil || owner == nil || transport.waits != 1 || transport.canonical[0] != 1 {
		t.Fatal("valid finalized genesis was mistaken for malformed evidence", err)
	}
	assertOwnerRecycleFinalityReadOnly(t, fixture, transport)
}

// A completed same-height contradiction is terminal before a subsequent
// unavailable numbered reply can replace its meaning or authorize a retry.
func TestOwnerRecycleOperatorsFinalityConflictPrecedesLaterAbsence(t *testing.T) {
	fixture, transport := newOwnerRecycleFinalityFixture(t, "same-height")
	owner, err := fixture.observe(t)
	if owner != nil || err == nil || !strings.Contains(err.Error(), "conflicts with its original witness") || RetryableEvidenceTransportError(err) || transport.waits != 0 || transport.finalized != 2 || len(transport.canonical) != 0 {
		t.Fatal("same-height finality contradiction was replaced by later availability", err)
	}
	assertOwnerRecycleFinalityReadOnly(t, fixture, transport)
}

// Neither a lower nor a newer head may erase an original finalized witness
// whose canonical hash changed; a later missing decision reply is irrelevant.
func TestOwnerRecycleOperatorsOriginalFinalityForkPrecedesLaterAbsence(t *testing.T) {
	for _, mode := range []string{"original-fork", "advanced-original-fork"} {
		fixture, transport := newOwnerRecycleFinalityFixture(t, mode)
		closing := transport.opening - 1
		if mode == "advanced-original-fork" {
			closing = transport.opening + 1
		}
		owner, err := fixture.observe(t)
		if owner != nil || err == nil || !strings.Contains(err.Error(), "canonical finality witness changed") || RetryableEvidenceTransportError(err) || transport.waits != 0 || transport.finalized != 2 || transport.canonical[closing] != 1 || transport.canonical[transport.opening] != 1 || transport.canonical[transport.decision] != 0 {
			t.Fatal("original canonical finality conflict was hidden by a later missing pin", mode, err)
		}
		assertOwnerRecycleFinalityReadOnly(t, fixture, transport)
	}
}

// The older decision pin stays independently canonical even when the opening
// finalized head and a lower closing head both retain their original hashes.
func TestOwnerRecycleOperatorsDecisionForkPrecedesLaterAbsence(t *testing.T) {
	fixture, transport := newOwnerRecycleFinalityFixture(t, "decision-fork")
	owner, err := fixture.observe(t)
	if owner != nil || err == nil || !strings.Contains(err.Error(), "canonical finality witness changed") || RetryableEvidenceTransportError(err) || transport.waits != 0 || transport.finalized != 2 || transport.canonical[transport.opening-1] != 1 || transport.canonical[transport.opening] != 1 || transport.canonical[transport.decision] != 1 {
		t.Fatal("decision canonical contradiction was promoted to transient lag", err)
	}
	assertOwnerRecycleFinalityReadOnly(t, fixture, transport)
}

// Empty fields, zero hash and an absent number are completed malformed data,
// not the explicit null response eligible for physical availability retry.
func TestOwnerRecycleOperatorsMalformedClosingFinalityIsHard(t *testing.T) {
	for _, mode := range []string{"empty-object", "zero-hash", "missing-number"} {
		fixture, transport := newOwnerRecycleFinalityFixture(t, mode)
		owner, err := fixture.observe(t)
		if owner != nil || err == nil || !strings.Contains(err.Error(), "present but invalid") || RetryableEvidenceTransportError(err) || transport.waits != 0 || transport.finalized != 2 {
			t.Fatal("present malformed finality was retried as absent evidence", mode, err)
		}
		assertOwnerRecycleFinalityReadOnly(t, fixture, transport)
	}
}

// An original hard transport sibling survives its joined deadline and every
// nested read owner. The raw error identity is retained for external diagnosis.
func TestOwnerRecycleOperatorsFinalityMixedCauseStaysHard(t *testing.T) {
	fixture, transport := newOwnerRecycleFinalityFixture(t, "transport")
	hard := errors.New("synthetic original finality transport integrity failure")
	transport.cause = errors.Join(hard, context.DeadlineExceeded)
	owner, err := fixture.observe(t)
	if owner != nil || !errors.Is(err, hard) || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, transport.cause) || RetryableEvidenceTransportError(err) || transport.waits != 0 || transport.finalized != 2 {
		t.Fatal("closing finality retry lost an original hard transport sibling", err)
	}
	assertOwnerRecycleFinalityReadOnly(t, fixture, transport)
}

// Each real RPC failure exposes its cause graph once. Subsequent local and
// outer classifiers cannot exchange the originally observed hard/soft child.
func TestOwnerRecycleOperatorsFinalityRetainsFirstTransportCause(t *testing.T) {
	for _, temporary := range []bool{false, true} {
		fixture, transport := newOwnerRecycleFinalityFixture(t, "transport")
		hard := &os.PathError{Op: "read", Path: "synthetic-finality-original", Err: context.DeadlineExceeded}
		probe := &releasePreparationChangingCause{first: hard, later: context.DeadlineExceeded}
		if temporary {
			probe.first, probe.later = context.DeadlineExceeded, hard
		}
		transport.cause = probe
		owner, err := fixture.observe(t)
		if probe.observations != 1 {
			t.Fatal("finality classified the same mutable RPC cause twice", temporary, probe.observations)
		}
		if temporary {
			if owner == nil || err != nil || transport.waits != 1 || transport.finalized != 3 {
				t.Fatal("original temporary finality cause was replaced by its later hard edge", err)
			}
		} else if owner != nil || !errors.Is(err, hard) || !errors.Is(err, probe) || RetryableEvidenceTransportError(err) || transport.waits != 0 || transport.finalized != 2 {
			t.Fatal("original hard finality cause was replaced by its later timeout", err)
		}
		assertOwnerRecycleFinalityReadOnly(t, fixture, transport)
	}
}

// Higher coverage preserves the original decision and validates every new
// canonical witness without imposing a spurious equality-only finality rule.
func TestOwnerRecycleOperatorsAdvancedFinalityKeepsOriginalDecision(t *testing.T) {
	fixture, transport := newOwnerRecycleFinalityFixture(t, "advance")
	if owner, err := fixture.observe(t); err != nil || owner == nil || transport.waits != 0 || transport.finalized != 2 || transport.canonical[transport.opening+1] != 1 || transport.canonical[transport.opening] != 1 {
		t.Fatal("canonical finalized advancement changed the original decision", err)
	}
	assertOwnerRecycleFinalityReadOnly(t, fixture, transport)
}

// Missing numbered coverage is retried without inferring a changed hash.
func TestOwnerRecycleOperatorsMissingCanonicalFinalityRecovers(t *testing.T) {
	fixture, transport := newOwnerRecycleFinalityFixture(t, "canonical-null")
	if owner, err := fixture.observe(t); err != nil || owner == nil || transport.waits != 1 || transport.finalized != 3 {
		t.Fatal("missing canonical original finality could not recover", err)
	}
	assertOwnerRecycleFinalityReadOnly(t, fixture, transport)
}

// Caller cancellation ends the same lag wait, retaining both the availability
// leaf and the actual cancellation instead of returning a partial owner.
func TestOwnerRecycleOperatorsFinalityLagCancellationRetainsCause(t *testing.T) {
	fixture, transport := newOwnerRecycleFinalityFixture(t, "persistent-lag")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.chain.readRetryHooks.wait = func(ctx context.Context, _ time.Duration) error {
		func() { transport.stateLock.Lock(); defer transport.stateLock.Unlock(); transport.waits++ }()
		cancel()
		return ctx.Err()
	}
	owner, err := ObserveOwnerRecycleMeasurementOperators(ctx, fixture.measurement.authority, fixture.chain, fixture.measurement.encoded, fixture.measurement.provider.options(t))
	var unavailable *releaseDecisionFinalityUnavailableError
	if owner != nil || !errors.Is(err, context.Canceled) || !errors.As(err, &unavailable) || transport.waits != 1 || transport.finalized != 2 || RetryableEvidenceTransportError(err) {
		t.Fatal("canceled finality lag lost its original cause or retained authority", err)
	}
	assertOwnerRecycleFinalityReadOnly(t, fixture, transport)
}

// Direct historical callers get the same one operation owner as the public
// recycle path; financial calls cannot grant closing recovery a fresh budget.
func TestReleaseEvidenceV2DecisionFinalityUsesOneOperationBudget(t *testing.T) {
	fixture := newReleaseDecisionV2TestFixture(t)
	owners := 0
	fixture.chain.readRetryHooks.withTimeout = func(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		owners++
		if timeout != 300*time.Second {
			t.Fatal("decision operation budget changed")
		}
		return context.WithTimeout(ctx, timeout)
	}
	if observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query); err != nil || observed == nil || owners != 1 {
		t.Fatal("decision opened a separate budget for each physical read", owners, err)
	}
}

// Four healthy 31-second replies each fit their physical 60-second slices and
// the original 300-second total. A grouped 60-second attempt would starve them.
func TestReleaseDecisionFinalityGivesEachHeaderItsOwnAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		blocks := map[uint64][32]byte{90: {0x90}, 100: {0xa0}, 101: {0xa1}}
		calls := 0
		chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
			raw, err := io.ReadAll(io.LimitReader(request.Body, 1024*1024+1))
			err = errors.Join(err, request.Body.Close())
			var call chainBatchRPCRequest
			if err != nil || len(raw) > 1024*1024 || json.Unmarshal(raw, &call) != nil || call.Method != "eth_getBlockByNumber" || len(call.Params) != 2 {
				return nil, errors.New("synthetic finality timing fixture received another RPC")
			}
			deadline, bounded := request.Context().Deadline()
			if !bounded || deadline.Sub(time.Now()) != 60*time.Second || deadline.After(started.Add(300*time.Second)) {
				return nil, errors.New("physical finality header did not receive its own bounded attempt")
			}
			var selector string
			if err := json.Unmarshal(call.Params[0], &selector); err != nil {
				return nil, err
			}
			number := uint64(101)
			if selector != "finalized" {
				var err error
				number, err = hexutil.DecodeUint64(selector)
				if err != nil {
					return nil, err
				}
			}
			calls++
			select {
			case <-request.Context().Done():
				return nil, request.Context().Err()
			case <-time.After(31 * time.Second):
			}
			return chainReadRetryResponse(t, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": map[string]any{"number": hexutil.EncodeUint64(number), "hash": common.Hash(blocks[number])}}), nil
		})
		number, hash, err := chain.readReleaseDecisionFinalityV2Context(t.Context(), 100, blocks[100], 90, blocks[90])
		if err != nil || number != 101 || hash != blocks[101] || calls != 4 || time.Since(started) != 124*time.Second {
			t.Fatal("healthy physical reads exhausted a grouped finality attempt", number, calls, time.Since(started), err)
		}
	})
}
