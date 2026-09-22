// A pipelined renewal can observe the pool before its preceding exact nonce
// arrives there. Retry that read without changing any approved nonce or intent.
package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
)

// Restrict preflight recovery to observation, without a signing capability.
type fleetRenewalPendingNonceReader interface {
	PendingNonceAt(context.Context, common.Address) (uint64, error)
}

// A lower pool nonce may reflect propagation of the prior ordered submission.
// A higher nonce never receives this recoverable classification.
type fleetRenewalPendingNonceLagError struct{ observed, expected uint64 }

// Report both checkpoints while preserving the exact approved reservation.
func (self *fleetRenewalPendingNonceLagError) Error() string {
	return fmt.Sprintf("renewal pending nonce %d has not reached approved nonce %d", self.observed, self.expected)
}

// Polling is bounded by the existing read owner, never by a fresh signing loop.
func (self *fleetRenewalPendingNonceLagError) Temporary() bool { return true }

// Pool lag is not itself a transport timeout.
func (self *fleetRenewalPendingNonceLagError) Timeout() bool { return false }

// A stale smaller observation may converge. A larger observation is returned
// unchanged so the existing approval check rejects it before any signature.
func readFleetRenewalPendingNonce(ctx context.Context, reader fleetRenewalPendingNonceReader, from common.Address, action Action, policy finalSemanticRPCRetryPolicy) (uint64, error) {
	expected, err := strconv.ParseUint(action.Parameters["renewal_expected_nonce"], 10, 64)
	if err != nil || reader == nil || !isFleetRenewalAction(action) || from == (common.Address{}) {
		return 0, errors.New("renewal pending nonce authority is incomplete")
	}
	var nonce uint64
	err = retryEvmReadRpcCall(ctx, "fleet renewal pending nonce", policy, func(attemptCtx context.Context) error {
		observed, err := reader.PendingNonceAt(attemptCtx, from)
		if err != nil {
			return err
		}
		if observed < expected {
			return &fleetRenewalPendingNonceLagError{observed: observed, expected: expected}
		}
		nonce = observed
		return nil
	})
	if err != nil {
		return 0, err
	}
	return nonce, nil
}
