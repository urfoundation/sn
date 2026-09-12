package validator

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

func TestProvisionalNativeWeightsClassifiesOnlyActualPreIntentRejections(t *testing.T) {
	_, _, empty := BuildWeightVectorExact(nil, nil, protocol.Rational{Numerator: 1, Denominator: 2}, nil)
	_, capped := crv4.ApplyMaxWeightLimitRational([]*big.Rat{big.NewRat(1, 1)}, 32768)
	var capError *crv4.InfeasibleWeightLimitError
	if !errors.Is(empty, errNoPositiveUnmaskedWeights) || !errors.As(capped, &capError) || capError.PositiveWeights != 1 || capError.MaxWeightLimit != 32768 {
		t.Fatalf("actual weight failures lost their classifications: empty=%v capped=%v", empty, capped)
	}
	for _, cause := range []error{empty, capped} {
		wrapped := fmt.Errorf("preparation: %w", cause)
		got := classifyProvisionalNativeWeights(context.Background(), true, 1400, 302, wrapped)
		var rejected *provisionalNativeWeightRejection
		if !errors.As(got, &rejected) || rejected.nativeEpoch != 1400 || rejected.settlementEpoch != 302 || !errors.Is(got, cause) {
			t.Fatalf("actual rejection was not retained: %v", got)
		}
		if got := classifyProvisionalNativeWeights(context.Background(), false, 1400, 302, wrapped); got != wrapped {
			t.Fatalf("ordinary startup classification changed: %v", got)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if got := classifyProvisionalNativeWeights(ctx, true, 1400, 302, wrapped); got != wrapped {
			t.Fatalf("canceled work classified as a retryable rejection: %v", got)
		}
	}
	_, malformed := crv4.ApplyMaxWeightLimitRational([]*big.Rat{nil}, 32768)
	for _, cause := range []error{
		malformed,
		errors.New("no positive unmasked weights"), // Text is not an authority.
		errors.Join(empty, context.Canceled),
		errors.Join(capped, errors.New("journal close failed")),
	} {
		if got := classifyProvisionalNativeWeights(context.Background(), true, 1400, 302, cause); got != cause {
			t.Fatalf("unrelated/mixed failure was classified: %v", got)
		}
	}
}

func TestProvisionalNativeWeightsRetriesPastFailureLimitAndAcceptsLaterFunding(t *testing.T) {
	for _, cause := range []error{errNoPositiveUnmaskedWeights, &crv4.InfeasibleWeightLimitError{MaxWeightLimit: 32768, PositiveWeights: 1}} {
		t.Run(cause.Error(), func(t *testing.T) {
			reads, attempts := 0, 0
			err := runReleaseSteeringLoopWithWaitAndDeferral(context.Background(), func() (uint64, error) { reads++; return 1400, nil }, func() error {
				attempts++
				if attempts <= releaseSteeringFailureLimit+2 {
					return classifyProvisionalNativeWeights(context.Background(), true, 1400, 302, cause)
				}
				// A fresh observation in the same epoch can now yield an actual
				// admissible result. It must still be attempted exactly once.
				return nil
			}, func() bool { return reads < releaseSteeringFailureLimit+5 }, true)
			if err != nil || attempts != releaseSteeringFailureLimit+3 {
				t.Fatalf("same-epoch recovery was suppressed: attempts=%d err=%v", attempts, err)
			}
		})
	}
}

func TestProvisionalNativeWeightsAllowsFreshEpochOnlyWithoutUnresolvedFailure(t *testing.T) {
	broken := errors.New("native receipt read failed")
	for _, preceding := range []string{"none", "submit", "scheduler", "pending cut"} {
		t.Run(preceding, func(t *testing.T) {
			reads, attempts := 0, 0
			err := runReleaseSteeringLoopWithWaitAndDeferral(context.Background(), func() (uint64, error) {
				reads++
				if reads == 2 && preceding == "scheduler" {
					return 0, broken
				}
				if reads > 2 {
					return 1401, nil
				}
				return 1400, nil
			}, func() error {
				attempts++
				if reads > 2 {
					return nil
				}
				if reads == 1 && preceding == "submit" {
					return broken
				}
				if reads == 2 && preceding == "pending cut" {
					return errAttemptCutPending
				}
				return classifyProvisionalNativeWeights(context.Background(), true, 1400, 302, errNoPositiveUnmaskedWeights)
			}, func() bool { return reads < 4 }, true)
			if preceding == "none" {
				if err != nil || attempts != 3 {
					t.Fatalf("fresh epoch was not attempted: attempts=%d err=%v", attempts, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "incomplete epoch") || ((preceding == "submit" || preceding == "scheduler") && !errors.Is(err, broken)) {
				t.Fatalf("unresolved failure/custody escaped continuity: %v", err)
			}
		})
	}
}

func TestProvisionalNativeWeightsPreservesStrictAndUnrelatedFailureBudget(t *testing.T) {
	rejected := &provisionalNativeWeightRejection{nativeEpoch: 1400, settlementEpoch: 302, cause: errNoPositiveUnmaskedWeights}
	for _, test := range []struct {
		name  string
		allow bool
		err   error
	}{
		{"strict", false, rejected},
		{"different epoch", true, &provisionalNativeWeightRejection{nativeEpoch: 1401, settlementEpoch: 302, cause: errNoPositiveUnmaskedWeights}},
		{"joined failure", true, errors.Join(rejected, errors.New("signed intent storage failure"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			err := runReleaseSteeringLoopWithWaitAndDeferral(context.Background(), func() (uint64, error) { return 1400, nil }, func() error { attempts++; return test.err }, func() bool { return attempts <= releaseSteeringFailureLimit+2 }, test.allow)
			if err == nil || attempts != releaseSteeringFailureLimit || !errors.Is(err, test.err) {
				t.Fatalf("fatal handling changed: attempts=%d err=%v", attempts, err)
			}
		})
	}
	broken := errors.New("native RPC failed")
	attempts := 0
	err := runReleaseSteeringLoopWithWaitAndDeferral(context.Background(), func() (uint64, error) { return 1400, nil }, func() error {
		attempts++
		if attempts >= releaseSteeringFailureLimit && attempts < 2*releaseSteeringFailureLimit+2 {
			return rejected
		}
		return broken
	}, func() bool { return attempts < 3*releaseSteeringFailureLimit }, true)
	if !errors.Is(err, broken) || attempts != 2*releaseSteeringFailureLimit+2 {
		t.Fatalf("weight rejection erased prior real failures: attempts=%d err=%v", attempts, err)
	}
}

func TestProvisionalNativeWeightsHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reads, attempts := 0, 0
	err := runReleaseSteeringLoopWithWaitAndDeferral(ctx, func() (uint64, error) { reads++; return 1400, nil }, func() error {
		attempts++
		return classifyProvisionalNativeWeights(ctx, true, 1400, 302, errNoPositiveUnmaskedWeights)
	}, func() bool { cancel(); return true }, true)
	if err != nil || reads != 1 || attempts != 1 {
		t.Fatalf("cancellation admitted further work: reads=%d attempts=%d err=%v", reads, attempts, err)
	}
}
