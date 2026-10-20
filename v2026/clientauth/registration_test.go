//go:build linux || darwin

package clientauth

// Actual private files, fsync/rename ownership and configured HTTP calls
// exercise request and credential crash boundaries. Tokens are synthetic.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// Per-fixture state models one server binding; the server package separately
// qualifies actual PostgreSQL allocation, concurrent dedup and tombstones.
type registrationTestFixture struct {
	test          *testing.T
	stateLock     sync.Mutex
	api           *sdk.Api
	scope         RegistrationScope
	networkPath   string
	clientPath    string
	request       []byte
	posts         int
	legacy        int
	status        int
	refreshStatus int
	beforeRefresh func(context.Context)
	responseId    string
	onCommit      func()
}

func registrationTestToken(t *testing.T, clientId, marker string) string {
	t.Helper()
	claims := gojwt.MapClaims{"network_id": "00000000-0000-0000-0000-000000000301", "user_id": "00000000-0000-0000-0000-000000000401", "roles": []string{"operator", "validator"}, "principal": "synthetic-owner", "marker": marker}
	if clientId != "" {
		claims["client_id"], claims["device_id"] = clientId, testDeviceId
	}
	token, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, claims).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func newRegistrationTestFixture(t *testing.T) *registrationTestFixture {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	self := &registrationTestFixture{test: t, networkPath: filepath.Join(dir, "network.jwt"), clientPath: filepath.Join(dir, "client.jwt"), responseId: testClientId}
	server := httptest.NewServer(http.HandlerFunc(self.serveHttp))
	t.Cleanup(server.Close)
	api, closeApi := testApi(t, server.URL)
	t.Cleanup(closeApi)
	self.api = api
	endpoint, err := api.NetworkClientRegistrationEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	self.scope = RegistrationScope{Endpoint: endpoint, DeploymentId: "synthetic-deployment", ChainId: 964, GenesisHash: "0x" + strings.Repeat("12", 32), Netuid: 71, ValidatorId: 1, OperatorNoId: 9, ClientKey: "0x" + strings.Repeat("34", 32)}
	if err := WriteToken(self.networkPath, registrationTestToken(t, "", "original")); err != nil {
		t.Fatal(err)
	}
	return self
}

// Consume/close finite request input before cancellation barriers. An actual
// request must already have its identical original bytes on private disk.
func (self *registrationTestFixture) serveHttp(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024))
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil {
		self.test.Error("registration fixture could not consume request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if r.URL.Path == "/hello" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.URL.Path == "/auth/refresh" {
		self.stateLock.Lock()
		status := self.refreshStatus
		before := self.beforeRefresh
		self.stateLock.Unlock()
		if before != nil {
			before(r.Context())
		}
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"by_jwt": registrationTestToken(self.test, testClientId, "refresh")})
		return
	}
	self.stateLock.Lock()
	if r.URL.Path == "/network/auth-client" {
		self.legacy++
		self.stateLock.Unlock()
		self.test.Error("durable registration reached non-idempotent legacy allocation")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if r.URL.Path != "/network/register-client-v1" || r.Method != http.MethodPost {
		self.stateLock.Unlock()
		self.test.Error("registration used an unrelated route")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	self.posts++
	status, responseId, committed := self.status, self.responseId, self.onCommit
	self.onCommit = nil
	if self.request == nil {
		self.request = bytes.Clone(body)
	} else if !bytes.Equal(self.request, body) {
		self.test.Error("recovery changed the server's bound request")
	}
	self.stateLock.Unlock()
	raw, err := os.ReadFile(self.clientPath + ".registration")
	var record registrationRecord
	if err != nil || json.Unmarshal(raw, &record) != nil {
		self.test.Error("registration request was sent before durable custody")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	want, err := sdk.EncodeNetworkClientRegistration(&record.Request)
	if err != nil || !bytes.Equal(body, want) {
		self.test.Error("HTTP request differs from original durable bytes")
	}
	anchor, anchorErr := os.ReadFile(self.clientPath + ".registration.started")
	expectedAnchor, expectedErr := registrationOperationAnchor(&record)
	if anchorErr != nil || expectedErr != nil || !bytes.Equal(anchor, expectedAnchor) {
		self.test.Error("registration request escaped before its durable first-send anchor")
	}
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
	digest := sha256.Sum256(body)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"schema": sdk.NetworkClientRegistrationSchema, "registration_id": record.Request.RegistrationId, "request_sha256": hex.EncodeToString(digest[:]), "client_id": responseId, "device_id": testDeviceId, "by_client_jwt": registrationTestToken(self.test, responseId, "registered")})
}

// A token left behind by an unpersisted revocation is never enough to regain
// production API readiness during an outage. Its current authority must reply.
func TestClientRegistrationRefreshRequiresCurrentAdmission(t *testing.T) {
	fixture := newRegistrationTestFixture(t)
	original, err := fixture.register(t.Context(), true, registrationHooks{})
	if err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	fixture.refreshStatus = http.StatusServiceUnavailable
	fixture.stateLock.Unlock()
	token, err := fixture.register(t.Context(), false, registrationHooks{})
	var unavailable *RegistrationRefreshUnavailableError
	stored, readErr := ReadToken(fixture.clientPath)
	if !errors.As(err, &unavailable) || token != "" || readErr != nil || stored != original || fixture.postCount() != 1 {
		t.Fatal("unknown original client authority became API readiness")
	}
	fixture.stateLock.Lock()
	fixture.refreshStatus = http.StatusUnauthorized
	fixture.stateLock.Unlock()
	_, err = fixture.register(t.Context(), false, registrationHooks{})
	var refused *RegistrationRefusedError
	if !errors.As(err, &refused) || refused.Code != "client_revoked" {
		t.Fatal("confirmed revocation did not remain explicit")
	}
	_, err = fixture.register(t.Context(), true, registrationHooks{})
	if !errors.As(err, &refused) || fixture.postCount() != 1 {
		t.Fatal("creation flag renewed a revoked operation")
	}
}

type registrationDeadlineTestContext struct{ context.Context }

// A synthetic owned deadline must not expose the embedded cancelCtx's private
// key: context.Cause would otherwise rewrite it to caller cancellation.
func (self registrationDeadlineTestContext) Value(key any) any {
	return context.WithoutCancel(self.Context).Value(key)
}

func (self registrationDeadlineTestContext) Err() error {
	if self.Context.Err() != nil {
		return context.DeadlineExceeded
	}
	return nil
}

// The actual HTTP read is in flight when its operation owner expires. Exhaustion
// leaves a typed wait and original credentials; caller cancellation stays distinct.
func TestClientRegistrationExpiredRefreshRetainsReadWait(t *testing.T) {
	for _, deadline := range []bool{true, false} {
		fixture := newRegistrationTestFixture(t)
		original, err := fixture.register(t.Context(), true, registrationHooks{})
		if err != nil {
			t.Fatal(err)
		}
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		fixture.stateLock.Lock()
		fixture.beforeRefresh = func(ctx context.Context) {
			once.Do(func() { close(entered) })
			select {
			case <-ctx.Done():
			case <-release:
			}
		}
		fixture.stateLock.Unlock()
		base, cancel := context.WithCancel(t.Context())
		var ctx context.Context = base
		if deadline {
			ctx = registrationDeadlineTestContext{Context: base}
		}
		done := make(chan error, 1)
		go func() { _, err := fixture.register(ctx, false, registrationHooks{}); done <- err }()
		select {
		case <-entered:
		case err := <-done:
			cancel()
			close(release)
			t.Fatalf("refresh did not reach the actual HTTP barrier: %v", err)
		case <-t.Context().Done():
			cancel()
			close(release)
			<-done
			t.Fatal(t.Context().Err())
		}
		cancel()
		err = <-done
		close(release)
		var unavailable *RegistrationRefreshUnavailableError
		if errors.As(err, &unavailable) != deadline {
			t.Fatalf("refresh exhaustion/cancellation lost its owned verdict: deadline=%v error=%v", deadline, err)
		}
		stored, readErr := ReadToken(fixture.clientPath)
		if readErr != nil || stored != original || fixture.postCount() != 1 {
			t.Fatal("interrupted refresh changed original credential or allocation")
		}
	}
}

func (self *registrationTestFixture) register(ctx context.Context, allowCreate bool, hooks registrationHooks) (string, error) {
	token, _, err := loadOrRegisterClientJwt(ctx, self.api, self.networkPath, self.clientPath, "synthetic validator", self.scope, allowCreate, hooks)
	return token, err
}

// The handler and test share only copied values under the fixture owner.
func (self *registrationTestFixture) postCount() int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.posts
}
func (self *registrationTestFixture) legacyCount() int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.legacy
}
func (self *registrationTestFixture) requestBytes() []byte {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return bytes.Clone(self.request)
}

// A process failure after request fsync precedes all server mutation. Token
// rotation changes bearer bytes only; restart replays the same opaque operation.
func TestClientRegistrationPersistsRequestBeforeMutation(t *testing.T) {
	fixture := newRegistrationTestFixture(t)
	canary := errors.New("synthetic failure after durable request")
	if _, err := fixture.register(t.Context(), true, registrationHooks{afterRequest: func() error { return canary }}); !errors.Is(err, canary) {
		t.Fatalf("actual request handoff did not stop before mutation: %v", err)
	}
	before, err := os.ReadFile(fixture.clientPath + ".registration")
	if err != nil || fixture.postCount() != 0 {
		t.Fatal("request failure lost original durable operation or sent a mutation")
	}
	var original registrationRecord
	if err := json.Unmarshal(before, &original); err != nil {
		t.Fatal(err)
	}
	if err := WriteToken(fixture.networkPath, registrationTestToken(t, "", "renewed-bearer")); err != nil {
		t.Fatal(err)
	}
	token, err := fixture.register(t.Context(), false, registrationHooks{})
	if err != nil || token == "" || fixture.postCount() != 1 || fixture.legacyCount() != 0 {
		t.Fatalf("restart did not recover the original operation: %v", err)
	}
	after, err := os.ReadFile(fixture.clientPath + ".registration")
	var bound registrationRecord
	if err != nil || json.Unmarshal(after, &bound) != nil || bound.Request != original.Request || !bound.Principal.equal(original.Principal) || bound.ClientId != testClientId || bound.DeviceId != testDeviceId {
		t.Fatal("token refresh or restart regenerated operation identity")
	}
}

// A stored server binding survives a failure before credential installation.
// The next owner must recover the same client/device rather than mint another.
func TestClientRegistrationBindingSurvivesCredentialHandoffFailure(t *testing.T) {
	fixture := newRegistrationTestFixture(t)
	canary := errors.New("synthetic failure after durable server binding")
	if _, err := fixture.register(t.Context(), true, registrationHooks{afterBinding: func() error { return canary }}); !errors.Is(err, canary) {
		t.Fatalf("actual binding handoff fault was lost: %v", err)
	}
	if _, err := os.Stat(fixture.clientPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("credential escaped before durable handoff completed")
	}
	before, err := os.ReadFile(fixture.clientPath + ".registration")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.register(t.Context(), false, registrationHooks{}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(fixture.clientPath + ".registration")
	if err != nil || !bytes.Equal(before, after) || fixture.postCount() != 2 {
		t.Fatal("credential recovery rewrote original binding or skipped exact replay")
	}
	if _, err := fixture.register(t.Context(), false, registrationHooks{}); err != nil || fixture.postCount() != 2 {
		t.Fatalf("known credential restart attempted allocation: %v", err)
	}
}

func TestClientRegistrationLostPhysicalReplyRetainsOriginalOperation(t *testing.T) {
	fixture := newRegistrationTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.stateLock.Lock()
	fixture.onCommit = cancel
	fixture.stateLock.Unlock()
	if _, err := fixture.register(ctx, true, registrationHooks{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost physical reply did not preserve its cancellation: %v", err)
	}
	before, err := os.ReadFile(fixture.clientPath + ".registration")
	if err != nil {
		t.Fatal(err)
	}
	var original registrationRecord
	if json.Unmarshal(before, &original) != nil || original.ClientId != "" {
		t.Fatal("unknown reply manufactured a known server identity")
	}
	if _, err := fixture.register(t.Context(), false, registrationHooks{}); err != nil {
		t.Fatal(err)
	}
	want, _ := sdk.EncodeNetworkClientRegistration(&original.Request)
	if !bytes.Equal(want, fixture.requestBytes()) || fixture.legacyCount() != 0 {
		t.Fatal("lost physical reply selected a replacement operation")
	}
}

func TestClientRegistrationRejectsChangedScopeAndBoundIdentity(t *testing.T) {
	fixture := newRegistrationTestFixture(t)
	canary := errors.New("synthetic credential handoff interruption")
	if _, err := fixture.register(t.Context(), true, registrationHooks{afterBinding: func() error { return canary }}); !errors.Is(err, canary) {
		t.Fatal(err)
	}
	before, err := os.ReadFile(fixture.clientPath + ".registration")
	if err != nil {
		t.Fatal(err)
	}
	fixture.scope.OperatorNoId++
	if _, err := fixture.register(t.Context(), true, registrationHooks{}); err == nil || fixture.postCount() != 1 {
		t.Fatal("changed creation scope reused or replaced an original allocation")
	}
	fixture.scope.OperatorNoId--
	fixture.stateLock.Lock()
	fixture.responseId = "00000000-0000-0000-0000-000000000501"
	fixture.stateLock.Unlock()
	if _, err := fixture.register(t.Context(), false, registrationHooks{}); err == nil || fixture.postCount() != 2 {
		t.Fatal("completed replay changed a bound server identity")
	}
	after, err := os.ReadFile(fixture.clientPath + ".registration")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected identity changed durable ownership")
	}
}

func TestClientRegistrationUnsupportedAndLegacyLossStayExplicit(t *testing.T) {
	fixture := newRegistrationTestFixture(t)
	if _, err := fixture.register(t.Context(), false, registrationHooks{}); err == nil || fixture.postCount() != 0 {
		t.Fatal("retained legacy credential loss guessed another client")
	}
	fixture.stateLock.Lock()
	fixture.status = http.StatusNotFound
	fixture.stateLock.Unlock()
	_, err := fixture.register(t.Context(), true, registrationHooks{})
	var unsupported *sdk.NetworkClientRegistrationUnsupportedError
	if !errors.As(err, &unsupported) || fixture.legacyCount() != 0 || fixture.postCount() != 1 {
		t.Fatalf("unsupported server lost explicit capability refusal: %v", err)
	}
	if _, err := os.Stat(fixture.clientPath + ".registration"); err != nil {
		t.Fatal("unsupported server erased original operation")
	}
}

func TestClientRegistrationPrivateCustodyRejectsLinksAndConcurrentOwner(t *testing.T) {
	fixture := newRegistrationTestFixture(t)
	owner, err := openRegistrationStore(fixture.clientPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.register(t.Context(), true, registrationHooks{}); err == nil || fixture.postCount() != 0 {
		t.Fatal("two owners acquired one registration operation")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fixture.networkPath, fixture.clientPath+".registration"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.register(t.Context(), true, registrationHooks{}); err == nil || fixture.postCount() != 0 {
		t.Fatal("registration followed a substituted private file")
	}
}

// Losing the main request after a possible send never grants another opaque
// operation, including a first launch that has not created any native intent.
func TestClientRegistrationMissingSentCustodyRefusesNewAllocation(t *testing.T) {
	fixture := newRegistrationTestFixture(t)
	canary := errors.New("synthetic failure after durable bound identity")
	if _, err := fixture.register(t.Context(), true, registrationHooks{afterBinding: func() error { return canary }}); !errors.Is(err, canary) {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.clientPath + ".registration"); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.register(t.Context(), true, registrationHooks{})
	var refused *RegistrationRefusedError
	if !errors.As(err, &refused) || refused.Code != "registration_custody_missing_after_send" || fixture.postCount() != 1 {
		t.Fatalf("missing sent custody manufactured another first launch: %v", err)
	}
}

// A creation-enabled configuration must not turn loss of an already observed
// legacy client token into another allocation. No server identity is guessed.
func TestClientRegistrationExistingIdentityLossRefusesCreation(t *testing.T) {
	fixture := newRegistrationTestFixture(t)
	if err := WriteToken(fixture.clientPath, registrationTestToken(t, testClientId, "existing-client")); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.register(t.Context(), true, registrationHooks{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fixture.clientPath + ".registration.existing"); err != nil {
		t.Fatal("existing identity was not durably retained before refresh")
	}
	if err := os.Remove(fixture.clientPath); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.register(t.Context(), true, registrationHooks{})
	var refused *RegistrationRefusedError
	if !errors.As(err, &refused) || refused.Code != "legacy_identity_requires_explicit_recovery" || fixture.postCount() != 0 {
		t.Fatalf("legacy token loss manufactured a new allocation: %v", err)
	}
}

// Missing data is not a wildcard. Every error-tree leaf must be transient;
// complete response, cancellation and local custody failures remain hard.
func TestClientRefreshRequiresPureTypedUnavailability(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "deadline", err: context.DeadlineExceeded, want: true},
		{name: "cancelled", err: context.Canceled},
		{name: "mixed", err: errors.Join(context.DeadlineExceeded, errors.New("synthetic integrity"))},
		{name: "response", err: &sdk.ClientControlResponseError{}},
		{name: "mixed response", err: errors.Join(context.DeadlineExceeded, &sdk.ClientControlResponseError{})},
		{name: "HTTP500", err: &connect.HttpStatusError{StatusCode: http.StatusInternalServerError}, want: true},
		{name: "mixedHTTP500", err: errors.Join(&connect.HttpStatusError{StatusCode: http.StatusInternalServerError}, &sdk.ClientControlResponseError{})},
	} {
		if got := retryableClientRefreshError(test.err); got != test.want {
			t.Errorf("%s: unavailable=%t want=%t", test.name, got, test.want)
		}
	}
}

// Physical unavailable refresh preserves the old credential; a complete bad
// response remains an error without installing or deleting any token.
func TestClientRefreshPhysicalResponsesPreserveCustody(t *testing.T) {
	for _, sample := range []struct {
		status  int
		raw     string
		success bool
	}{
		{status: http.StatusInternalServerError, raw: "synthetic outage", success: true},
		{status: http.StatusOK, raw: "null"},
		{status: http.StatusOK, raw: "{"},
		{status: http.StatusOK, raw: `{"by_jwt":"synthetic","error":{"message":"synthetic refusal"}}`},
		{status: http.StatusOK, raw: `{"error":{"message":"first","message":"second"}}`},
	} {
		original := testClientJwt(t, "physical-refresh")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hello" {
				w.WriteHeader(http.StatusOK)
				return
			}
			if r.URL.Path != "/auth/refresh" {
				t.Error("refresh allocated another identity")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(sample.status)
			_, _ = w.Write([]byte(sample.raw))
		}))
		api, closeApi := testApi(t, server.URL)
		path := filepath.Join(t.TempDir(), "client.jwt")
		if err := WriteToken(path, original); err != nil {
			t.Fatal(err)
		}
		_, _, err := LoadOrCreateClientJwt(t.Context(), api, path+".network", path, "synthetic")
		closeApi()
		server.Close()
		stored, readErr := ReadToken(path)
		if (err == nil) != sample.success || readErr != nil || stored != original {
			t.Fatalf("completed refresh verdict changed custody or readiness: status=%d error=%v", sample.status, err)
		}
		if _, err := os.Stat(path + ".rejected"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("unconfirmed rejection deleted or revoked the credential")
		}
	}
}

func TestClientRefreshRejectsChangedIdentityAndNull(t *testing.T) {
	for _, changed := range []bool{false, true} {
		original := testClientJwt(t, "original")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hello" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if changed {
				_ = json.NewEncoder(w).Encode(map[string]string{"by_jwt": registrationTestToken(t, "00000000-0000-0000-0000-000000000501", "changed")})
			} else {
				_, _ = w.Write([]byte("null"))
			}
		}))
		api, closeApi := testApi(t, server.URL)
		path := filepath.Join(t.TempDir(), "client.jwt")
		if err := WriteToken(path, original); err != nil {
			t.Fatal(err)
		}
		_, _, err := LoadOrCreateClientJwt(t.Context(), api, filepath.Join(filepath.Dir(path), "missing-network.jwt"), path, "synthetic")
		closeApi()
		server.Close()
		stored, readErr := ReadToken(path)
		if err == nil || readErr != nil || stored != original {
			t.Fatalf("changed=%t refresh replaced original ownership: %v", changed, err)
		}
	}
}
