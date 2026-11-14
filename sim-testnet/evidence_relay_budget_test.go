//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

// Complete action identities make stage/ordering controls independent of
// production signing or transport fixtures.
func evidenceRelayBudgetTestAction(t *testing.T, slot byte) Action {
	t.Helper()
	action := Action{ID: fmt.Sprintf("%s%x", evidenceRelayActionPrefix, [32]byte{slot}), Kind: "evm-transaction", Target: common.Address{1}.Hex(), Spend: Spend{EVMGasWei: decimalUint64(10)}}
	var err error
	action.IntentHash, err = actionIntentHash(action)
	if err != nil {
		t.Fatal(err)
	}
	return action
}

func TestEvidenceRelayAdmissionCountsFailedAndFinalizedSlotsWithoutRefund(t *testing.T) {
	plan := common.Hash{1}.Hex()
	first, second, third := evidenceRelayBudgetTestAction(t, 1), evidenceRelayBudgetTestAction(t, 2), evidenceRelayBudgetTestAction(t, 3)
	entries := []JournalEntry{}
	for _, action := range []Action{first, second} {
		for _, stage := range []JournalStage{StageIntent, StageBroadcast, StageIncluded, StageFinalized, StageFailed, StageIntent} {
			entries = append(entries, JournalEntry{PlanHash: plan, ActionID: action.ID, IntentHash: action.IntentHash, Stage: stage})
		}
	}
	for _, action := range []Action{first, second} {
		count, retry, err := evidenceRelayAdmissionCount(entries, plan, action, 2)
		if err != nil || count != 2 || !retry {
			t.Fatalf("exact retry changed allocation: %d %t %v", count, retry, err)
		}
	}
	if _, _, err := evidenceRelayAdmissionCount(entries, plan, third, 2); err == nil {
		t.Fatal("failed/finalized history refunded an already admitted slot")
	}
}

func TestEvidenceRelayAdmissionRejectsOrphanedAndConflictingJournalHistory(t *testing.T) {
	plan := common.Hash{1}.Hex()
	action := evidenceRelayBudgetTestAction(t, 1)
	intent := JournalEntry{PlanHash: plan, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent}
	for _, stage := range []JournalStage{StageBroadcast, StageIncluded, StageFinalized, StageVerified, StageFailed} {
		orphan := intent
		orphan.Stage = stage
		if _, _, err := evidenceRelayAdmissionCount([]JournalEntry{orphan}, plan, action, 2); err == nil {
			t.Fatalf("orphaned stage %s was admitted", stage)
		}
	}
	for name, mutate := range map[string]func(*JournalEntry){
		"different intent": func(value *JournalEntry) { value.IntentHash = common.Hash{9}.Hex() },
		"malformed intent": func(value *JournalEntry) { value.IntentHash = "not-a-hash" },
		"malformed slot":   func(value *JournalEntry) { value.ActionID = evidenceRelayActionPrefix + "../escape" },
	} {
		changed := intent
		mutate(&changed)
		if _, _, err := evidenceRelayAdmissionCount([]JournalEntry{intent, changed}, plan, action, 2); err == nil {
			t.Fatalf("%s was admitted", name)
		}
	}
}

func TestEvidenceRelayMaximumGasRetainsFullWidthAndRequiresExplicitBounds(t *testing.T) {
	cfg := testResolvedConfig(t)
	if value, err := evidenceRelayMaximumGas(cfg); err != nil || !value.IsZero() {
		t.Fatal("legacy configuration acquired a relay budget", value, err)
	}
	cfg.Config.ValidatorEvidenceV2 = []validatorcomponent.ReleaseValidatorEvidenceV2Config{{ValidatorID: 1}}
	if _, err := evidenceRelayMaximumGas(cfg); err == nil {
		t.Fatal("missing relay allowance was defaulted")
	}
	cfg.Config.ValidatorEvidenceRelay = evidenceRelayConfig{MaxSlots: ^uint64(0), GasUnits: ^uint64(0)}
	want := new(big.Int).SetUint64(^uint64(0))
	want.Mul(want, new(big.Int).SetUint64(^uint64(0)))
	want.Mul(want, new(big.Int).SetUint64(cfg.Config.Budgets.MaximumEVMFeePerGasWei))
	actual, err := evidenceRelayMaximumGas(cfg)
	if err != nil || actual.String() != want.String() {
		t.Fatal("finite configuration product narrowed through uint64", actual, err)
	}
	for _, value := range []evidenceRelayConfig{{MaxSlots: 0, GasUnits: 21_000}, {MaxSlots: 1, GasUnits: 20_999}} {
		cfg.Config.ValidatorEvidenceRelay = value
		if _, err := evidenceRelayMaximumGas(cfg); err == nil {
			t.Fatalf("invalid bound accepted: %+v", value)
		}
	}
}

// Actual configured role signatures and the actual approved reserve drive
// the action admission; there is no injected signature/recovery verdict.
func evidenceRelayAdmissionTestFixture(t *testing.T) (*Executor, validatorcomponent.ValidatorEvidenceTransactionV2Expected) {
	t.Helper()
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	journal, err := OpenJournal(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	activation := fixture.prepared.Members[0].Activation
	domain, err := activation.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	window := protocol.ValidatorEvidenceWindow{Epoch: activation.Domain.Epoch, StartBlock: 300, EndBlock: 400, FinalizedBlock: 400}
	header := protocol.ValidatorEvidenceHeader{Domain: domain, Hotkey: activation.Hotkey, NoID: activation.NoID, Epoch: window.Epoch,
		Kind: protocol.ValidatorEvidenceClosedCensus, VPK: activation.VPK, BoundaryBlock: 399, BoundaryHash: [32]byte{7}, CensusHash: [32]byte{8}, PayloadHash: [32]byte{9}, PayloadBytes: 100}
	hotkey, key, err := runtimeEvidenceActivationKeysV2(fixture.roles, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	vpkSignature, err := header.SignVPK(key)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := header.Digest()
	if err != nil {
		t.Fatal(err)
	}
	hotkeySignature, err := hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	expected := validatorcomponent.ValidatorEvidenceTransactionV2Expected{Journal: fixture.plan.ValidatorEvidence.Address,
		RuntimeHash: [32]byte(fixture.plan.ValidatorEvidence.RuntimeCodeHash), Activation: activation, Window: window,
		Evidence: validatorcomponent.ValidatorEvidenceSignedV2{Schema: validatorcomponent.ValidatorEvidenceSignedV2Schema, Header: header, VPKSignature: vpkSignature, HotkeySignature: hotkeySignature}, MaxTransactionBytes: 64 * 1024, MaxReceiptLogs: 1024}
	return &Executor{cfg: fixture.cfg, plan: fixture.plan, journal: journal, roles: fixture.roles, stateDir: fixture.stateDir}, expected
}

func TestEvidenceRelayAdmissionRetainsExactSignedRequestAcrossJournalReopen(t *testing.T) {
	executor, expected := evidenceRelayAdmissionTestFixture(t)
	action, err := executor.admitEvidenceRelayAction(context.Background(), expected)
	if err != nil {
		t.Fatal(err)
	}
	entries := executor.journal.Entries()
	if len(entries) != 1 || entries[0].Stage != StageIntent || entries[0].IntentHash != action.IntentHash {
		t.Fatal("exact budget intent was not retained")
	}
	if err := executor.journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenJournal(executor.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	executor.journal = reopened
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	again, err := executor.admitEvidenceRelayAction(context.Background(), expected)
	if err != nil || again.IntentHash != action.IntentHash || len(reopened.Entries()) != 1 {
		t.Fatal("exact restart consumed a new allowance", err)
	}
	slot, _ := expected.Evidence.Header.SlotKey()
	path := filepath.Join(executor.stateDir, "evidence-relay", fmt.Sprintf("%x.json", slot))
	if _, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(context.Background(), path, evidenceRelayActionBytes); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceRelayAdmissionRejectsChangedReserveAndSignatureBeforeJournalMutation(t *testing.T) {
	executor, expected := evidenceRelayAdmissionTestFixture(t)
	for index := range executor.plan.Actions {
		if executor.plan.Actions[index].ID == evidenceRelayReserveId {
			original := executor.plan.Actions[index].Parameters["maximum_slots"]
			executor.plan.Actions[index].Parameters["maximum_slots"] = "999999"
			if _, err := executor.admitEvidenceRelayAction(context.Background(), expected); err == nil {
				t.Fatal("reserve content could retain a stale intent hash")
			}
			executor.plan.Actions[index].Parameters["maximum_slots"] = original
		}
	}
	expected.Evidence.HotkeySignature[0] ^= 1
	if _, err := executor.admitEvidenceRelayAction(context.Background(), expected); err == nil {
		t.Fatal("changed source signature consumed allowance")
	}
	if len(executor.journal.Entries()) != 0 {
		t.Fatal("refused authority changed the durable journal")
	}
}

func TestEvidenceRelayShutdownRetainsErrorsAdjacentToCancellation(t *testing.T) {
	failure := errors.New("canonical receipt changed")
	if err := evidenceRelayNonCancellationError(fmt.Errorf("request: %w", context.Canceled)); err != nil {
		t.Fatal(err)
	}
	actual := evidenceRelayNonCancellationError(errors.Join(fmt.Errorf("request: %w", context.Canceled), fmt.Errorf("readback: %w", failure)))
	if !errors.Is(actual, failure) || strings.Contains(actual.Error(), "context canceled") {
		t.Fatal("shutdown discarded an adjacent failure or kept cancellation", actual)
	}
	if !errors.Is(evidenceRelayNonCancellationError(context.DeadlineExceeded), context.DeadlineExceeded) {
		t.Fatal("shutdown hid a real request timeout")
	}
}
