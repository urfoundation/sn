// Work-count contexts cancel the actual owner without timing or scheduler assumptions.
package merkle

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
)

// The inherited Done/Err belongs to a real cancellation, triggered at a fixed work boundary.
type treeWorkContext struct {
	context.Context
	cancel context.CancelFunc
	calls  atomic.Int32
	stop   int32
}

// Cancellation never depends on elapsed time or goroutine scheduling.
func (self *treeWorkContext) Err() error {
	if self.calls.Add(1) == self.stop {
		self.cancel()
	}
	return self.Context.Err()
}

// Each test owns and releases the genuine operation context.
func treeCancelAt(t *testing.T, stop int32) *treeWorkContext {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	return &treeWorkContext{Context: ctx, cancel: cancel, stop: stop}
}

// A mid-input stop may not expose a prefix tree that looks like a complete root.
func TestMerkleOwnedInputCancellation(t *testing.T) {
	ctx := treeCancelAt(t, 3)
	tree, err := NewTreeWithContext(ctx, testLeaves("owned-context", 17))
	if tree != nil || !errors.Is(err, context.Canceled) || ctx.calls.Load() != 3 {
		t.Fatal("tree ignored owned input cancellation", tree, err, ctx.calls.Load())
	}
}

// Odd-level promotion and every proof stay byte-identical under the new owner API.
func TestMerkleOwnedCanonicalCompatibility(t *testing.T) {
	leaves := testLeaves("owned-context", 17)
	legacy, err := NewTree(leaves)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := NewTreeWithContext(t.Context(), leaves)
	if err != nil || owned.Root() != legacy.Root() {
		t.Fatal("owned tree changed root", err)
	}
	for _, leaf := range leaves {
		original, err := legacy.Proof(leaf)
		if err != nil {
			t.Fatal(err)
		}
		proof, err := owned.Proof(leaf)
		if err != nil || !reflect.DeepEqual(original, proof) || !Verify(owned.Root(), leaf, proof) {
			t.Fatal("owned tree changed proof", err)
		}
	}
}
