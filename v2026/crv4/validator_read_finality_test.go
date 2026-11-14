// Raw finalized heads change only after real identity, stake or epoch bytes.
// These public-reader controls never replace admission with a synthetic verdict.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Each fixture owns one public reader and its exact last dependent response.
type validatorReadFinalityTestFixture struct {
	identity         *validatorIdentityTestFixture
	read             func() (ValidatorIdentityObservation, error)
	afterDependent   func()
	dependentRead    bool
	closingHeadReads int
}

// The shared script retains the actual storage, metadata and metagraph codecs.
func newValidatorReadFinalityTestFixture(t *testing.T, reader string) *validatorReadFinalityTestFixture {
	t.Helper()
	fixture := &validatorReadFinalityTestFixture{}
	var read func() (ValidatorIdentityObservation, error)
	switch reader {
	case "identity":
		fixture.identity = newValidatorIdentityTestFixture(t)
		read = func() (ValidatorIdentityObservation, error) {
			return ReadValidatorIdentityAtContext(fixture.identity.ctx, fixture.identity.chain, fixture.identity.query, fixture.identity.allowed...)
		}
	case "stake":
		stake := newValidatorStakeTestFixture(t)
		fixture.identity = stake.identity
		read = func() (ValidatorIdentityObservation, error) {
			observed, err := stake.read()
			if err != nil && observed != (ValidatorStakeObservation{}) {
				t.Fatalf("stake failure published partial data: %+v %v", observed, err)
			}
			if err == nil && (observed.TotalStakeRao != 150 || !observed.MeetsNonSelfStakeAndPermit()) {
				t.Fatalf("stake success skipped real calculated stake: %+v", observed)
			}
			return observed.Identity, err
		}
	case "census":
		stake := newValidatorStakeTestFixture(t)
		fixture.identity = stake.identity
		read = func() (ValidatorIdentityObservation, error) {
			observed, err := ReadValidatorStakeCensusAtContext(stake.identity.ctx, stake.identity.chain, stake.identity.query, stake.identity.allowed...)
			if err != nil && (observed.Selected != (ValidatorStakeObservation{}) || observed.Entries != nil) {
				t.Fatalf("census failure published partial data: %+v %v", observed, err)
			}
			if err == nil && (len(observed.Entries) != 3 || observed.Selected.TotalStakeRao != 150 || observed.Entries[1].TotalStakeFloorRao != 150 || !observed.Selected.MeetsNonSelfStakeAndPermit()) {
				t.Fatalf("census success skipped real calculated stake: %+v", observed)
			}
			return observed.Selected.Identity, err
		}
	case "schedule":
		stake, query := newValidatorScheduleTestFixture(t)
		fixture.identity = stake.identity
		read = func() (ValidatorIdentityObservation, error) {
			observed, err := ReadValidatorScheduleAtContext(stake.identity.ctx, stake.identity.chain, query, stake.identity.allowed...)
			if err != nil && observed != (ValidatorScheduleObservation{}) {
				t.Fatalf("schedule failure published partial data: %+v %v", observed, err)
			}
			if err == nil && (observed.SubnetEpochIndex != 77 || observed.Stake.TotalStakeRao != 150 || !observed.Stake.MeetsNonSelfStakeAndPermit()) {
				t.Fatalf("schedule success skipped real epoch or stake: %+v", observed)
			}
			return observed.Stake.Identity, err
		}
	default:
		t.Fatalf("unknown public validator reader %q", reader)
	}
	fixture.read = func() (ValidatorIdentityObservation, error) {
		fixture.dependentRead, fixture.closingHeadReads = false, 0
		return read()
	}
	complete := func() {
		fixture.dependentRead = true
		if fixture.afterDependent != nil {
			fixture.afterDependent()
		}
	}
	identity := fixture.identity
	originalHook := identity.hook
	identity.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
		if method == "chain_getFinalizedHead" && fixture.dependentRead {
			fixture.closingHeadReads++
		}
		if originalHook != nil {
			handled, err := originalHook(ctx, result, method, args...)
			if (reader == "stake" || reader == "census") && method == "state_call" && handled && err == nil {
				complete()
			}
			return handled, err
		}
		return false, nil
	}
	identity.after = func(method string, args ...any) {
		if method != "state_getStorage" {
			return
		}
		field := identity.keyNames[args[0].(string)]
		if reader == "identity" && field == "ValidatorPermit" || reader == "schedule" && field == "SubnetEpochIndex" {
			complete()
		}
	}
	return fixture
}

// Authentic full headers let lower-only fixtures retain every canonical hash.
func (self *validatorReadFinalityTestFixture) setHead(t *testing.T, number uint64) types.Hash {
	t.Helper()
	if hash, found := self.identity.blockHashes[number]; found {
		self.identity.finalized = hash
		return hash
	}
	header, hash := receiptTestHeader(t, types.Hash{41}, number, nil, 1)
	self.identity.headers[hash.Hex()], self.identity.blockHashes[number] = header, hash
	self.identity.finalized = hash
	return hash
}

// Even a still-covered selected block cannot erase the earlier finalized head.
func TestValidatorReadFinalityClosesEachDependentObservation(t *testing.T) {
	t.Parallel()
	for _, reader := range []string{"identity", "stake", "census", "schedule"} {
		for _, number := range []uint64{100, 99} {
			fixture := newValidatorReadFinalityTestFixture(t, reader)
			fixture.afterDependent = func() { fixture.setHead(t, number) }
			observed, err := fixture.read()
			var unavailable *ReceiptEvidenceUnavailableError
			if !fixture.dependentRead || fixture.closingHeadReads != 1 || observed != (ValidatorIdentityObservation{}) || !errors.As(err, &unavailable) || strings.Contains(err.Error(), "changed") {
				t.Fatalf("%s closing head %d escaped or became a fork: %+v %v, heads=%d", reader, number, observed, err, fixture.closingHeadReads)
			}
		}
	}
}

// Stable and advancing heads preserve the historical pin and signing state.
func TestValidatorReadFinalityAcceptsStableAndAdvancedHeads(t *testing.T) {
	t.Parallel()
	for _, reader := range []string{"identity", "stake", "census", "schedule"} {
		for _, number := range []uint64{103, 104} {
			fixture := newValidatorReadFinalityTestFixture(t, reader)
			identity := fixture.identity
			opening, metadata, runtime := identity.finalized, identity.chain.Meta, identity.chain.Runtime
			fixture.afterDependent = func() { fixture.setHead(t, number) }
			observed, err := fixture.read()
			if err != nil || !fixture.dependentRead || fixture.closingHeadReads != 1 || observed.BlockHash != identity.query.BlockHash || observed.BlockNumber != 100 || observed.FinalizedHash != opening || observed.FinalizedNumber != 103 || observed.Runtime != identity.allowed[0] || identity.chain.Meta != metadata || identity.chain.Runtime != runtime {
				t.Fatalf("%s valid closing head %d changed authority or pins: %+v %v", reader, number, observed, err)
			}
		}
	}
}

// Opening lag is unavailable rather than evidence that a canonical block forked.
func TestValidatorReadFinalityOpeningCoverageIsPending(t *testing.T) {
	t.Parallel()
	for _, reader := range []string{"identity", "stake", "census", "schedule"} {
		fixture := newValidatorReadFinalityTestFixture(t, reader)
		fixture.setHead(t, 99)
		observed, err := fixture.read()
		var unavailable *ReceiptEvidenceUnavailableError
		if fixture.dependentRead || observed != (ValidatorIdentityObservation{}) || !errors.As(err, &unavailable) {
			t.Fatalf("%s authentic opening lag became hard or published: %+v %v", reader, observed, err)
		}
	}
}

// Absent opening hashes are unavailable; malformed, zero and conflicting bytes stay hard.
func TestValidatorReadFinalityOpeningCanonicalAbsenceRemainsPending(t *testing.T) {
	t.Parallel()
	for _, reader := range []string{"identity", "stake", "census", "schedule"} {
		for _, number := range []uint64{0, 100} {
			for _, fault := range []string{"null", "empty", "absent", "malformed", "zero", "different"} {
				fixture := newValidatorReadFinalityTestFixture(t, reader)
				identity := fixture.identity
				parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
				defer cancel()
				identity.ctx = WithFinalityReadOwnerContext(parent)
				originalHook := identity.hook
				injected := false
				identity.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
					if method == "chain_getBlockHash" && args[0] == number && !injected {
						injected = true
						switch fault {
						case "null":
							return true, setRuntimeIdentityTestResult(result, nil)
						case "empty":
							return true, setRuntimeIdentityTestResult(result, "")
						case "absent":
							*result.(*json.RawMessage) = nil
							return true, nil
						case "malformed":
							return true, setRuntimeIdentityTestResult(result, "0x1234")
						case "zero":
							return true, setRuntimeIdentityTestResult(result, types.Hash{}.Hex())
						default:
							return true, setRuntimeIdentityTestResult(result, types.Hash{98}.Hex())
						}
					}
					return originalHook(ctx, result, method, args...)
				}
				observed, err := fixture.read()
				var unavailable *ReceiptEvidenceUnavailableError
				missing := fault == "null" || fault == "empty" || fault == "absent"
				if !injected || fixture.dependentRead || observed != (ValidatorIdentityObservation{}) || err == nil || errors.As(err, &unavailable) != missing || substrateRPCDisconnected(err) {
					t.Fatalf("%s opening %d %s changed evidence classification: %+v %v", reader, number, fault, observed, err)
				}
				observed, err = fixture.read()
				if err != nil || observed.BlockHash != identity.query.BlockHash || !fixture.dependentRead {
					t.Fatalf("%s opening %d %s recovery changed selected block: %+v %v", reader, number, fault, observed, err)
				}
			}
		}
	}
}

// Actual current, selected and original hash contradictions dominate lower heads.
func TestValidatorReadFinalityAuthenticatesCurrentAndRetainedWitnesses(t *testing.T) {
	t.Parallel()
	for _, reader := range []string{"identity", "stake", "census", "schedule"} {
		for _, fault := range []string{"orphan current", "changed original", "changed selected", "same height fork"} {
			fixture := newValidatorReadFinalityTestFixture(t, reader)
			identity := fixture.identity
			fixture.afterDependent = func() {
				switch fault {
				case "orphan current":
					fixture.setHead(t, 104)
					identity.blockHashes[104] = types.Hash{91}
				case "changed original":
					fixture.setHead(t, 99)
					identity.blockHashes[103] = types.Hash{92}
				case "changed selected":
					fixture.setHead(t, 99)
					identity.blockHashes[100] = types.Hash{93}
				case "same height fork":
					header, hash := receiptTestHeader(t, types.Hash{94}, 103, nil, 1)
					identity.headers[hash.Hex()], identity.blockHashes[103], identity.finalized = header, hash, hash
				}
			}
			observed, err := fixture.read()
			var unavailable *ReceiptEvidenceUnavailableError
			marker := "changed its canonical hash"
			if fault == "same height fork" {
				marker = "completed finalized witnesses disagree at the same height"
			}
			if !fixture.dependentRead || fixture.closingHeadReads != 1 || observed != (ValidatorIdentityObservation{}) || err == nil || errors.As(err, &unavailable) || !strings.Contains(err.Error(), marker) || substrateRPCDisconnected(err) {
				t.Fatalf("%s %s lost its hard completed contradiction: %+v %v", reader, fault, observed, err)
			}
		}
	}
}

// Re-entering a public reader borrows the same finite owner and original high head.
func TestValidatorReadFinalityRetainsWitnessAcrossPublicRetries(t *testing.T) {
	t.Parallel()
	for _, reader := range []string{"identity", "stake", "census", "schedule"} {
		fixture := newValidatorReadFinalityTestFixture(t, reader)
		parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		defer cancel()
		fixture.identity.ctx = WithFinalityReadOwnerContext(parent)
		fixture.afterDependent = func() { fixture.setHead(t, 100) }
		for attempt := 0; attempt < 2; attempt++ {
			observed, err := fixture.read()
			var unavailable *ReceiptEvidenceUnavailableError
			if observed != (ValidatorIdentityObservation{}) || !errors.As(err, &unavailable) || fixture.dependentRead != (attempt == 0) {
				t.Fatalf("%s retry %d reset its original witness: %+v %v", reader, attempt, observed, err)
			}
		}
		fixture.afterDependent = nil
		fixture.setHead(t, 104)
		observed, err := fixture.read()
		if err != nil || observed.BlockHash != fixture.identity.query.BlockHash || !fixture.dependentRead {
			t.Fatalf("%s recovery did not retain its selected block: %+v %v", reader, observed, err)
		}
	}
}

// A nested identity's advancing closing head constrains later dependent reads.
func TestValidatorReadFinalityRetainsNestedClosingHighWater(t *testing.T) {
	t.Parallel()
	for _, reader := range []string{"stake", "census", "schedule"} {
		fixture := newValidatorReadFinalityTestFixture(t, reader)
		identity := fixture.identity
		originalAfter := identity.after
		identity.after = func(method string, args ...any) {
			if method == "state_getStorage" && identity.keyNames[args[0].(string)] == "ValidatorPermit" {
				fixture.setHead(t, 104)
			}
			originalAfter(method, args...)
		}
		fixture.afterDependent = func() { fixture.setHead(t, 103) }
		observed, err := fixture.read()
		var unavailable *ReceiptEvidenceUnavailableError
		if !fixture.dependentRead || fixture.closingHeadReads != 1 || observed != (ValidatorIdentityObservation{}) || !errors.As(err, &unavailable) || !strings.Contains(err.Error(), "original read owner witness") {
			t.Fatalf("%s discarded the nested closing high head: %+v %v", reader, observed, err)
		}
	}
}

// A later opening below the selected height must still check the owner's old hash.
func TestValidatorReadFinalityRetainedForkDominatesOpeningLag(t *testing.T) {
	t.Parallel()
	for _, reader := range []string{"identity", "stake", "census", "schedule"} {
		fixture := newValidatorReadFinalityTestFixture(t, reader)
		parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		defer cancel()
		fixture.identity.ctx = WithFinalityReadOwnerContext(parent)
		fixture.afterDependent = func() { fixture.setHead(t, 100) }
		_, firstErr := fixture.read()
		var unavailable *ReceiptEvidenceUnavailableError
		if !errors.As(firstErr, &unavailable) {
			t.Fatalf("%s initial lower head did not retain pending evidence: %v", reader, firstErr)
		}
		fixture.setHead(t, 99)
		fixture.identity.blockHashes[103] = types.Hash{95}
		observed, err := fixture.read()
		if fixture.dependentRead || observed != (ValidatorIdentityObservation{}) || err == nil || errors.As(err, &unavailable) || !strings.Contains(err.Error(), "changed its canonical hash") {
			t.Fatalf("%s opening lag concealed the retained fork: %+v %v", reader, observed, err)
		}
	}
}

// Missing heads and transport deadlines remain unknown; joined hard causes do not.
func TestValidatorReadFinalityPreservesUnavailableAndTimeoutCauses(t *testing.T) {
	t.Parallel()
	for _, reader := range []string{"identity", "stake", "census", "schedule"} {
		for _, fault := range []string{"null", "empty", "deadline", "mixed hard"} {
			fixture := newValidatorReadFinalityTestFixture(t, reader)
			identity := fixture.identity
			parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			identity.ctx = WithFinalityReadOwnerContext(parent)
			originalHook := identity.hook
			injected := false
			hard := errors.New("synthetic invalid finalized reply")
			identity.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
				if method == "chain_getFinalizedHead" && fixture.dependentRead && !injected {
					injected = true
					switch fault {
					case "null":
						return true, setRuntimeIdentityTestResult(result, nil)
					case "empty":
						return true, setRuntimeIdentityTestResult(result, "")
					case "deadline":
						return true, fmt.Errorf("synthetic finalized read: %w", context.DeadlineExceeded)
					default:
						return true, errors.Join(hard, context.DeadlineExceeded)
					}
				}
				return originalHook(ctx, result, method, args...)
			}
			observed, err := fixture.read()
			var unavailable *ReceiptEvidenceUnavailableError
			missing := fault == "null" || fault == "empty"
			if !injected || observed != (ValidatorIdentityObservation{}) || err == nil || errors.As(err, &unavailable) != missing || errors.Is(err, context.DeadlineExceeded) != !missing || strings.Contains(err.Error(), "changed") || errors.Is(err, hard) != (fault == "mixed hard") || substrateRPCDisconnected(err) != (fault == "deadline") {
				t.Fatalf("%s %s changed its full error cause: %+v %v", reader, fault, observed, err)
			}
			observed, err = fixture.read()
			if err != nil || observed.BlockHash != identity.query.BlockHash || !fixture.dependentRead {
				t.Fatalf("%s %s recovery changed selected block: %+v %v", reader, fault, observed, err)
			}
		}
	}
}

// Completed canonical bytes retain a hard contradiction at a late context boundary.
func TestValidatorReadFinalityCompletedForkDominatesLateBoundary(t *testing.T) {
	t.Parallel()
	for _, reader := range []string{"identity", "stake", "census", "schedule"} {
		for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
			for _, fork := range []bool{false, true} {
				fixture := newValidatorReadFinalityTestFixture(t, reader)
				identity := fixture.identity
				parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
				defer cancel()
				late := &finalityReadLateContext{Context: parent, done: make(chan struct{}), cause: cause}
				t.Cleanup(func() {
					select {
					case <-late.done:
					default:
						close(late.done)
					}
				})
				identity.ctx = WithFinalityReadOwnerContext(late)
				originalHook := identity.hook
				completed := false
				identity.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
					if fixture.dependentRead && method == "chain_getBlockHash" && args[0] == uint64(103) {
						hash := identity.finalized
						if fork {
							hash = types.Hash{96}
						}
						if err := setRuntimeIdentityTestResult(result, hash.Hex()); err != nil {
							return true, err
						}
						completed = true
						close(late.done)
						<-ctx.Done()
						return true, nil
					}
					return originalHook(ctx, result, method, args...)
				}
				observed, err := fixture.read()
				if !completed || observed != (ValidatorIdentityObservation{}) || !errors.Is(err, cause) || strings.Contains(err.Error(), "changed its canonical hash") != fork || substrateRPCDisconnected(err) != (!fork && cause == context.DeadlineExceeded) {
					t.Fatalf("%s completed fork=%t lost bytes or context %v: %+v %v", reader, fork, cause, observed, err)
				}
			}
		}
	}
}

// Fresh public invocations must compare completed tuples before late cancellation.
func TestValidatorReadFinalityRetainedTupleDominatesLateOpeningBoundary(t *testing.T) {
	t.Parallel()
	for _, reader := range []string{"identity", "stake", "census", "schedule"} {
		for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
			for _, fork := range []bool{false, true} {
				fixture := newValidatorReadFinalityTestFixture(t, reader)
				identity := fixture.identity
				parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
				defer cancel()
				late := &finalityReadLateContext{Context: parent, done: make(chan struct{}), cause: cause}
				t.Cleanup(func() {
					select {
					case <-late.done:
					default:
						close(late.done)
					}
				})
				identity.ctx = WithFinalityReadOwnerContext(late)
				if _, err := fixture.read(); err != nil {
					t.Fatalf("%s could not retain the first finalized tuple: %v", reader, err)
				}
				if fork {
					header, hash := receiptTestHeader(t, types.Hash{97}, 103, nil, 1)
					identity.headers[hash.Hex()], identity.blockHashes[103], identity.finalized = header, hash, hash
				}
				originalHook := identity.hook
				completed := false
				identity.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
					if method == "chain_getBlockHash" && args[0] == uint64(103) {
						if err := setRuntimeIdentityTestResult(result, identity.finalized.Hex()); err != nil {
							return true, err
						}
						completed = true
						close(late.done)
						<-ctx.Done()
						return true, nil
					}
					return originalHook(ctx, result, method, args...)
				}
				observed, err := fixture.read()
				if !completed || fixture.dependentRead || observed != (ValidatorIdentityObservation{}) || !errors.Is(err, cause) || strings.Contains(err.Error(), "completed finalized witnesses disagree at the same height") != fork || substrateRPCDisconnected(err) != (!fork && cause == context.DeadlineExceeded) {
					t.Fatalf("%s retained fork=%t lost completed opening tuple at %v: %+v %v", reader, fork, cause, observed, err)
				}
			}
		}
	}
}
