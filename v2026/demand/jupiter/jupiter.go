// Package jupiter is the demand deposit funder's client for Jupiter's Swap API v2
// (https://api.jup.ag/swap/v2): /order quotes a swap and, given a taker,
// builds the unsigned transaction; /execute lands the taker-signed
// transaction and reports the outcome. Field names were checked against
// developers.jup.ag and live /order responses on 2026-10-22; /execute was
// read from the docs only, since calling it means signing a real swap.
//
// A taker's order takes one of two shapes (live orders, 2026-11-02). One from an
// AMM route has a lastValidBlockHeight and a slippage-derived minimum, and the
// taker pays. One from JupiterZ, an RFQ, is gasless: it has no
// lastValidBlockHeight, only an expireAt under a minute ahead, its minimum is its
// output, and it has two signature slots with the maker's first, empty, and paying
// the fee. The maker signs inside /execute, so the transaction's signature does
// not exist until /execute reports it.
package jupiter

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
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL = "https://api.jup.ag/swap/v2"
	userAgent      = "urnetwork-sn-demand"
	// Responses are small JSON documents; anything larger is a fault, not data.
	maxResponseBytes = 4 << 20
	maxErrorBody     = 512
	// /execute codes: 0 is success; -1005 means the blockhash expired first.
	codeExpired = -1005
)

// Client calls the Swap API. /order is paced to Jupiter's per-key budget (one
// request per 2 s without a key, one per second with a free key); /execute
// has its own, larger budget and is not paced. It is safe for concurrent use.
type Client struct {
	apiKey string
	base   string
	http   *http.Client

	mu       sync.Mutex
	next     time.Time
	interval time.Duration
	now      func() time.Time
	sleep    func(ctx context.Context, d time.Duration) error
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

// WithBaseURL points the client at another origin (tests).
func WithBaseURL(u string) Option {
	return func(c *Client) {
		if u != "" {
			c.base = strings.TrimRight(u, "/")
		}
	}
}

// NewClient builds a client; apiKey may be empty.
func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:   apiKey,
		base:     defaultBaseURL,
		http:     &http.Client{Timeout: 30 * time.Second},
		interval: 2 * time.Second,
		now:      time.Now,
		sleep:    sleepContext,
	}
	if apiKey != "" {
		c.interval = time.Second
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

// pace reserves the next /order slot and waits for it.
func (c *Client) pace(ctx context.Context) error {
	c.mu.Lock()
	now := c.now()
	at := c.next
	if at.Before(now) {
		at = now
	}
	c.next = at.Add(c.interval)
	c.mu.Unlock()
	return c.sleep(ctx, at.Sub(now))
}

// APIError is a non-2xx response. Body is truncated to 512 bytes.
type APIError struct {
	Status int
	Path   string
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("jupiter: %s: HTTP %d: %s", e.Path, e.Status, e.Body)
}

// OrderError is a 2xx /order whose router could not build the transaction:
// errorCode 1 is insufficient funds, 2 insufficient SOL for fees, 3 below the
// gasless minimum (aggregator routers; JupiterZ numbers differ).
type OrderError struct {
	Router  string
	Code    int
	Message string
}

func (e *OrderError) Error() string {
	return fmt.Sprintf("jupiter: order refused by %s (code %d): %s", e.Router, e.Code, e.Message)
}

// OrderRequest asks for an exact-in swap of Amount base units of InputMint.
// Without Taker the response is only a quote. Receiver, a wallet address
// other than the taker, gets the output in its associated token account.
// SlippageBps 0 lets Jupiter choose.
type OrderRequest struct {
	InputMint   string
	OutputMint  string
	Amount      uint64
	Taker       string
	Receiver    string
	SlippageBps int
}

// Order is /order's quote and, for a taker, its unsigned transaction.
// OtherAmountThreshold is the least output the transaction accepts.
// LastValidBlockHeight is 0 on an order that has none, and ExpireAt zero on one
// without a time, each of which real orders can be; see the package comment.
type Order struct {
	RequestID            string
	Transaction          []byte
	InAmount             uint64
	OutAmount            uint64
	OtherAmountThreshold uint64
	SlippageBps          int
	FeeBps               int
	PriceImpactPct       float64
	Router               string
	LastValidBlockHeight uint64
	ExpireAt             time.Time
	Gasless              bool
	SignatureFeePayer    string
	Raw                  json.RawMessage
}

// Order calls GET /order.
func (c *Client) Order(ctx context.Context, req OrderRequest) (*Order, error) {
	switch {
	case req.InputMint == "" || req.OutputMint == "":
		return nil, errors.New("jupiter: order needs both mints")
	case req.Amount == 0:
		return nil, errors.New("jupiter: order needs a positive amount")
	case req.Receiver != "" && req.Taker == "":
		return nil, errors.New("jupiter: a receiver needs a taker")
	case req.Receiver != "" && req.Receiver == req.Taker:
		return nil, errors.New("jupiter: the receiver must differ from the taker")
	case req.SlippageBps < 0 || req.SlippageBps > 10_000:
		return nil, fmt.Errorf("jupiter: slippage of %d bps is out of range", req.SlippageBps)
	}
	query := url.Values{}
	query.Set("inputMint", req.InputMint)
	query.Set("outputMint", req.OutputMint)
	query.Set("amount", strconv.FormatUint(req.Amount, 10))
	if req.Taker != "" {
		query.Set("taker", req.Taker)
	}
	if req.Receiver != "" {
		query.Set("receiver", req.Receiver)
	}
	if req.SlippageBps > 0 {
		query.Set("slippageBps", strconv.Itoa(req.SlippageBps))
	}
	if err := c.pace(ctx); err != nil {
		return nil, err
	}
	raw, err := c.do(ctx, http.MethodGet, "/order?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var body struct {
		RequestID            string    `json:"requestId"`
		Transaction          *string   `json:"transaction"`
		InAmount             flexUint  `json:"inAmount"`
		OutAmount            flexUint  `json:"outAmount"`
		OtherAmountThreshold flexUint  `json:"otherAmountThreshold"`
		SlippageBps          int       `json:"slippageBps"`
		FeeBps               int       `json:"feeBps"`
		PriceImpactPct       flexFloat `json:"priceImpactPct"`
		Router               string    `json:"router"`
		LastValidBlockHeight flexUint  `json:"lastValidBlockHeight"`
		ExpireAt             flexUint  `json:"expireAt"`
		Gasless              bool      `json:"gasless"`
		SignatureFeePayer    *string   `json:"signatureFeePayer"`
		ErrorCode            int       `json:"errorCode"`
		ErrorMessage         string    `json:"errorMessage"`
		Error                string    `json:"error"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("jupiter: decode /order: %w", err)
	}
	if body.ErrorCode != 0 || body.ErrorMessage != "" || body.Error != "" {
		message := body.ErrorMessage
		if message == "" {
			message = body.Error
		}
		return nil, &OrderError{Router: body.Router, Code: body.ErrorCode, Message: message}
	}
	order := &Order{
		RequestID:            body.RequestID,
		InAmount:             uint64(body.InAmount),
		OutAmount:            uint64(body.OutAmount),
		OtherAmountThreshold: uint64(body.OtherAmountThreshold),
		SlippageBps:          body.SlippageBps,
		FeeBps:               body.FeeBps,
		PriceImpactPct:       float64(body.PriceImpactPct),
		Router:               body.Router,
		LastValidBlockHeight: uint64(body.LastValidBlockHeight),
		Gasless:              body.Gasless,
		Raw:                  append(json.RawMessage(nil), raw...),
	}
	if body.ExpireAt != 0 {
		order.ExpireAt = time.Unix(int64(body.ExpireAt), 0)
	}
	if body.SignatureFeePayer != nil {
		order.SignatureFeePayer = *body.SignatureFeePayer
	}
	if order.RequestID == "" || order.InAmount != req.Amount || order.OutAmount == 0 {
		return nil, fmt.Errorf("jupiter: /order answered requestId %q, in %d, out %d for %d in", order.RequestID, order.InAmount, order.OutAmount, req.Amount)
	}
	if body.Transaction != nil && *body.Transaction != "" {
		if order.Transaction, err = base64.StdEncoding.DecodeString(*body.Transaction); err != nil {
			return nil, fmt.Errorf("jupiter: /order transaction: %w", err)
		}
	}
	if req.Taker != "" && len(order.Transaction) == 0 {
		return nil, &OrderError{Router: body.Router, Message: "no transaction for the taker"}
	}
	return order, nil
}

// ExecuteResult is /execute's report. A landed swap has Status "Success" and
// Code 0; InputAmount and OutputAmount are what left and reached the wallets,
// fees included.
type ExecuteResult struct {
	Status       string
	Code         int
	Signature    string
	Slot         uint64
	Error        string
	InputAmount  uint64
	OutputAmount uint64
	Raw          json.RawMessage
}

// Succeeded reports a confirmed swap.
func (r *ExecuteResult) Succeeded() bool { return r.Status == "Success" && r.Code == 0 }

// Expired reports that the blockhash expired before the swap landed, so the
// signed transaction can never land and a new order is needed.
func (r *ExecuteResult) Expired() bool { return r.Code == codeExpired }

// Execute calls POST /execute with the signed transaction. It is never
// retried: a resend after an unknown outcome must first check the signature.
// A failed swap is a result, not an error.
func (c *Client) Execute(ctx context.Context, signedTx []byte, requestID string) (*ExecuteResult, error) {
	if len(signedTx) == 0 || requestID == "" {
		return nil, errors.New("jupiter: execute needs a signed transaction and its requestId")
	}
	payload, err := json.Marshal(map[string]string{
		"signedTransaction": base64.StdEncoding.EncodeToString(signedTx),
		"requestId":         requestID,
	})
	if err != nil {
		return nil, err
	}
	raw, err := c.do(ctx, http.MethodPost, "/execute", payload)
	if err != nil {
		return nil, err
	}
	var body struct {
		Status             string   `json:"status"`
		Code               int      `json:"code"`
		Signature          string   `json:"signature"`
		Slot               flexUint `json:"slot"`
		Error              string   `json:"error"`
		TotalInputAmount   flexUint `json:"totalInputAmount"`
		TotalOutputAmount  flexUint `json:"totalOutputAmount"`
		InputAmountResult  flexUint `json:"inputAmountResult"`
		OutputAmountResult flexUint `json:"outputAmountResult"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("jupiter: decode /execute: %w", err)
	}
	result := &ExecuteResult{
		Status:       body.Status,
		Code:         body.Code,
		Signature:    body.Signature,
		Slot:         uint64(body.Slot),
		Error:        body.Error,
		InputAmount:  uint64(body.TotalInputAmount),
		OutputAmount: uint64(body.TotalOutputAmount),
		Raw:          append(json.RawMessage(nil), raw...),
	}
	if result.InputAmount == 0 {
		result.InputAmount = uint64(body.InputAmountResult)
	}
	if result.OutputAmount == 0 {
		result.OutputAmount = uint64(body.OutputAmountResult)
	}
	if result.Status == "" {
		return nil, fmt.Errorf("jupiter: /execute answered without a status: %s", truncate(string(raw)))
	}
	return result, nil
}

func (c *Client) do(ctx context.Context, method, path string, payload []byte) ([]byte, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("jupiter: build %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("x-api-key", c.apiKey)
	}
	name := path
	if i := strings.IndexByte(name, '?'); i >= 0 {
		name = name[:i]
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jupiter: %s: %w", name, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("jupiter: %s: read response: %w", name, err)
	}
	if len(raw) > maxResponseBytes {
		return nil, fmt.Errorf("jupiter: %s: response exceeds %d bytes", name, maxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &APIError{Status: resp.StatusCode, Path: name, Body: truncate(string(raw))}
	}
	return raw, nil
}

func truncate(s string) string {
	if len(s) > maxErrorBody {
		return s[:maxErrorBody]
	}
	return s
}

// flexUint accepts a JSON number or a decimal string, as Jupiter sends both
// (amounts, lastValidBlockHeight and expireAt are strings, slot may be either).
type flexUint uint64

func (f *flexUint) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return fmt.Errorf("not an unsigned integer: %s", b)
	}
	*f = flexUint(v)
	return nil
}

// flexFloat accepts a JSON number or a numeric string.
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("not a number: %s", b)
	}
	*f = flexFloat(v)
	return nil
}
