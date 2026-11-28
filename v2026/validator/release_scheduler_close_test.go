package validator

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/urfoundation/sn/v2026/crv4"
)

// A normal close during a scheduler observation creates no native custody.
// It must not poison the next explicit pre-intent no-submission disposition.
func TestReleaseSchedulerNormalClosePreservesFreshWeightRejection(t *testing.T) {
	_, infeasible := crv4.ApplyMaxWeightLimitRational([]*big.Rat{big.NewRat(1, 1)}, 32768)
	if infeasible == nil {
		t.Fatal("one-recipient fixture unexpectedly meets the weight cap")
	}
	reads, attempts := 0, 0
	err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) {
		reads++
		if reads == 2 {
			return 0, fmt.Errorf("crv4: finalized head: %w", &websocket.CloseError{Code: websocket.CloseNormalClosure})
		}
		if reads == 29 {
			return 22, nil
		}
		return 21, nil
	}, func() error {
		attempts++
		if reads == 29 {
			return nil
		}
		return classifyProvisionalNativeWeights(t.Context(), true, 21, 31, infeasible)
	}, func() bool { return reads < 29 }, false, true)
	if err != nil || reads != 29 || attempts != 28 {
		t.Fatalf("recovered scheduler poisoned pre-intent no-submission: reads=%d attempts=%d err=%v", reads, attempts, err)
	}
}

// An intervening scheduler disconnect must not count as a fourth failed
// native submission, nor clear the first three unresolved native failures.
func TestReleaseSchedulerNormalClosePreservesNativeFailureAccounting(t *testing.T) {
	closed := &websocket.CloseError{Code: websocket.CloseNormalClosure}
	broken := errors.New("prepared native intent receipt unavailable")
	reads, submissions := 0, 0
	err := runReleaseSteeringLoopWithWait(t.Context(), func() (uint64, error) {
		reads++
		if reads == 4 {
			return 0, fmt.Errorf("crv4: finalized head: %w", closed)
		}
		return 21, nil
	}, func() error { submissions++; return broken }, func() bool { return reads < releaseSteeringFailureLimit+3 })
	if !errors.Is(err, broken) || errors.Is(err, closed) || reads != releaseSteeringFailureLimit+1 || submissions != releaseSteeringFailureLimit {
		t.Fatalf("scheduler disconnect spent or cleared native failures: reads=%d submissions=%d err=%v", reads, submissions, err)
	}
}

func TestReleaseSchedulerCloseKeepsPermanentAndMixedFailuresBlocking(t *testing.T) {
	closed := &websocket.CloseError{Code: websocket.CloseNormalClosure}
	for _, cause := range []error{
		errors.Join(closed, errors.New("native metadata does not match")),
		errors.Join(closed, context.Canceled),
		&websocket.CloseError{Code: websocket.ClosePolicyViolation},
		&websocket.CloseError{Code: websocket.CloseProtocolError},
		errors.New(closed.Error()),
	} {
		reads, attempts := 0, 0
		err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) {
			reads++
			if reads == 2 {
				return 0, cause
			}
			if reads == 4 {
				return 22, nil
			}
			return 21, nil
		}, func() error {
			attempts++
			return classifyProvisionalNativeWeights(t.Context(), true, 21, 31, errNoPositiveUnmaskedWeights)
		}, func() bool { return reads < 5 }, false, true)
		if !errors.Is(err, cause) || !strings.Contains(err.Error(), "incomplete epoch 21 to 22") || attempts != 2 {
			t.Fatalf("weight rejection hid an independent scheduler failure: attempts=%d cause=%v err=%v", attempts, cause, err)
		}
	}
}

func TestReleaseSchedulerWebsocketCloseClassificationIsTyped(t *testing.T) {
	for _, code := range []int{websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseInternalServerErr, websocket.CloseServiceRestart, websocket.CloseTryAgainLater} {
		cause := fmt.Errorf("finalized scheduler read: %w", &websocket.CloseError{Code: code})
		if !RetryableEvidenceTransportError(cause) || RetryableEvidenceTransportError(errors.Join(cause, errors.New("independent close failed"))) {
			t.Fatalf("close %d lost its typed-only transport scope", code)
		}
	}
	for _, code := range []int{0, websocket.CloseProtocolError, websocket.CloseUnsupportedData, websocket.CloseInvalidFramePayloadData, websocket.ClosePolicyViolation, websocket.CloseMessageTooBig, websocket.CloseMandatoryExtension, websocket.CloseTLSHandshake} {
		if RetryableEvidenceTransportError(&websocket.CloseError{Code: code}) {
			t.Fatalf("permanent/unclassified close %d authorized a read retry", code)
		}
	}
}

// A one-recipient vector cannot meet the cap, but a later authenticated
// observation with two positive recipients can. Do not invent zero-weight
// recipients or declare an entire epoch deferred from the first observation.
func TestReleaseSchedulerInfeasibleVectorKeepsExplicitNoSubmitUntilFreshWeights(t *testing.T) {
	reads, observations, nativeSubmissions := 0, 0, 0
	err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) { reads++; return 21, nil }, func() error {
		observations++
		weights := []*big.Rat{big.NewRat(1, 1)}
		if observations == 3 {
			weights = append(weights, big.NewRat(1, 1))
		}
		capped, err := crv4.ApplyMaxWeightLimitRational(weights, 32768)
		if err != nil {
			if capped != nil {
				t.Fatal("infeasible input returned admissible weights")
			}
			return classifyProvisionalNativeWeights(t.Context(), true, 21, 31, err)
		}
		if len(capped) != 2 || capped[0].Sign() <= 0 || capped[1].Sign() <= 0 {
			t.Fatalf("fresh eligible vector differs: %v", capped)
		}
		nativeSubmissions++
		return nil
	}, func() bool { return reads < 4 }, false, true)
	if err != nil || observations != 3 || nativeSubmissions != 1 {
		t.Fatalf("no-submit disposition suppressed valid same-epoch recovery: observations=%d native=%d err=%v", observations, nativeSubmissions, err)
	}
}
