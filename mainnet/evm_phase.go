// One independent approval seals a bounded installation graph. The first three
// CREATE actions are executable; all later actions remain unexecuted.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const evmPhaseSchema = "urnetwork-mainnet-contract-phase-v1"
const evmPhaseApprovalSchema = "urnetwork-mainnet-contract-phase-approval-v1"
const evmPhaseConfigSchema = "urnetwork-mainnet-contract-phase-config-v1"
const evmCreateStateFile = "reserve-create.json"
const evmVaultCreateStateFile = "vault-create.json"
const evmCoordinatorCreateStateFile = "coordinator-create.json"

// All native transaction fields are explicit; no live quote may replace an
// approved nonce, fee, target, value or byte of calldata during recovery.
type evmPhaseAction struct {
	Id        string          `json:"id"`
	Sender    common.Address  `json:"sender"`
	Nonce     uint64          `json:"nonce"`
	To        *common.Address `json:"to"`
	Data      string          `json:"data"`
	ValueWei  string          `json:"value_wei"`
	Gas       uint64          `json:"gas"`
	FeeCapWei string          `json:"fee_cap_wei"`
	TipCapWei string          `json:"tip_cap_wei"`
}

// The graph can be approved once for all prepared actions. This implementation
// never treats an unimplemented descendant as completed or executable.
type evmPhasePlan struct {
	Schema              string               `json:"schema"`
	DeploymentId        string               `json:"deployment_id"`
	Network             planNetwork          `json:"network"`
	Runtime             rootReceiptProfile   `json:"runtime"`
	Route               ownedSubmissionRoute `json:"route"`
	RunDirectory        string               `json:"run_directory"`
	Artifacts           planFileReference    `json:"artifacts"`
	SourceLockHash      string               `json:"source_lock_hash"`
	CustodyId           string               `json:"custody_id"`
	CustodyFenceHash    string               `json:"custody_fence_hash"`
	CutoverEvidenceHash string               `json:"cutover_evidence_hash"`
	StartNativeNumber   uint64               `json:"start_native_number"`
	StartNativeHash     string               `json:"start_native_hash"`
	ValidThroughNative  uint64               `json:"valid_through_native"`
	MaximumAttempts     uint8                `json:"maximum_attempts"`
	MaximumTotalWei     string               `json:"maximum_total_wei"`
	Netuid              uint16               `json:"netuid"`
	ReserveHotkey       string               `json:"reserve_hotkey"`
	Actions             []evmPhaseAction     `json:"actions"`
}

// Trust is independently provisioned, never learned from retained state.
// The approval signs the whole graph, not an individual resume invocation.
// Its signature is exactly 128 unprefixed lowercase hex characters; the public
// key retains its distinct 0x-prefixed 32-byte encoding.
type evmPhaseConfig struct {
	Schema            string       `json:"schema"`
	ApprovalPublicKey string       `json:"approval_public_key_ed25519"`
	Plan              evmPhasePlan `json:"plan"`
	Signature         string       `json:"approval_signature_ed25519"`
}

// A canonical unsigned integer has no signs, whitespace or alternate spelling.
func evmWei(encoded string) (*big.Int, error) {
	value, ok := new(big.Int).SetString(encoded, 10)
	if !ok || value.Sign() < 0 || value.BitLen() > 256 || value.String() != encoded {
		return nil, errors.New("EVM amount is not a canonical uint256")
	}
	return value, nil
}

// Unsigned envelope construction is the sole transaction-format policy. Only
// EIP-1559 with an empty access list is admitted by this bounded first version.
func (self evmPhaseAction) unsigned() (*types.Transaction, error) {
	value, e1 := evmWei(self.ValueWei)
	fee, e2 := evmWei(self.FeeCapWei)
	tip, e3 := evmWei(self.TipCapWei)
	data, e4 := rootReceiptHex(self.Data, 64*1024)
	if err := errors.Join(e1, e2, e3, e4); err != nil {
		return nil, err
	}
	if self.Sender == (common.Address{}) || self.Gas < 21000 || self.Gas > 100_000_000 || fee.Sign() == 0 || tip.Cmp(fee) > 0 || len(data) == 0 || self.Data != "0x"+hex.EncodeToString(data) || self.To != nil && *self.To == (common.Address{}) {
		return nil, errors.New("EVM action identity or envelope is invalid")
	}
	return types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(mainnetEvmChainId), Nonce: self.Nonce, To: self.To, Data: data, Value: value, Gas: self.Gas, GasFeeCap: fee, GasTipCap: tip, AccessList: types.AccessList{}}), nil
}

// Public signed bytes are decoded, sender-recovered and compared to every
// approved envelope field. Alternate valid signatures cannot replace custody.
func (self evmPhaseAction) signed(raw []byte) (*types.Transaction, error) {
	if len(raw) == 0 || len(raw) > 128*1024 {
		return nil, errors.New("signed EVM transaction exceeds its bound")
	}
	var tx types.Transaction
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, err
	}
	canonical, err := tx.MarshalBinary()
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, errors.New("signed EVM bytes are not canonical")
	}
	expected, err := self.unsigned()
	if err != nil {
		return nil, err
	}
	signer := types.LatestSignerForChainID(big.NewInt(mainnetEvmChainId))
	from, err := types.Sender(signer, &tx)
	if err != nil || from != self.Sender || !tx.Protected() || tx.Type() != types.DynamicFeeTxType || tx.ChainId().Cmp(big.NewInt(mainnetEvmChainId)) != 0 || signer.Hash(&tx) != signer.Hash(expected) {
		return nil, errors.Join(errors.New("signed EVM transaction differs from exact approved envelope"), err)
	}
	return &tx, nil
}

// Typed JSON and a dedicated domain prevent native approval reuse.
func (self evmPhasePlan) signingBytes() ([]byte, error) {
	raw, err := json.Marshal(self)
	return append([]byte(evmPhaseApprovalSchema+"\x00"), raw...), err
}

// The plan hash identifies all graph authority and does not depend on a file's
// whitespace or an imported signature. Raw artifact identity is separately pinned.
func (self evmPhasePlan) hash() string {
	raw, _ := self.signingBytes()
	return rootObjectHash(struct {
		Domain string
		Bytes  []byte
	}{Domain: evmPhaseSchema, Bytes: raw})
}

// Unsigned review enforces the same finite structure, identities, fee envelope
// and key encoding. This check alone never admits an action owner or journal.
func (self evmPhaseConfig) validateStructure() error {
	p := self.Plan
	if self.Schema != evmPhaseConfigSchema || p.Schema != evmPhaseSchema || !planLabel(p.DeploymentId) || p.Network.NativeChain == "" || p.Network.EvmChainId != mainnetEvmChainId || !rootCanonicalHash(p.Network.GenesisHash) || p.Netuid != 25 || !rootCanonicalHash(p.ReserveHotkey) || !planLabel(p.CustodyId) || !planSha256(p.CustodyFenceHash) || !planSha256(p.CutoverEvidenceHash) || !planSha256(p.SourceLockHash) || !planSha256(p.Artifacts.Sha256) || !bootstrapRootAbsolutePath(p.Artifacts.Path) || !bootstrapRootAbsolutePath(p.RunDirectory) || p.StartNativeNumber == 0 || !rootCanonicalHash(p.StartNativeHash) || p.ValidThroughNative <= p.StartNativeNumber || p.ValidThroughNative-p.StartNativeNumber > 7200 || p.MaximumAttempts == 0 || p.MaximumAttempts > 8 || len(p.Actions) == 0 || len(p.Actions) > 9 {
		return errors.New("contract phase lacks exact identity, scope or finite bounds")
	}
	profile := p.Runtime
	if profile.RuntimeSourceCommit != frontierMappingSourceCommit || profile.RuntimeVersion.SpecName == "" || profile.RuntimeVersion.SpecVersion == 0 || !rootCanonicalHash(profile.RuntimeCodeHash) || !rootCanonicalHash(profile.RuntimeMetadataHash) {
		return errors.New("contract phase runtime/source approval is incomplete")
	}
	if p.Route.ReadRetrySeconds < 60 || p.Route.ReadRetrySeconds > 900 || p.Route.SendTimeoutSeconds == 0 || p.Route.SendTimeoutSeconds > 60 {
		return errors.New("contract phase transport bounds are invalid")
	}
	if err := p.Route.validate(); err != nil {
		return err
	}
	ids := []string{"reserve-create", "vault-create", "coordinator-create", "escrow-register", "proxy-create", "reserve-link", "vault-link", "evidence-create", "evidence-anchor"}
	total := new(big.Int)
	seen := map[string]bool{}
	for i, action := range p.Actions {
		if action.Id != ids[i] {
			return errors.New("contract phase action sequence differs")
		}
		tx, err := action.unsigned()
		if err != nil {
			return err
		}
		key := action.Sender.Hex() + ":" + new(big.Int).SetUint64(action.Nonce).String()
		if seen[key] {
			return errors.New("contract phase reuses a sender nonce")
		}
		seen[key] = true
		total.Add(total, new(big.Int).Add(tx.Value(), new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), tx.GasFeeCap())))
	}
	maximum, err := evmWei(p.MaximumTotalWei)
	if err != nil || total.BitLen() > 256 || total.Cmp(maximum) > 0 {
		return errors.Join(errors.New("contract phase exceeds original total cost reservation"), err)
	}
	if p.Actions[0].To != nil || p.Actions[0].ValueWei != "0" {
		return errors.New("reserve action must be a zero-value CREATE")
	}
	key, err := rootReceiptHex(self.ApprovalPublicKey, 32)
	if err != nil || len(key) != 32 {
		return errors.New("contract phase approval key is invalid")
	}
	return nil
}

// Execution always verifies the independently supplied approval after complete
// structural admission. Preview cannot change or waive this authority boundary.
func (self evmPhaseConfig) validate() error {
	if err := self.validateStructure(); err != nil {
		return err
	}
	key, err := rootReceiptHex(self.ApprovalPublicKey, 32)
	if err != nil {
		return err
	}
	signature, err := rootOfflineSignatureBytes(self.Signature)
	if err != nil {
		return errors.Join(errors.New("contract phase approval signature requires 128 unprefixed lowercase hex characters"), err)
	}
	message, msgErr := self.Plan.signingBytes()
	if msgErr != nil {
		return msgErr
	}
	if !ed25519.Verify(key, message, signature) {
		return errors.New("contract phase independent approval is invalid")
	}
	return nil
}

// Selection preserves the whole approved graph. A vault projection retains its
// reserve prerequisite without altering any signed plan field or legacy hash.
type evmCreatePlan struct {
	Config           evmPhaseConfig
	Address          common.Address
	Runtime          []byte
	Getters          []contractGetter
	Storage          []contractStorageWord
	ActionIndex      int
	Reserve          *evmCreatePlan
	Vault            *evmCreatePlan
	Prerequisites    []evmActionRecord
	VaultConstructor *contractVaultConstructor
}

// Only implemented selections may choose a journal or index approved actions.
func (self evmCreatePlan) validateSelection() error {
	if self.ActionIndex == 0 && self.Reserve == nil && self.Vault == nil {
		return nil
	}
	if self.ActionIndex < 1 || self.ActionIndex > 2 || len(self.Config.Plan.Actions) <= self.ActionIndex || self.Reserve == nil || self.Reserve.ActionIndex != 0 || self.Reserve.Reserve != nil || self.Reserve.Vault != nil || rootObjectHash(self.Reserve.Config) != rootObjectHash(self.Config) {
		return errors.New("contract action selection lacks its approved reserve prerequisite")
	}
	reserve, vault := self.Config.Plan.Actions[0], self.Config.Plan.Actions[1]
	if reserve.Nonce == ^uint64(0) || vault.Sender != reserve.Sender || vault.Nonce != reserve.Nonce+1 || vault.To != nil || vault.ValueWei != "0" {
		return errors.New("vault must be the same deployer's next zero-value CREATE")
	}
	if self.ActionIndex == 1 && self.Vault != nil {
		return errors.New("vault selection contains a descendant projection")
	}
	if self.ActionIndex == 2 {
		if self.Vault == nil || self.Vault.ActionIndex != 1 || rootObjectHash(self.Vault.Config) != rootObjectHash(self.Config) {
			return errors.New("coordinator selection lacks the approved vault prerequisite")
		}
		if err := self.Vault.validateSelection(); err != nil {
			return err
		}
		coordinator := self.Config.Plan.Actions[2]
		if vault.Nonce == ^uint64(0) || coordinator.Sender != vault.Sender || coordinator.Nonce != vault.Nonce+1 || coordinator.To != nil || coordinator.ValueWei != "0" {
			return errors.New("coordinator implementation must be the same deployer's next zero-value CREATE")
		}
	}
	return nil
}

// The finite implemented prefix has at most two predecessors, in graph order.
func (self evmCreatePlan) priorPlan(index int) evmCreatePlan {
	if index == 0 {
		return *self.Reserve
	}
	return *self.Vault
}

// Completion names identify only the selected contract, never the whole graph.
func (self evmCreatePlan) completedStatus() string {
	if self.ActionIndex == 2 {
		return "coordinator-created"
	}
	if self.ActionIndex == 1 {
		return "vault-created"
	}
	return "reserve-created"
}

// Explicit vault selection validates its sealed reservation without making
// legacy reserve-only commands depend on unimplemented descendant semantics.
func selectEvmCreatePlan(ctx context.Context, reserve evmCreatePlan, actionId, configPath string) (evmCreatePlan, error) {
	if actionId == "reserve-create" {
		return reserve, nil
	}
	if actionId == "coordinator-create" {
		vault, err := selectEvmCreatePlan(ctx, reserve, "vault-create", configPath)
		if err != nil {
			return evmCreatePlan{}, err
		}
		result := evmCreatePlan{Config: reserve.Config, ActionIndex: 2, Reserve: &reserve, Vault: &vault, VaultConstructor: vault.VaultConstructor}
		if err := result.validateSelection(); err != nil {
			return result, err
		}
		statePath := filepath.Join(result.Config.Plan.RunDirectory, evmCoordinatorCreateStateFile)
		for _, input := range []string{configPath, result.Config.Plan.Artifacts.Path} {
			if input == statePath || input == statePath+".lock" {
				return result, errors.New("contract phase input aliases its coordinator journal")
			}
		}
		artifacts, err := loadContractRelease(ctx, result.Config.Plan.Artifacts)
		if err != nil {
			return result, err
		}
		var artifact contractReleaseArtifact
		for _, candidate := range artifacts.Artifacts {
			if candidate.Name == "Coordinator" {
				artifact = candidate
			}
		}
		action := result.Config.Plan.Actions[2]
		data, runtime, getters, storage, err := contractCoordinatorPayload(artifact, action.Sender, action.Nonce)
		if err != nil || action.Data != "0x"+hex.EncodeToString(data) {
			return result, errors.Join(errors.New("approved coordinator implementation constructor differs from exact release"), err)
		}
		result.Address, result.Runtime, result.Getters, result.Storage = crypto.CreateAddress(action.Sender, action.Nonce), runtime, getters, storage
		return result, nil
	}
	result := evmCreatePlan{Config: reserve.Config, ActionIndex: 1, Reserve: &reserve}
	if actionId != "vault-create" {
		return result, errors.New("contract action is not implemented")
	}
	if err := result.validateSelection(); err != nil {
		return result, err
	}
	p := result.Config.Plan
	statePath := filepath.Join(p.RunDirectory, evmVaultCreateStateFile)
	for _, input := range []string{configPath, p.Artifacts.Path} {
		if input == statePath || input == statePath+".lock" {
			return result, errors.New("contract phase input aliases its vault journal")
		}
	}
	artifacts, err := loadContractRelease(ctx, p.Artifacts)
	if err != nil {
		return result, err
	}
	var artifact contractReleaseArtifact
	for _, candidate := range artifacts.Artifacts {
		if candidate.Name == "SettlementVault" {
			artifact = candidate
		}
	}
	creation, err := contractCode(artifact.Creation, 48*1024)
	if err != nil {
		return result, err
	}
	action := p.Actions[1]
	approved, err := rootReceiptHex(action.Data, 64*1024)
	if err != nil || len(approved) != len(creation)+6*32 || !bytes.Equal(approved[:len(creation)], creation) {
		return result, errors.Join(errors.New("vault payload is not exact release creation and six constructor words"), err)
	}
	arguments := approved[len(creation):]
	var hotkey [32]byte
	copy(hotkey[:], arguments[32:64])
	constructor := contractVaultConstructor{EscrowHotkey: "0x" + hex.EncodeToString(hotkey[:]), MinimumClaimTtlBlocks: binary.BigEndian.Uint64(arguments[120:128]), MinimumTransferTaoRao: binary.BigEndian.Uint64(arguments[152:160])}
	if constructor.EscrowHotkey == p.ReserveHotkey {
		return result, errors.New("vault escrow hotkey must differ from the reserve hotkey")
	}
	data, runtime, getters, err := contractVaultPayload(artifact, p.Netuid, hotkey, constructor.MinimumClaimTtlBlocks, constructor.MinimumTransferTaoRao, action.Sender, action.Nonce)
	if err != nil || !bytes.Equal(approved, data) {
		return result, errors.Join(errors.New("approved vault constructor differs from release and custody identities"), err)
	}
	result.Address, result.Runtime, result.Getters = crypto.CreateAddress(action.Sender, action.Nonce), runtime, getters
	result.VaultConstructor = &constructor
	return result, nil
}

// Both review paths decode one bounded public config with duplicate/unknown
// fields rejected. Signature policy stays with the caller, not the file loader.
func readEvmPhaseConfig(ctx context.Context, path string) (evmPhaseConfig, error) {
	var config evmPhaseConfig
	raw, _, err := readBootstrapRootFile(ctx, path, 2*1024*1024)
	if err != nil {
		return config, err
	}
	if err := decodePlanJson(raw, &config); err != nil {
		return config, err
	}
	return config, nil
}

// Signed commands still require independent approval and an existing private
// custody directory. Read-only unsigned review never calls this admission path.
func loadEvmCreatePlan(ctx context.Context, path string) (evmCreatePlan, error) {
	config, err := readEvmPhaseConfig(ctx, path)
	if err != nil {
		return evmCreatePlan{}, err
	}
	if err := config.validate(); err != nil {
		return evmCreatePlan{}, err
	}
	if err := bootstrapRootDirectory(config.Plan.RunDirectory); err != nil {
		return evmCreatePlan{}, err
	}
	return buildEvmCreatePlan(ctx, config, path)
}

// Rebuilding the constructor and immutables rejects arbitrary CREATE payloads
// in both signed and unsigned review. Only config and artifact files are read;
// the future custody directory is compared syntactically and never inspected.
func buildEvmCreatePlan(ctx context.Context, config evmPhaseConfig, path string) (evmCreatePlan, error) {
	result := evmCreatePlan{Config: config}
	p := config.Plan
	statePath := filepath.Join(p.RunDirectory, evmCreateStateFile)
	for _, input := range []string{path, p.Artifacts.Path} {
		if input == statePath || input == statePath+".lock" {
			return result, errors.New("contract phase input aliases its journal")
		}
	}
	artifacts, err := loadContractRelease(ctx, p.Artifacts)
	if err != nil {
		return result, err
	}
	var artifact contractReleaseArtifact
	for _, candidate := range artifacts.Artifacts {
		if candidate.Name == "ReserveSink" {
			artifact = candidate
		}
	}
	var hotkey [32]byte
	decoded, _ := hex.DecodeString(strings.TrimPrefix(p.ReserveHotkey, "0x"))
	copy(hotkey[:], decoded)
	action := p.Actions[0]
	data, runtime, getters, err := contractReservePayload(artifact, p.Netuid, hotkey, action.Sender, action.Nonce)
	if err != nil || action.Data != "0x"+hex.EncodeToString(data) {
		return result, errors.Join(errors.New("approved reserve constructor differs from release and custody identities"), err)
	}
	result.Address, result.Runtime, result.Getters = crypto.CreateAddress(action.Sender, action.Nonce), runtime, getters
	return result, nil
}
