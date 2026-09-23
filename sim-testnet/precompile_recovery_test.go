// Synthetic signed receipts exercise every recovery boundary without a node,
// clock sleeps or private deployment identity.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type precompileRecoveryTestFixture struct {
	cfg        *ResolvedConfig
	plan       *SetupPlan
	roles      *RoleSecrets
	original   *PrecompileConformanceEvidence
	evidence   *PrecompileConformanceEvidence
	completion *PrecompileRecoveryCompletion
	stateDir   string
	entries    []JournalEntry
	reader     *deploymentBoundaryFixture
	base       *precompileProbeSuccessorFixture
}

// The optional residual forces a successful original transfer with new credit
// left at inclusion, followed by the separately bounded reseed and second sweep.
func newPrecompileRecoveryTestFixture(t *testing.T) *precompileRecoveryTestFixture {
	t.Helper()
	return precompileRecoveryTestFixtureWithSampleResidual(t, false)
}
func precompileRecoveryTestFixtureWithSampleResidual(t *testing.T, residual bool) *precompileRecoveryTestFixture {
	t.Helper()
	base := newPrecompileProbeSuccessorFixture(t)
	if err := os.Chmod(base.stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	evidence, entries, reader := precompileProbeSuccessorCallFixture(t, base)
	roles, err := BuildRoleSecrets(base.cfg)
	if err != nil {
		t.Fatal(err)
	}
	f := &precompileRecoveryTestFixture{cfg: base.cfg, plan: base.plan, roles: roles, evidence: evidence, stateDir: base.stateDir, entries: entries[:len(base.entries)+1], reader: reader, base: base}
	f.plan.MaximumEVMFeePerGasWei = 100_000_000_000
	for i := range f.plan.Actions {
		if f.plan.Actions[i].ID == "campaign.evm-gas-reserve" {
			f.plan.Actions[i].Spend.EVMGasWei = "10000000000000000000"
			f.plan.Actions[i].IntentHash, err = actionIntentHash(f.plan.Actions[i])
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	evidence.Battery = completePrecompileEvidence().Battery
	evidence.Seed = PrecompileValueStep{TAORao: 3, ValueWei: "3000000000", AfterRao: 4_000_000_000, DeltaRao: 4_000_000_000, BlockNumber: 211}
	evidence.Forward = PrecompileMoveStep{AmountRao: 2_000_000_000, FromBeforeRao: 4_000_000_000, FromAfterRao: 2_000_000_000, ToAfterRao: 2_000_000_000, BlockNumber: 212}
	evidence.Back = PrecompileMoveStep{AmountRao: 2_000_000_000, FromBeforeRao: 2_000_000_017, FromAfterRao: 17, ToBeforeRao: 2_000_000_021, ToAfterRao: 4_000_000_021, BlockNumber: 213}
	evidence.Snapshot = PrecompileSnapshotStep{BaselineRao: 4_000_000_021, SinceBlock: 214, BlockNumber: 214}
	evidence.Dividend = PrecompileDividendStep{BaselineRao: evidence.Snapshot.BaselineRao, CurrentRao: evidence.Snapshot.BaselineRao + 100, DeltaRao: 100, SinceBlock: 214, FinalizedHead: ChainHead{Number: 220, Hash: common.BigToHash(big.NewInt(1220)).Hex()}}
	evidence.Transfer = PrecompileTransferStep{}
	evidence.Complete = false
	if err := recordPrecompileRoundTripCredits(evidence); err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{"precompile.seed", "precompile.move-forward", "precompile.move-back", "precompile.snapshot"} {
		f.appendOriginal(t, id, uint64(211+index))
	}
	if err := writePrecompileEvidence(f.stateDir, evidence); err != nil {
		t.Fatal(err)
	}
	original, err := loadPrecompileEvidence(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	f.original = original
	budget := PrecompileRecoveryBudget{Schema: "urnetwork-precompile-recovery-budget-v1", PlanHash: f.plan.PlanHash, JournalHash: common.Hash{101}.Hex(), CampaignReserveWei: "10000000000000000000", CommittedOrPendingMaxWei: "0", AvailableWei: "10000000000000000000", MaximumRecoveryWei: "400000006000000000", MaximumReseedWei: "6000000000", Verified: true}
	request, err := newPrecompileRecoveryRequest(f.plan, evidence, budget)
	if err != nil {
		t.Fatal(err)
	}
	authority := PrecompileRecoveryAuthorization{Request: request}
	f.sign(t, request, &authority.Hash, &authority.OwnerSignature, &authority.DeployerSignature)
	evidence.Recovery = &PrecompileRecoveryEvidence{Authorization: authority}
	topup := PrecompileRecoveryStep{Operation: "top-up", QuoteHead: ChainHead{Number: 221, Hash: common.BigToHash(big.NewInt(1221)).Hex()}, QuoteSourceRao: evidence.Dividend.CurrentRao + 10, QuoteDestinationRao: 20, Move: PrecompileMoveStep{AmountRao: precompileRecoveryTopUpRao, FromBeforeRao: evidence.Dividend.CurrentRao + 10, ToBeforeRao: 20}}
	f.appendRecovery(t, topup, 222, map[string]any{"amount": new(big.Int).SetUint64(precompileRecoveryTopUpRao), "fromBefore": new(big.Int).SetUint64(topup.QuoteSourceRao + 2), "fromAfter": new(big.Int).SetUint64(topup.QuoteSourceRao + 2 - precompileRecoveryTopUpRao), "toBefore": big.NewInt(21), "toAfter": new(big.Int).SetUint64(precompileRecoveryTopUpRao + 21)})
	sweep := PrecompileRecoveryStep{Operation: "recover-move", QuoteHead: ChainHead{Number: 223, Hash: common.BigToHash(big.NewInt(1223)).Hex()}, QuoteSourceRao: precompileRecoveryTopUpRao + 25, QuoteDestinationRao: 50, Move: PrecompileMoveStep{AmountRao: precompileRecoveryTopUpRao + 25, FromBeforeRao: precompileRecoveryTopUpRao + 25, ToBeforeRao: 50}}
	f.appendRecovery(t, sweep, 224, map[string]any{"amount": new(big.Int).SetUint64(sweep.Move.AmountRao), "sourceBefore": new(big.Int).SetUint64(sweep.QuoteSourceRao), "sourceAfter": big.NewInt(0), "destinationBefore": big.NewInt(50), "destinationAfter": new(big.Int).SetUint64(50 + sweep.QuoteSourceRao)})
	sample, move, settled, err := precompileRecoveryPositions(evidence)
	if err != nil || move != 0 || !settled {
		t.Fatalf("move recovery: %v", err)
	}
	quote := sample + 20
	evidence.Recovery.SampleTransferQuoteHead = ChainHead{Number: 225, Hash: common.BigToHash(big.NewInt(1225)).Hex()}
	evidence.Recovery.SampleTransferSourceQuoteRao = quote
	evidence.Recovery.SampleTransferDestinationQuoteRao = 100
	evidence.Transfer = PrecompileTransferStep{AmountRao: quote, ProbeBeforeRao: quote, ProviderBeforeRao: 100}
	var extra uint64
	if residual {
		extra = 3
	}
	transfer, err := reconcilePrecompileRecoveryOriginalTransfer(evidence, map[string]any{"amount": new(big.Int).SetUint64(quote), "sourceBefore": new(big.Int).SetUint64(quote + extra), "sourceAfter": new(big.Int).SetUint64(extra), "destinationBefore": big.NewInt(100), "destinationAfter": new(big.Int).SetUint64(100 + quote)})
	if err != nil {
		t.Fatal(err)
	}
	evidence.Transfer = transfer
	f.appendOriginal(t, "precompile.transfer-out", 226)
	if residual {
		reseed := PrecompileRecoveryStep{Operation: "reseed-sample", QuoteHead: ChainHead{Number: 227, Hash: common.BigToHash(big.NewInt(1227)).Hex()}, QuoteSourceRao: 3, Seed: &PrecompileValueStep{TAORao: 3, ValueWei: "3000000000", BeforeRao: 3}}
		f.appendRecovery(t, reseed, 228, map[string]any{"amountArg": big.NewInt(3), "valueSent": big.NewInt(3_000_000_000), "stakeAfter": big.NewInt(1_000_000_003)})
		recovery := PrecompileRecoveryStep{Operation: "recover-sample", QuoteHead: ChainHead{Number: 229, Hash: common.BigToHash(big.NewInt(1229)).Hex()}, QuoteSourceRao: 1_000_000_003, QuoteDestinationRao: evidence.Transfer.ProviderAfterRao, Move: PrecompileMoveStep{AmountRao: 1_000_000_003, FromBeforeRao: 1_000_000_003, ToBeforeRao: evidence.Transfer.ProviderAfterRao}}
		f.appendRecovery(t, recovery, 230, map[string]any{"amount": big.NewInt(1_000_000_003), "sourceBefore": big.NewInt(1_000_000_003), "sourceAfter": big.NewInt(0), "destinationBefore": new(big.Int).SetUint64(recovery.QuoteDestinationRao), "destinationAfter": new(big.Int).SetUint64(recovery.QuoteDestinationRao + 1_000_000_003)})
	}
	evidence.Complete = true
	if err := writePrecompileEvidence(f.stateDir, evidence); err != nil {
		t.Fatal(err)
	}
	record := PrecompileRecoveryCompletionRecord{Schema: "urnetwork-precompile-recovery-complete-v1", PlanHash: f.plan.PlanHash, ConfigHash: evidence.ConfigHash, DeploymentId: f.plan.DeploymentID, AuthorizationHash: authority.Hash, OriginalEvidenceHash: f.original.EvidenceHash, EvidenceHash: evidence.EvidenceHash, JournalHash: f.entries[len(f.entries)-1].EntryHash, FinalizedHead: reader.finalized, Owner: common.HexToAddress(f.plan.Roles.Owner), Deployer: common.HexToAddress(f.plan.Roles.Deployer)}
	f.completion = &PrecompileRecoveryCompletion{Record: record}
	f.sign(t, record, &f.completion.Hash, &f.completion.OwnerSignature, &f.completion.DeployerSignature)
	return f
}
func (self *precompileRecoveryTestFixture) sign(t *testing.T, value any, hash, owner, deployer *string) {
	t.Helper()
	for _, name := range []string{coordinatorRepairOwnerRole, coordinatorRepairDeployerRole} {
		role, err := self.roles.EVMKey(name)
		if err != nil {
			t.Fatal(err)
		}
		key, err := crypto.HexToECDSA(role.PrivateKeyHex)
		if err != nil {
			t.Fatal(err)
		}
		h, sig, err := coordinatorRepairSignature(value, key)
		if err != nil {
			t.Fatal(err)
		}
		*hash = h
		if name == coordinatorRepairOwnerRole {
			*owner = sig
		} else {
			*deployer = sig
		}
	}
}
func (self *precompileRecoveryTestFixture) appendCall(t *testing.T, action Action, data []byte, value *big.Int, eventName string, indexed []common.Hash, values []any, block uint64) *types.Receipt {
	t.Helper()
	role, err := self.roles.EVMKey(coordinatorRepairDeployerRole)
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(role.PrivateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	nonce := self.plan.PrecompileProbeSuccessor.DeployerNonce + uint64(len(self.entries)-len(self.base.entries))
	probe := common.HexToAddress(self.evidence.ProbeAddress)
	transaction, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: nonce, To: &probe, Value: value, Data: data, Gas: 500000, GasPrice: big.NewInt(1)}), types.LatestSignerForChainID(new(big.Int).SetUint64(self.plan.ChainID)), key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := abi.JSON(strings.NewReader(SubnetProbeABI))
	if err != nil {
		t.Fatal(err)
	}
	event := parsed.Events[eventName]
	encoded, err := event.Inputs.NonIndexed().Pack(values...)
	if err != nil {
		t.Fatal(err)
	}
	blockHash := common.BigToHash(new(big.Int).SetUint64(block + 1000))
	receipt := &types.Receipt{TxHash: transaction.Hash(), BlockNumber: new(big.Int).SetUint64(block), BlockHash: blockHash, Status: types.ReceiptStatusSuccessful, Logs: []*types.Log{{Address: probe, Topics: append([]common.Hash{event.ID}, indexed...), Data: encoded}}}
	self.reader.blockHeads[block] = ChainHead{Number: block, Hash: blockHash.Hex()}
	self.reader.receipts[receipt.TxHash], self.reader.transactions[receipt.TxHash] = receipt, transaction
	sequence := self.entries[len(self.entries)-1].Sequence + 1
	self.entries = append(self.entries, JournalEntry{Sequence: sequence, DeploymentID: self.plan.DeploymentID, PlanHash: self.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: receipt.TxHash.Hex(), BlockNumber: block, BlockHash: blockHash.Hex(), EntryHash: common.BigToHash(new(big.Int).SetUint64(sequence + 2000)).Hex()})
	return receipt
}
func (self *precompileRecoveryTestFixture) appendOriginal(t *testing.T, id string, block uint64) {
	t.Helper()
	e := self.evidence
	data, value, _, err := precompileProbeSuccessorCall(self.plan, e, id)
	if err != nil {
		t.Fatal(err)
	}
	sample, move, recovery := common.HexToHash(e.SampleHotkey), common.HexToHash(e.MoveHotkey), common.HexToHash(e.RecoveryColdkey)
	var name string
	var topics []common.Hash
	var values []any
	switch id {
	case "precompile.seed":
		name, topics, values = "Seeded", []common.Hash{sample}, []any{new(big.Int).SetUint64(e.Seed.TAORao), value, new(big.Int).SetUint64(e.Seed.AfterRao)}
	case "precompile.move-forward", "precompile.move-back":
		step := e.Forward
		topics = []common.Hash{sample, move}
		if id == "precompile.move-back" {
			step = e.Back
			topics = []common.Hash{move, sample}
		}
		name = "MoveRoundTrip"
		values = []any{new(big.Int).SetUint64(step.AmountRao), new(big.Int).SetUint64(step.FromBeforeRao), new(big.Int).SetUint64(step.FromAfterRao), new(big.Int).SetUint64(step.ToBeforeRao), new(big.Int).SetUint64(step.ToAfterRao)}
	case "precompile.snapshot":
		name, topics, values = "DividendSnapshot", []common.Hash{sample}, []any{new(big.Int).SetUint64(e.Snapshot.BaselineRao), e.Snapshot.SinceBlock}
	case "precompile.transfer-out":
		name, topics, values = "TransferredOut", []common.Hash{recovery, sample}, []any{new(big.Int).SetUint64(e.Transfer.AmountRao), new(big.Int).SetUint64(e.Transfer.ProbeBeforeRao), new(big.Int).SetUint64(e.Transfer.ProbeAfterRao), new(big.Int).SetUint64(e.Transfer.ProviderBeforeRao), new(big.Int).SetUint64(e.Transfer.ProviderAfterRao)}
	}
	receipt := self.appendCall(t, actionByID(t, self.plan, id), data, value, name, topics, values, block)
	tx, n, h := receiptFields(receipt)
	switch id {
	case "precompile.seed":
		e.Seed.TransactionHash, e.Seed.BlockNumber, e.Seed.BlockHash = tx, n, h
	case "precompile.move-forward":
		e.Forward.TransactionHash, e.Forward.BlockNumber, e.Forward.BlockHash = tx, n, h
	case "precompile.move-back":
		e.Back.TransactionHash, e.Back.BlockNumber, e.Back.BlockHash = tx, n, h
	case "precompile.snapshot":
		e.Snapshot.TransactionHash, e.Snapshot.BlockNumber, e.Snapshot.BlockHash = tx, n, h
	case "precompile.transfer-out":
		e.Transfer.TransactionHash, e.Transfer.BlockNumber, e.Transfer.BlockHash = tx, n, h
	}
}
func (self *precompileRecoveryTestFixture) appendRecovery(t *testing.T, step PrecompileRecoveryStep, block uint64, values map[string]any) {
	t.Helper()
	e := self.evidence
	action, data, err := precompileRecoveryAction(&e.Recovery.Authorization, len(e.Recovery.Steps), step)
	if err != nil {
		t.Fatal(err)
	}
	step.Action = action
	observed, err := reconcilePrecompileRecoveryEvent(step, values)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	var topics []common.Hash
	var fields []string
	sample, move, recovery := common.HexToHash(e.SampleHotkey), common.HexToHash(e.MoveHotkey), common.HexToHash(e.RecoveryColdkey)
	switch step.Operation {
	case "top-up":
		name, topics, fields = "MoveRoundTrip", []common.Hash{sample, move}, []string{"amount", "fromBefore", "fromAfter", "toBefore", "toAfter"}
	case "recover-move", "recover-sample":
		hotkey := move
		if step.Operation == "recover-sample" {
			hotkey = sample
		}
		name, topics, fields = "TransferredOut", []common.Hash{recovery, hotkey}, []string{"amount", "sourceBefore", "sourceAfter", "destinationBefore", "destinationAfter"}
	case "reseed-sample":
		name, topics, fields = "Seeded", []common.Hash{sample}, []string{"amountArg", "valueSent", "stakeAfter"}
	}
	var ordered []any
	for _, field := range fields {
		ordered = append(ordered, values[field])
	}
	value, ok := new(big.Int).SetString(action.Parameters["recovery_value_wei"], 10)
	if !ok {
		t.Fatal("invalid value")
	}
	receipt := self.appendCall(t, action, data, value, name, topics, ordered, block)
	if observed.Seed != nil {
		observed.Seed.TransactionHash, observed.Seed.BlockNumber, observed.Seed.BlockHash = receiptFields(receipt)
	} else {
		observed.Move.TransactionHash, observed.Move.BlockNumber, observed.Move.BlockHash = receiptFields(receipt)
	}
	e.Recovery.Steps = append(e.Recovery.Steps, observed)
}

func TestPrecompileRecoveryClosesBothPositionsAndRetainsOriginalEvidence(t *testing.T) {
	f := newPrecompileRecoveryTestFixture(t)
	if err := verifyPrecompileRecoveryCompletion(f.plan, f.evidence, f.completion); err != nil {
		t.Fatal(err)
	}
	if err := verifyPrecompileRecoveryOriginalEvidence(f.original, f.evidence); err != nil {
		t.Fatal(err)
	}
	if !precompileEvidenceComplete(f.evidence) || precompileEvidenceComplete(f.original) {
		t.Fatal("final recovery was confused with original pending conformance")
	}
	nonce := f.plan.PrecompileProbeSuccessor.DeployerNonce + uint64(len(f.entries)-len(f.base.entries))
	if err := verifyPrecompileProbeSuccessorCalls(t.Context(), f.cfg, f.stateDir, f.plan, f.entries, f.reader, f.reader.finalized, nonce); err != nil {
		t.Fatal(err)
	}
}
func TestPrecompileRecoveryReseedsAndClearsQuoteToInclusionDust(t *testing.T) {
	f := precompileRecoveryTestFixtureWithSampleResidual(t, true)
	if f.evidence.Transfer.ProbeAfterRao != 3 || f.evidence.Recovery.SampleTransferInclusionCreditRao != 3 {
		t.Fatal("original successful transfer dust was rewritten")
	}
	if err := verifyPrecompileRecoveryCompletion(f.plan, f.evidence, f.completion); err != nil {
		t.Fatal(err)
	}
	nonce := f.plan.PrecompileProbeSuccessor.DeployerNonce + uint64(len(f.entries)-len(f.base.entries))
	if err := verifyPrecompileProbeSuccessorCalls(t.Context(), f.cfg, f.stateDir, f.plan, f.entries, f.reader, f.reader.finalized, nonce); err != nil {
		t.Fatal(err)
	}
}
func TestPrecompileRecoveryRejectsUnsignedAuthorityAndChangedAccounting(t *testing.T) {
	base := newPrecompileRecoveryTestFixture(t)
	raw, err := json.Marshal(base.evidence)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"signer", "limit", "original", "recipient", "runtime", "budget", "accrual", "missing step", "extra step", "dust", "signature", "seed mixing"} {
		f := *base
		var evidence PrecompileConformanceEvidence
		if err := json.Unmarshal(raw, &evidence); err != nil {
			t.Fatal(err)
		}
		f.evidence = &evidence
		completion := *base.completion
		f.completion = &completion
		switch fault {
		case "signer":
			f.evidence.Recovery.Authorization.DeployerSignature = f.evidence.Recovery.Authorization.OwnerSignature
		case "limit":
			f.evidence.Recovery.Authorization.Request.MaximumSteps++
		case "original":
			f.evidence.Back.ToAfterRao++
		case "recipient":
			f.evidence.Recovery.Authorization.Request.RecoveryColdkey = common.Hash{41}.Hex()
		case "runtime":
			f.evidence.Recovery.Authorization.Request.ProbeRuntimeHash = common.Hash{42}.Hex()
		case "budget":
			f.evidence.Recovery.Authorization.Request.Budget.MaximumRecoveryWei = "1"
		case "accrual":
			f.evidence.Recovery.Steps[0].SourceCreditRao++
		case "missing step":
			f.evidence.Recovery.Steps = f.evidence.Recovery.Steps[1:]
		case "extra step":
			f.evidence.Recovery.Steps = append(f.evidence.Recovery.Steps, f.evidence.Recovery.Steps[0])
		case "dust":
			f.evidence.Recovery.Steps[1].Move.FromAfterRao++
		case "signature":
			f.completion.OwnerSignature = f.completion.DeployerSignature
		case "seed mixing":
			f.evidence.Recovery.Steps[0].Seed = &PrecompileValueStep{TAORao: 3}
		}
		if err := verifyPrecompileRecoveryCompletion(f.plan, f.evidence, f.completion); err == nil {
			t.Fatalf("accepted %s", fault)
		}
	}
}
func TestPrecompileRecoveryRetainsPendingStepAtReceiptCrashBoundary(t *testing.T) {
	f := newPrecompileRecoveryTestFixture(t)
	last := f.evidence.Recovery.Steps[1]
	f.evidence.Recovery.Steps = f.evidence.Recovery.Steps[:2]
	f.evidence.Transfer = PrecompileTransferStep{}
	f.evidence.Recovery.Steps[1].Move = PrecompileMoveStep{AmountRao: last.Move.AmountRao, FromBeforeRao: last.QuoteSourceRao, ToBeforeRao: last.QuoteDestinationRao}
	f.evidence.Recovery.Steps[1].SourceCreditRao, f.evidence.Recovery.Steps[1].DestinationCreditRao = 0, 0
	f.evidence.Complete = false
	if err := writePrecompileEvidence(f.stateDir, f.evidence); err != nil {
		t.Fatal(err)
	}
	prefix := f.entries[:len(f.entries)-1]
	nonce := f.plan.PrecompileProbeSuccessor.DeployerNonce + uint64(len(prefix)-len(f.base.entries))
	if err := verifyPrecompileProbeSuccessorCalls(t.Context(), f.cfg, f.stateDir, f.plan, prefix, f.reader, f.reader.finalized, nonce); err != nil {
		t.Fatal(err)
	}
	_, _, settled, err := precompileRecoveryPositions(f.evidence)
	if err != nil || settled {
		t.Fatalf("crash receipt was mistaken for completed evidence: %v", err)
	}
	f.evidence.Recovery.Steps[1] = last
	if err := writePrecompileEvidence(f.stateDir, f.evidence); err != nil {
		t.Fatal(err)
	}
	_, move, settled, err := precompileRecoveryPositions(f.evidence)
	if err != nil || !settled || move != 0 {
		t.Fatalf("receipt replay did not preserve completed value: %v", err)
	}
}
func TestPrecompileRecoveryExactSavedTransactionResumesWithoutNonceRead(t *testing.T) {
	f := newPrecompileRecoveryTestFixture(t)
	step := f.evidence.Recovery.Steps[0]
	tx := f.reader.transactions[common.HexToHash(step.Move.TransactionHash)]
	journal, err := OpenJournal(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	raw, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.stateDir, "transactions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.stateDir, "transactions", stringsTrim0x(tx.Hash().Hex())+".rlp"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(JournalEntry{DeploymentID: f.plan.DeploymentID, PlanHash: f.plan.PlanHash, ActionID: step.Action.ID, IntentHash: step.Action.IntentHash, Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(JournalEntry{DeploymentID: f.plan.DeploymentID, PlanHash: f.plan.PlanHash, ActionID: step.Action.ID, IntentHash: step.Action.IntentHash, Stage: StageBroadcast, Signer: f.plan.Roles.Deployer, Nonce: strconv.FormatUint(tx.Nonce(), 10), TransactionHash: tx.Hash().Hex(), RecoveryBlock: step.QuoteHead.Number, RecoveryBlockHash: step.QuoteHead.Hash}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err = OpenJournal(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	manager := &EvmTxManager{stateDir: f.stateDir, journal: journal, chainID: new(big.Int).SetUint64(f.plan.ChainID), deploymentID: f.plan.DeploymentID}
	_, data, err := precompileRecoveryAction(&f.evidence.Recovery.Authorization, 0, step)
	if err != nil {
		t.Fatal(err)
	}
	probe := common.HexToAddress(f.evidence.ProbeAddress)
	resumed, err := manager.prepareOwnedEVMTransaction(t.Context(), f.plan.PlanHash, step.Action, &probe, new(big.Int), data, 0)
	if err != nil || resumed.Hash() != tx.Hash() {
		t.Fatalf("exact durable transaction did not resume without client or new nonce: %v", err)
	}
	changed := step.Action
	changed.Parameters = map[string]string{}
	for key, value := range step.Action.Parameters {
		changed.Parameters[key] = value
	}
	changed.Parameters["recovery_value_wei"] = "1"
	if _, err := manager.prepareOwnedEVMTransaction(t.Context(), f.plan.PlanHash, changed, &probe, new(big.Int), data, 0); err == nil {
		t.Fatal("saved transaction bypassed changed value authority")
	}
}
func TestPrecompileRecoveryOriginalEvidenceAndLegacyEncodingRemainImmutable(t *testing.T) {
	f := newPrecompileRecoveryTestFixture(t)
	before, err := json.Marshal(f.original)
	if err != nil {
		t.Fatal(err)
	}
	f.evidence.Dividend.CurrentRao++
	if err := verifyPrecompileRecoveryOriginalEvidence(f.original, f.evidence); err == nil {
		t.Fatal("changed retained dividend passed composition")
	}
	after, err := json.Marshal(f.original)
	if err != nil || !reflect.DeepEqual(before, after) || strings.Contains(string(before), "\"recovery\"") {
		t.Fatalf("original evidence was mutated or recoded: %v", err)
	}
}

func TestPrecompileRecoveryBindsQuotesBeforeDurableReplay(t *testing.T) {
	f := newPrecompileRecoveryTestFixture(t)
	step := f.evidence.Recovery.Steps[0]
	changed := step
	changed.QuoteSourceRao--
	action, _, err := precompileRecoveryAction(&f.evidence.Recovery.Authorization, 0, changed)
	if err != nil {
		t.Fatal(err)
	}
	if action.IntentHash == step.Action.IntentHash {
		t.Fatal("changed quote did not change the durable action intent")
	}
	changed.Action = action
	f.evidence.Recovery.Steps[0] = changed
	var entry JournalEntry
	for _, record := range f.entries {
		if record.ActionID == step.Action.ID {
			entry = record
		}
	}
	if err := verifyPrecompileRecoveryCall(t.Context(), f.reader, f.reader.finalized, f.plan, f.evidence, entry, 0, false); err == nil {
		t.Fatal("changed quote bypassed the exact original journal intent")
	}
}
func TestPrecompileRecoveryReplayEnforcesGasAndFeeBounds(t *testing.T) {
	for _, gasFault := range []bool{true, false} {
		f := newPrecompileRecoveryTestFixture(t)
		step := &f.evidence.Recovery.Steps[0]
		old := f.reader.transactions[common.HexToHash(step.Move.TransactionHash)]
		gas, fee := old.Gas(), old.GasFeeCap()
		if gasFault {
			gas = f.evidence.Recovery.Authorization.Request.MaximumGasUnits + 1
		} else {
			fee = new(big.Int).SetUint64(f.evidence.Recovery.Authorization.Request.MaximumFeePerGasWei + 1)
		}
		role, err := f.roles.EVMKey(coordinatorRepairDeployerRole)
		if err != nil {
			t.Fatal(err)
		}
		key, err := crypto.HexToECDSA(role.PrivateKeyHex)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: old.Nonce(), To: old.To(), Value: old.Value(), Gas: gas, GasPrice: fee, Data: old.Data()}), types.LatestSignerForChainID(old.ChainId()), key)
		if err != nil {
			t.Fatal(err)
		}
		receipt := *f.reader.receipts[old.Hash()]
		receipt.TxHash = tx.Hash()
		f.reader.receipts[tx.Hash()], f.reader.transactions[tx.Hash()] = &receipt, tx
		step.Move.TransactionHash = tx.Hash().Hex()
		entry := JournalEntry{ActionID: step.Action.ID, IntentHash: step.Action.IntentHash, TransactionHash: tx.Hash().Hex(), BlockNumber: step.Move.BlockNumber, BlockHash: step.Move.BlockHash}
		if err := verifyPrecompileRecoveryCall(t.Context(), f.reader, f.reader.finalized, f.plan, f.evidence, entry, old.Nonce(), false); err == nil || !strings.Contains(err.Error(), "gas or fee cap") {
			t.Fatalf("replay failed to enforce gasFault=%t: %v", gasFault, err)
		}
	}
}
func TestPrecompileRecoveryTurnPropagatesBothPhaseFailures(t *testing.T) {
	f := newPrecompileRecoveryTestFixture(t)
	failure := errors.New("synthetic transport failure")
	for _, dividend := range []bool{true, false} {
		evidence := *f.original
		if dividend {
			evidence.Dividend = PrecompileDividendStep{}
		}
		calls := 0
		execute := func(_ context.Context, action Action) error {
			calls++
			want := "precompile.transfer-out"
			if dividend {
				want = "precompile.dividend"
			}
			if action.ID != want {
				t.Fatalf("executed %s, want %s", action.ID, want)
			}
			return failure
		}
		if err := precompileRecoveryTurn(t.Context(), f.plan, &evidence, execute, execute); !errors.Is(err, failure) || calls != 1 {
			t.Fatalf("phase error was lost: dividend=%t calls=%d err=%v", dividend, calls, err)
		}
	}
}
func TestPrecompileRecoveryRequiresExplicitExecutionAndReviewedBudget(t *testing.T) {
	commonOptions := cliOptions{ProvisionalResume: true, PlanHash: common.Hash{51}.Hex()}
	if err := validatePrecompileRecoveryOptions("probe-recovery", commonOptions); err != nil {
		t.Fatalf("read-only proposal refused: %v", err)
	}
	execute := commonOptions
	execute.Apply, execute.ProbeRecoveryExecute = true, true
	if err := validatePrecompileRecoveryOptions("probe-recovery", execute); err != nil {
		t.Fatalf("exact authorized execution refused: %v", err)
	}
	for _, fault := range []string{"missing apply", "foreign command", "inline new budget", "unreviewed publication", "detach", "prepare"} {
		changed := execute
		command := "probe-recovery"
		switch fault {
		case "missing apply":
			changed.Apply = false
		case "foreign command":
			command = "resume"
		case "inline new budget":
			changed.ProbeRecoveryBudget = "/synthetic/budget.json"
		case "unreviewed publication":
			changed.ProbeRecoveryExecute = false
		case "detach":
			changed.Detach = true
		case "prepare":
			changed.PrepareOnly = true
		}
		if err := validatePrecompileRecoveryOptions(command, changed); err == nil {
			t.Fatalf("accepted %s", fault)
		}
	}
}
func TestPrecompileRecoveryProposalRetainsSeparateGasAndReseedBounds(t *testing.T) {
	f := newPrecompileRecoveryTestFixture(t)
	budget, err := newPrecompileRecoveryBudget(f.cfg, f.stateDir, f.plan, f.entries)
	if err != nil {
		t.Fatal(err)
	}
	if budget.MaximumRecoveryWei != "400000006000000000" || budget.MaximumReseedWei != "6000000000" || budget.PlanHash != f.plan.PlanHash || !budget.Verified {
		t.Fatalf("incorrect explicit budget: %+v", budget)
	}
}
func TestPrecompileRecoveryCannotRetireSupplementalEvidenceAsUnfunded(t *testing.T) {
	f := newPrecompileProbeRetirementFixture(t)
	f.evidence.Recovery = &PrecompileRecoveryEvidence{}
	if err := writePrecompileEvidence(f.stateDir, f.evidence); err != nil {
		t.Fatal(err)
	}
	if err := validatePrecompileProbeSeedFailureEvidence(f.plan, f.plan.PrecompileProbeSuccessor, f.evidence); err == nil {
		t.Fatal("supplemental custody evidence was discarded by unfunded retirement")
	}
}
