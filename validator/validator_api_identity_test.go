//go:build linux || darwin

// The validator's api identity: every request to an installed api origin
// carries the validator's bearer and its SN25 rate class token, and no other
// request carries either.
package validator

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/protocol"
)

// A fake operator api: it mints a class token for a consent that verifies for
// the bearer's session and replica, and records the headers of every other
// request.
type validatorAPIIdentityTestAPI struct {
	server      *httptest.Server
	replicaNoID uint64
	mints       atomic.Int64
	failMints   atomic.Bool
	// when set, a mint answers only once this closes
	blockMints atomic.Pointer[chan struct{}]
	lock       sync.Mutex
	seen       []http.Header
}

func newValidatorAPIIdentityTestAPI(t *testing.T, replicaNoID uint64, now func() time.Time) *validatorAPIIdentityTestAPI {
	t.Helper()
	self := &validatorAPIIdentityTestAPI{replicaNoID: replicaNoID}
	self.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == validatorClassMintPath {
			self.mints.Add(1)
			if block := self.blockMints.Load(); block != nil {
				select {
				case <-*block:
				case <-r.Context().Done():
					return
				}
			}
			credential, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			intent, err := protocol.VerifyValidatorClassHeader(r.Header.Get(protocol.ValidatorClassIntentHeader))
			sessionHash, hashErr := protocol.ValidatorAttemptUploadSessionHash(credential)
			if self.failMints.Load() || !ok || err != nil || hashErr != nil || intent.SessionHash != sessionHash || intent.ReplicaNoID != self.replicaNoID {
				http.Error(w, "403 Validator staging authority refused.", http.StatusForbidden)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "class-for-" + credential, "expires_at": now().Add(4 * time.Minute).UTC()})
			return
		}
		self.lock.Lock()
		self.seen = append(self.seen, r.Header.Clone())
		self.lock.Unlock()
	}))
	t.Cleanup(self.server.Close)
	return self
}

func (self *validatorAPIIdentityTestAPI) lastHeaders() http.Header {
	self.lock.Lock()
	defer self.lock.Unlock()
	if len(self.seen) == 0 {
		return nil
	}
	return self.seen[len(self.seen)-1]
}

// The validator's signer for the upload fixture's real activation.
func validatorAPIIdentityTestSigner(t *testing.T, replicaNoID uint64) *validatorAttemptUploadSigner {
	t.Helper()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	signer, err := newValidatorAttemptUploadSigner(fixture.authority.Expected, replicaNoID, 25, ed25519.NewKeyFromSeed(append([]byte{0x41}, make([]byte, 31)...)))
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// Class tokens that already hold token, live for an hour, so no mint runs.
func validatorClassTestTokens(t *testing.T, token string) *validatorClassTokens {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	return &validatorClassTokens{ctx: ctx, cancel: cancel, credential: func() string { return "" }, now: time.Now, mintWait: validatorClassMintWait,
		token: token, expires: time.Now().Add(time.Hour)}
}

func validatorAPIIdentityTestGet(t *testing.T, url string, authorization string) {
	t.Helper()
	client := &http.Client{Transport: validatorAPITransport(nil)}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}

// An installed api's requests carry the bearer and a minted class token; a
// request's own Authorization is kept; another host's requests carry
// neither; once removed, nothing is added. Before, a validator's reads
// carried no credential at all.
func TestValidatorAPITransportAddsIdentityOnlyForTheAPIHost(t *testing.T) {
	t.Parallel()
	api := newValidatorAPIIdentityTestAPI(t, 9, time.Now)
	other := newValidatorAPIIdentityTestAPI(t, 9, time.Now)
	class, err := newValidatorClassTokens(t.Context(), 9, api.server.URL, func() string { return "client-jwt" }, validatorAPIIdentityTestSigner(t, 9))
	if err != nil {
		t.Fatal(err)
	}
	uninstall, err := installValidatorAPIIdentity(api.server.URL, &validatorAPIIdentity{credential: func() string { return "client-jwt" }, class: class})
	if err != nil {
		t.Fatal(err)
	}

	validatorAPIIdentityTestGet(t, api.server.URL+"/sn/artifact", "")
	seen := api.lastHeaders()
	if seen.Get("Authorization") != "Bearer client-jwt" || seen.Get(protocol.ValidatorClassHeader) != "class-for-client-jwt" {
		t.Fatalf("an api read carried %q and %q", seen.Get("Authorization"), seen.Get(protocol.ValidatorClassHeader))
	}
	validatorAPIIdentityTestGet(t, api.server.URL+"/sn/attempt-artifact", "Bearer upload-jwt")
	if seen := api.lastHeaders(); seen.Get("Authorization") != "Bearer upload-jwt" || seen.Get(protocol.ValidatorClassHeader) != "class-for-client-jwt" {
		t.Fatalf("a request's own bearer was replaced: %q", seen.Get("Authorization"))
	}
	if api.mints.Load() != 1 {
		t.Fatalf("%d mints for two requests within a token's life", api.mints.Load())
	}
	validatorAPIIdentityTestGet(t, other.server.URL+"/sn/artifact", "")
	if seen := other.lastHeaders(); seen.Get("Authorization") != "" || seen.Get(protocol.ValidatorClassHeader) != "" {
		t.Fatal("another host's request carried the validator's identity")
	}
	uninstall()
	validatorAPIIdentityTestGet(t, api.server.URL+"/sn/artifact", "")
	if seen := api.lastHeaders(); seen.Get("Authorization") != "" || seen.Get(protocol.ValidatorClassHeader) != "" {
		t.Fatal("a removed identity was still added")
	}
}

// A test clock safe to read from the mint's goroutine and the fake api.
type validatorClassTestClock struct {
	nanos atomic.Int64
}

func newValidatorClassTestClock() *validatorClassTestClock {
	self := &validatorClassTestClock{}
	self.nanos.Store(time.Unix(1_800_000_000, 0).UnixNano())
	return self
}

func (self *validatorClassTestClock) now() time.Time {
	return time.Unix(0, self.nanos.Load())
}

func (self *validatorClassTestClock) advance(d time.Duration) {
	self.nanos.Add(int64(d))
}

// Waits for a running background mint to be recorded.
func (self *validatorClassTokens) testingSettle() {
	self.lock.Lock()
	minting := self.minting
	self.lock.Unlock()
	if minting != nil {
		<-minting
	}
}

// Without a class token a request is sent as it is: no bearer is added, so
// its limits stay those of a request without one (owner decision
// 2026-11-24). Before, the bearer was added alone, moving the validator's
// anonymous reads from the per-address limit to the lower identity limit.
func TestValidatorAPITransportAddsNothingWithoutAClassToken(t *testing.T) {
	t.Parallel()
	api := newValidatorAPIIdentityTestAPI(t, 9, time.Now)
	api.failMints.Store(true)
	refused, err := newValidatorClassTokens(t.Context(), 9, api.server.URL, func() string { return "client-jwt" }, validatorAPIIdentityTestSigner(t, 9))
	if err != nil {
		t.Fatal(err)
	}
	for name, class := range map[string]*validatorClassTokens{"no activation": nil, "a refused mint": refused} {
		uninstall, err := installValidatorAPIIdentity(api.server.URL, &validatorAPIIdentity{credential: func() string { return "client-jwt" }, class: class})
		if err != nil {
			t.Fatal(err)
		}
		validatorAPIIdentityTestGet(t, api.server.URL+"/sn/artifact", "")
		uninstall()
		if seen := api.lastHeaders(); seen.Get("Authorization") != "" || seen.Get(protocol.ValidatorClassHeader) != "" {
			t.Fatalf("%s: a request without a class token carried %q and %q", name, seen.Get("Authorization"), seen.Get(protocol.ValidatorClassHeader))
		}
	}
	if api.mints.Load() != 1 {
		t.Fatalf("%d mints were tried, not the one refused", api.mints.Load())
	}
}

// Two runtimes naming the same api origin can't be told apart by host: its
// requests carry neither's identity until one is removed.
func TestValidatorAPITransportAddsNoIdentityForASharedHost(t *testing.T) {
	t.Parallel()
	api := newValidatorAPIIdentityTestAPI(t, 9, time.Now)
	uninstallA, err := installValidatorAPIIdentity(api.server.URL, &validatorAPIIdentity{credential: func() string { return "client-a" }, class: validatorClassTestTokens(t, "class-a")})
	if err != nil {
		t.Fatal(err)
	}
	defer uninstallA()
	uninstallB, err := installValidatorAPIIdentity(api.server.URL, &validatorAPIIdentity{credential: func() string { return "client-b" }, class: validatorClassTestTokens(t, "class-b")})
	if err != nil {
		t.Fatal(err)
	}
	validatorAPIIdentityTestGet(t, api.server.URL+"/sn/artifact", "")
	if seen := api.lastHeaders(); seen.Get("Authorization") != "" || seen.Get(protocol.ValidatorClassHeader) != "" {
		t.Fatalf("a shared host's request carried %q", seen.Get("Authorization"))
	}
	uninstallB()
	validatorAPIIdentityTestGet(t, api.server.URL+"/sn/artifact", "")
	if seen := api.lastHeaders(); seen.Get("Authorization") != "Bearer client-a" || seen.Get(protocol.ValidatorClassHeader) != "class-a" {
		t.Fatalf("the remaining runtime's request carried %q and %q", seen.Get("Authorization"), seen.Get(protocol.ValidatorClassHeader))
	}
}

// A token is reused until the refresh margin, then minted again in the
// background while it is still sent. A failed mint keeps the live token and
// is retried after 30 s, doubling while it keeps failing; an expired token is
// never sent.
func TestValidatorClassTokensRefreshAndBackOff(t *testing.T) {
	t.Parallel()
	clock := newValidatorClassTestClock()
	api := newValidatorAPIIdentityTestAPI(t, 9, clock.now)
	class, err := newValidatorClassTokens(t.Context(), 9, api.server.URL, func() string { return "client-jwt" }, validatorAPIIdentityTestSigner(t, 9))
	if err != nil {
		t.Fatal(err)
	}
	class.now = clock.now
	// tokens from the fake api live 4 min
	if token := class.current(t.Context()); token != "class-for-client-jwt" || api.mints.Load() != 1 {
		t.Fatalf("first token %q after %d mints", token, api.mints.Load())
	}
	clock.advance(time.Minute)
	if class.current(t.Context()); api.mints.Load() != 1 {
		t.Fatal("a token was minted again outside its refresh margin")
	}
	// inside the margin, with mints failing: the live token is still sent
	api.failMints.Store(true)
	clock.advance(90 * time.Second)
	if token := class.current(t.Context()); token != "class-for-client-jwt" {
		t.Fatalf("a refresh withheld the live token: %q", token)
	}
	class.testingSettle()
	if token := class.current(t.Context()); token != "class-for-client-jwt" || api.mints.Load() != 2 {
		t.Fatalf("a failed refresh lost the live token: %q after %d mints", token, api.mints.Load())
	}
	// past the token's expiry and the first retry interval
	clock.advance(2 * time.Minute)
	if token := class.current(t.Context()); token != "" || api.mints.Load() != 3 {
		t.Fatalf("an expired token was sent: %q after %d mints", token, api.mints.Load())
	}
	// the second failure doubled the interval to 60 s
	api.failMints.Store(false)
	clock.advance(validatorClassRetryInterval)
	if token := class.current(t.Context()); token != "" || api.mints.Load() != 3 {
		t.Fatalf("a mint was retried inside the doubled interval: %q after %d mints", token, api.mints.Load())
	}
	clock.advance(validatorClassRetryInterval)
	if token := class.current(t.Context()); token != "class-for-client-jwt" || api.mints.Load() != 4 {
		t.Fatalf("the class did not recover: %q after %d mints", token, api.mints.Load())
	}
}

// A slow mint never holds a request that has a live token, and holds one
// without a live token at most the mint wait. Without a session, nothing is
// minted and no retry is spent.
func TestValidatorClassTokensNeverHoldRequestsOnASlowMint(t *testing.T) {
	t.Parallel()
	clock := newValidatorClassTestClock()
	api := newValidatorAPIIdentityTestAPI(t, 9, clock.now)
	var credential atomic.Value
	credential.Store("")
	class, err := newValidatorClassTokens(t.Context(), 9, api.server.URL, func() string { return credential.Load().(string) }, validatorAPIIdentityTestSigner(t, 9))
	if err != nil {
		t.Fatal(err)
	}
	class.now = clock.now
	class.mintWait = 50 * time.Millisecond
	if token := class.current(t.Context()); token != "" || api.mints.Load() != 0 {
		t.Fatalf("a mint without a session: %q after %d mints", token, api.mints.Load())
	}
	credential.Store("client-jwt")
	if token := class.current(t.Context()); token != "class-for-client-jwt" {
		t.Fatalf("a session's first token was %q; a missing session spent a retry", token)
	}
	block := make(chan struct{})
	api.blockMints.Store(&block)
	clock.advance(150 * time.Second)
	started := time.Now()
	if token := class.current(t.Context()); token != "class-for-client-jwt" || time.Second < time.Since(started) {
		t.Fatalf("a request with a live token waited %s on a slow mint and got %q", time.Since(started), token)
	}
	// the token expires while the mint is still held
	clock.advance(2 * time.Minute)
	started = time.Now()
	if token := class.current(t.Context()); token != "" || time.Second < time.Since(started) {
		t.Fatalf("a request without a live token waited %s and got %q", time.Since(started), token)
	}
	close(block)
	class.testingSettle()
	if token := class.current(t.Context()); token != "class-for-client-jwt" || api.mints.Load() != 2 {
		t.Fatalf("the held mint's token was not kept: %q after %d mints", token, api.mints.Load())
	}
	// a closed class ends its mints and starts none
	clock.advance(5 * time.Minute)
	class.close()
	if token := class.current(t.Context()); token != "" || api.mints.Load() != 2 {
		t.Fatalf("a closed class minted: %q after %d mints", token, api.mints.Load())
	}
}

type validatorAPIIdentityTestCloser struct {
	http.RoundTripper
	closed atomic.Int64
}

func (self *validatorAPIIdentityTestCloser) CloseIdleConnections() {
	self.closed.Add(1)
}

// A client's CloseIdleConnections still reaches the transport under the
// wrapper; before, it stopped at the wrapper.
func TestValidatorAPITransportClosesIdleConnections(t *testing.T) {
	t.Parallel()
	base := &validatorAPIIdentityTestCloser{RoundTripper: http.DefaultTransport}
	(&http.Client{Transport: validatorAPITransport(base)}).CloseIdleConnections()
	if base.closed.Load() != 1 {
		t.Fatal("closing a wrapped client's idle connections did not reach its transport")
	}
}

// Every api client the validator builds sends through the identity wrapper.
func TestValidatorAPIClientsCarryTheIdentity(t *testing.T) {
	t.Parallel()
	bounds, _ := attemptReplayV2TestBounds()
	reader, err := NewHTTPAttemptStreamV2Reader("https://api.example", bounds)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := NewHTTPAttemptStreamV2Writer("https://api.example", bounds, func() string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	history, err := NewHTTPClientKeyHistoryReader("https://api.example", func() string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	wallet, err := NewHttpWalletMappingReader("https://api.example")
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := NewHTTPArtifactReader("https://api.example", "sn25-mainnet-20261007-01", 25)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := newSettlementV2Objects("https://api.example")
	if err != nil {
		t.Fatal(err)
	}
	for name, client := range map[string]*http.Client{"attempt reader": reader.client, "attempt writer": writer.client, "client-key history": history.client,
		"wallet mapping": wallet.client, "artifact": artifact.client, "server keys": newReleaseServerKeysV2Client(), "ledger objects": objects.client} {
		if _, ok := client.Transport.(*validatorAPITransportWrapper); !ok {
			t.Errorf("the %s client sends without the validator's api identity", name)
		}
	}
	// the provider attempt source builds its client per call
	api := newValidatorAPIIdentityTestAPI(t, 9, time.Now)
	uninstall, err := installValidatorAPIIdentity(api.server.URL, &validatorAPIIdentity{credential: func() string { return "client-jwt" }, class: validatorClassTestTokens(t, "class-token")})
	if err != nil {
		t.Fatal(err)
	}
	defer uninstall()
	_, _ = providerAttemptHttp(t.Context(), api.server.URL+"/sn/attempt", http.MethodGet, nil, 1024)
	if seen := api.lastHeaders(); seen == nil || seen.Get("Authorization") != "Bearer client-jwt" || seen.Get(protocol.ValidatorClassHeader) != "class-token" {
		t.Fatal("the provider attempt source's request carried no class token and bearer")
	}
}
