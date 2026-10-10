//go:build linux || darwin

// Service availability does not change signed requests, admission reservations
// or the ownership of a response that fails while closing.
package validator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Both public readers retry a service refusal with identical authenticated
// request bytes, after closing the first body, and retain the real reply.
func TestReleaseClientKeyObservationServiceStatusRetryPreservesRequest(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusInternalServerError, http.StatusTooEarly} {
		for _, batch := range []bool{false, true} {
			fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
			reader := newReleaseHeadV2ClientKeyReader(t, &fixture.cfg, fixture.domain.NoID)
			requests := releaseClientKeyHistoryBatchV2Requests(fixture, 1)
			calls, closed := 0, 0
			var firstRequest []byte
			var firstAuthorization string
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
					firstRequest, firstAuthorization = bytes.Clone(encoded), request.Header.Get("Authorization")
					return &http.Response{StatusCode: status, Request: request, Body: &attemptStreamV2HTTPTestBody{
						read: bytes.NewReader(nil).Read, close: func() error { closed++; return nil },
					}}, nil
				}
				if calls != 2 || closed != 1 || request.Method != http.MethodPost || firstAuthorization == "" || request.Header.Get("Authorization") != firstAuthorization || !bytes.Equal(encoded, firstRequest) {
					return nil, errors.New("service retry changed its original request or body ownership")
				}
				response, err := http.DefaultTransport.RoundTrip(request)
				if err == nil {
					original := response.Body
					response.Body = &attemptStreamV2HTTPTestBody{read: original.Read, close: func() error { closed++; return original.Close() }}
				}
				return response, err
			})
			var encoded []byte
			var err error
			if batch {
				var result [][]byte
				result, err = reader.ReadBatch(t.Context(), requests, protocol.MaxClientKeyObservationBatchResponseBytes)
				if len(result) == 1 {
					encoded = result[0]
				}
			} else {
				encoded, err = reader.Read(t.Context(), requests[0], protocol.MaxClientKeyHistoryResponseBytes)
			}
			if err != nil || len(encoded) == 0 || calls != protocol.MaxClientKeyObservationReservationAttempts || closed != calls {
				t.Fatalf("status=%d batch=%t did not recover the original response: bytes=%d calls=%d closed=%d error=%v", status, batch, len(encoded), calls, closed, err)
			}
		}
	}
}

// A service retry and an explicit work fallback share the two reservations.
// Neither order, nor a persistent service refusal, can issue a third attempt.
func TestReleaseClientKeyObservationServiceRetryKeepsReservationCap(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusInternalServerError, http.StatusTooEarly} {
		for _, sequence := range []string{"persistent", "service-work", "work-service"} {
			fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
			reader := newReleaseHeadV2ClientKeyReader(t, &fixture.cfg, fixture.domain.NoID)
			requests := releaseClientKeyHistoryBatchV2Requests(fixture, 32)
			calls, closed := 0, 0
			reservations := map[[16]byte]int{}
			var firstRequest []byte
			reader.client.Transport = clientKeyHistoryTestRoundTripper(func(request *http.Request) (*http.Response, error) {
				if calls != closed {
					return nil, errors.New("service retry abandoned a response body")
				}
				calls++
				encoded, err := io.ReadAll(request.Body)
				if err := errors.Join(err, request.Body.Close()); err != nil {
					return nil, err
				}
				batch, err := protocol.DecodeClientKeyObservationBatchRequest(encoded)
				if err != nil {
					return nil, err
				}
				want := requests
				if sequence == "work-service" && calls == 2 {
					want = requests[:protocol.ClientKeyObservationFallbackBatchClients]
				}
				if !reflect.DeepEqual(batch.Requests, want) {
					return nil, errors.New("service retry or fallback changed original member requests")
				}
				if calls == 1 {
					firstRequest = bytes.Clone(encoded)
				} else if sequence != "work-service" && !bytes.Equal(firstRequest, encoded) {
					return nil, errors.New("service retry changed the original census")
				}
				for _, member := range batch.Requests {
					reservations[member.ClientID]++
					if reservations[member.ClientID] > protocol.MaxClientKeyObservationReservationAttempts {
						return nil, errors.New("service retry exceeded a member reservation")
					}
				}
				responseStatus := status
				header := http.Header{}
				if sequence == "work-service" && calls == 1 || sequence == "service-work" && calls == 2 {
					responseStatus = http.StatusRequestEntityTooLarge
					header.Set("X-Ur-Client-Key-Batch-Admission", "work")
				}
				return &http.Response{StatusCode: responseStatus, Header: header, Request: request, Body: &attemptStreamV2HTTPTestBody{
					read: bytes.NewReader(nil).Read, close: func() error { closed++; return nil },
				}}, nil
			})
			result, err := reader.ReadBatch(t.Context(), requests, protocol.MaxClientKeyObservationBatchResponseBytes)
			if result != nil || err == nil || calls != protocol.MaxClientKeyObservationReservationAttempts || closed != calls {
				t.Fatalf("status=%d sequence=%s changed the finite refusal: calls=%d closed=%d error=%v", status, sequence, calls, closed, err)
			}
			for index, request := range requests {
				want := protocol.MaxClientKeyObservationReservationAttempts
				if sequence == "work-service" && index >= protocol.ClientKeyObservationFallbackBatchClients {
					want = 1
				}
				if reservations[request.ClientID] != want {
					t.Fatalf("status=%d sequence=%s member=%d changed reservations: got=%d want=%d", status, sequence, index, reservations[request.ClientID], want)
				}
			}
		}
	}
}

// Quota, conflict and unmarked admission refusals stay hard. A service status
// cannot hide a hard close or owner cancellation in either public reader.
func TestReleaseClientKeyObservationServiceRetryPreservesHardFailures(t *testing.T) {
	t.Parallel()
	hardClose := errors.New("synthetic original close failed")
	for _, test := range []struct {
		name     string
		status   int
		closeErr error
		cancel   bool
	}{
		{name: "server-close", status: http.StatusInternalServerError, closeErr: hardClose},
		{name: "early-close", status: http.StatusTooEarly, closeErr: hardClose},
		{name: "server-cancel", status: http.StatusInternalServerError, cancel: true},
		{name: "early-cancel", status: http.StatusTooEarly, cancel: true},
		{name: "conflict", status: http.StatusConflict},
		{name: "quota", status: http.StatusTooManyRequests},
		{name: "admission", status: http.StatusRequestEntityTooLarge},
	} {
		for _, batch := range []bool{false, true} {
			fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
			reader := newReleaseHeadV2ClientKeyReader(t, &fixture.cfg, fixture.domain.NoID)
			requests := releaseClientKeyHistoryBatchV2Requests(fixture, 1)
			ctx, cancel := context.WithCancel(t.Context())
			calls, closed := 0, 0
			reader.client.Transport = clientKeyHistoryTestRoundTripper(func(request *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: test.status, Request: request, Body: &attemptStreamV2HTTPTestBody{
					read: bytes.NewReader(nil).Read, close: func() error {
						closed++
						if test.cancel {
							cancel()
							return context.Canceled
						}
						return test.closeErr
					},
				}}, nil
			})
			var err error
			var count int
			if batch {
				var result [][]byte
				result, err = reader.ReadBatch(ctx, requests, protocol.MaxClientKeyObservationBatchResponseBytes)
				count = len(result)
			} else {
				var result []byte
				result, err = reader.Read(ctx, requests[0], protocol.MaxClientKeyHistoryResponseBytes)
				count = len(result)
			}
			cancel()
			if err == nil || count != 0 || calls != 1 || closed != 1 || retryableClientKeyObservationHttpError(err) || transientReleaseSnapshotError(err) || test.closeErr != nil && !errors.Is(err, test.closeErr) || test.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("%s batch=%t lost its hard cause: count=%d calls=%d closed=%d error=%v", test.name, batch, count, calls, closed, err)
			}
		}
	}
}
