package main

// These tests pin exact native mutation identity against wire-level and
// lifecycle substitutions.

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	gsrpcregistry "github.com/centrifuge/go-substrate-rpc-client/v4/registry"
	gsrpcparser "github.com/centrifuge/go-substrate-rpc-client/v4/registry/parser"
	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	gsrpccodec "github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/crv4"
)

// Creates a deterministic sr25519 identity and canonical public key.
func finalNativeTestAccount(t *testing.T, marker byte) (*crv4.Keypair, string) {
	t.Helper()
	seed := [32]byte{marker, marker + 1, marker + 2}
	pair, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	key := pair.PublicKey()
	return pair, "0x" + hex.EncodeToString(key[:])
}

// Models registry-decoded fixed bytes without metadata I/O.
func finalNativeTestBytes(value []byte) []any {
	result := make([]any, len(value))
	for index := range value {
		result[index] = gsrpctypes.U8(value[index])
	}
	return result
}

// Models a deterministic decoded field list for exact verifier tests.
func finalNativeTestFields(values ...any) gsrpcregistry.DecodedFields {
	result := make(gsrpcregistry.DecodedFields, len(values))
	for index := range values {
		result[index] = &gsrpcregistry.DecodedField{Name: "field", Value: values[index]}
	}
	return result
}

// Builds the exact decoded registration corresponding to one expectation.
func finalNativeTestRegistrationDecoded(t *testing.T, expected FinalNativeCallEvidence) *decodedFinalNativeExtrinsic {
	t.Helper()
	hotkey, err := finalNativeAccountPublicKey(expected.Hotkey)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := finalNativeEncodeCall(
		gsrpctypes.CallIndex{SectionIndex: finalNativeSubtensorPalletIndex, MethodIndex: finalNativeRegisterLimitCallIndex},
		gsrpctypes.NewU16(expected.Netuid), gsrpctypes.AccountID(hotkey), gsrpctypes.NewU64(expected.RegistrationLimitRao),
	)
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := finalNativeAccountPublicKey(expected.Signer)
	return &decodedFinalNativeExtrinsic{
		Version: finalNativeSignedExtrinsicVersion, Signer: signer, Nonce: expected.Nonce, Immortal: true,
		CallIndex: gsrpctypes.CallIndex{SectionIndex: finalNativeSubtensorPalletIndex, MethodIndex: finalNativeRegisterLimitCallIndex},
		CallName:  finalNativeRegisterLimitCall, RawCall: raw,
		CallFields: finalNativeTestFields(gsrpctypes.NewU16(expected.Netuid), finalNativeTestBytes(hotkey[:]), gsrpctypes.NewU64(expected.RegistrationLimitRao)),
	}
}

// Builds a deterministic prepared commit and its source-derived identity.
func finalNativeTestCommitEvidence(t *testing.T) (FinalNativeCallEvidence, []byte, *crv4.PreparedSubmission) {
	t.Helper()
	_, hotkey := finalNativeTestAccount(t, 0x31)
	hotkeyRaw, _ := finalNativeAccountPublicKey(hotkey)
	cycle := FinalCRv4Cycle{
		SubnetEpoch: 19, NativeSnapshot: ChainHead{Number: 100, Hash: finalTestHex(0x41)},
		Reveal: FinalNativeReceipt{Block: ChainHead{Number: 120, Hash: finalTestHex(0x42)}},
	}
	prepared := finalTestPreparedSubmission(t, []uint16{7, 8}, []uint16{100, 0}, cycle, hotkeyRaw)
	ciphertext, err := gsrpccodec.HexDecodeString(prepared.CiphertextHex)
	if err != nil {
		t.Fatal(err)
	}
	rawCall, err := finalNativeEncodeCall(
		gsrpctypes.CallIndex{SectionIndex: finalNativeSubtensorPalletIndex, MethodIndex: finalNativeCommitCallIndex},
		gsrpctypes.NewU16(prepared.Netuid), gsrpctypes.NewBytes(ciphertext), gsrpctypes.NewU64(prepared.RevealRound), gsrpctypes.NewU16(prepared.CommitRevealVersion),
	)
	if err != nil {
		t.Fatal(err)
	}
	// PreparedSubmission.Validate deliberately treats the signed envelope as
	// opaque. Give this unit fixture a stable opaque prefix and the exact call
	// suffix which the FINAL source boundary additionally requires.
	body := append([]byte{finalNativeSignedExtrinsicVersion}, make([]byte, 96)...)
	body = append(body, rawCall...)
	length, err := gsrpccodec.Encode(gsrpctypes.NewUCompactFromUInt(uint64(len(body))))
	if err != nil {
		t.Fatal(err)
	}
	raw := append(length, body...)
	digest := blake2b.Sum256(raw)
	prepared.ExtrinsicHex = gsrpccodec.HexEncodeToString(raw)
	prepared.ExtrinsicHash = gsrpctypes.Hash(digest).Hex()
	expected, err := finalNativeCommitCallEvidence(prepared, 4, 110)
	if err != nil {
		t.Fatal(err)
	}
	return expected, ciphertext, prepared
}

// Builds the exact decoded commit corresponding to one expectation.
func finalNativeTestCommitDecoded(t *testing.T, expected FinalNativeCallEvidence, ciphertext []byte) *decodedFinalNativeExtrinsic {
	t.Helper()
	args := []any{gsrpctypes.NewU16(expected.Netuid)}
	index := finalNativeCommitCallIndex
	if expected.Mecid != nil {
		index = finalNativeCommitMechanismCallIndex
		args = append(args, gsrpctypes.NewU8(*expected.Mecid))
	}
	args = append(args, gsrpctypes.NewBytes(ciphertext), gsrpctypes.NewU64(expected.RevealRound), gsrpctypes.NewU16(expected.CommitRevealVersion))
	raw, err := finalNativeEncodeCall(gsrpctypes.CallIndex{SectionIndex: finalNativeSubtensorPalletIndex, MethodIndex: index}, args...)
	if err != nil {
		t.Fatal(err)
	}
	fields := []any{gsrpctypes.NewU16(expected.Netuid)}
	if expected.Mecid != nil {
		fields = append(fields, gsrpctypes.NewU8(*expected.Mecid))
	}
	fields = append(fields, finalNativeTestBytes(ciphertext), gsrpctypes.NewU64(expected.RevealRound), gsrpctypes.NewU16(expected.CommitRevealVersion))
	signer, _ := finalNativeAccountPublicKey(expected.Signer)
	return &decodedFinalNativeExtrinsic{
		Version: finalNativeSignedExtrinsicVersion, Signer: signer, Nonce: expected.Nonce, Immortal: true,
		CallIndex: gsrpctypes.CallIndex{SectionIndex: finalNativeSubtensorPalletIndex, MethodIndex: index},
		CallName:  expected.Pallet + "." + expected.Call, RawCall: raw, CallFields: finalNativeTestFields(fields...),
	}
}

// Pins the exact call indices and hashes derived from plan/prepared fields.
func TestFinalNativeCallEvidenceDerivesReviewedWireIdentity(t *testing.T) {
	signer, _ := finalNativeTestAccount(t, 0x11)
	_, hotkey := finalNativeTestAccount(t, 0x21)
	registration, err := finalNativeRegistrationCallEvidence(signer.Address(), 17, 521, hotkey, 54, 500_000)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalNativeCallEvidenceShape(registration, finalNativeOperationRegistration); err != nil {
		t.Fatalf("derived registration rejected: %v", err)
	}
	if err := verifyFinalNativeExtrinsicIdentity(finalNativeTestRegistrationDecoded(t, registration), registration); err != nil {
		t.Fatalf("exact registration extrinsic rejected: %v", err)
	}
	commit, ciphertext, _ := finalNativeTestCommitEvidence(t)
	if err := verifyFinalNativeCallEvidenceShape(commit, finalNativeOperationCommit); err != nil {
		t.Fatalf("derived commit rejected: %v", err)
	}
	if err := verifyFinalNativeExtrinsicIdentity(finalNativeTestCommitDecoded(t, commit, ciphertext), commit); err != nil {
		t.Fatalf("exact commit extrinsic rejected: %v", err)
	}
	reveal, err := finalNativeAutomaticCallEvidence(commit, finalNativeOperationReveal, commit.RevealBlock)
	if err != nil || reveal.Operation != finalNativeOperationReveal {
		t.Fatalf("derived reveal lineage rejected: value=%+v err=%v", reveal, err)
	}
	application, err := finalNativeAutomaticCallEvidence(commit, finalNativeOperationApplication, commit.RevealBlock+3)
	if err != nil || application.Operation != finalNativeOperationApplication {
		t.Fatalf("derived application lineage rejected: value=%+v err=%v", application, err)
	}
}

// Proves a successful unrelated signed call cannot stand in for reviewed
// native work.
func TestFinalNativeExtrinsicIdentityRejectsCallAndSignerSubstitution(t *testing.T) {
	signer, _ := finalNativeTestAccount(t, 0x51)
	_, hotkey := finalNativeTestAccount(t, 0x61)
	expected, err := finalNativeRegistrationCallEvidence(signer.Address(), 9, 521, hotkey, 75, 700_000)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*decodedFinalNativeExtrinsic)
	}{
		{name: "wrong signer", mutate: func(value *decodedFinalNativeExtrinsic) { value.Signer[0] ^= 0xff }},
		{name: "wrong nonce", mutate: func(value *decodedFinalNativeExtrinsic) { value.Nonce++ }},
		{name: "mortal", mutate: func(value *decodedFinalNativeExtrinsic) { value.Immortal = false }},
		{name: "nonzero tip", mutate: func(value *decodedFinalNativeExtrinsic) { value.Tip = 1 }},
		{name: "metadata mode", mutate: func(value *decodedFinalNativeExtrinsic) { value.MetadataMode = 1 }},
		{name: "wrong netuid", mutate: func(value *decodedFinalNativeExtrinsic) { value.CallFields[0].Value = gsrpctypes.NewU16(520) }},
		{name: "wrong hotkey", mutate: func(value *decodedFinalNativeExtrinsic) {
			value.CallFields[1].Value = finalNativeTestBytes(make([]byte, 32))
		}},
		{name: "wrong burn limit", mutate: func(value *decodedFinalNativeExtrinsic) { value.CallFields[2].Value = gsrpctypes.NewU64(700_001) }},
		{name: "unrelated registration", mutate: func(value *decodedFinalNativeExtrinsic) { value.CallName = "SubtensorModule.burned_register" }},
	}
	for _, test := range tests {
		candidate := finalNativeTestRegistrationDecoded(t, expected)
		test.mutate(candidate)
		if err := verifyFinalNativeExtrinsicIdentity(candidate, expected); err == nil {
			t.Fatalf("%s substitution accepted", test.name)
		}
	}
}

// Reproduces substring-only prepared-submission authentication with a
// successful unrelated remark carrying the reviewed ciphertext.
func TestFinalNativeCommitRejectsSuccessfulRemarkContainingCiphertext(t *testing.T) {
	expected, ciphertext, prepared := finalNativeTestCommitEvidence(t)
	remark := finalNativeTestCommitDecoded(t, expected, ciphertext)
	remark.CallIndex = gsrpctypes.CallIndex{SectionIndex: 0, MethodIndex: 1}
	remark.CallName = "System.remark"
	remark.RawCall = append([]byte{0, 1, byte(len(ciphertext) << 2)}, ciphertext...)
	remark.CallFields = finalNativeTestFields(finalNativeTestBytes(ciphertext))
	if !strings.Contains(hex.EncodeToString(remark.RawCall), hex.EncodeToString(ciphertext)) {
		t.Fatal("remark regression fixture does not contain the exact ciphertext")
	}
	if err := verifyFinalNativeExtrinsicIdentity(remark, expected); err == nil {
		t.Fatal("successful System.remark containing the ciphertext was accepted as a CRv4 commit")
	}
	raw, err := gsrpccodec.HexDecodeString(prepared.ExtrinsicHex)
	if err != nil {
		t.Fatal(err)
	}
	commitCall := finalNativeTestCommitDecoded(t, expected, ciphertext).RawCall
	if len(raw) <= len(commitCall) || !bytes.HasSuffix(raw, commitCall) {
		t.Fatal("prepared commit fixture does not end in its exact call")
	}
	prefixSize := 1 << (raw[0] & 3)
	if raw[0]&3 == 3 || prefixSize > len(raw)-len(commitCall) {
		t.Fatal("prepared commit fixture has unsupported compact length")
	}
	remarkCall, err := finalNativeEncodeCall(gsrpctypes.CallIndex{SectionIndex: 0, MethodIndex: 1}, gsrpctypes.NewBytes(ciphertext))
	if err != nil {
		t.Fatal(err)
	}
	body := append(append([]byte(nil), raw[prefixSize:len(raw)-len(commitCall)]...), remarkCall...)
	length, err := gsrpccodec.Encode(gsrpctypes.NewUCompactFromUInt(uint64(len(body))))
	if err != nil {
		t.Fatal(err)
	}
	raw = append(length, body...)
	prepared.ExtrinsicHex = gsrpccodec.HexEncodeToString(raw)
	digest := blake2b.Sum256(raw)
	prepared.ExtrinsicHash = gsrpctypes.Hash(digest).Hex()
	if _, err := finalNativeCommitCallEvidence(prepared, 4, 110); err == nil || !strings.Contains(err.Error(), "exact CRv4 call") {
		t.Fatalf("prepared remark-shaped payload accepted: %v", err)
	}
}

// Models one parsed runtime event with explicit phase and fields.
func finalNativeTestEvent(name string, phase gsrpctypes.Phase, fields ...any) *gsrpcparser.Event {
	return &gsrpcparser.Event{Name: name, Phase: &phase, Fields: finalNativeTestFields(fields...)}
}

// Pins the exact registration, commit, and automatic reveal/application event
// identities.
func TestFinalNativeOperationEventsRejectSubstitutionAndAmbiguity(t *testing.T) {
	signer, _ := finalNativeTestAccount(t, 0x71)
	_, hotkey := finalNativeTestAccount(t, 0x72)
	registration, err := finalNativeRegistrationCallEvidence(signer.Address(), 3, 521, hotkey, 81, 600_000)
	if err != nil {
		t.Fatal(err)
	}
	hotkeyKey, _ := finalNativeAccountPublicKey(registration.Hotkey)
	phase := gsrpctypes.Phase{IsApplyExtrinsic: true, AsApplyExtrinsic: 4}
	index := uint32(4)
	registrationEvent := finalNativeTestEvent(finalNativeNeuronRegisteredEvent, phase, gsrpctypes.NewU16(521), gsrpctypes.NewU16(81), finalNativeTestBytes(hotkeyKey[:]))
	if err := verifyFinalNativeOperationEvents([]*gsrpcparser.Event{registrationEvent}, &index, registration); err != nil {
		t.Fatalf("exact registration event rejected: %v", err)
	}
	wrongNetuid := finalNativeTestEvent(finalNativeNeuronRegisteredEvent, phase, gsrpctypes.NewU16(520), gsrpctypes.NewU16(81), finalNativeTestBytes(hotkeyKey[:]))
	if err := verifyFinalNativeOperationEvents([]*gsrpcparser.Event{wrongNetuid}, &index, registration); err == nil {
		t.Fatal("wrong-netuid registration event accepted")
	}
	if err := verifyFinalNativeOperationEvents([]*gsrpcparser.Event{registrationEvent, registrationEvent}, &index, registration); err == nil {
		t.Fatal("ambiguous duplicate registration events accepted")
	}

	commit, _, _ := finalNativeTestCommitEvidence(t)
	commitSigner, _ := finalNativeAccountPublicKey(commit.Signer)
	commitHash, _ := gsrpccodec.HexDecodeString(commit.CiphertextBlake2)
	commitEvent := finalNativeTestEvent(finalNativeTimelockedCommittedEvent, phase, finalNativeTestBytes(commitSigner[:]), gsrpctypes.NewU16(commit.Netuid), finalNativeTestBytes(commitHash), gsrpctypes.NewU64(commit.RevealRound))
	if err := verifyFinalNativeOperationEvents([]*gsrpcparser.Event{commitEvent}, &index, commit); err != nil {
		t.Fatalf("exact CRv4 commit event rejected: %v", err)
	}
	wrongSigner := append([]byte(nil), commitSigner[:]...)
	wrongSigner[0] ^= 0xff
	commitEvent = finalNativeTestEvent(finalNativeTimelockedCommittedEvent, phase, finalNativeTestBytes(wrongSigner), gsrpctypes.NewU16(commit.Netuid), finalNativeTestBytes(commitHash), gsrpctypes.NewU64(commit.RevealRound))
	if err := verifyFinalNativeOperationEvents([]*gsrpcparser.Event{commitEvent}, &index, commit); err == nil {
		t.Fatal("wrong-signer CRv4 commit event accepted")
	}

	reveal, err := finalNativeAutomaticCallEvidence(commit, finalNativeOperationReveal, commit.RevealBlock)
	if err != nil {
		t.Fatal(err)
	}
	initialization := gsrpctypes.Phase{IsInitialization: true}
	weightsEvent := finalNativeTestEvent(finalNativeWeightsSetEvent, initialization, gsrpctypes.NewU16(reveal.Netuid), gsrpctypes.NewU16(reveal.UID))
	revealEvent := finalNativeTestEvent(finalNativeTimelockedRevealedEvent, initialization, gsrpctypes.NewU16(reveal.Netuid), finalNativeTestBytes(commitSigner[:]))
	if err := verifyFinalNativeOperationEvents([]*gsrpcparser.Event{weightsEvent, revealEvent}, nil, reveal); err != nil {
		t.Fatalf("exact reveal/application events rejected: %v", err)
	}
	if err := verifyFinalNativeOperationEvents([]*gsrpcparser.Event{weightsEvent}, nil, reveal); err == nil {
		t.Fatal("missing reveal event with an available old weight row was accepted")
	}
	if err := verifyFinalNativeOperationEvents([]*gsrpcparser.Event{revealEvent, weightsEvent}, nil, reveal); err == nil {
		t.Fatal("reordered reveal/application events accepted")
	}
	if err := verifyFinalNativeOperationEvents([]*gsrpcparser.Event{weightsEvent, revealEvent, revealEvent}, nil, reveal); err == nil {
		t.Fatal("ambiguous duplicate reveal events accepted")
	}
}

// Proves a fresh commit/reveal lineage cannot authenticate a prior vector or
// another owner.
func TestFinalNativeApplicationRejectsOldStateAndUIDReassignment(t *testing.T) {
	commit, _, _ := finalNativeTestCommitEvidence(t)
	reveal, err := finalNativeAutomaticCallEvidence(commit, finalNativeOperationReveal, commit.RevealBlock)
	if err != nil {
		t.Fatal(err)
	}
	applicationHead := ChainHead{Number: commit.RevealBlock + 3, Hash: finalTestHex(0xa1)}
	application, err := finalNativeAutomaticCallEvidence(commit, finalNativeOperationApplication, applicationHead.Number)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalNativeCRv4Lineage(commit, reveal, application); err != nil {
		t.Fatalf("exact native CRv4 lineage rejected: %v", err)
	}
	uids, values := []uint16{7, 8}, []uint16{100, 50}
	state := FinalNativeWeightState{ValidatorUID: commit.UID, ValidatorHotkey: commit.Signer, LastUpdate: commit.CommitBlock, UIDs: uids, Values: values, Block: applicationHead}
	if err := verifyFinalNativeApplicationState(state, application, applicationHead, commit.UID, commit.Signer, uids, values); err != nil {
		t.Fatalf("exact native application state rejected: %v", err)
	}
	old := state
	old.LastUpdate--
	if err := verifyFinalNativeApplicationState(old, application, applicationHead, commit.UID, commit.Signer, uids, values); err == nil {
		t.Fatal("old weights relabeled with a fresh cycle were accepted")
	}
	_, reassigned := finalNativeTestAccount(t, 0x7f)
	state.ValidatorHotkey = reassigned
	if err := verifyFinalNativeApplicationState(state, application, applicationHead, commit.UID, commit.Signer, uids, values); err == nil {
		t.Fatal("application UID reassignment was accepted")
	}
	missingReveal := reveal
	missingReveal.RevealBlock = 0
	if err := verifyFinalNativeCRv4Lineage(commit, missingReveal, application); err == nil {
		t.Fatal("application lineage without its exact reveal was accepted")
	}
}

// Proves byte-identical old commitment evidence cannot be relabeled as
// another accepted cycle.
func TestFinalNativeApplicationRejectsReusedCycleLineage(t *testing.T) {
	commit, _, _ := finalNativeTestCommitEvidence(t)
	application, err := finalNativeAutomaticCallEvidence(commit, finalNativeOperationApplication, commit.RevealBlock+2)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalNativeUniqueCRv4Cycles([]FinalNativeCallEvidence{application}); err != nil {
		t.Fatalf("unique native cycle rejected: %v", err)
	}
	if err := verifyFinalNativeUniqueCRv4Cycles([]FinalNativeCallEvidence{application, application}); err == nil || !strings.Contains(err.Error(), "reuses commit extrinsic") {
		t.Fatalf("reused native cycle lineage accepted: %v", err)
	}
}

// Permits an exact lifecycle/recovery reference to a cycle while rejecting
// reuse under another validator epoch identity.
func TestFinalNativeEvidenceCycleAliasesMustKeepOneIdentity(t *testing.T) {
	commit, _, _ := finalNativeTestCommitEvidence(t)
	application, err := finalNativeAutomaticCallEvidence(commit, finalNativeOperationApplication, commit.RevealBlock+2)
	if err != nil {
		t.Fatal(err)
	}
	receipt := FinalNativeReceipt{Call: &application}
	evidence := &FinalSemanticEvidence{
		Validators: []FinalValidatorIdentityEvidence{{ValidatorID: 1, Cycles: []FinalCRv4Cycle{{SettlementEpoch: 10, SubnetEpoch: 20, Application: receipt}}}},
		FleetLifecycle: &FinalFleetLifecycleEvidence{AppliedDecisions: []FinalFleetLifecycleAppliedDecision{{
			ValidatorID: 1, SettlementEpoch: 10, SubnetEpoch: 20, ApplicationCall: application,
		}}},
	}
	if err := verifyFinalNativeEvidenceCycleUniqueness(evidence); err != nil {
		t.Fatalf("exact native cycle alias rejected: %v", err)
	}
	evidence.FleetLifecycle.AppliedDecisions[0].SettlementEpoch++
	if err := verifyFinalNativeEvidenceCycleUniqueness(evidence); err == nil || !strings.Contains(err.Error(), "reuses commit extrinsic") {
		t.Fatalf("native call relabeled under another cycle was accepted: %v", err)
	}
}
