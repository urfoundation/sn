// Settlement v2 builders rebuild every constructor, call and readback
// expectation from the approved graph and the pinned v2 release. Decoded
// values are review projections; they add no authority beyond the approval.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/big"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Reviewed OpenZeppelin 5 storage coordinates, the same words the original
// coordinator implementation and proxy checks read.
const contractImplementationSlot = "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc"
const contractAdminSlot = "0xb53127684a568b3173ae13b9f8a6016e243e63b6e8ee1178d6a717850b5d6103"
const contractBeaconSlot = "0xa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50"
const contractInitializableSlot = "0xf0c57e16840df040f15088dc2f81fe391c3923bec73e23a9662efc9c229c6a00"
const contractOwnableSlot = "0x9016d09d72d40fdae2fd8ceac6b6234c7706214fd39c1cd1e609a0528c199300"

// The plan's fixed vault2 challenge window, a contract constant.
const contractVault2ChallengeBlocks = 14_400

// The staking precompile, whose collateral view reports registration locks.
const contractStakingPrecompile = "0x0000000000000000000000000000000000000805"

// Review values decoded from the approved vault2 constructor calldata. Netuid,
// the address-derived coldkey and the bootstrap are rebuilt independently.
type contractVault2Constructor struct {
	EscrowHotkey string `json:"escrow_hotkey"`
	DemandFeeWei string `json:"demand_fee_wei"`
}

// The escrow position's registration collateral, as getMinerCollateral on the
// staking precompile reports it (A3).
type contractEscrowCollateral struct {
	Exists     bool   `json:"exists"`
	Locked     uint64 `json:"locked"`
	DrainRatio string `json:"drain_ratio"`
	MinLocked  uint64 `json:"min_locked"`
	Earned     uint64 `json:"earned"`
}

// While SN25's collateral lock share is zero, registration locks nothing and
// no collateral row exists: the view returns (false, 0, 0, 0, 0). A nonzero
// position refuses readback until a new approval reviews it.
var contractEscrowCollateralExpected = contractEscrowCollateral{DrainRatio: "0"}

// Decoded initializer values. A configured owner proves no live Safe authority.
type contractSettlement2Initializer struct {
	Owner         common.Address   `json:"owner"`
	V1Coordinator common.Address   `json:"v1_coordinator"`
	Vault         common.Address   `json:"vault"`
	Auditors      []common.Address `json:"auditors"`
	Quorum        uint64           `json:"quorum"`
}

// The payload is the exact release creation and five constructor words: netuid
// 25, the escrow hotkey, the predicted address's mapped coldkey, the deployer
// as bootstrap and the demand fee. The hotkey and the fee are approved inputs,
// required nonzero, and the fee a whole number of rao (10^9 wei), because the
// runtime truncates sub-rao value (A5). No default fee exists.
func contractVault2Payload(artifact contractReleaseArtifact, p evmPhasePlan) (contractVault2Constructor, []byte, error) {
	var result contractVault2Constructor
	action := p.Actions[0]
	creation, creationErr := contractCode(artifact.Creation, 48*1024)
	approved, approvedErr := rootReceiptHex(action.Data, 64*1024)
	parsed, abiErr := abi.JSON(strings.NewReader(artifact.Abi))
	if err := errors.Join(creationErr, approvedErr, abiErr); err != nil || artifact.Name != "SettlementVault2" || len(approved) != len(creation)+5*32 || !bytes.Equal(approved[:len(creation)], creation) {
		return result, nil, errors.Join(errors.New("vault2 payload is not the exact release creation and five constructor words"), err)
	}
	arguments := approved[len(creation):]
	hotkey, fee := [32]byte(arguments[32:64]), new(big.Int).SetBytes(arguments[128:160])
	mirror := ss58.EvmMirrorPubkey(crypto.CreateAddress(action.Sender, action.Nonce))
	expected, err := parsed.Pack("", p.Netuid, hotkey, mirror, action.Sender, fee)
	if err != nil || hotkey == ([32]byte{}) || fee.Sign() == 0 || new(big.Int).Mod(fee, big.NewInt(1_000_000_000)).Sign() != 0 || !bytes.Equal(arguments, expected) || !bytes.Equal(arguments, stabi.NewSTSettlementVault2().PackConstructor(p.Netuid, hotkey, mirror, action.Sender, fee)) {
		return result, nil, errors.Join(errors.New("approved vault2 constructor differs from its netuid, mapped coldkey and bootstrap, or lacks a nonzero escrow hotkey and a nonzero whole-rao demand fee"), err)
	}
	runtime, err := artifact.withImmutables(map[string][]byte{"netuid": common.BigToHash(big.NewInt(int64(p.Netuid))).Bytes(), "escrowHotkey": hotkey[:], "selfColdkey": mirror[:], "bootstrap": common.BytesToHash(action.Sender[:]).Bytes(), "demandFeeWei": common.BigToHash(fee).Bytes()})
	if err != nil {
		return result, nil, err
	}
	return contractVault2Constructor{EscrowHotkey: common.Hash(hotkey).Hex(), DemandFeeWei: fee.String()}, runtime, nil
}

// Vault2's immutable identity, its coordinator binding and its empty
// accounting, under which conservation holds. liveEscrowStake alone is not
// read back: anyone may transfer stake to vault2's escrow position, and
// conservationHolds already evaluates it through the precompile. The escrow
// flag, its getter and slot zero, is read back only from the escrow
// registration's own receipt on: the back-to-back registration (A7) usually
// executes in vault2's own block, so vault2's creation cannot pin it. Once
// registered, the escrow position's collateral is read back as well.
// Reviewed STSettlementVault2 layout: coordinator in the low 20 bytes of slot
// zero with escrowRegistered at byte offset 20 and the reentrancy flag at
// offset 21, and totalCaptured and totalPaid packed in slot one
// (escrowAccounted is their difference).
func contractVault2State(vault evmCreatePlan, coordinator common.Address, registered bool) ([]contractGetter, []contractStorageWord) {
	contract := stabi.NewSTSettlementVault2()
	fee, _ := new(big.Int).SetString(vault.Vault2Constructor.DemandFeeWei, 10)
	type word struct {
		data  []byte
		value common.Hash
	}
	words := []word{
		{data: contract.PackNetuid(), value: common.BigToHash(big.NewInt(int64(vault.Config.Plan.Netuid)))},
		{data: contract.PackEscrowHotkey(), value: common.HexToHash(vault.Vault2Constructor.EscrowHotkey)},
		{data: contract.PackSelfColdkey(), value: common.Hash(ss58.EvmMirrorPubkey(vault.Address))},
		{data: contract.PackBootstrap(), value: common.BytesToHash(vault.Config.Plan.Actions[0].Sender[:])},
		{data: contract.PackDemandFeeWei(), value: common.BigToHash(fee)},
		{data: contract.PackCHALLENGEBLOCKS(), value: common.BigToHash(big.NewInt(contractVault2ChallengeBlocks))},
		{data: contract.PackCoordinator(), value: common.BytesToHash(coordinator[:])},
	}
	if registered {
		words = append(words, word{data: contract.PackEscrowRegistered(), value: common.BigToHash(big.NewInt(1))})
	}
	words = append(words, word{data: contract.PackTotalCaptured()}, word{data: contract.PackTotalPaid()}, word{data: contract.PackEscrowAccounted()}, word{data: contract.PackConservationHolds(), value: common.BigToHash(big.NewInt(1))})
	getters := []contractGetter{}
	for _, getter := range words {
		getters = append(getters, contractGetter{Data: "0x" + hex.EncodeToString(getter.data), Expected: getter.value.Hex()})
	}
	storage := []contractStorageWord{}
	if registered {
		netuid, hotkey, coldkey := common.BigToHash(big.NewInt(int64(vault.Config.Plan.Netuid))), common.HexToHash(vault.Vault2Constructor.EscrowHotkey), common.Hash(ss58.EvmMirrorPubkey(vault.Address))
		data := append(append(append(crypto.Keccak256([]byte("getMinerCollateral(uint16,bytes32,bytes32)"))[:4], netuid[:]...), hotkey[:]...), coldkey[:]...)
		getters = append(getters, contractGetter{To: contractStakingPrecompile, Data: "0x" + hex.EncodeToString(data), Expected: "0x" + strings.Repeat("00", 5*32)})
		packed := common.BytesToHash(coordinator[:])
		packed[11] = 1
		storage = append(storage, contractStorageWord{Slot: (common.Hash{}).Hex(), Expected: packed.Hex()})
	}
	storage = append(storage, contractStorageWord{Slot: common.BigToHash(big.NewInt(1)).Hex(), Expected: (common.Hash{}).Hex()})
	return getters, storage
}

// Reviewed STSettlement2 layout (evm/layout/STSettlement2.json): slot zero
// v1Coordinator, one vault, two the auditors' length with their addresses from
// keccak256(2), three quorum and four auditorSetVersion.
func contractSettlement2Storage(initializer contractSettlement2Initializer, version uint64) []contractStorageWord {
	word := func(value *big.Int) string { return common.BigToHash(value).Hex() }
	storage := []contractStorageWord{
		{Slot: word(big.NewInt(0)), Expected: common.BytesToHash(initializer.V1Coordinator[:]).Hex()},
		{Slot: word(big.NewInt(1)), Expected: common.BytesToHash(initializer.Vault[:]).Hex()},
		{Slot: word(big.NewInt(2)), Expected: word(big.NewInt(int64(len(initializer.Auditors))))},
		{Slot: word(big.NewInt(3)), Expected: word(new(big.Int).SetUint64(initializer.Quorum))},
		{Slot: word(big.NewInt(4)), Expected: word(new(big.Int).SetUint64(version))},
	}
	data := new(big.Int).SetBytes(crypto.Keccak256(common.BigToHash(big.NewInt(2)).Bytes()))
	for index, auditor := range initializer.Auditors {
		storage = append(storage, contractStorageWord{Slot: word(new(big.Int).Add(data, big.NewInt(int64(index)))), Expected: common.BytesToHash(auditor[:]).Hex()})
	}
	return storage
}

// STSettlement2's public state through either address, plus OpenZeppelin's
// interface version, a constant in both execution contexts.
func contractSettlement2Getters(initializer contractSettlement2Initializer, version uint64) []contractGetter {
	contract := stabi.NewSTSettlement2()
	addresses, _ := abi.NewType("address[]", "", nil)
	text, _ := abi.NewType("string", "", nil)
	auditors, _ := abi.Arguments{{Type: addresses}}.Pack(initializer.Auditors)
	interfaceVersion, _ := abi.Arguments{{Type: text}}.Pack("5.0.0")
	getters := []contractGetter{}
	for _, getter := range []struct {
		data []byte
		word []byte
	}{
		{data: contract.PackVault(), word: common.BytesToHash(initializer.Vault[:]).Bytes()},
		{data: contract.PackV1Coordinator(), word: common.BytesToHash(initializer.V1Coordinator[:]).Bytes()},
		{data: contract.PackAuditors(), word: auditors},
		{data: contract.PackQuorum(), word: common.BigToHash(new(big.Int).SetUint64(initializer.Quorum)).Bytes()},
		{data: contract.PackAuditorSetVersion(), word: common.BigToHash(new(big.Int).SetUint64(version)).Bytes()},
		{data: contract.PackOwner(), word: common.BytesToHash(initializer.Owner[:]).Bytes()},
		{data: contract.PackUPGRADEINTERFACEVERSION(), word: interfaceVersion},
	} {
		getters = append(getters, contractGetter{Data: "0x" + hex.EncodeToString(getter.data), Expected: "0x" + hex.EncodeToString(getter.word)})
	}
	return getters
}

// The implementation constructor only disables initialization; its UUPS self
// word binds the predicted CREATE address. All public state stays zero, its
// own ERC1967 slot is empty and its initializer is disabled at uint64 maximum.
func contractSettlement2Payload(artifact contractReleaseArtifact, action evmPhaseAction) ([]byte, []contractGetter, []contractStorageWord, error) {
	creation, creationErr := contractCode(artifact.Creation, 48*1024)
	parsed, abiErr := abi.JSON(strings.NewReader(artifact.Abi))
	if err := errors.Join(creationErr, abiErr); err != nil || artifact.Name != "Settlement2" || len(parsed.Constructor.Inputs) != 0 || action.Data != "0x"+hex.EncodeToString(creation) {
		return nil, nil, nil, errors.Join(errors.New("approved settlement2 implementation differs from the exact argument-free release creation"), err)
	}
	address := crypto.CreateAddress(action.Sender, action.Nonce)
	runtime, err := artifact.withImmutables(map[string][]byte{"__self": common.BytesToHash(address[:]).Bytes()})
	if err != nil {
		return nil, nil, nil, err
	}
	getters := append(contractSettlement2Getters(contractSettlement2Initializer{}, 0), contractGetter{Data: "0x" + hex.EncodeToString(stabi.NewSTSettlement2().PackProxiableUUID()), Expected: contractImplementationSlot})
	storage := []contractStorageWord{{Slot: contractInitializableSlot, Expected: "0x" + strings.Repeat("00", 24) + strings.Repeat("ff", 8)}, {Slot: contractImplementationSlot, Expected: (common.Hash{}).Hex()}}
	return runtime, getters, append(storage, contractSettlement2Storage(contractSettlement2Initializer{}, 0)...), nil
}

// The optional claim batcher is the exact argument-free release creation. It
// has no immutables and no state, so its exact runtime is its whole readback.
func contractClaimBatcherPayload(artifact contractReleaseArtifact, action evmPhaseAction) ([]byte, error) {
	creation, creationErr := contractCode(artifact.Creation, 48*1024)
	parsed, abiErr := abi.JSON(strings.NewReader(artifact.Abi))
	if err := errors.Join(creationErr, abiErr); err != nil || artifact.Name != "ClaimBatcher" || len(parsed.Constructor.Inputs) != 0 || action.Data != "0x"+hex.EncodeToString(creation) {
		return nil, errors.Join(errors.New("approved claim batcher differs from the exact argument-free release creation"), err)
	}
	return artifact.withImmutables(map[string][]byte{})
}

// Action two calls the derived vault2 with one exact uint64 burn cap and its
// wei funding, the original registration's 10^9 rao conversion.
func contractEscrow2Action(p evmPhasePlan, vault evmCreatePlan) (contractEscrowRegistration, error) {
	var result contractEscrowRegistration
	action := p.Actions[1]
	data, err := rootReceiptHex(action.Data, 36)
	if err != nil || len(data) != 36 || action.To == nil || *action.To != vault.Address {
		return result, errors.Join(errors.New("vault2 escrow registration must be one exact uint64 call to the derived vault2"), err)
	}
	cap := binary.BigEndian.Uint64(data[28:36])
	funding := new(big.Int).Mul(new(big.Int).SetUint64(cap), big.NewInt(1_000_000_000))
	if cap == 0 || !bytes.Equal(data, stabi.NewSTSettlementVault2().PackRegisterEscrow(cap)) || action.ValueWei != funding.String() {
		return result, errors.New("vault2 escrow registration selector, nonzero burn cap or exact wei funding differs")
	}
	mirror := ss58.EvmMirrorPubkey(vault.Address)
	return contractEscrowRegistration{MaximumBurnRao: cap, FundingWei: funding.String(), Hotkey: vault.Vault2Constructor.EscrowHotkey, Coldkey: common.Hash(mirror).Hex()}, nil
}

// The proxy CREATE is the exact release creation with (implementation,
// initializer). The initializer names the derived vault2, a nonzero owner and
// v1 coordinator, and an auditor set within the contract's own bounds. The
// bootstrap deployer can be neither owner nor auditor. Both ABI layers repack
// exactly, refusing shifted offsets, trailing bytes and noncanonical words.
func contractSettlement2ProxyPayload(artifact contractReleaseArtifact, p evmPhasePlan, vault, implementation evmCreatePlan) (contractSettlement2Initializer, []byte, []contractGetter, []contractStorageWord, error) {
	var result contractSettlement2Initializer
	action := p.Actions[3]
	creation, creationErr := contractCode(artifact.Creation, 48*1024)
	approved, approvedErr := rootReceiptHex(action.Data, 64*1024)
	parsed, abiErr := abi.JSON(strings.NewReader(artifact.Abi))
	if err := errors.Join(creationErr, approvedErr, abiErr); err != nil || artifact.Name != "ERC1967Proxy" || len(approved) <= len(creation) || !bytes.Equal(approved[:len(creation)], creation) {
		return result, nil, nil, nil, errors.Join(errors.New("settlement2 proxy payload differs from the exact release creation"), err)
	}
	arguments, err := parsed.Constructor.Inputs.Unpack(approved[len(creation):])
	if err != nil || len(arguments) != 2 {
		return result, nil, nil, nil, errors.Join(errors.New("settlement2 proxy constructor requires an implementation and an atomic initializer"), err)
	}
	target, targetOk := arguments[0].(common.Address)
	initializer, initializerOk := arguments[1].([]byte)
	address, _ := abi.NewType("address", "", nil)
	addresses, _ := abi.NewType("address[]", "", nil)
	number, _ := abi.NewType("uint256", "", nil)
	var values []any
	if initializerOk && len(initializer) >= 4 {
		values, err = abi.Arguments{{Type: address}, {Type: address}, {Type: address}, {Type: addresses}, {Type: number}}.Unpack(initializer[4:])
	}
	if !targetOk || target != implementation.Address || len(values) != 5 {
		return result, nil, nil, nil, errors.Join(errors.New("settlement2 proxy implementation or initializer differs"), err)
	}
	quorum := values[4].(*big.Int)
	result = contractSettlement2Initializer{Owner: values[0].(common.Address), V1Coordinator: values[1].(common.Address), Vault: values[2].(common.Address), Auditors: values[3].([]common.Address)}
	valid := result.Vault == vault.Address && result.Owner != (common.Address{}) && result.V1Coordinator != (common.Address{}) && len(result.Auditors) >= 1 && len(result.Auditors) <= 16 && quorum.Sign() > 0 && quorum.Cmp(big.NewInt(int64(len(result.Auditors)))) <= 0 && result.Owner != action.Sender
	for index, auditor := range result.Auditors {
		valid = valid && auditor != (common.Address{}) && auditor != action.Sender && !slices.Contains(result.Auditors[:index], auditor)
	}
	if !valid {
		return result, nil, nil, nil, errors.New("settlement2 initializer vault, owner, v1 coordinator, auditors, quorum or role separation differs")
	}
	result.Quorum = quorum.Uint64()
	expected := stabi.NewSTSettlement2().PackInitialize(result.Owner, result.V1Coordinator, result.Vault, result.Auditors, quorum)
	packed, err := parsed.Pack("", implementation.Address, expected)
	if err != nil || !bytes.Equal(initializer, expected) || !bytes.Equal(approved, append(append([]byte(nil), creation...), packed...)) {
		return result, nil, nil, nil, errors.Join(errors.New("settlement2 proxy payload differs from the exact release and initializer encoding"), err)
	}
	runtime, err := artifact.withImmutables(map[string][]byte{})
	if err != nil {
		return result, nil, nil, nil, err
	}
	zero := (common.Hash{}).Hex()
	storage := []contractStorageWord{
		{Slot: contractImplementationSlot, Expected: common.BytesToHash(implementation.Address[:]).Hex()},
		{Slot: contractAdminSlot, Expected: zero},
		{Slot: contractBeaconSlot, Expected: zero},
		{Slot: contractInitializableSlot, Expected: "0x" + strings.Repeat("00", 31) + "01"},
		{Slot: contractOwnableSlot, Expected: common.BytesToHash(result.Owner[:]).Hex()},
	}
	return result, runtime, contractSettlement2Getters(result, 1), append(storage, contractSettlement2Storage(result, 1)...), nil
}

// Action five fixes vault2's coordinator to the initialized proxy, once, with
// zero value. Vault2 binds only a nonzero address that already has code.
func contractVault2LinkAction(p evmPhasePlan, vault, proxy evmCreatePlan) (contractVaultBinding, error) {
	action := p.Actions[4]
	data, err := rootReceiptHex(action.Data, 36)
	if err != nil || action.To == nil || *action.To != vault.Address || action.ValueWei != "0" || !bytes.Equal(data, stabi.NewSTSettlementVault2().PackSetCoordinatorOnce(proxy.Address)) {
		return contractVaultBinding{}, errors.Join(errors.New("vault2 binding must call the derived vault2 with the exact initialized proxy and zero value"), err)
	}
	return contractVaultBinding{Vault: vault.Address, Coordinator: proxy.Address}, nil
}
