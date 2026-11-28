//go:build linux || darwin

package validator

// Runtime terminal ownership uses the same Stats writer tokens as ordinary
// checkpoints. Typed stream sealing and complete batch replay happen outside
// every state mutex; a journal owns one exact all-operator postimage.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/urnetwork/connect/v2026"
)

// The root owns ledger lifetime. Recovery accepts only fresh, unattached Stats;
// live operations require this exact ledger already attached to the engine.
type AttemptSettlementRuntimeV2Participant struct {
	NoID     uint64
	StateDir string
	Stats    *StatsEngine
	Ledger   *AttemptLedger
}

// Signing and replay authority are independently authenticated caller inputs.
// Each seal and verification pass needs a distinct, fresh scratch namespace.
type AttemptSettlementRuntimeV2Options struct {
	Authority   AttemptSettlementV2Options
	Persistence AttemptSettlementRuntimeV2PersistenceBounds
	Seal        map[uint64]AttemptCutV2SealOptions
	PrivateKeys map[uint64]ed25519.PrivateKey
}

// Canonical participant order and immutable engine order are intentionally
// separate. Candidates are private until one coherent publication boundary.
type attemptSettlementV2Owners struct {
	ordered     []AttemptSettlementRuntimeV2Participant
	engineOrder []AttemptSettlementParticipant
	owners      []*statsWriteOwner
	candidates  []*StatsEngine
	persistence AttemptSettlementRuntimeV2PersistenceBounds
}

// Pure namespace admission precedes filesystem and transport callbacks.
func validateAttemptSettlementV2Paths(paths []string) error {
	for index, path := range paths {
		if !utf8.ValidString(path) || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
			return errors.New("compact runtime path is not lossless UTF-8, clean, absolute and non-root")
		}
		for _, prior := range paths[:index] {
			if path == prior || strings.HasPrefix(path, prior+string(filepath.Separator)) || strings.HasPrefix(prior, path+string(filepath.Separator)) {
				return errors.New("compact runtime namespaces overlap")
			}
		}
	}
	return nil
}

// Complete labels, physical ledger routing and fresh/live attachment are
// checked before token waits and again after all writers have been acquired.
func validateAttemptSettlementRuntimeV2Participants(coordinator string, participants []AttemptSettlementRuntimeV2Participant, authority AttemptSettlementV2Options, fresh bool) ([]AttemptSettlementRuntimeV2Participant, error) {
	if len(participants) != len(authority.Operators) || len(participants) == 0 {
		return nil, errors.New("compact runtime participant census differs from authority")
	}
	ordered := slices.Clone(participants)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].NoID < ordered[j].NoID })
	engines := map[*StatsEngine]bool{}
	ledgers := map[*AttemptLedger]bool{}
	if err := validateAttemptSettlementV2Paths([]string{coordinator}); err != nil {
		return nil, err
	}
	paths := []string{}
	for index, participant := range ordered {
		operator, exists := authority.Operators[participant.NoID]
		if !exists || participant.Stats == nil || participant.Ledger == nil || engines[participant.Stats] || ledgers[participant.Ledger] || index > 0 && ordered[index-1].NoID == participant.NoID {
			return nil, errors.New("compact runtime participant ownership is incomplete or duplicated")
		}
		engines[participant.Stats], ledgers[participant.Ledger] = true, true
		store, disk := participant.Ledger.disk.(*attemptRecordStore)
		if !disk || store == nil || participant.Ledger.identity != operator.Expected.Identity || store.identity.Identity != operator.Expected.Identity || store.identity.Coordinator != "0x"+hex.EncodeToString(operator.Expected.Activation.Domain.Coordinator[:]) || participant.StateDir != filepath.Dir(participant.Ledger.path) {
			return nil, errors.New("compact runtime participant differs from its root-owned disk ledger")
		}
		valid := func() bool {
			stats := participant.Stats
			stats.mu.Lock()
			defer stats.mu.Unlock()
			if fresh {
				return stats.attemptLedger == nil && stats.attemptV2 == nil && stats.settlementTransition == nil && !stats.settlementEpochKnown && stats.settlementEpoch == 0 && stats.egressGeneration == 0 && stats.attemptLastAppliedSequence == 0 && stats.attemptSettlementFirstSequence == 0 && stats.attemptEgressFirstSequence == 0 && stats.activeAttemptCount == 0 && !stats.attemptCutPending && !stats.attemptSettlementCutPending && len(stats.window) == 0 && len(stats.ema) == 0 && len(stats.emaPPM) == 0 && len(stats.egress) == 0
			}
			return stats.attemptLedger == participant.Ledger
		}()
		if !valid {
			return nil, errors.New("compact runtime requires exact live attachment or explicitly fresh recovery engines")
		}
		if participant.StateDir == coordinator || strings.HasPrefix(coordinator, participant.StateDir+string(filepath.Separator)) {
			return nil, errors.New("compact runtime operator cannot contain the coordinator directory")
		}
		if err := validateAttemptSettlementV2Paths([]string{coordinator, operator.Measurement.Replay.ScratchDirectory}); err != nil {
			return nil, err
		}
		paths = append(paths, participant.StateDir, operator.Measurement.Replay.ScratchDirectory)
	}
	if err := validateAttemptSettlementV2Paths(paths); err != nil {
		return nil, err
	}
	return ordered, nil
}

// Acquires every token in immutable engine order without retaining state locks.
func acquireAttemptSettlementV2Owners(ctx context.Context, coordinator string, participants []AttemptSettlementRuntimeV2Participant, authority AttemptSettlementV2Options, fresh bool, persistence AttemptSettlementRuntimeV2PersistenceBounds, physical attemptSettlementV2IO) (*attemptSettlementV2Owners, error) {
	if err := persistence.validate(); err != nil {
		return nil, err
	}
	ordered, err := validateAttemptSettlementRuntimeV2Participants(coordinator, participants, authority, fresh)
	if err != nil {
		return nil, err
	}
	legacy := make([]AttemptSettlementParticipant, len(ordered))
	for index, participant := range ordered {
		legacy[index] = AttemptSettlementParticipant{NoID: participant.NoID, StateDir: participant.StateDir, Stats: participant.Stats}
	}
	engineOrder, err := orderStatsEngineOwners(legacy)
	if err != nil {
		return nil, err
	}
	batch := &attemptSettlementV2Owners{ordered: ordered, engineOrder: engineOrder, candidates: make([]*StatsEngine, len(ordered)), persistence: persistence}
	byEngine := map[*StatsEngine]*statsWriteOwner{}
	for _, participant := range engineOrder {
		owner, err := participant.Stats.acquireStatsWrite(ctx, "settlement-v2")
		if err != nil {
			batch.release()
			return nil, err
		}
		batch.owners = append(batch.owners, owner)
		byEngine[participant.Stats] = owner
	}
	if _, err := validateAttemptSettlementRuntimeV2Participants(coordinator, ordered, authority, fresh); err != nil {
		batch.release()
		return nil, err
	}
	if err := admitAttemptSettlementV2OwnedBorrowed(ctx, batch, authority); err != nil {
		batch.release()
		return nil, err
	}
	if physical.step != nil {
		for _, participant := range ordered {
			if err := physical.step(fmt.Sprintf("before-stats-clone-%d", participant.NoID)); err != nil {
				batch.release()
				return nil, err
			}
		}
	}
	if err := admitAttemptSettlementV2OwnedBorrowed(ctx, batch, authority); err != nil {
		batch.release()
		return nil, err
	}
	for index, participant := range ordered {
		batch.candidates[index] = byEngine[participant.Stats].clone()
	}
	return batch, nil
}

// Releases only tokens acquired by this batch, including partial acquisition.
func (self *attemptSettlementV2Owners) release() {
	for index := len(self.owners) - 1; index >= 0; index-- {
		self.owners[index].release()
	}
}

// No callback or persistence occurs while these short state locks are held.
func (self *attemptSettlementV2Owners) publish() {
	for _, participant := range self.engineOrder {
		participant.Stats.mu.Lock()
	}
	for index, participant := range self.ordered {
		participant.Stats.publishStatsWithLock(self.candidates[index])
	}
	for index := len(self.engineOrder) - 1; index >= 0; index-- {
		self.engineOrder[index].Stats.mu.Unlock()
	}
}

// Reserve the entire batch before testing any active trail count. A retry
// never takes over a native detach or another settlement's reservation.
func (self *attemptSettlementV2Owners) reserve(epoch uint64) error {
	for _, stats := range self.candidates {
		if stats.attemptSettlementCutPending && stats.attemptSettlementCutEpoch != epoch || stats.attemptCutPending && !stats.attemptSettlementCutPending {
			return errAttemptCutPending
		}
	}
	for _, stats := range self.candidates {
		stats.attemptCutPending, stats.attemptSettlementCutPending, stats.attemptSettlementCutEpoch = true, true, epoch
	}
	self.publish()
	for _, stats := range self.candidates {
		if stats.activeAttemptCount != 0 {
			return errAttemptCutPending
		}
	}
	return nil
}

// Successful completion releases only this coordinator's reservation.
func (self *attemptSettlementV2Owners) clearReservation(epoch uint64) {
	for _, stats := range self.candidates {
		if stats.attemptSettlementCutPending && stats.attemptSettlementCutEpoch == epoch {
			stats.attemptCutPending, stats.attemptSettlementCutPending = false, false
		}
	}
	self.publish()
}

// Bounds the union before copying a raw measurement. Carried v1/v2 history
// remains in durable Stats, never in a terminal's nonrecursive raw preimage.
func attemptSettlementV2RawMeasurement(stats *StatsEngine, operator AttemptSettlementV2OperatorOptions) (ReleaseStatsMeasurement, error) {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	ids := map[connect.Id]bool{}
	add := func(id connect.Id) error {
		if !ids[id] && uint64(len(ids)) >= operator.Measurement.MaxProviders {
			return errors.New("compact runtime provider census exceeds its bound")
		}
		ids[id] = true
		return nil
	}
	for id := range stats.window {
		if err := add(id); err != nil {
			return ReleaseStatsMeasurement{}, err
		}
	}
	for id := range stats.emaPPM {
		if err := add(id); err != nil {
			return ReleaseStatsMeasurement{}, err
		}
	}
	for id := range stats.egress {
		if err := add(id); err != nil {
			return ReleaseStatsMeasurement{}, err
		}
	}
	var hashes uint64
	for _, values := range stats.egress {
		if uint64(len(values)) > operator.Measurement.MaxEgressHashes-hashes {
			return ReleaseStatsMeasurement{}, errors.New("compact runtime egress census exceeds its bound")
		}
		hashes += uint64(len(values))
	}
	measurement := stats.releaseStatsMeasurementWithLock()
	measurement.SettlementTransition = nil
	if measurement.Config != operator.Measurement.ExpectedConfig {
		return ReleaseStatsMeasurement{}, errors.New("compact runtime scoring config differs from authority")
	}
	return measurement, nil
}

// Owns the complete sealer census and key material before any sealer callback.
func ownAttemptSettlementRuntimeV2Options(ctx context.Context, coordinator string, participants []AttemptSettlementRuntimeV2Participant, options AttemptSettlementRuntimeV2Options) (AttemptSettlementRuntimeV2Options, error) {
	if err := options.Persistence.validate(); err != nil {
		return AttemptSettlementRuntimeV2Options{}, err
	}
	if err := validateAttemptSettlementV2MetadataLimit(options.Authority.MaxClosureBytes); err != nil {
		return AttemptSettlementRuntimeV2Options{}, err
	}
	authority, err := ownAttemptSettlementV2Options(ctx, options.Authority)
	if err != nil {
		return AttemptSettlementRuntimeV2Options{}, err
	}
	ordered, err := validateAttemptSettlementRuntimeV2Participants(coordinator, participants, authority, false)
	if err != nil {
		return AttemptSettlementRuntimeV2Options{}, err
	}
	if len(options.Seal) != len(ordered) || len(options.PrivateKeys) != len(ordered) {
		return AttemptSettlementRuntimeV2Options{}, errors.New("compact runtime seal or signer census differs")
	}
	owned := AttemptSettlementRuntimeV2Options{Authority: authority, Persistence: options.Persistence, Seal: map[uint64]AttemptCutV2SealOptions{}, PrivateKeys: map[uint64]ed25519.PrivateKey{}}
	paths := []string{}
	for _, participant := range ordered {
		operator := authority.Operators[participant.NoID]
		seal, exists := options.Seal[participant.NoID]
		if !exists || seal.WriteRecords == nil || seal.WriteProofs == nil || seal.WriteMetadata == nil || seal.ReadMetadata == nil || seal.OpenData == nil {
			return AttemptSettlementRuntimeV2Options{}, errors.New("compact runtime sealer callbacks are incomplete")
		}
		if err := seal.ReplayBounds.validate(operator.Bounds); err != nil {
			return AttemptSettlementRuntimeV2Options{}, err
		}
		vpk, err := canonicalAttemptHex32("compact runtime signing identity", operator.Expected.Identity.ValidatorVPK, false)
		if err != nil {
			return AttemptSettlementRuntimeV2Options{}, err
		}
		if err := attemptCutV2PrivateKey(options.PrivateKeys[participant.NoID], vpk[:]); err != nil {
			return AttemptSettlementRuntimeV2Options{}, err
		}
		seal.ServerKeys = maps.Clone(seal.ServerKeys)
		for id, key := range seal.ServerKeys {
			if !bytes.Equal(key, operator.Measurement.Replay.ServerKeys[id]) || len(key) != ed25519.PublicKeySize {
				return AttemptSettlementRuntimeV2Options{}, errors.New("compact runtime seal server-key authority differs")
			}
			seal.ServerKeys[id] = slices.Clone(key)
		}
		if len(seal.ServerKeys) != len(operator.Measurement.Replay.ServerKeys) {
			return AttemptSettlementRuntimeV2Options{}, errors.New("compact runtime seal server-key census differs")
		}
		owned.Seal[participant.NoID], owned.PrivateKeys[participant.NoID] = seal, slices.Clone(options.PrivateKeys[participant.NoID])
		if err := validateAttemptSettlementV2Paths([]string{coordinator, seal.ScratchDirectory}); err != nil {
			return AttemptSettlementRuntimeV2Options{}, err
		}
		paths = append(paths, participant.StateDir, seal.ScratchDirectory, operator.Measurement.Replay.ScratchDirectory)
	}
	if err := validateAttemptSettlementV2Paths(paths); err != nil {
		return AttemptSettlementRuntimeV2Options{}, err
	}
	return owned, nil
}

// Initializes a drained, genuinely empty current window at the exact owned
// prefix. Existing nonempty windows require an explicit separate migration.
func InitializeAttemptSettlementEpochV2(ctx context.Context, coordinator string, participants []AttemptSettlementRuntimeV2Participant, epoch uint64, authority AttemptSettlementV2Options, persistence AttemptSettlementRuntimeV2PersistenceBounds) error {
	return initializeAttemptSettlementEpochV2(ctx, coordinator, participants, epoch, authority, persistence, attemptSettlementV2PhysicalIO())
}

// Folds and persists one complete typed terminal; retries finish journal-owned
// bytes instead of resealing or folding the raw observations for a second time.
func AdvanceAttemptSettlementEpochV2(ctx context.Context, coordinator string, participants []AttemptSettlementRuntimeV2Participant, epoch uint64, terminalBoundary AttemptBoundary, options AttemptSettlementRuntimeV2Options) (*AttemptSettlementClosureV2, error) {
	return advanceAttemptSettlementEpochV2(ctx, coordinator, participants, epoch, terminalBoundary, options, attemptSettlementV2PhysicalIO(), true)
}

// Startup must call this before constructing workers. Neither a failed replay
// nor a partially recovered disk image can attach one public operator early.
func RecoverAttemptSettlementEpochV2(ctx context.Context, coordinator string, participants []AttemptSettlementRuntimeV2Participant, authority AttemptSettlementV2Options, persistence AttemptSettlementRuntimeV2PersistenceBounds) error {
	return recoverAttemptSettlementEpochV2(ctx, coordinator, participants, authority, persistence, attemptSettlementV2PhysicalIO())
}
