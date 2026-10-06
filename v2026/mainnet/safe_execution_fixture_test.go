// Published proxy and singleton code supply an independent in-memory oracle.
// Every key, account, transaction and block below is generated for tests only.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// These identities select maintained public artifacts, never actual accounts.
var safeExecutionTestProfiles = []struct{ version, variant string }{
	{version: "1.4.1", variant: "Safe"}, {version: "1.4.1", variant: "SafeL2"},
	{version: "1.5.0", variant: "Safe"}, {version: "1.5.0", variant: "SafeL2"},
}

// One fixture owns its state, separately parsed oracle ABI and synthetic keys.
// Public setup installs the owners; only the proxy pointer and nonce are seeded.
type safeExecutionFixture struct {
	t           *testing.T
	profile     *safeExecutionProfile
	oracleAbi   abi.ABI
	rawArtifact []byte
	state       *state.StateDB
	vm          runtime.Config
	transaction safeExecutionTransaction
	keys        []*ecdsa.PrivateKey
	owners      []common.Address
}

// The catalog includes both singleton variants, while the archive reader retains
// only the selected variant and proxy. Filter before accessing or decoding bytes.
func safeExecutionOracleArtifacts(t *testing.T, pin safeReleasePin, variant string, members map[string][]byte) ([]byte, safeReleaseArtifact, safeReleaseArtifact) {
	t.Helper()
	var rawSingleton []byte
	var singleton, proxy safeReleaseArtifact
	for _, artifactPin := range pin.Artifacts {
		if artifactPin.Name != variant && artifactPin.Name != "SafeProxy" {
			continue
		}
		raw := members[artifactPin.ArchivePath]
		if len(raw) == 0 {
			t.Fatalf("selected oracle artifact is absent: %s", artifactPin.Name)
		}
		var artifact safeReleaseArtifact
		if err := json.Unmarshal(raw, &artifact); err != nil {
			t.Fatalf("selected oracle artifact could not decode: %s: %v", artifactPin.Name, err)
		}
		if artifactPin.Name == "SafeProxy" {
			proxy = artifact
		} else {
			rawSingleton, singleton = slices.Clone(raw), artifact
		}
	}
	if len(rawSingleton) == 0 || singleton.Name != variant || proxy.Name != "SafeProxy" {
		t.Fatal("selected oracle artifact census differs")
	}
	return rawSingleton, singleton, proxy
}

// The oracle executes the pinned proxy, so its digest domain uses proxy storage
// and address rather than a copied Go implementation of the signing algorithm.
func newSafeExecutionFixture(t *testing.T, version, variant string) *safeExecutionFixture {
	t.Helper()
	pin, _, members := safeReleaseTestInputs(t, version, variant)
	self := &safeExecutionFixture{t: t}
	singleton := common.BytesToAddress(crypto.Keccak256([]byte("synthetic singleton " + version + variant)))
	proxy := common.BytesToAddress(crypto.Keccak256([]byte("synthetic Safe proxy")))
	var err error
	self.state, err = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	rawSingleton, singletonArtifact, proxyArtifact := safeExecutionOracleArtifacts(t, pin, variant, members)
	self.rawArtifact = rawSingleton
	self.state.SetCode(proxy, common.FromHex(proxyArtifact.Runtime), tracing.CodeChangeUnspecified)
	self.state.SetCode(singleton, common.FromHex(singletonArtifact.Runtime), tracing.CodeChangeUnspecified)
	self.oracleAbi, err = abi.JSON(bytes.NewReader(singletonArtifact.Abi))
	if err != nil {
		t.Fatal(err)
	}
	self.profile, err = newSafeExecutionProfile(version, variant, self.rawArtifact)
	if err != nil {
		t.Fatal(err)
	}
	self.state.SetState(proxy, common.Hash{}, common.BytesToHash(singleton[:]))
	for i := 0; i < 3; i++ {
		key, err := crypto.ToECDSA(crypto.Keccak256([]byte(fmt.Sprintf("synthetic pure Safe owner %d", i))))
		if err != nil {
			t.Fatal(err)
		}
		self.keys = append(self.keys, key)
	}
	slices.SortFunc(self.keys, func(a, b *ecdsa.PrivateKey) int {
		left, right := crypto.PubkeyToAddress(a.PublicKey), crypto.PubkeyToAddress(b.PublicKey)
		return bytes.Compare(left[:], right[:])
	})
	for _, key := range self.keys {
		self.owners = append(self.owners, crypto.PubkeyToAddress(key.PublicKey))
	}
	self.transaction = safeExecutionTransaction{ChainId: big.NewInt(43119), Safe: proxy,
		To: common.BytesToAddress(crypto.Keccak256([]byte("synthetic Safe target"))), Value: big.NewInt(0),
		Data: []byte("synthetic inner call"), SafeTxGas: big.NewInt(100000), BaseGas: big.NewInt(700),
		GasPrice: big.NewInt(0), Nonce: big.NewInt(17)}
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = new(big.Int).Set(self.transaction.ChainId)
	self.vm = runtime.Config{ChainConfig: &chainConfig, State: self.state, Origin: self.owners[2],
		BlockNumber: big.NewInt(38), Time: 128, GasLimit: 5_000_000, GasPrice: big.NewInt(2),
		Value: big.NewInt(0), BaseFee: big.NewInt(1)}
	setup, err := self.oracleAbi.Pack("setup", self.owners, big.NewInt(2), common.Address{}, []byte{},
		common.Address{}, common.Address{}, big.NewInt(0), common.Address{})
	if err != nil {
		t.Fatal(err)
	}
	if output, _, err := runtime.Call(proxy, setup, &self.vm); err != nil {
		t.Fatalf("published Safe setup failed: %v %x", err, output)
	}
	self.state.SetState(proxy, common.BigToHash(big.NewInt(5)), common.BigToHash(self.transaction.Nonce))
	self.state.SetCode(self.transaction.To, []byte{0x00}, tracing.CodeChangeUnspecified)
	return self
}

// Hashing calls the published singleton through its proxy without using the
// production helper's encoding or digest result.
func (self *safeExecutionFixture) oracleDigest(transaction safeExecutionTransaction) common.Hash {
	self.t.Helper()
	input, err := self.oracleAbi.Pack("getTransactionHash", transaction.To, transaction.Value, transaction.Data,
		transaction.Operation, transaction.SafeTxGas, transaction.BaseGas, transaction.GasPrice,
		transaction.GasToken, transaction.RefundReceiver, transaction.Nonce)
	if err != nil {
		self.t.Fatal(err)
	}
	config := self.vm
	chainConfig := *self.vm.ChainConfig
	chainConfig.ChainID = new(big.Int).Set(transaction.ChainId)
	config.ChainConfig = &chainConfig
	output, _, err := runtime.Call(transaction.Safe, input, &config)
	if err != nil || len(output) != 32 {
		self.t.Fatalf("published Safe digest failed: %v %x", err, output)
	}
	return common.BytesToHash(output)
}

// Only this test fixture creates signatures, using the oracle's digest and
// synthetic keys. High-s twins deliberately exercise Safe's ecrecover policy.
func (self *safeExecutionFixture) signatures(transaction safeExecutionTransaction, modes ...string) []byte {
	self.t.Helper()
	digest := self.oracleDigest(transaction)
	var result []byte
	for i, mode := range modes {
		hash := digest
		if mode == "eth-sign" {
			hash = crypto.Keccak256Hash([]byte("\x19Ethereum Signed Message:\n32"), hash[:])
		}
		raw, err := crypto.Sign(hash[:], self.keys[i])
		if err != nil {
			self.t.Fatal(err)
		}
		if mode == "high-s" {
			s := new(big.Int).Sub(crypto.S256().Params().N, new(big.Int).SetBytes(raw[32:64]))
			s.FillBytes(raw[32:64])
			raw[64] ^= 1
		}
		raw[64] += 27
		if mode == "eth-sign" {
			raw[64] += 4
		}
		result = append(result, raw...)
	}
	return result
}

// The first synthetic owner is a contract signature and the second remains an
// ordinary owner. The dynamic region intentionally starts without word padding.
func (self *safeExecutionFixture) contractSignatures(payload []byte) []byte {
	signatures := self.signatures(self.transaction, "raw", "raw")
	clear(signatures[:65])
	copy(signatures[12:32], self.owners[0][:])
	big.NewInt(130).FillBytes(signatures[32:64])
	length := make([]byte, 32)
	big.NewInt(int64(len(payload))).FillBytes(length)
	return append(append(signatures, length...), payload...)
}

// The two releases expose different primary signature-check methods. Selecting
// by full ABI signature avoids depending on geth's overloaded-method names.
func (self *safeExecutionFixture) oracleCheck(transaction safeExecutionTransaction, signatures []byte, executor common.Address) error {
	self.t.Helper()
	digest := self.oracleDigest(transaction)
	signature := "checkSignatures(address,bytes32,bytes)"
	arguments := []any{executor, digest, signatures}
	if self.profile.version == "1.4.1" {
		input, err := self.oracleAbi.Pack("encodeTransactionData", transaction.To, transaction.Value, transaction.Data,
			transaction.Operation, transaction.SafeTxGas, transaction.BaseGas, transaction.GasPrice,
			transaction.GasToken, transaction.RefundReceiver, transaction.Nonce)
		if err != nil {
			self.t.Fatal(err)
		}
		output, _, err := runtime.Call(transaction.Safe, input, &self.vm)
		if err != nil {
			self.t.Fatal(err)
		}
		values, err := self.oracleAbi.Unpack("encodeTransactionData", output)
		if err != nil || len(values) != 1 {
			self.t.Fatalf("published signing preimage failed: %v", err)
		}
		signature, arguments = "checkSignatures(bytes32,bytes,bytes)", []any{digest, values[0].([]byte), signatures}
	}
	for name, method := range self.oracleAbi.Methods {
		if method.Sig != signature {
			continue
		}
		input, err := self.oracleAbi.Pack(name, arguments...)
		if err != nil {
			self.t.Fatal(err)
		}
		config := self.vm
		config.Origin = executor
		_, _, err = runtime.Call(transaction.Safe, input, &config)
		return err
	}
	self.t.Fatalf("published signature-check method is absent: %s", signature)
	return nil
}

// Actual execution supplies logs and storage effects for the classifier; no
// production encoding helper participates in constructing this oracle call.
func (self *safeExecutionFixture) execute(transaction safeExecutionTransaction, signatures []byte) (safeExecutionReceipt, []byte, error) {
	self.t.Helper()
	input, err := self.oracleAbi.Pack("execTransaction", transaction.To, transaction.Value, transaction.Data,
		transaction.Operation, transaction.SafeTxGas, transaction.BaseGas, transaction.GasPrice,
		transaction.GasToken, transaction.RefundReceiver, signatures)
	if err != nil {
		self.t.Fatal(err)
	}
	hash := crypto.Keccak256Hash([]byte("synthetic outer Safe transaction"), input)
	blockHash := crypto.Keccak256Hash([]byte("synthetic Safe evidence block"))
	self.state.SetTxContext(hash, 0)
	output, _, executionError := runtime.Call(transaction.Safe, input, &self.vm)
	status := uint64(types.ReceiptStatusSuccessful)
	if executionError != nil {
		status = types.ReceiptStatusFailed
	}
	receipt := safeExecutionReceipt{TransactionHash: hash, BlockHash: blockHash, BlockNumber: 38,
		To: transaction.Safe, Status: status, Logs: self.state.GetLogs(hash, 38, blockHash, 128)}
	return receipt, output, executionError
}

// Each mutation gets its own dynamic values so the baseline cannot change.
func cloneSafeExecutionTransaction(transaction safeExecutionTransaction) safeExecutionTransaction {
	transaction.ChainId = new(big.Int).Set(transaction.ChainId)
	transaction.Value = new(big.Int).Set(transaction.Value)
	transaction.SafeTxGas = new(big.Int).Set(transaction.SafeTxGas)
	transaction.BaseGas = new(big.Int).Set(transaction.BaseGas)
	transaction.GasPrice = new(big.Int).Set(transaction.GasPrice)
	transaction.Nonce = new(big.Int).Set(transaction.Nonce)
	transaction.Data = slices.Clone(transaction.Data)
	return transaction
}

// Receipt mutations cannot share log topics or data with the original evidence.
func cloneSafeExecutionReceipt(receipt safeExecutionReceipt) safeExecutionReceipt {
	logs := make([]*types.Log, len(receipt.Logs))
	for i, entry := range receipt.Logs {
		log := *entry
		log.Topics, log.Data = slices.Clone(entry.Topics), slices.Clone(entry.Data)
		logs[i] = &log
	}
	receipt.Logs = logs
	return receipt
}
