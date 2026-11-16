//go:build linux || darwin

package validator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
)

// The release-1.0 operator whose network JWT the operator rejects names the
// network_jwt_file to write a fresh sign-in to.
func TestReleaseOperatorNamesARejectedSignIn(t *testing.T) {
	fixture := newReleaseClientSeedContinuityFixture(t)
	var bootstraps atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hello":
			w.WriteHeader(http.StatusOK)
		case "/network/auth-client":
			bootstraps.Add(1)
			http.Error(w, "not authorized", http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	op := fixture.op
	op.APIURL = server.URL
	if isOwnerRecycleProductionConfig(fixture.cfg) {
		t.Skip("the fixture configuration takes the production operator path")
	}
	network, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims{"network_id": "00000000-0000-0000-0000-000000000301", "user_id": "00000000-0000-0000-0000-000000000401"}).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if err := clientauth.WriteNetworkToken(op.NetworkJWTFile, network); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(op.ClientJWTFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	runtime, err := startReleaseOperatorWithAdmission(context.Background(), fixture.cfg, op, func() uint64 { return 42 },
		func(context.Context, *AttemptBoundary, []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			return AttemptBoundary{}, nil, errors.New("unexpected boundary resolution")
		}, fixture.state, func([32]byte) error { return nil })
	if runtime != nil {
		if runtime.close != nil {
			runtime.close()
		}
		t.Fatal("a rejected sign-in returned runtime authority")
	}
	if !clientauth.IsNetworkCredentialRejected(err) || !strings.Contains(err.Error(), "the network sign-in was rejected or has expired; sign in again and write a fresh network JWT to "+op.NetworkJWTFile) {
		t.Fatalf("err = %v", err)
	}
	if bootstraps.Load() != 1 {
		t.Fatalf("bootstraps = %d", bootstraps.Load())
	}
}
