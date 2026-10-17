// One root service owns a bounded decision stream and its single approved
// existing-seat action. Its journal atomically couples decision and intent;
// independent ports supply current authority, custody and submission.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"time"
)

const rootServiceConfigSchema = "urnetwork-mainnet-root-service-config-v1"
const rootServiceStateSchema = "urnetwork-mainnet-root-service-state-v1"

// Configuration is independently provisioned, never learned from journal data.
// The approved action state path is this service's composite journal path.
type rootServiceConfig struct {
	Schema              string                   `json:"schema"`
	CustodyTrust        rootOfflineCustodyTrust  `json:"custody_trust"`
	Packet              rootOfflineCustodyPacket `json:"approved_packet"`
	MaximumObservations uint32                   `json:"maximum_observations"`
}

// Only one explicitly approved basket and bounded observation allowance exist.
func (self rootServiceConfig) validate() error {
	if self.Schema != rootServiceConfigSchema || self.MaximumObservations == 0 || self.MaximumObservations > 10000 {
		return errors.New("root service requires its exact schema and 1..10000 lifetime observations")
	}
	return self.Packet.validate(self.CustodyTrust)
}

// Configuration copies cannot retain mutable caller-owned policy vectors.
func copyRootServiceConfig(config rootServiceConfig) rootServiceConfig {
	config.Packet.Action = copyRootAction(config.Packet.Action)
	config.Packet.Approval.Action = copyRootAction(config.Packet.Approval.Action)
	return config
}

// Interrupted observations consume their attempt and preserve the last complete
// decision. Once active, all future steps reconcile the same exact action.
type rootServiceRecord struct {
	Schema               string                     `json:"schema"`
	Config               rootServiceConfig          `json:"config"`
	Phase                string                     `json:"phase"`
	Observations         uint32                     `json:"observations"`
	ObservationPending   bool                       `json:"observation_pending"`
	Decision             *rootWeightDecision        `json:"last_decision,omitempty"`
	RecoveredSignature   bool                       `json:"recovered_signature,omitempty"`
	SubmissionConfigHash string                     `json:"submission_config_hash,omitempty"`
	SubmissionPrepared   bool                       `json:"submission_prepared,omitempty"`
	Recovery             *rootServiceRecoveryRecord `json:"recovery,omitempty"`
	Action               rootActionRecord           `json:"action"`
	ContentHash          string                     `json:"content_hash"`
}

// Both approval and retained decision are revalidated under independent config.
// Completed, missing or failed state can never create another action allowance.
func (self rootServiceRecord) validate(config rootServiceConfig) error {
	if err := config.validate(); err != nil {
		return err
	}
	claimed := self.ContentHash
	self.ContentHash = ""
	if self.Schema != rootServiceStateSchema || rootObjectHash(self.Config) != rootObjectHash(config) || claimed != rootObjectHash(self) ||
		self.Observations > config.MaximumObservations || self.Action.Action.RequestHash != config.Packet.Action.RequestHash ||
		self.SubmissionConfigHash != "" && !planSha256(self.SubmissionConfigHash) || self.SubmissionPrepared && self.SubmissionConfigHash == "" {
		return errors.New("root service state differs from its original configuration or bounds")
	}
	if err := self.Action.validate(); err != nil {
		return err
	}
	if self.Recovery != nil {
		if err := self.Recovery.validate(); err != nil {
			return err
		}
	}
	if self.Decision != nil {
		if err := self.Decision.validate(self.Action.Action); err != nil {
			return err
		}
		if self.Decision.Attempt > self.Observations || !self.ObservationPending && self.Decision.Attempt != self.Observations || self.ObservationPending && self.Decision.Attempt >= self.Observations {
			return errors.New("root service decision does not match its retained observation attempt")
		}
	} else if !self.ObservationPending && self.Observations != 0 {
		return errors.New("root service completed an observation without retaining its decision")
	}
	if self.ObservationPending && self.Observations == 0 {
		return errors.New("root service has an uncounted observation")
	}
	// A public native signature proves an existing liability even if no local
	// decision ever completed. Its receipt-only recovery must not invent an
	// intent, erase a consumed observation or create a fresh mutation allowance.
	if self.RecoveredSignature {
		if self.Action.Signature == "" || self.Phase != "active" && self.Phase != "complete" ||
			(self.Phase == "complete") != (self.Action.Reconciliation != nil) ||
			self.Decision != nil && (self.Action.LastFinalized < self.Decision.Observation.Position.FinalizedNumber ||
				self.Action.LastFinalized == self.Decision.Observation.Position.FinalizedNumber && self.Action.LastFinalizedHash != self.Decision.Observation.Position.FinalizedHash) {
			return errors.New("root service recovered liability lacks exact signed bytes or retained decision continuity")
		}
		return nil
	}
	switch self.Phase {
	case "observing":
		if self.Action.Phase != "reserved" || self.Action.LastFinalized != 0 || self.Decision != nil && self.Decision.Outcome == "intent" {
			return errors.New("root service observing phase carries an active intent")
		}
	case "active", "complete":
		if self.ObservationPending || self.Decision == nil || self.Decision.Outcome != "intent" || self.Action.LastFinalized < self.Decision.Observation.Position.FinalizedNumber ||
			self.Action.LastFinalized == self.Decision.Observation.Position.FinalizedNumber && self.Action.LastFinalizedHash != self.Decision.Observation.Position.FinalizedHash ||
			(self.Phase == "complete") != (self.Action.Reconciliation != nil) {
			return errors.New("root service action lacks its original durable decision or terminal evidence")
		}
	default:
		return errors.New("root service phase is unknown")
	}
	return nil
}

// The caller closes the private store after joining all service operations.
type rootServiceStorage interface {
	load() (rootServiceRecord, error)
	save(rootServiceRecord) error
}

// Observation can propose an intent but cannot supply signer or spend authority.
type rootServiceObserver interface {
	observeRootWeights(context.Context, rootAction) (rootWeightObservation, error)
}

// Canonical receipt ownership is separate from mutation capability. In
// particular, attaching the read-only chain port never enables submission.
type rootServiceReconciler interface {
	reconcile(context.Context, rootAction, []byte) (rootActionReconciliation, error)
}

// A future production submitter must independently admit this exact approved
// request and signed bytes under its owned route/custody policy. The owned
// submission adapter implements this port with separate signed route approval;
// no route can be inferred from observation.
type rootServiceSubmitter interface {
	submitRoot(context.Context, rootServiceSubmission) error
}

// Submission admission receives the independent approval artifact and durable
// attempt identity. A transport must fence/replay that identity idempotently;
// neither this local config hash nor a caller's packet approves a route.
type rootServiceSubmission struct {
	Packet        rootOfflineCustodyPacket `json:"approved_packet"`
	ConfigHash    string                   `json:"service_config_hash"`
	Attempt       uint8                    `json:"broadcast_attempt"`
	RawExtrinsic  string                   `json:"raw_extrinsic"`
	ExtrinsicHash string                   `json:"extrinsic_hash"`
}

// The service's capabilities are supplied independently. Nil authority, signer
// or submitter blocks before a fresh signature or broadcast-attempt reservation.
type rootServicePorts struct {
	Observer   rootServiceObserver
	Reconciler rootServiceReconciler
	Authority  rootActionAuthority
	Signer     rootActionSigner
	Submitter  rootServiceSubmitter
}

// Entry operations are serialized with context-aware ownership. No background
// worker or signer is started; Run joins each synchronous port before returning.
type rootServiceOwner struct {
	config      rootServiceConfig
	store       rootServiceStorage
	ports       rootServicePorts
	ownerCh     chan struct{}
	actionOwner *rootActionOwner
	poisoned    bool
	failure     error
	wait        func(context.Context, time.Duration) bool
}

// Construction authenticates retained state, without observing or activating.
func newRootServiceOwner(config rootServiceConfig, store rootServiceStorage, ports rootServicePorts) (*rootServiceOwner, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("root service journal is absent")
	}
	self := &rootServiceOwner{config: copyRootServiceConfig(config), store: store, ports: ports, ownerCh: make(chan struct{}, 1), wait: waitRootService}
	if _, err := self.load(); err != nil {
		return nil, err
	}
	self.actionOwner = &rootActionOwner{
		store: &rootServiceActionStore{owner: self}, authority: &rootServiceAdmission{owner: self},
		signer: &rootServiceSigner{owner: self}, chain: &rootServiceChain{owner: self},
	}
	return self, nil
}

// Internal journal access requires the service owner channel or construction.
func (self *rootServiceOwner) load() (rootServiceRecord, error) {
	if self.poisoned {
		return rootServiceRecord{}, errors.Join(errors.New("root service must be reopened after an ambiguous durability or integrity failure"), self.failure)
	}
	record, err := self.store.load()
	if err == nil {
		err = record.validate(self.config)
	}
	if err != nil && !mainnetDurableAdmissionPending(err) {
		self.poisoned, self.failure = true, err
	}
	return record, err
}

// Every local intent is synced before calling another port or publishing it.
func (self *rootServiceOwner) persist(record rootServiceRecord) error {
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(self.config); err != nil {
		return err
	}
	if err := self.store.save(record); err != nil {
		if !mainnetDurableAdmissionPending(err) {
			self.poisoned, self.failure = true, err
		}
		return err
	}
	return nil
}

// A step reports only its own completed work. Activation readiness is never
// implied by a target match, durable intent or an adapter's successful return.
type rootServiceEvent struct {
	Phase           string              `json:"phase"`
	Status          string              `json:"status"`
	Observations    uint32              `json:"observations"`
	Decision        *rootWeightDecision `json:"decision,omitempty"`
	Action          *rootActionStep     `json:"action,omitempty"`
	ActivationReady bool                `json:"activation_ready"`
	Detail          string              `json:"detail,omitempty"`
	terminalFailure bool
}

// A pending action always wins over new observations. A decision and reserved
// action are committed atomically; they are never split across journal files.
func (self *rootServiceOwner) step(ctx context.Context) (result rootServiceEvent, resultErr error) {
	result = rootServiceEvent{Status: "blocked"}
	if ctx == nil {
		return result, errors.New("root service requires a context")
	}
	select {
	case self.ownerCh <- struct{}{}:
		defer func() { <-self.ownerCh }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	// Snapshot custody failure before releasing the serialized step. Run must
	// retain this original error, not replace it with a later poisoned-owner read.
	defer func() {
		result.terminalFailure = self.poisoned
		if mainnetDurableAdmissionPending(resultErr) {
			result.Status = "storage-pending"
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	record, err := self.load()
	if err != nil {
		return result, err
	}
	result.Phase, result.Observations = record.Phase, record.Observations
	if record.Phase != "observing" {
		actionResult, err := self.actionOwner.step(ctx)
		result.Action, result.Status = &actionResult, actionResult.Status
		if self.actionOwner.poisoned {
			self.poisoned, self.failure = true, self.actionOwner.failure
		}
		if actionResult.Status == "complete" {
			result.Phase = "complete"
		}
		return result, err
	}
	if self.ports.Observer == nil || record.Observations == self.config.MaximumObservations {
		return result, errors.New("root weight observer is absent or its lifetime observation budget is exhausted")
	}
	record.Observations++
	record.ObservationPending = true
	if err := self.persist(record); err != nil {
		return result, err
	}
	result.Observations = record.Observations
	observation, err := self.ports.Observer.observeRootWeights(ctx, copyRootAction(record.Action.Action))
	if err != nil {
		result.Status = "observation-unavailable"
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	decision, err := decideRootWeights(record.Action.Action, observation, record.Observations)
	if err != nil {
		return result, err
	}
	if record.Decision != nil {
		previous, current := record.Decision.Observation.Position, observation.Position
		if current.FinalizedNumber < previous.FinalizedNumber || current.FinalizedNumber == previous.FinalizedNumber && current.FinalizedHash != previous.FinalizedHash || current.FinalizedNumber > previous.FinalizedNumber && current.FinalizedHash == previous.FinalizedHash {
			return result, errors.New("root weight decision finalized continuity changed")
		}
	}
	record.Decision, record.ObservationPending = &decision, false
	if decision.Outcome == "intent" {
		record.Phase = "active"
		record.Action.LastFinalized = observation.Position.FinalizedNumber
		record.Action.LastFinalizedHash = observation.Position.FinalizedHash
		record.Action.ContentHash = ""
		record.Action.ContentHash = rootObjectHash(record.Action)
	}
	if err := self.persist(record); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Phase, result.Status, result.Decision = record.Phase, decision.Outcome, &decision
	return result, nil
}

// The finite supervisor consumes its concrete output owner. Optional delivery
// never controls journal progress; every port, cadence and exporter is joined.
func (self *rootServiceOwner) Run(ctx context.Context, maximumSteps uint32, interval time.Duration, output *rootServiceOutput) (runErr error) {
	if output != nil {
		defer func() { runErr = errors.Join(runErr, output.close()) }()
	}
	if ctx == nil || maximumSteps == 0 || maximumSteps > 10000 || interval < time.Second || interval > time.Hour || output == nil {
		return errors.New("root service run requires context, 1..10000 steps, 1s..1h cadence and an owned diagnostic exporter")
	}
	for step := uint32(0); step < maximumSteps; step++ {
		result, err := self.step(ctx)
		if ctx.Err() != nil {
			return errors.Join(err, ctx.Err())
		}
		output.offer(result, err != nil)
		if result.terminalFailure || result.Status == "complete" || result.Status == "blocked" || step+1 == maximumSteps {
			return err
		}
		if !self.wait(ctx, interval) {
			return errors.Join(err, ctx.Err())
		}
	}
	return nil
}

// The existing action core writes through the same composite journal while the
// outer service owns serialization. It cannot replace the approved request.
type rootServiceActionStore struct{ owner *rootServiceOwner }

// Only a durable active intent or recovered signed liability can reach the
// native action owner; both preserve the original request and spent allowance.
func (self *rootServiceActionStore) load() (rootActionRecord, error) {
	record, err := self.owner.load()
	if err != nil || record.Phase == "observing" {
		return rootActionRecord{}, errors.Join(errors.New("root service has no active durable intent"), err)
	}
	return record.Action, nil
}

// Terminal action receipts and service completion are one atomic write.
func (self *rootServiceActionStore) save(action rootActionRecord) error {
	record, err := self.owner.load()
	if err != nil || record.Phase != "active" || action.Action.RequestHash != record.Action.Action.RequestHash {
		return errors.Join(errors.New("root service action store ownership differs"), err)
	}
	record.Action = action
	if action.Reconciliation != nil {
		record.Phase = "complete"
	}
	return self.owner.persist(record)
}

// A journal/observer is not live authority. Missing mutation ports are refused
// before rootActionOwner reserves signing or a same-byte broadcast attempt.
type rootServiceAdmission struct{ owner *rootServiceOwner }

// All current eligibility, global fencing and enforceable exposure remain with
// the independently provided authority; no read-only-ready flag is consumed.
func (self *rootServiceAdmission) authorize(ctx context.Context, action rootAction, observation rootActionObservation) error {
	if err := errors.Join(ctx.Err(), action.validate(), observation.matches(action, true)); err != nil {
		return err
	}
	if self.owner.ports.Authority == nil || self.owner.ports.Signer == nil || self.owner.ports.Submitter == nil || action.RequestHash != self.owner.config.Packet.Action.RequestHash {
		return errors.New("root service current authority, signer or independent submitter is absent")
	}
	record, err := self.owner.load()
	if err != nil || record.RecoveredSignature {
		return errors.Join(errors.New("root service recovered liability permits receipt reconciliation only"), err)
	}
	return self.owner.ports.Authority.authorize(ctx, action, observation)
}

// A separately supplied custody port may sign only this immutable request.
type rootServiceSigner struct{ owner *rootServiceOwner }

// The action core has already persisted its signing intent before this call.
func (self *rootServiceSigner) signOnce(ctx context.Context, action rootAction) ([]byte, error) {
	if self.owner.ports.Signer == nil || action.RequestHash != self.owner.config.Packet.Action.RequestHash {
		return nil, errors.New("root service signer is absent or names another request")
	}
	if err := errors.Join(ctx.Err(), action.validate()); err != nil {
		return nil, err
	}
	record, err := self.owner.load()
	if err != nil || record.Phase != "active" || record.Action.Phase != "signing" {
		return nil, errors.Join(errors.New("root service signer requires a durable signing intent"), err)
	}
	return self.owner.ports.Signer.signOnce(ctx, action)
}

// Recovery never creates a new request or substitutes an inferred never-issued.
func (self *rootServiceSigner) recoverSignature(ctx context.Context, request string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if self.owner.ports.Signer == nil || request != self.owner.config.Packet.Action.RequestHash {
		return nil, errors.New("root service custody recovery is absent or names another request")
	}
	return self.owner.ports.Signer.recoverSignature(ctx, request)
}

// Read-only reconciliation and independently approved submission have separate
// capability objects; no endpoint or rpc write method is chosen by this bridge.
type rootServiceChain struct{ owner *rootServiceOwner }

// Pending/terminal evidence remains readable even when every mutation port is absent.
func (self *rootServiceChain) reconcile(ctx context.Context, action rootAction, raw []byte) (rootActionReconciliation, error) {
	if err := errors.Join(ctx.Err(), action.validate()); err != nil {
		return rootActionReconciliation{}, err
	}
	if self.owner.ports.Reconciler == nil || action.RequestHash != self.owner.config.Packet.Action.RequestHash {
		return rootActionReconciliation{}, errors.New("root service canonical reconciler is absent or names another request")
	}
	return self.owner.ports.Reconciler.reconcile(ctx, action, raw)
}

// Even direct adapter use cannot substitute another signed call or broadcast a
// receipt-only recovered liability. The public runtime has no submission port.
func (self *rootServiceChain) submit(ctx context.Context, raw []byte) error {
	if self.owner.ports.Submitter == nil {
		return errRootSubmissionUnavailable
	}
	action := copyRootAction(self.owner.config.Packet.Action)
	if len(raw) == 0 {
		return errors.New("root service submission requires exact signed bytes")
	}
	if err := rootReceiptSignedAction(action, raw); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	record, err := self.owner.load()
	if err != nil || record.RecoveredSignature || record.Phase != "active" || record.Action.Phase != "pending" || record.Action.RawExtrinsic != "0x"+hex.EncodeToString(raw) {
		return errors.Join(errors.New("root service submission requires its exact durable broadcast intent"), err)
	}
	config := copyRootServiceConfig(self.owner.config)
	intent := rootServiceSubmission{Packet: config.Packet, ConfigHash: rootObjectHash(config), Attempt: record.Action.Broadcasts, RawExtrinsic: record.Action.RawExtrinsic, ExtrinsicHash: record.Action.ExtrinsicHash}
	return self.owner.ports.Submitter.submitRoot(ctx, intent)
}
