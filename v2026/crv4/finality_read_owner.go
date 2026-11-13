// Finality retention is private to a bounded read owner. Retained constraints
// never replace fresh canonical evidence or grant runtime/signing authority.
package crv4

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// A read cannot silently evict an earlier witness to admit more identities.
const MaximumFinalityReadOwnerEntries = 256

type finalityReadOwnerKey struct{}

type finalityReadKey struct {
	api   *gsrpc.SubstrateAPI
	block types.Hash
}

type finalityReadWitness struct {
	hash   types.Hash
	number uint64
}

type finalityReadEntry struct {
	gate      chan struct{}
	selected  types.Hash
	witnesses atomic.Pointer[finalityReadWitnesses]
}

type finalityReadWitnesses struct {
	first  finalityReadWitness
	latest finalityReadWitness
}

type finalityReadOwner struct {
	mu      sync.Mutex
	entries map[finalityReadKey]*finalityReadEntry
}

// The enclosing read creates and cancels its deadline. Nested readers borrow
// that owner's constraints; an unbounded context never installs retained state.
func WithFinalityReadOwnerContext(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	if _, found := ctx.Value(finalityReadOwnerKey{}).(*finalityReadOwner); found {
		return ctx
	}
	if _, finite := ctx.Deadline(); !finite {
		return ctx
	}
	return context.WithValue(ctx, finalityReadOwnerKey{}, &finalityReadOwner{entries: map[finalityReadKey]*finalityReadEntry{}})
}

// Map locking never surrounds an RPC. The per-identity gate is cancelable, so
// a slow peer read cannot extend another caller's earlier deadline.
func acquireFinalityReadEntry(ctx context.Context, chain *Chain, block types.Hash) (*finalityReadEntry, func(), error) {
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil {
		return nil, nil, errors.New("crv4: finality read owner identity is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	owner, _ := ctx.Value(finalityReadOwnerKey{}).(*finalityReadOwner)
	if owner == nil {
		return nil, func() {}, nil
	}
	key := finalityReadKey{api: chain.API, block: block}
	owner.mu.Lock()
	entry := owner.entries[key]
	if entry == nil {
		if len(owner.entries) >= MaximumFinalityReadOwnerEntries {
			owner.mu.Unlock()
			return nil, nil, errors.New("crv4: finality read owner exceeds its retained identity bound")
		}
		entry = &finalityReadEntry{gate: make(chan struct{}, 1)}
		owner.entries[key] = entry
	}
	owner.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case entry.gate <- struct{}{}:
		return entry, func() { <-entry.gate }, nil
	}
}

// Zero means the first finalized selection of this owner, including later
// calls to the public reader. Explicit historical selections never drift.
func SelectFinalityReadBlockContext(ctx context.Context, chain *Chain, requested, candidate types.Hash) (types.Hash, error) {
	if ctx == nil {
		return types.Hash{}, errors.New("crv4: finalized read selection context is absent")
	}
	if requested != (types.Hash{}) {
		return requested, ctx.Err()
	}
	if candidate == (types.Hash{}) {
		return types.Hash{}, errors.New("crv4: finalized read candidate is absent")
	}
	entry, release, err := acquireFinalityReadEntry(ctx, chain, types.Hash{})
	if err != nil {
		return types.Hash{}, err
	}
	defer release()
	if entry == nil {
		return candidate, ctx.Err()
	}
	if entry.selected == (types.Hash{}) {
		entry.selected = candidate
	}
	return entry.selected, ctx.Err()
}

// Callers first authenticate current's full header and canonical hash. This
// adds the original owner's first/highest constraints, rechecking their actual
// canonical hashes before treating a lower current head as unavailable.
func RetainFinalityReadWitnessContext(ctx context.Context, chain *Chain, block, current types.Hash, number uint64) error {
	if block == (types.Hash{}) || current == (types.Hash{}) || number > math.MaxUint32 {
		return errors.New("crv4: retained finality witness is incomplete")
	}
	if err := CheckRetainedFinalityReadWitnessContext(ctx, chain, block, current, number); err != nil {
		return err
	}
	entry, release, err := acquireFinalityReadEntry(ctx, chain, block)
	if err != nil {
		return err
	}
	defer release()
	if entry == nil {
		return ctx.Err()
	}
	previous := entry.witnesses.Load()
	if previous == nil {
		previous = &finalityReadWitnesses{}
	}
	if err := compareFinalityReadWitnesses(ctx, previous, current, number); err != nil {
		return err
	}
	for index, retained := range []finalityReadWitness{previous.first, previous.latest} {
		if retained.hash == (types.Hash{}) {
			continue
		}
		if index != 0 && retained.hash == previous.first.hash {
			continue
		}
		if retained.hash == current {
			continue
		}
		if err := checkFinalityReadCanonicalContext(ctx, chain, retained.hash, retained.number); err != nil {
			return errors.Join(err, ctx.Err())
		}
	}
	if number < previous.latest.number {
		return errors.Join(&ReceiptEvidenceUnavailableError{BlockHash: previous.latest.hash, Field: "finalized head through original read owner witness"}, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	witness := finalityReadWitness{hash: current, number: number}
	next := &finalityReadWitnesses{first: previous.first, latest: witness}
	if next.first.hash == (types.Hash{}) {
		next.first = witness
	}
	entry.witnesses.Store(next)
	return nil
}

// A completed current hash can already contradict an immutable retained tuple,
// even when cancellation prevents further RPCs or a gate acquisition. This
// check publishes no success and must be followed by the ordinary read checks.
func CheckRetainedFinalityReadWitnessContext(ctx context.Context, chain *Chain, block, current types.Hash, number uint64) error {
	if ctx == nil || chain == nil || chain.API == nil {
		return errors.New("crv4: completed finality comparison identity is incomplete")
	}
	owner, _ := ctx.Value(finalityReadOwnerKey{}).(*finalityReadOwner)
	if owner == nil {
		return nil
	}
	owner.mu.Lock()
	entry := owner.entries[finalityReadKey{api: chain.API, block: block}]
	owner.mu.Unlock()
	if entry == nil {
		return nil
	}
	return compareFinalityReadWitnesses(ctx, entry.witnesses.Load(), current, number)
}

func compareFinalityReadWitnesses(ctx context.Context, previous *finalityReadWitnesses, current types.Hash, number uint64) error {
	if previous == nil {
		return nil
	}
	for _, retained := range []finalityReadWitness{previous.first, previous.latest} {
		if retained.hash == (types.Hash{}) {
			continue
		}
		if retained.number == number && retained.hash != current {
			return errors.Join(errors.New("crv4: completed finalized witnesses disagree at the same height"), ctx.Err())
		}
		if retained.hash == current && retained.number != number {
			return errors.Join(errors.New("crv4: retained finalized hash changed its authenticated height"), ctx.Err())
		}
	}
	return nil
}

// Authenticate a complete current witness before consulting the owner's
// immutable constraints. A late cancellation cannot erase a completed fork.
func readFinalityReadWitnessContext(ctx context.Context, chain *Chain, block types.Hash) (types.Hash, uint64, error) {
	var encoded *string
	if err := chain.API.Client.CallContext(ctx, &encoded, "chain_getFinalizedHead"); err != nil {
		return types.Hash{}, 0, err
	}
	if encoded == nil || *encoded == "" {
		return types.Hash{}, 0, &ReceiptEvidenceUnavailableError{Field: "finalized head"}
	}
	hash, err := receiptHash(*encoded)
	if err != nil {
		return types.Hash{}, 0, errors.Join(err, ctx.Err())
	}
	_, number, err := chain.receiptHeaderAt(ctx, hash)
	if err != nil {
		return types.Hash{}, 0, err
	}
	if err := checkFinalityReadCanonicalContext(ctx, chain, hash, number); err != nil {
		return types.Hash{}, 0, err
	}
	if err := CheckRetainedFinalityReadWitnessContext(ctx, chain, block, hash, number); err != nil {
		return types.Hash{}, 0, err
	}
	return hash, number, nil
}

// Missing old-witness replies remain unavailable. A completed different hash
// remains hard even if the same RPC return boundary also cancels its context.
func checkFinalityReadCanonicalContext(ctx context.Context, chain *Chain, hash types.Hash, number uint64) error {
	var encoded *string
	if err := chain.API.Client.CallContext(ctx, &encoded, "chain_getBlockHash", number); err != nil {
		return err
	}
	if encoded == nil || *encoded == "" {
		return &ReceiptEvidenceUnavailableError{BlockHash: hash, Field: "retained canonical finality hash"}
	}
	canonical, err := receiptHash(*encoded)
	if err != nil {
		return errors.Join(err, ctx.Err())
	}
	if canonical != hash {
		return errors.Join(errors.New("crv4: original read owner finalized witness changed its canonical hash"), ctx.Err())
	}
	return nil
}
