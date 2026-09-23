//go:build linux || darwin

// Real authenticated Http responses and independently checked durable captures
// prove retry ownership without sleeping to manufacture a timeout ordering.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Drop the first completed server response before the client sees authority.
// The exact request/nonce is retried, and recovery reads the accepted immutable
// slots even after the live authenticated session has ended.
func TestReleaseClientKeyObservationRetryPreservesRequestAndDurableCapture(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	fixture.cfg.StateDir = newReleaseHeadV2TestStateDir(t)
	reader := newReleaseHeadV2ClientKeyReader(t, &fixture.cfg, fixture.domain.NoID)
	requests := releaseClientKeyHistoryBatchV2Requests(fixture, 3)
	calls, closed := 0, 0
	var firstRequest []byte
	reader.client.Transport = clientKeyHistoryTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		body, err := request.GetBody()
		if err != nil {
			return nil, err
		}
		encoded, readErr := io.ReadAll(body)
		if err := errors.Join(readErr, body.Close()); err != nil {
			return nil, err
		}
		if calls == 1 {
			firstRequest = bytes.Clone(encoded)
		} else if !bytes.Equal(firstRequest, encoded) || closed != 1 {
			return nil, errors.New("retry changed its nonce/decision or outlived its first body")
		}
		response, err := http.DefaultTransport.RoundTrip(request)
		if err != nil {
			return nil, err
		}
		if calls == 1 {
			_, readErr := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			closed++
			return nil, errors.Join(readErr, closeErr, context.DeadlineExceeded)
		}
		return response, nil
	})
	ctx, owner := fixture.owner(t, t.Context(), protocol.MaxClientKeyObservationBatchControlBytes)
	first, err := captureReleaseClientKeysV2(ctx, fixture.chain, reader, fixture.cfg.StateDir, fixture.domain, requests, protocol.MaxClientKeyObservationBatchResponseBytes, 1024*1024*1024)
	if err := errors.Join(err, owner.finish(err)); err != nil || len(first) != len(requests) || calls != 2 {
		t.Fatalf("same-request retry did not finish the real capture: count=%d calls=%d error=%v", len(first), calls, err)
	}
	reader.byJwt = func() string { return "" }
	ctx, owner = fixture.owner(t, t.Context(), protocol.MaxClientKeyObservationBatchControlBytes)
	retained, err := captureReleaseClientKeysV2(ctx, fixture.chain, reader, fixture.cfg.StateDir, fixture.domain, requests, protocol.MaxClientKeyObservationBatchResponseBytes, 1024*1024*1024)
	if err := errors.Join(err, owner.finish(err)); err != nil || !reflect.DeepEqual(first, retained) || calls != 2 {
		t.Fatalf("recovery repeated live observation or replaced accepted bytes: calls=%d error=%v", calls, err)
	}
}

// Singleton captures share the same caller cancellation and exact nonce rules.
func TestReleaseClientKeyObservationSingletonRetriesTransportOnce(t *testing.T) {
	t.Parallel()
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	reader := newReleaseHeadV2ClientKeyReader(t, &fixture.cfg, fixture.domain.NoID)
	request := releaseClientKeyHistoryBatchV2Requests(fixture, 1)[0]
	calls := 0
	reader.client.Transport = clientKeyHistoryTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, io.ErrUnexpectedEOF
		}
		return http.DefaultTransport.RoundTrip(request)
	})
	encoded, err := reader.Read(t.Context(), request, protocol.MaxClientKeyHistoryResponseBytes)
	if err != nil || len(encoded) == 0 || calls != 2 {
		t.Fatalf("singleton transport retry failed: bytes=%d calls=%d error=%v", len(encoded), calls, err)
	}
}

// Work fallback and transient retry consume the same finite reservation. A
// lost response can never turn one admitted batch into an unbounded loop.
func TestReleaseClientKeyObservationRetryCannotMultiplyAdmissionFallback(t *testing.T) {
	t.Parallel()
	for _, firstTimeout := range []bool{false, true} {
		fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
		reader := newReleaseHeadV2ClientKeyReader(t, &fixture.cfg, fixture.domain.NoID)
		requests := releaseClientKeyHistoryBatchV2Requests(fixture, 32)
		calls := 0
		reader.client.Transport = clientKeyHistoryTestRoundTripper(func(request *http.Request) (*http.Response, error) {
			calls++
			if (calls == 1) == firstTimeout {
				return nil, context.DeadlineExceeded
			}
			return &http.Response{StatusCode: http.StatusRequestEntityTooLarge, Header: http.Header{"X-Ur-Client-Key-Batch-Admission": []string{"work"}}, Body: io.NopCloser(strings.NewReader("admission refused")), Request: request}, nil
		})
		result, err := reader.ReadBatch(t.Context(), requests, protocol.MaxClientKeyObservationBatchResponseBytes)
		if err == nil || result != nil || calls != protocol.MaxClientKeyObservationReservationAttempts {
			t.Fatalf("timeout-first=%t expanded the quota: calls=%d result=%d err=%v", firstTimeout, calls, len(result), err)
		}
	}
}

// A hard leaf or caller cancellation cannot be hidden by a joined timeout.
func TestReleaseClientKeyObservationRetryRejectsMixedAndCanceledFailures(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{
		context.DeadlineExceeded, io.EOF, io.ErrUnexpectedEOF,
		fmt.Errorf("read timed out: %w", context.DeadlineExceeded),
		&clientKeyObservationHttpStatusError{status: http.StatusServiceUnavailable, operation: "batch"},
	} {
		if !retryableClientKeyObservationHttpError(failure) || !transientReleaseSnapshotError(failure) {
			t.Errorf("transient observation became hard: %v", failure)
		}
	}
	for _, failure := range []error{
		nil, context.Canceled, errors.Join(context.DeadlineExceeded, errors.New("wrong signer")), errors.Join(context.DeadlineExceeded, io.ErrShortWrite),
		&clientKeyObservationHttpStatusError{status: http.StatusUnauthorized, operation: "batch"},
		&clientKeyObservationHttpStatusError{status: http.StatusTooManyRequests, operation: "batch"},
	} {
		if retryableClientKeyObservationHttpError(failure) || transientReleaseSnapshotError(failure) {
			t.Errorf("hard observation became retryable: %v", failure)
		}
	}
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	reader := newReleaseHeadV2ClientKeyReader(t, &fixture.cfg, fixture.domain.NoID)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	reader.client.Transport = clientKeyHistoryTestRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		cancel()
		return nil, context.DeadlineExceeded
	})
	result, err := reader.ReadBatch(ctx, releaseClientKeyHistoryBatchV2Requests(fixture, 1), protocol.MaxClientKeyObservationBatchResponseBytes)
	if !errors.Is(err, context.Canceled) || result != nil || calls != 1 {
		t.Fatalf("caller cancellation retried: calls=%d error=%v", calls, err)
	}
}

// Exercise the actual compact head collector: an unavailable observation has
// no completed signer verdict. Exhausted transport retry returns to steering,
// and the same in-process collector can subsequently complete the live head.
func TestReleaseClientKeyObservationHeadTimeoutReturnsToSteeringRetry(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 1)
	failed := false
	for _, operator := range fixture.steerer.contexts {
		operator.ClientKeyHistory.client.Transport = clientKeyHistoryTestRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		})
	}
	attempts := 0
	err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) { return uint64(41 + attempts), nil }, func() error {
		attempts++
		_, err := fixture.gather(t.Context(), fixture.options(t))
		if attempts == 1 {
			failed = errors.Is(err, context.DeadlineExceeded) && transientReleaseSnapshotError(err)
			for _, operator := range fixture.steerer.contexts {
				operator.ClientKeyHistory.client.Transport = http.DefaultTransport
			}
		}
		return err
	}, func() bool { return attempts < 2 }, true)
	if err != nil || !failed || attempts != 2 {
		t.Fatalf("head observation timeout consumed a process restart: attempts=%d first-retryable=%t error=%v", attempts, failed, err)
	}
}
