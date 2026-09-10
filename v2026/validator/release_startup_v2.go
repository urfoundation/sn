//go:build linux || darwin

// Startup keeps public Stats dormant while the genuine all-operator recovery,
// journal-first native reconciliation and proof projections complete privately.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"slices"

	"github.com/urfoundation/sn/v2026/crv4"
)

// This is the sole semantic join for the root's acquired disk census. It does
// not start workers, submit transactions or remove the release entrypoint gate.
// The caller continues to own every ledger and must join users before Close.
func startReleaseEvidenceV2DiskState(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, inputs []releaseEvidenceV2ActivationInput, serverKeys map[uint64]map[byte]ed25519.PublicKey, origins [2]string, disk *releaseEvidenceV2DiskState) error {
	if cfg == nil {
		return errors.New("evidence semantic startup configuration is absent")
	}
	runtime := crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.RuntimeSpec, TransactionVersion: cfg.TransactionVersion, StateVersion: cfg.StateVersion}, CodeHash: cfg.RuntimeCodeHash, MetadataHash: cfg.RuntimeMetadataHash}
	return startReleaseEvidenceV2DiskStateWithRuntime(ctx, cfg, chain, native, inputs, serverKeys, origins, disk, runtime, attemptSettlementV2PhysicalIO())
}

// Tests may select actual reviewed fixture metadata and physical failures, but
// cannot supply activation, cursor, replay or proof-projection verdicts.
func startReleaseEvidenceV2DiskStateWithRuntime(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, inputs []releaseEvidenceV2ActivationInput, serverKeys map[uint64]map[byte]ed25519.PublicKey, origins [2]string, disk *releaseEvidenceV2DiskState, runtime crv4.RuntimeArtifactIdentity, physical attemptSettlementV2IO) (resultErr error) {
	return startReleaseEvidenceV2DiskStateOwned(ctx, cfg, chain, native, inputs, serverKeys, origins, disk, runtime, physical, nil)
}

// The production root retains only the already-reconstructed cursor inputs
// after genuine semantic publication and every physical owner has closed.
// It cannot replace a replay result or expose a partial startup census.
func startReleaseEvidenceV2DiskStateOwned(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, inputs []releaseEvidenceV2ActivationInput, serverKeys map[uint64]map[byte]ed25519.PublicKey, origins [2]string, disk *releaseEvidenceV2DiskState, runtime crv4.RuntimeArtifactIdentity, physical attemptSettlementV2IO, retained **releaseEvidenceV2StartupHistory) (resultErr error) {
	if ctx == nil {
		return errors.New("evidence semantic startup context is absent")
	}
	if retained != nil {
		*retained = nil
		defer func() {
			if resultErr != nil {
				*retained = nil
			}
		}()
	}
	defer func() { resultErr = errors.Join(resultErr, ctx.Err()) }()
	if err := physical.validate(); err != nil {
		return err
	}
	history, err := readReleaseEvidenceV2StartupHistoryWithRuntime(ctx, cfg, chain, native, inputs, serverKeys, origins, disk, runtime)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, history.files.close()) }()
	initial, err := history.preflightImages(ctx)
	if err != nil {
		return err
	}
	// Reference authentication is separate from statistics replay and never
	// returns a head/weight decision. Journals before Begin remain in history.
	references, err := history.readIntentReferences(ctx, chain, native, runtime)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, references.close()) }()
	if err := errors.Join(history.files.check(), references.check(), ctx.Err()); err != nil {
		return err
	}
	options, err := history.recoveryAuthority(ctx, "startup-recovery")
	if err != nil {
		return err
	}
	// Retain the public batch's exclusive tokens across all private work.
	// No state mutex is retained through RPC, public streams, writes or closes.
	publication, err := acquireAttemptSettlementV2Owners(ctx, history.cfg.StateDir, history.participants, options, true, history.cfg.EvidenceV2.Bounds.Persistence, attemptSettlementV2PhysicalIO())
	if err != nil {
		return err
	}
	defer publication.release()
	private := &releaseEvidenceV2DiskState{states: make(map[uint64]*releaseAttemptState, len(history.participants)), participants: slices.Clone(history.participants)}
	for index, participant := range private.participants {
		stats := NewStatsEngine(participant.Stats.cfg)
		if initial != nil {
			stats = initial[index]
			stats.attemptLedger = participant.Ledger
		}
		private.participants[index].Stats = stats
		private.states[participant.NoID] = &releaseAttemptState{ledger: participant.Ledger, stats: stats}
	}
	// The immutable closure census ends at this exact authenticated boundary.
	// A pending transaction may now genuinely publish its missing closure.
	// Ordinary inputs and intent references remain retained through mutation.
	if err := history.files.directories[1].close(); err != nil {
		return err
	}
	check := func() error { return errors.Join(history.files.directories[0].check(), references.check(), ctx.Err()) }
	writeSnapshot, writeJournal, removeJournal, step := physical.writeSnapshot, physical.writeJournal, physical.removeJournal, physical.step
	physical.writeSnapshot = func(root *attemptPrivateDirectory, name string, encoded []byte) error {
		if err := check(); err != nil {
			return err
		}
		return writeSnapshot(root, name, encoded)
	}
	physical.writeJournal = func(root *attemptPrivateDirectory, name string, encoded []byte) error {
		if err := check(); err != nil {
			return err
		}
		return writeJournal(root, name, encoded)
	}
	physical.removeJournal = func(root *attemptPrivateDirectory, name string) error {
		if err := check(); err != nil {
			return err
		}
		return removeJournal(root, name)
	}
	physical.step = func(phase string) error {
		if step != nil {
			if err := step(phase); err != nil {
				return err
			}
		}
		return check()
	}
	physical.startupImages = history.images
	if err := check(); err != nil {
		return err
	}
	if initial != nil {
		err = initializeAttemptSettlementEpochV2(ctx, history.cfg.StateDir, private.participants, initial[0].settlementEpoch, options, history.cfg.EvidenceV2.Bounds.Persistence, physical)
	} else {
		err = recoverAttemptSettlementEpochV2(ctx, history.cfg.StateDir, private.participants, options, history.cfg.EvidenceV2.Bounds.Persistence, physical)
	}
	if err != nil {
		return err
	}
	for _, participant := range private.participants {
		if err := history.reconcileOrdinary(ctx, participant); err != nil {
			return err
		}
		// Attachment is deliberately ignored only on this private clone: the
		// exact cursor/history comparator admits no active consumer or pointer.
		participant.Stats.mu.Lock()
		candidate := participant.Stats.cloneStatsWithLock()
		participant.Stats.mu.Unlock()
		candidate.attemptLedger = nil
		if err := history.matchImage(ctx, participant, candidate, history.current[participant.NoID], false); err != nil {
			return err
		}
	}
	if err := check(); err != nil {
		return err
	}
	if err := prepareReleaseEvidenceV2ProofStores(ctx, private); err != nil {
		return err
	}
	if err := errors.Join(history.files.close(), references.close(), ctx.Err()); err != nil {
		return err
	}
	// Final destination checks precede the no-callback publication boundary.
	// The original dormant Stats objects are retained, never replaced by an
	// attacker-mutable map entry or an early single-operator attachment.
	if len(disk.states) != len(history.participants) || len(disk.participants) != len(history.participants) {
		return errors.New("evidence semantic startup publication census changed")
	}
	for index, participant := range history.participants {
		state := disk.states[participant.NoID]
		if state == nil || state != history.states[participant.NoID] || state.stats != participant.Stats || state.ledger != participant.Ledger || state.store != nil || disk.participants[index] != participant || private.states[participant.NoID].store == nil {
			return errors.New("evidence semantic startup publication ownership changed")
		}
		publication.candidates[index] = private.participants[index].Stats
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, participant := range history.participants {
		history.states[participant.NoID].store = private.states[participant.NoID].store
	}
	publication.publish()
	if retained != nil {
		*retained = history
	}
	return nil
}

// Only the latest current-window ordinary journal can have missed its atomic
// snapshot rotation. Its actual bounded immutable bytes are reacquired before
// the existing full replay and real egress-suffix reconciliation are invoked.
func (self *releaseEvidenceV2StartupHistory) reconcileOrdinary(ctx context.Context, participant AttemptSettlementRuntimeV2Participant) (resultErr error) {
	journal := self.lastOrdinary[participant.NoID]
	if journal == nil {
		return ctx.Err()
	}
	path := releaseMeasurementInputV2Path(self.cfg.StateDir, journal.SubnetEpoch, participant.NoID)
	owner, err := acquireReleaseMeasurementInputV2Owner(ctx, path, self.cfg.EvidenceV2.Bounds.MaxInputJournalBytes, releaseMeasurementInputV2ReadHooks{}, false)
	defer func() { resultErr = errors.Join(resultErr, owner.finish(), ctx.Err()) }()
	if err != nil {
		return err
	}
	encoded, err := owner.read()
	if err != nil {
		return err
	}
	want, err := canonicalReleaseMeasurementInputBytes(journal)
	if err != nil || !bytes.Equal(encoded, want) {
		return errors.Join(errors.New("startup latest ordinary journal changed before real reconciliation"), err)
	}
	input := journal.MeasurementInput
	operator, err := self.operator(ctx, participant.NoID, self.lastOrdinaryBefore[participant.NoID], input.AttemptCutV2.Context.Boundary, 0, "startup-reconcile")
	if err != nil {
		return err
	}
	options := releaseStatsV2Options{Activation: operator.Expected.Activation, Policy: operator.Policy, Bounds: operator.Bounds, Stats: AttemptCutV2StatsOptions{ExpectedConfig: operator.Measurement.ExpectedConfig, MaxProviders: operator.Measurement.MaxProviders, MaxEgressHashes: operator.Measurement.MaxEgressHashes, Replay: operator.Measurement.Replay}}
	guard := func(phase string) error {
		if phase == "before-save" {
			return owner.check()
		}
		if phase == "after-save" {
			return errors.Join(owner.check(), owner.finish(), ctx.Err())
		}
		return fmt.Errorf("startup ordinary guard phase %q is invalid", phase)
	}
	if err := owner.sync(); err != nil {
		return err
	}
	if err := participant.Stats.reconcileReleaseStatsCutV2WithJournalGuard(ctx, participant.StateDir, input.Stats, *input.AttemptCutV2, options, guard); err != nil {
		return fmt.Errorf("startup latest ordinary no_id %d: %w", participant.NoID, err)
	}
	return nil
}
