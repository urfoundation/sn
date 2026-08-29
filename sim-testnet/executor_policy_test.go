package main

import (
	"math/big"
	"strings"
	"testing"

	"github.com/urfoundation/sn/stabi"
)

func TestProductionScheduleEpochRejectsEarlyAndLateFirstWrites(t *testing.T) {
	for _, test := range []struct {
		name             string
		current          uint64
		alreadyScheduled bool
		wantError        string
	}{
		{name: "early first write", current: 19, wantError: "requires 20 reconciled"},
		{name: "exact first write", current: 20},
		{name: "late first write", current: 21, wantError: "must be scheduled at exact epoch 20"},
		{name: "early adoption", current: 19, alreadyScheduled: true, wantError: "requires 20 reconciled"},
		{name: "same-epoch adoption", current: 20, alreadyScheduled: true},
		{name: "later adoption", current: 25, alreadyScheduled: true},
	} {
		err := validateProductionScheduleEpoch(test.current, 20, test.alreadyScheduled)
		if test.wantError == "" && err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
		if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
			t.Fatalf("%s: error = %v, want substring %q", test.name, err, test.wantError)
		}
	}
}

func TestProductionPolicyMatchRequiresExactBoundaryAndCompleteCaps(t *testing.T) {
	cfg := testResolvedConfig(t)
	hash, err := decodeHash(cfg.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Policy.ProductionCadence
	policy := stabi.STCoordinatorPolicySnapshot{
		PolicyHash: hash, EffectiveEpoch: p.AfterAcceleratedEpochs + 1, EffectiveBlock: 100,
		EpochBlocks: p.EpochBlocks, RootCommitWindowBlocks: p.RootCommitWindowBlocks,
		FinalizeOffsetBlocks: p.FinalizeOffsetBlocks, CloseGraceBlocks: p.CloseGraceBlocks,
		ClaimTTLEpochs: cfg.Policy.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: cfg.Policy.Settlement.ClaimGraceEpochs,
		MaximumBindingValidityEpochs: cfg.Policy.Binding.MaximumValidityEpochs, CommitmentMaxAgeBlocks: p.EpochBlocks * 2,
		EpochDepositCapRao:    new(big.Int).SetUint64(cfg.Policy.Deposit.EpochCapRaoPerOperator),
		CampaignDepositCapRao: new(big.Int).SetUint64(cfg.Policy.Deposit.TotalTestCampaignCapRao),
	}
	if !productionPolicyMatches(cfg, policy) {
		t.Fatal("canonical production policy was rejected")
	}
	policy.EffectiveEpoch++
	if productionPolicyMatches(cfg, policy) {
		t.Fatal("late production policy was accepted")
	}
	policy.EffectiveEpoch--
	policy.EpochDepositCapRao = nil
	if productionPolicyMatches(cfg, policy) {
		t.Fatal("production policy with a missing deposit cap was accepted")
	}
}

func TestResumeCurrentPostconditionClassificationIsNarrow(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{id: "evm.fund-deployer", want: false},
		{id: "fleet.fund.1", want: false},
		{id: "fleet.fund-hotkey.1", want: false},
		{id: "churn.fund.1", want: false},
		{id: "validator.fund.1", want: false},
		{id: "alpha.transfer.operator-deposit.1", want: false},
		{id: "alpha.transfer.validator.1", want: true},
		{id: "fleet.register.1", want: true},
		{id: "operator.register.1", want: true},
		{id: "production.schedule-policy", want: true},
		{id: "precompile.commitment-write", want: true},
		{id: "governance.guardian-pause", want: true},
		{id: "topology.launch", want: true},
	}
	for _, test := range tests {
		if got := actionRequiresCurrentPostcondition(Action{ID: test.id}); got != test.want {
			t.Fatalf("action %s current revalidation=%t, want %t", test.id, got, test.want)
		}
	}
}

func TestInitialRegistrationPositionStopsAtTopologyBarrier(t *testing.T) {
	plan := &SetupPlan{Actions: []Action{
		{ID: "register.a", Spend: Spend{Registrations: 1}},
		{ID: "unrelated"},
		{ID: "register.b", Spend: Spend{Registrations: 1}},
		{ID: "topology.launch"},
		{ID: "fleet.register.201", Spend: Spend{Registrations: 1}},
	}}
	position, err := initialRegistrationPosition(plan, "register.b")
	if err != nil {
		t.Fatal(err)
	}
	if !position.Applicable || position.PriorRegistrations != 1 || position.TotalRegistrations != 2 || len(position.PriorActionIDs) != 1 || position.PriorActionIDs[0] != "register.a" || len(position.LaterActionIDs) != 0 {
		t.Fatalf("unexpected initial registration position: %+v", position)
	}
	challenger, err := initialRegistrationPosition(plan, "fleet.register.201")
	if err != nil || challenger.Applicable {
		t.Fatalf("challenger position=%+v error=%v, want non-applicable", challenger, err)
	}
	for _, malformed := range []*SetupPlan{
		nil,
		{Actions: []Action{{ID: "register.a", Spend: Spend{Registrations: 1}}}},
		{Actions: []Action{{ID: "topology.launch"}, {ID: "topology.launch"}, {ID: "register.a"}}},
	} {
		if _, err := initialRegistrationPosition(malformed, "register.a"); err == nil {
			t.Fatalf("malformed plan %+v was accepted", malformed)
		}
	}
}

func TestInitialRegistrationPreStateRejectsCapacityRacesAndOutOfOrderSetup(t *testing.T) {
	canonical := initialRegistrationPreState{
		ExistingUIDs: 2, PriorRegistrations: 47, CurrentActionRegistrations: 1,
		TotalRegistrations: 254, CurrentUIDs: 49, MaximumUIDs: 256,
		PriorActionsVerified: true, LaterActionsUnverified: true,
	}
	if err := validateInitialRegistrationPreState(canonical); err != nil {
		t.Fatalf("canonical pre-state: %v", err)
	}
	resumed := canonical
	resumed.CurrentUIDs++
	resumed.CurrentActionObserved = true
	if err := validateInitialRegistrationPreState(resumed); err != nil {
		t.Fatalf("canonical resumed post-state: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*initialRegistrationPreState)
		want   string
	}{
		{name: "external registration", mutate: func(s *initialRegistrationPreState) { s.CurrentUIDs++ }, want: "exact pre-state"},
		{name: "missing prior registration", mutate: func(s *initialRegistrationPreState) { s.CurrentUIDs-- }, want: "exact pre-state"},
		{name: "prior unverified", mutate: func(s *initialRegistrationPreState) { s.PriorActionsVerified = false }, want: "earlier"},
		{name: "later verified", mutate: func(s *initialRegistrationPreState) { s.LaterActionsUnverified = false }, want: "out of order"},
		{name: "plan underfills", mutate: func(s *initialRegistrationPreState) { s.TotalRegistrations-- }, want: "does not exactly fill"},
		{name: "multi registration action", mutate: func(s *initialRegistrationPreState) { s.CurrentActionRegistrations = 2 }, want: "exactly one"},
		{name: "resumed without resulting UID", mutate: func(s *initialRegistrationPreState) { s.CurrentActionObserved = true }, want: "exact post-state"},
	}
	for _, test := range tests {
		state := canonical
		test.mutate(&state)
		if err := validateInitialRegistrationPreState(state); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: error=%v, want substring %q", test.name, err, test.want)
		}
	}
}

func TestHyperparameterUint64AcceptsRuntimeUnsignedShapes(t *testing.T) {
	for _, value := range []any{uint8(7), uint16(7), uint32(7), uint64(7), uint(7), int8(7), int16(7), int32(7), int64(7), int(7), float64(7)} {
		if got := hyperparameterUint64(value); got != 7 {
			t.Fatalf("hyperparameterUint64(%T(%v))=%d, want 7", value, value, got)
		}
	}
	for _, value := range []any{int(-1), int64(-1), "7", nil} {
		if got := hyperparameterUint64(value); got != 0 {
			t.Fatalf("hyperparameterUint64(%T(%v))=%d, want 0", value, value, got)
		}
	}
}

func TestInitialImmunityPostconditionTracksOnlyItsVerifiedSuccessor(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Hyperparameters.OwnerControlled["immunity_period"] = 7200
	cfg.Hyperparameters.ProductionOwnerControlled["immunity_period"] = 2400
	initial, successor, err := lifecycleHyperparameterExpectation(cfg, "immunity_period", false)
	if err != nil || initial != 7200 || successor != "" {
		t.Fatalf("initial expectation=%v successor=%q err=%v", initial, successor, err)
	}
	production, successor, err := lifecycleHyperparameterExpectation(cfg, "immunity_period", true)
	if err != nil || production != 2400 || successor != "production.hyperparameter.immunity_period" {
		t.Fatalf("production expectation=%v successor=%q err=%v", production, successor, err)
	}
	tempo, successor, err := lifecycleHyperparameterExpectation(cfg, "tempo", false)
	if err != nil || tempo != 360 || successor != "" {
		t.Fatalf("unrelated expectation=%v successor=%q err=%v", tempo, successor, err)
	}
	delete(cfg.Hyperparameters.ProductionOwnerControlled, "immunity_period")
	if _, _, err := lifecycleHyperparameterExpectation(cfg, "immunity_period", true); err == nil {
		t.Fatal("missing production immunity value was accepted")
	}
	burnHalfLife, successor, err := lifecycleHyperparameterExpectation(cfg, "burn_half_life", true)
	if err != nil || burnHalfLife != 360 || successor != "production.hyperparameter.burn_half_life" {
		t.Fatalf("production burn half-life expectation=%v successor=%q err=%v", burnHalfLife, successor, err)
	}
}
