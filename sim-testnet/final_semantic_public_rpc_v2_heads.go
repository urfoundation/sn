//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common/hexutil"
	validatorpkg "github.com/urfoundation/sn/validator"
)

func finalV2RPCString(raw json.RawMessage) (string, error) {
	var value string
	if json.Unmarshal(raw, &value) != nil || value == "" {
		return "", errors.New("public V2 RPC parameter is not a string")
	}
	return value, nil
}

func finalV2RPCHash(raw json.RawMessage) (string, error) {
	value, err := finalV2RPCString(raw)
	if err != nil {
		return "", err
	}
	value = strings.ToLower(value)
	if !validCanonicalHashHex(value) {
		return "", errors.New("public V2 RPC hash is not canonical")
	}
	return value, nil
}

func finalV2RPCNumber(raw json.RawMessage) (uint64, error) {
	if len(raw) != 0 && raw[0] == '"' {
		value, err := finalV2RPCString(raw)
		if err != nil {
			return 0, err
		}
		return hexutil.DecodeUint64(value)
	}
	return strconv.ParseUint(string(raw), 10, 64)
}

func finalV2EVMSelector(raw json.RawMessage) (string, error) {
	var selector finalEVMBlockSelector
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&selector); err != nil || !selector.RequireCanonical || !validCanonicalHashHex(selector.BlockHash) {
		return "", errors.New("public V2 EVM state read lacks its exact canonical hash selector")
	}
	return selector.BlockHash, nil
}

// Admission happens before the EVM transport is called. Every state read is
// hash pinned; only the ordinary live-finality guard may use a moving tag.
func finalV2EVMReadMethod(method string, params []json.RawMessage) error {
	switch method {
	case "eth_chainId":
		if len(params) == 0 {
			return nil
		}
	case "eth_getBlockByNumber", "eth_getBlockByHash":
		if len(params) == 2 && bytes.Equal(params[1], []byte("false")) {
			if method == "eth_getBlockByHash" {
				_, err := finalV2RPCHash(params[0])
				return err
			}
			if string(params[0]) == `"finalized"` {
				return nil
			}
			_, err := finalV2RPCNumber(params[0])
			return err
		}
	case "eth_call", "eth_getCode":
		if len(params) == 2 {
			_, err := finalV2EVMSelector(params[1])
			return err
		}
	case "eth_getStorageAt":
		if len(params) == 3 {
			_, err := finalV2EVMSelector(params[2])
			return err
		}
	case "eth_getLogs":
		if len(params) == 1 {
			var filter struct {
				BlockHash string `json:"blockHash"`
				FromBlock string `json:"fromBlock"`
				ToBlock   string `json:"toBlock"`
			}
			if err := json.Unmarshal(params[0], &filter); err != nil {
				return err
			}
			if filter.BlockHash != "" && filter.FromBlock == "" && filter.ToBlock == "" && validCanonicalHashHex(filter.BlockHash) {
				return nil
			}
			from, fromErr := hexutil.DecodeUint64(filter.FromBlock)
			to, toErr := hexutil.DecodeUint64(filter.ToBlock)
			if filter.BlockHash == "" && fromErr == nil && toErr == nil && from <= to {
				return nil
			}
		}
	case "eth_getTransactionReceipt":
		if len(params) == 1 {
			_, err := finalV2RPCHash(params[0])
			return err
		}
	}
	return fmt.Errorf("public V2 RPC method %s is not an admitted immutable read", method)
}

func (self *finalV2RPCRecorder) cachedHead(chain, hash string) (ChainHead, bool) {
	self.mu.Lock()
	defer self.mu.Unlock()
	heads := self.nativeHeads
	if chain == "evm" {
		heads = self.evmHeads
	}
	head, found := heads[hash]
	return head, found
}

func (self *finalV2RPCRecorder) rememberHead(chain string, head ChainHead) error {
	if err := verifyFinalHead("public V2 canonical", head); err != nil {
		return err
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	heads, numbers := self.nativeHeads, self.nativeNumbers
	if chain == "evm" {
		heads, numbers = self.evmHeads, self.evmNumbers
	}
	if old, found := heads[head.Hash]; found && old != head {
		return errors.New("public V2 hash changed its block number")
	}
	if old, found := numbers[head.Number]; found && old != head.Hash {
		return errors.New("public V2 canonical block changed within source observation")
	}
	heads[head.Hash], numbers[head.Number] = head, head.Hash
	return nil
}

// Header number and hash are resolved independently for each chain. Only these
// authenticated immutable pairs are reused; no EVM/native height equivalence
// and no captured observation supplies a header or canonicality verdict.
func (self *finalV2RPCRecorder) evmHead(ctx context.Context, hash string, header json.RawMessage) (ChainHead, error) {
	if cached, found := self.cachedHead("evm", hash); found {
		return cached, nil
	}
	if !validCanonicalHashHex(hash) {
		return ChainHead{}, errors.New("public V2 EVM head hash is invalid")
	}
	var err error
	if len(header) == 0 {
		header, _, err = self.sourceRaw(ctx, "evm", ChainHead{}, "eth_getBlockByHash", hash, false)
		if err != nil {
			return ChainHead{}, err
		}
	}
	head, err := finalEVMBlock(header)
	if err != nil || head.Hash != hash {
		return ChainHead{}, errors.Join(errors.New("public V2 EVM header identity differs"), err)
	}
	canonical, exchange, err := self.sourceRaw(ctx, "evm", head, "eth_getBlockByNumber", hexutil.EncodeUint64(head.Number), false)
	if err != nil {
		return ChainHead{}, err
	}
	observed, err := finalEVMBlock(canonical)
	if err != nil || observed != head {
		return ChainHead{}, errors.Join(errors.New("public V2 EVM block is not canonical"), err)
	}
	if err := self.retain(exchange); err != nil {
		return ChainHead{}, err
	}
	params, _ := json.Marshal([]any{hash, false})
	if err := self.retain(FinalRPCExchange{Chain: "evm", Method: "eth_getBlockByHash", Params: params, PinnedHead: head, Result: header}); err != nil {
		return ChainHead{}, err
	}
	return head, self.rememberHead("evm", head)
}

func (self *finalV2RPCRecorder) nativeHead(ctx context.Context, hash string, header json.RawMessage) (ChainHead, error) {
	if cached, found := self.cachedHead("substrate", hash); found {
		return cached, nil
	}
	if !validCanonicalHashHex(hash) {
		return ChainHead{}, errors.New("public V2 native head hash is invalid")
	}
	var err error
	if len(header) == 0 {
		header, _, err = self.sourceRaw(ctx, "substrate", ChainHead{}, "chain_getHeader", hash)
		if err != nil {
			return ChainHead{}, err
		}
	}
	number, included, err := finalSubstrateHeader(header)
	if err != nil || included != "" && !strings.EqualFold(included, hash) {
		return ChainHead{}, errors.Join(errors.New("public V2 native header identity differs"), err)
	}
	head := ChainHead{Number: number, Hash: hash}
	canonical, exchange, err := self.sourceRaw(ctx, "substrate", head, "chain_getBlockHash", number)
	if err != nil {
		return ChainHead{}, err
	}
	observed, err := finalV2RPCHash(canonical)
	if err != nil || observed != hash {
		return ChainHead{}, errors.Join(errors.New("public V2 native block is not canonical"), err)
	}
	if err := self.retain(exchange); err != nil {
		return ChainHead{}, err
	}
	params, _ := json.Marshal([]any{hash})
	if err := self.retain(FinalRPCExchange{Chain: "substrate", Method: "chain_getHeader", Params: params, PinnedHead: head, Result: header}); err != nil {
		return ChainHead{}, err
	}
	return head, self.rememberHead("substrate", head)
}

func (self *finalV2RPCRecorder) retainEVM(ctx context.Context, method string, params []json.RawMessage, raw []byte) error {
	var head ChainHead
	var err error
	switch method {
	case "eth_chainId":
		chainID, decodeErr := finalV2RPCNumber(raw)
		if decodeErr != nil || chainID != self.reader.evidence.ChainID {
			return errors.New("public V2 EVM chain identity differs")
		}
		head = self.reader.evidence.EVMTerminalHead
	case "eth_getBlockByNumber", "eth_getBlockByHash":
		if method == "eth_getBlockByNumber" && string(params[0]) == `"finalized"` {
			return nil
		}
		observed, decodeErr := finalEVMBlock(raw)
		if decodeErr != nil {
			return decodeErr
		}
		if method == "eth_getBlockByNumber" {
			want, decodeErr := finalV2RPCNumber(params[0])
			if decodeErr != nil || want != observed.Number {
				return errors.New("public V2 EVM header changed its requested number")
			}
		} else {
			want, decodeErr := finalV2RPCHash(params[0])
			if decodeErr != nil || want != observed.Hash {
				return errors.New("public V2 EVM header changed its requested hash")
			}
		}
		var exactHashResponse json.RawMessage
		if method == "eth_getBlockByHash" {
			exactHashResponse = raw
		}
		head, err = self.evmHead(ctx, observed.Hash, exactHashResponse)
	case "eth_call", "eth_getCode", "eth_getStorageAt":
		hash, decodeErr := finalV2EVMSelector(params[len(params)-1])
		if decodeErr != nil {
			return decodeErr
		}
		head, err = self.evmHead(ctx, hash, nil)
	case "eth_getLogs":
		var filter struct {
			BlockHash string `json:"blockHash"`
			ToBlock   string `json:"toBlock"`
		}
		if err := json.Unmarshal(params[0], &filter); err != nil {
			return err
		}
		if filter.BlockHash != "" {
			head, err = self.evmHead(ctx, filter.BlockHash, nil)
		} else {
			header, _, readErr := self.sourceRaw(ctx, "evm", ChainHead{}, "eth_getBlockByNumber", filter.ToBlock, false)
			if readErr != nil {
				return readErr
			}
			observed, readErr := finalEVMBlock(header)
			want, numberErr := hexutil.DecodeUint64(filter.ToBlock)
			if readErr != nil || numberErr != nil || observed.Number != want {
				return errors.New("public V2 log range terminal header differs")
			}
			head, err = self.evmHead(ctx, observed.Hash, nil)
			if err == nil {
				query, _ := json.Marshal([]any{filter.ToBlock, false})
				err = self.retain(FinalRPCExchange{Chain: "evm", Method: "eth_getBlockByNumber", Params: query, PinnedHead: head, Result: header})
			}
		}
	case "eth_getTransactionReceipt":
		var receipt struct {
			BlockHash       string `json:"blockHash"`
			BlockNumber     string `json:"blockNumber"`
			TransactionHash string `json:"transactionHash"`
		}
		if err := json.Unmarshal(raw, &receipt); err != nil {
			return err
		}
		want, decodeErr := finalV2RPCHash(params[0])
		number, numberErr := hexutil.DecodeUint64(receipt.BlockNumber)
		if decodeErr != nil || numberErr != nil || !strings.EqualFold(receipt.TransactionHash, want) {
			return errors.New("public V2 receipt changed its requested identity")
		}
		head, err = self.evmHead(ctx, strings.ToLower(receipt.BlockHash), nil)
		if err == nil && head.Number != number {
			return errors.New("public V2 receipt changed its canonical inclusion number")
		}
	default:
		return errors.New("public V2 EVM transcript received a non-read method")
	}
	if err != nil {
		return err
	}
	if params == nil {
		params = []json.RawMessage{}
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return self.retain(FinalRPCExchange{Chain: "evm", Method: method, Params: encoded, PinnedHead: head, Result: raw})
}

func (self *finalV2RPCRecorder) retainNative(ctx context.Context, read validatorpkg.ReleaseEvidenceV2NativeRead) (resultErr error) {
	defer func() { self.fail(resultErr) }()
	var params []json.RawMessage
	if read.Schema != "urnetwork-validator-native-read-v2" || json.Unmarshal(read.Parameters, &params) != nil {
		return errors.New("public V2 native read wire is invalid")
	}
	var hash string
	var head ChainHead
	var header json.RawMessage
	var err error
	switch read.Method {
	case "chain_getFinalizedHead":
		if len(params) != 0 {
			return errors.New("public V2 finalized native query has arguments")
		}
		hash, err = finalV2RPCHash(read.Result)
		if err != nil {
			return err
		}
		self.mu.Lock()
		self.liveNativeHeads[hash] = true
		self.mu.Unlock()
		return nil
	case "chain_getBlockHash":
		if len(params) != 1 {
			return errors.New("public V2 native canonical hash query is not height pinned")
		}
		number, decodeErr := finalV2RPCNumber(params[0])
		if decodeErr != nil {
			return decodeErr
		}
		hash, err = finalV2RPCHash(read.Result)
		if err != nil {
			return err
		}
		if number == 0 {
			if !strings.EqualFold(hash, self.reader.evidence.GenesisHash) {
				return errors.New("public V2 native genesis differs")
			}
			head = self.reader.evidence.NativeTerminalHead
		} else {
			head, err = self.nativeHead(ctx, hash, nil)
			if err == nil && head.Number != number {
				return errors.New("public V2 native canonical query changed its requested number")
			}
		}
	case "chain_getHeader", "chain_getBlock", "state_getRuntimeVersion", "state_getMetadata":
		if len(params) != 1 {
			return errors.New("public V2 native historical read is not hash pinned")
		}
		hash, err = finalV2RPCHash(params[0])
		if err != nil {
			return err
		}
		if read.Method == "chain_getHeader" {
			self.mu.Lock()
			live := self.liveNativeHeads[hash]
			self.mu.Unlock()
			if live {
				return nil
			}
			header = read.Result
		}
		head, err = self.nativeHead(ctx, hash, header)
	case "state_getStorage", "state_getStorageHash", "state_call":
		want := 2
		if read.Method == "state_call" {
			want = 3
		}
		if len(params) != want {
			return errors.New("public V2 native state read is not hash pinned")
		}
		hash, err = finalV2RPCHash(params[len(params)-1])
		if err != nil {
			return err
		}
		head, err = self.nativeHead(ctx, hash, nil)
	default:
		return errors.New("public V2 native transcript received a non-read method")
	}
	if err != nil {
		return err
	}
	return self.retain(FinalRPCExchange{Chain: "substrate", Method: read.Method, Params: read.Parameters, PinnedHead: head, Result: read.Result})
}
