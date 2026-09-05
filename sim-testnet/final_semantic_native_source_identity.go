package main

// This file projects native call identity only from authenticated setup and
// validator source records. Public replay must independently decode the exact
// canonical block bytes against these expectations.

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	validatorpkg "github.com/urfoundation/sn/validator"
)

// Projects one ordinary registration only from the approved plan, exact
// broadcast/finalized journal pair, and independently observed postcondition.
func finalNativeRegistrationEvidenceFromSource(plan *SetupPlan, entries []JournalEntry, finalized JournalEntry, postcondition *ActionPostcondition) (FinalNativeCallEvidence, error) {
	if plan == nil || postcondition == nil || len(entries) == 0 {
		return FinalNativeCallEvidence{}, errors.New("native registration source evidence is incomplete")
	}
	if err := verifyFinalSetupPlanActionReceipt(plan, postcondition); err != nil {
		return FinalNativeCallEvidence{}, err
	}
	if finalized.Stage != StageFinalized || finalized.DeploymentID != postcondition.DeploymentID || finalized.PlanHash != postcondition.PlanHash || finalized.ActionID != postcondition.ActionID || finalized.IntentHash != postcondition.IntentHash || finalized.TransactionHash == "" || finalized.BlockNumber == 0 || finalized.BlockHash == "" {
		return FinalNativeCallEvidence{}, errors.New("native registration finalized journal identity is incomplete or inconsistent")
	}
	if finalized.TransactionHash != strings.ToLower(finalized.TransactionHash) || requireFinalHex32("native registration extrinsic", finalized.TransactionHash) != nil {
		return FinalNativeCallEvidence{}, errors.New("native registration finalized extrinsic hash is not canonical")
	}
	var action *Action
	for index := range plan.Actions {
		if plan.Actions[index].ID == postcondition.ActionID {
			action = &plan.Actions[index]
			break
		}
	}
	if action == nil || action.Kind != "substrate-extrinsic" || action.Spend.Registrations != 1 {
		return FinalNativeCallEvidence{}, errors.New("native registration is not one exact approved Substrate registration action")
	}
	limit, err := strconv.ParseUint(action.Parameters["maximum_burn_rao"], 10, 64)
	if err != nil || strconv.FormatUint(limit, 10) != action.Parameters["maximum_burn_rao"] || limit != plan.RegistrationBurnLimitRao || limit == 0 {
		return FinalNativeCallEvidence{}, errors.New("native registration burn limit differs from its approved plan")
	}
	role, roleOK := postcondition.Observed["role"].(string)
	hotkey, hotkeyOK := postcondition.Observed["hotkey"].(string)
	coldkey, coldkeyOK := postcondition.Observed["coldkey"].(string)
	uid, uidOK := finalSemanticObservedUint(postcondition.Observed, "uid")
	expectedRole, roleErr := finalNativeRegistrationRole(*action)
	if roleErr != nil {
		return FinalNativeCallEvidence{}, fmt.Errorf("derive native registration role: %w", roleErr)
	}
	if !roleOK || !hotkeyOK || !coldkeyOK || !uidOK || uid > uint64(^uint16(0)) || role != expectedRole {
		return FinalNativeCallEvidence{}, fmt.Errorf("native registration postcondition identity differs from its approved action: role=%q expected=%q target=%q hotkey=%t coldkey=%t uid=%t", role, expectedRole, action.Target, hotkeyOK, coldkeyOK, uidOK)
	}
	for _, field := range []string{"role", "hotkey", "coldkey", "uid"} {
		if !finalJSONEqual(postcondition.Observed[field], postcondition.IndependentObserved[field]) {
			return FinalNativeCallEvidence{}, fmt.Errorf("native registration independent %s observation differs", field)
		}
	}
	wantSigner, err := finalNativeAccountHex(coldkey)
	if err != nil {
		return FinalNativeCallEvidence{}, fmt.Errorf("native registration coldkey: %w", err)
	}
	var signer string
	var nonce uint64
	foundBroadcast := false
	for _, entry := range entries {
		if entry.Stage != StageBroadcast || entry.PlanHash != finalized.PlanHash || entry.ActionID != finalized.ActionID || entry.IntentHash != finalized.IntentHash {
			continue
		}
		if entry.TransactionHash != finalized.TransactionHash {
			return FinalNativeCallEvidence{}, errors.New("native registration broadcast transaction differs from finalized inclusion")
		}
		entrySigner, signerErr := finalNativeAccountHex(entry.Signer)
		entryNonce, nonceErr := strconv.ParseUint(entry.Nonce, 10, 32)
		if signerErr != nil || nonceErr != nil || strconv.FormatUint(entryNonce, 10) != entry.Nonce || entrySigner != wantSigner {
			return FinalNativeCallEvidence{}, errors.New("native registration broadcast signer or nonce differs from its coldkey source")
		}
		if foundBroadcast && (signer != entrySigner || nonce != entryNonce) {
			return FinalNativeCallEvidence{}, errors.New("native registration has ambiguous broadcast identity")
		}
		foundBroadcast, signer, nonce = true, entrySigner, entryNonce
	}
	if !foundBroadcast {
		return FinalNativeCallEvidence{}, errors.New("native registration has no exact broadcast journal identity")
	}
	return finalNativeRegistrationCallEvidence(signer, uint32(nonce), plan.Netuid, hotkey, uint16(uid), limit)
}

// Maps an approved registration action to the exact signer-role label emitted
// by its native postcondition.  Plan targets describe the operational class
// (for example, a challenger fleet), while the observed role identifies the
// concrete hotkey that was registered.
func finalNativeRegistrationRole(action Action) (string, error) {
	if action.ID == "" {
		return "", errors.New("native registration action ID is empty")
	}
	for _, candidate := range []struct {
		prefix string
		label  func(int) string
	}{
		{prefix: "fleet.register.", label: fleetHotkeyLabel},
		{prefix: "validator.register.", label: validatorHotkeyLabel},
	} {
		if !strings.HasPrefix(action.ID, candidate.prefix) {
			continue
		}
		index, err := strconv.Atoi(strings.TrimPrefix(action.ID, candidate.prefix))
		if err != nil || index < 1 || action.ID != candidate.prefix+strconv.Itoa(index) {
			return "", fmt.Errorf("native registration action %q has a noncanonical role index", action.ID)
		}
		return candidate.label(index), nil
	}
	return action.Target, nil
}

// Projects one lifecycle replacement from its approved action, broadcast
// signer/nonce, and exact pre/post UID mutation evidence.
func finalNativeLifecycleRegistrationEvidence(plan *SetupPlan, entries []JournalEntry, registration *FleetLifecycleRegistrationEvidence) (FinalNativeCallEvidence, error) {
	if plan == nil || registration == nil || len(entries) == 0 {
		return FinalNativeCallEvidence{}, errors.New("native lifecycle registration source evidence is incomplete")
	}
	var action *Action
	for index := range plan.Actions {
		if plan.Actions[index].ID == registration.ActionID {
			action = &plan.Actions[index]
			break
		}
	}
	if action == nil || action.Kind != "substrate-extrinsic" || action.Spend.Registrations != 1 || action.Target == "" || action.IntentHash != registration.IntentHash || plan.PlanHash != registration.PlanHash {
		return FinalNativeCallEvidence{}, errors.New("native lifecycle registration differs from its approved action")
	}
	limit, err := strconv.ParseUint(action.Parameters["maximum_burn_rao"], 10, 64)
	if err != nil || strconv.FormatUint(limit, 10) != action.Parameters["maximum_burn_rao"] || limit != plan.RegistrationBurnLimitRao || limit == 0 {
		return FinalNativeCallEvidence{}, errors.New("native lifecycle registration burn limit differs from its approved plan")
	}
	wantSigner, err := finalNativeAccountHex(registration.ReplacementColdkey)
	if err != nil {
		return FinalNativeCallEvidence{}, fmt.Errorf("native lifecycle registration coldkey: %w", err)
	}
	var signer string
	var nonce uint64
	found := false
	for _, entry := range entries {
		if entry.Stage != StageBroadcast || entry.PlanHash != plan.PlanHash || entry.ActionID != action.ID || entry.IntentHash != action.IntentHash {
			continue
		}
		entrySigner, signerErr := finalNativeAccountHex(entry.Signer)
		entryNonce, nonceErr := strconv.ParseUint(entry.Nonce, 10, 32)
		if signerErr != nil || nonceErr != nil || strconv.FormatUint(entryNonce, 10) != entry.Nonce || entrySigner != wantSigner || !strings.EqualFold(entry.TransactionHash, registration.TransactionHash) {
			return FinalNativeCallEvidence{}, errors.New("native lifecycle registration broadcast identity differs from its finalized mutation")
		}
		if found && (signer != entrySigner || nonce != entryNonce) {
			return FinalNativeCallEvidence{}, errors.New("native lifecycle registration has ambiguous broadcast identity")
		}
		found, signer, nonce = true, entrySigner, entryNonce
	}
	if !found {
		return FinalNativeCallEvidence{}, errors.New("native lifecycle registration has no exact broadcast journal identity")
	}
	return finalNativeRegistrationCallEvidence(signer, uint32(nonce), plan.Netuid, registration.ReplacementHotkey, registration.VictimUID, limit)
}

// Projects commit, automatic reveal, or application from one sealed intent.
func finalNativeIntentCallEvidence(intent *validatorpkg.SteeringIntent, operation string) (FinalNativeCallEvidence, error) {
	if intent == nil || intent.Prepared == nil || intent.Status != "applied" {
		return FinalNativeCallEvidence{}, errors.New("native CRv4 applied intent is incomplete")
	}
	if err := intent.VerifyVectorHash(); err != nil {
		return FinalNativeCallEvidence{}, fmt.Errorf("native CRv4 vector hash: %w", err)
	}
	prepared := intent.Prepared
	if intent.Netuid != prepared.Netuid || !strings.EqualFold(intent.ExtrinsicHash, prepared.ExtrinsicHash) || intent.FinalizedBlock == 0 || intent.RevealBlock != prepared.RevealBlock || intent.ApplicationBlock < intent.RevealBlock || !slices.Equal(intent.UIDs, prepared.UIDs) || !slices.Equal(intent.Values, prepared.Values) {
		return FinalNativeCallEvidence{}, errors.New("native CRv4 applied intent differs from its prepared submission or lifecycle")
	}
	commit, err := finalNativeCommitCallEvidence(prepared, intent.SelfUID, intent.FinalizedBlock)
	if err != nil {
		return FinalNativeCallEvidence{}, err
	}
	switch operation {
	case finalNativeOperationCommit:
		return commit, nil
	case finalNativeOperationReveal:
		return finalNativeAutomaticCallEvidence(commit, operation, intent.RevealBlock)
	case finalNativeOperationApplication:
		return finalNativeAutomaticCallEvidence(commit, operation, intent.ApplicationBlock)
	default:
		return FinalNativeCallEvidence{}, fmt.Errorf("unsupported native intent operation %q", operation)
	}
}
