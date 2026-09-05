// These deterministic checks isolate historical EVM accounting, payout state,
// and runtime identity from the larger release fixture.
package main

import (
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Drops transcript exchanges when a unit test is concerned only with state.
func finalSemanticDiscardExchanges(string, ChainHead, []FinalRPCExchange) error {
	return nil
}

// Builds a deliberately small state-only fixture so these regressions avoid a
// minute-scale 1,000-miner setup without losing their invariant coverage.
func finalSemanticEVMAuditFixture() FinalSemanticEvidence {
	baseline := ChainHead{Number: 9, Hash: finalTestHex(0x09)}
	snapshot := ChainHead{Number: 12, Hash: finalTestHex(0x0c)}
	finalize := ChainHead{Number: 15, Hash: finalTestHex(0x0f)}
	terminal := ChainHead{Number: 20, Hash: finalTestHex(0x14)}
	payee := finalTestHex(0xa1)
	cycle := FinalCRv4Cycle{
		SettlementEpoch: 0, EVMSnapshot: snapshot,
		Pools: []FinalPoolWeightEvidence{{NoID: 1, ConvictionBeforeRao: "7", ObservedDepositRao: "11"}},
	}
	return FinalSemanticEvidence{
		Window:               ScenarioAcceptanceWindow{BaselineHead: baseline, TerminalBlock: terminal.Number},
		EVMCampaignStartHead: ChainHead{Number: 4, Hash: finalTestHex(0x04)},
		EVMTerminalHead:      terminal,
		Deployment: FinalContractDeploymentEvidence{
			CoordinatorProxy: finalTestHex(0x31), CoordinatorImplementation: finalTestHex(0x32),
			ObservedImplementationSlot: finalTestHex(0x33), CoordinatorProxyCodeHash: finalTestHex(0x34), ImplementationCodeHash: finalTestHex(0x35),
		},
		Pools:      []FinalPoolUIDEvidence{{NoID: 1, FinalCarryRao: "0"}},
		Validators: []FinalValidatorIdentityEvidence{{Cycles: []FinalCRv4Cycle{cycle}}},
		Epochs: []FinalEpochOperatorEvidence{{
			Epoch: 0, NoID: 1, CarryInRao: "0", CarryOutRao: "0", Finalize: FinalEVMReceipt{Block: finalize},
			Claims: []FinalClaimEvidence{{LeafIndex: 0, Payee: payee, ShareBPS: 10_000, ClaimedRao: "100", PaidRao: "100"}},
		}},
		ClaimPayments: []FinalClaimPaymentEvidence{{Coldkey: payee, AmountRao: "100"}},
	}
}

// Builds a minimal canonical payment ledger with one authenticated receipt.
func finalSemanticClaimPaymentLedgerFixture() FinalSemanticEvidence {
	baseline := ChainHead{Number: 9, Hash: finalTestHex(0x09)}
	receipt := FinalEVMReceipt{
		TransactionHash: finalTestHex(0x41), Block: ChainHead{Number: 10, Hash: finalTestHex(0x0a)}, Status: "success", LogsHash: finalTestHex(0x42),
		Proof: FinalArtifactLocator{Kind: "evm-receipt", URI: "claim-paid.json", ContentHash: "sha256:" + strings.Repeat("a", 64), SizeBytes: 1},
	}
	return FinalSemanticEvidence{
		Window:               ScenarioAcceptanceWindow{BaselineHead: baseline, TerminalBlock: 20},
		SettlementAccounting: FinalSettlementVaultAccounting{ClaimPaidEventRao: "100", TotalPaidDeltaRao: "100"},
		ClaimPayments:        []FinalClaimPaymentEvidence{{Coldkey: finalTestHex(0xa1), AmountRao: "100", Receipt: receipt}},
	}
}

// Builds a captured withdrawal-only payment event and its closed archive view.
func finalSemanticClaimPaidSourceFixture(t *testing.T, address string) (*finalSemanticArchive, FinalSemanticEvidence, *finalSemanticEventIndex) {
	t.Helper()
	block := ChainHead{Number: 55, Hash: finalPayloadTestHash(0x55).Hex()}
	coldkey := finalPayloadTestBytes32(0xa2)
	log := finalPayloadTestEvent(t, SettlementVaultABI, "ClaimPaid", address, finalPayloadTestHash(0xa5).Hex(), block, 0, map[string]any{
		"coldkey": coldkey, "amount": big.NewInt(25), "relayer": common.HexToAddress("0x6000000000000000000000000000000000000006"),
	})
	event := finalSemanticEvent{Name: "ClaimPaid", Log: log, Args: map[string]any{"coldkey": coldkey, "amount": big.NewInt(25)}}
	return &finalSemanticArchive{runRoot: t.TempDir()}, FinalSemanticEvidence{
		Window:          ScenarioAcceptanceWindow{BaselineHead: ChainHead{Number: 50, Hash: finalTestHex(0x50)}},
		EVMTerminalHead: ChainHead{Number: 60, Hash: finalTestHex(0x60)},
		Deployment:      FinalContractDeploymentEvidence{SettlementVault: address},
	}, &finalSemanticEventIndex{byName: map[string][]finalSemanticEvent{"ClaimPaid": {event}}, byTx: map[string][]finalCanonicalEVMLog{log.TransactionHash: {log}}}
}

// Ensures a standalone credit withdrawal is retained as canonical evidence.
func TestFinalSemanticBuildClaimPaymentsCapturesWithdrawalOnlyReceipt(t *testing.T) {
	address := "0x2000000000000000000000000000000000000002"
	archive, source, events := finalSemanticClaimPaidSourceFixture(t, address)
	if err := archive.buildClaimPayments(&source, events); err != nil {
		t.Fatal(err)
	}
	if len(source.ClaimPayments) != 1 {
		t.Fatalf("ClaimPaid records=%d, want 1", len(source.ClaimPayments))
	}
	payment := source.ClaimPayments[0]
	if payment.AmountRao != "25" || payment.Coldkey != finalTestHex(0xa2) || payment.Receipt.TransactionHash != finalPayloadTestHash(0xa5).Hex() || payment.Receipt.Proof.Kind != "evm-receipt" {
		t.Fatalf("withdrawal-only ClaimPaid record=%+v", payment)
	}
}

// Rejects a payment event emitted by an address other than the settlement vault.
func TestFinalSemanticBuildClaimPaymentsRejectsForeignVault(t *testing.T) {
	archive, source, events := finalSemanticClaimPaidSourceFixture(t, "0x2000000000000000000000000000000000000002")
	events.byName["ClaimPaid"][0].Log.Address = "0x3000000000000000000000000000000000000003"
	if err := archive.buildClaimPayments(&source, events); err == nil || !strings.Contains(err.Error(), "settlement vault") {
		t.Fatalf("foreign ClaimPaid vault error=%v", err)
	}
}

// Proves operator-scoped claim keys remain distinct from share-scoped leaves.
func TestFinalSemanticVaultClaimKeySeparatesOperatorAndPayoutLeaf(t *testing.T) {
	coldkey := finalTestHex(0xa1)
	shareFourThousandKey, err := finalSemanticVaultClaimKey(1, coldkey)
	if err != nil {
		t.Fatal(err)
	}
	shareSixThousandKey, err := finalSemanticVaultClaimKey(1, coldkey)
	if err != nil {
		t.Fatal(err)
	}
	if shareFourThousandKey != shareSixThousandKey {
		t.Fatalf("claim key changed with payout share: %s != %s", shareFourThousandKey, shareSixThousandKey)
	}
	secondOperatorKey, err := finalSemanticVaultClaimKey(2, coldkey)
	if err != nil {
		t.Fatal(err)
	}
	if shareFourThousandKey == secondOperatorKey {
		t.Fatalf("claim keys collide across operators: %s", shareFourThousandKey)
	}
	firstLeaf, err := finalSemanticVaultPayoutLeaf(coldkey, 4_000)
	if err != nil {
		t.Fatal(err)
	}
	secondLeaf, err := finalSemanticVaultPayoutLeaf(coldkey, 6_000)
	if err != nil {
		t.Fatal(err)
	}
	if firstLeaf == secondLeaf {
		t.Fatalf("payout leaves collide across shares: %s", firstLeaf)
	}
	if shareFourThousandKey == firstLeaf {
		t.Fatalf("claim key reused the payout leaf: %s", shareFourThousandKey)
	}
}

// Rejects a different claim-once key even when surrounding claim state matches.
func TestFinalSemanticVaultClaimAuditRejectsClaimKeySubstitution(t *testing.T) {
	evidence := finalSemanticEVMAuditFixture()
	reader := &finalTestChainReader{evidence: &evidence, corruptVaultClaimKey: true}
	if err := verifyFinalSemanticVaultClaims(context.Background(), &evidence, reader, finalSemanticDiscardExchanges); err == nil || !strings.Contains(err.Error(), "terminal vault claim state") {
		t.Fatalf("substituted claim key error=%v", err)
	}
}

// Rejects a carry observation substituted from a different operator.
func TestFinalSemanticVaultCarryAuditRejectsOperatorCarrySubstitution(t *testing.T) {
	evidence := finalSemanticEVMAuditFixture()
	reader := &finalTestChainReader{evidence: &evidence, corruptCarry: true}
	if err := verifyFinalSemanticVaultCarries(context.Background(), &evidence, reader, finalSemanticDiscardExchanges); err == nil || !strings.Contains(err.Error(), "vault carry") {
		t.Fatalf("substituted operator carry error=%v", err)
	}
}

// Rejects a share leaf substituted for the represented claim.
func TestFinalSemanticVaultClaimAuditRejectsPayoutLeafSubstitution(t *testing.T) {
	evidence := finalSemanticEVMAuditFixture()
	reader := &finalTestChainReader{evidence: &evidence, corruptVaultPayoutLeaf: true}
	if err := verifyFinalSemanticVaultClaims(context.Background(), &evidence, reader, finalSemanticDiscardExchanges); err == nil || !strings.Contains(err.Error(), "terminal vault claim state") {
		t.Fatalf("substituted payout leaf error=%v", err)
	}
}

// Accepts a payment that settles prior credit and a separate credit withdrawal.
func TestFinalSemanticVaultClaimAuditReconcilesPriorCreditAndWithdrawal(t *testing.T) {
	evidence := finalSemanticEVMAuditFixture()
	coldkey := strings.ToLower(evidence.Epochs[0].Claims[0].Payee)
	paymentIndex := -1
	for index := range evidence.ClaimPayments {
		if strings.EqualFold(evidence.ClaimPayments[index].Coldkey, coldkey) {
			paymentIndex = index
			break
		}
	}
	if paymentIndex < 0 {
		t.Fatal("fixture has no ClaimPaid record for first claim coldkey")
	}
	amount, ok := new(big.Int).SetString(evidence.ClaimPayments[paymentIndex].AmountRao, 10)
	if !ok {
		t.Fatal("fixture ClaimPaid amount is invalid")
	}
	evidence.ClaimPayments[paymentIndex].AmountRao = amount.Add(amount, big.NewInt(50)).String()
	withdrawalColdkey := finalTestHex(0xfa)
	evidence.ClaimPayments = append(evidence.ClaimPayments, FinalClaimPaymentEvidence{Coldkey: withdrawalColdkey, AmountRao: "25"})
	reader := &finalTestChainReader{
		evidence:            &evidence,
		claimCreditBaseline: map[string]string{coldkey: "50", withdrawalColdkey: "25"},
		claimCreditTerminal: map[string]string{coldkey: "0", withdrawalColdkey: "0"},
	}
	if err := verifyFinalSemanticVaultClaims(context.Background(), &evidence, reader, finalSemanticDiscardExchanges); err != nil {
		t.Fatalf("prior-credit withdrawal reconciliation: %v", err)
	}
}

// Rejects payment totals that exceed prior credit plus represented claims.
func TestFinalSemanticVaultClaimAuditRejectsClaimCreditUnderflow(t *testing.T) {
	evidence := finalSemanticEVMAuditFixture()
	coldkey := strings.ToLower(evidence.Epochs[0].Claims[0].Payee)
	for index := range evidence.ClaimPayments {
		if strings.EqualFold(evidence.ClaimPayments[index].Coldkey, coldkey) {
			evidence.ClaimPayments[index].AmountRao = "999999999999999999999"
			break
		}
	}
	reader := &finalTestChainReader{
		evidence:            &evidence,
		claimCreditBaseline: map[string]string{coldkey: "0"},
		claimCreditTerminal: map[string]string{coldkey: "0"},
	}
	if err := verifyFinalSemanticVaultClaims(context.Background(), &evidence, reader, finalSemanticDiscardExchanges); err == nil || !strings.Contains(err.Error(), "underflows") {
		t.Fatalf("claim-credit underflow error=%v", err)
	}
}

// Rejects a terminal credit balance that does not reconcile to the ledger.
func TestFinalSemanticVaultClaimAuditRejectsTerminalCreditSubstitution(t *testing.T) {
	evidence := finalSemanticEVMAuditFixture()
	coldkey := strings.ToLower(evidence.Epochs[0].Claims[0].Payee)
	reader := &finalTestChainReader{
		evidence:            &evidence,
		claimCreditBaseline: map[string]string{coldkey: "0"},
		claimCreditTerminal: map[string]string{coldkey: "1"},
	}
	if err := verifyFinalSemanticVaultClaims(context.Background(), &evidence, reader, finalSemanticDiscardExchanges); err == nil || !strings.Contains(err.Error(), "terminal balance") {
		t.Fatalf("terminal claim-credit substitution error=%v", err)
	}
}

// Rejects a missing record when settlement totals still claim it was paid.
func TestFinalClaimPaymentLedgerRejectsMissingPayment(t *testing.T) {
	evidence := finalSemanticClaimPaymentLedgerFixture()
	if err := verifyFinalClaimPayments(&evidence); err != nil {
		t.Fatalf("baseline ClaimPaid ledger: %v", err)
	}
	evidence.ClaimPayments = evidence.ClaimPayments[:len(evidence.ClaimPayments)-1]
	if err := verifyFinalClaimPayments(&evidence); err == nil || !strings.Contains(err.Error(), "does not equal") {
		t.Fatalf("missing ClaimPaid record error=%v", err)
	}
}

// Rejects duplicate receipt identity rather than accepting duplicate payment.
func TestFinalClaimPaymentLedgerRejectsDuplicateReceipt(t *testing.T) {
	evidence := finalSemanticClaimPaymentLedgerFixture()
	duplicate := evidence.ClaimPayments[len(evidence.ClaimPayments)-1]
	evidence.ClaimPayments = append(evidence.ClaimPayments, duplicate)
	if err := verifyFinalClaimPayments(&evidence); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("duplicate ClaimPaid receipt error=%v", err)
	}
}

// Accepts a nonzero settlement-epoch conviction increment in the signed tier.
func TestFinalSemanticCycleConvictionAuditIncludesEpochIncrement(t *testing.T) {
	evidence := finalSemanticEVMAuditFixture()
	cycle := evidence.Validators[0].Cycles[0]
	reader := &finalTestChainReader{evidence: &evidence, epochConvictionAddedRao: "13"}
	if err := verifyFinalSemanticCycleConvictionPrincipals(context.Background(), reader, cycle, finalSemanticDiscardExchanges); err != nil {
		t.Fatalf("nonzero epoch conviction increment: %v", err)
	}
}

// Deliberately returns an increment for the wrong operator to exercise binding.
type finalSemanticSwappedEpochIncrementReader struct {
	*finalTestChainReader
}

// Changes the returned operator id after an otherwise valid underlying read.
func (self finalSemanticSwappedEpochIncrementReader) EpochConvictionAdded(ctx context.Context, epoch, noID uint64, head ChainHead) (FinalEpochConvictionAddedChainState, []FinalRPCExchange, error) {
	state, exchanges, err := self.finalTestChainReader.EpochConvictionAdded(ctx, epoch, noID, head)
	if err == nil {
		state.NoID++
	}
	return state, exchanges, err
}

// Rejects an increment result whose operator id was swapped at the same head.
func TestFinalSemanticCycleConvictionAuditRejectsSwappedEpochIncrement(t *testing.T) {
	evidence := finalSemanticEVMAuditFixture()
	cycle := evidence.Validators[0].Cycles[0]
	reader := finalSemanticSwappedEpochIncrementReader{finalTestChainReader: &finalTestChainReader{evidence: &evidence}}
	if err := verifyFinalSemanticCycleConvictionPrincipals(context.Background(), reader, cycle, finalSemanticDiscardExchanges); err == nil || !strings.Contains(err.Error(), "mismatched snapshot") {
		t.Fatalf("swapped epoch conviction increment error=%v", err)
	}
}

// Rejects an implementation changed at a historical head then restored later.
func TestFinalSemanticCoordinatorRuntimeAuditRejectsHistoricalUpgradeBack(t *testing.T) {
	evidence := finalSemanticEVMAuditFixture()
	reader := &finalTestChainReader{evidence: &evidence, corruptRuntime: true}
	heads := []ChainHead{evidence.EVMCampaignStartHead, evidence.Window.BaselineHead, evidence.EVMTerminalHead}
	if err := verifyFinalSemanticCoordinatorRuntimes(context.Background(), &evidence, reader, heads, finalSemanticDiscardExchanges); err == nil || !strings.Contains(err.Error(), "implementation/runtime differs") {
		t.Fatalf("historical upgrade-back error=%v", err)
	}
}
