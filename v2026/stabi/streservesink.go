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

// STReserveSinkMetaData contains all meta data concerning the STReserveSink contract.
var STReserveSinkMetaData = bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[{\"name\":\"netuid_\",\"type\":\"uint16\",\"internalType\":\"uint16\"},{\"name\":\"reserveHotkey_\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"selfColdkey_\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"bootstrap_\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"bootstrap\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"liveStake\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"netuid\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint16\",\"internalType\":\"uint16\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"operatorPrincipal\",\"inputs\":[{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"principal\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"recordPrincipal\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"recorder\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"reserveHotkey\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"selfColdkey\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"setRecorderOnce\",\"inputs\":[{\"name\":\"recorder_\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"event\",\"name\":\"RecorderFixed\",\"inputs\":[{\"name\":\"recorder\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ReservePrincipalAdded\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"noId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"operatorPrincipal\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"totalPrincipal\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"liveStake\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"AlreadyInitialized\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidConfiguration\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"ReserveUnderfunded\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"Unauthorized\",\"inputs\":[]}]",
	ID:  "STReserveSink",
}

// STReserveSink is an auto generated Go binding around an Ethereum contract.
type STReserveSink struct {
	abi abi.ABI
}

// NewSTReserveSink creates a new instance of STReserveSink.
func NewSTReserveSink() *STReserveSink {
	parsed, err := STReserveSinkMetaData.ParseABI()
	if err != nil {
		panic(errors.New("invalid ABI: " + err.Error()))
	}
	return &STReserveSink{abi: *parsed}
}

// Instance creates a wrapper for a deployed contract instance at the given address.
// Use this to create the instance object passed to abigen v2 library functions Call, Transact, etc.
func (c *STReserveSink) Instance(backend bind.ContractBackend, addr common.Address) *bind.BoundContract {
	return bind.NewBoundContract(addr, c.abi, backend, backend, backend)
}

// PackConstructor is the Go binding used to pack the parameters required for
// contract deployment.
//
// Solidity: constructor(uint16 netuid_, bytes32 reserveHotkey_, bytes32 selfColdkey_, address bootstrap_) returns()
func (sTReserveSink *STReserveSink) PackConstructor(netuid_ uint16, reserveHotkey_ [32]byte, selfColdkey_ [32]byte, bootstrap_ common.Address) []byte {
	enc, err := sTReserveSink.abi.Pack("", netuid_, reserveHotkey_, selfColdkey_, bootstrap_)
	if err != nil {
		panic(err)
	}
	return enc
}

// PackBootstrap is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xfb969b0a.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function bootstrap() view returns(address)
func (sTReserveSink *STReserveSink) PackBootstrap() []byte {
	enc, err := sTReserveSink.abi.Pack("bootstrap")
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
func (sTReserveSink *STReserveSink) TryPackBootstrap() ([]byte, error) {
	return sTReserveSink.abi.Pack("bootstrap")
}

// UnpackBootstrap is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xfb969b0a.
//
// Solidity: function bootstrap() view returns(address)
func (sTReserveSink *STReserveSink) UnpackBootstrap(data []byte) (common.Address, error) {
	out, err := sTReserveSink.abi.Unpack("bootstrap", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackLiveStake is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x5a5d6ce9.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function liveStake() view returns(uint256)
func (sTReserveSink *STReserveSink) PackLiveStake() []byte {
	enc, err := sTReserveSink.abi.Pack("liveStake")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackLiveStake is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x5a5d6ce9.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function liveStake() view returns(uint256)
func (sTReserveSink *STReserveSink) TryPackLiveStake() ([]byte, error) {
	return sTReserveSink.abi.Pack("liveStake")
}

// UnpackLiveStake is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x5a5d6ce9.
//
// Solidity: function liveStake() view returns(uint256)
func (sTReserveSink *STReserveSink) UnpackLiveStake(data []byte) (*big.Int, error) {
	out, err := sTReserveSink.abi.Unpack("liveStake", data)
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
func (sTReserveSink *STReserveSink) PackNetuid() []byte {
	enc, err := sTReserveSink.abi.Pack("netuid")
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
func (sTReserveSink *STReserveSink) TryPackNetuid() ([]byte, error) {
	return sTReserveSink.abi.Pack("netuid")
}

// UnpackNetuid is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xe78015b1.
//
// Solidity: function netuid() view returns(uint16)
func (sTReserveSink *STReserveSink) UnpackNetuid(data []byte) (uint16, error) {
	out, err := sTReserveSink.abi.Unpack("netuid", data)
	if err != nil {
		return *new(uint16), err
	}
	out0 := *abi.ConvertType(out[0], new(uint16)).(*uint16)
	return out0, nil
}

// PackOperatorPrincipal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x41b93c36.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function operatorPrincipal(uint256 noId) view returns(uint256 amount)
func (sTReserveSink *STReserveSink) PackOperatorPrincipal(noId *big.Int) []byte {
	enc, err := sTReserveSink.abi.Pack("operatorPrincipal", noId)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackOperatorPrincipal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x41b93c36.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function operatorPrincipal(uint256 noId) view returns(uint256 amount)
func (sTReserveSink *STReserveSink) TryPackOperatorPrincipal(noId *big.Int) ([]byte, error) {
	return sTReserveSink.abi.Pack("operatorPrincipal", noId)
}

// UnpackOperatorPrincipal is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x41b93c36.
//
// Solidity: function operatorPrincipal(uint256 noId) view returns(uint256 amount)
func (sTReserveSink *STReserveSink) UnpackOperatorPrincipal(data []byte) (*big.Int, error) {
	out, err := sTReserveSink.abi.Unpack("operatorPrincipal", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackPrincipal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xba5d3078.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function principal() view returns(uint256)
func (sTReserveSink *STReserveSink) PackPrincipal() []byte {
	enc, err := sTReserveSink.abi.Pack("principal")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackPrincipal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xba5d3078.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function principal() view returns(uint256)
func (sTReserveSink *STReserveSink) TryPackPrincipal() ([]byte, error) {
	return sTReserveSink.abi.Pack("principal")
}

// UnpackPrincipal is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xba5d3078.
//
// Solidity: function principal() view returns(uint256)
func (sTReserveSink *STReserveSink) UnpackPrincipal(data []byte) (*big.Int, error) {
	out, err := sTReserveSink.abi.Unpack("principal", data)
	if err != nil {
		return new(big.Int), err
	}
	out0 := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return out0, nil
}

// PackRecordPrincipal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf1c32dbe.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function recordPrincipal(uint256 epoch, uint256 noId, uint256 amount) returns()
func (sTReserveSink *STReserveSink) PackRecordPrincipal(epoch *big.Int, noId *big.Int, amount *big.Int) []byte {
	enc, err := sTReserveSink.abi.Pack("recordPrincipal", epoch, noId, amount)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRecordPrincipal is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf1c32dbe.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function recordPrincipal(uint256 epoch, uint256 noId, uint256 amount) returns()
func (sTReserveSink *STReserveSink) TryPackRecordPrincipal(epoch *big.Int, noId *big.Int, amount *big.Int) ([]byte, error) {
	return sTReserveSink.abi.Pack("recordPrincipal", epoch, noId, amount)
}

// PackRecorder is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf33930d9.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function recorder() view returns(address)
func (sTReserveSink *STReserveSink) PackRecorder() []byte {
	enc, err := sTReserveSink.abi.Pack("recorder")
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackRecorder is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xf33930d9.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function recorder() view returns(address)
func (sTReserveSink *STReserveSink) TryPackRecorder() ([]byte, error) {
	return sTReserveSink.abi.Pack("recorder")
}

// UnpackRecorder is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0xf33930d9.
//
// Solidity: function recorder() view returns(address)
func (sTReserveSink *STReserveSink) UnpackRecorder(data []byte) (common.Address, error) {
	out, err := sTReserveSink.abi.Unpack("recorder", data)
	if err != nil {
		return *new(common.Address), err
	}
	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	return out0, nil
}

// PackReserveHotkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x6ac86cc5.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function reserveHotkey() view returns(bytes32)
func (sTReserveSink *STReserveSink) PackReserveHotkey() []byte {
	enc, err := sTReserveSink.abi.Pack("reserveHotkey")
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
func (sTReserveSink *STReserveSink) TryPackReserveHotkey() ([]byte, error) {
	return sTReserveSink.abi.Pack("reserveHotkey")
}

// UnpackReserveHotkey is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x6ac86cc5.
//
// Solidity: function reserveHotkey() view returns(bytes32)
func (sTReserveSink *STReserveSink) UnpackReserveHotkey(data []byte) ([32]byte, error) {
	out, err := sTReserveSink.abi.Unpack("reserveHotkey", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackSelfColdkey is the Go binding used to pack the parameters required for calling
// the contract method with ID 0x877e4394.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function selfColdkey() view returns(bytes32)
func (sTReserveSink *STReserveSink) PackSelfColdkey() []byte {
	enc, err := sTReserveSink.abi.Pack("selfColdkey")
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
func (sTReserveSink *STReserveSink) TryPackSelfColdkey() ([]byte, error) {
	return sTReserveSink.abi.Pack("selfColdkey")
}

// UnpackSelfColdkey is the Go binding that unpacks the parameters returned
// from invoking the contract method with ID 0x877e4394.
//
// Solidity: function selfColdkey() view returns(bytes32)
func (sTReserveSink *STReserveSink) UnpackSelfColdkey(data []byte) ([32]byte, error) {
	out, err := sTReserveSink.abi.Unpack("selfColdkey", data)
	if err != nil {
		return *new([32]byte), err
	}
	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	return out0, nil
}

// PackSetRecorderOnce is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc00d1252.  This method will panic if any
// invalid/nil inputs are passed.
//
// Solidity: function setRecorderOnce(address recorder_) returns()
func (sTReserveSink *STReserveSink) PackSetRecorderOnce(recorder common.Address) []byte {
	enc, err := sTReserveSink.abi.Pack("setRecorderOnce", recorder)
	if err != nil {
		panic(err)
	}
	return enc
}

// TryPackSetRecorderOnce is the Go binding used to pack the parameters required for calling
// the contract method with ID 0xc00d1252.  This method will return an error
// if any inputs are invalid/nil.
//
// Solidity: function setRecorderOnce(address recorder_) returns()
func (sTReserveSink *STReserveSink) TryPackSetRecorderOnce(recorder common.Address) ([]byte, error) {
	return sTReserveSink.abi.Pack("setRecorderOnce", recorder)
}

// STReserveSinkRecorderFixed represents a RecorderFixed event raised by the STReserveSink contract.
type STReserveSinkRecorderFixed struct {
	Recorder common.Address
	Raw      *types.Log // Blockchain specific contextual infos
}

const STReserveSinkRecorderFixedEventName = "RecorderFixed"

// ContractEventName returns the user-defined event name.
func (STReserveSinkRecorderFixed) ContractEventName() string {
	return STReserveSinkRecorderFixedEventName
}

// UnpackRecorderFixedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event RecorderFixed(address indexed recorder)
func (sTReserveSink *STReserveSink) UnpackRecorderFixedEvent(log *types.Log) (*STReserveSinkRecorderFixed, error) {
	event := "RecorderFixed"
	if len(log.Topics) == 0 || log.Topics[0] != sTReserveSink.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STReserveSinkRecorderFixed)
	if len(log.Data) > 0 {
		if err := sTReserveSink.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTReserveSink.abi.Events[event].Inputs {
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

// STReserveSinkReservePrincipalAdded represents a ReservePrincipalAdded event raised by the STReserveSink contract.
type STReserveSinkReservePrincipalAdded struct {
	Epoch             *big.Int
	NoId              *big.Int
	Amount            *big.Int
	OperatorPrincipal *big.Int
	TotalPrincipal    *big.Int
	LiveStake         *big.Int
	Raw               *types.Log // Blockchain specific contextual infos
}

const STReserveSinkReservePrincipalAddedEventName = "ReservePrincipalAdded"

// ContractEventName returns the user-defined event name.
func (STReserveSinkReservePrincipalAdded) ContractEventName() string {
	return STReserveSinkReservePrincipalAddedEventName
}

// UnpackReservePrincipalAddedEvent is the Go binding that unpacks the event data emitted
// by contract.
//
// Solidity: event ReservePrincipalAdded(uint256 indexed epoch, uint256 indexed noId, uint256 amount, uint256 operatorPrincipal, uint256 totalPrincipal, uint256 liveStake)
func (sTReserveSink *STReserveSink) UnpackReservePrincipalAddedEvent(log *types.Log) (*STReserveSinkReservePrincipalAdded, error) {
	event := "ReservePrincipalAdded"
	if len(log.Topics) == 0 || log.Topics[0] != sTReserveSink.abi.Events[event].ID {
		return nil, errors.New("event signature mismatch")
	}
	out := new(STReserveSinkReservePrincipalAdded)
	if len(log.Data) > 0 {
		if err := sTReserveSink.abi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return nil, err
		}
	}
	var indexed abi.Arguments
	for _, arg := range sTReserveSink.abi.Events[event].Inputs {
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
func (sTReserveSink *STReserveSink) UnpackError(raw []byte) (any, error) {
	if bytes.Equal(raw[:4], sTReserveSink.abi.Errors["AlreadyInitialized"].ID.Bytes()[:4]) {
		return sTReserveSink.UnpackAlreadyInitializedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTReserveSink.abi.Errors["InvalidConfiguration"].ID.Bytes()[:4]) {
		return sTReserveSink.UnpackInvalidConfigurationError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTReserveSink.abi.Errors["ReserveUnderfunded"].ID.Bytes()[:4]) {
		return sTReserveSink.UnpackReserveUnderfundedError(raw[4:])
	}
	if bytes.Equal(raw[:4], sTReserveSink.abi.Errors["Unauthorized"].ID.Bytes()[:4]) {
		return sTReserveSink.UnpackUnauthorizedError(raw[4:])
	}
	return nil, errors.New("Unknown error")
}

// STReserveSinkAlreadyInitialized represents a AlreadyInitialized error raised by the STReserveSink contract.
type STReserveSinkAlreadyInitialized struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error AlreadyInitialized()
func STReserveSinkAlreadyInitializedErrorID() common.Hash {
	return common.HexToHash("0x0dc149f07762891dbcea3fe72770f3d63a1863fc54b2f084e8c59ec476996927")
}

// UnpackAlreadyInitializedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error AlreadyInitialized()
func (sTReserveSink *STReserveSink) UnpackAlreadyInitializedError(raw []byte) (*STReserveSinkAlreadyInitialized, error) {
	out := new(STReserveSinkAlreadyInitialized)
	if err := sTReserveSink.abi.UnpackIntoInterface(out, "AlreadyInitialized", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STReserveSinkInvalidConfiguration represents a InvalidConfiguration error raised by the STReserveSink contract.
type STReserveSinkInvalidConfiguration struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error InvalidConfiguration()
func STReserveSinkInvalidConfigurationErrorID() common.Hash {
	return common.HexToHash("0xc52a9bd3d9e475b9056a93172ef6968d775a7cd41c4255bbebf12e90a5fbbd39")
}

// UnpackInvalidConfigurationError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error InvalidConfiguration()
func (sTReserveSink *STReserveSink) UnpackInvalidConfigurationError(raw []byte) (*STReserveSinkInvalidConfiguration, error) {
	out := new(STReserveSinkInvalidConfiguration)
	if err := sTReserveSink.abi.UnpackIntoInterface(out, "InvalidConfiguration", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STReserveSinkReserveUnderfunded represents a ReserveUnderfunded error raised by the STReserveSink contract.
type STReserveSinkReserveUnderfunded struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error ReserveUnderfunded()
func STReserveSinkReserveUnderfundedErrorID() common.Hash {
	return common.HexToHash("0x1af32e45a8b1748f5f5a3a523ef0207315d317df6d98eda631869b6580162c98")
}

// UnpackReserveUnderfundedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error ReserveUnderfunded()
func (sTReserveSink *STReserveSink) UnpackReserveUnderfundedError(raw []byte) (*STReserveSinkReserveUnderfunded, error) {
	out := new(STReserveSinkReserveUnderfunded)
	if err := sTReserveSink.abi.UnpackIntoInterface(out, "ReserveUnderfunded", raw); err != nil {
		return nil, err
	}
	return out, nil
}

// STReserveSinkUnauthorized represents a Unauthorized error raised by the STReserveSink contract.
type STReserveSinkUnauthorized struct {
}

// ErrorID returns the hash of canonical representation of the error's signature.
//
// Solidity: error Unauthorized()
func STReserveSinkUnauthorizedErrorID() common.Hash {
	return common.HexToHash("0x82b4290015f7ec7256ca2a6247d3c2a89c4865c0e791456df195f40ad0a81367")
}

// UnpackUnauthorizedError is the Go binding used to decode the provided
// error data into the corresponding Go error struct.
//
// Solidity: error Unauthorized()
func (sTReserveSink *STReserveSink) UnpackUnauthorizedError(raw []byte) (*STReserveSinkUnauthorized, error) {
	out := new(STReserveSinkUnauthorized)
	if err := sTReserveSink.abi.UnpackIntoInterface(out, "Unauthorized", raw); err != nil {
		return nil, err
	}
	return out, nil
}
