package miner

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func claimAdmissionCandidates(epochs ...int64) []claimPollCandidate {
	candidates := []claimPollCandidate{}
	for _, epoch := range epochs {
		candidates = append(candidates, claimPollCandidate{entry: &ClaimQueueEntry{Epoch: epoch, Status: "pending"}})
	}
	return candidates
}

func claimTestAcquire(t *testing.T, admission *claimAdmission, owner string, want bool, epochs ...int64) int {
	t.Helper()
	index, acquired, err := admission.acquire(context.Background(), owner, claimAdmissionCandidates(epochs...))
	if err != nil || acquired != want {
		t.Fatalf("owner %s acquired=%t index=%d error=%v, want %t", owner, acquired, index, err, want)
	}
	return index
}

// Repeated polls retain one place per class. A new member cannot pass an
// already waiting current member, even after subscribing after release.
func TestClaimAdmissionRetainsCrossMemberFIFOAndWake(t *testing.T) {
	admission := &claimAdmission{}
	admission.observeEpoch(20)
	claimTestAcquire(t, admission, "owner", true, 20)
	for pass := 0; pass < 4; pass++ {
		claimTestAcquire(t, admission, "first", false, 20, 12)
		claimTestAcquire(t, admission, "second", false, 20, 12)
	}
	if len(admission.waiting) != 4 {
		t.Fatalf("repeated polls grew tickets: %d", len(admission.waiting))
	}
	admission.release("owner")
	select {
	case <-admission.ready("first"):
	default:
		t.Fatal("release before subscribe lost ready notification")
	}
	claimTestAcquire(t, admission, "late", false, 20)
	claimTestAcquire(t, admission, "first", true, 20, 12)
	claimTestAcquire(t, admission, "second", false, 20, 12)
	admission.release("first")
	claimTestAcquire(t, admission, "second", true, 20, 12)
	admission.release("second")
}

// Historical work retains a separate ticket across unlimited fresh arrivals;
// eight recent turns are the hard bound before the oldest historical turn.
func TestClaimAdmissionBoundsRecentBurstWithoutReplacingHistory(t *testing.T) {
	admission := &claimAdmission{}
	admission.observeEpoch(30)
	claimTestAcquire(t, admission, "initial", true, 30)
	claimTestAcquire(t, admission, "backlog", false, 12)
	admission.release("initial")
	for turn := 1; turn < claimRecentBurst; turn++ {
		name := fmt.Sprintf("new-%d", turn)
		claimTestAcquire(t, admission, name, true, 30)
		// Adding fresh work for the same backlogged member preserves its old lane.
		claimTestAcquire(t, admission, "backlog", false, 30, 12)
		admission.release(name)
		// Let the backlog member consume its recent ticket, then add newcomers.
		if admission.recentBurst < claimRecentBurst {
			if index := claimTestAcquire(t, admission, "backlog", true, 30, 12); index != 0 {
				t.Fatal("premature historical selection")
			}
			admission.release("backlog")
			turn++
		}
	}
	claimTestAcquire(t, admission, "latest", false, 30)
	if index := claimTestAcquire(t, admission, "backlog", true, 30, 12); index != 1 {
		t.Fatalf("historical ticket was replaced by fresh work: index=%d", index)
	}
	admission.release("backlog")
	claimTestAcquire(t, admission, "latest", false, 30)
	claimTestAcquire(t, admission, "backlog", true, 30)
	admission.release("backlog")
	claimTestAcquire(t, admission, "latest", true, 30)
	admission.release("latest")
}

// Tickets already waiting at a lagging local epoch lose current priority as
// soon as another member discovers a newer epoch. Local recovery still runs.
func TestClaimAdmissionUsesSharedDiscoveryHighWater(t *testing.T) {
	admission := &claimAdmission{}
	admission.observeEpoch(14)
	claimTestAcquire(t, admission, "owner", true, 14)
	claimTestAcquire(t, admission, "lagging", false, 14, 13)
	admission.observeEpoch(17)
	claimTestAcquire(t, admission, "current", false, 17)
	admission.release("owner")
	claimTestAcquire(t, admission, "lagging", false, 14, 13)
	claimTestAcquire(t, admission, "current", true, 17)
	admission.release("current")
	claimTestAcquire(t, admission, "lagging", true, 14, 13)
	admission.release("lagging")
	if got := admission.observeEpoch(14); got != 17 {
		t.Fatalf("shared discovery regressed to %d", got)
	}
}

// A cancelled waiter removes only its own tickets. It cannot unlock an owner,
// consume a submission attempt, or steal a ready signal from another waiter.
func TestClaimAdmissionCancellationDoesNotReleaseAnotherOwner(t *testing.T) {
	admission := &claimAdmission{}
	claimTestAcquire(t, admission, "owner", true, 1)
	claimTestAcquire(t, admission, "cancelled", false, 1)
	claimTestAcquire(t, admission, "next", false, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, acquired, err := admission.acquire(ctx, "cancelled", claimAdmissionCandidates(1)); acquired || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquire = %t, %v", acquired, err)
	}
	admission.forget("cancelled")
	claimTestAcquire(t, admission, "next", false, 1)
	admission.release("owner")
	claimTestAcquire(t, admission, "next", true, 1)
	admission.release("next")
}

func TestClaimAdmissionBudgetStartsOnlyAfterGrant(t *testing.T) {
	admission := &claimAdmission{}
	claimTestAcquire(t, admission, "held", true, 1)
	if index, ctx, done, err := beginClaimOperation(context.Background(), admission, "waiting", claimAdmissionCandidates(1)); index != -1 || ctx != nil || done != nil || err != nil {
		t.Fatalf("waiting allocated an operation: %d %v %v", index, ctx, err)
	}
	admission.release("held")
	before := time.Now()
	index, ctx, done, err := beginClaimOperation(context.Background(), admission, "waiting", claimAdmissionCandidates(1))
	if err != nil || index != 0 {
		t.Fatalf("grant = %d, %v", index, err)
	}
	deadline, ok := ctx.Deadline()
	if !ok || deadline.Before(before.Add(5*time.Minute)) || deadline.After(time.Now().Add(5*time.Minute)) {
		t.Fatalf("grant lost its five-minute budget: %v", deadline)
	}
	done()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("release did not cancel its owner context")
	}
	parent, cancel := context.WithCancel(context.Background())
	_, ctx, done, err = beginClaimOperation(parent, admission, "parent", claimAdmissionCandidates(1))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("parent cancellation was hidden")
	}
	done()
}

// Queue ownership is serial, but admission misses are immediate. Two explicit
// discovery events advance the durable frontier while another miner holds the
// nonce; no readiness RPC or signing callback may execute on the waiting miner.
func TestClaimQueueDiscoveryAdvancesWhileAnotherMemberOwnsNonce(t *testing.T) {
	admission := &claimAdmission{}
	admission.observeEpoch(10)
	claimTestAcquire(t, admission, "held", true, 10)
	queue := &ClaimQueue{LastDiscovered: 10, Entries: map[string]*ClaimQueueEntry{"10": {Epoch: 10, Status: "pending"}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	epochs := make(chan int64)
	saved := make(chan int64)
	done := make(chan error, 1)
	hooks := claimPollTestHooks(t, time.Now())
	hooks.latestEpoch = admission.observeEpoch
	hooks.begin = func(ctx context.Context, candidates []claimPollCandidate) (int, context.Context, func(), error) {
		return beginClaimOperation(ctx, admission, "waiting", candidates)
	}
	hooks.save = func(queue *ClaimQueue) error {
		for _, entry := range queue.Entries {
			if entry.Attempts != 0 || entry.Status != "pending" {
				return fmt.Errorf("waiting changed transaction state: %+v", entry)
			}
		}
		saved <- queue.LastDiscovered
		return nil
	}
	go func() {
		defer admission.forget("waiting")
		done <- runClaimQueue(ctx, queue, 2, epochs, nil, func() <-chan struct{} { return admission.ready("waiting") }, hooks)
	}()
	for _, epoch := range []int64{12, 14} {
		epochs <- epoch
		if got := <-saved; got != epoch-1 {
			t.Fatalf("discovery saved %d, want %d", got, epoch-1)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if admission.owner != "held" || admission.ready("waiting") != nil {
		t.Fatal("waiting cancellation released owner or retained a ticket")
	}
	admission.release("held")
}

// A blocked epoch API is independent of both ticket readiness and cancellation.
// The barrier is the read itself; no sleep or short negative timeout is needed.
func TestClaimEpochReaderCancellationJoinsBlockedRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan context.Context, 1)
	before := time.Now()
	_, done := startClaimEpochReader(ctx, time.Hour, func(readCtx context.Context) (int64, error) {
		entered <- readCtx
		<-readCtx.Done()
		return 0, readCtx.Err()
	})
	readCtx := <-entered
	if deadline, ok := readCtx.Deadline(); !ok || deadline.Before(before.Add(5*time.Minute)) || deadline.After(time.Now().Add(5*time.Minute)) {
		t.Fatal("epoch reader has no five-minute budget")
	}
	cancel()
	<-done
}

// Admission covers reconciliation, intent fsync, preparation and completion;
// signed cancellation is durably uncertain before the next member can proceed.
func TestClaimQueueAdmissionSpansReconcileAndSignedTimeout(t *testing.T) {
	admission := &claimAdmission{}
	queue := &ClaimQueue{LastDiscovered: 5, Entries: map[string]*ClaimQueueEntry{"5": {Epoch: 5, Status: "pending"}}}
	hooks := claimPollTestHooks(t, time.Now())
	hooks.latestEpoch = admission.observeEpoch
	hooks.begin = func(ctx context.Context, candidates []claimPollCandidate) (int, context.Context, func(), error) {
		return beginClaimOperation(ctx, admission, "owner", candidates)
	}
	states := []string{}
	blocked := func() { claimTestAcquire(t, admission, "next", false, 5) }
	hooks.reconcile = func(context.Context, *ClaimQueueEntry) (string, error) { blocked(); return "", nil }
	hooks.save = func(queue *ClaimQueue) error {
		blocked()
		states = append(states, queue.Entries["5"].Status)
		return nil
	}
	hooks.submit = func(ctx context.Context, entry *ClaimQueueEntry) error {
		blocked()
		entry.TxHash, entry.RawTxHex = "synthetic retained hash", "synthetic retained raw"
		return context.DeadlineExceeded
	}
	if err := pollClaimQueue(context.Background(), queue, hooks); err != nil {
		t.Fatal(err)
	}
	entry := queue.Entries["5"]
	if !reflect.DeepEqual(states, []string{"submitting", "uncertain"}) || entry.Attempts != 1 || entry.TxHash != "synthetic retained hash" || entry.RawTxHex != "synthetic retained raw" {
		t.Fatalf("timeout lost custody: states=%v entry=%+v", states, entry)
	}
	claimTestAcquire(t, admission, "next", true, 5)
	admission.release("next")
}

// Even when each attempt outlasts its retry delay, older signed outcomes get
// a turn instead of the newest failed historical entry monopolizing the lane.
func TestClaimHistoricalOutcomesProgressAcrossSlowAttempts(t *testing.T) {
	admission := &claimAdmission{}
	admission.observeEpoch(30)
	queue := &ClaimQueue{LastDiscovered: 30, Entries: map[string]*ClaimQueueEntry{}}
	for epoch := int64(1); epoch <= 3; epoch++ {
		queue.Entries[fmt.Sprint(epoch)] = &ClaimQueueEntry{Epoch: epoch, Status: "uncertain", TxHash: "synthetic retained hash", RawTxHex: "synthetic retained raw"}
	}
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	hooks := claimPollTestHooks(t, now)
	hooks.now = func() time.Time { return now }
	hooks.save = func(*ClaimQueue) error { return nil }
	hooks.latestEpoch = admission.observeEpoch
	hooks.begin = func(ctx context.Context, candidates []claimPollCandidate) (int, context.Context, func(), error) {
		return beginClaimOperation(ctx, admission, "member", candidates)
	}
	visited := []int64{}
	hooks.reconcile = func(_ context.Context, entry *ClaimQueueEntry) (string, error) {
		visited = append(visited, entry.Epoch)
		return "", errors.New("synthetic pending exact outcome")
	}
	for index := 0; index < 6; index++ {
		if err := pollClaimQueue(context.Background(), queue, hooks); err != nil {
			t.Fatal(err)
		}
		now = now.Add(10 * time.Minute)
	}
	if !reflect.DeepEqual(visited, []int64{3, 2, 1, 3, 2, 1}) {
		t.Fatalf("slow historical outcome starved older entries: %v", visited)
	}
}

// A prepared checkpoint can outlive a key/identity failure. Stop the whole
// nonce domain before release, rather than racing the swarm's later cancel.
func TestClaimAdmissionCustodyFailureBlocksNextOwner(t *testing.T) {
	admission := &claimAdmission{}
	claimTestAcquire(t, admission, "owner", true, 1)
	claimTestAcquire(t, admission, "waiting", false, 1)
	failure := admission.rememberSigned(&ClaimDaemonConfig{}, &ClaimQueueEntry{Epoch: 1, RawTxHex: "synthetic invalid signed bytes", TxHash: "synthetic hash"})
	var custody *claimNonceSafetyError
	if !errors.As(failure, &custody) {
		t.Fatalf("missing typed custody failure: %v", failure)
	}
	admission.release("owner")
	_, acquired, err := admission.acquire(context.Background(), "waiting", claimAdmissionCandidates(1))
	if acquired || !errors.As(err, &custody) {
		t.Fatalf("next owner bypassed unresolved nonce custody: acquired=%t err=%v", acquired, err)
	}
}

// A ready retained ticket executes while the epoch API is still blocked. The
// read is cancelled and joined only after that queued operation has completed.
func TestClaimQueueReadyTicketBypassesBlockedEpochRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readEntered := make(chan struct{}, 1)
	epochs, readDone := startClaimEpochReader(ctx, time.Hour, func(ctx context.Context) (int64, error) { readEntered <- struct{}{}; <-ctx.Done(); return 0, ctx.Err() })
	<-readEntered
	admission := &claimAdmission{}
	admission.observeEpoch(10)
	claimTestAcquire(t, admission, "held", true, 10)
	queue := &ClaimQueue{LastDiscovered: 10, Entries: map[string]*ClaimQueueEntry{"10": {Epoch: 10, Status: "pending"}}}
	waiting := make(chan struct{}, 1)
	processed := make(chan struct{}, 1)
	hooks := claimPollTestHooks(t, time.Now())
	hooks.latestEpoch = admission.observeEpoch
	hooks.begin = func(ctx context.Context, candidates []claimPollCandidate) (int, context.Context, func(), error) {
		index, operationCtx, done, err := beginClaimOperation(ctx, admission, "waiting", candidates)
		if index < 0 && len(candidates) > 0 {
			select {
			case waiting <- struct{}{}:
			default:
			}
		}
		return index, operationCtx, done, err
	}
	hooks.reconcile = func(context.Context, *ClaimQueueEntry) (string, error) { return "no-claim", nil }
	hooks.save = func(*ClaimQueue) error { processed <- struct{}{}; return nil }
	done := make(chan error, 1)
	go func() {
		defer admission.forget("waiting")
		done <- runClaimQueue(ctx, queue, 2, epochs, nil, func() <-chan struct{} { return admission.ready("waiting") }, hooks)
	}()
	<-waiting
	admission.release("held")
	<-processed
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-readDone
	if queue.Entries["10"].Status != "no-claim" || queue.Entries["10"].Attempts != 0 {
		t.Fatalf("ready operation changed claim accounting: %+v", queue.Entries["10"])
	}
}
