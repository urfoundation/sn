//go:build linux || darwin

package validator

// Live retry, first activation and fresh startup share one journal validator.
// Only the first live terminal computes a Stats fold; every retry persists the
// authenticated exact postimage, retaining the complete admission reservation.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// Retained writer tokens make this byte comparison stable through the commit.
func checkAttemptSettlementV2LiveImages(ctx context.Context, transaction *attemptSettlementTransactionV2, batch *attemptSettlementV2Owners) error {
	for index, stats := range batch.candidates {
		encoded, err := encodeAttemptSettlementV2Engine(ctx, stats, batch.persistence.MaxSnapshotBytes)
		if err != nil {
			return err
		}
		digest, entry := attemptSettlementV2ImageHash(encoded), transaction.Snapshots[index]
		if digest != entry.PreImageHash && digest != entry.StatsHash {
			return errors.New("compact transaction live generation is not an exact owned preimage or postimage")
		}
	}
	return nil
}

// Checks callbacks' physical ledger effects before the next commit boundary.
func checkAttemptSettlementV2TransactionHeads(transaction *attemptSettlementTransactionV2, participants []AttemptSettlementRuntimeV2Participant, postimages []*StatsEngine) error {
	for index, participant := range participants {
		head, err := participant.Ledger.checkedHead()
		if err != nil {
			return err
		}
		post := postimages[index]
		if head.LastSequence != post.attemptLastAppliedSequence || head.Root != post.attemptV2.SettlementPriorRoot {
			return fmt.Errorf("compact transaction no_id %d durable head changed during commit", participant.NoID)
		}
	}
	return nil
}

// The explicit initializer never invents EMA or closes a legacy nonempty
// window. It may finish its own exact interrupted activation journal.
func initializeAttemptSettlementEpochV2(ctx context.Context, coordinator string, participants []AttemptSettlementRuntimeV2Participant, epoch uint64, authority AttemptSettlementV2Options, persistence AttemptSettlementRuntimeV2PersistenceBounds, physical attemptSettlementV2IO) (resultErr error) {
	if ctx == nil {
		return errors.New("compact initialization context is nil")
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
	batch, err := acquireAttemptSettlementV2Owners(ctx, coordinator, participants, owned, false, persistence, physical)
	if err != nil {
		return err
	}
	defer batch.release()
	if err := admitAttemptSettlementV2LiveSnapshots(ctx, batch); err != nil {
		return err
	}
	physical, err = retainAttemptSettlementV2Roots(coordinator, batch.ordered, physical)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, physical.closeRoots()) }()
	if err := batch.reserve(epoch); err != nil {
		return err
	}
	encoded, exists, err := readAttemptSettlementV2Path(attemptSettlementTransactionV2Path(coordinator), batch.persistence.MaxJournalBytes, physical)
	if err != nil {
		return err
	}
	if exists {
		transaction, preimages, postimages, err := decodeAttemptSettlementTransactionV2(ctx, encoded, batch.ordered, batch.persistence)
		if err != nil {
			return err
		}
		if transaction.Kind != "activate" || transaction.Epoch != epoch {
			return errors.New("compact initialization journal belongs to another operation")
		}
		if err := checkAttemptSettlementV2LiveImages(ctx, transaction, batch); err != nil {
			return err
		}
		if _, err := validateAttemptSettlementTransactionV2(ctx, transaction, batch.ordered, preimages, postimages, owned, batch.persistence); err != nil {
			return err
		}
		if err := finishAttemptSettlementTransactionV2(ctx, coordinator, transaction, batch, postimages, owned, physical, true); err != nil {
			return err
		}
		if err := physical.closeRoots(); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		batch.clearReservation(epoch)
		return nil
	}
	if err := validateAttemptSettlementV2Activation(ctx, batch.ordered, batch.candidates, epoch, owned, batch.persistence); err != nil {
		return err
	}
	active := 0
	for _, stats := range batch.candidates {
		if stats.attemptV2 != nil {
			active++
		}
	}
	if active != 0 {
		if active != len(batch.candidates) {
			return errors.New("compact activation is partially published without a journal")
		}
		for index, participant := range batch.ordered {
			data, exists, err := readAttemptSettlementV2Path(filepath.Join(participant.StateDir, "stats.json"), batch.persistence.MaxSnapshotBytes, physical)
			if err != nil {
				return err
			}
			live, err := encodeAttemptSettlementV2Engine(ctx, batch.candidates[index], batch.persistence.MaxSnapshotBytes)
			if err != nil || !exists || !bytes.Equal(data, live) {
				return errors.Join(errors.New("compact current activation disk image differs"), err)
			}
		}
		// A previous removal may have succeeded but its directory sync failed.
		if err := physical.removeOwnedJournal(physical.roots[coordinator], filepath.Base(attemptSettlementTransactionV2Path(coordinator))); err != nil {
			return err
		}
		if err := physical.closeRoots(); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		batch.clearReservation(epoch)
		return nil
	}
	postimages := make([]*StatsEngine, len(batch.candidates))
	for index, stats := range batch.candidates {
		stats.mu.Lock()
		post := stats.cloneStatsWithLock()
		stats.mu.Unlock()
		head, err := batch.ordered[index].Ledger.checkedHead()
		if err != nil {
			return err
		}
		activation := owned.Operators[batch.ordered[index].NoID].Expected.Activation
		// The prefix is derived from the retained, fully applied ledger, then
		// checked against independent activation; no candidate controls it.
		activation.FirstSequence, activation.PriorRoot = head.LastSequence+1, head.Root
		post.attemptV2 = &attemptStatsV2State{Activation: activation, SettlementPriorRoot: head.Root}
		postimages[index] = post
	}
	transaction, err := newAttemptSettlementTransactionV2(ctx, "activate", epoch, batch, postimages, nil, owned, physical)
	if err != nil {
		return err
	}
	encoded, err = canonicalAttemptSettlementTransactionV2(ctx, transaction, batch.persistence.MaxJournalBytes)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := physical.checkRoots(); err != nil {
		return err
	}
	if err := physical.writeImage(physical.roots[coordinator], filepath.Base(attemptSettlementTransactionV2Path(coordinator)), encoded, batch.persistence.MaxJournalBytes, physical.writeJournal); err != nil {
		return err
	}
	if err := finishAttemptSettlementTransactionV2(ctx, coordinator, transaction, batch, postimages, owned, physical, true); err != nil {
		return err
	}
	if err := physical.closeRoots(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	batch.clearReservation(epoch)
	return nil
}

// finishCurrent=false makes an unchanged refresh a genuine no-op, preserving
// any native reservation. A current retry cannot release a later generation.
func advanceAttemptSettlementEpochV2(ctx context.Context, coordinator string, participants []AttemptSettlementRuntimeV2Participant, epoch uint64, terminalBoundary AttemptBoundary, options AttemptSettlementRuntimeV2Options, physical attemptSettlementV2IO, finishCurrent bool) (result *AttemptSettlementClosureV2, resultErr error) {
	if ctx == nil {
		return nil, errors.New("compact settlement context is nil")
	}
	physical.ctx = ctx
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if err := physical.validate(); err != nil {
		return nil, err
	}
	owned, err := ownAttemptSettlementRuntimeV2Options(ctx, coordinator, participants, options)
	if err != nil {
		return nil, err
	}
	batch, err := acquireAttemptSettlementV2Owners(ctx, coordinator, participants, owned.Authority, false, owned.Persistence, physical)
	if err != nil {
		return nil, err
	}
	defer batch.release()
	if err := admitAttemptSettlementV2LiveSnapshots(ctx, batch); err != nil {
		return nil, err
	}
	allCurrent := true
	for _, stats := range batch.candidates {
		if stats.settlementEpochKnown && stats.settlementEpoch > epoch {
			return nil, errAttemptSettlementSnapshotStale
		}
		allCurrent = allCurrent && stats.settlementEpochKnown && stats.settlementEpoch == epoch
	}
	if allCurrent && !finishCurrent {
		return nil, nil
	}
	physical, err = retainAttemptSettlementV2Roots(coordinator, batch.ordered, physical)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, physical.closeRoots()) }()
	if err := batch.reserve(epoch); err != nil {
		return nil, err
	}
	encoded, exists, err := readAttemptSettlementV2Path(attemptSettlementTransactionV2Path(coordinator), batch.persistence.MaxJournalBytes, physical)
	if err != nil {
		return nil, err
	}
	if exists {
		transaction, preimages, postimages, err := decodeAttemptSettlementTransactionV2(ctx, encoded, batch.ordered, batch.persistence)
		if err != nil {
			return nil, err
		}
		if transaction.Kind != "advance" || transaction.Epoch != epoch {
			return nil, errors.New("compact settlement journal belongs to another operation")
		}
		if err := checkAttemptSettlementV2LiveImages(ctx, transaction, batch); err != nil {
			return nil, err
		}
		closure, err := validateAttemptSettlementTransactionV2(ctx, transaction, batch.ordered, preimages, postimages, owned.Authority, batch.persistence)
		if err != nil {
			return nil, err
		}
		if closure.Transitions[0].FromBoundary != terminalBoundary {
			return nil, errors.New("compact retry terminal boundary differs")
		}
		if err := finishAttemptSettlementTransactionV2(ctx, coordinator, transaction, batch, postimages, owned.Authority, physical, true); err != nil {
			return nil, err
		}
		if err := physical.closeRoots(); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch.clearReservation(epoch)
		return closure, nil
	}
	if allCurrent {
		closure, err := finishCurrentAttemptSettlementV2(ctx, coordinator, epoch, terminalBoundary, batch, owned.Authority, physical)
		if err != nil {
			return nil, err
		}
		if err := physical.closeRoots(); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch.clearReservation(epoch)
		return closure, nil
	}
	inputs := make([]AttemptSettlementV2Input, len(batch.ordered))
	heads := make([]AttemptLedgerHead, len(batch.ordered))
	// Validate every operator before the first typed stream writer is called.
	for index, participant := range batch.ordered {
		operator := owned.Authority.Operators[participant.NoID]
		head, err := validateAttemptSettlementV2Terminal(ctx, participant, batch.candidates[index], epoch, terminalBoundary, operator, batch.persistence.MaxSnapshotBytes)
		if err != nil {
			return nil, err
		}
		heads[index] = head
		raw, err := attemptSettlementV2RawMeasurement(batch.candidates[index], operator)
		if err != nil {
			return nil, err
		}
		inputs[index].PreFold = raw
	}
	for index, participant := range batch.ordered {
		operator := owned.Authority.Operators[participant.NoID]
		cut, _, err := SealAttemptCutV2(ctx, participant.Ledger, operator.Expected, operator.Policy, owned.PrivateKeys[participant.NoID], operator.Bounds, owned.Seal[participant.NoID])
		if err != nil {
			return nil, err
		}
		if cut.LastSequence != heads[index].LastSequence || cut.Root != heads[index].Root {
			return nil, errors.New("compact terminal head changed while sealing")
		}
		inputs[index].Cut = *cut
	}
	closure, err := SealAttemptSettlementBatchV2(ctx, inputs, owned.PrivateKeys, owned.Authority)
	if err != nil {
		return nil, err
	}
	postimages := make([]*StatsEngine, len(batch.ordered))
	for index, participant := range batch.ordered {
		head, err := participant.Ledger.checkedHead()
		if err != nil || head != heads[index] {
			return nil, errors.Join(errors.New("compact terminal durable prefix changed after replay"), err)
		}
		post, err := attemptSettlementV2Postimage(ctx, batch.candidates[index], closure.Transitions[index], true, batch.persistence.MaxSnapshotBytes)
		if err != nil {
			return nil, err
		}
		postimages[index] = post
	}
	transaction, err := newAttemptSettlementTransactionV2(ctx, "advance", epoch, batch, postimages, closure, owned.Authority, physical)
	if err != nil {
		return nil, err
	}
	encoded, err = canonicalAttemptSettlementTransactionV2(ctx, transaction, batch.persistence.MaxJournalBytes)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := physical.checkRoots(); err != nil {
		return nil, err
	}
	if err := physical.writeImage(physical.roots[coordinator], filepath.Base(attemptSettlementTransactionV2Path(coordinator)), encoded, batch.persistence.MaxJournalBytes, physical.writeJournal); err != nil {
		return nil, err
	}
	if err := finishAttemptSettlementTransactionV2(ctx, coordinator, transaction, batch, postimages, owned.Authority, physical, true); err != nil {
		return nil, err
	}
	if err := physical.closeRoots(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	batch.clearReservation(epoch)
	return closure, nil
}

// A removed-but-not-synced journal cannot justify resealing. Read and fully
// replay the already immutable closure, require the exact immediate successor,
// then resync both closure and journal absence before opening admission.
func finishCurrentAttemptSettlementV2(ctx context.Context, coordinator string, epoch uint64, boundary AttemptBoundary, batch *attemptSettlementV2Owners, authority AttemptSettlementV2Options, physical attemptSettlementV2IO) (*AttemptSettlementClosureV2, error) {
	if epoch == 0 {
		return nil, errors.New("compact current retry has no prior closed epoch")
	}
	encoded, exists, err := readAttemptSettlementV2Path(AttemptSettlementClosureV2Path(coordinator, epoch-1), authority.MaxClosureBytes, physical)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("compact current retry is missing its immutable closure")
	}
	closure, _, err := DecodeAttemptSettlementClosureV2(ctx, encoded, authority)
	if err != nil {
		return nil, err
	}
	if closure.Epoch+1 != epoch || closure.Transitions[0].FromBoundary != boundary {
		return nil, errors.New("compact current retry boundary differs")
	}
	for index, participant := range batch.ordered {
		stats, terminal := batch.candidates[index], closure.Transitions[index]
		if stats.attemptV2 == nil || stats.attemptV2.Terminal == nil || stats.egressGeneration != terminal.Cut.Context.EgressGeneration+1 || stats.attemptLastAppliedSequence != terminal.Cut.LastSequence || stats.attemptSettlementFirstSequence != terminal.Cut.LastSequence+1 || stats.attemptEgressFirstSequence != stats.attemptSettlementFirstSequence || len(stats.window) != 0 || len(stats.egress) != 0 {
			return nil, errors.New("compact current retry has a later or unrelated generation")
		}
		left, err := marshalAttemptSettlementV2JSON(ctx, stats.attemptV2.Terminal, batch.persistence.MaxSnapshotBytes, false, false)
		if err != nil {
			return nil, err
		}
		right, err := marshalAttemptSettlementV2JSON(ctx, terminal, batch.persistence.MaxSnapshotBytes, false, false)
		if err != nil || !bytes.Equal(left, right) {
			return nil, errors.Join(errors.New("compact current retry lost exact terminal history"), err)
		}
		if err := validateAttemptSettlementV2LedgerState(ctx, participant, stats, authority.Operators[participant.NoID], batch.persistence.MaxSnapshotBytes); err != nil {
			return nil, err
		}
		disk, exists, err := readAttemptSettlementV2Path(filepath.Join(participant.StateDir, "stats.json"), batch.persistence.MaxSnapshotBytes, physical)
		if err != nil {
			return nil, err
		}
		live, err := encodeAttemptSettlementV2Engine(ctx, stats, batch.persistence.MaxSnapshotBytes)
		if err != nil || !exists || !bytes.Equal(disk, live) {
			return nil, errors.Join(errors.New("compact current retry disk snapshot differs"), err)
		}
	}
	if err := publishAttemptSettlementClosureV2(coordinator, epoch-1, encoded, authority.MaxClosureBytes, physical); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := physical.checkRoots(); err != nil {
		return nil, err
	}
	if err := physical.removeOwnedJournal(physical.roots[coordinator], filepath.Base(attemptSettlementTransactionV2Path(coordinator))); err != nil {
		return nil, err
	}
	if err := physical.checkRoots(); err != nil {
		return nil, err
	}
	return closure, ctx.Err()
}
