// The bootstrap owner advances an executable local custody phase by calling
// the existing child owners. It holds no native key, rpc or spend authority.
package main

import (
	"context"
	"errors"
	"os"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

const bootstrapRootResultSchema = "urnetwork-mainnet-bootstrap-root-result-v1"

// Local completion and the external next step are distinct from service or
// chain activation. The packet is the actual retained public custody request.
type bootstrapRootResult struct {
	Schema               string                   `json:"schema"`
	PlanHash             string                   `json:"plan_hash"`
	Phase                string                   `json:"phase"`
	LocalCustodyComplete bool                     `json:"local_custody_complete"`
	SignatureStatus      string                   `json:"signature_status"`
	NextPhase            string                   `json:"next_phase"`
	ChainPhasesPending   bool                     `json:"chain_phases_pending"`
	ActivationReady      bool                     `json:"activation_ready"`
	Packet               rootOfflineCustodyPacket `json:"approved_packet"`
	ExtrinsicHash        string                   `json:"extrinsic_hash,omitempty"`
	ServicePhase         string                   `json:"service_phase"`
	ActionPhase          string                   `json:"action_phase"`
	Observations         uint32                   `json:"service_observations"`
	Broadcasts           uint8                    `json:"broadcast_attempts"`
}

// Operations serialize with cancellation-aware ownership and join every child
// before returning. A storage ambiguity requires reopening the whole owner.
type bootstrapRootOwner struct {
	plan     bootstrapRootPlan
	store    bootstrapRootStorage
	ownerCh  chan struct{}
	poisoned bool
	failure  error
}

// Construction validates independently loaded plan and progress without opening
// a child, reading a key, sampling a network or spending an observation budget.
func newBootstrapRootOwner(plan bootstrapRootPlan, store bootstrapRootStorage) (*bootstrapRootOwner, error) {
	if err := plan.validate(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("bootstrap root progress owner is absent")
	}
	self := &bootstrapRootOwner{plan: copyBootstrapRootPlan(plan), store: store, ownerCh: make(chan struct{}, 1)}
	if _, err := self.load(); err != nil {
		return nil, err
	}
	return self, nil
}

// The owner channel is held, or construction has not yet published the owner.
func (self *bootstrapRootOwner) load() (bootstrapRootRecord, error) {
	if self.poisoned {
		return bootstrapRootRecord{}, errors.Join(errors.New("bootstrap root must reopen after ambiguous or invalid progress"), self.failure)
	}
	record, err := self.store.load()
	if err == nil {
		err = record.validate(self.plan)
	}
	if err != nil && !mainnetDurableAdmissionPending(err) {
		self.poisoned, self.failure = true, err
	}
	return record, err
}

// Child state is already durable when progress is recorded. A failed progress
// save cannot roll back a signature or turn a child into a fresh allowance.
func (self *bootstrapRootOwner) persist(record bootstrapRootRecord) error {
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(self.plan); err != nil {
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

// A not-yet-completed child may claim its prepared empty marker. The concrete
// child opener still authenticates its mandatory snapshot head before writing.
// Missing markers never grant creation, even when parent progress is pending.
func bootstrapRootCreateChild(path string, completed bool) (bool, error) {
	_, stateErr := os.Lstat(path)
	marker, markerErr := os.Lstat(path + ".lock")
	if errors.Is(markerErr, os.ErrNotExist) {
		return false, errors.Join(durablevolume.ErrIdentity, errors.New("bootstrap child lost its preprovisioned marker"), markerErr)
	}
	if markerErr != nil || stateErr != nil && !errors.Is(stateErr, os.ErrNotExist) {
		return false, mainnetDurableUnavailable("cannot observe bootstrap child custody", errors.Join(stateErr, markerErr))
	}
	if errors.Is(stateErr, os.ErrNotExist) && marker.Size() == 0 {
		if completed {
			return false, errors.New("bootstrap root completed child disappeared; refusing a new allowance")
		}
		return true, nil
	}
	return false, nil
}

// Every retry reconciles actual child state, including effects that completed
// before a progress/output failure. Missing issuance is never a never-signed
// proof, and no service step or submission method is called in this phase.
func (self *bootstrapRootOwner) advance(ctx context.Context, receipt *rootOfflineSignature) (bootstrapRootResult, error) {
	if ctx == nil {
		return bootstrapRootResult{}, errors.New("bootstrap root context is absent")
	}
	select {
	case self.ownerCh <- struct{}{}:
		defer func() { <-self.ownerCh }()
	case <-ctx.Done():
		return bootstrapRootResult{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return bootstrapRootResult{}, err
	}
	record, err := self.load()
	if err != nil {
		return bootstrapRootResult{}, err
	}
	if self.plan.PassiveService != nil {
		if receipt != nil {
			return bootstrapRootResult{}, errors.New("passive root observation cannot import a native signature")
		}
		if record.Phase == "claimed" {
			record.Phase = "passive-service-retained"
			if err := self.persist(record); err != nil {
				return bootstrapRootResult{}, err
			}
		}
		return bootstrapRootResult{Schema: "urnetwork-mainnet-bootstrap-root-result-v2", PlanHash: self.plan.ContentHash,
			Phase: bootstrapRootPassivePhase, LocalCustodyComplete: true, SignatureStatus: "not-applicable-passive-observation",
			NextPhase: "run-approved-passive-root-service", ChainPhasesPending: true, ServicePhase: record.Phase}, ctx.Err()
	}
	createCustody, err := bootstrapRootCreateChild(self.plan.Service.CustodyTrust.StatePath, record.Phase != "claimed")
	if err != nil {
		return bootstrapRootResult{}, err
	}
	var createPacket *rootOfflineCustodyPacket
	if createCustody {
		packet := copyRootServiceConfig(self.plan.Service).Packet
		createPacket = &packet
	}
	custodyStore, err := openRootOfflineCustodyStore(self.plan.Service.CustodyTrust, createPacket, ctx)
	if err != nil {
		return bootstrapRootResult{}, err
	}
	defer custodyStore.close()
	custody, err := newRootOfflineCustody(self.plan.Service.CustodyTrust, custodyStore)
	if err != nil {
		return bootstrapRootResult{}, err
	}
	packet, err := custody.packet(ctx)
	if err != nil || packet.ContentHash != self.plan.Service.Packet.ContentHash {
		return bootstrapRootResult{}, errors.Join(errors.New("bootstrap root custody retained another approved packet"), err)
	}
	if record.Phase == "claimed" {
		record.Phase = "custody-retained"
		if err := self.persist(record); err != nil {
			return bootstrapRootResult{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return bootstrapRootResult{}, err
	}
	createService, err := bootstrapRootCreateChild(packet.Action.Scope.StatePath, record.Phase != "custody-retained")
	if err != nil {
		return bootstrapRootResult{}, err
	}
	serviceStore, err := openRootServiceStore(self.plan.Service, createService, ctx)
	if err != nil {
		return bootstrapRootResult{}, err
	}
	defer serviceStore.close()
	service, err := newRootServiceOwner(self.plan.Service, serviceStore, rootServicePorts{Signer: custody})
	if err != nil {
		return bootstrapRootResult{}, err
	}
	if record.Phase == "custody-retained" {
		record.Phase = "service-retained"
		if err := self.persist(record); err != nil {
			return bootstrapRootResult{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return bootstrapRootResult{}, err
	}
	actualService, err := service.load()
	if err != nil {
		return bootstrapRootResult{}, err
	}
	actualCustody, err := custodyStore.load()
	if err != nil || record.Phase == "signature-retained" && actualCustody.ExtrinsicHash != record.SignatureHash {
		return bootstrapRootResult{}, errors.Join(errors.New("bootstrap root custody lost its retained signature"), err)
	}
	if actualService.Action.Signature != "" && (actualCustody.Signature != "" && actualCustody.Signature != actualService.Action.Signature || receipt != nil && receipt.Signature != actualService.Action.Signature) {
		return bootstrapRootResult{}, errors.New("bootstrap root signature differs from the service's original signed intent")
	}
	if receipt != nil {
		if err := custody.importSignature(ctx, *receipt); err != nil {
			return bootstrapRootResult{}, err
		}
	}
	actualCustody, err = custodyStore.load()
	if err != nil || record.Phase == "signature-retained" && actualCustody.ExtrinsicHash != record.SignatureHash {
		return bootstrapRootResult{}, errors.Join(errors.New("bootstrap root custody lost its retained signature"), err)
	}
	if actualCustody.Phase == "signed" && record.Phase != "signature-retained" {
		record.Phase, record.SignatureHash = "signature-retained", actualCustody.ExtrinsicHash
		if err := self.persist(record); err != nil {
			return bootstrapRootResult{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return bootstrapRootResult{}, err
	}
	result := bootstrapRootResult{Schema: bootstrapRootResultSchema, PlanHash: self.plan.ContentHash, Phase: bootstrapRootPhase,
		LocalCustodyComplete: true, SignatureStatus: "awaiting-import", NextPhase: "external-native-signature", ChainPhasesPending: true,
		Packet: packet, ExtrinsicHash: record.SignatureHash, ServicePhase: actualService.Phase, ActionPhase: actualService.Action.Phase,
		Observations: actualService.Observations, Broadcasts: actualService.Action.Broadcasts}
	if record.Phase == "signature-retained" {
		result.SignatureStatus, result.NextPhase = "retained", "qualified-current-authority-and-root-service"
		if actualService.Phase == "complete" {
			result.NextPhase = "remaining-bootstrap-chain-phases"
		}
	}
	return result, nil
}
