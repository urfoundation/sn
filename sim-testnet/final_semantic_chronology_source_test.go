package main

// This unit exercises the historical coordinator ABI boundary using the same
// generated contract ABIs and canonical event projections as release capture.

import (
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/stabi"
)

// Holds one registration call and the predecessor deployment facts required
// to exercise the complete proxy-plus-vault receipt graph.
type finalHistoricalRegistrationFixture struct {
	evidence    *FinalSemanticEvidence
	plan        *SetupPlan
	action      Action
	receipt     FinalEVMReceipt
	transaction FinalCollectedEVMTransaction
	logs        []finalCanonicalEVMLog
}

// Builds a byte-accurate predecessor registration rather than a synthetic
// selector-only example, preserving the coupled calldata and event fields.
func newFinalHistoricalRegistrationFixture(t *testing.T) finalHistoricalRegistrationFixture {
	t.Helper()
	payload := newFinalReceiptPayloadFixture(t)
	pool := payload.evidence.Pools[0]
	owner := common.HexToAddress("0x7000000000000000000000000000000000000007")
	plan := &SetupPlan{
		RegistrationBurnLimitRao: 100,
		Roles:                    PublicRoles{Owner: owner.Hex(), OperatorDepositSigners: []string{pool.DepositSigner}},
		Deployment: ContractDeployment{
			CoordinatorProxy:          common.HexToAddress(payload.evidence.Deployment.CoordinatorProxy),
			SettlementVault:           common.HexToAddress(payload.evidence.Deployment.SettlementVault),
			ReserveSink:               common.HexToAddress(payload.evidence.Deployment.ReserveSink),
			CoordinatorImplementation: common.HexToAddress("0x1000000000000000000000000000000000000012"),
		},
	}
	action := Action{
		ID: "operator.register.1", Kind: "evm-transaction", Target: "no:1",
		Parameters: map[string]string{"maximum_burn_rao": "100"}, Spend: Spend{Registrations: 1},
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	coldkey := finalHistoricalSS58Bytes32(t, pool.OperatorColdkey)
	hotkey := finalHistoricalSS58Bytes32(t, pool.Hotkey)
	depositHotkey := finalHistoricalSS58Bytes32(t, pool.DepositHotkey)
	input, err := parsed.Pack("registerOperator", big.NewInt(1), coldkey, hotkey, depositHotkey, common.HexToAddress(pool.DepositSigner), common.HexToAddress(pool.PayoutRootSigner), uint64(pool.EffectiveEpoch), uint64(100))
	if err != nil {
		t.Fatal(err)
	}
	return finalHistoricalRegistrationFixture{
		evidence: payload.evidence, plan: plan, action: action, receipt: payload.registration,
		transaction: FinalCollectedEVMTransaction{
			TransactionHash: payload.registration.TransactionHash, Block: payload.registration.Block,
			From: strings.ToLower(owner.Hex()), To: strings.ToLower(plan.Deployment.CoordinatorProxy.Hex()), Input: hexutil.Encode(input), ValueWei: registrationFundingWei(100).String(),
		},
		logs: append([]finalCanonicalEVMLog(nil), payload.logs[payload.registration.TransactionHash]...),
	}
}

// Converts the tested SS58 identity into exactly the ABI bytes32 form.
func finalHistoricalSS58Bytes32(t *testing.T, value string) [32]byte {
	t.Helper()
	encoded, err := finalSemanticReceiptSS58Hex(value)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := hexutil.Decode(encoded)
	if err != nil || len(raw) != common.HashLength {
		t.Fatalf("SS58 ABI bytes=%x err=%v", raw, err)
	}
	var result [32]byte
	copy(result[:], raw)
	return result
}

// Rejects independently mutated sender, target, value, selector, argument,
// or secondary-emitter fields before a carried registration can be replayed.
func TestFinalSemanticHistoricalRegistrationRejectsTransactionAndEmitterMutations(t *testing.T) {
	fixture := newFinalHistoricalRegistrationFixture(t)
	emitters, err := finalHistoricalCoordinatorEmitterGraph(fixture.logs)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorAction(fixture.evidence, fixture.plan, fixture.action, fixture.receipt, fixture.transaction, fixture.logs, emitters); err != nil {
		t.Fatalf("exact predecessor registration rejected: %v", err)
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	badNoID, err := parsed.Pack("registerOperator", big.NewInt(2), finalHistoricalSS58Bytes32(t, fixture.evidence.Pools[0].OperatorColdkey), finalHistoricalSS58Bytes32(t, fixture.evidence.Pools[0].Hotkey), finalHistoricalSS58Bytes32(t, fixture.evidence.Pools[0].DepositHotkey), common.HexToAddress(fixture.evidence.Pools[0].DepositSigner), common.HexToAddress(fixture.evidence.Pools[0].PayoutRootSigner), uint64(fixture.evidence.Pools[0].EffectiveEpoch), uint64(100))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		label string
		apply func(*finalHistoricalRegistrationFixture)
	}{
		{label: "sender", apply: func(value *finalHistoricalRegistrationFixture) {
			value.transaction.From = "0x8000000000000000000000000000000000000008"
		}},
		{label: "target", apply: func(value *finalHistoricalRegistrationFixture) {
			value.transaction.To = "0x9000000000000000000000000000000000000009"
		}},
		{label: "value", apply: func(value *finalHistoricalRegistrationFixture) { value.transaction.ValueWei = "1" }},
		{label: "selector", apply: func(value *finalHistoricalRegistrationFixture) { value.transaction.Input = "0xdeadbeef" }},
		{label: "argument", apply: func(value *finalHistoricalRegistrationFixture) { value.transaction.Input = hexutil.Encode(badNoID) }},
		{label: "vault emitter", apply: func(value *finalHistoricalRegistrationFixture) {
			value.logs = append([]finalCanonicalEVMLog(nil), value.logs[1:]...)
		}},
	}
	for _, mutation := range mutations {
		candidate := fixture
		candidate.logs = append([]finalCanonicalEVMLog(nil), fixture.logs...)
		mutation.apply(&candidate)
		candidateEmitters, emitterErr := finalHistoricalCoordinatorEmitterGraph(candidate.logs)
		if emitterErr == nil {
			emitterErr = verifyFinalHistoricalCoordinatorAction(candidate.evidence, candidate.plan, candidate.action, candidate.receipt, candidate.transaction, candidate.logs, candidateEmitters)
		}
		if emitterErr == nil {
			t.Errorf("accepted mutated historical registration %s", mutation.label)
		}
	}
	staleReceipt := fixture.receipt
	staleReceipt.LogsHash = finalTestHex(0xfe)
	if err := verifyFinalHistoricalCoordinatorAction(fixture.evidence, fixture.plan, fixture.action, staleReceipt, fixture.transaction, fixture.logs, emitters); err == nil {
		t.Fatal("accepted registration logs under a substituted receipt hash")
	}
}

// Exercises every authoritative registration event field independently. The
// transaction-level test above catches ABI and graph substitutions; this
// table makes a one-field event drift fail even after the receipt has decoded
// successfully against the predecessor deployment ABI.
func TestFinalSemanticHistoricalRegistrationBindsFullEventTuples(t *testing.T) {
	fixture := newFinalHistoricalRegistrationFixture(t)
	events, err := finalHistoricalCoordinatorDecodeLogs(fixture.plan, fixture.logs)
	if err != nil {
		t.Fatal(err)
	}
	pool := fixture.evidence.Pools[0]
	if err := finalHistoricalRegisterEvents(events, &pool, 1, pool.EffectiveEpoch, common.HexToAddress(pool.DepositSigner), common.HexToAddress(pool.PayoutRootSigner)); err != nil {
		t.Fatalf("exact registration events rejected: %v", err)
	}
	for _, mutation := range []struct {
		label string
		apply func([]finalSemanticEvent)
	}{
		{label: "registered hotkey", apply: func(values []finalSemanticEvent) { values[0].Args["hotkey"] = finalPayloadTestBytes32(0x81) }},
		{label: "registered uid", apply: func(values []finalSemanticEvent) { values[0].Args["uid"] = uint16(pool.UID + 1) }},
		{label: "scheduled epoch", apply: func(values []finalSemanticEvent) { values[1].Args["effectiveEpoch"] = pool.EffectiveEpoch + 1 }},
		{label: "scheduled coldkey", apply: func(values []finalSemanticEvent) { values[1].Args["coldkey"] = finalPayloadTestBytes32(0x82) }},
		{label: "scheduled pool hotkey", apply: func(values []finalSemanticEvent) { values[1].Args["poolHotkey"] = finalPayloadTestBytes32(0x83) }},
		{label: "scheduled deposit hotkey", apply: func(values []finalSemanticEvent) { values[1].Args["depositHotkey"] = finalPayloadTestBytes32(0x84) }},
		{label: "scheduled deposit signer", apply: func(values []finalSemanticEvent) {
			values[1].Args["depositSigner"] = common.HexToAddress("0x8000000000000000000000000000000000000008")
		}},
		{label: "scheduled root signer", apply: func(values []finalSemanticEvent) {
			values[1].Args["rootSigner"] = common.HexToAddress("0x9000000000000000000000000000000000000009")
		}},
		{label: "scheduled active", apply: func(values []finalSemanticEvent) { values[1].Args["active"] = false }},
	} {
		candidate := finalHistoricalCloneEvents(events)
		mutation.apply(candidate)
		if err := finalHistoricalRegisterEvents(candidate, &pool, 1, pool.EffectiveEpoch, common.HexToAddress(pool.DepositSigner), common.HexToAddress(pool.PayoutRootSigner)); err == nil {
			t.Errorf("accepted registration event mutation %s", mutation.label)
		}
	}
}

// Duplicates decoded event maps so one deterministic mutation cannot leak
// into another table entry or weaken the exact successful baseline.
func finalHistoricalCloneEvents(source []finalSemanticEvent) []finalSemanticEvent {
	result := make([]finalSemanticEvent, len(source))
	for index := range source {
		result[index] = source[index]
		result[index].Args = make(map[string]any, len(source[index].Args))
		for key, value := range source[index].Args {
			result[index].Args[key] = value
		}
	}
	return result
}

// Requires both proxy and reserve logs and binds every add-conviction tuple
// field to the approved predecessor action before accepting the receipt.
func TestFinalSemanticHistoricalConvictionRejectsReserveAndCalldataMutations(t *testing.T) {
	payload := newFinalReceiptPayloadFixture(t)
	pool := payload.evidence.Pools[0]
	plan := &SetupPlan{
		PolicyHash: payload.evidence.PolicyHash,
		Roles:      PublicRoles{OperatorDepositSigners: []string{pool.DepositSigner}},
		Deployment: ContractDeployment{
			CoordinatorProxy: common.HexToAddress(payload.evidence.Deployment.CoordinatorProxy),
			SettlementVault:  common.HexToAddress(payload.evidence.Deployment.SettlementVault),
			ReserveSink:      common.HexToAddress(payload.evidence.Deployment.ReserveSink),
		},
	}
	block := payload.deposit.Block
	transactionHash := payload.deposit.TransactionHash
	logs := []finalCanonicalEVMLog{
		payload.depositLogs[0],
		finalPayloadTestEvent(t, CoordinatorABI, "ConvictionAdded", payload.evidence.Deployment.CoordinatorProxy, transactionHash, block, 1, map[string]any{
			"noId": big.NewInt(1), "epoch": big.NewInt(20), "funder": common.HexToAddress(pool.DepositSigner),
			"amount": big.NewInt(30), "policyHash": finalPayloadTestBytes32(0x91), "nonce": big.NewInt(5),
		}),
	}
	receipt := finalPayloadTestReceipt(t, logs)
	evidence := finalPayloadTestRebindDepositReceipt(payload.evidence, receipt)
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	input, err := parsed.Pack("addConviction", big.NewInt(1), big.NewInt(30), big.NewInt(5), block.Number+10)
	if err != nil {
		t.Fatal(err)
	}
	action := Action{ID: voluntaryConvictionActionID, Kind: "evm-transaction", Target: "no:1", Parameters: map[string]string{"amount_rao": "30"}}
	transaction := FinalCollectedEVMTransaction{TransactionHash: receipt.TransactionHash, Block: receipt.Block, From: strings.ToLower(pool.DepositSigner), To: strings.ToLower(plan.Deployment.CoordinatorProxy.Hex()), Input: hexutil.Encode(input), ValueWei: "0"}
	emitters, err := finalHistoricalCoordinatorEmitterGraph(logs)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorAction(evidence, plan, action, receipt, transaction, logs, emitters); err != nil {
		t.Fatalf("exact historical conviction rejected: %v", err)
	}
	for _, mutation := range []struct {
		label string
		apply func(*FinalSemanticEvidence)
	}{
		{label: "reserve epoch", apply: func(value *FinalSemanticEvidence) { value.Reserve.PrincipalAdditions[0].Epoch++ }},
		{label: "reserve operator principal", apply: func(value *FinalSemanticEvidence) { value.Reserve.PrincipalAdditions[0].OperatorPrincipalRao = "29" }},
		{label: "reserve total principal", apply: func(value *FinalSemanticEvidence) { value.Reserve.PrincipalAdditions[0].TotalPrincipalRao = "29" }},
		{label: "reserve live stake", apply: func(value *FinalSemanticEvidence) { value.Reserve.PrincipalAdditions[0].LiveStakeRao = "39" }},
	} {
		candidate := finalPayloadTestRebindDepositReceipt(evidence, receipt)
		mutation.apply(candidate)
		if err := verifyFinalHistoricalCoordinatorAction(candidate, plan, action, receipt, transaction, logs, emitters); err == nil {
			t.Errorf("accepted conviction with mutated %s", mutation.label)
		}
	}
	wrongAmount, err := parsed.Pack("addConviction", big.NewInt(1), big.NewInt(31), big.NewInt(5), block.Number+10)
	if err != nil {
		t.Fatal(err)
	}
	transaction.Input = hexutil.Encode(wrongAmount)
	if err := verifyFinalHistoricalCoordinatorAction(evidence, plan, action, receipt, transaction, logs, emitters); err == nil {
		t.Fatal("accepted conviction calldata amount substitution")
	}
	transaction.Input = hexutil.Encode(input)
	staleDeadline, err := parsed.Pack("addConviction", big.NewInt(1), big.NewInt(30), big.NewInt(5), block.Number-1)
	if err != nil {
		t.Fatal(err)
	}
	transaction.Input = hexutil.Encode(staleDeadline)
	if err := verifyFinalHistoricalCoordinatorAction(evidence, plan, action, receipt, transaction, logs, emitters); err == nil {
		t.Fatal("accepted conviction with an expired deadline")
	}
	transaction.Input = hexutil.Encode(input)
	logs = logs[1:]
	emitters, err = finalHistoricalCoordinatorEmitterGraph(logs)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorAction(evidence, plan, action, receipt, transaction, logs, emitters); err == nil {
		t.Fatal("accepted conviction without reserve emitter")
	}
}

// Exercises the deliberate deposit branch separately so the no-2 signer and
// reserve-side graph cannot accidentally inherit add-conviction coverage.
func TestFinalSemanticHistoricalDepositRejectsReserveAndCalldataMutations(t *testing.T) {
	payload := newFinalReceiptPayloadFixture(t)
	amount := new(big.Int).SetUint64(dishonestDepositRao)
	block := ChainHead{Number: 75, Hash: finalPayloadTestHash(0x75).Hex()}
	transactionHash := finalPayloadTestHash(0xd2).Hex()
	funder := "0x4000000000000000000000000000000000000004"
	logs := []finalCanonicalEVMLog{
		finalPayloadTestEvent(t, ReserveSinkABI, "ReservePrincipalAdded", payload.evidence.Deployment.ReserveSink, transactionHash, block, 0, map[string]any{
			"epoch": big.NewInt(21), "noId": big.NewInt(2), "amount": amount, "operatorPrincipal": amount, "totalPrincipal": amount, "liveStake": amount,
		}),
		finalPayloadTestEvent(t, CoordinatorABI, "Deposit", payload.evidence.Deployment.CoordinatorProxy, transactionHash, block, 1, map[string]any{
			"noId": big.NewInt(2), "epoch": big.NewInt(21), "funder": common.HexToAddress(funder), "amount": amount, "policyHash": finalPayloadTestBytes32(0x91), "nonce": big.NewInt(7),
		}),
	}
	plan := &SetupPlan{
		PolicyHash: payload.evidence.PolicyHash,
		Roles:      PublicRoles{OperatorDepositSigners: []string{"0x3000000000000000000000000000000000000003", funder}},
		Deployment: ContractDeployment{
			CoordinatorProxy: common.HexToAddress(payload.evidence.Deployment.CoordinatorProxy), SettlementVault: common.HexToAddress(payload.evidence.Deployment.SettlementVault), ReserveSink: common.HexToAddress(payload.evidence.Deployment.ReserveSink),
		},
	}
	action := Action{ID: dishonestDepositActionID, Kind: "evm-transaction", Target: "no:2", Parameters: map[string]string{
		"no_id": "2", "amount_rao": amount.String(), "target_epoch": "next_fresh_production_epoch",
		"reserve_runtime_share_transitions": strconv.FormatUint(reserveRuntimeShareTransitionCount, 10), "reserve_rounding_allowance_rao": strconv.FormatUint(reserveRoundingAllowancePerCallRao, 10),
	}}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	input, err := parsed.Pack("deposit", big.NewInt(2), amount, big.NewInt(7), block.Number+10)
	if err != nil {
		t.Fatal(err)
	}
	receipt := finalPayloadTestReceipt(t, logs)
	evidence := finalPayloadTestRebindDepositReceipt(payload.evidence, receipt)
	evidence.Reserve.PrincipalAdditions[0] = FinalReservePrincipalAddedEvidence{
		Epoch:                21,
		NoID:                 2,
		AmountRao:            amount.String(),
		OperatorPrincipalRao: amount.String(),
		TotalPrincipalRao:    amount.String(),
		LiveStakeRao:         amount.String(),
		Receipt:              receipt,
	}
	transaction := FinalCollectedEVMTransaction{TransactionHash: receipt.TransactionHash, Block: receipt.Block, From: funder, To: strings.ToLower(plan.Deployment.CoordinatorProxy.Hex()), Input: hexutil.Encode(input), ValueWei: "0"}
	emitters, err := finalHistoricalCoordinatorEmitterGraph(logs)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorAction(evidence, plan, action, receipt, transaction, logs, emitters); err != nil {
		t.Fatalf("exact historical deposit rejected: %v", err)
	}
	for _, mutation := range []struct {
		label string
		apply func(*FinalSemanticEvidence)
	}{
		{label: "reserve epoch", apply: func(value *FinalSemanticEvidence) { value.Reserve.PrincipalAdditions[0].Epoch++ }},
		{label: "reserve amount", apply: func(value *FinalSemanticEvidence) {
			value.Reserve.PrincipalAdditions[0].AmountRao = new(big.Int).Add(amount, big.NewInt(1)).String()
		}},
		{label: "reserve operator principal", apply: func(value *FinalSemanticEvidence) {
			value.Reserve.PrincipalAdditions[0].OperatorPrincipalRao = new(big.Int).Add(amount, big.NewInt(1)).String()
		}},
		{label: "reserve total principal", apply: func(value *FinalSemanticEvidence) {
			value.Reserve.PrincipalAdditions[0].TotalPrincipalRao = new(big.Int).Add(amount, big.NewInt(1)).String()
		}},
		{label: "reserve live stake", apply: func(value *FinalSemanticEvidence) {
			value.Reserve.PrincipalAdditions[0].LiveStakeRao = new(big.Int).Sub(amount, big.NewInt(1)).String()
		}},
	} {
		candidate := finalPayloadTestRebindDepositReceipt(evidence, receipt)
		mutation.apply(candidate)
		if err := verifyFinalHistoricalCoordinatorAction(candidate, plan, action, receipt, transaction, logs, emitters); err == nil {
			t.Errorf("accepted deposit with mutated %s", mutation.label)
		}
	}
	wrongNoID, err := parsed.Pack("deposit", big.NewInt(1), amount, big.NewInt(7), block.Number+10)
	if err != nil {
		t.Fatal(err)
	}
	transaction.Input = hexutil.Encode(wrongNoID)
	if err := verifyFinalHistoricalCoordinatorAction(evidence, plan, action, receipt, transaction, logs, emitters); err == nil {
		t.Fatal("accepted deposit calldata operator substitution")
	}
	transaction.Input = hexutil.Encode(input)
	staleDeadline, err := parsed.Pack("deposit", big.NewInt(2), amount, big.NewInt(7), block.Number-1)
	if err != nil {
		t.Fatal(err)
	}
	transaction.Input = hexutil.Encode(staleDeadline)
	if err := verifyFinalHistoricalCoordinatorAction(evidence, plan, action, receipt, transaction, logs, emitters); err == nil {
		t.Fatal("accepted deposit with an expired deadline")
	}
	transaction.Input = hexutil.Encode(input)
	wrongPolicyLogs := append([]finalCanonicalEVMLog(nil), logs...)
	wrongPolicyLogs[1] = finalPayloadTestEvent(t, CoordinatorABI, "Deposit", payload.evidence.Deployment.CoordinatorProxy, transactionHash, block, 1, map[string]any{
		"noId": big.NewInt(2), "epoch": big.NewInt(21), "funder": common.HexToAddress(funder), "amount": amount, "policyHash": finalPayloadTestBytes32(0x92), "nonce": big.NewInt(7),
	})
	wrongPolicyReceipt := finalPayloadTestReceipt(t, wrongPolicyLogs)
	wrongPolicyEvidence := finalPayloadTestRebindDepositReceipt(evidence, wrongPolicyReceipt)
	wrongPolicyEvidence.Reserve.PrincipalAdditions[0].NoID = 2
	wrongPolicyEvidence.Reserve.PrincipalAdditions[0].Epoch = 21
	wrongPolicyEvidence.Reserve.PrincipalAdditions[0].AmountRao = amount.String()
	wrongPolicyEvidence.Reserve.PrincipalAdditions[0].OperatorPrincipalRao = amount.String()
	wrongPolicyEvidence.Reserve.PrincipalAdditions[0].TotalPrincipalRao = amount.String()
	wrongPolicyEvidence.Reserve.PrincipalAdditions[0].LiveStakeRao = amount.String()
	wrongEmitters, err := finalHistoricalCoordinatorEmitterGraph(wrongPolicyLogs)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorAction(wrongPolicyEvidence, plan, action, wrongPolicyReceipt, transaction, wrongPolicyLogs, wrongEmitters); err == nil {
		t.Fatal("accepted deposit with a mismatched policy hash")
	}
	for _, accounting := range []struct {
		label, operator, total, live string
	}{
		{label: "operator principal exceeds total", operator: new(big.Int).Add(amount, big.NewInt(1)).String(), total: amount.String(), live: amount.String()},
		{label: "live stake trails total", operator: amount.String(), total: amount.String(), live: new(big.Int).Sub(amount, big.NewInt(1)).String()},
	} {
		incoherentLogs := append([]finalCanonicalEVMLog(nil), logs...)
		incoherentLogs[0] = finalPayloadTestEvent(t, ReserveSinkABI, "ReservePrincipalAdded", payload.evidence.Deployment.ReserveSink, transactionHash, block, 0, map[string]any{
			"epoch": big.NewInt(21), "noId": big.NewInt(2), "amount": amount,
			"operatorPrincipal": finalHistoricalDecimal(t, accounting.operator), "totalPrincipal": finalHistoricalDecimal(t, accounting.total), "liveStake": finalHistoricalDecimal(t, accounting.live),
		})
		incoherentReceipt := finalPayloadTestReceipt(t, incoherentLogs)
		incoherentEvidence := finalPayloadTestRebindDepositReceipt(evidence, incoherentReceipt)
		incoherentEvidence.Reserve.PrincipalAdditions[0] = FinalReservePrincipalAddedEvidence{
			Epoch: 21, NoID: 2, AmountRao: amount.String(), OperatorPrincipalRao: accounting.operator,
			TotalPrincipalRao: accounting.total, LiveStakeRao: accounting.live, Receipt: incoherentReceipt,
		}
		incoherentEmitters, emitterErr := finalHistoricalCoordinatorEmitterGraph(incoherentLogs)
		if emitterErr != nil {
			t.Fatal(emitterErr)
		}
		if err := verifyFinalHistoricalCoordinatorAction(incoherentEvidence, plan, action, incoherentReceipt, transaction, incoherentLogs, incoherentEmitters); err == nil {
			t.Errorf("accepted incoherent reserve accounting: %s", accounting.label)
		}
	}
	logs = logs[1:]
	emitters, err = finalHistoricalCoordinatorEmitterGraph(logs)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorAction(evidence, plan, action, receipt, transaction, logs, emitters); err == nil {
		t.Fatal("accepted deposit without reserve emitter")
	}
}

// Replays the exact schedulePolicy tuple while treating its effective block
// as contract-derived. The sealed postcondition therefore binds the emitted
// index, hash, epoch, and dynamic effective block instead of conflating the
// zero calldata sentinel with the stored policy snapshot.
func TestFinalSemanticHistoricalPolicyScheduleBindsPostStateAndCalldata(t *testing.T) {
	cfg := testResolvedConfig(t)
	policy, err := finalSemanticFixtureReleasePolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if policy.EffectiveEpoch == 0 {
		t.Fatal("fixture policy has no effective epoch")
	}
	policyHash, err := policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := decodeHex32("test policy hash", policyHash)
	if err != nil {
		t.Fatal(err)
	}
	proxy := common.HexToAddress("0x1000000000000000000000000000000000000001")
	owner := common.HexToAddress("0x7000000000000000000000000000000000000007")
	plan := &SetupPlan{PolicyHash: policyHash, Roles: PublicRoles{Owner: owner.Hex()}, Deployment: ContractDeployment{CoordinatorProxy: proxy}}
	action := Action{ID: "policy.schedule-bootstrap", Kind: "evm-transaction", Target: proxy.Hex(), Parameters: map[string]string{"policy_hash": policyHash}}
	block := ChainHead{Number: 100, Hash: finalPayloadTestHash(0x64).Hex()}
	effectiveBlock := uint64(120)
	snapshot := stabi.STCoordinatorPolicySnapshot{
		PolicyHash: hash, EffectiveEpoch: policy.EffectiveEpoch, EffectiveBlock: 0,
		EpochBlocks: policy.Settlement.EpochBlocks, RootCommitWindowBlocks: policy.Settlement.RootCommitWindowBlocks,
		FinalizeOffsetBlocks: policy.Settlement.FinalizeOffsetBlocks, CloseGraceBlocks: policy.Settlement.CloseGraceBlocks,
		ClaimTTLEpochs: policy.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: policy.Settlement.ClaimGraceEpochs,
		MaximumBindingValidityEpochs: policy.Binding.MaximumValidityEpochs, CommitmentMaxAgeBlocks: policy.Settlement.EpochBlocks * 2,
		EpochDepositCapRao: new(big.Int).SetUint64(policy.Deposit.EpochCapRaoPerOperator), CampaignDepositCapRao: new(big.Int).SetUint64(policy.Deposit.TotalTestCampaignCapRao),
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	input, err := parsed.Pack("schedulePolicy", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	transactionHash := finalPayloadTestHash(0xf1).Hex()
	logs := []finalCanonicalEVMLog{finalPayloadTestEvent(t, CoordinatorABI, "PolicyScheduled", proxy.Hex(), transactionHash, block, 0, map[string]any{
		"index": big.NewInt(1), "policyHash": hash, "effectiveEpoch": policy.EffectiveEpoch, "effectiveBlock": effectiveBlock,
	})}
	receipt := finalPayloadTestReceipt(t, logs)
	transaction := FinalCollectedEVMTransaction{TransactionHash: receipt.TransactionHash, Block: receipt.Block, From: strings.ToLower(owner.Hex()), To: strings.ToLower(proxy.Hex()), Input: hexutil.Encode(input), ValueWei: "0"}
	postcondition := &ActionPostcondition{
		ActionID: action.ID, EVMFinalized: ChainHead{Number: block.Number + 1, Hash: finalPayloadTestHash(0x65).Hex()},
		Observed: map[string]any{
			"policy_count":                     uint64(2),
			"current_epoch":                    policy.EffectiveEpoch - 1,
			"policy_hash":                      policyHash,
			"scheduled_policy_index":           uint64(1),
			"scheduled_policy_hash":            policyHash,
			"scheduled_policy_effective_epoch": policy.EffectiveEpoch,
			"scheduled_policy_effective_block": effectiveBlock,
		},
	}
	emitters, err := finalHistoricalCoordinatorEmitterGraph(logs)
	if err != nil {
		t.Fatal(err)
	}
	evidence := &FinalSemanticEvidence{PolicyHash: policyHash}
	if err := verifyFinalHistoricalCoordinatorActionWithPostcondition(evidence, &policy, postcondition, plan, action, receipt, transaction, logs, emitters); err != nil {
		t.Fatalf("exact historical policy schedule rejected: %v", err)
	}
	for _, mutation := range []struct {
		label string
		apply func(*ActionPostcondition)
	}{
		{label: "policy count", apply: func(value *ActionPostcondition) { value.Observed["policy_count"] = uint64(3) }},
		{label: "scheduled index", apply: func(value *ActionPostcondition) { value.Observed["scheduled_policy_index"] = uint64(2) }},
		{label: "scheduled hash", apply: func(value *ActionPostcondition) { value.Observed["scheduled_policy_hash"] = finalTestHex(0x92) }},
		{label: "scheduled epoch", apply: func(value *ActionPostcondition) {
			value.Observed["scheduled_policy_effective_epoch"] = policy.EffectiveEpoch + 1
		}},
		{label: "scheduled block", apply: func(value *ActionPostcondition) {
			value.Observed["scheduled_policy_effective_block"] = effectiveBlock + 1
		}},
	} {
		candidate := *postcondition
		candidate.Observed = make(map[string]any, len(postcondition.Observed))
		for key, value := range postcondition.Observed {
			candidate.Observed[key] = value
		}
		mutation.apply(&candidate)
		if err := verifyFinalHistoricalCoordinatorActionWithPostcondition(evidence, &policy, &candidate, plan, action, receipt, transaction, logs, emitters); err == nil {
			t.Errorf("accepted historical policy schedule with mutated %s", mutation.label)
		}
	}
	badInput := append([]byte(nil), input...)
	badInput[len(badInput)-1] ^= 0x01
	transaction.Input = hexutil.Encode(badInput)
	if err := verifyFinalHistoricalCoordinatorActionWithPostcondition(evidence, &policy, postcondition, plan, action, receipt, transaction, logs, emitters); err == nil {
		t.Fatal("accepted policy schedule calldata mutation")
	}
}

// Rejects any change to the owner-authorized UUPS activation envelope or its
// sole emitted implementation identity. A predecessor proxy upgrade cannot
// be justified by a later runtime census or by a selector-only match.
func TestFinalSemanticHistoricalUpgradeBindsTransactionAndEvent(t *testing.T) {
	payload := newFinalReceiptPayloadFixture(t)
	proxy := common.HexToAddress(payload.evidence.Deployment.CoordinatorProxy)
	owner := common.HexToAddress("0x7000000000000000000000000000000000000007")
	implementation := common.HexToAddress("0x1000000000000000000000000000000000000012")
	plan := &SetupPlan{
		Roles: PublicRoles{Owner: owner.Hex()},
		Deployment: ContractDeployment{
			CoordinatorProxy: proxy, SettlementVault: common.HexToAddress(payload.evidence.Deployment.SettlementVault), ReserveSink: common.HexToAddress(payload.evidence.Deployment.ReserveSink),
		},
		CoordinatorUpgrade: CoordinatorUpgrade{Implementation: implementation, RuntimeCodeHash: finalTestHex(0xa1)},
	}
	action := Action{ID: "evm.coordinator-upgrade-activate", Kind: "evm-transaction", Target: proxy.Hex(), Parameters: map[string]string{
		"implementation": implementation.Hex(), "runtime_code_hash": plan.CoordinatorUpgrade.RuntimeCodeHash,
	}}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	input, err := parsed.Pack("upgradeToAndCall", implementation, []byte{})
	if err != nil {
		t.Fatal(err)
	}
	block := ChainHead{Number: 100, Hash: finalPayloadTestHash(0x64).Hex()}
	transactionHash := finalPayloadTestHash(0xf2).Hex()
	logs := []finalCanonicalEVMLog{finalPayloadTestEvent(t, CoordinatorABI, "Upgraded", proxy.Hex(), transactionHash, block, 0, map[string]any{"implementation": implementation})}
	receipt := finalPayloadTestReceipt(t, logs)
	transaction := FinalCollectedEVMTransaction{TransactionHash: receipt.TransactionHash, Block: receipt.Block, From: strings.ToLower(owner.Hex()), To: strings.ToLower(proxy.Hex()), Input: hexutil.Encode(input), ValueWei: "0"}
	emitters, err := finalHistoricalCoordinatorEmitterGraph(logs)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorAction(payload.evidence, plan, action, receipt, transaction, logs, emitters); err != nil {
		t.Fatalf("exact historical upgrade rejected: %v", err)
	}
	for _, mutation := range []struct {
		label string
		apply func(*FinalCollectedEVMTransaction)
	}{
		{label: "sender", apply: func(value *FinalCollectedEVMTransaction) { value.From = "0x8000000000000000000000000000000000000008" }},
		{label: "target", apply: func(value *FinalCollectedEVMTransaction) { value.To = "0x9000000000000000000000000000000000000009" }},
		{label: "value", apply: func(value *FinalCollectedEVMTransaction) { value.ValueWei = "1" }},
		{label: "calldata", apply: func(value *FinalCollectedEVMTransaction) { value.Input = hexutil.Encode(append(input, 1)) }},
	} {
		candidate := transaction
		mutation.apply(&candidate)
		if err := verifyFinalHistoricalCoordinatorAction(payload.evidence, plan, action, receipt, candidate, logs, emitters); err == nil {
			t.Errorf("accepted upgrade transaction mutation %s", mutation.label)
		}
	}
	wrongLogs := []finalCanonicalEVMLog{finalPayloadTestEvent(t, CoordinatorABI, "Upgraded", proxy.Hex(), transactionHash, block, 0, map[string]any{"implementation": common.HexToAddress("0x9000000000000000000000000000000000000009")})}
	wrongReceipt := finalPayloadTestReceipt(t, wrongLogs)
	wrongEmitters, err := finalHistoricalCoordinatorEmitterGraph(wrongLogs)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorAction(payload.evidence, plan, action, wrongReceipt, transaction, wrongLogs, wrongEmitters); err == nil {
		t.Fatal("accepted upgrade with an emitted implementation substitution")
	}
}

// Compares both independently sealed receipt representations event by event;
// a matching aggregate hash alone is not enough to authenticate a multi-log
// registration or reserve transaction.
func TestFinalSemanticHistoricalReceiptProofBindsRawLogs(t *testing.T) {
	fixture := newFinalHistoricalRegistrationFixture(t)
	row := &FinalHistoricalCoordinatorReceiptEvidence{Receipt: fixture.receipt}
	proof := finalHistoricalCoordinatorReceiptProof{
		Status: fixture.receipt.Status, TransactionHash: fixture.receipt.TransactionHash, Block: fixture.receipt.Block,
		Logs: append([]finalCanonicalEVMLog(nil), fixture.logs...),
	}
	data, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorReceiptProof(row, data, fixture.logs); err != nil {
		t.Fatalf("exact historical receipt proof rejected: %v", err)
	}
	proof.Logs = append([]finalCanonicalEVMLog(nil), proof.Logs...)
	proof.Logs[1].Data = "0x"
	mutated, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorReceiptProof(row, mutated, fixture.logs); err == nil {
		t.Fatal("accepted raw receipt-log substitution")
	}
}

// Converts a canonical nonnegative decimal fixture into the ABI integer used
// by one event without accepting malformed values in the accounting table.
func finalHistoricalDecimal(t *testing.T, value string) *big.Int {
	t.Helper()
	parsed, ok := new(big.Int).SetString(value, 10)
	if !ok || parsed.Sign() < 0 || parsed.String() != value {
		t.Fatalf("invalid historical decimal %q", value)
	}
	return parsed
}

// Keeps failed pre-window probes out of the mutation-replay domain. They have
// their own typed exit-criterion verification but cannot carry an executable
// state transition, plan action, or predecessor runtime obligation.
func TestFinalSemanticHistoricalReceiptCensusSkipsFailedNonMutations(t *testing.T) {
	evidence := &FinalSemanticEvidence{
		EVMCampaignStartHead: ChainHead{Number: 10, Hash: finalTestHex(0x10)},
		ExitCriteria: []FinalExitCriterionEvidence{{EVMReceipts: []FinalEVMReceipt{{
			TransactionHash: finalTestHex(0x11), Block: ChainHead{Number: 9, Hash: finalTestHex(0x09)}, Status: "failed", LogsHash: finalTestHex(0x12),
		}}}},
	}
	carried, err := finalSemanticUniqueCarriedEVMReceipts(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(carried) != 0 {
		t.Fatalf("failed non-mutation entered historical receipt replay: %+v", carried)
	}
}

// Rejects omissions or substitutions in the content-addressed full receipt
// before action decoding can use a partial multi-emitter event projection.
func TestFinalSemanticHistoricalReceiptArtifactRejectsEnvelopeAndLogMutations(t *testing.T) {
	fixture := newFinalHistoricalRegistrationFixture(t)
	row := &FinalHistoricalCoordinatorReceiptEvidence{
		Receipt: fixture.receipt, TransactionFrom: fixture.transaction.From, TransactionTo: fixture.transaction.To,
		TransactionInput: fixture.transaction.Input, TransactionValueWei: fixture.transaction.ValueWei,
	}
	artifact := finalHistoricalCoordinatorReceiptArtifact{
		Status: fixture.receipt.Status, TransactionHash: fixture.transaction.TransactionHash, Block: fixture.transaction.Block,
		From: fixture.transaction.From, To: fixture.transaction.To, Input: fixture.transaction.Input, ValueWei: fixture.transaction.ValueWei,
		Logs: append([]finalCanonicalEVMLog(nil), fixture.logs...),
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := finalHistoricalCoordinatorReceiptArtifactTransaction(row, data); err != nil {
		t.Fatalf("exact historical receipt artifact rejected: %v", err)
	}
	artifact.Input = "0xdeadbeef"
	badInput, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := finalHistoricalCoordinatorReceiptArtifactTransaction(row, badInput); err == nil {
		t.Fatal("accepted historical receipt input substitution")
	}
	artifact.Input = fixture.transaction.Input
	artifact.Logs = artifact.Logs[1:]
	missingEmitter, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := finalHistoricalCoordinatorReceiptArtifactTransaction(row, missingEmitter); err == nil {
		t.Fatal("accepted historical receipt without its vault log")
	}
}

// Exercises target-derived enumeration over multiple approved revisions. The
// production attempt contains repeated coordinator upgrades, so a census
// keyed only by action ID would incorrectly retain one arbitrary occurrence.
func TestFinalSemanticHistoricalCoordinatorTargetCensusIncludesAllFinalizedActions(t *testing.T) {
	proxy := common.HexToAddress("0xa100000000000000000000000000000000000001")
	currentHash := finalTestHex(0x01)
	evidence := &FinalSemanticEvidence{
		DeploymentID:         "deployment",
		EVMCampaignStartHead: ChainHead{Number: 100, Hash: finalTestHex(0x64)},
	}
	current := &SetupPlan{PlanHash: currentHash, DeploymentID: evidence.DeploymentID, ChainID: testnetChainID, Netuid: 521}
	plans := map[string]*SetupPlan{currentHash: current}
	entries := make([]JournalEntry, 0, 12)
	for index := 0; index < 11; index++ {
		planHash := finalTestHex(byte(0x10 + index))
		intent := finalTestHex(byte(0x40 + index))
		actionID := "evm.coordinator-upgrade-activate"
		parameters := map[string]string{"implementation": common.HexToAddress("0xb100000000000000000000000000000000000001").Hex(), "runtime_code_hash": finalTestHex(0x80)}
		switch index {
		case 7, 8:
			actionID = "policy.schedule-bootstrap"
			parameters = map[string]string{"policy_hash": finalTestHex(0x81)}
		case 9:
			actionID = "fleet.refresh.oracle-activate"
			parameters = map[string]string{"oracle": common.HexToAddress("0xc100000000000000000000000000000000000001").Hex()}
		case 10:
			actionID = "fleet.refresh.oracle-restore"
			parameters = map[string]string{"oracle": common.HexToAddress("0xd100000000000000000000000000000000000001").Hex()}
		}
		plan := &SetupPlan{
			PlanHash: planHash, DeploymentID: evidence.DeploymentID, ChainID: testnetChainID, Netuid: 521,
			Deployment: ContractDeployment{CoordinatorProxy: proxy},
			Actions:    []Action{{ID: actionID, Kind: "evm-transaction", Target: proxy.Hex(), IntentHash: intent, Parameters: parameters}},
		}
		current.PriorPlanHashes = append(current.PriorPlanHashes, planHash)
		plans[planHash] = plan
		entries = append(entries, JournalEntry{
			DeploymentID: evidence.DeploymentID, PlanHash: planHash, ActionID: actionID, IntentHash: intent,
			Stage: StageFinalized, TransactionHash: finalTestHex(byte(0x90 + index)), BlockNumber: uint64(20 + index), BlockHash: finalTestHex(byte(0xa0 + index)),
		})
	}
	ignoredHash := finalTestHex(0xf1)
	ignoredIntent := finalTestHex(0xf2)
	ignored := &SetupPlan{
		PlanHash: ignoredHash, DeploymentID: evidence.DeploymentID, ChainID: testnetChainID, Netuid: 521,
		Deployment: ContractDeployment{CoordinatorProxy: proxy},
		Actions:    []Action{{ID: "operator.register.1", Kind: "evm-transaction", Target: "no:1", IntentHash: ignoredIntent}},
	}
	current.PriorPlanHashes = append(current.PriorPlanHashes, ignoredHash)
	plans[ignoredHash] = ignored
	entries = append(entries, JournalEntry{
		DeploymentID: evidence.DeploymentID, PlanHash: ignoredHash, ActionID: "operator.register.1", IntentHash: ignoredIntent,
		Stage: StageFinalized, TransactionHash: finalTestHex(0xf3), BlockNumber: 42, BlockHash: finalTestHex(0xf4),
	})
	targets, err := finalHistoricalCoordinatorJournalActions(evidence, current, plans, entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 11 {
		t.Fatalf("coordinator target census=%d, want all 11 finalized actions", len(targets))
	}
	upgrades, policies, activates, restores := 0, 0, 0, 0
	for _, target := range targets {
		switch target.action.ID {
		case "evm.coordinator-upgrade-activate":
			upgrades++
		case "policy.schedule-bootstrap":
			policies++
		case "fleet.refresh.oracle-activate":
			activates++
		case "fleet.refresh.oracle-restore":
			restores++
		default:
			t.Fatalf("unexpected target-derived action %s", target.action.ID)
		}
	}
	if upgrades != 7 || policies != 2 || activates != 1 || restores != 1 {
		t.Fatalf("target-derived counts upgrade=%d policy=%d activate=%d restore=%d", upgrades, policies, activates, restores)
	}
}

// Binds the temporary fleet-oracle schedule to canonical calldata, its only
// event, and the dual observer checkpoint. Each one-field mutation proves a
// successful receipt cannot substitute for the required active-window proof.
func TestFinalSemanticHistoricalCommitmentOracleScheduleBindsCalldataEventAndPostcondition(t *testing.T) {
	proxy := common.HexToAddress("0xa200000000000000000000000000000000000002")
	owner := common.HexToAddress("0xb200000000000000000000000000000000000002")
	immutable := common.HexToAddress("0xc200000000000000000000000000000000000002")
	batcher := common.HexToAddress("0xd200000000000000000000000000000000000002")
	plan := &SetupPlan{
		Roles: PublicRoles{Owner: owner.Hex(), CommitmentOracle: immutable.Hex()},
		Deployment: ContractDeployment{
			CoordinatorProxy: proxy,
			SettlementVault:  common.HexToAddress("0xe200000000000000000000000000000000000002"),
			ReserveSink:      common.HexToAddress("0xf200000000000000000000000000000000000002"),
		},
	}
	action := Action{
		ID: "fleet.refresh.oracle-activate", Kind: "evm-transaction", Target: proxy.Hex(),
		Parameters: map[string]string{"oracle": batcher.Hex()},
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	input, err := parsed.Pack("scheduleCommitmentOracle", batcher, uint64(10))
	if err != nil {
		t.Fatal(err)
	}
	block := ChainHead{Number: 50, Hash: finalTestHex(0x50)}
	transactionHash := finalTestHex(0xa2)
	logs := []finalCanonicalEVMLog{finalPayloadTestEvent(t, CoordinatorABI, "CommitmentOracleScheduled", proxy.Hex(), transactionHash, block, 0, map[string]any{
		"oracle": batcher, "effectiveEpoch": uint64(10),
	})}
	receipt := finalPayloadTestReceipt(t, logs)
	transaction := FinalCollectedEVMTransaction{
		TransactionHash: receipt.TransactionHash, Block: receipt.Block, From: strings.ToLower(owner.Hex()), To: strings.ToLower(proxy.Hex()), Input: hexutil.Encode(input), ValueWei: "0",
	}
	observed := map[string]any{
		"current_epoch": uint64(9), "immutable_oracle": immutable.Hex(), "active_oracle": immutable.Hex(),
		"pending_oracle": batcher.Hex(), "pending_epoch": uint64(10), "target_oracle": batcher.Hex(),
	}
	postcondition := &ActionPostcondition{
		ActionID: action.ID, EVMFinalized: block, IndependentEVMFinalized: block,
		Observed: observed, IndependentObserved: map[string]any{
			"current_epoch": uint64(9), "immutable_oracle": immutable.Hex(), "active_oracle": immutable.Hex(),
			"pending_oracle": batcher.Hex(), "pending_epoch": uint64(10), "target_oracle": batcher.Hex(),
		},
	}
	emitters, err := finalHistoricalCoordinatorEmitterGraph(logs)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorActionWithPostcondition(&FinalSemanticEvidence{}, nil, postcondition, plan, action, receipt, transaction, logs, emitters); err != nil {
		t.Fatalf("exact commitment oracle schedule rejected: %v", err)
	}
	wrongOracle, err := parsed.Pack("scheduleCommitmentOracle", immutable, uint64(10))
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		label string
		apply func(*FinalCollectedEVMTransaction, *ActionPostcondition, *[]finalCanonicalEVMLog)
	}{
		{label: "calldata target", apply: func(value *FinalCollectedEVMTransaction, _ *ActionPostcondition, _ *[]finalCanonicalEVMLog) {
			value.Input = hexutil.Encode(wrongOracle)
		}},
		{label: "value", apply: func(value *FinalCollectedEVMTransaction, _ *ActionPostcondition, _ *[]finalCanonicalEVMLog) {
			value.ValueWei = "1"
		}},
		{label: "event epoch", apply: func(_ *FinalCollectedEVMTransaction, _ *ActionPostcondition, logs *[]finalCanonicalEVMLog) {
			(*logs)[0] = finalPayloadTestEvent(t, CoordinatorABI, "CommitmentOracleScheduled", proxy.Hex(), transactionHash, block, 0, map[string]any{"oracle": batcher, "effectiveEpoch": uint64(11)})
		}},
		{label: "pending epoch", apply: func(_ *FinalCollectedEVMTransaction, post *ActionPostcondition, _ *[]finalCanonicalEVMLog) {
			post.Observed["pending_epoch"] = uint64(11)
		}},
		{label: "missing postcondition", apply: func(_ *FinalCollectedEVMTransaction, post *ActionPostcondition, _ *[]finalCanonicalEVMLog) {
			post.Observed = nil
		}},
	} {
		candidateTransaction := transaction
		candidatePostcondition := *postcondition
		candidatePostcondition.Observed = map[string]any{}
		for key, value := range postcondition.Observed {
			candidatePostcondition.Observed[key] = value
		}
		candidatePostcondition.IndependentObserved = map[string]any{}
		for key, value := range postcondition.IndependentObserved {
			candidatePostcondition.IndependentObserved[key] = value
		}
		candidateLogs := append([]finalCanonicalEVMLog(nil), logs...)
		mutation.apply(&candidateTransaction, &candidatePostcondition, &candidateLogs)
		candidateReceipt := finalPayloadTestReceipt(t, candidateLogs)
		candidateEmitters, emitterErr := finalHistoricalCoordinatorEmitterGraph(candidateLogs)
		if emitterErr == nil {
			emitterErr = verifyFinalHistoricalCoordinatorActionWithPostcondition(&FinalSemanticEvidence{}, nil, &candidatePostcondition, plan, action, candidateReceipt, candidateTransaction, candidateLogs, candidateEmitters)
		}
		if emitterErr == nil {
			t.Errorf("accepted commitment oracle schedule mutation %s", mutation.label)
		}
	}
}

// Rejects an impossible shared EVM transaction coordinate instead of using a
// hash comparison to invent order between an oracle transition and a batch.
func TestFinalSemanticHistoricalCoordinatorPositionRejectsCoordinateCollision(t *testing.T) {
	left := finalHistoricalCoordinatorTransactionPosition{
		block:            ChainHead{Number: 55, Hash: finalTestHex(0x55)},
		transactionIndex: 2,
		transactionHash:  finalTestHex(0x56),
	}
	right := left
	right.transactionHash = finalTestHex(0x57)
	if _, err := finalHistoricalCoordinatorPositionBefore(left, right); err == nil {
		t.Fatal("accepted two different transactions at one canonical EVM coordinate")
	}
}
