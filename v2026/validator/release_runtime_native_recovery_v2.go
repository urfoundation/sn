//go:build linux || darwin

// A native journal commits before its Stats snapshot. Interrupted publication
// retains the independently chosen cut context until real replay joins both
// owners; a newer polling snapshot cannot choose a replacement cut.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
)

// One worker owns this finite witness until all workers join the runtime gate.
// The signed file remains the durable source; startup reconstructs its context
// from independent activation and ordered ledger history after process exit.
type releaseMeasurementInputV2CutAuthority struct {
	expected     AttemptCutV2Context
	journalHash  [32]byte
	journalBytes uint64
}

// Signed context and exact bytes must agree before any Stats reconciliation.
func (self *releaseMeasurementInputV2CutAuthority) retain(encoded []byte, cut AttemptCutV2, bounds AttemptCutV2Bounds) error {
	if err := cut.VerifyHeader(self.expected, bounds); err != nil {
		return err
	}
	hash := sha256.Sum256(encoded)
	if self.journalBytes != 0 && (self.journalBytes != uint64(len(encoded)) || self.journalHash != hash) {
		return errors.New("retained compact native journal changed after publication")
	}
	self.journalHash, self.journalBytes = hash, uint64(len(encoded))
	return nil
}

// The map has at most one entry per configured participant, including attempts
// interrupted before publication. No native transaction or signing key lives
// here; reconciliation can only adopt the original immutable local cut.
type releaseRuntimeNativeInputV2 struct {
	authority   *releaseMeasurementInputV2CutAuthority
	subnetEpoch uint64
	nativeBlock uint64
	nativeHash  string
}

// Both collection and background settlement advancement hold the gate. They
// finish a published native cut before closing its settlement or cancelling an
// unsigned reservation; complete siblings retain their independent progress.
func (self *releaseRuntimeV2) reconcileNativeInputsOwned(ctx context.Context, snapshot *ReleaseSnapshot) error {
	if len(self.nativeInputNoIdKVs) == 0 {
		return ctx.Err()
	}
	if snapshot == nil || snapshot.Epoch == nil || !snapshot.Epoch.IsUint64() || snapshot.BlockNumber == 0 {
		return errors.New("native input recovery has no finalized snapshot")
	}
	var failures []error
	for _, participant := range self.history.participants {
		noId := participant.NoID
		pending := self.nativeInputNoIdKVs[noId]
		if pending == nil {
			continue
		}
		err := func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			expected := pending.authority.expected
			boundary := expected.Boundary
			current, err := self.context(noId, self.history.current[noId], boundary)
			if err != nil || current != expected {
				return errors.Join(errors.New("native input recovery lost its original cursor authority"), err)
			}
			if snapshot.Epoch.Uint64() < boundary.SettlementEpoch || snapshot.BlockNumber < boundary.EVMBlock {
				return errAttemptSettlementSnapshotStale
			}
			if !releaseBlockAtOrBefore(boundary.EVMBlock, boundary.EVMBlockHash, snapshot.BlockNumber, releaseHex32(snapshot.BlockHash)) {
				return errors.New("native input recovery snapshot conflicts with its original boundary")
			}
			bounds := self.cfg.EvidenceV2.Bounds
			encoded, err := readReleaseMeasurementInputV2Context(ctx, releaseMeasurementInputV2Path(self.cfg.StateDir, pending.subnetEpoch, noId), bounds.MaxInputJournalBytes, releaseMeasurementInputV2ReadHooks{})
			if err != nil {
				if pending.authority.journalBytes == 0 && releaseMeasurementInputV2InitialAbsence(err) {
					delete(self.nativeInputNoIdKVs, noId)
					return nil
				}
				return err
			}
			operator, err := self.operator(ctx, expected, "ordinary-recovery")
			if err != nil {
				return err
			}
			options := releaseMeasurementInputV2Options{MaxJournalBytes: bounds.MaxInputJournalBytes, cutAuthority: pending.authority,
				Stats: releaseStatsV2Options{Activation: expected.Activation, Policy: operator.Policy, Bounds: bounds.Cut,
					Stats: AttemptCutV2StatsOptions{ExpectedConfig: operator.Measurement.ExpectedConfig, MaxProviders: bounds.MaxProviders, MaxEgressHashes: bounds.MaxEgressHashes, Replay: operator.Measurement.Replay}}}
			journal, err := decodeReleaseMeasurementInputV2(ctx, encoded, options)
			if err != nil {
				return err
			}
			if err := pending.authority.retain(encoded, *journal.MeasurementInput.AttemptCutV2, bounds.Cut); err != nil {
				return err
			}
			hash, err := parseReleaseHex32("retained native cut Evm hash", boundary.EVMBlockHash, false)
			if err != nil {
				return err
			}
			original := &ReleaseSnapshot{Epoch: new(big.Int).SetUint64(boundary.SettlementEpoch), BlockNumber: boundary.EVMBlock, BlockHash: hash}
			// This reader has only the actual Stats owner and configuration. It
			// has no intent store or native signer and cannot dispatch a weight.
			reader := &ReleaseSteerer{cfg: &self.cfg, contexts: map[uint64]*ReleaseMeasurementContext{noId: {NoID: noId, Stats: participant.Stats}}}
			input, err := reader.loadOrDetachReleaseMeasurementInputV2(ctx, noId, pending.subnetEpoch, pending.nativeBlock, pending.nativeHash, original, options)
			if err != nil {
				return err
			}
			return self.retainNativeInputOwned(ctx, input, pending.subnetEpoch, expected, options, false)
		}()
		if err != nil {
			failures = append(failures, fmt.Errorf("release V2 native input recovery no_id %d: %w", noId, err))
		}
	}
	return errors.Join(append(failures, ctx.Err())...)
}

// The journal's own bytes are decoded again after physical custody and Stats
// replay complete. Only that exact result can advance the independent cursor.
func (self *releaseRuntimeV2) retainNativeInputOwned(ctx context.Context, input ReleaseMeasurementInput, subnetEpoch uint64, expected AttemptCutV2Context, options releaseMeasurementInputV2Options, retry bool) error {
	noId, bounds := expected.Identity.NoID, self.cfg.EvidenceV2.Bounds
	if input.AttemptCutV2 == nil {
		return errors.New("release V2 native detach omitted its actual signed cut")
	}
	if err := input.AttemptCutV2.VerifyHeader(expected, bounds.Cut); err != nil {
		return err
	}
	cursor := self.history.current[noId]
	if !retry {
		current, err := self.context(noId, cursor, expected.Boundary)
		if err != nil || current != expected {
			return errors.Join(errors.New("native input adoption differs from its original cursor authority"), err)
		}
	}
	encoded, err := readReleaseMeasurementInputV2Context(ctx, releaseMeasurementInputV2Path(self.cfg.StateDir, subnetEpoch, noId), bounds.MaxInputJournalBytes, releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		return err
	}
	journal, err := decodeReleaseMeasurementInputV2(ctx, encoded, options)
	if err != nil {
		return err
	}
	if err := options.cutAuthority.retain(encoded, *journal.MeasurementInput.AttemptCutV2, bounds.Cut); err != nil {
		return err
	}
	actual, err := marshalAttemptSettlementV2JSON(ctx, input, bounds.MaxInputJournalBytes, false, true)
	if err != nil {
		return err
	}
	retained, err := marshalAttemptSettlementV2JSON(ctx, journal.MeasurementInput, bounds.MaxInputJournalBytes, false, true)
	if err != nil || !bytes.Equal(actual, retained) {
		return errors.Join(errors.New("release V2 native journal changed after actual detach"), err)
	}
	if self.history.inputByEpoch[subnetEpoch] == nil {
		self.history.inputByEpoch[subnetEpoch] = make(map[uint64]*releaseMeasurementInputJournal)
	}
	self.history.inputByEpoch[subnetEpoch][noId] = journal
	if self.history.inputContextsByEpoch == nil {
		self.history.inputContextsByEpoch = make(map[uint64]map[uint64]AttemptCutV2Context)
	}
	if self.history.inputContextsByEpoch[subnetEpoch] == nil {
		self.history.inputContextsByEpoch[subnetEpoch] = make(map[uint64]AttemptCutV2Context)
	}
	self.history.inputContextsByEpoch[subnetEpoch][noId] = expected
	if !retry {
		cursor.egressFirst, cursor.generation = input.AttemptCutV2.LastSequence+1, cursor.generation+1
		cursor.lastSequence, cursor.lastRoot, cursor.lastBoundary = input.AttemptCutV2.LastSequence, input.AttemptCutV2.Root, expected.Boundary
		self.history.current[noId] = cursor
		self.history.lastOrdinary[noId] = journal
		if self.history.ordinaryContexts == nil {
			self.history.ordinaryContexts = make(map[uint64]AttemptCutV2Context)
		}
		self.history.ordinaryContexts[noId] = expected
	}
	delete(self.nativeInputNoIdKVs, noId)
	delete(self.nativeReservations, noId)
	return nil
}
