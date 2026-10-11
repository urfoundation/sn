// Public HTTPS admission uses synthetic approvals and a real local TLS peer.
// DNS is mapped explicitly to that fixture; no test contacts a public service.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Endpoint validation never resolves a name or discovers transport authority.
func TestOwnedSubmissionApprovedHttpsEndpoints(t *testing.T) {
	pin := rootObjectHash("synthetic public RPC certificate")
	for _, endpoint := range []string{
		"https://archive.chain.rpc-service.example", "https://archive.chain.rpc-service.example:443",
		"https://rpc.transport.example:8443/archive", "https://xn--bcher-kva.example",
		"https://192.0.2.10:443", "http://192.0.2.10:9944", "http://[2001:db8::1]:9944",
	} {
		func() {
			route := ownedSubmissionRoute{RpcUrl: endpoint, ReadRetrySeconds: 60, SendTimeoutSeconds: 1}
			if strings.HasPrefix(endpoint, "https:") {
				route.TlsSpkiHash = pin
			}
			if err := route.validate(); err != nil {
				t.Fatal("explicit approved endpoint was refused", endpoint, err)
			}
		}()
	}
	for _, endpoint := range []string{
		"http://archive.chain.rpc-service.example", "http://rpc.transport.example:80", "https://localhost",
		"https://RPC.transport.example", "https://rpc.transport.example.", "https://rpc..transport.example",
		"https://-rpc.transport.example", "https://rpc-.transport.example", "https://rpc_transport.example",
		"https://b\u00fccher.example", "https://127.1", "https://0177.0.0.1", "https://0x7f000001",
		"https://192.0.2.10", "https://[::1]", "https://[::ffff:192.0.2.10]:443",
		"https://0.0.0.0:443", "https://224.0.0.1:443", "https://[fe80::1%25eth0]:443",
		"https://rpc.transport.example:", "https://rpc.transport.example:0", "https://rpc.transport.example:0443",
		"https://rpc.transport.example:65536", "https://user@rpc.transport.example", "https://rpc.transport.example?",
		"https://rpc.transport.example?key=x", "https://rpc.transport.example#fragment", "https://rpc.transport.example/%2fother",
		"https://" + strings.Repeat("a", 64) + ".example", "https://" + strings.Repeat("a.", 127) + "test",
	} {
		func() {
			route := ownedSubmissionRoute{RpcUrl: endpoint, ReadRetrySeconds: 60, SendTimeoutSeconds: 1}
			if strings.HasPrefix(endpoint, "https:") {
				route.TlsSpkiHash = pin
			}
			if err := route.validate(); err == nil {
				t.Fatal("ambiguous or unauthenticated endpoint was admitted", endpoint)
			}
		}()
	}
	for _, pin := range []string{"", "sha256:" + strings.Repeat("0", 64), "sha256:" + strings.Repeat("A", 64)} {
		route := ownedSubmissionRoute{RpcUrl: "https://archive.chain.rpc-service.example", TlsSpkiHash: pin}
		if route.validate() == nil {
			t.Fatal("hostname admitted without canonical nonzero certificate pin", pin)
		}
	}
}

// net/http supplies the default port to DialContext even when the approved URL
// omits it. Cancellation reaches the dialer without any DNS or network activity.
func TestOwnedSubmissionApprovedHttpsDefaultPortDialScope(t *testing.T) {
	route := ownedSubmissionRoute{RpcUrl: "https://archive.chain.rpc-service.example", TlsSpkiHash: rootObjectHash("synthetic pin"), ReadRetrySeconds: 60}
	client, err := newOwnedSubmissionClient(route)
	if err != nil {
		t.Fatal(err)
	}
	defer client.httpClient.CloseIdleConnections()
	transport := client.httpClient.Transport.(*http.Transport)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := transport.DialContext(ctx, "tcp", "archive.chain.rpc-service.example:443"); !errors.Is(err, context.Canceled) {
		t.Fatal("default HTTPS endpoint did not reach its canceled dial", err)
	}
	for _, endpoint := range []string{"archive.chain.rpc-service.example", "archive.chain.rpc-service.example:8443", "other.transport.example:443", "192.0.2.10:443"} {
		if _, err := transport.DialContext(ctx, "tcp", endpoint); err == nil || errors.Is(err, context.Canceled) {
			t.Fatal("unapproved dial escaped the exact endpoint guard", endpoint, err)
		}
	}
	if _, err := transport.DialContext(ctx, "tcp6", "archive.chain.rpc-service.example:443"); err == nil || errors.Is(err, context.Canceled) {
		t.Fatal("unapproved transport escaped the route guard", err)
	}
	if transport.Proxy != nil || !transport.DisableKeepAlives || transport.ForceAttemptHTTP2 || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("public hostname changed proxy, replay or certificate boundaries")
	}
}

// Only the local fixture's DNS mapping and trust root are supplied by the test.
// The production TLS hostname/SPKI checks and HTTP request mechanics stay real.
func newOwnedSubmissionHttpsFixture(t *testing.T, handler http.Handler, routeChanges ...func(*ownedSubmissionRoute)) (*rpcClient, ownedSubmissionRoute, *http.Transport) {
	t.Helper()
	private := ed25519.NewKeyFromSeed([]byte(strings.Repeat("q", ed25519.SeedSize)))
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"rpc.transport.example"},
		NotBefore: time.Unix(0, 0), NotAfter: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA: true, BasicConstraintsValid: true}
	encoded, err := x509.CreateCertificate(rand.Reader, template, template, private.Public(), private)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(encoded)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{encoded}, PrivateKey: private}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)
	route := ownedSubmissionRoute{RpcUrl: "https://rpc.transport.example:" + port, TlsSpkiHash: "sha256:" + hex.EncodeToString(digest[:]), ReadRetrySeconds: 60, SendTimeoutSeconds: 5}
	for _, change := range routeChanges {
		change(&route)
	}
	client, err := newOwnedSubmissionClient(route)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.httpClient.CloseIdleConnections)
	transport := client.httpClient.Transport.(*http.Transport)
	transport.TLSClientConfig.RootCAs = x509.NewCertPool()
	transport.TLSClientConfig.RootCAs.AddCert(certificate)
	dial := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, endpoint string) (net.Conn, error) {
		// Exercise the actual route gate before replacing DNS with the one
		// known local address. The canceled probe cannot leave this process.
		probeCtx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := dial(probeCtx, network, endpoint); !errors.Is(err, context.Canceled) {
			return nil, errors.Join(errors.New("synthetic fixture received an unapproved dial"), err)
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	return client, route, transport
}

// Trusted certificate chains, exact hostname and independently approved SPKI
// are each required before even one transaction reaches the application peer.
func TestOwnedSubmissionApprovedHttpsAuthenticatesActualPeer(t *testing.T) {
	for _, change := range []string{"none", "spki", "ordinary-trust", "hostname"} {
		func() {
			var calls atomic.Int64
			client, route, transport := newOwnedSubmissionHttpsFixture(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": testFinalizedHash})
			}), func(route *ownedSubmissionRoute) {
				if change == "spki" {
					route.TlsSpkiHash = rootObjectHash("synthetic different peer key")
				} else if change == "hostname" {
					route.RpcUrl = strings.Replace(route.RpcUrl, "rpc.transport.example", "other.transport.example", 1)
				}
			})
			if change == "ordinary-trust" {
				transport.TLSClientConfig.RootCAs = x509.NewCertPool()
			}
			hash, err := ownedSubmissionPost(t.Context(), client, route, "eth_sendRawTransaction", "0x010203", testFinalizedHash)
			if change == "none" {
				if err != nil || hash != testFinalizedHash || calls.Load() != 1 {
					t.Fatal("approved HTTPS peer failed", hash, err, calls.Load())
				}
			} else if !errors.Is(err, errRootSubmissionUncertain) || hash != "" || calls.Load() != 0 {
				t.Fatal("unauthenticated HTTPS peer received a transaction", change, hash, err, calls.Load())
			}
		}()
	}
}

// Both mutation methods preserve exactly one uncertain send. Redirects cannot
// delegate it, and overload, lost replies or bad acknowledgements cannot replay it.
func TestOwnedSubmissionApprovedHttpsDoesNotReplayWrites(t *testing.T) {
	for _, method := range []string{"author_submitExtrinsic", "eth_sendRawTransaction"} {
		for _, fault := range []string{"redirect", "redirect-retrieval", "overload", "lost-reply", "wrong-hash"} {
			func() {
				var calls, redirected atomic.Int64
				client, route, _ := newOwnedSubmissionHttpsFixture(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					if request.URL.Path != "" && request.URL.Path != "/" {
						redirected.Add(1)
						return
					}
					calls.Add(1)
					var call struct {
						Method string   `json:"method"`
						Params []string `json:"params"`
					}
					if json.NewDecoder(request.Body).Decode(&call) != nil || call.Method != method || len(call.Params) != 1 || call.Params[0] != "0x010203" {
						t.Error("original mutation body changed")
					}
					switch fault {
					case "redirect":
						http.Redirect(writer, request, "/other", http.StatusTemporaryRedirect)
					case "redirect-retrieval":
						http.Redirect(writer, request, "/other", http.StatusSeeOther)
					case "overload":
						http.Error(writer, "synthetic overload", http.StatusServiceUnavailable)
					case "lost-reply":
						connection, _, err := writer.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						connection.Close()
					case "wrong-hash":
						json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": testGenesisHash})
					}
				}))
				hash, err := ownedSubmissionPost(t.Context(), client, route, method, "0x010203", testFinalizedHash)
				if !errors.Is(err, errRootSubmissionUncertain) || hash != "" || calls.Load() != 1 || redirected.Load() != 0 {
					t.Fatal("uncertain public RPC write was lost, replayed or redirected", method, fault, hash, err, calls.Load(), redirected.Load())
				}
			}()
		}
	}
}

// Cancellation joins the real outstanding HTTPS request. It cannot spend a
// second attempt merely because the first request had no acknowledgement.
func TestOwnedSubmissionApprovedHttpsCancellationJoinsRequest(t *testing.T) {
	started, joined := make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	client, route, _ := newOwnedSubmissionHttpsFixture(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if calls.Add(1) != 1 {
			t.Error("canceled request was replayed")
			return
		}
		if _, err := io.Copy(io.Discard, request.Body); err != nil {
			t.Error("local request body unavailable", err)
		}
		close(started)
		select {
		case <-request.Context().Done():
		case <-release:
		}
		close(joined)
	}))
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := ownedSubmissionPost(ctx, client, route, "eth_sendRawTransaction", "0x010203", testFinalizedHash)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("local HTTPS request did not enter its handler")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, errRootSubmissionUncertain) {
			t.Fatal("canceled request lost its original uncertainty/cause", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("canceled local HTTPS owner did not join")
	}
	select {
	case <-joined:
	case <-time.After(10 * time.Second):
		t.Fatal("canceled local HTTPS handler did not join")
	}
	if calls.Load() != 1 {
		t.Fatal("cancellation renewed a network attempt", calls.Load())
	}
}

// New hostname plans retain the existing signed fields and authority domains.
// Neither old approvals nor newly approved routes permit a URL or pin override.
func TestBootstrapApprovedHttpsRetainsExactSignedRoute(t *testing.T) {
	endpoint, pin := "https://archive.chain.rpc-service.example", rootObjectHash("synthetic public certificate")
	native := newRootSubmissionFixture(t)
	original := native.config
	native.config.Approval.RpcUrl, native.config.Approval.TlsSpkiHash = endpoint, pin
	if native.config.validate() == nil {
		t.Fatal("original native signature approved a changed public route")
	}
	native.approve(t)
	if err := native.config.validate(); err != nil {
		t.Fatal("new independently signed public native route refused", err)
	}
	for _, change := range []string{"url", "pin"} {
		changed := native.config
		if change == "url" {
			changed.Approval.RpcUrl += ":443"
		} else {
			changed.Approval.TlsSpkiHash = rootObjectHash("another synthetic certificate")
		}
		if changed.validate() == nil {
			t.Fatal("signed native route changed", change)
		}
	}
	if err := original.validate(); err != nil {
		t.Fatal("legacy signed IP route changed", err)
	}
	evm := newEvmCreateFixture(t)
	originalEvm := copyEvmPhaseConfig(evm.config)
	evm.config.Plan.Route.RpcUrl, evm.config.Plan.Route.TlsSpkiHash = endpoint, pin
	if evm.config.validate() == nil {
		t.Fatal("original EVM signature approved a changed public route")
	}
	evm.publishConfig()
	if _, code, diagnostic := evm.command("plan"); code != 0 {
		t.Fatal("new signed public contract plan refused", code, diagnostic)
	}
	for _, change := range []string{"url", "pin"} {
		changed := copyEvmPhaseConfig(evm.config)
		if change == "url" {
			changed.Plan.Route.RpcUrl += ":443"
		} else {
			changed.Plan.Route.TlsSpkiHash = rootObjectHash("another synthetic certificate")
		}
		if changed.validate() == nil {
			t.Fatal("signed EVM route changed", change)
		}
	}
	if err := originalEvm.validate(); err != nil || len(evm.writes) != 0 || len(evm.counts) != 0 || len(native.sent()) != 0 {
		t.Fatal("public route planning changed historical authority or performed RPC", err)
	}
}
