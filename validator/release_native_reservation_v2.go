//go:build linux || darwin

package validator

import (
	"context"
	"errors"
	"fmt"
)

// collect owns the runtime gate. A native-only reservation has no independent
// worker: its next collect is the only operation that can finish it. Once the
// settlement closes, advance cannot take that reservation over. Cancel only
// drained, unsigned reservations so normal terminal closure can proceed; any
// signed journal remains immutable and is handled by closed-input deferral.
func (self *releaseRuntimeV2) cancelExpiredNativeReservationOwned(ctx context.Context, current *SteeringIntent, nativeEpoch uint64, snapshot *ReleaseSnapshot) error {
	if !provisionalClosedNativeInputEnabled(&self.cfg) {
		return nil
	}
	if ctx == nil || snapshot == nil || snapshot.Epoch == nil || !snapshot.Epoch.IsUint64() {
		return errors.New("expired native reservation has no current settlement")
	}
	if current != nil && (current.Status == "pending" || current.SubnetEpoch >= nativeEpoch) {
		return nil
	}
	legacy := make([]AttemptSettlementParticipant, len(self.history.participants))
	byNoID := make(map[uint64]AttemptSettlementRuntimeV2Participant, len(legacy))
	for index, participant := range self.history.participants {
		legacy[index] = AttemptSettlementParticipant{NoID: participant.NoID, StateDir: participant.StateDir, Stats: participant.Stats}
		byNoID[participant.NoID] = participant
	}
	ordered, err := orderStatsEngineOwners(legacy)
	if err != nil {
		return err
	}
	owners := make([]*statsWriteOwner, 0, len(ordered))
	defer func() {
		for index := len(owners) - 1; index >= 0; index-- {
			owners[index].release()
		}
	}()
	type cancellation struct {
		owner       *statsWriteOwner
		candidate   *StatsEngine
		noID        uint64
		nativeEpoch uint64
	}
	var cancellations []cancellation
	for _, orderedParticipant := range ordered {
		participant := byNoID[orderedParticipant.NoID]
		owner, err := participant.Stats.acquireStatsWrite(ctx, "cancel-expired-native-v2")
		if err != nil {
			return err
		}
		owners = append(owners, owner)
		candidate := owner.clone()
		if !candidate.attemptCutPending || candidate.attemptSettlementCutPending || candidate.settlementEpoch >= snapshot.Epoch.Uint64() {
			continue
		}
		cursor, known := self.history.current[participant.NoID]
		if !known || !candidate.settlementEpochKnown || candidate.attemptLedger != participant.Ledger || candidate.settlementEpoch != cursor.epoch || candidate.egressGeneration != cursor.generation || candidate.attemptSettlementFirstSequence != cursor.first || candidate.attemptEgressFirstSequence != cursor.egressFirst {
			return errors.New("expired native reservation differs from its live cursor owner")
		}
		if candidate.activeAttemptCount != 0 {
			return errAttemptCutPending
		}
		reservedEpoch, owned := self.nativeReservations[participant.NoID]
		if !owned || reservedEpoch > nativeEpoch {
			return errors.New("expired native reservation has no matching native epoch owner")
		}
		// A signed-but-unreconciled journal must finish its original custody
		// path. This cancellation cannot discard it or silently advance it.
		if self.history.inputByEpoch[reservedEpoch][participant.NoID] != nil {
			return errors.New("expired native reservation still owns a signed input")
		}
		_, err = readReleaseMeasurementInputV2Context(ctx, releaseMeasurementInputV2Path(self.cfg.StateDir, reservedEpoch, participant.NoID), self.cfg.EvidenceV2.Bounds.MaxInputJournalBytes, releaseMeasurementInputV2ReadHooks{})
		if !releaseMeasurementInputV2InitialAbsence(err) {
			return errors.Join(errors.New("expired native reservation has a retained input requiring reconciliation"), err)
		}
		cancellations = append(cancellations, cancellation{owner, candidate, participant.NoID, reservedEpoch})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, item := range cancellations {
		item.candidate.attemptCutPending = false
		item.owner.publish(item.candidate)
		delete(self.nativeReservations, item.noID)
		fmt.Printf("release steer: provisional native epoch %d cancelled drained unsigned reservation no_id %d settlement %d; active settlement %d; no native submission\n", item.nativeEpoch, item.noID, item.candidate.settlementEpoch, snapshot.Epoch.Uint64())
	}
	return nil
}
