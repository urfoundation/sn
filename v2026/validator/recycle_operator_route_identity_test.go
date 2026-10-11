//go:build linux || darwin

// Real public operator admission compares each decoded route identity before
// another physical read, while retaining its original proof and read owner.
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
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// A physical attempt records its real context, independently of a retry verdict.
type ownerRecycleRouteTestAttempt struct {
	id       int
	deadline time.Time
	owner    time.Time
}

// Only instance-owned attempt contexts carry these fixture observations.
type ownerRecycleRouteTestAttemptKey struct{}

// Opening and closing requests are distinguished by the completed financial
// census, not by a replacement authority or a changed approved route.
type ownerRecycleRouteTestRequest struct {
	method  string
	closing bool
	attempt ownerRecycleRouteTestAttempt
}

// The script changes actual serialized identity results or original transport
// causes. All other requests use the signed fixture's real HTTP endpoint.
type ownerRecycleRouteTestTransport struct {
	original     *http.Transport
	fixture      *recycleOperatorFixture
	script       func(context.Context, string, bool, int) (any, error)
	stateLock    sync.Mutex
	requests     []ownerRecycleRouteTestRequest
	methodCounts map[string]int
	owners       int
	attempts     int
	waits        int
	deadline     time.Time
}

// Read and close the bounded request before forwarding its owned body copy.
func (self *ownerRecycleRouteTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(io.LimitReader(request.Body, 1024*1024+1))
	err = errors.Join(err, request.Body.Close())
	if err != nil || len(raw) > 1024*1024 {
		return nil, errors.Join(errors.New("synthetic route request exceeded its bound"), err)
	}
	var calls []chainBatchRPCRequest
	if len(raw) != 0 && raw[0] == '[' {
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
			self.methodCounts[call.Method]++
		}
	}()
	if len(calls) != 1 || calls[0].Method != "eth_chainId" && calls[0].Method != "chain_getBlockHash" {
		forwarded := request.Clone(request.Context())
		forwarded.Body = io.NopCloser(bytes.NewReader(raw))
		return self.original.RoundTrip(forwarded)
	}
	call := calls[0]
	if call.Method == "chain_getBlockHash" {
		var number uint64
		if len(call.Params) != 1 || json.Unmarshal(call.Params[0], &number) != nil || number != 0 {
			return nil, errors.New("synthetic route changed its native genesis selector")
		}
	} else if len(call.Params) != 0 {
		return nil, errors.New("synthetic route changed its chain id arguments")
	}
	attempt, ok := request.Context().Value(ownerRecycleRouteTestAttemptKey{}).(ownerRecycleRouteTestAttempt)
	deadline, bounded := request.Context().Deadline()
	if !ok || !bounded || deadline != attempt.deadline || deadline.After(attempt.owner) {
		return nil, errors.New("synthetic route request escaped its physical or original deadline")
	}
	closing := self.fixture.count("currentEpoch") >= 2
	count := func() int {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.requests = append(self.requests, ownerRecycleRouteTestRequest{method: call.Method, closing: closing, attempt: attempt})
		count := 0
		for _, observed := range self.requests {
			if observed.method == call.Method && observed.closing == closing {
				count++
			}
		}
		return count
	}()
	value, err := self.script(request.Context(), call.Method, closing, count)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": value})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), ContentLength: int64(len(encoded)), Body: io.NopCloser(bytes.NewReader(encoded))}, nil
}

// Expected values come from the independent approved deployment domain.
func (self *ownerRecycleRouteTestTransport) expected(method string) any {
	if method == "eth_chainId" {
		return hexutil.EncodeUint64(self.fixture.query.domain.ChainID)
	}
	return common.Hash(self.fixture.query.domain.GenesisHash)
}

// Inspect request counts only after the synchronous public reader has returned.
func (self *ownerRecycleRouteTestTransport) count(method string, closing bool) int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	count := 0
	for _, request := range self.requests {
		if request.method == method && request.closing == closing {
			count++
		}
	}
	return count
}

// The original route, signed fixture and actual proof replay are retained.
// Logical failure transitions preserve full production timeout arguments.
func newOwnerRecycleRouteIdentityFixture(t *testing.T) (*recycleOperatorFixture, *ownerRecycleRouteTestTransport) {
	t.Helper()
	fixture := newRecycleOperatorFixture(t)
	transport := &ownerRecycleRouteTestTransport{original: http.DefaultTransport.(*http.Transport).Clone(), fixture: fixture, methodCounts: map[string]int{}}
	transport.script = func(_ context.Context, method string, _ bool, _ int) (any, error) {
		return transport.expected(method), nil
	}
	t.Cleanup(transport.original.CloseIdleConnections)
	client, err := gethrpc.DialOptions(t.Context(), fixture.chain.rpcUrl, gethrpc.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	fixture.chain.client = ethclient.NewClient(client)
	hooks := chainReadRetryTestHooks(4)
	withTimeout, wait := hooks.withTimeout, hooks.wait
	hooks.withTimeout = func(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		if timeout != 300*time.Second {
			t.Fatal("route changed the original operation budget")
		}
		ctx, cancel := withTimeout(parent, timeout)
		deadline, bounded := ctx.Deadline()
		if !bounded {
			t.Fatal("route lost its finite operation deadline")
		}
		func() {
			transport.stateLock.Lock()
			defer transport.stateLock.Unlock()
			transport.owners++
			transport.deadline = deadline
		}()
		return ctx, cancel
	}
	hooks.withAttemptTimeout = func(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		if timeout != 60*time.Second {
			t.Fatal("route changed a physical attempt budget")
		}
		ctx, cancel := context.WithTimeout(parent, timeout)
		deadline, bounded := ctx.Deadline()
		owner, owned := parent.Deadline()
		if !bounded || !owned {
			t.Fatal("route physical attempt has no original owner")
		}
		id := func() int {
			transport.stateLock.Lock()
			defer transport.stateLock.Unlock()
			transport.attempts++
			return transport.attempts
		}()
		return context.WithValue(ctx, ownerRecycleRouteTestAttemptKey{}, ownerRecycleRouteTestAttempt{id: id, deadline: deadline, owner: owner}), cancel
	}
	hooks.wait = func(ctx context.Context, delay time.Duration) error {
		func() { transport.stateLock.Lock(); defer transport.stateLock.Unlock(); transport.waits++ }()
		return wait(ctx, delay)
	}
	fixture.chain.readRetryHooks = hooks
	return fixture, transport
}

// Failure cannot retain partial authority or repeat completed financial work.
// The original input owner and write permissions remain untouched on success.
func assertOwnerRecycleRouteIdentityScope(t *testing.T, fixture *recycleOperatorFixture, transport *ownerRecycleRouteTestTransport, completed bool) {
	t.Helper()
	want := 0
	if completed {
		want = 2
	}
	if len(fixture.measurement.authority.operatorEvidence) != 0 || fixture.count("rootCommitments") != want || fixture.count("epochDeposits") != want || fixture.count("cumulativeConviction") != want || fixture.count("currentEpoch") != want {
		t.Fatal("route identity changed its immutable owner or repeated financial work")
	}
	transport.stateLock.Lock()
	defer transport.stateLock.Unlock()
	if transport.owners != 1 {
		t.Fatal("route identity minted another operation budget", transport.owners)
	}
	for method := range transport.methodCounts {
		switch method {
		case "eth_chainId", "chain_getBlockHash", "eth_getBlockByNumber", "eth_getBlockByHash", "eth_call":
		default:
			t.Fatal("route identity issued a non-read request", method)
		}
	}
}

// A completed wrong first value must stop before the armed later timeout,
// both before proof replay and after the complete actual financial census.
func TestOwnerRecycleRouteIdentityRejectsChainBeforeGenesis(t *testing.T) {
	for _, closing := range []bool{false, true} {
		fixture, transport := newOwnerRecycleRouteIdentityFixture(t)
		transport.script = func(_ context.Context, method string, actualClosing bool, _ int) (any, error) {
			if actualClosing == closing {
				if method == "eth_chainId" {
					return hexutil.EncodeUint64(fixture.query.domain.ChainID + 1), nil
				}
				return nil, context.DeadlineExceeded
			}
			return transport.expected(method), nil
		}
		owner, err := fixture.observe(t)
		if owner != nil || err == nil || !strings.Contains(err.Error(), "changed mainnet EVM chain id") || RetryableEvidenceTransportError(err) || transport.waits != 0 || transport.count("eth_chainId", closing) != 1 || transport.count("chain_getBlockHash", closing) != 0 {
			t.Fatal("completed wrong chain id was replaced by a later genesis timeout", closing, err)
		}
		assertOwnerRecycleRouteIdentityScope(t, fixture, transport, closing)
	}
}

// Literal null has no identity to compare and can recover in the same owner.
// A completed matching first field is retained while only genesis retries.
func TestOwnerRecycleRouteIdentityMissingReplyRecovers(t *testing.T) {
	for _, closing := range []bool{false, true} {
		for _, method := range []string{"eth_chainId", "chain_getBlockHash"} {
			fixture, transport := newOwnerRecycleRouteIdentityFixture(t)
			transport.script = func(_ context.Context, actual string, actualClosing bool, count int) (any, error) {
				if actual == method && actualClosing == closing && count == 1 {
					return nil, nil
				}
				return transport.expected(actual), nil
			}
			owner, err := fixture.observe(t)
			if err != nil || owner == nil || owner == fixture.measurement.authority || transport.waits != 1 || transport.count(method, closing) != 2 {
				t.Fatal("missing identity did not recover under its original owner", closing, method, err)
			}
			if method == "chain_getBlockHash" && transport.count("eth_chainId", closing) != 1 {
				t.Fatal("genesis retry repeated the already authenticated chain id")
			}
			var evidence OwnerRecycleOperatorEvidence
			if err := json.Unmarshal(owner.operatorEvidence, &evidence); err != nil || evidence.Decision != fixture.measurement.authority.expected {
				t.Fatal("identity recovery changed the original decision", err)
			}
			assertOwnerRecycleRouteIdentityScope(t, fixture, transport, true)
		}
	}
}

// Empty, malformed, zero and different present values cannot become an absent
// response or a retry marker, even if another method would later be available.
func TestOwnerRecycleRouteIdentityPresentInvalidReplyStaysHard(t *testing.T) {
	for _, closing := range []bool{false, true} {
		for _, method := range []string{"eth_chainId", "chain_getBlockHash"} {
			values := []any{"", "malformed", "0x00", "0x0", "0x1"}
			if method == "chain_getBlockHash" {
				values = []any{"", "malformed", "0x01", common.Hash{}, common.Hash{0xe7}}
			}
			for _, value := range values {
				fixture, transport := newOwnerRecycleRouteIdentityFixture(t)
				transport.script = func(_ context.Context, actual string, actualClosing bool, _ int) (any, error) {
					if actual == method && actualClosing == closing {
						return value, nil
					}
					return transport.expected(actual), nil
				}
				owner, err := fixture.observe(t)
				var unavailable *ownerRecycleRouteUnavailableError
				if owner != nil || err == nil || errors.As(err, &unavailable) || RetryableEvidenceTransportError(err) || transport.waits != 0 || transport.count(method, closing) != 1 {
					t.Fatal("present invalid identity was softened into availability", closing, method, value, err)
				}
				assertOwnerRecycleRouteIdentityScope(t, fixture, transport, closing)
			}
		}
	}
}

// Sustained absence expires the original logical budget with the exact missing
// field retained, never a fabricated canonical or chain identity contradiction.
func TestOwnerRecycleRouteIdentityMissingReplyKeepsOriginalBudget(t *testing.T) {
	for _, closing := range []bool{false, true} {
		for _, method := range []string{"eth_chainId", "chain_getBlockHash"} {
			fixture, transport := newOwnerRecycleRouteIdentityFixture(t)
			transport.script = func(_ context.Context, actual string, actualClosing bool, _ int) (any, error) {
				if actual == method && actualClosing == closing {
					return nil, nil
				}
				return transport.expected(actual), nil
			}
			owner, err := fixture.observe(t)
			var unavailable *ownerRecycleRouteUnavailableError
			if owner != nil || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &unavailable) || unavailable.method != method || !RetryableEvidenceTransportError(err) || transport.waits != 4 || transport.count(method, closing) != 4 || strings.Contains(err.Error(), "changed") {
				t.Fatal("missing identity escaped its original availability budget", closing, method, err)
			}
			assertOwnerRecycleRouteIdentityScope(t, fixture, transport, closing)
		}
	}
}

// A physical deadline carries no completed identity fact. Repeated timeouts
// retain that actual cause without manufacturing a changed value or absence.
func TestOwnerRecycleRouteIdentityTimeoutDoesNotInventConflict(t *testing.T) {
	for _, closing := range []bool{false, true} {
		for _, method := range []string{"eth_chainId", "chain_getBlockHash"} {
			fixture, transport := newOwnerRecycleRouteIdentityFixture(t)
			transport.script = func(_ context.Context, actual string, actualClosing bool, _ int) (any, error) {
				if actual == method && actualClosing == closing {
					return nil, context.DeadlineExceeded
				}
				return transport.expected(actual), nil
			}
			owner, err := fixture.observe(t)
			var unavailable *ownerRecycleRouteUnavailableError
			if owner != nil || !errors.Is(err, context.DeadlineExceeded) || errors.As(err, &unavailable) || !RetryableEvidenceTransportError(err) || transport.waits != 4 || strings.Contains(err.Error(), "changed") {
				t.Fatal("physical identity timeout became a fabricated contradiction", closing, method, err)
			}
			assertOwnerRecycleRouteIdentityScope(t, fixture, transport, closing)
		}
	}
}

// Both joined orders preserve the same original local integrity cause and
// deadline. The transport origin must not authorize retry over that hard leaf.
func TestOwnerRecycleRouteIdentityPreservesMixedOriginalCause(t *testing.T) {
	for _, closing := range []bool{false, true} {
		for _, method := range []string{"eth_chainId", "chain_getBlockHash"} {
			for _, reversed := range []bool{false, true} {
				fixture, transport := newOwnerRecycleRouteIdentityFixture(t)
				hard := &os.LinkError{Op: "rename", Old: "synthetic-original", New: "synthetic-successor", Err: context.DeadlineExceeded}
				cause := errors.Join(hard, context.DeadlineExceeded)
				if reversed {
					cause = errors.Join(context.DeadlineExceeded, hard)
				}
				transport.script = func(_ context.Context, actual string, actualClosing bool, _ int) (any, error) {
					if actual == method && actualClosing == closing {
						return nil, cause
					}
					return transport.expected(actual), nil
				}
				owner, err := fixture.observe(t)
				if owner != nil || !errors.Is(err, cause) || !errors.Is(err, hard) || !errors.Is(err, context.DeadlineExceeded) || RetryableEvidenceTransportError(err) || transport.waits != 0 || transport.count(method, closing) != 1 {
					t.Fatal("identity retry erased an independent original hard cause", closing, method, reversed, err)
				}
				assertOwnerRecycleRouteIdentityScope(t, fixture, transport, closing)
			}
		}
	}
}

// The actual transport failure exposes one original cause graph. A later
// diagnostic/classifier cannot replace its originally hard or temporary edge.
func TestOwnerRecycleRouteIdentityObservesOriginalCauseOnce(t *testing.T) {
	for _, closing := range []bool{false, true} {
		for _, method := range []string{"eth_chainId", "chain_getBlockHash"} {
			for _, temporary := range []bool{false, true} {
				fixture, transport := newOwnerRecycleRouteIdentityFixture(t)
				hard := &os.PathError{Op: "read", Path: "synthetic-route-original", Err: context.DeadlineExceeded}
				cause := &releasePreparationChangingCause{first: hard, later: context.DeadlineExceeded}
				if temporary {
					cause.first, cause.later = context.DeadlineExceeded, hard
				}
				transport.script = func(_ context.Context, actual string, actualClosing bool, count int) (any, error) {
					if actual == method && actualClosing == closing && count == 1 {
						return nil, cause
					}
					return transport.expected(actual), nil
				}
				owner, err := fixture.observe(t)
				if temporary {
					if owner == nil || err != nil || transport.waits != 1 {
						t.Fatal("first temporary route cause was replaced by a later hard edge", closing, method, err)
					}
				} else if owner != nil || !errors.Is(err, cause) || !errors.Is(err, hard) || RetryableEvidenceTransportError(err) || transport.waits != 0 {
					t.Fatal("first hard route cause was replaced by a later timeout", closing, method, err)
				}
				if cause.observations != 1 {
					t.Fatal("route re-inspected its original mutable cause", closing, method, temporary, cause.observations)
				}
				assertOwnerRecycleRouteIdentityScope(t, fixture, transport, closing || temporary)
			}
		}
	}
}

// Cancellation during either actual identity request stops without retaining
// partial authority, retrying, or manufacturing a mismatch from an empty result.
func TestOwnerRecycleRouteIdentityCancellationDiscardsAuthority(t *testing.T) {
	for _, closing := range []bool{false, true} {
		for _, method := range []string{"eth_chainId", "chain_getBlockHash"} {
			fixture, transport := newOwnerRecycleRouteIdentityFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			transport.script = func(_ context.Context, actual string, actualClosing bool, _ int) (any, error) {
				if actual == method && actualClosing == closing {
					cancel()
					return nil, ctx.Err()
				}
				return transport.expected(actual), nil
			}
			owner, err := ObserveOwnerRecycleMeasurementOperators(ctx, fixture.measurement.authority, fixture.chain, fixture.measurement.encoded, fixture.measurement.provider.options(t))
			cancel()
			if owner != nil || !errors.Is(err, context.Canceled) || RetryableEvidenceTransportError(err) || transport.waits != 0 || transport.count(method, closing) != 1 || strings.Contains(err.Error(), "changed") {
				t.Fatal("canceled route request retained authority or fabricated a conflict", closing, method, err)
			}
			assertOwnerRecycleRouteIdentityScope(t, fixture, transport, closing)
		}
	}
}

// Healthy opening and closing pairs use separate physical attempt contexts,
// all clipped by the caller's existing earlier deadline or one default owner.
func TestOwnerRecycleRouteIdentityKeepsSeparateAttemptsAndParentBudget(t *testing.T) {
	for _, bound := range []time.Duration{0, 45 * time.Second} {
		fixture, transport := newOwnerRecycleRouteIdentityFixture(t)
		ctx, cancel := context.WithCancel(t.Context())
		if bound != 0 {
			cancel()
			ctx, cancel = context.WithTimeout(t.Context(), bound)
		}
		owner, err := ObserveOwnerRecycleMeasurementOperators(ctx, fixture.measurement.authority, fixture.chain, fixture.measurement.encoded, fixture.measurement.provider.options(t))
		deadline, bounded := ctx.Deadline()
		cancel()
		if owner == nil || err != nil || transport.waits != 0 || bounded && transport.deadline != deadline {
			t.Fatal("healthy route changed its original caller budget", bound, err)
		}
		transport.stateLock.Lock()
		requests := append([]ownerRecycleRouteTestRequest(nil), transport.requests...)
		transport.stateLock.Unlock()
		if len(requests) != 4 {
			t.Fatal("healthy route did not perform exactly two identity pairs", len(requests))
		}
		for index, request := range requests {
			if request.attempt.owner != transport.deadline || index > 0 && request.attempt.id == requests[index-1].attempt.id {
				t.Fatal("independent physical identity reads shared a single attempt", bound, index)
			}
		}
		assertOwnerRecycleRouteIdentityScope(t, fixture, transport, true)
	}
}
