//go:build linux || darwin

// Public provider startup uses real private files and the real SDK HTTP path.
// Synthetic allocation barriers stop before constructing a serving device.
package miner

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

	gojwt "github.com/golang-jwt/jwt/v5"
	"golang.org/x/net/proxy"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// These fields observe the actual canonical record without exposing its private
// implementation or supplying a substitute verifier to the provider runner.
type providerRegistrationTestRecord struct {
	Scope    clientauth.RegistrationScope  `json:"scope"`
	Request  sdk.RegisterNetworkClientArgs `json:"request"`
	ClientId string                        `json:"client_id"`
	DeviceId string                        `json:"device_id"`
}

// One fixture models versioned server dedup and deliberately non-idempotent
// legacy allocation. Real PostgreSQL allocation is independently qualified.
type providerRegistrationFixture struct {
	test           *testing.T
	dir            string
	server         *httptest.Server
	stateLock      sync.Mutex
	requests       map[string][]byte
	clients        map[string]string
	posts          int
	legacy         int
	allocations    int
	refreshes      int
	refreshClients map[string]int
	statuses       []int
	status         int
	malformed      bool
	onCommit       func()
}

// Explicit private parents keep both positive and negative custody fixtures
// independent of the runner's umask.
func providerRegistrationPrivateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Signatures are deliberately absent: these tokens exercise local identity
// binding, while the local synthetic HTTP server represents API admission.
func providerRegistrationTestToken(t *testing.T, client, marker string) string {
	t.Helper()
	claims := gojwt.MapClaims{"network_id": "00000000-0000-0000-0000-000000000301", "user_id": "00000000-0000-0000-0000-000000000401", "roles": []string{"provider"}, "principal": "synthetic-provider-owner", "marker": marker, "exp": time.Now().Add(30 * 24 * time.Hour).Unix()}
	if client != "" {
		claims["client_id"], claims["device_id"] = client, "00000000-0000-0000-0000-000000000202"
	}
	token, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, claims).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// Each root owns its directory and HTTP listener; no production configuration,
// DNS name, account or network endpoint is used by the fixture.
func newProviderRegistrationFixture(t *testing.T) *providerRegistrationFixture {
	t.Helper()
	self := &providerRegistrationFixture{test: t, dir: providerRegistrationPrivateDir(t), requests: map[string][]byte{}, clients: map[string]string{}, refreshClients: map[string]int{}}
	t.Setenv("URNETWORK_STATE_DIR", self.dir)
	if err := clientauth.WriteToken(filepath.Join(self.dir, "jwt"), providerRegistrationTestToken(t, "", "bootstrap")); err != nil {
		t.Fatal(err)
	}
	self.server = httptest.NewServer(http.HandlerFunc(self.serveHttp))
	t.Cleanup(self.server.Close)
	return self
}

// Even the public runner's transport can dial only this exact local listener.
// Direct and proxy workers retain their real independent scope/custody paths.
func (self *providerRegistrationFixture) settings(allow bool, members ...*connect.ProxySettings) providerRunSettings {
	return providerRunSettings{apiUrl: self.server.URL, connectUrl: self.server.URL, allowClientRegistration: allow, proxySettings: members,
		testEgressDialer: &connect.DialContextSettings{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != self.server.Listener.Addr().String() {
				return nil, errors.New("synthetic provider fixture refuses nonlocal egress")
			}
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}}}
}

// The timeout is only a deadlock backstop. Named fsync/HTTP/owner barriers decide
// the expected ordering; no sleep or negative timeout supplies the verdict.
func (self *providerRegistrationFixture) run(ctx context.Context, allow bool, hooks providerRegistrationHooks, members ...*connect.ProxySettings) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return self.settings(allow, members...).run(context.WithValue(ctx, providerRegistrationHooksKey{}, hooks), &providerRefusedWriter{})
}

// Read counters under the same ownership used by concurrent proxy handlers.
func (self *providerRegistrationFixture) counts() (int, int, int, int) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.posts, self.legacy, self.allocations, self.refreshes
}

// Parallel transport can race several physical GETs for one logical refresh.
// Client identities, authenticated handoffs and allocation POSTs are separate.
func (self *providerRegistrationFixture) refreshedClients() map[string]int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	result := make(map[string]int, len(self.refreshClients))
	for id, count := range self.refreshClients {
		result[id] = count
	}
	return result
}

// A versioned POST must already name the exact durable key, request and anchor.
// The fixture observes disk; it cannot create or repair custody for the runner.
func (self *providerRegistrationFixture) observeCustody(body []byte, request sdk.RegisterNetworkClientArgs) {
	seed, err := os.ReadFile(filepath.Join(self.dir, ".provider.key"))
	marker, markerErr := os.ReadFile(filepath.Join(self.dir, ".provider.key.identity"))
	if err != nil || markerErr != nil || len(seed) != ed25519.SeedSize {
		self.test.Error("provider allocated before its key and public marker were durable")
		return
	}
	public := "0x" + hex.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	if string(marker) != "urnetwork-provider-client-key-v1\n"+public+"\n" {
		self.test.Error("provider registration marker differs from its durable seed")
	}
	names, err := os.ReadDir(self.dir)
	if err != nil {
		self.test.Error(err)
		return
	}
	var matched bool
	for _, name := range names {
		if !strings.HasSuffix(name.Name(), ".jwt.registration") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(self.dir, name.Name()))
		var record providerRegistrationTestRecord
		if err != nil || json.Unmarshal(raw, &record) != nil || record.Request.ScopeSha256 != request.ScopeSha256 {
			continue
		}
		if matched {
			self.test.Error("provider scope appeared in more than one credential slot")
		}
		matched = true
		encoded, encodeErr := sdk.EncodeNetworkClientRegistration(&record.Request)
		scope, scopeErr := json.Marshal(record.Scope)
		digest := sha256.Sum256(scope)
		if encodeErr != nil || scopeErr != nil || !bytes.Equal(encoded, body) || hex.EncodeToString(digest[:]) != request.ScopeSha256 {
			self.test.Error("provider POST changed its durable original request or opaque scope")
		}
		if record.Scope.ClientRole != "provider-v1" || record.Scope.ClientKey != public || record.Scope.Endpoint != self.server.URL+"/network/register-client-v1" || record.Scope.ValidatorId != 0 || record.Scope.OperatorNoId != 0 || record.Scope.Netuid != 0 || record.Scope.ChainId != 0 || record.Scope.DeploymentId != "" || record.Scope.GenesisHash != "" || request.DeviceDescription != "provider" {
			self.test.Error("provider fabricated chain authority or mutable description")
		}
		anchor, anchorErr := os.ReadFile(filepath.Join(self.dir, name.Name()+".started"))
		if anchorErr != nil || !bytes.Contains(anchor, []byte(request.RegistrationId)) {
			self.test.Error("provider POST escaped before its retained first-send anchor")
		}
	}
	if !matched {
		self.test.Error("provider POST escaped before its original request was durable")
	}
}

// Allocation happens before a lost-reply barrier. A subsequent versioned call
// can return only the same client; the legacy route allocates on every call.
func (self *providerRegistrationFixture) serveHttp(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024))
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if r.URL.Path == "/hello" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.URL.Path == "/auth/refresh" {
		claims := gojwt.MapClaims{}
		_, _, err := gojwt.NewParser().ParseUnverified(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), claims)
		client, _ := claims["client_id"].(string)
		if err != nil || client == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		self.stateLock.Lock()
		self.refreshes++
		self.refreshClients[client]++
		self.stateLock.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"by_jwt": providerRegistrationTestToken(self.test, client, "refreshed")})
		return
	}
	versioned := r.URL.Path == "/network/register-client-v1"
	if (!versioned && r.URL.Path != "/network/auth-client") || r.Method != http.MethodPost {
		self.test.Error("provider fixture received an unrelated HTTP route")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var request sdk.RegisterNetworkClientArgs
	if versioned {
		if json.Unmarshal(body, &request) != nil {
			self.test.Error("provider request could not be decoded")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		self.observeCustody(body, request)
	}
	self.stateLock.Lock()
	var client string
	if versioned {
		self.posts++
		key := request.ScopeSha256
		original, exists := self.requests[key]
		if exists && !bytes.Equal(original, body) {
			self.stateLock.Unlock()
			self.test.Error("provider retry changed the server's bound operation")
			w.WriteHeader(http.StatusConflict)
			return
		}
		if !exists {
			self.allocations++
			self.requests[key] = bytes.Clone(body)
			self.clients[key] = fmt.Sprintf("00000000-0000-0000-0000-%012d", 100+self.allocations)
		}
		client = self.clients[key]
	} else {
		self.legacy++
		self.allocations++
		client = fmt.Sprintf("00000000-0000-0000-0000-%012d", 100+self.allocations)
	}
	status := self.status
	if len(self.statuses) > 0 {
		status, self.statuses = self.statuses[0], self.statuses[1:]
	}
	malformed, committed := self.malformed, self.onCommit
	self.onCommit = nil
	self.stateLock.Unlock()
	if committed != nil {
		committed()
		connection, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = connection.Close()
		}
		return
	}
	if status != 0 {
		w.WriteHeader(status)
		return
	}
	if malformed {
		_, _ = w.Write([]byte(`{"schema":"synthetic-wrong-schema"}`))
		return
	}
	if !versioned {
		_ = json.NewEncoder(w).Encode(map[string]string{"by_client_jwt": providerRegistrationTestToken(self.test, client, "legacy")})
		return
	}
	digest := sha256.Sum256(body)
	_ = json.NewEncoder(w).Encode(map[string]any{"schema": sdk.NetworkClientRegistrationSchema, "registration_id": request.RegistrationId, "request_sha256": hex.EncodeToString(digest[:]), "client_id": client, "device_id": "00000000-0000-0000-0000-000000000202", "by_client_jwt": providerRegistrationTestToken(self.test, client, "registered")})
}

// Both allocating commands parse explicit permission; ordinary starts keep it
// false. The flag never supplies a validator identity or bypasses disk checks.
func TestProviderRegistrationCreationFlagIsExplicit(t *testing.T) {
	for _, command := range []string{"provide", "auth-provide"} {
		for _, mode := range []string{"none", "create", "adopt"} {
			allow, adopt := mode == "create", mode == "adopt"
			args := []string{command}
			if command == "auth-provide" {
				args = append(args, "--user_auth=synthetic-provider")
			}
			if allow {
				args = append(args, "--allow-client-registration")
			} else if adopt {
				args = append(args, "--adopt-legacy-provider-key")
			}
			opts := parseArgsForTest(t, args)
			actual, err := opts.Bool("--allow-client-registration")
			if err != nil || actual != allow {
				t.Fatal("provider command lost its explicit creation decision", command, err)
			}
			actualAdopt, err := opts.Bool("--adopt-legacy-provider-key")
			if err != nil || actualAdopt != adopt {
				t.Fatal("provider command merged legacy key adoption and client creation", command, err)
			}
		}
	}
	fixture := newProviderRegistrationFixture(t)
	if err := fixture.run(t.Context(), false, providerRegistrationHooks{}); err == nil {
		t.Fatal("provider startup created identity without explicit permission")
	}
	posts, legacy, allocated, _ := fixture.counts()
	_, keyErr := os.Stat(filepath.Join(fixture.dir, ".provider.key"))
	if posts != 0 || legacy != 0 || allocated != 0 || !errors.Is(keyErr, os.ErrNotExist) {
		t.Fatal("unapproved provider startup allocated a key or client")
	}
}

// This is the original duplicate-allocation failure at the actual daemon
// boundary: commit, lose the physical reply, stop, and restart without the flag.
func TestProviderRegistrationLostReplyReplaysOriginalClient(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.onCommit = cancel
	if err := fixture.run(ctx, true, providerRegistrationHooks{}); !errors.Is(err, context.Canceled) {
		t.Fatal("provider lost reply did not interrupt its real HTTP owner", err)
	}
	seed, err := os.ReadFile(filepath.Join(fixture.dir, ".provider.key"))
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("synthetic stop after authenticated provider")
	var recovered connect.Id
	var returnedSeed []byte
	err = fixture.run(t.Context(), false, providerRegistrationHooks{afterAuthenticated: func(_ string, id connect.Id, key []byte) error { recovered, returnedSeed = id, key; return stop }})
	posts, legacy, allocated, _ := fixture.counts()
	if !errors.Is(err, stop) || allocated != 1 || legacy != 0 || posts != 2 || recovered.String() != "00000000-0000-0000-0000-000000000101" {
		t.Fatalf("provider restart allocated a replacement after a lost reply: posts=%d legacy=%d allocations=%d error=%v", posts, legacy, allocated, err)
	}
	stored, readErr := os.ReadFile(filepath.Join(fixture.dir, ".provider.key"))
	if readErr != nil || !bytes.Equal(seed, stored) || !bytes.Equal(seed, returnedSeed) {
		t.Fatal("provider restart rotated its durable registration key")
	}
	var record providerRegistrationTestRecord
	raw, err := os.ReadFile(filepath.Join(fixture.dir, ".provider.jwt.registration"))
	if err != nil || json.Unmarshal(raw, &record) != nil || record.ClientId != recovered.String() || record.DeviceId == "" {
		t.Fatal("provider credential escaped without retained server identity binding")
	}
}

// A completed refusal does not become availability. A transient reply can only
// retry the same retained request, then reaches the real authentication seam.
func TestProviderRegistrationRetriesOnlySameTransientOperation(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusNotFound, http.StatusForbidden, http.StatusBadRequest} {
		fixture := newProviderRegistrationFixture(t)
		fixture.statuses = []int{status}
		stop := errors.New("synthetic stop after transient replay")
		err := fixture.run(t.Context(), true, providerRegistrationHooks{afterAuthenticated: func(string, connect.Id, []byte) error { return stop }})
		posts, legacy, allocated, _ := fixture.counts()
		if status == http.StatusServiceUnavailable {
			if !errors.Is(err, stop) || posts != 2 || allocated != 1 || legacy != 0 {
				t.Fatal("provider transient retry lost its exact original operation", err)
			}
		} else if err == nil || errors.Is(err, stop) || posts != 1 || legacy != 0 {
			t.Fatal("provider retried a completed refusal or reached legacy allocation", status, err)
		}
	}
}

// A malformed successful HTTP reply retains the request and never installs a
// token, reaches serving readiness, or chooses another allocating route.
func TestProviderRegistrationMalformedReplyRetainsRecovery(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	fixture.malformed = true
	var authenticated atomic.Bool
	err := fixture.run(t.Context(), true, providerRegistrationHooks{afterAuthenticated: func(string, connect.Id, []byte) error { authenticated.Store(true); return nil }})
	posts, legacy, _, _ := fixture.counts()
	_, tokenErr := os.Stat(filepath.Join(fixture.dir, ".provider.jwt"))
	_, requestErr := os.Stat(filepath.Join(fixture.dir, ".provider.jwt.registration"))
	if err == nil || authenticated.Load() || posts != 1 || legacy != 0 || !errors.Is(tokenErr, os.ErrNotExist) || requestErr != nil {
		t.Fatal("malformed provider reply escaped its retained recovery boundary", err)
	}
}

// The shared key remains exclusively owned across another process's overlap
// and until the actual API join. After run returns, ordinary restart can own it.
func TestProviderRegistrationOwnerSpansApiJoin(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	stop := errors.New("synthetic authentication handoff stop")
	var joined atomic.Bool
	var earlyRelease atomic.Bool
	done := make(chan error, 1)
	go func() {
		done <- fixture.run(t.Context(), true, providerRegistrationHooks{
			afterAuthenticated: func(string, connect.Id, []byte) error { close(entered); <-release; return stop },
			afterApiJoined: func() {
				owner, err := clientauth.OpenProviderClientKey(t.Context(), filepath.Join(fixture.dir, ".provider.key"), clientauth.ProviderClientKeyOptions{})
				if err == nil {
					earlyRelease.Store(true)
					_ = owner.Close()
				}
				joined.Store(true)
			},
		})
	}()
	select {
	case <-entered:
	case err := <-done:
		close(release)
		t.Fatal("provider did not reach real authenticated handoff", err)
	case <-time.After(25 * time.Second):
		close(release)
		<-done
		t.Fatal("provider authenticated handoff did not complete")
	}
	duplicateErr := fixture.run(t.Context(), true, providerRegistrationHooks{afterAuthenticated: func(string, connect.Id, []byte) error { return stop }})
	close(release)
	err := <-done
	posts, legacy, allocated, refreshed := fixture.counts()
	if !errors.Is(err, stop) || duplicateErr == nil || errors.Is(duplicateErr, stop) || !joined.Load() || earlyRelease.Load() || posts != 1 || allocated != 1 || legacy != 0 || refreshed != 0 {
		t.Fatal("provider key ownership ended before all API users joined", err, duplicateErr)
	}
	owner, err := clientauth.OpenProviderClientKey(t.Context(), filepath.Join(fixture.dir, ".provider.key"), clientauth.ProviderClientKeyOptions{})
	if err != nil {
		t.Fatal("joined provider retained its key lock", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}

// Direct and two independently connected proxies get distinct operations;
// credential rotation and map order retain those operations and the shared key.
func TestProviderRegistrationSlotsSurviveCredentialAndOrderChanges(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	first := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.10:1080", Auth: &proxy.Auth{User: "synthetic-first", Password: "synthetic-password"}}
	second := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.11:1080"}
	stop := errors.New("synthetic proxy authentication handoff")
	var handoffLock sync.Mutex
	handoffs := map[string]int{}
	var handoffSeed []byte
	hooks := providerRegistrationHooks{afterAuthenticated: func(_ string, id connect.Id, seed []byte) error {
		handoffLock.Lock()
		defer handoffLock.Unlock()
		if handoffSeed == nil {
			handoffSeed = bytes.Clone(seed)
		} else if !bytes.Equal(handoffSeed, seed) {
			t.Error("provider slots did not retain their shared identity key")
		}
		handoffs[id.String()]++
		return stop
	}}
	if err := fixture.run(t.Context(), true, hooks, nil, first, second); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	posts, legacy, allocated, refreshed := fixture.counts()
	t.Logf("provider slot allocation: posts=%d legacy=%d allocated=%d physical_refreshes=%d handoffs=%v", posts, legacy, allocated, refreshed, handoffs)
	if posts != 3 || legacy != 0 || allocated != 3 || refreshed != 0 || len(handoffs) != 3 {
		t.Fatal("provider slots did not complete three original allocations")
	}
	paths := map[string]bool{}
	records := map[string]providerRegistrationTestRecord{}
	for _, member := range []*connect.ProxySettings{nil, first, second} {
		path, err := providerClientJwtPath(member)
		if err != nil || paths[path] {
			t.Fatal("distinct provider slots shared a credential path")
		}
		paths[path] = true
		raw, err := os.ReadFile(path + ".registration")
		var record providerRegistrationTestRecord
		if err != nil || json.Unmarshal(raw, &record) != nil || record.Scope.ClientSlot != providerRegistrationSlot(member) || record.Request.DeviceDescription != "provider" || record.ClientId == "" || record.DeviceId == "" || handoffs[record.ClientId] != 1 {
			t.Fatal("provider slot did not retain its exact stable identity")
		}
		records[path] = record
	}
	rotated := *first
	rotated.Auth = &proxy.Auth{User: "synthetic-rotated", Password: "synthetic-new-password"}
	if err := fixture.run(t.Context(), false, hooks, second, &rotated, nil); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	posts, legacy, allocated, refreshed = fixture.counts()
	refreshClients := fixture.refreshedClients()
	t.Logf("provider slot restart: posts=%d legacy=%d allocated=%d physical_refreshes=%d refreshed_clients=%v handoffs=%v", posts, legacy, allocated, refreshed, refreshClients, handoffs)
	if posts != 3 || legacy != 0 || allocated != 3 || refreshed < 3 || len(refreshClients) != 3 || len(handoffs) != 3 {
		t.Fatal("provider proxy credentials or ordering changed allocation identity")
	}
	for path, original := range records {
		raw, err := os.ReadFile(path + ".registration")
		var record providerRegistrationTestRecord
		if err != nil || json.Unmarshal(raw, &record) != nil || record != original || handoffs[original.ClientId] != 2 || refreshClients[original.ClientId] < 1 {
			t.Fatal("provider proxy restart changed its original request")
		}
	}
	seed, err := os.ReadFile(filepath.Join(fixture.dir, ".provider.key"))
	if err != nil || !bytes.Equal(seed, handoffSeed) {
		t.Fatal("provider slots did not retain their durable identity key")
	}
}

// Two configured entries that parse to the same effective proxy are rejected
// before workers, key creation or any HTTP allocation can begin.
func TestProviderRegistrationRejectsDuplicateEffectiveSlots(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	first := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.20:1080", Auth: &proxy.Auth{User: "synthetic-one"}}
	second := &connect.ProxySettings{Network: "tcp", Address: first.Address, Auth: &proxy.Auth{User: "synthetic-two"}}
	stop := errors.New("synthetic duplicate reached authentication")
	err := fixture.run(t.Context(), true, providerRegistrationHooks{afterAuthenticated: func(string, connect.Id, []byte) error { return stop }}, first, second)
	posts, legacy, _, _ := fixture.counts()
	_, keyErr := os.Stat(filepath.Join(fixture.dir, ".provider.key"))
	if err == nil || errors.Is(err, stop) || posts != 0 || legacy != 0 || !errors.Is(keyErr, os.ErrNotExist) {
		t.Fatal("duplicate effective provider slots reached identity allocation")
	}
}

// Legacy custody is refreshed without new allocation. Its durable marker makes
// later token loss explicit, even when new-operation permission is supplied.
func TestProviderRegistrationAdoptsLegacyAndRefusesLostCredential(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	keyPath := filepath.Join(fixture.dir, ".provider.key")
	clientPath := filepath.Join(fixture.dir, ".provider.jwt")
	seed := bytes.Repeat([]byte{17}, ed25519.SeedSize)
	if err := os.WriteFile(keyPath, seed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := clientauth.WriteToken(clientPath, providerRegistrationTestToken(t, "00000000-0000-0000-0000-000000000101", "legacy")); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("synthetic legacy refresh handoff")
	var handoffs atomic.Int32
	hooks := providerRegistrationHooks{afterAuthenticated: func(_ string, id connect.Id, _ []byte) error {
		if id.String() != "00000000-0000-0000-0000-000000000101" {
			t.Error("provider legacy adoption changed its authenticated client")
		}
		handoffs.Add(1)
		return stop
	}}
	if err := fixture.run(t.Context(), true, hooks); err == nil || errors.Is(err, stop) {
		t.Fatal("provider silently blessed an unproven legacy seed with new-create permission")
	}
	posts, legacy, allocated, refreshed := fixture.counts()
	if posts != 0 || legacy != 0 || allocated != 0 || refreshed != 0 {
		t.Fatal("provider unapproved legacy key adoption reached the API")
	}
	settings := fixture.settings(false)
	settings.adoptLegacyProviderKey = true
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := settings.run(context.WithValue(ctx, providerRegistrationHooksKey{}, hooks), &providerRefusedWriter{}); !errors.Is(err, stop) {
		t.Fatal("provider explicit legacy key adoption did not refresh its original credential", err)
	}
	posts, legacy, allocated, refreshed = fixture.counts()
	t.Logf("provider legacy adoption: posts=%d legacy=%d allocated=%d physical_refreshes=%d handoffs=%d", posts, legacy, allocated, refreshed, handoffs.Load())
	if posts != 0 || legacy != 0 || allocated != 0 || refreshed < 1 || handoffs.Load() != 1 || len(fixture.refreshedClients()) != 1 {
		t.Fatal("provider legacy adoption did not retain exactly one client without allocation")
	}
	refreshBaseline := refreshed
	marker, err := os.ReadFile(clientPath + ".registration.existing")
	if err != nil || len(marker) == 0 {
		t.Fatal("provider legacy adoption did not retain identity custody")
	}
	if err := os.Remove(clientPath); err != nil {
		t.Fatal(err)
	}
	err = fixture.run(t.Context(), true, hooks)
	posts, legacy, allocated, refreshed = fixture.counts()
	t.Logf("provider lost legacy credential: posts=%d legacy=%d allocated=%d physical_refreshes=%d baseline_refreshes=%d handoffs=%d", posts, legacy, allocated, refreshed, refreshBaseline, handoffs.Load())
	var refused *clientauth.RegistrationRefusedError
	if !errors.As(err, &refused) || refused.Code != "legacy_identity_requires_explicit_recovery" || errors.Is(err, stop) || posts != 0 || legacy != 0 || allocated != 0 || refreshed != refreshBaseline || handoffs.Load() != 1 {
		t.Fatal("provider lost legacy credential became a replacement allocation", err)
	}
	retained, markerErr := os.ReadFile(clientPath + ".registration.existing")
	retainedSeed, seedErr := os.ReadFile(keyPath)
	_, clientErr := os.Stat(clientPath)
	_, recordErr := os.Stat(clientPath + ".registration")
	if markerErr != nil || !bytes.Equal(marker, retained) || seedErr != nil || !bytes.Equal(seed, retainedSeed) || !errors.Is(clientErr, os.ErrNotExist) || !errors.Is(recordErr, os.ErrNotExist) {
		t.Fatal("provider lost legacy credential rewrote its original custody")
	}
}

// After a real versioned request, losing the public marker cannot bless a
// changed seed under either flag. The original request remains untouched.
func TestProviderRegistrationLostKeyMarkerRequiresRecovery(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	fixture.onCommit = cancel
	if err := fixture.run(ctx, true, providerRegistrationHooks{}); !errors.Is(err, context.Canceled) {
		cancel()
		t.Fatal(err)
	}
	cancel()
	requestPath := filepath.Join(fixture.dir, ".provider.jwt.registration")
	original, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(fixture.dir, ".provider.key.identity")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.dir, ".provider.key"), bytes.Repeat([]byte{29}, ed25519.SeedSize), 0600); err != nil {
		t.Fatal(err)
	}
	for _, adopt := range []bool{false, true} {
		settings := fixture.settings(!adopt)
		settings.adoptLegacyProviderKey = adopt
		if err := settings.run(t.Context(), &providerRefusedWriter{}); err == nil {
			t.Fatal("provider marker loss silently adopted a replacement key")
		}
		_, markerErr := os.Stat(filepath.Join(fixture.dir, ".provider.key.identity"))
		after, err := os.ReadFile(requestPath)
		posts, legacy, _, _ := fixture.counts()
		if !errors.Is(markerErr, os.ErrNotExist) || err != nil || !bytes.Equal(original, after) || posts != 1 || legacy != 0 {
			t.Fatal("provider marker-loss recovery rewrote original key custody")
		}
	}
}

// A mixed cause cannot be relabeled transient by finding just one matching
// leaf. Cancellation, unsupported endpoints and local failures remain hard.
func TestProviderRegistrationRetryPreservesAllCauses(t *testing.T) {
	unavailable := &sdk.NetworkClientRegistrationUnavailableError{}
	for _, err := range []error{unavailable, fmt.Errorf("synthetic transport: %w", unavailable), errors.Join(unavailable, &clientauth.RegistrationRefreshUnavailableError{})} {
		if !providerRegistrationRetryable(err, 0) {
			t.Fatal("provider lost a typed transient availability verdict")
		}
	}
	for _, err := range []error{nil, context.Canceled, context.DeadlineExceeded, &sdk.NetworkClientRegistrationUnsupportedError{Status: 404}, errors.Join(unavailable, errors.New("synthetic custody failure")), errors.Join(unavailable, context.Canceled)} {
		if providerRegistrationRetryable(err, 0) {
			t.Fatal("provider retry ignored a hard cause in its owned result")
		}
	}
}

// A refresh may change bearer bytes, but changing the owned principal or client
// must cancel before replacing the credential on disk.
func TestProviderRegistrationRefreshPreservesOriginalIdentity(t *testing.T) {
	for _, fault := range []string{"valid", "client", "principal", "roles"} {
		changed := fault != "valid"
		ctx, cancel := context.WithCancel(t.Context())
		path := filepath.Join(providerRegistrationPrivateDir(t), "client.jwt")
		initial := providerRegistrationTestToken(t, "00000000-0000-0000-0000-000000000101", "initial")
		refreshed := providerRegistrationTestToken(t, "00000000-0000-0000-0000-000000000101", "refreshed")
		if fault == "client" {
			refreshed = providerRegistrationTestToken(t, "00000000-0000-0000-0000-000000000102", "changed")
		} else if fault == "principal" || fault == "roles" {
			claims := gojwt.MapClaims{}
			if _, _, err := gojwt.NewParser().ParseUnverified(refreshed, claims); err != nil {
				t.Fatal(err)
			}
			if fault == "principal" {
				claims["principal"] = "synthetic-other-owner"
			} else {
				claims["roles"] = []string{"synthetic-other-role"}
			}
			var err error
			refreshed, err = gojwt.NewWithClaims(gojwt.SigningMethodNone, claims).SignedString(gojwt.UnsafeAllowNoneSignatureType)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := clientauth.WriteToken(path, initial); err != nil {
			t.Fatal(err)
		}
		owner := newProviderDiagnosticTestOwner(t, &providerRefusedWriter{})
		callbacks := &providerAuthenticationCallbacks{diagnostics: owner, clientJwtPath: path, cancel: cancel}
		bound := &providerBoundRefresh{original: initial, callbacks: callbacks}
		bound.JwtRefreshed(refreshed)
		stored, err := clientauth.ReadToken(path)
		if changed {
			if err != nil || stored != initial || callbacks.failure() == nil || ctx.Err() == nil {
				t.Fatal("provider persisted a refresh for a different owned identity")
			}
		} else if err != nil || stored != refreshed || callbacks.failure() != nil || ctx.Err() != nil {
			t.Fatal("provider rejected a stable-identity credential refresh")
		}
		cancel()
	}
}

// The actual SDK notices carry a generation. Equal-byte replacement logins
// invalidate old notices just as different-byte logins do.
func TestProviderRegistrationIntegrityNoticeHonorsCurrentGeneration(t *testing.T) {
	for _, stale := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		owner := newProviderDiagnosticTestOwner(t, &providerRefusedWriter{})
		initial := providerRegistrationTestToken(t, "00000000-0000-0000-0000-000000000101", "initial")
		api := providerDiagnosticTestApi(t, ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"by_jwt":"synthetic-malformed-token"}`))
		}))
		callbacks := &providerAuthenticationCallbacks{diagnostics: owner, cancel: cancel}
		bound := &providerBoundRefresh{original: initial, callbacks: callbacks}
		delivered := make(chan struct{})
		var once sync.Once
		sub := api.AddClientRefreshIntegrityListener(clientauth.ClientRefreshIntegrityListenerFunc(func(notice *sdk.ClientRefreshIntegrityNotice) {
			once.Do(func() {
				if stale {
					api.SetByJwt(initial)
				}
				bound.ClientRefreshInvalid(notice)
				close(delivered)
			})
		}))
		api.SetByJwt(initial)
		api.StartJwtRefresh()
		select {
		case <-delivered:
		case <-time.After(15 * time.Second):
			cancel()
			t.Fatal("provider SDK integrity notice did not reach its owner")
		}
		if stale {
			if ctx.Err() != nil || callbacks.failure() != nil {
				t.Fatal("stale provider integrity notice canceled a replacement generation")
			}
		} else if ctx.Err() == nil || callbacks.failure() == nil {
			t.Fatal("current provider integrity notice left invalid authentication live")
		}
		cancel()
		if err := api.CloseAndWait(context.Background()); err != nil {
			t.Fatal(err)
		}
		sub.Close()
	}
}

// The adjacent diagnostic regression now waits at a typed versioned HTTP
// outage, after explicit key/request admission. Cancellation keeps its cause
// and joins API/output ownership. Missing custody itself is no longer retried.
func TestProviderDiagnosticsRunAuthenticationWaitCancels(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	fixture.status = http.StatusServiceUnavailable
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var closed atomic.Bool
	ctx = context.WithValue(ctx, providerDiagnosticHooksKey{}, providerDiagnosticHooks{afterClose: func(_ *providerDiagnostics, _ error) { closed.Store(true) }})
	sink := &providerDiagnosticRecorder{records: make(chan []byte, 16)}
	result := make(chan error, 1)
	go func() { result <- fixture.settings(true).run(ctx, sink) }()
	joined := false
	defer func() {
		cancel()
		if !joined {
			select {
			case <-result:
			case <-time.After(15 * time.Second):
				t.Error("provider authentication fixture owner did not join")
			}
		}
	}()
	found := false
	for range 2 {
		raw := sink.next(t)
		if bytes.Contains(raw, []byte(`"event":"retry_wait"`)) {
			if !bytes.Contains(raw, []byte(`"provider":0`)) || !bytes.Contains(raw, []byte(`"provider_known":true`)) {
				t.Fatal("authentication wait lost provider attribution")
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("actual authentication retry did not publish its closed event")
	}
	cancel()
	select {
	case err := <-result:
		joined = true
		if !errors.Is(err, context.Canceled) || !closed.Load() {
			t.Fatal("authentication cancellation did not join actual output lifecycle")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("authentication wait held provider cancellation")
	}
}

// A real unavailable attempt releases its slot lock but retains the physical
// key-directory owner. Replacement paths cannot manufacture another request.
func TestProviderRegistrationRetryRejectsReplacedDirectory(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	fixture.status = http.StatusServiceUnavailable
	retained := filepath.Join(providerRegistrationPrivateDir(t), "retained")
	var original []byte
	var replaced bool
	err := fixture.run(t.Context(), true, providerRegistrationHooks{afterAttempt: func(err error) error {
		if replaced {
			return nil
		}
		if !providerRegistrationRetryable(err, 0) {
			return errors.New("synthetic directory barrier did not follow unavailable HTTP")
		}
		var readErr error
		original, readErr = os.ReadFile(filepath.Join(fixture.dir, ".provider.jwt.registration"))
		if readErr != nil {
			return readErr
		}
		if err := os.Rename(fixture.dir, retained); err != nil {
			return err
		}
		if err := os.Mkdir(fixture.dir, 0700); err != nil {
			return err
		}
		for _, name := range []string{".provider.key", ".provider.key.identity"} {
			raw, err := os.ReadFile(filepath.Join(retained, name))
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(fixture.dir, name), raw, 0600); err != nil {
				return err
			}
		}
		if err := clientauth.WriteToken(filepath.Join(fixture.dir, "jwt"), providerRegistrationTestToken(t, "", "replacement-bootstrap")); err != nil {
			return err
		}
		replaced = true
		return nil
	}})
	posts, legacy, allocated, _ := fixture.counts()
	_, newRequestErr := os.Stat(filepath.Join(fixture.dir, ".provider.jwt.registration"))
	_, newLockErr := os.Stat(filepath.Join(fixture.dir, ".provider.jwt.registration.lock"))
	retainedRequest, readErr := os.ReadFile(filepath.Join(retained, ".provider.jwt.registration"))
	if !replaced || err == nil || posts != 1 || legacy != 0 || allocated != 1 || !errors.Is(newRequestErr, os.ErrNotExist) || !errors.Is(newLockErr, os.ErrNotExist) || readErr != nil || !bytes.Equal(original, retainedRequest) {
		t.Fatal("provider retry consulted replacement custody instead of its original directory", err)
	}
}

// Real refresh/logout callbacks use the same descriptor-bound custody. A
// replacement namespace cannot receive a renewed token or rejection tombstone.
func TestProviderRegistrationCallbacksRetainPhysicalCustody(t *testing.T) {
	for _, action := range []string{"refresh", "logout"} {
		for _, replace := range []bool{false, true} {
			dir := providerRegistrationPrivateDir(t)
			path := filepath.Join(dir, ".provider.jwt")
			keyOwner, err := clientauth.OpenProviderClientKey(t.Context(), filepath.Join(dir, ".provider.key"), clientauth.ProviderClientKeyOptions{AllowCreate: true})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = keyOwner.Close() })
			initial := providerRegistrationTestToken(t, "00000000-0000-0000-0000-000000000101", "original")
			refreshed := providerRegistrationTestToken(t, "00000000-0000-0000-0000-000000000101", "refreshed")
			replacement := providerRegistrationTestToken(t, "00000000-0000-0000-0000-000000000102", "replacement")
			if err := clientauth.WriteToken(path, initial); err != nil {
				t.Fatal(err)
			}
			originalPath := path
			if replace {
				retained := filepath.Join(providerRegistrationPrivateDir(t), "retained")
				if err := os.Rename(dir, retained); err != nil {
					t.Fatal(err)
				}
				originalPath = filepath.Join(retained, ".provider.jwt")
				if err := clientauth.WriteToken(path, replacement); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			output := newProviderDiagnosticTestOwner(t, &providerRefusedWriter{})
			callbacks := &providerAuthenticationCallbacks{diagnostics: output, clientJwtPath: path, networkJwtPath: filepath.Join(dir, "jwt"), cancel: cancel, custody: keyOwner}
			if action == "refresh" {
				(&providerBoundRefresh{original: initial, callbacks: callbacks}).JwtRefreshed(refreshed)
			} else {
				callbacks.AuthLogout()
			}
			stored, readErr := clientauth.ReadToken(path)
			marker, markerErr := clientauth.ReadToken(path + ".rejected")
			if replace {
				original, originalErr := clientauth.ReadToken(originalPath)
				if ctx.Err() == nil || callbacks.failure() == nil || readErr != nil || stored != replacement || !errors.Is(markerErr, os.ErrNotExist) || originalErr != nil || original != initial {
					t.Fatal("provider callback wrote through a replacement custody directory", action)
				}
			} else if action == "refresh" {
				if callbacks.failure() != nil || ctx.Err() != nil || readErr != nil || stored != refreshed {
					t.Fatal("provider owned refresh did not persist its original client")
				}
			} else if callbacks.failure() != nil || ctx.Err() == nil || !errors.Is(readErr, os.ErrNotExist) || markerErr != nil || marker != "blocked" {
				t.Fatal("provider owned rejection did not retain its sticky tombstone")
			}
			cancel()
			if err := keyOwner.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}
