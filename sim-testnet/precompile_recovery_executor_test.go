// A process restart must reconcile its durable pending call before requiring
// finalized nonce continuity. Final proof still requires that complete prefix.
package main

import (
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// The persisted crash has one quoted step and signed bytes, with no receipt or
// later action in either the evidence or real journal. Native roles are public
// fixture identities; this executor never signs a native transaction.
func newPendingPrecompileRecoveryExecutorFixture(t *testing.T) (*precompileRecoveryTestFixture, *Journal, *types.Transaction) {
	t.Helper()
	f := newPrecompileRecoveryTestFixture(t)
	f.cfg.Public.Chain.SubstratePublicReadEndpoint = "wss://verification-rpc.example"
	f.cfg.Public.Chain.EVMPublicReadEndpoint = "https://verification-rpc.example"
	f.reader.codes = map[common.Address][]byte{approvedPrecompileProbe(f.plan): f.base.payloads.ExpectedRuntime[approvedPrecompileProbe(f.plan)]}
	step := f.evidence.Recovery.Steps[0]
	tx := f.reader.transactions[common.HexToHash(step.Move.TransactionHash)]
	f.evidence.Recovery.Steps = nil
	f.evidence.Recovery.SampleTransferQuoteHead = ChainHead{}
	f.evidence.Recovery.SampleTransferSourceQuoteRao = 0
	f.evidence.Recovery.SampleTransferDestinationQuoteRao = 0
	f.evidence.Recovery.SampleTransferCreditRao = 0
	f.evidence.Recovery.SampleTransferInclusionCreditRao = 0
	f.evidence.Recovery.SampleTransferDestinationCreditRao = 0
	f.evidence.Transfer = PrecompileTransferStep{}
	f.evidence.Complete = false
	journal, err := OpenJournal(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(JournalEntry{DeploymentID: f.plan.DeploymentID, PlanHash: f.plan.PlanHash, ActionID: "precompile.snapshot", IntentHash: actionByID(t, f.plan, "precompile.snapshot").IntentHash, Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	authority := &f.evidence.Recovery.Authorization
	authority.Request.Budget.JournalHash = journal.Entries()[0].EntryHash
	authority.Request.BudgetHash, err = canonicalHashHex(authority.Request.Budget)
	if err != nil {
		t.Fatal(err)
	}
	f.sign(t, authority.Request, &authority.Hash, &authority.OwnerSignature, &authority.DeployerSignature)
	step.Move = PrecompileMoveStep{AmountRao: step.Move.AmountRao, FromBeforeRao: step.QuoteSourceRao, ToBeforeRao: step.QuoteDestinationRao}
	step.SourceCreditRao, step.DestinationCreditRao = 0, 0
	step.Action, _, err = precompileRecoveryAction(authority, 0, step)
	if err != nil {
		t.Fatal(err)
	}
	f.evidence.Recovery.Steps = []PrecompileRecoveryStep{step}
	if err := writePrecompileEvidence(f.stateDir, f.evidence); err != nil {
		t.Fatal(err)
	}
	for label, key := range map[string]string{validatorHotkeyLabel(1): f.evidence.SampleHotkey, validatorHotkeyLabel(2): f.evidence.MoveHotkey, fleetColdkeyLabel(1): f.evidence.RecoveryColdkey} {
		role := f.roles.Substrate[label]
		role.PublicKeyHex = stringsTrim0x(key)
		f.roles.Substrate[label] = role
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(f.stateDir, "transactions", stringsTrim0x(tx.Hash().Hex())+".rlp"), raw, 0o600); err != nil {
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
	t.Cleanup(func() { _ = journal.Close() })
	return f, journal, tx
}

func TestPrecompileRecoveryScopedExecutorReconcilesSignedUnfinalizedCrash(t *testing.T) {
	f, journal, tx := newPendingPrecompileRecoveryExecutorFixture(t)
	step := f.evidence.Recovery.Steps[0]
	if _, err := precompileProbeSuccessorPrefixWithRecovery(f.plan, f.entries[:len(f.base.entries)+5], tx.Nonce()+1, f.evidence); err == nil {
		t.Fatal("strict final nonce replay accepted the missing repair finalization")
	}
	owner, err := preparePrecompileRecoveryExecutor(t.Context(), f.cfg, f.cfg, f.stateDir, f.plan, journal, f.roles, f.evidence)
	if err != nil {
		t.Fatal(err)
	}
	finalized, receipt := f.reader.finalized, f.reader.receipts[tx.Hash()]
	f.reader.finalized, f.reader.pending = step.QuoteHead, true
	delete(f.reader.receipts, tx.Hash())
	owner.deployer = &EvmTxManager{client: f.reader.client(t), chainID: new(big.Int).SetUint64(f.plan.ChainID), deploymentID: f.plan.DeploymentID, stateDir: f.stateDir, journal: journal}
	if err := owner.authenticatePrecompileRecoveryRuntime(t.Context(), f.evidence); err != nil {
		t.Fatal(err)
	}
	if owner.payloads == nil || owner.payloads.PrecompileProbeAddress != approvedPrecompileProbe(f.plan) || owner.owner != nil || owner.keeper != nil || !owner.precompileRecoveryOnly {
		t.Fatal("scoped admission opened unrelated authority or lost probe identity")
	}
	// Constructor returned with the transaction still unfinalized. Chain progress
	// is explicit, after the admission boundary and before exact receipt recovery.
	f.reader.finalized, f.reader.pending = finalized, false
	f.reader.receipts[tx.Hash()] = receipt
	if err := owner.execute(t.Context(), step.Action); err != nil {
		t.Fatal(err)
	}
	retained, err := loadPrecompileEvidence(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if retained.Recovery.Steps[0].Move.TransactionHash != tx.Hash().Hex() || retained.Recovery.Steps[0].QuoteHead != step.QuoteHead {
		t.Fatal("reconciliation replaced the saved transaction or original quote")
	}
	broadcasts, finalizedEntries := 0, 0
	for _, entry := range journal.Entries() {
		if entry.ActionID != step.Action.ID {
			continue
		}
		if entry.Stage == StageBroadcast {
			broadcasts++
			if entry.Nonce != strconv.FormatUint(tx.Nonce(), 10) || entry.TransactionHash != tx.Hash().Hex() {
				t.Fatal("recovery allocated another nonce or transaction")
			}
		}
		if entry.Stage == StageFinalized {
			finalizedEntries++
		}
	}
	if broadcasts != 1 || finalizedEntries != 1 || f.reader.unexpectedRequests.Load() != 0 {
		t.Fatalf("recovery did not retain one exact signed call: broadcasts=%d finalized=%d unexpected RPC=%d", broadcasts, finalizedEntries, f.reader.unexpectedRequests.Load())
	}
	if precompileEvidenceComplete(retained) {
		t.Fatal("one recovered receipt waived remaining custody or strict final completion")
	}
	before := len(journal.Entries())
	unrelated := actionByID(t, f.plan, "precompile.seed")
	if err := owner.Execute(t.Context(), unrelated); err == nil {
		t.Fatal("scoped public execution permitted reseeding outside signed repair authority")
	}
	if err := owner.execute(t.Context(), unrelated); err == nil || len(journal.Entries()) != before {
		t.Fatal("direct dispatch or rejected action changed the retained journal")
	}
}

func TestPrecompileRecoveryScopedExecutorRejectsChangedCustodyAndRuntime(t *testing.T) {
	f, journal, _ := newPendingPrecompileRecoveryExecutorFixture(t)
	label := fleetColdkeyLabel(1)
	role := f.roles.Substrate[label]
	changed := role
	changed.PublicKeyHex = stringsTrim0x(common.Hash{88}.Hex())
	f.roles.Substrate[label] = changed
	if _, err := preparePrecompileRecoveryExecutor(t.Context(), f.cfg, f.cfg, f.stateDir, f.plan, journal, f.roles, f.evidence); err == nil {
		t.Fatal("scoped executor accepted a changed recovery recipient")
	}
	f.roles.Substrate[label] = role
	owner, err := preparePrecompileRecoveryExecutor(t.Context(), f.cfg, f.cfg, f.stateDir, f.plan, journal, f.roles, f.evidence)
	if err != nil {
		t.Fatal(err)
	}
	owner.deployer = &EvmTxManager{client: f.reader.client(t)}
	f.reader.codes[approvedPrecompileProbe(f.plan)] = []byte{0x60, 0x11}
	if err := owner.authenticatePrecompileRecoveryRuntime(t.Context(), f.evidence); err == nil || owner.payloads != nil {
		t.Fatal("changed immutable runtime obtained a reusable payload")
	}
	if err := owner.validatePrecompileRecoveryDispatch(actionByID(t, f.plan, "precompile.transfer-out")); err == nil {
		t.Fatal("unauthenticated runtime reached the original transfer")
	}
	if _, err := os.Stat(filepath.Join(f.stateDir, precompileRecoveryCompletionFilename)); !os.IsNotExist(err) {
		t.Fatal("rejected scoped admission published a completion")
	}
}

func TestPrecompileRecoverySavedTransactionEnforcesGasAndFeeBeforeReplay(t *testing.T) {
	f := newPrecompileRecoveryTestFixture(t)
	step := f.evidence.Recovery.Steps[0]
	original := f.reader.transactions[common.HexToHash(step.Move.TransactionHash)]
	role, err := f.roles.EVMKey("deployer")
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(role.PrivateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"gas", "fee"} {
		gas, fee := original.Gas(), original.GasFeeCap()
		if fault == "gas" {
			gas = f.evidence.Recovery.Authorization.Request.MaximumGasUnits + 1
		} else {
			fee = new(big.Int).SetUint64(f.evidence.Recovery.Authorization.Request.MaximumFeePerGasWei + 1)
		}
		transaction, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: original.Nonce(), To: original.To(), Value: original.Value(), Data: original.Data(), Gas: gas, GasPrice: fee}), types.LatestSignerForChainID(original.ChainId()), key)
		if err != nil {
			t.Fatal(err)
		}
		stateDir := t.TempDir()
		if err := os.Chmod(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		journal, err := OpenJournal(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = journal.Close() })
		raw, err := transaction.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(stateDir, "transactions", stringsTrim0x(transaction.Hash().Hex())+".rlp"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := journal.Append(JournalEntry{DeploymentID: f.plan.DeploymentID, PlanHash: f.plan.PlanHash, ActionID: step.Action.ID, IntentHash: step.Action.IntentHash, Stage: StageIntent}); err != nil {
			t.Fatal(err)
		}
		if err := journal.Append(JournalEntry{DeploymentID: f.plan.DeploymentID, PlanHash: f.plan.PlanHash, ActionID: step.Action.ID, IntentHash: step.Action.IntentHash, Stage: StageBroadcast, Signer: f.plan.Roles.Deployer, Nonce: strconv.FormatUint(transaction.Nonce(), 10), TransactionHash: transaction.Hash().Hex(), RecoveryBlock: step.QuoteHead.Number, RecoveryBlockHash: step.QuoteHead.Hash}); err != nil {
			t.Fatal(err)
		}
		manager := &EvmTxManager{stateDir: stateDir, journal: journal, chainID: original.ChainId(), deploymentID: f.plan.DeploymentID}
		if _, err := manager.prepareOwnedEVMTransaction(t.Context(), f.plan.PlanHash, step.Action, transaction.To(), transaction.Value(), transaction.Data(), 0); err == nil || !strings.Contains(err.Error(), "exceeded its approved gas or fee cap") {
			t.Fatalf("saved transaction %s did not enforce bounds before any RPC or rebroadcast: %v", fault, err)
		}
	}
}
