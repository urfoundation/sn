// Actual plan archives, signed transactions, journal replay and HTTP reads
// supply carry authority. No fixture returns an accepted carry observation.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type validatorEvidenceCarryRPC struct {
	base         *validatorEvidenceInstallRPC
	transactions map[common.Hash]*types.Transaction
	inclusions   map[common.Hash]ChainHead
	creation     common.Hash
	chainID      uint64
	fault        string
	codeHook     func(context.Context) error
	calls        atomic.Uint64
	sends        atomic.Uint64
}

func (self *validatorEvidenceCarryRPC) selected(selector finalEVMBlockSelector) (ChainHead, error) {
	if !selector.RequireCanonical {
		return ChainHead{}, errors.New("carry read omitted canonical hash requirement")
	}
	for _, head := range []ChainHead{testEVMHead(100, 0xee), testEVMHead(101, 0xef), testEVMHead(110, 0xf0)} {
		if selector.BlockHash == head.Hash {
			return head, nil
		}
	}
	return ChainHead{}, errors.New("carry read used another block hash")
}

func (self *validatorEvidenceCarryRPC) ChainId(context.Context) (*hexutil.Big, error) {
	self.calls.Add(1)
	return (*hexutil.Big)(new(big.Int).SetUint64(self.chainID)), nil
}

func (self *validatorEvidenceCarryRPC) GetBlockByNumber(_ context.Context, selector string, full bool) (*evmRPCBlock, error) {
	self.calls.Add(1)
	if full {
		return nil, errors.New("carry requested full block")
	}
	for _, head := range []ChainHead{testEVMHead(100, 0xee), testEVMHead(101, 0xef), testEVMHead(110, 0xf0)} {
		if selector == hexutil.EncodeUint64(head.Number) || selector == "finalized" && head.Number == 110 {
			if self.fault == "reorg" && head.Number == 100 {
				head.Hash = testEVMHead(100, 0x7e).Hash
			}
			return &evmRPCBlock{Number: hexutil.EncodeUint64(head.Number), Hash: head.Hash}, nil
		}
	}
	return nil, errors.New("carry requested unexpected block selector")
}

func (self *validatorEvidenceCarryRPC) GetCode(ctx context.Context, address common.Address, selector finalEVMBlockSelector) (hexutil.Bytes, error) {
	self.calls.Add(1)
	if _, err := self.selected(selector); err != nil {
		return nil, err
	}
	if address != self.base.address {
		return nil, errors.New("carry requested another code address")
	}
	if self.codeHook != nil {
		if err := self.codeHook(ctx); err != nil {
			return nil, err
		}
	}
	code := bytes.Clone(self.base.code)
	if self.fault == "runtime" {
		code[0] ^= 1
	}
	return code, nil
}

func (self *validatorEvidenceCarryRPC) Call(_ context.Context, message ValidatorEvidenceInstallRPCMessage, selector finalEVMBlockSelector) (hexutil.Bytes, error) {
	self.calls.Add(1)
	head, err := self.selected(selector)
	if err != nil {
		return nil, err
	}
	key := validatorEvidenceInstallCall{address: message.To, data: hexutil.Encode(message.Data)}
	value, exists := self.base.responses[key]
	if !exists {
		return nil, errors.New("carry requested another target or calldata")
	}
	anchor := hexutil.Encode(crypto.Keccak256([]byte("validatorEvidence()"))[:4])
	if key.data == anchor {
		if self.fault == "anchor" {
			return abiWordAddress(common.HexToAddress("0x1111111111111111111111111111111111111111")), nil
		}
		if head.Number == 100 {
			return abiWordAddress(common.Address{}), nil
		}
	}
	value = bytes.Clone(value)
	if self.fault == "padding" {
		value = append(value, 0)
	}
	return value, nil
}

func (self *validatorEvidenceCarryRPC) GetTransactionReceipt(_ context.Context, hash common.Hash) (*types.Receipt, error) {
	self.calls.Add(1)
	head, exists := self.inclusions[hash]
	if !exists || self.fault == "missing-receipt" {
		return nil, nil
	}
	created := common.Address{}
	if hash == self.creation {
		created = self.base.address
	}
	status := uint64(types.ReceiptStatusSuccessful)
	if self.fault == "reverted" {
		status = types.ReceiptStatusFailed
	}
	if self.fault == "receipt-address" {
		created[0] ^= 1
	}
	return &types.Receipt{Status: status, TxHash: hash, BlockHash: common.HexToHash(head.Hash), BlockNumber: new(big.Int).SetUint64(head.Number), ContractAddress: created, GasUsed: 50_000, CumulativeGasUsed: 50_000, Logs: []*types.Log{}, EffectiveGasPrice: big.NewInt(1)}, nil
}

func (self *validatorEvidenceCarryRPC) GetTransactionByHash(_ context.Context, hash common.Hash) (json.RawMessage, error) {
	self.calls.Add(1)
	transaction, exists := self.transactions[hash]
	if !exists || self.fault == "missing-transaction" {
		return nil, nil
	}
	raw, err := transaction.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if self.fault != "pending" {
		head := self.inclusions[hash]
		fields["blockNumber"], _ = json.Marshal(hexutil.EncodeUint64(head.Number))
		fields["blockHash"], _ = json.Marshal(head.Hash)
		fields["transactionIndex"] = json.RawMessage(`"0x0"`)
	}
	return json.Marshal(fields)
}

func (self *validatorEvidenceCarryRPC) SendRawTransaction(context.Context, hexutil.Bytes) (common.Hash, error) {
	self.sends.Add(1)
	return common.Hash{}, errors.New("carry attempted a new transaction")
}

func (self *validatorEvidenceCarryRPC) client(t *testing.T) (*ethclient.Client, string) {
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

type validatorEvidenceCarryTestFixture struct {
	executor    *Executor
	rpc         *validatorEvidenceCarryRPC
	independent *validatorEvidenceCarryRPC
	creation    *types.Transaction
	anchor      *types.Transaction
}

// Alter only a genuine Solidity metadata digest, including the copy embedded
// in its initializer. Executable instructions, ABI and immutable offsets stay
// the real generated artifact; the original approval binds the changed bytes.
func validatorEvidenceCarryHistoricalArtifactTest(t *testing.T, current ContractArtifact) ContractArtifact {
	t.Helper()
	original := cloneValidatorEvidenceArtifact(current)
	runtime, creation := hexBytes(original.RuntimeBytecode), hexBytes(original.CreationBytecode)
	marker := []byte{0xa2, 0x64, 0x69, 0x70, 0x66, 0x73, 0x58, 0x22, 0x12, 0x20}
	offset := bytes.LastIndex(runtime, marker) + len(marker)
	if offset < len(marker) || offset+32 > len(runtime) || bytes.Count(creation, runtime) != 1 {
		t.Fatal("real generated artifact has no exact single embedded metadata digest")
	}
	changed := bytes.Clone(runtime)
	changed[offset] ^= 0x80
	original.RuntimeBytecode = hexutil.Encode(changed)
	original.CreationBytecode = hexutil.Encode(bytes.Replace(creation, runtime, changed, 1))
	original.RuntimeBytecodeHash = crypto.Keccak256Hash(changed).Hex()
	original.FoundryArtifactHash = crypto.Keccak256Hash([]byte(original.CreationBytecode), []byte(original.RuntimeBytecode), []byte(original.ABI)).Hex()
	if err := validateValidatorEvidenceSourceArtifact(original); err != nil {
		t.Fatalf("historical compatible generated artifact: %v", err)
	}
	return original
}

// Uses a complete public release lock, not the deliberately partial generic
// test lock. The three new artifact pins are derived from the actual artifact
// archived in this fixture's independently hash-approved source plan.
func newValidatorEvidenceCarryTestFixture(t *testing.T, historical bool) validatorEvidenceCarryTestFixture {
	t.Helper()
	return newValidatorEvidenceCarryModeTestFixture(t, historical, false)
}

func newValidatorEvidenceCarryModeTestFixture(t *testing.T, historical, private bool) validatorEvidenceCarryTestFixture {
	t.Helper()
	return newValidatorEvidenceCarryUpgradeTestFixture(t, historical, private, false)
}

// The optional older upgrade changes only genuine compiler metadata before
// the source plan is approved, while all six original custody contracts stay
// byte-identical. It exposes migration fast paths which consult only those six.
func newValidatorEvidenceCarryUpgradeTestFixture(t *testing.T, historical, private, olderUpgrade bool) validatorEvidenceCarryTestFixture {
	return newValidatorEvidenceCarryConfiguredTestFixture(t, historical, private, olderUpgrade, nil)
}

func newValidatorEvidenceCarryConfiguredTestFixture(t *testing.T, historical, private, olderUpgrade bool, configure func(*ResolvedConfig)) validatorEvidenceCarryTestFixture {
	t.Helper()
	cfg, secrets, payloads := validatorEvidenceInstallTest(t)
	if configure != nil {
		configure(cfg)
		var err error
		cfg.ConfigHash, err = releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters)
		if err != nil {
			t.Fatal(err)
		}
	}
	artifact := cloneValidatorEvidenceArtifact(payloads.ValidatorEvidence.Artifact)
	if historical {
		artifact = validatorEvidenceCarryHistoricalArtifactTest(t, artifact)
	}
	lock := testReleaseLockFixture(t)
	lock.EVMBuild["validator_evidence_artifact_hash"] = artifact.FoundryArtifactHash
	lock.EVMBuild["validator_evidence_runtime_hash"] = artifact.RuntimeBytecodeHash
	lock.EVMBuild["validator_evidence_storage_layout_hash"] = artifact.StorageLayoutHash
	if err := validateReleaseLockStatic(lock); err != nil {
		t.Fatalf("complete original release prerequisite: %v", err)
	}
	cfg.Release, cfg.OperationalRPCMode = lock, rpcModePublicOverride
	if private {
		cfg.OperationalRPCMode = rpcModePrivateAuthority
	}
	public, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	facts := *testSetupFacts()
	facts.DeployerNonce = payloads.Manifest.InitialNonce
	plan, err := buildPlan(cfg, &facts, public, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	plan.ValidatorEvidenceSource, err = newValidatorEvidenceSource(lock, artifact)
	if err != nil {
		t.Fatal(err)
	}
	plan.validatorEvidenceHistorical = historical
	if err := rebindValidatorEvidencePlan(plan); err != nil {
		t.Fatal(err)
	}
	if olderUpgrade {
		marker := []byte{0xa2, 0x64, 0x69, 0x70, 0x66, 0x73, 0x58, 0x22, 0x12, 0x20}
		for _, wire := range [][]byte{payloads.UpgradeImplementation, payloads.ExpectedRuntime[payloads.CoordinatorUpgrade.Implementation]} {
			offset := bytes.LastIndex(wire, marker) + len(marker)
			if offset < len(marker) || offset+32 > len(wire) {
				t.Fatal("actual coordinator has no canonical metadata digest")
			}
			wire[offset] ^= 0x80
		}
		payloads.CoordinatorUpgrade.RuntimeCodeHash = crypto.Keccak256Hash(payloads.ExpectedRuntime[payloads.CoordinatorUpgrade.Implementation]).Hex()
		if err := rebindPlanCoordinatorUpgrade(plan, payloads); err != nil {
			t.Fatal(err)
		}
	}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePlanBudget(plan); err != nil {
		t.Fatalf("actual source approval prerequisite: %v", err)
	}
	payloads.ValidatorEvidence, err = validatorEvidencePayloadsForPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	fixtureRPC := &validatorEvidenceCarryRPC{base: validatorEvidenceInstallRPCTest(t, payloads.ValidatorEvidence), transactions: map[common.Hash]*types.Transaction{}, inclusions: map[common.Hash]ChainHead{}, chainID: plan.ChainID}
	transactions := make([]*types.Transaction, 2)
	for index, label := range []string{"deployer", "testnet-owner"} {
		role, err := secrets.EVMKey(label)
		if err != nil {
			t.Fatal(err)
		}
		key, err := crypto.HexToECDSA(role.PrivateKeyHex)
		if err != nil {
			t.Fatal(err)
		}
		data, nonce := payloads.ValidatorEvidence.Creation, payloads.ValidatorEvidence.Manifest.DeployerNonce
		actionID := validatorEvidenceDeployActionID
		var to *common.Address
		if index == 1 {
			to, data, nonce, actionID = &payloads.Manifest.CoordinatorProxy, payloads.ValidatorEvidence.Anchor, 7, validatorEvidenceAnchorActionID
		}
		maximumGas, _, err := evmActionFeeEnvelope(actionByID(t, plan, actionID))
		if err != nil {
			t.Fatal(err)
		}
		transaction, err := types.SignNewTx(key, types.LatestSignerForChainID(new(big.Int).SetUint64(plan.ChainID)), &types.LegacyTx{Nonce: nonce, To: to, Value: new(big.Int), Gas: min(maximumGas, 5_000_000), GasPrice: big.NewInt(1), Data: data})
		if err != nil {
			t.Fatal(err)
		}
		transactions[index] = transaction
		fixtureRPC.transactions[transaction.Hash()] = transaction
		fixtureRPC.inclusions[transaction.Hash()] = testEVMHead(100+uint64(index), byte(0xee+index))
	}
	fixtureRPC.creation = transactions[0].Hash()
	client, endpoint := fixtureRPC.client(t)
	cfg.OperationalEVM, cfg.Public.Chain.EVMPublicReadEndpoint = endpoint, endpoint
	var independent *validatorEvidenceCarryRPC
	var independentClient *ethclient.Client
	if private {
		independent = &validatorEvidenceCarryRPC{base: fixtureRPC.base, transactions: fixtureRPC.transactions, inclusions: fixtureRPC.inclusions, creation: fixtureRPC.creation, chainID: fixtureRPC.chainID}
		independentClient, cfg.Public.Chain.EVMPublicReadEndpoint = independent.client(t)
	}
	stateDir := filepath.Join(t.TempDir(), "owner")
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	executor := &Executor{cfg: cfg, roles: secrets, stateDir: stateDir, plan: plan, payloads: payloads, journal: journal, deployer: &EvmTxManager{client: client}, independentEVM: independentClient}
	t.Cleanup(func() {
		if executor.journal != nil {
			if err := executor.journal.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(stateDir, "plans", stringsTrim0x(plan.PlanHash)+".json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{validatorEvidenceDeployActionID, validatorEvidenceAnchorActionID} {
		action := actionByID(t, plan, id)
		transaction := transactions[index]
		wire, err := transaction.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(stateDir, "transactions", stringsTrim0x(transaction.Hash().Hex())+".rlp"), wire, 0o600); err != nil {
			t.Fatal(err)
		}
		signer, err := types.Sender(types.LatestSignerForChainID(transaction.ChainId()), transaction)
		if err != nil {
			t.Fatal(err)
		}
		head := fixtureRPC.inclusions[transaction.Hash()]
		for _, stage := range []JournalStage{StageIntent, StageBroadcast, StageIncluded, StageFinalized} {
			entry := JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: id, IntentHash: action.IntentHash, Stage: stage}
			if stage == StageBroadcast {
				entry.Signer, entry.Nonce, entry.TransactionHash = signer.Hex(), strconv.FormatUint(transaction.Nonce(), 10), transaction.Hash().Hex()
				entry.RecoveryBlock, entry.RecoveryBlockHash = 99, testEVMHead(99, 0xed).Hash
			}
			if stage == StageIncluded || stage == StageFinalized {
				entry.TransactionHash, entry.BlockNumber, entry.BlockHash = transaction.Hash().Hex(), head.Number, head.Hash
			}
			if err := journal.Append(entry); err != nil {
				t.Fatal(err)
			}
		}
		observed, err := executor.actionPostState(t.Context(), action, head)
		if err != nil {
			t.Fatalf("original actual HTTP postcondition: %v", err)
		}
		independentObserved, err := cloneObservedPostState(observed)
		if err != nil {
			t.Fatal(err)
		}
		if private {
			independentObserved, err = executor.independentReadExecutor().actionPostState(t.Context(), action, head)
			if err != nil {
				t.Fatalf("original independent HTTP postcondition: %v", err)
			}
		}
		record := &ActionPostcondition{Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: id, IntentHash: action.IntentHash, OperationalRPCMode: cfg.OperationalRPCMode, IndependentRPC: private, SubstrateFinalized: testEVMHead(99, 0xab), EVMFinalized: head, EVMHashDomain: "evm-rpc", Observed: observed, IndependentSubstrateFinalized: testEVMHead(99, 0xab), IndependentEVMFinalized: head, IndependentEVMHashDomain: "evm-rpc", IndependentObserved: independentObserved}
		path, hash, err := executor.persistActionPostcondition(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := journal.Append(JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: id, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionPath: path, PostconditionHash: hash}); err != nil {
			t.Fatal(err)
		}
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	executor.journal, err = OpenJournal(stateDir)
	if err != nil {
		t.Fatalf("actual persisted journal replay: %v", err)
	}
	return validatorEvidenceCarryTestFixture{executor: executor, rpc: fixtureRPC, independent: independent, creation: transactions[0], anchor: transactions[1]}
}

func (self validatorEvidenceCarryTestFixture) authenticate(t *testing.T) *validatorEvidenceCarryObservation {
	t.Helper()
	executor := self.executor
	observed, err := authenticateValidatorEvidenceCarry(t.Context(), executor.cfg, executor.stateDir, executor.plan, executor.journal.Entries(), executor.deployer.client, executor.independentEVM)
	if err != nil || observed == nil {
		t.Fatalf("actual original approval/journal/HTTP carry prerequisite: %v", err)
	}
	return observed
}

// Retained tests which construct a different approved release must update the
// original source archive with the same lock; an arbitrary hash is not a lock.
func rebindValidatorEvidenceReleaseLockTest(t *testing.T, plan *SetupPlan, lock *ReleaseLock) {
	t.Helper()
	if plan == nil || plan.ValidatorEvidenceSource == nil || plan.ValidatorEvidenceCarry != nil {
		t.Fatal("fixture release rebind requires a fresh source approval")
	}
	var err error
	plan.ValidatorEvidenceSource, err = newValidatorEvidenceSource(lock, plan.ValidatorEvidenceSource.Artifact)
	if err != nil {
		t.Fatal(err)
	}
	plan.ReleaseLockHash, err = canonicalHashHex(lock)
	if err != nil {
		t.Fatal(err)
	}
}

func distinctValidatorEvidenceReleaseLockTest(t *testing.T, plan *SetupPlan, digestByte string) {
	t.Helper()
	if plan == nil || plan.ValidatorEvidenceSource == nil {
		t.Fatal("fixture source lock is absent")
	}
	source, err := newValidatorEvidenceSource(plan.ValidatorEvidenceSource.ReleaseLock, plan.ValidatorEvidenceSource.Artifact)
	if err != nil {
		t.Fatal(err)
	}
	source.ReleaseLock.Dependencies["redis"] = "redis:8-alpine@sha256:" + strings.Repeat(digestByte, 32)
	rebindValidatorEvidenceReleaseLockTest(t, plan, source.ReleaseLock)
}

// The existing coordinator observer uses numbered finalized reads; companion
// history still requires its exact EIP-1898 selectors through the embedded RPC.
// No method returns an accepted migration or bypasses the real ABI/runtime path.
type validatorEvidenceCarryUpgradeRPC struct {
	*validatorEvidenceCarryRPC
	codes                          map[common.Address][]byte
	nonce                          uint64
	deployer, proxy, active, drill common.Address
}

// Geth's numbered CallContract uses input; the companion's explicit canonical
// batch uses data. Refuse mixed fields instead of guessing which is authority.
type ValidatorEvidenceCarryUpgradeRPCMessage struct {
	To    common.Address `json:"to"`
	Data  hexutil.Bytes  `json:"data"`
	Input hexutil.Bytes  `json:"input"`
}

func (self *validatorEvidenceCarryUpgradeRPC) GetCode(ctx context.Context, address common.Address, raw json.RawMessage) (hexutil.Bytes, error) {
	var selector finalEVMBlockSelector
	if len(raw) > 0 && raw[0] == '{' {
		if err := json.Unmarshal(raw, &selector); err != nil {
			return nil, err
		}
		return self.validatorEvidenceCarryRPC.GetCode(ctx, address, selector)
	}
	self.calls.Add(1)
	var block string
	if err := json.Unmarshal(raw, &block); err != nil || block != hexutil.EncodeUint64(110) {
		return nil, errors.New("upgrade code read is not at the configured finalized block")
	}
	code, exists := self.codes[address]
	if !exists {
		return nil, errors.New("upgrade code read used an unconfigured address")
	}
	return bytes.Clone(code), nil
}

func (self *validatorEvidenceCarryUpgradeRPC) GetTransactionCount(_ context.Context, address common.Address, block string) (hexutil.Uint64, error) {
	self.calls.Add(1)
	if address != self.deployer || block != hexutil.EncodeUint64(110) {
		return 0, errors.New("upgrade nonce read has another account or block")
	}
	return hexutil.Uint64(self.nonce), nil
}

func (self *validatorEvidenceCarryUpgradeRPC) GetStorageAt(_ context.Context, address common.Address, slot common.Hash, block string) (hexutil.Bytes, error) {
	self.calls.Add(1)
	if address != self.proxy || slot != common.HexToHash(erc1967ImplementationSlot) || block != hexutil.EncodeUint64(110) {
		return nil, errors.New("upgrade slot read has another proxy, slot or block")
	}
	return abiWordAddress(self.active), nil
}

func (self *validatorEvidenceCarryUpgradeRPC) Call(ctx context.Context, message ValidatorEvidenceCarryUpgradeRPCMessage, raw json.RawMessage) (hexutil.Bytes, error) {
	var selector finalEVMBlockSelector
	if len(raw) > 0 && raw[0] == '{' {
		if len(message.Data) == 0 || len(message.Input) != 0 {
			return nil, errors.New("canonical companion read changed calldata fields")
		}
		if err := json.Unmarshal(raw, &selector); err != nil {
			return nil, err
		}
		return self.validatorEvidenceCarryRPC.Call(ctx, ValidatorEvidenceInstallRPCMessage{To: message.To, Data: message.Data}, selector)
	}
	self.calls.Add(1)
	var block string
	if err := json.Unmarshal(raw, &block); err != nil || block != hexutil.EncodeUint64(110) || message.To != self.drill || len(message.Input) == 0 || len(message.Data) != 0 {
		return nil, errors.New("upgrade governance read has another target, block or calldata field")
	}
	if bytes.Equal(message.Input, crypto.Keccak256([]byte("DRILL_VERSION()"))[:4]) {
		return crypto.Keccak256([]byte("urnetwork/coordinator-adversary/v1")), nil
	}
	if bytes.Equal(message.Input, crypto.Keccak256([]byte("proxiableUUID()"))[:4]) {
		return common.HexToHash(erc1967ImplementationSlot).Bytes(), nil
	}
	return nil, errors.New("upgrade governance read used another selector")
}

func (self *validatorEvidenceCarryUpgradeRPC) client(t *testing.T) (*ethclient.Client, string) {
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
