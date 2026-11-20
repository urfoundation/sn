// Public commands and canonical receipt adapters exercise actual custody with
// synthetic native signatures and explicitly controlled state transitions.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
	"golang.org/x/crypto/blake2b"
	"gopkg.in/yaml.v3"
)

// Strict public YAML cannot introduce seeds, alternate accounts or extra docs.
func TestTreasuryPublicDescriptorRejectsPrivateOrAmbiguousCustody(t *testing.T) {
	f := newTreasuryFixture(t)
	raw, err := yaml.Marshal(f.config.Action.Descriptor)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(f.config.Action.StatePath)
	path, pin := ownerRecycleTestFile(t, dir, "custody.yml", raw)
	var out, diagnostic bytes.Buffer
	if code := runMain(t.Context(), []string{"treasury", "describe", "--custody", path, "--custody-sha256", pin}, &out, &diagnostic); code != 0 {
		t.Fatal(code, diagnostic.String())
	}
	var got treasuryDescriptor
	if err := decodePlanJson(out.Bytes(), &got); err != nil || rootObjectHash(got) != rootObjectHash(f.config.Action.Descriptor) {
		t.Fatal("public descriptor changed", err)
	}
	for _, changed := range [][]byte{append(bytes.Clone(raw), []byte("seed: forbidden\n")...), append(bytes.Clone(raw), []byte("schema: duplicate\n")...), append(bytes.Clone(raw), []byte("---\n{}\n")...), bytes.Replace(raw, []byte("threshold: 2"), []byte("threshold: 1"), 1), bytes.Replace(raw, []byte(f.config.Action.Descriptor.Multisig.AccountId), []byte("0x"+strings.Repeat("99", 32)), 1)} {
		if _, err := decodeTreasuryDescriptor(changed); err == nil {
			t.Fatal("ambiguous/private custody admitted")
		}
	}
	input := f.input
	input.Action.Descriptor.Multisig.Threshold = 0
	path, _ = treasuryTestJson(t, dir, "invalid-threshold.json", input)
	out.Reset()
	diagnostic.Reset()
	if code := runTreasuryCommand(t.Context(), []string{"plan", "--input", path}, &out, &diagnostic); code == 0 || out.Len() != 0 {
		t.Fatal("malformed public plan acquired multisig authority", code, diagnostic.String())
	}
}

// The independent GSRPC call encoder verifies inline call and weight semantics.
func TestTreasuryNativeMultisigMatchesTypedRuntimeCall(t *testing.T) {
	f := newTreasuryFixture(t)
	a := f.config.Action
	hotkeyRaw, _ := hex.DecodeString(a.Inner.Hotkey[2:])
	hotkey, _ := types.NewAccountID(hotkeyRaw)
	inner, err := types.NewCall(f.metadata, "SubtensorModule.register_limit", types.U16(25), *hotkey, types.U64(a.Inner.LimitPrice))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := codec.Encode(inner)
	expected, innerErr := a.innerCall()
	if err != nil || innerErr != nil || !bytes.Equal(encoded, expected) {
		t.Fatal("native bounded registration codec differs", err, innerErr)
	}
	others := []types.AccountID{}
	for _, s := range a.Descriptor.Multisig.Signatories {
		if s.AccountId != a.Owner {
			raw, _ := hex.DecodeString(s.AccountId[2:])
			id, _ := types.NewAccountID(raw)
			others = append(others, *id)
		}
	}
	weight := types.NewWeight(types.NewUCompactFromUInt(a.MaxRefTime), types.NewUCompactFromUInt(a.MaxProofSize))
	for _, operation := range []string{"approve_as_multi", "as_multi", "cancel_as_multi"} {
		a.Operation = operation
		var call types.Call
		switch operation {
		case "approve_as_multi":
			hash, _ := types.NewHashFromHexString(rootExtrinsicHash(expected))
			call, err = types.NewCall(f.metadata, "Multisig.approve_as_multi", types.U16(2), others, types.BytesBare{0}, hash, weight)
		case "as_multi":
			call, err = types.NewCall(f.metadata, "Multisig.as_multi", types.U16(2), others, types.BytesBare{0}, inner, weight)
		case "cancel_as_multi":
			a.Timepoint = &treasuryTimepoint{Height: 90, Index: 2}
			hash, _ := types.NewHashFromHexString(rootExtrinsicHash(expected))
			call, err = types.NewCall(f.metadata, "Multisig.cancel_as_multi", types.U16(2), others, types.TimePoint{Height: 90, Index: 2}, hash)
		}
		if err != nil {
			t.Fatal(err)
		}
		typed, err := codec.Encode(call)
		manual, _, encodeErr := a.encoding()
		if err != nil || encodeErr != nil || !bytes.Equal(typed, manual) {
			t.Fatal(operation, "outer encoding differs", err, encodeErr)
		}
	}
	// A wider, still bounded signer set crosses Substrate's payload boundary.
	a = f.config.Action
	for _, label := range []string{"signatory four", "signatory five", "signatory six"} {
		key := treasuryTestKey(label)
		s := a.Descriptor.Multisig.Signatories[0]
		s.AccountId = "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey))
		a.Descriptor.Multisig.Signatories = append(a.Descriptor.Multisig.Signatories, s)
	}
	sort.Slice(a.Descriptor.Multisig.Signatories, func(i, j int) bool {
		return a.Descriptor.Multisig.Signatories[i].AccountId < a.Descriptor.Multisig.Signatories[j].AccountId
	})
	accounts := make([][32]byte, len(a.Descriptor.Multisig.Signatories))
	for i, signer := range a.Descriptor.Multisig.Signatories {
		raw, _ := hex.DecodeString(signer.AccountId[2:])
		copy(accounts[i][:], raw)
	}
	multisig, err := crv4.DeriveNativeMultisigAccount(accounts, a.Descriptor.Multisig.Threshold)
	if err != nil {
		t.Fatal(err)
	}
	a.Descriptor.Multisig.AccountId = "0x" + hex.EncodeToString(multisig[:])
	call, payload, err := a.encoding()
	if err != nil || len(payload) <= 256 {
		t.Fatal("fixture did not cross native signing boundary", len(payload), err)
	}
	a.Call, a.Payload, a.RequestHash = "0x"+hex.EncodeToString(call), "0x"+hex.EncodeToString(payload), ""
	a.RequestHash = rootObjectHash(a)
	digest := blake2b.Sum256(payload)
	if !bytes.Equal(a.signingBytes(), digest[:]) {
		t.Fatal("large multisig payload did not use native Blake2-256 signing")
	}
	if _, err := a.signed(ed25519.Sign(f.signer, payload)); err == nil {
		t.Fatal("full large payload signature replaced native digest signature")
	}
	signed, err := a.signed(ed25519.Sign(f.signer, digest[:]))
	if err != nil || treasurySignedAction(a, signed) != nil {
		t.Fatal("native digest signature did not retain exact extrinsic", err)
	}
}

// Removed durable state cannot become an unused nonce or signing allowance.
func TestTreasuryPublicCustodyRetainsExactRequestAndReply(t *testing.T) {
	f := newTreasuryFixture(t)
	dir := filepath.Dir(f.config.Action.StatePath)
	configPath, _ := treasuryTestJson(t, dir, "approved.json", f.config)
	metadataPath, _ := ownerRecycleTestFile(t, dir, "metadata.hex", []byte(f.input.Metadata))
	ledgerPath, _ := ownerRecycleTestFile(t, dir, "ledger.hex", []byte(f.input.LedgerMetadata))
	common := []string{"--config", configPath, "--approval-key", f.key, "--accept-action-hash", f.config.Action.RequestHash, "--production-authority-hash", f.config.AuthorityHash}
	run := func(mode string, extra ...string) []byte {
		t.Helper()
		var out, diagnostic bytes.Buffer
		args := append([]string{mode}, common...)
		args = append(args, extra...)
		if code := runTreasuryCommand(f.storage.Context, args, &out, &diagnostic); code != 0 {
			t.Fatal(mode, code, diagnostic.String())
		}
		return bytes.Clone(out.Bytes())
	}
	run("reserve")
	raw := run("export", "--metadata", metadataPath, "--ledger-metadata", ledgerPath)
	if !bytes.Equal(raw, run("export", "--metadata", metadataPath, "--ledger-metadata", ledgerPath)) {
		t.Fatal("lost output changed request")
	}
	var request treasurySigningRequest
	if err := decodePlanJson(raw, &request); err != nil {
		t.Fatal(err)
	}
	reply, err := newTreasurySigningReply(request, ed25519.Sign(f.signer, f.config.Action.signingBytes()))
	if err != nil {
		t.Fatal(err)
	}
	replyPath, replyHash := treasuryTestJson(t, dir, "reply.json", reply)
	run("import-reply", "--reply", replyPath, "--reply-sha256", replyHash)
	run("import-reply", "--reply", replyPath, "--reply-sha256", replyHash)
	var record treasuryRecord
	if err := decodePlanJson(run("status"), &record); err != nil || record.Phase != "signed" || record.RawExtrinsic != reply.RawExtrinsic || record.Attempts != 0 {
		t.Fatal("original public signature not retained", err)
	}
	if err := os.Remove(f.config.Action.StatePath); err != nil {
		t.Fatal(err)
	}
	if store, err := openTreasuryStore(f.config, f.key, false, f.storage.Context); err == nil {
		store.close()
		t.Fatal("lost custody became a fresh allowance")
	}
}

// Exact descriptor bytes are checked before hardware; unknown issuance is never
// replayed even after the owner process reopens its durable original journal.
func TestTreasuryPublicLedgerReferenceAndUnknownIssuance(t *testing.T) {
	f := newTreasuryFixture(t)
	device := ownerSigningTestDeviceConfig(t)
	ctx := ownerLocalDurableTestContext(t, filepath.Dir(device.StatePath))
	public := treasuryLedgerConfig{Schema: "urnetwork-native-treasury-ledger-device-v1", AccountId: f.config.Action.Owner, DerivationPath: f.config.Action.DerivationPath, PythonPath: device.PythonPath, HelperPath: device.HelperPath, HelperHash: device.HelperHash, BackendPath: device.BackendPath, BackendHash: device.BackendHash, AppVersion: device.AppVersion}
	raw, _ := json.Marshal(public)
	devicePath, _ := ownerRecycleTestFile(t, filepath.Dir(device.StatePath), "public-ledger.json", raw)
	digest := sha256.Sum256(raw)
	for i := range f.input.Action.Descriptor.Multisig.Signatories {
		if f.input.Action.Descriptor.Multisig.Signatories[i].AccountId == f.config.Action.Owner {
			f.input.Action.Descriptor.Multisig.Signatories[i].DeviceConfig = treasuryDeviceReference{Path: devicePath, Bytes: uint64(len(raw)), Sha256: "0x" + hex.EncodeToString(digest[:])}
		}
	}
	f.input.Observation = f.observation(t, f.input.Action, f.input.Action.BirthBlock, f.input.Action.BirthHash)
	f.input.Action.ObservationHash = f.input.Observation.ContentHash
	f.replan(t)
	request := f.request(t)
	requestPath, _ := treasuryTestJson(t, filepath.Dir(device.StatePath), "request.json", request)
	args := []string{"sign", "--request", requestPath, "--accept-request-hash", request.ContentHash, "--approval-key", f.key, "--signatory-account-id", f.config.Action.Owner, "--expected-genesis", f.config.Action.Policy.GenesisHash, "--device-config", devicePath, "--owner-state", device.StatePath}
	prepares, signs := 0, 0
	adapter := func(_ context.Context, c ownerSigningDeviceConfig, input ownerSigningAdapterInput) (ownerSigningAdapterResult, error) {
		result := ownerSigningAdapterResult{Schema: ownerSigningAdapterSchema, Mode: input.Mode, RequestHash: request.ContentHash, SourceCommit: rootActionV1Source, MetadataDigest: f.config.Action.MetadataDigest, ProofHash: rootObjectHash("synthetic treasury RFC78 proof")}
		if input.Call+input.IncludedExtrinsic[2:]+input.IncludedSignedData[2:] != f.config.Action.Payload {
			t.Fatal("Ledger payload seams differ")
		}
		if input.Mode == "prepare" {
			prepares++
			return result, nil
		}
		signs++
		retained, err := os.ReadFile(c.StatePath)
		var record ownerSigningDeviceRecord
		if err != nil || decodePlanJson(retained, &record) != nil || record.Phase != "signing" || record.Schema != treasuryDeviceStateSchema {
			t.Fatal("device reached before durable intent", err)
		}
		return result, errors.New("synthetic disconnected Ledger after request issuance")
	}
	changed := bytes.Replace(raw, []byte(public.HelperHash), []byte(rootObjectHash("changed helper")), 1)
	if err := os.WriteFile(devicePath, changed, 0600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code := runTreasuryCommandWithAdapter(ctx, args, &out, &diagnostic, adapter); code == 0 || prepares != 0 || signs != 0 {
		t.Fatal("changed public device file reached hardware", code, diagnostic.String())
	}
	if err := os.WriteFile(devicePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		out.Reset()
		diagnostic.Reset()
		if code := runTreasuryCommandWithAdapter(ctx, args, &out, &diagnostic, adapter); code == 0 {
			t.Fatal("unknown device result invented signature")
		}
	}
	if prepares != 1 || signs != 1 {
		t.Fatal("unknown Ledger issuance repeated", prepares, signs)
	}
	// A response retained after lost stdout resolves the same issuance. Neither
	// recovery nor another read can prompt the hardware for a new signature.
	response := ownerSigningAdapterResult{Schema: ownerSigningAdapterSchema, Mode: "sign", RequestHash: request.ContentHash, SourceCommit: rootActionV1Source, MetadataDigest: f.config.Action.MetadataDigest, ProofHash: rootObjectHash("synthetic treasury RFC78 proof"), PublicKey: f.config.Action.Owner, AppVersion: device.AppVersion, Response: "0x" + hex.EncodeToString(append([]byte{0}, ed25519.Sign(f.signer, f.config.Action.signingBytes())...))}
	retained, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(device.StatePath+".ledger-response", retained, 0600); err != nil {
		t.Fatal(err)
	}
	var original []byte
	for i := 0; i < 2; i++ {
		out.Reset()
		diagnostic.Reset()
		if code := runTreasuryCommandWithAdapter(ctx, args, &out, &diagnostic, adapter); code != 0 {
			t.Fatal("retained original hardware response did not recover", code, diagnostic.String())
		}
		var reply ownerSigningReply
		if err := decodePlanJson(out.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		if _, err := validateTreasurySigningReply(request, reply); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			original = bytes.Clone(out.Bytes())
		} else if !bytes.Equal(original, out.Bytes()) {
			t.Fatal("recovered original hardware reply changed")
		}
	}
	if prepares != 1 || signs != 1 {
		t.Fatal("retained response recovery repeated hardware issuance", prepares, signs)
	}
}

// This local server authenticates real header/body hashes and original events.
func treasuryTestChain(t *testing.T, f *treasuryFixture, kind string) (*treasuryCanonicalChain, *rootReceiptFixture, treasurySigningRequest, []byte) {
	t.Helper()
	a := f.input.Action
	if kind == "inner-failed" || kind == "executed" {
		a.Operation = "as_multi"
		a.Timepoint = &treasuryTimepoint{Height: 90, Index: 2}
		depositor := a.Descriptor.Multisig.Signatories[0].AccountId
		if depositor == a.Owner {
			depositor = a.Descriptor.Multisig.Signatories[1].AccountId
		}
		depositorRaw, _ := hex.DecodeString(depositor[2:])
		pending := binary.LittleEndian.AppendUint32(nil, 90)
		pending = binary.LittleEndian.AppendUint32(pending, 2)
		pending = binary.LittleEndian.AppendUint64(pending, 20)
		pending = append(pending, depositorRaw...)
		pending = append(pending, 4)
		pending = append(pending, depositorRaw...)
		inner, _ := a.innerCall()
		hash, _ := hex.DecodeString(rootExtrinsicHash(inner)[2:])
		multisig, _ := hex.DecodeString(a.Descriptor.Multisig.AccountId[2:])
		f.set(t, "Multisig", "Multisigs", pending, multisig, hash)
	}
	fixture := &rootReceiptFixture{metadata: f.metadata, metadataHex: f.input.Metadata, profile: a.Policy.rootReceiptProfile, headers: map[string]rootReceiptHeader{}, byHeight: map[uint64]string{}, bodies: map[string][]string{}, storageKVs: f.values, evmHex: "0x3c4", runtimeKVs: map[string]rootReceiptProfile{}, metadataKVs: map[string]string{}, counts: map[string]int{}}
	fixture.action.Scope.NativeChain, fixture.action.Scope.GenesisHash = a.Policy.NativeChain, a.Policy.GenesisHash
	anchor, birth := rootReceiptHeaderFixture(t, "0x"+strings.Repeat("9a", 32), a.BirthBlock, nil, false)
	fixture.headers[birth], fixture.byHeight[a.BirthBlock] = anchor, birth
	a.BirthHash = birth
	f.input.Action = a
	f.input.Observation = f.observation(t, a, a.BirthBlock, birth)
	f.input.Action.ObservationHash = f.input.Observation.ContentHash
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	t.Cleanup(server.Close)
	f.input.Route.RpcUrl = server.URL
	f.replan(t)
	a = f.config.Action
	signed, err := a.signed(ed25519.Sign(f.signer, a.signingBytes()))
	if err != nil {
		t.Fatal(err)
	}
	body := [][]byte{signed}
	header, hash := rootReceiptHeaderFixture(t, birth, a.BirthBlock+1, body, false)
	fixture.headers[hash], fixture.byHeight[a.BirthBlock+1], fixture.bodies[hash], fixture.finalized = header, hash, []string{"0x" + hex.EncodeToString(signed)}, hash
	owner, _ := hex.DecodeString(a.Owner[2:])
	multisig, _ := hex.DecodeString(a.Descriptor.Multisig.AccountId[2:])
	inner, _ := a.innerCall()
	callHash, _ := hex.DecodeString(rootExtrinsicHash(inner)[2:])
	events := []byte{12}
	events = append(events, rootReceiptEventFixture(t, f.metadata, "TransactionPayment.TransactionFeePaid", 0, owner, binary.LittleEndian.AppendUint64(nil, 12), make([]byte, 8))...)
	events = append(events, rootReceiptEventFixture(t, f.metadata, "System.ExtrinsicSuccess", 0)...)
	if kind == "opened" {
		events = append(events, rootReceiptEventFixture(t, f.metadata, "Multisig.NewMultisig", 0, owner, multisig, callHash)...)
		pending := binary.LittleEndian.AppendUint32(nil, uint32(a.BirthBlock+1))
		pending = binary.LittleEndian.AppendUint32(pending, 0)
		pending = binary.LittleEndian.AppendUint64(pending, 20)
		pending = append(pending, owner...)
		pending = append(pending, 4)
		pending = append(pending, owner...)
		f.set(t, "Multisig", "Multisigs", pending, multisig, callHash)
	} else {
		point := binary.LittleEndian.AppendUint32(nil, a.Timepoint.Height)
		point = binary.LittleEndian.AppendUint32(point, a.Timepoint.Index)
		result := []byte{0}
		if kind == "inner-failed" {
			result = []byte{1, 0}
		}
		events = append(events, rootReceiptEventFixture(t, f.metadata, "Multisig.MultisigExecuted", 0, owner, point, multisig, callHash, result)...)
		key, _ := types.CreateStorageKey(f.metadata, "Multisig", "Multisigs", multisig, callHash)
		delete(f.values, key.Hex())
		if kind == "executed" {
			recipient, _ := hex.DecodeString(a.Inner.Hotkey[2:])
			f.set(t, "SubtensorModule", "SubnetworkN", []byte{1, 0}, []byte{25, 0})
			f.set(t, "SubtensorModule", "Owner", multisig, recipient)
			f.set(t, "SubtensorModule", "Uids", []byte{0, 0}, []byte{25, 0}, recipient)
			f.set(t, "SubtensorModule", "Keys", recipient, []byte{25, 0}, []byte{0, 0})
			f.set(t, "SubtensorModule", "IsNetworkMember", []byte{1}, recipient, []byte{25, 0})
			f.set(t, "SubtensorModule", "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, a.BirthBlock+1), []byte{25, 0}, []byte{0, 0})
		}
	}
	f.set(t, "System", "Events", events)
	account := make([]byte, 56)
	binary.LittleEndian.PutUint32(account, a.Nonce+1)
	binary.LittleEndian.PutUint64(account[16:], 99000)
	f.set(t, "System", "Account", account, owner)
	chain, err := newTreasuryCanonicalChain(f.config, f.key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chain.client.httpClient.CloseIdleConnections)
	return chain, fixture, f.request(t), signed
}

// A successful first approval reserves an operation and never claims execution.
func TestTreasuryCanonicalFirstApprovalIsNotExecution(t *testing.T) {
	f := newTreasuryFixture(t)
	chain, _, request, signed := treasuryTestChain(t, f, "opened")
	e, err := chain.reconcile(t.Context(), request, signed)
	if err != nil || e.Dispatch == nil || e.Dispatch.Kind != "opened" || treasuryPhase(request, e) != "approval-recorded" {
		t.Fatal("first approval changed into execution", e, err)
	}
}

// Pallet multisig returns outer success even when registration's inner call fails.
func TestTreasuryCanonicalOuterSuccessRetainsInnerFailure(t *testing.T) {
	f := newTreasuryFixture(t)
	chain, _, request, signed := treasuryTestChain(t, f, "inner-failed")
	e, err := chain.reconcile(t.Context(), request, signed)
	if err != nil || e.Receipt == nil || !e.Receipt.Success || e.Dispatch == nil || e.Dispatch.InnerSuccess || e.Dispatch.InnerError == "" || treasuryPhase(request, e) != "inner-dispatch-failed" {
		t.Fatal("outer success promoted inner failure", e, err)
	}
}

// A matched inner success still needs exact ownership and generation readback.
func TestTreasuryCanonicalRegistrationRequiresActualReadback(t *testing.T) {
	f := newTreasuryFixture(t)
	chain, fixture, request, signed := treasuryTestChain(t, f, "executed")
	e, err := chain.reconcile(t.Context(), request, signed)
	if err != nil || treasuryPhase(request, e) != "executed" {
		t.Fatal("registered native treasury recipient not admitted", err)
	}
	hotkey, _ := hex.DecodeString(f.config.Action.Inner.Hotkey[2:])
	key, _ := types.CreateStorageKey(f.metadata, "SubtensorModule", "Owner", hotkey)
	fixture.stateLock.Lock()
	fixture.storageKVs[key.Hex()] = "0x" + strings.Repeat("88", 32)
	fixture.stateLock.Unlock()
	if _, err := chain.reconcile(t.Context(), request, signed); !errors.Is(err, errRpcIntegrity) {
		t.Fatal("foreign actual owner became treasury success", err)
	}
}

// Cancellation remains available after the original earning roster has drifted.
func TestTreasuryCancellationPreservesOriginalRecoveryAfterRecipientDrift(t *testing.T) {
	f := newTreasuryFixture(t)
	a := f.input.Action
	a.Operation = "cancel_as_multi"
	a.Timepoint = &treasuryTimepoint{Height: 90, Index: 2}
	owner, _ := hex.DecodeString(a.Owner[2:])
	multisig, _ := hex.DecodeString(a.Descriptor.Multisig.AccountId[2:])
	inner, _ := a.innerCall()
	hash, _ := hex.DecodeString(rootExtrinsicHash(inner)[2:])
	pending := binary.LittleEndian.AppendUint32(nil, 90)
	pending = binary.LittleEndian.AppendUint32(pending, 2)
	pending = binary.LittleEndian.AppendUint64(pending, 20)
	pending = append(pending, owner...)
	pending = append(pending, 4)
	pending = append(pending, owner...)
	f.set(t, "Multisig", "Multisigs", pending, multisig, hash)
	hotkey, _ := hex.DecodeString(a.Inner.Hotkey[2:])
	f.set(t, "SubtensorModule", "Owner", bytes.Repeat([]byte{94}, 32), hotkey)
	observation := f.observation(t, a, a.BirthBlock, a.BirthHash)
	a.ObservationHash = observation.ContentHash
	input := f.input
	input.Action, input.Observation = a, observation
	dir := filepath.Dir(a.StatePath)
	path, _ := treasuryTestJson(t, dir, "cancel-input.json", input)
	var out, diagnostic bytes.Buffer
	if code := runTreasuryCommand(t.Context(), []string{"plan", "--input", path}, &out, &diagnostic); code != 0 {
		t.Fatal("original depositor cancellation blocked by unrelated recipient drift", diagnostic.String())
	}
	a.Operation = "as_multi"
	_, err := treasuryReadFacts(t.Context(), a, f.metadata, a.BirthBlock, func(_ context.Context, key string, fallback []byte) ([]byte, bool, error) {
		raw, ok := f.values[key]
		if !ok {
			return fallback, false, nil
		}
		data, err := hex.DecodeString(raw[2:])
		return data, true, err
	})
	if !errors.Is(err, errRpcIntegrity) {
		t.Fatal("execution ignored actual recipient drift", err)
	}
}
