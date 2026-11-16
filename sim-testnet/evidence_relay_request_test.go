//go:build linux || darwin

// Real role signatures and the durable admission writer reproduce the dynamic
// action capture boundary without live network or scheduling assumptions.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

// Reads the exact file emitted by the production admission path.
func evidenceRelayRequestTestFixture(t *testing.T) (*Executor, Action, []byte) {
	t.Helper()
	executor, expected := evidenceRelayAdmissionTestFixture(t)
	action, err := executor.admitEvidenceRelayAction(t.Context(), expected)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimPrefix(action.ID, evidenceRelayActionPrefix) + ".json"
	raw, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(t.Context(), filepath.Join(executor.stateDir, "evidence-relay", name), evidenceRelayActionBytes)
	if err != nil {
		t.Fatal(err)
	}
	return executor, action, raw
}

func TestEvidenceRelayHistoricalCaptureAdmitsOriginalSignedJournalRequest(t *testing.T) {
	executor, action, raw := evidenceRelayRequestTestFixture(t)
	finalized := JournalEntry{DeploymentID: executor.plan.DeploymentID, PlanHash: executor.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, BlockNumber: 41, BlockHash: common.Hash{0x51}.Hex(), TransactionHash: common.Hash{0x52}.Hex()}
	if err := executor.journal.Append(finalized); err != nil {
		t.Fatal(err)
	}
	deployment := executor.plan.Deployment
	deployment.DeployBlock = 100
	batcher := common.Address{0x91}
	plans := map[string]*SetupPlan{executor.plan.PlanHash: executor.plan}
	entries := executor.journal.Entries()
	if _, err := finalCaptureReleaseContractCensusForLineage(executor.plan, &deployment, batcher, plans, entries); err == nil {
		t.Fatal("legacy census accepted an unknown dynamic action without its original request")
	}
	requests := map[evidenceRelayRequestKey][]byte{{planHash: executor.plan.PlanHash, actionId: action.ID}: raw}
	census, err := finalCaptureReleaseContractCensusWithRelayRequests(executor.plan, &deployment, batcher, plans, entries, requests)
	if err != nil || census.fromBlock != 41 {
		t.Fatal("exact funded action could not define the capture floor", census, err)
	}
	fromState, err := finalCaptureReleaseContractCensusFromStateContext(t.Context(), executor.stateDir, executor.plan, &deployment, batcher)
	if err != nil || !finalJSONEqual(fromState.releaseAddresses, census.releaseAddresses) || fromState.fromBlock != census.fromBlock {
		t.Fatal("live state read and pure archive admission disagree", err)
	}
	if finalHistoricalCaptureContains(census.releaseAddresses, executor.plan.ValidatorEvidence.Address) {
		t.Fatal("relay request silently changed the separate companion emitter census")
	}
}

func TestEvidenceRelayRequestRejectsRehashedActionsAndChangedSource(t *testing.T) {
	executor, _, raw := evidenceRelayRequestTestFixture(t)
	for name, mutate := range map[string]func(*evidenceRelayRequestRecord){
		"target":               func(value *evidenceRelayRequestRecord) { value.Action.Target = common.Address{0x92}.Hex() },
		"spend":                func(value *evidenceRelayRequestRecord) { value.Action.Spend.EVMGasWei = decimalUint64(1) },
		"dependency":           func(value *evidenceRelayRequestRecord) { value.Action.DependsOn = nil },
		"parameter":            func(value *evidenceRelayRequestRecord) { value.Action.Parameters["unapproved"] = "true" },
		"gas bound":            func(value *evidenceRelayRequestRecord) { value.Evidence.MaxGas++ },
		"fee bound":            func(value *evidenceRelayRequestRecord) { value.Evidence.MaxFeePerGas++ },
		"source signature":     func(value *evidenceRelayRequestRecord) { value.Evidence.Evidence.HotkeySignature[0] ^= 1 },
		"path signature":       func(value *evidenceRelayRequestRecord) { value.Evidence.Evidence.VPKSignature[0] ^= 1 },
		"plan owner":           func(value *evidenceRelayRequestRecord) { value.PlanHash = common.Hash{0x93}.Hex() },
		"borrowed transaction": func(value *evidenceRelayRequestRecord) { value.Evidence.SignedTransaction = []byte{1} },
		"borrowed signer":      func(value *evidenceRelayRequestRecord) { value.Evidence.Relayer = common.Address{0x94} },
	} {
		var record evidenceRelayRequestRecord
		if err := decodeStrictJSONBytes(raw, &record); err != nil {
			t.Fatal(err)
		}
		mutate(&record)
		var err error
		record.Action.IntentHash, err = actionIntentHash(record.Action)
		if err != nil {
			t.Fatal(err)
		}
		changed, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		entries := executor.journal.Entries()
		for index := range entries {
			entries[index].IntentHash = record.Action.IntentHash
		}
		if _, err := validateEvidenceRelayRequest(executor.plan, entries, changed); err == nil {
			t.Fatalf("%s survived exact action reconstruction", name)
		}
	}
	for _, changed := range [][]byte{append(append([]byte(nil), raw...), '\n'), bytes.Replace(raw, []byte(`"schema":`), []byte(`"schema":"urnetwork-sim-evidence-relay-action-v2","schema":`), 1)} {
		if _, err := validateEvidenceRelayRequest(executor.plan, executor.journal.Entries(), changed); err == nil {
			t.Fatal("noncanonical immutable request was accepted")
		}
	}
}

// The gas reserve cannot acquire native principal or registration authority
// merely by recomputing its local intent hash.
func TestEvidenceRelayPlanAllowanceRejectsRehashedNativeSpend(t *testing.T) {
	executor, _, _ := evidenceRelayRequestTestFixture(t)
	original, err := exactPlanActionByID(executor.plan, evidenceRelayReserveId)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := evidenceRelayPlanAllowance(executor.plan, original); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Spend){
		func(value *Spend) { value.TAORao = 1 },
		func(value *Spend) { value.AlphaRao = 1 },
		func(value *Spend) { value.Registrations = 1 },
		func(value *Spend) { value.SubnetCreations = 1 },
	} {
		changed := original
		mutate(&changed.Spend)
		changed.IntentHash, err = actionIntentHash(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := evidenceRelayPlanAllowance(executor.plan, changed); err == nil {
			t.Fatal("rehashing the gas reserve admitted unrelated spend authority")
		}
	}
}

func TestEvidenceRelayRequestRejectsMissingForeignAndOverdrawnAdmission(t *testing.T) {
	executor, action, raw := evidenceRelayRequestTestFixture(t)
	original := executor.journal.Entries()
	finalized := original[0]
	finalized.Stage = StageFinalized
	finalized.BlockNumber = 1
	foreign := original[0]
	foreign.DeploymentID = "different-deployment"
	conflict := original[0]
	conflict.IntentHash = common.Hash{0x95}.Hex()
	for _, entries := range [][]JournalEntry{nil, {finalized}, {foreign}, {original[0], conflict}} {
		if _, err := validateEvidenceRelayRequest(executor.plan, entries, raw); err == nil {
			t.Fatal("incomplete or conflicting budget admission was accepted")
		}
	}
	reserve, err := exactPlanActionByID(executor.plan, evidenceRelayReserveId)
	if err != nil {
		t.Fatal(err)
	}
	_, _, maximum, err := evidenceRelayPlanAllowance(executor.plan, reserve)
	if err != nil || maximum > 1000 {
		t.Fatal("test allowance is not a small finite fixture", err)
	}
	entries := append([]JournalEntry(nil), original...)
	for index := uint64(0); index < maximum; index++ {
		other := action
		other.ID = evidenceRelayActionPrefix + stringsTrim0x(common.BigToHash(new(big.Int).SetUint64(index+1)).Hex())
		other.IntentHash, err = actionIntentHash(other)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, JournalEntry{DeploymentID: executor.plan.DeploymentID, PlanHash: executor.plan.PlanHash, ActionID: other.ID, IntentHash: other.IntentHash, Stage: StageIntent})
	}
	if _, err := validateEvidenceRelayRequest(executor.plan, entries, raw); err == nil {
		t.Fatal("overdrawn aggregate allowance was accepted")
	}
}

func TestEvidenceRelayHistoricalCaptureKeepsPredecessorOwnershipAndFailedRequests(t *testing.T) {
	executor, action, raw := evidenceRelayRequestTestFixture(t)
	entries := executor.journal.Entries()
	failed := entries[0]
	failed.Stage = StageFailed
	entries = append(entries, failed)
	current := *executor.plan
	current.PlanHash = common.Hash{0x96}.Hex()
	current.PriorPlanHashes = []string{executor.plan.PlanHash}
	plans := map[string]*SetupPlan{current.PlanHash: &current, executor.plan.PlanHash: executor.plan}
	key := evidenceRelayRequestKey{planHash: executor.plan.PlanHash, actionId: action.ID}
	requests := map[evidenceRelayRequestKey][]byte{key: raw}
	if _, err := evidenceRelayRequestActions(plans, entries, requests); err != nil {
		t.Fatal("failed predecessor request lost its durable debit", err)
	}
	if _, err := evidenceRelayRequestActions(plans, entries, nil); err == nil {
		t.Fatal("failed request disappeared from the source census")
	}
	wrong := evidenceRelayRequestKey{planHash: current.PlanHash, actionId: action.ID}
	if _, err := evidenceRelayRequestActions(plans, entries, map[evidenceRelayRequestKey][]byte{wrong: raw}); err == nil {
		t.Fatal("predecessor request was relabeled as current")
	}
	entries[1].Stage, entries[1].BlockNumber = StageFinalized, 41
	deployment := current.Deployment
	deployment.DeployBlock = 100
	if census, err := finalCaptureReleaseContractCensusWithRelayRequests(&current, &deployment, common.Address{0x91}, plans, entries, requests); err != nil || census.fromBlock != 41 {
		t.Fatal("authenticated predecessor relay prevented later phase capture", err)
	}
}

func TestEvidenceRelayHistoricalCaptureRejectsPathEscapeBeforeReadAndHonorsCancellation(t *testing.T) {
	executor, action, raw := evidenceRelayRequestTestFixture(t)
	plans := map[string]*SetupPlan{executor.plan.PlanHash: executor.plan}
	entries := executor.journal.Entries()
	entries[0].ActionID = evidenceRelayActionPrefix + "../outside"
	reads := 0
	if _, err := evidenceRelayRequestsForJournal(plans, entries, func(evidenceRelayRequestKey) ([]byte, error) { reads++; return raw, nil }); err == nil || reads != 0 {
		t.Fatal("malformed journal slot reached source reader", reads, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := evidenceRelayRequestsFromState(ctx, executor.stateDir, plans, executor.journal.Entries()); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled capture performed a new state read", err)
	}
	if _, err := finalCaptureReleaseContractCensusFromStateContext(ctx, filepath.Join(executor.stateDir, "absent"), executor.plan, &executor.plan.Deployment, common.Address{0x91}); !errors.Is(err, context.Canceled) {
		t.Fatal("live census opened state before observing cancellation", err)
	}
	if _, err := evidenceRelayRequestsForJournal(plans, executor.journal.Entries(), func(key evidenceRelayRequestKey) ([]byte, error) {
		if key.actionId != action.ID {
			t.Fatal("reader selected another slot")
		}
		return nil, errors.New("missing original request")
	}); err == nil {
		t.Fatal("missing original request became a prefix-only action")
	}
}
