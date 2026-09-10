package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func newValidatorEvidenceActivationCarryTestFixture(t *testing.T) validatorEvidenceCarryTestFixture {
	t.Helper()
	return newValidatorEvidenceCarryConfiguredTestFixture(t, true, false, false, func(cfg *ResolvedConfig) {
		configureRuntimeEvidenceV2Test(t, cfg, t.TempDir())
		cfg.Config.ValidatorEvidenceV2 = runtimeEvidenceTemplateV2(cfg.Config.ValidatorEvidenceV2)
		cfg.Config.ProvisionValidatorEvidenceV2 = true
		cfg.Config.ValidatorEvidenceActivationGasUnits = 1_000_000
		for index := range cfg.Config.ValidatorEvidenceV2 {
			bounds := &cfg.Config.ValidatorEvidenceV2[index].Evidence.Bounds
			bounds.Cut.MaxHeaderBytes = 64 * 1024
			bounds.Cut.Records.MaxPageBytes = bounds.Cut.MaxHeaderBytes
		}
	})
}

func TestValidatorEvidenceCarryRevisionBindsFreshActivationAndRelayOwners(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceActivationCarryTestFixture(t)
	observed := fixture.authenticate(t)
	executor := fixture.executor
	prior := *executor.plan
	prior.validatorEvidenceObserved = observed
	current := prior.LiveFacts
	current.DeployerNonce = prior.ValidatorEvidence.DeployerNonce + 1
	before, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	revised, err := buildPlanRevisionFromFacts(executor.cfg, executor.stateDir, &prior, &current, executor.journal.Entries(), time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if *revised.ValidatorEvidence != *prior.ValidatorEvidence {
		t.Fatal("retained companion changed")
	}
	prepared := &runtimeEvidenceActivationPreparedV2{Members: make([]runtimeEvidenceActivationMemberV2, 4)}
	consumer := &Executor{cfg: executor.cfg, plan: revised}
	count := 0
	for _, action := range revised.Actions {
		activation := strings.HasPrefix(action.ID, "evidence.activate.")
		if !activation && action.ID != runtimeEvidenceActivationBoundaryActionId && action.ID != evidenceRelayReserveId {
			continue
		}
		count++
		if action.Target != prior.ValidatorEvidence.Address.Hex() {
			t.Errorf("%s retained companion action targets predicted replacement %s, want %s", action.ID, action.Target, prior.ValidatorEvidence.Address.Hex())
		}
		if action.ID == evidenceRelayReserveId && action.Parameters["runtime_code_hash"] != prior.ValidatorEvidence.RuntimeCodeHash.Hex() {
			t.Error("relay reserve lost retained runtime identity")
		}
		if action.ID == evidenceRelayReserveId {
			if _, _, _, err := evidenceRelayPlanAllowance(revised, action); err != nil {
				t.Errorf("retained relay allowance refused: %v", err)
			}
		}
		intent, err := actionIntentHash(action)
		if err != nil || intent != action.IntentHash {
			t.Errorf("%s rebind has invalid approved intent: %v", action.ID, err)
		}
		old := actionByID(t, &prior, action.ID)
		if action.Spend != old.Spend {
			t.Errorf("%s changed approved spend", action.ID)
		}
		if !reflect.DeepEqual(action.AcceptedPriorIntentHashes, old.AcceptedPriorIntentHashes) {
			t.Errorf("%s grandfathered stale quota identity", action.ID)
		}
		if activation {
			if _, err := consumer.runtimeEvidenceActivationMemberV2(action, prepared); err != nil {
				t.Errorf("approved carried activation refused: %v", err)
			}
			changed := action
			changed.Target = "0x0000000000000000000000000000000000000001"
			if _, err := consumer.runtimeEvidenceActivationMemberV2(changed, prepared); err == nil {
				t.Error("foreign activation target consumed retained quota")
			}
			changed = action
			changed.Spend.EVMGasWei = decimalUint64(1)
			if _, err := consumer.runtimeEvidenceActivationMemberV2(changed, prepared); err == nil {
				t.Error("changed quota consumed retained approved intent")
			}
		}
	}
	if count != 6 {
		t.Fatalf("dependent source census=%d, want6", count)
	}
	for _, id := range []string{validatorEvidenceDeployActionID, validatorEvidenceAnchorActionID} {
		if !reflect.DeepEqual(actionByID(t, revised, id), actionByID(t, &prior, id)) {
			t.Errorf("carried original %s changed", id)
		}
	}
	if err := validatePlanBudget(revised); err != nil {
		t.Fatal(err)
	}
	if revised.MaximumSpend != prior.MaximumSpend || revised.SupersededSpend != prior.SupersededSpend || !reflect.DeepEqual(revised.Limits, prior.Limits) {
		t.Error("carry changed maximum/superseded spend or limits")
	}
	// Budget reservations are not actual spending, including a previously
	// verified reservation under its old approved action intent.
	entries := append(executor.journal.Entries(), JournalEntry{PlanHash: prior.PlanHash, ActionID: evidenceRelayReserveId, IntentHash: actionByID(t, &prior, evidenceRelayReserveId).IntentHash, Stage: StageVerified})
	oldRemaining, oldErr := remainingPlanSpend(&prior, entries)
	newRemaining, newErr := remainingPlanSpend(revised, entries)
	if oldErr != nil || newErr != nil || oldRemaining != newRemaining {
		t.Errorf("verified relay reservation changed remaining spend: %v %v", oldErr, newErr)
	}
	after, err := json.Marshal(prior)
	if err != nil || string(before) != string(after) {
		t.Fatal("source approval mutated")
	}
	if fixture.rpc.sends.Load() != 0 {
		t.Fatal("read-only carry submitted a transaction")
	}
}
