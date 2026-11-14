//go:build linux || darwin

package miner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/hotkeyauth"
	"github.com/urfoundation/sn/v2026/operatorlist"
)

// An automatic sign-in is never a deliberate recovery for a revoked client.
// An operator child registers its slot, the network revokes the slot's
// client, the expired network sign-in is rejected at renewal and set aside,
// the supervisor signs in again with the hotkey and starts the child, and the
// slot still refuses: provider custody blocks on any rejection marker,
// whatever network sign-in follows.
func TestAutomaticSignInKeepsARevokedSlotBlocked(t *testing.T) {
	base := providerRegistrationPrivateDir(t)
	domain := "operator.example"
	dir := operatorlist.DomainStateDir(base, domain)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := newProviderRegistrationFixtureIn(t, dir)
	jwtPath := filepath.Join(dir, "jwt")

	// the child registers its direct slot
	handoff := errors.New("synthetic authenticated handoff")
	if err := fixture.run(t.Context(), true, providerRegistrationHooks{afterAuthenticated: func(string, connect.Id, []byte) error { return handoff }}); !errors.Is(err, handoff) {
		t.Fatalf("first registration: %v", err)
	}
	posts, _, _, _ := fixture.counts()
	if posts != 1 {
		t.Fatalf("registration posts = %d", posts)
	}

	// the network revokes the slot's client: the runtime rejection tombstones
	// it in provider custody
	keyOwner, err := clientauth.OpenProviderClientKey(t.Context(), filepath.Join(dir, ".provider.key"), clientauth.ProviderClientKeyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	clientPath, err := providerClientJwtPath(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := keyOwner.RejectClientJwt(clientPath); err != nil {
		t.Fatal(err)
	}
	if err := keyOwner.Close(); err != nil {
		t.Fatal(err)
	}

	// the operator's network sign-in has expired: the renewal call is
	// rejected and the child sets the sign-in aside
	expired, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims{"network_id": "00000000-0000-0000-0000-000000000301", "user_id": "00000000-0000-0000-0000-000000000401", "roles": []string{"provider"}, "principal": "synthetic-provider-owner", "exp": time.Now().Add(-10 * 24 * time.Hour).Unix()}).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if err := clientauth.WriteNetworkToken(jwtPath, expired); err != nil {
		t.Fatal(err)
	}
	output := newProviderDiagnosticTestOwner(t, &providerDiagnosticRecorder{records: make(chan []byte, 16)})
	quarantined := make(chan clientauth.NetworkTokenQuarantine, 1)
	stopRenewal := startNetworkTokenRenewal(t.Context(), fixture.server.URL, nil, fixture.settings(true).testEgressDialer, jwtPath, output, true, func(quarantine clientauth.NetworkTokenQuarantine) { quarantined <- quarantine })
	var quarantine clientauth.NetworkTokenQuarantine
	select {
	case quarantine = <-quarantined:
		if token, _ := clientauth.ReadToken(quarantine.Path); token != expired {
			t.Fatal("the set-aside file does not hold the expired sign-in")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the rejected sign-in was not set aside")
	}
	if err := stopRenewal(); err != nil {
		t.Fatal(err)
	}

	// the supervisor signs in again with the hotkey and starts the child
	automatic := providerRegistrationTestToken(t, "", "automatic sign-in")
	supervisor := newTestOperatorFixture(t)
	// the supervisor's clock just after the child's quarantine
	supervisor.clock.set(quarantine.Time.Sub(testOperatorEpoch) + time.Second)
	supervisor.base = base
	supervisor.settings.baseStateDir = base
	supervisor.settings.autoRegister = true
	supervisor.settings.hotkey = testHotkey(t, 11)
	supervisor.signIn = func(context.Context, hotkeyauth.Settings) (*hotkeyauth.Network, error) {
		return &hotkeyauth.Network{ByJwt: automatic}, nil
	}
	supervisor.publish(operatorlist.Operator{Domain: domain, ApiUrl: fixture.server.URL, ConnectUrl: "ws" + strings.TrimPrefix(fixture.server.URL, "http")})
	supervisor.run()
	at := supervisor.journal.wait(t, 0, "log operator operator.example: signing in again with the hotkey after 1 rejected network sign-ins in a row")
	supervisor.journal.wait(t, at, "log operator operator.example: signed in with the hotkey")
	child := supervisor.nextStart()
	if token, err := clientauth.ReadToken(jwtPath); err != nil || token != automatic {
		t.Fatalf("operator jwt after the automatic sign-in: %v", err)
	}

	// the started child refuses the revoked slot and allocates nothing
	err = fixture.run(t.Context(), true, providerRegistrationHooks{afterAuthenticated: func(string, connect.Id, []byte) error {
		t.Error("a revoked slot authenticated after the automatic sign-in")
		return handoff
	}})
	var refused *clientauth.RegistrationRefusedError
	if !errors.As(err, &refused) || refused.Code != "client_revoked" {
		t.Fatalf("the revoked slot after the automatic sign-in: %v", err)
	}
	if after, _, _, _ := fixture.counts(); after != posts {
		t.Fatalf("registration posts = %d after the automatic sign-in, want %d", after, posts)
	}
	supervisor.stop(syscall.SIGTERM, child)
}

// A run with --quarantine-rejected-sign-in whose renewal call is rejected
// sets the sign-in aside and ends with the quarantine, which provide turns
// into exit status 75 for the supervisor. Without the flag the same run keeps
// its sign-in in place.
func TestProviderRunEndsWithAQuarantinedSignIn(t *testing.T) {
	for _, quarantine := range []bool{true, false} {
		t.Run(map[bool]string{true: "flag", false: "no flag"}[quarantine], func(t *testing.T) {
			fixture := newProviderRegistrationFixture(t)
			jwtPath := filepath.Join(fixture.dir, "jwt")
			expired, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims{"network_id": "00000000-0000-0000-0000-000000000301", "user_id": "00000000-0000-0000-0000-000000000401", "roles": []string{"provider"}, "principal": "synthetic-provider-owner", "exp": time.Now().Add(-10 * 24 * time.Hour).Unix()}).SignedString(gojwt.UnsafeAllowNoneSignatureType)
			if err != nil {
				t.Fatal(err)
			}
			if err := clientauth.WriteNetworkToken(jwtPath, expired); err != nil {
				t.Fatal(err)
			}
			settings := fixture.settings(true)
			settings.quarantineRejectedSignIn = quarantine
			handoff := errors.New("synthetic handoff")
			hooks := providerRegistrationHooks{afterAuthenticated: func(string, connect.Id, []byte) error {
				// hold the worker until the renewal call was answered
				deadline := time.Now().Add(10 * time.Second)
				for time.Now().Before(deadline) {
					fixture.stateLock.Lock()
					answered := fixture.networkRefreshes > 0
					fixture.stateLock.Unlock()
					if answered {
						return handoff
					}
					time.Sleep(10 * time.Millisecond)
				}
				return errors.New("the renewal call was never made")
			}}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			err = settings.run(context.WithValue(ctx, providerRegistrationHooksKey{}, hooks), &providerRefusedWriter{})
			code, exits := providerRunExitCode(err)
			_, set, _ := clientauth.LatestNetworkTokenQuarantine(jwtPath)
			if quarantine {
				if !errors.Is(err, errProviderNetworkSignInQuarantined) || !exits || code != providerQuarantinedExitCode || !set {
					t.Fatalf("run err = %v, exit %d %t, set aside %t", err, code, exits, set)
				}
				if _, err := os.Stat(jwtPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("the rejected sign-in is still in place: %v", err)
				}
				return
			}
			if errors.Is(err, errProviderNetworkSignInQuarantined) || exits || set {
				t.Fatalf("without the flag: err = %v, exit %t, set aside %t", err, exits, set)
			}
			if token, err := clientauth.ReadToken(jwtPath); err != nil || token != expired {
				t.Fatalf("without the flag the sign-in moved: %v", err)
			}
		})
	}
}
