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

// STValidatorEvidenceActivation is an auto generated low-level Go binding around an user-defined struct.
type STValidatorEvidenceActivation struct {
	Record         ValidatorEvidenceActivationRecord
	PublishedBlock uint64
}

// STValidatorEvidenceCommitment is an auto generated low-level Go binding around an user-defined struct.
type STValidatorEvidenceCommitment struct {
	Header         ValidatorEvidenceHeader
	PublishedBlock uint64
}

// ValidatorEvidenceActivationDomain is an auto generated low-level Go binding around an user-defined struct.
type ValidatorEvidenceActivationDomain struct {
	ChainId          uint64
	GenesisHash      [32]byte
	Netuid           uint16
	Coordinator      common.Address
	SettlementVault  common.Address
	DeploymentIdHash [32]byte
	PolicyHash       [32]byte
	Epoch            uint64
}

// ValidatorEvidenceActivationRecord is an auto generated low-level Go binding around an user-defined struct.
type ValidatorEvidenceActivationRecord struct {
	Domain        ValidatorEvidenceActivationDomain
	Hotkey        [32]byte
	NoId          uint64
	Vpk           [32]byte
	FirstSequence uint64
	PriorRoot     [32]byte
	NativeBlock   uint64
	NativeHash    [32]byte
	EvmBlock      uint64
	EvmHash       [32]byte
}

// ValidatorEvidenceDomain is an auto generated low-level Go binding around an user-defined struct.
type ValidatorEvidenceDomain struct {
	ChainId          uint64
	GenesisHash      [32]byte
	Netuid           uint16
	Coordinator      common.Address
	SettlementVault  common.Address
	DeploymentIdHash [32]byte
	PolicyHash       [32]byte
	ActivationEpoch  uint64
	ActivationHash   [32]byte
}

// ValidatorEvidenceHeader is an auto generated low-level Go binding around an user-defined struct.
type ValidatorEvidenceHeader struct {
	Domain        ValidatorEvidenceDomain
	Hotkey        [32]byte
	NoId          uint64
	Epoch         uint64
	Kind          uint8
	Subject       ValidatorEvidenceSubject
	Vpk           [32]byte
	BoundaryBlock uint64
	BoundaryHash  [32]byte
	CensusHash    [32]byte
	PayloadHash   [32]byte
	PayloadBytes  uint64
}

// ValidatorEvidenceSubject is an auto generated low-level Go binding around an user-defined struct.
type ValidatorEvidenceSubject struct {
	ObservationEpoch uint64
	NativeEpoch      uint64
}

// STValidatorEvidenceMetaData contains all meta data concerning the STValidatorEvidence contract.
var STValidatorEvidenceMetaData = bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[{\"name\":\"coordinator_\",\"type\":\"address\",\"internalType\":\"contractSTCoordinator\"},{\"name\":\"genesisHash_\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"deploymentIdHash_\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"activation\",\"inputs\":[{\"name\":\"digest\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple\",\"internalType\":\"structSTValidatorEvidence.Activation\",\"components\":[{\"name\":\"record\",\"type\":\"tuple\",\"internalType\":\"structValidatorEvidenceActivation.Record\",\"components\":[{\"name\":\"domain\",\"type\":\"tuple\",\"internalType\":\"structValidatorEvidenceActivation.Domain\",\"components\":[{\"name\":\"chainId\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"genesisHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"netuid\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"coordinator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"settlementVault\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"deploymentIdHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"policyHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"epoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"}]},{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"noId\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"vpk\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"firstSequence\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"priorRoot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"nativeBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"nativeHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"evmBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"evmHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}]},{\"name\":\"publishedBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"chainId\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"commitEvidence\",\"inputs\":[{\"name\":\"header\",\"type\":\"tuple\",\"internalType\":\"structValidatorEvidence.Header\",\"components\":[{\"name\":\"domain\",\"type\":\"tuple\",\"internalType\":\"structValidatorEvidence.Domain\",\"components\":[{\"name\":\"chainId\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"genesisHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"netuid\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"coordinator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"settlementVault\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"deploymentIdHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"policyHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"activationEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"activationHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}]},{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"noId\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"epoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"kind\",\"type\":\"uint8\",\"internalType\":\"uint8\"},{\"name\":\"subject\",\"type\":\"tuple\",\"internalType\":\"structValidatorEvidence.Subject\",\"components\":[{\"name\":\"observationEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"nativeEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"}]},{\"name\":\"vpk\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"boundaryBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"boundaryHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"censusHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"payloadHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"payloadBytes\",\"type\":\"uint64\",\"internalType\":\"uint64\"}]},{\"name\":\"vpkSignature\",\"type\":\"bytes\",\"internalType\":\"bytes\"},{\"name\":\"hotkeySignature\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[{\"name\":\"slot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"commitment\",\"inputs\":[{\"name\":\"slot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple\",\"internalType\":\"structSTValidatorEvidence.Commitment\",\"components\":[{\"name\":\"header\",\"type\":\"tuple\",\"internalType\":\"structValidatorEvidence.Header\",\"components\":[{\"name\":\"domain\",\"type\":\"tuple\",\"internalType\":\"structValidatorEvidence.Domain\",\"components\":[{\"name\":\"chainId\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"genesisHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"netuid\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"coordinator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"settlementVault\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"deploymentIdHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"policyHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"activationEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"activationHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}]},{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"noId\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"epoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"kind\",\"type\":\"uint8\",\"internalType\":\"uint8\"},{\"name\":\"subject\",\"type\":\"tuple\",\"internalType\":\"structValidatorEvidence.Subject\",\"components\":[{\"name\":\"observationEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"nativeEpoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"}]},{\"name\":\"vpk\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"boundaryBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"boundaryHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"censusHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"payloadHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"payloadBytes\",\"type\":\"uint64\",\"internalType\":\"uint64\"}]},{\"name\":\"publishedBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"coordinator\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"contractSTCoordinator\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"deploymentIdHash\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"genesisHash\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"netuid\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint16\",\"internalType\":\"uint16\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"publishActivation\",\"inputs\":[{\"name\":\"record\",\"type\":\"tuple\",\"internalType\":\"structValidatorEvidenceActivation.Record\",\"components\":[{\"name\":\"domain\",\"type\":\"tuple\",\"internalType\":\"structValidatorEvidenceActivation.Domain\",\"components\":[{\"name\":\"chainId\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"genesisHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"netuid\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"coordinator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"settlementVault\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"deploymentIdHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"policyHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"epoch\",\"type\":\"uint64\",\"internalType\":\"uint64\"}]},{\"name\":\"hotkey\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"noId\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"vpk\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"firstSequence\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"priorRoot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"nativeBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"nativeHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"evmBlock\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"evmHash\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}]},{\"name\":\"vpkSignature\",\"type\":\"bytes\",\"internalType\":\"bytes\"},{\"name\":\"hotkeySignature\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[{\"name\":\"digest\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"settlementVault\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"event\",\"name\":\"ActivationPublished\",\"inputs\":[{\"name\":\"activationHash\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"hotkey\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"noId\",\"type\":\"uint64\",\"indexed\":true,\"internalType\":\"uint64\"},{\"name\":\"epoch\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EvidenceCommitted\",\"inputs\":[{\"name\":\"slot\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"noId\",\"type\":\"uint64\",\"indexed\":true,\"internalType\":\"uint64\"},{\"name\":\"epoch\",\"type\":\"uint64\",\"indexed\":true,\"internalType\":\"uint64\"},{\"name\":\"headerHash\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"payloadHash\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"censusHash\",\"type\":\"bytes32\",\"indexed\":false,\"internalType\":\"bytes32\"},{\"name\":\"payloadBytes\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"AlreadyCommitted\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"AlreadyPublished\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidActivation\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidActivation\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidConfiguration\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidEvidence\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidEvidence\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"SafeCastOverflowedUintDowncast\",\"inputs\":[{\"name\":\"bits\",\"type\":\"uint8\",\"internalType\":\"uint8\"},{\"name\":\"value\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"Unanchored\",\"inputs\":[]}]",
	ID:  "STValidatorEvidence",
}

// STValidatorEvidence is an auto generated Go binding around an Ethereum contract.
type STValidatorEvidence struct {
	abi abi.ABI
}

// NewSTValidatorEvidence creates a new instance of STValidatorEvidence.
func NewSTValidatorEvidence() *STValidatorEvidence {
	parsed, err := STValidatorEvidenceMetaData.ParseABI()
	if err != nil {
		panic(errors.New("invalid ABI: " + err.Error()))
	}
	return &STValidatorEvidence{abi: *parsed}
}

// Instance creates a wrapper for a deployed contract instance at the given address.
// Use this to create the instance object passed to abigen v2 library functions Call, Transact, etc.
func (c *STValidatorEvidence) Instance(backend bind.ContractBackend, addr common.Address) *bind.BoundContract {
	return bind.NewBoundContract(addr, c.abi, backend, backend, backend)
}

// PackConstructor is the Go binding used to pack the parameters required for
// contract deployment.
//
// Solidity: constructor(address coordinator_, bytes32 genesisHash_, bytes32 deploymentIdHash_) returns()
func (sTValidatorEvidence *STValidatorEvidence) PackConstructor(coordinator_ common.Address, genesisHash_ [32]byte, deploymentIdHash_ [32]byte) []byte {
	enc, err := sTValidatorEvidence.abi.Pack("", coordinator_, genesisHash_, deploymentIdHash_)
	if err != nil {
		panic(err)
	}
	return enc
}

// PackActivation is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x62718f25.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function activation(bytes32 digest) view returns((((uint64,bytes32,uint16,address,address,bytes32,bytes32,uint64),bytes32,uint64,bytes32,uint64,bytes32,uint64,bytes32,uint64,bytes32),uint64))
func (sTValidatorEvidence *STValidatorEvidence) PackActivation(digest [32]byte) []byte {
	enc, err := sTValidatorEvidence.abi.Pack("activation", digest)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackActivation is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x62718f25.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function activation(bytes32 digest) view returns((((uint64,bytes32,uint16,address,address,bytes32,bytes32,uint64),bytes32,uint64,bytes32,uint64,bytes32,uint64,bytes32,uint64,bytes32),uint64))
func (sTValidatorEvidence *STValidatorEvidence) TryPackActivation(digest [32]byte) ([]byte, error) {
	return sTValidatorEvidence.abi.Pack("activation", digest)
}

// UnpackActivation is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x62718f25.
//
// Solidity: function activation(bytes32 digest) view returns((((uint64,bytes32,uint16,address,address,bytes32,bytes32,uint64),bytes32,uint64,bytes32,uint64,bytes32,uint64,bytes32,uint64,bytes32),uint64))
func (sTValidatorEvidence *STValidatorEvidence) UnpackActivation(data []byte) (STValidatorEvidenceActivation, error) {
	out, err := sTValidatorEvidence.abi.Unpack("activation", data)
	if err != nil {
		return *new(STValidatorEvidenceActivation), err
	}
	out0 := *abi.ConvertType(out[0], new(STValidatorEvidenceActivation)).(*STValidatorEvidenceActivation)
	return out0, nil
}

// PackChainId is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x9a8a0592.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function chainId() view returns(uint64)
func (sTValidatorEvidence *STValidatorEvidence) PackChainId() []byte {
	enc, err := sTValidatorEvidence.abi.Pack("chainId")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackChainId is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x9a8a0592.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function chainId() view returns(uint64)
func (sTValidatorEvidence *STValidatorEvidence) TryPackChainId() ([]byte, error) {
	return sTValidatorEvidence.abi.Pack("chainId")
}

// UnpackChainId is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x9a8a0592.
//
// Solidity: function chainId() view returns(uint64)
func (sTValidatorEvidence *STValidatorEvidence) UnpackChainId(data []byte) (uint64, error) {
	out, err := sTValidatorEvidence.abi.Unpack("chainId", data)
	if err != nil {
		return *new(uint64), err
	}
	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	return out0, nil
}

// PackCommitEvidence is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xbfa11cff.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function commitEvidence(((uint64,bytes32,uint16,address,address,bytes32,bytes32,uint64,bytes32),bytes32,uint64,uint64,uint8,(uint64,uint64),bytes32,uint64,bytes32,bytes32,bytes32,uint64) header, bytes vpkSignature, bytes hotkeySignature) returns(bytes32 slot)
func (sTValidatorEvidence *STValidatorEvidence) PackCommitEvidence(header ValidatorEvidenceHeader, vpkSignature []byte, hotkeySignature []byte) []byte {
	enc, err := sTValidatorEvidence.abi.Pack("commitEvidence", header, vpkSignature, hotkeySignature)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCommitEvidence is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xbfa11cff.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function commitEvidence(((uint64,bytes32,uint16,address,address,bytes32,bytes32,uint64,bytes32),bytes32,uint64,uint64,uint8,(uint64,uint64),bytes32,uint64,bytes32,bytes32,bytes32,uint64) header, bytes vpkSignature, bytes hotkeySignature) returns(bytes32 slot)
func (sTValidatorEvidence *STValidatorEvidence) TryPackCommitEvidence(header ValidatorEvidenceHeader, vpkSignature []byte, hotkeySignature []byte) ([]byte, error) {
	return sTValidatorEvidence.abi.Pack("commitEvidence", header, vpkSignature, hotkeySignature)
}

// UnpackCommitEvidence is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xbfa11cff.
//
// Solidity: function commitEvidence(((uint64,bytes32,uint16,address,address,bytes32,bytes32,uint64,bytes32),bytes32,uint64,uint64,uint8,(uint64,uint64),bytes32,uint64,bytes32,bytes32,bytes32,uint64) header, bytes vpkSignature, bytes hotkeySignature) returns(bytes32 slot)
func (sTValidatorEvidence *STValidatorEvidence) UnpackCommitEvidence(data []byte) ([32]byte, error) {
	out, err := sTValidatorEvidence.abi.Unpack("commitEvidence", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackCommitment is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x9fcb0985.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function commitment(bytes32 slot) view returns((((uint64,bytes32,uint16,address,address,bytes32,bytes32,uint64,bytes32),bytes32,uint64,uint64,uint8,(uint64,uint64),bytes32,uint64,bytes32,bytes32,bytes32,uint64),uint64))
func (sTValidatorEvidence *STValidatorEvidence) PackCommitment(slot [32]byte) []byte {
	enc, err := sTValidatorEvidence.abi.Pack("commitment", slot)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackCommitment is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x9fcb0985.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function commitment(bytes32 slot) view returns((((uint64,bytes32,uint16,address,address,bytes32,bytes32,uint64,bytes32),bytes32,uint64,uint64,uint8,(uint64,uint64),bytes32,uint64,bytes32,bytes32,bytes32,uint64),uint64))
func (sTValidatorEvidence *STValidatorEvidence) TryPackCommitment(slot [32]byte) ([]byte, error) {
	return sTValidatorEvidence.abi.Pack("commitment", slot)
}

// UnpackCommitment is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x9fcb0985.
//
// Solidity: function commitment(bytes32 slot) view returns((((uint64,bytes32,uint16,address,address,bytes32,bytes32,uint64,bytes32),bytes32,uint64,uint64,uint8,(uint64,uint64),bytes32,uint64,bytes32,bytes32,bytes32,uint64),uint64))
func (sTValidatorEvidence *STValidatorEvidence) UnpackCommitment(data []byte) (STValidatorEvidenceCommitment, error) {
	out, err := sTValidatorEvidence.abi.Unpack("commitment", data)
	if err != nil {
		return *new(STValidatorEvidenceCommitment), err
	}
	out0 := *abi.ConvertType(out[0], new(STValidatorEvidenceCommitment)).(*STValidatorEvidenceCommitment)
	return out0, nil
}

// PackCoordinator is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x0a009097.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function coordinator() view returns(address)
func (sTValidatorEvidence *STValidatorEvidence) PackCoordinator() []byte {
	enc, err := sTValidatorEvidence.abi.Pack("coordinator")
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
func (sTValidatorEvidence *STValidatorEvidence) TryPackCoordinator() ([]byte, error) {
	return sTValidatorEvidence.abi.Pack("coordinator")
}

// UnpackCoordinator is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x0a009097.
//
// Solidity: function coordinator() view returns(address)
func (sTValidatorEvidence *STValidatorEvidence) UnpackCoordinator(data []byte) (common.Address, error) {
	out, err := sTValidatorEvidence.abi.Unpack("coordinator", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackDeploymentIdHash is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x05f548dc.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function deploymentIdHash() view returns(bytes32)
func (sTValidatorEvidence *STValidatorEvidence) PackDeploymentIdHash() []byte {
	enc, err := sTValidatorEvidence.abi.Pack("deploymentIdHash")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackDeploymentIdHash is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x05f548dc.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function deploymentIdHash() view returns(bytes32)
func (sTValidatorEvidence *STValidatorEvidence) TryPackDeploymentIdHash() ([]byte, error) {
	return sTValidatorEvidence.abi.Pack("deploymentIdHash")
}

// UnpackDeploymentIdHash is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x05f548dc.
//
// Solidity: function deploymentIdHash() view returns(bytes32)
func (sTValidatorEvidence *STValidatorEvidence) UnpackDeploymentIdHash(data []byte) ([32]byte, error) {
	out, err := sTValidatorEvidence.abi.Unpack("deploymentIdHash", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackGenesisHash is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x94391a6d.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function genesisHash() view returns(bytes32)
func (sTValidatorEvidence *STValidatorEvidence) PackGenesisHash() []byte {
	enc, err := sTValidatorEvidence.abi.Pack("genesisHash")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackGenesisHash is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x94391a6d.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function genesisHash() view returns(bytes32)
func (sTValidatorEvidence *STValidatorEvidence) TryPackGenesisHash() ([]byte, error) {
	return sTValidatorEvidence.abi.Pack("genesisHash")
}

// UnpackGenesisHash is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x94391a6d.
//
// Solidity: function genesisHash() view returns(bytes32)
func (sTValidatorEvidence *STValidatorEvidence) UnpackGenesisHash(data []byte) ([32]byte, error) {
	out, err := sTValidatorEvidence.abi.Unpack("genesisHash", data)
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
func (sTValidatorEvidence *STValidatorEvidence) PackNetuid() []byte {
	enc, err := sTValidatorEvidence.abi.Pack("netuid")
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
func (sTValidatorEvidence *STValidatorEvidence) TryPackNetuid() ([]byte, error) {
	return sTValidatorEvidence.abi.Pack("netuid")
}

// UnpackNetuid is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xe78015b1.
//
// Solidity: function netuid() view returns(uint16)
func (sTValidatorEvidence *STValidatorEvidence) UnpackNetuid(data []byte) (uint16, error) {
	out, err := sTValidatorEvidence.abi.Unpack("netuid", data)
	if err != nil {
		return *new(uint16), err
	}
	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)
	return out0, nil
}

// PackPublishActivation is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe5d99b44.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function publishActivation(((uint64,bytes32,uint16,address,address,bytes32,bytes32,uint64),bytes32,uint64,bytes32,uint64,bytes32,uint64,bytes32,uint64,bytes32) record, bytes vpkSignature, bytes hotkeySignature) returns(bytes32 digest)
func (sTValidatorEvidence *STValidatorEvidence) PackPublishActivation(record ValidatorEvidenceActivationRecord, vpkSignature []byte, hotkeySignature []byte) []byte {
	enc, err := sTValidatorEvidence.abi.Pack("publishActivation", record, vpkSignature, hotkeySignature)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPublishActivation is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xe5d99b44.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function publishActivation(((uint64,bytes32,uint16,address,address,bytes32,bytes32,uint64),bytes32,uint64,bytes32,uint64,bytes32,uint64,bytes32,uint64,bytes32) record, bytes vpkSignature, bytes hotkeySignature) returns(bytes32 digest)
func (sTValidatorEvidence *STValidatorEvidence) TryPackPublishActivation(record ValidatorEvidenceActivationRecord, vpkSignature []byte, hotkeySignature []byte) ([]byte, error) {
	return sTValidatorEvidence.abi.Pack("publishActivation", record, vpkSignature, hotkeySignature)
}

// UnpackPublishActivation is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xe5d99b44.
//
// Solidity: function publishActivation(((uint64,bytes32,uint16,address,address,bytes32,bytes32,uint64),bytes32,uint64,bytes32,uint64,bytes32,uint64,bytes32,uint64,bytes32) record, bytes vpkSignature, bytes hotkeySignature) returns(bytes32 digest)
func (sTValidatorEvidence *STValidatorEvidence) UnpackPublishActivation(data []byte) ([32]byte, error) {
	out, err := sTValidatorEvidence.abi.Unpack("publishActivation", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackSettlementVault is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x2aa84ce6.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function settlementVault() view returns(address)
func (sTValidatorEvidence *STValidatorEvidence) PackSettlementVault() []byte {
	enc, err := sTValidatorEvidence.abi.Pack("settlementVault")
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
func (sTValidatorEvidence *STValidatorEvidence) TryPackSettlementVault() ([]byte, error) {
	return sTValidatorEvidence.abi.Pack("settlementVault")
}

// UnpackSettlementVault is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x2aa84ce6.
//
// Solidity: function settlementVault() view returns(address)
func (sTValidatorEvidence *STValidatorEvidence) UnpackSettlementVault(data []byte) (common.Address, error) {
	out, err := sTValidatorEvidence.abi.Unpack("settlementVault", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// STValidatorEvidenceActivationPublished represents a ActivationPublished event raised by the STValidatorEvidence contract.
type STValidatorEvidenceActivationPublished struct {
	ActivationHash [32]byte
	Hotkey         [32]byte
	NoId           uint64
	Epoch          uint64
	Raw            *types.Log // Blockchain specific contextual infos
}

const STValidatorEvidenceActivationPublishedEventName = "ActivationPublished"

// ContractEventName returns the user-defined event name.
func (STValidatorEvidenceActivationPublished) ContractEventName() string {
	return STValidatorEvidenceActivationPublishedEventName
}

// UnpackActivationPublishedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event ActivationPublished(bytes32 indexed activationHash, bytes32 indexed hotkey, uint64 indexed noId, uint64 epoch)
func (sTValidatorEvidence *STValidatorEvidence) UnpackActivationPublishedEvent(log *types.Log) (*STValidatorEvidenceActivationPublished, error) {
	event := "ActivationPublished"
	if len(log.Topics) == 0 || log.Topics[0] != sTValidatorEvidence.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STValidatorEvidenceActivationPublished)
	if len(log.Data) > 0 {
		if err := sTValidatorEvidence.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTValidatorEvidence.abi.Events[event].Inputs {
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

// STValidatorEvidenceEvidenceCommitted represents a EvidenceCommitted event raised by the STValidatorEvidence contract.
type STValidatorEvidenceEvidenceCommitted struct {
	Slot         [32]byte
	NoId         uint64
	Epoch        uint64
	HeaderHash   [32]byte
	PayloadHash  [32]byte
	CensusHash   [32]byte
	PayloadBytes uint64
	Raw          *types.Log // Blockchain specific contextual infos
}

const STValidatorEvidenceEvidenceCommittedEventName = "EvidenceCommitted"

// ContractEventName returns the user-defined event name.
func (STValidatorEvidenceEvidenceCommitted) ContractEventName() string {
	return STValidatorEvidenceEvidenceCommittedEventName
}

// UnpackEvidenceCommittedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event EvidenceCommitted(bytes32 indexed slot, uint64 indexed noId, uint64 indexed epoch, bytes32 headerHash, bytes32 payloadHash, bytes32 censusHash, uint64 payloadBytes)
func (sTValidatorEvidence *STValidatorEvidence) UnpackEvidenceCommittedEvent(log *types.Log) (*STValidatorEvidenceEvidenceCommitted, error) {
	event := "EvidenceCommitted"
	if len(log.Topics) == 0 || log.Topics[0] != sTValidatorEvidence.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STValidatorEvidenceEvidenceCommitted)
	if len(log.Data) > 0 {
		if err := sTValidatorEvidence.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTValidatorEvidence.abi.Events[event].Inputs {
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
func (sTValidatorEvidence *STValidatorEvidence) UnpackError(raw []byte) (any, error) {
	if bytes.Equal(raw[:4], sTValidatorEvidence.abi.Errors["AlreadyCommitted"].ID.Bytes()[:4]) {
		return sTValidatorEvidence.UnpackAlreadyCommittedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTValidatorEvidence.abi.Errors["AlreadyPublished"].ID.Bytes()[:4]) {
		return sTValidatorEvidence.UnpackAlreadyPublishedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTValidatorEvidence.abi.Errors["InvalidActivation"].ID.Bytes()[:4]) {
		return sTValidatorEvidence.UnpackInvalidActivationError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTValidatorEvidence.abi.Errors["InvalidConfiguration"].ID.Bytes()[:4]) {
		return sTValidatorEvidence.UnpackInvalidConfigurationError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTValidatorEvidence.abi.Errors["InvalidEvidence"].ID.Bytes()[:4]) {
		return sTValidatorEvidence.UnpackInvalidEvidenceError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTValidatorEvidence.abi.Errors["SafeCastOverflowedUintDowncast"].ID.Bytes()[:4]) {
		return sTValidatorEvidence.UnpackSafeCastOverflowedUintDowncastError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTValidatorEvidence.abi.Errors["Unanchored"].ID.Bytes()[:4]) {
		return sTValidatorEvidence.UnpackUnanchoredError(raw[4:])
	}
	return nil, errors.New("Unknown error")
}

// STValidatorEvidenceAlreadyCommitted represents a AlreadyCommitted error raised by the STValidatorEvidence contract.
type STValidatorEvidenceAlreadyCommitted struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error AlreadyCommitted()
func STValidatorEvidenceAlreadyCommittedErrorID() common.Hash {
	return common.HexToHash("0xbfec55587600524a9afa4af6da1e1345f08fce59d99e4869f19166c295681a3a")
}

// UnpackAlreadyCommittedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error AlreadyCommitted()
func (sTValidatorEvidence *STValidatorEvidence) UnpackAlreadyCommittedError(raw []byte) (*STValidatorEvidenceAlreadyCommitted, error) {
	out := new(STValidatorEvidenceAlreadyCommitted)
	if err := sTValidatorEvidence.abi.UnpackIntoInterface(out, "AlreadyCommitted", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STValidatorEvidenceAlreadyPublished represents a AlreadyPublished error raised by the STValidatorEvidence contract.
type STValidatorEvidenceAlreadyPublished struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error AlreadyPublished()
func STValidatorEvidenceAlreadyPublishedErrorID() common.Hash {
	return common.HexToHash("0x9ac89bcd36636601bfeb875d31ac8b074c026ad452c8287f1dd86f71a87a4b2d")
}

// UnpackAlreadyPublishedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error AlreadyPublished()
func (sTValidatorEvidence *STValidatorEvidence) UnpackAlreadyPublishedError(raw []byte) (*STValidatorEvidenceAlreadyPublished, error) {
	out := new(STValidatorEvidenceAlreadyPublished)
	if err := sTValidatorEvidence.abi.UnpackIntoInterface(out, "AlreadyPublished", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STValidatorEvidenceInvalidActivation represents a InvalidActivation error raised by the STValidatorEvidence contract.
type STValidatorEvidenceInvalidActivation struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidActivation()
func STValidatorEvidenceInvalidActivationErrorID() common.Hash {
	return common.HexToHash("0xe72f8984b932f5edba5c41acd88c04a4f7aab1e6f717e95e30b7688164f1e776")
}

// UnpackInvalidActivationError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidActivation()
func (sTValidatorEvidence *STValidatorEvidence) UnpackInvalidActivationError(raw []byte) (*STValidatorEvidenceInvalidActivation, error) {
	out := new(STValidatorEvidenceInvalidActivation)
	if err := sTValidatorEvidence.abi.UnpackIntoInterface(out, "InvalidActivation", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STValidatorEvidenceInvalidConfiguration represents a InvalidConfiguration error raised by the STValidatorEvidence contract.
type STValidatorEvidenceInvalidConfiguration struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidConfiguration()
func STValidatorEvidenceInvalidConfigurationErrorID() common.Hash {
	return common.HexToHash("0xc52a9bd3d9e475b9056a93172ef6968d775a7cd41c4255bbebf12e90a5fbbd39")
}

// UnpackInvalidConfigurationError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidConfiguration()
func (sTValidatorEvidence *STValidatorEvidence) UnpackInvalidConfigurationError(raw []byte) (*STValidatorEvidenceInvalidConfiguration, error) {
	out := new(STValidatorEvidenceInvalidConfiguration)
	if err := sTValidatorEvidence.abi.UnpackIntoInterface(out, "InvalidConfiguration", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STValidatorEvidenceInvalidEvidence represents a InvalidEvidence error raised by the STValidatorEvidence contract.
type STValidatorEvidenceInvalidEvidence struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidEvidence()
func STValidatorEvidenceInvalidEvidenceErrorID() common.Hash {
	return common.HexToHash("0xc9779e3cd09ff10b811ed85fd247314eb00b92b6d6bad730133e2fa6c692935d")
}

// UnpackInvalidEvidenceError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidEvidence()
func (sTValidatorEvidence *STValidatorEvidence) UnpackInvalidEvidenceError(raw []byte) (*STValidatorEvidenceInvalidEvidence, error) {
	out := new(STValidatorEvidenceInvalidEvidence)
	if err := sTValidatorEvidence.abi.UnpackIntoInterface(out, "InvalidEvidence", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STValidatorEvidenceSafeCastOverflowedUintDowncast represents a SafeCastOverflowedUintDowncast error raised by the STValidatorEvidence contract.
type STValidatorEvidenceSafeCastOverflowedUintDowncast struct {
	Bits  uint8
	Value *big.Int
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error SafeCastOverflowedUintDowncast(uint8 bits, uint256 value)
func STValidatorEvidenceSafeCastOverflowedUintDowncastErrorID() common.Hash {
	return common.HexToHash("0x6dfcc6503a32754ce7a89698e18201fc5294fd4aad43edefee786f88423b1a12")
}

// UnpackSafeCastOverflowedUintDowncastError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error SafeCastOverflowedUintDowncast(uint8 bits, uint256 value)
func (sTValidatorEvidence *STValidatorEvidence) UnpackSafeCastOverflowedUintDowncastError(raw []byte) (*STValidatorEvidenceSafeCastOverflowedUintDowncast, error) {
	out := new(STValidatorEvidenceSafeCastOverflowedUintDowncast)
	if err := sTValidatorEvidence.abi.UnpackIntoInterface(out, "SafeCastOverflowedUintDowncast", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STValidatorEvidenceUnanchored represents a Unanchored error raised by the STValidatorEvidence contract.
type STValidatorEvidenceUnanchored struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error Unanchored()
func STValidatorEvidenceUnanchoredErrorID() common.Hash {
	return common.HexToHash("0xda4f1dafc76815bd5f884bce92d9fcfd8a49717e8a13fcb6452a319ad9a273b9")
}

// UnpackUnanchoredError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error Unanchored()
func (sTValidatorEvidence *STValidatorEvidence) UnpackUnanchoredError(raw []byte) (*STValidatorEvidenceUnanchored, error) {
	out := new(STValidatorEvidenceUnanchored)
	if err := sTValidatorEvidence.abi.UnpackIntoInterface(out, "Unanchored", raw); err != nil {
		return nil, err
	}
	return out, nil
}
