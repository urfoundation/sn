//go:build linux || darwin

// A lagging finalized endpoint cannot replace an original decision witness.
// Retry only unavailable coverage after checking fresh canonical block facts.
package validator

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

// This private leaf records physical absence or lower finalized coverage.
// It has no matching or unwrap authority and cannot conceal another cause.
type releaseDecisionFinalityUnavailableError struct {
	requiredBlock uint64
	requiredHash  [32]byte
	observedBlock uint64
	observedHash  [32]byte
	field         string
}

// Diagnostics retain the original witness and the actual observed lower head.
func (self *releaseDecisionFinalityUnavailableError) Error() string {
	return fmt.Sprintf("decision finalized coverage unavailable: %s; required %d (0x%x), observed %d (0x%x)", self.field, self.requiredBlock, self.requiredHash, self.observedBlock, self.observedHash)
}

// Lower coverage is provider availability, never a manufactured timeout.
func (*releaseDecisionFinalityUnavailableError) Timeout() bool { return false }

// The existing strict retry owner still inspects every joined hard sibling.
func (*releaseDecisionFinalityUnavailableError) Temporary() bool { return true }

// Read only header evidence on retry, with at most three distinct numbered
// anchors: the current head, original witness and independently pinned decision.
// A completed contradiction is returned before any later unavailable read.
// An incomplete retry pass cannot replace the original completed witness.
func (self *ChainClient) readReleaseDecisionFinalityV2Context(ctx context.Context, minimum uint64, minimumHash [32]byte, block uint64, blockHash [32]byte) (uint64, [32]byte, error) {
	ctx, cancel := self.chainReadOperationContext(ctx)
	defer cancel()
	// Each physical header gets its own attempt slice of this original owner.
	// Several healthy slow replies must not share a single request deadline.
	readAttempt := func() (uint64, [32]byte, error) {
		// Decode present headers before the retry owner checks late cancellation.
		// A literal null is availability; malformed or incomplete data is hard.
		read := func(selector string) (uint64, [32]byte, bool, error) {
			if err := ctx.Err(); err != nil {
				return 0, [32]byte{}, false, err
			}
			callCtx, cancel := self.chainReadAttemptContext(ctx)
			defer cancel()
			var header *chainRPCBlock
			if err := self.client.Client().CallContext(callCtx, &header, "eth_getBlockByNumber", selector, false); err != nil {
				// Local and outer retry owners inspect one captured cause graph.
				return 0, [32]byte{}, false, errors.Join(observeReleaseError(err), callCtx.Err())
			}
			if header == nil {
				return 0, [32]byte{}, true, callCtx.Err()
			}
			number, hash, err := header.identity()
			if err != nil {
				return 0, [32]byte{}, false, errors.Join(errors.New("decision finality header is present but invalid"), err, callCtx.Err())
			}
			if err := self.rememberBlockIdentity(number, hash); err != nil {
				return 0, [32]byte{}, false, errors.Join(err, callCtx.Err())
			}
			return number, hash, false, callCtx.Err()
		}
		number, hash, missing, failure := read("finalized")
		if hash != ([32]byte{}) && (number == minimum && hash != minimumHash || number == block && hash != blockHash || hash == minimumHash && number != minimum || hash == blockHash && number != block) {
			return 0, [32]byte{}, errors.Join(failure, errors.New("decision finalized head conflicts with its original witness"))
		}
		if failure != nil && !RetryableEvidenceTransportError(failure) {
			return 0, [32]byte{}, failure
		}
		anchors := [3]struct {
			number uint64
			hash   [32]byte
		}{{number: number, hash: hash}, {number: minimum, hash: minimumHash}, {number: block, hash: blockHash}}
		for index, anchor := range anchors {
			if anchor.hash == ([32]byte{}) {
				continue
			}
			repeated := false
			for _, previous := range anchors[:index] {
				if previous.number == anchor.number {
					if previous.hash != anchor.hash {
						return 0, [32]byte{}, errors.Join(failure, errors.New("decision canonical anchors conflict at the same height"))
					}
					repeated = true
				}
			}
			if repeated {
				continue
			}
			actual, actualHash, absent, err := read(hexutil.EncodeUint64(anchor.number))
			if actualHash != ([32]byte{}) && (actual != anchor.number || actualHash != anchor.hash) {
				return 0, [32]byte{}, errors.Join(failure, err, fmt.Errorf("decision canonical finality witness changed at block %d", anchor.number))
			}
			if err != nil {
				failure = errors.Join(failure, err)
				if !RetryableEvidenceTransportError(err) {
					return 0, [32]byte{}, failure
				}
			}
			if absent {
				failure = errors.Join(failure, &releaseDecisionFinalityUnavailableError{requiredBlock: anchor.number, requiredHash: anchor.hash, observedBlock: number, observedHash: hash, field: "canonical block header"})
			}
		}
		if missing || failure == nil && number < minimum {
			failure = errors.Join(failure, &releaseDecisionFinalityUnavailableError{requiredBlock: minimum, requiredHash: minimumHash, observedBlock: number, observedHash: hash, field: "finalized head through original witness"})
		}
		if failure != nil {
			return 0, [32]byte{}, failure
		}
		return number, hash, nil
	}
	var lastErr error
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return 0, [32]byte{}, errors.Join(lastErr, err)
		}
		finalized, finalizedHash, err := readAttempt()
		err = errors.Join(err, ctx.Err())
		if err == nil {
			return finalized, finalizedHash, nil
		}
		lastErr = err
		if !RetryableEvidenceTransportError(lastErr) {
			return 0, [32]byte{}, lastErr
		}
		if err := self.waitChainReadRetry(ctx, attempt); err != nil {
			return 0, [32]byte{}, errors.Join(lastErr, err)
		}
	}
}
