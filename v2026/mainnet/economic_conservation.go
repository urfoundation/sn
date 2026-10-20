// The cross-domain observer retains earning occurrences separately from vault
// custody movements and accepted claims. It reconciles observed facts without
// granting finality, runtime, native fee or full quantization authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

const economicConservationSchema = "urnetwork-economic-conservation-v1"
const economicConservationPolicySchema = "urnetwork-economic-conservation-policy-v1"

// Route membership is independently configured. It selects which verified
// native provider owns a direct reward or a pool; it supplies no reward amount.
type economicConservationRoute struct {
	Hotkey  string `json:"hotkey"`
	Coldkey string `json:"coldkey"`
	Kind    string `json:"kind"`
	PoolId  string `json:"pool_id,omitempty"`
}

type economicConservationPolicy struct {
	PrincipalRetention *economicConservationPrincipalRetentionPolicy `json:"principal_retention,omitempty"`
	EntitlementSources *economicConservationEntitlementPolicy        `json:"original_entitlement_sources,omitempty"`
	StorageProfile     *economicConservationStorageProfile           `json:"storage_profile,omitempty"`
	operatingHeadBytes uint64
	FeeAuthority       *economicNativeFeePolicy                `json:"native_fee_authority,omitempty"`
	Continuation       *economicConservationContinuationPolicy `json:"continuation,omitempty"`
	Schema             string                                  `json:"schema"`
	Native             monitorEconomicNativePolicy             `json:"native"`
	Vault              monitorEconomicEvmPolicy                `json:"vault"`
	Claims             []monitorClaimPolicy                    `json:"claims"`
	Routes             []economicConservationRoute             `json:"routes"`
	MaximumFacts       uint64                                  `json:"maximum_facts"`
	ReadBudgetSeconds  uint64                                  `json:"read_budget_seconds,omitempty"`
	resourceBasis      *economicConservationPolicy
}

func (self economicConservationPolicy) validate() error {
	if err := self.StorageProfile.validate(); err != nil {
		return err
	}
	maximumFacts := uint64(8192)
	if self.StorageProfile != nil {
		maximumFacts = economicConservationMaximumFacts
	}
	network := self.Native.Observation.Network
	expected := identityExpectation{NativeChain: network.NativeChain, GenesisHash: network.GenesisHash, EvmChainId: network.EvmChainId}
	if self.Schema != economicConservationPolicySchema || self.Native.Observation.Execution == nil || self.Vault.Network != network || self.Vault.Netuid != self.Native.Observation.Netuid || self.Vault.ContractKind != "settlement-vault" || self.MaximumFacts < 16 || self.MaximumFacts > maximumFacts || self.ReadBudgetSeconds != 0 && (self.ReadBudgetSeconds < 60 || self.ReadBudgetSeconds > 900) || len(self.Routes) == 0 || len(self.Routes) > rootCensusLimit || len(self.Claims) > maxMonitorClaims {
		return errors.New("economic conservation requires original native execution, one vault, independent routes and finite resources")
	}
	if err := errors.Join(self.Native.validate(expected), self.Vault.validate(expected)); err != nil {
		return err
	}
	if err := errors.Join(self.Continuation.validate(self), self.initialResources().validatePolicy(self)); err != nil {
		return err
	}
	if err := self.validateYumaCapacity(); err != nil {
		return err
	}
	if err := self.validatePrincipalAuthority(); err != nil {
		return err
	}
	if err := self.PrincipalRetention.validate(self); err != nil {
		return err
	}
	if err := self.validateEntitlementSources(); err != nil {
		return err
	}
	if err := self.validateFeeAuthority(); err != nil {
		return err
	}
	if err := self.validateWholeFeeAuthority(); err != nil {
		return err
	}
	if self.Native.HistoryCatalog != nil || self.Vault.HistoryCatalog != nil || self.Vault.ResourceRevision != nil {
		return errors.New("economic conservation requires its own archive/resource adoption; component revisions cannot be borrowed")
	}
	seenHotkeys, seenPools, seenClaims := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, route := range self.Routes {
		if !rootCanonicalHash(route.Hotkey) || !rootCanonicalHash(route.Coldkey) || seenHotkeys[route.Hotkey] {
			return errors.New("economic route lacks unique original hotkey/coldkey membership")
		}
		seenHotkeys[route.Hotkey] = true
		switch route.Kind {
		case "direct-head":
			if route.PoolId != "" {
				return errors.New("direct native reward cannot also be a pool capture")
			}
		case "tail-pool":
			if !slices.Contains(self.Vault.PoolIds, route.PoolId) || seenPools[route.PoolId] {
				return errors.New("tail route must select one independently monitored pool")
			}
			seenPools[route.PoolId] = true
		default:
			return errors.New("economic route kind is unsupported")
		}
	}
	for _, claim := range self.Claims {
		if claim.Renewal != nil || claim.Window != nil {
			return errors.New("economic conservation cannot borrow a Claim owner's policy renewal")
		}
		if err := claim.validate(expected); err != nil {
			return err
		}
		if !strings.EqualFold(claim.ExpectedPool.Vault, self.Vault.Address) || !slices.Contains(self.Vault.PoolIds, claim.ExpectedPool.NoId) || !slices.Contains(self.Vault.Coldkeys, claim.ExpectedPool.Coldkey) || seenClaims[claim.Role] {
			return errors.New("economic claim expectation differs from original vault/pool/coldkey census")
		}
		seenClaims[claim.Role] = true
	}
	return nil
}

// The exact native header commits the EVM hash. Neither heights nor timestamps
// can create this mapping. Header/finality provenance remains separately named.
type economicConservationMapping struct {
	Native      economicEmissionBoundary `json:"native"`
	EvmHash     string                   `json:"evm_hash"`
	OutcomeHash string                   `json:"outcome_hash"`
}

type economicConservationLot struct {
	Id             string                     `json:"id"`
	Boundary       economicEmissionBoundary   `json:"boundary"`
	ProjectionHash string                     `json:"projection_hash"`
	OutcomeHash    string                     `json:"original_outcome_hash"`
	JobHash        string                     `json:"original_job_hash"`
	EventIndex     uint64                     `json:"event_index"`
	Effect         nativeExecutionEffect      `json:"effect"`
	Route          *economicConservationRoute `json:"route,omitempty"`
}

// A backing reference is a conservation edge, not another earned reward.
// Expiry and RootMissed preserve the original capture DAG rather than minting
// fresh income at the epoch where that obligation later becomes payable.
type economicConservationBacking struct {
	Kind   string `json:"kind"`
	Id     string `json:"id"`
	Amount string `json:"alpha"`
}

type economicConservationCapture struct {
	PrincipalEffects      *economicConservationCaptureEffects `json:"original_native_capture,omitempty"`
	Id                    string                              `json:"id"`
	Event                 monitorEconomicEvmEvent             `json:"event"`
	Native                *economicEmissionBoundary           `json:"native,omitempty"`
	Lots                  []string                            `json:"native_lots"`
	KnownLiquidAlpha      *string                             `json:"known_native_liquid_alpha"`
	AmountDifferenceAlpha *string                             `json:"capture_minus_known_liquid_alpha"`
	OpeningPrincipalAlpha *string                             `json:"opening_principal_alpha"`
	Status                string                              `json:"status"`
}

type economicConservationEntitlement struct {
	CarryEvent          *monitorEconomicEvmEvent                  `json:"original_carry_event,omitempty"`
	Finalization        *monitorEconomicEvmEvent                  `json:"original_finalization,omitempty"`
	Census              *economicConservationEntitlementCensus    `json:"original_leaf_census,omitempty"`
	CensusReference     *economicConservationEntitlementReference `json:"original_leaf_census_reference,omitempty"`
	CensusIssue         string                                    `json:"census_issue,omitempty"`
	CensusHeld          bool                                      `json:"census_integrity_held,omitempty"`
	CensusCapacityBasis string                                    `json:"census_capacity_basis,omitempty"`
	Id                  string                                    `json:"id"`
	Epoch               string                                    `json:"epoch"`
	PoolId              string                                    `json:"pool_id"`
	Status              string                                    `json:"status"`
	Funded              string                                    `json:"funded_alpha"`
	Total               *string                                   `json:"total_alpha"`
	Claimed             string                                    `json:"claimed_alpha"`
	PayoutRoot          string                                    `json:"payout_root,omitempty"`
	ArtifactHash        string                                    `json:"artifact_hash,omitempty"`
	Sources             []economicConservationBacking             `json:"sources"`
}

type economicConservationClaim struct {
	Id          string                  `json:"id"`
	Event       monitorEconomicEvmEvent `json:"event"`
	Entitlement string                  `json:"entitlement"`
	Status      string                  `json:"status"`
}

type economicConservationCredit struct {
	Opening string   `json:"opening_alpha"`
	Claims  []string `json:"accepted_claims"`
}

// Aggregate payment can span many epochs/pools. Keep the whole credit set; do
// not assign the entire payment to the newest leaf or count it as new income.
type economicConservationPayment struct {
	Id     string                     `json:"id"`
	Event  monitorEconomicEvmEvent    `json:"event"`
	Credit economicConservationCredit `json:"credit"`
	Status string                     `json:"status"`
}

type economicConservationReceipt struct {
	Role        string                    `json:"role"`
	Epoch       int64                     `json:"epoch"`
	ClaimId     string                    `json:"matched_claim_id,omitempty"`
	Status      string                    `json:"status"`
	Observation protocol.ClaimObservation `json:"original_observation"`
}

// These finite active facts retain original source identities. Capacity holds
// before an append and never prunes an unresolved liability. Archived matched
// facts stay authenticated by exact checkpoints under separately held custody.
type economicConservationState struct {
	principalProvisional *economicConservationPrincipalProvisional
	OriginalFees         []nativeFeeCensusProjection              `json:"original_complete_fee_census,omitempty"`
	FinalityApproval     []byte                                   `json:"original_consensus_approval,omitempty"`
	FinalityWindows      []nativeExecutionFinalityWindow          `json:"original_consensus_windows,omitempty"`
	FinalityFrom         *economicEmissionBoundary                `json:"original_consensus_vault_from,omitempty"`
	FinalityVaultBlocks  []economicEmissionBoundary               `json:"original_consensus_vault_blocks,omitempty"`
	EntitlementReadAfter map[string]string                        `json:"entitlement_read_after,omitempty"`
	NativeRenewal        *economicConservationNativeRenewal       `json:"native_approval_adoption,omitempty"`
	Yuma                 []economicConservationYumaEvidence       `json:"original_yuma_evidence,omitempty"`
	PrincipalExecutions  []economicConservationPrincipalExecution `json:"principal_execution_evidence,omitempty"`
	OpeningPrincipals    *economicConservationOpeningPrincipal    `json:"opening_principal_evidence,omitempty"`
	ClaimWindows         []economicConservationClaimWindow        `json:"claim_windows,omitempty"`
	FeeRevision          *economicConservationFeeRevision         `json:"native_fee_revision,omitempty"`
	NativeFeeObligations []economicConservationFeeObligation      `json:"native_fee_obligations,omitempty"`
	NativeFeeIssue       string                                   `json:"native_fee_issue,omitempty"`
	NativeFeeHeldRequest string                                   `json:"native_fee_held_request,omitempty"`
	NativeFeeHeldPolicy  string                                   `json:"native_fee_held_policy,omitempty"`
	NativeFeePending     bool                                     `json:"native_fee_pending,omitempty"`
	NativeFees           []economicConservationFeeEvidence        `json:"native_fee_evidence,omitempty"`
	Archive              *economicConservationArchive             `json:"archive,omitempty"`
	Renewal              *economicConservationRenewal             `json:"resource_renewal,omitempty"`
	Schema               string                                   `json:"schema"`
	PolicyHash           string                                   `json:"policy_hash"`
	Native               monitorEconomicNativeState               `json:"native"`
	Vault                monitorEconomicEvmState                  `json:"vault"`
	ClaimStates          []monitorClaimState                      `json:"claims"`
	OpeningVault         *monitorEconomicEvmSnapshot              `json:"opening_vault,omitempty"`
	Mappings             []economicConservationMapping            `json:"native_evm_mappings"`
	Lots                 []economicConservationLot                `json:"native_lots"`
	Captures             []economicConservationCapture            `json:"captures"`
	Entitlements         []economicConservationEntitlement        `json:"entitlements"`
	Carry                map[string][]economicConservationBacking `json:"carry"`
	Claims               []economicConservationClaim              `json:"accepted_claims"`
	Credits              map[string]economicConservationCredit    `json:"credits"`
	Payments             []economicConservationPayment            `json:"payments"`
	Receipts             []economicConservationReceipt            `json:"claim_receipts"`
	SampleAt             time.Time                                `json:"sample_at"`
	NativePending        bool                                     `json:"native_pending,omitempty"`
	NativeIssue          string                                   `json:"native_issue,omitempty"`
	VaultIssue           string                                   `json:"vault_issue,omitempty"`
	NativeHeld           bool                                     `json:"native_integrity_held"`
	VaultHeld            bool                                     `json:"vault_integrity_held"`
	JoinIssue            string                                   `json:"join_issue,omitempty"`
	ContentHash          string                                   `json:"content_hash"`
	entitlementIds       map[string]int
	claimIds             map[string]int
	captureKeys          map[string]bool
	claimKeys            map[string]bool
	archiveView          *economicConservationArchiveView
}

func newEconomicConservationState(policy economicConservationPolicy) *economicConservationState {
	result := &economicConservationState{Schema: economicConservationSchema, PolicyHash: policy.identityHash(), Native: *newMonitorEconomicNativeState(policy.Native), Vault: *newMonitorEconomicEvmState(policy.Vault), Carry: map[string][]economicConservationBacking{}, Credits: map[string]economicConservationCredit{}}
	for _, claim := range policy.Claims {
		result.ClaimStates = append(result.ClaimStates, *newMonitorClaimState(claim))
	}
	return result
}

func (self economicConservationState) hash() string {
	self.ContentHash = ""
	return rootObjectHash(self)
}

func (self economicConservationState) facts() uint64 {
	principalFacts := uint64(0)
	if self.OpeningPrincipals != nil {
		principalFacts = uint64(len(self.OpeningPrincipals.Projection.Observations)) + 1
	}
	return self.wholeFeeFacts() + self.finalityFacts() + self.entitlementCensusFacts() + principalFacts + self.yumaFacts() + self.principalEffectFacts() + uint64(len(self.Mappings)+len(self.Lots)+len(self.Captures)+len(self.Entitlements)+len(self.Claims)+len(self.Payments)+len(self.Receipts)) + self.feeFacts()
}

func (self economicConservationState) validate(ctx context.Context, policy economicConservationPolicy) error {
	policy = policy.withStorageProfile()
	if ctx == nil {
		return errors.New("economic conservation validation has no lifecycle owner")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := self.nativeFeeAuthority(policy); err != nil {
		return err
	}
	operating, err := self.operatingPolicy(policy)
	if err != nil {
		return err
	}
	if err := self.Native.admitRuntime(ctx, operating.Native); err != nil {
		return err
	}
	if self.Schema != economicConservationSchema || self.PolicyHash != policy.identityHash() || self.ContentHash != self.hash() || len(self.ClaimStates) != len(policy.Claims) || self.facts() > operating.MaximumFacts || len(self.NativeIssue) > 2048 || len(self.VaultIssue) > 2048 || len(self.JoinIssue) > 2048 {
		return errors.New("economic conservation checkpoint changed original policy, evidence or capacity")
	}
	if err := errors.Join(self.Native.validate(operating.Native), self.Vault.validate(policy.Vault)); err != nil {
		return err
	}
	for index, claim := range self.ClaimStates {
		if err := validateMonitorClaimState(operating.Claims[index], claim); err != nil {
			return err
		}
	}
	if err := self.validateClaimWindows(policy); err != nil {
		return err
	}
	// Component compact summaries borrow only this combined owner's exact
	// archived checkpoints. They cannot import another role's archive catalog.
	if self.Native.Catalog != nil || self.Vault.Catalog != nil || self.Archive == nil && (self.Native.Archive != nil || self.Vault.Archive != nil) {
		return errors.New("economic conservation archive adoption is not configured")
	}
	if err := self.Archive.validate(policy, &self); err != nil {
		return err
	}
	if err := self.validateFinality(ctx, policy); err != nil {
		return err
	}
	if err := self.validateWholeFees(ctx, policy); err != nil {
		return err
	}
	if err := self.validateOpeningPrincipal(policy); err != nil {
		return err
	}
	if err := self.validateYuma(ctx, policy); err != nil {
		return err
	}
	if err := self.validatePrincipalEffects(operating); err != nil {
		return err
	}
	if _, err := self.feeSummary(policy); err != nil {
		return err
	}
	if err := self.validateEntitlementShapes(policy); err != nil {
		return err
	}
	if len(self.NativeFeeIssue) > 2048 || self.NativeFeeHeldRequest != "" && !planSha256(self.NativeFeeHeldRequest) || policy.FeeAuthority == nil && (self.NativeFeeIssue != "" || self.NativeFeeHeldRequest != "" || self.NativeFeePending) {
		return errors.New("economic native fee status differs from original policy or resource bound")
	}
	if self.NativeFeeHeldPolicy != "" && (!planSha256(self.NativeFeeHeldPolicy) || self.NativeFeeHeldRequest == "" || policy.FeeAuthority == nil) {
		return errors.New("economic unadmitted fee policy hold lost its exact request")
	}
	if self.Vault.BatchCount != 0 && self.OpeningVault == nil {
		return errors.New("economic conservation lost its opening vault obligations")
	}
	if self.OpeningVault != nil {
		if err := self.OpeningVault.validate(policy.Vault); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, lot := range self.Lots {
		if seen[lot.Id] || self.archiveView != nil && self.archiveView.lotIds[lot.Id] || !planSha256(lot.Id) || lot.Id != economicConservationLotId(lot.ProjectionHash, lot.Effect) || lot.Boundary.Number <= policy.Native.Observation.From.Number || lot.Boundary.Number > self.Native.Cursor.Number || !rootCanonicalHash(lot.Boundary.Hash) || !planSha256(lot.OutcomeHash) || !planSha256(lot.JobHash) {
			return errors.New("economic conservation repeated or relabelled an earning occurrence")
		}
		seen[lot.Id] = true
	}
	for _, capture := range self.Captures {
		if seen[capture.Id] || capture.Id != rootObjectHash(capture.Event) || capture.Event.Name != "EmissionCaptured" || capture.OpeningPrincipalAlpha != nil && capture.PrincipalEffects == nil {
			return errors.New("economic capture changed original event or invented principal evidence")
		}
		seen[capture.Id] = true
	}
	if self.Archive == nil || self.archiveView != nil {
		checked := self
		if err := checked.reconcileCaptureEffects(ctx, policy, true); err != nil {
			return err
		}
	}
	for _, claim := range self.Claims {
		if seen[claim.Id] || claim.Id != rootObjectHash(claim.Event) || claim.Event.Name != "Claimed" {
			return errors.New("economic claim changed original acceptance")
		}
		seen[claim.Id] = true
	}
	for _, payment := range self.Payments {
		if seen[payment.Id] || payment.Id != rootObjectHash(payment.Event) || payment.Event.Name != "ClaimPaid" {
			return errors.New("economic payment repeated original transfer")
		}
		seen[payment.Id] = true
	}
	seenReceipts := map[string]bool{}
	claimPolicies := make(map[string]monitorClaimPolicy, len(policy.Claims))
	for _, claim := range operating.Claims {
		claimPolicies[claim.Role] = claim
	}
	for _, receipt := range self.Receipts {
		key := economicConservationReceiptKey(receipt)
		claim, roleKnown := claimPolicies[receipt.Role]
		epochKnown := false
		for _, epoch := range claim.Epochs {
			epochKnown = epochKnown || epoch.Epoch == receipt.Epoch && epoch.ShareBps == receipt.Observation.ShareBps
		}
		if seenReceipts[key] || receipt.Observation.Validate() != nil || receipt.Observation.EvidenceKind != "signed-receipt" || receipt.Observation.Epoch != receipt.Epoch ||
			!roleKnown || !epochKnown || receipt.Observation.Pool != claim.ExpectedPool ||
			receipt.ClaimId == "" && receipt.Status != "original-receipt-not-yet-observed" || receipt.ClaimId != "" && (!planSha256(receipt.ClaimId) || receipt.Status != "original-acceptance-observed") {
			return errors.New("economic original Claim receipt identity or status differs")
		}
		seenReceipts[key] = true
		if _, err := self.archiveView.retainedReceipt(receipt); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(self)
	if err != nil {
		return err
	}
	if uint64(len(raw)+1) > operating.headBytes() {
		return errMonitorEconomicCapacity
	}
	return nil
}

func economicConservationLotId(projection string, effect nativeExecutionEffect) string {
	return rootObjectHash(struct {
		Projection string                `json:"projection"`
		Effect     nativeExecutionEffect `json:"effect"`
	}{Projection: projection, Effect: effect})
}

// No amount is reconstructed from timestamp windows. The native observer has
// already replayed the original job; the parser joins the exact header digest.
func (self *economicConservationState) appendNative(ctx context.Context, policy economicConservationPolicy, observation *economicEmissionObservation, now time.Time) error {
	if observation == nil {
		return nil
	}
	next, err := self.Native.append(policy.Native, observation, now, ctx)
	if err != nil {
		return err
	}
	routes := make(map[string]economicConservationRoute, len(policy.Routes))
	for _, route := range policy.Routes {
		routes[route.Hotkey] = route
	}
	for _, block := range observation.Blocks {
		outcome := block.ExecutionOutcome
		if outcome == nil {
			return errors.New("economic native source did not retain original execution")
		}
		if err := self.appendFinality(ctx, policy, *outcome, block); err != nil {
			return err
		}
		if err := self.appendWholeFees(ctx, policy, *outcome, block); err != nil {
			return err
		}
		if err := self.appendOpeningPrincipal(policy, *outcome); err != nil {
			return err
		}
		if err := self.appendYuma(ctx, policy, *outcome); err != nil {
			return err
		}
		if err := self.appendPrincipalEffects(policy, *outcome, next); err != nil {
			return err
		}
		projection := outcome.RecipientEffects
		if err := projection.validate(*outcome); err != nil {
			return err
		}
		if self.facts()+uint64(len(projection.Effects))+1 > policy.MaximumFacts {
			return errMonitorEconomicCapacity
		}
		if projection.Parent.Hash != block.Header.ParentHash {
			return errors.New("economic earning projection changed original parent")
		}
		// Absence of an admitted Frontier mapping remains unmatched; do not
		// derive a mapping from equal block numbers or nearby timestamps.
		if mapping, err := finalizedFrontierPostLog(block.Header); err == nil {
			self.Mappings = append(self.Mappings, economicConservationMapping{Native: block.Boundary, EvmHash: mapping.BlockHash, OutcomeHash: outcome.ContentHash})
		} else if !errors.Is(err, errFinalizedMappingUnavailable) || errors.Is(err, errRpcIntegrity) {
			return err
		}
		for _, effect := range projection.Effects {
			lot := economicConservationLot{Id: economicConservationLotId(projection.ContentHash, effect), Boundary: block.Boundary, ProjectionHash: projection.ContentHash, OutcomeHash: outcome.ContentHash, JobHash: outcome.JobHash, EventIndex: *projection.EventIndex, Effect: effect}
			if self.archiveView != nil && self.archiveView.lotIds[lot.Id] {
				return errors.New("economic earning occurrence repeats an archived original")
			}
			if route, ok := routes[effect.Recipient.Hotkey]; ok {
				if !effect.Provider || route.Coldkey != effect.Recipient.Coldkey {
					return errors.New("economic route contradicts original provider generation or reward owner")
				}
				lot.Route = &route
			}
			self.Lots = append(self.Lots, lot)
		}
	}
	self.Native = *next
	return nil
}

// A value is checked before arithmetic. Keep alpha, native rao and wei in
// different typed fields; none is converted using an assumed exchange ratio.
func economicConservationSum(values ...string) (string, error) {
	result := new(big.Int)
	for _, encoded := range values {
		value, err := monitorEconomicInteger(encoded)
		if err != nil {
			return "", err
		}
		result.Add(result, value)
		if result.BitLen() > 256 {
			return "", errors.New("economic conservation integer exceeds uint256")
		}
	}
	return result.String(), nil
}

func economicConservationEntitlementId(epoch, pool string) string { return epoch + "/" + pool }

// These instance-owned indexes are reconstructed once after decoding and then
// updated with their slices. They have no authority beyond the retained facts.
func (self *economicConservationState) index() error {
	if self.entitlementIds != nil {
		return nil
	}
	self.entitlementIds, self.claimIds = map[string]int{}, map[string]int{}
	self.captureKeys, self.claimKeys = map[string]bool{}, map[string]bool{}
	for index, entitlement := range self.Entitlements {
		if _, ok := self.entitlementIds[entitlement.Id]; ok {
			return errors.New("economic original entitlement is duplicated")
		}
		self.entitlementIds[entitlement.Id] = index
	}
	for _, capture := range self.Captures {
		key := economicConservationEntitlementId(capture.Event.Values["epoch"], capture.Event.Values["noId"])
		if self.captureKeys[key] {
			return errors.New("economic original capture is duplicated")
		}
		self.captureKeys[key] = true
	}
	for index, claim := range self.Claims {
		key := claim.Entitlement + "/" + claim.Event.Values["coldkey"]
		if self.claimKeys[key] {
			return errors.New("economic original claimed leaf is duplicated")
		}
		self.claimKeys[key], self.claimIds[claim.Id] = true, index
	}
	return nil
}

func (self *economicConservationState) entitlement(epoch, pool string) *economicConservationEntitlement {
	id := economicConservationEntitlementId(epoch, pool)
	if index, ok := self.entitlementIds[id]; ok {
		return &self.Entitlements[index]
	}
	self.entitlementIds[id] = len(self.Entitlements)
	if self.archiveView != nil {
		if prior, ok := self.archiveView.entitlements[id]; ok {
			prior.Sources = slices.Clone(prior.Sources)
			self.Entitlements = append(self.Entitlements, prior)
			return &self.Entitlements[len(self.Entitlements)-1]
		}
	}
	self.Entitlements = append(self.Entitlements, economicConservationEntitlement{Id: id, Epoch: epoch, PoolId: pool, Status: "opening-obligation-unattributed", Funded: "0", Claimed: "0"})
	return &self.Entitlements[len(self.Entitlements)-1]
}

func (self *economicConservationState) opening(snapshot monitorEconomicEvmSnapshot) {
	value := snapshot.clone()
	self.OpeningVault = &value
	for pool, amount := range snapshot.Pools {
		self.Carry[pool] = []economicConservationBacking{{Kind: "opening-carry-unattributed", Id: rootObjectHash(snapshot) + "/" + pool, Amount: amount}}
	}
	for coldkey, amount := range snapshot.Credits {
		self.Credits[coldkey] = economicConservationCredit{Opening: amount, Claims: []string{}}
	}
}

// Receipt trie/body/getter verification remains in the existing public reader.
// These transitions only preserve provenance through the checked state machine.
func (self *economicConservationState) appendVault(policy economicConservationPolicy, observation *monitorEvmObservation, now time.Time) error {
	if observation == nil {
		return nil
	}
	if err := self.index(); err != nil {
		return err
	}
	next, err := self.Vault.append(policy.Vault, observation, now)
	if err != nil {
		return err
	}
	if self.OpeningVault == nil {
		self.opening(observation.Before)
	}
	trackFinality := policy.Native.Observation.Execution != nil && policy.Native.Observation.Execution.Producer != nil
	if trackFinality && self.FinalityFrom == nil {
		boundary := self.Vault.Cursor
		self.FinalityFrom = &boundary
	}
	for _, block := range observation.Blocks {
		if block.Boundary.Number > next.Cursor.Number {
			break
		}
		if trackFinality {
			if self.facts()+1 > policy.MaximumFacts {
				return errMonitorEconomicCapacity
			}
			self.FinalityVaultBlocks = append(self.FinalityVaultBlocks, block.Boundary)
		}
		for _, event := range block.Events {
			if self.facts()+2 > policy.MaximumFacts {
				return errMonitorEconomicCapacity
			}
			if err := self.vaultEvent(event, block.Funded); err != nil {
				return err
			}
			if event.Name == "EntitlementFinalized" && policy.EntitlementSources != nil {
				self.entitlement(event.Values["epoch"], event.Values["noId"]).Finalization = &event
			}
			if (event.Name == "RootMissed" || event.Name == "EntitlementExpired") && policy.EntitlementSources != nil {
				self.entitlement(event.Values["epoch"], event.Values["noId"]).CarryEvent = &event
			}
		}
	}
	self.Vault = *next
	return nil
}

func (self *economicConservationState) vaultEvent(event monitorEconomicEvmEvent, funded map[string]string) error {
	if err := self.index(); err != nil {
		return err
	}
	id, values := rootObjectHash(event), event.Values
	epoch, pool := values["epoch"], values["noId"]
	switch event.Name {
	case "EmissionCaptured":
		key := economicConservationEntitlementId(epoch, pool)
		if self.captureKeys[key] || self.archiveView != nil && self.archiveView.captureKeys[key] != "" {
			return errors.New("economic capture repeated an original epoch or event")
		}
		self.captureKeys[key] = true
		self.Captures = append(self.Captures, economicConservationCapture{Id: id, Event: event, Lots: []string{}, Status: "native-mapping-unavailable"})
		record := self.entitlement(epoch, pool)
		if record.Status != "opening-obligation-unattributed" {
			return errors.New("economic capture replaced a retained entitlement")
		}
		record.Status, record.Funded = "funded", values["amount"]
		record.Sources = []economicConservationBacking{{Kind: "capture", Id: id, Amount: record.Funded}}
	case "EmissionDeferred":
		record := self.entitlement(epoch, pool)
		if record.Status != "opening-obligation-unattributed" {
			return errors.New("economic deferral repeated a funding transition")
		}
		record.Status = "funded"
	case "RootMissed", "EntitlementFinalized":
		record := self.entitlement(epoch, pool)
		originalFunded, ok := funded[record.Id]
		if !ok {
			return errors.New("economic entitlement has no checked original funded value")
		}
		if record.Status == "opening-obligation-unattributed" {
			record.Funded = originalFunded
			record.Sources = []economicConservationBacking{{Kind: "opening-funding-unattributed", Id: record.Id, Amount: originalFunded}}
		} else if record.Status != "funded" || record.Funded != originalFunded {
			return errors.New("economic entitlement replaced original funding")
		}
		if event.Name == "RootMissed" {
			if originalFunded != values["carried"] {
				return errors.New("economic missed root changed the original capture amount")
			}
			record.Status = "root-missed"
			self.Carry[pool] = append(self.Carry[pool], economicConservationBacking{Kind: "root-missed", Id: record.Id, Amount: originalFunded})
		} else {
			total := originalFunded
			for _, carry := range self.Carry[pool] {
				var err error
				total, err = economicConservationSum(total, carry.Amount)
				if err != nil {
					return err
				}
			}
			if total != values["total"] {
				return errors.New("economic finalization omitted original pool carry")
			}
			record.Sources = append(record.Sources, self.Carry[pool]...)
			self.Carry[pool] = nil
			record.Status, record.Total, record.PayoutRoot, record.ArtifactHash = "finalized", &total, values["payoutRoot"], values["artifactHash"]
		}
	case "EntitlementExpired":
		record := self.entitlement(epoch, pool)
		if record.Status != "finalized" && record.Status != "opening-obligation-unattributed" {
			return errors.New("economic expiry repeated or bypassed original finalization")
		}
		if record.Total != nil {
			total, err := economicConservationSum(record.Claimed, values["unclaimed"])
			if err != nil || total != *record.Total {
				return errors.Join(errors.New("economic expiry erased or repeated accepted credit"), err)
			}
		}
		record.Status = "carried"
		self.Carry[pool] = append(self.Carry[pool], economicConservationBacking{Kind: "expired-entitlement", Id: record.Id, Amount: values["unclaimed"]})
	case "Claimed":
		record := self.entitlement(epoch, pool)
		if record.Status != "finalized" && record.Status != "opening-obligation-unattributed" {
			return errors.New("economic claim bypassed original live entitlement")
		}
		status := "opening-entitlement-unattributed"
		if record.Total != nil {
			total, e1 := monitorEconomicInteger(*record.Total)
			share, e2 := monitorEconomicInteger(values["shareBps"])
			if e1 != nil || e2 != nil || share.Sign() == 0 || share.Cmp(big.NewInt(10000)) > 0 || new(big.Int).Quo(new(big.Int).Mul(total, share), big.NewInt(10000)).String() != values["amount"] {
				return errors.New("economic accepted claim differs from the exact original entitlement/share")
			}
			status = "original-entitlement-observed"
		}
		key := record.Id + "/" + values["coldkey"]
		if self.claimKeys[key] || self.archiveView != nil && self.archiveView.claimKeys[key] != "" {
			return errors.New("economic accepted leaf was counted twice")
		}
		self.claimKeys[key], self.claimIds[id] = true, len(self.Claims)
		var err error
		record.Claimed, err = economicConservationSum(record.Claimed, values["amount"])
		if err != nil {
			return err
		}
		if record.Total != nil {
			claimed, _ := monitorEconomicInteger(record.Claimed)
			total, err := monitorEconomicInteger(*record.Total)
			if err != nil || claimed.Cmp(total) > 0 {
				return errors.Join(errors.New("economic accepted credits exceed original entitlement"), err)
			}
		}
		self.Claims = append(self.Claims, economicConservationClaim{Id: id, Event: event, Entitlement: record.Id, Status: status})
		credit, ok := self.Credits[values["coldkey"]]
		if !ok {
			// An unselected coldkey may already have credit from older windows.
			credit.Opening = ""
		}
		credit.Claims = append(credit.Claims, id)
		self.Credits[values["coldkey"]] = credit
	case "ClaimPaid":
		credit, ok := self.Credits[values["coldkey"]]
		status := "opening-credit-unavailable"
		if ok && credit.Opening != "" {
			amount := credit.Opening
			for _, claimId := range credit.Claims {
				index, found := self.claimIds[claimId]
				if !found {
					return errors.New("economic aggregate payment lost an original accepted claim")
				}
				var err error
				amount, err = economicConservationSum(amount, self.Claims[index].Event.Values["amount"])
				if err != nil {
					return err
				}
			}
			if amount != values["amount"] {
				return errors.New("economic aggregate payment differs from retained coldkey credit")
			}
			status = "aggregate-credit-observed"
		}
		self.Payments = append(self.Payments, economicConservationPayment{Id: id, Event: event, Credit: credit, Status: status})
		self.Credits[values["coldkey"]] = economicConservationCredit{Opening: "0", Claims: []string{}}
	}
	return nil
}

// Actual native-header/EVM-hash matching selects source boundaries. The source
// amount is a candidate only: unproved opening principal, validator income or
// other stake effects cannot be silently explained away by matching totals.
func (self *economicConservationState) reconcile(policy economicConservationPolicy) error {
	mappings := make(map[string]economicConservationMapping, len(self.Mappings))
	for _, mapping := range self.Mappings {
		if prior, ok := mappings[mapping.EvmHash]; ok && prior != mapping {
			return errors.New("economic EVM hash has conflicting native execution mappings")
		}
		mappings[mapping.EvmHash] = mapping
	}
	previous := map[string]economicEmissionBoundary{}
	if self.Archive != nil {
		for pool, boundary := range self.Archive.PoolBoundaries {
			previous[pool] = boundary
		}
	}
	blocked := map[string]bool{}
	usedLots := map[string]bool{}
	poolLots := map[string][]economicConservationLot{}
	poolRoutes := map[string]economicConservationRoute{}
	positions := map[string]int{}
	for _, route := range policy.Routes {
		if route.Kind == "tail-pool" {
			poolRoutes[route.PoolId] = route
		}
	}
	for _, lot := range self.Lots {
		if lot.Route != nil && lot.Route.Kind == "tail-pool" {
			poolLots[lot.Route.PoolId] = append(poolLots[lot.Route.PoolId], lot)
		}
	}
	for index := range self.Captures {
		capture := &self.Captures[index]
		pool := capture.Event.Values["noId"]
		route, routed := poolRoutes[pool]
		if !routed {
			capture.Status = "independent-native-pool-route-unavailable"
			continue
		}
		if route.Hotkey != capture.Event.Values["poolHotkey"] {
			return errors.New("economic vault capture differs from independently configured pool hotkey")
		}
		mapping, ok := mappings[capture.Event.Block.Hash]
		if !ok && self.archiveView != nil {
			mapping, ok = self.archiveView.mappings[capture.Event.Block.Hash]
		}
		if !ok || blocked[pool] {
			capture.Status = "native-mapping-unavailable"
			blocked[pool] = true
			continue
		}
		from, exists := previous[pool]
		if !exists {
			from = policy.Native.Observation.From
		}
		if mapping.Native.Number < from.Number {
			return errors.New("economic capture moved behind its original native source boundary")
		}
		capture.Native, capture.Lots = &mapping.Native, []string{}
		known := "0"
		for positions[pool] < len(poolLots[pool]) {
			lot := poolLots[pool][positions[pool]]
			if lot.Boundary.Number > mapping.Native.Number {
				break
			}
			if lot.Boundary.Number <= from.Number {
				return errors.New("economic earning occurrence survived an already consumed source interval")
			}
			positions[pool]++
			if lot.Effect.Recipient.Hotkey != capture.Event.Values["poolHotkey"] || usedLots[lot.Id] {
				return errors.New("economic capture reused or relabelled an original native recipient")
			}
			usedLots[lot.Id] = true
			capture.Lots = append(capture.Lots, lot.Id)
			var err error
			known, err = economicConservationSum(known, lot.Effect.Liquid)
			if err != nil {
				return err
			}
		}
		amount, err := monitorEconomicInteger(capture.Event.Values["amount"])
		if err != nil {
			return err
		}
		liquid, err := monitorEconomicInteger(known)
		if err != nil {
			return err
		}
		difference := new(big.Int).Sub(amount, liquid).String()
		capture.KnownLiquidAlpha, capture.AmountDifferenceAlpha = &known, &difference
		capture.Status = "source-difference-unresolved"
		if difference == "0" {
			capture.Status = "amount-equal-principal-and-stake-effects-unproved"
		}
		previous[pool] = mapping.Native
	}
	self.Receipts = nil
	claimEvents := make(map[string]economicConservationClaim, len(self.Claims))
	claimKey := func(transaction, block, epoch, pool, coldkey string) string {
		return transaction + "/" + block + "/" + epoch + "/" + pool + "/" + coldkey
	}
	for _, claim := range self.Claims {
		event := claim.Event
		claimEvents[claimKey(event.TransactionHash, event.Block.Hash, event.Values["epoch"], event.Values["noId"], event.Values["coldkey"])] = claim
	}
	for index, claimPolicy := range policy.Claims {
		for _, epoch := range self.ClaimStates[index].Epochs {
			observation := epoch.Observation
			if observation == nil || observation.EvidenceKind != "signed-receipt" {
				continue
			}
			receipt := economicConservationReceipt{Role: claimPolicy.Role, Epoch: epoch.Epoch, Status: "original-receipt-not-yet-observed", Observation: *cloneMonitorClaimObservation(observation)}
			key := claimKey(observation.TransactionHash, observation.BlockHash, fmt.Sprint(epoch.Epoch), observation.Pool.NoId, observation.Pool.Coldkey)
			claim, matched := claimEvents[key]
			if !matched && self.archiveView != nil {
				archivedId := self.archiveView.claimKeys[economicConservationEntitlementId(fmt.Sprint(epoch.Epoch), observation.Pool.NoId)+"/"+observation.Pool.Coldkey]
				if archived, exists := self.archiveView.claims[archivedId]; exists && claimKey(archived.Event.TransactionHash, archived.Event.Block.Hash, archived.Event.Values["epoch"], archived.Event.Values["noId"], archived.Event.Values["coldkey"]) == key {
					claim, matched = archived, true
				}
			}
			if matched {
				event := claim.Event
				if event.Values["amount"] != observation.AcceptedAmountRao || event.Values["shareBps"] != fmt.Sprint(observation.ShareBps) || event.Block.Number != observation.BlockNumber {
					return errors.New("economic original Claim receipt contradicts its checked vault event")
				}
				receipt.ClaimId, receipt.Status = claim.Id, "original-acceptance-observed"
			}
			known, err := self.archiveView.retainedReceipt(receipt)
			if err != nil {
				return err
			}
			if known {
				continue
			}
			self.Receipts = append(self.Receipts, receipt)
		}
	}
	return nil
}
