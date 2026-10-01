package clientauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

const testClientId = "00000000-0000-0000-0000-000000000101"
const testDeviceId = "00000000-0000-0000-0000-000000000202"

func testClientJwt(t *testing.T, marker string) string {
	t.Helper()
	token, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims{
		"client_id": testClientId,
		"device_id": testDeviceId,
		"exp":       time.Now().Add(30 * 24 * time.Hour).Unix(),
		"marker":    marker,
	}).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func testApi(t *testing.T, serverUrl string) (*sdk.Api, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	strategy := connect.NewClientStrategyWithDefaults(ctx)
	api := sdk.NewApi(ctx, strategy, serverUrl)
	return api, func() {
		_ = api.CloseAndWait(context.Background())
		strategy.Close()
		cancel()
	}
}

func TestLoadOrCreateClientJwtBootstrapsAndPersists(t *testing.T) {
	clientJwt := testClientJwt(t, "bootstrap")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hello":
			w.WriteHeader(http.StatusOK)
			return
		case "/network/auth-client":
		default:
			http.NotFound(w, r)
			t.Errorf("path = %s", r.URL.Path)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer network-jwt" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"by_client_jwt":%q}`, clientJwt)
	}))
	defer server.Close()

	dir := t.TempDir()
	networkPath := filepath.Join(dir, "jwt")
	clientPath := filepath.Join(dir, "client.jwt")
	if err := WriteToken(networkPath, "network-jwt"); err != nil {
		t.Fatal(err)
	}
	api, closeApi := testApi(t, server.URL)
	defer closeApi()

	gotJwt, gotClientId, err := LoadOrCreateClientJwt(context.Background(), api, networkPath, clientPath, "test")
	if err != nil {
		t.Fatal(err)
	}
	if gotJwt != clientJwt || api.GetByJwt() != clientJwt {
		t.Fatalf("client JWT was not installed: got=%q api=%q", gotJwt, api.GetByJwt())
	}
	if gotClientId.String() != testClientId {
		t.Fatalf("client id = %s", gotClientId)
	}
	stored, err := ReadToken(clientPath)
	if err != nil || stored != clientJwt {
		t.Fatalf("stored client JWT = %q, err=%v", stored, err)
	}
	info, err := os.Stat(clientPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("client JWT mode = %o, want 600", got)
	}
}

func TestLoadOrCreateClientJwtRefreshesOnRestart(t *testing.T) {
	storedJwt := testClientJwt(t, "stored")
	refreshedJwt := testClientJwt(t, "refreshed")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hello" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/auth/refresh" {
			http.NotFound(w, r)
			t.Errorf("unexpected bootstrap request: %s", r.URL.Path)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+storedJwt {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"by_jwt":%q}`, refreshedJwt)
	}))
	defer server.Close()

	dir := t.TempDir()
	clientPath := filepath.Join(dir, "client.jwt")
	if err := WriteToken(clientPath, storedJwt); err != nil {
		t.Fatal(err)
	}
	api, closeApi := testApi(t, server.URL)
	defer closeApi()

	gotJwt, _, err := LoadOrCreateClientJwt(context.Background(), api, filepath.Join(dir, "missing-network-jwt"), clientPath, "test")
	if err != nil {
		t.Fatal(err)
	}
	if gotJwt != refreshedJwt || api.GetByJwt() != refreshedJwt {
		t.Fatalf("restart did not install refresh: got=%q api=%q", gotJwt, api.GetByJwt())
	}
	stored, err := ReadToken(clientPath)
	if err != nil || stored != refreshedJwt {
		t.Fatalf("restart did not persist refresh: stored=%q err=%v", stored, err)
	}
}

func TestLoadOrCreateClientJwtRequiresFreshAuthAfterConfirmedRejection(t *testing.T) {
	rejectedJwt := testClientJwt(t, "rejected")
	replacementJwt := testClientJwt(t, "replacement")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hello":
			w.WriteHeader(http.StatusOK)
		case "/auth/refresh":
			if got := r.Header.Get("Authorization"); got != "Bearer "+rejectedJwt {
				t.Errorf("refresh authorization = %q", got)
			}
			http.Error(w, "not authorized", http.StatusUnauthorized)
		case "/network/auth-client":
			if got := r.Header.Get("Authorization"); got != "Bearer new-network-jwt" {
				t.Errorf("bootstrap authorization = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"by_client_jwt":%q}`, replacementJwt)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	networkPath := filepath.Join(dir, "jwt")
	clientPath := filepath.Join(dir, "client.jwt")
	if err := WriteToken(networkPath, "network-jwt"); err != nil {
		t.Fatal(err)
	}
	if err := WriteToken(clientPath, rejectedJwt); err != nil {
		t.Fatal(err)
	}
	api, closeApi := testApi(t, server.URL)
	defer closeApi()

	if _, _, err := LoadOrCreateClientJwt(context.Background(), api, networkPath, clientPath, "test"); err == nil {
		t.Fatal("confirmed client rejection silently recreated the revoked client")
	}
	if _, err := ReadToken(rejectionPath(clientPath)); err != nil {
		t.Fatalf("rejection marker was not persisted: %v", err)
	}

	// An explicit auth writes a new network JWT. Its different fingerprint
	// authorizes a deliberate new-client bootstrap on the next start.
	if err := WriteToken(networkPath, "new-network-jwt"); err != nil {
		t.Fatal(err)
	}
	gotJwt, _, err := LoadOrCreateClientJwt(context.Background(), api, networkPath, clientPath, "test")
	if err != nil {
		t.Fatal(err)
	}
	if gotJwt != replacementJwt || api.GetByJwt() != replacementJwt {
		t.Fatalf("fresh auth did not bootstrap replacement: got=%q api=%q", gotJwt, api.GetByJwt())
	}
}
