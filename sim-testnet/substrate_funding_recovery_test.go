// Native-funding recovery tests prove that only exact executor-shaped balance
// transfers may cross a plan revision after interrupted postcondition writing.
package main

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"maps"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

// Build a compact synthetic signed-version-4 wire image whose only variable
// field under test is the randomized sr25519 signature.
func testSignedSubstrateEnvelope(t *testing.T) []byte {
	t.Helper()
	body := make([]byte, 1+1+32+1+64+12)
	body[0] = 0x84
	body[1] = 0
	for index := 0; index < 32; index++ {
		body[2+index] = byte(index + 1)
	}
	body[34] = 1
	for index := 0; index < 64; index++ {
		body[35+index] = byte(index + 65)
	}
	for index := 0; index < 12; index++ {
		body[99+index] = byte(index + 129)
	}
	raw := make([]byte, 2+len(body))
	binary.LittleEndian.PutUint16(raw[:2], uint16(len(body)<<2)|1)
	copy(raw[2:], body)
	return raw
}

// Every deterministic byte must match while two valid randomized signatures
// for the same payload are allowed to differ.
func TestSubstrateFundingRecoveryComparesOnlyCanonicalRandomizedSignature(t *testing.T) {
	expected := testSignedSubstrateEnvelope(t)
	actual := append([]byte(nil), expected...)
	start, end, err := signedSubstrateSignatureRange(actual)
	if err != nil || end-start != 64 {
		t.Fatalf("canonical signature range=%d:%d error=%v", start, end, err)
	}
	for index := start; index < end; index++ {
		actual[index] ^= 0xff
	}
	if err := validateSignedSubstrateCallMatches(actual, expected); err != nil {
		t.Fatalf("randomized signature bytes were rejected: %v", err)
	}
	for _, index := range []int{2, 3, 10, end, len(actual) - 1} {
		mutated := append([]byte(nil), actual...)
		mutated[index] ^= 0xff
		if err := validateSignedSubstrateCallMatches(mutated, expected); err == nil {
			t.Errorf("deterministic envelope mutation at byte %d was accepted", index)
		}
	}
	wrongSignatureType := append([]byte(nil), actual...)
	wrongSignatureType[start-1] = 0
	if err := validateSignedSubstrateCallMatches(wrongSignatureType, expected); err == nil {
		t.Fatal("non-sr25519 signature was accepted")
	}
	wrongLength := append([]byte(nil), actual...)
	wrongLength[0] ^= 4
	if err := validateSignedSubstrateCallMatches(wrongLength, expected); err == nil {
		t.Fatal("noncanonical compact length was accepted")
	}
	if err := validateSignedSubstrateCallMatches(actual[:len(actual)-1], expected); err == nil {
		t.Fatal("truncated signed envelope was accepted")
	}
}

// Cover every executor funding family, both head/challenger target variants,
// and the canonical numeric namespace.
func TestSubstrateFundingRecoveryResolvesEveryExecutorFundingRole(t *testing.T) {
	cfg := testResolvedConfig(t)
	tests := []struct {
		actionID string
		label    string
		target   string
	}{
		{actionID: "churn.fund.1", label: churnColdkeyLabel(1), target: "churn-coldkey:1"},
		{actionID: "fleet.fund.167", label: fleetColdkeyLabel(167), target: "head-fleet-coldkey:167"},
		{actionID: "fleet.fund.201", label: fleetColdkeyLabel(201), target: "challenger-fleet-coldkey:201"},
		{actionID: "fleet.fund-hotkey.167", label: fleetHotkeyLabel(167), target: "head-fleet-hotkey:167"},
		{actionID: "fleet.fund-hotkey.201", label: fleetHotkeyLabel(201), target: "challenger-fleet-hotkey:201"},
		{actionID: "validator.fund.2", label: "validator-2-coldkey", target: "validator-coldkey:2"},
	}
	for _, test := range tests {
		label, target, err := substrateFundingRole(cfg, test.actionID)
		if err != nil || label != test.label || target != test.target {
			t.Errorf("%s resolved label=%q target=%q error=%v", test.actionID, label, target, err)
		}
	}
	for _, actionID := range []string{
		"churn.fund.0", "churn.fund.01", "churn.fund.48",
		"fleet.fund.0", "fleet.fund.0201", "fleet.fund.203",
		"fleet.fund-hotkey.0", "fleet.fund-hotkey.0201", "fleet.fund-hotkey.203",
		"validator.fund.0", "validator.fund.3", "wallet.fund.1",
	} {
		if _, _, err := substrateFundingRole(cfg, actionID); err == nil {
			t.Errorf("noncanonical substrate-funding action %q was accepted", actionID)
		}
	}
}

// Reproduce the live failure shape: a latest plan rewired an action while the
// pending transfer's archived source plan retained the exact signed intent.
func TestSubstrateFundingRecoveryUsesExactTransactionSourceAction(t *testing.T) {
	cfg := testResolvedConfig(t)
	action := Action{
		ID: "fleet.fund-hotkey.167", Kind: "substrate-extrinsic", Target: "head-fleet-hotkey:167",
		Parameters: map[string]string{"maximum_fee_rao": "6000000", "keep_alive_reserve_rao": "500"},
		Spend:      Spend{TAORao: 6_000_500}, DependsOn: []string{"fleet.register.167"},
	}
	var err error
	action.IntentHash, err = actionIntentHash(action)
	if err != nil {
		t.Fatal(err)
	}
	source := &SetupPlan{PlanHash: "0x" + strings.Repeat("11", 32), Actions: []Action{action}}
	got, label, err := exactSubstrateFundingPlanAction(cfg, source, action.ID, action.IntentHash)
	if err != nil || got.IntentHash != action.IntentHash || label != fleetHotkeyLabel(167) {
		t.Fatalf("exact source funding action was rejected: action=%+v label=%q error=%v", got, label, err)
	}

	mutations := []Action{action, action, action, action, action}
	mutations[0].Kind = "substrate-read"
	mutations[1].Target = "challenger-fleet-hotkey:167"
	mutations[2].Spend.AlphaRao = 1
	mutations[3].Spend.EVMGasWei = DecimalUint("1")
	mutations[4].Spend.TAORao = 0
	for index, mutation := range mutations {
		mutation.IntentHash = action.IntentHash
		plan := &SetupPlan{PlanHash: source.PlanHash, Actions: []Action{mutation}}
		if _, _, err := exactSubstrateFundingPlanAction(cfg, plan, action.ID, action.IntentHash); err == nil {
			t.Errorf("unsafe source-action mutation %d was accepted", index)
		}
	}
	duplicate := &SetupPlan{PlanHash: source.PlanHash, Actions: []Action{action, action}}
	if _, _, err := exactSubstrateFundingPlanAction(cfg, duplicate, action.ID, action.IntentHash); err == nil {
		t.Fatal("duplicate exact source actions were accepted")
	}
	if _, _, err := exactSubstrateFundingPlanAction(cfg, source, action.ID, "0x"+strings.Repeat("22", 32)); err == nil {
		t.Fatal("sibling source intent was accepted")
	}
}

// Require the historical delta and both no-broadcast balance checkpoints.
func TestSubstrateFundingRecoveryRequiresExactConvergentBalances(t *testing.T) {
	action := Action{Spend: Spend{TAORao: 6_000_500}}
	transfer, err := validateSubstrateFundingRecoveryBalances(action, 3_000_500, 6_000_500, 6_000_500)
	if err != nil || transfer != 3_000_000 {
		t.Fatalf("exact funding balances produced transfer=%d error=%v", transfer, err)
	}
	tests := []struct {
		action                                  Action
		recoveryBalance, inclusionBalance, live uint64
	}{
		{action: Action{}, recoveryBalance: 1, inclusionBalance: 1, live: 1},
		{action: action, recoveryBalance: 6_000_500, inclusionBalance: 6_000_500, live: 6_000_500},
		{action: action, recoveryBalance: 6_000_501, inclusionBalance: 6_000_501, live: 6_000_501},
		{action: action, recoveryBalance: 3_000_500, inclusionBalance: 6_000_499, live: 6_000_500},
		{action: action, recoveryBalance: 3_000_500, inclusionBalance: 6_000_500, live: 6_000_499},
	}
	for index, test := range tests {
		if _, err := validateSubstrateFundingRecoveryBalances(test.action, test.recoveryBalance, test.inclusionBalance, test.live); err == nil {
			t.Errorf("unsafe funding balance mutation %d was accepted", index)
		}
	}
}

// Parse persisted JSON balances without accepting rounding, signs, overflow,
// or unrelated implementation-specific integer types.
func TestSubstrateFundingRecoveryObservedBalanceParsingFailsClosed(t *testing.T) {
	for _, value := range []any{float64(6_000_500), json.Number("6000500"), uint64(6_000_500)} {
		if parsed, err := substrateFundingObservedRao(value); err != nil || parsed != 6_000_500 {
			t.Errorf("valid observed balance %v parsed=%d error=%v", value, parsed, err)
		}
	}
	for _, value := range []any{float64(-1), float64(1.5), float64(math.MaxUint64), math.NaN(), math.Inf(1), json.Number("-1"), "6000500", int(6_000_500), nil} {
		if _, err := substrateFundingObservedRao(value); err == nil {
			t.Errorf("invalid observed balance %v was accepted", value)
		}
	}
}

// Persist an exact later dual-RPC postcondition and prove it closes only an
// earlier journal transaction, allowing dependent fee consumption afterward.
func TestSubstrateFundingRecoveryRecognizesOnlyOrderedVerifiedDescendant(t *testing.T) {
	cfg := testResolvedConfig(t)
	action := Action{
		ID: "fleet.fund-hotkey.167", Kind: "substrate-extrinsic", Target: "head-fleet-hotkey:167",
		Parameters: map[string]string{"maximum_fee_rao": "6000000", "keep_alive_reserve_rao": "500"},
		Spend:      Spend{TAORao: 6_000_500}, DependsOn: []string{"fleet.register.167"},
	}
	var err error
	action.IntentHash, err = actionIntentHash(action)
	if err != nil {
		t.Fatal(err)
	}
	sourceHash := "0x" + strings.Repeat("11", 32)
	currentHash := "0x" + strings.Repeat("22", 32)
	prior := &SetupPlan{PlanHash: currentHash, PriorPlanHashes: []string{sourceHash}, Actions: []Action{action}}
	var account [32]byte
	for index := range account {
		account[index] = byte(index + 1)
	}
	roleLabel := fleetHotkeyLabel(167)
	observed := map[string]any{
		"kind": action.Kind, "target": action.Target, "role": roleLabel,
		"account": "0x" + hex.EncodeToString(account[:]), "free_balance_rao": uint64(6_000_500),
	}
	record := ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: currentHash, ActionID: action.ID, IntentHash: action.IntentHash,
		OperationalRPCMode: cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(cfg),
		SubstrateFinalized: ChainHead{Number: 100, Hash: "0x" + strings.Repeat("31", 32)},
		EVMFinalized:       ChainHead{Number: 100, Hash: "0x" + strings.Repeat("32", 32)}, EVMHashDomain: "evm-rpc",
		Observed:                      observed,
		IndependentSubstrateFinalized: ChainHead{Number: 100, Hash: "0x" + strings.Repeat("31", 32)},
		IndependentEVMFinalized:       ChainHead{Number: 100, Hash: "0x" + strings.Repeat("32", 32)}, IndependentEVMHashDomain: "evm-rpc",
		IndependentObserved: observed,
	}
	stateDir := t.TempDir()
	path, err := postconditionRelativePath(currentHash, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := canonicalHashHex(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(stateDir, filepath.FromSlash(path)), record); err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{
		Sequence: 20, PlanHash: currentHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified,
		PostconditionHash: hash, PostconditionPath: path,
	}
	transaction := planRevisionTransaction{PlanHash: sourceHash, ActionID: action.ID, IntentHash: action.IntentHash, JournalSequence: 10}
	verified, err := verifiedSubstrateFundingDescendant(cfg, stateDir, prior, []JournalEntry{entry}, transaction, action, roleLabel, account)
	if err != nil || !verified {
		t.Fatalf("exact ordered funding descendant verified=%t error=%v", verified, err)
	}

	tooEarly := entry
	tooEarly.Sequence = transaction.JournalSequence
	if verified, err := verifiedSubstrateFundingDescendant(cfg, stateDir, prior, []JournalEntry{tooEarly}, transaction, action, roleLabel, account); err != nil || verified {
		t.Fatalf("unordered descendant marker was not ignored: verified=%t error=%v", verified, err)
	}
	foreign := entry
	foreign.PlanHash = "0x" + strings.Repeat("41", 32)
	if verified, err := verifiedSubstrateFundingDescendant(cfg, stateDir, prior, []JournalEntry{foreign}, transaction, action, roleLabel, account); err != nil || verified {
		t.Fatalf("foreign descendant marker was not ignored: verified=%t error=%v", verified, err)
	}
	wrongHash := entry
	wrongHash.PostconditionHash = "0x" + strings.Repeat("42", 32)
	if verified, err := verifiedSubstrateFundingDescendant(cfg, stateDir, prior, []JournalEntry{wrongHash}, transaction, action, roleLabel, account); err == nil || verified {
		t.Fatalf("unauthenticated descendant receipt was accepted: verified=%t error=%v", verified, err)
	}
	wrongObserved := maps.Clone(observed)
	wrongObserved["free_balance_rao"] = uint64(6_000_499)
	record.Observed = wrongObserved
	record.IndependentObserved = wrongObserved
	hash, err = canonicalHashHex(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(stateDir, filepath.FromSlash(path)), record); err != nil {
		t.Fatal(err)
	}
	entry.PostconditionHash = hash
	if verified, err := verifiedSubstrateFundingDescendant(cfg, stateDir, prior, []JournalEntry{entry}, transaction, action, roleLabel, account); err == nil || verified {
		t.Fatalf("underfunded descendant receipt was accepted: verified=%t error=%v", verified, err)
	}
	mutations := []struct {
		field string
		value any
	}{
		{field: "kind", value: "evm-read"},
		{field: "target", value: "head-fleet-hotkey:166"},
		{field: "unexpected", value: true},
	}
	for _, mutation := range mutations {
		mutated := maps.Clone(observed)
		mutated[mutation.field] = mutation.value
		record.Observed = mutated
		record.IndependentObserved = mutated
		hash, err = canonicalHashHex(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := writePublicJSON(filepath.Join(stateDir, filepath.FromSlash(path)), record); err != nil {
			t.Fatal(err)
		}
		entry.PostconditionHash = hash
		if verified, err := verifiedSubstrateFundingDescendant(cfg, stateDir, prior, []JournalEntry{entry}, transaction, action, roleLabel, account); err == nil || verified {
			t.Fatalf("mutated descendant field %s was accepted: verified=%t error=%v", mutation.field, verified, err)
		}
	}
}

// A recovery may cross the hash boundary only when the fresh plan retains the
// same full action intent, and neither action nor transaction is duplicated.
func TestSubstrateFundingRecoveryRequiresUnchangedRevision(t *testing.T) {
	action := Action{ID: "fleet.fund-hotkey.167", IntentHash: "0x" + strings.Repeat("11", 32)}
	recovery := finalizedSubstrateFundingRecovery{
		Transaction: planRevisionTransaction{
			ActionID: action.ID, IntentHash: action.IntentHash,
			TransactionHash: "0x" + strings.Repeat("22", 32), BlockNumber: 123, BlockHash: "0x" + strings.Repeat("23", 32),
		},
		Action: action, RoleLabel: fleetHotkeyLabel(167), Account: [32]byte{1}, TransferRao: 3_000_000,
	}
	revised := &SetupPlan{Actions: []Action{action}}
	if err := validateRevisedSubstrateFundingRecoveries(revised, []finalizedSubstrateFundingRecovery{recovery}); err != nil {
		t.Fatalf("unchanged funding recovery was rejected: %v", err)
	}
	changed := action
	changed.IntentHash = "0x" + strings.Repeat("33", 32)
	if err := validateRevisedSubstrateFundingRecoveries(&SetupPlan{Actions: []Action{changed}}, []finalizedSubstrateFundingRecovery{recovery}); err == nil {
		t.Fatal("changed funding action crossed the plan revision")
	}
	if err := validateRevisedSubstrateFundingRecoveries(revised, []finalizedSubstrateFundingRecovery{recovery, recovery}); err == nil {
		t.Fatal("duplicate funding recovery was accepted")
	}
	incomplete := recovery
	incomplete.TransferRao = 0
	if err := validateRevisedSubstrateFundingRecoveries(revised, []finalizedSubstrateFundingRecovery{incomplete}); err == nil {
		t.Fatal("incomplete funding recovery was accepted")
	}
}
