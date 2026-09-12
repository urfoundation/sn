package validator

// Explicit bounded ownership is constructor-selected, never inferred from a
// filename, epoch or candidate transcript. Legacy-created stores remain legacy.

import (
	"context"
	"errors"
	"math/big"
	"os"
	"sync/atomic"

	"github.com/urfoundation/sn/protocol"
)

// Concurrent or callback-reentrant operations never wait on their own owner.
var errHeadEMAStoreV2Busy = errors.New("bounded head EMA operation is already owned")

// The startup reader supplies these exact observations before its handles
// close; later operations may check them but cannot replace their authority.
type headEMAStoreV2Namespace struct {
	directory attemptPrivateFileState
	leaf      attemptPrivateFileState
	digest    [32]byte
	missing   bool
}

// Limits and the path in the parent store are immutable. The atomic bit grants
// exclusive access to witness/fault; no callback executes under the state mutex.
// A fault is sticky until a separately admitted reload. An unresolved durable
// marker also blocks that reload; deleting it blindly is not a recovery method.
type headEMAStoreV2Owner struct {
	limits    HeadEMAStoreV2Limits
	namespace headEMAStoreV2Namespace
	active    atomic.Bool
	fault     error
	// Immutable after authenticated provisional runtime construction. Missing
	// native attempts have no EMA observation; fold the next actual input once.
	provisionalEpochGaps bool
	historyAdoption *releaseHistoryAdoptionV2
}

func (self *HeadEMAStore) allowsHeadEMAEpochGaps() bool {
	return self != nil && self.v2 != nil && self.v2.provisionalEpochGaps
}

// Only the first approved fresh epoch can bridge the adopted terminal state.
// Advancing the store consumes this edge naturally; later skips stay strict.
func (self *HeadEMAStore) allowsHeadEMAEpochGapTo(epoch uint64) bool {
	if self.allowsHeadEMAEpochGaps() {
		return true
	}
	if self == nil || self.v2 == nil || self.v2.historyAdoption == nil || self.lastSubnetEpoch == nil {
		return false
	}
	request := self.v2.historyAdoption.request
	return *self.lastSubnetEpoch == request.LastNativeEpoch && epoch == request.FirstNativeEpoch
}

// Hooks observe actual boundaries; none can replace bytes, syscalls or math.
// Callers must not concurrently mutate borrowed arguments; hooks see only
// already-owned copies. File-close hooks run after the real Close.
type headEMAStoreV2RuntimeHooks struct {
	step       func(string, *os.File) error
	afterClose func(*os.File) error
}

// The operation gate, not an EMA state mutex, remains owned across observers.
func (self headEMAStoreV2RuntimeHooks) observe(ctx context.Context, step string, file *os.File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if self.step != nil {
		if err := self.step(step, file); err != nil {
			return errors.Join(err, ctx.Err())
		}
	}
	return ctx.Err()
}

// A separate routing predicate makes the legacy fallthrough auditable.
func useHeadEMAStoreV2Runtime(store *HeadEMAStore) bool { return store != nil && store.v2 != nil }

// Public operation kinds preserve each existing method's epoch semantics.
type headEMAStoreV2Operation uint8

const (
	headEMAStoreV2Preview headEMAStoreV2Operation = iota
	headEMAStoreV2Commit
	headEMAStoreV2FoldEpoch
	headEMAStoreV2Fold
)

// Context-aware preview is the live v2 API. Background dispatch from the old
// signature is compatibility only, not permission to drop a live caller's ctx.
func (self *HeadEMAStore) PreviewForEpochV2(ctx context.Context, epoch uint64, raw map[FleetScoreKey]*big.Rat, alpha protocol.Rational) (map[uint16]*big.Rat, []HeadEMAMeasurement, error) {
	return self.runHeadEMAStoreV2(ctx, headEMAStoreV2Preview, epoch, raw, nil, alpha, headEMAStoreV2RuntimeHooks{})
}

// A genuine independently reconstructed transcript is durably committed only
// after all bounded custody witnesses succeed. Same-epoch retry is exact.
func (self *HeadEMAStore) CommitForEpochV2(ctx context.Context, epoch uint64, records []HeadEMAMeasurement, alpha protocol.Rational) error {
	_, _, err := self.runHeadEMAStoreV2(ctx, headEMAStoreV2Commit, epoch, nil, records, alpha, headEMAStoreV2RuntimeHooks{})
	return err
}

// Like the original FoldForEpoch, this API permits a forward epoch jump.
// Preview/Commit require the immediate successor unless the authenticated
// provisional runtime selected the same gap semantics; retries never refold.
func (self *HeadEMAStore) FoldForEpochV2(ctx context.Context, epoch uint64, raw map[FleetScoreKey]*big.Rat, alpha protocol.Rational) (map[uint16]*big.Rat, []HeadEMAMeasurement, error) {
	return self.runHeadEMAStoreV2(ctx, headEMAStoreV2FoldEpoch, epoch, raw, nil, alpha, headEMAStoreV2RuntimeHooks{})
}

// The unepoched compatibility operation deliberately clears fold metadata,
// retaining the original arithmetic and persisted schema semantics.
func (self *HeadEMAStore) FoldV2(ctx context.Context, raw map[FleetScoreKey]*big.Rat, alpha protocol.Rational) (map[uint16]*big.Rat, error) {
	out, _, err := self.runHeadEMAStoreV2(ctx, headEMAStoreV2Fold, 0, raw, nil, alpha, headEMAStoreV2RuntimeHooks{})
	return out, err
}

// Only constructor-owned stores may enter. All proportional copies and math
// are planned before callbacks; speculation mutates a private scratch store.
func (self *HeadEMAStore) runHeadEMAStoreV2(ctx context.Context, operation headEMAStoreV2Operation, epoch uint64, raw map[FleetScoreKey]*big.Rat, records []HeadEMAMeasurement, alpha protocol.Rational, hooks headEMAStoreV2RuntimeHooks) (out map[uint16]*big.Rat, transcript []HeadEMAMeasurement, resultErr error) {
	if ctx == nil {
		return nil, nil, errors.New("bounded head EMA context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if self == nil || self.v2 == nil {
		return nil, nil, errors.New("head EMA store was not created by the bounded constructor")
	}
	owner := self.v2
	if !owner.active.CompareAndSwap(false, true) {
		return nil, nil, errors.Join(errHeadEMAStoreV2Busy, ctx.Err())
	}
	durablyPublished := false
	defer owner.active.Store(false)
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil && durablyPublished {
			owner.fault = errors.Join(owner.fault, resultErr)
		}
		if resultErr != nil {
			out, transcript = nil, nil
		}
	}()
	if owner.fault != nil {
		return nil, nil, errors.Join(errors.New("bounded head EMA owner requires unresolved-write investigation or fresh admission"), owner.fault)
	}
	var scratch *HeadEMAStore
	var ownedRaw map[FleetScoreKey]*big.Rat
	var ownedRecords []HeadEMAMeasurement
	var budget headEMAStoreV2Budget
	err := func() error {
		self.mu.Lock()
		defer self.mu.Unlock()
		var err error
		budget, err = self.admitHeadEMAStoreV2WithLock(ctx, operation, epoch, raw, records, alpha, owner.limits)
		if err != nil {
			return err
		}
		scratch = &HeadEMAStore{path: self.path, values: cloneHeadEMAEntries(self.values)}
		if self.lastSubnetEpoch != nil {
			value := *self.lastSubnetEpoch
			scratch.lastSubnetEpoch = &value
		}
		if self.lastAlpha != nil {
			value := *self.lastAlpha
			scratch.lastAlpha = &value
		}
		scratch.lastFold = append([]HeadEMAMeasurement(nil), self.lastFold...)
		if operation == headEMAStoreV2Commit {
			ownedRecords = append([]HeadEMAMeasurement(nil), records...)
		} else {
			ownedRaw = make(map[FleetScoreKey]*big.Rat, len(raw))
			for key, value := range raw {
				if err := ctx.Err(); err != nil {
					return err
				}
				ownedRaw[key] = new(big.Rat).Set(value)
			}
		}
		return nil
	}()
	if err != nil {
		return nil, nil, err
	}
	if err := hooks.observe(ctx, "operands-owned", nil); err != nil {
		return nil, nil, err
	}
	if operation == headEMAStoreV2Commit && scratch.lastSubnetEpoch != nil && epoch == *scratch.lastSubnetEpoch {
		if !equalHeadEMAFolds(scratch.lastFold, ownedRecords) {
			return nil, nil, errors.New("committed same-epoch head EMA transcript changed")
		}
		err := witnessHeadEMAStoreV2Runtime(ctx, self.path, owner.namespace, hooks)
		return nil, nil, err
	}
	if operation == headEMAStoreV2Commit {
		ownedRaw, err = rawHeadEMAInputs(ownedRecords)
		if err != nil {
			return nil, nil, err
		}
	}
	if operation == headEMAStoreV2Preview || operation == headEMAStoreV2FoldEpoch && scratch.lastSubnetEpoch != nil && epoch == *scratch.lastSubnetEpoch {
		scratch.mu.Lock()
		out, transcript, err = scratch.previewForEpochWithGapPolicy(epoch, ownedRaw, alpha, owner.provisionalEpochGaps)
		scratch.mu.Unlock()
	} else {
		out, transcript, err = func() (map[uint16]*big.Rat, []HeadEMAMeasurement, error) {
			scratch.mu.Lock()
			defer scratch.mu.Unlock()
			return scratch.foldWithLock(ownedRaw, alpha)
		}()
	}
	if err = errors.Join(err, ctx.Err()); err != nil {
		return nil, nil, err
	}
	if operation == headEMAStoreV2Commit && !equalHeadEMAFolds(transcript, ownedRecords) {
		return nil, nil, errors.New("head EMA transcript does not extend durable prior state")
	}
	sameEpoch := scratch.lastSubnetEpoch != nil && epoch == *scratch.lastSubnetEpoch
	write := operation != headEMAStoreV2Preview && !(operation == headEMAStoreV2FoldEpoch && sameEpoch)
	if write {
		if operation == headEMAStoreV2Fold {
			scratch.lastSubnetEpoch, scratch.lastAlpha, scratch.lastFold = nil, nil, nil
		} else {
			scratch.lastSubnetEpoch, scratch.lastAlpha = &epoch, &alpha
			scratch.lastFold = append([]HeadEMAMeasurement(nil), transcript...)
		}
	}
	if err := checkHeadEMAStoreV2Completed(ctx, scratch, out, transcript, budget); err != nil {
		return nil, nil, err
	}
	if err := hooks.observe(ctx, "fold-complete", nil); err != nil {
		return nil, nil, err
	}
	if !write {
		if err := witnessHeadEMAStoreV2Runtime(ctx, self.path, owner.namespace, hooks); err != nil {
			return nil, nil, err
		}
		return out, transcript, nil
	}
	encoded, err := encodeHeadEMAStoreV2File(ctx, scratch, &budget, owner.limits.MaxFileBytes)
	if err != nil {
		return nil, nil, err
	}
	next, uncertain, err := writeHeadEMAStoreV2(ctx, self.path, encoded, owner.namespace, owner.limits, hooks)
	if err != nil {
		if uncertain {
			owner.fault = err
		}
		return nil, nil, err
	}
	owner.namespace = next
	durablyPublished = true
	func() {
		self.mu.Lock()
		defer self.mu.Unlock()
		self.values, self.lastSubnetEpoch, self.lastAlpha, self.lastFold = scratch.values, scratch.lastSubnetEpoch, scratch.lastAlpha, scratch.lastFold
	}()
	if err := ctx.Err(); err != nil {
		owner.fault = err
		return nil, nil, err
	}
	return out, transcript, nil
}
