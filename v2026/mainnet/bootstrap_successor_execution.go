// Independently approved execution custody adopts the exact completed prefix,
// Safe signatures and relayer bytes. Chain authority is a separate adapter gate.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

const bootstrapSuccessorExecutionSchema = "urnetwork-mainnet-successor-execution-v1"
const bootstrapSuccessorExecutionApprovalSchema = "urnetwork-mainnet-successor-execution-approval-v1"
const bootstrapSuccessorExecutionEnvelopeSchema = "urnetwork-mainnet-successor-execution-envelope-v1"
const bootstrapSuccessorExecutionRequestSchema = "urnetwork-mainnet-successor-execution-request-v1"
const bootstrapSuccessorExecutionPrefix = "contract-successor-execution"
const bootstrapSuccessorExecutionStagePrefix = ".successor-execution-"
const maximumBootstrapSuccessorExecutionBytes = 512 * 1024

// This initial profile admits exactly three independently chosen owners, two
// EIP-712 ECDSA signatures, and no modules, guards or fallback handler. Imported
// signatures are public bytes, not proof of current membership or key custody.
type bootstrapSuccessorExecutionRequest struct {
	Schema             string            `json:"schema"`
	SafeReviewHash     string            `json:"safe_review_hash"`
	RegistryDirectory  string            `json:"registry_directory"`
	Owners             []common.Address  `json:"owners"`
	Singleton          common.Address    `json:"singleton"`
	SafeSignatures     planFileReference `json:"safe_signatures"`
	RelayerTransaction planFileReference `json:"relayer_transaction"`
}

// The complete static review and exact signed payloads enter this domain. The
// original independently pinned approver also approves the registry's physical
// identity; selecting another registry cannot happen through a resume flag.
type bootstrapSuccessorExecutionPlan struct {
	Schema           string                             `json:"schema"`
	Request          bootstrapSuccessorExecutionRequest `json:"request"`
	RequestReference planFileReference                  `json:"request_reference"`
	Review           bootstrapSuccessorSafeReview       `json:"safe_review"`
	Registry         bootstrapSuccessorRootIdentity     `json:"registry_physical_root"`
	SafeSignatures   string                             `json:"safe_signatures"`
	SignedRelayer    string                             `json:"signed_relayer"`
	TransactionHash  common.Hash                        `json:"transaction_hash"`
}

// Approval is independent of the original graph and local preparation domains.
// It cannot establish any fact about a current chain or distributed signer.
type bootstrapSuccessorExecutionApproval struct {
	Schema    string                          `json:"schema"`
	Plan      bootstrapSuccessorExecutionPlan `json:"plan"`
	Signature string                          `json:"signature_ed25519"`
}

// Strict input grammar precedes any custody mutation or large archive read.
func (self bootstrapSuccessorExecutionRequest) validate() error {
	if self.Schema != bootstrapSuccessorExecutionRequestSchema || !planSha256(self.SafeReviewHash) ||
		!bootstrapRootAbsolutePath(self.RegistryDirectory) || len(self.Owners) != 3 || self.Singleton == (common.Address{}) ||
		!bootstrapRootAbsolutePath(self.SafeSignatures.Path) || !planSha256(self.SafeSignatures.Sha256) ||
		!bootstrapRootAbsolutePath(self.RelayerTransaction.Path) || !planSha256(self.RelayerTransaction.Sha256) || self.SafeSignatures.Path == self.RelayerTransaction.Path {
		return errors.New("successor execution requires exact review, physical registry, three owners and pinned signature files")
	}
	for i, owner := range self.Owners {
		if owner == (common.Address{}) || owner == common.BytesToAddress([]byte{1}) || i > 0 && bytes.Compare(self.Owners[i-1][:], owner[:]) >= 0 {
			return errors.New("successor execution owners must be three distinct sorted nonzero identities")
		}
	}
	return nil
}

// Mathematical Safe inputs are reconstructed from the exact retained review.
func (self bootstrapSuccessorExecutionPlan) transaction() safeExecutionTransaction {
	t := self.Review.Transaction
	nonce, _ := evmWei(t.Nonce)
	return safeExecutionTransaction{ChainId: new(big.Int).SetUint64(t.ChainId), Safe: t.Safe, To: t.To, Value: new(big.Int),
		Data: common.FromHex(t.Data), Operation: 0, SafeTxGas: new(big.Int), BaseGas: new(big.Int), GasPrice: new(big.Int), Nonce: nonce}
}

// Every signed outer field is fixed; a signature replacement needs a new schema
// and independently approved migration, including both old nonce liabilities.
func (self bootstrapSuccessorExecutionPlan) outer(profile *safeExecutionProfile) (evmPhaseAction, error) {
	signatures, err := rootReceiptHex(self.SafeSignatures, 130)
	if err != nil || len(signatures) != 130 {
		return evmPhaseAction{}, errors.Join(errors.New("successor execution requires exactly two Safe signatures"), err)
	}
	inspection, err := profile.inspectSignatures(self.transaction(), signatures, 2)
	if err != nil {
		return evmPhaseAction{}, err
	}
	for _, signature := range inspection.Prefix {
		if signature.Kind != "eip712-ecdsa" || !signature.EcdsaRecoveryVerified || !slices.Contains(self.Request.Owners, signature.Signer) {
			return evmPhaseAction{}, errors.New("successor Safe signature is not an approved EIP-712 owner")
		}
	}
	data, err := profile.encodeTransaction(self.transaction(), signatures)
	if err != nil {
		return evmPhaseAction{}, err
	}
	r := self.Review.Relayer
	return evmPhaseAction{Id: "evidence-anchor", Sender: r.Sender, Nonce: r.Nonce, To: &r.To, Data: "0x" + hex.EncodeToString(data),
		ValueWei: r.ValueWei, Gas: r.Gas, FeeCapWei: r.FeeCapWei, TipCapWei: r.TipCapWei}, nil
}

// Pure validation checks approval structure and independently reconstructs all
// digest/envelope arithmetic. Public loading additionally rebuilds the review
// from actual original files and authenticates the complete published archive.
func (self bootstrapSuccessorExecutionPlan) validate(profile *safeExecutionProfile) error {
	p := self.Review.Preparation.Approval.Plan
	r := self.Review
	if err := errors.Join(self.Request.validate(), p.validate(), r.Preparation.validate(r.Preparation.Approval), r.Request.validate(), profile.checkProfile()); err != nil {
		return err
	}
	seal := r.ContentHash
	r.ContentHash = ""
	if self.Schema != bootstrapSuccessorExecutionSchema || self.Registry.Inode == 0 ||
		!bootstrapRootAbsolutePath(self.RequestReference.Path) || !planSha256(self.RequestReference.Sha256) ||
		seal != rootObjectHash(r) || seal != self.Request.SafeReviewHash || r.Schema != bootstrapSuccessorSafeReviewSchema || r.Status != "offline-review-authority-unresolved" ||
		r.Request.PreparationPlanHash != p.hash() || r.Request.PreparationRecordHash != r.Preparation.ContentHash ||
		profile.version != r.Request.Version || profile.variant != r.Request.Variant ||
		!r.PreparationApprovalVerified || !r.LocalPreparationComplete || !r.SafeDigestComputed || r.ExecutionApprovalVerified || r.SigningAuthorized || r.Executable ||
		r.CurrentChainVerified || r.SafeAuthorityVerified || r.GlobalSigningCustodyVerified || r.NetworkEffects || r.InstallationComplete || r.ActivationReady ||
		self.Request.RegistryDirectory == p.Proposal.OriginalRunDirectory ||
		strings.HasPrefix(self.Request.RegistryDirectory, p.Proposal.OriginalRunDirectory+string(filepath.Separator)) ||
		strings.HasPrefix(p.Proposal.OriginalRunDirectory, self.Request.RegistryDirectory+string(filepath.Separator)) {
		return errors.New("successor execution differs from exact offline review or separate registry scope")
	}
	t := self.Review.Transaction
	intent := p.Proposal.Request
	if t.Safe != intent.IntendedOwnerSafe || t.To != p.Proposal.Anchor.Coordinator || t.Data != p.Proposal.Anchor.CallData || t.Nonce != intent.IntendedSafeNonce ||
		t.ChainId != mainnetEvmChainId || t.ValueWei != "0" || t.Operation != 0 || t.SafeTxGas != "0" || t.BaseGas != "0" || t.GasPrice != "0" ||
		t.GasToken != (common.Address{}) || t.RefundReceiver != (common.Address{}) ||
		self.Request.Singleton == t.Safe || self.Request.Singleton == self.Review.Relayer.Sender {
		return errors.New("successor execution changed the exact zero-value Safe anchor")
	}
	digest, err := profile.transactionDigest(self.transaction())
	if err != nil || digest.Hash != t.Digest || digest.DomainSeparator != t.DomainSeparator || digest.StructHash != t.StructHash || "0x"+hex.EncodeToString(digest.Preimage) != t.Preimage {
		return errors.Join(errors.New("successor execution Safe digest differs"), err)
	}
	outer, err := self.outer(profile)
	if err != nil {
		return err
	}
	raw, err := rootReceiptHex(self.SignedRelayer, 128*1024)
	if err != nil {
		return err
	}
	tx, err := outer.signed(raw)
	if err != nil || tx.Hash() != self.TransactionHash || self.SafeSignatures != "0x"+hex.EncodeToString(common.FromHex(self.SafeSignatures)) || self.SignedRelayer != "0x"+hex.EncodeToString(raw) ||
		safeReleaseHash(common.FromHex(self.SafeSignatures)) != self.Request.SafeSignatures.Sha256 || safeReleaseHash(raw) != self.Request.RelayerTransaction.Sha256 {
		return errors.Join(errors.New("successor execution differs from exact pinned signed relayer bytes"), err)
	}
	b := p.Proposal.Budget
	completed, e1 := evmWei(b.CompletedEnvelopeReservationWei)
	unexecuted, e2 := evmWei(b.UnexecutedEnvelopeReservationWei)
	ceiling, e3 := evmWei(b.ProposedMaximumLifetimeWei)
	if err := errors.Join(e1, e2, e3); err != nil {
		return err
	}
	liability := new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), tx.GasFeeCap())
	total := new(big.Int).Add(new(big.Int).Add(completed, unexecuted), liability)
	if total.BitLen() > 256 || total.Cmp(ceiling) > 0 || r.Relayer.MaximumLiabilityWei != liability.String() || r.Relayer.CumulativeLiabilityWei != total.String() ||
		r.Relayer.CumulativeCeilingWei != ceiling.String() || r.Relayer.UnreservedHeadroomWei != new(big.Int).Sub(ceiling, total).String() ||
		r.Relayer.Sender != intent.IntendedRelayer || r.Relayer.Nonce != intent.IntendedRelayerNonce || r.Relayer.To != t.Safe || r.Relayer.ChainId != mainnetEvmChainId ||
		r.Relayer.ValueWei != "0" || r.Relayer.Gas != r.Request.RelayerGas || r.Relayer.FeeCapWei != r.Request.RelayerFeeCapWei || r.Relayer.TipCapWei != r.Request.RelayerTipCapWei ||
		r.Relayer.CalldataComplete || r.Relayer.NonceReserved || r.Relayer.BudgetReserved {
		return errors.New("successor execution lost original liability floors or separate relayer nonce")
	}
	return nil
}

// The independent domain covers compact complete JSON, including both public
// signatures. Only an authenticated static profile can expose signing bytes.
func (self bootstrapSuccessorExecutionPlan) signingBytes(profile *safeExecutionProfile) ([]byte, error) {
	if err := self.validate(profile); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > maximumBootstrapSuccessorExecutionBytes {
		return nil, errors.Join(errors.New("successor execution plan exceeds its byte bound"), err)
	}
	return append([]byte(bootstrapSuccessorExecutionApprovalSchema+"\x00"), raw...), nil
}

// Domain and plan identity are stable independently of envelope signature bytes.
func (self bootstrapSuccessorExecutionPlan) hash() string {
	return rootObjectHash(struct {
		Domain string                          `json:"domain"`
		Plan   bootstrapSuccessorExecutionPlan `json:"plan"`
	}{Domain: bootstrapSuccessorExecutionApprovalSchema, Plan: self})
}

// The expected approver is reloaded from original signed custody, never learned
// from this envelope. Neither earlier approval domain can authorize execution.
func (self bootstrapSuccessorExecutionApproval) validate(expected bootstrapSuccessorExecutionPlan, profile *safeExecutionProfile) error {
	if self.Schema != bootstrapSuccessorExecutionEnvelopeSchema || rootObjectHash(self.Plan) != rootObjectHash(expected) {
		return errors.New("successor execution approval differs from reconstructed original custody")
	}
	message, err := expected.signingBytes(profile)
	key, keyErr := rootReceiptHex(expected.Review.Preparation.Approval.Plan.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	if err != nil || keyErr != nil || signatureErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.Join(errors.New("successor execution independent approval is invalid"), err, keyErr, signatureErr)
	}
	return nil
}

// Exact pinned public bytes are read under private-file rules. No key is opened
// and no new signature is produced by this builder.
func buildBootstrapSuccessorExecution(ctx context.Context, review bootstrapSuccessorSafeReview, request bootstrapSuccessorExecutionRequest, reference planFileReference, profile *safeExecutionProfile) (bootstrapSuccessorExecutionPlan, error) {
	var plan bootstrapSuccessorExecutionPlan
	if err := request.validate(); err != nil {
		return plan, err
	}
	registry, err := bootstrapSuccessorPhysicalRoot(request.RegistryDirectory)
	if err != nil {
		return plan, err
	}
	signatures, signatureHash, err := readBootstrapRootFile(ctx, request.SafeSignatures.Path, 130)
	if err != nil || signatureHash != request.SafeSignatures.Sha256 || len(signatures) != 130 {
		return plan, errors.Join(errors.New("successor Safe signature input differs"), err)
	}
	raw, rawHash, err := readBootstrapRootFile(ctx, request.RelayerTransaction.Path, 128*1024)
	if err != nil || rawHash != request.RelayerTransaction.Sha256 {
		return plan, errors.Join(errors.New("successor relayer transaction input differs"), err)
	}
	plan = bootstrapSuccessorExecutionPlan{Schema: bootstrapSuccessorExecutionSchema, Request: request, RequestReference: reference, Review: review, Registry: registry,
		SafeSignatures: "0x" + hex.EncodeToString(signatures), SignedRelayer: "0x" + hex.EncodeToString(raw)}
	outer, err := plan.outer(profile)
	if err != nil {
		return plan, err
	}
	tx, err := outer.signed(raw)
	if err != nil {
		return plan, err
	}
	plan.TransactionHash = tx.Hash()
	return plan, errors.Join(ctx.Err(), plan.validate(profile))
}
