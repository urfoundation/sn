package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ss58"
	validatorpkg "github.com/urfoundation/sn/validator"
)

func TestFinalSemanticArchiveActionPostconditionUsesExactV4JournalObject(t *testing.T) {
	actionID := "fleet.register.201"
	planHash := finalTestHex(0x21)
	path, err := postconditionRelativePath(planHash, actionID)
	if err != nil {
		t.Fatal(err)
	}
	observed := map[string]any{"role": "fleet-201-hotkey", "uid": uint64(1200), "replaced_uid": uint64(1200), "replaced_churn": uint64(1)}
	record := &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: "deployment-v4", PlanHash: planHash,
		ActionID: actionID, IntentHash: finalTestHex(0x22), OperationalRPCMode: rpcModePrivateAuthority, IndependentRPC: true,
		SubstrateFinalized: ChainHead{Number: 90, Hash: finalTestHex(90)}, EVMFinalized: ChainHead{Number: 96, Hash: finalTestHex(96)}, EVMHashDomain: "evm-rpc", Observed: observed,
		IndependentSubstrateFinalized: ChainHead{Number: 91, Hash: finalTestHex(91)}, IndependentEVMFinalized: ChainHead{Number: 97, Hash: finalTestHex(97)},
		IndependentEVMHashDomain: "evm-rpc", IndependentObserved: observed,
	}
	recordBytes, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	durable, err := decodeActionPostcondition(recordBytes)
	if err != nil {
		t.Fatal(err)
	}
	postconditionHash, err := canonicalHashHex(durable)
	if err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{
		Schema: "urnetwork-sim-journal-v1", Sequence: 1, DeploymentID: record.DeploymentID, PlanHash: record.PlanHash,
		ActionID: record.ActionID, IntentHash: record.IntentHash, Stage: StageVerified, PostconditionHash: postconditionHash, PostconditionPath: path,
	}
	journalBytes, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	archive := &finalSemanticArchive{files: map[string][]byte{"launch-foundation/journal.jsonl": append(journalBytes, '\n'), path: recordBytes}}
	got, exact, err := archive.actionPostcondition(actionID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(exact, recordBytes) || !finalJSONEqual(got, durable) {
		t.Fatal("journal-selected v4 postcondition changed during strict decoding")
	}
	duplicated := bytes.Replace(recordBytes, []byte(`{"schema":`), []byte(`{"schema":"urnetwork-sim-action-postcondition-v4","schema":`), 1)
	archive.files[path] = duplicated
	if _, _, err := archive.actionPostcondition(actionID); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate v4 JSON field was accepted: %v", err)
	}

	incomplete := map[string]any{
		"schema": record.Schema, "deployment_id": record.DeploymentID, "plan_hash": record.PlanHash,
		"action_id": record.ActionID, "intent_hash": record.IntentHash, "substrate_finalized": record.SubstrateFinalized,
		"evm_finalized": record.EVMFinalized, "observed": record.Observed,
	}
	archive.files[path], err = json.Marshal(incomplete)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := archive.actionPostcondition(actionID); err == nil || !strings.Contains(err.Error(), "wire fields") {
		t.Fatalf("incomplete duplicate postcondition wire shape was accepted: %v", err)
	}

	var missingBoolean map[string]any
	if err := json.Unmarshal(recordBytes, &missingBoolean); err != nil {
		t.Fatal(err)
	}
	delete(missingBoolean, "independent_rpc")
	missingBooleanBytes, err := json.Marshal(missingBoolean)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeFinalActionPostconditionV4(missingBooleanBytes); err == nil || !strings.Contains(err.Error(), "wire field") {
		t.Fatalf("v4 postcondition with an omitted false-capable field was accepted: %v", err)
	}
}

func TestFinalSemanticArchiveAdjacentPersistedWiresMatchProductionWriters(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	identityBytes, err := json.Marshal(roles.Public())
	if err != nil {
		t.Fatal(err)
	}
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	planBytes, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if persisted, decodeErr := decodePersistedPlanBytes(planBytes); decodeErr != nil || persisted.PlanHash != plan.PlanHash {
		t.Fatalf("production setup plan wire was rejected: %v", decodeErr)
	}
	archive := &finalSemanticArchive{files: map[string][]byte{
		"launch-foundation/plan.json": planBytes,
		"public/identities.json":      identityBytes,
	}}
	if got, err := archive.planHash(); err != nil || got != strings.ToLower(plan.PlanHash) {
		t.Fatalf("exact persisted setup plan hash = %q, %v", got, err)
	}
	identities, err := archive.identities()
	if err != nil {
		t.Fatal(err)
	}
	if identities.DeploymentID != roles.DeploymentID || len(identities.EVM) != len(roles.EVM) || len(identities.Substrate) != len(roles.Substrate) || len(identities.Clients) != len(roles.Clients) {
		t.Fatal("typed public identity wire differs from RoleSecrets.Public")
	}
	var drifted map[string]any
	if err := json.Unmarshal(planBytes, &drifted); err != nil {
		t.Fatal(err)
	}
	drifted["unrecognized_release_field"] = true
	archive.files["launch-foundation/plan.json"], err = json.Marshal(drifted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archive.planHash(); err == nil {
		t.Fatal("drifted persisted setup-plan wire was accepted")
	}
}

func TestFinalSemanticBuilderDerivesNativeStartFromSealedAppliedIntents(t *testing.T) {
	archive, chain := finalSemanticBuilderNativeArchive(t)

	got, err := archive.nativeStartHead(chain)
	if err != nil {
		t.Fatal(err)
	}
	want := ChainHead{Number: 15, Hash: finalTestHex(15)}
	if got != want {
		t.Fatalf("native start=%+v, want earliest signed native snapshot %+v", got, want)
	}
	if got == (ChainHead{Number: 4, Hash: finalTestHex(4)}) || got == (ChainHead{Number: 99, Hash: finalTestHex(99)}) {
		t.Fatalf("native start reused an EVM checkpoint: %+v", got)
	}

	item := &archive.collected.Validators[0].Intents[0]
	var intent validatorpkg.SteeringIntent
	if err := decodeStrictJSONBytes(archive.files[item.Artifact.URI], &intent); err != nil {
		t.Fatal(err)
	}
	intent.NativeSnapshotHash = finalTestHex(0xee)
	tampered, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	archive.files[item.Artifact.URI] = tampered
	if _, err := archive.nativeStartHead(chain); err == nil || !strings.Contains(err.Error(), "conflicting hashes") {
		t.Fatalf("same-height native fork was not rejected: %v", err)
	}
}

func TestFinalSemanticSettlementAccountingBindsBothHeadsAndEventDeltas(t *testing.T) {
	vault := common.HexToAddress("0x1111111111111111111111111111111111111111")
	beforeHead := ChainHead{Number: 100, Hash: finalTestHex(0x10)}
	afterHead := ChainHead{Number: 200, Hash: finalTestHex(0x20)}
	before := &ContractView{
		FinalizedHead: beforeHead, TotalCaptured: "100", TotalPaid: "40", EscrowAccounted: "60",
		PendingFunding: "10", Outstanding: "50", LiveEscrowStake: "70", ConservationHolds: true,
	}
	after := &ContractView{
		FinalizedHead: afterHead, TotalCaptured: "170", TotalPaid: "90", EscrowAccounted: "80",
		PendingFunding: "20", Outstanding: "60", LiveEscrowStake: "95", ConservationHolds: true,
	}
	event := func(name string, block uint64, amount int64) finalSemanticEvent {
		return finalSemanticEvent{Name: name, Log: finalCanonicalEVMLog{Address: strings.ToLower(vault.Hex()), BlockNumber: block}, Args: map[string]any{"amount": big.NewInt(amount)}}
	}
	events := &finalSemanticEventIndex{byName: map[string][]finalSemanticEvent{
		"EmissionCaptured": {event("EmissionCaptured", 150, 70)},
		"ClaimPaid":        {event("ClaimPaid", 151, 50)},
	}}
	got, err := finalSemanticSettlementAccounting(before, after, beforeHead, afterHead, vault, events)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalCapturedDeltaRao != "70" || got.TotalPaidDeltaRao != "50" || got.EscrowAccountedDeltaRao != "20" || got.PendingFundingDeltaRao != "10" || got.OutstandingLiabilityDeltaRao != "10" || got.LiveEscrowStakeDeltaRao != "25" || got.EmissionCapturedEventRao != "70" || got.ClaimPaidEventRao != "50" {
		t.Fatalf("settlement accounting deltas differ: %+v", got)
	}

	badAmount := *events
	badAmount.byName = map[string][]finalSemanticEvent{
		"EmissionCaptured": {event("EmissionCaptured", 150, 69)},
		"ClaimPaid":        append([]finalSemanticEvent(nil), events.byName["ClaimPaid"]...),
	}
	if _, err := finalSemanticSettlementAccounting(before, after, beforeHead, afterHead, vault, &badAmount); err == nil || !strings.Contains(err.Error(), "event sums") {
		t.Fatalf("substituted EmissionCaptured sum was accepted: %v", err)
	}

	wrongVault := *events
	wrongVault.byName = map[string][]finalSemanticEvent{
		"EmissionCaptured": append([]finalSemanticEvent(nil), events.byName["EmissionCaptured"]...),
		"ClaimPaid":        {event("ClaimPaid", 151, 50)},
	}
	wrongVault.byName["ClaimPaid"][0].Log.Address = "0x2222222222222222222222222222222222222222"
	if _, err := finalSemanticSettlementAccounting(before, after, beforeHead, afterHead, vault, &wrongVault); err == nil || !strings.Contains(err.Error(), "settlement vault") {
		t.Fatalf("foreign ClaimPaid event was accepted: %v", err)
	}

	brokenEquation := *after
	brokenEquation.Outstanding = "59"
	if _, err := finalSemanticSettlementAccounting(before, &brokenEquation, beforeHead, afterHead, vault, events); err == nil || !strings.Contains(err.Error(), "exact global accounting") {
		t.Fatalf("broken terminal accounting equation was accepted: %v", err)
	}
}

func TestFinalSemanticBuilderBindsCompleteClosedCallInputs(t *testing.T) {
	result := ScenarioResult{RunID: "run-1", Name: "release-1.0", EvidenceHash: finalTestHex(1), ConfigHash: finalTestHex(2)}
	terminal := ScenarioObservation{ObservationHash: finalTestHex(3), PublicIdentityCount: 1004}
	history := []*ScenarioObservation{{ObservationHash: finalTestHex(4)}, &terminal}
	resultData, _ := json.Marshal(result)
	terminalData, _ := json.Marshal(terminal)
	historyData, _ := json.Marshal(history)
	archive := &finalSemanticArchive{
		collected: &FinalSemanticCollectedInputs{
			ScenarioResult:      FinalArtifactLocator{URI: "result"},
			TerminalObservation: FinalArtifactLocator{URI: "terminal"},
			ObservationHistory:  FinalArtifactLocator{URI: "history"},
		},
		files: map[string][]byte{"result": resultData, "terminal": terminalData, "history": historyData},
	}
	if err := archive.bindCallInputs(&result, &terminal, history); err != nil {
		t.Fatalf("exact closed inputs rejected: %v", err)
	}
	mutated := result
	mutated.ConfigHash = finalTestHex(9)
	if err := archive.bindCallInputs(&mutated, &terminal, history); err == nil {
		t.Fatal("modified result with the same run/result identity was accepted")
	}
	mutatedTerminal := terminal
	mutatedTerminal.PublicIdentityCount++
	if err := archive.bindCallInputs(&result, &mutatedTerminal, history); err == nil {
		t.Fatal("modified terminal observation with the same observation hash was accepted")
	}
	mutatedHistory := append([]*ScenarioObservation(nil), history...)
	copyEntry := *mutatedHistory[0]
	copyEntry.PublicIdentityCount = 1
	mutatedHistory[0] = &copyEntry
	if err := archive.bindCallInputs(&result, &terminal, mutatedHistory); err == nil {
		t.Fatal("modified observation history with the same hashes was accepted")
	}
}

func TestFinalSemanticBuilderUsesCleanAnomalyLedgerStatus(t *testing.T) {
	result := &ScenarioResult{Anomalies: &ScenarioAnomalyLedger{Status: "clean", Entries: []ScenarioAnomaly{}}}
	terminal := &ScenarioObservation{Status: &DeploymentStatus{Supervisor: &SupervisorState{Processes: []ProcessState{{ID: "validator-1", Healthy: true}}}}}
	restarts := []FinalProcessRestartEvidence{{ProcessID: "validator-1"}}
	if got := finalSemanticProcessAnomalyCount(result, terminal, restarts); got != 0 {
		t.Fatalf("clean terminal anomaly count=%d, want 0", got)
	}
	result.Anomalies.Status = "pass"
	if got := finalSemanticProcessAnomalyCount(result, terminal, restarts); got != 1 {
		t.Fatalf("unsupported anomaly status count=%d, want 1", got)
	}
	result.Anomalies.Status = "clean"
	terminal.Status.Supervisor.Processes[0].Restarts = 1
	restarts[0].ExpectedRestarts, restarts[0].ObservedRestarts, restarts[0].FaultIDs = 1, 1, []string{"scheduled"}
	if got := finalSemanticProcessAnomalyCount(result, terminal, restarts); got != 0 {
		t.Fatalf("attributed restart anomaly count=%d, want 0", got)
	}
	restarts[0].ObservedRestarts = 0
	if got := finalSemanticProcessAnomalyCount(result, terminal, restarts); got != 2 {
		t.Fatalf("restart mismatch anomaly count=%d, want 2", got)
	}
}

func finalSemanticBuilderRestartFault(id, target, role, identity string, beforePID, afterPID int, firstBlock uint64) ScenarioFaultRecord {
	return ScenarioFaultRecord{
		ID: id, Kind: "process-restart", Targets: []string{target}, TriggerBlock: firstBlock, RestoreBlock: firstBlock + 3,
		AppliedBlock: firstBlock + 1, AppliedBlockHash: finalTestHex(byte(firstBlock + 1)), RestoredBlock: firstBlock + 4, RestoredBlockHash: finalTestHex(byte(firstBlock + 4)),
		Processes:         []FaultProcessEvidence{{ID: target, Role: role, Identity: identity, PID: beforePID}},
		RestoredProcesses: []FaultProcessEvidence{{ID: target, Role: role, Identity: identity, PID: afterPID}}, Status: "restored",
	}
}

func finalSemanticBuilderRestartFixture(t *testing.T) (*finalSemanticArchive, *FinalSemanticEvidence, *ScenarioResult, *ScenarioObservation) {
	t.Helper()
	priorWindow := ScenarioAcceptanceWindow{FirstEpoch: 10, EpochCount: 1, StartBlock: 100, TerminalBlock: 199}
	priorFault := finalSemanticBuilderRestartFault("release-rolling-01", "validator-1", "validator", "validator:1", 101, 102, 110)
	prior := &ScenarioResult{
		Name: "release-1.0", RunID: "release-run", EvidenceHash: finalTestHex(0x31), Result: "pass", AcceptanceWindow: &priorWindow,
		Faults: []ScenarioFaultRecord{priorFault}, Assertions: []AssertionRecord{{ID: "fault_" + priorFault.ID, Passed: true}},
	}
	currentFault := finalSemanticBuilderRestartFault("production-rolling-01", "validator-1", "validator", "validator:1", 201, 202, 210)
	current := &ScenarioResult{
		Name: "production-soak", RunID: "production-run", EvidenceHash: finalTestHex(0x32), Result: "pass",
		Faults: []ScenarioFaultRecord{currentFault}, Assertions: []AssertionRecord{{ID: "fault_" + currentFault.ID, Passed: true}},
	}
	rotation := verifyKeyRotationEvidence{
		Schema: "urnetwork-verify-key-rotation-v1", DeploymentID: "deployment-1", PolicyHash: finalTestHex(0x33),
		Operators: []verifyKeyRotationOperator{{NoID: 1, OldPublicKey: finalTestHex(0x41), NewPublicKey: finalTestHex(0x42), BeforePID: 301, AfterPID: 302}},
	}
	priorBytes, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	rotationBytes, err := json.Marshal(rotation)
	if err != nil {
		t.Fatal(err)
	}
	archive := &finalSemanticArchive{
		collected: &FinalSemanticCollectedInputs{PriorPhase: &FinalCollectedPriorPhaseInputs{
			RunID: prior.RunID, ResultHash: prior.EvidenceHash, Window: priorWindow, ScenarioResult: FinalArtifactLocator{URI: "prior-result.json"},
		}},
		files: map[string][]byte{"prior-result.json": priorBytes, "public/verify-key-rotation.json": rotationBytes},
	}
	source := &FinalSemanticEvidence{
		Phase: current.Name, RunID: current.RunID, ResultHash: current.EvidenceHash, DeploymentID: rotation.DeploymentID, PolicyHash: rotation.PolicyHash, ExpectedOperators: 1,
	}
	terminal := &ScenarioObservation{Status: &DeploymentStatus{Supervisor: &SupervisorState{Processes: []ProcessState{
		{ID: "operator-1-api", Role: "operator-api", Identity: "no:1", PID: 402, Restarts: 1, Healthy: true},
		{ID: "unchanged", Role: "support", Identity: "support:1", PID: 403, Healthy: true},
		{ID: "validator-1", Role: "validator", Identity: "validator:1", PID: 404, Restarts: 2, Healthy: true},
	}}}}
	return archive, source, current, terminal
}

func TestFinalSemanticBuilderAttributesPriorCurrentAndVerifyKeyRestarts(t *testing.T) {
	archive, source, current, terminal := finalSemanticBuilderRestartFixture(t)
	rows, err := archive.processRestartEvidence(source, current, terminal)
	if err != nil {
		t.Fatal(err)
	}
	want := []FinalProcessRestartEvidence{
		{ProcessID: "operator-1-api", ExpectedRestarts: 1, ObservedRestarts: 1, FaultIDs: []string{"verify-key-rotation-1"}},
		{ProcessID: "unchanged"},
		{ProcessID: "validator-1", ExpectedRestarts: 2, ObservedRestarts: 2, FaultIDs: []string{"production-rolling-01", "release-rolling-01"}},
	}
	if !finalJSONEqual(rows, want) {
		t.Fatalf("restart rows=%+v, want %+v", rows, want)
	}

	terminal.Status.Supervisor.Processes[2].Restarts++
	if _, err := archive.processRestartEvidence(source, current, terminal); err == nil || !strings.Contains(err.Error(), "differs from 2 authenticated") {
		t.Fatalf("unattributed terminal restart was accepted: %v", err)
	}
}

func TestFinalSemanticBuilderRejectsMissingTamperedAndDuplicateRestartEvidence(t *testing.T) {
	t.Run("missing rotation", func(t *testing.T) {
		archive, source, current, terminal := finalSemanticBuilderRestartFixture(t)
		delete(archive.files, "public/verify-key-rotation.json")
		if _, err := archive.processRestartEvidence(source, current, terminal); err == nil || !strings.Contains(err.Error(), "verify-key rotation") {
			t.Fatalf("missing rotation evidence was accepted: %v", err)
		}
	})
	t.Run("duplicate rotation operator", func(t *testing.T) {
		archive, source, current, terminal := finalSemanticBuilderRestartFixture(t)
		var rotation verifyKeyRotationEvidence
		if err := decodeStrictJSONBytes(archive.files["public/verify-key-rotation.json"], &rotation); err != nil {
			t.Fatal(err)
		}
		rotation.Operators = append(rotation.Operators, rotation.Operators[0])
		archive.files["public/verify-key-rotation.json"], _ = json.Marshal(rotation)
		if _, err := archive.processRestartEvidence(source, current, terminal); err == nil || !strings.Contains(err.Error(), "differs from the production campaign") {
			t.Fatalf("duplicate rotation operator was accepted: %v", err)
		}
	})
	t.Run("tampered rotation identity", func(t *testing.T) {
		archive, source, current, terminal := finalSemanticBuilderRestartFixture(t)
		var rotation verifyKeyRotationEvidence
		if err := decodeStrictJSONBytes(archive.files["public/verify-key-rotation.json"], &rotation); err != nil {
			t.Fatal(err)
		}
		rotation.DeploymentID = "other-deployment"
		archive.files["public/verify-key-rotation.json"], _ = json.Marshal(rotation)
		if _, err := archive.processRestartEvidence(source, current, terminal); err == nil || !strings.Contains(err.Error(), "differs from the production campaign") {
			t.Fatalf("tampered rotation identity was accepted: %v", err)
		}
	})
	t.Run("duplicate campaign fault", func(t *testing.T) {
		archive, source, current, terminal := finalSemanticBuilderRestartFixture(t)
		var prior ScenarioResult
		if err := decodeStrictJSONBytes(archive.files["prior-result.json"], &prior); err != nil {
			t.Fatal(err)
		}
		current.Faults[0].ID = prior.Faults[0].ID
		current.Assertions[0].ID = "fault_" + current.Faults[0].ID
		if _, err := archive.processRestartEvidence(source, current, terminal); err == nil || !strings.Contains(err.Error(), "duplicated") {
			t.Fatalf("duplicate campaign fault identity was accepted: %v", err)
		}
	})
	t.Run("unchanged restart PID", func(t *testing.T) {
		archive, source, current, terminal := finalSemanticBuilderRestartFixture(t)
		current.Faults[0].RestoredProcesses[0].PID = current.Faults[0].Processes[0].PID
		if _, err := archive.processRestartEvidence(source, current, terminal); err == nil || !strings.Contains(err.Error(), "PID transition") {
			t.Fatalf("unchanged restart PID was accepted: %v", err)
		}
	})
}

func finalSemanticBuilderArchiveFixture(t *testing.T) (*finalSemanticArchive, *FinalSemanticEvidence, *FinalCollectedChainSnapshot, FinalArchiveRetentionPreflight, string) {
	t.Helper()
	cfg := testResolvedConfig(t)
	campaignStart := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	proxy := common.HexToAddress("0x1000000000000000000000000000000000000001")
	implementation := common.HexToAddress("0x2000000000000000000000000000000000000002")
	vault := common.HexToAddress("0x3000000000000000000000000000000000000003")
	reserve := common.HexToAddress("0x4000000000000000000000000000000000000004")
	public := PublicDeploymentManifest{
		Schema: "urnetwork-sim-public-deployment-v1", Release: "1.0", DeploymentID: "deployment-archive", GeneratedAt: campaignStart.Add(-48 * time.Hour).Format(time.RFC3339Nano),
		ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, PlanHash: finalTestHex(0x51),
		RuntimeSpec: cfg.Public.Chain.ExpectedRuntimeSpec, TransactionVersion: cfg.Public.Chain.ExpectedTransactionVersion, StateVersion: cfg.Public.Chain.ExpectedStateVersion,
		RuntimeCodeHash: cfg.Release.Runtime.CodeHash, RuntimeMetadataHash: cfg.Release.Runtime.MetadataHash,
		SubstrateRPC: "wss://substrate.example.test", EVMRPC: "https://evm.example.test", Topology: cfg.Config.Topology,
		Contracts: &ContractDeployment{Schema: "urnetwork-contract-deployment-v1", DeploymentID: "deployment-archive", CoordinatorProxy: proxy, CoordinatorImplementation: implementation, SettlementVault: vault, ReserveSink: reserve, DeployBlock: 100_000, DeployBlockHash: finalTestHex(0x52)},
	}
	publicBytes, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	publicHash, err := canonicalHashHex(&public)
	if err != nil {
		t.Fatal(err)
	}
	span, err := FinalCompositeArchiveSpan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	margin, err := finalArchiveReviewerSafetyMargin(cfg)
	if err != nil {
		t.Fatal(err)
	}
	futureDepth, ok := checkedAdd(span, margin)
	if !ok {
		t.Fatal("archive fixture depth overflow")
	}
	if futureDepth < minimumFinalArchiveProbeDepthBlocks {
		futureDepth = minimumFinalArchiveProbeDepthBlocks
	}
	earliest := uint64(100_000)
	finalized := uint64(110_000)
	requiredDepth := finalized - (earliest - futureDepth)
	receipt := FinalArchiveRetentionPreflight{
		Schema: finalArchiveRetentionPreflightSchema, GeneratedAt: campaignStart.Add(-time.Hour).Format(time.RFC3339Nano), DeploymentID: public.DeploymentID, PublicManifestHash: publicHash,
		PlannedSpanBlocks: span, SafetyMarginBlocks: margin, RequiredDepthBlocks: requiredDepth, Passed: true,
		Substrate: FinalArchiveProbeResult{
			Endpoint: public.SubstrateRPC, FinalizedHead: ChainHead{Number: finalized, Hash: finalTestHex(0x53)}, EarliestRequiredHead: ChainHead{Number: earliest, Hash: finalTestHex(0x54)}, HistoricalHead: ChainHead{Number: earliest - futureDepth, Hash: finalTestHex(0x55)}, RequiredDepthBlocks: requiredDepth,
			MetadataHash: "sha256:" + strings.Repeat("11", 32), EventsHash: "sha256:" + strings.Repeat("22", 32), ExactMetadataHash: "sha256:" + strings.Repeat("33", 32), ExactEventsHash: "sha256:" + strings.Repeat("44", 32),
		},
		EVM: FinalArchiveProbeResult{
			Endpoint: public.EVMRPC, FinalizedHead: ChainHead{Number: finalized, Hash: finalTestHex(0x56)}, EarliestRequiredHead: ChainHead{Number: earliest, Hash: finalTestHex(0x57)}, HistoricalHead: ChainHead{Number: earliest - futureDepth, Hash: finalTestHex(0x58)}, RequiredDepthBlocks: requiredDepth,
			GenericStateHash: finalTestHex(0x59), ExactStateHash: finalTestHex(0x5a), DeploymentHead: ChainHead{Number: public.Contracts.DeployBlock, Hash: public.Contracts.DeployBlockHash}, CodeHash: finalTestHex(0x5b), CallResultHash: finalTestHex(0x5c),
		},
	}
	receipt.EvidenceHash, err = finalArchiveRetentionPreflightHash(&receipt)
	if err != nil {
		t.Fatal(err)
	}
	receiptBytes, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	receiptBytes = append(receiptBytes, '\n')
	receiptPath := "receipts/archive-retention-preflight-" + strings.TrimPrefix(receipt.EvidenceHash, "0x") + ".json"
	archive := &finalSemanticArchive{
		cfg: cfg, runRoot: t.TempDir(), files: map[string][]byte{"launch-foundation/public.json": publicBytes, receiptPath: receiptBytes},
	}
	source := &FinalSemanticEvidence{
		CampaignStartedAt: campaignStart.Format(time.RFC3339Nano), DeploymentID: public.DeploymentID, PlanHash: public.PlanHash, ConfigHash: public.ConfigHash, PolicyHash: public.PolicyHash,
		GenesisHash: public.GenesisHash, ChainID: public.ChainID, Netuid: public.Netuid,
		Deployment: FinalContractDeploymentEvidence{CoordinatorProxy: proxy.Hex(), CoordinatorImplementation: implementation.Hex(), SettlementVault: vault.Hex(), ReserveSink: reserve.Hex()},
	}
	chain := &FinalCollectedChainSnapshot{EVMFromBlock: public.Contracts.DeployBlock}
	return archive, source, chain, receipt, receiptPath
}

func finalSemanticBuilderPutArchiveReceipt(t *testing.T, archive *finalSemanticArchive, receipt FinalArchiveRetentionPreflight) string {
	t.Helper()
	var err error
	receipt.EvidenceHash, err = finalArchiveRetentionPreflightHash(&receipt)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	wire = append(wire, '\n')
	path := "receipts/archive-retention-preflight-" + strings.TrimPrefix(receipt.EvidenceHash, "0x") + ".json"
	archive.files[path] = wire
	return path
}

func TestFinalSemanticBuilderSelectsLatestFreshArchiveRetentionReceipt(t *testing.T) {
	archive, source, chain, selected, selectedPath := finalSemanticBuilderArchiveFixture(t)
	older := selected
	older.GeneratedAt = time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	olderPath := finalSemanticBuilderPutArchiveReceipt(t, archive, older)
	if err := archive.buildArchiveRetention(source, chain); err != nil {
		t.Fatal(err)
	}
	if source.ArchiveRetention.EvidenceHash != selected.EvidenceHash || source.ArchiveRetention.PublicManifestHash != selected.PublicManifestHash {
		t.Fatalf("selected archive receipt=%+v, want latest %s", source.ArchiveRetention, selected.EvidenceHash)
	}
	if !strings.HasPrefix(source.ArchiveRetention.Artifact.URI, "final-derived/") || source.ArchiveRetention.Artifact.URI == selectedPath || source.ArchiveRetention.Artifact.URI == olderPath {
		t.Fatalf("archive receipt was not copied into self-contained derived evidence: %+v", source.ArchiveRetention.Artifact)
	}
	want := archive.files[selectedPath]
	got, err := os.ReadFile(filepath.Join(archive.runRoot, filepath.FromSlash(source.ArchiveRetention.Artifact.URI)))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("derived archive receipt differs from closed bytes: %v", err)
	}
}

func TestFinalSemanticBuilderRejectsUnboundOrStaleArchiveRetentionReceipt(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*finalSemanticArchive, *FinalSemanticEvidence, *FinalCollectedChainSnapshot, *FinalArchiveRetentionPreflight, string)
	}{
		{name: "missing", mutate: func(archive *finalSemanticArchive, _ *FinalSemanticEvidence, _ *FinalCollectedChainSnapshot, _ *FinalArchiveRetentionPreflight, path string) {
			delete(archive.files, path)
		}},
		{name: "wrong manifest", mutate: func(archive *finalSemanticArchive, _ *FinalSemanticEvidence, _ *FinalCollectedChainSnapshot, receipt *FinalArchiveRetentionPreflight, path string) {
			delete(archive.files, path)
			receipt.PublicManifestHash = finalTestHex(0xee)
			finalSemanticBuilderPutArchiveReceipt(t, archive, *receipt)
		}},
		{name: "stale", mutate: func(archive *finalSemanticArchive, _ *FinalSemanticEvidence, _ *FinalCollectedChainSnapshot, receipt *FinalArchiveRetentionPreflight, path string) {
			delete(archive.files, path)
			receipt.GeneratedAt = time.Date(2026, 9, 2, 11, 59, 59, 0, time.UTC).Format(time.RFC3339Nano)
			finalSemanticBuilderPutArchiveReceipt(t, archive, *receipt)
		}},
		{name: "wrong endpoint", mutate: func(archive *finalSemanticArchive, _ *FinalSemanticEvidence, _ *FinalCollectedChainSnapshot, receipt *FinalArchiveRetentionPreflight, path string) {
			delete(archive.files, path)
			receipt.EVM.Endpoint = "https://other.example.test"
			finalSemanticBuilderPutArchiveReceipt(t, archive, *receipt)
		}},
		{name: "insufficient configured span", mutate: func(archive *finalSemanticArchive, _ *FinalSemanticEvidence, _ *FinalCollectedChainSnapshot, receipt *FinalArchiveRetentionPreflight, path string) {
			delete(archive.files, path)
			receipt.PlannedSpanBlocks--
			finalSemanticBuilderPutArchiveReceipt(t, archive, *receipt)
		}},
		{name: "public manifest mutation", mutate: func(archive *finalSemanticArchive, _ *FinalSemanticEvidence, _ *FinalCollectedChainSnapshot, _ *FinalArchiveRetentionPreflight, _ string) {
			var public PublicDeploymentManifest
			if err := decodeStrictJSONBytes(archive.files["launch-foundation/public.json"], &public); err != nil {
				t.Fatal(err)
			}
			public.Commands = map[string]string{"analyze": "changed"}
			archive.files["launch-foundation/public.json"], _ = json.Marshal(public)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive, source, chain, receipt, path := finalSemanticBuilderArchiveFixture(t)
			test.mutate(archive, source, chain, &receipt, path)
			if err := archive.buildArchiveRetention(source, chain); err == nil {
				t.Fatal("unbound archive-retention receipt was accepted")
			}
		})
	}
}

func TestFinalSemanticBuilderDepositReceiptSelectsExactCumulativePrefix(t *testing.T) {
	logs := []finalCanonicalEVMLog{
		finalSemanticBuilderDepositLog(t, 1, 10, 40, 20, 0, 0x11),
		finalSemanticBuilderDepositLog(t, 1, 10, 60, 21, 0, 0x12),
		finalSemanticBuilderDepositLog(t, 1, 10, 25, 22, 0, 0x13),
	}
	index, err := indexFinalSemanticEvents(&FinalCollectedChainSnapshot{EVMLogs: logs})
	if err != nil {
		t.Fatal(err)
	}
	archive := &finalSemanticArchive{runRoot: t.TempDir()}
	receipt, err := archive.depositReceipt(index, validatorpkg.DepositAudit{NoID: 1, Epoch: 10, ObservedDepositRao: "100", ObservedAtBlock: 21}, "exact-prefix")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.TransactionHash != logs[1].TransactionHash || receipt.Block.Number != 21 || receipt.Status != "success" {
		t.Fatalf("receipt=%+v, want second cumulative deposit", receipt)
	}
	if _, err := archive.depositReceipt(index, validatorpkg.DepositAudit{NoID: 1, Epoch: 10, ObservedDepositRao: "99", ObservedAtBlock: 21}, "non-prefix"); err == nil || !strings.Contains(err.Error(), "does not equal") {
		t.Fatalf("non-prefix audit was accepted: %v", err)
	}
}

func finalSemanticBuilderDishonestDepositFixture(t *testing.T) (*finalSemanticArchive, *FinalSemanticEvidence, *ScenarioObservation, *finalSemanticEventIndex, finalCanonicalEVMLog) {
	t.Helper()
	cfg := testResolvedConfig(t)
	underAmount := cfg.Config.Scenarios.DishonestDepositRao
	recoveryAmount := underAmount * 4
	under := finalSemanticBuilderDepositLog(t, 2, 10, underAmount, 20, 0, 0x61)
	recovery := finalSemanticBuilderDepositLog(t, 2, 11, recoveryAmount, 30, 0, 0x62)
	index, err := indexFinalSemanticEvents(&FinalCollectedChainSnapshot{EVMLogs: []finalCanonicalEVMLog{under, recovery}})
	if err != nil {
		t.Fatal(err)
	}
	archive := &finalSemanticArchive{cfg: cfg, runRoot: t.TempDir()}
	recoveryReceipt, err := archive.receiptFromIndex(index, index.byName["Deposit"][1], "fixture-recovery")
	if err != nil {
		t.Fatal(err)
	}
	policyBytes := [32]byte{1}
	policyHash := "0x" + hex.EncodeToString(policyBytes[:])
	funder := common.HexToAddress("0x0000000000000000000000000000000000000042").Hex()
	source := &FinalSemanticEvidence{
		DeploymentID: "deployment-dishonest", PolicyHash: policyHash, Netuid: 521, ExpectedValidators: 2,
		EVMCampaignStartHead: ChainHead{Number: 10, Hash: finalTestHex(10)}, EVMTerminalHead: ChainHead{Number: 100, Hash: finalTestHex(100)}, NativeTerminalHead: ChainHead{Number: 100, Hash: finalTestHex(100)},
		Deployment: FinalContractDeploymentEvidence{CoordinatorProxy: common.HexToAddress(under.Address).Hex()},
		Pools:      []FinalPoolUIDEvidence{{NoID: 2, UID: 7, DepositSigner: funder}},
	}
	for validatorID := uint64(1); validatorID <= 2; validatorID++ {
		weight := uint16(0)
		if validatorID == 1 {
			weight = 123
		}
		source.Validators = append(source.Validators, FinalValidatorIdentityEvidence{
			ValidatorID: validatorID,
			Cycles: []FinalCRv4Cycle{{SettlementEpoch: 11, Pools: []FinalPoolWeightEvidence{{
				NoID: 2, UID: 7, RequiredDepositRao: strconv.FormatUint(recoveryAmount, 10), ObservedDepositRao: strconv.FormatUint(recoveryAmount, 10),
				AuditStatus: validatorpkg.DepositAuditCompliant, AuditCompliant: true, AuditDisposition: "pool_weight_eligible", ObservedAtBlock: 30, DepositReceipt: recoveryReceipt, AppliedWeight: weight,
			}}}},
		})
	}
	transaction := DishonestDepositTransactionEvidence{
		Schema: dishonestDepositTransactionV1, DeploymentID: source.DeploymentID, NoID: 2, Epoch: 10, AmountRao: strconv.FormatUint(underAmount, 10), Nonce: strconv.FormatUint(0x61, 10), Funder: funder, PolicyHash: policyHash,
		TransactionHash: under.TransactionHash, FinalizedBlock: under.BlockNumber, FinalizedBlockHash: under.BlockHash,
	}
	terminal := &ScenarioObservation{DishonestDepositValid: true, DishonestDeposit: &DishonestDepositEvidence{
		Schema: dishonestDepositEvidenceV1, DeploymentID: source.DeploymentID, Netuid: source.Netuid, Transaction: transaction,
	}}
	for validatorID := 1; validatorID <= 2; validatorID++ {
		terminal.DishonestDeposit.Validators = append(terminal.DishonestDeposit.Validators, DishonestDepositValidatorEvidence{
			ValidatorID: validatorID, VectorHash: finalTestHex(byte(0x70 + validatorID)), ApplicationBlock: uint64(40 + validatorID), ApplicationBlockHash: finalTestHex(byte(0x80 + validatorID)), PoolUID: 7,
			PoolMasked: validatorID == 2, Audit: validatorpkg.DepositAudit{NoID: 2, Epoch: 10, RequiredDepositRao: strconv.FormatUint(underAmount*2, 10), ObservedDepositRao: strconv.FormatUint(underAmount, 10), Status: validatorpkg.DepositAuditMismatch, Disposition: "zero_pool_weight"},
		})
	}
	return archive, source, terminal, index, recovery
}

func TestFinalSemanticBuilderBindsDishonestUnderpaymentToValidatorRecovery(t *testing.T) {
	archive, source, terminal, events, recovery := finalSemanticBuilderDishonestDepositFixture(t)
	receipts, err := archive.dishonestDepositReceipts(source, terminal, events)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 2 {
		t.Fatalf("dishonest-deposit receipts=%d, want 2", len(receipts))
	}
	foundRecovery := false
	for _, receipt := range receipts {
		foundRecovery = foundRecovery || receipt.TransactionHash == recovery.TransactionHash
	}
	if !foundRecovery {
		t.Fatalf("validator-agreed recovery receipt %s is absent: %+v", recovery.TransactionHash, receipts)
	}
}

func TestFinalSemanticBuilderRejectsDishonestDepositSemanticSubstitutions(t *testing.T) {
	t.Run("arbitrary later deposit", func(t *testing.T) {
		archive, source, terminal, events, _ := finalSemanticBuilderDishonestDepositFixture(t)
		unrelated := finalSemanticBuilderDepositLog(t, 2, 99, 1, 31, 0, 0x63)
		logs := []finalCanonicalEVMLog{}
		for _, group := range events.byTx {
			logs = append(logs, group...)
		}
		logs = append(logs, unrelated)
		sort.Slice(logs, func(i, j int) bool {
			if logs[i].BlockNumber != logs[j].BlockNumber {
				return logs[i].BlockNumber < logs[j].BlockNumber
			}
			return logs[i].TransactionHash < logs[j].TransactionHash
		})
		mutatedEvents, err := indexFinalSemanticEvents(&FinalCollectedChainSnapshot{EVMLogs: logs})
		if err != nil {
			t.Fatal(err)
		}
		unrelatedReceipt, err := archive.receiptFromIndex(mutatedEvents, mutatedEvents.byName["Deposit"][2], "unrelated")
		if err != nil {
			t.Fatal(err)
		}
		for index := range source.Validators {
			source.Validators[index].Cycles[0].Pools[0].DepositReceipt = unrelatedReceipt
		}
		if _, err := archive.dishonestDepositReceipts(source, terminal, mutatedEvents); err == nil {
			t.Fatalf("arbitrary later deposit was accepted as recovery: %v", err)
		}
	})
	t.Run("no demand mismatch", func(t *testing.T) {
		archive, source, terminal, events, _ := finalSemanticBuilderDishonestDepositFixture(t)
		for index := range terminal.DishonestDeposit.Validators {
			terminal.DishonestDeposit.Validators[index].Audit.RequiredDepositRao = terminal.DishonestDeposit.Transaction.AmountRao
		}
		if _, err := archive.dishonestDepositReceipts(source, terminal, events); err == nil || !strings.Contains(err.Error(), "penalty evidence") {
			t.Fatalf("non-mismatch penalty was accepted: %v", err)
		}
	})
	t.Run("validator recovery disagreement", func(t *testing.T) {
		archive, source, terminal, events, _ := finalSemanticBuilderDishonestDepositFixture(t)
		source.Validators[1].Cycles[0].Pools[0].DepositReceipt.LogsHash = finalTestHex(0xdd)
		if _, err := archive.dishonestDepositReceipts(source, terminal, events); err == nil || !strings.Contains(err.Error(), "disagree") {
			t.Fatalf("validator recovery disagreement was accepted: %v", err)
		}
	})
	t.Run("wrong underpayment policy", func(t *testing.T) {
		archive, source, terminal, events, _ := finalSemanticBuilderDishonestDepositFixture(t)
		source.PolicyHash = finalTestHex(0xee)
		terminal.DishonestDeposit.Transaction.PolicyHash = source.PolicyHash
		if _, err := archive.dishonestDepositReceipts(source, terminal, events); err == nil || !strings.Contains(err.Error(), "exact dishonest underpayment") {
			t.Fatalf("underpayment event with another policy was accepted: %v", err)
		}
	})
}

func finalSemanticBuilderReweightCycle(t *testing.T, cycle *FinalCRv4Cycle) {
	t.Helper()
	headInputs := make([]validatorpkg.ExactWeightInput, 0, finalHeadSlotCount)
	for _, candidate := range cycle.Candidates {
		if !candidate.Selected {
			continue
		}
		score, err := finalPositiveRational("candidate score", candidate.RawScore)
		if err != nil {
			t.Fatal(err)
		}
		headInputs = append(headInputs, validatorpkg.ExactWeightInput{UID: candidate.UID, Score: score})
	}
	poolInputs := make([]validatorpkg.ExactWeightInput, 0, len(cycle.Pools))
	for _, pool := range cycle.Pools {
		if !pool.AuditCompliant {
			continue
		}
		score, err := finalPositiveRational("pool score", pool.RawScore)
		if err != nil {
			t.Fatal(err)
		}
		poolInputs = append(poolInputs, validatorpkg.ExactWeightInput{UID: pool.UID, Score: score})
	}
	theta, err := finalProtocolRational(cycle.Theta)
	if err != nil {
		t.Fatal(err)
	}
	masked := make(map[uint16]bool, len(cycle.MaskedUIDs))
	for _, uid := range cycle.MaskedUIDs {
		masked[uid] = true
	}
	uids, scores, err := validatorpkg.BuildWeightVectorExact(poolInputs, headInputs, theta, masked)
	if err != nil {
		t.Fatal(err)
	}
	capped, err := crv4.ApplyMaxWeightLimitRational(scores, cycle.MaxWeightLimitU16)
	if err != nil {
		t.Fatal(err)
	}
	valueUIDs, values, err := crv4.NormalizeRationalToU16(uids, capped)
	if err != nil {
		t.Fatal(err)
	}
	if err := finalRepairMaxWeightLimitU16(valueUIDs, values, cycle.MaxWeightLimitU16); err != nil {
		t.Fatal(err)
	}
	cycle.Submitted = cycle.Submitted[:0]
	valueByUID := make(map[uint16]uint16, len(valueUIDs))
	for index, uid := range valueUIDs {
		valueByUID[uid] = values[index]
		cycle.Submitted = append(cycle.Submitted, FinalSubmittedWeight{UID: uid, Score: finalRationalFromBig(scores[index]), Value: values[index]})
	}
	for index := range cycle.Candidates {
		cycle.Candidates[index].AppliedWeight = valueByUID[cycle.Candidates[index].UID]
	}
	for index := range cycle.Pools {
		cycle.Pools[index].AppliedWeight = valueByUID[cycle.Pools[index].UID]
	}
	cycle.RealizedHeadValue, cycle.RealizedPoolValue, cycle.RealizedTotalValue = 0, 0, 0
	for _, candidate := range cycle.Candidates {
		if candidate.Selected {
			cycle.RealizedHeadValue += uint64(candidate.AppliedWeight)
		}
	}
	for _, pool := range cycle.Pools {
		cycle.RealizedPoolValue += uint64(pool.AppliedWeight)
	}
	for _, submitted := range cycle.Submitted {
		cycle.RealizedTotalValue += uint64(submitted.Value)
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	cycle.ValuesHash = bytesSHA256(encoded)
}

func finalSemanticBuilderDishonestDecisionEvidence(t *testing.T) (*FinalSemanticEvidence, map[uint64]FinalPoolUIDEvidence, map[uint64]FinalValidatorIdentityEvidence) {
	t.Helper()
	evidence, _ := finalSemanticFixture(t)
	evidence.Phase = "production-soak"
	poolByNO := make(map[uint64]FinalPoolUIDEvidence, len(evidence.Pools))
	for _, pool := range evidence.Pools {
		poolByNO[pool.NoID] = pool
	}
	validatorByID := make(map[uint64]FinalValidatorIdentityEvidence, len(evidence.Validators))
	for _, validator := range evidence.Validators {
		validatorByID[validator.ValidatorID] = validator
	}
	recoveryPool := evidence.Validators[0].Cycles[0].Pools[0]
	underpayment := recoveryPool.DepositReceipt
	underpayment.TransactionHash = finalTestHex(0xe1)
	underpayment.LogsHash = finalTestHex(0xe2)
	underpayment.Block = ChainHead{Number: 89, Hash: finalTestHex(89)}
	requiredDepositRao, ok := new(big.Int).SetString(recoveryPool.RequiredDepositRao, 10)
	if !ok || requiredDepositRao.Cmp(big.NewInt(1)) <= 0 {
		t.Fatalf("fixture recovery deposit is invalid: %q", recoveryPool.RequiredDepositRao)
	}
	dishonest := &FinalDishonestDepositEvidence{
		NoID: recoveryPool.NoID, PoolUID: recoveryPool.UID, RequiredDepositRao: recoveryPool.RequiredDepositRao, ObservedDepositRao: new(big.Int).Sub(requiredDepositRao, big.NewInt(1)).String(),
		RecoveryRequiredDepositRao: recoveryPool.RequiredDepositRao, RecoveryObservedDepositRao: recoveryPool.ObservedDepositRao,
		UnderpaymentReceipt: underpayment, RecoveryDepositReceipt: recoveryPool.DepositReceipt,
	}
	for validatorIndex := range evidence.Validators {
		validator := &evidence.Validators[validatorIndex]
		recoveryCycle := validator.Cycles[0]
		var penaltyCycle FinalCRv4Cycle
		wire, err := json.Marshal(recoveryCycle)
		if err != nil || json.Unmarshal(wire, &penaltyCycle) != nil {
			t.Fatal("clone dishonest-deposit penalty cycle")
		}
		penaltyCycle.SettlementEpoch--
		penaltyCycle.SubnetEpoch--
		penaltyCycle.NativeSnapshot = ChainHead{Number: 10, Hash: finalTestHex(10)}
		penaltyCycle.Commit.Block = ChainHead{Number: 11, Hash: finalTestHex(11)}
		penaltyCycle.Reveal.Block = ChainHead{Number: 12, Hash: finalTestHex(12)}
		penaltyCycle.Application.Block = ChainHead{Number: 13, Hash: finalTestHex(13)}
		penaltyCycle.EVMSnapshot = ChainHead{Number: 90, Hash: finalTestHex(90)}
		for poolIndex := range penaltyCycle.Pools {
			pool := &penaltyCycle.Pools[poolIndex]
			pool.SourceEpoch--
			pool.ObservedAtBlock = penaltyCycle.EVMSnapshot.Number
			if pool.NoID != dishonest.NoID {
				continue
			}
			pool.ObservedDepositRao, pool.DepositReceipt = dishonest.ObservedDepositRao, underpayment
			pool.AuditStatus, pool.AuditCompliant, pool.AuditDisposition, pool.AuditError = validatorpkg.DepositAuditMismatch, false, "zero_pool_weight", "observed deposit is below exact demand"
			pool.QualityPPM = 0
			pool.QualityFactor, pool.ImpliedUsageGiB, pool.RawScore = FinalRational{Numerator: "0", Denominator: "1"}, FinalRational{Numerator: "0", Denominator: "1"}, FinalRational{Numerator: "0", Denominator: "1"}
		}
		finalSemanticBuilderReweightCycle(t, &penaltyCycle)
		penaltyPresent, penaltyApplied := finalSemanticSubmittedPool(penaltyCycle.Submitted, dishonest.PoolUID)
		recoveryPresent, recoveryApplied := finalSemanticSubmittedPool(recoveryCycle.Submitted, dishonest.PoolUID)
		dishonest.Penalties = append(dishonest.Penalties, FinalDishonestDepositDecision{ValidatorID: validator.ValidatorID, ValidatorUID: validator.UID, PoolUID: dishonest.PoolUID, PoolPresent: penaltyPresent, PoolAppliedWeight: penaltyApplied, Cycle: penaltyCycle})
		dishonest.Recoveries = append(dishonest.Recoveries, FinalDishonestDepositDecision{ValidatorID: validator.ValidatorID, ValidatorUID: validator.UID, PoolUID: dishonest.PoolUID, PoolPresent: recoveryPresent, PoolAppliedWeight: recoveryApplied, Cycle: recoveryCycle})
	}
	evidence.DishonestDeposit = dishonest
	return &evidence, poolByNO, validatorByID
}

type finalSemanticDishonestChainReader struct {
	*finalTestChainReader
	corruptPenalty bool
}

func (r *finalSemanticDishonestChainReader) NativeWeights(ctx context.Context, netuid uint16, validatorUID uint16, head ChainHead) (FinalNativeWeightState, []FinalRPCExchange, error) {
	if r.evidence.DishonestDeposit != nil {
		for _, decision := range r.evidence.DishonestDeposit.Penalties {
			if decision.ValidatorUID == validatorUID && decision.Cycle.Application.Block == head {
				uids, values := finalSubmittedValues(decision.Cycle.Submitted)
				if r.corruptPenalty && len(values) != 0 {
					values[0]++
				}
				return FinalNativeWeightState{ValidatorUID: validatorUID, UIDs: uids, Values: values, Block: head}, r.exchange("substrate", "state_getStorage", head), nil
			}
		}
	}
	return r.finalTestChainReader.NativeWeights(ctx, netuid, validatorUID, head)
}

func TestFinalSemanticDishonestDepositDecisionsAndPublicReplay(t *testing.T) {
	evidence, pools, validators := finalSemanticBuilderDishonestDecisionEvidence(t)
	if err := verifyFinalDishonestDeposit(evidence, pools, validators); err != nil {
		t.Fatal(err)
	}
	reader := &finalSemanticDishonestChainReader{finalTestChainReader: &finalTestChainReader{evidence: evidence}}
	if _, err := executeFinalSemanticOnChain(context.Background(), evidence, reader); err != nil {
		t.Fatalf("exact penalty/recovery public replay failed: %v", err)
	}
	reader.corruptPenalty = true
	if _, err := executeFinalSemanticOnChain(context.Background(), evidence, reader); err == nil || !strings.Contains(err.Error(), "vector differs from signed intent") {
		t.Fatalf("tampered public penalty vector was accepted: %v", err)
	}

	mutations := []struct {
		name string
		edit func(*FinalDishonestDepositEvidence)
		want string
	}{
		{name: "missing validator", edit: func(d *FinalDishonestDepositEvidence) { d.Penalties = d.Penalties[:1] }, want: "validator census"},
		{name: "declared penalty inclusion", edit: func(d *FinalDishonestDepositEvidence) { d.Penalties[0].PoolPresent = true }, want: "declared pool application"},
		{name: "nonzero penalty", edit: func(d *FinalDishonestDepositEvidence) { d.Penalties[0].PoolAppliedWeight = 1 }, want: "declared pool application"},
		{name: "zero recovery", edit: func(d *FinalDishonestDepositEvidence) { d.Recoveries[0].PoolAppliedWeight = 0 }, want: "declared pool application"},
		{name: "wrong underpayment receipt", edit: func(d *FinalDishonestDepositEvidence) {
			d.Penalties[0].Cycle.Pools[0].DepositReceipt.TransactionHash = finalTestHex(0xef)
		}, want: "exact zero-weight underpayment"},
		{name: "penalty in accepted epoch", edit: func(d *FinalDishonestDepositEvidence) { d.Penalties[0].Cycle.SettlementEpoch++ }, want: "deposit/rate lineage"},
	}
	baseline, err := json.Marshal(evidence.DishonestDeposit)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			var candidate FinalDishonestDepositEvidence
			if err := json.Unmarshal(baseline, &candidate); err != nil {
				t.Fatal(err)
			}
			mutation.edit(&candidate)
			evidence.DishonestDeposit = &candidate
			if err := verifyFinalDishonestDeposit(evidence, pools, validators); err == nil || !strings.Contains(err.Error(), mutation.want) {
				t.Fatalf("dishonest-deposit mutation was accepted or returned wrong error: %v", err)
			}
		})
	}
}

func TestFinalSemanticBuilderProjectsDeterministicNativeRewardSnapshots(t *testing.T) {
	source, history, chain := finalSemanticBuilderRewardSource(t)
	archive := &finalSemanticArchive{runRoot: t.TempDir()}
	if err := archive.buildRewards(&source, history, chain); err != nil {
		t.Fatal(err)
	}
	if len(source.NativeRewards) != 8 {
		t.Fatalf("reward rows=%d, want 8", len(source.NativeRewards))
	}
	wantExpectations := map[string]string{"10/head/1": "positive", "10/head/2": "zero", "10/pool/1": "positive", "10/validator/1": "positive", "11/head/1": "zero", "11/head/2": "positive", "11/pool/1": "zero", "11/validator/1": "positive"}
	for _, row := range source.NativeRewards {
		key := new(big.Int).SetUint64(row.Epoch).String() + "/" + row.Role + "/" + new(big.Int).SetUint64(row.SubjectID).String()
		if row.Expected != wantExpectations[key] {
			t.Fatalf("reward %s expectation=%q, want %q", key, row.Expected, wantExpectations[key])
		}
		if row.Epoch == 10 && (row.Before.Number != 10 || row.After.Number != 30) {
			t.Fatalf("epoch 10 reward checkpoints=%+v -> %+v", row.Before, row.After)
		}
		if row.Epoch == 11 && (row.Before.Number != 30 || row.After.Number != 60) {
			t.Fatalf("epoch 11 reward checkpoints=%+v -> %+v", row.Before, row.After)
		}
	}

	conflict := *history[0]
	conflictReward := *history[0].NativeRewards
	conflictReward.EmissionRao = append([]string(nil), conflictReward.EmissionRao...)
	conflictReward.EmissionRao[source.HeadFleets[0].UID] = "999"
	conflict.NativeRewards = &conflictReward
	if _, err := finalSemanticRewardSnapshots(append(history, &conflict), &source); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("conflicting same-head reward snapshots were accepted: %v", err)
	}
}

func TestFinalSemanticBuilderRejectsOwnerStakeSubstitutionAndExternalDelegation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*FinalSemanticEvidence, []*ScenarioObservation, *FinalCollectedChainSnapshot)
		want   string
	}{
		{name: "owner coldkey substitution", mutate: func(_ *FinalSemanticEvidence, _ []*ScenarioObservation, chain *FinalCollectedChainSnapshot) {
			chain.RewardStakeSnapshots[1].Positions[0].ColdkeyPublicKey = "0x" + strings.Repeat("ef", 32)
		}, want: "lacks owner pair"},
		{name: "malformed owner stake", mutate: func(_ *FinalSemanticEvidence, _ []*ScenarioObservation, chain *FinalCollectedChainSnapshot) {
			chain.RewardStakeSnapshots[1].Positions[0].StakeRao = "01"
		}, want: "not canonical"},
		{name: "external delegation counterfeits head aggregate", mutate: func(source *FinalSemanticEvidence, history []*ScenarioObservation, _ *FinalCollectedChainSnapshot) {
			uid := source.HeadFleets[0].UID
			history[1].NativeRewards.TotalHotkeyAlphaRao[uid] = "111"
		}, want: "head owner-pair stake differs"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, history, chain := finalSemanticBuilderRewardSource(t)
			test.mutate(&source, history, chain)
			archive := &finalSemanticArchive{runRoot: t.TempDir()}
			if err := archive.buildRewards(&source, history, chain); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("owner-stake mutation error=%v, want %q", err, test.want)
			}
		})
	}
}

func TestFinalSemanticBuilderRecoversPriorNativeTerminalFromBoundClosedBundle(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateRoot := t.TempDir()
	priorRunID := "release-run-1"
	resultHash := finalTestHex(1)
	epochCount := uint64(cfg.Config.Scenarios.ShortEpochs)
	window := ScenarioAcceptanceWindow{Schema: "urnetwork-sim-acceptance-window-v1", BaselineHead: ChainHead{Number: 99, Hash: finalTestHex(99)}, BaselineObservationHash: finalTestHex(2), BaselineEpoch: 9, FirstEpoch: 10, EpochCount: epochCount, EpochBlocks: 10, StartBlock: 100, EndBlock: 100 + 10*epochCount, FinalizeOffsetBlocks: 2, TerminalBlock: 102 + 10*epochCount, PolicyEffectiveEpoch: 10, PolicyEffectiveBlock: 100}
	prior := finalSemanticBuilderCollectedManifest(cfg, priorRunID, resultHash, window)
	nativeTerminal := ChainHead{Number: 500, Hash: finalTestHex(0x50)}
	evmTerminal := ChainHead{Number: window.TerminalBlock, Hash: finalTestHex(0x51)}
	priorSnapshot := FinalCollectedChainSnapshot{
		Schema: finalCollectedChainSnapshotSchema, Phase: "release-1.0", RunID: priorRunID, DeploymentID: cfg.Config.Deployment.DeploymentID,
		EVMHead: evmTerminal, NativeHead: nativeTerminal, NativeHeads: []ChainHead{nativeTerminal},
	}
	snapshotData, _ := json.Marshal(priorSnapshot)
	bundle := FinalCollectedFileBundle{Schema: finalCollectedFileBundleSchema, Name: "live-chain", Files: []FinalCollectedFileBundleEntry{{Path: "live-chain/final-chain-snapshot.json", ContentHash: bytesSHA256(snapshotData), SizeBytes: uint64(len(snapshotData)), Data: snapshotData}}}
	bundleData, _ := json.Marshal(bundle)
	bundleLocator := FinalArtifactLocator{Kind: "closed-input-bundle", URI: "final-inputs/bundles/live-chain.json", ContentHash: bytesSHA256(bundleData), SizeBytes: uint64(len(bundleData))}
	prior.ClosedInputBundles = []FinalArtifactLocator{bundleLocator}
	prior.EvidenceHash, _ = finalSemanticCollectedInputsHash(prior)
	if err := verifyFinalSemanticCollectedInputs(cfg, prior); err != nil {
		t.Fatalf("test prior manifest is invalid: %v", err)
	}
	priorRoot := filepath.Join(stateRoot, "runs", priorRunID)
	if err := os.MkdirAll(filepath.Join(priorRoot, "final-inputs", "bundles"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(priorRoot, filepath.FromSlash(bundleLocator.URI)), bundleData, 0o600); err != nil {
		t.Fatal(err)
	}
	manifestData, _ := json.Marshal(prior)
	manifestLocator := FinalArtifactLocator{Kind: "prior-collected-input-manifest", URI: "prior-inputs", ContentHash: bytesSHA256(manifestData), SizeBytes: uint64(len(manifestData))}
	archive := &finalSemanticArchive{cfg: cfg, stateRoot: stateRoot, files: map[string][]byte{manifestLocator.URI: manifestData}}
	binding := &FinalCollectedPriorPhaseInputs{RunID: priorRunID, ResultHash: resultHash, Window: window, CollectedInputsManifest: manifestLocator}
	got, err := archive.priorNativeTerminalHead(binding, cfg.Config.Deployment.DeploymentID, evmTerminal)
	if err != nil {
		t.Fatal(err)
	}
	if got != nativeTerminal {
		t.Fatalf("prior terminal native head=%+v, want %+v", got, nativeTerminal)
	}
	if err := os.WriteFile(filepath.Join(priorRoot, filepath.FromSlash(bundleLocator.URI)), append(bundleData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.priorNativeTerminalHead(binding, cfg.Config.Deployment.DeploymentID, evmTerminal); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("mutated prior closed bundle was accepted: %v", err)
	}
}

func TestFinalSemanticBuilderUsesSelfContainedCopiedPriorLiveChain(t *testing.T) {
	priorRunID := "release-run-copied"
	window := ScenarioAcceptanceWindow{FirstEpoch: 10, EpochCount: finalReleaseEpochCount, EpochBlocks: finalReleaseEpochBlocks, FinalizeOffsetBlocks: finalReleaseFinalizeOffsetBlocks, TerminalBlock: 1750}
	nativeTerminal := ChainHead{Number: 500, Hash: finalTestHex(0x50)}
	evmTerminal := ChainHead{Number: window.TerminalBlock, Hash: finalTestHex(0x51)}
	snapshot := FinalCollectedChainSnapshot{
		Schema: finalCollectedChainSnapshotSchema, Phase: "release-1.0", RunID: priorRunID, DeploymentID: "deployment-copied",
		EVMHead: evmTerminal, NativeHead: nativeTerminal, NativeHeads: []ChainHead{nativeTerminal},
	}
	snapshotData, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	bundle := FinalCollectedFileBundle{Schema: finalCollectedFileBundleSchema, Name: "live-chain", Files: []FinalCollectedFileBundleEntry{{Path: "live-chain/final-chain-snapshot.json", ContentHash: bytesSHA256(snapshotData), SizeBytes: uint64(len(snapshotData)), Data: snapshotData}}}
	bundleData, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	locator := FinalArtifactLocator{Kind: "prior-live-chain-bundle", URI: "final-inputs/prior-release/live-chain/bundle-000.json", ContentHash: bytesSHA256(bundleData), SizeBytes: uint64(len(bundleData))}
	prior := &FinalCollectedPriorPhaseInputs{RunID: priorRunID, Window: window, LiveChainBundles: []FinalArtifactLocator{locator}}
	// Deliberately point stateRoot at an empty directory: success proves the
	// builder consumes only the copied current-closure bytes.
	archive := &finalSemanticArchive{stateRoot: t.TempDir(), files: map[string][]byte{locator.URI: bundleData}}
	got, err := archive.collectedPriorNativeTerminalHead(prior, snapshot.DeploymentID, evmTerminal)
	if err != nil {
		t.Fatal(err)
	}
	if got != nativeTerminal {
		t.Fatalf("copied prior native terminal=%+v, want %+v", got, nativeTerminal)
	}
	archive.files[locator.URI] = append(append([]byte(nil), bundleData...), 'x')
	if _, err := archive.collectedPriorNativeTerminalHead(prior, snapshot.DeploymentID, evmTerminal); err == nil {
		t.Fatal("tampered copied prior live-chain bundle was accepted")
	}
}

func finalSemanticBuilderNativeArchive(t *testing.T) (*finalSemanticArchive, *FinalCollectedChainSnapshot) {
	t.Helper()
	window := ScenarioAcceptanceWindow{FirstEpoch: 10, EpochCount: 2}
	collected := &FinalSemanticCollectedInputs{Window: window}
	files := map[string][]byte{}
	blockHeads := map[uint64]ChainHead{100: {Number: 100, Hash: finalTestHex(100)}}
	for validatorID := uint64(1); validatorID <= 2; validatorID++ {
		validator := FinalCollectedValidatorInputs{ValidatorID: validatorID}
		for index, epoch := range []uint64{10, 11} {
			snapshotBlock := uint64(15 + index*20)
			commitBlock := snapshotBlock + 5
			intent := validatorpkg.SteeringIntent{ValidatorID: validatorID, SettlementEpoch: epoch, SubnetEpoch: epoch + 100, Status: "applied", VectorHash: finalTestHex(byte(epoch)), NativeSnapshotBlock: snapshotBlock, NativeSnapshotHash: finalTestHex(byte(snapshotBlock)), FinalizedBlock: commitBlock, FinalizedBlockHash: finalTestHex(byte(commitBlock))}
			data, err := json.Marshal(intent)
			if err != nil {
				t.Fatal(err)
			}
			path := new(big.Int).SetUint64(validatorID).String() + "/" + new(big.Int).SetUint64(epoch).String()
			files[path] = data
			validator.Intents = append(validator.Intents, FinalCollectedValidatorIntent{Sequence: uint64(index + 1), SettlementEpoch: epoch, SubnetEpoch: intent.SubnetEpoch, Status: intent.Status, VectorHash: intent.VectorHash, Artifact: FinalArtifactLocator{URI: path}})
			blockHeads[commitBlock] = ChainHead{Number: commitBlock, Hash: intent.FinalizedBlockHash}
		}
		collected.Validators = append(collected.Validators, validator)
	}
	numbers := make([]uint64, 0, len(blockHeads))
	for number := range blockHeads {
		numbers = append(numbers, number)
	}
	sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })
	heads := make([]ChainHead, 0, len(numbers))
	for _, number := range numbers {
		heads = append(heads, blockHeads[number])
	}
	chain := &FinalCollectedChainSnapshot{NativeHead: blockHeads[100], NativeHeads: heads}
	return &finalSemanticArchive{collected: collected, files: files}, chain
}

func finalSemanticBuilderDepositLog(t *testing.T, noID, epoch, amount, block, txIndex uint64, seed byte) finalCanonicalEVMLog {
	t.Helper()
	contract, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	event := contract.Events["Deposit"]
	funder := common.HexToAddress("0x0000000000000000000000000000000000000042")
	data, err := event.Inputs.NonIndexed().Pack(new(big.Int).SetUint64(amount), [32]byte{1}, new(big.Int).SetUint64(uint64(seed)))
	if err != nil {
		t.Fatal(err)
	}
	return finalCanonicalEVMLog{
		Address: "0x0000000000000000000000000000000000000001",
		Topics:  []string{event.ID.Hex(), common.BigToHash(new(big.Int).SetUint64(noID)).Hex(), common.BigToHash(new(big.Int).SetUint64(epoch)).Hex(), common.BytesToHash(funder.Bytes()).Hex()},
		Data:    "0x" + hex.EncodeToString(data), BlockNumber: block, BlockHash: finalTestHex(byte(block)), TransactionHash: finalTestHex(seed), TransactionIndex: txIndex, LogIndex: 0,
	}
}

func finalSemanticBuilderRewardSource(t *testing.T) (FinalSemanticEvidence, []*ScenarioObservation, *FinalCollectedChainSnapshot) {
	t.Helper()
	key := func(seed byte) string {
		var value [32]byte
		value[0], value[31] = seed, seed^0xff
		encoded, err := ss58.Encode(value, ss58.BittensorPrefix)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	cycle10 := FinalCRv4Cycle{SettlementEpoch: 10, Application: FinalNativeReceipt{Block: ChainHead{Number: 20, Hash: finalTestHex(20)}}, Candidates: []FinalHeadCandidateEvidence{{FleetID: 1, Selected: true}, {FleetID: 2}}, Pools: []FinalPoolWeightEvidence{{NoID: 1, AuditCompliant: true}}}
	cycle11 := FinalCRv4Cycle{SettlementEpoch: 11, Application: FinalNativeReceipt{Block: ChainHead{Number: 40, Hash: finalTestHex(40)}}, Candidates: []FinalHeadCandidateEvidence{{FleetID: 1}, {FleetID: 2, Selected: true}}, Pools: []FinalPoolWeightEvidence{{NoID: 1}}}
	reserveSelf := "0x" + strings.Repeat("71", 32)
	source := FinalSemanticEvidence{
		Window: ScenarioAcceptanceWindow{FirstEpoch: 10, EpochCount: 2}, NativeStartHead: ChainHead{Number: 10, Hash: finalTestHex(10)}, NativeTerminalHead: ChainHead{Number: 100, Hash: finalTestHex(100)},
		Deployment: FinalContractDeploymentEvidence{ReserveSelfColdkey: reserveSelf},
		HeadFleets: []FinalHeadFleetEvidence{{FleetID: 1, UID: 1, Hotkey: key(0x11), Coldkey: key(0x21)}, {FleetID: 2, UID: 2, Hotkey: key(0x12), Coldkey: key(0x22)}},
		Pools:      []FinalPoolUIDEvidence{{NoID: 1, UID: 3, Hotkey: key(0x13), Coldkey: key(0x23)}},
		Validators: []FinalValidatorIdentityEvidence{{ValidatorID: 1, UID: 4, Hotkey: key(0x14), Coldkey: key(0x24), Cycles: []FinalCRv4Cycle{cycle10, cycle11}}},
	}
	reward := func(number uint64) *NativeRewardObservation {
		return &NativeRewardObservation{FinalizedHead: ChainHead{Number: number, Hash: finalTestHex(byte(number))}, EmissionRao: []string{"0", "0", "0", "0", "0"}, Incentive: make([]uint16, 5), Dividends: make([]uint16, 5), TotalHotkeyAlphaRao: []string{"0", "100", "100", "100", "200"}}
	}
	before := reward(10)
	after10 := reward(30)
	after10.EmissionRao[1], after10.Incentive[1] = "10", 10
	after10.EmissionRao[3], after10.Incentive[3] = "20", 20
	after10.EmissionRao[4], after10.Dividends[4] = "30", 30
	after10.TotalHotkeyAlphaRao = []string{"0", "110", "100", "90", "205"}
	after11 := reward(60)
	after11.EmissionRao[2], after11.Incentive[2] = "11", 11
	after11.EmissionRao[4], after11.Dividends[4] = "31", 31
	after11.TotalHotkeyAlphaRao = []string{"0", "110", "110", "95", "210"}
	history := []*ScenarioObservation{{NativeRewards: before}, {NativeRewards: after10}, {NativeRewards: after11}}
	positionKeys := func(snapshot *NativeRewardObservation) FinalCollectedRewardStakeSnapshot {
		result := FinalCollectedRewardStakeSnapshot{NativeHead: snapshot.FinalizedHead, EVMHead: ChainHead{Number: snapshot.FinalizedHead.Number, Hash: finalTestHex(byte(snapshot.FinalizedHead.Number + 100))}}
		for _, role := range []struct {
			identity        string
			hotkey, coldkey string
			uid             uint16
		}{
			{"head-1", source.HeadFleets[0].Hotkey, source.HeadFleets[0].Coldkey, 1},
			{"head-2", source.HeadFleets[1].Hotkey, source.HeadFleets[1].Coldkey, 2},
			{"pool-1", source.Pools[0].Hotkey, source.Pools[0].Coldkey, 3},
			{"validator-1", source.Validators[0].Hotkey, source.Validators[0].Coldkey, 4},
		} {
			hotkey, coldkey, err := finalSemanticSS58Pair(role.identity, role.hotkey, role.coldkey)
			if err != nil {
				t.Fatal(err)
			}
			stake := snapshot.TotalHotkeyAlphaRao[role.uid]
			if role.identity == "validator-1" {
				value, _ := new(big.Int).SetString(stake, 10)
				stake = new(big.Int).Quo(value, big.NewInt(2)).String()
			}
			result.Positions = append(result.Positions, FinalCollectedRewardStakePosition{Identity: role.identity, HotkeyPublicKey: "0x" + hex.EncodeToString(hotkey[:]), ColdkeyPublicKey: "0x" + hex.EncodeToString(coldkey[:]), StakeRao: stake})
		}
		validatorHotkey, _, _ := finalSemanticSS58Pair("validator", source.Validators[0].Hotkey, source.Validators[0].Coldkey)
		total, _ := new(big.Int).SetString(snapshot.TotalHotkeyAlphaRao[4], 10)
		owner, _ := new(big.Int).SetString(result.Positions[3].StakeRao, 10)
		result.Positions = append(result.Positions, FinalCollectedRewardStakePosition{Identity: "reserve-validator-sink", HotkeyPublicKey: "0x" + hex.EncodeToString(validatorHotkey[:]), ColdkeyPublicKey: reserveSelf, StakeRao: new(big.Int).Sub(total, owner).String()})
		sort.Slice(result.Positions, func(i, j int) bool { return result.Positions[i].Identity < result.Positions[j].Identity })
		return result
	}
	chain := &FinalCollectedChainSnapshot{NativeHead: source.NativeTerminalHead}
	for _, observation := range history {
		chain.RewardStakeSnapshots = append(chain.RewardStakeSnapshots, positionKeys(observation.NativeRewards))
	}
	return source, history, chain
}

func finalSemanticBuilderCollectedManifest(cfg *ResolvedConfig, runID, resultHash string, window ScenarioAcceptanceWindow) *FinalSemanticCollectedInputs {
	dummy := func(kind, name string) FinalArtifactLocator {
		data := []byte(kind + ":" + name)
		return FinalArtifactLocator{Kind: kind, URI: "dummy/" + name, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	}
	collected := &FinalSemanticCollectedInputs{
		Schema: finalSemanticCollectedInputsSchema, Phase: "release-1.0", RunID: runID, ResultHash: resultHash, Window: window,
		Policy: dummy("policy", "policy"), ScenarioResult: dummy("scenario-result-candidate", "result"), TerminalObservation: dummy("scenario-terminal-observation", "terminal"), ObservationHistory: dummy("scenario-observation-history", "history"),
		ClosedInputBundles: []FinalArtifactLocator{dummy("closed-input-bundle", "placeholder")},
	}
	for epoch := window.FirstEpoch - 1; epoch < window.FirstEpoch+window.EpochCount; epoch++ {
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			name := new(big.Int).SetUint64(epoch).String() + "-" + new(big.Int).SetUint64(uint64(noID)).String()
			collected.Payouts = append(collected.Payouts, FinalCollectedPayoutArtifact{NoID: uint64(noID), Epoch: epoch, Artifact: dummy("payout-artifact", "payout-"+name)})
		}
	}
	for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
		id := new(big.Int).SetUint64(uint64(validatorID)).String()
		validator := FinalCollectedValidatorInputs{ValidatorID: uint64(validatorID), PathVPK: finalTestHex(byte(0x70 + validatorID)), IntentStore: dummy("validator-steering-intent-store", "intent-store-"+id)}
		for offset := uint64(0); offset < window.EpochCount; offset++ {
			epoch := window.FirstEpoch + offset
			name := id + "-" + new(big.Int).SetUint64(epoch).String()
			validator.Intents = append(validator.Intents, FinalCollectedValidatorIntent{Sequence: offset + 1, SettlementEpoch: epoch, SubnetEpoch: epoch + 100, Status: "applied", VectorHash: finalTestHex(byte(epoch + uint64(validatorID))), Artifact: dummy("steering-intent", "intent-"+name), Measurement: dummy("validator-release-measurement", "measurement-"+name), Envelope: dummy("validator-release-measurement-envelope", "envelope-"+name)})
		}
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			no := new(big.Int).SetUint64(uint64(noID)).String()
			validator.Attempts = append(validator.Attempts, FinalCollectedValidatorAttempts{NoID: uint64(noID), RecordCount: 1, CompleteCount: 1, Artifact: dummy("validator-attempt-records", "attempt-"+id+"-"+no)})
			validator.PathProofs = append(validator.PathProofs, FinalCollectedValidatorPathProof{NoID: uint64(noID), FirstEpoch: window.FirstEpoch, LastEpoch: window.FirstEpoch + window.EpochCount - 1, ProofCount: window.EpochCount, Artifact: dummy("validator-path-proofs", "proof-"+id+"-"+no)})
		}
		collected.Validators = append(collected.Validators, validator)
	}
	return collected
}

type finalSemanticCarryRow struct {
	funded    uint64
	carryIn   uint64
	committed bool
	claim     bool
}

func finalSemanticBuilderCarryEvidence(t *testing.T, rows []finalSemanticCarryRow) (*FinalSemanticEvidence, map[uint64]FinalPoolUIDEvidence) {
	t.Helper()
	if len(rows) == 0 {
		t.Fatal("carry fixture requires at least one epoch")
	}
	artifact := func(kind, name string) FinalArtifactLocator {
		data := []byte(kind + ":" + name)
		return FinalArtifactLocator{Kind: kind, URI: "carry/" + name, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	}
	receiptIndex := byte(1)
	receipt := func(name string, block uint64) FinalEVMReceipt {
		receiptIndex++
		return FinalEVMReceipt{
			TransactionHash: finalTestHex(receiptIndex), Block: ChainHead{Number: block, Hash: finalTestHex(byte(block))},
			Status: "success", LogsHash: finalTestHex(receiptIndex + 100), Proof: artifact("evm-receipt", name+".json"),
		}
	}
	evidence := &FinalSemanticEvidence{
		ExpectedOperators: 1,
		Window:            ScenarioAcceptanceWindow{FirstEpoch: 10, EpochCount: uint64(len(rows)), StartBlock: 100, TerminalBlock: 1000},
	}
	totals := map[string]*big.Int{}
	for _, name := range []string{"captured", "carry_in", "funded", "claimed", "paid", "deferred", "outstanding", "carry_out"} {
		totals[name] = new(big.Int)
	}
	for index, spec := range rows {
		epoch := uint64(10 + index)
		block := uint64(110 + index*10)
		funded := new(big.Int).SetUint64(spec.funded)
		carryIn := new(big.Int).SetUint64(spec.carryIn)
		row := FinalEpochOperatorEvidence{
			Epoch: epoch, NoID: 1, Capture: receipt("capture-"+strconv.Itoa(index), block), Finalize: receipt("finalize-"+strconv.Itoa(index), block+2),
			CapturedRao: funded.String(), CarryInRao: carryIn.String(), FundedRao: funded.String(), Claims: []FinalClaimEvidence{},
		}
		if spec.committed {
			total := new(big.Int).Add(new(big.Int).Set(funded), carryIn)
			rootReceipt := receipt("root-"+strconv.Itoa(index), block+1)
			row.RootDisposition, row.Root, row.Status = "committed", &rootReceipt, 2
			row.PayoutRoot, row.ArtifactHash = finalTestHex(byte(0x20+index)), finalTestHex(byte(0x40+index))
			payout := artifact("payout-artifact", "payout-"+strconv.Itoa(index)+".json")
			row.PayoutArtifact = &payout
			row.TotalRao, row.CarryOutRao = total.String(), "0"
			if spec.claim {
				claimReceipt := receipt("claim-"+strconv.Itoa(index), block+3)
				row.Claims = []FinalClaimEvidence{{LeafIndex: 0, Payee: finalTestHex(byte(0x60 + index)), ShareBPS: 10_000, ClaimedRao: total.String(), PaidRao: total.String(), DeferredRao: "0", Receipt: claimReceipt}}
				row.ClaimedRao, row.PaidRao, row.DeferredCreditRao, row.OutstandingRao = total.String(), total.String(), "0", "0"
			} else {
				row.ClaimedRao, row.PaidRao, row.DeferredCreditRao, row.OutstandingRao = "0", "0", "0", total.String()
			}
		} else {
			carryOut := new(big.Int).Add(new(big.Int).Set(carryIn), funded)
			row.RootDisposition, row.Status = "missed", 3
			row.TotalRao, row.ClaimedRao, row.PaidRao, row.DeferredCreditRao, row.OutstandingRao, row.CarryOutRao = funded.String(), "0", "0", "0", "0", carryOut.String()
		}
		evidence.Epochs = append(evidence.Epochs, row)
		values, err := finalEpochAmounts(&row)
		if err != nil {
			t.Fatal(err)
		}
		for name := range totals {
			totals[name].Add(totals[name], values[name])
		}
	}
	evidence.Conservation = FinalPoolConservation{
		CapturedRao: totals["captured"].String(), CarryInRao: totals["carry_in"].String(), FundedRao: totals["funded"].String(),
		ClaimedRao: totals["claimed"].String(), PaidRao: totals["paid"].String(), DeferredCreditRao: totals["deferred"].String(),
		OutstandingRao: totals["outstanding"].String(), CarryOutRao: totals["carry_out"].String(),
	}
	last := evidence.Epochs[len(evidence.Epochs)-1]
	pools := map[uint64]FinalPoolUIDEvidence{1: {NoID: 1, FinalCarryRao: last.CarryOutRao}}
	return evidence, pools
}

func TestFinalSemanticCarryModelMatchesSettlementVault(t *testing.T) {
	tests := []struct {
		name string
		rows []finalSemanticCarryRow
	}{
		{name: "zero carry committed", rows: []finalSemanticCarryRow{{funded: 100, committed: true, claim: true}}},
		{name: "nonzero entering carry committed", rows: []finalSemanticCarryRow{{funded: 100, carryIn: 25, committed: true, claim: true}}},
		{name: "missed then committed", rows: []finalSemanticCarryRow{{funded: 100}, {funded: 50, carryIn: 100, committed: true, claim: true}}},
		{name: "consecutive misses then committed", rows: []finalSemanticCarryRow{{funded: 100}, {funded: 50, carryIn: 100}, {funded: 25, carryIn: 150, committed: true, claim: true}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, pools := finalSemanticBuilderCarryEvidence(t, test.rows)
			if err := verifyFinalPoolEpochs(evidence, pools); err != nil {
				t.Fatal(err)
			}
			for index, row := range evidence.Epochs {
				if row.FundedRao != row.CapturedRao {
					t.Fatalf("row %d funded=%s captured=%s", index, row.FundedRao, row.CapturedRao)
				}
			}
		})
	}
}

func TestFinalSemanticCarryModelFailsClosedOnAdjacentAccountingErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*FinalSemanticEvidence, map[uint64]FinalPoolUIDEvidence)
		want   string
	}{
		{name: "funded improperly includes carry", mutate: func(e *FinalSemanticEvidence, _ map[uint64]FinalPoolUIDEvidence) { e.Epochs[2].FundedRao = "175" }, want: "funded != boundary capture"},
		{name: "missed total improperly includes carry", mutate: func(e *FinalSemanticEvidence, _ map[uint64]FinalPoolUIDEvidence) { e.Epochs[1].TotalRao = "150" }, want: "missed root does not preserve"},
		{name: "consecutive miss loses prior carry", mutate: func(e *FinalSemanticEvidence, _ map[uint64]FinalPoolUIDEvidence) { e.Epochs[1].CarryOutRao = "50" }, want: "missed root does not preserve"},
		{name: "committed total omits carry", mutate: func(e *FinalSemanticEvidence, _ map[uint64]FinalPoolUIDEvidence) { e.Epochs[2].TotalRao = "25" }, want: "committed total/carry"},
		{name: "no successful claims", mutate: func(e *FinalSemanticEvidence, _ map[uint64]FinalPoolUIDEvidence) {
			e.Epochs[2].Claims = nil
			e.Epochs[2].ClaimedRao, e.Epochs[2].PaidRao, e.Epochs[2].OutstandingRao = "0", "0", e.Epochs[2].TotalRao
			e.Conservation.ClaimedRao, e.Conservation.PaidRao, e.Conservation.OutstandingRao = "0", "0", e.Epochs[2].TotalRao
		}, want: "lacks a nonzero committed-root and successful-claim census"},
		{name: "claim amount is not merkle share", mutate: func(e *FinalSemanticEvidence, _ map[uint64]FinalPoolUIDEvidence) {
			e.Epochs[2].Claims[0].ShareBPS = 5_000
		}, want: "floor(share_bps*total/10000)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, pools := finalSemanticBuilderCarryEvidence(t, []finalSemanticCarryRow{{funded: 100}, {funded: 50, carryIn: 100}, {funded: 25, carryIn: 150, committed: true, claim: true}})
			test.mutate(evidence, pools)
			if err := verifyFinalPoolEpochs(evidence, pools); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("accounting mutation was accepted or returned wrong error: %v", err)
			}
		})
	}
}

func finalSemanticBuilderWindow(phase string) *FinalSemanticEvidence {
	count, blocks, finalize := finalReleaseEpochCount, finalReleaseEpochBlocks, finalReleaseFinalizeOffsetBlocks
	if phase == "production-soak" {
		count, blocks, finalize = finalProductionEpochCount, finalProductionEpochBlocks, finalProductionFinalizeOffsetBlocks
	}
	window := ScenarioAcceptanceWindow{
		Schema: "urnetwork-sim-acceptance-window-v1", BaselineHead: ChainHead{Number: 99, Hash: finalTestHex(99)}, BaselineObservationHash: finalTestHex(98),
		BaselineEpoch: 9, FirstEpoch: 10, EpochCount: count, EpochBlocks: blocks, StartBlock: 100, FinalizeOffsetBlocks: finalize,
		PolicyEffectiveEpoch: 10, PolicyEffectiveBlock: 100,
	}
	window.EndBlock = window.StartBlock + window.EpochCount*window.EpochBlocks
	window.TerminalBlock = window.EndBlock + window.FinalizeOffsetBlocks
	return &FinalSemanticEvidence{
		Phase: phase, Window: window, EVMCampaignStartHead: ChainHead{Number: 50, Hash: finalTestHex(50)},
		NativeStartHead: ChainHead{Number: 10, Hash: finalTestHex(10)}, NativeTerminalHead: ChainHead{Number: 20, Hash: finalTestHex(20)},
		EVMTerminalHead: ChainHead{Number: window.TerminalBlock, Hash: finalTestHex(90)},
	}
}

func TestFinalSemanticPhaseCadenceIsExact(t *testing.T) {
	for _, phase := range []string{"release-1.0", "production-soak"} {
		t.Run(phase, func(t *testing.T) {
			evidence := finalSemanticBuilderWindow(phase)
			if err := verifyFinalWindow(evidence); err != nil {
				t.Fatal(err)
			}
			mutations := []struct {
				name string
				edit func(*ScenarioAcceptanceWindow)
			}{
				{name: "epoch count", edit: func(window *ScenarioAcceptanceWindow) { window.EpochCount-- }},
				{name: "epoch blocks", edit: func(window *ScenarioAcceptanceWindow) { window.EpochBlocks-- }},
				{name: "finalize offset", edit: func(window *ScenarioAcceptanceWindow) { window.FinalizeOffsetBlocks-- }},
			}
			for _, mutation := range mutations {
				t.Run(mutation.name, func(t *testing.T) {
					candidate := *evidence
					candidate.Window = evidence.Window
					mutation.edit(&candidate.Window)
					candidate.Window.EndBlock = candidate.Window.StartBlock + candidate.Window.EpochCount*candidate.Window.EpochBlocks
					candidate.Window.TerminalBlock = candidate.Window.EndBlock + candidate.Window.FinalizeOffsetBlocks
					candidate.EVMTerminalHead.Number = candidate.Window.TerminalBlock
					if err := verifyFinalWindow(&candidate); err == nil || !strings.Contains(err.Error(), "acceptance cadence") {
						t.Fatalf("mutated cadence was accepted: %v", err)
					}
				})
			}
		})
	}
}

func finalSemanticBuilderPriorBinding() *FinalSemanticEvidence {
	artifact := func(kind, name string) FinalArtifactLocator {
		data := []byte(kind + ":" + name)
		return FinalArtifactLocator{Kind: kind, URI: "prior/" + name, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	}
	evidence := finalSemanticBuilderWindow("production-soak")
	evidence.RunID, evidence.DeploymentID = "production-run", "deployment-1"
	evidence.Window.BaselineHead = ChainHead{Number: 10_000, Hash: finalTestHex(0x71)}
	evidence.NativeStartHead = ChainHead{Number: 1_000, Hash: finalTestHex(0x72)}
	priorWindow := ScenarioAcceptanceWindow{
		Schema: "urnetwork-sim-acceptance-window-v1", BaselineHead: ChainHead{Number: 99, Hash: finalTestHex(99)}, BaselineObservationHash: finalTestHex(0x73),
		BaselineEpoch: 9, FirstEpoch: 10, EpochCount: finalReleaseEpochCount, EpochBlocks: finalReleaseEpochBlocks, StartBlock: 100,
		EndBlock: 100 + finalReleaseEpochCount*finalReleaseEpochBlocks, FinalizeOffsetBlocks: finalReleaseFinalizeOffsetBlocks,
		PolicyEffectiveEpoch: 10, PolicyEffectiveBlock: 100,
	}
	priorWindow.TerminalBlock = priorWindow.EndBlock + priorWindow.FinalizeOffsetBlocks
	evidence.PriorPhase = &FinalPriorPhaseBinding{
		RunID: "release-run", ResultHash: finalTestHex(0x74), SemanticEvidenceHash: finalTestHex(0x75), PublicTranscriptHash: finalTestHex(0x76),
		OwnerCompletionEnvelopeHash: "sha256:" + strings.Repeat("11", 32), EvidenceManifestEnvelopeHash: "sha256:" + strings.Repeat("22", 32), SemanticSupplementEnvelopeHash: "sha256:" + strings.Repeat("33", 32),
		Completion: artifact("scenario-complete", "complete.json"), EvidenceManifest: artifact("campaign-evidence-manifest", "manifest.json"),
		SemanticSupplement:       artifact("prior-semantic-supplement-envelope", "semantic-verified.evidence.json"),
		SemanticEvidenceEnvelope: artifact("prior-semantic-file-envelope", "semantic-evidence.evidence.json"), SemanticEvidence: artifact("prior-semantic-evidence", "semantic-evidence.json"),
		AcceptanceWindow: priorWindow, TerminalNativeHead: ChainHead{Number: 900, Hash: finalTestHex(0x77)}, TerminalEVMHead: ChainHead{Number: priorWindow.TerminalBlock, Hash: finalTestHex(0x78)},
	}
	return evidence
}

func TestFinalSemanticProductionLineageRequiresSemanticVerifiedPredecessor(t *testing.T) {
	evidence := finalSemanticBuilderPriorBinding()
	if err := verifyFinalPhaseLineage(evidence); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*FinalSemanticEvidence)
		want   string
	}{
		{name: "missing predecessor", mutate: func(e *FinalSemanticEvidence) { e.PriorPhase = nil }, want: "authenticated release-1.0 predecessor"},
		{name: "same run replay", mutate: func(e *FinalSemanticEvidence) { e.PriorPhase.RunID = e.RunID }, want: "authenticated release-1.0 predecessor"},
		{name: "missing semantic hash", mutate: func(e *FinalSemanticEvidence) { e.PriorPhase.SemanticEvidenceHash = "" }, want: "semantic evidence hash"},
		{name: "missing public transcript", mutate: func(e *FinalSemanticEvidence) { e.PriorPhase.PublicTranscriptHash = "" }, want: "public transcript hash"},
		{name: "missing signed supplement", mutate: func(e *FinalSemanticEvidence) { e.PriorPhase.SemanticSupplement = FinalArtifactLocator{} }, want: "semantic supplement"},
		{name: "shortened release", mutate: func(e *FinalSemanticEvidence) { e.PriorPhase.AcceptanceWindow.EpochCount-- }, want: "acceptance window"},
		{name: "terminal EVM mismatch", mutate: func(e *FinalSemanticEvidence) { e.PriorPhase.TerminalEVMHead.Number-- }, want: "terminal heads"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := *evidence
			prior := *evidence.PriorPhase
			candidate.PriorPhase = &prior
			test.mutate(&candidate)
			if err := verifyFinalPhaseLineage(&candidate); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid production lineage was accepted or returned wrong error: %v", err)
			}
		})
	}
}

type finalSemanticBarrierFixture struct {
	cfg               *ResolvedConfig
	stateRoot         string
	prior             *ScenarioResult
	roles             *RoleSecrets
	priorRunRoot      string
	production        *ScenarioResult
	productionRunRoot string
}

func newFinalSemanticBarrierFixture(t *testing.T) *finalSemanticBarrierFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	stateRoot := t.TempDir()
	prior, roles, priorRunRoot := writeReleaseCampaignFixture(t, cfg, stateRoot, 26, 32)
	production := &ScenarioResult{
		Name: "production-soak", RunID: "production-run", StartedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		DeploymentID: prior.DeploymentID, ConfigHash: prior.ConfigHash, PolicyHash: prior.PolicyHash, ChainID: prior.ChainID, GenesisHash: prior.GenesisHash, Netuid: prior.Netuid,
	}
	return &finalSemanticBarrierFixture{cfg: cfg, stateRoot: stateRoot, prior: prior, roles: roles, priorRunRoot: priorRunRoot, production: production, productionRunRoot: filepath.Join(stateRoot, "runs", production.RunID)}
}

// writeMarker publishes the canonical readiness marker used by barrier tests.
func (fixture *finalSemanticBarrierFixture) writeMarker(t *testing.T, resultHash string) string {
	return fixture.writeMarkerWithClosureHashes(t, resultHash, finalTestHex(0x33), finalTestHex(0x44))
}

// writeMarkerWithClosureHashes lets barrier regressions distinguish canonical
// evidence identities from SHA-256 storage-address identities.
func (fixture *finalSemanticBarrierFixture) writeMarkerWithClosureHashes(t *testing.T, resultHash, captureHash, collectedHash string) string {
	t.Helper()
	payload := FinalSemanticSupplementPayload{
		Schema: finalSemanticSupplementSchema, Status: finalSemanticSupplementStatus, Phase: "release-1.0", RunID: fixture.prior.RunID, ResultHash: resultHash,
		ScenarioCompleteHash: "sha256:" + strings.Repeat("11", 32), ScenarioEvidenceManifestHash: "sha256:" + strings.Repeat("22", 32),
		CaptureStatusHash: captureHash, CollectedInputsHash: collectedHash,
		SemanticEvidenceHash: finalTestHex(0x81), PublicTranscriptHash: finalTestHex(0x82),
		Files: []FinalSemanticSupplementFile{
			{Path: finalSemanticMarkdownFilename, ContentHash: "sha256:" + strings.Repeat("55", 32), Size: 1, EnvelopeHash: "sha256:" + strings.Repeat("66", 32)},
			{Path: finalSemanticEvidenceFilename, ContentHash: "sha256:" + strings.Repeat("77", 32), Size: 1, EnvelopeHash: "sha256:" + strings.Repeat("88", 32)},
		},
	}
	marker, err := signEvidence(fixture.cfg, finalSemanticSupplementKind, fixture.prior.RunID, payload, fixture.roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(fixture.priorRunRoot, finalSemanticSupplementFilename)
	if err := writePublicJSON(markerPath, marker); err != nil {
		t.Fatal(err)
	}
	return markerPath
}

func TestFinalSemanticProductionCollectorWaitsForAuthenticatedReleaseAnalysisBeforeWrites(t *testing.T) {
	fixture := newFinalSemanticBarrierFixture(t)
	done := make(chan error, 1)
	waiting := make(chan struct{})
	go func() {
		done <- awaitFinalPriorSemanticReadyObserved(context.Background(), fixture.cfg, fixture.stateRoot, fixture.production, func() { close(waiting) })
	}()
	select {
	case <-waiting:
	case err := <-done:
		t.Fatalf("production collector failed before entering release-analysis barrier: %v", err)
	case <-time.After(time.Second):
		t.Fatal("production collector did not enter release-analysis barrier")
	}
	select {
	case err := <-done:
		t.Fatalf("production collector did not remain at release-analysis barrier: %v", err)
	default:
	}
	if _, err := os.Stat(filepath.Join(fixture.productionRunRoot, "final-inputs")); !os.IsNotExist(err) {
		t.Fatalf("readiness wait created partial production capture: %v", err)
	}
	fixture.writeMarker(t, fixture.prior.EvidenceHash)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("authenticated semantic_verified marker did not release collector: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("collector remained blocked after authenticated release analysis")
	}
}

func TestFinalSemanticProductionCollectorCancellationLeavesNoPartialCapture(t *testing.T) {
	fixture := newFinalSemanticBarrierFixture(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancelDone := make(chan error, 1)
	cancelWaiting := make(chan struct{})
	go func() {
		cancelDone <- awaitFinalPriorSemanticReadyObserved(cancelled, fixture.cfg, fixture.stateRoot, fixture.production, func() { close(cancelWaiting) })
	}()
	select {
	case <-cancelWaiting:
	case err := <-cancelDone:
		t.Fatalf("collector failed before entering canceled barrier: %v", err)
	case <-time.After(time.Second):
		t.Fatal("collector did not enter cancelable release-analysis barrier")
	}
	cancel()
	select {
	case err := <-cancelDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled release analysis returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled release analysis did not promptly join collector")
	}
	if _, err := os.Stat(filepath.Join(fixture.productionRunRoot, "final-inputs")); !os.IsNotExist(err) {
		t.Fatalf("canceled readiness wait created partial production capture: %v", err)
	}
}

func TestFinalSemanticProductionCollectorRejectsAuthenticatedWrongReleaseMarker(t *testing.T) {
	fixture := newFinalSemanticBarrierFixture(t)
	fixture.writeMarker(t, finalTestHex(0xee))
	if err := awaitFinalPriorSemanticReady(context.Background(), fixture.cfg, fixture.stateRoot, fixture.production); err == nil || !strings.Contains(err.Error(), "does not bind") {
		t.Fatalf("authenticated marker for another result was accepted: %v", err)
	}
}

func TestFinalSemanticProductionCollectorRejectsStorageHashesForCanonicalClosure(t *testing.T) {
	fixture := newFinalSemanticBarrierFixture(t)
	fixture.writeMarkerWithClosureHashes(t, fixture.prior.EvidenceHash, "sha256:"+strings.Repeat("33", 32), "sha256:"+strings.Repeat("44", 32))
	if err := awaitFinalPriorSemanticReady(context.Background(), fixture.cfg, fixture.stateRoot, fixture.production); err == nil || !strings.Contains(err.Error(), "does not bind") {
		t.Fatalf("storage-address hashes bypassed canonical prior closure identities: %v", err)
	}
}

func TestFinalSemanticPostCaptureOutputsDoNotInvalidateSignedLiveClosure(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateRoot := t.TempDir()
	result, roles, runRoot := writeReleaseCampaignFixture(t, cfg, stateRoot, 26, 32)
	if _, _, err := loadCompletedScenarioCampaign(cfg, stateRoot, roles, "release-1.0"); err != nil {
		t.Fatalf("baseline signed live closure is invalid: %v", err)
	}
	outputs := map[string][]byte{
		finalSemanticMarkdownFilename:                                   []byte("# FINAL\n"),
		finalSemanticEvidenceFilename:                                   []byte("{}\n"),
		finalSemanticSupplementFilename:                                 []byte("{}\n"),
		"final-derived/receipt.json":                                    []byte("{}\n"),
		finalSemanticStagePrefix + "crash/final-semantic-evidence.json": []byte("{}\n"),
		finalSemanticStagePrefix + "crash/FINAL.md":                     []byte("# staged FINAL\n"),
	}
	for name, data := range outputs {
		if !isFinalSemanticPostCapturePath(name) {
			t.Fatalf("test post-capture output %q is outside the exact allowlist", name)
		}
		path := filepath.Join(runRoot, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, forged := range []string{finalSemanticStagePrefix + "forged.json", finalSemanticStagePrefix + "forged/other.json"} {
		if isFinalSemanticPostCapturePath(forged) {
			t.Fatalf("non-stage path %q bypassed the signed live closure", forged)
		}
	}
	loaded, _, err := loadCompletedScenarioCampaign(cfg, stateRoot, roles, "release-1.0")
	if err != nil || loaded.RunID != result.RunID || loaded.EvidenceHash != result.EvidenceHash {
		t.Fatalf("owner-signed live closure changed after separate semantic outputs: loaded=%+v err=%v", loaded, err)
	}
	for _, forged := range []string{finalSemanticStagePrefix + "forged.json", finalSemanticStagePrefix + "forged/other.json"} {
		path := filepath.Join(runRoot, filepath.FromSlash(forged))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadCompletedScenarioCampaign(cfg, stateRoot, roles, "release-1.0"); err == nil || !strings.Contains(err.Error(), "file hashes") {
			t.Fatalf("forged private-stage path %q did not invalidate the signed closure: %v", forged, err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := atomicWrite(filepath.Join(runRoot, "unexpected-live-artifact.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadCompletedScenarioCampaign(cfg, stateRoot, roles, "release-1.0"); err == nil || !strings.Contains(err.Error(), "file hashes") {
		t.Fatalf("non-semantic live artifact tamper did not invalidate signed closure: %v", err)
	}
}
