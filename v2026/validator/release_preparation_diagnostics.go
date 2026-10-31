// Closed preparation stages use the existing bounded exporter without changing
// operation ownership, returned errors, or any retry and admission decision.
package validator

import (
	"context"
	"fmt"
	"time"
)

// Stages name actual call boundaries, never a supplied error or endpoint.
type releasePreparationStage uint8

const (
	releasePreparationUnknown releasePreparationStage = iota
	releasePreparationRetainedIntent
	releasePreparationNativeSnapshot
	releasePreparationEvmSnapshot
	releasePreparationPinnedChains
	releasePreparationRetainedHead
	releasePreparationMetagraph
	releasePreparationNativeSchedule
	releasePreparationCollectGate
	releasePreparationCollectReconcile
	releasePreparationCollectAdvance
	releasePreparationServerKeys
	releasePreparationCollectInputs
	releasePreparationInputOwner
	releasePreparationInputRead
	releasePreparationInputDecode
	releasePreparationInputSync
	releasePreparationInputReconcile
	releasePreparationInputDetach
	releasePreparationInputRetain
	releasePreparationStatsWrite
	releasePreparationStatsSeal
	releasePreparationStatsVerify
	releasePreparationStatsJournal
	releasePreparationStatsSnapshot
	releasePreparationSealRecords
	releasePreparationSealProofs
	releasePreparationSealMetadata
	releasePreparationSealReplay
	releasePreparationGatherHead
	releasePreparationMeasurement
	releasePreparationHttpOpen
	releasePreparationHttpRead
	releasePreparationHttpClose
	releasePreparationHttpWrite
	releasePreparationCollect
	releasePreparationCollectResults
	releasePreparationSealPrepare
)

// One goroutine owns each token. Parallel operators receive separate tokens;
// no mutable stage or context is shared between their actual operations.
type releasePreparationDiagnostic struct {
	owner         *releaseDiagnostics
	ctx           context.Context
	stage         releasePreparationStage
	started       time.Time
	operationAt   time.Time
	nativeEpoch   uint64
	epochKnown    bool
	operatorId    uint64
	operatorKnown bool
	failureOnly   bool
	finished      bool
}

// An absent diagnostic owner allocates nothing and changes no operation.
func newReleasePreparationDiagnostic(ctx context.Context, stage releasePreparationStage, epoch uint64, known bool, operatorId uint64, operatorKnown bool) *releasePreparationDiagnostic {
	owner := releaseDiagnosticOwner(ctx)
	if owner == nil {
		return nil
	}
	now := owner.now()
	self := &releasePreparationDiagnostic{owner: owner, ctx: ctx, stage: stage, started: now, operationAt: now, nativeEpoch: epoch, epochKnown: known, operatorId: operatorId, operatorKnown: operatorKnown}
	self.offer("begin")
	return self
}

// HTTP successes are numerous during retained replay. Only an actual failed
// HTTP owner offers a record; endpoints, credentials and errors stay private.
func newReleasePreparationHttpDiagnostic(ctx context.Context, stage releasePreparationStage) *releasePreparationDiagnostic {
	owner := releaseDiagnosticOwner(ctx)
	if owner == nil {
		return nil
	}
	now := owner.now()
	return &releasePreparationDiagnostic{owner: owner, ctx: ctx, stage: stage, started: now, operationAt: now, failureOnly: true}
}

// Moving follows an actual successful call, before the next call can block.
func (self *releasePreparationDiagnostic) move(stage releasePreparationStage) {
	if self == nil {
		return
	}
	self.offer("done")
	self.stage, self.started = stage, self.owner.now()
	self.offer("begin")
}

// The caller supplies only the schedule it actually authenticated.
func (self *releasePreparationDiagnostic) setNativeEpoch(epoch uint64) {
	if self != nil {
		self.nativeEpoch, self.epochKnown = epoch, true
	}
}

// The caller supplies only its already admitted participant identity.
func (self *releasePreparationDiagnostic) setOperator(operatorId uint64) {
	if self != nil {
		self.operatorId, self.operatorKnown = operatorId, true
	}
}

// Reading only nilness cannot invoke Error, Is, As or Unwrap. The caller keeps
// the exact original error object and graph, including mixed hard failures.
func (self *releasePreparationDiagnostic) finish(err error) {
	if self == nil || self.finished || self.failureOnly && err == nil {
		return
	}
	event := "done"
	if err != nil {
		event = "failed"
	}
	self.offer(event)
	self.finished = true
}

// Closed labels and saturated durations keep every offered line bounded.
func (self *releasePreparationDiagnostic) offer(event string) {
	if self == nil {
		return
	}
	stages := [...]string{"unknown", "retained_intent", "native_snapshot", "evm_snapshot", "pinned_chains", "retained_head", "metagraph", "native_schedule", "collect_gate", "collect_reconcile", "collect_advance", "server_keys", "collect_inputs", "input_owner", "input_read", "input_decode", "input_sync", "input_reconcile", "input_detach", "input_retain", "stats_write", "stats_seal", "stats_verify", "stats_journal", "stats_snapshot", "seal_records", "seal_proofs", "seal_metadata", "seal_replay", "gather_head", "measurement", "http_open", "http_read", "http_close", "http_write", "collect", "collect_results", "seal_prepare"}
	stage := "unknown"
	if int(self.stage) < len(stages) {
		stage = stages[self.stage]
	}
	switch event {
	case "begin", "done", "failed":
	default:
		event = "unknown"
	}
	cause := "other"
	switch context.Cause(self.ctx) {
	case nil:
		cause = "none"
	case context.Canceled:
		cause = "canceled"
	case context.DeadlineExceeded:
		cause = "deadline"
	}
	epoch, operatorId := self.nativeEpoch, self.operatorId
	if !self.epochKnown {
		epoch = 0
	}
	if !self.operatorKnown {
		operatorId = 0
	}
	now := self.owner.now()
	elapsed := min(max(now.Sub(self.started).Milliseconds(), 0), (24 * time.Hour).Milliseconds())
	operationElapsed := min(max(now.Sub(self.operationAt).Milliseconds(), 0), (24 * time.Hour).Milliseconds())
	raw := []byte(fmt.Sprintf("validator diagnostic: domain=steering code=preparation_stage native_epoch=%d epoch_known=%t operator_id=%d operator_known=%t stage=%s event=%s elapsed_ms=%d operation_elapsed_ms=%d cancel_cause=%s\n", epoch, self.epochKnown, operatorId, self.operatorKnown, stage, event, elapsed, operationElapsed, cause))
	self.owner.exporter.Offer("steering", raw)
}
