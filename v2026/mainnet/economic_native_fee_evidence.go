// An independent policy admits the exact original runtime fee callsites. The
// producer then joins real replay observations to signed receipt/native proofs;
// approval contains no fee amounts and cannot turn a missing refund into zero.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"time"
)

const economicNativeFeeEvidenceSchema = "urnetwork-admitted-native-fee-evidence-v1"
const economicNativeFeeApprovalSchema = "urnetwork-native-fee-semantics-approval-v1"

var errEconomicNativeFeeIntegrity = errors.New("native fee original evidence conflict")

// This is an operator-selected trust root, separate from the signed artifact.
// It must be retained with the consuming owner's original policy identity.
type economicNativeFeePolicy struct {
	ApprovalPublicKey string `json:"approval_ed25519_public_key"`
	Genesis           string `json:"genesis_hash"`
	EvmChainId        uint64 `json:"evm_chain_id"`
	EngineSha256      string `json:"engine_sha256"`
	CheckpointSha256  string `json:"native_checkpoint_sha256"`
	ReviewSha256      string `json:"runtime_semantics_review_sha256"`
	ProfileSha256     string `json:"original_callsite_profile_sha256"`
}

// Signature admission binds original inputs, including the generated metadata
// and rollback-aware original callsites. Receipt gas never supplies an amount.
// V1 signatures are exactly 128 lowercase hexadecimal characters without 0x;
// public keys and native block hashes retain their separate 0x-prefixed grammar.
type economicNativeFeeApproval struct {
	Schema             string `json:"schema"`
	PolicyHash         string `json:"policy_hash"`
	ContextRequestHash string `json:"context_request_hash"`
	RuntimeCodeSha256  string `json:"runtime_code_sha256"`
	MetadataSha256     string `json:"generated_metadata_sha256"`
	ParentHash         string `json:"native_parent_hash"`
	ChildHash          string `json:"native_child_hash"`
	Semantics          string `json:"semantics"`
	Signature          string `json:"signature_ed25519"`
}

type economicNativeFeeRequest struct {
	Schema   string                      `json:"schema"`
	Policy   economicNativeFeePolicy     `json:"policy"`
	Approval planFileReference           `json:"approval"`
	Context  historicalFeeContextRequest `json:"context"`
}

// The report retains every original transaction and unknown component. The
// aggregate pointers become present only when the entire selected census is
// complete; their values are decimal native Rao, never receipt gas units.
type economicNativeFeeEvidence struct {
	Schema                    string                      `json:"schema"`
	RequestHash               string                      `json:"request_hash"`
	ApprovalHash              string                      `json:"approval_hash"`
	Context                   *historicalFeeContextReport `json:"context"`
	SelectedTransactions      uint64                      `json:"selected_transactions"`
	AuthenticatedTransactions uint64                      `json:"authenticated_transactions"`
	WithdrawalRao             *string                     `json:"withdrawal_rao"`
	RefundRao                 *string                     `json:"refund_rao"`
	DebitRao                  *string                     `json:"debit_rao"`
	SelectedCensusComplete    bool                        `json:"selected_census_complete"`
	WholeBlockCensusComplete  bool                        `json:"whole_block_census_complete"`
	SpendingAuthorized        bool                        `json:"spending_authorized"`
	ContentHash               string                      `json:"content_hash"`
}

func (self economicNativeFeeApproval) signingBytes() ([]byte, error) {
	self.Signature = ""
	raw, err := json.Marshal(self)
	if err != nil {
		return nil, err
	}
	return append([]byte(economicNativeFeeApprovalSchema+"\x00"), raw...), nil
}

func economicNativeFeeSha(value historicalReplayDigest) string {
	return "sha256:" + hex.EncodeToString(value[:])
}
func economicNativeFeeHash(value historicalReplayDigest) string {
	return "0x" + hex.EncodeToString(value[:])
}

func (self economicNativeFeeRequest) validate(approval economicNativeFeeApproval, job historicalReplayJob) error {
	policy := self.Policy
	if err := self.Context.validate(); err != nil {
		return err
	}
	if self.Schema != economicNativeFeeEvidenceSchema || !rootCanonicalHash(policy.ApprovalPublicKey) || !rootCanonicalHash(policy.Genesis) || policy.EvmChainId == 0 || self.Context.Genesis != policy.Genesis || self.Context.EvmChainId != policy.EvmChainId || !planSha256(policy.EngineSha256) || !planSha256(policy.CheckpointSha256) || !planSha256(policy.ReviewSha256) || !planSha256(policy.ProfileSha256) || self.Context.Engine.Sha256 != policy.EngineSha256 || self.Context.Checkpoint.Sha256 != policy.CheckpointSha256 {
		return errors.New("native fee policy differs from independently selected engine, checkpoint or network")
	}
	profile := job.ObservationProfile
	if profile == nil || profile.MetadataSha256 == nil {
		return errors.New("native fee admission requires exact runtime-generated metadata and original callsites")
	}
	if err := profile.validate(job); err != nil {
		return err
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	if monitorReadDigest(raw) != policy.ProfileSha256 || economicNativeFeeSha(profile.SourceReviewSha256) != policy.ReviewSha256 || approval.Schema != economicNativeFeeApprovalSchema || approval.PolicyHash != rootObjectHash(policy) || approval.ContextRequestHash != rootObjectHash(self.Context) || approval.RuntimeCodeSha256 != economicNativeFeeSha(job.RuntimeCodeSha256) || approval.MetadataSha256 != economicNativeFeeSha(*profile.MetadataSha256) || approval.ParentHash != economicNativeFeeHash(job.ParentHash) || approval.ChildHash != economicNativeFeeHash(job.ChildHash) || approval.Semantics != "original-native-withdrawal-refund-and-ethereum-context-with-rollback-v1" {
		return errors.New("native fee approval differs from original replay, layout or withdrawal/refund semantics")
	}
	purposes := map[string]bool{}
	for _, rule := range profile.Rules {
		purposes[rule.Purpose] = true
	}
	if !purposes["fee-withdraw"] || !purposes["fee-refund"] || !purposes["ethereum-executed"] {
		return errors.New("native fee profile omits a required original semantic boundary")
	}
	key, keyErr := rootReceiptHex(policy.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(approval.Signature)
	message, messageErr := approval.signingBytes()
	if keyErr != nil || signatureErr != nil || messageErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.New("native fee independent semantics signature is invalid")
	}
	return nil
}

// Verify immutable approval before dispatch; the existing owner independently
// rereads the same hash-pinned job and executes it. A read error stays a cause,
// and no external serialized report enters this producer.
func runEconomicNativeFeeEvidence(ctx context.Context, request economicNativeFeeRequest, budget time.Duration, hooks historicalReplayHooks) (result *economicNativeFeeEvidence, resultErr error) {
	if ctx == nil || budget < time.Minute || budget > 15*time.Minute || !bootstrapRootAbsolutePath(request.Approval.Path) || !planSha256(request.Approval.Sha256) {
		return nil, errors.New("native fee producer requires bounded owner and original approval")
	}
	owner, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, owner.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	read := func(reference planFileReference, limit int, value any) error {
		raw, digest, err := readBootstrapRootFile(owner, reference.Path, limit)
		if err != nil {
			return err
		}
		if digest != reference.Sha256 {
			return errors.Join(errEconomicNativeFeeIntegrity, errors.New("native fee original input differs from its pin"))
		}
		if err := decodePlanJson(raw, value); err != nil {
			return errors.Join(errEconomicNativeFeeIntegrity, err)
		}
		return nil
	}
	var approval economicNativeFeeApproval
	if err := read(request.Approval, nativeExecutionAdmissionLimit, &approval); err != nil {
		return nil, err
	}
	var job historicalReplayJob
	if err := read(request.Context.Job, historicalNativeJobLimit, &job); err != nil {
		return nil, err
	}
	if err := request.validate(approval, job); err != nil {
		return nil, errors.Join(errEconomicNativeFeeIntegrity, err)
	}
	joined, err := runHistoricalFeeContext(owner, request.Context, budget, hooks)
	if err != nil {
		return nil, err
	}
	return deriveEconomicNativeFeeEvidence(owner, request, approval, joined)
}

// Relative proof verification is promoted only through the separately checked
// checkpoint/runtime policy. Reverted EVM calls still retain their actual fees;
// rollback-discarded native observations cannot become a surviving debit.
func deriveEconomicNativeFeeEvidence(ctx context.Context, request economicNativeFeeRequest, approval economicNativeFeeApproval, joined *historicalFeeContextReport) (*economicNativeFeeEvidence, error) {
	if joined == nil || joined.RequestHash != rootObjectHash(request.Context) || joined.Replay == nil || joined.Replay.HookObservations == nil || joined.Replay.HookObservations.FeeEvents == nil || joined.ContextProof == nil || joined.ContextProof.Finality == nil || !joined.ContextProof.Finality.GrandpaCertificatesVerified || !joined.ContextProof.Finality.NativeHeaderAncestryVerified || !joined.ContextProof.Finality.NativeEvmCommitmentVerified {
		return nil, errors.New("native fee producer lacks its completed original context")
	}
	result := &economicNativeFeeEvidence{Schema: economicNativeFeeEvidenceSchema, RequestHash: rootObjectHash(request), ApprovalHash: rootObjectHash(approval), Context: joined}
	withdrawal, refund, debit := new(big.Int), new(big.Int), new(big.Int)
	for index := range joined.Transactions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item := &joined.Transactions[index]
		if item.NativeBlock == nil {
			continue
		}
		result.SelectedTransactions++
		candidate := item.Candidate
		if candidate == nil || candidate.WithdrawalRao == nil || candidate.RefundRao == nil || candidate.DebitRao == nil {
			continue
		}
		w, e1 := monitorEconomicInteger(*candidate.WithdrawalRao)
		r, e2 := monitorEconomicInteger(*candidate.RefundRao)
		d, e3 := monitorEconomicInteger(*candidate.DebitRao)
		if e1 != nil || e2 != nil || e3 != nil || r.Cmp(w) > 0 || new(big.Int).Sub(w, r).Cmp(d) != 0 || candidate.ExtrinsicIndex == nil || len(candidate.EventOrdinals) != 3 {
			return nil, errors.New("native fee surviving withdrawal/refund pair does not conserve original debit")
		}
		// Each pointer owns a detached copy; changing a candidate cannot rewrite
		// the admitted observation held by the downstream conservation owner.
		wRaw, rRaw, dRaw := w.String(), r.String(), d.String()
		item.ActualWithdrawalRao, item.ActualRefundRao, item.ActualDebitRao = &wRaw, &rRaw, &dRaw
		item.FeeAuthenticated, item.State = true, "admitted-original-native-fee"
		withdrawal.Add(withdrawal, w)
		refund.Add(refund, r)
		debit.Add(debit, d)
		if withdrawal.BitLen() > 256 || refund.BitLen() > 256 || debit.BitLen() > 256 {
			return nil, errors.New("native fee aggregate exceeds bounded arithmetic")
		}
		result.AuthenticatedTransactions++
	}
	result.SelectedCensusComplete = result.SelectedTransactions > 0 && result.SelectedTransactions == result.AuthenticatedTransactions
	// An archive selects transactions; it cannot prove the full block's account
	// or entitlement census merely because every selected receipt has a pair.
	if result.SelectedCensusComplete {
		w, r, d := withdrawal.String(), refund.String(), debit.String()
		result.WithdrawalRao, result.RefundRao, result.DebitRao = &w, &r, &d
	}
	joined.AuthorityCheckpointAuthenticated, joined.RuntimeSourceAuthenticated, joined.FinalityAuthenticated = true, true, true
	joined.NativeFeesAuthenticated = result.SelectedCensusComplete && result.SelectedTransactions == uint64(len(joined.Transactions))
	joined.Admission = "independently-admitted-original-native-fee-context"
	joined.MissingAuthorities = []string{"whole-block provider/entitlement/funding census is separate from this original selected transaction archive", "an unobserved refund remains unknown; no event is synthesized", "spending and activation authority are not granted by fee observation"}
	result.ContentHash = rootObjectHash(result)
	return result, ctx.Err()
}
