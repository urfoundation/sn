// Native and EVM owners share the same fixed-route transport. This helper owns
// no journal, nonce or approval and never retries a network write.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"time"
)

// Public transport fields are covered by each caller's domain-specific signed
// approval. Construction cannot choose a method or grant submission authority.
type ownedSubmissionRoute struct {
	RpcUrl             string `json:"rpc_url"`
	TlsSpkiHash        string `json:"tls_spki_sha256,omitempty"`
	ReadRetrySeconds   uint32 `json:"read_retry_seconds"`
	SendTimeoutSeconds uint32 `json:"send_timeout_seconds"`
}

// An explicit canonical ip endpoint avoids mutable resolver/fallback routes.
// Https also requires normal certificate verification and an approved spki pin.
// Plain http relies on the independently approved owned network's integrity.
func (self ownedSubmissionRoute) validate() error {
	approval := self
	parsed, err := url.Parse(approval.RpcUrl)
	if err != nil || parsed == nil || parsed.String() != approval.RpcUrl || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" || parsed.RawPath != "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("root submission requires one canonical credential-free http(s) route")
	}
	address, err := netip.ParseAddr(parsed.Hostname())
	port, portErr := strconv.ParseUint(parsed.Port(), 10, 16)
	if err != nil || portErr != nil || port == 0 || address.Zone() != "" || address.IsUnspecified() || address.IsMulticast() || address.Is4In6() || parsed.Host != net.JoinHostPort(address.String(), strconv.FormatUint(port, 10)) {
		return errors.New("root submission requires one explicit canonical IP and port without dns or zone fallback")
	}
	if parsed.Scheme == "https" && !planSha256(approval.TlsSpkiHash) || parsed.Scheme == "http" && approval.TlsSpkiHash != "" {
		return errors.New("root submission tls pin is absent or attached to plaintext http")
	}
	return nil
}

// Reads and writes share only this constructor-owned fixed route. Disable
// connection reuse and body replay so net/http cannot retry an uncertain post.
func newOwnedSubmissionClient(approval ownedSubmissionRoute) (*rpcClient, error) {
	if err := approval.validate(); err != nil {
		return nil, err
	}
	client, err := newRpcClient(approval.RpcUrl, time.Duration(approval.ReadRetrySeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	parsed, _ := url.Parse(approval.RpcUrl)
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: false,
		TLSHandshakeTimeout: 30 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != parsed.Host {
				return nil, errors.New("root submission transport attempted another route")
			}
			return dialer.DialContext(ctx, network, parsed.Host)
		},
	}
	if parsed.Scheme == "https" {
		transport.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			VerifyConnection: func(state tls.ConnectionState) error {
				if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
					return errors.New("root submission peer lacks ordinary tls verification")
				}
				digest := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
				if "sha256:"+hex.EncodeToString(digest[:]) != approval.TlsSpkiHash {
					return errors.New("root submission peer certificate differs from approved spki")
				}
				return nil
			},
		}
	}
	client.httpClient.Transport = transport
	return client, nil
}

// No retry loop exists here. All errors, including overload, duplicate-pool and
// malformed replies, leave the durable attempt uncertain for canonical lookup.
func ownedSubmissionPost(ctx context.Context, client *rpcClient, route ownedSubmissionRoute, method, raw, expectedHash string) (string, error) {
	timeout := route.SendTimeoutSeconds
	if ctx == nil || client == nil || client.url != route.RpcUrl || timeout == 0 || timeout > 60 || method != "author_submitExtrinsic" && method != "eth_sendRawTransaction" {
		return "", errors.New("owned submission method, context or deadline is invalid")
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	body, err := json.Marshal(struct {
		JsonRpc string   `json:"jsonrpc"`
		Id      uint8    `json:"id"`
		Method  string   `json:"method"`
		Params  []string `json:"params"`
	}{JsonRpc: "2.0", Id: 1, Method: method, Params: []string{raw}})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, route.RpcUrl, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return "", err
	}
	request.ContentLength = int64(len(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", errors.Join(errRootSubmissionUncertain, err)
	}
	defer response.Body.Close()
	encoded, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(encoded) > 64*1024 || response.StatusCode != http.StatusOK {
		return "", errors.Join(errRootSubmissionUncertain, fmt.Errorf("root submission HTTP %d or invalid bounded reply", response.StatusCode), err)
	}
	var reply rpcReply
	if err := decodePlanJson(encoded, &reply); err != nil {
		return "", errors.Join(errRootSubmissionUncertain, err)
	}
	var hash string
	if reply.JsonRpc != "2.0" || reply.Id != 1 || reply.Error != nil || json.Unmarshal(reply.Result, &hash) != nil || hash != expectedHash {
		return "", errors.Join(errRootSubmissionUncertain, errors.New("root submission reply did not acknowledge the exact native hash"))
	}
	return hash, requestCtx.Err()
}
