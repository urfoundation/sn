// One durable journal owns one original signatory nonce and finite send budget.
// Recovery never creates an approval, timepoint, signature or new allowance.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"time"
)

const treasuryRecordSchema = "urnetwork-native-treasury-state-v1"

// Permanent custody includes the original approval and every consumed send.
type treasuryRecord struct {
	Schema         string                  `json:"schema"`
	Config         treasuryConfig          `json:"config"`
	ApprovalKey    string                  `json:"approval_public_key_ed25519"`
	Phase          string                  `json:"phase"`
	Request        *treasurySigningRequest `json:"signing_request,omitempty"`
	Signature      string                  `json:"signature,omitempty"`
	RawExtrinsic   string                  `json:"raw_extrinsic,omitempty"`
	ExtrinsicHash  string                  `json:"extrinsic_hash,omitempty"`
	Attempts       uint8                   `json:"consumed_post_reservations"`
	Reconciliation *treasuryReconciliation `json:"reconciliation,omitempty"`
	ContentHash    string                  `json:"content_hash"`
}

// Approval and inner execution are intentionally different terminal outcomes.
func treasuryPhase(request treasurySigningRequest, e treasuryReconciliation) string {
	a := request.Config.Action
	if r := e.Receipt; r != nil {
		if r.ActualFeeRao > a.FeeReserveRao {
			return "fee-overrun"
		}
		if !r.Success {
			return "outer-dispatch-failed"
		}
		if e.Dispatch == nil {
			return "finalized-readback-pending"
		}
		switch e.Dispatch.Kind {
		case "opened", "approved":
			return "approval-recorded"
		case "cancelled":
			return "cancelled"
		case "executed":
			if !e.Dispatch.InnerSuccess {
				return "inner-dispatch-failed"
			}
		default:
			return "finalized-state-conflict"
		}
		if e.Readback == nil {
			return "finalized-readback-pending"
		}
		metadata, _, err := nativePinnedMetadata(request.Metadata, a.Policy.RuntimeMetadataHash)
		if err != nil {
			return "finalized-state-conflict"
		}
		f, err := e.Readback.facts(context.Background(), a, metadata)
		if err != nil {
			return "finalized-state-conflict"
		}
		if f.RegistrationBlock != a.SubnetRegistrationBlock || f.SubnetGeneration != a.SubnetGeneration || f.Pending != nil {
			return "finalized-state-conflict"
		}
		if a.Inner.Kind == "register_limit" {
			for _, recipient := range f.Recipients {
				if recipient.Hotkey == a.Inner.Hotkey && recipient.Coldkey == a.Descriptor.Multisig.AccountId && recipient.RegistrationBlock == r.BlockNumber {
					return "executed"
				}
			}
			return "finalized-state-conflict"
		}
		return "executed"
	}
	if e.AccountNonce != nil {
		if *e.AccountNonce > a.Nonce {
			return "nonce-conflict"
		}
		if e.FinalizedNumber >= a.BirthBlock+a.Period && *e.AccountNonce == a.Nonce {
			return "expired"
		}
	}
	return "signed"
}

// Record verification replays metadata, signature and raw inner outcome events.
func (self treasuryRecord) validate(config treasuryConfig, key string) error {
	if err := config.validate(key); err != nil {
		return err
	}
	claimed := self.ContentHash
	self.ContentHash = ""
	if self.Schema != treasuryRecordSchema || self.ApprovalKey != key || rootObjectHash(self.Config) != rootObjectHash(config) || claimed != rootObjectHash(self) || self.Attempts > config.MaximumPosts {
		return errors.New("treasury custody scope, checksum or consumed allowance changed")
	}
	if self.Phase == "reserved" {
		if self.Request != nil || self.Signature != "" || self.RawExtrinsic != "" || self.ExtrinsicHash != "" || self.Reconciliation != nil || self.Attempts != 0 {
			return errors.New("reserved treasury custody contains later progress")
		}
		return nil
	}
	if self.Request == nil || rootObjectHash(self.Request.Config) != rootObjectHash(config) {
		return errors.New("treasury lost its original exported request")
	}
	if err := self.Request.validate(ownerSigningTrust{RequestHash: self.Request.ContentHash, ApprovalKey: key, Owner: config.Action.Owner, Genesis: config.Action.Policy.GenesisHash}); err != nil {
		return err
	}
	if self.Phase == "exported" {
		if self.Signature != "" || self.RawExtrinsic != "" || self.ExtrinsicHash != "" || self.Reconciliation != nil || self.Attempts != 0 {
			return errors.New("treasury exported request contains later progress")
		}
		return nil
	}
	signature, err := rootOfflineSignatureBytes(self.Signature)
	if err != nil {
		return err
	}
	raw, err := config.Action.signed(signature)
	if err != nil || self.RawExtrinsic != "0x"+hex.EncodeToString(raw) || self.ExtrinsicHash != rootExtrinsicHash(raw) {
		return errors.Join(errors.New("treasury original signature or extrinsic changed"), err)
	}
	if self.Reconciliation == nil {
		if self.Phase != "signed" {
			return errors.New("treasury terminal phase lacks original evidence")
		}
		return nil
	}
	if err := self.Reconciliation.validate(*self.Request, raw); err != nil {
		return err
	}
	if self.Phase != treasuryPhase(*self.Request, *self.Reconciliation) {
		return errors.New("treasury phase contradicts original native outcome")
	}
	return nil
}

// One local supervisor owns the durable store; no in-memory fallback exists.
type treasuryStorage interface {
	load() (treasuryRecord, error)
	save(treasuryRecord) error
}
type treasuryCustody struct {
	config  treasuryConfig
	key     string
	store   treasuryStorage
	failure error
}

// Uncertain writes poison the current instance, preserving restart recovery.
func (self *treasuryCustody) load() (treasuryRecord, error) {
	if self.failure != nil || self.store == nil {
		return treasuryRecord{}, errors.Join(errors.New("treasury custody must reopen"), self.failure)
	}
	r, err := self.store.load()
	if err == nil {
		err = r.validate(self.config, self.key)
	}
	if err != nil && !mainnetDurableAdmissionPending(err) {
		self.failure = err
	}
	return r, err
}

// A receipt, request or allowance becomes visible only after durable publication.
func (self *treasuryCustody) persist(record treasuryRecord) error {
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	err := record.validate(self.config, self.key)
	if err == nil {
		err = self.store.save(record)
	}
	if err != nil && !mainnetDurableAdmissionPending(err) {
		self.failure = err
	}
	return err
}

// Lost output can only re-export the identical original request.
func (self *treasuryCustody) export(metadata, ledger string) (treasurySigningRequest, error) {
	r, err := self.load()
	if err != nil {
		return treasurySigningRequest{}, err
	}
	request, err := newTreasurySigningRequest(self.config, self.key, metadata, ledger)
	if err != nil {
		return treasurySigningRequest{}, err
	}
	if r.Request != nil {
		if rootObjectHash(*r.Request) != rootObjectHash(request) {
			return treasurySigningRequest{}, errors.New("treasury re-export would replace original request")
		}
		return *r.Request, nil
	}
	if r.Phase != "reserved" {
		return treasurySigningRequest{}, errors.New("treasury export is not reserved")
	}
	r.Phase, r.Request = "exported", &request
	if err := self.persist(r); err != nil {
		return treasurySigningRequest{}, err
	}
	return request, nil
}

// Only the exact owner-local reply can enter host custody; no private key path.
func (self *treasuryCustody) importReply(reply ownerSigningReply) (treasuryRecord, error) {
	r, err := self.load()
	if err != nil {
		return treasuryRecord{}, err
	}
	if r.Request == nil {
		return r, errors.New("treasury reply has no original export")
	}
	signature, err := validateTreasurySigningReply(*r.Request, reply)
	if err != nil {
		return r, err
	}
	if r.Signature != "" {
		if r.Signature != hex.EncodeToString(signature) || r.RawExtrinsic != reply.RawExtrinsic {
			return r, errors.New("treasury reply cannot replace original signature")
		}
		return r, nil
	}
	if r.Phase != "exported" {
		return r, errors.New("treasury reply is outside reserved export")
	}
	r.Signature, r.RawExtrinsic, r.ExtrinsicHash, r.Phase = hex.EncodeToString(signature), reply.RawExtrinsic, reply.ExtrinsicHash, "signed"
	if err := self.persist(r); err != nil {
		return treasuryRecord{}, err
	}
	return self.load()
}

// Reconciliation is read-only, including expired or inner-failed operations.
func (self *treasuryCustody) reconcile(ctx context.Context, chain *treasuryCanonicalChain) (treasuryRecord, error) {
	r, err := self.load()
	if err != nil {
		return r, err
	}
	if chain == nil || ctx == nil || r.Request == nil || r.Signature == "" {
		return r, errors.New("treasury recovery requires original returned signature and owned chain")
	}
	if r.Phase != "signed" && r.Phase != "finalized-readback-pending" {
		return r, ctx.Err()
	}
	if prior := r.Reconciliation; prior != nil {
		if _, err := chain.client.readNativeFinalityCovering(ctx, nativeFinalityPoint{Number: prior.FinalizedNumber, Hash: prior.FinalizedHash}); err != nil {
			return r, err
		}
	}
	raw, _ := hex.DecodeString(r.RawExtrinsic[2:])
	evidence, err := chain.reconcile(ctx, *r.Request, raw)
	if err != nil {
		return r, err
	}
	if err := evidence.validate(*r.Request, raw); err != nil {
		return r, err
	}
	if prior := r.Reconciliation; prior != nil {
		if evidence.FinalizedNumber < prior.FinalizedNumber || evidence.FinalizedNumber == prior.FinalizedNumber && evidence.FinalizedHash != prior.FinalizedHash || prior.Receipt != nil && (rootObjectHash(prior.Receipt) != rootObjectHash(evidence.Receipt) || prior.Events != evidence.Events) {
			return r, errors.New("treasury recovery changed retained finalized continuity or original receipt")
		}
	}
	r.Phase, r.Reconciliation = treasuryPhase(*r.Request, evidence), &evidence
	if err := self.persist(r); err != nil {
		return treasuryRecord{}, err
	}
	return self.load()
}

// Reconcile, recheck admission, consume one durable allowance, then post once.
// A transport error leaves that allowance consumed and all original bytes intact.
func (self *treasuryCustody) submit(ctx context.Context, chain *treasuryCanonicalChain) (treasuryRecord, error) {
	r, err := self.load()
	if err != nil {
		return r, err
	}
	if ctx == nil || chain == nil || chain.key != self.key || rootObjectHash(chain.config) != rootObjectHash(self.config) || chain.client.url != self.config.Route.RpcUrl {
		return r, errors.New("treasury submission route/config differs from original approval")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(self.config.Route.ReadRetrySeconds)*time.Second)
	defer cancel()
	r, err = self.reconcile(ctx, chain)
	if err != nil || r.Phase != "signed" {
		return r, err
	}
	if r.Attempts >= r.Config.MaximumPosts {
		return r, errors.New("treasury cumulative transmission allowance exhausted; reconcile only")
	}
	a := r.Config.Action
	e := r.Reconciliation
	if e == nil || e.FinalizedNumber+1 >= a.BirthBlock+a.Period {
		return r, errors.New("treasury original mortal era has no safe remaining submission block")
	}
	observation, err := chain.treasuryObservationAt(ctx, a, e.FinalizedHash, e.FinalizedNumber)
	if err != nil {
		return r, err
	}
	metadata, _, err := nativePinnedMetadata(r.Request.Metadata, a.Policy.RuntimeMetadataHash)
	if err != nil {
		return r, err
	}
	f, err := observation.facts(ctx, a, metadata)
	if err != nil {
		return r, err
	}
	if err := f.admits(a, false); err != nil {
		return r, err
	}
	if err := chain.network(ctx); err != nil {
		return r, err
	}
	r.Attempts++
	if err := self.persist(r); err != nil {
		return treasuryRecord{}, err
	}
	point, err := chain.client.readNativeFinalityCovering(ctx, nativeFinalityPoint{Number: e.FinalizedNumber, Hash: e.FinalizedHash})
	if err != nil {
		return r, err
	}
	if point.Number != e.FinalizedNumber || point.Hash != e.FinalizedHash {
		return r, errors.New("treasury finalized state advanced after admission; reconcile before another bounded send")
	}
	if _, err := self.load(); err != nil {
		return treasuryRecord{}, err
	}
	_, sendErr := ownedSubmissionPost(ctx, chain.client, r.Config.Route, "author_submitExtrinsic", r.RawExtrinsic, r.ExtrinsicHash)
	current, custodyErr := self.load()
	return current, errors.Join(sendErr, custodyErr)
}
