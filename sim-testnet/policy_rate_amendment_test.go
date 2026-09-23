// Synthetic approvals prove that policy history and custody stay immutable
// while one exact future governance action acquires its own intent.
package main

import (
	"bytes"
	"encoding/json"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// Clone every mutable rate field to make the old document an independent owner.
func rateAmendmentTestPolicy(t *testing.T, previous *protocol.Policy) *protocol.Policy {
	t.Helper()
	next := *previous
	next.PolicyID++
	next.Deposit.Tiers = append([]protocol.DepositTier(nil), previous.Deposit.Tiers...)
	for index, rate := range []uint64{40_000_000_000, 32_000_000_000, 24_000_000_000} {
		next.Deposit.Tiers[index].RateNumeratorRaoPerGiB = rate
	}
	if err := validateFuturePolicyRateAmendment(previous, &next); err != nil {
		t.Fatal(err)
	}
	return &next
}

// A synthetic old plan and exact future policy provide the shared proof seam.
func rateAmendmentTestPlans(t *testing.T) (*ResolvedConfig, *SetupPlan, *SetupPlan) {
	t.Helper()
	cfg := testResolvedConfig(t)
	previous := *cfg.Policy
	old := &SetupPlan{PlanHash: common.Hash{0x41}.Hex(), PolicyHash: cfg.PolicyHash}
	cfg.Policy = rateAmendmentTestPolicy(t, &previous)
	var err error
	cfg.PolicyHash, err = cfg.Policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	next := &SetupPlan{PlanHash: common.Hash{0x42}.Hex(), PolicyHash: cfg.PolicyHash, PriorPlanHashes: []string{old.PlanHash},
		PolicyRateAmendment: &PolicyRateAmendment{Schema: policyRateAmendmentSchema, PriorPlanHash: old.PlanHash, Previous: previous, Next: *cfg.Policy}}
	return cfg, old, next
}

// Every coordinator-visible field is populated rather than accepting a hash
// without its custody caps and settlement geometry.
func rateAmendmentTestSnapshot(cfg *ResolvedConfig, epoch, block uint64) stabi.STCoordinatorPolicySnapshot {
	return stabi.STCoordinatorPolicySnapshot{
		PolicyHash: [32]byte(common.HexToHash(cfg.PolicyHash)), EffectiveEpoch: epoch, EffectiveBlock: block,
		EpochBlocks: cfg.Policy.Settlement.EpochBlocks, RootCommitWindowBlocks: cfg.Policy.Settlement.RootCommitWindowBlocks,
		FinalizeOffsetBlocks: cfg.Policy.Settlement.FinalizeOffsetBlocks, CloseGraceBlocks: cfg.Policy.Settlement.CloseGraceBlocks,
		ClaimTTLEpochs: cfg.Policy.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: cfg.Policy.Settlement.ClaimGraceEpochs,
		MaximumBindingValidityEpochs: cfg.Policy.Binding.MaximumValidityEpochs, CommitmentMaxAgeBlocks: cfg.Policy.Settlement.EpochBlocks * 2,
		EpochDepositCapRao: new(big.Int).SetUint64(cfg.Policy.Deposit.EpochCapRaoPerOperator), CampaignDepositCapRao: new(big.Int).SetUint64(cfg.Policy.Deposit.TotalTestCampaignCapRao),
	}
}

// In-flight and activated successors are idempotent, but a foreign schedule,
// cap, epoch or exhausted policy inventory remains a hard refusal.
func TestPolicyRateAmendmentAuthenticatesFutureSchedule(t *testing.T) {
	cfg, old, next := rateAmendmentTestPlans(t)
	previous := &next.PolicyRateAmendment.Previous
	prior := *cfg
	prior.Policy, prior.PolicyHash = previous, old.PolicyHash
	active := rateAmendmentTestSnapshot(&prior, 4, 1000)
	pending := rateAmendmentTestSnapshot(cfg, 11, 3100)
	for _, state := range []struct {
		current, count uint64
		active, last   stabi.STCoordinatorPolicySnapshot
	}{
		{current: 10, count: 2, active: active, last: active},
		{current: 10, count: 3, active: active, last: pending},
		{current: 11, count: 3, active: pending, last: pending},
	} {
		if err := validatePolicyRateSchedule(cfg, previous, state.current, state.count, state.active, state.last); err != nil {
			t.Fatal(err)
		}
	}
	for _, mutation := range []func(*stabi.STCoordinatorPolicySnapshot){
		func(value *stabi.STCoordinatorPolicySnapshot) { value.PolicyHash[0] ^= 1 },
		func(value *stabi.STCoordinatorPolicySnapshot) { value.EpochDepositCapRao = big.NewInt(1) },
		func(value *stabi.STCoordinatorPolicySnapshot) { value.EffectiveEpoch = 10 },
		func(value *stabi.STCoordinatorPolicySnapshot) { value.EffectiveBlock = active.EffectiveBlock },
		func(value *stabi.STCoordinatorPolicySnapshot) { value.EpochBlocks++ },
	} {
		changed := pending
		mutation(&changed)
		if err := validatePolicyRateSchedule(cfg, previous, 10, 3, active, changed); err == nil {
			t.Fatal("foreign or nonfuture policy accepted")
		}
	}
	if err := validatePolicyRateSchedule(cfg, previous, 10, 64, active, active); err == nil {
		t.Fatal("full policy inventory accepted")
	}
}

// The old document survives both historical replay and runtime rendering.
func TestPolicyRateAmendmentKeepsExactHistoricalPolicy(t *testing.T) {
	cfg, old, next := rateAmendmentTestPlans(t)
	before, _ := json.Marshal(next.PolicyRateAmendment.Previous)
	if !policyRateAmendmentAllowsAncestor(next, old) {
		t.Fatal("exact predecessor rejected")
	}
	view := historicalPlanConfig(cfg, old, next)
	if view.PolicyHash != old.PolicyHash || view.Policy.Deposit.Tiers[0].RateNumeratorRaoPerGiB != 1_000_000 || cfg.Policy.Deposit.Tiers[0].RateNumeratorRaoPerGiB != 40_000_000_000 {
		t.Fatal("old and current policy were mixed")
	}
	view.Policy.Deposit.Tiers[0].RateNumeratorRaoPerGiB++
	after, _ := json.Marshal(next.PolicyRateAmendment.Previous)
	if !bytes.Equal(before, after) {
		t.Fatal("historical replay mutated approval")
	}
	resolved, err := configWithPolicyRateAmendment(cfg, next)
	if err != nil || resolved.previousPolicy == nil || !reflect.DeepEqual(*resolved.previousPolicy, next.PolicyRateAmendment.Previous) || cfg.previousPolicy != nil {
		t.Fatalf("runtime proof ownership: %v", err)
	}
	foreign := *old
	foreign.PolicyHash = common.Hash{0x77}.Hex()
	if policyRateAmendmentAllowsAncestor(next, &foreign) {
		t.Fatal("foreign policy admitted")
	}
	next.PolicyRateAmendment.PriorPlanHash = common.Hash{0x78}.Hex()
	if validatePolicyRateAmendmentPlan(next) == nil {
		t.Fatal("unapproved predecessor admitted")
	}
}

// Classification authenticates two exact rendered documents and does not
// mistake the rate amendment for pre-campaign reserve accounting.
func TestPolicyRateAmendmentClassificationChecksBothPolicies(t *testing.T) {
	cfg, old, next := rateAmendmentTestPlans(t)
	stateDir := t.TempDir()
	for _, id := range []int{1, 2} {
		writeRenderedPolicy(t, stateDir, id, &next.PolicyRateAmendment.Previous, old.PolicyHash)
	}
	entries := []JournalEntry{{PlanHash: old.PlanHash, ActionID: "topology.launch", Stage: StageVerified}}
	decision, err := classifyPolicyRevision(cfg, stateDir, old, entries)
	if err != nil || decision.Class != policyRevisionFutureRate || !decision.RestartRequired {
		t.Fatalf("rate revision refused: %+v %v", decision, err)
	}
	changed := next.PolicyRateAmendment.Previous
	changed.Deposit.EpochCapRaoPerOperator++
	writeRenderedPolicy(t, stateDir, 2, &changed, old.PolicyHash)
	if _, err := classifyPolicyRevision(cfg, stateDir, old, entries); err == nil || !strings.Contains(err.Error(), "authenticate") {
		t.Fatalf("tampered old document accepted: %v", err)
	}
}
