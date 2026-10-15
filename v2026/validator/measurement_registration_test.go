//go:build linux || darwin

// The real no-config runner exercises private custody and local HTTP. Handoff
// observers stop after authentication; a separate root runs and joins real
// measurement, API and transport workers. All identities are synthetic.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docopt/docopt-go"
	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/operatorlist"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

const measurementTestClientId = "00000000-0000-0000-0000-000000000101"
const measurementTestDeviceId = "00000000-0000-0000-0000-000000000202"

type measurementTestPrincipal struct {
	NetworkId string   `json:"network_id"`
	UserId    string   `json:"user_id"`
	Roles     []string `json:"roles"`
	Principal string   `json:"principal"`
}
type measurementTestRecord struct {
	Schema    string                        `json:"schema"`
	Scope     clientauth.RegistrationScope  `json:"scope"`
	Principal measurementTestPrincipal      `json:"principal"`
	Request   sdk.RegisterNetworkClientArgs `json:"request"`
	ClientId  string                        `json:"client_id,omitempty"`
	DeviceId  string                        `json:"device_id,omitempty"`
}

func measurementTestToken(t *testing.T, client, marker string) string {
	t.Helper()
	claims := gojwt.MapClaims{"network_id": "00000000-0000-0000-0000-000000000301", "user_id": "00000000-0000-0000-0000-000000000401", "roles": []string{"validator"}, "principal": "synthetic-measurement-owner", "marker": marker, "exp": time.Now().Add(30 * 24 * time.Hour).Unix()}
	if client != "" {
		claims["client_id"], claims["device_id"] = client, measurementTestDeviceId
	}
	token, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, claims).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

type measurementRegistrationFixture struct {
	test        *testing.T
	dir         string
	networkPath string
	server      *httptest.Server
	stateLock   sync.Mutex
	posts       int
	legacy      int
	refreshes   int
	discoveries int
	original    []byte
	onCommit    func()
	onDiscovery func()
	status      int
}

func newMeasurementRunFixture(t *testing.T) *measurementRegistrationFixture {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	networkDir := t.TempDir()
	if err := os.Chmod(networkDir, 0700); err != nil {
		t.Fatal(err)
	}
	self := &measurementRegistrationFixture{test: t, dir: dir, networkPath: filepath.Join(networkDir, "jwt")}
	if err := os.WriteFile(filepath.Join(dir, ".validator.key"), bytes.Repeat([]byte{37}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if err := clientauth.WriteToken(filepath.Join(dir, ".validator.jwt"), measurementTestToken(t, measurementTestClientId, "legacy")); err != nil {
		t.Fatal(err)
	}
	if err := clientauth.WriteToken(self.networkPath, measurementTestToken(t, "", "bootstrap")); err != nil {
		t.Fatal(err)
	}
	self.server = httptest.NewServer(http.HandlerFunc(self.serveHttp))
	t.Cleanup(self.server.Close)
	return self
}

// Only this loopback listener can be reached, even by transport background work.
func (self *measurementRegistrationFixture) settings(adopt bool) measurementRunSettings {
	strategy := connect.DefaultClientStrategySettings()
	strategy.EnableResilient = false
	strategy.RequestTimeout = 30 * time.Second
	strategy.DialContextSettings = &connect.DialContextSettings{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != self.server.Listener.Addr().String() {
			return nil, errors.New("synthetic measurement fixture refuses nonlocal egress")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	return measurementRunSettings{apiUrl: self.server.URL, connectUrl: "ws" + strings.TrimPrefix(self.server.URL, "http"), stateDir: self.dir, networkPath: self.networkPath, concurrency: 1, m: 4, adoptLegacyKey: adopt, strategySettings: strategy}
}

func (self *measurementRegistrationFixture) run(parent context.Context, adopt bool, hooks measurementRunHooks) error {
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	return self.settings(adopt).run(context.WithValue(ctx, measurementRunHooksKey{}, hooks), io.Discard)
}

func (self *measurementRegistrationFixture) counts() (int, int, int, int) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.posts, self.legacy, self.refreshes, self.discoveries
}

func (self *measurementRegistrationFixture) serveHttp(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024))
	if err := errors.Join(err, r.Body.Close()); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	switch r.URL.Path {
	case "/hello":
		w.WriteHeader(http.StatusOK)
		return
	case "/":
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	case "/auth/refresh":
		claims := gojwt.MapClaims{}
		_, _, err := gojwt.NewParser().ParseUnverified(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), claims)
		if err != nil || claims["client_id"] != measurementTestClientId {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		self.stateLock.Lock()
		self.refreshes++
		self.stateLock.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"by_jwt": measurementTestToken(self.test, measurementTestClientId, "refreshed")})
		return
	case "/network/find-providers2":
		self.stateLock.Lock()
		self.discoveries++
		observed := self.onDiscovery
		self.stateLock.Unlock()
		if observed != nil {
			observed()
		}
		_, _ = w.Write([]byte(`{"providers":[]}`))
		return
	case "/network/register-client-v1":
		self.stateLock.Lock()
		self.posts++
		status := self.status
		commit := self.onCommit
		self.onCommit = nil
		if self.original == nil {
			self.original = bytes.Clone(body)
		} else if !bytes.Equal(self.original, body) {
			self.test.Error("measurement replay changed the original HTTP operation")
		}
		self.stateLock.Unlock()
		raw, err := os.ReadFile(filepath.Join(self.dir, ".validator.jwt.registration"))
		var record measurementTestRecord
		if err != nil || json.Unmarshal(raw, &record) != nil {
			self.test.Error("measurement POST preceded its original custody")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		encoded, err := sdk.EncodeNetworkClientRegistration(&record.Request)
		anchor, anchorErr := os.ReadFile(filepath.Join(self.dir, ".validator.jwt.registration.started"))
		if err != nil || !bytes.Equal(body, encoded) || anchorErr != nil || !bytes.Contains(anchor, []byte(record.Request.RegistrationId)) {
			self.test.Error("measurement POST differs from its original request and sent anchor")
		}
		if commit != nil {
			commit()
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		digest := sha256.Sum256(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"schema": sdk.NetworkClientRegistrationSchema, "registration_id": record.Request.RegistrationId, "request_sha256": hex.EncodeToString(digest[:]), "client_id": measurementTestClientId, "device_id": measurementTestDeviceId, "by_client_jwt": measurementTestToken(self.test, measurementTestClientId, "registered")})
		return
	case "/network/auth-client":
		self.stateLock.Lock()
		self.legacy++
		self.stateLock.Unlock()
		self.test.Error("measurement runner called the legacy allocating endpoint")
	}
	w.WriteHeader(http.StatusNotFound)
}

// Model an independently recovered original operation. The public command has
// no creation flag and cannot produce this record from a missing credential.
func (self *measurementRegistrationFixture) retainOperation() []byte {
	owner, err := clientauth.OpenValidatorMeasurementClientKey(self.test.Context(), filepath.Join(self.dir, ".validator.key"), true)
	if err != nil {
		self.test.Fatal(err)
	}
	seed := owner.Seed()
	if err := owner.Close(); err != nil {
		self.test.Fatal(err)
	}
	public := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	scope := clientauth.RegistrationScope{Endpoint: self.server.URL + "/network/register-client-v1", ClientKey: "0x" + hex.EncodeToString(public), ClientRole: "validator-measurement-v1", ClientSlot: "direct"}
	scopeRaw, err := json.Marshal(scope)
	if err != nil {
		self.test.Fatal(err)
	}
	digest := sha256.Sum256(scopeRaw)
	record := measurementTestRecord{Schema: "urnetwork-durable-client-registration-v1", Scope: scope, Principal: measurementTestPrincipal{NetworkId: "00000000-0000-0000-0000-000000000301", UserId: "00000000-0000-0000-0000-000000000401", Roles: []string{"validator"}, Principal: "synthetic-measurement-owner"}, Request: sdk.RegisterNetworkClientArgs{Schema: sdk.NetworkClientRegistrationSchema, RegistrationId: strings.Repeat("57", 32), ScopeSha256: hex.EncodeToString(digest[:]), DeviceDescription: "validator measurement"}}
	raw, err := json.Marshal(record)
	if err != nil {
		self.test.Fatal(err)
	}
	if err := clientauth.WriteToken(filepath.Join(self.dir, ".validator.jwt.registration"), string(raw)); err != nil {
		self.test.Fatal(err)
	}
	if err := os.Remove(filepath.Join(self.dir, ".validator.jwt")); err != nil {
		self.test.Fatal(err)
	}
	return raw
}

func TestMeasurementRunRefusesMissingKeyAndUnknownClient(t *testing.T) {
	for _, fault := range []string{"key", "client", "assertion"} {
		fixture := newMeasurementRunFixture(t)
		path := filepath.Join(fixture.dir, ".validator.key")
		if fault == "key" {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
		if fault == "client" {
			if err := os.Remove(filepath.Join(fixture.dir, ".validator.jwt")); err != nil {
				t.Fatal(err)
			}
		}
		var reached atomic.Bool
		err := fixture.run(t.Context(), fault != "assertion", measurementRunHooks{afterAuthenticated: func(string, connect.Id, []byte) error { reached.Store(true); return errors.New("unexpected handoff") }})
		posts, legacy, refreshes, _ := fixture.counts()
		_, markerErr := os.Stat(path + ".identity")
		if err == nil || reached.Load() || posts != 0 || legacy != 0 || refreshes != 0 || !errors.Is(markerErr, os.ErrNotExist) {
			t.Fatal("measurement startup invented missing key or client custody", fault, err)
		}
		if fault == "key" {
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("measurement startup regenerated its missing key")
			}
		}
	}
}

func TestMeasurementRunAdoptsLegacyAndPreservesUnknownLoss(t *testing.T) {
	fixture := newMeasurementRunFixture(t)
	stop := errors.New("synthetic measurement authentication handoff")
	var handoffs int
	hooks := measurementRunHooks{afterAuthenticated: func(token string, id connect.Id, seed []byte) error {
		handoffs++
		if id.String() != measurementTestClientId || len(seed) != 32 || token == "" {
			t.Error("measurement adoption changed its original client or key")
		}
		return stop
	}}
	if err := fixture.run(t.Context(), true, hooks); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(fixture.dir, ".validator.jwt.registration.existing"))
	if err != nil || !bytes.Contains(marker, []byte(`"client_role":"validator-measurement-v1"`)) {
		t.Fatal("measurement adoption lacks durable role custody")
	}
	if err := fixture.run(t.Context(), false, hooks); !errors.Is(err, stop) {
		t.Fatal("measurement ordinary restart required new adoption", err)
	}
	beforePosts, beforeLegacy, beforeRefresh, _ := fixture.counts()
	if err := os.Remove(filepath.Join(fixture.dir, ".validator.jwt")); err != nil {
		t.Fatal(err)
	}
	err = fixture.run(t.Context(), false, hooks)
	posts, legacy, refreshes, _ := fixture.counts()
	var refused *clientauth.RegistrationRefusedError
	if !errors.As(err, &refused) || refused.Code != "legacy_identity_requires_explicit_recovery" || handoffs != 2 || posts != beforePosts || legacy != beforeLegacy || refreshes != beforeRefresh || posts != 0 || beforeRefresh < 2 {
		t.Fatal("measurement lost legacy credential allocated or refreshed replacement work", err)
	}
}

func TestMeasurementRunLostReplyReplaysOriginalRequest(t *testing.T) {
	fixture := newMeasurementRunFixture(t)
	original := fixture.retainOperation()
	ctx, cancel := context.WithCancel(t.Context())
	fixture.onCommit = cancel
	if err := fixture.run(ctx, false, measurementRunHooks{}); !errors.Is(err, context.Canceled) {
		t.Fatal("measurement lost-reply fixture missed its HTTP commit", err)
	}
	after, err := os.ReadFile(filepath.Join(fixture.dir, ".validator.jwt.registration"))
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("measurement lost reply changed original custody")
	}
	stop := errors.New("synthetic recovered measurement handoff")
	if err := fixture.run(t.Context(), false, measurementRunHooks{afterAuthenticated: func(_ string, id connect.Id, _ []byte) error {
		if id.String() != measurementTestClientId {
			t.Error("measurement replay changed the server client")
		}
		return stop
	}}); !errors.Is(err, stop) {
		t.Fatal("measurement replay required unowned creation", err)
	}
	posts, legacy, _, _ := fixture.counts()
	if posts != 2 || legacy != 0 {
		t.Fatal("measurement replay did not reuse exactly the retained operation")
	}
}

func TestMeasurementRunRetryRefusesReplacedCustody(t *testing.T) {
	fixture := newMeasurementRunFixture(t)
	original := fixture.retainOperation()
	fixture.status = http.StatusServiceUnavailable
	var attempts int
	retained := fixture.dir + "-retained"
	t.Cleanup(func() { _ = os.RemoveAll(retained) })
	err := fixture.run(t.Context(), false, measurementRunHooks{afterAttempt: func(err error) error {
		attempts++
		if attempts == 1 {
			var unavailable *sdk.NetworkClientRegistrationUnavailableError
			if !errors.As(err, &unavailable) {
				t.Error("measurement retry fixture missed typed HTTP unavailability")
			}
			if err := os.Rename(fixture.dir, retained); err != nil {
				return err
			}
			if err := os.Mkdir(fixture.dir, 0700); err != nil {
				return err
			}
		}
		return nil
	}})
	posts, legacy, _, _ := fixture.counts()
	after, readErr := os.ReadFile(filepath.Join(retained, ".validator.jwt.registration"))
	if err == nil || attempts != 2 || posts != 1 || legacy != 0 || readErr != nil || !bytes.Equal(original, after) {
		t.Fatal("measurement retry escaped its original key directory", err)
	}
}

func TestMeasurementRunRetryRetainsExactOriginalOperation(t *testing.T) {
	fixture := newMeasurementRunFixture(t)
	original := fixture.retainOperation()
	fixture.status = http.StatusServiceUnavailable
	stop := errors.New("synthetic measurement resumed handoff")
	var attempts int
	err := fixture.run(t.Context(), false, measurementRunHooks{
		afterAttempt: func(err error) error {
			attempts++
			if attempts == 1 {
				var unavailable *sdk.NetworkClientRegistrationUnavailableError
				if !errors.As(err, &unavailable) {
					t.Error("measurement retry did not follow an actual unavailable POST")
				}
				fixture.stateLock.Lock()
				fixture.status = 0
				fixture.stateLock.Unlock()
			}
			return nil
		},
		afterAuthenticated: func(string, connect.Id, []byte) error { return stop },
	})
	posts, legacy, _, _ := fixture.counts()
	after, readErr := os.ReadFile(filepath.Join(fixture.dir, ".validator.jwt.registration"))
	var beforeRecord, afterRecord measurementTestRecord
	if !errors.Is(err, stop) || attempts != 2 || posts != 2 || legacy != 0 || readErr != nil || json.Unmarshal(original, &beforeRecord) != nil || json.Unmarshal(after, &afterRecord) != nil || beforeRecord.Scope != afterRecord.Scope || beforeRecord.Request != afterRecord.Request {
		t.Fatal("measurement transient recovery replaced or abandoned its original operation", err)
	}
}

// The first original client remains owned at its actual authenticated handoff.
// A second distinct key/state can now finish its own retained operation using
// the shared login; it cannot acquire the first state's still-live key owner.
func TestMeasurementRunCompletedReplayReleasesOnlySharedBootstrap(t *testing.T) {
	first, second := newMeasurementRunFixture(t), newMeasurementRunFixture(t)
	second.networkPath = first.networkPath
	if err := os.WriteFile(filepath.Join(second.dir, ".validator.key"), bytes.Repeat([]byte{38}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	first.retainOperation()
	second.retainOperation()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	defer finish()
	stop := errors.New("synthetic shared-bootstrap handoff")
	result := make(chan error, 1)
	go func() {
		result <- first.run(t.Context(), false, measurementRunHooks{afterAuthenticated: func(string, connect.Id, []byte) error { close(entered); <-release; return stop }})
	}()
	joined := false
	defer func() {
		finish()
		if !joined {
			select {
			case <-result:
			case <-time.After(60 * time.Second):
				t.Error("measurement shared-bootstrap fixture did not join")
			}
		}
	}()
	select {
	case <-entered:
	case <-time.After(60 * time.Second):
		t.Fatal("first retained measurement did not reach authenticated handoff")
	}
	if err := second.run(t.Context(), false, measurementRunHooks{afterAuthenticated: func(string, connect.Id, []byte) error { return stop }}); !errors.Is(err, stop) {
		t.Error("completed measurement monopolized bootstrap needed by an independent original operation", err)
	}
	other, err := clientauth.OpenValidatorMeasurementClientKey(t.Context(), filepath.Join(first.dir, ".validator.key"), false)
	if other != nil {
		_ = other.Close()
	}
	if err == nil {
		t.Error("measurement shared-bootstrap release also released its original key")
	}
	finish()
	select {
	case err := <-result:
		joined = true
		if !errors.Is(err, stop) {
			t.Fatal(err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("first measurement handoff did not join")
	}
	firstPosts, firstLegacy, _, _ := first.counts()
	secondPosts, secondLegacy, _, _ := second.counts()
	if firstPosts != 1 || secondPosts != 1 || firstLegacy != 0 || secondLegacy != 0 {
		t.Fatal("independent retained measurement replay did not complete both original operations")
	}
}

func TestMeasurementAuthenticationRetryPreservesEveryCause(t *testing.T) {
	unavailable := &sdk.NetworkClientRegistrationUnavailableError{}
	hard := errors.New("synthetic local custody failure")
	for _, err := range []error{hard, errors.Join(unavailable, hard), errors.Join(hard, unavailable), &clientauth.RegistrationRefusedError{Code: "synthetic-refusal"}} {
		if measurementAuthenticationRetryable(err, 0) {
			t.Fatal("measurement mixed or hard failure became an automatic replay")
		}
	}
	if !measurementAuthenticationRetryable(errors.Join(unavailable, unavailable), 0) {
		t.Fatal("measurement exact unavailable causes lost resumable progress")
	}
}

func TestMeasurementRunJoinsWorkersBeforeReleasingKey(t *testing.T) {
	fixture := newMeasurementRunFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	discovered := make(chan struct{})
	refreshed := make(chan struct{})
	var discoveryOnce sync.Once
	var refreshOnce sync.Once
	fixture.onDiscovery = func() { discoveryOnce.Do(func() { close(discovered) }) }
	trailAtJoin, releaseTrail := make(chan struct{}), make(chan struct{})
	statsAtJoin := make(chan struct{})
	waiting := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseTrail) }) }
	defer release()
	var trailJoined, statsJoined, apiJoined, earlyApiJoin atomic.Bool
	hooks := measurementRunHooks{afterRefreshPersisted: func() { refreshOnce.Do(func() { close(refreshed) }) }, beforeWorkersWait: func() { close(waiting) }, afterTrailJoined: func() { close(trailAtJoin); <-releaseTrail; trailJoined.Store(true) }, afterStatsJoined: func() { statsJoined.Store(true); close(statsAtJoin) }, afterApiJoined: func() {
		if !trailJoined.Load() || !statsJoined.Load() {
			earlyApiJoin.Store(true)
		}
		apiJoined.Store(true)
	}}
	result := make(chan error, 1)
	go func() { result <- fixture.run(ctx, true, hooks) }()
	joined := false
	defer func() {
		cancel()
		release()
		if !joined {
			select {
			case <-result:
			case <-time.After(60 * time.Second):
				t.Error("measurement cleanup did not join")
			}
		}
	}()
	select {
	case <-discovered:
	case <-time.After(60 * time.Second):
		t.Fatal("real measurement engine did not issue seed discovery")
	}
	select {
	case <-refreshed:
	case <-time.After(60 * time.Second):
		t.Fatal("measurement worker fixture did not join its startup refresh persistence")
	}
	cancel()
	select {
	case <-trailAtJoin:
	case <-time.After(60 * time.Second):
		t.Fatal("real measurement engine did not reach its join boundary")
	}
	select {
	case <-waiting:
	case <-time.After(60 * time.Second):
		t.Fatal("measurement owner did not reach worker wait")
	}
	select {
	case <-statsAtJoin:
	case <-time.After(60 * time.Second):
		t.Fatal("measurement periodic stats worker did not join")
	}
	// A slow fixture may already have a periodic snapshot. Its worker has
	// stopped and the trail worker still holds final shutdown, so removing
	// that earlier file isolates the real final save without timing bounds.
	statsPath := filepath.Join(fixture.dir, "stats.json")
	if err := os.Remove(statsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	other, err := clientauth.OpenValidatorMeasurementClientKey(t.Context(), filepath.Join(fixture.dir, ".validator.key"), false)
	if other != nil {
		_ = other.Close()
	}
	if err == nil {
		t.Error("measurement key was released while its trail worker was still owned")
	}
	release()
	select {
	case err := <-result:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("measurement runner did not join its real workers")
	}
	if !trailJoined.Load() || !statsJoined.Load() || !apiJoined.Load() || earlyApiJoin.Load() {
		t.Fatal("measurement worker or API join escaped key ownership")
	}
	owner, err := clientauth.OpenValidatorMeasurementClientKey(t.Context(), filepath.Join(fixture.dir, ".validator.key"), false)
	if err != nil {
		t.Fatal("joined measurement runner retained its key lock", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(statsPath); err != nil {
		t.Fatal("measurement joined shutdown omitted its final stats save", err)
	}
}

func TestMeasurementRefreshValidatesIdentityBeforePersistenceAndPublication(t *testing.T) {
	for _, fault := range []string{"valid", "client", "roles", "directory", "logout"} {
		fixture := newMeasurementRunFixture(t)
		owner, err := clientauth.OpenValidatorMeasurementClientKey(t.Context(), filepath.Join(fixture.dir, ".validator.key"), true)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = owner.Close() })
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		original := measurementTestToken(t, measurementTestClientId, "original")
		refreshed := measurementTestToken(t, measurementTestClientId, "refreshed")
		var published string
		callbacks := &measurementAuthenticationCallbacks{owner: owner, original: original, cancel: cancel, publish: func(token string) { published = token }}
		if fault == "client" {
			refreshed = measurementTestToken(t, "00000000-0000-0000-0000-000000000999", "changed")
		}
		if fault == "roles" {
			claims := gojwt.MapClaims{}
			if _, _, err := gojwt.NewParser().ParseUnverified(refreshed, claims); err != nil {
				t.Fatal(err)
			}
			claims["roles"] = []string{"synthetic-other-role"}
			refreshed, err = gojwt.NewWithClaims(gojwt.SigningMethodNone, claims).SignedString(gojwt.UnsafeAllowNoneSignatureType)
			if err != nil {
				t.Fatal(err)
			}
		}
		if fault == "directory" {
			moved := fixture.dir + "-retained"
			if err := os.Rename(fixture.dir, moved); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(moved) })
			if err := os.Mkdir(fixture.dir, 0700); err != nil {
				t.Fatal(err)
			}
		}
		if fault == "logout" {
			callbacks.AuthLogout()
		}
		before, beforeErr := os.ReadFile(filepath.Join(fixture.dir, ".validator.jwt"))
		callbacks.JwtRefreshed(refreshed)
		after, afterErr := os.ReadFile(filepath.Join(fixture.dir, ".validator.jwt"))
		if fault == "valid" {
			if callbacks.failure() != nil || ctx.Err() != nil || published != refreshed || afterErr != nil || string(after) != refreshed {
				t.Fatal("measurement valid renewal was not durably published")
			}
		} else if callbacks.failure() == nil || ctx.Err() == nil || published != "" || !bytes.Equal(before, after) || errors.Is(beforeErr, os.ErrNotExist) != errors.Is(afterErr, os.ErrNotExist) {
			t.Fatal("measurement invalid or unowned renewal was published", fault)
		}
	}
}

func TestMeasurementCliPreservesProductionDispatchAndNoCreateFlag(t *testing.T) {
	for _, args := range [][]string{{"run", "--state_dir=/synthetic/measurement", "--adopt-legacy-measurement-key"}, {"run", "--config=/synthetic/production.yml"}} {
		opts, err := parseValidatorArgsForTest(t, args)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(args, " "), "--config") {
			if optString(opts, "--config", "") != "/synthetic/production.yml" {
				t.Fatal("measurement CLI changed production config dispatch")
			}
		} else {
			settings, err := measurementSettingsFromOpts(opts)
			if err != nil || !settings.adoptLegacyKey || settings.stateDir != "/synthetic/measurement" {
				t.Fatal("measurement CLI lost explicit legacy key assertion", err)
			}
		}
	}
	for _, args := range [][]string{{"run", "--allow-client-registration"}, {"run", "--config=/synthetic/production.yml", "--adopt-legacy-measurement-key"}} {
		// Expected invalid input must return through the real parser, not
		// terminate the test process through docopt's default help handler.
		_, err := parseValidatorArgsForTest(t, args)
		var invalid *docopt.UserError
		if !errors.As(err, &invalid) {
			t.Fatal("measurement CLI admitted creation or mixed-mode flags", args, err)
		}
	}
}

// A real API worker pauses after persisting a valid refresh. Cancellation may
// join all trail work but cannot release the key while this callback is owned.
func TestMeasurementRunKeyOwnerSpansActualRefreshJoin(t *testing.T) {
	fixture := newMeasurementRunFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	waiting := make(chan struct{})
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	defer finish()
	var callbackJoined, earlyApiJoin atomic.Bool
	hooks := measurementRunHooks{
		beforeApiWait:         func() { close(waiting) },
		afterRefreshPersisted: func() { close(entered); <-release; callbackJoined.Store(true) },
		afterApiJoined: func() {
			if !callbackJoined.Load() {
				earlyApiJoin.Store(true)
			}
		},
	}
	result := make(chan error, 1)
	go func() { result <- fixture.run(ctx, true, hooks) }()
	joined := false
	defer func() {
		cancel()
		finish()
		if !joined {
			select {
			case <-result:
			case <-time.After(60 * time.Second):
				t.Error("measurement refresh owner did not join")
			}
		}
	}()
	select {
	case <-entered:
	case <-time.After(60 * time.Second):
		t.Fatal("real measurement API refresh did not reach durable callback")
	}
	cancel()
	select {
	case <-waiting:
	case <-time.After(60 * time.Second):
		t.Fatal("measurement owner did not reach API callback wait")
	}
	other, err := clientauth.OpenValidatorMeasurementClientKey(t.Context(), filepath.Join(fixture.dir, ".validator.key"), false)
	if other != nil {
		_ = other.Close()
	}
	if err == nil {
		t.Error("measurement key owner ended while its refresh callback was active")
	}
	finish()
	select {
	case err := <-result:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("measurement refresh join did not complete")
	}
	if !callbackJoined.Load() || earlyApiJoin.Load() {
		t.Fatal("measurement API join returned before its persisted refresh callback")
	}
	owner, err := clientauth.OpenValidatorMeasurementClientKey(t.Context(), filepath.Join(fixture.dir, ".validator.key"), false)
	if err != nil {
		t.Fatal("completed measurement refresh retained its key owner", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}

// The actual SDK notice captures the login generation. A same-byte replacement
// login invalidates the old notice; a current malformed reply withdraws service.
func TestMeasurementRefreshIntegrityNoticeHonorsCurrentGeneration(t *testing.T) {
	for _, stale := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hello" {
				w.WriteHeader(http.StatusOK)
				return
			}
			_, _ = w.Write([]byte(`{"by_jwt":"synthetic-malformed-token"}`))
		}))
		strategySettings := connect.DefaultClientStrategySettings()
		strategySettings.EnableResilient = false
		strategy := connect.NewClientStrategy(ctx, strategySettings)
		api := sdk.NewApi(ctx, strategy, server.URL)
		initial := measurementTestToken(t, measurementTestClientId, "initial")
		callbacks := &measurementAuthenticationCallbacks{original: initial, cancel: cancel}
		delivered := make(chan struct{})
		var once sync.Once
		sub := api.AddClientRefreshIntegrityListener(clientauth.ClientRefreshIntegrityListenerFunc(func(notice *sdk.ClientRefreshIntegrityNotice) {
			once.Do(func() {
				if stale {
					api.SetByJwt(initial)
				}
				callbacks.ClientRefreshInvalid(notice)
				close(delivered)
			})
		}))
		closeOwners := func() {
			cancel()
			if err := api.CloseAndWait(context.Background()); err != nil {
				t.Error(err)
			}
			sub.Close()
			strategy.Close()
			server.Close()
		}
		api.SetByJwt(initial)
		api.StartJwtRefresh()
		select {
		case <-delivered:
		case <-time.After(60 * time.Second):
			closeOwners()
			t.Fatal("measurement SDK integrity notice did not reach its callback")
		}
		if stale {
			if ctx.Err() != nil || callbacks.failure() != nil {
				closeOwners()
				t.Fatal("stale measurement integrity notice canceled a replacement login")
			}
		} else if ctx.Err() == nil || callbacks.failure() == nil {
			closeOwners()
			t.Fatal("current measurement integrity notice left malformed authority live")
		}
		closeOwners()
	}
}

// A retained registration the operator refuses for its network sign-in names
// the command that signs in again, not measurement custody recovery.
func TestMeasurementRunNamesARejectedSignIn(t *testing.T) {
	for _, authCommand := range []string{"", "validator auth --operator=op.example"} {
		fixture := newMeasurementRunFixture(t)
		fixture.retainOperation()
		fixture.status = http.StatusUnauthorized
		settings := fixture.settings(false)
		settings.authCommand = authCommand
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		err := settings.run(ctx, io.Discard)
		cancel()
		want := "`validator auth`"
		if authCommand != "" {
			want = "`" + authCommand + "`"
		}
		if !clientauth.IsNetworkCredentialRejected(err) || !strings.Contains(err.Error(), "the network sign-in at "+fixture.networkPath+" was rejected or has expired") || !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want guidance naming %s", err, want)
		}
		if posts, _, _, _ := fixture.counts(); posts != 1 {
			t.Fatalf("a rejected sign-in was retried (%d posts)", posts)
		}
	}
	operator := operatorlist.Operator{Domain: "op.example", ApiUrl: "https://api.op.example", ConnectUrl: "wss://connect.op.example"}
	settings := allOperatorsSettings{measurement: measurementRunSettings{stateDir: t.TempDir()}, list: operatorListOptions{url: operatorlist.DefaultUrl}}
	if runner := settings.operatorRunner(operator); runner.authCommand != settings.authCommand("op.example") || !strings.HasPrefix(runner.authCommand, "validator auth --operator=op.example") {
		t.Fatalf("operator runner auth command = %q", runner.authCommand)
	}
}
