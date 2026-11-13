package crv4

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Retention is finite, per API identity and scoped to one deadline. Filling
// the owner cannot evict an earlier constraint to make room for another.
func TestFinalityReadOwnerBoundAndScope(t *testing.T) {
	fixture := newValidatorIdentityTestFixture(t)
	if got := WithFinalityReadOwnerContext(context.Background()); got.Value(finalityReadOwnerKey{}) != nil {
		t.Fatal("unbounded context retained finality")
	}
	parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	ctx := WithFinalityReadOwnerContext(parent)
	if WithFinalityReadOwnerContext(ctx) != ctx {
		t.Fatal("nested read replaced its owner")
	}
	first := types.Hash{1}
	for index := 1; index <= MaximumFinalityReadOwnerEntries; index++ {
		var block types.Hash
		binary.LittleEndian.PutUint32(block[:], uint32(index))
		if err := RetainFinalityReadWitnessContext(ctx, fixture.chain, block, fixture.finalized, 103); err != nil {
			t.Fatal(index, err)
		}
	}
	var extra types.Hash
	binary.LittleEndian.PutUint32(extra[:], MaximumFinalityReadOwnerEntries+1)
	if err := RetainFinalityReadWitnessContext(ctx, fixture.chain, extra, fixture.finalized, 103); err == nil || !strings.Contains(err.Error(), "bound") {
		t.Fatalf("owner silently exceeded its bound: %v", err)
	}
	if err := RetainFinalityReadWitnessContext(ctx, fixture.chain, first, fixture.finalized, 103); err != nil {
		t.Fatalf("full owner evicted original entry: %v", err)
	}
	fresh := WithFinalityReadOwnerContext(parent)
	if err := RetainFinalityReadWitnessContext(fresh, fixture.chain, extra, fixture.finalized, 103); err != nil {
		t.Fatalf("ended scope contaminated another owner: %v", err)
	}
	other := *fixture.chain
	api := *fixture.chain.API
	other.API = &api
	if err := RetainFinalityReadWitnessContext(fresh, &other, extra, types.Hash{9}, 103); err != nil {
		t.Fatalf("different API identity inherited another route's witness: %v", err)
	}
	borrow := *fixture.chain
	if err := RetainFinalityReadWitnessContext(fresh, &borrow, extra, types.Hash{9}, 103); err == nil {
		t.Fatal("private view lost its original API constraint")
	}
}

type finalityReadGateContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (self *finalityReadGateContext) Done() <-chan struct{} {
	self.once.Do(func() { close(self.entered) })
	return self.Context.Done()
}

// No RPC lock surrounds the immutable contradiction check. A canceled peer
// can leave the same-key gate while a completed hard witness still dominates.
func TestFinalityReadOwnerCanceledGateAndCompletedContradiction(t *testing.T) {
	fixture := newValidatorIdentityTestFixture(t)
	parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	ctx := WithFinalityReadOwnerContext(parent)
	block := fixture.query.BlockHash
	if err := RetainFinalityReadWitnessContext(ctx, fixture.chain, block, fixture.finalized, 103); err != nil {
		t.Fatal(err)
	}
	_, release, err := acquireFinalityReadEntry(ctx, fixture.chain, block)
	if err != nil {
		t.Fatal(err)
	}
	child, stop := context.WithCancel(ctx)
	waiting := &finalityReadGateContext{Context: child, entered: make(chan struct{})}
	returned := make(chan error, 1)
	go func() {
		_, unlock, err := acquireFinalityReadEntry(waiting, fixture.chain, block)
		if unlock != nil {
			unlock()
		}
		returned <- err
	}()
	<-waiting.entered
	stop()
	if err := <-returned; !errors.Is(err, context.Canceled) {
		t.Fatalf("same-key gate ignored cancellation: %v", err)
	}
	err = CheckRetainedFinalityReadWitnessContext(waiting, fixture.chain, block, types.Hash{99}, 103)
	if err == nil || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "same height") {
		t.Fatalf("completed contradiction waited for gate or lost its hard cause: %v", err)
	}
	release()
	if err := RetainFinalityReadWitnessContext(ctx, fixture.chain, block, fixture.finalized, 103); err != nil {
		t.Fatalf("canceled gate waiter consumed admission: %v", err)
	}
}
