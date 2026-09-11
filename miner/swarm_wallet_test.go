package miner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"
	"github.com/urnetwork/server/model"
)

func swarmWalletTestSettings() *connect.ClientStrategySettings {
	settings := connect.DefaultClientStrategySettings()
	settings.EnableResilient = false
	settings.RequestTimeout = 2 * time.Second
	settings.ConnectTimeout = time.Second
	return settings
}

func TestSetSwarmMemberWalletSignsFreshChallengeForExistingProvider(t *testing.T) {
	member := validProviderSwarmConfig(t).Members[0]
	var challenges, wallets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/hello" {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("wallet request lacks its existing network authorization")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/auth/wallet-challenge":
			var args sdk.AuthWalletChallengeArgs
			if err := json.NewDecoder(r.Body).Decode(&args); err != nil || args.Blockchain != "TAO" || args.WalletAddress != member.Wallet {
				t.Errorf("challenge does not select the provisioned coldkey: %v", err)
				http.Error(w, "invalid challenge", http.StatusBadRequest)
				return
			}
			index := challenges.Add(1)
			message := model.FormatWalletAuthChallengeMessage(fmt.Sprintf("wallet-%d", index), 1700000000+int64(index))
			_ = json.NewEncoder(w).Encode(sdk.AuthWalletChallengeResult{MessageTemplate: message})
		case "/sn/wallet":
			var args sdk.SnSetWalletArgs
			if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
				t.Error(err)
				http.Error(w, "invalid wallet", http.StatusBadRequest)
				return
			}
			index := wallets.Add(1)
			message := model.FormatWalletAuthChallengeMessage(fmt.Sprintf("wallet-%d", index), 1700000000+int64(index))
			valid, err := model.VerifyBittensorSignature(member.Wallet, args.Message, args.Signature)
			if !valid || err != nil || args.ColdkeySs58 != member.Wallet || args.Message != message || challenges.Load() != index || args.ClientId == nil || args.ClientId.String() != "00000000-0000-0000-0000-000000000001" {
				t.Errorf("signed handoff lost its exact challenge, payout key or provider identity: valid=%t err=%v", valid, err)
				_ = json.NewEncoder(w).Encode(sdk.SnSetWalletResult{Error: &sdk.SnSetWalletError{Message: "invalid signed wallet"}})
				return
			}
			if replayValid, _ := model.VerifyBittensorSignature(member.Wallet, args.Message+"changed", args.Signature); replayValid {
				t.Error("signature accepted changed challenge bytes")
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	member.APIURL = server.URL
	for range 2 {
		if err := setSwarmMemberWallet(context.Background(), member, swarmWalletTestSettings()); err != nil {
			t.Fatal(err)
		}
	}
	if challenges.Load() != 2 || wallets.Load() != 2 {
		t.Fatalf("requests: challenges=%d wallets=%d", challenges.Load(), wallets.Load())
	}
}

func TestSetSwarmMemberWalletRejectsKeyMismatchBeforeHTTP(t *testing.T) {
	member := validProviderSwarmConfig(t).Members[0]
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "must not contact server", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	member.APIURL = server.URL
	member.Wallet = "different-payout-address"
	if err := setSwarmMemberWallet(context.Background(), member, swarmWalletTestSettings()); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("wallet/key mismatch was not rejected: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("mismatched wallet sent %d HTTP requests", requests.Load())
	}
	if err := os.Remove(member.WalletSeedFile); err != nil {
		t.Fatal(err)
	}
	if err := setSwarmMemberWallet(context.Background(), member, swarmWalletTestSettings()); err == nil {
		t.Fatal("missing wallet seed was accepted")
	}
	if _, err := os.Lstat(member.WalletSeedFile); !os.IsNotExist(err) {
		t.Fatalf("missing wallet was regenerated: %v", err)
	}
}

func TestSetSwarmMemberWalletPreservesServerRefusal(t *testing.T) {
	for _, refusedPath := range []string{"/auth/wallet-challenge", "/sn/wallet"} {
		t.Run(refusedPath, func(t *testing.T) {
			member := validProviderSwarmConfig(t).Members[0]
			var wallets atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/hello" {
					return
				}
				if r.URL.Path == "/sn/wallet" {
					wallets.Add(1)
				}
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == refusedPath {
					_, _ = w.Write([]byte(`{"error":{"message":"wallet authorization refused"}}`))
					return
				}
				_ = json.NewEncoder(w).Encode(sdk.AuthWalletChallengeResult{MessageTemplate: model.FormatWalletAuthChallengeMessage("refusal-test", 1700000000)})
			}))
			t.Cleanup(server.Close)
			member.APIURL = server.URL
			if err := setSwarmMemberWallet(context.Background(), member, swarmWalletTestSettings()); err == nil || !strings.Contains(err.Error(), "wallet authorization refused") {
				t.Fatalf("server refusal was not preserved: %v", err)
			}
			if refusedPath == "/auth/wallet-challenge" && wallets.Load() != 0 {
				t.Fatal("wallet was submitted after challenge refusal")
			}
		})
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
		if r.URL.Path == "/auth/wallet-challenge" {
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
	ctx, cancel := context.WithCancel(context.Background())
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
