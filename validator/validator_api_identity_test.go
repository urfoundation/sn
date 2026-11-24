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
	lock        sync.Mutex
	seen        []http.Header
}

func newValidatorAPIIdentityTestAPI(t *testing.T, replicaNoID uint64, now func() time.Time) *validatorAPIIdentityTestAPI {
	t.Helper()
	self := &validatorAPIIdentityTestAPI{replicaNoID: replicaNoID}
	self.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == validatorClassMintPath {
			self.mints.Add(1)
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
	class, err := newValidatorClassTokens(api.server.URL, func() string { return "client-jwt" }, validatorAPIIdentityTestSigner(t, 9))
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

// A token is reused until the refresh margin, then minted again. A failed
// mint keeps the live token, isn't retried for a while, and after the token
// expires leaves requests with none.
func TestValidatorClassTokensRefreshAndBackOff(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0)
	clock := func() time.Time { return now }
	api := newValidatorAPIIdentityTestAPI(t, 9, clock)
	class, err := newValidatorClassTokens(api.server.URL, func() string { return "client-jwt" }, validatorAPIIdentityTestSigner(t, 9))
	if err != nil {
		t.Fatal(err)
	}
	class.now = clock
	if token := class.current(context.Background()); token != "class-for-client-jwt" || api.mints.Load() != 1 {
		t.Fatalf("first token %q after %d mints", token, api.mints.Load())
	}
	now = now.Add(time.Minute)
	if class.current(context.Background()); api.mints.Load() != 1 {
		t.Fatal("a token was minted again inside its refresh margin")
	}
	// inside the margin: a failed mint keeps the live token
	api.failMints.Store(true)
	now = now.Add(90 * time.Second)
	if token := class.current(context.Background()); token != "class-for-client-jwt" || api.mints.Load() != 2 {
		t.Fatalf("a failed refresh lost the live token: %q after %d mints", token, api.mints.Load())
	}
	if class.current(context.Background()); api.mints.Load() != 2 {
		t.Fatal("a failed mint was retried at once")
	}
	// past the token's expiry and the retry interval, with mints failing
	now = now.Add(2 * time.Minute)
	if token := class.current(context.Background()); token != "" || api.mints.Load() != 3 {
		t.Fatalf("an expired token was sent: %q after %d mints", token, api.mints.Load())
	}
	api.failMints.Store(false)
	now = now.Add(validatorClassRetryInterval)
	if token := class.current(context.Background()); token != "class-for-client-jwt" || api.mints.Load() != 4 {
		t.Fatalf("the class did not recover: %q after %d mints", token, api.mints.Load())
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
	uninstall, err := installValidatorAPIIdentity(api.server.URL, &validatorAPIIdentity{credential: func() string { return "client-jwt" }})
	if err != nil {
		t.Fatal(err)
	}
	defer uninstall()
	_, _ = providerAttemptHttp(t.Context(), api.server.URL+"/sn/attempt", http.MethodGet, nil, 1024)
	if seen := api.lastHeaders(); seen == nil || seen.Get("Authorization") != "Bearer client-jwt" {
		t.Fatal("the provider attempt source's request carried no bearer")
	}
}
