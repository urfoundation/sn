package main

// final_semantic_evidence.go is the offline, external-verifier boundary for a
// successful simulator run.  It deliberately does not read live RPC state:
// every decision is bound to an immutable checkpoint and content-addressed
// artifact, then independently reconstructed before FINAL.md can be emitted.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"path"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/ss58"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026"
)

const (
	finalSemanticEvidenceSchema                  = "urnetwork-final-semantic-evidence-v2"
	finalReleaseArchiveMinimumSpanBlocks         = uint64(3_570)
	finalReleaseArchiveMinimumSafetyMarginBlocks = uint64(7_200)
	finalHeadCandidateCount                      = 202
	finalHeadSlotCount                           = 200
	finalMinerSwarmProcessCount                  = 20
	finalReleaseEpochCount                       = uint64(5)
	finalReleaseEpochBlocks                      = uint64(300)
	finalReleaseFinalizeOffsetBlocks             = uint64(150)
	finalProductionEpochCount                    = uint64(3)
	finalProductionEpochBlocks                   = uint64(360)
	finalProductionFinalizeOffsetBlocks          = uint64(180)
	finalDepositFormula                          = "floor(usage_bytes*rate_numerator/(2^30*rate_denominator)); implied_usage=deposit/rate; raw_score=implied_usage*q"
	finalWeightFormula                           = "(1-theta)*pool/sum(pool)+theta*head/sum(head); mask; max_weight_limit; u16"
)

var finalCommonRequiredExitCriterionIDs = []string{
	"all-miner-tier-assignments",
	"deposit-conviction-receipts",
	"invalid-merkle-proof-rejected",
	"no-process-log-anomalies",
	"payout-double-claim-rejected",
	"reserve-one-way-backed",
	"theta-head-tail-realized",
	"unauthorized-upgrade-rejected",
}

// Deep measurement reconstruction is intentionally expensive at release
// scale. Re-validation still reads and hashes every artifact byte; this small
// success-only cache skips the deterministic decoding/replay pass only when
// both the complete signed semantic object and the exact content-addressed
// artifact graph have already passed in this process. A replaced file either
// fails its locator hash before lookup or produces a different key.
var finalSemanticArtifactVerificationCache = struct {
	sync.Mutex
	entries map[string]struct{}
}{entries: map[string]struct{}{}}

const finalSemanticArtifactVerificationCacheLimit = 32

var finalProductionRequiredExitCriterionIDs = []string{"dishonest-deposit-recovery"}

// FinalArtifactLocator is safe to hand to an external verifier. URI is either
// a canonical run-relative path or an immutable https/s3/minio locator. Query
// parameters and userinfo are rejected so credentials cannot enter FINAL.md.
type FinalArtifactLocator struct {
	Kind        string `json:"kind"`
	URI         string `json:"uri"`
	ContentHash string `json:"content_sha256"`
	SizeBytes   uint64 `json:"size_bytes"`
}

type FinalRational struct {
	Numerator   string `json:"numerator"`
	Denominator string `json:"denominator"`
}

type FinalNativeReceipt struct {
	ExtrinsicHash string               `json:"extrinsic_hash,omitempty"`
	Block         ChainHead            `json:"block"`
	Proof         FinalArtifactLocator `json:"proof"`
}

type FinalEVMReceipt struct {
	TransactionHash string               `json:"transaction_hash"`
	Block           ChainHead            `json:"block"`
	Status          string               `json:"status"`
	LogsHash        string               `json:"logs_hash"`
	Proof           FinalArtifactLocator `json:"proof"`
}

// Binds the EVM registration transaction to terminal native UID ownership.
type FinalPoolUIDEvidence struct {
	NoID              uint64               `json:"no_id"`
	UID               uint16               `json:"uid"`
	Hotkey            string               `json:"hotkey"`
	Coldkey           string               `json:"coldkey"`
	OperatorColdkey   string               `json:"operator_registry_coldkey"`
	Registered        bool                 `json:"registered"`
	Registration      FinalEVMReceipt      `json:"registration"`
	Snapshot          ChainHead            `json:"snapshot"`
	FinalCarryRao     string               `json:"final_carry_rao"`
	DepositHotkey     string               `json:"deposit_hotkey"`
	DepositSigner     string               `json:"deposit_signer"`
	PayoutRootSigner  string               `json:"payout_root_signer"`
	ConvictionReceipt FinalEVMReceipt      `json:"conviction_receipt"`
	EffectiveEpoch    uint64               `json:"effective_epoch"`
	VersionCount      uint64               `json:"version_count"`
	Active            bool                 `json:"active"`
	ServerKeyHistory  []FinalServerKey     `json:"server_key_history"`
	OwnershipArtifact FinalArtifactLocator `json:"ownership_artifact"`
}

type FinalServerKey struct {
	KeyID     uint8  `json:"key_id"`
	PublicKey string `json:"public_key"`
}

type FinalValidatorIdentityEvidence struct {
	ValidatorID       uint64               `json:"validator_id"`
	UID               uint16               `json:"uid"`
	Hotkey            string               `json:"hotkey"`
	Coldkey           string               `json:"coldkey"`
	Registered        bool                 `json:"registered"`
	Registration      FinalNativeReceipt   `json:"registration"`
	StakeRao          string               `json:"stake_rao"`
	ValidatorPermit   bool                 `json:"validator_permit"`
	ValidatorTrustU16 uint16               `json:"validator_trust_u16"`
	PathVPK           string               `json:"path_vpk"`
	Snapshot          ChainHead            `json:"snapshot"`
	SnapshotArtifact  FinalArtifactLocator `json:"snapshot_artifact"`
	Cycles            []FinalCRv4Cycle     `json:"crv4_cycles"`
}

type FinalHeadCandidateEvidence struct {
	FleetID       uint64        `json:"fleet_id"`
	Rank          uint16        `json:"rank"`
	UID           uint16        `json:"uid"`
	RawScore      FinalRational `json:"raw_score"`
	Selected      bool          `json:"selected"`
	AppliedWeight uint16        `json:"applied_weight"`
}

type FinalHeadFleetEvidence struct {
	FleetID         uint64               `json:"fleet_id"`
	UID             uint16               `json:"uid"`
	Hotkey          string               `json:"hotkey"`
	Coldkey         string               `json:"coldkey"`
	Generation      uint64               `json:"generation"`
	MemberCount     int                  `json:"member_count"`
	Registered      bool                 `json:"registered"`
	Registration    FinalNativeReceipt   `json:"registration"`
	Snapshot        ChainHead            `json:"snapshot"`
	BindingArtifact FinalArtifactLocator `json:"binding_artifact"`
}

type FinalHeadTournamentTransition struct {
	ChallengerFleetID      uint64               `json:"challenger_fleet_id"`
	PromotedUID            uint16               `json:"promoted_uid"`
	PromotedHotkey         string               `json:"promoted_hotkey"`
	PrunedUID              uint16               `json:"pruned_uid"`
	PrunedChurn            uint64               `json:"pruned_churn"`
	PrunedHotkey           string               `json:"pruned_hotkey"`
	OperationalRPCMode     string               `json:"operational_rpc_mode"`
	IndependentRPC         bool                 `json:"independent_rpc"`
	Registration           FinalNativeReceipt   `json:"registration"`
	Snapshot               ChainHead            `json:"snapshot"`
	IndependentSnapshot    ChainHead            `json:"independent_snapshot"`
	EVMSnapshot            ChainHead            `json:"evm_snapshot"`
	IndependentEVMSnapshot ChainHead            `json:"independent_evm_snapshot"`
	Artifact               FinalArtifactLocator `json:"artifact"`
}

type finalHeadTournamentIdentity struct {
	Role      string `json:"role"`
	PublicKey string `json:"public_key"`
	SS58      string `json:"ss58"`
}

type finalHeadTournamentTransitionArtifact struct {
	Postcondition *ActionPostcondition        `json:"postcondition"`
	Pruned        finalHeadTournamentIdentity `json:"pruned_identity"`
}

type FinalValidatorViewTransition struct {
	FaultEpoch          uint64               `json:"fault_epoch"`
	RestoredEpoch       uint64               `json:"restored_epoch"`
	AffectedValidatorID uint64               `json:"affected_validator_id"`
	ControlValidatorID  uint64               `json:"control_validator_id"`
	WithheldFleetID     uint64               `json:"withheld_fleet_id"`
	ReplacementFleetID  uint64               `json:"replacement_fleet_id"`
	Artifact            FinalArtifactLocator `json:"artifact"`
}

type finalValidatorViewTransitionArtifact struct {
	FaultEpoch          uint64 `json:"fault_epoch"`
	RestoredEpoch       uint64 `json:"restored_epoch"`
	AffectedValidatorID uint64 `json:"affected_validator_id"`
	ControlValidatorID  uint64 `json:"control_validator_id"`
	WithheldFleetID     uint64 `json:"withheld_fleet_id"`
	ReplacementFleetID  uint64 `json:"replacement_fleet_id"`
}

type FinalPoolWeightEvidence struct {
	NoID                   uint64               `json:"no_id"`
	UID                    uint16               `json:"uid"`
	SourceEpoch            uint64               `json:"source_epoch"`
	UsageBytes             uint64               `json:"usage_bytes"`
	ConvictionBeforeRao    string               `json:"conviction_before_rao"`
	RateNumeratorRaoPerGiB uint64               `json:"rate_numerator_rao_per_gib"`
	RateDenominator        uint64               `json:"rate_denominator"`
	EpochDepositCapRao     string               `json:"epoch_deposit_cap_rao"`
	RequiredDepositRao     string               `json:"required_deposit_rao"`
	ObservedDepositRao     string               `json:"observed_deposit_rao"`
	QualityPPM             uint32               `json:"quality_ppm"`
	QualityFactor          FinalRational        `json:"q"`
	ImpliedUsageGiB        FinalRational        `json:"implied_usage_gib"`
	RawScore               FinalRational        `json:"raw_score"`
	Formula                string               `json:"formula"`
	AuditStatus            string               `json:"audit_status"`
	AuditCompliant         bool                 `json:"audit_compliant"`
	AuditDisposition       string               `json:"audit_disposition"`
	AuditError             string               `json:"audit_error,omitempty"`
	ArtifactContentHash    string               `json:"artifact_content_hash"`
	ArtifactHash           string               `json:"artifact_hash"`
	PayoutRoot             string               `json:"payout_root"`
	ArtifactSigner         string               `json:"artifact_signer"`
	RootCommitter          string               `json:"root_committer"`
	RootSigner             string               `json:"root_signer"`
	SourceStartBlock       uint64               `json:"source_start_block"`
	SourceStartHash        string               `json:"source_start_hash"`
	SourceEndBlock         uint64               `json:"source_end_block"`
	SourceEndHash          string               `json:"source_end_hash"`
	RootCommitBlock        uint64               `json:"root_commit_block"`
	ObservedAtBlock        uint64               `json:"observed_at_block"`
	ArtifactDeadlineBlock  uint64               `json:"artifact_deadline_block"`
	PayoutArtifact         FinalArtifactLocator `json:"payout_artifact"`
	DepositReceipt         FinalEVMReceipt      `json:"deposit_receipt"`
	AppliedWeight          uint16               `json:"applied_weight"`
}

type FinalSubmittedWeight struct {
	UID   uint16        `json:"uid"`
	Score FinalRational `json:"score"`
	Value uint16        `json:"value"`
}

type FinalCRv4Cycle struct {
	SettlementEpoch     uint64                       `json:"settlement_epoch"`
	SubnetEpoch         uint64                       `json:"subnet_epoch"`
	NativeSnapshot      ChainHead                    `json:"native_snapshot"`
	EVMSnapshot         ChainHead                    `json:"evm_snapshot"`
	Theta               FinalRational                `json:"theta"`
	QualityMinimumPPM   uint32                       `json:"quality_minimum_ppm"`
	QualityMaximumPPM   uint32                       `json:"quality_maximum_ppm"`
	MaximumHeadFleets   uint16                       `json:"maximum_head_fleets"`
	MaxWeightLimitU16   uint16                       `json:"max_weight_limit_u16"`
	Formula             string                       `json:"formula"`
	MaskedUIDs          []uint16                     `json:"masked_uids"`
	Candidates          []FinalHeadCandidateEvidence `json:"candidates"`
	Pools               []FinalPoolWeightEvidence    `json:"pools"`
	Submitted           []FinalSubmittedWeight       `json:"submitted"`
	RealizedHeadValue   uint64                       `json:"realized_head_u16_sum"`
	RealizedPoolValue   uint64                       `json:"realized_pool_u16_sum"`
	RealizedTotalValue  uint64                       `json:"realized_total_u16_sum"`
	ValuesHash          string                       `json:"values_hash"`
	IntentVectorHash    string                       `json:"intent_vector_hash"`
	IntentArtifact      FinalArtifactLocator         `json:"intent_artifact"`
	MeasurementArtifact FinalArtifactLocator         `json:"measurement_artifact"`
	MeasurementEnvelope FinalArtifactLocator         `json:"measurement_envelope"`
	Commit              FinalNativeReceipt           `json:"commit_receipt"`
	Reveal              FinalNativeReceipt           `json:"reveal_receipt"`
	Application         FinalNativeReceipt           `json:"application_receipt"`
}

// FinalDishonestDepositDecision carries the complete signed CRv4 decision and
// native application receipt for one validator. Public replay must observe the
// exact submitted vector at Application: the penalized pool is absent/zero,
// then present with positive weight after an exact recovery deposit.
type FinalDishonestDepositDecision struct {
	ValidatorID       uint64         `json:"validator_id"`
	ValidatorUID      uint16         `json:"validator_uid"`
	PoolUID           uint16         `json:"pool_uid"`
	PoolPresent       bool           `json:"pool_present"`
	PoolAppliedWeight uint16         `json:"pool_applied_weight_u16"`
	Cycle             FinalCRv4Cycle `json:"cycle"`
}

type FinalDishonestDepositEvidence struct {
	NoID                       uint64                          `json:"no_id"`
	PoolUID                    uint16                          `json:"pool_uid"`
	RequiredDepositRao         string                          `json:"required_deposit_rao"`
	ObservedDepositRao         string                          `json:"observed_deposit_rao"`
	RecoveryRequiredDepositRao string                          `json:"recovery_required_deposit_rao"`
	RecoveryObservedDepositRao string                          `json:"recovery_observed_deposit_rao"`
	UnderpaymentReceipt        FinalEVMReceipt                 `json:"underpayment_receipt"`
	RecoveryDepositReceipt     FinalEVMReceipt                 `json:"recovery_deposit_receipt"`
	Penalties                  []FinalDishonestDepositDecision `json:"penalty_decisions"`
	Recoveries                 []FinalDishonestDepositDecision `json:"recovery_decisions"`
}

type FinalClaimEvidence struct {
	LeafIndex   uint64          `json:"leaf_index"`
	Payee       string          `json:"payee_coldkey"`
	ShareBPS    uint64          `json:"share_bps"`
	ClaimedRao  string          `json:"claimed_rao"`
	PaidRao     string          `json:"paid_rao"`
	DeferredRao string          `json:"deferred_rao"`
	Receipt     FinalEVMReceipt `json:"receipt"`
}

type FinalEpochOperatorEvidence struct {
	Epoch             uint64                `json:"epoch"`
	NoID              uint64                `json:"no_id"`
	Capture           FinalEVMReceipt       `json:"capture_receipt"`
	RootDisposition   string                `json:"root_disposition"`
	Root              *FinalEVMReceipt      `json:"root_receipt,omitempty"`
	Finalize          FinalEVMReceipt       `json:"finalize_receipt"`
	PayoutRoot        string                `json:"payout_root,omitempty"`
	ArtifactHash      string                `json:"artifact_hash,omitempty"`
	PayoutArtifact    *FinalArtifactLocator `json:"payout_artifact,omitempty"`
	CapturedRao       string                `json:"captured_rao"`
	CarryInRao        string                `json:"carry_in_rao"`
	FundedRao         string                `json:"funded_rao"`
	TotalRao          string                `json:"total_rao"`
	ClaimedRao        string                `json:"claimed_rao"`
	PaidRao           string                `json:"paid_rao"`
	DeferredCreditRao string                `json:"deferred_credit_rao"`
	OutstandingRao    string                `json:"outstanding_rao"`
	CarryOutRao       string                `json:"carry_out_rao"`
	Status            uint8                 `json:"status"`
	Claims            []FinalClaimEvidence  `json:"claims"`
}

type FinalPoolConservation struct {
	CapturedRao       string `json:"captured_rao"`
	CarryInRao        string `json:"carry_in_rao"`
	FundedRao         string `json:"funded_rao"`
	ClaimedRao        string `json:"claimed_rao"`
	PaidRao           string `json:"paid_rao"`
	DeferredCreditRao string `json:"deferred_credit_rao"`
	OutstandingRao    string `json:"outstanding_rao"`
	CarryOutRao       string `json:"carry_out_rao"`
}

type FinalNativeRewardDelta struct {
	Epoch                 uint64               `json:"epoch"`
	Role                  string               `json:"role"`
	SubjectID             uint64               `json:"subject_id"`
	UID                   uint16               `json:"uid"`
	Hotkey                string               `json:"hotkey_public_key"`
	Before                ChainHead            `json:"before"`
	After                 ChainHead            `json:"after"`
	BeforeRao             string               `json:"before_rao"`
	AfterRao              string               `json:"after_rao"`
	DeltaRao              string               `json:"delta_rao"`
	StakeBeforeRao        string               `json:"stake_before_rao"`
	StakeAfterRao         string               `json:"stake_after_rao"`
	StakeDeltaRao         string               `json:"stake_delta_rao"`
	OwnerColdkey          string               `json:"owner_coldkey"`
	OwnerStakeBeforeRao   string               `json:"owner_stake_before_rao"`
	OwnerStakeAfterRao    string               `json:"owner_stake_after_rao"`
	OwnerStakeDeltaRao    string               `json:"owner_stake_delta_rao"`
	OwnerStakeBeforeEVM   ChainHead            `json:"owner_stake_before_evm_head"`
	OwnerStakeAfterEVM    ChainHead            `json:"owner_stake_after_evm_head"`
	ReserveColdkey        string               `json:"reserve_coldkey,omitempty"`
	ReserveStakeBeforeRao string               `json:"reserve_stake_before_rao,omitempty"`
	ReserveStakeAfterRao  string               `json:"reserve_stake_after_rao,omitempty"`
	ReserveStakeDeltaRao  string               `json:"reserve_stake_delta_rao,omitempty"`
	BeforeIncentiveU16    uint16               `json:"before_incentive_u16"`
	AfterIncentiveU16     uint16               `json:"after_incentive_u16"`
	BeforeDividendsU16    uint16               `json:"before_dividends_u16"`
	AfterDividendsU16     uint16               `json:"after_dividends_u16"`
	Expected              string               `json:"expected"`
	SnapshotArtifact      FinalArtifactLocator `json:"snapshot_artifact"`
}

type FinalValidatorPathProofEvidence struct {
	ValidatorID uint64               `json:"validator_id"`
	NoID        uint64               `json:"no_id"`
	FirstEpoch  uint64               `json:"first_epoch"`
	LastEpoch   uint64               `json:"last_epoch"`
	ProofCount  uint64               `json:"proof_count"`
	TrailDepth  int                  `json:"trail_depth"`
	ProofsHash  string               `json:"proofs_hash"`
	Artifact    FinalArtifactLocator `json:"artifact"`
}

type FinalMinerProcessEvidence struct {
	MinerID           uint64 `json:"miner_id"`
	ProcessID         string `json:"process_id"`
	ProcessGeneration uint64 `json:"process_generation"`
	ClientID          string `json:"client_id"`
	ProviderID        string `json:"provider_id"`
	SDKSourceHash     string `json:"sdk_source_hash"`
	Running           bool   `json:"running_through_terminal"`
}

type FinalFleetMemberBindingEvidence struct {
	MinerID       uint64 `json:"miner_id"`
	FleetID       uint64 `json:"fleet_id"`
	NoID          uint64 `json:"no_id"`
	HeadUID       uint16 `json:"head_uid"`
	ClientID      string `json:"client_id"`
	ProviderID    string `json:"provider_id"`
	Tier          string `json:"tier"`
	Generation    uint64 `json:"generation"`
	BindingActive bool   `json:"binding_active_through_terminal"`
}

// FinalProcessRestartEvidence distinguishes release-locked, successfully
// restored restart faults from unplanned process churn. Expected and observed
// counts must match exactly; a blanket "zero restarts" gate would reject the
// mandatory rolling-restart campaign and hide the distinction reviewers need.
type FinalProcessRestartEvidence struct {
	ProcessID        string   `json:"process_id"`
	ExpectedRestarts uint64   `json:"expected_restarts"`
	ObservedRestarts uint64   `json:"observed_restarts"`
	FaultIDs         []string `json:"fault_ids,omitempty"`
}

type FinalTopologyEvidence struct {
	MinerSDKInstances   int                           `json:"miner_sdk_instances"`
	MinerSwarmProcesses int                           `json:"miner_swarm_processes"`
	HeadCandidateFleets int                           `json:"head_candidate_fleets"`
	HeadSlots           int                           `json:"head_slots"`
	ValidatorProcesses  int                           `json:"validator_processes"`
	OperatorPools       int                           `json:"operator_pools"`
	MinerManifestHash   string                        `json:"miner_manifest_hash"`
	MinerManifest       FinalArtifactLocator          `json:"miner_manifest"`
	BindingManifestHash string                        `json:"binding_manifest_hash"`
	BindingManifest     FinalArtifactLocator          `json:"binding_manifest"`
	ProcessRestarts     []FinalProcessRestartEvidence `json:"process_restarts"`
}

type FinalOperatorContractCleanupEvidence struct {
	NoID           uint64               `json:"no_id"`
	TaskworkerID   string               `json:"taskworker_id"`
	Passes         int                  `json:"passes"`
	Closed         int64                `json:"closed"`
	Converged      bool                 `json:"converged"`
	ResultArtifact FinalArtifactLocator `json:"result_artifact"`
	LogArtifact    FinalArtifactLocator `json:"log_artifact"`
}

type FinalContractCleanupEvidence struct {
	Schema                   string                                 `json:"schema"`
	Cutoff                   string                                 `json:"cutoff"`
	CutoffUnixNano           int64                                  `json:"cutoff_unix_nano"`
	SupervisorManifestHash   string                                 `json:"supervisor_manifest_hash"`
	SupervisorStartTimeTicks uint64                                 `json:"supervisor_start_time_ticks"`
	SuccessfulInvocations    int                                    `json:"successful_invocations"`
	FailedInvocations        int                                    `json:"failed_invocations"`
	SupervisorStateArtifact  FinalArtifactLocator                   `json:"supervisor_state_artifact"`
	Operators                []FinalOperatorContractCleanupEvidence `json:"operators"`
}

type FinalContractDeploymentEvidence struct {
	CoordinatorProxy                  string               `json:"coordinator_proxy"`
	CoordinatorImplementation         string               `json:"coordinator_implementation"`
	SettlementVault                   string               `json:"settlement_vault"`
	ReserveSink                       string               `json:"reserve_sink"`
	GovernanceOwner                   string               `json:"governance_owner"`
	CoordinatorNetuid                 uint16               `json:"coordinator_netuid"`
	CoordinatorSelfColdkey            string               `json:"coordinator_self_coldkey"`
	CoordinatorSettlementVault        string               `json:"coordinator_settlement_vault"`
	CoordinatorReserveSink            string               `json:"coordinator_reserve_sink"`
	CoordinatorGuardian               string               `json:"coordinator_guardian"`
	CoordinatorActiveGuardian         string               `json:"coordinator_active_guardian"`
	CoordinatorPaused                 bool                 `json:"coordinator_paused"`
	CoordinatorCommitmentOracle       string               `json:"coordinator_commitment_oracle"`
	CoordinatorActiveCommitmentOracle string               `json:"coordinator_active_commitment_oracle"`
	VaultCoordinator                  string               `json:"vault_coordinator"`
	VaultNetuid                       uint16               `json:"vault_netuid"`
	VaultSelfColdkey                  string               `json:"vault_self_coldkey"`
	VaultEscrowHotkey                 string               `json:"vault_escrow_hotkey"`
	VaultEscrowRegistered             bool                 `json:"vault_escrow_registered"`
	VaultMinimumClaimTTLBlocks        uint64               `json:"vault_minimum_claim_ttl_blocks"`
	VaultMinimumTransferTaoRao        uint64               `json:"vault_minimum_transfer_tao_rao"`
	PlanDefaultMinTransferTaoRao      uint64               `json:"plan_default_min_transfer_tao_rao"`
	ReserveRecorder                   string               `json:"reserve_recorder"`
	ReserveNetuid                     uint16               `json:"reserve_netuid"`
	ReserveSelfColdkey                string               `json:"reserve_self_coldkey"`
	ReserveHotkey                     string               `json:"reserve_hotkey"`
	CoordinatorProxyCodeHash          string               `json:"coordinator_proxy_code_hash"`
	ImplementationCodeHash            string               `json:"implementation_code_hash"`
	SettlementVaultCodeHash           string               `json:"settlement_vault_code_hash"`
	ReserveSinkCodeHash               string               `json:"reserve_sink_code_hash"`
	ERC1967ImplementationSlot         string               `json:"erc1967_implementation_slot"`
	ObservedImplementationSlot        string               `json:"observed_implementation_slot"`
	PolicyVersion                     uint64               `json:"policy_version"`
	PolicyEffectiveEpoch              uint64               `json:"policy_effective_epoch"`
	PolicyEffectiveBlock              uint64               `json:"policy_effective_block"`
	Snapshot                          ChainHead            `json:"snapshot"`
	Artifact                          FinalArtifactLocator `json:"artifact"`
}

// FinalSettlementVaultState is one immutable global accounting snapshot. The
// two conservation identities are checked from these exact values instead of
// trusting the contract's convenience boolean.
type FinalSettlementVaultState struct {
	TotalCapturedRao        string    `json:"total_captured_rao"`
	TotalPaidRao            string    `json:"total_paid_rao"`
	EscrowAccountedRao      string    `json:"escrow_accounted_rao"`
	PendingFundingRao       string    `json:"pending_funding_rao"`
	OutstandingLiabilityRao string    `json:"outstanding_liability_rao"`
	LiveEscrowStakeRao      string    `json:"live_escrow_stake_rao"`
	Block                   ChainHead `json:"block"`
}

type FinalSettlementVaultAccounting struct {
	Before                       FinalSettlementVaultState `json:"before"`
	After                        FinalSettlementVaultState `json:"after"`
	TotalCapturedDeltaRao        string                    `json:"total_captured_delta_rao"`
	TotalPaidDeltaRao            string                    `json:"total_paid_delta_rao"`
	EscrowAccountedDeltaRao      string                    `json:"escrow_accounted_delta_rao"`
	PendingFundingDeltaRao       string                    `json:"pending_funding_delta_rao"`
	OutstandingLiabilityDeltaRao string                    `json:"outstanding_liability_delta_rao"`
	LiveEscrowStakeDeltaRao      string                    `json:"live_escrow_stake_delta_rao"`
	EmissionCapturedEventRao     string                    `json:"emission_captured_event_rao"`
	ClaimPaidEventRao            string                    `json:"claim_paid_event_rao"`
}

type FinalReservePrincipalAddedEvidence struct {
	Epoch                uint64          `json:"epoch"`
	NoID                 uint64          `json:"no_id"`
	AmountRao            string          `json:"amount_rao"`
	OperatorPrincipalRao string          `json:"operator_principal_rao"`
	TotalPrincipalRao    string          `json:"total_principal_rao"`
	LiveStakeRao         string          `json:"live_stake_rao"`
	Receipt              FinalEVMReceipt `json:"receipt"`
}

type FinalReserveEvidence struct {
	PrincipalBeforeRao string                               `json:"principal_before_rao"`
	PrincipalAfterRao  string                               `json:"principal_after_rao"`
	PrincipalDeltaRao  string                               `json:"principal_delta_rao"`
	PrincipalAddedRao  string                               `json:"principal_added_event_rao"`
	LiveStakeBeforeRao string                               `json:"live_stake_before_rao"`
	LiveStakeAfterRao  string                               `json:"live_stake_after_rao"`
	Before             ChainHead                            `json:"before"`
	After              ChainHead                            `json:"after"`
	PrincipalAdditions []FinalReservePrincipalAddedEvidence `json:"principal_additions"`
	Artifact           FinalArtifactLocator                 `json:"artifact"`
}

// FinalArchiveRetentionEvidence binds the immutable launch-time proof that the
// public Substrate and EVM readers can retain every historical checkpoint for
// the complete two-phase campaign plus the peer-review window.
type FinalArchiveRetentionEvidence struct {
	GeneratedAt         string               `json:"generated_at"`
	DeploymentID        string               `json:"deployment_id"`
	PublicManifestHash  string               `json:"public_manifest_hash"`
	PlannedSpanBlocks   uint64               `json:"planned_span_blocks"`
	SafetyMarginBlocks  uint64               `json:"safety_margin_blocks"`
	RequiredDepthBlocks uint64               `json:"required_depth_blocks"`
	EvidenceHash        string               `json:"evidence_hash"`
	Artifact            FinalArtifactLocator `json:"artifact"`
}

type FinalExitCriterionEvidence struct {
	ID                  string                 `json:"id"`
	Expected            string                 `json:"expected"`
	Observed            string                 `json:"observed"`
	Passed              bool                   `json:"passed"`
	Checkpoint          ChainHead              `json:"checkpoint"`
	Assertions          []FinalMetricAssertion `json:"assertions"`
	EVMReceipts         []FinalEVMReceipt      `json:"evm_receipts,omitempty"`
	Artifacts           []FinalArtifactLocator `json:"artifacts"`
	PublicRequestHashes []string               `json:"public_request_hashes"`
}

type FinalMetricAssertion struct {
	Metric   string `json:"metric"`
	Expected uint64 `json:"expected"`
	Observed uint64 `json:"observed"`
}

type FinalSemanticEvidence struct {
	Schema               string                            `json:"schema"`
	Phase                string                            `json:"phase"`
	RunID                string                            `json:"run_id"`
	ResultHash           string                            `json:"result_hash"`
	CampaignStartedAt    string                            `json:"campaign_started_at"`
	CampaignCompletedAt  string                            `json:"campaign_completed_at"`
	DeploymentID         string                            `json:"deployment_id"`
	PlanHash             string                            `json:"plan_hash"`
	ConfigHash           string                            `json:"config_hash"`
	PolicyHash           string                            `json:"policy_hash"`
	GenesisHash          string                            `json:"native_genesis_hash"`
	ChainID              uint64                            `json:"evm_chain_id"`
	Netuid               uint16                            `json:"netuid"`
	PlanArtifact         FinalArtifactLocator              `json:"plan_artifact"`
	PolicyArtifact       FinalArtifactLocator              `json:"policy_artifact"`
	PriorPhase           *FinalPriorPhaseBinding           `json:"prior_phase,omitempty"`
	Window               ScenarioAcceptanceWindow          `json:"acceptance_window"`
	EVMCampaignStartHead ChainHead                         `json:"evm_campaign_start_head"`
	NativeStartHead      ChainHead                         `json:"native_start_head"`
	NativeTerminalHead   ChainHead                         `json:"native_terminal_head"`
	EVMTerminalHead      ChainHead                         `json:"evm_terminal_head"`
	ExpectedOperators    int                               `json:"expected_operators"`
	ExpectedValidators   int                               `json:"expected_validators"`
	ExpectedMiners       int                               `json:"expected_miners"`
	ExpectedCandidates   int                               `json:"expected_candidates"`
	ExpectedHeadSlots    int                               `json:"expected_head_slots"`
	Topology             FinalTopologyEvidence             `json:"topology"`
	FleetLifecycle       *FinalFleetLifecycleEvidence      `json:"fleet_lifecycle,omitempty"`
	HeadFleets           []FinalHeadFleetEvidence          `json:"head_fleet_identities"`
	HeadTransitions      []FinalHeadTournamentTransition   `json:"head_tournament_transitions"`
	ValidatorView        FinalValidatorViewTransition      `json:"validator_local_view_transition"`
	ContractCleanup      FinalContractCleanupEvidence      `json:"pre_start_contract_cleanup"`
	ArchiveRetention     FinalArchiveRetentionEvidence     `json:"archive_retention_preflight"`
	Deployment           FinalContractDeploymentEvidence   `json:"contract_deployment"`
	SettlementAccounting FinalSettlementVaultAccounting    `json:"settlement_vault_accounting"`
	Reserve              FinalReserveEvidence              `json:"reserve"`
	Pools                []FinalPoolUIDEvidence            `json:"pool_uid_ownership"`
	Validators           []FinalValidatorIdentityEvidence  `json:"validators"`
	DishonestDeposit     *FinalDishonestDepositEvidence    `json:"dishonest_deposit,omitempty"`
	Epochs               []FinalEpochOperatorEvidence      `json:"pool_epochs"`
	Conservation         FinalPoolConservation             `json:"pool_conservation"`
	NativeRewards        []FinalNativeRewardDelta          `json:"native_reward_deltas"`
	PathProofs           []FinalValidatorPathProofEvidence `json:"validator_path_proofs"`
	ExitCriteria         []FinalExitCriterionEvidence      `json:"exit_criteria"`
	PublicVerification   *FinalPublicChainVerification     `json:"public_chain_verification,omitempty"`
	EvidenceHash         string                            `json:"evidence_hash"`
}

// FinalPriorPhaseBinding makes the terminal production report a continuation
// of the independently signed accelerated release phase, rather than a second
// report that silently replaces it.
type FinalPriorPhaseBinding struct {
	RunID                          string                   `json:"run_id"`
	ResultHash                     string                   `json:"result_hash"`
	SemanticEvidenceHash           string                   `json:"semantic_evidence_hash"`
	PublicTranscriptHash           string                   `json:"public_transcript_hash"`
	OwnerCompletionEnvelopeHash    string                   `json:"owner_completion_envelope_hash"`
	EvidenceManifestEnvelopeHash   string                   `json:"evidence_manifest_envelope_hash"`
	SemanticSupplementEnvelopeHash string                   `json:"semantic_supplement_envelope_hash"`
	Completion                     FinalArtifactLocator     `json:"owner_completion"`
	EvidenceManifest               FinalArtifactLocator     `json:"evidence_manifest"`
	SemanticSupplement             FinalArtifactLocator     `json:"semantic_verified_supplement"`
	SemanticEvidenceEnvelope       FinalArtifactLocator     `json:"semantic_evidence_envelope"`
	SemanticEvidence               FinalArtifactLocator     `json:"semantic_evidence"`
	AcceptanceWindow               ScenarioAcceptanceWindow `json:"acceptance_window"`
	TerminalNativeHead             ChainHead                `json:"terminal_native_head"`
	TerminalEVMHead                ChainHead                `json:"terminal_evm_head"`
}

// FinalArtifactLoader retrieves bytes without prescribing storage. Callers can
// enforce their own immutable-object/read-back policy before verification.
type FinalArtifactLoader func(context.Context, FinalArtifactLocator) ([]byte, error)

// BuildFinalSemanticEvidence canonicalizes a complete typed input and binds it
// with EvidenceHash. It is deterministic and rejects missing semantic fields.
func BuildFinalSemanticEvidence(source FinalSemanticEvidence) (*FinalSemanticEvidence, error) {
	b, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	var evidence FinalSemanticEvidence
	if err := json.Unmarshal(b, &evidence); err != nil {
		return nil, err
	}
	if evidence.Schema == "" {
		evidence.Schema = finalSemanticEvidenceSchema
	}
	evidence.EvidenceHash = ""
	canonicalizeFinalSemanticEvidence(&evidence)
	if err := verifyFinalSemanticEvidence(&evidence, false); err != nil {
		return nil, err
	}
	hash, err := finalSemanticEvidenceHash(&evidence)
	if err != nil {
		return nil, err
	}
	evidence.EvidenceHash = hash
	if err := VerifyFinalSemanticEvidence(&evidence); err != nil {
		return nil, err
	}
	return &evidence, nil
}

// VerifyFinalSemanticEvidence reconstructs all arithmetic and ordering using
// only the published object. It performs no network access.
func VerifyFinalSemanticEvidence(evidence *FinalSemanticEvidence) error {
	return verifyFinalSemanticEvidence(evidence, true)
}

func verifyFinalSemanticEvidence(evidence *FinalSemanticEvidence, requireHash bool) error {
	if evidence == nil {
		return errors.New("final semantic evidence is nil")
	}
	if evidence.Schema != finalSemanticEvidenceSchema {
		return fmt.Errorf("unsupported final semantic evidence schema %q", evidence.Schema)
	}
	if evidence.DeploymentID == "" || strings.ContainsAny(evidence.DeploymentID, "\r\n\x00") {
		return errors.New("deployment identity is missing or unsafe")
	}
	if evidence.RunID == "" || strings.ContainsAny(evidence.RunID, "/\\\r\n\x00") {
		return errors.New("current semantic run identity is missing or unsafe")
	}
	if err := requireFinalHex32("current semantic result hash", evidence.ResultHash); err != nil {
		return err
	}
	startedAt, err := time.Parse(time.RFC3339Nano, evidence.CampaignStartedAt)
	if err != nil || evidence.CampaignStartedAt != startedAt.UTC().Format(time.RFC3339Nano) {
		return errors.New("final semantic campaign start time is not canonical UTC")
	}
	completedAt, err := time.Parse(time.RFC3339Nano, evidence.CampaignCompletedAt)
	if err != nil || evidence.CampaignCompletedAt != completedAt.UTC().Format(time.RFC3339Nano) || completedAt.Before(startedAt) {
		return errors.New("final semantic campaign completion time is not canonical UTC")
	}
	if err := verifyFinalPhaseLineage(evidence); err != nil {
		return err
	}
	for label, value := range map[string]string{
		"plan hash": evidence.PlanHash, "config hash": evidence.ConfigHash,
		"policy hash": evidence.PolicyHash, "native genesis hash": evidence.GenesisHash,
	} {
		if err := requireFinalHex32(label, value); err != nil {
			return err
		}
	}
	if evidence.ChainID == 0 || evidence.Netuid == 0 {
		return errors.New("chain identity is incomplete")
	}
	if err := verifyFinalArtifact("approved setup plan", evidence.PlanArtifact, "setup-plan"); err != nil {
		return err
	}
	if evidence.ExpectedOperators != 2 || evidence.ExpectedValidators != 2 || evidence.ExpectedMiners != 1000 {
		return fmt.Errorf("release topology is miners/operators/validators=%d/%d/%d, want 1000/2/2", evidence.ExpectedMiners, evidence.ExpectedOperators, evidence.ExpectedValidators)
	}
	if evidence.ExpectedCandidates != finalHeadCandidateCount || evidence.ExpectedHeadSlots != finalHeadSlotCount {
		return fmt.Errorf("head topology is %d/%d, want %d/%d candidates/slots", evidence.ExpectedCandidates, evidence.ExpectedHeadSlots, finalHeadCandidateCount, finalHeadSlotCount)
	}
	if err := verifyFinalWindow(evidence); err != nil {
		return err
	}
	if err := verifyFinalArchiveRetentionEvidence(evidence); err != nil {
		return err
	}
	if err := verifyFinalArtifact("canonical policy", evidence.PolicyArtifact, "policy"); err != nil {
		return err
	}
	if err := verifyFinalTopology(evidence); err != nil {
		return err
	}
	if err := verifyFinalFleetLifecycle(evidence); err != nil {
		return err
	}
	if err := verifyFinalContractCleanup(evidence); err != nil {
		return err
	}
	if err := verifyFinalDeployment(evidence); err != nil {
		return err
	}
	if err := verifyFinalSettlementAccounting(evidence); err != nil {
		return err
	}
	if err := verifyFinalReserve(evidence); err != nil {
		return err
	}
	poolByNO, err := verifyFinalPools(evidence)
	if err != nil {
		return err
	}
	validatorByID, err := verifyFinalValidators(evidence, poolByNO)
	if err != nil {
		return err
	}
	if err := verifyFinalDishonestDeposit(evidence, poolByNO, validatorByID); err != nil {
		return err
	}
	if err := verifyFinalPoolEpochs(evidence, poolByNO); err != nil {
		return err
	}
	if err := verifyFinalRewards(evidence, poolByNO, validatorByID); err != nil {
		return err
	}
	if err := verifyFinalPathProofs(evidence, poolByNO, validatorByID); err != nil {
		return err
	}
	if evidence.PublicVerification != nil {
		if err := verifyFinalPublicChainVerification(evidence.PublicVerification, evidence.ChainID, evidence.GenesisHash); err != nil {
			return err
		}
	}
	if err := verifyFinalExitCriteria(evidence); err != nil {
		return err
	}
	if requireHash {
		if err := requireFinalHex32("final semantic evidence hash", evidence.EvidenceHash); err != nil {
			return err
		}
		want, err := finalSemanticEvidenceHash(evidence)
		if err != nil {
			return err
		}
		if evidence.EvidenceHash != want {
			return fmt.Errorf("final semantic evidence hash %s, reconstructed %s", evidence.EvidenceHash, want)
		}
	}
	return nil
}

func verifyFinalArchiveRetentionEvidence(evidence *FinalSemanticEvidence) error {
	value := evidence.ArchiveRetention
	generated, err := time.Parse(time.RFC3339Nano, value.GeneratedAt)
	if err != nil || value.GeneratedAt != generated.UTC().Format(time.RFC3339Nano) {
		return errors.New("archive-retention semantic timestamp is not canonical UTC")
	}
	started, err := time.Parse(time.RFC3339Nano, evidence.CampaignStartedAt)
	if err != nil || generated.After(started) || started.Sub(generated) > finalArchiveReviewerSafetyWindow {
		return errors.New("archive-retention preflight is stale or follows campaign start")
	}
	if value.DeploymentID != evidence.DeploymentID {
		return errors.New("archive-retention preflight deployment differs from semantic evidence")
	}
	if err := requireFinalHex32("archive-retention public manifest hash", value.PublicManifestHash); err != nil {
		return err
	}
	if err := requireFinalHex32("archive-retention evidence hash", value.EvidenceHash); err != nil {
		return err
	}
	depth, ok := checkedAdd(value.PlannedSpanBlocks, value.SafetyMarginBlocks)
	if !ok || value.PlannedSpanBlocks < finalReleaseArchiveMinimumSpanBlocks || value.SafetyMarginBlocks < finalReleaseArchiveMinimumSafetyMarginBlocks || value.RequiredDepthBlocks < depth {
		return errors.New("archive-retention preflight does not cover the full campaign and peer-review margin")
	}
	if err := verifyFinalArtifact("archive-retention preflight", value.Artifact, "archive-retention-preflight"); err != nil {
		return err
	}
	return nil
}

func verifyFinalPhaseLineage(evidence *FinalSemanticEvidence) error {
	switch evidence.Phase {
	case "release-1.0":
		if evidence.PriorPhase != nil {
			return errors.New("accelerated release semantic evidence must not claim a prior phase")
		}
	case "production-soak":
		prior := evidence.PriorPhase
		if prior == nil || prior.RunID == "" || strings.ContainsAny(prior.RunID, "/\\\r\n\x00") || prior.RunID == evidence.RunID || prior.RunID == evidence.DeploymentID {
			return errors.New("production semantic evidence lacks an authenticated release-1.0 predecessor")
		}
		if err := requireFinalHex32("prior release result hash", prior.ResultHash); err != nil {
			return err
		}
		if err := requireFinalHex32("prior release semantic evidence hash", prior.SemanticEvidenceHash); err != nil {
			return err
		}
		if err := requireFinalHex32("prior release public transcript hash", prior.PublicTranscriptHash); err != nil {
			return err
		}
		if err := requireFinalSHA256("prior release owner completion envelope hash", prior.OwnerCompletionEnvelopeHash); err != nil {
			return err
		}
		if err := requireFinalSHA256("prior release evidence manifest envelope hash", prior.EvidenceManifestEnvelopeHash); err != nil {
			return err
		}
		if err := requireFinalSHA256("prior release semantic supplement envelope hash", prior.SemanticSupplementEnvelopeHash); err != nil {
			return err
		}
		if err := verifyFinalArtifact("prior release owner completion", prior.Completion, "scenario-complete"); err != nil {
			return err
		}
		if err := verifyFinalArtifact("prior release evidence manifest", prior.EvidenceManifest, "campaign-evidence-manifest"); err != nil {
			return err
		}
		if err := verifyFinalArtifact("prior release semantic supplement", prior.SemanticSupplement, "prior-semantic-supplement-envelope"); err != nil {
			return err
		}
		if err := verifyFinalArtifact("prior release semantic evidence envelope", prior.SemanticEvidenceEnvelope, "prior-semantic-file-envelope"); err != nil {
			return err
		}
		if err := verifyFinalArtifact("prior release semantic evidence", prior.SemanticEvidence, "prior-semantic-evidence"); err != nil {
			return err
		}
		if prior.AcceptanceWindow.Schema != "urnetwork-sim-acceptance-window-v1" || prior.AcceptanceWindow.EpochCount != finalReleaseEpochCount || prior.AcceptanceWindow.EpochBlocks != finalReleaseEpochBlocks || prior.AcceptanceWindow.FinalizeOffsetBlocks != finalReleaseFinalizeOffsetBlocks || prior.AcceptanceWindow.TerminalBlock == 0 || prior.AcceptanceWindow.TerminalBlock >= evidence.Window.BaselineHead.Number {
			return errors.New("prior release acceptance window does not precede production")
		}
		if err := verifyFinalHead("prior release terminal native", prior.TerminalNativeHead); err != nil {
			return err
		}
		if err := verifyFinalHead("prior release terminal EVM", prior.TerminalEVMHead); err != nil {
			return err
		}
		if prior.TerminalEVMHead.Number < prior.AcceptanceWindow.TerminalBlock || prior.TerminalEVMHead.Number >= evidence.Window.BaselineHead.Number || prior.TerminalNativeHead.Number > evidence.NativeStartHead.Number {
			return errors.New("prior release terminal heads do not precede production")
		}
	default:
		return fmt.Errorf("unsupported final semantic phase %q", evidence.Phase)
	}
	return nil
}

func verifyFinalContractCleanup(evidence *FinalSemanticEvidence) error {
	cleanup := evidence.ContractCleanup
	if cleanup.Schema != "urnetwork-sim-final-contract-cleanup-v1" || cleanup.CutoffUnixNano <= 0 || cleanup.SupervisorStartTimeTicks == 0 || cleanup.SuccessfulInvocations != evidence.ExpectedOperators || cleanup.FailedInvocations != 0 || len(cleanup.Operators) != evidence.ExpectedOperators {
		return errors.New("accepted-generation pre-start contract cleanup is incomplete")
	}
	if err := requireFinalHex32("contract cleanup supervisor manifest hash", cleanup.SupervisorManifestHash); err != nil {
		return err
	}
	if err := verifyFinalArtifact("contract cleanup supervisor generation", cleanup.SupervisorStateArtifact, "supervisor-cleanup-generation"); err != nil {
		return err
	}
	cutoff, err := time.Parse(time.RFC3339Nano, cleanup.Cutoff)
	if err != nil || cutoff.Location() != time.UTC || cutoff.UnixNano() != cleanup.CutoffUnixNano || cutoff.Format(time.RFC3339Nano) != cleanup.Cutoff {
		return errors.New("contract cleanup cutoff is not one exact canonical UTC instant")
	}
	seenTaskworkers := map[string]bool{}
	wantSuffix := "-contract-cleanup-" + strconv.FormatInt(cleanup.CutoffUnixNano, 10)
	for i, operator := range cleanup.Operators {
		if operator.NoID != uint64(i+1) || operator.TaskworkerID == "" || strings.ContainsAny(operator.TaskworkerID, "/\\\r\n\x00") || seenTaskworkers[operator.TaskworkerID] || operator.Passes < 1 || operator.Passes > serverContractCleanupMaxPasses || operator.Closed < 0 || !operator.Converged {
			return fmt.Errorf("operator %d pre-start contract cleanup is incomplete or non-canonical", i+1)
		}
		seenTaskworkers[operator.TaskworkerID] = true
		if err := verifyFinalArtifact("operator contract cleanup result", operator.ResultArtifact, "server-contract-cleanup-result"); err != nil {
			return err
		}
		if err := verifyFinalArtifact("operator contract cleanup log", operator.LogArtifact, "server-contract-cleanup-log"); err != nil {
			return err
		}
		if path.Base(operator.ResultArtifact.URI) != operator.TaskworkerID+wantSuffix+".json" || path.Base(operator.LogArtifact.URI) != operator.TaskworkerID+wantSuffix+".log" {
			return fmt.Errorf("operator %d contract cleanup artifacts are not bound to cutoff %d", operator.NoID, cleanup.CutoffUnixNano)
		}
	}
	return nil
}

func verifyFinalWindow(evidence *FinalSemanticEvidence) error {
	w := evidence.Window
	if w.Schema != "urnetwork-sim-acceptance-window-v1" || w.EpochCount == 0 || w.EpochBlocks == 0 {
		return errors.New("acceptance window is incomplete")
	}
	if w.FirstEpoch != w.BaselineEpoch+1 || w.StartBlock == 0 || w.EndBlock != w.StartBlock+w.EpochCount*w.EpochBlocks || w.TerminalBlock != w.EndBlock+w.FinalizeOffsetBlocks {
		return errors.New("acceptance window boundaries are inconsistent")
	}
	wantCount, wantBlocks, wantFinalize := finalReleaseEpochCount, finalReleaseEpochBlocks, finalReleaseFinalizeOffsetBlocks
	if evidence.Phase == "production-soak" {
		wantCount, wantBlocks, wantFinalize = finalProductionEpochCount, finalProductionEpochBlocks, finalProductionFinalizeOffsetBlocks
	}
	if w.EpochCount != wantCount || w.EpochBlocks != wantBlocks || w.FinalizeOffsetBlocks != wantFinalize {
		return fmt.Errorf("%s acceptance cadence is %dx%d+%d blocks, want %dx%d+%d", evidence.Phase, w.EpochCount, w.EpochBlocks, w.FinalizeOffsetBlocks, wantCount, wantBlocks, wantFinalize)
	}
	if w.BaselineHead.Number >= w.StartBlock || w.PolicyEffectiveEpoch > w.FirstEpoch || w.PolicyEffectiveBlock > w.StartBlock {
		return errors.New("acceptance window baseline or policy is not effective before the window")
	}
	if err := verifyFinalHead("acceptance baseline", w.BaselineHead); err != nil {
		return err
	}
	if err := requireFinalHex32("baseline observation hash", w.BaselineObservationHash); err != nil {
		return err
	}
	if err := verifyFinalHead("native start", evidence.NativeStartHead); err != nil {
		return err
	}
	if err := verifyFinalHead("native terminal", evidence.NativeTerminalHead); err != nil {
		return err
	}
	if evidence.NativeStartHead.Number >= evidence.NativeTerminalHead.Number {
		return errors.New("native evidence window is empty")
	}
	if err := verifyFinalHead("EVM campaign start", evidence.EVMCampaignStartHead); err != nil {
		return err
	}
	if evidence.EVMCampaignStartHead.Number > w.BaselineHead.Number {
		return errors.New("EVM campaign start follows the acceptance baseline")
	}
	if err := verifyFinalHead("EVM terminal", evidence.EVMTerminalHead); err != nil {
		return err
	}
	if evidence.EVMTerminalHead.Number < w.TerminalBlock {
		return errors.New("EVM terminal checkpoint predates the fixed acceptance terminal block")
	}
	if evidence.FleetLifecycle != nil {
		var evmDeadline uint64
		switch evidence.Phase {
		case "release-1.0":
			evmDeadline = evidence.FleetLifecycle.State.ReleaseEVMEvidenceDeadlineBlock
		case "production-soak":
			evmDeadline = evidence.FleetLifecycle.State.ProductionEVMEvidenceDeadlineBlock
		}
		if evmDeadline < w.TerminalBlock {
			return errors.New("signed lifecycle EVM evidence-tail bound predates the fixed acceptance terminal")
		}
	} else if evidence.EVMTerminalHead.Number != w.TerminalBlock {
		return errors.New("EVM terminal checkpoint exceeds acceptance without signed lifecycle tail evidence")
	}
	return nil
}

func verifyFinalTopology(evidence *FinalSemanticEvidence) error {
	topology := evidence.Topology
	if topology.MinerSDKInstances != 1000 || topology.MinerSwarmProcesses != finalMinerSwarmProcessCount || topology.HeadCandidateFleets != finalHeadCandidateCount || topology.HeadSlots != finalHeadSlotCount || topology.ValidatorProcesses != 2 || topology.OperatorPools != 2 {
		return fmt.Errorf("topology manifest declares miners/swarms/candidates/slots/validators/operators=%d/%d/%d/%d/%d/%d, want 1000/20/202/200/2/2", topology.MinerSDKInstances, topology.MinerSwarmProcesses, topology.HeadCandidateFleets, topology.HeadSlots, topology.ValidatorProcesses, topology.OperatorPools)
	}
	if topology.MinerSDKInstances != evidence.ExpectedMiners || topology.HeadCandidateFleets != evidence.ExpectedCandidates || topology.HeadSlots != evidence.ExpectedHeadSlots || topology.ValidatorProcesses != evidence.ExpectedValidators || topology.OperatorPools != evidence.ExpectedOperators {
		return errors.New("topology manifest counts do not match declared release topology")
	}
	if err := requireFinalSHA256("miner manifest hash", topology.MinerManifestHash); err != nil {
		return err
	}
	if err := verifyFinalArtifact("miner process manifest", topology.MinerManifest, "miner-process-manifest"); err != nil {
		return err
	}
	if topology.MinerManifestHash != topology.MinerManifest.ContentHash {
		return errors.New("miner process manifest locator hash mismatch")
	}
	if _, err := finalExpectedRestartCounts(evidence); err != nil {
		return err
	}
	if err := requireFinalSHA256("fleet binding manifest hash", topology.BindingManifestHash); err != nil {
		return err
	}
	if err := verifyFinalArtifact("fleet binding manifest", topology.BindingManifest, "fleet-binding-manifest"); err != nil {
		return err
	}
	if topology.BindingManifestHash != topology.BindingManifest.ContentHash {
		return errors.New("fleet binding manifest locator hash mismatch")
	}
	if len(evidence.HeadFleets) != finalHeadCandidateCount {
		return fmt.Errorf("head fleet identities=%d, want %d", len(evidence.HeadFleets), finalHeadCandidateCount)
	}
	seenUIDs := map[uint16]bool{}
	for i, fleet := range evidence.HeadFleets {
		if fleet.FleetID != uint64(i+1) || seenUIDs[fleet.UID] || fleet.Hotkey == "" || fleet.Coldkey == "" || fleet.Generation == 0 || fleet.MemberCount != 4 || !fleet.Registered || fleet.Snapshot != evidence.NativeTerminalHead {
			return fmt.Errorf("head fleet identity %d is incomplete, duplicated, or non-canonical", i+1)
		}
		seenUIDs[fleet.UID] = true
		if err := verifyFinalNativeReceipt("head fleet registration", fleet.Registration, 0, evidence.NativeTerminalHead.Number, true); err != nil {
			return err
		}
		if err := verifyFinalArtifact("head fleet binding", fleet.BindingArtifact, "head-fleet-binding"); err != nil {
			return err
		}
	}
	if len(evidence.HeadTransitions) != 2 {
		return fmt.Errorf("head tournament transitions=%d, want 2", len(evidence.HeadTransitions))
	}
	seenPrunedChurn := map[uint64]bool{}
	for i, transition := range evidence.HeadTransitions {
		wantFleet := uint64(finalHeadSlotCount + i + 1)
		fleet := evidence.HeadFleets[wantFleet-1]
		if transition.ChallengerFleetID != wantFleet || transition.PromotedUID != fleet.UID || transition.PromotedHotkey != fleet.Hotkey || transition.Registration != fleet.Registration || transition.PrunedUID != transition.PromotedUID || transition.PrunedChurn == 0 || seenPrunedChurn[transition.PrunedChurn] || transition.PrunedHotkey == "" || transition.PrunedHotkey == transition.PromotedHotkey || transition.Snapshot.Number < transition.Registration.Block.Number || transition.Snapshot.Number > evidence.NativeTerminalHead.Number || transition.IndependentSnapshot.Number < transition.Snapshot.Number || transition.IndependentSnapshot.Number > evidence.NativeTerminalHead.Number || transition.EVMSnapshot.Number > evidence.EVMTerminalHead.Number || transition.IndependentEVMSnapshot.Number < transition.EVMSnapshot.Number || transition.IndependentEVMSnapshot.Number > evidence.EVMTerminalHead.Number {
			return fmt.Errorf("head tournament transition %d is incomplete", i+1)
		}
		seenPrunedChurn[transition.PrunedChurn] = true
		if i > 0 {
			previous := evidence.HeadTransitions[i-1]
			if transition.Registration.Block.Number < previous.Registration.Block.Number || transition.Snapshot.Number < previous.Snapshot.Number || transition.IndependentSnapshot.Number < previous.IndependentSnapshot.Number || transition.EVMSnapshot.Number < previous.EVMSnapshot.Number || transition.IndependentEVMSnapshot.Number < previous.IndependentEVMSnapshot.Number {
				return fmt.Errorf("head tournament transition %d precedes transition %d", i+1, i)
			}
		}
		if (transition.OperationalRPCMode != rpcModePrivateAuthority && transition.OperationalRPCMode != rpcModePublicOverride) || transition.IndependentRPC != (transition.OperationalRPCMode == rpcModePrivateAuthority) {
			return fmt.Errorf("head tournament transition %d has an invalid RPC mode", i+1)
		}
		if transition.OperationalRPCMode == rpcModePublicOverride && (transition.Snapshot != transition.IndependentSnapshot || transition.EVMSnapshot != transition.IndependentEVMSnapshot) {
			return fmt.Errorf("head tournament transition %d public-override checkpoints differ", i+1)
		}
		for _, checkpoint := range []struct {
			label string
			head  ChainHead
		}{
			{"transition snapshot", transition.Snapshot},
			{"transition independent snapshot", transition.IndependentSnapshot},
			{"transition EVM snapshot", transition.EVMSnapshot},
			{"transition independent EVM snapshot", transition.IndependentEVMSnapshot},
		} {
			if err := verifyFinalHead(checkpoint.label, checkpoint.head); err != nil {
				return err
			}
		}
		if err := verifyFinalNativeReceipt("challenger registration", transition.Registration, 0, evidence.NativeTerminalHead.Number, true); err != nil {
			return err
		}
		if err := verifyFinalArtifact("head tournament transition", transition.Artifact, "head-tournament-transition"); err != nil {
			return err
		}
	}
	view := evidence.ValidatorView
	if view.FaultEpoch < evidence.Window.FirstEpoch || view.RestoredEpoch <= view.FaultEpoch || view.RestoredEpoch >= evidence.Window.FirstEpoch+evidence.Window.EpochCount || view.AffectedValidatorID == 0 || view.ControlValidatorID == 0 || view.AffectedValidatorID == view.ControlValidatorID || view.WithheldFleetID == 0 || view.WithheldFleetID > finalHeadCandidateCount || view.ReplacementFleetID == 0 || view.ReplacementFleetID > finalHeadCandidateCount || view.WithheldFleetID == view.ReplacementFleetID {
		return errors.New("validator-local view transition is incomplete")
	}
	if err := verifyFinalArtifact("validator-local view transition", view.Artifact, "validator-view-transition"); err != nil {
		return err
	}
	return nil
}

func deriveFinalValidatorViewTransition(evidence *FinalSemanticEvidence) (finalValidatorViewTransitionArtifact, error) {
	if evidence == nil || len(evidence.Validators) != 2 {
		return finalValidatorViewTransitionArtifact{}, errors.New("validator-local view requires exactly two validators")
	}
	affected, control := &evidence.Validators[0], &evidence.Validators[1]
	if affected.ValidatorID >= control.ValidatorID || len(affected.Cycles) == 0 || len(affected.Cycles) != len(control.Cycles) {
		return finalValidatorViewTransitionArtifact{}, errors.New("validator-local view cycle sets are incomplete or non-canonical")
	}
	controlByEpoch := make(map[uint64]FinalCRv4Cycle, len(control.Cycles))
	for _, cycle := range control.Cycles {
		if _, duplicate := controlByEpoch[cycle.SettlementEpoch]; duplicate {
			return finalValidatorViewTransitionArtifact{}, errors.New("control validator repeats a settlement epoch")
		}
		controlByEpoch[cycle.SettlementEpoch] = cycle
	}
	derived := finalValidatorViewTransitionArtifact{AffectedValidatorID: affected.ValidatorID, ControlValidatorID: control.ValidatorID}
	for index, cycle := range affected.Cycles {
		if index > 0 && cycle.SettlementEpoch <= affected.Cycles[index-1].SettlementEpoch {
			return finalValidatorViewTransitionArtifact{}, errors.New("affected validator cycles are not canonical")
		}
		other, ok := controlByEpoch[cycle.SettlementEpoch]
		if !ok {
			return finalValidatorViewTransitionArtifact{}, fmt.Errorf("control validator lacks settlement epoch %d", cycle.SettlementEpoch)
		}
		missing := finalSemanticSetDifference(finalSemanticSelectedFleets(other), finalSemanticSelectedFleets(cycle))
		extra := finalSemanticSetDifference(finalSemanticSelectedFleets(cycle), finalSemanticSelectedFleets(other))
		equal := len(missing) == 0 && len(extra) == 0
		if derived.FaultEpoch == 0 {
			if equal {
				continue
			}
			if len(missing) != 1 || len(extra) != 1 {
				return finalValidatorViewTransitionArtifact{}, fmt.Errorf("validator-local divergence at epoch %d is not one exact fleet substitution", cycle.SettlementEpoch)
			}
			derived.FaultEpoch, derived.WithheldFleetID, derived.ReplacementFleetID = cycle.SettlementEpoch, missing[0], extra[0]
			continue
		}
		if derived.RestoredEpoch == 0 {
			if equal {
				derived.RestoredEpoch = cycle.SettlementEpoch
				continue
			}
			if len(missing) != 1 || len(extra) != 1 || missing[0] != derived.WithheldFleetID || extra[0] != derived.ReplacementFleetID {
				return finalValidatorViewTransitionArtifact{}, fmt.Errorf("validator-local divergence changes before restoration at epoch %d", cycle.SettlementEpoch)
			}
			continue
		}
		if !equal {
			return finalValidatorViewTransitionArtifact{}, fmt.Errorf("validator-local view diverges again after restoration at epoch %d", cycle.SettlementEpoch)
		}
	}
	if derived.FaultEpoch == 0 || derived.RestoredEpoch == 0 {
		return finalValidatorViewTransitionArtifact{}, errors.New("accepted signed cycles do not prove divergence followed by restoration")
	}
	return derived, nil
}

func finalExpectedRestartCounts(evidence *FinalSemanticEvidence) (map[string]uint64, error) {
	if evidence == nil || len(evidence.Topology.ProcessRestarts) == 0 {
		return nil, errors.New("topology has no exact process restart census")
	}
	counts := make(map[string]uint64, len(evidence.Topology.ProcessRestarts))
	seenFaults := map[string]bool{}
	positive := 0
	for index, process := range evidence.Topology.ProcessRestarts {
		if process.ProcessID == "" || strings.ContainsAny(process.ProcessID, "/\\\r\n\x00") || index > 0 && process.ProcessID <= evidence.Topology.ProcessRestarts[index-1].ProcessID {
			return nil, errors.New("process restart census is not canonical")
		}
		if process.ExpectedRestarts != process.ObservedRestarts || uint64(len(process.FaultIDs)) != process.ExpectedRestarts {
			return nil, fmt.Errorf("process %s restart count expected/observed/faults=%d/%d/%d", process.ProcessID, process.ExpectedRestarts, process.ObservedRestarts, len(process.FaultIDs))
		}
		for faultIndex, faultID := range process.FaultIDs {
			if faultID == "" || strings.ContainsAny(faultID, "/\\\r\n\x00") || faultIndex > 0 && faultID <= process.FaultIDs[faultIndex-1] || seenFaults[faultID] {
				return nil, fmt.Errorf("process %s restart fault identities are not canonical", process.ProcessID)
			}
			seenFaults[faultID] = true
		}
		if process.ExpectedRestarts > 0 {
			positive++
		}
		counts[process.ProcessID] = process.ExpectedRestarts
	}
	if positive == 0 {
		return nil, errors.New("topology restart census does not bind the mandatory restart faults")
	}
	return counts, nil
}

func verifyFinalDeployment(evidence *FinalSemanticEvidence) error {
	deployment := evidence.Deployment
	addresses := map[string]string{
		"coordinator proxy": deployment.CoordinatorProxy, "coordinator implementation": deployment.CoordinatorImplementation,
		"settlement vault": deployment.SettlementVault, "reserve sink": deployment.ReserveSink, "governance owner": deployment.GovernanceOwner,
	}
	seen := map[string]bool{}
	for label, address := range addresses {
		if err := requireFinalEVMAddress(label, address); err != nil {
			return err
		}
		if label != "governance owner" && seen[address] {
			return fmt.Errorf("%s address is not role-separated", label)
		}
		seen[address] = true
	}
	for label, hash := range map[string]string{
		"coordinator proxy runtime code":          deployment.CoordinatorProxyCodeHash,
		"coordinator implementation runtime code": deployment.ImplementationCodeHash,
		"settlement vault runtime code":           deployment.SettlementVaultCodeHash,
		"reserve sink runtime code":               deployment.ReserveSinkCodeHash,
	} {
		if err := requireFinalHex32(label, hash); err != nil {
			return err
		}
	}
	const implementationSlot = "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc"
	if deployment.ERC1967ImplementationSlot != implementationSlot {
		return errors.New("ERC1967 implementation slot constant is incorrect")
	}
	wantImplementation := "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(deployment.CoordinatorImplementation, "0x")
	if deployment.ObservedImplementationSlot != wantImplementation {
		return errors.New("ERC1967 implementation slot does not point to the declared implementation")
	}
	if deployment.PolicyVersion == 0 || deployment.PolicyEffectiveEpoch != evidence.Window.PolicyEffectiveEpoch || deployment.PolicyEffectiveBlock != evidence.Window.PolicyEffectiveBlock {
		return errors.New("governance policy version/effective schedule is incomplete or inconsistent")
	}
	if deployment.CoordinatorNetuid != evidence.Netuid || deployment.VaultNetuid != evidence.Netuid || deployment.ReserveNetuid != evidence.Netuid {
		return errors.New("contract custody netuids do not match the signed deployment netuid")
	}
	for label, value := range map[string]string{
		"coordinator settlement vault": deployment.CoordinatorSettlementVault,
		"coordinator reserve sink":     deployment.CoordinatorReserveSink,
		"coordinator guardian":         deployment.CoordinatorGuardian,
		"coordinator active guardian":  deployment.CoordinatorActiveGuardian,
		"coordinator oracle":           deployment.CoordinatorCommitmentOracle,
		"coordinator active oracle":    deployment.CoordinatorActiveCommitmentOracle,
		"vault coordinator":            deployment.VaultCoordinator,
		"reserve recorder":             deployment.ReserveRecorder,
	} {
		if err := requireFinalEVMAddress(label, value); err != nil {
			return err
		}
	}
	if !strings.EqualFold(deployment.CoordinatorSettlementVault, deployment.SettlementVault) ||
		!strings.EqualFold(deployment.CoordinatorReserveSink, deployment.ReserveSink) ||
		!strings.EqualFold(deployment.VaultCoordinator, deployment.CoordinatorProxy) ||
		!strings.EqualFold(deployment.ReserveRecorder, deployment.CoordinatorProxy) {
		return errors.New("contract custody linkages do not match the immutable deployment")
	}
	if !strings.EqualFold(deployment.CoordinatorGuardian, deployment.CoordinatorActiveGuardian) ||
		!strings.EqualFold(deployment.CoordinatorCommitmentOracle, deployment.CoordinatorActiveCommitmentOracle) || deployment.CoordinatorPaused {
		return errors.New("contract guardian/oracle activation or pause state is unsafe")
	}
	for _, item := range []struct {
		label    string
		observed string
		expected [32]byte
	}{
		{"coordinator self coldkey", deployment.CoordinatorSelfColdkey, ss58.EvmMirrorPubkey(common.HexToAddress(deployment.CoordinatorProxy))},
		{"vault self coldkey", deployment.VaultSelfColdkey, ss58.EvmMirrorPubkey(common.HexToAddress(deployment.SettlementVault))},
		{"reserve self coldkey", deployment.ReserveSelfColdkey, ss58.EvmMirrorPubkey(common.HexToAddress(deployment.ReserveSink))},
	} {
		if item.observed != strings.ToLower(fmt.Sprintf("0x%x", item.expected[:])) {
			return fmt.Errorf("%s does not match its EVM mirror", item.label)
		}
	}
	if err := requireFinalHex32("vault escrow hotkey", deployment.VaultEscrowHotkey); err != nil {
		return err
	}
	if err := requireFinalHex32("reserve hotkey", deployment.ReserveHotkey); err != nil {
		return err
	}
	if !deployment.VaultEscrowRegistered || deployment.VaultMinimumClaimTTLBlocks == 0 || deployment.VaultMinimumTransferTaoRao == 0 || deployment.PlanDefaultMinTransferTaoRao == 0 || deployment.VaultMinimumTransferTaoRao != deployment.PlanDefaultMinTransferTaoRao {
		return errors.New("vault custody registration/TTL/minimum-transfer differs from the signed plan")
	}
	if deployment.Snapshot != evidence.EVMTerminalHead {
		return errors.New("contract deployment state is not terminal-window bound")
	}
	return verifyFinalArtifact("contract deployment artifact", deployment.Artifact, "contract-deployment")
}

func finalSettlementVaultValues(label string, state FinalSettlementVaultState) (map[string]*big.Int, error) {
	if err := verifyFinalHead(label+" block", state.Block); err != nil {
		return nil, err
	}
	encoded := map[string]string{
		"total captured": state.TotalCapturedRao, "total paid": state.TotalPaidRao,
		"escrow accounted": state.EscrowAccountedRao, "pending funding": state.PendingFundingRao,
		"outstanding liability": state.OutstandingLiabilityRao, "live escrow stake": state.LiveEscrowStakeRao,
	}
	values := make(map[string]*big.Int, len(encoded))
	for name, value := range encoded {
		parsed, err := finalNonnegativeInteger(label+" "+name, value)
		if err != nil {
			return nil, err
		}
		values[name] = parsed
	}
	if values["total captured"].Cmp(new(big.Int).Add(values["total paid"], values["escrow accounted"])) != 0 {
		return nil, fmt.Errorf("%s totalCaptured != totalPaid + escrowAccounted", label)
	}
	if values["escrow accounted"].Cmp(new(big.Int).Add(values["pending funding"], values["outstanding liability"])) != 0 {
		return nil, fmt.Errorf("%s escrowAccounted != pendingFunding + outstandingLiability", label)
	}
	if values["live escrow stake"].Cmp(values["escrow accounted"]) < 0 {
		return nil, fmt.Errorf("%s live escrow stake does not back accounted escrow", label)
	}
	return values, nil
}

func verifyFinalSettlementAccounting(evidence *FinalSemanticEvidence) error {
	accounting := evidence.SettlementAccounting
	if accounting.Before.Block != evidence.Window.BaselineHead || accounting.After.Block != evidence.EVMTerminalHead {
		return errors.New("settlement-vault accounting is not bound to the acceptance baseline and terminal checkpoints")
	}
	before, err := finalSettlementVaultValues("settlement-vault baseline", accounting.Before)
	if err != nil {
		return err
	}
	after, err := finalSettlementVaultValues("settlement-vault terminal", accounting.After)
	if err != nil {
		return err
	}
	fields := []struct {
		name  string
		delta string
	}{
		{"total captured", accounting.TotalCapturedDeltaRao}, {"total paid", accounting.TotalPaidDeltaRao},
		{"escrow accounted", accounting.EscrowAccountedDeltaRao}, {"pending funding", accounting.PendingFundingDeltaRao},
		{"outstanding liability", accounting.OutstandingLiabilityDeltaRao}, {"live escrow stake", accounting.LiveEscrowStakeDeltaRao},
	}
	for _, field := range fields {
		got, err := finalSignedInteger("settlement-vault "+field.name+" delta", field.delta)
		if err != nil {
			return err
		}
		want := new(big.Int).Sub(after[field.name], before[field.name])
		if got.Cmp(want) != 0 {
			return fmt.Errorf("settlement-vault %s delta does not match snapshots", field.name)
		}
	}
	if after["total captured"].Cmp(before["total captured"]) < 0 || after["total paid"].Cmp(before["total paid"]) < 0 {
		return errors.New("settlement-vault cumulative captured/paid counters decreased")
	}
	capturedEvents, err := finalNonnegativeInteger("EmissionCaptured event sum", accounting.EmissionCapturedEventRao)
	if err != nil {
		return err
	}
	paidEvents, err := finalNonnegativeInteger("ClaimPaid event sum", accounting.ClaimPaidEventRao)
	if err != nil {
		return err
	}
	if capturedEvents.String() != accounting.TotalCapturedDeltaRao || paidEvents.String() != accounting.TotalPaidDeltaRao {
		return errors.New("settlement-vault cumulative deltas do not equal EmissionCaptured/ClaimPaid event sums")
	}
	return nil
}

func verifyFinalReserve(evidence *FinalSemanticEvidence) error {
	reserve := evidence.Reserve
	principalBefore, err := finalPositiveInteger("reserve principal before", reserve.PrincipalBeforeRao)
	if err != nil {
		return err
	}
	principalAfter, err := finalPositiveInteger("reserve principal after", reserve.PrincipalAfterRao)
	if err != nil {
		return err
	}
	liveBefore, err := finalPositiveInteger("reserve live stake before", reserve.LiveStakeBeforeRao)
	if err != nil {
		return err
	}
	liveAfter, err := finalPositiveInteger("reserve live stake after", reserve.LiveStakeAfterRao)
	if err != nil {
		return err
	}
	if reserve.Before != evidence.Window.BaselineHead || reserve.After != evidence.EVMTerminalHead {
		return errors.New("reserve evidence is not bound to the acceptance baseline and terminal checkpoints")
	}
	if principalAfter.Cmp(principalBefore) < 0 || liveBefore.Cmp(principalBefore) < 0 || liveAfter.Cmp(principalAfter) < 0 || liveAfter.Cmp(liveBefore) < 0 {
		return errors.New("reserve principal/live stake violates one-way backing")
	}
	if liveBefore.Cmp(principalBefore) <= 0 || liveAfter.Cmp(principalAfter) <= 0 {
		return errors.New("reserve stake does not prove native emission auto-compounding above principal at both checkpoints")
	}
	principalDelta, err := finalNonnegativeInteger("reserve principal delta", reserve.PrincipalDeltaRao)
	if err != nil {
		return err
	}
	principalAdded, err := finalNonnegativeInteger("ReservePrincipalAdded sum", reserve.PrincipalAddedRao)
	if err != nil {
		return err
	}
	if principalDelta.Cmp(new(big.Int).Sub(principalAfter, principalBefore)) != 0 || principalAdded.Cmp(principalDelta) != 0 || len(reserve.PrincipalAdditions) == 0 {
		return errors.New("reserve principal delta does not equal the ReservePrincipalAdded receipt sum")
	}
	additionSum := new(big.Int)
	runningPrincipal := new(big.Int).Set(principalBefore)
	operatorPrincipalByNO := make(map[uint64]*big.Int, evidence.ExpectedOperators)
	seenTransactions := map[string]bool{}
	for index, addition := range reserve.PrincipalAdditions {
		if addition.NoID == 0 || addition.NoID > uint64(evidence.ExpectedOperators) || addition.Epoch < evidence.Window.FirstEpoch || addition.Epoch >= evidence.Window.FirstEpoch+evidence.Window.EpochCount {
			return fmt.Errorf("ReservePrincipalAdded row %d has an invalid epoch/operator", index)
		}
		amount, err := finalPositiveInteger("ReservePrincipalAdded amount", addition.AmountRao)
		if err != nil {
			return err
		}
		operatorPrincipal, err := finalPositiveInteger("ReservePrincipalAdded operator principal", addition.OperatorPrincipalRao)
		if err != nil {
			return err
		}
		totalPrincipal, err := finalPositiveInteger("ReservePrincipalAdded total principal", addition.TotalPrincipalRao)
		if err != nil {
			return err
		}
		liveStake, err := finalPositiveInteger("ReservePrincipalAdded live stake", addition.LiveStakeRao)
		if err != nil {
			return err
		}
		wantTotal := new(big.Int).Add(runningPrincipal, amount)
		priorOperator, seenOperator := operatorPrincipalByNO[addition.NoID]
		operatorConsistent := operatorPrincipal.Cmp(amount) >= 0
		if seenOperator {
			operatorConsistent = operatorPrincipal.Cmp(new(big.Int).Add(priorOperator, amount)) == 0
		}
		if totalPrincipal.Cmp(wantTotal) != 0 || !operatorConsistent || operatorPrincipal.Cmp(totalPrincipal) > 0 || liveStake.Cmp(totalPrincipal) <= 0 {
			return fmt.Errorf("ReservePrincipalAdded row %d violates operator/total/live backing", index)
		}
		if err := verifyFinalEVMReceipt("ReservePrincipalAdded", addition.Receipt, evidence.Window.BaselineHead.Number+1, evidence.EVMTerminalHead.Number); err != nil {
			return err
		}
		if seenTransactions[addition.Receipt.TransactionHash] || index > 0 && addition.Receipt.Block.Number < reserve.PrincipalAdditions[index-1].Receipt.Block.Number {
			return errors.New("ReservePrincipalAdded receipts are duplicated or non-canonical")
		}
		seenTransactions[addition.Receipt.TransactionHash] = true
		additionSum.Add(additionSum, amount)
		runningPrincipal.Set(totalPrincipal)
		operatorPrincipalByNO[addition.NoID] = new(big.Int).Set(operatorPrincipal)
	}
	if additionSum.Cmp(principalAdded) != 0 || runningPrincipal.Cmp(principalAfter) != 0 {
		return errors.New("ReservePrincipalAdded receipt amounts do not sum to the signed principal addition")
	}
	return verifyFinalArtifact("reserve one-way artifact", reserve.Artifact, "reserve-state")
}

func verifyFinalExitCriteria(evidence *FinalSemanticEvidence) error {
	requiredIDs := finalRequiredExitCriteriaForPhase(evidence.Phase)
	if len(evidence.ExitCriteria) != len(requiredIDs) {
		return fmt.Errorf("final exit criteria=%d, want %d", len(evidence.ExitCriteria), len(requiredIDs))
	}
	requiredAssertions := map[string]map[string]uint64{
		"all-miner-tier-assignments": {
			"active_fleet_member_bindings": uint64(finalHeadCandidateCount * 4),
			"miner_tier_assignments":       uint64(evidence.ExpectedMiners),
			"pool_tail_assignments":        uint64(evidence.ExpectedMiners - finalHeadCandidateCount*4),
		},
		"deposit-conviction-receipts": {
			"operator_epoch_deposit_audits": uint64(evidence.ExpectedValidators*evidence.ExpectedOperators) * evidence.Window.EpochCount,
			"operator_conviction_receipts":  uint64(evidence.ExpectedOperators),
		},
		"dishonest-deposit-recovery":    {"dishonest_underpayments_succeeded": 1, "recovery_topups_succeeded": 1},
		"invalid-merkle-proof-rejected": {"invalid_merkle_attempts_rejected": 1},
		"no-process-log-anomalies":      {"error_warning_panic_restart_anomalies": 0},
		"payout-double-claim-rejected":  {"double_claim_attempts_rejected": 1},
		"reserve-one-way-backed":        {"reserve_backing_violations": 0},
		"theta-head-tail-realized":      {"verified_theta_weight_vectors": uint64(evidence.ExpectedValidators) * evidence.Window.EpochCount},
		"unauthorized-upgrade-rejected": {"unauthorized_upgrade_attempts_rejected": 1},
	}
	requestHashes := map[string]bool{}
	if evidence.PublicVerification != nil {
		for _, exchange := range evidence.PublicVerification.Exchanges {
			requestHashes[exchange.RequestHash] = true
		}
	}
	for i, criterion := range evidence.ExitCriteria {
		if criterion.ID != requiredIDs[i] || criterion.Expected == "" || criterion.Observed == "" || !criterion.Passed || criterion.Checkpoint != evidence.EVMTerminalHead || len(criterion.Artifacts) == 0 || (evidence.PublicVerification != nil && len(criterion.PublicRequestHashes) == 0) {
			return fmt.Errorf("final exit criterion %q is missing, failed, or non-canonical", criterion.ID)
		}
		wantAssertions := requiredAssertions[criterion.ID]
		if len(criterion.Assertions) != len(wantAssertions) {
			return fmt.Errorf("final exit criterion %s typed assertions=%d, want %d", criterion.ID, len(criterion.Assertions), len(wantAssertions))
		}
		for assertionIndex, assertion := range criterion.Assertions {
			if assertion.Metric == "" || (assertionIndex > 0 && assertion.Metric <= criterion.Assertions[assertionIndex-1].Metric) {
				return fmt.Errorf("final exit criterion %s assertions are not canonical", criterion.ID)
			}
			want, ok := wantAssertions[assertion.Metric]
			if !ok || assertion.Expected != want || assertion.Observed != assertion.Expected {
				return fmt.Errorf("final exit criterion %s assertion %s did not meet its typed expectation: expected=%d observed=%d want=%d known=%t", criterion.ID, assertion.Metric, assertion.Expected, assertion.Observed, want, ok)
			}
		}
		failedReceipts, successfulReceipts := 0, 0
		for receiptIndex, receipt := range criterion.EVMReceipts {
			// Preparation is deliberately part of the live adversarial campaign,
			// but complete-epoch acceptance begins only after preparation. Bind
			// those receipts to the immutable campaign-start checkpoint rather
			// than incorrectly requiring them to follow the later baseline.
			if err := verifyFinalEVMReceiptStatus("exit criterion "+criterion.ID, receipt, evidence.EVMCampaignStartHead.Number, evidence.Window.TerminalBlock, receipt.Status); err != nil {
				return err
			}
			if receiptIndex > 0 && receipt.TransactionHash <= criterion.EVMReceipts[receiptIndex-1].TransactionHash {
				return fmt.Errorf("final exit criterion %s EVM receipts are not canonical", criterion.ID)
			}
			if receipt.Status == "failed" {
				failedReceipts++
			} else {
				successfulReceipts++
			}
		}
		switch criterion.ID {
		case "dishonest-deposit-recovery":
			if failedReceipts != 0 || successfulReceipts < 2 {
				return errors.New("dishonest deposit recovery requires successful underpayment and top-up receipts")
			}
		}
		for _, artifact := range criterion.Artifacts {
			if err := verifyFinalArtifact("exit criterion "+criterion.ID, artifact, "exit-criterion"); err != nil {
				return err
			}
		}
		for j, hash := range criterion.PublicRequestHashes {
			if err := requireFinalHex32("exit criterion public request hash", hash); err != nil {
				return err
			}
			if j > 0 && hash <= criterion.PublicRequestHashes[j-1] {
				return fmt.Errorf("exit criterion %s public request hashes are not canonical", criterion.ID)
			}
			if evidence.PublicVerification != nil && !requestHashes[hash] {
				return fmt.Errorf("exit criterion %s references absent public RPC request %s", criterion.ID, hash)
			}
		}
	}
	return nil
}

func finalRequiredExitCriteriaForPhase(phase string) []string {
	result := append([]string(nil), finalCommonRequiredExitCriterionIDs...)
	if phase == "production-soak" {
		result = append(result, finalProductionRequiredExitCriterionIDs...)
	}
	sort.Strings(result)
	return result
}

func verifyFinalPools(evidence *FinalSemanticEvidence) (map[uint64]FinalPoolUIDEvidence, error) {
	if len(evidence.Pools) != evidence.ExpectedOperators {
		return nil, fmt.Errorf("pool ownership records=%d, want %d", len(evidence.Pools), evidence.ExpectedOperators)
	}
	byNO := make(map[uint64]FinalPoolUIDEvidence, len(evidence.Pools))
	uids := map[uint16]bool{}
	rootSigners := map[string]bool{}
	serverKeys := map[string]bool{}
	if !common.IsHexAddress(evidence.Deployment.SettlementVault) {
		return nil, errors.New("pool ownership has no valid settlement vault address")
	}
	vaultMirror := ss58.EvmMirrorPubkey(common.HexToAddress(evidence.Deployment.SettlementVault))
	vaultColdkey, err := ss58.Encode(vaultMirror, ss58.BittensorPrefix)
	if err != nil {
		return nil, fmt.Errorf("encode expected settlement-vault coldkey: %w", err)
	}
	for i, pool := range evidence.Pools {
		if pool.NoID == 0 || (i > 0 && pool.NoID <= evidence.Pools[i-1].NoID) {
			return nil, errors.New("pool ownership records are not canonically ordered")
		}
		if pool.Coldkey != vaultColdkey {
			return nil, fmt.Errorf("pool no=%d is not owned by the immutable settlement-vault coldkey", pool.NoID)
		}
		if pool.OperatorColdkey == "" || pool.OperatorColdkey == pool.Coldkey {
			return nil, fmt.Errorf("pool no=%d does not separate operator registry identity from vault custody", pool.NoID)
		}
		if uids[pool.UID] || pool.Hotkey == "" || pool.DepositHotkey == "" || !pool.Registered || !pool.Active || pool.VersionCount == 0 || pool.EffectiveEpoch > evidence.Window.FirstEpoch {
			return nil, fmt.Errorf("pool ownership no=%d is incomplete or duplicated", pool.NoID)
		}
		for label, address := range map[string]string{"deposit signer": pool.DepositSigner, "payout root signer": pool.PayoutRootSigner} {
			canonical, err := finalCanonicalAddress(address)
			if err != nil || canonical != address {
				return nil, fmt.Errorf("pool no=%d %s is invalid", pool.NoID, label)
			}
		}
		if rootSigners[pool.PayoutRootSigner] {
			return nil, errors.New("operator payout root signers are not distinct")
		}
		rootSigners[pool.PayoutRootSigner] = true
		if len(pool.ServerKeyHistory) == 0 {
			return nil, fmt.Errorf("pool no=%d server key history is empty", pool.NoID)
		}
		for keyIndex, key := range pool.ServerKeyHistory {
			if keyIndex > 0 && key.KeyID <= pool.ServerKeyHistory[keyIndex-1].KeyID {
				return nil, fmt.Errorf("pool no=%d server key history is not canonical", pool.NoID)
			}
			decoded, err := finalEd25519PublicKey("operator server key", key.PublicKey)
			if err != nil || serverKeys[string(decoded)] {
				return nil, fmt.Errorf("pool no=%d server key %d is invalid or reused", pool.NoID, key.KeyID)
			}
			serverKeys[string(decoded)] = true
		}
		uids[pool.UID] = true
		if err := verifyFinalEVMReceipt("pool registration", pool.Registration, 1, evidence.Window.TerminalBlock); err != nil {
			return nil, fmt.Errorf("pool no=%d: %w", pool.NoID, err)
		}
		if err := verifyFinalEVMReceipt("pool cumulative conviction", pool.ConvictionReceipt, 1, evidence.Window.TerminalBlock); err != nil {
			return nil, fmt.Errorf("pool no=%d: %w", pool.NoID, err)
		}
		if pool.Snapshot != evidence.NativeTerminalHead {
			return nil, fmt.Errorf("pool no=%d ownership is not terminal-window bound", pool.NoID)
		}
		if _, err := finalNonnegativeInteger("pool final carry", pool.FinalCarryRao); err != nil {
			return nil, err
		}
		if err := verifyFinalArtifact("pool ownership artifact", pool.OwnershipArtifact, "native-ownership"); err != nil {
			return nil, err
		}
		byNO[pool.NoID] = pool
	}
	return byNO, nil
}

func verifyFinalValidators(evidence *FinalSemanticEvidence, pools map[uint64]FinalPoolUIDEvidence) (map[uint64]FinalValidatorIdentityEvidence, error) {
	if len(evidence.Validators) != evidence.ExpectedValidators {
		return nil, fmt.Errorf("validator records=%d, want %d", len(evidence.Validators), evidence.ExpectedValidators)
	}
	byID := make(map[uint64]FinalValidatorIdentityEvidence, len(evidence.Validators))
	uids := map[uint16]bool{}
	vpks := map[string]bool{}
	for i, validator := range evidence.Validators {
		if validator.ValidatorID == 0 || (i > 0 && validator.ValidatorID <= evidence.Validators[i-1].ValidatorID) {
			return nil, errors.New("validator records are not canonically ordered")
		}
		if err := verifyFinalValidatorIdentity(evidence, &validator, uids, vpks); err != nil {
			return nil, err
		}
		if uint64(len(validator.Cycles)) != evidence.Window.EpochCount {
			return nil, fmt.Errorf("validator %d CRv4 cycles=%d, want %d", validator.ValidatorID, len(validator.Cycles), evidence.Window.EpochCount)
		}
		var previousSubnetEpoch uint64
		for cycleIndex := range validator.Cycles {
			cycle := &validator.Cycles[cycleIndex]
			wantSettlement := evidence.Window.FirstEpoch + uint64(cycleIndex)
			if cycle.SettlementEpoch != wantSettlement || (cycleIndex > 0 && cycle.SubnetEpoch <= previousSubnetEpoch) {
				return nil, fmt.Errorf("validator %d CRv4 cycle lineage is incomplete at settlement epoch %d", validator.ValidatorID, wantSettlement)
			}
			if err := verifyFinalCRv4Cycle(evidence, validator.ValidatorID, validator.UID, cycle, pools); err != nil {
				return nil, fmt.Errorf("validator %d settlement epoch %d: %w", validator.ValidatorID, cycle.SettlementEpoch, err)
			}
			previousSubnetEpoch = cycle.SubnetEpoch
		}
		byID[validator.ValidatorID] = validator
	}
	return byID, nil
}

func verifyFinalDishonestDeposit(evidence *FinalSemanticEvidence, pools map[uint64]FinalPoolUIDEvidence, validators map[uint64]FinalValidatorIdentityEvidence) error {
	if evidence.Phase == "release-1.0" {
		if evidence.DishonestDeposit != nil {
			return errors.New("release semantic evidence unexpectedly contains a dishonest-deposit decision")
		}
		return nil
	}
	dishonest := evidence.DishonestDeposit
	if evidence.Phase != "production-soak" || dishonest == nil {
		return errors.New("production semantic evidence lacks typed dishonest-deposit decisions")
	}
	pool, ok := pools[dishonest.NoID]
	required, requiredErr := finalPositiveInteger("dishonest required deposit", dishonest.RequiredDepositRao)
	observed, observedErr := finalPositiveInteger("dishonest observed deposit", dishonest.ObservedDepositRao)
	recoveryRequired, recoveryRequiredErr := finalPositiveInteger("recovery required deposit", dishonest.RecoveryRequiredDepositRao)
	recoveryObserved, recoveryObservedErr := finalPositiveInteger("recovery observed deposit", dishonest.RecoveryObservedDepositRao)
	if !ok || pool.UID != dishonest.PoolUID || requiredErr != nil || observedErr != nil || recoveryRequiredErr != nil || recoveryObservedErr != nil || observed.Cmp(required) >= 0 || recoveryObserved.Cmp(recoveryRequired) != 0 {
		return errors.New("dishonest-deposit pool or exact demand amounts are invalid")
	}
	if err := verifyFinalEVMReceipt("dishonest underpayment", dishonest.UnderpaymentReceipt, evidence.EVMCampaignStartHead.Number, evidence.EVMTerminalHead.Number); err != nil {
		return err
	}
	if err := verifyFinalEVMReceipt("dishonest recovery", dishonest.RecoveryDepositReceipt, evidence.EVMCampaignStartHead.Number, evidence.EVMTerminalHead.Number); err != nil {
		return err
	}
	if dishonest.RecoveryDepositReceipt.Block.Number <= dishonest.UnderpaymentReceipt.Block.Number || dishonest.RecoveryDepositReceipt.TransactionHash == dishonest.UnderpaymentReceipt.TransactionHash || len(dishonest.Penalties) != evidence.ExpectedValidators || len(dishonest.Recoveries) != evidence.ExpectedValidators {
		return errors.New("dishonest-deposit receipt order or validator census is invalid")
	}
	receiptMatches := func(left, right FinalEVMReceipt) bool {
		return left.TransactionHash == right.TransactionHash && left.Block == right.Block && left.Status == right.Status && left.LogsHash == right.LogsHash
	}
	verifyDecision := func(decision *FinalDishonestDepositDecision, recovery bool) error {
		validator, ok := validators[decision.ValidatorID]
		if !ok || decision.ValidatorUID != validator.UID || decision.PoolUID != dishonest.PoolUID {
			return errors.New("dishonest-deposit decision identity differs from validator/pool ownership")
		}
		minimumEVM := evidence.EVMCampaignStartHead.Number
		if recovery {
			minimumEVM = evidence.Window.StartBlock
		}
		if err := verifyFinalCRv4CycleFrom(evidence, decision.ValidatorID, decision.ValidatorUID, &decision.Cycle, pools, minimumEVM); err != nil {
			return err
		}
		var poolWeight *FinalPoolWeightEvidence
		for index := range decision.Cycle.Pools {
			if decision.Cycle.Pools[index].NoID == dishonest.NoID {
				poolWeight = &decision.Cycle.Pools[index]
			}
		}
		present, applied := false, uint16(0)
		for _, submitted := range decision.Cycle.Submitted {
			if submitted.UID == dishonest.PoolUID {
				present, applied = true, submitted.Value
			}
		}
		if poolWeight == nil || decision.PoolPresent != present || decision.PoolAppliedWeight != applied || poolWeight.AppliedWeight != applied {
			return errors.New("dishonest-deposit declared pool application differs from the signed vector")
		}
		if !recovery {
			if decision.Cycle.SettlementEpoch+1 != evidence.Window.FirstEpoch || decision.PoolPresent || decision.PoolAppliedWeight != 0 || poolWeight.AuditCompliant || poolWeight.AuditStatus != validatorpkg.DepositAuditMismatch || poolWeight.AuditDisposition != "zero_pool_weight" || poolWeight.RequiredDepositRao != dishonest.RequiredDepositRao || poolWeight.ObservedDepositRao != dishonest.ObservedDepositRao || !receiptMatches(poolWeight.DepositReceipt, dishonest.UnderpaymentReceipt) {
				return errors.New("dishonest-deposit penalty is not an exact zero-weight underpayment decision")
			}
			return nil
		}
		if decision.Cycle.SettlementEpoch < evidence.Window.FirstEpoch || decision.Cycle.SettlementEpoch >= evidence.Window.FirstEpoch+evidence.Window.EpochCount || !decision.PoolPresent || decision.PoolAppliedWeight == 0 || !poolWeight.AuditCompliant || poolWeight.AuditStatus != validatorpkg.DepositAuditCompliant || poolWeight.AuditDisposition != "pool_weight_eligible" || poolWeight.RequiredDepositRao != dishonest.RecoveryRequiredDepositRao || poolWeight.ObservedDepositRao != dishonest.RecoveryObservedDepositRao || !receiptMatches(poolWeight.DepositReceipt, dishonest.RecoveryDepositReceipt) {
			return errors.New("dishonest-deposit recovery is not an exact positive-weight compliant decision")
		}
		matched := false
		for index := range validator.Cycles {
			if finalJSONEqual(validator.Cycles[index], decision.Cycle) {
				matched = true
			}
		}
		if !matched {
			return errors.New("dishonest-deposit recovery decision is not an accepted validator cycle")
		}
		return nil
	}
	for index := 0; index < evidence.ExpectedValidators; index++ {
		wantID := uint64(index + 1)
		if dishonest.Penalties[index].ValidatorID != wantID || dishonest.Recoveries[index].ValidatorID != wantID {
			return errors.New("dishonest-deposit decisions are not canonically ordered")
		}
		if err := verifyDecision(&dishonest.Penalties[index], false); err != nil {
			return fmt.Errorf("validator %d dishonest-deposit penalty: %w", wantID, err)
		}
		if err := verifyDecision(&dishonest.Recoveries[index], true); err != nil {
			return fmt.Errorf("validator %d dishonest-deposit recovery: %w", wantID, err)
		}
	}
	return nil
}

// Checks one native validator identity independently from its epoch cycles.
// UID zero is a real Subtensor UID; only reuse is invalid.
func verifyFinalValidatorIdentity(evidence *FinalSemanticEvidence, validator *FinalValidatorIdentityEvidence, seenUIDs map[uint16]bool, seenVPKs map[string]bool) error {
	stake, err := finalNonnegativeInteger("validator stake", validator.StakeRao)
	if err != nil || stake.Sign() == 0 || seenUIDs[validator.UID] || validator.Hotkey == "" || validator.Coldkey == "" || !validator.Registered || !validator.ValidatorPermit || validator.ValidatorTrustU16 == 0 {
		return fmt.Errorf("validator %d registration/stake/permit/vtrust evidence is incomplete", validator.ValidatorID)
	}
	vpk, err := finalEd25519PublicKey("validator path VPK", validator.PathVPK)
	if err != nil || seenVPKs[string(vpk)] {
		return fmt.Errorf("validator %d path VPK is invalid or reused", validator.ValidatorID)
	}
	if err := verifyFinalNativeReceipt("validator registration", validator.Registration, 0, evidence.NativeTerminalHead.Number, true); err != nil {
		return fmt.Errorf("validator %d: %w", validator.ValidatorID, err)
	}
	if validator.Snapshot != evidence.NativeTerminalHead {
		return fmt.Errorf("validator %d state is not terminal-window bound", validator.ValidatorID)
	}
	if err := verifyFinalArtifact("validator snapshot artifact", validator.SnapshotArtifact, "native-validator-state"); err != nil {
		return err
	}
	seenUIDs[validator.UID] = true
	seenVPKs[string(vpk)] = true
	return nil
}

func verifyFinalCRv4Cycle(evidence *FinalSemanticEvidence, validatorID uint64, validatorUID uint16, cycle *FinalCRv4Cycle, pools map[uint64]FinalPoolUIDEvidence) error {
	return verifyFinalCRv4CycleFrom(evidence, validatorID, validatorUID, cycle, pools, evidence.Window.StartBlock)
}

func verifyFinalCRv4CycleFrom(evidence *FinalSemanticEvidence, validatorID uint64, validatorUID uint16, cycle *FinalCRv4Cycle, pools map[uint64]FinalPoolUIDEvidence, minimumEVMBlock uint64) error {
	if cycle.SubnetEpoch == 0 || cycle.QualityMinimumPPM == 0 || cycle.QualityMinimumPPM > cycle.QualityMaximumPPM || cycle.QualityMaximumPPM > 1_000_000 || cycle.MaximumHeadFleets != finalHeadSlotCount || cycle.MaxWeightLimitU16 == 0 || cycle.Formula != finalWeightFormula {
		return errors.New("CRv4 policy inputs are incomplete")
	}
	if cycle.NativeSnapshot.Number < evidence.NativeStartHead.Number || cycle.NativeSnapshot.Number > evidence.NativeTerminalHead.Number {
		return errors.New("native snapshot is outside the evidence window")
	}
	if err := verifyFinalHead("CRv4 native snapshot", cycle.NativeSnapshot); err != nil {
		return err
	}
	if cycle.EVMSnapshot.Number < minimumEVMBlock || cycle.EVMSnapshot.Number >= evidence.Window.EndBlock {
		return errors.New("EVM snapshot is outside the accepted complete epochs")
	}
	if err := verifyFinalHead("CRv4 EVM snapshot", cycle.EVMSnapshot); err != nil {
		return err
	}
	theta, err := finalPositiveRational("theta", cycle.Theta)
	if err != nil || theta.Cmp(big.NewRat(1, 1)) > 0 {
		return errors.New("theta is not a canonical rational in (0,1]")
	}
	if len(cycle.Candidates) != finalHeadCandidateCount {
		return fmt.Errorf("candidate evidence=%d, want %d", len(cycle.Candidates), finalHeadCandidateCount)
	}
	eligibleUIDs := make([]uint16, len(cycle.Candidates))
	eligibleScores := make([]validatorpkg.RationalJSON, len(cycle.Candidates))
	selectedUIDs := make([]uint16, 0, finalHeadSlotCount)
	rejectedUIDs := make([]uint16, 0, finalHeadCandidateCount-finalHeadSlotCount)
	headInputs := make([]validatorpkg.ExactWeightInput, 0, finalHeadSlotCount)
	seenCandidate := map[uint16]bool{}
	for i, candidate := range cycle.Candidates {
		score, scoreErr := finalPositiveRational("candidate raw score", candidate.RawScore)
		if scoreErr != nil || candidate.Rank != uint16(i+1) || seenCandidate[candidate.UID] {
			return fmt.Errorf("candidate rank %d is incomplete, duplicated, or non-canonical", i+1)
		}
		seenCandidate[candidate.UID] = true
		eligibleUIDs[i] = candidate.UID
		eligibleScores[i] = validatorpkg.RationalJSON{Numerator: candidate.RawScore.Numerator, Denominator: candidate.RawScore.Denominator}
		if i < finalHeadSlotCount {
			if !candidate.Selected || candidate.AppliedWeight == 0 {
				return fmt.Errorf("top-200 candidate rank %d was not positively applied", i+1)
			}
			selectedUIDs = append(selectedUIDs, candidate.UID)
			headInputs = append(headInputs, validatorpkg.ExactWeightInput{UID: candidate.UID, Score: score})
		} else {
			if candidate.Selected || candidate.AppliedWeight != 0 {
				return fmt.Errorf("zero-weight reject rank %d was applied", i+1)
			}
			rejectedUIDs = append(rejectedUIDs, candidate.UID)
		}
	}
	if err := validatorpkg.ValidateHeadSelectionEvidence(eligibleUIDs, eligibleScores, selectedUIDs, rejectedUIDs, cycle.MaximumHeadFleets); err != nil {
		return fmt.Errorf("head selection: %w", err)
	}
	masked := map[uint16]bool{}
	for i, uid := range cycle.MaskedUIDs {
		if (i > 0 && uid <= cycle.MaskedUIDs[i-1]) || masked[uid] {
			return errors.New("masked UIDs are not canonical")
		}
		masked[uid] = true
		if seenCandidate[uid] {
			return fmt.Errorf("eligible candidate UID %d is also masked", uid)
		}
	}
	if !masked[validatorUID] {
		return fmt.Errorf("validator %d self UID %d is not masked", validatorID, validatorUID)
	}
	if len(cycle.Pools) != len(pools) {
		return fmt.Errorf("pool weight records=%d, want %d", len(cycle.Pools), len(pools))
	}
	poolInputs := make([]validatorpkg.ExactWeightInput, 0, len(cycle.Pools))
	for i := range cycle.Pools {
		pool := &cycle.Pools[i]
		if i > 0 && pool.NoID <= cycle.Pools[i-1].NoID {
			return errors.New("pool weight records are not canonical")
		}
		ownership, ok := pools[pool.NoID]
		if !ok || ownership.UID != pool.UID || masked[pool.UID] || seenCandidate[pool.UID] {
			return fmt.Errorf("pool weight no=%d is not bound to an unmasked owned UID", pool.NoID)
		}
		if pool.ObservedAtBlock != cycle.EVMSnapshot.Number || pool.ArtifactSigner != ownership.PayoutRootSigner || pool.RootCommitter != ownership.PayoutRootSigner || pool.RootSigner != ownership.PayoutRootSigner {
			return fmt.Errorf("pool weight no=%d artifact/root authority or observation checkpoint differs from the active operator version: observed=%d snapshot=%d artifact=%q committer=%q signer=%q want=%q", pool.NoID, pool.ObservedAtBlock, cycle.EVMSnapshot.Number, pool.ArtifactSigner, pool.RootCommitter, pool.RootSigner, ownership.PayoutRootSigner)
		}
		rawScore, eligible, err := verifyFinalPoolWeight(evidence, cycle.SettlementEpoch, cycle.QualityMinimumPPM, cycle.QualityMaximumPPM, pool)
		if err != nil {
			return fmt.Errorf("pool no=%d: %w", pool.NoID, err)
		}
		if eligible {
			poolInputs = append(poolInputs, validatorpkg.ExactWeightInput{UID: pool.UID, Score: rawScore})
		}
	}
	protoTheta, err := finalProtocolRational(cycle.Theta)
	if err != nil {
		return err
	}
	uids, scores, err := validatorpkg.BuildWeightVectorExact(poolInputs, headInputs, protoTheta, masked)
	if err != nil {
		return fmt.Errorf("reconstruct exact weights: %w", err)
	}
	capped, err := crv4.ApplyMaxWeightLimitRational(scores, cycle.MaxWeightLimitU16)
	if err != nil {
		return fmt.Errorf("reconstruct weight cap: %w", err)
	}
	valueUIDs, values, err := crv4.NormalizeRationalToU16(uids, capped)
	if err != nil {
		return fmt.Errorf("reconstruct u16 weights: %w", err)
	}
	if err := finalRepairMaxWeightLimitU16(valueUIDs, values, cycle.MaxWeightLimitU16); err != nil {
		return fmt.Errorf("reconstruct u16 cap repair: %w", err)
	}
	if len(cycle.Submitted) != len(valueUIDs) {
		return fmt.Errorf("submitted weights=%d, reconstructed %d", len(cycle.Submitted), len(valueUIDs))
	}
	valueByUID := make(map[uint16]uint16, len(valueUIDs))
	for i, uid := range valueUIDs {
		valueByUID[uid] = values[i]
		got := cycle.Submitted[i]
		if got.UID != uid || got.Value != values[i] {
			return fmt.Errorf("submitted weight %d does not match reconstructed UID/value", i)
		}
		wantScore := finalRationalFromBig(scores[i])
		if got.Score != wantScore {
			return fmt.Errorf("submitted weight UID %d exact score does not match", uid)
		}
		if i > 0 && got.UID <= cycle.Submitted[i-1].UID {
			return errors.New("submitted weights are not canonical")
		}
	}
	for _, candidate := range cycle.Candidates {
		if candidate.AppliedWeight != valueByUID[candidate.UID] {
			return fmt.Errorf("candidate UID %d applied value does not match submitted vector", candidate.UID)
		}
	}
	for _, pool := range cycle.Pools {
		value, submitted := valueByUID[pool.UID]
		if pool.AuditCompliant && (!submitted || value == 0) {
			return fmt.Errorf("eligible pool UID %d is absent or zero in submitted vector", pool.UID)
		}
		if !pool.AuditCompliant && submitted {
			return fmt.Errorf("rejected pool UID %d appears in submitted vector", pool.UID)
		}
		if pool.AppliedWeight != value {
			return fmt.Errorf("pool UID %d applied value does not match submitted vector", pool.UID)
		}
	}
	var realizedHead, realizedPool, realizedTotal uint64
	for _, candidate := range cycle.Candidates {
		if candidate.Selected {
			realizedHead += uint64(candidate.AppliedWeight)
		}
	}
	for _, pool := range cycle.Pools {
		realizedPool += uint64(pool.AppliedWeight)
	}
	for _, submitted := range cycle.Submitted {
		realizedTotal += uint64(submitted.Value)
	}
	if cycle.RealizedHeadValue != realizedHead || cycle.RealizedPoolValue != realizedPool || cycle.RealizedTotalValue != realizedTotal || realizedHead+realizedPool != realizedTotal {
		return errors.New("realized theta head/tail u16 allocation does not match the submitted vector")
	}
	encodedValues, err := json.Marshal(values)
	if err != nil {
		return err
	}
	if cycle.ValuesHash != bytesSHA256(encodedValues) {
		return errors.New("CRv4 values hash does not match reconstructed u16 vector")
	}
	if err := requireFinalHex32("intent vector hash", cycle.IntentVectorHash); err != nil {
		return err
	}
	if err := verifyFinalArtifact("steering intent artifact", cycle.IntentArtifact, "steering-intent"); err != nil {
		return err
	}
	if err := verifyFinalArtifact("validator release measurement", cycle.MeasurementArtifact, "validator-release-measurement"); err != nil {
		return err
	}
	if err := verifyFinalArtifact("validator release measurement envelope", cycle.MeasurementEnvelope, "validator-release-measurement-envelope"); err != nil {
		return err
	}
	if err := verifyFinalNativeReceipt("CRv4 commit", cycle.Commit, evidence.NativeStartHead.Number, evidence.NativeTerminalHead.Number, true); err != nil {
		return err
	}
	if err := verifyFinalNativeReceipt("CRv4 reveal", cycle.Reveal, evidence.NativeStartHead.Number, evidence.NativeTerminalHead.Number, false); err != nil {
		return err
	}
	if err := verifyFinalNativeReceipt("CRv4 application", cycle.Application, evidence.NativeStartHead.Number, evidence.NativeTerminalHead.Number, false); err != nil {
		return err
	}
	if cycle.Commit.Block.Number > cycle.Reveal.Block.Number || cycle.Reveal.Block.Number > cycle.Application.Block.Number {
		return errors.New("CRv4 commit/reveal/application receipt order is invalid")
	}
	return nil
}

func verifyFinalPoolWeight(evidence *FinalSemanticEvidence, settlementEpoch uint64, minimumQualityPPM, maximumQualityPPM uint32, pool *FinalPoolWeightEvidence) (*big.Rat, bool, error) {
	if pool.SourceEpoch+1 != settlementEpoch || pool.UsageBytes == 0 || pool.RateNumeratorRaoPerGiB == 0 || pool.RateDenominator == 0 || pool.Formula != finalDepositFormula {
		return nil, false, errors.New("deposit/rate lineage is incomplete")
	}
	if _, err := finalNonnegativeInteger("conviction before", pool.ConvictionBeforeRao); err != nil {
		return nil, false, err
	}
	capRao, err := finalPositiveInteger("deposit epoch cap", pool.EpochDepositCapRao)
	if err != nil {
		return nil, false, err
	}
	required, err := finalNonnegativeInteger("required deposit", pool.RequiredDepositRao)
	if err != nil {
		return nil, false, err
	}
	observed, err := finalNonnegativeInteger("observed deposit", pool.ObservedDepositRao)
	if err != nil {
		return nil, false, err
	}
	formulaRequired := new(big.Int).Mul(new(big.Int).SetUint64(pool.UsageBytes), new(big.Int).SetUint64(pool.RateNumeratorRaoPerGiB))
	formulaRequired.Quo(formulaRequired, new(big.Int).Mul(new(big.Int).SetUint64(pool.RateDenominator), new(big.Int).SetUint64(1<<30)))
	if formulaRequired.Cmp(capRao) > 0 {
		formulaRequired.Set(capRao)
	}
	if required.Cmp(formulaRequired) != 0 {
		return nil, false, errors.New("required deposit does not match exact floor-and-cap formula")
	}
	if err := requireFinalSHA256("signed payout artifact content hash", pool.ArtifactContentHash); err != nil {
		return nil, false, err
	}
	if err := requireFinalHex32("committed payout artifact hash", pool.ArtifactHash); err != nil {
		return nil, false, err
	}
	if !strings.EqualFold(pool.ArtifactHash, "0x"+strings.TrimPrefix(pool.ArtifactContentHash, "sha256:")) {
		return nil, false, errors.New("payout artifact content address does not match committed artifact hash")
	}
	if err := requireFinalHex32("committed payout root", pool.PayoutRoot); err != nil {
		return nil, false, err
	}
	for label, address := range map[string]string{"artifact signer": pool.ArtifactSigner, "root committer": pool.RootCommitter, "root signer": pool.RootSigner} {
		if err := requireFinalEVMAddress(label, address); err != nil || address != strings.ToLower(address) {
			return nil, false, fmt.Errorf("%s is not a canonical operator authority", label)
		}
	}
	if pool.RootCommitter != pool.RootSigner || pool.SourceStartBlock == 0 || pool.SourceStartBlock > pool.SourceEndBlock || pool.SourceEndBlock > pool.RootCommitBlock || pool.RootCommitBlock > pool.ObservedAtBlock || pool.ObservedAtBlock > pool.ArtifactDeadlineBlock {
		return nil, false, errors.New("payout artifact boundary, commitment, observation, or deadline lineage is invalid")
	}
	if err := requireFinalHex32("payout source start hash", pool.SourceStartHash); err != nil {
		return nil, false, err
	}
	if err := requireFinalHex32("payout source end hash", pool.SourceEndHash); err != nil {
		return nil, false, err
	}
	if err := verifyFinalArtifact("signed payout artifact", pool.PayoutArtifact, "payout-artifact"); err != nil {
		return nil, false, err
	}
	if err := verifyFinalEVMReceipt("operator deposit", pool.DepositReceipt, 1, evidence.Window.TerminalBlock); err != nil {
		return nil, false, err
	}
	if !pool.AuditCompliant {
		if pool.AuditStatus != validatorpkg.DepositAuditMismatch || pool.AuditDisposition != "zero_pool_weight" || pool.AuditError == "" || strings.ContainsAny(pool.AuditError, "\r\n\x00") || observed.Cmp(required) >= 0 || pool.AppliedWeight != 0 || pool.QualityPPM != 0 || pool.QualityFactor != (FinalRational{Numerator: "0", Denominator: "1"}) || pool.ImpliedUsageGiB != (FinalRational{Numerator: "0", Denominator: "1"}) || pool.RawScore != (FinalRational{Numerator: "0", Denominator: "1"}) {
			return nil, false, errors.New("rejected deposit audit is not an exact underpayment/zero-pool disposition")
		}
		return big.NewRat(0, 1), false, nil
	}
	if pool.AuditStatus != validatorpkg.DepositAuditCompliant || pool.AuditDisposition != "pool_weight_eligible" || pool.AuditError != "" || observed.Cmp(required) != 0 {
		return nil, false, errors.New("deposit audit is not compliant and pool-weight eligible")
	}
	if pool.QualityPPM == 0 || pool.QualityPPM > 1_000_000 {
		return nil, false, errors.New("quality PPM is outside (0,1000000]")
	}
	qualityPPM := pool.QualityPPM
	if qualityPPM < minimumQualityPPM {
		qualityPPM = minimumQualityPPM
	}
	if qualityPPM > maximumQualityPPM {
		qualityPPM = maximumQualityPPM
	}
	quality, err := finalPositiveRational("quality factor", pool.QualityFactor)
	if err != nil || quality.Cmp(new(big.Rat).SetFrac(new(big.Int).SetUint64(uint64(qualityPPM)), big.NewInt(1_000_000))) != 0 {
		return nil, false, errors.New("quality factor does not match quality PPM")
	}
	implied, err := finalPositiveRational("implied usage", pool.ImpliedUsageGiB)
	if err != nil {
		return nil, false, err
	}
	wantImplied := new(big.Rat).SetFrac(new(big.Int).Mul(observed, new(big.Int).SetUint64(pool.RateDenominator)), new(big.Int).SetUint64(pool.RateNumeratorRaoPerGiB))
	if implied.Cmp(wantImplied) != 0 {
		return nil, false, errors.New("implied usage does not equal deposit/rate")
	}
	rawScore, err := finalPositiveRational("pool raw score", pool.RawScore)
	if err != nil {
		return nil, false, err
	}
	if rawScore.Cmp(new(big.Rat).Mul(implied, quality)) != 0 {
		return nil, false, errors.New("pool raw score does not equal implied usage times Q")
	}
	return rawScore, true, nil
}

func verifyFinalPoolEpochs(evidence *FinalSemanticEvidence, pools map[uint64]FinalPoolUIDEvidence) error {
	wantRows := int(evidence.Window.EpochCount) * evidence.ExpectedOperators
	if len(evidence.Epochs) != wantRows {
		return fmt.Errorf("pool epoch records=%d, want %d", len(evidence.Epochs), wantRows)
	}
	totals := map[string]*big.Int{}
	for _, name := range []string{"captured", "carry_in", "funded", "total", "claimed", "paid", "deferred", "outstanding", "carry_out"} {
		totals[name] = new(big.Int)
	}
	lastCarry := map[uint64]*big.Int{}
	committedByPool := map[uint64]uint64{}
	claimsByPool := map[uint64]uint64{}
	seen := map[string]bool{}
	for i := range evidence.Epochs {
		row := &evidence.Epochs[i]
		if i > 0 {
			previous := evidence.Epochs[i-1]
			if row.Epoch < previous.Epoch || (row.Epoch == previous.Epoch && row.NoID <= previous.NoID) {
				return errors.New("pool epoch records are not canonical")
			}
		}
		if row.Epoch < evidence.Window.FirstEpoch || row.Epoch >= evidence.Window.FirstEpoch+evidence.Window.EpochCount {
			return fmt.Errorf("pool epoch %d is outside acceptance", row.Epoch)
		}
		if _, ok := pools[row.NoID]; !ok {
			return fmt.Errorf("pool epoch references unknown no=%d", row.NoID)
		}
		key := fmt.Sprintf("%d/%d", row.Epoch, row.NoID)
		if seen[key] {
			return fmt.Errorf("duplicate pool epoch %s", key)
		}
		seen[key] = true
		if err := verifyFinalEVMReceipt("pool capture", row.Capture, evidence.Window.StartBlock, evidence.Window.TerminalBlock); err != nil {
			return fmt.Errorf("epoch %s: %w", key, err)
		}
		if err := verifyFinalEVMReceipt("pool finalize", row.Finalize, evidence.Window.StartBlock, evidence.Window.TerminalBlock); err != nil {
			return fmt.Errorf("epoch %s: %w", key, err)
		}
		if row.Capture.Block.Number > row.Finalize.Block.Number {
			return fmt.Errorf("epoch %s capture follows finalize", key)
		}
		switch row.RootDisposition {
		case "committed":
			committedByPool[row.NoID]++
			if row.Root == nil || row.PayoutArtifact == nil {
				return fmt.Errorf("epoch %s committed root evidence is missing", key)
			}
			if err := verifyFinalEVMReceipt("payout root", *row.Root, evidence.Window.StartBlock, evidence.Window.TerminalBlock); err != nil {
				return fmt.Errorf("epoch %s: %w", key, err)
			}
			if row.Capture.Block.Number > row.Root.Block.Number || row.Root.Block.Number > row.Finalize.Block.Number {
				return fmt.Errorf("epoch %s root receipt order is invalid", key)
			}
			if err := requireFinalHex32("payout root", row.PayoutRoot); err != nil {
				return err
			}
			if err := requireFinalHex32("payout artifact hash", row.ArtifactHash); err != nil {
				return err
			}
			if err := verifyFinalArtifact("payout artifact", *row.PayoutArtifact, "payout-artifact"); err != nil {
				return err
			}
		case "missed":
			if row.Root != nil || row.PayoutArtifact != nil || row.PayoutRoot != "" || row.ArtifactHash != "" || len(row.Claims) != 0 {
				return fmt.Errorf("epoch %s missed root has contradictory root/claim evidence", key)
			}
		default:
			return fmt.Errorf("epoch %s has unsupported root disposition %q", key, row.RootDisposition)
		}
		amounts, err := finalEpochAmounts(row)
		if err != nil {
			return fmt.Errorf("epoch %s: %w", key, err)
		}
		if amounts["funded"].Cmp(amounts["captured"]) != 0 {
			return fmt.Errorf("epoch %s funded != boundary capture", key)
		}
		if row.Status == 0 || (row.RootDisposition == "committed" && row.Status != 2) || (row.RootDisposition == "missed" && row.Status != 3) {
			return fmt.Errorf("epoch %s entitlement status does not match root disposition", key)
		}
		claimOutputs := new(big.Int).Add(amounts["paid"], amounts["deferred"])
		if amounts["claimed"].Cmp(claimOutputs) != 0 {
			return fmt.Errorf("epoch %s claimed != paid + deferred credit", key)
		}
		if previous, ok := lastCarry[row.NoID]; ok && amounts["carry_in"].Cmp(previous) != 0 {
			return fmt.Errorf("epoch %s carry-in does not equal prior carry-out", key)
		}
		lastCarry[row.NoID] = new(big.Int).Set(amounts["carry_out"])
		switch row.RootDisposition {
		case "committed":
			wantTotal := new(big.Int).Add(amounts["funded"], amounts["carry_in"])
			if amounts["total"].Cmp(wantTotal) != 0 || amounts["carry_out"].Sign() != 0 {
				return fmt.Errorf("epoch %s committed total/carry does not consume exact boundary funding plus carry-in", key)
			}
			if amounts["total"].Cmp(new(big.Int).Add(amounts["claimed"], amounts["outstanding"])) != 0 {
				return fmt.Errorf("epoch %s committed total != claimed + outstanding", key)
			}
		case "missed":
			wantCarry := new(big.Int).Add(amounts["carry_in"], amounts["funded"])
			if amounts["total"].Cmp(amounts["funded"]) != 0 || amounts["claimed"].Sign() != 0 || amounts["paid"].Sign() != 0 || amounts["deferred"].Sign() != 0 || amounts["outstanding"].Sign() != 0 || amounts["carry_out"].Cmp(wantCarry) != 0 {
				return fmt.Errorf("epoch %s missed root does not preserve funded record and cumulative operator carry", key)
			}
		}
		claimClaimed, claimPaid, claimDeferred := new(big.Int), new(big.Int), new(big.Int)
		for claimIndex, claim := range row.Claims {
			if claim.Payee == "" || (claimIndex > 0 && claim.LeafIndex <= row.Claims[claimIndex-1].LeafIndex) {
				return fmt.Errorf("epoch %s claims are incomplete or not canonical", key)
			}
			if err := verifyFinalEVMReceipt("pool claim", claim.Receipt, row.Finalize.Block.Number, evidence.Window.TerminalBlock); err != nil {
				return fmt.Errorf("epoch %s claim %d: %w", key, claim.LeafIndex, err)
			}
			claimed, err := finalPositiveInteger("claim amount", claim.ClaimedRao)
			if err != nil {
				return err
			}
			paid, err := finalNonnegativeInteger("claim payment", claim.PaidRao)
			if err != nil {
				return err
			}
			deferred, err := finalNonnegativeInteger("claim deferred credit", claim.DeferredRao)
			if err != nil {
				return err
			}
			entitlement := new(big.Int).Mul(new(big.Int).SetUint64(claim.ShareBPS), amounts["total"])
			entitlement.Quo(entitlement, big.NewInt(10_000))
			if claim.ShareBPS == 0 || claim.ShareBPS > 10_000 || claimed.Cmp(entitlement) != 0 {
				return fmt.Errorf("epoch %s claim %d amount does not equal floor(share_bps*total/10000)", key, claim.LeafIndex)
			}
			if claimed.Cmp(new(big.Int).Add(paid, deferred)) != 0 {
				return fmt.Errorf("epoch %s claim %d payment plus deferred credit mismatch", key, claim.LeafIndex)
			}
			claimClaimed.Add(claimClaimed, claimed)
			claimPaid.Add(claimPaid, paid)
			claimDeferred.Add(claimDeferred, deferred)
			claimsByPool[row.NoID]++
		}
		if claimClaimed.Cmp(amounts["claimed"]) != 0 || claimPaid.Cmp(amounts["paid"]) != 0 || claimDeferred.Cmp(amounts["deferred"]) != 0 {
			return fmt.Errorf("epoch %s claim receipts do not sum to epoch totals", key)
		}
		for name, amount := range amounts {
			totals[name].Add(totals[name], amount)
		}
	}
	for noID, pool := range pools {
		carry, ok := lastCarry[noID]
		if !ok || carry.String() != pool.FinalCarryRao {
			return fmt.Errorf("pool no=%d terminal carry does not match last accepted epoch", noID)
		}
		if committedByPool[noID] == 0 || claimsByPool[noID] == 0 {
			return fmt.Errorf("pool no=%d lacks a nonzero committed-root and successful-claim census in the accepted phase", noID)
		}
	}
	wantTotals := map[string]string{
		"captured": evidence.Conservation.CapturedRao, "carry_in": evidence.Conservation.CarryInRao,
		"funded": evidence.Conservation.FundedRao, "claimed": evidence.Conservation.ClaimedRao,
		"paid": evidence.Conservation.PaidRao, "deferred": evidence.Conservation.DeferredCreditRao,
		"outstanding": evidence.Conservation.OutstandingRao, "carry_out": evidence.Conservation.CarryOutRao,
	}
	for name, encoded := range wantTotals {
		value, err := finalNonnegativeInteger("pool conservation "+name, encoded)
		if err != nil {
			return err
		}
		if value.Cmp(totals[name]) != 0 {
			return fmt.Errorf("pool conservation %s=%s, reconstructed %s", name, encoded, totals[name])
		}
	}
	return nil
}

func finalEpochAmounts(row *FinalEpochOperatorEvidence) (map[string]*big.Int, error) {
	encoded := map[string]string{
		"captured": row.CapturedRao, "carry_in": row.CarryInRao, "funded": row.FundedRao,
		"total": row.TotalRao, "claimed": row.ClaimedRao, "paid": row.PaidRao, "deferred": row.DeferredCreditRao,
		"outstanding": row.OutstandingRao, "carry_out": row.CarryOutRao,
	}
	values := make(map[string]*big.Int, len(encoded))
	for name, value := range encoded {
		parsed, err := finalNonnegativeInteger("epoch "+name, value)
		if err != nil {
			return nil, err
		}
		values[name] = parsed
	}
	return values, nil
}

func verifyFinalRewards(evidence *FinalSemanticEvidence, pools map[uint64]FinalPoolUIDEvidence, validators map[uint64]FinalValidatorIdentityEvidence) error {
	wantPerEpoch := len(evidence.HeadFleets) + len(pools) + len(validators)
	if len(evidence.NativeRewards) != int(evidence.Window.EpochCount)*wantPerEpoch {
		return fmt.Errorf("native reward deltas=%d, want %d complete per-epoch head/pool/validator subjects", len(evidence.NativeRewards), int(evidence.Window.EpochCount)*wantPerEpoch)
	}
	headByFleet := make(map[uint64]FinalHeadFleetEvidence, len(evidence.HeadFleets))
	for _, fleet := range evidence.HeadFleets {
		headByFleet[fleet.FleetID] = fleet
	}
	expectedHeadReward := map[string]string{}
	expectedPoolReward := map[string]string{}
	for epoch := evidence.Window.FirstEpoch; epoch < evidence.Window.FirstEpoch+evidence.Window.EpochCount; epoch++ {
		selected := map[uint64]int{}
		poolEligible := map[uint64]bool{}
		for _, validator := range evidence.Validators {
			for _, cycle := range validator.Cycles {
				if cycle.SettlementEpoch != epoch {
					continue
				}
				for _, candidate := range cycle.Candidates {
					if candidate.Selected {
						selected[candidate.FleetID]++
					}
				}
				for _, pool := range cycle.Pools {
					poolEligible[pool.NoID] = poolEligible[pool.NoID] || pool.AuditCompliant
				}
			}
		}
		for fleetID := range headByFleet {
			expectation := "observed"
			switch selected[fleetID] {
			case 0:
				expectation = "zero"
			case len(evidence.Validators):
				expectation = "positive"
			}
			expectedHeadReward[fmt.Sprintf("%d/%d", epoch, fleetID)] = expectation
		}
		for noID := range pools {
			expectation := "zero"
			if poolEligible[noID] {
				expectation = "positive"
			}
			expectedPoolReward[fmt.Sprintf("%d/%d", epoch, noID)] = expectation
		}
	}
	type nativeCheckpointKey struct {
		Head ChainHead
		UID  uint16
	}
	type nativeCheckpointState struct {
		Hotkey    string
		Emission  string
		Stake     string
		Incentive uint16
		Dividends uint16
	}
	type ownerCheckpointKey struct {
		Head    ChainHead
		Hotkey  string
		Coldkey string
	}
	nativeCheckpoints := map[nativeCheckpointKey]nativeCheckpointState{}
	ownerCheckpoints := map[ownerCheckpointKey]string{}
	recordNativeCheckpoint := func(reward FinalNativeRewardDelta, head ChainHead, emission, stake string, incentive, dividends uint16) error {
		key := nativeCheckpointKey{Head: head, UID: reward.UID}
		state := nativeCheckpointState{Hotkey: reward.Hotkey, Emission: emission, Stake: stake, Incentive: incentive, Dividends: dividends}
		if prior, exists := nativeCheckpoints[key]; exists && prior != state {
			return fmt.Errorf("native reward checkpoint conflict for UID %d at block %d/%s", reward.UID, head.Number, head.Hash)
		}
		nativeCheckpoints[key] = state
		return nil
	}
	recordOwnerCheckpoint := func(head ChainHead, hotkey, coldkey, stake string) error {
		key := ownerCheckpointKey{Head: head, Hotkey: hotkey, Coldkey: coldkey}
		if prior, exists := ownerCheckpoints[key]; exists && prior != stake {
			return fmt.Errorf("native owner stake checkpoint conflict for %s/%s at block %d/%s", hotkey, coldkey, head.Number, head.Hash)
		}
		ownerCheckpoints[key] = stake
		return nil
	}
	seen := map[string]bool{}
	for i, reward := range evidence.NativeRewards {
		if i > 0 {
			previous := evidence.NativeRewards[i-1]
			if reward.Epoch < previous.Epoch || (reward.Epoch == previous.Epoch && (reward.Role < previous.Role || (reward.Role == previous.Role && reward.SubjectID <= previous.SubjectID))) {
				return errors.New("native reward deltas are not canonical")
			}
		}
		key := fmt.Sprintf("%d/%s/%d", reward.Epoch, reward.Role, reward.SubjectID)
		if seen[key] {
			return fmt.Errorf("duplicate native reward delta %s", key)
		}
		seen[key] = true
		var ownerHotkey, ownerColdkey [32]byte
		var err error
		switch reward.Role {
		case "head":
			fleet, ok := headByFleet[reward.SubjectID]
			wantUID, uidErr := finalSemanticRewardUIDAt(evidence, reward.SubjectID, reward.Epoch, fleet.UID)
			if !ok || uidErr != nil || wantUID != reward.UID {
				return fmt.Errorf("reward %s does not match head fleet UID ownership", key)
			}
			ownerHotkey, ownerColdkey, err = finalSemanticRewardOwnerPairAt(evidence, reward.Role, reward.SubjectID, reward.Epoch)
			if err != nil {
				return err
			}
			if reward.Expected != expectedHeadReward[fmt.Sprintf("%d/%d", reward.Epoch, reward.SubjectID)] {
				return fmt.Errorf("reward %s expectation does not match validator selection consensus", key)
			}
		case "pool":
			pool, ok := pools[reward.SubjectID]
			if !ok || pool.UID != reward.UID {
				return fmt.Errorf("reward %s does not match pool UID ownership", key)
			}
			ownerHotkey, ownerColdkey, err = finalSemanticSS58Pair("reward "+key, pool.Hotkey, pool.Coldkey)
			if err != nil {
				return err
			}
			if reward.Expected != expectedPoolReward[fmt.Sprintf("%d/%d", reward.Epoch, reward.SubjectID)] {
				return fmt.Errorf("reward %s expectation does not match pool audit eligibility", key)
			}
		case "validator":
			validator, ok := validators[reward.SubjectID]
			if !ok || validator.UID != reward.UID {
				return fmt.Errorf("reward %s does not match validator UID ownership", key)
			}
			ownerHotkey, ownerColdkey, err = finalSemanticSS58Pair("reward "+key, validator.Hotkey, validator.Coldkey)
			if err != nil {
				return err
			}
			if reward.Expected != "positive" {
				return fmt.Errorf("reward %s validator expectation is not positive", key)
			}
		default:
			return fmt.Errorf("reward %s has unsupported role", key)
		}
		if reward.Expected != "positive" && reward.Expected != "zero" && reward.Expected != "observed" {
			return fmt.Errorf("reward %s has unsupported expectation %q", key, reward.Expected)
		}
		if err := verifyFinalHead("reward before", reward.Before); err != nil {
			return err
		}
		if err := verifyFinalHead("reward after", reward.After); err != nil {
			return err
		}
		if reward.Epoch < evidence.Window.FirstEpoch || reward.Epoch >= evidence.Window.FirstEpoch+evidence.Window.EpochCount || reward.Before.Number < evidence.NativeStartHead.Number || reward.After.Number > evidence.NativeTerminalHead.Number || reward.Before.Number >= reward.After.Number {
			return fmt.Errorf("reward %s is outside or does not span the native evidence window", key)
		}
		before, err := finalNonnegativeInteger("reward before emission", reward.BeforeRao)
		if err != nil {
			return err
		}
		after, err := finalNonnegativeInteger("reward after emission", reward.AfterRao)
		if err != nil {
			return err
		}
		delta, err := finalSignedInteger("reward emission change", reward.DeltaRao)
		if err != nil {
			return err
		}
		if delta.Cmp(new(big.Int).Sub(after, before)) != 0 {
			return fmt.Errorf("reward %s emission change does not match snapshots", key)
		}
		stakeBefore, err := finalNonnegativeInteger("reward aggregate stake before", reward.StakeBeforeRao)
		if err != nil {
			return err
		}
		stakeAfter, err := finalNonnegativeInteger("reward aggregate stake after", reward.StakeAfterRao)
		if err != nil {
			return err
		}
		stakeDelta, err := finalSignedInteger("reward aggregate stake change", reward.StakeDeltaRao)
		if err != nil {
			return err
		}
		if stakeDelta.Cmp(new(big.Int).Sub(stakeAfter, stakeBefore)) != 0 {
			return fmt.Errorf("reward %s aggregate stake change does not match snapshots", key)
		}
		if reward.Role == "head" && ((reward.Expected == "positive" && stakeDelta.Sign() <= 0) || (reward.Expected == "zero" && stakeDelta.Sign() != 0)) {
			return fmt.Errorf("reward %s head aggregate stake change does not match unanimous selection", key)
		}
		if reward.Role == "validator" && stakeDelta.Sign() <= 0 {
			return fmt.Errorf("reward %s validator aggregate stake did not grow", key)
		}
		if reward.Hotkey != strings.ToLower(fmt.Sprintf("0x%x", ownerHotkey[:])) || reward.OwnerColdkey != strings.ToLower(fmt.Sprintf("0x%x", ownerColdkey[:])) || ownerHotkey == ([32]byte{}) {
			return fmt.Errorf("reward %s hotkey/owner coldkey does not match signed role identity", key)
		}
		if err := recordNativeCheckpoint(reward, reward.Before, reward.BeforeRao, reward.StakeBeforeRao, reward.BeforeIncentiveU16, reward.BeforeDividendsU16); err != nil {
			return err
		}
		if err := recordNativeCheckpoint(reward, reward.After, reward.AfterRao, reward.StakeAfterRao, reward.AfterIncentiveU16, reward.AfterDividendsU16); err != nil {
			return err
		}
		if evidence.FleetLifecycle != nil && reward.Epoch == evidence.FleetLifecycle.State.ProviderEffectiveEpoch && reward.Before != evidence.FleetLifecycle.State.PostRegistrationRewardBaseline {
			return fmt.Errorf("reward %s predates or bypasses the authenticated post-registration baseline", key)
		}
		ownerBefore, err := finalNonnegativeInteger("reward owner stake before", reward.OwnerStakeBeforeRao)
		if err != nil {
			return err
		}
		ownerAfter, err := finalNonnegativeInteger("reward owner stake after", reward.OwnerStakeAfterRao)
		if err != nil {
			return err
		}
		ownerDelta, err := finalSignedInteger("reward owner stake change", reward.OwnerStakeDeltaRao)
		if err != nil {
			return err
		}
		if ownerDelta.Cmp(new(big.Int).Sub(ownerAfter, ownerBefore)) != 0 {
			return fmt.Errorf("reward %s owner-pair stake change does not match snapshots", key)
		}
		if reward.OwnerStakeBeforeEVM.Number != reward.Before.Number || reward.OwnerStakeAfterEVM.Number != reward.After.Number {
			return fmt.Errorf("reward %s owner-pair EVM checkpoints do not match native reward heights", key)
		}
		if err := verifyFinalHead("reward owner stake before EVM", reward.OwnerStakeBeforeEVM); err != nil {
			return err
		}
		if err := verifyFinalHead("reward owner stake after EVM", reward.OwnerStakeAfterEVM); err != nil {
			return err
		}
		if err := recordOwnerCheckpoint(reward.OwnerStakeBeforeEVM, reward.Hotkey, reward.OwnerColdkey, reward.OwnerStakeBeforeRao); err != nil {
			return err
		}
		if err := recordOwnerCheckpoint(reward.OwnerStakeAfterEVM, reward.Hotkey, reward.OwnerColdkey, reward.OwnerStakeAfterRao); err != nil {
			return err
		}
		if reward.Role == "head" {
			if ownerBefore.Cmp(stakeBefore) != 0 || ownerAfter.Cmp(stakeAfter) != 0 {
				return fmt.Errorf("reward %s head owner-pair stake differs from TotalHotkeyAlpha", key)
			}
			if reward.Expected == "positive" && ownerDelta.Sign() <= 0 || reward.Expected == "zero" && ownerDelta.Sign() != 0 {
				return fmt.Errorf("reward %s head owner-pair change does not match unanimous selection", key)
			}
		}
		if reward.Role == "pool" && (ownerBefore.Cmp(stakeBefore) != 0 || ownerAfter.Cmp(stakeAfter) != 0) {
			return fmt.Errorf("reward %s pool custody position does not reconcile to TotalHotkeyAlpha", key)
		}
		if reward.Role == "validator" {
			if ownerDelta.Sign() <= 0 {
				return fmt.Errorf("reward %s validator owner-pair stake did not grow", key)
			}
			if reward.SubjectID == 1 {
				if reward.ReserveColdkey != evidence.Deployment.ReserveSelfColdkey {
					return fmt.Errorf("reward %s reserve-validator sink coldkey differs from contract custody", key)
				}
				reserveBefore, err := finalNonnegativeInteger("reserve-validator sink stake before", reward.ReserveStakeBeforeRao)
				if err != nil {
					return err
				}
				reserveAfter, err := finalNonnegativeInteger("reserve-validator sink stake after", reward.ReserveStakeAfterRao)
				if err != nil {
					return err
				}
				reserveDelta, err := finalSignedInteger("reserve-validator sink stake change", reward.ReserveStakeDeltaRao)
				if err != nil {
					return err
				}
				if reserveDelta.Cmp(new(big.Int).Sub(reserveAfter, reserveBefore)) != 0 || reserveDelta.Sign() <= 0 ||
					stakeBefore.Cmp(new(big.Int).Add(ownerBefore, reserveBefore)) != 0 || stakeAfter.Cmp(new(big.Int).Add(ownerAfter, reserveAfter)) != 0 {
					return fmt.Errorf("reward %s reserve-validator aggregate does not reconcile owner and reserve-sink positions", key)
				}
				if err := recordOwnerCheckpoint(reward.OwnerStakeBeforeEVM, reward.Hotkey, reward.ReserveColdkey, reward.ReserveStakeBeforeRao); err != nil {
					return err
				}
				if err := recordOwnerCheckpoint(reward.OwnerStakeAfterEVM, reward.Hotkey, reward.ReserveColdkey, reward.ReserveStakeAfterRao); err != nil {
					return err
				}
			} else {
				if reward.ReserveColdkey != "" || reward.ReserveStakeBeforeRao != "" || reward.ReserveStakeAfterRao != "" || reward.ReserveStakeDeltaRao != "" || ownerBefore.Cmp(stakeBefore) != 0 || ownerAfter.Cmp(stakeAfter) != 0 {
					return fmt.Errorf("reward %s independent validator stake does not reconcile to its owner position", key)
				}
			}
		} else if reward.ReserveColdkey != "" || reward.ReserveStakeBeforeRao != "" || reward.ReserveStakeAfterRao != "" || reward.ReserveStakeDeltaRao != "" {
			return fmt.Errorf("reward %s non-reserve role has reserve-validator stake fields", key)
		}
		// Subtensor Emission is a per-epoch vector, not a cumulative balance.
		// A selected UID can therefore have a smaller positive value in the next
		// epoch. Selection/rejection is proven by the terminal emission and
		// incentive/dividends channels, never by assuming a monotonic delta.
		if reward.Role == "validator" {
			if reward.BeforeIncentiveU16 != 0 || reward.AfterIncentiveU16 != 0 {
				return fmt.Errorf("reward %s validator emission/dividends channels are inconsistent", key)
			}
		} else if reward.BeforeDividendsU16 != 0 || reward.AfterDividendsU16 != 0 {
			return fmt.Errorf("reward %s head/pool emission/incentive channels are inconsistent", key)
		}
		terminalScore := reward.AfterIncentiveU16
		if reward.Role == "validator" {
			terminalScore = reward.AfterDividendsU16
		}
		if (reward.Expected == "positive" && (after.Sign() == 0 || terminalScore == 0)) || (reward.Expected == "zero" && (after.Sign() != 0 || terminalScore != 0)) {
			return fmt.Errorf("reward %s terminal native channels do not match expectation", key)
		}
		if err := verifyFinalArtifact("native reward snapshot", reward.SnapshotArtifact, "native-reward-snapshot"); err != nil {
			return err
		}
	}
	for epoch := evidence.Window.FirstEpoch; epoch < evidence.Window.FirstEpoch+evidence.Window.EpochCount; epoch++ {
		for fleetID := range headByFleet {
			if !seen[fmt.Sprintf("%d/head/%d", epoch, fleetID)] {
				return fmt.Errorf("native head reward epoch %d fleet %d is missing", epoch, fleetID)
			}
		}
		for noID := range pools {
			if !seen[fmt.Sprintf("%d/pool/%d", epoch, noID)] {
				return fmt.Errorf("native pool reward epoch %d no=%d is missing", epoch, noID)
			}
		}
		for validatorID := range validators {
			if !seen[fmt.Sprintf("%d/validator/%d", epoch, validatorID)] {
				return fmt.Errorf("native validator reward epoch %d validator=%d is missing", epoch, validatorID)
			}
		}
	}
	return nil
}

func verifyFinalPathProofs(evidence *FinalSemanticEvidence, pools map[uint64]FinalPoolUIDEvidence, validators map[uint64]FinalValidatorIdentityEvidence) error {
	want := len(pools) * len(validators)
	if len(evidence.PathProofs) != want {
		return fmt.Errorf("validator path proof records=%d, want %d", len(evidence.PathProofs), want)
	}
	seen := map[string]bool{}
	for i, proof := range evidence.PathProofs {
		if i > 0 {
			previous := evidence.PathProofs[i-1]
			if proof.ValidatorID < previous.ValidatorID || (proof.ValidatorID == previous.ValidatorID && proof.NoID <= previous.NoID) {
				return errors.New("validator path proofs are not canonical")
			}
		}
		if _, ok := validators[proof.ValidatorID]; !ok {
			return fmt.Errorf("path proof references unknown validator %d", proof.ValidatorID)
		}
		if _, ok := pools[proof.NoID]; !ok {
			return fmt.Errorf("path proof references unknown no=%d", proof.NoID)
		}
		key := fmt.Sprintf("%d/%d", proof.ValidatorID, proof.NoID)
		if seen[key] {
			return fmt.Errorf("duplicate validator path proof %s", key)
		}
		seen[key] = true
		lastEpoch := evidence.Window.FirstEpoch + evidence.Window.EpochCount - 1
		if proof.FirstEpoch != evidence.Window.FirstEpoch || proof.LastEpoch != lastEpoch || proof.ProofCount < evidence.Window.EpochCount {
			return fmt.Errorf("path proof %s does not cover every accepted epoch", key)
		}
		if proof.TrailDepth < 1 || proof.TrailDepth > 255 {
			return fmt.Errorf("path proof %s trail depth is invalid", key)
		}
		if err := requireFinalSHA256("path proof content hash", proof.ProofsHash); err != nil {
			return err
		}
		if err := verifyFinalArtifact("validator path proofs", proof.Artifact, "validator-path-proofs"); err != nil {
			return err
		}
		if proof.ProofsHash != proof.Artifact.ContentHash {
			return fmt.Errorf("path proof %s locator hash mismatch", key)
		}
	}
	return nil
}

type finalSemanticArtifactVerificationTask func() (*validatorpkg.ReleaseMeasurementArtifact, error)

type finalSemanticArtifactVerificationResult struct {
	measurement *validatorpkg.ReleaseMeasurementArtifact
	err         error
}

func finalSemanticArtifactVerificationWorkers(taskCount int) int {
	if taskCount <= 0 {
		return 0
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	if workers > taskCount {
		workers = taskCount
	}
	return workers
}

// Measurement verification is CPU-bound and independent across signed
// validator cycles. Keep results in canonical task order so the caller can
// retain deterministic error and lineage semantics after all workers join.
func runFinalSemanticArtifactVerificationTasks(ctx context.Context, tasks []finalSemanticArtifactVerificationTask, workers int) []finalSemanticArtifactVerificationResult {
	results := make([]finalSemanticArtifactVerificationResult, len(tasks))
	if len(tasks) == 0 {
		return results
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(tasks) {
		workers = len(tasks)
	}
	jobs := make(chan int, len(tasks))
	var wait sync.WaitGroup
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer wait.Done()
			for index := range jobs {
				if err := ctx.Err(); err != nil {
					results[index].err = err
					continue
				}
				results[index].measurement, results[index].err = tasks[index]()
			}
		}()
	}
	for index := range tasks {
		jobs <- index
	}
	close(jobs)
	wait.Wait()
	return results
}

// VerifyFinalSemanticArtifacts proves every referenced immutable object by
// content and performs an additional semantic check of steering-intent and
// JSONL path-proof artifacts.
func VerifyFinalSemanticArtifacts(ctx context.Context, evidence *FinalSemanticEvidence, load FinalArtifactLoader) error {
	if err := VerifyFinalSemanticEvidence(evidence); err != nil {
		return err
	}
	if ctx == nil || load == nil {
		return errors.New("final semantic artifact loader is unavailable")
	}
	type use struct {
		locator   FinalArtifactLocator
		pathProof *FinalValidatorPathProofEvidence
		payout    *finalPayoutArtifactExpectation
	}
	uses := make([]use, 0)
	var lifecyclePayoutExpectations []finalPayoutArtifactExpectation
	add := func(locator FinalArtifactLocator) { uses = append(uses, use{locator: locator}) }
	add(evidence.PlanArtifact)
	add(evidence.PolicyArtifact)
	add(evidence.ArchiveRetention.Artifact)
	if evidence.PriorPhase != nil {
		add(evidence.PriorPhase.Completion)
		add(evidence.PriorPhase.EvidenceManifest)
		add(evidence.PriorPhase.SemanticSupplement)
		add(evidence.PriorPhase.SemanticEvidenceEnvelope)
		add(evidence.PriorPhase.SemanticEvidence)
	}
	add(evidence.Topology.MinerManifest)
	add(evidence.Topology.BindingManifest)
	if evidence.FleetLifecycle != nil {
		add(evidence.FleetLifecycle.LineageArtifact)
		for _, decision := range evidence.FleetLifecycle.AppliedDecisions {
			add(decision.Intent)
			add(decision.Measurement)
			add(decision.Envelope)
		}
		lifecyclePayoutExpectations = make([]finalPayoutArtifactExpectation, len(evidence.FleetLifecycle.PayoutArtifacts))
		for index, item := range evidence.FleetLifecycle.PayoutArtifacts {
			var stateRecord *FleetLifecyclePayoutEvidence
			for payoutIndex := range evidence.FleetLifecycle.State.Payouts {
				candidate := &evidence.FleetLifecycle.State.Payouts[payoutIndex]
				if candidate.Epoch == item.Epoch && uint64(candidate.NoID) == item.NoID {
					stateRecord = candidate
					break
				}
			}
			if stateRecord == nil {
				return fmt.Errorf("fleet lifecycle payout artifact epoch %d operator %d has no terminal state record", item.Epoch, item.NoID)
			}
			lifecyclePayoutExpectations[index] = finalPayoutArtifactExpectation{Epoch: item.Epoch, NoID: item.NoID, ArtifactHash: "0x" + strings.TrimPrefix(stateRecord.ContentHash, "sha256:"), PayoutRoot: stateRecord.PayoutRoot}
			uses = append(uses, use{locator: item.Artifact, payout: &lifecyclePayoutExpectations[index]})
			add(item.Root.Proof)
		}
	}
	add(evidence.ContractCleanup.SupervisorStateArtifact)
	for _, operator := range evidence.ContractCleanup.Operators {
		add(operator.ResultArtifact)
		add(operator.LogArtifact)
	}
	add(evidence.Deployment.Artifact)
	add(evidence.Reserve.Artifact)
	for _, addition := range evidence.Reserve.PrincipalAdditions {
		add(addition.Receipt.Proof)
	}
	for _, criterion := range evidence.ExitCriteria {
		for _, locator := range criterion.Artifacts {
			add(locator)
		}
		for _, receipt := range criterion.EVMReceipts {
			add(receipt.Proof)
		}
	}
	for _, pool := range evidence.Pools {
		for _, receipt := range []FinalEVMReceipt{pool.Registration, pool.ConvictionReceipt} {
			add(receipt.Proof)
		}
		add(pool.OwnershipArtifact)
	}
	for _, fleet := range evidence.HeadFleets {
		add(fleet.Registration.Proof)
		add(fleet.BindingArtifact)
	}
	for _, transition := range evidence.HeadTransitions {
		add(transition.Registration.Proof)
		add(transition.Artifact)
	}
	add(evidence.ValidatorView.Artifact)
	for validatorIndex := range evidence.Validators {
		validator := &evidence.Validators[validatorIndex]
		add(validator.Registration.Proof)
		add(validator.SnapshotArtifact)
		for cycleIndex := range validator.Cycles {
			cycle := &validator.Cycles[cycleIndex]
			for _, locator := range []FinalArtifactLocator{cycle.Commit.Proof, cycle.Reveal.Proof, cycle.Application.Proof} {
				add(locator)
			}
			for _, pool := range cycle.Pools {
				uses = append(uses, use{locator: pool.PayoutArtifact, payout: &finalPayoutArtifactExpectation{NoID: pool.NoID, Epoch: pool.SourceEpoch, UsageBytes: pool.UsageBytes, PayoutRoot: pool.PayoutRoot, ArtifactHash: pool.ArtifactHash}})
				add(pool.DepositReceipt.Proof)
			}
			add(cycle.IntentArtifact)
			add(cycle.MeasurementArtifact)
			add(cycle.MeasurementEnvelope)
		}
	}
	if evidence.DishonestDeposit != nil {
		add(evidence.DishonestDeposit.UnderpaymentReceipt.Proof)
		add(evidence.DishonestDeposit.RecoveryDepositReceipt.Proof)
		for _, decisions := range [][]FinalDishonestDepositDecision{evidence.DishonestDeposit.Penalties, evidence.DishonestDeposit.Recoveries} {
			for index := range decisions {
				cycle := &decisions[index].Cycle
				for _, locator := range []FinalArtifactLocator{cycle.Commit.Proof, cycle.Reveal.Proof, cycle.Application.Proof, cycle.IntentArtifact, cycle.MeasurementArtifact, cycle.MeasurementEnvelope} {
					add(locator)
				}
				for _, pool := range cycle.Pools {
					uses = append(uses, use{locator: pool.PayoutArtifact, payout: &finalPayoutArtifactExpectation{NoID: pool.NoID, Epoch: pool.SourceEpoch, UsageBytes: pool.UsageBytes, PayoutRoot: pool.PayoutRoot, ArtifactHash: pool.ArtifactHash}})
					add(pool.DepositReceipt.Proof)
				}
			}
		}
	}
	for i := range evidence.Epochs {
		row := &evidence.Epochs[i]
		add(row.Capture.Proof)
		add(row.Finalize.Proof)
		if row.Root != nil {
			add(row.Root.Proof)
		}
		if row.PayoutArtifact != nil {
			uses = append(uses, use{locator: *row.PayoutArtifact, payout: &finalPayoutArtifactExpectation{NoID: row.NoID, Epoch: row.Epoch, PayoutRoot: row.PayoutRoot, ArtifactHash: row.ArtifactHash, Claims: append([]FinalClaimEvidence(nil), row.Claims...)}})
		}
		for _, claim := range row.Claims {
			add(claim.Receipt.Proof)
		}
	}
	for _, reward := range evidence.NativeRewards {
		add(reward.SnapshotArtifact)
	}
	for i := range evidence.PathProofs {
		proof := &evidence.PathProofs[i]
		uses = append(uses, use{locator: proof.Artifact, pathProof: proof})
	}
	cache := map[string][]byte{}
	for _, item := range uses {
		data, ok := cache[item.locator.URI]
		if !ok {
			if err := ctx.Err(); err != nil {
				return err
			}
			var err error
			data, err = load(ctx, item.locator)
			if err != nil {
				return fmt.Errorf("load final artifact %s: %w", item.locator.URI, err)
			}
			// Own the exact byte stream used for both the content-address check
			// and deep replay. A loader-backed mutable buffer cannot race the
			// verifier or change after the cache key is computed.
			data = append([]byte(nil), data...)
			cache[item.locator.URI] = data
		}
		if uint64(len(data)) != item.locator.SizeBytes || bytesSHA256(data) != item.locator.ContentHash {
			return fmt.Errorf("final artifact %s size or content hash mismatch", item.locator.URI)
		}
	}
	cacheKey, err := finalSemanticArtifactVerificationCacheKey(evidence, cache)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if finalSemanticArtifactVerificationCacheHit(cacheKey) {
		return nil
	}
	bindingData := cache[evidence.Topology.BindingManifest.URI]
	tiers, err := finalMinerTierByClient(bindingData)
	if err != nil {
		return err
	}
	seenPathIDs := map[string]bool{}
	seenTrailIDs := map[string]bool{}
	for _, item := range uses {
		data := cache[item.locator.URI]
		if item.pathProof != nil {
			validator := finalValidatorByID(evidence, item.pathProof.ValidatorID)
			pool := finalPoolByNO(evidence, item.pathProof.NoID)
			if err := verifyFinalPathProofArtifactBound(item.pathProof, data, validator, pool, seenPathIDs, seenTrailIDs); err != nil {
				return fmt.Errorf("path proof artifact %s: %w", item.locator.URI, err)
			}
		}
		if item.payout != nil {
			pool := finalPoolByNO(evidence, item.payout.NoID)
			if err := verifyFinalPayoutArtifact(evidence, pool, item.payout, tiers, data); err != nil {
				return fmt.Errorf("payout artifact %s: %w", item.locator.URI, err)
			}
		}
	}
	policy, err := verifyFinalPolicyArtifact(evidence, cache[evidence.PolicyArtifact.URI])
	if err != nil {
		return err
	}
	if _, err := verifyFinalSetupPlanArtifact(evidence, cache[evidence.PlanArtifact.URI]); err != nil {
		return err
	}
	if err := verifyFinalPolicyInputs(evidence, policy); err != nil {
		return err
	}
	if err := verifyFinalArchiveRetentionArtifact(evidence, cache[evidence.ArchiveRetention.Artifact.URI]); err != nil {
		return err
	}
	if err := verifyFinalTopologyArtifacts(evidence, cache[evidence.Topology.MinerManifest.URI], cache[evidence.Topology.BindingManifest.URI]); err != nil {
		return err
	}
	if err := verifyFinalHeadFleetBindingArtifacts(evidence, cache, cache[evidence.Topology.BindingManifest.URI]); err != nil {
		return err
	}
	if evidence.FleetLifecycle != nil {
		if err := verifyFinalFleetLifecycleArtifacts(evidence, cache[evidence.FleetLifecycle.LineageArtifact.URI]); err != nil {
			return err
		}
		decisionCount := len(evidence.FleetLifecycle.AppliedDecisions)
		if decisionCount > 0 {
			if err := runOrderedConcurrentAudits(decisionCount, finalSemanticArtifactVerificationWorkers(decisionCount), func(index int) error {
				decision := &evidence.FleetLifecycle.AppliedDecisions[index]
				if decision.CensusIndex >= uint64(len(evidence.FleetLifecycle.State.CandidateCensuses)) {
					return fmt.Errorf("fleet lifecycle signed decision %d has an invalid census index", index)
				}
				census := &evidence.FleetLifecycle.State.CandidateCensuses[decision.CensusIndex]
				var row *FleetLifecycleValidatorCensus
				for rowIndex := range census.Validators {
					if uint64(census.Validators[rowIndex].ValidatorID) == decision.ValidatorID {
						row = &census.Validators[rowIndex]
					}
				}
				if err := verifyFinalFleetLifecycleAppliedDecisionArtifacts(evidence, decision, row, finalFleetLifecycleValidator(evidence, decision.ValidatorID), cache[decision.Intent.URI], cache[decision.Measurement.URI], cache[decision.Envelope.URI]); err != nil {
					return fmt.Errorf("fleet lifecycle signed decision %d: %w", index, err)
				}
				return nil
			}); err != nil {
				return err
			}
		}
	}
	if err := verifyFinalContractCleanupArtifacts(evidence, cache); err != nil {
		return err
	}
	if err := verifyFinalDeploymentArtifact(evidence, cache[evidence.Deployment.Artifact.URI]); err != nil {
		return err
	}
	if err := verifyFinalReserveArtifact(evidence, cache[evidence.Reserve.Artifact.URI]); err != nil {
		return err
	}
	if err := verifyFinalNativeRewardArtifacts(evidence, cache); err != nil {
		return err
	}
	if err := verifyFinalNativeIdentityArtifacts(evidence, cache); err != nil {
		return err
	}
	if evidence.PriorPhase != nil {
		if err := verifyFinalPriorSemanticArtifacts(evidence, cache); err != nil {
			return err
		}
	}
	cycleTaskIndexes := make([][]int, len(evidence.Validators))
	penaltyTaskIndexes := make([]int, len(evidence.Validators))
	penaltyCycles := make([]*FinalCRv4Cycle, len(evidence.Validators))
	for index := range penaltyTaskIndexes {
		penaltyTaskIndexes[index] = -1
	}
	verificationTasks := make([]finalSemanticArtifactVerificationTask, 0)
	for validatorIndex := range evidence.Validators {
		validator := &evidence.Validators[validatorIndex]
		cycleTaskIndexes[validatorIndex] = make([]int, len(validator.Cycles))
		for cycleIndex := range validator.Cycles {
			cycle := &validator.Cycles[cycleIndex]
			cycleTaskIndexes[validatorIndex][cycleIndex] = len(verificationTasks)
			verificationTasks = append(verificationTasks, func() (*validatorpkg.ReleaseMeasurementArtifact, error) {
				var measurement *validatorpkg.ReleaseMeasurementArtifact
				err := verifyFinalIntentAndMeasurementArtifacts(evidence, validator, cycle, cache[cycle.IntentArtifact.URI], cache[cycle.MeasurementArtifact.URI], cache[cycle.MeasurementEnvelope.URI], &measurement)
				return measurement, err
			})
		}
		if evidence.DishonestDeposit != nil {
			for decisionIndex := range evidence.DishonestDeposit.Penalties {
				decision := &evidence.DishonestDeposit.Penalties[decisionIndex]
				if decision.ValidatorID == validator.ValidatorID {
					penaltyCycles[validatorIndex] = &decision.Cycle
				}
			}
			if penalty := penaltyCycles[validatorIndex]; penalty != nil && len(validator.Cycles) > 0 {
				penaltyTaskIndexes[validatorIndex] = len(verificationTasks)
				verificationTasks = append(verificationTasks, func() (*validatorpkg.ReleaseMeasurementArtifact, error) {
					var measurement *validatorpkg.ReleaseMeasurementArtifact
					err := verifyFinalIntentAndMeasurementArtifacts(evidence, validator, penalty, cache[penalty.IntentArtifact.URI], cache[penalty.MeasurementArtifact.URI], cache[penalty.MeasurementEnvelope.URI], &measurement)
					return measurement, err
				})
			}
		}
	}
	verificationResults := runFinalSemanticArtifactVerificationTasks(ctx, verificationTasks, finalSemanticArtifactVerificationWorkers(len(verificationTasks)))
	lineageTaskIndexes := make([][]int, len(evidence.Validators))
	penaltyLineageTaskIndexes := make([]int, len(evidence.Validators))
	for index := range penaltyLineageTaskIndexes {
		penaltyLineageTaskIndexes[index] = -1
	}
	lineageTasks := make([]finalSemanticArtifactVerificationTask, 0)
	for validatorIndex := range evidence.Validators {
		validator := &evidence.Validators[validatorIndex]
		lineageTaskIndexes[validatorIndex] = make([]int, len(validator.Cycles))
		for index := range lineageTaskIndexes[validatorIndex] {
			lineageTaskIndexes[validatorIndex][index] = -1
		}
		for cycleIndex := 1; cycleIndex < len(validator.Cycles); cycleIndex++ {
			previousCycle := &validator.Cycles[cycleIndex-1]
			currentResult := verificationResults[cycleTaskIndexes[validatorIndex][cycleIndex]]
			lineageTaskIndexes[validatorIndex][cycleIndex] = len(lineageTasks)
			lineageTasks = append(lineageTasks, func() (*validatorpkg.ReleaseMeasurementArtifact, error) {
				if currentResult.measurement == nil {
					return nil, nil
				}
				err := validatorpkg.VerifyReleaseMeasurementLineage(cache[previousCycle.MeasurementArtifact.URI], currentResult.measurement)
				return currentResult.measurement, err
			})
		}
		if penalty := penaltyCycles[validatorIndex]; penalty != nil && len(validator.Cycles) > 0 && penaltyTaskIndexes[validatorIndex] >= 0 {
			firstResult := verificationResults[cycleTaskIndexes[validatorIndex][0]]
			penaltyLineageTaskIndexes[validatorIndex] = len(lineageTasks)
			lineageTasks = append(lineageTasks, func() (*validatorpkg.ReleaseMeasurementArtifact, error) {
				if firstResult.measurement == nil {
					return nil, nil
				}
				err := validatorpkg.VerifyReleaseMeasurementLineage(cache[penalty.MeasurementArtifact.URI], firstResult.measurement)
				return firstResult.measurement, err
			})
		}
	}
	lineageResults := runFinalSemanticArtifactVerificationTasks(ctx, lineageTasks, finalSemanticArtifactVerificationWorkers(len(lineageTasks)))
	verifiedMeasurements := make(map[string]*validatorpkg.ReleaseMeasurementArtifact, len(verificationTasks))
	for validatorIndex := range evidence.Validators {
		validator := &evidence.Validators[validatorIndex]
		var firstAcceptedMeasurement *validatorpkg.ReleaseMeasurementArtifact
		for cycleIndex := range validator.Cycles {
			cycle := &validator.Cycles[cycleIndex]
			result := verificationResults[cycleTaskIndexes[validatorIndex][cycleIndex]]
			if result.err != nil {
				return fmt.Errorf("validator %d epoch %d measurement decision: %w", validator.ValidatorID, cycle.SettlementEpoch, result.err)
			}
			if cycleIndex > 0 {
				lineageResult := lineageResults[lineageTaskIndexes[validatorIndex][cycleIndex]]
				if lineageResult.err != nil {
					return fmt.Errorf("validator %d epoch %d measurement lineage: %w", validator.ValidatorID, cycle.SettlementEpoch, lineageResult.err)
				}
			} else {
				firstAcceptedMeasurement = result.measurement
			}
			verifiedMeasurements[cycle.MeasurementArtifact.URI] = result.measurement
		}
		if evidence.DishonestDeposit != nil {
			penalty := penaltyCycles[validatorIndex]
			if penalty == nil || len(validator.Cycles) == 0 {
				return fmt.Errorf("validator %d dishonest-deposit measurement lineage is incomplete", validator.ValidatorID)
			}
			penaltyResult := verificationResults[penaltyTaskIndexes[validatorIndex]]
			if penaltyResult.err != nil {
				return fmt.Errorf("validator %d dishonest-deposit measurement decision: %w", validator.ValidatorID, penaltyResult.err)
			}
			if penaltyResult.measurement == nil || firstAcceptedMeasurement == nil {
				return fmt.Errorf("validator %d dishonest-deposit decoded measurement lineage is incomplete", validator.ValidatorID)
			}
			lineageResult := lineageResults[penaltyLineageTaskIndexes[validatorIndex]]
			if lineageResult.err != nil {
				return fmt.Errorf("validator %d dishonest-deposit to acceptance measurement lineage: %w", validator.ValidatorID, lineageResult.err)
			}
		}
	}
	if err := verifyFinalHeadTournamentTransitionArtifactsWithMeasurements(evidence, cache, verifiedMeasurements); err != nil {
		return err
	}
	if err := verifyFinalValidatorViewTransitionArtifact(evidence, cache[evidence.ValidatorView.Artifact.URI]); err != nil {
		return err
	}
	finalSemanticArtifactVerificationCacheStore(cacheKey)
	return nil
}

func finalSemanticArtifactVerificationCacheKey(evidence *FinalSemanticEvidence, artifacts map[string][]byte) (string, error) {
	if evidence == nil || len(artifacts) == 0 || !validCanonicalHashHex(evidence.EvidenceHash) {
		return "", errors.New("final semantic artifact cache identity is incomplete")
	}
	type artifactIdentity struct {
		URI         string `json:"uri"`
		ContentHash string `json:"content_hash"`
		Size        uint64 `json:"size"`
	}
	paths := make([]string, 0, len(artifacts))
	for uri := range artifacts {
		paths = append(paths, uri)
	}
	sort.Strings(paths)
	graph := make([]artifactIdentity, 0, len(paths))
	for _, uri := range paths {
		data := artifacts[uri]
		graph = append(graph, artifactIdentity{URI: uri, ContentHash: bytesSHA256(data), Size: uint64(len(data))})
	}
	return canonicalHashHex(struct {
		EvidenceHash string             `json:"evidence_hash"`
		Artifacts    []artifactIdentity `json:"artifacts"`
	}{EvidenceHash: strings.ToLower(evidence.EvidenceHash), Artifacts: graph})
}

func finalSemanticArtifactVerificationCacheHit(key string) bool {
	finalSemanticArtifactVerificationCache.Lock()
	defer finalSemanticArtifactVerificationCache.Unlock()
	_, ok := finalSemanticArtifactVerificationCache.entries[key]
	return ok
}

func finalSemanticArtifactVerificationCacheStore(key string) {
	finalSemanticArtifactVerificationCache.Lock()
	defer finalSemanticArtifactVerificationCache.Unlock()
	if len(finalSemanticArtifactVerificationCache.entries) >= finalSemanticArtifactVerificationCacheLimit {
		// Cache eviction is purely a performance concern. Clearing the bounded
		// set is deterministic, race-safe, and can never convert a failure into
		// success because every miss performs the complete verification again.
		finalSemanticArtifactVerificationCache.entries = map[string]struct{}{}
	}
	finalSemanticArtifactVerificationCache.entries[key] = struct{}{}
}

func verifyFinalDeploymentArtifact(evidence *FinalSemanticEvidence, data []byte) error {
	if evidence == nil {
		return errors.New("contract deployment artifact evidence is unavailable")
	}
	var artifact struct {
		Deployment                   ContractDeployment  `json:"deployment"`
		Upgrade                      CoordinatorUpgrade  `json:"upgrade"`
		Terminal                     ChainHead           `json:"terminal"`
		RuntimeCodeHashes            map[string]string   `json:"runtime_code_hashes"`
		Policy                       PolicyView          `json:"policy"`
		Custody                      ContractCustodyView `json:"custody"`
		PlanHash                     string              `json:"plan_hash"`
		PlanDefaultMinTransferTaoRao uint64              `json:"plan_default_min_transfer_rao"`
		ExpectedGuardian             string              `json:"expected_guardian"`
		ExpectedCommitmentOracle     string              `json:"expected_commitment_oracle"`
	}
	if err := decodeStrictJSONBytes(data, &artifact); err != nil {
		return fmt.Errorf("decode contract deployment artifact: %w", err)
	}
	d := evidence.Deployment
	activeImplementation := artifact.Upgrade.Implementation
	if activeImplementation == (common.Address{}) {
		activeImplementation = artifact.Deployment.CoordinatorImplementation
	}
	if artifact.Deployment.DeploymentID != evidence.DeploymentID || artifact.Terminal != d.Snapshot ||
		!strings.EqualFold(artifact.Deployment.CoordinatorProxy.Hex(), d.CoordinatorProxy) ||
		!strings.EqualFold(activeImplementation.Hex(), d.CoordinatorImplementation) ||
		!strings.EqualFold(artifact.Deployment.SettlementVault.Hex(), d.SettlementVault) ||
		!strings.EqualFold(artifact.Deployment.ReserveSink.Hex(), d.ReserveSink) ||
		!strings.EqualFold(artifact.PlanHash, evidence.PlanHash) || artifact.PlanDefaultMinTransferTaoRao != d.PlanDefaultMinTransferTaoRao ||
		!strings.EqualFold(artifact.ExpectedGuardian, d.CoordinatorGuardian) || !strings.EqualFold(artifact.ExpectedCommitmentOracle, d.CoordinatorCommitmentOracle) {
		return errors.New("contract deployment artifact differs from signed deployment/plan/role evidence")
	}
	c := artifact.Custody
	if c.CoordinatorNetuid != d.CoordinatorNetuid || !strings.EqualFold(c.CoordinatorSelfColdkey, d.CoordinatorSelfColdkey) ||
		!strings.EqualFold(c.CoordinatorVault, d.CoordinatorSettlementVault) || !strings.EqualFold(c.CoordinatorReserve, d.CoordinatorReserveSink) ||
		!strings.EqualFold(c.CoordinatorGuardian, d.CoordinatorGuardian) || !strings.EqualFold(c.CoordinatorActiveGuardian, d.CoordinatorActiveGuardian) || c.CoordinatorPaused != d.CoordinatorPaused ||
		!strings.EqualFold(c.CoordinatorCommitmentOracle, d.CoordinatorCommitmentOracle) || !strings.EqualFold(c.CoordinatorActiveCommitmentOracle, d.CoordinatorActiveCommitmentOracle) ||
		!strings.EqualFold(c.VaultCoordinator, d.VaultCoordinator) || c.VaultNetuid != d.VaultNetuid || !strings.EqualFold(c.VaultSelfColdkey, d.VaultSelfColdkey) ||
		!strings.EqualFold(c.VaultEscrowHotkey, d.VaultEscrowHotkey) || c.VaultEscrowRegistered != d.VaultEscrowRegistered || c.VaultMinimumClaimTTLBlocks != d.VaultMinimumClaimTTLBlocks || c.VaultMinimumTransferRao != d.VaultMinimumTransferTaoRao ||
		!strings.EqualFold(c.ReserveRecorder, d.ReserveRecorder) || c.ReserveNetuid != d.ReserveNetuid || !strings.EqualFold(c.ReserveSelfColdkey, d.ReserveSelfColdkey) || !strings.EqualFold(c.ReserveHotkey, d.ReserveHotkey) {
		return errors.New("contract deployment artifact custody differs from signed semantic evidence")
	}
	return nil
}

func verifyFinalReserveArtifact(evidence *FinalSemanticEvidence, data []byte) error {
	if evidence == nil {
		return errors.New("reserve artifact evidence is unavailable")
	}
	var artifact struct {
		Before               *ContractView                        `json:"before"`
		After                *ContractView                        `json:"after"`
		SettlementAccounting FinalSettlementVaultAccounting       `json:"settlement_accounting"`
		PrincipalAdditions   []FinalReservePrincipalAddedEvidence `json:"principal_additions"`
	}
	if err := decodeStrictJSONBytes(data, &artifact); err != nil {
		return fmt.Errorf("decode reserve accounting artifact: %w", err)
	}
	if artifact.Before == nil || artifact.After == nil {
		return errors.New("reserve accounting artifact omits its baseline or terminal view")
	}
	if artifact.Before.FinalizedHead != evidence.Reserve.Before || artifact.After.FinalizedHead != evidence.Reserve.After {
		return errors.New("reserve accounting artifact heads differ from signed baseline/terminal evidence")
	}
	if artifact.Before.ReservePrincipal != evidence.Reserve.PrincipalBeforeRao || artifact.After.ReservePrincipal != evidence.Reserve.PrincipalAfterRao {
		return errors.New("reserve accounting artifact principal differs from signed baseline/terminal evidence")
	}
	if artifact.Before.ReserveLiveStake != evidence.Reserve.LiveStakeBeforeRao || artifact.After.ReserveLiveStake != evidence.Reserve.LiveStakeAfterRao {
		return errors.New("reserve accounting artifact live stake differs from signed baseline/terminal evidence")
	}
	if !finalJSONEqual(artifact.SettlementAccounting, evidence.SettlementAccounting) {
		return errors.New("reserve accounting artifact settlement state differs from signed baseline/terminal evidence")
	}
	if !finalJSONEqual(artifact.PrincipalAdditions, evidence.Reserve.PrincipalAdditions) {
		return errors.New("reserve accounting artifact principal additions differ from signed event evidence")
	}
	return nil
}

func verifyFinalNativeRewardArtifacts(evidence *FinalSemanticEvidence, cache map[string][]byte) error {
	if evidence == nil {
		return errors.New("native reward artifact evidence is unavailable")
	}
	type rewardArtifact struct {
		Epoch             uint64                             `json:"epoch"`
		ApplicationBlock  uint64                             `json:"application_block"`
		Before            *NativeRewardObservation           `json:"before"`
		After             *NativeRewardObservation           `json:"after"`
		BeforeOwnerStakes *FinalCollectedRewardStakeSnapshot `json:"before_owner_stakes"`
		AfterOwnerStakes  *FinalCollectedRewardStakeSnapshot `json:"after_owner_stakes"`
	}
	decoded := map[string]*rewardArtifact{}
	for _, reward := range evidence.NativeRewards {
		artifact := decoded[reward.SnapshotArtifact.URI]
		if artifact == nil {
			artifact = &rewardArtifact{}
			if err := decodeStrictJSONBytes(cache[reward.SnapshotArtifact.URI], artifact); err != nil {
				return fmt.Errorf("decode native reward artifact %s: %w", reward.SnapshotArtifact.URI, err)
			}
			decoded[reward.SnapshotArtifact.URI] = artifact
		}
		applicationBlock, err := finalSemanticApplicationBlock(evidence, reward.Epoch)
		if err != nil {
			return err
		}
		if artifact.Epoch != reward.Epoch || artifact.ApplicationBlock != applicationBlock || artifact.Before == nil || artifact.After == nil || artifact.BeforeOwnerStakes == nil || artifact.AfterOwnerStakes == nil ||
			artifact.Before.FinalizedHead != reward.Before || artifact.After.FinalizedHead != reward.After ||
			artifact.BeforeOwnerStakes.NativeHead != reward.Before || artifact.AfterOwnerStakes.NativeHead != reward.After ||
			artifact.BeforeOwnerStakes.EVMHead != reward.OwnerStakeBeforeEVM || artifact.AfterOwnerStakes.EVMHead != reward.OwnerStakeAfterEVM {
			return fmt.Errorf("native reward artifact %s is not bound to reward %d/%s/%d checkpoints", reward.SnapshotArtifact.URI, reward.Epoch, reward.Role, reward.SubjectID)
		}
		beforeEmission, beforeIncentive, beforeDividends, ok := nativeRewardAt(artifact.Before, reward.UID)
		if !ok {
			return fmt.Errorf("native reward artifact lacks before UID %d", reward.UID)
		}
		afterEmission, afterIncentive, afterDividends, ok := nativeRewardAt(artifact.After, reward.UID)
		if !ok {
			return fmt.Errorf("native reward artifact lacks after UID %d", reward.UID)
		}
		beforeAggregate, ok := nativeRewardStakeAt(artifact.Before, reward.UID)
		if !ok {
			return fmt.Errorf("native reward artifact lacks before UID %d aggregate stake", reward.UID)
		}
		afterAggregate, ok := nativeRewardStakeAt(artifact.After, reward.UID)
		if !ok {
			return fmt.Errorf("native reward artifact lacks after UID %d aggregate stake", reward.UID)
		}
		if beforeEmission.String() != reward.BeforeRao || afterEmission.String() != reward.AfterRao || beforeAggregate.String() != reward.StakeBeforeRao || afterAggregate.String() != reward.StakeAfterRao ||
			beforeIncentive != reward.BeforeIncentiveU16 || afterIncentive != reward.AfterIncentiveU16 || beforeDividends != reward.BeforeDividendsU16 || afterDividends != reward.AfterDividendsU16 {
			return fmt.Errorf("native reward artifact UID %d channels differ from signed evidence", reward.UID)
		}
		hotkey, _, err := finalSemanticRewardOwnerPairAt(evidence, reward.Role, reward.SubjectID, reward.Epoch)
		if err != nil {
			return err
		}
		ownerColdkey, err := decodeHex32("native reward owner coldkey", reward.OwnerColdkey)
		if err != nil {
			return err
		}
		ownerBefore, err := finalSemanticStakePosition(artifact.BeforeOwnerStakes, hotkey, ownerColdkey)
		if err != nil || ownerBefore.String() != reward.OwnerStakeBeforeRao {
			return stateMismatchError(err, "native reward artifact owner position differs before UID %d", reward.UID)
		}
		ownerAfter, err := finalSemanticStakePosition(artifact.AfterOwnerStakes, hotkey, ownerColdkey)
		if err != nil || ownerAfter.String() != reward.OwnerStakeAfterRao {
			return stateMismatchError(err, "native reward artifact owner position differs after UID %d", reward.UID)
		}
		if reward.ReserveColdkey != "" {
			reserveColdkey, err := decodeHex32("native reward reserve coldkey", reward.ReserveColdkey)
			if err != nil {
				return err
			}
			reserveBefore, err := finalSemanticStakePosition(artifact.BeforeOwnerStakes, hotkey, reserveColdkey)
			if err != nil || reserveBefore.String() != reward.ReserveStakeBeforeRao {
				return stateMismatchError(err, "native reward artifact reserve position differs before UID %d", reward.UID)
			}
			reserveAfter, err := finalSemanticStakePosition(artifact.AfterOwnerStakes, hotkey, reserveColdkey)
			if err != nil || reserveAfter.String() != reward.ReserveStakeAfterRao {
				return stateMismatchError(err, "native reward artifact reserve position differs after UID %d", reward.UID)
			}
		}
	}
	return nil
}

func verifyFinalNativeIdentityArtifacts(evidence *FinalSemanticEvidence, cache map[string][]byte) error {
	for _, pool := range evidence.Pools {
		var artifact struct {
			Snapshot                ChainHead                    `json:"snapshot"`
			State                   FinalCollectedNativeUIDState `json:"state"`
			SettlementVault         string                       `json:"settlement_vault"`
			VaultMirrorColdkey      string                       `json:"vault_mirror_coldkey"`
			OperatorRegistryColdkey string                       `json:"operator_registry_coldkey"`
		}
		if err := decodeStrictJSONBytes(cache[pool.OwnershipArtifact.URI], &artifact); err != nil {
			return fmt.Errorf("decode pool %d native ownership artifact: %w", pool.NoID, err)
		}
		hotkey, coldkey, err := finalSemanticSS58Pair(fmt.Sprintf("pool %d", pool.NoID), pool.Hotkey, pool.Coldkey)
		if err != nil || artifact.Snapshot != pool.Snapshot || artifact.State.UID != pool.UID || artifact.State.HotkeyPublicKey != "0x"+hex.EncodeToString(hotkey[:]) || artifact.State.ColdkeyPublicKey != "0x"+hex.EncodeToString(coldkey[:]) || artifact.State.RegistrationBlock != pool.Registration.Block.Number || !strings.EqualFold(artifact.SettlementVault, evidence.Deployment.SettlementVault) || artifact.VaultMirrorColdkey != pool.Coldkey || artifact.OperatorRegistryColdkey != pool.OperatorColdkey {
			return stateMismatchError(err, "pool %d native ownership artifact differs from its exact identity/receipt/checkpoint", pool.NoID)
		}
	}
	for _, validator := range evidence.Validators {
		var artifact struct {
			Snapshot ChainHead                    `json:"snapshot"`
			State    FinalCollectedNativeUIDState `json:"state"`
		}
		if err := decodeStrictJSONBytes(cache[validator.SnapshotArtifact.URI], &artifact); err != nil {
			return fmt.Errorf("decode validator %d native state artifact: %w", validator.ValidatorID, err)
		}
		hotkey, coldkey, err := finalSemanticSS58Pair(fmt.Sprintf("validator %d", validator.ValidatorID), validator.Hotkey, validator.Coldkey)
		if err != nil || artifact.Snapshot != validator.Snapshot || artifact.State.UID != validator.UID || artifact.State.HotkeyPublicKey != "0x"+hex.EncodeToString(hotkey[:]) || artifact.State.ColdkeyPublicKey != "0x"+hex.EncodeToString(coldkey[:]) || artifact.State.RegistrationBlock != validator.Registration.Block.Number || artifact.State.StakeRao != validator.StakeRao || artifact.State.ValidatorPermit != validator.ValidatorPermit || artifact.State.ValidatorTrustU16 != validator.ValidatorTrustU16 {
			return stateMismatchError(err, "validator %d native state artifact differs from its exact identity/receipt/checkpoint", validator.ValidatorID)
		}
	}
	return nil
}

func verifyFinalArchiveRetentionArtifact(evidence *FinalSemanticEvidence, data []byte) error {
	var receipt FinalArchiveRetentionPreflight
	if err := decodeStrictJSONBytes(data, &receipt); err != nil {
		return fmt.Errorf("decode archive-retention preflight artifact: %w", err)
	}
	if err := verifyFinalArchiveRetentionPreflight(&receipt); err != nil {
		return fmt.Errorf("verify archive-retention preflight artifact: %w", err)
	}
	declared := evidence.ArchiveRetention
	if receipt.GeneratedAt != declared.GeneratedAt || receipt.DeploymentID != declared.DeploymentID || !strings.EqualFold(receipt.PublicManifestHash, declared.PublicManifestHash) || receipt.PlannedSpanBlocks != declared.PlannedSpanBlocks || receipt.SafetyMarginBlocks != declared.SafetyMarginBlocks || receipt.RequiredDepthBlocks != declared.RequiredDepthBlocks || !strings.EqualFold(receipt.EvidenceHash, declared.EvidenceHash) {
		return errors.New("archive-retention preflight artifact differs from semantic declaration")
	}
	return nil
}

func verifyFinalPriorSemanticArtifacts(evidence *FinalSemanticEvidence, cache map[string][]byte) error {
	if evidence == nil || evidence.PriorPhase == nil {
		return nil
	}
	prior := evidence.PriorPhase
	decodeEnvelope := func(label string, locator FinalArtifactLocator, kind string) (*ReleaseEvidenceEnvelope, error) {
		var envelope ReleaseEvidenceEnvelope
		if err := decodeStrictJSONBytes(cache[locator.URI], &envelope); err != nil {
			return nil, fmt.Errorf("decode %s: %w", label, err)
		}
		if err := verifyEvidence(&envelope, nil); err != nil {
			return nil, fmt.Errorf("verify %s signature: %w", label, err)
		}
		created, err := time.Parse(time.RFC3339Nano, envelope.CreatedAt)
		if err != nil || envelope.CreatedAt != created.UTC().Format(time.RFC3339Nano) || envelope.Kind != kind || envelope.RunID != prior.RunID || envelope.DeploymentID != evidence.DeploymentID || envelope.ChainID != evidence.ChainID || envelope.Netuid != evidence.Netuid || !strings.EqualFold(envelope.GenesisHash, evidence.GenesisHash) || !strings.EqualFold(envelope.Signer.Hex(), evidence.Deployment.GovernanceOwner) {
			return nil, fmt.Errorf("%s identity, signer, or timestamp differs from the production lineage", label)
		}
		return &envelope, nil
	}
	completion, err := decodeEnvelope("prior owner completion", prior.Completion, "scenario-complete")
	if err != nil {
		return err
	}
	manifest, err := decodeEnvelope("prior evidence manifest", prior.EvidenceManifest, campaignEvidenceManifestKind)
	if err != nil {
		return err
	}
	supplement, err := decodeEnvelope("prior semantic_verified supplement", prior.SemanticSupplement, finalSemanticSupplementKind)
	if err != nil {
		return err
	}
	fileEnvelope, err := decodeEnvelope("prior semantic evidence file", prior.SemanticEvidenceEnvelope, finalSemanticSupplementFileKind)
	if err != nil {
		return err
	}
	completionCreated, _ := time.Parse(time.RFC3339Nano, completion.CreatedAt)
	manifestCreated, _ := time.Parse(time.RFC3339Nano, manifest.CreatedAt)
	supplementCreated, _ := time.Parse(time.RFC3339Nano, supplement.CreatedAt)
	fileCreated, _ := time.Parse(time.RFC3339Nano, fileEnvelope.CreatedAt)
	productionStarted, _ := time.Parse(time.RFC3339Nano, evidence.CampaignStartedAt)
	if !completionCreated.Before(productionStarted) || !manifestCreated.Before(productionStarted) || manifestCreated.After(completionCreated) || supplementCreated.Before(completionCreated) || fileCreated.Before(completionCreated) || fileCreated.After(supplementCreated) {
		return errors.New("prior completion, semantic files, and semantic_verified supplement have invalid causal ordering")
	}
	if completion.ContentHash != prior.OwnerCompletionEnvelopeHash || manifest.ContentHash != prior.EvidenceManifestEnvelopeHash || supplement.ContentHash != prior.SemanticSupplementEnvelopeHash {
		return errors.New("prior signed envelope hashes differ from production lineage")
	}
	var completePayload scenarioCompletePayload
	if err := decodeStrictJSONBytes(completion.Payload, &completePayload); err != nil || completePayload.ResultHash != prior.ResultHash || completePayload.EvidenceManifestHash != manifest.ContentHash || completePayload.LifecycleHandoff == nil || completePayload.PriorRelease != nil {
		return stateMismatchError(err, "prior owner completion does not bind the release result and evidence manifest")
	}
	var payload FinalSemanticSupplementPayload
	if err := decodeStrictJSONBytes(supplement.Payload, &payload); err != nil || payload.Schema != finalSemanticSupplementSchema || payload.Status != finalSemanticSupplementStatus || payload.Phase != "release-1.0" || payload.RunID != prior.RunID || payload.ResultHash != prior.ResultHash || payload.ScenarioCompleteHash != completion.ContentHash || payload.ScenarioEvidenceManifestHash != manifest.ContentHash || payload.SemanticEvidenceHash != prior.SemanticEvidenceHash || payload.PublicTranscriptHash != prior.PublicTranscriptHash {
		return stateMismatchError(err, "prior semantic_verified supplement differs from production lineage")
	}
	var filePayload finalSemanticSupplementFilePayload
	if err := decodeStrictJSONBytes(fileEnvelope.Payload, &filePayload); err != nil || filePayload.Schema != finalSemanticSupplementFileSchema || filePayload.RunID != prior.RunID || filePayload.Path != finalSemanticEvidenceFilename || filePayload.ContentHash != prior.SemanticEvidence.ContentHash || filePayload.Size != prior.SemanticEvidence.SizeBytes || uint64(len(filePayload.Data)) != prior.SemanticEvidence.SizeBytes || !bytes.Equal(filePayload.Data, cache[prior.SemanticEvidence.URI]) {
		return stateMismatchError(err, "prior signed semantic evidence file differs from its locator")
	}
	matched := false
	for _, item := range payload.Files {
		if item.Path == finalSemanticEvidenceFilename && item.ContentHash == filePayload.ContentHash && item.Size == filePayload.Size && item.EnvelopeHash == fileEnvelope.ContentHash {
			matched = true
		}
	}
	if !matched {
		return errors.New("prior semantic_verified supplement does not enumerate the carried semantic evidence envelope")
	}
	var semantic FinalSemanticEvidence
	if err := decodeStrictJSONBytes(filePayload.Data, &semantic); err != nil || VerifyFinalSemanticEvidence(&semantic) != nil || semantic.PublicVerification == nil || semantic.Phase != "release-1.0" || semantic.RunID != prior.RunID || semantic.ResultHash != prior.ResultHash || semantic.EvidenceHash != prior.SemanticEvidenceHash || semantic.PublicVerification.TranscriptHash != prior.PublicTranscriptHash || semantic.Window != prior.AcceptanceWindow || semantic.NativeTerminalHead != prior.TerminalNativeHead || semantic.EVMTerminalHead != prior.TerminalEVMHead {
		return stateMismatchError(err, "prior signed semantic evidence does not match the production predecessor")
	}
	if semantic.FleetLifecycle == nil || completePayload.LifecycleHandoff.Schema != scenarioLifecycleHandoffSchema || completePayload.LifecycleHandoff.ReleaseRunID != prior.RunID || completePayload.LifecycleHandoff.Stage != fleetLifecycleStageReleaseHandoff || completePayload.LifecycleHandoff.File != scenarioLifecycleHandoffFilename || completePayload.LifecycleHandoff.ContentHash != semantic.FleetLifecycle.ReleaseHandoffHash || completePayload.LifecycleHandoff.SizeBytes != semantic.FleetLifecycle.ReleaseHandoffSize {
		return errors.New("prior owner completion lifecycle handoff differs from signed semantic evidence")
	}
	if err := verifyFinalFleetLifecycleContinuity(semantic.FleetLifecycle, evidence.FleetLifecycle); err != nil {
		return fmt.Errorf("prior signed fleet lifecycle handoff: %w", err)
	}
	return nil
}

func verifyFinalContractCleanupArtifacts(evidence *FinalSemanticEvidence, cache map[string][]byte) error {
	cleanup := &evidence.ContractCleanup
	var state SupervisorState
	if err := decodeStrictJSONBytes(cache[cleanup.SupervisorStateArtifact.URI], &state); err != nil {
		return fmt.Errorf("decode accepted supervisor cleanup generation: %w", err)
	}
	if state.Schema != "urnetwork-sim-supervisor-state-v1" || state.SupervisorPID <= 1 || state.SupervisorStartTimeTicks != cleanup.SupervisorStartTimeTicks || state.ManifestHash != cleanup.SupervisorManifestHash || state.ContractCleanupCutoff != cleanup.Cutoff {
		return errors.New("accepted supervisor generation does not match contract cleanup evidence")
	}
	processByID := make(map[string]ProcessState, len(state.Processes))
	expectedRestarts, err := finalExpectedRestartCounts(evidence)
	if err != nil {
		return err
	}
	for _, process := range state.Processes {
		if process.ID == "" || processByID[process.ID].ID != "" {
			return errors.New("accepted supervisor generation has an invalid process census")
		}
		expected, ok := expectedRestarts[process.ID]
		if !ok || !process.Healthy || process.PID <= 1 || process.ExitError != "" || uint64(process.Restarts) != expected {
			return fmt.Errorf("accepted supervisor process %s health/restarts differ from the fault-attributed census", process.ID)
		}
		processByID[process.ID] = process
	}
	if len(processByID) != len(expectedRestarts) {
		return errors.New("accepted supervisor and restart censuses have different process sets")
	}
	for _, operator := range cleanup.Operators {
		var result serverContractCleanupResult
		if err := decodeStrictJSONBytes(cache[operator.ResultArtifact.URI], &result); err != nil {
			return fmt.Errorf("decode operator %d contract cleanup result: %w", operator.NoID, err)
		}
		if result.Schema != "urnetwork-sim-server-contract-cleanup-v1" || result.Cutoff != cleanup.Cutoff || result.Passes != operator.Passes || result.Closed != operator.Closed || result.Converged != operator.Converged {
			return fmt.Errorf("operator %d contract cleanup result does not match accepted generation", operator.NoID)
		}
		process, ok := processByID[operator.TaskworkerID]
		if !ok || process.Role != "operator-taskworker" {
			return fmt.Errorf("operator %d cleanup taskworker is absent or unhealthy in accepted generation", operator.NoID)
		}
	}
	return nil
}

func verifyFinalTopologyArtifacts(evidence *FinalSemanticEvidence, minerData, bindingData []byte) error {
	var miners []FinalMinerProcessEvidence
	if err := decodeStrictJSONBytes(minerData, &miners); err != nil {
		return fmt.Errorf("decode miner process manifest: %w", err)
	}
	if len(miners) != 1000 {
		return fmt.Errorf("miner process manifest entries=%d, want 1000", len(miners))
	}
	minerByID := make(map[uint64]FinalMinerProcessEvidence, len(miners))
	processCounts, processGenerations, clients, providers := map[string]int{}, map[string]uint64{}, map[string]bool{}, map[string]bool{}
	for i, miner := range miners {
		if miner.MinerID != uint64(i+1) || miner.ProcessID == "" || miner.ProcessGeneration == 0 || miner.ClientID == "" || miner.ProviderID == "" || !miner.Running || clients[miner.ClientID] || providers[miner.ProviderID] {
			return fmt.Errorf("miner process manifest entry %d is incomplete, duplicated, stopped, or non-canonical", i+1)
		}
		if err := requireFinalHex32("miner SDK source hash", miner.SDKSourceHash); err != nil {
			return err
		}
		processCounts[miner.ProcessID]++
		if generation := processGenerations[miner.ProcessID]; generation != 0 && generation != miner.ProcessGeneration {
			return fmt.Errorf("miner swarm process %s spans generation %d and %d", miner.ProcessID, generation, miner.ProcessGeneration)
		}
		processGenerations[miner.ProcessID] = miner.ProcessGeneration
		clients[miner.ClientID], providers[miner.ProviderID] = true, true
		minerByID[miner.MinerID] = miner
	}
	if len(processCounts) != evidence.Topology.MinerSwarmProcesses {
		return fmt.Errorf("miner SDK instances are hosted by %d swarm processes, want %d", len(processCounts), evidence.Topology.MinerSwarmProcesses)
	}
	for processID, count := range processCounts {
		if count != evidence.ExpectedMiners/evidence.Topology.MinerSwarmProcesses {
			return fmt.Errorf("miner swarm process %s hosts %d SDK instances, want %d", processID, count, evidence.ExpectedMiners/evidence.Topology.MinerSwarmProcesses)
		}
	}
	var bindings []FinalFleetMemberBindingEvidence
	if err := decodeStrictJSONBytes(bindingData, &bindings); err != nil {
		return fmt.Errorf("decode fleet binding manifest: %w", err)
	}
	if len(bindings) != 1000 {
		return fmt.Errorf("fleet member binding entries=%d, want 1000", len(bindings))
	}
	fleetMembers, assignedMiners := map[uint64]uint64{}, map[uint64]bool{}
	headMembers, poolTailMembers := 0, 0
	poolIDs := map[uint64]bool{}
	for _, pool := range evidence.Pools {
		poolIDs[pool.NoID] = true
	}
	for i, binding := range bindings {
		miner := minerByID[binding.MinerID]
		if binding.MinerID != uint64(i+1) || !poolIDs[binding.NoID] || assignedMiners[binding.MinerID] || binding.ClientID != miner.ClientID || binding.ProviderID != miner.ProviderID {
			return fmt.Errorf("miner tier assignment entry %d is incomplete, duplicated, or inconsistent", i+1)
		}
		switch binding.Tier {
		case "head-candidate":
			if binding.FleetID == 0 || binding.FleetID > finalHeadCandidateCount || binding.HeadUID != evidence.HeadFleets[binding.FleetID-1].UID || binding.Generation == 0 || !binding.BindingActive {
				return fmt.Errorf("head-candidate assignment %d has an invalid fleet/UID", i+1)
			}
			for _, validator := range evidence.Validators {
				for _, cycle := range validator.Cycles {
					cycleUID, err := finalSemanticRewardUIDAt(evidence, binding.FleetID, cycle.SettlementEpoch, binding.HeadUID)
					if err != nil {
						return err
					}
					found := false
					for _, candidate := range cycle.Candidates {
						if candidate.FleetID == binding.FleetID && candidate.UID == cycleUID {
							found = true
							break
						}
					}
					if !found {
						return fmt.Errorf("fleet %d lifecycle UID %d is absent from validator %d epoch %d candidate census", binding.FleetID, cycleUID, validator.ValidatorID, cycle.SettlementEpoch)
					}
				}
			}
			fleetMembers[binding.FleetID]++
			headMembers++
		case "pool-tail":
			if binding.FleetID != 0 || binding.HeadUID != 0 || binding.Generation != 0 || binding.BindingActive {
				return fmt.Errorf("pool-tail assignment %d falsely claims a head fleet", i+1)
			}
			poolTailMembers++
		default:
			return fmt.Errorf("miner tier assignment %d has unsupported tier %q", i+1, binding.Tier)
		}
		assignedMiners[binding.MinerID] = true
	}
	if len(assignedMiners) != evidence.ExpectedMiners || headMembers != finalHeadCandidateCount*4 || poolTailMembers != evidence.ExpectedMiners-finalHeadCandidateCount*4 || len(fleetMembers) != finalHeadCandidateCount {
		return fmt.Errorf("miner tier assignments cover total/head/tail/fleets=%d/%d/%d/%d, want 1000/808/192/202", len(assignedMiners), headMembers, poolTailMembers, len(fleetMembers))
	}
	for fleetID := uint64(1); fleetID <= finalHeadCandidateCount; fleetID++ {
		if fleetMembers[fleetID] != 4 {
			return fmt.Errorf("head fleet %d member assignments=%d, want 4", fleetID, fleetMembers[fleetID])
		}
	}
	return nil
}

type finalHeadFleetManifestIdentity struct {
	FleetKey       string
	Hotkey         string
	CommitmentHash string
	UID            uint16
	Generation     uint64
	Members        map[string]string
}

func decodeFinalHeadFleetManifestIdentity(evidence *FinalSemanticEvidence, fleet *FinalHeadFleetEvidence, data []byte) (finalHeadFleetManifestIdentity, error) {
	if evidence == nil || fleet == nil {
		return finalHeadFleetManifestIdentity{}, errors.New("head fleet manifest identity is unavailable")
	}
	var artifact struct {
		Manifest json.RawMessage `json:"manifest"`
		UID      uint16          `json:"uid"`
		Snapshot ChainHead       `json:"snapshot"`
	}
	if err := decodeStrictJSONBytes(data, &artifact); err != nil {
		return finalHeadFleetManifestIdentity{}, fmt.Errorf("decode head fleet %d binding artifact: %w", fleet.FleetID, err)
	}
	manifest, err := protocol.ParseFleetManifest(artifact.Manifest)
	if err != nil {
		return finalHeadFleetManifestIdentity{}, fmt.Errorf("head fleet %d canonical manifest: %w", fleet.FleetID, err)
	}
	hotkey, prefix, err := ss58.Decode(fleet.Hotkey)
	if err != nil || prefix != ss58.BittensorPrefix {
		return finalHeadFleetManifestIdentity{}, stateMismatchError(err, "head fleet %d hotkey is not canonical Bittensor SS58", fleet.FleetID)
	}
	coordinator := common.HexToAddress(evidence.Deployment.CoordinatorProxy)
	if artifact.UID != fleet.UID || artifact.Snapshot != fleet.Snapshot || manifest.ChainID != evidence.ChainID || manifest.Netuid != evidence.Netuid || !bytes.Equal(manifest.Coordinator[:], coordinator.Bytes()) || manifest.Hotkey != hotkey || manifest.Generation != fleet.Generation || len(manifest.Members) != fleet.MemberCount {
		return finalHeadFleetManifestIdentity{}, fmt.Errorf("head fleet %d binding artifact differs from its signed identity/checkpoint", fleet.FleetID)
	}
	commitment, err := manifest.CommitmentHash()
	if err != nil {
		return finalHeadFleetManifestIdentity{}, err
	}
	identity := finalHeadFleetManifestIdentity{
		FleetKey: "0x" + hex.EncodeToString(manifest.FleetID[:]), Hotkey: "0x" + hex.EncodeToString(manifest.Hotkey[:]),
		CommitmentHash: "0x" + hex.EncodeToString(commitment[:]), UID: artifact.UID, Generation: manifest.Generation,
		Members: make(map[string]string, len(manifest.Members)),
	}
	for _, member := range manifest.Members {
		client := hex.EncodeToString(member.ClientID[:])
		if _, duplicate := identity.Members[client]; duplicate {
			return finalHeadFleetManifestIdentity{}, fmt.Errorf("head fleet %d manifest repeats a client", fleet.FleetID)
		}
		identity.Members[client] = "0x" + hex.EncodeToString(member.ClientKey[:])
	}
	return identity, nil
}

func verifyFinalHeadFleetBindingArtifacts(evidence *FinalSemanticEvidence, cache map[string][]byte, bindingData []byte) error {
	var bindings []FinalFleetMemberBindingEvidence
	if err := decodeStrictJSONBytes(bindingData, &bindings); err != nil {
		return fmt.Errorf("decode fleet binding manifest for head artifacts: %w", err)
	}
	clientsByFleet := make(map[uint64]map[string]bool, len(evidence.HeadFleets))
	for _, binding := range bindings {
		if binding.Tier != "head-candidate" {
			continue
		}
		key, err := finalSemanticClientKey(binding.ClientID)
		if err != nil {
			return err
		}
		if clientsByFleet[binding.FleetID] == nil {
			clientsByFleet[binding.FleetID] = map[string]bool{}
		}
		clientsByFleet[binding.FleetID][key] = true
	}
	seenFleetKeys := map[string]bool{}
	seenClientKeys := map[string]bool{}
	for index := range evidence.HeadFleets {
		fleet := &evidence.HeadFleets[index]
		identity, err := decodeFinalHeadFleetManifestIdentity(evidence, fleet, cache[fleet.BindingArtifact.URI])
		if err != nil {
			return err
		}
		if seenFleetKeys[identity.FleetKey] {
			return fmt.Errorf("head fleet %d reuses a canonical fleet identity", fleet.FleetID)
		}
		seenFleetKeys[identity.FleetKey] = true
		wantClients := clientsByFleet[fleet.FleetID]
		if len(wantClients) != len(identity.Members) {
			return fmt.Errorf("head fleet %d binding artifact member census differs", fleet.FleetID)
		}
		clients := make([]string, 0, len(identity.Members))
		for client := range identity.Members {
			clients = append(clients, client)
		}
		sort.Strings(clients)
		for _, client := range clients {
			clientKey := identity.Members[client]
			if !wantClients[client] {
				return fmt.Errorf("head fleet %d binding artifact contains an unauthenticated client", fleet.FleetID)
			}
			if seenClientKeys[clientKey] {
				return fmt.Errorf("head fleet %d binding artifact reuses a client verification key", fleet.FleetID)
			}
			seenClientKeys[clientKey] = true
		}
	}
	return nil
}

func finalObservedString(observed map[string]any, name string) (string, bool) {
	value, ok := observed[name]
	if !ok {
		return "", false
	}
	result, ok := value.(string)
	return result, ok && result != ""
}

func verifyFinalSignedCycleFleetIdentity(identity finalHeadFleetManifestIdentity, owners map[string]uint64, measurement *validatorpkg.ReleaseMeasurementArtifact, validatorID, epoch uint64) error {
	if measurement == nil || measurement.ValidatorID != validatorID || measurement.SettlementEpoch != epoch {
		return errors.New("signed measurement identity is unavailable for head fleet replay")
	}
	seen := map[string]bool{}
	for _, binding := range measurement.Bindings {
		client, err := finalSemanticClientKey(binding.ClientID)
		if err != nil {
			return err
		}
		clientKey, expectedMember := identity.Members[client]
		sameFleet := binding.FleetID == identity.FleetKey
		if !expectedMember && !sameFleet {
			continue
		}
		owner, ownerOK := owners[client]
		if !expectedMember || !sameFleet || !ownerOK || seen[client] || !binding.Active || binding.NoID != owner || binding.Hotkey != identity.Hotkey || binding.ClientKey != clientKey || binding.LocalClientKey != clientKey || binding.CommitmentHash != identity.CommitmentHash || binding.Generation != identity.Generation || !binding.LiveUIDFound || binding.RecordUID != identity.UID || binding.LiveUID != identity.UID {
			return fmt.Errorf("validator %d epoch %d signed binding differs from challenger fleet manifest identity", validatorID, epoch)
		}
		seen[client] = true
	}
	if len(seen) != len(identity.Members) {
		return fmt.Errorf("validator %d epoch %d signed challenger member census=%d, want %d", validatorID, epoch, len(seen), len(identity.Members))
	}
	return nil
}

func verifyFinalHeadTournamentTransitionArtifacts(evidence *FinalSemanticEvidence, cache map[string][]byte) error {
	return verifyFinalHeadTournamentTransitionArtifactsWithMeasurements(evidence, cache, nil)
}

func verifyFinalHeadTournamentTransitionArtifactsWithMeasurements(evidence *FinalSemanticEvidence, cache map[string][]byte, measurements map[string]*validatorpkg.ReleaseMeasurementArtifact) error {
	plan, err := verifyFinalSetupPlanArtifact(evidence, cache[evidence.PlanArtifact.URI])
	if err != nil {
		return err
	}
	var topologyBindings []FinalFleetMemberBindingEvidence
	if err := decodeStrictJSONBytes(cache[evidence.Topology.BindingManifest.URI], &topologyBindings); err != nil {
		return fmt.Errorf("decode fleet binding manifest for tournament replay: %w", err)
	}
	ownersByFleet := make(map[uint64]map[string]uint64, len(evidence.HeadFleets))
	for _, binding := range topologyBindings {
		if binding.Tier != "head-candidate" {
			continue
		}
		client, err := finalSemanticClientKey(binding.ClientID)
		if err != nil {
			return err
		}
		if ownersByFleet[binding.FleetID] == nil {
			ownersByFleet[binding.FleetID] = map[string]uint64{}
		}
		if ownersByFleet[binding.FleetID][client] != 0 {
			return fmt.Errorf("head fleet %d repeats a tournament client owner", binding.FleetID)
		}
		ownersByFleet[binding.FleetID][client] = binding.NoID
	}
	if measurements == nil {
		measurements = map[string]*validatorpkg.ReleaseMeasurementArtifact{}
	}
	for index := range evidence.HeadTransitions {
		transition := &evidence.HeadTransitions[index]
		if transition.ChallengerFleetID == 0 || transition.ChallengerFleetID > uint64(len(evidence.HeadFleets)) {
			return fmt.Errorf("head tournament transition %d challenger fleet is outside the authenticated fleet graph", index+1)
		}
		fleet := &evidence.HeadFleets[transition.ChallengerFleetID-1]
		identity, err := decodeFinalHeadFleetManifestIdentity(evidence, fleet, cache[fleet.BindingArtifact.URI])
		if err != nil {
			return err
		}
		var encodedArtifact struct {
			Postcondition json.RawMessage             `json:"postcondition"`
			Pruned        finalHeadTournamentIdentity `json:"pruned_identity"`
		}
		if err := decodeStrictJSONBytes(cache[transition.Artifact.URI], &encodedArtifact); err != nil {
			return fmt.Errorf("decode head tournament transition %d artifact: %w", index+1, err)
		}
		postcondition, err := decodeFinalActionPostconditionV4(encodedArtifact.Postcondition)
		if err != nil {
			return fmt.Errorf("head tournament transition %d artifact lacks its v4 postcondition: %w", index+1, err)
		}
		artifact := finalHeadTournamentTransitionArtifact{Postcondition: postcondition, Pruned: encodedArtifact.Pruned}
		proof, proofErr := decodeFinalActionPostconditionV4(cache[transition.Registration.Proof.URI])
		if proofErr != nil || !finalJSONEqual(proof, artifact.Postcondition) {
			return stateMismatchError(proofErr, "head tournament transition %d registration proof and artifact contain different v4 postconditions", index+1)
		}
		if postcondition.DeploymentID != evidence.DeploymentID || postcondition.ActionID != fmt.Sprintf("fleet.register.%d", transition.ChallengerFleetID) || postcondition.SubstrateFinalized != transition.Snapshot || postcondition.IndependentSubstrateFinalized != transition.IndependentSnapshot || postcondition.EVMFinalized != transition.EVMSnapshot || postcondition.IndependentEVMFinalized != transition.IndependentEVMSnapshot || postcondition.OperationalRPCMode != transition.OperationalRPCMode || postcondition.IndependentRPC != transition.IndependentRPC {
			return fmt.Errorf("head tournament transition %d declaration differs from its exact v4 checkpoints", index+1)
		}
		if err := verifyFinalSetupPlanActionReceipt(plan, postcondition); err != nil {
			return fmt.Errorf("head tournament transition %d setup lineage: %w", index+1, err)
		}
		if !finalJSONEqual(postcondition.Observed, postcondition.IndependentObserved) {
			return fmt.Errorf("head tournament transition %d RPC observations disagree", index+1)
		}
		uid, uidOK := finalSemanticObservedUint(postcondition.Observed, "uid")
		replacedUID, replacedUIDOK := finalSemanticObservedUint(postcondition.Observed, "replaced_uid")
		replacedChurn, replacedChurnOK := finalSemanticObservedUint(postcondition.Observed, "replaced_churn")
		uidCount, uidCountOK := finalSemanticObservedUint(postcondition.Observed, "uid_count")
		role, roleOK := finalObservedString(postcondition.Observed, "role")
		hotkeyHex, hotkeyOK := finalObservedString(postcondition.Observed, "hotkey")
		coldkeyHex, coldkeyOK := finalObservedString(postcondition.Observed, "coldkey")
		promotedHotkey, promotedColdkey, identityErr := finalSemanticSS58Pair(fmt.Sprintf("challenger fleet %d", transition.ChallengerFleetID), transition.PromotedHotkey, evidence.HeadFleets[transition.ChallengerFleetID-1].Coldkey)
		if !uidOK || !replacedUIDOK || !replacedChurnOK || !uidCountOK || !roleOK || !hotkeyOK || !coldkeyOK || identityErr != nil || uid != uint64(transition.PromotedUID) || replacedUID != uint64(transition.PrunedUID) || replacedChurn != transition.PrunedChurn || uidCount <= uid || role != fmt.Sprintf("fleet-%d-hotkey", transition.ChallengerFleetID) || hotkeyHex != "0x"+hex.EncodeToString(promotedHotkey[:]) || coldkeyHex != "0x"+hex.EncodeToString(promotedColdkey[:]) {
			return stateMismatchError(identityErr, "head tournament transition %d observed registration identity differs", index+1)
		}
		prunedKey, keyErr := decodeHex32("pruned tournament hotkey", artifact.Pruned.PublicKey)
		prunedSS58, prunedPrefix, ss58Err := ss58.Decode(artifact.Pruned.SS58)
		indexedPruned, indexedErr := finalFleetLifecycleRole(evidence.FleetLifecycle, artifact.Pruned.Role)
		if keyErr != nil || ss58Err != nil || indexedErr != nil || prunedPrefix != ss58.BittensorPrefix || prunedKey != prunedSS58 || artifact.Pruned.Role != fmt.Sprintf("churn-%d-hotkey", transition.PrunedChurn) || artifact.Pruned.PublicKey != indexedPruned.PublicKey || artifact.Pruned.SS58 != indexedPruned.SS58 || artifact.Pruned.SS58 != transition.PrunedHotkey {
			return stateMismatchError(errors.Join(keyErr, ss58Err, indexedErr), "head tournament transition %d pruned identity differs from its exact authenticated role", index+1)
		}
		for _, validator := range evidence.Validators {
			for _, cycle := range validator.Cycles {
				matches := 0
				for _, candidate := range cycle.Candidates {
					if candidate.FleetID == transition.ChallengerFleetID {
						matches++
						if candidate.UID != transition.PromotedUID {
							return fmt.Errorf("validator %d epoch %d signed cycle maps challenger fleet %d to UID %d, want %d", validator.ValidatorID, cycle.SettlementEpoch, transition.ChallengerFleetID, candidate.UID, transition.PromotedUID)
						}
					}
				}
				if matches != 1 {
					return fmt.Errorf("validator %d epoch %d signed cycle contains %d identities for challenger fleet %d", validator.ValidatorID, cycle.SettlementEpoch, matches, transition.ChallengerFleetID)
				}
				measurement := measurements[cycle.MeasurementArtifact.URI]
				if measurement == nil {
					var decodeErr error
					measurement, _, decodeErr = validatorpkg.DecodeReleaseMeasurementArtifact(cache[cycle.MeasurementArtifact.URI])
					if decodeErr != nil {
						return fmt.Errorf("decode validator %d epoch %d signed measurement for tournament replay: %w", validator.ValidatorID, cycle.SettlementEpoch, decodeErr)
					}
					measurements[cycle.MeasurementArtifact.URI] = measurement
				}
				if err := verifyFinalSignedCycleFleetIdentity(identity, ownersByFleet[transition.ChallengerFleetID], measurement, validator.ValidatorID, cycle.SettlementEpoch); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func verifyFinalValidatorViewTransitionArtifact(evidence *FinalSemanticEvidence, data []byte) error {
	derived, err := deriveFinalValidatorViewTransition(evidence)
	if err != nil {
		return err
	}
	declared := finalValidatorViewTransitionArtifact{
		FaultEpoch: evidence.ValidatorView.FaultEpoch, RestoredEpoch: evidence.ValidatorView.RestoredEpoch,
		AffectedValidatorID: evidence.ValidatorView.AffectedValidatorID, ControlValidatorID: evidence.ValidatorView.ControlValidatorID,
		WithheldFleetID: evidence.ValidatorView.WithheldFleetID, ReplacementFleetID: evidence.ValidatorView.ReplacementFleetID,
	}
	if declared != derived {
		return errors.New("validator-local view declaration differs from independently rederived signed cycles")
	}
	var artifact finalValidatorViewTransitionArtifact
	if err := decodeStrictJSONBytes(data, &artifact); err != nil {
		return fmt.Errorf("decode validator-local view transition artifact: %w", err)
	}
	if artifact != derived {
		return errors.New("validator-local view artifact differs from independently rederived signed cycles")
	}
	return nil
}

func verifyFinalIntentArtifact(evidence *FinalSemanticEvidence, validatorID uint64, cycle *FinalCRv4Cycle, data []byte) error {
	var intent validatorpkg.SteeringIntent
	if err := decodeStrictJSONBytes(data, &intent); err != nil {
		return err
	}
	if intent.Schema != validatorpkg.SteeringIntentSchema || intent.Prepared == nil {
		return errors.New("intent schema or exact prepared submission is missing")
	}
	if _, err := intent.Prepared.Validate(); err != nil {
		return fmt.Errorf("prepared CRv4 submission: %w", err)
	}
	if err := intent.VerifyVectorHash(); err != nil {
		return err
	}
	if intent.ValidatorID != validatorID || intent.Netuid != evidence.Netuid || intent.SettlementEpoch != cycle.SettlementEpoch || intent.SubnetEpoch != cycle.SubnetEpoch || intent.PolicyHash != evidence.PolicyHash || intent.VectorHash != cycle.IntentVectorHash {
		return errors.New("intent identity or lineage does not match semantic evidence")
	}
	if intent.NativeSnapshotBlock != cycle.NativeSnapshot.Number || intent.NativeSnapshotHash != cycle.NativeSnapshot.Hash || intent.EVMSnapshotBlock != cycle.EVMSnapshot.Number || intent.EVMSnapshotHash != cycle.EVMSnapshot.Hash {
		return errors.New("intent checkpoints do not match semantic evidence")
	}
	selected, rejected := finalCandidateUIDs(cycle.Candidates)
	if !slices.Equal(intent.SelectedHeadUIDs, selected) || !slices.Equal(intent.RejectedHeadUIDs, rejected) || !slices.Equal(intent.MaskedUIDs, cycle.MaskedUIDs) {
		return errors.New("intent selection or mask does not match semantic evidence")
	}
	eligibleUIDs := make([]uint16, len(cycle.Candidates))
	eligibleScores := make([]validatorpkg.RationalJSON, len(cycle.Candidates))
	for i, candidate := range cycle.Candidates {
		eligibleUIDs[i] = candidate.UID
		eligibleScores[i] = validatorpkg.RationalJSON{Numerator: candidate.RawScore.Numerator, Denominator: candidate.RawScore.Denominator}
	}
	if !slices.Equal(intent.EligibleHeadUIDs, eligibleUIDs) || !slices.Equal(intent.EligibleHeadScores, eligibleScores) {
		return errors.New("intent eligible candidate evidence does not match semantic evidence")
	}
	uids := make([]uint16, len(cycle.Submitted))
	values := make([]uint16, len(cycle.Submitted))
	scores := make([]validatorpkg.RationalJSON, len(cycle.Submitted))
	for i, submitted := range cycle.Submitted {
		uids[i], values[i] = submitted.UID, submitted.Value
		scores[i] = validatorpkg.RationalJSON{Numerator: submitted.Score.Numerator, Denominator: submitted.Score.Denominator}
	}
	if !slices.Equal(intent.UIDs, uids) || !slices.Equal(intent.Scores, scores) || !slices.Equal(intent.Values, values) || intent.Status != "applied" || intent.ExtrinsicHash != cycle.Commit.ExtrinsicHash || intent.FinalizedBlock != cycle.Commit.Block.Number || intent.FinalizedBlockHash != cycle.Commit.Block.Hash || intent.RevealBlock != cycle.Reveal.Block.Number || intent.ApplicationBlock != cycle.Application.Block.Number || intent.ApplicationBlockHash != cycle.Application.Block.Hash {
		return errors.New("intent vector or CRv4 receipts do not match semantic evidence")
	}
	if intent.Prepared.Netuid != evidence.Netuid || intent.Prepared.SubnetEpoch != cycle.SubnetEpoch || !slices.Equal(intent.Prepared.UIDs, uids) || !slices.Equal(intent.Prepared.Values, values) || intent.Prepared.ExtrinsicHash != cycle.Commit.ExtrinsicHash || intent.Prepared.RevealBlock != cycle.Reveal.Block.Number {
		return errors.New("prepared CRv4 submission does not match semantic evidence")
	}
	audits := make([]validatorpkg.DepositAudit, len(cycle.Pools))
	for i, pool := range cycle.Pools {
		audits[i] = finalDepositAuditFromPool(cycle.SettlementEpoch, &pool)
	}
	if !slices.Equal(intent.DepositAudits, audits) {
		return errors.New("intent deposit audits do not match semantic evidence")
	}
	return nil
}

func verifyFinalIntentAndMeasurementArtifacts(evidence *FinalSemanticEvidence, validator *FinalValidatorIdentityEvidence, cycle *FinalCRv4Cycle, intentData, measurementData, envelopeData []byte, decoded **validatorpkg.ReleaseMeasurementArtifact) error {
	if validator == nil {
		return errors.New("validator identity is unavailable for measurement envelope")
	}
	if decoded == nil {
		return errors.New("decoded measurement output is unavailable")
	}
	*decoded = nil
	validatorID := validator.ValidatorID
	if err := verifyFinalIntentArtifact(evidence, validatorID, cycle, intentData); err != nil {
		return err
	}
	var intent validatorpkg.SteeringIntent
	if err := decodeStrictJSONBytes(intentData, &intent); err != nil {
		return err
	}
	if validatorpkg.ReleaseMeasurementContentHash(measurementData) != intent.MeasurementArtifactHash || uint64(len(measurementData)) != intent.MeasurementArtifactSize || cycle.MeasurementArtifact.ContentHash != intent.MeasurementArtifactHash || cycle.MeasurementArtifact.SizeBytes != intent.MeasurementArtifactSize {
		return errors.New("measurement content address does not match steering intent")
	}
	if validatorpkg.ReleaseMeasurementEnvelopeContentHash(envelopeData) != intent.MeasurementEnvelopeHash || uint64(len(envelopeData)) != intent.MeasurementEnvelopeSize || cycle.MeasurementEnvelope.ContentHash != intent.MeasurementEnvelopeHash || cycle.MeasurementEnvelope.SizeBytes != intent.MeasurementEnvelopeSize {
		return errors.New("measurement envelope content address does not match steering intent")
	}
	envelope, err := validatorpkg.DecodeReleaseMeasurementEnvelope(envelopeData)
	if err != nil {
		return fmt.Errorf("decode validator-signed measurement envelope: %w", err)
	}
	hotkeyBytes, err := hex.DecodeString(strings.TrimPrefix(envelope.ValidatorHotkey, "0x"))
	if err != nil || len(hotkeyBytes) != 32 || finalAccountMatches(validator.Hotkey, hotkeyBytes) != nil {
		return errors.New("measurement envelope signer is not the pinned validator hotkey")
	}
	var hotkey [32]byte
	copy(hotkey[:], hotkeyBytes)
	if intent.Prepared.HotkeyHex != envelope.ValidatorHotkey {
		return errors.New("prepared submission hotkey differs from measurement envelope signer")
	}
	artifact, verified, err := validatorpkg.VerifyReleaseMeasurementEnvelope(envelope, measurementData, hotkey, validator.UID, intent.Prepared.ExtrinsicHash)
	if err != nil {
		return fmt.Errorf("validator-signed measurement envelope: %w", err)
	}
	signedAt, err := time.Parse(time.RFC3339Nano, envelope.SignedAt)
	if err != nil {
		return errors.New("measurement envelope signing time is invalid")
	}
	startedAt, _ := time.Parse(time.RFC3339Nano, evidence.CampaignStartedAt)
	completedAt, _ := time.Parse(time.RFC3339Nano, evidence.CampaignCompletedAt)
	if signedAt.Before(startedAt) || signedAt.After(completedAt) {
		return errors.New("measurement envelope signing time is outside the campaign")
	}
	if err := validatorpkg.VerifyReleaseMeasurementIntent(&intent, artifact, verified); err != nil {
		return err
	}
	if artifact.DeploymentID != evidence.DeploymentID || artifact.ChainID != evidence.ChainID || !strings.EqualFold(artifact.GenesisHash, evidence.GenesisHash) || !strings.EqualFold(artifact.Coordinator, evidence.Deployment.CoordinatorProxy) || !strings.EqualFold(artifact.SettlementVault, evidence.Deployment.SettlementVault) || artifact.ValidatorID != validatorID || artifact.Netuid != evidence.Netuid || artifact.SettlementEpoch != cycle.SettlementEpoch || artifact.SubnetEpoch != cycle.SubnetEpoch || !strings.EqualFold(artifact.PolicyHash, evidence.PolicyHash) {
		return errors.New("measurement deployment or epoch identity differs from semantic evidence")
	}
	if artifact.NativeSnapshotBlock != cycle.NativeSnapshot.Number || !strings.EqualFold(artifact.NativeSnapshotHash, cycle.NativeSnapshot.Hash) || artifact.EVMSnapshotBlock != cycle.EVMSnapshot.Number || !strings.EqualFold(artifact.EVMSnapshotHash, cycle.EVMSnapshot.Hash) || artifact.SelfUID != finalValidatorUID(evidence, validatorID) {
		return errors.New("measurement snapshot or validator identity differs from semantic evidence")
	}
	if len(verified.EligibleHead) != len(cycle.Candidates) || len(verified.Pools) != len(cycle.Pools) || !slices.Equal(verified.MaskedUIDs, cycle.MaskedUIDs) || len(verified.UIDs) != len(cycle.Submitted) || len(verified.Scores) != len(cycle.Submitted) {
		return errors.New("measurement-derived decision coverage differs from semantic evidence")
	}
	selected := make(map[uint16]bool, len(verified.SelectedHead))
	for _, head := range verified.SelectedHead {
		selected[head.UID] = true
	}
	for index, head := range verified.EligibleHead {
		candidate := cycle.Candidates[index]
		if candidate.UID != head.UID || candidate.RawScore != finalRationalFromBig(head.Score) || candidate.Selected != selected[head.UID] {
			return fmt.Errorf("measurement-derived head candidate %d differs", index)
		}
	}
	for index, pool := range verified.Pools {
		declared := cycle.Pools[index]
		if declared.NoID != pool.NoID || declared.UID != pool.UID || declared.QualityPPM != pool.QualityPPM || declared.RawScore != finalRationalFromBig(pool.Score) || finalDepositAuditFromPool(cycle.SettlementEpoch, &declared) != pool.Audit {
			return fmt.Errorf("measurement-derived pool decision no_id %d differs", pool.NoID)
		}
	}
	for index, uid := range verified.UIDs {
		if cycle.Submitted[index].UID != uid || cycle.Submitted[index].Score != finalRationalFromBig(verified.Scores[index]) {
			return fmt.Errorf("measurement-derived submitted weight %d differs", index)
		}
	}
	*decoded = artifact
	return nil
}

func finalDepositAuditFromPool(settlementEpoch uint64, pool *FinalPoolWeightEvidence) validatorpkg.DepositAudit {
	if pool == nil {
		return validatorpkg.DepositAudit{}
	}
	return validatorpkg.DepositAudit{
		NoID: pool.NoID, Epoch: settlementEpoch, SourceEpoch: pool.SourceEpoch,
		ArtifactHash: pool.ArtifactContentHash, CommittedArtifactHash: pool.ArtifactHash, PayoutRoot: pool.PayoutRoot,
		ArtifactSigner: pool.ArtifactSigner, RootCommitter: pool.RootCommitter, RootSigner: pool.RootSigner,
		SourceStartBlock: pool.SourceStartBlock, SourceStartHash: pool.SourceStartHash,
		SourceEndBlock: pool.SourceEndBlock, SourceEndHash: pool.SourceEndHash,
		RootCommitBlock: pool.RootCommitBlock, ObservedAtBlock: pool.ObservedAtBlock, ArtifactDeadlineBlock: pool.ArtifactDeadlineBlock,
		UsageBytes: pool.UsageBytes, ConvictionBeforeRao: pool.ConvictionBeforeRao,
		RateNumeratorRaoPerGiB: pool.RateNumeratorRaoPerGiB, RateDenominator: pool.RateDenominator,
		RequiredDepositRao: pool.RequiredDepositRao, ObservedDepositRao: pool.ObservedDepositRao,
		Status: pool.AuditStatus, Compliant: pool.AuditCompliant, Disposition: pool.AuditDisposition, Error: pool.AuditError,
	}
}

func finalValidatorUID(evidence *FinalSemanticEvidence, validatorID uint64) uint16 {
	if validator := finalValidatorByID(evidence, validatorID); validator != nil {
		return validator.UID
	}
	return 0
}

func finalValidatorByID(evidence *FinalSemanticEvidence, id uint64) *FinalValidatorIdentityEvidence {
	for i := range evidence.Validators {
		if evidence.Validators[i].ValidatorID == id {
			return &evidence.Validators[i]
		}
	}
	return nil
}

func finalPoolByNO(evidence *FinalSemanticEvidence, noID uint64) *FinalPoolUIDEvidence {
	for i := range evidence.Pools {
		if evidence.Pools[i].NoID == noID {
			return &evidence.Pools[i]
		}
	}
	return nil
}

func verifyFinalPathProofArtifactBound(proof *FinalValidatorPathProofEvidence, data []byte, validator *FinalValidatorIdentityEvidence, pool *FinalPoolUIDEvidence, seenPathIDs, seenTrailIDs map[string]bool) error {
	if proof == nil || validator == nil || pool == nil {
		return errors.New("path proof identity is unavailable")
	}
	vpk, err := finalEd25519PublicKey("validator path VPK", validator.PathVPK)
	if err != nil {
		return err
	}
	serverKeys := make(map[byte]ed25519.PublicKey, len(pool.ServerKeyHistory))
	for _, key := range pool.ServerKeyHistory {
		decoded, err := finalEd25519PublicKey("operator server key", key.PublicKey)
		if err != nil {
			return err
		}
		serverKeys[byte(key.KeyID)] = decoded
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	count := uint64(0)
	epochs := map[uint64]bool{}
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		var envelope FinalCollectedProofRecord
		if err := decoder.Decode(&envelope); err != nil {
			return fmt.Errorf("line %d is not a canonical proof record: %w", count+1, err)
		}
		if envelope.Schema != finalCollectedProofRecordSchema {
			return fmt.Errorf("line %d proof record schema is invalid", count+1)
		}
		record := envelope.Record
		var trailing any
		if err := decoder.Decode(&trailing); err == nil {
			return fmt.Errorf("line %d contains trailing JSON", count+1)
		}
		if record.Epoch < proof.FirstEpoch || record.Epoch > proof.LastEpoch {
			return fmt.Errorf("line %d epoch %d is outside [%d,%d]", count+1, record.Epoch, proof.FirstEpoch, proof.LastEpoch)
		}
		if err := validatorpkg.VerifyProofRecord(&record, vpk, serverKeys, proof.TrailDepth); err != nil {
			return fmt.Errorf("line %d: %w", count+1, err)
		}
		pathID, trailID := hex.EncodeToString(record.PathId), record.TrailId.String()
		if seenPathIDs[pathID] || seenTrailIDs[trailID] {
			return fmt.Errorf("line %d duplicates a path or trail identity", count+1)
		}
		seenPathIDs[pathID], seenTrailIDs[trailID], epochs[record.Epoch] = true, true, true
		count++
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if count != proof.ProofCount {
		return fmt.Errorf("JSONL proof count=%d, declared %d", count, proof.ProofCount)
	}
	for epoch := proof.FirstEpoch; epoch <= proof.LastEpoch; epoch++ {
		if !epochs[epoch] {
			return fmt.Errorf("path proof epoch %d is uncovered", epoch)
		}
	}
	return nil
}

// verifyFinalPathProofArtifact is retained as the narrow count/shape helper
// used by legacy decoder tests. Release verification always calls the bound,
// cryptographic variant above.
func verifyFinalPathProofArtifact(proof *FinalValidatorPathProofEvidence, data []byte) error {
	if proof == nil {
		return errors.New("path proof evidence is nil")
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	count := uint64(0)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(line, &object); err != nil || len(object) == 0 {
			return fmt.Errorf("line %d is not a JSON object", count+1)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if count != proof.ProofCount {
		return fmt.Errorf("JSONL proof count=%d, declared %d", count, proof.ProofCount)
	}
	return nil
}

type finalPayoutArtifactExpectation struct {
	NoID         uint64
	Epoch        uint64
	UsageBytes   uint64
	PayoutRoot   string
	ArtifactHash string
	Claims       []FinalClaimEvidence
}

type finalMinerAssignment struct {
	NoID uint64
	Tier string
}

func finalPayoutClientID(value string) (connect.Id, error) {
	if id, err := connect.ParseId(value); err == nil && id != (connect.Id{}) {
		return id, nil
	}
	raw, ok := evidenceFixedHex(strings.ToLower(value), 16)
	if !ok {
		return connect.Id{}, fmt.Errorf("invalid payout client id %q", value)
	}
	var id connect.Id
	copy(id[:], raw)
	if id == (connect.Id{}) {
		return connect.Id{}, errors.New("payout client id is zero")
	}
	return id, nil
}

func finalLifecyclePayoutVariant(disposition string) (string, string, error) {
	switch disposition {
	case "pruned-provider-returned-to-operator-pool":
		return fleetLifecycleVariantTargetTakeover, "pool-tail", nil
	case "fallback-provider-head-excluded":
		return fleetLifecycleVariantFallback, "head-candidate", nil
	case "reregistered-provider-head-excluded":
		return fleetLifecycleVariantProvider, "head-candidate", nil
	case "second-pruned-provider-returned-to-operator-pool":
		return fleetLifecycleVariantCompanionTakeover, "pool-tail", nil
	default:
		return "", "", fmt.Errorf("unknown fleet lifecycle payout disposition %q", disposition)
	}
}

func finalLifecycleVariantByName(lifecycle *FinalFleetLifecycleEvidence, name string) (*FinalFleetLifecycleVariantEvidence, error) {
	if lifecycle == nil {
		return nil, errors.New("fleet lifecycle evidence is unavailable")
	}
	for index := range lifecycle.Variants {
		if lifecycle.Variants[index].Name == name {
			return &lifecycle.Variants[index], nil
		}
	}
	return nil, fmt.Errorf("fleet lifecycle variant %s is absent", name)
}

func finalPayoutAssignmentsAt(evidence *FinalSemanticEvidence, expected *finalPayoutArtifactExpectation, assignments map[connect.Id]finalMinerAssignment) (map[connect.Id]finalMinerAssignment, []FleetLifecyclePayoutEvidence, error) {
	result := make(map[connect.Id]finalMinerAssignment, len(assignments))
	for id, assignment := range assignments {
		result[id] = assignment
	}
	if evidence == nil || evidence.FleetLifecycle == nil {
		return result, nil, nil
	}
	lifecycle := evidence.FleetLifecycle
	state := &lifecycle.State
	if state.TakeoverEffectiveEpoch == 0 || state.FallbackEffectiveEpoch <= state.TakeoverEffectiveEpoch || state.ProviderEffectiveEpoch <= state.FallbackEffectiveEpoch || state.TerminalEffectiveEpoch <= state.ProviderEffectiveEpoch {
		return nil, nil, errors.New("fleet lifecycle payout assignments have incomplete settlement-epoch transitions")
	}
	memberSet := func(variantName string) (map[connect.Id]bool, error) {
		variant, err := finalLifecycleVariantByName(lifecycle, variantName)
		if err != nil {
			return nil, err
		}
		ids := make(map[connect.Id]bool, len(variant.Members))
		for _, member := range variant.Members {
			id, err := finalPayoutClientID(member.ClientID)
			if err != nil || ids[id] {
				return nil, stateMismatchError(err, "fleet lifecycle %s has an invalid or duplicate member", variantName)
			}
			ids[id] = true
		}
		return ids, nil
	}
	targetIDs, err := memberSet(fleetLifecycleVariantTargetTakeover)
	if err != nil {
		return nil, nil, err
	}
	providerIDs, err := memberSet(fleetLifecycleVariantProvider)
	if err != nil {
		return nil, nil, err
	}
	companionIDs, err := memberSet(fleetLifecycleVariantCompanionTakeover)
	if err != nil {
		return nil, nil, err
	}
	terminalIDs, err := memberSet(fleetLifecycleVariantTerminal)
	if err != nil {
		return nil, nil, err
	}
	fallbackIDs, err := memberSet(fleetLifecycleVariantFallback)
	if err != nil {
		return nil, nil, err
	}
	equalSets := func(left, right map[connect.Id]bool) bool {
		if len(left) != len(right) {
			return false
		}
		for id := range left {
			if !right[id] {
				return false
			}
		}
		return true
	}
	if !equalSets(targetIDs, providerIDs) || !equalSets(companionIDs, terminalIDs) {
		return nil, nil, errors.New("fleet lifecycle replacement generations change their immutable client membership")
	}
	for id := range fallbackIDs {
		if targetIDs[id] || companionIDs[id] {
			return nil, nil, fmt.Errorf("fleet lifecycle fallback client %s overlaps a provider fleet", id.String())
		}
	}
	for id := range targetIDs {
		if companionIDs[id] {
			return nil, nil, fmt.Errorf("fleet lifecycle target client %s overlaps the companion fleet", id.String())
		}
	}
	applyTier := func(variantName, tier string) error {
		variant, err := finalLifecycleVariantByName(lifecycle, variantName)
		if err != nil {
			return err
		}
		for _, member := range variant.Members {
			id, err := finalPayoutClientID(member.ClientID)
			if err != nil {
				return err
			}
			assignment, ok := result[id]
			if !ok {
				return fmt.Errorf("fleet lifecycle %s client %s is absent from miner assignments", variantName, id.String())
			}
			assignment.Tier = tier
			result[id] = assignment
		}
		return nil
	}
	if expected.Epoch >= state.TakeoverEffectiveEpoch {
		if err := applyTier(fleetLifecycleVariantTargetTakeover, "head-candidate"); err != nil {
			return nil, nil, err
		}
		if err := applyTier(fleetLifecycleVariantCompanionTakeover, "head-candidate"); err != nil {
			return nil, nil, err
		}
	}
	if expected.Epoch >= state.FallbackEffectiveEpoch {
		if err := applyTier(fleetLifecycleVariantTargetTakeover, "pool-tail"); err != nil {
			return nil, nil, err
		}
		if err := applyTier(fleetLifecycleVariantFallback, "head-candidate"); err != nil {
			return nil, nil, err
		}
	}
	if expected.Epoch >= state.ProviderEffectiveEpoch {
		if err := applyTier(fleetLifecycleVariantProvider, "head-candidate"); err != nil {
			return nil, nil, err
		}
		if err := applyTier(fleetLifecycleVariantCompanionTakeover, "pool-tail"); err != nil {
			return nil, nil, err
		}
	}
	if expected.Epoch >= state.TerminalEffectiveEpoch {
		if err := applyTier(fleetLifecycleVariantTerminal, "head-candidate"); err != nil {
			return nil, nil, err
		}
		if err := applyTier(fleetLifecycleVariantFallback, "pool-tail"); err != nil {
			return nil, nil, err
		}
	}
	var records []FleetLifecyclePayoutEvidence
	assignedLifecycleClients := map[connect.Id]string{}
	for _, payout := range state.Payouts {
		if payout.Epoch != expected.Epoch || uint64(payout.NoID) != expected.NoID {
			continue
		}
		variantName, tier, err := finalLifecyclePayoutVariant(payout.Disposition)
		if err != nil {
			return nil, nil, err
		}
		variant, err := finalLifecycleVariantByName(evidence.FleetLifecycle, variantName)
		if err != nil {
			return nil, nil, err
		}
		want := make(map[connect.Id]bool, len(variant.Members))
		for _, member := range variant.Members {
			id, err := finalPayoutClientID(member.ClientID)
			if err != nil || want[id] {
				return nil, nil, stateMismatchError(err, "fleet lifecycle %s has an invalid or duplicate member", variantName)
			}
			assignment, ok := assignments[id]
			if !ok || assignment.NoID != expected.NoID {
				return nil, nil, fmt.Errorf("fleet lifecycle %s client %s is not assigned to operator %d", variantName, id.String(), expected.NoID)
			}
			want[id] = true
		}
		got := make(map[connect.Id]bool, len(payout.ClientIDs))
		for _, encoded := range payout.ClientIDs {
			id, err := finalPayoutClientID(encoded)
			if err != nil || got[id] {
				return nil, nil, stateMismatchError(err, "fleet lifecycle payout %s has an invalid or duplicate client", payout.Disposition)
			}
			got[id] = true
		}
		if len(got) != len(want) {
			return nil, nil, fmt.Errorf("fleet lifecycle payout %s client count=%d, want %d", payout.Disposition, len(got), len(want))
		}
		for id := range want {
			if !got[id] {
				return nil, nil, fmt.Errorf("fleet lifecycle payout %s omits exact client %s", payout.Disposition, id.String())
			}
			if prior, duplicate := assignedLifecycleClients[id]; duplicate {
				return nil, nil, fmt.Errorf("operator %d epoch %d lifecycle client %s appears in both %s and %s", expected.NoID, expected.Epoch, id.String(), prior, payout.Disposition)
			}
			assignedLifecycleClients[id] = payout.Disposition
			if assignment := result[id]; assignment.Tier != tier {
				return nil, nil, fmt.Errorf("fleet lifecycle payout %s client %s has historical tier %s, want %s", payout.Disposition, id.String(), assignment.Tier, tier)
			}
		}
		records = append(records, payout)
	}
	if len(records) > 2 {
		return nil, nil, fmt.Errorf("operator %d epoch %d has %d lifecycle payout dispositions, want at most one returned and one excluded set", expected.NoID, expected.Epoch, len(records))
	}
	if len(records) == 2 {
		seenTier := map[string]bool{}
		for _, payout := range records {
			_, tier, _ := finalLifecyclePayoutVariant(payout.Disposition)
			if seenTier[tier] {
				return nil, nil, fmt.Errorf("operator %d epoch %d lifecycle payout pair repeats tier %s", expected.NoID, expected.Epoch, tier)
			}
			seenTier[tier] = true
		}
		if !seenTier["pool-tail"] || !seenTier["head-candidate"] {
			return nil, nil, fmt.Errorf("operator %d epoch %d lifecycle payout pair is incomplete", expected.NoID, expected.Epoch)
		}
	}
	return result, records, nil
}

func verifyFinalPayoutArtifact(evidence *FinalSemanticEvidence, pool *FinalPoolUIDEvidence, expected *finalPayoutArtifactExpectation, assignments map[connect.Id]finalMinerAssignment, data []byte) error {
	if evidence == nil || pool == nil || expected == nil {
		return errors.New("payout artifact identity is incomplete")
	}
	artifact, err := payoutartifact.Decode(data)
	if err != nil {
		return err
	}
	if artifact.DeploymentID != evidence.DeploymentID || artifact.ChainID != evidence.ChainID || artifact.Netuid != evidence.Netuid || !strings.EqualFold(artifact.GenesisHash, evidence.GenesisHash) || !strings.EqualFold(artifact.PolicyHash, evidence.PolicyHash) || !strings.EqualFold(artifact.Coordinator.Hex(), evidence.Deployment.CoordinatorProxy) || !strings.EqualFold(artifact.SettlementVault.Hex(), evidence.Deployment.SettlementVault) || artifact.NoID != expected.NoID || artifact.Epoch != expected.Epoch {
		return errors.New("payout artifact deployment, policy, operator, or epoch mismatch")
	}
	if strings.ToLower(artifact.Signer.Hex()) != pool.PayoutRootSigner {
		return errors.New("payout artifact signer is not the authorized operator root signer")
	}
	if !strings.EqualFold("0x"+strings.TrimPrefix(artifact.ContentHash, "sha256:"), expected.ArtifactHash) {
		return fmt.Errorf("payout artifact content hash %s is not the committed artifact hash %s for epoch %d operator %d", artifact.ContentHash, expected.ArtifactHash, expected.Epoch, expected.NoID)
	}
	if expected.UsageBytes != 0 && artifact.TotalUsageBytes != expected.UsageBytes {
		return errors.New("payout artifact usage does not match deposit audit")
	}
	if expected.PayoutRoot != "" && !strings.EqualFold("0x"+hex.EncodeToString(artifact.PayoutRoot[:]), expected.PayoutRoot) {
		return errors.New("payout artifact Merkle root does not match committed root")
	}
	epochAssignments, lifecyclePayouts, err := finalPayoutAssignmentsAt(evidence, expected, assignments)
	if err != nil {
		return err
	}
	tailLeaves := 0
	providerByClient := make(map[connect.Id]payoutartifact.ProviderInput, len(artifact.Providers))
	for _, provider := range artifact.Providers {
		clientID := connect.Id(provider.ClientID)
		assignment, ok := epochAssignments[clientID]
		if !ok {
			return fmt.Errorf("payout provider %s is absent from miner tier assignments", clientID.String())
		}
		if _, duplicate := providerByClient[clientID]; duplicate {
			return fmt.Errorf("payout artifact duplicates provider %s", clientID.String())
		}
		providerByClient[clientID] = provider
		switch assignment.Tier {
		case "head-candidate":
			if !provider.HeadExcluded {
				return fmt.Errorf("head candidate %s is not excluded from pool payout", clientID.String())
			}
		case "pool-tail":
			if provider.HeadExcluded {
				return fmt.Errorf("pool-tail provider %s is incorrectly head-excluded", clientID.String())
			}
		default:
			return fmt.Errorf("provider %s has unknown tier %q", clientID.String(), assignment.Tier)
		}
	}
	leafByClient := make(map[connect.Id]bool, len(artifact.Leaves))
	for _, leaf := range artifact.Leaves {
		id := connect.Id(leaf.ClientID)
		if leafByClient[id] {
			return fmt.Errorf("payout artifact duplicates leaf client %s", id.String())
		}
		leafByClient[id] = true
		if epochAssignments[id].Tier != "pool-tail" {
			return errors.New("payout leaf belongs to a head candidate")
		}
		tailLeaves++
	}
	if tailLeaves == 0 {
		return errors.New("payout artifact has no pool-tail leaves")
	}
	for _, payout := range lifecyclePayouts {
		_, tier, _ := finalLifecyclePayoutVariant(payout.Disposition)
		for _, encoded := range payout.ClientIDs {
			id, _ := finalPayoutClientID(encoded)
			provider, ok := providerByClient[id]
			if !ok {
				return fmt.Errorf("fleet lifecycle payout %s client %s is absent from the decoded payout providers", payout.Disposition, id.String())
			}
			switch tier {
			case "head-candidate":
				if !provider.HeadExcluded || leafByClient[id] {
					return fmt.Errorf("fleet lifecycle payout %s client %s is not exactly head-excluded", payout.Disposition, id.String())
				}
			case "pool-tail":
				if provider.HeadExcluded || !provider.Eligible || provider.UsageBytes == 0 || !leafByClient[id] {
					return fmt.Errorf("fleet lifecycle payout %s client %s is not an eligible included pool leaf", payout.Disposition, id.String())
				}
			}
		}
	}
	if len(lifecyclePayouts) != 0 {
		tracked := map[connect.Id]bool{}
		for _, variant := range evidence.FleetLifecycle.Variants {
			for _, member := range variant.Members {
				id, err := finalPayoutClientID(member.ClientID)
				if err != nil {
					return err
				}
				assignment, ok := epochAssignments[id]
				if !ok || assignment.NoID != expected.NoID || tracked[id] {
					continue
				}
				tracked[id] = true
				provider, present := providerByClient[id]
				if !present {
					return fmt.Errorf("fleet lifecycle tracked client %s is absent from operator %d epoch %d payout", id.String(), expected.NoID, expected.Epoch)
				}
				switch assignment.Tier {
				case "head-candidate":
					if !provider.HeadExcluded || leafByClient[id] {
						return fmt.Errorf("fleet lifecycle tracked head client %s is not exclusively excluded", id.String())
					}
				case "pool-tail":
					if provider.HeadExcluded || !provider.Eligible || provider.UsageBytes == 0 || !leafByClient[id] {
						return fmt.Errorf("fleet lifecycle tracked pool client %s is not exclusively included", id.String())
					}
				default:
					return fmt.Errorf("fleet lifecycle tracked client %s has unknown tier %q", id.String(), assignment.Tier)
				}
			}
		}
	}
	for _, claim := range expected.Claims {
		if claim.LeafIndex >= uint64(len(artifact.Leaves)) {
			return fmt.Errorf("claim leaf %d is outside payout artifact", claim.LeafIndex)
		}
		leaf := artifact.Leaves[claim.LeafIndex]
		if !strings.EqualFold("0x"+hex.EncodeToString(leaf.Coldkey[:]), claim.Payee) || leaf.ShareBPS != claim.ShareBPS {
			return fmt.Errorf("claim leaf %d payee/share does not match payout artifact", claim.LeafIndex)
		}
	}
	return nil
}

func finalMinerTierByClient(data []byte) (map[connect.Id]finalMinerAssignment, error) {
	var bindings []FinalFleetMemberBindingEvidence
	if err := decodeStrictJSONBytes(data, &bindings); err != nil {
		return nil, fmt.Errorf("decode fleet binding manifest: %w", err)
	}
	result := make(map[connect.Id]finalMinerAssignment, len(bindings))
	for _, binding := range bindings {
		clientID, err := finalPayoutClientID(binding.ClientID)
		if err != nil || clientID == (connect.Id{}) || result[clientID].Tier != "" || binding.NoID == 0 {
			return nil, errors.New("miner tier assignments contain an invalid or duplicate client id")
		}
		result[clientID] = finalMinerAssignment{NoID: binding.NoID, Tier: binding.Tier}
	}
	return result, nil
}

func verifyFinalPolicyArtifact(evidence *FinalSemanticEvidence, data []byte) (*protocol.Policy, error) {
	var policy protocol.Policy
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return nil, fmt.Errorf("decode canonical policy artifact: %w", err)
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	canonical, err := policy.CanonicalBytes()
	if err != nil || !bytes.Equal(canonical, data) {
		return nil, errors.New("policy artifact is not canonical JSON")
	}
	hash, err := policy.HashHex()
	if err != nil || hash != evidence.PolicyHash {
		return nil, errors.New("policy artifact hash does not match on-chain policy hash")
	}
	return &policy, nil
}

// Decode the exact approved plan rather than treating its declared digest as
// an authority. Besides authenticating the current plan, this is the bounded
// lineage used to accept a journaled v4 receipt carried across a revision.
func verifyFinalSetupPlanArtifact(evidence *FinalSemanticEvidence, data []byte) (*SetupPlan, error) {
	if evidence == nil || len(data) == 0 {
		return nil, errors.New("approved setup plan artifact is unavailable")
	}
	var plan SetupPlan
	if err := decodeStrictJSONBytes(data, &plan); err != nil {
		return nil, fmt.Errorf("decode approved setup plan artifact: %w", err)
	}
	observedHash, err := persistedSetupPlanHash(data, plan.Schema)
	if err != nil || plan.Schema != currentSetupPlanSchema || plan.PlanHash != evidence.PlanHash || observedHash != evidence.PlanHash || plan.DeploymentID != evidence.DeploymentID || plan.ChainID != evidence.ChainID || plan.GenesisHash != evidence.GenesisHash || plan.Netuid != evidence.Netuid || plan.ConfigHash != evidence.ConfigHash || plan.PolicyHash != evidence.PolicyHash {
		return nil, stateMismatchError(err, "approved setup plan artifact differs from the final deployment identity")
	}
	seenPlans := map[string]bool{plan.PlanHash: true}
	for _, hash := range plan.PriorPlanHashes {
		if requireFinalHex32("approved prior plan hash", hash) != nil || seenPlans[hash] {
			return nil, errors.New("approved setup plan lineage is noncanonical or duplicated")
		}
		seenPlans[hash] = true
	}
	seenActions := map[string]bool{}
	for _, action := range plan.Actions {
		if action.ID == "" || seenActions[action.ID] {
			return nil, errors.New("approved setup plan action identity is empty or duplicated")
		}
		seenActions[action.ID] = true
		if err := requireFinalHex32("approved action intent hash", action.IntentHash); err != nil {
			return nil, err
		}
		derivedIntent, err := actionIntentHash(action)
		if err != nil || derivedIntent != action.IntentHash {
			return nil, stateMismatchError(err, "approved setup action %s intent hash is not derived from its exact fields", action.ID)
		}
		seenIntents := map[string]bool{action.IntentHash: true}
		for _, hash := range action.AcceptedPriorIntentHashes {
			if requireFinalHex32("approved prior action intent hash", hash) != nil || seenIntents[hash] {
				return nil, fmt.Errorf("approved setup action %s has a noncanonical or duplicated intent lineage", action.ID)
			}
			seenIntents[hash] = true
		}
	}
	return &plan, nil
}

func verifyFinalSetupPlanActionReceipt(plan *SetupPlan, postcondition *ActionPostcondition) error {
	if plan == nil || postcondition == nil || !plan.allowedPlanHashes()[postcondition.PlanHash] {
		return errors.New("v4 postcondition plan is outside the approved setup lineage")
	}
	for _, action := range plan.Actions {
		if action.ID == postcondition.ActionID {
			if !actionAcceptsIntent(action, postcondition.IntentHash) {
				return fmt.Errorf("approved setup action %s does not accept the v4 postcondition intent", action.ID)
			}
			return nil
		}
	}
	return fmt.Errorf("approved setup plan has no action %s", postcondition.ActionID)
}

func verifyFinalPolicyInputs(evidence *FinalSemanticEvidence, policy *protocol.Policy) error {
	if evidence == nil || policy == nil {
		return errors.New("canonical policy input is unavailable")
	}
	w := evidence.Window
	if evidence.Phase == "release-1.0" {
		if w.EpochBlocks != policy.Settlement.EpochBlocks || w.FinalizeOffsetBlocks != policy.Settlement.FinalizeOffsetBlocks {
			return errors.New("release acceptance cadence does not match canonical accelerated policy")
		}
	} else if evidence.Phase == "production-soak" {
		if w.EpochBlocks != policy.ProductionCadence.EpochBlocks || w.FinalizeOffsetBlocks != policy.ProductionCadence.FinalizeOffsetBlocks {
			return errors.New("production acceptance cadence does not match canonical production policy")
		}
	} else {
		return fmt.Errorf("unsupported semantic policy phase %q", evidence.Phase)
	}
	wantTTL, ok := checkedMul(policy.Settlement.EpochBlocks, policy.Settlement.ClaimTTLEpochs)
	if !ok || wantTTL == 0 || evidence.Deployment.VaultMinimumClaimTTLBlocks != wantTTL {
		return errors.New("vault minimum claim TTL does not match the canonical signed policy")
	}
	for _, validator := range evidence.Validators {
		for _, cycle := range validator.Cycles {
			if cycle.Theta.Numerator != strconv.FormatUint(policy.Steering.Theta.Numerator, 10) || cycle.Theta.Denominator != strconv.FormatUint(policy.Steering.Theta.Denominator, 10) || cycle.QualityMinimumPPM != policy.Steering.QualityTransform.MinimumPPM || cycle.QualityMaximumPPM != policy.Steering.QualityTransform.MaximumPPM || cycle.MaximumHeadFleets != policy.Steering.MaximumHeadFleets || cycle.MaxWeightLimitU16 != policy.Steering.MaxWeightLimitU16 {
				return fmt.Errorf("validator %d epoch %d steering parameters do not match canonical policy", validator.ValidatorID, cycle.SettlementEpoch)
			}
			for _, pool := range cycle.Pools {
				conviction, err := finalNonnegativeInteger("conviction before", pool.ConvictionBeforeRao)
				if err != nil {
					return err
				}
				amount, tier, err := protocol.RequiredDepositRao(pool.UsageBytes, conviction, policy.Deposit)
				if err != nil || amount.String() != pool.RequiredDepositRao || tier.RateNumeratorRaoPerGiB != pool.RateNumeratorRaoPerGiB || tier.RateDenominator != pool.RateDenominator || strconv.FormatUint(policy.Deposit.EpochCapRaoPerOperator, 10) != pool.EpochDepositCapRao {
					return fmt.Errorf("validator %d epoch %d pool %d deposit inputs do not match canonical policy", validator.ValidatorID, cycle.SettlementEpoch, pool.NoID)
				}
			}
		}
	}
	for _, proof := range evidence.PathProofs {
		if proof.TrailDepth != policy.Verify.TrailDepth {
			return fmt.Errorf("validator %d pool %d trail depth does not match canonical policy", proof.ValidatorID, proof.NoID)
		}
	}
	return nil
}

func finalFleetLifecyclePhaseEvidenceHeads(lifecycle *FinalFleetLifecycleEvidence, phase string) (ChainHead, ChainHead) {
	var evm, native ChainHead
	update := func(destination *ChainHead, candidate ChainHead) {
		if candidate.Number > destination.Number {
			*destination = candidate
		}
	}
	if lifecycle == nil {
		return evm, native
	}
	for _, census := range lifecycle.State.CandidateCensuses {
		if census.Phase != phase {
			continue
		}
		update(&evm, census.ObservedHead)
		update(&native, census.NativeObservedHead)
		for _, validator := range census.Validators {
			update(&evm, validator.EVMSnapshot)
			update(&native, validator.NativeSnapshot)
			update(&native, validator.Commit)
			update(&native, ChainHead{Number: validator.RevealBlock, Hash: validator.RevealBlockHash})
			update(&native, validator.Application)
		}
	}
	if phase != "release-1.0" {
		return evm, native
	}
	if lifecycle.State.LaunchPrune != nil {
		update(&native, lifecycle.State.LaunchPrune.Head)
	}
	for _, variant := range lifecycle.Variants {
		update(&native, ChainHead{Number: variant.Commitment.FinalizedBlock, Hash: variant.Commitment.FinalizedBlockHash})
		update(&evm, ChainHead{Number: variant.Mirror.BlockNumber, Hash: variant.Mirror.BlockHash})
		for _, binding := range variant.Bindings {
			update(&evm, ChainHead{Number: binding.BlockNumber, Hash: binding.BlockHash})
		}
		if variant.Registration != nil {
			update(&native, ChainHead{Number: variant.Registration.BlockNumber, Hash: variant.Registration.BlockHash})
		}
		for _, cleanup := range variant.Cleanups {
			update(&evm, ChainHead{Number: cleanup.BlockNumber, Hash: cleanup.BlockHash})
		}
	}
	for _, payout := range lifecycle.PayoutArtifacts {
		update(&evm, payout.Root.Block)
	}
	return evm, native
}

// RenderFinalSemanticEvidenceMarkdown emits no output until the complete
// semantic object verifies, preventing a partial FINAL.md from looking final.
func RenderFinalSemanticEvidenceMarkdown(evidence *FinalSemanticEvidence) ([]byte, error) {
	if err := VerifyFinalSemanticEvidence(evidence); err != nil {
		return nil, fmt.Errorf("refuse FINAL.md semantic section: %w", err)
	}
	if evidence.PublicVerification == nil {
		return nil, errors.New("refuse FINAL.md semantic section: public archive-RPC verification is missing")
	}
	var out strings.Builder
	fmt.Fprintf(&out, "## External semantic evidence\n\n")
	fmt.Fprintf(&out, "Evidence `%s` binds current %s run `%s`, result `%s`, deployment `%s`, plan `%s`, config `%s`, policy `%s`, native genesis `%s`, EVM chain %d, and netuid %d. The adversarial EVM campaign begins at %d/`%s`; the accepted EVM window is epochs %d–%d (blocks %d–%d, terminal %d); the native proof window is %d–%d.\n\n",
		evidence.EvidenceHash, finalMarkdown(evidence.Phase), finalMarkdown(evidence.RunID), evidence.ResultHash, finalMarkdown(evidence.DeploymentID), evidence.PlanHash, evidence.ConfigHash, evidence.PolicyHash, evidence.GenesisHash, evidence.ChainID, evidence.Netuid,
		evidence.EVMCampaignStartHead.Number, evidence.EVMCampaignStartHead.Hash,
		evidence.Window.FirstEpoch, evidence.Window.FirstEpoch+evidence.Window.EpochCount-1, evidence.Window.StartBlock, evidence.Window.EndBlock-1, evidence.Window.TerminalBlock,
		evidence.NativeStartHead.Number, evidence.NativeTerminalHead.Number)
	if evidence.PriorPhase != nil {
		fmt.Fprintf(&out, "Production phase `%s` continues authenticated, semantic_verified release-1.0 run `%s` (result `%s`, semantic evidence `%s`, public transcript `%s`, owner completion envelope `%s`, evidence-manifest envelope `%s`, semantic supplement envelope `%s`; carried [semantic evidence](%s)), whose terminal native checkpoint is %d/`%s` and terminal EVM checkpoint is %d/`%s`.\n\n", evidence.Phase, finalMarkdown(evidence.PriorPhase.RunID), evidence.PriorPhase.ResultHash, evidence.PriorPhase.SemanticEvidenceHash, evidence.PriorPhase.PublicTranscriptHash, evidence.PriorPhase.OwnerCompletionEnvelopeHash, evidence.PriorPhase.EvidenceManifestEnvelopeHash, evidence.PriorPhase.SemanticSupplementEnvelopeHash, finalMarkdown(evidence.PriorPhase.SemanticEvidence.URI), evidence.PriorPhase.TerminalNativeHead.Number, evidence.PriorPhase.TerminalNativeHead.Hash, evidence.PriorPhase.TerminalEVMHead.Number, evidence.PriorPhase.TerminalEVMHead.Hash)
	}
	fmt.Fprintf(&out, "Archive-retention preflight `%s`, generated `%s`, proves public history depth %d blocks for the %d-block composite campaign plus a %d-block peer-review margin; immutable receipt [%s](%s).\n\n", evidence.ArchiveRetention.EvidenceHash, evidence.ArchiveRetention.GeneratedAt, evidence.ArchiveRetention.RequiredDepthBlocks, evidence.ArchiveRetention.PlannedSpanBlocks, evidence.ArchiveRetention.SafetyMarginBlocks, evidence.ArchiveRetention.Artifact.ContentHash, finalMarkdown(evidence.ArchiveRetention.Artifact.URI))
	fmt.Fprintf(&out, "Authenticated public deployment manifest `%s` is replicated at exactly two distinct operator origins; primary/current URI `%s` is one of them. Public archive verification transcript `%s` contains %d pinned JSON-RPC exchanges against Substrate `%s` (terminal %d/`%s`) and EVM `%s` (terminal %d/`%s`). From a clean checkout with no existing simulator state, independently inspect and analyze the complete signed deployment/completion/archive graph through each origin:\n\n", evidence.PublicVerification.PublicManifestHash, finalMarkdown(evidence.PublicVerification.EvidenceURI), evidence.PublicVerification.TranscriptHash, len(evidence.PublicVerification.Exchanges), finalMarkdown(evidence.PublicVerification.SubstrateRPC), evidence.NativeTerminalHead.Number, evidence.NativeTerminalHead.Hash, finalMarkdown(evidence.PublicVerification.EVMRPC), evidence.EVMTerminalHead.Number, evidence.EVMTerminalHead.Hash)
	for _, origin := range evidence.PublicVerification.OperatorEvidenceOrigins {
		fmt.Fprintf(&out, "Operator %d manifest `%s`:\n\n```sh\ngo run ./sim-testnet inspect --config sim-testnet/testnet.yml --manifest %s --format json\ngo run ./sim-testnet analyze --config sim-testnet/testnet.yml --manifest %s --run-id %s --format json\n```\n\n", origin.OperatorNoID, finalMarkdown(origin.ManifestURI), finalMarkdown(origin.ManifestURI), finalMarkdown(origin.ManifestURI), finalMarkdown(evidence.RunID))
	}
	fmt.Fprintf(&out, "Topology is exactly %d SDK miner instances hosted by %d swarm processes in %d candidate fleets competing for %d slots, with %d validator processes and %d operator pools. Full process and member censuses: [%s](%s), [%s](%s).\n\n", evidence.Topology.MinerSDKInstances, evidence.Topology.MinerSwarmProcesses, evidence.Topology.HeadCandidateFleets, evidence.Topology.HeadSlots, evidence.Topology.ValidatorProcesses, evidence.Topology.OperatorPools, evidence.Topology.MinerManifestHash, finalMarkdown(evidence.Topology.MinerManifest.URI), evidence.Topology.BindingManifestHash, finalMarkdown(evidence.Topology.BindingManifest.URI))
	if lifecycle := evidence.FleetLifecycle; lifecycle != nil {
		state := &lifecycle.State
		settlementFirst, settlementLast, subnetFirst, subnetLast := ^uint64(0), uint64(0), ^uint64(0), uint64(0)
		for _, census := range state.CandidateCensuses {
			for _, validator := range census.Validators {
				settlementFirst = min(settlementFirst, validator.SettlementEpoch)
				settlementLast = max(settlementLast, validator.SettlementEpoch)
				subnetFirst = min(subnetFirst, validator.SubnetEpoch)
				subnetLast = max(subnetLast, validator.SubnetEpoch)
			}
		}
		fmt.Fprintf(&out, "Fleet lifecycle `%s` stage `%s` proves the exact runtime-453 launch victim and five authenticated commitment/mirror/binding generations, including three action/journal-bound replacement receipts, %d binding cleanups, four dynamically timed payout dispositions, %d captured applied-decision censuses, and %d exact validator-signed intent/measurement/envelope rows. The independent counters are reported explicitly: settlement epochs %d–%d and native subnet epochs %d–%d. Provider reward replay is fenced at post-registration native checkpoint %d/`%s`; the complete captured plan/journal/artifact graph is [%s](%s).\n\n", state.Schema, state.Stage, len(state.TargetCleanup)+len(state.CompanionCleanup)+len(state.FallbackCleanup), len(state.CandidateCensuses), len(lifecycle.AppliedDecisions), settlementFirst, settlementLast, subnetFirst, subnetLast, state.PostRegistrationRewardBaseline.Number, state.PostRegistrationRewardBaseline.Hash, lifecycle.LineageArtifact.ContentHash, finalMarkdown(lifecycle.LineageArtifact.URI))
		releaseEVMEnd, releaseNativeEnd := finalFleetLifecyclePhaseEvidenceHeads(lifecycle, "release-1.0")
		fmt.Fprintf(&out, "Release acceptance geometry remains exactly blocks %d–%d with terminal %d (%d blocks). Its separately labelled handoff lifecycle evidence reaches EVM %d/`%s` and native %d/`%s`, within signed EVM/native deadlines %d/%d; EVM evidence after acceptance terminal %d is lifecycle-tail proof only and is not counted as an accepted epoch. Assignment-filter restoration is operational handoff-tail work, never accepted-window geometry.\n\n", state.AcceptanceStartBlock, state.AcceptanceEndBlock-1, state.AcceptanceTerminalBlock, state.AcceptanceTerminalBlock-state.AcceptanceStartBlock, releaseEVMEnd.Number, releaseEVMEnd.Hash, releaseNativeEnd.Number, releaseNativeEnd.Hash, state.ReleaseEVMEvidenceDeadlineBlock, state.ReleaseHandoffSchedule.ApplicationDeadlineBlock, state.AcceptanceTerminalBlock)
		if evidence.Phase == "production-soak" {
			productionEVMEnd, productionNativeEnd := finalFleetLifecyclePhaseEvidenceHeads(lifecycle, "production-soak")
			fmt.Fprintf(&out, "Production acceptance geometry remains exactly blocks %d–%d with terminal %d (%d blocks). Its separately labelled terminal-proof lifecycle evidence reaches EVM %d/`%s` and native %d/`%s`, within signed EVM/native deadlines %d/%d; it proves terminal-active lifecycle state but does not enlarge the production acceptance window.\n\n", state.ProductionAcceptanceStartBlock, state.ProductionAcceptanceEndBlock-1, state.ProductionAcceptanceTerminalBlock, state.ProductionAcceptanceTerminalBlock-state.ProductionAcceptanceStartBlock, productionEVMEnd.Number, productionEVMEnd.Hash, productionNativeEnd.Number, productionNativeEnd.Hash, state.ProductionEVMEvidenceDeadlineBlock, state.ProductionNativeSchedule.ApplicationDeadlineBlock)
		}
	}
	var expectedRestarts uint64
	for _, process := range evidence.Topology.ProcessRestarts {
		expectedRestarts += process.ExpectedRestarts
	}
	fmt.Fprintf(&out, "The terminal supervisor census contains %d managed processes and exactly %d release-scheduled, successfully restored restarts; observed counts equal the fault-attributed counts for every process.\n\n", len(evidence.Topology.ProcessRestarts), expectedRestarts)
	fmt.Fprintf(&out, "Accepted supervisor generation `%s` / start ticks %d completed %d/%d pre-start operator contract cleanups at exact cutoff `%s`, with zero failed invocations.\n\n", evidence.ContractCleanup.SupervisorManifestHash, evidence.ContractCleanup.SupervisorStartTimeTicks, evidence.ContractCleanup.SuccessfulInvocations, evidence.ExpectedOperators, evidence.ContractCleanup.Cutoff)
	fmt.Fprintf(&out, "Contracts at EVM block %d: coordinator proxy `%s` → implementation `%s`, settlement vault `%s`, immutable reserve sink `%s`, governance owner `%s`, policy version %d. Runtime hashes proxy/implementation/vault/sink are `%s` / `%s` / `%s` / `%s`; ERC1967 slot `%s` contains `%s`.\n\n", evidence.Deployment.Snapshot.Number, evidence.Deployment.CoordinatorProxy, evidence.Deployment.CoordinatorImplementation, evidence.Deployment.SettlementVault, evidence.Deployment.ReserveSink, evidence.Deployment.GovernanceOwner, evidence.Deployment.PolicyVersion, evidence.Deployment.CoordinatorProxyCodeHash, evidence.Deployment.ImplementationCodeHash, evidence.Deployment.SettlementVaultCodeHash, evidence.Deployment.ReserveSinkCodeHash, evidence.Deployment.ERC1967ImplementationSlot, evidence.Deployment.ObservedImplementationSlot)
	fmt.Fprintf(&out, "Terminal custody is netuid %d throughout: coordinator/vault/reserve self-coldkeys `%s` / `%s` / `%s`; guardian `%s` is active, commitment oracle `%s` is active, and the coordinator is unpaused. Vault linkage/escrow is `%s` / `%s` (registered), claim TTL %d blocks, and minimum transfer %d rao equals the signed-plan DefaultMinTransfer. Reserve recorder/hotkey is `%s` / `%s`.\n\n", evidence.Netuid, evidence.Deployment.CoordinatorSelfColdkey, evidence.Deployment.VaultSelfColdkey, evidence.Deployment.ReserveSelfColdkey, evidence.Deployment.CoordinatorActiveGuardian, evidence.Deployment.CoordinatorActiveCommitmentOracle, evidence.Deployment.VaultCoordinator, evidence.Deployment.VaultEscrowHotkey, evidence.Deployment.VaultMinimumClaimTTLBlocks, evidence.Deployment.VaultMinimumTransferTaoRao, evidence.Deployment.ReserveRecorder, evidence.Deployment.ReserveHotkey)
	fmt.Fprintf(&out, "Settlement-vault global accounting at baseline/terminal: totalCaptured %s/%s, totalPaid %s/%s, escrowAccounted %s/%s, pendingFunding %s/%s, outstandingLiability %s/%s, liveEscrowStake %s/%s rao. Exact interval deltas are captured %s (= EmissionCaptured events %s), paid %s (= ClaimPaid events %s), escrow %s, pending %s, liability %s, and live stake %s. Both heads satisfy totalCaptured = totalPaid + escrowAccounted, escrowAccounted = pendingFunding + outstandingLiability, and liveEscrowStake >= escrowAccounted.\n\n", evidence.SettlementAccounting.Before.TotalCapturedRao, evidence.SettlementAccounting.After.TotalCapturedRao, evidence.SettlementAccounting.Before.TotalPaidRao, evidence.SettlementAccounting.After.TotalPaidRao, evidence.SettlementAccounting.Before.EscrowAccountedRao, evidence.SettlementAccounting.After.EscrowAccountedRao, evidence.SettlementAccounting.Before.PendingFundingRao, evidence.SettlementAccounting.After.PendingFundingRao, evidence.SettlementAccounting.Before.OutstandingLiabilityRao, evidence.SettlementAccounting.After.OutstandingLiabilityRao, evidence.SettlementAccounting.Before.LiveEscrowStakeRao, evidence.SettlementAccounting.After.LiveEscrowStakeRao, evidence.SettlementAccounting.TotalCapturedDeltaRao, evidence.SettlementAccounting.EmissionCapturedEventRao, evidence.SettlementAccounting.TotalPaidDeltaRao, evidence.SettlementAccounting.ClaimPaidEventRao, evidence.SettlementAccounting.EscrowAccountedDeltaRao, evidence.SettlementAccounting.PendingFundingDeltaRao, evidence.SettlementAccounting.OutstandingLiabilityDeltaRao, evidence.SettlementAccounting.LiveEscrowStakeDeltaRao)
	fmt.Fprintf(&out, "Reserve principal/live stake moved one-way from %s/%s to %s/%s rao across EVM blocks %d–%d; principal delta %s exactly equals %s across %d publicly replayed ReservePrincipalAdded receipts.\n\n", evidence.Reserve.PrincipalBeforeRao, evidence.Reserve.LiveStakeBeforeRao, evidence.Reserve.PrincipalAfterRao, evidence.Reserve.LiveStakeAfterRao, evidence.Reserve.Before.Number, evidence.Reserve.After.Number, evidence.Reserve.PrincipalDeltaRao, evidence.Reserve.PrincipalAddedRao, len(evidence.Reserve.PrincipalAdditions))
	fmt.Fprintf(&out, "### Native ownership and validator state\n\n")
	fmt.Fprintf(&out, "Pool rows bind the EVM registration transaction to the terminal native UID ownership snapshot. Each pool custody coldkey is the exact SS58 mirror of settlement vault `%s`; its distinct operator registry coldkey is shown separately. Fleet and validator registration receipts remain native extrinsics.\n\n", evidence.Deployment.SettlementVault)
	fmt.Fprintf(&out, "| Role | ID | UID | Hotkey | Custody coldkey | Operator registry coldkey | Stake / carry (rao) | Permit | vtrust | Registration | Native snapshot artifact |\n|---|---:|---:|---|---|---|---:|---|---:|---|---|\n")
	for _, pool := range evidence.Pools {
		fmt.Fprintf(&out, "| pool | %d | %d | `%s` | `%s` | `%s` | %s | n/a | n/a | EVM `%s` @ %d ([receipt](%s)) | %d/`%s` [%s](%s) |\n", pool.NoID, pool.UID, finalMarkdown(pool.Hotkey), finalMarkdown(pool.Coldkey), finalMarkdown(pool.OperatorColdkey), pool.FinalCarryRao, pool.Registration.TransactionHash, pool.Registration.Block.Number, finalMarkdown(pool.Registration.Proof.URI), pool.Snapshot.Number, pool.Snapshot.Hash, pool.OwnershipArtifact.ContentHash, finalMarkdown(pool.OwnershipArtifact.URI))
	}
	for _, validator := range evidence.Validators {
		fmt.Fprintf(&out, "| validator | %d | %d | `%s` | `%s` | n/a | %s | %t | %d | native `%s` @ %d | %d/`%s` [%s](%s) |\n", validator.ValidatorID, validator.UID, finalMarkdown(validator.Hotkey), finalMarkdown(validator.Coldkey), validator.StakeRao, validator.ValidatorPermit, validator.ValidatorTrustU16, validator.Registration.ExtrinsicHash, validator.Registration.Block.Number, validator.Snapshot.Number, validator.Snapshot.Hash, validator.SnapshotArtifact.ContentHash, finalMarkdown(validator.SnapshotArtifact.URI))
	}
	fmt.Fprintf(&out, "\n### CRv4 decisions\n\n")
	for _, validator := range evidence.Validators {
		for _, cycle := range validator.Cycles {
			fmt.Fprintf(&out, "Validator %d, settlement epoch %d / subnet epoch %d: 202 positive candidates, deterministic top 200, two zero-weight rejects, theta %s/%s, values `%s`, intent [%s](%s), commit/reveal/application blocks %d/%d/%d.\n\n", validator.ValidatorID, cycle.SettlementEpoch, cycle.SubnetEpoch, cycle.Theta.Numerator, cycle.Theta.Denominator, cycle.ValuesHash, cycle.IntentArtifact.ContentHash, finalMarkdown(cycle.IntentArtifact.URI), cycle.Commit.Block.Number, cycle.Reveal.Block.Number, cycle.Application.Block.Number)
			fmt.Fprintf(&out, "| Rank | UID | Raw score | Selected | Applied u16 |\n|---:|---:|---:|---|---:|\n")
			for _, candidate := range cycle.Candidates {
				fmt.Fprintf(&out, "| %d | %d | %s/%s | %t | %d |\n", candidate.Rank, candidate.UID, candidate.RawScore.Numerator, candidate.RawScore.Denominator, candidate.Selected, candidate.AppliedWeight)
			}
			fmt.Fprintln(&out)
		}
	}
	if evidence.DishonestDeposit != nil {
		fmt.Fprintf(&out, "### Dishonest operator demand-deposit penalty and recovery\n\n")
		fmt.Fprintf(&out, "Pool %d / UID %d underpaid %s of required %s rao in EVM transaction `%s`; the later exact recovery deposit is `%s` (%s/%s rao). Each application row below is independently replayed from public Subtensor `Weights` at its pinned block.\n\n", evidence.DishonestDeposit.NoID, evidence.DishonestDeposit.PoolUID, evidence.DishonestDeposit.ObservedDepositRao, evidence.DishonestDeposit.RequiredDepositRao, evidence.DishonestDeposit.UnderpaymentReceipt.TransactionHash, evidence.DishonestDeposit.RecoveryDepositReceipt.TransactionHash, evidence.DishonestDeposit.RecoveryObservedDepositRao, evidence.DishonestDeposit.RecoveryRequiredDepositRao)
		fmt.Fprintf(&out, "| Stage | Validator | Validator UID | Settlement / subnet epoch | Pool UID | In applied vector | Applied u16 | Vector hash | Application checkpoint | Signed intent |\n|---|---:|---:|---|---:|---|---:|---|---|---|\n")
		for _, stage := range []struct {
			name      string
			decisions []FinalDishonestDepositDecision
		}{{name: "penalty", decisions: evidence.DishonestDeposit.Penalties}, {name: "recovery", decisions: evidence.DishonestDeposit.Recoveries}} {
			for _, decision := range stage.decisions {
				fmt.Fprintf(&out, "| %s | %d | %d | %d / %d | %d | %t | %d | `%s` | %d/`%s` | [%s](%s) |\n", stage.name, decision.ValidatorID, decision.ValidatorUID, decision.Cycle.SettlementEpoch, decision.Cycle.SubnetEpoch, decision.PoolUID, decision.PoolPresent, decision.PoolAppliedWeight, decision.Cycle.IntentVectorHash, decision.Cycle.Application.Block.Number, decision.Cycle.Application.Block.Hash, decision.Cycle.IntentArtifact.ContentHash, finalMarkdown(decision.Cycle.IntentArtifact.URI))
			}
		}
		fmt.Fprintln(&out)
	}
	fmt.Fprintf(&out, "### Deposit, payout, and carry conservation\n\n")
	fmt.Fprintf(&out, "| Epoch | NO | Captured | Carry in | Funded | Entitlement total | Claimed | Paid | Deferred | Outstanding | Carry out | Root |\n|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|\n")
	for _, row := range evidence.Epochs {
		root := row.RootDisposition
		if row.PayoutArtifact != nil {
			root = fmt.Sprintf("[%s](%s)", row.PayoutArtifact.ContentHash, finalMarkdown(row.PayoutArtifact.URI))
		}
		fmt.Fprintf(&out, "| %d | %d | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", row.Epoch, row.NoID, row.CapturedRao, row.CarryInRao, row.FundedRao, row.TotalRao, row.ClaimedRao, row.PaidRao, row.DeferredCreditRao, row.OutstandingRao, row.CarryOutRao, root)
	}
	fmt.Fprintf(&out, "\nReconstructed totals: boundary-captured %s = entitlement-funded %s; committed rows consume their exact carry-in, while missed rows preserve cumulative carry. Claimed %s = paid %s + deferred %s; aggregate carry-in observations %s, remaining outstanding %s, and carry-out observations %s.\n\n", evidence.Conservation.CapturedRao, evidence.Conservation.FundedRao, evidence.Conservation.ClaimedRao, evidence.Conservation.PaidRao, evidence.Conservation.DeferredCreditRao, evidence.Conservation.CarryInRao, evidence.Conservation.OutstandingRao, evidence.Conservation.CarryOutRao)
	fmt.Fprintf(&out, "### Native rewards and validator path proofs\n\n")
	for _, reward := range evidence.NativeRewards {
		reserve := ""
		if reward.ReserveColdkey != "" {
			reserve = fmt.Sprintf("; reserve-sink pair `%s` %s → %s (signed change %s)", finalMarkdown(reward.ReserveColdkey), reward.ReserveStakeBeforeRao, reward.ReserveStakeAfterRao, reward.ReserveStakeDeltaRao)
		}
		fmt.Fprintf(&out, "- %s %d / UID %d, epoch %d: emission %s → %s rao (signed change %s); aggregate hotkey alpha %s → %s (signed change %s); exact hotkey/owner pair `%s` / `%s` %s → %s (signed change %s) at EVM checkpoints %d/`%s` → %d/`%s`%s; terminal expectation %s, [%s](%s).\n", reward.Role, reward.SubjectID, reward.UID, reward.Epoch, reward.BeforeRao, reward.AfterRao, reward.DeltaRao, reward.StakeBeforeRao, reward.StakeAfterRao, reward.StakeDeltaRao, finalMarkdown(reward.Hotkey), finalMarkdown(reward.OwnerColdkey), reward.OwnerStakeBeforeRao, reward.OwnerStakeAfterRao, reward.OwnerStakeDeltaRao, reward.OwnerStakeBeforeEVM.Number, reward.OwnerStakeBeforeEVM.Hash, reward.OwnerStakeAfterEVM.Number, reward.OwnerStakeAfterEVM.Hash, reserve, reward.Expected, reward.SnapshotArtifact.ContentHash, finalMarkdown(reward.SnapshotArtifact.URI))
	}
	for _, proof := range evidence.PathProofs {
		fmt.Fprintf(&out, "- Validator %d → NO %d: %d proofs covering epochs %d–%d, [%s](%s).\n", proof.ValidatorID, proof.NoID, proof.ProofCount, proof.FirstEpoch, proof.LastEpoch, proof.ProofsHash, finalMarkdown(proof.Artifact.URI))
	}
	fmt.Fprintf(&out, "\n### Mandatory negative and operational gates\n\n")
	for _, criterion := range evidence.ExitCriteria {
		fmt.Fprintf(&out, "- `%s`: %s; observed %s; typed assertions %d; on-chain receipts %d; public requests %d; artifact [%s](%s).\n", criterion.ID, finalMarkdown(criterion.Expected), finalMarkdown(criterion.Observed), len(criterion.Assertions), len(criterion.EVMReceipts), len(criterion.PublicRequestHashes), criterion.Artifacts[0].ContentHash, finalMarkdown(criterion.Artifacts[0].URI))
	}
	return []byte(out.String()), nil
}

func canonicalizeFinalSemanticEvidence(evidence *FinalSemanticEvidence) {
	if evidence.FleetLifecycle != nil {
		lifecycle := evidence.FleetLifecycle
		sort.Slice(lifecycle.Roles, func(i, j int) bool { return lifecycle.Roles[i].Label < lifecycle.Roles[j].Label })
		canonicalizeFinalFleetLifecycleVariants(lifecycle.Variants)
		// State is the byte-for-byte authenticated producer state (and, for
		// release, the handoff hash preimage). Its already-validated order must
		// never be rewritten while canonicalizing the enclosing semantic object.
		sort.Slice(lifecycle.AppliedDecisions, func(i, j int) bool {
			if lifecycle.AppliedDecisions[i].CensusIndex != lifecycle.AppliedDecisions[j].CensusIndex {
				return lifecycle.AppliedDecisions[i].CensusIndex < lifecycle.AppliedDecisions[j].CensusIndex
			}
			return lifecycle.AppliedDecisions[i].ValidatorID < lifecycle.AppliedDecisions[j].ValidatorID
		})
		sort.Slice(lifecycle.PayoutArtifacts, func(i, j int) bool {
			if lifecycle.PayoutArtifacts[i].Epoch != lifecycle.PayoutArtifacts[j].Epoch {
				return lifecycle.PayoutArtifacts[i].Epoch < lifecycle.PayoutArtifacts[j].Epoch
			}
			return lifecycle.PayoutArtifacts[i].NoID < lifecycle.PayoutArtifacts[j].NoID
		})
	}
	if evidence.DishonestDeposit != nil {
		sort.Slice(evidence.DishonestDeposit.Penalties, func(i, j int) bool {
			return evidence.DishonestDeposit.Penalties[i].ValidatorID < evidence.DishonestDeposit.Penalties[j].ValidatorID
		})
		sort.Slice(evidence.DishonestDeposit.Recoveries, func(i, j int) bool {
			return evidence.DishonestDeposit.Recoveries[i].ValidatorID < evidence.DishonestDeposit.Recoveries[j].ValidatorID
		})
	}
	for index := range evidence.Topology.ProcessRestarts {
		sort.Strings(evidence.Topology.ProcessRestarts[index].FaultIDs)
	}
	sort.Slice(evidence.Topology.ProcessRestarts, func(i, j int) bool {
		return evidence.Topology.ProcessRestarts[i].ProcessID < evidence.Topology.ProcessRestarts[j].ProcessID
	})
	sort.Slice(evidence.ContractCleanup.Operators, func(i, j int) bool {
		return evidence.ContractCleanup.Operators[i].NoID < evidence.ContractCleanup.Operators[j].NoID
	})
	sort.Slice(evidence.Pools, func(i, j int) bool { return evidence.Pools[i].NoID < evidence.Pools[j].NoID })
	sort.Slice(evidence.Validators, func(i, j int) bool { return evidence.Validators[i].ValidatorID < evidence.Validators[j].ValidatorID })
	for validatorIndex := range evidence.Validators {
		validator := &evidence.Validators[validatorIndex]
		sort.Slice(validator.Cycles, func(i, j int) bool { return validator.Cycles[i].SettlementEpoch < validator.Cycles[j].SettlementEpoch })
		for cycleIndex := range validator.Cycles {
			cycle := &validator.Cycles[cycleIndex]
			sort.Slice(cycle.Pools, func(i, j int) bool { return cycle.Pools[i].NoID < cycle.Pools[j].NoID })
			sort.Slice(cycle.MaskedUIDs, func(i, j int) bool { return cycle.MaskedUIDs[i] < cycle.MaskedUIDs[j] })
		}
	}
	sort.Slice(evidence.Epochs, func(i, j int) bool {
		if evidence.Epochs[i].Epoch != evidence.Epochs[j].Epoch {
			return evidence.Epochs[i].Epoch < evidence.Epochs[j].Epoch
		}
		return evidence.Epochs[i].NoID < evidence.Epochs[j].NoID
	})
	for i := range evidence.Epochs {
		sort.Slice(evidence.Epochs[i].Claims, func(a, b int) bool {
			return evidence.Epochs[i].Claims[a].LeafIndex < evidence.Epochs[i].Claims[b].LeafIndex
		})
	}
	sort.Slice(evidence.NativeRewards, func(i, j int) bool {
		if evidence.NativeRewards[i].Epoch != evidence.NativeRewards[j].Epoch {
			return evidence.NativeRewards[i].Epoch < evidence.NativeRewards[j].Epoch
		}
		if evidence.NativeRewards[i].Role != evidence.NativeRewards[j].Role {
			return evidence.NativeRewards[i].Role < evidence.NativeRewards[j].Role
		}
		return evidence.NativeRewards[i].SubjectID < evidence.NativeRewards[j].SubjectID
	})
	sort.Slice(evidence.PathProofs, func(i, j int) bool {
		if evidence.PathProofs[i].ValidatorID != evidence.PathProofs[j].ValidatorID {
			return evidence.PathProofs[i].ValidatorID < evidence.PathProofs[j].ValidatorID
		}
		return evidence.PathProofs[i].NoID < evidence.PathProofs[j].NoID
	})
	sort.Slice(evidence.ExitCriteria, func(i, j int) bool { return evidence.ExitCriteria[i].ID < evidence.ExitCriteria[j].ID })
	for i := range evidence.ExitCriteria {
		criterion := &evidence.ExitCriteria[i]
		sort.Slice(criterion.Assertions, func(a, b int) bool { return criterion.Assertions[a].Metric < criterion.Assertions[b].Metric })
		sort.Slice(criterion.EVMReceipts, func(a, b int) bool {
			return criterion.EVMReceipts[a].TransactionHash < criterion.EVMReceipts[b].TransactionHash
		})
		sort.Strings(criterion.PublicRequestHashes)
	}
}

func finalSemanticEvidenceHash(evidence *FinalSemanticEvidence) (string, error) {
	copy := *evidence
	copy.EvidenceHash = ""
	return canonicalHashHex(copy)
}

func finalCanonicalAddress(value string) (string, error) {
	if !common.IsHexAddress(value) {
		return "", errors.New("address is invalid")
	}
	canonical := strings.ToLower(common.HexToAddress(value).Hex())
	if canonical == strings.ToLower((common.Address{}).Hex()) {
		return "", errors.New("address is zero")
	}
	return canonical, nil
}

func finalEd25519PublicKey(label, value string) (ed25519.PublicKey, error) {
	if !strings.HasPrefix(value, "0x") || value != strings.ToLower(value) {
		return nil, fmt.Errorf("%s is not canonical hex", label)
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%s is not an Ed25519 public key", label)
	}
	return ed25519.PublicKey(decoded), nil
}

func verifyFinalNativeReceipt(label string, receipt FinalNativeReceipt, minimum, maximum uint64, requireExtrinsic bool) error {
	if requireExtrinsic {
		if err := requireFinalHex32(label+" extrinsic", receipt.ExtrinsicHash); err != nil {
			return err
		}
	} else if receipt.ExtrinsicHash != "" {
		if err := requireFinalHex32(label+" extrinsic", receipt.ExtrinsicHash); err != nil {
			return err
		}
	}
	if err := verifyFinalHead(label+" block", receipt.Block); err != nil {
		return err
	}
	if receipt.Block.Number < minimum || receipt.Block.Number > maximum {
		return fmt.Errorf("%s block %d is outside [%d,%d]", label, receipt.Block.Number, minimum, maximum)
	}
	return verifyFinalArtifact(label+" proof", receipt.Proof, "native-receipt")
}

func verifyFinalEVMReceipt(label string, receipt FinalEVMReceipt, minimum, maximum uint64) error {
	return verifyFinalEVMReceiptStatus(label, receipt, minimum, maximum, "success")
}

func verifyFinalEVMReceiptStatus(label string, receipt FinalEVMReceipt, minimum, maximum uint64, expectedStatus string) error {
	if err := requireFinalHex32(label+" transaction", receipt.TransactionHash); err != nil {
		return err
	}
	if err := requireFinalHex32(label+" logs hash", receipt.LogsHash); err != nil {
		return err
	}
	if err := verifyFinalHead(label+" block", receipt.Block); err != nil {
		return err
	}
	if (expectedStatus != "success" && expectedStatus != "failed") || receipt.Status != expectedStatus || receipt.Block.Number < minimum || receipt.Block.Number > maximum {
		return fmt.Errorf("%s is failed or outside [%d,%d]", label, minimum, maximum)
	}
	return verifyFinalArtifact(label+" proof", receipt.Proof, "evm-receipt")
}

func verifyFinalHead(label string, head ChainHead) error {
	if head.Number == 0 {
		return fmt.Errorf("%s block number is zero", label)
	}
	return requireFinalHex32(label+" hash", head.Hash)
}

func verifyFinalArtifact(label string, locator FinalArtifactLocator, kind string) error {
	if locator.Kind != kind || locator.SizeBytes == 0 {
		return fmt.Errorf("%s kind/size is incomplete", label)
	}
	if err := requireFinalSHA256(label+" content hash", locator.ContentHash); err != nil {
		return err
	}
	if locator.URI == "" || strings.ContainsAny(locator.URI, "\\\r\n\x00") {
		return fmt.Errorf("%s URI is empty or unsafe", label)
	}
	parsed, err := url.Parse(locator.URI)
	if err != nil || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("%s URI is not credential-free and canonical", label)
	}
	if parsed.Scheme == "" {
		if strings.HasPrefix(locator.URI, "/") || path.Clean(locator.URI) != locator.URI || locator.URI == "." || strings.HasPrefix(locator.URI, "../") {
			return fmt.Errorf("%s relative URI is not canonical", label)
		}
		return nil
	}
	if parsed.Scheme != "https" && parsed.Scheme != "s3" && parsed.Scheme != "minio" {
		return fmt.Errorf("%s URI scheme %q is unsupported", label, parsed.Scheme)
	}
	if parsed.RawQuery != "" {
		values, queryErr := url.ParseQuery(parsed.RawQuery)
		canonicalQuery := "hash=" + url.QueryEscape(locator.ContentHash)
		literalQuery := "hash=" + locator.ContentHash
		if parsed.Scheme != "https" || queryErr != nil || len(values) != 1 || len(values["hash"]) != 1 || values.Get("hash") != locator.ContentHash || (parsed.RawQuery != canonicalQuery && parsed.RawQuery != literalQuery) {
			return fmt.Errorf("%s URI query is not the exact content hash", label)
		}
	}
	if parsed.Host == "" || parsed.Path == "" || path.Clean(parsed.Path) != parsed.Path {
		return fmt.Errorf("%s URI host/path is incomplete or non-canonical", label)
	}
	return nil
}

func requireFinalHex32(label, value string) error {
	if value == "" || value != strings.ToLower(value) || !strings.HasPrefix(value, "0x") {
		return fmt.Errorf("%s is not canonical lowercase 32-byte hex", label)
	}
	if _, err := decodeHex32(label, value); err != nil {
		return err
	}
	return nil
}

func requireFinalEVMAddress(label, value string) error {
	if len(value) != 42 || !strings.HasPrefix(value, "0x") || value != strings.ToLower(value) {
		return fmt.Errorf("%s is not a canonical lowercase EVM address", label)
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil || len(decoded) != 20 {
		return fmt.Errorf("%s is not a 20-byte EVM address", label)
	}
	allZero := true
	for _, item := range decoded {
		if item != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return fmt.Errorf("%s is the zero address", label)
	}
	return nil
}

func requireFinalSHA256(label, value string) error {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 || value != strings.ToLower(value) {
		return fmt.Errorf("%s is not canonical sha256", label)
	}
	if _, err := decodeHex32(label, "0x"+strings.TrimPrefix(value, "sha256:")); err != nil {
		return err
	}
	return nil
}

func finalNonnegativeInteger(label, encoded string) (*big.Int, error) {
	value, ok := new(big.Int).SetString(encoded, 10)
	if !ok || value.Sign() < 0 || value.String() != encoded {
		return nil, fmt.Errorf("%s %q is not a canonical non-negative integer", label, encoded)
	}
	return value, nil
}

func finalSignedInteger(label, encoded string) (*big.Int, error) {
	value, ok := new(big.Int).SetString(encoded, 10)
	if !ok || value.String() != encoded {
		return nil, fmt.Errorf("%s %q is not a canonical integer", label, encoded)
	}
	return value, nil
}

func finalPositiveInteger(label, encoded string) (*big.Int, error) {
	value, err := finalNonnegativeInteger(label, encoded)
	if err != nil {
		return nil, err
	}
	if value.Sign() == 0 {
		return nil, fmt.Errorf("%s is zero", label)
	}
	return value, nil
}

func finalPositiveRational(label string, encoded FinalRational) (*big.Rat, error) {
	numerator, err := finalPositiveInteger(label+" numerator", encoded.Numerator)
	if err != nil {
		return nil, err
	}
	denominator, err := finalPositiveInteger(label+" denominator", encoded.Denominator)
	if err != nil {
		return nil, err
	}
	value := new(big.Rat).SetFrac(numerator, denominator)
	if value.Num().String() != encoded.Numerator || value.Denom().String() != encoded.Denominator {
		return nil, fmt.Errorf("%s is not canonically reduced", label)
	}
	return value, nil
}

func finalProtocolRational(encoded FinalRational) (protocol.Rational, error) {
	numerator, err := finalPositiveInteger("theta numerator", encoded.Numerator)
	if err != nil || !numerator.IsUint64() {
		return protocol.Rational{}, errors.New("theta numerator exceeds uint64")
	}
	denominator, err := finalPositiveInteger("theta denominator", encoded.Denominator)
	if err != nil || !denominator.IsUint64() {
		return protocol.Rational{}, errors.New("theta denominator exceeds uint64")
	}
	return protocol.Rational{Numerator: numerator.Uint64(), Denominator: denominator.Uint64()}, nil
}

func finalRationalFromBig(value *big.Rat) FinalRational {
	return FinalRational{Numerator: value.Num().String(), Denominator: value.Denom().String()}
}

func finalCandidateUIDs(candidates []FinalHeadCandidateEvidence) ([]uint16, []uint16) {
	selected, rejected := []uint16{}, []uint16{}
	for _, candidate := range candidates {
		if candidate.Selected {
			selected = append(selected, candidate.UID)
		} else {
			rejected = append(rejected, candidate.UID)
		}
	}
	return selected, rejected
}

// Mirrors CRv4's deterministic post-rounding repair. The public exact capping
// and normalization functions reconstruct all prior steps; this final integer
// adjustment is reproduced here so offline evidence matches signed bytes even
// when max_weight_limit is below 65535.
func finalRepairMaxWeightLimitU16(uids, values []uint16, limit uint16) error {
	if len(uids) != len(values) || limit == 0 {
		return errors.New("invalid u16 cap repair inputs")
	}
	if len(values) == 0 || limit == crv4.U16Max {
		return nil
	}
	var maximum uint16
	var sum uint64
	positive := 0
	for _, value := range values {
		sum += uint64(value)
		if value > 0 {
			positive++
		}
		if value > maximum {
			maximum = value
		}
	}
	if maximum == 0 {
		return nil
	}
	if uint64(limit)*uint64(positive) < uint64(crv4.U16Max) {
		return errors.New("max weight limit is infeasible")
	}
	left, right := uint64(maximum)*uint64(crv4.U16Max), sum*uint64(limit)
	if left <= right {
		return nil
	}
	requiredSum := (left + uint64(limit) - 1) / uint64(limit)
	deficit := requiredSum - sum
	indices := make([]int, 0, len(values))
	for i, value := range values {
		if value != 0 && value != maximum {
			indices = append(indices, i)
		}
	}
	sort.SliceStable(indices, func(i, j int) bool { return uids[indices[i]] < uids[indices[j]] })
	for _, index := range indices {
		increment := uint64(maximum - values[index])
		if increment > deficit {
			increment = deficit
		}
		values[index] += uint16(increment)
		deficit -= increment
		if deficit == 0 {
			break
		}
	}
	if deficit != 0 {
		return errors.New("max weight limit rounding repair could not satisfy cap")
	}
	return nil
}

func finalMarkdown(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "`", "\\`")
	return value
}
