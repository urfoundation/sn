// Real local tls and forced response boundaries exercise production framing and
// ambiguous-outcome recovery without external services or production identities.
package payoutroster

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Every fixture signs generated test identities in a visibly synthetic domain.
func transportTestOriginal(t *testing.T) (payoutartifact.WholeWorkAuthority, []byte) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	authority, err := payoutartifact.SignWholeWorkAuthority(t.Context(), payoutartifact.WholeWorkAuthority{
		Domain: protocol.ClientKeyHistoryDomain{ChainID: 31337, GenesisHash: [32]byte{1}, Netuid: 1, Coordinator: common.Address{2}, SettlementVault: common.Address{3}, DeploymentIDHash: [32]byte{4}, PolicyHash: [32]byte{5}, NoID: 1},
		Epoch:  7, Start: payoutartifact.Boundary{Number: 20, Hash: common.Hash{6}.Hex()}, End: payoutartifact.Boundary{Number: 30, Hash: common.Hash{7}.Hex()}, RequestPublicKey: [32]byte{8},
		Owners: []payoutartifact.WholeWorkOwner{}, ExpectedProviders: []payoutartifact.WholeWorkExpectedProvider{}, PriorContracts: []payoutartifact.WholeWorkPriorContract{},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := authority.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return authority, raw
}

// This is the exact production providerWorkJson numeric-array envelope.
func transportTestReceipt(t *testing.T, raw []byte) []byte {
	t.Helper()
	receipt, err := json.Marshal(struct {
		AuthorityHash [32]byte `json:"authority_hash"`
	}{AuthorityHash: sha256.Sum256(raw)})
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

// An accepted post loses its reply, then the exact same post obtains the receipt.
func TestTransportReconcilesAmbiguousPublication(t *testing.T) {
	authority, raw := transportTestOriginal(t)
	receipt := transportTestReceipt(t, raw)
	var posts atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(body, raw) || r.Method != http.MethodPost || r.URL.Path != "/provider-work/v1/authorities" || r.URL.RawQuery != "" || r.ContentLength != int64(len(raw)) || r.Header.Get("Content-Type") != "application/json" || len(r.TransferEncoding) != 0 || r.Header.Get("Content-Encoding") != "" || len(r.Trailer) != 0 {
			t.Errorf("publication differs from the current server contract: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if posts.Add(1) == 1 {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("interrupt accepted response: %v", err)
				return
			}
			_ = connection.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(receipt)
	}))
	defer server.Close()
	transport, err := NewTransport(TransportSettings{ApiBase: server.URL, HttpClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	waits := 0
	transport.wait = func(ctx context.Context, delay time.Duration) error {
		waits++
		if delay != time.Second || ctx.Err() != nil {
			t.Fatal("retry lost its owner or configured pacing")
		}
		return nil
	}
	if err := transport.Publish(t.Context(), raw, authority.Signer); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 2 || waits != 1 {
		t.Fatalf("posts=%d waits=%d", posts.Load(), waits)
	}
}

// Busy and unavailable responses retain exact bytes, and inherited cookies are
// not allowed to turn public signed upload into an ambient authenticated call.
func TestTransportRetriesTransientStatusesWithoutCookies(t *testing.T) {
	authority, raw := transportTestOriginal(t)
	receipt := transportTestReceipt(t, raw)
	var posts atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(body, raw) || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("retry changed bytes or leaked ambient authentication")
		}
		statuses := []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusRequestTimeout, http.StatusOK}
		index := int(posts.Add(1)) - 1
		if index >= len(statuses) {
			t.Error("unexpected extra retry")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statuses[index])
		if statuses[index] == http.StatusOK {
			_, _ = w.Write(receipt)
		}
	}))
	defer server.Close()
	client := server.Client()
	client.Jar, _ = cookiejar.New(nil)
	endpoint, _ := url.Parse(server.URL)
	client.Jar.SetCookies(endpoint, []*http.Cookie{{Name: "synthetic_session", Value: "must-not-be-sent"}})
	transport, err := NewTransport(TransportSettings{ApiBase: server.URL, HttpClient: client})
	if err != nil {
		t.Fatal(err)
	}
	transport.wait = func(context.Context, time.Duration) error { return nil }
	if err := transport.Publish(t.Context(), raw, authority.Signer); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 4 {
		t.Fatalf("posts=%d", posts.Load())
	}
}

// Client refusals, unsigned framing ambiguity and mismatched digests are final.
func TestTransportRejectsPermanentResponsesWithoutRetry(t *testing.T) {
	authority, raw := transportTestOriginal(t)
	receipt := transportTestReceipt(t, raw)
	for _, test := range []struct {
		name   string
		status int
		body   []byte
		want   error
	}{
		{name: "refused", status: http.StatusBadRequest, body: []byte("synthetic refusal"), want: ErrPublicationRefused},
		{name: "competing original", status: http.StatusConflict, body: []byte("synthetic conflict"), want: ErrPublicationRefused},
		{name: "wrong digest", status: http.StatusOK, body: transportTestReceipt(t, []byte("different synthetic original")), want: ErrPublicationIntegrity},
		{name: "unknown field", status: http.StatusOK, body: append(append([]byte{}, receipt[:len(receipt)-1]...), []byte(`,"extra":true}`)...), want: ErrPublicationIntegrity},
		{name: "duplicate key", status: http.StatusOK, body: append(append([]byte{}, receipt[:len(receipt)-1]...), append([]byte(","), receipt[1:]...)...), want: ErrPublicationIntegrity},
		{name: "trailing object", status: http.StatusOK, body: append(bytes.Clone(receipt), []byte("{}")...), want: ErrPublicationIntegrity},
		{name: "bounded body", status: http.StatusOK, body: bytes.Repeat([]byte("x"), 1025), want: ErrPublicationIntegrity},
	} {
		var posts atomic.Int32
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			posts.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(test.status)
			_, _ = w.Write(test.body)
		}))
		transport, err := NewTransport(TransportSettings{ApiBase: server.URL, HttpClient: server.Client()})
		if err != nil {
			t.Fatal(err)
		}
		transport.wait = func(context.Context, time.Duration) error {
			t.Error("permanent failure retried")
			return context.Canceled
		}
		err = transport.Publish(t.Context(), raw, authority.Signer)
		server.Close()
		if !errors.Is(err, test.want) || posts.Load() != 1 {
			t.Errorf("%s: error=%v posts=%d", test.name, err, posts.Load())
		}
	}
}

// Even a configured client that follows redirects must not forward this upload.
func TestTransportDoesNotFollowRedirect(t *testing.T) {
	authority, raw := transportTestOriginal(t)
	var forwarded atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/private-target" {
			forwarded.Add(1)
		}
		http.Redirect(w, r, "/private-target", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return nil }
	transport, err := NewTransport(TransportSettings{ApiBase: server.URL, HttpClient: client})
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.Publish(t.Context(), raw, authority.Signer); !errors.Is(err, ErrPublicationRefused) || forwarded.Load() != 0 {
		t.Fatalf("redirect error=%v forwarded=%d", err, forwarded.Load())
	}
}

// Cancellation happens after headers but before the response body can complete.
func TestTransportCancellationInterruptsResponseBody(t *testing.T) {
	authority, raw := transportTestOriginal(t)
	bodyStarted := make(chan struct{})
	serverReleased := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverReleased)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(bodyStarted)
		<-r.Context().Done()
	}))
	defer server.Close()
	transport, err := NewTransport(TransportSettings{ApiBase: server.URL, HttpClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- transport.Publish(ctx, raw, authority.Signer) }()
	<-bodyStarted
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled body returned %v", err)
	}
	<-serverReleased
}

// Invalid endpoints and undersized owners fail locally without printing them.
func TestTransportRejectsUnsafeConfiguration(t *testing.T) {
	for _, endpoint := range []string{"http://roster.example", "https://synthetic-user:synthetic-password@roster.example", "https://roster.example?private=synthetic", "https://roster.example#private", "https://roster.example/%70rivate"} {
		if _, err := NewTransport(TransportSettings{ApiBase: endpoint}); !errors.Is(err, ErrTransportConfig) || strings.Contains(err.Error(), "synthetic") {
			t.Errorf("unsafe endpoint was accepted or exposed: %v", err)
		}
	}
	if _, err := NewTransport(TransportSettings{ApiBase: "https://roster.example", TotalTimeout: 59 * time.Second}); !errors.Is(err, ErrTransportConfig) {
		t.Fatalf("undersized owner accepted: %v", err)
	}
	transport, err := NewTransport(TransportSettings{ApiBase: "https://roster.example"})
	if err != nil || transport.totalTimeout != 300*time.Second || transport.attemptTimeout != 30*time.Second {
		t.Fatalf("unexpected production timeout defaults: %v", err)
	}
}

// Disabling certificate verification cannot turn a forged receipt into success.
func TestTransportRejectsUnauthenticatedReceipt(t *testing.T) {
	authority, raw := transportTestOriginal(t)
	receipt := transportTestReceipt(t, raw)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(receipt)
	}))
	defer server.Close()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	defer client.CloseIdleConnections()
	transport, err := NewTransport(TransportSettings{ApiBase: server.URL, HttpClient: client})
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.Publish(t.Context(), raw, authority.Signer); !errors.Is(err, ErrPublicationIntegrity) {
		t.Fatalf("unauthenticated receipt accepted: %v", err)
	}
}
