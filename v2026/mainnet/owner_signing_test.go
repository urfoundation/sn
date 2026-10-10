// Synthetic Ed25519 fixtures test the portable contract and native codec.
// They do not emulate Ledger key derivation, RFC78 proof validation or firmware.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/extrinsic"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Go's numbered TempDir children inherit the process umask. Every owner file
// fixture explicitly establishes the same private directory contract as users.
func ownerSigningTestDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return directory
}

// This publicly reproducible seed is strictly a synthetic test signer.
func ownerSigningTestKey() ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("synthetic offline owner Ed25519 fixture only"))
	return ed25519.NewKeyFromSeed(seed[:])
}

// A separate action approval binds the new scheme, digest and derivation path.
func ownerSigningTestLedgerConfig(t *testing.T, fixture *ownerTrimTestFixture) {
	t.Helper()
	fixture.config.Schema = ownerTrimLedgerExecutionSchema
	action := fixture.config.Action
	action.Schema, action.SignatureScheme = ownerTrimLedgerActionSchema, "ed25519"
	action.Coldkey = "0x" + hex.EncodeToString(ownerSigningTestKey().Public().(ed25519.PublicKey))
	action.MetadataDigest, action.DerivationPath = "0x"+strings.Repeat("ab", 32), "m/44'/354'/7'/0'/3'"
	fixture.ledgerMetadata = "0x" + hex.EncodeToString(append([]byte{'m', 'e', 't', 'a', 15}, []byte("synthetic metadata shape for adapter fixture only")...))
	action.LedgerMetadataHash, _ = ownerLedgerMetadataHash(fixture.ledgerMetadata)
	var err error
	fixture.config.Action, err = prepareOwnerTrimAction(action, fixture.metadata)
	if err != nil {
		t.Fatal(err)
	}
	fixture.approve()
}

// The metadata fixture contains actual Ed25519 and sr25519 enum shapes.
func ownerSigningTestRequest(t *testing.T) (*ownerTrimTestFixture, ownerSigningRequest, ownerSigningTrust) {
	t.Helper()
	fixture := newOwnerTrimActionTestFixture(t)
	ownerSigningTestLedgerConfig(t, fixture)
	request, err := newOwnerSigningRequest(fixture.config, fixture.key, fixture.metadata, fixture.ledgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, request, ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: fixture.key,
		Owner: fixture.config.Action.Coldkey, Genesis: fixture.config.Action.Network.GenesisHash}
}

// Typed GSRPC encodes the direct call, enabled metadata implicit and variant-zero
// envelope independently of the production owner's manual assembly.
func TestOwnerSigningEd25519MatchesTypedSubstrateCodec(t *testing.T) {
	f, request, _ := ownerSigningTestRequest(t)
	action := f.config.Action
	metadata, _, err := crv4.DecodeRuntimeMetadata(f.metadata)
	if err != nil {
		t.Fatal(err)
	}
	call, err := types.NewCall(metadata, "AdminUtils.sudo_trim_to_max_allowed_uids", types.U16(action.Netuid), types.U16(action.MaximumUids))
	if err != nil {
		t.Fatal(err)
	}
	callRaw, err := codec.Encode(call)
	if err != nil || "0x"+hex.EncodeToString(callRaw) != action.Call {
		t.Fatal("typed owner call differs", err)
	}
	genesis, _ := types.NewHashFromHexString(action.Network.GenesisHash)
	birthHash, _ := types.NewHashFromHexString(action.BirthHash)
	metadataHash, _ := types.NewHashFromHexString(action.MetadataDigest)
	payload := extrinsic.Payload{EncodedCall: types.BytesBare(callRaw), SignedFields: []*extrinsic.SignedField{
		{Name: extrinsic.EraSignedField, Value: types.ExtrinsicEra{IsMortalEra: true, AsMortalEra: types.MortalEra{First: 2, Second: 0}}, Mutated: true},
		{Name: extrinsic.NonceSignedField, Value: types.NewUCompactFromUInt(uint64(action.Nonce)), Mutated: true},
		{Name: extrinsic.TipSignedField, Value: types.NewUCompactFromUInt(0), Mutated: true},
		{Name: extrinsic.CheckMetadataHashModeSignedField, Value: types.U8(1), Mutated: true},
	}, SignedExtraFields: []*extrinsic.SignedField{
		{Name: extrinsic.SpecVersionSignedField, Value: types.U32(action.Runtime.RuntimeVersion.SpecVersion), Mutated: true},
		{Name: extrinsic.TransactionVersionSignedField, Value: types.U32(action.Runtime.RuntimeVersion.TransactionVersion), Mutated: true},
		{Name: extrinsic.GenesisHashSignedField, Value: genesis, Mutated: true},
		{Name: extrinsic.BlockHashSignedField, Value: birthHash, Mutated: true},
		{Name: extrinsic.CheckMetadataHashSignedField, Value: types.NewOption(metadataHash), Mutated: true},
	}}
	payloadRaw, err := codec.Encode(&payload)
	if err != nil || "0x"+hex.EncodeToString(payloadRaw) != action.Payload || action.Payload != request.SigningBytes {
		t.Fatal("typed Ed25519 owner payload or exact signing bytes differ", err)
	}
	key := ownerSigningTestKey()
	signature := ed25519.Sign(key, payloadRaw)
	address, _ := types.NewMultiAddressFromAccountID(key.Public().(ed25519.PublicKey))
	typed := extrinsic.Extrinsic{Version: extrinsic.Version4 | extrinsic.BitSigned, Method: call,
		Signature: &extrinsic.Signature{Signer: address, Signature: types.MultiSignature{IsEd25519: true, AsEd25519: types.NewSignature(signature)}, SignedFields: payload.SignedFields}}
	want, err := codec.Encode(typed)
	if err != nil {
		t.Fatal(err)
	}
	got, err := action.signed(signature)
	if err != nil || !bytes.Equal(got, want) || ownerTrimSignedAction(action, got) != nil {
		t.Fatal("typed Ed25519 native envelope differs", err)
	}
	for _, mutation := range []func(*ownerTrimAction){
		func(a *ownerTrimAction) { a.Nonce++ },
		func(a *ownerTrimAction) { a.BirthHash = "0x" + strings.Repeat("cd", 32) },
		func(a *ownerTrimAction) { a.MaximumUids++ },
		func(a *ownerTrimAction) { a.MetadataDigest = "0x" + strings.Repeat("ef", 32) },
		func(a *ownerTrimAction) { a.Coldkey = "0x" + strings.Repeat("12", 32) },
	} {
		changed := action
		mutation(&changed)
		changed, err = prepareOwnerTrimAction(changed, f.metadata)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := changed.signed(signature); err == nil {
			t.Fatal("original native signature admitted a changed action or owner")
		}
	}
}

// The new schema cannot reinterpret any original v1 action or approval domain.
func TestOwnerSigningPreservesV1AndRequiresFreshV2Approval(t *testing.T) {
	f := newOwnerTrimActionTestFixture(t)
	original, _ := json.Marshal(f.config.Action)
	if bytes.Contains(original, []byte("signature_scheme")) || bytes.Contains(original, []byte("check_metadata_hash")) || bytes.Contains(original, []byte("signer_derivation_path")) {
		t.Fatal("v1 canonical JSON changed")
	}
	payload, _ := hex.DecodeString(f.config.Action.Payload[2:])
	signature, err := f.pair.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := f.config.Action.signed(signature)
	if err != nil || ownerTrimSignedAction(f.config.Action, raw) != nil {
		t.Fatal("original v1 signature lost compatibility", err)
	}
	oldApproval := f.config.Signature
	oldAction := f.config.Action
	ownerSigningTestLedgerConfig(t, f)
	f.config.Signature = oldApproval
	if f.config.validate(f.key) == nil {
		t.Fatal("v1 approval silently authorized v2")
	}
	oldAction.SignatureScheme = "ed25519"
	if _, err := prepareOwnerTrimAction(oldAction, f.metadata); err == nil {
		t.Fatal("v1 action silently selected a different signature scheme")
	}
	f.approve()
	f.config.Schema = ownerTrimExecutionSchema
	if f.config.validate(f.key) == nil {
		t.Fatal("v2 action accepted the old approval domain")
	}
}

// Select the Ed25519 variant explicitly, without weakening the root's sr25519
// profile or accepting a same-width wrong enum variant.
func TestOwnerSigningRejectsChangedEd25519MetadataVariant(t *testing.T) {
	f, _, _ := ownerSigningTestRequest(t)
	metadata, _, err := crv4.DecodeRuntimeMetadata(f.metadata)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range metadata.AsMetadataV14.EfficientLookup {
		if entry.Def.IsVariant {
			for i := range entry.Def.Variant.Variants {
				if entry.Def.Variant.Variants[i].Name == "Ed25519" {
					entry.Def.Variant.Variants[i].Name = "SyntheticWrongSignature"
				}
			}
		}
	}
	if nativeSigningProfileForSignature(metadata, "Ed25519", 0) == nil || nativeSigningProfile(metadata) != nil {
		t.Fatal("Ed25519 refusal changed sr25519 semantics or ignored the selected variant")
	}
}

// Rehashing a packet does not repair a changed approval, metadata or signing body.
func TestOwnerSigningRequestAndReplyRejectCrossDomainChanges(t *testing.T) {
	_, request, trust := ownerSigningTestRequest(t)
	for _, mutation := range []func(*ownerSigningRequest){
		func(r *ownerSigningRequest) { r.Schema += "-foreign" },
		func(r *ownerSigningRequest) { r.SignatureScheme = "sr25519" },
		func(r *ownerSigningRequest) { r.SigningBytes += "00" },
		func(r *ownerSigningRequest) { r.MetadataHex = "0x00" },
		func(r *ownerSigningRequest) { r.Config.Action.Nonce++ },
		func(r *ownerSigningRequest) { r.Config.Signature = strings.Repeat("00", 64) },
	} {
		changed := request
		mutation(&changed)
		changed.ContentHash = ""
		changed.ContentHash = rootObjectHash(changed)
		changedTrust := trust
		changedTrust.RequestHash = changed.ContentHash
		if changed.validate(changedTrust) == nil {
			t.Fatal("resealed request admitted changed signing or approval semantics")
		}
	}
	for _, mutation := range []func(*ownerSigningTrust){
		func(v *ownerSigningTrust) { v.RequestHash = rootObjectHash("another request") },
		func(v *ownerSigningTrust) { v.ApprovalKey = "0x" + strings.Repeat("12", 32) },
		func(v *ownerSigningTrust) { v.Owner = "0x" + strings.Repeat("13", 32) },
		func(v *ownerSigningTrust) { v.Genesis = "0x" + strings.Repeat("14", 32) },
	} {
		changed := trust
		mutation(&changed)
		if request.validate(changed) == nil {
			t.Fatal("request supplied its own trust")
		}
	}
	reply, err := newOwnerSigningReply(request, ed25519.Sign(ownerSigningTestKey(), ownerSigningBytes(request.Config.Action)))
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(*ownerSigningReply){
		func(r *ownerSigningReply) { r.RequestHash = rootObjectHash("different packet") },
		func(r *ownerSigningReply) { r.ActionHash = rootObjectHash("different action") },
		func(r *ownerSigningReply) { r.SignatureScheme = "sr25519" },
		func(r *ownerSigningReply) { r.Owner = "0x" + strings.Repeat("12", 32) },
		func(r *ownerSigningReply) { r.RawExtrinsic += "00" },
		func(r *ownerSigningReply) { r.ExtrinsicHash = "0x" + strings.Repeat("15", 32) },
		func(r *ownerSigningReply) { r.Signature = strings.Repeat("00", 64) },
	} {
		changed := reply
		mutation(&changed)
		if _, err := changed.validate(request); err == nil {
			t.Fatal("signed reply admitted a different domain or envelope")
		}
	}
}

// A bounded protocol emulator checks the complete wire transcript and returns
// one synthetic signature. Its metadata bytes are a fixture, not a Merkle proof.
func ownerSigningTestLedgerResponse(t *testing.T, request ownerSigningRequest) []byte {
	t.Helper()
	proof := bytes.Repeat([]byte("synthetic-proof-not-rfc78-verified"), 12)
	transcript, err := newOwnerLedgerTranscript(request, proof)
	if err != nil {
		t.Fatal(err)
	}
	if transcript.Signing || transcript.NetworkEffects || transcript.DeviceQualified || transcript.MetadataProofVerified || len(transcript.SigningApdus) < 3 {
		t.Fatal("fixture transcript claimed effects or device/proof qualification")
	}
	address, _ := hex.DecodeString(transcript.AddressApdu[2:])
	wantPath := []uint32{0x8000002c, 0x80000162, 0x80000007, 0x80000000, 0x80000003}
	if len(address) != 27 || !bytes.Equal(address[:5], []byte{0xf9, 1, 1, 0, 22}) || binary.LittleEndian.Uint16(address[25:]) != 42 {
		t.Fatal("address confirmation APDU differs")
	}
	for index, want := range wantPath {
		if binary.LittleEndian.Uint32(address[5+index*4:]) != want {
			t.Fatal("derivation path APDU differs")
		}
	}
	key := ownerSigningTestKey()
	if transcript.ExpectedPublicKey != "0x"+hex.EncodeToString(key.Public().(ed25519.PublicKey)) {
		t.Fatal("emulated device key differs from existing owner")
	}
	var received []byte
	payloadLength := 0
	for index, encoded := range transcript.SigningApdus {
		apdu, err := hex.DecodeString(encoded[2:])
		if err != nil || len(apdu) < 5 || apdu[0] != 0xf9 || apdu[1] != 2 || apdu[3] != 0 || int(apdu[4]) != len(apdu)-5 || len(apdu) > 255 {
			t.Fatal("sign APDU envelope differs", err)
		}
		if index == 0 {
			if len(apdu) != 27 || apdu[2] != 0 || !bytes.Equal(apdu[5:25], address[5:25]) {
				t.Fatal("sign init path or length differs")
			}
			payloadLength = int(binary.LittleEndian.Uint16(apdu[25:]))
		} else {
			kind := byte(1)
			if index == len(transcript.SigningApdus)-1 {
				kind = 2
			}
			if apdu[2] != kind {
				t.Fatal("sign APDU chunk ordering differs")
			}
			received = append(received, apdu[5:]...)
		}
	}
	payload, _ := hex.DecodeString(request.Config.Action.Payload[2:])
	if payloadLength != len(payload) || !bytes.Equal(received, append(payload, proof...)) || len(received) > ownerLedgerPayloadLimit {
		t.Fatal("native payload/proof boundary differs")
	}
	return append([]byte{0}, ed25519.Sign(key, received[:payloadLength])...)
}

// The app's typed reply is distinct from native signature files and APDU status.
func TestOwnerSigningLedgerTranscriptAndResponseFixture(t *testing.T) {
	_, request, _ := ownerSigningTestRequest(t)
	response := ownerSigningTestLedgerResponse(t, request)
	signature, err := ownerLedgerResponse(request, response)
	if err != nil || !bytes.Equal(signature, response[1:]) {
		t.Fatal("bounded synthetic response refused", err)
	}
	for _, malformed := range [][]byte{response[1:], append(bytes.Clone(response), 0x90, 0), append([]byte{1}, response[1:]...), make([]byte, 65)} {
		if _, err := ownerLedgerResponse(request, malformed); err == nil {
			t.Fatal("Ledger response guessed an algorithm, identity or status boundary")
		}
	}
	for _, proof := range [][]byte{nil, make([]byte, ownerLedgerPayloadLimit)} {
		if _, err := newOwnerLedgerTranscript(request, proof); err == nil {
			t.Fatal("empty or oversized Ledger request admitted")
		}
	}
	f := newOwnerTrimActionTestFixture(t)
	legacy, err := newOwnerSigningRequest(f.config, f.key, f.metadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newOwnerLedgerTranscript(legacy, []byte{1}); err == nil {
		t.Fatal("sr25519/disabled-metadata payload sent to Ledger generic app")
	}
	for _, path := range []string{"", "m/44'/354'", "m/44'/354'/01/0/0", "m/44'/354'/2147483648/0/0", "m/44'/355'/0'/0'/0'", "m/44'/354'/0h/0'/0'"} {
		if _, err := ownerLedgerDerivationPath(path); err == nil {
			t.Fatal("noncanonical or wrong app derivation path admitted", path)
		}
	}
}

// This helper exercises the public command using independent portable files.
func ownerSigningTestCommand(t *testing.T, mode, path string, trust ownerSigningTrust, extra ...string) ([]byte, string, int) {
	t.Helper()
	args := []string{"owner-signing", mode, "--request", path, "--accept-request-hash", trust.RequestHash,
		"--trim-approval-key", trust.ApprovalKey, "--owner-account-id", trust.Owner, "--expected-genesis", trust.Genesis}
	var stdout, stderr bytes.Buffer
	code := runMain(t.Context(), append(args, extra...), &stdout, &stderr)
	return stdout.Bytes(), stderr.String(), code
}

// Removing the host's entire custody directory proves the owner command does
// not require Snow paths or access. Public response verification remains exact.
func TestOwnerSigningCommandWorksWithoutHostCustody(t *testing.T) {
	f, request, trust := ownerSigningTestRequest(t)
	if err := os.RemoveAll(filepath.Dir(f.config.Action.StatePath)); err != nil {
		t.Fatal(err)
	}
	directory := ownerSigningTestDirectory(t)
	requestPath := filepath.Join(directory, "portable-owner-request.json")
	bootstrapRootTestWrite(t, requestPath, request)
	if raw, diagnostic, code := ownerSigningTestCommand(t, "inspect", requestPath, trust); code != 0 || !bytes.Contains(raw, []byte(`"device_qualified":false`)) || !bytes.Contains(raw, []byte("sudo_trim_to_max_allowed_uids")) {
		t.Fatalf("owner inspect: %d %s", code, diagnostic)
	}
	response := ownerSigningTestLedgerResponse(t, request)
	responsePath := filepath.Join(directory, "public-ledger-response.hex")
	if err := os.WriteFile(responsePath, []byte(hex.EncodeToString(response)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, diagnostic, code := ownerSigningTestCommand(t, "reply", requestPath, trust, "--ledger-response", responsePath)
	var reply ownerSigningReply
	if code != 0 || decodePlanJson(raw, &reply) != nil {
		t.Fatalf("owner reply: %d %s", code, diagnostic)
	}
	if _, err := reply.validate(request); err != nil {
		t.Fatal(err)
	}
	reference := bootstrapRootTestWrite(t, filepath.Join(directory, "public-reply.json"), reply)
	verified, diagnostic, code := ownerSigningTestCommand(t, "verify", requestPath, trust, "--reply", reference.Path, "--reply-sha256", reference.Sha256)
	if code != 0 || !bytes.Equal(raw, verified) {
		t.Fatalf("owner verify: %d %s", code, diagnostic)
	}
	for _, extra := range [][]string{{"--rpc", "http://192.0.2.1:9944"}, {"--private-key", "not-a-key"}, {"--run-dir", "/unavailable-snow-custody"}, {"--submit"}} {
		if _, _, code := ownerSigningTestCommand(t, "inspect", requestPath, trust, extra...); code != 2 {
			t.Fatal("owner command accepted network, secret or host custody flags")
		}
	}
	if _, err := os.Stat(filepath.Dir(f.config.Action.StatePath)); !os.IsNotExist(err) {
		t.Fatal("owner command recreated host custody", err)
	}
	if !reflect.DeepEqual(response, append([]byte{0}, ed25519.Sign(ownerSigningTestKey(), ownerSigningBytes(request.Config.Action))...)) {
		t.Fatal("fixture response changed exact signing bytes")
	}
}

// The transcript command retains its explicit unqualified status and refuses
// altered proof files before producing any protocol bytes.
func TestOwnerSigningLedgerPlanCommandPinsProofAndRefusesDeviceFlags(t *testing.T) {
	_, request, trust := ownerSigningTestRequest(t)
	directory := ownerSigningTestDirectory(t)
	requestPath := filepath.Join(directory, "request.json")
	bootstrapRootTestWrite(t, requestPath, request)
	proof := []byte("synthetic bounded proof bytes, not an RFC78 proof")
	proofPath := filepath.Join(directory, "proof.scale")
	if err := os.WriteFile(proofPath, proof, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(proof)
	extra := []string{"--ledger-metadata", proofPath, "--ledger-metadata-sha256", "sha256:" + hex.EncodeToString(hash[:])}
	raw, diagnostic, code := ownerSigningTestCommand(t, "ledger-plan", requestPath, trust, extra...)
	var transcript ownerLedgerTranscript
	if code != 0 || decodePlanJson(raw, &transcript) != nil || transcript.RequestHash != request.ContentHash || transcript.DeviceQualified ||
		transcript.MetadataProofVerified || transcript.Signing || transcript.NetworkEffects || len(transcript.SigningApdus) < 2 {
		t.Fatalf("offline Ledger plan: %d %s", code, diagnostic)
	}
	extra[len(extra)-1] = rootObjectHash("wrong proof file")
	if raw, _, code := ownerSigningTestCommand(t, "ledger-plan", requestPath, trust, extra...); code != 2 || len(raw) != 0 {
		t.Fatal("wrong proof pin produced protocol bytes", code)
	}
	if raw, _, code := ownerSigningTestCommand(t, "sign", requestPath, trust, "--device", "ledger"); code != 2 || len(raw) != 0 {
		t.Fatal("unqualified device signing was reachable", code)
	}
	encoded, _ := json.Marshal(request)
	duplicate := append([]byte(`{"schema":"duplicate",`), encoded[1:]...)
	if err := os.WriteFile(requestPath, duplicate, 0600); err != nil {
		t.Fatal(err)
	}
	if raw, _, code := ownerSigningTestCommand(t, "inspect", requestPath, trust); code != 3 || len(raw) != 0 {
		t.Fatal("duplicate request fields admitted", code)
	}
}
