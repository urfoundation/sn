//go:build linux || darwin

package validator

// Compact collection borrows the constructor's durable EMA owner for its
// entire callback tree. No state mutex spans chain, key or proof callbacks.

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"reflect"

	"github.com/urfoundation/sn/v2026/protocol"
)

// This token is private to one synchronous operation. The owner is retained
// even if an external callback replaces the steerer field that selected it.
type releaseHeadEMAOwnerV2 struct {
	store *HeadEMAStore
	owner *headEMAStoreV2Owner
}

// Both trusted limits constrain this operation; neither is inferred from
// candidate data or expanded to a default. Validate before taking the minimum.
func (self *HeadEMAStore) releaseHeadV2Limits(maxEntries uint64, budget releaseHeadV2Budget) (uint64, releaseHeadV2Budget, error) {
	if self == nil || self.v2 == nil {
		return 0, budget, errors.New("compact live head requires a store admitted by NewHeadEMAStoreV2")
	}
	if maxEntries == 0 || budget.limit == 0 || budget.limit > maxReleaseMeasurementArtifactBytes || budget.used > budget.limit {
		return 0, budget, errors.New("compact head EMA owner or bounds are invalid")
	}
	if err := self.v2.limits.validate(); err != nil {
		return 0, budget, err
	}
	budget.limit = min(budget.limit, self.v2.limits.MaxControlBytes)
	if budget.used > budget.limit {
		return 0, budget, errors.New("compact head collection exceeds the retained EMA control allowance")
	}
	return min(maxEntries, self.v2.limits.MaxEntries), budget, nil
}

// Acquisition is nonblocking, does no filesystem I/O and rejects callback
// reentry/concurrent commits under the same atomic owner used by public APIs.
func (self *HeadEMAStore) ownReleaseHeadEMAV2(ctx context.Context, maxEntries uint64, budget releaseHeadV2Budget) (*releaseHeadEMAOwnerV2, uint64, releaseHeadV2Budget, error) {
	if ctx == nil {
		return nil, 0, budget, errors.New("compact head EMA context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, budget, err
	}
	maxEntries, budget, err := self.releaseHeadV2Limits(maxEntries, budget)
	if err != nil {
		return nil, 0, budget, err
	}
	if uint64(reflect.TypeFor[releaseHeadEMAOwnerV2]().Size()) > budget.limit-budget.used {
		return nil, 0, budget, errors.New("compact head EMA operation owner exceeds its control bound")
	}
	owner := self.v2
	if !owner.active.CompareAndSwap(false, true) {
		return nil, 0, budget, errors.Join(errHeadEMAStoreV2Busy, ctx.Err())
	}
	if owner.fault != nil {
		fault := owner.fault
		owner.active.Store(false)
		return nil, 0, budget, errors.Join(errors.New("bounded head EMA owner requires unresolved-write investigation or fresh admission"), fault, ctx.Err())
	}
	return &releaseHeadEMAOwnerV2{store: self, owner: owner}, maxEntries, budget, nil
}

// The current namespace is checked only after complete admission, and again
// before successful publication. A refusal still releases the atomic owner.
func (self *releaseHeadEMAOwnerV2) witness(ctx context.Context) error {
	return witnessHeadEMAStoreV2Runtime(ctx, self.store.path, self.owner.namespace, headEMAStoreV2RuntimeHooks{})
}

// Release never publishes a failed preview or repairs/re-blesses a path.
// Existing failure causes survive alongside final cancellation/custody errors.
func (self *releaseHeadEMAOwnerV2) finish(ctx context.Context, resultErr error) error {
	defer self.owner.active.Store(false)
	if resultErr == nil {
		resultErr = self.witness(ctx)
	}
	return errors.Join(resultErr, ctx.Err())
}

// Logical owner, exact preview scratch, descriptor states and physical path
// work coexist with the caller's draft/native/binding payload. Retained rows,
// current operands and generated transcript are charged by the same ledger.
func (self *HeadEMAStore) reserveReleaseHeadV2OwnerWithLock(budget releaseHeadV2Budget) (releaseHeadV2Budget, error) {
	fixed := headEMAStoreV2FixedControlBytes() +
		uint64(reflect.TypeFor[HeadEMAStore]().Size()) +
		uint64(reflect.TypeFor[releaseHeadEMAOwnerV2]().Size()) +
		uint64(reflect.TypeFor[releaseHeadV2Budget]().Size()) +
		2*(uint64(reflect.TypeFor[attemptPrivateDirectory]().Size())+uint64(reflect.TypeFor[os.File]().Size())+uint64(reflect.TypeFor[attemptPrivateFileState]().Size()))
	if self.lastSubnetEpoch != nil {
		fixed += uint64(reflect.TypeFor[uint64]().Size())
	}
	if self.lastAlpha != nil {
		fixed += uint64(reflect.TypeFor[protocol.Rational]().Size())
	}
	if err := budget.charge(1, fixed); err != nil {
		return budget, err
	}
	// Native component traversal owns string headers as well as path bytes.
	if err := budget.charge(uint64(len(self.path)), 12+3*uint64(reflect.TypeFor[string]().Size())); err != nil {
		return budget, err
	}
	return budget, nil
}

// Common wire ownership also copies operator/key maps and retains independent
// context/scratch strings. Their owners join the head ledger before that copy;
// a large trusted option is not permission to reset the EMA control allowance.
func reserveReleaseHeadV2OperatorControls(ctx context.Context, options ReleaseMeasurementV2Options, budget releaseHeadV2Budget) (releaseHeadV2Budget, error) {
	if err := ctx.Err(); err != nil {
		return budget, err
	}
	if options.Settlement != nil || len(options.Bindings) != 0 || len(options.Pools) != 0 || len(options.DepositAudits) != 0 {
		return budget, errors.New("compact live head has preselected observation owners")
	}
	if err := budget.charge(1, uint64(reflect.TypeFor[ReleaseMeasurementV2Options]().Size())); err != nil {
		return budget, err
	}
	if err := budget.charge(uint64(len(options.ControlledNOIDs)), 8); err != nil {
		return budget, err
	}
	if err := budget.charge(uint64(len(options.Operators)), 8+uint64(reflect.TypeFor[ReleaseMeasurementV2OperatorOptions]().Size())+uint64(reflect.TypeFor[string]().Size())+1); err != nil {
		return budget, err
	}
	remaining := budget.limit - budget.used
	if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(options.Expected), &remaining); err != nil {
		return budget, err
	}
	if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(options.Policy), &remaining); err != nil {
		return budget, err
	}
	budget.used = budget.limit - remaining
	for _, operator := range options.Operators {
		if err := ctx.Err(); err != nil {
			return budget, err
		}
		if len(operator.Measurement.CurrentBindingKVs) != 0 {
			return budget, errors.New("compact live head has a preselected current-binding owner")
		}
		if err := budget.charge(uint64(len(operator.CutNativeBlockHash)), 1); err != nil {
			return budget, err
		}
		if err := budget.charge(uint64(len(operator.Measurement.Replay.ScratchDirectory)), 2); err != nil {
			return budget, err
		}
		remaining := budget.limit - budget.used
		if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(operator.Expected), &remaining); err != nil {
			return budget, err
		}
		budget.used = budget.limit - remaining
		keys := operator.Measurement.Replay.ServerKeys
		for _, key := range keys {
			if err := ctx.Err(); err != nil {
				return budget, err
			}
			if len(key) != ed25519.PublicKeySize {
				return budget, errors.New("compact operator server key has invalid width")
			}
		}
		if err := budget.charge(uint64(len(keys)), 1+uint64(reflect.TypeFor[ed25519.PublicKey]().Size())+ed25519.PublicKeySize); err != nil {
			return budget, err
		}
	}
	return budget, ctx.Err()
}
