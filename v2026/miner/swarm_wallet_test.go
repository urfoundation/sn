package miner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

func swarmWalletTestSettings() *connect.ClientStrategySettings {
	settings := connect.DefaultClientStrategySettings()
	settings.EnableResilient = false
	settings.RequestTimeout = 2 * time.Second
	settings.ConnectTimeout = time.Second
	return settings
}

func TestSetSwarmMemberWalletSignsFreshChallengeForExistingProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	member := validProviderSwarmConfig(t).Members[0]
	providerJwt, err := os.ReadFile(filepath.Join(member.StateDir, ".provider.jwt"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := swarmMemberWalletKey(member)
	if err != nil {
		t.Fatal(err)
	}
	f := newFinancialWalletMappingHttp(t, string(providerJwt), member.Wallet, key.PublicKey())
	member.APIURL = f.endpoint.URL
	// A separately selected earning interval needs fresh consent; retry of the
	// same interval is covered by the retained-original replay roots.
	for index := range 2 {
		from, through := int64(3+index*2), int64(4+index*2)
		member.WalletFromEpoch, member.WalletThroughEpoch = &from, &through
		f.stateLock.Lock()
		f.epoch, f.fromEpoch, f.throughEpoch = from-1, from, through
		f.stateLock.Unlock()
		if err := setSwarmMemberWallet(ctx, member, swarmWalletTestSettings()); err != nil {
			t.Fatal(err)
		}
	}
	f.stateLock.Lock()
	defer f.stateLock.Unlock()
	if len(f.challengeRequests) != 2 || len(f.walletBodies) != 2 || f.messages[0] == f.messages[1] {
		t.Fatal("fresh earning windows lost their distinct challenges", len(f.challengeRequests), len(f.walletBodies))
	}
	for index, raw := range f.walletBodies {
		var args sdk.SnSetWalletArgs
		if err := json.Unmarshal(raw, &args); err != nil || args.Message != f.messages[index] {
			t.Fatal("signed handoff lost its exact fresh challenge", index, err)
		}
		original, _, err := financialWalletMappingTestReceipt(ctx, &args)
		if err != nil {
			t.Fatal("signed handoff lost its original coldkey proof", err)
		}
		original.Message += "changed"
		if _, _, err := protocol.VerifyWalletMappingConsent(ctx, original); err == nil {
			t.Fatal("signature accepted changed challenge bytes")
		}
	}
}

func TestSetSwarmMemberWalletRejectsKeyMismatchBeforeHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	member := validProviderSwarmConfig(t).Members[0]
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "must not contact server", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	member.APIURL = server.URL
	originalWallet := member.Wallet
	otherKey, err := crv4.KeypairFromSeed([32]byte{154})
	if err != nil {
		t.Fatal(err)
	}
	member.Wallet = otherKey.Address()
	if err := setSwarmMemberWallet(ctx, member, swarmWalletTestSettings()); err == nil || !strings.Contains(err.Error(), "derives") {
		t.Fatalf("wallet/key mismatch was not rejected: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("mismatched wallet sent %d HTTP requests", requests.Load())
	}
	if err := os.Remove(member.WalletSeedFile); err != nil {
		t.Fatal(err)
	}
	member.Wallet = originalWallet
	if err := setSwarmMemberWallet(ctx, member, swarmWalletTestSettings()); err == nil {
		t.Fatal("missing wallet seed was accepted")
	}
	if _, err := os.Lstat(member.WalletSeedFile); !os.IsNotExist(err) {
		t.Fatalf("missing wallet was regenerated: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("missing fresh wallet seed sent %d HTTP requests", requests.Load())
	}
}

func TestSetSwarmMemberWalletPreservesServerRefusal(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for _, refusedPath := range []string{"/sn/wallet/consent", "/sn/wallet"} {
		member := validProviderSwarmConfig(t).Members[0]
		providerJwt, err := os.ReadFile(filepath.Join(member.StateDir, ".provider.jwt"))
		if err != nil {
			t.Fatal(err)
		}
		key, err := swarmMemberWalletKey(member)
		if err != nil {
			t.Fatal(err)
		}
		f := newFinancialWalletMappingHttp(t, string(providerJwt), member.Wallet, key.PublicKey())
		if refusedPath == "/sn/wallet/consent" {
			f.challengeFault = "refused"
		} else {
			f.receiptFault = "refused"
		}
		member.APIURL = f.endpoint.URL
		if err := setSwarmMemberWallet(ctx, member, swarmWalletTestSettings()); err == nil || !strings.Contains(err.Error(), "wallet authorization refused") {
			t.Fatalf("%s: server refusal was not preserved: %v", refusedPath, err)
		}
		f.stateLock.Lock()
		wallets := len(f.walletBodies)
		f.stateLock.Unlock()
		if refusedPath == "/sn/wallet/consent" && wallets != 0 || refusedPath == "/sn/wallet" && wallets != 1 {
			t.Fatal("refused wallet handoff changed its request boundary", refusedPath, wallets)
		}
	}
}

func TestSetSwarmMemberWalletCancellationStopsChallenge(t *testing.T) {
	member := validProviderSwarmConfig(t).Members[0]
	started, joined := make(chan struct{}), make(chan struct{})
	cleanup := make(chan struct{})
	var wallets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hello" {
			return
		}
		if r.URL.Path == "/sn/epoch" {
			_ = json.NewEncoder(w).Encode(sdk.SnEpochResult{Epoch: 2})
			return
		}
		if r.URL.Path == "/sn/wallet/consent" {
			if _, err := io.Copy(io.Discard, r.Body); err != nil {
				t.Error(err)
				return
			}
			close(started)
			select {
			case <-r.Context().Done():
				close(joined)
			case <-cleanup:
			}
			return
		}
		wallets.Add(1)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(cleanup) })
	t.Cleanup(server.CloseClientConnections)
	member.APIURL = server.URL
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	result := make(chan error, 1)
	go func() { result <- setSwarmMemberWallet(ctx, member, swarmWalletTestSettings()) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("challenge did not start")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled challenge succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wallet handoff did not join after cancellation")
	}
	select {
	case <-joined:
	case <-time.After(5 * time.Second):
		t.Fatal("challenge HTTP request survived cancellation")
	}
	if wallets.Load() != 0 {
		t.Fatal("wallet was submitted after challenge cancellation")
	}
}
