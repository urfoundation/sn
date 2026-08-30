package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/urfoundation/sn/protocol"
)

type AnalysisConservation struct {
	CapturedRao               string `json:"captured_rao"`
	PaidRao                   string `json:"paid_rao"`
	EscrowAccountedRao        string `json:"escrow_accounted_rao"`
	PendingFundingRao         string `json:"pending_funding_rao"`
	OutstandingRao            string `json:"outstanding_liability_rao"`
	PaidPlusEscrowRao         string `json:"paid_plus_escrow_rao"`
	PendingPlusOutstandingRao string `json:"pending_plus_outstanding_rao"`
	DeltaRao                  string `json:"captured_minus_paid_plus_escrow_rao"`
	EscrowDeltaRao            string `json:"escrow_minus_pending_plus_outstanding_rao"`
	LiveEscrowStakeRao        string `json:"live_escrow_stake_rao"`
	EscrowBackingDeltaRao     string `json:"escrow_backing_delta_rao"`
	Holds                     bool   `json:"holds"`
	ReservePrincipalRao       string `json:"reserve_principal_rao"`
	ReserveLiveStakeRao       string `json:"reserve_live_stake_rao"`
	ReserveBackingDeltaRao    string `json:"reserve_backing_delta_rao"`
}

type AnalysisOperator struct {
	NoID                       uint64 `json:"no_id"`
	Active                     bool   `json:"active"`
	PoolUID                    uint16 `json:"pool_uid"`
	PoolLive                   bool   `json:"pool_live"`
	ConvictionRao              string `json:"conviction_rao"`
	CarryRao                   string `json:"carry_rao"`
	StatsRows                  int    `json:"stats_rows"`
	ProofRows                  int    `json:"proof_rows"`
	ValidArtifacts             int    `json:"valid_artifacts"`
	MatchingArtifacts          int    `json:"matching_onchain_artifacts"`
	ExpectedFinalizedArtifacts int    `json:"expected_finalized_artifacts"`
	FinalizedClaims            int    `json:"finalized_claims"`
	UncertainClaims            int    `json:"uncertain_claims"`
}

type AnalysisEpochOperator struct {
	Epoch                  uint64 `json:"epoch"`
	NoID                   uint64 `json:"no_id"`
	DepositRao             string `json:"deposit_rao"`
	ConvictionAddedRao     string `json:"conviction_added_rao"`
	PreEpochConvictionRao  string `json:"pre_epoch_conviction_rao,omitempty"`
	TierMinConvictionRao   string `json:"tier_min_conviction_rao,omitempty"`
	RateNumeratorRaoPerGiB string `json:"rate_numerator_rao_per_gib,omitempty"`
	RateDenominator        string `json:"rate_denominator,omitempty"`
	ImpliedUsageBytesFloor string `json:"implied_usage_bytes_floor,omitempty"`
	PayoutRoot             string `json:"payout_root"`
	ArtifactHash           string `json:"artifact_hash"`
	FundedRao              string `json:"funded_rao"`
	TotalRao               string `json:"total_rao"`
	ClaimedRao             string `json:"claimed_rao"`
	CarryRao               string `json:"carry_rao,omitempty"`
	ExpiryBlock            uint64 `json:"expiry_block"`
	Status                 uint8  `json:"status"`
}

type AnalysisValidator struct {
	ValidatorID      int      `json:"validator_id"`
	CurrentStatus    string   `json:"current_status"`
	CurrentEpoch     uint64   `json:"current_epoch"`
	VectorHash       string   `json:"vector_hash"`
	ValuesHash       string   `json:"values_hash"`
	FinalizedIntents int      `json:"finalized_intents"`
	AppliedIntents   int      `json:"applied_intents"`
	IntentHashes     []string `json:"intent_hashes"`
}

type AnalysisReport struct {
	Schema                      string                  `json:"schema"`
	Release                     string                  `json:"release"`
	DeploymentID                string                  `json:"deployment_id"`
	ChainID                     uint64                  `json:"chain_id"`
	GenesisHash                 string                  `json:"genesis_hash"`
	Netuid                      uint16                  `json:"netuid"`
	ConfigHash                  string                  `json:"config_hash"`
	PolicyHash                  string                  `json:"policy_hash"`
	FinalizedHead               ChainHead               `json:"finalized_head"`
	CurrentEpoch                uint64                  `json:"current_epoch"`
	RuntimeCodeMatches          bool                    `json:"runtime_code_matches"`
	PolicyHashMatches           bool                    `json:"policy_hash_matches"`
	Conservation                AnalysisConservation    `json:"conservation"`
	Operators                   []AnalysisOperator      `json:"operators"`
	EpochOperators              []AnalysisEpochOperator `json:"epoch_operators"`
	Validators                  []AnalysisValidator     `json:"validators"`
	FleetCommitmentValid        bool                    `json:"fleet_commitment_valid"`
	FleetBindingsValid          bool                    `json:"fleet_bindings_valid"`
	FleetBindingCount           int                     `json:"fleet_binding_count"`
	PublicIdentitiesValid       bool                    `json:"public_identities_valid"`
	ReserveValidatorRegistered  bool                    `json:"reserve_validator_registered"`
	ReserveValidatorUID         uint16                  `json:"reserve_validator_uid"`
	ReserveDelegateTake         *uint16                 `json:"reserve_delegate_take,omitempty"`
	EscrowHotkeyRegistered      bool                    `json:"escrow_hotkey_registered"`
	EscrowHotkeyUID             uint16                  `json:"escrow_hotkey_uid"`
	TierReconstructionComplete  bool                    `json:"tier_reconstruction_complete"`
	VoluntaryConvictionObserved bool                    `json:"voluntary_conviction_observed"`
	ConvictionTierCrossed       bool                    `json:"conviction_tier_crossed"`
	ObservationHash             string                  `json:"observation_hash"`
	Discrepancies               []string                `json:"discrepancies"`
}

func decimal(value string) *big.Int {
	n, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return new(big.Int)
	}
	return n
}

func depositTierAt(tiers []protocol.DepositTier, conviction *big.Int) (protocol.DepositTier, bool) {
	if conviction == nil || conviction.Sign() < 0 || len(tiers) == 0 {
		return protocol.DepositTier{}, false
	}
	selected := tiers[0]
	for _, tier := range tiers {
		if conviction.Cmp(new(big.Int).SetUint64(tier.MinConvictionRao)) < 0 {
			break
		}
		selected = tier
	}
	return selected, selected.RateNumeratorRaoPerGiB != 0 && selected.RateDenominator != 0
}

func impliedUsageBytesFloor(deposit *big.Int, tier protocol.DepositTier) string {
	if deposit == nil || deposit.Sign() < 0 || tier.RateNumeratorRaoPerGiB == 0 || tier.RateDenominator == 0 {
		return ""
	}
	n := new(big.Int).Mul(deposit, new(big.Int).SetUint64(tier.RateDenominator))
	n.Mul(n, new(big.Int).Lsh(big.NewInt(1), 30))
	n.Div(n, new(big.Int).SetUint64(tier.RateNumeratorRaoPerGiB))
	return n.String()
}

func analyzeScenarioObservation(cfg *ResolvedConfig, observation *ScenarioObservation) *AnalysisReport {
	report := &AnalysisReport{Schema: "urnetwork-sim-analysis-v1", Release: "1.0", DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash}
	if observation == nil {
		report.Discrepancies = append(report.Discrepancies, "scenario observation is missing")
		return report
	}
	report.ObservationHash = observation.ObservationHash
	report.FleetCommitmentValid = observation.FleetCommitmentValid
	report.FleetBindingsValid = observation.FleetBindingsValid
	report.FleetBindingCount = observation.FleetBindingCount
	report.PublicIdentitiesValid = observation.PublicIdentitiesValid
	report.ReserveValidatorRegistered = observation.ReserveValidatorRegistered
	report.ReserveValidatorUID = observation.ReserveValidatorUID
	report.ReserveDelegateTake = observation.ReserveDelegateTake
	report.EscrowHotkeyRegistered = observation.EscrowHotkeyRegistered
	report.EscrowHotkeyUID = observation.EscrowHotkeyUID
	if observation.VoluntaryConvictionValid && observation.VoluntaryConviction != nil {
		report.VoluntaryConvictionObserved = true
		before := decimal(observation.VoluntaryConviction.BeforeConvictionRao)
		after := decimal(observation.VoluntaryConviction.AfterConvictionRao)
		beforeTier, beforeOK := depositTierAt(cfg.Policy.Deposit.Tiers, before)
		afterTier, afterOK := depositTierAt(cfg.Policy.Deposit.Tiers, after)
		if beforeOK && afterOK && beforeTier.MinConvictionRao != afterTier.MinConvictionRao {
			report.ConvictionTierCrossed = true
		}
	} else if observation.VoluntaryConvictionError != "" {
		report.Discrepancies = append(report.Discrepancies, "voluntary conviction: "+observation.VoluntaryConvictionError)
	}
	if observation.NativeCustodyError != "" {
		report.Discrepancies = append(report.Discrepancies, "native custody roles: "+observation.NativeCustodyError)
	}
	if observation.Status == nil || observation.Status.Contracts == nil {
		report.Discrepancies = append(report.Discrepancies, "finalized contract view is missing")
		return report
	}
	contracts := observation.Status.Contracts
	report.FinalizedHead = contracts.FinalizedHead
	report.CurrentEpoch = contracts.CurrentEpoch
	report.RuntimeCodeMatches = contracts.RuntimeCodeMatches
	report.PolicyHashMatches = strings.EqualFold(contracts.PolicyHash, cfg.PolicyHash)
	captured := decimal(contracts.TotalCaptured)
	paid := decimal(contracts.TotalPaid)
	escrow := decimal(contracts.EscrowAccounted)
	pending := decimal(contracts.PendingFunding)
	outstanding := decimal(contracts.Outstanding)
	liveEscrow := decimal(contracts.LiveEscrowStake)
	paidPlusEscrow := new(big.Int).Add(new(big.Int).Set(paid), escrow)
	pendingPlusOutstanding := new(big.Int).Add(new(big.Int).Set(pending), outstanding)
	principal := decimal(contracts.ReservePrincipal)
	liveStake := decimal(contracts.ReserveLiveStake)
	report.Conservation = AnalysisConservation{CapturedRao: captured.String(), PaidRao: paid.String(), EscrowAccountedRao: escrow.String(), PendingFundingRao: pending.String(), OutstandingRao: outstanding.String(), PaidPlusEscrowRao: paidPlusEscrow.String(), PendingPlusOutstandingRao: pendingPlusOutstanding.String(), DeltaRao: new(big.Int).Sub(new(big.Int).Set(captured), paidPlusEscrow).String(), EscrowDeltaRao: new(big.Int).Sub(new(big.Int).Set(escrow), pendingPlusOutstanding).String(), LiveEscrowStakeRao: liveEscrow.String(), EscrowBackingDeltaRao: new(big.Int).Sub(new(big.Int).Set(liveEscrow), escrow).String(), Holds: contracts.ConservationHolds && captured.Cmp(paidPlusEscrow) == 0 && escrow.Cmp(pendingPlusOutstanding) == 0 && liveEscrow.Cmp(escrow) >= 0, ReservePrincipalRao: principal.String(), ReserveLiveStakeRao: liveStake.String(), ReserveBackingDeltaRao: new(big.Int).Sub(new(big.Int).Set(liveStake), principal).String()}

	operatorEvidence := map[uint64]OperatorObservation{}
	for _, operator := range observation.Operators {
		operatorEvidence[uint64(operator.NoID)] = operator
		if operator.Error != "" {
			report.Discrepancies = append(report.Discrepancies, fmt.Sprintf("operator %d: %s", operator.NoID, operator.Error))
		}
	}
	claimFinalized, claimUncertain := map[int]int{}, map[int]int{}
	for _, claim := range observation.Claims {
		claimFinalized[claim.NoID] += claim.Finalized
		claimUncertain[claim.NoID] += claim.Uncertain + claim.Failed
		if claim.Error != "" {
			report.Discrepancies = append(report.Discrepancies, fmt.Sprintf("miner %d claim queue: %s", claim.MinerID, claim.Error))
		}
	}
	for _, operator := range contracts.Operators {
		evidence := operatorEvidence[operator.NoID]
		report.Operators = append(report.Operators, AnalysisOperator{NoID: operator.NoID, Active: operator.Active, PoolUID: operator.PoolUID, PoolLive: operator.PoolLive, ConvictionRao: operator.ConvictionRao, CarryRao: operator.CarryRao, StatsRows: evidence.StatsRows, ProofRows: evidence.ProofRows, ValidArtifacts: evidence.ValidArtifacts, MatchingArtifacts: evidence.MatchingArtifacts, ExpectedFinalizedArtifacts: evidence.ExpectedFinalizedArtifacts, FinalizedClaims: claimFinalized[int(operator.NoID)], UncertainClaims: claimUncertain[int(operator.NoID)]})
	}

	// Current conviction is an on-chain cumulative counter. Subtracting every
	// deposit and voluntary addition in the retained suffix gives the exact
	// conviction immediately before that suffix, even when epoch zero has
	// expired from the contract view. This avoids both guessing and dependence
	// on a trusted local event index.
	preConviction := map[uint64]*big.Int{}
	retainedMovement := map[uint64]*big.Int{}
	for _, epoch := range contracts.Epochs {
		for _, operator := range epoch.Operators {
			if retainedMovement[operator.NoID] == nil {
				retainedMovement[operator.NoID] = new(big.Int)
			}
			retainedMovement[operator.NoID].Add(retainedMovement[operator.NoID], decimal(operator.DepositRao))
			retainedMovement[operator.NoID].Add(retainedMovement[operator.NoID], decimal(operator.ConvictionAddedRao))
		}
	}
	canReconstructTier := len(contracts.Epochs) > 0
	for _, operator := range contracts.Operators {
		current := decimal(operator.ConvictionRao)
		movement := retainedMovement[operator.NoID]
		if movement == nil {
			movement = new(big.Int)
		}
		if current.Cmp(movement) < 0 {
			canReconstructTier = false
			report.Discrepancies = append(report.Discrepancies, fmt.Sprintf("operator %d current conviction %s is below retained movement %s", operator.NoID, current, movement))
			continue
		}
		preConviction[operator.NoID] = new(big.Int).Sub(current, movement)
	}
	for _, epoch := range contracts.Epochs {
		for _, operator := range epoch.Operators {
			row := AnalysisEpochOperator{Epoch: epoch.Epoch, NoID: operator.NoID, DepositRao: operator.DepositRao, ConvictionAddedRao: operator.ConvictionAddedRao, PayoutRoot: operator.PayoutRoot, ArtifactHash: operator.ArtifactHash, FundedRao: operator.FundedRao, TotalRao: operator.TotalRao, ClaimedRao: operator.ClaimedRao, ExpiryBlock: operator.ExpiryBlock, Status: operator.Status}
			if canReconstructTier {
				if preConviction[operator.NoID] == nil {
					canReconstructTier = false
					report.Discrepancies = append(report.Discrepancies, fmt.Sprintf("operator %d has retained epochs but no current conviction view", operator.NoID))
					report.EpochOperators = append(report.EpochOperators, row)
					continue
				}
				pre := new(big.Int).Set(preConviction[operator.NoID])
				row.PreEpochConvictionRao = pre.String()
				if tier, ok := depositTierAt(cfg.Policy.Deposit.Tiers, pre); ok {
					row.TierMinConvictionRao = fmt.Sprint(tier.MinConvictionRao)
					row.RateNumeratorRaoPerGiB = fmt.Sprint(tier.RateNumeratorRaoPerGiB)
					row.RateDenominator = fmt.Sprint(tier.RateDenominator)
					row.ImpliedUsageBytesFloor = impliedUsageBytesFloor(decimal(operator.DepositRao), tier)
				}
				deposit := decimal(operator.DepositRao)
				added := decimal(operator.ConvictionAddedRao)
				if added.Sign() > 0 {
					report.VoluntaryConvictionObserved = true
				}
				post := new(big.Int).Add(new(big.Int).Set(pre), deposit)
				post.Add(post, added)
				beforeTier, beforeOK := depositTierAt(cfg.Policy.Deposit.Tiers, pre)
				afterTier, afterOK := depositTierAt(cfg.Policy.Deposit.Tiers, post)
				if beforeOK && afterOK && beforeTier.MinConvictionRao != afterTier.MinConvictionRao {
					report.ConvictionTierCrossed = true
				}
				preConviction[operator.NoID].Set(post)
			}
			report.EpochOperators = append(report.EpochOperators, row)
		}
	}
	report.TierReconstructionComplete = canReconstructTier
	if len(contracts.Epochs) == 0 {
		report.Discrepancies = append(report.Discrepancies, "no retained epoch view is available for tier reconstruction")
	}
	for _, validator := range observation.Validators {
		report.Validators = append(report.Validators, AnalysisValidator{ValidatorID: validator.ValidatorID, CurrentStatus: validator.CurrentStatus, CurrentEpoch: validator.CurrentEpoch, VectorHash: validator.VectorHash, ValuesHash: validator.ValuesHash, FinalizedIntents: validator.FinalizedIntents, AppliedIntents: validator.AppliedIntents, IntentHashes: append([]string(nil), validator.IntentHashes...)})
		if validator.Error != "" {
			report.Discrepancies = append(report.Discrepancies, fmt.Sprintf("validator %d: %s", validator.ValidatorID, validator.Error))
		}
	}
	if !report.RuntimeCodeMatches {
		report.Discrepancies = append(report.Discrepancies, "deployed runtime code hashes differ from the release manifest")
	}
	if !report.PolicyHashMatches {
		report.Discrepancies = append(report.Discrepancies, "effective policy hash differs from the release policy")
	}
	if !report.Conservation.Holds {
		report.Discrepancies = append(report.Discrepancies, "settlement vault conservation does not hold")
	}
	sort.Strings(report.Discrepancies)
	return report
}

var analysisTemplate = template.Must(template.New("analysis").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>URnetwork subnet release analysis</title>
<style>body{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;margin:2rem;line-height:1.45;color:#17202a;background:#f8f9fa}h1{font-family:system-ui,sans-serif}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:white;border:1px solid #d5d8dc;border-radius:8px;padding:1rem}.pass{color:#117a65}.fail{color:#b03a2e}</style></head>
<body><h1>URnetwork subnet release 1.0 analysis</h1><p>Deployment <code>{{.DeploymentID}}</code>, netuid <code>{{.Netuid}}</code>, finalized block <code>{{.FinalizedHead.Number}}</code>.</p>
<p class="{{if .Conservation.Holds}}pass{{else}}fail{{end}}">Exact rao conservation: {{.Conservation.Holds}}</p>
<pre id="report">{{.JSON}}</pre></body></html>`))

func writeAnalysisHTML(path string, report *AnalysisReport) error {
	if report == nil {
		return fmt.Errorf("analysis report is nil")
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	view := struct {
		*AnalysisReport
		JSON string
	}{report, string(b)}
	var out bytes.Buffer
	if err := analysisTemplate.Execute(&out, view); err != nil {
		return err
	}
	return atomicWrite(path, out.Bytes(), 0o644)
}

func writeStandaloneAnalysis(stateDir string, report *AnalysisReport) error {
	if err := os.MkdirAll(filepath.Join(stateDir, "public"), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(stateDir, "public", "analysis.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}
	return writeAnalysisHTML(filepath.Join(stateDir, "public", "analysis.html"), report)
}
