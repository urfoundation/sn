// Current admission observes the exact Safe and contract domain at one finalized
// mapping and pending state. Unrelated accounts never become admission gates.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Raw words and ABI outputs must be canonical, bounded and fully consumed.
func (self *bootstrapSuccessorCanonicalChain) word(ctx context.Context, address common.Address, slot common.Hash, block any) (common.Hash, error) {
	var encoded string
	if err := self.chain.client.callRequiredEvmStateRead(ctx, "eth_getStorageAt", []any{address.Hex(), slot.Hex(), block}, &encoded); err != nil {
		return common.Hash{}, err
	}
	raw, err := rootReceiptHex(encoded, 32)
	if err != nil || len(raw) != 32 || encoded != "0x"+hex.EncodeToString(raw) {
		return common.Hash{}, errors.Join(errRpcIntegrity, errors.New("successor canonical storage word is not exact"))
	}
	return common.BytesToHash(raw), nil
}

// Code is compared before invoking that account's getters.
func (self *bootstrapSuccessorCanonicalChain) code(ctx context.Context, address common.Address, block any) ([]byte, error) {
	var encoded string
	if err := self.chain.client.callRequiredEvmStateRead(ctx, "eth_getCode", []any{address.Hex(), block}, &encoded); err != nil {
		return nil, err
	}
	raw, err := rootReceiptHex(encoded, 64*1024)
	if err != nil || encoded != "0x"+hex.EncodeToString(raw) {
		return nil, errors.Join(errRpcIntegrity, errors.New("successor canonical runtime encoding differs"))
	}
	return raw, nil
}

// The pinned published ABI supplies only encoding. These bounded current-state
// observations cannot establish complete deployment/storage provenance.
func (self *bootstrapSuccessorCanonicalChain) safeCall(ctx context.Context, plan bootstrapSuccessorExecutionPlan, block any, maximum int, name string, args ...any) ([]any, error) {
	profile := self.owner.profile
	input, err := profile.contractAbi.Pack(name, args...)
	if err != nil {
		return nil, err
	}
	var encoded string
	if err := self.chain.client.callRequiredEvmStateRead(ctx, "eth_call", []any{map[string]any{"to": plan.Review.Transaction.Safe.Hex(), "data": "0x" + hex.EncodeToString(input)}, block}, &encoded); err != nil {
		return nil, err
	}
	raw, err := rootReceiptHex(encoded, maximum)
	if err != nil || encoded != "0x"+hex.EncodeToString(raw) {
		return nil, errors.Join(errRpcIntegrity, errors.New("successor canonical Safe getter is not bounded canonical bytes"))
	}
	values, err := profile.contractAbi.Methods[name].Outputs.Unpack(raw)
	if err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	canonical, err := profile.contractAbi.Methods[name].Outputs.Pack(values...)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, errors.Join(errRpcIntegrity, errors.New("successor canonical Safe getter has padding or trailing bytes"))
	}
	return values, nil
}

// Both pinned releases share slots zero through five and the guard/fallback
// namespaces. The module-guard namespace is additionally required empty for
// the older release. Empty sentinel lists cannot exclude unreachable enabled
// owner/module entries; separate complete provenance is mandatory for admission.
func (self *bootstrapSuccessorCanonicalChain) safeState(ctx context.Context, plan bootstrapSuccessorExecutionPlan, block any) (bootstrapSuccessorExecutionObservation, error) {
	var result bootstrapSuccessorExecutionObservation
	proxy, err := self.code(ctx, plan.Review.Transaction.Safe, block)
	if err != nil {
		return result, err
	}
	singletonWord, err := self.word(ctx, plan.Review.Transaction.Safe, common.Hash{}, block)
	if err != nil {
		return result, err
	}
	if !bytes.Equal(singletonWord[:12], make([]byte, 12)) {
		return result, errors.Join(errRpcIntegrity, errors.New("successor canonical singleton word differs"))
	}
	result.Singleton = common.BytesToAddress(singletonWord[12:])
	if result.Singleton != plan.Request.Singleton {
		return result, errors.Join(errRpcIntegrity, errors.New("successor canonical singleton changed"))
	}
	singleton, err := self.code(ctx, result.Singleton, block)
	if err != nil {
		return result, err
	}
	result.SafeProxyRuntimeHash, result.SingletonRuntimeHash = crypto.Keccak256Hash(proxy), crypto.Keccak256Hash(singleton)
	pin, err := loadSafeReleasePin(plan.Review.Request.Version, plan.Review.Request.Variant)
	if err != nil {
		return result, err
	}
	for _, artifact := range pin.Artifacts {
		if artifact.Name == "SafeProxy" && result.SafeProxyRuntimeHash.Hex() != artifact.RuntimeKeccak256 ||
			artifact.Name == plan.Review.Request.Variant && result.SingletonRuntimeHash.Hex() != artifact.RuntimeKeccak256 {
			return result, errors.Join(errRpcIntegrity, errors.New("successor canonical Safe runtime differs from reviewed release"))
		}
	}
	// The signed execution request selects the census; getters cannot choose it.
	ownerCount, required := safeOwnerProfile(plan.Request.singleOwner())
	owners, err := self.safeCall(ctx, plan, block, 160, "getOwners")
	if err != nil {
		return result, err
	}
	if len(owners) != 1 {
		return result, errors.Join(errRpcIntegrity, errors.New("successor canonical owner census differs"))
	}
	ownerAddresses, ok := owners[0].([]common.Address)
	if !ok || len(ownerAddresses) != ownerCount {
		return result, errors.Join(errRpcIntegrity, errors.New("successor canonical owner census differs"))
	}
	result.Owners = slices.Clone(ownerAddresses)
	slices.SortFunc(result.Owners, func(a, b common.Address) int { return bytes.Compare(a[:], b[:]) })
	if !slices.Equal(result.Owners, plan.Request.Owners) {
		return result, errors.Join(errRpcIntegrity, errors.New("successor canonical current owners differ"))
	}
	threshold, err := self.safeCall(ctx, plan, block, 32, "getThreshold")
	if err != nil {
		return result, err
	}
	if len(threshold) != 1 {
		return result, errors.Join(errRpcIntegrity, errors.New("successor canonical threshold differs"))
	}
	thresholdNumber, ok := threshold[0].(*big.Int)
	if !ok || !thresholdNumber.IsUint64() || thresholdNumber.Uint64() != uint64(required) {
		return result, errors.Join(errRpcIntegrity, errors.New("successor canonical threshold differs"))
	}
	result.Threshold = thresholdNumber.Uint64()
	for _, value := range []struct{ slot, expected uint64 }{{slot: 3, expected: uint64(ownerCount)}, {slot: 4, expected: uint64(required)}} {
		word, err := self.word(ctx, plan.Review.Transaction.Safe, common.BigToHash(new(big.Int).SetUint64(value.slot)), block)
		if err != nil {
			return result, err
		}
		if word != common.BigToHash(new(big.Int).SetUint64(value.expected)) {
			return result, errors.Join(errRpcIntegrity, errors.New("successor canonical owner storage differs from getters"))
		}
	}
	nonce, err := self.safeCall(ctx, plan, block, 32, "nonce")
	if err != nil {
		return result, err
	}
	if len(nonce) != 1 {
		return result, errors.Join(errRpcIntegrity, errors.New("successor canonical Safe nonce differs"))
	}
	nonceNumber, ok := nonce[0].(*big.Int)
	if !ok || nonceNumber.Sign() < 0 || nonceNumber.BitLen() > 256 {
		return result, errors.Join(errRpcIntegrity, errors.New("successor canonical Safe nonce is malformed"))
	}
	nonceWord, err := self.word(ctx, plan.Review.Transaction.Safe, common.BigToHash(big.NewInt(5)), block)
	if err != nil {
		return result, err
	}
	if nonceWord != common.BigToHash(nonceNumber) {
		return result, errors.Join(errRpcIntegrity, errors.New("successor canonical Safe nonce storage differs"))
	}
	result.SafeNonce = nonceNumber.String()
	modules, err := self.safeCall(ctx, plan, block, 128, "getModulesPaginated", common.HexToAddress("0x1"), big.NewInt(1))
	if err != nil {
		return result, err
	}
	if len(modules) != 2 {
		return result, errors.Join(errRpcIntegrity, errors.New("successor canonical module census differs"))
	}
	moduleAddresses, ok := modules[0].([]common.Address)
	if !ok || len(moduleAddresses) != 0 || modules[1] != common.HexToAddress("0x1") {
		return result, errors.Join(errRpcIntegrity, errors.New("successor canonical module census is not empty"))
	}
	result.Modules = []common.Address{}
	for _, slot := range []string{"0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8", "0xb104e0b93118902c651344349b610029d694cfdec91c589c91ebafbcd0289947", "0x6c9a6c4a39284e37ed1cf53d337577d14212a4870fb976a4366c693b939918d5"} {
		word, err := self.word(ctx, plan.Review.Transaction.Safe, common.HexToHash(slot), block)
		if err != nil {
			return result, err
		}
		if word != (common.Hash{}) {
			return result, errors.Join(errRpcIntegrity, errors.New("successor canonical guard, module guard or fallback is active"))
		}
	}
	tx := plan.transaction()
	digest, err := self.safeCall(ctx, plan, block, 32, "getTransactionHash", tx.To, tx.Value, tx.Data, tx.Operation, tx.SafeTxGas, tx.BaseGas, tx.GasPrice, tx.GasToken, tx.RefundReceiver, tx.Nonce)
	if err != nil {
		return result, err
	}
	if len(digest) != 1 || digest[0] != [32]byte(plan.Review.Transaction.Digest) {
		return result, errors.Join(errRpcIntegrity, errors.New("successor canonical Safe digest differs from exact retained inner operation"))
	}
	return result, nil
}

// Reuse the original exact runtime, getter and slot projections. The only
// changed postcondition is the one-shot evidence pointer after installation;
// currentEpoch is a clock rather than a stable bootstrap invariant.
func (self *bootstrapSuccessorCanonicalChain) contractsState(ctx context.Context, block any, evidence common.Address) (string, string, error) {
	coordinator := stabi.NewSTCoordinator()
	evidenceSelector := "0x" + hex.EncodeToString(coordinator.PackValidatorEvidence())
	clockSelector := "0x" + hex.EncodeToString(coordinator.PackCurrentEpoch())
	for _, index := range []int{2, 4, 5, 6, 7} {
		plan := self.plans[index]
		code, err := self.code(ctx, plan.Address, block)
		if err != nil {
			return "", "", err
		}
		if !bytes.Equal(code, plan.Runtime) {
			return "", "", errors.Join(errRpcIntegrity, errors.New("successor canonical contract runtime differs"))
		}
		getters, err := plan.receiptGetters(*self.records[index].Receipt)
		if err != nil {
			return "", "", err
		}
		for _, getter := range getters {
			if index == 4 && getter.Data == clockSelector {
				continue
			}
			expected := getter.Expected
			if index == 4 && getter.Data == evidenceSelector {
				expected = common.BytesToHash(evidence[:]).Hex()
			}
			var output string
			if err := self.chain.client.callRequiredEvmStateRead(ctx, "eth_call", []any{map[string]any{"to": plan.Address.Hex(), "data": getter.Data}, block}, &output); err != nil {
				return "", "", err
			}
			if output != expected {
				return "", "", errors.Join(errRpcIntegrity, errors.New("successor canonical contract getter or evidence binding differs"))
			}
		}
		for _, expected := range plan.Storage {
			word, err := self.word(ctx, plan.Address, common.HexToHash(expected.Slot), block)
			if err != nil {
				return "", "", err
			}
			if word.Hex() != expected.Expected {
				return "", "", errors.Join(errRpcIntegrity, errors.New("successor canonical contract storage differs"))
			}
		}
	}
	return crypto.Keccak256Hash(self.plans[7].Runtime).Hex(), rootObjectHash(self.plans[7].Getters), nil
}

// Network and both approved native start points are rechecked independently of
// runtime upgrades and approval expiry, so historical reconciliation stays usable.
func (self *bootstrapSuccessorCanonicalChain) identity(ctx context.Context, plan bootstrapSuccessorExecutionPlan) (chainIdentity, error) {
	head, err := self.chain.client.readIdentity(ctx)
	if err != nil {
		return head, err
	}
	p := self.plans[0].Config.Plan
	if err := self.chain.continuity(ctx, p, evmActionRecord{ScanNumber: plan.Review.Request.StartNativeNumber, ScanHash: plan.Review.Request.StartNativeHash}, head); err != nil {
		return head, err
	}
	return head, nil
}

// Current runtime must match a complete independently retained artifact.
// Historical originals continue to use their own immutable approved artifacts.
func (self *bootstrapSuccessorCanonicalChain) currentRuntime(ctx context.Context, head chainIdentity) error {
	runtime, err := self.chain.client.readRuntimeSnapshotAtIdentity(ctx, head)
	if err != nil {
		return err
	}
	for _, profile := range self.runtimeProfiles {
		if runtime.Version == profile.RuntimeVersion && runtime.CodeHash == profile.RuntimeCodeHash && runtime.MetadataHash == profile.RuntimeMetadataHash {
			return nil
		}
	}
	return errors.Join(errRpcIntegrity, errors.New("successor canonical current runtime differs from its independent successor approval"))
}

// Finalized reads keep one canonical hash while ordinary head advancement is
// allowed. Pending authority follows those reads; a later head updates window
// and runtime admission without restarting the snapshot or awaiting quiescence.
// Each RPC has its own bounded retry window under caller cancellation.
func (self *bootstrapSuccessorCanonicalChain) observe(ctx context.Context, plan bootstrapSuccessorExecutionPlan) (bootstrapSuccessorExecutionObservation, error) {
	var result bootstrapSuccessorExecutionObservation
	if self != nil {
		self.admitted = false
		self.currentProof = nil
	}
	if self != nil && self.owner != nil && (self.owner.safeCurrentHistory.hash() != "" || self.owner.safeCurrentHistory.pendingHash != "") {
		if err := self.currentPolicyReady(ctx, plan, self.owner.safeCurrentHistory.hash()); err != nil {
			return result, err
		}
	}
	if self == nil || self.provenance == nil && self.currentPolicy == nil {
		return result, errBootstrapSuccessorSafeProvenanceUnavailable
	}
	if err := self.checkpoint(ctx, plan); err != nil {
		return result, err
	}
	if !self.authenticated {
		return result, errors.New("successor canonical observation precedes historical adoption")
	}
	if self.owner.runtimeHistory.pendingHash != "" {
		return result, errors.New("successor canonical admission has an incomplete runtime revision")
	}
	head, err := self.identity(ctx, plan)
	if err != nil {
		return result, err
	}
	if err := self.currentRuntime(ctx, head); err != nil {
		return result, err
	}
	mapping, err := self.chain.client.readFinalizedMappingAtIdentity(ctx, head)
	if err != nil {
		return result, err
	}
	block := map[string]any{"blockHash": mapping.EvmHeader.Hash, "requireCanonical": true}
	result, err = self.safeState(ctx, plan, block)
	if err != nil {
		return result, err
	}
	safeHash := rootObjectHash(result)
	runtimeHash, getterHash, err := self.contractsState(ctx, block, common.Address{})
	if err != nil {
		return result, err
	}
	result.EvidenceRuntimeHash, result.EvidenceGetterHash = runtimeHash, getterHash
	result.CoordinatorOwner = self.plans[4].ProxyConstructor.Owner
	var confirmedNonce, pendingNonce, confirmedBalance, pendingBalance string
	for _, read := range []struct {
		method string
		value  *string
	}{{method: "eth_getTransactionCount", value: &confirmedNonce}, {method: "eth_getBalance", value: &confirmedBalance}} {
		if err := self.chain.read(ctx, read.method, []any{plan.Review.Relayer.Sender.Hex(), block}, read.value); err != nil {
			return result, err
		}
	}
	// History authentication can be expensive. It binds the pinned finalized
	// snapshot before every pending observation and the final canonical,
	// runtime and window checks, so changes during proof cannot reuse them.
	if err := self.authenticateSafeAuthority(ctx, plan, head); err != nil {
		return result, err
	}
	// Scoped pending state cannot enumerate off-node signatures or other
	// relayers. Independent signer cutover explicitly owns that assumption.
	if _, _, err := self.contractsState(ctx, "pending", common.Address{}); err != nil {
		return result, err
	}
	pending, err := self.safeState(ctx, plan, "pending")
	if err != nil {
		return result, err
	}
	if rootObjectHash(pending) != safeHash {
		return result, errors.Join(errRpcIntegrity, errors.New("successor canonical Safe pending authority or nonce differs"))
	}
	for _, read := range []struct {
		method string
		value  *string
	}{{method: "eth_getTransactionCount", value: &pendingNonce}, {method: "eth_getBalance", value: &pendingBalance}} {
		if err := self.chain.read(ctx, read.method, []any{plan.Review.Relayer.Sender.Hex(), "pending"}, read.value); err != nil {
			return result, err
		}
	}
	confirmed, e1 := evmQuantity(confirmedNonce, 64)
	queued, e2 := evmQuantity(pendingNonce, 64)
	balance, e3 := evmQuantity(confirmedBalance, 256)
	available, e4 := evmQuantity(pendingBalance, 256)
	if err := errors.Join(e1, e2, e3, e4); err != nil {
		return result, err
	}
	result.RelayerNonce, result.RelayerPendingNonce = confirmed.Uint64(), queued.Uint64()
	if balance.Cmp(available) < 0 {
		available = balance
	}
	result.RelayerBalanceWei = available.String()
	known, err := self.retainedTransactionKnown(ctx, plan)
	if err != nil {
		return result, err
	}
	if known {
		return result, errors.New("successor canonical retained transaction is pending or unresolved for this account")
	}
	if _, err := self.chain.client.readFinalizedMappingAtIdentity(ctx, head); err != nil {
		return result, err
	}
	latest, err := self.identity(ctx, plan)
	if err != nil {
		return result, err
	}
	if err := self.chain.continuity(ctx, self.plans[0].Config.Plan, evmActionRecord{ScanNumber: head.FinalizedNumber, ScanHash: head.FinalizedHash}, latest); err != nil {
		return result, err
	}
	if latest.FinalizedHash != head.FinalizedHash {
		if err := self.currentRuntime(ctx, latest); err != nil {
			return result, err
		}
	} else if latest.runtimeVersion != head.runtimeVersion {
		return result, errors.New("successor canonical runtime changed at the pinned finalized hash")
	}
	result.NativeNumber, result.NativeHash = latest.FinalizedNumber, common.HexToHash(latest.FinalizedHash)
	if err := self.readmitCurrentPolicy(ctx, plan, safeHash, &result); err != nil {
		return result, err
	}
	if err := plan.admit(result); err != nil {
		return result, err
	}
	if err := self.checkpoint(ctx, plan); err != nil {
		return result, err
	}
	self.admitted, self.admittedSequence = true, self.owner.last.Sequence
	return result, nil
}
