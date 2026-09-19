package protocol

// These tests cover fixed-width consent, not historical validator eligibility.
// Golden bytes come from the independent literal-width encoder in testdata.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/vedhavyas/go-subkey/v2"
	"github.com/vedhavyas/go-subkey/v2/sr25519"
)

// Synthetic public test identities match the independent reference fixture.
func validatorEvidenceFixture(t *testing.T) (ValidatorEvidenceHeader, ed25519.PrivateKey, subkey.KeyPair) {
	t.Helper()
	var vpkSeed, hotkeySeed [32]byte
	for index := range vpkSeed {
		vpkSeed[index], hotkeySeed[index] = byte(index), byte(0x80+index)
	}
	vpkKey := ed25519.NewKeyFromSeed(vpkSeed[:])
	hotkeyKey, err := (sr25519.Scheme{}).FromSeed(hotkeySeed[:])
	if err != nil {
		t.Fatal(err)
	}
	var header ValidatorEvidenceHeader
	header.Domain.ChainID, header.Domain.Netuid, header.Domain.ActivationEpoch = 945, 17, 42
	for index := range header.Domain.Coordinator {
		header.Domain.Coordinator[index], header.Domain.SettlementVault[index] = 0x11, 0x12
	}
	for index := range header.Domain.GenesisHash {
		header.Domain.GenesisHash[index], header.Domain.DeploymentIDHash[index], header.Domain.PolicyHash[index], header.Domain.ActivationHash[index] = 0x13, 0x14, 0x15, 0x16
		header.BoundaryHash[index], header.CensusHash[index], header.PayloadHash[index] = 0x17, 0x18, 0x19
	}
	copy(header.Hotkey[:], hotkeyKey.Public())
	copy(header.VPK[:], vpkKey.Public().(ed25519.PublicKey))
	header.NoID, header.Epoch, header.Kind, header.BoundaryBlock, header.PayloadBytes = 7, 44, ValidatorEvidenceClosedCensus, 1059, 4096
	return header, vpkKey, hotkeyKey
}

// This trusted fixture closes epoch 44 at block 1060, excluding that next block.
func validatorEvidenceWindow(header ValidatorEvidenceHeader) ValidatorEvidenceWindow {
	return ValidatorEvidenceWindow{Epoch: 44, StartBlock: 1000, EndBlock: 1060, FinalizedBlock: 1080, Subject: header.Subject}
}

// Both signing keys consent to the same digest, without any native intent.
func validatorEvidenceSign(t *testing.T, header ValidatorEvidenceHeader, vpkKey ed25519.PrivateKey, hotkeyKey subkey.KeyPair) ([]byte, []byte) {
	t.Helper()
	vpkSignature, err := header.SignVPK(vpkKey)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := header.Digest()
	if err != nil {
		t.Fatal(err)
	}
	hotkeySignature, err := hotkeyKey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return vpkSignature, hotkeySignature
}

// Every field is changed to another valid value; slot stability is intentional.
type validatorEvidenceMutation struct {
	name     string
	change   func(*ValidatorEvidenceHeader)
	sameSlot bool
}

// The audit fixture leaves room for every single-field epoch change.
func validatorEvidenceMutations() []validatorEvidenceMutation {
	return []validatorEvidenceMutation{
		{name: "chain", change: func(h *ValidatorEvidenceHeader) { h.Domain.ChainID++ }},
		{name: "genesis", change: func(h *ValidatorEvidenceHeader) { h.Domain.GenesisHash[0] ^= 1 }},
		{name: "netuid", change: func(h *ValidatorEvidenceHeader) { h.Domain.Netuid++ }},
		{name: "coordinator", change: func(h *ValidatorEvidenceHeader) { h.Domain.Coordinator[0] ^= 1 }},
		{name: "vault", change: func(h *ValidatorEvidenceHeader) { h.Domain.SettlementVault[0] ^= 1 }},
		{name: "deployment", change: func(h *ValidatorEvidenceHeader) { h.Domain.DeploymentIDHash[0] ^= 1 }},
		{name: "policy", change: func(h *ValidatorEvidenceHeader) { h.Domain.PolicyHash[0] ^= 1 }, sameSlot: true},
		{name: "activation epoch", change: func(h *ValidatorEvidenceHeader) { h.Domain.ActivationEpoch++ }, sameSlot: true},
		{name: "activation anchor", change: func(h *ValidatorEvidenceHeader) { h.Domain.ActivationHash[0] ^= 1 }, sameSlot: true},
		{name: "hotkey", change: func(h *ValidatorEvidenceHeader) { h.Hotkey[0] ^= 1 }},
		{name: "operator", change: func(h *ValidatorEvidenceHeader) { h.NoID++ }},
		{name: "closed epoch", change: func(h *ValidatorEvidenceHeader) { h.Epoch-- }},
		{name: "kind", change: func(h *ValidatorEvidenceHeader) {
			h.Kind = ValidatorEvidenceClosedCensus
			h.Subject = ValidatorEvidenceSubject{}
		}},
		{name: "observation epoch", change: func(h *ValidatorEvidenceHeader) { h.Subject.ObservationEpoch++ }},
		{name: "native cycle", change: func(h *ValidatorEvidenceHeader) { h.Subject.NativeEpoch++ }},
		{name: "vpk", change: func(h *ValidatorEvidenceHeader) { h.VPK[0] ^= 1 }, sameSlot: true},
		{name: "boundary block", change: func(h *ValidatorEvidenceHeader) { h.BoundaryBlock++ }, sameSlot: true},
		{name: "boundary hash", change: func(h *ValidatorEvidenceHeader) { h.BoundaryHash[0] ^= 1 }, sameSlot: true},
		{name: "census", change: func(h *ValidatorEvidenceHeader) { h.CensusHash[0] ^= 1 }, sameSlot: true},
		{name: "payload hash", change: func(h *ValidatorEvidenceHeader) { h.PayloadHash[0] ^= 1 }, sameSlot: true},
		{name: "payload bytes", change: func(h *ValidatorEvidenceHeader) { h.PayloadBytes++ }, sameSlot: true},
	}
}

// Both languages pin these saved bytes, including verification of saved random
// sr25519 signatures. Fresh sr25519 signatures need not be byte-identical.
func TestValidatorEvidenceGoldenVectors(t *testing.T) {
	encoded, err := os.ReadFile("testdata/validator-evidence-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	vectorKVs := map[string]map[string]string{}
	if err := json.Unmarshal(encoded, &vectorKVs); err != nil || len(vectorKVs) != 2 {
		t.Fatalf("golden vector framing: %v", err)
	}
	for _, name := range []string{"closed", "audit"} {
		header, vpkKey, _ := validatorEvidenceFixture(t)
		if name == "audit" {
			header.Kind = ValidatorEvidenceDepositAudit
			header.Subject = ValidatorEvidenceSubject{ObservationEpoch: 45, NativeEpoch: 700}
			header.BoundaryBlock, header.PayloadBytes = 1070, 512
		}
		vector := vectorKVs[name]
		if len(vector) != 8 {
			t.Fatalf("%s vector has %d fields", name, len(vector))
		}
		payload, err := header.Payload()
		if err != nil {
			t.Fatal(err)
		}
		digest, _ := header.Digest()
		slot, _ := header.SlotKey()
		subject, _ := header.SubjectHash()
		vpkSignature, err := header.SignVPK(vpkKey)
		if err != nil {
			t.Fatal(err)
		}
		if len(payload) != ValidatorEvidenceHeaderPayloadSize {
			t.Fatalf("%s payload width: %d", name, len(payload))
		}
		for field, actual := range map[string]string{
			"payload": hex.EncodeToString(payload), "digest": hex.EncodeToString(digest[:]),
			"slot_key": hex.EncodeToString(slot[:]), "subject_hash": hex.EncodeToString(subject[:]),
			"hotkey": hex.EncodeToString(header.Hotkey[:]), "vpk": hex.EncodeToString(header.VPK[:]),
			"vpk_signature": hex.EncodeToString(vpkSignature),
		} {
			if actual != vector[field] {
				t.Errorf("%s %s differs from independent golden", name, field)
			}
		}
		hotkeySignature, err := hex.DecodeString(vector["hotkey_signature"])
		if err != nil || len(hotkeySignature) != 64 {
			t.Fatalf("%s hotkey signature encoding: %v", name, err)
		}
		if err := header.Verify(header.Domain, validatorEvidenceWindow(header), vpkSignature, hotkeySignature); err != nil {
			t.Fatalf("%s saved signatures: %v", name, err)
		}
	}
}

// Consent covers every field, including fields deliberately excluded from slots.
func TestValidatorEvidenceEveryFieldBindsDigest(t *testing.T) {
	header, vpkKey, hotkeyKey := validatorEvidenceFixture(t)
	header.Kind = ValidatorEvidenceDepositAudit
	header.Subject = ValidatorEvidenceSubject{ObservationEpoch: 50, NativeEpoch: 700}
	header.BoundaryBlock = 1070
	vpkSignature, hotkeySignature := validatorEvidenceSign(t, header, vpkKey, hotkeyKey)
	digest, _ := header.Digest()
	for _, mutation := range validatorEvidenceMutations() {
		changed := header
		mutation.change(&changed)
		changedDigest, err := changed.Digest()
		if err != nil || changedDigest == digest {
			t.Errorf("%s did not change a valid digest: %v", mutation.name, err)
		}
		if changed.VerifyVPK(vpkSignature) || changed.VerifyHotkey(hotkeySignature) {
			t.Errorf("%s accepted old consent", mutation.name)
		}
	}
}

// Valid signatures over a caller-selected deployment do not authenticate it.
func TestValidatorEvidenceExpectedDomainRejectsResignedInputs(t *testing.T) {
	header, vpkKey, hotkeyKey := validatorEvidenceFixture(t)
	expected, window := header.Domain, validatorEvidenceWindow(header)
	for _, mutation := range validatorEvidenceMutations()[:9] {
		changed := header
		mutation.change(&changed)
		vpkSignature, hotkeySignature := validatorEvidenceSign(t, changed, vpkKey, hotkeyKey)
		if !changed.VerifyVPK(vpkSignature) || !changed.VerifyHotkey(hotkeySignature) {
			t.Fatalf("%s control was not freshly signed", mutation.name)
		}
		if err := changed.Verify(expected, window, vpkSignature, hotkeySignature); err == nil {
			t.Errorf("%s authenticated its own untrusted domain", mutation.name)
		}
	}
}

// Pure history checks reject future, pre-activation and nonterminal boundaries.
func TestValidatorEvidenceWindowAndActivationBounds(t *testing.T) {
	header, _, _ := validatorEvidenceFixture(t)
	window := validatorEvidenceWindow(header)
	for _, change := range []struct {
		name   string
		header func(*ValidatorEvidenceHeader)
		window func(*ValidatorEvidenceWindow)
	}{
		{name: "wrong epoch", window: func(w *ValidatorEvidenceWindow) { w.Epoch++ }},
		{name: "unknown start", window: func(w *ValidatorEvidenceWindow) { w.StartBlock = 0 }},
		{name: "empty window", window: func(w *ValidatorEvidenceWindow) { w.EndBlock = w.StartBlock }},
		{name: "unclosed window", window: func(w *ValidatorEvidenceWindow) { w.FinalizedBlock = w.EndBlock - 1 }},
		{name: "future block", header: func(h *ValidatorEvidenceHeader) { h.BoundaryBlock = window.FinalizedBlock + 1 }},
		{name: "early terminal", header: func(h *ValidatorEvidenceHeader) { h.BoundaryBlock-- }},
		{name: "next epoch terminal", header: func(h *ValidatorEvidenceHeader) { h.BoundaryBlock++ }},
		{name: "wrong expected subject", window: func(w *ValidatorEvidenceWindow) { w.Subject.NativeEpoch = 1 }},
		{name: "before activation", header: func(h *ValidatorEvidenceHeader) { h.Epoch = h.Domain.ActivationEpoch - 1 }},
	} {
		changed, changedWindow := header, window
		if change.header != nil {
			change.header(&changed)
		}
		if change.window != nil {
			change.window(&changedWindow)
		}
		if err := changed.ValidateAt(header.Domain, changedWindow); err == nil {
			t.Errorf("%s accepted", change.name)
		}
	}
	header.Domain.ActivationEpoch, header.Epoch = 0, 0
	header.BoundaryBlock = 1
	window = ValidatorEvidenceWindow{Epoch: 0, StartBlock: 1, EndBlock: 2, FinalizedBlock: 2}
	if err := header.ValidateAt(header.Domain, window); err != nil {
		t.Fatalf("anchored fresh deployment rejected: %v", err)
	}
	header.Domain.ChainID, header.Domain.Netuid = math.MaxUint64, math.MaxUint16
	header.Domain.ActivationEpoch, header.Epoch, header.NoID, header.PayloadBytes = math.MaxUint64, math.MaxUint64, math.MaxUint64, math.MaxUint64
	header.BoundaryBlock = math.MaxUint64 - 1
	window = ValidatorEvidenceWindow{Epoch: math.MaxUint64, StartBlock: math.MaxUint64 - 2, EndBlock: math.MaxUint64, FinalizedBlock: math.MaxUint64}
	if err := header.ValidateAt(header.Domain, window); err != nil {
		t.Fatalf("exact maximum fixed-width values rejected/overflowed: %v", err)
	}
	if payload, err := header.Payload(); err != nil || len(payload) != ValidatorEvidenceHeaderPayloadSize {
		t.Fatalf("maximum-width encoding changed: %d, %v", len(payload), err)
	}
	header.Kind = ValidatorEvidenceDepositAudit
	header.Subject.ObservationEpoch = math.MaxUint64
	if err := header.Validate(); err == nil {
		t.Fatal("maximum epoch wrapped into an alleged later audit")
	}
}

// Rotating keys, content and policy cannot acquire a second owner slot.
func TestValidatorEvidenceSlotExcludesMutableContent(t *testing.T) {
	header, _, _ := validatorEvidenceFixture(t)
	header.Kind = ValidatorEvidenceDepositAudit
	header.Subject = ValidatorEvidenceSubject{ObservationEpoch: 50, NativeEpoch: 700}
	header.BoundaryBlock = 1070
	slot, _ := header.SlotKey()
	for _, mutation := range validatorEvidenceMutations() {
		if !mutation.sameSlot {
			continue
		}
		changed := header
		mutation.change(&changed)
		if actual, err := changed.SlotKey(); err != nil || actual != slot {
			t.Errorf("%s created another slot: %v", mutation.name, err)
		}
	}
	header.Kind, header.Subject = ValidatorEvidenceClosedCensus, ValidatorEvidenceSubject{}
	slot, _ = header.SlotKey()
	header.BoundaryBlock++
	header.BoundaryHash[0] ^= 1
	if actual, err := header.SlotKey(); err != nil || actual != slot {
		t.Fatalf("alternate terminal boundary created another slot: %v", err)
	}
}

// A different hotkey/operator/epoch/deployment or actual audit cycle is distinct.
func TestValidatorEvidenceSlotSeparatesOwnersAndAuditCycles(t *testing.T) {
	header, _, _ := validatorEvidenceFixture(t)
	header.Kind = ValidatorEvidenceDepositAudit
	header.Subject = ValidatorEvidenceSubject{ObservationEpoch: 50, NativeEpoch: 700}
	header.BoundaryBlock = 1070
	slot, _ := header.SlotKey()
	seenSlots := map[[32]byte]string{slot: "original"}
	for _, mutation := range validatorEvidenceMutations() {
		if mutation.sameSlot {
			continue
		}
		changed := header
		mutation.change(&changed)
		actual, err := changed.SlotKey()
		if err != nil {
			t.Fatal(err)
		}
		if prior, ok := seenSlots[actual]; ok {
			t.Errorf("%s collided with %s", mutation.name, prior)
		}
		seenSlots[actual] = mutation.name
	}
}

// Legacy raw messages, wrong keys and truncated/extended signatures cannot
// authorize the fixed digest, even when a library would truncate long input.
func TestValidatorEvidenceSignatureShapeAndCrossDomainRejection(t *testing.T) {
	header, vpkKey, hotkeyKey := validatorEvidenceFixture(t)
	vpkSignature, hotkeySignature := validatorEvidenceSign(t, header, vpkKey, hotkeyKey)
	for _, signature := range [][]byte{nil, vpkSignature[:63], append(append([]byte(nil), vpkSignature...), 0), bytes.Repeat([]byte{0}, 64)} {
		if header.VerifyVPK(signature) {
			t.Error("malformed VPK signature accepted")
		}
	}
	for _, signature := range [][]byte{nil, hotkeySignature[:63], append(append([]byte(nil), hotkeySignature...), 0), bytes.Repeat([]byte{0}, 64)} {
		if header.VerifyHotkey(signature) {
			t.Error("malformed hotkey signature accepted")
		}
	}
	payload, _ := header.Payload()
	rawVPK := ed25519.Sign(vpkKey, payload)
	rawHotkey, err := hotkeyKey.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	if header.VerifyVPK(rawVPK) || header.VerifyHotkey(rawHotkey) {
		t.Fatal("raw-message signatures accepted for fixed-digest consent")
	}
	digest, _ := header.Digest()
	wrongVPK := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x55}, 32))
	wrongHotkey, err := (sr25519.Scheme{}).FromSeed(bytes.Repeat([]byte{0x56}, 32))
	if err != nil {
		t.Fatal(err)
	}
	wrongHotkeySignature, err := wrongHotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	if header.VerifyVPK(ed25519.Sign(wrongVPK, digest[:])) || header.VerifyHotkey(wrongHotkeySignature) {
		t.Fatal("another key authorized the victim hotkey/operator slot")
	}
}

// The public half of a 64-byte private key is not proof that its seed matches.
func TestValidatorEvidencePrivateKeyConsistency(t *testing.T) {
	header, privateKey, _ := validatorEvidenceFixture(t)
	malformed := append(ed25519.PrivateKey(nil), privateKey...)
	malformed[0] ^= 1
	if !bytes.Equal(malformed.Public().(ed25519.PublicKey), header.VPK[:]) {
		t.Fatal("malformed-key control no longer preserves the declared public half")
	}
	for _, key := range []ed25519.PrivateKey{nil, privateKey[:63], append(append(ed25519.PrivateKey(nil), privateKey...), 0), malformed} {
		if signature, err := header.SignVPK(key); err == nil || signature != nil {
			t.Error("inconsistent private key emitted a signature")
		}
	}
	header.VPK[0] ^= 1
	if signature, err := header.SignVPK(privateKey); err == nil || signature != nil {
		t.Fatal("matching seed with different declared VPK signed")
	}
}

// Missing identity, content and domain values fail before hashing or signing.
func TestValidatorEvidenceRejectsIncompleteShape(t *testing.T) {
	header, privateKey, _ := validatorEvidenceFixture(t)
	for _, change := range []struct {
		name   string
		mutate func(*ValidatorEvidenceHeader)
	}{
		{name: "chain", mutate: func(h *ValidatorEvidenceHeader) { h.Domain.ChainID = 0 }},
		{name: "genesis", mutate: func(h *ValidatorEvidenceHeader) { h.Domain.GenesisHash = [32]byte{} }},
		{name: "netuid", mutate: func(h *ValidatorEvidenceHeader) { h.Domain.Netuid = 0 }},
		{name: "coordinator", mutate: func(h *ValidatorEvidenceHeader) { h.Domain.Coordinator = [20]byte{} }},
		{name: "vault", mutate: func(h *ValidatorEvidenceHeader) { h.Domain.SettlementVault = [20]byte{} }},
		{name: "aliased vault", mutate: func(h *ValidatorEvidenceHeader) { h.Domain.SettlementVault = h.Domain.Coordinator }},
		{name: "deployment", mutate: func(h *ValidatorEvidenceHeader) { h.Domain.DeploymentIDHash = [32]byte{} }},
		{name: "policy", mutate: func(h *ValidatorEvidenceHeader) { h.Domain.PolicyHash = [32]byte{} }},
		{name: "activation", mutate: func(h *ValidatorEvidenceHeader) { h.Domain.ActivationHash = [32]byte{} }},
		{name: "hotkey", mutate: func(h *ValidatorEvidenceHeader) { h.Hotkey = [32]byte{} }},
		{name: "vpk", mutate: func(h *ValidatorEvidenceHeader) { h.VPK = [32]byte{} }},
		{name: "operator", mutate: func(h *ValidatorEvidenceHeader) { h.NoID = 0 }},
		{name: "boundary block", mutate: func(h *ValidatorEvidenceHeader) { h.BoundaryBlock = 0 }},
		{name: "boundary hash", mutate: func(h *ValidatorEvidenceHeader) { h.BoundaryHash = [32]byte{} }},
		{name: "census", mutate: func(h *ValidatorEvidenceHeader) { h.CensusHash = [32]byte{} }},
		{name: "payload hash", mutate: func(h *ValidatorEvidenceHeader) { h.PayloadHash = [32]byte{} }},
		{name: "payload bytes", mutate: func(h *ValidatorEvidenceHeader) { h.PayloadBytes = 0 }},
		{name: "unknown kind", mutate: func(h *ValidatorEvidenceHeader) { h.Kind = 0 }},
		{name: "terminal observation", mutate: func(h *ValidatorEvidenceHeader) { h.Subject.ObservationEpoch = 1 }},
		{name: "terminal native cycle", mutate: func(h *ValidatorEvidenceHeader) { h.Subject.NativeEpoch = 1 }},
	} {
		changed := header
		change.mutate(&changed)
		if err := changed.Validate(); err == nil {
			t.Errorf("%s accepted", change.name)
		}
		if payload, err := changed.Payload(); err == nil || payload != nil {
			t.Errorf("%s emitted signing bytes", change.name)
		}
		if signature, err := changed.SignVPK(privateKey); err == nil || signature != nil {
			t.Errorf("%s emitted consent", change.name)
		}
	}
}

// Returned bytes are detached and signing never rewrites the caller's key.
func TestValidatorEvidencePayloadOwnership(t *testing.T) {
	header, privateKey, _ := validatorEvidenceFixture(t)
	originalHeader, originalKey := header, append(ed25519.PrivateKey(nil), privateKey...)
	payload, _ := header.Payload()
	originalPayload := append([]byte(nil), payload...)
	payload[0] ^= 1
	signature, err := header.SignVPK(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	signature[0] ^= 1
	freshPayload, _ := header.Payload()
	freshSignature, err := header.SignVPK(privateKey)
	if err != nil || !header.VerifyVPK(freshSignature) || !bytes.Equal(freshPayload, originalPayload) || !bytes.Equal(privateKey, originalKey) || header != originalHeader {
		t.Fatalf("returned bytes or signing aliased source ownership: %v", err)
	}
}

// Transport field names are explicit; JSON is not the canonical signing codec.
// Unsigned scalar widths reject overflow instead of silently truncating.
func TestValidatorEvidenceTransportIntegerBounds(t *testing.T) {
	header, _, _ := validatorEvidenceFixture(t)
	encoded, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"chain_id", "genesis_hash", "netuid", "coordinator", "settlement_vault", "deployment_id_hash", "policy_hash", "activation_epoch", "activation_hash", "hotkey", "no_id", "epoch", "kind", "subject", "observation_epoch", "native_epoch", "vpk", "boundary_block", "boundary_hash", "census_hash", "payload_hash", "payload_bytes"} {
		if !bytes.Contains(encoded, []byte("\""+field+"\":")) {
			t.Errorf("transport field %s lost its explicit name", field)
		}
	}
	for _, malformed := range []string{
		"{\"no_id\":-1}", "{\"no_id\":18446744073709551616}", "{\"payload_bytes\":1.5}",
		"{\"kind\":256}", "{\"domain\":{\"netuid\":65536}}", "{\"domain\":{\"chain_id\":18446744073709551616}}",
		"{\"subject\":{\"native_epoch\":-1}}",
	} {
		var parsed ValidatorEvidenceHeader
		if err := json.Unmarshal([]byte(malformed), &parsed); err == nil {
			t.Errorf("out-of-range scalar accepted: %s", malformed)
		}
	}
	if strings.Contains(string(encoded), "\"PayloadBytes\"") {
		t.Fatal("default Go casing became transport schema")
	}
}

// A later audit needs neither an operator payout nor a prepared native intent.
// Historical role/VPK eligibility remains a separate public-replay obligation.
func TestValidatorEvidenceLateAuditIndependentOfPayoutsAndIntents(t *testing.T) {
	header, vpkKey, hotkeyKey := validatorEvidenceFixture(t)
	header.Kind = ValidatorEvidenceDepositAudit
	header.Subject = ValidatorEvidenceSubject{ObservationEpoch: 45, NativeEpoch: 0}
	header.BoundaryBlock, header.PayloadBytes = 1070, 512
	vpkSignature, hotkeySignature := validatorEvidenceSign(t, header, vpkKey, hotkeyKey)
	window := validatorEvidenceWindow(header)
	if err := header.Verify(header.Domain, window, vpkSignature, hotkeySignature); err != nil {
		t.Fatalf("intent-free late audit rejected: %v", err)
	}
	for _, mutation := range []func(*ValidatorEvidenceHeader){
		func(h *ValidatorEvidenceHeader) { h.Subject.ObservationEpoch++ },
		func(h *ValidatorEvidenceHeader) { h.Subject.NativeEpoch++ },
		func(h *ValidatorEvidenceHeader) { h.BoundaryBlock = window.EndBlock - 1 },
	} {
		changed := header
		mutation(&changed)
		newVPK, newHotkey := validatorEvidenceSign(t, changed, vpkKey, hotkeyKey)
		if err := changed.Verify(header.Domain, window, newVPK, newHotkey); err == nil {
			t.Error("fresh consent bypassed the expected audit cycle/closed boundary")
		}
	}
	header.Subject.ObservationEpoch = header.Epoch
	if err := header.Validate(); err == nil {
		t.Fatal("same-epoch audit accepted as later observation")
	}
	header, _, _ = validatorEvidenceFixture(t)
	header.Kind = 3
	if err := header.Validate(); err == nil {
		t.Fatal("unsupported ordinary-cut kind accepted as closed evidence")
	}
}
