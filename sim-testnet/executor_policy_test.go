package main

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/stabi"
)

func TestProductionScheduleEpochUsesCompletedLiveCampaignAndAllowsLateResume(t *testing.T) {
	for _, test := range []struct {
		name               string
		current            uint64
		scheduledEffective uint64
		wantError          string
	}{
		{name: "campaign incomplete", current: 45, wantError: "through epoch 46"},
		{name: "exact campaign boundary", current: 46},
		{name: "late first write", current: 52},
		{name: "late interrupted adoption", current: 58, scheduledEffective: 53},
		{name: "schedule overlaps campaign", current: 52, scheduledEffective: 46, wantError: "does not follow"},
		{name: "early adoption", current: 45, scheduledEffective: 47, wantError: "through epoch 46"},
	} {
		err := validateProductionScheduleEpoch(test.current, 46, test.scheduledEffective)
		if test.wantError == "" && err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
		if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
			t.Fatalf("%s: error = %v, want substring %q", test.name, err, test.wantError)
		}
	}
}

func TestProductionPolicyMatchAllowsCampaignRelativeEpochAndRequiresCompleteCaps(t *testing.T) {
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
	policy.EffectiveEpoch = 53
	if !productionPolicyMatches(cfg, policy) {
		t.Fatal("campaign-relative production policy was rejected")
	}
	policy.EffectiveEpoch = 0
	if productionPolicyMatches(cfg, policy) {
		t.Fatal("zero-effective production policy was accepted")
	}
	policy.EffectiveEpoch = 53
	policy.EpochDepositCapRao = nil
	if productionPolicyMatches(cfg, policy) {
		t.Fatal("production policy with a missing deposit cap was accepted")
	}
}

func TestProductionPolicyReceiptBindsExactScheduledSnapshot(t *testing.T) {
	cfg := testResolvedConfig(t)
	hash, err := decodeHash(cfg.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Policy.ProductionCadence
	policy := stabi.STCoordinatorPolicySnapshot{
		PolicyHash: hash, EffectiveEpoch: 53, EffectiveBlock: 12_345,
		EpochBlocks: p.EpochBlocks, RootCommitWindowBlocks: p.RootCommitWindowBlocks,
		FinalizeOffsetBlocks: p.FinalizeOffsetBlocks, CloseGraceBlocks: p.CloseGraceBlocks,
		ClaimTTLEpochs: cfg.Policy.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: cfg.Policy.Settlement.ClaimGraceEpochs,
		MaximumBindingValidityEpochs: cfg.Policy.Binding.MaximumValidityEpochs, CommitmentMaxAgeBlocks: p.EpochBlocks * 2,
		EpochDepositCapRao:    new(big.Int).SetUint64(cfg.Policy.Deposit.EpochCapRaoPerOperator),
		CampaignDepositCapRao: new(big.Int).SetUint64(cfg.Policy.Deposit.TotalTestCampaignCapRao),
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	event := parsed.Events["PolicyScheduled"]
	data, err := event.Inputs.NonIndexed().Pack(policy.EffectiveBlock)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := common.HexToAddress("0x1234")
	receipt := &ethTypes.Receipt{Status: ethTypes.ReceiptStatusSuccessful, Logs: []*ethTypes.Log{{
		Address: coordinator,
		Topics:  []common.Hash{event.ID, common.BigToHash(big.NewInt(1)), common.BytesToHash(policy.PolicyHash[:]), common.BigToHash(new(big.Int).SetUint64(policy.EffectiveEpoch))},
		Data:    data,
	}}}
	if err := productionPolicyReceiptMatches(receipt, coordinator, policy, 1); err != nil {
		t.Fatal(err)
	}
	if err := productionPolicyReceiptMatches(receipt, coordinator, policy, 2); err == nil {
		t.Fatal("wrong policy-history index was accepted")
	}
	receipt.Status = ethTypes.ReceiptStatusFailed
	if err := productionPolicyReceiptMatches(receipt, coordinator, policy, 1); err == nil {
		t.Fatal("reverted production policy receipt was accepted")
	}
}

func TestProductionPolicyRecoveryTransactionBindsOwnerTargetValueAndCalldata(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	chainID := big.NewInt(945)
	owner := crypto.PubkeyToAddress(key.PublicKey)
	coordinator := common.HexToAddress("0x1234")
	data := []byte{1, 2, 3, 4}
	sign := func(target common.Address, value *big.Int, input []byte) *ethTypes.Transaction {
		t.Helper()
		transaction := ethTypes.NewTx(&ethTypes.DynamicFeeTx{ChainID: chainID, Nonce: 7, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2), Gas: 100_000, To: &target, Value: value, Data: input})
		signed, signErr := ethTypes.SignTx(transaction, ethTypes.LatestSignerForChainID(chainID), key)
		if signErr != nil {
			t.Fatal(signErr)
		}
		return signed
	}
	canonical := sign(coordinator, big.NewInt(0), data)
	if err := validateProductionPolicyTransaction(canonical, chainID, owner, coordinator, data); err != nil {
		t.Fatal(err)
	}
	for _, transaction := range []*ethTypes.Transaction{
		sign(common.HexToAddress("0x5678"), big.NewInt(0), data),
		sign(coordinator, big.NewInt(1), data),
		sign(coordinator, big.NewInt(0), []byte{1, 2, 3, 5}),
	} {
		if err := validateProductionPolicyTransaction(transaction, chainID, owner, coordinator, data); err == nil {
			t.Fatal("mutated recovery transaction was accepted")
		}
	}
}

func TestBootstrapPolicyMatchBindsEveryAcceleratedFieldAndCap(t *testing.T) {
	cfg := testResolvedConfig(t)
	hash, err := decodeHash(cfg.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	policy := stabi.STCoordinatorPolicySnapshot{
		PolicyHash: hash, EffectiveEpoch: 14, EffectiveBlock: 1_000,
		EpochBlocks:                  cfg.Policy.Settlement.EpochBlocks,
		RootCommitWindowBlocks:       cfg.Policy.Settlement.RootCommitWindowBlocks,
		FinalizeOffsetBlocks:         cfg.Policy.Settlement.FinalizeOffsetBlocks,
		CloseGraceBlocks:             cfg.Policy.Settlement.CloseGraceBlocks,
		ClaimTTLEpochs:               cfg.Policy.Settlement.ClaimTTLEpochs,
		ClaimGraceEpochs:             cfg.Policy.Settlement.ClaimGraceEpochs,
		MaximumBindingValidityEpochs: cfg.Policy.Binding.MaximumValidityEpochs,
		CommitmentMaxAgeBlocks:       cfg.Policy.Settlement.EpochBlocks * 2,
		EpochDepositCapRao:           new(big.Int).SetUint64(cfg.Policy.Deposit.EpochCapRaoPerOperator),
		CampaignDepositCapRao:        new(big.Int).SetUint64(cfg.Policy.Deposit.TotalTestCampaignCapRao),
	}
	if !bootstrapPolicyMatches(cfg, policy) {
		t.Fatal("canonical accelerated policy was rejected")
	}
	policy.EpochDepositCapRao = new(big.Int).Sub(policy.EpochDepositCapRao, big.NewInt(1))
	if bootstrapPolicyMatches(cfg, policy) {
		t.Fatal("accelerated policy with a one-rao cap drift was accepted")
	}
	policy.EpochDepositCapRao = new(big.Int).SetUint64(cfg.Policy.Deposit.EpochCapRaoPerOperator)
	policy.CommitmentMaxAgeBlocks++
	if bootstrapPolicyMatches(cfg, policy) {
		t.Fatal("accelerated policy with a commitment-age drift was accepted")
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
	replacement := &SetupPlan{Deployment: ContractDeployment{RegistrationRoleGeneration: 1}, Actions: []Action{
		{ID: "evm.vault-register-escrow", Spend: Spend{Registrations: 1}},
		{ID: "operator.register.1", Spend: Spend{Registrations: 1}},
		{ID: "topology.launch"},
	}}
	for _, actionID := range []string{"evm.vault-register-escrow", "operator.register.1"} {
		position, positionErr := initialRegistrationPosition(replacement, actionID)
		if positionErr != nil || position.Applicable {
			t.Fatalf("replacement %s position=%+v error=%v, want stronger non-initial precondition", actionID, position, positionErr)
		}
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
	cfg.Hyperparameters.OwnerControlled["immunity_period"] = testnetBootstrapImmunityPeriodBlocks
	cfg.Hyperparameters.ProductionOwnerControlled["immunity_period"] = 2400
	initial, successor, err := lifecycleHyperparameterExpectation(cfg, "immunity_period", false)
	if err != nil || initial != testnetBootstrapImmunityPeriodBlocks || successor != "" {
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
