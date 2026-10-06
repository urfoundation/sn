// Remote absence cannot assert changed Safe authority. These controls exercise
// actual published Safe getters, scoped pending reads and their owned transport.
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
)

// Each selection names the exact account method/slot rather than masking all
// historical receipts or unrelated contract state on the same HTTP route.
type safeStateReadSelection struct{ method, account, slot, getter string }

func (self safeStateReadSelection) matches(method string, params []any) bool {
	if method != self.method {
		return false
	}
	if self.account != "" && (len(params) != 2 || params[0] != self.account) {
		return false
	}
	if self.slot != "" {
		return len(params) == 3 && params[1] == self.slot
	}
	if self.getter != "" {
		if len(params) != 2 {
			return false
		}
		call, ok := params[0].(map[string]any)
		if !ok {
			return false
		}
		data, ok := call["data"].(string)
		return ok && strings.HasPrefix(data, self.getter)
	}
	return true
}

// All other traffic reaches the original route. The fault callback runs
// synchronously on this instance, so counters and retry barriers need no sleep.
func safeStateReadTransport(t *testing.T, base http.RoundTripper, selection safeStateReadSelection, fault func() (json.RawMessage, error, bool)) http.RoundTripper {
	t.Helper()
	return roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := request.GetBody()
		if err != nil {
			return nil, err
		}
		var call struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		err = json.NewDecoder(body).Decode(&call)
		body.Close()
		if err != nil {
			return nil, err
		}
		if selection.matches(call.Method, call.Params) {
			if raw, err, selected := fault(); selected {
				if err != nil {
					return nil, err
				}
				response := append(append([]byte(`{"jsonrpc":"2.0","id":1,"result":`), raw...), '}')
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(response))}, nil
			}
		}
		return base.RoundTrip(request)
	})
}

// A new synthetic signed phase selects 300 seconds. Production never edits a
// retained approval to extend its window or replace the original route.
func newSafeStateReadFixture(t *testing.T) (*bootstrapSuccessorCanonicalChain, *bootstrapSuccessorExecutionFixture, *evmCreateFixture) {
	t.Helper()
	adapter, model, chain := bootstrapSuccessorCanonicalSafeFixture(t)
	chain.config.Plan.Route.ReadRetrySeconds = 300
	chain.publishConfig()
	owned, err := newEvmOwnedChain(chain.config)
	if err != nil {
		t.Fatal(err)
	}
	adapter.chain.client.httpClient.CloseIdleConnections()
	adapter.chain = owned
	t.Cleanup(owned.client.httpClient.CloseIdleConnections)
	return adapter, model, chain
}

func safeStateReadSelections(model *bootstrapSuccessorExecutionFixture) []safeStateReadSelection {
	return []safeStateReadSelection{
		{method: "eth_getCode"},
		{method: "eth_getStorageAt", slot: common.Hash{}.Hex()},
		{method: "eth_getStorageAt", slot: common.BigToHash(big.NewInt(3)).Hex()},
		{method: "eth_getStorageAt", slot: common.BigToHash(big.NewInt(5)).Hex()},
		{method: "eth_getStorageAt", slot: "0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8"},
		{method: "eth_call", getter: "0x" + common.Bytes2Hex(model.oracle.oracleAbi.Methods["getTransactionHash"].ID)},
	}
}

// The operation retains one exact signed deadline through more than a logical
// minute of retries, then admits only the original returned state.
func TestSafeStateReadRetainsOriginalRetryBudget(t *testing.T) {
	adapter, model, chain := newSafeStateReadFixture(t)
	base := adapter.chain.client.httpClient.Transport
	for _, selection := range safeStateReadSelections(model) {
		calls, waits, elapsed := 0, 0, time.Duration(0)
		var deadline time.Time
		adapter.chain.client.httpClient.Transport = safeStateReadTransport(t, base, selection, func() (json.RawMessage, error, bool) {
			calls++
			if calls > 21 {
				return nil, nil, false
			}
			return json.RawMessage("null"), nil, true
		})
		adapter.chain.client.retryWait = func(ctx context.Context, delay time.Duration) error {
			waits++
			elapsed += delay
			current, found := ctx.Deadline()
			if !found || !deadline.IsZero() && current != deadline || time.Until(current) <= time.Minute || time.Until(current) > 300*time.Second {
				t.Error("required state read changed its signed finite deadline", selection, current, deadline)
			}
			deadline = current
			return ctx.Err()
		}
		state, err := adapter.safeState(t.Context(), model.approval.Plan, "pending")
		if err != nil || state.SafeNonce != "17" || state.Threshold != 2 || calls < 22 || waits != 21 || elapsed <= time.Minute {
			t.Fatal("required original state did not recover after bounded retry", selection, err, calls, waits, elapsed)
		}
	}
	if len(chain.writes) != 0 {
		t.Fatal("read recovery acquired a transaction effect")
	}
}

// Timeout/null/cancellation are observation failures. Returned contradictory
// state is tested separately and cannot use this retry classification.
func TestSafeStateReadUnavailableDoesNotClaimChangedAuthority(t *testing.T) {
	adapter, model, chain := newSafeStateReadFixture(t)
	base := adapter.chain.client.httpClient.Transport
	for _, selection := range safeStateReadSelections(model) {
		for _, mode := range []string{"timeout", "null", "cancel"} {
			ctx, cancel := context.WithCancel(t.Context())
			waits := 0
			adapter.chain.client.httpClient.Transport = safeStateReadTransport(t, base, selection, func() (json.RawMessage, error, bool) {
				if mode == "timeout" {
					return nil, context.DeadlineExceeded, true
				}
				return json.RawMessage("null"), nil, true
			})
			adapter.chain.client.retryWait = func(waitCtx context.Context, _ time.Duration) error {
				waits++
				if mode == "cancel" {
					cancel()
					return waitCtx.Err()
				}
				return context.DeadlineExceeded
			}
			_, err := adapter.safeState(ctx, model.approval.Plan, "pending")
			cancel()
			cause := error(context.DeadlineExceeded)
			if mode == "cancel" {
				cause = context.Canceled
			}
			if !errors.Is(err, cause) || !errors.Is(err, errRpcObservationUnavailable) || errors.Is(err, errRpcIntegrity) || waits != 1 {
				t.Fatal("unavailable required state claimed contradictory authority", selection, mode, err, waits)
			}
			for _, label := range []string{"differs", "changed", "is active"} {
				if strings.Contains(err.Error(), label) {
					t.Fatal("unobserved Safe state acquired a positive conflict label", selection, mode, err)
				}
			}
		}
	}
	adapter.chain.client.httpClient.Transport, adapter.chain.client.retryWait = base, nil
	if state, err := adapter.safeState(t.Context(), model.approval.Plan, "pending"); err != nil || state.SafeNonce != "17" || len(chain.writes) != 0 {
		t.Fatal("same original read owner did not recover", state, err)
	}
}

// The returned value is well-formed where possible, so each original semantic
// comparison still rejects actual code, owner, nonce, guard or digest changes.
func TestSafeStateReadReturnedConflictsRemainIntegrity(t *testing.T) {
	adapter, model, chain := newSafeStateReadFixture(t)
	base := adapter.chain.client.httpClient.Transport
	// Proxy and singleton are distinct required reads. Count the selected
	// account once; reading its healthy peer is not a retry of the conflict.
	selections := []safeStateReadSelection{
		{method: "eth_getCode", account: model.approval.Plan.Review.Transaction.Safe.Hex()},
		{method: "eth_getCode", account: model.approval.Plan.Request.Singleton.Hex()},
	}
	selections = append(selections, safeStateReadSelections(model)[1:]...)
	for _, selection := range selections {
		waits, calls := 0, 0
		value := common.Hash{31: 99}.Hex()
		if selection.method == "eth_getCode" {
			value = "0x00"
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		adapter.chain.client.httpClient.Transport = safeStateReadTransport(t, base, selection, func() (json.RawMessage, error, bool) { calls++; return raw, nil, true })
		adapter.chain.client.retryWait = func(context.Context, time.Duration) error { waits++; return context.Canceled }
		if _, err := adapter.safeState(t.Context(), model.approval.Plan, "pending"); !errors.Is(err, errRpcIntegrity) || errors.Is(err, errRpcObservationUnavailable) || calls != 1 || waits != 0 {
			t.Fatal("returned conflicting Safe authority was retried or admitted", selection, err, calls, waits)
		}
	}
	if len(chain.writes) != 0 {
		t.Fatal("conflicting state reached submission")
	}
}

// This is the actual finalized-proof-to-pending consumer, including its code
// and storage wrappers. It never claims complete pending authority or sending.
func TestSafeCurrentPendingSeparatesUnavailableFromChanged(t *testing.T) {
	adapter, model, chain := newSafeStateReadFixture(t)
	plan := model.approval.Plan
	f := safeCurrentProofFixtureFromOracle(t, model.oracle, plan.Review.Request.Version, plan.Review.Request.Variant)
	witness := safeCurrentTestWitness(t, f.entries)
	finalized, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness)
	if err != nil {
		t.Fatal(err)
	}
	base := adapter.chain.client.httpClient.Transport
	for _, selection := range []safeStateReadSelection{{method: "eth_getCode"}, {method: "eth_getStorageAt", slot: common.BigToHash(big.NewInt(8)).Hex()}} {
		for _, mode := range []string{"timeout", "null", "cancel", "conflict"} {
			ctx, cancel := context.WithCancel(t.Context())
			waits := 0
			adapter.chain.client.httpClient.Transport = safeStateReadTransport(t, base, selection, func() (json.RawMessage, error, bool) {
				if mode == "timeout" {
					return nil, context.DeadlineExceeded, true
				}
				if mode == "conflict" {
					raw, _ := json.Marshal(common.Hash{31: 99}.Hex())
					return raw, nil, true
				}
				return json.RawMessage("null"), nil, true
			})
			adapter.chain.client.retryWait = func(waitCtx context.Context, _ time.Duration) error {
				waits++
				if mode == "cancel" {
					cancel()
					return waitCtx.Err()
				}
				return context.DeadlineExceeded
			}
			result, err := adapter.readSafeCurrentPending(ctx, f.scope, finalized)
			cancel()
			if mode == "conflict" {
				if result != nil || !errors.Is(err, errRpcIntegrity) || waits != 0 {
					t.Fatal("returned pending conflict was admitted or retried", selection, err, waits)
				}
			} else {
				cause := error(context.DeadlineExceeded)
				if mode == "cancel" {
					cause = context.Canceled
				}
				if result != nil || !errors.Is(err, cause) || !errors.Is(err, errRpcObservationUnavailable) || errors.Is(err, errRpcIntegrity) || strings.Contains(err.Error(), "changed") || waits != 1 {
					t.Fatal("pending read failure became changed authority", selection, mode, err, waits)
				}
			}
		}
	}
	adapter.chain.client.httpClient.Transport, adapter.chain.client.retryWait = base, nil
	if result, err := adapter.readSafeCurrentPending(t.Context(), f.scope, finalized); err != nil || result == nil || !result.ScopedWordsMatched || result.CompletePendingVerified || result.SendAuthorized || len(chain.writes) != 0 {
		t.Fatal("same pending observer did not recover its limited scope", result, err)
	}
}

// Historical receipt authentication consumes required code as well. Its
// absent archive value keeps the exact original signed transaction unresolved,
// without erasing the receipt or purchasing another attempt.
func TestEvmRequiredStateAbsencePreservesRetainedReceipt(t *testing.T) {
	f, owner, chain := newEvmRetainedReadFixture(t)
	before := bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)
	base := chain.client.httpClient.Transport
	for _, timeout := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		calls, waits := 0, 0
		chain.client.httpClient.Transport = safeStateReadTransport(t, base, safeStateReadSelection{method: "eth_getCode"}, func() (json.RawMessage, error, bool) {
			calls++
			if timeout {
				return nil, context.DeadlineExceeded, true
			}
			return json.RawMessage("null"), nil, true
		})
		chain.client.retryWait = func(waitCtx context.Context, _ time.Duration) error { waits++; cancel(); return waitCtx.Err() }
		_, err := owner.advance(ctx, nil, true, true)
		cancel()
		if !errors.Is(err, context.Canceled) || !errors.Is(err, errRpcObservationUnavailable) || errors.Is(err, errRpcIntegrity) || calls != 1 || waits != 1 || len(f.writes) != 1 || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.config.Plan.RunDirectory)) {
			t.Fatal("unavailable original contract state changed receipt or spend custody", timeout, err, calls, waits)
		}
	}
	chain.client.httpClient.Transport, chain.client.retryWait = base, nil
	if result, err := owner.advance(t.Context(), nil, true, true); err != nil || result.Receipt == nil || result.Attempts != 1 || len(f.writes) != 1 {
		t.Fatal("same original receipt could not recover its archive state", result, err)
	}
}
