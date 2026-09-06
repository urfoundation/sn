//go:build linux || darwin

package validator

// Complete policy-aware replay stages provider/quality/egress mutations under
// one private generation. The committed header cannot advance until every
// record, proof projection, reader and replay scratch close has succeeded.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
)

// A stage holds fixed-size headers and one count/byte-bounded row batch. The
// full cut remains the replay authority; this is not a validation capability.
type statsAggregateStage struct {
	store         *statsAggregateStore
	head          statsAggregateHead
	old           statsAggregateHead
	matchedPrefix bool
	points        *statsAggregateBatch
}

// A single owned writer never crosses another writer or a partial failed stage.
// Readers may continue observing the old coherent generation during replay.
func (self *statsAggregateStore) Replay(ctx context.Context, cut AttemptCutV2, expected AttemptCutV2Context, policy protocol.Policy, bounds AttemptCutV2Bounds, options AttemptCutV2ReplayOptions, advance statsAggregateAdvance) (statsAggregateHead, error) {
	operationCtx, done, err := self.begin(ctx)
	if err != nil {
		return statsAggregateHead{}, err
	}
	defer done()
	select {
	case self.writeGate <- struct{}{}:
	case <-operationCtx.Done():
		return statsAggregateHead{}, operationCtx.Err()
	}
	defer func() { <-self.writeGate }()
	if err := self.check(operationCtx); err != nil {
		return statsAggregateHead{}, err
	}
	self.stateLock.Lock()
	needsRecovery := self.needsRecovery
	self.stateLock.Unlock()
	if needsRecovery {
		return statsAggregateHead{}, errors.New("private aggregate requires reopen after its failed stage")
	}
	old, err := self.readHead(self.db)
	if err != nil {
		return statsAggregateHead{}, err
	}
	if err := statsAggregatePolicyConfig(expected, policy, old.Config); err != nil {
		return statsAggregateHead{}, err
	}
	if err := cut.VerifyHeader(expected, bounds); err != nil {
		return statsAggregateHead{}, err
	}
	if options.VisitRecord != nil {
		return statsAggregateHead{}, errors.New("private aggregate owns its staging record visitor")
	}
	if old.Identity != expected.Identity || old.Activation != expected.Activation || old.SettlementEpoch != expected.Boundary.SettlementEpoch || old.SettlementFirstSequence != expected.FirstSequence || old.SettlementPriorRoot != expected.PriorRoot || old.EgressGeneration != expected.EgressGeneration || old.EgressFirstSequence != expected.EgressFirstSequence || cut.LastSequence < old.LastAppliedSequence || !releaseBlockAtOrBefore(old.Boundary.EVMBlock, old.Boundary.EVMBlockHash, expected.Boundary.EVMBlock, expected.Boundary.EVMBlockHash) {
		return statsAggregateHead{}, errors.New("private aggregate cut differs from its committed namespace, prefix or clocks")
	}
	if old.Generation >= ^uint64(0)-1 || advance.RotateNative && old.EgressGeneration == ^uint64(0) || advance.ToSettlementEpoch != 0 && (old.SettlementEpoch == ^uint64(0) || advance.ToSettlementEpoch != old.SettlementEpoch+1 || !advance.RotateNative) {
		return statsAggregateHead{}, errors.New("private aggregate advance is invalid or overflows")
	}
	if err := options.Bounds.validate(bounds); err != nil {
		return statsAggregateHead{}, err
	}
	if options.ReadMetadata == nil || options.OpenData == nil {
		return statsAggregateHead{}, errors.New("private aggregate replay readers are missing")
	}
	keys := make(map[byte]ed25519.PublicKey, len(options.ServerKeys))
	for keyID, key := range options.ServerKeys {
		if len(key) != ed25519.PublicKeySize {
			return statsAggregateHead{}, errors.New("private aggregate server key length differs")
		}
		keys[keyID] = bytes.Clone(key)
	}
	options.ServerKeys = keys
	cut.Signature = bytes.Clone(cut.Signature)
	if err := self.disk.step("aggregate-before-replay", ""); err != nil {
		return statsAggregateHead{}, err
	}
	if err := self.recoverRows(operationCtx, old); err != nil {
		// Physical writers/readers already latch their own failures. A
		// canceled cleanup is incomplete recovery, not a failed filesystem.
		self.stateLock.Lock()
		self.needsRecovery = true
		self.stateLock.Unlock()
		return statsAggregateHead{}, err
	}
	stage := statsAggregateStage{store: self, head: old, old: old, matchedPrefix: old.LastAppliedSequence == expected.FirstSequence-1 && old.LastAppliedRoot == expected.PriorRoot}
	stage.head.Generation++
	stage.head.Boundary = expected.Boundary
	options.VisitRecord = func(record AttemptRecord) error { return stage.record(operationCtx, record) }
	self.stateLock.Lock()
	self.needsRecovery = true
	self.stateLock.Unlock()
	// This public entry point independently rechecks both policy and header.
	// Its successful return includes every stream EOF and owned scratch close.
	if _, err := ReplayAttemptCutV2WithPolicy(operationCtx, cut, expected, policy, bounds, options); err != nil {
		return statsAggregateHead{}, err
	}
	if !stage.matchedPrefix {
		return statsAggregateHead{}, errors.New("private aggregate replay did not match the prior applied root")
	}
	stage.head.LastAppliedSequence, stage.head.LastAppliedRoot = cut.LastSequence, cut.Root
	if advance.ToSettlementEpoch != 0 {
		if err := stage.fold(operationCtx); err != nil {
			return statsAggregateHead{}, err
		}
		stage.head.SettlementEpoch = advance.ToSettlementEpoch
		stage.head.SettlementFirstSequence = cut.LastSequence + 1
		stage.head.SettlementPriorRoot = cut.Root
	}
	if advance.RotateNative {
		stage.head.EgressGeneration++
		stage.head.EgressFirstSequence = cut.LastSequence + 1
		stage.head.EgressHashCount, stage.head.EgressClaimCount = 0, 0
	}
	if err := stage.head.validate(self.bounds); err != nil {
		return statsAggregateHead{}, err
	}
	return stage.publish(operationCtx)
}

// Every admitted point is one fixed-size row owned by this generation. The
// publication helper below is the only path that makes the generation visible.
func (self *statsAggregateStage) putPoint(ctx context.Context, base, value []byte) error {
	if err := self.store.check(ctx); err != nil {
		return err
	}
	// Refuse malformed caller input before allocating its versioned key.
	if len(base) != 17 && len(base) != 57 && len(base) != 65 {
		return errors.New("private aggregate point key length differs")
	}
	if self.points == nil {
		self.points = newStatsAggregateBatch(self.store, false)
	}
	return self.points.append(ctx, statsAggregateVersionKey(base, self.head.Generation), value)
}

// Every provider, empty egress value and complete claim reads through the same
// current bounded batch before its generation-aware durable lookup.
func (self *statsAggregateStage) point(base []byte) ([]byte, bool, error) {
	if self.points != nil {
		value, found, err := self.points.pending(statsAggregateVersionKey(base, self.head.Generation))
		if err != nil || found {
			return value, found, err
		}
	}
	return statsAggregatePoint(self.store.db, base, self.head.Generation)
}

// Flushes no more than one pending bounded batch before a scan or publication.
func (self *statsAggregateStage) flush(ctx context.Context) error {
	if self.points != nil {
		return self.points.flush(ctx)
	}
	return self.store.check(ctx)
}

// The caller has completed full replay and all final census/clock checks. This
// private persistence boundary is not a capability to bypass authentication.
func (self *statsAggregateStage) publish(ctx context.Context) (statsAggregateHead, error) {
	if err := self.flush(ctx); err != nil {
		return statsAggregateHead{}, err
	}
	if self.store.hooks.BeforeCommit != nil {
		if err := self.store.hooks.BeforeCommit(self.head); err != nil {
			return statsAggregateHead{}, err
		}
	}
	if err := self.store.disk.step("aggregate-before-publish", ""); err != nil {
		return statsAggregateHead{}, err
	}
	if err := self.store.check(ctx); err != nil {
		return statsAggregateHead{}, err
	}
	raw, err := json.Marshal(self.head)
	if err != nil {
		return statsAggregateHead{}, err
	}
	// Successful synced publication is the commit point. Later cancellation
	// cannot pretend that a committed generation vanished.
	if err := self.store.db.Put([]byte{'h'}, raw, &opt.WriteOptions{Sync: true, NoWriteMerge: true}); err != nil {
		self.store.latchFault(err)
		return statsAggregateHead{}, err
	}
	self.store.stateLock.Lock()
	fault := self.store.fault
	if fault == nil {
		self.store.needsRecovery = false
	}
	self.store.stateLock.Unlock()
	if fault != nil {
		return statsAggregateHead{}, fault
	}
	return self.head, nil
}

// Previously counted checkpoints are fully reauthenticated, but only a newly
// terminal record updates counters. The exact old root binds suffix reuse.
func (self *statsAggregateStage) record(ctx context.Context, record AttemptRecord) error {
	if err := self.store.check(ctx); err != nil {
		return err
	}
	if record.Sequence == self.old.LastAppliedSequence {
		if record.RecordHash != self.old.LastAppliedRoot {
			return errors.New("private aggregate replay changes the prior applied root")
		}
		self.matchedPrefix = true
	}
	if record.Sequence <= self.old.LastAppliedSequence {
		return nil
	}
	if !self.matchedPrefix {
		return errors.New("private aggregate suffix precedes its verified prior root")
	}
	if record.Disposition == AttemptDispositionPending {
		return nil
	}
	for _, assignment := range record.Assignments {
		row, exists, err := self.provider(assignment.NextHop)
		if err != nil {
			return err
		}
		if !exists && self.head.ProviderCount == self.store.bounds.MaxProviders {
			return errors.New("private aggregate provider count exceeds its bound")
		}
		if row.Window.Assignments == ^uint64(0) || assignment.Confirmed && (row.Window.Confirmations == ^uint64(0) || int(assignment.LatencyBucket) >= statsLatencyBuckets || row.Window.LatencyBuckets[assignment.LatencyBucket] == ^uint64(0)) {
			return errors.New("private aggregate provider counter overflows")
		}
		row.WindowPresent = true
		row.Window.Assignments++
		if assignment.Confirmed {
			row.Window.Confirmations++
			row.Window.LatencyBuckets[assignment.LatencyBucket]++
		}
		if err := row.validate(self.head.Config); err != nil {
			return err
		}
		if err := self.putPoint(ctx, statsAggregateProviderKey(row.ClientID), row.encode()); err != nil {
			return err
		}
		if !exists {
			self.head.ProviderCount++
		}
	}
	if record.Disposition != AttemptDispositionComplete || record.Proof == nil || record.Sequence < self.head.EgressFirstSequence {
		return nil
	}
	for index := 1; index < len(record.Proof.Hops); index++ {
		hop := record.Proof.Hops[index]
		if hop.EgressIpHash == ([32]byte{}) {
			continue
		}
		key := statsAggregateEgressKey(self.head.EgressGeneration, hop.ClientId, hop.EgressIpHash)
		_, exists, err := self.point(key)
		if err != nil {
			return err
		}
		if !exists {
			if self.head.EgressHashCount == self.store.bounds.MaxEgressHashes {
				return errors.New("private aggregate egress count exceeds its bound")
			}
			if err := self.putPoint(ctx, key, nil); err != nil {
				return err
			}
			self.head.EgressHashCount++
		}
		var binding AttemptBinding
		found := false
		// The signed protocol bounds one trail to at most sixteen hops; this
		// search does not allocate a provider- or history-sized binding map.
		for _, assignment := range record.Assignments {
			if assignment.NextHop == hop.ClientId {
				binding, found = assignment.Binding, true
				break
			}
		}
		if !found {
			return errors.New("private aggregate complete hop has no signed binding")
		}
		if !binding.Active || !binding.UIDFound {
			continue
		}
		value, err := encodeStatsAggregateClaim(binding)
		if err != nil {
			return err
		}
		key = statsAggregateClaimKey(self.head.EgressGeneration, record.Sequence, hop.ClientId, hop.EgressIpHash)
		prior, exists, err := self.point(key)
		if err != nil {
			return err
		}
		if exists {
			if !bytes.Equal(prior, value) {
				return errors.New("private aggregate exact claim key has conflicting binding")
			}
			continue
		}
		if self.head.EgressClaimCount == self.store.bounds.MaxEgressClaims {
			return errors.New("private aggregate claim count exceeds its bound")
		}
		if err := self.putPoint(ctx, key, value); err != nil {
			return err
		}
		self.head.EgressClaimCount++
	}
	return nil
}

// A single provider lookup has one exact version seek and one fixed-size value.
func (self *statsAggregateStage) provider(clientID connect.Id) (statsAggregateProvider, bool, error) {
	raw, exists, err := self.point(statsAggregateProviderKey(clientID))
	if err != nil {
		return statsAggregateProvider{}, false, err
	}
	if !exists {
		return statsAggregateProvider{ClientID: clientID}, false, nil
	}
	row, err := decodeStatsAggregateProvider(clientID, raw, self.head.Config)
	return row, true, err
}

// A settlement fold scans every identity with bounded memory and updates only
// that provider's fixed row. Sparse epochs retain both independent priors.
func (self *statsAggregateStage) fold(ctx context.Context) error {
	if err := self.flush(ctx); err != nil {
		return err
	}
	var count uint64
	err := walkStatsAggregateRows(ctx, self.store.db, []byte{'p'}, 17, self.head.Generation, func(key, value []byte) error {
		var clientID connect.Id
		copy(clientID[:], key[1:])
		row, err := decodeStatsAggregateProvider(clientID, value, self.head.Config)
		if err != nil {
			return err
		}
		ppm, hasPPM, ema, hasEMA, err := row.quality(self.head.Config)
		if err != nil {
			return err
		}
		row.PriorQualityPPM, row.HasPriorQuality = ppm, hasPPM
		row.PriorEMA, row.HasPriorEMA = ema, hasEMA
		row.Window, row.WindowPresent = ProviderWindow{}, false
		if err := self.putPoint(ctx, key, row.encode()); err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		return err
	}
	if count != self.head.ProviderCount {
		return fmt.Errorf("private aggregate fold census %d differs from %d", count, self.head.ProviderCount)
	}
	return nil
}
