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

// STSubnetMetaData contains all meta data concerning the STSubnet contract.
var STSubnetMetaData = bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"receive\",\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"BPS\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"HEAD_BIND_DOMAIN\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MAX_ROLLS_PER_CALL\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"UPGRADE_INTERFACE_VERSION\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"accountedStake\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"bindHead\",\"inputs\":[{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"clientId\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"clientIdSig\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"buybackTotal\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"carry\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"claimMiner\",\"inputs\":[{\"name\":\"e\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"coldkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"shareBps\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"proof\",\"type\":\"bytes32[]\",\"internalType\":\"bytes32[]\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"claimedMiner\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"commitOperator\",\"inputs\":[{\"name\":\"e\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"payoutRoot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"off\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"commitWindowBlocks\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"deposit\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"alphaAmount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"epoch\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"epochCloseBlock\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"epochStartBlock\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"finalizeEpoch\",\"inputs\":[{\"name\":\"e\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"finalizeOffsetBlocks\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"finalized\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"guardian\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"headBindDigest\",\"inputs\":[{\"name\":\"registrant\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"clientId\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"headClientIdToHotkey\",\"inputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"headHotkeyToClientId\",\"inputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"initialize\",\"inputs\":[{\"name\":\"netuid_\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"owner_\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"guardian_\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"treasuryHotkey_\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"reserveHotkey_\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"tEpoch_\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"commitWindowBlocks_\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"trailsWindowBlocks_\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"finalizeOffsetBlocks_\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"selfColdkey_\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"minerClaimedBy\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"minerHotkeyUsed\",\"inputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"minerLeafHash\",\"inputs\":[{\"name\":\"coldkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"shareBps\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"pure\"},{\"type\":\"function\",\"name\":\"netuid\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint16\",\"internalType\":\"uint16\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"nextFinalizeEpoch\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"noCommit\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"payoutRoot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"off\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"operatorAddress\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"operatorCount\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"operatorIds\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"operators\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"coldkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"minerUid\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"minerHotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"active\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"owner\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"paused\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"pendingEpoch\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"poolAccrued\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"poolBaseline\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"poolEmission\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"poolTotal\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"proxiableUUID\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"registerOperator\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"coldkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"minerHotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"renounceOwnership\",\"inputs\":[],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"reserveHotkey\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"rollEpochs\",\"inputs\":[],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"selfColdkey\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"setEpochParams\",\"inputs\":[{\"name\":\"tEpoch_\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"commitWindowBlocks_\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"trailsWindowBlocks_\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"finalizeOffsetBlocks_\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"setGuardian\",\"inputs\":[{\"name\":\"guardian_\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"setOperatorActive\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"active\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"setOperatorAddress\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"addr\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"setPaused\",\"inputs\":[{\"name\":\"paused_\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"setSelfColdkey\",\"inputs\":[{\"name\":\"selfColdkey_\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"sweepPool\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"tEpoch\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"trailsWindowBlocks\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"transferOwnership\",\"inputs\":[{\"name\":\"newOwner\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"treasuryHotkey\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"unbindHead\",\"inputs\":[{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"upgradeToAndCall\",\"inputs\":[{\"name\":\"newImplementation\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"payable\"},{\"type\":\"event\",\"name\":\"BuybackReserved\",\"inputs\":[{\"name\":\"e\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"buybackTotal\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Deposited\",\"inputs\":[{\"name\":\"e\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"from\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EpochFinalized\",\"inputs\":[{\"name\":\"e\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EpochParamsSet\",\"inputs\":[{\"name\":\"tEpoch\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"},{\"name\":\"commitWindowBlocks\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"},{\"name\":\"trailsWindowBlocks\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"},{\"name\":\"finalizeOffsetBlocks\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EpochRolled\",\"inputs\":[{\"name\":\"closedEpoch\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"newEpoch\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"closeBlock\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"GuardianSet\",\"inputs\":[{\"name\":\"guardian\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"HeadBound\",\"inputs\":[{\"name\":\"hotkey\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"clientId\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"uid\",\"type\":\"uint16\",\"indexed\":false,\"internalType\":\"uint16\"},{\"name\":\"registrant\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"HeadUnbound\",\"inputs\":[{\"name\":\"hotkey\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"clientId\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"uid\",\"type\":\"uint16\",\"indexed\":false,\"internalType\":\"uint16\"},{\"name\":\"registrant\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Initialized\",\"inputs\":[{\"name\":\"version\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"MinerClaimed\",\"inputs\":[{\"name\":\"e\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"coldkey\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"shareBps\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"caller\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OperatorActiveSet\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"active\",\"type\":\"bool\",\"indexed\":false,\"internalType\":\"bool\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OperatorAddressSet\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"addr\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OperatorCommitted\",\"inputs\":[{\"name\":\"e\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"payoutRoot\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"off\",\"type\":\"bytes\",\"indexed\":false,\"internalType\":\"bytes\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OperatorRegistered\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"coldkey\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"minerUid\",\"type\":\"uint16\",\"indexed\":false,\"internalType\":\"uint16\"},{\"name\":\"minerHotkey\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OwnershipTransferred\",\"inputs\":[{\"name\":\"previousOwner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"newOwner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"PausedSet\",\"inputs\":[{\"name\":\"paused\",\"type\":\"bool\",\"indexed\":false,\"internalType\":\"bool\"},{\"name\":\"by\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"PoolCarried\",\"inputs\":[{\"name\":\"e\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"carried\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"PoolFinalized\",\"inputs\":[{\"name\":\"e\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"poolTotal\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"PoolSwept\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"measured\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"swept\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"moveOk\",\"type\":\"bool\",\"indexed\":false,\"internalType\":\"bool\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"SelfColdkeySet\",\"inputs\":[{\"name\":\"selfColdkey\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Upgraded\",\"inputs\":[{\"name\":\"implementation\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"AddressEmptyCode\",\"inputs\":[{\"name\":\"target\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ERC1967InvalidImplementation\",\"inputs\":[{\"name\":\"implementation\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ERC1967NonPayable\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"FailedCall\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidInitialization\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"NotInitializing\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"OwnableInvalidOwner\",\"inputs\":[{\"name\":\"owner\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"OwnableUnauthorizedAccount\",\"inputs\":[{\"name\":\"account\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"UUPSUnauthorizedCallContext\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"UUPSUnsupportedProxiableUUID\",\"inputs\":[{\"name\":\"slot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}]}]",
	ID:  "STSubnet",
}

// STSubnet is an auto generated Go binding around an Ethereum contract.
type STSubnet struct {
	abi abi.ABI
}

// NewSTSubnet creates a new instance of STSubnet.
func NewSTSubnet() *STSubnet {
	parsed, err := STSubnetMetaData.ParseABI()
	if err != nil {
		panic(errors.New("invalid ABI: " + err.Error()))
	}
	return &STSubnet{abi: *parsed}
}

// Instance creates a wrapper for a deployed contract instance at the given address.
// Use this to create the instance object passed to abigen v2 library functions Call, Transact, etc.
func (c *STSubnet) Instance(backend bind.ContractBackend, addr common.Address) *bind.BoundContract {
	return bind.NewBoundContract(addr, c.abi, backend, backend, backend)
}

// PackBPS is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x249d39e9.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function BPS() view returns(uint256)
func (sTSubnet *STSubnet) PackBPS() []byte {
	enc, err := sTSubnet.abi.Pack("BPS")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackBPS is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x249d39e9.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function BPS() view returns(uint256)
func (sTSubnet *STSubnet) TryPackBPS() ([]byte, error) {
	return sTSubnet.abi.Pack("BPS")
}

// UnpackBPS is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x249d39e9.
//
// Solidity: function BPS() view returns(uint256)
func (sTSubnet *STSubnet) UnpackBPS(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("BPS", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackHEADBINDDOMAIN is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x46d7f3fe.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function HEAD_BIND_DOMAIN() view returns(bytes32)
func (sTSubnet *STSubnet) PackHEADBINDDOMAIN() []byte {
	enc, err := sTSubnet.abi.Pack("HEAD_BIND_DOMAIN")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackHEADBINDDOMAIN is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x46d7f3fe.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function HEAD_BIND_DOMAIN() view returns(bytes32)
func (sTSubnet *STSubnet) TryPackHEADBINDDOMAIN() ([]byte, error) {
	return sTSubnet.abi.Pack("HEAD_BIND_DOMAIN")
}

// UnpackHEADBINDDOMAIN is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x46d7f3fe.
//
// Solidity: function HEAD_BIND_DOMAIN() view returns(bytes32)
func (sTSubnet *STSubnet) UnpackHEADBINDDOMAIN(data []byte) ([32]byte, error) {
	out, err := sTSubnet.abi.Unpack("HEAD_BIND_DOMAIN", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackMAXROLLSPERCALL is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x8b923488.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function MAX_ROLLS_PER_CALL() view returns(uint256)
func (sTSubnet *STSubnet) PackMAXROLLSPERCALL() []byte {
	enc, err := sTSubnet.abi.Pack("MAX_ROLLS_PER_CALL")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackMAXROLLSPERCALL is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x8b923488.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function MAX_ROLLS_PER_CALL() view returns(uint256)
func (sTSubnet *STSubnet) TryPackMAXROLLSPERCALL() ([]byte, error) {
	return sTSubnet.abi.Pack("MAX_ROLLS_PER_CALL")
}

// UnpackMAXROLLSPERCALL is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x8b923488.
//
// Solidity: function MAX_ROLLS_PER_CALL() view returns(uint256)
func (sTSubnet *STSubnet) UnpackMAXROLLSPERCALL(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("MAX_ROLLS_PER_CALL", data)
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
func (sTSubnet *STSubnet) PackUPGRADEINTERFACEVERSION() []byte {
	enc, err := sTSubnet.abi.Pack("UPGRADE_INTERFACE_VERSION")
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
func (sTSubnet *STSubnet) TryPackUPGRADEINTERFACEVERSION() ([]byte, error) {
	return sTSubnet.abi.Pack("UPGRADE_INTERFACE_VERSION")
}

// UnpackUPGRADEINTERFACEVERSION is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xad3cb1cc.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (sTSubnet *STSubnet) UnpackUPGRADEINTERFACEVERSION(data []byte) (string, error) {
	out, err := sTSubnet.abi.Unpack("UPGRADE_INTERFACE_VERSION", data)
	if err != nil {
		return *new(string), err
	}
	out0 := *abi.ConvertType(out[0], new(string)).(*string)
	return out0, nil
}

// PackAccountedStake is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x311ec096.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function accountedStake() view returns(uint256)
func (sTSubnet *STSubnet) PackAccountedStake() []byte {
	enc, err := sTSubnet.abi.Pack("accountedStake")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackAccountedStake is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x311ec096.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function accountedStake() view returns(uint256)
func (sTSubnet *STSubnet) TryPackAccountedStake() ([]byte, error) {
	return sTSubnet.abi.Pack("accountedStake")
}

// UnpackAccountedStake is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x311ec096.
//
// Solidity: function accountedStake() view returns(uint256)
func (sTSubnet *STSubnet) UnpackAccountedStake(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("accountedStake", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackBindHead is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x55d756e7.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function bindHead(bytes32 hotkey, bytes32 clientId, bytes clientIdSig) returns()
func (sTSubnet *STSubnet) PackBindHead(hotkey [32]byte, clientId [32]byte, clientIdSig []byte) []byte {
	enc, err := sTSubnet.abi.Pack("bindHead", hotkey, clientId, clientIdSig)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackBindHead is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x55d756e7.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function bindHead(bytes32 hotkey, bytes32 clientId, bytes clientIdSig) returns()
func (sTSubnet *STSubnet) TryPackBindHead(hotkey [32]byte, clientId [32]byte, clientIdSig []byte) ([]byte, error) {
	return sTSubnet.abi.Pack("bindHead", hotkey, clientId, clientIdSig)
}

// PackBuybackTotal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x07d62be8.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function buybackTotal() view returns(uint256)
func (sTSubnet *STSubnet) PackBuybackTotal() []byte {
	enc, err := sTSubnet.abi.Pack("buybackTotal")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackBuybackTotal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x07d62be8.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function buybackTotal() view returns(uint256)
func (sTSubnet *STSubnet) TryPackBuybackTotal() ([]byte, error) {
	return sTSubnet.abi.Pack("buybackTotal")
}

// UnpackBuybackTotal is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x07d62be8.
//
// Solidity: function buybackTotal() view returns(uint256)
func (sTSubnet *STSubnet) UnpackBuybackTotal(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("buybackTotal", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackCarry is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x044964ea.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function carry(uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) PackCarry(arg0 *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("carry", arg0)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCarry is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x044964ea.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function carry(uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) TryPackCarry(arg0 *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("carry", arg0)
}

// UnpackCarry is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x044964ea.
//
// Solidity: function carry(uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) UnpackCarry(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("carry", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackClaimMiner is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4c207962.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function claimMiner(uint256 e, uint256 noId, bytes32 coldkey, uint256 shareBps, bytes32[] proof) returns()
func (sTSubnet *STSubnet) PackClaimMiner(e *big.Int, noId *big.Int, coldkey [32]byte, shareBps *big.Int, proof [][32]byte) []byte {
	enc, err := sTSubnet.abi.Pack("claimMiner", e, noId, coldkey, shareBps, proof)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackClaimMiner is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4c207962.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function claimMiner(uint256 e, uint256 noId, bytes32 coldkey, uint256 shareBps, bytes32[] proof) returns()
func (sTSubnet *STSubnet) TryPackClaimMiner(e *big.Int, noId *big.Int, coldkey [32]byte, shareBps *big.Int, proof [][32]byte) ([]byte, error) {
	return sTSubnet.abi.Pack("claimMiner", e, noId, coldkey, shareBps, proof)
}

// PackClaimedMiner is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe7da9a82.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function claimedMiner(uint256 , uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) PackClaimedMiner(arg0 *big.Int, arg1 *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("claimedMiner", arg0, arg1)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackClaimedMiner is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe7da9a82.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function claimedMiner(uint256 , uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) TryPackClaimedMiner(arg0 *big.Int, arg1 *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("claimedMiner", arg0, arg1)
}

// UnpackClaimedMiner is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xe7da9a82.
//
// Solidity: function claimedMiner(uint256 , uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) UnpackClaimedMiner(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("claimedMiner", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackCommitOperator is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x751af220.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function commitOperator(uint256 e, uint256 noId, bytes32 payoutRoot, bytes off) returns()
func (sTSubnet *STSubnet) PackCommitOperator(e *big.Int, noId *big.Int, payoutRoot [32]byte, off []byte) []byte {
	enc, err := sTSubnet.abi.Pack("commitOperator", e, noId, payoutRoot, off)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCommitOperator is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x751af220.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function commitOperator(uint256 e, uint256 noId, bytes32 payoutRoot, bytes off) returns()
func (sTSubnet *STSubnet) TryPackCommitOperator(e *big.Int, noId *big.Int, payoutRoot [32]byte, off []byte) ([]byte, error) {
	return sTSubnet.abi.Pack("commitOperator", e, noId, payoutRoot, off)
}

// PackCommitWindowBlocks is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xec9e1305.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function commitWindowBlocks() view returns(uint64)
func (sTSubnet *STSubnet) PackCommitWindowBlocks() []byte {
	enc, err := sTSubnet.abi.Pack("commitWindowBlocks")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCommitWindowBlocks is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xec9e1305.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function commitWindowBlocks() view returns(uint64)
func (sTSubnet *STSubnet) TryPackCommitWindowBlocks() ([]byte, error) {
	return sTSubnet.abi.Pack("commitWindowBlocks")
}

// UnpackCommitWindowBlocks is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xec9e1305.
//
// Solidity: function commitWindowBlocks() view returns(uint64)
func (sTSubnet *STSubnet) UnpackCommitWindowBlocks(data []byte) (uint64, error) {
	out, err := sTSubnet.abi.Unpack("commitWindowBlocks", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackDeposit is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe2bbb158.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function deposit(uint256 noId, uint256 alphaAmount) returns()
func (sTSubnet *STSubnet) PackDeposit(noId *big.Int, alphaAmount *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("deposit", noId, alphaAmount)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackDeposit is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe2bbb158.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function deposit(uint256 noId, uint256 alphaAmount) returns()
func (sTSubnet *STSubnet) TryPackDeposit(noId *big.Int, alphaAmount *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("deposit", noId, alphaAmount)
}

// PackEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x900cf0cf.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function epoch() view returns(uint256)
func (sTSubnet *STSubnet) PackEpoch() []byte {
	enc, err := sTSubnet.abi.Pack("epoch")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x900cf0cf.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function epoch() view returns(uint256)
func (sTSubnet *STSubnet) TryPackEpoch() ([]byte, error) {
	return sTSubnet.abi.Pack("epoch")
}

// UnpackEpoch is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x900cf0cf.
//
// Solidity: function epoch() view returns(uint256)
func (sTSubnet *STSubnet) UnpackEpoch(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("epoch", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackEpochCloseBlock is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x3d55b0e9.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function epochCloseBlock(uint256 ) view returns(uint64)
func (sTSubnet *STSubnet) PackEpochCloseBlock(arg0 *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("epochCloseBlock", arg0)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackEpochCloseBlock is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x3d55b0e9.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function epochCloseBlock(uint256 ) view returns(uint64)
func (sTSubnet *STSubnet) TryPackEpochCloseBlock(arg0 *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("epochCloseBlock", arg0)
}

// UnpackEpochCloseBlock is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x3d55b0e9.
//
// Solidity: function epochCloseBlock(uint256 ) view returns(uint64)
func (sTSubnet *STSubnet) UnpackEpochCloseBlock(data []byte) (uint64, error) {
	out, err := sTSubnet.abi.Unpack("epochCloseBlock", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackEpochStartBlock is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x3ed55b7b.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function epochStartBlock() view returns(uint64)
func (sTSubnet *STSubnet) PackEpochStartBlock() []byte {
	enc, err := sTSubnet.abi.Pack("epochStartBlock")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackEpochStartBlock is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x3ed55b7b.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function epochStartBlock() view returns(uint64)
func (sTSubnet *STSubnet) TryPackEpochStartBlock() ([]byte, error) {
	return sTSubnet.abi.Pack("epochStartBlock")
}

// UnpackEpochStartBlock is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x3ed55b7b.
//
// Solidity: function epochStartBlock() view returns(uint64)
func (sTSubnet *STSubnet) UnpackEpochStartBlock(data []byte) (uint64, error) {
	out, err := sTSubnet.abi.Unpack("epochStartBlock", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackFinalizeEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x986bce2c.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function finalizeEpoch(uint256 e) returns()
func (sTSubnet *STSubnet) PackFinalizeEpoch(e *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("finalizeEpoch", e)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackFinalizeEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x986bce2c.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function finalizeEpoch(uint256 e) returns()
func (sTSubnet *STSubnet) TryPackFinalizeEpoch(e *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("finalizeEpoch", e)
}

// PackFinalizeOffsetBlocks is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xb5415fcb.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function finalizeOffsetBlocks() view returns(uint64)
func (sTSubnet *STSubnet) PackFinalizeOffsetBlocks() []byte {
	enc, err := sTSubnet.abi.Pack("finalizeOffsetBlocks")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackFinalizeOffsetBlocks is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xb5415fcb.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function finalizeOffsetBlocks() view returns(uint64)
func (sTSubnet *STSubnet) TryPackFinalizeOffsetBlocks() ([]byte, error) {
	return sTSubnet.abi.Pack("finalizeOffsetBlocks")
}

// UnpackFinalizeOffsetBlocks is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xb5415fcb.
//
// Solidity: function finalizeOffsetBlocks() view returns(uint64)
func (sTSubnet *STSubnet) UnpackFinalizeOffsetBlocks(data []byte) (uint64, error) {
	out, err := sTSubnet.abi.Unpack("finalizeOffsetBlocks", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackFinalized is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4ddaced2.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function finalized(uint256 ) view returns(bool)
func (sTSubnet *STSubnet) PackFinalized(arg0 *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("finalized", arg0)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackFinalized is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4ddaced2.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function finalized(uint256 ) view returns(bool)
func (sTSubnet *STSubnet) TryPackFinalized(arg0 *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("finalized", arg0)
}

// UnpackFinalized is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x4ddaced2.
//
// Solidity: function finalized(uint256 ) view returns(bool)
func (sTSubnet *STSubnet) UnpackFinalized(data []byte) (bool, error) {
	out, err := sTSubnet.abi.Unpack("finalized", data)
	if err != nil {
		return *new(bool), err
	}
	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)
	return out0, nil
}

// PackGuardian is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x452a9320.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function guardian() view returns(address)
func (sTSubnet *STSubnet) PackGuardian() []byte {
	enc, err := sTSubnet.abi.Pack("guardian")
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
func (sTSubnet *STSubnet) TryPackGuardian() ([]byte, error) {
	return sTSubnet.abi.Pack("guardian")
}

// UnpackGuardian is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x452a9320.
//
// Solidity: function guardian() view returns(address)
func (sTSubnet *STSubnet) UnpackGuardian(data []byte) (common.Address, error) {
	out, err := sTSubnet.abi.Unpack("guardian", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackHeadBindDigest is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x850f7cb3.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function headBindDigest(address registrant, bytes32 hotkey, bytes32 clientId) view returns(bytes32)
func (sTSubnet *STSubnet) PackHeadBindDigest(registrant common.Address, hotkey [32]byte, clientId [32]byte) []byte {
	enc, err := sTSubnet.abi.Pack("headBindDigest", registrant, hotkey, clientId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackHeadBindDigest is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x850f7cb3.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function headBindDigest(address registrant, bytes32 hotkey, bytes32 clientId) view returns(bytes32)
func (sTSubnet *STSubnet) TryPackHeadBindDigest(registrant common.Address, hotkey [32]byte, clientId [32]byte) ([]byte, error) {
	return sTSubnet.abi.Pack("headBindDigest", registrant, hotkey, clientId)
}

// UnpackHeadBindDigest is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x850f7cb3.
//
// Solidity: function headBindDigest(address registrant, bytes32 hotkey, bytes32 clientId) view returns(bytes32)
func (sTSubnet *STSubnet) UnpackHeadBindDigest(data []byte) ([32]byte, error) {
	out, err := sTSubnet.abi.Unpack("headBindDigest", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackHeadClientIdToHotkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x3154ab68.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function headClientIdToHotkey(bytes32 ) view returns(bytes32)
func (sTSubnet *STSubnet) PackHeadClientIdToHotkey(arg0 [32]byte) []byte {
	enc, err := sTSubnet.abi.Pack("headClientIdToHotkey", arg0)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackHeadClientIdToHotkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x3154ab68.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function headClientIdToHotkey(bytes32 ) view returns(bytes32)
func (sTSubnet *STSubnet) TryPackHeadClientIdToHotkey(arg0 [32]byte) ([]byte, error) {
	return sTSubnet.abi.Pack("headClientIdToHotkey", arg0)
}

// UnpackHeadClientIdToHotkey is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x3154ab68.
//
// Solidity: function headClientIdToHotkey(bytes32 ) view returns(bytes32)
func (sTSubnet *STSubnet) UnpackHeadClientIdToHotkey(data []byte) ([32]byte, error) {
	out, err := sTSubnet.abi.Unpack("headClientIdToHotkey", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackHeadHotkeyToClientId is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x190fa383.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function headHotkeyToClientId(bytes32 ) view returns(bytes32)
func (sTSubnet *STSubnet) PackHeadHotkeyToClientId(arg0 [32]byte) []byte {
	enc, err := sTSubnet.abi.Pack("headHotkeyToClientId", arg0)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackHeadHotkeyToClientId is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x190fa383.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function headHotkeyToClientId(bytes32 ) view returns(bytes32)
func (sTSubnet *STSubnet) TryPackHeadHotkeyToClientId(arg0 [32]byte) ([]byte, error) {
	return sTSubnet.abi.Pack("headHotkeyToClientId", arg0)
}

// UnpackHeadHotkeyToClientId is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x190fa383.
//
// Solidity: function headHotkeyToClientId(bytes32 ) view returns(bytes32)
func (sTSubnet *STSubnet) UnpackHeadHotkeyToClientId(data []byte) ([32]byte, error) {
	out, err := sTSubnet.abi.Unpack("headHotkeyToClientId", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackInitialize is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xd7a9b3db.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function initialize(uint16 netuid_, address owner_, address guardian_, bytes32 treasuryHotkey_, bytes32 reserveHotkey_, uint64 tEpoch_, uint64 commitWindowBlocks_, uint64 trailsWindowBlocks_, uint64 finalizeOffsetBlocks_, bytes32 selfColdkey_) returns()
func (sTSubnet *STSubnet) PackInitialize(netuid uint16, owner common.Address, guardian common.Address, treasuryHotkey [32]byte, reserveHotkey [32]byte, tEpoch uint64, commitWindowBlocks uint64, trailsWindowBlocks uint64, finalizeOffsetBlocks uint64, selfColdkey [32]byte) []byte {
	enc, err := sTSubnet.abi.Pack("initialize", netuid, owner, guardian, treasuryHotkey, reserveHotkey, tEpoch, commitWindowBlocks, trailsWindowBlocks, finalizeOffsetBlocks, selfColdkey)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackInitialize is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xd7a9b3db.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function initialize(uint16 netuid_, address owner_, address guardian_, bytes32 treasuryHotkey_, bytes32 reserveHotkey_, uint64 tEpoch_, uint64 commitWindowBlocks_, uint64 trailsWindowBlocks_, uint64 finalizeOffsetBlocks_, bytes32 selfColdkey_) returns()
func (sTSubnet *STSubnet) TryPackInitialize(netuid uint16, owner common.Address, guardian common.Address, treasuryHotkey [32]byte, reserveHotkey [32]byte, tEpoch uint64, commitWindowBlocks uint64, trailsWindowBlocks uint64, finalizeOffsetBlocks uint64, selfColdkey [32]byte) ([]byte, error) {
	return sTSubnet.abi.Pack("initialize", netuid, owner, guardian, treasuryHotkey, reserveHotkey, tEpoch, commitWindowBlocks, trailsWindowBlocks, finalizeOffsetBlocks, selfColdkey)
}

// PackMinerClaimedBy is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xebec6bdd.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function minerClaimedBy(uint256 , bytes32 ) view returns(bool)
func (sTSubnet *STSubnet) PackMinerClaimedBy(arg0 *big.Int, arg1 [32]byte) []byte {
	enc, err := sTSubnet.abi.Pack("minerClaimedBy", arg0, arg1)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackMinerClaimedBy is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xebec6bdd.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function minerClaimedBy(uint256 , bytes32 ) view returns(bool)
func (sTSubnet *STSubnet) TryPackMinerClaimedBy(arg0 *big.Int, arg1 [32]byte) ([]byte, error) {
	return sTSubnet.abi.Pack("minerClaimedBy", arg0, arg1)
}

// UnpackMinerClaimedBy is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xebec6bdd.
//
// Solidity: function minerClaimedBy(uint256 , bytes32 ) view returns(bool)
func (sTSubnet *STSubnet) UnpackMinerClaimedBy(data []byte) (bool, error) {
	out, err := sTSubnet.abi.Unpack("minerClaimedBy", data)
	if err != nil {
		return *new(bool), err
	}
	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)
	return out0, nil
}

// PackMinerHotkeyUsed is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc8c2818c.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function minerHotkeyUsed(bytes32 ) view returns(bool)
func (sTSubnet *STSubnet) PackMinerHotkeyUsed(arg0 [32]byte) []byte {
	enc, err := sTSubnet.abi.Pack("minerHotkeyUsed", arg0)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackMinerHotkeyUsed is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc8c2818c.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function minerHotkeyUsed(bytes32 ) view returns(bool)
func (sTSubnet *STSubnet) TryPackMinerHotkeyUsed(arg0 [32]byte) ([]byte, error) {
	return sTSubnet.abi.Pack("minerHotkeyUsed", arg0)
}

// UnpackMinerHotkeyUsed is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xc8c2818c.
//
// Solidity: function minerHotkeyUsed(bytes32 ) view returns(bool)
func (sTSubnet *STSubnet) UnpackMinerHotkeyUsed(data []byte) (bool, error) {
	out, err := sTSubnet.abi.Unpack("minerHotkeyUsed", data)
	if err != nil {
		return *new(bool), err
	}
	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)
	return out0, nil
}

// PackMinerLeafHash is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x238ecabf.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function minerLeafHash(bytes32 coldkey, uint256 shareBps) pure returns(bytes32)
func (sTSubnet *STSubnet) PackMinerLeafHash(coldkey [32]byte, shareBps *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("minerLeafHash", coldkey, shareBps)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackMinerLeafHash is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x238ecabf.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function minerLeafHash(bytes32 coldkey, uint256 shareBps) pure returns(bytes32)
func (sTSubnet *STSubnet) TryPackMinerLeafHash(coldkey [32]byte, shareBps *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("minerLeafHash", coldkey, shareBps)
}

// UnpackMinerLeafHash is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x238ecabf.
//
// Solidity: function minerLeafHash(bytes32 coldkey, uint256 shareBps) pure returns(bytes32)
func (sTSubnet *STSubnet) UnpackMinerLeafHash(data []byte) ([32]byte, error) {
	out, err := sTSubnet.abi.Unpack("minerLeafHash", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackNetuid is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe78015b1.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function netuid() view returns(uint16)
func (sTSubnet *STSubnet) PackNetuid() []byte {
	enc, err := sTSubnet.abi.Pack("netuid")
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
func (sTSubnet *STSubnet) TryPackNetuid() ([]byte, error) {
	return sTSubnet.abi.Pack("netuid")
}

// UnpackNetuid is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xe78015b1.
//
// Solidity: function netuid() view returns(uint16)
func (sTSubnet *STSubnet) UnpackNetuid(data []byte) (uint16, error) {
	out, err := sTSubnet.abi.Unpack("netuid", data)
	if err != nil {
		return *new(uint16), err
	}
	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)
	return out0, nil
}

// PackNextFinalizeEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf0785a2f.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function nextFinalizeEpoch() view returns(uint256)
func (sTSubnet *STSubnet) PackNextFinalizeEpoch() []byte {
	enc, err := sTSubnet.abi.Pack("nextFinalizeEpoch")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackNextFinalizeEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf0785a2f.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function nextFinalizeEpoch() view returns(uint256)
func (sTSubnet *STSubnet) TryPackNextFinalizeEpoch() ([]byte, error) {
	return sTSubnet.abi.Pack("nextFinalizeEpoch")
}

// UnpackNextFinalizeEpoch is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xf0785a2f.
//
// Solidity: function nextFinalizeEpoch() view returns(uint256)
func (sTSubnet *STSubnet) UnpackNextFinalizeEpoch(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("nextFinalizeEpoch", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackNoCommit is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc477c60f.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function noCommit(uint256 , uint256 ) view returns(bytes32 payoutRoot, bytes off)
func (sTSubnet *STSubnet) PackNoCommit(arg0 *big.Int, arg1 *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("noCommit", arg0, arg1)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackNoCommit is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc477c60f.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function noCommit(uint256 , uint256 ) view returns(bytes32 payoutRoot, bytes off)
func (sTSubnet *STSubnet) TryPackNoCommit(arg0 *big.Int, arg1 *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("noCommit", arg0, arg1)
}

// NoCommitOutput serves as a container for the return parameters of contract
// method NoCommit.
type NoCommitOutput struct {
	PayoutRoot [32]byte
	Off        []byte
}

// UnpackNoCommit is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xc477c60f.
//
// Solidity: function noCommit(uint256 , uint256 ) view returns(bytes32 payoutRoot, bytes off)
func (sTSubnet *STSubnet) UnpackNoCommit(data []byte) (NoCommitOutput, error) {
	out, err := sTSubnet.abi.Unpack("noCommit", data)
	outstruct := new(NoCommitOutput)
	if err != nil {
		return *outstruct, err
	}
	outstruct.PayoutRoot = *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	outstruct.Off = *abi.ConvertType(out[1], new([]byte)).(*[]byte)
	return *outstruct, nil
}

// PackOperatorAddress is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x201f6754.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function operatorAddress(uint256 ) view returns(address)
func (sTSubnet *STSubnet) PackOperatorAddress(arg0 *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("operatorAddress", arg0)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackOperatorAddress is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x201f6754.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function operatorAddress(uint256 ) view returns(address)
func (sTSubnet *STSubnet) TryPackOperatorAddress(arg0 *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("operatorAddress", arg0)
}

// UnpackOperatorAddress is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x201f6754.
//
// Solidity: function operatorAddress(uint256 ) view returns(address)
func (sTSubnet *STSubnet) UnpackOperatorAddress(data []byte) (common.Address, error) {
	out, err := sTSubnet.abi.Unpack("operatorAddress", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackOperatorCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x7c6f3158.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function operatorCount() view returns(uint256)
func (sTSubnet *STSubnet) PackOperatorCount() []byte {
	enc, err := sTSubnet.abi.Pack("operatorCount")
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
func (sTSubnet *STSubnet) TryPackOperatorCount() ([]byte, error) {
	return sTSubnet.abi.Pack("operatorCount")
}

// UnpackOperatorCount is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x7c6f3158.
//
// Solidity: function operatorCount() view returns(uint256)
func (sTSubnet *STSubnet) UnpackOperatorCount(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("operatorCount", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackOperatorIds is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xbfbdaffd.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function operatorIds(uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) PackOperatorIds(arg0 *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("operatorIds", arg0)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackOperatorIds is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xbfbdaffd.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function operatorIds(uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) TryPackOperatorIds(arg0 *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("operatorIds", arg0)
}

// UnpackOperatorIds is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xbfbdaffd.
//
// Solidity: function operatorIds(uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) UnpackOperatorIds(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("operatorIds", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackOperators is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe28d4906.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function operators(uint256 ) view returns(bytes32 coldkey, uint16 minerUid, bytes32 minerHotkey, bool active)
func (sTSubnet *STSubnet) PackOperators(arg0 *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("operators", arg0)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackOperators is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe28d4906.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function operators(uint256 ) view returns(bytes32 coldkey, uint16 minerUid, bytes32 minerHotkey, bool active)
func (sTSubnet *STSubnet) TryPackOperators(arg0 *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("operators", arg0)
}

// OperatorsOutput serves as a container for the return parameters of contract
// method Operators.
type OperatorsOutput struct {
	Coldkey     [32]byte
	MinerUid    uint16
	MinerHotkey [32]byte
	Active      bool
}

// UnpackOperators is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xe28d4906.
//
// Solidity: function operators(uint256 ) view returns(bytes32 coldkey, uint16 minerUid, bytes32 minerHotkey, bool active)
func (sTSubnet *STSubnet) UnpackOperators(data []byte) (OperatorsOutput, error) {
	out, err := sTSubnet.abi.Unpack("operators", data)
	outstruct := new(OperatorsOutput)
	if err != nil {
		return *outstruct, err
	}
	outstruct.Coldkey = *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	outstruct.MinerUid = *abi.ConvertType(out[1], new(uint16)).(*uint16)
	outstruct.MinerHotkey = *abi.ConvertType(out[2], new([32]byte)).(*[32]byte)
	outstruct.Active = *abi.ConvertType(out[3], new(bool)).(*bool)
	return *outstruct, nil
}

// PackOwner is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x8da5cb5b.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function owner() view returns(address)
func (sTSubnet *STSubnet) PackOwner() []byte {
	enc, err := sTSubnet.abi.Pack("owner")
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
func (sTSubnet *STSubnet) TryPackOwner() ([]byte, error) {
	return sTSubnet.abi.Pack("owner")
}

// UnpackOwner is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (sTSubnet *STSubnet) UnpackOwner(data []byte) (common.Address, error) {
	out, err := sTSubnet.abi.Unpack("owner", data)
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
func (sTSubnet *STSubnet) PackPaused() []byte {
	enc, err := sTSubnet.abi.Pack("paused")
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
func (sTSubnet *STSubnet) TryPackPaused() ([]byte, error) {
	return sTSubnet.abi.Pack("paused")
}

// UnpackPaused is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x5c975abb.
//
// Solidity: function paused() view returns(bool)
func (sTSubnet *STSubnet) UnpackPaused(data []byte) (bool, error) {
	out, err := sTSubnet.abi.Unpack("paused", data)
	if err != nil {
		return *new(bool), err
	}
	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)
	return out0, nil
}

// PackPendingEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf552501a.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function pendingEpoch() view returns(uint256)
func (sTSubnet *STSubnet) PackPendingEpoch() []byte {
	enc, err := sTSubnet.abi.Pack("pendingEpoch")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPendingEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf552501a.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function pendingEpoch() view returns(uint256)
func (sTSubnet *STSubnet) TryPackPendingEpoch() ([]byte, error) {
	return sTSubnet.abi.Pack("pendingEpoch")
}

// UnpackPendingEpoch is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xf552501a.
//
// Solidity: function pendingEpoch() view returns(uint256)
func (sTSubnet *STSubnet) UnpackPendingEpoch(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("pendingEpoch", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackPoolAccrued is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xdf579965.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function poolAccrued(uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) PackPoolAccrued(arg0 *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("poolAccrued", arg0)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPoolAccrued is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xdf579965.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function poolAccrued(uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) TryPackPoolAccrued(arg0 *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("poolAccrued", arg0)
}

// UnpackPoolAccrued is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xdf579965.
//
// Solidity: function poolAccrued(uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) UnpackPoolAccrued(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("poolAccrued", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackPoolBaseline is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x15e3d0c2.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function poolBaseline(uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) PackPoolBaseline(arg0 *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("poolBaseline", arg0)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPoolBaseline is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x15e3d0c2.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function poolBaseline(uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) TryPackPoolBaseline(arg0 *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("poolBaseline", arg0)
}

// UnpackPoolBaseline is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x15e3d0c2.
//
// Solidity: function poolBaseline(uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) UnpackPoolBaseline(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("poolBaseline", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackPoolEmission is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x28491fb8.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function poolEmission(uint256 , uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) PackPoolEmission(arg0 *big.Int, arg1 *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("poolEmission", arg0, arg1)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPoolEmission is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x28491fb8.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function poolEmission(uint256 , uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) TryPackPoolEmission(arg0 *big.Int, arg1 *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("poolEmission", arg0, arg1)
}

// UnpackPoolEmission is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x28491fb8.
//
// Solidity: function poolEmission(uint256 , uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) UnpackPoolEmission(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("poolEmission", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackPoolTotal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xb8c47b3a.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function poolTotal(uint256 , uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) PackPoolTotal(arg0 *big.Int, arg1 *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("poolTotal", arg0, arg1)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPoolTotal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xb8c47b3a.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function poolTotal(uint256 , uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) TryPackPoolTotal(arg0 *big.Int, arg1 *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("poolTotal", arg0, arg1)
}

// UnpackPoolTotal is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xb8c47b3a.
//
// Solidity: function poolTotal(uint256 , uint256 ) view returns(uint256)
func (sTSubnet *STSubnet) UnpackPoolTotal(data []byte) (*big.Int, error) {
	out, err := sTSubnet.abi.Unpack("poolTotal", data)
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
func (sTSubnet *STSubnet) PackProxiableUUID() []byte {
	enc, err := sTSubnet.abi.Pack("proxiableUUID")
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
func (sTSubnet *STSubnet) TryPackProxiableUUID() ([]byte, error) {
	return sTSubnet.abi.Pack("proxiableUUID")
}

// UnpackProxiableUUID is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (sTSubnet *STSubnet) UnpackProxiableUUID(data []byte) ([32]byte, error) {
	out, err := sTSubnet.abi.Unpack("proxiableUUID", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackRegisterOperator is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x82bb618d.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function registerOperator(uint256 noId, bytes32 coldkey, bytes32 minerHotkey) returns()
func (sTSubnet *STSubnet) PackRegisterOperator(noId *big.Int, coldkey [32]byte, minerHotkey [32]byte) []byte {
	enc, err := sTSubnet.abi.Pack("registerOperator", noId, coldkey, minerHotkey)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRegisterOperator is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x82bb618d.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function registerOperator(uint256 noId, bytes32 coldkey, bytes32 minerHotkey) returns()
func (sTSubnet *STSubnet) TryPackRegisterOperator(noId *big.Int, coldkey [32]byte, minerHotkey [32]byte) ([]byte, error) {
	return sTSubnet.abi.Pack("registerOperator", noId, coldkey, minerHotkey)
}

// PackRenounceOwnership is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x715018a6.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function renounceOwnership() returns()
func (sTSubnet *STSubnet) PackRenounceOwnership() []byte {
	enc, err := sTSubnet.abi.Pack("renounceOwnership")
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
func (sTSubnet *STSubnet) TryPackRenounceOwnership() ([]byte, error) {
	return sTSubnet.abi.Pack("renounceOwnership")
}

// PackReserveHotkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x6ac86cc5.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function reserveHotkey() view returns(bytes32)
func (sTSubnet *STSubnet) PackReserveHotkey() []byte {
	enc, err := sTSubnet.abi.Pack("reserveHotkey")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackReserveHotkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x6ac86cc5.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function reserveHotkey() view returns(bytes32)
func (sTSubnet *STSubnet) TryPackReserveHotkey() ([]byte, error) {
	return sTSubnet.abi.Pack("reserveHotkey")
}

// UnpackReserveHotkey is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x6ac86cc5.
//
// Solidity: function reserveHotkey() view returns(bytes32)
func (sTSubnet *STSubnet) UnpackReserveHotkey(data []byte) ([32]byte, error) {
	out, err := sTSubnet.abi.Unpack("reserveHotkey", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackRollEpochs is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x26431fe4.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function rollEpochs() returns()
func (sTSubnet *STSubnet) PackRollEpochs() []byte {
	enc, err := sTSubnet.abi.Pack("rollEpochs")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRollEpochs is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x26431fe4.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function rollEpochs() returns()
func (sTSubnet *STSubnet) TryPackRollEpochs() ([]byte, error) {
	return sTSubnet.abi.Pack("rollEpochs")
}

// PackSelfColdkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x877e4394.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function selfColdkey() view returns(bytes32)
func (sTSubnet *STSubnet) PackSelfColdkey() []byte {
	enc, err := sTSubnet.abi.Pack("selfColdkey")
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
func (sTSubnet *STSubnet) TryPackSelfColdkey() ([]byte, error) {
	return sTSubnet.abi.Pack("selfColdkey")
}

// UnpackSelfColdkey is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x877e4394.
//
// Solidity: function selfColdkey() view returns(bytes32)
func (sTSubnet *STSubnet) UnpackSelfColdkey(data []byte) ([32]byte, error) {
	out, err := sTSubnet.abi.Unpack("selfColdkey", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackSetEpochParams is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x8550c52c.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function setEpochParams(uint64 tEpoch_, uint64 commitWindowBlocks_, uint64 trailsWindowBlocks_, uint64 finalizeOffsetBlocks_) returns()
func (sTSubnet *STSubnet) PackSetEpochParams(tEpoch uint64, commitWindowBlocks uint64, trailsWindowBlocks uint64, finalizeOffsetBlocks uint64) []byte {
	enc, err := sTSubnet.abi.Pack("setEpochParams", tEpoch, commitWindowBlocks, trailsWindowBlocks, finalizeOffsetBlocks)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSetEpochParams is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x8550c52c.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function setEpochParams(uint64 tEpoch_, uint64 commitWindowBlocks_, uint64 trailsWindowBlocks_, uint64 finalizeOffsetBlocks_) returns()
func (sTSubnet *STSubnet) TryPackSetEpochParams(tEpoch uint64, commitWindowBlocks uint64, trailsWindowBlocks uint64, finalizeOffsetBlocks uint64) ([]byte, error) {
	return sTSubnet.abi.Pack("setEpochParams", tEpoch, commitWindowBlocks, trailsWindowBlocks, finalizeOffsetBlocks)
}

// PackSetGuardian is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x8a0dac4a.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function setGuardian(address guardian_) returns()
func (sTSubnet *STSubnet) PackSetGuardian(guardian common.Address) []byte {
	enc, err := sTSubnet.abi.Pack("setGuardian", guardian)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSetGuardian is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x8a0dac4a.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function setGuardian(address guardian_) returns()
func (sTSubnet *STSubnet) TryPackSetGuardian(guardian common.Address) ([]byte, error) {
	return sTSubnet.abi.Pack("setGuardian", guardian)
}

// PackSetOperatorActive is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x11491b5b.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function setOperatorActive(uint256 noId, bool active) returns()
func (sTSubnet *STSubnet) PackSetOperatorActive(noId *big.Int, active bool) []byte {
	enc, err := sTSubnet.abi.Pack("setOperatorActive", noId, active)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSetOperatorActive is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x11491b5b.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function setOperatorActive(uint256 noId, bool active) returns()
func (sTSubnet *STSubnet) TryPackSetOperatorActive(noId *big.Int, active bool) ([]byte, error) {
	return sTSubnet.abi.Pack("setOperatorActive", noId, active)
}

// PackSetOperatorAddress is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf87e5296.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function setOperatorAddress(uint256 noId, address addr) returns()
func (sTSubnet *STSubnet) PackSetOperatorAddress(noId *big.Int, addr common.Address) []byte {
	enc, err := sTSubnet.abi.Pack("setOperatorAddress", noId, addr)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSetOperatorAddress is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf87e5296.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function setOperatorAddress(uint256 noId, address addr) returns()
func (sTSubnet *STSubnet) TryPackSetOperatorAddress(noId *big.Int, addr common.Address) ([]byte, error) {
	return sTSubnet.abi.Pack("setOperatorAddress", noId, addr)
}

// PackSetPaused is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x16c38b3c.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function setPaused(bool paused_) returns()
func (sTSubnet *STSubnet) PackSetPaused(paused bool) []byte {
	enc, err := sTSubnet.abi.Pack("setPaused", paused)
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
func (sTSubnet *STSubnet) TryPackSetPaused(paused bool) ([]byte, error) {
	return sTSubnet.abi.Pack("setPaused", paused)
}

// PackSetSelfColdkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xbbf4a9e7.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function setSelfColdkey(bytes32 selfColdkey_) returns()
func (sTSubnet *STSubnet) PackSetSelfColdkey(selfColdkey [32]byte) []byte {
	enc, err := sTSubnet.abi.Pack("setSelfColdkey", selfColdkey)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSetSelfColdkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xbbf4a9e7.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function setSelfColdkey(bytes32 selfColdkey_) returns()
func (sTSubnet *STSubnet) TryPackSetSelfColdkey(selfColdkey [32]byte) ([]byte, error) {
	return sTSubnet.abi.Pack("setSelfColdkey", selfColdkey)
}

// PackSweepPool is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc12991f1.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function sweepPool(uint256 noId) returns()
func (sTSubnet *STSubnet) PackSweepPool(noId *big.Int) []byte {
	enc, err := sTSubnet.abi.Pack("sweepPool", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSweepPool is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc12991f1.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function sweepPool(uint256 noId) returns()
func (sTSubnet *STSubnet) TryPackSweepPool(noId *big.Int) ([]byte, error) {
	return sTSubnet.abi.Pack("sweepPool", noId)
}

// PackTEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa124f856.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function tEpoch() view returns(uint64)
func (sTSubnet *STSubnet) PackTEpoch() []byte {
	enc, err := sTSubnet.abi.Pack("tEpoch")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackTEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa124f856.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function tEpoch() view returns(uint64)
func (sTSubnet *STSubnet) TryPackTEpoch() ([]byte, error) {
	return sTSubnet.abi.Pack("tEpoch")
}

// UnpackTEpoch is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xa124f856.
//
// Solidity: function tEpoch() view returns(uint64)
func (sTSubnet *STSubnet) UnpackTEpoch(data []byte) (uint64, error) {
	out, err := sTSubnet.abi.Unpack("tEpoch", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackTrailsWindowBlocks is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf0d1af9b.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function trailsWindowBlocks() view returns(uint64)
func (sTSubnet *STSubnet) PackTrailsWindowBlocks() []byte {
	enc, err := sTSubnet.abi.Pack("trailsWindowBlocks")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackTrailsWindowBlocks is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf0d1af9b.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function trailsWindowBlocks() view returns(uint64)
func (sTSubnet *STSubnet) TryPackTrailsWindowBlocks() ([]byte, error) {
	return sTSubnet.abi.Pack("trailsWindowBlocks")
}

// UnpackTrailsWindowBlocks is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xf0d1af9b.
//
// Solidity: function trailsWindowBlocks() view returns(uint64)
func (sTSubnet *STSubnet) UnpackTrailsWindowBlocks(data []byte) (uint64, error) {
	out, err := sTSubnet.abi.Unpack("trailsWindowBlocks", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackTransferOwnership is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf2fde38b.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (sTSubnet *STSubnet) PackTransferOwnership(newOwner common.Address) []byte {
	enc, err := sTSubnet.abi.Pack("transferOwnership", newOwner)
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
func (sTSubnet *STSubnet) TryPackTransferOwnership(newOwner common.Address) ([]byte, error) {
	return sTSubnet.abi.Pack("transferOwnership", newOwner)
}

// PackTreasuryHotkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xeb1e24eb.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function treasuryHotkey() view returns(bytes32)
func (sTSubnet *STSubnet) PackTreasuryHotkey() []byte {
	enc, err := sTSubnet.abi.Pack("treasuryHotkey")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackTreasuryHotkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xeb1e24eb.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function treasuryHotkey() view returns(bytes32)
func (sTSubnet *STSubnet) TryPackTreasuryHotkey() ([]byte, error) {
	return sTSubnet.abi.Pack("treasuryHotkey")
}

// UnpackTreasuryHotkey is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xeb1e24eb.
//
// Solidity: function treasuryHotkey() view returns(bytes32)
func (sTSubnet *STSubnet) UnpackTreasuryHotkey(data []byte) ([32]byte, error) {
	out, err := sTSubnet.abi.Unpack("treasuryHotkey", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackUnbindHead is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xfa88f380.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function unbindHead(bytes32 hotkey) returns()
func (sTSubnet *STSubnet) PackUnbindHead(hotkey [32]byte) []byte {
	enc, err := sTSubnet.abi.Pack("unbindHead", hotkey)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackUnbindHead is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xfa88f380.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function unbindHead(bytes32 hotkey) returns()
func (sTSubnet *STSubnet) TryPackUnbindHead(hotkey [32]byte) ([]byte, error) {
	return sTSubnet.abi.Pack("unbindHead", hotkey)
}

// PackUpgradeToAndCall is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4f1ef286.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (sTSubnet *STSubnet) PackUpgradeToAndCall(newImplementation common.Address, data []byte) []byte {
	enc, err := sTSubnet.abi.Pack("upgradeToAndCall", newImplementation, data)
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
func (sTSubnet *STSubnet) TryPackUpgradeToAndCall(newImplementation common.Address, data []byte) ([]byte, error) {
	return sTSubnet.abi.Pack("upgradeToAndCall", newImplementation, data)
}

// STSubnetBuybackReserved represents a BuybackReserved event raised by the STSubnet contract.
type STSubnetBuybackReserved struct {
	E            *big.Int
	NoId         *big.Int
	Amount       *big.Int
	BuybackTotal *big.Int
	Raw          *types.Log // Blockchain specific contextual infos
}

const STSubnetBuybackReservedEventName = "BuybackReserved"

// ContractEventName returns the user-defined event name.
func (STSubnetBuybackReserved) ContractEventName() string {
	return STSubnetBuybackReservedEventName
}

// UnpackBuybackReservedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event BuybackReserved(uint256 indexed e, uint256 indexed noId, uint256 amount, uint256 buybackTotal)
func (sTSubnet *STSubnet) UnpackBuybackReservedEvent(log *types.Log) (*STSubnetBuybackReserved, error) {
	event := "BuybackReserved"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetBuybackReserved)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetDeposited represents a Deposited event raised by the STSubnet contract.
type STSubnetDeposited struct {
	E      *big.Int
	NoId   *big.Int
	From   common.Address
	Amount *big.Int
	Raw    *types.Log // Blockchain specific contextual infos
}

const STSubnetDepositedEventName = "Deposited"

// ContractEventName returns the user-defined event name.
func (STSubnetDeposited) ContractEventName() string {
	return STSubnetDepositedEventName
}

// UnpackDepositedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event Deposited(uint256 indexed e, uint256 indexed noId, address from, uint256 amount)
func (sTSubnet *STSubnet) UnpackDepositedEvent(log *types.Log) (*STSubnetDeposited, error) {
	event := "Deposited"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetDeposited)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetEpochFinalized represents a EpochFinalized event raised by the STSubnet contract.
type STSubnetEpochFinalized struct {
	E   *big.Int
	Raw *types.Log // Blockchain specific contextual infos
}

const STSubnetEpochFinalizedEventName = "EpochFinalized"

// ContractEventName returns the user-defined event name.
func (STSubnetEpochFinalized) ContractEventName() string {
	return STSubnetEpochFinalizedEventName
}

// UnpackEpochFinalizedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event EpochFinalized(uint256 indexed e)
func (sTSubnet *STSubnet) UnpackEpochFinalizedEvent(log *types.Log) (*STSubnetEpochFinalized, error) {
	event := "EpochFinalized"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetEpochFinalized)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetEpochParamsSet represents a EpochParamsSet event raised by the STSubnet contract.
type STSubnetEpochParamsSet struct {
	TEpoch               uint64
	CommitWindowBlocks   uint64
	TrailsWindowBlocks   uint64
	FinalizeOffsetBlocks uint64
	Raw                  *types.Log // Blockchain specific contextual infos
}

const STSubnetEpochParamsSetEventName = "EpochParamsSet"

// ContractEventName returns the user-defined event name.
func (STSubnetEpochParamsSet) ContractEventName() string {
	return STSubnetEpochParamsSetEventName
}

// UnpackEpochParamsSetEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event EpochParamsSet(uint64 tEpoch, uint64 commitWindowBlocks, uint64 trailsWindowBlocks, uint64 finalizeOffsetBlocks)
func (sTSubnet *STSubnet) UnpackEpochParamsSetEvent(log *types.Log) (*STSubnetEpochParamsSet, error) {
	event := "EpochParamsSet"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetEpochParamsSet)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetEpochRolled represents a EpochRolled event raised by the STSubnet contract.
type STSubnetEpochRolled struct {
	ClosedEpoch *big.Int
	NewEpoch    *big.Int
	CloseBlock  uint64
	Raw         *types.Log // Blockchain specific contextual infos
}

const STSubnetEpochRolledEventName = "EpochRolled"

// ContractEventName returns the user-defined event name.
func (STSubnetEpochRolled) ContractEventName() string {
	return STSubnetEpochRolledEventName
}

// UnpackEpochRolledEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event EpochRolled(uint256 indexed closedEpoch, uint256 indexed newEpoch, uint64 closeBlock)
func (sTSubnet *STSubnet) UnpackEpochRolledEvent(log *types.Log) (*STSubnetEpochRolled, error) {
	event := "EpochRolled"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetEpochRolled)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetGuardianSet represents a GuardianSet event raised by the STSubnet contract.
type STSubnetGuardianSet struct {
	Guardian common.Address
	Raw      *types.Log // Blockchain specific contextual infos
}

const STSubnetGuardianSetEventName = "GuardianSet"

// ContractEventName returns the user-defined event name.
func (STSubnetGuardianSet) ContractEventName() string {
	return STSubnetGuardianSetEventName
}

// UnpackGuardianSetEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event GuardianSet(address guardian)
func (sTSubnet *STSubnet) UnpackGuardianSetEvent(log *types.Log) (*STSubnetGuardianSet, error) {
	event := "GuardianSet"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetGuardianSet)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetHeadBound represents a HeadBound event raised by the STSubnet contract.
type STSubnetHeadBound struct {
	Hotkey     [32]byte
	ClientId   [32]byte
	Uid        uint16
	Registrant common.Address
	Raw        *types.Log // Blockchain specific contextual infos
}

const STSubnetHeadBoundEventName = "HeadBound"

// ContractEventName returns the user-defined event name.
func (STSubnetHeadBound) ContractEventName() string {
	return STSubnetHeadBoundEventName
}

// UnpackHeadBoundEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event HeadBound(bytes32 indexed hotkey, bytes32 indexed clientId, uint16 uid, address registrant)
func (sTSubnet *STSubnet) UnpackHeadBoundEvent(log *types.Log) (*STSubnetHeadBound, error) {
	event := "HeadBound"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetHeadBound)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetHeadUnbound represents a HeadUnbound event raised by the STSubnet contract.
type STSubnetHeadUnbound struct {
	Hotkey     [32]byte
	ClientId   [32]byte
	Uid        uint16
	Registrant common.Address
	Raw        *types.Log // Blockchain specific contextual infos
}

const STSubnetHeadUnboundEventName = "HeadUnbound"

// ContractEventName returns the user-defined event name.
func (STSubnetHeadUnbound) ContractEventName() string {
	return STSubnetHeadUnboundEventName
}

// UnpackHeadUnboundEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event HeadUnbound(bytes32 indexed hotkey, bytes32 indexed clientId, uint16 uid, address registrant)
func (sTSubnet *STSubnet) UnpackHeadUnboundEvent(log *types.Log) (*STSubnetHeadUnbound, error) {
	event := "HeadUnbound"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetHeadUnbound)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetInitialized represents a Initialized event raised by the STSubnet contract.
type STSubnetInitialized struct {
	Version uint64
	Raw     *types.Log // Blockchain specific contextual infos
}

const STSubnetInitializedEventName = "Initialized"

// ContractEventName returns the user-defined event name.
func (STSubnetInitialized) ContractEventName() string {
	return STSubnetInitializedEventName
}

// UnpackInitializedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event Initialized(uint64 version)
func (sTSubnet *STSubnet) UnpackInitializedEvent(log *types.Log) (*STSubnetInitialized, error) {
	event := "Initialized"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetInitialized)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetMinerClaimed represents a MinerClaimed event raised by the STSubnet contract.
type STSubnetMinerClaimed struct {
	E        *big.Int
	NoId     *big.Int
	Coldkey  [32]byte
	ShareBps *big.Int
	Amount   *big.Int
	Caller   common.Address
	Raw      *types.Log // Blockchain specific contextual infos
}

const STSubnetMinerClaimedEventName = "MinerClaimed"

// ContractEventName returns the user-defined event name.
func (STSubnetMinerClaimed) ContractEventName() string {
	return STSubnetMinerClaimedEventName
}

// UnpackMinerClaimedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event MinerClaimed(uint256 indexed e, uint256 indexed noId, bytes32 indexed coldkey, uint256 shareBps, uint256 amount, address caller)
func (sTSubnet *STSubnet) UnpackMinerClaimedEvent(log *types.Log) (*STSubnetMinerClaimed, error) {
	event := "MinerClaimed"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetMinerClaimed)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetOperatorActiveSet represents a OperatorActiveSet event raised by the STSubnet contract.
type STSubnetOperatorActiveSet struct {
	NoId   *big.Int
	Active bool
	Raw    *types.Log // Blockchain specific contextual infos
}

const STSubnetOperatorActiveSetEventName = "OperatorActiveSet"

// ContractEventName returns the user-defined event name.
func (STSubnetOperatorActiveSet) ContractEventName() string {
	return STSubnetOperatorActiveSetEventName
}

// UnpackOperatorActiveSetEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event OperatorActiveSet(uint256 indexed noId, bool active)
func (sTSubnet *STSubnet) UnpackOperatorActiveSetEvent(log *types.Log) (*STSubnetOperatorActiveSet, error) {
	event := "OperatorActiveSet"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetOperatorActiveSet)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetOperatorAddressSet represents a OperatorAddressSet event raised by the STSubnet contract.
type STSubnetOperatorAddressSet struct {
	NoId *big.Int
	Addr common.Address
	Raw  *types.Log // Blockchain specific contextual infos
}

const STSubnetOperatorAddressSetEventName = "OperatorAddressSet"

// ContractEventName returns the user-defined event name.
func (STSubnetOperatorAddressSet) ContractEventName() string {
	return STSubnetOperatorAddressSetEventName
}

// UnpackOperatorAddressSetEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event OperatorAddressSet(uint256 indexed noId, address addr)
func (sTSubnet *STSubnet) UnpackOperatorAddressSetEvent(log *types.Log) (*STSubnetOperatorAddressSet, error) {
	event := "OperatorAddressSet"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetOperatorAddressSet)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetOperatorCommitted represents a OperatorCommitted event raised by the STSubnet contract.
type STSubnetOperatorCommitted struct {
	E          *big.Int
	NoId       *big.Int
	PayoutRoot [32]byte
	Off        []byte
	Raw        *types.Log // Blockchain specific contextual infos
}

const STSubnetOperatorCommittedEventName = "OperatorCommitted"

// ContractEventName returns the user-defined event name.
func (STSubnetOperatorCommitted) ContractEventName() string {
	return STSubnetOperatorCommittedEventName
}

// UnpackOperatorCommittedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event OperatorCommitted(uint256 indexed e, uint256 indexed noId, bytes32 payoutRoot, bytes off)
func (sTSubnet *STSubnet) UnpackOperatorCommittedEvent(log *types.Log) (*STSubnetOperatorCommitted, error) {
	event := "OperatorCommitted"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetOperatorCommitted)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetOperatorRegistered represents a OperatorRegistered event raised by the STSubnet contract.
type STSubnetOperatorRegistered struct {
	NoId        *big.Int
	Coldkey     [32]byte
	MinerUid    uint16
	MinerHotkey [32]byte
	Raw         *types.Log // Blockchain specific contextual infos
}

const STSubnetOperatorRegisteredEventName = "OperatorRegistered"

// ContractEventName returns the user-defined event name.
func (STSubnetOperatorRegistered) ContractEventName() string {
	return STSubnetOperatorRegisteredEventName
}

// UnpackOperatorRegisteredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event OperatorRegistered(uint256 indexed noId, bytes32 coldkey, uint16 minerUid, bytes32 minerHotkey)
func (sTSubnet *STSubnet) UnpackOperatorRegisteredEvent(log *types.Log) (*STSubnetOperatorRegistered, error) {
	event := "OperatorRegistered"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetOperatorRegistered)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetOwnershipTransferred represents a OwnershipTransferred event raised by the STSubnet contract.
type STSubnetOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           *types.Log // Blockchain specific contextual infos
}

const STSubnetOwnershipTransferredEventName = "OwnershipTransferred"

// ContractEventName returns the user-defined event name.
func (STSubnetOwnershipTransferred) ContractEventName() string {
	return STSubnetOwnershipTransferredEventName
}

// UnpackOwnershipTransferredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (sTSubnet *STSubnet) UnpackOwnershipTransferredEvent(log *types.Log) (*STSubnetOwnershipTransferred, error) {
	event := "OwnershipTransferred"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetOwnershipTransferred)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetPausedSet represents a PausedSet event raised by the STSubnet contract.
type STSubnetPausedSet struct {
	Paused bool
	By     common.Address
	Raw    *types.Log // Blockchain specific contextual infos
}

const STSubnetPausedSetEventName = "PausedSet"

// ContractEventName returns the user-defined event name.
func (STSubnetPausedSet) ContractEventName() string {
	return STSubnetPausedSetEventName
}

// UnpackPausedSetEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event PausedSet(bool paused, address by)
func (sTSubnet *STSubnet) UnpackPausedSetEvent(log *types.Log) (*STSubnetPausedSet, error) {
	event := "PausedSet"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetPausedSet)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetPoolCarried represents a PoolCarried event raised by the STSubnet contract.
type STSubnetPoolCarried struct {
	E       *big.Int
	NoId    *big.Int
	Carried *big.Int
	Raw     *types.Log // Blockchain specific contextual infos
}

const STSubnetPoolCarriedEventName = "PoolCarried"

// ContractEventName returns the user-defined event name.
func (STSubnetPoolCarried) ContractEventName() string {
	return STSubnetPoolCarriedEventName
}

// UnpackPoolCarriedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event PoolCarried(uint256 indexed e, uint256 indexed noId, uint256 carried)
func (sTSubnet *STSubnet) UnpackPoolCarriedEvent(log *types.Log) (*STSubnetPoolCarried, error) {
	event := "PoolCarried"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetPoolCarried)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetPoolFinalized represents a PoolFinalized event raised by the STSubnet contract.
type STSubnetPoolFinalized struct {
	E         *big.Int
	NoId      *big.Int
	PoolTotal *big.Int
	Raw       *types.Log // Blockchain specific contextual infos
}

const STSubnetPoolFinalizedEventName = "PoolFinalized"

// ContractEventName returns the user-defined event name.
func (STSubnetPoolFinalized) ContractEventName() string {
	return STSubnetPoolFinalizedEventName
}

// UnpackPoolFinalizedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event PoolFinalized(uint256 indexed e, uint256 indexed noId, uint256 poolTotal)
func (sTSubnet *STSubnet) UnpackPoolFinalizedEvent(log *types.Log) (*STSubnetPoolFinalized, error) {
	event := "PoolFinalized"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetPoolFinalized)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetPoolSwept represents a PoolSwept event raised by the STSubnet contract.
type STSubnetPoolSwept struct {
	NoId     *big.Int
	Measured *big.Int
	Swept    *big.Int
	MoveOk   bool
	Raw      *types.Log // Blockchain specific contextual infos
}

const STSubnetPoolSweptEventName = "PoolSwept"

// ContractEventName returns the user-defined event name.
func (STSubnetPoolSwept) ContractEventName() string {
	return STSubnetPoolSweptEventName
}

// UnpackPoolSweptEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event PoolSwept(uint256 indexed noId, uint256 measured, uint256 swept, bool moveOk)
func (sTSubnet *STSubnet) UnpackPoolSweptEvent(log *types.Log) (*STSubnetPoolSwept, error) {
	event := "PoolSwept"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetPoolSwept)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetSelfColdkeySet represents a SelfColdkeySet event raised by the STSubnet contract.
type STSubnetSelfColdkeySet struct {
	SelfColdkey [32]byte
	Raw         *types.Log // Blockchain specific contextual infos
}

const STSubnetSelfColdkeySetEventName = "SelfColdkeySet"

// ContractEventName returns the user-defined event name.
func (STSubnetSelfColdkeySet) ContractEventName() string {
	return STSubnetSelfColdkeySetEventName
}

// UnpackSelfColdkeySetEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event SelfColdkeySet(bytes32 selfColdkey)
func (sTSubnet *STSubnet) UnpackSelfColdkeySetEvent(log *types.Log) (*STSubnetSelfColdkeySet, error) {
	event := "SelfColdkeySet"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetSelfColdkeySet)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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

// STSubnetUpgraded represents a Upgraded event raised by the STSubnet contract.
type STSubnetUpgraded struct {
	Implementation common.Address
	Raw            *types.Log // Blockchain specific contextual infos
}

const STSubnetUpgradedEventName = "Upgraded"

// ContractEventName returns the user-defined event name.
func (STSubnetUpgraded) ContractEventName() string {
	return STSubnetUpgradedEventName
}

// UnpackUpgradedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event Upgraded(address indexed implementation)
func (sTSubnet *STSubnet) UnpackUpgradedEvent(log *types.Log) (*STSubnetUpgraded, error) {
	event := "Upgraded"
	if len(log.Topics) == 0 || log.Topics[0] != sTSubnet.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSubnetUpgraded)
	if len(log.Data) > 0 {
		if err := sTSubnet.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSubnet.abi.Events[event].Inputs {
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
func (sTSubnet *STSubnet) UnpackError(raw []byte) (any, error) {
	if bytes.Equal(raw[:4], sTSubnet.abi.Errors["AddressEmptyCode"].ID.Bytes()[:4]) {
		return sTSubnet.UnpackAddressEmptyCodeError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSubnet.abi.Errors["ERC1967InvalidImplementation"].ID.Bytes()[:4]) {
		return sTSubnet.UnpackERC1967InvalidImplementationError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSubnet.abi.Errors["ERC1967NonPayable"].ID.Bytes()[:4]) {
		return sTSubnet.UnpackERC1967NonPayableError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSubnet.abi.Errors["FailedCall"].ID.Bytes()[:4]) {
		return sTSubnet.UnpackFailedCallError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSubnet.abi.Errors["InvalidInitialization"].ID.Bytes()[:4]) {
		return sTSubnet.UnpackInvalidInitializationError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSubnet.abi.Errors["NotInitializing"].ID.Bytes()[:4]) {
		return sTSubnet.UnpackNotInitializingError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSubnet.abi.Errors["OwnableInvalidOwner"].ID.Bytes()[:4]) {
		return sTSubnet.UnpackOwnableInvalidOwnerError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSubnet.abi.Errors["OwnableUnauthorizedAccount"].ID.Bytes()[:4]) {
		return sTSubnet.UnpackOwnableUnauthorizedAccountError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSubnet.abi.Errors["UUPSUnauthorizedCallContext"].ID.Bytes()[:4]) {
		return sTSubnet.UnpackUUPSUnauthorizedCallContextError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSubnet.abi.Errors["UUPSUnsupportedProxiableUUID"].ID.Bytes()[:4]) {
		return sTSubnet.UnpackUUPSUnsupportedProxiableUUIDError(raw[4:])
	}
	return nil, errors.New("Unknown error")
}

// STSubnetAddressEmptyCode represents a AddressEmptyCode error raised by the STSubnet contract.
type STSubnetAddressEmptyCode struct {
	Target common.Address
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error AddressEmptyCode(address target)
func STSubnetAddressEmptyCodeErrorID() common.Hash {
	return common.HexToHash("0x9996b315c842ff135b8fc4a08ad5df1c344efbc03d2687aecc0678050d2aac89")
}

// UnpackAddressEmptyCodeError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error AddressEmptyCode(address target)
func (sTSubnet *STSubnet) UnpackAddressEmptyCodeError(raw []byte) (*STSubnetAddressEmptyCode, error) {
	out := new(STSubnetAddressEmptyCode)
	if err := sTSubnet.abi.UnpackIntoInterface(out, "AddressEmptyCode", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSubnetERC1967InvalidImplementation represents a ERC1967InvalidImplementation error raised by the STSubnet contract.
type STSubnetERC1967InvalidImplementation struct {
	Implementation common.Address
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error ERC1967InvalidImplementation(address implementation)
func STSubnetERC1967InvalidImplementationErrorID() common.Hash {
	return common.HexToHash("0x4c9c8ce3ceb3130f17f7cdba48d89b5b0129f266a8bac114e6e315a41879b617")
}

// UnpackERC1967InvalidImplementationError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error ERC1967InvalidImplementation(address implementation)
func (sTSubnet *STSubnet) UnpackERC1967InvalidImplementationError(raw []byte) (*STSubnetERC1967InvalidImplementation, error) {
	out := new(STSubnetERC1967InvalidImplementation)
	if err := sTSubnet.abi.UnpackIntoInterface(out, "ERC1967InvalidImplementation", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSubnetERC1967NonPayable represents a ERC1967NonPayable error raised by the STSubnet contract.
type STSubnetERC1967NonPayable struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error ERC1967NonPayable()
func STSubnetERC1967NonPayableErrorID() common.Hash {
	return common.HexToHash("0xb398979fa84f543c8e222f17890372c487baf85e062276c127fef521eea7224b")
}

// UnpackERC1967NonPayableError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error ERC1967NonPayable()
func (sTSubnet *STSubnet) UnpackERC1967NonPayableError(raw []byte) (*STSubnetERC1967NonPayable, error) {
	out := new(STSubnetERC1967NonPayable)
	if err := sTSubnet.abi.UnpackIntoInterface(out, "ERC1967NonPayable", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSubnetFailedCall represents a FailedCall error raised by the STSubnet contract.
type STSubnetFailedCall struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error FailedCall()
func STSubnetFailedCallErrorID() common.Hash {
	return common.HexToHash("0xd6bda27508c0fb6d8a39b4b122878dab26f731a7d4e4abe711dd3731899052a4")
}

// UnpackFailedCallError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error FailedCall()
func (sTSubnet *STSubnet) UnpackFailedCallError(raw []byte) (*STSubnetFailedCall, error) {
	out := new(STSubnetFailedCall)
	if err := sTSubnet.abi.UnpackIntoInterface(out, "FailedCall", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSubnetInvalidInitialization represents a InvalidInitialization error raised by the STSubnet contract.
type STSubnetInvalidInitialization struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidInitialization()
func STSubnetInvalidInitializationErrorID() common.Hash {
	return common.HexToHash("0xf92ee8a957075833165f68c320933b1a1294aafc84ee6e0dd3fb178008f9aaf5")
}

// UnpackInvalidInitializationError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidInitialization()
func (sTSubnet *STSubnet) UnpackInvalidInitializationError(raw []byte) (*STSubnetInvalidInitialization, error) {
	out := new(STSubnetInvalidInitialization)
	if err := sTSubnet.abi.UnpackIntoInterface(out, "InvalidInitialization", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSubnetNotInitializing represents a NotInitializing error raised by the STSubnet contract.
type STSubnetNotInitializing struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NotInitializing()
func STSubnetNotInitializingErrorID() common.Hash {
	return common.HexToHash("0xd7e6bcf8597daa127dc9f0048d2f08d5ef140a2cb659feabd700beff1f7a8302")
}

// UnpackNotInitializingError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NotInitializing()
func (sTSubnet *STSubnet) UnpackNotInitializingError(raw []byte) (*STSubnetNotInitializing, error) {
	out := new(STSubnetNotInitializing)
	if err := sTSubnet.abi.UnpackIntoInterface(out, "NotInitializing", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSubnetOwnableInvalidOwner represents a OwnableInvalidOwner error raised by the STSubnet contract.
type STSubnetOwnableInvalidOwner struct {
	Owner common.Address
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error OwnableInvalidOwner(address owner)
func STSubnetOwnableInvalidOwnerErrorID() common.Hash {
	return common.HexToHash("0x1e4fbdf7f3ef8bcaa855599e3abf48b232380f183f08f6f813d9ffa5bd585188")
}

// UnpackOwnableInvalidOwnerError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error OwnableInvalidOwner(address owner)
func (sTSubnet *STSubnet) UnpackOwnableInvalidOwnerError(raw []byte) (*STSubnetOwnableInvalidOwner, error) {
	out := new(STSubnetOwnableInvalidOwner)
	if err := sTSubnet.abi.UnpackIntoInterface(out, "OwnableInvalidOwner", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSubnetOwnableUnauthorizedAccount represents a OwnableUnauthorizedAccount error raised by the STSubnet contract.
type STSubnetOwnableUnauthorizedAccount struct {
	Account common.Address
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error OwnableUnauthorizedAccount(address account)
func STSubnetOwnableUnauthorizedAccountErrorID() common.Hash {
	return common.HexToHash("0x118cdaa7a341953d1887a2245fd6665d741c67c8c50581daa59e1d03373fa188")
}

// UnpackOwnableUnauthorizedAccountError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error OwnableUnauthorizedAccount(address account)
func (sTSubnet *STSubnet) UnpackOwnableUnauthorizedAccountError(raw []byte) (*STSubnetOwnableUnauthorizedAccount, error) {
	out := new(STSubnetOwnableUnauthorizedAccount)
	if err := sTSubnet.abi.UnpackIntoInterface(out, "OwnableUnauthorizedAccount", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSubnetUUPSUnauthorizedCallContext represents a UUPSUnauthorizedCallContext error raised by the STSubnet contract.
type STSubnetUUPSUnauthorizedCallContext struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error UUPSUnauthorizedCallContext()
func STSubnetUUPSUnauthorizedCallContextErrorID() common.Hash {
	return common.HexToHash("0xe07c8dba242a06571ac65fe4bbe20522c9fb111cb33599b799ff8039c1ed18f4")
}

// UnpackUUPSUnauthorizedCallContextError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error UUPSUnauthorizedCallContext()
func (sTSubnet *STSubnet) UnpackUUPSUnauthorizedCallContextError(raw []byte) (*STSubnetUUPSUnauthorizedCallContext, error) {
	out := new(STSubnetUUPSUnauthorizedCallContext)
	if err := sTSubnet.abi.UnpackIntoInterface(out, "UUPSUnauthorizedCallContext", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSubnetUUPSUnsupportedProxiableUUID represents a UUPSUnsupportedProxiableUUID error raised by the STSubnet contract.
type STSubnetUUPSUnsupportedProxiableUUID struct {
	Slot [32]byte
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error UUPSUnsupportedProxiableUUID(bytes32 slot)
func STSubnetUUPSUnsupportedProxiableUUIDErrorID() common.Hash {
	return common.HexToHash("0xaa1d49a4c084bfa9aeeee2a0be65267a7f19ba7e1476b114dac513d2c14cb563")
}

// UnpackUUPSUnsupportedProxiableUUIDError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error UUPSUnsupportedProxiableUUID(bytes32 slot)
func (sTSubnet *STSubnet) UnpackUUPSUnsupportedProxiableUUIDError(raw []byte) (*STSubnetUUPSUnsupportedProxiableUUID, error) {
	out := new(STSubnetUUPSUnsupportedProxiableUUID)
	if err := sTSubnet.abi.UnpackIntoInterface(out, "UUPSUnsupportedProxiableUUID", raw); err != nil {
		return nil, err
	}
	return out, nil
}
