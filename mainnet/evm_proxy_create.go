// Proxy creation derives governance and policy only from the approved atomic
// initializer. Canonical receipt state binds its actual EVM initialization height.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/ss58"
	"github.com/urfoundation/sn/stabi"
)

// All fields are decoded review values, not independently supplied authority.
// The initializer replaces the approved effective epoch/block during execution.
type contractProxyPolicy struct {
	PolicyHash                   string `json:"policy_hash"`
	EffectiveEpoch               uint64 `json:"effective_epoch"`
	EffectiveBlock               uint64 `json:"effective_block"`
	EpochBlocks                  uint64 `json:"epoch_blocks"`
	RootCommitWindowBlocks       uint64 `json:"root_commit_window_blocks"`
	FinalizeOffsetBlocks         uint64 `json:"finalize_offset_blocks"`
	CloseGraceBlocks             uint64 `json:"close_grace_blocks"`
	ClaimTtlEpochs               uint64 `json:"claim_ttl_epochs"`
	ClaimGraceEpochs             uint64 `json:"claim_grace_epochs"`
	MaximumBindingValidityEpochs uint64 `json:"maximum_binding_validity_epochs"`
	CommitmentMaxAgeBlocks       uint64 `json:"commitment_max_age_blocks"`
	EpochDepositCapRao           string `json:"epoch_deposit_cap_rao"`
	CampaignDepositCapRao        string `json:"campaign_deposit_cap_rao"`
}

// Configured owner identity does not prove live Safe ownership or approval.
type contractProxyConstructor struct {
	Owner            common.Address      `json:"owner"`
	Guardian         common.Address      `json:"guardian"`
	CommitmentOracle common.Address      `json:"commitment_oracle"`
	SelfColdkey      string              `json:"self_coldkey"`
	ApprovedPolicy   contractProxyPolicy `json:"approved_policy"`
}

// The exact generated tuple keeps full uint256 caps and original ignored words.
func (self contractProxyPolicy) binding() stabi.STCoordinatorPolicySnapshot {
	epoch, _ := new(big.Int).SetString(self.EpochDepositCapRao, 10)
	campaign, _ := new(big.Int).SetString(self.CampaignDepositCapRao, 10)
	return stabi.STCoordinatorPolicySnapshot{PolicyHash: common.HexToHash(self.PolicyHash), EffectiveEpoch: self.EffectiveEpoch, EffectiveBlock: self.EffectiveBlock, EpochBlocks: self.EpochBlocks, RootCommitWindowBlocks: self.RootCommitWindowBlocks, FinalizeOffsetBlocks: self.FinalizeOffsetBlocks, CloseGraceBlocks: self.CloseGraceBlocks, ClaimTTLEpochs: self.ClaimTtlEpochs, ClaimGraceEpochs: self.ClaimGraceEpochs, MaximumBindingValidityEpochs: self.MaximumBindingValidityEpochs, CommitmentMaxAgeBlocks: self.CommitmentMaxAgeBlocks, EpochDepositCapRao: epoch, CampaignDepositCapRao: campaign}
}

// Repacking both ABI layers rejects shifted offsets, trailing bytes, narrowed
// integers, alternate implementations and noncanonical initializer arguments.
func contractProxyPayload(artifact contractReleaseArtifact, plan evmPhasePlan, vault, implementation evmCreatePlan) (contractProxyConstructor, []byte, []contractGetter, []contractStorageWord, error) {
	var constructor contractProxyConstructor
	if artifact.Name != "ERC1967Proxy" || len(plan.Actions) < 5 || vault.VaultConstructor == nil {
		return constructor, nil, nil, nil, errors.New("proxy lacks its approved constructor predecessors")
	}
	action := plan.Actions[4]
	creation, err := contractCode(artifact.Creation, 48*1024)
	if err != nil {
		return constructor, nil, nil, nil, err
	}
	approved, err := rootReceiptHex(action.Data, 64*1024)
	if err != nil || len(approved) <= len(creation) || !bytes.Equal(approved[:len(creation)], creation) {
		return constructor, nil, nil, nil, errors.New("proxy payload differs from exact release creation")
	}
	parsed, err := abi.JSON(strings.NewReader(artifact.Abi))
	if err != nil {
		return constructor, nil, nil, nil, err
	}
	arguments, err := parsed.Constructor.Inputs.Unpack(approved[len(creation):])
	if err != nil || len(arguments) != 2 {
		return constructor, nil, nil, nil, errors.New("proxy constructor requires implementation and atomic initializer")
	}
	address, addressOk := arguments[0].(common.Address)
	initializer, initializerOk := arguments[1].([]byte)
	if !addressOk || address != implementation.Address || !initializerOk || len(initializer) != 4+20*32 {
		return constructor, nil, nil, nil, errors.New("proxy implementation or complete initializer differs")
	}
	word := func(index int) []byte { return initializer[4+32*index : 4+32*(index+1)] }
	value := func(index int) uint64 { return binary.BigEndian.Uint64(word(index)[24:]) }
	constructor = contractProxyConstructor{Owner: common.BytesToAddress(word(1)), Guardian: common.BytesToAddress(word(2)), CommitmentOracle: common.BytesToAddress(word(6)), SelfColdkey: common.BytesToHash(word(3)).Hex(), ApprovedPolicy: contractProxyPolicy{PolicyHash: common.BytesToHash(word(7)).Hex(), EffectiveEpoch: value(8), EffectiveBlock: value(9), EpochBlocks: value(10), RootCommitWindowBlocks: value(11), FinalizeOffsetBlocks: value(12), CloseGraceBlocks: value(13), ClaimTtlEpochs: value(14), ClaimGraceEpochs: value(15), MaximumBindingValidityEpochs: value(16), CommitmentMaxAgeBlocks: value(17), EpochDepositCapRao: new(big.Int).SetBytes(word(18)).String(), CampaignDepositCapRao: new(big.Int).SetBytes(word(19)).String()}}
	p := constructor.ApprovedPolicy.binding()
	if constructor.Owner == (common.Address{}) || constructor.Guardian == (common.Address{}) || constructor.CommitmentOracle == (common.Address{}) || p.PolicyHash == ([32]byte{}) || p.EpochBlocks == 0 || p.RootCommitWindowBlocks == 0 || p.FinalizeOffsetBlocks == 0 || p.CloseGraceBlocks == 0 || p.ClaimTTLEpochs == 0 || p.MaximumBindingValidityEpochs == 0 || p.CommitmentMaxAgeBlocks == 0 || p.EpochDepositCapRao.Sign() == 0 || p.CampaignDepositCapRao.Sign() == 0 || p.CloseGraceBlocks > p.RootCommitWindowBlocks || p.RootCommitWindowBlocks > p.FinalizeOffsetBlocks || p.FinalizeOffsetBlocks >= p.EpochBlocks || p.ClaimGraceEpochs > p.ClaimTTLEpochs || p.EpochDepositCapRao.Cmp(p.CampaignDepositCapRao) > 0 {
		return constructor, nil, nil, nil, errors.New("proxy initializer roles or policy differ from source bounds")
	}
	horizon := new(big.Int).Add(new(big.Int).SetUint64(p.ClaimTTLEpochs), new(big.Int).SetUint64(p.ClaimGraceEpochs))
	horizon.Mul(horizon, new(big.Int).SetUint64(p.EpochBlocks))
	required := new(big.Int).Add(new(big.Int).SetUint64(vault.VaultConstructor.MinimumClaimTtlBlocks), new(big.Int).SetUint64(p.FinalizeOffsetBlocks))
	if horizon.Cmp(required.Add(required, big.NewInt(1))) < 0 {
		return constructor, nil, nil, nil, errors.New("proxy initial policy cannot satisfy immutable vault claim window")
	}
	proxy := crypto.CreateAddress(action.Sender, action.Nonce)
	mirror := ss58.EvmMirrorPubkey(proxy)
	binding := stabi.NewSTCoordinator()
	expected := binding.PackInitialize(plan.Netuid, constructor.Owner, constructor.Guardian, mirror, vault.Address, crypto.CreateAddress(plan.Actions[0].Sender, plan.Actions[0].Nonce), constructor.CommitmentOracle, p)
	packed, err := parsed.Pack("", implementation.Address, expected)
	if err != nil || !bytes.Equal(initializer, expected) || !bytes.Equal(approved, append(append([]byte(nil), creation...), packed...)) {
		return constructor, nil, nil, nil, errors.New("proxy atomic initializer differs from exact approved release encoding or graph identities")
	}
	runtime, err := artifact.withImmutables(map[string][]byte{})
	if err != nil {
		return constructor, nil, nil, nil, err
	}
	zero := "0x" + strings.Repeat("00", 32)
	addressWord := func(address common.Address) string {
		return "0x" + hex.EncodeToString(common.LeftPadBytes(address.Bytes(), 32))
	}
	getters := []contractGetter{}
	for _, query := range []struct {
		data []byte
		word string
	}{
		{data: binding.PackNetuid(), word: "0x" + hex.EncodeToString(common.LeftPadBytes(new(big.Int).SetUint64(uint64(plan.Netuid)).Bytes(), 32))},
		{data: binding.PackSelfColdkey(), word: constructor.SelfColdkey},
		{data: binding.PackSettlementVault(), word: addressWord(vault.Address)},
		{data: binding.PackReserveSink(), word: addressWord(crypto.CreateAddress(plan.Actions[0].Sender, plan.Actions[0].Nonce))},
		{data: binding.PackOwner(), word: addressWord(constructor.Owner)},
		{data: binding.PackGuardian(), word: addressWord(constructor.Guardian)},
		{data: binding.PackActiveGuardian(), word: addressWord(constructor.Guardian)},
		{data: binding.PackCommitmentOracle(), word: addressWord(constructor.CommitmentOracle)},
		{data: binding.PackActiveCommitmentOracle(), word: addressWord(constructor.CommitmentOracle)},
		{data: binding.PackPolicyCount(), word: "0x" + strings.Repeat("00", 31) + "01"},
	} {
		getters = append(getters, contractGetter{Data: "0x" + hex.EncodeToString(query.data), Expected: query.word})
	}
	for _, data := range [][]byte{binding.PackPendingGuardian(), binding.PackPendingGuardianEpoch(), binding.PackPaused(), binding.PackCampaignReserved(), binding.PackPendingCommitmentOracle(), binding.PackPendingCommitmentOracleEpoch(), binding.PackValidatorEvidence(), binding.PackOperatorCount(), binding.PackCurrentEpoch()} {
		getters = append(getters, contractGetter{Data: "0x" + hex.EncodeToString(data), Expected: zero})
	}
	// proxiableUUID deliberately reverts through a proxy; the interface version
	// is a direct constant getter in both execution contexts.
	coordinatorAbi, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return constructor, nil, nil, nil, err
	}
	version, err := coordinatorAbi.Methods["UPGRADE_INTERFACE_VERSION"].Outputs.Pack("5.0.0")
	if err != nil {
		return constructor, nil, nil, nil, err
	}
	getters = append(getters, contractGetter{Data: "0x" + hex.EncodeToString(binding.PackUPGRADEINTERFACEVERSION()), Expected: "0x" + hex.EncodeToString(version)})
	storage := []contractStorageWord{
		{Slot: "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc", Expected: addressWord(implementation.Address)},
		{Slot: "0xb53127684a568b3173ae13b9f8a6016e243e63b6e8ee1178d6a717850b5d6103", Expected: zero},
		{Slot: "0xa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50", Expected: zero},
		{Slot: "0xf0c57e16840df040f15088dc2f81fe391c3923bec73e23a9662efc9c229c6a00", Expected: "0x" + strings.Repeat("00", 31) + "01"},
		{Slot: "0x9016d09d72d40fdae2fd8ceac6b6234c7706214fd39c1cd1e609a0528c199300", Expected: addressWord(constructor.Owner)},
	}
	return constructor, runtime, getters, storage, nil
}

// Initial policy block is an EVM observation, never the native checkpoint. The
// same deterministic projection verifies online and retained completion hashes.
func (self evmCreatePlan) receiptGetters(receipt evmCreateReceipt) ([]contractGetter, error) {
	if self.ActionIndex != 4 {
		return self.Getters, nil
	}
	if self.ProxyConstructor == nil || receipt.BlockNumber == 0 {
		return nil, errors.New("proxy policy observation lacks its authenticated EVM inclusion")
	}
	policy := self.ProxyConstructor.ApprovedPolicy.binding()
	policy.EffectiveEpoch, policy.EffectiveBlock = 0, receipt.BlockNumber
	binding, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, err
	}
	encoded, err := binding.Methods["policyByIndex"].Outputs.Pack(policy)
	if err != nil {
		return nil, err
	}
	contract := stabi.NewSTCoordinator()
	start := new(big.Int).SetUint64(receipt.BlockNumber)
	end := new(big.Int).Add(start, new(big.Int).SetUint64(policy.EpochBlocks))
	getters := append([]contractGetter(nil), self.Getters...)
	for _, query := range []struct {
		data     []byte
		expected []byte
	}{
		{data: contract.PackPolicyByIndex(big.NewInt(0)), expected: encoded},
		{data: contract.PackPolicyAt(big.NewInt(0)), expected: encoded},
		{data: contract.PackEpochStartBlock(big.NewInt(0)), expected: common.LeftPadBytes(start.Bytes(), 32)},
		{data: contract.PackEpochEndBlock(big.NewInt(0)), expected: common.LeftPadBytes(end.Bytes(), 32)},
	} {
		getters = append(getters, contractGetter{Data: "0x" + hex.EncodeToString(query.data), Expected: "0x" + hex.EncodeToString(query.expected)})
	}
	return getters, nil
}

// The implementation slot names the already reviewed code at this same proxy
// inclusion, independently from its historical predecessor receipt.
func (self *evmOwnedChain) authenticateProxyImplementation(ctx context.Context, plan evmCreatePlan, block map[string]any) error {
	var code string
	if err := self.read(ctx, "eth_getCode", []any{plan.Coordinator.Address.Hex(), block}, &code); err != nil {
		return err
	}
	if code != "0x"+hex.EncodeToString(plan.Coordinator.Runtime) {
		return errors.New("proxy canonical implementation runtime differs")
	}
	for _, expected := range plan.Coordinator.Storage {
		var word string
		if err := self.read(ctx, "eth_getStorageAt", []any{plan.Coordinator.Address.Hex(), expected.Slot, block}, &word); err != nil {
			return err
		}
		if word != expected.Expected {
			return errors.New("proxy canonical implementation storage differs")
		}
	}
	return nil
}
