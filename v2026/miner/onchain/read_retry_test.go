// Public submissions use real HTTP/geth decoding while an instance clock
// advances only after a complete failed response. No test sleeps for recovery.
package onchain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// The handler records exact parameters separately from geth's changing Rpc id.
// Its mutable observations are shared with the test only through this lock.
type onchainReadTestServer struct {
	t          *testing.T
	server     *httptest.Server
	stateLock  sync.Mutex
	calls      map[string]int
	parameters map[string][]byte
	reject     func(string, int) int
	firstHash  common.Hash
	firstRaw   []byte
}

// Every accepted method corresponds to one concrete production callsite.
func newOnchainReadTestServer(t *testing.T, reject func(string, int) int) *onchainReadTestServer {
	t.Helper()
	self := &onchainReadTestServer{t: t, calls: map[string]int{}, parameters: map[string][]byte{}, reject: reject}
	self.server = httptest.NewServer(http.HandlerFunc(self.serve))
	t.Cleanup(self.server.Close)
	return self
}

// Complete responses and explicit HTTP failures exercise the physical adapter.
func (self *onchainReadTestServer) serve(writer http.ResponseWriter, request *http.Request) {
	var call struct {
		Id     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		self.t.Error(err)
		return
	}
	count := func() int {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.calls[call.Method]++
		if call.Method != "eth_getBlockByNumber" {
			if original, ok := self.parameters[call.Method]; ok && !bytes.Equal(original, call.Params) {
				self.t.Errorf("retry changed original %s parameters", call.Method)
			} else if !ok {
				self.parameters[call.Method] = append([]byte(nil), call.Params...)
			}
		}
		return self.calls[call.Method]
	}()
	if self.reject != nil {
		if status := self.reject(call.Method, count); status != 0 {
			http.Error(writer, "synthetic temporary response", status)
			return
		}
	}
	var result any
	switch call.Method {
	case "eth_chainId":
		result = "0x67932"
	case "eth_call":
		result = "0x"
	case "eth_estimateGas":
		result = "0x5208"
	case "eth_getTransactionCount":
		result = "0x7"
	case "eth_gasPrice":
		result = "0x2"
	case "eth_sendRawTransaction":
		var arguments []string
		if err := json.Unmarshal(call.Params, &arguments); err != nil || len(arguments) != 1 {
			self.t.Error("synthetic send lacks exact raw transaction")
			return
		}
		raw, err := hex.DecodeString(strings.TrimPrefix(arguments[0], "0x"))
		var tx types.Transaction
		if err != nil || tx.UnmarshalBinary(raw) != nil {
			self.t.Error("synthetic send cannot decode original transaction")
			return
		}
		self.stateLock.Lock()
		self.firstHash, self.firstRaw = tx.Hash(), append([]byte(nil), raw...)
		self.stateLock.Unlock()
		result = tx.Hash().Hex()
	case "eth_getTransactionReceipt":
		if count == 2 {
			result = nil
			break
		}
		self.stateLock.Lock()
		hash := self.firstHash
		self.stateLock.Unlock()
		result = &types.Receipt{Status: 1, CumulativeGasUsed: 21000, Logs: []*types.Log{}, TxHash: hash, GasUsed: 21000, EffectiveGasPrice: big.NewInt(2), BlockHash: common.Hash{0x44}, BlockNumber: big.NewInt(4)}
	case "eth_getBlockByNumber":
		var arguments []json.RawMessage
		var selector string
		if json.Unmarshal(call.Params, &arguments) != nil || len(arguments) != 2 || json.Unmarshal(arguments[0], &selector) != nil {
			self.t.Error("synthetic block request has no selector")
			return
		}
		if selector == "0x4" {
			result = syntheticBlockIdentityFixture(self.t, 4, common.Hash{0x44})
		} else if selector == "finalized" || selector == "0x6" {
			result = syntheticBlockIdentityFixture(self.t, 6, common.Hash{0x66})
		} else {
			self.t.Errorf("unexpected block selector %q", selector)
		}
	default:
		self.t.Errorf("unexpected synthetic submission method %s", call.Method)
	}
	_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result})
}

// Snapshot a completed method without relying on handler scheduling.
func (self *onchainReadTestServer) count(method string) int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.calls[method]
}

// The key and addresses are synthetic; all transports terminate at the fixture.
func (self *onchainReadTestServer) params(dryRun bool) SubmitParams {
	self.t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("17", 32))
	if err != nil {
		self.t.Fatal(err)
	}
	return SubmitParams{Contract: common.Address{0x53}, Rpcs: []string{self.server.URL}, Key: key, Calldata: []byte{1, 2, 3, 4}, ChainID: big.NewInt(424242), DryRun: dryRun}
}

// Keep actual per-request contexts; replace only pacing and observe the outer
// deadline requested by each real read owner.
func onchainReadTestContext(t *testing.T, parent context.Context, waits func(context.Context, time.Duration) error) (context.Context, *atomic.Int32) {
	t.Helper()
	budgets := &atomic.Int32{}
	hooks := onchainReadRetryHooks{withTimeout: func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
		if duration != 300*time.Second {
			t.Errorf("read recovery allowance changed to %s", duration)
		}
		budgets.Add(1)
		return context.WithTimeout(ctx, duration)
	}, wait: waits}
	if hooks.wait == nil {
		hooks.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	}
	return context.WithValue(parent, onchainReadRetryHooksKey{}, hooks), budgets
}

// The initial identity read can survive 250 logical seconds under its original
// five-minute allowance; retries cannot mutate later preflight or signing data.
func TestOnchainSubmitReadRecoversOriginalFiveMinuteIntent(t *testing.T) {
	var elapsed atomic.Int64
	fixture := newOnchainReadTestServer(t, func(method string, _ int) int {
		if method == "eth_chainId" && time.Duration(elapsed.Load()) < 250*time.Second {
			return http.StatusServiceUnavailable
		}
		return 0
	})
	params := fixture.params(true)
	ctx, budgets := onchainReadTestContext(t, t.Context(), func(ctx context.Context, delay time.Duration) error {
		if delay < 2*time.Second || delay >= 3*time.Second {
			t.Fatal("read retry lost its finite pacing")
		}
		elapsed.Add(int64(delay))
		params.Calldata[0] = 99
		params.ChainID.SetInt64(999999)
		return ctx.Err()
	})
	result, err := Submit(ctx, params)
	if err != nil || result != nil || budgets.Load() != 3 || fixture.count("eth_chainId") <= 64 || fixture.count("eth_call") != 1 || fixture.count("eth_estimateGas") != 1 || fixture.count("eth_sendRawTransaction") != 0 || time.Duration(elapsed.Load()) >= 300*time.Second {
		t.Fatalf("public dry-run lost read recovery: result=%v err=%v budgets=%d elapsed=%s", result, err, budgets.Load(), time.Duration(elapsed.Load()))
	}
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	if !bytes.Contains(fixture.parameters["eth_call"], []byte("0x01020304")) || !bytes.Contains(fixture.parameters["eth_estimateGas"], []byte("0x01020304")) {
		t.Fatal("a later caller mutation replaced retained preflight calldata")
	}
}

// Every read before signing may recover independently. An ambiguous send is
// still exactly one send and one prepared original, never a read-loop retry.
func TestOnchainSubmitReadRecoveryNeverRetriesSignedTransport(t *testing.T) {
	fixture := newOnchainReadTestServer(t, func(method string, count int) int {
		if count == 1 || method == "eth_sendRawTransaction" {
			return http.StatusServiceUnavailable
		}
		return 0
	})
	ctx, budgets := onchainReadTestContext(t, t.Context(), nil)
	prepared, before, broadcast := 0, 0, 0
	var original []byte
	result, err := SubmitWithHooks(ctx, fixture.params(false), SubmitHooks{
		Prepared:        func(_ common.Hash, raw []byte) error { prepared++; original = append([]byte(nil), raw...); return nil },
		BeforeBroadcast: func(common.Hash) error { before++; return nil },
		Broadcast:       func(common.Hash) error { broadcast++; return nil },
	})
	for _, method := range []string{"eth_chainId", "eth_call", "eth_estimateGas", "eth_getTransactionCount", "eth_gasPrice"} {
		if fixture.count(method) != 2 {
			t.Errorf("read %s did not retain its retry owner: %d", method, fixture.count(method))
		}
	}
	if err == nil || result != nil || budgets.Load() != 5 || prepared != 1 || before != 1 || broadcast != 0 || len(original) == 0 || fixture.count("eth_sendRawTransaction") != 1 || fixture.count("eth_getTransactionReceipt") != 0 {
		t.Fatalf("read recovery changed signed transport: result=%v err=%v prepared=%d before=%d broadcast=%d", result, err, prepared, before, broadcast)
	}
}

// A gateway outage after the only send does not abort the original receipt
// observation or create a second signature; ordinary finality still closes.
func TestOnchainSubmitRetainsReceiptObservationAfterTransientHttp(t *testing.T) {
	fixture := newOnchainReadTestServer(t, func(method string, count int) int {
		if method == "eth_getTransactionReceipt" && count == 1 {
			return http.StatusServiceUnavailable
		}
		return 0
	})
	ctx, _ := onchainReadTestContext(t, t.Context(), nil)
	prepared := 0
	result, err := SubmitWithHooks(ctx, fixture.params(false), SubmitHooks{Prepared: func(common.Hash, []byte) error { prepared++; return nil }})
	if err != nil || result == nil || result.Status != 1 || result.BlockHash != (common.Hash{0x44}) || prepared != 1 || fixture.count("eth_sendRawTransaction") != 1 || fixture.count("eth_getTransactionReceipt") != 3 || fixture.count("eth_getBlockByNumber") != 5 {
		t.Fatalf("receipt recovery changed original submission: result=%v err=%v prepared=%d", result, err, prepared)
	}
}

// Malformed identity remains hard at endpoint selection; a second healthy
// endpoint cannot launder the first configured endpoint's completed refusal.
func TestOnchainSubmitReadRefusesHardIdentityBeforeFailover(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			Id json.RawMessage `json:"id"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": "not-a-quantity"})
	}))
	defer first.Close()
	second := newOnchainReadTestServer(t, nil)
	params := second.params(true)
	params.Rpcs = []string{first.URL, second.server.URL}
	waits := 0
	ctx, _ := onchainReadTestContext(t, t.Context(), func(context.Context, time.Duration) error { waits++; return nil })
	if result, err := Submit(ctx, params); err == nil || result != nil || waits != 0 || second.count("eth_chainId") != 0 {
		t.Fatalf("hard identity gained retry/failover authority: result=%v err=%v waits=%d", result, err, waits)
	}
}

// A completed contract refusal cannot gain availability authority from its
// server error code. No estimate, durable preparation or send follows it.
func TestOnchainSubmitPreflightRefusalNeverRetriesOrSigns(t *testing.T) {
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			Id     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		if call.Method == "eth_chainId" {
			_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": "0x67932"})
			return
		}
		reads.Add(1)
		if call.Method != "eth_call" {
			t.Errorf("refused preflight admitted %s", call.Method)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "error": map[string]any{"code": -32000, "message": "synthetic contract refusal", "data": "0x"}})
	}))
	defer server.Close()
	fixture := newOnchainReadTestServer(t, nil)
	params := fixture.params(false)
	params.Rpcs = []string{server.URL}
	waits, prepared := 0, 0
	ctx, _ := onchainReadTestContext(t, t.Context(), func(context.Context, time.Duration) error { waits++; return errors.New("synthetic forbidden retry") })
	result, err := SubmitWithHooks(ctx, params, SubmitHooks{Prepared: func(common.Hash, []byte) error { prepared++; return nil }})
	if err == nil || result != nil || reads.Load() != 1 || waits != 0 || prepared != 0 {
		t.Fatalf("contract refusal gained read authority: result=%v err=%v reads=%d waits=%d prepared=%d", result, err, reads.Load(), waits, prepared)
	}
}

// An explicit gas limit may replace an unavailable estimate, but cannot turn
// cancellation of that real request into a successful dry-run completion.
func TestOnchainSubmitExplicitGasCannotHideEstimateCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture := newOnchainReadTestServer(t, func(method string, _ int) int {
		if method == "eth_estimateGas" {
			cancel()
			return http.StatusServiceUnavailable
		}
		return 0
	})
	params := fixture.params(true)
	params.GasLimit = 25000
	ctx, _ := onchainReadTestContext(t, parent, nil)
	result, err := Submit(ctx, params)
	if result != nil || !errors.Is(err, context.Canceled) || fixture.count("eth_estimateGas") != 1 || fixture.count("eth_getTransactionCount") != 0 || fixture.count("eth_sendRawTransaction") != 0 {
		t.Fatalf("explicit gas limit hid canceled estimate: result=%v err=%v", result, err)
	}
}

// Cancellation interrupts the actual current HTTP request and joins it; no
// later endpoint, signer or read owner is admitted after the server barrier.
func TestOnchainSubmitReadCancellationJoinsCurrentRequest(t *testing.T) {
	watchdog, stop := context.WithTimeout(t.Context(), 30*time.Second)
	defer stop()
	parent, cancel := context.WithCancel(watchdog)
	defer cancel()
	started, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		close(started)
		select {
		case <-request.Context().Done():
		case <-watchdog.Done():
		}
		close(stopped)
	}))
	defer func() {
		cancel()
		server.CloseClientConnections()
		server.Close()
	}()
	fixture := newOnchainReadTestServer(t, nil)
	params := fixture.params(true)
	params.Rpcs = []string{server.URL, fixture.server.URL}
	ctx, budgets := onchainReadTestContext(t, parent, func(ctx context.Context, _ time.Duration) error {
		return errors.Join(ctx.Err(), watchdog.Err())
	})
	done := make(chan error, 1)
	go func() { _, err := Submit(ctx, params); done <- err }()
	select {
	case <-started:
	case <-watchdog.Done():
		t.Fatal("public submission never reached actual request")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || budgets.Load() != 1 || fixture.count("eth_chainId") != 0 {
			t.Fatalf("canceled read continued: budgets=%d err=%v", budgets.Load(), err)
		}
	case <-watchdog.Done():
		t.Fatal("canceled public submission did not join")
	}
	select {
	case <-stopped:
	case <-watchdog.Done():
		t.Fatal("canceled request retained its HTTP handler")
	}
}

// A deterministic deadline is advanced solely by completed retry waits.
type onchainReadTestDeadline struct {
	context.Context
	deadline time.Time
	done     chan struct{}
	expired  atomic.Bool
}

// Child request timers can observe the owner's original finite deadline.
func (self *onchainReadTestDeadline) Deadline() (time.Time, bool) { return self.deadline, true }

// The test closes this once only after its last physical request has returned.
func (self *onchainReadTestDeadline) Done() <-chan struct{} { return self.done }

// Keep cancellation distinct from a retry budget exhausted by the test clock.
func (self *onchainReadTestDeadline) Err() error {
	if self.expired.Load() {
		return context.DeadlineExceeded
	}
	return self.Context.Err()
}

// Idempotent closure also releases any child context watchers during cleanup.
func (self *onchainReadTestDeadline) finish() {
	if self.expired.CompareAndSwap(false, true) {
		close(self.done)
	}
}

// Repeated fast gateway failures cannot renew a public command's read budget.
func TestOnchainSubmitReadDeadlineRetainsLastFailure(t *testing.T) {
	fixture := newOnchainReadTestServer(t, func(string, int) int { return http.StatusServiceUnavailable })
	var elapsed time.Duration
	var owner *onchainReadTestDeadline
	budgets := 0
	ctx := context.WithValue(t.Context(), onchainReadRetryHooksKey{}, onchainReadRetryHooks{
		withTimeout: func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			budgets++
			if duration != 300*time.Second {
				t.Fatal("public read did not request the original five-minute allowance")
			}
			owner = &onchainReadTestDeadline{Context: ctx, deadline: time.Now().Add(duration), done: make(chan struct{})}
			return owner, owner.finish
		},
		wait: func(ctx context.Context, delay time.Duration) error {
			elapsed += delay
			if elapsed >= 300*time.Second {
				owner.finish()
			}
			return ctx.Err()
		},
	})
	result, err := Submit(ctx, fixture.params(true))
	var status rpc.HTTPError
	if result != nil || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &status) || status.StatusCode != http.StatusServiceUnavailable || budgets != 1 || fixture.count("eth_chainId") <= 64 || fixture.count("eth_call") != 0 {
		t.Fatalf("deadline erased cause or renewed read: result=%v err=%v budgets=%d elapsed=%s", result, err, budgets, elapsed)
	}
}

// The actual geth client may return several transport causes. This adapter
// gives that client an instance-owned error without changing global transport.
type onchainReadTestTransport func(*http.Request) (*http.Response, error)

// A request always completes synchronously before another may be admitted.
func (self onchainReadTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

// Neither finality nor receipt polling may hide a hard cause behind a timeout
// or NotFound leaf. Both execute the real geth HTTP error propagation path.
func TestOnchainReceiptReadsRefuseMixedHardTransportCauses(t *testing.T) {
	hard := errors.New("synthetic physical response integrity failure")
	for _, finality := range []bool{false, true} {
		for _, cause := range []error{
			errors.Join(&net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, ethereum.NotFound, hard),
			&os.LinkError{Op: "read", Old: "synthetic-old", New: "synthetic-new", Err: context.DeadlineExceeded},
			&net.DNSError{IsTimeout: true, UnwrapErr: &os.PathError{Op: "read", Path: "synthetic-custody", Err: context.DeadlineExceeded}},
			&net.DNSError{IsTimeout: true, UnwrapErr: errors.Join(io.EOF, hard)},
			&net.DNSError{IsTimeout: true, IsNotFound: true, UnwrapErr: syscall.ECONNRESET},
			&net.DNSError{IsNotFound: true, UnwrapErr: syscall.ECONNRESET},
			&net.DNSError{UnwrapErr: &os.PathError{Op: "read", Path: "synthetic-custody", Err: context.DeadlineExceeded}},
			&net.DNSError{},
			&net.DNSError{IsTemporary: true, UnwrapErr: context.Canceled},
		} {
			calls, waits := 0, 0
			transport := onchainReadTestTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, cause
			})
			client, err := rpc.DialOptions(t.Context(), "http://synthetic-rpc.example", rpc.WithHTTPClient(&http.Client{Transport: transport}))
			if err != nil {
				t.Fatal(err)
			}
			actual := ethclient.NewClient(client)
			ctx, _ := onchainReadTestContext(t, t.Context(), func(context.Context, time.Duration) error { waits++; return errors.New("synthetic unexpected retry") })
			if finality {
				err = waitFinalized(ctx, actual, &types.Receipt{TxHash: common.Hash{0x11}, BlockHash: common.Hash{0x44}, BlockNumber: big.NewInt(4)})
			} else {
				_, err = waitMined(ctx, actual, common.Hash{0x11})
			}
			actual.Close()
			if !errors.Is(err, cause) || calls != 1 || waits != 0 {
				t.Fatalf("hard transport cause became retryable: cause=%T finality=%t calls=%d waits=%d err=%v", cause, finality, calls, waits, err)
			}
		}
	}
}

// DNS availability flags and complete transient children retain recovery at the
// actual finality owner. The next read traverses a real HTTP server and parser.
func TestOnchainFinalityRecoversCompleteDnsAvailability(t *testing.T) {
	for _, cause := range []error{&net.DNSError{IsTimeout: true}, &net.DNSError{IsTemporary: true, UnwrapErr: syscall.ECONNRESET}, &net.DNSError{UnwrapErr: syscall.ECONNRESET}, &net.DNSError{UnwrapErr: context.DeadlineExceeded}} {
		fixture := newOnchainReadTestServer(t, nil)
		base := &http.Transport{Proxy: nil}
		calls, waits := 0, 0
		transport := onchainReadTestTransport(func(request *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return nil, cause
			}
			return base.RoundTrip(request)
		})
		client, err := rpc.DialOptions(t.Context(), fixture.server.URL, rpc.WithHTTPClient(&http.Client{Transport: transport}))
		if err != nil {
			base.CloseIdleConnections()
			t.Fatal(err)
		}
		actual := ethclient.NewClient(client)
		ctx, _ := onchainReadTestContext(t, t.Context(), func(ctx context.Context, _ time.Duration) error { waits++; return ctx.Err() })
		err = waitFinalized(ctx, actual, &types.Receipt{TxHash: common.Hash{0x11}, BlockHash: common.Hash{0x44}, BlockNumber: big.NewInt(4)})
		actual.Close()
		base.CloseIdleConnections()
		if err != nil || waits != 1 || calls != 6 || fixture.count("eth_getBlockByNumber") != 5 {
			t.Fatalf("complete DNS availability lost recovery: cause=%T calls=%d waits=%d err=%v", cause, calls, waits, err)
		}
	}
}

// A public submission retains the actual unavailable identity result beneath
// an unflagged DNS wrapper. The next real read leads to one original send.
func TestOnchainSubmitRecoversDnsWrappedOriginalRead(t *testing.T) {
	fixture := newOnchainReadTestServer(t, func(method string, count int) int {
		if method == "eth_chainId" && count == 1 {
			return http.StatusServiceUnavailable
		}
		return 0
	})
	parent, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	waits, faults := 0, 0
	ctx, budgets := onchainReadTestContext(t, parent, func(ctx context.Context, _ time.Duration) error { waits++; return ctx.Err() })
	hooks := ctx.Value(onchainReadRetryHooksKey{}).(onchainReadRetryHooks)
	hooks.additionalReadError = func(err error) error {
		faults++
		return &net.DNSError{Err: "synthetic wrapped original read", Name: "identity.example", UnwrapErr: err}
	}
	ctx = context.WithValue(ctx, onchainReadRetryHooksKey{}, hooks)
	prepared, before, broadcast := 0, 0, 0
	var original []byte
	var originalHash common.Hash
	result, err := SubmitWithHooks(ctx, fixture.params(false), SubmitHooks{
		Prepared: func(hash common.Hash, raw []byte) error {
			prepared++
			originalHash, original = hash, bytes.Clone(raw)
			return nil
		},
		BeforeBroadcast: func(common.Hash) error { before++; return nil },
		Broadcast:       func(common.Hash) error { broadcast++; return nil },
	})
	if err != nil || result == nil || faults != 1 || waits != 1 || budgets.Load() != 5 || prepared != 1 || before != 1 || broadcast != 1 || fixture.count("eth_chainId") != 2 || fixture.count("eth_sendRawTransaction") != 1 || fixture.count("eth_getTransactionReceipt") != 1 || fixture.count("eth_getBlockByNumber") != 5 {
		t.Fatalf("public submission lost original DNS child recovery: result=%v err=%v faults=%d waits=%d budgets=%d", result, err, faults, waits, budgets.Load())
	}
	fixture.stateLock.Lock()
	sameOriginal := bytes.Equal(original, fixture.firstRaw) && originalHash == fixture.firstHash
	fixture.stateLock.Unlock()
	if len(original) == 0 || !sameOriginal {
		t.Fatal("DNS recovery replaced the originally prepared transaction")
	}
}

// Classifiers must not ask a foreign leaf to emulate errors.Is/errors.As.
type onchainReadTestForeignError struct{}

func (*onchainReadTestForeignError) Error() string { return "synthetic foreign error" }
func (*onchainReadTestForeignError) Is(error) bool { panic("foreign Is invoked") }
func (*onchainReadTestForeignError) As(any) bool   { panic("foreign As invoked") }

// A cycle must exhaust fixed traversal work without consulting Error text.
type onchainReadTestCycle struct{ visits int }

func (*onchainReadTestCycle) Error() string      { return "synthetic cycle" }
func (self *onchainReadTestCycle) Unwrap() error { self.visits++; return self }

// Resource bounds also apply to unknown graphs at the concrete finality owner.
func TestOnchainReadClassificationBoundsIncompleteErrorGraphs(t *testing.T) {
	var absent *net.OpError
	cycle := &onchainReadTestCycle{}
	wide := make([]error, 129)
	for index := range wide {
		wide[index] = io.EOF
	}
	for _, cause := range []error{absent, cycle, errors.Join(wide...), &onchainReadTestForeignError{}, &os.PathError{Op: "read", Path: "synthetic-custody", Err: syscall.ECONNRESET}, errors.Join(io.EOF, context.Canceled), errors.Join(io.EOF, errors.New("synthetic malformed response"))} {
		if retryableOnchainRead(cause, true) {
			t.Fatalf("incomplete graph gained retry authority: %T", cause)
		}
	}
	if cycle.visits > 33 {
		t.Fatal("cyclic error exceeded finite traversal work")
	}
}
