// Contract recovery uses the submitting owned route for native and EVM reads.
// Native/EVM heights are independent; the reviewed Frontier insertion proves
// execution location. Finality remains an explicitly approved RPC assertion.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	native "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/crv4"
)

// The adapter borrows the already fenced HTTP route and cannot subscribe, sign
// or invoke an unbounded background read through the native library interface.
type evmNativeReadClient struct{ client *rpcClient }

// Storage absence is explicit; other native reads keep the existing allowlist.
func (self *evmNativeReadClient) CallContext(ctx context.Context, result any, method string, args ...any) error {
	if method == "state_getStorage" {
		return self.client.callBoundedRead(ctx, method, args, result, true, maximumRuntimeCodeRpcReplyBytes)
	}
	return self.client.call(ctx, method, args, result)
}

// Background calls cannot acquire authority without a bounded caller lifetime.
func (self *evmNativeReadClient) Call(any, string, ...any) error {
	return errors.New("EVM native reads require a context")
}

// The native bridge cannot create a subscription lifecycle.
func (self *evmNativeReadClient) Subscribe(context.Context, string, string, string, string, any, ...any) (*gsrpcgeth.ClientSubscription, error) {
	return nil, errors.New("EVM native reads cannot subscribe")
}

// Diagnostics contain no credentials or mutable endpoint discovery.
func (self *evmNativeReadClient) URL() string { return "owned-evm-native-read" }

// Transport ownership stays with the phase invocation.
func (self *evmNativeReadClient) Close() {}

// Each adapter is bound to one independently approved phase and route.
type evmOwnedChain struct {
	client     *rpcClient
	configHash string
}

// Construction adds no write to the ordinary read client's method profile.
func newEvmOwnedChain(config evmPhaseConfig) (*evmOwnedChain, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	client, err := newOwnedSubmissionClient(config.Plan.Route)
	if err != nil {
		return nil, err
	}
	return &evmOwnedChain{client: client, configHash: rootObjectHash(config)}, nil
}

// Only explicit EVM observations reach this additional bounded read profile.
func (self *evmOwnedChain) read(ctx context.Context, method string, params []any, result any) error {
	allowAbsent := false
	switch method {
	case "eth_getTransactionReceipt":
		allowAbsent = true
	case "eth_getTransactionByBlockHashAndIndex", "eth_getTransactionCount", "eth_getBalance", "eth_getCode", "eth_call", "eth_getStorageAt":
	default:
		return errors.New("method is outside the contract EVM read profile")
	}
	return self.client.callAdmittedRead(ctx, method, params, result, allowAbsent, maxRpcReplyBytes)
}

// Every quantity is canonical and width checked; missing fields never become
// plausible zero status, nonce, fee or position values after JSON decoding.
func evmQuantity(encoded string, bits int) (*big.Int, error) {
	value, err := hexutil.DecodeBig(encoded)
	if err != nil || value == nil || value.Sign() < 0 || value.BitLen() > bits || hexutil.EncodeBig(value) != encoded {
		return nil, errors.New("EVM quantity is missing, noncanonical or oversized")
	}
	return value, nil
}

// Receipt extraction requires the complete financial/position tuple while
// tolerating ordinary additional node fields. Duplicate fields are rejected.
func evmReceiptFacts(raw json.RawMessage, record evmActionRecord, plan evmCreatePlan) (evmCreateReceipt, uint64, error) {
	var result evmCreateReceipt
	if err := plan.validateSelection(); err != nil {
		return result, 0, err
	}
	var fields map[string]json.RawMessage
	if err := decodePlanJson(raw, &fields); err != nil {
		return result, 0, err
	}
	textField := func(name string) (string, error) {
		var value string
		part, ok := fields[name]
		if !ok || json.Unmarshal(part, &value) != nil || value == "" {
			return "", fmt.Errorf("EVM receipt lacks %s", name)
		}
		return value, nil
	}
	for _, field := range []struct {
		name   string
		target *string
	}{{name: "transactionHash", target: &result.TransactionHash}, {name: "blockHash", target: &result.BlockHash}, {name: "contractAddress", target: &result.ContractAddress}} {
		value, err := textField(field.name)
		if field.name == "contractAddress" && bytes.Equal(fields[field.name], []byte("null")) {
			value = ""
			err = nil
		}
		if err != nil {
			return result, 0, err
		}
		*field.target = value
	}
	values := map[string]*big.Int{}
	for _, name := range []string{"blockNumber", "transactionIndex", "status", "gasUsed", "effectiveGasPrice"} {
		encoded, err := textField(name)
		if err != nil {
			return result, 0, err
		}
		bits := 64
		if name == "effectiveGasPrice" {
			bits = 256
		}
		value, err := evmQuantity(encoded, bits)
		if err != nil {
			return result, 0, err
		}
		values[name] = value
	}
	result.BlockNumber = values["blockNumber"].Uint64()
	result.Status = values["status"].Uint64()
	result.GasUsed = values["gasUsed"].Uint64()
	result.EffectiveGasPrice = values["effectiveGasPrice"].String()
	if result.ContractAddress != "" {
		if !common.IsHexAddress(result.ContractAddress) {
			return result, 0, errors.New("EVM receipt contract address is malformed")
		}
		address := common.HexToAddress(result.ContractAddress)
		result.ContractAddress = address.Hex()
		if address == (common.Address{}) {
			result.ContractAddress = ""
		}
	}
	action := plan.Config.Plan.Actions[plan.ActionIndex]
	fee, _ := evmWei(action.FeeCapWei)
	expectedAddress := plan.Address.Hex()
	if plan.ActionIndex == 3 || plan.ActionIndex == 5 || plan.ActionIndex == 6 {
		expectedAddress = ""
		if !bytes.Equal(fields["contractAddress"], []byte("null")) {
			return result, 0, errors.New("contract call receipt requires a null CREATE address")
		}
		to, toErr := textField("to")
		from, fromErr := textField("from")
		if toErr != nil || fromErr != nil || !common.IsHexAddress(to) || !common.IsHexAddress(from) || common.HexToAddress(to) != *action.To || common.HexToAddress(from) != action.Sender {
			return result, 0, errors.New("contract call receipt sender or target differs")
		}
	}
	if plan.ActionIndex == 4 {
		from, fromErr := textField("from")
		if !bytes.Equal(fields["to"], []byte("null")) || fromErr != nil || !common.IsHexAddress(from) || common.HexToAddress(from) != action.Sender {
			return result, 0, errors.New("proxy receipt sender or CREATE target differs")
		}
	}
	if result.TransactionHash != record.TransactionHash || !rootCanonicalHash(result.BlockHash) || result.BlockNumber == 0 || result.Status > 1 || result.GasUsed == 0 || result.GasUsed > action.Gas || values["effectiveGasPrice"].Cmp(fee) > 0 || result.Status == 1 && result.ContractAddress != expectedAddress || result.Status == 0 && result.ContractAddress != "" {
		return result, 0, errors.New("EVM receipt contradicts the original CREATE")
	}
	if plan.ActionIndex == 3 {
		if err := evmEscrowReceiptEvent(fields, &result, values["transactionIndex"].Uint64(), plan); err != nil {
			return result, 0, err
		}
	}
	if plan.ActionIndex == 5 {
		if err := evmReserveLinkReceiptEvent(fields, &result, values["transactionIndex"].Uint64(), plan); err != nil {
			return result, 0, err
		}
	}
	if plan.ActionIndex == 6 {
		if err := evmVaultLinkReceiptEvent(fields, &result, values["transactionIndex"].Uint64(), plan); err != nil {
			return result, 0, err
		}
	}
	if plan.ActionIndex == 7 {
		if err := evmEvidenceCreateReceipt(fields, plan); err != nil {
			return result, 0, err
		}
	}
	return result, values["transactionIndex"].Uint64(), nil
}

// Native canonical continuity is checked before scanning and again after all
// receipt reads. The approval's initial head can never be replaced by a restart.
func (self *evmOwnedChain) continuity(ctx context.Context, p evmPhasePlan, record evmActionRecord, head chainIdentity) error {
	expected := identityExpectation{NativeChain: p.Network.NativeChain, GenesisHash: p.Network.GenesisHash, EvmChainId: p.Network.EvmChainId}
	if err := expected.match(head); err != nil {
		return err
	}
	if head.FinalizedNumber < record.ScanNumber {
		return errors.New("EVM recovery finalized head regressed")
	}
	for _, point := range []struct {
		number uint64
		hash   string
	}{{number: p.StartNativeNumber, hash: p.StartNativeHash}, {number: record.ScanNumber, hash: record.ScanHash}} {
		var hash string
		if err := self.client.call(ctx, "chain_getBlockHash", []any{point.number}, &hash); err != nil {
			return err
		}
		if hash != point.hash {
			return errors.New("EVM recovery canonical native ancestry changed")
		}
	}
	return nil
}

// Each call scans at most 128 new native headers. The persisted cursor is only
// an optimization; its canonical hash and parent links are rechecked on resume.
func (self *evmOwnedChain) locate(ctx context.Context, head chainIdentity, record evmActionRecord, receipt evmCreateReceipt) (evmActionObservation, error) {
	result := evmActionObservation{Status: "receipt-awaiting-finalized-mapping", ScanNumber: record.ScanNumber, ScanHash: record.ScanHash}
	if record.Receipt != nil {
		if record.Receipt.BlockHash != receipt.BlockHash || record.Receipt.BlockNumber != receipt.BlockNumber {
			return result, errors.New("EVM receipt moved after finalization")
		}
		result.Receipt = &receipt
		result.Receipt.NativeHash = record.Receipt.NativeHash
		result.Receipt.NativeNumber = record.Receipt.NativeNumber
		return result, nil
	}
	for count := 0; count < 128 && result.ScanNumber < head.FinalizedNumber; count++ {
		var hash string
		var header rootReceiptHeader
		if err := self.client.call(ctx, "chain_getBlockHash", []any{result.ScanNumber + 1}, &hash); err != nil {
			return result, err
		}
		if err := self.client.call(ctx, "chain_getHeader", []any{hash}, &header); err != nil {
			return result, err
		}
		number, err := header.authenticate(hash)
		if err != nil || number != result.ScanNumber+1 || header.ParentHash != result.ScanHash {
			return result, errors.Join(errors.New("EVM recovery native parent link differs"), err)
		}
		post, err := finalizedFrontierPostLog(header)
		if err != nil {
			return result, err
		}
		result.ScanNumber, result.ScanHash = number, hash
		if post.BlockHash == receipt.BlockHash {
			receipt.NativeHash, receipt.NativeNumber = hash, number
			result.Receipt = &receipt
			return result, nil
		}
	}
	return result, ctx.Err()
}

// Historical runtime authority is evaluated at the actual inclusion and its
// parent, so an unrelated later upgrade does not erase an original receipt.
func (self *evmOwnedChain) authenticateReceipt(ctx context.Context, plan evmCreatePlan, record evmActionRecord, receipt evmCreateReceipt, index uint64) (evmCreateReceipt, error) {
	p := plan.Config.Plan
	identity, err := self.client.readIdentityAt(ctx, receipt.NativeHash)
	if err != nil {
		return receipt, err
	}
	if err := self.continuity(ctx, p, evmActionRecord{ScanNumber: receipt.NativeNumber, ScanHash: receipt.NativeHash}, identity); err != nil {
		return receipt, err
	}
	mapping, err := self.client.readFinalizedMappingAtIdentity(ctx, identity)
	if err != nil {
		return receipt, err
	}
	if mapping.EvmHeader.Hash != receipt.BlockHash || mapping.EvmHeader.Number != receipt.BlockNumber {
		return receipt, errors.New("EVM receipt does not match exact native commitment")
	}
	genesis, _ := native.NewHashFromHexString(p.Network.GenesisHash)
	nativeHash, _ := native.NewHashFromHexString(receipt.NativeHash)
	evmHash, _ := native.NewHashFromHexString(receipt.BlockHash)
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: &evmNativeReadClient{client: self.client}}, GenesisHash: genesis}
	profile := p.Runtime
	_, err = crv4.ReadEVMCheckpointAtContext(ctx, chain, crv4.EVMCheckpointQuery{GenesisHash: genesis, NativeHash: nativeHash, NativeNumber: receipt.NativeNumber, EVMHash: evmHash, EVMNumber: receipt.BlockNumber}, crv4.RuntimeArtifactIdentity{Version: profile.RuntimeVersion, CodeHash: profile.RuntimeCodeHash, MetadataHash: profile.RuntimeMetadataHash})
	if err != nil {
		return receipt, err
	}
	var tx types.Transaction
	if err := self.read(ctx, "eth_getTransactionByBlockHashAndIndex", []any{receipt.BlockHash, fmt.Sprintf("0x%x", index)}, &tx); err != nil {
		return receipt, err
	}
	raw, err := tx.MarshalBinary()
	if err != nil || "0x"+hex.EncodeToString(raw) != record.Signed {
		return receipt, errors.Join(errors.New("canonical EVM position does not contain original signed bytes"), err)
	}
	// Variant1's native transaction vector independently commits the position.
	if mapping.PostLog.Variant == 1 && (index >= uint64(len(mapping.PostLog.TransactionHashes)) || mapping.PostLog.TransactionHashes[index] != record.TransactionHash) {
		return receipt, errors.New("EVM transaction differs from native committed vector")
	}
	block := map[string]any{"blockHash": receipt.BlockHash, "requireCanonical": true}
	if receipt.Status == 1 {
		var code string
		if err := self.read(ctx, "eth_getCode", []any{plan.Address.Hex(), block}, &code); err != nil {
			return receipt, err
		}
		if code != "0x"+hex.EncodeToString(plan.Runtime) {
			return receipt, errors.New("created contract runtime differs from exact patched release")
		}
		getters, err := plan.receiptGetters(receipt)
		if err != nil {
			return receipt, err
		}
		for _, getter := range getters {
			var output string
			if err := self.read(ctx, "eth_call", []any{map[string]any{"to": plan.Address.Hex(), "data": getter.Data}, block}, &output); err != nil {
				return receipt, err
			}
			if output != getter.Expected {
				return receipt, errors.New("created contract constructor getter differs")
			}
		}
		for _, word := range plan.Storage {
			var output string
			if err := self.read(ctx, "eth_getStorageAt", []any{plan.Address.Hex(), word.Slot, block}, &output); err != nil {
				return receipt, err
			}
			if output != word.Expected {
				return receipt, errors.New("created implementation constructor storage differs")
			}
		}
		receipt.RuntimeHash = crypto.Keccak256Hash(plan.Runtime).Hex()
		receipt.GetterHash = rootObjectHash(getters)
		if len(plan.Storage) != 0 {
			receipt.StorageHash = rootObjectHash(plan.Storage)
		}
		if plan.ActionIndex == 3 {
			if err := self.authenticateEscrowRegistration(ctx, plan, receipt, block); err != nil {
				return receipt, err
			}
			receipt.RegistrationHash = evmEscrowRegistrationHash(plan, receipt)
		}
		if plan.ActionIndex == 4 {
			if err := self.authenticateProxyImplementation(ctx, plan, block); err != nil {
				return receipt, err
			}
		}
		if plan.ActionIndex == 5 {
			if err := self.authenticateReserveRecorder(ctx, plan, block); err != nil {
				return receipt, err
			}
			receipt.RecorderBindingHash = evmReserveBindingHash(plan, receipt)
		}
		if plan.ActionIndex == 6 {
			if err := self.authenticateVaultCoordinator(ctx, plan, block); err != nil {
				return receipt, err
			}
			receipt.VaultBindingHash = evmVaultBindingHash(plan, receipt)
		}
		if plan.ActionIndex == 7 {
			if err := self.authenticateEvidenceDependencies(ctx, plan, block); err != nil {
				return receipt, err
			}
		}
	}
	// Re-read both canonical mappings after contract and transaction observations.
	if _, err := self.client.readFinalizedMappingAtIdentity(ctx, identity); err != nil {
		return receipt, err
	}
	return receipt, ctx.Err()
}

// No absent receipt can erase a previously retained canonical outcome. Nonce
// movement keeps the original liability unresolved instead of buying a new nonce.
func (self *evmOwnedChain) reconcile(ctx context.Context, plan evmCreatePlan, record evmActionRecord) (evmActionObservation, error) {
	result := evmActionObservation{ScanNumber: record.ScanNumber, ScanHash: record.ScanHash}
	if ctx == nil || self.configHash != rootObjectHash(plan.Config) || self.client.url != plan.Config.Plan.Route.RpcUrl || record.Signed == "" {
		return result, errors.New("EVM adapter scope differs")
	}
	if err := plan.validateSelection(); err != nil {
		return result, err
	}
	if err := record.validateForAction(plan.Config, plan.ActionIndex); err != nil {
		return result, err
	}
	if err := validateEvmCreatePrerequisite(plan, record); err != nil {
		return result, err
	}
	p := plan.Config.Plan
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.Route.ReadRetrySeconds)*time.Second)
	defer cancel()
	head, err := self.client.readIdentity(ctx)
	if err != nil {
		return result, err
	}
	if err := self.continuity(ctx, p, record, head); err != nil {
		return result, err
	}
	for _, prior := range plan.Prerequisites {
		if err := self.continuity(ctx, p, prior, head); err != nil {
			return result, err
		}
	}
	var raw json.RawMessage
	if err := self.read(ctx, "eth_getTransactionReceipt", []any{record.TransactionHash}, &raw); err != nil {
		return result, err
	}
	if !bytes.Equal(raw, []byte("null")) {
		receipt, index, err := evmReceiptFacts(raw, record, plan)
		if err != nil {
			return result, err
		}
		result, err = self.locate(ctx, head, record, receipt)
		if err != nil {
			return result, err
		}
		if result.Receipt != nil {
			authenticated, err := self.authenticateReceipt(ctx, plan, record, *result.Receipt, index)
			if err != nil {
				return result, err
			}
			for _, prior := range plan.Prerequisites {
				if authenticated.NativeNumber < prior.Receipt.NativeNumber || authenticated.BlockNumber < prior.Receipt.BlockNumber {
					return result, errors.New("CREATE inclusion precedes its prerequisite")
				}
			}
			result.Receipt = &authenticated
			result.Status = plan.completedStatus()
			if authenticated.Status == 0 {
				result.Status = plan.revertedStatus()
			}
		}
		return result, ctx.Err()
	}
	if record.Receipt != nil {
		return result, errors.New("retained canonical EVM receipt disappeared")
	}
	result, err = self.admitCurrent(ctx, plan, record, head)
	if err != nil || !result.SendReady {
		return result, err
	}
	check, err := self.client.readIdentity(ctx)
	if err != nil {
		return result, err
	}
	if check.FinalizedNumber < head.FinalizedNumber || check.FinalizedNumber == head.FinalizedNumber && check.FinalizedHash != head.FinalizedHash {
		return result, errors.New("EVM finalized ancestry regressed during admission")
	}
	if err := self.continuity(ctx, p, evmActionRecord{ScanNumber: head.FinalizedNumber, ScanHash: head.FinalizedHash}, check); err != nil {
		return result, err
	}
	for _, prior := range plan.Prerequisites {
		if err := self.continuity(ctx, p, prior, check); err != nil {
			return result, err
		}
	}
	if check.FinalizedHash != head.FinalizedHash {
		// A healthy advancing head refreshes only affected current authority.
		// This bounded second observation never asks the chain to stop moving.
		return self.admitCurrent(ctx, plan, record, check)
	}
	if check.runtimeVersion != head.runtimeVersion {
		return result, errors.New("EVM runtime tuple changed at the same native hash")
	}
	return result, ctx.Err()
}

// One current observation binds runtime and canonical state to a selected head,
// plus pending nonce/balance/collision reads. It is not an inclusion deadline.
func (self *evmOwnedChain) admitCurrent(ctx context.Context, plan evmCreatePlan, record evmActionRecord, head chainIdentity) (evmActionObservation, error) {
	result := evmActionObservation{Status: "signed-awaiting-admission", ScanNumber: record.ScanNumber, ScanHash: record.ScanHash}
	p := plan.Config.Plan
	if head.FinalizedNumber > p.ValidThroughNative {
		result.Status = "approval-expired-signed-liability-retained"
		return result, nil
	}
	if head.FinalizedNumber < p.StartNativeNumber {
		return result, errors.New("EVM head precedes approved custody checkpoint")
	}
	runtime, err := self.client.readRuntimeSnapshotAtIdentity(ctx, head)
	if err != nil {
		return result, err
	}
	if runtime.Version != p.Runtime.RuntimeVersion || runtime.CodeHash != p.Runtime.RuntimeCodeHash || runtime.MetadataHash != p.Runtime.RuntimeMetadataHash {
		return result, errors.New("EVM current runtime differs from independent phase approval")
	}
	mapping, err := self.client.readFinalizedMappingAtIdentity(ctx, head)
	if err != nil {
		return result, err
	}
	block := map[string]any{"blockHash": mapping.EvmHeader.Hash, "requireCanonical": true}
	action := p.Actions[plan.ActionIndex]
	var confirmed, pending, balance, code string
	for _, read := range []struct {
		method string
		params []any
		target *string
	}{{method: "eth_getTransactionCount", params: []any{action.Sender.Hex(), block}, target: &confirmed}, {method: "eth_getTransactionCount", params: []any{action.Sender.Hex(), "pending"}, target: &pending}, {method: "eth_getBalance", params: []any{action.Sender.Hex(), "pending"}, target: &balance}, {method: "eth_getCode", params: []any{plan.Address.Hex(), "pending"}, target: &code}} {
		if err := self.read(ctx, read.method, read.params, read.target); err != nil {
			return result, err
		}
	}
	confirmedNonce, e1 := evmQuantity(confirmed, 64)
	pendingNonce, e2 := evmQuantity(pending, 64)
	available, e3 := evmQuantity(balance, 256)
	if err := errors.Join(e1, e2, e3); err != nil {
		return result, err
	}
	if confirmedNonce.Uint64() > action.Nonce {
		result.Status = "nonce-consumed-receipt-unresolved"
		return result, nil
	}
	if confirmedNonce.Uint64() < action.Nonce {
		result.Status = "predecessor-nonce-unresolved"
		return result, nil
	}
	if pendingNonce.Uint64() != action.Nonce {
		result.Status = "pending-nonce-unresolved"
		return result, nil
	}
	if plan.ActionIndex == 3 {
		if err := self.admitEscrowTarget(ctx, plan, block, code); err != nil {
			return result, err
		}
	} else if plan.ActionIndex == 5 {
		if err := self.admitReserveLinkTarget(ctx, plan, block, code); err != nil {
			return result, err
		}
	} else if plan.ActionIndex == 6 {
		if err := self.admitVaultLinkTarget(ctx, plan, block, code); err != nil {
			return result, err
		}
	} else if code != "0x" {
		return result, errors.New("CREATE address already has pending code without its canonical receipt")
	}
	if plan.ActionIndex == 7 {
		if err := self.admitEvidenceCreate(ctx, plan, block); err != nil {
			return result, err
		}
	}
	tx, _ := action.unsigned()
	cost := new(big.Int).Add(tx.Value(), new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), tx.GasFeeCap()))
	if plan.ActionIndex > 0 {
		// Later sealed reservations still own this sender's funds. They do not
		// become executable or acquire new nonce/fee authority through this read.
		for _, reserved := range p.Actions[plan.ActionIndex+1:] {
			if reserved.Sender == action.Sender {
				future, err := reserved.unsigned()
				if err != nil {
					return result, err
				}
				cost.Add(cost, new(big.Int).Add(future.Value(), new(big.Int).Mul(new(big.Int).SetUint64(future.Gas()), future.GasFeeCap())))
			}
		}
	}
	if available.Cmp(cost) < 0 {
		return result, errors.New("EVM balance cannot cover original maximum liability")
	}
	result.Status = "exact-original-transaction-admitted"
	result.SendReady = true
	return result, ctx.Err()
}

// A durable attempt is required before the one permitted EVM write. This
// method never retries, reprices, changes nonce or signs another transaction.
func (self *evmOwnedChain) submit(ctx context.Context, plan evmCreatePlan, record evmActionRecord) error {
	if self.configHash != rootObjectHash(plan.Config) || record.Attempts == 0 {
		return errors.New("EVM write lacks retained phase scope or attempt")
	}
	if err := plan.validateSelection(); err != nil {
		return err
	}
	if err := record.validateForAction(plan.Config, plan.ActionIndex); err != nil {
		return err
	}
	if err := validateEvmCreatePrerequisite(plan, record); err != nil {
		return err
	}
	_, err := ownedSubmissionPost(ctx, self.client, plan.Config.Plan.Route, "eth_sendRawTransaction", record.Signed, record.TransactionHash)
	return err
}
