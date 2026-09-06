package validator

// One exclusive write owner orders every Stats mutation and snapshot without
// blocking public readers during external I/O. Multi-engine owners acquire all
// tokens in immutable engine order before taking any short-lived state mutex.
// Caller-provided operator labels only order canonical output, never locks.

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"sort"
	"sync"

	"github.com/urnetwork/connect"
)

// This tiny allocator orders engine instances only. It never covers callbacks,
// persistence, token waits or any participant state mutex, so unrelated batches
// retain independent progress. Exhaustion fails without wrapping an order id.
var statsWriteOrders struct {
	stateLock sync.Mutex
	next      uint64
}

// Private hooks are immutable after publication and never run under Stats.mu.
// A callback may read Stats, but must not recursively mutate an owned engine.
type statsWriteHooks struct {
	step          func(operation, stage string)
	writeSnapshot func(string, []byte) error
}

// The non-zero-sized owner pointer uniquely identifies one retained token.
type statsWriteOwner struct {
	engine    *StatsEngine
	ctx       context.Context
	operation string
	hooks     statsWriteHooks
}

// Captures control state as well as the exclusive owner. Comparison happens
// before the first snapshot write, with the token retained through publication.
type statsReplayBasis struct {
	ledger                                       *AttemptLedger
	epoch, generation, applied, settlement, head uint64
	known, cutPending, settlementPending         bool
	settlementCutEpoch, active                   uint64
	transition                                   *AttemptSettlementTransition
}

// Once-published engine order and gate remain immutable for the engine's whole
// lifetime, including before a ledger is attached to a compatibility engine.
func (self *StatsEngine) prepareStatsWrite() error {
	self.writeGateOnce.Do(func() {
		order, err := func() (uint64, error) {
			statsWriteOrders.stateLock.Lock()
			defer statsWriteOrders.stateLock.Unlock()
			if statsWriteOrders.next == ^uint64(0) {
				return 0, errors.New("statistics engine order exhausted")
			}
			statsWriteOrders.next++
			return statsWriteOrders.next, nil
		}()
		self.writeInitErr = err
		if err == nil {
			self.writeOrder = order
			self.writeGate = make(chan struct{}, 1)
		}
	})
	return self.writeInitErr
}

// A participant can be labeled differently in another batch. Engine order is
// shared by writer-token acquisition and all multi-engine state-mutex readers;
// canonical NoID output ordering remains the caller's separate validated slice.
func orderStatsEngineOwners(participants []AttemptSettlementParticipant) ([]AttemptSettlementParticipant, error) {
	ordered := append([]AttemptSettlementParticipant(nil), participants...)
	for _, participant := range ordered {
		if participant.Stats == nil {
			return nil, errors.New("statistics engine order contains a nil engine")
		}
		if err := participant.Stats.prepareStatsWrite(); err != nil {
			return nil, err
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Stats.writeOrder < ordered[j].Stats.writeOrder })
	for index := 1; index < len(ordered); index++ {
		if ordered[index-1].Stats == ordered[index].Stats {
			return nil, errors.New("statistics engine order contains a duplicate engine")
		}
	}
	return ordered, nil
}

// Admission is cancellable before and while queued, independently of state
// mutex ownership. Init never runs callbacks or performs filesystem operations.
func (self *StatsEngine) acquireStatsWrite(ctx context.Context, operation string) (*statsWriteOwner, error) {
	if ctx == nil {
		return nil, errors.New("statistics write context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := self.prepareStatsWrite(); err != nil {
		return nil, err
	}
	select {
	case self.writeGate <- struct{}{}:
	default:
		if self.writeHooks.step != nil {
			self.writeHooks.step(operation, "waiting")
		}
		select {
		case self.writeGate <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		<-self.writeGate
		return nil, err
	}
	owner := &statsWriteOwner{engine: self, ctx: ctx, operation: operation, hooks: self.writeHooks}
	self.mu.Lock()
	self.writeOwner = owner
	self.mu.Unlock()
	return owner, nil
}

// Compatibility mutators have no cancellation result; they wait for the same
// exclusive token instead of silently dropping data while startup owns it.
func (self *StatsEngine) lockStatsWrite(operation string) *statsWriteOwner {
	owner, err := self.acquireStatsWrite(context.Background(), operation)
	if err != nil {
		panic(err)
	}
	return owner
}

// Releasing ownership is separate from state publication and joins no worker.
func (self *statsWriteOwner) release() {
	self.engine.mu.Lock()
	if self.engine.writeOwner != self {
		self.engine.mu.Unlock()
		panic("statistics write ownership changed before release")
	}
	self.engine.writeOwner = nil
	self.engine.mu.Unlock()
	<-self.engine.writeGate
}

// Hooks can force deterministic admission/persistence boundaries, not change
// the ownership contract. They execute with no Stats state mutex retained.
func (self *statsWriteOwner) step(stage string) {
	if self.hooks.step != nil {
		self.hooks.step(self.operation, stage)
	}
}

// Validates the retained token before an external side effect. No later writer
// can pass admission until both durable and memory publication have completed.
func (self *statsWriteOwner) check() error {
	self.engine.mu.Lock()
	defer self.engine.mu.Unlock()
	if self.engine.writeOwner != self {
		return errors.New("statistics write ownership changed")
	}
	return nil
}

// The commit boundary is the start of the writer after the final cancellation
// check. A successful write is published even if cancellation arrives during it.
func (self *statsWriteOwner) persist(path string, data []byte) error {
	return self.persistChecked(path, data, self.check)
}

// Startup supplies its stronger generation comparison at the same final
// pre-write boundary, after hooks and cancellation but before any writer call.
func (self *statsWriteOwner) persistChecked(path string, data []byte, check func() error) error {
	self.step("before-snapshot")
	if err := self.ctx.Err(); err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	if self.hooks.writeSnapshot != nil {
		return self.hooks.writeSnapshot(path, data)
	}
	return atomicStateWrite(path, data, 0o600)
}

// Only startup/boundary work clones the full window; hot-path checkpoints and
// terminal commits retain incremental counters under the same write token.
func (self *StatsEngine) cloneStatsWithLock() *StatsEngine {
	candidate := &StatsEngine{
		cfg: self.cfg, window: cloneProviderWindows(self.window), ema: maps.Clone(self.ema), emaPPM: maps.Clone(self.emaPPM),
		egress:          make(map[connect.Id]map[[32]byte]bool, len(self.egress)),
		settlementEpoch: self.settlementEpoch, settlementEpochKnown: self.settlementEpochKnown, egressGeneration: self.egressGeneration,
		attemptLedger: self.attemptLedger, attemptLastAppliedSequence: self.attemptLastAppliedSequence,
		attemptSettlementFirstSequence: self.attemptSettlementFirstSequence, attemptEgressFirstSequence: self.attemptEgressFirstSequence,
		activeAttemptCount: self.activeAttemptCount, attemptCutPending: self.attemptCutPending,
		attemptSettlementCutPending: self.attemptSettlementCutPending, attemptSettlementCutEpoch: self.attemptSettlementCutEpoch,
		settlementTransition: self.settlementTransition,
	}
	for clientID, hashes := range self.egress {
		candidate.egress[clientID] = maps.Clone(hashes)
	}
	return candidate
}

// Copies all mutable state, including retry reservations, but never the
// owner's synchronization or callback fields. Caller retains original Stats.mu.
func (self *StatsEngine) publishStatsWithLock(candidate *StatsEngine) {
	self.window, self.ema, self.emaPPM, self.egress = candidate.window, candidate.ema, candidate.emaPPM, candidate.egress
	self.settlementEpoch, self.settlementEpochKnown, self.egressGeneration = candidate.settlementEpoch, candidate.settlementEpochKnown, candidate.egressGeneration
	self.attemptLedger, self.attemptLastAppliedSequence = candidate.attemptLedger, candidate.attemptLastAppliedSequence
	self.attemptSettlementFirstSequence, self.attemptEgressFirstSequence = candidate.attemptSettlementFirstSequence, candidate.attemptEgressFirstSequence
	self.activeAttemptCount, self.attemptCutPending = candidate.activeAttemptCount, candidate.attemptCutPending
	self.attemptSettlementCutPending, self.attemptSettlementCutEpoch = candidate.attemptSettlementCutPending, candidate.attemptSettlementCutEpoch
	self.settlementTransition = candidate.settlementTransition
}

// Creates a detached candidate while a caller retains the exclusive token.
func (self *statsWriteOwner) clone() *StatsEngine {
	self.engine.mu.Lock()
	defer self.engine.mu.Unlock()
	return self.engine.cloneStatsWithLock()
}

// Publishes one complete candidate under a short state lock, before release.
func (self *statsWriteOwner) publish(candidate *StatsEngine) {
	self.engine.mu.Lock()
	defer self.engine.mu.Unlock()
	if self.engine.writeOwner != self {
		panic("statistics write ownership changed before publication")
	}
	self.engine.publishStatsWithLock(candidate)
}

// A candidate can use existing snapshot encoding without retaining any state
// mutex while the supplied write operation executes.
func (self *StatsEngine) saveOwned(dir string, persist func(string, []byte) error) error {
	b, err := encodeStatsSnapshot(self.snapshotStats())
	if err != nil {
		return err
	}
	return persist(filepath.Join(dir, "stats.json"), b)
}

// Copy under only the short state mutex, never across encoding or persistence.
func (self *StatsEngine) snapshotStats() statsSnapshot {
	self.mu.Lock()
	defer self.mu.Unlock()
	return self.snapshotWithLock()
}

// Boundary candidates are exclusively owned; this short lock also maintains
// the existing pure in-memory helper's explicit lock convention.
func (self *StatsEngine) foldStatsOwned() {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.foldWithLock()
}

// Captures startup's control-generation comparison while holding Stats.mu.
func (self *StatsEngine) replayBasisWithLock() statsReplayBasis {
	return statsReplayBasis{
		ledger: self.attemptLedger, epoch: self.settlementEpoch, known: self.settlementEpochKnown,
		generation: self.egressGeneration, applied: self.attemptLastAppliedSequence,
		settlement: self.attemptSettlementFirstSequence, head: self.attemptEgressFirstSequence,
		active: self.activeAttemptCount, cutPending: self.attemptCutPending,
		settlementPending: self.attemptSettlementCutPending, settlementCutEpoch: self.attemptSettlementCutEpoch,
		transition: self.settlementTransition,
	}
}

// The comparison is before persistence; the same owner prevents a writer from
// changing the compared generation between this check and the atomic rename.
func (self *statsWriteOwner) checkReplayBasis(basis statsReplayBasis) error {
	self.engine.mu.Lock()
	defer self.engine.mu.Unlock()
	if self.engine.writeOwner != self || self.engine.replayBasisWithLock() != basis {
		return errors.New("statistics replay generation changed before snapshot publication")
	}
	return nil
}
