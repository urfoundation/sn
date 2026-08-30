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

// STSettlementVaultEntitlement is an auto generated low-level Go binding around an user-defined struct.
type STSettlementVaultEntitlement struct {
	PayoutRoot   [32]byte
	ArtifactHash [32]byte
	Funded       *big.Int
	Total        *big.Int
	Claimed      *big.Int
	ExpiryBlock  uint64
	Status       uint8
}

// STSettlementVaultMetaData contains all meta data concerning the STSettlementVault contract.
var STSettlementVaultMetaData = bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[{\"name\":\"netuid_\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"escrowHotkey_\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"selfColdkey_\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"minimumClaimTTLBlocks_\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"minimumTransferTaoRao_\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"bootstrap_\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"receive\",\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"BPS\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"bootstrap\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"captureEmission\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"carry\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"claim\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"coldkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"shareBps\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"proof\",\"type\":\"bytes32[]\",\"internalType\":\"bytes32[]\"}],\"outputs\":[{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"claimCredit\",\"inputs\":[{\"name\":\"coldkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"conservationHolds\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"coordinator\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"deferEmission\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"entitlement\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple\",\"internalType\":\"structSTSettlementVault.Entitlement\",\"components\":[{\"name\":\"payoutRoot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"artifactHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"funded\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"total\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"claimed\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"expiryBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"status\",\"type\":\"uint8\",\"internalType\":\"enumSTSettlementVault.EpochStatus\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"escrowAccounted\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"escrowHotkey\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"escrowRegistered\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"expireEntitlement\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"finalizeEntitlement\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"payoutRoot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"artifactHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"expiryBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"leafClaimed\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"claimKey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"claimed\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"liveEscrowStake\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"markRootMissed\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"minimumClaimTTLBlocks\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"minimumTransferTaoRao\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"netuid\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint16\",\"internalType\":\"uint16\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"outstandingLiability\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"payoutLeaf\",\"inputs\":[{\"name\":\"coldkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"shareBps\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"pure\"},{\"type\":\"function\",\"name\":\"pendingFunding\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"poolHotkeyUsed\",\"inputs\":[{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"used\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"pools\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"uid\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"active\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"registerEscrow\",\"inputs\":[{\"name\":\"maximumBurnRao\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"outputs\":[{\"name\":\"uid\",\"type\":\"uint16\",\"internalType\":\"uint16\"}],\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"registerPool\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"poolHotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"maximumBurnRao\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"outputs\":[{\"name\":\"uid\",\"type\":\"uint16\",\"internalType\":\"uint16\"}],\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"selfColdkey\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"setCoordinatorOnce\",\"inputs\":[{\"name\":\"coordinator_\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"setPoolActive\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"active\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"totalCaptured\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"totalPaid\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"withdrawClaimCredit\",\"inputs\":[{\"name\":\"coldkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"event\",\"name\":\"ClaimPaid\",\"inputs\":[{\"name\":\"coldkey\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"relayer\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ClaimPaymentDeferred\",\"inputs\":[{\"name\":\"coldkey\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"creditAlphaRao\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"taoEquivalentRao\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"minimumTransferTaoRao\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"},{\"name\":\"reason\",\"type\":\"uint8\",\"indexed\":false,\"internalType\":\"enumSTSettlementVault.PaymentDeferralReason\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Claimed\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"coldkey\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"shareBps\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"relayer\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"CoordinatorFixed\",\"inputs\":[{\"name\":\"coordinator\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EmissionCaptured\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"poolHotkey\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EmissionDeferred\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EmissionDustDeferred\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"poolHotkey\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"observedAlphaRao\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"taoEquivalentRao\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"minimumTransferTaoRao\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EntitlementExpired\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"unclaimed\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"operatorCarry\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EntitlementFinalized\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"payoutRoot\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"artifactHash\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"total\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"expiryBlock\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EscrowRegistered\",\"inputs\":[{\"name\":\"hotkey\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"uid\",\"type\":\"uint16\",\"indexed\":false,\"internalType\":\"uint16\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"PoolActiveSet\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"active\",\"type\":\"bool\",\"indexed\":false,\"internalType\":\"bool\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"PoolRegistered\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"hotkey\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"uid\",\"type\":\"uint16\",\"indexed\":false,\"internalType\":\"uint16\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"RootMissed\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"carried\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"AlreadyClaimed\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"AlreadyInitialized\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"ClaimExpired\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidConfiguration\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidProof\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidTransition\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"NativeRefundFailed\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"NothingToWithdraw\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"Reentrancy\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"RuntimeAccountingMismatch\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"RuntimePriceUnavailable\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"RuntimeTransferFailed\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"TransferBelowMinimum\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"Unauthorized\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"Underfunded\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"UnknownPool\",\"inputs\":[]}]",
	ID:  "STSettlementVault",
}

// STSettlementVault is an auto generated Go binding around an Ethereum contract.
type STSettlementVault struct {
	abi abi.ABI
}

// NewSTSettlementVault creates a new instance of STSettlementVault.
func NewSTSettlementVault() *STSettlementVault {
	parsed, err := STSettlementVaultMetaData.ParseABI()
	if err != nil {
		panic(errors.New("invalid ABI: " + err.Error()))
	}
	return &STSettlementVault{abi: *parsed}
}

// Instance creates a wrapper for a deployed contract instance at the given address.
// Use this to create the instance object passed to abigen v2 library functions Call, Transact, etc.
func (c *STSettlementVault) Instance(backend bind.ContractBackend, addr common.Address) *bind.BoundContract {
	return bind.NewBoundContract(addr, c.abi, backend, backend, backend)
}

// PackConstructor is the Go binding used to pack the parameters required for
// contract deployment.
//
// Solidity: constructor(uint16 netuid_, bytes32 escrowHotkey_, bytes32 selfColdkey_, uint64 minimumClaimTTLBlocks_, uint64 minimumTransferTaoRao_, address bootstrap_) returns()
func (sTSettlementVault *STSettlementVault) PackConstructor(netuid_ uint16, escrowHotkey_ [32]byte, selfColdkey_ [32]byte, minimumClaimTTLBlocks_ uint64, minimumTransferTaoRao_ uint64, bootstrap_ common.Address) []byte {
	enc, err := sTSettlementVault.abi.Pack("", netuid_, escrowHotkey_, selfColdkey_, minimumClaimTTLBlocks_, minimumTransferTaoRao_, bootstrap_)
	if err != nil {
		panic(err)
	}
	return enc
}

// PackBPS is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x249d39e9.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function BPS() view returns(uint256)
func (sTSettlementVault *STSettlementVault) PackBPS() []byte {
	enc, err := sTSettlementVault.abi.Pack("BPS")
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
func (sTSettlementVault *STSettlementVault) TryPackBPS() ([]byte, error) {
	return sTSettlementVault.abi.Pack("BPS")
}

// UnpackBPS is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x249d39e9.
//
// Solidity: function BPS() view returns(uint256)
func (sTSettlementVault *STSettlementVault) UnpackBPS(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault.abi.Unpack("BPS", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackBootstrap is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xfb969b0a.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function bootstrap() view returns(address)
func (sTSettlementVault *STSettlementVault) PackBootstrap() []byte {
	enc, err := sTSettlementVault.abi.Pack("bootstrap")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackBootstrap is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xfb969b0a.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function bootstrap() view returns(address)
func (sTSettlementVault *STSettlementVault) TryPackBootstrap() ([]byte, error) {
	return sTSettlementVault.abi.Pack("bootstrap")
}

// UnpackBootstrap is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xfb969b0a.
//
// Solidity: function bootstrap() view returns(address)
func (sTSettlementVault *STSettlementVault) UnpackBootstrap(data []byte) (common.Address, error) {
	out, err := sTSettlementVault.abi.Unpack("bootstrap", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackCaptureEmission is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe0b08aa5.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function captureEmission(uint256 epoch, uint256 noId) returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) PackCaptureEmission(epoch *big.Int, noId *big.Int) []byte {
	enc, err := sTSettlementVault.abi.Pack("captureEmission", epoch, noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCaptureEmission is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe0b08aa5.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function captureEmission(uint256 epoch, uint256 noId) returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) TryPackCaptureEmission(epoch *big.Int, noId *big.Int) ([]byte, error) {
	return sTSettlementVault.abi.Pack("captureEmission", epoch, noId)
}

// UnpackCaptureEmission is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xe0b08aa5.
//
// Solidity: function captureEmission(uint256 epoch, uint256 noId) returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) UnpackCaptureEmission(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault.abi.Unpack("captureEmission", data)
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
// Solidity: function carry(uint256 noId) view returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) PackCarry(noId *big.Int) []byte {
	enc, err := sTSettlementVault.abi.Pack("carry", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCarry is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x044964ea.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function carry(uint256 noId) view returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) TryPackCarry(noId *big.Int) ([]byte, error) {
	return sTSettlementVault.abi.Pack("carry", noId)
}

// UnpackCarry is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x044964ea.
//
// Solidity: function carry(uint256 noId) view returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) UnpackCarry(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault.abi.Unpack("carry", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackClaim is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xce479a1b.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function claim(uint256 epoch, uint256 noId, bytes32 coldkey, uint256 shareBps, bytes32[] proof) returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) PackClaim(epoch *big.Int, noId *big.Int, coldkey [32]byte, shareBps *big.Int, proof [][32]byte) []byte {
	enc, err := sTSettlementVault.abi.Pack("claim", epoch, noId, coldkey, shareBps, proof)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackClaim is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xce479a1b.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function claim(uint256 epoch, uint256 noId, bytes32 coldkey, uint256 shareBps, bytes32[] proof) returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) TryPackClaim(epoch *big.Int, noId *big.Int, coldkey [32]byte, shareBps *big.Int, proof [][32]byte) ([]byte, error) {
	return sTSettlementVault.abi.Pack("claim", epoch, noId, coldkey, shareBps, proof)
}

// UnpackClaim is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xce479a1b.
//
// Solidity: function claim(uint256 epoch, uint256 noId, bytes32 coldkey, uint256 shareBps, bytes32[] proof) returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) UnpackClaim(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault.abi.Unpack("claim", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackClaimCredit is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xdebbe9e2.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function claimCredit(bytes32 coldkey) view returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) PackClaimCredit(coldkey [32]byte) []byte {
	enc, err := sTSettlementVault.abi.Pack("claimCredit", coldkey)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackClaimCredit is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xdebbe9e2.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function claimCredit(bytes32 coldkey) view returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) TryPackClaimCredit(coldkey [32]byte) ([]byte, error) {
	return sTSettlementVault.abi.Pack("claimCredit", coldkey)
}

// UnpackClaimCredit is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xdebbe9e2.
//
// Solidity: function claimCredit(bytes32 coldkey) view returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) UnpackClaimCredit(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault.abi.Unpack("claimCredit", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackConservationHolds is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x13944c3f.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function conservationHolds() view returns(bool)
func (sTSettlementVault *STSettlementVault) PackConservationHolds() []byte {
	enc, err := sTSettlementVault.abi.Pack("conservationHolds")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackConservationHolds is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x13944c3f.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function conservationHolds() view returns(bool)
func (sTSettlementVault *STSettlementVault) TryPackConservationHolds() ([]byte, error) {
	return sTSettlementVault.abi.Pack("conservationHolds")
}

// UnpackConservationHolds is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x13944c3f.
//
// Solidity: function conservationHolds() view returns(bool)
func (sTSettlementVault *STSettlementVault) UnpackConservationHolds(data []byte) (bool, error) {
	out, err := sTSettlementVault.abi.Unpack("conservationHolds", data)
	if err != nil {
		return *new(bool), err
	}
	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)
	return out0, nil
}

// PackCoordinator is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x0a009097.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function coordinator() view returns(address)
func (sTSettlementVault *STSettlementVault) PackCoordinator() []byte {
	enc, err := sTSettlementVault.abi.Pack("coordinator")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCoordinator is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x0a009097.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function coordinator() view returns(address)
func (sTSettlementVault *STSettlementVault) TryPackCoordinator() ([]byte, error) {
	return sTSettlementVault.abi.Pack("coordinator")
}

// UnpackCoordinator is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x0a009097.
//
// Solidity: function coordinator() view returns(address)
func (sTSettlementVault *STSettlementVault) UnpackCoordinator(data []byte) (common.Address, error) {
	out, err := sTSettlementVault.abi.Unpack("coordinator", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackDeferEmission is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x95b990c1.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function deferEmission(uint256 epoch, uint256 noId) returns()
func (sTSettlementVault *STSettlementVault) PackDeferEmission(epoch *big.Int, noId *big.Int) []byte {
	enc, err := sTSettlementVault.abi.Pack("deferEmission", epoch, noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackDeferEmission is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x95b990c1.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function deferEmission(uint256 epoch, uint256 noId) returns()
func (sTSettlementVault *STSettlementVault) TryPackDeferEmission(epoch *big.Int, noId *big.Int) ([]byte, error) {
	return sTSettlementVault.abi.Pack("deferEmission", epoch, noId)
}

// PackEntitlement is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x11f5fe7d.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function entitlement(uint256 epoch, uint256 noId) view returns((bytes32,bytes32,uint256,uint256,uint256,uint64,uint8))
func (sTSettlementVault *STSettlementVault) PackEntitlement(epoch *big.Int, noId *big.Int) []byte {
	enc, err := sTSettlementVault.abi.Pack("entitlement", epoch, noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackEntitlement is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x11f5fe7d.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function entitlement(uint256 epoch, uint256 noId) view returns((bytes32,bytes32,uint256,uint256,uint256,uint64,uint8))
func (sTSettlementVault *STSettlementVault) TryPackEntitlement(epoch *big.Int, noId *big.Int) ([]byte, error) {
	return sTSettlementVault.abi.Pack("entitlement", epoch, noId)
}

// UnpackEntitlement is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x11f5fe7d.
//
// Solidity: function entitlement(uint256 epoch, uint256 noId) view returns((bytes32,bytes32,uint256,uint256,uint256,uint64,uint8))
func (sTSettlementVault *STSettlementVault) UnpackEntitlement(data []byte) (STSettlementVaultEntitlement, error) {
	out, err := sTSettlementVault.abi.Unpack("entitlement", data)
	if err != nil {
		return *new(STSettlementVaultEntitlement), err
	}
	out0 := *abi.ConvertType(out[0], new(STSettlementVaultEntitlement)).(*STSettlementVaultEntitlement)
	return out0, nil
}

// PackEscrowAccounted is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xfd67fc0e.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function escrowAccounted() view returns(uint256)
func (sTSettlementVault *STSettlementVault) PackEscrowAccounted() []byte {
	enc, err := sTSettlementVault.abi.Pack("escrowAccounted")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackEscrowAccounted is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xfd67fc0e.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function escrowAccounted() view returns(uint256)
func (sTSettlementVault *STSettlementVault) TryPackEscrowAccounted() ([]byte, error) {
	return sTSettlementVault.abi.Pack("escrowAccounted")
}

// UnpackEscrowAccounted is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xfd67fc0e.
//
// Solidity: function escrowAccounted() view returns(uint256)
func (sTSettlementVault *STSettlementVault) UnpackEscrowAccounted(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault.abi.Unpack("escrowAccounted", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackEscrowHotkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x164bf360.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function escrowHotkey() view returns(bytes32)
func (sTSettlementVault *STSettlementVault) PackEscrowHotkey() []byte {
	enc, err := sTSettlementVault.abi.Pack("escrowHotkey")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackEscrowHotkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x164bf360.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function escrowHotkey() view returns(bytes32)
func (sTSettlementVault *STSettlementVault) TryPackEscrowHotkey() ([]byte, error) {
	return sTSettlementVault.abi.Pack("escrowHotkey")
}

// UnpackEscrowHotkey is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x164bf360.
//
// Solidity: function escrowHotkey() view returns(bytes32)
func (sTSettlementVault *STSettlementVault) UnpackEscrowHotkey(data []byte) ([32]byte, error) {
	out, err := sTSettlementVault.abi.Unpack("escrowHotkey", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackEscrowRegistered is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x92770c58.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function escrowRegistered() view returns(bool)
func (sTSettlementVault *STSettlementVault) PackEscrowRegistered() []byte {
	enc, err := sTSettlementVault.abi.Pack("escrowRegistered")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackEscrowRegistered is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x92770c58.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function escrowRegistered() view returns(bool)
func (sTSettlementVault *STSettlementVault) TryPackEscrowRegistered() ([]byte, error) {
	return sTSettlementVault.abi.Pack("escrowRegistered")
}

// UnpackEscrowRegistered is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x92770c58.
//
// Solidity: function escrowRegistered() view returns(bool)
func (sTSettlementVault *STSettlementVault) UnpackEscrowRegistered(data []byte) (bool, error) {
	out, err := sTSettlementVault.abi.Unpack("escrowRegistered", data)
	if err != nil {
		return *new(bool), err
	}
	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)
	return out0, nil
}

// PackExpireEntitlement is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x2596bf32.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function expireEntitlement(uint256 epoch, uint256 noId) returns()
func (sTSettlementVault *STSettlementVault) PackExpireEntitlement(epoch *big.Int, noId *big.Int) []byte {
	enc, err := sTSettlementVault.abi.Pack("expireEntitlement", epoch, noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackExpireEntitlement is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x2596bf32.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function expireEntitlement(uint256 epoch, uint256 noId) returns()
func (sTSettlementVault *STSettlementVault) TryPackExpireEntitlement(epoch *big.Int, noId *big.Int) ([]byte, error) {
	return sTSettlementVault.abi.Pack("expireEntitlement", epoch, noId)
}

// PackFinalizeEntitlement is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4738cfa0.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function finalizeEntitlement(uint256 epoch, uint256 noId, bytes32 payoutRoot, bytes32 artifactHash, uint64 expiryBlock) returns()
func (sTSettlementVault *STSettlementVault) PackFinalizeEntitlement(epoch *big.Int, noId *big.Int, payoutRoot [32]byte, artifactHash [32]byte, expiryBlock uint64) []byte {
	enc, err := sTSettlementVault.abi.Pack("finalizeEntitlement", epoch, noId, payoutRoot, artifactHash, expiryBlock)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackFinalizeEntitlement is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4738cfa0.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function finalizeEntitlement(uint256 epoch, uint256 noId, bytes32 payoutRoot, bytes32 artifactHash, uint64 expiryBlock) returns()
func (sTSettlementVault *STSettlementVault) TryPackFinalizeEntitlement(epoch *big.Int, noId *big.Int, payoutRoot [32]byte, artifactHash [32]byte, expiryBlock uint64) ([]byte, error) {
	return sTSettlementVault.abi.Pack("finalizeEntitlement", epoch, noId, payoutRoot, artifactHash, expiryBlock)
}

// PackLeafClaimed is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf3549e36.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function leafClaimed(uint256 epoch, bytes32 claimKey) view returns(bool claimed)
func (sTSettlementVault *STSettlementVault) PackLeafClaimed(epoch *big.Int, claimKey [32]byte) []byte {
	enc, err := sTSettlementVault.abi.Pack("leafClaimed", epoch, claimKey)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackLeafClaimed is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf3549e36.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function leafClaimed(uint256 epoch, bytes32 claimKey) view returns(bool claimed)
func (sTSettlementVault *STSettlementVault) TryPackLeafClaimed(epoch *big.Int, claimKey [32]byte) ([]byte, error) {
	return sTSettlementVault.abi.Pack("leafClaimed", epoch, claimKey)
}

// UnpackLeafClaimed is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xf3549e36.
//
// Solidity: function leafClaimed(uint256 epoch, bytes32 claimKey) view returns(bool claimed)
func (sTSettlementVault *STSettlementVault) UnpackLeafClaimed(data []byte) (bool, error) {
	out, err := sTSettlementVault.abi.Unpack("leafClaimed", data)
	if err != nil {
		return *new(bool), err
	}
	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)
	return out0, nil
}

// PackLiveEscrowStake is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf2f930ec.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function liveEscrowStake() view returns(uint256)
func (sTSettlementVault *STSettlementVault) PackLiveEscrowStake() []byte {
	enc, err := sTSettlementVault.abi.Pack("liveEscrowStake")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackLiveEscrowStake is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf2f930ec.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function liveEscrowStake() view returns(uint256)
func (sTSettlementVault *STSettlementVault) TryPackLiveEscrowStake() ([]byte, error) {
	return sTSettlementVault.abi.Pack("liveEscrowStake")
}

// UnpackLiveEscrowStake is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xf2f930ec.
//
// Solidity: function liveEscrowStake() view returns(uint256)
func (sTSettlementVault *STSettlementVault) UnpackLiveEscrowStake(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault.abi.Unpack("liveEscrowStake", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackMarkRootMissed is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x74cb349f.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function markRootMissed(uint256 epoch, uint256 noId) returns()
func (sTSettlementVault *STSettlementVault) PackMarkRootMissed(epoch *big.Int, noId *big.Int) []byte {
	enc, err := sTSettlementVault.abi.Pack("markRootMissed", epoch, noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackMarkRootMissed is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x74cb349f.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function markRootMissed(uint256 epoch, uint256 noId) returns()
func (sTSettlementVault *STSettlementVault) TryPackMarkRootMissed(epoch *big.Int, noId *big.Int) ([]byte, error) {
	return sTSettlementVault.abi.Pack("markRootMissed", epoch, noId)
}

// PackMinimumClaimTTLBlocks is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x1a56fbd7.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function minimumClaimTTLBlocks() view returns(uint64)
func (sTSettlementVault *STSettlementVault) PackMinimumClaimTTLBlocks() []byte {
	enc, err := sTSettlementVault.abi.Pack("minimumClaimTTLBlocks")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackMinimumClaimTTLBlocks is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x1a56fbd7.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function minimumClaimTTLBlocks() view returns(uint64)
func (sTSettlementVault *STSettlementVault) TryPackMinimumClaimTTLBlocks() ([]byte, error) {
	return sTSettlementVault.abi.Pack("minimumClaimTTLBlocks")
}

// UnpackMinimumClaimTTLBlocks is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x1a56fbd7.
//
// Solidity: function minimumClaimTTLBlocks() view returns(uint64)
func (sTSettlementVault *STSettlementVault) UnpackMinimumClaimTTLBlocks(data []byte) (uint64, error) {
	out, err := sTSettlementVault.abi.Unpack("minimumClaimTTLBlocks", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackMinimumTransferTaoRao is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x47dd1c15.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function minimumTransferTaoRao() view returns(uint64)
func (sTSettlementVault *STSettlementVault) PackMinimumTransferTaoRao() []byte {
	enc, err := sTSettlementVault.abi.Pack("minimumTransferTaoRao")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackMinimumTransferTaoRao is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x47dd1c15.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function minimumTransferTaoRao() view returns(uint64)
func (sTSettlementVault *STSettlementVault) TryPackMinimumTransferTaoRao() ([]byte, error) {
	return sTSettlementVault.abi.Pack("minimumTransferTaoRao")
}

// UnpackMinimumTransferTaoRao is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x47dd1c15.
//
// Solidity: function minimumTransferTaoRao() view returns(uint64)
func (sTSettlementVault *STSettlementVault) UnpackMinimumTransferTaoRao(data []byte) (uint64, error) {
	out, err := sTSettlementVault.abi.Unpack("minimumTransferTaoRao", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackNetuid is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe78015b1.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function netuid() view returns(uint16)
func (sTSettlementVault *STSettlementVault) PackNetuid() []byte {
	enc, err := sTSettlementVault.abi.Pack("netuid")
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
func (sTSettlementVault *STSettlementVault) TryPackNetuid() ([]byte, error) {
	return sTSettlementVault.abi.Pack("netuid")
}

// UnpackNetuid is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xe78015b1.
//
// Solidity: function netuid() view returns(uint16)
func (sTSettlementVault *STSettlementVault) UnpackNetuid(data []byte) (uint16, error) {
	out, err := sTSettlementVault.abi.Unpack("netuid", data)
	if err != nil {
		return *new(uint16), err
	}
	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)
	return out0, nil
}

// PackOutstandingLiability is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x536c9fe4.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function outstandingLiability() view returns(uint256)
func (sTSettlementVault *STSettlementVault) PackOutstandingLiability() []byte {
	enc, err := sTSettlementVault.abi.Pack("outstandingLiability")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackOutstandingLiability is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x536c9fe4.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function outstandingLiability() view returns(uint256)
func (sTSettlementVault *STSettlementVault) TryPackOutstandingLiability() ([]byte, error) {
	return sTSettlementVault.abi.Pack("outstandingLiability")
}

// UnpackOutstandingLiability is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x536c9fe4.
//
// Solidity: function outstandingLiability() view returns(uint256)
func (sTSettlementVault *STSettlementVault) UnpackOutstandingLiability(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault.abi.Unpack("outstandingLiability", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackPayoutLeaf is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc186080a.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function payoutLeaf(bytes32 coldkey, uint256 shareBps) pure returns(bytes32)
func (sTSettlementVault *STSettlementVault) PackPayoutLeaf(coldkey [32]byte, shareBps *big.Int) []byte {
	enc, err := sTSettlementVault.abi.Pack("payoutLeaf", coldkey, shareBps)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPayoutLeaf is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc186080a.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function payoutLeaf(bytes32 coldkey, uint256 shareBps) pure returns(bytes32)
func (sTSettlementVault *STSettlementVault) TryPackPayoutLeaf(coldkey [32]byte, shareBps *big.Int) ([]byte, error) {
	return sTSettlementVault.abi.Pack("payoutLeaf", coldkey, shareBps)
}

// UnpackPayoutLeaf is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xc186080a.
//
// Solidity: function payoutLeaf(bytes32 coldkey, uint256 shareBps) pure returns(bytes32)
func (sTSettlementVault *STSettlementVault) UnpackPayoutLeaf(data []byte) ([32]byte, error) {
	out, err := sTSettlementVault.abi.Unpack("payoutLeaf", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackPendingFunding is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x636b7e56.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function pendingFunding() view returns(uint256)
func (sTSettlementVault *STSettlementVault) PackPendingFunding() []byte {
	enc, err := sTSettlementVault.abi.Pack("pendingFunding")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPendingFunding is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x636b7e56.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function pendingFunding() view returns(uint256)
func (sTSettlementVault *STSettlementVault) TryPackPendingFunding() ([]byte, error) {
	return sTSettlementVault.abi.Pack("pendingFunding")
}

// UnpackPendingFunding is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x636b7e56.
//
// Solidity: function pendingFunding() view returns(uint256)
func (sTSettlementVault *STSettlementVault) UnpackPendingFunding(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault.abi.Unpack("pendingFunding", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackPoolHotkeyUsed is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x411e90d5.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function poolHotkeyUsed(bytes32 hotkey) view returns(bool used)
func (sTSettlementVault *STSettlementVault) PackPoolHotkeyUsed(hotkey [32]byte) []byte {
	enc, err := sTSettlementVault.abi.Pack("poolHotkeyUsed", hotkey)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPoolHotkeyUsed is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x411e90d5.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function poolHotkeyUsed(bytes32 hotkey) view returns(bool used)
func (sTSettlementVault *STSettlementVault) TryPackPoolHotkeyUsed(hotkey [32]byte) ([]byte, error) {
	return sTSettlementVault.abi.Pack("poolHotkeyUsed", hotkey)
}

// UnpackPoolHotkeyUsed is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x411e90d5.
//
// Solidity: function poolHotkeyUsed(bytes32 hotkey) view returns(bool used)
func (sTSettlementVault *STSettlementVault) UnpackPoolHotkeyUsed(data []byte) (bool, error) {
	out, err := sTSettlementVault.abi.Unpack("poolHotkeyUsed", data)
	if err != nil {
		return *new(bool), err
	}
	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)
	return out0, nil
}

// PackPools is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xac4afa38.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function pools(uint256 noId) view returns(bytes32 hotkey, uint16 uid, bool active)
func (sTSettlementVault *STSettlementVault) PackPools(noId *big.Int) []byte {
	enc, err := sTSettlementVault.abi.Pack("pools", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPools is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xac4afa38.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function pools(uint256 noId) view returns(bytes32 hotkey, uint16 uid, bool active)
func (sTSettlementVault *STSettlementVault) TryPackPools(noId *big.Int) ([]byte, error) {
	return sTSettlementVault.abi.Pack("pools", noId)
}

// PoolsOutput serves as a container for the return parameters of contract
// method Pools.
type PoolsOutput struct {
	Hotkey [32]byte
	Uid    uint16
	Active bool
}

// UnpackPools is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xac4afa38.
//
// Solidity: function pools(uint256 noId) view returns(bytes32 hotkey, uint16 uid, bool active)
func (sTSettlementVault *STSettlementVault) UnpackPools(data []byte) (PoolsOutput, error) {
	out, err := sTSettlementVault.abi.Unpack("pools", data)
	outstruct := new(PoolsOutput)
	if err != nil {
		return *outstruct, err
	}
	outstruct.Hotkey = *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	outstruct.Uid = *abi.ConvertType(out[1], new(uint16)).(*uint16)
	outstruct.Active = *abi.ConvertType(out[2], new(bool)).(*bool)
	return *outstruct, nil
}

// PackRegisterEscrow is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x775d2762.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function registerEscrow(uint64 maximumBurnRao) payable returns(uint16 uid)
func (sTSettlementVault *STSettlementVault) PackRegisterEscrow(maximumBurnRao uint64) []byte {
	enc, err := sTSettlementVault.abi.Pack("registerEscrow", maximumBurnRao)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRegisterEscrow is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x775d2762.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function registerEscrow(uint64 maximumBurnRao) payable returns(uint16 uid)
func (sTSettlementVault *STSettlementVault) TryPackRegisterEscrow(maximumBurnRao uint64) ([]byte, error) {
	return sTSettlementVault.abi.Pack("registerEscrow", maximumBurnRao)
}

// UnpackRegisterEscrow is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x775d2762.
//
// Solidity: function registerEscrow(uint64 maximumBurnRao) payable returns(uint16 uid)
func (sTSettlementVault *STSettlementVault) UnpackRegisterEscrow(data []byte) (uint16, error) {
	out, err := sTSettlementVault.abi.Unpack("registerEscrow", data)
	if err != nil {
		return *new(uint16), err
	}
	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)
	return out0, nil
}

// PackRegisterPool is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x431439d8.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function registerPool(uint256 noId, bytes32 poolHotkey, uint64 maximumBurnRao) payable returns(uint16 uid)
func (sTSettlementVault *STSettlementVault) PackRegisterPool(noId *big.Int, poolHotkey [32]byte, maximumBurnRao uint64) []byte {
	enc, err := sTSettlementVault.abi.Pack("registerPool", noId, poolHotkey, maximumBurnRao)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRegisterPool is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x431439d8.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function registerPool(uint256 noId, bytes32 poolHotkey, uint64 maximumBurnRao) payable returns(uint16 uid)
func (sTSettlementVault *STSettlementVault) TryPackRegisterPool(noId *big.Int, poolHotkey [32]byte, maximumBurnRao uint64) ([]byte, error) {
	return sTSettlementVault.abi.Pack("registerPool", noId, poolHotkey, maximumBurnRao)
}

// UnpackRegisterPool is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x431439d8.
//
// Solidity: function registerPool(uint256 noId, bytes32 poolHotkey, uint64 maximumBurnRao) payable returns(uint16 uid)
func (sTSettlementVault *STSettlementVault) UnpackRegisterPool(data []byte) (uint16, error) {
	out, err := sTSettlementVault.abi.Unpack("registerPool", data)
	if err != nil {
		return *new(uint16), err
	}
	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)
	return out0, nil
}

// PackSelfColdkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x877e4394.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function selfColdkey() view returns(bytes32)
func (sTSettlementVault *STSettlementVault) PackSelfColdkey() []byte {
	enc, err := sTSettlementVault.abi.Pack("selfColdkey")
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
func (sTSettlementVault *STSettlementVault) TryPackSelfColdkey() ([]byte, error) {
	return sTSettlementVault.abi.Pack("selfColdkey")
}

// UnpackSelfColdkey is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x877e4394.
//
// Solidity: function selfColdkey() view returns(bytes32)
func (sTSettlementVault *STSettlementVault) UnpackSelfColdkey(data []byte) ([32]byte, error) {
	out, err := sTSettlementVault.abi.Unpack("selfColdkey", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackSetCoordinatorOnce is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf406b76b.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function setCoordinatorOnce(address coordinator_) returns()
func (sTSettlementVault *STSettlementVault) PackSetCoordinatorOnce(coordinator common.Address) []byte {
	enc, err := sTSettlementVault.abi.Pack("setCoordinatorOnce", coordinator)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSetCoordinatorOnce is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf406b76b.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function setCoordinatorOnce(address coordinator_) returns()
func (sTSettlementVault *STSettlementVault) TryPackSetCoordinatorOnce(coordinator common.Address) ([]byte, error) {
	return sTSettlementVault.abi.Pack("setCoordinatorOnce", coordinator)
}

// PackSetPoolActive is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xea17fa93.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function setPoolActive(uint256 noId, bool active) returns()
func (sTSettlementVault *STSettlementVault) PackSetPoolActive(noId *big.Int, active bool) []byte {
	enc, err := sTSettlementVault.abi.Pack("setPoolActive", noId, active)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSetPoolActive is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xea17fa93.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function setPoolActive(uint256 noId, bool active) returns()
func (sTSettlementVault *STSettlementVault) TryPackSetPoolActive(noId *big.Int, active bool) ([]byte, error) {
	return sTSettlementVault.abi.Pack("setPoolActive", noId, active)
}

// PackTotalCaptured is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x886f9669.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function totalCaptured() view returns(uint256)
func (sTSettlementVault *STSettlementVault) PackTotalCaptured() []byte {
	enc, err := sTSettlementVault.abi.Pack("totalCaptured")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackTotalCaptured is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x886f9669.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function totalCaptured() view returns(uint256)
func (sTSettlementVault *STSettlementVault) TryPackTotalCaptured() ([]byte, error) {
	return sTSettlementVault.abi.Pack("totalCaptured")
}

// UnpackTotalCaptured is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x886f9669.
//
// Solidity: function totalCaptured() view returns(uint256)
func (sTSettlementVault *STSettlementVault) UnpackTotalCaptured(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault.abi.Unpack("totalCaptured", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackTotalPaid is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe7b0f666.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function totalPaid() view returns(uint256)
func (sTSettlementVault *STSettlementVault) PackTotalPaid() []byte {
	enc, err := sTSettlementVault.abi.Pack("totalPaid")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackTotalPaid is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe7b0f666.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function totalPaid() view returns(uint256)
func (sTSettlementVault *STSettlementVault) TryPackTotalPaid() ([]byte, error) {
	return sTSettlementVault.abi.Pack("totalPaid")
}

// UnpackTotalPaid is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xe7b0f666.
//
// Solidity: function totalPaid() view returns(uint256)
func (sTSettlementVault *STSettlementVault) UnpackTotalPaid(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault.abi.Unpack("totalPaid", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackWithdrawClaimCredit is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xac399a93.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function withdrawClaimCredit(bytes32 coldkey) returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) PackWithdrawClaimCredit(coldkey [32]byte) []byte {
	enc, err := sTSettlementVault.abi.Pack("withdrawClaimCredit", coldkey)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackWithdrawClaimCredit is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xac399a93.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function withdrawClaimCredit(bytes32 coldkey) returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) TryPackWithdrawClaimCredit(coldkey [32]byte) ([]byte, error) {
	return sTSettlementVault.abi.Pack("withdrawClaimCredit", coldkey)
}

// UnpackWithdrawClaimCredit is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xac399a93.
//
// Solidity: function withdrawClaimCredit(bytes32 coldkey) returns(uint256 amount)
func (sTSettlementVault *STSettlementVault) UnpackWithdrawClaimCredit(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault.abi.Unpack("withdrawClaimCredit", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// STSettlementVaultClaimPaid represents a ClaimPaid event raised by the STSettlementVault contract.
type STSettlementVaultClaimPaid struct {
	Coldkey [32]byte
	Amount  *big.Int
	Relayer common.Address
	Raw     *types.Log // Blockchain specific contextual infos
}

const STSettlementVaultClaimPaidEventName = "ClaimPaid"

// ContractEventName returns the user-defined event name.
func (STSettlementVaultClaimPaid) ContractEventName() string {
	return STSettlementVaultClaimPaidEventName
}

// UnpackClaimPaidEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event ClaimPaid(bytes32 indexed coldkey, uint256 amount, address indexed relayer)
func (sTSettlementVault *STSettlementVault) UnpackClaimPaidEvent(log *types.Log) (*STSettlementVaultClaimPaid, error) {
	event := "ClaimPaid"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVaultClaimPaid)
	if len(log.Data) > 0 {
		if err := sTSettlementVault.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault.abi.Events[event].Inputs {
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

// STSettlementVaultClaimPaymentDeferred represents a ClaimPaymentDeferred event raised by the STSettlementVault contract.
type STSettlementVaultClaimPaymentDeferred struct {
	Coldkey               [32]byte
	CreditAlphaRao        *big.Int
	TaoEquivalentRao      *big.Int
	MinimumTransferTaoRao uint64
	Reason                uint8
	Raw                   *types.Log // Blockchain specific contextual infos
}

const STSettlementVaultClaimPaymentDeferredEventName = "ClaimPaymentDeferred"

// ContractEventName returns the user-defined event name.
func (STSettlementVaultClaimPaymentDeferred) ContractEventName() string {
	return STSettlementVaultClaimPaymentDeferredEventName
}

// UnpackClaimPaymentDeferredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event ClaimPaymentDeferred(bytes32 indexed coldkey, uint256 creditAlphaRao, uint256 taoEquivalentRao, uint64 minimumTransferTaoRao, uint8 reason)
func (sTSettlementVault *STSettlementVault) UnpackClaimPaymentDeferredEvent(log *types.Log) (*STSettlementVaultClaimPaymentDeferred, error) {
	event := "ClaimPaymentDeferred"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVaultClaimPaymentDeferred)
	if len(log.Data) > 0 {
		if err := sTSettlementVault.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault.abi.Events[event].Inputs {
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

// STSettlementVaultClaimed represents a Claimed event raised by the STSettlementVault contract.
type STSettlementVaultClaimed struct {
	Epoch    *big.Int
	NoId     *big.Int
	Coldkey  [32]byte
	ShareBps *big.Int
	Amount   *big.Int
	Relayer  common.Address
	Raw      *types.Log // Blockchain specific contextual infos
}

const STSettlementVaultClaimedEventName = "Claimed"

// ContractEventName returns the user-defined event name.
func (STSettlementVaultClaimed) ContractEventName() string {
	return STSettlementVaultClaimedEventName
}

// UnpackClaimedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event Claimed(uint256 indexed epoch, uint256 indexed noId, bytes32 indexed coldkey, uint256 shareBps, uint256 amount, address relayer)
func (sTSettlementVault *STSettlementVault) UnpackClaimedEvent(log *types.Log) (*STSettlementVaultClaimed, error) {
	event := "Claimed"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVaultClaimed)
	if len(log.Data) > 0 {
		if err := sTSettlementVault.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault.abi.Events[event].Inputs {
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

// STSettlementVaultCoordinatorFixed represents a CoordinatorFixed event raised by the STSettlementVault contract.
type STSettlementVaultCoordinatorFixed struct {
	Coordinator common.Address
	Raw         *types.Log // Blockchain specific contextual infos
}

const STSettlementVaultCoordinatorFixedEventName = "CoordinatorFixed"

// ContractEventName returns the user-defined event name.
func (STSettlementVaultCoordinatorFixed) ContractEventName() string {
	return STSettlementVaultCoordinatorFixedEventName
}

// UnpackCoordinatorFixedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event CoordinatorFixed(address indexed coordinator)
func (sTSettlementVault *STSettlementVault) UnpackCoordinatorFixedEvent(log *types.Log) (*STSettlementVaultCoordinatorFixed, error) {
	event := "CoordinatorFixed"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVaultCoordinatorFixed)
	if len(log.Data) > 0 {
		if err := sTSettlementVault.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault.abi.Events[event].Inputs {
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

// STSettlementVaultEmissionCaptured represents a EmissionCaptured event raised by the STSettlementVault contract.
type STSettlementVaultEmissionCaptured struct {
	Epoch      *big.Int
	NoId       *big.Int
	PoolHotkey [32]byte
	Amount     *big.Int
	Raw        *types.Log // Blockchain specific contextual infos
}

const STSettlementVaultEmissionCapturedEventName = "EmissionCaptured"

// ContractEventName returns the user-defined event name.
func (STSettlementVaultEmissionCaptured) ContractEventName() string {
	return STSettlementVaultEmissionCapturedEventName
}

// UnpackEmissionCapturedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event EmissionCaptured(uint256 indexed epoch, uint256 indexed noId, bytes32 indexed poolHotkey, uint256 amount)
func (sTSettlementVault *STSettlementVault) UnpackEmissionCapturedEvent(log *types.Log) (*STSettlementVaultEmissionCaptured, error) {
	event := "EmissionCaptured"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVaultEmissionCaptured)
	if len(log.Data) > 0 {
		if err := sTSettlementVault.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault.abi.Events[event].Inputs {
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

// STSettlementVaultEmissionDeferred represents a EmissionDeferred event raised by the STSettlementVault contract.
type STSettlementVaultEmissionDeferred struct {
	Epoch *big.Int
	NoId  *big.Int
	Raw   *types.Log // Blockchain specific contextual infos
}

const STSettlementVaultEmissionDeferredEventName = "EmissionDeferred"

// ContractEventName returns the user-defined event name.
func (STSettlementVaultEmissionDeferred) ContractEventName() string {
	return STSettlementVaultEmissionDeferredEventName
}

// UnpackEmissionDeferredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event EmissionDeferred(uint256 indexed epoch, uint256 indexed noId)
func (sTSettlementVault *STSettlementVault) UnpackEmissionDeferredEvent(log *types.Log) (*STSettlementVaultEmissionDeferred, error) {
	event := "EmissionDeferred"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVaultEmissionDeferred)
	if len(log.Data) > 0 {
		if err := sTSettlementVault.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault.abi.Events[event].Inputs {
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

// STSettlementVaultEmissionDustDeferred represents a EmissionDustDeferred event raised by the STSettlementVault contract.
type STSettlementVaultEmissionDustDeferred struct {
	Epoch                 *big.Int
	NoId                  *big.Int
	PoolHotkey            [32]byte
	ObservedAlphaRao      *big.Int
	TaoEquivalentRao      *big.Int
	MinimumTransferTaoRao uint64
	Raw                   *types.Log // Blockchain specific contextual infos
}

const STSettlementVaultEmissionDustDeferredEventName = "EmissionDustDeferred"

// ContractEventName returns the user-defined event name.
func (STSettlementVaultEmissionDustDeferred) ContractEventName() string {
	return STSettlementVaultEmissionDustDeferredEventName
}

// UnpackEmissionDustDeferredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event EmissionDustDeferred(uint256 indexed epoch, uint256 indexed noId, bytes32 indexed poolHotkey, uint256 observedAlphaRao, uint256 taoEquivalentRao, uint64 minimumTransferTaoRao)
func (sTSettlementVault *STSettlementVault) UnpackEmissionDustDeferredEvent(log *types.Log) (*STSettlementVaultEmissionDustDeferred, error) {
	event := "EmissionDustDeferred"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVaultEmissionDustDeferred)
	if len(log.Data) > 0 {
		if err := sTSettlementVault.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault.abi.Events[event].Inputs {
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

// STSettlementVaultEntitlementExpired represents a EntitlementExpired event raised by the STSettlementVault contract.
type STSettlementVaultEntitlementExpired struct {
	Epoch         *big.Int
	NoId          *big.Int
	Unclaimed     *big.Int
	OperatorCarry *big.Int
	Raw           *types.Log // Blockchain specific contextual infos
}

const STSettlementVaultEntitlementExpiredEventName = "EntitlementExpired"

// ContractEventName returns the user-defined event name.
func (STSettlementVaultEntitlementExpired) ContractEventName() string {
	return STSettlementVaultEntitlementExpiredEventName
}

// UnpackEntitlementExpiredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event EntitlementExpired(uint256 indexed epoch, uint256 indexed noId, uint256 unclaimed, uint256 operatorCarry)
func (sTSettlementVault *STSettlementVault) UnpackEntitlementExpiredEvent(log *types.Log) (*STSettlementVaultEntitlementExpired, error) {
	event := "EntitlementExpired"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVaultEntitlementExpired)
	if len(log.Data) > 0 {
		if err := sTSettlementVault.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault.abi.Events[event].Inputs {
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

// STSettlementVaultEntitlementFinalized represents a EntitlementFinalized event raised by the STSettlementVault contract.
type STSettlementVaultEntitlementFinalized struct {
	Epoch        *big.Int
	NoId         *big.Int
	PayoutRoot   [32]byte
	ArtifactHash [32]byte
	Total        *big.Int
	ExpiryBlock  uint64
	Raw          *types.Log // Blockchain specific contextual infos
}

const STSettlementVaultEntitlementFinalizedEventName = "EntitlementFinalized"

// ContractEventName returns the user-defined event name.
func (STSettlementVaultEntitlementFinalized) ContractEventName() string {
	return STSettlementVaultEntitlementFinalizedEventName
}

// UnpackEntitlementFinalizedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event EntitlementFinalized(uint256 indexed epoch, uint256 indexed noId, bytes32 payoutRoot, bytes32 artifactHash, uint256 total, uint64 expiryBlock)
func (sTSettlementVault *STSettlementVault) UnpackEntitlementFinalizedEvent(log *types.Log) (*STSettlementVaultEntitlementFinalized, error) {
	event := "EntitlementFinalized"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVaultEntitlementFinalized)
	if len(log.Data) > 0 {
		if err := sTSettlementVault.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault.abi.Events[event].Inputs {
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

// STSettlementVaultEscrowRegistered represents a EscrowRegistered event raised by the STSettlementVault contract.
type STSettlementVaultEscrowRegistered struct {
	Hotkey [32]byte
	Uid    uint16
	Raw    *types.Log // Blockchain specific contextual infos
}

const STSettlementVaultEscrowRegisteredEventName = "EscrowRegistered"

// ContractEventName returns the user-defined event name.
func (STSettlementVaultEscrowRegistered) ContractEventName() string {
	return STSettlementVaultEscrowRegisteredEventName
}

// UnpackEscrowRegisteredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event EscrowRegistered(bytes32 indexed hotkey, uint16 uid)
func (sTSettlementVault *STSettlementVault) UnpackEscrowRegisteredEvent(log *types.Log) (*STSettlementVaultEscrowRegistered, error) {
	event := "EscrowRegistered"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVaultEscrowRegistered)
	if len(log.Data) > 0 {
		if err := sTSettlementVault.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault.abi.Events[event].Inputs {
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

// STSettlementVaultPoolActiveSet represents a PoolActiveSet event raised by the STSettlementVault contract.
type STSettlementVaultPoolActiveSet struct {
	NoId   *big.Int
	Active bool
	Raw    *types.Log // Blockchain specific contextual infos
}

const STSettlementVaultPoolActiveSetEventName = "PoolActiveSet"

// ContractEventName returns the user-defined event name.
func (STSettlementVaultPoolActiveSet) ContractEventName() string {
	return STSettlementVaultPoolActiveSetEventName
}

// UnpackPoolActiveSetEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event PoolActiveSet(uint256 indexed noId, bool active)
func (sTSettlementVault *STSettlementVault) UnpackPoolActiveSetEvent(log *types.Log) (*STSettlementVaultPoolActiveSet, error) {
	event := "PoolActiveSet"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVaultPoolActiveSet)
	if len(log.Data) > 0 {
		if err := sTSettlementVault.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault.abi.Events[event].Inputs {
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

// STSettlementVaultPoolRegistered represents a PoolRegistered event raised by the STSettlementVault contract.
type STSettlementVaultPoolRegistered struct {
	NoId   *big.Int
	Hotkey [32]byte
	Uid    uint16
	Raw    *types.Log // Blockchain specific contextual infos
}

const STSettlementVaultPoolRegisteredEventName = "PoolRegistered"

// ContractEventName returns the user-defined event name.
func (STSettlementVaultPoolRegistered) ContractEventName() string {
	return STSettlementVaultPoolRegisteredEventName
}

// UnpackPoolRegisteredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event PoolRegistered(uint256 indexed noId, bytes32 indexed hotkey, uint16 uid)
func (sTSettlementVault *STSettlementVault) UnpackPoolRegisteredEvent(log *types.Log) (*STSettlementVaultPoolRegistered, error) {
	event := "PoolRegistered"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVaultPoolRegistered)
	if len(log.Data) > 0 {
		if err := sTSettlementVault.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault.abi.Events[event].Inputs {
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

// STSettlementVaultRootMissed represents a RootMissed event raised by the STSettlementVault contract.
type STSettlementVaultRootMissed struct {
	Epoch   *big.Int
	NoId    *big.Int
	Carried *big.Int
	Raw     *types.Log // Blockchain specific contextual infos
}

const STSettlementVaultRootMissedEventName = "RootMissed"

// ContractEventName returns the user-defined event name.
func (STSettlementVaultRootMissed) ContractEventName() string {
	return STSettlementVaultRootMissedEventName
}

// UnpackRootMissedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event RootMissed(uint256 indexed epoch, uint256 indexed noId, uint256 carried)
func (sTSettlementVault *STSettlementVault) UnpackRootMissedEvent(log *types.Log) (*STSettlementVaultRootMissed, error) {
	event := "RootMissed"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVaultRootMissed)
	if len(log.Data) > 0 {
		if err := sTSettlementVault.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault.abi.Events[event].Inputs {
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
func (sTSettlementVault *STSettlementVault) UnpackError(raw []byte) (any, error) {
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["AlreadyClaimed"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackAlreadyClaimedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["AlreadyInitialized"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackAlreadyInitializedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["ClaimExpired"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackClaimExpiredError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["InvalidConfiguration"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackInvalidConfigurationError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["InvalidProof"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackInvalidProofError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["InvalidTransition"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackInvalidTransitionError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["NativeRefundFailed"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackNativeRefundFailedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["NothingToWithdraw"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackNothingToWithdrawError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["Reentrancy"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackReentrancyError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["RuntimeAccountingMismatch"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackRuntimeAccountingMismatchError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["RuntimePriceUnavailable"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackRuntimePriceUnavailableError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["RuntimeTransferFailed"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackRuntimeTransferFailedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["TransferBelowMinimum"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackTransferBelowMinimumError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["Unauthorized"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackUnauthorizedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["Underfunded"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackUnderfundedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault.abi.Errors["UnknownPool"].ID.Bytes()[:4]) {
		return sTSettlementVault.UnpackUnknownPoolError(raw[4:])
	}
	return nil, errors.New("Unknown error")
}

// STSettlementVaultAlreadyClaimed represents a AlreadyClaimed error raised by the STSettlementVault contract.
type STSettlementVaultAlreadyClaimed struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error AlreadyClaimed()
func STSettlementVaultAlreadyClaimedErrorID() common.Hash {
	return common.HexToHash("0x646cf558a545d59f8a09cbf8a0eb8a9332f1d17834843b20fc8d154839dc46d7")
}

// UnpackAlreadyClaimedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error AlreadyClaimed()
func (sTSettlementVault *STSettlementVault) UnpackAlreadyClaimedError(raw []byte) (*STSettlementVaultAlreadyClaimed, error) {
	out := new(STSettlementVaultAlreadyClaimed)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "AlreadyClaimed", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultAlreadyInitialized represents a AlreadyInitialized error raised by the STSettlementVault contract.
type STSettlementVaultAlreadyInitialized struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error AlreadyInitialized()
func STSettlementVaultAlreadyInitializedErrorID() common.Hash {
	return common.HexToHash("0x0dc149f07762891dbcea3fe72770f3d63a1863fc54b2f084e8c59ec476996927")
}

// UnpackAlreadyInitializedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error AlreadyInitialized()
func (sTSettlementVault *STSettlementVault) UnpackAlreadyInitializedError(raw []byte) (*STSettlementVaultAlreadyInitialized, error) {
	out := new(STSettlementVaultAlreadyInitialized)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "AlreadyInitialized", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultClaimExpired represents a ClaimExpired error raised by the STSettlementVault contract.
type STSettlementVaultClaimExpired struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error ClaimExpired()
func STSettlementVaultClaimExpiredErrorID() common.Hash {
	return common.HexToHash("0x82a49d9e1a771843d39e8826b2cc5ec620f1a84fb3845ddd134da6fe9b0b747c")
}

// UnpackClaimExpiredError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error ClaimExpired()
func (sTSettlementVault *STSettlementVault) UnpackClaimExpiredError(raw []byte) (*STSettlementVaultClaimExpired, error) {
	out := new(STSettlementVaultClaimExpired)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "ClaimExpired", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultInvalidConfiguration represents a InvalidConfiguration error raised by the STSettlementVault contract.
type STSettlementVaultInvalidConfiguration struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidConfiguration()
func STSettlementVaultInvalidConfigurationErrorID() common.Hash {
	return common.HexToHash("0xc52a9bd3d9e475b9056a93172ef6968d775a7cd41c4255bbebf12e90a5fbbd39")
}

// UnpackInvalidConfigurationError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidConfiguration()
func (sTSettlementVault *STSettlementVault) UnpackInvalidConfigurationError(raw []byte) (*STSettlementVaultInvalidConfiguration, error) {
	out := new(STSettlementVaultInvalidConfiguration)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "InvalidConfiguration", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultInvalidProof represents a InvalidProof error raised by the STSettlementVault contract.
type STSettlementVaultInvalidProof struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidProof()
func STSettlementVaultInvalidProofErrorID() common.Hash {
	return common.HexToHash("0x09bde339c6b182be216ee7ef8ccff6338c6ef7993445216112ae575c5438fd27")
}

// UnpackInvalidProofError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidProof()
func (sTSettlementVault *STSettlementVault) UnpackInvalidProofError(raw []byte) (*STSettlementVaultInvalidProof, error) {
	out := new(STSettlementVaultInvalidProof)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "InvalidProof", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultInvalidTransition represents a InvalidTransition error raised by the STSettlementVault contract.
type STSettlementVaultInvalidTransition struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidTransition()
func STSettlementVaultInvalidTransitionErrorID() common.Hash {
	return common.HexToHash("0xa6532e5d22b2d9016a3c919977e18f1cf795d3c7af3b6848d32d8b9403d143e1")
}

// UnpackInvalidTransitionError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidTransition()
func (sTSettlementVault *STSettlementVault) UnpackInvalidTransitionError(raw []byte) (*STSettlementVaultInvalidTransition, error) {
	out := new(STSettlementVaultInvalidTransition)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "InvalidTransition", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultNativeRefundFailed represents a NativeRefundFailed error raised by the STSettlementVault contract.
type STSettlementVaultNativeRefundFailed struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NativeRefundFailed()
func STSettlementVaultNativeRefundFailedErrorID() common.Hash {
	return common.HexToHash("0x8520d710691d90175ff6d0cde74ada1f9d91ac205406750a2381430c693c006e")
}

// UnpackNativeRefundFailedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NativeRefundFailed()
func (sTSettlementVault *STSettlementVault) UnpackNativeRefundFailedError(raw []byte) (*STSettlementVaultNativeRefundFailed, error) {
	out := new(STSettlementVaultNativeRefundFailed)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "NativeRefundFailed", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultNothingToWithdraw represents a NothingToWithdraw error raised by the STSettlementVault contract.
type STSettlementVaultNothingToWithdraw struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NothingToWithdraw()
func STSettlementVaultNothingToWithdrawErrorID() common.Hash {
	return common.HexToHash("0xd0d04f60bf4f7629141a1f00f5d2908fa0f3e15bf4cbf8bb8edc6fcbdf2509fa")
}

// UnpackNothingToWithdrawError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NothingToWithdraw()
func (sTSettlementVault *STSettlementVault) UnpackNothingToWithdrawError(raw []byte) (*STSettlementVaultNothingToWithdraw, error) {
	out := new(STSettlementVaultNothingToWithdraw)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "NothingToWithdraw", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultReentrancy represents a Reentrancy error raised by the STSettlementVault contract.
type STSettlementVaultReentrancy struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error Reentrancy()
func STSettlementVaultReentrancyErrorID() common.Hash {
	return common.HexToHash("0xab143c06c9772d69bbbc9f2fe74acd02f810e93b099f3d1dac8448ac9ae35991")
}

// UnpackReentrancyError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error Reentrancy()
func (sTSettlementVault *STSettlementVault) UnpackReentrancyError(raw []byte) (*STSettlementVaultReentrancy, error) {
	out := new(STSettlementVaultReentrancy)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "Reentrancy", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultRuntimeAccountingMismatch represents a RuntimeAccountingMismatch error raised by the STSettlementVault contract.
type STSettlementVaultRuntimeAccountingMismatch struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error RuntimeAccountingMismatch()
func STSettlementVaultRuntimeAccountingMismatchErrorID() common.Hash {
	return common.HexToHash("0x2bd1ed7d24b1da053721d64e4a287a7ad1b37138c31a5847af6e59aa54f85b99")
}

// UnpackRuntimeAccountingMismatchError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error RuntimeAccountingMismatch()
func (sTSettlementVault *STSettlementVault) UnpackRuntimeAccountingMismatchError(raw []byte) (*STSettlementVaultRuntimeAccountingMismatch, error) {
	out := new(STSettlementVaultRuntimeAccountingMismatch)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "RuntimeAccountingMismatch", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultRuntimePriceUnavailable represents a RuntimePriceUnavailable error raised by the STSettlementVault contract.
type STSettlementVaultRuntimePriceUnavailable struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error RuntimePriceUnavailable()
func STSettlementVaultRuntimePriceUnavailableErrorID() common.Hash {
	return common.HexToHash("0x98e10d492b6d7b5cd393d24709c2115856b7756ed523388723f84fdbebd418cf")
}

// UnpackRuntimePriceUnavailableError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error RuntimePriceUnavailable()
func (sTSettlementVault *STSettlementVault) UnpackRuntimePriceUnavailableError(raw []byte) (*STSettlementVaultRuntimePriceUnavailable, error) {
	out := new(STSettlementVaultRuntimePriceUnavailable)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "RuntimePriceUnavailable", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultRuntimeTransferFailed represents a RuntimeTransferFailed error raised by the STSettlementVault contract.
type STSettlementVaultRuntimeTransferFailed struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error RuntimeTransferFailed()
func STSettlementVaultRuntimeTransferFailedErrorID() common.Hash {
	return common.HexToHash("0xfa2bac2e575840b1dbf852e8ecb073f029604bfc6557eaeb107938d8376c8f5b")
}

// UnpackRuntimeTransferFailedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error RuntimeTransferFailed()
func (sTSettlementVault *STSettlementVault) UnpackRuntimeTransferFailedError(raw []byte) (*STSettlementVaultRuntimeTransferFailed, error) {
	out := new(STSettlementVaultRuntimeTransferFailed)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "RuntimeTransferFailed", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultTransferBelowMinimum represents a TransferBelowMinimum error raised by the STSettlementVault contract.
type STSettlementVaultTransferBelowMinimum struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error TransferBelowMinimum()
func STSettlementVaultTransferBelowMinimumErrorID() common.Hash {
	return common.HexToHash("0xc6634409b9e04ae00ba354382b49127bb433f2ce702821fb0ca1f58e992d0387")
}

// UnpackTransferBelowMinimumError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error TransferBelowMinimum()
func (sTSettlementVault *STSettlementVault) UnpackTransferBelowMinimumError(raw []byte) (*STSettlementVaultTransferBelowMinimum, error) {
	out := new(STSettlementVaultTransferBelowMinimum)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "TransferBelowMinimum", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultUnauthorized represents a Unauthorized error raised by the STSettlementVault contract.
type STSettlementVaultUnauthorized struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error Unauthorized()
func STSettlementVaultUnauthorizedErrorID() common.Hash {
	return common.HexToHash("0x82b4290015f7ec7256ca2a6247d3c2a89c4865c0e791456df195f40ad0a81367")
}

// UnpackUnauthorizedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error Unauthorized()
func (sTSettlementVault *STSettlementVault) UnpackUnauthorizedError(raw []byte) (*STSettlementVaultUnauthorized, error) {
	out := new(STSettlementVaultUnauthorized)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "Unauthorized", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultUnderfunded represents a Underfunded error raised by the STSettlementVault contract.
type STSettlementVaultUnderfunded struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error Underfunded()
func STSettlementVaultUnderfundedErrorID() common.Hash {
	return common.HexToHash("0xe1a8ca9e5b1fc9283d6e2bdee616fe535e0d88cf3046c6fbfab03e86eaa0167a")
}

// UnpackUnderfundedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error Underfunded()
func (sTSettlementVault *STSettlementVault) UnpackUnderfundedError(raw []byte) (*STSettlementVaultUnderfunded, error) {
	out := new(STSettlementVaultUnderfunded)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "Underfunded", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVaultUnknownPool represents a UnknownPool error raised by the STSettlementVault contract.
type STSettlementVaultUnknownPool struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error UnknownPool()
func STSettlementVaultUnknownPoolErrorID() common.Hash {
	return common.HexToHash("0xf7139e330b4806569379fe159fc41bed9c88b667fc4b9b3c7047d03f1a25197a")
}

// UnpackUnknownPoolError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error UnknownPool()
func (sTSettlementVault *STSettlementVault) UnpackUnknownPoolError(raw []byte) (*STSettlementVaultUnknownPool, error) {
	out := new(STSettlementVaultUnknownPool)
	if err := sTSettlementVault.abi.UnpackIntoInterface(out, "UnknownPool", raw); err != nil {
		return nil, err
	}
	return out, nil
}
