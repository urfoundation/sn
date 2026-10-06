//go:build linux || darwin

// Virtual time exercises the unchanged request and operation deadlines. Real
// clients, response reads and closes retain every original transport cause.
package validator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Every transient response closes before the same original get or idempotent
// lookup is retried. Physical read and close eof retain transport provenance.
func TestProviderAttemptHttpRetriesTransientResponsesWithOriginalInput(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		synctest.Test(t, func(t *testing.T) {
			endpoint := "https://provider-attempt.example/original/7/11"
			requestBody := []byte(nil)
			if method == http.MethodPost {
				requestBody = []byte(`{"original":"synthetic-request"}`)
			}
			statuses := []int{408, 425, 429, 500, 502, 503, 504, 599}
			responseBody := []byte(`{"original":"synthetic-response"}`)
			started := time.Now()
			calls, closed := 0, 0
			client := &http.Client{Transport: attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
				if calls != closed {
					t.Fatal("retry began before the original response closed")
				}
				calls++
				actualBody, err := io.ReadAll(request.Body)
				if err != nil || request.Method != method || request.URL.String() != endpoint || !bytes.Equal(actualBody, requestBody) {
					t.Fatalf("retry changed the original request: %v", err)
				}
				deadline, bounded := request.Context().Deadline()
				if !bounded || deadline.Sub(time.Now()) != 60*time.Second || deadline.After(started.Add(300*time.Second)) {
					t.Fatal("request changed its sixty-second or total allowance")
				}
				status := http.StatusOK
				body := &attemptStreamV2HTTPTestBody{read: bytes.NewReader(responseBody).Read, close: func() error { closed++; return nil }}
				switch {
				case calls <= len(statuses):
					status = statuses[calls-1]
				case calls == len(statuses)+1:
					body.read = func([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
				case calls == len(statuses)+2:
					body.close = func() error { closed++; return io.EOF }
				}
				return &http.Response{StatusCode: status, Body: body}, nil
			})}
			raw, err := providerAttemptHttpWithClient(t.Context(), endpoint, method, requestBody, 1024, client)
			if err != nil || !bytes.Equal(raw, responseBody) || calls != len(statuses)+3 || closed != calls || time.Since(started) != time.Duration(calls-1)*time.Second {
				t.Fatalf("%s original read did not recover: calls=%d closed=%d elapsed=%s error=%v", method, calls, closed, time.Since(started), err)
			}
		})
	}
}

// Fast service failures consume the complete original five-minute budget,
// retaining the final typed status without issuing a request after expiry.
func TestProviderAttemptHttpRetriesForCompleteBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		calls, closed := 0, 0
		client := &http.Client{Transport: attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
			if time.Since(started) >= 300*time.Second || calls != closed || request.Context().Err() != nil {
				t.Fatal("request escaped its original live owner")
			}
			calls++
			return &http.Response{StatusCode: http.StatusInternalServerError, Body: &attemptStreamV2HTTPTestBody{
				read: bytes.NewReader(nil).Read, close: func() error { closed++; return nil },
			}}, nil
		})}
		raw, err := providerAttemptHttpWithClient(t.Context(), "https://provider-budget.example/window", http.MethodGet, nil, 1024, client)
		var status *releaseHttpGetStatusError
		if raw != nil || time.Since(started) != 300*time.Second || calls <= 60 || calls != closed || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &status) || status.status != 500 || !RetryableEvidenceTransportError(err) || errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
			t.Fatalf("read lost its full budget or transport cause: calls=%d closed=%d elapsed=%s error=%v", calls, closed, time.Since(started), err)
		}
	})
}

// An earlier caller deadline remains the original operation boundary even if
// its timer and the retry timer become runnable at the same instant.
func TestProviderAttemptHttpKeepsEarlierParentDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()
		calls, closed := 0, 0
		client := &http.Client{Transport: attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
			deadline, bounded := request.Context().Deadline()
			if !bounded || deadline != started.Add(60*time.Second) || !time.Now().Before(deadline) || calls != closed || request.Context().Err() != nil {
				t.Fatal("request escaped its original earlier parent deadline")
			}
			calls++
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: &attemptStreamV2HTTPTestBody{
				read: bytes.NewReader(nil).Read, close: func() error { closed++; return nil },
			}}, nil
		})}
		raw, err := providerAttemptHttpWithClient(ctx, "https://provider-parent-deadline.example/window", http.MethodGet, nil, 1024, client)
		var status *releaseHttpGetStatusError
		if raw != nil || time.Since(started) != 60*time.Second || calls != 60 || closed != calls || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &status) || status.status != http.StatusServiceUnavailable || !RetryableEvidenceTransportError(err) || errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
			t.Fatalf("read changed its original parent budget or final cause: calls=%d closed=%d elapsed=%s error=%v", calls, closed, time.Since(started), err)
		}
	})
}

// A complete conflict, permanent network failure or independent read/close
// defect cannot borrow retry authority from another transient cause or status.
func TestProviderAttemptHttpRejectsHardAndMixedFailures(t *testing.T) {
	broken := errors.New("synthetic independent response failure")
	for _, test := range []struct {
		name     string
		status   int
		readErr  error
		closeErr error
	}{
		{name: "conflict", status: 409},
		{name: "authentication", status: 401},
		{name: "service-close", status: 503, closeErr: broken},
		{name: "service-read", status: 503, readErr: broken},
		{name: "read-close", status: 200, readErr: io.ErrUnexpectedEOF, closeErr: broken},
		{name: "timeout-close", status: 200, readErr: context.DeadlineExceeded, closeErr: broken},
		{name: "permanent-network", status: 200, readErr: &net.DNSError{Err: "synthetic nonexistent host", Name: "missing-provider.example", IsNotFound: true}},
	} {
		synctest.Test(t, func(t *testing.T) {
			started := time.Now()
			calls, closed := 0, 0
			client := &http.Client{Transport: attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
				calls++
				body := &attemptStreamV2HTTPTestBody{read: bytes.NewReader([]byte("synthetic body")).Read, close: func() error { closed++; return test.closeErr }}
				if test.readErr != nil {
					body.read = func([]byte) (int, error) { return 0, test.readErr }
				}
				return &http.Response{StatusCode: test.status, Body: body}, nil
			})}
			raw, err := providerAttemptHttpWithClient(t.Context(), "https://provider-hard.example/window", http.MethodGet, nil, 1024, client)
			if raw != nil || !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) || RetryableEvidenceTransportError(err) || calls != 1 || closed != 1 || time.Since(started) != 0 || test.readErr != nil && !errors.Is(err, test.readErr) || test.closeErr != nil && !errors.Is(err, test.closeErr) {
				t.Fatalf("%s was retried or lost its cause: calls=%d closed=%d error=%v", test.name, calls, closed, err)
			}
		})
	}
}

// Cancellation stops new requests without inventing integrity. A returned
// conflict remains explicit even when cancellation or the deadline also fires.
func TestProviderAttemptHttpCancellationKeepsCompletedConflict(t *testing.T) {
	for _, scenario := range []string{"before", "service", "conflict", "deadline-conflict"} {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "deadline-conflict" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 60*time.Second)
				defer stop()
			}
			if scenario == "before" {
				cancel()
			}
			calls, closed := 0, 0
			client := &http.Client{Transport: attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				status := http.StatusConflict
				if scenario == "service" {
					status = http.StatusServiceUnavailable
				}
				if scenario == "deadline-conflict" {
					<-request.Context().Done()
				} else {
					cancel()
				}
				return &http.Response{StatusCode: status, Body: &attemptStreamV2HTTPTestBody{
					read: bytes.NewReader(nil).Read, close: func() error { closed++; return nil },
				}}, nil
			})}
			raw, err := providerAttemptHttpWithClient(ctx, "https://provider-cancel.example/window", http.MethodGet, nil, 1024, client)
			wantCalls, wantCause := 1, error(context.Canceled)
			if scenario == "before" {
				wantCalls = 0
			} else if scenario == "deadline-conflict" {
				wantCause = context.DeadlineExceeded
			}
			wantConflict := scenario == "conflict" || scenario == "deadline-conflict"
			if raw != nil || !errors.Is(err, wantCause) || errors.Is(err, protocol.ErrProviderAttemptsIntegrity) != wantConflict || calls != wantCalls || closed != wantCalls {
				t.Fatalf("%s changed cancellation or completed conflict: calls=%d closed=%d error=%v", scenario, calls, closed, err)
			}
			if wantConflict {
				var status *releaseHttpGetStatusError
				if !errors.As(err, &status) || status.status != http.StatusConflict {
					t.Fatalf("completed conflict disappeared behind cancellation: %v", err)
				}
			}
		})
	}
}

// A complete attempt timeout is recoverable inside the original operation;
// the next request starts only after the prior request context has ended.
func TestProviderAttemptHttpRecoversAfterAttemptDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		calls, closed := 0, 0
		var expired context.Context
		client := &http.Client{Transport: attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				expired = request.Context()
				<-expired.Done()
				return nil, expired.Err()
			}
			if expired.Err() != context.DeadlineExceeded || time.Since(started) != 61*time.Second {
				t.Fatal("retry changed the full request allowance or began before expiry")
			}
			return &http.Response{StatusCode: http.StatusOK, Body: &attemptStreamV2HTTPTestBody{
				read: bytes.NewReader([]byte("original")).Read, close: func() error { closed++; return nil },
			}}, nil
		})}
		raw, err := providerAttemptHttpWithClient(t.Context(), "https://provider-attempt-deadline.example/window", http.MethodGet, nil, 1024, client)
		if err != nil || string(raw) != "original" || calls != 2 || closed != 1 || time.Since(started) != 61*time.Second {
			t.Fatalf("full attempt timeout did not recover: calls=%d closed=%d elapsed=%s error=%v", calls, closed, time.Since(started), err)
		}
	})
}
