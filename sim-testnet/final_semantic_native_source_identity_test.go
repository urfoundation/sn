package main

// These tests bind native call projections to authenticated plan, journal,
// postcondition, and prepared-submission sources.

import (
	"testing"

	validatorpkg "github.com/urfoundation/sn/validator"
)

// Builds one exact approved registration plan/journal/postcondition graph.
func finalNativeTestRegistrationSource(t *testing.T) (*SetupPlan, []JournalEntry, JournalEntry, *ActionPostcondition) {
	t.Helper()
	signer, _ := finalNativeTestAccount(t, 0x81)
	_, hotkey := finalNativeTestAccount(t, 0x82)
	planHash, intentHash, transactionHash := finalTestHex(0x83), finalTestHex(0x84), finalTestHex(0x85)
	action := Action{ID: "validator.register.1", Kind: "substrate-extrinsic", Target: "validator:1", Parameters: map[string]string{"maximum_burn_rao": "600000"}, Spend: Spend{Registrations: 1}, IntentHash: intentHash}
	plan := &SetupPlan{PlanHash: planHash, Netuid: 521, RegistrationBurnLimitRao: 600_000, Actions: []Action{action}}
	observed := map[string]any{"role": validatorHotkeyLabel(1), "hotkey": hotkey, "coldkey": signer.Address(), "uid": uint64(12)}
	postcondition := &ActionPostcondition{DeploymentID: "deployment", PlanHash: planHash, ActionID: action.ID, IntentHash: intentHash, Observed: observed, IndependentObserved: map[string]any{"role": validatorHotkeyLabel(1), "hotkey": hotkey, "coldkey": signer.Address(), "uid": uint64(12)}}
	broadcast := JournalEntry{DeploymentID: postcondition.DeploymentID, PlanHash: planHash, ActionID: action.ID, IntentHash: intentHash, Stage: StageBroadcast, Signer: signer.Address(), Nonce: "7", TransactionHash: transactionHash}
	finalized := JournalEntry{DeploymentID: postcondition.DeploymentID, PlanHash: planHash, ActionID: action.ID, IntentHash: intentHash, Stage: StageFinalized, TransactionHash: transactionHash, BlockNumber: 99, BlockHash: finalTestHex(0x86)}
	return plan, []JournalEntry{broadcast, finalized}, finalized, postcondition
}

// Proves the expected signed call is projected from three independently bound
// sources.
func TestFinalNativeRegistrationSourceBindsPlanJournalAndPostcondition(t *testing.T) {
	plan, entries, finalized, postcondition := finalNativeTestRegistrationSource(t)
	evidence, err := finalNativeRegistrationEvidenceFromSource(plan, entries, finalized, postcondition)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Operation != finalNativeOperationRegistration || evidence.Netuid != 521 || evidence.Nonce != 7 || evidence.UID != 12 || evidence.RegistrationLimitRao != 600_000 {
		t.Fatalf("registration source projection differs: %+v", evidence)
	}
	wrongSigner := append([]JournalEntry(nil), entries...)
	other, _ := finalNativeTestAccount(t, 0x87)
	wrongSigner[0].Signer = other.Address()
	if _, err := finalNativeRegistrationEvidenceFromSource(plan, wrongSigner, finalized, postcondition); err == nil {
		t.Fatal("registration source accepted a broadcast by another signer")
	}
	wrongLimit := *plan
	wrongLimit.Actions = append([]Action(nil), plan.Actions...)
	wrongLimit.Actions[0].Parameters = map[string]string{"maximum_burn_rao": "600001"}
	if _, err := finalNativeRegistrationEvidenceFromSource(&wrongLimit, entries, finalized, postcondition); err == nil {
		t.Fatal("registration source accepted another burn limit")
	}
	wrongIndependent := *postcondition
	wrongIndependent.IndependentObserved = map[string]any{"role": validatorHotkeyLabel(1), "hotkey": postcondition.Observed["hotkey"], "coldkey": postcondition.Observed["coldkey"], "uid": uint64(13)}
	if _, err := finalNativeRegistrationEvidenceFromSource(plan, entries, finalized, &wrongIndependent); err == nil {
		t.Fatal("registration source accepted a contradictory independent UID")
	}
}

// Builds one applied intent with an exact prepared CRv4 call suffix.
func finalNativeTestAppliedIntent(t *testing.T) *validatorpkg.SteeringIntent {
	t.Helper()
	_, _, prepared := finalNativeTestCommitEvidence(t)
	intent := &validatorpkg.SteeringIntent{
		Schema: validatorpkg.SteeringIntentSchema, ValidatorID: 1, Netuid: prepared.Netuid, SubnetEpoch: prepared.SubnetEpoch,
		SelfUID: 4, UIDs: append([]uint16(nil), prepared.UIDs...), Scores: []validatorpkg.RationalJSON{{Numerator: "1", Denominator: "1"}, {Numerator: "1", Denominator: "2"}}, Prepared: prepared,
	}
	hash, err := intent.ReconstructedVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	intent.VectorHash, intent.Status = hash, "applied"
	intent.Values = append([]uint16(nil), prepared.Values...)
	intent.ExtrinsicHash, intent.FinalizedBlock, intent.FinalizedBlockHash = prepared.ExtrinsicHash, 110, finalTestHex(0x91)
	intent.RevealBlock = prepared.RevealBlock
	intent.ApplicationBlock, intent.ApplicationBlockHash = prepared.RevealBlock+3, finalTestHex(0x92)
	return intent
}

// Proves commit, reveal, and application evidence cannot be projected from
// inconsistent intent fields.
func TestFinalNativeIntentSourceBindsPreparedLifecycle(t *testing.T) {
	intent := finalNativeTestAppliedIntent(t)
	commit, err := finalNativeIntentCallEvidence(intent, finalNativeOperationCommit)
	if err != nil {
		t.Fatal(err)
	}
	reveal, err := finalNativeIntentCallEvidence(intent, finalNativeOperationReveal)
	if err != nil {
		t.Fatal(err)
	}
	application, err := finalNativeIntentCallEvidence(intent, finalNativeOperationApplication)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalNativeCRv4Lineage(commit, reveal, application); err != nil {
		t.Fatal(err)
	}
	wrongNetuid := *intent
	wrongNetuid.Netuid--
	if _, err := finalNativeIntentCallEvidence(&wrongNetuid, finalNativeOperationCommit); err == nil {
		t.Fatal("intent with a prepared call for another netuid was accepted")
	}
	wrongReveal := *intent
	wrongReveal.RevealBlock++
	if _, err := finalNativeIntentCallEvidence(&wrongReveal, finalNativeOperationReveal); err == nil {
		t.Fatal("intent with a relabeled reveal block was accepted")
	}
	wrongValues := *intent
	wrongValues.Values = append([]uint16(nil), intent.Values...)
	wrongValues.Values[0]++
	if _, err := finalNativeIntentCallEvidence(&wrongValues, finalNativeOperationApplication); err == nil {
		t.Fatal("intent with values different from its prepared submission was accepted")
	}
}
