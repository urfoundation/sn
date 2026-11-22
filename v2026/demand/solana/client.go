package solana

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"
	"time"
)

// Shapes were checked against api.mainnet-beta.solana.com (apiVersion 4.3)
// on 2026-10-22.

const (
	userAgent = "urnetwork-sn-demand"
	// Responses are small JSON documents; anything larger is a fault, not data.
	maxResponseBytes = 8 << 20
	maxErrorBody     = 512
	// A 429's Retry-After is honored up to this cap.
	maxRetryAfter = 5 * time.Second
)

// Client calls one Solana JSON-RPC endpoint. A read is retried once on 429, 5xx
// or a transport error; SendTransaction is not, since its outcome is unknown
// until its signature is looked up. It is safe for concurrent use.
type Client struct {
	endpoint string
	http     *http.Client
	ids      atomic.Uint64

	// Waits before the single retry; tests replace it.
	sleep func(ctx context.Context, d time.Duration) error
	retry time.Duration
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient replaces the default 30-second-timeout client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// Redact is endpoint without its path or query, where a provider puts its
// key: what errors and logs may name.
func Redact(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "(endpoint)"
	}
	return u.Scheme + "://" + u.Host
}

// NewClient builds a client for a JSON-RPC endpoint URL.
func NewClient(endpoint string, opts ...Option) *Client {
	c := &Client{
		endpoint: endpoint,
		http:     &http.Client{Timeout: 30 * time.Second},
		sleep:    sleepContext,
		retry:    500 * time.Millisecond,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// RPCError is a JSON-RPC error object, for example -32015 for a transaction
// version the request did not allow.
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("solana: rpc error %d: %s", e.Code, e.Message)
}

// HTTPError is a non-2xx response. Body is truncated to 512 bytes.
type HTTPError struct {
	Status int
	Method string
	Body   string

	retryAfter string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("solana: %s: HTTP %d: %s", e.Method, e.Status, e.Body)
}

func (c *Client) call(ctx context.Context, method string, params []any, out any) error {
	err := c.once(ctx, method, params, out)
	var httpErr *HTTPError
	var transportErr *transportError
	switch {
	case errors.As(err, &httpErr) && (httpErr.Status == http.StatusTooManyRequests || httpErr.Status >= 500):
	case errors.As(err, &transportErr):
	default:
		return err
	}
	wait := c.retry
	if httpErr != nil && httpErr.retryAfter != "" {
		if seconds, parseErr := strconv.Atoi(httpErr.retryAfter); parseErr == nil && seconds >= 0 {
			wait = time.Duration(seconds) * time.Second
		}
	}
	if sleepErr := c.sleep(ctx, min(wait, maxRetryAfter)); sleepErr != nil {
		return errors.Join(err, sleepErr)
	}
	return c.once(ctx, method, params, out)
}

type transportError struct {
	method string
	err    error
}

func (e *transportError) Error() string { return fmt.Sprintf("solana: %s: %v", e.method, e.err) }

func (e *transportError) Unwrap() error { return e.err }

func (c *Client) once(ctx context.Context, method string, params []any, out any) error {
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.ids.Add(1), "method": method, "params": params})
	if err != nil {
		return fmt.Errorf("solana: encode %s: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("solana: build %s: bad endpoint %s", method, Redact(c.endpoint))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("solana: %s: %w", method, ctxErr)
		}
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			urlErr.URL = Redact(urlErr.URL)
		}
		return &transportError{method: method, err: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return &transportError{method: method, err: fmt.Errorf("read response: %w", err)}
	}
	if len(raw) > maxResponseBytes {
		return fmt.Errorf("solana: %s: response exceeds %d bytes", method, maxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body := string(raw)
		if len(body) > maxErrorBody {
			body = body[:maxErrorBody]
		}
		return &HTTPError{Status: resp.StatusCode, Method: method, Body: body, retryAfter: resp.Header.Get("Retry-After")}
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("solana: decode %s: %w", method, err)
	}
	if envelope.Error != nil {
		return &RPCError{Code: envelope.Error.Code, Message: envelope.Error.Message}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		return fmt.Errorf("solana: decode %s result: %w", method, err)
	}
	return nil
}

// Balance is account's lamports at confirmed commitment.
func (c *Client) Balance(ctx context.Context, account PublicKey) (uint64, error) {
	var out struct {
		Value uint64 `json:"value"`
	}
	if err := c.call(ctx, "getBalance", []any{account.String(), map[string]any{"commitment": "confirmed"}}, &out); err != nil {
		return 0, err
	}
	return out.Value, nil
}

// BlockHeight is the confirmed block height, the clock lastValidBlockHeight
// is measured against.
func (c *Client) BlockHeight(ctx context.Context) (uint64, error) {
	var out uint64
	if err := c.call(ctx, "getBlockHeight", []any{map[string]any{"commitment": "confirmed"}}, &out); err != nil {
		return 0, err
	}
	return out, nil
}

// TokenAccount is an SPL token account as jsonParsed reports it.
type TokenAccount struct {
	Address         PublicKey
	Mint            PublicKey
	Owner           PublicKey
	Amount          uint64
	Decimals        uint8
	Delegate        *PublicKey
	DelegatedAmount uint64
	CloseAuthority  *PublicKey
	State           string
}

// jsonAccount is one account in jsonParsed encoding. Data is either an object
// (parsed) or a [base64, "base64"] pair.
type jsonAccount struct {
	Lamports   uint64          `json:"lamports"`
	Owner      string          `json:"owner"`
	Executable bool            `json:"executable"`
	Data       json.RawMessage `json:"data"`
}

type parsedTokenAccount struct {
	Program string `json:"program"`
	Parsed  struct {
		Type string `json:"type"`
		Info struct {
			Mint            string        `json:"mint"`
			Owner           string        `json:"owner"`
			State           string        `json:"state"`
			TokenAmount     parsedAmount  `json:"tokenAmount"`
			Delegate        string        `json:"delegate"`
			DelegatedAmount *parsedAmount `json:"delegatedAmount"`
			CloseAuthority  string        `json:"closeAuthority"`
		} `json:"info"`
	} `json:"parsed"`
}

type parsedAmount struct {
	Amount   string `json:"amount"`
	Decimals uint8  `json:"decimals"`
}

// tokenAccount decodes a jsonParsed account; ok is false when it is not an
// SPL token account.
func (a *jsonAccount) tokenAccount(address PublicKey) (*TokenAccount, bool, error) {
	if a.Owner != TokenProgram.String() && a.Owner != Token2022Program.String() {
		return nil, false, nil
	}
	var parsed parsedTokenAccount
	if err := json.Unmarshal(a.Data, &parsed); err != nil || parsed.Parsed.Type != "account" {
		return nil, false, nil
	}
	info := parsed.Parsed.Info
	t := &TokenAccount{Address: address, State: info.State, Decimals: info.TokenAmount.Decimals}
	var err error
	if t.Mint, err = ParsePublicKey(info.Mint); err != nil {
		return nil, true, err
	}
	if t.Owner, err = ParsePublicKey(info.Owner); err != nil {
		return nil, true, err
	}
	if t.Amount, err = strconv.ParseUint(info.TokenAmount.Amount, 10, 64); err != nil {
		return nil, true, fmt.Errorf("solana: token amount %q: %w", info.TokenAmount.Amount, err)
	}
	if t.Delegate, err = optionalKey(info.Delegate); err != nil {
		return nil, true, err
	}
	if info.DelegatedAmount != nil && info.DelegatedAmount.Amount != "" {
		if t.DelegatedAmount, err = strconv.ParseUint(info.DelegatedAmount.Amount, 10, 64); err != nil {
			return nil, true, fmt.Errorf("solana: delegated amount %q: %w", info.DelegatedAmount.Amount, err)
		}
	}
	if t.CloseAuthority, err = optionalKey(info.CloseAuthority); err != nil {
		return nil, true, err
	}
	return t, true, nil
}

func optionalKey(s string) (*PublicKey, error) {
	if s == "" {
		return nil, nil
	}
	p, err := ParsePublicKey(s)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// TokenAccount reads one token account; (nil, nil) when the account does not
// exist, an error when it exists but is not a token account.
func (c *Client) TokenAccount(ctx context.Context, address PublicKey) (*TokenAccount, error) {
	var out struct {
		Value *jsonAccount `json:"value"`
	}
	if err := c.call(ctx, "getAccountInfo", []any{address.String(), map[string]any{"encoding": "jsonParsed", "commitment": "confirmed"}}, &out); err != nil {
		return nil, err
	}
	if out.Value == nil {
		return nil, nil
	}
	t, ok, err := out.Value.tokenAccount(address)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("solana: %s is not a token account (owner %s)", address, out.Value.Owner)
	}
	return t, nil
}

// Account is an account's raw contents.
type Account struct {
	Owner      PublicKey
	Lamports   uint64
	Data       []byte
	Executable bool
}

// Account reads address in base64; (nil, nil) when it does not exist.
func (c *Client) Account(ctx context.Context, address PublicKey) (*Account, error) {
	config := map[string]any{"encoding": "base64", "commitment": "confirmed"}
	var out struct {
		Value *jsonAccount `json:"value"`
	}
	if err := c.call(ctx, "getAccountInfo", []any{address.String(), config}, &out); err != nil {
		return nil, err
	}
	if out.Value == nil {
		return nil, nil
	}
	return out.Value.raw()
}

// dataLen is the size of an account's data in jsonParsed encoding: a [base64,
// "base64"] pair, or for a program it can parse, an object naming its space. What
// it cannot read counts as holding data.
func (a *jsonAccount) dataLen() int {
	var pair []string
	if err := json.Unmarshal(a.Data, &pair); err == nil && len(pair) == 2 && pair[1] == "base64" {
		if data, err := base64.StdEncoding.DecodeString(pair[0]); err == nil {
			return len(data)
		}
		return len(a.Data)
	}
	var parsed struct {
		Space *int `json:"space"`
	}
	if err := json.Unmarshal(a.Data, &parsed); err == nil && parsed.Space != nil {
		return *parsed.Space
	}
	return len(a.Data)
}

func (a *jsonAccount) raw() (*Account, error) {
	owner, err := ParsePublicKey(a.Owner)
	if err != nil {
		return nil, err
	}
	var pair []string
	if err := json.Unmarshal(a.Data, &pair); err != nil || len(pair) != 2 || pair[1] != "base64" {
		return nil, errors.New("solana: account data is not base64-encoded")
	}
	data, err := base64.StdEncoding.DecodeString(pair[0])
	if err != nil {
		return nil, fmt.Errorf("solana: account data: %w", err)
	}
	return &Account{Owner: owner, Lamports: a.Lamports, Data: data, Executable: a.Executable}, nil
}

// SimulatedAccount is one requested account's state after a simulation.
// DataLen is the size of its data. Token is set when it is an SPL token account.
type SimulatedAccount struct {
	Exists   bool
	Lamports uint64
	Owner    PublicKey
	DataLen  int
	Token    *TokenAccount
}

// Simulation is simulateTransaction's result. Err is nil when the transaction
// would succeed; otherwise it is the raw error, a string such as
// "BlockhashNotFound" or an object such as {"InstructionError":[2,{"Custom":1}]}.
// When the transaction fails before executing, the accounts are all absent.
type Simulation struct {
	Err           json.RawMessage
	Logs          []string
	UnitsConsumed uint64
	Accounts      []SimulatedAccount
}

// Simulate runs tx exactly as signed (or unsigned): no signature check, no
// blockhash replacement, against the processed bank so a fresh blockhash is
// found. Accounts are returned in the order requested.
func (c *Client) Simulate(ctx context.Context, tx []byte, accounts []PublicKey) (*Simulation, error) {
	config := map[string]any{"encoding": "base64", "sigVerify": false, "replaceRecentBlockhash": false, "commitment": "processed"}
	if len(accounts) > 0 {
		addresses := make([]string, len(accounts))
		for i, a := range accounts {
			addresses[i] = a.String()
		}
		config["accounts"] = map[string]any{"encoding": "jsonParsed", "addresses": addresses}
	}
	var out struct {
		Value struct {
			Err           json.RawMessage `json:"err"`
			Logs          []string        `json:"logs"`
			UnitsConsumed uint64          `json:"unitsConsumed"`
			Accounts      []*jsonAccount  `json:"accounts"`
		} `json:"value"`
	}
	if err := c.call(ctx, "simulateTransaction", []any{base64.StdEncoding.EncodeToString(tx), config}, &out); err != nil {
		return nil, err
	}
	sim := &Simulation{Logs: out.Value.Logs, UnitsConsumed: out.Value.UnitsConsumed}
	if len(out.Value.Err) > 0 && string(out.Value.Err) != "null" {
		sim.Err = out.Value.Err
	}
	if len(accounts) > 0 && len(out.Value.Accounts) != len(accounts) {
		return nil, fmt.Errorf("solana: simulation returned %d accounts for %d requested", len(out.Value.Accounts), len(accounts))
	}
	sim.Accounts = make([]SimulatedAccount, len(accounts))
	for i, a := range out.Value.Accounts {
		if a == nil {
			continue
		}
		owner, err := ParsePublicKey(a.Owner)
		if err != nil {
			return nil, err
		}
		sim.Accounts[i] = SimulatedAccount{Exists: true, Lamports: a.Lamports, Owner: owner, DataLen: a.dataLen()}
		token, ok, err := a.tokenAccount(accounts[i])
		if err != nil {
			return nil, err
		}
		if ok {
			sim.Accounts[i].Token = token
		}
	}
	return sim, nil
}

// SignatureStatus is a transaction's status; Err is nil when it succeeded.
// ConfirmationStatus is "processed", "confirmed" or "finalized".
type SignatureStatus struct {
	Slot               uint64
	Confirmations      *uint64
	Err                json.RawMessage
	ConfirmationStatus string
}

// SignatureStatuses looks signatures up, searching history beyond the recent
// status cache. An entry is nil when the cluster has never seen it.
func (c *Client) SignatureStatuses(ctx context.Context, signatures []string) ([]*SignatureStatus, error) {
	var out struct {
		Value []*struct {
			Slot               uint64          `json:"slot"`
			Confirmations      *uint64         `json:"confirmations"`
			Err                json.RawMessage `json:"err"`
			ConfirmationStatus string          `json:"confirmationStatus"`
		} `json:"value"`
	}
	if err := c.call(ctx, "getSignatureStatuses", []any{signatures, map[string]any{"searchTransactionHistory": true}}, &out); err != nil {
		return nil, err
	}
	if len(out.Value) != len(signatures) {
		return nil, fmt.Errorf("solana: %d statuses for %d signatures", len(out.Value), len(signatures))
	}
	statuses := make([]*SignatureStatus, len(out.Value))
	for i, v := range out.Value {
		if v == nil {
			continue
		}
		s := &SignatureStatus{Slot: v.Slot, Confirmations: v.Confirmations, ConfirmationStatus: v.ConfirmationStatus}
		if len(v.Err) > 0 && string(v.Err) != "null" {
			s.Err = v.Err
		}
		statuses[i] = s
	}
	return statuses, nil
}

// Blockhash is a recent blockhash and the last block height it is valid at.
type Blockhash struct {
	Hash                 [32]byte
	LastValidBlockHeight uint64
}

// LatestBlockhash reads a blockhash at confirmed commitment.
func (c *Client) LatestBlockhash(ctx context.Context) (Blockhash, error) {
	var out struct {
		Value struct {
			Blockhash            string `json:"blockhash"`
			LastValidBlockHeight uint64 `json:"lastValidBlockHeight"`
		} `json:"value"`
	}
	if err := c.call(ctx, "getLatestBlockhash", []any{map[string]any{"commitment": "confirmed"}}, &out); err != nil {
		return Blockhash{}, err
	}
	raw, err := Base58Decode(out.Value.Blockhash)
	if err != nil || len(raw) != 32 {
		return Blockhash{}, fmt.Errorf("solana: bad blockhash %q", out.Value.Blockhash)
	}
	return Blockhash{Hash: [32]byte(raw), LastValidBlockHeight: out.Value.LastValidBlockHeight}, nil
}

// SendTransaction broadcasts a signed transaction with preflight at confirmed
// commitment and returns its signature. It is never retried here: after an
// unknown outcome the caller looks the signature up, then rebroadcasts the same
// bytes, which cannot land twice.
func (c *Client) SendTransaction(ctx context.Context, tx []byte) (string, error) {
	config := map[string]any{"encoding": "base64", "preflightCommitment": "confirmed", "maxRetries": 5}
	var out string
	if err := c.once(ctx, "sendTransaction", []any{base64.StdEncoding.EncodeToString(tx), config}, &out); err != nil {
		return "", err
	}
	return out, nil
}
