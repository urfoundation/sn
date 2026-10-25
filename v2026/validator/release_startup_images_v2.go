//go:build linux || darwin

// Startup admits every actual and journal-contained image against the complete
// independently reconstructed history before a recovery owner may mutate disk.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
)

// The latest ordinary journal has two legal images: its exact independent
// predecessor cursor and its one successor. An older native generation is not
// rescued by replaying an arbitrary snapshot-selected prefix of history.
func (self *releaseEvidenceV2StartupHistory) matchImage(ctx context.Context, participant AttemptSettlementRuntimeV2Participant, stats *StatsEngine, cursor releaseEvidenceV2StartupCursor, inactive bool) error {
	if stats == nil || !stats.settlementEpochKnown || stats.settlementEpoch != cursor.epoch || stats.egressGeneration != cursor.generation || stats.attemptSettlementFirstSequence != cursor.first || stats.attemptEgressFirstSequence != cursor.egressFirst || stats.attemptLastAppliedSequence < cursor.lastSequence || stats.attemptLedger != nil || stats.activeAttemptCount != 0 {
		return errors.New("startup statistics image differs from the independently replayed current cursor")
	}
	initial := self.initial[participant.NoID].InitialCut
	if inactive {
		if stats.attemptV2 != nil || cursor.terminal != nil || cursor.epoch != initial.Boundary.SettlementEpoch || cursor.first != initial.FirstSequence || cursor.egressFirst != initial.EgressFirstSequence || cursor.generation != initial.EgressGeneration || cursor.priorRoot != initial.PriorRoot {
			return errors.New("startup inactive image is not the independently pinned initial boundary")
		}
	} else if stats.attemptV2 == nil || stats.attemptV2.Activation != initial.Activation || stats.attemptV2.SettlementPriorRoot != cursor.priorRoot {
		return errors.New("startup statistics image differs from retained activation or settlement prefix")
	}
	var legacy *AttemptSettlementTransition
	if self.activationHistory != nil {
		for _, transition := range self.activationHistory.Transitions {
			if transition.Identity.NoID == participant.NoID {
				legacy = transition
				break
			}
		}
	}
	if err := matchReleaseActivationHistoryV2Transition(legacy, stats.settlementTransition); err != nil {
		return err
	}
	if len(stats.ema) != len(stats.emaPPM) || !slices.Equal(sortedAttemptEMAQualities(stats.emaPPM), cursor.prior) {
		return errors.New("startup statistics image differs from its exact authenticated prior EMA")
	}
	if !inactive {
		limit := self.cfg.EvidenceV2.Bounds.Persistence.MaxSnapshotBytes
		want, err := marshalAttemptSettlementV2JSON(ctx, cursor.terminal, limit, false, false)
		if err != nil {
			return err
		}
		actual, err := marshalAttemptSettlementV2JSON(ctx, stats.attemptV2.Terminal, limit, false, false)
		if err != nil || !bytes.Equal(actual, want) {
			return errors.Join(errors.New("startup statistics image selects another carried terminal"), err)
		}
	}
	return ctx.Err()
}

// A previously absent checkpoint can only initialize the real empty ledger.
// Nonempty legacy history must retain its original drained snapshot and exact
// signed transition; replayed PostFold is never pasted into invented Stats.
func (self *releaseEvidenceV2StartupHistory) pristine(ctx context.Context, participant AttemptSettlementRuntimeV2Participant) (*StatsEngine, error) {
	cursor := self.current[participant.NoID]
	if self.activationHistory != nil || self.terminal != nil || len(self.lastOrdinary) != 0 || cursor.first != 1 || cursor.egressFirst != 1 || cursor.generation != 1 || cursor.lastSequence != 0 || cursor.priorRoot != zeroAttemptHash() || len(cursor.prior) != 0 {
		return nil, errors.New("startup cannot invent a missing nonempty legacy or current checkpoint")
	}
	head, err := participant.Ledger.checkedHead()
	if err != nil || head.LastSequence != 0 || head.Root != zeroAttemptHash() {
		return nil, errors.Join(errors.New("startup missing initial checkpoint has a nonempty actual ledger"), err)
	}
	stats := NewStatsEngine(participant.Stats.cfg)
	stats.settlementEpochKnown, stats.settlementEpoch = true, cursor.epoch
	stats.egressGeneration, stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence = 1, 1, 1
	return stats, ctx.Err()
}

// This entire function is read-only. It validates original, pre- and post-
// images even if the currently visible disk happens to contain a good member.
// Actual lower recovery repeats its signed replay and exact-byte checks.
func (self *releaseEvidenceV2StartupHistory) preflightImages(ctx context.Context) (initial []*StatsEngine, resultErr error) {
	bounds := self.cfg.EvidenceV2.Bounds
	if self.journal != nil {
		for index, participant := range self.participants {
			preCursor := self.current[participant.NoID]
			if self.journal.Kind == "advance" {
				preCursor = self.terminalBefore[participant.NoID]
			}
			if err := self.matchImage(ctx, participant, self.journalPreimages[index], preCursor, self.journal.Kind == "activate"); err != nil {
				return nil, fmt.Errorf("startup journal preimage no_id %d: %w", participant.NoID, err)
			}
			if err := self.matchImage(ctx, participant, self.journalPostimages[index], self.current[participant.NoID], false); err != nil {
				return nil, fmt.Errorf("startup journal postimage no_id %d: %w", participant.NoID, err)
			}
			entry := self.journal.Snapshots[index]
			if len(entry.OriginalJSON) != 0 {
				original, err := decodeAttemptSettlementV2Stats(ctx, entry.OriginalJSON, participant.Stats.cfg, bounds.Persistence.MaxSnapshotBytes)
				if err != nil {
					return nil, err
				}
				if err := self.matchImage(ctx, participant, original, preCursor, self.journal.Kind == "activate"); err != nil {
					return nil, fmt.Errorf("startup journal original no_id %d: %w", participant.NoID, err)
				}
			}
			image := self.images.paths[filepath.Join(participant.StateDir, "stats.json")]
			if !image.present && len(entry.OriginalJSON) != 0 || image.present && !bytes.Equal(image.encoded, entry.OriginalJSON) && !bytes.Equal(image.encoded, entry.PreImageJSON) && !bytes.Equal(image.encoded, entry.StatsJSON) {
				return nil, errors.New("startup visible checkpoint is not an exact independently admitted journal image")
			}
		}
		options, err := self.recoveryAuthority(ctx, "preflight-journal")
		if err != nil {
			return nil, err
		}
		// The real journal verifier can mutate a private activation preimage;
		// no caller or later exact-byte admission relies on that decoded object.
		if _, err := validateAttemptSettlementTransactionV2(ctx, self.journal, self.participants, self.journalPreimages, self.journalPostimages, options, bounds.Persistence); err != nil {
			return nil, err
		}
		if err := checkAttemptSettlementV2TransactionHeads(self.journal, self.participants, self.journalPostimages); err != nil {
			return nil, err
		}
		for index, participant := range self.participants {
			if err := preflightAttemptSettlementV2Pending(ctx, participant, self.journalPostimages[index], options.Operators[participant.NoID]); err != nil {
				return nil, err
			}
		}
		return nil, ctx.Err()
	}
	candidates := make([]*StatsEngine, len(self.participants))
	inactiveCount := 0
	for index, participant := range self.participants {
		image := self.images.paths[filepath.Join(participant.StateDir, "stats.json")]
		var stats *StatsEngine
		var err error
		if image.present {
			stats, err = decodeAttemptSettlementV2Stats(ctx, image.encoded, participant.Stats.cfg, bounds.Persistence.MaxSnapshotBytes)
		} else {
			stats, err = self.pristine(ctx, participant)
		}
		if err != nil {
			return nil, err
		}
		inactive := stats.attemptV2 == nil
		cursor := self.current[participant.NoID]
		if inactive {
			inactiveCount++
		} else if before, exists := self.lastOrdinaryBefore[participant.NoID]; exists && stats.egressGeneration == before.generation {
			cursor = before
		}
		if err := self.matchImage(ctx, participant, stats, cursor, inactive); err != nil {
			return nil, fmt.Errorf("startup visible image no_id %d: %w", participant.NoID, err)
		}
		candidates[index] = stats
	}
	if inactiveCount != 0 && inactiveCount != len(candidates) {
		return nil, errors.New("startup partial activation has no complete durable transaction")
	}
	options, err := self.recoveryAuthority(ctx, "preflight-images")
	if err != nil {
		return nil, err
	}
	if inactiveCount != 0 {
		if self.terminal != nil || len(self.lastOrdinary) != 0 {
			return nil, errors.New("startup inactive checkpoints precede already durable compact history")
		}
		if err := validateAttemptSettlementV2Activation(ctx, self.participants, candidates, candidates[0].settlementEpoch, options, bounds.Persistence); err != nil {
			return nil, err
		}
		return candidates, ctx.Err()
	}
	for index, participant := range self.participants {
		operator := options.Operators[participant.NoID]
		if err := validateAttemptSettlementV2LedgerState(ctx, participant, candidates[index], operator, bounds.Persistence.MaxSnapshotBytes); err != nil {
			return nil, err
		}
		if err := preflightAttemptSettlementV2Pending(ctx, participant, candidates[index], operator); err != nil {
			return nil, err
		}
	}
	return nil, ctx.Err()
}

// Recovery's terminal verifier needs the genuine pre-terminal cursor, not the
// latest native cursor or a value projected from its own candidate snapshot.
func (self *releaseEvidenceV2StartupHistory) recoveryAuthority(ctx context.Context, purpose string) (AttemptSettlementV2Options, error) {
	if self.terminal != nil {
		return self.authority(ctx, self.terminalBefore, self.terminal.Transitions[0].FromBoundary, 0, purpose)
	}
	cursors := make(map[uint64]releaseEvidenceV2StartupCursor, len(self.participants))
	for _, participant := range self.participants {
		initial := self.initial[participant.NoID].InitialCut
		cursors[participant.NoID] = releaseEvidenceV2StartupCursor{epoch: initial.Boundary.SettlementEpoch, first: initial.FirstSequence, egressFirst: initial.EgressFirstSequence, generation: initial.EgressGeneration, priorRoot: initial.PriorRoot}
	}
	return self.authority(ctx, cursors, self.initial[self.participants[0].NoID].InitialCut.Boundary, 0, purpose)
}
