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

// ISTSettlementVault2Commitment is an auto generated low-level Go binding around an user-defined struct.
type ISTSettlementVault2Commitment struct {
	Root       [32]byte
	Total      *big.Int
	LeafCount  uint64
	LedgerHash [32]byte
}

// ISTSettlementVault2Pending is an auto generated low-level Go binding around an user-defined struct.
type ISTSettlementVault2Pending struct {
	Seq            uint64
	Root           [32]byte
	Total          *big.Int
	LeafCount      uint64
	Epoch          uint64
	LedgerHash     [32]byte
	CommittedBlock uint64
	OpenDemands    uint64
}

// ISTSettlementVault2Pool is an auto generated low-level Go binding around an user-defined struct.
type ISTSettlementVault2Pool struct {
	Hotkey     [32]byte
	FirstEpoch uint64
	Registered bool
}

// ISTSettlementVault2Root is an auto generated low-level Go binding around an user-defined struct.
type ISTSettlementVault2Root struct {
	Root       [32]byte
	Total      *big.Int
	LeafCount  uint64
	Epoch      uint64
	LedgerHash [32]byte
}

// LedgerTreeAccount is an auto generated low-level Go binding around an user-defined struct.
type LedgerTreeAccount struct {
	Index      *big.Int
	Coldkey    [32]byte
	Cumulative *big.Int
}

// LedgerTreePathNode is an auto generated low-level Go binding around an user-defined struct.
type LedgerTreePathNode struct {
	LeftHash  [32]byte
	LeftSum   *big.Int
	RightHash [32]byte
	RightSum  *big.Int
}

// LedgerTreeProofNode is an auto generated low-level Go binding around an user-defined struct.
type LedgerTreeProofNode struct {
	Hash [32]byte
	Sum  *big.Int
}

// STClaimBatcherClaim is an auto generated low-level Go binding around an user-defined struct.
type STClaimBatcherClaim struct {
	NoId      *big.Int
	RootIndex *big.Int
	Account   LedgerTreeAccount
	Proof     []LedgerTreeProofNode
}

// STClaimBatcherMetaData contains all meta data concerning the STClaimBatcher contract.
var STClaimBatcherMetaData = bind.MetaData{
	ABI: "[{\"inputs\":[],\"name\":\"BOOKKEEPING_GAS\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"contractISTSettlementVault2\",\"name\":\"vault\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"rootIndex\",\"type\":\"uint256\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"coldkey\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"cumulative\",\"type\":\"uint256\"}],\"internalType\":\"structLedgerTree.Account\",\"name\":\"account\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"hash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"sum\",\"type\":\"uint256\"}],\"internalType\":\"structLedgerTree.ProofNode[]\",\"name\":\"proof\",\"type\":\"tuple[]\"}],\"internalType\":\"structSTClaimBatcher.Claim[]\",\"name\":\"claims\",\"type\":\"tuple[]\"},{\"internalType\":\"uint256\",\"name\":\"gasPerClaim\",\"type\":\"uint256\"}],\"name\":\"claimMany\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"attempted\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"item\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"ClaimPaid\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"item\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"bytes4\",\"name\":\"selector\",\"type\":\"bytes4\"}],\"name\":\"ClaimRefused\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"item\",\"type\":\"uint256\"}],\"name\":\"ClaimsNotAttempted\",\"type\":\"event\"}]",
	ID:  "cb42d81c871579b238387dbda4591af878",
}

// STClaimBatcher is an auto generated Go binding around an Ethereum contract.
type STClaimBatcher struct {
	abi abi.ABI
}

// NewSTClaimBatcher creates a new instance of STClaimBatcher.
func NewSTClaimBatcher() *STClaimBatcher {
	parsed, err := STClaimBatcherMetaData.ParseABI()
	if err != nil {
		panic(errors.New("invalid ABI: " + err.Error()))
	}
	return &STClaimBatcher{abi: *parsed}
}

// Instance creates a wrapper for a deployed contract instance at the given address.
// Use this to create the instance object passed to abigen v2 library functions Call, Transact, etc.
func (c *STClaimBatcher) Instance(backend bind.ContractBackend, addr common.Address) *bind.BoundContract {
	return bind.NewBoundContract(addr, c.abi, backend, backend, backend)
}

// PackBOOKKEEPINGGAS is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x3ff04c8d.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function BOOKKEEPING_GAS() view returns(uint256)
func (sTClaimBatcher *STClaimBatcher) PackBOOKKEEPINGGAS() []byte {
	enc, err := sTClaimBatcher.abi.Pack("BOOKKEEPING_GAS")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackBOOKKEEPINGGAS is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x3ff04c8d.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function BOOKKEEPING_GAS() view returns(uint256)
func (sTClaimBatcher *STClaimBatcher) TryPackBOOKKEEPINGGAS() ([]byte, error) {
	return sTClaimBatcher.abi.Pack("BOOKKEEPING_GAS")
}

// UnpackBOOKKEEPINGGAS is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x3ff04c8d.
//
// Solidity: function BOOKKEEPING_GAS() view returns(uint256)
func (sTClaimBatcher *STClaimBatcher) UnpackBOOKKEEPINGGAS(data []byte) (*big.Int, error) {
	out, err := sTClaimBatcher.abi.Unpack("BOOKKEEPING_GAS", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackClaimMany is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xd77e363f.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function claimMany(address vault, (uint256,uint256,(uint256,bytes32,uint256),(bytes32,uint256)[])[] claims, uint256 gasPerClaim) returns(uint256 attempted)
func (sTClaimBatcher *STClaimBatcher) PackClaimMany(vault common.Address, claims []STClaimBatcherClaim, gasPerClaim *big.Int) []byte {
	enc, err := sTClaimBatcher.abi.Pack("claimMany", vault, claims, gasPerClaim)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackClaimMany is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xd77e363f.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function claimMany(address vault, (uint256,uint256,(uint256,bytes32,uint256),(bytes32,uint256)[])[] claims, uint256 gasPerClaim) returns(uint256 attempted)
func (sTClaimBatcher *STClaimBatcher) TryPackClaimMany(vault common.Address, claims []STClaimBatcherClaim, gasPerClaim *big.Int) ([]byte, error) {
	return sTClaimBatcher.abi.Pack("claimMany", vault, claims, gasPerClaim)
}

// UnpackClaimMany is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xd77e363f.
//
// Solidity: function claimMany(address vault, (uint256,uint256,(uint256,bytes32,uint256),(bytes32,uint256)[])[] claims, uint256 gasPerClaim) returns(uint256 attempted)
func (sTClaimBatcher *STClaimBatcher) UnpackClaimMany(data []byte) (*big.Int, error) {
	out, err := sTClaimBatcher.abi.Unpack("claimMany", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// STClaimBatcherClaimPaid represents a ClaimPaid event raised by the STClaimBatcher contract.
type STClaimBatcherClaimPaid struct {
	Item   *big.Int
	Amount *big.Int
	Raw    *types.Log // Blockchain specific contextual infos
}

const STClaimBatcherClaimPaidEventName = "ClaimPaid"

// ContractEventName returns the user-defined event name.
func (STClaimBatcherClaimPaid) ContractEventName() string {
	return STClaimBatcherClaimPaidEventName
}

// UnpackClaimPaidEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event ClaimPaid(uint256 item, uint256 amount)
func (sTClaimBatcher *STClaimBatcher) UnpackClaimPaidEvent(log *types.Log) (*STClaimBatcherClaimPaid, error) {
	event := "ClaimPaid"
	if len(log.Topics) == 0 || log.Topics[0] != sTClaimBatcher.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STClaimBatcherClaimPaid)
	if len(log.Data) > 0 {
		if err := sTClaimBatcher.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTClaimBatcher.abi.Events[event].Inputs {
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

// STClaimBatcherClaimRefused represents a ClaimRefused event raised by the STClaimBatcher contract.
type STClaimBatcherClaimRefused struct {
	Item     *big.Int
	Selector [4]byte
	Raw      *types.Log // Blockchain specific contextual infos
}

const STClaimBatcherClaimRefusedEventName = "ClaimRefused"

// ContractEventName returns the user-defined event name.
func (STClaimBatcherClaimRefused) ContractEventName() string {
	return STClaimBatcherClaimRefusedEventName
}

// UnpackClaimRefusedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event ClaimRefused(uint256 item, bytes4 selector)
func (sTClaimBatcher *STClaimBatcher) UnpackClaimRefusedEvent(log *types.Log) (*STClaimBatcherClaimRefused, error) {
	event := "ClaimRefused"
	if len(log.Topics) == 0 || log.Topics[0] != sTClaimBatcher.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STClaimBatcherClaimRefused)
	if len(log.Data) > 0 {
		if err := sTClaimBatcher.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTClaimBatcher.abi.Events[event].Inputs {
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

// STClaimBatcherClaimsNotAttempted represents a ClaimsNotAttempted event raised by the STClaimBatcher contract.
type STClaimBatcherClaimsNotAttempted struct {
	Item *big.Int
	Raw  *types.Log // Blockchain specific contextual infos
}

const STClaimBatcherClaimsNotAttemptedEventName = "ClaimsNotAttempted"

// ContractEventName returns the user-defined event name.
func (STClaimBatcherClaimsNotAttempted) ContractEventName() string {
	return STClaimBatcherClaimsNotAttemptedEventName
}

// UnpackClaimsNotAttemptedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event ClaimsNotAttempted(uint256 item)
func (sTClaimBatcher *STClaimBatcher) UnpackClaimsNotAttemptedEvent(log *types.Log) (*STClaimBatcherClaimsNotAttempted, error) {
	event := "ClaimsNotAttempted"
	if len(log.Topics) == 0 || log.Topics[0] != sTClaimBatcher.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STClaimBatcherClaimsNotAttempted)
	if len(log.Data) > 0 {
		if err := sTClaimBatcher.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTClaimBatcher.abi.Events[event].Inputs {
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

// STSettlement2MetaData contains all meta data concerning the STSettlement2 contract.
var STSettlement2MetaData = bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"stateMutability\":\"payable\",\"type\":\"receive\"},{\"inputs\":[],\"name\":\"UPGRADE_INTERFACE_VERSION\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"approvalCount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"seq\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"root\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"ledgerHash\",\"type\":\"bytes32\"}],\"name\":\"approveDistribution\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"auditorSetVersion\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"auditors\",\"outputs\":[{\"internalType\":\"address[]\",\"name\":\"\",\"type\":\"address[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"closeOperatorEpoch\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"prevRootCount\",\"type\":\"uint256\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"root\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"total\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"leafCount\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"ledgerHash\",\"type\":\"bytes32\"}],\"internalType\":\"structISTSettlementVault2.Commitment\",\"name\":\"commitment\",\"type\":\"tuple\"}],\"name\":\"commitDistribution\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"seq\",\"type\":\"uint64\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"finalizeDistribution\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"rootIndex\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"auditor\",\"type\":\"address\"}],\"name\":\"hasApproved\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner_\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"v1Coordinator_\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"vault_\",\"type\":\"address\"},{\"internalType\":\"address[]\",\"name\":\"auditors_\",\"type\":\"address[]\"},{\"internalType\":\"uint256\",\"name\":\"quorum_\",\"type\":\"uint256\"}],\"name\":\"initialize\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"proxiableUUID\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"quorum\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"hotkey\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"maximumBurnRao\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"firstEpoch\",\"type\":\"uint64\"}],\"name\":\"registerOperatorPool\",\"outputs\":[{\"internalType\":\"uint16\",\"name\":\"uid\",\"type\":\"uint16\"}],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"renounceOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address[]\",\"name\":\"auditors_\",\"type\":\"address[]\"},{\"internalType\":\"uint256\",\"name\":\"quorum_\",\"type\":\"uint256\"}],\"name\":\"setAuditors\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"}],\"name\":\"settlementAt\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"vault_\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"hotkey\",\"type\":\"bytes32\"},{\"internalType\":\"uint8\",\"name\":\"version\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newImplementation\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"upgradeToAndCall\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"v1Coordinator\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"vault\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"version\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"address[]\",\"name\":\"auditors\",\"type\":\"address[]\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"quorum\",\"type\":\"uint256\"}],\"name\":\"AuditorsSet\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"seq\",\"type\":\"uint64\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"auditor\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"auditorSetVersion\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"approvals\",\"type\":\"uint256\"}],\"name\":\"DistributionApproved\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"version\",\"type\":\"uint64\"}],\"name\":\"Initialized\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"implementation\",\"type\":\"address\"}],\"name\":\"Upgraded\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"target\",\"type\":\"address\"}],\"name\":\"AddressEmptyCode\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"AlreadyApproved\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ApprovalClosed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"CadenceTooShort\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"implementation\",\"type\":\"address\"}],\"name\":\"ERC1967InvalidImplementation\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ERC1967NonPayable\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"FailedCall\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InactiveOperator\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidAuditors\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidConfiguration\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidEpoch\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidInitialization\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidWindow\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NativeRefundFailed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NoPendingRoot\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NotInitializing\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"}],\"name\":\"OwnableInvalidOwner\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"OwnableUnauthorizedAccount\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"PendingMismatch\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"PendingRootLive\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"QuorumNotReached\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"SettlementPaused\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UUPSUnauthorizedCallContext\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"slot\",\"type\":\"bytes32\"}],\"name\":\"UUPSUnsupportedProxiableUUID\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"Unauthorized\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UnknownPool\",\"type\":\"error\"}]",
	ID:  "0dbf989ae40f9803f9164a8286da92b2b5",
}

// STSettlement2 is an auto generated Go binding around an Ethereum contract.
type STSettlement2 struct {
	abi abi.ABI
}

// NewSTSettlement2 creates a new instance of STSettlement2.
func NewSTSettlement2() *STSettlement2 {
	parsed, err := STSettlement2MetaData.ParseABI()
	if err != nil {
		panic(errors.New("invalid ABI: " + err.Error()))
	}
	return &STSettlement2{abi: *parsed}
}

// Instance creates a wrapper for a deployed contract instance at the given address.
// Use this to create the instance object passed to abigen v2 library functions Call, Transact, etc.
func (c *STSettlement2) Instance(backend bind.ContractBackend, addr common.Address) *bind.BoundContract {
	return bind.NewBoundContract(addr, c.abi, backend, backend, backend)
}

// PackUPGRADEINTERFACEVERSION is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xad3cb1cc.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (sTSettlement2 *STSettlement2) PackUPGRADEINTERFACEVERSION() []byte {
	enc, err := sTSettlement2.abi.Pack("UPGRADE_INTERFACE_VERSION")
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
func (sTSettlement2 *STSettlement2) TryPackUPGRADEINTERFACEVERSION() ([]byte, error) {
	return sTSettlement2.abi.Pack("UPGRADE_INTERFACE_VERSION")
}

// UnpackUPGRADEINTERFACEVERSION is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xad3cb1cc.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (sTSettlement2 *STSettlement2) UnpackUPGRADEINTERFACEVERSION(data []byte) (string, error) {
	out, err := sTSettlement2.abi.Unpack("UPGRADE_INTERFACE_VERSION", data)
	if err != nil {
		return *new(string), err
	}
	out0 := *abi.ConvertType(out[0], new(string)).(*string)
	return out0, nil
}

// PackApprovalCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x40ad01ea.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function approvalCount(uint256 noId) view returns(uint256)
func (sTSettlement2 *STSettlement2) PackApprovalCount(noId *big.Int) []byte {
	enc, err := sTSettlement2.abi.Pack("approvalCount", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackApprovalCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x40ad01ea.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function approvalCount(uint256 noId) view returns(uint256)
func (sTSettlement2 *STSettlement2) TryPackApprovalCount(noId *big.Int) ([]byte, error) {
	return sTSettlement2.abi.Pack("approvalCount", noId)
}

// UnpackApprovalCount is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x40ad01ea.
//
// Solidity: function approvalCount(uint256 noId) view returns(uint256)
func (sTSettlement2 *STSettlement2) UnpackApprovalCount(data []byte) (*big.Int, error) {
	out, err := sTSettlement2.abi.Unpack("approvalCount", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackApproveDistribution is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x20a9aed6.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function approveDistribution(uint256 noId, uint64 seq, bytes32 root, bytes32 ledgerHash) returns()
func (sTSettlement2 *STSettlement2) PackApproveDistribution(noId *big.Int, seq uint64, root [32]byte, ledgerHash [32]byte) []byte {
	enc, err := sTSettlement2.abi.Pack("approveDistribution", noId, seq, root, ledgerHash)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackApproveDistribution is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x20a9aed6.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function approveDistribution(uint256 noId, uint64 seq, bytes32 root, bytes32 ledgerHash) returns()
func (sTSettlement2 *STSettlement2) TryPackApproveDistribution(noId *big.Int, seq uint64, root [32]byte, ledgerHash [32]byte) ([]byte, error) {
	return sTSettlement2.abi.Pack("approveDistribution", noId, seq, root, ledgerHash)
}

// PackAuditorSetVersion is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xb55bacfb.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function auditorSetVersion() view returns(uint64)
func (sTSettlement2 *STSettlement2) PackAuditorSetVersion() []byte {
	enc, err := sTSettlement2.abi.Pack("auditorSetVersion")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackAuditorSetVersion is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xb55bacfb.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function auditorSetVersion() view returns(uint64)
func (sTSettlement2 *STSettlement2) TryPackAuditorSetVersion() ([]byte, error) {
	return sTSettlement2.abi.Pack("auditorSetVersion")
}

// UnpackAuditorSetVersion is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xb55bacfb.
//
// Solidity: function auditorSetVersion() view returns(uint64)
func (sTSettlement2 *STSettlement2) UnpackAuditorSetVersion(data []byte) (uint64, error) {
	out, err := sTSettlement2.abi.Unpack("auditorSetVersion", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackAuditors is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x6d4593d2.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function auditors() view returns(address[])
func (sTSettlement2 *STSettlement2) PackAuditors() []byte {
	enc, err := sTSettlement2.abi.Pack("auditors")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackAuditors is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x6d4593d2.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function auditors() view returns(address[])
func (sTSettlement2 *STSettlement2) TryPackAuditors() ([]byte, error) {
	return sTSettlement2.abi.Pack("auditors")
}

// UnpackAuditors is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x6d4593d2.
//
// Solidity: function auditors() view returns(address[])
func (sTSettlement2 *STSettlement2) UnpackAuditors(data []byte) ([]common.Address, error) {
	out, err := sTSettlement2.abi.Unpack("auditors", data)
	if err != nil {
		return *new([]common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new([]common.Address)).(*[]common.Address)
	return out0, nil
}

// PackCloseOperatorEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xbb2126c9.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function closeOperatorEpoch(uint256 epoch, uint256 noId) returns(uint256 amount)
func (sTSettlement2 *STSettlement2) PackCloseOperatorEpoch(epoch *big.Int, noId *big.Int) []byte {
	enc, err := sTSettlement2.abi.Pack("closeOperatorEpoch", epoch, noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCloseOperatorEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xbb2126c9.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function closeOperatorEpoch(uint256 epoch, uint256 noId) returns(uint256 amount)
func (sTSettlement2 *STSettlement2) TryPackCloseOperatorEpoch(epoch *big.Int, noId *big.Int) ([]byte, error) {
	return sTSettlement2.abi.Pack("closeOperatorEpoch", epoch, noId)
}

// UnpackCloseOperatorEpoch is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xbb2126c9.
//
// Solidity: function closeOperatorEpoch(uint256 epoch, uint256 noId) returns(uint256 amount)
func (sTSettlement2 *STSettlement2) UnpackCloseOperatorEpoch(data []byte) (*big.Int, error) {
	out, err := sTSettlement2.abi.Unpack("closeOperatorEpoch", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackCommitDistribution is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xdbf536b5.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function commitDistribution(uint256 epoch, uint256 noId, uint256 prevRootCount, (bytes32,uint256,uint64,bytes32) commitment) returns(uint64 seq)
func (sTSettlement2 *STSettlement2) PackCommitDistribution(epoch *big.Int, noId *big.Int, prevRootCount *big.Int, commitment ISTSettlementVault2Commitment) []byte {
	enc, err := sTSettlement2.abi.Pack("commitDistribution", epoch, noId, prevRootCount, commitment)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCommitDistribution is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xdbf536b5.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function commitDistribution(uint256 epoch, uint256 noId, uint256 prevRootCount, (bytes32,uint256,uint64,bytes32) commitment) returns(uint64 seq)
func (sTSettlement2 *STSettlement2) TryPackCommitDistribution(epoch *big.Int, noId *big.Int, prevRootCount *big.Int, commitment ISTSettlementVault2Commitment) ([]byte, error) {
	return sTSettlement2.abi.Pack("commitDistribution", epoch, noId, prevRootCount, commitment)
}

// UnpackCommitDistribution is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xdbf536b5.
//
// Solidity: function commitDistribution(uint256 epoch, uint256 noId, uint256 prevRootCount, (bytes32,uint256,uint64,bytes32) commitment) returns(uint64 seq)
func (sTSettlement2 *STSettlement2) UnpackCommitDistribution(data []byte) (uint64, error) {
	out, err := sTSettlement2.abi.Unpack("commitDistribution", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackFinalizeDistribution is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x95f52f4e.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function finalizeDistribution(uint256 noId) returns(uint256 rootIndex)
func (sTSettlement2 *STSettlement2) PackFinalizeDistribution(noId *big.Int) []byte {
	enc, err := sTSettlement2.abi.Pack("finalizeDistribution", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackFinalizeDistribution is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x95f52f4e.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function finalizeDistribution(uint256 noId) returns(uint256 rootIndex)
func (sTSettlement2 *STSettlement2) TryPackFinalizeDistribution(noId *big.Int) ([]byte, error) {
	return sTSettlement2.abi.Pack("finalizeDistribution", noId)
}

// UnpackFinalizeDistribution is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x95f52f4e.
//
// Solidity: function finalizeDistribution(uint256 noId) returns(uint256 rootIndex)
func (sTSettlement2 *STSettlement2) UnpackFinalizeDistribution(data []byte) (*big.Int, error) {
	out, err := sTSettlement2.abi.Unpack("finalizeDistribution", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackHasApproved is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x2358d5a8.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function hasApproved(uint256 noId, address auditor) view returns(bool)
func (sTSettlement2 *STSettlement2) PackHasApproved(noId *big.Int, auditor common.Address) []byte {
	enc, err := sTSettlement2.abi.Pack("hasApproved", noId, auditor)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackHasApproved is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x2358d5a8.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function hasApproved(uint256 noId, address auditor) view returns(bool)
func (sTSettlement2 *STSettlement2) TryPackHasApproved(noId *big.Int, auditor common.Address) ([]byte, error) {
	return sTSettlement2.abi.Pack("hasApproved", noId, auditor)
}

// UnpackHasApproved is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x2358d5a8.
//
// Solidity: function hasApproved(uint256 noId, address auditor) view returns(bool)
func (sTSettlement2 *STSettlement2) UnpackHasApproved(data []byte) (bool, error) {
	out, err := sTSettlement2.abi.Unpack("hasApproved", data)
	if err != nil {
		return *new(bool), err
	}
	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)
	return out0, nil
}

// PackInitialize is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x75d1a9d6.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function initialize(address owner_, address v1Coordinator_, address vault_, address[] auditors_, uint256 quorum_) returns()
func (sTSettlement2 *STSettlement2) PackInitialize(owner common.Address, v1Coordinator common.Address, vault common.Address, auditors []common.Address, quorum *big.Int) []byte {
	enc, err := sTSettlement2.abi.Pack("initialize", owner, v1Coordinator, vault, auditors, quorum)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackInitialize is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x75d1a9d6.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function initialize(address owner_, address v1Coordinator_, address vault_, address[] auditors_, uint256 quorum_) returns()
func (sTSettlement2 *STSettlement2) TryPackInitialize(owner common.Address, v1Coordinator common.Address, vault common.Address, auditors []common.Address, quorum *big.Int) ([]byte, error) {
	return sTSettlement2.abi.Pack("initialize", owner, v1Coordinator, vault, auditors, quorum)
}

// PackOwner is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x8da5cb5b.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function owner() view returns(address)
func (sTSettlement2 *STSettlement2) PackOwner() []byte {
	enc, err := sTSettlement2.abi.Pack("owner")
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
func (sTSettlement2 *STSettlement2) TryPackOwner() ([]byte, error) {
	return sTSettlement2.abi.Pack("owner")
}

// UnpackOwner is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (sTSettlement2 *STSettlement2) UnpackOwner(data []byte) (common.Address, error) {
	out, err := sTSettlement2.abi.Unpack("owner", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackProxiableUUID is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x52d1902d.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (sTSettlement2 *STSettlement2) PackProxiableUUID() []byte {
	enc, err := sTSettlement2.abi.Pack("proxiableUUID")
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
func (sTSettlement2 *STSettlement2) TryPackProxiableUUID() ([]byte, error) {
	return sTSettlement2.abi.Pack("proxiableUUID")
}

// UnpackProxiableUUID is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (sTSettlement2 *STSettlement2) UnpackProxiableUUID(data []byte) ([32]byte, error) {
	out, err := sTSettlement2.abi.Unpack("proxiableUUID", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackQuorum is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x1703a018.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function quorum() view returns(uint256)
func (sTSettlement2 *STSettlement2) PackQuorum() []byte {
	enc, err := sTSettlement2.abi.Pack("quorum")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackQuorum is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x1703a018.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function quorum() view returns(uint256)
func (sTSettlement2 *STSettlement2) TryPackQuorum() ([]byte, error) {
	return sTSettlement2.abi.Pack("quorum")
}

// UnpackQuorum is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x1703a018.
//
// Solidity: function quorum() view returns(uint256)
func (sTSettlement2 *STSettlement2) UnpackQuorum(data []byte) (*big.Int, error) {
	out, err := sTSettlement2.abi.Unpack("quorum", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackRegisterOperatorPool is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x8f477b7d.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function registerOperatorPool(uint256 noId, bytes32 hotkey, uint64 maximumBurnRao, uint64 firstEpoch) payable returns(uint16 uid)
func (sTSettlement2 *STSettlement2) PackRegisterOperatorPool(noId *big.Int, hotkey [32]byte, maximumBurnRao uint64, firstEpoch uint64) []byte {
	enc, err := sTSettlement2.abi.Pack("registerOperatorPool", noId, hotkey, maximumBurnRao, firstEpoch)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRegisterOperatorPool is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x8f477b7d.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function registerOperatorPool(uint256 noId, bytes32 hotkey, uint64 maximumBurnRao, uint64 firstEpoch) payable returns(uint16 uid)
func (sTSettlement2 *STSettlement2) TryPackRegisterOperatorPool(noId *big.Int, hotkey [32]byte, maximumBurnRao uint64, firstEpoch uint64) ([]byte, error) {
	return sTSettlement2.abi.Pack("registerOperatorPool", noId, hotkey, maximumBurnRao, firstEpoch)
}

// UnpackRegisterOperatorPool is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x8f477b7d.
//
// Solidity: function registerOperatorPool(uint256 noId, bytes32 hotkey, uint64 maximumBurnRao, uint64 firstEpoch) payable returns(uint16 uid)
func (sTSettlement2 *STSettlement2) UnpackRegisterOperatorPool(data []byte) (uint16, error) {
	out, err := sTSettlement2.abi.Unpack("registerOperatorPool", data)
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
func (sTSettlement2 *STSettlement2) PackRenounceOwnership() []byte {
	enc, err := sTSettlement2.abi.Pack("renounceOwnership")
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
func (sTSettlement2 *STSettlement2) TryPackRenounceOwnership() ([]byte, error) {
	return sTSettlement2.abi.Pack("renounceOwnership")
}

// PackSetAuditors is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe800843a.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function setAuditors(address[] auditors_, uint256 quorum_) returns()
func (sTSettlement2 *STSettlement2) PackSetAuditors(auditors []common.Address, quorum *big.Int) []byte {
	enc, err := sTSettlement2.abi.Pack("setAuditors", auditors, quorum)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSetAuditors is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe800843a.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function setAuditors(address[] auditors_, uint256 quorum_) returns()
func (sTSettlement2 *STSettlement2) TryPackSetAuditors(auditors []common.Address, quorum *big.Int) ([]byte, error) {
	return sTSettlement2.abi.Pack("setAuditors", auditors, quorum)
}

// PackSettlementAt is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xcf3cf236.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function settlementAt(uint256 noId, uint256 epoch) view returns(address vault_, bytes32 hotkey, uint8 version)
func (sTSettlement2 *STSettlement2) PackSettlementAt(noId *big.Int, epoch *big.Int) []byte {
	enc, err := sTSettlement2.abi.Pack("settlementAt", noId, epoch)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSettlementAt is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xcf3cf236.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function settlementAt(uint256 noId, uint256 epoch) view returns(address vault_, bytes32 hotkey, uint8 version)
func (sTSettlement2 *STSettlement2) TryPackSettlementAt(noId *big.Int, epoch *big.Int) ([]byte, error) {
	return sTSettlement2.abi.Pack("settlementAt", noId, epoch)
}

// SettlementAtOutput serves as a container for the return parameters of contract
// method SettlementAt.
type SettlementAtOutput struct {
	Vault   common.Address
	Hotkey  [32]byte
	Version uint8
}

// UnpackSettlementAt is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xcf3cf236.
//
// Solidity: function settlementAt(uint256 noId, uint256 epoch) view returns(address vault_, bytes32 hotkey, uint8 version)
func (sTSettlement2 *STSettlement2) UnpackSettlementAt(data []byte) (SettlementAtOutput, error) {
	out, err := sTSettlement2.abi.Unpack("settlementAt", data)
	outstruct := new(SettlementAtOutput)
	if err != nil {
		return *outstruct, err
	}
	outstruct.Vault = *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	outstruct.Hotkey = *abi.ConvertType(out[1], new([32]byte)).(*[32]byte)
	outstruct.Version = *abi.ConvertType(out[2], new(uint8)).(*uint8)
	return *outstruct, nil
}

// PackTransferOwnership is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf2fde38b.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (sTSettlement2 *STSettlement2) PackTransferOwnership(newOwner common.Address) []byte {
	enc, err := sTSettlement2.abi.Pack("transferOwnership", newOwner)
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
func (sTSettlement2 *STSettlement2) TryPackTransferOwnership(newOwner common.Address) ([]byte, error) {
	return sTSettlement2.abi.Pack("transferOwnership", newOwner)
}

// PackUpgradeToAndCall is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4f1ef286.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (sTSettlement2 *STSettlement2) PackUpgradeToAndCall(newImplementation common.Address, data []byte) []byte {
	enc, err := sTSettlement2.abi.Pack("upgradeToAndCall", newImplementation, data)
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
func (sTSettlement2 *STSettlement2) TryPackUpgradeToAndCall(newImplementation common.Address, data []byte) ([]byte, error) {
	return sTSettlement2.abi.Pack("upgradeToAndCall", newImplementation, data)
}

// PackV1Coordinator is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4f18b76c.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function v1Coordinator() view returns(address)
func (sTSettlement2 *STSettlement2) PackV1Coordinator() []byte {
	enc, err := sTSettlement2.abi.Pack("v1Coordinator")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackV1Coordinator is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x4f18b76c.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function v1Coordinator() view returns(address)
func (sTSettlement2 *STSettlement2) TryPackV1Coordinator() ([]byte, error) {
	return sTSettlement2.abi.Pack("v1Coordinator")
}

// UnpackV1Coordinator is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x4f18b76c.
//
// Solidity: function v1Coordinator() view returns(address)
func (sTSettlement2 *STSettlement2) UnpackV1Coordinator(data []byte) (common.Address, error) {
	out, err := sTSettlement2.abi.Unpack("v1Coordinator", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackVault is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xfbfa77cf.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function vault() view returns(address)
func (sTSettlement2 *STSettlement2) PackVault() []byte {
	enc, err := sTSettlement2.abi.Pack("vault")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackVault is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xfbfa77cf.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function vault() view returns(address)
func (sTSettlement2 *STSettlement2) TryPackVault() ([]byte, error) {
	return sTSettlement2.abi.Pack("vault")
}

// UnpackVault is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xfbfa77cf.
//
// Solidity: function vault() view returns(address)
func (sTSettlement2 *STSettlement2) UnpackVault(data []byte) (common.Address, error) {
	out, err := sTSettlement2.abi.Unpack("vault", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// STSettlement2AuditorsSet represents a AuditorsSet event raised by the STSettlement2 contract.
type STSettlement2AuditorsSet struct {
	Version  uint64
	Auditors []common.Address
	Quorum   *big.Int
	Raw      *types.Log // Blockchain specific contextual infos
}

const STSettlement2AuditorsSetEventName = "AuditorsSet"

// ContractEventName returns the user-defined event name.
func (STSettlement2AuditorsSet) ContractEventName() string {
	return STSettlement2AuditorsSetEventName
}

// UnpackAuditorsSetEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event AuditorsSet(uint64 indexed version, address[] auditors, uint256 quorum)
func (sTSettlement2 *STSettlement2) UnpackAuditorsSetEvent(log *types.Log) (*STSettlement2AuditorsSet, error) {
	event := "AuditorsSet"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlement2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlement2AuditorsSet)
	if len(log.Data) > 0 {
		if err := sTSettlement2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlement2.abi.Events[event].Inputs {
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

// STSettlement2DistributionApproved represents a DistributionApproved event raised by the STSettlement2 contract.
type STSettlement2DistributionApproved struct {
	NoId              *big.Int
	Seq               uint64
	Auditor           common.Address
	AuditorSetVersion uint64
	Approvals         *big.Int
	Raw               *types.Log // Blockchain specific contextual infos
}

const STSettlement2DistributionApprovedEventName = "DistributionApproved"

// ContractEventName returns the user-defined event name.
func (STSettlement2DistributionApproved) ContractEventName() string {
	return STSettlement2DistributionApprovedEventName
}

// UnpackDistributionApprovedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event DistributionApproved(uint256 indexed noId, uint64 indexed seq, address indexed auditor, uint64 auditorSetVersion, uint256 approvals)
func (sTSettlement2 *STSettlement2) UnpackDistributionApprovedEvent(log *types.Log) (*STSettlement2DistributionApproved, error) {
	event := "DistributionApproved"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlement2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlement2DistributionApproved)
	if len(log.Data) > 0 {
		if err := sTSettlement2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlement2.abi.Events[event].Inputs {
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

// STSettlement2Initialized represents a Initialized event raised by the STSettlement2 contract.
type STSettlement2Initialized struct {
	Version uint64
	Raw     *types.Log // Blockchain specific contextual infos
}

const STSettlement2InitializedEventName = "Initialized"

// ContractEventName returns the user-defined event name.
func (STSettlement2Initialized) ContractEventName() string {
	return STSettlement2InitializedEventName
}

// UnpackInitializedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event Initialized(uint64 version)
func (sTSettlement2 *STSettlement2) UnpackInitializedEvent(log *types.Log) (*STSettlement2Initialized, error) {
	event := "Initialized"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlement2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlement2Initialized)
	if len(log.Data) > 0 {
		if err := sTSettlement2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlement2.abi.Events[event].Inputs {
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

// STSettlement2OwnershipTransferred represents a OwnershipTransferred event raised by the STSettlement2 contract.
type STSettlement2OwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           *types.Log // Blockchain specific contextual infos
}

const STSettlement2OwnershipTransferredEventName = "OwnershipTransferred"

// ContractEventName returns the user-defined event name.
func (STSettlement2OwnershipTransferred) ContractEventName() string {
	return STSettlement2OwnershipTransferredEventName
}

// UnpackOwnershipTransferredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (sTSettlement2 *STSettlement2) UnpackOwnershipTransferredEvent(log *types.Log) (*STSettlement2OwnershipTransferred, error) {
	event := "OwnershipTransferred"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlement2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlement2OwnershipTransferred)
	if len(log.Data) > 0 {
		if err := sTSettlement2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlement2.abi.Events[event].Inputs {
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

// STSettlement2Upgraded represents a Upgraded event raised by the STSettlement2 contract.
type STSettlement2Upgraded struct {
	Implementation common.Address
	Raw            *types.Log // Blockchain specific contextual infos
}

const STSettlement2UpgradedEventName = "Upgraded"

// ContractEventName returns the user-defined event name.
func (STSettlement2Upgraded) ContractEventName() string {
	return STSettlement2UpgradedEventName
}

// UnpackUpgradedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event Upgraded(address indexed implementation)
func (sTSettlement2 *STSettlement2) UnpackUpgradedEvent(log *types.Log) (*STSettlement2Upgraded, error) {
	event := "Upgraded"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlement2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlement2Upgraded)
	if len(log.Data) > 0 {
		if err := sTSettlement2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlement2.abi.Events[event].Inputs {
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
func (sTSettlement2 *STSettlement2) UnpackError(raw []byte) (any, error) {
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["AddressEmptyCode"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackAddressEmptyCodeError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["AlreadyApproved"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackAlreadyApprovedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["ApprovalClosed"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackApprovalClosedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["CadenceTooShort"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackCadenceTooShortError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["ERC1967InvalidImplementation"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackERC1967InvalidImplementationError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["ERC1967NonPayable"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackERC1967NonPayableError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["FailedCall"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackFailedCallError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["InactiveOperator"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackInactiveOperatorError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["InvalidAuditors"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackInvalidAuditorsError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["InvalidConfiguration"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackInvalidConfigurationError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["InvalidEpoch"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackInvalidEpochError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["InvalidInitialization"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackInvalidInitializationError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["InvalidWindow"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackInvalidWindowError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["NativeRefundFailed"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackNativeRefundFailedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["NoPendingRoot"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackNoPendingRootError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["NotInitializing"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackNotInitializingError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["OwnableInvalidOwner"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackOwnableInvalidOwnerError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["OwnableUnauthorizedAccount"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackOwnableUnauthorizedAccountError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["PendingMismatch"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackPendingMismatchError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["PendingRootLive"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackPendingRootLiveError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["QuorumNotReached"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackQuorumNotReachedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["SettlementPaused"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackSettlementPausedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["UUPSUnauthorizedCallContext"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackUUPSUnauthorizedCallContextError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["UUPSUnsupportedProxiableUUID"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackUUPSUnsupportedProxiableUUIDError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["Unauthorized"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackUnauthorizedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlement2.abi.Errors["UnknownPool"].ID.Bytes()[:4]) {
		return sTSettlement2.UnpackUnknownPoolError(raw[4:])
	}
	return nil, errors.New("Unknown error")
}

// STSettlement2AddressEmptyCode represents a AddressEmptyCode error raised by the STSettlement2 contract.
type STSettlement2AddressEmptyCode struct {
	Target common.Address
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error AddressEmptyCode(address target)
func STSettlement2AddressEmptyCodeErrorID() common.Hash {
	return common.HexToHash("0x9996b315c842ff135b8fc4a08ad5df1c344efbc03d2687aecc0678050d2aac89")
}

// UnpackAddressEmptyCodeError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error AddressEmptyCode(address target)
func (sTSettlement2 *STSettlement2) UnpackAddressEmptyCodeError(raw []byte) (*STSettlement2AddressEmptyCode, error) {
	out := new(STSettlement2AddressEmptyCode)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "AddressEmptyCode", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2AlreadyApproved represents a AlreadyApproved error raised by the STSettlement2 contract.
type STSettlement2AlreadyApproved struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error AlreadyApproved()
func STSettlement2AlreadyApprovedErrorID() common.Hash {
	return common.HexToHash("0x101f817a2457b2fcd2728e709f840e8180b3b74a761c9cbd09a4fa605a43f8b1")
}

// UnpackAlreadyApprovedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error AlreadyApproved()
func (sTSettlement2 *STSettlement2) UnpackAlreadyApprovedError(raw []byte) (*STSettlement2AlreadyApproved, error) {
	out := new(STSettlement2AlreadyApproved)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "AlreadyApproved", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2ApprovalClosed represents a ApprovalClosed error raised by the STSettlement2 contract.
type STSettlement2ApprovalClosed struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error ApprovalClosed()
func STSettlement2ApprovalClosedErrorID() common.Hash {
	return common.HexToHash("0x3dc7a58fb1f5281dce7ee6a1428f457dfca450080e6c93b6eb2d2fdebdef3b1e")
}

// UnpackApprovalClosedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error ApprovalClosed()
func (sTSettlement2 *STSettlement2) UnpackApprovalClosedError(raw []byte) (*STSettlement2ApprovalClosed, error) {
	out := new(STSettlement2ApprovalClosed)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "ApprovalClosed", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2CadenceTooShort represents a CadenceTooShort error raised by the STSettlement2 contract.
type STSettlement2CadenceTooShort struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error CadenceTooShort()
func STSettlement2CadenceTooShortErrorID() common.Hash {
	return common.HexToHash("0x43bd555a55a3230a433493d99d181aa2aecea6705ffdff221e1390d8ab1c9924")
}

// UnpackCadenceTooShortError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error CadenceTooShort()
func (sTSettlement2 *STSettlement2) UnpackCadenceTooShortError(raw []byte) (*STSettlement2CadenceTooShort, error) {
	out := new(STSettlement2CadenceTooShort)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "CadenceTooShort", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2ERC1967InvalidImplementation represents a ERC1967InvalidImplementation error raised by the STSettlement2 contract.
type STSettlement2ERC1967InvalidImplementation struct {
	Implementation common.Address
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error ERC1967InvalidImplementation(address implementation)
func STSettlement2ERC1967InvalidImplementationErrorID() common.Hash {
	return common.HexToHash("0x4c9c8ce3ceb3130f17f7cdba48d89b5b0129f266a8bac114e6e315a41879b617")
}

// UnpackERC1967InvalidImplementationError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error ERC1967InvalidImplementation(address implementation)
func (sTSettlement2 *STSettlement2) UnpackERC1967InvalidImplementationError(raw []byte) (*STSettlement2ERC1967InvalidImplementation, error) {
	out := new(STSettlement2ERC1967InvalidImplementation)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "ERC1967InvalidImplementation", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2ERC1967NonPayable represents a ERC1967NonPayable error raised by the STSettlement2 contract.
type STSettlement2ERC1967NonPayable struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error ERC1967NonPayable()
func STSettlement2ERC1967NonPayableErrorID() common.Hash {
	return common.HexToHash("0xb398979fa84f543c8e222f17890372c487baf85e062276c127fef521eea7224b")
}

// UnpackERC1967NonPayableError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error ERC1967NonPayable()
func (sTSettlement2 *STSettlement2) UnpackERC1967NonPayableError(raw []byte) (*STSettlement2ERC1967NonPayable, error) {
	out := new(STSettlement2ERC1967NonPayable)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "ERC1967NonPayable", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2FailedCall represents a FailedCall error raised by the STSettlement2 contract.
type STSettlement2FailedCall struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error FailedCall()
func STSettlement2FailedCallErrorID() common.Hash {
	return common.HexToHash("0xd6bda27508c0fb6d8a39b4b122878dab26f731a7d4e4abe711dd3731899052a4")
}

// UnpackFailedCallError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error FailedCall()
func (sTSettlement2 *STSettlement2) UnpackFailedCallError(raw []byte) (*STSettlement2FailedCall, error) {
	out := new(STSettlement2FailedCall)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "FailedCall", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2InactiveOperator represents a InactiveOperator error raised by the STSettlement2 contract.
type STSettlement2InactiveOperator struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InactiveOperator()
func STSettlement2InactiveOperatorErrorID() common.Hash {
	return common.HexToHash("0xc3c2f8d7f8c41ca277101f642c0890a56466fe79b366140100a1d4ff2dc17132")
}

// UnpackInactiveOperatorError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InactiveOperator()
func (sTSettlement2 *STSettlement2) UnpackInactiveOperatorError(raw []byte) (*STSettlement2InactiveOperator, error) {
	out := new(STSettlement2InactiveOperator)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "InactiveOperator", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2InvalidAuditors represents a InvalidAuditors error raised by the STSettlement2 contract.
type STSettlement2InvalidAuditors struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidAuditors()
func STSettlement2InvalidAuditorsErrorID() common.Hash {
	return common.HexToHash("0xeb4dc46b44aebe7cb01bbd84342705a763e8be3cd9b8b428440964a09f053b08")
}

// UnpackInvalidAuditorsError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidAuditors()
func (sTSettlement2 *STSettlement2) UnpackInvalidAuditorsError(raw []byte) (*STSettlement2InvalidAuditors, error) {
	out := new(STSettlement2InvalidAuditors)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "InvalidAuditors", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2InvalidConfiguration represents a InvalidConfiguration error raised by the STSettlement2 contract.
type STSettlement2InvalidConfiguration struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidConfiguration()
func STSettlement2InvalidConfigurationErrorID() common.Hash {
	return common.HexToHash("0xc52a9bd3d9e475b9056a93172ef6968d775a7cd41c4255bbebf12e90a5fbbd39")
}

// UnpackInvalidConfigurationError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidConfiguration()
func (sTSettlement2 *STSettlement2) UnpackInvalidConfigurationError(raw []byte) (*STSettlement2InvalidConfiguration, error) {
	out := new(STSettlement2InvalidConfiguration)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "InvalidConfiguration", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2InvalidEpoch represents a InvalidEpoch error raised by the STSettlement2 contract.
type STSettlement2InvalidEpoch struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidEpoch()
func STSettlement2InvalidEpochErrorID() common.Hash {
	return common.HexToHash("0xd5b25b63142e607aedcb4587d8d0a7885e39fe07e954f7f94b12e1c076cdbd11")
}

// UnpackInvalidEpochError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidEpoch()
func (sTSettlement2 *STSettlement2) UnpackInvalidEpochError(raw []byte) (*STSettlement2InvalidEpoch, error) {
	out := new(STSettlement2InvalidEpoch)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "InvalidEpoch", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2InvalidInitialization represents a InvalidInitialization error raised by the STSettlement2 contract.
type STSettlement2InvalidInitialization struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidInitialization()
func STSettlement2InvalidInitializationErrorID() common.Hash {
	return common.HexToHash("0xf92ee8a957075833165f68c320933b1a1294aafc84ee6e0dd3fb178008f9aaf5")
}

// UnpackInvalidInitializationError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidInitialization()
func (sTSettlement2 *STSettlement2) UnpackInvalidInitializationError(raw []byte) (*STSettlement2InvalidInitialization, error) {
	out := new(STSettlement2InvalidInitialization)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "InvalidInitialization", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2InvalidWindow represents a InvalidWindow error raised by the STSettlement2 contract.
type STSettlement2InvalidWindow struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidWindow()
func STSettlement2InvalidWindowErrorID() common.Hash {
	return common.HexToHash("0x392334ed85cf5b0998d72154ce4383ddab4c856d648b4f9bd678a630b321c06c")
}

// UnpackInvalidWindowError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidWindow()
func (sTSettlement2 *STSettlement2) UnpackInvalidWindowError(raw []byte) (*STSettlement2InvalidWindow, error) {
	out := new(STSettlement2InvalidWindow)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "InvalidWindow", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2NativeRefundFailed represents a NativeRefundFailed error raised by the STSettlement2 contract.
type STSettlement2NativeRefundFailed struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NativeRefundFailed()
func STSettlement2NativeRefundFailedErrorID() common.Hash {
	return common.HexToHash("0x8520d710691d90175ff6d0cde74ada1f9d91ac205406750a2381430c693c006e")
}

// UnpackNativeRefundFailedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NativeRefundFailed()
func (sTSettlement2 *STSettlement2) UnpackNativeRefundFailedError(raw []byte) (*STSettlement2NativeRefundFailed, error) {
	out := new(STSettlement2NativeRefundFailed)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "NativeRefundFailed", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2NoPendingRoot represents a NoPendingRoot error raised by the STSettlement2 contract.
type STSettlement2NoPendingRoot struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NoPendingRoot()
func STSettlement2NoPendingRootErrorID() common.Hash {
	return common.HexToHash("0x2a01895ee863735ea8533479c7609527402e57aaebaef575f9a1b9cb21c4beee")
}

// UnpackNoPendingRootError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NoPendingRoot()
func (sTSettlement2 *STSettlement2) UnpackNoPendingRootError(raw []byte) (*STSettlement2NoPendingRoot, error) {
	out := new(STSettlement2NoPendingRoot)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "NoPendingRoot", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2NotInitializing represents a NotInitializing error raised by the STSettlement2 contract.
type STSettlement2NotInitializing struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NotInitializing()
func STSettlement2NotInitializingErrorID() common.Hash {
	return common.HexToHash("0xd7e6bcf8597daa127dc9f0048d2f08d5ef140a2cb659feabd700beff1f7a8302")
}

// UnpackNotInitializingError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NotInitializing()
func (sTSettlement2 *STSettlement2) UnpackNotInitializingError(raw []byte) (*STSettlement2NotInitializing, error) {
	out := new(STSettlement2NotInitializing)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "NotInitializing", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2OwnableInvalidOwner represents a OwnableInvalidOwner error raised by the STSettlement2 contract.
type STSettlement2OwnableInvalidOwner struct {
	Owner common.Address
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error OwnableInvalidOwner(address owner)
func STSettlement2OwnableInvalidOwnerErrorID() common.Hash {
	return common.HexToHash("0x1e4fbdf7f3ef8bcaa855599e3abf48b232380f183f08f6f813d9ffa5bd585188")
}

// UnpackOwnableInvalidOwnerError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error OwnableInvalidOwner(address owner)
func (sTSettlement2 *STSettlement2) UnpackOwnableInvalidOwnerError(raw []byte) (*STSettlement2OwnableInvalidOwner, error) {
	out := new(STSettlement2OwnableInvalidOwner)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "OwnableInvalidOwner", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2OwnableUnauthorizedAccount represents a OwnableUnauthorizedAccount error raised by the STSettlement2 contract.
type STSettlement2OwnableUnauthorizedAccount struct {
	Account common.Address
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error OwnableUnauthorizedAccount(address account)
func STSettlement2OwnableUnauthorizedAccountErrorID() common.Hash {
	return common.HexToHash("0x118cdaa7a341953d1887a2245fd6665d741c67c8c50581daa59e1d03373fa188")
}

// UnpackOwnableUnauthorizedAccountError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error OwnableUnauthorizedAccount(address account)
func (sTSettlement2 *STSettlement2) UnpackOwnableUnauthorizedAccountError(raw []byte) (*STSettlement2OwnableUnauthorizedAccount, error) {
	out := new(STSettlement2OwnableUnauthorizedAccount)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "OwnableUnauthorizedAccount", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2PendingMismatch represents a PendingMismatch error raised by the STSettlement2 contract.
type STSettlement2PendingMismatch struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error PendingMismatch()
func STSettlement2PendingMismatchErrorID() common.Hash {
	return common.HexToHash("0x3d87e06556badfed8da7a94177def8f3f3dc8ca393d178e73b57219cfc2958d1")
}

// UnpackPendingMismatchError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error PendingMismatch()
func (sTSettlement2 *STSettlement2) UnpackPendingMismatchError(raw []byte) (*STSettlement2PendingMismatch, error) {
	out := new(STSettlement2PendingMismatch)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "PendingMismatch", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2PendingRootLive represents a PendingRootLive error raised by the STSettlement2 contract.
type STSettlement2PendingRootLive struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error PendingRootLive()
func STSettlement2PendingRootLiveErrorID() common.Hash {
	return common.HexToHash("0x354764bdac714f96360ed45a08b0d92a10a0f00f2e6e35a3525725b699df83a6")
}

// UnpackPendingRootLiveError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error PendingRootLive()
func (sTSettlement2 *STSettlement2) UnpackPendingRootLiveError(raw []byte) (*STSettlement2PendingRootLive, error) {
	out := new(STSettlement2PendingRootLive)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "PendingRootLive", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2QuorumNotReached represents a QuorumNotReached error raised by the STSettlement2 contract.
type STSettlement2QuorumNotReached struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error QuorumNotReached()
func STSettlement2QuorumNotReachedErrorID() common.Hash {
	return common.HexToHash("0xaa26a693e6c52658460cb33272f74f5d667649540c66a50f7846b5655fa65694")
}

// UnpackQuorumNotReachedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error QuorumNotReached()
func (sTSettlement2 *STSettlement2) UnpackQuorumNotReachedError(raw []byte) (*STSettlement2QuorumNotReached, error) {
	out := new(STSettlement2QuorumNotReached)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "QuorumNotReached", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2SettlementPaused represents a SettlementPaused error raised by the STSettlement2 contract.
type STSettlement2SettlementPaused struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error SettlementPaused()
func STSettlement2SettlementPausedErrorID() common.Hash {
	return common.HexToHash("0x4c211ccd408082882e0fc07866a05ff542c6bfcb7a14d44dc9bc2d967d237d18")
}

// UnpackSettlementPausedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error SettlementPaused()
func (sTSettlement2 *STSettlement2) UnpackSettlementPausedError(raw []byte) (*STSettlement2SettlementPaused, error) {
	out := new(STSettlement2SettlementPaused)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "SettlementPaused", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2UUPSUnauthorizedCallContext represents a UUPSUnauthorizedCallContext error raised by the STSettlement2 contract.
type STSettlement2UUPSUnauthorizedCallContext struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error UUPSUnauthorizedCallContext()
func STSettlement2UUPSUnauthorizedCallContextErrorID() common.Hash {
	return common.HexToHash("0xe07c8dba242a06571ac65fe4bbe20522c9fb111cb33599b799ff8039c1ed18f4")
}

// UnpackUUPSUnauthorizedCallContextError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error UUPSUnauthorizedCallContext()
func (sTSettlement2 *STSettlement2) UnpackUUPSUnauthorizedCallContextError(raw []byte) (*STSettlement2UUPSUnauthorizedCallContext, error) {
	out := new(STSettlement2UUPSUnauthorizedCallContext)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "UUPSUnauthorizedCallContext", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2UUPSUnsupportedProxiableUUID represents a UUPSUnsupportedProxiableUUID error raised by the STSettlement2 contract.
type STSettlement2UUPSUnsupportedProxiableUUID struct {
	Slot [32]byte
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error UUPSUnsupportedProxiableUUID(bytes32 slot)
func STSettlement2UUPSUnsupportedProxiableUUIDErrorID() common.Hash {
	return common.HexToHash("0xaa1d49a4c084bfa9aeeee2a0be65267a7f19ba7e1476b114dac513d2c14cb563")
}

// UnpackUUPSUnsupportedProxiableUUIDError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error UUPSUnsupportedProxiableUUID(bytes32 slot)
func (sTSettlement2 *STSettlement2) UnpackUUPSUnsupportedProxiableUUIDError(raw []byte) (*STSettlement2UUPSUnsupportedProxiableUUID, error) {
	out := new(STSettlement2UUPSUnsupportedProxiableUUID)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "UUPSUnsupportedProxiableUUID", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2Unauthorized represents a Unauthorized error raised by the STSettlement2 contract.
type STSettlement2Unauthorized struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error Unauthorized()
func STSettlement2UnauthorizedErrorID() common.Hash {
	return common.HexToHash("0x82b4290015f7ec7256ca2a6247d3c2a89c4865c0e791456df195f40ad0a81367")
}

// UnpackUnauthorizedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error Unauthorized()
func (sTSettlement2 *STSettlement2) UnpackUnauthorizedError(raw []byte) (*STSettlement2Unauthorized, error) {
	out := new(STSettlement2Unauthorized)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "Unauthorized", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlement2UnknownPool represents a UnknownPool error raised by the STSettlement2 contract.
type STSettlement2UnknownPool struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error UnknownPool()
func STSettlement2UnknownPoolErrorID() common.Hash {
	return common.HexToHash("0xf7139e330b4806569379fe159fc41bed9c88b667fc4b9b3c7047d03f1a25197a")
}

// UnpackUnknownPoolError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error UnknownPool()
func (sTSettlement2 *STSettlement2) UnpackUnknownPoolError(raw []byte) (*STSettlement2UnknownPool, error) {
	out := new(STSettlement2UnknownPool)
	if err := sTSettlement2.abi.UnpackIntoInterface(out, "UnknownPool", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2MetaData contains all meta data concerning the STSettlementVault2 contract.
var STSettlementVault2MetaData = bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"uint16\",\"name\":\"netuid_\",\"type\":\"uint16\"},{\"internalType\":\"bytes32\",\"name\":\"escrowHotkey_\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"selfColdkey_\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"bootstrap_\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"demandFeeWei_\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"CHALLENGE_BLOCKS\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"hotkey\",\"type\":\"bytes32\"}],\"name\":\"absorbStake\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"assignSurplus\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"bootstrap\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"captureEmission\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"capturedTotal\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"rootIndex\",\"type\":\"uint256\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"coldkey\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"cumulative\",\"type\":\"uint256\"}],\"internalType\":\"structLedgerTree.Account\",\"name\":\"account\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"hash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"sum\",\"type\":\"uint256\"}],\"internalType\":\"structLedgerTree.ProofNode[]\",\"name\":\"oldProof\",\"type\":\"tuple[]\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"leftHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"leftSum\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"rightHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"rightSum\",\"type\":\"uint256\"}],\"internalType\":\"structLedgerTree.PathNode[]\",\"name\":\"newPath\",\"type\":\"tuple[]\"}],\"name\":\"checkPath\",\"outputs\":[{\"internalType\":\"enumLedgerTree.Fault\",\"name\":\"fault\",\"type\":\"uint8\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"rootIndex\",\"type\":\"uint256\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"coldkey\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"cumulative\",\"type\":\"uint256\"}],\"internalType\":\"structLedgerTree.Account\",\"name\":\"account\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"hash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"sum\",\"type\":\"uint256\"}],\"internalType\":\"structLedgerTree.ProofNode[]\",\"name\":\"proof\",\"type\":\"tuple[]\"}],\"name\":\"claim\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"}],\"name\":\"claimed\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"claimedTotal\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"prevRootCount\",\"type\":\"uint256\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"root\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"total\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"leafCount\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"ledgerHash\",\"type\":\"bytes32\"}],\"internalType\":\"structISTSettlementVault2.Commitment\",\"name\":\"commitment\",\"type\":\"tuple\"}],\"name\":\"commitPending\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"seq\",\"type\":\"uint64\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"commitSeq\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"seq\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"conservationHolds\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"coordinator\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"deferEmission\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"demandFeeWei\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"rootIndex\",\"type\":\"uint256\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"coldkey\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"cumulative\",\"type\":\"uint256\"}],\"internalType\":\"structLedgerTree.Account\",\"name\":\"account\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"hash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"sum\",\"type\":\"uint256\"}],\"internalType\":\"structLedgerTree.ProofNode[]\",\"name\":\"oldProof\",\"type\":\"tuple[]\"}],\"name\":\"demandPath\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"seq\",\"type\":\"uint64\"},{\"internalType\":\"uint256\",\"name\":\"rootIndex\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"}],\"name\":\"demanded\",\"outputs\":[{\"internalType\":\"enumISTSettlementVault2.DemandState\",\"name\":\"state\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"escrowAccounted\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"escrowHotkey\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"escrowRegistered\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"finalizePending\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"rootIndex\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"finalizedTotal\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"}],\"name\":\"fundedThrough\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"lastCommitBlock\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"blockNumber\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"lastFundedEpoch\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"liveEscrowStake\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"netuid\",\"outputs\":[{\"internalType\":\"uint16\",\"name\":\"\",\"type\":\"uint16\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"pending\",\"outputs\":[{\"components\":[{\"internalType\":\"uint64\",\"name\":\"seq\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"root\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"total\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"leafCount\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"ledgerHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"committedBlock\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"openDemands\",\"type\":\"uint64\"}],\"internalType\":\"structISTSettlementVault2.Pending\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"hotkey\",\"type\":\"bytes32\"}],\"name\":\"poolHotkeyUsed\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"used\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"pools\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"hotkey\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"firstEpoch\",\"type\":\"uint64\"},{\"internalType\":\"bool\",\"name\":\"registered\",\"type\":\"bool\"}],\"internalType\":\"structISTSettlementVault2.Pool\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"maximumBurnRao\",\"type\":\"uint64\"}],\"name\":\"registerEscrow\",\"outputs\":[{\"internalType\":\"uint16\",\"name\":\"uid\",\"type\":\"uint16\"}],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"hotkey\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"maximumBurnRao\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"firstEpoch\",\"type\":\"uint64\"}],\"name\":\"registerPool\",\"outputs\":[{\"internalType\":\"uint16\",\"name\":\"uid\",\"type\":\"uint16\"}],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"maximumBurnRao\",\"type\":\"uint64\"}],\"name\":\"registerPoolUid\",\"outputs\":[{\"internalType\":\"uint16\",\"name\":\"uid\",\"type\":\"uint16\"}],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"}],\"name\":\"rootCount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"rootIndex\",\"type\":\"uint256\"}],\"name\":\"roots\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"root\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"total\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"leafCount\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"internalType\":\"bytes32\",\"name\":\"ledgerHash\",\"type\":\"bytes32\"}],\"internalType\":\"structISTSettlementVault2.Root\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"selfColdkey\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"coordinator_\",\"type\":\"address\"}],\"name\":\"setCoordinatorOnce\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"totalCaptured\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"totalPaid\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"rootIndex\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"coldkey\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"cumulative\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"caller\",\"type\":\"address\"}],\"name\":\"Claimed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"coordinator\",\"type\":\"address\"}],\"name\":\"CoordinatorFixed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"seq\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"root\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"total\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"leafCount\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"ledgerHash\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"replacedSeq\",\"type\":\"uint64\"}],\"name\":\"DistributionCommitted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"seq\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"rootIndex\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"enumLedgerTree.Fault\",\"name\":\"fault\",\"type\":\"uint8\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"level\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"challenger\",\"type\":\"address\"}],\"name\":\"DistributionDeleted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"seq\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"rootIndex\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"root\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"total\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"leafCount\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"ledgerHash\",\"type\":\"bytes32\"}],\"name\":\"DistributionFinalized\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"observed\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"fundedThrough\",\"type\":\"uint256\"}],\"name\":\"EmissionCaptured\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"fundedThrough\",\"type\":\"uint256\"}],\"name\":\"EmissionDeferred\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"hotkey\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint16\",\"name\":\"uid\",\"type\":\"uint16\"}],\"name\":\"EscrowRegistered\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"seq\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"rootIndex\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"answerer\",\"type\":\"address\"}],\"name\":\"PathAnswered\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"seq\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"rootIndex\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"demander\",\"type\":\"address\"}],\"name\":\"PathDemanded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"hotkey\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint16\",\"name\":\"uid\",\"type\":\"uint16\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"firstEpoch\",\"type\":\"uint64\"}],\"name\":\"PoolRegistered\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"hotkey\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint16\",\"name\":\"uid\",\"type\":\"uint16\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"payer\",\"type\":\"address\"}],\"name\":\"PoolUidRegistered\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"hotkey\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"observed\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"caller\",\"type\":\"address\"}],\"name\":\"StakeAbsorbed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"noId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"SurplusAssigned\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"AlreadyDemanded\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"AlreadyInitialized\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"AmountTooLarge\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ChallengeWindowClosed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ChallengeWindowOpen\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"CommitTooSoon\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"DemandOpen\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"EpochOutOfRange\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"IndexOutOfRange\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidColdkey\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidConfiguration\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidDemandFee\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidHotkey\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidLeafCount\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidLeafCount\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidProof\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidRoot\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidSurplus\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidTransition\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"MalformedPath\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NativeBalanceDecreased\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NativeRefundFailed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NoFault\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NoFinalizedRoot\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NoPendingRoot\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NothingToAbsorb\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NothingToClaim\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"PoolCapExceeded\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"Reentrancy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"RuntimeAccountingMismatch\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"uint8\",\"name\":\"bits\",\"type\":\"uint8\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"SafeCastOverflowedUintDowncast\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"StaleRootCount\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"TotalMismatch\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"TotalNotIncreased\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UidExists\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"Unauthorized\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"Underfunded\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UnknownPool\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UnknownRoot\",\"type\":\"error\"}]",
	ID:  "92d3f3ace1a953d2a491cc7b5b5a1fa65c",
}

// STSettlementVault2 is an auto generated Go binding around an Ethereum contract.
type STSettlementVault2 struct {
	abi abi.ABI
}

// NewSTSettlementVault2 creates a new instance of STSettlementVault2.
func NewSTSettlementVault2() *STSettlementVault2 {
	parsed, err := STSettlementVault2MetaData.ParseABI()
	if err != nil {
		panic(errors.New("invalid ABI: " + err.Error()))
	}
	return &STSettlementVault2{abi: *parsed}
}

// Instance creates a wrapper for a deployed contract instance at the given address.
// Use this to create the instance object passed to abigen v2 library functions Call, Transact, etc.
func (c *STSettlementVault2) Instance(backend bind.ContractBackend, addr common.Address) *bind.BoundContract {
	return bind.NewBoundContract(addr, c.abi, backend, backend, backend)
}

// PackConstructor is the Go binding used to pack the parameters required for
// contract deployment.
//
// Solidity: constructor(uint16 netuid_, bytes32 escrowHotkey_, bytes32 selfColdkey_, address bootstrap_, uint256 demandFeeWei_) returns()
func (sTSettlementVault2 *STSettlementVault2) PackConstructor(netuid_ uint16, escrowHotkey_ [32]byte, selfColdkey_ [32]byte, bootstrap_ common.Address, demandFeeWei_ *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("", netuid_, escrowHotkey_, selfColdkey_, bootstrap_, demandFeeWei_)
	if err != nil {
		panic(err)
	}
	return enc
}

// PackCHALLENGEBLOCKS is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x400ee9b1.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function CHALLENGE_BLOCKS() view returns(uint64)
func (sTSettlementVault2 *STSettlementVault2) PackCHALLENGEBLOCKS() []byte {
	enc, err := sTSettlementVault2.abi.Pack("CHALLENGE_BLOCKS")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCHALLENGEBLOCKS is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x400ee9b1.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function CHALLENGE_BLOCKS() view returns(uint64)
func (sTSettlementVault2 *STSettlementVault2) TryPackCHALLENGEBLOCKS() ([]byte, error) {
	return sTSettlementVault2.abi.Pack("CHALLENGE_BLOCKS")
}

// UnpackCHALLENGEBLOCKS is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x400ee9b1.
//
// Solidity: function CHALLENGE_BLOCKS() view returns(uint64)
func (sTSettlementVault2 *STSettlementVault2) UnpackCHALLENGEBLOCKS(data []byte) (uint64, error) {
	out, err := sTSettlementVault2.abi.Unpack("CHALLENGE_BLOCKS", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackAbsorbStake is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x585480db.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function absorbStake(bytes32 hotkey) returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) PackAbsorbStake(hotkey [32]byte) []byte {
	enc, err := sTSettlementVault2.abi.Pack("absorbStake", hotkey)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackAbsorbStake is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x585480db.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function absorbStake(bytes32 hotkey) returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) TryPackAbsorbStake(hotkey [32]byte) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("absorbStake", hotkey)
}

// UnpackAbsorbStake is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x585480db.
//
// Solidity: function absorbStake(bytes32 hotkey) returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) UnpackAbsorbStake(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("absorbStake", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackAssignSurplus is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x5f57f630.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function assignSurplus(uint256 noId, uint256 amount) returns()
func (sTSettlementVault2 *STSettlementVault2) PackAssignSurplus(noId *big.Int, amount *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("assignSurplus", noId, amount)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackAssignSurplus is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x5f57f630.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function assignSurplus(uint256 noId, uint256 amount) returns()
func (sTSettlementVault2 *STSettlementVault2) TryPackAssignSurplus(noId *big.Int, amount *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("assignSurplus", noId, amount)
}

// PackBootstrap is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xfb969b0a.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function bootstrap() view returns(address)
func (sTSettlementVault2 *STSettlementVault2) PackBootstrap() []byte {
	enc, err := sTSettlementVault2.abi.Pack("bootstrap")
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
func (sTSettlementVault2 *STSettlementVault2) TryPackBootstrap() ([]byte, error) {
	return sTSettlementVault2.abi.Pack("bootstrap")
}

// UnpackBootstrap is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xfb969b0a.
//
// Solidity: function bootstrap() view returns(address)
func (sTSettlementVault2 *STSettlementVault2) UnpackBootstrap(data []byte) (common.Address, error) {
	out, err := sTSettlementVault2.abi.Unpack("bootstrap", data)
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
func (sTSettlementVault2 *STSettlementVault2) PackCaptureEmission(epoch *big.Int, noId *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("captureEmission", epoch, noId)
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
func (sTSettlementVault2 *STSettlementVault2) TryPackCaptureEmission(epoch *big.Int, noId *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("captureEmission", epoch, noId)
}

// UnpackCaptureEmission is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xe0b08aa5.
//
// Solidity: function captureEmission(uint256 epoch, uint256 noId) returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) UnpackCaptureEmission(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("captureEmission", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackCapturedTotal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x9e2a3bc2.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function capturedTotal(uint256 noId) view returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) PackCapturedTotal(noId *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("capturedTotal", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCapturedTotal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x9e2a3bc2.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function capturedTotal(uint256 noId) view returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) TryPackCapturedTotal(noId *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("capturedTotal", noId)
}

// UnpackCapturedTotal is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x9e2a3bc2.
//
// Solidity: function capturedTotal(uint256 noId) view returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) UnpackCapturedTotal(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("capturedTotal", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackCheckPath is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xaa001e62.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function checkPath(uint256 noId, uint256 rootIndex, (uint256,bytes32,uint256) account, (bytes32,uint256)[] oldProof, (bytes32,uint256,bytes32,uint256)[] newPath) returns(uint8 fault)
func (sTSettlementVault2 *STSettlementVault2) PackCheckPath(noId *big.Int, rootIndex *big.Int, account LedgerTreeAccount, oldProof []LedgerTreeProofNode, newPath []LedgerTreePathNode) []byte {
	enc, err := sTSettlementVault2.abi.Pack("checkPath", noId, rootIndex, account, oldProof, newPath)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCheckPath is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xaa001e62.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function checkPath(uint256 noId, uint256 rootIndex, (uint256,bytes32,uint256) account, (bytes32,uint256)[] oldProof, (bytes32,uint256,bytes32,uint256)[] newPath) returns(uint8 fault)
func (sTSettlementVault2 *STSettlementVault2) TryPackCheckPath(noId *big.Int, rootIndex *big.Int, account LedgerTreeAccount, oldProof []LedgerTreeProofNode, newPath []LedgerTreePathNode) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("checkPath", noId, rootIndex, account, oldProof, newPath)
}

// UnpackCheckPath is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xaa001e62.
//
// Solidity: function checkPath(uint256 noId, uint256 rootIndex, (uint256,bytes32,uint256) account, (bytes32,uint256)[] oldProof, (bytes32,uint256,bytes32,uint256)[] newPath) returns(uint8 fault)
func (sTSettlementVault2 *STSettlementVault2) UnpackCheckPath(data []byte) (uint8, error) {
	out, err := sTSettlementVault2.abi.Unpack("checkPath", data)
	if err != nil {
		return *new(uint8), err
	}
	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)
	return out0, nil
}

// PackClaim is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xb7a6b907.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function claim(uint256 noId, uint256 rootIndex, (uint256,bytes32,uint256) account, (bytes32,uint256)[] proof) returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) PackClaim(noId *big.Int, rootIndex *big.Int, account LedgerTreeAccount, proof []LedgerTreeProofNode) []byte {
	enc, err := sTSettlementVault2.abi.Pack("claim", noId, rootIndex, account, proof)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackClaim is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xb7a6b907.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function claim(uint256 noId, uint256 rootIndex, (uint256,bytes32,uint256) account, (bytes32,uint256)[] proof) returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) TryPackClaim(noId *big.Int, rootIndex *big.Int, account LedgerTreeAccount, proof []LedgerTreeProofNode) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("claim", noId, rootIndex, account, proof)
}

// UnpackClaim is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xb7a6b907.
//
// Solidity: function claim(uint256 noId, uint256 rootIndex, (uint256,bytes32,uint256) account, (bytes32,uint256)[] proof) returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) UnpackClaim(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("claim", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackClaimed is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa54ab457.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function claimed(uint256 noId, uint256 index) view returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) PackClaimed(noId *big.Int, index *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("claimed", noId, index)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackClaimed is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa54ab457.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function claimed(uint256 noId, uint256 index) view returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) TryPackClaimed(noId *big.Int, index *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("claimed", noId, index)
}

// UnpackClaimed is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xa54ab457.
//
// Solidity: function claimed(uint256 noId, uint256 index) view returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) UnpackClaimed(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("claimed", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackClaimedTotal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xff6ad927.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function claimedTotal(uint256 noId) view returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) PackClaimedTotal(noId *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("claimedTotal", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackClaimedTotal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xff6ad927.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function claimedTotal(uint256 noId) view returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) TryPackClaimedTotal(noId *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("claimedTotal", noId)
}

// UnpackClaimedTotal is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xff6ad927.
//
// Solidity: function claimedTotal(uint256 noId) view returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) UnpackClaimedTotal(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("claimedTotal", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackCommitPending is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xdaa53270.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function commitPending(uint256 epoch, uint256 noId, uint256 prevRootCount, (bytes32,uint256,uint64,bytes32) commitment) returns(uint64 seq)
func (sTSettlementVault2 *STSettlementVault2) PackCommitPending(epoch *big.Int, noId *big.Int, prevRootCount *big.Int, commitment ISTSettlementVault2Commitment) []byte {
	enc, err := sTSettlementVault2.abi.Pack("commitPending", epoch, noId, prevRootCount, commitment)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCommitPending is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xdaa53270.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function commitPending(uint256 epoch, uint256 noId, uint256 prevRootCount, (bytes32,uint256,uint64,bytes32) commitment) returns(uint64 seq)
func (sTSettlementVault2 *STSettlementVault2) TryPackCommitPending(epoch *big.Int, noId *big.Int, prevRootCount *big.Int, commitment ISTSettlementVault2Commitment) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("commitPending", epoch, noId, prevRootCount, commitment)
}

// UnpackCommitPending is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xdaa53270.
//
// Solidity: function commitPending(uint256 epoch, uint256 noId, uint256 prevRootCount, (bytes32,uint256,uint64,bytes32) commitment) returns(uint64 seq)
func (sTSettlementVault2 *STSettlementVault2) UnpackCommitPending(data []byte) (uint64, error) {
	out, err := sTSettlementVault2.abi.Unpack("commitPending", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackCommitSeq is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x9e6d81f9.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function commitSeq(uint256 noId) view returns(uint64 seq)
func (sTSettlementVault2 *STSettlementVault2) PackCommitSeq(noId *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("commitSeq", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCommitSeq is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x9e6d81f9.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function commitSeq(uint256 noId) view returns(uint64 seq)
func (sTSettlementVault2 *STSettlementVault2) TryPackCommitSeq(noId *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("commitSeq", noId)
}

// UnpackCommitSeq is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x9e6d81f9.
//
// Solidity: function commitSeq(uint256 noId) view returns(uint64 seq)
func (sTSettlementVault2 *STSettlementVault2) UnpackCommitSeq(data []byte) (uint64, error) {
	out, err := sTSettlementVault2.abi.Unpack("commitSeq", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackConservationHolds is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x13944c3f.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function conservationHolds() view returns(bool)
func (sTSettlementVault2 *STSettlementVault2) PackConservationHolds() []byte {
	enc, err := sTSettlementVault2.abi.Pack("conservationHolds")
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
func (sTSettlementVault2 *STSettlementVault2) TryPackConservationHolds() ([]byte, error) {
	return sTSettlementVault2.abi.Pack("conservationHolds")
}

// UnpackConservationHolds is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x13944c3f.
//
// Solidity: function conservationHolds() view returns(bool)
func (sTSettlementVault2 *STSettlementVault2) UnpackConservationHolds(data []byte) (bool, error) {
	out, err := sTSettlementVault2.abi.Unpack("conservationHolds", data)
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
func (sTSettlementVault2 *STSettlementVault2) PackCoordinator() []byte {
	enc, err := sTSettlementVault2.abi.Pack("coordinator")
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
func (sTSettlementVault2 *STSettlementVault2) TryPackCoordinator() ([]byte, error) {
	return sTSettlementVault2.abi.Pack("coordinator")
}

// UnpackCoordinator is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x0a009097.
//
// Solidity: function coordinator() view returns(address)
func (sTSettlementVault2 *STSettlementVault2) UnpackCoordinator(data []byte) (common.Address, error) {
	out, err := sTSettlementVault2.abi.Unpack("coordinator", data)
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
func (sTSettlementVault2 *STSettlementVault2) PackDeferEmission(epoch *big.Int, noId *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("deferEmission", epoch, noId)
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
func (sTSettlementVault2 *STSettlementVault2) TryPackDeferEmission(epoch *big.Int, noId *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("deferEmission", epoch, noId)
}

// PackDemandFeeWei is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa70778b5.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function demandFeeWei() view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) PackDemandFeeWei() []byte {
	enc, err := sTSettlementVault2.abi.Pack("demandFeeWei")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackDemandFeeWei is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa70778b5.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function demandFeeWei() view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) TryPackDemandFeeWei() ([]byte, error) {
	return sTSettlementVault2.abi.Pack("demandFeeWei")
}

// UnpackDemandFeeWei is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xa70778b5.
//
// Solidity: function demandFeeWei() view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) UnpackDemandFeeWei(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("demandFeeWei", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackDemandPath is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x007185b0.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function demandPath(uint256 noId, uint256 rootIndex, (uint256,bytes32,uint256) account, (bytes32,uint256)[] oldProof) payable returns()
func (sTSettlementVault2 *STSettlementVault2) PackDemandPath(noId *big.Int, rootIndex *big.Int, account LedgerTreeAccount, oldProof []LedgerTreeProofNode) []byte {
	enc, err := sTSettlementVault2.abi.Pack("demandPath", noId, rootIndex, account, oldProof)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackDemandPath is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x007185b0.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function demandPath(uint256 noId, uint256 rootIndex, (uint256,bytes32,uint256) account, (bytes32,uint256)[] oldProof) payable returns()
func (sTSettlementVault2 *STSettlementVault2) TryPackDemandPath(noId *big.Int, rootIndex *big.Int, account LedgerTreeAccount, oldProof []LedgerTreeProofNode) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("demandPath", noId, rootIndex, account, oldProof)
}

// PackDemanded is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xac7c72bb.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function demanded(uint256 noId, uint64 seq, uint256 rootIndex, uint256 index) view returns(uint8 state)
func (sTSettlementVault2 *STSettlementVault2) PackDemanded(noId *big.Int, seq uint64, rootIndex *big.Int, index *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("demanded", noId, seq, rootIndex, index)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackDemanded is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xac7c72bb.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function demanded(uint256 noId, uint64 seq, uint256 rootIndex, uint256 index) view returns(uint8 state)
func (sTSettlementVault2 *STSettlementVault2) TryPackDemanded(noId *big.Int, seq uint64, rootIndex *big.Int, index *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("demanded", noId, seq, rootIndex, index)
}

// UnpackDemanded is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xac7c72bb.
//
// Solidity: function demanded(uint256 noId, uint64 seq, uint256 rootIndex, uint256 index) view returns(uint8 state)
func (sTSettlementVault2 *STSettlementVault2) UnpackDemanded(data []byte) (uint8, error) {
	out, err := sTSettlementVault2.abi.Unpack("demanded", data)
	if err != nil {
		return *new(uint8), err
	}
	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)
	return out0, nil
}

// PackEscrowAccounted is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xfd67fc0e.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function escrowAccounted() view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) PackEscrowAccounted() []byte {
	enc, err := sTSettlementVault2.abi.Pack("escrowAccounted")
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
func (sTSettlementVault2 *STSettlementVault2) TryPackEscrowAccounted() ([]byte, error) {
	return sTSettlementVault2.abi.Pack("escrowAccounted")
}

// UnpackEscrowAccounted is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xfd67fc0e.
//
// Solidity: function escrowAccounted() view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) UnpackEscrowAccounted(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("escrowAccounted", data)
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
func (sTSettlementVault2 *STSettlementVault2) PackEscrowHotkey() []byte {
	enc, err := sTSettlementVault2.abi.Pack("escrowHotkey")
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
func (sTSettlementVault2 *STSettlementVault2) TryPackEscrowHotkey() ([]byte, error) {
	return sTSettlementVault2.abi.Pack("escrowHotkey")
}

// UnpackEscrowHotkey is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x164bf360.
//
// Solidity: function escrowHotkey() view returns(bytes32)
func (sTSettlementVault2 *STSettlementVault2) UnpackEscrowHotkey(data []byte) ([32]byte, error) {
	out, err := sTSettlementVault2.abi.Unpack("escrowHotkey", data)
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
func (sTSettlementVault2 *STSettlementVault2) PackEscrowRegistered() []byte {
	enc, err := sTSettlementVault2.abi.Pack("escrowRegistered")
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
func (sTSettlementVault2 *STSettlementVault2) TryPackEscrowRegistered() ([]byte, error) {
	return sTSettlementVault2.abi.Pack("escrowRegistered")
}

// UnpackEscrowRegistered is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x92770c58.
//
// Solidity: function escrowRegistered() view returns(bool)
func (sTSettlementVault2 *STSettlementVault2) UnpackEscrowRegistered(data []byte) (bool, error) {
	out, err := sTSettlementVault2.abi.Unpack("escrowRegistered", data)
	if err != nil {
		return *new(bool), err
	}
	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)
	return out0, nil
}

// PackFinalizePending is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x75db4441.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function finalizePending(uint256 noId) returns(uint256 rootIndex)
func (sTSettlementVault2 *STSettlementVault2) PackFinalizePending(noId *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("finalizePending", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackFinalizePending is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x75db4441.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function finalizePending(uint256 noId) returns(uint256 rootIndex)
func (sTSettlementVault2 *STSettlementVault2) TryPackFinalizePending(noId *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("finalizePending", noId)
}

// UnpackFinalizePending is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x75db4441.
//
// Solidity: function finalizePending(uint256 noId) returns(uint256 rootIndex)
func (sTSettlementVault2 *STSettlementVault2) UnpackFinalizePending(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("finalizePending", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackFinalizedTotal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa9e2b324.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function finalizedTotal(uint256 noId) view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) PackFinalizedTotal(noId *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("finalizedTotal", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackFinalizedTotal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa9e2b324.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function finalizedTotal(uint256 noId) view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) TryPackFinalizedTotal(noId *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("finalizedTotal", noId)
}

// UnpackFinalizedTotal is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xa9e2b324.
//
// Solidity: function finalizedTotal(uint256 noId) view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) UnpackFinalizedTotal(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("finalizedTotal", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackFundedThrough is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xcccbd0b7.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function fundedThrough(uint256 noId, uint256 epoch) view returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) PackFundedThrough(noId *big.Int, epoch *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("fundedThrough", noId, epoch)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackFundedThrough is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xcccbd0b7.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function fundedThrough(uint256 noId, uint256 epoch) view returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) TryPackFundedThrough(noId *big.Int, epoch *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("fundedThrough", noId, epoch)
}

// UnpackFundedThrough is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xcccbd0b7.
//
// Solidity: function fundedThrough(uint256 noId, uint256 epoch) view returns(uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) UnpackFundedThrough(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("fundedThrough", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackLastCommitBlock is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x82c50e4c.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function lastCommitBlock(uint256 noId) view returns(uint64 blockNumber)
func (sTSettlementVault2 *STSettlementVault2) PackLastCommitBlock(noId *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("lastCommitBlock", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackLastCommitBlock is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x82c50e4c.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function lastCommitBlock(uint256 noId) view returns(uint64 blockNumber)
func (sTSettlementVault2 *STSettlementVault2) TryPackLastCommitBlock(noId *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("lastCommitBlock", noId)
}

// UnpackLastCommitBlock is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x82c50e4c.
//
// Solidity: function lastCommitBlock(uint256 noId) view returns(uint64 blockNumber)
func (sTSettlementVault2 *STSettlementVault2) UnpackLastCommitBlock(data []byte) (uint64, error) {
	out, err := sTSettlementVault2.abi.Unpack("lastCommitBlock", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackLastFundedEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc63bc9d4.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function lastFundedEpoch(uint256 noId) view returns(uint256 epoch)
func (sTSettlementVault2 *STSettlementVault2) PackLastFundedEpoch(noId *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("lastFundedEpoch", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackLastFundedEpoch is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc63bc9d4.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function lastFundedEpoch(uint256 noId) view returns(uint256 epoch)
func (sTSettlementVault2 *STSettlementVault2) TryPackLastFundedEpoch(noId *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("lastFundedEpoch", noId)
}

// UnpackLastFundedEpoch is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xc63bc9d4.
//
// Solidity: function lastFundedEpoch(uint256 noId) view returns(uint256 epoch)
func (sTSettlementVault2 *STSettlementVault2) UnpackLastFundedEpoch(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("lastFundedEpoch", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackLiveEscrowStake is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf2f930ec.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function liveEscrowStake() view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) PackLiveEscrowStake() []byte {
	enc, err := sTSettlementVault2.abi.Pack("liveEscrowStake")
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
func (sTSettlementVault2 *STSettlementVault2) TryPackLiveEscrowStake() ([]byte, error) {
	return sTSettlementVault2.abi.Pack("liveEscrowStake")
}

// UnpackLiveEscrowStake is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xf2f930ec.
//
// Solidity: function liveEscrowStake() view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) UnpackLiveEscrowStake(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("liveEscrowStake", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackNetuid is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe78015b1.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function netuid() view returns(uint16)
func (sTSettlementVault2 *STSettlementVault2) PackNetuid() []byte {
	enc, err := sTSettlementVault2.abi.Pack("netuid")
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
func (sTSettlementVault2 *STSettlementVault2) TryPackNetuid() ([]byte, error) {
	return sTSettlementVault2.abi.Pack("netuid")
}

// UnpackNetuid is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xe78015b1.
//
// Solidity: function netuid() view returns(uint16)
func (sTSettlementVault2 *STSettlementVault2) UnpackNetuid(data []byte) (uint16, error) {
	out, err := sTSettlementVault2.abi.Unpack("netuid", data)
	if err != nil {
		return *new(uint16), err
	}
	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)
	return out0, nil
}

// PackPending is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xaf6e6238.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function pending(uint256 noId) view returns((uint64,bytes32,uint256,uint64,uint64,bytes32,uint64,uint64))
func (sTSettlementVault2 *STSettlementVault2) PackPending(noId *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("pending", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPending is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xaf6e6238.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function pending(uint256 noId) view returns((uint64,bytes32,uint256,uint64,uint64,bytes32,uint64,uint64))
func (sTSettlementVault2 *STSettlementVault2) TryPackPending(noId *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("pending", noId)
}

// UnpackPending is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xaf6e6238.
//
// Solidity: function pending(uint256 noId) view returns((uint64,bytes32,uint256,uint64,uint64,bytes32,uint64,uint64))
func (sTSettlementVault2 *STSettlementVault2) UnpackPending(data []byte) (ISTSettlementVault2Pending, error) {
	out, err := sTSettlementVault2.abi.Unpack("pending", data)
	if err != nil {
		return *new(ISTSettlementVault2Pending), err
	}
	out0 := *abi.ConvertType(out[0], new(ISTSettlementVault2Pending)).(*ISTSettlementVault2Pending)
	return out0, nil
}

// PackPoolHotkeyUsed is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x411e90d5.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function poolHotkeyUsed(bytes32 hotkey) view returns(bool used)
func (sTSettlementVault2 *STSettlementVault2) PackPoolHotkeyUsed(hotkey [32]byte) []byte {
	enc, err := sTSettlementVault2.abi.Pack("poolHotkeyUsed", hotkey)
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
func (sTSettlementVault2 *STSettlementVault2) TryPackPoolHotkeyUsed(hotkey [32]byte) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("poolHotkeyUsed", hotkey)
}

// UnpackPoolHotkeyUsed is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x411e90d5.
//
// Solidity: function poolHotkeyUsed(bytes32 hotkey) view returns(bool used)
func (sTSettlementVault2 *STSettlementVault2) UnpackPoolHotkeyUsed(data []byte) (bool, error) {
	out, err := sTSettlementVault2.abi.Unpack("poolHotkeyUsed", data)
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
// Solidity: function pools(uint256 noId) view returns((bytes32,uint64,bool))
func (sTSettlementVault2 *STSettlementVault2) PackPools(noId *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("pools", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPools is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xac4afa38.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function pools(uint256 noId) view returns((bytes32,uint64,bool))
func (sTSettlementVault2 *STSettlementVault2) TryPackPools(noId *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("pools", noId)
}

// UnpackPools is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xac4afa38.
//
// Solidity: function pools(uint256 noId) view returns((bytes32,uint64,bool))
func (sTSettlementVault2 *STSettlementVault2) UnpackPools(data []byte) (ISTSettlementVault2Pool, error) {
	out, err := sTSettlementVault2.abi.Unpack("pools", data)
	if err != nil {
		return *new(ISTSettlementVault2Pool), err
	}
	out0 := *abi.ConvertType(out[0], new(ISTSettlementVault2Pool)).(*ISTSettlementVault2Pool)
	return out0, nil
}

// PackRegisterEscrow is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x775d2762.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function registerEscrow(uint64 maximumBurnRao) payable returns(uint16 uid)
func (sTSettlementVault2 *STSettlementVault2) PackRegisterEscrow(maximumBurnRao uint64) []byte {
	enc, err := sTSettlementVault2.abi.Pack("registerEscrow", maximumBurnRao)
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
func (sTSettlementVault2 *STSettlementVault2) TryPackRegisterEscrow(maximumBurnRao uint64) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("registerEscrow", maximumBurnRao)
}

// UnpackRegisterEscrow is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x775d2762.
//
// Solidity: function registerEscrow(uint64 maximumBurnRao) payable returns(uint16 uid)
func (sTSettlementVault2 *STSettlementVault2) UnpackRegisterEscrow(data []byte) (uint16, error) {
	out, err := sTSettlementVault2.abi.Unpack("registerEscrow", data)
	if err != nil {
		return *new(uint16), err
	}
	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)
	return out0, nil
}

// PackRegisterPool is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa05ec6a9.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function registerPool(uint256 noId, bytes32 hotkey, uint64 maximumBurnRao, uint64 firstEpoch) payable returns(uint16 uid)
func (sTSettlementVault2 *STSettlementVault2) PackRegisterPool(noId *big.Int, hotkey [32]byte, maximumBurnRao uint64, firstEpoch uint64) []byte {
	enc, err := sTSettlementVault2.abi.Pack("registerPool", noId, hotkey, maximumBurnRao, firstEpoch)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRegisterPool is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xa05ec6a9.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function registerPool(uint256 noId, bytes32 hotkey, uint64 maximumBurnRao, uint64 firstEpoch) payable returns(uint16 uid)
func (sTSettlementVault2 *STSettlementVault2) TryPackRegisterPool(noId *big.Int, hotkey [32]byte, maximumBurnRao uint64, firstEpoch uint64) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("registerPool", noId, hotkey, maximumBurnRao, firstEpoch)
}

// UnpackRegisterPool is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xa05ec6a9.
//
// Solidity: function registerPool(uint256 noId, bytes32 hotkey, uint64 maximumBurnRao, uint64 firstEpoch) payable returns(uint16 uid)
func (sTSettlementVault2 *STSettlementVault2) UnpackRegisterPool(data []byte) (uint16, error) {
	out, err := sTSettlementVault2.abi.Unpack("registerPool", data)
	if err != nil {
		return *new(uint16), err
	}
	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)
	return out0, nil
}

// PackRegisterPoolUid is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x0450b020.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function registerPoolUid(uint256 noId, uint64 maximumBurnRao) payable returns(uint16 uid)
func (sTSettlementVault2 *STSettlementVault2) PackRegisterPoolUid(noId *big.Int, maximumBurnRao uint64) []byte {
	enc, err := sTSettlementVault2.abi.Pack("registerPoolUid", noId, maximumBurnRao)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRegisterPoolUid is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x0450b020.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function registerPoolUid(uint256 noId, uint64 maximumBurnRao) payable returns(uint16 uid)
func (sTSettlementVault2 *STSettlementVault2) TryPackRegisterPoolUid(noId *big.Int, maximumBurnRao uint64) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("registerPoolUid", noId, maximumBurnRao)
}

// UnpackRegisterPoolUid is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x0450b020.
//
// Solidity: function registerPoolUid(uint256 noId, uint64 maximumBurnRao) payable returns(uint16 uid)
func (sTSettlementVault2 *STSettlementVault2) UnpackRegisterPoolUid(data []byte) (uint16, error) {
	out, err := sTSettlementVault2.abi.Unpack("registerPoolUid", data)
	if err != nil {
		return *new(uint16), err
	}
	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)
	return out0, nil
}

// PackRootCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x619ebd13.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function rootCount(uint256 noId) view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) PackRootCount(noId *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("rootCount", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRootCount is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x619ebd13.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function rootCount(uint256 noId) view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) TryPackRootCount(noId *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("rootCount", noId)
}

// UnpackRootCount is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x619ebd13.
//
// Solidity: function rootCount(uint256 noId) view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) UnpackRootCount(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("rootCount", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackRoots is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x0fc3cec0.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function roots(uint256 noId, uint256 rootIndex) view returns((bytes32,uint256,uint64,uint64,bytes32))
func (sTSettlementVault2 *STSettlementVault2) PackRoots(noId *big.Int, rootIndex *big.Int) []byte {
	enc, err := sTSettlementVault2.abi.Pack("roots", noId, rootIndex)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRoots is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x0fc3cec0.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function roots(uint256 noId, uint256 rootIndex) view returns((bytes32,uint256,uint64,uint64,bytes32))
func (sTSettlementVault2 *STSettlementVault2) TryPackRoots(noId *big.Int, rootIndex *big.Int) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("roots", noId, rootIndex)
}

// UnpackRoots is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x0fc3cec0.
//
// Solidity: function roots(uint256 noId, uint256 rootIndex) view returns((bytes32,uint256,uint64,uint64,bytes32))
func (sTSettlementVault2 *STSettlementVault2) UnpackRoots(data []byte) (ISTSettlementVault2Root, error) {
	out, err := sTSettlementVault2.abi.Unpack("roots", data)
	if err != nil {
		return *new(ISTSettlementVault2Root), err
	}
	out0 := *abi.ConvertType(out[0], new(ISTSettlementVault2Root)).(*ISTSettlementVault2Root)
	return out0, nil
}

// PackSelfColdkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x877e4394.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function selfColdkey() view returns(bytes32)
func (sTSettlementVault2 *STSettlementVault2) PackSelfColdkey() []byte {
	enc, err := sTSettlementVault2.abi.Pack("selfColdkey")
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
func (sTSettlementVault2 *STSettlementVault2) TryPackSelfColdkey() ([]byte, error) {
	return sTSettlementVault2.abi.Pack("selfColdkey")
}

// UnpackSelfColdkey is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x877e4394.
//
// Solidity: function selfColdkey() view returns(bytes32)
func (sTSettlementVault2 *STSettlementVault2) UnpackSelfColdkey(data []byte) ([32]byte, error) {
	out, err := sTSettlementVault2.abi.Unpack("selfColdkey", data)
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
func (sTSettlementVault2 *STSettlementVault2) PackSetCoordinatorOnce(coordinator common.Address) []byte {
	enc, err := sTSettlementVault2.abi.Pack("setCoordinatorOnce", coordinator)
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
func (sTSettlementVault2 *STSettlementVault2) TryPackSetCoordinatorOnce(coordinator common.Address) ([]byte, error) {
	return sTSettlementVault2.abi.Pack("setCoordinatorOnce", coordinator)
}

// PackTotalCaptured is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x886f9669.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function totalCaptured() view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) PackTotalCaptured() []byte {
	enc, err := sTSettlementVault2.abi.Pack("totalCaptured")
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
func (sTSettlementVault2 *STSettlementVault2) TryPackTotalCaptured() ([]byte, error) {
	return sTSettlementVault2.abi.Pack("totalCaptured")
}

// UnpackTotalCaptured is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x886f9669.
//
// Solidity: function totalCaptured() view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) UnpackTotalCaptured(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("totalCaptured", data)
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
func (sTSettlementVault2 *STSettlementVault2) PackTotalPaid() []byte {
	enc, err := sTSettlementVault2.abi.Pack("totalPaid")
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
func (sTSettlementVault2 *STSettlementVault2) TryPackTotalPaid() ([]byte, error) {
	return sTSettlementVault2.abi.Pack("totalPaid")
}

// UnpackTotalPaid is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xe7b0f666.
//
// Solidity: function totalPaid() view returns(uint256)
func (sTSettlementVault2 *STSettlementVault2) UnpackTotalPaid(data []byte) (*big.Int, error) {
	out, err := sTSettlementVault2.abi.Unpack("totalPaid", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// STSettlementVault2Claimed represents a Claimed event raised by the STSettlementVault2 contract.
type STSettlementVault2Claimed struct {
	NoId       *big.Int
	RootIndex  *big.Int
	Index      *big.Int
	Coldkey    [32]byte
	Cumulative *big.Int
	Amount     *big.Int
	Caller     common.Address
	Raw        *types.Log // Blockchain specific contextual infos
}

const STSettlementVault2ClaimedEventName = "Claimed"

// ContractEventName returns the user-defined event name.
func (STSettlementVault2Claimed) ContractEventName() string {
	return STSettlementVault2ClaimedEventName
}

// UnpackClaimedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event Claimed(uint256 indexed noId, uint256 rootIndex, uint256 indexed index, bytes32 indexed coldkey, uint256 cumulative, uint256 amount, address caller)
func (sTSettlementVault2 *STSettlementVault2) UnpackClaimedEvent(log *types.Log) (*STSettlementVault2Claimed, error) {
	event := "Claimed"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVault2Claimed)
	if len(log.Data) > 0 {
		if err := sTSettlementVault2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault2.abi.Events[event].Inputs {
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

// STSettlementVault2CoordinatorFixed represents a CoordinatorFixed event raised by the STSettlementVault2 contract.
type STSettlementVault2CoordinatorFixed struct {
	Coordinator common.Address
	Raw         *types.Log // Blockchain specific contextual infos
}

const STSettlementVault2CoordinatorFixedEventName = "CoordinatorFixed"

// ContractEventName returns the user-defined event name.
func (STSettlementVault2CoordinatorFixed) ContractEventName() string {
	return STSettlementVault2CoordinatorFixedEventName
}

// UnpackCoordinatorFixedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event CoordinatorFixed(address indexed coordinator)
func (sTSettlementVault2 *STSettlementVault2) UnpackCoordinatorFixedEvent(log *types.Log) (*STSettlementVault2CoordinatorFixed, error) {
	event := "CoordinatorFixed"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVault2CoordinatorFixed)
	if len(log.Data) > 0 {
		if err := sTSettlementVault2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault2.abi.Events[event].Inputs {
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

// STSettlementVault2DistributionCommitted represents a DistributionCommitted event raised by the STSettlementVault2 contract.
type STSettlementVault2DistributionCommitted struct {
	NoId        *big.Int
	Seq         uint64
	Epoch       uint64
	Root        [32]byte
	Total       *big.Int
	LeafCount   uint64
	LedgerHash  [32]byte
	ReplacedSeq uint64
	Raw         *types.Log // Blockchain specific contextual infos
}

const STSettlementVault2DistributionCommittedEventName = "DistributionCommitted"

// ContractEventName returns the user-defined event name.
func (STSettlementVault2DistributionCommitted) ContractEventName() string {
	return STSettlementVault2DistributionCommittedEventName
}

// UnpackDistributionCommittedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event DistributionCommitted(uint256 indexed noId, uint64 indexed seq, uint64 epoch, bytes32 root, uint256 total, uint64 leafCount, bytes32 ledgerHash, uint64 replacedSeq)
func (sTSettlementVault2 *STSettlementVault2) UnpackDistributionCommittedEvent(log *types.Log) (*STSettlementVault2DistributionCommitted, error) {
	event := "DistributionCommitted"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVault2DistributionCommitted)
	if len(log.Data) > 0 {
		if err := sTSettlementVault2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault2.abi.Events[event].Inputs {
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

// STSettlementVault2DistributionDeleted represents a DistributionDeleted event raised by the STSettlementVault2 contract.
type STSettlementVault2DistributionDeleted struct {
	NoId       *big.Int
	Seq        uint64
	RootIndex  *big.Int
	Index      *big.Int
	Fault      uint8
	Level      *big.Int
	Challenger common.Address
	Raw        *types.Log // Blockchain specific contextual infos
}

const STSettlementVault2DistributionDeletedEventName = "DistributionDeleted"

// ContractEventName returns the user-defined event name.
func (STSettlementVault2DistributionDeleted) ContractEventName() string {
	return STSettlementVault2DistributionDeletedEventName
}

// UnpackDistributionDeletedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event DistributionDeleted(uint256 indexed noId, uint64 indexed seq, uint256 rootIndex, uint256 index, uint8 fault, uint256 level, address challenger)
func (sTSettlementVault2 *STSettlementVault2) UnpackDistributionDeletedEvent(log *types.Log) (*STSettlementVault2DistributionDeleted, error) {
	event := "DistributionDeleted"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVault2DistributionDeleted)
	if len(log.Data) > 0 {
		if err := sTSettlementVault2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault2.abi.Events[event].Inputs {
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

// STSettlementVault2DistributionFinalized represents a DistributionFinalized event raised by the STSettlementVault2 contract.
type STSettlementVault2DistributionFinalized struct {
	NoId       *big.Int
	Seq        uint64
	RootIndex  *big.Int
	Epoch      uint64
	Root       [32]byte
	Total      *big.Int
	LeafCount  uint64
	LedgerHash [32]byte
	Raw        *types.Log // Blockchain specific contextual infos
}

const STSettlementVault2DistributionFinalizedEventName = "DistributionFinalized"

// ContractEventName returns the user-defined event name.
func (STSettlementVault2DistributionFinalized) ContractEventName() string {
	return STSettlementVault2DistributionFinalizedEventName
}

// UnpackDistributionFinalizedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event DistributionFinalized(uint256 indexed noId, uint64 indexed seq, uint256 rootIndex, uint64 epoch, bytes32 root, uint256 total, uint64 leafCount, bytes32 ledgerHash)
func (sTSettlementVault2 *STSettlementVault2) UnpackDistributionFinalizedEvent(log *types.Log) (*STSettlementVault2DistributionFinalized, error) {
	event := "DistributionFinalized"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVault2DistributionFinalized)
	if len(log.Data) > 0 {
		if err := sTSettlementVault2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault2.abi.Events[event].Inputs {
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

// STSettlementVault2EmissionCaptured represents a EmissionCaptured event raised by the STSettlementVault2 contract.
type STSettlementVault2EmissionCaptured struct {
	Epoch         *big.Int
	NoId          *big.Int
	Observed      *big.Int
	Amount        *big.Int
	FundedThrough *big.Int
	Raw           *types.Log // Blockchain specific contextual infos
}

const STSettlementVault2EmissionCapturedEventName = "EmissionCaptured"

// ContractEventName returns the user-defined event name.
func (STSettlementVault2EmissionCaptured) ContractEventName() string {
	return STSettlementVault2EmissionCapturedEventName
}

// UnpackEmissionCapturedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event EmissionCaptured(uint256 indexed epoch, uint256 indexed noId, uint256 observed, uint256 amount, uint256 fundedThrough)
func (sTSettlementVault2 *STSettlementVault2) UnpackEmissionCapturedEvent(log *types.Log) (*STSettlementVault2EmissionCaptured, error) {
	event := "EmissionCaptured"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVault2EmissionCaptured)
	if len(log.Data) > 0 {
		if err := sTSettlementVault2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault2.abi.Events[event].Inputs {
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

// STSettlementVault2EmissionDeferred represents a EmissionDeferred event raised by the STSettlementVault2 contract.
type STSettlementVault2EmissionDeferred struct {
	Epoch         *big.Int
	NoId          *big.Int
	FundedThrough *big.Int
	Raw           *types.Log // Blockchain specific contextual infos
}

const STSettlementVault2EmissionDeferredEventName = "EmissionDeferred"

// ContractEventName returns the user-defined event name.
func (STSettlementVault2EmissionDeferred) ContractEventName() string {
	return STSettlementVault2EmissionDeferredEventName
}

// UnpackEmissionDeferredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event EmissionDeferred(uint256 indexed epoch, uint256 indexed noId, uint256 fundedThrough)
func (sTSettlementVault2 *STSettlementVault2) UnpackEmissionDeferredEvent(log *types.Log) (*STSettlementVault2EmissionDeferred, error) {
	event := "EmissionDeferred"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVault2EmissionDeferred)
	if len(log.Data) > 0 {
		if err := sTSettlementVault2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault2.abi.Events[event].Inputs {
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

// STSettlementVault2EscrowRegistered represents a EscrowRegistered event raised by the STSettlementVault2 contract.
type STSettlementVault2EscrowRegistered struct {
	Hotkey [32]byte
	Uid    uint16
	Raw    *types.Log // Blockchain specific contextual infos
}

const STSettlementVault2EscrowRegisteredEventName = "EscrowRegistered"

// ContractEventName returns the user-defined event name.
func (STSettlementVault2EscrowRegistered) ContractEventName() string {
	return STSettlementVault2EscrowRegisteredEventName
}

// UnpackEscrowRegisteredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event EscrowRegistered(bytes32 indexed hotkey, uint16 uid)
func (sTSettlementVault2 *STSettlementVault2) UnpackEscrowRegisteredEvent(log *types.Log) (*STSettlementVault2EscrowRegistered, error) {
	event := "EscrowRegistered"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVault2EscrowRegistered)
	if len(log.Data) > 0 {
		if err := sTSettlementVault2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault2.abi.Events[event].Inputs {
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

// STSettlementVault2PathAnswered represents a PathAnswered event raised by the STSettlementVault2 contract.
type STSettlementVault2PathAnswered struct {
	NoId      *big.Int
	Seq       uint64
	RootIndex *big.Int
	Index     *big.Int
	Answerer  common.Address
	Raw       *types.Log // Blockchain specific contextual infos
}

const STSettlementVault2PathAnsweredEventName = "PathAnswered"

// ContractEventName returns the user-defined event name.
func (STSettlementVault2PathAnswered) ContractEventName() string {
	return STSettlementVault2PathAnsweredEventName
}

// UnpackPathAnsweredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event PathAnswered(uint256 indexed noId, uint64 indexed seq, uint256 rootIndex, uint256 index, address answerer)
func (sTSettlementVault2 *STSettlementVault2) UnpackPathAnsweredEvent(log *types.Log) (*STSettlementVault2PathAnswered, error) {
	event := "PathAnswered"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVault2PathAnswered)
	if len(log.Data) > 0 {
		if err := sTSettlementVault2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault2.abi.Events[event].Inputs {
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

// STSettlementVault2PathDemanded represents a PathDemanded event raised by the STSettlementVault2 contract.
type STSettlementVault2PathDemanded struct {
	NoId      *big.Int
	Seq       uint64
	RootIndex *big.Int
	Index     *big.Int
	Demander  common.Address
	Raw       *types.Log // Blockchain specific contextual infos
}

const STSettlementVault2PathDemandedEventName = "PathDemanded"

// ContractEventName returns the user-defined event name.
func (STSettlementVault2PathDemanded) ContractEventName() string {
	return STSettlementVault2PathDemandedEventName
}

// UnpackPathDemandedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event PathDemanded(uint256 indexed noId, uint64 indexed seq, uint256 rootIndex, uint256 index, address demander)
func (sTSettlementVault2 *STSettlementVault2) UnpackPathDemandedEvent(log *types.Log) (*STSettlementVault2PathDemanded, error) {
	event := "PathDemanded"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVault2PathDemanded)
	if len(log.Data) > 0 {
		if err := sTSettlementVault2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault2.abi.Events[event].Inputs {
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

// STSettlementVault2PoolRegistered represents a PoolRegistered event raised by the STSettlementVault2 contract.
type STSettlementVault2PoolRegistered struct {
	NoId       *big.Int
	Hotkey     [32]byte
	Uid        uint16
	FirstEpoch uint64
	Raw        *types.Log // Blockchain specific contextual infos
}

const STSettlementVault2PoolRegisteredEventName = "PoolRegistered"

// ContractEventName returns the user-defined event name.
func (STSettlementVault2PoolRegistered) ContractEventName() string {
	return STSettlementVault2PoolRegisteredEventName
}

// UnpackPoolRegisteredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event PoolRegistered(uint256 indexed noId, bytes32 indexed hotkey, uint16 uid, uint64 firstEpoch)
func (sTSettlementVault2 *STSettlementVault2) UnpackPoolRegisteredEvent(log *types.Log) (*STSettlementVault2PoolRegistered, error) {
	event := "PoolRegistered"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVault2PoolRegistered)
	if len(log.Data) > 0 {
		if err := sTSettlementVault2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault2.abi.Events[event].Inputs {
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

// STSettlementVault2PoolUidRegistered represents a PoolUidRegistered event raised by the STSettlementVault2 contract.
type STSettlementVault2PoolUidRegistered struct {
	NoId   *big.Int
	Hotkey [32]byte
	Uid    uint16
	Payer  common.Address
	Raw    *types.Log // Blockchain specific contextual infos
}

const STSettlementVault2PoolUidRegisteredEventName = "PoolUidRegistered"

// ContractEventName returns the user-defined event name.
func (STSettlementVault2PoolUidRegistered) ContractEventName() string {
	return STSettlementVault2PoolUidRegisteredEventName
}

// UnpackPoolUidRegisteredEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event PoolUidRegistered(uint256 indexed noId, bytes32 indexed hotkey, uint16 uid, address payer)
func (sTSettlementVault2 *STSettlementVault2) UnpackPoolUidRegisteredEvent(log *types.Log) (*STSettlementVault2PoolUidRegistered, error) {
	event := "PoolUidRegistered"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVault2PoolUidRegistered)
	if len(log.Data) > 0 {
		if err := sTSettlementVault2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault2.abi.Events[event].Inputs {
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

// STSettlementVault2StakeAbsorbed represents a StakeAbsorbed event raised by the STSettlementVault2 contract.
type STSettlementVault2StakeAbsorbed struct {
	Hotkey   [32]byte
	Observed *big.Int
	Amount   *big.Int
	Caller   common.Address
	Raw      *types.Log // Blockchain specific contextual infos
}

const STSettlementVault2StakeAbsorbedEventName = "StakeAbsorbed"

// ContractEventName returns the user-defined event name.
func (STSettlementVault2StakeAbsorbed) ContractEventName() string {
	return STSettlementVault2StakeAbsorbedEventName
}

// UnpackStakeAbsorbedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event StakeAbsorbed(bytes32 indexed hotkey, uint256 observed, uint256 amount, address caller)
func (sTSettlementVault2 *STSettlementVault2) UnpackStakeAbsorbedEvent(log *types.Log) (*STSettlementVault2StakeAbsorbed, error) {
	event := "StakeAbsorbed"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVault2StakeAbsorbed)
	if len(log.Data) > 0 {
		if err := sTSettlementVault2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault2.abi.Events[event].Inputs {
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

// STSettlementVault2SurplusAssigned represents a SurplusAssigned event raised by the STSettlementVault2 contract.
type STSettlementVault2SurplusAssigned struct {
	NoId   *big.Int
	Epoch  *big.Int
	Amount *big.Int
	Raw    *types.Log // Blockchain specific contextual infos
}

const STSettlementVault2SurplusAssignedEventName = "SurplusAssigned"

// ContractEventName returns the user-defined event name.
func (STSettlementVault2SurplusAssigned) ContractEventName() string {
	return STSettlementVault2SurplusAssignedEventName
}

// UnpackSurplusAssignedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event SurplusAssigned(uint256 indexed noId, uint256 indexed epoch, uint256 amount)
func (sTSettlementVault2 *STSettlementVault2) UnpackSurplusAssignedEvent(log *types.Log) (*STSettlementVault2SurplusAssigned, error) {
	event := "SurplusAssigned"
	if len(log.Topics) == 0 || log.Topics[0] != sTSettlementVault2.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STSettlementVault2SurplusAssigned)
	if len(log.Data) > 0 {
		if err := sTSettlementVault2.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTSettlementVault2.abi.Events[event].Inputs {
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
func (sTSettlementVault2 *STSettlementVault2) UnpackError(raw []byte) (any, error) {
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["AlreadyDemanded"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackAlreadyDemandedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["AlreadyInitialized"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackAlreadyInitializedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["AmountTooLarge"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackAmountTooLargeError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["ChallengeWindowClosed"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackChallengeWindowClosedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["ChallengeWindowOpen"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackChallengeWindowOpenError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["CommitTooSoon"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackCommitTooSoonError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["DemandOpen"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackDemandOpenError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["EpochOutOfRange"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackEpochOutOfRangeError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["IndexOutOfRange"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackIndexOutOfRangeError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["InvalidColdkey"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackInvalidColdkeyError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["InvalidConfiguration"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackInvalidConfigurationError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["InvalidDemandFee"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackInvalidDemandFeeError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["InvalidHotkey"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackInvalidHotkeyError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["InvalidLeafCount"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackInvalidLeafCountError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["InvalidProof"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackInvalidProofError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["InvalidRoot"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackInvalidRootError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["InvalidSurplus"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackInvalidSurplusError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["InvalidTransition"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackInvalidTransitionError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["MalformedPath"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackMalformedPathError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["NativeBalanceDecreased"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackNativeBalanceDecreasedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["NativeRefundFailed"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackNativeRefundFailedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["NoFault"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackNoFaultError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["NoFinalizedRoot"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackNoFinalizedRootError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["NoPendingRoot"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackNoPendingRootError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["NothingToAbsorb"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackNothingToAbsorbError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["NothingToClaim"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackNothingToClaimError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["PoolCapExceeded"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackPoolCapExceededError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["Reentrancy"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackReentrancyError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["RuntimeAccountingMismatch"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackRuntimeAccountingMismatchError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["SafeCastOverflowedUintDowncast"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackSafeCastOverflowedUintDowncastError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["StaleRootCount"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackStaleRootCountError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["TotalMismatch"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackTotalMismatchError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["TotalNotIncreased"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackTotalNotIncreasedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["UidExists"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackUidExistsError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["Unauthorized"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackUnauthorizedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["Underfunded"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackUnderfundedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["UnknownPool"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackUnknownPoolError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTSettlementVault2.abi.Errors["UnknownRoot"].ID.Bytes()[:4]) {
		return sTSettlementVault2.UnpackUnknownRootError(raw[4:])
	}
	return nil, errors.New("Unknown error")
}

// STSettlementVault2AlreadyDemanded represents a AlreadyDemanded error raised by the STSettlementVault2 contract.
type STSettlementVault2AlreadyDemanded struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error AlreadyDemanded()
func STSettlementVault2AlreadyDemandedErrorID() common.Hash {
	return common.HexToHash("0xafb6e2bbe01e8b74296c9231cdd4d241595616d39dd3b9d8acc050ea4db50d55")
}

// UnpackAlreadyDemandedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error AlreadyDemanded()
func (sTSettlementVault2 *STSettlementVault2) UnpackAlreadyDemandedError(raw []byte) (*STSettlementVault2AlreadyDemanded, error) {
	out := new(STSettlementVault2AlreadyDemanded)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "AlreadyDemanded", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2AlreadyInitialized represents a AlreadyInitialized error raised by the STSettlementVault2 contract.
type STSettlementVault2AlreadyInitialized struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error AlreadyInitialized()
func STSettlementVault2AlreadyInitializedErrorID() common.Hash {
	return common.HexToHash("0x0dc149f07762891dbcea3fe72770f3d63a1863fc54b2f084e8c59ec476996927")
}

// UnpackAlreadyInitializedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error AlreadyInitialized()
func (sTSettlementVault2 *STSettlementVault2) UnpackAlreadyInitializedError(raw []byte) (*STSettlementVault2AlreadyInitialized, error) {
	out := new(STSettlementVault2AlreadyInitialized)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "AlreadyInitialized", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2AmountTooLarge represents a AmountTooLarge error raised by the STSettlementVault2 contract.
type STSettlementVault2AmountTooLarge struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error AmountTooLarge()
func STSettlementVault2AmountTooLargeErrorID() common.Hash {
	return common.HexToHash("0x06250401c072295744520da9e60a3a29ab28ceb9fda5d792222d5242d22e8a44")
}

// UnpackAmountTooLargeError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error AmountTooLarge()
func (sTSettlementVault2 *STSettlementVault2) UnpackAmountTooLargeError(raw []byte) (*STSettlementVault2AmountTooLarge, error) {
	out := new(STSettlementVault2AmountTooLarge)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "AmountTooLarge", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2ChallengeWindowClosed represents a ChallengeWindowClosed error raised by the STSettlementVault2 contract.
type STSettlementVault2ChallengeWindowClosed struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error ChallengeWindowClosed()
func STSettlementVault2ChallengeWindowClosedErrorID() common.Hash {
	return common.HexToHash("0x2b11bdc298ffc265fcf6baa2e27256df05526a009d15c00e61d30aad7e13d5cd")
}

// UnpackChallengeWindowClosedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error ChallengeWindowClosed()
func (sTSettlementVault2 *STSettlementVault2) UnpackChallengeWindowClosedError(raw []byte) (*STSettlementVault2ChallengeWindowClosed, error) {
	out := new(STSettlementVault2ChallengeWindowClosed)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "ChallengeWindowClosed", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2ChallengeWindowOpen represents a ChallengeWindowOpen error raised by the STSettlementVault2 contract.
type STSettlementVault2ChallengeWindowOpen struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error ChallengeWindowOpen()
func STSettlementVault2ChallengeWindowOpenErrorID() common.Hash {
	return common.HexToHash("0xfa7bc547730fcff559fa41be1123fbc1feef32f4eae67cfe18dc77b296d7b92f")
}

// UnpackChallengeWindowOpenError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error ChallengeWindowOpen()
func (sTSettlementVault2 *STSettlementVault2) UnpackChallengeWindowOpenError(raw []byte) (*STSettlementVault2ChallengeWindowOpen, error) {
	out := new(STSettlementVault2ChallengeWindowOpen)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "ChallengeWindowOpen", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2CommitTooSoon represents a CommitTooSoon error raised by the STSettlementVault2 contract.
type STSettlementVault2CommitTooSoon struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error CommitTooSoon()
func STSettlementVault2CommitTooSoonErrorID() common.Hash {
	return common.HexToHash("0x00aad9e6e66937a904ab73c12a8bc529bb57e5e3c0f65f6730ad53cb7f64eb18")
}

// UnpackCommitTooSoonError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error CommitTooSoon()
func (sTSettlementVault2 *STSettlementVault2) UnpackCommitTooSoonError(raw []byte) (*STSettlementVault2CommitTooSoon, error) {
	out := new(STSettlementVault2CommitTooSoon)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "CommitTooSoon", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2DemandOpen represents a DemandOpen error raised by the STSettlementVault2 contract.
type STSettlementVault2DemandOpen struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error DemandOpen()
func STSettlementVault2DemandOpenErrorID() common.Hash {
	return common.HexToHash("0xa990b857dd01686a954dee1153baa4bd3f51c1c71a1eaea2d52008090c41fd52")
}

// UnpackDemandOpenError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error DemandOpen()
func (sTSettlementVault2 *STSettlementVault2) UnpackDemandOpenError(raw []byte) (*STSettlementVault2DemandOpen, error) {
	out := new(STSettlementVault2DemandOpen)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "DemandOpen", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2EpochOutOfRange represents a EpochOutOfRange error raised by the STSettlementVault2 contract.
type STSettlementVault2EpochOutOfRange struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error EpochOutOfRange()
func STSettlementVault2EpochOutOfRangeErrorID() common.Hash {
	return common.HexToHash("0x16f4c85307c6cc0c1c44fc91d5b04f21f83ef5cf88f24a446651b6d1116d28c9")
}

// UnpackEpochOutOfRangeError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error EpochOutOfRange()
func (sTSettlementVault2 *STSettlementVault2) UnpackEpochOutOfRangeError(raw []byte) (*STSettlementVault2EpochOutOfRange, error) {
	out := new(STSettlementVault2EpochOutOfRange)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "EpochOutOfRange", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2IndexOutOfRange represents a IndexOutOfRange error raised by the STSettlementVault2 contract.
type STSettlementVault2IndexOutOfRange struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error IndexOutOfRange()
func STSettlementVault2IndexOutOfRangeErrorID() common.Hash {
	return common.HexToHash("0x1390f2a1dd33cddc8f706e7eab83b4fd617571b61798f54adfc3b21852444e79")
}

// UnpackIndexOutOfRangeError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error IndexOutOfRange()
func (sTSettlementVault2 *STSettlementVault2) UnpackIndexOutOfRangeError(raw []byte) (*STSettlementVault2IndexOutOfRange, error) {
	out := new(STSettlementVault2IndexOutOfRange)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "IndexOutOfRange", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2InvalidColdkey represents a InvalidColdkey error raised by the STSettlementVault2 contract.
type STSettlementVault2InvalidColdkey struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidColdkey()
func STSettlementVault2InvalidColdkeyErrorID() common.Hash {
	return common.HexToHash("0x79b7ecc1c48748603d7eab4f6fc7f6a4b2892b996a594fdd7b47433669b50d03")
}

// UnpackInvalidColdkeyError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidColdkey()
func (sTSettlementVault2 *STSettlementVault2) UnpackInvalidColdkeyError(raw []byte) (*STSettlementVault2InvalidColdkey, error) {
	out := new(STSettlementVault2InvalidColdkey)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "InvalidColdkey", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2InvalidConfiguration represents a InvalidConfiguration error raised by the STSettlementVault2 contract.
type STSettlementVault2InvalidConfiguration struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidConfiguration()
func STSettlementVault2InvalidConfigurationErrorID() common.Hash {
	return common.HexToHash("0xc52a9bd3d9e475b9056a93172ef6968d775a7cd41c4255bbebf12e90a5fbbd39")
}

// UnpackInvalidConfigurationError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidConfiguration()
func (sTSettlementVault2 *STSettlementVault2) UnpackInvalidConfigurationError(raw []byte) (*STSettlementVault2InvalidConfiguration, error) {
	out := new(STSettlementVault2InvalidConfiguration)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "InvalidConfiguration", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2InvalidDemandFee represents a InvalidDemandFee error raised by the STSettlementVault2 contract.
type STSettlementVault2InvalidDemandFee struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidDemandFee()
func STSettlementVault2InvalidDemandFeeErrorID() common.Hash {
	return common.HexToHash("0x6f6fe55e9353e275d76a54fa21ab40a979a412fad847d735df65f08b68f6911f")
}

// UnpackInvalidDemandFeeError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidDemandFee()
func (sTSettlementVault2 *STSettlementVault2) UnpackInvalidDemandFeeError(raw []byte) (*STSettlementVault2InvalidDemandFee, error) {
	out := new(STSettlementVault2InvalidDemandFee)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "InvalidDemandFee", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2InvalidHotkey represents a InvalidHotkey error raised by the STSettlementVault2 contract.
type STSettlementVault2InvalidHotkey struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidHotkey()
func STSettlementVault2InvalidHotkeyErrorID() common.Hash {
	return common.HexToHash("0x582a9119fd32563f9446f4c24275f3ad57e923e36310d19683543ded834545cd")
}

// UnpackInvalidHotkeyError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidHotkey()
func (sTSettlementVault2 *STSettlementVault2) UnpackInvalidHotkeyError(raw []byte) (*STSettlementVault2InvalidHotkey, error) {
	out := new(STSettlementVault2InvalidHotkey)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "InvalidHotkey", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2InvalidLeafCount represents a InvalidLeafCount error raised by the STSettlementVault2 contract.
type STSettlementVault2InvalidLeafCount struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidLeafCount()
func STSettlementVault2InvalidLeafCountErrorID() common.Hash {
	return common.HexToHash("0x3438704d2326a7c9b287e91aeb0ec7d2483baea8ed58e96d8f42cbb4eec226c0")
}

// UnpackInvalidLeafCountError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidLeafCount()
func (sTSettlementVault2 *STSettlementVault2) UnpackInvalidLeafCountError(raw []byte) (*STSettlementVault2InvalidLeafCount, error) {
	out := new(STSettlementVault2InvalidLeafCount)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "InvalidLeafCount", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2InvalidProof represents a InvalidProof error raised by the STSettlementVault2 contract.
type STSettlementVault2InvalidProof struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidProof()
func STSettlementVault2InvalidProofErrorID() common.Hash {
	return common.HexToHash("0x09bde339c6b182be216ee7ef8ccff6338c6ef7993445216112ae575c5438fd27")
}

// UnpackInvalidProofError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidProof()
func (sTSettlementVault2 *STSettlementVault2) UnpackInvalidProofError(raw []byte) (*STSettlementVault2InvalidProof, error) {
	out := new(STSettlementVault2InvalidProof)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "InvalidProof", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2InvalidRoot represents a InvalidRoot error raised by the STSettlementVault2 contract.
type STSettlementVault2InvalidRoot struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidRoot()
func STSettlementVault2InvalidRootErrorID() common.Hash {
	return common.HexToHash("0x504570e3dbb7b86bfb4ee628f3b5ae306dd12bad6ed9b4dc6a090b78236f968b")
}

// UnpackInvalidRootError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidRoot()
func (sTSettlementVault2 *STSettlementVault2) UnpackInvalidRootError(raw []byte) (*STSettlementVault2InvalidRoot, error) {
	out := new(STSettlementVault2InvalidRoot)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "InvalidRoot", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2InvalidSurplus represents a InvalidSurplus error raised by the STSettlementVault2 contract.
type STSettlementVault2InvalidSurplus struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidSurplus()
func STSettlementVault2InvalidSurplusErrorID() common.Hash {
	return common.HexToHash("0xaf7269dc56bba6517fdf6194b56db5446a4445460421655ea8a9cabfe34e7d60")
}

// UnpackInvalidSurplusError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidSurplus()
func (sTSettlementVault2 *STSettlementVault2) UnpackInvalidSurplusError(raw []byte) (*STSettlementVault2InvalidSurplus, error) {
	out := new(STSettlementVault2InvalidSurplus)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "InvalidSurplus", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2InvalidTransition represents a InvalidTransition error raised by the STSettlementVault2 contract.
type STSettlementVault2InvalidTransition struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidTransition()
func STSettlementVault2InvalidTransitionErrorID() common.Hash {
	return common.HexToHash("0xa6532e5d22b2d9016a3c919977e18f1cf795d3c7af3b6848d32d8b9403d143e1")
}

// UnpackInvalidTransitionError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidTransition()
func (sTSettlementVault2 *STSettlementVault2) UnpackInvalidTransitionError(raw []byte) (*STSettlementVault2InvalidTransition, error) {
	out := new(STSettlementVault2InvalidTransition)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "InvalidTransition", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2MalformedPath represents a MalformedPath error raised by the STSettlementVault2 contract.
type STSettlementVault2MalformedPath struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error MalformedPath()
func STSettlementVault2MalformedPathErrorID() common.Hash {
	return common.HexToHash("0xd6c3bc107eef83b8b04413a351dfefde20c3fb502fa06bf319c327cc232ad475")
}

// UnpackMalformedPathError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error MalformedPath()
func (sTSettlementVault2 *STSettlementVault2) UnpackMalformedPathError(raw []byte) (*STSettlementVault2MalformedPath, error) {
	out := new(STSettlementVault2MalformedPath)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "MalformedPath", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2NativeBalanceDecreased represents a NativeBalanceDecreased error raised by the STSettlementVault2 contract.
type STSettlementVault2NativeBalanceDecreased struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NativeBalanceDecreased()
func STSettlementVault2NativeBalanceDecreasedErrorID() common.Hash {
	return common.HexToHash("0x0ee99a8030e708d2f8de4b361da1ea80d1b1bcf529193186800a3483e4dd1f97")
}

// UnpackNativeBalanceDecreasedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NativeBalanceDecreased()
func (sTSettlementVault2 *STSettlementVault2) UnpackNativeBalanceDecreasedError(raw []byte) (*STSettlementVault2NativeBalanceDecreased, error) {
	out := new(STSettlementVault2NativeBalanceDecreased)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "NativeBalanceDecreased", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2NativeRefundFailed represents a NativeRefundFailed error raised by the STSettlementVault2 contract.
type STSettlementVault2NativeRefundFailed struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NativeRefundFailed()
func STSettlementVault2NativeRefundFailedErrorID() common.Hash {
	return common.HexToHash("0x8520d710691d90175ff6d0cde74ada1f9d91ac205406750a2381430c693c006e")
}

// UnpackNativeRefundFailedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NativeRefundFailed()
func (sTSettlementVault2 *STSettlementVault2) UnpackNativeRefundFailedError(raw []byte) (*STSettlementVault2NativeRefundFailed, error) {
	out := new(STSettlementVault2NativeRefundFailed)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "NativeRefundFailed", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2NoFault represents a NoFault error raised by the STSettlementVault2 contract.
type STSettlementVault2NoFault struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NoFault()
func STSettlementVault2NoFaultErrorID() common.Hash {
	return common.HexToHash("0x6ccd0bbcc8f98637dc4acb8f31da7db62b1840e328048e46c265d4b47064c98b")
}

// UnpackNoFaultError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NoFault()
func (sTSettlementVault2 *STSettlementVault2) UnpackNoFaultError(raw []byte) (*STSettlementVault2NoFault, error) {
	out := new(STSettlementVault2NoFault)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "NoFault", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2NoFinalizedRoot represents a NoFinalizedRoot error raised by the STSettlementVault2 contract.
type STSettlementVault2NoFinalizedRoot struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NoFinalizedRoot()
func STSettlementVault2NoFinalizedRootErrorID() common.Hash {
	return common.HexToHash("0xacfcb26ab43249100352bb197ea78f23530ddb3e96f8be28384df52f635e7ab0")
}

// UnpackNoFinalizedRootError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NoFinalizedRoot()
func (sTSettlementVault2 *STSettlementVault2) UnpackNoFinalizedRootError(raw []byte) (*STSettlementVault2NoFinalizedRoot, error) {
	out := new(STSettlementVault2NoFinalizedRoot)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "NoFinalizedRoot", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2NoPendingRoot represents a NoPendingRoot error raised by the STSettlementVault2 contract.
type STSettlementVault2NoPendingRoot struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NoPendingRoot()
func STSettlementVault2NoPendingRootErrorID() common.Hash {
	return common.HexToHash("0x2a01895ee863735ea8533479c7609527402e57aaebaef575f9a1b9cb21c4beee")
}

// UnpackNoPendingRootError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NoPendingRoot()
func (sTSettlementVault2 *STSettlementVault2) UnpackNoPendingRootError(raw []byte) (*STSettlementVault2NoPendingRoot, error) {
	out := new(STSettlementVault2NoPendingRoot)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "NoPendingRoot", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2NothingToAbsorb represents a NothingToAbsorb error raised by the STSettlementVault2 contract.
type STSettlementVault2NothingToAbsorb struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NothingToAbsorb()
func STSettlementVault2NothingToAbsorbErrorID() common.Hash {
	return common.HexToHash("0x1a0632fe4e5e904d7a5732e86555d9aef90baedb1e6b3a8aafbc27969d8b902d")
}

// UnpackNothingToAbsorbError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NothingToAbsorb()
func (sTSettlementVault2 *STSettlementVault2) UnpackNothingToAbsorbError(raw []byte) (*STSettlementVault2NothingToAbsorb, error) {
	out := new(STSettlementVault2NothingToAbsorb)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "NothingToAbsorb", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2NothingToClaim represents a NothingToClaim error raised by the STSettlementVault2 contract.
type STSettlementVault2NothingToClaim struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error NothingToClaim()
func STSettlementVault2NothingToClaimErrorID() common.Hash {
	return common.HexToHash("0x969bf7288f744225521096ebffff657851af77dd4c63700d76b8d1cf6481d1f1")
}

// UnpackNothingToClaimError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error NothingToClaim()
func (sTSettlementVault2 *STSettlementVault2) UnpackNothingToClaimError(raw []byte) (*STSettlementVault2NothingToClaim, error) {
	out := new(STSettlementVault2NothingToClaim)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "NothingToClaim", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2PoolCapExceeded represents a PoolCapExceeded error raised by the STSettlementVault2 contract.
type STSettlementVault2PoolCapExceeded struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error PoolCapExceeded()
func STSettlementVault2PoolCapExceededErrorID() common.Hash {
	return common.HexToHash("0x14ef863c0b0df07b35d02e4517457575888cab7129f601aa7fb97caf2f405b85")
}

// UnpackPoolCapExceededError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error PoolCapExceeded()
func (sTSettlementVault2 *STSettlementVault2) UnpackPoolCapExceededError(raw []byte) (*STSettlementVault2PoolCapExceeded, error) {
	out := new(STSettlementVault2PoolCapExceeded)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "PoolCapExceeded", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2Reentrancy represents a Reentrancy error raised by the STSettlementVault2 contract.
type STSettlementVault2Reentrancy struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error Reentrancy()
func STSettlementVault2ReentrancyErrorID() common.Hash {
	return common.HexToHash("0xab143c06c9772d69bbbc9f2fe74acd02f810e93b099f3d1dac8448ac9ae35991")
}

// UnpackReentrancyError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error Reentrancy()
func (sTSettlementVault2 *STSettlementVault2) UnpackReentrancyError(raw []byte) (*STSettlementVault2Reentrancy, error) {
	out := new(STSettlementVault2Reentrancy)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "Reentrancy", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2RuntimeAccountingMismatch represents a RuntimeAccountingMismatch error raised by the STSettlementVault2 contract.
type STSettlementVault2RuntimeAccountingMismatch struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error RuntimeAccountingMismatch()
func STSettlementVault2RuntimeAccountingMismatchErrorID() common.Hash {
	return common.HexToHash("0x2bd1ed7d24b1da053721d64e4a287a7ad1b37138c31a5847af6e59aa54f85b99")
}

// UnpackRuntimeAccountingMismatchError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error RuntimeAccountingMismatch()
func (sTSettlementVault2 *STSettlementVault2) UnpackRuntimeAccountingMismatchError(raw []byte) (*STSettlementVault2RuntimeAccountingMismatch, error) {
	out := new(STSettlementVault2RuntimeAccountingMismatch)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "RuntimeAccountingMismatch", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2SafeCastOverflowedUintDowncast represents a SafeCastOverflowedUintDowncast error raised by the STSettlementVault2 contract.
type STSettlementVault2SafeCastOverflowedUintDowncast struct {
	Bits  uint8
	Value *big.Int
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error SafeCastOverflowedUintDowncast(uint8 bits, uint256 value)
func STSettlementVault2SafeCastOverflowedUintDowncastErrorID() common.Hash {
	return common.HexToHash("0x6dfcc6503a32754ce7a89698e18201fc5294fd4aad43edefee786f88423b1a12")
}

// UnpackSafeCastOverflowedUintDowncastError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error SafeCastOverflowedUintDowncast(uint8 bits, uint256 value)
func (sTSettlementVault2 *STSettlementVault2) UnpackSafeCastOverflowedUintDowncastError(raw []byte) (*STSettlementVault2SafeCastOverflowedUintDowncast, error) {
	out := new(STSettlementVault2SafeCastOverflowedUintDowncast)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "SafeCastOverflowedUintDowncast", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2StaleRootCount represents a StaleRootCount error raised by the STSettlementVault2 contract.
type STSettlementVault2StaleRootCount struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error StaleRootCount()
func STSettlementVault2StaleRootCountErrorID() common.Hash {
	return common.HexToHash("0x885ca95d920d641bebb5aaeccf503dd5d929337ae304935616a352fd2031a567")
}

// UnpackStaleRootCountError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error StaleRootCount()
func (sTSettlementVault2 *STSettlementVault2) UnpackStaleRootCountError(raw []byte) (*STSettlementVault2StaleRootCount, error) {
	out := new(STSettlementVault2StaleRootCount)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "StaleRootCount", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2TotalMismatch represents a TotalMismatch error raised by the STSettlementVault2 contract.
type STSettlementVault2TotalMismatch struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error TotalMismatch()
func STSettlementVault2TotalMismatchErrorID() common.Hash {
	return common.HexToHash("0x8297ae7f81adbead2415b2888676344fd81d6103c3dc4a7a94c78535a0aab2c5")
}

// UnpackTotalMismatchError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error TotalMismatch()
func (sTSettlementVault2 *STSettlementVault2) UnpackTotalMismatchError(raw []byte) (*STSettlementVault2TotalMismatch, error) {
	out := new(STSettlementVault2TotalMismatch)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "TotalMismatch", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2TotalNotIncreased represents a TotalNotIncreased error raised by the STSettlementVault2 contract.
type STSettlementVault2TotalNotIncreased struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error TotalNotIncreased()
func STSettlementVault2TotalNotIncreasedErrorID() common.Hash {
	return common.HexToHash("0xe73718d257151f6420e8c6489060c4b654b28344f8568a106021d714d8ff1c54")
}

// UnpackTotalNotIncreasedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error TotalNotIncreased()
func (sTSettlementVault2 *STSettlementVault2) UnpackTotalNotIncreasedError(raw []byte) (*STSettlementVault2TotalNotIncreased, error) {
	out := new(STSettlementVault2TotalNotIncreased)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "TotalNotIncreased", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2UidExists represents a UidExists error raised by the STSettlementVault2 contract.
type STSettlementVault2UidExists struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error UidExists()
func STSettlementVault2UidExistsErrorID() common.Hash {
	return common.HexToHash("0x6d29187d528b4e31d9f135bf4192fac99b0f043b07c3502025d4dbebd3ed7710")
}

// UnpackUidExistsError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error UidExists()
func (sTSettlementVault2 *STSettlementVault2) UnpackUidExistsError(raw []byte) (*STSettlementVault2UidExists, error) {
	out := new(STSettlementVault2UidExists)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "UidExists", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2Unauthorized represents a Unauthorized error raised by the STSettlementVault2 contract.
type STSettlementVault2Unauthorized struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error Unauthorized()
func STSettlementVault2UnauthorizedErrorID() common.Hash {
	return common.HexToHash("0x82b4290015f7ec7256ca2a6247d3c2a89c4865c0e791456df195f40ad0a81367")
}

// UnpackUnauthorizedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error Unauthorized()
func (sTSettlementVault2 *STSettlementVault2) UnpackUnauthorizedError(raw []byte) (*STSettlementVault2Unauthorized, error) {
	out := new(STSettlementVault2Unauthorized)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "Unauthorized", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2Underfunded represents a Underfunded error raised by the STSettlementVault2 contract.
type STSettlementVault2Underfunded struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error Underfunded()
func STSettlementVault2UnderfundedErrorID() common.Hash {
	return common.HexToHash("0xe1a8ca9e5b1fc9283d6e2bdee616fe535e0d88cf3046c6fbfab03e86eaa0167a")
}

// UnpackUnderfundedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error Underfunded()
func (sTSettlementVault2 *STSettlementVault2) UnpackUnderfundedError(raw []byte) (*STSettlementVault2Underfunded, error) {
	out := new(STSettlementVault2Underfunded)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "Underfunded", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2UnknownPool represents a UnknownPool error raised by the STSettlementVault2 contract.
type STSettlementVault2UnknownPool struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error UnknownPool()
func STSettlementVault2UnknownPoolErrorID() common.Hash {
	return common.HexToHash("0xf7139e330b4806569379fe159fc41bed9c88b667fc4b9b3c7047d03f1a25197a")
}

// UnpackUnknownPoolError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error UnknownPool()
func (sTSettlementVault2 *STSettlementVault2) UnpackUnknownPoolError(raw []byte) (*STSettlementVault2UnknownPool, error) {
	out := new(STSettlementVault2UnknownPool)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "UnknownPool", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STSettlementVault2UnknownRoot represents a UnknownRoot error raised by the STSettlementVault2 contract.
type STSettlementVault2UnknownRoot struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error UnknownRoot()
func STSettlementVault2UnknownRootErrorID() common.Hash {
	return common.HexToHash("0x8c52011601d6d0155bd76830d0a6648bd4d7f99aabdc01dc4bf2b363f2cb0fd7")
}

// UnpackUnknownRootError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error UnknownRoot()
func (sTSettlementVault2 *STSettlementVault2) UnpackUnknownRootError(raw []byte) (*STSettlementVault2UnknownRoot, error) {
	out := new(STSettlementVault2UnknownRoot)
	if err := sTSettlementVault2.abi.UnpackIntoInterface(out, "UnknownRoot", raw); err != nil {
		return nil, err
	}
	return out, nil
}
