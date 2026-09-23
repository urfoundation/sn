// Synthetic transports exercise retry and response ownership without sleeping
// or depending on public endpoints. Actor tests retain strict semantic checks.
package main

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Each test owns its request callback; the production transport stays intact.
type adversaryGetTestTransport func(*http.Request) (*http.Response, error)

// Dispatch the request under the original attempt context.
func (self adversaryGetTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

// Use fresh response bodies so a retry cannot accidentally reuse closed input.
func adversaryGetTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

// A zero-delay request gate and explicit wait callback make retry deterministic.
func adversaryGetTestClient(transport adversaryGetTestTransport) *adversaryHTTP {
	return &adversaryHTTP{
		gate: &adversaryRequestGate{now: time.Now}, timeout: time.Minute,
		transportForTest: transport,
		retryWait:        func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	}
}

// Every allowed transport/status class receives one bounded, gated retry.
func TestAdversaryGetRetriesTransientFailuresWithExactAccounting(t *testing.T) {
	failures := []struct {
		status int
		err    error
	}{
		{err: context.DeadlineExceeded}, {err: io.EOF}, {err: io.ErrUnexpectedEOF},
		{err: syscall.ECONNRESET}, {err: syscall.ECONNREFUSED}, {err: syscall.ETIMEDOUT},
		{status: http.StatusRequestTimeout}, {status: http.StatusTooManyRequests},
		{status: http.StatusBadGateway}, {status: http.StatusServiceUnavailable}, {status: http.StatusGatewayTimeout},
	}
	for _, failure := range failures {
		calls := 0
		client := adversaryGetTestClient(func(request *http.Request) (*http.Response, error) {
			calls++
			if request.Method != http.MethodGet || request.URL.Path != "/status" {
				t.Errorf("retry changed read identity: %s %s", request.Method, request.URL)
			}
			if calls == 1 {
				if failure.err != nil {
					return nil, failure.err
				}
				return adversaryGetTestResponse(failure.status, "synthetic outage"), nil
			}
			return adversaryGetTestResponse(http.StatusOK, `{"healthy":true}`), nil
		})
		var waits []time.Duration
		client.retryWait = func(ctx context.Context, delay time.Duration) error { waits = append(waits, delay); return ctx.Err() }
		at := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
		now := at
		client.gate.interval = time.Second
		client.gate.now = func() time.Time { current := now; now = now.Add(time.Second); return current }
		result := client.get(context.Background(), "http://operator.example/status", "", 1024)
		if result.Err != nil || result.Status != http.StatusOK || calls != 2 || result.Requests != 2 || result.TransientFailures != 1 || !reflect.DeepEqual(waits, []time.Duration{adversaryGetRetryDelay}) || !client.gate.next.Equal(at.Add(2*time.Second)) {
			t.Fatalf("failure=%+v result=%+v calls=%d waits=%v gate=%s", failure, result, calls, waits, client.gate.next)
		}
		accounting := adversaryGetAccounting{}
		accounting.observe(result)
		if metrics := accounting.metrics(); metrics["http_attempts"] != 2 || metrics["http_retries"] != 1 || metrics["http_transient_failures"] != 1 {
			t.Fatalf("recovered failure disappeared from metrics: %v", metrics)
		}
	}
}

// Persistent unavailability consumes the finite budget and stays an error.
func TestAdversaryGetExhaustionKeepsFinalFailureAndFiniteBudget(t *testing.T) {
	calls := 0
	client := adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		return adversaryGetTestResponse(http.StatusServiceUnavailable, "synthetic outage"), nil
	})
	var waits []time.Duration
	client.retryWait = func(ctx context.Context, delay time.Duration) error { waits = append(waits, delay); return ctx.Err() }
	result := client.get(context.Background(), "http://operator.example/status", "", 1024)
	if result.Err == nil || calls != 3 || result.Requests != 3 || result.TransientFailures != 3 || !adversaryGetUnavailable(result) || !reflect.DeepEqual(waits, []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}) {
		t.Fatalf("persistent outage result=%+v calls=%d waits=%v", result, calls, waits)
	}
}

// Canceling the sample interrupts backoff without admitting another request.
func TestAdversaryGetCancellationCannotRestartSampleBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	client := adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, context.DeadlineExceeded
	})
	client.retryWait = func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
	result := client.get(ctx, "http://operator.example/status", "", 1024)
	if !errors.Is(result.Err, context.Canceled) || calls != 1 || result.Requests != 1 {
		t.Fatalf("cancellation restarted a request: %+v, calls=%d", result, calls)
	}
	result = client.get(ctx, "http://operator.example/status", "", 1024)
	if !errors.Is(result.Err, context.Canceled) || calls != 1 || result.Requests != 0 {
		t.Fatalf("already canceled sample admitted work: %+v, calls=%d", result, calls)
	}
}

// Retry timing reserves the remaining attempts inside the existing deadline.
func TestAdversaryGetTimeoutDividesOriginalFiniteBudget(t *testing.T) {
	if got := adversaryGetAttemptTimeout(time.Minute, 30*time.Second, 0); got != 10*time.Second {
		t.Fatalf("first timeout=%s", got)
	}
	if got := adversaryGetAttemptTimeout(time.Minute, 18*time.Second, 1); got != 9*time.Second {
		t.Fatalf("second timeout=%s", got)
	}
	if got := adversaryGetAttemptTimeout(5*time.Second, 20*time.Second, 2); got != 5*time.Second {
		t.Fatalf("configured timeout expanded=%s", got)
	}
	if got := adversaryGetAttemptTimeout(0, 0, 0); got != adversaryGetDefaultTimeout {
		t.Fatalf("unconfigured read became unbounded=%s", got)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Minute))
	defer cancel()
	original, _ := ctx.Deadline()
	client := adversaryGetTestClient(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		if !ok || !deadline.Before(original.Add(-30*time.Second)) {
			t.Errorf("first request consumed the full sample deadline: %s, original=%s", deadline, original)
		}
		return adversaryGetTestResponse(http.StatusOK, `{}`), nil
	})
	if result := client.get(ctx, "http://operator.example/status", "", 1024); result.Err != nil {
		t.Fatal(result.Err)
	}
}

// Plain text, certificate and mixed semantic errors are never transport retry
// authority; a responsive permanent HTTP failure is likewise single-shot.
func TestAdversaryGetSemanticFailuresDoNotRetry(t *testing.T) {
	failures := []error{errors.New("synthetic timeout in invalid payload"), x509.UnknownAuthorityError{}, errors.Join(io.EOF, errors.New("synthetic signature mismatch")), context.Canceled}
	for _, failure := range failures {
		calls := 0
		client := adversaryGetTestClient(func(*http.Request) (*http.Response, error) { calls++; return nil, failure })
		result := client.get(context.Background(), "http://operator.example/status", "", 1024)
		if result.Err == nil || calls != 1 || result.TransientFailures != 0 || adversaryGetUnavailable(result) {
			t.Fatalf("semantic failure retried: %v, result=%+v, calls=%d", failure, result, calls)
		}
	}
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		calls := 0
		client := adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
			calls++
			return adversaryGetTestResponse(status, `{}`), nil
		})
		result := client.get(context.Background(), "http://operator.example/status", "", 1024)
		if calls != 1 || result.Status != status || result.TransientFailures != 0 || adversaryGetUnavailable(result) {
			t.Fatalf("permanent HTTP failure retried: %+v, calls=%d", result, calls)
		}
	}
}

// Body reads and close each have one owner, including incomplete transfers.
type adversaryGetTestBody struct {
	read  func([]byte) (int, error)
	close func() error
}

// Deliver the explicit test transport condition.
func (self *adversaryGetTestBody) Read(value []byte) (int, error) { return self.read(value) }

// Publish close before another request is admitted.
func (self *adversaryGetTestBody) Close() error { return self.close() }

// An incomplete body closes before retry; a successful oversized response is
// an integrity/resource error and cannot multiply traffic through retries.
func TestAdversaryGetClosesIncompleteBodyBeforeRetryAndKeepsSizeBound(t *testing.T) {
	calls, closes := 0, 0
	client := adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: &adversaryGetTestBody{
				read:  func([]byte) (int, error) { return 0, io.ErrUnexpectedEOF },
				close: func() error { closes++; return nil },
			}}, nil
		}
		if closes != 1 {
			t.Error("retried before closing the incomplete body")
		}
		return adversaryGetTestResponse(http.StatusOK, `{}`), nil
	})
	result := client.get(context.Background(), "http://operator.example/status", "", 16)
	if result.Err != nil || calls != 2 || closes != 1 || result.TransientFailures != 1 {
		t.Fatalf("body retry=%+v calls=%d closes=%d", result, calls, closes)
	}
	calls = 0
	client.transportForTest = adversaryGetTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return adversaryGetTestResponse(http.StatusOK, strings.Repeat("x", 17)), nil
	})
	result = client.get(context.Background(), "http://operator.example/status", "", 16)
	if result.Err == nil || calls != 1 || result.TransientFailures != 0 {
		t.Fatalf("oversized response became retryable: %+v, calls=%d", result, calls)
	}
}

// Only the explicit GET entry point retries. Mutation probes retain the exact
// original one-request semantics even when their response is ambiguous.
func TestAdversaryGetDoesNotAuthorizePostReplay(t *testing.T) {
	calls := 0
	client := adversaryGetTestClient(func(*http.Request) (*http.Response, error) { calls++; return nil, context.DeadlineExceeded })
	if _, _, err := client.do(context.Background(), http.MethodPost, "http://operator.example/verify", "", []byte(`{}`), 1024); err == nil || calls != 1 {
		t.Fatalf("POST was retried: calls=%d error=%v", calls, err)
	}
}

// Redirects cannot leave the exact evidence origin or hide unmetered calls.
func TestAdversaryGetDoesNotFollowRedirectToAnotherEvidenceOrigin(t *testing.T) {
	calls := 0
	client := adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		response := adversaryGetTestResponse(http.StatusFound, "synthetic redirect")
		response.Header.Set("Location", "http://foreign.example/artifact")
		return response, nil
	})
	result := client.get(context.Background(), "http://operator.example/status", "", 1024)
	if calls != 1 || result.Requests != 1 || result.Status != http.StatusFound || adversaryGetUnavailable(result) {
		t.Fatalf("redirect escaped the exact request origin: %+v, calls=%d", result, calls)
	}
}

// Response cleanup failure is not success and cannot turn into an automatic
// retry by mentioning a timeout in its diagnostic text.
func TestAdversaryGetCloseFailureRemainsHard(t *testing.T) {
	calls, closes := 0, 0
	client := adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		reader := strings.NewReader(`{}`)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: &adversaryGetTestBody{
			read: reader.Read, close: func() error { closes++; return errors.New("synthetic cleanup timeout text") },
		}}, nil
	})
	result := client.get(context.Background(), "http://operator.example/status", "", 1024)
	if result.Err == nil || result.Requests != 1 || calls != 1 || closes != 1 || result.TransientFailures != 0 {
		t.Fatalf("close failure was retried or ignored: %+v, calls=%d closes=%d", result, calls, closes)
	}
}

// Actor evidence includes recovered failures, while an exhausted unscheduled
// outage and malformed JSON remain errors under the final error budget.
func TestAdversaryGetOperatorSampleRetainsRecoveryAndStrictFailures(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	if err := writePublicJSON(filepath.Join(stateDir, "supervisor.state.json"), SupervisorState{Processes: []ProcessState{{ID: "operator-1-api", Role: "api", Identity: "synthetic-api", Healthy: true}}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, context.DeadlineExceeded
		}
		return adversaryGetTestResponse(http.StatusOK, `{"healthy":true}`), nil
	})
	window := newAdversaryFaultWindow(time.Second)
	actor := &operatorAPIAdversary{cfg: cfg, stateDir: stateDir, http: client, faults: window}
	result := actor.Sample(context.Background(), adversaryControlPhase, 0)
	if result.Outcome != adversaryOutcomeSuccess || result.Requests != 2 || result.Metrics["http_retries"] != 1 || result.Metrics["http_transient_failures"] != 1 {
		t.Fatalf("recovered API observation=%+v", result)
	}
	client.transportForTest = adversaryGetTestTransport(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })
	result = actor.Sample(context.Background(), adversaryControlPhase, 0)
	if result.Outcome != adversaryOutcomeError || result.Requests != 3 || result.Metrics["http_transient_failures"] != 3 {
		t.Fatalf("exhausted outage lost its final error budget: %+v", result)
	}
	window.Update([]string{"operator-1-api"})
	calls = 0
	client.transportForTest = adversaryGetTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return adversaryGetTestResponse(http.StatusOK, "not-json"), nil
	})
	result = actor.Sample(context.Background(), adversaryControlPhase, 0)
	if result.Outcome != adversaryOutcomeError || result.Requests != 1 || calls != 1 {
		t.Fatalf("scheduled outage hid malformed JSON: %+v", result)
	}
}

// A malformed payout page is never attributed to an API outage. The exact
// signed history and artifact validation remain outside the retry boundary.
func TestAdversaryGetMalformedHistoryIsHardForArtifactAndMerkleActors(t *testing.T) {
	cfg := testResolvedConfig(t)
	calls := 0
	client := adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		return adversaryGetTestResponse(http.StatusOK, "not-json"), nil
	})
	window := newAdversaryFaultWindow(time.Second)
	window.Update([]string{"operator-1-api"})
	actor := &artifactAdversary{cfg: cfg, http: client, faults: window}
	result := actor.Sample(context.Background(), adversaryAttackPhase, 0)
	if result.Outcome != adversaryOutcomeError || calls != 1 || result.Requests != 1 {
		t.Fatalf("scheduled outage hid invalid artifact history: %+v, calls=%d", result, calls)
	}
	stateDir := t.TempDir()
	if err := saveContractDeployment(stateDir, ContractDeployment{CoordinatorProxy: common.HexToAddress("0x100"), SettlementVault: common.HexToAddress("0x200")}); err != nil {
		t.Fatal(err)
	}
	evidence, err := liveInvalidMerkleProofProbe(context.Background(), cfg, stateDir, "http://operator.example", "http://rpc.example", 1, client, client, 0)
	if err == nil || errors.Is(err, errLiveMerkleOperatorUnavailable) || liveMerkleRetryable(err, true) || evidence.Requests != 1 || calls != 2 {
		t.Fatalf("Merkle semantic failure became an outage: evidence=%+v error=%v calls=%d", evidence, err, calls)
	}
}

// Adjacent key/proof GET callers propagate actual attempts and reject malformed
// successful bodies immediately, keeping their cryptographic checks unchanged.
func TestAdversaryGetVerifyCallersCountRetriesAndRejectMalformedBodies(t *testing.T) {
	calls := 0
	client := adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return adversaryGetTestResponse(http.StatusGatewayTimeout, "synthetic outage"), nil
		}
		return adversaryGetTestResponse(http.StatusOK, `{"keys":[]}`), nil
	})
	window := newAdversaryFaultWindow(time.Second)
	window.Update([]string{"operator-1-api"})
	actor := &verifyAdversary{http: client, faults: window}
	if _, requests, err := actor.serverKeys(context.Background(), 1); err == nil || requests != 2 || calls != 2 || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("verify key observation requests=%d calls=%d error=%v", requests, calls, err)
	} else if result := actor.sampleError(1, err, requests, 1); result.Outcome != adversaryOutcomeError {
		t.Fatalf("scheduled outage hid malformed key evidence: %+v", result)
	}
	calls = 0
	client.transportForTest = adversaryGetTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, io.ErrUnexpectedEOF
		}
		return adversaryGetTestResponse(http.StatusOK, fmt.Sprintf(`{"schema":%q,"rows":[]}`, "synthetic-wrong-schema")), nil
	})
	if requests, err := actor.requireUniqueProof(context.Background(), 1, [16]byte{}); err == nil || requests != 2 || calls != 2 {
		t.Fatalf("verify proof observation requests=%d calls=%d error=%v", requests, calls, err)
	} else if result := actor.sampleError(1, err, requests, 1); result.Outcome != adversaryOutcomeError {
		t.Fatalf("scheduled outage hid malformed proof history: %+v", result)
	}
}

// Worktree aliases must expose the same exact source references as physical
// repositories; resolving a root never manufactures a missing declaration.
func TestAdversaryReferenceDiscoveryFollowsSymlinkedRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "physical")
	if err := os.MkdirAll(filepath.Join(physical, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{
		"synthetic_test.go":     "package synthetic\nimport \"testing\"\nfunc TestSyntheticOwned(t *testing.T) {}\n",
		"Synthetic.t.sol":       "contract Synthetic { function test_owned() public {} }\n",
		"scripts/test-owned.sh": "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(physical, name), []byte(contents), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	references := map[string]bool{}
	discoverGoTestReferences(t, alias, "server", references)
	discoverSolidityTestReferences(t, alias, references)
	discoverShellTestReferences(t, alias, references)
	if !reflect.DeepEqual(references, map[string]bool{"server/TestSyntheticOwned": true, "Synthetic.test_owned": true, "scripts/test-owned.sh": true}) {
		t.Fatalf("aliased source references=%v", references)
	}
}
