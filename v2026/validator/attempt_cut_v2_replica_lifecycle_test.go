//go:build linux || darwin

package validator

// Response and test-worker ownership is observed at explicit callback/read/close
// barriers. A completed join never stands in for successful work, and every
// test-owned publisher goroutine is canceled and joined on early assertion exits.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Shared by the original concurrency test and its real blocked-worker control.
// Idempotent cancellation and a closed completion channel make repeated joins safe.
func joinAttemptCutV2ReplicaTestWorker(cancel context.CancelFunc, joined <-chan struct{}) {
	cancel()
	<-joined
}

// Goexit inside an owned body Read must close the response and fail the worker,
// for both the buffered metadata and streamed records/proofs paths.
func TestAttemptCutV2ReplicaReadGoexitClosesResponseAndFails(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"metadata", AttemptStreamV2Records, AttemptStreamV2Proofs} {
		replicas, _ := newAttemptCutV2ReplicaTestStores(t)
		publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas)
		if err != nil {
			t.Fatal(err)
		}
		raw := []byte("{}\n")
		var reads, closes atomic.Int32
		publisher.readers[1].client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
			contentType := "application/x-ndjson"
			if kind == "metadata" {
				contentType = "application/json"
			}
			body := &attemptStreamV2HTTPTestBody{
				read: func([]byte) (int, error) {
					reads.Add(1)
					runtime.Goexit()
					return 0, nil
				},
				close: func() error { closes.Add(1); return nil },
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, ContentLength: int64(len(raw)), Body: body}, nil
		})
		err = publisher.writer(kind)(t.Context(), attemptHex32(sha256.Sum256(raw)), raw)
		if err == nil || !strings.Contains(err.Error(), "worker did not complete publication") || reads.Load() != 1 || closes.Load() != 1 {
			t.Fatalf("%s read exit lost ownership/failure: reads=%d closes=%d error=%v", kind, reads.Load(), closes.Load(), err)
		}
	}
}

// A normal EOF does not turn a nonreturning Close into successful verification.
func TestAttemptCutV2ReplicaCloseGoexitCannotAcknowledgeResponse(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"metadata", AttemptStreamV2Records, AttemptStreamV2Proofs} {
		replicas, _ := newAttemptCutV2ReplicaTestStores(t)
		publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas)
		if err != nil {
			t.Fatal(err)
		}
		raw := []byte("{}\n")
		var closes atomic.Int32
		publisher.readers[1].client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
			contentType := "application/x-ndjson"
			if kind == "metadata" {
				contentType = "application/json"
			}
			reader := bytes.NewReader(raw)
			body := &attemptStreamV2HTTPTestBody{
				read: reader.Read,
				close: func() error {
					closes.Add(1)
					runtime.Goexit()
					return nil
				},
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, ContentLength: int64(len(raw)), Body: body}, nil
		})
		err = publisher.writer(kind)(t.Context(), attemptHex32(sha256.Sum256(raw)), raw)
		if err == nil || !strings.Contains(err.Error(), "worker did not complete publication") || closes.Load() != 1 {
			t.Fatalf("%s close exit was acknowledged: closes=%d error=%v", kind, closes.Load(), err)
		}
	}
}

// A real HTTP response supplies a flushed prefix and remains open. The sibling
// fails only after the client has entered that actual response body's Read;
// cancellation must close the owned body and join both publication workers.
func TestAttemptCutV2ReplicaSiblingFailureJoinsRealHTTPResponse(t *testing.T) {
	t.Parallel()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	bodyEntered, handlerJoined := make(chan struct{}), make(chan struct{})
	raw := []byte("{\"real_response\":true}\n")
	hash := attemptHex32(sha256.Sum256(raw))
	var requests, closes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer close(handlerJoined)
		requests.Add(1)
		if request.Method != http.MethodGet || request.URL.Path != "/sn/attempt-artifact" || request.URL.Query().Get("kind") != AttemptStreamV2Proofs || request.URL.Query().Get("hash") != hash {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		objects, _, _ := stores[0].snapshot()
		if !bytes.Equal(objects[AttemptStreamV2Proofs+"/"+hash], raw) {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		response.Header().Set("Content-Type", "application/x-ndjson")
		response.Header().Set("Content-Length", strconv.Itoa(len(raw)))
		if _, err := response.Write(raw[:1]); err != nil {
			return
		}
		if err := http.NewResponseController(response).Flush(); err != nil {
			return
		}
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	replicas[0].Origin = server.URL
	injected := errors.New("sibling failure after real response ownership")
	stores[1].beforePut = func(ctx context.Context, _, _ string, _ []byte) error {
		select {
		case <-bodyEntered:
			return injected
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas)
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(transport.CloseIdleConnections)
	publisher.readers[0].client.Transport = attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(request)
		if err != nil {
			return response, err
		}
		body := response.Body
		var entered sync.Once
		response.Body = &attemptStreamV2HTTPTestBody{
			read: func(value []byte) (int, error) {
				entered.Do(func() { close(bodyEntered) })
				return body.Read(value)
			},
			close: func() error {
				closes.Add(1)
				return body.Close()
			},
		}
		return response, nil
	})
	err = publisher.writer(AttemptStreamV2Proofs)(ctx, hash, raw)
	if !errors.Is(err, injected) || requests.Load() != 1 || closes.Load() != 1 {
		t.Fatalf("real response was not canceled/closed before publisher join: requests=%d closes=%d error=%v", requests.Load(), closes.Load(), err)
	}
	// Join the test-owned server handler as well as the production workers.
	select {
	case <-handlerJoined:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

// Both callbacks enter before returning their distinct errors. One sibling's
// cancellation must not erase the other's actual returned storage failure.
func TestAttemptCutV2ReplicaPreservesBothJoinedWriterErrors(t *testing.T) {
	t.Parallel()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	failures := [2]error{errors.New("first joined publisher error"), errors.New("second joined publisher error")}
	var entered, exited atomic.Int32
	bothEntered := make(chan struct{})
	for index, store := range stores {
		failure := failures[index]
		store.beforePut = func(ctx context.Context, _, _ string, _ []byte) error {
			defer exited.Add(1)
			if entered.Add(1) == 2 {
				close(bothEntered)
			}
			select {
			case <-bothEntered:
				return failure
			case <-ctx.Done():
				return failure
			}
		}
	}
	publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("{}\n")
	err = publisher.writer("metadata")(t.Context(), attemptHex32(sha256.Sum256(raw)), raw)
	if !errors.Is(err, failures[0]) || !errors.Is(err, failures[1]) || entered.Load() != 2 || exited.Load() != 2 {
		t.Fatalf("one joined error disappeared: entered=%d exited=%d error=%v", entered.Load(), exited.Load(), err)
	}
}

// The same cleanup used by the original test releases two real blocked
// callbacks and waits for the owning publisher goroutine before returning.
func TestAttemptCutV2ReplicaTestCleanupCancelsAndJoinsPublishers(t *testing.T) {
	t.Parallel()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{}, 2)
	var exited atomic.Int32
	for _, store := range stores {
		store.beforePut = func(ctx context.Context, _, _ string, _ []byte) error {
			defer exited.Add(1)
			entered <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		}
	}
	publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas)
	if err != nil {
		t.Fatal(err)
	}
	result, joined := make(chan error, 1), make(chan struct{})
	raw := []byte("{}\n")
	go func() {
		defer close(joined)
		result <- publisher.writer("metadata")(ctx, attemptHex32(sha256.Sum256(raw)), raw)
	}()
	defer joinAttemptCutV2ReplicaTestWorker(cancel, joined)
	for range 2 {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	joinAttemptCutV2ReplicaTestWorker(cancel, joined)
	if err := <-result; !errors.Is(err, context.Canceled) || exited.Load() != 2 {
		t.Fatalf("test cleanup returned before callback/owner join: exited=%d error=%v", exited.Load(), err)
	}
}
