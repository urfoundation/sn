//go:build runtime_rehearsal

package rehearsal

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/ss58"
)

var (
	neuronAddress  = common.HexToAddress("0x0000000000000000000000000000000000000804")
	stakingAddress = common.HexToAddress("0x0000000000000000000000000000000000000805")
	alphaAddress   = common.HexToAddress("0x0000000000000000000000000000000000000808")
)

// evmKey is a fresh EVM account.
type evmKey struct {
	private *ecdsa.PrivateKey
	address common.Address
}

func newEVMKey(t *testing.T) evmKey {
	t.Helper()
	private, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return evmKey{private: private, address: crypto.PubkeyToAddress(private.PublicKey)}
}

// mirror is the coldkey the runtime acts for when address calls a precompile.
func mirror(address common.Address) [32]byte {
	return ss58.EvmMirrorPubkey(address)
}

// fundEVM funds address's mapped account; its EVM balance is rao × 10^9 wei.
func (self *node) fundEVM(t *testing.T, address common.Address, rao uint64) {
	t.Helper()
	self.fund(t, mirror(address), rao)
}

// artifact is a contract compiled by forge with the pinned toolchain of
// evm/foundry.toml; creation is empty for an interface.
type artifact struct {
	abi      abi.ABI
	creation []byte
}

// forgeArtifacts holds every loaded artifact, and the names of the custom
// errors they declare by selector, so a revert that bubbles up through
// another contract is still named.
var forgeArtifacts = struct {
	sync.Mutex
	loaded map[string]artifact
	errors map[string]string
}{loaded: map[string]artifact{}, errors: map[string]string{}}

// forgeArtifact builds source (relative to evm/) with forge and loads the
// artifact of contract name from evm/out. Builds run one at a time, and each
// artifact loads once per test binary, so parallel tests share them.
func forgeArtifact(t *testing.T, source, name string) artifact {
	t.Helper()
	forgeArtifacts.Lock()
	defer forgeArtifacts.Unlock()
	if loaded, ok := forgeArtifacts.loaded[source+":"+name]; ok {
		return loaded
	}
	evm, err := filepath.Abs(filepath.Join("..", "evm"))
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("forge", "build", source)
	build.Dir = evm
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("forge build %s: %v\n%s", source, err, output)
	}
	raw, err := os.ReadFile(filepath.Join(evm, "out", filepath.Base(source), name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		ABI      json.RawMessage `json:"abi"`
		Bytecode struct {
			Object string `json:"object"`
		} `json:"bytecode"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	contract, err := abi.JSON(bytes.NewReader(parsed.ABI))
	if err != nil {
		t.Fatal(err)
	}
	creation, err := hexutil.Decode(parsed.Bytecode.Object)
	if err != nil {
		t.Fatalf("%s bytecode: %v", name, err)
	}
	loaded := artifact{abi: contract, creation: creation}
	forgeArtifacts.loaded[source+":"+name] = loaded
	for errorName, custom := range contract.Errors {
		forgeArtifacts.errors[hexutil.Encode(custom.ID[:4])] = errorName
	}
	return loaded
}

// pack ABI-encodes a call to method.
func (self artifact) pack(t *testing.T, method string, args ...any) []byte {
	t.Helper()
	data, err := self.abi.Pack(method, args...)
	if err != nil {
		t.Fatalf("pack %s: %v", method, err)
	}
	return data
}

// unpack decodes method's return values.
func (self artifact) unpack(t *testing.T, method string, data []byte) []any {
	t.Helper()
	values, err := self.abi.Unpack(method, data)
	if err != nil {
		t.Fatalf("unpack %s: %v", method, err)
	}
	return values
}

// transact sends a transaction, seals it into its own block and returns its
// receipt, which may report a failure. value may be nil; gas zero takes
// eth_estimateGas.
func (self *node) transact(t *testing.T, from evmKey, to *common.Address, value *big.Int, data []byte, gas uint64) *gethtypes.Receipt {
	t.Helper()
	price, err := self.eth.SuggestGasPrice(self.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return self.sendSigned(t, self.sign(t, from, to, value, data, gas, price))
}

// sign signs a transaction at from's next nonce without sending it; gas zero
// takes eth_estimateGas.
func (self *node) sign(t *testing.T, from evmKey, to *common.Address, value *big.Int, data []byte, gas uint64, price *big.Int) *gethtypes.Transaction {
	t.Helper()
	nonce, err := self.eth.NonceAt(self.ctx, from.address, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gas == 0 {
		if gas, err = self.eth.EstimateGas(self.ctx, ethereum.CallMsg{From: from.address, To: to, Value: value, Data: data}); err != nil {
			t.Fatalf("estimate gas: %v", err)
		}
	}
	tx, err := gethtypes.SignNewTx(from.private, gethtypes.LatestSignerForChainID(big.NewInt(evmChainID)),
		&gethtypes.LegacyTx{Nonce: nonce, GasPrice: price, Gas: gas, To: to, Value: value, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

// sendSigned sends a signed transaction, seals it into its own block and
// returns its receipt, which may report a failure.
func (self *node) sendSigned(t *testing.T, tx *gethtypes.Transaction) *gethtypes.Receipt {
	t.Helper()
	if err := self.eth.SendTransaction(self.ctx, tx); err != nil {
		t.Fatalf("send transaction: %v", err)
	}
	self.seal(t)
	// The node indexes Ethereum blocks just after import.
	deadline := time.Now().Add(30 * time.Second)
	for {
		receipt, err := self.eth.TransactionReceipt(self.ctx, tx.Hash())
		if err == nil {
			return receipt
		}
		if !errors.Is(err, ethereum.NotFound) || time.Now().After(deadline) {
			t.Fatalf("receipt of %s: %v", tx.Hash(), err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// sendTx signs and sends a transaction without value, naming gas, and
// returns the pool's answer; an accepted one is sealed.
func (self *node) sendTx(t *testing.T, from evmKey, to *common.Address, data []byte, gas uint64) error {
	t.Helper()
	nonce, err := self.eth.NonceAt(self.ctx, from.address, nil)
	if err != nil {
		t.Fatal(err)
	}
	price, err := self.eth.SuggestGasPrice(self.ctx)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := gethtypes.SignNewTx(from.private, gethtypes.LatestSignerForChainID(big.NewInt(evmChainID)),
		&gethtypes.LegacyTx{Nonce: nonce, GasPrice: price, Gas: gas, To: to, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if err := self.eth.SendTransaction(self.ctx, tx); err != nil {
		return err
	}
	self.seal(t)
	return nil
}

// deploy runs creation bytecode (with any constructor arguments appended)
// and returns the new contract's address.
func (self *node) deploy(t *testing.T, from evmKey, creation []byte) common.Address {
	t.Helper()
	receipt := self.transact(t, from, nil, nil, creation, 0)
	if receipt.Status != gethtypes.ReceiptStatusSuccessful {
		t.Fatalf("deployment %s failed", receipt.TxHash)
	}
	return receipt.ContractAddress
}

// ethCall runs data against to at the latest block without a transaction.
// eth_call skips EIP-3607, so from may be a contract: a call to a precompile
// from a contract's address then runs as that contract's mapped coldkey,
// which reports the runtime's reason for a refusal.
func (self *node) ethCall(from, to common.Address, data []byte) ([]byte, error) {
	return self.eth.CallContract(self.ctx, ethereum.CallMsg{From: from, To: &to, Data: data}, nil)
}

// stake reads coldkey's alpha on hotkey through the staking precompile's getStake.
func (self *node) stake(t *testing.T, hotkey, coldkey [32]byte, netuid uint16) uint64 {
	t.Helper()
	out, err := self.ethCall(common.Address{}, stakingAddress, self.staking.pack(t, "getStake", hotkey, coldkey, big.NewInt(int64(netuid))))
	if err != nil {
		t.Fatalf("getStake: %v", err)
	}
	stake := self.staking.unpack(t, "getStake", out)[0].(*big.Int)
	if !stake.IsUint64() {
		t.Fatalf("getStake returned %s", stake)
	}
	return stake.Uint64()
}

// estimate runs eth_estimateGas: the gas limit a call needs.
func (self *node) estimate(from, to common.Address, value *big.Int, data []byte) (uint64, error) {
	return self.eth.EstimateGas(self.ctx, ethereum.CallMsg{From: from, To: &to, Value: value, Data: data})
}

// least returns the least gas limit, to the gas, at which an eth_call of msg
// at the latest block satisfies ok. It must do so at high.
func (self *node) least(t *testing.T, msg ethereum.CallMsg, high uint64, ok func([]byte, error) bool) uint64 {
	t.Helper()
	call := func(gas uint64) bool {
		msg.Gas = gas
		return ok(self.eth.CallContract(self.ctx, msg, nil))
	}
	if !call(high) {
		t.Fatalf("the call fails with %d gas", high)
	}
	low := uint64(21_000)
	for high-low > 1 {
		if middle := (low + high) / 2; call(middle) {
			high = middle
		} else {
			low = middle
		}
	}
	return high
}

// sendLeast is send with the least gas limit at which an eth_call of the
// call succeeds, searched below the limit eth_estimateGas gives, which it
// returns with that least limit.
func (self *node) sendLeast(t *testing.T, from evmKey, to common.Address, contract artifact, value *big.Int, method string, args ...any) (receipt *gethtypes.Receipt, estimate, least uint64) {
	t.Helper()
	data := contract.pack(t, method, args...)
	estimate, err := self.estimate(from.address, to, value, data)
	if err != nil {
		t.Fatalf("%s: %s", method, reason(err))
	}
	least = self.least(t, ethereum.CallMsg{From: from.address, To: &to, Value: value, Data: data}, estimate,
		func(_ []byte, err error) bool { return err == nil })
	receipt = self.transact(t, from, &to, value, data, least)
	if receipt.Status != gethtypes.ReceiptStatusSuccessful {
		t.Fatalf("%s sent with its least limit %d failed in block %s", method, least, receipt.BlockNumber)
	}
	return receipt, estimate, least
}

// send calls method of contract at `to` in its own block and fails the test,
// naming the revert, unless it succeeded. value may be nil.
func (self *node) send(t *testing.T, from evmKey, to common.Address, contract artifact, value *big.Int, method string, args ...any) *gethtypes.Receipt {
	t.Helper()
	data := contract.pack(t, method, args...)
	gas, err := self.estimate(from.address, to, value, data)
	if err != nil {
		t.Fatalf("%s: %s", method, reason(err))
	}
	receipt := self.transact(t, from, &to, value, data, gas)
	if receipt.Status != gethtypes.ReceiptStatusSuccessful {
		t.Fatalf("%s failed in block %s", method, receipt.BlockNumber)
	}
	return receipt
}

// refused requires method to fail in a mined transaction and returns the
// reason an eth_call of it gives: a custom error of one of the contracts, or
// the node's message.
func (self *node) refused(t *testing.T, from evmKey, to common.Address, contract artifact, value *big.Int, method string, args ...any) string {
	t.Helper()
	data := contract.pack(t, method, args...)
	_, err := self.eth.CallContract(self.ctx, ethereum.CallMsg{From: from.address, To: &to, Value: value, Data: data}, nil)
	if err == nil {
		t.Fatalf("%s did not revert", method)
	}
	if receipt := self.transact(t, from, &to, value, data, refusedGas); receipt.Status != gethtypes.ReceiptStatusFailed {
		t.Fatalf("%s succeeded in block %s", method, receipt.BlockNumber)
	}
	return reason(err)
}

// read eth_calls a view of contract at `to`.
func (self *node) read(t *testing.T, to common.Address, contract artifact, method string, args ...any) []any {
	t.Helper()
	out, err := self.ethCall(common.Address{}, to, contract.pack(t, method, args...))
	if err != nil {
		t.Fatalf("%s: %s", method, reason(err))
	}
	return contract.unpack(t, method, out)
}

// reason names a revert by the custom error it encodes, from any loaded
// artifact, or returns the node's message.
func reason(err error) string {
	var reverted rpc.DataError
	if errors.As(err, &reverted) {
		if data, ok := reverted.ErrorData().(string); ok && len(data) >= 10 {
			forgeArtifacts.Lock()
			defer forgeArtifacts.Unlock()
			if name, ok := forgeArtifacts.errors[data[:10]]; ok {
				return name
			}
		}
	}
	return err.Error()
}

// requireEvent requires exactly one event `name` from `at` in receipt, with
// exactly the fields of want, indexed ones included.
func requireEvent(t *testing.T, receipt *gethtypes.Receipt, contract artifact, at common.Address, name string, want map[string]any) {
	t.Helper()
	event := contract.abi.Events[name]
	var found []map[string]any
	for _, log := range receipt.Logs {
		if log.Address != at || len(log.Topics) == 0 || log.Topics[0] != event.ID {
			continue
		}
		fields := map[string]any{}
		if err := event.Inputs.UnpackIntoMap(fields, log.Data); err != nil {
			t.Fatal(err)
		}
		var indexed abi.Arguments
		for _, input := range event.Inputs {
			if input.Indexed {
				indexed = append(indexed, input)
			}
		}
		if err := abi.ParseTopicsIntoMap(fields, indexed, log.Topics[1:]); err != nil {
			t.Fatal(err)
		}
		found = append(found, fields)
	}
	if len(found) != 1 {
		t.Fatalf("%d %s events, want 1", len(found), name)
	}
	if len(found[0]) != len(want) {
		t.Fatalf("%s has fields %v, want %v", name, found[0], want)
	}
	for key, value := range want {
		if fmt.Sprint(found[0][key]) != fmt.Sprint(value) {
			t.Fatalf("%s.%s = %v, want %v", name, key, found[0][key], value)
		}
	}
}

// fee returns the fee a transaction paid, in wei.
func fee(receipt *gethtypes.Receipt) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(receipt.GasUsed), receipt.EffectiveGasPrice)
}
