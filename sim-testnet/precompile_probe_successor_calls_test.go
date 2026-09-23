// Synthetic signed calls reproduce successor restarts after any finalized phase.
package main

import (
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Produces canonical read-only receipts for CREATE and all five probe calls.
func precompileProbeSuccessorCallFixture(t *testing.T, fixture *precompileProbeSuccessorFixture) (*PrecompileConformanceEvidence, []JournalEntry, *deploymentBoundaryFixture) {
	t.Helper()
	return precompileProbeSuccessorCallFixtureWithMoveResidue(t, fixture, 0)
}

// The optional remainder keeps actual transfers fixed while changing the exact
// signed forward request and all connected balances by synthetic units.
func precompileProbeSuccessorCallFixtureWithMoveResidue(t *testing.T, fixture *precompileProbeSuccessorFixture, residue uint64) (*PrecompileConformanceEvidence, []JournalEntry, *deploymentBoundaryFixture) {
	t.Helper()
	plan := fixture.plan
	plan.LiveFacts.ProbeTAORao = 3
	evidence := plan.PrecompileProbeSuccessor.Evidence
	evidence.ProbeAddress = plan.PrecompileProbeSuccessor.Probe
	coldkey := ss58Mirror(common.HexToAddress(evidence.ProbeAddress))
	evidence.ProbeColdkey = hexBytesValue(coldkey[:])
	evidence.CommitmentSource = precompileProbeCommitmentSource(plan.PrecompileProbeSuccessor)
	evidence.Seed = PrecompileValueStep{TAORao: 3, ValueWei: "3000000000", AfterRao: 100, DeltaRao: 100}
	evidence.Forward = PrecompileMoveStep{AmountRao: 50, FromBeforeRao: 100, FromAfterRao: 50, ToBeforeRao: 0, ToAfterRao: 50}
	evidence.Back = PrecompileMoveStep{AmountRao: 50, FromBeforeRao: 50, FromAfterRao: 0, ToBeforeRao: 50, ToAfterRao: 100}
	evidence.Snapshot = PrecompileSnapshotStep{BaselineRao: 100, SinceBlock: 214}
	evidence.Transfer = PrecompileTransferStep{AmountRao: 110, ProbeBeforeRao: 110, ProbeAfterRao: 0, ProviderBeforeRao: 20, ProviderAfterRao: 130}
	evidence.Complete = true
	evidence.Seed.AfterRao += 2 * residue
	evidence.Seed.DeltaRao += 2 * residue
	evidence.Forward.AmountRao += residue
	evidence.Forward.FromBeforeRao += 2 * residue
	evidence.Forward.FromAfterRao += 2 * residue
	evidence.Forward.NativeShareResidueRao = residue
	evidence.Back.ToBeforeRao += 2 * residue
	evidence.Back.ToAfterRao += 2 * residue
	evidence.Snapshot.BaselineRao += 2 * residue
	evidence.Transfer.AmountRao += 2 * residue
	evidence.Transfer.ProbeBeforeRao += 2 * residue
	evidence.Transfer.ProviderAfterRao += 2 * residue
	roles, err := BuildRoleSecrets(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	role, err := roles.EVMKey("deployer")
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(role.PrivateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := abi.JSON(strings.NewReader(SubnetProbeABI))
	if err != nil {
		t.Fatal(err)
	}
	reader := &deploymentBoundaryFixture{finalized: ChainHead{Number: 300, Hash: common.Hash{90}.Hex()}, blockHeads: map[uint64]ChainHead{}, receipts: map[common.Hash]*types.Receipt{}, transactions: map[common.Hash]*types.Transaction{}}
	reader.blockHeads[reader.finalized.Number] = reader.finalized
	entries := slices.Clone(fixture.entries)
	for index, actionId := range precompileProbeSuccessorTransactionIds() {
		action := actionByID(t, plan, actionId)
		probe := common.HexToAddress(evidence.ProbeAddress)
		to, data, value := &probe, fixture.payloads.PrecompileProbe, new(big.Int)
		if index == 0 {
			to = nil
		} else {
			data, value, _, err = precompileProbeSuccessorCall(plan, &evidence, actionId)
			if err != nil {
				t.Fatal(err)
			}
		}
		transaction, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: plan.PrecompileProbeSuccessor.DeployerNonce + uint64(index), To: to, Value: value, Data: data, Gas: 500000, GasPrice: big.NewInt(1)}), types.LatestSignerForChainID(new(big.Int).SetUint64(plan.ChainID)), key)
		if err != nil {
			t.Fatal(err)
		}
		block, blockHash := uint64(210+index), common.Hash{byte(80 + index)}
		receipt := &types.Receipt{TxHash: transaction.Hash(), BlockNumber: new(big.Int).SetUint64(block), BlockHash: blockHash, Status: types.ReceiptStatusSuccessful}
		if index == 0 {
			receipt.ContractAddress = probe
		} else {
			var name string
			var indexed []common.Hash
			var values []any
			sample, move, recovery := common.HexToHash(evidence.SampleHotkey), common.HexToHash(evidence.MoveHotkey), common.HexToHash(evidence.RecoveryColdkey)
			switch actionId {
			case "precompile.seed":
				name, indexed, values = "Seeded", []common.Hash{sample}, []any{big.NewInt(3), big.NewInt(3000000000), new(big.Int).SetUint64(evidence.Seed.AfterRao)}
				evidence.Seed.TransactionHash, evidence.Seed.BlockNumber, evidence.Seed.BlockHash = receiptFields(receipt)
			case "precompile.move-forward":
				name, indexed, values = "MoveRoundTrip", []common.Hash{sample, move}, []any{new(big.Int).SetUint64(evidence.Forward.AmountRao), new(big.Int).SetUint64(evidence.Forward.FromBeforeRao), new(big.Int).SetUint64(evidence.Forward.FromAfterRao), new(big.Int).SetUint64(evidence.Forward.ToBeforeRao), new(big.Int).SetUint64(evidence.Forward.ToAfterRao)}
				evidence.Forward.TransactionHash, evidence.Forward.BlockNumber, evidence.Forward.BlockHash = receiptFields(receipt)
			case "precompile.move-back":
				name, indexed, values = "MoveRoundTrip", []common.Hash{move, sample}, []any{new(big.Int).SetUint64(evidence.Back.AmountRao), new(big.Int).SetUint64(evidence.Back.FromBeforeRao), new(big.Int).SetUint64(evidence.Back.FromAfterRao), new(big.Int).SetUint64(evidence.Back.ToBeforeRao), new(big.Int).SetUint64(evidence.Back.ToAfterRao)}
				evidence.Back.TransactionHash, evidence.Back.BlockNumber, evidence.Back.BlockHash = receiptFields(receipt)
			case "precompile.snapshot":
				name, indexed, values = "DividendSnapshot", []common.Hash{sample}, []any{new(big.Int).SetUint64(evidence.Snapshot.BaselineRao), uint64(214)}
				evidence.Snapshot.TransactionHash, evidence.Snapshot.BlockNumber, evidence.Snapshot.BlockHash = receiptFields(receipt)
			case "precompile.transfer-out":
				name, indexed, values = "TransferredOut", []common.Hash{recovery, sample}, []any{new(big.Int).SetUint64(evidence.Transfer.AmountRao), new(big.Int).SetUint64(evidence.Transfer.ProbeBeforeRao), new(big.Int).SetUint64(evidence.Transfer.ProbeAfterRao), new(big.Int).SetUint64(evidence.Transfer.ProviderBeforeRao), new(big.Int).SetUint64(evidence.Transfer.ProviderAfterRao)}
				evidence.Transfer.TransactionHash, evidence.Transfer.BlockNumber, evidence.Transfer.BlockHash = receiptFields(receipt)
			}
			event := parsed.Events[name]
			encoded, err := event.Inputs.NonIndexed().Pack(values...)
			if err != nil {
				t.Fatal(err)
			}
			receipt.Logs = []*types.Log{{Address: probe, Topics: append([]common.Hash{event.ID}, indexed...), Data: encoded}}
		}
		reader.blockHeads[block] = ChainHead{Number: block, Hash: blockHash.Hex()}
		reader.receipts[receipt.TxHash], reader.transactions[receipt.TxHash] = receipt, transaction
		entries = append(entries, JournalEntry{Sequence: plan.PrecompileProbeSuccessor.JournalSequence + 1 + uint64(index), DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: receipt.TxHash.Hex(), BlockNumber: block, BlockHash: blockHash.Hex()})
	}
	if err := writePrecompileEvidence(fixture.stateDir, &evidence); err != nil {
		t.Fatal(err)
	}
	return &evidence, entries, reader
}

// Seed and full conformance both survive the real nonce admission and reducer.
func TestPrecompileProbeSuccessorRerendersAfterValuePhases(t *testing.T) {
	for _, consumed := range []int{2, 6} {
		fixture := newPrecompileProbeSuccessorFixture(t)
		evidence, entries, reader := precompileProbeSuccessorCallFixture(t, fixture)
		truncatePrecompileProbeSuccessorEvidence(evidence, consumed)
		if err := writePrecompileEvidence(fixture.stateDir, evidence); err != nil {
			t.Fatal(err)
		}
		prefix := entries[:len(fixture.entries)+consumed]
		nonce := fixture.plan.PrecompileProbeSuccessor.DeployerNonce + uint64(consumed)
		if ok, err := precompileProbeSuccessorNonce(fixture.plan, prefix, nonce); err != nil || !ok {
			t.Fatalf("completed value phase lost successor nonce admission: %v", err)
		}
		if err := verifyPrecompileProbeSuccessorCalls(t.Context(), fixture.cfg, fixture.stateDir, fixture.plan, prefix, reader, reader.finalized, nonce); err != nil {
			t.Fatalf("completed value phase lost exact finalized call proof: %v", err)
		}
		observed := &coordinatorRepairCarryObservation{reference: *fixture.plan.CoordinatorRepairCarry, deployerNonce: nonce}
		fixture.plan.coordinatorRepairObserved = observed
		migration := &coordinatorUpgradeMigration{Deployment: fixture.plan.Deployment, Baseline: fixture.plan.CoordinatorUpgradeBaseline, Upgrade: fixture.plan.CoordinatorUpgrade, Repair: observed, ProbeSuccessor: fixture.plan.PrecompileProbeSuccessor}
		if ok, err := coordinatorUpgradeMigrationNonceMatches(fixture.plan, migration, fixture.payloads, nonce, prefix); err != nil || !ok {
			t.Fatalf("completed value phase lost rerender observation: %v", err)
		}
	}
}

// Models only phases completed by the selected transaction prefix.
func truncatePrecompileProbeSuccessorEvidence(evidence *PrecompileConformanceEvidence, consumed int) {
	if consumed < 6 {
		evidence.Transfer = PrecompileTransferStep{}
		evidence.Dividend = PrecompileDividendStep{}
		evidence.Complete = false
	}
	if consumed < 5 {
		evidence.Snapshot = PrecompileSnapshotStep{}
	}
	if consumed < 4 {
		evidence.Back = PrecompileMoveStep{}
	}
	if consumed < 3 {
		evidence.Forward = PrecompileMoveStep{}
	}
}

// Only a contiguous exact prefix can explain the observed account nonce.
func TestPrecompileProbeSuccessorRejectsCallPrefixDrift(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	_, original, _ := precompileProbeSuccessorCallFixture(t, fixture)
	nonce := fixture.plan.PrecompileProbeSuccessor.DeployerNonce + 6
	for _, change := range []string{"missing", "duplicate", "order", "intent", "plan", "deployment", "unrelated", "not finalized", "checkpoint", "extra nonce"} {
		entries, observed := slices.Clone(original), nonce
		index := len(fixture.entries) + 2
		switch change {
		case "missing":
			entries = append(entries[:index], entries[index+1:]...)
		case "duplicate":
			copy := entries[index]
			copy.TransactionHash = common.Hash{99}.Hex()
			entries = append(entries, copy)
		case "order":
			entries[index].Sequence = entries[index-1].Sequence
		case "intent":
			entries[index].IntentHash = common.Hash{99}.Hex()
		case "plan":
			entries[index].PlanHash = common.Hash{99}.Hex()
		case "deployment":
			entries[index].DeploymentID = "synthetic-unapproved-deployment"
		case "unrelated":
			entries[index].ActionID = "unrelated.call"
		case "not finalized":
			entries[index].Stage = StageBroadcast
		case "checkpoint":
			entries[index].BlockHash = ""
		case "extra nonce":
			observed++
		}
		if ok, err := precompileProbeSuccessorNonce(fixture.plan, entries, observed); err == nil || ok {
			t.Fatalf("%s call prefix was accepted", change)
		}
	}
}

// Value-call recovery can journal the same receipt again without spending again.
func TestPrecompileProbeSuccessorAcceptsExactRefinalization(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	_, entries, reader := precompileProbeSuccessorCallFixture(t, fixture)
	for _, consumed := range []int{2, 6} {
		prefix := slices.Clone(entries[:len(fixture.entries)+consumed])
		first := prefix[len(prefix)-1]
		repeated := first
		repeated.Sequence++
		prefix = append(prefix, repeated)
		nonce := fixture.plan.PrecompileProbeSuccessor.DeployerNonce + uint64(consumed)
		admitted, err := precompileProbeSuccessorPrefix(fixture.plan, prefix, nonce)
		if err != nil || len(admitted) != consumed || admitted[len(admitted)-1].Sequence != first.Sequence {
			t.Fatalf("exact refinalization lost its first ordered receipt: %v", err)
		}
		if err := verifyPrecompileProbeSuccessorCalls(t.Context(), fixture.cfg, fixture.stateDir, fixture.plan, prefix, reader, reader.finalized, nonce); err != nil {
			t.Fatalf("exact refinalization lost canonical call proof: %v", err)
		}
		for _, change := range []string{"transaction", "block", "block hash", "recovery", "recovery hash", "intent", "plan"} {
			changed := slices.Clone(prefix)
			last := &changed[len(changed)-1]
			switch change {
			case "transaction":
				last.TransactionHash = common.Hash{99}.Hex()
			case "block":
				last.BlockNumber++
			case "block hash":
				last.BlockHash = common.Hash{99}.Hex()
			case "recovery":
				last.RecoveryBlock++
			case "recovery hash":
				last.RecoveryBlockHash = common.Hash{99}.Hex()
			case "intent":
				last.IntentHash = common.Hash{99}.Hex()
			case "plan":
				last.PlanHash = common.Hash{99}.Hex()
			}
			if _, err := precompileProbeSuccessorPrefix(fixture.plan, changed, nonce); err == nil {
				t.Fatalf("changed %s refinalization was accepted", change)
			}
		}
	}
	create := slices.Clone(entries[:len(fixture.entries)+1])
	create = append(create, create[len(create)-1])
	if _, err := precompileProbeSuccessorPrefix(fixture.plan, create, fixture.plan.PrecompileProbeSuccessor.DeployerNonce+1); err == nil {
		t.Fatal("CREATE duplicate guard was weakened")
	}
}

// Signed transaction, receipt and event substitutions fail even with a valid prefix.
func TestPrecompileProbeSuccessorRejectsChangedCallProof(t *testing.T) {
	for _, change := range []string{"signer", "nonce", "target", "calldata", "value", "chain", "pending", "failed receipt", "receipt hash", "canonical block", "evidence checkpoint", "event", "event address", "native source", "role", "dynamic amount"} {
		fixture := newPrecompileProbeSuccessorFixture(t)
		evidence, entries, reader := precompileProbeSuccessorCallFixture(t, fixture)
		entry := &entries[len(fixture.entries)+1]
		hash := common.HexToHash(entry.TransactionHash)
		transaction := reader.transactions[hash]
		if slices.Contains([]string{"signer", "nonce", "target", "calldata", "value", "chain"}, change) {
			roles, err := BuildRoleSecrets(fixture.cfg)
			if err != nil {
				t.Fatal(err)
			}
			label := "deployer"
			if change == "signer" {
				label = "testnet-owner"
			}
			role, err := roles.EVMKey(label)
			if err != nil {
				t.Fatal(err)
			}
			key, err := crypto.HexToECDSA(role.PrivateKeyHex)
			if err != nil {
				t.Fatal(err)
			}
			call := &types.LegacyTx{Nonce: transaction.Nonce(), To: transaction.To(), Value: transaction.Value(), Data: slices.Clone(transaction.Data()), Gas: transaction.Gas(), GasPrice: transaction.GasPrice()}
			chainId := new(big.Int).SetUint64(fixture.plan.ChainID)
			switch change {
			case "nonce":
				call.Nonce++
			case "target":
				target := common.Address{99}
				call.To = &target
			case "calldata":
				call.Data[len(call.Data)-1] ^= 1
			case "value":
				call.Value = new(big.Int).Add(call.Value, big.NewInt(1))
			case "chain":
				chainId.Add(chainId, big.NewInt(1))
			}
			changed, err := types.SignTx(types.NewTx(call), types.LatestSignerForChainID(chainId), key)
			if err != nil {
				t.Fatal(err)
			}
			receipt := *reader.receipts[hash]
			receipt.TxHash = changed.Hash()
			reader.receipts[changed.Hash()], reader.transactions[changed.Hash()] = &receipt, changed
			entry.TransactionHash, evidence.Seed.TransactionHash = changed.Hash().Hex(), changed.Hash().Hex()
		} else {
			switch change {
			case "pending":
				reader.pending = true
			case "failed receipt":
				reader.receipts[hash].Status = types.ReceiptStatusFailed
			case "receipt hash":
				reader.receipts[hash].TxHash = common.Hash{99}
			case "canonical block":
				reader.blockHeads[entry.BlockNumber] = ChainHead{Number: entry.BlockNumber, Hash: common.Hash{99}.Hex()}
			case "evidence checkpoint":
				evidence.Seed.BlockNumber++
			case "event":
				reader.receipts[hash].Logs[0].Data[len(reader.receipts[hash].Logs[0].Data)-1] ^= 1
			case "event address":
				reader.receipts[hash].Logs[0].Address = common.Address{99}
			case "native source":
				evidence.CommitmentSource.PlanHash = common.Hash{99}.Hex()
			case "role":
				evidence.RecoveryColdkey = common.Hash{99}.Hex()
			case "dynamic amount":
				evidence.Forward.AmountRao++
			}
		}
		if err := writePrecompileEvidence(fixture.stateDir, evidence); err != nil {
			t.Fatal(err)
		}
		if err := verifyPrecompileProbeSuccessorCalls(t.Context(), fixture.cfg, fixture.stateDir, fixture.plan, entries, reader, reader.finalized, fixture.plan.PrecompileProbeSuccessor.DeployerNonce+6); err == nil {
			t.Fatalf("%s successor call proof was accepted", change)
		}
	}
}

// The last finalized call can resume before its evidence write, using exact input.
func TestPrecompileProbeSuccessorResumesFinalizedCallBeforeEvidenceWrite(t *testing.T) {
	for _, consumed := range []int{2, 3, 4, 5, 6} {
		fixture := newPrecompileProbeSuccessorFixture(t)
		evidence, entries, reader := precompileProbeSuccessorCallFixture(t, fixture)
		truncatePrecompileProbeSuccessorEvidence(evidence, consumed)
		prefix := entries[:len(fixture.entries)+consumed]
		entry := prefix[len(prefix)-1]
		switch entry.ActionID {
		case "precompile.seed":
			evidence.Seed.TransactionHash, evidence.Seed.BlockNumber, evidence.Seed.BlockHash = "", 0, ""
			evidence.Seed.AfterRao, evidence.Seed.DeltaRao = 0, 0
		case "precompile.move-forward":
			evidence.Forward.TransactionHash, evidence.Forward.BlockNumber, evidence.Forward.BlockHash = "", 0, ""
			evidence.Forward.FromAfterRao, evidence.Forward.ToAfterRao = 0, 0
		case "precompile.move-back":
			evidence.Back.TransactionHash, evidence.Back.BlockNumber, evidence.Back.BlockHash = "", 0, ""
			evidence.Back.FromAfterRao, evidence.Back.ToAfterRao = 0, 0
		case "precompile.snapshot":
			evidence.Snapshot.TransactionHash, evidence.Snapshot.BlockNumber, evidence.Snapshot.BlockHash = "", 0, ""
			evidence.Snapshot.SinceBlock = 0
		case "precompile.transfer-out":
			evidence.Transfer.TransactionHash, evidence.Transfer.BlockNumber, evidence.Transfer.BlockHash = "", 0, ""
			evidence.Transfer.ProbeAfterRao, evidence.Transfer.ProviderAfterRao, evidence.Complete = 0, 0, false
		}
		if err := writePrecompileEvidence(fixture.stateDir, evidence); err != nil {
			t.Fatal(err)
		}
		nonce := fixture.plan.PrecompileProbeSuccessor.DeployerNonce + uint64(consumed)
		if err := verifyPrecompileProbeSuccessorCalls(t.Context(), fixture.cfg, fixture.stateDir, fixture.plan, prefix, reader, reader.finalized, nonce); err != nil {
			t.Fatalf("finalized %s lost recoverable input: %v", entry.ActionID, err)
		}
		if err := verifyPrecompileProbeSuccessorCall(t.Context(), reader, reader.finalized, fixture.plan, evidence, entry, nonce-1, false); err == nil {
			t.Fatalf("completed %s silently lost its evidence receipt", entry.ActionID)
		}
	}
}
