//go:build linux || darwin

// An elapsed deadline with cancellation still queued is driven explicitly;
// no timer scheduling, sleeps or trusted verifier outputs supply the result.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// The real cancel owner stays live while the test models its queued timer.
type evidenceReadPendingDeadlineTestContext struct {
	context.Context
	deadline time.Time
}

// Advancing only the deadline forces the interval before Err is published.
func (self *evidenceReadPendingDeadlineTestContext) Deadline() (time.Time, bool) {
	return self.deadline, true
}

// Before admission, after pacing and after a response, the original deadline
// wins without erasing a completed service or integrity cause.
func TestReleaseHttpGetRefusesElapsedDeadlineBeforeCancellation(t *testing.T) {
	for _, scenario := range []string{"before", "retry", "success", "conflict"} {
		parent, cancel := context.WithCancel(t.Context())
		owner := &evidenceReadPendingDeadlineTestContext{Context: parent, deadline: time.Now().Add(time.Minute)}
		if scenario == "before" {
			owner.deadline = time.Now()
		}
		calls, waits := 0, 0
		status := &releaseHttpGetStatusError{endpoint: "https://pending-deadline.example/original", status: http.StatusServiceUnavailable}
		if scenario == "conflict" {
			status.status = http.StatusConflict
		}
		err := retryReleaseHttpGet(owner, func(context.Context) error {
			calls++
			if calls > 1 {
				t.Fatal("elapsed operation acquired another request")
			}
			if scenario != "retry" {
				owner.deadline = time.Now()
			}
			if scenario == "success" {
				return nil
			}
			return status
		}, releaseHttpGetRetryHooks{wait: func(context.Context, time.Duration) error {
			waits++
			owner.deadline = time.Now()
			return nil
		}})
		wantCalls, wantWaits := 1, 0
		if scenario == "before" {
			wantCalls = 0
		} else if scenario == "retry" {
			wantWaits = 1
		}
		pending := owner.Err() == nil
		cancel()
		if !pending || calls != wantCalls || waits != wantWaits || !errors.Is(err, context.DeadlineExceeded) || (scenario == "retry" || scenario == "conflict") && !errors.Is(err, status) {
			t.Fatalf("%s lost the elapsed original deadline or cause: calls=%d waits=%d pending=%t error=%v", scenario, calls, waits, pending, err)
		}
	}
}

// The public wallet reader never retries or publishes after the same pending
// deadline; original signed history and physical close failures remain real.
func TestWalletMappingHttpRefusesElapsedDeadlineBeforeCancellation(t *testing.T) {
	original, expected := walletMappingHttpFixture(t)
	body, err := json.Marshal(struct {
		Originals []protocol.WalletMappingConsent `json:"originals"`
	}{Originals: []protocol.WalletMappingConsent{original}})
	if err != nil {
		t.Fatal(err)
	}
	broken := errors.New("synthetic independent wallet close failure")
	for _, scenario := range []string{"before", "retry", "success", "conflict", "close"} {
		parent, cancel := context.WithCancel(t.Context())
		owner := &evidenceReadPendingDeadlineTestContext{Context: parent, deadline: time.Now().Add(time.Minute)}
		if scenario == "before" {
			owner.deadline = time.Now()
		}
		calls, closed, waits := 0, 0, 0
		reader, err := NewHttpWalletMappingReader("https://pending-wallet-deadline.example")
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		reader.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
			calls++
			if calls > 1 {
				t.Fatal("elapsed wallet owner acquired another request")
			}
			status := http.StatusOK
			if scenario == "retry" {
				status = http.StatusServiceUnavailable
			} else if scenario == "conflict" {
				status = http.StatusConflict
			}
			return &http.Response{StatusCode: status, Body: &attemptStreamV2HTTPTestBody{read: bytes.NewReader(body).Read, close: func() error {
				closed++
				if scenario != "retry" {
					owner.deadline = time.Now()
				}
				if scenario == "close" {
					return broken
				}
				return nil
			}}}, nil
		})
		reader.wait = func(context.Context, time.Duration) error {
			waits++
			owner.deadline = time.Now()
			return nil
		}
		originals, value, err := reader.Read(owner, expected)
		wantCalls, wantWaits := 1, 0
		if scenario == "before" {
			wantCalls = 0
		} else if scenario == "retry" {
			wantWaits = 1
		}
		pending := owner.Err() == nil
		cancel()
		reader.CloseIdleConnections()
		if !pending || originals != nil || value != nil || calls != wantCalls || closed != calls || waits != wantWaits || !errors.Is(err, context.DeadlineExceeded) || scenario == "close" && !errors.Is(err, broken) {
			t.Fatalf("%s lost the wallet deadline, body ownership or cause: calls=%d closed=%d waits=%d pending=%t error=%v", scenario, calls, closed, waits, pending, err)
		}
		if scenario == "retry" || scenario == "conflict" {
			var status *clientKeyObservationHttpStatusError
			wantStatus := http.StatusServiceUnavailable
			if scenario == "conflict" {
				wantStatus = http.StatusConflict
			}
			if !errors.As(err, &status) || status.status != wantStatus {
				t.Fatalf("%s lost its completed original status: %v", scenario, err)
			}
		}
	}
}
