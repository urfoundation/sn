// Preserves original immutable journal authority through real revision,
// signed-envelope recovery and canonical finalized HTTP readback.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

func TestValidatorEvidenceCarryAuthenticatesOriginalApprovalAndRuntime(t *testing.T) {
	t.Parallel()
	for _, historical := range []bool{false, true} {
		fixture := newValidatorEvidenceCarryTestFixture(t, historical)
		observed := fixture.authenticate(t)
		plan := fixture.executor.plan
		if observed.reference.SourcePlanHash != plan.PlanHash || observed.reference.SourceReleaseLockHash != plan.ReleaseLockHash || observed.reference.Creation.TransactionHash != fixture.creation.Hash().Hex() || observed.reference.Anchor.TransactionHash != fixture.anchor.Hash().Hex() || !bytes.Equal(observed.payloads.Runtime, fixture.executor.payloads.ValidatorEvidence.Runtime) {
			t.Fatalf("historical=%t original signed/approved identity was not retained", historical)
		}
		if historical {
			if err := validateValidatorEvidenceSource(plan, false); err == nil {
				t.Fatal("historical compiler bytes entered the fresh-install path")
			}
			if observed.payloads.Artifact.RuntimeBytecodeHash == ValidatorEvidenceRuntimeBytecodeHash {
				t.Fatal("historical fixture did not differ from current generated runtime")
			}
		}
		if fixture.rpc.sends.Load() != 0 {
			t.Fatal("read-only carry submitted a transaction")
		}
	}
}

func TestValidatorEvidenceCarryRejectsIncompleteOriginalJournal(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, false)
	fixture.authenticate(t)
	executor := fixture.executor
	for _, fault := range []string{"empty", "broadcast", "finalized", "verified", "duplicate-finalized", "competing-intent", "verification-order", "cross-action-order"} {
		entries := executor.journal.Entries()
		switch fault {
		case "empty":
			entries = nil
		case "broadcast", "finalized", "verified":
			stage := map[string]JournalStage{"broadcast": StageBroadcast, "finalized": StageFinalized, "verified": StageVerified}[fault]
			entries = slices.DeleteFunc(entries, func(entry JournalEntry) bool {
				return entry.ActionID == validatorEvidenceDeployActionID && entry.Stage == stage
			})
		case "duplicate-finalized":
			for _, entry := range entries {
				if entry.ActionID == validatorEvidenceDeployActionID && entry.Stage == StageFinalized {
					entries = append(entries, entry)
					break
				}
			}
		case "competing-intent":
			entries[1].IntentHash = common.HexToHash("0xab").Hex()
		case "verification-order":
			for index := range entries {
				if entries[index].ActionID == validatorEvidenceDeployActionID && entries[index].Stage == StageVerified {
					entries[index].Sequence = 1
				}
			}
		case "cross-action-order":
			for index := range entries {
				if entries[index].ActionID == validatorEvidenceAnchorActionID && entries[index].Stage == StageBroadcast {
					entries[index].Sequence = 2
				}
			}
		}
		before := fixture.rpc.calls.Load()
		observed, err := authenticateValidatorEvidenceCarry(t.Context(), executor.cfg, executor.stateDir, executor.plan, entries, executor.deployer.client, nil)
		if err == nil || observed != nil || fixture.rpc.calls.Load() != before {
			t.Fatalf("%s incomplete source history reached chain authority: %v", fault, err)
		}
	}
	fixture.authenticate(t)
}

func TestValidatorEvidenceCarryRefusesArchivedSourceSubstitution(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, true)
	fixture.authenticate(t)
	executor := fixture.executor
	path := filepath.Join(executor.stateDir, "plans", stringsTrim0x(executor.plan.PlanHash)+".json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"lock", "artifact", "runtime", "immutable", "source-schema", "plan-hash"} {
		var changed SetupPlan
		if err := json.Unmarshal(original, &changed); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "lock":
			changed.ValidatorEvidenceSource.ReleaseLock.Runtime.SpecVersion++
		case "artifact":
			changed.ValidatorEvidenceSource.Artifact.ABI = strings.Replace(changed.ValidatorEvidenceSource.Artifact.ABI, `"coordinator"`, `"anotherCoordinator"`, 1)
		case "runtime":
			changed.ValidatorEvidenceSource.Artifact.RuntimeBytecodeHash = common.HexToHash("0x99").Hex()
		case "immutable":
			changed.ValidatorEvidenceSource.Artifact.ImmutableReferences["coordinator"][0]++
		case "source-schema":
			changed.ValidatorEvidenceSource.Schema += "-other"
		case "plan-hash":
			changed.PlanHash = common.HexToHash("0x99").Hex()
		}
		raw, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(bytes.TrimSpace(original), raw) {
			t.Fatalf("%s source mutation did not change original bytes", fault)
		}
		if err := atomicWrite(path, append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		before := fixture.rpc.calls.Load()
		observed, err := authenticateValidatorEvidenceCarry(t.Context(), executor.cfg, executor.stateDir, executor.plan, executor.journal.Entries(), executor.deployer.client, nil)
		if err == nil || observed != nil || fixture.rpc.calls.Load() != before {
			t.Fatalf("%s archived source substitution crossed original approval: %v", fault, err)
		}
		if err := atomicWrite(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fixture.authenticate(t)
}

func TestValidatorEvidenceCarryRejectsConflictingFinalizedHTTPHistory(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, true)
	fixture.authenticate(t)
	executor := fixture.executor
	for _, fault := range []string{"chain", "reorg", "runtime", "anchor", "padding", "missing-receipt", "reverted", "receipt-address", "missing-transaction", "pending"} {
		peer := &validatorEvidenceCarryRPC{base: fixture.rpc.base, transactions: fixture.rpc.transactions, inclusions: fixture.rpc.inclusions, creation: fixture.rpc.creation, chainID: fixture.rpc.chainID, fault: fault}
		if fault == "chain" {
			peer.chainID++
		}
		client, _ := peer.client(t)
		observed, err := authenticateValidatorEvidenceCarry(t.Context(), executor.cfg, executor.stateDir, executor.plan, executor.journal.Entries(), client, nil)
		if err == nil || observed != nil || peer.sends.Load() != 0 {
			t.Fatalf("%s live source conflict was accepted: %v", fault, err)
		}
	}
}

func TestValidatorEvidenceCarryOriginalSignedEnvelopesRemainExact(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, true)
	fixture.authenticate(t)
	executor := fixture.executor
	for _, id := range []string{validatorEvidenceDeployActionID, validatorEvidenceAnchorActionID} {
		action := actionByID(t, executor.plan, id)
		reference, broadcast, _, err := validatorEvidenceHistoryReceipt(executor.plan, executor.journal.Entries(), action)
		if err != nil {
			t.Fatal(err)
		}
		original, err := readValidatorEvidenceSourceTransaction(executor.stateDir, executor.plan, action, reference, broadcast)
		if err != nil {
			t.Fatalf("actual signed source prerequisite: %v", err)
		}
		label := "deployer"
		if id == validatorEvidenceAnchorActionID {
			label = "testnet-owner"
		}
		role, err := executor.roles.EVMKey(label)
		if err != nil {
			t.Fatal(err)
		}
		originalKey, err := crypto.HexToECDSA(role.PrivateKeyHex)
		if err != nil {
			t.Fatal(err)
		}
		chain := new(big.Int).SetUint64(executor.plan.ChainID)
		dynamic, err := types.SignNewTx(originalKey, types.LatestSignerForChainID(chain), &types.DynamicFeeTx{ChainID: chain, Nonce: original.Nonce(), To: original.To(), Value: original.Value(), Data: original.Data(), Gas: original.Gas(), GasFeeCap: big.NewInt(1), GasTipCap: big.NewInt(1)})
		if err != nil {
			t.Fatal(err)
		}
		dynamicWire, err := dynamic.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(executor.stateDir, "transactions", stringsTrim0x(dynamic.Hash().Hex())+".rlp"), dynamicWire, 0o600); err != nil {
			t.Fatal(err)
		}
		dynamicReference, dynamicBroadcast := reference, broadcast
		dynamicReference.TransactionHash, dynamicBroadcast.TransactionHash = dynamic.Hash().Hex(), dynamic.Hash().Hex()
		if accepted, err := readValidatorEvidenceSourceTransaction(executor.stateDir, executor.plan, action, dynamicReference, dynamicBroadcast); err != nil || accepted == nil || accepted.Hash() != dynamic.Hash() {
			t.Fatalf("%s actual protected dynamic-fee prerequisite: %v", id, err)
		}
		for _, fault := range []string{"signer", "chain", "unprotected", "target", "value", "data", "broadcast-nonce", "gas", "fee", "type"} {
			key, chain := originalKey, new(big.Int).SetUint64(executor.plan.ChainID)
			to, value, data := original.To(), new(big.Int).Set(original.Value()), bytes.Clone(original.Data())
			gas, fee := original.Gas(), original.GasPrice()
			maximumGas, maximumFee, err := evmActionFeeEnvelope(action)
			if err != nil {
				t.Fatal(err)
			}
			var signer types.Signer = types.LatestSignerForChainID(chain)
			switch fault {
			case "signer":
				key, err = crypto.ToECDSA(common.LeftPadBytes([]byte{9}, 32))
				if err != nil {
					t.Fatal(err)
				}
			case "chain":
				chain.Add(chain, big.NewInt(1))
				signer = types.LatestSignerForChainID(chain)
			case "unprotected":
				signer = types.HomesteadSigner{}
			case "target":
				other := common.HexToAddress("0x1111111111111111111111111111111111111111")
				to = &other
			case "value":
				value.SetUint64(1)
			case "data":
				data[len(data)-1] ^= 1
			case "gas":
				gas = maximumGas + 1
			case "fee":
				fee = new(big.Int).Add(new(big.Int).SetUint64(maximumFee), big.NewInt(1))
			}
			var unsigned types.TxData = &types.LegacyTx{Nonce: original.Nonce(), To: to, Value: value, Data: data, Gas: gas, GasPrice: fee}
			if fault == "type" {
				unsigned = &types.AccessListTx{ChainID: chain, Nonce: original.Nonce(), To: to, Value: value, Data: data, Gas: gas, GasPrice: fee}
			}
			transaction, err := types.SignNewTx(key, signer, unsigned)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := transaction.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			if err := atomicWrite(filepath.Join(executor.stateDir, "transactions", stringsTrim0x(transaction.Hash().Hex())+".rlp"), wire, 0o600); err != nil {
				t.Fatal(err)
			}
			changedReference, changedBroadcast := reference, broadcast
			changedReference.TransactionHash, changedBroadcast.TransactionHash = transaction.Hash().Hex(), transaction.Hash().Hex()
			changedBroadcast.Signer = crypto.PubkeyToAddress(key.PublicKey).Hex()
			if fault == "broadcast-nonce" {
				changedBroadcast.Nonce = strconv.FormatUint(original.Nonce()+1, 10)
			}
			if accepted, err := readValidatorEvidenceSourceTransaction(executor.stateDir, executor.plan, action, changedReference, changedBroadcast); err == nil || accepted != nil {
				t.Fatalf("%s/%s signed attack self-authorized: %v", id, fault, err)
			}
		}
	}
}

func TestValidatorEvidenceCarryRevisionRetainsOriginalActionsAndSpend(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, true)
	observed := fixture.authenticate(t)
	executor := fixture.executor
	prior := *executor.plan
	prior.validatorEvidenceObserved = observed
	current := prior.LiveFacts
	current.DeployerNonce = prior.ValidatorEvidence.DeployerNonce + 1
	before, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	revised, err := buildPlanRevisionFromFacts(executor.cfg, executor.stateDir, &prior, &current, executor.journal.Entries(), time.Unix(2, 0))
	if err != nil || revised == nil {
		t.Fatalf("actual authenticated revision: %v", err)
	}
	if revised.ValidatorEvidenceCarry == nil || *revised.ValidatorEvidenceCarry != observed.reference || *revised.ValidatorEvidence != *prior.ValidatorEvidence || !slices.Contains(revised.PriorPlanHashes, observed.reference.SourcePlanHash) {
		t.Fatal("revision did not retain original companion approval")
	}
	for _, id := range []string{validatorEvidenceDeployActionID, validatorEvidenceAnchorActionID} {
		if !reflect.DeepEqual(actionByID(t, revised, id), actionByID(t, &prior, id)) {
			t.Fatalf("revision changed original %s envelope or spend", id)
		}
	}
	remaining, err := remainingPlanSpend(revised, executor.journal.Entries())
	if err != nil {
		t.Fatal(err)
	}
	want, err := subtractDecimalUints(revised.MaximumSpend.EVMGasWei, actionByID(t, &prior, validatorEvidenceDeployActionID).Spend.EVMGasWei, actionByID(t, &prior, validatorEvidenceAnchorActionID).Spend.EVMGasWei)
	if err != nil || remaining.EVMGasWei != want {
		t.Fatalf("carried original transactions were charged again: %v", err)
	}
	after, err := json.Marshal(prior)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("read-only revision mutated the original approval: %v", err)
	}
	raw, err := json.Marshal(revised)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := decodePersistedPlanBytes(raw)
	if err != nil {
		t.Fatalf("persisted carry approval restart: %v", err)
	}
	if _, err := authenticateValidatorEvidenceCarry(t.Context(), executor.cfg, executor.stateDir, restarted, executor.journal.Entries(), executor.deployer.client, nil); err != nil {
		t.Fatalf("actual carry restart readback: %v", err)
	}
}

func TestValidatorEvidenceCarryExecutorNeverRedeploysOrReanchors(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, false)
	observed := fixture.authenticate(t)
	executor := fixture.executor
	raw, err := json.Marshal(executor.plan)
	if err != nil {
		t.Fatal(err)
	}
	var revised SetupPlan
	if err := json.Unmarshal(raw, &revised); err != nil {
		t.Fatal(err)
	}
	revised.PriorPlanHashes = []string{executor.plan.PlanHash}
	if err := carryValidatorEvidencePlan(&revised, executor.plan, observed); err != nil {
		t.Fatal(err)
	}
	if err := validatePlanBudget(&revised); err != nil {
		t.Fatal(err)
	}
	executor.plan = &revised
	beforeEntries := executor.journal.Entries()
	for _, id := range []string{validatorEvidenceDeployActionID, validatorEvidenceAnchorActionID} {
		action := actionByID(t, &revised, id)
		if id == validatorEvidenceDeployActionID {
			err = executor.executeDeployment(t.Context(), action)
		} else {
			err = executor.anchorValidatorEvidence(t.Context(), action)
		}
		if err != nil {
			t.Fatalf("verify-only carried %s: %v", id, err)
		}
		action.Target = common.HexToAddress("0x11").Hex()
		if err := executor.verifyValidatorEvidenceCarryAction(t.Context(), action); err == nil {
			t.Fatal("cached carry admitted a changed action envelope")
		}
	}
	if fixture.rpc.sends.Load() != 0 || !reflect.DeepEqual(beforeEntries, executor.journal.Entries()) {
		t.Fatal("carried action submitted or journalled another transaction")
	}
	journal := executor.journal
	executor.journal = nil
	err = executor.anchorValidatorEvidence(t.Context(), actionByID(t, &revised, validatorEvidenceAnchorActionID))
	executor.journal = journal
	if err == nil {
		t.Fatal("cached payload bypassed missing source journal")
	}
}

func TestValidatorEvidenceCarryNonceSuffixIsConsumedExactlyOnce(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, false)
	observed := fixture.authenticate(t)
	prior := *fixture.executor.plan
	prior.validatorEvidenceObserved = observed
	batcher := actionByID(t, &prior, "fleet.refresh.deploy-batcher")
	entries := append(fixture.executor.journal.Entries(), JournalEntry{PlanHash: prior.PlanHash, ActionID: batcher.ID, IntentHash: batcher.IntentHash, Stage: StageVerified})
	boundary, _, err := repeatedCoordinatorUpgradeBoundary(&prior, entries)
	if err != nil || boundary != prior.ValidatorEvidence.DeployerNonce+1 {
		t.Fatalf("original companion nonce was not consumed once: %d %v", boundary, err)
	}
	if _, _, err := repeatedCoordinatorUpgradeBoundary(&prior, fixture.executor.journal.Entries()); err == nil {
		t.Fatal("companion suffix invented missing batcher predecessor authority")
	}
	for _, next := range []uint64{boundary, boundary + 7} {
		got, err := validatorEvidenceConsumedNonceBoundary(&prior, next)
		if err != nil || got != next {
			t.Fatalf("later generation counted original CREATE again: %d %v", got, err)
		}
	}
	if _, err := validatorEvidenceConsumedNonceBoundary(&prior, prior.ValidatorEvidence.DeployerNonce-1); err == nil {
		t.Fatal("nonce gap was silently counted as consumed")
	}
	payloads, err := buildDeploymentPayloads(fixture.executor.cfg, fixture.executor.roles, prior.Deployment.InitialNonce)
	if err != nil {
		t.Fatal(err)
	}
	if err := bindValidatorEvidenceCarryPayloads(payloads, observed); err != nil {
		t.Fatal(err)
	}
	for _, nonce := range []uint64{boundary, boundary + 9} {
		if err := configureCoordinatorUpgradeNonce(payloads, nonce); err != nil {
			t.Fatalf("authenticated repeated upgrade suffix: %v", err)
		}
		if payloads.ValidatorEvidence.Manifest != *prior.ValidatorEvidence || !bytes.Equal(payloads.ValidatorEvidence.Runtime, observed.payloads.Runtime) {
			t.Fatal("repeated upgrade rewrote original journal identity")
		}
	}
	before, err := json.Marshal(payloads)
	if err != nil {
		t.Fatal(err)
	}
	for _, apply := range []func() error{func() error { return configureCoordinatorUpgradeNonce(payloads, prior.ValidatorEvidence.DeployerNonce) }, func() error { return configurePrecompileProbeNonce(payloads, prior.ValidatorEvidence.DeployerNonce) }} {
		if err := apply(); err == nil {
			t.Fatal("new CREATE aliases authenticated original journal")
		}
		after, err := json.Marshal(payloads)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("collision refusal partially rebound payloads: %v", err)
		}
	}
}

func TestValidatorEvidenceCarryObservedUpgradeAndContinuationPreserveOriginalJournal(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryUpgradeTestFixture(t, true, false, true)
	observed := fixture.authenticate(t)
	executor := fixture.executor
	prior := *executor.plan
	prior.validatorEvidenceObserved = observed
	originalManifest, originalReference := *prior.ValidatorEvidence, observed.reference
	originalActions := []Action{actionByID(t, &prior, validatorEvidenceDeployActionID), actionByID(t, &prior, validatorEvidenceAnchorActionID)}
	if err := saveContractDeployment(executor.stateDir, prior.Deployment); err != nil {
		t.Fatal(err)
	}
	activePayloads := executor.payloads
	entries := executor.journal.Entries()
	// Coordinator progress is the existing observer's verified-facts input.
	// Companion authority always comes from its actual reopened signed journal
	// and HTTP receipts, never these unrelated coordinator stage declarations.
	for generation := 0; generation < 2; generation++ {
		for _, id := range []string{"evm.coordinator-upgrade-implementation", "evm.coordinator-upgrade-activate", "fleet.refresh.deploy-batcher"} {
			action := actionByID(t, &prior, id)
			entries = append(entries, JournalEntry{DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash, ActionID: id, IntentHash: action.IntentHash, Stage: StageVerified})
		}
		boundary, _, err := repeatedCoordinatorUpgradeBoundary(&prior, entries)
		if err != nil {
			t.Fatal(err)
		}
		wantBoundary := prior.CoordinatorUpgrade.DeployerNonce + 2
		if generation == 0 {
			wantBoundary++
		}
		if boundary != wantBoundary {
			t.Fatalf("generation %d consumed original journal nonce again: got=%d want=%d", generation, boundary, wantBoundary)
		}
		codes := maps.Clone(activePayloads.ExpectedRuntime)
		codes[activePayloads.FleetBatcherAddress] = bytes.Clone(activePayloads.FleetBatcherRuntime)
		codes[crypto.CreateAddress(activePayloads.Deployer, boundary)] = nil
		codes[crypto.CreateAddress(activePayloads.Deployer, boundary+1)] = nil
		peer := &validatorEvidenceCarryUpgradeRPC{
			validatorEvidenceCarryRPC: &validatorEvidenceCarryRPC{base: fixture.rpc.base, transactions: fixture.rpc.transactions, inclusions: fixture.rpc.inclusions, creation: fixture.rpc.creation, chainID: fixture.rpc.chainID},
			codes:                     codes, nonce: boundary, deployer: activePayloads.Deployer, proxy: prior.Deployment.CoordinatorProxy, active: prior.CoordinatorUpgrade.Implementation, drill: prior.Deployment.GovernanceDrillImplementation,
		}
		_, endpoint := peer.client(t)
		cfg := *executor.cfg
		cfg.OperationalEVM = endpoint
		current := prior.LiveFacts
		current.DeployerNonce, current.EVMFinalizedBlock = boundary, 110
		current.EVMFinalizedBlockHash = testEVMHead(110, 0xf0).Hash
		migration, err := observeCoordinatorUpgradeMigration(t.Context(), &cfg, executor.stateDir, &prior, &current, entries, executor.roles)
		if err != nil || migration == nil {
			t.Fatalf("generation %d actual HTTP upgrade observation: %v", generation, err)
		}
		if generation == 0 && (migration.Upgrade.DeployerNonce != boundary || migration.Upgrade == prior.CoordinatorUpgrade || !migration.Baseline.isRepeated()) {
			t.Fatal("changed executable did not require the exact new authenticated upgrade")
		}
		if generation == 1 && (migration.Upgrade != prior.CoordinatorUpgrade || migration.Baseline != prior.CoordinatorUpgradeBaseline) {
			t.Fatal("unchanged continuation allocated another coordinator upgrade")
		}
		revised, err := buildPlanRevisionFromFactsWithMigration(&cfg, executor.stateDir, &prior, &current, entries, time.Unix(int64(generation+2), 0), migration)
		if err != nil || revised == nil {
			t.Fatalf("generation %d real revision after HTTP observation: %v", generation, err)
		}
		if revised.CoordinatorUpgrade != migration.Upgrade || !reflect.DeepEqual(revised.Deployment, prior.Deployment) || revised.ValidatorEvidenceCarry == nil || *revised.ValidatorEvidenceCarry != originalReference || *revised.ValidatorEvidence != originalManifest {
			t.Fatalf("generation %d six-contract fast path lost the authenticated upgrade or original journal", generation)
		}
		for _, action := range originalActions {
			if !reflect.DeepEqual(action, actionByID(t, revised, action.ID)) {
				t.Fatalf("generation %d rewrote original action %s", generation, action.ID)
			}
		}
		if err := writeRunInputs(&cfg, executor.stateDir, revised, executor.roles); err != nil {
			t.Fatalf("generation %d real source archival: %v", generation, err)
		}
		wire, err := os.ReadFile(filepath.Join(executor.stateDir, "plan.json"))
		if err != nil {
			t.Fatal(err)
		}
		restarted, err := decodePersistedPlanBytes(wire)
		if err != nil {
			t.Fatalf("generation %d approved carry restart: %v", generation, err)
		}
		fresh, err := authenticateValidatorEvidenceCarry(t.Context(), executor.cfg, executor.stateDir, restarted, executor.journal.Entries(), executor.deployer.client, nil)
		if err != nil || fresh == nil {
			t.Fatalf("generation %d original journal/receipt restart: %v", generation, err)
		}
		restarted.validatorEvidenceObserved = fresh
		prior = *restarted
		activePayloads, err = buildDeploymentPayloads(executor.cfg, executor.roles, prior.Deployment.InitialNonce)
		if err != nil {
			t.Fatal(err)
		}
		if err := bindValidatorEvidenceCarryPayloads(activePayloads, fresh); err != nil {
			t.Fatal(err)
		}
		if err := configureCoordinatorUpgradeNonce(activePayloads, prior.CoordinatorUpgrade.DeployerNonce); err != nil {
			t.Fatal(err)
		}
		if peer.sends.Load() != 0 {
			t.Fatal("carry/revision observer sent a transaction")
		}
	}
}

func TestValidatorEvidenceCarryCanceledReadReturnsNoAuthority(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, false)
	fixture.authenticate(t)
	executor := fixture.executor
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := fixture.rpc.calls.Load()
	if got, err := authenticateValidatorEvidenceCarry(ctx, executor.cfg, executor.stateDir, executor.plan, executor.journal.Entries(), executor.deployer.client, nil); got != nil || !errors.Is(err, context.Canceled) || fixture.rpc.calls.Load() != before {
		t.Fatalf("pre-canceled carry produced authority or RPC: %v", err)
	}
	entered, joined := make(chan struct{}), make(chan struct{})
	peer := &validatorEvidenceCarryRPC{base: fixture.rpc.base, transactions: fixture.rpc.transactions, inclusions: fixture.rpc.inclusions, creation: fixture.rpc.creation, chainID: fixture.rpc.chainID, codeHook: func(ctx context.Context) error { close(entered); defer close(joined); <-ctx.Done(); return ctx.Err() }}
	client, _ := peer.client(t)
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	type result struct {
		value *validatorEvidenceCarryObservation
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := authenticateValidatorEvidenceCarry(ctx, executor.cfg, executor.stateDir, executor.plan, executor.journal.Entries(), client, nil)
		done <- result{value: value, err: err}
	}()
	select {
	case <-entered:
	case actual := <-done:
		t.Fatalf("carry returned before the actual HTTP cancellation barrier: %v", actual.err)
	}
	cancel()
	actual := <-done
	<-joined
	if actual.value != nil || !errors.Is(actual.err, context.Canceled) || peer.sends.Load() != 0 {
		t.Fatalf("in-flight cancellation published carry authority: %v", actual.err)
	}
}

func TestValidatorEvidenceCarrySourceTransportBoundsPrecedeDecoding(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "owner")
	if err := ensurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(dir, "exact.json"), []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	if raw, err := readValidatorEvidenceHistoricalFile(dir, "exact.json", 4); err != nil || string(raw) != "1234" {
		t.Fatalf("exact source byte bound: %v", err)
	}
	for _, input := range []struct {
		name    string
		maximum int64
	}{{name: "exact.json", maximum: 3}, {name: "exact.json", maximum: 0}, {name: "../exact.json", maximum: 4}, {name: "exact.json", maximum: maximumCampaignEvidenceRawFileBytes + 1}} {
		if raw, err := readValidatorEvidenceHistoricalFile(dir, input.name, input.maximum); err == nil || raw != nil {
			t.Fatalf("source byte/path admission accepted %+v: %v", input, err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape.json")); err != nil {
		t.Fatal(err)
	}
	if raw, err := readValidatorEvidenceHistoricalFile(dir, "escape.json", 4); err == nil || raw != nil {
		t.Fatal("source descriptor escaped its exact owner")
	}
}

// Both providers must agree when the deployment mode requires independence.
// The original source postconditions cannot be relabelled into another mode.
func TestValidatorEvidenceCarryIndependentModeCannotBorrowPublicReceipts(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, false)
	fixture.authenticate(t)
	executor := fixture.executor
	cfg := *executor.cfg
	cfg.OperationalRPCMode = rpcModePrivateAuthority
	for _, independent := range []*ethclient.Client{nil, executor.deployer.client} {
		before := fixture.rpc.calls.Load()
		got, err := authenticateValidatorEvidenceCarry(t.Context(), &cfg, executor.stateDir, executor.plan, executor.journal.Entries(), executor.deployer.client, independent)
		if err == nil || got != nil || fixture.rpc.calls.Load() != before {
			t.Fatalf("independent mode borrowed public-only original proof: %v", err)
		}
	}
}

func TestValidatorEvidenceCarryAuthenticatesBothIndependentProviders(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryModeTestFixture(t, true, true)
	fixture.authenticate(t)
	if fixture.independent == nil || fixture.independent.calls.Load() == 0 {
		t.Fatal("private carry did not use its separate actual HTTP reader")
	}
	executor := fixture.executor
	peer := &validatorEvidenceCarryRPC{base: fixture.rpc.base, transactions: fixture.rpc.transactions, inclusions: fixture.rpc.inclusions, creation: fixture.rpc.creation, chainID: fixture.rpc.chainID, fault: "reorg"}
	client, _ := peer.client(t)
	got, err := authenticateValidatorEvidenceCarry(t.Context(), executor.cfg, executor.stateDir, executor.plan, executor.journal.Entries(), executor.deployer.client, client)
	if err == nil || got != nil || peer.calls.Load() == 0 {
		t.Fatalf("operational approval overrode independent canonical conflict: %v", err)
	}
}

func TestValidatorEvidenceCarryRejectsConfiguredDomainDriftBeforeRPC(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, false)
	fixture.authenticate(t)
	executor := fixture.executor
	for _, fault := range []string{"chain", "genesis", "netuid", "owner", "deployment"} {
		cfg, public, config := *executor.cfg, *executor.cfg.Public, *executor.cfg.Config
		cfg.Public, cfg.Config = &public, &config
		switch fault {
		case "chain":
			cfg.ChainID++
		case "genesis":
			public.Chain.GenesisHash = common.HexToHash("0x88").Hex()
		case "netuid":
			cfg.Netuid++
		case "owner":
			cfg.WalletPublic += "-other"
		case "deployment":
			config.Deployment.DeploymentID += "-other"
		}
		before := fixture.rpc.calls.Load()
		got, err := authenticateValidatorEvidenceCarry(t.Context(), &cfg, executor.stateDir, executor.plan, executor.journal.Entries(), executor.deployer.client, nil)
		if err == nil || got != nil || fixture.rpc.calls.Load() != before {
			t.Fatalf("%s configured source domain drift reached RPC: %v", fault, err)
		}
	}
}

func TestValidatorEvidenceCarryRejectsUnreviewedABIAndImmutableAliases(t *testing.T) {
	t.Parallel()
	artifact, err := currentValidatorEvidenceArtifact()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateValidatorEvidenceSourceArtifact(artifact); err != nil {
		t.Fatalf("actual generated artifact/public binding ABI prerequisite: %v", err)
	}
	for _, fault := range []string{"getter-name", "getter-type", "duplicate-field", "missing-immutable", "overlapping-immutable", "negative-offset", "offset-end", "runtime-hash", "creation-hex", "storage-layout", "absent-artifact"} {
		changed := cloneValidatorEvidenceArtifact(artifact)
		switch fault {
		case "getter-name":
			changed.ABI = strings.Replace(changed.ABI, `"coordinator"`, `"anotherCoordinator"`, 1)
		case "getter-type":
			changed.ABI = strings.Replace(changed.ABI, `"type":"uint64"`, `"type":"uint128"`, 1)
		case "duplicate-field":
			changed.ABI = strings.Replace(changed.ABI, `"type":"constructor"`, `"type":"constructor","type":"constructor"`, 1)
		case "missing-immutable":
			delete(changed.ImmutableReferences, "coordinator")
		case "overlapping-immutable":
			changed.ImmutableReferences["settlementVault"][0] = changed.ImmutableReferences["coordinator"][0] + 1
		case "negative-offset":
			changed.ImmutableReferences["coordinator"][0] = -1
		case "offset-end":
			changed.ImmutableReferences["coordinator"][0] = len(hexBytes(changed.RuntimeBytecode)) - 31
		case "runtime-hash":
			changed.RuntimeBytecodeHash = common.HexToHash("0x33").Hex()
		case "creation-hex":
			changed.CreationBytecode += "z"
		case "storage-layout":
			changed.StorageLayoutHash = "sha256:00"
		case "absent-artifact":
			changed.FoundryArtifactHash = (common.Hash{}).Hex()
		}
		if reflect.DeepEqual(changed, artifact) {
			t.Fatalf("%s did not mutate the actual artifact", fault)
		}
		if err := validateValidatorEvidenceSourceArtifact(changed); err == nil {
			t.Fatalf("%s unreviewed source interface/immutable layout accepted", fault)
		}
	}
	if err := validateValidatorEvidenceSourceArtifact(artifact); err != nil {
		t.Fatalf("detached mutation poisoned original artifact: %v", err)
	}
}
