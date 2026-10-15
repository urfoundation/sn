// One EVM role reads bounded, complete blocks. Transaction and receipt tries
// prevent an omitted event from masquerading as an empty successful sample.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The mapping preface has its own existing response bounds. This budget covers
// every retained block body, receipt, code and getter reply in the economic page.
type monitorEvmReader struct {
	entitlementObserver *economicEntitlementReceiptObserver
	client              *rpcClient
	policy              monitorEconomicEvmPolicy
	contract            *abi.ABI
	used                int
}

type monitorEvmBlock struct {
	Boundary         economicEmissionBoundary   `json:"boundary"`
	ParentHash       string                     `json:"parent_hash"`
	TransactionsRoot string                     `json:"transactions_root"`
	ReceiptsRoot     string                     `json:"receipts_root"`
	Events           []monitorEconomicEvmEvent  `json:"events"`
	Fees             []monitorEconomicEvmFee    `json:"fees"`
	Snapshot         monitorEconomicEvmSnapshot `json:"snapshot"`
	Funded           map[string]string          `json:"entitlement_funded"`
}

type monitorEvmObservation struct {
	From             economicEmissionBoundary
	RequestedThrough economicEmissionBoundary
	Finalized        economicEmissionBoundary
	MappingHash      string
	Before           monitorEconomicEvmSnapshot
	Blocks           []monitorEvmBlock
}

func monitorEvmAbi(kind string) (*abi.ABI, error) {
	if kind == "settlement-vault" {
		return stabi.STSettlementVaultMetaData.ParseABI()
	}
	if kind == "reserve-sink" {
		return stabi.STReserveSinkMetaData.ParseABI()
	}
	return nil, errors.New("unknown economic contract ABI purpose")
}

func monitorEvmIntegrity(message string) error {
	return fmt.Errorf("%w: %s", errRpcIntegrity, message)
}

// An input array is bounded before allocating its element census. Every element
// still has the enclosing RPC response byte bound and unique-key validation.
func monitorEvmArray(raw json.RawMessage, maximum int) ([]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('[') {
		return nil, monitorEvmIntegrity("economic RPC array is absent or malformed")
	}
	values := []json.RawMessage{}
	for decoder.More() {
		if len(values) == maximum {
			return nil, errMonitorEconomicCapacity
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errors.Join(errRpcIntegrity, err)
		}
		values = append(values, value)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, monitorEvmIntegrity("economic RPC array has trailing data")
	}
	return values, nil
}

func (self *monitorEvmReader) read(ctx context.Context, method string, params []any, result any) error {
	var raw json.RawMessage
	var err error
	if method == "eth_getBlockByHash" || method == "eth_getBlockByNumber" {
		err = self.client.callFinalizedMappingRead(ctx, method, params, &raw)
	} else {
		err = self.client.callEvmRead(ctx, method, params, &raw)
	}
	if err != nil {
		return err
	}
	if len(raw) > maximumMonitorEvmReadBytes-self.used {
		return errMonitorEconomicCapacity
	}
	self.used += len(raw)
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("%w: economic %s observation is absent", errFinalizedMappingUnavailable, method)
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return errors.Join(errRpcIntegrity, err)
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return errors.Join(errRpcIntegrity, err)
	}
	return ctx.Err()
}

// The RLP hash authenticates header fields; the independent number lookup is
// only an owned-RPC canonicality assertion, explicitly retained as that trust.
func (self *monitorEvmReader) header(ctx context.Context, number uint64) (*types.Header, error) {
	var value struct {
		Hash   string `json:"hash"`
		Number string `json:"number"`
	}
	if err := self.read(ctx, "eth_getBlockByNumber", []any{hexutil.EncodeUint64(number), false}, &value); err != nil {
		return nil, err
	}
	n, err := evmQuantity(value.Number, 64)
	if err != nil || n.Uint64() != number || !rootCanonicalHash(value.Hash) {
		return nil, monitorEvmIntegrity("economic canonical block has another position")
	}
	mapped, err := self.client.readMappedEvmHeader(ctx, value.Hash)
	if err != nil {
		return nil, err
	}
	if len(mapped.HeaderRlp) > maximumMonitorEvmReadBytes-self.used {
		return nil, errMonitorEconomicCapacity
	}
	self.used += len(mapped.HeaderRlp)
	raw, err := hexutil.Decode(mapped.HeaderRlp)
	if err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	var header types.Header
	if err := rlp.DecodeBytes(raw, &header); err != nil || header.Number == nil || !header.Number.IsUint64() || header.Number.Uint64() != number || header.Hash().Hex() != value.Hash {
		return nil, monitorEvmIntegrity("economic block RLP differs from its original identity")
	}
	return &header, nil
}

// All state calls use a hash with requireCanonical, never a changing height.
func (self *monitorEvmReader) getter(ctx context.Context, hash, name string, args ...any) ([]any, error) {
	input, err := self.contract.Pack(name, args...)
	if err != nil {
		return nil, err
	}
	var encoded string
	if err := self.read(ctx, "eth_call", []any{map[string]any{"to": self.policy.Address, "data": hexutil.Encode(input)}, map[string]any{"blockHash": hash, "requireCanonical": true}}, &encoded); err != nil {
		return nil, err
	}
	raw, err := hexutil.Decode(encoded)
	if err != nil || len(raw) > 1024 {
		return nil, monitorEvmIntegrity("economic getter has malformed or oversized ABI data")
	}
	method := self.contract.Methods[name]
	values, err := method.Outputs.Unpack(raw)
	if err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	canonical, err := method.Outputs.Pack(values...)
	if err != nil || !bytes.Equal(raw, canonical) {
		return nil, monitorEvmIntegrity("economic getter has noncanonical ABI output")
	}
	return values, nil
}

func monitorEvmScalar(value any) (string, error) {
	switch value := value.(type) {
	case *big.Int:
		if value == nil || value.Sign() < 0 || value.BitLen() > 256 {
			return "", monitorEvmIntegrity("economic ABI amount exceeds uint256")
		}
		return value.String(), nil
	case uint8:
		return fmt.Sprint(value), nil
	case uint16:
		return fmt.Sprint(value), nil
	case uint64:
		return fmt.Sprint(value), nil
	case bool:
		if value {
			return "true", nil
		}
		return "false", nil
	case common.Address:
		return value.Hex(), nil
	case [32]byte:
		return common.Hash(value).Hex(), nil
	default:
		return "", monitorEvmIntegrity("economic ABI scalar is outside the reviewed grammar")
	}
}

func (self *monitorEvmReader) snapshot(ctx context.Context, hash string) (monitorEconomicEvmSnapshot, error) {
	result := monitorEconomicEvmSnapshot{Counters: map[string]string{}, Pools: map[string]string{}, Credits: map[string]string{}}
	var code string
	if err := self.read(ctx, "eth_getCode", []any{self.policy.Address, map[string]any{"blockHash": hash, "requireCanonical": true}}, &code); err != nil {
		return result, err
	}
	raw, err := hexutil.Decode(code)
	if err != nil || len(raw) == 0 || crypto.Keccak256Hash(raw).Hex() != self.policy.CodeHash {
		return result, monitorEvmIntegrity("economic contract code differs from independently reviewed artifact")
	}
	read := func(name string, args ...any) (string, error) {
		values, err := self.getter(ctx, hash, name, args...)
		if err != nil {
			return "", err
		}
		if len(values) != 1 {
			return "", monitorEvmIntegrity("economic getter arity differs")
		}
		return monitorEvmScalar(values[0])
	}
	netuid, err := read("netuid")
	if err != nil {
		return result, err
	}
	if netuid != fmt.Sprint(self.policy.Netuid) {
		return result, monitorEvmIntegrity("economic contract subnet identity differs")
	}
	names := []string{"principal", "liveStake"}
	poolMethod := "operatorPrincipal"
	if self.policy.ContractKind == "settlement-vault" {
		names = []string{"totalCaptured", "totalPaid", "pendingFunding", "outstandingLiability", "escrowAccounted", "liveEscrowStake"}
		poolMethod = "carry"
	}
	for _, name := range names {
		value, err := read(name)
		if err != nil {
			return result, err
		}
		result.Counters[name] = value
	}
	for _, id := range self.policy.PoolIds {
		n, _ := monitorEconomicInteger(id)
		value, err := read(poolMethod, n)
		if err != nil {
			return result, err
		}
		result.Pools[id] = value
	}
	for _, coldkey := range self.policy.Coldkeys {
		value, err := read("claimCredit", [32]byte(common.HexToHash(coldkey)))
		if err != nil {
			return result, err
		}
		result.Credits[coldkey] = value
	}
	if err := result.validate(self.policy); err != nil {
		return result, errors.Join(errRpcIntegrity, err)
	}
	if err := result.conservation(self.policy); err != nil {
		return result, err
	}
	return result, nil
}

// Complete topic/data round trips refuse aliasing, extra indexed fields and
// malformed padding. Event names come only from the reviewed generated ABI.
func (self *monitorEvmReader) event(log *types.Log, receiptHash string) (monitorEconomicEvmEvent, error) {
	result := monitorEconomicEvmEvent{}
	if log == nil || len(log.Topics) == 0 {
		return result, monitorEvmIntegrity("economic log lacks its ABI topic")
	}
	event, err := self.contract.EventByID(log.Topics[0])
	if err != nil {
		return result, monitorEvmIntegrity("economic contract emitted an unreviewed event topic")
	}
	indexed := abi.Arguments{}
	for _, field := range event.Inputs {
		if field.Indexed {
			indexed = append(indexed, field)
		}
	}
	if len(log.Topics) != len(indexed)+1 {
		return result, monitorEvmIntegrity("economic event indexed census differs")
	}
	values := map[string]any{}
	if err := abi.ParseTopicsIntoMap(values, indexed, log.Topics[1:]); err != nil {
		return result, errors.Join(errRpcIntegrity, err)
	}
	nonIndexed := event.Inputs.NonIndexed()
	unpacked, err := nonIndexed.Unpack(log.Data)
	if err != nil {
		return result, errors.Join(errRpcIntegrity, err)
	}
	encoded, err := nonIndexed.Pack(unpacked...)
	if err != nil || !bytes.Equal(encoded, log.Data) {
		return result, monitorEvmIntegrity("economic event ABI payload is noncanonical")
	}
	for index, field := range nonIndexed {
		values[field.Name] = unpacked[index]
	}
	for index, field := range indexed {
		// Indexed scalar encoding is one canonical 32-byte ABI word.
		plain := field
		plain.Indexed = false
		encoded, err := (abi.Arguments{plain}).Pack(values[field.Name])
		if err != nil || !bytes.Equal(encoded, log.Topics[index+1][:]) {
			return result, monitorEvmIntegrity("economic event indexed word is noncanonical")
		}
	}
	result = monitorEconomicEvmEvent{Block: economicEmissionBoundary{Number: log.BlockNumber, Hash: log.BlockHash.Hex()}, TransactionHash: log.TxHash.Hex(), TransactionIndex: uint64(log.TxIndex), LogIndex: uint64(log.Index), Name: event.Name, Values: map[string]string{}, ReceiptHash: receiptHash}
	for name, value := range values {
		text, err := monitorEvmScalar(value)
		if err != nil {
			return monitorEconomicEvmEvent{}, err
		}
		result.Values[name] = text
	}
	return result, nil
}

// Receipt annotations are checked against signed transaction bytes and the
// receipt trie. Effective gas price remains an explicitly labelled RPC field;
// the legacy Frontier header does not commit a London base-fee annotation.
func (self *monitorEvmReader) block(ctx context.Context, header *types.Header) (monitorEvmBlock, error) {
	result := monitorEvmBlock{Boundary: economicEmissionBoundary{Number: header.Number.Uint64(), Hash: header.Hash().Hex()}, ParentHash: header.ParentHash.Hex(), TransactionsRoot: header.TxHash.Hex(), ReceiptsRoot: header.ReceiptHash.Hex(), Events: []monitorEconomicEvmEvent{}, Fees: []monitorEconomicEvmFee{}, Funded: map[string]string{}}
	var body struct {
		Hash         string          `json:"hash"`
		Number       string          `json:"number"`
		Transactions json.RawMessage `json:"transactions"`
	}
	if err := self.read(ctx, "eth_getBlockByHash", []any{result.Boundary.Hash, true}, &body); err != nil {
		return result, err
	}
	if body.Hash != result.Boundary.Hash || body.Number != hexutil.EncodeUint64(result.Boundary.Number) {
		return result, monitorEvmIntegrity("economic full block identity differs")
	}
	encoded, err := monitorEvmArray(body.Transactions, maximumMonitorEvmTransactions)
	if err != nil {
		return result, err
	}
	transactions := make(types.Transactions, len(encoded))
	for index, raw := range encoded {
		var tx types.Transaction
		if err := json.Unmarshal(raw, &tx); err != nil {
			return result, errors.Join(errRpcIntegrity, err)
		}
		if tx.Type() > types.DynamicFeeTxType {
			return result, fmt.Errorf("%w: economic transaction type is outside reviewed legacy/access-list/dynamic-fee profile", errFinalizedMappingUnavailable)
		}
		if !tx.Protected() || tx.ChainId().Cmp(new(big.Int).SetUint64(self.policy.Network.EvmChainId)) != 0 {
			return result, monitorEvmIntegrity("economic block transaction has another chain authority")
		}
		transactions[index] = &tx
	}
	if types.DeriveSha(transactions, trie.NewStackTrie(nil)) != header.TxHash {
		return result, monitorEvmIntegrity("economic block transaction census differs from authenticated trie")
	}
	receipts := make(types.Receipts, len(transactions))
	var cumulative uint64
	var logIndex uint64
	for index, tx := range transactions {
		var raw json.RawMessage
		if err := self.read(ctx, "eth_getTransactionReceipt", []any{tx.Hash().Hex()}, &raw); err != nil {
			return result, err
		}
		decoded, err := decodeEvmReceiptFields(raw)
		if err != nil {
			return result, errors.Join(errRpcIntegrity, err)
		}
		logs, err := monitorEvmArray(decoded.fields["logs"], maximumMonitorEvmLogs-int(logIndex))
		if err != nil {
			return result, err
		}
		var receipt types.Receipt
		if err := json.Unmarshal(raw, &receipt); err != nil {
			return result, errors.Join(errRpcIntegrity, err)
		}
		sender, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
		if err != nil {
			return result, errors.Join(errRpcIntegrity, err)
		}
		from, fromErr := decoded.text("from")
		if fromErr != nil || !common.IsHexAddress(from) || common.HexToAddress(from) != sender || receipt.Type != tx.Type() || receipt.TxHash != tx.Hash() || receipt.BlockHash != header.Hash() || receipt.BlockNumber == nil || receipt.BlockNumber.Cmp(header.Number) != 0 || receipt.TransactionIndex != uint(index) || receipt.Status > 1 || receipt.GasUsed == 0 || receipt.GasUsed > tx.Gas() || receipt.CumulativeGasUsed < cumulative || receipt.CumulativeGasUsed-cumulative != receipt.GasUsed || len(receipt.Logs) != len(logs) || receipt.Bloom != types.CreateBloom(&receipt) {
			return result, monitorEvmIntegrity("economic receipt differs from signed transaction, position or gas census")
		}
		if tx.To() == nil {
			if !bytes.Equal(decoded.fields["to"], []byte("null")) || receipt.Status == 1 && receipt.ContractAddress != crypto.CreateAddress(sender, tx.Nonce()) || receipt.Status == 0 && receipt.ContractAddress != (common.Address{}) {
				return result, monitorEvmIntegrity("economic CREATE receipt target differs")
			}
		} else {
			to, toErr := decoded.text("to")
			if toErr != nil || !common.IsHexAddress(to) || common.HexToAddress(to) != *tx.To() || receipt.ContractAddress != (common.Address{}) {
				return result, monitorEvmIntegrity("economic call receipt target differs")
			}
		}
		price := decoded.values["effectiveGasPrice"]
		if price.Cmp(tx.GasFeeCap()) > 0 || tx.Type() == types.LegacyTxType && price.Cmp(tx.GasPrice()) != 0 {
			return result, monitorEvmIntegrity("economic receipt fee price exceeds its original signed authority")
		}
		if receipt.Status != types.ReceiptStatusSuccessful && len(receipt.Logs) != 0 {
			return result, monitorEvmIntegrity("failed economic receipt retained committed contract logs")
		}
		cumulative = receipt.CumulativeGasUsed
		digest := sha256.Sum256(raw)
		receiptHash := "sha256:" + hex.EncodeToString(digest[:])
		for _, log := range receipt.Logs {
			if log == nil || log.Removed || log.BlockHash != header.Hash() || log.BlockNumber != header.Number.Uint64() || log.TxHash != tx.Hash() || log.TxIndex != uint(index) || uint64(log.Index) != logIndex {
				return result, monitorEvmIntegrity("economic receipt log position is missing, repeated or removed")
			}
			logIndex++
			if observer := self.entitlementObserver; observer != nil && log.Address == observer.address {
				decoder := monitorEvmReader{contract: observer.contract}
				event, err := decoder.event(log, receiptHash)
				if err != nil {
					return result, err
				}
				observer.events = append(observer.events, event)
			}
			if log.Address == common.HexToAddress(self.policy.Address) {
				event, err := self.event(log, receiptHash)
				if err != nil {
					return result, err
				}
				result.Events = append(result.Events, event)
			}
		}
		if slices.Contains(self.policy.FeePayers, sender.Hex()) {
			fee := new(big.Int).Mul(price, new(big.Int).SetUint64(receipt.GasUsed))
			if fee.BitLen() > 256 {
				return result, monitorEvmIntegrity("economic receipt cost exceeds uint256")
			}
			result.Fees = append(result.Fees, monitorEconomicEvmFee{Block: result.Boundary, TransactionHash: tx.Hash().Hex(), TransactionIndex: uint64(index), Payer: sender.Hex(), GasUsed: receipt.GasUsed, EffectiveGasPriceWei: price.String(), FeeWei: fee.String(), Success: receipt.Status == 1, ReceiptHash: receiptHash})
		}
		receipts[index] = &receipt
	}
	if cumulative != header.GasUsed || types.DeriveSha(receipts, trie.NewStackTrie(nil)) != header.ReceiptHash {
		return result, monitorEvmIntegrity("economic receipt census differs from authenticated gas or receipt root")
	}
	result.Snapshot, err = self.snapshot(ctx, result.Boundary.Hash)
	if err != nil {
		return result, err
	}
	for index := range result.Events {
		event := &result.Events[index]
		identity, err := self.captureIdentity(ctx, *event)
		if err != nil {
			return result, err
		}
		event.CaptureIdentity = identity
	}
	for _, event := range result.Events {
		if event.Name != "EntitlementFinalized" && event.Name != "RootMissed" {
			continue
		}
		key := event.Values["epoch"] + "/" + event.Values["noId"]
		if _, ok := result.Funded[key]; ok {
			continue
		}
		epoch, e1 := monitorEconomicInteger(event.Values["epoch"])
		pool, e2 := monitorEconomicInteger(event.Values["noId"])
		if e1 != nil || e2 != nil {
			return result, monitorEvmIntegrity("economic entitlement position is malformed")
		}
		values, err := self.getter(ctx, result.Boundary.Hash, "entitlement", epoch, pool)
		if err != nil {
			return result, err
		}
		if len(values) != 1 {
			return result, monitorEvmIntegrity("economic entitlement getter arity differs")
		}
		entitlement := *abi.ConvertType(values[0], new(stabi.STSettlementVaultEntitlement)).(*stabi.STSettlementVaultEntitlement)
		if entitlement.Funded == nil || entitlement.Funded.Sign() < 0 || entitlement.Funded.BitLen() > 256 || entitlement.Status < 2 || entitlement.Status > 4 {
			return result, monitorEvmIntegrity("economic entitlement funding or terminal status differs")
		}
		result.Funded[key] = entitlement.Funded.String()
	}
	return result, nil
}
