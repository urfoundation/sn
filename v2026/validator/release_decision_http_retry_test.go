//go:build linux || darwin

// Committed-content recovery keeps real sockets, signed bytes, body custody and
// immutable selectors. Only completed-attempt retry time advances logically.
package validator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// Preserve the real transport and attach faults only to physical body methods.
type committedHttpTestTransport func(*http.Request) (*http.Response, error)

// Every request still reaches the actual local HTTP listener.
func (self committedHttpTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

// Preserve the real transport's connection-discard method across instrumentation.
type committedHttpTestConnectionOwner struct {
	http.RoundTripper
	discard func()
}

// The callback closes actual idle sockets and observes the owner boundary.
func (self *committedHttpTestConnectionOwner) CloseIdleConnections() {
	self.discard()
}

// One response owns its close census and optional physical read/close failure.
type committedHttpTestBody struct {
	io.ReadCloser
	closes    *atomic.Int32
	closeErr  error
	readErr   error
	readAfter int
	readBytes int
}

// The optional failure follows the genuine bytes that cross the exact bound.
func (self *committedHttpTestBody) Read(buffer []byte) (int, error) {
	n, err := self.ReadCloser.Read(buffer)
	self.readBytes += n
	if self.readErr != nil && self.readBytes >= self.readAfter {
		err = errors.Join(err, self.readErr)
	}
	return n, err
}

// Close the real body before reporting either its census or the injected cause.
func (self *committedHttpTestBody) Close() error {
	err := self.ReadCloser.Close()
	self.closes.Add(1)
	return errors.Join(err, self.closeErr)
}

// A status outage, a truncated transfer and failed close all recover using the
// same independently committed hash after 150 logical seconds.
func TestCommittedArtifactHttpReadRecoversWithinOriginalBudget(t *testing.T) {
	artifact, encoded := validatorTestArtifact(t)
	hash, err := parseReleaseContentHash(artifact.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	var calls, opened, closed, discarded atomic.Int32
	var failedConnection atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/sn/artifact" || request.URL.Query().Get("hash") != artifact.ContentHash || len(request.URL.Query()) != 1 {
			http.Error(writer, "synthetic committed selector differs", http.StatusBadRequest)
			return
		}
		call := calls.Add(1)
		if call == 3 {
			failedConnection.Store(request.RemoteAddr)
		} else if call == 4 && failedConnection.Load() == request.RemoteAddr {
			t.Error("GET reused the connection whose body close failed")
		}
		if call == 1 {
			http.Error(writer, "synthetic committed-content outage", http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Content-Length", fmt.Sprint(len(encoded)))
		if call == 2 {
			_, _ = writer.Write(encoded[:len(encoded)/2])
			return
		}
		_, _ = writer.Write(encoded)
	}))
	defer server.Close()
	reader, err := NewHTTPArtifactReader(server.URL, artifact.DeploymentID, artifact.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	reader.client.Transport = &committedHttpTestConnectionOwner{RoundTripper: committedHttpTestTransport(func(request *http.Request) (*http.Response, error) {
		if opened.Load() != closed.Load() {
			t.Error("committed GET started before prior body closed")
		}
		if opened.Load() == 3 && discarded.Load() != 1 {
			t.Error("GET retried before discarding the failed-close connection")
		}
		if deadline, ok := request.Context().Deadline(); !ok || time.Until(deadline) > time.Minute || reader.client.Timeout != time.Minute {
			t.Error("committed GET lost its sixty-second attempt allowance")
		}
		response, err := transport.RoundTrip(request)
		if err == nil {
			var closeErr error
			if opened.Add(1) == 3 {
				closeErr = io.EOF
			}
			response.Body = &committedHttpTestBody{ReadCloser: response.Body, closes: &closed, closeErr: closeErr}
		}
		return response, err
	}), discard: func() {
		discarded.Add(1)
		transport.CloseIdleConnections()
	}}
	elapsed := time.Duration(0)
	hooks := releaseHttpGetTestDeadlineHooks(t, &elapsed, 50*time.Second)
	owners := 0
	reader.retryHooks = releaseHttpGetRetryHooks{
		withTimeout: func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			owners++
			return hooks.withTimeout(ctx, duration)
		},
		wait: func(ctx context.Context, delay time.Duration) error {
			if opened.Load() != closed.Load() {
				t.Error("retry waited while retaining a response body")
			}
			return hooks.wait(ctx, delay)
		},
	}
	actual, err := reader.readCommittedReleaseDecisionV2Artifact(t.Context(), hash, uint64(len(encoded)))
	if err != nil || actual == nil || actual.ContentHash != artifact.ContentHash || actual.TotalUsageBytes != artifact.TotalUsageBytes || calls.Load() != 4 || opened.Load() != 4 || closed.Load() != 4 || discarded.Load() != 1 || owners != 1 || elapsed != 150*time.Second {
		t.Fatalf("committed GET recovery changed ownership: calls=%d opened=%d closed=%d discarded=%d owners=%d elapsed=%s err=%v", calls.Load(), opened.Load(), closed.Load(), discarded.Load(), owners, elapsed, err)
	}
}

// The original 300-second owner ends a persistent real HTTP outage and keeps
// the response status available without publishing an artifact.
func TestCommittedArtifactHttpReadExpiresAtOriginalDeadline(t *testing.T) {
	artifact, encoded := validatorTestArtifact(t)
	hash, err := parseReleaseContentHash(artifact.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	var calls, closed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		http.Error(writer, "synthetic continuing outage", http.StatusGatewayTimeout)
	}))
	defer server.Close()
	reader, err := NewHTTPArtifactReader(server.URL, artifact.DeploymentID, artifact.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	reader.client.Transport = committedHttpTestTransport(func(request *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(request)
		if err == nil {
			response.Body = &committedHttpTestBody{ReadCloser: response.Body, closes: &closed}
		}
		return response, err
	})
	elapsed := time.Duration(0)
	hooks := releaseHttpGetTestDeadlineHooks(t, &elapsed, 75*time.Second)
	owners := 0
	reader.retryHooks = releaseHttpGetRetryHooks{
		withTimeout: func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			owners++
			return hooks.withTimeout(ctx, duration)
		},
		wait: hooks.wait,
	}
	actual, err := reader.readCommittedReleaseDecisionV2Artifact(t.Context(), hash, uint64(len(encoded)))
	var status *releaseHttpGetStatusError
	if actual != nil || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &status) || status.status != http.StatusGatewayTimeout || owners != 1 || calls.Load() != 4 || closed.Load() != 4 || elapsed != 300*time.Second {
		t.Fatalf("committed GET renewed or lost its deadline: calls=%d closed=%d owners=%d elapsed=%s err=%v", calls.Load(), closed.Load(), owners, elapsed, err)
	}
}

// Positive complete content cannot hide hard framing, digest, grammar or
// filesystem causes; a physical timeout/EOF sibling never softens them.
func TestCommittedArtifactHttpReadRejectsHardAndMixedCauses(t *testing.T) {
	artifact, encoded := validatorTestArtifact(t)
	originalHash, err := parseReleaseContentHash(artifact.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	hard := errors.New("synthetic committed body integrity refusal")
	for _, refusal := range []string{"status", "redirect", "media", "grammar", "digest", "length", "overflow-read", "path-close", "mixed-close", "grammar-close"} {
		var calls, closed atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			calls.Add(1)
			writer.Header().Set("Content-Type", "application/json")
			switch refusal {
			case "status":
				writer.WriteHeader(http.StatusForbidden)
			case "redirect":
				writer.Header().Set("Location", "/moved")
				writer.WriteHeader(http.StatusFound)
			case "media":
				writer.Header().Set("Content-Type", "text/plain")
			case "grammar", "grammar-close":
				_, _ = io.WriteString(writer, "{")
				return
			}
			_, _ = writer.Write(encoded)
		}))
		reader, err := NewHTTPArtifactReader(server.URL, artifact.DeploymentID, artifact.Netuid)
		if err != nil {
			t.Fatal(err)
		}
		hash, maximum := originalHash, uint64(len(encoded))
		if refusal == "digest" {
			hash[0] ^= 1
		}
		if refusal == "length" || refusal == "overflow-read" {
			maximum--
		}
		transport := &http.Transport{Proxy: nil}
		reader.client.Transport = committedHttpTestTransport(func(request *http.Request) (*http.Response, error) {
			response, err := transport.RoundTrip(request)
			if err != nil {
				return response, err
			}
			body := &committedHttpTestBody{ReadCloser: response.Body, closes: &closed}
			switch refusal {
			case "overflow-read":
				response.ContentLength = -1
				body.readErr, body.readAfter = io.ErrUnexpectedEOF, int(maximum)+1
			case "path-close":
				body.closeErr = &os.PathError{Op: "close", Path: "synthetic-committed-response", Err: context.DeadlineExceeded}
			case "mixed-close":
				body.closeErr = errors.Join(io.EOF, hard)
			case "grammar-close":
				body.closeErr = io.ErrUnexpectedEOF
			}
			response.Body = body
			return response, nil
		})
		waits := 0
		reader.retryHooks.wait = func(context.Context, time.Duration) error { waits++; return errors.New("unexpected hard-cause retry") }
		actual, err := reader.readCommittedReleaseDecisionV2Artifact(t.Context(), hash, maximum)
		transport.CloseIdleConnections()
		server.Close()
		if actual != nil || err == nil || RetryableEvidenceTransportError(err) || calls.Load() != 1 || closed.Load() != 1 || waits != 0 {
			t.Fatalf("%s was hidden by a transient sibling: calls=%d closes=%d waits=%d err=%v", refusal, calls.Load(), closed.Load(), waits, err)
		}
		if refusal == "mixed-close" && !errors.Is(err, hard) || (refusal == "overflow-read" || refusal == "grammar-close") && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("%s lost the actual physical cause: %v", refusal, err)
		}
	}
}

// Earlier caller allowance and cancellation propagate through an already open
// real HTTP body. No result or retry survives that joined close boundary.
func TestCommittedArtifactHttpReadCancellationJoinsOpenBody(t *testing.T) {
	artifact, encoded := validatorTestArtifact(t)
	hash, err := parseReleaseContentHash(artifact.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	entered, released := make(chan struct{}), make(chan struct{})
	var calls, closed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, "{")
		writer.(http.Flusher).Flush()
		<-request.Context().Done()
		close(released)
	}))
	defer server.Close()
	reader, err := NewHTTPArtifactReader(server.URL, artifact.DeploymentID, artifact.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	defer cancel()
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	reader.client.Transport = committedHttpTestTransport(func(request *http.Request) (*http.Response, error) {
		if actual, ok := request.Context().Deadline(); !ok || !actual.Equal(deadline) {
			t.Error("committed GET replaced the earlier caller deadline")
		}
		response, err := transport.RoundTrip(request)
		if err == nil {
			response.Body = &committedHttpTestBody{ReadCloser: response.Body, closes: &closed}
			close(entered)
		}
		return response, err
	})
	done := make(chan error, 1)
	go func() {
		actual, err := reader.readCommittedReleaseDecisionV2Artifact(ctx, hash, uint64(len(encoded)))
		if actual != nil {
			t.Error("canceled committed GET published an artifact")
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("committed GET did not open a real body")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("committed GET lost cancellation", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("committed GET did not join its canceled body")
	}
	select {
	case <-released:
	case <-time.After(10 * time.Second):
		t.Fatal("committed server still owns the canceled body")
	}
	if calls.Load() != 1 || closed.Load() != 1 {
		t.Fatal("cancellation started another GET or lost closure", calls.Load(), closed.Load())
	}
}

// A current transport deadline stays retryable at the historical audit caller;
// it never becomes an invented historical unavailability observation.
func TestHistoricalCommittedArtifactHttpFailurePreservesTransportCause(t *testing.T) {
	fixture := newReleaseDecisionV2TestFixture(t)
	observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query)
	if err != nil {
		t.Fatal(err)
	}
	entered, released := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, "{")
		writer.(http.Flusher).Flush()
		close(entered)
		<-request.Context().Done()
		close(released)
	}))
	defer server.Close()
	cfg := validReleaseConfig(t)
	cfg.Operators[0].APIURL = server.URL
	history := &releaseEvidenceV2StartupHistory{cfg: cfg}
	owner := &releaseHttpGetTestDeadline{Context: t.Context(), deadline: time.Now().Add(30 * time.Second), done: make(chan struct{})}
	type auditResult struct {
		audit DepositAudit
		err   error
	}
	done := make(chan auditResult, 1)
	go func() {
		audit, err := history.historicalDepositAudit(owner, observed, observed.operators[0], DepositAuditCompliant)
		done <- auditResult{audit: audit, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		close(owner.done)
		t.Fatal("historical audit did not start committed GET")
	}
	close(owner.done)
	select {
	case result := <-done:
		if result.audit != (DepositAudit{}) || !errors.Is(result.err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(result.err) || errors.Is(result.err, errReleaseHistoricalArtifactV2) {
			t.Fatal("historical caller changed transport failure into authority", result.audit, result.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("historical audit did not join expired GET")
	}
	select {
	case <-released:
	case <-time.After(10 * time.Second):
		t.Fatal("historical GET did not release its actual body")
	}
	if calls.Load() != 1 {
		t.Fatal("historical deadline started another read", calls.Load())
	}
}
