// Real dual-key signatures and independent ABI words verify the boundary
// between protocol consent and deployable contract calldata.
package stabi

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/vedhavyas/go-subkey/v2"
	"github.com/vedhavyas/go-subkey/v2/sr25519"
)

// Independent native/EVM heights deliberately have opposite numerical order.
func evidenceBindingFixture(t *testing.T) (protocol.ValidatorEvidenceActivation, protocol.ValidatorEvidenceHeader, protocol.ValidatorEvidenceWindow, ed25519.PrivateKey, subkey.KeyPair) {
	t.Helper()
	vpkKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x41}, 32))
	hotkeyKey, err := (sr25519.Scheme{}).FromSeed(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	activation := protocol.ValidatorEvidenceActivation{
		Domain: protocol.ValidatorEvidenceActivationDomain{
			ChainID: 945, GenesisHash: [32]byte{0x11}, Netuid: 521,
			Coordinator: [20]byte{0x12}, SettlementVault: [20]byte{0x13},
			DeploymentIDHash: [32]byte{0x14}, PolicyHash: [32]byte{0x15}, Epoch: 7,
		},
		NoID: 2, FirstSequence: 5, PriorRoot: [32]byte{0x16},
		NativeBlock: 9000, NativeHash: [32]byte{0x17}, EVMBlock: 1000, EVMHash: [32]byte{0x18},
	}
	copy(activation.VPK[:], vpkKey.Public().(ed25519.PublicKey))
	copy(activation.Hotkey[:], hotkeyKey.Public())
	domain, err := activation.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	header := protocol.ValidatorEvidenceHeader{
		Domain: domain, Hotkey: activation.Hotkey, NoID: activation.NoID,
		Epoch: 7, Kind: protocol.ValidatorEvidenceClosedCensus, VPK: activation.VPK,
		BoundaryBlock: 1099, BoundaryHash: [32]byte{0x19}, CensusHash: [32]byte{0x1a},
		PayloadHash: [32]byte{0x1b}, PayloadBytes: 12345,
	}
	window := protocol.ValidatorEvidenceWindow{Epoch: 7, StartBlock: 1000, EndBlock: 1100, FinalizedBlock: 1200}
	return activation, header, window, vpkKey, hotkeyKey
}

// Every field is independently ABI-padded directly from the protocol values.
func evidenceBindingWords(t *testing.T, fields ...any) []byte {
	t.Helper()
	var result []byte
	for _, field := range fields {
		var word [32]byte
		switch value := field.(type) {
		case [32]byte:
			word = value
		case [20]byte:
			copy(word[12:], value[:])
		case uint64:
			binary.BigEndian.PutUint64(word[24:], value)
		case uint16:
			binary.BigEndian.PutUint16(word[30:], value)
		case uint8:
			word[31] = value
		default:
			t.Fatalf("unexpected ABI fixture word type %T", field)
		}
		result = append(result, word[:]...)
	}
	return result
}

// Signatures are detached values and may not be omitted, reordered or aliased.
func evidenceBindingSignatures(t *testing.T, digest [32]byte, vpkKey ed25519.PrivateKey, hotkeyKey subkey.KeyPair) ([]byte, []byte) {
	t.Helper()
	hotkeySignature, err := hotkeyKey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return ed25519.Sign(vpkKey, digest[:]), hotkeySignature
}

// A compiler-generated tuple round-trip retains all activation fields.
func TestValidatorEvidenceActivationTupleRoundTrip(t *testing.T) {
	activation, _, _, _, _ := evidenceBindingFixture(t)
	if got := ValidatorEvidenceActivationRecordFromProtocol(activation).ProtocolRecord(); got != activation {
		t.Fatalf("activation tuple lost signed fields: got=%+v want=%+v", got, activation)
	}
}

// Both kinds retain all signed fields, particularly nonzero audit coordinates.
func TestValidatorEvidenceHeaderTupleRoundTrip(t *testing.T) {
	_, header, _, _, _ := evidenceBindingFixture(t)
	for _, kind := range []uint8{protocol.ValidatorEvidenceClosedCensus, protocol.ValidatorEvidenceDepositAudit} {
		header.Kind = kind
		if kind == protocol.ValidatorEvidenceDepositAudit {
			header.Subject = protocol.ValidatorEvidenceSubject{ObservationEpoch: 8, NativeEpoch: 17}
		}
		if got := ValidatorEvidenceHeaderFromProtocol(header).ProtocolHeader(); got != header {
			t.Fatalf("kind %d tuple lost signed fields: got=%+v want=%+v", kind, got, header)
		}
	}
}

// Pin all 17 static activation words and the two detached signature offsets.
func TestValidatorEvidenceActivationCalldataMatchesIndependentWords(t *testing.T) {
	record, _, _, vpkKey, hotkeyKey := evidenceBindingFixture(t)
	digest, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	vpkSignature, hotkeySignature := evidenceBindingSignatures(t, digest, vpkKey, hotkeyKey)
	calldata, err := PackValidatorEvidenceActivation(record, record, vpkSignature, hotkeySignature)
	if err != nil {
		t.Fatal(err)
	}
	want := evidenceBindingWords(t, record.Domain.ChainID, record.Domain.GenesisHash, record.Domain.Netuid,
		record.Domain.Coordinator, record.Domain.SettlementVault, record.Domain.DeploymentIDHash,
		record.Domain.PolicyHash, record.Domain.Epoch, record.Hotkey, record.NoID, record.VPK,
		record.FirstSequence, record.PriorRoot, record.NativeBlock, record.NativeHash, record.EVMBlock, record.EVMHash,
		uint64(19*32), uint64(22*32), uint64(64))
	want = append(want, vpkSignature...)
	want = append(want, evidenceBindingWords(t, uint64(64))...)
	want = append(want, hotkeySignature...)
	contractABI, err := STValidatorEvidenceMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	if len(calldata) != 804 || !bytes.Equal(calldata[:4], contractABI.Methods["publishActivation"].ID) || !bytes.Equal(calldata[4:], want) {
		t.Fatal("activation calldata differs from independently padded words and signatures")
	}
	retained := bytes.Clone(calldata)
	vpkSignature[0] ^= 1
	hotkeySignature[0] ^= 1
	if !bytes.Equal(calldata, retained) {
		t.Fatal("activation calldata borrowed signature bytes")
	}
}

// An audit packs both subject coordinates; a subject hash alone is not its ABI.
func TestValidatorEvidenceCommitmentCalldataMatchesIndependentWords(t *testing.T) {
	_, header, window, vpkKey, hotkeyKey := evidenceBindingFixture(t)
	header.Kind = protocol.ValidatorEvidenceDepositAudit
	header.Subject = protocol.ValidatorEvidenceSubject{ObservationEpoch: 8, NativeEpoch: 17}
	header.BoundaryBlock = 1199
	window.Subject = header.Subject
	digest, err := header.Digest()
	if err != nil {
		t.Fatal(err)
	}
	vpkSignature, hotkeySignature := evidenceBindingSignatures(t, digest, vpkKey, hotkeyKey)
	calldata, err := PackValidatorEvidenceCommitment(header.Domain, window, header, vpkSignature, hotkeySignature)
	if err != nil {
		t.Fatal(err)
	}
	want := evidenceBindingWords(t, header.Domain.ChainID, header.Domain.GenesisHash, header.Domain.Netuid,
		header.Domain.Coordinator, header.Domain.SettlementVault, header.Domain.DeploymentIDHash,
		header.Domain.PolicyHash, header.Domain.ActivationEpoch, header.Domain.ActivationHash,
		header.Hotkey, header.NoID, header.Epoch, header.Kind, header.Subject.ObservationEpoch, header.Subject.NativeEpoch,
		header.VPK, header.BoundaryBlock, header.BoundaryHash, header.CensusHash, header.PayloadHash, header.PayloadBytes,
		uint64(23*32), uint64(26*32), uint64(64))
	want = append(want, vpkSignature...)
	want = append(want, evidenceBindingWords(t, uint64(64))...)
	want = append(want, hotkeySignature...)
	contractABI, err := STValidatorEvidenceMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	if len(calldata) != 932 || !bytes.Equal(calldata[:4], contractABI.Methods["commitEvidence"].ID) || !bytes.Equal(calldata[4:], want) {
		t.Fatal("commitment calldata differs from independently padded words and signatures")
	}
}

// Valid signatures from both keys cannot substitute a candidate-selected prefix
// for the runtime's independently authenticated activation.
func TestValidatorEvidenceActivationCalldataRejectsCandidateAuthority(t *testing.T) {
	expected, _, _, vpkKey, hotkeyKey := evidenceBindingFixture(t)
	for _, mutation := range []struct {
		name string
		edit func(*protocol.ValidatorEvidenceActivation)
	}{
		{name: "coordinator", edit: func(record *protocol.ValidatorEvidenceActivation) { record.Domain.Coordinator[19] ^= 1 }},
		{name: "netuid", edit: func(record *protocol.ValidatorEvidenceActivation) { record.Domain.Netuid++ }},
		{name: "operator", edit: func(record *protocol.ValidatorEvidenceActivation) { record.NoID++ }},
		{name: "prior-root", edit: func(record *protocol.ValidatorEvidenceActivation) { record.PriorRoot[0]++ }},
		{name: "native-hash", edit: func(record *protocol.ValidatorEvidenceActivation) { record.NativeHash[0]++ }},
		{name: "EVM-hash", edit: func(record *protocol.ValidatorEvidenceActivation) { record.EVMHash[0]++ }},
	} {
		record := expected
		mutation.edit(&record)
		if err := record.Validate(); err != nil || record == expected {
			t.Fatalf("%s mutation must be a different well-shaped activation: %v", mutation.name, err)
		}
		digest, err := record.Digest()
		if err != nil {
			t.Fatalf("%s candidate digest: %v", mutation.name, err)
		}
		vpkSignature, hotkeySignature := evidenceBindingSignatures(t, digest, vpkKey, hotkeyKey)
		if err := record.Verify(record, vpkSignature, hotkeySignature); err != nil {
			t.Fatalf("%s candidate lacks genuine dual-key consent: %v", mutation.name, err)
		}
		if calldata, err := PackValidatorEvidenceActivation(expected, record, vpkSignature, hotkeySignature); err == nil || calldata != nil {
			t.Fatalf("%s candidate authority emitted activation calldata", mutation.name)
		}
	}
}

// Either missing/bad consent refuses all calldata, in both publication paths.
func TestValidatorEvidenceCalldataRequiresBothKeyConsents(t *testing.T) {
	record, header, window, vpkKey, hotkeyKey := evidenceBindingFixture(t)
	activationDigest, _ := record.Digest()
	headerDigest, _ := header.Digest()
	for _, mutation := range []string{"missing-vpk", "missing-hotkey", "wrong-vpk", "wrong-hotkey"} {
		for _, activation := range []bool{true, false} {
			digest := headerDigest
			if activation {
				digest = activationDigest
			}
			vpkSignature, hotkeySignature := evidenceBindingSignatures(t, digest, vpkKey, hotkeyKey)
			switch mutation {
			case "missing-vpk":
				vpkSignature = nil
			case "missing-hotkey":
				hotkeySignature = nil
			case "wrong-vpk":
				vpkSignature[0] ^= 1
			case "wrong-hotkey":
				hotkeySignature[0] ^= 1
			}
			var calldata []byte
			var err error
			if activation {
				calldata, err = PackValidatorEvidenceActivation(record, record, vpkSignature, hotkeySignature)
			} else {
				calldata, err = PackValidatorEvidenceCommitment(header.Domain, window, header, vpkSignature, hotkeySignature)
			}
			if err == nil || calldata != nil {
				t.Fatalf("activation=%t %s emitted unauthenticated calldata", activation, mutation)
			}
		}
	}
}

// A validly signed later header still cannot alter the trusted domain/window.
func TestValidatorEvidenceCommitmentCalldataRejectsDomainAndWindowDrift(t *testing.T) {
	_, original, window, vpkKey, hotkeyKey := evidenceBindingFixture(t)
	for _, edit := range []func(*protocol.ValidatorEvidenceHeader){
		func(header *protocol.ValidatorEvidenceHeader) { header.Domain.ActivationHash[0]++ },
		func(header *protocol.ValidatorEvidenceHeader) { header.Domain.PolicyHash[0]++ },
		func(header *protocol.ValidatorEvidenceHeader) { header.Domain.GenesisHash[0]++ },
		func(header *protocol.ValidatorEvidenceHeader) { header.Epoch++ },
		func(header *protocol.ValidatorEvidenceHeader) { header.BoundaryBlock-- },
	} {
		header := original
		edit(&header)
		digest, err := header.Digest()
		if err != nil {
			t.Fatal(err)
		}
		vpkSignature, hotkeySignature := evidenceBindingSignatures(t, digest, vpkKey, hotkeyKey)
		if calldata, err := PackValidatorEvidenceCommitment(original.Domain, window, header, vpkSignature, hotkeySignature); err == nil || calldata != nil {
			t.Fatal("valid signatures bypassed independent publication domain/window")
		}
	}
}
