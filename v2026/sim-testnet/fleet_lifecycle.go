package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

type FleetLifecycleCleanupEvidence struct {
	Schema            string    `json:"schema"`
	DeploymentID      string    `json:"deployment_id"`
	PlanHash          string    `json:"plan_hash"`
	ActionID          string    `json:"action_id"`
	IntentHash        string    `json:"intent_hash"`
	ClientID          string    `json:"client_id"`
	FleetID           string    `json:"fleet_id"`
	Generation        uint64    `json:"generation"`
	CleanedAtEpoch    uint64    `json:"cleaned_at_epoch"`
	MemberCountBefore uint64    `json:"member_count_before"`
	MemberCountAfter  uint64    `json:"member_count_after"`
	TransactionHash   string    `json:"transaction_hash"`
	BeforeBlock       ChainHead `json:"before_block"`
	BlockNumber       uint64    `json:"block_number"`
	BlockHash         string    `json:"block_hash"`
}

type FleetLifecycleMirrorEvidence struct {
	Schema             string `json:"schema"`
	DeploymentID       string `json:"deployment_id"`
	PlanHash           string `json:"plan_hash"`
	ActionID           string `json:"action_id"`
	IntentHash         string `json:"intent_hash"`
	Hotkey             string `json:"hotkey"`
	CommitmentHash     string `json:"commitment_hash"`
	FinalizedBlock     uint64 `json:"finalized_block"`
	FinalizedBlockHash string `json:"finalized_block_hash"`
	TransactionHash    string `json:"transaction_hash"`
	BlockNumber        uint64 `json:"block_number"`
	BlockHash          string `json:"block_hash"`
}

type FleetLifecycleRegistrationEvidence struct {
	Schema             string                      `json:"schema"`
	DeploymentID       string                      `json:"deployment_id"`
	PlanHash           string                      `json:"plan_hash"`
	ActionID           string                      `json:"action_id"`
	IntentHash         string                      `json:"intent_hash"`
	VictimFleet        int                         `json:"victim_fleet"`
	VictimRole         string                      `json:"victim_role"`
	VictimUID          uint16                      `json:"victim_uid"`
	VictimHotkey       string                      `json:"victim_hotkey"`
	VictimColdkey      string                      `json:"victim_coldkey"`
	ReplacementHotkey  string                      `json:"replacement_hotkey"`
	ReplacementColdkey string                      `json:"replacement_coldkey"`
	PrePrune           FleetLifecyclePruneSnapshot `json:"pre_prune"`
	PostRegistration   FleetLifecyclePruneSnapshot `json:"post_registration"`
	TransactionHash    string                      `json:"transaction_hash"`
	BlockNumber        uint64                      `json:"block_number"`
	BlockHash          string                      `json:"block_hash"`
}

type FleetLifecycleValidatorCensus struct {
	ValidatorID             int                       `json:"validator_id"`
	SettlementEpoch         uint64                    `json:"settlement_epoch"`
	SubnetEpoch             uint64                    `json:"subnet_epoch"`
	NativeSnapshot          ChainHead                 `json:"native_snapshot"`
	EVMSnapshot             ChainHead                 `json:"evm_snapshot"`
	MeasurementArtifactHash string                    `json:"measurement_artifact_hash"`
	VectorHash              string                    `json:"vector_hash"`
	ExtrinsicHash           string                    `json:"extrinsic_hash"`
	Commit                  ChainHead                 `json:"commit"`
	RevealBlock             uint64                    `json:"reveal_block"`
	RevealBlockHash         string                    `json:"reveal_block_hash"`
	Application             ChainHead                 `json:"application"`
	EligibleUIDs            []uint16                  `json:"eligible_uids"`
	SelectedUIDs            []uint16                  `json:"selected_uids"`
	RejectedUIDs            []uint16                  `json:"rejected_uids"`
	AppliedWeights          []IntentWeightObservation `json:"applied_weights"`
}

type FleetLifecycleCandidateCensus struct {
	Phase              string                          `json:"phase"`
	PostAcceptance     bool                            `json:"post_acceptance,omitempty"`
	Milestone          string                          `json:"milestone,omitempty"`
	ObservationHash    string                          `json:"observation_hash"`
	ObservedHead       ChainHead                       `json:"observed_evm_head"`
	NativeObservedHead ChainHead                       `json:"observed_native_head"`
	CandidateUIDs      []uint16                        `json:"candidate_uids"`
	CandidateHotkeys   []string                        `json:"candidate_hotkeys"`
	Validators         []FleetLifecycleValidatorCensus `json:"validators"`
}

type FleetLifecyclePayoutEvidence struct {
	Epoch       uint64   `json:"epoch"`
	NoID        int      `json:"no_id"`
	ContentHash string   `json:"content_hash"`
	PayoutRoot  string   `json:"payout_root"`
	ClientIDs   []string `json:"client_ids"`
	Disposition string   `json:"disposition"`
}

// FleetLifecycleNativeSchedule binds the post-acceptance evidence tail to one
// finalized runtime-453 CRv4 schedule. The acceptance window is never enlarged;
// only the lifecycle proof may wait through this exact application deadline.
type FleetLifecycleNativeSchedule struct {
	Phase                      string    `json:"phase"`
	ObservedHead               ChainHead `json:"observed_head"`
	LastEpochBlock             uint64    `json:"last_epoch_block"`
	PendingEpochAt             uint64    `json:"pending_epoch_at"`
	SubnetEpoch                uint64    `json:"subnet_epoch"`
	Tempo                      uint16    `json:"tempo"`
	BlocksSinceLastStep        uint64    `json:"blocks_since_last_step"`
	RevealPeriodEpochs         uint64    `json:"reveal_period_epochs"`
	RequiredMilestones         uint64    `json:"required_milestones"`
	RequiredMutations          uint64    `json:"required_mutations"`
	ApplicationSafetyBlocks    uint64    `json:"application_safety_blocks"`
	FirstQualifyingRevealBlock uint64    `json:"first_qualifying_reveal_block"`
	ApplicationDeadlineBlock   uint64    `json:"application_deadline_block"`
}

// FleetLifecycleEvidence is the resumable public state machine and final
// on-chain replay index for the M2 prune/fallback/re-registration exercise.
type FleetLifecycleEvidence struct {
	Schema       string `json:"schema"`
	DeploymentID string `json:"deployment_id"`
	PlanHash     string `json:"plan_hash"`
	// RunID is the authenticated release-1.0 run which created the
	// lifecycle. ProductionRunID names the only production-soak run allowed
	// to resume it. Keeping both identities prevents a phase-local success
	// marker from being mistaken for composite lifecycle completion.
	RunID              string `json:"release_run_id"`
	ProductionRunID    string `json:"production_run_id,omitempty"`
	ReleaseHandoffHash string `json:"release_handoff_hash,omitempty"`
	Stage              string `json:"stage"`
	// FirstAcceptedEpoch is the EVM settlement epoch at AcceptanceStartBlock.
	// Its explicit wire name prevents this counter from being confused with a
	// validator's independent native SubnetEpoch.
	FirstAcceptedEpoch                 uint64                              `json:"first_settlement_epoch,omitempty"`
	AcceptanceStartBlock               uint64                              `json:"acceptance_start_block,omitempty"`
	AcceptanceEndBlock                 uint64                              `json:"acceptance_end_block,omitempty"`
	AcceptanceTerminalBlock            uint64                              `json:"acceptance_terminal_block,omitempty"`
	ReleaseHandoffSchedule             *FleetLifecycleNativeSchedule       `json:"release_handoff_schedule,omitempty"`
	ReleaseEVMEvidenceDeadlineBlock    uint64                              `json:"release_evm_evidence_deadline_block,omitempty"`
	ProductionFirstSettlementEpoch     uint64                              `json:"production_first_settlement_epoch,omitempty"`
	ProductionAcceptanceStartBlock     uint64                              `json:"production_acceptance_start_block,omitempty"`
	ProductionAcceptanceEndBlock       uint64                              `json:"production_acceptance_end_block,omitempty"`
	ProductionAcceptanceTerminalBlock  uint64                              `json:"production_acceptance_terminal_block,omitempty"`
	ProductionNativeSchedule           *FleetLifecycleNativeSchedule       `json:"production_native_schedule,omitempty"`
	ProductionEVMEvidenceDeadlineBlock uint64                              `json:"production_evm_evidence_deadline_block,omitempty"`
	TakeoverEffectiveEpoch             uint64                              `json:"takeover_effective_epoch,omitempty"`
	FallbackEffectiveEpoch             uint64                              `json:"fallback_effective_epoch,omitempty"`
	ProviderEffectiveEpoch             uint64                              `json:"provider_effective_epoch,omitempty"`
	TerminalEffectiveEpoch             uint64                              `json:"terminal_effective_epoch,omitempty"`
	PostRegistrationRewardBaseline     ChainHead                           `json:"post_registration_reward_baseline,omitempty"`
	LaunchPrune                        *FleetLifecyclePruneSnapshot        `json:"launch_prune,omitempty"`
	FallbackRegistration               *FleetLifecycleRegistrationEvidence `json:"fallback_registration,omitempty"`
	ProviderRegistration               *FleetLifecycleRegistrationEvidence `json:"provider_registration,omitempty"`
	TerminalRegistration               *FleetLifecycleRegistrationEvidence `json:"terminal_registration,omitempty"`
	TargetCleanup                      []FleetLifecycleCleanupEvidence     `json:"target_cleanup,omitempty"`
	CompanionCleanup                   []FleetLifecycleCleanupEvidence     `json:"companion_cleanup,omitempty"`
	FallbackCleanup                    []FleetLifecycleCleanupEvidence     `json:"fallback_cleanup,omitempty"`
	Payouts                            []FleetLifecyclePayoutEvidence      `json:"payouts,omitempty"`
	CandidateCensuses                  []FleetLifecycleCandidateCensus     `json:"candidate_censuses,omitempty"`
}

const (
	fleetLifecycleStageAwaitingDemotion  = "awaiting-demotion"
	fleetLifecycleStageFallbackInstalled = "fallback-installed"
	fleetLifecycleStageFallbackPaid      = "fallback-paid"
	fleetLifecycleStageProviderInstalled = "provider-installed"
	fleetLifecycleStageProviderPaid      = "provider-paid"
	fleetLifecycleStageTerminalInstalled = "terminal-installed"
	fleetLifecycleStageReleaseHandoff    = "release-handoff"
	fleetLifecycleStageComplete          = "complete"
)

const (
	fleetLifecycleMilestoneTakeoverRejected = "takeover-rejected"
	fleetLifecycleMilestoneFallbackActive   = "fallback-active"
	fleetLifecycleMilestoneProviderActive   = "provider-active"
	fleetLifecycleMilestoneTerminalActive   = "terminal-active"
)

type fleetLifecycleRegistrationPreparation struct {
	Schema       string                      `json:"schema"`
	PlanHash     string                      `json:"plan_hash"`
	ActionID     string                      `json:"action_id"`
	IntentHash   string                      `json:"intent_hash"`
	VictimFleet  int                         `json:"victim_fleet"`
	VictimHotkey string                      `json:"victim_hotkey"`
	Snapshot     FleetLifecyclePruneSnapshot `json:"snapshot"`
}

const (
	fleetLifecycleTargetFleet          = 5
	fleetLifecycleCompanionFleet       = 6
	fleetLifecycleTargetChurn          = 6
	fleetLifecycleCompanionChurn       = 7
	fleetLifecycleFallbackChurn        = 1
	fleetLifecycleTerminalVictimChurn  = 8
	fleetLifecycleTargetExpectedUID    = 7
	fleetLifecycleCompanionExpectedUID = 8
	fleetLifecycleTerminalVictimUID    = 9
	// Generations one and two are the installed and refreshed ordinary fleet.
	// Generation three moves the same clients to the exact live prune targets;
	// generation four restores each provider after its runtime replacement.
	fleetLifecycleTakeoverGeneration = 3
	fleetLifecycleGeneration         = 4
)

const fleetLifecycleEvidenceSchema = "urnetwork-sim-fleet-lifecycle-v2"

const (
	fleetLifecycleFallbackManifestName    = "fleet-lifecycle-fallback.json"
	fleetLifecycleFallbackCommitmentName  = "fleet-lifecycle-fallback.commitment.json"
	fleetLifecycleTargetManifestName      = "fleet-5.lifecycle-generation-3.json"
	fleetLifecycleTargetCommitmentName    = "fleet-5.lifecycle-generation-3.commitment.json"
	fleetLifecycleCompanionManifestName   = "fleet-6.lifecycle-generation-3.json"
	fleetLifecycleCompanionCommitmentName = "fleet-6.lifecycle-generation-3.commitment.json"
	fleetLifecycleProviderManifestName    = "fleet-5.lifecycle-generation-4.json"
	fleetLifecycleProviderCommitmentName  = "fleet-5.lifecycle-generation-4.commitment.json"
	fleetLifecycleTerminalManifestName    = "fleet-6.lifecycle-generation-4.json"
	fleetLifecycleTerminalCommitmentName  = "fleet-6.lifecycle-generation-4.commitment.json"
)

// FleetLifecyclePruneInput is one complete runtime-453 auction row at an exact
// finalized state root. Public verification can independently recompute both
// immunity partitions and all three tie breakers from these rows.
type FleetLifecyclePruneInput struct {
	UID               uint16 `json:"uid"`
	Hotkey            string `json:"hotkey"`
	Coldkey           string `json:"coldkey"`
	EmissionRao       uint64 `json:"emission_rao"`
	RegistrationBlock uint64 `json:"registration_block"`
	Immune            bool   `json:"immune"`
	Immortal          bool   `json:"immortal"`
}

// FleetLifecyclePruneSnapshot authenticates every input used by the pinned
// runtime's registration replacement decision at one finalized native head.
type FleetLifecyclePruneSnapshot struct {
	Head                 ChainHead                  `json:"head"`
	UIDCount             uint16                     `json:"uid_count"`
	MaximumUIDs          uint16                     `json:"maximum_uids"`
	ImmunityPeriodBlocks uint16                     `json:"immunity_period_blocks"`
	MinimumNonImmuneUIDs uint16                     `json:"minimum_non_immune_uids"`
	NonImmuneUIDs        uint16                     `json:"non_immune_uids"`
	RuntimePruneUID      uint16                     `json:"runtime_prune_uid"`
	Inputs               []FleetLifecyclePruneInput `json:"inputs"`
}

// fleetProviderHotkeyLabel returns the identity whose live UID validators use
// for a logical provider fleet. The two M2 targets deliberately use the exact
// oldest surviving churn registrations authenticated by the launch snapshot.
func fleetProviderHotkeyLabel(fleet int) string {
	switch fleet {
	case fleetLifecycleTargetFleet:
		return churnHotkeyLabel(fleetLifecycleTargetChurn)
	case fleetLifecycleCompanionFleet:
		return churnHotkeyLabel(fleetLifecycleCompanionChurn)
	default:
		return fleetHotkeyLabel(fleet)
	}
}

// fleetProviderColdkeyLabel mirrors fleetProviderHotkeyLabel's ownership.
func fleetProviderColdkeyLabel(fleet int) string {
	switch fleet {
	case fleetLifecycleTargetFleet:
		return churnColdkeyLabel(fleetLifecycleTargetChurn)
	case fleetLifecycleCompanionFleet:
		return churnColdkeyLabel(fleetLifecycleCompanionChurn)
	default:
		return fleetColdkeyLabel(fleet)
	}
}

// fleetLifecycleFundingRole is the single role map used by both execution and
// postcondition verification. Registration funding belongs to the coldkey
// which signs and pays runtime 453; commitment funding belongs to the hotkey
// which publishes the manifest. Keeping this separate from ordinary fleet
// funding prevents fleet 5/6 setup actions from being silently redirected to
// their temporary lifecycle identities.
func fleetLifecycleFundingRole(actionID string) (string, bool) {
	switch actionID {
	case "lifecycle.prepare.target.fund-hotkey":
		return churnHotkeyLabel(fleetLifecycleTargetChurn), true
	case "lifecycle.prepare.companion.fund-hotkey":
		return churnHotkeyLabel(fleetLifecycleCompanionChurn), true
	case "lifecycle.fallback.fund":
		return churnColdkeyLabel(fleetLifecycleFallbackChurn), true
	case "lifecycle.fallback.fund-hotkey":
		return churnHotkeyLabel(fleetLifecycleFallbackChurn), true
	case "lifecycle.provider.fund":
		return fleetProviderColdkeyLabel(fleetLifecycleTargetFleet), true
	case "lifecycle.provider.fund-hotkey":
		return fleetProviderHotkeyLabel(fleetLifecycleTargetFleet), true
	case "lifecycle.terminal.fund":
		return churnColdkeyLabel(fleetLifecycleCompanionChurn), true
	case "lifecycle.terminal.fund-hotkey":
		return churnHotkeyLabel(fleetLifecycleCompanionChurn), true
	default:
		return "", false
	}
}

func validateFleetLifecycleTopology(topology TopologyConfig) error {
	if topology.HeadFleets < fleetLifecycleCompanionFleet {
		return fmt.Errorf("fleet lifecycle requires at least %d head fleets", fleetLifecycleCompanionFleet)
	}
	if topology.ChurnFloorUIDs < fleetLifecycleFallbackChurn {
		return fmt.Errorf("fleet lifecycle requires at least %d churn-floor identities", fleetLifecycleFallbackChurn)
	}
	if topology.ChallengerFleets != 2 || topology.HeadSlots != topology.HeadFleets || topology.fleetCandidates() != 202 {
		return fmt.Errorf("fleet lifecycle requires the exact 200+2 candidate topology")
	}
	return nil
}

func fleetLifecycleFallbackMinerIndex(cfg *ResolvedConfig, member int) (int, error) {
	if cfg == nil || cfg.Config == nil || member < 1 || member > cfg.Config.Topology.ClientsPerHeadFleet {
		return 0, fmt.Errorf("fleet lifecycle fallback member %d is out of range", member)
	}
	miner := cfg.Config.Topology.fleetCandidateMiners() + 1 + (member-1)*cfg.Config.Topology.Operators
	if miner > cfg.Config.Topology.Miners {
		return 0, fmt.Errorf("fleet lifecycle fallback miner %d exceeds topology", miner)
	}
	return miner, nil
}

func loadFleetLifecycleEvidence(stateDir string) (*FleetLifecycleEvidence, error) {
	var evidence FleetLifecycleEvidence
	if err := readJSONFile(filepath.Join(stateDir, "public", "fleet-lifecycle.json"), &evidence); err != nil {
		return nil, err
	}
	if evidence.Schema != fleetLifecycleEvidenceSchema || evidence.DeploymentID == "" || evidence.PlanHash == "" || evidence.RunID == "" || evidence.Stage == "" {
		return nil, errors.New("fleet lifecycle evidence is incomplete")
	}
	return &evidence, nil
}

func fleetLifecycleCandidateMinerSet(cfg *ResolvedConfig, evidence *FleetLifecycleEvidence, epoch uint64) map[int]bool {
	result := make(map[int]bool, cfg.Config.Topology.fleetCandidateMiners())
	for miner := 1; miner <= cfg.Config.Topology.fleetCandidateMiners(); miner++ {
		result[miner] = true
	}
	if evidence == nil || evidence.FallbackEffectiveEpoch == 0 || epoch < evidence.FallbackEffectiveEpoch {
		return result
	}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		delete(result, fleetMemberMinerIndex(cfg, fleetLifecycleTargetFleet, member))
		fallback, err := fleetLifecycleFallbackMinerIndex(cfg, member)
		if err == nil {
			result[fallback] = true
		}
	}
	if evidence.ProviderEffectiveEpoch == 0 || epoch < evidence.ProviderEffectiveEpoch {
		return result
	}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		result[fleetMemberMinerIndex(cfg, fleetLifecycleTargetFleet, member)] = true
		delete(result, fleetMemberMinerIndex(cfg, fleetLifecycleCompanionFleet, member))
	}
	if evidence.TerminalEffectiveEpoch == 0 || epoch < evidence.TerminalEffectiveEpoch {
		return result
	}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		result[fleetMemberMinerIndex(cfg, fleetLifecycleCompanionFleet, member)] = true
		fallback, err := fleetLifecycleFallbackMinerIndex(cfg, member)
		if err == nil {
			delete(result, fallback)
		}
	}
	return result
}

type fleetLifecycleEvidenceDescriptor struct {
	ManifestName   string
	CommitmentName string
	BindingNames   []string
	MinerIDs       []int
}

const (
	fleetLifecycleVariantTargetTakeover    = "target-takeover"
	fleetLifecycleVariantCompanionTakeover = "companion-takeover"
	fleetLifecycleVariantFallback          = "fallback"
	fleetLifecycleVariantProvider          = "provider"
	fleetLifecycleVariantTerminal          = "terminal-companion"
)

type fleetLifecycleVariant struct {
	Name           string
	Fleet          int
	HotkeyLabel    string
	Generation     uint64
	ManifestName   string
	CommitmentName string
	BindingName    func(int) string
	Fallback       bool
}

func fleetLifecycleVariantFor(name string) (fleetLifecycleVariant, error) {
	switch name {
	case fleetLifecycleVariantTargetTakeover:
		return fleetLifecycleVariant{Name: name, Fleet: fleetLifecycleTargetFleet, HotkeyLabel: churnHotkeyLabel(fleetLifecycleTargetChurn), Generation: fleetLifecycleTakeoverGeneration, ManifestName: fleetLifecycleTargetManifestName, CommitmentName: fleetLifecycleTargetCommitmentName, BindingName: func(member int) string {
			return fmt.Sprintf("fleet-5-member-%d.lifecycle-generation-3.binding.json", member)
		}}, nil
	case fleetLifecycleVariantCompanionTakeover:
		return fleetLifecycleVariant{Name: name, Fleet: fleetLifecycleCompanionFleet, HotkeyLabel: churnHotkeyLabel(fleetLifecycleCompanionChurn), Generation: fleetLifecycleTakeoverGeneration, ManifestName: fleetLifecycleCompanionManifestName, CommitmentName: fleetLifecycleCompanionCommitmentName, BindingName: func(member int) string {
			return fmt.Sprintf("fleet-6-member-%d.lifecycle-generation-3.binding.json", member)
		}}, nil
	case fleetLifecycleVariantFallback:
		return fleetLifecycleVariant{Name: name, Fleet: fleetLifecycleTargetFleet, HotkeyLabel: churnHotkeyLabel(fleetLifecycleFallbackChurn), Generation: 1, ManifestName: fleetLifecycleFallbackManifestName, CommitmentName: fleetLifecycleFallbackCommitmentName, BindingName: func(member int) string {
			return fmt.Sprintf("fleet-lifecycle-fallback-member-%d.binding.json", member)
		}, Fallback: true}, nil
	case fleetLifecycleVariantProvider:
		return fleetLifecycleVariant{Name: name, Fleet: fleetLifecycleTargetFleet, HotkeyLabel: churnHotkeyLabel(fleetLifecycleTargetChurn), Generation: fleetLifecycleGeneration, ManifestName: fleetLifecycleProviderManifestName, CommitmentName: fleetLifecycleProviderCommitmentName, BindingName: func(member int) string {
			return fmt.Sprintf("fleet-5-member-%d.lifecycle-generation-4.binding.json", member)
		}}, nil
	case fleetLifecycleVariantTerminal:
		return fleetLifecycleVariant{Name: name, Fleet: fleetLifecycleCompanionFleet, HotkeyLabel: churnHotkeyLabel(fleetLifecycleCompanionChurn), Generation: fleetLifecycleGeneration, ManifestName: fleetLifecycleTerminalManifestName, CommitmentName: fleetLifecycleTerminalCommitmentName, BindingName: func(member int) string {
			return fmt.Sprintf("fleet-6-member-%d.lifecycle-generation-4.binding.json", member)
		}}, nil
	default:
		return fleetLifecycleVariant{}, fmt.Errorf("unknown fleet lifecycle variant %q", name)
	}
}

// Bind each persisted mutation artifact to the one plan action that can create
// it. Keeping this mapping centralized prevents prefix-based cross-variant
// resume from accepting a structurally valid artifact from another wave.
func fleetLifecycleCommitmentActionID(variantName string) (string, error) {
	switch variantName {
	case fleetLifecycleVariantTargetTakeover:
		return "lifecycle.prepare.target.commitment", nil
	case fleetLifecycleVariantCompanionTakeover:
		return "lifecycle.prepare.companion.commitment", nil
	case fleetLifecycleVariantFallback:
		return "lifecycle.fallback.commitment", nil
	case fleetLifecycleVariantProvider:
		return "lifecycle.provider.commitment", nil
	case fleetLifecycleVariantTerminal:
		return "lifecycle.terminal.commitment", nil
	default:
		return "", fmt.Errorf("unknown fleet lifecycle commitment variant %q", variantName)
	}
}

func fleetLifecycleBindingActionID(variantName string, member int) (string, error) {
	if member < 1 {
		return "", fmt.Errorf("fleet lifecycle binding member %d is invalid", member)
	}
	switch variantName {
	case fleetLifecycleVariantTargetTakeover:
		return fmt.Sprintf("lifecycle.prepare.target.bind.%d", member), nil
	case fleetLifecycleVariantCompanionTakeover:
		return fmt.Sprintf("lifecycle.prepare.companion.bind.%d", member), nil
	case fleetLifecycleVariantFallback:
		return fmt.Sprintf("lifecycle.fallback.bind.%d", member), nil
	case fleetLifecycleVariantProvider:
		return fmt.Sprintf("lifecycle.provider.bind.%d", member), nil
	case fleetLifecycleVariantTerminal:
		return fmt.Sprintf("lifecycle.terminal.bind.%d", member), nil
	default:
		return "", fmt.Errorf("unknown fleet lifecycle binding variant %q", variantName)
	}
}

func fleetLifecycleMirrorActionID(variantName string) (string, error) {
	switch variantName {
	case fleetLifecycleVariantTargetTakeover:
		return "lifecycle.prepare.target.mirror", nil
	case fleetLifecycleVariantCompanionTakeover:
		return "lifecycle.prepare.companion.mirror", nil
	case fleetLifecycleVariantFallback:
		return "lifecycle.fallback.mirror", nil
	case fleetLifecycleVariantProvider:
		return "lifecycle.provider.mirror", nil
	case fleetLifecycleVariantTerminal:
		return "lifecycle.terminal.mirror", nil
	default:
		return "", fmt.Errorf("unknown fleet lifecycle mirror variant %q", variantName)
	}
}

func fleetLifecycleRegistrationActionIDFor(variantName string) (string, error) {
	switch variantName {
	case fleetLifecycleVariantFallback:
		return "lifecycle.fallback.register", nil
	case fleetLifecycleVariantProvider:
		return "lifecycle.provider.register", nil
	case fleetLifecycleVariantTerminal:
		return "lifecycle.terminal.register", nil
	default:
		return "", fmt.Errorf("unknown fleet lifecycle registration variant %q", variantName)
	}
}

func fleetLifecycleCleanupActionID(variantName string, member int) (string, error) {
	if member < 1 {
		return "", fmt.Errorf("fleet lifecycle cleanup member %d is invalid", member)
	}
	switch variantName {
	case fleetLifecycleVariantTargetTakeover:
		return fmt.Sprintf("lifecycle.provider.cleanup.%d", member), nil
	case fleetLifecycleVariantCompanionTakeover:
		return fmt.Sprintf("lifecycle.terminal.cleanup-companion.%d", member), nil
	case fleetLifecycleVariantFallback:
		return fmt.Sprintf("lifecycle.terminal.cleanup-fallback.%d", member), nil
	default:
		return "", fmt.Errorf("unknown fleet lifecycle cleanup variant %q", variantName)
	}
}

func fleetLifecycleVariantManifest(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, variant fleetLifecycleVariant) (protocol.FleetManifest, []byte, [32]byte, error) {
	miners := make([]int, 0, cfg.Config.Topology.ClientsPerHeadFleet)
	if variant.Fallback {
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			miner, err := fleetLifecycleFallbackMinerIndex(cfg, member)
			if err != nil {
				return protocol.FleetManifest{}, nil, [32]byte{}, err
			}
			miners = append(miners, miner)
		}
		return fleetManifestForMembers(cfg, stateDir, roles, derive32(cfg, "fleet-lifecycle/fallback-id"), variant.HotkeyLabel, variant.Generation, miners)
	}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		miners = append(miners, fleetMemberMinerIndex(cfg, variant.Fleet, member))
	}
	return fleetManifestForMembers(cfg, stateDir, roles, derive32(cfg, fmt.Sprintf("fleet-id/%d", variant.Fleet)), variant.HotkeyLabel, variant.Generation, miners)
}

func fleetLifecycleVariantDescriptor(cfg *ResolvedConfig, name string) (fleetLifecycleEvidenceDescriptor, error) {
	variant, err := fleetLifecycleVariantFor(name)
	if err != nil {
		return fleetLifecycleEvidenceDescriptor{}, err
	}
	descriptor := fleetLifecycleEvidenceDescriptor{ManifestName: variant.ManifestName, CommitmentName: variant.CommitmentName}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		miner := fleetMemberMinerIndex(cfg, variant.Fleet, member)
		if variant.Fallback {
			miner, err = fleetLifecycleFallbackMinerIndex(cfg, member)
			if err != nil {
				return fleetLifecycleEvidenceDescriptor{}, err
			}
		}
		descriptor.BindingNames = append(descriptor.BindingNames, variant.BindingName(member))
		descriptor.MinerIDs = append(descriptor.MinerIDs, miner)
	}
	return descriptor, nil
}

func standardFleetEvidenceDescriptor(cfg *ResolvedConfig, fleet int) fleetLifecycleEvidenceDescriptor {
	descriptor := fleetLifecycleEvidenceDescriptor{
		ManifestName: fmt.Sprintf("fleet-%d.json", fleet), CommitmentName: fmt.Sprintf("fleet-%d.commitment.json", fleet),
		BindingNames: make([]string, 0, cfg.Config.Topology.ClientsPerHeadFleet), MinerIDs: make([]int, 0, cfg.Config.Topology.ClientsPerHeadFleet),
	}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		descriptor.BindingNames = append(descriptor.BindingNames, fmt.Sprintf("fleet-%d-member-%d.binding.json", fleet, member))
		descriptor.MinerIDs = append(descriptor.MinerIDs, fleetMemberMinerIndex(cfg, fleet, member))
	}
	return descriptor
}

func fallbackFleetEvidenceDescriptor(cfg *ResolvedConfig) (fleetLifecycleEvidenceDescriptor, error) {
	return fleetLifecycleVariantDescriptor(cfg, fleetLifecycleVariantFallback)
}

func providerFleetEvidenceDescriptor(cfg *ResolvedConfig) fleetLifecycleEvidenceDescriptor {
	descriptor, _ := fleetLifecycleVariantDescriptor(cfg, fleetLifecycleVariantProvider)
	return descriptor
}

func fleetLifecycleEvidenceDescriptors(cfg *ResolvedConfig, stateDir string, epoch uint64) ([]fleetLifecycleEvidenceDescriptor, error) {
	if cfg == nil || cfg.Config == nil {
		return nil, errors.New("fleet lifecycle evidence descriptor configuration is unavailable")
	}
	var lifecycle *FleetLifecycleEvidence
	loaded, err := loadFleetLifecycleEvidence(stateDir)
	if err == nil {
		lifecycle = loaded
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	fallbackActive := lifecycle != nil && lifecycle.FallbackEffectiveEpoch != 0 && epoch >= lifecycle.FallbackEffectiveEpoch
	providerActive := lifecycle != nil && lifecycle.ProviderEffectiveEpoch != 0 && epoch >= lifecycle.ProviderEffectiveEpoch
	terminalActive := lifecycle != nil && lifecycle.TerminalEffectiveEpoch != 0 && epoch >= lifecycle.TerminalEffectiveEpoch
	fallback, err := fallbackFleetEvidenceDescriptor(cfg)
	if err != nil {
		return nil, err
	}
	descriptors := make([]fleetLifecycleEvidenceDescriptor, 0, cfg.Config.Topology.fleetCandidates())
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		switch {
		case terminalActive && fleet == fleetLifecycleTargetFleet:
			descriptor, descriptorErr := fleetLifecycleVariantDescriptor(cfg, fleetLifecycleVariantProvider)
			if descriptorErr != nil {
				return nil, descriptorErr
			}
			descriptors = append(descriptors, descriptor)
		case terminalActive && fleet == fleetLifecycleCompanionFleet:
			descriptor, descriptorErr := fleetLifecycleVariantDescriptor(cfg, fleetLifecycleVariantTerminal)
			if descriptorErr != nil {
				return nil, descriptorErr
			}
			descriptors = append(descriptors, descriptor)
		case providerActive && fleet == fleetLifecycleTargetFleet:
			descriptors = append(descriptors, providerFleetEvidenceDescriptor(cfg))
		case providerActive && fleet == fleetLifecycleCompanionFleet:
			descriptors = append(descriptors, fallback)
		case fallbackActive && fleet == fleetLifecycleTargetFleet:
			descriptors = append(descriptors, fallback)
		case lifecycle != nil && fleet == fleetLifecycleTargetFleet:
			descriptor, descriptorErr := fleetLifecycleVariantDescriptor(cfg, fleetLifecycleVariantTargetTakeover)
			if descriptorErr != nil {
				return nil, descriptorErr
			}
			descriptors = append(descriptors, descriptor)
		case lifecycle != nil && fleet == fleetLifecycleCompanionFleet:
			descriptor, descriptorErr := fleetLifecycleVariantDescriptor(cfg, fleetLifecycleVariantCompanionTakeover)
			if descriptorErr != nil {
				return nil, descriptorErr
			}
			descriptors = append(descriptors, descriptor)
		default:
			descriptors = append(descriptors, standardFleetEvidenceDescriptor(cfg, fleet))
		}
	}
	return descriptors, nil
}

func fleetLifecycleFallbackManifest(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets) (protocol.FleetManifest, []byte, [32]byte, error) {
	variant, _ := fleetLifecycleVariantFor(fleetLifecycleVariantFallback)
	return fleetLifecycleVariantManifest(cfg, stateDir, roles, variant)
}

func fleetLifecycleProviderManifest(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets) (protocol.FleetManifest, []byte, [32]byte, error) {
	variant, _ := fleetLifecycleVariantFor(fleetLifecycleVariantProvider)
	return fleetLifecycleVariantManifest(cfg, stateDir, roles, variant)
}

func loadFleetLifecycleCommitment(stateDir, manifestName, evidenceName string) (protocol.FleetManifest, [32]byte, *FleetCommitmentEvidence, error) {
	manifestBytes, err := os.ReadFile(filepath.Join(stateDir, "public", manifestName))
	if err != nil {
		return protocol.FleetManifest{}, [32]byte{}, nil, err
	}
	manifest, err := protocol.ParseFleetManifest(manifestBytes)
	if err != nil {
		return protocol.FleetManifest{}, [32]byte{}, nil, err
	}
	hash, err := manifest.CommitmentHash()
	if err != nil {
		return protocol.FleetManifest{}, [32]byte{}, nil, err
	}
	evidenceBytes, err := os.ReadFile(filepath.Join(stateDir, "public", evidenceName))
	if err != nil {
		return protocol.FleetManifest{}, [32]byte{}, nil, err
	}
	var evidence FleetCommitmentEvidence
	if err := json.Unmarshal(evidenceBytes, &evidence); err != nil {
		return protocol.FleetManifest{}, [32]byte{}, nil, err
	}
	wantHash := fleetLifecycleHex(hash)
	wantHotkey := fleetLifecycleHex(manifest.Hotkey)
	if evidence.Schema != fleetCommitmentEvidenceSchemaV2 || evidence.DeploymentID == "" || evidence.PlanHash == "" || evidence.ActionID == "" || evidence.IntentHash == "" || evidence.ManifestURI != manifestName || !strings.EqualFold(evidence.CommitmentHash, wantHash) || !strings.EqualFold(evidence.Hotkey, wantHotkey) || evidence.FinalizedBlock == 0 || evidence.FinalizedBlock != evidence.CommitmentBlock {
		return protocol.FleetManifest{}, [32]byte{}, nil, errors.New("fleet lifecycle commitment evidence differs from its manifest")
	}
	if _, err := decodeHex32("fleet lifecycle commitment transaction", evidence.ExtrinsicHash); err != nil {
		return protocol.FleetManifest{}, [32]byte{}, nil, err
	}
	if _, err := decodeHex32("fleet lifecycle commitment block", evidence.FinalizedBlockHash); err != nil {
		return protocol.FleetManifest{}, [32]byte{}, nil, err
	}
	return *manifest, hash, &evidence, nil
}

func validateFleetLifecycleCommitmentLineage(evidence FleetCommitmentEvidence, action Action, variantName, deploymentID, planHash string, transaction JournalEntry) error {
	variant, err := fleetLifecycleVariantFor(variantName)
	if err != nil {
		return err
	}
	wantActionID, err := fleetLifecycleCommitmentActionID(variantName)
	if err != nil {
		return err
	}
	if action.ID != wantActionID || action.Kind != "substrate-extrinsic" || action.Target != variant.HotkeyLabel || action.Parameters["generation"] != strconv.FormatUint(variant.Generation, 10) {
		return errors.New("fleet lifecycle commitment action differs from its exact variant")
	}
	if evidence.DeploymentID != deploymentID || evidence.PlanHash != planHash || evidence.ActionID != action.ID || evidence.IntentHash != action.IntentHash || transaction.Stage != StageFinalized || transaction.PlanHash != planHash || transaction.ActionID != action.ID || transaction.IntentHash != action.IntentHash || !strings.EqualFold(transaction.TransactionHash, evidence.ExtrinsicHash) || transaction.BlockNumber != evidence.FinalizedBlock || !strings.EqualFold(transaction.BlockHash, evidence.FinalizedBlockHash) || transaction.RecoveryBlock == 0 || transaction.RecoveryBlock >= transaction.BlockNumber || transaction.RecoveryBlockHash == "" {
		return errors.New("fleet lifecycle commitment has no exact approved finalized journal lineage")
	}
	return nil
}

func (self *Executor) validateFleetLifecycleCommitmentAction(ctx context.Context, action Action, variantName string, manifest protocol.FleetManifest, commitmentHash [32]byte, evidence FleetCommitmentEvidence) error {
	variant, err := fleetLifecycleVariantFor(variantName)
	if err != nil {
		return err
	}
	transaction, found := self.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash)
	if !found {
		return errors.New("fleet lifecycle commitment journal lineage is absent")
	}
	if err := validateFleetLifecycleCommitmentLineage(evidence, action, variantName, self.cfg.Config.Deployment.DeploymentID, self.plan.PlanHash, transaction); err != nil {
		return err
	}
	if evidence.ManifestURI != variant.ManifestName || !strings.EqualFold(evidence.CommitmentHash, fleetLifecycleHex(commitmentHash)) || !strings.EqualFold(evidence.Hotkey, fleetLifecycleHex(manifest.Hotkey)) {
		return errors.New("fleet lifecycle commitment differs from its exact manifest identity")
	}
	if err := self.verifySubstrateTransactionEvidence(ChainHead{Number: evidence.FinalizedBlock, Hash: evidence.FinalizedBlockHash}, evidence.ExtrinsicHash); err != nil {
		return err
	}
	blockHash, err := types.NewHashFromHexString(evidence.FinalizedBlockHash)
	if err != nil {
		return err
	}
	observed, err := self.substrate.fleetCommitmentAt(manifest.Hotkey, blockHash)
	if err != nil {
		return err
	}
	return crv4.ValidateFleetCommitmentWrite(commitmentHash, evidence.FinalizedBlock, observed)
}

func (self *Executor) publishFleetLifecycleCommitment(ctx context.Context, action Action, variantName string) error {
	variant, err := fleetLifecycleVariantFor(variantName)
	if err != nil {
		return err
	}
	wantActionID, err := fleetLifecycleCommitmentActionID(variantName)
	if err != nil {
		return err
	}
	if action.ID != wantActionID || action.Kind != "substrate-extrinsic" || action.Target != variant.HotkeyLabel || action.Parameters["generation"] != strconv.FormatUint(variant.Generation, 10) {
		return errors.New("fleet lifecycle commitment action differs from its exact variant")
	}
	manifest, canonical, hash, err := fleetLifecycleVariantManifest(self.cfg, self.stateDir, self.roles, variant)
	if err != nil {
		return err
	}
	if expected := action.Parameters["expected_uid"]; expected != "" {
		want, parseErr := strconv.ParseUint(expected, 10, 16)
		uid, found, readErr := self.substrate.UID(manifest.Hotkey)
		if parseErr != nil || readErr != nil || !found || uid != uint16(want) {
			return stateMismatchError(errors.Join(parseErr, readErr), "fleet lifecycle %s hotkey UID=%d found=%t, want %s", variantName, uid, found, expected)
		}
	}
	manifestPath := filepath.Join(self.stateDir, "public", variant.ManifestName)
	if prior, readErr := os.ReadFile(manifestPath); readErr == nil {
		if !bytes.Equal(prior, append(append([]byte(nil), canonical...), '\n')) {
			return errors.New("persisted fleet lifecycle manifest differs from its canonical plan-bound identity")
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	} else if err := atomicWrite(manifestPath, append(canonical, '\n'), 0o644); err != nil {
		return err
	}
	if _, _, evidence, loadErr := loadFleetLifecycleCommitment(self.stateDir, variant.ManifestName, variant.CommitmentName); loadErr == nil {
		return self.validateFleetLifecycleCommitmentAction(ctx, action, variantName, manifest, hash, *evidence)
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return loadErr
	}
	call, err := self.substrate.chain.NewSetFleetCommitmentCall(self.cfg.Netuid, hash)
	if err != nil {
		return err
	}
	signer, err := self.substrate.RoleSigner(self.roles, variant.HotkeyLabel)
	if err != nil {
		return err
	}
	txHash, block, err := self.substrate.SendAs(ctx, self.plan.PlanHash, action, call, signer)
	if err != nil {
		return err
	}
	blockHash, err := self.substrate.chain.API.RPC.Chain.GetBlockHash(block)
	if err != nil {
		return err
	}
	observed, err := self.substrate.fleetCommitmentAt(manifest.Hotkey, blockHash)
	if err != nil {
		return err
	}
	if err := crv4.ValidateFleetCommitmentWrite(hash, block, observed); err != nil {
		return err
	}
	evidence := FleetCommitmentEvidence{
		Schema: fleetCommitmentEvidenceSchemaV2, DeploymentID: self.cfg.Config.Deployment.DeploymentID,
		PlanHash: self.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		ManifestURI: variant.ManifestName, CommitmentHash: fleetLifecycleHex(hash), Hotkey: fleetLifecycleHex(manifest.Hotkey),
		ExtrinsicHash: txHash.Hex(), CommitmentBlock: observed.CommitmentBlock, FinalizedBlock: block, FinalizedBlockHash: blockHash.Hex(),
	}
	if err := self.validateFleetLifecycleCommitmentAction(ctx, action, variantName, manifest, hash, evidence); err != nil {
		return err
	}
	return writePublicJSON(filepath.Join(self.stateDir, "public", variant.CommitmentName), evidence)
}

func (self *Executor) fleetLifecycleManifestAndCommitment(variantName string) (protocol.FleetManifest, [32]byte, *FleetCommitmentEvidence, error) {
	variant, err := fleetLifecycleVariantFor(variantName)
	if err != nil {
		return protocol.FleetManifest{}, [32]byte{}, nil, err
	}
	return loadFleetLifecycleCommitment(self.stateDir, variant.ManifestName, variant.CommitmentName)
}

func fleetLifecycleMirrorEvidenceName(variantName string) string {
	return fmt.Sprintf("fleet-lifecycle-%s.mirror.json", variantName)
}

func validateFleetLifecycleMirrorLineage(evidence FleetLifecycleMirrorEvidence, action Action, variantName, deploymentID, planHash string, transaction JournalEntry) error {
	wantActionID, err := fleetLifecycleMirrorActionID(variantName)
	if err != nil {
		return err
	}
	if action.ID != wantActionID || action.Kind != "evm-transaction" || evidence.Schema != "urnetwork-sim-fleet-commitment-mirror-v1" || evidence.DeploymentID != deploymentID || evidence.PlanHash != planHash || evidence.ActionID != action.ID || evidence.IntentHash != action.IntentHash || evidence.FinalizedBlock == 0 || evidence.BlockNumber == 0 || transaction.Stage != StageFinalized || transaction.PlanHash != planHash || transaction.ActionID != action.ID || transaction.IntentHash != action.IntentHash || !strings.EqualFold(transaction.TransactionHash, evidence.TransactionHash) || transaction.BlockNumber != evidence.BlockNumber || !strings.EqualFold(transaction.BlockHash, evidence.BlockHash) || transaction.RecoveryBlock == 0 || transaction.RecoveryBlock >= transaction.BlockNumber || transaction.RecoveryBlockHash == "" {
		return errors.New("fleet lifecycle mirror has no exact approved finalized action lineage")
	}
	for label, value := range map[string]string{
		"hotkey": evidence.Hotkey, "commitment": evidence.CommitmentHash, "native block": evidence.FinalizedBlockHash,
		"transaction": evidence.TransactionHash, "block": evidence.BlockHash,
	} {
		if _, ok := evidenceFixedHex(value, 32); !ok {
			return fmt.Errorf("fleet lifecycle mirror %s is invalid", label)
		}
	}
	return nil
}

func (self *Executor) validateFleetLifecycleMirrorAction(ctx context.Context, action Action, variantName string, manifest protocol.FleetManifest, commitmentHash [32]byte, commitment FleetCommitmentEvidence, evidence FleetLifecycleMirrorEvidence) error {
	transaction, found := self.oracle.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash)
	if !found {
		return errors.New("fleet lifecycle mirror journal lineage is absent")
	}
	if err := validateFleetLifecycleMirrorLineage(evidence, action, variantName, self.cfg.Config.Deployment.DeploymentID, self.plan.PlanHash, transaction); err != nil {
		return err
	}
	if !strings.EqualFold(evidence.Hotkey, fleetLifecycleHex(manifest.Hotkey)) || !strings.EqualFold(evidence.CommitmentHash, fleetLifecycleHex(commitmentHash)) || evidence.FinalizedBlock != commitment.FinalizedBlock || !strings.EqualFold(evidence.FinalizedBlockHash, commitment.FinalizedBlockHash) {
		return errors.New("fleet lifecycle mirror evidence differs from its exact native commitment")
	}
	finalized, err := finalizedEVMHead(ctx, self.oracle.client)
	if err != nil {
		return err
	}
	receipt, err := verifyFinalizedEVMReceipt(ctx, self.oracle.client, finalized, evidence.TransactionHash, evidence.BlockNumber, evidence.BlockHash)
	if err != nil {
		return err
	}
	coordinator := stabi.NewSTCoordinator()
	address := common.BytesToAddress(manifest.Coordinator[:])
	if !strings.EqualFold(action.Target, address.Hex()) {
		return errors.New("fleet lifecycle mirror action targets another coordinator")
	}
	nativeHash, err := decodeHex32("fleet lifecycle mirror native block", evidence.FinalizedBlockHash)
	if err != nil {
		return err
	}
	mirror, err := rawCoordinatorCallAt(ctx, self.oracle, address, coordinator.PackMirroredCommitments(manifest.Hotkey), coordinator.UnpackMirroredCommitments, evidence.BlockNumber)
	if err != nil || !fleetMirrorMatches(mirror, commitmentHash, evidence.FinalizedBlock, nativeHash) {
		return stateMismatchError(err, "fleet lifecycle mirror differs from exact historical coordinator state")
	}
	events := 0
	for _, log := range receipt.Logs {
		if log == nil || log.Address != address {
			continue
		}
		event, eventErr := coordinator.UnpackCommitmentMirroredEvent(log)
		if eventErr == nil && event.Hotkey == manifest.Hotkey && event.CommitmentHash == commitmentHash && event.FinalizedBlock == evidence.FinalizedBlock && event.FinalizedBlockHash == nativeHash {
			events++
		}
	}
	if events != 1 {
		return fmt.Errorf("fleet lifecycle mirror receipt has %d exact CommitmentMirrored events, want 1", events)
	}
	return nil
}

func (self *Executor) mirrorFleetLifecycleCommitment(ctx context.Context, action Action, variantName string) error {
	manifest, commitmentHash, evidence, err := self.fleetLifecycleManifestAndCommitment(variantName)
	if err != nil {
		return err
	}
	commitmentActionID, err := fleetLifecycleCommitmentActionID(variantName)
	if err != nil {
		return err
	}
	commitmentAction, err := self.planAction(commitmentActionID)
	if err != nil {
		return err
	}
	if err := self.validateFleetLifecycleCommitmentAction(ctx, commitmentAction, variantName, manifest, commitmentHash, *evidence); err != nil {
		return err
	}
	wantMirrorActionID, err := fleetLifecycleMirrorActionID(variantName)
	if err != nil {
		return err
	}
	if action.ID != wantMirrorActionID || action.Kind != "evm-transaction" || !strings.EqualFold(action.Target, common.BytesToAddress(manifest.Coordinator[:]).Hex()) {
		return errors.New("fleet lifecycle mirror action differs from its exact variant or coordinator")
	}
	mirrorPath := filepath.Join(self.stateDir, "public", fleetLifecycleMirrorEvidenceName(variantName))
	if raw, readErr := os.ReadFile(mirrorPath); readErr == nil {
		var mirrorEvidence FleetLifecycleMirrorEvidence
		if json.Unmarshal(raw, &mirrorEvidence) != nil {
			return errors.New("fleet lifecycle mirror evidence is invalid JSON")
		}
		return self.validateFleetLifecycleMirrorAction(ctx, action, variantName, manifest, commitmentHash, *evidence, mirrorEvidence)
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	finalizedHash, err := decodeHex32("fleet lifecycle commitment finalized block", evidence.FinalizedBlockHash)
	if err != nil {
		return err
	}
	coordinator := stabi.NewSTCoordinator()
	address := common.BytesToAddress(manifest.Coordinator[:])
	if current, readErr := rawCoordinatorCall(ctx, self.oracle, address, coordinator.PackMirroredCommitments(manifest.Hotkey), coordinator.UnpackMirroredCommitments); readErr == nil && fleetMirrorMatches(current, commitmentHash, evidence.FinalizedBlock, finalizedHash) {
		if _, found := self.oracle.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash); !found {
			return errors.New("fleet lifecycle mirror exists without exact approved transaction lineage")
		}
	}
	nativeHash, err := types.NewHashFromHexString(evidence.FinalizedBlockHash)
	if err != nil {
		return err
	}
	observed, err := self.substrate.fleetCommitmentAt(manifest.Hotkey, nativeHash)
	if err != nil {
		return err
	}
	if err := crv4.ValidateFleetCommitmentWrite(commitmentHash, evidence.FinalizedBlock, observed); err != nil {
		return err
	}
	canonical, err := self.substrate.chain.API.RPC.Chain.GetBlockHash(evidence.FinalizedBlock)
	if err != nil || canonical != nativeHash {
		return stateMismatchError(err, "fleet lifecycle commitment block %d is not canonical", evidence.FinalizedBlock)
	}
	data, err := coordinator.TryPackMirrorCommitment(manifest.Hotkey, commitmentHash, evidence.FinalizedBlock, finalizedHash)
	if err != nil {
		return err
	}
	receipt, err := self.oracle.Send(ctx, self.plan.PlanHash, action, &address, big.NewInt(0), data)
	if err != nil {
		return err
	}
	current, err := rawCoordinatorCall(ctx, self.oracle, address, coordinator.PackMirroredCommitments(manifest.Hotkey), coordinator.UnpackMirroredCommitments)
	if err != nil || !fleetMirrorMatches(current, commitmentHash, evidence.FinalizedBlock, finalizedHash) {
		return stateMismatchError(err, "fleet lifecycle commitment mirror postcondition mismatch")
	}
	mirrorEvidence := FleetLifecycleMirrorEvidence{
		Schema: "urnetwork-sim-fleet-commitment-mirror-v1", DeploymentID: self.cfg.Config.Deployment.DeploymentID,
		PlanHash: self.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		Hotkey: fleetLifecycleHex(manifest.Hotkey), CommitmentHash: fleetLifecycleHex(commitmentHash), FinalizedBlock: evidence.FinalizedBlock, FinalizedBlockHash: evidence.FinalizedBlockHash,
		TransactionHash: receipt.TxHash.Hex(), BlockNumber: receipt.BlockNumber.Uint64(), BlockHash: receipt.BlockHash.Hex(),
	}
	if err := self.validateFleetLifecycleMirrorAction(ctx, action, variantName, manifest, commitmentHash, *evidence, mirrorEvidence); err != nil {
		return err
	}
	return writePublicJSON(mirrorPath, mirrorEvidence)
}

func fleetLifecycleBindingEvidenceName(variantName string, member int) string {
	variant, err := fleetLifecycleVariantFor(variantName)
	if err != nil {
		return ""
	}
	return variant.BindingName(member)
}

func buildFleetLifecycleBindingEvidenceFromReceipt(action Action, deploymentID, planHash string, manifest protocol.FleetManifest, commitmentHash [32]byte, member protocol.FleetMember, clientPrivateKey ed25519.PrivateKey, hotkey *crv4.Keypair, receipt *ethtypes.Receipt) (FleetBindingEvidence, error) {
	if hotkey == nil || receipt == nil || receipt.BlockNumber == nil || receipt.BlockNumber.Sign() <= 0 || receipt.BlockHash == (common.Hash{}) {
		return FleetBindingEvidence{}, errors.New("fleet lifecycle binding receipt is incomplete")
	}
	coordinator := stabi.NewSTCoordinator()
	address := common.BytesToAddress(manifest.Coordinator[:])
	var bound *stabi.STCoordinatorFleetBound
	for _, log := range receipt.Logs {
		if log == nil || log.Address != address {
			continue
		}
		event, err := coordinator.UnpackFleetBoundEvent(log)
		if err != nil || event.ClientId != member.ClientID || event.FleetId != manifest.FleetID || event.Hotkey != manifest.Hotkey || event.Generation != manifest.Generation {
			continue
		}
		if bound != nil {
			return FleetBindingEvidence{}, errors.New("fleet lifecycle binding receipt has duplicate exact FleetBound events")
		}
		bound = event
	}
	if bound == nil || bound.ValidFromEpoch == 0 || bound.ValidToEpoch < bound.ValidFromEpoch {
		return FleetBindingEvidence{}, errors.New("fleet lifecycle binding receipt has no exact FleetBound event")
	}
	binding, err := manifest.Binding(member, bound.ValidFromEpoch, bound.ValidToEpoch)
	if err != nil {
		return FleetBindingEvidence{}, err
	}
	clientSignature, err := binding.SignClient(clientPrivateKey)
	if err != nil {
		return FleetBindingEvidence{}, err
	}
	digest, err := binding.Digest()
	if err != nil {
		return FleetBindingEvidence{}, err
	}
	hotkeySignature, err := hotkey.Sign(digest[:])
	if err != nil {
		return FleetBindingEvidence{}, err
	}
	return FleetBindingEvidence{
		Schema: "urnetwork-fleet-binding-evidence-v1", DeploymentID: deploymentID, PlanHash: planHash, ActionID: action.ID, IntentHash: action.IntentHash,
		ClientID: fleetLifecycleHex16(member.ClientID), ClientKey: fleetLifecycleHex(member.ClientKey), FleetID: fleetLifecycleHex(binding.FleetID), Hotkey: fleetLifecycleHex(binding.Hotkey),
		Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, CommitmentHash: fleetLifecycleHex(commitmentHash), BindingDigest: fleetLifecycleHex(digest),
		ClientSignature: "0x" + hex.EncodeToString(clientSignature), HotkeySignature: "0x" + hex.EncodeToString(hotkeySignature),
		TransactionHash: receipt.TxHash.Hex(), BlockNumber: receipt.BlockNumber.Uint64(), BlockHash: receipt.BlockHash.Hex(), UID: bound.Uid,
	}, nil
}

func (self *Executor) bindFleetLifecycleMember(ctx context.Context, action Action, variantName string, memberIndex int) error {
	variant, err := fleetLifecycleVariantFor(variantName)
	if err != nil {
		return err
	}
	manifest, commitmentHash, commitmentEvidence, err := self.fleetLifecycleManifestAndCommitment(variantName)
	if err != nil {
		return err
	}
	if memberIndex < 1 || memberIndex > len(manifest.Members) {
		return fmt.Errorf("fleet lifecycle member index %d is out of range", memberIndex)
	}
	member := manifest.Members[memberIndex-1]
	minerIndex := fleetMemberMinerIndex(self.cfg, variant.Fleet, memberIndex)
	if variant.Fallback {
		minerIndex, err = fleetLifecycleFallbackMinerIndex(self.cfg, memberIndex)
		if err != nil {
			return err
		}
	}
	wantActionID, err := fleetLifecycleBindingActionID(variantName, memberIndex)
	if err != nil {
		return err
	}
	if action.ID != wantActionID || action.Kind != "evm-transaction" || action.Target != fmt.Sprintf("miner:%d", minerIndex) {
		return errors.New("fleet lifecycle binding action differs from its exact variant and client")
	}
	clientRole := self.roles.Clients[fmt.Sprintf("miner-%d", minerIndex)]
	seed, err := hex.DecodeString(clientRole.SeedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		return fmt.Errorf("fleet lifecycle miner-%d client seed is invalid", minerIndex)
	}
	hotkeyRole, ok := self.roles.Substrate[variant.HotkeyLabel]
	if !ok {
		return fmt.Errorf("fleet lifecycle hotkey role %s is unavailable", variant.HotkeyLabel)
	}
	hotkey, err := crv4.KeypairFromSeedHex(hotkeyRole.SeedHex)
	if err != nil {
		return err
	}
	commitmentActionID, err := fleetLifecycleCommitmentActionID(variantName)
	if err != nil {
		return err
	}
	commitmentAction, err := self.planAction(commitmentActionID)
	if err != nil {
		return err
	}
	if err := self.validateFleetLifecycleCommitmentAction(ctx, commitmentAction, variantName, manifest, commitmentHash, *commitmentEvidence); err != nil {
		return err
	}
	evidencePath := filepath.Join(self.stateDir, "public", fleetLifecycleBindingEvidenceName(variantName, memberIndex))
	if _, readErr := os.Stat(evidencePath); readErr == nil {
		finalized, finalizedErr := finalizedEVMHead(ctx, self.keeper.client)
		if finalizedErr != nil {
			return finalizedErr
		}
		_, verifyErr := self.verifyFleetLifecycleBindingAt(ctx, variantName, memberIndex, finalized)
		return verifyErr
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	coordinator := stabi.NewSTCoordinator()
	address := common.BytesToAddress(manifest.Coordinator[:])
	clientPrivateKey := ed25519.NewKeyFromSeed(seed)
	if transaction, found := self.keeper.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash); found && transaction.Stage == StageFinalized {
		finalized, finalizedErr := finalizedEVMHead(ctx, self.keeper.client)
		if finalizedErr != nil {
			return finalizedErr
		}
		receipt, receiptErr := verifyFinalizedEVMReceipt(ctx, self.keeper.client, finalized, transaction.TransactionHash, transaction.BlockNumber, transaction.BlockHash)
		if receiptErr != nil {
			return receiptErr
		}
		evidence, evidenceErr := buildFleetLifecycleBindingEvidenceFromReceipt(action, self.cfg.Config.Deployment.DeploymentID, self.plan.PlanHash, manifest, commitmentHash, member, clientPrivateKey, hotkey, receipt)
		if evidenceErr != nil {
			return evidenceErr
		}
		if writeErr := writePublicJSON(evidencePath, evidence); writeErr != nil {
			return writeErr
		}
		_, verifyErr := self.verifyFleetLifecycleBindingAt(ctx, variantName, memberIndex, finalized)
		return verifyErr
	}
	for {
		window, err := waitFutureEpochTransactionWindow(ctx, self.keeper, address, coordinator)
		if err != nil {
			return err
		}
		validTo, err := fleetBindingValidityEnd(window.EffectiveEpoch, self.cfg.Policy.Binding.MaximumValidityEpochs)
		if err != nil {
			return err
		}
		binding, err := manifest.Binding(member, window.EffectiveEpoch, validTo)
		if err != nil {
			return err
		}
		if prior, readErr := rawCoordinatorCall(ctx, self.keeper, address, coordinator.PackBindingAt(member.ClientID, new(big.Int).SetUint64(window.EffectiveEpoch)), coordinator.UnpackBindingAt); readErr == nil && prior.Active && prior.Record.Generation == binding.Generation && prior.Record.CommitmentHash == binding.CommitmentHash {
			if _, found := self.keeper.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash); !found {
				return errors.New("fleet lifecycle binding exists without exact approved transaction lineage")
			}
		}
		clientSignature, err := binding.SignClient(clientPrivateKey)
		if err != nil {
			return err
		}
		digest, err := binding.Digest()
		if err != nil {
			return err
		}
		hotkeySignature, err := hotkey.Sign(digest[:])
		if err != nil {
			return err
		}
		contractBinding := stabi.STCoordinatorFleetBinding{ChainId: binding.ChainID, Netuid: binding.Netuid, Coordinator: address, FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientId: binding.ClientID, ClientKey: binding.ClientKey, Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, CommitmentHash: binding.CommitmentHash}
		data, err := coordinator.TryPackBindFleetMember(contractBinding, clientSignature, hotkeySignature)
		if err != nil {
			return err
		}
		receipt, sendErr := self.keeper.Send(ctx, self.plan.PlanHash, action, &address, big.NewInt(0), data)
		if sendErr != nil {
			_, persisted := self.keeper.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash)
			latest, _, readErr := readFutureEpochTransactionWindow(ctx, self.keeper, address, coordinator)
			if readErr == nil && retryFleetBindingAfterEpochTransition(latest.CurrentEpoch, window.EffectiveEpoch, persisted) {
				continue
			}
			return sendErr
		}
		evidence, err := buildFleetLifecycleBindingEvidenceFromReceipt(action, self.cfg.Config.Deployment.DeploymentID, self.plan.PlanHash, manifest, commitmentHash, member, clientPrivateKey, hotkey, receipt)
		if err != nil {
			return err
		}
		if err := writePublicJSON(evidencePath, evidence); err != nil {
			return err
		}
		finalized, err := finalizedEVMHead(ctx, self.keeper.client)
		if err != nil {
			return err
		}
		_, err = self.verifyFleetLifecycleBindingAt(ctx, variantName, memberIndex, finalized)
		return err
	}
}

func fleetLifecycleHex16(value [16]byte) string {
	return "0x" + hex.EncodeToString(value[:])
}

func fleetLifecycleCleanupEvidenceName(variantName string, member int) string {
	return fmt.Sprintf("fleet-lifecycle-%s-member-%d.cleanup.json", variantName, member)
}

func fleetLifecycleRegistrationNames(variantName string) (string, string) {
	switch variantName {
	case fleetLifecycleVariantProvider:
		return "fleet-lifecycle-provider.registration-pre.json", "fleet-lifecycle-provider.registration.json"
	case fleetLifecycleVariantTerminal:
		return "fleet-lifecycle-terminal.registration-pre.json", "fleet-lifecycle-terminal.registration.json"
	default:
		return "fleet-lifecycle-fallback.registration-pre.json", "fleet-lifecycle-fallback.registration.json"
	}
}

func fleetLifecyclePruneInputByHotkey(snapshot FleetLifecyclePruneSnapshot, hotkey [32]byte) (FleetLifecyclePruneInput, bool) {
	encoded := fleetLifecycleHex(hotkey)
	for _, input := range snapshot.Inputs {
		if strings.EqualFold(input.Hotkey, encoded) {
			return input, true
		}
	}
	return FleetLifecyclePruneInput{}, false
}

func validateFleetLifecycleRegistrationEvidence(evidence FleetLifecycleRegistrationEvidence) error {
	if evidence.Schema != "urnetwork-sim-fleet-registration-replacement-v1" || evidence.DeploymentID == "" || evidence.PlanHash == "" || evidence.ActionID == "" || evidence.IntentHash == "" || evidence.VictimRole == "" || evidence.VictimFleet < 0 || evidence.TransactionHash == "" || evidence.BlockNumber == 0 || evidence.BlockHash == "" || evidence.PostRegistration.Head.Number != evidence.BlockNumber || !strings.EqualFold(evidence.PostRegistration.Head.Hash, evidence.BlockHash) {
		return errors.New("fleet lifecycle registration evidence is incomplete")
	}
	victimHotkey, err := decodeHex32("fleet lifecycle victim hotkey", evidence.VictimHotkey)
	if err != nil {
		return err
	}
	victimColdkey, err := decodeHex32("fleet lifecycle victim coldkey", evidence.VictimColdkey)
	if err != nil {
		return err
	}
	replacementHotkey, err := decodeHex32("fleet lifecycle replacement hotkey", evidence.ReplacementHotkey)
	if err != nil {
		return err
	}
	replacementColdkey, err := decodeHex32("fleet lifecycle replacement coldkey", evidence.ReplacementColdkey)
	if err != nil {
		return err
	}
	if err := validateFleetLifecyclePruneSnapshot(evidence.PrePrune, victimHotkey, victimColdkey); err != nil {
		return err
	}
	preVictim, found := fleetLifecyclePruneInputByHotkey(evidence.PrePrune, victimHotkey)
	if !found || preVictim.UID != evidence.VictimUID {
		return errors.New("fleet lifecycle pre-state does not contain the exact victim UID")
	}
	if _, found := fleetLifecyclePruneInputByHotkey(evidence.PrePrune, replacementHotkey); found {
		return errors.New("fleet lifecycle replacement hotkey was already live in pre-state")
	}
	if _, found := fleetLifecyclePruneInputByHotkey(evidence.PostRegistration, victimHotkey); found {
		return errors.New("fleet lifecycle victim hotkey remains live after replacement")
	}
	postReplacement, found := fleetLifecyclePruneInputByHotkey(evidence.PostRegistration, replacementHotkey)
	if !found || postReplacement.UID != evidence.VictimUID || !strings.EqualFold(postReplacement.Coldkey, fleetLifecycleHex(replacementColdkey)) {
		return errors.New("fleet lifecycle replacement did not assume the exact victim UID and owner")
	}
	if evidence.PrePrune.UIDCount != evidence.PostRegistration.UIDCount || evidence.PrePrune.MaximumUIDs != evidence.PostRegistration.MaximumUIDs || evidence.BlockNumber < evidence.PrePrune.Head.Number {
		return errors.New("fleet lifecycle registration changed capacity or predates its exact checkpoint")
	}
	if _, err := decodeHex32("fleet lifecycle registration transaction", evidence.TransactionHash); err != nil {
		return err
	}
	_, err = decodeHex32("fleet lifecycle registration block", evidence.BlockHash)
	return err
}

type fleetLifecycleRegistrationExpectation struct {
	victimFleet, expectedUID                        int
	victimHotkeyLabel, victimColdkeyLabel           string
	replacementHotkeyLabel, replacementColdkeyLabel string
}

func fleetLifecycleRegistrationExpectationFor(variantName string) (fleetLifecycleRegistrationExpectation, error) {
	switch variantName {
	case fleetLifecycleVariantFallback:
		return fleetLifecycleRegistrationExpectation{
			victimFleet: fleetLifecycleTargetFleet, expectedUID: fleetLifecycleTargetExpectedUID,
			victimHotkeyLabel: churnHotkeyLabel(fleetLifecycleTargetChurn), victimColdkeyLabel: churnColdkeyLabel(fleetLifecycleTargetChurn),
			replacementHotkeyLabel: churnHotkeyLabel(fleetLifecycleFallbackChurn), replacementColdkeyLabel: churnColdkeyLabel(fleetLifecycleFallbackChurn),
		}, nil
	case fleetLifecycleVariantProvider:
		return fleetLifecycleRegistrationExpectation{
			victimFleet: fleetLifecycleCompanionFleet, expectedUID: fleetLifecycleCompanionExpectedUID,
			victimHotkeyLabel: churnHotkeyLabel(fleetLifecycleCompanionChurn), victimColdkeyLabel: churnColdkeyLabel(fleetLifecycleCompanionChurn),
			replacementHotkeyLabel: churnHotkeyLabel(fleetLifecycleTargetChurn), replacementColdkeyLabel: churnColdkeyLabel(fleetLifecycleTargetChurn),
		}, nil
	case fleetLifecycleVariantTerminal:
		return fleetLifecycleRegistrationExpectation{
			victimFleet: 0, expectedUID: fleetLifecycleTerminalVictimUID,
			victimHotkeyLabel: churnHotkeyLabel(fleetLifecycleTerminalVictimChurn), victimColdkeyLabel: churnColdkeyLabel(fleetLifecycleTerminalVictimChurn),
			replacementHotkeyLabel: churnHotkeyLabel(fleetLifecycleCompanionChurn), replacementColdkeyLabel: churnColdkeyLabel(fleetLifecycleCompanionChurn),
		}, nil
	default:
		return fleetLifecycleRegistrationExpectation{}, fmt.Errorf("unsupported fleet lifecycle registration variant %q", variantName)
	}
}

func validateFleetLifecycleRegistrationLineage(evidence FleetLifecycleRegistrationEvidence, action Action, variantName, deploymentID, planHash string, roles *RoleSecrets, transaction JournalEntry) error {
	if err := validateFleetLifecycleRegistrationEvidence(evidence); err != nil {
		return err
	}
	expected, err := fleetLifecycleRegistrationExpectationFor(variantName)
	if err != nil {
		return err
	}
	wantActionID, err := fleetLifecycleRegistrationActionIDFor(variantName)
	if err != nil {
		return err
	}
	if action.ID != wantActionID || action.Kind != "substrate-extrinsic" || action.Target != expected.replacementHotkeyLabel {
		return errors.New("fleet lifecycle registration action differs from its exact variant and replacement role")
	}
	for field, want := range map[string]string{
		"expected_pruned_fleet": strconv.Itoa(expected.victimFleet), "expected_pruned_hotkey": expected.victimHotkeyLabel,
		"expected_pruned_uid": strconv.Itoa(expected.expectedUID), "expected_replacement_hotkey": expected.replacementHotkeyLabel,
	} {
		if action.Parameters[field] != want {
			return fmt.Errorf("fleet lifecycle registration %s=%q, want %q", field, action.Parameters[field], want)
		}
	}
	victimHotkey, err := roleBytes32(roles, expected.victimHotkeyLabel)
	if err != nil {
		return err
	}
	victimColdkey, err := roleBytes32(roles, expected.victimColdkeyLabel)
	if err != nil {
		return err
	}
	replacementHotkey, err := roleBytes32(roles, expected.replacementHotkeyLabel)
	if err != nil {
		return err
	}
	replacementColdkey, err := roleBytes32(roles, expected.replacementColdkeyLabel)
	if err != nil {
		return err
	}
	if evidence.DeploymentID != deploymentID || evidence.PlanHash != planHash || evidence.ActionID != action.ID || evidence.IntentHash != action.IntentHash || evidence.VictimFleet != expected.victimFleet || evidence.VictimRole != expected.victimHotkeyLabel || int(evidence.VictimUID) != expected.expectedUID || !strings.EqualFold(evidence.VictimHotkey, fleetLifecycleHex(victimHotkey)) || !strings.EqualFold(evidence.VictimColdkey, fleetLifecycleHex(victimColdkey)) || !strings.EqualFold(evidence.ReplacementHotkey, fleetLifecycleHex(replacementHotkey)) || !strings.EqualFold(evidence.ReplacementColdkey, fleetLifecycleHex(replacementColdkey)) {
		return errors.New("fleet lifecycle registration evidence differs from its exact variant, roles, or approved action")
	}
	if transaction.Stage != StageFinalized || transaction.PlanHash != planHash || transaction.ActionID != action.ID || transaction.IntentHash != action.IntentHash || !strings.EqualFold(transaction.TransactionHash, evidence.TransactionHash) || transaction.BlockNumber != evidence.BlockNumber || !strings.EqualFold(transaction.BlockHash, evidence.BlockHash) || transaction.RecoveryBlock != evidence.PrePrune.Head.Number || !strings.EqualFold(transaction.RecoveryBlockHash, evidence.PrePrune.Head.Hash) || transaction.RecoveryBlock == 0 || transaction.RecoveryBlock >= transaction.BlockNumber {
		return errors.New("fleet lifecycle registration evidence has no exact finalized journal lineage")
	}
	return nil
}

// validateFleetLifecycleRegistrationRecoverySnapshot proves the exact native
// state on which a lifecycle registration is authorized to broadcast. The
// runtime call itself has no expected-victim argument, so accepting a nearby
// but different finalized head would allow unrelated registration activity to
// redirect the prune between the simulator's preflight and journal checkpoint.
func validateFleetLifecycleRegistrationRecoverySnapshot(snapshot FleetLifecyclePruneSnapshot, variantName string, roles *RoleSecrets) error {
	expected, err := fleetLifecycleRegistrationExpectationFor(variantName)
	if err != nil {
		return err
	}
	if roles == nil {
		return errors.New("fleet lifecycle registration recovery roles are unavailable")
	}
	victimHotkey, err := roleBytes32(roles, expected.victimHotkeyLabel)
	if err != nil {
		return err
	}
	victimColdkey, err := roleBytes32(roles, expected.victimColdkeyLabel)
	if err != nil {
		return err
	}
	replacementHotkey, err := roleBytes32(roles, expected.replacementHotkeyLabel)
	if err != nil {
		return err
	}
	if err := validateFleetLifecyclePruneSnapshot(snapshot, victimHotkey, victimColdkey); err != nil {
		return err
	}
	victim, found := fleetLifecyclePruneInputByHotkey(snapshot, victimHotkey)
	if !found || int(victim.UID) != expected.expectedUID {
		return fmt.Errorf("fleet lifecycle recovery victim UID=%d found=%t, want %d", victim.UID, found, expected.expectedUID)
	}
	if _, live := fleetLifecyclePruneInputByHotkey(snapshot, replacementHotkey); live {
		return errors.New("fleet lifecycle recovery replacement is already live")
	}
	return nil
}

func (self *Executor) validateFleetLifecycleRegistrationAction(ctx context.Context, action Action, variantName string, evidence FleetLifecycleRegistrationEvidence) error {
	transaction, found := self.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash)
	if !found {
		return errors.New("fleet lifecycle registration journal lineage is absent")
	}
	if err := validateFleetLifecycleRegistrationLineage(evidence, action, variantName, self.cfg.Config.Deployment.DeploymentID, self.plan.PlanHash, self.roles, transaction); err != nil {
		return err
	}
	if err := self.verifySubstrateTransactionEvidence(ChainHead{Number: evidence.BlockNumber, Hash: evidence.BlockHash}, evidence.TransactionHash); err != nil {
		return err
	}
	recoveryHash, err := types.NewHashFromHexString(evidence.PrePrune.Head.Hash)
	if err != nil {
		return err
	}
	pre, err := self.substrate.fleetLifecyclePruneSnapshotAt(recoveryHash, evidence.PrePrune.Head.Number)
	if err != nil {
		return err
	}
	blockHash, err := types.NewHashFromHexString(evidence.BlockHash)
	if err != nil {
		return err
	}
	post, err := self.substrate.fleetLifecyclePruneSnapshotAt(blockHash, evidence.BlockNumber)
	if err != nil {
		return err
	}
	preHash, _ := canonicalHashHex(pre)
	wantPreHash, _ := canonicalHashHex(evidence.PrePrune)
	postHash, _ := canonicalHashHex(post)
	wantPostHash, _ := canonicalHashHex(evidence.PostRegistration)
	if preHash != wantPreHash || postHash != wantPostHash {
		return errors.New("fleet lifecycle registration snapshots differ from canonical public chain state")
	}
	return nil
}

func (self *Executor) resumeFleetLifecycleRegistration(ctx context.Context, action Action, variantName, coldkeyLabel, hotkeyLabel string, transaction JournalEntry, found bool) error {
	if found && (transaction.PlanHash != self.plan.PlanHash || transaction.ActionID != action.ID || transaction.IntentHash != action.IntentHash || transaction.TransactionHash == "" || transaction.RecoveryBlock == 0 || transaction.RecoveryBlockHash == "") {
		return errors.New("fleet lifecycle registration recovery journal identity is invalid")
	}
	hotkey, err := roleBytes32(self.roles, hotkeyLabel)
	if err != nil {
		return err
	}
	signer, err := self.substrate.RoleSigner(self.roles, coldkeyLabel)
	if err != nil {
		return err
	}
	_, limit, err := self.boundedRegistrationBurn(action)
	if err != nil {
		return err
	}
	call, err := self.substrate.BurnRegisterLimitCall(hotkey, limit)
	if err != nil {
		return err
	}
	if found {
		recoveryHash, decodeErr := types.NewHashFromHexString(transaction.RecoveryBlockHash)
		if decodeErr != nil {
			return decodeErr
		}
		snapshot, readErr := self.substrate.fleetLifecyclePruneSnapshotAt(recoveryHash, transaction.RecoveryBlock)
		if readErr != nil {
			return readErr
		}
		if err := validateFleetLifecycleRegistrationRecoverySnapshot(snapshot, variantName, self.roles); err != nil {
			return fmt.Errorf("fleet lifecycle registration recorded recovery checkpoint: %w", err)
		}
	}
	if !found || transaction.Stage != StageFinalized {
		precondition := func(recoveryHash types.Hash, recoveryBlock uint64) error {
			snapshot, readErr := self.substrate.fleetLifecyclePruneSnapshotAt(recoveryHash, recoveryBlock)
			if readErr != nil {
				return readErr
			}
			return validateFleetLifecycleRegistrationRecoverySnapshot(snapshot, variantName, self.roles)
		}
		if found {
			// SendAs resumes the exact raw transaction and deliberately does not
			// rebind its already journaled recovery checkpoint.
			precondition = nil
		}
		if _, _, err := self.substrate.SendAsWithRecoveryPrecondition(ctx, self.plan.PlanHash, action, call, signer, precondition); err != nil {
			return err
		}
	}
	coldkey, err := roleBytes32(self.roles, coldkeyLabel)
	if err != nil {
		return err
	}
	uid, live, err := self.substrate.UID(hotkey)
	if err != nil || !live {
		return stateMismatchError(err, "fleet lifecycle recovered registration hotkey is not live")
	}
	owner, err := self.substrate.HotkeyOwner(hotkey)
	if err != nil {
		return err
	}
	if err := validateHotkeyOwner(hotkeyLabel, owner, coldkey); err != nil {
		return err
	}
	if uid == 0 && self.cfg.Netuid != 0 {
		return errors.New("fleet lifecycle recovered registration unexpectedly assumed reserved UID zero")
	}
	return nil
}

func (self *Executor) registerFleetLifecycle(ctx context.Context, action Action, variantName string) error {
	expected, err := fleetLifecycleRegistrationExpectationFor(variantName)
	if err != nil {
		return err
	}
	victimFleet, expectedUID := expected.victimFleet, expected.expectedUID
	victimHotkeyLabel, victimColdkeyLabel := expected.victimHotkeyLabel, expected.victimColdkeyLabel
	replacementHotkeyLabel, replacementColdkeyLabel := expected.replacementHotkeyLabel, expected.replacementColdkeyLabel
	wantActionID, err := fleetLifecycleRegistrationActionIDFor(variantName)
	if err != nil {
		return err
	}
	if action.ID != wantActionID || action.Kind != "substrate-extrinsic" || action.Target != replacementHotkeyLabel {
		return errors.New("fleet lifecycle registration action differs from its exact variant and replacement role")
	}
	for field, want := range map[string]string{
		"expected_pruned_fleet": strconv.Itoa(victimFleet), "expected_pruned_hotkey": victimHotkeyLabel, "expected_pruned_uid": strconv.Itoa(expectedUID), "expected_replacement_hotkey": replacementHotkeyLabel,
	} {
		if action.Parameters[field] != want {
			return fmt.Errorf("fleet lifecycle registration %s=%q, want %q", field, action.Parameters[field], want)
		}
	}
	victimHotkey, err := roleBytes32(self.roles, victimHotkeyLabel)
	if err != nil {
		return err
	}
	victimColdkey, err := roleBytes32(self.roles, victimColdkeyLabel)
	if err != nil {
		return err
	}
	replacementHotkey, err := roleBytes32(self.roles, replacementHotkeyLabel)
	if err != nil {
		return err
	}
	replacementColdkey, err := roleBytes32(self.roles, replacementColdkeyLabel)
	if err != nil {
		return err
	}
	preName, evidenceName := fleetLifecycleRegistrationNames(variantName)
	evidencePath := filepath.Join(self.stateDir, "public", evidenceName)
	if raw, readErr := os.ReadFile(evidencePath); readErr == nil {
		var evidence FleetLifecycleRegistrationEvidence
		if json.Unmarshal(raw, &evidence) != nil {
			return errors.New("fleet lifecycle registration evidence is invalid JSON")
		}
		if evidence.PlanHash != self.plan.PlanHash || evidence.ActionID != action.ID || evidence.IntentHash != action.IntentHash {
			return errors.New("fleet lifecycle registration evidence is not bound to the approved action")
		}
		return self.validateFleetLifecycleRegistrationAction(ctx, action, variantName, evidence)
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	preparationPath := filepath.Join(self.stateDir, "public", preName)
	var persistedPreparation *fleetLifecycleRegistrationPreparation
	if raw, readErr := os.ReadFile(preparationPath); readErr == nil {
		var preparation fleetLifecycleRegistrationPreparation
		if json.Unmarshal(raw, &preparation) != nil || preparation.Schema != "urnetwork-sim-fleet-registration-pre-v1" || preparation.PlanHash != self.plan.PlanHash || preparation.ActionID != action.ID || preparation.IntentHash != action.IntentHash || preparation.VictimFleet != victimFleet || !strings.EqualFold(preparation.VictimHotkey, fleetLifecycleHex(victimHotkey)) {
			return errors.New("fleet lifecycle registration preparation is not bound to the approved action")
		}
		if err := validateFleetLifecyclePruneSnapshot(preparation.Snapshot, victimHotkey, victimColdkey); err != nil {
			return fmt.Errorf("fleet lifecycle persisted registration preflight: %w", err)
		}
		if _, live := fleetLifecyclePruneInputByHotkey(preparation.Snapshot, replacementHotkey); live {
			return errors.New("fleet lifecycle persisted registration preflight already contains the replacement")
		}
		persistedPreparation = &preparation
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	transaction, transactionFound := self.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash)
	if !transactionFound {
		prepared, readErr := self.substrate.FleetLifecyclePruneSnapshot()
		if readErr != nil {
			return readErr
		}
		if err := validateFleetLifecyclePruneSnapshot(prepared, victimHotkey, victimColdkey); err != nil {
			return err
		}
		if _, found := fleetLifecyclePruneInputByHotkey(prepared, replacementHotkey); found {
			return errors.New("fleet lifecycle replacement identity is unexpectedly live")
		}
		if persistedPreparation == nil {
			preparation := fleetLifecycleRegistrationPreparation{Schema: "urnetwork-sim-fleet-registration-pre-v1", PlanHash: self.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, VictimFleet: victimFleet, VictimHotkey: fleetLifecycleHex(victimHotkey), Snapshot: prepared}
			if err := writePublicJSON(preparationPath, preparation); err != nil {
				return err
			}
			persistedPreparation = &preparation
		}
	}
	if persistedPreparation == nil {
		return errors.New("fleet lifecycle registration journal has no exact pre-broadcast preparation artifact")
	}
	if err := self.resumeFleetLifecycleRegistration(ctx, action, variantName, replacementColdkeyLabel, replacementHotkeyLabel, transaction, transactionFound); err != nil {
		return err
	}
	transaction, found := self.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash)
	if !found || transaction.Stage != StageFinalized || transaction.BlockNumber == 0 || transaction.BlockHash == "" || transaction.RecoveryBlock == 0 || transaction.RecoveryBlockHash == "" {
		return errors.New("fleet lifecycle registration has no exact finalized transaction checkpoints")
	}
	recoveryHash, err := types.NewHashFromHexString(transaction.RecoveryBlockHash)
	if err != nil {
		return err
	}
	pre, err := self.substrate.fleetLifecyclePruneSnapshotAt(recoveryHash, transaction.RecoveryBlock)
	if err != nil {
		return err
	}
	if err := validateFleetLifecyclePruneSnapshot(pre, victimHotkey, victimColdkey); err != nil {
		return fmt.Errorf("fleet lifecycle transaction checkpoint: %w", err)
	}
	blockHash, err := types.NewHashFromHexString(transaction.BlockHash)
	if err != nil {
		return err
	}
	post, err := self.substrate.fleetLifecyclePruneSnapshotAt(blockHash, transaction.BlockNumber)
	if err != nil {
		return err
	}
	victim, found := fleetLifecyclePruneInputByHotkey(pre, victimHotkey)
	if !found || victim.UID != uint16(expectedUID) {
		return fmt.Errorf("fleet lifecycle transaction checkpoint victim UID=%d found=%t, want %d", victim.UID, found, expectedUID)
	}
	evidence := FleetLifecycleRegistrationEvidence{
		Schema: "urnetwork-sim-fleet-registration-replacement-v1", DeploymentID: self.cfg.Config.Deployment.DeploymentID,
		PlanHash: self.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, VictimFleet: victimFleet, VictimRole: victimHotkeyLabel, VictimUID: victim.UID,
		VictimHotkey: fleetLifecycleHex(victimHotkey), VictimColdkey: fleetLifecycleHex(victimColdkey), ReplacementHotkey: fleetLifecycleHex(replacementHotkey), ReplacementColdkey: fleetLifecycleHex(replacementColdkey),
		PrePrune: pre, PostRegistration: post, TransactionHash: transaction.TransactionHash, BlockNumber: transaction.BlockNumber, BlockHash: transaction.BlockHash,
	}
	if err := self.validateFleetLifecycleRegistrationAction(ctx, action, variantName, evidence); err != nil {
		return err
	}
	return writePublicJSON(evidencePath, evidence)
}

func validateFleetLifecycleCleanupEvidence(evidence FleetLifecycleCleanupEvidence, member protocol.FleetMember, fleetID [32]byte, generation uint64) error {
	if evidence.Schema != "urnetwork-sim-fleet-binding-cleanup-v2" || evidence.DeploymentID == "" || evidence.PlanHash == "" || evidence.ActionID == "" || evidence.IntentHash == "" || evidence.ClientID != fleetLifecycleHex16(member.ClientID) || evidence.FleetID != fleetLifecycleHex(fleetID) || evidence.Generation != generation || evidence.CleanedAtEpoch == 0 || evidence.MemberCountBefore == 0 || evidence.MemberCountAfter+1 != evidence.MemberCountBefore || evidence.BlockNumber < 2 || evidence.BeforeBlock.Number+1 != evidence.BlockNumber {
		return errors.New("fleet lifecycle cleanup evidence is incomplete or inconsistent")
	}
	if _, err := decodeHex32("fleet lifecycle cleanup transaction", evidence.TransactionHash); err != nil {
		return err
	}
	if _, err := decodeHex32("fleet lifecycle cleanup block", evidence.BlockHash); err != nil {
		return err
	}
	_, err := decodeHex32("fleet lifecycle cleanup parent block", evidence.BeforeBlock.Hash)
	return err
}

func validateFleetLifecycleCleanupLineage(evidence FleetLifecycleCleanupEvidence, action Action, deploymentID, planHash string, transaction JournalEntry) error {
	if evidence.DeploymentID != deploymentID || evidence.PlanHash != planHash || evidence.ActionID != action.ID || evidence.IntentHash != action.IntentHash || transaction.Stage != StageFinalized || transaction.PlanHash != planHash || transaction.ActionID != action.ID || transaction.IntentHash != action.IntentHash || !strings.EqualFold(transaction.TransactionHash, evidence.TransactionHash) || transaction.BlockNumber != evidence.BlockNumber || !strings.EqualFold(transaction.BlockHash, evidence.BlockHash) || transaction.RecoveryBlock == 0 || transaction.RecoveryBlock >= transaction.BlockNumber || transaction.RecoveryBlockHash == "" {
		return errors.New("fleet lifecycle cleanup evidence has no exact approved finalized journal lineage")
	}
	return nil
}

func (self *Executor) validateFleetLifecycleCleanupAction(ctx context.Context, action Action, variantName string, memberIndex int, evidence FleetLifecycleCleanupEvidence) error {
	variant, err := fleetLifecycleVariantFor(variantName)
	if err != nil {
		return err
	}
	manifest, _, _, err := fleetLifecycleVariantManifest(self.cfg, self.stateDir, self.roles, variant)
	if err != nil || memberIndex < 1 || memberIndex > len(manifest.Members) {
		return stateMismatchError(err, "fleet lifecycle cleanup member is invalid")
	}
	if err := validateFleetLifecycleCleanupEvidence(evidence, manifest.Members[memberIndex-1], manifest.FleetID, manifest.Generation); err != nil {
		return err
	}
	wantActionID, err := fleetLifecycleCleanupActionID(variantName, memberIndex)
	if err != nil {
		return err
	}
	wantMiner := fleetMemberMinerIndex(self.cfg, variant.Fleet, memberIndex)
	if variant.Fallback {
		wantMiner, err = fleetLifecycleFallbackMinerIndex(self.cfg, memberIndex)
		if err != nil {
			return err
		}
	}
	if action.ID != wantActionID || action.Kind != "evm-transaction" || action.Target != fmt.Sprintf("miner:%d", wantMiner) {
		return errors.New("fleet lifecycle cleanup action differs from its exact source binding")
	}
	transaction, found := self.keeper.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash)
	if !found {
		return errors.New("fleet lifecycle cleanup journal lineage is absent")
	}
	if err := validateFleetLifecycleCleanupLineage(evidence, action, self.cfg.Config.Deployment.DeploymentID, self.plan.PlanHash, transaction); err != nil {
		return err
	}
	finalized, err := finalizedEVMHead(ctx, self.keeper.client)
	if err != nil {
		return err
	}
	receipt, err := verifyFinalizedEVMReceipt(ctx, self.keeper.client, finalized, evidence.TransactionHash, evidence.BlockNumber, evidence.BlockHash)
	if err != nil {
		return err
	}
	parent, err := self.keeper.client.HeaderByNumber(ctx, new(big.Int).SetUint64(evidence.BeforeBlock.Number))
	if err != nil {
		return err
	}
	if !strings.EqualFold(parent.Hash().Hex(), evidence.BeforeBlock.Hash) {
		return errors.New("fleet lifecycle cleanup parent block differs from the canonical chain")
	}
	member := manifest.Members[memberIndex-1]
	coordinator := stabi.NewSTCoordinator()
	address := common.BytesToAddress(manifest.Coordinator[:])
	beforeRecord, err := rawCoordinatorCallAt(ctx, self.keeper, address, coordinator.PackGetFleetBinding(member.ClientID), coordinator.UnpackGetFleetBinding, evidence.BlockNumber-1)
	if err != nil {
		return err
	}
	afterRecord, err := rawCoordinatorCallAt(ctx, self.keeper, address, coordinator.PackGetFleetBinding(member.ClientID), coordinator.UnpackGetFleetBinding, evidence.BlockNumber)
	if err != nil {
		return err
	}
	beforeCount, err := rawCoordinatorCallAt(ctx, self.keeper, address, coordinator.PackFleetMemberCount(manifest.FleetID), coordinator.UnpackFleetMemberCount, evidence.BlockNumber-1)
	if err != nil || !beforeCount.IsUint64() {
		return stateMismatchError(err, "fleet lifecycle historical pre-cleanup count is invalid")
	}
	afterCount, err := rawCoordinatorCallAt(ctx, self.keeper, address, coordinator.PackFleetMemberCount(manifest.FleetID), coordinator.UnpackFleetMemberCount, evidence.BlockNumber)
	if err != nil || !afterCount.IsUint64() {
		return stateMismatchError(err, "fleet lifecycle historical post-cleanup count is invalid")
	}
	if beforeRecord.Cleaned || beforeRecord.Generation != manifest.Generation || beforeRecord.FleetId != manifest.FleetID || beforeRecord.Hotkey != manifest.Hotkey || !afterRecord.Cleaned || afterRecord.CleanedAtEpoch != evidence.CleanedAtEpoch || beforeCount.Uint64() != evidence.MemberCountBefore || afterCount.Uint64() != evidence.MemberCountAfter {
		return errors.New("fleet lifecycle cleanup evidence differs from exact historical coordinator state")
	}
	events := 0
	for _, log := range receipt.Logs {
		if log == nil || log.Address != address {
			continue
		}
		event, eventErr := coordinator.UnpackFleetBindingCleanedEvent(log)
		if eventErr == nil && event.ClientId == member.ClientID && event.CleanedAtEpoch == evidence.CleanedAtEpoch {
			events++
		}
	}
	if events != 1 {
		return fmt.Errorf("fleet lifecycle cleanup receipt has %d exact events, want 1", events)
	}
	return nil
}

func (self *Executor) cleanupFleetLifecycleMember(ctx context.Context, action Action, variantName string, memberIndex int) error {
	variant, err := fleetLifecycleVariantFor(variantName)
	if err != nil {
		return err
	}
	manifest, _, _, err := fleetLifecycleVariantManifest(self.cfg, self.stateDir, self.roles, variant)
	if err != nil {
		return err
	}
	if memberIndex < 1 || memberIndex > len(manifest.Members) {
		return fmt.Errorf("fleet lifecycle cleanup member %d is out of range", memberIndex)
	}
	wantActionID, err := fleetLifecycleCleanupActionID(variantName, memberIndex)
	if err != nil {
		return err
	}
	wantMiner := fleetMemberMinerIndex(self.cfg, variant.Fleet, memberIndex)
	if variant.Fallback {
		wantMiner, err = fleetLifecycleFallbackMinerIndex(self.cfg, memberIndex)
		if err != nil {
			return err
		}
	}
	if action.ID != wantActionID || action.Kind != "evm-transaction" || action.Target != fmt.Sprintf("miner:%d", wantMiner) {
		return errors.New("fleet lifecycle cleanup action differs from its exact source binding")
	}
	member := manifest.Members[memberIndex-1]
	evidencePath := filepath.Join(self.stateDir, "public", fleetLifecycleCleanupEvidenceName(variantName, memberIndex))
	if existing, readErr := os.ReadFile(evidencePath); readErr == nil {
		var evidence FleetLifecycleCleanupEvidence
		if json.Unmarshal(existing, &evidence) != nil {
			return errors.New("fleet lifecycle cleanup evidence is invalid JSON")
		}
		return self.validateFleetLifecycleCleanupAction(ctx, action, variantName, memberIndex, evidence)
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	coordinator := stabi.NewSTCoordinator()
	address := common.BytesToAddress(manifest.Coordinator[:])
	data, err := coordinator.TryPackCleanupFleetBinding(member.ClientID)
	if err != nil {
		return err
	}
	receipt, err := self.keeper.Send(ctx, self.plan.PlanHash, action, &address, big.NewInt(0), data)
	if err != nil {
		return err
	}
	block := receipt.BlockNumber.Uint64()
	if block < 2 {
		return errors.New("fleet lifecycle cleanup receipt has no historical pre-state")
	}
	parent, err := self.keeper.client.HeaderByNumber(ctx, new(big.Int).SetUint64(block-1))
	if err != nil {
		return err
	}
	beforeRecord, err := rawCoordinatorCallAt(ctx, self.keeper, address, coordinator.PackGetFleetBinding(member.ClientID), coordinator.UnpackGetFleetBinding, block-1)
	if err != nil {
		return err
	}
	afterRecord, err := rawCoordinatorCallAt(ctx, self.keeper, address, coordinator.PackGetFleetBinding(member.ClientID), coordinator.UnpackGetFleetBinding, block)
	if err != nil {
		return err
	}
	beforeCount, err := rawCoordinatorCallAt(ctx, self.keeper, address, coordinator.PackFleetMemberCount(manifest.FleetID), coordinator.UnpackFleetMemberCount, block-1)
	if err != nil || !beforeCount.IsUint64() {
		return stateMismatchError(err, "fleet lifecycle pre-cleanup member count is invalid")
	}
	afterCount, err := rawCoordinatorCallAt(ctx, self.keeper, address, coordinator.PackFleetMemberCount(manifest.FleetID), coordinator.UnpackFleetMemberCount, block)
	if err != nil || !afterCount.IsUint64() {
		return stateMismatchError(err, "fleet lifecycle post-cleanup member count is invalid")
	}
	if beforeRecord.Cleaned || beforeRecord.Generation != manifest.Generation || beforeRecord.FleetId != manifest.FleetID || beforeRecord.Hotkey != manifest.Hotkey || afterRecord != beforeRecord && (!afterRecord.Cleaned || afterRecord.CleanedAtEpoch == 0) {
		return errors.New("fleet lifecycle cleanup binding transition is invalid")
	}
	if !afterRecord.Cleaned || afterRecord.CleanedAtEpoch == 0 || beforeCount.Uint64() != afterCount.Uint64()+1 {
		return errors.New("fleet lifecycle cleanup did not decrement the exact fleet member count")
	}
	cleanedEvents := 0
	for _, log := range receipt.Logs {
		if log == nil || log.Address != address {
			continue
		}
		event, eventErr := coordinator.UnpackFleetBindingCleanedEvent(log)
		if eventErr == nil && event.ClientId == member.ClientID && event.CleanedAtEpoch == afterRecord.CleanedAtEpoch {
			cleanedEvents++
		}
	}
	if cleanedEvents != 1 {
		return fmt.Errorf("fleet lifecycle cleanup emitted %d exact FleetBindingCleaned events, want 1", cleanedEvents)
	}
	evidence := FleetLifecycleCleanupEvidence{
		Schema: "urnetwork-sim-fleet-binding-cleanup-v2", DeploymentID: self.cfg.Config.Deployment.DeploymentID, PlanHash: self.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		ClientID: fleetLifecycleHex16(member.ClientID), FleetID: fleetLifecycleHex(manifest.FleetID), Generation: manifest.Generation,
		CleanedAtEpoch: afterRecord.CleanedAtEpoch, MemberCountBefore: beforeCount.Uint64(), MemberCountAfter: afterCount.Uint64(),
		TransactionHash: receipt.TxHash.Hex(), BeforeBlock: ChainHead{Number: block - 1, Hash: parent.Hash().Hex()}, BlockNumber: block, BlockHash: receipt.BlockHash.Hex(),
	}
	if err := self.validateFleetLifecycleCleanupAction(ctx, action, variantName, memberIndex, evidence); err != nil {
		return err
	}
	return writePublicJSON(evidencePath, evidence)
}

func (self *Executor) executeFleetLifecycleAction(ctx context.Context, action Action) error {
	if role, ok := fleetLifecycleFundingRole(action.ID); ok {
		return self.fundSubstrateRole(ctx, action, role)
	}
	switch {
	case action.ID == "lifecycle.prepare.target.commitment":
		return self.publishFleetLifecycleCommitment(ctx, action, fleetLifecycleVariantTargetTakeover)
	case action.ID == "lifecycle.prepare.target.mirror":
		return self.mirrorFleetLifecycleCommitment(ctx, action, fleetLifecycleVariantTargetTakeover)
	case strings.HasPrefix(action.ID, "lifecycle.prepare.target.bind."):
		return self.bindFleetLifecycleMember(ctx, action, fleetLifecycleVariantTargetTakeover, suffixInt(action.ID))
	case action.ID == "lifecycle.prepare.target.installed":
		return nil
	case action.ID == "lifecycle.prepare.companion.commitment":
		return self.publishFleetLifecycleCommitment(ctx, action, fleetLifecycleVariantCompanionTakeover)
	case action.ID == "lifecycle.prepare.companion.mirror":
		return self.mirrorFleetLifecycleCommitment(ctx, action, fleetLifecycleVariantCompanionTakeover)
	case strings.HasPrefix(action.ID, "lifecycle.prepare.companion.bind."):
		return self.bindFleetLifecycleMember(ctx, action, fleetLifecycleVariantCompanionTakeover, suffixInt(action.ID))
	case action.ID == "lifecycle.prepare.companion.installed":
		return nil
	case action.ID == "lifecycle.fallback.register":
		return self.registerFleetLifecycle(ctx, action, fleetLifecycleVariantFallback)
	case action.ID == "lifecycle.fallback.commitment":
		return self.publishFleetLifecycleCommitment(ctx, action, fleetLifecycleVariantFallback)
	case action.ID == "lifecycle.fallback.mirror":
		return self.mirrorFleetLifecycleCommitment(ctx, action, fleetLifecycleVariantFallback)
	case strings.HasPrefix(action.ID, "lifecycle.fallback.bind."):
		return self.bindFleetLifecycleMember(ctx, action, fleetLifecycleVariantFallback, suffixInt(action.ID))
	case action.ID == "lifecycle.fallback.installed":
		return nil
	case strings.HasPrefix(action.ID, "lifecycle.provider.cleanup."):
		return self.cleanupFleetLifecycleMember(ctx, action, fleetLifecycleVariantTargetTakeover, suffixInt(action.ID))
	case action.ID == "lifecycle.provider.register":
		return self.registerFleetLifecycle(ctx, action, fleetLifecycleVariantProvider)
	case action.ID == "lifecycle.provider.commitment":
		return self.publishFleetLifecycleCommitment(ctx, action, fleetLifecycleVariantProvider)
	case action.ID == "lifecycle.provider.mirror":
		return self.mirrorFleetLifecycleCommitment(ctx, action, fleetLifecycleVariantProvider)
	case strings.HasPrefix(action.ID, "lifecycle.provider.bind."):
		return self.bindFleetLifecycleMember(ctx, action, fleetLifecycleVariantProvider, suffixInt(action.ID))
	case action.ID == "lifecycle.provider.installed":
		return nil
	case strings.HasPrefix(action.ID, "lifecycle.terminal.cleanup-companion."):
		return self.cleanupFleetLifecycleMember(ctx, action, fleetLifecycleVariantCompanionTakeover, suffixInt(action.ID))
	case strings.HasPrefix(action.ID, "lifecycle.terminal.cleanup-fallback."):
		return self.cleanupFleetLifecycleMember(ctx, action, fleetLifecycleVariantFallback, suffixInt(action.ID))
	case action.ID == "lifecycle.terminal.register":
		return self.registerFleetLifecycle(ctx, action, fleetLifecycleVariantTerminal)
	case action.ID == "lifecycle.terminal.commitment":
		return self.publishFleetLifecycleCommitment(ctx, action, fleetLifecycleVariantTerminal)
	case action.ID == "lifecycle.terminal.mirror":
		return self.mirrorFleetLifecycleCommitment(ctx, action, fleetLifecycleVariantTerminal)
	case strings.HasPrefix(action.ID, "lifecycle.terminal.bind."):
		return self.bindFleetLifecycleMember(ctx, action, fleetLifecycleVariantTerminal, suffixInt(action.ID))
	case action.ID == "lifecycle.terminal.installed":
		return nil
	default:
		return fmt.Errorf("unsupported fleet lifecycle action %s", action.ID)
	}
}

func loadFleetLifecycleBindingEvidence(stateDir, variantName string, member int) (*FleetBindingEvidence, error) {
	var evidence FleetBindingEvidence
	if err := readJSONFile(filepath.Join(stateDir, "public", fleetLifecycleBindingEvidenceName(variantName, member)), &evidence); err != nil {
		return nil, err
	}
	if evidence.Schema != "urnetwork-fleet-binding-evidence-v1" || evidence.DeploymentID == "" || evidence.PlanHash == "" || evidence.ActionID == "" || evidence.IntentHash == "" || evidence.Generation == 0 || evidence.ValidFromEpoch == 0 || evidence.ValidToEpoch < evidence.ValidFromEpoch || evidence.BlockNumber == 0 {
		return nil, errors.New("fleet lifecycle binding evidence is incomplete")
	}
	for label, value := range map[string]string{
		"client id": evidence.ClientID, "client key": evidence.ClientKey, "fleet id": evidence.FleetID,
		"hotkey": evidence.Hotkey, "commitment": evidence.CommitmentHash, "digest": evidence.BindingDigest,
		"client signature": evidence.ClientSignature, "hotkey signature": evidence.HotkeySignature,
		"transaction": evidence.TransactionHash, "block": evidence.BlockHash,
	} {
		size := 32
		if label == "client id" {
			size = 16
		} else if strings.Contains(label, "signature") {
			size = 64
		}
		if raw, ok := evidenceFixedHex(value, size); !ok || len(raw) != size {
			return nil, fmt.Errorf("fleet lifecycle binding %s is invalid", label)
		}
	}
	return &evidence, nil
}

func validateFleetLifecycleBindingLineage(evidence FleetBindingEvidence, action Action, variantName string, member int, deploymentID, planHash string, transaction JournalEntry) error {
	wantActionID, err := fleetLifecycleBindingActionID(variantName, member)
	if err != nil {
		return err
	}
	if action.ID != wantActionID || action.Kind != "evm-transaction" {
		return errors.New("fleet lifecycle binding action differs from its exact variant and member")
	}
	if evidence.DeploymentID != deploymentID || evidence.PlanHash != planHash || evidence.ActionID != action.ID || evidence.IntentHash != action.IntentHash || transaction.Stage != StageFinalized || transaction.PlanHash != planHash || transaction.ActionID != action.ID || transaction.IntentHash != action.IntentHash || !strings.EqualFold(transaction.TransactionHash, evidence.TransactionHash) || transaction.BlockNumber != evidence.BlockNumber || !strings.EqualFold(transaction.BlockHash, evidence.BlockHash) || transaction.RecoveryBlock == 0 || transaction.RecoveryBlock >= transaction.BlockNumber || transaction.RecoveryBlockHash == "" {
		return errors.New("fleet lifecycle binding has no exact approved finalized journal lineage")
	}
	return nil
}

func (self *Executor) verifyFleetLifecycleBindingAt(ctx context.Context, variantName string, member int, evmHead ChainHead) (*FleetBindingEvidence, error) {
	variant, err := fleetLifecycleVariantFor(variantName)
	if err != nil {
		return nil, err
	}
	manifest, commitmentHash, _, err := self.fleetLifecycleManifestAndCommitment(variantName)
	if err != nil {
		return nil, err
	}
	if member < 1 || member > len(manifest.Members) || evmHead.Number == 0 {
		return nil, errors.New("fleet lifecycle binding verification inputs are incomplete")
	}
	evidence, err := loadFleetLifecycleBindingEvidence(self.stateDir, variantName, member)
	if err != nil {
		return nil, err
	}
	manifestMember := manifest.Members[member-1]
	clientIDRaw, _ := evidenceFixedHex(evidence.ClientID, 16)
	clientKeyRaw, _ := evidenceFixedHex(evidence.ClientKey, 32)
	fleetIDRaw, _ := evidenceFixedHex(evidence.FleetID, 32)
	hotkeyRaw, _ := evidenceFixedHex(evidence.Hotkey, 32)
	commitmentRaw, _ := evidenceFixedHex(evidence.CommitmentHash, 32)
	digestRaw, _ := evidenceFixedHex(evidence.BindingDigest, 32)
	clientSignature, _ := evidenceFixedHex(evidence.ClientSignature, 64)
	hotkeySignature, _ := evidenceFixedHex(evidence.HotkeySignature, 64)
	var binding protocol.FleetBinding
	binding.ChainID, binding.Netuid, binding.Coordinator = manifest.ChainID, manifest.Netuid, manifest.Coordinator
	copy(binding.ClientID[:], clientIDRaw)
	copy(binding.ClientKey[:], clientKeyRaw)
	copy(binding.FleetID[:], fleetIDRaw)
	copy(binding.Hotkey[:], hotkeyRaw)
	copy(binding.CommitmentHash[:], commitmentRaw)
	binding.Generation, binding.ValidFromEpoch, binding.ValidToEpoch = evidence.Generation, evidence.ValidFromEpoch, evidence.ValidToEpoch
	digest, err := binding.Digest()
	if err != nil || binding.ClientID != manifestMember.ClientID || binding.ClientKey != manifestMember.ClientKey || binding.FleetID != manifest.FleetID || binding.Hotkey != manifest.Hotkey || binding.CommitmentHash != commitmentHash || !bytes.Equal(digest[:], digestRaw) || !binding.VerifyClient(clientSignature) || !binding.VerifyHotkey(hotkeySignature) {
		return nil, stateMismatchError(err, "fleet lifecycle binding cryptographic evidence differs from its manifest")
	}
	actionID, err := fleetLifecycleBindingActionID(variantName, member)
	if err != nil {
		return nil, err
	}
	action, err := self.planAction(actionID)
	if err != nil {
		return nil, err
	}
	wantMiner := fleetMemberMinerIndex(self.cfg, variant.Fleet, member)
	if variant.Fallback {
		wantMiner, err = fleetLifecycleFallbackMinerIndex(self.cfg, member)
		if err != nil {
			return nil, err
		}
	}
	if action.Target != fmt.Sprintf("miner:%d", wantMiner) {
		return nil, errors.New("fleet lifecycle binding action targets another client")
	}
	transaction, found := self.keeper.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash)
	if !found {
		return nil, errors.New("fleet lifecycle binding journal lineage is absent")
	}
	if err := validateFleetLifecycleBindingLineage(*evidence, action, variantName, member, self.cfg.Config.Deployment.DeploymentID, self.plan.PlanHash, transaction); err != nil {
		return nil, err
	}
	finalized, err := finalizedEVMHead(ctx, self.keeper.client)
	if err != nil {
		return nil, err
	}
	if evmHead.Number < evidence.BlockNumber || finalized.Number < evidence.BlockNumber {
		return nil, errors.New("fleet lifecycle binding inclusion is not finalized at the requested verification head")
	}
	receipt, err := verifyFinalizedEVMReceipt(ctx, self.keeper.client, finalized, evidence.TransactionHash, evidence.BlockNumber, evidence.BlockHash)
	if err != nil {
		return nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	address := common.BytesToAddress(manifest.Coordinator[:])
	post, err := rawCoordinatorCallAt(ctx, self.keeper, address, coordinator.PackBindingAt(binding.ClientID, new(big.Int).SetUint64(binding.ValidFromEpoch)), coordinator.UnpackBindingAt, evidence.BlockNumber)
	if err != nil || !post.Active || post.Record.FleetId != binding.FleetID || post.Record.Hotkey != binding.Hotkey || post.Record.ClientKey != binding.ClientKey || post.Record.CommitmentHash != binding.CommitmentHash || post.Record.Generation != binding.Generation || post.Record.ValidFromEpoch != binding.ValidFromEpoch || post.Record.ValidToEpoch != binding.ValidToEpoch || post.Record.Uid != evidence.UID {
		return nil, stateMismatchError(err, "fleet lifecycle binding historical coordinator state mismatch")
	}
	events := 0
	for _, log := range receipt.Logs {
		if log == nil || log.Address != address {
			continue
		}
		event, eventErr := coordinator.UnpackFleetBoundEvent(log)
		if eventErr == nil && event.ClientId == binding.ClientID && event.FleetId == binding.FleetID && event.Hotkey == binding.Hotkey && event.Uid == evidence.UID && event.Generation == binding.Generation && event.ValidFromEpoch == binding.ValidFromEpoch && event.ValidToEpoch == binding.ValidToEpoch {
			events++
		}
	}
	if events != 1 {
		return nil, fmt.Errorf("fleet lifecycle binding receipt has %d exact FleetBound events, want 1", events)
	}
	return evidence, nil
}

func fleetLifecycleVariantFromAction(actionID string) (string, error) {
	switch {
	case strings.HasPrefix(actionID, "lifecycle.prepare.target."):
		return fleetLifecycleVariantTargetTakeover, nil
	case strings.HasPrefix(actionID, "lifecycle.prepare.companion."):
		return fleetLifecycleVariantCompanionTakeover, nil
	case strings.HasPrefix(actionID, "lifecycle.fallback."):
		return fleetLifecycleVariantFallback, nil
	case strings.HasPrefix(actionID, "lifecycle.provider."):
		return fleetLifecycleVariantProvider, nil
	case strings.HasPrefix(actionID, "lifecycle.terminal."):
		return fleetLifecycleVariantTerminal, nil
	default:
		return "", fmt.Errorf("action %s has no fleet lifecycle variant", actionID)
	}
}

func fleetLifecycleCleanupVariantFromAction(actionID string) (string, error) {
	switch {
	case strings.HasPrefix(actionID, "lifecycle.provider.cleanup."):
		return fleetLifecycleVariantTargetTakeover, nil
	case strings.HasPrefix(actionID, "lifecycle.terminal.cleanup-companion."):
		return fleetLifecycleVariantCompanionTakeover, nil
	case strings.HasPrefix(actionID, "lifecycle.terminal.cleanup-fallback."):
		return fleetLifecycleVariantFallback, nil
	default:
		return "", fmt.Errorf("action %s has no fleet lifecycle cleanup source", actionID)
	}
}

func (self *Executor) verifyFleetLifecyclePostcondition(ctx context.Context, action Action, evmHead ChainHead, state map[string]any) (map[string]any, error) {
	if role, ok := fleetLifecycleFundingRole(action.ID); ok {
		return self.verifySubstrateFunding(action, role, state)
	}
	switch {
	case action.ID == "lifecycle.fallback.register" || action.ID == "lifecycle.provider.register" || action.ID == "lifecycle.terminal.register":
		variantName := fleetLifecycleVariantFallback
		if action.ID == "lifecycle.provider.register" {
			variantName = fleetLifecycleVariantProvider
		} else if action.ID == "lifecycle.terminal.register" {
			variantName = fleetLifecycleVariantTerminal
		}
		_, evidenceName := fleetLifecycleRegistrationNames(variantName)
		var evidence FleetLifecycleRegistrationEvidence
		if err := readJSONFile(filepath.Join(self.stateDir, "public", evidenceName), &evidence); err != nil {
			return nil, err
		}
		if err := self.validateFleetLifecycleRegistrationAction(ctx, action, variantName, evidence); err != nil {
			return nil, err
		}
		state["victim_uid"], state["replacement_hotkey"], state["transaction_hash"] = evidence.VictimUID, evidence.ReplacementHotkey, evidence.TransactionHash
		return state, nil
	case strings.HasSuffix(action.ID, ".commitment"):
		variantName, variantErr := fleetLifecycleVariantFromAction(action.ID)
		if variantErr != nil {
			return nil, variantErr
		}
		manifest, hash, evidence, err := self.fleetLifecycleManifestAndCommitment(variantName)
		if err != nil {
			return nil, err
		}
		if err := self.validateFleetLifecycleCommitmentAction(ctx, action, variantName, manifest, hash, *evidence); err != nil {
			return nil, err
		}
		state["commitment_hash"], state["commitment_block"] = fleetLifecycleHex(hash), evidence.CommitmentBlock
		return state, nil
	case strings.HasSuffix(action.ID, ".mirror"):
		variantName, variantErr := fleetLifecycleVariantFromAction(action.ID)
		if variantErr != nil {
			return nil, variantErr
		}
		manifest, hash, evidence, err := self.fleetLifecycleManifestAndCommitment(variantName)
		if err != nil {
			return nil, err
		}
		finalizedHash, err := decodeHex32("fleet lifecycle mirror block", evidence.FinalizedBlockHash)
		if err != nil {
			return nil, err
		}
		coordinator := stabi.NewSTCoordinator()
		address := common.BytesToAddress(manifest.Coordinator[:])
		mirror, err := rawCoordinatorCallAt(ctx, self.oracle, address, coordinator.PackMirroredCommitments(manifest.Hotkey), coordinator.UnpackMirroredCommitments, evmHead.Number)
		if err != nil || !fleetMirrorMatches(mirror, hash, evidence.FinalizedBlock, finalizedHash) {
			return nil, stateMismatchError(err, "fleet lifecycle mirror postcondition mismatch")
		}
		var mirrorEvidence FleetLifecycleMirrorEvidence
		if err := readJSONFile(filepath.Join(self.stateDir, "public", fleetLifecycleMirrorEvidenceName(variantName)), &mirrorEvidence); err != nil {
			return nil, err
		}
		if err := self.validateFleetLifecycleMirrorAction(ctx, action, variantName, manifest, hash, *evidence, mirrorEvidence); err != nil {
			return nil, err
		}
		state["hotkey"], state["commitment_hash"] = fleetLifecycleHex(manifest.Hotkey), fleetLifecycleHex(hash)
		return state, nil
	case strings.Contains(action.ID, ".bind."):
		variantName, variantErr := fleetLifecycleVariantFromAction(action.ID)
		if variantErr != nil {
			return nil, variantErr
		}
		evidence, err := self.verifyFleetLifecycleBindingAt(ctx, variantName, suffixInt(action.ID), evmHead)
		if err != nil {
			return nil, err
		}
		state["client_id"], state["uid"], state["valid_from_epoch"] = evidence.ClientID, evidence.UID, evidence.ValidFromEpoch
		return state, nil
	case strings.Contains(action.ID, ".cleanup.") || strings.Contains(action.ID, ".cleanup-"):
		variantName, variantErr := fleetLifecycleCleanupVariantFromAction(action.ID)
		if variantErr != nil {
			return nil, variantErr
		}
		memberIndex := suffixInt(action.ID)
		variant, _ := fleetLifecycleVariantFor(variantName)
		manifest, _, _, err := fleetLifecycleVariantManifest(self.cfg, self.stateDir, self.roles, variant)
		if err != nil || memberIndex < 1 || memberIndex > len(manifest.Members) {
			return nil, stateMismatchError(err, "fleet lifecycle cleanup member is invalid")
		}
		var evidence FleetLifecycleCleanupEvidence
		if err := readJSONFile(filepath.Join(self.stateDir, "public", fleetLifecycleCleanupEvidenceName(variantName, memberIndex)), &evidence); err != nil {
			return nil, err
		}
		if err := self.validateFleetLifecycleCleanupAction(ctx, action, variantName, memberIndex, evidence); err != nil {
			return nil, err
		}
		state["client_id"], state["cleaned_at_epoch"], state["member_count_after"] = evidence.ClientID, evidence.CleanedAtEpoch, evidence.MemberCountAfter
		return state, nil
	case strings.HasSuffix(action.ID, ".installed"):
		variantName, variantErr := fleetLifecycleVariantFromAction(action.ID)
		if variantErr != nil {
			return nil, variantErr
		}
		var effective uint64
		for member := 1; member <= self.cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			evidence, err := self.verifyFleetLifecycleBindingAt(ctx, variantName, member, evmHead)
			if err != nil {
				return nil, err
			}
			if effective == 0 {
				effective = evidence.ValidFromEpoch
			} else if effective != evidence.ValidFromEpoch {
				return nil, errors.New("fleet lifecycle bindings do not share one effective epoch")
			}
		}
		state["variant"], state["effective_epoch"], state["member_count"] = variantName, effective, self.cfg.Config.Topology.ClientsPerHeadFleet
		return state, nil
	default:
		return nil, fmt.Errorf("unsupported fleet lifecycle postcondition %s", action.ID)
	}
}

// FleetLifecyclePruneSnapshot reads the complete auction at a single finalized
// state root. It deliberately does not compose latest-value convenience calls.
func (self *SubstrateManager) FleetLifecyclePruneSnapshot() (FleetLifecyclePruneSnapshot, error) {
	if self == nil || self.chain == nil || self.chain.Meta == nil {
		return FleetLifecyclePruneSnapshot{}, errors.New("fleet lifecycle substrate reader is unavailable")
	}
	finalized, block, err := self.finalizedHead()
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, err
	}
	return self.fleetLifecyclePruneSnapshotAt(finalized, block)
}

func (self *SubstrateManager) fleetLifecyclePruneSnapshotAt(finalized types.Hash, block uint64) (FleetLifecyclePruneSnapshot, error) {
	if self == nil || self.chain == nil || self.chain.Meta == nil || finalized == (types.Hash{}) || block == 0 {
		return FleetLifecyclePruneSnapshot{}, errors.New("fleet lifecycle exact substrate reader is unavailable")
	}
	historical, err := self.releaseHistoryChainAt(finalized)
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, fmt.Errorf("authenticate fleet lifecycle prune history at %d/%s: %w", block, finalized.Hex(), err)
	}
	header, err := historical.API.RPC.Chain.GetHeader(finalized)
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, fmt.Errorf("read fleet lifecycle prune header at %s: %w", finalized.Hex(), err)
	}
	if header == nil {
		return FleetLifecyclePruneSnapshot{}, fmt.Errorf("fleet lifecycle prune header at %s is unavailable", finalized.Hex())
	}
	if uint64(header.Number) != block {
		return FleetLifecyclePruneSnapshot{}, fmt.Errorf("fleet lifecycle prune hash %s is block %d, want %d", finalized.Hex(), header.Number, block)
	}
	topology, err := readSubnetTopologyAt(historical, self.cfg.Netuid, finalized)
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, err
	}
	facts, err := readExistingUIDFactsAt(historical, self.cfg.Netuid, finalized, topology)
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, err
	}
	readU16 := func(storage string) (uint16, error) {
		key, keyErr := types.CreateStorageKey(historical.Meta, crv4.PalletName, storage, netuidArg(self.cfg.Netuid))
		if keyErr != nil {
			return 0, keyErr
		}
		var value types.U16
		if readErr := readRequiredStorageAt(historical, key, crv4.PalletName, storage, &value, finalized); readErr != nil {
			return 0, readErr
		}
		return uint16(value), nil
	}
	immunity, err := readU16("ImmunityPeriod")
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, err
	}
	minimumNonImmune, err := readU16("MinNonImmuneUids")
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, err
	}
	maximum, err := readU16("MaxAllowedUids")
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, err
	}
	emissionKey, err := types.CreateStorageKey(historical.Meta, crv4.PalletName, "Emission", netuidArg(self.cfg.Netuid))
	if err != nil {
		return FleetLifecyclePruneSnapshot{}, err
	}
	var emissions []types.U64
	if _, err := readStorageAt(historical, emissionKey, crv4.PalletName, "Emission", &emissions, finalized); err != nil {
		return FleetLifecyclePruneSnapshot{}, err
	}
	if len(facts) != int(topology.UIDCount) {
		return FleetLifecyclePruneSnapshot{}, fmt.Errorf("fleet lifecycle UID census=%d, want %d", len(facts), topology.UIDCount)
	}
	rows := make([]runtime453PruneNeuron, 0, len(facts))
	result := FleetLifecyclePruneSnapshot{
		Head: ChainHead{Number: block, Hash: finalized.Hex()}, UIDCount: topology.UIDCount, MaximumUIDs: maximum,
		ImmunityPeriodBlocks: immunity, MinimumNonImmuneUIDs: minimumNonImmune,
		Inputs: make([]FleetLifecyclePruneInput, 0, len(facts)),
	}
	for _, fact := range facts {
		hotkey, decodeErr := decodeHex32("fleet lifecycle hotkey", fact.Hotkey)
		if decodeErr != nil {
			return FleetLifecyclePruneSnapshot{}, decodeErr
		}
		emission := uint64(0)
		if int(fact.UID) < len(emissions) {
			emission = uint64(emissions[fact.UID])
		}
		age := uint64(0)
		if block >= fact.RegistrationBlock {
			age = block - fact.RegistrationBlock
		}
		immune := age < uint64(immunity)
		if !immune && !fact.SubnetOwner {
			result.NonImmuneUIDs++
		}
		rows = append(rows, runtime453PruneNeuron{
			UID: fact.UID, Hotkey: hotkey, EmissionRao: emission, RegistrationBlock: fact.RegistrationBlock,
			Immune: immune, Immortal: fact.SubnetOwner,
		})
		result.Inputs = append(result.Inputs, FleetLifecyclePruneInput{
			UID: fact.UID, Hotkey: fact.Hotkey, Coldkey: fact.Coldkey, EmissionRao: emission,
			RegistrationBlock: fact.RegistrationBlock, Immune: immune, Immortal: fact.SubnetOwner,
		})
	}
	result.RuntimePruneUID, err = runtime453PruneCandidate(rows, minimumNonImmune)
	return result, err
}

func validateFleetLifecyclePruneSnapshot(snapshot FleetLifecyclePruneSnapshot, targetHotkey, targetColdkey [32]byte) error {
	if snapshot.Head.Number == 0 || snapshot.Head.Hash == "" || snapshot.UIDCount == 0 || snapshot.UIDCount != snapshot.MaximumUIDs || len(snapshot.Inputs) != int(snapshot.UIDCount) {
		return errors.New("fleet lifecycle prune snapshot has an incomplete head or UID census")
	}
	if _, err := decodeHex32("fleet lifecycle prune head", snapshot.Head.Hash); err != nil {
		return err
	}
	rows := make([]runtime453PruneNeuron, 0, len(snapshot.Inputs))
	var target *FleetLifecyclePruneInput
	var nonImmune uint16
	for index := range snapshot.Inputs {
		input := &snapshot.Inputs[index]
		if int(input.UID) != index {
			return fmt.Errorf("fleet lifecycle prune census UID %d is out of canonical order at %d", input.UID, index)
		}
		hotkey, err := decodeHex32("fleet lifecycle prune hotkey", input.Hotkey)
		if err != nil {
			return err
		}
		coldkey, err := decodeHex32("fleet lifecycle prune coldkey", input.Coldkey)
		if err != nil {
			return err
		}
		if !input.Immune && !input.Immortal {
			nonImmune++
		}
		rows = append(rows, runtime453PruneNeuron{UID: input.UID, Hotkey: hotkey, EmissionRao: input.EmissionRao, RegistrationBlock: input.RegistrationBlock, Immune: input.Immune, Immortal: input.Immortal})
		if hotkey == targetHotkey {
			if target != nil || coldkey != targetColdkey {
				return errors.New("fleet lifecycle prune target has duplicate or foreign ownership")
			}
			target = input
		}
	}
	computed, err := runtime453PruneCandidate(rows, snapshot.MinimumNonImmuneUIDs)
	if err != nil {
		return err
	}
	if target == nil || target.EmissionRao != 0 || target.UID != computed || computed != snapshot.RuntimePruneUID || nonImmune != snapshot.NonImmuneUIDs {
		return fmt.Errorf("fleet lifecycle target/prune mismatch target=%v computed=%d recorded=%d nonimmune=%d/%d", target, computed, snapshot.RuntimePruneUID, nonImmune, snapshot.NonImmuneUIDs)
	}
	return nil
}

// validateFleetLifecycleLaunchSnapshot binds the campaign to the exact live
// post-setup topology observed on public testnet. Churn identities 1-5 were
// consumed by setup replacements; accepting the former role map would make a
// purported lifecycle exercise act on a non-existent UID.
func validateFleetLifecycleLaunchSnapshot(snapshot FleetLifecyclePruneSnapshot, roles *RoleSecrets) error {
	if roles == nil {
		return errors.New("fleet lifecycle launch roles are unavailable")
	}
	byHotkey := make(map[string]FleetLifecyclePruneInput, len(snapshot.Inputs))
	for _, input := range snapshot.Inputs {
		byHotkey[strings.ToLower(input.Hotkey)] = input
	}
	for churn := 1; churn <= 5; churn++ {
		hotkey, err := roleBytes32(roles, churnHotkeyLabel(churn))
		if err != nil {
			return err
		}
		if _, live := byHotkey[strings.ToLower(fleetLifecycleHex(hotkey))]; live {
			return fmt.Errorf("stale fleet lifecycle role map: already-consumed churn-%d is unexpectedly live", churn)
		}
	}
	for _, expected := range []struct {
		churn int
		uid   uint16
	}{{fleetLifecycleTargetChurn, fleetLifecycleTargetExpectedUID}, {fleetLifecycleCompanionChurn, fleetLifecycleCompanionExpectedUID}, {fleetLifecycleTerminalVictimChurn, fleetLifecycleTerminalVictimUID}} {
		hotkey, err := roleBytes32(roles, churnHotkeyLabel(expected.churn))
		if err != nil {
			return err
		}
		coldkey, err := roleBytes32(roles, churnColdkeyLabel(expected.churn))
		if err != nil {
			return err
		}
		row, live := byHotkey[strings.ToLower(fleetLifecycleHex(hotkey))]
		if !live || row.UID != expected.uid || !strings.EqualFold(row.Coldkey, fleetLifecycleHex(coldkey)) {
			return fmt.Errorf("fleet lifecycle churn-%d live binding=%+v present=%t, want UID=%d and exact owner", expected.churn, row, live, expected.uid)
		}
	}
	targetHotkey, err := roleBytes32(roles, churnHotkeyLabel(fleetLifecycleTargetChurn))
	if err != nil {
		return err
	}
	targetColdkey, err := roleBytes32(roles, churnColdkeyLabel(fleetLifecycleTargetChurn))
	if err != nil {
		return err
	}
	return validateFleetLifecyclePruneSnapshot(snapshot, targetHotkey, targetColdkey)
}

func fleetLifecycleHex(value [32]byte) string {
	return "0x" + hex.EncodeToString(value[:])
}
