// These checks bind canonical receipt logs to typed release evidence and reject
// malformed or substituted payloads.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/ss58"
)

// Groups the canonical receipts and log slices used by payload checks.
type finalReceiptPayloadFixture struct {
	evidence     *FinalSemanticEvidence
	registration FinalEVMReceipt
	deposit      FinalEVMReceipt
	capture      FinalEVMReceipt
	root         FinalEVMReceipt
	finalize     FinalEVMReceipt
	claim        FinalEVMReceipt
	depositLogs  []finalCanonicalEVMLog
	logs         map[string][]finalCanonicalEVMLog
}

// Accepts the complete ordinary economic receipt set.
func TestFinalSemanticReceiptPayloadBindsReleaseEconomicEvents(t *testing.T) {
	fixture := newFinalReceiptPayloadFixture(t)
	for _, item := range []struct {
		label   string
		receipt FinalEVMReceipt
		logs    []finalCanonicalEVMLog
	}{
		{label: "pool registration", receipt: fixture.registration, logs: fixture.logs[fixture.registration.TransactionHash]},
		{label: "demand deposit and reserve principal", receipt: fixture.deposit, logs: fixture.depositLogs},
		{label: "emission capture", receipt: fixture.capture, logs: fixture.logs[fixture.capture.TransactionHash]},
		{label: "payout root", receipt: fixture.root, logs: fixture.logs[fixture.root.TransactionHash]},
		{label: "entitlement finalize", receipt: fixture.finalize, logs: fixture.logs[fixture.finalize.TransactionHash]},
		{label: "claim payment", receipt: fixture.claim, logs: fixture.logs[fixture.claim.TransactionHash]},
	} {
		if _, err := verifyFinalSemanticReceiptPayload(fixture.evidence, item.receipt, item.logs); err != nil {
			t.Fatalf("%s: %v", item.label, err)
		}
	}
}

// Accepts a standalone credit withdrawal recorded independently of a claim.
func TestFinalSemanticReceiptPayloadBindsWithdrawalOnlyClaimPaid(t *testing.T) {
	fixture := newFinalReceiptPayloadFixture(t)
	block := ChainHead{Number: 57, Hash: finalPayloadTestHash(0x57).Hex()}
	coldkey := finalPayloadTestBytes32(0x7b)
	logs := []finalCanonicalEVMLog{finalPayloadTestEvent(t, SettlementVaultABI, "ClaimPaid", fixture.evidence.Deployment.SettlementVault, finalPayloadTestHash(0xab).Hex(), block, 0, map[string]any{
		"coldkey": coldkey, "amount": big.NewInt(25), "relayer": common.HexToAddress("0x6000000000000000000000000000000000000006"),
	})}
	receipt := finalPayloadTestReceipt(t, logs)
	fixture.evidence.ClaimPayments = append(fixture.evidence.ClaimPayments, FinalClaimPaymentEvidence{Coldkey: "0x" + fmt.Sprintf("%x", coldkey[:]), AmountRao: "25", Receipt: receipt})
	if _, err := verifyFinalSemanticReceiptPayload(fixture.evidence, receipt, logs); err != nil {
		t.Fatalf("withdrawal-only ClaimPaid binding: %v", err)
	}
}

// Rejects a payment event without an exact canonical payment record.
func TestFinalSemanticReceiptPayloadRejectsUnrepresentedClaimPaid(t *testing.T) {
	fixture := newFinalReceiptPayloadFixture(t)
	block := ChainHead{Number: 57, Hash: finalPayloadTestHash(0x57).Hex()}
	logs := []finalCanonicalEVMLog{finalPayloadTestEvent(t, SettlementVaultABI, "ClaimPaid", fixture.evidence.Deployment.SettlementVault, finalPayloadTestHash(0xac).Hex(), block, 0, map[string]any{
		"coldkey": finalPayloadTestBytes32(0x7c), "amount": big.NewInt(25), "relayer": common.HexToAddress("0x6000000000000000000000000000000000000006"),
	})}
	receipt := finalPayloadTestReceipt(t, logs)
	if _, err := verifyFinalSemanticReceiptPayload(fixture.evidence, receipt, logs); err == nil || !strings.Contains(err.Error(), "canonical payment-record binding") {
		t.Fatalf("unrepresented ClaimPaid error=%v", err)
	}
}

// Rejects a payment record whose coldkey differs from its emitted event.
func TestFinalSemanticReceiptPayloadRejectsClaimPaidColdkeySubstitution(t *testing.T) {
	fixture := newFinalReceiptPayloadFixture(t)
	fixture.evidence.ClaimPayments[0].Coldkey = finalTestHex(0xee)
	if _, err := verifyFinalSemanticReceiptPayload(fixture.evidence, fixture.claim, fixture.logs[fixture.claim.TransactionHash]); err == nil || !strings.Contains(err.Error(), "coldkey") {
		t.Fatalf("substituted ClaimPaid coldkey error=%v", err)
	}
}

// Replays ABI event payloads from a pinned public receipt response.
func TestPublicFinalSemanticReceiptReplaysCanonicalABIPayload(t *testing.T) {
	fixture := newFinalReceiptPayloadFixture(t)
	wireLogs := make([]map[string]any, len(fixture.depositLogs))
	for index, log := range fixture.depositLogs {
		wireLogs[index] = map[string]any{
			"address": log.Address, "topics": log.Topics, "data": log.Data,
			"blockNumber": fmt.Sprintf("0x%x", log.BlockNumber), "blockHash": log.BlockHash, "transactionHash": log.TransactionHash,
			"transactionIndex": fmt.Sprintf("0x%x", log.TransactionIndex), "logIndex": fmt.Sprintf("0x%x", log.LogIndex), "removed": false,
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var call struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Errorf("decode receipt request: %v", err)
			return
		}
		if call.Method != "eth_getTransactionReceipt" || len(call.Params) != 1 || !strings.Contains(string(call.Params[0]), fixture.deposit.TransactionHash) {
			t.Errorf("unexpected receipt request: method=%s params=%s", call.Method, call.Params)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{
			"jsonrpc": "2.0", "id": call.ID, "result": map[string]any{
				"transactionHash": fixture.deposit.TransactionHash, "blockHash": fixture.deposit.Block.Hash, "blockNumber": fmt.Sprintf("0x%x", fixture.deposit.Block.Number), "status": "0x1", "logs": wireLogs,
			},
		}); err != nil {
			t.Errorf("encode receipt response: %v", err)
		}
	}))
	defer server.Close()
	client, err := rpc.DialHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	reader := &PublicFinalSemanticChainReader{evidence: fixture.evidence, evm: client, evmRetry: immediateFinalSemanticRetryPolicy()}
	state, exchanges, err := reader.EVMReceipt(context.Background(), fixture.deposit)
	if err != nil {
		t.Fatal(err)
	}
	if state.TransactionHash != fixture.deposit.TransactionHash || state.Block != fixture.deposit.Block || state.Status != fixture.deposit.Status || state.LogsHash != fixture.deposit.LogsHash || state.receiptPayload == nil || len(state.receiptPayload.deposits) != 1 || state.receiptPayload.deposits[0].Nonce.Cmp(big.NewInt(5)) != 0 || len(exchanges) != 1 || exchanges[0].PinnedHead != fixture.deposit.Block {
		t.Fatalf("public receipt replay state=%+v exchanges=%+v", state, exchanges)
	}
}

// Binds voluntary conviction fields.
func TestFinalSemanticReceiptPayloadBindsConvictionAdded(t *testing.T) {
	fixture := newFinalReceiptPayloadFixture(t)
	block := ChainHead{Number: 57, Hash: finalPayloadTestHash(0x57).Hex()}
	logs := []finalCanonicalEVMLog{finalPayloadTestEvent(t, CoordinatorABI, "ConvictionAdded", fixture.evidence.Deployment.CoordinatorProxy, finalPayloadTestHash(0xa7).Hex(), block, 0, map[string]any{
		"noId": big.NewInt(1), "epoch": big.NewInt(20), "funder": common.HexToAddress(fixture.evidence.Pools[0].DepositSigner),
		"amount": big.NewInt(11), "policyHash": finalPayloadTestBytes32(0x91), "nonce": big.NewInt(6),
	})}
	receipt := finalPayloadTestReceipt(t, logs)
	fixture.evidence.Pools[0].ConvictionReceipt = receipt
	if _, err := verifyFinalSemanticReceiptPayload(fixture.evidence, receipt, logs); err != nil {
		t.Fatal(err)
	}
}

// Binds deferred capture and dust cases.
func TestFinalSemanticReceiptPayloadBindsDeferredCapture(t *testing.T) {
	fixture := newFinalReceiptPayloadFixture(t)
	block := ChainHead{Number: 57, Hash: finalPayloadTestHash(0x57).Hex()}
	logs := []finalCanonicalEVMLog{
		finalPayloadTestEvent(t, SettlementVaultABI, "EmissionDeferred", fixture.evidence.Deployment.SettlementVault, finalPayloadTestHash(0xa7).Hex(), block, 0, map[string]any{"epoch": big.NewInt(20), "noId": big.NewInt(1)}),
		finalPayloadTestEvent(t, SettlementVaultABI, "EmissionDustDeferred", fixture.evidence.Deployment.SettlementVault, finalPayloadTestHash(0xa7).Hex(), block, 1, map[string]any{
			"epoch": big.NewInt(20), "noId": big.NewInt(1), "poolHotkey": finalPayloadTestBytes32(0x71), "observedAlphaRao": big.NewInt(8), "taoEquivalentRao": big.NewInt(8), "minimumTransferTaoRao": uint64(9),
		}),
	}
	receipt := finalPayloadTestReceipt(t, logs)
	fixture.evidence.Epochs[0].Capture = receipt
	fixture.evidence.Epochs[0].CapturedRao = "0"
	if _, err := verifyFinalSemanticReceiptPayload(fixture.evidence, receipt, logs); err != nil {
		t.Fatal(err)
	}
}

// Binds the carried-value missed-root branch.
func TestFinalSemanticReceiptPayloadBindsRootMissed(t *testing.T) {
	fixture := newFinalReceiptPayloadFixture(t)
	block := ChainHead{Number: 58, Hash: finalPayloadTestHash(0x58).Hex()}
	logs := []finalCanonicalEVMLog{
		finalPayloadTestEvent(t, SettlementVaultABI, "RootMissed", fixture.evidence.Deployment.SettlementVault, finalPayloadTestHash(0xa8).Hex(), block, 0, map[string]any{"epoch": big.NewInt(20), "noId": big.NewInt(1), "carried": big.NewInt(100)}),
		finalPayloadTestEvent(t, CoordinatorABI, "OperatorEpochFinalized", fixture.evidence.Deployment.CoordinatorProxy, finalPayloadTestHash(0xa8).Hex(), block, 1, map[string]any{"epoch": big.NewInt(20), "noId": big.NewInt(1), "rootPresent": false}),
	}
	receipt := finalPayloadTestReceipt(t, logs)
	fixture.evidence.Epochs[0].Root = nil
	fixture.evidence.Epochs[0].RootDisposition = "missed"
	fixture.evidence.Epochs[0].Finalize = receipt
	if _, err := verifyFinalSemanticReceiptPayload(fixture.evidence, receipt, logs); err != nil {
		t.Fatal(err)
	}
}

// Binds credit deferral after a claim.
func TestFinalSemanticReceiptPayloadBindsDeferredClaim(t *testing.T) {
	fixture := newFinalReceiptPayloadFixture(t)
	block := ChainHead{Number: 59, Hash: finalPayloadTestHash(0x59).Hex()}
	payee := finalPayloadTestBytes32(0x74)
	logs := []finalCanonicalEVMLog{
		finalPayloadTestEvent(t, SettlementVaultABI, "Claimed", fixture.evidence.Deployment.SettlementVault, finalPayloadTestHash(0xa9).Hex(), block, 0, map[string]any{
			"epoch": big.NewInt(20), "noId": big.NewInt(1), "coldkey": payee, "shareBps": big.NewInt(10_000), "amount": big.NewInt(100), "relayer": common.HexToAddress("0x6000000000000000000000000000000000000006"),
		}),
		finalPayloadTestEvent(t, SettlementVaultABI, "ClaimPaymentDeferred", fixture.evidence.Deployment.SettlementVault, finalPayloadTestHash(0xa9).Hex(), block, 1, map[string]any{
			"coldkey": payee, "creditAlphaRao": big.NewInt(100), "taoEquivalentRao": big.NewInt(0), "minimumTransferTaoRao": uint64(9), "reason": uint8(1),
		}),
	}
	receipt := finalPayloadTestReceipt(t, logs)
	claim := &fixture.evidence.Epochs[0].Claims[0]
	claim.PaidRao, claim.DeferredRao, claim.Receipt = "0", "100", receipt
	if _, err := verifyFinalSemanticReceiptPayload(fixture.evidence, receipt, logs); err != nil {
		t.Fatal(err)
	}
}

// Accepts historic initial authority after a later terminal rotation.
func TestFinalSemanticReceiptPayloadBindsHistoricScheduleBeforeTerminalRotation(t *testing.T) {
	fixture := newFinalReceiptPayloadFixture(t)
	rotatedHotkey := finalPayloadTestBytes32(0x77)
	rotatedSS58, err := ss58.Encode(rotatedHotkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	fixture.evidence.Pools[0].VersionCount = 2
	fixture.evidence.Pools[0].DepositHotkey = rotatedSS58
	fixture.evidence.Pools[0].DepositSigner = "0x4000000000000000000000000000000000000007"
	fixture.evidence.Pools[0].PayoutRootSigner = "0x5000000000000000000000000000000000000008"
	fixture.evidence.Pools[0].EffectiveEpoch = 21
	fixture.evidence.Pools[0].Active = false
	if _, err := verifyFinalSemanticReceiptPayload(fixture.evidence, fixture.registration, fixture.logs[fixture.registration.TransactionHash]); err != nil {
		t.Fatalf("historic registration schedule rejected after terminal rotation: %v", err)
	}
}

// Rejects future authority projected back into an initial schedule.
func TestFinalSemanticReceiptPayloadRejectsFutureHistoricSchedule(t *testing.T) {
	fixture := newFinalReceiptPayloadFixture(t)
	logs := append([]finalCanonicalEVMLog(nil), fixture.logs[fixture.registration.TransactionHash]...)
	logs[1] = finalPayloadTestEvent(t, CoordinatorABI, "OperatorScheduled", fixture.evidence.Deployment.CoordinatorProxy, fixture.registration.TransactionHash, fixture.registration.Block, 1, map[string]any{
		"noId": big.NewInt(1), "effectiveEpoch": uint64(21), "coldkey": finalPayloadTestBytes32(0x72), "poolHotkey": finalPayloadTestBytes32(0x71), "depositHotkey": finalPayloadTestBytes32(0x73),
		"depositSigner": common.HexToAddress(fixture.evidence.Pools[0].DepositSigner), "rootSigner": common.HexToAddress(fixture.evidence.Pools[0].PayoutRootSigner), "active": true,
	})
	receipt := finalPayloadTestReceipt(t, logs)
	fixture.evidence.Pools[0].Registration = receipt
	if _, err := verifyFinalSemanticReceiptPayload(fixture.evidence, receipt, logs); err == nil || !strings.Contains(err.Error(), "historic authority fields") {
		t.Fatalf("future historic schedule error=%v", err)
	}
}

// Rejects terminal authority or version-count substitution.
func TestFinalSemanticPoolOperatorVersionRejectsTerminalAuthoritySubstitution(t *testing.T) {
	fixture := newFinalReceiptPayloadFixture(t)
	pool := fixture.evidence.Pools[0]
	head := ChainHead{Number: 80, Hash: finalPayloadTestHash(0x80).Hex()}
	state := FinalOperatorVersionChainState{
		NoID: pool.NoID, VersionCount: pool.VersionCount, Coldkey: pool.OperatorColdkey, PoolHotkey: pool.Hotkey, DepositHotkey: pool.DepositHotkey,
		DepositSigner: pool.DepositSigner, RootSigner: pool.PayoutRootSigner, EffectiveEpoch: pool.EffectiveEpoch, Active: pool.Active, Block: head,
	}
	if err := verifyFinalSemanticPoolOperatorVersion(pool, state, head); err != nil {
		t.Fatal(err)
	}
	state.RootSigner = "0x5000000000000000000000000000000000000008"
	if err := verifyFinalSemanticPoolOperatorVersion(pool, state, head); err == nil || !strings.Contains(err.Error(), "terminal pool evidence") {
		t.Fatalf("substituted terminal root signer error=%v", err)
	}
	state = FinalOperatorVersionChainState{
		NoID: pool.NoID, VersionCount: pool.VersionCount + 1, Coldkey: pool.OperatorColdkey, PoolHotkey: pool.Hotkey, DepositHotkey: pool.DepositHotkey,
		DepositSigner: pool.DepositSigner, RootSigner: pool.PayoutRootSigner, EffectiveEpoch: pool.EffectiveEpoch, Active: pool.Active, Block: head,
	}
	if err := verifyFinalSemanticPoolOperatorVersion(pool, state, head); err == nil || !strings.Contains(err.Error(), "terminal pool evidence") {
		t.Fatalf("substituted terminal version count error=%v", err)
	}
}

// Rejects policy or signer substitution in a deposit.
func TestFinalSemanticReceiptPayloadRejectsDepositPolicyAndSignerSubstitution(t *testing.T) {
	for _, item := range []struct {
		label  string
		funder common.Address
		policy [32]byte
	}{
		{label: "policy", funder: common.HexToAddress("0x4000000000000000000000000000000000000004"), policy: finalPayloadTestBytes32(0x92)},
		{label: "signer", funder: common.HexToAddress("0x4000000000000000000000000000000000000007"), policy: finalPayloadTestBytes32(0x91)},
	} {
		fixture := newFinalReceiptPayloadFixture(t)
		logs := append([]finalCanonicalEVMLog(nil), fixture.depositLogs...)
		logs[1] = finalPayloadTestEvent(t, CoordinatorABI, "Deposit", fixture.evidence.Deployment.CoordinatorProxy, fixture.deposit.TransactionHash, fixture.deposit.Block, 1, map[string]any{
			"noId": big.NewInt(1), "epoch": big.NewInt(20), "funder": item.funder, "amount": big.NewInt(30), "policyHash": item.policy, "nonce": big.NewInt(5),
		})
		receipt := finalPayloadTestReceipt(t, logs)
		evidence := finalPayloadTestRebindDepositReceipt(fixture.evidence, receipt)
		if _, err := verifyFinalSemanticReceiptPayload(evidence, receipt, logs); err == nil || !strings.Contains(err.Error(), "policy and signer") {
			t.Errorf("%s substitution error=%v", item.label, err)
		}
	}
}

// Rejects corrupt hashes and noncanonical logs.
func TestFinalSemanticReceiptPayloadRejectsCorruptAndNoncanonicalLogs(t *testing.T) {
	fixture := newFinalReceiptPayloadFixture(t)
	corruptAmount := append([]finalCanonicalEVMLog(nil), fixture.depositLogs...)
	corruptAmount[1] = finalPayloadTestEvent(t, CoordinatorABI, "Deposit", fixture.evidence.Deployment.CoordinatorProxy, fixture.deposit.TransactionHash, fixture.deposit.Block, 1, map[string]any{
		"noId": big.NewInt(1), "epoch": big.NewInt(20), "funder": common.HexToAddress(fixture.evidence.Pools[0].DepositSigner),
		"amount": big.NewInt(31), "policyHash": finalPayloadTestBytes32(0x91), "nonce": big.NewInt(5),
	})
	missingReserve := append([]finalCanonicalEVMLog(nil), fixture.depositLogs[1:]...)
	duplicate := append(append([]finalCanonicalEVMLog(nil), fixture.depositLogs...), fixture.depositLogs[1])
	reordered := append([]finalCanonicalEVMLog(nil), fixture.depositLogs...)
	reordered[0], reordered[1] = reordered[1], reordered[0]
	wrongContract := append([]finalCanonicalEVMLog(nil), fixture.depositLogs...)
	wrongContract[1].Address = strings.ToLower(fixture.evidence.Deployment.ReserveSink)
	wrongTopic := append([]finalCanonicalEVMLog(nil), fixture.depositLogs...)
	wrongTopic[1].Topics = append([]string(nil), wrongTopic[1].Topics...)
	wrongTopic[1].Topics[0] = finalPayloadTestHash(0xef).Hex()
	wrongData := append([]finalCanonicalEVMLog(nil), fixture.depositLogs...)
	wrongData[1].Data = "0x1234"

	for _, item := range []struct {
		label string
		logs  []finalCanonicalEVMLog
		want  string
		valid bool
	}{
		{label: "corrupt event amount", logs: corruptAmount, want: "cumulative deposit", valid: true},
		{label: "missing reserve event", logs: missingReserve, want: "ReservePrincipalAdded", valid: true},
		{label: "duplicate log", logs: duplicate, want: "duplicate position"},
		{label: "reordered logs", logs: reordered, want: "canonical order", valid: true},
		{label: "wrong contract", logs: wrongContract, want: "unknown event topic", valid: true},
		{label: "wrong topic", logs: wrongTopic, want: "unknown event topic", valid: true},
		{label: "wrong data", logs: wrongData, want: "decode release event", valid: true},
	} {
		receipt := finalPayloadTestUncheckedReceipt(item.logs)
		if item.valid {
			receipt = finalPayloadTestReceipt(t, item.logs)
		}
		evidence := finalPayloadTestRebindDepositReceipt(fixture.evidence, receipt)
		if _, err := verifyFinalSemanticReceiptPayload(evidence, receipt, item.logs); err == nil || !strings.Contains(err.Error(), item.want) {
			t.Errorf("%s error=%v, want %q", item.label, err, item.want)
		}
	}
	if _, err := verifyFinalSemanticReceiptPayload(fixture.evidence, fixture.root, fixture.logs[fixture.capture.TransactionHash]); err == nil || !strings.Contains(err.Error(), "canonical logs hash") {
		t.Fatalf("swapped receipt error=%v", err)
	}
}

// Queries the settlement epoch rather than the source payout epoch.
func TestFinalSemanticEpochDepositUsesSettlementEpochNotSourceEpoch(t *testing.T) {
	t.Parallel()
	source, _ := finalSemanticFixture(t)
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	cycle := draft.Validators[0].Cycles[0]
	if cycle.SettlementEpoch == 0 || len(cycle.Pools) == 0 {
		t.Fatal("fixture has no validator deposit audit")
	}
	cycle.Pools[0].SourceEpoch = cycle.SettlementEpoch + 99
	reader := &finalReceiptEpochDepositSpy{finalTestChainReader: &finalTestChainReader{evidence: draft}}
	if err := verifyFinalSemanticCycleEpochDeposits(context.Background(), reader, cycle, func(_ string, _ ChainHead, _ []FinalRPCExchange) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if reader.epoch != cycle.SettlementEpoch {
		t.Fatalf("epochDeposits queried epoch=%d, want settlement epoch %d (source epoch %d)", reader.epoch, cycle.SettlementEpoch, cycle.Pools[0].SourceEpoch)
	}
}

// Records the epoch requested by the public audit.
type finalReceiptEpochDepositSpy struct {
	*finalTestChainReader
	epoch uint64
}

// Records the request before delegating to the standard fixture reader.
func (self *finalReceiptEpochDepositSpy) EpochDeposit(ctx context.Context, epoch, noID uint64, head ChainHead) (FinalEpochDepositChainState, []FinalRPCExchange, error) {
	self.epoch = epoch
	return self.finalTestChainReader.EpochDeposit(ctx, epoch, noID, head)
}

// Rejects a receipt-only schema before payload exchanges are required.
func TestFinalPublicChainVerificationRejectsV2ReceiptOnlyTranscript(t *testing.T) {
	verification := &FinalPublicChainVerification{Schema: "urnetwork-final-public-chain-verification-v2", Exchanges: []FinalRPCExchange{{}}}
	if err := finalizePublicChainVerification(verification, 945, finalTestHex(0x01)); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("v2 public verification schema error=%v", err)
	}
}

// Requires complete ordered decoded payloads for dishonest-deposit receipts.
func TestFinalSemanticDishonestDepositReceiptPayloadIsMandatoryAndOrdered(t *testing.T) {
	underpayment := FinalEVMReceipt{TransactionHash: finalTestHex(0xa7), Block: ChainHead{Number: 70, Hash: finalTestHex(0x70)}, Status: "success", LogsHash: finalTestHex(0xb7)}
	recovery := FinalEVMReceipt{TransactionHash: finalTestHex(0xa8), Block: ChainHead{Number: 71, Hash: finalTestHex(0x71)}, Status: "success", LogsHash: finalTestHex(0xb8)}
	dishonest := &FinalDishonestDepositEvidence{
		NoID: 1, ObservedDepositRao: "10", RecoveryObservedDepositRao: "20", UnderpaymentReceipt: underpayment, RecoveryDepositReceipt: recovery,
		Penalties:  []FinalDishonestDepositDecision{{Cycle: FinalCRv4Cycle{SettlementEpoch: 20}}},
		Recoveries: []FinalDishonestDepositDecision{{Cycle: FinalCRv4Cycle{SettlementEpoch: 21}}},
	}
	base := &finalTestChainReader{evidence: &FinalSemanticEvidence{PolicyHash: finalTestHex(0x91), DishonestDeposit: dishonest}}
	for _, item := range []struct {
		label    string
		reader   *finalReceiptPayloadOverrideReader
		wantText string
	}{
		{label: "nil payload", reader: &finalReceiptPayloadOverrideReader{finalTestChainReader: base, nilPayload: true}, wantText: "projection is incomplete"},
		{label: "reused nonce", reader: &finalReceiptPayloadOverrideReader{finalTestChainReader: base, recoveryNonce: 1}, wantText: "does not follow"},
	} {
		if err := verifyFinalSemanticDishonestDepositReceiptsOnChain(context.Background(), item.reader, dishonest, func(_ string, _ ChainHead, _ []FinalRPCExchange) error { return nil }); err == nil || !strings.Contains(err.Error(), item.wantText) {
			t.Errorf("%s error=%v, want %q", item.label, err, item.wantText)
		}
	}
}

// Rejects nonce reuse across underpayment and recovery receipts.
func TestFinalSemanticReceiptPayloadBindsDishonestDepositNonceOrder(t *testing.T) {
	fixture := newFinalReceiptPayloadFixture(t)
	underpaymentBlock := ChainHead{Number: 70, Hash: finalPayloadTestHash(0x70).Hex()}
	recoveryBlock := ChainHead{Number: 71, Hash: finalPayloadTestHash(0x71).Hex()}
	underpaymentLogs := []finalCanonicalEVMLog{finalPayloadTestEvent(t, CoordinatorABI, "Deposit", fixture.evidence.Deployment.CoordinatorProxy, finalPayloadTestHash(0xb1).Hex(), underpaymentBlock, 0, map[string]any{
		"noId": big.NewInt(1), "epoch": big.NewInt(20), "funder": common.HexToAddress(fixture.evidence.Pools[0].DepositSigner), "amount": big.NewInt(10), "policyHash": finalPayloadTestBytes32(0x91), "nonce": big.NewInt(5),
	})}
	recoveryLogs := []finalCanonicalEVMLog{finalPayloadTestEvent(t, CoordinatorABI, "Deposit", fixture.evidence.Deployment.CoordinatorProxy, finalPayloadTestHash(0xb2).Hex(), recoveryBlock, 0, map[string]any{
		"noId": big.NewInt(1), "epoch": big.NewInt(21), "funder": common.HexToAddress(fixture.evidence.Pools[0].DepositSigner), "amount": big.NewInt(20), "policyHash": finalPayloadTestBytes32(0x91), "nonce": big.NewInt(6),
	})}
	underpayment := finalPayloadTestReceipt(t, underpaymentLogs)
	recovery := finalPayloadTestReceipt(t, recoveryLogs)
	fixture.evidence.DishonestDeposit = &FinalDishonestDepositEvidence{
		NoID: 1, ObservedDepositRao: "10", RecoveryObservedDepositRao: "20", UnderpaymentReceipt: underpayment, RecoveryDepositReceipt: recovery,
		Penalties:  []FinalDishonestDepositDecision{{Cycle: FinalCRv4Cycle{SettlementEpoch: 20}}},
		Recoveries: []FinalDishonestDepositDecision{{Cycle: FinalCRv4Cycle{SettlementEpoch: 21}}},
	}
	reader := &finalReceiptPayloadLogReader{
		finalTestChainReader: &finalTestChainReader{evidence: fixture.evidence},
		logsByTransaction: map[string][]finalCanonicalEVMLog{
			underpayment.TransactionHash: underpaymentLogs,
			recovery.TransactionHash:     recoveryLogs,
		},
	}
	if err := verifyFinalSemanticDishonestDepositReceiptsOnChain(context.Background(), reader, fixture.evidence.DishonestDeposit, func(_ string, _ ChainHead, _ []FinalRPCExchange) error { return nil }); err != nil {
		t.Fatal(err)
	}
	// Reusing the underpayment nonce in a separately valid ABI event must not
	// be hidden by the receipt hashes: the cross-receipt order is release data.
	recoveryLogs[0] = finalPayloadTestEvent(t, CoordinatorABI, "Deposit", fixture.evidence.Deployment.CoordinatorProxy, recovery.TransactionHash, recovery.Block, 0, map[string]any{
		"noId": big.NewInt(1), "epoch": big.NewInt(21), "funder": common.HexToAddress(fixture.evidence.Pools[0].DepositSigner), "amount": big.NewInt(20), "policyHash": finalPayloadTestBytes32(0x91), "nonce": big.NewInt(5),
	})
	recovery = finalPayloadTestReceipt(t, recoveryLogs)
	fixture.evidence.DishonestDeposit.RecoveryDepositReceipt = recovery
	reader.logsByTransaction[recovery.TransactionHash] = recoveryLogs
	if err := verifyFinalSemanticDishonestDepositReceiptsOnChain(context.Background(), reader, fixture.evidence.DishonestDeposit, func(_ string, _ ChainHead, _ []FinalRPCExchange) error { return nil }); err == nil || !strings.Contains(err.Error(), "does not follow") {
		t.Fatalf("reused ABI nonce error=%v", err)
	}
}

// Injects absent or substituted decoded deposits into a fixture reader.
type finalReceiptPayloadOverrideReader struct {
	*finalTestChainReader
	nilPayload    bool
	recoveryNonce int64
}

// Replays stored canonical logs instead of an RPC response.
type finalReceiptPayloadLogReader struct {
	*finalTestChainReader
	logsByTransaction map[string][]finalCanonicalEVMLog
}

// Decodes stored transaction logs into the public reader response.
func (self *finalReceiptPayloadLogReader) EVMReceipt(_ context.Context, receipt FinalEVMReceipt) (FinalEVMReceiptState, []FinalRPCExchange, error) {
	logs, ok := self.logsByTransaction[receipt.TransactionHash]
	if !ok {
		return FinalEVMReceiptState{}, nil, fmt.Errorf("fixture receipt %s is absent", receipt.TransactionHash)
	}
	payload, err := verifyFinalSemanticReceiptPayload(self.evidence, receipt, logs)
	if err != nil {
		return FinalEVMReceiptState{}, nil, err
	}
	state := FinalEVMReceiptState{TransactionHash: receipt.TransactionHash, Block: receipt.Block, Status: receipt.Status, LogsHash: receipt.LogsHash, receiptPayload: payload}
	return state, self.exchange("evm", "eth_getTransactionReceipt", receipt.Block), nil
}

// Optionally removes payloads or changes the recovery nonce after delegation.
func (self *finalReceiptPayloadOverrideReader) EVMReceipt(ctx context.Context, receipt FinalEVMReceipt) (FinalEVMReceiptState, []FinalRPCExchange, error) {
	state, exchanges, err := self.finalTestChainReader.EVMReceipt(ctx, receipt)
	if err != nil || self.nilPayload {
		state.receiptPayload = nil
		return state, exchanges, err
	}
	if finalSemanticReceiptMatches(receipt, self.evidence.DishonestDeposit.RecoveryDepositReceipt) && self.recoveryNonce != 0 {
		state.receiptPayload.deposits[0].Nonce = big.NewInt(self.recoveryNonce)
	}
	return state, exchanges, nil
}

// Constructs one ordinary multi-contract receipt sequence and matching evidence.
func newFinalReceiptPayloadFixture(t *testing.T) *finalReceiptPayloadFixture {
	t.Helper()
	coordinator := "0x1000000000000000000000000000000000000001"
	bootstrap := "0x1000000000000000000000000000000000000011"
	implementation := "0x1000000000000000000000000000000000000012"
	batcher := "0x1000000000000000000000000000000000000013"
	governanceDrill := "0x1000000000000000000000000000000000000014"
	precompileProbe := "0x1000000000000000000000000000000000000015"
	vault := "0x2000000000000000000000000000000000000002"
	reserve := "0x3000000000000000000000000000000000000003"
	funder := "0x4000000000000000000000000000000000000004"
	rootSigner := "0x5000000000000000000000000000000000000005"
	relayer := "0x6000000000000000000000000000000000000006"
	poolKey := finalPayloadTestBytes32(0x71)
	coldkey := finalPayloadTestBytes32(0x72)
	depositKey := finalPayloadTestBytes32(0x73)
	payee := finalPayloadTestBytes32(0x74)
	payoutRoot := finalPayloadTestBytes32(0x75)
	artifactHash := finalPayloadTestBytes32(0x76)
	policyHash := finalPayloadTestBytes32(0x91)
	poolSS58, err := ss58.Encode(poolKey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	coldkeySS58, err := ss58.Encode(coldkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	depositSS58, err := ss58.Encode(depositKey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	block := func(number uint64, seed byte) ChainHead {
		return ChainHead{Number: number, Hash: finalPayloadTestHash(seed).Hex()}
	}
	registrationBlock, depositBlock := block(51, 0x51), block(52, 0x52)
	captureBlock, rootBlock, finalizeBlock, claimBlock := block(53, 0x53), block(54, 0x54), block(55, 0x55), block(56, 0x56)
	registrationLogs := []finalCanonicalEVMLog{
		finalPayloadTestEvent(t, SettlementVaultABI, "PoolRegistered", vault, finalPayloadTestHash(0xa1).Hex(), registrationBlock, 0, map[string]any{"noId": big.NewInt(1), "hotkey": poolKey, "uid": uint16(7)}),
		finalPayloadTestEvent(t, CoordinatorABI, "OperatorScheduled", coordinator, finalPayloadTestHash(0xa1).Hex(), registrationBlock, 1, map[string]any{
			"noId": big.NewInt(1), "effectiveEpoch": uint64(20), "coldkey": coldkey, "poolHotkey": poolKey, "depositHotkey": depositKey,
			"depositSigner": common.HexToAddress(funder), "rootSigner": common.HexToAddress(rootSigner), "active": true,
		}),
	}
	depositLogs := []finalCanonicalEVMLog{
		finalPayloadTestEvent(t, ReserveSinkABI, "ReservePrincipalAdded", reserve, finalPayloadTestHash(0xa2).Hex(), depositBlock, 0, map[string]any{"epoch": big.NewInt(20), "noId": big.NewInt(1), "amount": big.NewInt(30), "operatorPrincipal": big.NewInt(30), "totalPrincipal": big.NewInt(30), "liveStake": big.NewInt(40)}),
		finalPayloadTestEvent(t, CoordinatorABI, "Deposit", coordinator, finalPayloadTestHash(0xa2).Hex(), depositBlock, 1, map[string]any{"noId": big.NewInt(1), "epoch": big.NewInt(20), "funder": common.HexToAddress(funder), "amount": big.NewInt(30), "policyHash": policyHash, "nonce": big.NewInt(5)}),
	}
	captureLogs := []finalCanonicalEVMLog{finalPayloadTestEvent(t, SettlementVaultABI, "EmissionCaptured", vault, finalPayloadTestHash(0xa3).Hex(), captureBlock, 0, map[string]any{"epoch": big.NewInt(20), "noId": big.NewInt(1), "poolHotkey": poolKey, "amount": big.NewInt(100)})}
	rootLogs := []finalCanonicalEVMLog{finalPayloadTestEvent(t, CoordinatorABI, "OperatorRootCommitted", coordinator, finalPayloadTestHash(0xa4).Hex(), rootBlock, 0, map[string]any{"epoch": big.NewInt(20), "noId": big.NewInt(1), "payoutRoot": payoutRoot, "artifactHash": artifactHash, "committer": common.HexToAddress(rootSigner)})}
	finalizeLogs := []finalCanonicalEVMLog{
		finalPayloadTestEvent(t, SettlementVaultABI, "EntitlementFinalized", vault, finalPayloadTestHash(0xa5).Hex(), finalizeBlock, 0, map[string]any{"epoch": big.NewInt(20), "noId": big.NewInt(1), "payoutRoot": payoutRoot, "artifactHash": artifactHash, "total": big.NewInt(100), "expiryBlock": uint64(80)}),
		finalPayloadTestEvent(t, CoordinatorABI, "OperatorEpochFinalized", coordinator, finalPayloadTestHash(0xa5).Hex(), finalizeBlock, 1, map[string]any{"epoch": big.NewInt(20), "noId": big.NewInt(1), "rootPresent": true}),
	}
	claimLogs := []finalCanonicalEVMLog{
		finalPayloadTestEvent(t, SettlementVaultABI, "Claimed", vault, finalPayloadTestHash(0xa6).Hex(), claimBlock, 0, map[string]any{"epoch": big.NewInt(20), "noId": big.NewInt(1), "coldkey": payee, "shareBps": big.NewInt(10_000), "amount": big.NewInt(100), "relayer": common.HexToAddress(relayer)}),
		finalPayloadTestEvent(t, SettlementVaultABI, "ClaimPaid", vault, finalPayloadTestHash(0xa6).Hex(), claimBlock, 1, map[string]any{"coldkey": payee, "amount": big.NewInt(100), "relayer": common.HexToAddress(relayer)}),
	}
	registration, deposit := finalPayloadTestReceipt(t, registrationLogs), finalPayloadTestReceipt(t, depositLogs)
	capture, root, finalize, claim := finalPayloadTestReceipt(t, captureLogs), finalPayloadTestReceipt(t, rootLogs), finalPayloadTestReceipt(t, finalizeLogs), finalPayloadTestReceipt(t, claimLogs)
	runtimeHash := func(seed byte) string {
		return strings.ToLower(finalPayloadTestHash(seed).Hex())
	}
	runtimeRoots := []FinalReleaseRuntimeRoot{
		{Name: "coordinator_bootstrap_implementation", Address: bootstrap, RuntimeCodeHash: runtimeHash(0xb1), ReleaseRuntimeHash: runtimeHash(0xc1)},
		{Name: "coordinator_proxy", Address: coordinator, RuntimeCodeHash: runtimeHash(0xb2), ReleaseRuntimeHash: runtimeHash(0xc2)},
		{Name: "coordinator_upgrade_implementation", Address: implementation, RuntimeCodeHash: runtimeHash(0xb3), ReleaseRuntimeHash: runtimeHash(0xc3)},
		{Name: "fleet_batcher", Address: batcher, RuntimeCodeHash: runtimeHash(0xb4), ReleaseRuntimeHash: runtimeHash(0xc4)},
		{Name: "governance_drill_implementation", Address: governanceDrill, RuntimeCodeHash: runtimeHash(0xb5), ReleaseRuntimeHash: runtimeHash(0xc5)},
		{Name: "precompile_probe", Address: precompileProbe, RuntimeCodeHash: runtimeHash(0xb6), ReleaseRuntimeHash: runtimeHash(0xc6)},
		{Name: "reserve_sink", Address: reserve, RuntimeCodeHash: runtimeHash(0xb7), ReleaseRuntimeHash: runtimeHash(0xc7)},
		{Name: "settlement_vault", Address: vault, RuntimeCodeHash: runtimeHash(0xb8), ReleaseRuntimeHash: runtimeHash(0xc8)},
	}
	evidence := &FinalSemanticEvidence{
		PolicyHash: policyHashHex(policyHash), Window: ScenarioAcceptanceWindow{BaselineHead: ChainHead{Number: 50, Hash: finalPayloadTestHash(0x50).Hex()}},
		Deployment: FinalContractDeploymentEvidence{
			CoordinatorProxy: coordinator, CoordinatorImplementation: implementation, SettlementVault: vault, ReserveSink: reserve, VaultMinimumTransferTaoRao: 9,
			CoordinatorProxyCodeHash: runtimeHash(0xb2), ImplementationCodeHash: runtimeHash(0xb3), SettlementVaultCodeHash: runtimeHash(0xb8), ReserveSinkCodeHash: runtimeHash(0xb7), RuntimeRoots: runtimeRoots,
		},
		Pools:   []FinalPoolUIDEvidence{{NoID: 1, UID: 7, Hotkey: poolSS58, OperatorColdkey: coldkeySS58, DepositHotkey: depositSS58, DepositSigner: funder, PayoutRootSigner: rootSigner, EffectiveEpoch: 20, VersionCount: 1, Active: true, Registration: registration, ConvictionReceipt: deposit}},
		Reserve: FinalReserveEvidence{PrincipalAdditions: []FinalReservePrincipalAddedEvidence{{Epoch: 20, NoID: 1, AmountRao: "30", OperatorPrincipalRao: "30", TotalPrincipalRao: "30", LiveStakeRao: "40", Receipt: deposit}}},
	}
	evidence.Validators = []FinalValidatorIdentityEvidence{{Cycles: []FinalCRv4Cycle{{SettlementEpoch: 20, EVMSnapshot: ChainHead{Number: 60, Hash: finalPayloadTestHash(0x60).Hex()}, Pools: []FinalPoolWeightEvidence{{NoID: 1, SourceEpoch: 20, ObservedDepositRao: "30", DepositReceipt: deposit, RootCommitter: rootSigner}}}}}}
	evidence.Epochs = []FinalEpochOperatorEvidence{{Epoch: 20, NoID: 1, Capture: capture, RootDisposition: "committed", Root: &root, Finalize: finalize, PayoutRoot: "0x" + fmt.Sprintf("%x", payoutRoot[:]), ArtifactHash: "0x" + fmt.Sprintf("%x", artifactHash[:]), CapturedRao: "100", TotalRao: "100", ExpiryBlock: 80, Claims: []FinalClaimEvidence{{LeafIndex: 0, Payee: "0x" + fmt.Sprintf("%x", payee[:]), ShareBPS: 10_000, ClaimedRao: "100", PaidRao: "100", DeferredRao: "0", Receipt: claim}}}}
	evidence.ClaimPayments = []FinalClaimPaymentEvidence{{Coldkey: "0x" + fmt.Sprintf("%x", payee[:]), AmountRao: "100", Receipt: claim}}
	return &finalReceiptPayloadFixture{evidence: evidence, registration: registration, deposit: deposit, capture: capture, root: root, finalize: finalize, claim: claim, depositLogs: depositLogs, logs: map[string][]finalCanonicalEVMLog{
		registration.TransactionHash: registrationLogs,
		deposit.TransactionHash:      depositLogs,
		capture.TransactionHash:      captureLogs,
		root.TransactionHash:         rootLogs,
		finalize.TransactionHash:     finalizeLogs,
		claim.TransactionHash:        claimLogs,
	}}
}

// Clones evidence while replacing every binding of the shared deposit receipt.
func finalPayloadTestRebindDepositReceipt(source *FinalSemanticEvidence, receipt FinalEVMReceipt) *FinalSemanticEvidence {
	copy := *source
	copy.Pools = append([]FinalPoolUIDEvidence(nil), source.Pools...)
	copy.Pools[0].ConvictionReceipt = receipt
	copy.Reserve = source.Reserve
	copy.Reserve.PrincipalAdditions = append([]FinalReservePrincipalAddedEvidence(nil), source.Reserve.PrincipalAdditions...)
	copy.Reserve.PrincipalAdditions[0].Receipt = receipt
	copy.Validators = append([]FinalValidatorIdentityEvidence(nil), source.Validators...)
	copy.Validators[0].Cycles = append([]FinalCRv4Cycle(nil), source.Validators[0].Cycles...)
	copy.Validators[0].Cycles[0].Pools = append([]FinalPoolWeightEvidence(nil), source.Validators[0].Cycles[0].Pools...)
	copy.Validators[0].Cycles[0].Pools[0].DepositReceipt = receipt
	return &copy
}

// Encodes one canonical ABI event log from typed values.
func finalPayloadTestEvent(t *testing.T, encodedABI, name, address, transaction string, block ChainHead, logIndex uint64, values map[string]any) finalCanonicalEVMLog {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(encodedABI))
	if err != nil {
		t.Fatal(err)
	}
	event := parsed.Events[name]
	nonIndexed := make([]any, 0, len(event.Inputs))
	topics := []common.Hash{event.ID}
	for _, input := range event.Inputs {
		value, ok := values[input.Name]
		if !ok {
			t.Fatalf("event %s is missing %s", name, input.Name)
		}
		if input.Indexed {
			encodedTopics, topicErr := abi.MakeTopics([]any{value})
			if topicErr != nil || len(encodedTopics) != 1 || len(encodedTopics[0]) != 1 {
				t.Fatalf("event %s topic %s: %v", name, input.Name, topicErr)
			}
			topics = append(topics, encodedTopics[0][0])
			continue
		}
		nonIndexed = append(nonIndexed, value)
	}
	data, err := event.Inputs.NonIndexed().Pack(nonIndexed...)
	if err != nil {
		t.Fatalf("event %s data: %v", name, err)
	}
	encodedTopics := make([]string, len(topics))
	for index, topic := range topics {
		encodedTopics[index] = strings.ToLower(topic.Hex())
	}
	return finalCanonicalEVMLog{Address: strings.ToLower(address), Topics: encodedTopics, Data: "0x" + fmt.Sprintf("%x", data), BlockNumber: block.Number, BlockHash: strings.ToLower(block.Hash), TransactionHash: strings.ToLower(transaction), TransactionIndex: 1, LogIndex: logIndex}
}

// Builds a successful receipt with a canonical log hash.
func finalPayloadTestReceipt(t *testing.T, logs []finalCanonicalEVMLog) FinalEVMReceipt {
	t.Helper()
	if len(logs) == 0 {
		t.Fatal("receipt logs are empty")
	}
	hash, err := finalCanonicalReceiptLogsHash(logs)
	if err != nil {
		t.Fatal(err)
	}
	return FinalEVMReceipt{TransactionHash: logs[0].TransactionHash, Block: ChainHead{Number: logs[0].BlockNumber, Hash: logs[0].BlockHash}, Status: "success", LogsHash: hash}
}

// Builds a receipt with intentionally incorrect log identity for negative tests.
func finalPayloadTestUncheckedReceipt(logs []finalCanonicalEVMLog) FinalEVMReceipt {
	return FinalEVMReceipt{TransactionHash: logs[0].TransactionHash, Block: ChainHead{Number: logs[0].BlockNumber, Hash: logs[0].BlockHash}, Status: "success", LogsHash: finalTestHex(0x9f)}
}

// Produces a deterministic 32-byte hash from a repeated seed.
func finalPayloadTestHash(seed byte) common.Hash {
	var result common.Hash
	for index := range result {
		result[index] = seed
	}
	return result
}

// Produces a deterministic bytes32 value from a repeated seed.
func finalPayloadTestBytes32(seed byte) [32]byte {
	var result [32]byte
	for index := range result {
		result[index] = seed
	}
	return result
}

// Encodes a policy hash as canonical lowercase bytes32 text.
func policyHashHex(value [32]byte) string {
	return "0x" + fmt.Sprintf("%x", value[:])
}
