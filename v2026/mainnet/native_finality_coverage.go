// Retained native history is not contradicted by a lagging finalized-head
// reply. Coverage waits own only repeatable reads inside the original budget.
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Rechecks exact retained canonical hashes before classifying a lower head as
// unavailable. Neither a retry nor a head advance replaces the retained points.
func (self *rpcClient) readNativeFinalityCovering(ctx context.Context, retained ...nativeFinalityPoint) (point nativeFinalityPoint, resultErr error) {
	if self == nil || self.retryWindow <= 0 || ctx == nil || len(retained) == 0 || len(retained) > 4 {
		return point, errors.New("native finality coverage requires one to four retained points and a context")
	}
	points := make([]nativeFinalityPoint, 0, len(retained))
	var required uint64
	for _, retainedPoint := range retained {
		if !rootCanonicalHash(retainedPoint.Hash) {
			return point, fmt.Errorf("%w: retained native finality hash is invalid", errRpcIntegrity)
		}
		duplicate := false
		for _, prior := range points {
			if prior.Number == retainedPoint.Number {
				if !strings.EqualFold(prior.Hash, retainedPoint.Hash) {
					return point, fmt.Errorf("%w: retained native hashes disagree at block %d", errRpcIntegrity, prior.Number)
				}
				duplicate = true
				break
			}
		}
		if !duplicate {
			points = append(points, retainedPoint)
		}
		required = max(required, retainedPoint.Number)
	}
	readCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, readCtx.Err(), context.Cause(readCtx))
		if resultErr != nil {
			point = nativeFinalityPoint{}
		}
	}()
	wait := self.retryWait
	if wait == nil {
		wait = waitRpcReadRetry
	}
	var unavailable error
	for attempt := 0; ; attempt++ {
		if err := readCtx.Err(); err != nil {
			return nativeFinalityPoint{}, errors.Join(unavailable, err)
		}
		current, err := self.readNativeFinality(readCtx)
		if err != nil {
			return nativeFinalityPoint{}, errors.Join(unavailable, err)
		}
		for _, retainedPoint := range points {
			var hash string
			// Null is unavailable for this exact retained fact, never changed
			// history. The current-head reader keeps its stricter grammar.
			if err := self.callAdmittedReadResult(readCtx, "chain_getBlockHash", []any{retainedPoint.Number}, &hash, false, true, maxRpcReplyBytes); err != nil {
				return nativeFinalityPoint{}, errors.Join(unavailable, err)
			}
			if !rootCanonicalHash(strings.ToLower(hash)) || !strings.EqualFold(hash, retainedPoint.Hash) {
				return nativeFinalityPoint{}, fmt.Errorf("%w: retained native canonical hash changed at block %d", errRpcIntegrity, retainedPoint.Number)
			}
		}
		if current.Number >= required {
			return current, nil
		}
		unavailable = fmt.Errorf("%w: native finalized head %d does not yet cover retained block %d", errRpcObservationUnavailable, current.Number, required)
		if err := wait(readCtx, min(time.Duration(attempt+1)*500*time.Millisecond, 5*time.Second)); err != nil || readCtx.Err() != nil {
			return nativeFinalityPoint{}, errors.Join(unavailable, err, readCtx.Err())
		}
	}
}
