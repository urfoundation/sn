//go:build linux || darwin

package validator

// Compact native cuts use the existing Stats write owner for drain, immutable
// evidence publication and durable egress rotation. No whole-history cut is
// built, and external object/ledger callbacks never retain the Stats mutex.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// Activation and all capacities are authenticated caller inputs. Each seal
// and statistics replay needs independent fresh scratch; callers must not
// mutate options or their callback/key ownership while an operation runs.
type releaseStatsV2Options struct {
	Activation      AttemptCutV2Activation
	Policy          protocol.Policy
	Bounds          AttemptCutV2Bounds
	Seal            AttemptCutV2SealOptions
	Stats           AttemptCutV2StatsOptions
	retainedStartup bool
}

// Admits the existing live census before copying any provider/hash collection.
// Carried v1 terminal evidence remains in Stats, but is not embedded in the
// compact raw-statistics view or silently treated as v2 replay authority.
func (self *StatsEngine) releaseStatsV2Measurement(options AttemptCutV2StatsOptions) (ReleaseStatsMeasurement, error) {
	self.mu.Lock()
	defer self.mu.Unlock()
	if options.MaxProviders == 0 || options.MaxEgressHashes == 0 {
		return ReleaseStatsMeasurement{}, errors.New("compact native statistics census bounds are absent")
	}
	providerKVs := map[connect.Id]bool{}
	admit := func(id connect.Id) bool {
		if !providerKVs[id] {
			if uint64(len(providerKVs)) >= options.MaxProviders {
				return false
			}
			providerKVs[id] = true
		}
		return true
	}
	for id := range self.window {
		if !admit(id) {
			return ReleaseStatsMeasurement{}, errors.New("compact native provider census exceeds its bound")
		}
	}
	for id := range self.emaPPM {
		if !admit(id) {
			return ReleaseStatsMeasurement{}, errors.New("compact native prior-provider census exceeds its bound")
		}
	}
	for id := range self.ema {
		if !admit(id) {
			return ReleaseStatsMeasurement{}, errors.New("compact native reporting-provider census exceeds its bound")
		}
	}
	var hashes uint64
	for id, values := range self.egress {
		if !admit(id) || uint64(len(values)) > options.MaxEgressHashes-hashes {
			return ReleaseStatsMeasurement{}, errors.New("compact native egress census exceeds its bound")
		}
		hashes += uint64(len(values))
	}
	measurement := self.releaseStatsMeasurementWithLock()
	measurement.SettlementTransition = nil
	if measurement.Config != options.ExpectedConfig {
		return ReleaseStatsMeasurement{}, errors.New("compact native statistics configuration differs")
	}
	return measurement, nil
}

// Each recipient gets owned bounded raw slices; publication callbacks cannot
// mutate the returned cut or the retained generation/cursor authority.
func cloneReleaseStatsV2Measurement(measurement ReleaseStatsMeasurement) ReleaseStatsMeasurement {
	cloned := ReleaseStatsMeasurement{Config: measurement.Config, Providers: make([]ReleaseProviderMeasurement, len(measurement.Providers))}
	for index, provider := range measurement.Providers {
		provider.LatencyBuckets = append([]uint64(nil), provider.LatencyBuckets...)
		provider.EgressIPHashHexes = append([]string(nil), provider.EgressIPHashHexes...)
		cloned.Providers[index] = provider
	}
	return cloned
}

// A single checked row supplies a cursor's authenticated root, including a
// carried pre-activation row. The disk reader authenticates its canonical VPK
// record; no sequence-sized slice or map is retained here.
func releaseStatsV2PrefixRoot(ctx context.Context, ledger *AttemptLedger, sequence uint64) (string, error) {
	if sequence == 0 {
		return zeroAttemptHash(), nil
	}
	root, count := "", 0
	err := ledger.Walk(ctx, sequence, sequence, func(record AttemptRecord) error {
		if record.Sequence != sequence || record.Identity != ledger.identity {
			return errors.New("compact native cursor row differs from its ledger")
		}
		root, count = record.RecordHash, count+1
		return ctx.Err()
	})
	if err != nil {
		return "", err
	}
	if count != 1 {
		return "", errors.New("compact native cursor row is absent")
	}
	if _, err := canonicalAttemptHex32("compact native cursor root", root, false); err != nil {
		return "", err
	}
	return root, nil
}

// The caller retains the Stats token. Both routing and the exact applied head
// are checked independently of a caller-supplied or journal-carried header.
func (self *StatsEngine) releaseStatsV2OwnedHead(dir string) (AttemptLedgerHead, error) {
	ledger := self.attemptLedger
	if ledger == nil || !filepath.IsAbs(dir) || filepath.Clean(dir) != filepath.Dir(ledger.path) {
		return AttemptLedgerHead{}, errors.New("compact native statistics are not bound to this ledger directory")
	}
	if !self.settlementEpochKnown || self.attemptSettlementFirstSequence == 0 || self.attemptEgressFirstSequence < self.attemptSettlementFirstSequence {
		return AttemptLedgerHead{}, errors.New("compact native statistics cursor ownership is incomplete")
	}
	head, err := ledger.Head()
	if err != nil {
		return AttemptLedgerHead{}, err
	}
	if head.LastSequence == ^uint64(0) || head.LastSequence != self.attemptLastAppliedSequence || self.attemptEgressFirstSequence > head.LastSequence+1 {
		return AttemptLedgerHead{}, errors.New("compact native durable head differs from applied statistics")
	}
	return head, nil
}

// Generation and both cursors come from the retained Stats candidate. Retained
// activation must match independent authority; its settlement root must match
// the actual ledger row, never be silently replaced by the reconstructed root.
func (self *StatsEngine) releaseStatsV2Context(ctx context.Context, boundary AttemptBoundary, options releaseStatsV2Options) (AttemptCutV2Context, error) {
	if boundary.SettlementEpoch != self.settlementEpoch {
		return AttemptCutV2Context{}, errors.New("compact native boundary differs from active settlement")
	}
	if self.attemptV2 != nil && self.attemptV2.Activation != options.Activation {
		return AttemptCutV2Context{}, errors.New("compact native retained activation differs from independent authority")
	}
	prior, err := releaseStatsV2PrefixRoot(ctx, self.attemptLedger, self.attemptSettlementFirstSequence-1)
	if err != nil {
		return AttemptCutV2Context{}, err
	}
	if self.attemptV2 != nil && self.attemptV2.SettlementPriorRoot != prior {
		return AttemptCutV2Context{}, errors.New("compact native retained settlement root differs from actual ledger prefix")
	}
	expected := AttemptCutV2Context{
		Identity: self.attemptLedger.identity, Activation: options.Activation, Boundary: boundary,
		FirstSequence: self.attemptSettlementFirstSequence, EgressFirstSequence: self.attemptEgressFirstSequence,
		EgressGeneration: self.egressGeneration, PriorRoot: prior,
	}
	if _, err := attemptCutV2PolicyDepth(expected, options.Policy); err != nil {
		return AttemptCutV2Context{}, err
	}
	return expected, nil
}

// Replay scratch must be independent of the sealer and its child replay
// namespace. The underlying APIs retain all existing storage/census limits.
func validateReleaseStatsV2Scratch(options releaseStatsV2Options) error {
	seal, replay := options.Seal.ScratchDirectory, options.Stats.Replay.ScratchDirectory
	if !filepath.IsAbs(seal) || !filepath.IsAbs(replay) || filepath.Clean(seal) != seal || filepath.Clean(replay) != replay || filepath.Dir(seal) == seal || filepath.Dir(replay) == replay {
		return errors.New("compact native scratch paths are invalid")
	}
	for _, pair := range [][2]string{{seal, replay}, {replay, seal}} {
		relative, err := filepath.Rel(pair[0], pair[1])
		if err != nil || relative != ".." && !strings.HasPrefix(relative, "../") {
			return errors.New("compact native seal and statistics scratch overlap")
		}
	}
	return nil
}

// Drain and every external operation belong to one cancellable write owner.
// A completed snapshot writer still publishes if cancellation arrives during
// its commit, matching the existing Stats ownership/commit boundary.
func (self *StatsEngine) detachReleaseStatsMeasurementV2(ctx context.Context, dir string, boundary AttemptBoundary, options releaseStatsV2Options, persist func(ReleaseStatsMeasurement, AttemptCutV2) error) (ReleaseStatsMeasurement, *AttemptCutV2, error) {
	return self.detachReleaseStatsMeasurementV2WithJournalGuard(ctx, dir, boundary, options, persist, nil)
}

// The optional guard belongs to one retained journal owner, not global Stats
// state. Real replay and snapshot writing remain the existing operations.
func (self *StatsEngine) detachReleaseStatsMeasurementV2WithJournalGuard(ctx context.Context, dir string, boundary AttemptBoundary, options releaseStatsV2Options, persist func(ReleaseStatsMeasurement, AttemptCutV2) error, guard func(string) error) (resultMeasurement ReleaseStatsMeasurement, resultCut *AttemptCutV2, resultErr error) {
	if persist == nil {
		return ReleaseStatsMeasurement{}, nil, errors.New("compact native journal writer is absent")
	}
	if err := validateReleaseStatsV2Scratch(options); err != nil {
		return ReleaseStatsMeasurement{}, nil, err
	}
	owner, err := self.acquireStatsWrite(ctx, "detach-native-v2")
	if err != nil {
		return ReleaseStatsMeasurement{}, nil, err
	}
	defer owner.release()
	measurement, err := self.releaseStatsV2Measurement(options.Stats)
	if err != nil {
		return ReleaseStatsMeasurement{}, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, owner.finishSnapshot()) }()
	if err := owner.prepareSnapshot(dir, true); err != nil {
		return ReleaseStatsMeasurement{}, nil, err
	}
	candidate := owner.clone()
	cut, err := candidate.detachReleaseStatsMeasurementV2OwnedWithJournalGuard(owner, dir, boundary, measurement, options, persist, guard)
	finishErr := owner.finishSnapshot()
	if err == nil && finishErr != nil {
		return ReleaseStatsMeasurement{}, nil, finishErr
	}
	err = errors.Join(err, finishErr)
	owner.publish(candidate)
	if err != nil {
		return ReleaseStatsMeasurement{}, nil, err
	}
	return measurement, cut, nil
}

// Failed persistence retains the admission reservation and old egress. The
// callback receives owned values only after complete signed content replay.
func (self *StatsEngine) detachReleaseStatsMeasurementV2Owned(owner *statsWriteOwner, dir string, boundary AttemptBoundary, measurement ReleaseStatsMeasurement, options releaseStatsV2Options, persist func(ReleaseStatsMeasurement, AttemptCutV2) error) (*AttemptCutV2, error) {
	return self.detachReleaseStatsMeasurementV2OwnedWithJournalGuard(owner, dir, boundary, measurement, options, persist, nil)
}

// Retain the journal check after the existing before-snapshot observer and
// cancellation boundary as well. Snapshot bytes use their own retained native
// owner, separate from the journal's owner and replay authority.
func releaseStatsV2JournalPersist(owner *statsWriteOwner, guard func(string) error) func(statsSnapshotWrite) error {
	if guard == nil {
		return owner.persist
	}
	return func(write statsSnapshotWrite) error {
		return owner.persistChecked(write, func() error { return errors.Join(owner.check(), guard("before-save")) })
	}
}

// Before/after-save custody checks run outside every Stats mutex. A late
// guard error follows existing failed-Save rollback and reservation semantics.
func (self *StatsEngine) detachReleaseStatsMeasurementV2OwnedWithJournalGuard(owner *statsWriteOwner, dir string, boundary AttemptBoundary, measurement ReleaseStatsMeasurement, options releaseStatsV2Options, persist func(ReleaseStatsMeasurement, AttemptCutV2) error, guard func(string) error) (*AttemptCutV2, error) {
	if self.attemptSettlementCutPending {
		return nil, errAttemptCutPending
	}
	if !self.settlementEpochKnown || boundary.SettlementEpoch != self.settlementEpoch {
		return nil, errors.New("compact native boundary differs from active settlement")
	}
	if self.egressGeneration == ^uint64(0) {
		return nil, errors.New("compact native egress generation overflows")
	}
	head, err := self.releaseStatsV2OwnedHead(dir)
	if err != nil {
		return nil, err
	}
	self.attemptCutPending = true
	if self.activeAttemptCount != 0 {
		return nil, errAttemptCutPending
	}
	expected, err := self.releaseStatsV2Context(owner.ctx, boundary, options)
	if err != nil {
		return nil, err
	}
	cut, _, err := SealAttemptCutV2(owner.ctx, self.attemptLedger, expected, options.Policy, self.attemptLedger.vsk, options.Bounds, options.Seal)
	if err != nil {
		return nil, err
	}
	if cut.LastSequence != head.LastSequence || cut.Root != head.Root {
		return nil, errors.New("compact native sealer changed the owned applied prefix")
	}
	if _, _, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(owner.ctx, measurement, *cut, expected, options.Policy, options.Bounds, options.Stats); err != nil {
		return nil, err
	}
	currentHead, err := self.releaseStatsV2OwnedHead(dir)
	if err != nil || currentHead != head {
		return nil, errors.Join(errors.New("compact native head changed before journal publication"), err)
	}
	owner.step("before-journal")
	if err := owner.ctx.Err(); err != nil {
		return nil, err
	}
	if err := owner.check(); err != nil {
		return nil, err
	}
	currentHead, err = self.releaseStatsV2OwnedHead(dir)
	if err != nil || currentHead != head {
		return nil, errors.Join(errors.New("compact native head changed at journal admission"), err)
	}
	publication := *cut
	publication.Signature = bytes.Clone(cut.Signature)
	if err := persist(cloneReleaseStatsV2Measurement(measurement), publication); err != nil {
		return nil, err
	}
	currentHead, err = self.releaseStatsV2OwnedHead(dir)
	if err != nil || currentHead != head {
		return nil, errors.Join(errors.New("compact native head changed after journal publication"), err)
	}
	if guard != nil {
		if err := guard("before-save"); err != nil {
			return nil, err
		}
	}
	priorEgress, priorGeneration, priorFirst := self.egress, self.egressGeneration, self.attemptEgressFirstSequence
	self.egress, self.egressGeneration, self.attemptEgressFirstSequence = map[connect.Id]map[[32]byte]bool{}, priorGeneration+1, cut.LastSequence+1
	if err := self.saveOwned(dir, releaseStatsV2JournalPersist(owner, guard)); err != nil {
		self.egress, self.egressGeneration, self.attemptEgressFirstSequence = priorEgress, priorGeneration, priorFirst
		return nil, err
	}
	if guard != nil {
		if err := guard("after-save"); err != nil {
			self.egress, self.egressGeneration, self.attemptEgressFirstSequence = priorEgress, priorGeneration, priorFirst
			return nil, err
		}
	}
	if err := owner.checkSnapshot(); err != nil {
		self.egress, self.egressGeneration, self.attemptEgressFirstSequence = priorEgress, priorGeneration, priorFirst
		return nil, err
	}
	self.attemptCutPending = false
	return cut, nil
}

// Complete signed journal replay precedes any recovery mutation. A later
// generation is a true no-op, including its newer pending reservation. A
// same-generation restart retains only authenticated post-cut suffix egress.
func (self *StatsEngine) reconcileReleaseStatsCutV2(ctx context.Context, dir string, measurement ReleaseStatsMeasurement, cut AttemptCutV2, options releaseStatsV2Options) error {
	return self.reconcileReleaseStatsCutV2WithJournalGuard(ctx, dir, measurement, cut, options, nil)
}

// The caller's journal owner survives all real record/proof callbacks.
func (self *StatsEngine) reconcileReleaseStatsCutV2WithJournalGuard(ctx context.Context, dir string, measurement ReleaseStatsMeasurement, cut AttemptCutV2, options releaseStatsV2Options, guard func(string) error) (resultErr error) {
	owner, err := self.acquireStatsWrite(ctx, "reconcile-native-v2")
	if err != nil {
		return err
	}
	defer owner.release()
	if _, err := self.releaseStatsV2Measurement(options.Stats); err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, owner.finishSnapshot()) }()
	if err := owner.prepareSnapshot(dir, true); err != nil {
		return err
	}
	candidate := owner.clone()
	err = candidate.reconcileReleaseStatsCutV2OwnedWithJournalGuard(owner, dir, measurement, cut, options, guard)
	finishErr := owner.finishSnapshot()
	if err == nil && finishErr != nil {
		return finishErr
	}
	err = errors.Join(err, finishErr)
	owner.publish(candidate)
	return err
}

// A journal cannot choose the live identity, activation, settlement start or
// prefix root. Its historical native cursor/generation are checked against
// current ownership and its signature, never used as a new active generation.
func (self *StatsEngine) reconcileReleaseStatsCutV2Owned(owner *statsWriteOwner, dir string, measurement ReleaseStatsMeasurement, cut AttemptCutV2, options releaseStatsV2Options) error {
	return self.reconcileReleaseStatsCutV2OwnedWithJournalGuard(owner, dir, measurement, cut, options, nil)
}

// The journal guard and the separately retained snapshot witness both finish
// before releasing admission. Neither can substitute for real signed replay.
func (self *StatsEngine) reconcileReleaseStatsCutV2OwnedWithJournalGuard(owner *statsWriteOwner, dir string, measurement ReleaseStatsMeasurement, cut AttemptCutV2, options releaseStatsV2Options, guard func(string) error) error {
	if self.attemptSettlementCutPending {
		return errAttemptCutPending
	}
	head, err := self.releaseStatsV2OwnedHead(dir)
	if err != nil {
		return err
	}
	if cut.Context.EgressGeneration > self.egressGeneration || cut.LastSequence > head.LastSequence || cut.Context.Boundary.SettlementEpoch != self.settlementEpoch {
		return errors.New("compact native journal exceeds current generation, prefix or settlement")
	}
	expected, err := self.releaseStatsV2Context(owner.ctx, cut.Context.Boundary, options)
	if err != nil {
		return err
	}
	expected.EgressGeneration = cut.Context.EgressGeneration
	if self.egressGeneration == cut.Context.EgressGeneration {
		if cut.Context.EgressFirstSequence != self.attemptEgressFirstSequence {
			return errors.New("compact native journal starts at another egress cursor")
		}
	} else {
		if self.attemptEgressFirstSequence < cut.LastSequence+1 {
			return errors.New("advanced compact statistics omit the signed cut cursor")
		}
		expected.EgressFirstSequence = cut.Context.EgressFirstSequence
	}
	if err := cut.VerifyHeader(expected, options.Bounds); err != nil {
		return err
	}
	root, err := releaseStatsV2PrefixRoot(owner.ctx, self.attemptLedger, cut.LastSequence)
	if err != nil || root != cut.Root {
		return errors.Join(errors.New("compact native journal is not the owned ledger prefix"), err)
	}
	if options.retainedStartup {
		if _, err := newAttemptCutV2StatsProjection(owner.ctx, measurement, expected, options.Policy, options.Stats, nil); err != nil {
			return err
		}
	} else {
		if _, _, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(owner.ctx, measurement, cut, expected, options.Policy, options.Bounds, options.Stats); err != nil {
			return err
		}
	}
	currentHead, err := self.releaseStatsV2OwnedHead(dir)
	if err != nil || currentHead != head {
		return errors.Join(errors.New("compact native head changed during journal replay"), err)
	}
	if self.egressGeneration > cut.Context.EgressGeneration {
		return nil
	}
	if self.egressGeneration == ^uint64(0) {
		return errors.New("compact native journal egress generation overflows")
	}
	self.attemptCutPending = true
	if self.activeAttemptCount != 0 {
		return errAttemptCutPending
	}
	egress, err := self.releaseStatsV2SuffixEgress(owner.ctx, cut, head, options)
	if err != nil {
		return err
	}
	currentHead, err = self.releaseStatsV2OwnedHead(dir)
	if err != nil || currentHead != head {
		return errors.Join(errors.New("compact native head changed during journal reconciliation"), err)
	}
	if guard != nil {
		if err := guard("before-save"); err != nil {
			return err
		}
	}
	priorEgress, priorGeneration, priorFirst := self.egress, self.egressGeneration, self.attemptEgressFirstSequence
	self.egress, self.egressGeneration, self.attemptEgressFirstSequence = egress, priorGeneration+1, cut.LastSequence+1
	if err := self.saveOwned(dir, releaseStatsV2JournalPersist(owner, guard)); err != nil {
		self.egress, self.egressGeneration, self.attemptEgressFirstSequence = priorEgress, priorGeneration, priorFirst
		return err
	}
	if guard != nil {
		if err := guard("after-save"); err != nil {
			self.egress, self.egressGeneration, self.attemptEgressFirstSequence = priorEgress, priorGeneration, priorFirst
			return err
		}
	}
	if err := owner.checkSnapshot(); err != nil {
		self.egress, self.egressGeneration, self.attemptEgressFirstSequence = priorEgress, priorGeneration, priorFirst
		return err
	}
	self.attemptCutPending = false
	return nil
}

// Replays only later local rows into bounded egress sets, never quality
// counters. The signed journal ended every old trail; disk lifecycle ownership
// and full per-record signatures protect the independently replayed suffix.
func (self *StatsEngine) releaseStatsV2SuffixEgress(ctx context.Context, cut AttemptCutV2, head AttemptLedgerHead, options releaseStatsV2Options) (map[connect.Id]map[[32]byte]bool, error) {
	egress := map[connect.Id]map[[32]byte]bool{}
	if cut.LastSequence == head.LastSequence {
		return egress, nil
	}
	if head.LastSequence-cut.LastSequence > options.Bounds.Records.MaxItems {
		return nil, errors.New("compact native suffix record bound exceeded")
	}
	depth, err := attemptCutV2PolicyDepth(cut.Context, options.Policy)
	if err != nil {
		return nil, err
	}
	vpk, err := canonicalAttemptHex32("compact native suffix validator", self.attemptLedger.identity.ValidatorVPK, false)
	if err != nil {
		return nil, err
	}
	previous, sequence, hashes := cut.Root, cut.LastSequence, uint64(0)
	err = self.attemptLedger.Walk(ctx, cut.LastSequence+1, head.LastSequence, func(record AttemptRecord) error {
		if record.Sequence != sequence+1 || record.PreviousHash != previous || record.Identity != self.attemptLedger.identity || record.Boundary.SettlementEpoch != self.settlementEpoch || record.M != depth {
			return errors.New("compact native suffix does not extend the signed journal")
		}
		if err := VerifyAttemptRecord(&record, self.attemptLedger.identity, vpk[:], options.Stats.Replay.ServerKeys); err != nil {
			return err
		}
		if record.Disposition == AttemptDispositionComplete {
			if record.Proof == nil {
				return errors.New("compact native suffix complete record has no proof")
			}
			for index := 1; index < len(record.Proof.Hops); index++ {
				hop := record.Proof.Hops[index]
				if hop.EgressIpHash == ([32]byte{}) {
					continue
				}
				if egress[hop.ClientId] == nil {
					if uint64(len(egress)) >= options.Stats.MaxProviders {
						return errors.New("compact native suffix provider bound exceeded")
					}
					egress[hop.ClientId] = map[[32]byte]bool{}
				}
				if !egress[hop.ClientId][hop.EgressIpHash] {
					if hashes >= options.Stats.MaxEgressHashes {
						return errors.New("compact native suffix hash bound exceeded")
					}
					egress[hop.ClientId][hop.EgressIpHash] = true
					hashes++
				}
			}
		}
		previous, sequence = record.RecordHash, record.Sequence
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	if sequence != head.LastSequence || previous != head.Root {
		return nil, fmt.Errorf("compact native suffix differs from applied head %d", head.LastSequence)
	}
	return egress, nil
}
