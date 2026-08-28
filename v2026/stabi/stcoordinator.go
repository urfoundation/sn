// Code generated via abigen V2 - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package stabi

import (
	"bytes"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = bytes.Equal
	_ = errors.New
	_ = big.NewInt
	_ = common.Big1
	_ = types.BloomLookup
	_ = abi.ConvertType
)

// STCoordinatorBindingRecord is an auto generated low-level Go binding around an user-defined struct.
type STCoordinatorBindingRecord struct {
	FleetId        [32]byte
	Hotkey         [32]byte
	ClientKey      [32]byte
	CommitmentHash [32]byte
	Generation     uint64
	ValidFromEpoch uint64
	ValidToEpoch   uint64
	CleanedAtEpoch uint64
	Uid            uint16
	Cleaned        bool
}

// STCoordinatorFleetBinding is an auto generated low-level Go binding around an user-defined struct.
type STCoordinatorFleetBinding struct {
	ChainId        uint64
	Netuid         uint16
	Coordinator    common.Address
	FleetId        [32]byte
	Hotkey         [32]byte
	ClientId       [16]byte
	ClientKey      [32]byte
	Generation     uint64
	ValidFromEpoch uint64
	ValidToEpoch   uint64
	CommitmentHash [32]byte
}

// STCoordinatorOperatorVersion is an auto generated low-level Go binding around an user-defined struct.
type STCoordinatorOperatorVersion struct {
	Coldkey        [32]byte
	PoolHotkey     [32]byte
	DepositHotkey  [32]byte
	DepositSigner  common.Address
	RootSigner     common.Address
	EffectiveEpoch uint64
	Active         bool
}

// STCoordinatorPolicySnapshot is an auto generated low-level Go binding around an user-defined struct.
type STCoordinatorPolicySnapshot struct {
	PolicyHash                   [32]byte
	EffectiveEpoch               uint64
	EffectiveBlock               uint64
	EpochBlocks                  uint64
	RootCommitWindowBlocks       uint64
	FinalizeOffsetBlocks         uint64
	CloseGraceBlocks             uint64
	ClaimTTLEpochs               uint64
	ClaimGraceEpochs             uint64
	MaximumBindingValidityEpochs uint64
	CommitmentMaxAgeBlocks       uint64
	EpochDepositCapRao           *big.Int
	CampaignDepositCapRao        *big.Int
}

// STCoordinatorMetaData contains all meta data concerning the STCoordinator contract.
var STCoordinatorMetaData = bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"receive\",\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"FLEET_BINDING_DOMAIN\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"FLEET_REVOKE_DOMAIN\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MAX_OPERATOR_VERSIONS\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MAX_POLICY_VERSIONS\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"UPGRADE_INTERFACE_VERSION\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"activeCommitmentOracle\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"activeGuardian\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"addConviction\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"nonce\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"deadlineBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"bindFleetMember\",\"inputs\":[{\"name\":\"binding\",\"type\":\"tuple\",\"internalType\":\"structSTCoordinator.FleetBinding\",\"components\":[{\"name\":\"chainId\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"netuid\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"coordinator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"fleetId\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"clientId\",\"type\":\"bytes16\",\"internalType\":\"bytes16\"},{\"name\":\"clientKey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"generation\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"validFromEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"validToEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"commitmentHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}]},{\"name\":\"clientSignature\",\"type\":\"bytes\",\"internalType\":\"bytes\"},{\"name\":\"hotkeySignature\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[{\"name\":\"uid\",\"type\":\"uint16\",\"internalType\":\"uint16\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"bindingAt\",\"inputs\":[{\"name\":\"clientId\",\"type\":\"bytes16\",\"internalType\":\"bytes16\"},{\"name\":\"epoch_\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"active\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"record\",\"type\":\"tuple\",\"internalType\":\"structSTCoordinator.BindingRecord\",\"components\":[{\"name\":\"fleetId\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"clientKey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"commitmentHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"generation\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"validFromEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"validToEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"cleanedAtEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"uid\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"cleaned\",\"type\":\"bool\",\"internalType\":\"bool\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"bindingVersionAt\",\"inputs\":[{\"name\":\"clientId\",\"type\":\"bytes16\",\"internalType\":\"bytes16\"},{\"name\":\"index\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple\",\"internalType\":\"structSTCoordinator.BindingRecord\",\"components\":[{\"name\":\"fleetId\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"clientKey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"commitmentHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"generation\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"validFromEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"validToEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"cleanedAtEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"uid\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"cleaned\",\"type\":\"bool\",\"internalType\":\"bool\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"bindingVersionCount\",\"inputs\":[{\"name\":\"clientId\",\"type\":\"bytes16\",\"internalType\":\"bytes16\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"campaignReserved\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"cleanupFleetBinding\",\"inputs\":[{\"name\":\"clientId\",\"type\":\"bytes16\",\"internalType\":\"bytes16\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"closeOperatorEpoch\",\"inputs\":[{\"name\":\"epoch_\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"commitOperatorRoot\",\"inputs\":[{\"name\":\"epoch_\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"payoutRoot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"artifactHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"commitmentOracle\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"cumulativeConviction\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"currentEpoch\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"deferMissedEmission\",\"inputs\":[{\"name\":\"epoch_\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"deposit\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"nonce\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"deadlineBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"depositHotkeyUsed\",\"inputs\":[{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"used\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"epochConvictionAdded\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"epochDeposits\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"epochEndBlock\",\"inputs\":[{\"name\":\"epoch_\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"epochStartBlock\",\"inputs\":[{\"name\":\"epoch_\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"finalizeOperatorEpoch\",\"inputs\":[{\"name\":\"epoch_\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"fleetBindingDigest\",\"inputs\":[{\"name\":\"binding\",\"type\":\"tuple\",\"internalType\":\"structSTCoordinator.FleetBinding\",\"components\":[{\"name\":\"chainId\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"netuid\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"coordinator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"fleetId\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"clientId\",\"type\":\"bytes16\",\"internalType\":\"bytes16\"},{\"name\":\"clientKey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"generation\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"validFromEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"validToEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"commitmentHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}]}],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"pure\"},{\"type\":\"function\",\"name\":\"fleetMemberCount\",\"inputs\":[{\"name\":\"fleetId\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"members\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"fleetRevokeDigest\",\"inputs\":[{\"name\":\"clientId\",\"type\":\"bytes16\",\"internalType\":\"bytes16\"},{\"name\":\"generation\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getFleetBinding\",\"inputs\":[{\"name\":\"clientId\",\"type\":\"bytes16\",\"internalType\":\"bytes16\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple\",\"internalType\":\"structSTCoordinator.BindingRecord\",\"components\":[{\"name\":\"fleetId\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"clientKey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"commitmentHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"generation\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"validFromEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"validToEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"cleanedAtEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"uid\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"cleaned\",\"type\":\"bool\",\"internalType\":\"bool\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"guardian\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"initialize\",\"inputs\":[{\"name\":\"netuid_\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"owner_\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"guardian_\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"selfColdkey_\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"settlementVault_\",\"type\":\"address\",\"internalType\":\"contractSTSettlementVault\"},{\"name\":\"reserveSink_\",\"type\":\"address\",\"internalType\":\"contractSTReserveSink\"},{\"name\":\"commitmentOracle_\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"initialPolicy\",\"type\":\"tuple\",\"internalType\":\"structSTCoordinator.PolicySnapshot\",\"components\":[{\"name\":\"policyHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"effectiveBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"epochBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"rootCommitWindowBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"finalizeOffsetBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"closeGraceBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"claimTTLEpochs\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"claimGraceEpochs\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"maximumBindingValidityEpochs\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"commitmentMaxAgeBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"epochDepositCapRao\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"campaignDepositCapRao\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"mirrorCommitment\",\"inputs\":[{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"commitmentHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"finalizedBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"finalizedBlockHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"mirroredCommitments\",\"inputs\":[{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"commitmentHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"finalizedBlockHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"finalizedBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"netuid\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint16\",\"internalType\":\"uint16\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"nextDepositNonce\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"nonce\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"operatorAt\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"epoch_\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple\",\"internalType\":\"structSTCoordinator.OperatorVersion\",\"components\":[{\"name\":\"coldkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"poolHotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"depositHotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"depositSigner\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"rootSigner\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"active\",\"type\":\"bool\",\"internalType\":\"bool\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"operatorCount\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"operatorIdAt\",\"inputs\":[{\"name\":\"index\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"operatorVersionCount\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"owner\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"paused\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"pendingCommitmentOracle\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"pendingCommitmentOracleEpoch\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"pendingGuardian\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"pendingGuardianEpoch\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"policyAt\",\"inputs\":[{\"name\":\"epoch_\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple\",\"internalType\":\"structSTCoordinator.PolicySnapshot\",\"components\":[{\"name\":\"policyHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"effectiveBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"epochBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"rootCommitWindowBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"finalizeOffsetBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"closeGraceBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"claimTTLEpochs\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"claimGraceEpochs\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"maximumBindingValidityEpochs\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"commitmentMaxAgeBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"epochDepositCapRao\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"campaignDepositCapRao\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"policyByIndex\",\"inputs\":[{\"name\":\"index\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple\",\"internalType\":\"structSTCoordinator.PolicySnapshot\",\"components\":[{\"name\":\"policyHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"effectiveBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"epochBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"rootCommitWindowBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"finalizeOffsetBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"closeGraceBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"claimTTLEpochs\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"claimGraceEpochs\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"maximumBindingValidityEpochs\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"commitmentMaxAgeBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"epochDepositCapRao\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"campaignDepositCapRao\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"policyCount\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"proxiableUUID\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"registerOperator\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"coldkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"poolHotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"depositHotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"depositSigner\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"rootSigner\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"maximumBurnRao\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"outputs\":[{\"name\":\"uid\",\"type\":\"uint16\",\"internalType\":\"uint16\"}],\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"renounceOwnership\",\"inputs\":[],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"reserveSink\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"contractSTReserveSink\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"revokeFleetBinding\",\"inputs\":[{\"name\":\"clientId\",\"type\":\"bytes16\",\"internalType\":\"bytes16\"},{\"name\":\"generation\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"clientSignature\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"rootCommitments\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"payoutRoot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"artifactHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"committer\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"commitBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"scheduleCommitmentOracle\",\"inputs\":[{\"name\":\"oracle\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"scheduleGuardian\",\"inputs\":[{\"name\":\"guardian_\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"scheduleOperator\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"depositHotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"depositSigner\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"rootSigner\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"active\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"schedulePolicy\",\"inputs\":[{\"name\":\"next\",\"type\":\"tuple\",\"internalType\":\"structSTCoordinator.PolicySnapshot\",\"components\":[{\"name\":\"policyHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"effectiveBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"epochBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"rootCommitWindowBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"finalizeOffsetBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"closeGraceBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"claimTTLEpochs\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"claimGraceEpochs\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"maximumBindingValidityEpochs\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"commitmentMaxAgeBlocks\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"epochDepositCapRao\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"campaignDepositCapRao\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"selfColdkey\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"setPaused\",\"inputs\":[{\"name\":\"paused_\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"settlementVault\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"contractSTSettlementVault\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"transferOwnership\",\"inputs\":[{\"name\":\"newOwner\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"upgradeToAndCall\",\"inputs\":[{\"name\":\"newImplementation\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"payable\"},{\"type\":\"event\",\"name\":\"CommitmentMirrored\",\"inputs\":[{\"name\":\"hotkey\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"commitmentHash\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"finalizedBlock\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"},{\"name\":\"finalizedBlockHash\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"CommitmentOracleScheduled\",\"inputs\":[{\"name\":\"oracle\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"indexed\":true,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ConvictionAdded\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"epoch\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"funder\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"policyHash\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"nonce\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Deposit\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"epoch\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"funder\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"policyHash\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"nonce\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"FleetBindingCleaned\",\"inputs\":[{\"name\":\"clientId\",\"type\":\"bytes16\",\"indexed\":true,\"internalType\":\"bytes16\"},{\"name\":\"cleanedAtEpoch\",\"type\":\"uint64\",\"indexed\":true,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"FleetBindingRevoked\",\"inputs\":[{\"name\":\"clientId\",\"type\":\"bytes16\",\"indexed\":true,\"internalType\":\"bytes16\"},{\"name\":\"generation\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"FleetBound\",\"inputs\":[{\"name\":\"clientId\",\"type\":\"bytes16\",\"indexed\":true,\"internalType\":\"bytes16\"},{\"name\":\"fleetId\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"hotkey\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"uid\",\"type\":\"uint16\",\"indexed\":false,\"internalType\":\"uint16\"},{\"name\":\"generation\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"},{\"name\":\"validFromEpoch\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"},{\"name\":\"validToEpoch\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"GuardianScheduled\",\"inputs\":[{\"name\":\"guardian\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"indexed\":true,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"GuardianSet\",\"inputs\":[{\"name\":\"guardian\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Initialized\",\"inputs\":[{\"name\":\"version\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OperatorEpochFinalized\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"rootPresent\",\"type\":\"bool\",\"indexed\":false,\"internalType\":\"bool\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OperatorRootCommitted\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"payoutRoot\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"artifactHash\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"committer\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OperatorScheduled\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"indexed\":true,\"internalType\":\"uint64\"},{\"name\":\"coldkey\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"poolHotkey\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"depositHotkey\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"depositSigner\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"},{\"name\":\"rootSigner\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"},{\"name\":\"active\",\"type\":\"bool\",\"indexed\":false,\"internalType\":\"bool\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OwnershipTransferred\",\"inputs\":[{\"name\":\"previousOwner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"newOwner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"PausedSet\",\"inputs\":[{\"name\":\"paused\",\"type\":\"bool\",\"indexed\":false,\"internalType\":\"bool\"},{\"name\":\"caller\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"PolicyScheduled\",\"inputs\":[{\"name\":\"index\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"policyHash\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"effectiveEpoch\",\"type\":\"uint64\",\"indexed\":true,\"internalType\":\"uint64\"},{\"name\":\"effectiveBlock\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Upgraded\",\"inputs\":[{\"name\":\"implementation\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"AddressEmptyCode\",\"inputs\":[{\"name\":\"target\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"AlreadyCommitted\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"CapExceeded\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"DeadlineExpired\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"ERC1967InvalidImplementation\",\"inputs\":[{\"name\":\"implementation\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ERC1967NonPayable\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"FailedCall\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"FundsNotReceived\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InactiveOperator\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidBinding\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidConfiguration\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidEpoch\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidInitialization\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidNonce\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidPolicy\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidSignature\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidWindow\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"NativeRefundFailed\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"NotInitializing\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"OwnableInvalidOwner\",\"inputs\":[{\"name\":\"owner\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"OwnableUnauthorizedAccount\",\"inputs\":[{\"name\":\"account\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"Paused\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"Reentrancy\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"RuntimeIdentityMissing\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"SafeCastOverflowedUintDowncast\",\"inputs\":[{\"name\":\"bits\",\"type\":\"uint8\",\"internalType\":\"uint8\"},{\"name\":\"value\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"StaleCommitment\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"UUPSUnauthorizedCallContext\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"UUPSUnsupportedProxiableUUID\",\"inputs\":[{\"name\":\"slot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}]},{\"type\":\"error\",\"name\":\"Unauthorized\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"UnknownOperator\",\"inputs\":[]}]",
	ID:  "STCoordinator",
}

// STCoordinator is an auto generated Go binding around an Ethereum contract.
type STCoordinator struct {
	abi abi.ABI
}

// NewSTCoordinator creates a new instance of STCoordinator.
func NewSTCoordinator() *STCoordinator {
	parsed, err := STCoordinatorMetaData.ParseABI()
	if err != nil {
		panic(errors.New("invalid ABI: " + err.Error()))
	}
	return &STCoordinator{abi: *parsed}
}

// Instance creates a wrapper for a deployed contract instance at the given address.
// Use this to create the instance object passed to abigen v2 library functions Call, Transact, etc.
func (c *STCoordinator) Instance(backend bind.ContractBackend, addr common.Address) *bind.BoundContract {
	return bind.NewBoundContract(addr, c.abi, backend, backend, backend)
}

// PackFLEETBINDINGDOMAIN is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa7c29b84.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function FLEET_BINDING_DOMAIN() view returns(string)
func (sTCoordinator *STCoordinator) PackFLEETBINDINGDOMAIN() []byte {
	enc, err := sTCoordinator.abi.Pack("FLEET_BINDING_DOMAIN")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackFLEETBINDINGDOMAIN is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa7c29b84.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function FLEET_BINDING_DOMAIN() view returns(string)
func (sTCoordinator *STCoordinator) TryPackFLEETBINDINGDOMAIN() ([]byte, error) {
	return sTCoordinator.abi.Pack("FLEET_BINDING_DOMAIN")
}

// UnpackFLEETBINDINGDOMAIN is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xa7c29b84.
//
// Solidity: function FLEET_BINDING_DOMAIN() view returns(string)
func (sTCoordinator *STCoordinator) UnpackFLEETBINDINGDOMAIN(data []byte) (string, error) {
	out, err := sTCoordinator.abi.Unpack("FLEET_BINDING_DOMAIN", data)
	if err != nil {
		return *new(string), err
	}
	out0 := *abi.ConvertType(out[0], new(string)).(*string)
	return out0, nil
}

// PackFLEETREVOKEDOMAIN is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x61f11fe7.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function FLEET_REVOKE_DOMAIN() view returns(string)
func (sTCoordinator *STCoordinator) PackFLEETREVOKEDOMAIN() []byte {
	enc, err := sTCoordinator.abi.Pack("FLEET_REVOKE_DOMAIN")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackFLEETREVOKEDOMAIN is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x61f11fe7.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function FLEET_REVOKE_DOMAIN() view returns(string)
func (sTCoordinator *STCoordinator) TryPackFLEETREVOKEDOMAIN() ([]byte, error) {
	return sTCoordinator.abi.Pack("FLEET_REVOKE_DOMAIN")
}

// UnpackFLEETREVOKEDOMAIN is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x61f11fe7.
//
// Solidity: function FLEET_REVOKE_DOMAIN() view returns(string)
func (sTCoordinator *STCoordinator) UnpackFLEETREVOKEDOMAIN(data []byte) (string, error) {
	out, err := sTCoordinator.abi.Unpack("FLEET_REVOKE_DOMAIN", data)
	if err != nil {
		return *new(string), err
	}
	out0 := *abi.ConvertType(out[0], new(string)).(*string)
	return out0, nil
}

// PackMAXOPERATORVERSIONS is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x98f7f625.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function MAX_OPERATOR_VERSIONS() view returns(uint256)
func (sTCoordinator *STCoordinator) PackMAXOPERATORVERSIONS() []byte {
	enc, err := sTCoordinator.abi.Pack("MAX_OPERATOR_VERSIONS")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackMAXOPERATORVERSIONS is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x98f7f625.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function MAX_OPERATOR_VERSIONS() view returns(uint256)
func (sTCoordinator *STCoordinator) TryPackMAXOPERATORVERSIONS() ([]byte, error) {
	return sTCoordinator.abi.Pack("MAX_OPERATOR_VERSIONS")
}

// UnpackMAXOPERATORVERSIONS is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x98f7f625.
//
// Solidity: function MAX_OPERATOR_VERSIONS() view returns(uint256)
func (sTCoordinator *STCoordinator) UnpackMAXOPERATORVERSIONS(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("MAX_OPERATOR_VERSIONS", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackMAXPOLICYVERSIONS is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa2135e34.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function MAX_POLICY_VERSIONS() view returns(uint256)
func (sTCoordinator *STCoordinator) PackMAXPOLICYVERSIONS() []byte {
	enc, err := sTCoordinator.abi.Pack("MAX_POLICY_VERSIONS")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackMAXPOLICYVERSIONS is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa2135e34.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function MAX_POLICY_VERSIONS() view returns(uint256)
func (sTCoordinator *STCoordinator) TryPackMAXPOLICYVERSIONS() ([]byte, error) {
	return sTCoordinator.abi.Pack("MAX_POLICY_VERSIONS")
}

// UnpackMAXPOLICYVERSIONS is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xa2135e34.
//
// Solidity: function MAX_POLICY_VERSIONS() view returns(uint256)
func (sTCoordinator *STCoordinator) UnpackMAXPOLICYVERSIONS(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("MAX_POLICY_VERSIONS", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackUPGRADEINTERFACEVERSION is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xad3cb1cc.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (sTCoordinator *STCoordinator) PackUPGRADEINTERFACEVERSION() []byte {
	enc, err := sTCoordinator.abi.Pack("UPGRADE_INTERFACE_VERSION")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackUPGRADEINTERFACEVERSION is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xad3cb1cc.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (sTCoordinator *STCoordinator) TryPackUPGRADEINTERFACEVERSION() ([]byte, error) {
	return sTCoordinator.abi.Pack("UPGRADE_INTERFACE_VERSION")
}

// UnpackUPGRADEINTERFACEVERSION is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xad3cb1cc.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (sTCoordinator *STCoordinator) UnpackUPGRADEINTERFACEVERSION(data []byte) (string, error) {
	out, err := sTCoordinator.abi.Unpack("UPGRADE_INTERFACE_VERSION", data)
	if err != nil {
		return *new(string), err
	}
	out0 := *abi.ConvertType(out[0], new(string)).(*string)
	return out0, nil
}

// PackActiveCommitmentOracle is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x06d902e9.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function activeCommitmentOracle() view returns(address)
func (sTCoordinator *STCoordinator) PackActiveCommitmentOracle() []byte {
	enc, err := sTCoordinator.abi.Pack("activeCommitmentOracle")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackActiveCommitmentOracle is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x06d902e9.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function activeCommitmentOracle() view returns(address)
func (sTCoordinator *STCoordinator) TryPackActiveCommitmentOracle() ([]byte, error) {
	return sTCoordinator.abi.Pack("activeCommitmentOracle")
}

// UnpackActiveCommitmentOracle is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x06d902e9.
//
// Solidity: function activeCommitmentOracle() view returns(address)
func (sTCoordinator *STCoordinator) UnpackActiveCommitmentOracle(data []byte) (common.Address, error) {
	out, err := sTCoordinator.abi.Unpack("activeCommitmentOracle", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackActiveGuardian is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4174b110.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function activeGuardian() view returns(address)
func (sTCoordinator *STCoordinator) PackActiveGuardian() []byte {
	enc, err := sTCoordinator.abi.Pack("activeGuardian")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackActiveGuardian is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4174b110.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function activeGuardian() view returns(address)
func (sTCoordinator *STCoordinator) TryPackActiveGuardian() ([]byte, error) {
	return sTCoordinator.abi.Pack("activeGuardian")
}

// UnpackActiveGuardian is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x4174b110.
//
// Solidity: function activeGuardian() view returns(address)
func (sTCoordinator *STCoordinator) UnpackActiveGuardian(data []byte) (common.Address, error) {
	out, err := sTCoordinator.abi.Unpack("activeGuardian", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackAddConviction is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x19a27e22.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function addConviction(uint256 noId, uint256 amount, uint256 nonce, uint64 deadlineBlock) returns()
func (sTCoordinator *STCoordinator) PackAddConviction(noId *big.Int, amount *big.Int, nonce *big.Int, deadlineBlock uint64) []byte {
	enc, err := sTCoordinator.abi.Pack("addConviction", noId, amount, nonce, deadlineBlock)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackAddConviction is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x19a27e22.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function addConviction(uint256 noId, uint256 amount, uint256 nonce, uint64 deadlineBlock) returns()
func (sTCoordinator *STCoordinator) TryPackAddConviction(noId *big.Int, amount *big.Int, nonce *big.Int, deadlineBlock uint64) ([]byte, error) {
	return sTCoordinator.abi.Pack("addConviction", noId, amount, nonce, deadlineBlock)
}

// PackBindFleetMember is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xd10781fa.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function bindFleetMember((uint64,uint16,address,bytes32,bytes32,bytes16,bytes32,uint64,uint64,uint64,bytes32) binding, bytes clientSignature, bytes hotkeySignature) returns(uint16 uid)
func (sTCoordinator *STCoordinator) PackBindFleetMember(binding STCoordinatorFleetBinding, clientSignature []byte, hotkeySignature []byte) []byte {
	enc, err := sTCoordinator.abi.Pack("bindFleetMember", binding, clientSignature, hotkeySignature)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackBindFleetMember is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xd10781fa.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function bindFleetMember((uint64,uint16,address,bytes32,bytes32,bytes16,bytes32,uint64,uint64,uint64,bytes32) binding, bytes clientSignature, bytes hotkeySignature) returns(uint16 uid)
func (sTCoordinator *STCoordinator) TryPackBindFleetMember(binding STCoordinatorFleetBinding, clientSignature []byte, hotkeySignature []byte) ([]byte, error) {
	return sTCoordinator.abi.Pack("bindFleetMember", binding, clientSignature, hotkeySignature)
}

// UnpackBindFleetMember is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xd10781fa.
//
// Solidity: function bindFleetMember((uint64,uint16,address,bytes32,bytes32,bytes16,bytes32,uint64,uint64,uint64,bytes32) binding, bytes clientSignature, bytes hotkeySignature) returns(uint16 uid)
func (sTCoordinator *STCoordinator) UnpackBindFleetMember(data []byte) (uint16, error) {
	out, err := sTCoordinator.abi.Unpack("bindFleetMember", data)
	if err != nil {
		return *new(uint16), err
	}
	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)
	return out0, nil
}

// PackBindingAt is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf1e9325b.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function bindingAt(bytes16 clientId, uint256 epoch_) view returns(bool active, (bytes32,bytes32,bytes32,bytes32,uint64,uint64,uint64,uint64,uint16,bool) record)
func (sTCoordinator *STCoordinator) PackBindingAt(clientId [16]byte, epoch *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("bindingAt", clientId, epoch)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackBindingAt is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf1e9325b.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function bindingAt(bytes16 clientId, uint256 epoch_) view returns(bool active, (bytes32,bytes32,bytes32,bytes32,uint64,uint64,uint64,uint64,uint16,bool) record)
func (sTCoordinator *STCoordinator) TryPackBindingAt(clientId [16]byte, epoch *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("bindingAt", clientId, epoch)
}

// BindingAtOutput serves as a container for the return parameters of contract
// method BindingAt.
type BindingAtOutput struct {
	Active bool
	Record STCoordinatorBindingRecord
}

// UnpackBindingAt is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xf1e9325b.
//
// Solidity: function bindingAt(bytes16 clientId, uint256 epoch_) view returns(bool active, (bytes32,bytes32,bytes32,bytes32,uint64,uint64,uint64,uint64,uint16,bool) record)
func (sTCoordinator *STCoordinator) UnpackBindingAt(data []byte) (BindingAtOutput, error) {
	out, err := sTCoordinator.abi.Unpack("bindingAt", data)
	outstruct := new(BindingAtOutput)
	if err != nil {
		return *outstruct, err
	}
	outstruct.Active = *abi.ConvertType(out[0], new(bool)).(*bool)
	outstruct.Record = *abi.ConvertType(out[1], new(STCoordinatorBindingRecord)).(*STCoordinatorBindingRecord)
	return *outstruct, nil
}

// PackBindingVersionAt is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x12526047.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function bindingVersionAt(bytes16 clientId, uint256 index) view returns((bytes32,bytes32,bytes32,bytes32,uint64,uint64,uint64,uint64,uint16,bool))
func (sTCoordinator *STCoordinator) PackBindingVersionAt(clientId [16]byte, index *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("bindingVersionAt", clientId, index)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackBindingVersionAt is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x12526047.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function bindingVersionAt(bytes16 clientId, uint256 index) view returns((bytes32,bytes32,bytes32,bytes32,uint64,uint64,uint64,uint64,uint16,bool))
func (sTCoordinator *STCoordinator) TryPackBindingVersionAt(clientId [16]byte, index *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("bindingVersionAt", clientId, index)
}

// UnpackBindingVersionAt is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x12526047.
//
// Solidity: function bindingVersionAt(bytes16 clientId, uint256 index) view returns((bytes32,bytes32,bytes32,bytes32,uint64,uint64,uint64,uint64,uint16,bool))
func (sTCoordinator *STCoordinator) UnpackBindingVersionAt(data []byte) (STCoordinatorBindingRecord, error) {
	out, err := sTCoordinator.abi.Unpack("bindingVersionAt", data)
	if err != nil {
		return *new(STCoordinatorBindingRecord), err
	}
	out0 := *abi.ConvertType(out[0], new(STCoordinatorBindingRecord)).(*STCoordinatorBindingRecord)
	return out0, nil
}

// PackBindingVersionCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe64d30e9.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function bindingVersionCount(bytes16 clientId) view returns(uint256)
func (sTCoordinator *STCoordinator) PackBindingVersionCount(clientId [16]byte) []byte {
	enc, err := sTCoordinator.abi.Pack("bindingVersionCount", clientId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackBindingVersionCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe64d30e9.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function bindingVersionCount(bytes16 clientId) view returns(uint256)
func (sTCoordinator *STCoordinator) TryPackBindingVersionCount(clientId [16]byte) ([]byte, error) {
	return sTCoordinator.abi.Pack("bindingVersionCount", clientId)
}

// UnpackBindingVersionCount is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xe64d30e9.
//
// Solidity: function bindingVersionCount(bytes16 clientId) view returns(uint256)
func (sTCoordinator *STCoordinator) UnpackBindingVersionCount(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("bindingVersionCount", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackCampaignReserved is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x9bbd35c7.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function campaignReserved() view returns(uint256)
func (sTCoordinator *STCoordinator) PackCampaignReserved() []byte {
	enc, err := sTCoordinator.abi.Pack("campaignReserved")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCampaignReserved is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x9bbd35c7.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function campaignReserved() view returns(uint256)
func (sTCoordinator *STCoordinator) TryPackCampaignReserved() ([]byte, error) {
	return sTCoordinator.abi.Pack("campaignReserved")
}

// UnpackCampaignReserved is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x9bbd35c7.
//
// Solidity: function campaignReserved() view returns(uint256)
func (sTCoordinator *STCoordinator) UnpackCampaignReserved(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("campaignReserved", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackCleanupFleetBinding is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x698ad256.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function cleanupFleetBinding(bytes16 clientId) returns()
func (sTCoordinator *STCoordinator) PackCleanupFleetBinding(clientId [16]byte) []byte {
	enc, err := sTCoordinator.abi.Pack("cleanupFleetBinding", clientId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCleanupFleetBinding is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x698ad256.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function cleanupFleetBinding(bytes16 clientId) returns()
func (sTCoordinator *STCoordinator) TryPackCleanupFleetBinding(clientId [16]byte) ([]byte, error) {
	return sTCoordinator.abi.Pack("cleanupFleetBinding", clientId)
}

// PackCloseOperatorEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xbb2126c9.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function closeOperatorEpoch(uint256 epoch_, uint256 noId) returns(uint256 amount)
func (sTCoordinator *STCoordinator) PackCloseOperatorEpoch(epoch *big.Int, noId *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("closeOperatorEpoch", epoch, noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCloseOperatorEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xbb2126c9.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function closeOperatorEpoch(uint256 epoch_, uint256 noId) returns(uint256 amount)
func (sTCoordinator *STCoordinator) TryPackCloseOperatorEpoch(epoch *big.Int, noId *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("closeOperatorEpoch", epoch, noId)
}

// UnpackCloseOperatorEpoch is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xbb2126c9.
//
// Solidity: function closeOperatorEpoch(uint256 epoch_, uint256 noId) returns(uint256 amount)
func (sTCoordinator *STCoordinator) UnpackCloseOperatorEpoch(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("closeOperatorEpoch", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackCommitOperatorRoot is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa721bb1f.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function commitOperatorRoot(uint256 epoch_, uint256 noId, bytes32 payoutRoot, bytes32 artifactHash) returns()
func (sTCoordinator *STCoordinator) PackCommitOperatorRoot(epoch *big.Int, noId *big.Int, payoutRoot [32]byte, artifactHash [32]byte) []byte {
	enc, err := sTCoordinator.abi.Pack("commitOperatorRoot", epoch, noId, payoutRoot, artifactHash)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCommitOperatorRoot is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa721bb1f.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function commitOperatorRoot(uint256 epoch_, uint256 noId, bytes32 payoutRoot, bytes32 artifactHash) returns()
func (sTCoordinator *STCoordinator) TryPackCommitOperatorRoot(epoch *big.Int, noId *big.Int, payoutRoot [32]byte, artifactHash [32]byte) ([]byte, error) {
	return sTCoordinator.abi.Pack("commitOperatorRoot", epoch, noId, payoutRoot, artifactHash)
}

// PackCommitmentOracle is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x7eb72645.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function commitmentOracle() view returns(address)
func (sTCoordinator *STCoordinator) PackCommitmentOracle() []byte {
	enc, err := sTCoordinator.abi.Pack("commitmentOracle")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCommitmentOracle is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x7eb72645.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function commitmentOracle() view returns(address)
func (sTCoordinator *STCoordinator) TryPackCommitmentOracle() ([]byte, error) {
	return sTCoordinator.abi.Pack("commitmentOracle")
}

// UnpackCommitmentOracle is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x7eb72645.
//
// Solidity: function commitmentOracle() view returns(address)
func (sTCoordinator *STCoordinator) UnpackCommitmentOracle(data []byte) (common.Address, error) {
	out, err := sTCoordinator.abi.Unpack("commitmentOracle", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackCumulativeConviction is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xd76955f4.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function cumulativeConviction(uint256 noId) view returns(uint256 amount)
func (sTCoordinator *STCoordinator) PackCumulativeConviction(noId *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("cumulativeConviction", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCumulativeConviction is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xd76955f4.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function cumulativeConviction(uint256 noId) view returns(uint256 amount)
func (sTCoordinator *STCoordinator) TryPackCumulativeConviction(noId *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("cumulativeConviction", noId)
}

// UnpackCumulativeConviction is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xd76955f4.
//
// Solidity: function cumulativeConviction(uint256 noId) view returns(uint256 amount)
func (sTCoordinator *STCoordinator) UnpackCumulativeConviction(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("cumulativeConviction", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackCurrentEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x76671808.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function currentEpoch() view returns(uint256)
func (sTCoordinator *STCoordinator) PackCurrentEpoch() []byte {
	enc, err := sTCoordinator.abi.Pack("currentEpoch")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCurrentEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x76671808.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function currentEpoch() view returns(uint256)
func (sTCoordinator *STCoordinator) TryPackCurrentEpoch() ([]byte, error) {
	return sTCoordinator.abi.Pack("currentEpoch")
}

// UnpackCurrentEpoch is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x76671808.
//
// Solidity: function currentEpoch() view returns(uint256)
func (sTCoordinator *STCoordinator) UnpackCurrentEpoch(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("currentEpoch", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackDeferMissedEmission is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x47068f2d.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function deferMissedEmission(uint256 epoch_, uint256 noId) returns()
func (sTCoordinator *STCoordinator) PackDeferMissedEmission(epoch *big.Int, noId *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("deferMissedEmission", epoch, noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackDeferMissedEmission is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x47068f2d.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function deferMissedEmission(uint256 epoch_, uint256 noId) returns()
func (sTCoordinator *STCoordinator) TryPackDeferMissedEmission(epoch *big.Int, noId *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("deferMissedEmission", epoch, noId)
}

// PackDeposit is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4b785efe.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function deposit(uint256 noId, uint256 amount, uint256 nonce, uint64 deadlineBlock) returns()
func (sTCoordinator *STCoordinator) PackDeposit(noId *big.Int, amount *big.Int, nonce *big.Int, deadlineBlock uint64) []byte {
	enc, err := sTCoordinator.abi.Pack("deposit", noId, amount, nonce, deadlineBlock)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackDeposit is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4b785efe.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function deposit(uint256 noId, uint256 amount, uint256 nonce, uint64 deadlineBlock) returns()
func (sTCoordinator *STCoordinator) TryPackDeposit(noId *big.Int, amount *big.Int, nonce *big.Int, deadlineBlock uint64) ([]byte, error) {
	return sTCoordinator.abi.Pack("deposit", noId, amount, nonce, deadlineBlock)
}

// PackDepositHotkeyUsed is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x6692f43f.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function depositHotkeyUsed(bytes32 hotkey) view returns(bool used)
func (sTCoordinator *STCoordinator) PackDepositHotkeyUsed(hotkey [32]byte) []byte {
	enc, err := sTCoordinator.abi.Pack("depositHotkeyUsed", hotkey)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackDepositHotkeyUsed is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x6692f43f.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function depositHotkeyUsed(bytes32 hotkey) view returns(bool used)
func (sTCoordinator *STCoordinator) TryPackDepositHotkeyUsed(hotkey [32]byte) ([]byte, error) {
	return sTCoordinator.abi.Pack("depositHotkeyUsed", hotkey)
}

// UnpackDepositHotkeyUsed is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x6692f43f.
//
// Solidity: function depositHotkeyUsed(bytes32 hotkey) view returns(bool used)
func (sTCoordinator *STCoordinator) UnpackDepositHotkeyUsed(data []byte) (bool, error) {
	out, err := sTCoordinator.abi.Unpack("depositHotkeyUsed", data)
	if err != nil {
		return *new(bool), err
	}
	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)
	return out0, nil
}

// PackEpochConvictionAdded is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xd0f68732.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function epochConvictionAdded(uint256 epoch, uint256 noId) view returns(uint256 amount)
func (sTCoordinator *STCoordinator) PackEpochConvictionAdded(epoch *big.Int, noId *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("epochConvictionAdded", epoch, noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackEpochConvictionAdded is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xd0f68732.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function epochConvictionAdded(uint256 epoch, uint256 noId) view returns(uint256 amount)
func (sTCoordinator *STCoordinator) TryPackEpochConvictionAdded(epoch *big.Int, noId *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("epochConvictionAdded", epoch, noId)
}

// UnpackEpochConvictionAdded is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xd0f68732.
//
// Solidity: function epochConvictionAdded(uint256 epoch, uint256 noId) view returns(uint256 amount)
func (sTCoordinator *STCoordinator) UnpackEpochConvictionAdded(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("epochConvictionAdded", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackEpochDeposits is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x928147a8.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function epochDeposits(uint256 epoch, uint256 noId) view returns(uint256 amount)
func (sTCoordinator *STCoordinator) PackEpochDeposits(epoch *big.Int, noId *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("epochDeposits", epoch, noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackEpochDeposits is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x928147a8.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function epochDeposits(uint256 epoch, uint256 noId) view returns(uint256 amount)
func (sTCoordinator *STCoordinator) TryPackEpochDeposits(epoch *big.Int, noId *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("epochDeposits", epoch, noId)
}

// UnpackEpochDeposits is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x928147a8.
//
// Solidity: function epochDeposits(uint256 epoch, uint256 noId) view returns(uint256 amount)
func (sTCoordinator *STCoordinator) UnpackEpochDeposits(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("epochDeposits", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackEpochEndBlock is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc5419226.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function epochEndBlock(uint256 epoch_) view returns(uint256)
func (sTCoordinator *STCoordinator) PackEpochEndBlock(epoch *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("epochEndBlock", epoch)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackEpochEndBlock is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc5419226.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function epochEndBlock(uint256 epoch_) view returns(uint256)
func (sTCoordinator *STCoordinator) TryPackEpochEndBlock(epoch *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("epochEndBlock", epoch)
}

// UnpackEpochEndBlock is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xc5419226.
//
// Solidity: function epochEndBlock(uint256 epoch_) view returns(uint256)
func (sTCoordinator *STCoordinator) UnpackEpochEndBlock(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("epochEndBlock", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackEpochStartBlock is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x0cea2e35.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function epochStartBlock(uint256 epoch_) view returns(uint256)
func (sTCoordinator *STCoordinator) PackEpochStartBlock(epoch *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("epochStartBlock", epoch)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackEpochStartBlock is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x0cea2e35.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function epochStartBlock(uint256 epoch_) view returns(uint256)
func (sTCoordinator *STCoordinator) TryPackEpochStartBlock(epoch *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("epochStartBlock", epoch)
}

// UnpackEpochStartBlock is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x0cea2e35.
//
// Solidity: function epochStartBlock(uint256 epoch_) view returns(uint256)
func (sTCoordinator *STCoordinator) UnpackEpochStartBlock(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("epochStartBlock", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackFinalizeOperatorEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x2bfea3f9.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function finalizeOperatorEpoch(uint256 epoch_, uint256 noId) returns()
func (sTCoordinator *STCoordinator) PackFinalizeOperatorEpoch(epoch *big.Int, noId *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("finalizeOperatorEpoch", epoch, noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackFinalizeOperatorEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x2bfea3f9.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function finalizeOperatorEpoch(uint256 epoch_, uint256 noId) returns()
func (sTCoordinator *STCoordinator) TryPackFinalizeOperatorEpoch(epoch *big.Int, noId *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("finalizeOperatorEpoch", epoch, noId)
}

// PackFleetBindingDigest is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x88aa1b38.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function fleetBindingDigest((uint64,uint16,address,bytes32,bytes32,bytes16,bytes32,uint64,uint64,uint64,bytes32) binding) pure returns(bytes32)
func (sTCoordinator *STCoordinator) PackFleetBindingDigest(binding STCoordinatorFleetBinding) []byte {
	enc, err := sTCoordinator.abi.Pack("fleetBindingDigest", binding)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackFleetBindingDigest is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x88aa1b38.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function fleetBindingDigest((uint64,uint16,address,bytes32,bytes32,bytes16,bytes32,uint64,uint64,uint64,bytes32) binding) pure returns(bytes32)
func (sTCoordinator *STCoordinator) TryPackFleetBindingDigest(binding STCoordinatorFleetBinding) ([]byte, error) {
	return sTCoordinator.abi.Pack("fleetBindingDigest", binding)
}

// UnpackFleetBindingDigest is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x88aa1b38.
//
// Solidity: function fleetBindingDigest((uint64,uint16,address,bytes32,bytes32,bytes16,bytes32,uint64,uint64,uint64,bytes32) binding) pure returns(bytes32)
func (sTCoordinator *STCoordinator) UnpackFleetBindingDigest(data []byte) ([32]byte, error) {
	out, err := sTCoordinator.abi.Unpack("fleetBindingDigest", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackFleetMemberCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x74f6d671.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function fleetMemberCount(bytes32 fleetId) view returns(uint256 members)
func (sTCoordinator *STCoordinator) PackFleetMemberCount(fleetId [32]byte) []byte {
	enc, err := sTCoordinator.abi.Pack("fleetMemberCount", fleetId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackFleetMemberCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x74f6d671.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function fleetMemberCount(bytes32 fleetId) view returns(uint256 members)
func (sTCoordinator *STCoordinator) TryPackFleetMemberCount(fleetId [32]byte) ([]byte, error) {
	return sTCoordinator.abi.Pack("fleetMemberCount", fleetId)
}

// UnpackFleetMemberCount is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x74f6d671.
//
// Solidity: function fleetMemberCount(bytes32 fleetId) view returns(uint256 members)
func (sTCoordinator *STCoordinator) UnpackFleetMemberCount(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("fleetMemberCount", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackFleetRevokeDigest is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x016aeca7.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function fleetRevokeDigest(bytes16 clientId, uint64 generation, uint64 effectiveEpoch) view returns(bytes32)
func (sTCoordinator *STCoordinator) PackFleetRevokeDigest(clientId [16]byte, generation uint64, effectiveEpoch uint64) []byte {
	enc, err := sTCoordinator.abi.Pack("fleetRevokeDigest", clientId, generation, effectiveEpoch)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackFleetRevokeDigest is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x016aeca7.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function fleetRevokeDigest(bytes16 clientId, uint64 generation, uint64 effectiveEpoch) view returns(bytes32)
func (sTCoordinator *STCoordinator) TryPackFleetRevokeDigest(clientId [16]byte, generation uint64, effectiveEpoch uint64) ([]byte, error) {
	return sTCoordinator.abi.Pack("fleetRevokeDigest", clientId, generation, effectiveEpoch)
}

// UnpackFleetRevokeDigest is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x016aeca7.
//
// Solidity: function fleetRevokeDigest(bytes16 clientId, uint64 generation, uint64 effectiveEpoch) view returns(bytes32)
func (sTCoordinator *STCoordinator) UnpackFleetRevokeDigest(data []byte) ([32]byte, error) {
	out, err := sTCoordinator.abi.Unpack("fleetRevokeDigest", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackGetFleetBinding is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xd46fc4f1.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function getFleetBinding(bytes16 clientId) view returns((bytes32,bytes32,bytes32,bytes32,uint64,uint64,uint64,uint64,uint16,bool))
func (sTCoordinator *STCoordinator) PackGetFleetBinding(clientId [16]byte) []byte {
	enc, err := sTCoordinator.abi.Pack("getFleetBinding", clientId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackGetFleetBinding is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xd46fc4f1.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function getFleetBinding(bytes16 clientId) view returns((bytes32,bytes32,bytes32,bytes32,uint64,uint64,uint64,uint64,uint16,bool))
func (sTCoordinator *STCoordinator) TryPackGetFleetBinding(clientId [16]byte) ([]byte, error) {
	return sTCoordinator.abi.Pack("getFleetBinding", clientId)
}

// UnpackGetFleetBinding is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xd46fc4f1.
//
// Solidity: function getFleetBinding(bytes16 clientId) view returns((bytes32,bytes32,bytes32,bytes32,uint64,uint64,uint64,uint64,uint16,bool))
func (sTCoordinator *STCoordinator) UnpackGetFleetBinding(data []byte) (STCoordinatorBindingRecord, error) {
	out, err := sTCoordinator.abi.Unpack("getFleetBinding", data)
	if err != nil {
		return *new(STCoordinatorBindingRecord), err
	}
	out0 := *abi.ConvertType(out[0], new(STCoordinatorBindingRecord)).(*STCoordinatorBindingRecord)
	return out0, nil
}

// PackGuardian is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x452a9320.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function guardian() view returns(address)
func (sTCoordinator *STCoordinator) PackGuardian() []byte {
	enc, err := sTCoordinator.abi.Pack("guardian")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackGuardian is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x452a9320.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function guardian() view returns(address)
func (sTCoordinator *STCoordinator) TryPackGuardian() ([]byte, error) {
	return sTCoordinator.abi.Pack("guardian")
}

// UnpackGuardian is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x452a9320.
//
// Solidity: function guardian() view returns(address)
func (sTCoordinator *STCoordinator) UnpackGuardian(data []byte) (common.Address, error) {
	out, err := sTCoordinator.abi.Unpack("guardian", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackInitialize is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xea6499eb.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function initialize(uint16 netuid_, address owner_, address guardian_, bytes32 selfColdkey_, address settlementVault_, address reserveSink_, address commitmentOracle_, (bytes32,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint256,uint256) initialPolicy) returns()
func (sTCoordinator *STCoordinator) PackInitialize(netuid uint16, owner common.Address, guardian common.Address, selfColdkey [32]byte, settlementVault common.Address, reserveSink common.Address, commitmentOracle common.Address, initialPolicy STCoordinatorPolicySnapshot) []byte {
	enc, err := sTCoordinator.abi.Pack("initialize", netuid, owner, guardian, selfColdkey, settlementVault, reserveSink, commitmentOracle, initialPolicy)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackInitialize is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xea6499eb.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function initialize(uint16 netuid_, address owner_, address guardian_, bytes32 selfColdkey_, address settlementVault_, address reserveSink_, address commitmentOracle_, (bytes32,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint256,uint256) initialPolicy) returns()
func (sTCoordinator *STCoordinator) TryPackInitialize(netuid uint16, owner common.Address, guardian common.Address, selfColdkey [32]byte, settlementVault common.Address, reserveSink common.Address, commitmentOracle common.Address, initialPolicy STCoordinatorPolicySnapshot) ([]byte, error) {
	return sTCoordinator.abi.Pack("initialize", netuid, owner, guardian, selfColdkey, settlementVault, reserveSink, commitmentOracle, initialPolicy)
}

// PackMirrorCommitment is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x6be27484.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function mirrorCommitment(bytes32 hotkey, bytes32 commitmentHash, uint64 finalizedBlock, bytes32 finalizedBlockHash) returns()
func (sTCoordinator *STCoordinator) PackMirrorCommitment(hotkey [32]byte, commitmentHash [32]byte, finalizedBlock uint64, finalizedBlockHash [32]byte) []byte {
	enc, err := sTCoordinator.abi.Pack("mirrorCommitment", hotkey, commitmentHash, finalizedBlock, finalizedBlockHash)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackMirrorCommitment is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x6be27484.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function mirrorCommitment(bytes32 hotkey, bytes32 commitmentHash, uint64 finalizedBlock, bytes32 finalizedBlockHash) returns()
func (sTCoordinator *STCoordinator) TryPackMirrorCommitment(hotkey [32]byte, commitmentHash [32]byte, finalizedBlock uint64, finalizedBlockHash [32]byte) ([]byte, error) {
	return sTCoordinator.abi.Pack("mirrorCommitment", hotkey, commitmentHash, finalizedBlock, finalizedBlockHash)
}

// PackMirroredCommitments is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf7c4fb1f.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function mirroredCommitments(bytes32 hotkey) view returns(bytes32 commitmentHash, bytes32 finalizedBlockHash, uint64 finalizedBlock)
func (sTCoordinator *STCoordinator) PackMirroredCommitments(hotkey [32]byte) []byte {
	enc, err := sTCoordinator.abi.Pack("mirroredCommitments", hotkey)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackMirroredCommitments is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf7c4fb1f.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function mirroredCommitments(bytes32 hotkey) view returns(bytes32 commitmentHash, bytes32 finalizedBlockHash, uint64 finalizedBlock)
func (sTCoordinator *STCoordinator) TryPackMirroredCommitments(hotkey [32]byte) ([]byte, error) {
	return sTCoordinator.abi.Pack("mirroredCommitments", hotkey)
}

// MirroredCommitmentsOutput serves as a container for the return parameters of contract
// method MirroredCommitments.
type MirroredCommitmentsOutput struct {
	CommitmentHash     [32]byte
	FinalizedBlockHash [32]byte
	FinalizedBlock     uint64
}

// UnpackMirroredCommitments is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xf7c4fb1f.
//
// Solidity: function mirroredCommitments(bytes32 hotkey) view returns(bytes32 commitmentHash, bytes32 finalizedBlockHash, uint64 finalizedBlock)
func (sTCoordinator *STCoordinator) UnpackMirroredCommitments(data []byte) (MirroredCommitmentsOutput, error) {
	out, err := sTCoordinator.abi.Unpack("mirroredCommitments", data)
	outstruct := new(MirroredCommitmentsOutput)
	if err != nil {
		return *outstruct, err
	}
	outstruct.CommitmentHash = *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	outstruct.FinalizedBlockHash = *abi.ConvertType(out[1], new([32]byte)).(*[32]byte)
	outstruct.FinalizedBlock = *abi.ConvertType(out[2], new(uint64)).(*uint64)
	return *outstruct, nil
}

// PackNetuid is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe78015b1.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function netuid() view returns(uint16)
func (sTCoordinator *STCoordinator) PackNetuid() []byte {
	enc, err := sTCoordinator.abi.Pack("netuid")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackNetuid is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe78015b1.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function netuid() view returns(uint16)
func (sTCoordinator *STCoordinator) TryPackNetuid() ([]byte, error) {
	return sTCoordinator.abi.Pack("netuid")
}

// UnpackNetuid is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xe78015b1.
//
// Solidity: function netuid() view returns(uint16)
func (sTCoordinator *STCoordinator) UnpackNetuid(data []byte) (uint16, error) {
	out, err := sTCoordinator.abi.Unpack("netuid", data)
	if err != nil {
		return *new(uint16), err
	}
	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)
	return out0, nil
}

// PackNextDepositNonce is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x491d94ac.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function nextDepositNonce(uint256 noId) view returns(uint256 nonce)
func (sTCoordinator *STCoordinator) PackNextDepositNonce(noId *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("nextDepositNonce", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackNextDepositNonce is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x491d94ac.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function nextDepositNonce(uint256 noId) view returns(uint256 nonce)
func (sTCoordinator *STCoordinator) TryPackNextDepositNonce(noId *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("nextDepositNonce", noId)
}

// UnpackNextDepositNonce is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x491d94ac.
//
// Solidity: function nextDepositNonce(uint256 noId) view returns(uint256 nonce)
func (sTCoordinator *STCoordinator) UnpackNextDepositNonce(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("nextDepositNonce", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackOperatorAt is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xdea85b7b.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function operatorAt(uint256 noId, uint256 epoch_) view returns((bytes32,bytes32,bytes32,address,address,uint64,bool))
func (sTCoordinator *STCoordinator) PackOperatorAt(noId *big.Int, epoch *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("operatorAt", noId, epoch)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackOperatorAt is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xdea85b7b.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function operatorAt(uint256 noId, uint256 epoch_) view returns((bytes32,bytes32,bytes32,address,address,uint64,bool))
func (sTCoordinator *STCoordinator) TryPackOperatorAt(noId *big.Int, epoch *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("operatorAt", noId, epoch)
}

// UnpackOperatorAt is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xdea85b7b.
//
// Solidity: function operatorAt(uint256 noId, uint256 epoch_) view returns((bytes32,bytes32,bytes32,address,address,uint64,bool))
func (sTCoordinator *STCoordinator) UnpackOperatorAt(data []byte) (STCoordinatorOperatorVersion, error) {
	out, err := sTCoordinator.abi.Unpack("operatorAt", data)
	if err != nil {
		return *new(STCoordinatorOperatorVersion), err
	}
	out0 := *abi.ConvertType(out[0], new(STCoordinatorOperatorVersion)).(*STCoordinatorOperatorVersion)
	return out0, nil
}

// PackOperatorCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x7c6f3158.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function operatorCount() view returns(uint256)
func (sTCoordinator *STCoordinator) PackOperatorCount() []byte {
	enc, err := sTCoordinator.abi.Pack("operatorCount")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackOperatorCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x7c6f3158.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function operatorCount() view returns(uint256)
func (sTCoordinator *STCoordinator) TryPackOperatorCount() ([]byte, error) {
	return sTCoordinator.abi.Pack("operatorCount")
}

// UnpackOperatorCount is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x7c6f3158.
//
// Solidity: function operatorCount() view returns(uint256)
func (sTCoordinator *STCoordinator) UnpackOperatorCount(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("operatorCount", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackOperatorIdAt is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xadf1fffa.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function operatorIdAt(uint256 index) view returns(uint256)
func (sTCoordinator *STCoordinator) PackOperatorIdAt(index *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("operatorIdAt", index)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackOperatorIdAt is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xadf1fffa.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function operatorIdAt(uint256 index) view returns(uint256)
func (sTCoordinator *STCoordinator) TryPackOperatorIdAt(index *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("operatorIdAt", index)
}

// UnpackOperatorIdAt is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xadf1fffa.
//
// Solidity: function operatorIdAt(uint256 index) view returns(uint256)
func (sTCoordinator *STCoordinator) UnpackOperatorIdAt(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("operatorIdAt", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackOperatorVersionCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x91ad4c0f.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function operatorVersionCount(uint256 noId) view returns(uint256)
func (sTCoordinator *STCoordinator) PackOperatorVersionCount(noId *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("operatorVersionCount", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackOperatorVersionCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x91ad4c0f.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function operatorVersionCount(uint256 noId) view returns(uint256)
func (sTCoordinator *STCoordinator) TryPackOperatorVersionCount(noId *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("operatorVersionCount", noId)
}

// UnpackOperatorVersionCount is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x91ad4c0f.
//
// Solidity: function operatorVersionCount(uint256 noId) view returns(uint256)
func (sTCoordinator *STCoordinator) UnpackOperatorVersionCount(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("operatorVersionCount", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackOwner is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x8da5cb5b.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function owner() view returns(address)
func (sTCoordinator *STCoordinator) PackOwner() []byte {
	enc, err := sTCoordinator.abi.Pack("owner")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackOwner is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x8da5cb5b.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function owner() view returns(address)
func (sTCoordinator *STCoordinator) TryPackOwner() ([]byte, error) {
	return sTCoordinator.abi.Pack("owner")
}

// UnpackOwner is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (sTCoordinator *STCoordinator) UnpackOwner(data []byte) (common.Address, error) {
	out, err := sTCoordinator.abi.Unpack("owner", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackPaused is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x5c975abb.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function paused() view returns(bool)
func (sTCoordinator *STCoordinator) PackPaused() []byte {
	enc, err := sTCoordinator.abi.Pack("paused")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPaused is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x5c975abb.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function paused() view returns(bool)
func (sTCoordinator *STCoordinator) TryPackPaused() ([]byte, error) {
	return sTCoordinator.abi.Pack("paused")
}

// UnpackPaused is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x5c975abb.
//
// Solidity: function paused() view returns(bool)
func (sTCoordinator *STCoordinator) UnpackPaused(data []byte) (bool, error) {
	out, err := sTCoordinator.abi.Unpack("paused", data)
	if err != nil {
		return *new(bool), err
	}
	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)
	return out0, nil
}

// PackPendingCommitmentOracle is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc2d94e2a.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function pendingCommitmentOracle() view returns(address)
func (sTCoordinator *STCoordinator) PackPendingCommitmentOracle() []byte {
	enc, err := sTCoordinator.abi.Pack("pendingCommitmentOracle")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPendingCommitmentOracle is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc2d94e2a.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function pendingCommitmentOracle() view returns(address)
func (sTCoordinator *STCoordinator) TryPackPendingCommitmentOracle() ([]byte, error) {
	return sTCoordinator.abi.Pack("pendingCommitmentOracle")
}

// UnpackPendingCommitmentOracle is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xc2d94e2a.
//
// Solidity: function pendingCommitmentOracle() view returns(address)
func (sTCoordinator *STCoordinator) UnpackPendingCommitmentOracle(data []byte) (common.Address, error) {
	out, err := sTCoordinator.abi.Unpack("pendingCommitmentOracle", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackPendingCommitmentOracleEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x465c2228.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function pendingCommitmentOracleEpoch() view returns(uint64)
func (sTCoordinator *STCoordinator) PackPendingCommitmentOracleEpoch() []byte {
	enc, err := sTCoordinator.abi.Pack("pendingCommitmentOracleEpoch")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPendingCommitmentOracleEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x465c2228.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function pendingCommitmentOracleEpoch() view returns(uint64)
func (sTCoordinator *STCoordinator) TryPackPendingCommitmentOracleEpoch() ([]byte, error) {
	return sTCoordinator.abi.Pack("pendingCommitmentOracleEpoch")
}

// UnpackPendingCommitmentOracleEpoch is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x465c2228.
//
// Solidity: function pendingCommitmentOracleEpoch() view returns(uint64)
func (sTCoordinator *STCoordinator) UnpackPendingCommitmentOracleEpoch(data []byte) (uint64, error) {
	out, err := sTCoordinator.abi.Unpack("pendingCommitmentOracleEpoch", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackPendingGuardian is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x762c31ba.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function pendingGuardian() view returns(address)
func (sTCoordinator *STCoordinator) PackPendingGuardian() []byte {
	enc, err := sTCoordinator.abi.Pack("pendingGuardian")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPendingGuardian is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x762c31ba.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function pendingGuardian() view returns(address)
func (sTCoordinator *STCoordinator) TryPackPendingGuardian() ([]byte, error) {
	return sTCoordinator.abi.Pack("pendingGuardian")
}

// UnpackPendingGuardian is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x762c31ba.
//
// Solidity: function pendingGuardian() view returns(address)
func (sTCoordinator *STCoordinator) UnpackPendingGuardian(data []byte) (common.Address, error) {
	out, err := sTCoordinator.abi.Unpack("pendingGuardian", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackPendingGuardianEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x1b782155.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function pendingGuardianEpoch() view returns(uint64)
func (sTCoordinator *STCoordinator) PackPendingGuardianEpoch() []byte {
	enc, err := sTCoordinator.abi.Pack("pendingGuardianEpoch")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPendingGuardianEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x1b782155.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function pendingGuardianEpoch() view returns(uint64)
func (sTCoordinator *STCoordinator) TryPackPendingGuardianEpoch() ([]byte, error) {
	return sTCoordinator.abi.Pack("pendingGuardianEpoch")
}

// UnpackPendingGuardianEpoch is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x1b782155.
//
// Solidity: function pendingGuardianEpoch() view returns(uint64)
func (sTCoordinator *STCoordinator) UnpackPendingGuardianEpoch(data []byte) (uint64, error) {
	out, err := sTCoordinator.abi.Unpack("pendingGuardianEpoch", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackPolicyAt is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xb9a7f076.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function policyAt(uint256 epoch_) view returns((bytes32,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint256,uint256))
func (sTCoordinator *STCoordinator) PackPolicyAt(epoch *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("policyAt", epoch)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPolicyAt is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xb9a7f076.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function policyAt(uint256 epoch_) view returns((bytes32,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint256,uint256))
func (sTCoordinator *STCoordinator) TryPackPolicyAt(epoch *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("policyAt", epoch)
}

// UnpackPolicyAt is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xb9a7f076.
//
// Solidity: function policyAt(uint256 epoch_) view returns((bytes32,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint256,uint256))
func (sTCoordinator *STCoordinator) UnpackPolicyAt(data []byte) (STCoordinatorPolicySnapshot, error) {
	out, err := sTCoordinator.abi.Unpack("policyAt", data)
	if err != nil {
		return *new(STCoordinatorPolicySnapshot), err
	}
	out0 := *abi.ConvertType(out[0], new(STCoordinatorPolicySnapshot)).(*STCoordinatorPolicySnapshot)
	return out0, nil
}

// PackPolicyByIndex is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x881d774c.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function policyByIndex(uint256 index) view returns((bytes32,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint256,uint256))
func (sTCoordinator *STCoordinator) PackPolicyByIndex(index *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("policyByIndex", index)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPolicyByIndex is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x881d774c.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function policyByIndex(uint256 index) view returns((bytes32,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint256,uint256))
func (sTCoordinator *STCoordinator) TryPackPolicyByIndex(index *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("policyByIndex", index)
}

// UnpackPolicyByIndex is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x881d774c.
//
// Solidity: function policyByIndex(uint256 index) view returns((bytes32,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint256,uint256))
func (sTCoordinator *STCoordinator) UnpackPolicyByIndex(data []byte) (STCoordinatorPolicySnapshot, error) {
	out, err := sTCoordinator.abi.Unpack("policyByIndex", data)
	if err != nil {
		return *new(STCoordinatorPolicySnapshot), err
	}
	out0 := *abi.ConvertType(out[0], new(STCoordinatorPolicySnapshot)).(*STCoordinatorPolicySnapshot)
	return out0, nil
}

// PackPolicyCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xde54d429.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function policyCount() view returns(uint256)
func (sTCoordinator *STCoordinator) PackPolicyCount() []byte {
	enc, err := sTCoordinator.abi.Pack("policyCount")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPolicyCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xde54d429.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function policyCount() view returns(uint256)
func (sTCoordinator *STCoordinator) TryPackPolicyCount() ([]byte, error) {
	return sTCoordinator.abi.Pack("policyCount")
}

// UnpackPolicyCount is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xde54d429.
//
// Solidity: function policyCount() view returns(uint256)
func (sTCoordinator *STCoordinator) UnpackPolicyCount(data []byte) (*big.Int, error) {
	out, err := sTCoordinator.abi.Unpack("policyCount", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackProxiableUUID is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x52d1902d.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (sTCoordinator *STCoordinator) PackProxiableUUID() []byte {
	enc, err := sTCoordinator.abi.Pack("proxiableUUID")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackProxiableUUID is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x52d1902d.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (sTCoordinator *STCoordinator) TryPackProxiableUUID() ([]byte, error) {
	return sTCoordinator.abi.Pack("proxiableUUID")
}

// UnpackProxiableUUID is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (sTCoordinator *STCoordinator) UnpackProxiableUUID(data []byte) ([32]byte, error) {
	out, err := sTCoordinator.abi.Unpack("proxiableUUID", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackRegisterOperator is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xd2d69a77.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function registerOperator(uint256 noId, bytes32 coldkey, bytes32 poolHotkey, bytes32 depositHotkey, address depositSigner, address rootSigner, uint64 effectiveEpoch, uint64 maximumBurnRao) payable returns(uint16 uid)
func (sTCoordinator *STCoordinator) PackRegisterOperator(noId *big.Int, coldkey [32]byte, poolHotkey [32]byte, depositHotkey [32]byte, depositSigner common.Address, rootSigner common.Address, effectiveEpoch uint64, maximumBurnRao uint64) []byte {
	enc, err := sTCoordinator.abi.Pack("registerOperator", noId, coldkey, poolHotkey, depositHotkey, depositSigner, rootSigner, effectiveEpoch, maximumBurnRao)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRegisterOperator is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xd2d69a77.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function registerOperator(uint256 noId, bytes32 coldkey, bytes32 poolHotkey, bytes32 depositHotkey, address depositSigner, address rootSigner, uint64 effectiveEpoch, uint64 maximumBurnRao) payable returns(uint16 uid)
func (sTCoordinator *STCoordinator) TryPackRegisterOperator(noId *big.Int, coldkey [32]byte, poolHotkey [32]byte, depositHotkey [32]byte, depositSigner common.Address, rootSigner common.Address, effectiveEpoch uint64, maximumBurnRao uint64) ([]byte, error) {
	return sTCoordinator.abi.Pack("registerOperator", noId, coldkey, poolHotkey, depositHotkey, depositSigner, rootSigner, effectiveEpoch, maximumBurnRao)
}

// UnpackRegisterOperator is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xd2d69a77.
//
// Solidity: function registerOperator(uint256 noId, bytes32 coldkey, bytes32 poolHotkey, bytes32 depositHotkey, address depositSigner, address rootSigner, uint64 effectiveEpoch, uint64 maximumBurnRao) payable returns(uint16 uid)
func (sTCoordinator *STCoordinator) UnpackRegisterOperator(data []byte) (uint16, error) {
	out, err := sTCoordinator.abi.Unpack("registerOperator", data)
	if err != nil {
		return *new(uint16), err
	}
	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)
	return out0, nil
}

// PackRenounceOwnership is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x715018a6.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function renounceOwnership() returns()
func (sTCoordinator *STCoordinator) PackRenounceOwnership() []byte {
	enc, err := sTCoordinator.abi.Pack("renounceOwnership")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRenounceOwnership is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x715018a6.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function renounceOwnership() returns()
func (sTCoordinator *STCoordinator) TryPackRenounceOwnership() ([]byte, error) {
	return sTCoordinator.abi.Pack("renounceOwnership")
}

// PackReserveSink is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x95e89219.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function reserveSink() view returns(address)
func (sTCoordinator *STCoordinator) PackReserveSink() []byte {
	enc, err := sTCoordinator.abi.Pack("reserveSink")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackReserveSink is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x95e89219.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function reserveSink() view returns(address)
func (sTCoordinator *STCoordinator) TryPackReserveSink() ([]byte, error) {
	return sTCoordinator.abi.Pack("reserveSink")
}

// UnpackReserveSink is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x95e89219.
//
// Solidity: function reserveSink() view returns(address)
func (sTCoordinator *STCoordinator) UnpackReserveSink(data []byte) (common.Address, error) {
	out, err := sTCoordinator.abi.Unpack("reserveSink", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackRevokeFleetBinding is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x7df52856.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function revokeFleetBinding(bytes16 clientId, uint64 generation, uint64 effectiveEpoch, bytes clientSignature) returns()
func (sTCoordinator *STCoordinator) PackRevokeFleetBinding(clientId [16]byte, generation uint64, effectiveEpoch uint64, clientSignature []byte) []byte {
	enc, err := sTCoordinator.abi.Pack("revokeFleetBinding", clientId, generation, effectiveEpoch, clientSignature)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRevokeFleetBinding is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x7df52856.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function revokeFleetBinding(bytes16 clientId, uint64 generation, uint64 effectiveEpoch, bytes clientSignature) returns()
func (sTCoordinator *STCoordinator) TryPackRevokeFleetBinding(clientId [16]byte, generation uint64, effectiveEpoch uint64, clientSignature []byte) ([]byte, error) {
	return sTCoordinator.abi.Pack("revokeFleetBinding", clientId, generation, effectiveEpoch, clientSignature)
}

// PackRootCommitments is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x548f3b86.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function rootCommitments(uint256 epoch, uint256 noId) view returns(bytes32 payoutRoot, bytes32 artifactHash, address committer, uint64 commitBlock)
func (sTCoordinator *STCoordinator) PackRootCommitments(epoch *big.Int, noId *big.Int) []byte {
	enc, err := sTCoordinator.abi.Pack("rootCommitments", epoch, noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRootCommitments is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x548f3b86.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function rootCommitments(uint256 epoch, uint256 noId) view returns(bytes32 payoutRoot, bytes32 artifactHash, address committer, uint64 commitBlock)
func (sTCoordinator *STCoordinator) TryPackRootCommitments(epoch *big.Int, noId *big.Int) ([]byte, error) {
	return sTCoordinator.abi.Pack("rootCommitments", epoch, noId)
}

// RootCommitmentsOutput serves as a container for the return parameters of contract
// method RootCommitments.
type RootCommitmentsOutput struct {
	PayoutRoot   [32]byte
	ArtifactHash [32]byte
	Committer    common.Address
	CommitBlock  uint64
}

// UnpackRootCommitments is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x548f3b86.
//
// Solidity: function rootCommitments(uint256 epoch, uint256 noId) view returns(bytes32 payoutRoot, bytes32 artifactHash, address committer, uint64 commitBlock)
func (sTCoordinator *STCoordinator) UnpackRootCommitments(data []byte) (RootCommitmentsOutput, error) {
	out, err := sTCoordinator.abi.Unpack("rootCommitments", data)
	outstruct := new(RootCommitmentsOutput)
	if err != nil {
		return *outstruct, err
	}
	outstruct.PayoutRoot = *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	outstruct.ArtifactHash = *abi.ConvertType(out[1], new([32]byte)).(*[32]byte)
	outstruct.Committer = *abi.ConvertType(out[2], new(common.Address)).(*common.Address)
	outstruct.CommitBlock = *abi.ConvertType(out[3], new(uint64)).(*uint64)
	return *outstruct, nil
}

// PackScheduleCommitmentOracle is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x43e375d9.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function scheduleCommitmentOracle(address oracle, uint64 effectiveEpoch) returns()
func (sTCoordinator *STCoordinator) PackScheduleCommitmentOracle(oracle common.Address, effectiveEpoch uint64) []byte {
	enc, err := sTCoordinator.abi.Pack("scheduleCommitmentOracle", oracle, effectiveEpoch)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackScheduleCommitmentOracle is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x43e375d9.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function scheduleCommitmentOracle(address oracle, uint64 effectiveEpoch) returns()
func (sTCoordinator *STCoordinator) TryPackScheduleCommitmentOracle(oracle common.Address, effectiveEpoch uint64) ([]byte, error) {
	return sTCoordinator.abi.Pack("scheduleCommitmentOracle", oracle, effectiveEpoch)
}

// PackScheduleGuardian is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x82287be8.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function scheduleGuardian(address guardian_, uint64 effectiveEpoch) returns()
func (sTCoordinator *STCoordinator) PackScheduleGuardian(guardian common.Address, effectiveEpoch uint64) []byte {
	enc, err := sTCoordinator.abi.Pack("scheduleGuardian", guardian, effectiveEpoch)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackScheduleGuardian is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x82287be8.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function scheduleGuardian(address guardian_, uint64 effectiveEpoch) returns()
func (sTCoordinator *STCoordinator) TryPackScheduleGuardian(guardian common.Address, effectiveEpoch uint64) ([]byte, error) {
	return sTCoordinator.abi.Pack("scheduleGuardian", guardian, effectiveEpoch)
}

// PackScheduleOperator is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x3d191915.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function scheduleOperator(uint256 noId, bytes32 depositHotkey, address depositSigner, address rootSigner, bool active, uint64 effectiveEpoch) returns()
func (sTCoordinator *STCoordinator) PackScheduleOperator(noId *big.Int, depositHotkey [32]byte, depositSigner common.Address, rootSigner common.Address, active bool, effectiveEpoch uint64) []byte {
	enc, err := sTCoordinator.abi.Pack("scheduleOperator", noId, depositHotkey, depositSigner, rootSigner, active, effectiveEpoch)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackScheduleOperator is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x3d191915.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function scheduleOperator(uint256 noId, bytes32 depositHotkey, address depositSigner, address rootSigner, bool active, uint64 effectiveEpoch) returns()
func (sTCoordinator *STCoordinator) TryPackScheduleOperator(noId *big.Int, depositHotkey [32]byte, depositSigner common.Address, rootSigner common.Address, active bool, effectiveEpoch uint64) ([]byte, error) {
	return sTCoordinator.abi.Pack("scheduleOperator", noId, depositHotkey, depositSigner, rootSigner, active, effectiveEpoch)
}

// PackSchedulePolicy is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xed7f5a1a.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function schedulePolicy((bytes32,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint256,uint256) next) returns()
func (sTCoordinator *STCoordinator) PackSchedulePolicy(next STCoordinatorPolicySnapshot) []byte {
	enc, err := sTCoordinator.abi.Pack("schedulePolicy", next)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSchedulePolicy is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xed7f5a1a.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function schedulePolicy((bytes32,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint64,uint256,uint256) next) returns()
func (sTCoordinator *STCoordinator) TryPackSchedulePolicy(next STCoordinatorPolicySnapshot) ([]byte, error) {
	return sTCoordinator.abi.Pack("schedulePolicy", next)
}

// PackSelfColdkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x877e4394.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function selfColdkey() view returns(bytes32)
func (sTCoordinator *STCoordinator) PackSelfColdkey() []byte {
	enc, err := sTCoordinator.abi.Pack("selfColdkey")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSelfColdkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x877e4394.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function selfColdkey() view returns(bytes32)
func (sTCoordinator *STCoordinator) TryPackSelfColdkey() ([]byte, error) {
	return sTCoordinator.abi.Pack("selfColdkey")
}

// UnpackSelfColdkey is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x877e4394.
//
// Solidity: function selfColdkey() view returns(bytes32)
func (sTCoordinator *STCoordinator) UnpackSelfColdkey(data []byte) ([32]byte, error) {
	out, err := sTCoordinator.abi.Unpack("selfColdkey", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackSetPaused is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x16c38b3c.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function setPaused(bool paused_) returns()
func (sTCoordinator *STCoordinator) PackSetPaused(paused bool) []byte {
	enc, err := sTCoordinator.abi.Pack("setPaused", paused)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSetPaused is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x16c38b3c.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function setPaused(bool paused_) returns()
func (sTCoordinator *STCoordinator) TryPackSetPaused(paused bool) ([]byte, error) {
	return sTCoordinator.abi.Pack("setPaused", paused)
}

// PackSettlementVault is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x2aa84ce6.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function settlementVault() view returns(address)
func (sTCoordinator *STCoordinator) PackSettlementVault() []byte {
	enc, err := sTCoordinator.abi.Pack("settlementVault")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSettlementVault is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x2aa84ce6.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function settlementVault() view returns(address)
func (sTCoordinator *STCoordinator) TryPackSettlementVault() ([]byte, error) {
	return sTCoordinator.abi.Pack("settlementVault")
}

// UnpackSettlementVault is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x2aa84ce6.
//
// Solidity: function settlementVault() view returns(address)
func (sTCoordinator *STCoordinator) UnpackSettlementVault(data []byte) (common.Address, error) {
	out, err := sTCoordinator.abi.Unpack("settlementVault", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackTransferOwnership is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf2fde38b.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (sTCoordinator *STCoordinator) PackTransferOwnership(newOwner common.Address) []byte {
	enc, err := sTCoordinator.abi.Pack("transferOwnership", newOwner)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackTransferOwnership is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf2fde38b.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (sTCoordinator *STCoordinator) TryPackTransferOwnership(newOwner common.Address) ([]byte, error) {
	return sTCoordinator.abi.Pack("transferOwnership", newOwner)
}

// PackUpgradeToAndCall is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4f1ef286.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (sTCoordinator *STCoordinator) PackUpgradeToAndCall(newImplementation common.Address, data []byte) []byte {
	enc, err := sTCoordinator.abi.Pack("upgradeToAndCall", newImplementation, data)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackUpgradeToAndCall is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4f1ef286.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (sTCoordinator *STCoordinator) TryPackUpgradeToAndCall(newImplementation common.Address, data []byte) ([]byte, error) {
	return sTCoordinator.abi.Pack("upgradeToAndCall", newImplementation, data)
}

// STCoordinatorCommitmentMirrored represents a CommitmentMirrored event raised by the STCoordinator contract.
type STCoordinatorCommitmentMirrored struct {
	Hotkey             [32]byte
	CommitmentHash     [32]byte
	FinalizedBlock     uint64
	FinalizedBlockHash [32]byte
	Raw                *types.Log // Blockchain specific contextual infos
}

const STCoordinatorCommitmentMirroredEventName = "CommitmentMirrored"

// ContractEventName returns the user-defined event name.
func (STCoordinatorCommitmentMirrored) ContractEventName() string {
	return STCoordinatorCommitmentMirroredEventName
}

// UnpackCommitmentMirroredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event CommitmentMirrored(bytes32 indexed hotkey, bytes32 indexed commitmentHash, uint64 finalizedBlock, bytes32 finalizedBlockHash)
func (sTCoordinator *STCoordinator) UnpackCommitmentMirroredEvent(log *types.Log) (*STCoordinatorCommitmentMirrored, error) {
	event := "CommitmentMirrored"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorCommitmentMirrored)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorCommitmentOracleScheduled represents a CommitmentOracleScheduled event raised by the STCoordinator contract.
type STCoordinatorCommitmentOracleScheduled struct {
	Oracle         common.Address
	EffectiveEpoch uint64
	Raw            *types.Log // Blockchain specific contextual infos
}

const STCoordinatorCommitmentOracleScheduledEventName = "CommitmentOracleScheduled"

// ContractEventName returns the user-defined event name.
func (STCoordinatorCommitmentOracleScheduled) ContractEventName() string {
	return STCoordinatorCommitmentOracleScheduledEventName
}

// UnpackCommitmentOracleScheduledEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event CommitmentOracleScheduled(address indexed oracle, uint64 indexed effectiveEpoch)
func (sTCoordinator *STCoordinator) UnpackCommitmentOracleScheduledEvent(log *types.Log) (*STCoordinatorCommitmentOracleScheduled, error) {
	event := "CommitmentOracleScheduled"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorCommitmentOracleScheduled)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorConvictionAdded represents a ConvictionAdded event raised by the STCoordinator contract.
type STCoordinatorConvictionAdded struct {
	NoId       *big.Int
	Epoch      *big.Int
	Funder     common.Address
	Amount     *big.Int
	PolicyHash [32]byte
	Nonce      *big.Int
	Raw        *types.Log // Blockchain specific contextual infos
}

const STCoordinatorConvictionAddedEventName = "ConvictionAdded"

// ContractEventName returns the user-defined event name.
func (STCoordinatorConvictionAdded) ContractEventName() string {
	return STCoordinatorConvictionAddedEventName
}

// UnpackConvictionAddedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event ConvictionAdded(uint256 indexed noId, uint256 indexed epoch, address indexed funder, uint256 amount, bytes32 policyHash, uint256 nonce)
func (sTCoordinator *STCoordinator) UnpackConvictionAddedEvent(log *types.Log) (*STCoordinatorConvictionAdded, error) {
	event := "ConvictionAdded"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorConvictionAdded)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorDeposit represents a Deposit event raised by the STCoordinator contract.
type STCoordinatorDeposit struct {
	NoId       *big.Int
	Epoch      *big.Int
	Funder     common.Address
	Amount     *big.Int
	PolicyHash [32]byte
	Nonce      *big.Int
	Raw        *types.Log // Blockchain specific contextual infos
}

const STCoordinatorDepositEventName = "Deposit"

// ContractEventName returns the user-defined event name.
func (STCoordinatorDeposit) ContractEventName() string {
	return STCoordinatorDepositEventName
}

// UnpackDepositEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event Deposit(uint256 indexed noId, uint256 indexed epoch, address indexed funder, uint256 amount, bytes32 policyHash, uint256 nonce)
func (sTCoordinator *STCoordinator) UnpackDepositEvent(log *types.Log) (*STCoordinatorDeposit, error) {
	event := "Deposit"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorDeposit)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorFleetBindingCleaned represents a FleetBindingCleaned event raised by the STCoordinator contract.
type STCoordinatorFleetBindingCleaned struct {
	ClientId       [16]byte
	CleanedAtEpoch uint64
	Raw            *types.Log // Blockchain specific contextual infos
}

const STCoordinatorFleetBindingCleanedEventName = "FleetBindingCleaned"

// ContractEventName returns the user-defined event name.
func (STCoordinatorFleetBindingCleaned) ContractEventName() string {
	return STCoordinatorFleetBindingCleanedEventName
}

// UnpackFleetBindingCleanedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event FleetBindingCleaned(bytes16 indexed clientId, uint64 indexed cleanedAtEpoch)
func (sTCoordinator *STCoordinator) UnpackFleetBindingCleanedEvent(log *types.Log) (*STCoordinatorFleetBindingCleaned, error) {
	event := "FleetBindingCleaned"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorFleetBindingCleaned)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorFleetBindingRevoked represents a FleetBindingRevoked event raised by the STCoordinator contract.
type STCoordinatorFleetBindingRevoked struct {
	ClientId       [16]byte
	Generation     uint64
	EffectiveEpoch uint64
	Raw            *types.Log // Blockchain specific contextual infos
}

const STCoordinatorFleetBindingRevokedEventName = "FleetBindingRevoked"

// ContractEventName returns the user-defined event name.
func (STCoordinatorFleetBindingRevoked) ContractEventName() string {
	return STCoordinatorFleetBindingRevokedEventName
}

// UnpackFleetBindingRevokedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event FleetBindingRevoked(bytes16 indexed clientId, uint64 generation, uint64 effectiveEpoch)
func (sTCoordinator *STCoordinator) UnpackFleetBindingRevokedEvent(log *types.Log) (*STCoordinatorFleetBindingRevoked, error) {
	event := "FleetBindingRevoked"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorFleetBindingRevoked)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorFleetBound represents a FleetBound event raised by the STCoordinator contract.
type STCoordinatorFleetBound struct {
	ClientId       [16]byte
	FleetId        [32]byte
	Hotkey         [32]byte
	Uid            uint16
	Generation     uint64
	ValidFromEpoch uint64
	ValidToEpoch   uint64
	Raw            *types.Log // Blockchain specific contextual infos
}

const STCoordinatorFleetBoundEventName = "FleetBound"

// ContractEventName returns the user-defined event name.
func (STCoordinatorFleetBound) ContractEventName() string {
	return STCoordinatorFleetBoundEventName
}

// UnpackFleetBoundEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event FleetBound(bytes16 indexed clientId, bytes32 indexed fleetId, bytes32 indexed hotkey, uint16 uid, uint64 generation, uint64 validFromEpoch, uint64 validToEpoch)
func (sTCoordinator *STCoordinator) UnpackFleetBoundEvent(log *types.Log) (*STCoordinatorFleetBound, error) {
	event := "FleetBound"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorFleetBound)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorGuardianScheduled represents a GuardianScheduled event raised by the STCoordinator contract.
type STCoordinatorGuardianScheduled struct {
	Guardian       common.Address
	EffectiveEpoch uint64
	Raw            *types.Log // Blockchain specific contextual infos
}

const STCoordinatorGuardianScheduledEventName = "GuardianScheduled"

// ContractEventName returns the user-defined event name.
func (STCoordinatorGuardianScheduled) ContractEventName() string {
	return STCoordinatorGuardianScheduledEventName
}

// UnpackGuardianScheduledEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event GuardianScheduled(address indexed guardian, uint64 indexed effectiveEpoch)
func (sTCoordinator *STCoordinator) UnpackGuardianScheduledEvent(log *types.Log) (*STCoordinatorGuardianScheduled, error) {
	event := "GuardianScheduled"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorGuardianScheduled)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorGuardianSet represents a GuardianSet event raised by the STCoordinator contract.
type STCoordinatorGuardianSet struct {
	Guardian common.Address
	Raw      *types.Log // Blockchain specific contextual infos
}

const STCoordinatorGuardianSetEventName = "GuardianSet"

// ContractEventName returns the user-defined event name.
func (STCoordinatorGuardianSet) ContractEventName() string {
	return STCoordinatorGuardianSetEventName
}

// UnpackGuardianSetEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event GuardianSet(address indexed guardian)
func (sTCoordinator *STCoordinator) UnpackGuardianSetEvent(log *types.Log) (*STCoordinatorGuardianSet, error) {
	event := "GuardianSet"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorGuardianSet)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorInitialized represents a Initialized event raised by the STCoordinator contract.
type STCoordinatorInitialized struct {
	Version uint64
	Raw     *types.Log // Blockchain specific contextual infos
}

const STCoordinatorInitializedEventName = "Initialized"

// ContractEventName returns the user-defined event name.
func (STCoordinatorInitialized) ContractEventName() string {
	return STCoordinatorInitializedEventName
}

// UnpackInitializedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event Initialized(uint64 version)
func (sTCoordinator *STCoordinator) UnpackInitializedEvent(log *types.Log) (*STCoordinatorInitialized, error) {
	event := "Initialized"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorInitialized)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorOperatorEpochFinalized represents a OperatorEpochFinalized event raised by the STCoordinator contract.
type STCoordinatorOperatorEpochFinalized struct {
	Epoch       *big.Int
	NoId        *big.Int
	RootPresent bool
	Raw         *types.Log // Blockchain specific contextual infos
}

const STCoordinatorOperatorEpochFinalizedEventName = "OperatorEpochFinalized"

// ContractEventName returns the user-defined event name.
func (STCoordinatorOperatorEpochFinalized) ContractEventName() string {
	return STCoordinatorOperatorEpochFinalizedEventName
}

// UnpackOperatorEpochFinalizedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event OperatorEpochFinalized(uint256 indexed epoch, uint256 indexed noId, bool rootPresent)
func (sTCoordinator *STCoordinator) UnpackOperatorEpochFinalizedEvent(log *types.Log) (*STCoordinatorOperatorEpochFinalized, error) {
	event := "OperatorEpochFinalized"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorOperatorEpochFinalized)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorOperatorRootCommitted represents a OperatorRootCommitted event raised by the STCoordinator contract.
type STCoordinatorOperatorRootCommitted struct {
	Epoch        *big.Int
	NoId         *big.Int
	PayoutRoot   [32]byte
	ArtifactHash [32]byte
	Committer    common.Address
	Raw          *types.Log // Blockchain specific contextual infos
}

const STCoordinatorOperatorRootCommittedEventName = "OperatorRootCommitted"

// ContractEventName returns the user-defined event name.
func (STCoordinatorOperatorRootCommitted) ContractEventName() string {
	return STCoordinatorOperatorRootCommittedEventName
}

// UnpackOperatorRootCommittedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event OperatorRootCommitted(uint256 indexed epoch, uint256 indexed noId, bytes32 payoutRoot, bytes32 artifactHash, address committer)
func (sTCoordinator *STCoordinator) UnpackOperatorRootCommittedEvent(log *types.Log) (*STCoordinatorOperatorRootCommitted, error) {
	event := "OperatorRootCommitted"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorOperatorRootCommitted)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorOperatorScheduled represents a OperatorScheduled event raised by the STCoordinator contract.
type STCoordinatorOperatorScheduled struct {
	NoId           *big.Int
	EffectiveEpoch uint64
	Coldkey        [32]byte
	PoolHotkey     [32]byte
	DepositHotkey  [32]byte
	DepositSigner  common.Address
	RootSigner     common.Address
	Active         bool
	Raw            *types.Log // Blockchain specific contextual infos
}

const STCoordinatorOperatorScheduledEventName = "OperatorScheduled"

// ContractEventName returns the user-defined event name.
func (STCoordinatorOperatorScheduled) ContractEventName() string {
	return STCoordinatorOperatorScheduledEventName
}

// UnpackOperatorScheduledEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event OperatorScheduled(uint256 indexed noId, uint64 indexed effectiveEpoch, bytes32 coldkey, bytes32 poolHotkey, bytes32 depositHotkey, address depositSigner, address rootSigner, bool active)
func (sTCoordinator *STCoordinator) UnpackOperatorScheduledEvent(log *types.Log) (*STCoordinatorOperatorScheduled, error) {
	event := "OperatorScheduled"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorOperatorScheduled)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorOwnershipTransferred represents a OwnershipTransferred event raised by the STCoordinator contract.
type STCoordinatorOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           *types.Log // Blockchain specific contextual infos
}

const STCoordinatorOwnershipTransferredEventName = "OwnershipTransferred"

// ContractEventName returns the user-defined event name.
func (STCoordinatorOwnershipTransferred) ContractEventName() string {
	return STCoordinatorOwnershipTransferredEventName
}

// UnpackOwnershipTransferredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (sTCoordinator *STCoordinator) UnpackOwnershipTransferredEvent(log *types.Log) (*STCoordinatorOwnershipTransferred, error) {
	event := "OwnershipTransferred"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorOwnershipTransferred)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorPausedSet represents a PausedSet event raised by the STCoordinator contract.
type STCoordinatorPausedSet struct {
	Paused bool
	Caller common.Address
	Raw    *types.Log // Blockchain specific contextual infos
}

const STCoordinatorPausedSetEventName = "PausedSet"

// ContractEventName returns the user-defined event name.
func (STCoordinatorPausedSet) ContractEventName() string {
	return STCoordinatorPausedSetEventName
}

// UnpackPausedSetEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event PausedSet(bool paused, address indexed caller)
func (sTCoordinator *STCoordinator) UnpackPausedSetEvent(log *types.Log) (*STCoordinatorPausedSet, error) {
	event := "PausedSet"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorPausedSet)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorPolicyScheduled represents a PolicyScheduled event raised by the STCoordinator contract.
type STCoordinatorPolicyScheduled struct {
	Index          *big.Int
	PolicyHash     [32]byte
	EffectiveEpoch uint64
	EffectiveBlock uint64
	Raw            *types.Log // Blockchain specific contextual infos
}

const STCoordinatorPolicyScheduledEventName = "PolicyScheduled"

// ContractEventName returns the user-defined event name.
func (STCoordinatorPolicyScheduled) ContractEventName() string {
	return STCoordinatorPolicyScheduledEventName
}

// UnpackPolicyScheduledEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event PolicyScheduled(uint256 indexed index, bytes32 indexed policyHash, uint64 indexed effectiveEpoch, uint64 effectiveBlock)
func (sTCoordinator *STCoordinator) UnpackPolicyScheduledEvent(log *types.Log) (*STCoordinatorPolicyScheduled, error) {
	event := "PolicyScheduled"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorPolicyScheduled)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// STCoordinatorUpgraded represents a Upgraded event raised by the STCoordinator contract.
type STCoordinatorUpgraded struct {
	Implementation common.Address
	Raw            *types.Log // Blockchain specific contextual infos
}

const STCoordinatorUpgradedEventName = "Upgraded"

// ContractEventName returns the user-defined event name.
func (STCoordinatorUpgraded) ContractEventName() string {
	return STCoordinatorUpgradedEventName
}

// UnpackUpgradedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event Upgraded(address indexed implementation)
func (sTCoordinator *STCoordinator) UnpackUpgradedEvent(log *types.Log) (*STCoordinatorUpgraded, error) {
	event := "Upgraded"
	if len(log.Topics) == 0 || log.Topics[0] != sTCoordinator.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STCoordinatorUpgraded)
	if len(log.Data) > 0 {
		if err := sTCoordinator.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTCoordinator.abi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	if err := abi.ParseTopics(out, indexed, log.Topics[1:]); err != nil {
		return nil, err
	}
	out.Raw = log
	return out, nil
}

// UnpackError attempts to decode the provided error data using user-defined
// error definitions.
func (sTCoordinator *STCoordinator) UnpackError(raw []byte) (any, error) {
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["AddressEmptyCode"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackAddressEmptyCodeError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["AlreadyCommitted"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackAlreadyCommittedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["CapExceeded"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackCapExceededError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["DeadlineExpired"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackDeadlineExpiredError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["ERC1967InvalidImplementation"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackERC1967InvalidImplementationError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["ERC1967NonPayable"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackERC1967NonPayableError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["FailedCall"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackFailedCallError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["FundsNotReceived"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackFundsNotReceivedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["InactiveOperator"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackInactiveOperatorError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["InvalidBinding"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackInvalidBindingError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["InvalidConfiguration"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackInvalidConfigurationError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["InvalidEpoch"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackInvalidEpochError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["InvalidInitialization"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackInvalidInitializationError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["InvalidNonce"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackInvalidNonceError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["InvalidPolicy"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackInvalidPolicyError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["InvalidSignature"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackInvalidSignatureError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["InvalidWindow"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackInvalidWindowError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["NativeRefundFailed"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackNativeRefundFailedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["NotInitializing"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackNotInitializingError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["OwnableInvalidOwner"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackOwnableInvalidOwnerError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["OwnableUnauthorizedAccount"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackOwnableUnauthorizedAccountError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["Paused"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackPausedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["Reentrancy"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackReentrancyError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["RuntimeIdentityMissing"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackRuntimeIdentityMissingError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["SafeCastOverflowedUintDowncast"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackSafeCastOverflowedUintDowncastError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["StaleCommitment"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackStaleCommitmentError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["UUPSUnauthorizedCallContext"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackUUPSUnauthorizedCallContextError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["UUPSUnsupportedProxiableUUID"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackUUPSUnsupportedProxiableUUIDError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["Unauthorized"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackUnauthorizedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTCoordinator.abi.Errors["UnknownOperator"].ID.Bytes()[:4]) {
		return sTCoordinator.UnpackUnknownOperatorError(raw[4:])
	}
	return nil, errors.New("Unknown error")
}

// STCoordinatorAddressEmptyCode represents a AddressEmptyCode error raised by the STCoordinator contract.
type STCoordinatorAddressEmptyCode struct {
	Target common.Address
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error AddressEmptyCode(address target)
func STCoordinatorAddressEmptyCodeErrorID() common.Hash {
	return common.HexToHash("0x9996b315c842ff135b8fc4a08ad5df1c344efbc03d2687aecc0678050d2aac89")
}

// UnpackAddressEmptyCodeError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error AddressEmptyCode(address target)
func (sTCoordinator *STCoordinator) UnpackAddressEmptyCodeError(raw []byte) (*STCoordinatorAddressEmptyCode, error) {
	out := new(STCoordinatorAddressEmptyCode)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "AddressEmptyCode", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorAlreadyCommitted represents a AlreadyCommitted error raised by the STCoordinator contract.
type STCoordinatorAlreadyCommitted struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error AlreadyCommitted()
func STCoordinatorAlreadyCommittedErrorID() common.Hash {
	return common.HexToHash("0xbfec55587600524a9afa4af6da1e1345f08fce59d99e4869f19166c295681a3a")
}

// UnpackAlreadyCommittedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error AlreadyCommitted()
func (sTCoordinator *STCoordinator) UnpackAlreadyCommittedError(raw []byte) (*STCoordinatorAlreadyCommitted, error) {
	out := new(STCoordinatorAlreadyCommitted)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "AlreadyCommitted", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorCapExceeded represents a CapExceeded error raised by the STCoordinator contract.
type STCoordinatorCapExceeded struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error CapExceeded()
func STCoordinatorCapExceededErrorID() common.Hash {
	return common.HexToHash("0xa4875a49e4c69b4dd564732678e29e8cc32caabdb4648fffc1c2f5dc38305b55")
}

// UnpackCapExceededError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error CapExceeded()
func (sTCoordinator *STCoordinator) UnpackCapExceededError(raw []byte) (*STCoordinatorCapExceeded, error) {
	out := new(STCoordinatorCapExceeded)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "CapExceeded", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorDeadlineExpired represents a DeadlineExpired error raised by the STCoordinator contract.
type STCoordinatorDeadlineExpired struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error DeadlineExpired()
func STCoordinatorDeadlineExpiredErrorID() common.Hash {
	return common.HexToHash("0x1ab7da6b04cf80fcc6534c1f63ce7b24fd7488ab40444b4e5271ccc1c7f64567")
}

// UnpackDeadlineExpiredError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error DeadlineExpired()
func (sTCoordinator *STCoordinator) UnpackDeadlineExpiredError(raw []byte) (*STCoordinatorDeadlineExpired, error) {
	out := new(STCoordinatorDeadlineExpired)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "DeadlineExpired", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorERC1967InvalidImplementation represents a ERC1967InvalidImplementation error raised by the STCoordinator contract.
type STCoordinatorERC1967InvalidImplementation struct {
	Implementation common.Address
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error ERC1967InvalidImplementation(address implementation)
func STCoordinatorERC1967InvalidImplementationErrorID() common.Hash {
	return common.HexToHash("0x4c9c8ce3ceb3130f17f7cdba48d89b5b0129f266a8bac114e6e315a41879b617")
}

// UnpackERC1967InvalidImplementationError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error ERC1967InvalidImplementation(address implementation)
func (sTCoordinator *STCoordinator) UnpackERC1967InvalidImplementationError(raw []byte) (*STCoordinatorERC1967InvalidImplementation, error) {
	out := new(STCoordinatorERC1967InvalidImplementation)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "ERC1967InvalidImplementation", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorERC1967NonPayable represents a ERC1967NonPayable error raised by the STCoordinator contract.
type STCoordinatorERC1967NonPayable struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error ERC1967NonPayable()
func STCoordinatorERC1967NonPayableErrorID() common.Hash {
	return common.HexToHash("0xb398979fa84f543c8e222f17890372c487baf85e062276c127fef521eea7224b")
}

// UnpackERC1967NonPayableError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error ERC1967NonPayable()
func (sTCoordinator *STCoordinator) UnpackERC1967NonPayableError(raw []byte) (*STCoordinatorERC1967NonPayable, error) {
	out := new(STCoordinatorERC1967NonPayable)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "ERC1967NonPayable", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorFailedCall represents a FailedCall error raised by the STCoordinator contract.
type STCoordinatorFailedCall struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error FailedCall()
func STCoordinatorFailedCallErrorID() common.Hash {
	return common.HexToHash("0xd6bda27508c0fb6d8a39b4b122878dab26f731a7d4e4abe711dd3731899052a4")
}

// UnpackFailedCallError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error FailedCall()
func (sTCoordinator *STCoordinator) UnpackFailedCallError(raw []byte) (*STCoordinatorFailedCall, error) {
	out := new(STCoordinatorFailedCall)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "FailedCall", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorFundsNotReceived represents a FundsNotReceived error raised by the STCoordinator contract.
type STCoordinatorFundsNotReceived struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error FundsNotReceived()
func STCoordinatorFundsNotReceivedErrorID() common.Hash {
	return common.HexToHash("0xeaed276fc68fa607bcc48ac9d6e8e327a9f00b2482a734601db29981acad003c")
}

// UnpackFundsNotReceivedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error FundsNotReceived()
func (sTCoordinator *STCoordinator) UnpackFundsNotReceivedError(raw []byte) (*STCoordinatorFundsNotReceived, error) {
	out := new(STCoordinatorFundsNotReceived)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "FundsNotReceived", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorInactiveOperator represents a InactiveOperator error raised by the STCoordinator contract.
type STCoordinatorInactiveOperator struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InactiveOperator()
func STCoordinatorInactiveOperatorErrorID() common.Hash {
	return common.HexToHash("0xc3c2f8d7f8c41ca277101f642c0890a56466fe79b366140100a1d4ff2dc17132")
}

// UnpackInactiveOperatorError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InactiveOperator()
func (sTCoordinator *STCoordinator) UnpackInactiveOperatorError(raw []byte) (*STCoordinatorInactiveOperator, error) {
	out := new(STCoordinatorInactiveOperator)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "InactiveOperator", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorInvalidBinding represents a InvalidBinding error raised by the STCoordinator contract.
type STCoordinatorInvalidBinding struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidBinding()
func STCoordinatorInvalidBindingErrorID() common.Hash {
	return common.HexToHash("0x0f30eb404c14d490d7d53cafdfed3675150f88e0529a21350c49bc2b9d8383d0")
}

// UnpackInvalidBindingError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidBinding()
func (sTCoordinator *STCoordinator) UnpackInvalidBindingError(raw []byte) (*STCoordinatorInvalidBinding, error) {
	out := new(STCoordinatorInvalidBinding)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "InvalidBinding", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorInvalidConfiguration represents a InvalidConfiguration error raised by the STCoordinator contract.
type STCoordinatorInvalidConfiguration struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidConfiguration()
func STCoordinatorInvalidConfigurationErrorID() common.Hash {
	return common.HexToHash("0xc52a9bd3d9e475b9056a93172ef6968d775a7cd41c4255bbebf12e90a5fbbd39")
}

// UnpackInvalidConfigurationError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidConfiguration()
func (sTCoordinator *STCoordinator) UnpackInvalidConfigurationError(raw []byte) (*STCoordinatorInvalidConfiguration, error) {
	out := new(STCoordinatorInvalidConfiguration)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "InvalidConfiguration", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorInvalidEpoch represents a InvalidEpoch error raised by the STCoordinator contract.
type STCoordinatorInvalidEpoch struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidEpoch()
func STCoordinatorInvalidEpochErrorID() common.Hash {
	return common.HexToHash("0xd5b25b63142e607aedcb4587d8d0a7885e39fe07e954f7f94b12e1c076cdbd11")
}

// UnpackInvalidEpochError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidEpoch()
func (sTCoordinator *STCoordinator) UnpackInvalidEpochError(raw []byte) (*STCoordinatorInvalidEpoch, error) {
	out := new(STCoordinatorInvalidEpoch)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "InvalidEpoch", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorInvalidInitialization represents a InvalidInitialization error raised by the STCoordinator contract.
type STCoordinatorInvalidInitialization struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidInitialization()
func STCoordinatorInvalidInitializationErrorID() common.Hash {
	return common.HexToHash("0xf92ee8a957075833165f68c320933b1a1294aafc84ee6e0dd3fb178008f9aaf5")
}

// UnpackInvalidInitializationError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidInitialization()
func (sTCoordinator *STCoordinator) UnpackInvalidInitializationError(raw []byte) (*STCoordinatorInvalidInitialization, error) {
	out := new(STCoordinatorInvalidInitialization)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "InvalidInitialization", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorInvalidNonce represents a InvalidNonce error raised by the STCoordinator contract.
type STCoordinatorInvalidNonce struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidNonce()
func STCoordinatorInvalidNonceErrorID() common.Hash {
	return common.HexToHash("0x756688fec2871909d72599c334b663ffcc94654c438569966c7fd3ab3a351f34")
}

// UnpackInvalidNonceError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidNonce()
func (sTCoordinator *STCoordinator) UnpackInvalidNonceError(raw []byte) (*STCoordinatorInvalidNonce, error) {
	out := new(STCoordinatorInvalidNonce)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "InvalidNonce", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorInvalidPolicy represents a InvalidPolicy error raised by the STCoordinator contract.
type STCoordinatorInvalidPolicy struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidPolicy()
func STCoordinatorInvalidPolicyErrorID() common.Hash {
	return common.HexToHash("0xd06b96b1115b9e92763e754ca172e49828c93297522e340979d4243da3748bec")
}

// UnpackInvalidPolicyError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidPolicy()
func (sTCoordinator *STCoordinator) UnpackInvalidPolicyError(raw []byte) (*STCoordinatorInvalidPolicy, error) {
	out := new(STCoordinatorInvalidPolicy)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "InvalidPolicy", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorInvalidSignature represents a InvalidSignature error raised by the STCoordinator contract.
type STCoordinatorInvalidSignature struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidSignature()
func STCoordinatorInvalidSignatureErrorID() common.Hash {
	return common.HexToHash("0x8baa579fce362245063d36f11747a89dd489c54795634fc673cc0e0db51fedc5")
}

// UnpackInvalidSignatureError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidSignature()
func (sTCoordinator *STCoordinator) UnpackInvalidSignatureError(raw []byte) (*STCoordinatorInvalidSignature, error) {
	out := new(STCoordinatorInvalidSignature)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "InvalidSignature", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorInvalidWindow represents a InvalidWindow error raised by the STCoordinator contract.
type STCoordinatorInvalidWindow struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidWindow()
func STCoordinatorInvalidWindowErrorID() common.Hash {
	return common.HexToHash("0x392334ed85cf5b0998d72154ce4383ddab4c856d648b4f9bd678a630b321c06c")
}

// UnpackInvalidWindowError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidWindow()
func (sTCoordinator *STCoordinator) UnpackInvalidWindowError(raw []byte) (*STCoordinatorInvalidWindow, error) {
	out := new(STCoordinatorInvalidWindow)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "InvalidWindow", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorNativeRefundFailed represents a NativeRefundFailed error raised by the STCoordinator contract.
type STCoordinatorNativeRefundFailed struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NativeRefundFailed()
func STCoordinatorNativeRefundFailedErrorID() common.Hash {
	return common.HexToHash("0x8520d710691d90175ff6d0cde74ada1f9d91ac205406750a2381430c693c006e")
}

// UnpackNativeRefundFailedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NativeRefundFailed()
func (sTCoordinator *STCoordinator) UnpackNativeRefundFailedError(raw []byte) (*STCoordinatorNativeRefundFailed, error) {
	out := new(STCoordinatorNativeRefundFailed)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "NativeRefundFailed", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorNotInitializing represents a NotInitializing error raised by the STCoordinator contract.
type STCoordinatorNotInitializing struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NotInitializing()
func STCoordinatorNotInitializingErrorID() common.Hash {
	return common.HexToHash("0xd7e6bcf8597daa127dc9f0048d2f08d5ef140a2cb659feabd700beff1f7a8302")
}

// UnpackNotInitializingError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NotInitializing()
func (sTCoordinator *STCoordinator) UnpackNotInitializingError(raw []byte) (*STCoordinatorNotInitializing, error) {
	out := new(STCoordinatorNotInitializing)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "NotInitializing", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorOwnableInvalidOwner represents a OwnableInvalidOwner error raised by the STCoordinator contract.
type STCoordinatorOwnableInvalidOwner struct {
	Owner common.Address
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error OwnableInvalidOwner(address owner)
func STCoordinatorOwnableInvalidOwnerErrorID() common.Hash {
	return common.HexToHash("0x1e4fbdf7f3ef8bcaa855599e3abf48b232380f183f08f6f813d9ffa5bd585188")
}

// UnpackOwnableInvalidOwnerError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error OwnableInvalidOwner(address owner)
func (sTCoordinator *STCoordinator) UnpackOwnableInvalidOwnerError(raw []byte) (*STCoordinatorOwnableInvalidOwner, error) {
	out := new(STCoordinatorOwnableInvalidOwner)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "OwnableInvalidOwner", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorOwnableUnauthorizedAccount represents a OwnableUnauthorizedAccount error raised by the STCoordinator contract.
type STCoordinatorOwnableUnauthorizedAccount struct {
	Account common.Address
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error OwnableUnauthorizedAccount(address account)
func STCoordinatorOwnableUnauthorizedAccountErrorID() common.Hash {
	return common.HexToHash("0x118cdaa7a341953d1887a2245fd6665d741c67c8c50581daa59e1d03373fa188")
}

// UnpackOwnableUnauthorizedAccountError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error OwnableUnauthorizedAccount(address account)
func (sTCoordinator *STCoordinator) UnpackOwnableUnauthorizedAccountError(raw []byte) (*STCoordinatorOwnableUnauthorizedAccount, error) {
	out := new(STCoordinatorOwnableUnauthorizedAccount)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "OwnableUnauthorizedAccount", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorPaused represents a Paused error raised by the STCoordinator contract.
type STCoordinatorPaused struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error Paused()
func STCoordinatorPausedErrorID() common.Hash {
	return common.HexToHash("0x9e87fac88ff661f02d44f95383c817fece4bce600a3dab7a54406878b965e752")
}

// UnpackPausedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error Paused()
func (sTCoordinator *STCoordinator) UnpackPausedError(raw []byte) (*STCoordinatorPaused, error) {
	out := new(STCoordinatorPaused)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "Paused", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorReentrancy represents a Reentrancy error raised by the STCoordinator contract.
type STCoordinatorReentrancy struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error Reentrancy()
func STCoordinatorReentrancyErrorID() common.Hash {
	return common.HexToHash("0xab143c06c9772d69bbbc9f2fe74acd02f810e93b099f3d1dac8448ac9ae35991")
}

// UnpackReentrancyError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error Reentrancy()
func (sTCoordinator *STCoordinator) UnpackReentrancyError(raw []byte) (*STCoordinatorReentrancy, error) {
	out := new(STCoordinatorReentrancy)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "Reentrancy", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorRuntimeIdentityMissing represents a RuntimeIdentityMissing error raised by the STCoordinator contract.
type STCoordinatorRuntimeIdentityMissing struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error RuntimeIdentityMissing()
func STCoordinatorRuntimeIdentityMissingErrorID() common.Hash {
	return common.HexToHash("0x56a9fcb6bba80e53bed8b0167e0e3784d5c97be596717c347dd67bb9d7b89c79")
}

// UnpackRuntimeIdentityMissingError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error RuntimeIdentityMissing()
func (sTCoordinator *STCoordinator) UnpackRuntimeIdentityMissingError(raw []byte) (*STCoordinatorRuntimeIdentityMissing, error) {
	out := new(STCoordinatorRuntimeIdentityMissing)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "RuntimeIdentityMissing", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorSafeCastOverflowedUintDowncast represents a SafeCastOverflowedUintDowncast error raised by the STCoordinator contract.
type STCoordinatorSafeCastOverflowedUintDowncast struct {
	Bits  uint8
	Value *big.Int
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error SafeCastOverflowedUintDowncast(uint8 bits, uint256 value)
func STCoordinatorSafeCastOverflowedUintDowncastErrorID() common.Hash {
	return common.HexToHash("0x6dfcc6503a32754ce7a89698e18201fc5294fd4aad43edefee786f88423b1a12")
}

// UnpackSafeCastOverflowedUintDowncastError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error SafeCastOverflowedUintDowncast(uint8 bits, uint256 value)
func (sTCoordinator *STCoordinator) UnpackSafeCastOverflowedUintDowncastError(raw []byte) (*STCoordinatorSafeCastOverflowedUintDowncast, error) {
	out := new(STCoordinatorSafeCastOverflowedUintDowncast)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "SafeCastOverflowedUintDowncast", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorStaleCommitment represents a StaleCommitment error raised by the STCoordinator contract.
type STCoordinatorStaleCommitment struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error StaleCommitment()
func STCoordinatorStaleCommitmentErrorID() common.Hash {
	return common.HexToHash("0x3d618e50240e5e66adf03ae85d51a8c5d8779d1d16a62a01162c0bf9dd05eb93")
}

// UnpackStaleCommitmentError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error StaleCommitment()
func (sTCoordinator *STCoordinator) UnpackStaleCommitmentError(raw []byte) (*STCoordinatorStaleCommitment, error) {
	out := new(STCoordinatorStaleCommitment)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "StaleCommitment", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorUUPSUnauthorizedCallContext represents a UUPSUnauthorizedCallContext error raised by the STCoordinator contract.
type STCoordinatorUUPSUnauthorizedCallContext struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error UUPSUnauthorizedCallContext()
func STCoordinatorUUPSUnauthorizedCallContextErrorID() common.Hash {
	return common.HexToHash("0xe07c8dba242a06571ac65fe4bbe20522c9fb111cb33599b799ff8039c1ed18f4")
}

// UnpackUUPSUnauthorizedCallContextError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error UUPSUnauthorizedCallContext()
func (sTCoordinator *STCoordinator) UnpackUUPSUnauthorizedCallContextError(raw []byte) (*STCoordinatorUUPSUnauthorizedCallContext, error) {
	out := new(STCoordinatorUUPSUnauthorizedCallContext)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "UUPSUnauthorizedCallContext", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorUUPSUnsupportedProxiableUUID represents a UUPSUnsupportedProxiableUUID error raised by the STCoordinator contract.
type STCoordinatorUUPSUnsupportedProxiableUUID struct {
	Slot [32]byte
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error UUPSUnsupportedProxiableUUID(bytes32 slot)
func STCoordinatorUUPSUnsupportedProxiableUUIDErrorID() common.Hash {
	return common.HexToHash("0xaa1d49a4c084bfa9aeeee2a0be65267a7f19ba7e1476b114dac513d2c14cb563")
}

// UnpackUUPSUnsupportedProxiableUUIDError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error UUPSUnsupportedProxiableUUID(bytes32 slot)
func (sTCoordinator *STCoordinator) UnpackUUPSUnsupportedProxiableUUIDError(raw []byte) (*STCoordinatorUUPSUnsupportedProxiableUUID, error) {
	out := new(STCoordinatorUUPSUnsupportedProxiableUUID)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "UUPSUnsupportedProxiableUUID", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorUnauthorized represents a Unauthorized error raised by the STCoordinator contract.
type STCoordinatorUnauthorized struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error Unauthorized()
func STCoordinatorUnauthorizedErrorID() common.Hash {
	return common.HexToHash("0x82b4290015f7ec7256ca2a6247d3c2a89c4865c0e791456df195f40ad0a81367")
}

// UnpackUnauthorizedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error Unauthorized()
func (sTCoordinator *STCoordinator) UnpackUnauthorizedError(raw []byte) (*STCoordinatorUnauthorized, error) {
	out := new(STCoordinatorUnauthorized)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "Unauthorized", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STCoordinatorUnknownOperator represents a UnknownOperator error raised by the STCoordinator contract.
type STCoordinatorUnknownOperator struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error UnknownOperator()
func STCoordinatorUnknownOperatorErrorID() common.Hash {
	return common.HexToHash("0x31158d11195648ada01ccf50424897a5161fc1dccdde451fdd37513704cd7744")
}

// UnpackUnknownOperatorError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error UnknownOperator()
func (sTCoordinator *STCoordinator) UnpackUnknownOperatorError(raw []byte) (*STCoordinatorUnknownOperator, error) {
	out := new(STCoordinatorUnknownOperator)
	if err := sTCoordinator.abi.UnpackIntoInterface(out, "UnknownOperator", raw); err != nil {
		return nil, err
	}
	return out, nil
}
