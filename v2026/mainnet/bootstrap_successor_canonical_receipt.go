// Exact receipt identity, native insertion and inner Safe outcome jointly
// establish installation. An unavailable lookup never becomes nonce reuse.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Reviewed Frontier's lookup searches its ready and future pools as well as
// mapped transactions. A null reply says only this exact hash is unknown to
// this node; signer cutover separately attests the rest of the account inventory.
func (self *bootstrapSuccessorCanonicalChain) retainedTransactionKnown(ctx context.Context, plan bootstrapSuccessorExecutionPlan) (bool, error) {
	var raw json.RawMessage
	if err := self.chain.client.callAdmittedRead(ctx, "eth_getTransactionByHash", []any{plan.TransactionHash.Hex()}, &raw, true, maxRpcReplyBytes); err != nil {
		return false, err
	}
	if bytes.Equal(raw, []byte("null")) {
		return false, nil
	}
	var tx types.Transaction
	if err := json.Unmarshal(raw, &tx); err != nil {
		return false, err
	}
	encoded, err := tx.MarshalBinary()
	if err != nil || "0x"+hex.EncodeToString(encoded) != plan.SignedRelayer || tx.Hash() != plan.TransactionHash {
		return false, errors.Join(errors.New("successor canonical exact-hash lookup changed signed bytes"), err)
	}
	return true, nil
}

// Decode required financial and log positions explicitly. Optional JSON zero
// values cannot turn an omitted receipt field into successful inner execution.
func bootstrapSuccessorCanonicalReceiptFacts(raw json.RawMessage, plan bootstrapSuccessorExecutionPlan) (safeExecutionReceipt, error) {
	var result safeExecutionReceipt
	var fields map[string]json.RawMessage
	if err := decodePlanJson(raw, &fields); err != nil {
		return result, err
	}
	text := func(name string) (string, error) {
		var value string
		if json.Unmarshal(fields[name], &value) != nil || value == "" {
			return "", errors.New("successor canonical receipt omits a required field: " + name)
		}
		return value, nil
	}
	strings := map[string]string{}
	for _, name := range []string{"transactionHash", "blockHash", "from", "to", "blockNumber", "transactionIndex", "status", "gasUsed", "effectiveGasPrice"} {
		value, err := text(name)
		if err != nil {
			return result, err
		}
		strings[name] = value
	}
	if strings["transactionHash"] != plan.TransactionHash.Hex() || !rootCanonicalHash(strings["blockHash"]) ||
		!common.IsHexAddress(strings["from"]) || common.HexToAddress(strings["from"]) != plan.Review.Relayer.Sender ||
		!common.IsHexAddress(strings["to"]) || common.HexToAddress(strings["to"]) != plan.Review.Transaction.Safe || !bytes.Equal(fields["contractAddress"], []byte("null")) {
		return result, errors.New("successor canonical receipt transaction, sender or target differs")
	}
	values := map[string]*big.Int{}
	for _, name := range []string{"blockNumber", "transactionIndex", "status", "gasUsed", "effectiveGasPrice"} {
		bits := 64
		if name == "effectiveGasPrice" {
			bits = 256
		}
		value, err := evmQuantity(strings[name], bits)
		if err != nil {
			return result, err
		}
		values[name] = value
	}
	fee, _ := evmWei(plan.Review.Relayer.FeeCapWei)
	if values["blockNumber"].Sign() == 0 || values["status"].Uint64() > 1 || values["gasUsed"].Sign() == 0 || values["gasUsed"].Uint64() > plan.Review.Relayer.Gas || values["effectiveGasPrice"].Cmp(fee) > 0 || values["transactionIndex"].BitLen() > 32 {
		return result, errors.New("successor canonical receipt financial bounds or position differs")
	}
	var logs []json.RawMessage
	if bytes.Equal(fields["logs"], []byte("null")) || json.Unmarshal(fields["logs"], &logs) != nil || len(logs) > maximumSafeExecutionLogs {
		return result, errors.New("successor canonical receipt lacks a bounded explicit log list")
	}
	result = safeExecutionReceipt{TransactionHash: plan.TransactionHash, BlockHash: common.HexToHash(strings["blockHash"]), BlockNumber: values["blockNumber"].Uint64(),
		TransactionIndex: uint(values["transactionIndex"].Uint64()), To: plan.Review.Transaction.Safe, Status: values["status"].Uint64(), Logs: []*types.Log{}}
	for _, raw := range logs {
		var required struct {
			Removed          *bool  `json:"removed"`
			BlockNumber      string `json:"blockNumber"`
			TransactionIndex string `json:"transactionIndex"`
			LogIndex         string `json:"logIndex"`
		}
		if json.Unmarshal(raw, &required) != nil || required.Removed == nil || *required.Removed {
			return result, errors.New("successor canonical receipt log lacks an explicit retained disposition")
		}
		for _, value := range []string{required.BlockNumber, required.TransactionIndex, required.LogIndex} {
			if _, err := evmQuantity(value, 64); err != nil {
				return result, err
			}
		}
		var entry types.Log
		if err := json.Unmarshal(raw, &entry); err != nil {
			return result, err
		}
		result.Logs = append(result.Logs, &entry)
	}
	return result, nil
}

// Locate the first canonical native commitment with this EVM height using
// bounded lower-bound search. Heights are never equated. Every candidate uses
// the existing authenticated mapping; a gap or changed hash remains unresolved.
func (self *bootstrapSuccessorCanonicalChain) locateReceipt(ctx context.Context, plan bootstrapSuccessorExecutionPlan, head chainIdentity, receipt safeExecutionReceipt) (evmCreateReceipt, error) {
	low, high := plan.Review.Request.StartNativeNumber, head.FinalizedNumber
	var found *evmCreateReceipt
	for probes := 0; low <= high && probes < 64; probes++ {
		middle := low + (high-low)/2
		var hash string
		if err := self.chain.client.call(ctx, "chain_getBlockHash", []any{middle}, &hash); err != nil {
			return evmCreateReceipt{}, err
		}
		identity, err := self.chain.client.readIdentityAt(ctx, hash)
		if err != nil || identity.FinalizedNumber != middle {
			return evmCreateReceipt{}, errors.Join(errors.New("successor canonical receipt candidate changed native position"), err)
		}
		mapping, err := self.chain.client.readFinalizedMappingAtIdentity(ctx, identity)
		if err != nil {
			return evmCreateReceipt{}, err
		}
		if mapping.EvmHeader.Number < receipt.BlockNumber {
			low = middle + 1
			continue
		}
		if mapping.EvmHeader.Number == receipt.BlockNumber {
			if mapping.EvmHeader.Hash != receipt.BlockHash.Hex() {
				return evmCreateReceipt{}, errors.New("successor canonical receipt block hash differs at committed height")
			}
			found = &evmCreateReceipt{TransactionHash: receipt.TransactionHash.Hex(), BlockHash: receipt.BlockHash.Hex(), BlockNumber: receipt.BlockNumber,
				NativeHash: hash, NativeNumber: middle, Status: receipt.Status}
		}
		if middle == 0 {
			break
		}
		high = middle - 1
	}
	if found == nil {
		return evmCreateReceipt{}, errors.New("successor canonical receipt has no finalized native insertion yet")
	}
	return *found, ctx.Err()
}

// Reconciliation precedes current expiry/runtime checks. The separately signed
// retained artifact pair applies at inclusion and parent; later upgrades are immaterial.
func (self *bootstrapSuccessorCanonicalChain) reconcile(ctx context.Context, plan bootstrapSuccessorExecutionPlan) (bootstrapSuccessorExecutionReconciliation, error) {
	var result bootstrapSuccessorExecutionReconciliation
	if err := self.checkpoint(ctx, plan); err != nil {
		return result, err
	}
	if !self.authenticated {
		return result, errors.New("successor canonical reconciliation precedes historical adoption")
	}
	head, err := self.identity(ctx, plan)
	if err != nil {
		return result, err
	}
	var raw json.RawMessage
	if err := self.chain.read(ctx, "eth_getTransactionReceipt", []any{plan.TransactionHash.Hex()}, &raw); err != nil {
		return result, err
	}
	if bytes.Equal(raw, []byte("null")) {
		known, err := self.retainedTransactionKnown(ctx, plan)
		if err != nil {
			return result, err
		}
		result.Status = "absent"
		if known {
			result.Status = "pending"
		}
		return result, self.checkpoint(ctx, plan)
	}
	receipt, err := bootstrapSuccessorCanonicalReceiptFacts(raw, plan)
	if err != nil {
		return result, err
	}
	located, err := self.locateReceipt(ctx, plan, head, receipt)
	if err != nil {
		return result, err
	}
	phase := self.plans[0].Config.Plan
	identity, err := self.chain.authenticatePositionWithRuntimeHistory(ctx, phase,
		evmActionRecord{Signed: plan.SignedRelayer, TransactionHash: plan.TransactionHash.Hex()}, located, uint64(receipt.TransactionIndex), self.runtimeProfiles)
	if err != nil {
		return result, err
	}
	block := map[string]any{"blockHash": receipt.BlockHash.Hex(), "requireCanonical": true}
	outcome, err := self.owner.profile.classifyReceipt(plan.transaction(), plan.TransactionHash, receipt)
	if err != nil {
		return result, err
	}
	bound := common.Address{}
	if outcome.Outcome == "safe-inner-success" {
		bound = plan.Review.Preparation.Approval.Plan.Proposal.Anchor.Evidence
	}
	runtimeHash, getterHash, err := self.contractsState(ctx, block, bound)
	if err != nil {
		return result, err
	}
	safe, err := self.safeState(ctx, plan, block)
	if err != nil {
		return result, err
	}
	nonce := new(big.Int).Set(plan.transaction().Nonce)
	if receipt.Status == 1 {
		nonce.Add(nonce, big.NewInt(1))
		nonce.Mod(nonce, new(big.Int).Lsh(big.NewInt(1), 256))
	}
	if safe.SafeNonce != nonce.String() {
		return result, errors.New("successor canonical inclusion Safe nonce differs from exact inner outcome")
	}
	if _, err := self.chain.client.readFinalizedMappingAtIdentity(ctx, identity); err != nil {
		return result, err
	}
	if err := self.checkpoint(ctx, plan); err != nil {
		return result, err
	}
	result.Status, result.Receipt = "included", &bootstrapSuccessorExecutionReceipt{NativeNumber: located.NativeNumber, NativeHash: common.HexToHash(located.NativeHash), Receipt: receipt,
		CoordinatorEvidence: bound, EvidenceRuntimeHash: runtimeHash, EvidenceGetterHash: getterHash}
	return result, nil
}
