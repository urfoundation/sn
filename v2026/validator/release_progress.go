//go:build linux || darwin

// Real operation owners publish scalar observations after releasing custody.
// The optional status worker owns no signer, replay reader or protocol retry.
package validator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Memory updates never perform filesystem I/O or hold a protocol owner. The
// sequence rejects a delayed callback after a later operation has completed.
type releaseProgress struct {
	stateLock          sync.Mutex
	value              protocol.ValidatorProgress
	sequence           atomic.Uint64
	intentSequence     uint64
	settlementSequence uint64
	now                func() time.Time
	publisher          *releaseProgressPublisher
	diagnostics        *releaseDiagnostics
}

// Configuration hashing uses the same complete canonical body as independent
// production approval; the telemetry path is deliberately outside that body.
func newReleaseProgress(ctx context.Context, cfg *ReleaseConfig, path string) (*releaseProgress, error) {
	if path == "" {
		return nil, nil
	}
	if ctx == nil || cfg == nil {
		return nil, errors.New("validator progress owner is absent")
	}
	source, err := releaseProgressSource(cfg)
	if err != nil {
		return nil, err
	}
	var instance [16]byte
	if _, err := rand.Read(instance[:]); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	self := &releaseProgress{now: time.Now, diagnostics: releaseDiagnosticOwner(ctx), value: protocol.ValidatorProgress{
		Schema: protocol.ValidatorProgressSchema, Source: source,
		InstanceId: hex.EncodeToString(instance[:]), StartedAt: now, HeartbeatAt: now,
		Publisher: protocol.ValidatorPublicationObservation{Outcome: "starting"},
	}}
	if err := self.value.Validate(); err != nil {
		return nil, err
	}
	self.publisher = newReleaseProgressPublisher(ctx, self, path, cfg.StateDir, func(state string) {
		releaseDiagnostic(ctx, "progress", state, 0, false, 0)
	})
	return self, nil
}

// Original intent configuration is resolved by the real replay path's exact
// authority selector, never replaced with the currently running config hash.
func releaseProgressSource(cfg *ReleaseConfig) (protocol.ValidatorProgressSource, error) {
	hash, err := OwnerRecycleConfigHash(cfg)
	if err != nil {
		return protocol.ValidatorProgressSource{}, err
	}
	return protocol.ValidatorProgressSource{ConfigHash: "sha256:" + hex.EncodeToString(hash[:]),
		DeploymentId: cfg.DeploymentID, ValidatorId: cfg.ValidatorID, ChainId: cfg.ChainID,
		GenesisHash: strings.ToLower(cfg.GenesisHash), Netuid: cfg.Netuid}, nil
}

// Shutdown joins the optional worker without changing the core operation's
// result. Publication failures are operational events, never signing failures.
func (self *releaseProgress) close() {
	if self != nil && self.publisher != nil {
		self.publisher.close()
	}
}

// Allocate while the real operation is owned, before releasing its gate.
func (self *releaseProgress) nextSequence() uint64 {
	if self == nil {
		return 0
	}
	return self.sequence.Add(1)
}

// This outer defer runs after the actual custody-close defer. It releases the
// intent owner before publishing and keeps telemetry errors out of its result.
func (self *IntentStore) finishProgressV2(release func(), sequence uint64, intent *SteeringIntent, operationErr error) {
	progress := self.v2.runtime.progress
	release()
	var value *protocol.ValidatorIntentProgress
	if progress != nil && operationErr == nil {
		value, operationErr = releaseIntentProgress(&self.v2.runtime.cfg, intent)
	}
	progress.observeIntent(sequence, value, operationErr)
}

// Copy only finite fields from the already authenticated result. This does not
// read a store or accept candidate artifact fields as protocol authority.
func releaseIntentProgress(cfg *ReleaseConfig, intent *SteeringIntent) (*protocol.ValidatorIntentProgress, error) {
	if intent == nil {
		return nil, nil
	}
	original, err := productionConfigForIntent(cfg, intent)
	if err != nil {
		return nil, err
	}
	source, err := releaseProgressSource(original)
	if err != nil {
		return nil, err
	}
	progressAt := intent.UpdatedAt
	if intent.Status == "pending" {
		progressAt = intent.CreatedAt
	}
	value := &protocol.ValidatorIntentProgress{ConfigHash: source.ConfigHash, VectorHash: intent.VectorHash,
		NativeEpoch: intent.SubnetEpoch, SettlementEpoch: intent.SettlementEpoch,
		Status: intent.Status, CreatedAt: intent.CreatedAt, ProgressAt: progressAt,
		FinalizedBlock: intent.FinalizedBlock, RevealBlock: intent.RevealBlock, ApplicationBlock: intent.ApplicationBlock}
	if intent.Prepared != nil {
		value.PreparedAtBlock = intent.Prepared.PreparedAtBlock
		if value.RevealBlock == 0 {
			value.RevealBlock = intent.Prepared.RevealBlock
		}
	}
	return value, nil
}

// A late custody or publication error preserves the last good intent without
// reporting the failed operation as a successful current observation.
func (self *releaseProgress) observeIntent(sequence uint64, value *protocol.ValidatorIntentProgress, operationErr error) {
	if self == nil {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if sequence <= self.intentSequence {
		return
	}
	self.intentSequence = sequence
	now := self.now().UTC().Format(time.RFC3339Nano)
	observation := self.value.Intent
	if observation == nil {
		observation = &protocol.ValidatorIntentObservation{}
		self.value.Intent = observation
	}
	observation.ObservedAt, observation.Current = now, operationErr == nil
	if operationErr != nil {
		return
	}
	if value != nil {
		copy := *value
		if prior := observation.Value; prior != nil && prior.VectorHash == copy.VectorHash && prior.Status == copy.Status &&
			prior.FinalizedBlock == copy.FinalizedBlock && prior.ApplicationBlock == copy.ApplicationBlock {
			copy.ProgressAt = prior.ProgressAt
		}
		value = &copy
	}
	observation.Value, observation.LastSuccessAt = value, now
}

// A successful real scheduler read supplies its complete fixed-size schedule.
// Ordinary read failures preserve the previous schedule as unavailable evidence.
func (self *releaseProgress) observeNative(state *crv4.EpochScheduleState, operationErr error) {
	if self == nil {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	now := self.now().UTC().Format(time.RFC3339Nano)
	value := self.value.Native
	if value == nil {
		value = &protocol.ValidatorNativeObservation{}
		self.value.Native = value
	}
	value.ObservedAt, value.Current = now, operationErr == nil && state != nil
	if value.Current {
		value.LastSuccessAt = now
		value.Block, value.Epoch = state.CurrentBlock, state.SubnetEpochIndex
		value.LastEpochBlock, value.PendingEpochAt = state.LastEpochBlock, state.PendingEpochAt
		value.Tempo, value.BlocksSinceLastStep = state.Tempo, state.BlocksSinceLastStep
	}
}

// The core loop supplies a closed classification after applying its existing
// retry policy. Success means a complete observation, not new protocol progress.
func (self *releaseProgress) observeSteering(epoch uint64, known bool, outcome string, success bool) {
	if self == nil {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	now := self.now().UTC().Format(time.RFC3339Nano)
	lastSuccess := ""
	if self.value.Steering != nil {
		lastSuccess = self.value.Steering.LastSuccessAt
	}
	if success {
		lastSuccess = now
	}
	if !known {
		epoch = 0
	}
	self.value.Steering = &protocol.ValidatorSteeringObservation{ObservedAt: now, LastSuccessAt: lastSuccess,
		Current: success, EpochKnown: known, NativeEpoch: epoch, Outcome: outcome}
}

// Copy under the existing runtime gate, then publish after its release. The
// current durable cursor remains meaningful when a later public write retries.
func (self *releaseRuntimeV2) progressSettlement(target uint64) *protocol.ValidatorSettlementObservation {
	if self.progress == nil || self.history == nil || len(self.history.participants) == 0 {
		return nil
	}
	value := &protocol.ValidatorSettlementObservation{CursorKnown: true, TargetEpoch: target, PendingPublications: uint64(len(self.publications))}
	first := true
	for _, participant := range self.history.participants {
		cursor, found := self.history.current[participant.NoID]
		if !found || !first && cursor.epoch != value.Epoch {
			return nil
		}
		value.Epoch, first = cursor.epoch, false
	}
	first = true
	for epoch := range self.publications {
		if first || epoch < value.FirstPendingEpoch {
			value.FirstPendingEpoch, first = epoch, false
		}
	}
	return value
}

// Only an actual durable cursor advance updates its progress timestamp.
// Startup and repeated successful reconciliation remain separate observations.
func (self *releaseProgress) observeSettlement(sequence uint64, value *protocol.ValidatorSettlementObservation, operationErr error) {
	if self == nil {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if sequence <= self.settlementSequence {
		return
	}
	self.settlementSequence = sequence
	now := self.now().UTC().Format(time.RFC3339Nano)
	prior := self.value.Settlement
	if value == nil {
		operationErr = errors.Join(operationErr, errors.New("settlement observation is unavailable"))
		if prior == nil {
			prior = &protocol.ValidatorSettlementObservation{}
		}
		copy := *prior
		value = &copy
	} else {
		copy := *value
		value = &copy
	}
	if prior != nil {
		value.LastSuccessAt, value.ProgressAt = prior.LastSuccessAt, prior.ProgressAt
		if value.CursorKnown && prior.CursorKnown && value.Epoch > prior.Epoch {
			value.ProgressAt = now
		}
	}
	value.ObservedAt, value.Current = now, operationErr == nil
	if value.Current {
		value.LastSuccessAt = now
	}
	self.value.Settlement = value
}

// Record only after real publication returns. A snapshot contains the previous
// acknowledged write, so a post-rename failure never refreshes exporter success.
func (self *releaseProgress) observePublication(success bool) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if success {
		self.value.Publisher.Outcome = "published"
		self.value.Publisher.LastSuccessAt = self.now().UTC().Format(time.RFC3339Nano)
	} else {
		self.value.Publisher.Outcome = "retrying"
	}
}

// Retained output supplies operational age only. A fresh successful callback
// wins; an earlier failed callback must not hide old good evidence on restart.
func (self *releaseProgress) retain(previous *protocol.ValidatorProgress) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.value.Publisher.LastSuccessAt == "" {
		self.value.Publisher.LastSuccessAt = previous.Publisher.LastSuccessAt
	}
	if previous.Intent != nil {
		copy := *previous.Intent
		copy.Current = false
		if copy.Value != nil {
			value := *copy.Value
			copy.Value = &value
		}
		if current := self.value.Intent; current == nil {
			self.value.Intent = &copy
		} else if current.LastSuccessAt == "" {
			current.LastSuccessAt, current.Value = copy.LastSuccessAt, copy.Value
		} else if current.Value != nil && copy.Value != nil &&
			current.Value.ConfigHash == copy.Value.ConfigHash && current.Value.VectorHash == copy.Value.VectorHash &&
			current.Value.Status == copy.Value.Status && current.Value.FinalizedBlock == copy.Value.FinalizedBlock &&
			current.Value.ApplicationBlock == copy.Value.ApplicationBlock {
			current.Value.ProgressAt = copy.Value.ProgressAt
		}
	}
	if previous.Native != nil {
		copy := *previous.Native
		copy.Current = false
		if current := self.value.Native; current == nil {
			self.value.Native = &copy
		} else if current.LastSuccessAt == "" {
			copy.ObservedAt = current.ObservedAt
			self.value.Native = &copy
		}
	}
	if previous.Settlement != nil {
		copy := *previous.Settlement
		copy.Current = false
		if current := self.value.Settlement; current == nil {
			self.value.Settlement = &copy
		} else if !current.CursorKnown {
			copy.ObservedAt = current.ObservedAt
			self.value.Settlement = &copy
		} else {
			if current.LastSuccessAt == "" {
				current.LastSuccessAt = copy.LastSuccessAt
			}
			if current.Epoch == copy.Epoch && copy.CursorKnown && current.ProgressAt == "" {
				current.ProgressAt = copy.ProgressAt
			}
		}
	}
	if previous.Steering != nil {
		copy := *previous.Steering
		copy.Current = false
		copy.Outcome = "starting"
		if current := self.value.Steering; current == nil {
			self.value.Steering = &copy
		} else if current.LastSuccessAt == "" {
			current.LastSuccessAt = copy.LastSuccessAt
		}
	}
}

// Encoding happens under the short memory lock. Filesystem writes happen only
// after return, so a blocked or failing exporter cannot own protocol progress.
func (self *releaseProgress) snapshot() ([]byte, error) {
	// Delivery observations precede this heartbeat and hold no progress lock.
	// Nested progress fields remain protected through their bounded encoding.
	diagnosticAt := self.now().UTC()
	diagnostic := self.diagnostics.snapshot(diagnosticAt)
	now := self.now().UTC()
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	value := self.value
	value.HeartbeatAt = now.Format(time.RFC3339Nano)
	value.Diagnostics = diagnostic
	return value.Encode()
}
