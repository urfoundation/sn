// Recycle submission requires a new independent approval over the original
// signed request. Each uncertain post consumes its durable immutable allowance.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"
)

const ownerRecycleSubmissionSchema = "urnetwork-mainnet-owner-recycle-submission-v1"

// Explicit acceptance describes what current reads cannot enforce at inclusion.
// A source-artifact exception approved only for planning supplies no signature.
func ownerRecycleSubmissionResiduals() []string {
	return []string{
		"This approval authorizes bounded submission of only the original signed owner Recycle transaction on its exact production runtime and route; planning-only artifact approval does not authorize this operation.",
		"Governance, privileged runtime changes, owner or proxy actions and subnet pruning or netuid reuse may change the checked generation, mode, rate limit or admin window before inclusion; the native call has no atomic generation predicate.",
		"The fee reserve is local admission, not an on-chain maximum; successful or failed dispatch can charge more than that reserve.",
		"Local custody does not fence other machines, rollback or other holders of the owner key; original nonce, signatures and pending transactions require external exclusive custody.",
		"RPC finality and state are trusted observations; independent production runtime provenance, release review and finality authority must be approved separately.",
		"A successful mode transition does not establish the native 10-percent provider and 90-percent recycle allocation or authorize validator activation.",
	}
}

// Fresh production authority binds original bytes, a finite window and a finite
// post cap. It cannot replenish a retained approval or renew the native era.
type ownerRecycleSubmissionApproval struct {
	Schema            string   `json:"schema"`
	ConfigHash        string   `json:"original_action_config_hash"`
	RequestHash       string   `json:"original_signing_request_hash"`
	ExtrinsicHash     string   `json:"original_signed_extrinsic_hash"`
	AuthorityHash     string   `json:"independent_production_authority_hash"`
	ValidFromBlock    uint64   `json:"valid_from_finalized_block"`
	ValidThroughBlock uint64   `json:"valid_through_finalized_block"`
	MaximumAttempts   uint8    `json:"maximum_cumulative_posts"`
	ResidualRisks     []string `json:"explicitly_accepted_residual_risks"`
	Signature         string   `json:"approval_signature_ed25519"`
}

// The retained key is audit data; every submit caller independently supplies it.
// A numbered reservation is consumed even when no acknowledgment is returned.
type ownerRecycleSubmissionRecord struct {
	Approval    ownerRecycleSubmissionApproval `json:"approval"`
	ApprovalKey string                         `json:"approval_public_key_ed25519"`
	Attempts    uint8                          `json:"consumed_post_reservations"`
}

// No prior action, trim or planning approval has this signature domain.
func (self ownerRecycleSubmissionApproval) signingBytes() []byte {
	self.Signature = ""
	raw, _ := json.Marshal(self)
	return append([]byte(ownerRecycleSubmissionSchema+"\x00"), raw...)
}

// Exact config covers runtime code/metadata/source, owner, original mortality,
// generation, route and fee reserve. The additional authority pin is independent.
func (self ownerRecycleSubmissionApproval) validate(record ownerRecycleRecord, key string) error {
	action := record.Config.Action
	if record.Request == nil || record.Signature == "" || self.Schema != ownerRecycleSubmissionSchema || self.ConfigHash != rootObjectHash(record.Config) ||
		self.RequestHash != record.Request.ContentHash || !rootCanonicalHash(record.ExtrinsicHash) || self.ExtrinsicHash != record.ExtrinsicHash ||
		!planSha256(self.AuthorityHash) || self.ValidFromBlock < action.BirthBlock || self.ValidFromBlock > self.ValidThroughBlock ||
		self.ValidThroughBlock >= action.BirthBlock+action.Period-1 || self.MaximumAttempts == 0 || self.MaximumAttempts > 8 ||
		!slices.Equal(self.ResidualRisks, ownerRecycleSubmissionResiduals()) || !rootCanonicalHash(key) {
		return errors.New("recycle submission approval differs from original bytes, production authority, mortal window or explicit risk bounds")
	}
	public, _ := hex.DecodeString(key[2:])
	signature, err := rootOfflineSignatureBytes(self.Signature)
	if err != nil || !ed25519.Verify(public, self.signingBytes(), signature) {
		return errors.New("recycle independent submission approval is invalid")
	}
	return nil
}

// The unsigned template cannot invent independent production authority. Its
// caller must obtain a new reviewed authority pin and actual approval signature.
func ownerRecycleSubmissionTemplate(record ownerRecycleRecord) (ownerRecycleSubmissionApproval, error) {
	if record.Phase != "signed" || record.Request == nil || record.Signature == "" || record.Submission != nil {
		return ownerRecycleSubmissionApproval{}, errors.New("recycle submission planning requires original signed custody without another submission approval")
	}
	action := record.Config.Action
	from := action.BirthBlock
	if record.Reconciliation != nil {
		from = max(from, record.Reconciliation.FinalizedNumber)
	}
	if from >= action.BirthBlock+action.Period-1 {
		return ownerRecycleSubmissionApproval{}, errors.New("recycle original era has no remaining inclusion window")
	}
	return ownerRecycleSubmissionApproval{Schema: ownerRecycleSubmissionSchema, ConfigHash: rootObjectHash(record.Config), RequestHash: record.Request.ContentHash,
		ExtrinsicHash: record.ExtrinsicHash, ValidFromBlock: from, ValidThroughBlock: action.BirthBlock + action.Period - 2,
		MaximumAttempts: 1, ResidualRisks: ownerRecycleSubmissionResiduals()}, nil
}

// Current admission rereads the original birth observation and exact finalized
// owner/runtime/window. No receipt, missing nonce or changed mode permits a send.
func (self *ownerRecycleCanonicalChain) admitSubmission(ctx context.Context, record ownerRecycleRecord, approval ownerRecycleSubmissionApproval) error {
	action := record.Config.Action
	evidence := record.Reconciliation
	if evidence == nil || evidence.Receipt != nil || evidence.HeadIssue != "" || evidence.AccountNonce == nil || *evidence.AccountNonce != action.Nonce ||
		evidence.FinalizedNumber < approval.ValidFromBlock || evidence.FinalizedNumber > approval.ValidThroughBlock {
		return errors.New("recycle submission lacks current original nonce, canonical absence or approved window")
	}
	original, err := self.recycleObservationAt(ctx, action.Policy, action.Owner, action.BirthHash, action.BirthBlock)
	if err != nil {
		return err
	}
	if original.ContentHash != action.ObservationHash {
		return errors.New("recycle authenticated birth observation differs from the approved action")
	}
	current, err := self.recycleObservationAt(ctx, action.Policy, action.Owner, evidence.FinalizedHash, evidence.FinalizedNumber)
	if err != nil {
		return err
	}
	metadata, _, err := nativePinnedMetadata(record.Request.Metadata, action.Policy.RuntimeMetadataHash)
	if err != nil {
		return err
	}
	window, err := current.window(action.Policy, metadata)
	if err != nil {
		return err
	}
	if window.Owner != action.Owner || window.RegistrationBlock != action.SubnetRegistrationBlock || window.Nonce != action.Nonce ||
		window.Mode != 0 || window.FreeRao < action.FeeReserveRao {
		return errors.New("recycle current owner, generation, nonce, Burn mode or fee balance changed")
	}
	for block := evidence.FinalizedNumber + 1; block < action.BirthBlock+action.Period; block++ {
		if !window.permits(block) {
			return errors.New("recycle current rate or admin window does not cover original remaining mortality")
		}
	}
	return ctx.Err()
}

// One call reconciles first, counts at most one post, then returns. A lost
// acknowledgment never retries transport or refreshes nonce, era or signature.
func (self *ownerRecycleCustody) submit(ctx context.Context, chain *ownerRecycleCanonicalChain, approval ownerRecycleSubmissionApproval, key string) (ownerRecycleResult, error) {
	record, err := self.load()
	if err != nil {
		return ownerRecycleResult{}, err
	}
	if ctx == nil || chain == nil || chain.client == nil || chain.client.url != self.config.Route.RpcUrl ||
		rootObjectHash(chain.config) != rootObjectHash(self.config) || chain.key != self.key {
		return ownerRecycleRetainedResult(record), errors.New("recycle submit requires the exact original canonical route and custody")
	}
	if err := approval.validate(record, key); err != nil {
		return ownerRecycleRetainedResult(record), err
	}
	if record.Submission != nil {
		if record.Submission.ApprovalKey != key || rootObjectHash(record.Submission.Approval) != rootObjectHash(approval) {
			return ownerRecycleRetainedResult(record), errors.New("recycle original submission approval cannot be replaced or replenished")
		}
	} else {
		if record.Phase != "signed" {
			return ownerRecycleRetainedResult(record), errors.New("recycle fresh submission approval requires original signed pending custody")
		}
		record.Submission = &ownerRecycleSubmissionRecord{Approval: approval, ApprovalKey: key}
		if err := self.persist(record); err != nil {
			return ownerRecycleResult{}, err
		}
	}
	operationCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	result, err := self.reconcile(operationCtx, chain)
	if err != nil || result.Phase != "signed" {
		return result, err
	}
	record, err = self.load()
	if err != nil {
		return ownerRecycleResult{}, err
	}
	if record.Submission.Attempts >= approval.MaximumAttempts {
		return ownerRecycleRetainedResult(record), errors.New("recycle original cumulative post allowance is exhausted; reconciliation remains available")
	}
	if err := chain.admitSubmission(operationCtx, record, approval); err != nil {
		return ownerRecycleRetainedResult(record), err
	}
	if err := chain.network(operationCtx); err != nil {
		return ownerRecycleRetainedResult(record), err
	}
	record.Submission.Attempts++
	if err := self.persist(record); err != nil {
		return ownerRecycleResult{}, err
	}
	var canonical, latest string
	if err := chain.client.call(operationCtx, "chain_getBlockHash", []any{record.Reconciliation.FinalizedNumber}, &canonical); err != nil {
		return ownerRecycleRetainedResult(record), err
	}
	if err := chain.client.call(operationCtx, "chain_getFinalizedHead", []any{}, &latest); err != nil {
		return ownerRecycleRetainedResult(record), err
	}
	if canonical != record.Reconciliation.FinalizedHash || latest != canonical {
		return ownerRecycleRetainedResult(record), errors.New("recycle admitted finalized head changed before the retained post")
	}
	if _, err := self.load(); err != nil {
		return ownerRecycleResult{}, err
	}
	_, err = ownedSubmissionPost(operationCtx, chain.client, self.config.Route, "author_submitExtrinsic", record.RawExtrinsic, record.ExtrinsicHash)
	if _, custodyErr := self.load(); custodyErr != nil {
		return ownerRecycleResult{}, errors.Join(err, custodyErr)
	}
	return ownerRecycleRetainedResult(record), err
}
