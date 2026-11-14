package miner

// Wallet control tests preserve the one-shot signed-write boundary across a
// lost response, source-bound TLS, redirects, and response-size failures.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

// The member's current consent peer: epoch, prospective challenge and signed
// write. Each test serves it behind its own TLS, source or reply boundary.
func newSwarmControlWalletHttp(t *testing.T, member ProviderSwarmMember) *financialWalletMappingHttp {
	t.Helper()
	providerJwt, err := os.ReadFile(filepath.Join(member.StateDir, ".provider.jwt"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := swarmMemberWalletKey(member)
	if err != nil {
		t.Fatal(err)
	}
	return newFinancialWalletMappingHttp(t, string(providerJwt), member.Wallet, key.PublicKey())
}

// The server consumes the first signed write and closes before its reply. The
// operation never retries it; a fresh operation replays the exact retained
// original instead of signing a new challenge while that original is pending.
func TestProviderSwarmControlWalletLostReplyReplaysRetainedOriginal(t *testing.T) {
	swarm := newSwarmControlTestSwarm(t)
	member := swarm.members["miner-1"]
	f := newSwarmControlWalletHttp(t, member)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/hello" {
			if host, _, err := net.SplitHostPort(request.RemoteAddr); err != nil || host != member.SourceIP {
				t.Errorf("wallet request lost its configured source: %s, %v", request.RemoteAddr, err)
			}
		}
		f.serve(writer, request)
	}))
	t.Cleanup(server.Close)
	member.APIURL = server.URL
	f.loseAck = func() {}
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
	f.stateLock.Lock()
	challenges, wallets := len(f.challengeRequests), len(f.walletBodies)
	f.loseAck = nil
	f.stateLock.Unlock()
	if challenges != 1 || wallets != 1 {
		t.Fatalf("lost reply was retried within its operation: challenges=%d wallets=%d", challenges, wallets)
	}
	waitSwarmControlTestResponse(t, startSwarmControlTestRequest(swarm, context.Background(), "enable"), http.StatusOK)
	readSwarmControlTestStatus(t, swarm, "miner-1", "running")
	f.stateLock.Lock()
	defer f.stateLock.Unlock()
	if len(f.challengeRequests) != 1 || len(f.walletBodies) != 2 || !bytes.Equal(f.walletBodies[0], f.walletBodies[1]) {
		t.Fatalf("fresh enable did not replay its exact retained original: challenges=%d wallets=%d", len(f.challengeRequests), len(f.walletBodies))
	}
}

// Following a temporary redirect would send the same signed payload twice.
func TestProviderSwarmControlWalletDoesNotFollowSignedWriteRedirect(t *testing.T) {
	member := validProviderSwarmConfig(t).Members[0]
	f := newSwarmControlWalletHttp(t, member)
	var wallets, redirected atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/sn/wallet":
			wallets.Add(1)
			writer.Header().Set("Location", "/replayed")
			writer.WriteHeader(http.StatusTemporaryRedirect)
		case "/replayed":
			redirected.Add(1)
			_, _ = writer.Write([]byte(`{}`))
		default:
			f.serve(writer, request)
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
	f := newSwarmControlWalletHttp(t, member)
	// The epoch and consent replies stay under 2 KiB, so only the signed
	// write's streamed reply can reach this bound.
	const maxResponseBytes = 4096
	var wallets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/sn/wallet" {
			f.serve(writer, request)
			return
		}
		wallets.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.(http.Flusher).Flush()
		_, _ = writer.Write([]byte(strings.Repeat("x", 4*maxResponseBytes)))
	}))
	t.Cleanup(server.Close)
	member.APIURL = server.URL
	settings := swarmWalletTestSettings()
	settings.MaxHttpResponseBodyBytes = maxResponseBytes
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

// Supplies read and close boundaries without transport timing or server races.
type swarmWalletTestBody struct {
	read  func([]byte) (int, error)
	close func() error
}

// Delegates each read to the test's explicit transition.
func (self *swarmWalletTestBody) Read(p []byte) (int, error) {
	return self.read(p)
}

// Delegates the one owned close to the test's explicit transition.
func (self *swarmWalletTestBody) Close() error {
	return self.close()
}

// Completed bytes must be discarded when cancellation occurs during either
// read or close, and cancellation never releases the body's close obligation.
func TestProviderSwarmControlWalletClosesCanceledResponses(t *testing.T) {
	for _, phase := range []string{"read", "close"} {
		ownerCtx, cancelOwner := context.WithCancel(context.Background())
		requestCtx, cancelRequest := context.WithCancel(context.Background())
		closes := 0
		body := &swarmWalletTestBody{
			read: func(p []byte) (int, error) {
				if phase == "read" {
					cancelRequest()
				}
				return copy(p, `{}`), io.EOF
			},
			close: func() error {
				closes++
				if phase == "close" {
					cancelOwner()
				}
				return nil
			},
		}
		responseBytes, err := readSwarmWalletResponse(ownerCtx, requestCtx, &http.Response{
			StatusCode: http.StatusOK, Body: body, ContentLength: -1,
		}, 256)
		cancelOwner()
		cancelRequest()
		if responseBytes != nil || !errors.Is(err, context.Canceled) || closes != 1 {
			t.Fatalf("%s cancellation ownership: bytes=%q err=%v closes=%d", phase, responseBytes, err, closes)
		}
	}
}

// A failing close invalidates otherwise complete bytes and remains visible
// alongside a read failure instead of replacing either failure.
func TestProviderSwarmControlWalletPreservesReadAndCloseFailures(t *testing.T) {
	closeErr := errors.New("synthetic body close failure")
	for _, readErr := range []error{io.EOF, io.ErrUnexpectedEOF} {
		closes := 0
		responseBytes, err := readSwarmWalletResponse(context.Background(), context.Background(), &http.Response{
			StatusCode: http.StatusOK, ContentLength: -1,
			Body: &swarmWalletTestBody{
				read: func(p []byte) (int, error) { return copy(p, `{}`), readErr },
				close: func() error {
					closes++
					return closeErr
				},
			},
		}, 256)
		if responseBytes != nil || !errors.Is(err, closeErr) || closes != 1 {
			t.Fatalf("close failure ownership: bytes=%q err=%v closes=%d", responseBytes, err, closes)
		}
		if readErr != io.EOF && !errors.Is(err, readErr) {
			t.Fatalf("close failure lost read failure: %v", err)
		}
		if swarmControlErrorStatus(err) != http.StatusConflict {
			t.Fatalf("semantic close failure became retryable: %v", err)
		}
	}
}

// A refusal's header remains semantic even when reading its body also fails.
func TestProviderSwarmControlWalletPreservesRefusalWithReadFailure(t *testing.T) {
	closes := 0
	responseBytes, err := readSwarmWalletResponse(context.Background(), context.Background(), &http.Response{
		StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized", ContentLength: -1,
		Body: &swarmWalletTestBody{
			read: func([]byte) (int, error) { return 0, io.ErrUnexpectedEOF },
			close: func() error {
				closes++
				return nil
			},
		},
	}, 256)
	var statusError *connect.HttpStatusError
	if responseBytes != nil || closes != 1 || !errors.Is(err, io.ErrUnexpectedEOF) ||
		!errors.As(err, &statusError) || statusError.StatusCode != http.StatusUnauthorized ||
		swarmControlErrorStatus(err) != http.StatusConflict {
		t.Fatalf("refusal and read failure: bytes=%q err=%v closes=%d", responseBytes, err, closes)
	}
}

// Wrapping a mixed join must not let a single nested transient cause hide a
// semantic refusal or malformed JSON, while all-transient joins can retry.
func TestProviderSwarmControlWalletClassifiesEveryJoinedCause(t *testing.T) {
	var decoded any
	decodeErr := json.Unmarshal([]byte(`{"error":`), &decoded)
	for _, testCase := range []struct {
		err  error
		code int
	}{
		{err: errors.Join(context.Canceled, io.ErrUnexpectedEOF), code: http.StatusServiceUnavailable},
		{err: errors.Join(context.DeadlineExceeded, errors.New("semantic wallet refusal")), code: http.StatusConflict},
		{err: errors.Join(io.ErrUnexpectedEOF, decodeErr), code: http.StatusConflict},
		{err: decodeErr, code: http.StatusConflict},
	} {
		if code := swarmControlErrorStatus(fmt.Errorf("wallet request: %w", testCase.err)); code != testCase.code {
			t.Errorf("joined cause classification: err=%v code=%d want=%d", testCase.err, code, testCase.code)
		}
	}
}
