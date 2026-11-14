// Real financial entry points use retained private credentials and local HTTP.
// Tokens are synthetic; only the test server substitutes external admission.
package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

const financialTestClientId = "00000000-0000-0000-0000-000000000201"

// This is local identity syntax, never a production credential or key.
func financialTestJwt(t testing.TB, clientId, marker string) string {
	t.Helper()
	claims := gojwt.MapClaims{"network_id": "00000000-0000-0000-0000-000000000301", "user_id": "00000000-0000-0000-0000-000000000401", "roles": []string{"provider"}, "principal": "synthetic-financial-owner", "exp": time.Now().Add(24 * time.Hour).Unix(), "marker": marker}
	if clientId != "" {
		claims["client_id"], claims["device_id"] = clientId, "00000000-0000-0000-0000-000000000501"
	}
	token, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, claims).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// Both files exist so a regression to the bootstrap token is observable.
func setTestProviderJwt(t *testing.T) string {
	t.Helper()
	setTestNetworkJwt(t, financialTestJwt(t, "", "bootstrap"))
	token := financialTestJwt(t, financialTestClientId, "provider")
	if err := os.WriteFile(filepath.Join(os.Getenv("URNETWORK_STATE_DIR"), ".provider.jwt"), []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	return token
}

// Parse the real grammar, then run the complete signer-free command. The
// actual SDK retains authorization and selector across both GET operations.
func TestFiniteClaimSelectsProviderCredentialAndExplicitLegacyOriginal(t *testing.T) {
	provider := setTestProviderJwt(t)
	network, err := os.ReadFile(filepath.Join(os.Getenv("URNETWORK_STATE_DIR"), "jwt"))
	if err != nil {
		t.Fatal(err)
	}
	second := financialTestJwt(t, "00000000-0000-0000-0000-000000000202", "second")
	secondPath := filepath.Join(t.TempDir(), "second.jwt")
	if err := os.WriteFile(secondPath, []byte(second), 0600); err != nil {
		t.Fatal(err)
	}
	claim := finiteClaimTestResult()
	legacy, err := ss58.Encode([32]byte(claim.Coldkey), ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	var expectedToken, expectedLegacy string
	var expectedLock sync.Mutex
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hello" {
			return
		}
		expectedLock.Lock()
		token, originalColdkey := expectedToken, expectedLegacy
		expectedLock.Unlock()
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("financial GET selected another credential")
			http.Error(w, "identity", 403)
			return
		}
		switch r.URL.Path {
		case "/sn/epoch":
			_ = json.NewEncoder(w).Encode(sdk.SnEpochResult{Epoch: 2})
		case "/sn/pool/claim":
			calls.Add(1)
			if r.URL.Query().Get("epoch") != "1" || r.URL.Query().Get("legacy_coldkey") != originalColdkey {
				t.Error("claim selector changed")
				http.Error(w, "scope", 400)
				return
			}
			_ = json.NewEncoder(w).Encode(claim)
		default:
			http.Error(w, "unexpected mutation/route", 400)
		}
	}))
	defer server.Close()
	for _, testCase := range []struct{ option, token, coldkey string }{{"", provider, ""}, {"--provider-jwt=" + secondPath, second, ""}, {"--legacy-coldkey=" + legacy, strings.TrimSpace(string(network)), legacy}} {
		expectedLock.Lock()
		expectedToken, expectedLegacy = testCase.token, testCase.coldkey
		expectedLock.Unlock()
		args := []string{"claim", "--api_url=" + server.URL}
		if testCase.option != "" {
			args = append(args, testCase.option)
		}
		opts, err := docopt.ParseArgs(mainUsage(), args, "synthetic")
		if err != nil {
			t.Fatal(err)
		}
		if err := runFiniteClaim(t.Context(), opts, finiteClaimTestHooks()); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 3 {
		t.Fatal("complete claim flows missing", calls.Load())
	}
}

// Missing, malformed or network-only provider files cannot trigger allocation,
// wallet writes, proof reads or replacement of their original credential bytes.
func TestProviderFinancialCommandsHoldMissingAndNetworkCredentials(t *testing.T) {
	setTestNetworkJwt(t, financialTestJwt(t, "", "bootstrap"))
	path := filepath.Join(os.Getenv("URNETWORK_STATE_DIR"), ".provider.jwt")
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); http.Error(w, "must remain local", 403) }))
	defer endpoint.Close()
	for _, original := range []string{"", financialTestJwt(t, "", "network-in-provider-slot"), "malformed", financialTestJwt(t, "00000000-0000-0000-0000-000000000000", "zero")} {
		if original != "" {
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := runFiniteClaim(t.Context(), docopt.Opts{"--api_url": endpoint.URL, "--epoch": "1"}, finiteClaimTestHooks()); err == nil {
			t.Fatal("unscoped claim admitted")
		}
		ctx, strategy := testClientStrategy(t)
		if err := snSetWallet(ctx, strategy, endpoint.URL, testAliceAddress, nil); err == nil {
			t.Fatal("unscoped wallet admitted")
		}
		retained, err := os.ReadFile(path)
		if original == "" {
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatal("missing provider identity was created", err)
			}
		} else if err != nil || string(retained) != original {
			t.Fatal("credential refusal changed original", err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("missing provider identity reached HTTP", requests.Load())
	}
}

// The real daemon API observes a renewed file without replacing its provider,
// and an unchanged disk token cannot roll back a completed SDK refresh.
func TestClaimProviderCredentialReloadPreservesOriginalIdentityAndRefresh(t *testing.T) {
	original := setTestProviderJwt(t)
	renewed := financialTestJwt(t, financialTestClientId, "sdk-renewed")
	diskRenewed := financialTestJwt(t, financialTestClientId, "disk-renewed")
	var requests atomic.Int32
	var authLock sync.Mutex
	var authorizations, refreshAuthorizations []string
	refreshToken := renewed
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hello":
			return
		case "/auth/refresh":
			authLock.Lock()
			refreshAuthorizations = append(refreshAuthorizations, r.Header.Get("Authorization"))
			reply := refreshToken
			authLock.Unlock()
			_ = json.NewEncoder(w).Encode(sdk.RefreshJwtResult{ByJwt: reply})
		case "/sn/pool/claim":
			requests.Add(1)
			authLock.Lock()
			authorizations = append(authorizations, r.Header.Get("Authorization"))
			authLock.Unlock()
			_ = json.NewEncoder(w).Encode(finiteClaimTestResult())
		default:
			t.Error("unexpected credential route", r.URL.Path)
			http.Error(w, "unexpected", 400)
		}
	}))
	defer endpoint.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	strategy := connect.NewClientStrategy(ctx, finiteClaimTestHooks().strategySettings)
	defer strategy.Close()
	api := sdk.NewApi(ctx, strategy, endpoint.URL)
	defer func() {
		if err := api.CloseAndWait(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	owner := &claimCredentialApi{api: api}
	if _, err := owner.SnPoolClaimSyncWithContext(ctx, &sdk.SnPoolClaimArgs{Epoch: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SnPoolClaimSyncWithContext(ctx, &sdk.SnPoolClaimArgs{Epoch: 1}); err != nil {
		t.Fatal(err)
	}
	authLock.Lock()
	second := authorizations[1]
	secondRefresh := refreshAuthorizations[1]
	authLock.Unlock()
	if second != "Bearer "+renewed || secondRefresh != "Bearer "+renewed || api.GetByJwt() != renewed {
		t.Fatal("unchanged original file rolled back SDK renewal")
	}
	path := filepath.Join(os.Getenv("URNETWORK_STATE_DIR"), ".provider.jwt")
	if raw, err := os.ReadFile(path); err != nil || string(raw) != original {
		t.Fatal("daemon rewrote provider-owned credential", err)
	}
	if err := os.WriteFile(path, []byte(diskRenewed), 0600); err != nil {
		t.Fatal(err)
	}
	// Observe the accepted disk generation; the synchronous refresh may already
	// have advanced the in-memory token again under the same original identity.
	if _, err := owner.SnPoolClaimSyncWithContext(ctx, &sdk.SnPoolClaimArgs{Epoch: 1}); err != nil || owner.diskToken != diskRenewed {
		t.Fatal("same-provider disk renewal refused", err)
	}
	authLock.Lock()
	thirdRefresh := refreshAuthorizations[2]
	authLock.Unlock()
	if thirdRefresh != "Bearer "+diskRenewed {
		t.Fatal("changed retained credential did not reach the actual refresh owner")
	}
	before := requests.Load()
	for _, replacement := range []string{financialTestJwt(t, "00000000-0000-0000-0000-000000000202", "sibling"), financialTestJwt(t, "", "network")} {
		if err := os.WriteFile(path, []byte(replacement), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.SnPoolClaimSyncWithContext(ctx, &sdk.SnPoolClaimArgs{Epoch: 1}); err == nil {
			t.Fatal("daemon changed original provider")
		}
	}
	if requests.Load() != before || !bytes.Equal([]byte(owner.original), []byte(original)) {
		t.Fatal("changed identity reached proof read")
	}
	if err := os.WriteFile(path, []byte(diskRenewed), 0600); err != nil {
		t.Fatal(err)
	}
	// A validly encoded refresh response cannot change any original identity
	// coordinate before publication to the SDK or the subsequent proof read.
	for claim, value := range map[string]any{"client_id": "00000000-0000-0000-0000-000000000202", "device_id": "00000000-0000-0000-0000-000000000502", "network_id": "00000000-0000-0000-0000-000000000302", "user_id": "00000000-0000-0000-0000-000000000402", "roles": []string{"owner"}, "principal": "another-owner"} {
		claims := gojwt.MapClaims{}
		if _, _, err := gojwt.NewParser().ParseUnverified(renewed, claims); err != nil {
			t.Fatal(err)
		}
		claims[claim] = value
		changed, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, claims).SignedString(gojwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatal(err)
		}
		authLock.Lock()
		refreshToken = changed
		authLock.Unlock()
		if _, err := owner.SnPoolClaimSyncWithContext(ctx, &sdk.SnPoolClaimArgs{Epoch: 1}); err == nil || api.GetByJwt() != renewed || requests.Load() != before {
			t.Fatal("changed refresh identity was published", claim, err)
		}
	}
}
