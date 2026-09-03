package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

const PolicySchema = "urnetwork-policy-v1"

type Rational struct {
	Numerator   uint64 `json:"numerator" yaml:"numerator"`
	Denominator uint64 `json:"denominator" yaml:"denominator"`
}

func (r Rational) Validate(name string) error {
	if r.Denominator == 0 {
		return fmt.Errorf("%s denominator is zero", name)
	}
	return nil
}

func (r Rational) cmp(other Rational) int {
	l := new(big.Int).Mul(new(big.Int).SetUint64(r.Numerator), new(big.Int).SetUint64(other.Denominator))
	right := new(big.Int).Mul(new(big.Int).SetUint64(other.Numerator), new(big.Int).SetUint64(r.Denominator))
	return l.Cmp(right)
}

type SettlementPolicy struct {
	EpochBlocks            uint64 `json:"epoch_blocks" yaml:"epoch_blocks"`
	RootCommitWindowBlocks uint64 `json:"root_commit_window_blocks" yaml:"root_commit_window_blocks"`
	FinalizeOffsetBlocks   uint64 `json:"finalize_offset_blocks" yaml:"finalize_offset_blocks"`
	CloseGraceBlocks       uint64 `json:"close_grace_blocks" yaml:"close_grace_blocks"`
	ClaimTTLEpochs         uint64 `json:"claim_ttl_epochs" yaml:"claim_ttl_epochs"`
	ClaimGraceEpochs       uint64 `json:"claim_grace_epochs" yaml:"claim_grace_epochs"`
	MissedRootAction       string `json:"missed_root_action" yaml:"missed_root_action"`
	ExpiredClaimAction     string `json:"expired_claim_action" yaml:"expired_claim_action"`
	SharesTotalBPS         uint64 `json:"shares_total_bps" yaml:"shares_total_bps"`
	Rounding               string `json:"rounding" yaml:"rounding"`
}

// CadenceSnapshot is the future production cadence committed by the policy.
// It intentionally describes a transition rule instead of one absolute epoch:
// setup time and testnet block progress are not deterministic, so the harness
// schedules this snapshot for currentEpoch+1 only after the accelerated gate
// has completed.
type CadenceSnapshot struct {
	AfterAcceleratedEpochs uint64 `json:"after_accelerated_epochs" yaml:"after_accelerated_epochs"`
	EpochBlocks            uint64 `json:"epoch_blocks" yaml:"epoch_blocks"`
	RootCommitWindowBlocks uint64 `json:"root_commit_window_blocks" yaml:"root_commit_window_blocks"`
	FinalizeOffsetBlocks   uint64 `json:"finalize_offset_blocks" yaml:"finalize_offset_blocks"`
	CloseGraceBlocks       uint64 `json:"close_grace_blocks" yaml:"close_grace_blocks"`
}

type QualityTransform struct {
	Kind       string `json:"kind" yaml:"kind"`
	MinimumPPM uint32 `json:"minimum_ppm" yaml:"minimum_ppm"`
	MaximumPPM uint32 `json:"maximum_ppm" yaml:"maximum_ppm"`
}

type SteeringPolicy struct {
	Theta             Rational         `json:"theta" yaml:"theta"`
	QualityTransform  QualityTransform `json:"quality_transform" yaml:"quality_transform"`
	HeadScoreEMA      Rational         `json:"head_score_ema" yaml:"head_score_ema"`
	MaximumHeadFleets uint16           `json:"maximum_head_fleets" yaml:"maximum_head_fleets"`
	MaxWeightLimitU16 uint16           `json:"max_weight_limit_u16" yaml:"max_weight_limit_u16"`
	EmptyChannel      string           `json:"empty_channel" yaml:"empty_channel"`
	Arithmetic        string           `json:"arithmetic" yaml:"arithmetic"`
}

type DepositTier struct {
	MinConvictionRao       uint64 `json:"min_conviction_rao" yaml:"min_conviction_rao"`
	RateNumeratorRaoPerGiB uint64 `json:"rate_numerator_rao_per_gib" yaml:"rate_numerator_rao_per_gib"`
	RateDenominator        uint64 `json:"rate_denominator" yaml:"rate_denominator"`
}

func (t DepositTier) rate() Rational {
	return Rational{Numerator: t.RateNumeratorRaoPerGiB, Denominator: t.RateDenominator}
}

type DepositPolicy struct {
	Unit                    string        `json:"unit" yaml:"unit"`
	EpochCapRaoPerOperator  uint64        `json:"epoch_cap_rao_per_operator" yaml:"epoch_cap_rao_per_operator"`
	TotalTestCampaignCapRao uint64        `json:"total_test_campaign_cap_rao" yaml:"total_test_campaign_cap_rao"`
	TierSnapshot            string        `json:"tier_snapshot" yaml:"tier_snapshot"`
	UsageLagEpochs          uint64        `json:"usage_lag_epochs" yaml:"usage_lag_epochs"`
	UsageArtifact           string        `json:"usage_artifact" yaml:"usage_artifact"`
	MismatchAction          string        `json:"mismatch_action" yaml:"mismatch_action"`
	UnavailableAction       string        `json:"unavailable_action" yaml:"unavailable_action"`
	Tiers                   []DepositTier `json:"tiers" yaml:"tiers"`
	ZeroOrInvalidRate       string        `json:"zero_or_invalid_rate" yaml:"zero_or_invalid_rate"`
}

// DepositTierAt returns the exact conviction snapshot tier used by both an
// operator sizing its demand deposit and validators auditing that deposit.
func DepositTierAt(policy DepositPolicy, conviction *big.Int) (DepositTier, error) {
	if conviction == nil || conviction.Sign() < 0 || len(policy.Tiers) == 0 {
		return DepositTier{}, errors.New("invalid conviction tier input")
	}
	selected := policy.Tiers[0]
	if selected.MinConvictionRao != 0 || selected.RateNumeratorRaoPerGiB == 0 || selected.RateDenominator == 0 {
		return DepositTier{}, errors.New("invalid conviction tier zero")
	}
	for index, tier := range policy.Tiers {
		if tier.RateNumeratorRaoPerGiB == 0 || tier.RateDenominator == 0 || (index > 0 && tier.MinConvictionRao <= policy.Tiers[index-1].MinConvictionRao) {
			return DepositTier{}, errors.New("invalid conviction tier schedule")
		}
		if conviction.Cmp(new(big.Int).SetUint64(tier.MinConvictionRao)) >= 0 {
			selected = tier
		}
	}
	return selected, nil
}

// RequiredDepositRao evaluates the canonical floor-and-cap formula without
// machine-word intermediate arithmetic:
//
//	floor(usage_bytes * rate_numerator / (GiB * rate_denominator))
//
// The result is capped by the per-operator epoch cap. A zero usage value is a
// valid zero deposit; malformed rates or conviction fail closed.
func RequiredDepositRao(usageBytes uint64, conviction *big.Int, policy DepositPolicy) (*big.Int, DepositTier, error) {
	tier, err := DepositTierAt(policy, conviction)
	if err != nil {
		return nil, DepositTier{}, err
	}
	if policy.EpochCapRaoPerOperator == 0 {
		return nil, DepositTier{}, errors.New("deposit epoch cap is zero")
	}
	amount := new(big.Int).Mul(new(big.Int).SetUint64(usageBytes), new(big.Int).SetUint64(tier.RateNumeratorRaoPerGiB))
	divisor := new(big.Int).Mul(new(big.Int).SetUint64(tier.RateDenominator), new(big.Int).SetUint64(1<<30))
	if divisor.Sign() == 0 {
		return nil, DepositTier{}, errors.New("deposit rate divisor is zero")
	}
	amount.Quo(amount, divisor)
	capRao := new(big.Int).SetUint64(policy.EpochCapRaoPerOperator)
	if amount.Cmp(capRao) > 0 {
		amount.Set(capRao)
	}
	return amount, tier, nil
}

type VerifyPolicy struct {
	TrailDepth                   int    `json:"trail_depth" yaml:"trail_depth"`
	StepTimeoutSeconds           int    `json:"step_timeout_seconds" yaml:"step_timeout_seconds"`
	StepTimeoutGraceSeconds      int    `json:"step_timeout_grace_seconds" yaml:"step_timeout_grace_seconds"`
	TrailTTLGraceSeconds         int    `json:"trail_ttl_grace_seconds" yaml:"trail_ttl_grace_seconds"`
	EgressTTLSeconds             int    `json:"egress_ttl_seconds" yaml:"egress_ttl_seconds"`
	EgressRefreshSeconds         int    `json:"egress_refresh_seconds" yaml:"egress_refresh_seconds"`
	ReliabilityAMin              uint64 `json:"reliability_a_min" yaml:"reliability_a_min"`
	StatsPeriodSeconds           int    `json:"stats_period_seconds" yaml:"stats_period_seconds"`
	EgressIPv4Prefix             int    `json:"egress_ipv4_prefix" yaml:"egress_ipv4_prefix"`
	EgressIPv6Prefix             int    `json:"egress_ipv6_prefix" yaml:"egress_ipv6_prefix"`
	EgressHashKeyID              string `json:"egress_hash_key_id" yaml:"egress_hash_key_id"`
	SoftGuardrailsEnabled        bool   `json:"soft_guardrails_enabled" yaml:"soft_guardrails_enabled"`
	HardSeedPerMinutePerSource   int    `json:"hard_seed_per_minute_per_source" yaml:"hard_seed_per_minute_per_source"`
	HardExtendPerMinutePerSource int    `json:"hard_extend_per_minute_per_source" yaml:"hard_extend_per_minute_per_source"`
	HardActiveTrailsPerSource    int    `json:"hard_active_trails_per_source" yaml:"hard_active_trails_per_source"`
}

type BindingPolicy struct {
	Schema                    string `json:"schema" yaml:"schema"`
	ChangesEffectiveNextEpoch bool   `json:"changes_effective_next_epoch" yaml:"changes_effective_next_epoch"`
	MaximumValidityEpochs     uint64 `json:"maximum_validity_epochs" yaml:"maximum_validity_epochs"`
	CommitmentsRequired       bool   `json:"commitments_required" yaml:"commitments_required"`
	ClientSignature           string `json:"client_signature" yaml:"client_signature"`
	HotkeySignature           string `json:"hotkey_signature" yaml:"hotkey_signature"`
}

type SafetyPolicy struct {
	MinimumHealthyNOCount         int  `json:"minimum_healthy_no_count" yaml:"minimum_healthy_no_count"`
	MinimumLiveValidatorCount     int  `json:"minimum_live_validator_count" yaml:"minimum_live_validator_count"`
	MaximumFinalizedHeadLagBlocks int  `json:"maximum_finalized_head_lag_blocks" yaml:"maximum_finalized_head_lag_blocks"`
	StopOnRuntimeChange           bool `json:"stop_on_runtime_change" yaml:"stop_on_runtime_change"`
	StopOnPolicyMismatch          bool `json:"stop_on_policy_mismatch" yaml:"stop_on_policy_mismatch"`
	StopOnIndexGap                bool `json:"stop_on_index_gap" yaml:"stop_on_index_gap"`
}

type Policy struct {
	Schema            string           `json:"schema" yaml:"schema"`
	NetworkProfile    string           `json:"network_profile" yaml:"network_profile"`
	PolicyID          uint64           `json:"policy_id" yaml:"policy_id"`
	EffectiveEpoch    uint64           `json:"effective_epoch" yaml:"effective_epoch"`
	Settlement        SettlementPolicy `json:"settlement" yaml:"settlement"`
	ProductionCadence CadenceSnapshot  `json:"production_cadence" yaml:"production_cadence"`
	Steering          SteeringPolicy   `json:"steering" yaml:"steering"`
	Deposit           DepositPolicy    `json:"deposit" yaml:"deposit"`
	Verify            VerifyPolicy     `json:"verify" yaml:"verify"`
	Binding           BindingPolicy    `json:"binding" yaml:"binding"`
	Safety            SafetyPolicy     `json:"safety" yaml:"safety"`
}

func LoadPolicy(path string) (*Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParsePolicy(b)
}

func ParsePolicy(b []byte) (*Policy, error) {
	var wrapper struct {
		Policy Policy `yaml:"policy"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("decode policy: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return nil, errors.New("decode policy: multiple YAML documents")
	}
	if err := wrapper.Policy.Validate(); err != nil {
		return nil, err
	}
	return &wrapper.Policy, nil
}

func (p Policy) Validate() error {
	if p.Schema != PolicySchema {
		return fmt.Errorf("policy schema %q, want %q", p.Schema, PolicySchema)
	}
	if p.NetworkProfile != "testnet" && p.NetworkProfile != "mainnet" {
		return errors.New("network_profile must be testnet or mainnet")
	}
	if p.PolicyID == 0 {
		return errors.New("policy_id is zero")
	}
	s := p.Settlement
	if s.EpochBlocks == 0 || s.RootCommitWindowBlocks == 0 || s.FinalizeOffsetBlocks == 0 || s.CloseGraceBlocks == 0 {
		return errors.New("settlement block windows must be nonzero")
	}
	if s.CloseGraceBlocks > s.RootCommitWindowBlocks || s.RootCommitWindowBlocks > s.FinalizeOffsetBlocks || s.FinalizeOffsetBlocks >= s.EpochBlocks {
		return errors.New("invalid settlement close/root/finalize windows")
	}
	production := p.ProductionCadence
	if production.AfterAcceleratedEpochs == 0 || production.EpochBlocks == 0 || production.RootCommitWindowBlocks == 0 || production.FinalizeOffsetBlocks == 0 || production.CloseGraceBlocks == 0 {
		return errors.New("production cadence fields must be nonzero")
	}
	if production.CloseGraceBlocks > production.RootCommitWindowBlocks || production.RootCommitWindowBlocks > production.FinalizeOffsetBlocks || production.FinalizeOffsetBlocks >= production.EpochBlocks || production.EpochBlocks <= s.EpochBlocks {
		return errors.New("invalid production cadence")
	}
	if p.NetworkProfile == "testnet" && production.EpochBlocks != 360 {
		return errors.New("testnet UR block must be exactly 360 chain blocks")
	}
	if p.NetworkProfile == "mainnet" && production.EpochBlocks != 50_400 {
		return errors.New("mainnet UR block must be exactly 50400 chain blocks")
	}
	if s.ClaimTTLEpochs == 0 || s.ClaimGraceEpochs > s.ClaimTTLEpochs {
		return errors.New("invalid claim ttl/grace")
	}
	// Every implementation derives a block horizon from the TTL, grace, and
	// one following epoch. Reject arithmetic which would wrap in the Go plan or
	// evidence paths even though Solidity itself would revert on overflow.
	const maximumUint64 = ^uint64(0)
	if s.ClaimTTLEpochs > maximumUint64-s.ClaimGraceEpochs {
		return errors.New("claim retention epoch count overflows uint64")
	}
	claimRetentionEpochs := s.ClaimTTLEpochs + s.ClaimGraceEpochs
	if claimRetentionEpochs == maximumUint64 {
		return errors.New("claim retention epoch count overflows uint64")
	}
	claimRetentionEpochs++
	if s.EpochBlocks > maximumUint64/claimRetentionEpochs || production.EpochBlocks > maximumUint64/claimRetentionEpochs {
		return errors.New("claim retention block horizon overflows uint64")
	}
	if s.MissedRootAction != "carry_same_operator" || s.ExpiredClaimAction != "carry_same_operator" {
		return errors.New("pool value may only carry to the same operator")
	}
	if s.SharesTotalBPS != 10_000 || s.Rounding != "largest_remainder_then_client_id_ascending" {
		return errors.New("unsupported share total or rounding policy")
	}
	if err := p.Steering.Theta.Validate("theta"); err != nil {
		return err
	}
	if p.Steering.Theta.Numerator > p.Steering.Theta.Denominator {
		return errors.New("theta exceeds one")
	}
	if err := p.Steering.HeadScoreEMA.Validate("head_score_ema"); err != nil {
		return err
	}
	if p.Steering.HeadScoreEMA.Numerator > p.Steering.HeadScoreEMA.Denominator {
		return errors.New("head_score_ema exceeds one")
	}
	if p.Steering.MaximumHeadFleets == 0 || p.Steering.MaximumHeadFleets > 200 {
		return errors.New("maximum_head_fleets must be in [1,200]")
	}
	if p.Steering.MaxWeightLimitU16 == 0 {
		return errors.New("max_weight_limit_u16 is zero")
	}
	q := p.Steering.QualityTransform
	if q.Kind != "clamp_ppm" || q.MinimumPPM > q.MaximumPPM || q.MaximumPPM > 1_000_000 {
		return errors.New("invalid quality clamp")
	}
	if p.Steering.EmptyChannel != "cede_to_nonempty" || p.Steering.Arithmetic != "integer_rational_v1" {
		return errors.New("unsupported steering semantics")
	}
	d := p.Deposit
	if d.Unit != "rao_per_gib" || d.EpochCapRaoPerOperator == 0 || d.TotalTestCampaignCapRao == 0 {
		return errors.New("invalid deposit unit or cap")
	}
	if d.TierSnapshot != "conviction_before_epoch" || d.UsageLagEpochs != 1 || d.UsageArtifact != "signed_payout_artifact_total_usage_bytes" || d.MismatchAction != "zero_pool_weight" || d.UnavailableAction != "zero_pool_weight_after_root_window" || d.ZeroOrInvalidRate != "halt" {
		return errors.New("unsupported deposit snapshot/failure policy")
	}
	if len(d.Tiers) == 0 || d.Tiers[0].MinConvictionRao != 0 {
		return errors.New("deposit tiers must begin at conviction zero")
	}
	tiers := append([]DepositTier(nil), d.Tiers...)
	sort.SliceStable(tiers, func(i, j int) bool { return tiers[i].MinConvictionRao < tiers[j].MinConvictionRao })
	for i, tier := range tiers {
		if tier.RateNumeratorRaoPerGiB == 0 || tier.RateDenominator == 0 {
			return fmt.Errorf("deposit tier %d has zero rate", i)
		}
		if i > 0 {
			if tier.MinConvictionRao <= tiers[i-1].MinConvictionRao {
				return errors.New("deposit conviction tiers are not strictly increasing")
			}
			if tier.rate().cmp(tiers[i-1].rate()) > 0 {
				return errors.New("deposit rate increases with conviction")
			}
		}
	}
	v := p.Verify
	if v.TrailDepth < 2 || v.StepTimeoutSeconds <= 0 || v.EgressTTLSeconds <= 0 || v.EgressRefreshSeconds <= 0 || v.EgressRefreshSeconds >= v.EgressTTLSeconds {
		return errors.New("invalid verify timing/depth")
	}
	if v.ReliabilityAMin == 0 || v.EgressIPv4Prefix < 0 || v.EgressIPv4Prefix > 32 || v.EgressIPv6Prefix < 0 || v.EgressIPv6Prefix > 128 || v.EgressHashKeyID == "" ||
		v.HardSeedPerMinutePerSource <= 0 || v.HardExtendPerMinutePerSource <= 0 || v.HardActiveTrailsPerSource <= 0 {
		return errors.New("invalid verify reliability/egress policy")
	}
	b := p.Binding
	if b.Schema != "urnetwork-fleet-binding-v1" || !b.ChangesEffectiveNextEpoch || b.MaximumValidityEpochs == 0 || !b.CommitmentsRequired || b.ClientSignature != "ed25519" || b.HotkeySignature != "sr25519" {
		return errors.New("unsupported binding policy")
	}
	if p.Safety.MinimumHealthyNOCount < 2 || p.Safety.MinimumLiveValidatorCount < 2 || p.Safety.MaximumFinalizedHeadLagBlocks <= 0 || !p.Safety.StopOnRuntimeChange || !p.Safety.StopOnPolicyMismatch || !p.Safety.StopOnIndexGap {
		return errors.New("unsafe safety policy")
	}
	if uint64(p.Steering.MaxWeightLimitU16)*uint64(p.Safety.MinimumHealthyNOCount) < uint64(^uint16(0)) {
		return errors.New("max_weight_limit_u16 is infeasible at the minimum healthy operator count")
	}
	return nil
}

func (p Policy) CanonicalBytes() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

func (p Policy) Hash() ([32]byte, error) {
	b, err := p.CanonicalBytes()
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(b), nil
}

func (p Policy) HashHex() (string, error) {
	h, err := p.Hash()
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(h[:]), nil
}
