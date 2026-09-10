//go:build linux || darwin

package validator

// Recovery belongs to fresh, unattached engines before workers exist. It
// authenticates the full batch and local signed suffix before any recovery
// write, then attaches all private candidates and publishes them together.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
)

// This bounded preflight covers all participants before recovery appends even
// one validator-error terminal. Root-owned startup ledgers admit no workers.
func preflightAttemptSettlementV2Pending(ctx context.Context, participant AttemptSettlementRuntimeV2Participant, stats *StatsEngine, operator AttemptSettlementV2OperatorOptions) error {
	head, err := participant.Ledger.checkedHead()
	if err != nil {
		return err
	}
	if head.LastSequence == ^uint64(0) || head.LastSequence+1 < stats.attemptSettlementFirstSequence || head.LastSequence+1-stats.attemptSettlementFirstSequence > operator.Bounds.Records.MaxItems {
		return errors.New("compact startup suffix exceeds its existing record bound")
	}
	remaining := operator.Bounds.Records.MaxItems - (head.LastSequence + 1 - stats.attemptSettlementFirstSequence)
	depth, err := attemptCutV2PolicyDepth(operator.Expected, operator.Policy)
	if err != nil {
		return err
	}
	vpk, err := canonicalAttemptHex32("compact startup validator", operator.Expected.Identity.ValidatorVPK, false)
	if err != nil {
		return err
	}
	// Authenticate every uncheckpointed row for every participant before any
	// recovery append, snapshot replacement or absence durability operation.
	if stats.attemptLastAppliedSequence < head.LastSequence {
		if err := participant.Ledger.Walk(ctx, stats.attemptLastAppliedSequence+1, head.LastSequence, func(record AttemptRecord) error {
			if record.M != depth || record.Boundary.SettlementEpoch != stats.settlementEpoch {
				return errors.New("compact startup unapplied policy or epoch differs")
			}
			return VerifyAttemptRecord(&record, operator.Expected.Identity, vpk[:], operator.Measurement.Replay.ServerKeys)
		}); err != nil {
			return err
		}
	}
	if err := participant.Ledger.begin(ctx); err != nil {
		return err
	}
	defer participant.Ledger.active.Done()
	return participant.Ledger.disk.Pending(ctx, func(record AttemptRecord) error {
		if remaining == 0 || record.M != depth || record.Boundary.SettlementEpoch != stats.settlementEpoch || record.Sequence < stats.attemptSettlementFirstSequence {
			return errors.New("compact startup pending recovery exceeds its bound, policy or epoch")
		}
		if err := VerifyAttemptRecord(&record, operator.Expected.Identity, vpk[:], operator.Measurement.Replay.ServerKeys); err != nil {
			return err
		}
		remaining--
		return nil
	})
}

// Replays the genuine uncheckpointed local suffix into a private candidate.
// Unlike the generic attachment API, this helper performs no Stats write; the
// coordinator first validates every candidate and then persists the whole set.
func prepareAttemptSettlementV2Attachment(ctx context.Context, participant AttemptSettlementRuntimeV2Participant, candidate *StatsEngine, operator AttemptSettlementV2OperatorOptions, maxBytes uint64) error {
	if candidate.attemptLedger != nil && candidate.attemptLedger != participant.Ledger {
		return errors.New("compact startup candidate is attached to another ledger")
	}
	candidate.attemptLedger = participant.Ledger
	if err := participant.Ledger.RecoverPendingContext(ctx, func(AttemptRecord) error { return nil }); err != nil {
		return err
	}
	head, err := participant.Ledger.checkedHead()
	if err != nil {
		return err
	}
	if head.LastSequence == ^uint64(0) || candidate.attemptLastAppliedSequence > head.LastSequence || head.LastSequence+1-candidate.attemptSettlementFirstSequence > operator.Bounds.Records.MaxItems {
		return errors.New("compact startup recovered head differs or exceeds its bound")
	}
	if candidate.attemptLastAppliedSequence < head.LastSequence {
		if err := participant.Ledger.Walk(ctx, candidate.attemptLastAppliedSequence+1, head.LastSequence, func(record AttemptRecord) error {
			if record.Boundary.SettlementEpoch != candidate.settlementEpoch {
				return errors.New("compact startup unapplied row belongs to another epoch")
			}
			candidate.mu.Lock()
			defer candidate.mu.Unlock()
			if record.Disposition != AttemptDispositionPending {
				if err := candidate.validateAttemptStatsWithLock(&record); err != nil {
					return err
				}
				candidate.applyAttemptStatsWithLock(&record)
			}
			candidate.attemptLastAppliedSequence = record.Sequence
			return nil
		}); err != nil {
			return err
		}
	}
	return validateAttemptSettlementV2LedgerState(ctx, participant, candidate, operator, maxBytes)
}

// Before the first compact terminal, any carried legacy prior is still
// authenticated as a complete batch against the actual activation prefix.
// Current raw observations after that prefix are not discarded or refolded.
func validateAttemptSettlementV2CarriedLegacy(ctx context.Context, participants []AttemptSettlementRuntimeV2Participant, statsTs []*StatsEngine, authority AttemptSettlementV2Options, persistence AttemptSettlementRuntimeV2PersistenceBounds) error {
	closure := &AttemptSettlementClosure{Schema: AttemptSettlementClosureSchema}
	serverKeys := map[uint64]map[byte]ed25519.PublicKey{}
	for index, participant := range participants {
		stats, expected := statsTs[index], authority.Operators[participant.NoID]
		state := stats.attemptV2
		legacy := stats.settlementTransition
		if state == nil || state.Activation != expected.Expected.Activation || state.Terminal != nil || stats.settlementEpoch != state.Activation.Domain.ActivationEpoch {
			return errors.New("compact first-window activation history differs")
		}
		if legacy == nil {
			if len(stats.ema) != 0 || len(stats.emaPPM) != 0 || state.Activation.FirstSequence != 1 || state.Activation.PriorRoot != zeroAttemptHash() {
				return errors.New("compact first-window prior has no signed activation history")
			}
			continue
		}
		cut := legacy.PreFold.AttemptCut
		if legacy.Identity != expected.Expected.Identity || legacy.ToEpoch != stats.settlementEpoch || legacy.PreFold.Config != expected.Measurement.ExpectedConfig || cut == nil || cut.LastSequence == ^uint64(0) || cut.LastSequence+1 != state.Activation.FirstSequence || cut.Root != state.Activation.PriorRoot || !slices.Equal(legacy.PostFold, sortedAttemptEMAQualities(stats.emaPPM)) {
			return errors.New("compact first-window legacy prior or prefix differs")
		}
		if len(closure.Transitions) == 0 {
			closure.Epoch = legacy.FromBoundary.SettlementEpoch
		}
		closure.Transitions = append(closure.Transitions, legacy)
		serverKeys[participant.NoID] = expected.Measurement.Replay.ServerKeys
	}
	if len(closure.Transitions) != 0 {
		if len(closure.Transitions) != len(participants) {
			return errors.New("compact first-window legacy census omits an operator")
		}
		encoded, err := marshalAttemptSettlementV2JSON(ctx, closure, persistence.MaxJournalBytes, false, true)
		if err != nil {
			return err
		}
		if _, err := DecodeAttemptSettlementClosureWithServerKeys(encoded, serverKeys); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// Exact persisted members must match one immutable complete closure. Full
// replay uses independent expected contexts; candidate fields never construct
// policy, activation, key history, boundary or the configured operator census.
func validateAttemptSettlementV2Carried(ctx context.Context, coordinator string, participants []AttemptSettlementRuntimeV2Participant, statsTs []*StatsEngine, authority AttemptSettlementV2Options, persistence AttemptSettlementRuntimeV2PersistenceBounds, physical attemptSettlementV2IO) error {
	terminalCount := 0
	var epoch uint64
	for index, participant := range participants {
		stats, operator := statsTs[index], authority.Operators[participant.NoID]
		if stats.attemptV2 == nil || stats.attemptV2.Activation != operator.Expected.Activation {
			return errors.New("compact recovery snapshot is missing or mismatches authenticated activation")
		}
		if index != 0 && stats.settlementEpoch != epoch {
			return errors.New("compact recovery participant epochs differ without a journal")
		}
		epoch = stats.settlementEpoch
		if stats.attemptV2.Terminal != nil {
			terminalCount++
		}
		if _, err := countAttemptSettlementV2Engine(ctx, stats, persistence.MaxSnapshotBytes); err != nil {
			return err
		}
		if err := validateAttemptStatsV2Snapshot(stats.snapshotStats()); err != nil {
			return err
		}
	}
	if terminalCount == 0 {
		if err := validateAttemptSettlementV2CarriedLegacy(ctx, participants, statsTs, authority, persistence); err != nil {
			return err
		}
	} else {
		if terminalCount != len(participants) || epoch == 0 {
			return errors.New("compact recovery terminal census is incomplete")
		}
		encoded, exists, err := readAttemptSettlementV2Path(AttemptSettlementClosureV2Path(coordinator, epoch-1), authority.MaxClosureBytes, physical)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("compact recovery immutable terminal is missing")
		}
		closure, _, err := DecodeAttemptSettlementClosureV2(ctx, encoded, authority)
		if err != nil {
			return err
		}
		if closure.Epoch+1 != epoch || len(closure.Transitions) != len(participants) {
			return errors.New("compact recovery immutable terminal epoch or census differs")
		}
		for index, stats := range statsTs {
			left, err := marshalAttemptSettlementV2JSON(ctx, stats.attemptV2.Terminal, persistence.MaxSnapshotBytes, false, false)
			if err != nil {
				return err
			}
			right, err := marshalAttemptSettlementV2JSON(ctx, closure.Transitions[index], persistence.MaxSnapshotBytes, false, false)
			if err != nil || !bytes.Equal(left, right) {
				return errors.Join(errors.New("compact recovery snapshot terminal differs from immutable closure"), err)
			}
		}
	}
	for index, participant := range participants {
		if err := validateAttemptSettlementV2LedgerState(ctx, participant, statsTs[index], authority.Operators[participant.NoID], persistence.MaxSnapshotBytes); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// Journal replay is completed before any candidate attachment. Candidate
// attachment may recover real pending records and persist their new cursor,
// but no public engine becomes live until every candidate has succeeded.
func recoverAttemptSettlementEpochV2(ctx context.Context, coordinator string, participants []AttemptSettlementRuntimeV2Participant, authority AttemptSettlementV2Options, persistence AttemptSettlementRuntimeV2PersistenceBounds, physical attemptSettlementV2IO) (resultErr error) {
	if ctx == nil {
		return errors.New("compact recovery context is nil")
	}
	if err := persistence.validate(); err != nil {
		return err
	}
	if err := validateAttemptSettlementV2MetadataLimit(authority.MaxClosureBytes); err != nil {
		return err
	}
	physical.ctx = ctx
	defer func() { resultErr = errors.Join(resultErr, ctx.Err()) }()
	if err := physical.validate(); err != nil {
		return err
	}
	owned, err := ownAttemptSettlementV2Options(ctx, authority)
	if err != nil {
		return err
	}
	batch, err := acquireAttemptSettlementV2Owners(ctx, coordinator, participants, owned, true, persistence, physical)
	if err != nil {
		return err
	}
	defer batch.release()
	physical, err = retainAttemptSettlementV2Roots(coordinator, batch.ordered, physical)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, physical.closeRoots()) }()
	encoded, exists, err := readAttemptSettlementV2Path(attemptSettlementTransactionV2Path(coordinator), persistence.MaxJournalBytes, physical)
	if err != nil {
		return err
	}
	var candidates []*StatsEngine
	if exists {
		transaction, preimages, postimages, err := decodeAttemptSettlementTransactionV2(ctx, encoded, batch.ordered, persistence)
		if err != nil {
			return err
		}
		if _, err := validateAttemptSettlementTransactionV2(ctx, transaction, batch.ordered, preimages, postimages, owned, persistence); err != nil {
			return err
		}
		// All real stream/activation/cursor checks above precede even the first
		// recovery snapshot replacement. Public engines remain untouched.
		if err := finishAttemptSettlementTransactionV2(ctx, coordinator, transaction, batch, postimages, owned, physical, false); err != nil {
			return err
		}
		candidates = postimages
	} else {
		candidates = make([]*StatsEngine, len(batch.ordered))
		for index, participant := range batch.ordered {
			data, exists, err := readAttemptSettlementV2Path(filepath.Join(participant.StateDir, "stats.json"), persistence.MaxSnapshotBytes, physical)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("compact recovery no_id %d snapshot is missing; explicit initialization is required", participant.NoID)
			}
			candidate, err := decodeAttemptSettlementV2Stats(ctx, data, participant.Stats.cfg, persistence.MaxSnapshotBytes)
			if err != nil {
				return err
			}
			candidates[index] = candidate
		}
		if err := validateAttemptSettlementV2Carried(ctx, coordinator, batch.ordered, candidates, owned, persistence, physical); err != nil {
			return err
		}
	}
	for index, participant := range batch.ordered {
		if err := preflightAttemptSettlementV2Pending(ctx, participant, candidates[index], owned.Operators[participant.NoID]); err != nil {
			return err
		}
	}
	for index, participant := range batch.ordered {
		if err := prepareAttemptSettlementV2Attachment(ctx, participant, candidates[index], owned.Operators[participant.NoID], persistence.MaxSnapshotBytes); err != nil {
			return err
		}
	}
	heads := make([]AttemptLedgerHead, len(batch.ordered))
	for index, participant := range batch.ordered {
		head, err := participant.Ledger.checkedHead()
		if err != nil || head.LastSequence != candidates[index].attemptLastAppliedSequence {
			return errors.Join(errors.New("compact startup candidate lost its applied head"), err)
		}
		heads[index] = head
	}
	checkHeads := func() error {
		if err := physical.checkRoots(); err != nil {
			return err
		}
		for index, participant := range batch.ordered {
			head, err := participant.Ledger.checkedHead()
			if err != nil || head != heads[index] {
				return errors.Join(errors.New("compact startup ledger changed during persistence"), err)
			}
		}
		return ctx.Err()
	}
	if err := checkHeads(); err != nil {
		return err
	}
	for _, candidate := range candidates {
		if _, err := countAttemptSettlementV2Engine(ctx, candidate, persistence.MaxSnapshotBytes); err != nil {
			return err
		}
	}
	if err := physical.removeOwnedJournal(physical.roots[coordinator], filepath.Base(attemptSettlementTransactionV2Path(coordinator))); err != nil {
		return err
	}
	for index, participant := range batch.ordered {
		if err := checkHeads(); err != nil {
			return err
		}
		data, err := encodeAttemptSettlementV2Engine(ctx, candidates[index], persistence.MaxSnapshotBytes)
		if err != nil {
			return err
		}
		if err := physical.writeImage(physical.roots[participant.StateDir], "stats.json", data, persistence.MaxSnapshotBytes, physical.writeSnapshot); err != nil {
			return err
		}
	}
	if err := checkHeads(); err != nil {
		return err
	}
	if err := physical.closeRoots(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	batch.candidates = candidates
	for _, candidate := range candidates {
		candidate.attemptCutPending, candidate.attemptSettlementCutPending = false, false
	}
	batch.publish()
	return nil
}
