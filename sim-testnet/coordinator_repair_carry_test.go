package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type coordinatorRepairCarryRPC struct {
	*validatorEvidenceCarryRPC
	plan     *SetupPlan
	upgrade  CoordinatorUpgrade
	code     map[common.Address][]byte
	identity map[string][]byte
	problem  string
}

func (self *coordinatorRepairCarryRPC) GetCode(ctx context.Context, address common.Address, selector finalEVMBlockSelector) (hexutil.Bytes, error) {
	self.calls.Add(1)
	if _, err := self.selected(selector); err != nil {
		return nil, err
	}
	code := bytes.Clone(self.code[address])
	if self.problem == "runtime" && address == self.upgrade.Implementation && len(code) > 0 {
		code[0] ^= 1
	}
	if self.problem == "immutable" && address == self.plan.Deployment.SettlementVault && len(code) > 0 {
		code[0] ^= 1
	}
	return code, ctx.Err()
}

func (self *coordinatorRepairCarryRPC) GetStorageAt(_ context.Context, address common.Address, slot string, selector finalEVMBlockSelector) (hexutil.Bytes, error) {
	self.calls.Add(1)
	if _, err := self.selected(selector); err != nil {
		return nil, err
	}
	if address != self.plan.Deployment.CoordinatorProxy || slot != erc1967ImplementationSlot {
		return nil, errors.New("repair requested another storage slot")
	}
	active := self.upgrade.Implementation
	if self.problem == "activation" {
		active = self.plan.CoordinatorUpgrade.Implementation
	}
	return abiWordAddress(active), nil
}

func (self *coordinatorRepairCarryRPC) Call(_ context.Context, message ValidatorEvidenceInstallRPCMessage, selector json.RawMessage) (hexutil.Bytes, error) {
	self.calls.Add(1)
	var number string
	if err := json.Unmarshal(selector, &number); err != nil {
		return nil, err
	}
	if number != "0x65" && number != "0x6e" || message.To != self.plan.Deployment.CoordinatorProxy {
		return nil, errors.New("repair identity used another checkpoint")
	}
	for method, value := range self.identity {
		if bytes.Equal(message.Data, crypto.Keccak256([]byte(method + "()"))[:4]) {
			result := bytes.Clone(value)
			if self.problem == "custody" && method == "selfColdkey" {
				result[0] ^= 1
			}
			return result, nil
		}
	}
	return nil, errors.New("repair identity used another method")
}

func (self *coordinatorRepairCarryRPC) GetTransactionCount(_ context.Context, address common.Address, selector string) (hexutil.Uint64, error) {
	self.calls.Add(1)
	if address != common.HexToAddress(self.plan.Roles.Deployer) || selector != "pending" && selector != "0x6e" {
		return 0, errors.New("repair nonce read escaped its owner")
	}
	nonce := self.upgrade.DeployerNonce + 1
	if self.problem == "nonce" || self.problem == "pending" && selector == "pending" {
		nonce++
	}
	return hexutil.Uint64(nonce), nil
}

func (self *coordinatorRepairCarryRPC) GetTransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	receipt, err := self.validatorEvidenceCarryRPC.GetTransactionReceipt(ctx, hash)
	if receipt != nil && hash == self.creation {
		receipt.ContractAddress = self.upgrade.Implementation
	}
	if receipt != nil && self.problem == "receipt" {
		receipt.Status = types.ReceiptStatusFailed
	}
	return receipt, err
}

func (self *coordinatorRepairCarryRPC) client(t *testing.T) (*ethclient.Client, string) {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("eth", self); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	client, err := ethclient.DialContext(t.Context(), httpServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client, httpServer.URL
}

type coordinatorRepairCarryFixture struct {
	executor            *Executor
	reader, independent *coordinatorRepairCarryRPC
	reference           CoordinatorRepairCarry
}

func newCoordinatorRepairCarryFixture(t *testing.T) coordinatorRepairCarryFixture {
	t.Helper()
	original := newValidatorEvidenceCarryModeTestFixture(t, false, true)
	e := original.executor
	source := e.plan
	artifact := artifactByName("Coordinator")
	artifactRaw, err := json.Marshal(map[string]any{"abi": json.RawMessage(artifact.ABI), "bytecode": map[string]any{"object": artifact.CreationBytecode}, "deployedBytecode": map[string]any{"object": artifact.RuntimeBytecode, "immutableReferences": artifact.ImmutableReferences}})
	if err != nil {
		t.Fatal(err)
	}
	nonce := source.CoordinatorUpgrade.DeployerNonce + 3
	implementation := crypto.CreateAddress(common.HexToAddress(source.Roles.Deployer), nonce)
	creation, runtime, err := coordinatorRepairArtifact(artifactRaw, implementation)
	if err != nil {
		t.Fatal(err)
	}
	reserve := actionByID(t, source, "campaign.evm-gas-reserve")
	entries := e.journal.Entries()
	budget := coordinatorRepairBudget{Schema: "urnetwork-provisional-coordinator-repair-budget-v1", PlanHash: source.PlanHash, JournalHash: entries[len(entries)-1].EntryHash, CampaignReserveWei: string(reserve.Spend.EVMGasWei), CommittedOrPendingMaxWei: "0", AvailableWei: string(reserve.Spend.EVMGasWei), RepairMaxWei: coordinatorRepairMaximumWei, Verified: true}
	// The actual command retains audit context outside its signed executable
	// budget projection. Exercise the complete typed reader on the live path.
	budgetRaw, err := json.Marshal(coordinatorRepairBudgetDocument{coordinatorRepairBudget: budget, ObservedAt: "2026-09-12T02:17:25.751430+00:00", JournalSequence: entries[len(entries)-1].Sequence, NewFundingWei: "0"})
	if err != nil {
		t.Fatal(err)
	}
	deploy, err := coordinatorRepairAction("repair.coordinator-rounding.deploy", common.HexToAddress(source.Roles.Deployer), nonce, nil, creation, 7_500_000)
	if err != nil {
		t.Fatal(err)
	}
	activateData := append(crypto.Keccak256([]byte("upgradeToAndCall(address,bytes)"))[:4], abiWordAddress(implementation)...)
	activateData = append(activateData, common.LeftPadBytes([]byte{64}, 32)...)
	activateData = append(activateData, make([]byte, 32)...)
	activate, err := coordinatorRepairAction("repair.coordinator-rounding.activate", common.HexToAddress(source.Roles.Owner), 19, &source.Deployment.CoordinatorProxy, activateData, 500_000)
	if err != nil {
		t.Fatal(err)
	}
	identity := map[string][]byte{"owner": abiWordAddress(common.HexToAddress(source.Roles.Owner)), "netuid": common.LeftPadBytes(new(big.Int).SetUint64(uint64(source.Netuid)).Bytes(), 32), "selfColdkey": abiWordAddress(common.HexToAddress(source.Roles.Owner)), "settlementVault": abiWordAddress(source.Deployment.SettlementVault), "reserveSink": abiWordAddress(source.Deployment.ReserveSink), "validatorEvidence": abiWordAddress(source.ValidatorEvidence.Address)}
	encodedIdentity := map[string]string{}
	for key, value := range identity {
		encodedIdentity[key] = hex.EncodeToString(value)
	}
	identityHash, err := canonicalHashHex(encodedIdentity)
	if err != nil {
		t.Fatal(err)
	}
	request := coordinatorRepairRequest{Schema: "urnetwork-provisional-coordinator-repair-v1", Provisional: true, PlanHash: source.PlanHash, ConfigHash: source.ConfigHash, DeploymentID: source.DeploymentID, ArtifactSHA256: strings.TrimPrefix(bytesSHA256(artifactRaw), "sha256:"), BudgetSHA256: strings.TrimPrefix(bytesSHA256(budgetRaw), "sha256:"), Budget: budget, Proxy: source.Deployment.CoordinatorProxy, Vault: source.Deployment.SettlementVault, Reserve: source.Deployment.ReserveSink, Owner: common.HexToAddress(source.Roles.Owner), Deployer: common.HexToAddress(source.Roles.Deployer), OldUpgrade: source.CoordinatorUpgrade, Upgrade: CoordinatorUpgrade{Schema: "urnetwork-coordinator-upgrade-v2", DeploymentID: source.DeploymentID, Implementation: implementation, DeployerNonce: nonce, RuntimeCodeHash: crypto.Keccak256Hash(runtime).Hex()}, IdentityHash: identityHash, Deploy: deploy, Activate: activate}
	ownerRole, err := e.roles.EVMKey("testnet-owner")
	if err != nil {
		t.Fatal(err)
	}
	ownerKey, err := crypto.HexToECDSA(ownerRole.PrivateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	reference := CoordinatorRepairCarry{Schema: "urnetwork-coordinator-repair-carry-v1", Request: signedCoordinatorRepairRequest{Request: request}}
	reference.Request.Hash, reference.Request.Signature, err = coordinatorRepairSignature(request, ownerKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCoordinatorRepairRequest(source, &reference.Request); err != nil {
		t.Fatal(err)
	}
	base := &validatorEvidenceCarryRPC{base: original.rpc.base, transactions: map[common.Hash]*types.Transaction{}, inclusions: map[common.Hash]ChainHead{}, chainID: source.ChainID}
	finals := make([]JournalEntry, 2)
	for index, action := range []Action{deploy, activate} {
		label, data, txNonce := "deployer", creation, nonce
		var to *common.Address
		if index == 1 {
			label, data, txNonce, to = "testnet-owner", activateData, 19, &source.Deployment.CoordinatorProxy
		}
		role, err := e.roles.EVMKey(label)
		if err != nil {
			t.Fatal(err)
		}
		key, err := crypto.HexToECDSA(role.PrivateKeyHex)
		if err != nil {
			t.Fatal(err)
		}
		transaction, err := types.SignNewTx(key, types.LatestSignerForChainID(new(big.Int).SetUint64(source.ChainID)), &types.LegacyTx{Nonce: txNonce, To: to, Value: new(big.Int), Gas: 300_000, GasPrice: big.NewInt(1), Data: data})
		if err != nil {
			t.Fatal(err)
		}
		wire, err := transaction.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(e.stateDir, "transactions", stringsTrim0x(transaction.Hash().Hex())+".rlp"), wire, 0o600); err != nil {
			t.Fatal(err)
		}
		head := testEVMHead(100+uint64(index), byte(0xee+index))
		base.transactions[transaction.Hash()], base.inclusions[transaction.Hash()] = transaction, head
		if index == 0 {
			base.creation = transaction.Hash()
		}
		for _, stage := range []JournalStage{StageIntent, StageBroadcast, StageIncluded, StageFinalized} {
			entry := JournalEntry{DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: stage}
			if stage == StageBroadcast {
				entry.Signer, entry.Nonce, entry.TransactionHash = crypto.PubkeyToAddress(key.PublicKey).Hex(), strconv.FormatUint(txNonce, 10), transaction.Hash().Hex()
			}
			if stage == StageIncluded || stage == StageFinalized {
				entry.TransactionHash, entry.BlockNumber, entry.BlockHash = transaction.Hash().Hex(), head.Number, head.Hash
			}
			if err := e.journal.Append(entry); err != nil {
				t.Fatal(err)
			}
		}
		entries := e.journal.Entries()
		finals[index] = entries[len(entries)-1]
	}
	reference.Result.Result = coordinatorRepairResult{Schema: "urnetwork-provisional-coordinator-repair-result-v1", Provisional: true, RequestHash: reference.Request.Hash, Deploy: finals[0], Activate: finals[1], ObservedHead: testEVMHead(101, 0xef), IdentityHash: identityHash}
	reference.Result.Hash, reference.Result.Signature, err = coordinatorRepairSignature(reference.Result.Result, ownerKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{"request.json": reference.Request, "result.json": reference.Result} {
		if err := writeCoordinatorRepairFile(filepath.Join(e.stateDir, coordinatorRepairDirectory, name), value); err != nil {
			t.Fatal(err)
		}
	}
	for name, raw := range map[string][]byte{"artifact.json": artifactRaw, "budget.json": budgetRaw} {
		if err := atomicWrite(filepath.Join(e.stateDir, coordinatorRepairDirectory, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	code := map[common.Address][]byte{}
	for address, raw := range e.payloads.ExpectedRuntime {
		code[address] = bytes.Clone(raw)
	}
	code[implementation] = runtime
	reader := &coordinatorRepairCarryRPC{validatorEvidenceCarryRPC: base, plan: source, upgrade: request.Upgrade, code: code, identity: identity}
	independentBase := &validatorEvidenceCarryRPC{base: base.base, transactions: base.transactions, inclusions: base.inclusions, creation: base.creation, chainID: base.chainID}
	independent := &coordinatorRepairCarryRPC{validatorEvidenceCarryRPC: independentBase, plan: source, upgrade: request.Upgrade, code: code, identity: identity}
	client, endpoint := reader.client(t)
	independentClient, independentEndpoint := independent.client(t)
	e.cfg.OperationalEVM, e.cfg.Public.Chain.EVMPublicReadEndpoint = endpoint, independentEndpoint
	e.deployer = &EvmTxManager{client: client}
	e.independentEVM = independentClient
	return coordinatorRepairCarryFixture{executor: e, reader: reader, independent: independent, reference: reference}
}

func TestCoordinatorRepairCarryReplaysExactFinalizedCorrectionWithoutWrites(t *testing.T) {
	fixture := newCoordinatorRepairCarryFixture(t)
	e := fixture.executor
	before := e.journal.Entries()
	observed, err := authenticateCoordinatorRepairCarry(t.Context(), e.cfg, e.stateDir, e.plan, before, e.deployer.client, e.independentEVM)
	if err != nil || observed == nil || !reflect.DeepEqual(observed.reference, fixture.reference) || observed.deployerNonce != fixture.reference.Request.Request.Upgrade.DeployerNonce+1 {
		t.Fatalf("exact completed correction was not authenticated: %v", err)
	}
	if fixture.reader.calls.Load() == 0 || fixture.independent.calls.Load() == 0 || fixture.reader.sends.Load() != 0 || fixture.independent.sends.Load() != 0 || !reflect.DeepEqual(before, e.journal.Entries()) {
		t.Fatal("carry omitted independent reads or changed transaction history")
	}
	for _, problem := range []string{"runtime", "immutable", "activation", "custody", "receipt", "nonce", "pending"} {
		t.Run(problem, func(t *testing.T) {
			fixture.independent.problem = problem
			defer func() { fixture.independent.problem = "" }()
			if got, err := authenticateCoordinatorRepairCarry(t.Context(), e.cfg, e.stateDir, e.plan, before, e.deployer.client, e.independentEVM); err == nil || got != nil {
				t.Fatalf("independent %s mismatch issued authority: %v", problem, err)
			}
		})
	}
	fixture.independent.fault = "reorg"
	if got, err := authenticateCoordinatorRepairCarry(t.Context(), e.cfg, e.stateDir, e.plan, before, e.deployer.client, e.independentEVM); err == nil || got != nil {
		t.Fatal("reorg admitted")
	}
}

func TestCoordinatorRepairCarryRejectsChangedSourceAndIncompleteHistory(t *testing.T) {
	fixture := newCoordinatorRepairCarryFixture(t)
	e := fixture.executor
	entries := e.journal.Entries()
	if _, err := readCoordinatorRepairCarry(e.stateDir, e.plan, entries); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"artifact.json", "budget.json", "request.json", "result.json"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(e.stateDir, coordinatorRepairDirectory, name)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.WriteFile(path, raw, 0o600); err != nil {
					t.Error(err)
				}
			}()
			changed := bytes.Clone(raw)
			changed[len(changed)/2] ^= 1
			if err := os.WriteFile(path, changed, 0o600); err != nil {
				t.Fatal(err)
			}
			if got, err := readCoordinatorRepairCarry(e.stateDir, e.plan, entries); err == nil || got != nil {
				t.Fatal("changed retained bytes admitted")
			}
		})
	}
	if got, err := readCoordinatorRepairCarry(e.stateDir, e.plan, entries[:len(entries)-1]); err == nil || got != nil {
		t.Fatal("missing finalization admitted")
	}
	changed := append([]JournalEntry(nil), entries...)
	changed[len(changed)-1].BlockNumber++
	if got, err := readCoordinatorRepairCarry(e.stateDir, e.plan, changed); err == nil || got != nil {
		t.Fatal("changed canonical journal receipt admitted")
	}
	copyPlan := *e.plan
	copyPlan.PlanHash = "0x" + strings.Repeat("ab", 32)
	if got, err := readCoordinatorRepairCarry(e.stateDir, &copyPlan, entries); err == nil || got != nil {
		t.Fatal("cross-lineage source admitted")
	}
}

func TestCoordinatorRepairCarryKeepsOriginalProbeAndNextNonceSeparate(t *testing.T) {
	_, payloads, retained, baseline, _ := replacementPrecompileProbeFixture(t)
	oldUpgrade := payloads.CoordinatorUpgrade
	if err := configureCoordinatorUpgradeNonce(payloads, oldUpgrade.DeployerNonce+3); err != nil {
		t.Fatal(err)
	}
	if err := configureFleetBatcherNonce(payloads, oldUpgrade.DeployerNonce+1); err != nil {
		t.Fatal(err)
	}
	carry := &CoordinatorRepairCarry{Request: signedCoordinatorRepairRequest{Request: coordinatorRepairRequest{OldUpgrade: oldUpgrade, Upgrade: payloads.CoordinatorUpgrade}}}
	if err := validateCoordinatorUpgradePayloadBaseline(baseline, retained, coordinatorRepairBaselinePayloads(payloads, carry)); err != nil {
		t.Fatalf("original probe/batcher proof lost: %v", err)
	}
	if err := validateCoordinatorUpgradeBaseline(baseline, retained, payloads.CoordinatorUpgrade); err == nil {
		t.Fatal("unadopted later CREATE was accepted as the original adjacent probe proof")
	}
	if payloads.CoordinatorUpgrade.DeployerNonce != oldUpgrade.DeployerNonce+3 {
		t.Fatal("historical baseline view changed current correction")
	}
	observation := &coordinatorRepairCarryObservation{reference: *carry, deployerNonce: payloads.CoordinatorUpgrade.DeployerNonce + 1}
	prior := &SetupPlan{coordinatorRepairObserved: observation}
	migration := &coordinatorUpgradeMigration{Upgrade: payloads.CoordinatorUpgrade, Repair: observation}
	if ok, err := coordinatorUpgradeMigrationNonceMatches(prior, migration, payloads, observation.deployerNonce, nil); err != nil || !ok {
		t.Fatalf("exact authenticated next nonce rejected: %v", err)
	}
	if ok, _ := coordinatorUpgradeMigrationNonceMatches(prior, migration, payloads, observation.deployerNonce+1, nil); ok {
		t.Fatal("another consumed nonce admitted")
	}
	other := *observation
	prior.coordinatorRepairObserved = &other
	if ok, _ := coordinatorUpgradeMigrationNonceMatches(prior, migration, payloads, observation.deployerNonce, nil); ok {
		t.Fatal("cross-observation migration admitted")
	}
}
