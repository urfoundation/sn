//go:build linux || darwin

// Readback and staging use their complete ordinary verifiers after recovery.
// Faults are injected only at the real serialized Rpc transport boundary.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// A transaction already broadcast remains the same transaction after receipt,
// inclusion, winner discovery or signed-byte lookup recovers. No send is owned
// by these entrypoints, including the permissionless winner path.
func TestChainReadBudgetRecoversEvidenceReadbackWithoutWriteReplay(t *testing.T) {
	for _, method := range []string{"eth_getTransactionReceipt", "eth_getTransactionByBlockHashAndIndex", "eth_getLogs", "eth_getTransactionByHash"} {
		fixture := newEvidenceTransactionV2Fixture(t, "", nil)
		attempts := 0
		var original []byte
		chain := canonicalReadbackTestClient(t, fixture.chain, func(ctx context.Context, calls []chainBatchRPCRequest) error {
			for _, call := range calls {
				if call.Method != method {
					continue
				}
				parameters, err := json.Marshal(call.Params)
				if err != nil {
					return err
				}
				attempts++
				if attempts == 1 {
					original = parameters
					return &gethrpc.HTTPError{StatusCode: http.StatusServiceUnavailable}
				}
				if !bytes.Equal(parameters, original) {
					return errors.New("readback retry replaced its original transaction or block")
				}
			}
			return ctx.Err()
		}, fixture.handler)
		result, err := chain.FindValidatorEvidenceSlotWinnerV2Context(t.Context(), fixture.expected)
		if err != nil || result == nil || attempts != 2 || result.Receipt.TxHash != fixture.transaction.Hash() || !bytes.Equal(result.SignedTransaction, fixture.expected.SignedTransaction) || fixture.requestCount("eth_sendRawTransaction") != 0 {
			t.Fatalf("%s readback retry failed or replaced retained bytes: attempts=%d result=%v error=%v", method, attempts, result, err)
		}
	}
}

// A transient-looking transport error cannot conceal a simultaneous semantic
// refusal at any newly retried evidence read. Cancellation similarly wins
// before the next request and no incomplete finalization result escapes.
func TestChainReadBudgetEvidenceReadbackKeepsHardFailureAndCancellation(t *testing.T) {
	for _, method := range []string{"eth_getTransactionReceipt", "eth_getTransactionByBlockHashAndIndex", "eth_getLogs", "eth_getTransactionByHash"} {
		for _, cancelOwner := range []bool{false, true} {
			fixture := newEvidenceTransactionV2Fixture(t, "", nil)
			ctx, cancel := context.WithCancel(t.Context())
			attempts, waits := 0, 0
			hard := errors.New("synthetic exact receipt identity conflict")
			chain := canonicalReadbackTestClient(t, fixture.chain, func(ctx context.Context, calls []chainBatchRPCRequest) error {
				for _, call := range calls {
					if call.Method == method {
						attempts++
						if cancelOwner {
							return context.DeadlineExceeded
						}
						return errors.Join(context.DeadlineExceeded, hard)
					}
				}
				return ctx.Err()
			}, fixture.handler)
			chain.readRetryHooks.wait = func(context.Context, time.Duration) error {
				waits++
				cancel()
				return nil
			}
			result, err := chain.FindValidatorEvidenceSlotWinnerV2Context(ctx, fixture.expected)
			cancel()
			want := hard
			if cancelOwner {
				want = context.Canceled
			}
			if result != nil || !errors.Is(err, want) || attempts != 1 || (!cancelOwner && waits != 0) || (cancelOwner && waits != 1) || RetryableEvidenceTransportError(err) || fixture.requestCount("eth_sendRawTransaction") != 0 {
				t.Fatalf("%s cancel=%t ignored hard owner: attempts=%d result=%v error=%v", method, cancelOwner, attempts, result, err)
			}
		}
	}
}

// A retried logs request keeps the exact historical range and epoch filter;
// all ordinary event, duplicate, canonical header and final checkpoint checks
// must still finish before returning deposit sums.
func TestChainReadBudgetRecoversDepositLogsAtOriginalEpoch(t *testing.T) {
	fixture := &depositedRPCFixture{finalized: 105, epoch: 5, epochStart: 101, contract: common.Address{0xcc}}
	fixture.logs = []map[string]any{depositedTestLog(fixture.contract, 101)}
	attempts := 0
	var original []byte
	chain := canonicalReadbackTestClient(t, fixture.client(t), func(ctx context.Context, calls []chainBatchRPCRequest) error {
		for _, call := range calls {
			if call.Method == "eth_getLogs" {
				parameters, err := json.Marshal(call.Params)
				if err != nil {
					return err
				}
				attempts++
				if attempts == 1 {
					original = parameters
					return context.DeadlineExceeded
				}
				if !bytes.Equal(parameters, original) {
					return errors.New("deposit retry replaced its range or epoch")
				}
			}
		}
		return ctx.Err()
	}, nil)
	sums, err := chain.depositedSumsAtFinalizedContext(t.Context(), 101, 105, big.NewInt(5), 105, [32]byte(depositedTestBlockHash(105)))
	if err != nil || sums == nil || sums["1"] == nil || sums["1"].Int64() != 100 || attempts != 2 {
		t.Fatalf("deposit retry failed: attempts=%d sums=%v error=%v", attempts, sums, err)
	}
}

// Staging discovery keeps its observed block and event window while transient
// finalized-head or logs transport failures recover. Complete event decoding
// and the before/after canonical rechecks remain mandatory.
func TestChainReadBudgetRecoversUploadObserverAndEvents(t *testing.T) {
	for _, method := range []string{"eth_getBlockByNumber", "eth_getLogs"} {
		fixture := newValidatorUploadAuthorityTestFixture(t)
		attempts := 0
		var original []byte
		chain := canonicalReadbackTestClient(t, fixture.chain, func(ctx context.Context, calls []chainBatchRPCRequest) error {
			for _, call := range calls {
				if call.Method != method || method == "eth_getBlockByNumber" && (len(call.Params) != 2 || string(call.Params[0]) != `"finalized"`) {
					continue
				}
				parameters, err := json.Marshal(call.Params)
				if err != nil {
					return err
				}
				attempts++
				if attempts == 1 {
					original = parameters
					return &gethrpc.HTTPError{StatusCode: http.StatusServiceUnavailable}
				}
				if !bytes.Equal(original, parameters) {
					return errors.New("upload discovery retry changed its original window")
				}
			}
			return ctx.Err()
		}, nil)
		observer, err := chain.ValidatorUploadObserverContext(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		events, err := chain.ValidatorUploadActivationEventsContext(t.Context(), fixture.deployment(), 1001, 1200, 200, 8, observer)
		if err != nil || len(events) != 1 || attempts != 2 || events[0].Digest != fixture.events[0].Digest {
			t.Fatalf("%s discovery did not recover: attempts=%d events=%v error=%v", method, attempts, events, err)
		}
	}
}

// Fresh client-key authority and its closing witness both re-observe the real
// route. A retry neither borrows the cached dial identity nor changes the
// original effective boundary before returning the authenticated root signer.
func TestChainReadBudgetRecoversClientKeyIdentityAndClosingWitness(t *testing.T) {
	for _, closing := range []bool{false, true} {
		methods := []string{"eth_chainId", "chain_getBlockHash"}
		if closing {
			methods = append(methods, "eth_getBlockByNumber")
		}
		for _, method := range methods {
			fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
			fixture.chain.rpcUrl = "http://authority-read.example"
			armed, attempts := !closing, 0
			var original []byte
			fixture.chain = canonicalReadbackTestClient(t, fixture.chain, func(ctx context.Context, calls []chainBatchRPCRequest) error {
				for _, call := range calls {
					if !armed || call.Method != method || method == "eth_getBlockByNumber" && (len(call.Params) != 2 || string(call.Params[0]) == `"finalized"`) {
						continue
					}
					parameters, err := json.Marshal(call.Params)
					if err != nil {
						return err
					}
					attempts++
					if attempts == 1 {
						original = parameters
						return &gethrpc.HTTPError{StatusCode: http.StatusServiceUnavailable}
					}
					if !bytes.Equal(original, parameters) {
						return errors.New("client-key authority retry changed its pinned source")
					}
				}
				return ctx.Err()
			}, fixture.rpc)
			ctx := t.Context()
			var owner *releaseClientKeyAuthorityV2Reads
			if closing {
				ctx, owner = fixture.owner(t, ctx, 4*1024*1024)
			}
			signer, err := fixture.chain.readReleaseClientKeyAuthorityV2(ctx, fixture.domain, fixture.request.DecisionBoundary)
			if err == nil && closing {
				armed = true
				err = owner.finish(nil)
			}
			if err != nil || signer == (common.Address{}) || attempts != 2 {
				t.Fatalf("%s closing=%t authority did not recover: attempts=%d signer=%s error=%v", method, closing, attempts, signer, err)
			}
		}
	}
}
