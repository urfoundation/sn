// Canonical reconciliation of each multisig step uses locally served native
// headers, bodies, events and exact-block storage. All state is synthetic.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// One included step and the controls a scenario may change before reading.
type ownerTrimMultisigReceiptFixture struct {
	chain     *ownerTrimCanonicalChain
	native    *rootReceiptFixture
	config    ownerTrimExecutionConfig
	raw       []byte
	signers   ownerTrimMultisigTestSigners
	timepoint treasuryTimepoint
}

// The step is signed by its synthetic signatory and included at birth+1, index 1.
// Events are built from the authenticated runtime registry for that block.
func newOwnerTrimMultisigReceiptFixture(t *testing.T, kind, event string, result []byte) *ownerTrimMultisigReceiptFixture {
	t.Helper()
	native, fixture := newRootReceiptFixture(t, 2, false)
	f := newOwnerTrimActionTestFixture(t)
	f.config.Action.BirthHash = fixture.byHeight[f.config.Action.BirthBlock]
	signers := newOwnerTrimMultisigTestSigners(t)
	ownerTrimMultisigTestConfig(t, f, signers, signers.accounts[2])
	config := f.config
	birth := config.Action.BirthBlock
	timepoint := treasuryTimepoint{Height: uint32(birth) - 5, Index: 1}
	if kind != "first" {
		operation, signatory := "as_multi", signers.accounts[0]
		if kind == "cancel" {
			operation, signatory = "cancel_as_multi", signers.accounts[2]
		}
		template := ownerTrimMultisigStepTemplate{Operation: operation, Signatory: signatory, DerivationPath: "m/44'/354'/1'/0'/0'", Nonce: 4,
			BirthBlock: birth, BirthHash: fixture.byHeight[birth], Period: 8, FeeReserveRao: 1_000_000, MaxBroadcasts: 2}
		if operation == "as_multi" {
			template.MaxRefTime, template.MaxProofSize = 1_000_000_000, 65_536
		}
		var err error
		config, err = ownerTrimMultisigStepConfig(f.config, template, timepoint)
		if err == nil {
			config.Action, err = prepareOwnerTrimAction(config.Action, f.metadata)
		}
		if err != nil {
			t.Fatal(err)
		}
	} else {
		timepoint = treasuryTimepoint{Height: uint32(birth) + 1, Index: 1}
	}
	config.Route.RpcUrl = native.client.url
	config.Signature = hex.EncodeToString(ed25519.Sign(f.approval, config.signingBytes()))
	action := config.Action
	payload, _ := hex.DecodeString(action.Payload[2:])
	raw, err := action.signed(ed25519.Sign(signers.keys[action.Multisig.Signatory], payload))
	if err != nil {
		t.Fatal(err)
	}
	parent := action.BirthHash
	for height := birth + 1; height <= birth+2; height++ {
		body := [][]byte{}
		if height == birth+1 {
			body = append(body, []byte{8, 4, 0}, raw)
		}
		header, hash := rootReceiptHeaderFixture(t, parent, height, body, false)
		fixture.headers[hash], fixture.byHeight[height], fixture.bodies[hash] = header, hash, []string{}
		for _, extrinsic := range body {
			fixture.bodies[hash] = append(fixture.bodies[hash], "0x"+hex.EncodeToString(extrinsic))
		}
		parent = hash
	}
	fixture.finalized = parent
	signer, _ := hex.DecodeString(action.Multisig.Signatory[2:])
	owner, _ := hex.DecodeString(action.Coldkey[2:])
	callHash, _ := hex.DecodeString(action.Multisig.CallHash[2:])
	point := binary.LittleEndian.AppendUint32(nil, timepoint.Height)
	point = binary.LittleEndian.AppendUint32(point, timepoint.Index)
	events := append([]byte{12}, rootReceiptEventFixture(t, fixture.metadata, "TransactionPayment.TransactionFeePaid", 1, signer, binary.LittleEndian.AppendUint64(nil, 17), make([]byte, 8))...)
	events = append(events, rootReceiptEventFixture(t, fixture.metadata, "System.ExtrinsicSuccess", 1)...)
	switch event {
	case "Multisig.NewMultisig":
		events = append(events, rootReceiptEventFixture(t, fixture.metadata, event, 1, signer, owner, callHash)...)
	case "Multisig.MultisigExecuted":
		events = append(events, rootReceiptEventFixture(t, fixture.metadata, event, 1, signer, point, owner, callHash, result)...)
	default:
		events = append(events, rootReceiptEventFixture(t, fixture.metadata, event, 1, signer, point, owner, callHash)...)
	}
	set := func(pallet, name string, value []byte, args ...[]byte) {
		key, err := types.CreateStorageKey(fixture.metadata, pallet, name, args...)
		if err != nil {
			t.Fatal(err)
		}
		if value == nil {
			delete(fixture.storageKVs, key.Hex())
		} else {
			fixture.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(value)
		}
	}
	set("System", "Events", events)
	account := make([]byte, 56)
	binary.LittleEndian.PutUint32(account, action.Nonce+1)
	set("System", "Account", account, signer)
	set("SubtensorModule", "SubnetOwner", owner, []byte{25, 0})
	set("SubtensorModule", "MaxAllowedUids", binary.LittleEndian.AppendUint16(nil, action.MaximumUids), []byte{25, 0})
	if kind == "first" {
		set("Multisig", "Multisigs", ownerTrimMultisigTestPending(timepoint, 196_000_000, action.Multisig.Signatory), owner, callHash)
	}
	fixture.fault = func(method string, _ []json.RawMessage, _ int) (any, bool) {
		if method == "system_version" {
			return "synthetic-receipt-node", true
		}
		return nil, false
	}
	_, _, policy := newSubnetFixture(t)
	policy.NativeChain, policy.GenesisHash, policy.EvmChainId = action.Network.NativeChain, action.Network.GenesisHash, action.Network.EvmChainId
	policy.RuntimeSourceCommit, policy.RuntimeVersion, policy.RuntimeCodeHash, policy.RuntimeMetadataHash = action.Runtime.RuntimeSourceCommit, action.Runtime.RuntimeVersion, action.Runtime.RuntimeCodeHash, action.Runtime.RuntimeMetadataHash
	policy.SubnetOwnerColdkey, policy.SubnetRegistrationBlock, policy.SubnetGeneration = action.Coldkey, new(action.SubnetRegistrationBlock), new(action.SubnetGeneration)
	chain, err := newOwnerTrimCanonicalChain(config, f.key, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chain.client.httpClient.CloseIdleConnections)
	return &ownerTrimMultisigReceiptFixture{chain: chain, native: fixture, config: config, raw: raw, signers: signers, timepoint: timepoint}
}

// pallet_multisig::Multisig approved by the original depositor and any others,
// with approvals in the runtime's sorted order.
func ownerTrimMultisigTestPending(timepoint treasuryTimepoint, deposit uint64, depositor string, others ...string) []byte {
	raw := binary.LittleEndian.AppendUint32(nil, timepoint.Height)
	raw = binary.LittleEndian.AppendUint32(raw, timepoint.Index)
	raw = binary.LittleEndian.AppendUint64(raw, deposit)
	account, _ := hex.DecodeString(depositor[2:])
	raw = append(raw, account...)
	approvals := append([]string{depositor}, others...)
	slices.Sort(approvals)
	raw = append(raw, rootCompact(uint64(len(approvals)))...)
	for _, approval := range approvals {
		decoded, _ := hex.DecodeString(approval[2:])
		raw = append(raw, decoded...)
	}
	return raw
}

// A successful first approval yields NewMultisig, its exact timepoint and the
// pending operation read back at the inclusion block; it never claims a trim.
func TestOwnerTrimMultisigReceiptFirstApprovalRecordsTimepoint(t *testing.T) {
	f := newOwnerTrimMultisigReceiptFixture(t, "first", "Multisig.NewMultisig", nil)
	evidence, err := f.chain.reconcile(t.Context(), f.config.Action, f.raw)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Receipt == nil || evidence.Receipt.ExtrinsicIndex != 1 || evidence.Receipt.ActualFeeRao != 17 || evidence.Multisig == nil ||
		evidence.Multisig.Dispatch.Kind != "opened" || *evidence.Multisig.Dispatch.Timepoint != f.timepoint || evidence.Census != nil ||
		ownerTrimMultisigPhase(f.config.Action, evidence, true) != "approval-recorded" {
		t.Fatalf("first approval outcome differs: %+v %+v", evidence.Receipt, evidence.Multisig)
	}
	facts, err := evidence.Multisig.Readback.facts(f.config.Action)
	if err != nil || facts.Pending == nil || facts.Pending.Depositor != f.config.Action.Multisig.Signatory || facts.SubnetOwner != f.config.Action.Coldkey {
		t.Fatal("pending operation readback lost its depositor or owner", err)
	}
	// Without the pending row the opening cannot be recorded as an approval.
	owner, _ := hex.DecodeString(f.config.Action.Coldkey[2:])
	callHash, _ := hex.DecodeString(f.config.Action.Multisig.CallHash[2:])
	key, _ := types.CreateStorageKey(f.native.metadata, "Multisig", "Multisigs", owner, callHash)
	f.native.stateLock.Lock()
	delete(f.native.storageKVs, key.Hex())
	f.native.stateLock.Unlock()
	evidence, err = f.chain.reconcile(t.Context(), f.config.Action, f.raw)
	if err != nil || ownerTrimMultisigPhase(f.config.Action, evidence, true) != "finalized-state-conflict" {
		t.Fatal("first approval without a pending operation was recorded", err)
	}
	if f.native.counts["author_submitExtrinsic"] != 0 {
		t.Fatal("reconciliation performed a network write")
	}
}

// MultisigExecuted with Ok plus exact capacity readback is execution; an Err
// inner result is a failed trim even though the outer extrinsic succeeded.
func TestOwnerTrimMultisigReceiptFinalExecutionRequiresOkInnerResultAndCapacity(t *testing.T) {
	f := newOwnerTrimMultisigReceiptFixture(t, "final", "Multisig.MultisigExecuted", []byte{0})
	evidence, err := f.chain.reconcile(t.Context(), f.config.Action, f.raw)
	if err != nil || evidence.Multisig == nil || !evidence.Multisig.Dispatch.InnerSuccess || ownerTrimMultisigPhase(f.config.Action, evidence, true) != "executed" ||
		evidence.Census == nil || evidence.Census.Issue == "" {
		t.Fatalf("final execution differs or invented census correspondence: %+v %v", evidence.Multisig, err)
	}
	key, _ := types.CreateStorageKey(f.native.metadata, "SubtensorModule", "MaxAllowedUids", []byte{25, 0})
	f.native.stateLock.Lock()
	f.native.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(binary.LittleEndian.AppendUint16(nil, f.config.Action.MaximumUids+1))
	f.native.stateLock.Unlock()
	evidence, err = f.chain.reconcile(t.Context(), f.config.Action, f.raw)
	if err != nil || ownerTrimMultisigPhase(f.config.Action, evidence, true) != "finalized-state-conflict" {
		t.Fatal("execution without the approved capacity readback was accepted", err)
	}
	failed := newOwnerTrimMultisigReceiptFixture(t, "final", "Multisig.MultisigExecuted", []byte{1, 0})
	evidence, err = failed.chain.reconcile(t.Context(), failed.config.Action, failed.raw)
	if err != nil || evidence.Receipt == nil || !evidence.Receipt.Success || evidence.Multisig.Dispatch.InnerSuccess || evidence.Multisig.Dispatch.InnerError == "" ||
		ownerTrimMultisigPhase(failed.config.Action, evidence, true) != "inner-dispatch-failed" {
		t.Fatalf("outer success promoted an Err inner trim result: %+v %v", evidence.Multisig, err)
	}
	approval := newOwnerTrimMultisigReceiptFixture(t, "final", "Multisig.MultisigApproval", nil)
	evidence, err = approval.chain.reconcile(t.Context(), approval.config.Action, approval.raw)
	if err != nil || ownerTrimMultisigPhase(approval.config.Action, evidence, true) != "finalized-state-conflict" {
		t.Fatal("a non-executing final approval was accepted", err)
	}
}

// The original depositor's cancellation releases the pending operation only
// when MultisigCancelled matches and the row is gone at the inclusion block.
func TestOwnerTrimMultisigReceiptCancellationReleasesOperation(t *testing.T) {
	f := newOwnerTrimMultisigReceiptFixture(t, "cancel", "Multisig.MultisigCancelled", nil)
	evidence, err := f.chain.reconcile(t.Context(), f.config.Action, f.raw)
	if err != nil || evidence.Multisig == nil || evidence.Multisig.Dispatch.Kind != "cancelled" || ownerTrimMultisigPhase(f.config.Action, evidence, true) != "cancelled" {
		t.Fatalf("depositor cancellation differs: %+v %v", evidence.Multisig, err)
	}
	owner, _ := hex.DecodeString(f.config.Action.Coldkey[2:])
	callHash, _ := hex.DecodeString(f.config.Action.Multisig.CallHash[2:])
	key, _ := types.CreateStorageKey(f.native.metadata, "Multisig", "Multisigs", owner, callHash)
	f.native.stateLock.Lock()
	f.native.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(ownerTrimMultisigTestPending(f.timepoint, 196_000_000, f.config.Action.Multisig.Signatory))
	f.native.stateLock.Unlock()
	evidence, err = f.chain.reconcile(t.Context(), f.config.Action, f.raw)
	if err != nil || ownerTrimMultisigPhase(f.config.Action, evidence, true) != "finalized-state-conflict" {
		t.Fatal("cancellation with a remaining pending row was accepted", err)
	}
}

// Events for another timepoint, call hash or signatory cannot settle a step.
func TestOwnerTrimMultisigReceiptRejectsWrongTimepointCallHashAndSignatory(t *testing.T) {
	for kind, reason := range map[string]string{"timepoint": "changed the original timepoint", "call-hash": "inner call hash differs", "signatory": "names another signatory"} {
		f := newOwnerTrimMultisigReceiptFixture(t, "final", "Multisig.MultisigExecuted", []byte{0})
		action := f.config.Action
		signer, _ := hex.DecodeString(action.Multisig.Signatory[2:])
		owner, _ := hex.DecodeString(action.Coldkey[2:])
		callHash, _ := hex.DecodeString(action.Multisig.CallHash[2:])
		point := binary.LittleEndian.AppendUint32(nil, f.timepoint.Height)
		point = binary.LittleEndian.AppendUint32(point, f.timepoint.Index)
		switch kind {
		case "timepoint":
			point = binary.LittleEndian.AppendUint32(nil, f.timepoint.Height-1)
			point = binary.LittleEndian.AppendUint32(point, f.timepoint.Index)
		case "call-hash":
			callHash = bytes.Repeat([]byte{0xcd}, 32)
		case "signatory":
			signer, _ = hex.DecodeString(f.signers.accounts[1][2:])
		}
		fee, _ := hex.DecodeString(action.Multisig.Signatory[2:])
		events := append([]byte{12}, rootReceiptEventFixture(t, f.native.metadata, "TransactionPayment.TransactionFeePaid", 1, fee, binary.LittleEndian.AppendUint64(nil, 17), make([]byte, 8))...)
		events = append(events, rootReceiptEventFixture(t, f.native.metadata, "System.ExtrinsicSuccess", 1)...)
		events = append(events, rootReceiptEventFixture(t, f.native.metadata, "Multisig.MultisigExecuted", 1, signer, point, owner, callHash, []byte{0})...)
		key, _ := types.CreateStorageKey(f.native.metadata, "System", "Events")
		f.native.stateLock.Lock()
		f.native.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(events)
		f.native.stateLock.Unlock()
		if evidence, err := f.chain.reconcile(t.Context(), action, f.raw); err == nil || !strings.Contains(err.Error(), reason) || evidence.Receipt != nil {
			t.Fatal(kind, "mismatched multisig event settled the step", err)
		}
	}
}

// Phases come only from retained evidence: a missing readback stays pending,
// and fee overrun is reported without stranding an opened operation.
func TestOwnerTrimMultisigPhaseKeepsReadbackGapAndFeeSeparate(t *testing.T) {
	f := newOwnerTrimMultisigReceiptFixture(t, "first", "Multisig.NewMultisig", nil)
	evidence, err := f.chain.reconcile(t.Context(), f.config.Action, f.raw)
	if err != nil {
		t.Fatal(err)
	}
	gap := ownerTrimTestCopy(t, evidence)
	gap.Multisig.Readback, gap.Multisig.ReadbackIssue = nil, "synthetic transient readback gap"
	if gap.validate(f.config.Action, f.raw) != nil || ownerTrimMultisigPhase(f.config.Action, gap, true) != "finalized-readback-pending" || ownerTrimMultisigSettled("finalized-readback-pending") {
		t.Fatal("transient readback gap was settled")
	}
	costly := ownerTrimTestCopy(t, evidence)
	costly.Receipt.ActualFeeRao = f.config.Action.FeeReserveRao + 1
	if ownerTrimMultisigPhase(f.config.Action, costly, true) != "approval-recorded" {
		t.Fatal("fee overrun stranded an opened multisig operation")
	}
	forged := ownerTrimTestCopy(t, evidence)
	forged.Multisig.Events = "0x00"
	if forged.validate(f.config.Action, f.raw) == nil {
		t.Fatal("retained events no longer bound by the receipt event hash")
	}
}
