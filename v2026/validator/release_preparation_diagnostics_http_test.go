//go:build linux || darwin

// Real Http owners expose their failed stage without changing cancellation or errors.
package validator

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type preparationDiagnosticsHttpFixture struct {
	ctx       context.Context
	owner     *releaseDiagnostics
	sink      *preparationDiagnosticsHttpSink
	endpoint  *httptest.Server
	client    *http.Client
	transport *http.Transport
}

type preparationDiagnosticsHttpSink struct {
	records chan []byte
}

func (self *preparationDiagnosticsHttpSink) Write([]byte) (int, error) {
	panic("unexpected synchronous diagnostic output")
}

func (self *preparationDiagnosticsHttpSink) WriteContext(ctx context.Context, raw []byte) (int, error) {
	select {
	case self.records <- append([]byte(nil), raw...):
		return len(raw), nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// Each fixture owns its real transport, exporter and server; no global seam changes.
func newPreparationDiagnosticsHttpFixture(t *testing.T, handler http.HandlerFunc) *preparationDiagnosticsHttpFixture {
	t.Helper()
	endpoint := httptest.NewUnstartedServer(handler)
	endpoint.EnableHTTP2 = true
	endpoint.StartTLS()
	t.Cleanup(endpoint.Close)
	transport := newReleaseAPIHTTPTransport()
	transport.TLSClientConfig = endpoint.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	t.Cleanup(transport.CloseIdleConnections)
	sink := &preparationDiagnosticsHttpSink{records: make(chan []byte, 16)}
	ctx, owner, err := newReleaseDiagnostics(t.Context(), sink, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.close() })
	return &preparationDiagnosticsHttpFixture{ctx: ctx, owner: owner, sink: sink, endpoint: endpoint, client: &http.Client{Transport: transport}, transport: transport}
}

// A second actual request shares only its live parent, never the canceled child.
func (self *preparationDiagnosticsHttpFixture) healthy(t *testing.T) {
	t.Helper()
	request, err := http.NewRequestWithContext(self.ctx, http.MethodGet, self.endpoint.URL+"/healthy", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := doReleaseApiHttpRequest(self.client, request)
	if err != nil {
		t.Fatal(err)
	}
	raw, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if response.ProtoMajor != 2 || response.StatusCode != http.StatusOK || string(raw) != "healthy" || readErr != nil || closeErr != nil || self.ctx.Err() != nil {
		t.Fatal("independent Http request or shared parent was retired")
	}
}

// A FIFO barrier makes even an absent pre-fix record fail without a timed wait.
func (self *preparationDiagnosticsHttpFixture) requireStage(t *testing.T, stage string, records int) {
	t.Helper()
	const barrier = "synthetic diagnostic delivery barrier\n"
	var output strings.Builder
	actualRecords := 0
	// The unchanged production queue holds two records. A real delivery
	// acknowledges space before retrying a barrier that met both pending.
	for !self.owner.exporter.Offer("steering", []byte(barrier)) {
		raw := <-self.sink.records
		actualRecords++
		output.Write(raw)
	}
	for {
		raw := <-self.sink.records
		if string(raw) == barrier {
			break
		}
		actualRecords++
		output.Write(raw)
	}
	if err := self.owner.close(); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	failedStage := false
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, " stage="+stage+" event=failed ") && strings.HasSuffix(line, " cancel_cause=deadline") {
			failedStage = true
			break
		}
	}
	if actualRecords != records || !failedStage {
		t.Fatalf("actual Http owner lost its closed stage or cancellation cause: %s", text)
	}
	if strings.Contains(text, "/blocked") || strings.Contains(text, "synthetic-session") || strings.Contains(text, "synthetic close refusal") {
		t.Fatal("Http diagnostics exported an endpoint, credential or original error")
	}
}

// A real server barrier withholds headers until the request's own cancellation.
func TestPreparationDiagnosticsHttpOpenOwnedDeadline(t *testing.T) {
	entered, stopped := make(chan struct{}), make(chan struct{})
	fixture := newPreparationDiagnosticsHttpFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthy" {
			_, _ = io.WriteString(w, "healthy")
			return
		}
		close(entered)
		<-r.Context().Done()
		close(stopped)
	})
	ctx, cancel := context.WithCancelCause(fixture.ctx)
	defer cancel(context.Canceled)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fixture.endpoint.URL+"/blocked", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := doReleaseApiHttpRequest(fixture.client, request); done <- err }()
	<-entered
	fixture.healthy(t)
	cancel(context.DeadlineExceeded)
	err = <-done
	<-stopped
	var requestErr *url.Error
	if !errors.As(err, &requestErr) || requestErr.Err != context.Canceled || fixture.ctx.Err() != nil {
		t.Fatal("diagnostics replaced the original request cancellation")
	}
	fixture.healthy(t)
	fixture.requireStage(t, "http_open", 1)
}

type preparationDiagnosticsReadBarrier struct {
	io.ReadCloser
	entered chan struct{}
}

func (self *preparationDiagnosticsReadBarrier) Read(raw []byte) (int, error) {
	close(self.entered)
	return self.ReadCloser.Read(raw)
}

// Flushed real headers transfer the same owner to a physical blocked body read.
func TestPreparationDiagnosticsHttpReadOwnedDeadline(t *testing.T) {
	stopped := make(chan struct{})
	fixture := newPreparationDiagnosticsHttpFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthy" {
			_, _ = io.WriteString(w, "healthy")
			return
		}
		w.Header().Set("Content-Length", "1")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(stopped)
	})
	ctx, cancel := context.WithCancelCause(fixture.ctx)
	defer cancel(context.Canceled)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fixture.endpoint.URL+"/blocked", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := doReleaseApiHttpRequest(fixture.client, request)
	if err != nil {
		t.Fatal(err)
	}
	body, ok := response.Body.(*releaseApiHttpResponseBody)
	if !ok || response.ProtoMajor != 2 {
		t.Fatal("actual transport did not transfer its response owner")
	}
	entered := make(chan struct{})
	body.ReadCloser = &preparationDiagnosticsReadBarrier{ReadCloser: body.ReadCloser, entered: entered}
	done := make(chan error, 1)
	go func() { _, err := io.ReadAll(body); done <- err }()
	<-entered
	fixture.healthy(t)
	cancel(context.DeadlineExceeded)
	err = <-done
	<-stopped
	if !errors.Is(err, context.Canceled) || body.Close() != nil || fixture.ctx.Err() != nil {
		t.Fatal("diagnostics replaced the physical body's cancellation")
	}
	fixture.healthy(t)
	fixture.requireStage(t, "http_read", 1)
}

// A consumed real upload withholds its acknowledgement at an explicit barrier.
func TestPreparationDiagnosticsHttpWriteOwnedDeadline(t *testing.T) {
	entered, stopped := make(chan struct{}), make(chan struct{})
	fixture := newPreparationDiagnosticsHttpFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthy" {
			_, _ = io.WriteString(w, "healthy")
			return
		}
		if r.Method != http.MethodPost || r.ProtoMajor != 2 {
			t.Error("upload did not reach the actual Http/2 writer")
		}
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
		close(stopped)
	})
	writer := newAttemptStreamV2HTTPTestWriter(t, fixture.endpoint.URL, func() string { return "synthetic-session" })
	writer.client = fixture.client
	ctx, cancel := context.WithCancelCause(fixture.ctx)
	defer cancel(context.Canceled)
	data := []byte("synthetic immutable upload\n")
	done := make(chan error, 1)
	go func() { done <- writer.Write(ctx, AttemptStreamV2Records, attemptHex32(sha256.Sum256(data)), data) }()
	<-entered
	fixture.healthy(t)
	cancel(context.DeadlineExceeded)
	err := <-done
	<-stopped
	var requestErr *url.Error
	if !errors.As(err, &requestErr) || requestErr.Err != context.Canceled || fixture.ctx.Err() != nil {
		t.Fatal("diagnostics replaced the original upload cancellation")
	}
	fixture.healthy(t)
	fixture.requireStage(t, "http_write", 2)
}

type preparationDiagnosticsCloseBarrier struct {
	io.ReadCloser
	entered chan struct{}
	leave   chan struct{}
	cause   error
}

func (self *preparationDiagnosticsCloseBarrier) Close() error {
	if err := self.ReadCloser.Close(); err != nil {
		return err
	}
	close(self.entered)
	<-self.leave
	return self.cause
}

// Close retains an independent hard graph and observes before its own release cancels.
func TestPreparationDiagnosticsHttpCloseRetainsOriginalGraph(t *testing.T) {
	fixture := newPreparationDiagnosticsHttpFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "healthy")
	})
	ctx, cancel := context.WithCancelCause(fixture.ctx)
	defer cancel(context.Canceled)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fixture.endpoint.URL+"/blocked", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := doReleaseApiHttpRequest(fixture.client, request)
	if err != nil {
		t.Fatal(err)
	}
	body, ok := response.Body.(*releaseApiHttpResponseBody)
	if !ok || response.ProtoMajor != 2 {
		t.Fatal("actual transport omitted its close owner")
	}
	if _, err := io.ReadAll(body); err != nil {
		t.Fatal(err)
	}
	hard := errors.New("synthetic close refusal")
	original := errors.Join(context.DeadlineExceeded, hard)
	if retryableProductionSteeringRead(observeProductionSteeringRead(original, false)) {
		t.Fatal("original mixed hard graph unexpectedly authorized retry")
	}
	entered, leave := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-leave:
		default:
			close(leave)
		}
	}()
	body.ReadCloser = &preparationDiagnosticsCloseBarrier{ReadCloser: body.ReadCloser, entered: entered, leave: leave, cause: original}
	done := make(chan error, 1)
	go func() { done <- body.Close() }()
	<-entered
	fixture.healthy(t)
	cancel(context.DeadlineExceeded)
	close(leave)
	err = <-done
	if err != original || body.Close() != original || fixture.ctx.Err() != nil || retryableProductionSteeringRead(observeProductionSteeringRead(err, false)) {
		t.Fatal("diagnostics changed original Close identity or mixed hard classification")
	}
	fixture.healthy(t)
	fixture.requireStage(t, "http_close", 1)
}
