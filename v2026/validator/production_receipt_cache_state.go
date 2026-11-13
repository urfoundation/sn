//go:build linux || darwin

// Optional receipt persistence can fail without ending native reconciliation.
// The actual runtime retains only a fixed-size authenticated memory prefix and
// a closed degradation state; it never adopts a candidate file as authority.
package validator

import (
	"context"
	"sync"
)

type productionReceiptCacheStage uint8

const (
	productionReceiptCacheRead productionReceiptCacheStage = iota + 1
	productionReceiptCacheWrite
)

// Diagnostic consumers may only copy/enqueue this closed scalar observation.
// They must not block, read custody, parse raw errors or change retry decisions.
type productionReceiptCacheObservation struct {
	nativeEpoch uint64
	stage       productionReceiptCacheStage
}

type productionReceiptCacheDiagnosticKey struct{}

// The actual service lifecycle supplies its independently bounded exporter.
// Tests observe this seam without replacing read, write or admission outcomes.
func withProductionReceiptCacheDiagnostic(ctx context.Context, observe func(productionReceiptCacheObservation)) context.Context {
	return context.WithValue(ctx, productionReceiptCacheDiagnosticKey{}, observe)
}

// This state belongs to one runtime, not an executable or process singleton.
// Disabled disk caching still permits verified memory progress across polls;
// process restart independently rescans if no valid durable prefix survives.
type productionReceiptCacheState struct {
	stateLock  sync.Mutex
	disabled   productionReceiptCacheStage
	checkpoint *productionReceiptCheckpoint
}

// Copies scalar proof state under a short lock; callers perform all I/O later.
func (self *productionReceiptCacheState) snapshot(scope [32]byte) (*productionReceiptCheckpoint, bool) {
	if self == nil {
		return nil, false
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.checkpoint == nil || self.checkpoint.Scope != scope {
		return nil, self.disabled != 0
	}
	copy := *self.checkpoint
	copy.Signature = nil
	return &copy, self.disabled != 0
}

// Only the scanner owner's admitted prefix enters this memory accelerator.
func (self *productionReceiptCacheState) remember(value *productionReceiptCheckpoint) {
	if self == nil {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	copy := *value
	copy.Signature = nil
	self.checkpoint = &copy
}

// Repeated cache incidents cannot flood diagnostics or repeatedly reopen a
// damaged optional namespace. Underlying intent and chain failures are separate.
func (self *ReleaseSteerer) disableReceiptCache(ctx context.Context, epoch uint64, stage productionReceiptCacheStage) {
	if self.runtimeV2 != nil && self.runtimeV2.receiptCache != nil {
		state := self.runtimeV2.receiptCache
		state.stateLock.Lock()
		first := state.disabled == 0
		state.disabled = stage
		state.stateLock.Unlock()
		if !first {
			return
		}
	}
	if observe, ok := ctx.Value(productionReceiptCacheDiagnosticKey{}).(func(productionReceiptCacheObservation)); ok && observe != nil {
		observe(productionReceiptCacheObservation{nativeEpoch: epoch, stage: stage})
	}
}
