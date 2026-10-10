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
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

const identitySchema = "urnetwork-mainnet-rpc-identity-v1"
const maxRpcReplyBytes = 1024 * 1024
const maxMetadataRpcReplyBytes = 8 * 1024 * 1024

var errRpcIntegrity = errors.New("RPC evidence is inconsistent or malformed")
var errRpcIdentityMismatch = errors.New("RPC identity mismatch")
var errRpcObservationUnavailable = errors.New("RPC observation is unavailable")

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
	// Preserve the complete live tuple for artifact cross-reads without changing
	// the existing observation schema. It is never reconstructed as authority.
	runtimeVersion crv4.RuntimeVersionIdentity
	// The original finality witness is independent of a historical selection.
	// Like the runtime tuple, it is live evidence and never imported from JSON.
	finalityWitness nativeFinalityPoint
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
	retryWait   func(context.Context, time.Duration) error
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

// rpcReply retains exact request identity and distinguishes explicit server timeouts.
type rpcReply struct {
	JsonRpc string          `json:"jsonrpc"`
	Id      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Retains the method and exact server code so optional read capabilities can
// distinguish an unsupported method without parsing display text.
type rpcCallError struct {
	method  string
	code    int
	message string
}

// Preserves the established RPC diagnostic spelling for existing callers.
func (self *rpcCallError) Error() string {
	return fmt.Sprintf("%s: RPC error %d: %s", self.method, self.code, self.message)
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
	return self.callBoundedRead(ctx, method, params, result, allowAbsent, replyLimit)
}

// Larger archive replies are opt-in and still bounded. Every invocation owns
// one finite read-retry budget; callers cannot use this helper for submission.
func (self *rpcClient) callBoundedRead(ctx context.Context, method string, params []any, result any, allowAbsent bool, replyLimit int) error {
	if ctx == nil || replyLimit <= 0 || replyLimit > 2*rootBodyBytesLimit+maxRpcReplyBytes || allowAbsent && method != "state_getStorage" {
		return errors.New("invalid bounded read budget or nullable method")
	}
	switch method {
	case "system_chain", "system_version", "eth_chainId", "chain_getBlockHash", "chain_getFinalizedHead", "chain_getHeader", "chain_getBlock", "state_getRuntimeVersion", "state_getMetadata", "state_getStorageHash", "state_getStorage", "state_getKeysPaged":
	default:
		return errors.New("RPC method is outside the read-only mainnet profile")
	}
	return self.callAdmittedRead(ctx, method, params, result, allowAbsent, replyLimit)
}

// Only explicit read profiles call this transport. The ordinary profile above
// keeps its existing whitelist; specialized observations add no global methods.
func (self *rpcClient) callAdmittedRead(ctx context.Context, method string, params []any, result any, allowAbsent bool, replyLimit int) error {
	return self.callAdmittedReadResult(ctx, method, params, result, allowAbsent, false, replyLimit)
}

// A separately selected retained-fact profile retries null without treating it
// as an observation of changed history. Other callers keep their null grammar.
func (self *rpcClient) callAdmittedReadResult(ctx context.Context, method string, params []any, result any, allowAbsent, retryAbsent bool, replyLimit int) error {
	if ctx == nil || replyLimit <= 0 || replyLimit > 2*rootBodyBytesLimit+maxRpcReplyBytes {
		return errors.New("invalid bounded read budget")
	}
	operationCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	exhausted := func(err error) error {
		if retryAbsent {
			err = errors.Join(errRpcObservationUnavailable, err)
		}
		return fmt.Errorf("%s: retry window exhausted: %w", method, err)
	}
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
		delay := min(time.Duration(attempt+1)*500*time.Millisecond, 5*time.Second)
		attemptCtx, attemptCancel := context.WithTimeout(operationCtx, min(self.retryWindow, 60*time.Second))
		request, requestErr := http.NewRequestWithContext(attemptCtx, http.MethodPost, self.url, bytes.NewReader(requestBody))
		if requestErr != nil {
			attemptCancel()
			return requestErr
		}
		request.Header.Set("Content-Type", "application/json")
		response, requestErr := self.httpClient.Do(request)
		if requestErr != nil {
			if !rpcReadTransportMayRetry(requestErr) {
				attemptCancel()
				return errors.Join(requestErr, operationCtx.Err())
			}
		} else {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, int64(replyLimit)+1))
			closeErr := response.Body.Close()
			requestErr = errors.Join(readErr, closeErr, attemptCtx.Err())
			if len(body) > replyLimit {
				attemptCancel()
				return errors.Join(fmt.Errorf("%w: %s: reply exceeds %d MiB", errRpcIntegrity, method, replyLimit/(1024*1024)), requestErr)
			}
			if requestErr != nil {
				// An interrupted body cannot erase a known terminal status.
				if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
					requestErr = errors.Join(fmt.Errorf("%w: %s: HTTP %d redirect from owned route", errRpcIntegrity, method, response.StatusCode), requestErr)
				} else if response.StatusCode != http.StatusOK && !rpcReadRetryStatus(response.StatusCode) {
					requestErr = errors.Join(fmt.Errorf("%s: HTTP %d", method, response.StatusCode), requestErr)
				}
				if !rpcReadTransportMayRetry(requestErr) {
					attemptCancel()
					return errors.Join(requestErr, operationCtx.Err())
				}
			}
			if readErr == nil && response.StatusCode == http.StatusOK {
				if decodeErr := protocol.ValidateUniqueJsonKeys(body); decodeErr != nil {
					attemptCancel()
					return errors.Join(fmt.Errorf("%w: %s: invalid JSON document: %v", errRpcIntegrity, method, decodeErr), requestErr)
				}
				var reply rpcReply
				if decodeErr := json.Unmarshal(body, &reply); decodeErr != nil {
					attemptCancel()
					return errors.Join(fmt.Errorf("%w: %s: invalid reply: %v", errRpcIntegrity, method, decodeErr), requestErr)
				}
				if reply.JsonRpc != "2.0" || reply.Id != 1 {
					attemptCancel()
					return errors.Join(fmt.Errorf("%w: %s: reply has the wrong request identity", errRpcIntegrity, method), requestErr)
				}
				if reply.Error != nil {
					if len(reply.Result) != 0 && !bytes.Equal(reply.Result, []byte("null")) {
						attemptCancel()
						return errors.Join(fmt.Errorf("%w: %s: reply has both result and error", errRpcIntegrity, method), requestErr)
					}
					requestErr = errors.Join(&rpcCallError{method: method, code: reply.Error.Code, message: reply.Error.Message}, requestErr)
					if !rpcTransientReadError(reply.Error.Code, reply.Error.Message) {
						attemptCancel()
						return requestErr
					}
				} else if retryAbsent && bytes.Equal(reply.Result, []byte("null")) {
					requestErr = errors.Join(fmt.Errorf("%w: %s did not return the retained fact", errRpcObservationUnavailable, method), requestErr)
				} else {
					if len(reply.Result) == 0 || bytes.Equal(reply.Result, []byte("null")) && !allowAbsent {
						attemptCancel()
						return errors.Join(fmt.Errorf("%w: %s: missing result", errRpcIntegrity, method), requestErr)
					}
					decodeTarget := result
					if requestErr != nil {
						// Validate typed contradictions without publishing fields
						// from a response whose physical close still needs retry.
						value := reflect.ValueOf(result)
						if value.IsValid() && value.Kind() == reflect.Pointer && !value.IsNil() {
							decodeTarget = reflect.New(value.Type().Elem()).Interface()
						}
					}
					decodeErr := json.Unmarshal(reply.Result, decodeTarget)
					if decodeErr != nil {
						attemptCancel()
						return errors.Join(fmt.Errorf("%w: %s: invalid result: %v", errRpcIntegrity, method, decodeErr), requestErr, operationCtx.Err())
					}
					if requestErr == nil {
						requestErr = attemptCtx.Err()
						if requestErr == nil {
							attemptCancel()
							return operationCtx.Err()
						}
					}
				}
			} else if readErr == nil && response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
				attemptCancel()
				return errors.Join(fmt.Errorf("%w: %s: HTTP %d redirect from owned route", errRpcIntegrity, method, response.StatusCode), requestErr)
			} else if readErr == nil && !rpcReadRetryStatus(response.StatusCode) {
				attemptCancel()
				return errors.Join(fmt.Errorf("%s: HTTP %d", method, response.StatusCode), requestErr)
			} else if readErr == nil {
				requestErr = errors.Join(fmt.Errorf("HTTP %d", response.StatusCode), requestErr)
				deadline, _ := operationCtx.Deadline()
				delay = rpcReadRetryDelay(response.Header, body, delay, time.Now(), deadline)
			}
		}
		attemptCancel()
		lastErr = requestErr
		if operationCtx.Err() != nil {
			return exhausted(errors.Join(operationCtx.Err(), lastErr))
		}
		wait := self.retryWait
		if wait == nil {
			wait = waitRpcReadRetry
		}
		if err := wait(operationCtx, delay); err != nil || operationCtx.Err() != nil {
			return exhausted(errors.Join(operationCtx.Err(), err, lastErr))
		}
	}
}

// readIdentity binds the finalized header to its hash and records runtime metadata.
func (self *rpcClient) readIdentity(ctx context.Context) (chainIdentity, error) {
	return self.readIdentityAt(ctx, "")
}

// A retained block must still be canonical and no later than the authenticated
// finalized head. Empty selects that head; neither mode proves RPC consensus.
func (self *rpcClient) readIdentityAt(ctx context.Context, blockHash string) (chainIdentity, error) {
	if blockHash != "" && !rootCanonicalHash(blockHash) {
		return chainIdentity{}, fmt.Errorf("%w: retained block hash is invalid", errRpcIntegrity)
	}
	sampleCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	identity := chainIdentity{Schema: identitySchema, RpcUrl: self.url}
	var evmChainHex string
	var header rootReceiptHeader
	var rawVersion json.RawMessage
	for _, step := range []struct {
		method string
		params []any
		result any
	}{
		{method: "system_chain", params: []any{}, result: &identity.NativeChain},
		{method: "chain_getBlockHash", params: []any{0}, result: &identity.GenesisHash},
		{method: "eth_chainId", params: []any{}, result: &evmChainHex},
		{method: "system_version", params: []any{}, result: &identity.NodeVersion},
		{method: "chain_getFinalizedHead", params: []any{}, result: &identity.FinalizedHash},
	} {
		if err := self.call(sampleCtx, step.method, step.params, step.result); err != nil {
			return chainIdentity{}, err
		}
	}
	if err := self.call(sampleCtx, "chain_getHeader", []any{identity.FinalizedHash}, &header); err != nil {
		return chainIdentity{}, err
	}
	// Hash spelling is not authority; normalize equivalent hex before hashing
	// the complete header, including its roots and bounded digest payloads.
	identity.GenesisHash = strings.ToLower(identity.GenesisHash)
	identity.FinalizedHash = strings.ToLower(identity.FinalizedHash)
	header.normalizeHashes()
	var err error
	identity.FinalizedNumber, err = header.authenticate(identity.FinalizedHash)
	if err != nil {
		return chainIdentity{}, fmt.Errorf("%w: finalized header: %v", errRpcIntegrity, err)
	}
	finalized := nativeFinalityPoint{Number: identity.FinalizedNumber, Hash: identity.FinalizedHash}
	if blockHash != "" && blockHash != identity.FinalizedHash {
		var finalizedByNumber string
		if err := self.call(sampleCtx, "chain_getBlockHash", []any{identity.FinalizedNumber}, &finalizedByNumber); err != nil {
			return chainIdentity{}, err
		}
		if !validHash(finalizedByNumber) || !strings.EqualFold(finalizedByNumber, identity.FinalizedHash) {
			return chainIdentity{}, fmt.Errorf("%w: finalized head is not canonical before historical read", errRpcIntegrity)
		}
		var retainedHeader rootReceiptHeader
		if err := self.call(sampleCtx, "chain_getHeader", []any{blockHash}, &retainedHeader); err != nil {
			return chainIdentity{}, err
		}
		retainedHeader.normalizeHashes()
		number, err := retainedHeader.authenticate(blockHash)
		if err != nil {
			return chainIdentity{}, fmt.Errorf("%w: retained header is invalid: %v", errRpcIntegrity, err)
		}
		if number >= identity.FinalizedNumber {
			finalized, err = self.readNativeFinalityCovering(sampleCtx, finalized, nativeFinalityPoint{Number: number, Hash: blockHash})
			if err != nil {
				return chainIdentity{}, err
			}
		}
		identity.FinalizedHash, identity.FinalizedNumber = blockHash, number
	}
	if err := self.call(sampleCtx, "state_getRuntimeVersion", []any{identity.FinalizedHash}, &rawVersion); err != nil {
		return chainIdentity{}, err
	}
	runtime, err := crv4.DecodeRuntimeVersionIdentity(rawVersion)
	if err != nil {
		return chainIdentity{}, fmt.Errorf("%w: finalized runtime version: %v", errRpcIntegrity, err)
	}
	identity.EvmChainId, err = parseHexNumber(evmChainHex)
	if err != nil {
		return chainIdentity{}, fmt.Errorf("%w: EVM chain ID: %v", errRpcIntegrity, err)
	}
	identity.RuntimeSpec, identity.RuntimeTx = uint64(runtime.SpecVersion), uint64(runtime.TransactionVersion)
	identity.runtimeVersion = runtime
	if !validHash(identity.GenesisHash) || !validHash(identity.FinalizedHash) || identity.NativeChain == "" || identity.NodeVersion == "" || identity.RuntimeSpec == 0 {
		return chainIdentity{}, fmt.Errorf("%w: identity is incomplete", errRpcIntegrity)
	}
	if err := self.closeNativeFinality(sampleCtx, finalized, nativeFinalityPoint{Number: identity.FinalizedNumber, Hash: identity.FinalizedHash}); err != nil {
		return chainIdentity{}, err
	}
	identity.finalityWitness = finalized
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
		return fmt.Errorf("%w: chain=%q genesis=%s EVM=%d; expected chain=%q genesis=%s EVM=%d", errRpcIdentityMismatch, identity.NativeChain, identity.GenesisHash, identity.EvmChainId, self.NativeChain, self.GenesisHash, self.EvmChainId)
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

// Only recognized read-timeout responses are retryable. Archive pruning,
// unknown runtime/method errors and integrity failures remain visible.
func rpcTransientReadError(code int, message string) bool {
	if code != -32000 && code != -32001 && code != -32002 && code != -32603 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(message)) {
	case "request timeout", "request timed out", "rpc timeout", "query timeout", "timeout":
		return true
	}
	return false
}
