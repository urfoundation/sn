// Package main keeps mainnet observation separate from signing and submission.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/urfoundation/sn/protocol"
)

const identitySchema = "urnetwork-mainnet-rpc-identity-v1"
const maxRpcReplyBytes = 1024 * 1024
const maxMetadataRpcReplyBytes = 8 * 1024 * 1024

var errRpcIntegrity = errors.New("RPC evidence is inconsistent or malformed")

// chainIdentity is one finalized, read-only observation from an owned RPC route.
type chainIdentity struct {
	Schema          string `json:"schema"`
	ObservedAt      string `json:"observed_at"`
	RpcUrl          string `json:"rpc_url"`
	NativeChain     string `json:"native_chain"`
	GenesisHash     string `json:"genesis_hash"`
	EvmChainId      uint64 `json:"evm_chain_id"`
	NodeVersion     string `json:"node_version"`
	FinalizedHash   string `json:"finalized_hash"`
	FinalizedNumber uint64 `json:"finalized_number"`
	RuntimeSpec     uint64 `json:"runtime_spec_version"`
	RuntimeTx       uint64 `json:"runtime_transaction_version"`
}

// identityEnvelope binds the observation bytes without treating them as approval.
type identityEnvelope struct {
	Identity    chainIdentity `json:"identity"`
	ContentHash string        `json:"content_hash"`
}

// identityExpectation is operator-supplied network identity, never inferred from a route.
type identityExpectation struct {
	NativeChain string
	GenesisHash string
	EvmChainId  uint64
}

// rpcClient owns a single route and bounded retry policy for read-only calls.
type rpcClient struct {
	url         string
	httpClient  *http.Client
	retryWindow time.Duration
}

// newRpcClient rejects credential-bearing and non-HTTP routes before any request.
func newRpcClient(rawUrl string, retryWindow time.Duration) (*rpcClient, error) {
	parsedUrl, err := url.Parse(rawUrl)
	if err != nil || parsedUrl == nil || (parsedUrl.Scheme != "http" && parsedUrl.Scheme != "https") || parsedUrl.Host == "" || parsedUrl.User != nil || parsedUrl.RawQuery != "" || parsedUrl.Fragment != "" {
		return nil, errors.New("RPC route must be an explicit HTTP(S) URL without credentials, query or fragment")
	}
	if retryWindow <= 0 {
		return nil, errors.New("RPC retry window must be positive")
	}
	return &rpcClient{
		url: rawUrl,
		httpClient: &http.Client{
			Transport: &http.Transport{Proxy: nil},
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		retryWindow: retryWindow,
	}, nil
}

// rpcReply carries only the JSON-RPC result; remote application errors are not guessed transient.
type rpcReply struct {
	JsonRpc string          `json:"jsonrpc"`
	Id      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// call retries transport and overload failures within one total operation budget.
func (self *rpcClient) call(ctx context.Context, method string, params []any, result any) error {
	return self.callWithStorageAbsence(ctx, method, params, result, false)
}

// Only an explicit storage reader can admit null as absence; identity and
// runtime artifact reads continue to require a concrete result.
func (self *rpcClient) callWithStorageAbsence(ctx context.Context, method string, params []any, result any, allowAbsent bool) error {
	if allowAbsent && method != "state_getStorage" {
		return errors.New("nullable results are restricted to explicit storage reads")
	}
	replyLimit := maxRpcReplyBytes
	if method == "state_getMetadata" {
		replyLimit = maxMetadataRpcReplyBytes
	}
	operationCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	requestBody, err := json.Marshal(struct {
		JsonRpc string `json:"jsonrpc"`
		Id      int    `json:"id"`
		Method  string `json:"method"`
		Params  []any  `json:"params"`
	}{JsonRpc: "2.0", Id: 1, Method: method, Params: params})
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; ; attempt++ {
		attemptCtx, attemptCancel := context.WithTimeout(operationCtx, 15*time.Second)
		request, requestErr := http.NewRequestWithContext(attemptCtx, http.MethodPost, self.url, bytes.NewReader(requestBody))
		if requestErr != nil {
			attemptCancel()
			return requestErr
		}
		request.Header.Set("Content-Type", "application/json")
		response, requestErr := self.httpClient.Do(request)
		if requestErr == nil {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, int64(replyLimit)+1))
			response.Body.Close()
			if readErr != nil {
				requestErr = readErr
			} else if len(body) > replyLimit {
				attemptCancel()
				return fmt.Errorf("%w: %s: reply exceeds %d MiB", errRpcIntegrity, method, replyLimit/(1024*1024))
			} else if response.StatusCode == http.StatusOK {
				if decodeErr := protocol.ValidateUniqueJsonKeys(body); decodeErr != nil {
					attemptCancel()
					return fmt.Errorf("%w: %s: invalid JSON document: %v", errRpcIntegrity, method, decodeErr)
				}
				var reply rpcReply
				if decodeErr := json.Unmarshal(body, &reply); decodeErr != nil {
					attemptCancel()
					return fmt.Errorf("%w: %s: invalid reply: %v", errRpcIntegrity, method, decodeErr)
				}
				if reply.JsonRpc != "2.0" || reply.Id != 1 {
					attemptCancel()
					return fmt.Errorf("%w: %s: reply has the wrong request identity", errRpcIntegrity, method)
				}
				if reply.Error != nil {
					attemptCancel()
					return fmt.Errorf("%s: RPC error %d: %s", method, reply.Error.Code, reply.Error.Message)
				}
				if len(reply.Result) == 0 || bytes.Equal(reply.Result, []byte("null")) && !allowAbsent {
					attemptCancel()
					return fmt.Errorf("%w: %s: missing result", errRpcIntegrity, method)
				}
				decodeErr := json.Unmarshal(reply.Result, result)
				attemptCancel()
				if decodeErr != nil {
					return fmt.Errorf("%w: %s: invalid result: %v", errRpcIntegrity, method, decodeErr)
				}
				return nil
			} else if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
				attemptCancel()
				return fmt.Errorf("%w: %s: HTTP %d redirect from owned route", errRpcIntegrity, method, response.StatusCode)
			} else if response.StatusCode != http.StatusRequestTimeout && response.StatusCode != http.StatusTooManyRequests && response.StatusCode != http.StatusBadGateway && response.StatusCode != http.StatusServiceUnavailable && response.StatusCode != http.StatusGatewayTimeout {
				attemptCancel()
				return fmt.Errorf("%s: HTTP %d", method, response.StatusCode)
			} else {
				requestErr = fmt.Errorf("HTTP %d", response.StatusCode)
			}
		}
		attemptCancel()
		lastErr = requestErr
		if operationCtx.Err() != nil {
			return fmt.Errorf("%s: retry window exhausted: %w", method, errors.Join(operationCtx.Err(), lastErr))
		}
		delay := time.Duration(attempt+1) * 500 * time.Millisecond
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
		select {
		case <-operationCtx.Done():
			return fmt.Errorf("%s: retry window exhausted: %w", method, errors.Join(operationCtx.Err(), lastErr))
		case <-time.After(delay):
		}
	}
}

// readIdentity binds the finalized header to its hash and records runtime metadata.
func (self *rpcClient) readIdentity(ctx context.Context) (chainIdentity, error) {
	sampleCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	identity := chainIdentity{Schema: identitySchema, RpcUrl: self.url}
	var evmChainHex string
	var header struct {
		Number string `json:"number"`
	}
	var runtime struct {
		SpecVersion        uint64 `json:"specVersion"`
		TransactionVersion uint64 `json:"transactionVersion"`
	}
	for _, step := range []struct {
		method string
		params []any
		result any
	}{
		{"system_chain", []any{}, &identity.NativeChain},
		{"chain_getBlockHash", []any{0}, &identity.GenesisHash},
		{"eth_chainId", []any{}, &evmChainHex},
		{"system_version", []any{}, &identity.NodeVersion},
		{"chain_getFinalizedHead", []any{}, &identity.FinalizedHash},
	} {
		if err := self.call(sampleCtx, step.method, step.params, step.result); err != nil {
			return chainIdentity{}, err
		}
	}
	if err := self.call(sampleCtx, "chain_getHeader", []any{identity.FinalizedHash}, &header); err != nil {
		return chainIdentity{}, err
	}
	if err := self.call(sampleCtx, "state_getRuntimeVersion", []any{identity.FinalizedHash}, &runtime); err != nil {
		return chainIdentity{}, err
	}
	var err error
	identity.EvmChainId, err = parseHexNumber(evmChainHex)
	if err != nil {
		return chainIdentity{}, fmt.Errorf("%w: EVM chain ID: %v", errRpcIntegrity, err)
	}
	identity.FinalizedNumber, err = parseHexNumber(header.Number)
	if err != nil {
		return chainIdentity{}, fmt.Errorf("%w: finalized block number: %v", errRpcIntegrity, err)
	}
	identity.RuntimeSpec, identity.RuntimeTx = runtime.SpecVersion, runtime.TransactionVersion
	if !validHash(identity.GenesisHash) || !validHash(identity.FinalizedHash) || identity.NativeChain == "" || identity.NodeVersion == "" || identity.RuntimeSpec == 0 {
		return chainIdentity{}, fmt.Errorf("%w: identity is incomplete", errRpcIntegrity)
	}
	var byNumberHash string
	if err := self.call(sampleCtx, "chain_getBlockHash", []any{identity.FinalizedNumber}, &byNumberHash); err != nil {
		return chainIdentity{}, err
	}
	if !validHash(byNumberHash) || !strings.EqualFold(byNumberHash, identity.FinalizedHash) {
		return chainIdentity{}, fmt.Errorf("%w: finalized header number does not resolve to its announced hash", errRpcIntegrity)
	}
	identity.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return identity, nil
}

// priorFinalizedMatches checks that a newly advanced head retains the prior
// finalized block. A height increase alone cannot prove chain continuity.
func (self *rpcClient) priorFinalizedMatches(ctx context.Context, state *monitorState, identity chainIdentity) (bool, error) {
	if state == nil || state.lastHash == "" || identity.FinalizedNumber <= state.lastNumber {
		return true, nil
	}
	var oldHash string
	if err := self.call(ctx, "chain_getBlockHash", []any{state.lastNumber}, &oldHash); err != nil {
		return false, err
	}
	if !validHash(oldHash) {
		return false, fmt.Errorf("%w: invalid prior finalized block hash", errRpcIntegrity)
	}
	return strings.EqualFold(oldHash, state.lastHash), nil
}

// match rejects an endpoint substitution even when one of its identity fields agrees.
func (self identityExpectation) match(identity chainIdentity) error {
	if self.NativeChain == "" || !validHash(self.GenesisHash) || self.EvmChainId == 0 {
		return errors.New("approved network identity is incomplete")
	}
	if identity.NativeChain != self.NativeChain || !strings.EqualFold(identity.GenesisHash, self.GenesisHash) || identity.EvmChainId != self.EvmChainId {
		return fmt.Errorf("RPC identity mismatch: chain=%q genesis=%s EVM=%d; expected chain=%q genesis=%s EVM=%d", identity.NativeChain, identity.GenesisHash, identity.EvmChainId, self.NativeChain, self.GenesisHash, self.EvmChainId)
	}
	return nil
}

// sealIdentity computes a content hash over the complete unsealed observation.
func sealIdentity(identity chainIdentity) (identityEnvelope, error) {
	raw, err := json.Marshal(identity)
	if err != nil {
		return identityEnvelope{}, err
	}
	hash := sha256.Sum256(raw)
	return identityEnvelope{Identity: identity, ContentHash: "sha256:" + hex.EncodeToString(hash[:])}, nil
}

// parseHexNumber accepts canonical JSON-RPC hex integers, not decimal strings.
func parseHexNumber(value string) (uint64, error) {
	if !strings.HasPrefix(value, "0x") || len(value) <= 2 {
		return 0, errors.New("expected 0x-prefixed integer")
	}
	return strconv.ParseUint(value[2:], 16, 64)
}

// validHash accepts an exact 32-byte hexadecimal chain hash.
func validHash(value string) bool {
	if !strings.HasPrefix(value, "0x") || len(value) != 66 {
		return false
	}
	_, err := hex.DecodeString(value[2:])
	return err == nil
}
