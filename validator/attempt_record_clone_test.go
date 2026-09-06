//go:build linux || darwin

package validator

// Genuine M8 lifecycle and disk/JSONL replay controls accompany pure copy
// contracts. Work observations count actual JSON serialization, not elapsed time.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/urnetwork/connect"
)

// This conditional own-root marker follows all later resource cleanups.
func attemptRecordCloneTestRoot(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Logf("ATTEMPT-CLONE-v1 PASS %s", t.Name())
		}
	})
}

// The original implementation is an independent compatibility oracle.
func attemptRecordCloneTestJSON(t *testing.T, record AttemptRecord) AttemptRecord {
	t.Helper()
	raw, err := json.Marshal(&record)
	if err != nil {
		t.Fatal(err)
	}
	var result AttemptRecord
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// Structural copy fixtures are deliberately not authenticated trail evidence.
func attemptRecordCloneTestValue() AttemptRecord {
	return AttemptRecord{
		Schema: "schema", Identity: AttemptLedgerIdentity{
			DeploymentID: "deployment", ChainID: 1, GenesisHash: "genesis", Netuid: 2,
			ValidatorID: 3, ValidatorUID: 4, NoID: 5, ValidatorVPK: "identity-vpk",
		},
		Sequence: 6, PreviousHash: "previous",
		Boundary: AttemptBoundary{SettlementEpoch: 7, EVMBlock: 8, EVMBlockHash: "block"},
		TrailID: connect.Id{0: 9}, ServerNonce: []byte{10, 11}, VPK: []byte{12, 13}, M: 8,
		Assignments: []AttemptAssignment{{
			Trail: []connect.Id{{0: 14}, {0: 15}}, NextHop: connect.Id{0: 16},
			ServerKeyID: 17, AssignMessage: []byte{18, 19}, AssignSignature: []byte{20, 21},
			Confirmed: true, HasLatency: true, LatencyBucket: 2,
			Binding: AttemptBinding{ClientID: connect.Id{0: 22}, Active: true,
				FleetID: "fleet", Hotkey: "hotkey", Generation: 23, UIDFound: true, UID: 24},
		}},
		Disposition: "disposition",
		Proof: &ProofRecord{Version: 1, Epoch: 25, TrailId: connect.Id{0: 26},
			ServerNonce: []byte{27, 28}, Vpk: []byte{29, 30}, M: 8,
			Hops: []connect.VerifyProofHop{{ClientId: connect.Id{0: 31}, TimeMs: 32, EgressIpHash: [32]byte{0: 33}}},
			ServerKeyId: 34, FinalSig: []byte{35, 36}, VerifierSig: []byte{37, 38},
			FinalDigest: []byte{39, 40}, VpkSig: []byte{41, 42}, Coverage: 7,
			PathId: []byte{43, 44}, CompleteTimeMs: 45},
		RecordHash: "record", Signature: []byte{46, 47},
	}
}

// Pointer targets and all scalar fields are mutated independently.
type attemptRecordCloneTestChange struct {
	name string
	edit func(*AttemptRecord)
}

// Future structure changes are pinned separately by the full field census.
func attemptRecordCloneTestChanges() []attemptRecordCloneTestChange {
	return []attemptRecordCloneTestChange{
		{name: "schema", edit: func(r *AttemptRecord) { r.Schema += "x" }},
		{name: "identity.deployment", edit: func(r *AttemptRecord) { r.Identity.DeploymentID += "x" }},
		{name: "identity.chain", edit: func(r *AttemptRecord) { r.Identity.ChainID++ }},
		{name: "identity.genesis", edit: func(r *AttemptRecord) { r.Identity.GenesisHash += "x" }},
		{name: "identity.netuid", edit: func(r *AttemptRecord) { r.Identity.Netuid++ }},
		{name: "identity.validator", edit: func(r *AttemptRecord) { r.Identity.ValidatorID++ }},
		{name: "identity.validator_uid", edit: func(r *AttemptRecord) { r.Identity.ValidatorUID++ }},
		{name: "identity.no", edit: func(r *AttemptRecord) { r.Identity.NoID++ }},
		{name: "identity.vpk", edit: func(r *AttemptRecord) { r.Identity.ValidatorVPK += "x" }},
		{name: "sequence", edit: func(r *AttemptRecord) { r.Sequence++ }},
		{name: "previous", edit: func(r *AttemptRecord) { r.PreviousHash += "x" }},
		{name: "boundary.epoch", edit: func(r *AttemptRecord) { r.Boundary.SettlementEpoch++ }},
		{name: "boundary.block", edit: func(r *AttemptRecord) { r.Boundary.EVMBlock++ }},
		{name: "boundary.hash", edit: func(r *AttemptRecord) { r.Boundary.EVMBlockHash += "x" }},
		{name: "trail_id", edit: func(r *AttemptRecord) { r.TrailID[0] ^= 1 }},
		{name: "server_nonce", edit: func(r *AttemptRecord) { r.ServerNonce[0] ^= 1 }},
		{name: "vpk", edit: func(r *AttemptRecord) { r.VPK[0] ^= 1 }},
		{name: "depth", edit: func(r *AttemptRecord) { r.M++ }},
		{name: "assignments", edit: func(r *AttemptRecord) { r.Assignments = nil }},
		{name: "assignment.trail", edit: func(r *AttemptRecord) { r.Assignments[0].Trail[0][0] ^= 1 }},
		{name: "assignment.next", edit: func(r *AttemptRecord) { r.Assignments[0].NextHop[0] ^= 1 }},
		{name: "assignment.key", edit: func(r *AttemptRecord) { r.Assignments[0].ServerKeyID ^= 1 }},
		{name: "assignment.message", edit: func(r *AttemptRecord) { r.Assignments[0].AssignMessage[0] ^= 1 }},
		{name: "assignment.signature", edit: func(r *AttemptRecord) { r.Assignments[0].AssignSignature[0] ^= 1 }},
		{name: "assignment.confirmed", edit: func(r *AttemptRecord) { r.Assignments[0].Confirmed = false }},
		{name: "assignment.latency", edit: func(r *AttemptRecord) { r.Assignments[0].HasLatency = false }},
		{name: "assignment.bucket", edit: func(r *AttemptRecord) { r.Assignments[0].LatencyBucket++ }},
		{name: "binding.client", edit: func(r *AttemptRecord) { r.Assignments[0].Binding.ClientID[0] ^= 1 }},
		{name: "binding.active", edit: func(r *AttemptRecord) { r.Assignments[0].Binding.Active = false }},
		{name: "binding.fleet", edit: func(r *AttemptRecord) { r.Assignments[0].Binding.FleetID += "x" }},
		{name: "binding.hotkey", edit: func(r *AttemptRecord) { r.Assignments[0].Binding.Hotkey += "x" }},
		{name: "binding.generation", edit: func(r *AttemptRecord) { r.Assignments[0].Binding.Generation++ }},
		{name: "binding.uid_found", edit: func(r *AttemptRecord) { r.Assignments[0].Binding.UIDFound = false }},
		{name: "binding.uid", edit: func(r *AttemptRecord) { r.Assignments[0].Binding.UID++ }},
		{name: "disposition", edit: func(r *AttemptRecord) { r.Disposition += "x" }},
		{name: "proof.pointer", edit: func(r *AttemptRecord) { r.Proof = nil }},
		{name: "proof.version", edit: func(r *AttemptRecord) { r.Proof.Version++ }},
		{name: "proof.epoch", edit: func(r *AttemptRecord) { r.Proof.Epoch++ }},
		{name: "proof.trail", edit: func(r *AttemptRecord) { r.Proof.TrailId[0] ^= 1 }},
		{name: "proof.nonce", edit: func(r *AttemptRecord) { r.Proof.ServerNonce[0] ^= 1 }},
		{name: "proof.vpk", edit: func(r *AttemptRecord) { r.Proof.Vpk[0] ^= 1 }},
		{name: "proof.depth", edit: func(r *AttemptRecord) { r.Proof.M++ }},
		{name: "proof.hops", edit: func(r *AttemptRecord) { r.Proof.Hops = nil }},
		{name: "proof.hop.client", edit: func(r *AttemptRecord) { r.Proof.Hops[0].ClientId[0] ^= 1 }},
		{name: "proof.hop.time", edit: func(r *AttemptRecord) { r.Proof.Hops[0].TimeMs++ }},
		{name: "proof.hop.egress", edit: func(r *AttemptRecord) { r.Proof.Hops[0].EgressIpHash[0] ^= 1 }},
		{name: "proof.key", edit: func(r *AttemptRecord) { r.Proof.ServerKeyId ^= 1 }},
		{name: "proof.final_sig", edit: func(r *AttemptRecord) { r.Proof.FinalSig[0] ^= 1 }},
		{name: "proof.verifier_sig", edit: func(r *AttemptRecord) { r.Proof.VerifierSig[0] ^= 1 }},
		{name: "proof.final_digest", edit: func(r *AttemptRecord) { r.Proof.FinalDigest[0] ^= 1 }},
		{name: "proof.vpk_sig", edit: func(r *AttemptRecord) { r.Proof.VpkSig[0] ^= 1 }},
		{name: "proof.coverage", edit: func(r *AttemptRecord) { r.Proof.Coverage++ }},
		{name: "proof.path", edit: func(r *AttemptRecord) { r.Proof.PathId[0] ^= 1 }},
		{name: "proof.completed", edit: func(r *AttemptRecord) { r.Proof.CompleteTimeMs++ }},
		{name: "record_hash", edit: func(r *AttemptRecord) { r.RecordHash += "x" }},
		{name: "signature", edit: func(r *AttemptRecord) { r.Signature[0] ^= 1 }},
	}
}

// Clone observation surrounds all eight genuine records; full real verification
// still authenticates every assignment, proof, record hash and cut signature.
func TestAttemptRecordCloneAvoidsJSONForRealM8Cut(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	cut, key, keys := attemptComparisonRealCut(t)
	cloned := *cut
	cloned.Records = make([]AttemptRecord, len(cut.Records))
	serializations := 0
	work := attemptRecordCloneWork{serialized: func() { serializations++ }}
	for index := range cut.Records {
		var err error
		cloned.Records[index], err = work.clone(cut.Records[index])
		if err != nil || !reflect.DeepEqual(cloned.Records[index], attemptRecordCloneTestJSON(t, cut.Records[index])) {
			t.Fatalf("real M8 copy %d differs: %v", index, err)
		}
	}
	if err := VerifyAttemptLedgerCut(&cloned, key.Public().(ed25519.PublicKey), keys); err != nil {
		t.Fatal(err)
	}
	t.Logf("ATTEMPT-CLONE-v1 WORK %s records=8 serialized=%d", t.Name(), serializations)
	if serializations != 0 {
		t.Fatalf("clone-work-real-m8: serialized %d complete record copies, want 0", serializations)
	}
}

// This is a value/encoding contract, not a claim that arbitrary strings pass
// the authenticated record parser.
func TestAttemptRecordCloneValidUTF8EscapesAvoidJSON(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	for _, value := range []string{"", "ordinary", "<>&\"\\\n\t\x00", "\u2028\u2029", "κόσμος😀\ufffd"} {
		record := attemptRecordCloneTestValue()
		record.Schema, record.Identity.DeploymentID, record.Identity.GenesisHash = value, value, value
		record.Identity.ValidatorVPK, record.PreviousHash, record.Boundary.EVMBlockHash = value, value, value
		record.Disposition, record.RecordHash = value, value
		record.Assignments[0].Binding.FleetID, record.Assignments[0].Binding.Hotkey = value, value
		serializations := 0
		got, err := (attemptRecordCloneWork{serialized: func() { serializations++ }}).clone(record)
		if err != nil || !reflect.DeepEqual(got, attemptRecordCloneTestJSON(t, record)) {
			t.Fatalf("valid UTF8 copy changed: %v", err)
		}
		t.Logf("ATTEMPT-CLONE-v1 WORK %s value=%q serialized=%d", t.Name(), value, serializations)
		if serializations != 0 {
			t.Errorf("clone-work-valid-utf8: serialized %q value copy %d times, want 0", value, serializations)
		}
	}
}

// Invalid runs are replaced byte-by-byte by the original JSON roundtrip;
// coalescing invalid runs with strings.ToValidUTF8 would change old behavior.
func TestAttemptRecordCloneInvalidUTF8FallbackEveryString(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	for _, encoded := range []string{string([]byte{0xff}), string([]byte{0xff, 0xfe}), string([]byte{0xe2, 0x82}), "a"+string([]byte{0xc0, 0xaf})+"z"} {
		for field := 0; field < 12; field++ {
			record := attemptRecordCloneTestValue()
			record.Assignments = append(record.Assignments, attemptRecordCloneTestValue().Assignments[0])
			fields := []*string{&record.Schema, &record.Identity.DeploymentID, &record.Identity.GenesisHash,
				&record.Identity.ValidatorVPK, &record.PreviousHash, &record.Boundary.EVMBlockHash,
				&record.Disposition, &record.RecordHash, &record.Assignments[0].Binding.FleetID,
				&record.Assignments[0].Binding.Hotkey, &record.Assignments[1].Binding.FleetID,
				&record.Assignments[1].Binding.Hotkey}
			*fields[field] = encoded
			serializations := 0
			got, err := (attemptRecordCloneWork{serialized: func() { serializations++ }}).clone(record)
			if err != nil || serializations != 1 || !reflect.DeepEqual(got, attemptRecordCloneTestJSON(t, record)) {
				t.Fatalf("fallback field%d bytes%x changed: count%d error%v", field, encoded, serializations, err)
			}
			if utf8.ValidString(*fields[field]) || *fields[field] != encoded {
				t.Fatal("fallback altered caller-owned invalid UTF8")
			}
			if field == 0 && encoded == string([]byte{0xff, 0xfe}) && got.Schema != "\ufffd\ufffd" {
				t.Fatal("fallback coalesced adjacent invalid bytes")
			}
		}
	}
}

// All nested fields must match the independent old JSON copy, including values
// that a later verifier refuses. Cloning does not create a validation boundary.
func TestAttemptRecordCloneJSONParityEveryField(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	original := attemptRecordCloneTestValue()
	before, err := json.Marshal(&original)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range attemptRecordCloneTestChanges() {
		input := attemptRecordCloneTestJSON(t, original)
		change.edit(&input)
		after, err := json.Marshal(&input)
		if err != nil || bytes.Equal(after, before) {
			t.Fatalf("%s mutation did not exercise a distinct field: %v", change.name, err)
		}
		got, err := cloneAttemptRecord(input)
		if err != nil || !reflect.DeepEqual(got, attemptRecordCloneTestJSON(t, input)) {
			t.Fatalf("%s JSON clone parity changed: %v", change.name, err)
		}
	}
}

// Mutations in either direction must not cross the outer slice, inner slices,
// proof pointer, proof hops or byte owners, including the compatibility branch.
func TestAttemptRecordCloneOwnsEveryMutableField(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	for _, invalid := range []bool{false, true} {
		for _, change := range attemptRecordCloneTestChanges() {
			for _, mutateInput := range []bool{false, true} {
				input := attemptRecordCloneTestValue()
				if invalid {
					input.Schema = string([]byte{0xff, 0xfe})
				}
				inputBefore, err := json.Marshal(&input)
				if err != nil {
					t.Fatal(err)
				}
				got, err := cloneAttemptRecord(input)
				if err != nil {
					t.Fatal(err)
				}
				gotBefore := attemptRecordCloneTestJSON(t, got)
				if mutateInput {
					change.edit(&input)
					if !reflect.DeepEqual(got, gotBefore) {
						t.Fatalf("%s caller mutation crossed owned copy, fallback=%t", change.name, invalid)
					}
				} else {
					change.edit(&got)
					inputAfter, err := json.Marshal(&input)
					if err != nil || !bytes.Equal(inputBefore, inputAfter) {
						t.Fatalf("%s output mutation crossed caller owner, fallback=%t: %v", change.name, invalid, err)
					}
				}
			}
		}
	}
	input := attemptRecordCloneTestValue()
	input.Assignments = append(input.Assignments, input.Assignments[0])
	input.VPK, input.Proof.Vpk = input.ServerNonce, input.ServerNonce
	got, err := cloneAttemptRecord(input)
	if err != nil {
		t.Fatal(err)
	}
	got.ServerNonce[0] ^= 1
	got.Assignments[0].Trail[0][0] ^= 1
	if got.VPK[0] != 10 || got.Proof.Vpk[0] != 10 || input.ServerNonce[0] != 10 ||
		got.Assignments[1].Trail[0][0] != 14 || input.Assignments[0].Trail[0][0] != 14 {
		t.Fatal("clone retained aliases between independently decoded JSON owners")
	}
}

// Every slice and optional pointer keeps its nil/non-nil distinction. Empty
// byte strings, arrays and a present empty proof must not collapse to null.
func TestAttemptRecordCloneNilAndEmptyParity(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	for _, empty := range []bool{false, true} {
		for _, proofPresent := range []bool{false, true} {
			input := AttemptRecord{}
			if empty {
				input.ServerNonce, input.VPK, input.Signature = []byte{}, []byte{}, []byte{}
				input.Assignments = []AttemptAssignment{}
			}
			if proofPresent {
				input.Proof = &ProofRecord{}
				if empty {
					input.Proof.ServerNonce, input.Proof.Vpk = []byte{}, []byte{}
					input.Proof.Hops = []connect.VerifyProofHop{}
					input.Proof.FinalSig, input.Proof.VerifierSig = []byte{}, []byte{}
					input.Proof.FinalDigest, input.Proof.VpkSig, input.Proof.PathId = []byte{}, []byte{}, []byte{}
				}
			}
			for _, assignments := range []int{0, 1} {
				if assignments != 0 {
					input.Assignments = []AttemptAssignment{{}}
					if empty {
						input.Assignments[0].Trail = []connect.Id{}
						input.Assignments[0].AssignMessage, input.Assignments[0].AssignSignature = []byte{}, []byte{}
					}
				}
				got, err := cloneAttemptRecord(input)
				if err != nil || !reflect.DeepEqual(got, input) || !reflect.DeepEqual(got, attemptRecordCloneTestJSON(t, input)) {
					t.Fatalf("nil/empty copy changed: empty=%t proof=%t assignments=%d error%v", empty, proofPresent, assignments, err)
				}
			}
		}
	}
	hiddenBytes := []byte{71}
	hiddenAssignments := []AttemptAssignment{{ServerKeyID: 72}}
	input := AttemptRecord{ServerNonce: hiddenBytes[:0], Assignments: hiddenAssignments[:0]}
	got, err := cloneAttemptRecord(input)
	if err != nil || got.ServerNonce == nil || got.Assignments == nil {
		t.Fatalf("empty-capacity ownership changed: %v", err)
	}
	got.ServerNonce = append(got.ServerNonce, 73)
	got.Assignments = append(got.Assignments, AttemptAssignment{ServerKeyID: 74})
	if hiddenBytes[0] != 71 || hiddenAssignments[0].ServerKeyID != 72 {
		t.Fatal("empty copy append overwrote hidden caller capacity")
	}
}

// No mutable field, JSON tag, custom marshaler or value-only hop may silently
// appear outside the handwritten copy and UTF8 compatibility census.
func TestAttemptRecordCloneFieldCensus(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	for _, row := range []struct {
		typeOf reflect.Type
		fields []string
	}{
		{typeOf: reflect.TypeOf(AttemptRecord{}), fields: []string{
			"Schema:string:schema", "Identity:validator.AttemptLedgerIdentity:identity", "Sequence:uint64:sequence",
			"PreviousHash:string:previous_hash", "Boundary:validator.AttemptBoundary:boundary", "TrailID:connect.Id:trail_id",
			"ServerNonce:[]uint8:server_nonce", "VPK:[]uint8:vpk", "M:int:M", "Assignments:[]validator.AttemptAssignment:assignments",
			"Disposition:string:disposition", "Proof:*validator.ProofRecord:proof,omitempty", "RecordHash:string:record_hash", "Signature:[]uint8:signature",
		}},
		{typeOf: reflect.TypeOf(AttemptLedgerIdentity{}), fields: []string{
			"DeploymentID:string:deployment_id", "ChainID:uint64:chain_id", "GenesisHash:string:genesis_hash", "Netuid:uint16:netuid",
			"ValidatorID:uint64:validator_id", "ValidatorUID:uint16:validator_uid", "NoID:uint64:no_id", "ValidatorVPK:string:validator_vpk",
		}},
		{typeOf: reflect.TypeOf(AttemptBoundary{}), fields: []string{
			"SettlementEpoch:uint64:settlement_epoch", "EVMBlock:uint64:evm_block", "EVMBlockHash:string:evm_block_hash",
		}},
		{typeOf: reflect.TypeOf(AttemptAssignment{}), fields: []string{
			"Trail:[]connect.Id:trail", "NextHop:connect.Id:next_hop", "ServerKeyID:uint8:server_key_id",
			"AssignMessage:[]uint8:assign_message", "AssignSignature:[]uint8:assign_signature",
			"Confirmed:bool:confirmed", "HasLatency:bool:has_latency", "LatencyBucket:uint8:latency_bucket", "Binding:validator.AttemptBinding:binding",
		}},
		{typeOf: reflect.TypeOf(AttemptBinding{}), fields: []string{
			"ClientID:connect.Id:client_id", "Active:bool:active", "FleetID:string:fleet_id", "Hotkey:string:hotkey",
			"Generation:uint64:generation", "UIDFound:bool:uid_found", "UID:uint16:uid",
		}},
		{typeOf: reflect.TypeOf(ProofRecord{}), fields: []string{
			"Version:int:v", "Epoch:uint64:epoch", "TrailId:connect.Id:trail_id", "ServerNonce:[]uint8:server_nonce", "Vpk:[]uint8:vpk",
			"M:int:m", "Hops:[]connect.VerifyProofHop:hops", "ServerKeyId:uint8:server_key_id", "FinalSig:[]uint8:final_sig",
			"VerifierSig:[]uint8:verifier_sig", "FinalDigest:[]uint8:final_digest", "VpkSig:[]uint8:vpk_sig",
			"Coverage:uint64:coverage", "PathId:[]uint8:path_id", "CompleteTimeMs:uint64:complete_time_ms",
		}},
		{typeOf: reflect.TypeOf(connect.VerifyProofHop{}), fields: []string{
			"ClientId:connect.Id:client_id", "TimeMs:uint64:time_ms", "EgressIpHash:[32]uint8:egress_ip_hash",
		}},
	} {
		actual := []string{}
		for index := 0; index < row.typeOf.NumField(); index++ {
			field := row.typeOf.Field(index)
			actual = append(actual, field.Name+":"+field.Type.String()+":"+field.Tag.Get("json"))
		}
		if !reflect.DeepEqual(actual, row.fields) {
			t.Fatalf("%s clone field census needs review: %v", row.typeOf, actual)
		}
		marshalType := reflect.TypeOf((*json.Marshaler)(nil)).Elem()
		unmarshalType := reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
		textMarshalType := reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
		textUnmarshalType := reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
		for _, checked := range []reflect.Type{row.typeOf, reflect.PointerTo(row.typeOf)} {
			if checked.Implements(marshalType) || checked.Implements(unmarshalType) ||
				checked.Implements(textMarshalType) || checked.Implements(textUnmarshalType) {
				t.Fatalf("%s new JSON method needs clone-equivalence review", checked)
			}
		}
	}
	if actual := reflect.TypeOf(connect.Id{}); actual.Kind() != reflect.Array || actual.Len() != 16 || actual.Elem().Kind() != reflect.Uint8 {
		t.Fatal("custom ID marshaler width needs clone-equivalence review")
	}
	for _, id := range []connect.Id{{}, {0: 0xff, 7: 0x80, 15: 0xfe}} {
		raw, err := json.Marshal(id)
		var decoded connect.Id
		if err != nil || len(raw) != 38 || json.Unmarshal(raw, &decoded) != nil || decoded != id {
			t.Fatal("custom ID JSON no longer preserves its complete value")
		}
	}
}

// The wrapper and all six production clone sites stay on one implementation.
// This routing guard supplements, and does not replace, the real API controls.
func TestAttemptRecordCloneAllProductionCallersRetained(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	want := map[string]int{"RecordsAfter": 1, "appendLegacyWithLock": 2, "BuildCut": 1, "Walk": 1, "appendDiskWithLock": 1}
	actual := map[string]int{}
	wrapperCalls := 0
	for _, path := range []string{"attempt_ledger.go", "attempt_ledger_stream.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			method, ok := declaration.(*ast.FuncDecl)
			if !ok || method.Body == nil {
				continue
			}
			if method.Name.Name == "cloneAttemptRecord" {
				if len(method.Body.List) != 1 {
					t.Fatal("clone wrapper performs work outside the observed helper")
				}
				returned, ok := method.Body.List[0].(*ast.ReturnStmt)
				if !ok || len(returned.Results) != 1 {
					t.Fatal("clone wrapper is not a direct owned-copy return")
				}
				call, ok := returned.Results[0].(*ast.CallExpr)
				if !ok || len(call.Args) != 1 {
					t.Fatal("clone wrapper changed its observed argument path")
				}
				argument, ok := call.Args[0].(*ast.Ident)
				if !ok || argument.Name != "record" {
					t.Fatal("clone wrapper transforms input before the observed copy")
				}
			}
			ast.Inspect(method.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if identifier, ok := call.Fun.(*ast.Ident); ok && identifier.Name == "cloneAttemptRecord" {
					actual[method.Name.Name]++
				}
				if method.Name.Name == "cloneAttemptRecord" {
					if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "clone" {
						receiver, ok := selector.X.(*ast.ParenExpr)
						if ok {
							literal, ok := receiver.X.(*ast.CompositeLit)
							if ok {
								typeName, ok := literal.Type.(*ast.Ident)
								if ok && typeName.Name == "attemptRecordCloneWork" && len(literal.Elts) == 0 {
									wrapperCalls++
								}
							}
						}
					}
				}
				return true
			})
		}
	}
	if !reflect.DeepEqual(actual, want) || wrapperCalls != 1 {
		t.Fatalf("clone route census changed: actual%v wrapper%d", actual, wrapperCalls)
	}
}

// Both the stored and returned legacy clones stay detached from caller memory.
// Canonical durable JSONL and all eight hashes/signatures remain byte-identical.
func TestAttemptRecordCloneLegacyAppendPreservesSignedJSONL(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	cut, key, keys := attemptComparisonRealCut(t)
	ledger, err := NewAttemptLedger(newAttemptLedgerDiskTestStateDir(t), cut.Identity, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	for index := range cut.Records {
		input := attemptRecordCloneTestJSON(t, cut.Records[index])
		committed, err := ledger.AppendContext(context.Background(), input)
		if err != nil || committed == nil || !reflect.DeepEqual(*committed, cut.Records[index]) {
			t.Fatalf("signed legacy append %d changed: %v", index, err)
		}
		input.ServerNonce[0] ^= 1
		input.Assignments[0].AssignSignature[0] ^= 1
		if input.Proof != nil {
			input.Proof.Hops[0].EgressIpHash[0] ^= 1
		}
		committed.ServerNonce[0] ^= 1
		committed.Assignments[0].AssignMessage[0] ^= 1
		if committed.Proof != nil {
			committed.Proof.FinalSig[0] ^= 1
		}
	}
	actual, err := ledger.RecordsAfter(0)
	if err != nil || !reflect.DeepEqual(actual, cut.Records) {
		t.Fatalf("legacy append retained external memory: %v", err)
	}
	raw, err := os.ReadFile(ledger.path)
	if err != nil || !bytes.Equal(raw, attemptLedgerDiskTestJSONL(t, cut.Records)) {
		t.Fatalf("legacy clone changed actual signed JSONL: %v", err)
	}
	replayed, err := ledger.BuildCut(cut.Boundary, 1, 1)
	if err != nil || !reflect.DeepEqual(replayed, cut) {
		t.Fatalf("legacy append cut changed: %v", err)
	}
	if err := VerifyAttemptLedgerCut(replayed, key.Public().(ed25519.PublicKey), keys); err != nil {
		t.Fatal(err)
	}
}

// RecordsAfter, Walk and BuildCut export independent owners; callbacks may
// synchronously read the head, and cancellation remains an exact range stop.
func TestAttemptRecordCloneLegacyReadersAndRestartStayOwned(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	cut, key, keys := attemptComparisonRealCut(t)
	dir := newAttemptLedgerDiskTestStateDir(t)
	ledger, err := NewAttemptLedger(dir, cut.Identity, key)
	if err != nil {
		t.Fatal(err)
	}
	firstLedger := ledger
	t.Cleanup(func() { _ = firstLedger.Close() })
	for _, record := range cut.Records {
		if _, err := ledger.Append(record); err != nil {
			t.Fatal(err)
		}
	}
	exported, err := ledger.RecordsAfter(0)
	if err != nil {
		t.Fatal(err)
	}
	exported[0].Assignments[0].AssignSignature[0] ^= 1
	exported[7].Proof.Hops[0].EgressIpHash[0] ^= 1
	visited := 0
	if err := ledger.Walk(context.Background(), 1, 8, func(record AttemptRecord) error {
		if ledger.LastSequence() != 8 || !reflect.DeepEqual(record, cut.Records[visited]) {
			return errors.New("walk changed signed record or held the state mutex")
		}
		visited++
		record.Assignments[0].Trail[0][0] ^= 1
		record.Signature[0] ^= 1
		if record.Proof != nil {
			record.Proof.VpkSig[0] ^= 1
		}
		return nil
	}); err != nil || visited != 8 {
		t.Fatalf("owned legacy walk visited%d: %v", visited, err)
	}
	derived, err := ledger.BuildCut(cut.Boundary, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	derived.Records[7].Proof.ServerNonce[0] ^= 1
	derived.Records[0].Assignments[0].AssignMessage[0] ^= 1
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	visited = 0
	err = ledger.Walk(ctx, 1, 8, func(record AttemptRecord) error {
		visited++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || visited != 1 {
		t.Fatalf("legacy walk cancellation lost: visited%d error%v", visited, err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	ledger, err = NewAttemptLedger(dir, cut.Identity, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	restarted, err := ledger.BuildCut(cut.Boundary, 1, 1)
	if err != nil || !reflect.DeepEqual(restarted, cut) {
		t.Fatalf("actual JSONL restart changed signed cut: %v", err)
	}
	if err := VerifyAttemptLedgerCut(restarted, key.Public().(ed25519.PublicKey), keys); err != nil {
		t.Fatal(err)
	}
}

// The disk append return copy changes only ownership; bounded canonical encode,
// actual durable storage, streaming reads and reopened prefix identity remain.
func TestAttemptRecordCloneDiskAppendWalkAndReopen(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	cut, key, keys := attemptComparisonRealCut(t)
	fixture := attemptRecordStoreTestFixture{recordTs: cut.Records, validatorKey: key, identity: cut.Identity}
	dir := newAttemptLedgerDiskTestStateDir(t)
	ledger := openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
	for index := range cut.Records {
		input := attemptRecordCloneTestJSON(t, cut.Records[index])
		committed, err := ledger.AppendContext(context.Background(), input)
		if err != nil || committed == nil || !reflect.DeepEqual(*committed, cut.Records[index]) {
			t.Fatalf("signed disk append %d changed: %v", index, err)
		}
		input.Assignments[0].AssignMessage[0] ^= 1
		committed.Assignments[0].AssignSignature[0] ^= 1
		if input.Proof != nil {
			input.Proof.Hops[0].EgressIpHash[0] ^= 1
			committed.Proof.FinalSig[0] ^= 1
		}
	}
	for restart := 0; restart < 2; restart++ {
		head, err := ledger.Head()
		if err != nil || head.LastSequence != 8 || head.Root != cut.Root {
			t.Fatalf("disk head changed at restart%d: %+v %v", restart, head, err)
		}
		visited := 0
		replayed := *cut
		replayed.Records = make([]AttemptRecord, 0, 8)
		err = ledger.Walk(context.Background(), 1, 8, func(record AttemptRecord) error {
			if !reflect.DeepEqual(record, cut.Records[visited]) {
				return fmt.Errorf("disk owned record %d changed", visited)
			}
			replayed.Records = append(replayed.Records, attemptRecordCloneTestJSON(t, record))
			visited++
			record.ServerNonce[0] ^= 1
			record.Assignments[0].Trail[0][0] ^= 1
			if record.Proof != nil {
				record.Proof.VerifierSig[0] ^= 1
			}
			return nil
		})
		if err != nil || visited != 8 {
			t.Fatalf("disk walk restart%d visited%d: %v", restart, visited, err)
		}
		if err := VerifyAttemptLedgerCut(&replayed, key.Public().(ed25519.PublicKey), keys); err != nil {
			t.Fatal(err)
		}
		if _, err := ledger.RecordsAfter(0); !errors.Is(err, ErrAttemptLedgerStreamingRequired) {
			t.Fatal("disk legacy materialization boundary changed")
		}
		if _, err := ledger.BuildCut(cut.Boundary, 1, 1); !errors.Is(err, ErrAttemptLedgerStreamingRequired) {
			t.Fatal("disk BuildCut materialization boundary changed")
		}
		if restart == 0 {
			if err := ledger.Close(); err != nil {
				t.Fatal(err)
			}
			ledger = openAttemptLedgerDiskTest(t, dir, fixture, attemptLedgerDiskHooks{})
		}
	}
}

// A genuine transport failure retains its signed M8 pending+failed terminal;
// typed ownership cannot invent a successful proof or suppress authentication.
func TestAttemptRecordCloneRetainsRealFailedM8Terminal(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	server, key, clientID := newMockVerifyServer(t, 12)
	engine, stats, _ := newTestEngine(t, server, key, clientID, 8, nil)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, t.TempDir(), &generation)
	for _, provider := range server.providers[1:] {
		server.failHops[provider] = true
	}
	proof, err := engine.RunTrail(context.Background())
	var trailError *TrailError
	if proof != nil || !errors.As(err, &trailError) || trailError.Kind != TrailErrorHop {
		t.Fatalf("actual M8 hop error was not retained: proof%v error%v", proof, err)
	}
	cut, err := ledger.BuildCut(attemptLedgerTestBoundary(), 1, 1)
	if err != nil || len(cut.Records) != 2 || cut.Records[1].Disposition != AttemptDispositionHopFailure ||
		cut.Records[0].M != 8 || cut.Records[1].M != 8 || cut.Records[1].Proof != nil {
		t.Fatalf("genuine failed M8 census changed: %v", err)
	}
	serializations := 0
	work := attemptRecordCloneWork{serialized: func() { serializations++ }}
	for index := range cut.Records {
		cut.Records[index], err = work.clone(cut.Records[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := VerifyAttemptLedgerCut(cut, key.Public().(ed25519.PublicKey), server.serverPublicKeys()); err != nil {
		t.Fatal(err)
	}
	t.Logf("ATTEMPT-CLONE-v1 WORK %s records=2 serialized=%d", t.Name(), serializations)
	if serializations != 0 {
		t.Fatalf("clone-work-failed-m8: serialized %d genuine failed terminal copies, want 0", serializations)
	}
}

// A mutated cloned proof remains unauthenticated; no clone or previous verify
// result can become a cache of accepted signed authority.
func TestAttemptRecordCloneMutationStillNeedsRealVerification(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	cut, key, keys := attemptComparisonRealCut(t)
	cloned := *cut
	cloned.Records = make([]AttemptRecord, len(cut.Records))
	for index := range cut.Records {
		var err error
		cloned.Records[index], err = cloneAttemptRecord(cut.Records[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := VerifyAttemptLedgerCut(&cloned, key.Public().(ed25519.PublicKey), keys); err != nil {
		t.Fatal(err)
	}
	cloned.Records[7].Proof.Hops[0].EgressIpHash[0] ^= 1
	if err := VerifyAttemptLedgerCut(&cloned, key.Public().(ed25519.PublicKey), keys); err == nil {
		t.Fatal("mutated copied proof reused accepted authority")
	}
	if err := VerifyAttemptLedgerCut(cut, key.Public().(ed25519.PublicKey), keys); err != nil {
		t.Fatalf("copied proof mutation changed original signed owner: %v", err)
	}
}

// The small negative fixture keeps genuine signed evidence and positive
// reconstructed prefixes. Independent raw clones are never verdict caches.
func TestAttemptRecordCloneReleaseFixtureIndependentRawBase(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	base := releaseMeasurementTopFixture(t, 2)
	before, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyReleaseMeasurementArtifact(base)
	if err != nil || len(verified.SelectedHead) != 2 {
		t.Fatalf("two-fleet signed control is not valid: %v", err)
	}
	changed := cloneReleaseMeasurementArtifact(t, base)
	changes := 0
	for inputIndex := range changed.Inputs {
		for providerIndex := range changed.Inputs[inputIndex].Stats.Providers {
			provider := &changed.Inputs[inputIndex].Stats.Providers[providerIndex]
			if provider.ClientID == releaseMeasurementTestID(1).String() {
				if len(provider.EgressIPHashHexes) < 2 {
					t.Fatal("small control lost its positive two-prefix evidence")
				}
				provider.EgressIPHashHexes = provider.EgressIPHashHexes[:1]
				changes++
			}
		}
	}
	if changes != 1 {
		t.Fatalf("small unproven-prefix mutation changed%d providers, want1", changes)
	}
	if _, err := VerifyReleaseMeasurementArtifact(changed); err == nil {
		t.Fatal("small signed fixture accepted an unproven positive score")
	}
	missing := cloneReleaseMeasurementArtifact(t, base)
	missing.Inputs[0].Stats.AttemptCut = nil
	if _, err := VerifyReleaseMeasurementArtifact(missing); err == nil {
		t.Fatal("independent raw variant accepted missing signed authority")
	}
	after, err := json.Marshal(base)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("independent raw mutations changed their private base: %v", err)
	}
	if _, err := VerifyReleaseMeasurementArtifact(base); err != nil {
		t.Fatalf("full real re-verification of untouched base failed: %v", err)
	}
}

// Work observers have only invocation ownership; no mutable package observer
// is installed and simultaneous calls retain independent fallback counts.
func TestAttemptRecordCloneObserversRemainCallLocal(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	start := make(chan struct{})
	type result struct {
		valid bool
		serializations int
		err error
	}
	done := make(chan result, 2)
	for _, valid := range []bool{false, true} {
		go func(valid bool) {
			record := attemptRecordCloneTestValue()
			if !valid {
				record.Schema = string([]byte{0xff, 0xfe})
			}
			count := 0
			<-start
			got, err := (attemptRecordCloneWork{serialized: func() { count++ }}).clone(record)
			if !valid && got.Schema != "\ufffd\ufffd" {
				err = errors.Join(err, errors.New("invalid UTF8 replacement changed"))
			}
			done <- result{valid: valid, serializations: count, err: err}
		}(valid)
	}
	close(start)
	for range 2 {
		got := <-done
		want := 1
		if got.valid {
			want = 0
		}
		t.Logf("ATTEMPT-CLONE-v1 WORK %s valid=%t serialized=%d", t.Name(), got.valid, got.serializations)
		if got.err != nil || got.serializations != want {
			t.Errorf("clone-work-call-local: valid%t actual%d want%d error%v", got.valid, got.serializations, want, got.err)
		}
	}
}

// All new behavioral roots are selected by the existing Attempt prefix when
// the broad producer gate runs; this does not claim that gate has been run.
func TestAttemptRecordCloneExistingProducerSelection(t *testing.T) {
	attemptRecordCloneTestRoot(t)
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile("(?m)^\\s*producer_tests='([^']+)'$").FindAllStringSubmatch(string(raw), -1)
	if len(matches) != 1 {
		t.Fatal("producer selector is absent or ambiguous")
	}
	selection, err := regexp.Compile(matches[0][1])
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "attempt_record_clone_test.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	selected := 0
	for _, declaration := range file.Decls {
		method, ok := declaration.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(method.Name.Name, "TestAttemptRecordClone") {
			continue
		}
		if !selection.MatchString(method.Name.Name) {
			t.Fatalf("existing producer selector omits %s", method.Name.Name)
		}
		selected++
	}
	if selected != 16 {
		t.Fatalf("clone selector root census changed: got%d want16", selected)
	}
}
