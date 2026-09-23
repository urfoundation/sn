// Real signed retained fixtures exercise policy migration without rewriting
// old approval, activation, payout, or failed campaign evidence.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"
)

// A cold/warm recovery traverses the old signed failed chain under its own
// policy. Its new attempt remains bound only to the approved successor.
func TestPolicyRateAmendmentCampaignRetainsSignedHistoricalChain(t *testing.T) {
	fixture := newCampaignSuccessionFixture(t)
	first, second, _ := createSecondCampaignRecovery(t, fixture)
	bindFailedRecoveryGeneration(t, fixture, second, 10)
	previous, previousPlan := *fixture.cfg.Policy, fixture.current.PlanHash
	fixture.cfg.Policy = rateAmendmentTestPolicy(t, &previous)
	var err error
	fixture.cfg.PolicyHash, err = fixture.cfg.Policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	// The reduced signed-chain fixture owns no deployment scheduler. Select
	// both policy documents before its render/topology fixture transition;
	// the full production revision path is exercised separately below.
	current := *fixture.current
	current.PolicyHash = fixture.cfg.PolicyHash
	current.PolicyRateAmendment = &PolicyRateAmendment{Schema: policyRateAmendmentSchema, PriorPlanHash: previousPlan, Previous: previous, Next: *fixture.cfg.Policy}
	fixture.current = &current
	advanceCampaignLineageFixture(t, fixture)
	fixture.current.ResolvedInputsHash, err = resolvedInputsHash(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	fixture.writeCurrent(t)
	fixture.cfg.provisionalResume.Record.PlanHash = fixture.current.PlanHash
	before := retainedCampaignLineageBytes(t, fixture.stateDir)
	if _, err := readScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0"); !errors.Is(err, errScenarioCampaignHistoricalLineage) {
		t.Fatalf("old policy admitted as current: %v", err)
	}
	attempt, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(3*time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	for _, ancestor := range []string{first.payload.RunID, second.payload.RunID} {
		for range 2 {
			if err := validateScenarioCampaignRecoveryAncestor(attempt, ancestor); err != nil {
				t.Fatalf("old policy cold/warm proof: %v", err)
			}
		}
	}
	if attempt.payload.PolicyHash != fixture.cfg.PolicyHash || attempt.cfg.Policy.Deposit.Tiers[0].RateNumeratorRaoPerGiB != 40_000_000_000 {
		t.Fatal("ancestor cache changed current policy")
	}
	for path, raw := range before {
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(raw, got) {
			t.Fatalf("historical bytes changed %s: %v", path, err)
		}
	}
}

// The real original activation signatures remain unchanged while the new
// renderer includes the exact prior policy as an independent replay authority.
func TestPolicyRateAmendmentCarriesOriginalActivationAndRendersBridge(t *testing.T) {
	fixture := newRuntimeEvidenceSetupOriginalCarryV2Test(t)
	entries := retainRuntimeEvidenceSetupCarryV2Test(t, fixture)
	previous := *fixture.cfg.Policy
	oldCfg := *fixture.cfg
	fixture.cfg.Policy = rateAmendmentTestPolicy(t, &previous)
	var err error
	fixture.cfg.PolicyHash, err = fixture.cfg.Policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2} {
		writeRenderedPolicy(t, fixture.stateDir, id, &previous, oldCfg.PolicyHash)
	}
	current := fixture.plan.LiveFacts
	current.DeployerNonce = fixture.plan.ValidatorEvidence.DeployerNonce + 1
	next, err := buildPlanRevisionFromFacts(fixture.cfg, fixture.stateDir, fixture.plan, &current, entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(fixture.stateDir, "plan.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	before := validatorNamespaceTreeSnapshot(t, fixture.stateDir)
	if _, err := runtimeEvidenceSetupSourcePlanV2(fixture.cfg, next, fixture.stateDir, fixture.roles, fixture.prepared, fixture.preparedBytes, fixture.completed, entries); err != nil {
		t.Fatal(err)
	}
	resolved, err := runtimeEvidenceV2ResolvedConfig(fixture.cfg, fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	expected, _, err := runtimeEvidenceFixedInputsV2(&oldCfg, fixture.plan, fixture.stateDir, fixture.roles, fixture.prepared, fixture.completed)
	if err != nil || !reflect.DeepEqual(expected, resolved.Config.ValidatorEvidenceV2) {
		t.Fatalf("activation domain changed: %v", err)
	}
	wire, err := marshalRuntimeValidatorConfig(resolved, fixture.stateDir, fixture.roles, map[string]any{"policy_hash": resolved.PolicyHash}, 1)
	if err != nil {
		t.Fatal(err)
	}
	var rendered map[string]any
	if err := yaml.Unmarshal(wire, &rendered); err != nil {
		t.Fatal(err)
	}
	if rendered["previous_policy"] == nil || rendered["policy_hash"] != resolved.PolicyHash || !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, fixture.stateDir)) {
		t.Fatal("render omitted bridge or rewrote immutable source")
	}
	if err := preservePolicyRateAmendmentSetup(next, fixture.plan, nil); err == nil {
		t.Fatal("unverified activation acquired historical authority")
	}
}

// Signed old artifacts remain verifiable while a newly signed current-policy
// artifact becomes the newest selection. Cached metadata binds both policies.
func TestPolicyRateAmendmentRetainsSignedPayoutHistory(t *testing.T) {
	fixture := newMerkleHistoryFixture(t)
	previous := *fixture.cfg.Policy
	fixture.cfg.Policy = rateAmendmentTestPolicy(t, &previous)
	fixture.cfg.previousPolicy = &previous
	fixture.cfg.PolicyHash, _ = fixture.cfg.Policy.HashHex()
	before := map[string][]byte{}
	for hash, wire := range fixture.bodyKVs {
		before[hash] = bytes.Clone(wire)
	}
	fixture.add(t, 4, 4)
	for range 2 {
		artifact, err := fixture.selectArtifact("rate-successor", fixture.get)
		if err != nil || artifact.Epoch != 4 || artifact.PolicyHash != fixture.cfg.PolicyHash {
			t.Fatalf("signed policy history: %+v %v", artifact, err)
		}
	}
	for hash, wire := range before {
		if !bytes.Equal(wire, fixture.bodyKVs[hash]) {
			t.Fatal("old signed payout changed")
		}
	}
	fixture.cfg.previousPolicy = nil
	if _, err := fixture.selectArtifact("rate-successor", fixture.get); err == nil {
		t.Fatal("warm cache retained removed prior policy authority")
	}
}

// The usage floor is conservative across all tiers and strict about the
// one-epoch lag, policy activation, source identity and native price.
func TestPolicyRateAmendmentReadinessRequiresCompleteViableLaggedUsage(t *testing.T) {
	cfg, _, plan := rateAmendmentTestPlans(t)
	cfg, err := configWithPolicyRateAmendment(cfg, plan)
	if err != nil {
		t.Fatal(err)
	}
	contracts := &ContractView{PolicyHash: cfg.PolicyHash, CurrentEpoch: 12, Policy: PolicyView{EffectiveEpoch: 11}, MinimumTransferRao: 100_000}
	sources := []PolicyRateSourceObservation{{NoId: 1, Epoch: 11, PolicyHash: cfg.PolicyHash, ContentHash: "sha256:" + stringsTrim0x(common.Hash{1}.Hex()), TotalUsageBytes: 24 * 1024 * 1024}, {NoId: 2, Epoch: 11, PolicyHash: cfg.PolicyHash, ContentHash: "sha256:" + stringsTrim0x(common.Hash{2}.Hex()), TotalUsageBytes: 24 * 1024 * 1024}}
	price := big.NewInt(500_000_000_000_000)
	if err := validatePolicyRateReadiness(cfg, contracts, sources, price); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*PolicyRateSourceObservation){
		func(row *PolicyRateSourceObservation) { row.TotalUsageBytes = 8 * 1024 * 1024 },
		func(row *PolicyRateSourceObservation) { row.Epoch-- },
		func(row *PolicyRateSourceObservation) { row.Epoch++ },
		func(row *PolicyRateSourceObservation) { row.PolicyHash = common.Hash{9}.Hex() },
		func(row *PolicyRateSourceObservation) { row.NoId = 2 },
		func(row *PolicyRateSourceObservation) { row.ContentHash = "unverified" },
	} {
		changed := append([]PolicyRateSourceObservation(nil), sources...)
		edit(&changed[0])
		if validatePolicyRateReadiness(cfg, contracts, changed, price) == nil {
			t.Fatal("incomplete or insufficient source was admitted")
		}
	}
	if validatePolicyRateReadiness(cfg, contracts, sources, big.NewInt(100_000_000_000_000)) == nil {
		t.Fatal("price deterioration bypassed margin")
	}
	contracts.CurrentEpoch = 11
	if validatePolicyRateReadiness(cfg, contracts, sources, price) == nil {
		t.Fatal("activation partial epoch counted as complete source")
	}
}

// The actual planner must preserve the original constructor and activation
// intents while producing a distinct schedule and reserving its additional gas.
func TestPolicyRateAmendmentPlanRevisionDoesNotReplaySetup(t *testing.T) {
	fixture := newRuntimeEvidenceSetupOriginalCarryV2Test(t)
	entries := retainRuntimeEvidenceSetupCarryV2Test(t, fixture)
	previous := *fixture.cfg.Policy
	for _, id := range []int{1, 2} {
		writeRenderedPolicy(t, fixture.stateDir, id, &previous, fixture.cfg.PolicyHash)
	}
	fixture.cfg.Policy = rateAmendmentTestPolicy(t, &previous)
	var err error
	fixture.cfg.PolicyHash, err = fixture.cfg.Policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	current := fixture.plan.LiveFacts
	current.DeployerNonce = fixture.plan.ValidatorEvidence.DeployerNonce + 1
	next, err := buildPlanRevisionFromFacts(fixture.cfg, fixture.stateDir, fixture.plan, &current, entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if validatePolicyRateAmendmentPlan(next) != nil || next.Limits != fixture.plan.Limits || !contractDeploymentAddressesEqual(next.Deployment, fixture.plan.Deployment) {
		t.Fatal("rate revision changed custody or allowance")
	}
	for _, before := range fixture.plan.Actions {
		if before.ID == runtimeEvidenceActivationBoundaryActionId || strings.HasPrefix(before.ID, "evidence.activate.") {
			after := actionByID(t, next, before.ID)
			if before.IntentHash != after.IntentHash {
				t.Fatalf("original activation %s changed", before.ID)
			}
		}
	}
	for _, id := range []string{"policy.schedule-bootstrap", "policy.await-bootstrap"} {
		before, after := actionByID(t, fixture.plan, id), actionByID(t, next, id)
		if before.IntentHash == after.IntentHash || !policyRateAmendmentSetupRepair(next, after) {
			t.Fatalf("policy action %s not exactly revised", id)
		}
		changed := after
		changed.Target = common.Address{0x55}.Hex()
		if policyRateAmendmentSetupRepair(next, changed) {
			t.Fatal("foreign governance target accepted")
		}
	}
}
