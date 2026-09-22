package miner

// Wallet control tests preserve the one-shot signed-write boundary across a
// lost response, source-bound TLS, redirects, and response-size failures.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"
	"github.com/urnetwork/server/model"
)

// The server consumes the first signed write and closes before its reply. A
// retry must leave the operation and obtain/sign a new challenge, not replay.
func TestProviderSwarmControlWalletLostReplyNeedsFreshChallenge(t *testing.T) {
	swarm := newSwarmControlTestSwarm(t)
	member := swarm.members["miner-1"]
	var challenges, wallets atomic.Int32
	var stateLock sync.Mutex
	var walletArgs []*sdk.SnSetWalletArgs
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/hello" {
			return
		}
		if host, _, err := net.SplitHostPort(request.RemoteAddr); err != nil || host != member.SourceIP {
			t.Errorf("wallet request lost its configured source: %s, %v", request.RemoteAddr, err)
		}
		if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer token" {
			t.Error("wallet request lost its existing authorization")
			http.Error(writer, "invalid wallet request", http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/auth/wallet-challenge":
			index := challenges.Add(1)
			message := model.FormatWalletAuthChallengeMessage(fmt.Sprintf("lost-reply-%d", index), 1700000000+int64(index))
			_ = json.NewEncoder(writer).Encode(sdk.AuthWalletChallengeResult{MessageTemplate: message})
		case "/sn/wallet":
			var args sdk.SnSetWalletArgs
			if err := json.NewDecoder(request.Body).Decode(&args); err != nil {
				t.Error(err)
				return
			}
			stateLock.Lock()
			walletArgs = append(walletArgs, &args)
			stateLock.Unlock()
			if valid, err := model.VerifyBittensorSignature(member.Wallet, args.Message, args.Signature); !valid || err != nil {
				t.Errorf("wallet signature invalid: %v", err)
			}
			if wallets.Add(1) == 1 {
				connection, _, err := writer.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = connection.Close()
				return
			}
			_, _ = writer.Write([]byte(`{}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	member.APIURL = server.URL
	settings := swarmWalletTestSettings()
	settings.TlsConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	var err error
	settings.DialContextSettings, err = testEgressDialContextForIP(member.SourceIP)
	if err != nil {
		t.Fatal(err)
	}
	swarm.startMember = func(ctx context.Context, _ ProviderSwarmMember, _ func(error)) (*providerSwarmInstance, error) {
		if err := setSwarmMemberWallet(ctx, member, settings); err != nil {
			return nil, err
		}
		return &providerSwarmInstance{}, nil
	}
	waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, context.Background(), "enable"), http.StatusServiceUnavailable)
	readSwarmControlTestStatus(t, swarm, "miner-1", "failed")
	if challenges.Load() != 1 || wallets.Load() != 1 {
		t.Fatalf("lost reply replayed a consumed challenge: challenges=%d wallets=%d", challenges.Load(), wallets.Load())
	}
	waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, context.Background(), "enable"), http.StatusOK)
	readSwarmControlTestStatus(t, swarm, "miner-1", "running")
	stateLock.Lock()
	defer stateLock.Unlock()
	if challenges.Load() != 2 || wallets.Load() != 2 || len(walletArgs) != 2 {
		t.Fatalf("fresh retry requests: challenges=%d wallets=%d args=%d", challenges.Load(), wallets.Load(), len(walletArgs))
	}
	if walletArgs[0].Message == walletArgs[1].Message || walletArgs[0].Signature == walletArgs[1].Signature {
		t.Fatal("new enable reused its previous challenge or signature")
	}
	if valid, err := model.VerifyBittensorSignature(member.Wallet, walletArgs[1].Message, walletArgs[0].Signature); valid || err != nil {
		t.Fatalf("old signature authorized new challenge: valid=%t, err=%v", valid, err)
	}
}

// Following a temporary redirect would send the same signed payload twice.
func TestProviderSwarmControlWalletDoesNotFollowSignedWriteRedirect(t *testing.T) {
	member := validProviderSwarmConfig(t).Members[0]
	var wallets, redirected atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/hello":
			return
		case "/auth/wallet-challenge":
			_ = json.NewEncoder(writer).Encode(sdk.AuthWalletChallengeResult{
				MessageTemplate: model.FormatWalletAuthChallengeMessage("redirect-test", 1700000000),
			})
		case "/sn/wallet":
			wallets.Add(1)
			writer.Header().Set("Location", "/replayed")
			writer.WriteHeader(http.StatusTemporaryRedirect)
		case "/replayed":
			redirected.Add(1)
			_, _ = writer.Write([]byte(`{}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	member.APIURL = server.URL
	err := setSwarmMemberWallet(context.Background(), member, swarmWalletTestSettings())
	var statusError *connect.HttpStatusError
	if !errors.As(err, &statusError) || statusError.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("signed-write redirect error = %v", err)
	}
	if wallets.Load() != 1 || redirected.Load() != 0 {
		t.Fatalf("signed-write redirect replayed: wallets=%d redirected=%d", wallets.Load(), redirected.Load())
	}
}

// A reply without a content length is still bounded before JSON decoding.
func TestProviderSwarmControlWalletBoundsStreamedReply(t *testing.T) {
	member := validProviderSwarmConfig(t).Members[0]
	var wallets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/hello":
			return
		case "/auth/wallet-challenge":
			_ = json.NewEncoder(writer).Encode(sdk.AuthWalletChallengeResult{
				MessageTemplate: model.FormatWalletAuthChallengeMessage("body-limit-test", 1700000000),
			})
		case "/sn/wallet":
			wallets.Add(1)
			writer.(http.Flusher).Flush()
			_, _ = writer.Write([]byte(strings.Repeat("x", 1024)))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	member.APIURL = server.URL
	settings := swarmWalletTestSettings()
	settings.MaxHttpResponseBodyBytes = 256
	err := setSwarmMemberWallet(context.Background(), member, settings)
	if !errors.Is(err, connect.ErrHttpResponseBodyTooLarge) || wallets.Load() != 1 {
		t.Fatalf("wallet reply bound: error=%v wallets=%d", err, wallets.Load())
	}
}

// A real transport callback's typed deadline must survive the wallet helper
// and the member handler, so a fresh operation can retry it as a 503.
func TestProviderSwarmControlWalletPreservesDialDeadline(t *testing.T) {
	swarm := newSwarmControlTestSwarm(t)
	member := swarm.members["miner-1"]
	member.APIURL = "https://api.example"
	settings := swarmWalletTestSettings()
	var dials atomic.Int32
	settings.DialContextSettings = &connect.DialContextSettings{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dials.Add(1)
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded}
		},
	}
	swarm.startMember = func(ctx context.Context, _ ProviderSwarmMember, _ func(error)) (*providerSwarmInstance, error) {
		err := setSwarmMemberWallet(ctx, member, settings)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("transport callback lost its typed deadline: %v", err)
		}
		return nil, err
	}
	waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, context.Background(), "enable"), http.StatusServiceUnavailable)
	if dials.Load() != 1 {
		t.Fatalf("one wallet operation made %d dial attempts", dials.Load())
	}
}
