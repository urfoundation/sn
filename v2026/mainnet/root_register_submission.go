// Root registration submission requires a new independent approval over the original
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

const rootRegisterSubmissionSchema = "urnetwork-mainnet-root-register-submission-v1"

// Explicit acceptance describes what current reads cannot enforce at inclusion.
// A source-artifact exception approved only for planning supplies no signature.
func rootRegisterSubmissionResiduals() []string {
	return append(rootRegisterExposureAcknowledgements(),
		"This separate approval authorizes only the retained original signed root registration on the exact production runtime and route, within its original era and finite cumulative post allowance; planning-only artifact approval is insufficient.",
		"A successful registration event and inclusion-block seat do not identify the exact burn amount; actual burn remains unknown without an original execution witness.",
	)
}

// Fresh production authority binds original bytes, a finite window and a finite
// post cap. It cannot replenish a retained approval or renew the native era.
type rootRegisterSubmissionApproval struct {
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
type rootRegisterSubmissionRecord struct {
	Approval    rootRegisterSubmissionApproval `json:"approval"`
	ApprovalKey string                         `json:"approval_public_key_ed25519"`
	Attempts    uint8                          `json:"consumed_post_reservations"`
}

// No prior action, trim or planning approval has this signature domain.
func (self rootRegisterSubmissionApproval) signingBytes() []byte {
	self.Signature = ""
	raw, _ := json.Marshal(self)
	return append([]byte(rootRegisterSubmissionSchema+"\x00"), raw...)
}

// Exact config covers runtime code/metadata/source, owner, original mortality,
// generation, route and fee reserve. The additional authority pin is independent.
func (self rootRegisterSubmissionApproval) validate(record rootRegisterRecord, key string) error {
	action := record.Config.Action
	if record.Request == nil || record.Signature == "" || self.Schema != rootRegisterSubmissionSchema || self.ConfigHash != rootObjectHash(record.Config) ||
		self.RequestHash != record.Request.ContentHash || !rootCanonicalHash(record.ExtrinsicHash) || self.ExtrinsicHash != record.ExtrinsicHash ||
		!planSha256(self.AuthorityHash) || self.ValidFromBlock < action.BirthBlock || self.ValidFromBlock > self.ValidThroughBlock ||
		self.ValidThroughBlock >= action.BirthBlock+action.Period-1 || self.MaximumAttempts == 0 || self.MaximumAttempts > 8 ||
		!slices.Equal(self.ResidualRisks, rootRegisterSubmissionResiduals()) || !rootCanonicalHash(key) {
		return errors.New("root registration submission approval differs from original bytes, production authority, mortal window or explicit risk bounds")
	}
	public, _ := hex.DecodeString(key[2:])
	signature, err := rootOfflineSignatureBytes(self.Signature)
	if err != nil || !ed25519.Verify(public, self.signingBytes(), signature) {
		return errors.New("root registration independent submission approval is invalid")
	}
	return nil
}

// The unsigned template cannot invent independent production authority. Its
// caller must obtain a new reviewed authority pin and actual approval signature.
func rootRegisterSubmissionTemplate(record rootRegisterRecord) (rootRegisterSubmissionApproval, error) {
	if record.Phase != "signed" || record.Request == nil || record.Signature == "" || record.Submission != nil {
		return rootRegisterSubmissionApproval{}, errors.New("root registration submission planning requires original signed custody without another submission approval")
	}
	action := record.Config.Action
	from := action.BirthBlock
	if record.Reconciliation != nil {
		from = max(from, record.Reconciliation.FinalizedNumber)
	}
	if from >= action.BirthBlock+action.Period-1 {
		return rootRegisterSubmissionApproval{}, errors.New("root registration original era has no remaining inclusion window")
	}
	return rootRegisterSubmissionApproval{Schema: rootRegisterSubmissionSchema, ConfigHash: rootObjectHash(record.Config), RequestHash: record.Request.ContentHash,
		ExtrinsicHash: record.ExtrinsicHash, ValidFromBlock: from, ValidThroughBlock: action.BirthBlock + action.Period - 2,
		MaximumAttempts: 1, ResidualRisks: rootRegisterSubmissionResiduals()}, nil
}

// Current admission rereads the original birth observation and exact finalized
// operator/runtime/eligibility. No receipt, missing nonce or changed eligibility permits a send.
func (self *rootRegisterCanonicalChain) admitSubmission(ctx context.Context, record rootRegisterRecord, approval rootRegisterSubmissionApproval) error {
	action := record.Config.Action
	evidence := record.Reconciliation
	if evidence == nil || evidence.Receipt != nil || evidence.HeadIssue != "" || evidence.AccountNonce == nil || *evidence.AccountNonce != action.Nonce ||
		evidence.FinalizedNumber < approval.ValidFromBlock || evidence.FinalizedNumber > approval.ValidThroughBlock {
		return errors.New("root registration submission lacks current original nonce, canonical absence or approved window")
	}
	original, err := self.rootRegisterObservationAt(ctx, action.Policy, action.BirthHash, action.BirthBlock)
	if err != nil {
		return err
	}
	if original.ContentHash != action.ObservationHash {
		return errors.New("root registration authenticated birth observation differs from the approved action")
	}
	metadata, _, err := nativePinnedMetadata(record.Request.Metadata, action.Policy.RuntimeMetadataHash)
	if err != nil {
		return err
	}
	birth, err := original.eligibility(action.Policy, metadata)
	if err != nil || !birth.Eligible || birth.ExistingSeat != nil || birth.Nonce != action.Nonce || birth.BurnRao != action.QuotedBurnRao || birth.FreeRao != action.ObservedFreeRao || birth.ConservativeReducibleRao != action.ObservedReducibleRao {
		return errors.Join(errors.New("root registration approved birth quote or eligibility differs from its original observation"), err)
	}
	current, err := self.rootRegisterObservationAt(ctx, action.Policy, evidence.FinalizedHash, evidence.FinalizedNumber)
	if err != nil {
		return err
	}
	window, err := current.eligibility(action.Policy, metadata)
	if err != nil {
		return err
	}
	if window.ExistingSeat != nil || !window.Eligible || window.Nonce != action.Nonce {
		return errors.New("root registration current seat, owner, eligibility or original nonce changed")
	}
	if err := rootRegisterPreflightExposure(action, window); err != nil {
		return err
	}
	return ctx.Err()
}

// One call reconciles first, counts at most one post, then returns. A lost
// acknowledgment never retries transport or refreshes nonce, era or signature.
func (self *rootRegisterCustody) submit(ctx context.Context, chain *rootRegisterCanonicalChain, approval rootRegisterSubmissionApproval, key string) (rootRegisterResult, error) {
	record, err := self.load()
	if err != nil {
		return rootRegisterResult{}, err
	}
	if ctx == nil || chain == nil || chain.client == nil || chain.client.url != self.config.Route.RpcUrl ||
		rootObjectHash(chain.config) != rootObjectHash(self.config) || chain.key != self.key {
		return rootRegisterRetainedResult(record), errors.New("root registration submit requires the exact original canonical route and custody")
	}
	if err := approval.validate(record, key); err != nil {
		return rootRegisterRetainedResult(record), err
	}
	if record.Submission != nil {
		if record.Submission.ApprovalKey != key || rootObjectHash(record.Submission.Approval) != rootObjectHash(approval) {
			return rootRegisterRetainedResult(record), errors.New("root registration original submission approval cannot be replaced or replenished")
		}
	} else {
		if record.Phase != "signed" {
			return rootRegisterRetainedResult(record), errors.New("root registration fresh submission approval requires original signed pending custody")
		}
		record.Submission = &rootRegisterSubmissionRecord{Approval: approval, ApprovalKey: key}
		if err := self.persist(record); err != nil {
			return rootRegisterResult{}, err
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
		return rootRegisterResult{}, err
	}
	if record.Submission.Attempts >= approval.MaximumAttempts {
		return rootRegisterRetainedResult(record), errors.New("root registration original cumulative post allowance is exhausted; reconciliation remains available")
	}
	if err := chain.admitSubmission(operationCtx, record, approval); err != nil {
		return rootRegisterRetainedResult(record), err
	}
	if err := chain.network(operationCtx); err != nil {
		return rootRegisterRetainedResult(record), err
	}
	// A lagging tag is a bounded read wait, not a consumed send. Normal advance
	// keeps the retained quote and authority window; neither is a native cap.
	closing, err := chain.client.readNativeFinalityCovering(operationCtx,
		nativeFinalityPoint{Number: record.Reconciliation.FinalizedNumber, Hash: record.Reconciliation.FinalizedHash},
		nativeFinalityPoint{Number: record.Config.Action.BirthBlock, Hash: record.Config.Action.BirthHash})
	if err != nil {
		return rootRegisterRetainedResult(record), err
	}
	if closing.Number > approval.ValidThroughBlock {
		return rootRegisterRetainedResult(record), errors.New("root registration closing finalized head exceeds the original submission window")
	}
	if err := operationCtx.Err(); err != nil {
		return rootRegisterRetainedResult(record), err
	}
	record.Submission.Attempts++
	if err := self.persist(record); err != nil {
		return rootRegisterResult{}, err
	}
	if _, err := self.load(); err != nil {
		return rootRegisterResult{}, err
	}
	_, err = ownedSubmissionPost(operationCtx, chain.client, self.config.Route, "author_submitExtrinsic", record.RawExtrinsic, record.ExtrinsicHash)
	if _, custodyErr := self.load(); custodyErr != nil {
		return rootRegisterResult{}, errors.Join(err, custodyErr)
	}
	return rootRegisterRetainedResult(record), err
}
