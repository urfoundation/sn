// Real local journals and signed synthetic envelopes exercise gas revision,
// crash recovery and exact terminal-snapshot composition without chain access.
package main

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Proposing and publishing a revision cannot be mixed with execution or used
// as an unreviewed way to add gas authority to another command.
func TestPrecompileRecoveryGasRevisionOptions(t *testing.T) {
	options := cliOptions{ProvisionalResume: true, PlanHash: common.Hash{70}.Hex(), ProbeRecoveryReviseGas: true}
	if err := validatePrecompileRecoveryOptions("probe-recovery", options); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"command", "apply", "execute"} {
		changed := options
		command := "probe-recovery"
		switch fault {
		case "command":
			command = "resume"
		case "apply":
			changed.Apply = true
		case "execute":
			changed.Apply, changed.ProbeRecoveryExecute = true, true
		}
		if err := validatePrecompileRecoveryOptions(command, changed); err == nil {
			t.Errorf("accepted revision %s", fault)
		}
	}
}

// A complete synthetic chain supplies original receipts; the mutable state
// stops at one unsigned top-up and its exact pre-signing gas-ceiling failure.
type precompileGasRevisionFixture struct {
	base          *precompileRecoveryTestFixture
	owner         *Executor
	evidence      *PrecompileConformanceEvidence
	authorization PrecompileRecoveryAuthorization
}

// Each test owns its files and journal. Table negatives clone only small signed
// records rather than rebuilding the full deployment or waiting on a network.
func newPrecompileGasRevisionFixture(t *testing.T) *precompileGasRevisionFixture {
	t.Helper()
	f := newPrecompileRecoveryTestFixture(t)
	journal, err := OpenJournal(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := journal.Append(JournalEntry{DeploymentID: f.plan.DeploymentID, PlanHash: f.plan.PlanHash, ActionID: "precompile.snapshot", IntentHash: actionByID(t, f.plan, "precompile.snapshot").IntentHash, Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	legacy := f.evidence.Recovery.Authorization
	legacy.Request.Budget.JournalHash = journal.Entries()[0].EntryHash
	legacy.Request.BudgetHash, err = canonicalHashHex(legacy.Request.Budget)
	if err != nil {
		t.Fatal(err)
	}
	f.sign(t, legacy.Request, &legacy.Hash, &legacy.OwnerSignature, &legacy.DeployerSignature)
	step := f.evidence.Recovery.Steps[0]
	step.Move = PrecompileMoveStep{AmountRao: step.Move.AmountRao, FromBeforeRao: step.QuoteSourceRao, ToBeforeRao: step.QuoteDestinationRao}
	step.SourceCreditRao, step.DestinationCreditRao = 0, 0
	step.Action, _, err = precompileRecoveryAction(&legacy, 0, step)
	if err != nil {
		t.Fatal(err)
	}
	evidence := *f.original
	evidence.Recovery = &PrecompileRecoveryEvidence{Authorization: legacy, Steps: []PrecompileRecoveryStep{step}}
	if err := writePrecompileEvidence(f.stateDir, &evidence); err != nil {
		t.Fatal(err)
	}
	if err := writeCoordinatorRepairFile(filepath.Join(f.stateDir, precompileRecoveryAuthorizationFilename), &legacy); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []JournalEntry{
		{DeploymentID: f.plan.DeploymentID, PlanHash: f.plan.PlanHash, ActionID: step.Action.ID, IntentHash: step.Action.IntentHash, Stage: StageIntent},
		{DeploymentID: f.plan.DeploymentID, PlanHash: f.plan.PlanHash, ActionID: step.Action.ID, IntentHash: step.Action.IntentHash, Stage: StageFailed, Error: step.Action.ID + " padded gas 2868304 exceeds approved gas-unit ceiling 500000"},
	} {
		if err := journal.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	budget, err := newPrecompileRecoveryBudgetForGas(f.cfg, f.stateDir, f.plan, journal.Entries(), precompileRecoveryRevisedGasUnits)
	if err != nil {
		t.Fatal(err)
	}
	request, err := newPrecompileRecoveryGasRevisionRequest(f.plan, &evidence, *budget, journal.Entries())
	if err != nil {
		t.Fatal(err)
	}
	authorization := PrecompileRecoveryAuthorization{Request: request}
	f.sign(t, request, &authorization.Hash, &authorization.OwnerSignature, &authorization.DeployerSignature)
	if err := validatePrecompileRecoveryPlan(f.plan, &evidence, &authorization); err != nil {
		t.Fatal(err)
	}
	if err := writeCoordinatorRepairFile(filepath.Join(f.stateDir, precompileRecoveryGasRevisionFilename), &authorization); err != nil {
		t.Fatal(err)
	}
	return &precompileGasRevisionFixture{base: f, evidence: &evidence, authorization: authorization, owner: &Executor{cfg: f.cfg, plan: f.plan, stateDir: f.stateDir, journal: journal}}
}

// JSON cloning retains wire semantics and removes aliases between negative cases.
func clonePrecompileGasRevisionEvidence(t *testing.T, evidence *PrecompileConformanceEvidence) *PrecompileConformanceEvidence {
	t.Helper()
	raw, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	var result PrecompileConformanceEvidence
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return &result
}

// The exact incident estimate fails under v1, succeeds within the explicit v2
// budget and leaves original approvals, quote, journal and custody unchanged.
func TestPrecompileRecoveryGasRevisionAdoptsOnlyUnsignedTopUp(t *testing.T) {
	f := newPrecompileGasRevisionFixture(t)
	prior := clonePrecompileGasRevisionEvidence(t, f.evidence)
	journalBefore, err := os.ReadFile(filepath.Join(f.base.stateDir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	authorityBefore, err := os.ReadFile(filepath.Join(f.base.stateDir, precompileRecoveryAuthorizationFilename))
	if err != nil {
		t.Fatal(err)
	}
	fee := new(big.Int).SetUint64(f.authorization.Request.MaximumFeePerGasWei)
	balance := new(big.Int).Mul(big.NewInt(10), big.NewInt(1_000_000_000_000_000_000))
	if _, _, err := validateEVMTransactionEnvelope(prior.Recovery.Steps[0].Action, 2_369_420, fee, balance, new(big.Int)); err == nil || !strings.Contains(err.Error(), "padded gas 2868304 exceeds approved gas-unit ceiling 500000") {
		t.Fatalf("incident was not reproduced: %v", err)
	}
	if err := f.owner.loadPrecompileRecoveryAuthorization(f.evidence); err != nil {
		t.Fatal(err)
	}
	step := f.evidence.Recovery.Steps[0]
	if step.Action.ID != precompileRecoveryActionPrefix+"v2.1" || step.Action.IntentHash == prior.Recovery.Steps[0].Action.IntentHash || step.QuoteHead != prior.Recovery.Steps[0].QuoteHead || step.Move != prior.Recovery.Steps[0].Move || f.evidence.Recovery.Authorization.Request.OriginalEvidenceHash != f.base.original.EvidenceHash {
		t.Fatal("revision changed custody or reused the old journal identity")
	}
	if gas, _, err := validateEVMTransactionEnvelope(step.Action, 2_369_420, fee, balance, new(big.Int)); err != nil || gas != 2_868_304 {
		t.Fatalf("revised finite envelope: gas=%d err=%v", gas, err)
	}
	if f.authorization.Request.MaximumGasUnits < 2*2_868_304 || f.authorization.Request.Budget.MaximumRecoveryWei != "4800000006000000000" {
		t.Fatal("revision lost the doubled gas margin or exact finite reserve")
	}
	first, err := os.ReadFile(filepath.Join(f.base.stateDir, "public/precompile-conformance.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.owner.loadPrecompileRecoveryAuthorization(f.evidence); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(f.base.stateDir, "public/precompile-conformance.json"))
	journalAfter, _ := os.ReadFile(filepath.Join(f.base.stateDir, "journal.jsonl"))
	authorityAfter, _ := os.ReadFile(filepath.Join(f.base.stateDir, precompileRecoveryAuthorizationFilename))
	if string(first) != string(second) || string(journalBefore) != string(journalAfter) || string(authorityBefore) != string(authorityAfter) {
		t.Fatal("idempotent adoption rewrote retained evidence")
	}
	if err := f.owner.journal.Append(JournalEntry{DeploymentID: f.base.plan.DeploymentID, PlanHash: f.base.plan.PlanHash, ActionID: step.Action.ID, IntentHash: step.Action.IntentHash, Stage: StageIntent}); err != nil {
		t.Fatalf("new journal identity could not resume: %v", err)
	}
}

// Even new owner signatures cannot enlarge the supported scope or substitute
// a different pending snapshot, old approval, gas refusal or journal lineage.
func TestPrecompileRecoveryGasRevisionRejectsAuthorityAndJournalDrift(t *testing.T) {
	f := newPrecompileGasRevisionFixture(t)
	good := clonePrecompileGasRevisionEvidence(t, f.evidence)
	good.Recovery.Authorization = f.authorization
	for _, fault := range []string{"original", "recipient", "fee", "gas", "count", "old receipt", "nested", "source hash", "refusal margin", "refusal hash", "refusal text", "old signature", "budget", "missing intent", "missing budget", "signed prior"} {
		changed := clonePrecompileGasRevisionEvidence(t, good)
		r := &changed.Recovery.Authorization.Request
		entries := f.owner.journal.Entries()
		switch fault {
		case "original":
			r.OriginalEvidenceHash = common.Hash{61}.Hex()
		case "recipient":
			r.RecoveryColdkey = common.Hash{62}.Hex()
		case "fee":
			r.MaximumFeePerGasWei--
		case "gas":
			r.MaximumGasUnits++
		case "count":
			r.MaximumSteps++
		case "old receipt":
			r.GasRevision.Evidence.Recovery.Steps[0].Move.TransactionHash = common.Hash{63}.Hex()
		case "nested":
			r.GasRevision.Evidence.Recovery.Authorization.Request.Schema = "urnetwork-precompile-recovery-v2"
		case "source hash":
			r.GasRevision.Evidence.EvidenceHash = common.Hash{64}.Hex()
		case "refusal margin":
			r.GasRevision.PaddedGasUnits = precompileRecoveryRevisedGasUnits/2 + 1
		case "refusal hash":
			r.GasRevision.JournalHash = common.Hash{65}.Hex()
		case "refusal text":
			entries[len(entries)-1].Error = "unrelated semantic failure"
		case "old signature":
			r.GasRevision.Evidence.Recovery.Authorization.OwnerSignature = r.GasRevision.Evidence.Recovery.Authorization.DeployerSignature
		case "budget":
			r.Budget.MaximumRecoveryWei = "1"
		case "missing intent":
			entries = append(entries[:1], entries[2:]...)
		case "missing budget":
			entries = entries[1:]
		case "signed prior":
			entries[len(entries)-1].TransactionHash = common.Hash{66}.Hex()
		}
		a := &changed.Recovery.Authorization
		f.base.sign(t, a.Request, &a.Hash, &a.OwnerSignature, &a.DeployerSignature)
		if err := validatePrecompileRecoveryGasRevisionJournal(f.base.plan, changed, entries); err == nil {
			t.Errorf("accepted %s", fault)
		}
	}
}

// Persisting the signature precedes its journal marker. Neither missing receipt
// nor missing broadcast metadata is sufficient to replace that operation.
func TestPrecompileRecoveryGasRevisionRejectsSavedSignatures(t *testing.T) {
	f := newPrecompileGasRevisionFixture(t)
	entries := f.owner.journal.Entries()
	if err := requireUnsignedPrecompileRecovery(f.base.stateDir, f.evidence, entries); err != nil {
		t.Fatal(err)
	}
	transaction := f.base.reader.transactions[common.HexToHash(f.base.evidence.Recovery.Steps[0].Move.TransactionHash)]
	raw, err := transaction.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.base.stateDir, "transactions", stringsTrim0x(transaction.Hash().Hex())+".rlp")
	if err := atomicWrite(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(f.base.stateDir, "public/precompile-conformance.json"))
	if err := f.owner.loadPrecompileRecoveryAuthorization(f.evidence); err == nil || !strings.Contains(err.Error(), "unjournaled signed probe") {
		t.Fatalf("orphan signed bytes allowed adoption: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(f.base.stateDir, "public/precompile-conformance.json"))
	if string(before) != string(after) {
		t.Fatal("refused adoption changed evidence")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []JournalStage{StageIntent, StageBroadcast, StageIncluded, StageFinalized} {
		changed := append([]JournalEntry(nil), entries...)
		changed[len(changed)-1].Stage = stage
		changed[len(changed)-1].TransactionHash = transaction.Hash().Hex()
		if err := requireUnsignedPrecompileRecovery(f.base.stateDir, f.evidence, changed); err == nil {
			t.Errorf("accepted signed %s", stage)
		}
	}
	owner := *f.owner
	owner.journal = &Journal{entries: entries}
	if err := owner.loadPrecompileRecoveryAuthorization(f.evidence); err == nil || !strings.Contains(err.Error(), "exclusive") {
		t.Fatalf("unowned adoption accepted: %v", err)
	}
}

// The immutable origin and exact pending terminal snapshot are distinct valid
// sources. Similar but unsigned snapshots never become composable by omission.
func TestPrecompileRecoveryGasRevisionAuthenticatesTerminalSnapshot(t *testing.T) {
	f := newPrecompileGasRevisionFixture(t)
	// Insert the actual unsigned refusal before the fixture's successful calls.
	// Its new signatures bind the resulting journal coordinates exactly.
	cut := 0
	for i, entry := range f.base.entries {
		if strings.HasPrefix(entry.ActionID, precompileRecoveryActionPrefix) {
			cut = i
			break
		}
	}
	if cut == 0 {
		t.Fatal("fixture has no recovery call boundary")
	}
	entries := append([]JournalEntry(nil), f.base.entries[:cut]...)
	appendEntry := func(entry JournalEntry) {
		t.Helper()
		entry.Sequence = entries[len(entries)-1].Sequence + 1
		entry.PreviousHash = entries[len(entries)-1].EntryHash
		entry.EntryHash = ""
		var err error
		entry.EntryHash, err = canonicalHashHex(entry)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	terminal := clonePrecompileGasRevisionEvidence(t, f.evidence)
	legacy := &terminal.Recovery.Authorization
	legacy.Request.Budget.JournalHash = entries[len(entries)-1].EntryHash
	var err error
	legacy.Request.BudgetHash, err = canonicalHashHex(legacy.Request.Budget)
	if err != nil {
		t.Fatal(err)
	}
	f.base.sign(t, legacy.Request, &legacy.Hash, &legacy.OwnerSignature, &legacy.DeployerSignature)
	terminal.Recovery.Steps[0].Action, _, err = precompileRecoveryAction(legacy, 0, terminal.Recovery.Steps[0])
	if err != nil {
		t.Fatal(err)
	}
	terminal.EvidenceHash = ""
	terminal.EvidenceHash, err = canonicalHashHex(*terminal)
	if err != nil {
		t.Fatal(err)
	}
	prior := terminal.Recovery.Steps[0].Action
	appendEntry(JournalEntry{DeploymentID: f.base.plan.DeploymentID, PlanHash: f.base.plan.PlanHash, ActionID: prior.ID, IntentHash: prior.IntentHash, Stage: StageIntent})
	appendEntry(JournalEntry{DeploymentID: f.base.plan.DeploymentID, PlanHash: f.base.plan.PlanHash, ActionID: prior.ID, IntentHash: prior.IntentHash, Stage: StageFailed, Error: prior.ID + " padded gas 2868304 exceeds approved gas-unit ceiling 500000"})
	budget := f.authorization.Request.Budget
	budget.JournalHash = entries[len(entries)-1].EntryHash
	request, err := newPrecompileRecoveryGasRevisionRequest(f.base.plan, terminal, budget, entries)
	if err != nil {
		t.Fatal(err)
	}
	f.authorization = PrecompileRecoveryAuthorization{Request: request}
	f.base.sign(t, request, &f.authorization.Hash, &f.authorization.OwnerSignature, &f.authorization.DeployerSignature)
	f.evidence = terminal
	completed := clonePrecompileGasRevisionEvidence(t, f.base.evidence)
	completed.Recovery.Authorization = f.authorization
	for i := range completed.Recovery.Steps {
		action, _, err := precompileRecoveryAction(&f.authorization, i, completed.Recovery.Steps[i])
		if err != nil {
			t.Fatal(err)
		}
		completed.Recovery.Steps[i].Action = action
	}
	for _, entry := range f.base.entries[cut:] {
		for i, old := range f.base.evidence.Recovery.Steps {
			if entry.ActionID == old.Action.ID {
				entry.ActionID, entry.IntentHash = completed.Recovery.Steps[i].Action.ID, completed.Recovery.Steps[i].Action.IntentHash
			}
		}
		appendEntry(entry)
	}
	if err := writePrecompileEvidence(f.base.stateDir, completed); err != nil {
		t.Fatal(err)
	}
	completion := *f.base.completion
	completion.Record.AuthorizationHash = f.authorization.Hash
	completion.Record.EvidenceHash = completed.EvidenceHash
	completion.Record.JournalHash = entries[len(entries)-1].EntryHash
	f.base.sign(t, completion.Record, &completion.Hash, &completion.OwnerSignature, &completion.DeployerSignature)
	if err := verifyPrecompileRecoveryCompletion(f.base.plan, completed, &completion); err != nil {
		t.Fatal(err)
	}
	nonce := f.base.plan.PrecompileProbeSuccessor.DeployerNonce + uint64(len(f.base.entries)-len(f.base.base.entries))
	if err := verifyPrecompileProbeSuccessorCalls(t.Context(), f.base.cfg, f.base.stateDir, f.base.plan, entries, f.base.reader, f.base.reader.finalized, nonce); err != nil {
		t.Fatalf("v2 strict receipt replay: %v", err)
	}
	for _, source := range []*PrecompileConformanceEvidence{f.base.original, f.evidence} {
		if err := verifyPrecompileRecoveryOriginalEvidence(source, completed); err != nil {
			t.Fatal(err)
		}
	}
	for _, fault := range []string{"quote", "action", "signature", "source hash", "receipt"} {
		changed := clonePrecompileGasRevisionEvidence(t, f.evidence)
		switch fault {
		case "quote":
			changed.Recovery.Steps[0].QuoteSourceRao++
		case "action":
			changed.Recovery.Steps[0].Action.IntentHash = common.Hash{67}.Hex()
		case "signature":
			changed.Recovery.Authorization.OwnerSignature = changed.Recovery.Authorization.DeployerSignature
		case "source hash":
			changed.EvidenceHash = common.Hash{68}.Hex()
		case "receipt":
			changed.Recovery.Steps[0].Move.TransactionHash = common.Hash{69}.Hex()
		}
		if fault != "source hash" {
			changed.EvidenceHash = ""
			var err error
			changed.EvidenceHash, err = canonicalHashHex(*changed)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := verifyPrecompileRecoveryOriginalEvidence(changed, completed); err == nil {
			t.Errorf("accepted altered terminal %s", fault)
		}
	}
	if !reflect.DeepEqual(f.evidence.Recovery.Authorization, f.authorization.Request.GasRevision.Evidence.Recovery.Authorization) {
		t.Fatal("terminal authority was rewritten")
	}
}
