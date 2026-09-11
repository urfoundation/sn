package crv4

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/registry"
	"github.com/centrifuge/go-substrate-rpc-client/v4/registry/parser"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
)

// Real sr25519 signs exact SCALE fixture bytes. No verifier outcome, metadata
// identity or finalized-event result is supplied by a test callback.
func sourcePreparedTest(t testing.TB) (*PreparedSubmission, *Keypair) {
	t.Helper()
	key, err := KeypairFromSeed([32]byte{71})
	if err != nil {
		t.Fatal(err)
	}
	public := key.PublicKey()
	payload, err := (&Payload{Hotkey: public, Uids: []uint16{1, 7}, Values: []uint16{65535, 32768}, VersionKey: 2}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	cipher := bytes.Repeat([]byte{0xc1, 0x04, 0x08, 0x7a}, 96)
	cipherHash := sha256.Sum256(cipher)
	prepared := &PreparedSubmission{Schema: PreparedSourceSubmissionSchema, Netuid: 521, HotkeyHex: codec.HexEncodeToString(public[:]), VersionKey: 2, CommitRevealVersion: 4, AccountNonce: 129, PreparedAtBlock: 500, PreparedAtBlockHash: types.Hash{5}.Hex(), SubnetEpoch: 23, RevealRound: 2200, RevealBlock: 900, UIDs: []uint16{1, 7}, Values: []uint16{65535, 32768}, PayloadHex: codec.HexEncodeToString(payload), CiphertextHex: codec.HexEncodeToString(cipher), CiphertextSHA256: codec.HexEncodeToString(cipherHash[:]), SourceCommitment: &PreparedSourceCommitment{Hash: types.Hash{9}.Hex(), GenesisHash: types.Hash{8}.Hex(), RuntimeSpec: 454, TransactionVersion: 1}}
	signSourcePreparedTest(t, prepared, key, nil)
	return prepared, key
}

func signSourcePreparedTest(t testing.TB, prepared *PreparedSubmission, key *Keypair, mutate func([]byte)) {
	t.Helper()
	call, fields, payload, err := preparedSourceEncoding(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(call)
		copy(payload, call)
	}
	if len(payload) > 256 {
		hash := blake2b.Sum256(payload)
		payload = hash[:]
	}
	signature, err := key.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	public := key.PublicKey()
	body := append([]byte{0x84, 0}, public[:]...)
	body = append(body, 1)
	body = append(body, signature...)
	body = append(body, fields...)
	body = append(body, call...)
	prefix, err := codec.Encode(types.NewUCompactFromUInt(uint64(len(body))))
	if err != nil {
		t.Fatal(err)
	}
	raw := append(prefix, body...)
	hash := blake2b.Sum256(raw)
	prepared.ExtrinsicHex, prepared.ExtrinsicHash = codec.HexEncodeToString(raw), types.Hash(hash).Hex()
}

func TestSourceCommitmentExactSignedBatchAndCiphertext(t *testing.T) {
	prepared, _ := sourcePreparedTest(t)
	raw, err := prepared.Validate()
	if err != nil || len(raw) == 0 {
		t.Fatalf("actual source signature failed: %v", err)
	}
	call, _, _, err := preparedSourceEncoding(prepared)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{11, 2, 8, 18, 0, 9, 2, 4, 131}
	if !bytes.Equal(call[:len(want)], want) || !bytes.Equal(call[41:43], []byte{7, 113}) {
		t.Fatalf("wrong exact atomic calls: %x", call[:43])
	}
}

func TestSourceCommitmentSignedNonAtomicBatchIsRejected(t *testing.T) {
	for _, index := range []int{0, 1, 2, 3, 4, 41, 42} {
		prepared, key := sourcePreparedTest(t)
		signSourcePreparedTest(t, prepared, key, func(call []byte) { call[index] ^= 1 })
		if _, err := prepared.Validate(); err == nil {
			t.Fatalf("real signature over wrong call structure at %d was accepted", index)
		}
	}
}

func TestSourceCommitmentEveryDurableIdentityIsBound(t *testing.T) {
	mutations := []func(*PreparedSubmission){
		func(value *PreparedSubmission) { value.SourceCommitment.Hash = types.Hash{7}.Hex() },
		func(value *PreparedSubmission) { value.SourceCommitment.GenesisHash = types.Hash{6}.Hex() },
		func(value *PreparedSubmission) { value.SourceCommitment.TransactionVersion++ },
		func(value *PreparedSubmission) { value.SourceCommitment.RuntimeSpec-- },
		func(value *PreparedSubmission) { value.AccountNonce++ },
		func(value *PreparedSubmission) { value.Netuid++ },
		func(value *PreparedSubmission) { value.RevealRound++ },
		func(value *PreparedSubmission) { value.Schema = PreparedSubmissionSchema },
	}
	for index, mutate := range mutations {
		prepared, _ := sourcePreparedTest(t)
		mutate(prepared)
		if _, err := prepared.Validate(); err == nil {
			t.Fatalf("mutated source identity %d was accepted", index)
		}
	}
}

func sourceEventsTest(prepared *PreparedSubmission, index uint32) []*parser.Event {
	public, _ := codec.HexDecodeString(prepared.HotkeyHex)
	cipher, _ := codec.HexDecodeString(prepared.CiphertextHex)
	hash := blake2b.Sum256(cipher)
	array := func(raw []byte) any {
		values := make([]any, len(raw))
		for index, value := range raw {
			values[index] = types.U8(value)
		}
		return registry.DecodedFields{&registry.DecodedField{Value: values}}
	}
	field := func(value any) *registry.DecodedField { return &registry.DecodedField{Value: value} }
	names := []string{"Commitments.Commitment", "Utility.ItemCompleted", "SubtensorModule.TimelockedWeightsCommitted", "Utility.ItemCompleted", "Utility.BatchCompleted", "System.ExtrinsicSuccess"}
	result := make([]*parser.Event, len(names))
	for i, name := range names {
		result[i] = &parser.Event{Name: name, Phase: &types.Phase{IsApplyExtrinsic: true, AsApplyExtrinsic: index}}
	}
	result[0].Fields = registry.DecodedFields{field(types.U16(prepared.Netuid)), field(array(public))}
	result[2].Fields = registry.DecodedFields{field(array(public)), field(types.U16(prepared.Netuid)), field(array(hash[:])), field(types.U64(prepared.RevealRound))}
	return result
}

func TestSourceCommitmentEventsRequireExactCipherIdentityAndOrder(t *testing.T) {
	prepared, _ := sourcePreparedTest(t)
	if err := verifySourceCommitmentEvents(prepared, 3, sourceEventsTest(prepared, 3)); err != nil {
		t.Fatal(err)
	}
	mutations := []func([]*parser.Event) []*parser.Event{
		func(events []*parser.Event) []*parser.Event { return events[1:] },
		func(events []*parser.Event) []*parser.Event {
			events[1], events[3] = events[3], events[1]
			events[2], events[0] = events[0], events[2]
			return events
		},
		func(events []*parser.Event) []*parser.Event { events[0].Phase.AsApplyExtrinsic++; return events },
		func(events []*parser.Event) []*parser.Event {
			events[2].Fields[3].Value = types.U64(prepared.RevealRound + 1)
			return events
		},
		func(events []*parser.Event) []*parser.Event {
			events[4].Name = "Utility.BatchInterrupted"
			return events
		},
		func(events []*parser.Event) []*parser.Event {
			events[0].Fields[0].Value = types.U16(prepared.Netuid + 1)
			return events
		},
		func(events []*parser.Event) []*parser.Event {
			events[2].Fields[1].Value = types.U16(prepared.Netuid + 1)
			return events
		},
	}
	for index, mutate := range mutations {
		if err := verifySourceCommitmentEvents(prepared, 3, mutate(sourceEventsTest(prepared, 3))); err == nil {
			t.Fatalf("incomplete/wrong source operation events %d were accepted", index)
		}
	}
}
