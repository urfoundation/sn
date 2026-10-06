// Native/EVM first insertion needs current finality as well as unchanged
// canonical hashes. These controls preserve the exact historical query.
package crv4

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Only the closing finalized response changes after both authenticated map
// reads. A valid advancement retains the selected native100/EVM70 mapping.
func TestEvmCheckpointClosesOriginalFinalizedWitness(t *testing.T) {
	for _, fault := range []string{"unchanged", "advanced", "regressed", "missing head", "changed closing hash", "head timeout", "header timeout"} {
		fixture, query, _ := newEVMCheckpointTestFixture(t)
		opening := fixture.finalized
		advancedHeader, advanced := receiptTestHeader(t, opening, 104, nil, 1)
		fixture.headers[advanced.Hex()], fixture.blockHashes[104] = advancedHeader, advanced
		originalHook := fixture.hook
		finalizedReads := 0
		fixture.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
			if method == "chain_getFinalizedHead" {
				finalizedReads++
				if finalizedReads == 2 {
					if len(fixture.storageCalls) != 2 {
						t.Fatalf("%s finality closure preceded map reads: %v", fault, fixture.storageCalls)
					}
					switch fault {
					case "advanced", "changed closing hash", "header timeout":
						return true, setRuntimeIdentityTestResult(result, advanced.Hex())
					case "regressed":
						return true, setRuntimeIdentityTestResult(result, query.NativeHash.Hex())
					case "missing head":
						return true, setRuntimeIdentityTestResult(result, "")
					case "head timeout":
						return true, context.DeadlineExceeded
					}
				}
			}
			if finalizedReads == 2 && fault == "header timeout" && method == "chain_getHeader" {
				return true, context.DeadlineExceeded
			}
			if finalizedReads == 2 && fault == "changed closing hash" && method == "chain_getBlockHash" && args[0] == uint64(104) {
				return true, setRuntimeIdentityTestResult(result, types.Hash{99}.Hex())
			}
			return originalHook(ctx, result, method, args...)
		}
		observed, err := ReadEVMCheckpointAtContext(fixture.ctx, fixture.chain, query, fixture.allowed...)
		wantSuccess := fault == "unchanged" || fault == "advanced"
		if (err == nil) != wantSuccess {
			t.Fatalf("%s finality success=%t finalized reads=%d observation=%+v error=%v", fault, wantSuccess, finalizedReads, observed, err)
		}
		var unavailable *ReceiptEvidenceUnavailableError
		if fault == "regressed" && !errors.As(err, &unavailable) || fault == "changed closing hash" && errors.As(err, &unavailable) {
			t.Fatalf("%s changed its pending/canonical cause: %v", fault, err)
		}
		if wantSuccess && observed.Query != query || !wantSuccess && observed != (EVMCheckpointObservation{}) {
			t.Fatalf("%s replaced the original query or published partial proof: %+v", fault, observed)
		}
		if strings.HasSuffix(fault, "timeout") && (!errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "regressed") || strings.Contains(err.Error(), "not canonical")) {
			t.Fatalf("%s became finality contradiction: %v", fault, err)
		}
		if fixture.chain.Runtime.SpecVersion != 999 {
			t.Fatalf("%s changed dial-time signing metadata", fault)
		}
	}
}

func TestEvmCheckpointOwnerRetainsLowerHeadAndOriginalCanonicalWitness(t *testing.T) {
	for _, changed := range []bool{false, true} {
		fixture, query, _ := newEVMCheckpointTestFixture(t)
		opening := fixture.finalized
		parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		defer cancel()
		fixture.ctx = WithFinalityReadOwnerContext(parent)
		originalHook := fixture.hook
		heads := 0
		fixture.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
			if method == "chain_getFinalizedHead" {
				heads++
				if heads >= 2 {
					return true, setRuntimeIdentityTestResult(result, query.NativeHash.Hex())
				}
			}
			if changed && heads >= 2 && method == "chain_getBlockHash" && args[0] == uint64(103) {
				return true, setRuntimeIdentityTestResult(result, types.Hash{99}.Hex())
			}
			return originalHook(ctx, result, method, args...)
		}
		for attempt := 0; attempt < 2; attempt++ {
			got, err := ReadEVMCheckpointAtContext(fixture.ctx, fixture.chain, query, fixture.allowed...)
			var unavailable *ReceiptEvidenceUnavailableError
			if err == nil || got != (EVMCheckpointObservation{}) || errors.As(err, &unavailable) == changed {
				t.Fatalf("changed=%t attempt%d lost original checkpoint constraint: %+v %v", changed, attempt, got, err)
			}
			if !changed && unavailable.BlockHash != opening {
				t.Fatal("pending checkpoint replaced its original opening")
			}
		}
		if len(fixture.storageCalls) != 2 {
			t.Fatal("second lower opening reread mapping after discarding retained finality")
		}
		fixture.hook = originalHook
		if got, err := ReadEVMCheckpointAtContext(fixture.ctx, fixture.chain, query, fixture.allowed...); err != nil || got.Query != query {
			t.Fatalf("original checkpoint failed to recover with original finality: %+v %v", got, err)
		}
	}
}

// Err changes only at a completed raw response; there is no timer race.
type finalityReadLateContext struct {
	context.Context
	done  chan struct{}
	cause error
}

func (self *finalityReadLateContext) Done() <-chan struct{} { return self.done }
func (self *finalityReadLateContext) Err() error {
	select {
	case <-self.done:
		return self.cause
	default:
		return nil
	}
}

func TestEvmCheckpointOwnerCompletedForkDominatesLateBoundary(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, fork := range []bool{false, true} {
			fixture, query, _ := newEVMCheckpointTestFixture(t)
			parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			late := &finalityReadLateContext{Context: parent, done: make(chan struct{}), cause: cause}
			expired := false
			defer func() {
				if !expired {
					close(late.done)
				}
			}()
			fixture.ctx = WithFinalityReadOwnerContext(late)
			if _, err := ReadEVMCheckpointAtContext(fixture.ctx, fixture.chain, query, fixture.allowed...); err != nil {
				t.Fatal(err)
			}
			if fork {
				header, replacement := receiptTestHeader(t, types.Hash{99}, 103, nil, 1)
				fixture.finalized = replacement
				fixture.headers[replacement.Hex()], fixture.blockHashes[103] = header, replacement
			}
			originalHook := fixture.hook
			fixture.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
				if method == "chain_getBlockHash" && args[0] == uint64(103) {
					if err := setRuntimeIdentityTestResult(result, fixture.finalized.Hex()); err != nil {
						return true, err
					}
					expired = true
					close(late.done)
					<-ctx.Done()
					return true, nil
				}
				return originalHook(ctx, result, method, args...)
			}
			got, err := ReadEVMCheckpointAtContext(fixture.ctx, fixture.chain, query, fixture.allowed...)
			if !expired || got != (EVMCheckpointObservation{}) || !errors.Is(err, cause) ||
				strings.Contains(err.Error(), "same height") != fork || len(fixture.storageCalls) != 2 {
				t.Fatalf("fork=%t cause=%v lost completed checkpoint evidence: %+v %v", fork, cause, got, err)
			}
		}
	}
}
