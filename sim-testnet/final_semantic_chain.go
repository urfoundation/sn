package main

// final_semantic_chain.go defines the public archive-RPC seam for FINAL.md.
// Implementations must decode public Substrate/EVM responses, return the
// semantic state below, and retain every exact JSON-RPC exchange. The verifier
// compares decoded state to the independently reconstructed evidence and binds
// the raw request/result transcript by hash.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
)

// v8 adds direct archive reads of the temporary fleet-refresh oracle at both
// observer checkpoints. Earlier transcripts can replay scheduling receipts
// without proving that the batcher was ever the active oracle.
const finalPublicChainVerificationSchema = "urnetwork-final-public-chain-verification-v8"

type FinalOperatorEvidenceOrigin struct {
	OperatorNoID int    `json:"operator_no_id"`
	ManifestURI  string `json:"manifest_uri"`
}

type FinalRPCExchange struct {
	Sequence     uint64          `json:"sequence"`
	Chain        string          `json:"chain"`
	Method       string          `json:"method"`
	Params       json.RawMessage `json:"params"`
	PinnedHead   ChainHead       `json:"pinned_head"`
	Result       json.RawMessage `json:"result"`
	RequestHash  string          `json:"request_hash"`
	ResponseHash string          `json:"response_hash"`
}

type FinalPublicChainVerification struct {
	Schema                   string `json:"schema"`
	SubstrateRPC             string `json:"substrate_rpc"`
	EVMRPC                   string `json:"evm_rpc"`
	EvidenceTransportProfile string `json:"evidence_transport_profile"`
	// EvidenceURI is the immutable authenticated deployment-manifest discovery
	// URI. It is not the semantic object's own URI, which is assigned only by
	// the outer completion/archive and therefore cannot be embedded without a
	// content-hash cycle.
	EvidenceURI             string                          `json:"evidence_uri"`
	OperatorEvidenceOrigins []FinalOperatorEvidenceOrigin   `json:"operator_evidence_origins"`
	PublicManifestHash      string                          `json:"public_manifest_hash"`
	FleetAudit              FinalPublicFleetAudit           `json:"ordinary_fleet_audit"`
	FleetGenerationAudit    FinalPublicFleetGenerationAudit `json:"ordinary_fleet_generation_audit"`
	ChronologyAudit         FinalPublicChronologyAudit      `json:"coordinator_chronology_audit"`
	NativePayoutAudit       FinalPublicNativePayoutAudit    `json:"native_payout_audit"`
	NativePayouts           []FinalNativeEpochPayoutState   `json:"native_payouts"`
	Exchanges               []FinalRPCExchange              `json:"exchanges"`
	TranscriptHash          string                          `json:"transcript_hash"`
}

type FinalNativeUIDState struct {
	UID               uint16 `json:"uid"`
	Hotkey            string `json:"hotkey"`
	Coldkey           string `json:"coldkey"`
	Registered        bool   `json:"registered"`
	StakeRao          string `json:"stake_rao"`
	ValidatorPermit   bool   `json:"validator_permit"`
	ValidatorTrustU16 uint16 `json:"validator_trust_u16"`
}

type FinalNativeEventState struct {
	ExtrinsicHash string    `json:"extrinsic_hash,omitempty"`
	Block         ChainHead `json:"block"`
	Success       bool      `json:"success"`
	Event         string    `json:"event"`
}

type FinalNativeWeightState struct {
	ValidatorUID    uint16    `json:"validator_uid"`
	ValidatorHotkey string    `json:"validator_hotkey"`
	LastUpdate      uint64    `json:"last_update"`
	UIDs            []uint16  `json:"uids"`
	Values          []uint16  `json:"values"`
	Block           ChainHead `json:"block"`
}

type FinalNativeRewardState struct {
	UID          uint16    `json:"uid"`
	StakeRao     string    `json:"stake_rao"`
	EmissionRao  string    `json:"emission_rao"`
	IncentiveU16 uint16    `json:"incentive_u16"`
	DividendsU16 uint16    `json:"dividends_u16"`
	Block        ChainHead `json:"block"`
}

type FinalNativeOwnerStakeState struct {
	HotkeyPublicKey  string    `json:"hotkey_public_key"`
	ColdkeyPublicKey string    `json:"coldkey_public_key"`
	StakeRao         string    `json:"stake_rao"`
	Block            ChainHead `json:"block"`
}

type FinalNativeFleetCommitmentState struct {
	Hotkey          string    `json:"hotkey"`
	CommitmentHash  string    `json:"commitment_hash"`
	CommitmentBlock uint64    `json:"commitment_block"`
	Block           ChainHead `json:"block"`
}

type FinalFleetMirrorChainState struct {
	Hotkey             string    `json:"hotkey"`
	CommitmentHash     string    `json:"commitment_hash"`
	FinalizedBlock     uint64    `json:"finalized_block"`
	FinalizedBlockHash string    `json:"finalized_block_hash"`
	Block              ChainHead `json:"block"`
}

type FinalFleetBindingChainState struct {
	Active         bool      `json:"active"`
	ClientID       string    `json:"client_id"`
	FleetID        string    `json:"fleet_id"`
	Hotkey         string    `json:"hotkey"`
	ClientKey      string    `json:"client_key"`
	CommitmentHash string    `json:"commitment_hash"`
	Generation     uint64    `json:"generation"`
	ValidFromEpoch uint64    `json:"valid_from_epoch"`
	ValidToEpoch   uint64    `json:"valid_to_epoch"`
	CleanedAtEpoch uint64    `json:"cleaned_at_epoch"`
	UID            uint16    `json:"uid"`
	Cleaned        bool      `json:"cleaned"`
	Block          ChainHead `json:"block"`
}

type FinalFleetLifecycleEventState struct {
	Kind               string    `json:"kind"`
	TransactionHash    string    `json:"transaction_hash"`
	Block              ChainHead `json:"block"`
	Hotkey             string    `json:"hotkey,omitempty"`
	CommitmentHash     string    `json:"commitment_hash,omitempty"`
	FinalizedBlock     uint64    `json:"finalized_block,omitempty"`
	FinalizedBlockHash string    `json:"finalized_block_hash,omitempty"`
	ClientID           string    `json:"client_id,omitempty"`
	FleetID            string    `json:"fleet_id,omitempty"`
	Generation         uint64    `json:"generation,omitempty"`
	UID                uint16    `json:"uid,omitempty"`
	ValidFromEpoch     uint64    `json:"valid_from_epoch,omitempty"`
	ValidToEpoch       uint64    `json:"valid_to_epoch,omitempty"`
	CleanedAtEpoch     uint64    `json:"cleaned_at_epoch,omitempty"`
}

type FinalEVMReceiptState struct {
	TransactionHash string    `json:"transaction_hash"`
	Block           ChainHead `json:"block"`
	Status          string    `json:"status"`
	LogsHash        string    `json:"logs_hash"`
	receiptPayload  *finalSemanticReceiptPayload
}

// Carries one historical transaction's target, exact input bytes, and every
// release-contract log retained by its receipt. It is deliberately separate
// from generic economic receipt decoding because fleet setup emits coordinator
// and batcher events whose semantics belong to the generation lineage.
type FinalFleetGenerationEVMWriteState struct {
	TransactionHash string                 `json:"transaction_hash"`
	To              string                 `json:"to"`
	Calldata        string                 `json:"calldata"`
	Block           ChainHead              `json:"block"`
	Status          string                 `json:"status"`
	Logs            []finalCanonicalEVMLog `json:"logs"`
}

// Carries the immutable coordinator ledger value at a validator's pinned EVM
// snapshot. It proves the complete prefix of deposits
// that the validator used for its demand-audit decision; a receipt alone can
// prove only the last transaction in that prefix.
type FinalEpochDepositChainState struct {
	Epoch     uint64    `json:"epoch"`
	NoID      uint64    `json:"no_id"`
	AmountRao string    `json:"amount_rao"`
	Block     ChainHead `json:"block"`
}

// Carries the terminal immutable coordinator version selected by the
// evidence's effective epoch. It binds the operator
// pool's active deposit/root authorities as well as its retained version
// count, rather than relying on a receipt hash alone.
type FinalOperatorVersionChainState struct {
	NoID           uint64    `json:"no_id"`
	VersionCount   uint64    `json:"version_count"`
	Coldkey        string    `json:"coldkey"`
	PoolHotkey     string    `json:"pool_hotkey"`
	DepositHotkey  string    `json:"deposit_hotkey"`
	DepositSigner  string    `json:"deposit_signer"`
	RootSigner     string    `json:"root_signer"`
	EffectiveEpoch uint64    `json:"effective_epoch"`
	Active         bool      `json:"active"`
	Block          ChainHead `json:"block"`
}

// Carries the exact cumulative coordinator conviction at one validator
// decision snapshot. It is intentionally separate
// from a Deposit receipt because a receipt cannot establish the complete
// prefix selected by a validator.
type FinalCoordinatorConvictionChainState struct {
	NoID          uint64    `json:"no_id"`
	ConvictionRao string    `json:"conviction_rao"`
	Block         ChainHead `json:"block"`
}

// Carries the amount added to the cumulative conviction ledger during one
// settlement epoch. The validator's
// ConvictionBeforeRao intentionally excludes both this value and the epoch's
// demand deposits, so all three values are necessary to independently replay
// its signed tier selection. Epoch zero is a valid contract epoch.
type FinalEpochConvictionAddedChainState struct {
	Epoch     uint64    `json:"epoch"`
	NoID      uint64    `json:"no_id"`
	AmountRao string    `json:"amount_rao"`
	Block     ChainHead `json:"block"`
}

// Carries the reserve's independently recorded principal for one operator at
// the same immutable decision
// snapshot. Matching it to coordinator conviction prevents a substituted NO
// or invented conviction tier from receiving weight.
type FinalReserveOperatorPrincipalChainState struct {
	NoID         uint64    `json:"no_id"`
	PrincipalRao string    `json:"principal_rao"`
	Block        ChainHead `json:"block"`
}

// Carries one operator's carry ledger at a transition boundary. Per-operator
// reads avoid accepting a valid total from another
// pool.
type FinalVaultCarryChainState struct {
	NoID     uint64    `json:"no_id"`
	CarryRao string    `json:"carry_rao"`
	Block    ChainHead `json:"block"`
}

// Binds one represented Merkle leaf and its claim-once key. The payout leaf is
// keyed by coldkey/shareBPS, whereas the
// claim-once key is keccak256(abi.encode(noID,coldkey)); they intentionally
// differ and both must be checked.
type FinalVaultClaimChainState struct {
	Epoch       uint64    `json:"epoch"`
	NoID        uint64    `json:"no_id"`
	Coldkey     string    `json:"coldkey"`
	ShareBPS    uint64    `json:"share_bps"`
	PayoutLeaf  string    `json:"payout_leaf"`
	ClaimKey    string    `json:"claim_key"`
	LeafClaimed bool      `json:"leaf_claimed"`
	Block       ChainHead `json:"block"`
}

// Carries a coldkey's cumulative claim credit at an immutable boundary. It is
// kept separate from a per-leaf read because a
// later claim or withdrawal can settle credit created by an earlier leaf.
type FinalVaultClaimCreditChainState struct {
	Coldkey   string    `json:"coldkey"`
	CreditRao string    `json:"credit_rao"`
	Block     ChainHead `json:"block"`
}

// Authenticates the proxy dispatch target and both executable byte streams at
// one historical head. The slot is kept
// as its complete 32-byte value so a non-address prefix cannot be discarded.
type FinalCoordinatorRuntimeChainState struct {
	CoordinatorProxy           string                    `json:"coordinator_proxy"`
	CoordinatorImplementation  string                    `json:"coordinator_implementation"`
	ObservedImplementationSlot string                    `json:"observed_implementation_slot"`
	ProxyCodeHash              string                    `json:"proxy_code_hash"`
	ImplementationCodeHash     string                    `json:"implementation_code_hash"`
	RuntimeRoots               []FinalReleaseRuntimeRoot `json:"runtime_roots"`
	Block                      ChainHead                 `json:"block"`
}

type FinalPoolEpochChainState struct {
	Epoch        uint64    `json:"epoch"`
	NoID         uint64    `json:"no_id"`
	PayoutRoot   string    `json:"payout_root,omitempty"`
	ArtifactHash string    `json:"artifact_hash,omitempty"`
	FundedRao    string    `json:"funded_rao"`
	TotalRao     string    `json:"total_rao"`
	ClaimedRao   string    `json:"claimed_rao"`
	ExpiryBlock  uint64    `json:"expiry_block"`
	Status       uint8     `json:"status"`
	Block        ChainHead `json:"block"`
}

type FinalContractDeploymentState struct {
	CoordinatorProxy                  string    `json:"coordinator_proxy"`
	CoordinatorImplementation         string    `json:"coordinator_implementation"`
	SettlementVault                   string    `json:"settlement_vault"`
	ReserveSink                       string    `json:"reserve_sink"`
	GovernanceOwner                   string    `json:"governance_owner"`
	CoordinatorNetuid                 uint16    `json:"coordinator_netuid"`
	CoordinatorSelfColdkey            string    `json:"coordinator_self_coldkey"`
	CoordinatorSettlementVault        string    `json:"coordinator_settlement_vault"`
	CoordinatorReserveSink            string    `json:"coordinator_reserve_sink"`
	CoordinatorGuardian               string    `json:"coordinator_guardian"`
	CoordinatorActiveGuardian         string    `json:"coordinator_active_guardian"`
	CoordinatorPaused                 bool      `json:"coordinator_paused"`
	CoordinatorCommitmentOracle       string    `json:"coordinator_commitment_oracle"`
	CoordinatorActiveCommitmentOracle string    `json:"coordinator_active_commitment_oracle"`
	VaultCoordinator                  string    `json:"vault_coordinator"`
	VaultNetuid                       uint16    `json:"vault_netuid"`
	VaultSelfColdkey                  string    `json:"vault_self_coldkey"`
	VaultEscrowHotkey                 string    `json:"vault_escrow_hotkey"`
	VaultEscrowRegistered             bool      `json:"vault_escrow_registered"`
	VaultMinimumClaimTTLBlocks        uint64    `json:"vault_minimum_claim_ttl_blocks"`
	VaultMinimumTransferTaoRao        uint64    `json:"vault_minimum_transfer_tao_rao"`
	ReserveRecorder                   string    `json:"reserve_recorder"`
	ReserveNetuid                     uint16    `json:"reserve_netuid"`
	ReserveSelfColdkey                string    `json:"reserve_self_coldkey"`
	ReserveHotkey                     string    `json:"reserve_hotkey"`
	CoordinatorProxyCodeHash          string    `json:"coordinator_proxy_code_hash"`
	ImplementationCodeHash            string    `json:"implementation_code_hash"`
	SettlementVaultCodeHash           string    `json:"settlement_vault_code_hash"`
	ReserveSinkCodeHash               string    `json:"reserve_sink_code_hash"`
	ObservedImplementationSlot        string    `json:"observed_implementation_slot"`
	PolicyHash                        string    `json:"policy_hash"`
	PolicyVersion                     uint64    `json:"policy_version"`
	PolicyEffectiveEpoch              uint64    `json:"policy_effective_epoch"`
	PolicyEffectiveBlock              uint64    `json:"policy_effective_block"`
	Block                             ChainHead `json:"block"`
}

type FinalReserveState struct {
	PrincipalRao string    `json:"principal_rao"`
	LiveStakeRao string    `json:"live_stake_rao"`
	Block        ChainHead `json:"block"`
}

type FinalSettlementVaultChainState struct {
	TotalCapturedRao        string    `json:"total_captured_rao"`
	TotalPaidRao            string    `json:"total_paid_rao"`
	EscrowAccountedRao      string    `json:"escrow_accounted_rao"`
	PendingFundingRao       string    `json:"pending_funding_rao"`
	OutstandingLiabilityRao string    `json:"outstanding_liability_rao"`
	LiveEscrowStakeRao      string    `json:"live_escrow_stake_rao"`
	Block                   ChainHead `json:"block"`
}

// FinalSemanticChainReader is intentionally semantic and read-only. Every
// method must execute public archive JSON-RPC calls at exactly at.Hash, verify
// the canonical number/hash pair, decode the returned storage/events, and
// return all underlying exchanges. Archive absence and pruned state are errors.
type FinalSemanticChainReader interface {
	Endpoints() (substrateRPC, evmRPC, evidenceURI string)
	PublicManifestHash() string
	CanonicalSubstrateHead(context.Context, ChainHead) ([]FinalRPCExchange, error)
	CanonicalEVMHead(context.Context, ChainHead) ([]FinalRPCExchange, error)
	NativeUID(context.Context, uint16, uint16, ChainHead) (FinalNativeUIDState, []FinalRPCExchange, error)
	NativeEvent(context.Context, FinalNativeReceipt, string) (FinalNativeEventState, []FinalRPCExchange, error)
	NativeWeights(context.Context, uint16, uint16, ChainHead) (FinalNativeWeightState, []FinalRPCExchange, error)
	NativeReward(context.Context, uint16, uint16, ChainHead) (FinalNativeRewardState, []FinalRPCExchange, error)
	NativeOwnerStake(context.Context, string, string, ChainHead) (FinalNativeOwnerStakeState, []FinalRPCExchange, error)
	EVMReceipt(context.Context, FinalEVMReceipt) (FinalEVMReceiptState, []FinalRPCExchange, error)
	EpochDeposit(context.Context, uint64, uint64, ChainHead) (FinalEpochDepositChainState, []FinalRPCExchange, error)
	EpochConvictionAdded(context.Context, uint64, uint64, ChainHead) (FinalEpochConvictionAddedChainState, []FinalRPCExchange, error)
	OperatorVersion(context.Context, uint64, uint64, ChainHead) (FinalOperatorVersionChainState, []FinalRPCExchange, error)
	CoordinatorConviction(context.Context, uint64, ChainHead) (FinalCoordinatorConvictionChainState, []FinalRPCExchange, error)
	ReserveOperatorPrincipal(context.Context, uint64, ChainHead) (FinalReserveOperatorPrincipalChainState, []FinalRPCExchange, error)
	VaultCarry(context.Context, uint64, ChainHead) (FinalVaultCarryChainState, []FinalRPCExchange, error)
	VaultClaim(context.Context, uint64, uint64, string, uint64, ChainHead) (FinalVaultClaimChainState, []FinalRPCExchange, error)
	VaultClaimCredit(context.Context, string, ChainHead) (FinalVaultClaimCreditChainState, []FinalRPCExchange, error)
	CoordinatorRuntime(context.Context, ChainHead) (FinalCoordinatorRuntimeChainState, []FinalRPCExchange, error)
	PoolEpoch(context.Context, uint64, uint64, ChainHead) (FinalPoolEpochChainState, []FinalRPCExchange, error)
	ContractDeployment(context.Context, ChainHead) (FinalContractDeploymentState, []FinalRPCExchange, error)
	SettlementVaultState(context.Context, ChainHead) (FinalSettlementVaultChainState, []FinalRPCExchange, error)
	ReserveState(context.Context, ChainHead) (FinalReserveState, []FinalRPCExchange, error)
}

// FinalSemanticOperatorOriginReader exposes the two authenticated deployment
// manifest locations. Keeping this separate from the RPC seam makes an older
// reader fail closed at runtime instead of silently inventing a second origin.
type FinalSemanticOperatorOriginReader interface {
	OperatorEvidenceOrigins() []FinalOperatorEvidenceOrigin
}

// FinalSemanticLifecycleChainReader is required whenever the semantic object
// carries lifecycle evidence. Keeping these expensive historical surfaces
// separate prevents non-lifecycle production evidence from silently receiving
// placeholder results while still making lifecycle replay fail closed.
type FinalSemanticLifecycleChainReader interface {
	NativePruneSnapshot(context.Context, uint16, ChainHead) (FleetLifecyclePruneSnapshot, []FinalRPCExchange, error)
	NativeFleetCommitment(context.Context, uint16, string, ChainHead) (FinalNativeFleetCommitmentState, []FinalRPCExchange, error)
	FleetMirror(context.Context, string, ChainHead) (FinalFleetMirrorChainState, []FinalRPCExchange, error)
	FleetBinding(context.Context, string, uint64, ChainHead) (FinalFleetBindingChainState, []FinalRPCExchange, error)
	FleetBindingRecord(context.Context, string, ChainHead) (FinalFleetBindingChainState, []FinalRPCExchange, error)
	FleetMemberCount(context.Context, string, ChainHead) (uint64, []FinalRPCExchange, error)
	FleetLifecycleEvents(context.Context, string, ChainHead) ([]FinalFleetLifecycleEventState, []FinalRPCExchange, error)
}

// Provides exact historical transaction payloads for the fixed ordinary-fleet
// lineage. The separate capability prevents a reader that only knows generic
// deposit/payout receipts from silently skipping coordinator/batcher writes.
type FinalSemanticFleetGenerationChainReader interface {
	FleetGenerationEVMWrite(context.Context, FinalFleetGenerationWriteEvidence) (FinalFleetGenerationEVMWriteState, []FinalRPCExchange, error)
}

// SealFinalSemanticEvidenceOnChain is the only path that makes evidence
// renderable. It runs all public historical checks and embeds their exact
// request/result transcript before recomputing EvidenceHash.
func SealFinalSemanticEvidenceOnChain(ctx context.Context, draft *FinalSemanticEvidence, reader FinalSemanticChainReader) (*FinalSemanticEvidence, error) {
	if draft == nil || reader == nil || ctx == nil {
		return nil, errors.New("public final semantic verifier is unavailable")
	}
	b, err := json.Marshal(draft)
	if err != nil {
		return nil, err
	}
	var copy FinalSemanticEvidence
	if err := json.Unmarshal(b, &copy); err != nil {
		return nil, err
	}
	copy.PublicVerification = nil
	copy.EvidenceHash = ""
	canonicalizeFinalSemanticEvidence(&copy)
	if err := verifyFinalSemanticEvidence(&copy, false); err != nil {
		return nil, err
	}
	verification, err := executeFinalSemanticOnChain(ctx, &copy, reader)
	if err != nil {
		return nil, err
	}
	copy.PublicVerification = verification
	allRequests := make([]string, len(verification.Exchanges))
	for i, exchange := range verification.Exchanges {
		allRequests[i] = exchange.RequestHash
	}
	sort.Strings(allRequests)
	allRequests = slices.Compact(allRequests)
	for i := range copy.ExitCriteria {
		if len(copy.ExitCriteria[i].PublicRequestHashes) == 0 {
			copy.ExitCriteria[i].PublicRequestHashes = append([]string(nil), allRequests...)
		}
	}
	hash, err := finalSemanticEvidenceHash(&copy)
	if err != nil {
		return nil, err
	}
	copy.EvidenceHash = hash
	if err := VerifyFinalSemanticEvidence(&copy); err != nil {
		return nil, err
	}
	return &copy, nil
}

// Makes one strict detached copy before an extension point receives evidence.
// Reader factories may retain their argument, so the caller's verification
// snapshot must never share maps, slices, or nested pointers with that input.
func finalSemanticEvidenceDetachedCopy(evidence *FinalSemanticEvidence) (*FinalSemanticEvidence, error) {
	if evidence == nil {
		return nil, errors.New("final semantic evidence snapshot is unavailable")
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return nil, err
	}
	var result FinalSemanticEvidence
	if err := decodeStrictJSONBytes(raw, &result); err != nil {
		return nil, fmt.Errorf("strict-decode final semantic verification snapshot: %w", err)
	}
	return &result, nil
}

// VerifyFinalSemanticEvidenceWithArtifactsOnChain seals one strict snapshot
// before constructing its public reader and replaying it. Artifact closure
// therefore happens before any public RPC work, and every reader method sees
// a detached factory copy while comparison remains against the artifact-
// verified snapshot.
func VerifyFinalSemanticEvidenceWithArtifactsOnChain(ctx context.Context, evidence *FinalSemanticEvidence, loader FinalArtifactLoader, newReader FinalSemanticChainReaderFactory) error {
	if ctx == nil || evidence == nil || loader == nil || newReader == nil {
		return errors.New("final semantic combined verification inputs are incomplete")
	}
	snapshot, err := finalSemanticEvidenceDetachedCopy(evidence)
	if err != nil {
		return err
	}
	if err := VerifyFinalSemanticArtifacts(ctx, snapshot, loader); err != nil {
		return err
	}
	factoryEvidence, err := finalSemanticEvidenceDetachedCopy(snapshot)
	if err != nil {
		return err
	}
	reader, err := newReader(ctx, factoryEvidence)
	if err != nil {
		return err
	}
	if reader == nil {
		return errors.New("final semantic combined verifier constructed no public reader")
	}
	return VerifyFinalSemanticEvidenceOnChain(ctx, snapshot, reader)
}

// VerifyFinalSemanticEvidenceOnChain replays an already artifact-verified
// evidence object through a public archive reader. Callers that have access
// to artifacts must use the combined verifier above so sealed source bytes
// and public RPC state are checked against one immutable snapshot.
func VerifyFinalSemanticEvidenceOnChain(ctx context.Context, evidence *FinalSemanticEvidence, reader FinalSemanticChainReader) error {
	if err := VerifyFinalSemanticEvidence(evidence); err != nil {
		return err
	}
	want := evidence.PublicVerification
	if want == nil {
		return errors.New("public chain verification transcript is missing")
	}
	got, err := executeFinalSemanticOnChain(ctx, evidence, reader)
	if err != nil {
		return err
	}
	if got.TranscriptHash != want.TranscriptHash {
		return fmt.Errorf("public chain transcript %s, embedded %s", got.TranscriptHash, want.TranscriptHash)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		return err
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		return err
	}
	if !slices.Equal(wantJSON, gotJSON) {
		return errors.New("public chain transcript differs despite hash comparison")
	}
	return nil
}

func executeFinalSemanticOnChain(ctx context.Context, evidence *FinalSemanticEvidence, reader FinalSemanticChainReader) (*FinalPublicChainVerification, error) {
	substrateRPC, evmRPC, manifestURI := reader.Endpoints()
	transportProfile, err := publicEvidenceTransportForURI(manifestURI, evidence.ChainID, evidence.GenesisHash)
	if err != nil {
		return nil, fmt.Errorf("public deployment manifest transport: %w", err)
	}
	originReader, ok := reader.(FinalSemanticOperatorOriginReader)
	if !ok {
		return nil, errors.New("public semantic reader does not expose two authenticated operator evidence origins")
	}
	origins := originReader.OperatorEvidenceOrigins()
	fleetAudit, err := finalPublicFleetAuditForEvidence(evidence)
	if err != nil {
		return nil, fmt.Errorf("public ordinary fleet audit projection: %w", err)
	}
	var fleetGenerationAudit FinalPublicFleetGenerationAudit
	if evidence.FleetGeneration != nil {
		fleetGenerationAudit, err = finalPublicFleetGenerationAuditForEvidence(evidence)
		if err != nil {
			return nil, fmt.Errorf("public ordinary fleet generation audit projection: %w", err)
		}
	}
	nativePayoutAudit, err := finalPublicNativePayoutAuditForEvidence(evidence)
	if err != nil {
		return nil, fmt.Errorf("public native payout audit projection: %w", err)
	}
	currentRuntimeHeads, err := finalSemanticCurrentRuntimeHeads(evidence)
	if err != nil {
		return nil, fmt.Errorf("public coordinator runtime head projection: %w", err)
	}
	chronologyAudit, err := finalPublicChronologyAuditForEvidence(evidence, currentRuntimeHeads)
	if err != nil {
		return nil, fmt.Errorf("public coordinator chronology projection: %w", err)
	}
	verification := &FinalPublicChainVerification{Schema: finalPublicChainVerificationSchema, SubstrateRPC: substrateRPC, EVMRPC: evmRPC, EvidenceTransportProfile: transportProfile, EvidenceURI: manifestURI, OperatorEvidenceOrigins: origins, PublicManifestHash: reader.PublicManifestHash(), FleetAudit: fleetAudit, FleetGenerationAudit: fleetGenerationAudit, ChronologyAudit: chronologyAudit, NativePayoutAudit: nativePayoutAudit}
	appendExchanges := func(chain string, head ChainHead, exchanges []FinalRPCExchange) error {
		if len(exchanges) == 0 {
			return fmt.Errorf("%s public RPC verification at %d returned no transcript", chain, head.Number)
		}
		for _, exchange := range exchanges {
			if exchange.Chain != chain || exchange.PinnedHead != head {
				return fmt.Errorf("%s transcript is not pinned to %d/%s", chain, head.Number, head.Hash)
			}
			exchange.Sequence = uint64(len(verification.Exchanges) + 1)
			exchange.RequestHash = ""
			exchange.ResponseHash = ""
			requestHash, responseHash, err := finalRPCExchangeHashes(exchange)
			if err != nil {
				return err
			}
			exchange.RequestHash, exchange.ResponseHash = requestHash, responseHash
			verification.Exchanges = append(verification.Exchanges, exchange)
		}
		return nil
	}

	nativeHeads, evmHeads, err := finalSemanticHeads(evidence)
	if err != nil {
		return nil, err
	}
	for _, head := range nativeHeads {
		exchanges, err := reader.CanonicalSubstrateHead(ctx, head)
		if err != nil {
			return nil, fmt.Errorf("public Substrate canonical head %d: %w", head.Number, err)
		}
		if err := appendExchanges("substrate", head, exchanges); err != nil {
			return nil, err
		}
	}
	for _, head := range evmHeads {
		exchanges, err := reader.CanonicalEVMHead(ctx, head)
		if err != nil {
			return nil, fmt.Errorf("public EVM canonical head %d: %w", head.Number, err)
		}
		if err := appendExchanges("evm", head, exchanges); err != nil {
			return nil, err
		}
	}
	if err := verifyFinalSemanticCoordinatorRuntimes(ctx, evidence, reader, currentRuntimeHeads, appendExchanges); err != nil {
		return nil, fmt.Errorf("public historical coordinator runtime audit: %w", err)
	}
	if err := verifyFinalSemanticChronologyOnChain(ctx, evidence, reader, chronologyAudit, appendExchanges); err != nil {
		return nil, fmt.Errorf("public coordinator chronology replay: %w", err)
	}
	if err := verifyFinalSemanticFleetGenerationOnChain(ctx, evidence, reader, appendExchanges); err != nil {
		return nil, fmt.Errorf("public ordinary fleet generation replay: %w", err)
	}
	if err := executeFinalSemanticLifecycleOnChain(ctx, evidence, reader, appendExchanges); err != nil {
		return nil, fmt.Errorf("public fleet lifecycle replay: %w", err)
	}
	if lifecycle := evidence.FleetLifecycle; lifecycle != nil {
		for _, item := range lifecycle.PayoutArtifacts {
			receipt, exchanges, err := reader.EVMReceipt(ctx, item.Root)
			if err != nil {
				return nil, fmt.Errorf("public fleet lifecycle payout root %d/%d: %w", item.Epoch, item.NoID, err)
			}
			if err := appendExchanges("evm", item.Root.Block, exchanges); err != nil {
				return nil, err
			}
			if receipt.TransactionHash != item.Root.TransactionHash || receipt.Block != item.Root.Block || receipt.Status != item.Root.Status || receipt.LogsHash != item.Root.LogsHash {
				return nil, fmt.Errorf("public fleet lifecycle payout root %d/%d receipt differs", item.Epoch, item.NoID)
			}
			var indexed *FleetLifecyclePayoutEvidence
			for index := range lifecycle.State.Payouts {
				candidate := &lifecycle.State.Payouts[index]
				if candidate.Epoch == item.Epoch && uint64(candidate.NoID) == item.NoID {
					indexed = candidate
					break
				}
			}
			if indexed == nil {
				return nil, fmt.Errorf("public fleet lifecycle payout root %d/%d lacks terminal index", item.Epoch, item.NoID)
			}
			state, exchanges, err := reader.PoolEpoch(ctx, item.Epoch, item.NoID, evidence.EVMTerminalHead)
			if err != nil {
				return nil, fmt.Errorf("public fleet lifecycle payout state %d/%d: %w", item.Epoch, item.NoID, err)
			}
			if err := appendExchanges("evm", evidence.EVMTerminalHead, exchanges); err != nil {
				return nil, err
			}
			if state.Epoch != item.Epoch || state.NoID != item.NoID || state.Block != evidence.EVMTerminalHead || !strings.EqualFold(state.PayoutRoot, indexed.PayoutRoot) || !strings.EqualFold(state.ArtifactHash, "0x"+strings.TrimPrefix(indexed.ContentHash, "sha256:")) {
				return nil, fmt.Errorf("public fleet lifecycle payout state %d/%d differs", item.Epoch, item.NoID)
			}
		}
	}
	for _, pool := range evidence.Pools {
		state, exchanges, err := reader.NativeUID(ctx, evidence.Netuid, pool.UID, pool.Snapshot)
		if err != nil {
			return nil, fmt.Errorf("public pool UID no=%d: %w", pool.NoID, err)
		}
		if err := appendExchanges("substrate", pool.Snapshot, exchanges); err != nil {
			return nil, err
		}
		if state.UID != pool.UID || state.Hotkey != pool.Hotkey || state.Coldkey != pool.Coldkey || state.Registered != pool.Registered {
			return nil, fmt.Errorf("public pool UID no=%d does not match ownership evidence", pool.NoID)
		}
		registrationState, registrationExchanges, err := reader.EVMReceipt(ctx, pool.Registration)
		if err != nil {
			return nil, fmt.Errorf("public pool registration receipt no=%d: %w", pool.NoID, err)
		}
		if err := appendExchanges("evm", pool.Registration.Block, registrationExchanges); err != nil {
			return nil, err
		}
		if registrationState.TransactionHash != pool.Registration.TransactionHash || registrationState.Block != pool.Registration.Block || registrationState.Status != pool.Registration.Status || registrationState.LogsHash != pool.Registration.LogsHash {
			return nil, fmt.Errorf("public pool registration receipt no=%d does not match evidence", pool.NoID)
		}
		receiptState, receiptExchanges, err := reader.EVMReceipt(ctx, pool.ConvictionReceipt)
		if err != nil {
			return nil, fmt.Errorf("public pool conviction receipt no=%d: %w", pool.NoID, err)
		}
		if err := appendExchanges("evm", pool.ConvictionReceipt.Block, receiptExchanges); err != nil {
			return nil, err
		}
		if receiptState.TransactionHash != pool.ConvictionReceipt.TransactionHash || receiptState.Block != pool.ConvictionReceipt.Block || receiptState.Status != pool.ConvictionReceipt.Status || receiptState.LogsHash != pool.ConvictionReceipt.LogsHash {
			return nil, fmt.Errorf("public pool conviction receipt no=%d does not match evidence", pool.NoID)
		}
		version, versionExchanges, err := reader.OperatorVersion(ctx, pool.NoID, pool.EffectiveEpoch, evidence.EVMTerminalHead)
		if err != nil {
			return nil, fmt.Errorf("public operator version no=%d: %w", pool.NoID, err)
		}
		if err := appendExchanges("evm", evidence.EVMTerminalHead, versionExchanges); err != nil {
			return nil, err
		}
		if err := verifyFinalSemanticPoolOperatorVersion(pool, version, evidence.EVMTerminalHead); err != nil {
			return nil, err
		}
	}
	for _, fleet := range evidence.HeadFleets {
		state, exchanges, err := reader.NativeUID(ctx, evidence.Netuid, fleet.UID, fleet.Snapshot)
		if err != nil {
			return nil, fmt.Errorf("public head fleet UID fleet=%d: %w", fleet.FleetID, err)
		}
		if err := appendExchanges("substrate", fleet.Snapshot, exchanges); err != nil {
			return nil, err
		}
		if state.UID != fleet.UID || state.Hotkey != fleet.Hotkey || state.Coldkey != fleet.Coldkey || state.Registered != fleet.Registered {
			return nil, fmt.Errorf("public head fleet UID fleet=%d does not match ownership evidence", fleet.FleetID)
		}
		if err := verifyFinalNativeEventOnChain(ctx, reader, fleet.Registration, "registration", appendExchanges); err != nil {
			return nil, fmt.Errorf("public head fleet registration fleet=%d: %w", fleet.FleetID, err)
		}
	}
	if err := verifyFinalSemanticFleetAudit(ctx, evidence, reader, appendExchanges); err != nil {
		return nil, fmt.Errorf("public ordinary head-fleet replay: %w", err)
	}
	for _, transition := range evidence.HeadTransitions {
		if err := verifyFinalNativeEventOnChain(ctx, reader, transition.Registration, "registration", appendExchanges); err != nil {
			return nil, fmt.Errorf("public challenger registration fleet=%d: %w", transition.ChallengerFleetID, err)
		}
		fleet := evidence.HeadFleets[transition.ChallengerFleetID-1]
		for _, checkpoint := range []struct {
			label string
			head  ChainHead
		}{{"operational", transition.Snapshot}, {"independent", transition.IndependentSnapshot}} {
			state, exchanges, err := reader.NativeUID(ctx, evidence.Netuid, transition.PromotedUID, checkpoint.head)
			if err != nil {
				return nil, fmt.Errorf("public challenger %s UID fleet=%d: %w", checkpoint.label, transition.ChallengerFleetID, err)
			}
			if err := appendExchanges("substrate", checkpoint.head, exchanges); err != nil {
				return nil, err
			}
			if state.UID != transition.PromotedUID || state.Hotkey != transition.PromotedHotkey || state.Coldkey != fleet.Coldkey || !state.Registered {
				return nil, fmt.Errorf("public challenger %s UID fleet=%d differs from its v4 checkpoint", checkpoint.label, transition.ChallengerFleetID)
			}
		}
	}
	validatorUID := map[uint64]uint16{}
	for _, validator := range evidence.Validators {
		validatorUID[validator.ValidatorID] = validator.UID
		state, exchanges, err := reader.NativeUID(ctx, evidence.Netuid, validator.UID, validator.Snapshot)
		if err != nil {
			return nil, fmt.Errorf("public validator UID %d: %w", validator.ValidatorID, err)
		}
		if err := appendExchanges("substrate", validator.Snapshot, exchanges); err != nil {
			return nil, err
		}
		if state.UID != validator.UID || state.Hotkey != validator.Hotkey || state.Coldkey != validator.Coldkey || !state.Registered || state.StakeRao != validator.StakeRao || state.ValidatorPermit != validator.ValidatorPermit || state.ValidatorTrustU16 != validator.ValidatorTrustU16 {
			return nil, fmt.Errorf("public validator %d registration/stake/permit/vtrust mismatch", validator.ValidatorID)
		}
		if err := verifyFinalNativeEventOnChain(ctx, reader, validator.Registration, "registration", appendExchanges); err != nil {
			return nil, fmt.Errorf("public validator registration %d: %w", validator.ValidatorID, err)
		}
		for cycleIndex := range validator.Cycles {
			cycle := &validator.Cycles[cycleIndex]
			for _, event := range []struct {
				kind    string
				receipt FinalNativeReceipt
			}{{"commit", cycle.Commit}, {"reveal", cycle.Reveal}, {"application", cycle.Application}} {
				if err := verifyFinalNativeEventOnChain(ctx, reader, event.receipt, event.kind, appendExchanges); err != nil {
					return nil, fmt.Errorf("public validator %d CRv4 %s: %w", validator.ValidatorID, event.kind, err)
				}
			}
			weights, exchanges, err := reader.NativeWeights(ctx, evidence.Netuid, validator.UID, cycle.Application.Block)
			if err != nil {
				return nil, fmt.Errorf("public validator %d applied weights: %w", validator.ValidatorID, err)
			}
			if err := appendExchanges("substrate", cycle.Application.Block, exchanges); err != nil {
				return nil, err
			}
			uids, values := finalSubmittedValues(cycle.Submitted)
			if cycle.Application.Call == nil {
				return nil, fmt.Errorf("public validator %d application has no exact native call identity", validator.ValidatorID)
			}
			if err := verifyFinalNativeApplicationState(weights, *cycle.Application.Call, cycle.Application.Block, validator.UID, validator.Hotkey, uids, values); err != nil {
				return nil, fmt.Errorf("public validator %d applied vector does not match fresh intent lineage: %w", validator.ValidatorID, err)
			}
			if err := verifyFinalSemanticCycleEpochDeposits(ctx, reader, *cycle, appendExchanges); err != nil {
				return nil, fmt.Errorf("public validator %d deposit-ledger audit: %w", validator.ValidatorID, err)
			}
			if err := verifyFinalSemanticCycleConvictionPrincipals(ctx, reader, *cycle, appendExchanges); err != nil {
				return nil, fmt.Errorf("public validator %d conviction/principal audit: %w", validator.ValidatorID, err)
			}
			for _, pool := range cycle.Pools {
				receipt := pool.DepositReceipt
				state, exchanges, err := reader.EVMReceipt(ctx, receipt)
				if err != nil {
					return nil, fmt.Errorf("public deposit receipt %s: %w", receipt.TransactionHash, err)
				}
				if err := appendExchanges("evm", receipt.Block, exchanges); err != nil {
					return nil, err
				}
				if state.TransactionHash != receipt.TransactionHash || state.Block != receipt.Block || state.Status != receipt.Status || state.LogsHash != receipt.LogsHash {
					return nil, fmt.Errorf("public deposit receipt %s does not match evidence", receipt.TransactionHash)
				}
			}
		}
	}
	nativePayouts, err := verifyFinalSemanticNativeEpochPayouts(ctx, evidence, reader, appendExchanges)
	if err != nil {
		return nil, fmt.Errorf("public exact native epoch payouts: %w", err)
	}
	verification.NativePayouts = nativePayouts
	if evidence.DishonestDeposit != nil {
		for _, stage := range []struct {
			name      string
			decisions []FinalDishonestDepositDecision
		}{{name: "penalty", decisions: evidence.DishonestDeposit.Penalties}, {name: "recovery", decisions: evidence.DishonestDeposit.Recoveries}} {
			for index := range stage.decisions {
				decision := &stage.decisions[index]
				for _, event := range []struct {
					kind    string
					receipt FinalNativeReceipt
				}{{"commit", decision.Cycle.Commit}, {"reveal", decision.Cycle.Reveal}, {"application", decision.Cycle.Application}} {
					if err := verifyFinalNativeEventOnChain(ctx, reader, event.receipt, event.kind, appendExchanges); err != nil {
						return nil, fmt.Errorf("public dishonest-deposit %s validator %d CRv4 %s: %w", stage.name, decision.ValidatorID, event.kind, err)
					}
				}
				weights, exchanges, err := reader.NativeWeights(ctx, evidence.Netuid, decision.ValidatorUID, decision.Cycle.Application.Block)
				if err != nil {
					return nil, fmt.Errorf("public dishonest-deposit %s validator %d weights: %w", stage.name, decision.ValidatorID, err)
				}
				if err := appendExchanges("substrate", decision.Cycle.Application.Block, exchanges); err != nil {
					return nil, err
				}
				uids, values := finalSubmittedValues(decision.Cycle.Submitted)
				if decision.Cycle.Application.Call == nil {
					return nil, fmt.Errorf("public dishonest-deposit %s validator %d application has no exact native call identity", stage.name, decision.ValidatorID)
				}
				validatorHotkey := ""
				for index := range evidence.Validators {
					if evidence.Validators[index].ValidatorID == decision.ValidatorID {
						validatorHotkey = evidence.Validators[index].Hotkey
					}
				}
				if validatorHotkey == "" {
					return nil, fmt.Errorf("public dishonest-deposit %s validator %d identity is absent", stage.name, decision.ValidatorID)
				}
				if err := verifyFinalNativeApplicationState(weights, *decision.Cycle.Application.Call, decision.Cycle.Application.Block, decision.ValidatorUID, validatorHotkey, uids, values); err != nil {
					return nil, fmt.Errorf("public dishonest-deposit %s validator %d vector differs from fresh signed intent lineage: %w", stage.name, decision.ValidatorID, err)
				}
				if err := verifyFinalSemanticCycleEpochDeposits(ctx, reader, decision.Cycle, appendExchanges); err != nil {
					return nil, fmt.Errorf("public dishonest-deposit %s validator %d deposit-ledger audit: %w", stage.name, decision.ValidatorID, err)
				}
				if err := verifyFinalSemanticCycleConvictionPrincipals(ctx, reader, decision.Cycle, appendExchanges); err != nil {
					return nil, fmt.Errorf("public dishonest-deposit %s validator %d conviction/principal audit: %w", stage.name, decision.ValidatorID, err)
				}
			}
		}
		if err := verifyFinalSemanticDishonestDepositReceiptsOnChain(ctx, reader, evidence.DishonestDeposit, appendExchanges); err != nil {
			return nil, fmt.Errorf("public dishonest-deposit receipts: %w", err)
		}
	}
	deployment, exchanges, err := reader.ContractDeployment(ctx, evidence.EVMTerminalHead)
	if err != nil {
		return nil, fmt.Errorf("public contract deployment state: %w", err)
	}
	if err := appendExchanges("evm", evidence.EVMTerminalHead, exchanges); err != nil {
		return nil, err
	}
	wantDeployment := FinalContractDeploymentState{
		CoordinatorProxy: evidence.Deployment.CoordinatorProxy, CoordinatorImplementation: evidence.Deployment.CoordinatorImplementation,
		SettlementVault: evidence.Deployment.SettlementVault, ReserveSink: evidence.Deployment.ReserveSink, GovernanceOwner: evidence.Deployment.GovernanceOwner,
		CoordinatorNetuid: evidence.Deployment.CoordinatorNetuid, CoordinatorSelfColdkey: evidence.Deployment.CoordinatorSelfColdkey,
		CoordinatorSettlementVault: evidence.Deployment.CoordinatorSettlementVault, CoordinatorReserveSink: evidence.Deployment.CoordinatorReserveSink,
		CoordinatorGuardian: evidence.Deployment.CoordinatorGuardian, CoordinatorActiveGuardian: evidence.Deployment.CoordinatorActiveGuardian,
		CoordinatorPaused: evidence.Deployment.CoordinatorPaused, CoordinatorCommitmentOracle: evidence.Deployment.CoordinatorCommitmentOracle,
		CoordinatorActiveCommitmentOracle: evidence.Deployment.CoordinatorActiveCommitmentOracle,
		VaultCoordinator:                  evidence.Deployment.VaultCoordinator, VaultNetuid: evidence.Deployment.VaultNetuid,
		VaultSelfColdkey: evidence.Deployment.VaultSelfColdkey, VaultEscrowHotkey: evidence.Deployment.VaultEscrowHotkey,
		VaultEscrowRegistered: evidence.Deployment.VaultEscrowRegistered, VaultMinimumClaimTTLBlocks: evidence.Deployment.VaultMinimumClaimTTLBlocks,
		VaultMinimumTransferTaoRao: evidence.Deployment.VaultMinimumTransferTaoRao,
		ReserveRecorder:            evidence.Deployment.ReserveRecorder, ReserveNetuid: evidence.Deployment.ReserveNetuid,
		ReserveSelfColdkey: evidence.Deployment.ReserveSelfColdkey, ReserveHotkey: evidence.Deployment.ReserveHotkey,
		CoordinatorProxyCodeHash: evidence.Deployment.CoordinatorProxyCodeHash, ImplementationCodeHash: evidence.Deployment.ImplementationCodeHash,
		SettlementVaultCodeHash: evidence.Deployment.SettlementVaultCodeHash, ReserveSinkCodeHash: evidence.Deployment.ReserveSinkCodeHash,
		ObservedImplementationSlot: evidence.Deployment.ObservedImplementationSlot, PolicyVersion: evidence.Deployment.PolicyVersion,
		PolicyHash:           evidence.PolicyHash,
		PolicyEffectiveEpoch: evidence.Deployment.PolicyEffectiveEpoch, PolicyEffectiveBlock: evidence.Deployment.PolicyEffectiveBlock, Block: evidence.EVMTerminalHead,
	}
	if deployment != wantDeployment {
		return nil, errors.New("public contract code/implementation/custody/policy state mismatch")
	}
	for _, point := range []FinalSettlementVaultState{evidence.SettlementAccounting.Before, evidence.SettlementAccounting.After} {
		state, exchanges, err := reader.SettlementVaultState(ctx, point.Block)
		if err != nil {
			return nil, fmt.Errorf("public settlement-vault state at %d: %w", point.Block.Number, err)
		}
		if err := appendExchanges("evm", point.Block, exchanges); err != nil {
			return nil, err
		}
		want := FinalSettlementVaultChainState{
			TotalCapturedRao: point.TotalCapturedRao, TotalPaidRao: point.TotalPaidRao, EscrowAccountedRao: point.EscrowAccountedRao,
			PendingFundingRao: point.PendingFundingRao, OutstandingLiabilityRao: point.OutstandingLiabilityRao,
			LiveEscrowStakeRao: point.LiveEscrowStakeRao, Block: point.Block,
		}
		if state != want {
			return nil, fmt.Errorf("public settlement-vault accounting mismatch at %d", point.Block.Number)
		}
	}
	for _, point := range []struct {
		head      ChainHead
		principal string
		live      string
	}{{evidence.Reserve.Before, evidence.Reserve.PrincipalBeforeRao, evidence.Reserve.LiveStakeBeforeRao}, {evidence.Reserve.After, evidence.Reserve.PrincipalAfterRao, evidence.Reserve.LiveStakeAfterRao}} {
		state, exchanges, err := reader.ReserveState(ctx, point.head)
		if err != nil {
			return nil, fmt.Errorf("public reserve state at %d: %w", point.head.Number, err)
		}
		if err := appendExchanges("evm", point.head, exchanges); err != nil {
			return nil, err
		}
		if state.PrincipalRao != point.principal || state.LiveStakeRao != point.live || state.Block != point.head {
			return nil, fmt.Errorf("public reserve state mismatch at %d", point.head.Number)
		}
	}
	for index, addition := range evidence.Reserve.PrincipalAdditions {
		state, exchanges, err := reader.EVMReceipt(ctx, addition.Receipt)
		if err != nil {
			return nil, fmt.Errorf("public ReservePrincipalAdded receipt %d: %w", index, err)
		}
		if err := appendExchanges("evm", addition.Receipt.Block, exchanges); err != nil {
			return nil, err
		}
		if state.TransactionHash != addition.Receipt.TransactionHash || state.Block != addition.Receipt.Block || state.Status != addition.Receipt.Status || state.LogsHash != addition.Receipt.LogsHash {
			return nil, fmt.Errorf("public ReservePrincipalAdded receipt %d does not match evidence", index)
		}
	}
	for i := range evidence.Epochs {
		row := &evidence.Epochs[i]
		receipts := []FinalEVMReceipt{row.Capture, row.Finalize}
		if row.Root != nil {
			receipts = append(receipts, *row.Root)
		}
		for _, claim := range row.Claims {
			receipts = append(receipts, claim.Receipt)
		}
		for _, receipt := range receipts {
			state, exchanges, err := reader.EVMReceipt(ctx, receipt)
			if err != nil {
				return nil, fmt.Errorf("public EVM receipt %s: %w", receipt.TransactionHash, err)
			}
			if err := appendExchanges("evm", receipt.Block, exchanges); err != nil {
				return nil, err
			}
			if state.TransactionHash != receipt.TransactionHash || state.Block != receipt.Block || state.Status != receipt.Status || state.LogsHash != receipt.LogsHash {
				return nil, fmt.Errorf("public EVM receipt %s does not match evidence", receipt.TransactionHash)
			}
		}
		state, exchanges, err := reader.PoolEpoch(ctx, row.Epoch, row.NoID, evidence.EVMTerminalHead)
		if err != nil {
			return nil, fmt.Errorf("public pool epoch %d/%d: %w", row.Epoch, row.NoID, err)
		}
		if err := appendExchanges("evm", evidence.EVMTerminalHead, exchanges); err != nil {
			return nil, err
		}
		want := FinalPoolEpochChainState{Epoch: row.Epoch, NoID: row.NoID, PayoutRoot: row.PayoutRoot, ArtifactHash: row.ArtifactHash, FundedRao: row.FundedRao, TotalRao: row.TotalRao, ClaimedRao: row.ClaimedRao, ExpiryBlock: row.ExpiryBlock, Status: row.Status, Block: evidence.EVMTerminalHead}
		if state != want {
			return nil, fmt.Errorf("public pool epoch %d/%d state mismatch", row.Epoch, row.NoID)
		}
	}
	for index, payment := range evidence.ClaimPayments {
		state, exchanges, err := reader.EVMReceipt(ctx, payment.Receipt)
		if err != nil {
			return nil, fmt.Errorf("public ClaimPaid receipt %d: %w", index, err)
		}
		if err := appendExchanges("evm", payment.Receipt.Block, exchanges); err != nil {
			return nil, err
		}
		if state.TransactionHash != payment.Receipt.TransactionHash || state.Block != payment.Receipt.Block || state.Status != payment.Receipt.Status || state.LogsHash != payment.Receipt.LogsHash {
			return nil, fmt.Errorf("public ClaimPaid receipt %d does not match evidence", index)
		}
	}
	if err := verifyFinalSemanticVaultCarries(ctx, evidence, reader, appendExchanges); err != nil {
		return nil, fmt.Errorf("public vault carry audit: %w", err)
	}
	if err := verifyFinalSemanticVaultClaims(ctx, evidence, reader, appendExchanges); err != nil {
		return nil, fmt.Errorf("public vault claim audit: %w", err)
	}
	for _, criterion := range evidence.ExitCriteria {
		for _, receipt := range criterion.EVMReceipts {
			state, exchanges, err := reader.EVMReceipt(ctx, receipt)
			if err != nil {
				return nil, fmt.Errorf("public exit criterion %s receipt %s: %w", criterion.ID, receipt.TransactionHash, err)
			}
			if err := appendExchanges("evm", receipt.Block, exchanges); err != nil {
				return nil, err
			}
			if state.TransactionHash != receipt.TransactionHash || state.Block != receipt.Block || state.Status != receipt.Status || state.LogsHash != receipt.LogsHash {
				return nil, fmt.Errorf("public exit criterion %s receipt %s does not match evidence", criterion.ID, receipt.TransactionHash)
			}
		}
	}
	for _, reward := range evidence.NativeRewards {
		hotkeyHex := reward.Hotkey
		for _, point := range []struct {
			head         ChainHead
			evmHead      ChainHead
			emission     string
			stake        string
			ownerStake   string
			reserveStake string
			incentive    uint16
			dividends    uint16
		}{{reward.Before, reward.OwnerStakeBeforeEVM, reward.BeforeRao, reward.StakeBeforeRao, reward.OwnerStakeBeforeRao, reward.ReserveStakeBeforeRao, reward.BeforeIncentiveU16, reward.BeforeDividendsU16}, {reward.After, reward.OwnerStakeAfterEVM, reward.AfterRao, reward.StakeAfterRao, reward.OwnerStakeAfterRao, reward.ReserveStakeAfterRao, reward.AfterIncentiveU16, reward.AfterDividendsU16}} {
			state, exchanges, err := reader.NativeReward(ctx, evidence.Netuid, reward.UID, point.head)
			if err != nil {
				return nil, fmt.Errorf("public native reward UID %d: %w", reward.UID, err)
			}
			if err := appendExchanges("substrate", point.head, exchanges); err != nil {
				return nil, err
			}
			if state.UID != reward.UID || state.EmissionRao != point.emission || state.StakeRao != point.stake || state.IncentiveU16 != point.incentive || state.DividendsU16 != point.dividends || state.Block != point.head {
				return nil, fmt.Errorf("public native reward UID %d emission/stake/incentive/dividends mismatch at %d", reward.UID, point.head.Number)
			}
			owner, ownerExchanges, err := reader.NativeOwnerStake(ctx, hotkeyHex, reward.OwnerColdkey, point.evmHead)
			if err != nil {
				return nil, fmt.Errorf("public native reward %s %d owner stake: %w", reward.Role, reward.SubjectID, err)
			}
			if err := appendExchanges("evm", point.evmHead, ownerExchanges); err != nil {
				return nil, err
			}
			if owner.HotkeyPublicKey != hotkeyHex || owner.ColdkeyPublicKey != reward.OwnerColdkey || owner.StakeRao != point.ownerStake || owner.Block != point.evmHead {
				return nil, fmt.Errorf("public native reward %s %d owner-pair stake mismatch at %d", reward.Role, reward.SubjectID, point.evmHead.Number)
			}
			if reward.ReserveColdkey != "" {
				reservePosition, reserveExchanges, err := reader.NativeOwnerStake(ctx, hotkeyHex, reward.ReserveColdkey, point.evmHead)
				if err != nil {
					return nil, fmt.Errorf("public reserve-validator sink stake: %w", err)
				}
				if err := appendExchanges("evm", point.evmHead, reserveExchanges); err != nil {
					return nil, err
				}
				if reservePosition.HotkeyPublicKey != hotkeyHex || reservePosition.ColdkeyPublicKey != reward.ReserveColdkey || reservePosition.StakeRao != point.reserveStake || reservePosition.Block != point.evmHead {
					return nil, fmt.Errorf("public reserve-validator sink-pair stake mismatch at %d", point.evmHead.Number)
				}
				reserveState, reserveStateExchanges, err := reader.ReserveState(ctx, point.evmHead)
				if err != nil {
					return nil, fmt.Errorf("public reserve live stake at validator checkpoint: %w", err)
				}
				if err := appendExchanges("evm", point.evmHead, reserveStateExchanges); err != nil {
					return nil, err
				}
				if reserveState.LiveStakeRao != point.reserveStake || reserveState.Block != point.evmHead {
					return nil, fmt.Errorf("public reserve live stake differs from sink-coldkey getStake at %d", point.evmHead.Number)
				}
			}
		}
	}
	if len(validatorUID) != evidence.ExpectedValidators {
		return nil, errors.New("public validator verification coverage is incomplete")
	}
	if err := finalizePublicChainVerification(verification, evidence.ChainID, evidence.GenesisHash); err != nil {
		return nil, err
	}
	return verification, nil
}

// Makes every mutable operator authority an exact terminal public-chain
// assertion. It is separate from the
// receipt decoder because a registration's initial OperatorScheduled event can
// predate an allowed authority rotation.
func verifyFinalSemanticPoolOperatorVersion(pool FinalPoolUIDEvidence, state FinalOperatorVersionChainState, head ChainHead) error {
	want := FinalOperatorVersionChainState{
		NoID: pool.NoID, VersionCount: pool.VersionCount, Coldkey: pool.OperatorColdkey, PoolHotkey: pool.Hotkey,
		DepositHotkey: pool.DepositHotkey, DepositSigner: pool.DepositSigner, RootSigner: pool.PayoutRootSigner,
		EffectiveEpoch: pool.EffectiveEpoch, Active: pool.Active, Block: head,
	}
	if state != want {
		return fmt.Errorf("public operator version no=%d differs from terminal pool evidence", pool.NoID)
	}
	return nil
}

func verifyFinalNativeEventOnChain(ctx context.Context, reader FinalSemanticChainReader, receipt FinalNativeReceipt, kind string, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	state, exchanges, err := reader.NativeEvent(ctx, receipt, kind)
	if err != nil {
		return err
	}
	if err := appendExchanges("substrate", receipt.Block, exchanges); err != nil {
		return err
	}
	if !state.Success || state.Block != receipt.Block || state.ExtrinsicHash != receipt.ExtrinsicHash || state.Event != kind {
		return errors.New("native event does not match receipt evidence")
	}
	return nil
}

// Binds each signed validator audit to the coordinator's historical cumulative
// ledger at the audit's exact EVM
// snapshot. A Deposit receipt may terminate a multi-transaction prefix, so
// comparing its final event amount alone would not prove ObservedDepositRao.
func verifyFinalSemanticCycleEpochDeposits(ctx context.Context, reader FinalSemanticChainReader, cycle FinalCRv4Cycle, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	for _, pool := range cycle.Pools {
		state, exchanges, err := reader.EpochDeposit(ctx, cycle.SettlementEpoch, pool.NoID, cycle.EVMSnapshot)
		if err != nil {
			return fmt.Errorf("operator %d epoch %d: %w", pool.NoID, cycle.SettlementEpoch, err)
		}
		if err := appendExchanges("evm", cycle.EVMSnapshot, exchanges); err != nil {
			return err
		}
		if state.Epoch != cycle.SettlementEpoch || state.NoID != pool.NoID || state.AmountRao != pool.ObservedDepositRao || state.Block != cycle.EVMSnapshot {
			return fmt.Errorf("operator %d epoch %d cumulative deposit differs from signed audit", pool.NoID, cycle.SettlementEpoch)
		}
	}
	return nil
}

// Independently reads the two successful demand-deposit transactions. Public
// receipt payload replay
// already checks their amount/policy/signer fields; this cross-receipt check
// additionally proves the recovery consumed a later nonce.
func verifyFinalSemanticDishonestDepositReceiptsOnChain(ctx context.Context, reader FinalSemanticChainReader, dishonest *FinalDishonestDepositEvidence, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	if dishonest == nil {
		return errors.New("dishonest-deposit evidence is nil")
	}
	states := make([]FinalEVMReceiptState, 2)
	for index, item := range []struct {
		label   string
		receipt FinalEVMReceipt
	}{{label: "underpayment", receipt: dishonest.UnderpaymentReceipt}, {label: "recovery", receipt: dishonest.RecoveryDepositReceipt}} {
		state, exchanges, err := reader.EVMReceipt(ctx, item.receipt)
		if err != nil {
			return fmt.Errorf("%s: %w", item.label, err)
		}
		if err := appendExchanges("evm", item.receipt.Block, exchanges); err != nil {
			return err
		}
		if state.TransactionHash != item.receipt.TransactionHash || state.Block != item.receipt.Block || state.Status != item.receipt.Status || state.LogsHash != item.receipt.LogsHash {
			return fmt.Errorf("%s receipt differs from semantic evidence", item.label)
		}
		states[index] = state
	}
	if states[0].receiptPayload == nil || states[1].receiptPayload == nil || len(states[0].receiptPayload.deposits) != 1 || len(states[1].receiptPayload.deposits) != 1 {
		return errors.New("dishonest-deposit receipt payload projection is incomplete")
	}
	underpayment, recovery := states[0].receiptPayload.deposits[0], states[1].receiptPayload.deposits[0]
	if underpayment.NoID != dishonest.NoID || recovery.NoID != dishonest.NoID || underpayment.Nonce == nil || recovery.Nonce == nil || recovery.Nonce.Cmp(underpayment.Nonce) <= 0 {
		return errors.New("dishonest-deposit recovery nonce does not follow the underpayment nonce")
	}
	return nil
}

func finalSemanticHeads(evidence *FinalSemanticEvidence) ([]ChainHead, []ChainHead, error) {
	native := []ChainHead{evidence.NativeStartHead, evidence.NativeTerminalHead}
	evm := []ChainHead{evidence.EVMCampaignStartHead, evidence.Window.BaselineHead, evidence.EVMTerminalHead}
	if lifecycle := evidence.FleetLifecycle; lifecycle != nil {
		state := &lifecycle.State
		for _, schedule := range []*FleetLifecycleNativeSchedule{state.ReleaseHandoffSchedule, state.ProductionNativeSchedule} {
			if schedule != nil {
				native = append(native, schedule.ObservedHead)
			}
		}
		if state.LaunchPrune != nil {
			native = append(native, state.LaunchPrune.Head)
		}
		for _, registration := range []*FleetLifecycleRegistrationEvidence{state.FallbackRegistration, state.ProviderRegistration, state.TerminalRegistration} {
			if registration != nil {
				native = append(native, registration.PrePrune.Head, registration.PostRegistration.Head, ChainHead{Number: registration.BlockNumber, Hash: strings.ToLower(registration.BlockHash)})
			}
		}
		if state.PostRegistrationRewardBaseline.Number != 0 {
			native = append(native, state.PostRegistrationRewardBaseline)
		}
		for _, census := range state.CandidateCensuses {
			native = append(native, census.NativeObservedHead)
			evm = append(evm, census.ObservedHead)
			for _, validator := range census.Validators {
				native = append(native, validator.Commit, ChainHead{Number: validator.RevealBlock, Hash: strings.ToLower(validator.RevealBlockHash)}, validator.Application)
			}
		}
		for _, variant := range lifecycle.Variants {
			native = append(native, ChainHead{Number: variant.Commitment.FinalizedBlock, Hash: strings.ToLower(variant.Commitment.FinalizedBlockHash)})
			evm = append(evm, ChainHead{Number: variant.Mirror.BlockNumber, Hash: strings.ToLower(variant.Mirror.BlockHash)})
			for _, binding := range variant.Bindings {
				evm = append(evm, ChainHead{Number: binding.BlockNumber, Hash: strings.ToLower(binding.BlockHash)})
			}
			for _, cleanup := range variant.Cleanups {
				evm = append(evm, cleanup.BeforeBlock, ChainHead{Number: cleanup.BlockNumber, Hash: strings.ToLower(cleanup.BlockHash)})
			}
		}
		for _, payout := range lifecycle.PayoutArtifacts {
			evm = append(evm, payout.Root.Block)
		}
	}
	if lineage := evidence.FleetGeneration; lineage != nil {
		for _, fleet := range lineage.SetupFleets {
			for _, version := range []FinalFleetGenerationVersionEvidence{fleet.Initial, fleet.Refresh} {
				native = append(native, version.NativeHead)
			}
		}
		for _, challenger := range lineage.ChallengerFleets {
			native = append(native, challenger.Initial.NativeHead, challenger.Registration.Block)
		}
		for _, batch := range lineage.Batches {
			for _, write := range batch.CarriedHistory {
				evm = append(evm, write.Receipt.Block, write.EVMHead)
				native = append(native, write.NativeHead)
			}
			if batch.BatchWrite != nil {
				evm = append(evm, batch.BatchWrite.Receipt.Block, batch.BatchWrite.EVMHead)
				native = append(native, batch.BatchWrite.NativeHead)
			}
		}
	}
	for _, checkpoint := range finalFleetRefreshOracleCheckpointRows(evidence.FleetRefreshOracleWindow.Checkpoints) {
		evm = append(evm, checkpoint.value.Head)
	}
	for _, receipt := range evidence.HistoricalCoordinatorReceipts {
		evm = append(evm, receipt.Receipt.Block, receipt.ExecutionHead)
	}
	for _, timeline := range evidence.HistoricalCoordinatorTimeline {
		evm = append(evm, timeline.Baseline)
		for _, upgrade := range timeline.Upgrades {
			evm = append(evm, upgrade.Block)
		}
	}
	for _, pool := range evidence.Pools {
		native = append(native, pool.Snapshot)
		evm = append(evm, pool.Registration.Block, pool.ConvictionReceipt.Block)
	}
	for _, fleet := range evidence.HeadFleets {
		native = append(native, fleet.Registration.Block, fleet.Snapshot)
	}
	for _, transition := range evidence.HeadTransitions {
		native = append(native, transition.Registration.Block, transition.Snapshot, transition.IndependentSnapshot)
		evm = append(evm, transition.EVMSnapshot, transition.IndependentEVMSnapshot)
	}
	for _, validator := range evidence.Validators {
		native = append(native, validator.Registration.Block, validator.Snapshot)
		for _, cycle := range validator.Cycles {
			native = append(native, cycle.NativeSnapshot, cycle.Commit.Block, cycle.Reveal.Block, cycle.Application.Block)
			evm = append(evm, cycle.EVMSnapshot)
			for _, pool := range cycle.Pools {
				evm = append(evm, pool.DepositReceipt.Block)
			}
		}
	}
	if evidence.DishonestDeposit != nil {
		for _, decisions := range [][]FinalDishonestDepositDecision{evidence.DishonestDeposit.Penalties, evidence.DishonestDeposit.Recoveries} {
			for _, decision := range decisions {
				cycle := decision.Cycle
				native = append(native, cycle.NativeSnapshot, cycle.Commit.Block, cycle.Reveal.Block, cycle.Application.Block)
				evm = append(evm, cycle.EVMSnapshot)
				for _, pool := range cycle.Pools {
					evm = append(evm, pool.DepositReceipt.Block)
				}
			}
		}
		evm = append(evm, evidence.DishonestDeposit.UnderpaymentReceipt.Block, evidence.DishonestDeposit.RecoveryDepositReceipt.Block)
	}
	evm = append(evm, evidence.Deployment.Snapshot, evidence.SettlementAccounting.Before.Block, evidence.SettlementAccounting.After.Block, evidence.Reserve.Before, evidence.Reserve.After)
	for _, addition := range evidence.Reserve.PrincipalAdditions {
		evm = append(evm, addition.Receipt.Block)
	}
	for _, row := range evidence.Epochs {
		evm = append(evm, row.Capture.Block, row.Finalize.Block)
		if row.Root != nil {
			evm = append(evm, row.Root.Block)
		}
		for _, claim := range row.Claims {
			evm = append(evm, claim.Receipt.Block)
		}
	}
	for _, criterion := range evidence.ExitCriteria {
		for _, receipt := range criterion.EVMReceipts {
			evm = append(evm, receipt.Block)
		}
	}
	for _, reward := range evidence.NativeRewards {
		native = append(native, reward.Before, reward.After)
		evm = append(evm, reward.OwnerStakeBeforeEVM, reward.OwnerStakeAfterEVM)
	}
	for _, proof := range evidence.PathProofs {
		for _, closure := range proof.SettlementClosures {
			evm = append(evm, closure.Boundary)
		}
	}
	native, err := finalUniqueHeads(native)
	if err != nil {
		return nil, nil, fmt.Errorf("native checkpoints: %w", err)
	}
	evm, err = finalUniqueHeads(evm)
	if err != nil {
		return nil, nil, fmt.Errorf("EVM checkpoints: %w", err)
	}
	return native, evm, nil
}

func finalUniqueHeads(heads []ChainHead) ([]ChainHead, error) {
	byNumber := map[uint64]ChainHead{}
	for _, head := range heads {
		if prior, ok := byNumber[head.Number]; ok && prior.Hash != head.Hash {
			return nil, fmt.Errorf("block %d has conflicting hashes %s and %s", head.Number, prior.Hash, head.Hash)
		}
		byNumber[head.Number] = head
	}
	out := make([]ChainHead, 0, len(byNumber))
	for _, head := range byNumber {
		out = append(out, head)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func finalSubmittedValues(submitted []FinalSubmittedWeight) ([]uint16, []uint16) {
	uids, values := make([]uint16, len(submitted)), make([]uint16, len(submitted))
	for i, weight := range submitted {
		uids[i], values[i] = weight.UID, weight.Value
	}
	return uids, values
}

func finalRPCExchangeHashes(exchange FinalRPCExchange) (string, string, error) {
	if exchange.Method == "" || strings.ContainsAny(exchange.Method, " \t\r\n") {
		return "", "", errors.New("public RPC transcript method is empty or unsafe")
	}
	var params any
	if err := json.Unmarshal(exchange.Params, &params); err != nil {
		return "", "", fmt.Errorf("public RPC %s params: %w", exchange.Method, err)
	}
	if _, ok := params.([]any); !ok {
		return "", "", fmt.Errorf("public RPC %s params are not an array", exchange.Method)
	}
	var result any
	if err := json.Unmarshal(exchange.Result, &result); err != nil {
		return "", "", fmt.Errorf("public RPC %s result: %w", exchange.Method, err)
	}
	requestHash, err := canonicalHashHex(struct {
		Chain      string    `json:"chain"`
		Method     string    `json:"method"`
		Params     any       `json:"params"`
		PinnedHead ChainHead `json:"pinned_head"`
	}{exchange.Chain, exchange.Method, params, exchange.PinnedHead})
	if err != nil {
		return "", "", err
	}
	responseHash, err := canonicalHashHex(result)
	return requestHash, responseHash, err
}

func finalizePublicChainVerification(verification *FinalPublicChainVerification, chainID uint64, genesisHash string) error {
	if verification == nil || verification.Schema != finalPublicChainVerificationSchema || len(verification.Exchanges) == 0 {
		return errors.New("public chain verification transcript is incomplete")
	}
	if err := verifyFinalPublicEndpoint("Substrate", verification.SubstrateRPC, "wss", "https"); err != nil {
		return err
	}
	if err := verifyFinalPublicEndpoint("EVM", verification.EVMRPC, "https", "wss"); err != nil {
		return err
	}
	if err := verifyFinalEvidenceURI("public deployment manifest", verification.EvidenceURI, verification.EvidenceTransportProfile, chainID, genesisHash); err != nil {
		return err
	}
	if err := validateFinalOperatorEvidenceOrigins(verification.OperatorEvidenceOrigins, verification.EvidenceURI, verification.EvidenceTransportProfile, chainID, genesisHash); err != nil {
		return err
	}
	if err := requireFinalHex32("public deployment manifest hash", verification.PublicManifestHash); err != nil {
		return err
	}
	if err := verifyFinalPublicFleetAuditShape(verification.FleetAudit); err != nil {
		return err
	}
	if err := verifyFinalPublicChronologyAuditShape(verification.ChronologyAudit); err != nil {
		return err
	}
	if err := verifyFinalPublicNativePayoutAuditShape(verification.NativePayoutAudit); err != nil {
		return err
	}
	if err := verifyFinalPublicNativePayoutObservationShape(verification.NativePayoutAudit, verification.NativePayouts); err != nil {
		return err
	}
	for i := range verification.Exchanges {
		exchange := &verification.Exchanges[i]
		if exchange.Sequence != uint64(i+1) || (exchange.Chain != "substrate" && exchange.Chain != "evm") {
			return errors.New("public RPC transcript sequence or chain is invalid")
		}
		if err := verifyFinalHead("public RPC pinned", exchange.PinnedHead); err != nil {
			return err
		}
		requestHash, responseHash, err := finalRPCExchangeHashes(*exchange)
		if err != nil {
			return err
		}
		if exchange.RequestHash != requestHash || exchange.ResponseHash != responseHash {
			return errors.New("public RPC transcript request/result hash mismatch")
		}
	}
	if err := verifyFinalPublicChronologyTranscript(verification); err != nil {
		return err
	}
	copy := *verification
	copy.TranscriptHash = ""
	hash, err := canonicalHashHex(copy)
	if err != nil {
		return err
	}
	verification.TranscriptHash = hash
	return nil
}

func validateFinalOperatorEvidenceOrigins(origins []FinalOperatorEvidenceOrigin, primaryURI, profile string, chainID uint64, genesisHash string) error {
	if len(origins) != 2 {
		return fmt.Errorf("operator evidence origins=%d, want exactly 2", len(origins))
	}
	seenOrigins := map[string]bool{}
	primary := false
	for index, origin := range origins {
		if origin.OperatorNoID != index+1 {
			return errors.New("operator evidence origins are not in canonical operator order")
		}
		if err := verifyFinalEvidenceURI(fmt.Sprintf("operator %d deployment manifest", origin.OperatorNoID), origin.ManifestURI, profile, chainID, genesisHash); err != nil {
			return err
		}
		parsed, _ := url.Parse(origin.ManifestURI)
		if parsed.RawQuery == "" {
			return fmt.Errorf("operator %d deployment manifest URI is not content-addressed", origin.OperatorNoID)
		}
		bare, err := publicEvidenceOrigin(origin.ManifestURI, profile, chainID, genesisHash)
		if err != nil || seenOrigins[bare] {
			return stateMismatchError(err, "operator evidence origins are not distinct")
		}
		seenOrigins[bare] = true
		if origin.ManifestURI == primaryURI {
			primary = true
		}
	}
	if !primary {
		return errors.New("primary deployment-manifest URI is not one of the two operator evidence origins")
	}
	return nil
}

func verifyFinalPublicChainVerification(verification *FinalPublicChainVerification, chainID uint64, genesisHash string) error {
	if verification == nil {
		return errors.New("public chain verification transcript is missing")
	}
	want := verification.TranscriptHash
	if err := requireFinalHex32("public chain transcript hash", want); err != nil {
		return err
	}
	copyBytes, err := json.Marshal(verification)
	if err != nil {
		return err
	}
	var copy FinalPublicChainVerification
	if err := json.Unmarshal(copyBytes, &copy); err != nil {
		return err
	}
	if err := finalizePublicChainVerification(&copy, chainID, genesisHash); err != nil {
		return err
	}
	if copy.TranscriptHash != want {
		return fmt.Errorf("public chain transcript hash %s, reconstructed %s", want, copy.TranscriptHash)
	}
	return nil
}

func verifyFinalPublicEndpoint(label, raw string, schemes ...string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s RPC is not a credential-free public endpoint", label)
	}
	for _, scheme := range schemes {
		if parsed.Scheme == scheme {
			return nil
		}
	}
	return fmt.Errorf("%s RPC is not a public TLS endpoint", label)
}

func verifyFinalURI(label, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Scheme != "https" || parsed.Fragment != "" || parsed.Path == "" {
		return fmt.Errorf("%s URI is not a credential-free public HTTPS object", label)
	}
	if parsed.RawQuery != "" {
		values, queryErr := url.ParseQuery(parsed.RawQuery)
		hash := values.Get("hash")
		canonicalQuery, literalQuery := "hash="+url.QueryEscape(hash), "hash="+hash
		if queryErr != nil || len(values) != 1 || len(values["hash"]) != 1 || requireFinalSHA256(label+" URI hash", hash) != nil || (parsed.RawQuery != canonicalQuery && parsed.RawQuery != literalQuery) {
			return fmt.Errorf("%s URI does not have one canonical content hash query", label)
		}
	}
	return nil
}

func verifyFinalEvidenceURI(label, raw, profile string, chainID uint64, genesisHash string) error {
	if err := verifyPublicEvidenceObjectURI(label, raw, profile, chainID, genesisHash); err != nil {
		return err
	}
	derived, err := publicEvidenceTransportForURI(raw, chainID, genesisHash)
	if err != nil || derived != profile {
		return stateMismatchError(err, "%s URI transport does not match profile %q", label, profile)
	}
	parsed, _ := url.Parse(raw)
	if parsed.RawQuery != "" {
		values, queryErr := url.ParseQuery(parsed.RawQuery)
		hash := values.Get("hash")
		canonicalQuery, literalQuery := "hash="+url.QueryEscape(hash), "hash="+hash
		if queryErr != nil || len(values) != 1 || len(values["hash"]) != 1 || requireFinalSHA256(label+" URI hash", hash) != nil || (parsed.RawQuery != canonicalQuery && parsed.RawQuery != literalQuery) {
			return fmt.Errorf("%s URI does not have one canonical content hash query", label)
		}
	}
	return nil
}
