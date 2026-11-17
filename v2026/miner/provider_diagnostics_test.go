// These controls drive real provider callbacks, files, HTTP status and SDK
// refresh ownership. Diagnostic capture never supplies an auth/custody verdict.
package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// A finite explicit context sink lets a test observe the actual emitted record
// before joining the writer. It never depends on Close draining queued records.
type providerDiagnosticRecorder struct{ records chan []byte }

// Unconditional io.Writer calls are forbidden; admission must use the contract.
func (self *providerDiagnosticRecorder) Write(raw []byte) (int, error) {
	return 0, errors.New("unexpected synchronous recorder write")
}

// Copy one admitted bounded record; a full fixture is a real write refusal.
func (self *providerDiagnosticRecorder) WriteContext(_ context.Context, raw []byte) (int, error) {
	select {
	case self.records <- bytes.Clone(raw):
		return len(raw), nil
	default:
		return 0, errors.New("synthetic diagnostic recorder is full")
	}
}

// A timeout is only a deadlock backstop around the actual write barrier.
func (self *providerDiagnosticRecorder) next(t *testing.T) []byte {
	t.Helper()
	select {
	case raw := <-self.records:
		return raw
	case <-time.After(10 * time.Second):
		t.Fatal("provider diagnostic record did not reach its owned sink")
		return nil
	}
}

// Refused sinks must never be called, including after callbacks or teardown.
type providerRefusedWriter struct{ calls atomic.Uint64 }

// Count forbidden admission without panicking and aborting a causal matrix.
func (self *providerRefusedWriter) Write(raw []byte) (int, error) {
	self.calls.Add(1)
	return 0, errors.New("synthetic synchronous sink must be refused")
}

// Raw error formatting is itself the forbidden observable effect.
type providerDiagnosticCauseProbe struct{ calls atomic.Uint64 }

// Returning synthetic text keeps a negative control local to its test root.
func (self *providerDiagnosticCauseProbe) Error() string {
	self.calls.Add(1)
	return "synthetic-private-error-text"
}

// Each fixture owns and joins its exporter, including assertion early returns.
func newProviderDiagnosticTestOwner(t *testing.T, writer io.Writer) *providerDiagnostics {
	t.Helper()
	owner, err := newProviderDiagnostics(t.Context(), writer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.close(); err != nil {
			t.Fatal("provider diagnostic fixture close failed")
		}
	})
	return owner
}

// Independent runs retain independent budgets and closed scalar facts. Large
// SDK error/address strings and arbitrary Error methods never enter output.
func TestProviderDiagnosticsClosedFactsAndIndependentOwners(t *testing.T) {
	firstSink := &providerDiagnosticRecorder{records: make(chan []byte, 16)}
	secondSink := &providerDiagnosticRecorder{records: make(chan []byte, 16)}
	first := newProviderDiagnosticTestOwner(t, firstSink)
	second := newProviderDiagnosticTestOwner(t, secondSink)
	cause := &providerDiagnosticCauseProbe{}
	first.observe(providerAuthenticationWait, math.MaxUint64, true, errors.Join(context.DeadlineExceeded, cause), time.Second, nil)
	raw := firstSink.next(t)
	if cause.calls.Load() != 0 || len(raw) > 1024 || bytes.Contains(raw, []byte("synthetic-private")) || !bytes.Contains(raw, []byte(`"cause":"unknown"`)) || !bytes.Contains(raw, []byte(`"provider":18446744073709551615`)) {
		t.Fatal("provider diagnostics formatted or misclassified arbitrary cause")
	}
	listener := newProviderExtenderStatusListener(first, 3)
	status := &sdk.ExtenderProvideStatus{Enabled: true, Listening: true, ListenError: strings.Repeat("synthetic-private", 128*1024), LastActivationError: strings.Repeat("synthetic-failure", 128*1024), Ipv4: "192.0.2.11"}
	listener.ExtenderProvideStatusChanged(status)
	raw = firstSink.next(t)
	status.ListenError, status.Ipv4 = "different synthetic text", "192.0.2.12"
	listener.ExtenderProvideStatusChanged(status)
	if len(raw) > 1024 || bytes.Contains(raw, []byte("synthetic-private")) || bytes.Contains(raw, []byte("192.0.2.")) || !bytes.Contains(raw, []byte(`"listen_failed":true`)) {
		t.Fatal("extender callback retained unbounded text or identity")
	}
	if err := first.close(); err != nil {
		t.Fatal(err)
	}
	if first.snapshot().Extender.Delivered != 1 {
		t.Fatal("repeated scalar extender facts manufactured progress")
	}
	second.observe(providerStarted, 9, true, nil, 0, nil)
	secondSink.next(t)
	if err := second.close(); err != nil || second.snapshot().Runtime.Delivered != 1 {
		t.Fatal("closing one provider stole another instance's output ownership")
	}
}

// Refusal cannot delay real credential writes or cancellation after a fault.
func TestProviderDiagnosticsAuthenticationFilesSurviveSinkRefusal(t *testing.T) {
	writer := &providerRefusedWriter{}
	owner := newProviderDiagnosticTestOwner(t, writer)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	path := filepath.Join(t.TempDir(), "client.jwt")
	callbacks := &providerAuthenticationCallbacks{diagnostics: owner, provider: 4, clientJwtPath: path, cancel: cancel}
	callbacks.JwtRefreshed("synthetic-local-token")
	if token, err := clientauth.ReadToken(path); err != nil || token != "synthetic-local-token" || ctx.Err() != nil {
		t.Fatal("refused output changed successful token persistence")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	callbacks.JwtRefreshed("synthetic-replacement")
	var pathErr *os.LinkError
	if ctx.Err() != context.Canceled || !errors.As(callbacks.failure(), &pathErr) || writer.calls.Load() != 0 {
		t.Fatal("refused output delayed cancellation or lost original token fault")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	state := owner.snapshot().Authentication
	if state.Outcome != "unavailable" || state.Delivered != 0 || state.Dropped != 1 || state.Unavailable != 0 {
		t.Fatal("refused sink fabricated attempted write or delivery")
	}
}

// Local-only unsigned fixture tokens identify generated clients; no credential
// authority or server acceptance is inferred from the SDK's shape parser.
func providerDiagnosticTestJwt(t *testing.T, client, device, network connect.Id, marker string) string {
	t.Helper()
	raw, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims{"client_id": client.String(), "device_id": device.String(), "network_id": network.String(), "exp": time.Now().Add(30 * 24 * time.Hour).Unix(), "marker": marker}).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// A genuine SDK refresh/logout loop dispatches the provider's actual callback.
// The fixture owns one direct local HTTP strategy and joins it before return.
func providerDiagnosticTestApi(t *testing.T, ctx context.Context, handler http.Handler) *sdk.Api {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hello" {
			w.WriteHeader(http.StatusOK)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	settings := connect.DefaultClientStrategySettings()
	settings.EnableNormal, settings.EnableResilient = true, false
	settings.Log = connect.NewNoopLogger()
	strategy := connect.NewClientStrategy(ctx, settings)
	api := sdk.NewApi(ctx, strategy, server.URL)
	t.Cleanup(func() {
		if err := api.CloseAndWait(context.Background()); err != nil {
			t.Fatal("SDK refresh fixture did not join")
		}
		strategy.Close()
	})
	return api
}

// The callback must cancel its actual SDK worker even when its token rename
// fails. A failed log sink does not become another authentication dependency.
func TestProviderDiagnosticsSdkRefreshFileFailureCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	owner := newProviderDiagnosticTestOwner(t, &providerRefusedWriter{})
	client, device, network := connect.NewId(), connect.NewId(), connect.NewId()
	initial := providerDiagnosticTestJwt(t, client, device, network, "initial")
	refreshed := providerDiagnosticTestJwt(t, client, device, network, "refreshed")
	var requests atomic.Uint64
	api := providerDiagnosticTestApi(t, ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/refresh" {
			t.Error("unexpected local API path")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"by_jwt": refreshed})
	}))
	path := filepath.Join(t.TempDir(), "blocked.jwt")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	callbacks := &providerAuthenticationCallbacks{diagnostics: owner, provider: 7, clientJwtPath: path, cancel: cancel}
	sub := api.AddJwtRefreshListener(callbacks)
	defer sub.Close()
	api.SetByJwt(initial)
	api.StartJwtRefresh()
	select {
	case <-ctx.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("SDK token failure did not reach provider cancellation")
	}
	if err := api.CloseAndWait(context.Background()); err != nil {
		t.Fatal(err)
	}
	var pathErr *os.LinkError
	if !errors.As(callbacks.failure(), &pathErr) || requests.Load() != 1 {
		t.Fatal("SDK callback lost original file failure or retried after cancellation")
	}
}

// A genuine server rejection retains the real tombstone before shutdown.
// A failed tombstone still cancels and retains its original filesystem cause.
func TestProviderDiagnosticsSdkLogoutRetainsRejection(t *testing.T) {
	for _, refuseMarker := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		owner := newProviderDiagnosticTestOwner(t, &providerRefusedWriter{})
		path := filepath.Join(t.TempDir(), "client.jwt")
		networkPath := filepath.Join(filepath.Dir(path), "network.jwt")
		initial := providerDiagnosticTestJwt(t, connect.NewId(), connect.NewId(), connect.NewId(), "initial")
		if err := errors.Join(clientauth.WriteToken(path, initial), clientauth.WriteToken(networkPath, "synthetic-bootstrap")); err != nil {
			t.Fatal(err)
		}
		if refuseMarker {
			if err := os.Mkdir(path+".rejected", 0700); err != nil {
				t.Fatal(err)
			}
		}
		api := providerDiagnosticTestApi(t, ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
		callbacks := &providerAuthenticationCallbacks{diagnostics: owner, provider: 8, clientJwtPath: path, networkJwtPath: networkPath, cancel: cancel}
		sub := api.AddAuthLogoutListener(callbacks)
		api.SetByJwt(initial)
		api.StartJwtRefresh()
		select {
		case <-ctx.Done():
		case <-time.After(15 * time.Second):
			cancel()
			t.Fatal("SDK rejection did not reach provider cancellation")
		}
		if err := api.CloseAndWait(context.Background()); err != nil {
			t.Fatal(err)
		}
		sub.Close()
		cancel()
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("rejected client token remained installed")
		}
		if refuseMarker {
			var pathErr *os.LinkError
			if !errors.As(callbacks.failure(), &pathErr) {
				t.Fatal("failed rejection tombstone lost its original cause")
			}
		} else if marker, err := clientauth.ReadToken(path + ".rejected"); err != nil || len(marker) != 64 || callbacks.failure() != nil {
			t.Fatal("SDK rejection did not retain its real bootstrap tombstone")
		}
	}
}

// Required key writes still happen with a refused sink, and key failures retain
// their historical nonfatal behavior without an invented persisted-key claim.
func TestProviderDiagnosticsKeyPersistenceAndFailureFacts(t *testing.T) {
	state := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", state)
	owner := newProviderDiagnosticTestOwner(t, &providerRefusedWriter{})
	seed := bytes.Repeat([]byte{37}, 32)
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("synthetic-certificate")})
	key := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("synthetic-private-key")})
	material := sdk.NewDeviceLocalKeyMaterial(seed, cert, key)
	material.SetExtenderKeySeed(bytes.Repeat([]byte{43}, 32))
	persistProviderKeyMaterial(owner, 2, material)
	if actual, err := readProviderClientKeySeed(); err != nil || !bytes.Equal(actual, seed) {
		t.Fatal("refused output prevented real client key persistence")
	}
	if actual, err := readProviderExtenderKeySeed(); err != nil || !bytes.Equal(actual, material.GetExtenderKeySeed()) {
		t.Fatal("refused output prevented real extender key persistence")
	}
	actualCert, actualKey, err := readProviderTlsCertAndKey()
	if err != nil || !bytes.Equal(actualCert, cert) || !bytes.Equal(actualKey, key) {
		t.Fatal("refused output prevented real TLS key persistence")
	}
	for _, name := range []string{".provider.key", ".provider.cert", ".provider.extender.key"} {
		path := filepath.Join(state, name)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	persistProviderKeyMaterial(owner, 2, material)
	if owner.snapshot().Keys.Dropped != 3 {
		t.Fatal("real key failures did not remain separate bounded facts")
	}
}

// Existing status fields and process-liveness semantics survive an optional
// bounded extension. An absent owner omits delivery rather than making it green.
func TestProviderDiagnosticsStatusOptionalAndBounded(t *testing.T) {
	t.Setenv("WARP_VERSION", "1.2.3")
	t.Setenv("WARP_HOST", "synthetic-provider.example")
	owner := newProviderDiagnosticTestOwner(t, &providerRefusedWriter{})
	owner.observe(providerJwtSaveFailed, 0, true, syscall.ENOSPC, 0, nil)
	for _, output := range []*providerDiagnostics{nil, owner} {
		response := httptest.NewRecorder()
		(&Status{diagnostics: output}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/status", nil))
		var current struct {
			Version     string                    `json:"version"`
			Status      string                    `json:"status"`
			Diagnostics *providerDiagnosticStatus `json:"diagnostics"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &current); err != nil || response.Code != http.StatusOK || current.Version != "1.2.3" || current.Status != "ok" || response.Body.Len() > 8*1024 {
			t.Fatal("optional diagnostics changed bounded process status")
		}
		if output == nil && current.Diagnostics != nil || output != nil && (current.Diagnostics == nil || current.Diagnostics.Schema != providerDiagnosticSchema || current.Diagnostics.Authentication.Outcome != "unavailable" || current.Diagnostics.Authentication.Delivered != 0 || current.Diagnostics.Authentication.Dropped != 1) {
			t.Fatal("missing or refused output became successful status delivery")
		}
	}
}

// Actual run admission creates output before a real listener refusal, then
// joins it on the early return. Cancellation and a panicking hook also release
// the same owner; no queued completion is inferred merely from Close.
func TestProviderDiagnosticsRunEarlyReturnAndCancellationJoin(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, kind := range []string{"listen", "canceled", "panic"} {
		writer := &providerRefusedWriter{}
		var created, closed *providerDiagnostics
		var closeCause error
		t.Cleanup(func() {
			if created != nil {
				if err := created.close(); err != nil {
					t.Error("retained early-return fixture owner failed cleanup")
				}
			}
		})
		ctx, cancel := context.WithCancel(t.Context())
		if kind == "canceled" {
			cancel()
		}
		ctx = context.WithValue(ctx, providerDiagnosticHooksKey{}, providerDiagnosticHooks{afterCreate: func(owner *providerDiagnostics) {
			created = owner
			if kind == "panic" {
				panic("synthetic early hook")
			}
		}, afterClose: func(owner *providerDiagnostics, err error) { closed, closeCause = owner, err }})
		settings := providerRunSettings{}
		if kind == "listen" {
			settings.port = listener.Addr().(*net.TCPAddr).Port
		}
		var recovered any
		runErr := func() error {
			defer func() { recovered = recover() }()
			return settings.run(ctx, writer)
		}()
		cancel()
		if created == nil || closed != created || closeCause != nil || writer.calls.Load() != 0 {
			t.Fatal("provider run early return did not join its actual diagnostic owner")
		}
		if kind == "listen" && !errors.Is(runErr, syscall.EADDRINUSE) || kind == "canceled" && !errors.Is(runErr, context.Canceled) || kind == "panic" && recovered != "synthetic early hook" {
			t.Fatal("diagnostic cleanup changed actual run termination")
		}
	}
}

// A real HTTP status connection sees optional delivery evidence, and closing
// its listener owner joins the Serve worker before the output owner releases.
func TestProviderDiagnosticsStatusOwnerJoins(t *testing.T) {
	t.Setenv("WARP_VERSION", "1.2.3")
	t.Setenv("WARP_HOST", "synthetic-provider.example")
	owner := newProviderDiagnosticTestOwner(t, &providerRefusedWriter{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := startProviderStatusServer(listener, owner, cancel)
	closed := false
	defer func() {
		if !closed {
			_ = server.close()
		}
	}()
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get("http://" + listener.Addr().String() + "/status")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	decodeErr := json.NewDecoder(io.LimitReader(response.Body, 8*1024)).Decode(&body)
	closeErr := response.Body.Close()
	if errors.Join(decodeErr, closeErr) != nil || len(body["diagnostics"]) == 0 || string(body["status"]) != `"ok"` {
		t.Fatal("real status owner omitted delivery state or changed process liveness")
	}
	if err := server.close(); err != nil || ctx.Err() != context.Canceled {
		t.Fatal("status owner close did not join cancellation")
	}
	closed = true
	client.CloseIdleConnections()
}

// Shutdown cancels a real admitted request, then joins its Status work. The
// entry/context barriers make the overlap explicit without timing guesses.
func TestProviderDiagnosticsStatusShutdownJoinsAdmittedHandler(t *testing.T) {
	t.Setenv("WARP_VERSION", "1.2.3")
	t.Setenv("WARP_HOST", "synthetic-provider.example")
	owner := newProviderDiagnosticTestOwner(t, &providerRefusedWriter{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	var completed atomic.Bool
	server := startProviderStatusServer(listener, owner, cancel, providerStatusHooks{afterAdmit: func(ctx context.Context) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-release
		completed.Store(true)
	}})
	requestDone := make(chan struct{})
	client := &http.Client{Timeout: 15 * time.Second}
	go func() {
		defer close(requestDone)
		response, _ := client.Get("http://" + listener.Addr().String() + "/status")
		if response != nil {
			_ = response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		close(release)
		_ = server.close()
		<-requestDone
		t.Fatal("status request was not admitted")
	}
	closed := make(chan error, 1)
	go func() { closed <- server.close() }()
	select {
	case <-canceled:
	case <-time.After(15 * time.Second):
		close(release)
		<-closed
		<-requestDone
		t.Fatal("status shutdown did not cancel the admitted request")
	}
	close(release)
	if err := <-closed; err != nil || !completed.Load() || ctx.Err() != context.Canceled {
		t.Fatal("status close returned before admitted handler completion")
	}
	<-requestDone
	client.CloseIdleConnections()
	// A request racing behind closed admission cannot call the original hook.
	server.handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/status", nil))
}
