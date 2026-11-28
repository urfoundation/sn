//go:build linux || darwin

// Transient upstream failures are waits, never a process restart. Each test
// reproduces a validator crash on snow with the standard library's own error
// values, or with the exact refusal an upstream returned; none opens a socket.
package validator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
)

// The body nginx sends with a limit_req refusal.
const releaseTransientTestRateLimitBody = "<html>\r\n<head><title>429 Too Many Requests</title></head>\r\n<body>\r\n<center><h1>429 Too Many Requests</h1></center>\r\n<hr><center>nginx</center>\r\n</body>\r\n</html>\r\n"

// net's own dial deadline value. An already expired context ends the dial
// before any socket is opened, and net reports it as "i/o timeout".
func releaseTransientTestDialTimeout(t *testing.T) *net.OpError {
	t.Helper()
	expired, cancel := context.WithDeadline(t.Context(), time.Unix(1, 0))
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(expired, "tcp", "192.0.2.1:443")
	if conn != nil {
		conn.Close()
	}
	var dial *net.OpError
	if !errors.As(err, &dial) || dial.Err == nil || dial.Err == context.DeadlineExceeded || dial.Err.Error() != "i/o timeout" || !errors.Is(dial.Err, context.DeadlineExceeded) {
		t.Fatalf("expired dial did not return net's own deadline value: %v", err)
	}
	return dial
}

// The error net's lookup returns when the dial deadline ends a lookup:
// newDNSError(mapErr(ctx.Err()), host, "") in lookupIPAddr, built here from
// net's own deadline value. It reads like the journal of 2026-11-13:
// `Get "https://api.example/verify/keys": dial tcp: lookup api.example: i/o timeout`.
func releaseTransientTestLookupTimeout(t *testing.T) error {
	t.Helper()
	timeout := releaseTransientTestDialTimeout(t).Err
	lookup := &net.DNSError{UnwrapErr: timeout, Err: timeout.Error(), Name: "api.example", IsTimeout: true, IsTemporary: true}
	return &url.Error{Op: "Get", URL: "https://api.example/verify/keys", Err: &net.OpError{Op: "dial", Net: "tcp", Err: lookup}}
}

// net/http's own Client.Timeout value. The transport is real; its dialer only
// waits for the request deadline, so the client reports the timeout itself.
func releaseTransientTestClientTimeout(t *testing.T) error {
	t.Helper()
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: time.Nanosecond, Transport: transport}
	response, err := client.Get("https://api.example/sn/client-key/observations")
	if response != nil {
		response.Body.Close()
	}
	var failure *url.Error
	if !errors.As(err, &failure) || fmt.Sprintf("%T", failure.Err) != "*http.timeoutError" {
		t.Fatalf("client deadline did not return net/http's own timeout value: %T %v", err, err)
	}
	return err
}

// net's own "operation was canceled", as a dial in flight at shutdown sees it.
func releaseTransientTestDialCanceled(t *testing.T) (context.Context, error) {
	t.Helper()
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	conn, err := (&net.Dialer{}).DialContext(canceled, "tcp", "192.0.2.1:443")
	if conn != nil {
		conn.Close()
	}
	var dial *net.OpError
	if !errors.As(err, &dial) || dial.Err == nil || dial.Err == context.Canceled || dial.Err.Error() != "operation was canceled" {
		t.Fatalf("canceled dial did not return net's own cancellation value: %v", err)
	}
	return canceled, &url.Error{Op: "Post", URL: "https://api.example/sn/attempt-artifact?kind=records", Err: err}
}

func releaseTransientTestRateLimit() *connect.HttpStatusError {
	return &connect.HttpStatusError{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests", Body: []byte(releaseTransientTestRateLimitBody)}
}

// 2026-11-13: the api's name did not resolve for two hours. Dial and lookup
// timeouts carry net's private deadline value, which implements Is, so every
// classifier refused it as an opaque matcher. Each steering attempt counted as
// hard, the tenth panicked, and each restart then panicked at its server-key
// read (101 restarts, native epochs 26312-26314 lost).
func TestStandardNetworkTimeoutsAreRetryableUpstreamReads(t *testing.T) {
	dial := &url.Error{Op: "Get", URL: "https://api.example/sn/attempt-artifact?kind=metadata", Err: releaseTransientTestDialTimeout(t)}
	cases := []struct {
		name string
		err  error
	}{
		{name: "dial timeout", err: dial},
		{name: "lookup timeout", err: releaseTransientTestLookupTimeout(t)},
		{name: "client timeout", err: releaseTransientTestClientTimeout(t)},
		{name: "wrapped lookup timeout", err: fmt.Errorf("compact settlement no_id 1 replay: %w", releaseTransientTestLookupTimeout(t))},
		{name: "joined timeouts", err: errors.Join(releaseTransientTestLookupTimeout(t), dial)},
	}
	for _, item := range cases {
		if !retryableProductionSteeringRead(item.err) {
			t.Errorf("%s: a startup or steering read was not retryable: %v", item.name, item.err)
		}
		if !RetryableEvidenceTransportError(item.err) || !transientReleaseSnapshotError(item.err) {
			t.Errorf("%s: evidence or startup retry refused it: %v", item.name, item.err)
		}
		observed := observeReleaseError(item.err)
		if original, ok := observed.(*releaseObservedOriginal); ok && original.hard {
			t.Errorf("%s: observation froze a hard cause: %v", item.name, observed)
		}
		if !retryableProductionSteeringRead(observed) {
			t.Errorf("%s: the observed cause was not retryable: %v", item.name, observed)
		}
		if !retryableProductionSteeringRead(observeProductionSteeringRead(item.err, false)) {
			t.Errorf("%s: the steering read capture was not retryable: %v", item.name, item.err)
		}
		if !retryableAttemptBindingReadError(&attemptBindingReadError{cause: item.err}) {
			t.Errorf("%s: a binding read refused it: %v", item.name, item.err)
		}
	}
	// A name that does not exist, and a timeout joined with a contradiction,
	// stay hard.
	missing := &url.Error{Op: "Get", URL: "https://api.example/verify/keys", Err: &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "api.example", IsNotFound: true}}}
	contradiction := errors.Join(releaseTransientTestLookupTimeout(t), errors.New("canonical commitment differs"))
	for _, failure := range []error{missing, contradiction} {
		if retryableProductionSteeringRead(failure) || retryableProductionSteeringRead(observeReleaseError(failure)) || transientReleaseSnapshotError(failure) {
			t.Errorf("a hard cause became retryable: %v", failure)
		}
	}
}

// Every restart of 2026-11-13 07:31-09:21Z panicked with "release V2
// server-key startup: ... lookup ...: i/o timeout": the production startup
// stage gave up on the timeout instead of waiting. It must wait it out, then
// finish the stage in the same process.
func TestProductionStartupStageWaitsOutLookupTimeouts(t *testing.T) {
	cfg := &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion, PollSeconds: 0}
	outage := releaseSteeringFailureLimit + 2
	attempts := 0
	err := awaitProductionStartupStage(t.Context(), cfg, nil, "server-key history", func(context.Context) error {
		attempts++
		if attempts <= outage {
			return releaseTransientTestLookupTimeout(t)
		}
		return nil
	})
	if err != nil || attempts != outage+1 {
		t.Fatalf("startup gave up on a lookup outage: attempts=%d error=%v", attempts, err)
	}
	hard := errors.New("synthetic server key differs from its pinned history")
	attempts = 0
	err = awaitProductionStartupStage(t.Context(), cfg, nil, "server-key history", func(context.Context) error {
		attempts++
		return errors.Join(releaseTransientTestLookupTimeout(t), hard)
	})
	if !errors.Is(err, hard) || attempts != 1 {
		t.Fatalf("startup retried a contradiction joined to a timeout: attempts=%d error=%v", attempts, err)
	}
}

// The steering side of the same outage: a preparation whose reads time out is
// a wait that spends no failure, however long the outage lasts, and the first
// attempt after it proceeds. A contradiction joined to the timeout still
// spends the budget.
func TestProductionSteeringLookupOutageSpendsNoFailures(t *testing.T) {
	progress, _ := newReleaseProgressTest(t)
	outage := releaseSteeringFailureLimit + 2
	attempts := 0
	err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
		attempts++
		if attempts <= outage {
			return productionPreparationFailure(t.Context(), fmt.Errorf("compact live head client key read for no_id 1: %w", releaseTransientTestLookupTimeout(t)))
		}
		return nil
	}, func() bool { return attempts <= outage }, progress)
	if err != nil || attempts != outage+1 || progress.value.Steering == nil || progress.value.Steering.Outcome != "complete" {
		t.Fatalf("a lookup outage restarted steering: attempts=%d error=%v", attempts, err)
	}
	hard := errors.New("synthetic canonical commitment differs")
	attempts = 0
	err = runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
		attempts++
		return productionPreparationFailure(t.Context(), errors.Join(releaseTransientTestLookupTimeout(t), hard))
	}, func() bool { return true }, progress)
	var limit *releaseSteeringFailureLimitError
	if !errors.As(err, &limit) || !errors.Is(err, hard) || attempts != releaseSteeringFailureLimit {
		t.Fatalf("a contradiction joined to a timeout stopped spending failures: attempts=%d error=%v", attempts, err)
	}
}

// 2026-11-15 16:52:48: the load balancer's per-address limit answered the
// validator's client-key reads with 429. Six batch reads (a typed client-key
// status) and four GET /key reads (connect's typed status) were counted as
// hard, and the tenth panicked the validator.
func TestRateLimitedClientKeyReadsAreRetryableUpstreamReads(t *testing.T) {
	batch := &clientKeyObservationHttpStatusError{status: http.StatusTooManyRequests, operation: "batch"}
	transient := []error{
		releaseTransientTestRateLimit(),
		fmt.Errorf("compact live head client key read for no_id 1 client %s: %w", connect.NewId(), releaseTransientTestRateLimit()),
		batch,
		fmt.Errorf("compact live head client census observation read: %w", batch),
		errors.Join(batch, releaseTransientTestRateLimit()),
		&clientKeyObservationHttpStatusError{status: http.StatusServiceUnavailable, operation: "history"},
	}
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooEarly, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		transient = append(transient, &connect.HttpStatusError{StatusCode: status, Status: http.StatusText(status)})
	}
	for _, failure := range transient {
		if !retryableProductionSteeringRead(failure) || !transientReleaseSnapshotError(failure) || !RetryableEvidenceTransportError(failure) {
			t.Errorf("a transient upstream status stayed hard: %v", failure)
		}
		if !retryableProductionSteeringRead(observeProductionSteeringRead(failure, false)) {
			t.Errorf("the steering read capture made it hard: %v", failure)
		}
	}
	hard := []error{
		&clientKeyObservationHttpStatusError{status: http.StatusUnauthorized, operation: "batch"},
		&clientKeyObservationHttpStatusError{status: http.StatusConflict, operation: "batch"},
		errors.Join(batch, errors.New("synthetic signed observation differs")),
	}
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusNotFound, http.StatusConflict} {
		hard = append(hard, &connect.HttpStatusError{StatusCode: status, Status: http.StatusText(status)})
	}
	for _, failure := range hard {
		if retryableProductionSteeringRead(failure) || transientReleaseSnapshotError(failure) {
			t.Errorf("a definite refusal became retryable: %v", failure)
		}
	}
	// The reader itself still never sends a second reservation after a 429.
	if retryableClientKeyObservationHttpError(batch) {
		t.Fatal("a 429 authorized the reader's immediate second reservation")
	}
}

// The crash of 2026-11-15 through the actual compact head collector and the
// production steering loop: twelve attempts in a row are refused with the load
// balancer's 429, alternately at GET /key and at the client-key batch. None
// spends a failure, and the first unrefused attempt completes the head.
func TestProductionSteeringRateLimitedHeadNeverRestartsTheValidator(t *testing.T) {
	fixture := newReleaseHeadV2TestFixture(t, 15)
	refused := releaseSteeringFailureLimit + 2
	attempts, batchRefusals, keyRefusals := 0, 0, 0
	for _, operator := range fixture.steerer.contexts {
		clientKey := operator.ClientKey
		operator.ClientKey = func(id connect.Id) ([32]byte, bool, error) {
			if attempts <= refused && attempts%2 == 1 {
				keyRefusals++
				return [32]byte{}, false, releaseTransientTestRateLimit()
			}
			return clientKey(id)
		}
		original := operator.ClientKeyHistory.client.Transport
		if original == nil {
			original = http.DefaultTransport
		}
		operator.ClientKeyHistory.client.Transport = clientKeyHistoryTestRoundTripper(func(request *http.Request) (*http.Response, error) {
			if attempts <= refused {
				batchRefusals++
				return &http.Response{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests", Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(releaseTransientTestRateLimitBody)), Request: request}, nil
			}
			return original.RoundTrip(request)
		})
	}
	progress, _ := newReleaseProgressTest(t)
	err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
		attempts++
		_, err := fixture.gather(t.Context(), fixture.options(t))
		return productionPreparationFailure(t.Context(), err)
	}, func() bool { return attempts <= refused }, progress)
	if err != nil || attempts != refused+1 || keyRefusals == 0 || batchRefusals == 0 {
		t.Fatalf("rate-limited head restarted the validator: attempts=%d key=%d batch=%d error=%v", attempts, keyRefusals, batchRefusals, err)
	}
	if progress.value.Steering == nil || progress.value.Steering.Outcome != "complete" || !progress.value.Steering.Current {
		t.Fatalf("the first unrefused head did not complete: %+v", progress.value.Steering)
	}
}
