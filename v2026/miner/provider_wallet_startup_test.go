//go:build linux || darwin

// Optional startup wallet work uses the actual newly authenticated slot.
// The observer stops only after the signed consent was durably acknowledged.
package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// Both the first direct allocation and a proxy allocation reach the real
// consent producer after registration; neither can borrow the bootstrap JWT.
func TestProviderStartupWalletUsesFirstDirectCredential(t *testing.T) {
	providerStartupWalletUsesAuthenticatedSlot(t, false)
}

// A proxy has its own retained credential even beside an unrelated direct one.
func TestProviderStartupWalletUsesProxyCredential(t *testing.T) {
	providerStartupWalletUsesAuthenticatedSlot(t, true)
}

// A retained direct credential without its shared key is recovery work, even
// when a fresh proxy slot and wallet handoff were explicitly requested.
func TestProviderStartupWalletRefusesProxyWhenRetainedKeyIsMissing(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	unrelated := providerRegistrationTestToken(t, "00000000-0000-0000-0000-000000000999", "unrelated-direct")
	directPath := filepath.Join(fixture.dir, ".provider.jwt")
	if err := clientauth.WriteToken(directPath, unrelated); err != nil {
		t.Fatal(err)
	}
	proxy := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.25:1080"}
	selectedPath, err := providerClientJwtPath(proxy)
	if err != nil {
		t.Fatal(err)
	}
	settings := fixture.settings(true, proxy)
	settings.wallet, settings.walletProof = testAliceAddress, &snWalletProof{SeedFile: writeTestSeedFile(t, testAliceSeedHex)}
	stop := errors.New("synthetic unexpected provider authentication")
	var authenticated atomic.Bool
	hooks := providerRegistrationHooks{afterAuthenticated: func(string, connect.Id, []byte) error {
		authenticated.Store(true)
		return stop
	}}
	err = settings.run(context.WithValue(ctx, providerRegistrationHooksKey{}, hooks), &providerRefusedWriter{})
	var refused *clientauth.RegistrationRefusedError
	posts, legacy, allocations, refreshes := fixture.counts()
	if !errors.As(err, &refused) || refused.Code != "provider_key_missing_with_retained_history" || authenticated.Load() || posts != 0 || legacy != 0 || allocations != 0 || refreshes != 0 {
		t.Fatal("proxy wallet startup bypassed original provider key recovery", err, posts, legacy, allocations, refreshes)
	}
	for _, path := range []string{filepath.Join(fixture.dir, ".provider.key"), filepath.Join(fixture.dir, ".provider.key.identity"), selectedPath, selectedPath + ".registration", selectedPath + ".wallet-consent"} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("refused proxy wallet startup created replacement custody", filepath.Base(path), err)
		}
	}
	if retained, err := clientauth.ReadToken(directPath); err != nil || retained != unrelated {
		t.Fatal("refused proxy wallet startup changed the retained direct credential", err)
	}
}

// This helper runs one complete independent startup per top-level root.
func providerStartupWalletUsesAuthenticatedSlot(t *testing.T, proxySlot bool) {
	t.Helper()
	fixture := newProviderRegistrationFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var proxy *connect.ProxySettings
	var originalSeed, originalMarker []byte
	unrelated := providerRegistrationTestToken(t, "00000000-0000-0000-0000-000000000999", "unrelated-direct")
	if proxySlot {
		proxy = &connect.ProxySettings{Network: "tcp", Address: "192.0.2.25:1080"}
		// Existing credentials require their original shared key and marker.
		keyOwner, err := clientauth.OpenProviderClientKey(ctx, filepath.Join(fixture.dir, ".provider.key"), clientauth.ProviderClientKeyOptions{AllowCreate: true})
		if err != nil {
			t.Fatal(err)
		}
		originalSeed = keyOwner.Seed()
		if err := errors.Join(keyOwner.PersistClientJwt(filepath.Join(fixture.dir, ".provider.jwt"), unrelated), keyOwner.Close()); err != nil {
			t.Fatal(err)
		}
		originalMarker, err = os.ReadFile(filepath.Join(fixture.dir, ".provider.key.identity"))
		if err != nil {
			t.Fatal(err)
		}
	}
	selectedPath, err := providerClientJwtPath(proxy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(selectedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fixture did not begin before provider registration", err)
	}
	seedPath := writeTestSeedFile(t, testAliceSeedHex)
	coldkey, err := ss58.DecodeWithPrefix(testAliceAddress, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	var ownerLock sync.Mutex
	var authenticated string
	var retainedSeed []byte
	var writes, challenges atomic.Int32
	fixture.stateLock.Lock()
	fixture.walletHandler = func(w http.ResponseWriter, request *http.Request) {
		ownerLock.Lock()
		token := authenticated
		ownerLock.Unlock()
		if token == "" || token == unrelated || request.Header.Get("Authorization") != "Bearer "+token {
			t.Error("wallet escaped before its actual provider authenticated")
			http.Error(w, "wrong provider", http.StatusForbidden)
			return
		}
		switch request.URL.Path {
		case "/sn/epoch":
			// This is the actual Server decimal-string no_id wire shape.
			_, _ = w.Write([]byte(`{"epoch":2,"no_id":"13"}`))
		case "/sn/wallet/consent":
			challenges.Add(1)
			var args sdk.SnWalletMappingChallengeArgs
			id, idErr := clientauth.ClientIdFromJwt(token)
			if err := json.NewDecoder(request.Body).Decode(&args); err != nil || idErr != nil || args.ClientId == nil || args.ClientId.String() != id.String() || args.ColdkeySs58 != testAliceAddress || args.FromEpoch != 3 || args.ThroughEpoch != 65538 {
				t.Error("startup wallet changed its provider or default earning interval", err)
				http.Error(w, "scope", 400)
				return
			}
			_ = json.NewEncoder(w).Encode(sdk.SnWalletMappingChallengeResult{Message: financialWalletMappingTestMessage(t, token, coldkey, args.FromEpoch, args.ThroughEpoch, 29)})
		case "/sn/wallet":
			var args sdk.SnSetWalletArgs
			if err := json.NewDecoder(request.Body).Decode(&args); err != nil {
				t.Error(err)
				http.Error(w, "body", 400)
				return
			}
			original, receipt, err := financialWalletMappingTestReceipt(request.Context(), &args)
			if err != nil {
				t.Error(err)
				http.Error(w, "signature", 400)
				return
			}
			var journal snWalletConsentJournal
			raw, readErr := os.ReadFile(filepath.Join(selectedPath+".wallet-consent", "history.json"))
			if readErr != nil || json.Unmarshal(raw, &journal) != nil || len(journal.Records) != 1 || journal.Records[0].Original != original || journal.Records[0].Acknowledged {
				t.Error("startup POST preceded original signed custody", readErr)
			}
			writes.Add(1)
			_ = json.NewEncoder(w).Encode(receipt)
		default:
			t.Error("startup used a projection-only wallet route", request.URL.Path)
			http.Error(w, "unexpected", 400)
		}
	}
	fixture.stateLock.Unlock()
	stop := errors.New("synthetic completed wallet handoff")
	hooks := providerRegistrationHooks{afterAuthenticated: func(token string, _ connect.Id, seed []byte) error {
		ownerLock.Lock()
		authenticated, retainedSeed = token, bytes.Clone(seed)
		ownerLock.Unlock()
		return nil
	}, afterWallet: func(err error) error {
		if err != nil {
			t.Error("actual startup wallet failed", err)
		}
		return stop
	}}
	settings := fixture.settings(true, proxy)
	settings.wallet, settings.walletProof = testAliceAddress, &snWalletProof{SeedFile: seedPath}
	if err := settings.run(context.WithValue(ctx, providerRegistrationHooksKey{}, hooks), &providerRefusedWriter{}); !errors.Is(err, stop) {
		t.Fatal("startup did not complete the real wallet handoff", err)
	}
	posts, legacy, allocations, _ := fixture.counts()
	if posts != 1 || legacy != 0 || allocations != 1 || writes.Load() != 1 || challenges.Load() != 1 {
		t.Fatal("startup wallet changed provider allocation or consent count", posts, legacy, allocations, writes.Load(), challenges.Load())
	}
	token, err := clientauth.ReadToken(selectedPath)
	if err != nil || token != authenticated {
		t.Fatal("wallet changed selected provider credential", err)
	}
	seed, err := os.ReadFile(filepath.Join(fixture.dir, ".provider.key"))
	if err != nil || !bytes.Equal(seed, retainedSeed) {
		t.Fatal("wallet changed provider key custody", err)
	}
	if proxySlot {
		marker, err := os.ReadFile(filepath.Join(fixture.dir, ".provider.key.identity"))
		if err != nil || !bytes.Equal(seed, originalSeed) || !bytes.Equal(marker, originalMarker) {
			t.Fatal("proxy wallet replaced the existing provider key or identity", err)
		}
		other, err := os.ReadFile(filepath.Join(fixture.dir, ".provider.jwt"))
		if err != nil || strings.TrimSpace(string(other)) != unrelated {
			t.Fatal("proxy wallet rewrote unrelated direct credential", err)
		}
	}
}
