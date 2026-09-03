package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	gsrpcTypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/payoutartifact"
	"github.com/urfoundation/sn/protocol"
	validatorpkg "github.com/urfoundation/sn/validator"
)

// AssertionRecord is the stable machine-readable assertion format shared by
// assertions.json and junit.xml. ObservationHash points at the exact snapshot
// on which the assertion was evaluated.
type AssertionRecord struct {
	ID              string  `json:"id"`
	Passed          bool    `json:"passed"`
	Message         string  `json:"message"`
	StartedAt       string  `json:"started_at"`
	CompletedAt     string  `json:"completed_at"`
	DurationSeconds float64 `json:"duration_seconds"`
	ObservationHash string  `json:"observation_hash"`
}

type VerifyKeyObservation struct {
	ServerKeyID byte   `json:"server_key_id"`
	PublicKey   []byte `json:"public_key"`
}

// OperatorPayoutClientTierObservation is the compact, explicit membership of
// one lifecycle client in an already signature- and chain-authenticated payout
// artifact. Exactly one of Leaf and HeadExcluded must be true. Keeping only the
// twelve lifecycle clients avoids copying every 1,000-miner artifact into every
// polling observation; the complete artifact remains in public history and is
// captured for final replay.
type OperatorPayoutClientTierObservation struct {
	ClientID     string `json:"client_id"`
	Leaf         bool   `json:"leaf"`
	HeadExcluded bool   `json:"head_excluded"`
}

type OperatorLifecyclePayoutArtifactObservation struct {
	Epoch       uint64                                `json:"epoch"`
	NoID        uint64                                `json:"no_id"`
	ContentHash string                                `json:"content_hash"`
	PayoutRoot  string                                `json:"payout_root"`
	Clients     []OperatorPayoutClientTierObservation `json:"clients"`
}

type OperatorObservation struct {
	NoID                        int                                          `json:"no_id"`
	APIURL                      string                                       `json:"api_url"`
	Healthy                     bool                                         `json:"healthy"`
	StatusCode                  int                                          `json:"status_code"`
	StatsRows                   int                                          `json:"stats_rows"`
	Assignments                 uint64                                       `json:"assignments"`
	Confirmations               uint64                                       `json:"confirmations"`
	ReliabilityPPM              uint32                                       `json:"reliability_ppm"`
	ProofRows                   int                                          `json:"proof_rows"`
	VerifyKeyIDs                []byte                                       `json:"verify_key_ids,omitempty"`
	VerifyKeys                  []VerifyKeyObservation                       `json:"verify_keys,omitempty"`
	ProofKeyIDs                 []byte                                       `json:"proof_key_ids,omitempty"`
	StatsHash                   string                                       `json:"stats_hash,omitempty"`
	ProofsHash                  string                                       `json:"proofs_hash,omitempty"`
	StatsPolicyHash             string                                       `json:"stats_policy_hash,omitempty"`
	ProofsPolicyHash            string                                       `json:"proofs_policy_hash,omitempty"`
	ArtifactHistoryObjects      int                                          `json:"artifact_history_objects"`
	ValidArtifacts              int                                          `json:"valid_artifacts"`
	MatchingArtifacts           int                                          `json:"matching_onchain_artifacts"`
	ExpectedFinalizedArtifacts  int                                          `json:"expected_finalized_artifacts"`
	ArtifactHashes              []string                                     `json:"artifact_hashes,omitempty"`
	LatestArtifactEpoch         uint64                                       `json:"latest_artifact_epoch,omitempty"`
	LatestArtifactHash          string                                       `json:"latest_artifact_hash,omitempty"`
	LatestPayoutRoot            string                                       `json:"latest_payout_root,omitempty"`
	LatestLeafClientIDs         []string                                     `json:"latest_leaf_client_ids,omitempty"`
	LatestHeadExcludedClientIDs []string                                     `json:"latest_head_excluded_client_ids,omitempty"`
	LifecyclePayoutArtifacts    []OperatorLifecyclePayoutArtifactObservation `json:"lifecycle_payout_artifacts,omitempty"`
	LatestArtifactProviders     int                                          `json:"latest_artifact_providers,omitempty"`
	CandidateProviders          int                                          `json:"candidate_providers,omitempty"`
	CandidateHeadExcluded       int                                          `json:"candidate_head_excluded,omitempty"`
	CandidateLeaves             int                                          `json:"candidate_leaves,omitempty"`
	PoolTailProviders           int                                          `json:"pool_tail_providers,omitempty"`
	PoolTailHeadExcluded        int                                          `json:"pool_tail_head_excluded,omitempty"`
	PoolTailLeaves              int                                          `json:"pool_tail_leaves,omitempty"`
	TierMembershipValid         bool                                         `json:"tier_membership_valid"`
	Error                       string                                       `json:"error,omitempty"`
}

type IntentWeightObservation struct {
	UID         uint16 `json:"uid"`
	Numerator   string `json:"numerator"`
	Denominator string `json:"denominator"`
	Value       uint16 `json:"value"`
}

// Preserves every applied validator decision so a later valid vector cannot
// hide an invalid top-slot decision made inside the same acceptance window.
type HeadDecisionObservation struct {
	VectorHash              string                      `json:"vector_hash"`
	ExtrinsicHash           string                      `json:"extrinsic_hash,omitempty"`
	SettlementEpoch         uint64                      `json:"settlement_epoch"`
	NativeSnapshot          ChainHead                   `json:"native_snapshot"`
	EVMSnapshot             ChainHead                   `json:"evm_snapshot"`
	FinalizedBlock          uint64                      `json:"finalized_block,omitempty"`
	FinalizedBlockHash      string                      `json:"finalized_block_hash,omitempty"`
	RevealBlock             uint64                      `json:"reveal_block,omitempty"`
	RevealBlockHash         string                      `json:"reveal_block_hash,omitempty"`
	SubnetEpoch             uint64                      `json:"subnet_epoch"`
	ApplicationBlock        uint64                      `json:"application_block"`
	ApplicationBlockHash    string                      `json:"application_block_hash"`
	MeasurementArtifactHash string                      `json:"measurement_artifact_hash"`
	CandidateFleetUIDs      []uint16                    `json:"candidate_fleet_uids"`
	CandidateFleetHotkeys   []string                    `json:"candidate_fleet_hotkeys"`
	MaskedUIDs              []uint16                    `json:"masked_uids,omitempty"`
	EligibleHeadUIDs        []uint16                    `json:"eligible_head_uids"`
	EligibleHeadScores      []validatorpkg.RationalJSON `json:"eligible_head_scores"`
	SelectedHeadUIDs        []uint16                    `json:"selected_head_uids"`
	RejectedHeadUIDs        []uint16                    `json:"rejected_head_uids"`
	StaleHeadBindings       int                         `json:"stale_head_bindings"`
	AppliedWeights          []IntentWeightObservation   `json:"applied_weights"`
	Error                   string                      `json:"error,omitempty"`
}

func headDecisionCandidateIdentities(artifact *validatorpkg.ReleaseMeasurementArtifact, eligible []uint16) ([]uint16, []string, error) {
	if artifact == nil || len(eligible) == 0 || len(uint16Set(eligible)) != len(eligible) {
		return nil, nil, errors.New("validator measurement has no exact eligible candidate set")
	}
	eligibleSet := uint16Set(eligible)
	hotkeyByUID := make(map[uint16]string, len(eligible))
	for _, binding := range artifact.Bindings {
		if !binding.Active || binding.Cleaned || !binding.LiveUIDFound || binding.LiveUID != binding.RecordUID || !eligibleSet[binding.LiveUID] {
			continue
		}
		hotkey := strings.ToLower(binding.Hotkey)
		if _, ok := evidenceFixedHex(hotkey, 32); !ok {
			return nil, nil, fmt.Errorf("validator measurement UID %d has a noncanonical hotkey", binding.LiveUID)
		}
		if prior := hotkeyByUID[binding.LiveUID]; prior != "" && prior != hotkey {
			return nil, nil, fmt.Errorf("validator measurement UID %d maps to multiple hotkeys", binding.LiveUID)
		}
		hotkeyByUID[binding.LiveUID] = hotkey
	}
	uids := append([]uint16(nil), eligible...)
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	hotkeys := make([]string, len(uids))
	for index, uid := range uids {
		hotkeys[index] = hotkeyByUID[uid]
		if hotkeys[index] == "" {
			return nil, nil, fmt.Errorf("validator measurement UID %d has no exact active binding identity", uid)
		}
	}
	return uids, hotkeys, nil
}

type ValidatorObservation struct {
	ValidatorID        int                         `json:"validator_id"`
	CurrentStatus      string                      `json:"current_status,omitempty"`
	CurrentEpoch       uint64                      `json:"current_epoch,omitempty"`
	VectorHash         string                      `json:"vector_hash,omitempty"`
	ValuesHash         string                      `json:"values_hash,omitempty"`
	FinalizedIntents   int                         `json:"finalized_intents"`
	AppliedIntents     int                         `json:"applied_intents"`
	SelfUID            uint16                      `json:"self_uid"`
	MaskedUIDs         []uint16                    `json:"masked_uids,omitempty"`
	EligibleHeadUIDs   []uint16                    `json:"eligible_head_uids,omitempty"`
	EligibleHeadScores []validatorpkg.RationalJSON `json:"eligible_head_scores,omitempty"`
	SelectedHeadUIDs   []uint16                    `json:"selected_head_uids,omitempty"`
	RejectedHeadUIDs   []uint16                    `json:"rejected_head_uids,omitempty"`
	HeadDecisionEpochs int                         `json:"head_decision_epochs"`
	HeadTransitions    int                         `json:"head_transitions"`
	PromotedHeadUIDs   []uint16                    `json:"promoted_head_uids,omitempty"`
	DemotedHeadUIDs    []uint16                    `json:"demoted_head_uids,omitempty"`
	StaleHeadBindings  int                         `json:"stale_head_bindings"`
	IntentHashes       []string                    `json:"intent_hashes,omitempty"`
	AppliedWeights     []IntentWeightObservation   `json:"applied_weights,omitempty"`
	HeadDecisions      []HeadDecisionObservation   `json:"head_decisions,omitempty"`
	DepositAudits      []validatorpkg.DepositAudit `json:"deposit_audits,omitempty"`
	PathProofCounts    map[int]int                 `json:"path_proof_counts,omitempty"`
	Error              string                      `json:"error,omitempty"`
}

type ClaimObservation struct {
	MinerID    int    `json:"miner_id"`
	NoID       int    `json:"no_id"`
	Discovered int    `json:"discovered"`
	Finalized  int    `json:"finalized"`
	NoClaim    int    `json:"no_claim"`
	Pending    int    `json:"pending"`
	Uncertain  int    `json:"uncertain"`
	Failed     int    `json:"failed"`
	LastTxHash string `json:"last_tx_hash,omitempty"`
	Error      string `json:"error,omitempty"`
}

type NativeRewardObservation struct {
	FinalizedHead       ChainHead `json:"finalized_head"`
	EmissionRao         []string  `json:"emission_rao"`
	Incentive           []uint16  `json:"incentive"`
	Dividends           []uint16  `json:"dividends"`
	TotalHotkeyAlphaRao []string  `json:"total_hotkey_alpha_rao"`
}

type ScenarioObservation struct {
	Schema                     string                         `json:"schema"`
	ObservedAt                 string                         `json:"observed_at"`
	Status                     *DeploymentStatus              `json:"status"`
	Operators                  []OperatorObservation          `json:"operators"`
	Validators                 []ValidatorObservation         `json:"validators"`
	Claims                     []ClaimObservation             `json:"claims"`
	PublicIdentityCount        int                            `json:"public_identity_count"`
	PublicIdentitiesValid      bool                           `json:"public_identities_valid"`
	FleetCommitmentValid       bool                           `json:"fleet_commitment_valid"`
	FleetBindingCount          int                            `json:"fleet_binding_count"`
	FleetBindingsValid         bool                           `json:"fleet_bindings_valid"`
	CandidateFleetUIDs         []uint16                       `json:"candidate_fleet_uids"`
	CandidateFleetHotkeys      []string                       `json:"candidate_fleet_hotkeys"`
	CandidateFleetMiners       [][]int                        `json:"candidate_fleet_miners"`
	NativeRewards              *NativeRewardObservation       `json:"native_rewards,omitempty"`
	NativeRewardsError         string                         `json:"native_rewards_error,omitempty"`
	ReserveValidatorRegistered bool                           `json:"reserve_validator_registered"`
	ReserveValidatorUID        uint16                         `json:"reserve_validator_uid"`
	ReserveDelegateTake        *uint16                        `json:"reserve_delegate_take,omitempty"`
	EscrowHotkeyRegistered     bool                           `json:"escrow_hotkey_registered"`
	EscrowHotkeyUID            uint16                         `json:"escrow_hotkey_uid"`
	NativeCustodyError         string                         `json:"native_custody_error,omitempty"`
	VoluntaryConviction        *VoluntaryConvictionEvidence   `json:"voluntary_conviction,omitempty"`
	VoluntaryConvictionValid   bool                           `json:"voluntary_conviction_valid"`
	VoluntaryConvictionError   string                         `json:"voluntary_conviction_error,omitempty"`
	GovernanceDrill            *GovernanceDrillEvidence       `json:"governance_drill,omitempty"`
	GovernanceDrillError       string                         `json:"governance_drill_error,omitempty"`
	FleetLifecycle             *FleetLifecycleEvidence        `json:"fleet_lifecycle,omitempty"`
	PrecompileConformance      *PrecompileConformanceEvidence `json:"precompile_conformance,omitempty"`
	PrecompileConformanceValid bool                           `json:"precompile_conformance_valid"`
	PrecompileConformanceError string                         `json:"precompile_conformance_error,omitempty"`
	DishonestDeposit           *DishonestDepositEvidence      `json:"dishonest_deposit,omitempty"`
	DishonestDepositValid      bool                           `json:"dishonest_deposit_valid"`
	DishonestDepositError      string                         `json:"dishonest_deposit_error,omitempty"`
	ProcessLogFindings         []ProcessLogFinding            `json:"process_log_findings,omitempty"`
	ExpectedFaultIDs           []string                       `json:"expected_fault_ids,omitempty"`
	ExpectedFaultTargets       []string                       `json:"expected_fault_targets,omitempty"`
	ObservationHash            string                         `json:"observation_hash"`
}

type ScenarioResult struct {
	Schema               string                     `json:"schema"`
	Release              string                     `json:"release"`
	RunID                string                     `json:"run_id"`
	DeploymentID         string                     `json:"deployment_id"`
	Name                 string                     `json:"name"`
	ScenarioDefinition   string                     `json:"scenario_definition_hash"`
	ScenarioMatrix       string                     `json:"scenario_matrix_hash,omitempty"`
	AdversarialMatrix    string                     `json:"adversarial_matrix_hash,omitempty"`
	ConfigHash           string                     `json:"config_hash"`
	PolicyHash           string                     `json:"policy_hash"`
	ChainID              uint64                     `json:"chain_id"`
	GenesisHash          string                     `json:"genesis_hash"`
	Netuid               uint16                     `json:"netuid"`
	StartedAt            string                     `json:"started_at"`
	CompletedAt          string                     `json:"completed_at"`
	CampaignStartHead    ChainHead                  `json:"campaign_start_finalized_head"`
	CampaignStartEpoch   uint64                     `json:"campaign_start_epoch"`
	StartHead            ChainHead                  `json:"start_finalized_head"`
	EndHead              ChainHead                  `json:"end_finalized_head"`
	StartEpoch           uint64                     `json:"start_epoch"`
	EndEpoch             uint64                     `json:"end_epoch"`
	AcceptanceWindow     *ScenarioAcceptanceWindow  `json:"acceptance_window,omitempty"`
	AssertionCount       int                        `json:"assertion_count"`
	FailedAssertionCount int                        `json:"failed_assertion_count"`
	Assertions           []AssertionRecord          `json:"assertions"`
	Faults               []ScenarioFaultRecord      `json:"faults,omitempty"`
	Adversaries          *AdversaryCampaignEvidence `json:"adversaries,omitempty"`
	Anomalies            *ScenarioAnomalyLedger     `json:"anomalies"`
	ValueReconciliation  map[string]string          `json:"value_reconciliation"`
	PublishedEvidence    []PublishedEvidence        `json:"published_evidence,omitempty"`
	LifecycleHandoff     *ScenarioLifecycleHandoff  `json:"lifecycle_handoff,omitempty"`
	PriorRelease         *ReleaseCampaignGate       `json:"prior_release,omitempty"`
	EvidenceHash         string                     `json:"evidence_hash"`
	Result               string                     `json:"result"`
}

// ScenarioAcceptanceWindow binds a release result to complete contract epochs
// which begin only after preparation and observation are live. EndBlock is the
// exclusive boundary after the last accepted epoch; TerminalBlock additionally
// includes that policy's finalization offset.
type ScenarioAcceptanceWindow struct {
	Schema                  string    `json:"schema"`
	BaselineHead            ChainHead `json:"baseline_finalized_head"`
	BaselineObservationHash string    `json:"baseline_observation_hash"`
	BaselineEpoch           uint64    `json:"baseline_epoch"`
	FirstEpoch              uint64    `json:"first_epoch"`
	EpochCount              uint64    `json:"epoch_count"`
	EpochBlocks             uint64    `json:"epoch_blocks"`
	StartBlock              uint64    `json:"start_block"`
	EndBlock                uint64    `json:"end_block"`
	FinalizeOffsetBlocks    uint64    `json:"finalize_offset_blocks"`
	TerminalBlock           uint64    `json:"terminal_block"`
	PolicyEffectiveEpoch    uint64    `json:"policy_effective_epoch"`
	PolicyEffectiveBlock    uint64    `json:"policy_effective_block"`
}

type ScenarioEvidenceBundle struct {
	Schema      string               `json:"schema"`
	Result      *ScenarioResult      `json:"result"`
	Observation *ScenarioObservation `json:"observation"`
	Analysis    *AnalysisReport      `json:"analysis"`
}

type scenarioProbe interface {
	Snapshot(context.Context) (*ScenarioObservation, error)
}

type liveScenarioProbe struct {
	cfg                  *ResolvedConfig
	stateDir             string
	client               *http.Client
	trustedEvidenceOwner common.Address
	publicManifestURI    string
	finalSemanticVerify  campaignFinalSemanticVerifier
	campaignResultVerify func(*ResolvedConfig, *ScenarioResult, string) error
}

type scenarioCheck struct {
	ID    string
	Check func(*scenarioEvaluation) (bool, string)
}

type scenarioDefinition struct {
	Name                  string
	GoalEpochs            uint64
	Checks                []scenarioCheck
	Faults                []scenarioFaultSpec
	MatrixHash            string
	AdversarialMatrixHash string
}

type scenarioEvaluation struct {
	Cfg        *ResolvedConfig
	Start      *ScenarioObservation
	Current    *ScenarioObservation
	GoalEpoch  uint64
	Window     *ScenarioAcceptanceWindow
	Definition scenarioDefinition
}

type scenarioRunOptions struct {
	Now                    func() time.Time
	PollInterval           time.Duration
	Timeout                time.Duration
	Roles                  *RoleSecrets
	Publish                bool
	FaultDriver            scenarioFaultDriver
	Adversaries            adversaryCampaign
	Prepare                func(context.Context) error
	ProcessLogs            scenarioProcessLogGate
	CollectFinalSemantic   finalSemanticCampaignInputCollector
	BuildFinalSemantic     finalSemanticCampaignSourceBuilder
	FinalSemanticArtifacts FinalArtifactLoader
	FinalSemanticReader    FinalSemanticChainReaderFactory
	FleetLifecycle         scenarioFleetLifecycle
	Attempt                *scenarioCampaignAttempt
	ProcessSessionID       string
}

type scenarioFleetLifecycleHandoffAuthenticator interface {
	AuthenticateReleaseHandoff([]byte, string, string) error
}

type finalSemanticCampaignSourceBuilder func(context.Context, *ResolvedConfig, string, string, *ScenarioResult, *ScenarioObservation, []*ScenarioObservation) (*FinalSemanticEvidence, error)
type finalSemanticCampaignInputCollector func(context.Context, *ResolvedConfig, string, string, *ScenarioResult, *ScenarioObservation, []*ScenarioObservation) (*FinalSemanticCollectedInputs, error)

// Hash every executable part of a scenario definition. Live release evidence
// and the production-transition gate share this function so a verifier cannot
// accidentally authenticate a different check or fault schedule.
func scenarioDefinitionHash(definition scenarioDefinition) (string, error) {
	return canonicalHashHex(struct {
		Name                  string              `json:"name"`
		GoalEpochs            uint64              `json:"goal_epochs"`
		Checks                []string            `json:"checks"`
		Faults                []scenarioFaultSpec `json:"faults,omitempty"`
		MatrixHash            string              `json:"scenario_matrix_hash,omitempty"`
		AdversarialMatrixHash string              `json:"adversarial_matrix_hash,omitempty"`
	}{Name: definition.Name, GoalEpochs: definition.GoalEpochs, Checks: func() []string {
		ids := make([]string, len(definition.Checks))
		for i := range definition.Checks {
			ids[i] = definition.Checks[i].ID
		}
		return ids
	}(), Faults: definition.Faults, MatrixHash: definition.MatrixHash, AdversarialMatrixHash: definition.AdversarialMatrixHash})
}

// buildScenarioAcceptanceWindow derives the first future full epoch from one
// finalized post-preparation snapshot. It never counts the baseline's partial
// epoch and uses checked arithmetic for every evidence boundary.
func buildScenarioAcceptanceWindow(cfg *ResolvedConfig, definition scenarioDefinition, baseline *ScenarioObservation) (*ScenarioAcceptanceWindow, error) {
	if definition.Name != "release-1.0" && definition.Name != "production-soak" {
		return nil, nil
	}
	if cfg == nil || cfg.Config == nil || cfg.Policy == nil || baseline == nil || baseline.Status == nil || baseline.Status.Contracts == nil {
		return nil, errors.New("scenario acceptance baseline is incomplete")
	}
	contracts := baseline.Status.Contracts
	policy := contracts.Policy
	wantEpochs := uint64(cfg.Config.Scenarios.ShortEpochs)
	wantBlocks := cfg.Policy.Settlement.EpochBlocks
	wantRootWindow := cfg.Policy.Settlement.RootCommitWindowBlocks
	wantFinalize := cfg.Policy.Settlement.FinalizeOffsetBlocks
	wantCloseGrace := cfg.Policy.Settlement.CloseGraceBlocks
	if definition.Name == "production-soak" {
		wantEpochs = uint64(cfg.Config.Scenarios.ProductionEpochs)
		wantBlocks = cfg.Policy.ProductionCadence.EpochBlocks
		wantRootWindow = cfg.Policy.ProductionCadence.RootCommitWindowBlocks
		wantFinalize = cfg.Policy.ProductionCadence.FinalizeOffsetBlocks
		wantCloseGrace = cfg.Policy.ProductionCadence.CloseGraceBlocks
		if policy.EffectiveEpoch == 0 || policy.EffectiveEpoch > contracts.CurrentEpoch {
			return nil, fmt.Errorf("production acceptance baseline epoch %d does not use an active production policy effective at %d", contracts.CurrentEpoch, policy.EffectiveEpoch)
		}
	}
	if wantEpochs == 0 || definition.GoalEpochs != wantEpochs {
		return nil, fmt.Errorf("scenario %s acceptance epochs=%d definition=%d", definition.Name, wantEpochs, definition.GoalEpochs)
	}
	if policy.EpochBlocks != wantBlocks || policy.RootCommitWindowBlocks != wantRootWindow || policy.FinalizeOffsetBlocks != wantFinalize || policy.CloseGraceBlocks != wantCloseGrace {
		return nil, fmt.Errorf("scenario %s baseline policy geometry=%d/%d/%d/%d want=%d/%d/%d/%d", definition.Name, policy.EpochBlocks, policy.RootCommitWindowBlocks, policy.FinalizeOffsetBlocks, policy.CloseGraceBlocks, wantBlocks, wantRootWindow, wantFinalize, wantCloseGrace)
	}
	if contracts.CurrentEpochStart > contracts.FinalizedHead.Number || contracts.FinalizedHead.Number >= contracts.CurrentEpochEnd || contracts.CurrentEpochEnd-contracts.CurrentEpochStart != wantBlocks {
		return nil, fmt.Errorf("scenario %s baseline has inconsistent epoch geometry: epoch=%d head=%d start=%d end=%d blocks=%d", definition.Name, contracts.CurrentEpoch, contracts.FinalizedHead.Number, contracts.CurrentEpochStart, contracts.CurrentEpochEnd, wantBlocks)
	}
	firstEpoch, ok := checkedAdd(contracts.CurrentEpoch, 1)
	if !ok {
		return nil, errors.New("scenario acceptance first epoch overflows uint64")
	}
	span, ok := checkedMul(wantEpochs, wantBlocks)
	if !ok {
		return nil, errors.New("scenario acceptance epoch span overflows uint64")
	}
	endBlock, ok := checkedAdd(contracts.CurrentEpochEnd, span)
	if !ok {
		return nil, errors.New("scenario acceptance end block overflows uint64")
	}
	terminalBlock, ok := checkedAdd(endBlock, wantFinalize)
	if !ok {
		return nil, errors.New("scenario acceptance terminal block overflows uint64")
	}
	return &ScenarioAcceptanceWindow{
		Schema: "urnetwork-sim-acceptance-window-v1", BaselineHead: contracts.FinalizedHead, BaselineObservationHash: baseline.ObservationHash,
		BaselineEpoch: contracts.CurrentEpoch, FirstEpoch: firstEpoch, EpochCount: wantEpochs, EpochBlocks: wantBlocks,
		StartBlock: contracts.CurrentEpochEnd, EndBlock: endBlock, FinalizeOffsetBlocks: wantFinalize, TerminalBlock: terminalBlock,
		PolicyEffectiveEpoch: policy.EffectiveEpoch, PolicyEffectiveBlock: policy.EffectiveBlock,
	}, nil
}

// acceptedEpochsTerminal proves that every operator position in every accepted
// epoch reached the immutable vault's terminal entitlement state. A deliberate
// missed root may carry zero value, but it must still be explicitly finalized.
func acceptedEpochsTerminal(contracts *ContractView, window *ScenarioAcceptanceWindow, operators int) (bool, string) {
	if contracts == nil || window == nil || operators < 1 {
		return false, "accepted epoch terminal state is unavailable"
	}
	epochs := make(map[uint64]EpochView, len(contracts.Epochs))
	for _, epoch := range contracts.Epochs {
		if _, duplicate := epochs[epoch.Epoch]; duplicate {
			return false, fmt.Sprintf("accepted epoch %d is duplicated", epoch.Epoch)
		}
		epochs[epoch.Epoch] = epoch
	}
	for offset := uint64(0); offset < window.EpochCount; offset++ {
		epochID, ok := checkedAdd(window.FirstEpoch, offset)
		if !ok {
			return false, "accepted epoch id overflows uint64"
		}
		epoch, found := epochs[epochID]
		if !found || len(epoch.Operators) != operators {
			return false, fmt.Sprintf("accepted epoch %d operator positions=%d want=%d", epochID, len(epoch.Operators), operators)
		}
		seen := make(map[uint64]bool, operators)
		for _, operator := range epoch.Operators {
			if operator.NoID == 0 || operator.NoID > uint64(operators) || seen[operator.NoID] || operator.Status != 2 {
				return false, fmt.Sprintf("accepted epoch %d operator %d has duplicate/invalid terminal status %d", epochID, operator.NoID, operator.Status)
			}
			seen[operator.NoID] = true
		}
	}
	return true, fmt.Sprintf("epochs=%d-%d operators=%d terminal", window.FirstEpoch, window.FirstEpoch+window.EpochCount-1, operators)
}

// acceptanceScenarioChecks prevents an epoch-number transition, partial
// observation, or stale historical settlement from satisfying a release gate.
func acceptanceScenarioChecks() []scenarioCheck {
	return []scenarioCheck{
		{ID: "complete_epoch_acceptance_window", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Window == nil || e.Current == nil || e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "complete-epoch acceptance window is unavailable"
			}
			contracts := e.Current.Status.Contracts
			endEpoch, ok := checkedAdd(e.Window.FirstEpoch, e.Window.EpochCount)
			passed := ok && contracts.FinalizedHead.Number >= e.Window.TerminalBlock && contracts.CurrentEpoch >= endEpoch
			return passed, fmt.Sprintf("first=%d count=%d end_epoch=%d current_epoch=%d end_block=%d terminal=%d finalized=%d", e.Window.FirstEpoch, e.Window.EpochCount, endEpoch, contracts.CurrentEpoch, e.Window.EndBlock, e.Window.TerminalBlock, contracts.FinalizedHead.Number)
		}},
		{ID: "accepted_operator_epochs_terminal", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current == nil || e.Current.Status == nil {
				return false, "accepted operator epoch state is unavailable"
			}
			return acceptedEpochsTerminal(e.Current.Status.Contracts, e.Window, e.Cfg.Config.Topology.Operators)
		}},
	}
}

func (p *liveScenarioProbe) Snapshot(ctx context.Context) (*ScenarioObservation, error) {
	status, err := Status(ctx, p.cfg, p.stateDir)
	if err != nil {
		return nil, err
	}
	observation := &ScenarioObservation{
		Schema:     "urnetwork-sim-scenario-observation-v1",
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Status:     status,
	}
	identities, expectedSigners := inspectPublicIdentities(p.cfg, p.stateDir)
	observation.PublicIdentityCount = identities
	observation.PublicIdentitiesValid = identities > 0
	identityBytes, identityErr := os.ReadFile(filepath.Join(p.stateDir, "public", "identities.json"))
	minerClients, minerClientsErr := inspectMinerClientIDsBytes(p.cfg, identityBytes)
	if identityErr != nil || minerClientsErr != nil {
		observation.PublicIdentitiesValid = false
	}
	currentEpoch := uint64(0)
	if status.Contracts != nil {
		currentEpoch = status.Contracts.CurrentEpoch
	}
	observation.FleetCommitmentValid, observation.FleetBindingCount, observation.FleetBindingsValid, observation.CandidateFleetUIDs, observation.CandidateFleetHotkeys, observation.CandidateFleetMiners = inspectFleetEvidence(p.cfg, p.stateDir, currentEpoch)
	observation.ReserveValidatorRegistered, observation.ReserveValidatorUID, observation.ReserveDelegateTake, observation.EscrowHotkeyRegistered, observation.EscrowHotkeyUID, observation.NativeCustodyError = inspectNativeCustodyRoles(p.cfg, p.stateDir)
	observation.NativeRewards, observation.NativeRewardsError = inspectNativeRewards(p.cfg, p.cfg.OperationalSubstrate)
	observation.VoluntaryConviction, observation.VoluntaryConvictionValid, observation.VoluntaryConvictionError = inspectVoluntaryConviction(ctx, p.cfg, p.stateDir, status.Contracts)
	if lifecycle, lifecycleErr := loadFleetLifecycleEvidence(p.stateDir); lifecycleErr == nil {
		observation.FleetLifecycle = lifecycle
	} else if !errors.Is(lifecycleErr, os.ErrNotExist) {
		return nil, fmt.Errorf("fleet lifecycle evidence: %w", lifecycleErr)
	}
	if evidence, readErr := func() (*GovernanceDrillEvidence, error) {
		var value GovernanceDrillEvidence
		if err := readJSONFile(filepath.Join(p.stateDir, "public", "governance-drill.json"), &value); err != nil {
			return nil, err
		}
		return &value, nil
	}(); readErr == nil {
		observation.GovernanceDrill = evidence
	} else if !errors.Is(readErr, os.ErrNotExist) {
		observation.GovernanceDrillError = readErr.Error()
	}
	if evidence, readErr := loadPrecompileEvidence(p.stateDir); readErr == nil {
		observation.PrecompileConformance = evidence
		if status.Contracts == nil || status.Contracts.Deployment == nil {
			observation.PrecompileConformanceError = "contract deployment is unavailable"
		} else if validateErr := validatePrecompileEvidenceIdentity(p.cfg, status.Contracts.Deployment, evidence); validateErr != nil {
			observation.PrecompileConformanceError = validateErr.Error()
		} else {
			observation.PrecompileConformanceValid = precompileEvidenceComplete(evidence)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		observation.PrecompileConformanceError = readErr.Error()
	}
	observation.DishonestDeposit, observation.DishonestDepositValid, observation.DishonestDepositError = inspectDishonestDepositEvidence(ctx, p.cfg, p.stateDir, status.Contracts)
	for noID := 1; noID <= p.cfg.Config.Topology.Operators; noID++ {
		observation.Operators = append(observation.Operators, p.inspectOperator(ctx, status.Contracts, noID, expectedSigners[noID], minerClients))
	}
	for validatorID := 1; validatorID <= p.cfg.Config.Topology.Validators; validatorID++ {
		validator := inspectValidatorIntent(p.stateDir, validatorID, p.cfg.Config.Topology.HeadSlots, p.cfg.Config.Topology.fleetCandidates())
		validator.PathProofCounts, err = inspectValidatorPathProofs(p.cfg, p.stateDir, validatorID, observation.Operators)
		if err != nil {
			if validator.Error == "" {
				validator.Error = err.Error()
			} else {
				validator.Error += "; " + err.Error()
			}
		}
		observation.Validators = append(observation.Validators, validator)
	}
	for minerID := 1; minerID <= p.cfg.Config.Topology.Miners; minerID++ {
		observation.Claims = append(observation.Claims, inspectClaimQueue(p.cfg, p.stateDir, minerID))
	}
	observation.ObservationHash = ""
	observation.ObservationHash, err = canonicalHashHex(observation)
	return observation, err
}

func inspectVoluntaryConviction(ctx context.Context, cfg *ResolvedConfig, stateDir string, contracts *ContractView) (*VoluntaryConvictionEvidence, bool, string) {
	b, err := os.ReadFile(filepath.Join(stateDir, "public", "voluntary-conviction.json"))
	if err != nil {
		return nil, false, err.Error()
	}
	roles, err := derivePublicRoles(cfg)
	if err != nil || len(roles.OperatorDepositSigners) == 0 {
		return nil, false, "planned operator-1 deposit signer is unavailable"
	}
	return inspectVoluntaryConvictionBytes(ctx, cfg, b, contracts, cfg.OperationalEVM, roles.OperatorDepositSigners[0])
}

func inspectVoluntaryConvictionBytes(ctx context.Context, cfg *ResolvedConfig, b []byte, contracts *ContractView, endpoint, expectedFunder string) (*VoluntaryConvictionEvidence, bool, string) {
	var evidence VoluntaryConvictionEvidence
	if json.Unmarshal(b, &evidence) != nil {
		return nil, false, "voluntary conviction evidence is not valid JSON"
	}
	txBytes, txErr := hex.DecodeString(strings.TrimPrefix(evidence.TransactionHash, "0x"))
	if evidence.Schema != "urnetwork-voluntary-conviction-evidence-v1" || evidence.DeploymentID != cfg.Config.Deployment.DeploymentID || evidence.NoID != 1 || evidence.AmountRao != fmt.Sprint(cfg.Config.Scenarios.VoluntaryConvictionRao) || evidence.BeforeConvictionRao != "0" || evidence.AfterConvictionRao != evidence.AmountRao || !strings.EqualFold(evidence.PolicyHash, cfg.PolicyHash) || txErr != nil || len(txBytes) != 32 {
		return &evidence, false, "voluntary conviction evidence identity is invalid"
	}
	if contracts == nil || contracts.Deployment == nil {
		return &evidence, false, "contract deployment is unavailable"
	}
	if expectedFunder == "" || !strings.EqualFold(evidence.Funder, expectedFunder) {
		return &evidence, false, "voluntary conviction funder is not the planned operator-1 deposit signer"
	}
	client, err := dialConfiguredEVMClient(ctx, cfg, endpoint)
	if err != nil {
		return &evidence, false, err.Error()
	}
	defer client.Close()
	receipt, err := client.TransactionReceipt(ctx, common.HexToHash(evidence.TransactionHash))
	if err != nil || receipt.Status != 1 || receipt.BlockNumber == nil || receipt.BlockNumber.Uint64() != evidence.FinalizedBlock || !strings.EqualFold(receipt.BlockHash.Hex(), evidence.FinalizedHash) {
		return &evidence, false, fmt.Sprintf("voluntary conviction receipt mismatch: %v", err)
	}
	head, err := finalizedEVMHead(ctx, client)
	if err != nil || head.Number < evidence.FinalizedBlock {
		return &evidence, false, fmt.Sprintf("voluntary conviction is not finalized: %v", err)
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return &evidence, false, err.Error()
	}
	event := parsed.Events["ConvictionAdded"]
	validEvent := false
	for _, log := range receipt.Logs {
		if log.Address != contracts.Deployment.CoordinatorProxy || len(log.Topics) != 4 || log.Topics[0] != event.ID || log.Topics[1].Big().Cmp(big.NewInt(1)) != 0 || !log.Topics[2].Big().IsUint64() || log.Topics[2].Big().Uint64() != evidence.Epoch || !strings.EqualFold(common.BytesToAddress(log.Topics[3].Bytes()[12:]).Hex(), evidence.Funder) {
			continue
		}
		values, unpackErr := event.Inputs.NonIndexed().Unpack(log.Data)
		if unpackErr != nil || len(values) != 3 {
			continue
		}
		amount, amountOK := values[0].(*big.Int)
		policyHash, policyOK := values[1].([32]byte)
		nonce, nonceOK := values[2].(*big.Int)
		if amountOK && policyOK && nonceOK && amount.String() == evidence.AmountRao && "0x"+hex.EncodeToString(policyHash[:]) == strings.ToLower(evidence.PolicyHash) && nonce.String() == evidence.Nonce {
			validEvent = true
			break
		}
	}
	if !validEvent {
		return &evidence, false, "finalized receipt lacks the recorded ConvictionAdded event"
	}
	values, err := contractCallAt(ctx, client, contracts.Deployment.CoordinatorProxy, parsed, "cumulativeConviction", head.Number, big.NewInt(1))
	if err != nil || len(values) != 1 {
		return &evidence, false, fmt.Sprintf("read current cumulative conviction: %v", err)
	}
	conviction, ok := values[0].(*big.Int)
	if !ok || conviction.Cmp(new(big.Int).SetUint64(cfg.Config.Scenarios.VoluntaryConvictionRao)) < 0 {
		return &evidence, false, "current cumulative conviction is below the voluntary amount"
	}
	return &evidence, true, ""
}

func inspectNativeCustodyRoles(cfg *ResolvedConfig, stateDir string) (bool, uint16, *uint16, bool, uint16, string) {
	b, err := os.ReadFile(filepath.Join(stateDir, "public", "identities.json"))
	if err != nil {
		return false, 0, nil, false, 0, err.Error()
	}
	deployment, err := loadContractDeployment(stateDir)
	if err != nil {
		return false, 0, nil, false, 0, err.Error()
	}
	return inspectNativeCustodyRolesBytes(cfg, b, cfg.OperationalSubstrate, deployment.RegistrationRoleGeneration)
}

func inspectNativeRewards(cfg *ResolvedConfig, endpoint string) (*NativeRewardObservation, string) {
	chain, err := crv4.DialChain(endpoint)
	if err != nil {
		return nil, err.Error()
	}
	defer chain.API.Client.Close()
	finalized, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return nil, err.Error()
	}
	header, err := chain.API.RPC.Chain.GetHeader(finalized)
	if err != nil {
		return nil, err.Error()
	}
	read := func(storage string, value any) error {
		key, keyErr := gsrpcTypes.CreateStorageKey(chain.Meta, crv4.PalletName, storage, netuidArg(cfg.Netuid))
		if keyErr != nil {
			return keyErr
		}
		return readRequiredStorageAt(chain, key, crv4.PalletName, storage, value, finalized)
	}
	var count gsrpcTypes.U16
	if err := read("SubnetworkN", &count); err != nil {
		return nil, err.Error()
	}
	var emission []gsrpcTypes.U64
	if err := read("Emission", &emission); err != nil {
		return nil, err.Error()
	}
	var incentive, dividends []gsrpcTypes.U16
	if err := read("Incentive", &incentive); err != nil {
		return nil, err.Error()
	}
	if err := read("Dividends", &dividends); err != nil {
		return nil, err.Error()
	}
	// Read every UID-to-hotkey mapping and its TotalHotkeyAlpha value at the
	// same finalized state root. Emission is an epoch vector, whereas this
	// monotonically owned stake is the durable native payout channel used by
	// the final report. The shared batch reader keeps this to two RPC requests
	// rather than one request per UID.
	facts, err := readExistingUIDFactsAt(chain, cfg.Netuid, finalized, SubnetTopologyFacts{UIDCount: uint16(count)})
	if err != nil {
		return nil, err.Error()
	}
	result, err := nativeRewardObservationFromFinalizedState(
		ChainHead{Number: uint64(header.Number), Hash: finalized.Hex()},
		emission,
		incentive,
		dividends,
		facts,
	)
	if err != nil {
		return nil, err.Error()
	}
	return result, ""
}

// Bind four runtime surfaces to one UID-indexed finalized snapshot. The UID
// checks prevent a reordered or partial map response from being mistaken for
// a different neuron's durable payout balance.
func nativeRewardObservationFromFinalizedState(head ChainHead, emission []gsrpcTypes.U64, incentive, dividends []gsrpcTypes.U16, facts []ExistingUIDFact) (*NativeRewardObservation, error) {
	count := len(emission)
	if head.Number == 0 || head.Hash == "" || count == 0 || len(incentive) != count || len(dividends) != count || len(facts) != count {
		return nil, fmt.Errorf("native reward snapshot head or vector lengths emission/incentive/dividends/stake=%d/%d/%d/%d are incomplete", count, len(incentive), len(dividends), len(facts))
	}
	result := &NativeRewardObservation{
		FinalizedHead:       head,
		EmissionRao:         make([]string, count),
		Incentive:           make([]uint16, count),
		Dividends:           make([]uint16, count),
		TotalHotkeyAlphaRao: make([]string, count),
	}
	for index := range emission {
		if facts[index].UID != uint16(index) {
			return nil, fmt.Errorf("native reward stake row %d identifies UID %d", index, facts[index].UID)
		}
		result.EmissionRao[index] = strconv.FormatUint(uint64(emission[index]), 10)
		result.Incentive[index] = uint16(incentive[index])
		result.Dividends[index] = uint16(dividends[index])
		result.TotalHotkeyAlphaRao[index] = strconv.FormatUint(facts[index].TotalHotkeyAlphaRao, 10)
	}
	return result, nil
}

func inspectNativeCustodyRolesBytes(cfg *ResolvedConfig, b []byte, endpoint string, generation uint64) (bool, uint16, *uint16, bool, uint16, string) {
	var identities struct {
		Substrate map[string]map[string]string `json:"substrate"`
	}
	if json.Unmarshal(b, &identities) != nil {
		return false, 0, nil, false, 0, "invalid public identities"
	}
	decode := func(label string) ([32]byte, error) {
		var out [32]byte
		raw, err := hex.DecodeString(strings.TrimPrefix(identities.Substrate[label]["public_key"], "0x"))
		if err != nil || len(raw) != 32 {
			return out, fmt.Errorf("%s public key is invalid", label)
		}
		copy(out[:], raw)
		return out, nil
	}
	reserve, err := decode("reserve-hotkey")
	if err != nil {
		return false, 0, nil, false, 0, err.Error()
	}
	escrow, err := decode(escrowHotkeyLabelForGeneration(generation))
	if err != nil {
		return false, 0, nil, false, 0, err.Error()
	}
	chain, err := crv4.DialChain(endpoint)
	if err != nil {
		return false, 0, nil, false, 0, err.Error()
	}
	defer chain.API.Client.Close()
	finalized, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return false, 0, nil, false, 0, err.Error()
	}
	readUID := func(hotkey [32]byte) (bool, uint16, error) {
		key, err := gsrpcTypes.CreateStorageKey(chain.Meta, crv4.PalletName, "Uids", netuidArg(cfg.Netuid), hotkey[:])
		if err != nil {
			return false, 0, err
		}
		var uid gsrpcTypes.U16
		ok, err := chain.API.RPC.State.GetStorage(key, &uid, finalized)
		return ok, uint16(uid), err
	}
	reserveOK, reserveUID, err := readUID(reserve)
	if err != nil {
		return false, 0, nil, false, 0, err.Error()
	}
	escrowOK, escrowUID, err := readUID(escrow)
	if err != nil {
		return reserveOK, reserveUID, nil, false, 0, err.Error()
	}
	takeKey, err := gsrpcTypes.CreateStorageKey(chain.Meta, crv4.PalletName, "Delegates", reserve[:])
	if err != nil {
		return reserveOK, reserveUID, nil, escrowOK, escrowUID, err.Error()
	}
	var take gsrpcTypes.U16
	if _, err := chain.API.RPC.State.GetStorage(takeKey, &take, finalized); err != nil {
		return reserveOK, reserveUID, nil, escrowOK, escrowUID, err.Error()
	}
	takeValue := uint16(take)
	return reserveOK, reserveUID, &takeValue, escrowOK, escrowUID, ""
}

func inspectPublicIdentities(cfg *ResolvedConfig, stateDir string) (int, map[int]string) {
	b, err := os.ReadFile(filepath.Join(stateDir, "public", "identities.json"))
	if err != nil {
		return 0, nil
	}
	return inspectPublicIdentityBytes(cfg, b)
}

func inspectPublicIdentityBytes(cfg *ResolvedConfig, b []byte) (int, map[int]string) {
	return inspectPublicIdentityBytesForManifest(b, cfg.Config.Deployment.DeploymentID, cfg.Config.Topology)
}

func inspectPublicIdentityBytesForManifest(b []byte, deploymentID string, topology TopologyConfig) (int, map[int]string) {
	if !json.Valid(b) {
		return 0, nil
	}
	lower := strings.ToLower(string(b))
	if strings.Contains(lower, "private_key") || strings.Contains(lower, "seed_hex") || strings.Contains(lower, "wallet_secret") {
		return 0, nil
	}
	var value struct {
		Schema       string                       `json:"schema"`
		DeploymentID string                       `json:"deployment_id"`
		EVM          map[string]string            `json:"evm"`
		Substrate    map[string]map[string]string `json:"substrate"`
		Clients      map[string]map[string]string `json:"clients"`
	}
	if json.Unmarshal(b, &value) != nil || value.Schema != "urnetwork-sim-public-identities-v1" || value.DeploymentID != deploymentID {
		return 0, nil
	}
	minimumEVM := 5 + 4*topology.Operators
	minimumSubstrate := 2 + 2*topology.fleetCandidates() + 2*topology.ChurnFloorUIDs + 3*topology.Operators + (2*topology.Validators - 1) + topology.Miners
	minimumClients := topology.Miners + topology.Validators*topology.Operators
	if len(value.EVM) < minimumEVM || len(value.Substrate) < minimumSubstrate || len(value.Clients) < minimumClients {
		return 0, nil
	}
	for _, client := range value.Clients {
		if len(strings.TrimPrefix(client["client_id"], "0x")) != 32 || len(strings.TrimPrefix(client["client_key"], "0x")) != 64 {
			return 0, nil
		}
	}
	signers := map[int]string{}
	for noID := 1; noID <= topology.Operators; noID++ {
		signers[noID] = value.EVM[fmt.Sprintf("operator-%d-artifact", noID)]
		if signers[noID] == "" {
			return 0, nil
		}
	}
	return len(value.EVM) + len(value.Substrate) + len(value.Clients), signers
}

func inspectMinerClientIDsBytes(cfg *ResolvedConfig, b []byte) (map[[16]byte]int, error) {
	if cfg == nil {
		return nil, errors.New("miner identity configuration is unavailable")
	}
	var value struct {
		Schema       string                       `json:"schema"`
		DeploymentID string                       `json:"deployment_id"`
		Clients      map[string]map[string]string `json:"clients"`
	}
	if json.Unmarshal(b, &value) != nil || value.Schema != "urnetwork-sim-public-identities-v1" || value.DeploymentID != cfg.Config.Deployment.DeploymentID {
		return nil, errors.New("public miner identities are invalid")
	}
	result := make(map[[16]byte]int, cfg.Config.Topology.Miners)
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		encoded := value.Clients[fmt.Sprintf("miner-%d", miner)]["client_id"]
		raw, ok := evidenceFixedHex(encoded, 16)
		if !ok {
			return nil, fmt.Errorf("miner-%d client id is invalid", miner)
		}
		var clientID [16]byte
		copy(clientID[:], raw)
		if prior := result[clientID]; prior != 0 {
			return nil, fmt.Errorf("miners %d and %d share one client id", prior, miner)
		}
		result[clientID] = miner
	}
	return result, nil
}

func inspectFleetEvidence(cfg *ResolvedConfig, stateDir string, epoch uint64) (bool, int, bool, []uint16, []string, [][]int) {
	setup := map[string]json.RawMessage{}
	descriptors, err := fleetLifecycleEvidenceDescriptors(cfg, stateDir, epoch)
	if err != nil || len(descriptors) != cfg.Config.Topology.fleetCandidates() {
		return false, 0, false, nil, nil, nil
	}
	minerGroups := make([][]int, 0, len(descriptors))
	for index, descriptor := range descriptors {
		fleet := index + 1
		paths := map[string]string{
			fmt.Sprintf("fleet_%d_manifest", fleet):   filepath.Join(stateDir, "public", descriptor.ManifestName),
			fmt.Sprintf("fleet_%d_commitment", fleet): filepath.Join(stateDir, "public", descriptor.CommitmentName),
		}
		for member, name := range descriptor.BindingNames {
			paths[fmt.Sprintf("fleet_%d_binding_%d", fleet, member+1)] = filepath.Join(stateDir, "public", name)
		}
		for name, path := range paths {
			b, readErr := os.ReadFile(path)
			if readErr != nil {
				return false, 0, false, nil, nil, nil
			}
			setup[name] = b
		}
		minerGroups = append(minerGroups, append([]int(nil), descriptor.MinerIDs...))
	}
	deployment, err := loadContractDeployment(stateDir)
	if err != nil {
		return false, 0, false, nil, nil, nil
	}
	commitments, count, bindings, uids := inspectFleetEvidenceBytes(cfg, setup, deployment.CoordinatorProxy)
	if !bindings || len(uids) != len(minerGroups) {
		return commitments, count, false, uids, nil, nil
	}
	hotkeys := make([]string, 0, len(descriptors))
	for fleet := 1; fleet <= len(descriptors); fleet++ {
		manifest, parseErr := protocol.ParseFleetManifest(setup[fmt.Sprintf("fleet_%d_manifest", fleet)])
		if parseErr != nil {
			return commitments, count, false, uids, nil, nil
		}
		hotkeys = append(hotkeys, fleetLifecycleHex(manifest.Hotkey))
	}
	return commitments, count, true, uids, hotkeys, minerGroups
}

func evidenceFixedHex(value string, size int) ([]byte, bool) {
	b, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	return b, err == nil && len(b) == size && value == "0x"+hex.EncodeToString(b)
}

func inspectFleetEvidenceBytes(cfg *ResolvedConfig, setup map[string]json.RawMessage, expectedCoordinator common.Address) (bool, int, bool, []uint16) {
	allCommitmentsValid, allBindingsValid := true, true
	totalBindings := 0
	uids := make([]uint16, 0, cfg.Config.Topology.fleetCandidates())
	seenUIDs := map[uint16]bool{}
	seenClients := map[[16]byte]bool{}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		commitmentOK, count, bindingsOK, uid, clients := inspectOneFleetEvidenceBytes(cfg, setup, expectedCoordinator, fleet)
		allCommitmentsValid = allCommitmentsValid && commitmentOK
		allBindingsValid = allBindingsValid && bindingsOK
		totalBindings += count
		if bindingsOK {
			if seenUIDs[uid] {
				allBindingsValid = false
			}
			seenUIDs[uid] = true
			uids = append(uids, uid)
		}
		for _, client := range clients {
			if seenClients[client] {
				allBindingsValid = false
			}
			seenClients[client] = true
		}
	}
	return allCommitmentsValid, totalBindings, allBindingsValid && len(uids) == cfg.Config.Topology.fleetCandidates(), uids
}

func inspectOneFleetEvidenceBytes(cfg *ResolvedConfig, setup map[string]json.RawMessage, expectedCoordinator common.Address, fleetIndex int) (bool, int, bool, uint16, [][16]byte) {
	prefix := fmt.Sprintf("fleet_%d", fleetIndex)
	manifest, err := protocol.ParseFleetManifest(setup[prefix+"_manifest"])
	if err != nil || manifest.ChainID != cfg.ChainID || manifest.Netuid != cfg.Netuid || common.BytesToAddress(manifest.Coordinator[:]) != expectedCoordinator || len(manifest.Members) != cfg.Config.Topology.ClientsPerHeadFleet {
		return false, 0, false, 0, nil
	}
	commitmentHash, err := manifest.CommitmentHash()
	if err != nil {
		return false, 0, false, 0, nil
	}
	commitmentBytes := setup[prefix+"_commitment"]
	var commitment FleetCommitmentEvidence
	if json.Unmarshal(commitmentBytes, &commitment) != nil {
		return false, 0, false, 0, nil
	}
	commitmentValue, commitmentOK := evidenceFixedHex(commitment.CommitmentHash, 32)
	hotkeyValue, hotkeyOK := evidenceFixedHex(commitment.Hotkey, 32)
	_, extrinsicHashOK := evidenceFixedHex(commitment.ExtrinsicHash, 32)
	_, finalizedHashOK := evidenceFixedHex(commitment.FinalizedBlockHash, 32)
	if commitment.Schema != fleetCommitmentEvidenceSchemaV2 || commitment.CommitmentBlock == 0 || commitment.FinalizedBlock != commitment.CommitmentBlock || !commitmentOK || !hotkeyOK || !extrinsicHashOK || !finalizedHashOK || !bytes.Equal(commitmentValue, commitmentHash[:]) || !bytes.Equal(hotkeyValue, manifest.Hotkey[:]) {
		return false, 0, false, 0, nil
	}
	valid := true
	count := 0
	var fleetUID uint16
	fleetUIDSet := false
	members := map[[16]byte]protocol.FleetMember{}
	for _, member := range manifest.Members {
		members[member.ClientID] = member
	}
	seen := map[[16]byte]bool{}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		b := setup[fmt.Sprintf("%s_binding_%d", prefix, member)]
		if len(b) == 0 {
			valid = false
			continue
		}
		var binding FleetBindingEvidence
		if json.Unmarshal(b, &binding) != nil || binding.Schema != "urnetwork-fleet-binding-evidence-v1" || binding.BlockNumber == 0 || binding.Generation != manifest.Generation || binding.ValidFromEpoch == 0 || binding.ValidToEpoch < binding.ValidFromEpoch || binding.ValidToEpoch-binding.ValidFromEpoch+1 > cfg.Policy.Binding.MaximumValidityEpochs {
			valid = false
			continue
		}
		clientID, idOK := evidenceFixedHex(binding.ClientID, 16)
		clientKey, keyOK := evidenceFixedHex(binding.ClientKey, 32)
		fleetID, fleetOK := evidenceFixedHex(binding.FleetID, 32)
		hotkey, hotOK := evidenceFixedHex(binding.Hotkey, 32)
		bindingCommitment, hashOK := evidenceFixedHex(binding.CommitmentHash, 32)
		digestValue, digestOK := evidenceFixedHex(binding.BindingDigest, 32)
		clientSignature, clientSigOK := evidenceFixedHex(binding.ClientSignature, 64)
		hotkeySignature, hotSigOK := evidenceFixedHex(binding.HotkeySignature, 64)
		_, txOK := evidenceFixedHex(binding.TransactionHash, 32)
		_, blockOK := evidenceFixedHex(binding.BlockHash, 32)
		if !idOK || !keyOK || !fleetOK || !hotOK || !hashOK || !digestOK || !clientSigOK || !hotSigOK || !txOK || !blockOK {
			valid = false
			continue
		}
		var canonical protocol.FleetBinding
		canonical.ChainID, canonical.Netuid, canonical.Coordinator = manifest.ChainID, manifest.Netuid, manifest.Coordinator
		copy(canonical.ClientID[:], clientID)
		copy(canonical.ClientKey[:], clientKey)
		copy(canonical.FleetID[:], fleetID)
		copy(canonical.Hotkey[:], hotkey)
		copy(canonical.CommitmentHash[:], bindingCommitment)
		canonical.Generation, canonical.ValidFromEpoch, canonical.ValidToEpoch = binding.Generation, binding.ValidFromEpoch, binding.ValidToEpoch
		digest, digestErr := canonical.Digest()
		manifestMember, memberOK := members[canonical.ClientID]
		if digestErr != nil || !memberOK || seen[canonical.ClientID] || manifestMember.ClientKey != canonical.ClientKey || canonical.FleetID != manifest.FleetID || canonical.Hotkey != manifest.Hotkey || canonical.CommitmentHash != commitmentHash || !bytes.Equal(digestValue, digest[:]) || !canonical.VerifyClient(clientSignature) || !canonical.VerifyHotkey(hotkeySignature) {
			valid = false
			continue
		}
		if fleetUIDSet && fleetUID != binding.UID {
			valid = false
			continue
		}
		fleetUID, fleetUIDSet = binding.UID, true
		seen[canonical.ClientID] = true
		count++
	}
	clients := make([][16]byte, 0, len(seen))
	for client := range seen {
		clients = append(clients, client)
	}
	return true, count, valid && count == cfg.Config.Topology.ClientsPerHeadFleet && fleetUIDSet, fleetUID, clients
}

func (p *liveScenarioProbe) get(ctx context.Context, url string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	client := p.client
	if client == nil {
		client = http.DefaultClient
	}
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noRedirect.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(b)) > limit {
		return nil, resp.StatusCode, errors.New("response exceeds evidence size limit")
	}
	if resp.StatusCode/100 != 2 {
		return b, resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return b, resp.StatusCode, nil
}

func bytesSHA256(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

type payoutTierMembership struct {
	Providers             int
	CandidateProviders    int
	CandidateHeadExcluded int
	CandidateLeaves       int
	PoolTailProviders     int
	PoolTailHeadExcluded  int
	PoolTailLeaves        int
}

func summarizePayoutTierMembership(cfg *ResolvedConfig, noID int, artifact *payoutArtifact, minerClients map[[16]byte]int) (payoutTierMembership, error) {
	candidates := make(map[int]bool, cfg.Config.Topology.fleetCandidateMiners())
	for miner := 1; miner <= cfg.Config.Topology.fleetCandidateMiners(); miner++ {
		candidates[miner] = true
	}
	return summarizePayoutTierMembershipForCandidates(cfg, noID, artifact, minerClients, candidates)
}

func summarizePayoutTierMembershipForCandidates(cfg *ResolvedConfig, noID int, artifact *payoutArtifact, minerClients map[[16]byte]int, candidates map[int]bool) (payoutTierMembership, error) {
	var result payoutTierMembership
	if cfg == nil || artifact == nil || len(minerClients) != cfg.Config.Topology.Miners || artifact.NoID != uint64(noID) || len(candidates) != cfg.Config.Topology.fleetCandidateMiners() {
		return result, errors.New("payout tier membership inputs are incomplete")
	}
	expectedCandidate, expectedTail := 0, 0
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		if operatorForMiner(cfg, miner) != noID {
			continue
		}
		if candidates[miner] {
			expectedCandidate++
		} else {
			expectedTail++
		}
	}
	leaves := make(map[[16]byte]bool, len(artifact.Leaves))
	for _, leaf := range artifact.Leaves {
		miner := minerClients[leaf.ClientID]
		if miner == 0 || operatorForMiner(cfg, miner) != noID || leaves[leaf.ClientID] {
			return result, fmt.Errorf("artifact leaf has an unknown, foreign, or duplicate client id")
		}
		leaves[leaf.ClientID] = true
		if candidates[miner] {
			result.CandidateLeaves++
		} else {
			result.PoolTailLeaves++
		}
	}
	providers := make(map[[16]byte]bool, len(artifact.Providers))
	for _, provider := range artifact.Providers {
		miner := minerClients[provider.ClientID]
		if miner == 0 || operatorForMiner(cfg, miner) != noID || providers[provider.ClientID] {
			return result, fmt.Errorf("artifact provider has an unknown, foreign, or duplicate client id")
		}
		providers[provider.ClientID] = true
		result.Providers++
		if candidates[miner] {
			result.CandidateProviders++
			if provider.HeadExcluded {
				result.CandidateHeadExcluded++
			}
			if !provider.HeadExcluded || provider.ExclusionReason != "head_fleet_active" || leaves[provider.ClientID] {
				return result, fmt.Errorf("candidate miner %d is not exclusively excluded from its pool", miner)
			}
		} else {
			result.PoolTailProviders++
			if provider.HeadExcluded {
				result.PoolTailHeadExcluded++
				return result, fmt.Errorf("pool-tail miner %d is incorrectly head-excluded", miner)
			}
		}
	}
	if result.CandidateProviders != expectedCandidate || result.CandidateHeadExcluded != expectedCandidate || result.PoolTailProviders != expectedTail || result.CandidateLeaves != 0 || result.PoolTailHeadExcluded != 0 || result.PoolTailLeaves == 0 {
		return result, fmt.Errorf("tier membership candidate=%d/%d excluded=%d leaves=%d tail=%d/%d excluded=%d leaves=%d", result.CandidateProviders, expectedCandidate, result.CandidateHeadExcluded, result.CandidateLeaves, result.PoolTailProviders, expectedTail, result.PoolTailHeadExcluded, result.PoolTailLeaves)
	}
	return result, nil
}

func fleetLifecyclePayoutEpochs(evidence *FleetLifecycleEvidence) map[uint64]bool {
	epochs := map[uint64]bool{}
	if evidence == nil {
		return epochs
	}
	if evidence.FallbackEffectiveEpoch != 0 {
		epochs[evidence.FallbackEffectiveEpoch] = true
	}
	if evidence.ProviderEffectiveEpoch != 0 {
		epochs[evidence.ProviderEffectiveEpoch] = true
	}
	return epochs
}

func fleetLifecycleTrackedClientIDs(cfg *ResolvedConfig, noID int, minerClients map[[16]byte]int) ([][16]byte, error) {
	if cfg == nil || cfg.Config == nil || noID < 1 || noID > cfg.Config.Topology.Operators || len(minerClients) != cfg.Config.Topology.Miners {
		return nil, errors.New("lifecycle payout client identity inputs are incomplete")
	}
	if err := validateFleetLifecycleTopology(cfg.Config.Topology); err != nil {
		return nil, err
	}
	clientByMiner := make(map[int][16]byte, len(minerClients))
	for clientID, miner := range minerClients {
		if miner < 1 || miner > cfg.Config.Topology.Miners {
			return nil, errors.New("lifecycle payout client identity names an out-of-range miner")
		}
		if _, duplicate := clientByMiner[miner]; duplicate {
			return nil, errors.New("lifecycle payout client identity duplicates a miner")
		}
		clientByMiner[miner] = clientID
	}
	tracked := map[int]bool{}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		tracked[fleetMemberMinerIndex(cfg, fleetLifecycleTargetFleet, member)] = true
		tracked[fleetMemberMinerIndex(cfg, fleetLifecycleCompanionFleet, member)] = true
		fallback, err := fleetLifecycleFallbackMinerIndex(cfg, member)
		if err != nil {
			return nil, err
		}
		tracked[fallback] = true
	}
	wantTracked := 3 * cfg.Config.Topology.ClientsPerHeadFleet
	if len(tracked) != wantTracked {
		return nil, fmt.Errorf("lifecycle payout client identity count=%d, want %d", len(tracked), wantTracked)
	}
	result := make([][16]byte, 0, wantTracked)
	for miner := range tracked {
		clientID, ok := clientByMiner[miner]
		if !ok {
			return nil, fmt.Errorf("lifecycle payout client identity for miner %d is absent", miner)
		}
		if operatorForMiner(cfg, miner) == noID {
			result = append(result, clientID)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("operator %d owns no lifecycle payout clients", noID)
	}
	sort.Slice(result, func(i, j int) bool { return bytes.Compare(result[i][:], result[j][:]) < 0 })
	return result, nil
}

func validateOperatorLifecyclePayoutArtifactObservation(row OperatorLifecyclePayoutArtifactObservation) error {
	contentHash, err := hex.DecodeString(strings.TrimPrefix(row.ContentHash, "sha256:"))
	if row.Epoch == 0 || row.NoID == 0 || err != nil || len(contentHash) != 32 || row.ContentHash != "sha256:"+hex.EncodeToString(contentHash) {
		return errors.New("lifecycle payout artifact observation has incomplete identity")
	}
	if raw, ok := evidenceFixedHex(row.PayoutRoot, 32); !ok || row.PayoutRoot != "0x"+hex.EncodeToString(raw) {
		return errors.New("lifecycle payout artifact observation has a noncanonical payout root")
	}
	if len(row.Clients) == 0 {
		return errors.New("lifecycle payout artifact observation has no tracked clients")
	}
	prior := ""
	for _, client := range row.Clients {
		raw, ok := evidenceFixedHex(client.ClientID, 16)
		canonical := ""
		if ok {
			canonical = "0x" + hex.EncodeToString(raw)
		}
		if !ok || client.ClientID != canonical || client.ClientID <= prior {
			return errors.New("lifecycle payout artifact observation clients are noncanonical, duplicated, or unsorted")
		}
		if client.Leaf == client.HeadExcluded {
			return fmt.Errorf("lifecycle payout client %s is not in exactly one payout tier", client.ClientID)
		}
		prior = client.ClientID
	}
	return nil
}

func compactLifecyclePayoutArtifact(cfg *ResolvedConfig, noID int, artifact *payoutArtifact, minerClients map[[16]byte]int) (OperatorLifecyclePayoutArtifactObservation, error) {
	if artifact == nil || artifact.NoID != uint64(noID) {
		return OperatorLifecyclePayoutArtifactObservation{}, errors.New("lifecycle payout artifact belongs to another operator")
	}
	tracked, err := fleetLifecycleTrackedClientIDs(cfg, noID, minerClients)
	if err != nil {
		return OperatorLifecyclePayoutArtifactObservation{}, err
	}
	leaves := make(map[[16]byte]bool, len(artifact.Leaves))
	for _, leaf := range artifact.Leaves {
		if leaves[leaf.ClientID] {
			return OperatorLifecyclePayoutArtifactObservation{}, errors.New("lifecycle payout artifact duplicates a leaf client")
		}
		leaves[leaf.ClientID] = true
	}
	providers := make(map[[16]byte]payoutartifact.ProviderInput, len(artifact.Providers))
	for _, provider := range artifact.Providers {
		if _, duplicate := providers[provider.ClientID]; duplicate {
			return OperatorLifecyclePayoutArtifactObservation{}, errors.New("lifecycle payout artifact duplicates a provider client")
		}
		providers[provider.ClientID] = provider
	}
	row := OperatorLifecyclePayoutArtifactObservation{
		Epoch: artifact.Epoch, NoID: artifact.NoID, ContentHash: artifact.ContentHash,
		PayoutRoot: fleetLifecycleHex(artifact.PayoutRoot),
		Clients:    make([]OperatorPayoutClientTierObservation, 0, len(tracked)),
	}
	for _, clientID := range tracked {
		provider, ok := providers[clientID]
		if !ok {
			return OperatorLifecyclePayoutArtifactObservation{}, fmt.Errorf("lifecycle payout artifact omits tracked client %s", fleetLifecycleHex16(clientID))
		}
		leaf, excluded := leaves[clientID], provider.HeadExcluded
		if leaf == excluded || leaf && !provider.Eligible || excluded && provider.ExclusionReason != "head_fleet_active" {
			return OperatorLifecyclePayoutArtifactObservation{}, fmt.Errorf("lifecycle payout client %s has invalid exclusive tier state", fleetLifecycleHex16(clientID))
		}
		row.Clients = append(row.Clients, OperatorPayoutClientTierObservation{ClientID: fleetLifecycleHex16(clientID), Leaf: leaf, HeadExcluded: excluded})
	}
	if err := validateOperatorLifecyclePayoutArtifactObservation(row); err != nil {
		return OperatorLifecyclePayoutArtifactObservation{}, err
	}
	return row, nil
}

func (p *liveScenarioProbe) inspectOperator(ctx context.Context, contracts *ContractView, noID int, expectedSigner string, minerClients map[[16]byte]int) OperatorObservation {
	base := fmt.Sprintf("http://127.0.0.1:%d", 18080+noID)
	return p.inspectOperatorAt(ctx, contracts, noID, expectedSigner, base, minerClients)
}

func (p *liveScenarioProbe) inspectOperatorAt(ctx context.Context, contracts *ContractView, noID int, expectedSigner, base string, minerClients map[[16]byte]int) OperatorObservation {
	base = strings.TrimSuffix(base, "/")
	o := OperatorObservation{NoID: noID, APIURL: base}
	if contracts != nil {
		for _, epoch := range contracts.Epochs {
			for _, operator := range epoch.Operators {
				if operator.NoID == uint64(noID) && operator.Status == 2 {
					o.ExpectedFinalizedArtifacts++
				}
			}
		}
	}
	_, statusCode, statusErr := p.get(ctx, base+"/status", 1*1024*1024)
	o.StatusCode = statusCode
	o.Healthy = statusErr == nil
	var problems []string
	if statusErr != nil {
		problems = append(problems, "status: "+statusErr.Error())
	}
	keysBytes, _, err := p.get(ctx, base+"/verify/keys", 1*1024*1024)
	if err != nil {
		problems = append(problems, "verify keys: "+err.Error())
	} else {
		var keys struct {
			Keys []struct {
				ServerKeyID byte   `json:"server_key_id"`
				PublicKey   []byte `json:"public_key"`
			} `json:"keys"`
		}
		seen := map[byte]bool{}
		if json.Unmarshal(keysBytes, &keys) != nil || len(keys.Keys) == 0 {
			problems = append(problems, "verify keys: invalid response")
		} else {
			for _, key := range keys.Keys {
				if len(key.PublicKey) != 32 || seen[key.ServerKeyID] {
					problems = append(problems, "verify keys: invalid/duplicate key")
					continue
				}
				seen[key.ServerKeyID] = true
				o.VerifyKeyIDs = append(o.VerifyKeyIDs, key.ServerKeyID)
				o.VerifyKeys = append(o.VerifyKeys, VerifyKeyObservation{ServerKeyID: key.ServerKeyID, PublicKey: append([]byte(nil), key.PublicKey...)})
			}
			sort.Slice(o.VerifyKeyIDs, func(i, j int) bool { return o.VerifyKeyIDs[i] < o.VerifyKeyIDs[j] })
			sort.Slice(o.VerifyKeys, func(i, j int) bool { return o.VerifyKeys[i].ServerKeyID < o.VerifyKeys[j].ServerKeyID })
		}
	}
	statsBytes, _, err := p.get(ctx, base+"/verify/stats?limit=100000", 32*1024*1024)
	if err != nil {
		problems = append(problems, "stats: "+err.Error())
	} else {
		var stats struct {
			Schema     string `json:"schema"`
			PolicyHash string `json:"policy_hash"`
			Rows       []struct {
				Assignments   uint64 `json:"assignments"`
				Confirmations uint64 `json:"confirmations"`
			} `json:"rows"`
		}
		if json.Unmarshal(statsBytes, &stats) != nil || stats.Schema != "urnetwork-verify-stats-index-v1" {
			problems = append(problems, "stats: invalid schema")
		} else {
			o.StatsRows, o.StatsHash, o.StatsPolicyHash = len(stats.Rows), bytesSHA256(statsBytes), stats.PolicyHash
			for _, row := range stats.Rows {
				o.Assignments += row.Assignments
				o.Confirmations += row.Confirmations
			}
			o.ReliabilityPPM = protocol.ReliabilityPPM(o.Confirmations, o.Assignments, p.cfg.Policy.Verify.ReliabilityAMin)
		}
	}
	proofBytes, _, err := p.get(ctx, base+"/verify/proofs?limit=10000", 32*1024*1024)
	if err != nil {
		problems = append(problems, "proofs: "+err.Error())
	} else {
		var proofs struct {
			Schema     string `json:"schema"`
			PolicyHash string `json:"policy_hash"`
			Rows       []struct {
				ServerKeyID byte `json:"server_key_id"`
			} `json:"rows"`
		}
		if json.Unmarshal(proofBytes, &proofs) != nil || proofs.Schema != "urnetwork-verify-proof-index-v1" {
			problems = append(problems, "proofs: invalid schema")
		} else {
			o.ProofRows, o.ProofsHash, o.ProofsPolicyHash = len(proofs.Rows), bytesSHA256(proofBytes), proofs.PolicyHash
			seen := map[byte]bool{}
			for _, proof := range proofs.Rows {
				if !seen[proof.ServerKeyID] {
					seen[proof.ServerKeyID] = true
					o.ProofKeyIDs = append(o.ProofKeyIDs, proof.ServerKeyID)
				}
			}
			sort.Slice(o.ProofKeyIDs, func(i, j int) bool { return o.ProofKeyIDs[i] < o.ProofKeyIDs[j] })
		}
	}
	var lifecycle *FleetLifecycleEvidence
	if value, lifecycleErr := loadFleetLifecycleEvidence(p.stateDir); lifecycleErr == nil {
		lifecycle = value
	} else if !errors.Is(lifecycleErr, os.ErrNotExist) {
		problems = append(problems, "fleet lifecycle: "+lifecycleErr.Error())
	}
	lifecycleEpochs := fleetLifecyclePayoutEpochs(lifecycle)
	lifecycleArtifacts := map[uint64]bool{}
	historyURL := fmt.Sprintf("%s/sn/artifacts?deployment_id=%s&netuid=%d", base, p.cfg.Config.Deployment.DeploymentID, p.cfg.Netuid)
	historyBytes, _, err := p.get(ctx, historyURL, 16*1024*1024)
	var latestMatching *payoutArtifact
	if err != nil {
		problems = append(problems, "artifacts: "+err.Error())
	} else {
		keys := artifactHistoryKeys(historyBytes)
		o.ArtifactHistoryObjects = len(keys)
		seen := map[string]bool{}
		for _, key := range keys {
			hash := strings.TrimSuffix(filepath.Base(key), filepath.Ext(key))
			if len(hash) != 64 || seen[hash] {
				continue
			}
			seen[hash] = true
			artifactBytes, _, artifactErr := p.get(ctx, base+"/sn/artifact?hash=sha256:"+hash, 32*1024*1024)
			if artifactErr != nil {
				problems = append(problems, "artifact "+hash+": "+artifactErr.Error())
				continue
			}
			var artifact payoutArtifact
			if json.Unmarshal(artifactBytes, &artifact) != nil || verifyPayoutArtifact(&artifact) != nil {
				problems = append(problems, "artifact "+hash+": integrity failure")
				continue
			}
			if artifact.DeploymentID != p.cfg.Config.Deployment.DeploymentID || artifact.ChainID != p.cfg.ChainID || artifact.Netuid != p.cfg.Netuid || artifact.NoID != uint64(noID) || !strings.EqualFold(artifact.GenesisHash, p.cfg.Public.Chain.GenesisHash) || !strings.EqualFold(artifact.PolicyHash, p.cfg.PolicyHash) || (expectedSigner != "" && !strings.EqualFold(artifact.Signer.Hex(), expectedSigner)) {
				problems = append(problems, "artifact "+hash+": deployment identity mismatch")
				continue
			}
			o.ValidArtifacts++
			o.ArtifactHashes = append(o.ArtifactHashes, artifact.ContentHash)
			if payoutArtifactMatchesChain(&artifact, contracts) {
				o.MatchingArtifacts++
				if lifecycleEpochs[artifact.Epoch] {
					if lifecycleArtifacts[artifact.Epoch] {
						problems = append(problems, fmt.Sprintf("artifact epoch %d: duplicate lifecycle payout artifact", artifact.Epoch))
					} else if compact, compactErr := compactLifecyclePayoutArtifact(p.cfg, noID, &artifact, minerClients); compactErr != nil {
						problems = append(problems, fmt.Sprintf("artifact epoch %d: %v", artifact.Epoch, compactErr))
					} else {
						lifecycleArtifacts[artifact.Epoch] = true
						o.LifecyclePayoutArtifacts = append(o.LifecyclePayoutArtifacts, compact)
					}
				}
				if latestMatching == nil || artifact.Epoch > latestMatching.Epoch {
					copy := artifact
					latestMatching = &copy
				}
			}
		}
	}
	sort.Slice(o.LifecyclePayoutArtifacts, func(i, j int) bool {
		return o.LifecyclePayoutArtifacts[i].Epoch < o.LifecyclePayoutArtifacts[j].Epoch
	})
	if latestMatching != nil {
		o.LatestArtifactEpoch = latestMatching.Epoch
		o.LatestArtifactHash = latestMatching.ContentHash
		o.LatestPayoutRoot = fleetLifecycleHex(latestMatching.PayoutRoot)
		for _, leaf := range latestMatching.Leaves {
			o.LatestLeafClientIDs = append(o.LatestLeafClientIDs, fleetLifecycleHex16(leaf.ClientID))
		}
		sort.Strings(o.LatestLeafClientIDs)
		for _, provider := range latestMatching.Providers {
			if provider.HeadExcluded {
				o.LatestHeadExcludedClientIDs = append(o.LatestHeadExcludedClientIDs, fleetLifecycleHex16(provider.ClientID))
			}
		}
		sort.Strings(o.LatestHeadExcludedClientIDs)
		membership, membershipErr := summarizePayoutTierMembershipForCandidates(p.cfg, noID, latestMatching, minerClients, fleetLifecycleCandidateMinerSet(p.cfg, lifecycle, latestMatching.Epoch))
		o.LatestArtifactProviders = membership.Providers
		o.CandidateProviders = membership.CandidateProviders
		o.CandidateHeadExcluded = membership.CandidateHeadExcluded
		o.CandidateLeaves = membership.CandidateLeaves
		o.PoolTailProviders = membership.PoolTailProviders
		o.PoolTailHeadExcluded = membership.PoolTailHeadExcluded
		o.PoolTailLeaves = membership.PoolTailLeaves
		o.TierMembershipValid = membershipErr == nil
		if membershipErr != nil {
			problems = append(problems, "latest artifact tier membership: "+membershipErr.Error())
		}
	}
	sort.Strings(o.ArtifactHashes)
	o.Error = strings.Join(problems, "; ")
	return o
}

func artifactHistoryKeys(b []byte) []string {
	var result struct {
		Schema  string            `json:"schema"`
		Objects []json.RawMessage `json:"objects"`
	}
	if json.Unmarshal(b, &result) != nil || result.Schema != "urnetwork-payout-artifact-history-v1" {
		return nil
	}
	keys := make([]string, 0, len(result.Objects))
	for _, raw := range result.Objects {
		var object struct {
			Key string `json:"key"`
		}
		if json.Unmarshal(raw, &object) == nil && object.Key != "" {
			keys = append(keys, object.Key)
			continue
		}
		var key string
		if json.Unmarshal(raw, &key) == nil && key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func payoutArtifactMatchesChain(a *payoutArtifact, contracts *ContractView) bool {
	if contracts == nil || contracts.Deployment == nil || a.End.Number > contracts.FinalizedHead.Number || !strings.EqualFold(a.Coordinator.Hex(), contracts.Deployment.CoordinatorProxy.Hex()) || !strings.EqualFold(a.SettlementVault.Hex(), contracts.Deployment.SettlementVault.Hex()) {
		return false
	}
	for _, epoch := range contracts.Epochs {
		if epoch.Epoch != a.Epoch {
			continue
		}
		for _, operator := range epoch.Operators {
			if operator.NoID != a.NoID {
				continue
			}
			return strings.EqualFold(operator.PayoutRoot, "0x"+hex.EncodeToString(a.PayoutRoot[:])) && strings.EqualFold(operator.ArtifactHash, "0x"+strings.TrimPrefix(a.ContentHash, "sha256:"))
		}
	}
	return false
}

type headSelectionHistory struct {
	DecisionEpochs int
	Transitions    int
	Promoted       []uint16
	Demoted        []uint16
}

func summarizeHeadSelectionHistory(intents []validatorpkg.SteeringIntent, headSlots, candidateFleets int) headSelectionHistory {
	var result headSelectionHistory
	var prior []uint16
	promoted, demoted := map[uint16]bool{}, map[uint16]bool{}
	for _, intent := range intents {
		if intent.Status != "applied" || len(intent.EligibleHeadUIDs) != candidateFleets || len(intent.SelectedHeadUIDs) != headSlots || len(intent.RejectedHeadUIDs) != candidateFleets-headSlots || len(intent.StaleHeadBindings) != 0 {
			continue
		}
		current := sortedUIDs(intent.SelectedHeadUIDs)
		result.DecisionEpochs++
		if prior != nil && !slices.Equal(prior, current) {
			result.Transitions++
			priorSet, currentSet := uint16Set(prior), uint16Set(current)
			for _, uid := range current {
				if !priorSet[uid] {
					promoted[uid] = true
				}
			}
			for _, uid := range prior {
				if !currentSet[uid] {
					demoted[uid] = true
				}
			}
		}
		prior = current
	}
	for uid := range promoted {
		result.Promoted = append(result.Promoted, uid)
	}
	for uid := range demoted {
		result.Demoted = append(result.Demoted, uid)
	}
	sort.Slice(result.Promoted, func(i, j int) bool { return result.Promoted[i] < result.Promoted[j] })
	sort.Slice(result.Demoted, func(i, j int) bool { return result.Demoted[i] < result.Demoted[j] })
	return result
}

func inspectValidatorIntent(stateDir string, validatorID, headSlots, candidateFleets int) ValidatorObservation {
	result := ValidatorObservation{ValidatorID: validatorID}
	intentStateDir := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID), "state")
	intentStore, err := validatorpkg.NewIntentStore(intentStateDir)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	all, err := intentStore.AuthenticatedIntents()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if len(all) != 0 {
		current := all[len(all)-1]
		result.CurrentStatus = current.Status
		result.CurrentEpoch = current.SubnetEpoch
		result.VectorHash = current.VectorHash
	}
	var latestApplied *validatorpkg.SteeringIntent
	for index := range all {
		item := &all[index]
		if item.Status == "finalized" || item.Status == "applied" {
			result.FinalizedIntents++
		}
		if item.Status == "applied" {
			result.AppliedIntents++
			decision := HeadDecisionObservation{
				VectorHash: item.VectorHash, ExtrinsicHash: item.ExtrinsicHash, SettlementEpoch: item.SettlementEpoch,
				NativeSnapshot: ChainHead{Number: item.NativeSnapshotBlock, Hash: strings.ToLower(item.NativeSnapshotHash)},
				EVMSnapshot:    ChainHead{Number: item.EVMSnapshotBlock, Hash: strings.ToLower(item.EVMSnapshotHash)},
				FinalizedBlock: item.FinalizedBlock, FinalizedBlockHash: item.FinalizedBlockHash, RevealBlock: item.RevealBlock,
				SubnetEpoch:      item.SubnetEpoch,
				ApplicationBlock: item.ApplicationBlock, ApplicationBlockHash: item.ApplicationBlockHash,
				MeasurementArtifactHash: item.MeasurementArtifactHash,
				MaskedUIDs:              append([]uint16(nil), item.MaskedUIDs...), EligibleHeadUIDs: append([]uint16(nil), item.EligibleHeadUIDs...),
				EligibleHeadScores: append([]validatorpkg.RationalJSON(nil), item.EligibleHeadScores...),
				SelectedHeadUIDs:   append([]uint16(nil), item.SelectedHeadUIDs...), RejectedHeadUIDs: append([]uint16(nil), item.RejectedHeadUIDs...),
				StaleHeadBindings: len(item.StaleHeadBindings),
			}
			artifact, verified, measurementErr := intentStore.MeasurementArtifact(item)
			if measurementErr == nil {
				measurementErr = validatorpkg.VerifyReleaseMeasurementIntent(item, artifact, verified)
			}
			if measurementErr == nil {
				decision.CandidateFleetUIDs, decision.CandidateFleetHotkeys, measurementErr = headDecisionCandidateIdentities(artifact, item.EligibleHeadUIDs)
			}
			if measurementErr != nil {
				decision.Error = "authenticated measurement candidate identity: " + measurementErr.Error()
			}
			if len(item.UIDs) != len(item.Scores) || len(item.UIDs) != len(item.Values) {
				decision.Error = strings.TrimSpace(decision.Error + " " + fmt.Sprintf("uids/scores/values=%d/%d/%d", len(item.UIDs), len(item.Scores), len(item.Values)))
			} else {
				for weightIndex, uid := range item.UIDs {
					decision.AppliedWeights = append(decision.AppliedWeights, IntentWeightObservation{UID: uid, Numerator: item.Scores[weightIndex].Numerator, Denominator: item.Scores[weightIndex].Denominator, Value: item.Values[weightIndex]})
				}
			}
			result.HeadDecisions = append(result.HeadDecisions, decision)
			if latestApplied == nil || item.SubnetEpoch > latestApplied.SubnetEpoch {
				latestApplied = item
			}
			if len(item.Values) != 0 {
				values, _ := json.Marshal(item.Values)
				result.ValuesHash = bytesSHA256(values)
			}
		}
		if item.VectorHash != "" {
			result.IntentHashes = append(result.IntentHashes, item.VectorHash)
		}
	}
	if latestApplied != nil && len(latestApplied.UIDs) == len(latestApplied.Scores) && len(latestApplied.UIDs) == len(latestApplied.Values) {
		result.SelfUID = latestApplied.SelfUID
		result.MaskedUIDs = append([]uint16(nil), latestApplied.MaskedUIDs...)
		result.EligibleHeadUIDs = append([]uint16(nil), latestApplied.EligibleHeadUIDs...)
		result.EligibleHeadScores = append([]validatorpkg.RationalJSON(nil), latestApplied.EligibleHeadScores...)
		result.SelectedHeadUIDs = append([]uint16(nil), latestApplied.SelectedHeadUIDs...)
		result.RejectedHeadUIDs = append([]uint16(nil), latestApplied.RejectedHeadUIDs...)
		result.StaleHeadBindings = len(latestApplied.StaleHeadBindings)
		result.DepositAudits = append([]validatorpkg.DepositAudit(nil), latestApplied.DepositAudits...)
		for i, uid := range latestApplied.UIDs {
			result.AppliedWeights = append(result.AppliedWeights, IntentWeightObservation{UID: uid, Numerator: latestApplied.Scores[i].Numerator, Denominator: latestApplied.Scores[i].Denominator, Value: latestApplied.Values[i]})
		}
	}
	history := summarizeHeadSelectionHistory(all, headSlots, candidateFleets)
	result.HeadDecisionEpochs = history.DecisionEpochs
	result.HeadTransitions = history.Transitions
	result.PromotedHeadUIDs = history.Promoted
	result.DemotedHeadUIDs = history.Demoted
	sort.Strings(result.IntentHashes)
	return result
}

// Independently verifies the append-only proof store for every operator domain
// measured by one validator. The configured client seed binds the VPK and the
// operator's published key history verifies FINAL signatures across rotations.
// A missing store is a valid zero baseline before the first completed trail.
func inspectValidatorPathProofs(cfg *ResolvedConfig, stateDir string, validatorID int, operators []OperatorObservation) (map[int]int, error) {
	if cfg == nil || cfg.Config == nil || validatorID < 1 || validatorID > cfg.Config.Topology.Validators {
		return nil, errors.New("validator path-proof identity is invalid")
	}
	if len(operators) != cfg.Config.Topology.Operators {
		return nil, fmt.Errorf("validator path-proof operator keys=%d want=%d", len(operators), cfg.Config.Topology.Operators)
	}
	serverKeys := map[int]map[byte]ed25519.PublicKey{}
	for _, operator := range operators {
		if operator.NoID < 1 || operator.NoID > cfg.Config.Topology.Operators || serverKeys[operator.NoID] != nil || len(operator.VerifyKeys) == 0 {
			return nil, fmt.Errorf("operator %d path-proof key history is missing, duplicated, or invalid", operator.NoID)
		}
		serverKeys[operator.NoID] = map[byte]ed25519.PublicKey{}
		for _, key := range operator.VerifyKeys {
			if len(key.PublicKey) != ed25519.PublicKeySize || serverKeys[operator.NoID][key.ServerKeyID] != nil {
				return nil, fmt.Errorf("operator %d path-proof server key %d is invalid or duplicated", operator.NoID, key.ServerKeyID)
			}
			serverKeys[operator.NoID][key.ServerKeyID] = append(ed25519.PublicKey(nil), key.PublicKey...)
		}
	}
	counts := make(map[int]int, cfg.Config.Topology.Operators)
	for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
		root := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID), "state", "operators", fmt.Sprintf("no-%d", noID))
		seed, err := os.ReadFile(filepath.Join(root, "client.key"))
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("validator %d operator %d client seed is unavailable or invalid", validatorID, noID)
		}
		expectedVPK := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
		lines, err := completedReleaseProofLines(filepath.Join(root, "proofs.jsonl"))
		if err != nil {
			return nil, fmt.Errorf("validator %d operator %d path proofs: %w", validatorID, noID, err)
		}
		for lineIndex, line := range lines {
			var record validatorpkg.ProofRecord
			if err := json.Unmarshal(line, &record); err != nil {
				return nil, fmt.Errorf("validator %d operator %d proof %d decode: %w", validatorID, noID, lineIndex+1, err)
			}
			if record.Epoch == 0 {
				return nil, fmt.Errorf("validator %d operator %d proof %d has no contract epoch", validatorID, noID, lineIndex+1)
			}
			if err := validatorpkg.VerifyProofRecord(&record, expectedVPK, serverKeys[noID], cfg.Policy.Verify.TrailDepth); err != nil {
				return nil, fmt.Errorf("validator %d operator %d proof %d: %w", validatorID, noID, lineIndex+1, err)
			}
		}
		counts[noID] = len(lines)
	}
	return counts, nil
}

func inspectClaimQueue(cfg *ResolvedConfig, stateDir string, minerID int) ClaimObservation {
	result := ClaimObservation{MinerID: minerID, NoID: operatorForMiner(cfg, minerID)}
	b, err := os.ReadFile(filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-%d", minerID), "claims", "claim-queue.json"))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	var queue struct {
		Schema  string `json:"schema"`
		Entries map[string]struct {
			Status string `json:"status"`
			TxHash string `json:"tx_hash"`
		} `json:"entries"`
	}
	if json.Unmarshal(b, &queue) != nil || queue.Schema != "urnetwork-provider-claim-queue-v1" {
		result.Error = "invalid claim queue schema"
		return result
	}
	result.Discovered = len(queue.Entries)
	for _, entry := range queue.Entries {
		switch entry.Status {
		case "finalized":
			result.Finalized++
		case "no-claim":
			result.NoClaim++
		case "uncertain", "submitting":
			result.Uncertain++
		case "failed":
			result.Failed++
		default:
			result.Pending++
		}
		if entry.TxHash != "" {
			result.LastTxHash = entry.TxHash
		}
	}
	return result
}

func commonScenarioChecks() []scenarioCheck {
	return []scenarioCheck{
		{ID: "topology_healthy", Check: func(e *scenarioEvaluation) (bool, string) {
			ok := e.Current.Status != nil && e.Current.Status.Supervisor != nil && e.Current.Status.Healthy
			return ok, fmt.Sprintf("healthy=%t", ok)
		}},
		{ID: "contracts_installed", Check: func(e *scenarioEvaluation) (bool, string) {
			ok := e.Current.Status != nil && e.Current.Status.Contracts != nil && e.Current.Status.Contracts.Deployment != nil
			return ok, fmt.Sprintf("installed=%t", ok)
		}},
		{ID: "operator_count", Check: func(e *scenarioEvaluation) (bool, string) {
			got := uint64(0)
			if e.Current.Status != nil && e.Current.Status.Contracts != nil {
				got = e.Current.Status.Contracts.OperatorCount
			}
			return got == uint64(e.Cfg.Config.Topology.Operators), fmt.Sprintf("got=%d want=%d", got, e.Cfg.Config.Topology.Operators)
		}},
		{ID: "runtime_code_matches", Check: func(e *scenarioEvaluation) (bool, string) {
			ok := e.Current.Status != nil && e.Current.Status.Contracts != nil && e.Current.Status.Contracts.RuntimeCodeMatches
			return ok, fmt.Sprintf("matches=%t", ok)
		}},
		{ID: "policy_hash_matches", Check: func(e *scenarioEvaluation) (bool, string) {
			got := ""
			if e.Current.Status != nil && e.Current.Status.Contracts != nil {
				got = e.Current.Status.Contracts.PolicyHash
			}
			return strings.EqualFold(got, e.Cfg.PolicyHash), fmt.Sprintf("got=%s want=%s", got, e.Cfg.PolicyHash)
		}},
		{ID: "rao_conservation", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract counters unavailable"
			}
			c := e.Current.Status.Contracts
			captured, a := new(big.Int).SetString(c.TotalCaptured, 10)
			paid, b := new(big.Int).SetString(c.TotalPaid, 10)
			escrow, d := new(big.Int).SetString(c.EscrowAccounted, 10)
			pending, f := new(big.Int).SetString(c.PendingFunding, 10)
			outstanding, g := new(big.Int).SetString(c.Outstanding, 10)
			live, h := new(big.Int).SetString(c.LiveEscrowStake, 10)
			ok := a && b && d && f && g && h && c.ConservationHolds && captured.Cmp(new(big.Int).Add(paid, escrow)) == 0 && escrow.Cmp(new(big.Int).Add(pending, outstanding)) == 0 && live.Cmp(escrow) >= 0
			return ok, fmt.Sprintf("captured=%s paid=%s escrow=%s pending=%s outstanding=%s live=%s", c.TotalCaptured, c.TotalPaid, c.EscrowAccounted, c.PendingFunding, c.Outstanding, c.LiveEscrowStake)
		}},
		{ID: "runtime_transfer_minimum_bound", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract state unavailable"
			}
			got := e.Current.Status.Contracts.MinimumTransferRao
			want := e.Cfg.Public.Chain.ExpectedDefaultMinTransferRao
			return got == want, fmt.Sprintf("vault=%d manifest=%d", got, want)
		}},
		{ID: "public_identities", Check: func(e *scenarioEvaluation) (bool, string) {
			return e.Current.PublicIdentitiesValid, fmt.Sprintf("valid=%t count=%d", e.Current.PublicIdentitiesValid, e.Current.PublicIdentityCount)
		}},
		{ID: "fleet_commitment_and_bindings", Check: func(e *scenarioEvaluation) (bool, string) {
			want := e.Cfg.Config.Topology.fleetCandidateMiners()
			ok := e.Current.FleetCommitmentValid && e.Current.FleetBindingsValid && e.Current.FleetBindingCount == want
			return ok, fmt.Sprintf("commitment=%t bindings=%d/%d valid=%t", e.Current.FleetCommitmentValid, e.Current.FleetBindingCount, want, e.Current.FleetBindingsValid)
		}},
		{ID: "native_custody_hotkeys", Check: func(e *scenarioEvaluation) (bool, string) {
			ok := e.Current.ReserveValidatorRegistered && e.Current.EscrowHotkeyRegistered && e.Current.NativeCustodyError == ""
			return ok, fmt.Sprintf("reserve_registered=%t reserve_uid=%d escrow_registered=%t escrow_uid=%d error=%s", e.Current.ReserveValidatorRegistered, e.Current.ReserveValidatorUID, e.Current.EscrowHotkeyRegistered, e.Current.EscrowHotkeyUID, e.Current.NativeCustodyError)
		}},
		{ID: "operator_public_apis", Check: func(e *scenarioEvaluation) (bool, string) {
			if len(e.Current.Operators) != e.Cfg.Config.Topology.Operators {
				return false, fmt.Sprintf("operators=%d", len(e.Current.Operators))
			}
			for _, operator := range e.Current.Operators {
				if !operator.Healthy || operator.Error != "" {
					return false, fmt.Sprintf("operator %d: %s", operator.NoID, operator.Error)
				}
			}
			return true, fmt.Sprintf("operators=%d", len(e.Current.Operators))
		}},
	}
}

func epochScenarioChecks() []scenarioCheck {
	return []scenarioCheck{
		{ID: "required_finalized_epochs", Check: func(e *scenarioEvaluation) (bool, string) {
			got := uint64(0)
			if e.Current.Status != nil && e.Current.Status.Contracts != nil {
				got = e.Current.Status.Contracts.CurrentEpoch
			}
			return got >= e.GoalEpoch, fmt.Sprintf("current=%d goal=%d", got, e.GoalEpoch)
		}},
		{ID: "per_no_verify_coverage", Check: func(e *scenarioEvaluation) (bool, string) {
			for _, operator := range e.Current.Operators {
				if operator.StatsRows == 0 || operator.ProofRows == 0 || !strings.EqualFold(operator.StatsPolicyHash, e.Cfg.PolicyHash) || !strings.EqualFold(operator.ProofsPolicyHash, e.Cfg.PolicyHash) {
					return false, fmt.Sprintf("no=%d stats=%d proofs=%d", operator.NoID, operator.StatsRows, operator.ProofRows)
				}
			}
			return len(e.Current.Operators) > 0, "all operators have policy-bound stats and proofs"
		}},
		{ID: "payout_artifacts_reconstruct", Check: func(e *scenarioEvaluation) (bool, string) {
			for _, operator := range e.Current.Operators {
				if operator.ValidArtifacts == 0 || operator.ExpectedFinalizedArtifacts == 0 || operator.MatchingArtifacts < operator.ExpectedFinalizedArtifacts {
					return false, fmt.Sprintf("no=%d valid=%d matching=%d expected_finalized=%d", operator.NoID, operator.ValidArtifacts, operator.MatchingArtifacts, operator.ExpectedFinalizedArtifacts)
				}
			}
			return len(e.Current.Operators) > 0, "all fetched artifacts match finalized roots"
		}},
		{ID: "validator_intents_finalized", Check: func(e *scenarioEvaluation) (bool, string) {
			if len(e.Current.Validators) != e.Cfg.Config.Topology.Validators {
				return false, "validator evidence missing"
			}
			for _, validator := range e.Current.Validators {
				if validator.Error != "" || validator.FinalizedIntents == 0 || validator.AppliedIntents == 0 {
					return false, fmt.Sprintf("validator=%d finalized=%d applied=%d error=%s", validator.ValidatorID, validator.FinalizedIntents, validator.AppliedIntents, validator.Error)
				}
			}
			return true, "all validator intents finalized and applied"
		}},
		{ID: "validator_path_proofs_advance", Check: func(e *scenarioEvaluation) (bool, string) {
			if len(e.Start.Validators) != e.Cfg.Config.Topology.Validators || len(e.Current.Validators) != e.Cfg.Config.Topology.Validators {
				return false, fmt.Sprintf("validator observations start/current=%d/%d", len(e.Start.Validators), len(e.Current.Validators))
			}
			requiredAdvance := int(e.Definition.GoalEpochs)
			if requiredAdvance < 1 {
				requiredAdvance = 1
			}
			baseline := map[int]ValidatorObservation{}
			for _, validator := range e.Start.Validators {
				if validator.ValidatorID < 1 || validator.ValidatorID > e.Cfg.Config.Topology.Validators {
					return false, fmt.Sprintf("baseline validator id %d is outside the release topology", validator.ValidatorID)
				}
				if _, duplicate := baseline[validator.ValidatorID]; duplicate {
					return false, fmt.Sprintf("baseline validator id %d is duplicated", validator.ValidatorID)
				}
				baseline[validator.ValidatorID] = validator
			}
			currentSeen := map[int]bool{}
			for _, validator := range e.Current.Validators {
				if validator.ValidatorID < 1 || validator.ValidatorID > e.Cfg.Config.Topology.Validators || currentSeen[validator.ValidatorID] {
					return false, fmt.Sprintf("current validator id %d is invalid or duplicated", validator.ValidatorID)
				}
				currentSeen[validator.ValidatorID] = true
				prior, ok := baseline[validator.ValidatorID]
				if !ok {
					return false, fmt.Sprintf("validator %d has no baseline observation", validator.ValidatorID)
				}
				if len(prior.PathProofCounts) != e.Cfg.Config.Topology.Operators || len(validator.PathProofCounts) != e.Cfg.Config.Topology.Operators {
					return false, fmt.Sprintf("validator %d path domains start/current=%d/%d want=%d", validator.ValidatorID, len(prior.PathProofCounts), len(validator.PathProofCounts), e.Cfg.Config.Topology.Operators)
				}
				for noID := 1; noID <= e.Cfg.Config.Topology.Operators; noID++ {
					before, beforeOK := prior.PathProofCounts[noID]
					after, afterOK := validator.PathProofCounts[noID]
					if !beforeOK || !afterOK || after-before < requiredAdvance {
						return false, fmt.Sprintf("validator %d operator %d path proofs start/current=%d/%d require_advance=%d", validator.ValidatorID, noID, before, after, requiredAdvance)
					}
				}
			}
			return true, fmt.Sprintf("every validator/operator path-proof store advanced at least %d times", requiredAdvance)
		}},
	}
}

func validateHeadSlotBoundary(validator ValidatorObservation, headSlots, candidateFleets int) (bool, string) {
	if headSlots < 1 || headSlots > int(^uint16(0)) || candidateFleets <= headSlots {
		return false, fmt.Sprintf("invalid boundary slots=%d candidates=%d", headSlots, candidateFleets)
	}
	if len(validator.EligibleHeadUIDs) != candidateFleets || len(validator.SelectedHeadUIDs) != headSlots || len(validator.RejectedHeadUIDs) != candidateFleets-headSlots {
		return false, fmt.Sprintf("eligible/selected/rejected=%d/%d/%d want %d/%d/%d", len(validator.EligibleHeadUIDs), len(validator.SelectedHeadUIDs), len(validator.RejectedHeadUIDs), candidateFleets, headSlots, candidateFleets-headSlots)
	}
	if validator.StaleHeadBindings != 0 {
		return false, fmt.Sprintf("stale_head_bindings=%d", validator.StaleHeadBindings)
	}
	if err := validatorpkg.ValidateHeadSelectionEvidence(validator.EligibleHeadUIDs, validator.EligibleHeadScores, validator.SelectedHeadUIDs, validator.RejectedHeadUIDs, uint16(headSlots)); err != nil {
		return false, fmt.Sprintf("score-ranked boundary: %v", err)
	}
	eligible := map[uint16]bool{}
	for _, uid := range validator.EligibleHeadUIDs {
		if eligible[uid] {
			return false, fmt.Sprintf("duplicate eligible UID %d", uid)
		}
		eligible[uid] = true
	}
	classified := map[uint16]string{}
	for _, entry := range []struct {
		label string
		uids  []uint16
	}{{label: "selected", uids: validator.SelectedHeadUIDs}, {label: "rejected", uids: validator.RejectedHeadUIDs}} {
		for _, uid := range entry.uids {
			if !eligible[uid] {
				return false, fmt.Sprintf("%s UID %d is not eligible", entry.label, uid)
			}
			if prior := classified[uid]; prior != "" {
				return false, fmt.Sprintf("UID %d is classified as both %s and %s", uid, prior, entry.label)
			}
			classified[uid] = entry.label
		}
	}
	if len(classified) != len(eligible) {
		return false, fmt.Sprintf("classified=%d eligible=%d", len(classified), len(eligible))
	}
	return true, fmt.Sprintf("eligible=%d selected=%d rejected=%d", len(eligible), len(validator.SelectedHeadUIDs), len(validator.RejectedHeadUIDs))
}

func sortedUIDs(values []uint16) []uint16 {
	result := append([]uint16(nil), values...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

// Checks one validator's complete score boundary against the exact vector
// observed after its reveal applied on the native chain.
func validateValidatorHeadWeightDecision(cfg *ResolvedConfig, candidateEvidence, poolEvidence map[uint16]bool, validator ValidatorObservation) (map[uint16]bool, int, int, error) {
	if ok, detail := validateHeadSlotBoundary(validator, cfg.Config.Topology.HeadSlots, cfg.Config.Topology.fleetCandidates()); !ok {
		return nil, 0, 0, errors.New(detail)
	}
	selected, rejected := sortedUIDs(validator.SelectedHeadUIDs), sortedUIDs(validator.RejectedHeadUIDs)
	weights, masked := map[uint16]uint16{}, uint16Set(validator.MaskedUIDs)
	for _, weight := range validator.AppliedWeights {
		if _, duplicate := weights[weight.UID]; duplicate {
			return nil, 0, 0, fmt.Errorf("applied UID=%d is duplicated", weight.UID)
		}
		weights[weight.UID] = weight.Value
	}
	selectedSet := uint16Set(selected)
	for uid, value := range weights {
		if value > 0 && !selectedSet[uid] && !poolEvidence[uid] {
			return nil, 0, 0, fmt.Errorf("unapproved head claimant UID=%d applied weight=%d", uid, value)
		}
	}
	uniqueSelected := map[uint16]bool{}
	weightedSelections := 0
	for _, uid := range selected {
		if !candidateEvidence[uid] {
			return nil, 0, 0, fmt.Errorf("selected unknown candidate UID=%d", uid)
		}
		uniqueSelected[uid] = true
		if masked[uid] {
			if weights[uid] != 0 {
				return nil, 0, 0, fmt.Errorf("masked selected UID=%d applied weight=%d", uid, weights[uid])
			}
			continue
		}
		if weights[uid] == 0 {
			return nil, 0, 0, fmt.Errorf("selected UID=%d has zero applied weight", uid)
		}
		weightedSelections++
	}
	for _, uid := range rejected {
		if !candidateEvidence[uid] || weights[uid] != 0 {
			return nil, 0, 0, fmt.Errorf("rejected UID=%d applied weight=%d", uid, weights[uid])
		}
	}
	return uniqueSelected, weightedSelections, len(rejected), nil
}

// Checks both validators' latest applied decisions against the current live
// fleet and operator-pool identity sets.
func validateHeadWeightDecision(cfg *ResolvedConfig, observation *ScenarioObservation) (bool, string) {
	if cfg == nil || observation == nil || len(observation.Validators) != cfg.Config.Topology.Validators {
		return false, "validator head decision evidence is incomplete"
	}
	candidateEvidence := uint16Set(observation.CandidateFleetUIDs)
	if len(candidateEvidence) != cfg.Config.Topology.fleetCandidates() {
		return false, fmt.Sprintf("candidate evidence UIDs=%d want=%d", len(candidateEvidence), cfg.Config.Topology.fleetCandidates())
	}
	poolEvidence := map[uint16]bool{}
	if observation.Status == nil || observation.Status.Contracts == nil {
		return false, "operator pool evidence is unavailable"
	}
	for _, operator := range observation.Status.Contracts.Operators {
		if operator.PoolLive {
			poolEvidence[operator.PoolUID] = true
		}
	}
	if len(poolEvidence) != cfg.Config.Topology.Operators {
		return false, fmt.Sprintf("live operator pool UIDs=%d want=%d", len(poolEvidence), cfg.Config.Topology.Operators)
	}
	uniqueSelected := map[uint16]bool{}
	weightedSelections, zeroRejections := 0, 0
	seenValidators := map[int]bool{}
	for _, validator := range observation.Validators {
		if validator.ValidatorID < 1 || validator.ValidatorID > cfg.Config.Topology.Validators || seenValidators[validator.ValidatorID] {
			return false, fmt.Sprintf("validator=%d is invalid or duplicated", validator.ValidatorID)
		}
		seenValidators[validator.ValidatorID] = true
		selected, weighted, rejected, err := validateValidatorHeadWeightDecision(cfg, candidateEvidence, poolEvidence, validator)
		if err != nil {
			return false, fmt.Sprintf("validator=%d %v", validator.ValidatorID, err)
		}
		for uid := range selected {
			uniqueSelected[uid] = true
		}
		weightedSelections += weighted
		zeroRejections += rejected
	}
	return len(seenValidators) == cfg.Config.Topology.Validators, fmt.Sprintf("candidate_fleets=%d unique_selected=%d weighted_validator_selections=%d zero_validator_rejections=%d validators=%d", len(candidateEvidence), len(uniqueSelected), weightedSelections, zeroRejections, len(seenValidators))
}

// Checks every applied decision created after the acceptance baseline. The
// append-only intent history makes a transient invalid vector permanently
// visible even after a later valid decision becomes current.
func validateHeadDecisionHistory(cfg *ResolvedConfig, start, current *ScenarioObservation) (bool, string) {
	if cfg == nil || start == nil || current == nil || len(start.Validators) != cfg.Config.Topology.Validators || len(current.Validators) != cfg.Config.Topology.Validators {
		return false, "validator head decision history is incomplete"
	}
	candidateEvidence := uint16Set(current.CandidateFleetUIDs)
	if len(candidateEvidence) != cfg.Config.Topology.fleetCandidates() || current.Status == nil || current.Status.Contracts == nil {
		return false, "candidate or operator pool history evidence is incomplete"
	}
	poolEvidence := map[uint16]bool{}
	for _, operator := range current.Status.Contracts.Operators {
		if operator.PoolLive {
			poolEvidence[operator.PoolUID] = true
		}
	}
	if len(poolEvidence) != cfg.Config.Topology.Operators {
		return false, fmt.Sprintf("live operator pool UIDs=%d want=%d", len(poolEvidence), cfg.Config.Topology.Operators)
	}
	baseline := map[int]map[string]bool{}
	for _, validator := range start.Validators {
		if validator.ValidatorID < 1 || validator.ValidatorID > cfg.Config.Topology.Validators || baseline[validator.ValidatorID] != nil {
			return false, fmt.Sprintf("baseline validator=%d is invalid or duplicated", validator.ValidatorID)
		}
		baseline[validator.ValidatorID] = map[string]bool{}
		for _, decision := range validator.HeadDecisions {
			if decision.VectorHash == "" || baseline[validator.ValidatorID][decision.VectorHash] {
				return false, fmt.Sprintf("baseline validator=%d has an empty or duplicate decision hash", validator.ValidatorID)
			}
			baseline[validator.ValidatorID][decision.VectorHash] = true
		}
	}
	freshDecisions := 0
	seenValidators := map[int]bool{}
	for _, validator := range current.Validators {
		if validator.ValidatorID < 1 || validator.ValidatorID > cfg.Config.Topology.Validators || seenValidators[validator.ValidatorID] || baseline[validator.ValidatorID] == nil {
			return false, fmt.Sprintf("current validator=%d is invalid, duplicated, or absent from baseline", validator.ValidatorID)
		}
		seenValidators[validator.ValidatorID] = true
		seen := map[string]bool{}
		validatorFreshDecisions := 0
		for _, decision := range validator.HeadDecisions {
			if decision.VectorHash == "" || seen[decision.VectorHash] {
				return false, fmt.Sprintf("validator=%d has an empty or duplicate decision hash", validator.ValidatorID)
			}
			seen[decision.VectorHash] = true
			if baseline[validator.ValidatorID][decision.VectorHash] {
				continue
			}
			freshDecisions++
			validatorFreshDecisions++
			if decision.Error != "" || decision.ApplicationBlock == 0 || decision.ApplicationBlockHash == "" {
				return false, fmt.Sprintf("validator=%d decision=%s application evidence is invalid: %s", validator.ValidatorID, decision.VectorHash, decision.Error)
			}
			observed := ValidatorObservation{
				ValidatorID: validator.ValidatorID, MaskedUIDs: decision.MaskedUIDs,
				EligibleHeadUIDs: decision.EligibleHeadUIDs, EligibleHeadScores: decision.EligibleHeadScores,
				SelectedHeadUIDs: decision.SelectedHeadUIDs, RejectedHeadUIDs: decision.RejectedHeadUIDs,
				StaleHeadBindings: decision.StaleHeadBindings, AppliedWeights: decision.AppliedWeights,
			}
			if _, _, _, err := validateValidatorHeadWeightDecision(cfg, candidateEvidence, poolEvidence, observed); err != nil {
				return false, fmt.Sprintf("validator=%d decision=%s: %v", validator.ValidatorID, decision.VectorHash, err)
			}
		}
		for hash := range baseline[validator.ValidatorID] {
			if !seen[hash] {
				return false, fmt.Sprintf("validator=%d baseline decision=%s disappeared from append-only history", validator.ValidatorID, hash)
			}
		}
		if validatorFreshDecisions == 0 {
			return false, fmt.Sprintf("validator=%d has no fresh applied decision after the acceptance baseline", validator.ValidatorID)
		}
	}
	if len(seenValidators) != cfg.Config.Topology.Validators || freshDecisions < cfg.Config.Topology.Validators {
		return false, fmt.Sprintf("fresh_decisions=%d validators=%d/%d", freshDecisions, len(seenValidators), cfg.Config.Topology.Validators)
	}
	return true, fmt.Sprintf("fresh_applied_decisions=%d validators=%d", freshDecisions, len(seenValidators))
}

func headDecisionWeight(decision HeadDecisionObservation, uid uint16) (uint16, error) {
	var value uint16
	found := false
	for _, weight := range decision.AppliedWeights {
		if weight.UID != uid {
			continue
		}
		if found {
			return 0, fmt.Errorf("decision %s duplicates applied UID=%d", decision.VectorHash, uid)
		}
		found, value = true, weight.Value
	}
	return value, nil
}

func decisionContainsUID(values []uint16, uid uint16) bool {
	return slices.Contains(values, uid)
}

type validatorLocalBoundaryDivergence struct {
	TargetUID    uint16
	Epoch        uint64
	Replacements map[uint16]bool
	Filtered     map[uint64]HeadDecisionObservation
	Independent  map[uint64]HeadDecisionObservation
	CommonEpochs []uint64
}

func freshHeadDecisionsByEpoch(start, current *ScenarioObservation, validatorIDs ...int) (map[int]map[uint64]HeadDecisionObservation, error) {
	if start == nil || current == nil || len(validatorIDs) == 0 {
		return nil, errors.New("fresh head decision evidence is incomplete")
	}
	required := map[int]bool{}
	for _, validatorID := range validatorIDs {
		if validatorID < 1 || required[validatorID] {
			return nil, errors.New("fresh head decision validator identities are invalid")
		}
		required[validatorID] = true
	}
	baseline := map[int]map[string]bool{}
	for _, observation := range start.Validators {
		if !required[observation.ValidatorID] {
			continue
		}
		if baseline[observation.ValidatorID] != nil {
			return nil, fmt.Errorf("baseline validator=%d is duplicated", observation.ValidatorID)
		}
		baseline[observation.ValidatorID] = map[string]bool{}
		for _, decision := range observation.HeadDecisions {
			if decision.VectorHash == "" || baseline[observation.ValidatorID][decision.VectorHash] {
				return nil, fmt.Errorf("baseline validator=%d decision identity is invalid", observation.ValidatorID)
			}
			baseline[observation.ValidatorID][decision.VectorHash] = true
		}
	}
	result := map[int]map[uint64]HeadDecisionObservation{}
	seenValidators := map[int]bool{}
	for _, observation := range current.Validators {
		if !required[observation.ValidatorID] {
			continue
		}
		if seenValidators[observation.ValidatorID] || baseline[observation.ValidatorID] == nil {
			return nil, fmt.Errorf("current validator=%d is duplicated or absent from baseline", observation.ValidatorID)
		}
		seenValidators[observation.ValidatorID] = true
		result[observation.ValidatorID] = map[uint64]HeadDecisionObservation{}
		seenHashes := map[string]bool{}
		for _, decision := range observation.HeadDecisions {
			if decision.VectorHash == "" || seenHashes[decision.VectorHash] {
				return nil, fmt.Errorf("validator=%d decision identity is empty or duplicated", observation.ValidatorID)
			}
			seenHashes[decision.VectorHash] = true
			if baseline[observation.ValidatorID][decision.VectorHash] {
				continue
			}
			if _, duplicate := result[observation.ValidatorID][decision.SubnetEpoch]; duplicate {
				return nil, fmt.Errorf("validator=%d duplicates fresh native epoch=%d", observation.ValidatorID, decision.SubnetEpoch)
			}
			result[observation.ValidatorID][decision.SubnetEpoch] = decision
		}
	}
	for validatorID := range required {
		if baseline[validatorID] == nil || !seenValidators[validatorID] {
			return nil, fmt.Errorf("validator=%d decision domain is absent", validatorID)
		}
	}
	return result, nil
}

func commonHeadDecisionEpochs(left, right map[uint64]HeadDecisionObservation) []uint64 {
	epochs := make([]uint64, 0, len(left))
	for epoch := range left {
		if _, ok := right[epoch]; ok {
			epochs = append(epochs, epoch)
		}
	}
	sort.Slice(epochs, func(i, j int) bool { return epochs[i] < epochs[j] })
	return epochs
}

func findValidatorLocalHeadBoundaryDivergence(cfg *ResolvedConfig, start, current *ScenarioObservation) (*validatorLocalBoundaryDivergence, error) {
	if cfg == nil || cfg.Config == nil || start == nil || current == nil || cfg.Config.Topology.Validators < 2 || len(current.CandidateFleetUIDs) != cfg.Config.Topology.fleetCandidates() || validatorLocalHeadBoundaryFleet > len(current.CandidateFleetUIDs) {
		return nil, errors.New("validator-local top-200 evidence is incomplete")
	}
	targetUID := current.CandidateFleetUIDs[validatorLocalHeadBoundaryFleet-1]
	byValidator, err := freshHeadDecisionsByEpoch(start, current, validatorLocalHeadBoundaryValidator, 2)
	if err != nil {
		return nil, err
	}
	filtered := byValidator[validatorLocalHeadBoundaryValidator]
	independent := byValidator[2]
	epochs := commonHeadDecisionEpochs(filtered, independent)
	for _, epoch := range epochs {
		left, right := filtered[epoch], independent[epoch]
		leftWeight, leftErr := headDecisionWeight(left, targetUID)
		rightWeight, rightErr := headDecisionWeight(right, targetUID)
		if leftErr != nil || rightErr != nil {
			return nil, fmt.Errorf("native epoch=%d target weight evidence errors=%v/%v", epoch, leftErr, rightErr)
		}
		if decisionContainsUID(left.MaskedUIDs, targetUID) || decisionContainsUID(right.MaskedUIDs, targetUID) {
			return nil, fmt.Errorf("validator-local target UID=%d is unexpectedly masked at native epoch=%d", targetUID, epoch)
		}
		if !decisionContainsUID(left.RejectedHeadUIDs, targetUID) || leftWeight != 0 || !decisionContainsUID(right.SelectedHeadUIDs, targetUID) || rightWeight == 0 {
			continue
		}
		replacements := map[uint16]bool{}
		for _, uid := range left.SelectedHeadUIDs {
			if !decisionContainsUID(right.RejectedHeadUIDs, uid) {
				continue
			}
			leftReplacementWeight, leftWeightErr := headDecisionWeight(left, uid)
			rightReplacementWeight, rightWeightErr := headDecisionWeight(right, uid)
			if leftWeightErr != nil || rightWeightErr != nil {
				return nil, fmt.Errorf("native epoch=%d replacement UID=%d weight evidence errors=%v/%v", epoch, uid, leftWeightErr, rightWeightErr)
			}
			if leftReplacementWeight > 0 && rightReplacementWeight == 0 {
				replacements[uid] = true
			}
		}
		if len(replacements) != 0 {
			return &validatorLocalBoundaryDivergence{TargetUID: targetUID, Epoch: epoch, Replacements: replacements, Filtered: filtered, Independent: independent, CommonEpochs: epochs}, nil
		}
	}
	return nil, nil
}

// Proves validator-local top-200 admission on the live paths. During the
// bounded operator view fault, validator 1 must reject the designated fleet
// with zero submitted weight while an independent validator selects the same
// on-chain claimant with positive weight in the same native epoch. A later
// common epoch must restore the fleet and zero the temporary replacement.
func validateValidatorLocalHeadBoundary(cfg *ResolvedConfig, start, current *ScenarioObservation) (bool, string) {
	divergence, err := findValidatorLocalHeadBoundaryDivergence(cfg, start, current)
	if err != nil {
		return false, err.Error()
	}
	if divergence == nil {
		return false, "no common native epoch proves validator-local rejected-zero versus selected-positive"
	}
	for _, epoch := range divergence.CommonEpochs {
		if epoch <= divergence.Epoch {
			continue
		}
		left, right := divergence.Filtered[epoch], divergence.Independent[epoch]
		leftWeight, leftErr := headDecisionWeight(left, divergence.TargetUID)
		rightWeight, rightErr := headDecisionWeight(right, divergence.TargetUID)
		if leftErr != nil || rightErr != nil {
			return false, fmt.Sprintf("native epoch=%d restoration weight evidence errors=%v/%v", epoch, leftErr, rightErr)
		}
		if !decisionContainsUID(left.SelectedHeadUIDs, divergence.TargetUID) || !decisionContainsUID(right.SelectedHeadUIDs, divergence.TargetUID) || leftWeight == 0 || rightWeight == 0 {
			continue
		}
		for uid := range divergence.Replacements {
			value, weightErr := headDecisionWeight(left, uid)
			if weightErr != nil {
				return false, weightErr.Error()
			}
			if decisionContainsUID(left.RejectedHeadUIDs, uid) && value == 0 {
				return true, fmt.Sprintf("target_uid=%d divergence_epoch=%d restoration_epoch=%d replacement_uid=%d", divergence.TargetUID, divergence.Epoch, epoch, uid)
			}
		}
	}
	return false, fmt.Sprintf("UID=%d diverged at native epoch=%d but no later common decision restored it and zeroed a replacement", divergence.TargetUID, divergence.Epoch)
}

func globalHeadBoundaryDiverged(cfg *ResolvedConfig, start, current *ScenarioObservation) (bool, error) {
	if cfg == nil || cfg.Config == nil || start == nil || current == nil || len(current.CandidateFleetUIDs) != cfg.Config.Topology.fleetCandidates() || cfg.Config.Topology.HeadFleets < 3 {
		return false, errors.New("global head-boundary evidence is incomplete")
	}
	byValidator, err := freshHeadDecisionsByEpoch(start, current, 1, 2)
	if err != nil {
		return false, err
	}
	targetUID := current.CandidateFleetUIDs[2]
	challengers := uint16Set(current.CandidateFleetUIDs[cfg.Config.Topology.HeadSlots:])
	for _, epoch := range commonHeadDecisionEpochs(byValidator[1], byValidator[2]) {
		complete := true
		for _, validatorID := range []int{1, 2} {
			decision := byValidator[validatorID][epoch]
			weight, weightErr := headDecisionWeight(decision, targetUID)
			if weightErr != nil {
				return false, weightErr
			}
			if decisionContainsUID(decision.MaskedUIDs, targetUID) || !decisionContainsUID(decision.RejectedHeadUIDs, targetUID) || weight != 0 {
				complete = false
				break
			}
			replacement := false
			for _, uid := range decision.SelectedHeadUIDs {
				value, valueErr := headDecisionWeight(decision, uid)
				if valueErr != nil {
					return false, valueErr
				}
				if challengers[uid] && value > 0 {
					replacement = true
					break
				}
			}
			if !replacement {
				complete = false
				break
			}
		}
		if complete {
			return true, nil
		}
	}
	return false, nil
}

func faultConditionMet(cfg *ResolvedConfig, start, current *ScenarioObservation, condition string) (bool, error) {
	switch condition {
	case "":
		return false, nil
	case "global-head-boundary-diverged":
		return globalHeadBoundaryDiverged(cfg, start, current)
	case "validator-local-head-boundary-diverged":
		divergence, err := findValidatorLocalHeadBoundaryDivergence(cfg, start, current)
		return divergence != nil, err
	case "fleet-lifecycle-fallback-installed":
		return current != nil && current.FleetLifecycle != nil && (current.FleetLifecycle.Stage == fleetLifecycleStageFallbackInstalled || current.FleetLifecycle.Stage == fleetLifecycleStageFallbackPaid || current.FleetLifecycle.Stage == fleetLifecycleStageProviderInstalled || current.FleetLifecycle.Stage == fleetLifecycleStageReleaseHandoff || current.FleetLifecycle.Stage == fleetLifecycleStageComplete), nil
	case "fleet-lifecycle-provider-installed":
		return current != nil && current.FleetLifecycle != nil && (current.FleetLifecycle.Stage == fleetLifecycleStageProviderInstalled || current.FleetLifecycle.Stage == fleetLifecycleStageProviderPaid || current.FleetLifecycle.Stage == fleetLifecycleStageTerminalInstalled || current.FleetLifecycle.Stage == fleetLifecycleStageReleaseHandoff || current.FleetLifecycle.Stage == fleetLifecycleStageComplete), nil
	case "fleet-lifecycle-provider-paid":
		return current != nil && current.FleetLifecycle != nil && (current.FleetLifecycle.Stage == fleetLifecycleStageProviderPaid || current.FleetLifecycle.Stage == fleetLifecycleStageTerminalInstalled || current.FleetLifecycle.Stage == fleetLifecycleStageReleaseHandoff || current.FleetLifecycle.Stage == fleetLifecycleStageComplete), nil
	case "fleet-lifecycle-terminal-effective":
		return current != nil && current.Status != nil && current.Status.Contracts != nil && current.FleetLifecycle != nil && current.FleetLifecycle.TerminalEffectiveEpoch != 0 && current.Status.Contracts.CurrentEpoch >= current.FleetLifecycle.TerminalEffectiveEpoch && (current.FleetLifecycle.Stage == fleetLifecycleStageTerminalInstalled || current.FleetLifecycle.Stage == fleetLifecycleStageReleaseHandoff || current.FleetLifecycle.Stage == fleetLifecycleStageComplete), nil
	case "fleet-lifecycle-complete":
		return current != nil && current.FleetLifecycle != nil && current.FleetLifecycle.Stage == fleetLifecycleStageComplete, nil
	default:
		return false, fmt.Errorf("unsupported fault condition %q", condition)
	}
}

func faultRestoreConditionMet(cfg *ResolvedConfig, start, current *ScenarioObservation, spec scenarioFaultSpec) (bool, error) {
	return faultConditionMet(cfg, start, current, spec.RestoreCondition)
}

func headBoundaryUIDTieGeometry(cfg *ResolvedConfig, observation *ScenarioObservation) (bool, string) {
	if cfg == nil || cfg.Config == nil || observation == nil || cfg.Config.Topology.HeadFleets < 4 || cfg.Config.Topology.ChallengerFleets < 2 || len(observation.CandidateFleetUIDs) != cfg.Config.Topology.fleetCandidates() || cfg.Config.Topology.HeadSlots != cfg.Config.Topology.HeadFleets {
		return false, "head-boundary UID tie geometry is incomplete"
	}
	targets := []uint16{observation.CandidateFleetUIDs[2], observation.CandidateFleetUIDs[validatorLocalHeadBoundaryFleet-1]}
	challengers := append([]uint16(nil), observation.CandidateFleetUIDs[cfg.Config.Topology.HeadSlots:]...)
	if len(challengers) != 2 || targets[0] == targets[1] {
		return false, fmt.Sprintf("targets=%v challengers=%v", targets, challengers)
	}
	maximumChallenger := challengers[0]
	for _, uid := range challengers[1:] {
		if uid > maximumChallenger {
			maximumChallenger = uid
		}
	}
	minimumTarget := targets[0]
	for _, uid := range targets[1:] {
		if uid < minimumTarget {
			minimumTarget = uid
		}
	}
	if maximumChallenger >= minimumTarget {
		return false, fmt.Sprintf("fault targets=%v do not lose an equal-score tie to challengers=%v", targets, challengers)
	}
	return true, fmt.Sprintf("fault_targets=%v lower_uid_challengers=%v", targets, challengers)
}

func nativeRewardAt(rewards *NativeRewardObservation, uid uint16) (*big.Int, uint16, uint16, bool) {
	if rewards == nil || int(uid) >= len(rewards.EmissionRao) || int(uid) >= len(rewards.Incentive) || int(uid) >= len(rewards.Dividends) {
		return nil, 0, 0, false
	}
	emission, ok := new(big.Int).SetString(rewards.EmissionRao[uid], 10)
	if !ok || emission.Sign() < 0 {
		return nil, 0, 0, false
	}
	return emission, rewards.Incentive[uid], rewards.Dividends[uid], true
}

// Decode the durable stake balance corresponding to one UID in the exact
// reward snapshot. Keeping this separate from nativeRewardAt prevents callers
// that only validate the three same-length epoch vectors from accidentally
// treating a missing stake census as a zero payout.
func nativeRewardStakeAt(rewards *NativeRewardObservation, uid uint16) (*big.Int, bool) {
	if rewards == nil || int(uid) >= len(rewards.TotalHotkeyAlphaRao) {
		return nil, false
	}
	stake, ok := new(big.Int).SetString(rewards.TotalHotkeyAlphaRao[uid], 10)
	if !ok || stake.Sign() < 0 || stake.String() != rewards.TotalHotkeyAlphaRao[uid] {
		return nil, false
	}
	return stake, true
}

func validateNativeRewardChannels(cfg *ResolvedConfig, observation *ScenarioObservation) (bool, string) {
	if cfg == nil || observation == nil || observation.NativeRewards == nil || observation.NativeRewardsError != "" {
		return false, "native reward vectors are unavailable: " + func() string {
			if observation == nil {
				return "observation unavailable"
			}
			return observation.NativeRewardsError
		}()
	}
	if len(observation.Validators) != cfg.Config.Topology.Validators || len(observation.CandidateFleetUIDs) != cfg.Config.Topology.fleetCandidates() {
		return false, "native reward role identities are incomplete"
	}
	selectedBy := map[uint16]int{}
	for _, validator := range observation.Validators {
		if ok, detail := validateHeadSlotBoundary(validator, cfg.Config.Topology.HeadSlots, cfg.Config.Topology.fleetCandidates()); !ok {
			return false, fmt.Sprintf("validator=%d %s", validator.ValidatorID, detail)
		}
		for _, uid := range validator.SelectedHeadUIDs {
			selectedBy[uid]++
		}
	}
	paidHeads, unanimousPaid, unanimousRejectedUnpaid, contested := 0, 0, 0, 0
	for _, uid := range observation.CandidateFleetUIDs {
		emission, incentive, _, ok := nativeRewardAt(observation.NativeRewards, uid)
		if !ok {
			return false, fmt.Sprintf("candidate UID=%d has no exact native reward row", uid)
		}
		paid := emission.Sign() > 0 && incentive > 0
		if (emission.Sign() > 0) != (incentive > 0) {
			return false, fmt.Sprintf("candidate UID=%d has inconsistent emission=%s incentive=%d", uid, emission, incentive)
		}
		if paid {
			paidHeads++
		}
		switch selectedBy[uid] {
		case len(observation.Validators):
			if emission.Sign() <= 0 || incentive == 0 {
				return false, fmt.Sprintf("unanimously selected UID=%d emission=%s incentive=%d", uid, emission, incentive)
			}
			unanimousPaid++
		case 0:
			if emission.Sign() != 0 || incentive != 0 {
				return false, fmt.Sprintf("unanimously rejected UID=%d emission=%s incentive=%d", uid, emission, incentive)
			}
			unanimousRejectedUnpaid++
		default:
			contested++
		}
	}
	paidPools := 0
	if observation.Status == nil || observation.Status.Contracts == nil {
		return false, "pool UID state is unavailable"
	}
	for _, operator := range observation.Status.Contracts.Operators {
		if !operator.PoolLive {
			continue
		}
		emission, incentive, _, ok := nativeRewardAt(observation.NativeRewards, operator.PoolUID)
		if !ok || emission.Sign() <= 0 || incentive == 0 {
			return false, fmt.Sprintf("pool no=%d UID=%d emission=%v incentive=%d", operator.NoID, operator.PoolUID, emission, incentive)
		}
		paidPools++
	}
	paidValidators := 0
	for _, validator := range observation.Validators {
		emission, _, dividends, ok := nativeRewardAt(observation.NativeRewards, validator.SelfUID)
		if !ok || emission.Sign() <= 0 || dividends == 0 {
			return false, fmt.Sprintf("validator=%d UID=%d emission=%v dividends=%d", validator.ValidatorID, validator.SelfUID, emission, dividends)
		}
		paidValidators++
	}
	ok := paidPools == cfg.Config.Topology.Operators && paidValidators == cfg.Config.Topology.Validators
	return ok, fmt.Sprintf("native paid_heads=%d unanimous_paid=%d unanimous_rejected_unpaid=%d contested=%d pools=%d validators=%d block=%d", paidHeads, unanimousPaid, unanimousRejectedUnpaid, contested, paidPools, paidValidators, observation.NativeRewards.FinalizedHead.Number)
}

func releaseScenarioChecks() []scenarioCheck {
	return []scenarioCheck{
		{ID: "governance_adversarial_drill_complete", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.GovernanceDrill == nil {
				return false, "governance evidence unavailable: " + e.Current.GovernanceDrillError
			}
			evidence := e.Current.GovernanceDrill
			ok := evidence.Stage == "complete" && evidence.After != nil && len(evidence.ProbeResults) == 4
			for _, succeeded := range evidence.ProbeResults {
				ok = ok && !succeeded
			}
			return ok, fmt.Sprintf("stage=%s probes=%d", evidence.Stage, len(evidence.ProbeResults))
		}},
		{ID: "reserve_delegate_take_zero", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.ReserveDelegateTake == nil {
				return false, "reserve delegate take is unavailable"
			}
			return *e.Current.ReserveDelegateTake == 0, fmt.Sprintf("take_parts_per_65535=%d", *e.Current.ReserveDelegateTake)
		}},
		{ID: "per_no_deposits_and_conviction", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract state unavailable"
			}
			deposited := map[uint64]bool{}
			for _, epoch := range e.Current.Status.Contracts.Epochs {
				for _, op := range epoch.Operators {
					amount, ok := new(big.Int).SetString(op.DepositRao, 10)
					if ok && amount.Sign() > 0 {
						deposited[op.NoID] = true
					}
				}
			}
			for _, op := range e.Current.Status.Contracts.Operators {
				conviction, ok := new(big.Int).SetString(op.ConvictionRao, 10)
				if !deposited[op.NoID] || !ok || conviction.Sign() <= 0 {
					return false, fmt.Sprintf("no=%d deposited=%t conviction=%s", op.NoID, deposited[op.NoID], op.ConvictionRao)
				}
			}
			return len(deposited) == e.Cfg.Config.Topology.Operators, "every NO has isolated deposit and conviction"
		}},
		{ID: "voluntary_conviction_crosses_tier_prospectively", Check: func(e *scenarioEvaluation) (bool, string) {
			report := analyzeScenarioObservation(e.Cfg, e.Current)
			ok := e.Current.VoluntaryConvictionValid && report.TierReconstructionComplete && report.VoluntaryConvictionObserved && report.ConvictionTierCrossed
			return ok, fmt.Sprintf("evidence_valid=%t reconstructed=%t voluntary=%t tier_crossed=%t error=%s", e.Current.VoluntaryConvictionValid, report.TierReconstructionComplete, report.VoluntaryConvictionObserved, report.ConvictionTierCrossed, e.Current.VoluntaryConvictionError)
		}},
		{ID: "quality_cohorts_differ", Check: func(e *scenarioEvaluation) (bool, string) {
			byNO := map[int]OperatorObservation{}
			for _, operator := range e.Current.Operators {
				byNO[operator.NoID] = operator
			}
			faulted := byNO[e.Cfg.Config.Scenarios.QualityFaultOperator]
			if faulted.Assignments < e.Cfg.Policy.Verify.ReliabilityAMin {
				return false, fmt.Sprintf("faulted_no=%d assignments=%d", faulted.NoID, faulted.Assignments)
			}
			for noID, operator := range byNO {
				if noID != faulted.NoID && operator.Assignments >= e.Cfg.Policy.Verify.ReliabilityAMin && operator.ReliabilityPPM > faulted.ReliabilityPPM {
					return true, fmt.Sprintf("healthy_no=%d reliability_ppm=%d faulted_no=%d reliability_ppm=%d", noID, operator.ReliabilityPPM, faulted.NoID, faulted.ReliabilityPPM)
				}
			}
			return false, fmt.Sprintf("faulted_no=%d reliability_ppm=%d cohorts=%v", faulted.NoID, faulted.ReliabilityPPM, byNO)
		}},
		{ID: "validator_vectors_independently_applied", Check: func(e *scenarioEvaluation) (bool, string) {
			for _, validator := range e.Current.Validators {
				if validator.ValuesHash == "" {
					return false, fmt.Sprintf("validator %d has no applied vector", validator.ValidatorID)
				}
			}
			return len(e.Current.Validators) == e.Cfg.Config.Topology.Validators, fmt.Sprintf("applied_vectors=%d", len(e.Current.Validators))
		}},
		{ID: "validator_deposit_audits_compliant", Check: func(e *scenarioEvaluation) (bool, string) {
			for _, validator := range e.Current.Validators {
				if len(validator.DepositAudits) != e.Cfg.Config.Topology.Operators {
					return false, fmt.Sprintf("validator=%d audits=%d want=%d", validator.ValidatorID, len(validator.DepositAudits), e.Cfg.Config.Topology.Operators)
				}
				for _, audit := range validator.DepositAudits {
					if !audit.Compliant || audit.Status != validatorpkg.DepositAuditCompliant || audit.Disposition != "pool_weight_eligible" || audit.ArtifactHash == "" || audit.RequiredDepositRao != audit.ObservedDepositRao {
						return false, fmt.Sprintf("validator=%d no=%d status=%s required=%s observed=%s error=%s", validator.ValidatorID, audit.NoID, audit.Status, audit.RequiredDepositRao, audit.ObservedDepositRao, audit.Error)
					}
				}
			}
			return len(e.Current.Validators) == e.Cfg.Config.Topology.Validators, "all validators independently accepted exact signed-usage deposits"
		}},
		{ID: "payout_artifacts_enforce_one_tier", Check: func(e *scenarioEvaluation) (bool, string) {
			for _, operator := range e.Current.Operators {
				if !operator.TierMembershipValid || operator.CandidateProviders == 0 || operator.CandidateHeadExcluded != operator.CandidateProviders || operator.CandidateLeaves != 0 || operator.PoolTailProviders == 0 || operator.PoolTailHeadExcluded != 0 || operator.PoolTailLeaves == 0 {
					return false, fmt.Sprintf("no=%d epoch=%d candidates=%d excluded=%d leaves=%d tail=%d tail_excluded=%d tail_leaves=%d", operator.NoID, operator.LatestArtifactEpoch, operator.CandidateProviders, operator.CandidateHeadExcluded, operator.CandidateLeaves, operator.PoolTailProviders, operator.PoolTailHeadExcluded, operator.PoolTailLeaves)
				}
			}
			return len(e.Current.Operators) == e.Cfg.Config.Topology.Operators, "every live fleet is excluded from pool artifacts and pool-tail providers retain leaves"
		}},
		{ID: "signed_weight_cap_enforced", Check: func(e *scenarioEvaluation) (bool, string) {
			cap := e.Cfg.Policy.Steering.MaxWeightLimitU16
			for _, validator := range e.Current.Validators {
				ok, maximum, sum := weightValuesRespectCap(validator.AppliedWeights, cap)
				if !ok {
					return false, fmt.Sprintf("validator=%d max=%d sum=%d cap=%d", validator.ValidatorID, maximum, sum, cap)
				}
			}
			return len(e.Current.Validators) == e.Cfg.Config.Topology.Validators, fmt.Sprintf("validators=%d cap=%d", len(e.Current.Validators), cap)
		}},
		{ID: "native_head_weight_observed", Check: func(e *scenarioEvaluation) (bool, string) {
			if !e.Current.FleetBindingsValid {
				return false, "fleet binding evidence is invalid"
			}
			observed := map[uint16]bool{}
			for _, validator := range e.Current.Validators {
				masked := uint16Set(validator.MaskedUIDs)
				for _, headUID := range validator.SelectedHeadUIDs {
					if masked[headUID] {
						continue
					}
					found := false
					for _, weight := range validator.AppliedWeights {
						if weight.UID == headUID && weight.Value > 0 {
							found = true
							observed[headUID] = true
						}
					}
					if !found {
						return false, fmt.Sprintf("validator=%d head_uid=%d has no nonzero applied native weight", validator.ValidatorID, headUID)
					}
				}
			}
			return len(e.Current.Validators) == e.Cfg.Config.Topology.Validators && len(observed) >= e.Cfg.Config.Topology.HeadSlots, fmt.Sprintf("observed_selected_heads=%d validators=%d", len(observed), len(e.Current.Validators))
		}},
		{ID: "head_slot_boundary_enforced", Check: func(e *scenarioEvaluation) (bool, string) {
			for _, validator := range e.Current.Validators {
				if ok, detail := validateHeadSlotBoundary(validator, e.Cfg.Config.Topology.HeadSlots, e.Cfg.Config.Topology.fleetCandidates()); !ok {
					return false, fmt.Sprintf("validator=%d %s", validator.ValidatorID, detail)
				}
			}
			return len(e.Current.Validators) == e.Cfg.Config.Topology.Validators, fmt.Sprintf("validators=%d selected=%d candidates=%d", len(e.Current.Validators), e.Cfg.Config.Topology.HeadSlots, e.Cfg.Config.Topology.fleetCandidates())
		}},
		{ID: "head_fault_uid_tiebreak_safe", Check: func(e *scenarioEvaluation) (bool, string) {
			return headBoundaryUIDTieGeometry(e.Cfg, e.Current)
		}},
		{ID: "head_selected_paid_rejected_zero_weight", Check: func(e *scenarioEvaluation) (bool, string) {
			return validateHeadWeightDecision(e.Cfg, e.Current)
		}},
		{ID: "head_decision_history_valid", Check: func(e *scenarioEvaluation) (bool, string) {
			return validateHeadDecisionHistory(e.Cfg, e.Start, e.Current)
		}},
		{ID: "validator_local_top200_disagreement", Check: func(e *scenarioEvaluation) (bool, string) {
			return validateValidatorLocalHeadBoundary(e.Cfg, e.Start, e.Current)
		}},
		{ID: "head_promotion_demotion_transition", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Start == nil || len(e.Start.Validators) != e.Cfg.Config.Topology.Validators {
				return false, "head transition baseline is unavailable"
			}
			baseline := make(map[int]ValidatorObservation, len(e.Start.Validators))
			for _, validator := range e.Start.Validators {
				if validator.ValidatorID < 1 || validator.ValidatorID > e.Cfg.Config.Topology.Validators || baseline[validator.ValidatorID].ValidatorID != 0 {
					return false, fmt.Sprintf("head transition baseline validator=%d is invalid or duplicated", validator.ValidatorID)
				}
				baseline[validator.ValidatorID] = validator
			}
			currentSeen := make(map[int]bool, len(e.Current.Validators))
			for _, validator := range e.Current.Validators {
				prior, ok := baseline[validator.ValidatorID]
				if !ok || currentSeen[validator.ValidatorID] || validator.HeadDecisionEpochs-prior.HeadDecisionEpochs < 2 || validator.HeadTransitions-prior.HeadTransitions < 2 || len(validator.PromotedHeadUIDs) == 0 || len(validator.DemotedHeadUIDs) == 0 {
					return false, fmt.Sprintf("validator=%d decisions=%d->%d transitions=%d->%d promoted=%v demoted=%v", validator.ValidatorID, prior.HeadDecisionEpochs, validator.HeadDecisionEpochs, prior.HeadTransitions, validator.HeadTransitions, validator.PromotedHeadUIDs, validator.DemotedHeadUIDs)
				}
				currentSeen[validator.ValidatorID] = true
			}
			return len(e.Current.Validators) == e.Cfg.Config.Topology.Validators, "every validator observed a fresh top-200 entrant and restoration exit inside this acceptance run"
		}},
		{ID: "native_head_pool_and_validator_rewards", Check: func(e *scenarioEvaluation) (bool, string) {
			return validateNativeRewardChannels(e.Cfg, e.Current)
		}},
		{ID: "two_fleet_shared_prefix_split", Check: func(e *scenarioEvaluation) (bool, string) {
			if len(e.Current.CandidateFleetUIDs) < 2 {
				return false, fmt.Sprintf("candidate_uids=%v", e.Current.CandidateFleetUIDs)
			}
			sharedUIDs := e.Current.CandidateFleetUIDs[:2]
			want := new(big.Rat).SetFrac64(1, 2)
			checked := 0
			for _, validator := range e.Current.Validators {
				masked := uint16Set(validator.MaskedUIDs)
				weights := map[uint16]*big.Rat{}
				for _, weight := range validator.AppliedWeights {
					numerator, numeratorOK := new(big.Int).SetString(weight.Numerator, 10)
					denominator, denominatorOK := new(big.Int).SetString(weight.Denominator, 10)
					if numeratorOK && denominatorOK && denominator.Sign() > 0 {
						weights[weight.UID] = new(big.Rat).SetFrac(numerator, denominator)
					}
				}
				for _, uid := range sharedUIDs {
					if masked[uid] {
						continue
					}
					if weights[uid] == nil || weights[uid].Cmp(want) != 0 {
						return false, fmt.Sprintf("validator=%d shared head_uid=%d score=%v, want 1/2", validator.ValidatorID, uid, weights[uid])
					}
					checked++
				}
			}
			return checked >= 2, fmt.Sprintf("unmasked_shared_head_observations=%d exact_score=1/2", checked)
		}},
		{ID: "validator_self_uids_masked", Check: func(e *scenarioEvaluation) (bool, string) {
			for _, validator := range e.Current.Validators {
				masked := uint16Set(validator.MaskedUIDs)
				expected := map[uint16]bool{validator.SelfUID: true}
				controlled := uint64Set(controlledNOIDsForValidator(validator.ValidatorID))
				if e.Current.Status != nil && e.Current.Status.Contracts != nil {
					for _, operator := range e.Current.Status.Contracts.Operators {
						if controlled[operator.NoID] && operator.PoolLive {
							expected[operator.PoolUID] = true
						}
					}
				}
				candidateMiners := e.Current.CandidateFleetMiners
				if len(candidateMiners) == 0 {
					candidateMiners = make([][]int, len(e.Current.CandidateFleetUIDs))
					for fleet := range candidateMiners {
						for member := 1; member <= e.Cfg.Config.Topology.ClientsPerHeadFleet; member++ {
							candidateMiners[fleet] = append(candidateMiners[fleet], fleetMemberMinerIndex(e.Cfg, fleet+1, member))
						}
					}
				}
				if len(candidateMiners) != len(e.Current.CandidateFleetUIDs) {
					return false, fmt.Sprintf("validator=%d candidate fleet membership evidence is incomplete", validator.ValidatorID)
				}
				for fleetIndex, uid := range e.Current.CandidateFleetUIDs {
					for _, miner := range candidateMiners[fleetIndex] {
						noID := uint64(operatorForMiner(e.Cfg, miner))
						if controlled[noID] {
							expected[uid] = true
						}
					}
				}
				for uid := range expected {
					if !masked[uid] {
						return false, fmt.Sprintf("validator=%d required_uid=%d missing from mask=%v", validator.ValidatorID, uid, validator.MaskedUIDs)
					}
				}
				for _, weight := range validator.AppliedWeights {
					if expected[weight.UID] && weight.Value != 0 {
						return false, fmt.Sprintf("validator=%d masked_uid=%d value=%d", validator.ValidatorID, weight.UID, weight.Value)
					}
				}
			}
			return len(e.Current.Validators) == e.Cfg.Config.Topology.Validators, fmt.Sprintf("validators=%d", len(e.Current.Validators))
		}},
		{ID: "validator_pool_scores_are_non_global", Check: func(e *scenarioEvaluation) (bool, string) {
			poolUID := map[int]uint16{}
			if e.Current.Status != nil && e.Current.Status.Contracts != nil {
				for _, operator := range e.Current.Status.Contracts.Operators {
					if operator.PoolLive {
						poolUID[int(operator.NoID)] = operator.PoolUID
					}
				}
			}
			faultedNO := e.Cfg.Config.Scenarios.QualityFaultOperator
			if _, ok := poolUID[faultedNO]; len(poolUID) != e.Cfg.Config.Topology.Operators || !ok {
				return false, fmt.Sprintf("pool_uids=%v", poolUID)
			}
			compared := 0
			for _, validator := range e.Current.Validators {
				weights := map[uint16]*big.Rat{}
				masked := uint16Set(validator.MaskedUIDs)
				for _, weight := range validator.AppliedWeights {
					n, nOK := new(big.Int).SetString(weight.Numerator, 10)
					d, dOK := new(big.Int).SetString(weight.Denominator, 10)
					if nOK && dOK && d.Sign() > 0 {
						weights[weight.UID] = new(big.Rat).SetFrac(n, d)
					}
				}
				faultedUID := poolUID[faultedNO]
				if masked[faultedUID] {
					continue
				}
				faultedScore := weights[faultedUID]
				if faultedScore == nil {
					return false, fmt.Sprintf("validator=%d has no faulted pool uid=%d", validator.ValidatorID, faultedUID)
				}
				different := false
				for noID, uid := range poolUID {
					if noID != faultedNO && !masked[uid] && weights[uid] != nil && weights[uid].Cmp(faultedScore) != 0 {
						different = true
					}
				}
				if len(poolUID)-len(controlledNOIDsForValidator(validator.ValidatorID)) < 2 {
					continue
				}
				if !different {
					return false, fmt.Sprintf("validator=%d pool scores are global/equal", validator.ValidatorID)
				}
				compared++
			}
			return compared > 0, fmt.Sprintf("unaffiliated_validator_comparisons=%d", compared)
		}},
		{ID: "claims_finalized_per_no", Check: func(e *scenarioEvaluation) (bool, string) {
			finalized := map[int]int{}
			uncertain := 0
			for _, claim := range e.Current.Claims {
				finalized[claim.NoID] += claim.Finalized
				uncertain += claim.Uncertain + claim.Failed
			}
			for noID := 1; noID <= e.Cfg.Config.Topology.Operators; noID++ {
				if finalized[noID] == 0 {
					return false, fmt.Sprintf("no=%d finalized_claims=0 uncertain_or_failed=%d", noID, uncertain)
				}
			}
			return uncertain == 0, fmt.Sprintf("finalized_by_no=%v uncertain_or_failed=%d", finalized, uncertain)
		}},
		{ID: "tier_exclusive_claim_outcomes", Check: func(e *scenarioEvaluation) (bool, string) {
			candidateNoClaim, tailFinalized := 0, 0
			for _, claim := range e.Current.Claims {
				if claim.Error != "" || claim.Uncertain != 0 || claim.Failed != 0 {
					return false, fmt.Sprintf("miner=%d error=%s uncertain=%d failed=%d", claim.MinerID, claim.Error, claim.Uncertain, claim.Failed)
				}
				if claim.MinerID <= e.Cfg.Config.Topology.fleetCandidateMiners() {
					if claim.NoClaim == 0 {
						return false, fmt.Sprintf("candidate miner=%d finalized=%d no_claim=%d", claim.MinerID, claim.Finalized, claim.NoClaim)
					}
					candidateNoClaim++
				} else {
					if claim.Finalized == 0 {
						return false, fmt.Sprintf("pool-tail miner=%d has no finalized claim", claim.MinerID)
					}
					tailFinalized++
				}
			}
			wantCandidates := e.Cfg.Config.Topology.fleetCandidateMiners()
			wantTail := e.Cfg.Config.Topology.Miners - wantCandidates
			return candidateNoClaim == wantCandidates && tailFinalized == wantTail, fmt.Sprintf("candidate_no_claim=%d/%d tail_finalized=%d/%d", candidateNoClaim, wantCandidates, tailFinalized, wantTail)
		}},
		{ID: "pool_payments_observed", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract state unavailable"
			}
			paid, ok := new(big.Int).SetString(e.Current.Status.Contracts.TotalPaid, 10)
			return ok && paid.Sign() > 0, "total_paid_rao=" + e.Current.Status.Contracts.TotalPaid
		}},
		{ID: "reserve_principal_backed", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract state unavailable"
			}
			principal, a := new(big.Int).SetString(e.Current.Status.Contracts.ReservePrincipal, 10)
			stake, b := new(big.Int).SetString(e.Current.Status.Contracts.ReserveLiveStake, 10)
			return a && b && principal.Sign() > 0 && stake.Cmp(principal) >= 0, fmt.Sprintf("principal=%s live_stake=%s", e.Current.Status.Contracts.ReservePrincipal, e.Current.Status.Contracts.ReserveLiveStake)
		}},
		{ID: "reserve_yield_auto_compounds", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract state unavailable"
			}
			principal, a := new(big.Int).SetString(e.Current.Status.Contracts.ReservePrincipal, 10)
			stake, b := new(big.Int).SetString(e.Current.Status.Contracts.ReserveLiveStake, 10)
			return a && b && principal.Sign() > 0 && stake.Cmp(principal) > 0, fmt.Sprintf("principal=%s live_stake=%s", e.Current.Status.Contracts.ReservePrincipal, e.Current.Status.Contracts.ReserveLiveStake)
		}},
		{ID: "finalized_entitlements_per_no", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract state unavailable"
			}
			finalized := map[uint64]bool{}
			for _, epoch := range e.Current.Status.Contracts.Epochs {
				for _, op := range epoch.Operators {
					if op.Status == 2 && op.PayoutRoot != "0x"+strings.Repeat("0", 64) {
						finalized[op.NoID] = true
					}
				}
			}
			return len(finalized) == e.Cfg.Config.Topology.Operators, fmt.Sprintf("finalized_no_count=%d", len(finalized))
		}},
	}
}

func uint16Set(values []uint16) map[uint16]bool {
	result := make(map[uint16]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

// weightValuesRespectCap checks the exact integer inequality used by
// Subtensor's normalized max-weight rule without introducing float rounding.
func weightValuesRespectCap(weights []IntentWeightObservation, cap uint16) (bool, uint16, uint64) {
	var maximum uint16
	var sum uint64
	for _, weight := range weights {
		sum += uint64(weight.Value)
		if weight.Value > maximum {
			maximum = weight.Value
		}
	}
	if cap == 0 || sum == 0 {
		return false, maximum, sum
	}
	return uint64(maximum)*uint64(^uint16(0)) <= sum*uint64(cap), maximum, sum
}

func uint64Set(values []uint64) map[uint64]bool {
	result := make(map[uint64]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func annotateScenarioExpectedFaults(observation *ScenarioObservation, records []ScenarioFaultRecord) error {
	if observation == nil {
		return nil
	}
	observation.ExpectedFaultIDs = nil
	observation.ExpectedFaultTargets = nil
	seenTargets := map[string]bool{}
	for _, record := range records {
		if record.Status != "active" {
			continue
		}
		observation.ExpectedFaultIDs = append(observation.ExpectedFaultIDs, record.ID)
		for _, target := range append(append([]string(nil), record.Targets...), record.Impacts...) {
			if !seenTargets[target] {
				seenTargets[target] = true
				observation.ExpectedFaultTargets = append(observation.ExpectedFaultTargets, target)
			}
		}
	}
	sort.Strings(observation.ExpectedFaultIDs)
	sort.Strings(observation.ExpectedFaultTargets)
	observation.ObservationHash = ""
	var err error
	observation.ObservationHash, err = canonicalHashHex(observation)
	return err
}

func scenarioFaultTargets(records []ScenarioFaultRecord, head uint64, includeDue bool) []string {
	seen := map[string]bool{}
	for _, record := range records {
		expected := record.Status == "active"
		if includeDue && record.Status == "pending" && head >= record.TriggerBlock {
			expected = true
		}
		if expected {
			for _, target := range append(append([]string(nil), record.Targets...), record.Impacts...) {
				seen[target] = true
			}
		}
	}
	targets := make([]string, 0, len(seen))
	for target := range seen {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets
}

func enableContinuousAdversaries(cfg *ResolvedConfig, definition *scenarioDefinition) error {
	if cfg == nil || cfg.Config == nil || definition == nil {
		return errors.New("continuous adversarial scenario configuration is incomplete")
	}
	matrix, err := loadAdversarialMatrix(cfg.Repos.SN, cfg.Config.Scenarios.Adversaries.Matrix)
	if err != nil {
		return fmt.Errorf("adversarial scenario matrix: %w", err)
	}
	if err := validateAdversarialActorCoverage(matrix, releaseAdversaryActorIDs); err != nil {
		return err
	}
	definition.AdversarialMatrixHash = matrix.Hash
	return nil
}

func scenarioDefinitionFor(cfg *ResolvedConfig, name string) (scenarioDefinition, error) {
	if name == "" {
		name = cfg.Config.Scenarios.Launch
	}
	definition := scenarioDefinition{Name: name, Checks: commonScenarioChecks()}
	switch name {
	case "smoke":
		return definition, nil
	case "precompile-conformance":
		definition.Checks = append(definition.Checks, scenarioCheck{ID: "precompile_conformance_complete", Check: func(e *scenarioEvaluation) (bool, string) {
			return e.Current.PrecompileConformanceValid, fmt.Sprintf("valid=%t error=%s", e.Current.PrecompileConformanceValid, e.Current.PrecompileConformanceError)
		}})
		return definition, nil
	case "epoch":
		definition.GoalEpochs = 1
		definition.Checks = append(definition.Checks, epochScenarioChecks()...)
		return definition, nil
	case "release-1.0":
		if cfg.Config.Scenarios.ShortEpochs < 1 {
			return scenarioDefinition{}, errors.New("release scenario requires short_epochs > 0")
		}
		definition.GoalEpochs = uint64(cfg.Config.Scenarios.ShortEpochs)
		faults, err := releaseCampaignFaults(cfg)
		if err != nil {
			return scenarioDefinition{}, err
		}
		definition.Faults = faults
		definition.Checks = append(definition.Checks, epochScenarioChecks()...)
		definition.Checks = append(definition.Checks, releaseScenarioChecks()...)
		definition.Checks = append(definition.Checks, acceptanceScenarioChecks()...)
		matrix, err := loadScenarioMatrix(cfg.Repos.SN)
		if err != nil {
			return scenarioDefinition{}, fmt.Errorf("release scenario matrix: %w", err)
		}
		definition.MatrixHash = matrix.Hash
		if err := validateScenarioMatrixCoverage(matrix, definition.Checks, definition.Faults); err != nil {
			return scenarioDefinition{}, err
		}
		definition.Checks = append(definition.Checks, scenarioCheck{ID: "scenario_matrix_coverage", Check: func(*scenarioEvaluation) (bool, string) {
			return true, fmt.Sprintf("20/20 matrix rows mapped; hash=%s", matrix.Hash)
		}})
		if err := enableContinuousAdversaries(cfg, &definition); err != nil {
			return scenarioDefinition{}, err
		}
		return definition, nil
	case "production-soak":
		if cfg.Config.Scenarios.ProductionEpochs < 3 {
			return scenarioDefinition{}, errors.New("production soak requires three complete testnet UR blocks")
		}
		// The post-preparation partial epoch is represented by the acceptance
		// baseline, not GoalEpochs. Only the three subsequent complete production
		// epochs are accepted.
		definition.GoalEpochs = uint64(cfg.Config.Scenarios.ProductionEpochs)
		definition.Faults = productionRollingFaults(cfg)
		definition.Checks = append(definition.Checks, epochScenarioChecks()...)
		definition.Checks = append(definition.Checks, releaseScenarioChecks()...)
		definition.Checks = append(definition.Checks, productionScenarioChecks()...)
		definition.Checks = append(definition.Checks, acceptanceScenarioChecks()...)
		if err := enableContinuousAdversaries(cfg, &definition); err != nil {
			return scenarioDefinition{}, err
		}
		return definition, nil
	default:
		if fault, ok := namedProcessFault(cfg, name); ok {
			definition.GoalEpochs = 1
			definition.Faults = []scenarioFaultSpec{fault}
			definition.Checks = append(definition.Checks, epochScenarioChecks()...)
			return definition, nil
		}
		return scenarioDefinition{}, fmt.Errorf("unknown scenario %q", name)
	}
}

func scenarioTimeout(cfg *ResolvedConfig, definition scenarioDefinition) time.Duration {
	blocks := definition.GoalEpochs * cfg.Policy.Settlement.EpochBlocks
	requiresHeadFaultGeometry := false
	for _, fault := range definition.Faults {
		requiresHeadFaultGeometry = requiresHeadFaultGeometry || fault.RestoreCondition == "global-head-boundary-diverged" || fault.RestoreCondition == "validator-local-head-boundary-diverged"
	}
	if definition.Name == "release-1.0" {
		// The acceptance window begins at the next complete epoch after
		// preparation. Budget the maximum remaining baseline epoch as well as
		// the accepted epochs and terminal finalization; otherwise a valid run
		// begun near an epoch start has only the generic ten-block slack in
		// which to reach that boundary.
		acceptedAndFinalized, ok := checkedAdd(blocks, cfg.Policy.Settlement.FinalizeOffsetBlocks)
		releaseLifecycle, lifecycleErr := fleetLifecycleReleaseScheduleRequired(
			hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["tempo"]),
			hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["commit_reveal_period"]),
		)
		if ok && lifecycleErr == nil && releaseLifecycle > acceptedAndFinalized {
			acceptedAndFinalized = releaseLifecycle
		}
		blocks, _ = checkedAdd(cfg.Policy.Settlement.EpochBlocks, acceptedAndFinalized)
	} else if requiresHeadFaultGeometry {
		blocks += cfg.Policy.Settlement.FinalizeOffsetBlocks
	}
	if definition.Name == "production-soak" {
		// Preparation owns the discarded partial production epoch, but the
		// watchdog starts before its next boundary. Include one complete epoch
		// of boundary-wait headroom in addition to the accepted epochs and
		// terminal finalization.
		blocks = (uint64(cfg.Config.Scenarios.ProductionEpochs)+1)*cfg.Policy.ProductionCadence.EpochBlocks + cfg.Policy.ProductionCadence.FinalizeOffsetBlocks
	}
	for _, fault := range definition.Faults {
		if end := fault.TriggerOffsetBlocks + fault.DurationBlocks; end > blocks {
			blocks = end
		}
	}
	if blocks == 0 {
		return 2 * time.Minute
	}
	seconds := blocks*cfg.Public.Chain.ExpectedBlockSeconds + 10*cfg.Public.Chain.ExpectedBlockSeconds + 120
	timeout := time.Duration(seconds) * time.Second
	if definition.AdversarialMatrixHash != "" {
		minimum := time.Duration(cfg.Config.Scenarios.Adversaries.MinimumSamplesPerActor+2) * time.Duration(cfg.Config.Scenarios.Adversaries.SampleIntervalMilliseconds) * time.Millisecond
		minimum += time.Duration(cfg.Config.Scenarios.Adversaries.RequestTimeoutMilliseconds) * time.Millisecond
		if timeout < minimum {
			timeout = minimum
		}
	}
	return timeout
}

func productionScenarioChecks() []scenarioCheck {
	return []scenarioCheck{
		{ID: "three_consecutive_fully_observed_ur_blocks", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Start == nil || e.Start.Status == nil || e.Start.Status.Contracts == nil || e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "production observation boundaries are unavailable"
			}
			// Conservatively discard the epoch containing the first snapshot. It
			// may be partial because preparation waits for the live mismatch.
			firstFull := e.Start.Status.Contracts.CurrentEpoch + 1
			complete := uint64(0)
			if e.Current.Status.Contracts.CurrentEpoch > firstFull {
				complete = e.Current.Status.Contracts.CurrentEpoch - firstFull
			}
			want := uint64(e.Cfg.Config.Scenarios.ProductionEpochs)
			return complete >= want, fmt.Sprintf("first_full_epoch=%d complete=%d want=%d current=%d", firstFull, complete, want, e.Current.Status.Contracts.CurrentEpoch)
		}},
		{ID: "dishonest_operator_deposit_penalized_and_recovered", Check: func(e *scenarioEvaluation) (bool, string) {
			if !e.Current.DishonestDepositValid || e.Current.DishonestDeposit == nil {
				return false, "dishonest-deposit evidence unavailable: " + e.Current.DishonestDepositError
			}
			bad := e.Current.DishonestDeposit
			poolUID := uint16(0)
			poolFound := false
			if e.Current.Status != nil && e.Current.Status.Contracts != nil {
				for _, operator := range e.Current.Status.Contracts.Operators {
					if operator.NoID == bad.Transaction.NoID && operator.PoolLive {
						poolUID = operator.PoolUID
						poolFound = true
					}
				}
			}
			if !poolFound {
				return false, "dishonest operator pool UID is unavailable"
			}
			recoveredUnmasked := false
			for _, validator := range e.Current.Validators {
				var recovery *validatorpkg.DepositAudit
				for auditIndex := range validator.DepositAudits {
					if validator.DepositAudits[auditIndex].NoID == bad.Transaction.NoID {
						copy := validator.DepositAudits[auditIndex]
						recovery = &copy
						break
					}
				}
				if recovery == nil || recovery.Epoch <= bad.Transaction.Epoch || !recovery.Compliant || recovery.Status != validatorpkg.DepositAuditCompliant || recovery.RequiredDepositRao != recovery.ObservedDepositRao {
					return false, fmt.Sprintf("validator=%d recovery_audit=%+v bad_epoch=%d", validator.ValidatorID, recovery, bad.Transaction.Epoch)
				}
				if slices.Contains(validator.MaskedUIDs, poolUID) {
					continue
				}
				for _, weight := range validator.AppliedWeights {
					if weight.UID == poolUID && weight.Value != 0 {
						recoveredUnmasked = true
					}
				}
			}
			return recoveredUnmasked, fmt.Sprintf("bad_epoch=%d validators_penalizing=%d recovered_unmasked=%t", bad.Transaction.Epoch, len(bad.Validators), recoveredUnmasked)
		}},
		{ID: "verify_key_rotation_preserves_history", Check: func(e *scenarioEvaluation) (bool, string) {
			for _, operator := range e.Current.Operators {
				keys := map[byte]bool{}
				proofs := map[byte]bool{}
				for _, id := range operator.VerifyKeyIDs {
					keys[id] = true
				}
				for _, id := range operator.ProofKeyIDs {
					proofs[id] = true
				}
				if !keys[0] || !keys[1] || !proofs[0] || !proofs[1] {
					return false, fmt.Sprintf("no=%d keys=%v proof_keys=%v", operator.NoID, operator.VerifyKeyIDs, operator.ProofKeyIDs)
				}
			}
			return len(e.Current.Operators) == e.Cfg.Config.Topology.Operators, "both old and rotated proofs remain publicly verifiable"
		}},
		{ID: "production_cadence_active", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract policy unavailable"
			}
			got := e.Current.Status.Contracts.Policy
			want := e.Cfg.Policy.ProductionCadence
			ok := got.EffectiveEpoch != 0 && got.EffectiveEpoch <= e.Current.Status.Contracts.CurrentEpoch && got.EpochBlocks == want.EpochBlocks && got.RootCommitWindowBlocks == want.RootCommitWindowBlocks && got.FinalizeOffsetBlocks == want.FinalizeOffsetBlocks && got.CloseGraceBlocks == want.CloseGraceBlocks
			return ok, fmt.Sprintf("effective=%d epoch=%d root=%d finalize=%d close=%d", got.EffectiveEpoch, got.EpochBlocks, got.RootCommitWindowBlocks, got.FinalizeOffsetBlocks, got.CloseGraceBlocks)
		}},
		{ID: "complete_production_epochs", Check: func(e *scenarioEvaluation) (bool, string) {
			if e.Current.Status == nil || e.Current.Status.Contracts == nil {
				return false, "contract policy unavailable"
			}
			contracts := e.Current.Status.Contracts
			effective := contracts.Policy.EffectiveEpoch
			complete := uint64(0)
			if contracts.CurrentEpoch > effective {
				complete = contracts.CurrentEpoch - effective
			}
			want := uint64(e.Cfg.Config.Scenarios.ProductionEpochs)
			return complete >= want, fmt.Sprintf("complete=%d want=%d effective=%d current=%d", complete, want, effective, contracts.CurrentEpoch)
		}},
	}
}

func appendObservation(path string, observation *ScenarioObservation) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	b, err := json.Marshal(observation)
	if err == nil {
		_, err = w.Write(append(b, '\n'))
	}
	if err == nil {
		err = w.Flush()
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func writeScenarioFaultEvidence(runDir string, faults []ScenarioFaultRecord) error {
	faultBytes, err := json.MarshalIndent(struct {
		Schema string                `json:"schema"`
		Faults []ScenarioFaultRecord `json:"faults"`
	}{"urnetwork-sim-faults-v1", faults}, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(runDir, "faults.json"), append(faultBytes, '\n'), 0o644)
}

func evaluateScenario(cfg *ResolvedConfig, definition scenarioDefinition, start, current *ScenarioObservation, window *ScenarioAcceptanceWindow, started time.Time) []AssertionRecord {
	goal := uint64(0)
	if window != nil {
		goal, _ = checkedAdd(window.FirstEpoch, window.EpochCount)
	} else if start != nil && start.Status != nil && start.Status.Contracts != nil {
		// Non-release scenarios retain their relative epoch goal. Release
		// scenarios use the complete-epoch window above.
		goal, _ = checkedAdd(start.Status.Contracts.CurrentEpoch, definition.GoalEpochs)
	}
	evaluation := &scenarioEvaluation{Cfg: cfg, Start: start, Current: current, GoalEpoch: goal, Window: window, Definition: definition}
	now := time.Now().UTC()
	assertions := make([]AssertionRecord, 0, len(definition.Checks))
	for _, check := range definition.Checks {
		passed, message := check.Check(evaluation)
		assertions = append(assertions, AssertionRecord{ID: check.ID, Passed: passed, Message: message, StartedAt: started.UTC().Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano), DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash})
	}
	sort.Slice(assertions, func(i, j int) bool { return assertions[i].ID < assertions[j].ID })
	return assertions
}

func appendFaultAssertions(assertions []AssertionRecord, records []ScenarioFaultRecord, started time.Time, current *ScenarioObservation) []AssertionRecord {
	now := time.Now().UTC()
	for _, record := range records {
		timingValid := record.RestoredBlock >= record.RestoreBlock
		if record.RestoreCondition != "" {
			minimumRestore, ok := checkedAdd(record.AppliedBlock, record.MinimumDurationBlocks)
			timingValid = ok && record.RestoredBlock >= minimumRestore && (record.RestoredBlock >= record.RestoreBlock || record.RestoreConditionMet && record.RestoreConditionBlock >= minimumRestore && record.RestoreConditionBlock <= record.RestoredBlock)
		}
		passed := record.Status == "restored" && record.AppliedBlock >= record.TriggerBlock && timingValid && len(record.Processes) == len(record.Targets)
		if record.PreAcceptance {
			_, hashOK := evidenceFixedHex(record.ArmedBlockHash, 32)
			passed = passed && record.ArmedBlock != 0 && record.ArmedBlock <= record.TriggerBlock && hashOK
		}
		if record.ActivationCondition != "" {
			passed = passed && record.ActivationConditionMet && record.ActivationConditionBlock >= record.TriggerBlock && record.ActivationConditionBlock <= record.AppliedBlock
		}
		if passed && (record.Kind == "process-restart" || record.Kind == "container-restart") {
			if len(record.RestoredProcesses) != len(record.Processes) {
				passed = false
			} else {
				before := map[string]int{}
				for _, process := range record.Processes {
					before[process.ID] = process.PID
				}
				for _, process := range record.RestoredProcesses {
					if process.PID <= 1 || process.PID == before[process.ID] {
						passed = false
					}
				}
			}
		}
		message := fmt.Sprintf("status=%s targets=%v armed=%d applied=%d restored=%d restore_deadline=%d activation=%s activation_met=%t condition=%s condition_met=%t", record.Status, record.Targets, record.ArmedBlock, record.AppliedBlock, record.RestoredBlock, record.RestoreBlock, record.ActivationCondition, record.ActivationConditionMet, record.RestoreCondition, record.RestoreConditionMet)
		if record.Error != "" {
			message += " error=" + record.Error
		}
		assertions = append(assertions, AssertionRecord{ID: "fault_" + record.ID, Passed: passed, Message: message, StartedAt: started.UTC().Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano), DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash})
	}
	sort.Slice(assertions, func(i, j int) bool { return assertions[i].ID < assertions[j].ID })
	return assertions
}

// appendAcceptanceFaultAssertion keeps ordinary fault claims inside the exact
// acceptance window. The two lifecycle filters are reported separately when
// their evidence-driven restoration uses the signed release-handoff tail.
func appendAcceptanceFaultAssertion(assertions []AssertionRecord, records []ScenarioFaultRecord, window *ScenarioAcceptanceWindow, started time.Time, current *ScenarioObservation) []AssertionRecord {
	if window == nil {
		return assertions
	}
	passed := len(records) > 0
	detail := "all scheduled faults are inside the accepted epochs"
	for _, record := range records {
		if record.PostAcceptanceEvidenceTail {
			continue
		}
		if record.TriggerBlock < window.StartBlock || record.RestoreBlock > window.EndBlock || record.AppliedBlock < window.StartBlock || record.RestoredBlock > window.EndBlock || record.PreAcceptance && (record.ArmedBlock == 0 || record.ArmedBlock >= window.StartBlock) {
			passed = false
			detail = fmt.Sprintf("fault %s interval trigger/restore=%d/%d applied/restored=%d/%d window=%d/%d", record.ID, record.TriggerBlock, record.RestoreBlock, record.AppliedBlock, record.RestoredBlock, window.StartBlock, window.EndBlock)
			break
		}
	}
	now := time.Now().UTC()
	assertions = append(assertions, AssertionRecord{ID: "faults_within_acceptance_window", Passed: passed, Message: detail, StartedAt: started.UTC().Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano), DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash})
	tailCount := 0
	tailPassed := true
	tailDetail := "no post-acceptance fault tail is configured"
	allowedTail := map[string]bool{"fleet-lifecycle-target-prune": true, "fleet-lifecycle-companion-prune": true}
	for _, record := range records {
		if !record.PostAcceptanceEvidenceTail {
			continue
		}
		tailCount++
		tailDetail = "both lifecycle filters restored within the bounded release-handoff tail"
		if !allowedTail[record.ID] || record.TriggerBlock < window.StartBlock || record.AppliedBlock < window.StartBlock || record.AppliedBlock > window.EndBlock || record.RestoreBlock <= window.EndBlock || record.RestoredBlock == 0 || record.RestoredBlock > record.RestoreBlock || !record.RestoreConditionMet {
			tailPassed = false
			tailDetail = fmt.Sprintf("fault %s tail trigger/restore=%d/%d applied/restored=%d/%d acceptance=%d/%d condition_met=%t", record.ID, record.TriggerBlock, record.RestoreBlock, record.AppliedBlock, record.RestoredBlock, window.StartBlock, window.EndBlock, record.RestoreConditionMet)
			break
		}
	}
	if tailCount != 0 {
		tailPassed = tailPassed && tailCount == 2
		assertions = append(assertions, AssertionRecord{ID: "fleet_lifecycle_fault_tail_bounded", Passed: tailPassed, Message: tailDetail, StartedAt: started.UTC().Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano), DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash})
	}
	sort.Slice(assertions, func(i, j int) bool { return assertions[i].ID < assertions[j].ID })
	return assertions
}

func assertionsPass(assertions []AssertionRecord) bool {
	if len(assertions) == 0 {
		return false
	}
	for _, assertion := range assertions {
		if !assertion.Passed {
			return false
		}
	}
	return true
}

func writeInitialScenarioFailure(cfg *ResolvedConfig, runDir, runID, definitionHash string, definition scenarioDefinition, started time.Time, observation *ScenarioObservation, attempt *scenarioCampaignAttempt, failure error) (*ScenarioResult, error) {
	completed := time.Now().UTC()
	observationHash := ""
	if observation != nil {
		observationHash = observation.ObservationHash
		_ = appendObservation(filepath.Join(runDir, "observations.jsonl"), observation)
	}
	assertion := AssertionRecord{ID: "initial_observation", Passed: false, Message: failure.Error(), StartedAt: started.Format(time.RFC3339Nano), CompletedAt: completed.Format(time.RFC3339Nano), DurationSeconds: completed.Sub(started).Seconds(), ObservationHash: observationHash}
	result := &ScenarioResult{
		Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", RunID: runID,
		DeploymentID: cfg.Config.Deployment.DeploymentID, Name: definition.Name, ScenarioDefinition: definitionHash, ScenarioMatrix: definition.MatrixHash, AdversarialMatrix: definition.AdversarialMatrixHash,
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		StartedAt: started.Format(time.RFC3339Nano), CompletedAt: completed.Format(time.RFC3339Nano),
		Assertions: []AssertionRecord{assertion}, Result: "fail",
	}
	applyScenarioAttemptBinding(result, attempt)
	attachScenarioAnomalyGate(result, completed, nil, observation)
	result.EvidenceHash, _ = canonicalScenarioResultHash(result)
	if err := writeScenarioOutputs(cfg, runDir, result, observation); err != nil {
		return result, err
	}
	return result, failure
}

func beginScenarioCampaignPreparation(ctx context.Context, phase, runID string, options scenarioRunOptions) error {
	var immutableHandoff []byte
	if options.Attempt != nil && phase == "production-soak" {
		var err error
		immutableHandoff, err = options.Attempt.authenticateProductionHandoff()
		if err != nil {
			return fmt.Errorf("authenticate exact release lifecycle handoff before production preparation: %w", err)
		}
		if options.FleetLifecycle == nil {
			return errors.New("production campaign has no fleet lifecycle successor authenticator")
		}
		authenticator, ok := options.FleetLifecycle.(scenarioFleetLifecycleHandoffAuthenticator)
		if !ok {
			return errors.New("production fleet lifecycle cannot authenticate an exact release handoff")
		}
		gate := options.Attempt.payload.PriorRelease
		if err := authenticator.AuthenticateReleaseHandoff(immutableHandoff, gate.LifecycleHandoff.ContentHash, gate.RunID); err != nil {
			return fmt.Errorf("bind exact release lifecycle handoff to production successor: %w", err)
		}
	}
	if options.Prepare != nil && (options.Attempt == nil || !options.Attempt.payload.PreparationComplete) {
		if err := options.Prepare(ctx); err != nil {
			return fmt.Errorf("prepare scenario: %w", err)
		}
		if options.Attempt != nil {
			if err := options.Attempt.updateProgress(options.Attempt.payload.HandoffAuthenticated, true); err != nil {
				return fmt.Errorf("commit scenario preparation boundary: %w", err)
			}
		}
	}
	if options.Prepare == nil && options.Attempt != nil && !options.Attempt.payload.PreparationComplete {
		if err := options.Attempt.updateProgress(options.Attempt.payload.HandoffAuthenticated, true); err != nil {
			return fmt.Errorf("commit empty scenario preparation boundary: %w", err)
		}
	}
	if options.FleetLifecycle != nil {
		if err := options.FleetLifecycle.BeginPhase(phase, runID); err != nil {
			if phase == "production-soak" {
				return fmt.Errorf("initialize authenticated production fleet lifecycle successor: %w", err)
			}
			return fmt.Errorf("initialize prepared fleet lifecycle: %w", err)
		}
	}
	return nil
}

func finalizeAdversaryEvidence(campaign adversaryCampaign, runDir string, started time.Time, observationHash string, completed time.Time) (*AdversaryCampaignEvidence, []AssertionRecord, error) {
	if campaign == nil {
		return nil, nil, nil
	}
	campaign.MarkHappyPathCompleted(completed)
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	evidence, stopErr := campaign.Stop(stopCtx)
	if evidence != nil {
		b, err := json.MarshalIndent(evidence, "", "  ")
		if err == nil {
			err = atomicWrite(filepath.Join(runDir, "adversaries.json"), append(b, '\n'), 0o644)
		}
		if err != nil {
			stopErr = errors.Join(stopErr, err)
		}
	}
	assertions := adversaryAssertions(evidence, started, observationHash)
	if stopErr != nil {
		now := time.Now().UTC()
		assertions = append(assertions, AssertionRecord{ID: "adversary_campaign_stop", Passed: false, Message: stopErr.Error(), StartedAt: started.UTC().Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano), DurationSeconds: now.Sub(started).Seconds(), ObservationHash: observationHash})
		sort.Slice(assertions, func(i, j int) bool { return assertions[i].ID < assertions[j].ID })
	}
	return evidence, assertions, stopErr
}

func runScenarioWithProbe(ctx context.Context, cfg *ResolvedConfig, stateDir string, definition scenarioDefinition, probe scenarioProbe, options scenarioRunOptions) (*ScenarioResult, error) {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Duration(max64(1, cfg.Public.Chain.ExpectedBlockSeconds)) * time.Second
	}
	if options.Timeout <= 0 {
		options.Timeout = scenarioTimeout(cfg, definition)
	}
	started := options.Now().UTC()
	runID := fmt.Sprintf("%s-%s", started.Format("20060102T150405.000000000Z"), definition.Name)
	if options.Attempt != nil {
		if options.Attempt.payload.Phase != definition.Name {
			return nil, errors.New("scenario campaign attempt phase differs from its definition")
		}
		attemptStarted, err := time.Parse(time.RFC3339Nano, options.Attempt.payload.StartedAt)
		if err != nil {
			return nil, fmt.Errorf("scenario campaign attempt start: %w", err)
		}
		started = attemptStarted.UTC()
		runID = options.Attempt.payload.RunID
	}
	runDir := filepath.Join(stateDir, "runs", runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, err
	}
	definitionHash, err := scenarioDefinitionHash(definition)
	if err != nil {
		return nil, fmt.Errorf("hash scenario definition: %w", err)
	}
	processSessionID := options.ProcessSessionID
	if processSessionID == "" {
		if scenarioProcessSessionIDErr != nil {
			return nil, scenarioProcessSessionIDErr
		}
		processSessionID = scenarioProcessSessionID
	}
	if !validCanonicalHashHex(processSessionID) {
		return nil, errors.New("scenario process session identity is noncanonical")
	}
	if options.Attempt != nil && options.Attempt.payload.AcceptanceBoundary != nil {
		reason := "attempt-reentered-after-acceptance"
		if !strings.EqualFold(options.Attempt.payload.AcceptanceBoundary.ProcessSessionID, processSessionID) {
			reason = "process-session-changed"
		}
		if err := options.Attempt.invalidateAcceptance(reason, options.Now().UTC()); err != nil {
			return nil, fmt.Errorf("invalidate interrupted scenario acceptance: %w", err)
		}
		interrupted := fmt.Errorf("scenario campaign acceptance was interrupted (%s); a fresh signed deployment and lifecycle namespace is required", reason)
		if options.FaultDriver != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := options.FaultDriver.Recover(cleanupCtx); err != nil {
				interrupted = errors.Join(interrupted, fmt.Errorf("recover interrupted scenario faults: %w", err))
			}
		}
		return nil, interrupted
	}
	scenarioCompleted := false
	defer func() {
		if scenarioCompleted || options.Attempt == nil || options.Attempt.payload.AcceptanceBoundary == nil {
			return
		}
		_ = options.Attempt.invalidateAcceptance("execution-exited-before-completion", options.Now().UTC())
	}()
	if (definition.Name == "release-1.0" || definition.Name == "production-soak") && options.ProcessLogs == nil {
		return nil, errors.New("release and production scenarios require the persisted process log gate")
	}
	if options.ProcessLogs != nil {
		if err := options.ProcessLogs.WriteEvidence(runDir); err != nil {
			return nil, fmt.Errorf("initialize scenario process log evidence: %w", err)
		}
	}
	if definition.AdversarialMatrixHash != "" {
		if options.Adversaries == nil {
			return nil, errors.New("release scenario requires a continuous adversarial campaign")
		}
		if err := options.Adversaries.Start(ctx); err != nil {
			return nil, fmt.Errorf("start continuous adversarial campaign: %w", err)
		}
		options.Adversaries.MarkHappyPathStarted(options.Now().UTC())
	}
	adversariesFinalized := false
	observationHistory := []*ScenarioObservation{}
	var faults []ScenarioFaultRecord
	faultCleanupComplete := false
	recoverInterruptedFaults := func() error {
		if options.FaultDriver == nil || faultCleanupComplete {
			return nil
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := options.FaultDriver.Recover(cleanupCtx); err != nil {
			return err
		}
		faultCleanupComplete = true
		return nil
	}
	defer func() {
		if options.Adversaries == nil || adversariesFinalized {
			return
		}
		options.Adversaries.MarkHappyPathCompleted(options.Now().UTC())
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = options.Adversaries.Stop(cleanupCtx)
	}()
	initialFailure := func(observation *ScenarioObservation, failure error) (*ScenarioResult, error) {
		if options.Attempt != nil && options.Attempt.payload.AcceptanceBoundary != nil {
			if cleanupErr := recoverInterruptedFaults(); cleanupErr != nil {
				failure = errors.Join(failure, fmt.Errorf("recover interrupted scenario faults: %w", cleanupErr))
			}
		}
		if logErr := scanScenarioProcessLogs(options.ProcessLogs, runDir, observation, true, activeProcessLogFaultScopes(faults)...); logErr != nil {
			failure = errors.Join(failure, fmt.Errorf("final process log gate: %w", logErr))
		}
		failureHistory := append([]*ScenarioObservation(nil), observationHistory...)
		if observation != nil && (len(failureHistory) == 0 || failureHistory[len(failureHistory)-1] != observation) {
			failureHistory = append(failureHistory, observation)
		}
		result, resultErr := writeInitialScenarioFailure(cfg, runDir, runID, definitionHash, definition, started, observation, options.Attempt, failure)
		observationHash := ""
		if len(failureHistory) != 0 {
			observationHash = failureHistory[len(failureHistory)-1].ObservationHash
		}
		evidence, adversaryRecords, stopErr := finalizeAdversaryEvidence(options.Adversaries, runDir, started, observationHash, options.Now().UTC())
		adversariesFinalized = true
		var rewriteErr error
		if result != nil {
			applyScenarioAttemptBinding(result, options.Attempt)
			result.Adversaries = evidence
			result.Assertions = append(result.Assertions, adversaryRecords...)
			attachScenarioAnomalyGate(result, options.Now().UTC(), nil, observation, failureHistory...)
			result.EvidenceHash, _ = canonicalScenarioResultHash(result)
			rewriteErr = writeScenarioOutputs(cfg, runDir, result, observation)
		}
		return result, errors.Join(resultErr, stopErr, rewriteErr)
	}
	// An owner-signed acceptance boundary may only be created by this process
	// invocation. Any pre-existing boundary was rejected above as an interrupted
	// attempt, so post-boundary execution can never be resumed.
	boundaryCommitted := false
	if len(definition.Faults) != 0 && options.FaultDriver == nil {
		return initialFailure(nil, errors.New("scenario fault schedule requires a fault driver"))
	}
	var start, current, campaignStart *ScenarioObservation
	var window *ScenarioAcceptanceWindow
	prearmedFaults := map[string][]FaultProcessEvidence{}
	defer func() {
		// A post-boundary exit permanently fails the attempt. Best-effort exact
		// restoration prevents its injected state from lingering, while the signed
		// attempt and fault ledger retain the failure evidence.
		if options.FaultDriver == nil || faultCleanupComplete {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		restored := map[string]bool{}
		for index := len(definition.Faults) - 1; index >= 0; index-- {
			spec := definition.Faults[index]
			active := index < len(faults) && faults[index].Status == "active"
			_, prearmed := prearmedFaults[spec.ID]
			prearmedPending := prearmed && (index >= len(faults) || faults[index].Status == "pending" || faults[index].Status == "active")
			if (active || prearmedPending) && !restored[spec.ID] {
				_, _ = options.FaultDriver.Restore(cleanupCtx, spec)
				restored[spec.ID] = true
			}
		}
	}()
	if len(definition.Faults) != 0 {
		if err := options.FaultDriver.Recover(ctx); err != nil {
			return initialFailure(nil, fmt.Errorf("recover prior scenario fault: %w", err))
		}
	}
	start, err = probe.Snapshot(ctx)
	if err != nil {
		return initialFailure(nil, fmt.Errorf("initial scenario observation: %w", err))
	}
	if err := scanScenarioProcessLogs(options.ProcessLogs, runDir, start, false); err != nil {
		return initialFailure(start, fmt.Errorf("initial process log gate: %w", err))
	}
	if start.Status == nil || start.Status.Contracts == nil {
		return initialFailure(start, errors.New("scenario requires an installed contract deployment"))
	}
	campaignStart = start
	observationHistory = append(observationHistory, start)
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), start); err != nil {
		return initialFailure(start, fmt.Errorf("persist initial scenario observation: %w", err))
	}
	current = start
	// Public-RPC deployment can consume arbitrary epochs, while precompile,
	// governance, key rotation, and dishonest-deposit preparation remain in
	// the observed happy path before the exact acceptance baseline is signed.
	if options.Prepare != nil || options.FleetLifecycle != nil || options.Attempt != nil {
		if err := beginScenarioCampaignPreparation(ctx, definition.Name, runID, options); err != nil {
			return initialFailure(start, err)
		}
	}
	prearmedFaults, err = armPreAcceptanceFaults(ctx, definition.Faults, options.FaultDriver)
	if err != nil {
		return initialFailure(start, err)
	}
	if options.Prepare != nil {
		prepared, prepareErr := probe.Snapshot(ctx)
		if prepareErr != nil {
			return initialFailure(start, fmt.Errorf("post-preparation scenario observation: %w", prepareErr))
		}
		if err := scanScenarioProcessLogs(options.ProcessLogs, runDir, prepared, false); err != nil {
			return initialFailure(prepared, fmt.Errorf("post-preparation process log gate: %w", err))
		}
		if prepared.Status == nil || prepared.Status.Contracts == nil {
			return initialFailure(prepared, errors.New("post-preparation observation lost deployment or contract state"))
		}
		current = prepared
		observationHistory = append(observationHistory, current)
		if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), current); err != nil {
			return initialFailure(current, fmt.Errorf("persist post-preparation scenario observation: %w", err))
		}
	}
	// A release acceptance interval begins only at the next contract boundary
	// after preparation. The current partial epoch remains in history but
	// cannot count toward the exact acceptance gate.
	window, err = buildScenarioAcceptanceWindow(cfg, definition, current)
	if err != nil {
		return initialFailure(current, fmt.Errorf("build complete-epoch acceptance window: %w", err))
	}
	if window != nil {
		start = current
	}
	faultBase := current.Status.Contracts.FinalizedHead.Number
	if window != nil {
		faultBase = window.StartBlock
	}
	faults, err = initializeFaultRecords(faultBase, definition.Faults)
	if err != nil {
		return initialFailure(current, fmt.Errorf("initialize scenario faults: %w", err))
	}
	for index := range faults {
		if !faults[index].PreAcceptance {
			continue
		}
		processes, ok := prearmedFaults[faults[index].ID]
		if !ok || len(processes) != len(faults[index].Targets) {
			return initialFailure(current, fmt.Errorf("pre-acceptance fault %s has no exact armed process census", faults[index].ID))
		}
		faults[index].ArmedBlock = current.Status.Contracts.FinalizedHead.Number
		faults[index].ArmedBlockHash = current.Status.Contracts.FinalizedHead.Hash
	}
	if options.Attempt != nil && window != nil {
		var adversaryStart *AdversaryCampaignEvidence
		if options.Adversaries != nil {
			adversaryStart = options.Adversaries.Snapshot()
		}
		if err := options.Attempt.bindAcceptanceBoundary(runDir, processSessionID, definitionHash, adversaryStart, options.Now().UTC(), campaignStart, current, window, faults); err != nil {
			return initialFailure(current, fmt.Errorf("commit scenario acceptance boundary: %w", err))
		}
		boundaryCommitted = true
	}
	if err := writeScenarioFaultEvidence(runDir, faults); err != nil {
		return initialFailure(current, fmt.Errorf("persist initial scenario faults: %w", err))
	}
	if options.FleetLifecycle != nil {
		if err := options.FleetLifecycle.BindAcceptanceWindowForPhase(definition.Name, window); err != nil {
			return initialFailure(current, fmt.Errorf("bind fleet lifecycle acceptance window: %w", err))
		}
	}
	if definition.Name == "release-1.0" {
		if ok, detail := headBoundaryUIDTieGeometry(cfg, start); !ok {
			return initialFailure(current, fmt.Errorf("head-boundary fault preflight: %s", detail))
		}
	}
	persistRuntimeObservation := func(observation *ScenarioObservation) error {
		if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), observation); err != nil {
			return err
		}
		if options.Attempt != nil && options.Attempt.payload.AcceptanceBoundary != nil {
			if err := options.Attempt.updateAuthenticatedRuntime(runDir, faults); err != nil {
				return fmt.Errorf("commit signed scenario runtime checkpoint: %w", err)
			}
		}
		return nil
	}
	assertions := appendFaultAssertions(evaluateScenario(cfg, definition, start, current, window, started), faults, started, current)
	assertions = appendAcceptanceFaultAssertion(assertions, faults, window, started, current)
	// Preparation actions have their own bounded waits. Give the exact accepted
	// interval its complete timeout instead of consuming it during preparation.
	deadline := options.Now().Add(options.Timeout)
	var faultErr error
	var terminalErr error
	var runtimeAssertions []AssertionRecord
	snapshotFailureCount := 0
scenarioLoop:
	for (!assertionsPass(assertions) || !faultsComplete(faults) || (options.FleetLifecycle != nil && !options.FleetLifecycle.Complete()) || (options.Adversaries != nil && !options.Adversaries.Ready())) && options.Now().Before(deadline) {
		timer := time.NewTimer(options.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			terminalErr = ctx.Err()
			break scenarioLoop
		case <-timer.C:
		}
		next, snapshotErr := probe.Snapshot(ctx)
		if snapshotErr != nil {
			snapshotFailureCount++
			now := options.Now().UTC()
			runtimeAssertions = append(runtimeAssertions, AssertionRecord{
				ID: fmt.Sprintf("scenario_snapshot_%06d", snapshotFailureCount), Passed: false,
				Message:   fmt.Sprintf("transient scenario observation failed: %v", snapshotErr),
				StartedAt: started.Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano),
				DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash,
			})
			continue
		}
		previous := current
		current = next
		if err := annotateScenarioExpectedFaults(current, faults); err != nil {
			return initialFailure(current, fmt.Errorf("annotate expected scenario faults: %w", err))
		}
		processLogErr := scanScenarioProcessLogs(options.ProcessLogs, runDir, current, false, activeProcessLogFaultScopes(faults)...)
		observationHistory = append(observationHistory, current)
		if current.Status == nil || current.Status.Contracts == nil {
			snapshotFailureCount++
			now := options.Now().UTC()
			runtimeAssertions = append(runtimeAssertions, AssertionRecord{
				ID: fmt.Sprintf("scenario_snapshot_%06d", snapshotFailureCount), Passed: false,
				Message:   "scenario observation lost deployment or contract state",
				StartedAt: started.Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano),
				DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash,
			})
			terminalErr = errors.New("scenario observation lost deployment or contract state")
			current = previous
			break scenarioLoop
		}
		if processLogErr != nil {
			if err := persistRuntimeObservation(current); err != nil {
				return initialFailure(current, fmt.Errorf("persist process-log failure observation: %w", err))
			}
			terminalErr = fmt.Errorf("runtime process log gate: %w", processLogErr)
			break scenarioLoop
		}
		if len(faults) != 0 {
			faultScopesBefore := activeProcessLogFaultScopes(faults)
			if options.Adversaries != nil {
				// Attribute endpoint failures before applying a due process fault;
				// the actor may have an in-flight request when SIGTERM/SIGSTOP lands.
				options.Adversaries.SetExpectedFaultTargets(scenarioFaultTargets(faults, current.Status.Contracts.FinalizedHead.Number, true))
			}
			faultErr = advanceFaultsWithConditions(
				ctx, current.Status.Contracts.FinalizedHead, definition.Faults, faults, options.FaultDriver,
				func(spec scenarioFaultSpec) (bool, error) {
					return faultConditionMet(cfg, start, current, spec.ActivationCondition)
				},
				func(spec scenarioFaultSpec) (bool, error) {
					return faultRestoreConditionMet(cfg, start, current, spec)
				},
			)
			if options.Adversaries != nil {
				options.Adversaries.SetExpectedFaultTargets(scenarioFaultTargets(faults, current.Status.Contracts.FinalizedHead.Number, false))
			}
			if err := writeScenarioFaultEvidence(runDir, faults); err != nil {
				return initialFailure(current, fmt.Errorf("persist scenario faults: %w", err))
			}
			transitionScopes := mergeProcessLogFaultScopes(faultScopesBefore, activeProcessLogFaultScopes(faults))
			if logErr := scanScenarioProcessLogs(options.ProcessLogs, runDir, current, false, transitionScopes...); logErr != nil {
				processLogErr = errors.Join(processLogErr, fmt.Errorf("fault-transition process log gate: %w", logErr))
			}
		}
		if faultErr == nil && options.FleetLifecycle != nil {
			if lifecycleErr := options.FleetLifecycle.Advance(ctx, current, faults); lifecycleErr != nil {
				faultErr = fmt.Errorf("advance fleet lifecycle: %w", lifecycleErr)
			}
		}
		if processLogErr != nil {
			if err := persistRuntimeObservation(current); err != nil {
				return initialFailure(current, fmt.Errorf("persist fault-transition process-log observation: %w", err))
			}
			terminalErr = fmt.Errorf("runtime process log gate: %w", processLogErr)
			break scenarioLoop
		}
		if err := persistRuntimeObservation(current); err != nil {
			return initialFailure(current, fmt.Errorf("persist scenario observation: %w", err))
		}
		assertions = appendFaultAssertions(evaluateScenario(cfg, definition, start, current, window, started), faults, started, current)
		assertions = appendAcceptanceFaultAssertion(assertions, faults, window, started, current)
		if faultErr != nil {
			break
		}
	}
	acceptanceIncomplete := terminalErr != nil || faultErr != nil || !assertionsPass(assertions) || !faultsComplete(faults) || options.FleetLifecycle != nil && !options.FleetLifecycle.Complete() || options.Adversaries != nil && !options.Adversaries.Ready()
	if boundaryCommitted && acceptanceIncomplete {
		if terminalErr == nil {
			terminalErr = errors.New("signed scenario acceptance ended before every assertion, fault, lifecycle, and adversary gate completed")
		}
		if cleanupErr := recoverInterruptedFaults(); cleanupErr != nil {
			terminalErr = errors.Join(terminalErr, fmt.Errorf("recover interrupted scenario faults: %w", cleanupErr))
		}
	}
	if terminalErr != nil {
		now := options.Now().UTC()
		assertions = append(assertions, AssertionRecord{ID: "scenario_context", Passed: false, Message: terminalErr.Error(), StartedAt: started.Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano), DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash})
		sort.Slice(assertions, func(i, j int) bool { return assertions[i].ID < assertions[j].ID })
	}
	completed := options.Now().UTC()
	adversaryEvidence, adversaryRecords, adversaryErr := finalizeAdversaryEvidence(options.Adversaries, runDir, started, current.ObservationHash, completed)
	adversariesFinalized = true
	if options.Attempt != nil && options.Attempt.payload.AcceptanceBoundary != nil {
		boundary := options.Attempt.payload.AcceptanceBoundary
		continuous := adversaryEvidence != nil && strings.EqualFold(adversaryEvidence.MatrixHash, boundary.AdversarialMatrixHash) && adversaryEvidence.StartedAt == boundary.AdversaryStartedAt && adversaryEvidence.HappyPathStartedAt == boundary.AdversaryHappyPathStartedAt && adversaryEvidence.Status == "stopped" && adversaryEvidence.StartedBeforeHappyPath && adversaryEvidence.StoppedAfterHappyPath
		message := "continuous adversary evidence extends from the signed campaign start marker through happy-path completion"
		if !continuous {
			message = "continuous adversary evidence does not extend from the signed campaign start marker through happy-path completion"
			adversaryErr = errors.Join(adversaryErr, errors.New(message))
		}
		acceptanceStarted, _ := time.Parse(time.RFC3339Nano, boundary.AcceptanceStartedAt)
		adversaryRecords = append(adversaryRecords, AssertionRecord{
			ID: "adversary_signed_start_continuity", Passed: continuous, Message: message,
			StartedAt: boundary.AcceptanceStartedAt, CompletedAt: completed.Format(time.RFC3339Nano),
			DurationSeconds: completed.Sub(acceptanceStarted).Seconds(), ObservationHash: current.ObservationHash,
		})
	}
	assertions = append(assertions, runtimeAssertions...)
	assertions = append(assertions, adversaryRecords...)
	var lifecycleHandoff *ScenarioLifecycleHandoff
	if options.FleetLifecycle != nil {
		passed, message := fleetLifecycleCompletionStatus(options.FleetLifecycle)
		if passed && definition.Name == "release-1.0" && options.Attempt != nil {
			binding, err := captureScenarioLifecycleHandoff(cfg, stateDir, runDir, runID)
			if err != nil {
				passed = false
				message = err.Error()
				if terminalErr == nil {
					terminalErr = err
				}
			} else {
				lifecycleHandoff = binding
			}
		}
		now := options.Now().UTC()
		assertions = append(assertions, AssertionRecord{
			ID: "fleet_lifecycle_complete", Passed: passed, Message: message,
			StartedAt: started.Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano),
			DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash,
		})
		if !passed && terminalErr == nil {
			terminalErr = errors.New(message)
		}
	}
	if adversaryErr != nil && terminalErr == nil {
		terminalErr = adversaryErr
	}
	if logErr := scanScenarioProcessLogs(options.ProcessLogs, runDir, current, false, activeProcessLogFaultScopes(faults)...); logErr != nil {
		now := options.Now().UTC()
		if persistErr := persistRuntimeObservation(current); persistErr != nil {
			logErr = errors.Join(logErr, fmt.Errorf("persist process-log completion observation: %w", persistErr))
		}
		assertions = append(assertions, AssertionRecord{
			ID: "process_log_completion", Passed: false, Message: logErr.Error(),
			StartedAt: started.Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano),
			DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash,
		})
		if terminalErr == nil {
			terminalErr = logErr
		}
	}
	sort.Slice(assertions, func(i, j int) bool { return assertions[i].ID < assertions[j].ID })
	completed = options.Now().UTC()
	result := &ScenarioResult{
		Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", RunID: runID,
		DeploymentID: cfg.Config.Deployment.DeploymentID, Name: definition.Name, ScenarioDefinition: definitionHash, ScenarioMatrix: definition.MatrixHash, AdversarialMatrix: definition.AdversarialMatrixHash,
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		StartedAt: started.Format(time.RFC3339Nano), CompletedAt: completed.Format(time.RFC3339Nano),
		CampaignStartHead: campaignStart.Status.Contracts.FinalizedHead, CampaignStartEpoch: campaignStart.Status.Contracts.CurrentEpoch,
		StartHead: start.Status.Contracts.FinalizedHead, EndHead: current.Status.Contracts.FinalizedHead,
		StartEpoch: start.Status.Contracts.CurrentEpoch, EndEpoch: current.Status.Contracts.CurrentEpoch, AcceptanceWindow: window,
		Assertions: assertions, Faults: faults, Adversaries: adversaryEvidence,
		ValueReconciliation: map[string]string{"captured_rao": current.Status.Contracts.TotalCaptured, "paid_rao": current.Status.Contracts.TotalPaid, "escrow_accounted_rao": current.Status.Contracts.EscrowAccounted, "pending_funding_rao": current.Status.Contracts.PendingFunding, "outstanding_liability_rao": current.Status.Contracts.Outstanding, "live_escrow_stake_rao": current.Status.Contracts.LiveEscrowStake, "reserve_principal_rao": current.Status.Contracts.ReservePrincipal, "reserve_live_stake_rao": current.Status.Contracts.ReserveLiveStake},
		Result:              "pass",
	}
	result.LifecycleHandoff = lifecycleHandoff
	applyScenarioAttemptBinding(result, options.Attempt)
	attachScenarioAnomalyGate(result, completed, campaignStart, current, observationHistory...)
	result.EvidenceHash, _ = canonicalScenarioResultHash(result)
	if err := writeScenarioOutputs(cfg, runDir, result, current); err != nil {
		return nil, err
	}
	publishedBundlePayloadHash := ""
	publishedBundleResultHash := ""
	if options.Publish {
		if result.Result == "pass" {
			if logErr := scanScenarioProcessLogs(options.ProcessLogs, runDir, current, false); logErr != nil {
				now := options.Now().UTC()
				if persistErr := persistRuntimeObservation(current); persistErr != nil {
					logErr = errors.Join(logErr, fmt.Errorf("persist process-log prepublication observation: %w", persistErr))
				}
				result.Assertions = append(result.Assertions, AssertionRecord{
					ID: "process_log_prepublication", Passed: false, Message: logErr.Error(),
					StartedAt: result.StartedAt, CompletedAt: now.Format(time.RFC3339Nano),
					DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash,
				})
				attachScenarioAnomalyGate(result, now, campaignStart, current, observationHistory...)
				result.EvidenceHash, _ = canonicalScenarioResultHash(result)
			}
		}
		publishResult := *result
		publishResult.PublishedEvidence = nil
		publishedBundleResultHash = publishResult.EvidenceHash
		bundle := ScenarioEvidenceBundle{Schema: "urnetwork-sim-scenario-evidence-v1", Result: &publishResult, Observation: current, Analysis: analyzeScenarioObservation(cfg, current)}
		bundlePayload, marshalErr := json.Marshal(bundle)
		if marshalErr == nil {
			publishedBundlePayloadHash = bytesSHA256(bundlePayload)
		}
		var published []PublishedEvidence
		publishErr := marshalErr
		if publishErr == nil {
			published, publishErr = publishEvidence(ctx, cfg, options.Roles, stateDir, "scenario-bundle", runID, bundle)
		}
		if publishErr == nil {
			publishErr = verifyPublishedEvidenceOrigins(ctx, cfg, options.Roles, published)
		}
		if publishErr != nil {
			result.Assertions = append(result.Assertions, AssertionRecord{ID: "evidence_publication", Passed: false, Message: publishErr.Error(), StartedAt: result.StartedAt, CompletedAt: options.Now().UTC().Format(time.RFC3339Nano), ObservationHash: current.ObservationHash})
		} else {
			result.PublishedEvidence = published
		}
		if logErr := scanScenarioProcessLogs(options.ProcessLogs, runDir, current, false); logErr != nil {
			now := options.Now().UTC()
			if persistErr := persistRuntimeObservation(current); persistErr != nil {
				logErr = errors.Join(logErr, fmt.Errorf("persist process-log publication observation: %w", persistErr))
			}
			result.Assertions = append(result.Assertions, AssertionRecord{
				ID: "process_log_publication", Passed: false, Message: logErr.Error(),
				StartedAt: result.StartedAt, CompletedAt: now.Format(time.RFC3339Nano),
				DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash,
			})
		}
		// Keep the candidate's canonical completion instant stable. A clean
		// publication scan must not change its signed result merely because the
		// wall clock advanced while operator origins were checked.
		candidateErr := refreshPublishedScenarioCandidate(result, completed, campaignStart, current, publishedBundleResultHash, observationHistory...)
		if result.Result == "pass" && candidateErr != nil {
			now := options.Now().UTC()
			result.Assertions = append(result.Assertions, AssertionRecord{
				ID: "evidence_publication_snapshot", Passed: false, Message: "process-log publication scan changed the signed scenario candidate",
				StartedAt: result.StartedAt, CompletedAt: now.Format(time.RFC3339Nano), DurationSeconds: now.Sub(started).Seconds(), ObservationHash: current.ObservationHash,
			})
			attachScenarioAnomalyGate(result, now, campaignStart, current, observationHistory...)
			result.EvidenceHash, _ = canonicalScenarioResultHash(result)
		}
		if err := writeScenarioOutputs(cfg, runDir, result, current); err != nil {
			return nil, err
		}
	}
	finalEvidenceFailure := func(id string, failure error) (*ScenarioResult, error) {
		now := options.Now().UTC()
		result.Assertions = append(result.Assertions, AssertionRecord{
			ID: id, Passed: false, Message: failure.Error(), StartedAt: result.StartedAt,
			CompletedAt: now.Format(time.RFC3339Nano), DurationSeconds: now.Sub(started).Seconds(),
			ObservationHash: current.ObservationHash,
		})
		attachScenarioAnomalyGate(result, now, campaignStart, current, observationHistory...)
		result.EvidenceHash, _ = canonicalScenarioResultHash(result)
		return result, errors.Join(failure, writeScenarioOutputs(cfg, runDir, result, current))
	}
	if result.Result == "pass" {
		if options.Publish && (definition.Name == "release-1.0" || definition.Name == "production-soak") {
			collect := options.CollectFinalSemantic
			if collect == nil {
				collect = CollectFinalSemanticInputs
			}
			if _, err := collect(ctx, cfg, stateDir, runDir, result, current, observationHistory); err != nil {
				return finalEvidenceFailure("final_semantic_input_collection", err)
			}
		}
		if err := scanEvidenceSecrets(stateDir, runDir, options.Roles, cfg.WalletSecret, cfg.WalletMaterial, cfg.WalletPasswordSecret, cfg.WalletPassword); err != nil {
			return finalEvidenceFailure("evidence_secret_scan", err)
		}
		// This is the final supervised-process read before the immutable hashes
		// and completion marker. The semantic producer below may perform pinned
		// public-chain archive reads and local artifact reads, but it must not call
		// a supervised operator API that can append a blocking process log.
		if err := scanScenarioProcessLogs(options.ProcessLogs, runDir, current, true); err != nil {
			if persistErr := persistRuntimeObservation(current); persistErr != nil {
				err = errors.Join(err, fmt.Errorf("persist final process-log observation: %w", persistErr))
			}
			return finalEvidenceFailure("process_log_final", err)
		}
		if options.Publish {
			if err := refreshPublishedScenarioCandidate(result, completed, campaignStart, current, publishedBundleResultHash, observationHistory...); err != nil {
				return finalEvidenceFailure("process_log_final_snapshot", err)
			}
			if err := writeScenarioOutputs(cfg, runDir, result, current); err != nil {
				return result, err
			}
		}
		hashes, err := evidenceFileHashes(runDir, cfg.Config.Topology.Operators)
		if err != nil {
			return finalEvidenceFailure("evidence_file_hashes", err)
		}
		publishedManifestHash := ""
		if options.Publish {
			manifest, publishErr := publishCampaignEvidenceArchive(ctx, cfg, options.Roles, stateDir, runID, result.EvidenceHash, publishedBundlePayloadHash, hashes, nil)
			if publishErr != nil {
				return finalEvidenceFailure("campaign_evidence_publication", publishErr)
			}
			publishedManifestHash = manifest.ContentHash
		}
		if options.Roles == nil {
			complete := map[string]any{"schema": "urnetwork-sim-complete-v1", "run_id": runID, "result_hash": result.EvidenceHash, "files": hashes}
			if result.LifecycleHandoff != nil {
				complete["lifecycle_handoff"] = result.LifecycleHandoff
			}
			if result.PriorRelease != nil {
				complete["prior_release"] = result.PriorRelease
			}
			if publishedBundlePayloadHash != "" {
				complete["bundle_payload_hash"] = publishedBundlePayloadHash
			}
			b, marshalErr := json.MarshalIndent(complete, "", "  ")
			if marshalErr != nil {
				return finalEvidenceFailure("complete_evidence_encoding", marshalErr)
			}
			if err := atomicWrite(filepath.Join(runDir, "complete.json"), append(b, '\n'), 0o644); err != nil {
				return finalEvidenceFailure("complete_evidence_write", err)
			}
		} else {
			owner, ok := options.Roles.EVM["testnet-owner"]
			if !ok {
				return finalEvidenceFailure("complete_evidence_signer", errors.New("testnet owner role is missing"))
			}
			completePayload := scenarioCompletePayload{ResultHash: result.EvidenceHash, Files: hashes, BundlePayloadHash: publishedBundlePayloadHash, EvidenceManifestHash: publishedManifestHash, LifecycleHandoff: result.LifecycleHandoff, PriorRelease: result.PriorRelease}
			complete, err := signEvidence(cfg, "scenario-complete", runID, completePayload, owner)
			if err != nil {
				return finalEvidenceFailure("complete_evidence_signature", err)
			}
			b, marshalErr := json.MarshalIndent(complete, "", "  ")
			if marshalErr != nil {
				return finalEvidenceFailure("complete_evidence_encoding", marshalErr)
			}
			b = append(b, '\n')
			if options.Publish {
				if !validSHA256ContentHash(publishedBundlePayloadHash) {
					return finalEvidenceFailure("complete_evidence_publication", errors.New("published scenario bundle has no commit payload hash"))
				}
				failureID, commitErr := commitPublishedScenarioCompletion(ctx, cfg, options.Roles, stateDir, runID, complete, b, nil)
				if commitErr != nil {
					return finalEvidenceFailure(failureID, commitErr)
				}
			} else if err := atomicWrite(filepath.Join(runDir, "complete.json"), b, 0o644); err != nil {
				return finalEvidenceFailure("complete_evidence_write", err)
			}
		}
	}
	if result.Result != "pass" {
		return result, fmt.Errorf("scenario %s failed; evidence %s", definition.Name, filepath.Join(runDir, "result.json"))
	}
	scenarioCompleted = true
	return result, nil
}

func produceFinalSemanticCampaignOutputs(ctx context.Context, cfg *ResolvedConfig, stateDir, runDir string, result *ScenarioResult, terminal *ScenarioObservation, history []*ScenarioObservation, options scenarioRunOptions) error {
	build := options.BuildFinalSemantic
	if build == nil {
		build = BuildFinalSemanticSourceFromCampaign
	}
	source, err := build(ctx, cfg, stateDir, runDir, result, terminal, history)
	if err != nil {
		return fmt.Errorf("build final semantic source: %w", err)
	}
	if source == nil {
		return errors.New("final semantic source builder returned nil")
	}
	load := options.FinalSemanticArtifacts
	if load == nil {
		load, err = NewFinalSemanticCampaignArtifactLoader(stateDir, runDir)
		if err != nil {
			return fmt.Errorf("construct final semantic artifact loader: %w", err)
		}
	}
	newReader := options.FinalSemanticReader
	if newReader == nil {
		newReader, err = publishedFinalSemanticReaderFactory(ctx, cfg, stateDir)
		if err != nil {
			return fmt.Errorf("construct final semantic public reader factory: %w", err)
		}
	}
	scan := NewFinalSemanticSecretScanner(options.Roles, cfg.WalletSecret, cfg.WalletMaterial, cfg.WalletPasswordSecret, cfg.WalletPassword)
	if _, err := ProduceFinalSemanticOutputs(ctx, runDir, *source, load, newReader, scan); err != nil {
		return err
	}
	return nil
}

func canonicalScenarioResultHash(result *ScenarioResult) (string, error) {
	copy := *result
	copy.EvidenceHash = ""
	copy.PublishedEvidence = nil
	return canonicalHashHex(copy)
}

func validateScenarioFinalSemanticSource(cfg *ResolvedConfig, roles *RoleSecrets, result *ScenarioResult, source *FinalSemanticEvidence) error {
	if cfg == nil || cfg.Config == nil || roles == nil || result == nil || result.AcceptanceWindow == nil || source == nil {
		return errors.New("final semantic campaign identity is incomplete")
	}
	owner, ok := roles.EVM["testnet-owner"]
	if !ok || !common.IsHexAddress(owner.Address) || common.HexToAddress(owner.Address) == (common.Address{}) {
		return errors.New("final semantic campaign owner is unavailable")
	}
	if source.Phase != result.Name || source.RunID != result.RunID || !strings.EqualFold(source.ResultHash, result.EvidenceHash) || source.DeploymentID != result.DeploymentID || source.DeploymentID != cfg.Config.Deployment.DeploymentID || source.ConfigHash != result.ConfigHash || source.ConfigHash != cfg.ConfigHash ||
		!strings.EqualFold(source.PolicyHash, result.PolicyHash) || !strings.EqualFold(source.PolicyHash, cfg.PolicyHash) || source.ChainID != result.ChainID || source.ChainID != cfg.ChainID ||
		source.Netuid != result.Netuid || source.Netuid != cfg.Netuid || !strings.EqualFold(source.GenesisHash, result.GenesisHash) || !strings.EqualFold(source.GenesisHash, cfg.Public.Chain.GenesisHash) ||
		source.EVMCampaignStartHead != result.CampaignStartHead || source.EVMTerminalHead != result.EndHead || source.Window != *result.AcceptanceWindow || !validCanonicalHashHex(source.PlanHash) ||
		source.ExpectedOperators != cfg.Config.Topology.Operators || source.ExpectedValidators != cfg.Config.Topology.Validators || source.ExpectedMiners != cfg.Config.Topology.Miners ||
		source.ExpectedCandidates != cfg.Config.Topology.HeadFleets+cfg.Config.Topology.ChallengerFleets || source.ExpectedHeadSlots != cfg.Config.Topology.HeadSlots ||
		!strings.EqualFold(source.Deployment.GovernanceOwner, owner.Address) {
		return errors.New("final semantic source does not bind the canonical scenario, configuration, topology, terminal checkpoint, and owner")
	}
	return nil
}

func refreshPublishedScenarioCandidate(result *ScenarioResult, completed time.Time, start, current *ScenarioObservation, expectedHash string, history ...*ScenarioObservation) error {
	attachScenarioAnomalyGate(result, completed, start, current, history...)
	hash, err := canonicalScenarioResultHash(result)
	if err != nil {
		return err
	}
	result.EvidenceHash = hash
	if !strings.EqualFold(hash, expectedHash) {
		return errors.New("process-log scan changed the signed scenario candidate")
	}
	return nil
}

func writeScenarioOutputs(cfg *ResolvedConfig, runDir string, result *ScenarioResult, observation *ScenarioObservation) error {
	if result.Anomalies == nil {
		return errors.New("scenario result has no anomaly ledger")
	}
	anomalies, err := json.MarshalIndent(result.Anomalies, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(runDir, "anomalies.json"), append(anomalies, '\n'), 0o644); err != nil {
		return err
	}
	assertions, err := json.MarshalIndent(assertionFile{Schema: "urnetwork-sim-assertions-v1", Assertions: result.Assertions}, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(runDir, "assertions.json"), append(assertions, '\n'), 0o644); err != nil {
		return err
	}
	analysis := analyzeScenarioObservation(cfg, observation)
	analysisBytes, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(runDir, "analysis.json"), append(analysisBytes, '\n'), 0o644); err != nil {
		return err
	}
	if err := writeAnalysisHTML(filepath.Join(runDir, "analysis.html"), analysis); err != nil {
		return err
	}
	if err := writeJUnit(filepath.Join(runDir, "junit.xml"), result.Name, result.Assertions); err != nil {
		return err
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(runDir, "result.json"), append(b, '\n'), 0o644)
}

type verifyKeyRotationOperator struct {
	NoID         int    `json:"no_id"`
	OldPublicKey string `json:"old_public_key"`
	NewPublicKey string `json:"new_public_key"`
	BeforePID    int    `json:"before_pid,omitempty"`
	AfterPID     int    `json:"after_pid"`
}

type verifyKeyRotationEvidence struct {
	Schema       string                      `json:"schema"`
	DeploymentID string                      `json:"deployment_id"`
	PolicyHash   string                      `json:"policy_hash"`
	Operators    []verifyKeyRotationOperator `json:"operators"`
}

func fetchVerifyPublicKeys(ctx context.Context, endpoint string) (map[byte]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(endpoint, "/")+"/verify/keys", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, (1*1024*1024)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 1*1024*1024 {
		return nil, errors.New("verify keys endpoint exceeded 1 MiB")
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("verify keys endpoint returned HTTP %d", resp.StatusCode)
	}
	var value struct {
		Keys []struct {
			ServerKeyID byte   `json:"server_key_id"`
			PublicKey   []byte `json:"public_key"`
		} `json:"keys"`
	}
	if json.Unmarshal(b, &value) != nil || len(value.Keys) == 0 {
		return nil, errors.New("verify keys endpoint returned an invalid response")
	}
	result := map[byte]string{}
	for _, key := range value.Keys {
		if len(key.PublicKey) != ed25519.PublicKeySize || result[key.ServerKeyID] != "" {
			return nil, errors.New("verify keys endpoint returned an invalid/duplicate key")
		}
		result[key.ServerKeyID] = "0x" + hex.EncodeToString(key.PublicKey)
	}
	return result, nil
}

func rotateOperatorVerifyKeys(ctx context.Context, cfg *ResolvedConfig, stateDir string, processLogs *processLogGate) error {
	if processLogs == nil {
		return errors.New("verify-key rotation requires the persisted process log gate")
	}
	driver := &liveScenarioFaultDriver{stateDir: stateDir, cfg: cfg}
	record := verifyKeyRotationEvidence{Schema: "urnetwork-verify-key-rotation-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, PolicyHash: cfg.PolicyHash}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		configBytes, expected, err := operatorVerifyConfig(cfg, operator, true)
		if err != nil {
			return err
		}
		path := filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator), "vault", "verify.yml")
		if err := atomicWrite(path, configBytes, 0o600); err != nil {
			return err
		}
		endpoint := fmt.Sprintf("http://127.0.0.1:%d", 18080+operator)
		observed, readErr := fetchVerifyPublicKeys(ctx, endpoint)
		states, _, stateErr := driver.processSnapshot()
		if stateErr != nil {
			return stateErr
		}
		processID := fmt.Sprintf("operator-%d-api", operator)
		beforePID := states[processID].PID
		if readErr != nil || !strings.EqualFold(observed[0], expected[0]) || !strings.EqualFold(observed[1], expected[1]) {
			fault := scenarioFaultSpec{ID: fmt.Sprintf("verify-key-rotation-%d", operator), Kind: "process-restart", Targets: []string{processID}, TriggerOffsetBlocks: 1, DurationBlocks: 1}
			if err := processLogs.RequireClean(false); err != nil {
				return err
			}
			scope := processLogFaultScope{ID: fault.ID, Kind: fault.Kind, Targets: append([]string(nil), fault.Targets...)}
			before, applyErr := driver.Apply(ctx, fault)
			var after []FaultProcessEvidence
			var restoreErr error
			if applyErr == nil {
				after, restoreErr = driver.Restore(ctx, fault)
			}
			scanResult, scanErr := processLogs.Scan(false, scope)
			if err := errors.Join(applyErr, restoreErr, scanErr, processLogFindingsError(scanResult.Findings)); err != nil {
				return err
			}
			if len(before) != 1 || len(after) != 1 || before[0].PID == after[0].PID {
				return fmt.Errorf("operator %d verify-key restart did not replace the API process", operator)
			}
			beforePID = before[0].PID
		}
		observed, err = fetchVerifyPublicKeys(ctx, endpoint)
		if err != nil || !strings.EqualFold(observed[0], expected[0]) || !strings.EqualFold(observed[1], expected[1]) {
			return stateMismatchError(err, "operator %d did not publish both verify keys after rotation", operator)
		}
		states, _, err = driver.processSnapshot()
		if err != nil || states[processID].PID <= 1 {
			return stateMismatchError(err, "operator %d API state unavailable after key rotation", operator)
		}
		record.Operators = append(record.Operators, verifyKeyRotationOperator{NoID: operator, OldPublicKey: expected[0], NewPublicKey: expected[1], BeforePID: beforePID, AfterPID: states[processID].PID})
	}
	return writePublicJSON(filepath.Join(stateDir, "public", "verify-key-rotation.json"), record)
}

// Execute the complete approved precompile battery. Keeping the selector in
// one place ensures the standalone conformance scenario and the release
// campaign exercise the identical plan actions.
func executePrecompileActions(ctx context.Context, executor *Executor) error {
	if executor == nil || executor.plan == nil {
		return errors.New("precompile conformance requires the approved deployment executor")
	}
	count := 0
	for _, action := range executor.plan.Actions {
		if !strings.HasPrefix(action.ID, "precompile.") {
			continue
		}
		if err := executor.Execute(ctx, action); err != nil {
			return err
		}
		count++
	}
	if count != 10 {
		return fmt.Errorf("approved plan has %d precompile actions, want exactly 10", count)
	}
	return nil
}

func RunScenario(ctx context.Context, cfg *ResolvedConfig, stateDir, name string, journal *Journal, executor *Executor) error {
	return runScenarioCampaignAttempt(ctx, cfg, stateDir, name, journal, executor, nil)
}

func runScenarioCampaignAttempt(ctx context.Context, cfg *ResolvedConfig, stateDir, name string, journal *Journal, executor *Executor, attempt *scenarioCampaignAttempt) error {
	// Validate the immutable definition before any live action. A malformed
	// release matrix must not be able to leave the coordinator paused/upgraded.
	definition, err := scenarioDefinitionFor(cfg, name)
	if err != nil {
		return err
	}
	processLogs, err := loadLiveProcessLogGate(stateDir)
	if err != nil {
		return fmt.Errorf("open live process log gate: %w", err)
	}
	roles, err := LoadOrWriteRoleSecrets(cfg, stateDir)
	if err != nil {
		return err
	}
	if name == "release-1.0" || name == "production-soak" {
		if executor == nil || executor.plan == nil || !validCanonicalHashHex(executor.plan.PlanHash) {
			return errors.New("release campaign attempt requires the approved setup plan")
		}
		if attempt == nil {
			loaded, loadErr := readScenarioCampaignAttempt(cfg, stateDir, roles, executor.plan.PlanHash, name)
			if loadErr == nil {
				attempt = loaded
			} else if !errors.Is(loadErr, os.ErrNotExist) {
				return fmt.Errorf("load scenario campaign attempt: %w", loadErr)
			} else {
				var prior *ReleaseCampaignGate
				if name == "production-soak" {
					prior, loadErr = loadReleaseCampaignGate(cfg, stateDir, roles)
					if loadErr != nil {
						return fmt.Errorf("load production release predecessor: %w", loadErr)
					}
				}
				attempt, loadErr = loadOrCreateScenarioCampaignAttempt(cfg, stateDir, roles, executor.plan.PlanHash, name, prior, time.Now().UTC())
				if loadErr != nil {
					return fmt.Errorf("create scenario campaign attempt: %w", loadErr)
				}
			}
		} else {
			loaded, loadErr := readScenarioCampaignAttempt(cfg, stateDir, roles, executor.plan.PlanHash, name)
			if loadErr != nil {
				return fmt.Errorf("authenticate supplied scenario campaign attempt: %w", loadErr)
			}
			if loaded.payload.RunID != attempt.payload.RunID || !releaseCampaignGatesEqual(loaded.payload.PriorRelease, attempt.payload.PriorRelease) {
				return errors.New("supplied scenario campaign attempt differs from its owner-signed durable record")
			}
			attempt = loaded
		}
	}
	runtimeCfg, err := campaignRPCConfig(cfg)
	if err != nil {
		return err
	}
	scenarioExecutor := executor
	if executor != nil {
		if journal == nil || executor.plan == nil {
			return errors.New("live scenario RPC handoff requires the approved plan and journal")
		}
		scenarioExecutor, runtimeCfg, err = NewCampaignExecutor(ctx, cfg, stateDir, executor.plan, journal, roles)
		if err != nil {
			return fmt.Errorf("open campaign executor through shared EVM egress: %w", err)
		}
		defer scenarioExecutor.Close()
		if err := scenarioExecutor.ensurePayloads(ctx); err != nil {
			return fmt.Errorf("load campaign deployment through shared EVM egress: %w", err)
		}
		if attempt != nil && attempt.payload.PriorRelease != nil {
			gate := *attempt.payload.PriorRelease
			scenarioExecutor.releaseGate = &gate
		}
	}
	var campaign adversaryCampaign
	if definition.AdversarialMatrixHash != "" {
		matrix, matrixErr := loadAdversarialMatrix(cfg.Repos.SN, cfg.Config.Scenarios.Adversaries.Matrix)
		if matrixErr != nil {
			return matrixErr
		}
		if matrix.Hash != definition.AdversarialMatrixHash {
			return errors.New("adversarial matrix changed after scenario definition validation")
		}
		actors, actorErr := newLiveAdversaryActors(runtimeCfg, stateDir, roles)
		if actorErr != nil {
			return actorErr
		}
		liveCampaign, campaignErr := newAdversaryCampaign(cfg.Config.Scenarios.Adversaries, matrix, actors)
		if campaignErr != nil {
			return campaignErr
		}
		campaign = liveCampaign
	}
	prepare := func(prepareCtx context.Context) error {
		if name == "precompile-conformance" {
			return executePrecompileActions(prepareCtx, scenarioExecutor)
		}
		if name == "release-1.0" {
			if scenarioExecutor == nil {
				return errors.New("release scenario requires the approved deployment executor")
			}
			precompile, readErr := loadPrecompileEvidence(stateDir)
			if readErr != nil || !precompileEvidenceComplete(precompile) {
				// Run conformance inside the release campaign so continuous
				// adversaries cover the actual precompile happy path. A prior
				// standalone run is adopted only after the same postconditions are
				// revalidated by Executor.Execute.
				if err := executePrecompileActions(prepareCtx, scenarioExecutor); err != nil {
					return fmt.Errorf("release precompile conformance: %w", err)
				}
				precompile, readErr = loadPrecompileEvidence(stateDir)
				if readErr != nil {
					return fmt.Errorf("release precompile evidence: %w", readErr)
				}
			}
			if scenarioExecutor.payloads == nil {
				return errors.New("release scenario requires installed deployment payloads")
			}
			if identityErr := validatePrecompileEvidenceIdentity(cfg, &scenarioExecutor.payloads.Manifest, precompile); identityErr != nil {
				return fmt.Errorf("release scenario precompile evidence identity: %w", identityErr)
			}
			if !precompileEvidenceComplete(precompile) {
				return errors.New("release scenario precompile-conformance gate is incomplete")
			}
			waitBlocks := cfg.Policy.Settlement.EpochBlocks + cfg.Policy.Settlement.FinalizeOffsetBlocks + 20
			if err := waitForGovernanceDrillReady(prepareCtx, scenarioExecutor, time.Duration(waitBlocks*cfg.Public.Chain.ExpectedBlockSeconds)*time.Second); err != nil {
				return err
			}
			for _, action := range scenarioExecutor.plan.Actions {
				if strings.HasPrefix(action.ID, "governance.") {
					if err := scenarioExecutor.Execute(prepareCtx, action); err != nil {
						return err
					}
				}
			}
			// Install takeover bindings last. Their future-effective epoch must be
			// the first complete accepted epoch; a long governance drill must not
			// consume the generation-3 boundary before M2 starts.
			for _, id := range fleetLifecycleActionIDs("prepare", cfg.Config.Topology.ClientsPerHeadFleet) {
				action, actionErr := scenarioExecutor.planAction(id)
				if actionErr != nil {
					return actionErr
				}
				if actionErr := scenarioExecutor.Execute(prepareCtx, action); actionErr != nil {
					return fmt.Errorf("prepare fleet lifecycle action %s: %w", id, actionErr)
				}
			}
		}
		if name == "production-soak" {
			if scenarioExecutor == nil {
				return errors.New("production soak requires the approved deployment executor")
			}
			for _, action := range scenarioExecutor.plan.Actions {
				if isProductionTransitionAction(action) {
					if err := scenarioExecutor.Execute(prepareCtx, action); err != nil {
						return err
					}
				}
			}
			if err := rotateOperatorVerifyKeys(prepareCtx, runtimeCfg, stateDir, processLogs); err != nil {
				return fmt.Errorf("rotate operator verify keys: %w", err)
			}
			if err := runDishonestDepositPhase(prepareCtx, runtimeCfg, stateDir, scenarioExecutor); err != nil {
				return fmt.Errorf("live dishonest operator deposit: %w", err)
			}
		}
		return nil
	}
	probe := &liveScenarioProbe{cfg: runtimeCfg, stateDir: stateDir, client: &http.Client{Timeout: 30 * time.Second}}
	var fleetLifecycle scenarioFleetLifecycle
	if name == "release-1.0" || name == "production-soak" {
		if scenarioExecutor == nil {
			return errors.New("release scenario requires an approved executor for the live fleet lifecycle")
		}
		fleetLifecycle = &liveFleetLifecycle{cfg: runtimeCfg, stateDir: stateDir, executor: scenarioExecutor}
	}
	faultDriver := &liveScenarioFaultDriver{stateDir: stateDir, cfg: cfg}
	if scenarioExecutor != nil && scenarioExecutor.plan != nil && scenarioExecutor.payloads != nil {
		faultDriver.planHash = scenarioExecutor.plan.PlanHash
		faultDriver.coordinator = strings.ToLower(scenarioExecutor.payloads.Manifest.CoordinatorProxy.Hex())
	}
	_, err = runScenarioWithProbe(ctx, cfg, stateDir, definition, probe, scenarioRunOptions{
		Roles: roles, Publish: true, FaultDriver: faultDriver,
		Adversaries: campaign, Prepare: prepare, ProcessLogs: processLogs, FleetLifecycle: fleetLifecycle, Attempt: attempt,
	})
	return err
}

// Select every approved action that moves the accelerated test campaign into
// production cadence. New production hyperparameters must not be silently
// omitted by an allowlist that predates them.
func isProductionTransitionAction(action Action) bool {
	return action.ID == "production.schedule-policy" || strings.HasPrefix(action.ID, "production.hyperparameter.")
}
