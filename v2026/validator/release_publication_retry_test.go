//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"math/big"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Reproduce the real publication owner's cancel-and-join transcript, then let
// the periodic refresh republish the identical object through both HTTP reads.
func TestReleaseSettlementRefreshRetriesJoinedReplicaTimeout(t *testing.T) {
	t.Parallel()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	siblingEntered := make(chan struct{})
	var firstCalls, secondCalls atomic.Int32
	stores[0].beforePut = func(context.Context, string, string, []byte) error {
		if firstCalls.Add(1) == 1 {
			<-siblingEntered
			return context.DeadlineExceeded
		}
		return nil
	}
	stores[1].beforePut = func(ctx context.Context, _ string, _ string, _ []byte) error {
		if secondCalls.Add(1) == 1 {
			close(siblingEntered)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("immutable publication retry\n")
	hash := attemptHex32(sha256.Sum256(raw))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	advances, published := 0, 0
	var firstFailure error
	err = runReleaseSettlementRefresh(ctx, time.Second, func(context.Context) (*ReleaseSnapshot, error) {
		return &ReleaseSnapshot{Epoch: big.NewInt(43)}, nil
	}, func(ctx context.Context, _ *ReleaseSnapshot) error {
		advances++
		result := publisher.writer(AttemptStreamV2Records)(ctx, hash, raw)
		if advances == 1 {
			firstFailure = result
		}
		return result
	}, func(*ReleaseSnapshot) {
		published++
		cancel()
	}, func(context.Context, time.Duration) error { return nil })
	if !errors.Is(err, context.Canceled) || advances != 2 || published != 1 || !errors.Is(firstFailure, context.DeadlineExceeded) || !errors.Is(firstFailure, context.Canceled) {
		t.Fatalf("joined replica timeout prevented refresh recovery: advances=%d published=%d error=%v first=%v", advances, published, err, firstFailure)
	}
	for index, store := range stores {
		objects, writes, reads := store.snapshot()
		if writes != 1 || reads != 1 || len(objects) != 1 || !bytes.Equal(objects[AttemptStreamV2Records+"/"+hash], raw) {
			t.Fatalf("replica %d retry changed bytes or omitted readback: writes=%d reads=%d objects=%v", index, writes, reads, objects)
		}
	}
}

func TestReleasePublicationRetriesOnlyOwnedTransientCauses(t *testing.T) {
	transient := &attemptReplicaPublicationError{causes: []error{context.DeadlineExceeded, context.Canceled}}
	hard := errors.New("immutable public body hash differs")
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"owned timeout and sibling cancellation", transient, true},
		{"parent cancellation", errors.Join(transient, context.Canceled), false},
		{"unowned cancellation", errors.Join(context.DeadlineExceeded, context.Canceled), false},
		{"pure sibling cancellation", &attemptReplicaPublicationError{causes: []error{context.Canceled}}, false},
		{"hard sibling", &attemptReplicaPublicationError{causes: []error{context.DeadlineExceeded, errors.Join(hard, context.Canceled)}}, false},
		{"unowned hard join", errors.Join(context.DeadlineExceeded, hard), false},
		{"late close failure", &attemptReplicaPublicationError{causes: []error{context.DeadlineExceeded, errors.New("late readback close failed")}}, false},
	}
	for _, test := range cases {
		if got := transientReleaseSnapshotError(test.err); got != test.want {
			t.Errorf("%s retry=%t want=%t: %v", test.name, got, test.want, test.err)
		}
	}
}

func TestReleasePublicationInterruptedReadbackRetainsItsCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(context.DeadlineExceeded)
	body := &attemptStreamV2HTTPBody{ctx: ctx, cancel: cancel, body: io.NopCloser(strings.NewReader("incomplete"))}
	_, readErr := body.Read(make([]byte, 4))
	closeErr := body.Close()
	publication := &attemptReplicaPublicationError{causes: []error{errors.Join(readErr, closeErr), context.Canceled}}
	if !errors.Is(closeErr, context.DeadlineExceeded) || !transientReleaseSnapshotError(publication) {
		t.Fatalf("canceled public readback hid its recoverable cause: read=%v close=%v", readErr, closeErr)
	}
	ctx, cancel = context.WithCancelCause(t.Context())
	defer cancel(nil)
	body = &attemptStreamV2HTTPBody{ctx: ctx, cancel: cancel, body: io.NopCloser(strings.NewReader("incomplete"))}
	publication = &attemptReplicaPublicationError{causes: []error{body.Close(), context.DeadlineExceeded}}
	if transientReleaseSnapshotError(publication) {
		t.Fatal("genuine early readback close became retryable beside an unrelated timeout")
	}
}
