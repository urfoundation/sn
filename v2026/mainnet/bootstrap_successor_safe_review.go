// Offline review joins retained successor preparation to a pinned Safe digest
// and bounded relayer intention. It creates no executable approval or custody.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/stabi"
)

const bootstrapSuccessorSafeRequestSchema = "urnetwork-mainnet-successor-safe-review-request-v1"
const bootstrapSuccessorSafeReviewSchema = "urnetwork-mainnet-successor-safe-review-v1"

// Profile and envelope choices are explicit unapproved review inputs. The Safe,
// relayer, both nonces and coordinator call come only from original preparation.
type bootstrapSuccessorSafeRequest struct {
	Schema                string            `json:"schema"`
	PreparationPlanHash   string            `json:"preparation_plan_hash"`
	PreparationRecordHash string            `json:"preparation_record_hash"`
	Version               string            `json:"version"`
	Variant               string            `json:"variant"`
	Archive               planFileReference `json:"archive"`
	RelayerGas            uint64            `json:"relayer_gas"`
	RelayerFeeCapWei      string            `json:"relayer_fee_cap_wei"`
	RelayerTipCapWei      string            `json:"relayer_tip_cap_wei"`
	StartNativeNumber     uint64            `json:"start_native_number"`
	StartNativeHash       string            `json:"start_native_hash"`
	ValidThroughNative    uint64            `json:"valid_through_native"`
}

// Every SafeTx field is visible. A zero-gas, zero-reimbursement CALL requires
// inner success in these releases; no signature or nonce reservation is made.
type bootstrapSuccessorSafeTransaction struct {
	ChainId         uint64         `json:"chain_id"`
	Safe            common.Address `json:"safe"`
	To              common.Address `json:"to"`
	ValueWei        string         `json:"value_wei"`
	Data            string         `json:"data"`
	Operation       uint8          `json:"operation"`
	SafeTxGas       string         `json:"safe_tx_gas"`
	BaseGas         string         `json:"base_gas"`
	GasPrice        string         `json:"gas_price"`
	GasToken        common.Address `json:"gas_token"`
	RefundReceiver  common.Address `json:"refund_receiver"`
	Nonce           string         `json:"nonce"`
	DomainSeparator common.Hash    `json:"domain_separator"`
	StructHash      common.Hash    `json:"struct_hash"`
	Digest          common.Hash    `json:"digest"`
	Preimage        string         `json:"preimage"`
}

// The outer data cannot be complete until approved Safe signatures exist.
// This value/fee ceiling is a proposal, never a transaction or reserved nonce.
type bootstrapSuccessorRelayerReview struct {
	ChainId                uint64         `json:"chain_id"`
	Sender                 common.Address `json:"sender"`
	Nonce                  uint64         `json:"nonce"`
	To                     common.Address `json:"to"`
	ValueWei               string         `json:"value_wei"`
	Gas                    uint64         `json:"gas"`
	FeeCapWei              string         `json:"fee_cap_wei"`
	TipCapWei              string         `json:"tip_cap_wei"`
	MaximumLiabilityWei    string         `json:"maximum_liability_wei"`
	CumulativeCeilingWei   string         `json:"cumulative_ceiling_wei"`
	CumulativeLiabilityWei string         `json:"cumulative_liability_wei"`
	UnreservedHeadroomWei  string         `json:"unreserved_headroom_wei"`
	CalldataComplete       bool           `json:"calldata_complete"`
	NonceReserved          bool           `json:"nonce_reserved"`
	BudgetReserved         bool           `json:"budget_reserved"`
}

// The complete prepared record and release facts remain independently sealed.
// Only local preparation, static artifacts and pure computation become true.
type bootstrapSuccessorSafeReview struct {
	Schema                         string                              `json:"schema"`
	Status                         string                              `json:"status"`
	Request                        bootstrapSuccessorSafeRequest       `json:"request"`
	RequestReference               planFileReference                   `json:"request_reference"`
	Preparation                    bootstrapSuccessorPreparationRecord `json:"preparation"`
	Release                        safeReleaseVerification             `json:"release"`
	Transaction                    bootstrapSuccessorSafeTransaction   `json:"safe_transaction"`
	Relayer                        bootstrapSuccessorRelayerReview     `json:"relayer"`
	PreparationApprovalVerified    bool                                `json:"preparation_approval_verified"`
	LocalPreparationComplete       bool                                `json:"local_preparation_complete"`
	SafeDigestComputed             bool                                `json:"safe_digest_computed"`
	ExecutionApprovalVerified      bool                                `json:"execution_approval_verified"`
	ApprovalSigningPayloadProvided bool                                `json:"approval_signing_payload_provided"`
	CurrentChainVerified           bool                                `json:"current_chain_verified"`
	SafeAuthorityVerified          bool                                `json:"safe_authority_verified"`
	GlobalSigningCustodyVerified   bool                                `json:"global_signing_custody_verified"`
	OriginalReservationsReconciled bool                                `json:"original_reservations_reconciled"`
	SigningAuthorized              bool                                `json:"signing_authorized"`
	Executable                     bool                                `json:"executable"`
	NetworkEffects                 bool                                `json:"network_effects"`
	InstallationComplete           bool                                `json:"installation_complete"`
	ActivationReady                bool                                `json:"activation_ready"`
	RequiredPrerequisites          []string                            `json:"required_prerequisites"`
	ContentHash                    string                              `json:"content_hash"`
}

// Profile pins, canonical fee values and finite native windows are checked
// before retained custody or a compressed release archive is opened.
func (self bootstrapSuccessorSafeRequest) validate() error {
	profile, profileErr := loadSafeReleasePin(self.Version, self.Variant)
	fee, feeErr := evmWei(self.RelayerFeeCapWei)
	tip, tipErr := evmWei(self.RelayerTipCapWei)
	if err := errors.Join(profileErr, feeErr, tipErr); err != nil {
		return err
	}
	if self.Schema != bootstrapSuccessorSafeRequestSchema || !planSha256(self.PreparationPlanHash) || !planSha256(self.PreparationRecordHash) ||
		!bootstrapRootAbsolutePath(self.Archive.Path) || self.Archive.Sha256 != profile.ArchiveSha256 ||
		self.RelayerGas < 21000 || self.RelayerGas > 100_000_000 || fee.Sign() == 0 || tip.Cmp(fee) > 0 ||
		new(big.Int).Mul(new(big.Int).SetUint64(self.RelayerGas), fee).BitLen() > 256 ||
		self.StartNativeNumber == 0 || !rootCanonicalHash(self.StartNativeHash) || self.StartNativeHash == (common.Hash{}).Hex() ||
		self.ValidThroughNative <= self.StartNativeNumber || self.ValidThroughNative-self.StartNativeNumber > 7200 {
		return errors.New("successor Safe review lacks exact preparation, release or bounded relayer intention")
	}
	return nil
}

// Callers reconstruct the original graph and borrow its completed preparation
// before entering this pure builder. Supplied reports cannot replace that path.
func buildBootstrapSuccessorSafeReview(ctx context.Context, expected bootstrapSuccessorPreparationPlan, record bootstrapSuccessorPreparationRecord, request bootstrapSuccessorSafeRequest, requestReference planFileReference, rawArchive []byte) (bootstrapSuccessorSafeReview, error) {
	var result bootstrapSuccessorSafeReview
	if ctx == nil {
		return result, errors.New("successor Safe review context is absent")
	}
	if err := errors.Join(ctx.Err(), request.validate(), record.Approval.validate(expected), record.validate(record.Approval)); err != nil {
		return result, err
	}
	if request.PreparationPlanHash != expected.hash() || request.PreparationRecordHash != record.ContentHash ||
		!bootstrapRootAbsolutePath(requestReference.Path) || !planSha256(requestReference.Sha256) {
		return result, errors.New("successor Safe review differs from the exact completed preparation")
	}
	p := expected.Proposal
	anchor, intent := p.Anchor, p.Request
	if anchor.ExpectedOwner == (common.Address{}) || anchor.ExpectedOwner != intent.IntendedOwnerSafe ||
		anchor.Coordinator == (common.Address{}) || anchor.Evidence == (common.Address{}) || anchor.Coordinator == anchor.Evidence ||
		anchor.ExpectedOwner == anchor.Coordinator || anchor.ExpectedOwner == anchor.Evidence ||
		intent.IntendedRelayer == (common.Address{}) || intent.IntendedRelayer == anchor.ExpectedOwner || intent.IntendedRelayer == anchor.Coordinator || intent.IntendedRelayer == anchor.Evidence ||
		anchor.ValueWei != "0" || anchor.CallData != "0x"+hex.EncodeToString(stabi.NewSTCoordinator().PackFixValidatorEvidence(anchor.Evidence)) ||
		p.AdoptedActions[4].Receipt.ContractAddress != anchor.Coordinator.Hex() || p.AdoptedActions[7].Receipt.ContractAddress != anchor.Evidence.Hex() {
		return result, errors.New("successor Safe review differs from the retained zero-value evidence anchor")
	}
	for _, action := range p.AdoptedActions {
		if request.StartNativeNumber < action.Receipt.NativeNumber || request.StartNativeNumber == action.Receipt.NativeNumber && request.StartNativeHash != action.Receipt.NativeHash {
			return result, errors.New("successor Safe review window contradicts an adopted original receipt")
		}
	}
	nonce, err := evmWei(intent.IntendedSafeNonce)
	if err != nil {
		return result, err
	}
	fee, _ := evmWei(request.RelayerFeeCapWei)
	liability := new(big.Int).Mul(new(big.Int).SetUint64(request.RelayerGas), fee)
	completed, _ := evmWei(p.Budget.CompletedEnvelopeReservationWei)
	unexecuted, _ := evmWei(p.Budget.UnexecutedEnvelopeReservationWei)
	ceiling, _ := evmWei(p.Budget.ProposedMaximumLifetimeWei)
	cumulative := new(big.Int).Add(completed, unexecuted)
	cumulative.Add(cumulative, liability)
	if cumulative.BitLen() > 256 || cumulative.Cmp(ceiling) > 0 {
		return result, errors.New("successor relayer intention exceeds cumulative lifetime liability including original reservations")
	}
	profile, err := loadSafeReleasePin(request.Version, request.Variant)
	if err != nil {
		return result, err
	}
	release, members, err := inspectSafeReleaseArchive(ctx, profile, request.Variant, request.Archive.Path, rawArchive)
	if err != nil {
		return result, err
	}
	var rawArtifact []byte
	for _, artifact := range profile.Artifacts {
		if artifact.Name == request.Variant {
			rawArtifact = members[artifact.ArchivePath]
		}
	}
	execution, err := newSafeExecutionProfile(request.Version, request.Variant, rawArtifact)
	if err != nil {
		return result, err
	}
	transaction := safeExecutionTransaction{ChainId: big.NewInt(mainnetEvmChainId), Safe: anchor.ExpectedOwner, To: anchor.Coordinator,
		Value: new(big.Int), Data: stabi.NewSTCoordinator().PackFixValidatorEvidence(anchor.Evidence), Operation: 0,
		SafeTxGas: new(big.Int), BaseGas: new(big.Int), GasPrice: new(big.Int), Nonce: nonce}
	digest, err := execution.transactionDigest(transaction)
	if err != nil {
		return result, err
	}
	result = bootstrapSuccessorSafeReview{Schema: bootstrapSuccessorSafeReviewSchema, Status: "offline-review-authority-unresolved",
		Request: request, RequestReference: requestReference, Preparation: record, Release: release,
		Transaction: bootstrapSuccessorSafeTransaction{ChainId: mainnetEvmChainId, Safe: anchor.ExpectedOwner, To: anchor.Coordinator,
			ValueWei: "0", Data: anchor.CallData, Operation: 0, SafeTxGas: "0", BaseGas: "0", GasPrice: "0", Nonce: intent.IntendedSafeNonce,
			DomainSeparator: digest.DomainSeparator, StructHash: digest.StructHash, Digest: digest.Hash, Preimage: "0x" + hex.EncodeToString(digest.Preimage)},
		Relayer: bootstrapSuccessorRelayerReview{ChainId: mainnetEvmChainId, Sender: intent.IntendedRelayer, Nonce: intent.IntendedRelayerNonce,
			To: anchor.ExpectedOwner, ValueWei: "0", Gas: request.RelayerGas, FeeCapWei: request.RelayerFeeCapWei, TipCapWei: request.RelayerTipCapWei,
			MaximumLiabilityWei: liability.String(), CumulativeCeilingWei: ceiling.String(), CumulativeLiabilityWei: cumulative.String(),
			UnreservedHeadroomWei: new(big.Int).Sub(ceiling, cumulative).String()},
		PreparationApprovalVerified: true, LocalPreparationComplete: true, SafeDigestComputed: true,
		RequiredPrerequisites: []string{"INDEPENDENT_SAFE_COMPILER_REBUILD_AND_RELEASE_REVIEW", "CURRENT_FINALIZED_CHAIN_AND_EIGHT_ORIGINAL_RECEIPTS",
			"CURRENT_SAFE_PROXY_SINGLETON_OWNERS_THRESHOLD_MODULES_GUARDS_AND_FALLBACK", "CURRENT_SAFE_NONCE_AND_PENDING_OPERATIONS",
			"CURRENT_EVIDENCE_RUNTIME_IMMUTABLE_DOMAIN_AND_COORDINATOR_BINDING", "ORIGINAL_UNEXECUTED_ENVELOPE_AND_NONCE_RECONCILIATION",
			"INDEPENDENT_EXECUTION_SUCCESSOR_APPROVAL_AND_DURABLE_CUMULATIVE_ACCOUNTING", "GLOBAL_SAFE_AND_RELAYER_SIGNING_CUSTODY",
			"OWNER_SIGNATURES_EXACT_OUTER_CALLDATA_FEES_NONCE_AND_FUNDING", "EXECUTION_WINDOW_ENFORCEMENT_AND_SIGNATURE_LIFETIME_CUSTODY",
			"CANONICAL_SAFE_INNER_SUCCESS_AND_ONE_SHOT_EVIDENCE_BINDING"}}
	if err := ctx.Err(); err != nil {
		return bootstrapSuccessorSafeReview{}, err
	}
	result.ContentHash = rootObjectHash(result)
	return result, nil
}
