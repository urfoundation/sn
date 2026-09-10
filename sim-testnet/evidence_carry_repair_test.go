// Historical recovery uses the original approved artifact without relaxing
// fresh execution, action lineage, source-file integrity or immutable custody.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

// A retained pre-companion fixture must actually omit the extension, its
// actions and every dependency. All slices/maps remain owned by this fixture.
func validatorEvidenceLegacyPlanTest(t *testing.T, original *SetupPlan) *SetupPlan {
	t.Helper()
	return validatorEvidenceLegacyPlanSchemaTest(t, original, setupPlanSchemaV11)
}

// Select the actual historical schema before validating its owned projection.
// This also permits a genuine v9 ancestor whose recovery shape v10 forbids.
func validatorEvidenceLegacyPlanSchemaTest(t *testing.T, original *SetupPlan, schema string) *SetupPlan {
	t.Helper()
	if planUsesValidatorEvidenceEnvelope(schema) {
		t.Fatal("evidence-free fixture projection requires a historical schema")
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var legacy SetupPlan
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.Schema = schema
	legacy.ValidatorEvidence, legacy.ValidatorEvidenceSource, legacy.ValidatorEvidenceCarry = nil, nil, nil
	legacy.Actions = slices.DeleteFunc(legacy.Actions, func(action Action) bool {
		return action.ID == validatorEvidenceDeployActionID || action.ID == validatorEvidenceAnchorActionID
	})
	for index := range legacy.Actions {
		action := &legacy.Actions[index]
		action.DependsOn = slices.DeleteFunc(slices.Clone(action.DependsOn), func(id string) bool {
			return id == validatorEvidenceDeployActionID || id == validatorEvidenceAnchorActionID
		})
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			t.Fatal(err)
		}
	}
	legacy.MaximumSpend, err = maximumActionSpend(legacy.Actions)
	if err != nil {
		t.Fatal(err)
	}
	legacy.PlanHash, err = legacy.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePlanBudget(&legacy); err != nil {
		t.Fatalf("complete pre-companion approval prerequisite: %v", err)
	}
	after, err := json.Marshal(original)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatalf("legacy projection mutated its current-plan owner: %v", err)
	}
	return &legacy
}

// The current approval is produced by the real carry renderer from reopened
// original signed transactions, journal records and actual HTTP observations.
func validatorEvidenceHistoricalSuccessorTest(t *testing.T, fixture validatorEvidenceCarryTestFixture) *SetupPlan {
	t.Helper()
	observed := fixture.authenticate(t)
	raw, err := json.Marshal(fixture.executor.plan)
	if err != nil {
		t.Fatal(err)
	}
	var current SetupPlan
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatal(err)
	}
	current.PriorPlanHashes = []string{fixture.executor.plan.PlanHash}
	if err := carryValidatorEvidencePlan(&current, fixture.executor.plan, observed); err != nil {
		t.Fatal(err)
	}
	if current.PlanHash == fixture.executor.plan.PlanHash || current.ValidatorEvidenceCarry == nil {
		t.Fatal("historical successor did not establish distinct current approval")
	}
	if err := validatePlanBudget(&current); err != nil {
		t.Fatalf("current carried approval prerequisite: %v", err)
	}
	if _, err := decodePersistedPlanBytes(raw); err == nil || !strings.Contains(err.Error(), "fresh validator evidence source") {
		t.Fatalf("historical original unexpectedly entered fresh execution: %v", err)
	}
	return &current
}

func TestValidatorEvidenceCarryHistoricalAncestorsRemainReadOnly(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, true)
	current := validatorEvidenceHistoricalSuccessorTest(t, fixture)
	original := fixture.executor.plan
	path := filepath.Join(fixture.executor.stateDir, "plans", stringsTrim0x(original.PlanHash)+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name string
		load func(string, *SetupPlan, string) (*SetupPlan, error)
	}{
		{name: "fleet-mirror", load: loadFleetMirrorLineagePlan},
		{name: "native-funding", load: loadSubstrateFundingLineagePlan},
		{name: "voluntary-conviction", load: loadVoluntaryConvictionLineagePlan},
	} {
		source, err := item.load(fixture.executor.stateDir, current, original.PlanHash)
		if err != nil || source == nil {
			t.Fatalf("historical %s ancestor was rejected: %v", item.name, err)
		}
		// The full canonical source wire is authority; JSON number Go types
		// may differ after reopening the authenticated archive.
		if source.PlanHash != original.PlanHash || !source.validatorEvidenceHistorical || !finalJSONEqual(source.ValidatorEvidenceSource, original.ValidatorEvidenceSource) {
			t.Fatalf("%s replaced original source authority", item.name)
		}
		foreign := *current
		foreign.PriorPlanHashes = nil
		if got, err := item.load(fixture.executor.stateDir, &foreign, original.PlanHash); err == nil || got != nil {
			t.Fatalf("%s accepted an unapproved ancestor", item.name)
		}
		if err := atomicWrite(path, append(bytes.Clone(raw), []byte("false")...), 0o600); err != nil {
			t.Fatal(err)
		}
		got, readErr := item.load(fixture.executor.stateDir, current, original.PlanHash)
		if err := atomicWrite(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if readErr == nil || got != nil {
			t.Fatalf("%s accepted altered archived bytes", item.name)
		}
	}
	if fixture.rpc.sends.Load() != 0 {
		t.Fatal("historical ancestor loading submitted a transaction")
	}
}

func TestValidatorEvidenceCarryHistoricalActionRecovery(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, true)
	current := validatorEvidenceHistoricalSuccessorTest(t, fixture)
	source := fixture.executor.plan
	gasAction := actionByID(t, source, validatorEvidenceDeployActionID)
	batchAction := actionByID(t, source, "fleet.install.batch.1")
	entries := []JournalEntry{
		{DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: gasAction.ID, IntentHash: gasAction.IntentHash, Stage: StageVerified},
		{DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: batchAction.ID, IntentHash: batchAction.IntentHash, Stage: StageVerified},
	}
	revised := *current
	revised.Actions = slices.Clone(current.Actions)
	if err := preserveVerifiedEVMGasReallocations(fixture.executor.stateDir, &revised, current, entries); err != nil {
		t.Fatalf("historical gas source was rejected: %v", err)
	}
	if err := preserveVerifiedFleetBatchActions(fixture.executor.cfg, fixture.executor.stateDir, &revised, current, entries); err != nil {
		t.Fatalf("historical fleet source was rejected: %v", err)
	}
	if !finalJSONEqual(actionByID(t, &revised, gasAction.ID), gasAction) || !finalJSONEqual(actionByID(t, &revised, batchAction.ID), batchAction) || !finalJSONEqual(revised.MaximumSpend, current.MaximumSpend) {
		t.Fatal("historical recovery changed source action identity or charged gas again")
	}
	executor := *fixture.executor
	executor.plan = current
	borrowed, action, err := executor.carriedFleetBatchSourceExecutor(batchAction, entries[1])
	if err != nil || borrowed == nil || borrowed == &executor || borrowed.plan.PlanHash != source.PlanHash || !finalJSONEqual(action, batchAction) {
		t.Fatalf("historical source executor was rejected: %v", err)
	}
	for _, fault := range []string{"plan", "intent", "target"} {
		entry, candidate := entries[1], batchAction
		switch fault {
		case "plan":
			entry.PlanHash = common.HexToHash("0xf1").Hex()
		case "intent":
			entry.IntentHash = common.HexToHash("0xf2").Hex()
		case "target":
			candidate.Target = common.HexToAddress("0xf3").Hex()
		}
		if owner, _, err := executor.carriedFleetBatchSourceExecutor(candidate, entry); err == nil || owner != nil {
			t.Fatalf("%s source executor drift was accepted", fault)
		}
	}
}

func TestValidatorEvidenceCarryHistoricalCommitmentRecovery(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, true)
	current := validatorEvidenceHistoricalSuccessorTest(t, fixture)
	source := fixture.executor.plan
	for _, id := range []string{"fleet.commitment.1", "fleet.refresh.commitment.1"} {
		action := actionByID(t, source, id)
		coordinates, err := fleetCommitmentActionCoordinates(fixture.executor.cfg, action, false)
		if err != nil {
			t.Fatal(err)
		}
		evidence := FleetCommitmentEvidence{Schema: fleetCommitmentEvidenceSchemaV2, ExtrinsicHash: common.HexToHash("0x31").Hex(), FinalizedBlock: 90, FinalizedBlockHash: testEVMHead(90, 0x32).Hash}
		entries := []JournalEntry{
			{Sequence: 1, DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: id, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: evidence.ExtrinsicHash, BlockNumber: evidence.FinalizedBlock, BlockHash: evidence.FinalizedBlockHash},
			{Sequence: 2, DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: id, IntentHash: action.IntentHash, Stage: StageVerified},
		}
		actual, err := verifiedFleetCommitmentEvidenceAction(fixture.executor.stateDir, fixture.executor.cfg, current, entries, coordinates, &evidence)
		// DecimalUint's Go zero and decoded JSON zero are the same exact
		// approved native-action gas amount, not different actions.
		if err != nil || !finalJSONEqual(actual, action) {
			t.Fatalf("historical commitment source was rejected for %s: %v", id, err)
		}
		for _, fault := range []string{"lineage", "finalization", "verification", "generation"} {
			changed := slices.Clone(entries)
			expected := coordinates
			switch fault {
			case "lineage":
				changed[0].PlanHash = common.HexToHash("0x41").Hex()
			case "finalization":
				changed[0].BlockHash = common.HexToHash("0x42").Hex()
			case "verification":
				changed[1].IntentHash = common.HexToHash("0x43").Hex()
			case "generation":
				expected.Generation++
			}
			if _, err := verifiedFleetCommitmentEvidenceAction(fixture.executor.stateDir, fixture.executor.cfg, current, changed, expected, &evidence); err == nil {
				t.Fatalf("%s %s historical commitment drift was accepted", id, fault)
			}
		}
	}
}

func TestValidatorEvidenceCarryHistoricalCaptureCensus(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, true)
	current := validatorEvidenceHistoricalSuccessorTest(t, fixture)
	deployment := current.Deployment
	deployment.DeployBlock, deployment.DeployBlockHash = 100, testEVMHead(100, 0xee).Hash
	batcher := fixture.executor.payloads.FleetBatcherAddress
	actual, err := finalCaptureReleaseContractCensusFromState(fixture.executor.stateDir, current, &deployment, batcher)
	if err != nil {
		t.Fatalf("historical release capture ancestor was rejected: %v", err)
	}
	if actual.fromBlock != 100 || len(actual.currentAddresses) != 4 || !slices.Equal(actual.currentAddresses, actual.releaseAddresses) || len(actual.queryAddresses) != 4 {
		t.Fatalf("historical capture changed the exact original emitter census: %+v", actual)
	}
	foreign := *current
	foreign.PriorPlanHashes = append(slices.Clone(current.PriorPlanHashes), common.HexToHash("0x51").Hex())
	if _, err := finalCaptureReleaseContractCensusFromState(fixture.executor.stateDir, &foreign, &deployment, batcher); err == nil {
		t.Fatal("historical capture accepted a missing approved predecessor")
	}
}

// Real code and an exact canonical implementation slot are returned only for
// the initializer's configured block and original approved deployment.
type validatorEvidenceHistoricalBaselineRPC struct {
	*validatorEvidenceCarryRPC
	proxy, implementation         common.Address
	proxyCode, implementationCode []byte
	fault                         string
}

func (self *validatorEvidenceHistoricalBaselineRPC) GetCode(_ context.Context, address common.Address, selector finalEVMBlockSelector) (hexutil.Bytes, error) {
	if _, err := self.selected(selector); err != nil || selector.BlockHash != testEVMHead(100, 0xee).Hash {
		return nil, errors.New("baseline code used another canonical block")
	}
	var code []byte
	switch address {
	case self.proxy:
		code = bytes.Clone(self.proxyCode)
	case self.implementation:
		code = bytes.Clone(self.implementationCode)
	default:
		return nil, errors.New("baseline code used another address")
	}
	if self.fault == "runtime" && address == self.implementation {
		code[0] ^= 1
	}
	return code, nil
}

func (self *validatorEvidenceHistoricalBaselineRPC) GetStorageAt(_ context.Context, address common.Address, slot common.Hash, selector finalEVMBlockSelector) (hexutil.Bytes, error) {
	if _, err := self.selected(selector); err != nil || selector.BlockHash != testEVMHead(100, 0xee).Hash || address != self.proxy || slot != common.HexToHash(erc1967ImplementationSlot) {
		return nil, errors.New("baseline slot used another canonical block or identity")
	}
	word := abiWordAddress(self.implementation)
	if self.fault == "slot" {
		word[31] ^= 1
	}
	return word, nil
}

func TestValidatorEvidenceCarryHistoricalCoordinatorBaseline(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryModeTestFixture(t, true, true)
	current := validatorEvidenceHistoricalSuccessorTest(t, fixture)
	source, payloads := fixture.executor.plan, fixture.executor.payloads
	action := actionByID(t, source, "evm.coordinator-proxy")
	role, err := fixture.executor.roles.EVMKey("deployer")
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(role.PrivateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	maximumGas, _, err := evmActionFeeEnvelope(action)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateApprovedEVMTransactionFields(action, payloads.Deployer, source.Deployment.InitialNonce+4, nil, new(big.Int), payloads.Proxy); err != nil {
		t.Fatalf("actual proxy transaction envelope prerequisite: %v", err)
	}
	transaction, err := types.SignNewTx(key, types.LatestSignerForChainID(new(big.Int).SetUint64(source.ChainID)), &types.LegacyTx{Nonce: source.Deployment.InitialNonce + 4, Value: new(big.Int), Gas: min(maximumGas, 5_000_000), GasPrice: big.NewInt(1), Data: payloads.Proxy})
	if err != nil {
		t.Fatal(err)
	}
	head := testEVMHead(100, 0xee)
	if err := fixture.executor.journal.Append(JournalEntry{DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: transaction.Hash().Hex(), BlockNumber: head.Number, BlockHash: head.Hash}); err != nil {
		t.Fatal(err)
	}
	log := finalCanonicalEVMLog{
		Address: strings.ToLower(source.Deployment.CoordinatorProxy.Hex()), Topics: []string{strings.ToLower(crypto.Keccak256Hash([]byte("Upgraded(address)")).Hex()), common.BytesToHash(source.Deployment.CoordinatorImplementation.Bytes()).Hex()}, Data: "0x",
		BlockNumber: head.Number, BlockHash: head.Hash, TransactionHash: transaction.Hash().Hex(),
	}
	for _, fault := range []string{"", "slot", "runtime"} {
		peer := &validatorEvidenceHistoricalBaselineRPC{validatorEvidenceCarryRPC: fixture.rpc, proxy: source.Deployment.CoordinatorProxy, implementation: source.Deployment.CoordinatorImplementation, proxyCode: payloads.ExpectedRuntime[source.Deployment.CoordinatorProxy], implementationCode: payloads.ExpectedRuntime[source.Deployment.CoordinatorImplementation], fault: fault}
		if len(peer.proxyCode) == 0 || len(peer.implementationCode) == 0 {
			t.Fatal("baseline fixture omitted the actual generated deployment")
		}
		server := rpc.NewServer()
		if err := server.RegisterName("eth", peer); err != nil {
			t.Fatal(err)
		}
		httpServer := httptest.NewServer(server)
		cfg := *fixture.executor.cfg
		cfg.OperationalEVM = httpServer.URL
		baselines, readErr := captureFinalHistoricalCoordinatorBaselines(t.Context(), &cfg, fixture.executor.stateDir, current, testEVMHead(110, 0xf0), []finalCanonicalEVMLog{log})
		httpServer.Close()
		server.Stop()
		if fault == "" {
			if readErr != nil || len(baselines) != 1 || baselines[0].Head != head || baselines[0].ImplementationRuntimeHash != strings.ToLower(source.Deployment.RuntimeHashes[source.Deployment.CoordinatorImplementation.Hex()]) {
				t.Fatalf("historical coordinator baseline ancestor was rejected: %v", readErr)
			}
		} else if readErr == nil || baselines != nil {
			t.Fatalf("%s baseline drift produced historical authority: %v", fault, readErr)
		}
	}
	if fixture.rpc.sends.Load() != 0 {
		t.Fatal("historical baseline capture submitted a transaction")
	}
}
