//go:build linux || darwin

// Typed failures cross the actual rpc transport and serialized evidence
// readers. Virtual time separates each bounded read from the caller lifetime.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// Decorates one fixture's transport without replacing a chain verdict. The
// optional serialized handler keeps all virtual-time work inside the bubble.
func canonicalReadbackTestClient(t *testing.T, prior *ChainClient, before func(context.Context, []chainBatchRPCRequest) error, handler http.Handler) *ChainClient {
	t.Helper()
	client := chainHTTPTestRPC(t, prior.rpcUrl, chainHTTPResponseLimit, func(base http.RoundTripper) http.RoundTripper {
		return chainHTTPTestRoundTripper(func(request *http.Request) (*http.Response, error) {
			raw, err := io.ReadAll(request.Body)
			err = errors.Join(err, request.Body.Close())
			if err != nil {
				return nil, err
			}
			var calls []chainBatchRPCRequest
			if len(raw) != 0 && raw[0] == '[' {
				err = json.Unmarshal(raw, &calls)
			} else {
				var call chainBatchRPCRequest
				err = json.Unmarshal(raw, &call)
				calls = []chainBatchRPCRequest{call}
			}
			if err != nil {
				return nil, err
			}
			if before != nil {
				if err := before(request.Context(), calls); err != nil {
					return nil, err
				}
			}
			request.Body = io.NopCloser(bytes.NewReader(raw))
			if handler == nil {
				return base.RoundTrip(request)
			}
			defer request.Body.Close()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			return response.Result(), nil
		})
	})
	chain := &ChainClient{client: ethclient.NewClient(client), rpcUrl: prior.rpcUrl, chainId: new(big.Int).Set(prior.chainId), st: prior.st, coordinator: prior.coordinator, contractAddr: prior.contractAddr, release: prior.release}
	if chain.release {
		chain.contract = chain.coordinator.Instance(chain.client, chain.contractAddr)
	} else {
		chain.contract = chain.st.Instance(chain.client, chain.contractAddr)
	}
	return chain
}

// Inclusion and finalized rechecks both retain their original transport cause
// after full receipt, calldata, event, epoch and contract authentication.
func TestValidatorEvidenceCanonicalReadbackPreservesTransport(t *testing.T) {
	for _, winner := range []bool{false, true} {
		for _, selector := range []string{"0x44d", "0x4b0"} {
			for _, failure := range []error{context.DeadlineExceeded, io.ErrUnexpectedEOF, &gethrpc.HTTPError{StatusCode: http.StatusServiceUnavailable}, context.Canceled, errors.Join(context.DeadlineExceeded, errors.New("synthetic integrity failure"))} {
				fixture := newEvidenceTransactionV2Fixture(t, "", nil)
				inclusionRead, injected := false, false
				chain := canonicalReadbackTestClient(t, fixture.chain, func(ctx context.Context, calls []chainBatchRPCRequest) error {
					for _, call := range calls {
						if call.Method == "eth_getTransactionByBlockHashAndIndex" {
							inclusionRead = true
						}
						if inclusionRead && !injected && call.Method == "eth_getBlockByNumber" && len(call.Params) == 2 && string(call.Params[0]) == `"`+selector+`"` {
							injected = true
							return failure
						}
					}
					return nil
				}, nil)
				confirm := chain.ConfirmValidatorEvidenceTransactionV2Context
				if winner {
					confirm = chain.FindValidatorEvidenceSlotWinnerV2Context
				}
				result, err := confirm(t.Context(), fixture.expected)
				retryable := failure == io.ErrUnexpectedEOF || RetryableEvidenceTransportError(failure)
				if !injected || result != nil || !errors.Is(err, failure) || RetryableEvidenceTransportError(err) != retryable || strings.Contains(err.Error(), "canonical boundary changed") {
					t.Fatalf("winner=%t boundary=%s cause=%v result=%v err=%v", winner, selector, failure, result, err)
				}
				if result, err := confirm(t.Context(), fixture.expected); err != nil || result == nil || result.Receipt.TxHash != fixture.transaction.Hash() {
					t.Fatalf("exact readback did not recover after %v: %v", failure, err)
				}
			}
		}
	}
}

// A successfully returned different hash remains a hard failure on both owned
// and permissionless publication paths, with no timeout-based retry authority.
func TestValidatorEvidenceCanonicalReadbackRejectsObservedFork(t *testing.T) {
	for _, winner := range []bool{false, true} {
		for _, fault := range []string{"late-inclusion-reorg", "finalized-reorg"} {
			fixture := newEvidenceTransactionV2Fixture(t, fault, nil)
			confirm := fixture.chain.ConfirmValidatorEvidenceTransactionV2Context
			if winner {
				confirm = fixture.chain.FindValidatorEvidenceSlotWinnerV2Context
			}
			result, err := confirm(t.Context(), fixture.expected)
			if result != nil || err == nil || RetryableEvidenceTransportError(err) || !strings.Contains(err.Error(), "canonical") {
				t.Fatalf("winner=%t fault=%s result=%v err=%v", winner, fault, result, err)
			}
		}
	}
}

// Sequential healthy reads may together exceed thirty seconds; each actual
// request still has its full finite allowance under the caller's deadline.
func TestValidatorEvidenceReadbackUsesIndependentRequestDeadlines(t *testing.T) {
	for _, winner := range []bool{false, true} {
		fixture := newEvidenceTransactionV2Fixture(t, "", nil)
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
			defer cancel()
			started := time.Now()
			chain := canonicalReadbackTestClient(t, fixture.chain, func(ctx context.Context, calls []chainBatchRPCRequest) error {
				deadline, bounded := ctx.Deadline()
				if !bounded || deadline.Sub(time.Now()) != chainCallTimeout {
					return errors.New("synthetic read inherited another request's spent timeout")
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(8 * time.Second):
					return nil
				}
			}, fixture.handler)
			confirm := chain.ConfirmValidatorEvidenceTransactionV2Context
			if winner {
				confirm = chain.FindValidatorEvidenceSlotWinnerV2Context
			}
			result, err := confirm(ctx, fixture.expected)
			if err != nil || result == nil || time.Since(started) <= chainCallTimeout || result.Receipt.TxHash != fixture.transaction.Hash() {
				t.Fatalf("winner=%t elapsed=%s result=%v err=%v", winner, time.Since(started), result, err)
			}
		})
	}
}

// A caller's shorter overall deadline still cancels the complete observation
// and returns no proof, even though each subcall has a fresh local allowance.
func TestValidatorEvidenceReadbackRetainsCallerDeadline(t *testing.T) {
	fixture := newEvidenceTransactionV2Fixture(t, "", nil)
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
		defer cancel()
		started := time.Now()
		chain := canonicalReadbackTestClient(t, fixture.chain, func(ctx context.Context, calls []chainBatchRPCRequest) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(8 * time.Second):
				return nil
			}
		}, fixture.handler)
		result, err := chain.FindValidatorEvidenceSlotWinnerV2Context(ctx, fixture.expected)
		if result != nil || !errors.Is(err, context.DeadlineExceeded) || time.Since(started) != 35*time.Second || !RetryableEvidenceTransportError(err) {
			t.Fatalf("caller deadline lost: elapsed=%s result=%v err=%v", time.Since(started), result, err)
		}
	})
}

// The four direct geth calls cannot become unbounded when the former shared
// deadline is removed; virtual time waits for their actual request context.
func TestValidatorEvidenceReadbackBoundsDirectRequests(t *testing.T) {
	for _, method := range []string{"eth_getTransactionReceipt", "eth_getTransactionByBlockHashAndIndex", "eth_getLogs", "eth_getTransactionByHash"} {
		fixture := newEvidenceTransactionV2Fixture(t, "", nil)
		synctest.Test(t, func(t *testing.T) {
			injected := false
			started := time.Now()
			chain := canonicalReadbackTestClient(t, fixture.chain, func(ctx context.Context, calls []chainBatchRPCRequest) error {
				if len(calls) == 1 && calls[0].Method == method {
					injected = true
					<-ctx.Done()
					return ctx.Err()
				}
				return nil
			}, fixture.handler)
			result, err := chain.FindValidatorEvidenceSlotWinnerV2Context(t.Context(), fixture.expected)
			if !injected || result != nil || !errors.Is(err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(err) || time.Since(started) != chainCallTimeout {
				t.Fatalf("method=%s elapsed=%s result=%v err=%v", method, time.Since(started), result, err)
			}
		})
	}
}

// Upload staging and attempt validation must preserve a failed canonical
// read; neither a timeout nor an unread epoch establishes a changed boundary.
func TestValidatorCanonicalReadbackPreservesUploadAndAttemptErrors(t *testing.T) {
	for _, scope := range []string{"upload", "attempt-hash", "attempt-epoch"} {
		fixture := newEvidenceTransactionV2Fixture(t, "", nil)
		injected := false
		chain := canonicalReadbackTestClient(t, fixture.chain, func(ctx context.Context, calls []chainBatchRPCRequest) error {
			for _, call := range calls {
				if !injected && (scope == "attempt-epoch" && call.Method == "eth_call" || scope != "attempt-epoch" && call.Method == "eth_getBlockByNumber") {
					injected = true
					return context.DeadlineExceeded
				}
			}
			return nil
		}, nil)
		boundary := AttemptBoundary{SettlementEpoch: fixture.expected.Window.Epoch, EVMBlock: fixture.expected.Window.FinalizedBlock, EVMBlockHash: attemptHex32([32]byte{0xa1})}
		validate := func() error {
			if scope == "upload" {
				return chain.recheckValidatorUploadBlockContext(t.Context(), boundary.EVMBlock, [32]byte{0xa1})
			}
			return (&chainAttemptBoundaryRPC{chain: chain}).Validate(t.Context(), boundary)
		}
		if err := validate(); !injected || !errors.Is(err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(err) {
			t.Fatalf("%s lost typed read failure: %v", scope, err)
		}
		if err := validate(); err != nil {
			t.Fatalf("%s exact boundary did not recover: %v", scope, err)
		}
		boundary.EVMBlockHash = attemptHex32([32]byte{0xa2})
		var err error
		if scope == "upload" {
			err = chain.recheckValidatorUploadBlockContext(t.Context(), boundary.EVMBlock, [32]byte{0xa2})
		} else {
			err = (&chainAttemptBoundaryRPC{chain: chain}).Validate(t.Context(), boundary)
		}
		if err == nil || RetryableEvidenceTransportError(err) {
			t.Fatalf("%s observed fork was retryable: %v", scope, err)
		}
	}
}

// Pre-scan, event inclusion, and post-scan checkpoints must all succeed before
// returning sums; one transient read cannot poison the retry classification.
func TestValidatorCanonicalReadbackPreservesDepositScanErrors(t *testing.T) {
	for _, stage := range []string{"before", "event", "after"} {
		fixture := &depositedRPCFixture{finalized: 105, epoch: 5, epochStart: 101, contract: common.Address{0xcc}}
		fixture.logs = []map[string]any{depositedTestLog(fixture.contract, 101)}
		checkpointReads, injected := 0, false
		chain := canonicalReadbackTestClient(t, fixture.client(t), func(ctx context.Context, calls []chainBatchRPCRequest) error {
			for _, call := range calls {
				if call.Method != "eth_getBlockByNumber" || len(call.Params) != 2 {
					continue
				}
				var selector string
				if err := json.Unmarshal(call.Params[0], &selector); err != nil {
					return err
				}
				if selector == hexutil.EncodeUint64(fixture.finalized) {
					checkpointReads++
				}
				if !injected && (stage == "before" && checkpointReads == 1 || stage == "event" && selector == hexutil.EncodeUint64(101) || stage == "after" && checkpointReads == 2) {
					injected = true
					return context.DeadlineExceeded
				}
			}
			return nil
		}, nil)
		scan := func() (DepositSums, error) {
			return chain.depositedSumsAtFinalizedContext(t.Context(), 101, 105, nil, 105, [32]byte(depositedTestBlockHash(105)))
		}
		if sums, err := scan(); !injected || sums != nil || !errors.Is(err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(err) {
			t.Fatalf("%s checkpoint exposed sums or lost timeout: %v/%v", stage, sums, err)
		}
		if sums, err := scan(); err != nil || sums.Get(big.NewInt(1)).Int64() != 100 {
			t.Fatalf("%s checkpoint did not recover exactly: %v/%v", stage, sums, err)
		}
	}
}

// A failed epoch, start, or cached canonical read preserves the entire prior
// deposit ledger; the next complete scan commits its exact delta only once.
func TestValidatorCanonicalReadbackPreservesDepositLedgerErrors(t *testing.T) {
	for _, stage := range []string{"epoch", "start", "cached"} {
		fixture := &depositedRPCFixture{finalized: 105, epoch: 5, epochStart: 101, contract: common.Address{0xcc}}
		fixture.logs = []map[string]any{depositedTestLog(fixture.contract, 101)}
		views, injected := 0, false
		chain := canonicalReadbackTestClient(t, fixture.client(t), func(ctx context.Context, calls []chainBatchRPCRequest) error {
			for _, call := range calls {
				if call.Method == "eth_call" {
					views++
				}
				if !injected && (stage == "epoch" && views == 1 || stage == "start" && views == 2 || stage == "cached" && call.Method == "eth_getBlockByNumber" && len(call.Params) == 2 && string(call.Params[0]) == `"0x64"`) {
					injected = true
					return context.DeadlineExceeded
				}
			}
			return nil
		}, nil)
		steerer := &Steerer{chain: chain, deposits: depositLedger{conviction: DepositSums{"1": big.NewInt(25)}, scannedThrough: 100, scannedHash: [32]byte(depositedTestBlockHash(100)), started: true}}
		if _, _, err := steerer.gatherDeposits(big.NewInt(5)); !injected || !errors.Is(err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(err) || steerer.deposits.scannedThrough != 100 || steerer.deposits.conviction.Get(big.NewInt(1)).Int64() != 25 {
			t.Fatalf("%s changed ledger or lost timeout: %+v/%v", stage, steerer.deposits, err)
		}
		if _, sums, err := steerer.gatherDeposits(big.NewInt(5)); err != nil || sums.Get(big.NewInt(1)).Int64() != 125 || steerer.deposits.scannedThrough != 105 {
			t.Fatalf("%s ledger did not commit exact recovery: %v/%v", stage, sums, err)
		}
	}
}
