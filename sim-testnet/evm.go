package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/urfoundation/sn/ss58"
	"github.com/urfoundation/sn/stabi"
)

type ContractDeployment struct {
	Schema                        string            `json:"schema"`
	DeploymentID                  string            `json:"deployment_id"`
	InitialNonce                  uint64            `json:"initial_nonce"`
	ReserveSink                   common.Address    `json:"reserve_sink"`
	SettlementVault               common.Address    `json:"settlement_vault"`
	CoordinatorImplementation     common.Address    `json:"coordinator_implementation"`
	CoordinatorProxy              common.Address    `json:"coordinator_proxy"`
	GovernanceDrillImplementation common.Address    `json:"governance_drill_implementation"`
	PrecompileProbe               common.Address    `json:"precompile_probe"`
	DeployBlock                   uint64            `json:"deploy_block,omitempty"`
	DeployBlockHash               string            `json:"deploy_block_hash,omitempty"`
	RuntimeHashes                 map[string]string `json:"runtime_hashes,omitempty"`
}

type DeploymentPayloads struct {
	Manifest                                                                                                   ContractDeployment
	Reserve, Vault, Implementation, RegisterEscrow, Proxy, GovernanceDrill, FixVault, FixSink, PrecompileProbe []byte
	ExpectedRuntime                                                                                            map[common.Address][]byte
}

func buildDeploymentPayloads(cfg *ResolvedConfig, roles *RoleSecrets, initialNonce uint64) (*DeploymentPayloads, error) {
	deployer, err := roles.EVMAddress("deployer")
	if err != nil {
		return nil, err
	}
	owner, err := roles.EVMAddress("testnet-owner")
	if err != nil {
		return nil, err
	}
	guardian, err := roles.EVMAddress("guardian")
	if err != nil {
		return nil, err
	}
	oracle, err := roles.EVMAddress("commitment-oracle")
	if err != nil {
		return nil, err
	}
	// The first three CREATEs are followed by the vault-owned escrow
	// registration, then the proxy and hostile implementation CREATEs and two
	// one-shot link calls. The conformance probe is therefore nonce+8.
	m := ContractDeployment{Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, InitialNonce: initialNonce, ReserveSink: crypto.CreateAddress(deployer, initialNonce), SettlementVault: crypto.CreateAddress(deployer, initialNonce+1), CoordinatorImplementation: crypto.CreateAddress(deployer, initialNonce+2), CoordinatorProxy: crypto.CreateAddress(deployer, initialNonce+4), GovernanceDrillImplementation: crypto.CreateAddress(deployer, initialNonce+5), PrecompileProbe: crypto.CreateAddress(deployer, initialNonce+8), RuntimeHashes: map[string]string{}}
	reserveHotkey, err := roleBytes32(roles, "reserve-hotkey")
	if err != nil {
		return nil, err
	}
	escrowHotkey, err := roleBytes32(roles, "escrow-hotkey")
	if err != nil {
		return nil, err
	}
	sinkSelf := ss58.EvmMirrorPubkey(m.ReserveSink)
	vaultSelf := ss58.EvmMirrorPubkey(m.SettlementVault)
	coordinatorSelf := ss58.EvmMirrorPubkey(m.CoordinatorProxy)
	reserveABI, err := abi.JSON(strings.NewReader(ReserveSinkABI))
	if err != nil {
		return nil, err
	}
	vaultABI, err := abi.JSON(strings.NewReader(SettlementVaultABI))
	if err != nil {
		return nil, err
	}
	coordABI, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return nil, err
	}
	proxyABI, err := abi.JSON(strings.NewReader(ERC1967ProxyABI))
	if err != nil {
		return nil, err
	}
	sinkArgs, err := reserveABI.Constructor.Inputs.Pack(cfg.Netuid, reserveHotkey, sinkSelf, deployer)
	if err != nil {
		return nil, fmt.Errorf("reserve constructor: %w", err)
	}
	minimumTTL := cfg.Policy.Settlement.EpochBlocks * cfg.Policy.Settlement.ClaimTTLEpochs
	vaultArgs, err := vaultABI.Constructor.Inputs.Pack(cfg.Netuid, escrowHotkey, vaultSelf, minimumTTL, deployer)
	if err != nil {
		return nil, fmt.Errorf("vault constructor: %w", err)
	}
	hash, err := decodeHash(cfg.PolicyHash)
	if err != nil {
		return nil, err
	}
	policy := stabi.STCoordinatorPolicySnapshot{PolicyHash: hash, EffectiveEpoch: 0, EffectiveBlock: 0, EpochBlocks: cfg.Policy.Settlement.EpochBlocks, RootCommitWindowBlocks: cfg.Policy.Settlement.RootCommitWindowBlocks, FinalizeOffsetBlocks: cfg.Policy.Settlement.FinalizeOffsetBlocks, CloseGraceBlocks: cfg.Policy.Settlement.CloseGraceBlocks, ClaimTTLEpochs: cfg.Policy.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: cfg.Policy.Settlement.ClaimGraceEpochs, MaximumBindingValidityEpochs: cfg.Policy.Binding.MaximumValidityEpochs, CommitmentMaxAgeBlocks: cfg.Policy.Settlement.EpochBlocks * 2, EpochDepositCapRao: new(big.Int).SetUint64(cfg.Policy.Deposit.EpochCapRaoPerOperator), CampaignDepositCapRao: new(big.Int).SetUint64(cfg.Policy.Deposit.TotalTestCampaignCapRao)}
	initData, err := coordABI.Pack("initialize", cfg.Netuid, owner, guardian, coordinatorSelf, m.SettlementVault, m.ReserveSink, oracle, policy)
	if err != nil {
		return nil, fmt.Errorf("coordinator initialize: %w", err)
	}
	proxyArgs, err := proxyABI.Constructor.Inputs.Pack(m.CoordinatorImplementation, initData)
	if err != nil {
		return nil, fmt.Errorf("proxy constructor: %w", err)
	}
	fixVault, err := vaultABI.Pack("setCoordinatorOnce", m.CoordinatorProxy)
	if err != nil {
		return nil, err
	}
	fixSink, err := reserveABI.Pack("setRecorderOnce", m.CoordinatorProxy)
	if err != nil {
		return nil, err
	}
	probeABI, err := abi.JSON(strings.NewReader(SubnetProbeABI))
	if err != nil {
		return nil, err
	}
	probeArgs, err := probeABI.Constructor.Inputs.Pack(cfg.Netuid)
	if err != nil {
		return nil, fmt.Errorf("precompile probe constructor: %w", err)
	}
	registerEscrow, err := vaultABI.Pack("registerEscrow", cfg.Config.Budgets.MaximumRegistrationBurnRao)
	if err != nil {
		return nil, err
	}
	p := &DeploymentPayloads{Manifest: m, Reserve: append(hexBytes(ReserveSinkCreationBytecode), sinkArgs...), Vault: append(hexBytes(SettlementVaultCreationBytecode), vaultArgs...), Implementation: hexBytes(CoordinatorCreationBytecode), RegisterEscrow: registerEscrow, Proxy: append(hexBytes(ERC1967ProxyCreationBytecode), proxyArgs...), GovernanceDrill: hexBytes(CoordinatorAdversaryCreationBytecode), FixVault: fixVault, FixSink: fixSink, PrecompileProbe: append(hexBytes(SubnetProbeCreationBytecode), probeArgs...), ExpectedRuntime: map[common.Address][]byte{}}
	p.ExpectedRuntime[m.ReserveSink], err = runtimeWithImmutables(artifactByName("ReserveSink"), map[string][]byte{"netuid": abiWordUint(uint64(cfg.Netuid)), "reserveHotkey": reserveHotkey[:], "selfColdkey": sinkSelf[:], "bootstrap": abiWordAddress(deployer)})
	if err != nil {
		return nil, err
	}
	p.ExpectedRuntime[m.SettlementVault], err = runtimeWithImmutables(artifactByName("SettlementVault"), map[string][]byte{"netuid": abiWordUint(uint64(cfg.Netuid)), "escrowHotkey": escrowHotkey[:], "selfColdkey": vaultSelf[:], "minimumClaimTTLBlocks": abiWordUint(minimumTTL), "bootstrap": abiWordAddress(deployer)})
	if err != nil {
		return nil, err
	}
	p.ExpectedRuntime[m.CoordinatorImplementation], err = runtimeWithImmutables(artifactByName("Coordinator"), map[string][]byte{"__self": abiWordAddress(m.CoordinatorImplementation)})
	if err != nil {
		return nil, err
	}
	p.ExpectedRuntime[m.CoordinatorProxy], err = runtimeWithImmutables(artifactByName("ERC1967Proxy"), nil)
	if err != nil {
		return nil, err
	}
	p.ExpectedRuntime[m.GovernanceDrillImplementation], err = runtimeWithImmutables(TestnetGovernanceDrillArtifact, map[string][]byte{"__self": abiWordAddress(m.GovernanceDrillImplementation)})
	if err != nil {
		return nil, err
	}
	p.ExpectedRuntime[m.PrecompileProbe], err = runtimeWithImmutables(TestnetPrecompileProbeArtifact, map[string][]byte{"owner": abiWordAddress(deployer), "netuid": abiWordUint(uint64(cfg.Netuid))})
	if err != nil {
		return nil, err
	}
	for addr, code := range p.ExpectedRuntime {
		p.Manifest.RuntimeHashes[addr.Hex()] = crypto.Keccak256Hash(code).Hex()
	}
	return p, nil
}

func roleBytes32(r *RoleSecrets, label string) ([32]byte, error) {
	var out [32]byte
	v, ok := r.Substrate[label]
	if !ok {
		return out, fmt.Errorf("missing substrate role %s", label)
	}
	b, err := hex.DecodeString(v.PublicKeyHex)
	if err != nil || len(b) != 32 {
		return out, fmt.Errorf("invalid role public key %s", label)
	}
	copy(out[:], b)
	return out, nil
}
func artifactByName(name string) ContractArtifact {
	for _, a := range ReleaseContractArtifacts {
		if a.Name == name {
			return a
		}
	}
	panic("unknown generated artifact " + name)
}
func runtimeWithImmutables(a ContractArtifact, values map[string][]byte) ([]byte, error) {
	code := hexBytes(a.RuntimeBytecode)
	if len(values) != len(a.ImmutableReferences) {
		return nil, fmt.Errorf("%s: got %d immutable values, want %d", a.Name, len(values), len(a.ImmutableReferences))
	}
	for name, offsets := range a.ImmutableReferences {
		v, ok := values[name]
		if !ok {
			return nil, fmt.Errorf("%s: missing immutable %s", a.Name, name)
		}
		if len(v) != 32 {
			return nil, fmt.Errorf("%s immutable %s is %d bytes", a.Name, name, len(v))
		}
		for _, off := range offsets {
			if off < 0 || off+32 > len(code) {
				return nil, fmt.Errorf("%s immutable offset out of range", a.Name)
			}
			copy(code[off:off+32], v)
		}
	}
	return code, nil
}
func hexBytes(s string) []byte {
	b, err := hex.DecodeString(stringsTrim0x(s))
	if err != nil {
		panic(err)
	}
	return b
}
func abiWordUint(v uint64) []byte {
	b := make([]byte, 32)
	new(big.Int).SetUint64(v).FillBytes(b)
	return b
}
func abiWordAddress(a common.Address) []byte { b := make([]byte, 32); copy(b[12:], a[:]); return b }
func max64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

type EVMTxManager struct {
	client       *ethclient.Client
	chainID      *big.Int
	deploymentID string
	stateDir     string
	journal      *Journal
	key          *ecdsa.PrivateKey
}

func DialEVMTxManager(ctx context.Context, cfg *ResolvedConfig, stateDir string, j *Journal, roles *RoleSecrets, roleLabel string) (*EVMTxManager, error) {
	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil {
		return nil, err
	}
	id, err := client.ChainID(ctx)
	if err != nil {
		client.Close()
		return nil, err
	}
	if id.Uint64() != testnetChainID {
		client.Close()
		return nil, fmt.Errorf("refusing EVM chain id %d", id.Uint64())
	}
	role, err := roles.EVMKey(roleLabel)
	if err != nil {
		client.Close()
		return nil, err
	}
	key, err := crypto.HexToECDSA(role.PrivateKeyHex)
	if err != nil {
		client.Close()
		return nil, err
	}
	return &EVMTxManager{client: client, chainID: id, deploymentID: cfg.Config.Deployment.DeploymentID, stateDir: stateDir, journal: j, key: key}, nil
}
func (m *EVMTxManager) Close() { m.client.Close() }
func (m *EVMTxManager) PendingNonce(ctx context.Context) (uint64, error) {
	return m.client.PendingNonceAt(ctx, crypto.PubkeyToAddress(m.key.PublicKey))
}

func (m *EVMTxManager) Send(ctx context.Context, planHash string, a Action, to *common.Address, value *big.Int, data []byte) (*types.Receipt, error) {
	if prior, ok := m.journal.LatestTransaction(planHash, a.ID, a.IntentHash); ok {
		rawPath := filepath.Join(m.stateDir, "transactions", stringsTrim0x(prior.TransactionHash)+".rlp")
		raw, err := os.ReadFile(rawPath)
		if err != nil {
			return nil, fmt.Errorf("resume exact EVM transaction %s: %w", prior.TransactionHash, err)
		}
		var tx types.Transaction
		if err := tx.UnmarshalBinary(raw); err != nil {
			return nil, fmt.Errorf("decode persisted EVM transaction %s: %w", prior.TransactionHash, err)
		}
		if !strings.EqualFold(tx.Hash().Hex(), prior.TransactionHash) {
			return nil, fmt.Errorf("persisted EVM transaction hash mismatch: got %s want %s", tx.Hash(), prior.TransactionHash)
		}
		return m.waitExactTransaction(ctx, planHash, a, &tx)
	}
	from := crypto.PubkeyToAddress(m.key.PublicKey)
	nonce, err := m.client.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, err
	}
	tip, err := m.client.SuggestGasTipCap(ctx)
	if err != nil {
		tip = big.NewInt(1_000_000_000)
	}
	header, err := m.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	feeCap := new(big.Int).Set(tip)
	if header.BaseFee != nil {
		feeCap.Add(new(big.Int).Mul(header.BaseFee, big.NewInt(2)), tip)
	}
	msg := ethereum.CallMsg{From: from, To: to, Value: value, Data: data, GasTipCap: tip, GasFeeCap: feeCap}
	gas, err := m.client.EstimateGas(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("estimate %s: %w", a.ID, err)
	}
	gas = gas + gas/5 + 25_000
	maxCost := new(big.Int).Mul(new(big.Int).SetUint64(gas), feeCap)
	if maxCost.BitLen() > 64 || maxCost.Uint64() > a.Spend.EVMGasWei {
		return nil, fmt.Errorf("%s maximum gas %s exceeds action ceiling %d", a.ID, maxCost, a.Spend.EVMGasWei)
	}
	tx := types.NewTx(&types.DynamicFeeTx{ChainID: m.chainID, Nonce: nonce, GasTipCap: tip, GasFeeCap: feeCap, Gas: gas, To: to, Value: value, Data: data})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(m.chainID), m.key)
	if err != nil {
		return nil, err
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return nil, err
	}
	rawPath := filepath.Join(m.stateDir, "transactions", stringsTrim0x(signed.Hash().Hex())+".rlp")
	if err := atomicWrite(rawPath, raw, 0o600); err != nil {
		return nil, err
	}
	recovery, err := finalizedEVMHead(ctx, m.client)
	if err != nil {
		return nil, err
	}
	if err := m.journal.Append(JournalEntry{DeploymentID: m.deploymentID, PlanHash: planHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageBroadcast, Signer: from.Hex(), Nonce: strconv.FormatUint(nonce, 10), TransactionHash: signed.Hash().Hex(), RecoveryBlock: recovery.Number, RecoveryBlockHash: recovery.Hash}); err != nil {
		return nil, err
	}
	return m.waitExactTransaction(ctx, planHash, a, signed)
}

func knownEVMTxError(err error) bool {
	if err == nil {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "known transaction") || strings.Contains(message, "already known") || strings.Contains(message, "already imported") ||
		strings.Contains(message, "nonce too low") || strings.Contains(message, "replacement transaction underpriced")
}

func (m *EVMTxManager) waitExactTransaction(ctx context.Context, planHash string, a Action, signed *types.Transaction) (*types.Receipt, error) {
	if signed == nil {
		return nil, errors.New("nil persisted EVM transaction")
	}
	from, err := types.Sender(types.LatestSignerForChainID(m.chainID), signed)
	if err != nil {
		return nil, fmt.Errorf("recover persisted EVM signer: %w", err)
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		receipt, err := m.client.TransactionReceipt(ctx, signed.Hash())
		if err == nil {
			return m.finalizeReceipt(ctx, planHash, a, receipt)
		}
		if err != ethereum.NotFound {
			return nil, err
		}
		head, err := finalizedEVMHead(ctx, m.client)
		if err != nil {
			return nil, err
		}
		nonce, err := m.client.NonceAt(ctx, from, new(big.Int).SetUint64(head.Number))
		if err != nil {
			return nil, err
		}
		if nonce > signed.Nonce() {
			return nil, fmt.Errorf("EVM nonce %d was consumed by a different finalized transaction", signed.Nonce())
		}
		if err := m.client.SendTransaction(ctx, signed); !knownEVMTxError(err) {
			return nil, fmt.Errorf("rebroadcast exact EVM transaction %s: %w", signed.Hash(), err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *EVMTxManager) finalizeReceipt(ctx context.Context, planHash string, a Action, r *types.Receipt) (*types.Receipt, error) {
	if err := m.journal.Append(JournalEntry{DeploymentID: m.deploymentID, PlanHash: planHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageIncluded, TransactionHash: r.TxHash.Hex(), BlockNumber: r.BlockNumber.Uint64(), BlockHash: r.BlockHash.Hex()}); err != nil {
		return r, err
	}
	finalized, err := waitEVMReceiptFinality(ctx, m.client, r.TxHash)
	if err != nil {
		return r, err
	}
	if finalized.Status != types.ReceiptStatusSuccessful {
		return finalized, fmt.Errorf("EVM transaction %s reverted in its canonical inclusion", finalized.TxHash)
	}
	if err := m.journal.Append(JournalEntry{DeploymentID: m.deploymentID, PlanHash: planHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageFinalized, TransactionHash: finalized.TxHash.Hex(), BlockNumber: finalized.BlockNumber.Uint64(), BlockHash: finalized.BlockHash.Hex()}); err != nil {
		return finalized, err
	}
	return finalized, nil
}
func waitEVMReceiptFinality(ctx context.Context, c *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		var hash string
		if err := c.Client().CallContext(ctx, &hash, "chain_getFinalizedHead"); err != nil {
			return nil, err
		}
		var h struct {
			Number string `json:"number"`
		}
		if err := c.Client().CallContext(ctx, &h, "chain_getHeader", hash); err != nil {
			return nil, err
		}
		n, err := strconv.ParseUint(stringsTrim0x(h.Number), 16, 64)
		if err != nil {
			return nil, err
		}
		receipt, receiptErr := c.TransactionReceipt(ctx, txHash)
		if receiptErr != nil && receiptErr != ethereum.NotFound {
			return nil, receiptErr
		}
		if receiptErr == nil && receipt.BlockNumber != nil && receipt.BlockNumber.IsUint64() && n >= receipt.BlockNumber.Uint64() {
			var canonicalHash string
			if err := c.Client().CallContext(ctx, &canonicalHash, "chain_getBlockHash", receipt.BlockNumber.Uint64()); err != nil {
				return nil, err
			}
			if receiptIsCanonicalAndFinalized(n, receipt, canonicalHash) {
				return receipt, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func receiptIsCanonicalAndFinalized(finalized uint64, receipt *types.Receipt, canonicalHash string) bool {
	return receipt != nil && receipt.BlockNumber != nil && receipt.BlockNumber.IsUint64() &&
		receipt.BlockNumber.Uint64() <= finalized && canonicalHash != "" &&
		strings.EqualFold(canonicalHash, receipt.BlockHash.Hex())
}

func verifyRuntimeCode(ctx context.Context, c *ethclient.Client, expected map[common.Address][]byte) (map[string]string, error) {
	head, err := finalizedEVMHead(ctx, c)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for addr, want := range expected {
		got, err := c.CodeAt(ctx, addr, new(big.Int).SetUint64(head.Number))
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(got, want) {
			return nil, fmt.Errorf("runtime bytecode mismatch at %s: got %s want %s", addr, crypto.Keccak256Hash(got), crypto.Keccak256Hash(want))
		}
		out[addr.Hex()] = crypto.Keccak256Hash(got).Hex()
	}
	return out, nil
}

func saveContractDeployment(stateDir string, m ContractDeployment) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(stateDir, "public", "contracts.json"), append(b, '\n'), 0o644)
}
func loadContractDeployment(stateDir string) (*ContractDeployment, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, "public", "contracts.json"))
	if err != nil {
		return nil, err
	}
	var m ContractDeployment
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
