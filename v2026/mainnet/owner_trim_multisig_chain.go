// Multisig steps reuse the canonical trim reconciliation and its fixed route.
// Readback and admission read authenticated storage at one exact block hash.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Exact-block evidence for one included multisig step. Integrity failures stop
// reconciliation; a transient read gap is retained explicitly for a later read.
func (self *ownerTrimCanonicalChain) multisigInclusion(ctx context.Context, metadata *types.Metadata, action ownerTrimAction, events []byte, bodyCount int, receipt rootActionReceipt) (ownerTrimMultisigEvidence, error) {
	result := ownerTrimMultisigEvidence{BodyCount: bodyCount, Events: "0x" + hex.EncodeToString(events)}
	if _, err := ownerTrimMultisigProfile(metadata, len(action.Multisig.Signatories), action.Multisig.Threshold); err != nil {
		return result, err
	}
	dispatch, err := decodeOwnerTrimMultisigDispatch(metadata, events, bodyCount, receipt, action)
	if err != nil {
		return result, err
	}
	result.Dispatch = dispatch
	if !receipt.Success {
		return result, nil
	}
	readback, err := self.readOwnerTrimMultisigState(ctx, metadata, action, receipt.BlockHash, receipt.BlockNumber)
	if err != nil {
		if errors.Is(err, errRpcIntegrity) {
			return result, err
		}
		result.ReadbackIssue = err.Error()
		return result, nil
	}
	result.Readback = &readback
	return result, nil
}

// Read the pending operation row, subnet owner and capacity at one block.
func (self *ownerTrimCanonicalChain) readOwnerTrimMultisigState(ctx context.Context, metadata *types.Metadata, action ownerTrimAction, block string, number uint64) (ownerTrimMultisigReadback, error) {
	result := ownerTrimMultisigReadback{BlockNumber: number, BlockHash: block}
	multisig := action.Multisig
	if multisig == nil {
		return result, errors.New("owner trim multisig readback requires a multisig step")
	}
	if _, err := ownerTrimMultisigProfile(metadata, len(multisig.Signatories), multisig.Threshold); err != nil {
		return result, fmt.Errorf("%w: %v", errRpcIntegrity, err)
	}
	account, _ := hex.DecodeString(multisig.AccountId[2:])
	hash, _ := hex.DecodeString(multisig.CallHash[2:])
	key, err := types.CreateStorageKey(metadata, "Multisig", "Multisigs", account, hash)
	if err != nil {
		return result, err
	}
	result.PendingKey = key.Hex()
	if err := self.client.callWithStorageAbsence(ctx, "state_getStorage", []any{key.Hex(), block}, &result.PendingStorage, true); err != nil {
		return result, err
	}
	specs := []rootStorageSpec{
		{name: "SubnetOwner", keys: []string{"u16"}, hashers: []string{"identity"}, value: "account"},
		{name: "MaxAllowedUids", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	}
	entries, err := observationStorageProfile(metadata, specs)
	if err != nil {
		return result, fmt.Errorf("%w: %v", errRpcIntegrity, err)
	}
	reader := rootStorageReader{client: self.client, metadata: metadata, entries: entries, specs: specs, block: block, valueKVs: map[string]rootStorageValue{}}
	netuid := binary.LittleEndian.AppendUint16(nil, action.Netuid)
	for _, spec := range specs {
		if _, err := reader.read(ctx, spec.name, netuid); err != nil {
			return result, err
		}
	}
	result.Storage = reader.evidence()
	if _, err := result.facts(action); err != nil {
		return result, fmt.Errorf("%w: %v", errRpcIntegrity, err)
	}
	return result, nil
}

// The outer signatory's nonce and spendable free balance at one block.
func (self *ownerTrimCanonicalChain) multisigSigner(ctx context.Context, metadata *types.Metadata, action ownerTrimAction, block string) (uint32, uint64, error) {
	if err := rootAccountProfile(metadata); err != nil {
		return 0, 0, err
	}
	account, _ := hex.DecodeString(action.signer()[2:])
	raw, exists, err := self.storage(ctx, metadata, "System", "Account", block, account)
	if err != nil {
		return 0, 0, err
	}
	if !exists || len(raw) != 56 {
		return 0, 0, errors.New("owner trim multisig signatory account is unfunded or its layout changed")
	}
	free, frozen := binary.LittleEndian.Uint64(raw[16:24]), binary.LittleEndian.Uint64(raw[32:40])
	spendable := uint64(0)
	if free > frozen {
		spendable = free - frozen
	}
	return binary.LittleEndian.Uint32(raw[:4]), spendable, nil
}

// A finalized hash is used only while it is still the canonical latest head.
func (self *ownerTrimCanonicalChain) multisigHeadUnchanged(ctx context.Context, number uint64, hash string) error {
	var canonical, latest string
	if err := self.client.call(ctx, "chain_getBlockHash", []any{number}, &canonical); err != nil {
		return err
	}
	if err := self.client.call(ctx, "chain_getFinalizedHead", []any{}, &latest); err != nil {
		return err
	}
	if canonical != hash || latest != hash {
		return errors.New("owner trim multisig finalized head changed; reconcile before any effect")
	}
	return nil
}

// The original pending operation must still carry the first approval's exact
// timepoint and depositor. A final approval needs a remaining approval slot.
func ownerTrimMultisigPendingAdmits(action ownerTrimAction, facts ownerTrimMultisigFacts, depositor string) error {
	multisig := action.Multisig
	if facts.SubnetOwner != multisig.AccountId {
		return errors.New("owner trim multisig readback found another subnet owner")
	}
	pending := facts.Pending
	switch multisig.kind() {
	case "first":
		if pending != nil {
			return errors.New("owner trim multisig operation is already pending; retain its original timepoint")
		}
		return nil
	case "final":
		if pending == nil || pending.Timepoint != *multisig.Timepoint || pending.Depositor != depositor || !slices.Contains(pending.Approvals, depositor) ||
			slices.Contains(pending.Approvals, multisig.Signatory) || len(pending.Approvals)+1 < int(multisig.Threshold) {
			return errors.New("owner trim multisig final approval lacks the original pending timepoint, depositor or a remaining approval")
		}
		return nil
	}
	if pending == nil || pending.Timepoint != *multisig.Timepoint || pending.Depositor != multisig.Signatory || depositor != multisig.Signatory {
		return errors.New("owner trim multisig cancellation requires the original pending timepoint and depositor")
	}
	return nil
}

// Planning observation retained for independent review of a later step.
type ownerTrimMultisigPlanObservation struct {
	FinalizedNumber    uint64                    `json:"finalized_number"`
	FinalizedHash      string                    `json:"finalized_hash"`
	Readback           ownerTrimMultisigReadback `json:"readback"`
	SignatoryNonce     uint32                    `json:"signatory_nonce"`
	SignatorySpendable uint64                    `json:"signatory_spendable_rao"`
}

// Read back the original pending operation, owner and signer nonce at the
// current finalized head before a later step can be approved.
func (self *ownerTrimCanonicalChain) planMultisigStep(ctx context.Context, action ownerTrimAction, depositor string) (ownerTrimMultisigPlanObservation, error) {
	var result ownerTrimMultisigPlanObservation
	if ctx == nil || action.Multisig == nil || action.Multisig.kind() == "first" {
		return result, errors.New("owner trim multisig planning requires a later step and context")
	}
	operationCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if err := self.network(operationCtx); err != nil {
		return result, err
	}
	var finalized string
	if err := self.client.call(operationCtx, "chain_getFinalizedHead", []any{}, &finalized); err != nil {
		return result, err
	}
	_, number, err := self.header(operationCtx, finalized)
	if err != nil {
		return result, err
	}
	var birth string
	if err := self.client.call(operationCtx, "chain_getBlockHash", []any{action.BirthBlock}, &birth); err != nil {
		return result, err
	}
	if action.BirthBlock > number || birth != action.BirthHash || uint64(action.Multisig.Timepoint.Height) > number {
		return result, errors.New("owner trim multisig step era anchor or timepoint is not finalized canonical history")
	}
	runtime, err := self.nativeRuntimeAt(operationCtx, finalized)
	if err != nil {
		return result, err
	}
	readback, err := self.readOwnerTrimMultisigState(operationCtx, runtime.metadata, action, finalized, number)
	if err != nil {
		return result, err
	}
	facts, _ := readback.facts(action)
	if err := ownerTrimMultisigPendingAdmits(action, facts, depositor); err != nil {
		return result, err
	}
	nonce, spendable, err := self.multisigSigner(operationCtx, runtime.metadata, action, finalized)
	if err != nil {
		return result, err
	}
	if nonce != action.Nonce {
		return result, errors.New("owner trim multisig step nonce differs from the signatory's finalized nonce")
	}
	if err := self.multisigHeadUnchanged(operationCtx, number, finalized); err != nil {
		return result, err
	}
	return ownerTrimMultisigPlanObservation{FinalizedNumber: number, FinalizedHash: finalized, Readback: readback, SignatoryNonce: nonce, SignatorySpendable: spendable}, operationCtx.Err()
}

// Recheck exact finalized state before one numbered post. Approvals keep the
// best-effort census and current predicate admission; cancellation needs only
// its original pending operation, depositor, nonce and fee reserve.
func (self *ownerTrimCanonicalChain) admitMultisigStep(ctx context.Context, review ownerTrimPlan, depositor string, step ownerTrimRecord, approval ownerTrimBestEffortApproval, evidence ownerTrimActionReconciliation) error {
	action, observation := step.Config.Action, evidence.Observation
	if ctx == nil || action.Multisig == nil || rootObjectHash(step.Config) != rootObjectHash(self.config) || evidence.Receipt != nil || observation.AccountNonce == nil ||
		*observation.AccountNonce != action.Nonce || observation.FinalizedNumber >= action.BirthBlock+action.Period ||
		observation.FinalizedNumber < approval.ValidFromBlock || observation.FinalizedNumber > approval.ValidThroughBlock {
		return errors.New("owner trim multisig admission lacks the exact step, signatory nonce or approved original window")
	}
	operationCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	runtime, err := self.nativeRuntimeAt(operationCtx, observation.FinalizedHash)
	if err != nil {
		return err
	}
	deposit, err := ownerTrimMultisigProfile(runtime.metadata, len(action.Multisig.Signatories), action.Multisig.Threshold)
	if err != nil {
		return err
	}
	readback, err := self.readOwnerTrimMultisigState(operationCtx, runtime.metadata, action, observation.FinalizedHash, observation.FinalizedNumber)
	if err != nil {
		return err
	}
	facts, _ := readback.facts(action)
	if err := ownerTrimMultisigPendingAdmits(action, facts, depositor); err != nil {
		return err
	}
	nonce, spendable, err := self.multisigSigner(operationCtx, runtime.metadata, action, observation.FinalizedHash)
	if err != nil {
		return err
	}
	required := action.FeeReserveRao
	if action.Multisig.kind() == "first" {
		if deposit > action.Multisig.DepositLimitRao || deposit > math.MaxUint64-required {
			return errors.New("owner trim multisig deposit exceeds its approved limit")
		}
		required += deposit
	}
	if nonce != action.Nonce || spendable < required {
		return errors.New("owner trim multisig signatory nonce changed or it lacks the approved fee and deposit reserve")
	}
	if action.Multisig.kind() != "cancel" {
		if err := self.bestEffortCurrentAdmits(operationCtx, self.policy, review, approval, action, observation); err != nil {
			return err
		}
	}
	if err := self.multisigHeadUnchanged(operationCtx, observation.FinalizedNumber, observation.FinalizedHash); err != nil {
		return err
	}
	return operationCtx.Err()
}
