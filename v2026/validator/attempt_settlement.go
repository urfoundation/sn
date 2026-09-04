package validator

// Settlement advancement is a validator-wide transaction. All operator
// engines stop admitting attempts, drain their active trails, prepare their
// next snapshots, and publish one recovery journal before any stats file is
// replaced. A restart replays that journal before loading an operator, so a
// crash between filesystem renames cannot split operators across epochs.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"

	"github.com/urnetwork/connect/v2026"
)

const attemptSettlementTransactionSchema = "urnetwork-validator-settlement-transaction-v1"

// AttemptSettlementParticipant identifies one isolated operator state. Stats
// is required for advancement and may be nil for startup recovery.
type AttemptSettlementParticipant struct {
	NoID     uint64
	StateDir string
	Stats    *StatsEngine
}

type attemptSettlementSnapshot struct {
	NoID      uint64 `json:"no_id"`
	StatsPath string `json:"stats_path"`
	StatsJSON []byte `json:"stats_json"`
}

type attemptSettlementTransaction struct {
	Schema    string                      `json:"schema"`
	Epoch     uint64                      `json:"epoch"`
	Snapshots []attemptSettlementSnapshot `json:"snapshots"`
}

type attemptSettlementCandidate struct {
	window                  map[connect.Id]*ProviderWindow
	ema                     map[connect.Id]float64
	emaPPM                  map[connect.Id]uint32
	egress                  map[connect.Id]map[[32]byte]bool
	egressGeneration        uint64
	settlementEpoch         uint64
	settlementEpochKnown    bool
	settlementFirstSequence uint64
	egressFirstSequence     uint64
	transition              *AttemptSettlementTransition
}

func attemptSettlementTransactionPath(coordinatorStateDir string) string {
	return filepath.Join(coordinatorStateDir, "settlement-transaction.json")
}

func (self *StatsEngine) requiresSettlementAdvance(epoch uint64) bool {
	self.mu.Lock()
	defer self.mu.Unlock()
	return !self.settlementEpochKnown || self.settlementEpoch != epoch
}

func canonicalAttemptSettlementTransaction(transaction *attemptSettlementTransaction) ([]byte, error) {
	encoded, err := json.Marshal(transaction)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func validateAttemptSettlementParticipants(participants []AttemptSettlementParticipant, requireStats bool) ([]AttemptSettlementParticipant, error) {
	ordered := append([]AttemptSettlementParticipant(nil), participants...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].NoID < ordered[j].NoID })
	paths := map[string]bool{}
	for index, participant := range ordered {
		if participant.NoID == 0 || participant.StateDir == "" || !filepath.IsAbs(participant.StateDir) || (index > 0 && participant.NoID == ordered[index-1].NoID) {
			return nil, errors.New("settlement participants are incomplete or duplicated")
		}
		if requireStats && participant.Stats == nil {
			return nil, fmt.Errorf("settlement participant no_id %d has no statistics engine", participant.NoID)
		}
		path := filepath.Clean(filepath.Join(participant.StateDir, "stats.json"))
		if paths[path] {
			return nil, errors.New("settlement participants share a statistics path")
		}
		paths[path] = true
	}
	if len(ordered) == 0 {
		return nil, errors.New("settlement transaction has no participants")
	}
	return ordered, nil
}

func decodeAttemptSettlementTransaction(encoded []byte, participants []AttemptSettlementParticipant) (*attemptSettlementTransaction, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var transaction attemptSettlementTransaction
	if err := decoder.Decode(&transaction); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("settlement transaction contains trailing JSON")
	}
	canonical, err := canonicalAttemptSettlementTransaction(&transaction)
	if err != nil || !bytes.Equal(encoded, canonical) {
		return nil, errors.New("settlement transaction is not canonical")
	}
	if transaction.Schema != attemptSettlementTransactionSchema || len(transaction.Snapshots) != len(participants) {
		return nil, errors.New("settlement transaction identity or coverage differs")
	}
	for index, snapshot := range transaction.Snapshots {
		expectedPath := filepath.Clean(filepath.Join(participants[index].StateDir, "stats.json"))
		if snapshot.NoID != participants[index].NoID || snapshot.StatsPath != expectedPath || len(snapshot.StatsJSON) == 0 {
			return nil, fmt.Errorf("settlement snapshot %d does not match configured participant", index)
		}
		var stats statsSnapshot
		statsDecoder := json.NewDecoder(bytes.NewReader(snapshot.StatsJSON))
		statsDecoder.DisallowUnknownFields()
		if err := statsDecoder.Decode(&stats); err != nil || stats.SettlementEpoch == nil || *stats.SettlementEpoch != transaction.Epoch {
			return nil, fmt.Errorf("settlement snapshot %d has invalid epoch state", index)
		}
		canonicalStats, err := encodeStatsSnapshot(stats)
		if err != nil || !bytes.Equal(canonicalStats, snapshot.StatsJSON) {
			return nil, fmt.Errorf("settlement snapshot %d statistics are not canonical", index)
		}
		if stats.SettlementTransition != nil {
			if err := VerifyAttemptSettlementTransition(stats.SettlementTransition); err != nil {
				return nil, fmt.Errorf("settlement snapshot %d transition: %w", index, err)
			}
		}
	}
	return &transaction, nil
}

func removeAttemptSettlementTransaction(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func finishCurrentAttemptSettlementTransaction(path string, epoch uint64, participants []AttemptSettlementParticipant, removeTransaction func(string) error) error {
	if removeTransaction == nil {
		return errors.New("settlement transaction remover is nil")
	}
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	transaction, err := decodeAttemptSettlementTransaction(encoded, participants)
	if err != nil || transaction.Epoch != epoch {
		return errors.New("persisted settlement transaction differs from current memory")
	}
	for index, participant := range participants {
		current, err := encodeStatsSnapshot(participant.Stats.snapshotWithLock())
		if err != nil || !bytes.Equal(current, transaction.Snapshots[index].StatsJSON) {
			return fmt.Errorf("no_id %d current statistics differ from the pending settlement transaction", participant.NoID)
		}
	}
	return removeTransaction(path)
}

// RecoverAttemptSettlementEpoch replays a prepared multi-operator transaction
// before any StatsEngine is loaded. The configured participant set prevents a
// corrupted journal from selecting arbitrary filesystem targets.
func RecoverAttemptSettlementEpoch(coordinatorStateDir string, participants []AttemptSettlementParticipant) error {
	return recoverAttemptSettlementEpochWithRemove(coordinatorStateDir, participants, removeAttemptSettlementTransaction)
}

func recoverAttemptSettlementEpochWithRemove(coordinatorStateDir string, participants []AttemptSettlementParticipant, removeTransaction func(string) error) error {
	ordered, err := validateAttemptSettlementParticipants(participants, false)
	if err != nil {
		return err
	}
	if removeTransaction == nil {
		return errors.New("settlement transaction remover is nil")
	}
	path := attemptSettlementTransactionPath(coordinatorStateDir)
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("settlement transaction is not a private regular file")
	}
	transaction, err := decodeAttemptSettlementTransaction(encoded, ordered)
	if err != nil {
		return err
	}
	for _, snapshot := range transaction.Snapshots {
		if err := atomicStateWrite(snapshot.StatsPath, snapshot.StatsJSON, 0o600); err != nil {
			return err
		}
	}
	return removeTransaction(path)
}

func advanceAttemptSettlementEpochWithWrite(coordinatorStateDir string, epoch uint64, terminalBoundary AttemptBoundary, participants []AttemptSettlementParticipant, writeSnapshot func(string, []byte) error) error {
	return advanceAttemptSettlementEpochWithIO(coordinatorStateDir, epoch, terminalBoundary, participants, writeSnapshot, removeAttemptSettlementTransaction)
}

func advanceAttemptSettlementEpochWithIO(coordinatorStateDir string, epoch uint64, terminalBoundary AttemptBoundary, participants []AttemptSettlementParticipant, writeSnapshot func(string, []byte) error, removeTransaction func(string) error) error {
	ordered, err := validateAttemptSettlementParticipants(participants, true)
	if err != nil {
		return err
	}
	if writeSnapshot == nil {
		return errors.New("settlement snapshot writer is nil")
	}
	if removeTransaction == nil {
		return errors.New("settlement transaction remover is nil")
	}
	for _, participant := range ordered {
		participant.Stats.mu.Lock()
	}
	defer func() {
		for index := len(ordered) - 1; index >= 0; index-- {
			ordered[index].Stats.mu.Unlock()
		}
	}()
	allCurrent := true
	initializing := false
	transitioning := false
	var priorEpoch uint64
	for _, participant := range ordered {
		stats := participant.Stats
		if !stats.settlementEpochKnown {
			if len(stats.window) != 0 || len(stats.ema) != 0 || len(stats.emaPPM) != 0 || len(stats.egress) != 0 || stats.attemptLedger == nil || stats.attemptLedger.LastSequence() != 0 {
				return fmt.Errorf("no_id %d has non-pristine statistics without settlement ownership", participant.NoID)
			}
			initializing = true
			allCurrent = false
		} else if stats.settlementEpoch == epoch {
			// Already current after an idempotent retry.
		} else {
			if stats.settlementEpoch == ^uint64(0) || stats.settlementEpoch+1 != epoch {
				return fmt.Errorf("no_id %d cannot advance settlement epoch %d from %d", participant.NoID, epoch, stats.settlementEpoch)
			}
			if !transitioning {
				priorEpoch = stats.settlementEpoch
			}
			if priorEpoch != stats.settlementEpoch {
				return errors.New("settlement participants do not share one prior epoch")
			}
			transitioning = true
			allCurrent = false
		}
		stats.attemptCutPending = true
	}
	if initializing && transitioning {
		return errors.New("settlement participants mix pristine and prior-epoch state")
	}
	if allCurrent {
		if err := finishCurrentAttemptSettlementTransaction(attemptSettlementTransactionPath(coordinatorStateDir), epoch, ordered, removeTransaction); err != nil {
			return err
		}
		for _, participant := range ordered {
			participant.Stats.attemptCutPending = false
		}
		return nil
	}
	for _, participant := range ordered {
		if participant.Stats.activeAttemptCount != 0 {
			return errAttemptCutPending
		}
	}
	if transitioning {
		if terminalBoundary.SettlementEpoch != priorEpoch || epoch != priorEpoch+1 {
			return errors.New("settlement terminal boundary has the wrong epoch")
		}
		if err := validateAttemptBoundary(terminalBoundary); err != nil {
			return err
		}
	}

	transaction := &attemptSettlementTransaction{Schema: attemptSettlementTransactionSchema, Epoch: epoch}
	candidates := make([]attemptSettlementCandidate, len(ordered))
	for index, participant := range ordered {
		stats := participant.Stats
		if stats.settlementEpoch == epoch {
			return errors.New("settlement participant set is already partially advanced in memory")
		}
		candidate := attemptSettlementCandidate{
			window: cloneProviderWindows(stats.window), ema: maps.Clone(stats.ema), emaPPM: maps.Clone(stats.emaPPM),
			egress: stats.egress, egressGeneration: stats.egressGeneration,
			settlementEpoch: epoch, settlementEpochKnown: true,
			settlementFirstSequence: stats.attemptSettlementFirstSequence,
			egressFirstSequence:     stats.attemptEgressFirstSequence,
		}
		if transitioning {
			if stats.attemptLedger == nil {
				return fmt.Errorf("no_id %d has no attempt ledger for settlement transition", participant.NoID)
			}
			preFold := stats.releaseStatsMeasurementWithLock()
			preFold.SettlementTransition = nil
			cut, err := stats.attemptLedger.BuildCut(terminalBoundary, stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence)
			if err != nil {
				return fmt.Errorf("no_id %d terminal attempt cut: %w", participant.NoID, err)
			}
			preFold.AttemptCut = cut
			if _, err := VerifyReleaseStatsMeasurement(preFold); err != nil {
				return fmt.Errorf("no_id %d terminal statistics: %w", participant.NoID, err)
			}
			candidate.transition = &AttemptSettlementTransition{
				Schema: attemptSettlementTransitionSchema, Identity: stats.attemptLedger.identity,
				FromBoundary: terminalBoundary, ToEpoch: epoch, PreFold: preFold,
			}
		}
		priorWindow, priorEMA, priorEMAPPM := stats.window, stats.ema, stats.emaPPM
		priorEgress, priorEgressGeneration := stats.egress, stats.egressGeneration
		previousEpoch, priorKnown := stats.settlementEpoch, stats.settlementEpochKnown
		priorSettlementFirst, priorEgressFirst := stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence
		stats.window, stats.ema, stats.emaPPM = candidate.window, candidate.ema, candidate.emaPPM
		if transitioning {
			stats.foldWithLock()
		}
		candidate.window, candidate.ema, candidate.emaPPM = stats.window, stats.ema, stats.emaPPM
		if transitioning {
			if stats.egressGeneration == ^uint64(0) {
				stats.window, stats.ema, stats.emaPPM = priorWindow, priorEMA, priorEMAPPM
				return errors.New("release statistics egress generation overflow")
			}
			nextSequence := stats.attemptLedger.LastSequence() + 1
			candidate.settlementFirstSequence, candidate.egressFirstSequence = nextSequence, nextSequence
			candidate.egress = map[connect.Id]map[[32]byte]bool{}
			candidate.egressGeneration++
			candidate.transition.PostFold = sortedAttemptEMAQualities(candidate.emaPPM)
		}
		stats.window, stats.ema, stats.emaPPM = priorWindow, priorEMA, priorEMAPPM
		stats.egress, stats.egressGeneration = priorEgress, priorEgressGeneration
		stats.settlementEpoch, stats.settlementEpochKnown = previousEpoch, priorKnown
		stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence = priorSettlementFirst, priorEgressFirst
		candidates[index] = candidate
	}
	if transitioning {
		batch := make([]AttemptSettlementMember, len(ordered))
		commonIdentity := candidates[0].transition.Identity
		for index, candidate := range candidates {
			if !equalAttemptSettlementIdentity(commonIdentity, candidate.transition.Identity) {
				return errors.New("settlement transition validator identities differ")
			}
			digest, err := attemptSettlementTransitionDigest(candidate.transition)
			if err != nil {
				return err
			}
			batch[index] = AttemptSettlementMember{NoID: ordered[index].NoID, Digest: attemptHex32(digest)}
		}
		for index := range candidates {
			candidates[index].transition.Batch = append([]AttemptSettlementMember(nil), batch...)
			if err := ordered[index].Stats.attemptLedger.signSettlementTransition(candidates[index].transition); err != nil {
				return err
			}
			if err := VerifyAttemptSettlementTransition(candidates[index].transition); err != nil {
				return err
			}
		}
	}
	for index, participant := range ordered {
		stats, candidate := participant.Stats, candidates[index]
		priorWindow, priorEMA, priorEMAPPM := stats.window, stats.ema, stats.emaPPM
		priorEgress, priorEgressGeneration := stats.egress, stats.egressGeneration
		previousEpoch, priorKnown := stats.settlementEpoch, stats.settlementEpochKnown
		priorSettlementFirst, priorEgressFirst := stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence
		priorTransition := stats.settlementTransition
		stats.window, stats.ema, stats.emaPPM = candidate.window, candidate.ema, candidate.emaPPM
		stats.egress, stats.egressGeneration = candidate.egress, candidate.egressGeneration
		stats.settlementEpoch, stats.settlementEpochKnown = epoch, true
		stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence = candidate.settlementFirstSequence, candidate.egressFirstSequence
		stats.settlementTransition = candidate.transition
		statsJSON, encodeErr := encodeStatsSnapshot(stats.snapshotWithLock())
		stats.window, stats.ema, stats.emaPPM = priorWindow, priorEMA, priorEMAPPM
		stats.egress, stats.egressGeneration = priorEgress, priorEgressGeneration
		stats.settlementEpoch, stats.settlementEpochKnown = previousEpoch, priorKnown
		stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence = priorSettlementFirst, priorEgressFirst
		stats.settlementTransition = priorTransition
		if encodeErr != nil {
			return encodeErr
		}
		transaction.Snapshots = append(transaction.Snapshots, attemptSettlementSnapshot{
			NoID: participant.NoID, StatsPath: filepath.Clean(filepath.Join(participant.StateDir, "stats.json")), StatsJSON: statsJSON,
		})
	}
	transactionBytes, err := canonicalAttemptSettlementTransaction(transaction)
	if err != nil {
		return err
	}
	transactionPath := attemptSettlementTransactionPath(coordinatorStateDir)
	if err := atomicStateWrite(transactionPath, transactionBytes, 0o600); err != nil {
		return err
	}
	for _, snapshot := range transaction.Snapshots {
		if err := writeSnapshot(snapshot.StatsPath, snapshot.StatsJSON); err != nil {
			return err
		}
	}
	for index, participant := range ordered {
		stats, candidate := participant.Stats, candidates[index]
		stats.window, stats.ema, stats.emaPPM = candidate.window, candidate.ema, candidate.emaPPM
		stats.egress, stats.egressGeneration = candidate.egress, candidate.egressGeneration
		stats.settlementEpoch, stats.settlementEpochKnown = candidate.settlementEpoch, candidate.settlementEpochKnown
		stats.attemptSettlementFirstSequence, stats.attemptEgressFirstSequence = candidate.settlementFirstSequence, candidate.egressFirstSequence
		stats.settlementTransition = candidate.transition
	}
	if err := removeTransaction(transactionPath); err != nil {
		return err
	}
	for _, participant := range ordered {
		participant.Stats.attemptCutPending = false
	}
	return nil
}

// AdvanceAttemptSettlementEpoch atomically folds every operator context. The
// recovery journal makes sequential filesystem replacement one logical commit.
func AdvanceAttemptSettlementEpoch(coordinatorStateDir string, epoch uint64, terminalBoundary AttemptBoundary, participants []AttemptSettlementParticipant) error {
	return advanceAttemptSettlementEpochWithWrite(coordinatorStateDir, epoch, terminalBoundary, participants, func(path string, payload []byte) error {
		return atomicStateWrite(path, payload, 0o600)
	})
}
