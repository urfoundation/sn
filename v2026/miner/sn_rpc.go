package miner

// sn_rpc.go — the minimal read-only JSON-RPC eth_call client behind
// `provider claim` and `provider bind-head` (sn/PLAN.md 7.3, decision D-6).
// The ABI encoding of the calls this sends is built with sn/stabi; only the
// http transport lives here. Reads (noCommit payout root, headBindDigest) go
// through this stdlib client; signing and submission go through
// sn/miner/onchain (go-ethereum). This is deliberately the read path only —
// it is not the duplication target the stabi/merkle/ss58 packages replaced.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Bound each individual request and the complete decoded response body.
const (
	ethRpcTimeout       = 15 * time.Second
	ethRpcResponseLimit = 1024 * 1024
)

// Read-only calls recover transient transport failures within one finite
// budget. Both allowed methods return canonical 0x-prefixed hex strings.
func ethRpcHexResult(ctx context.Context, rpcUrl string, method string, params []any) (string, error) {
	return ethRpcHexResultWithClient(ctx, http.DefaultClient, rpcUrl, method, params)
}

// The caller retains ownership of the client. Retry state belongs to this
// operation and cannot affect another command's deadline or transport.
func ethRpcHexResultWithClient(ctx context.Context, client *http.Client, rpcUrl string, method string, params []any) (string, error) {
	return ethRpcHexResultWithRetry(ctx, client, rpcUrl, method, params, ethRpcRetryHooks{})
}

// Authenticate the configured chain before asking it for contract state. A
// wrong-chain node never gains a view request or transient failover authority.
func ethRpcHexView(ctx context.Context, rpcUrl string, expectedChainId uint64, params []any) ([]byte, error) {
	chainIdHex, err := ethRpcHexResult(ctx, rpcUrl, "eth_chainId", []any{})
	if err != nil {
		return nil, err
	}
	chainId, err := parseEthHexQuantity(chainIdHex)
	if err != nil {
		return nil, err
	}
	if chainId != expectedChainId {
		return nil, fmt.Errorf("%s chain id %d differs from expected %d", rpcUrl, chainId, expectedChainId)
	}
	result, err := ethRpcHexResult(ctx, rpcUrl, "eth_call", params)
	if err != nil {
		return nil, err
	}
	return parseEthHexBytes(result)
}

// Marshal once before retry admission so every attempt retains the same
// method, selector, contract and calldata, even if caller data later changes.
func ethRpcHexResultWithRetry(ctx context.Context, client *http.Client, rpcUrl string, method string, params []any, hooks ethRpcRetryHooks) (string, error) {
	if ctx == nil || client == nil || (method != "eth_chainId" && method != "eth_call") {
		return "", errors.New("EVM read-only RPC owner or method is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	requestBodyBytes, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return "", err
	}
	return retryEthRpcRead(ctx, hooks, func(attemptCtx context.Context) (string, error) {
		return ethRpcHexRequest(attemptCtx, client, rpcUrl, method, requestBodyBytes)
	})
}

// Each attempt owns its complete response and closes it before retrying or
// returning any result. Only physical transport errors receive retry origin.
func ethRpcHexRequest(ctx context.Context, client *http.Client, rpcUrl string, method string, requestBodyBytes []byte) (string, error) {
	requestCtx, requestCancel := context.WithTimeout(ctx, ethRpcTimeout)
	defer requestCancel()
	request, err := http.NewRequestWithContext(requestCtx, "POST", rpcUrl, bytes.NewReader(requestBodyBytes))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	// A configured endpoint is read authority; redirects must not substitute
	// another node even when it returns a plausible chain id and result.
	requestClient := *client
	requestClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := requestClient.Do(request)
	if err != nil {
		return "", &ethRpcTransportError{cause: err}
	}
	if response.StatusCode != http.StatusOK {
		return "", errors.Join(&ethRpcStatusError{method: method, status: response.StatusCode}, response.Body.Close(), requestCtx.Err())
	}
	if response.ContentLength > ethRpcResponseLimit {
		return "", errors.Join(fmt.Errorf("%s: RPC response exceeds %d bytes", method, ethRpcResponseLimit), response.Body.Close(), requestCtx.Err())
	}
	responseBodyBytes, readErr := io.ReadAll(io.LimitReader(response.Body, ethRpcResponseLimit+1))
	if readErr != nil {
		readErr = &ethRpcTransportError{cause: readErr}
	}
	if len(responseBodyBytes) > ethRpcResponseLimit {
		readErr = errors.Join(readErr, fmt.Errorf("%s: RPC response exceeds %d bytes", method, ethRpcResponseLimit))
	}
	if err := errors.Join(readErr, response.Body.Close(), requestCtx.Err()); err != nil {
		return "", err
	}
	if err := protocol.ValidateUniqueJsonKeys(responseBodyBytes); err != nil {
		return "", fmt.Errorf("%s: malformed JSON-RPC response: %v", method, err)
	}
	var rpcResponse struct {
		Version string          `json:"jsonrpc"`
		Id      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(responseBodyBytes, &rpcResponse); err != nil {
		return "", fmt.Errorf("%s: bad json-rpc response: %w", method, err)
	}
	if rpcResponse.Version != "2.0" || !bytes.Equal(rpcResponse.Id, []byte("1")) || (len(rpcResponse.Result) == 0) == (len(rpcResponse.Error) == 0) {
		return "", fmt.Errorf("%s: JSON-RPC response identity or result/error envelope differs from the request", method)
	}
	if len(rpcResponse.Error) != 0 {
		var rpcError *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rpcResponse.Error, &rpcError); err != nil || rpcError == nil {
			return "", fmt.Errorf("%s: malformed JSON-RPC error", method)
		}
		return "", fmt.Errorf("%s: rpc error %d: %s", method, rpcError.Code, rpcError.Message)
	}
	var hexResult string
	if err := json.Unmarshal(rpcResponse.Result, &hexResult); err != nil {
		return "", fmt.Errorf("%s: non-string result", method)
	}
	if method == "eth_chainId" {
		_, err = parseEthHexQuantity(hexResult)
	} else {
		_, err = parseEthHexBytes(hexResult)
	}
	if err != nil {
		return "", fmt.Errorf("%s: malformed hexadecimal result: %w", method, err)
	}
	if err := requestCtx.Err(); err != nil {
		return "", err
	}
	return hexResult, nil
}

// parseEthHexQuantity parses a 0x-prefixed json-rpc quantity such as the
// eth_chainId result.
func parseEthHexQuantity(hexQuantity string) (uint64, error) {
	if !strings.HasPrefix(hexQuantity, "0x") {
		return 0, hexutil.ErrMissingPrefix
	}
	return hexutil.DecodeUint64(hexQuantity)
}

// parseEthHexBytes parses 0x-prefixed hex data such as the eth_call return
// data.
func parseEthHexBytes(hexData string) ([]byte, error) {
	if !strings.HasPrefix(hexData, "0x") {
		return nil, hexutil.ErrMissingPrefix
	}
	return hexutil.Decode(hexData)
}
