//go:build linux || darwin

// Real pending custody remains usable when optional acceleration fails or a
// genuine body prefix outlives one read attempt's logical deadline.
package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// Two deterministic deadline barriers interrupt the actual shared scan after
// complete bodies. The production retry owner must preserve those bodies and
// continue, not repeatedly spend its 60-second attempt on the same prefix.
func TestProductionReceiptInterruptedBudgetRetainsActualPartialPrefix(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	original, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.production.head, native.receiptNumber = 104, 105
	fixture.production.epoch++
	native.bodyReads = map[uint64]int{}
	var attempt *productionNativeReadDeadline
	var attempts, intentAttempts, operations, waits, interrupted int
	fixture.steerer.productionReadHooks.withTimeout = func(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
		switch duration {
		case 300 * time.Second:
			operations++
			return context.WithTimeout(parent, duration)
		case productionSteeringIntentReadAttemptTimeout:
			// The retained intent read that precedes reconciliation.
			intentAttempts++
			return context.WithTimeout(parent, duration)
		case productionSteeringReadAttemptTimeout:
			attempts++
			var cancel context.CancelFunc
			attempt, cancel = newProductionNativeReadDeadline(parent, duration)
			return attempt, cancel
		default:
			t.Fatalf("receipt retry changed the production read budget: %v", duration)
			return nil, nil
		}
	}
	fixture.steerer.productionReadHooks.wait = func(ctx context.Context, duration time.Duration) error {
		if ctx.Err() != nil || duration != releaseSnapshotStartupRetryDelay {
			t.Fatalf("partial-prefix retry lost its live operation owner: %v", ctx.Err())
		}
		waits++
		return nil
	}
	native.afterBody = func(number uint64) {
		if number != 100 && number != 102 {
			return
		}
		if native.bodyReads[number] != 1 {
			t.Fatalf("attempt deadline replayed an already admitted partial-prefix body %d", number)
		}
		if attempt == nil || attempt.Err() != nil {
			t.Fatal("actual body barrier lacks its live production attempt")
		}
		interrupted++
		attempt.finish(context.DeadlineExceeded)
	}
	var pendingWait *productionPendingReconciliation
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &pendingWait) {
		t.Fatalf("slow complete bodies stopped original reconciliation: %v", err)
	}
	if interrupted != 2 || waits != 2 || operations == 0 || intentAttempts != 1 || attempts+intentAttempts <= operations || !slices.Equal(native.nonceReads, []uint64{104}) {
		t.Fatalf("partial retries borrowed an incomplete or different boundary: interrupted=%d waits=%d attempts=%d intent attempts=%d operations=%d nonces=%v", interrupted, waits, attempts, intentAttempts, operations, native.nonceReads)
	}
	for number := uint64(100); number <= 104; number++ {
		if native.bodyReads[number] != 1 {
			t.Fatalf("budget recovery repeated complete body %d: %d reads", number, native.bodyReads[number])
		}
	}
	owner, err := newProductionReceiptCheckpointOwner(fixture.steerer, fixture.steerer.cfg, pending)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := owner.load(t.Context())
	if err != nil || checkpoint == nil || checkpoint.Through != 104 {
		t.Fatalf("successful partial recovery did not persist its real final boundary: %+v %v", checkpoint, err)
	}
	after, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil || !bytes.Equal(original, after) || !slices.Equal(native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) {
		t.Fatal("partial deadline recovery mutated or replaced the signed liability")
	}
}

// A receipt attempt whose runtime timer lags its wall-clock deadline. Once
// armed, its deadline passes just after the next Err answers, and Err stays
// unset until the retry owner cancels the attempt.
type productionReceiptLateTimerAttempt struct {
	context.Context
	stateLock sync.Mutex
	deadline  time.Time
	armed     bool
	expired   bool
}

func (self *productionReceiptLateTimerAttempt) Deadline() (time.Time, bool) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.deadline, true
}

// Armed at a chunk's last body, the next Err is the first check after the
// chunk completed: the scanner's own completion check, or the read owner's
// when a found receipt ends the scan. It answers, then the deadline passes.
func (self *productionReceiptLateTimerAttempt) Err() error {
	err := self.Context.Err()
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.armed && err == nil {
		self.armed, self.expired = false, true
		self.deadline = time.Unix(1, 0)
	}
	return err
}

func (self *productionReceiptLateTimerAttempt) arm() {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.armed = true
}

func (self *productionReceiptLateTimerAttempt) passed() bool {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.expired
}

// Head 104 makes one 100-104 chunk. The first read of body last arms the live
// receipt attempt, so the chunk completes and then that attempt's deadline
// passes before its timer sets Err. Other read budgets keep their real timers.
func productionReceiptTestLateChunk(t *testing.T, last uint64) (*productionContinuationTestFixture, *productionContinuationNativeTestClient, *SteeringIntent, []byte, func() *productionReceiptLateTimerAttempt) {
	t.Helper()
	fixture := newProductionContinuationTestFixture(t)
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	original, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.production.head, native.receiptNumber = 104, 105
	fixture.production.epoch++
	native.bodyReads = map[uint64]int{}
	var attempt, late *productionReceiptLateTimerAttempt
	fixture.steerer.productionReadHooks.withTimeout = func(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithTimeout(parent, duration)
		if duration != productionSteeringReadAttemptTimeout {
			return ctx, cancel
		}
		deadline, _ := ctx.Deadline()
		attempt = &productionReceiptLateTimerAttempt{Context: ctx, deadline: deadline}
		return attempt, cancel
	}
	native.afterBody = func(number uint64) {
		if number == last && native.bodyReads[last] == 1 {
			if attempt == nil || attempt.Err() != nil {
				t.Fatal("the chunk's last body lacks its live receipt attempt")
			}
			late = attempt
			late.arm()
		}
	}
	return fixture, native, pending, original, func() *productionReceiptLateTimerAttempt { return late }
}

// Lets the retry owner pace and retry an expired attempt, counting each wait.
func productionReceiptTestRetries(t *testing.T, fixture *productionContinuationTestFixture) *int {
	t.Helper()
	waits := 0
	fixture.steerer.productionReadHooks.wait = func(ctx context.Context, duration time.Duration) error {
		if ctx.Err() != nil || duration != releaseSnapshotStartupRetryDelay {
			t.Fatalf("late chunk retry lost its live operation owner: %v", ctx.Err())
		}
		waits++
		return nil
	}
	return &waits
}

// The retry owner refuses a chunk that completed as its attempt expired, like
// any late response. Its real bodies are kept anyway, and the retry resumes
// after them instead of reading the same chunk again.
func TestProductionReceiptLateChunkResumesInTheRetry(t *testing.T) {
	fixture, native, pending, original, late := productionReceiptTestLateChunk(t, 104)
	waits := productionReceiptTestRetries(t, fixture)
	var pendingWait *productionPendingReconciliation
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &pendingWait) {
		t.Fatalf("a late completed chunk stopped original reconciliation: %v", err)
	}
	if late() == nil || !late().passed() || *waits != 1 || !slices.Equal(native.nonceReads, []uint64{104}) {
		t.Fatalf("the late chunk was not the one expired attempt, or the nonce left coverage: waits=%d nonces=%v", *waits, native.nonceReads)
	}
	for number := uint64(100); number <= 104; number++ {
		if native.bodyReads[number] != 1 {
			t.Fatalf("the retry read completed body %d again: %d reads", number, native.bodyReads[number])
		}
	}
	owner, err := newProductionReceiptCheckpointOwner(fixture.steerer, fixture.steerer.cfg, pending)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := owner.load(t.Context())
	if err != nil || checkpoint == nil || checkpoint.Through != 104 {
		t.Fatalf("the late chunk's real boundary was not checkpointed: %+v %v", checkpoint, err)
	}
	after, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil || !bytes.Equal(original, after) || !slices.Equal(native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) {
		t.Fatal("keeping a late chunk mutated or replaced the signed liability")
	}
}

// When the expired attempt is its read's last, the late chunk is still kept on
// disk and in memory. A cold runtime's new intent owner resumes after it.
func TestProductionReceiptLateChunkIsCheckpointedWhenItsReadEnds(t *testing.T) {
	fixture, native, pending, original, late := productionReceiptTestLateChunk(t, 104)
	var wait *productionSteeringReadWait
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &wait) || wait.phase != productionReadReceipt || !errors.Is(err, context.DeadlineExceeded) || late() == nil || !late().passed() {
		t.Fatalf("the late chunk's expiry did not end the receipt read: %v", err)
	}
	owner, err := newProductionReceiptCheckpointOwner(fixture.steerer, fixture.steerer.cfg, pending)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := owner.load(t.Context())
	if err != nil || checkpoint == nil || checkpoint.Through != 104 {
		t.Fatalf("the late chunk was not saved to disk: %+v %v", checkpoint, err)
	}
	memory, disabled := fixture.runtime.receiptCache.snapshot(owner.scope)
	if disabled || memory == nil || memory.Through != 104 {
		t.Fatalf("the late chunk was not kept in memory: disabled=%t memory=%+v", disabled, memory)
	}
	fixture.runtime.receiptCache = &productionReceiptCacheState{}
	fixture.restart(t)
	var pendingWait *productionPendingReconciliation
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.As(err, &pendingWait) {
		t.Fatalf("the checkpointed late chunk stranded original reconciliation: %v", err)
	}
	for number := uint64(100); number <= 104; number++ {
		if native.bodyReads[number] != 1 {
			t.Fatalf("the next poll read checkpointed body %d again: %d reads", number, native.bodyReads[number])
		}
	}
	after, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil || !bytes.Equal(original, after) || !slices.Equal(native.nonceReads, []uint64{104}) || !slices.Equal(native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) {
		t.Fatalf("the checkpointed late chunk rewrote custody or read a nonce outside coverage: nonces=%v", native.nonceReads)
	}
}

// A chunk that found the original transaction has no absence to checkpoint.
// When it completes as its attempt expires, the retry keeps its receipt
// instead of reading the chunk again to find the same inclusion.
func TestProductionReceiptLateChunkKeepsItsReceiptInTheRetry(t *testing.T) {
	fixture, native, pending, original, late := productionReceiptTestLateChunk(t, 103)
	native.receiptNumber = 103
	fixture.production.extrinsicsKVs = map[uint64][]string{103: {pending.Prepared.ExtrinsicHex}}
	waits := productionReceiptTestRetries(t, fixture)
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("a late chunk's receipt did not finalize the original intent: %v", err)
	}
	if late() == nil || !late().passed() || *waits != 1 || len(native.nonceReads) != 0 {
		t.Fatalf("the late receipt chunk was not the one expired attempt, or a nonce was read: waits=%d nonces=%v", *waits, native.nonceReads)
	}
	// The receipt's own authentication reads body 103 again; only the scan
	// reads the absent bodies before it, and the scan stops at the receipt.
	for number := uint64(100); number < 103; number++ {
		if native.bodyReads[number] != 1 {
			t.Fatalf("the retry read completed body %d again: %d reads", number, native.bodyReads[number])
		}
	}
	if native.bodyReads[104] != 0 {
		t.Fatal("the scan read past its receipt")
	}
	after, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil || bytes.Equal(original, after) {
		t.Fatalf("the kept receipt did not finalize custody: %v", err)
	}
	finalized := fixture.restart(t)
	if finalized.Status != "finalized" || finalized.FinalizedBlock != 103 || finalized.CreatedAt != pending.CreatedAt || finalized.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || !slices.Equal(native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) {
		t.Fatal("the kept receipt lost the original signature, age or inclusion block")
	}
}

// A corrupt optional prefix is never used as evidence. Actual chain reads
// restore progress, while a closed observation reports cache degradation once.
func TestProductionReceiptCorruptCacheFallsBackToOriginalIntent(t *testing.T) {
	productionReceiptTestCacheDegradation(t, productionReceiptCacheRead)
}

// Failed optional persistence retains the proven memory boundary, without
// claiming durability or consuming the service's original-intent error budget.
func TestProductionReceiptUnwritableCachePreservesReconciliation(t *testing.T) {
	productionReceiptTestCacheDegradation(t, productionReceiptCacheWrite)
}

// The only fault is a real optional-path file, introduced before reading or
// after a body barrier. Source signatures, store replay and RPC admission run.
func productionReceiptTestCacheDegradation(t *testing.T, stage productionReceiptCacheStage) {
	t.Helper()
	fixture := newProductionContinuationTestFixture(t)
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	original, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.production.head, native.receiptNumber = 101, 103
	fixture.production.epoch++
	native.bodyReads = map[uint64]int{}
	owner, err := newProductionReceiptCheckpointOwner(fixture.steerer, fixture.steerer.cfg, pending)
	if err != nil {
		t.Fatal(err)
	}
	if stage == productionReceiptCacheRead {
		if err := os.Mkdir(owner.path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(owner.path, productionReceiptCheckpointName), []byte("{invalid cache"), 0o600); err != nil {
			t.Fatal(err)
		}
	} else {
		native.afterBody = func(number uint64) {
			if number == 100 {
				if err := os.WriteFile(owner.path, []byte("optional namespace unavailable"), 0o600); err != nil {
					t.Fatal(err)
				}
				native.afterBody = nil
			}
		}
	}
	var observations []productionReceiptCacheObservation
	ctx := withProductionReceiptCacheDiagnostic(t.Context(), func(value productionReceiptCacheObservation) {
		if fixture.steerer.intents.v2.active.Load() {
			t.Fatal("cache diagnostic ran while original intent custody was active")
		}
		observations = append(observations, value)
	})
	var pendingWait *productionPendingReconciliation
	if err := fixture.steerer.submitOnceV2(ctx); !errors.As(err, &pendingWait) {
		t.Fatalf("optional cache failure stopped original pending reconciliation: %v", err)
	}
	memory, disabled := fixture.runtime.receiptCache.snapshot(owner.scope)
	if !disabled || memory == nil || memory.Through != 101 || len(observations) != 1 || observations[0].nativeEpoch != pending.SubnetEpoch || observations[0].stage != stage {
		t.Fatalf("cache degradation lost verified progress or its bounded notice: disabled=%t memory=%+v observations=%+v", disabled, memory, observations)
	}
	// A new actual disk intent owner in this runtime must not reopen the bad
	// optional cache or repeat the already admitted prefix on its next poll.
	fixture.restart(t)
	fixture.production.head = 102
	if err := fixture.steerer.submitOnceV2(ctx); !errors.As(err, &pendingWait) {
		t.Fatalf("disabled cache stranded later original reconciliation: %v", err)
	}
	for number := uint64(100); number <= 102; number++ {
		if native.bodyReads[number] != 1 {
			t.Fatalf("disabled cache repeated authenticated body %d: %d reads", number, native.bodyReads[number])
		}
	}
	after, err := os.ReadFile(fixture.steerer.intents.path)
	if err != nil || !bytes.Equal(original, after) || len(observations) != 1 || !slices.Equal(native.nonceReads, []uint64{101, 102}) || !slices.Equal(native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) {
		t.Fatal("cache failure erased, replaced or obscured original signed progress")
	}
}
