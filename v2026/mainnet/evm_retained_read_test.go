// Retained observations use real action owners and the owned HTTP route. The
// instance wait seam advances logical retry work without wall-clock sleeps.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Only the synthetic original approval selects 300 seconds. No retained signed
// configuration is changed when the owner opens or when a read is retried.
func newEvmRetainedReadFixture(t *testing.T) (*evmCreateFixture, *evmCreateOwner, *evmOwnedChain) {
	t.Helper()
	f := newEvmCreateFixture(t)
	f.config.Plan.Route.ReadRetrySeconds = 300
	f.publishConfig()
	f.prepareSigned()
	if _, code, diagnostic := f.command("resume", "--online", "--submit"); code != 0 {
		t.Fatal("original synthetic transaction did not execute", code, diagnostic)
	}
	if result, code, diagnostic := f.command("resume", "--online"); code != 0 || result.Receipt == nil || result.Attempts != 1 {
		t.Fatal("original completed receipt was not retained", result, code, diagnostic)
	}
	store, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.close(); err != nil {
			t.Error(err)
		}
	})
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chain.client.httpClient.CloseIdleConnections)
	owner, err := newEvmCreateOwner(f.plan, store, chain)
	if err != nil {
		t.Fatal(err)
	}
	return f, owner, chain
}

// Only the selected read is overridden. Identity, mapping, runtime, getters and
// other owners continue through the original HTTP transport.
func evmRetainedReadTransport(t *testing.T, base http.RoundTripper, method string, fault func() (json.RawMessage, error, bool)) http.RoundTripper {
	t.Helper()
	return roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := request.GetBody()
		if err != nil {
			return nil, err
		}
		var call struct {
			Method string `json:"method"`
		}
		err = json.NewDecoder(body).Decode(&call)
		body.Close()
		if err != nil {
			return nil, err
		}
		if call.Method == method {
			if raw, err, selected := fault(); selected {
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(append(append([]byte(`{"jsonrpc":"2.0","id":1,"result":`), raw...), '}')))}, nil
			}
		}
		return base.RoundTrip(request)
	})
}

// More than sixty seconds of scheduled retry work can precede the original
// response. Each retry retains the signed 300-second deadline and never sends.
func TestEvmRetainedReadRecoversAfterLogicalMinute(t *testing.T) {
	f, owner, chain := newEvmRetainedReadFixture(t)
	base := chain.client.httpClient.Transport
	for _, fault := range []struct {
		name, method string
		timeout      bool
	}{
		{name: "receipt absence", method: "eth_getTransactionReceipt"},
		{name: "position absence", method: "eth_getTransactionByBlockHashAndIndex"},
		{name: "receipt timeout", method: "eth_getTransactionReceipt", timeout: true},
	} {
		calls, waits, elapsed := 0, 0, time.Duration(0)
		var deadline time.Time
		chain.client.httpClient.Transport = evmRetainedReadTransport(t, base, fault.method, func() (json.RawMessage, error, bool) {
			calls++
			if calls > 21 {
				return nil, nil, false
			}
			if fault.timeout {
				return nil, context.DeadlineExceeded, true
			}
			return json.RawMessage("null"), nil, true
		})
		chain.client.retryWait = func(ctx context.Context, delay time.Duration) error {
			waits++
			elapsed += delay
			current, ok := ctx.Deadline()
			if !ok || delay <= 0 || delay > 5*time.Second || !deadline.IsZero() && current != deadline || time.Until(current) <= time.Minute || time.Until(current) > 300*time.Second {
				t.Error("retained read lost its original finite deadline", fault.name, delay, current, deadline)
			}
			deadline = current
			return ctx.Err()
		}
		result, err := owner.advance(t.Context(), nil, true, true)
		if err != nil || result.Receipt == nil || result.Attempts != 1 || calls != 22 || waits != 21 || elapsed <= time.Minute || len(f.writes) != 1 {
			t.Fatal("retained observation did not recover without a new send", fault.name, result, err, calls, waits, elapsed)
		}
	}
}

// The wait boundary exhausts the exact logical budget deterministically. No
// unavailable reply is admitted as history, and same-owner read recovery works.
func TestEvmRetainedReadExhaustionPreservesOriginalCustody(t *testing.T) {
	f, owner, chain := newEvmRetainedReadFixture(t)
	base := chain.client.httpClient.Transport
	before := bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)
	for _, timeout := range []bool{false, true} {
		calls, elapsed := 0, time.Duration(0)
		chain.client.httpClient.Transport = evmRetainedReadTransport(t, base, "eth_getTransactionReceipt", func() (json.RawMessage, error, bool) {
			calls++
			if timeout {
				return nil, context.DeadlineExceeded, true
			}
			return json.RawMessage("null"), nil, true
		})
		chain.client.retryWait = func(ctx context.Context, delay time.Duration) error {
			elapsed += delay
			if elapsed >= 300*time.Second {
				return context.DeadlineExceeded
			}
			return ctx.Err()
		}
		_, err := owner.advance(t.Context(), nil, true, true)
		if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errRpcIntegrity) || !strings.Contains(err.Error(), "RPC observation is unavailable") || calls < 2 || elapsed < 300*time.Second || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)) || len(f.writes) != 1 {
			t.Fatal("read budget exhaustion changed original custody or claimed inconsistent history", timeout, err, calls, elapsed)
		}
	}
	chain.client.httpClient.Transport, chain.client.retryWait = base, nil
	if result, err := owner.advance(t.Context(), nil, true, true); err != nil || result.Receipt == nil || result.Attempts != 1 || len(f.writes) != 1 {
		t.Fatal("same original owner could not recover observation", result, err)
	}
}

// Canceling one retained read releases its finite wait without changing its
// journal or preventing an unrelated real owner from making approved progress.
func TestEvmRetainedReadCancellationKeepsOtherOwnerUsable(t *testing.T) {
	f, owner, chain := newEvmRetainedReadFixture(t)
	peer := newEvmCreateFixture(t)
	peer.prepareSigned()
	before := bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	chain.client.httpClient.Transport = evmRetainedReadTransport(t, chain.client.httpClient.Transport, "eth_getTransactionReceipt", func() (json.RawMessage, error, bool) {
		calls++
		return json.RawMessage("null"), nil, true
	})
	chain.client.retryWait = func(waitCtx context.Context, _ time.Duration) error { cancel(); return waitCtx.Err() }
	_, err := owner.advance(ctx, nil, true, true)
	if !errors.Is(err, context.Canceled) || errors.Is(err, errRpcIntegrity) || calls != 1 || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)) || len(f.writes) != 1 {
		t.Fatal("canceled read changed or resent original custody", err, calls)
	}
	if result, code, diagnostic := peer.command("resume", "--online", "--submit"); code != 0 || result.Attempts != 1 || len(peer.writes) != 1 {
		t.Fatal("unrelated owner did not retain independent progress", result, code, diagnostic)
	}
}

// Returned contradictory facts are different from absent observations. None
// gets a retry, a changed receipt, another signature or another nonce attempt.
func TestEvmRetainedReadRejectsReturnedConflicts(t *testing.T) {
	f, owner, chain := newEvmRetainedReadFixture(t)
	base := chain.client.httpClient.Transport
	before := bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)
	for _, fault := range []string{"malformed", "hash", "status", "nonce"} {
		method, raw := "eth_getTransactionReceipt", json.RawMessage(`{"transactionHash":17}`)
		if fault == "hash" || fault == "status" {
			receipt := maps.Clone(f.receipt)
			if fault == "hash" {
				receipt["transactionHash"] = common.Hash{31: 29}.Hex()
			} else {
				receipt["status"], receipt["contractAddress"] = "0x0", nil
			}
			raw, _ = json.Marshal(receipt)
		}
		if fault == "nonce" {
			action := f.config.Plan.Actions[0]
			action.Nonce++
			unsigned, err := action.unsigned()
			if err != nil {
				t.Fatal(err)
			}
			key, err := crypto.HexToECDSA(strings.Repeat("17", 32))
			if err != nil {
				t.Fatal(err)
			}
			tx, err := types.SignTx(unsigned, types.LatestSignerForChainID(big.NewInt(964)), key)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ = json.Marshal(tx)
			method = "eth_getTransactionByBlockHashAndIndex"
		}
		calls, waits := 0, 0
		chain.client.httpClient.Transport = evmRetainedReadTransport(t, base, method, func() (json.RawMessage, error, bool) { calls++; return raw, nil, true })
		chain.client.retryWait = func(context.Context, time.Duration) error {
			waits++
			return errors.New("a returned conflict must never reach retry")
		}
		_, err := owner.advance(t.Context(), nil, true, true)
		if !errors.Is(err, errRpcIntegrity) || strings.Contains(err.Error(), "RPC observation is unavailable") || calls != 1 || waits != 0 || len(f.writes) != 1 || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)) {
			t.Fatal("returned conflict was retried or replaced retained authority", fault, err, calls, waits)
		}
	}
}

// The actual eight-action adapter must not relabel an exhausted historical
// lookup as a positive canonical difference; recovery authenticates the same seals.
func TestBootstrapSuccessorCanonicalHistoricalReadUnavailable(t *testing.T) {
	f := newBootstrapSuccessorCanonicalFixture(t)
	owner, adapter := f.open()
	base := adapter.chain.client.httpClient.Transport
	before := bootstrapSuccessorPreparationTestFiles(t, owner.local.path)
	for _, timeout := range []bool{false, true} {
		calls := 0
		adapter.chain.client.httpClient.Transport = evmRetainedReadTransport(t, base, "eth_getTransactionReceipt", func() (json.RawMessage, error, bool) {
			calls++
			if timeout {
				return nil, context.DeadlineExceeded, true
			}
			return json.RawMessage("null"), nil, true
		})
		adapter.chain.client.retryWait = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
		_, err := adapter.authenticate(t.Context(), owner.planCopy())
		if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errRpcIntegrity) || strings.Contains(err.Error(), "postcondition differs") || strings.Contains(err.Error(), "disappeared") || !strings.Contains(err.Error(), "RPC observation is unavailable") || calls != 1 || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, owner.local.path)) || len(f.original.contracts.writes) != 8 {
			t.Fatal("historical observation failure became altered original authority", timeout, err, calls)
		}
	}
	adapter.chain.client.httpClient.Transport, adapter.chain.client.retryWait = base, nil
	if seals, err := adapter.authenticate(t.Context(), owner.planCopy()); err != nil || len(seals) != 8 || len(f.original.contracts.writes) != 8 {
		t.Fatal("same historical adapter could not authenticate original seals after read recovery", seals, err)
	}
}
