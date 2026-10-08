package protocol

// Real signing keys exercise earlier activation consent. No fixture claims
// chain inclusion, historical permit authority or completed migration replay.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/vedhavyas/go-subkey/v2"
)

// Uses the established synthetic evidence identities and a nonempty prefix.
func validatorEvidenceActivationFixture(t *testing.T) (ValidatorEvidenceActivation, ed25519.PrivateKey, subkey.KeyPair) {
	t.Helper()
	header, vpkKey, hotkeyKey := validatorEvidenceFixture(t)
	return ValidatorEvidenceActivation{
		Domain: ValidatorEvidenceActivationDomain{
			ChainID: header.Domain.ChainID, GenesisHash: header.Domain.GenesisHash,
			Netuid: header.Domain.Netuid, Coordinator: header.Domain.Coordinator,
			SettlementVault: header.Domain.SettlementVault, DeploymentIDHash: header.Domain.DeploymentIDHash,
			PolicyHash: header.Domain.PolicyHash, Epoch: header.Domain.ActivationEpoch,
		},
		Hotkey: header.Hotkey, NoID: header.NoID, VPK: header.VPK,
		FirstSequence: 5, PriorRoot: [32]byte{0x21}, NativeBlock: 500,
		NativeHash: [32]byte{0x22}, EVMBlock: 980, EVMHash: [32]byte{0x23},
	}, vpkKey, hotkeyKey
}

// No raw FINAL or native-weight signature is reused for the activation domain.
func signValidatorEvidenceActivationTest(t *testing.T, activation ValidatorEvidenceActivation, vpkKey ed25519.PrivateKey, hotkeyKey subkey.KeyPair) ([]byte, []byte) {
	t.Helper()
	vpkSignature, err := activation.SignVPK(vpkKey)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := activation.Digest()
	if err != nil {
		t.Fatal(err)
	}
	hotkeySignature, err := hotkeyKey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return vpkSignature, hotkeySignature
}

// Each mutation changes exactly one fixed field without invalidating shape.
type validatorEvidenceActivationMutation struct {
	name string
	key  bool
	edit func(*ValidatorEvidenceActivation)
}

func validatorEvidenceActivationMutations() []validatorEvidenceActivationMutation {
	return []validatorEvidenceActivationMutation{
		{name: "chain", edit: func(a *ValidatorEvidenceActivation) { a.Domain.ChainID++ }},
		{name: "genesis", edit: func(a *ValidatorEvidenceActivation) { a.Domain.GenesisHash[0]++ }},
		{name: "netuid", edit: func(a *ValidatorEvidenceActivation) { a.Domain.Netuid++ }},
		{name: "coordinator", edit: func(a *ValidatorEvidenceActivation) { a.Domain.Coordinator[0]++ }},
		{name: "vault", edit: func(a *ValidatorEvidenceActivation) { a.Domain.SettlementVault[0]++ }},
		{name: "deployment", edit: func(a *ValidatorEvidenceActivation) { a.Domain.DeploymentIDHash[0]++ }},
		{name: "policy", edit: func(a *ValidatorEvidenceActivation) { a.Domain.PolicyHash[0]++ }},
		{name: "epoch", edit: func(a *ValidatorEvidenceActivation) { a.Domain.Epoch++ }},
		{name: "hotkey", key: true, edit: func(a *ValidatorEvidenceActivation) { a.Hotkey[0] ^= 1 }},
		{name: "operator", edit: func(a *ValidatorEvidenceActivation) { a.NoID++ }},
		{name: "vpk", key: true, edit: func(a *ValidatorEvidenceActivation) { a.VPK[0] ^= 1 }},
		{name: "first sequence", edit: func(a *ValidatorEvidenceActivation) { a.FirstSequence++ }},
		{name: "prior root", edit: func(a *ValidatorEvidenceActivation) { a.PriorRoot[0]++ }},
		{name: "native block", edit: func(a *ValidatorEvidenceActivation) { a.NativeBlock++ }},
		{name: "native hash", edit: func(a *ValidatorEvidenceActivation) { a.NativeHash[0]++ }},
		{name: "EVM block", edit: func(a *ValidatorEvidenceActivation) { a.EVMBlock++ }},
		{name: "EVM hash", edit: func(a *ValidatorEvidenceActivation) { a.EVMHash[0]++ }},
	}
}

// Independent binary.Write encoding pins field order and concrete widths rather
// than calling any production domain/payload helper to construct the oracle.
func TestValidatorEvidenceActivationPayloadMatchesIndependentWidths(t *testing.T) {
	t.Parallel()
	activation, _, _ := validatorEvidenceActivationFixture(t)
	var reference bytes.Buffer
	reference.WriteString("urnetwork/validator-evidence-activation/v1\x00")
	for _, field := range []any{
		activation.Domain.ChainID, activation.Domain.GenesisHash, activation.Domain.Netuid,
		activation.Domain.Coordinator, activation.Domain.SettlementVault,
		activation.Domain.DeploymentIDHash, activation.Domain.PolicyHash, activation.Domain.Epoch,
		activation.Hotkey, activation.NoID, activation.VPK, activation.FirstSequence, activation.PriorRoot,
		activation.NativeBlock, activation.NativeHash, activation.EVMBlock, activation.EVMHash,
	} {
		if err := binary.Write(&reference, binary.BigEndian, field); err != nil {
			t.Fatal(err)
		}
	}
	got, err := activation.Payload()
	if err != nil || len(got) != ValidatorEvidenceActivationPayloadSize || !bytes.Equal(got, reference.Bytes()) {
		t.Fatalf("activation payload differs: bytes=%d expected=%d error=%v", len(got), reference.Len(), err)
	}
	digest, err := activation.Digest()
	if err != nil || digest != sha256.Sum256(reference.Bytes()) {
		t.Fatalf("activation digest differs from independent fixed-width bytes: %v", err)
	}
}

// Added fields require an explicit encoder and mutation-census review.
func TestValidatorEvidenceActivationFieldCensusIsComplete(t *testing.T) {
	t.Parallel()
	activationType := reflect.TypeOf(ValidatorEvidenceActivation{})
	domainType := reflect.TypeOf(ValidatorEvidenceActivationDomain{})
	if activationType.NumField() != 10 || domainType.NumField() != 8 || len(validatorEvidenceActivationMutations()) != activationType.NumField()-1+domainType.NumField() {
		t.Fatal("activation fixed-field and mutation census changed")
	}
	for _, recordType := range []reflect.Type{activationType, domainType} {
		for index := 0; index < recordType.NumField(); index++ {
			field := recordType.Field(index)
			if !field.IsExported() || field.Tag.Get("json") == "" {
				t.Fatalf("activation field has no explicit public transport identity: %s", field.Name)
			}
			switch field.Type.Kind() {
			case reflect.Uint16, reflect.Uint64:
			case reflect.Array:
				if field.Type.Elem().Kind() != reflect.Uint8 || field.Type.Len() != 20 && field.Type.Len() != 32 {
					t.Fatalf("activation field is not a reviewed fixed byte width: %s", field.Name)
				}
			case reflect.Struct:
				if field.Type != domainType {
					t.Fatalf("activation embeds an unreviewed type: %s", field.Name)
				}
			default:
				t.Fatalf("activation field is not fixed-width: %s", field.Name)
			}
		}
	}
}

// Every migration, chain and identity field participates in the earlier hash.
func TestValidatorEvidenceActivationEveryFieldBindsConsent(t *testing.T) {
	t.Parallel()
	activation, vpkKey, hotkeyKey := validatorEvidenceActivationFixture(t)
	vpkSignature, hotkeySignature := signValidatorEvidenceActivationTest(t, activation, vpkKey, hotkeyKey)
	originalDigest, _ := activation.Digest()
	for _, mutation := range validatorEvidenceActivationMutations() {
		changed := activation
		mutation.edit(&changed)
		digest, err := changed.Digest()
		if err != nil || digest == originalDigest || changed.VerifyVPK(vpkSignature) || changed.VerifyHotkey(hotkeySignature) {
			t.Fatalf("%s retained old digest/consent or changed shape: %v", mutation.name, err)
		}
	}
}

// Even new valid signatures cannot replace the independently pinned record.
func TestValidatorEvidenceActivationRefusesResignedExpectedContextDrift(t *testing.T) {
	t.Parallel()
	activation, vpkKey, hotkeyKey := validatorEvidenceActivationFixture(t)
	for _, mutation := range validatorEvidenceActivationMutations() {
		if mutation.key {
			continue
		}
		changed := activation
		mutation.edit(&changed)
		vpkSignature, hotkeySignature := signValidatorEvidenceActivationTest(t, changed, vpkKey, hotkeyKey)
		if err := changed.Verify(changed, vpkSignature, hotkeySignature); err != nil {
			t.Fatalf("%s fixture does not have genuine consent: %v", mutation.name, err)
		}
		if err := changed.Verify(activation, vpkSignature, hotkeySignature); err == nil {
			t.Fatalf("%s replaced the independently expected activation", mutation.name)
		}
	}
}

// Missing, reordered, truncated or extended signatures cannot satisfy consent.
func TestValidatorEvidenceActivationRequiresBothExactOwners(t *testing.T) {
	t.Parallel()
	activation, vpkKey, hotkeyKey := validatorEvidenceActivationFixture(t)
	vpkSignature, hotkeySignature := signValidatorEvidenceActivationTest(t, activation, vpkKey, hotkeyKey)
	if err := activation.Verify(activation, vpkSignature, hotkeySignature); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ vpk, hotkey []byte }{
		{vpk: nil, hotkey: hotkeySignature}, {vpk: vpkSignature, hotkey: nil},
		{vpk: hotkeySignature, hotkey: vpkSignature},
		{vpk: vpkSignature[:63], hotkey: hotkeySignature},
		{vpk: vpkSignature, hotkey: hotkeySignature[:63]},
		{vpk: append(bytes.Clone(vpkSignature), 0), hotkey: hotkeySignature},
		{vpk: vpkSignature, hotkey: append(bytes.Clone(hotkeySignature), 0)},
	} {
		if err := activation.Verify(activation, entry.vpk, entry.hotkey); err == nil {
			t.Fatal("incomplete or substituted activation ownership was accepted")
		}
	}
}

// The original header signatures cannot authorize an earlier activation record.
func TestValidatorEvidenceActivationRejectsHeaderDomainSignatures(t *testing.T) {
	t.Parallel()
	activation, _, _ := validatorEvidenceActivationFixture(t)
	header, vpkKey, hotkeyKey := validatorEvidenceFixture(t)
	vpkSignature, hotkeySignature := validatorEvidenceSign(t, header, vpkKey, hotkeyKey)
	if activation.VerifyVPK(vpkSignature) || activation.VerifyHotkey(hotkeySignature) {
		t.Fatal("later header consent crossed the earlier activation signature domain")
	}
	vpkSignature, hotkeySignature = signValidatorEvidenceActivationTest(t, activation, vpkKey, hotkeyKey)
	if header.VerifyVPK(vpkSignature) || header.VerifyHotkey(hotkeySignature) {
		t.Fatal("activation consent crossed the later header signature domain")
	}
}

// Wrong seed/public halves are refused without mutating the key or record.
func TestValidatorEvidenceActivationChecksPrivateKeyConsistency(t *testing.T) {
	t.Parallel()
	activation, key, _ := validatorEvidenceActivationFixture(t)
	wrongSeed, wrongPublic := bytes.Clone(key), bytes.Clone(key)
	wrongSeed[0] ^= 1
	wrongPublic[63] ^= 1
	otherKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize))
	for _, candidate := range []ed25519.PrivateKey{nil, key[:31], key[:63], append(bytes.Clone(key), 0), wrongSeed, wrongPublic, otherKey} {
		before := bytes.Clone(candidate)
		if signature, err := activation.SignVPK(candidate); err == nil || signature != nil || !bytes.Equal(before, candidate) {
			t.Fatalf("invalid activation key accepted or modified: %v", err)
		}
	}
	if signature, err := activation.SignVPK(key); err != nil || !activation.VerifyVPK(signature) {
		t.Fatalf("correct activation key refused: %v", err)
	}
}

// Empty migration is exactly sequence one with a zero root, never an implied
// reset of a supplied nonempty prefix. Snapshot/identity absence also refuses.
func TestValidatorEvidenceActivationRejectsIncompleteMigrationAndIdentity(t *testing.T) {
	t.Parallel()
	activation, key, _ := validatorEvidenceActivationFixture(t)
	for _, edit := range []func(*ValidatorEvidenceActivation){
		func(a *ValidatorEvidenceActivation) { a.Domain.ChainID = 0 },
		func(a *ValidatorEvidenceActivation) { a.Domain.GenesisHash = [32]byte{} },
		func(a *ValidatorEvidenceActivation) { a.Domain.Netuid = 0 },
		func(a *ValidatorEvidenceActivation) { a.Domain.Coordinator = [20]byte{} },
		func(a *ValidatorEvidenceActivation) { a.Domain.SettlementVault = [20]byte{} },
		func(a *ValidatorEvidenceActivation) { a.Domain.SettlementVault = a.Domain.Coordinator },
		func(a *ValidatorEvidenceActivation) { a.Domain.DeploymentIDHash = [32]byte{} },
		func(a *ValidatorEvidenceActivation) { a.Domain.PolicyHash = [32]byte{} },
		func(a *ValidatorEvidenceActivation) { a.Hotkey = [32]byte{} },
		func(a *ValidatorEvidenceActivation) { a.VPK = [32]byte{} },
		func(a *ValidatorEvidenceActivation) { a.NoID = 0 },
		func(a *ValidatorEvidenceActivation) { a.FirstSequence = 0 },
		func(a *ValidatorEvidenceActivation) { a.FirstSequence = math.MaxUint64 },
		func(a *ValidatorEvidenceActivation) { a.FirstSequence = 1 },
		func(a *ValidatorEvidenceActivation) { a.PriorRoot = [32]byte{} },
		func(a *ValidatorEvidenceActivation) { a.NativeBlock = 0 },
		func(a *ValidatorEvidenceActivation) { a.NativeHash = [32]byte{} },
		func(a *ValidatorEvidenceActivation) { a.EVMBlock = 0 },
		func(a *ValidatorEvidenceActivation) { a.EVMHash = [32]byte{} },
	} {
		changed := activation
		edit(&changed)
		if err := changed.Validate(); err == nil {
			t.Fatal("incomplete activation shape was accepted")
		}
		if data, err := changed.Payload(); err == nil || data != nil {
			t.Fatal("invalid activation emitted a payload")
		}
		if digest, err := changed.Digest(); err == nil || digest != ([32]byte{}) {
			t.Fatal("invalid activation emitted a digest")
		}
		if domain, err := changed.EvidenceDomain(); err == nil || domain != (ValidatorEvidenceDomain{}) {
			t.Fatal("invalid activation emitted a later evidence domain")
		}
		if signature, err := changed.SignVPK(key); err == nil || signature != nil {
			t.Fatal("invalid activation emitted VPK consent")
		}
	}
}

// Full-width unsigned values survive transport; snapshot heights from distinct
// chains are not compared numerically or truncated to signed machine integers.
func TestValidatorEvidenceActivationEpochZeroAndFullWidthRoundTrip(t *testing.T) {
	t.Parallel()
	activation, _, _ := validatorEvidenceActivationFixture(t)
	activation.Domain.ChainID, activation.Domain.Netuid = math.MaxUint64, math.MaxUint16
	activation.NoID, activation.NativeBlock, activation.EVMBlock = math.MaxUint64, math.MaxUint64, 1
	activation.FirstSequence = math.MaxUint64 - 1
	for _, epoch := range []uint64{0, math.MaxUint64} {
		activation.Domain.Epoch = epoch
		if err := activation.Validate(); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(activation)
		if err != nil {
			t.Fatal(err)
		}
		var decoded ValidatorEvidenceActivation
		if err := json.Unmarshal(raw, &decoded); err != nil || decoded != activation {
			t.Fatalf("activation transport truncated exact integers: %v", err)
		}
		if payload, err := decoded.Payload(); err != nil || len(payload) != ValidatorEvidenceActivationPayloadSize {
			t.Fatalf("activation full-width payload differs: %v", err)
		}
	}
	activation.FirstSequence, activation.PriorRoot = 1, [32]byte{}
	if err := activation.Validate(); err != nil {
		t.Fatalf("explicit empty bootstrap refused: %v", err)
	}
}

// Payload buffers and derived domains are independent values, not mutation
// authority over the earlier record or a cached signing result.
func TestValidatorEvidenceActivationOwnsPayloadAndRechecksCalls(t *testing.T) {
	t.Parallel()
	activation, _, _ := validatorEvidenceActivationFixture(t)
	original := activation
	before, err := activation.Payload()
	if err != nil {
		t.Fatal(err)
	}
	changed, err := activation.Payload()
	if err != nil {
		t.Fatal(err)
	}
	for index := range changed {
		changed[index] ^= 0xff
	}
	after, err := activation.Payload()
	if err != nil || !bytes.Equal(before, after) || activation != original {
		t.Fatal("activation payload mutation reached another call or its record")
	}
	domain, err := activation.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	domain.ActivationHash[0] ^= 1
	again, err := activation.EvidenceDomain()
	if err != nil || again.ActivationHash == domain.ActivationHash || activation != original {
		t.Fatal("derived activation domain aliases caller-owned state")
	}
	activation.NativeHash[0] ^= 1
	updated, err := activation.EvidenceDomain()
	if err != nil || updated.ActivationHash == again.ActivationHash {
		t.Fatal("changed activation reused an earlier digest")
	}
}

// Later evidence references the earlier unsigned record without a cycle or an
// operator payout root. Both real signature domains remain independently valid.
func TestValidatorEvidenceActivationJoinsLaterHeaderWithoutSelfReference(t *testing.T) {
	t.Parallel()
	activation, activationKey, activationHotkey := validatorEvidenceActivationFixture(t)
	vpkSignature, hotkeySignature := signValidatorEvidenceActivationTest(t, activation, activationKey, activationHotkey)
	if err := activation.Verify(activation, vpkSignature, hotkeySignature); err != nil {
		t.Fatal(err)
	}
	domain, err := activation.EvidenceDomain()
	if err != nil || domain.Validate() != nil {
		t.Fatalf("activation did not produce a complete later domain: %v", err)
	}
	header, key, hotkey := validatorEvidenceFixture(t)
	header.Domain = domain
	vpkSignature, hotkeySignature = validatorEvidenceSign(t, header, key, hotkey)
	if err := header.Verify(domain, validatorEvidenceWindow(header), vpkSignature, hotkeySignature); err != nil {
		t.Fatalf("later header does not bind the earlier activation: %v", err)
	}
	priorDigest, _ := activation.Digest()
	header.CensusHash[0] ^= 1
	header.PayloadHash[0] ^= 1
	if got, _ := activation.Digest(); got != priorDigest {
		t.Fatal("later evidence changed the earlier activation digest")
	}
	activation.NativeBlock++
	changedDomain, err := activation.EvidenceDomain()
	if err != nil || changedDomain.ActivationHash == domain.ActivationHash {
		t.Fatal("earlier activation mutation retained its old anchor")
	}
	header.CensusHash[0] ^= 1
	header.PayloadHash[0] ^= 1
	if err := header.Verify(changedDomain, validatorEvidenceWindow(header), vpkSignature, hotkeySignature); err == nil {
		t.Fatal("later header accepted a different independently expected activation")
	}
}
