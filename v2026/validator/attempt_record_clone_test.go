// Typed ledger copies retain the old codec's exact values while allocating
// only detached owners. These controls use synthetic records and real trails.
package validator

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

// Every current mutable owner is populated independently, including a second
// assignment and every proof byte slice. Identity values are synthetic.
func attemptClonePopulatedRecord() AttemptRecord {
	first := connect.Id{0: 1, 15: 2}
	second := connect.Id{0: 3, 15: 4}
	return AttemptRecord{
		Schema: "synthetic-clone<&>",
		Identity: AttemptLedgerIdentity{
			DeploymentID: "clone-deployment", ChainID: 117,
			GenesisHash: "clone-genesis", Netuid: 123,
			ValidatorID: 19, ValidatorUID: 0, NoID: 23, ValidatorVPK: "clone-key",
		},
		Sequence: ^uint64(0), PreviousHash: "clone-parent",
		Boundary: AttemptBoundary{SettlementEpoch: 27, EVMBlock: 31, EVMBlockHash: "clone-block"},
		TrailID:  first, ServerNonce: []byte{1, 2}, VPK: []byte{3, 4}, M: 3,
		Assignments: []AttemptAssignment{
			{
				Trail: []connect.Id{first}, NextHop: second, ServerKeyID: 5,
				AssignMessage: []byte{6, 7}, AssignSignature: []byte{8, 9},
				Confirmed: true, HasLatency: true, LatencyBucket: 4,
				Binding: AttemptBinding{ClientID: second, Active: true, FleetID: "clone-fleet-a", Hotkey: "clone-hotkey-a", Generation: 37, UIDFound: true, UID: 0},
			},
			{
				Trail: []connect.Id{first, second}, NextHop: first, ServerKeyID: 11,
				AssignMessage: []byte{12, 13}, AssignSignature: []byte{14, 15},
				Binding: AttemptBinding{ClientID: first, FleetID: "clone-fleet-b", Hotkey: "clone-hotkey-b", Generation: ^uint64(0), UID: ^uint16(0)},
			},
		},
		Disposition: AttemptDispositionComplete, RecordHash: "clone-record", Signature: []byte{16, 17},
		Proof: &ProofRecord{
			Version: 1, Epoch: 27, TrailId: first, ServerNonce: []byte{18, 19}, Vpk: []byte{20, 21}, M: 3,
			Hops:        []connect.VerifyProofHop{{ClientId: first, TimeMs: ^uint64(0), EgressIpHash: [32]byte{0: 22, 31: 23}}},
			ServerKeyId: 24, FinalSig: []byte{25, 26}, VerifierSig: []byte{27, 28},
			FinalDigest: []byte{29, 30}, VpkSig: []byte{31, 32}, Coverage: 2,
			PathId: []byte{33, 34}, CompleteTimeMs: ^uint64(0),
		},
	}
}

// Use the removed serialization operation as an independent value oracle.
func attemptCloneLegacyRecord(t *testing.T, record AttemptRecord) AttemptRecord {
	t.Helper()
	encoded, err := json.Marshal(&record)
	if err != nil {
		t.Fatal(err)
	}
	var cloned AttemptRecord
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

// Mutate every populated leaf, traversing arrays, slices and pointers. Passing
// a copied outer record still exposes any nested owner the clone failed to copy.
func mutateAttemptCloneLeaves(value reflect.Value) {
	switch value.Kind() {
	case reflect.Struct:
		for index := range value.NumField() {
			mutateAttemptCloneLeaves(value.Field(index))
		}
	case reflect.Pointer:
		if !value.IsNil() {
			mutateAttemptCloneLeaves(value.Elem())
		}
	case reflect.Slice, reflect.Array:
		for index := range value.Len() {
			mutateAttemptCloneLeaves(value.Index(index))
		}
	case reflect.String:
		value.SetString(value.String() + "-mutated")
	case reflect.Bool:
		value.SetBool(!value.Bool())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(value.Uint() ^ 1)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(value.Int() ^ 1)
	}
}

// Neither direction can change the other's bytes, proof values or wire hash.
func TestAttemptRecordCloneDetachesEveryMutableOwner(t *testing.T) {
	t.Parallel()
	for _, mutateSource := range []bool{false, true} {
		source := attemptClonePopulatedRecord()
		expected := attemptCloneLegacyRecord(t, source)
		cloned, err := cloneAttemptRecord(source)
		if err != nil || !reflect.DeepEqual(cloned, expected) {
			t.Fatalf("initial clone changed values: %v", err)
		}
		expectedHash, err := attemptRecordHash(&expected)
		if err != nil {
			t.Fatal(err)
		}
		mutated, untouched := &cloned, &source
		if mutateSource {
			mutated, untouched = &source, &cloned
		}
		mutateAttemptCloneLeaves(reflect.ValueOf(mutated).Elem())
		if reflect.DeepEqual(*mutated, expected) || !reflect.DeepEqual(*untouched, expected) {
			t.Fatalf("mutable ownership leaked across clone: mutateSource=%t", mutateSource)
		}
		actualHash, err := attemptRecordHash(untouched)
		if err != nil || actualHash != expectedHash {
			t.Fatalf("detached record's signed hash changed: %v", err)
		}
	}
}

// Nil and allocated-empty slices are distinct values in the previous codec;
// invalid strings replace each invalid byte, not a whole run of invalid bytes.
func TestAttemptRecordClonePreservesLegacyCodecValues(t *testing.T) {
	t.Parallel()
	empty := AttemptRecord{
		ServerNonce: []byte{}, VPK: []byte{}, Signature: []byte{}, Assignments: []AttemptAssignment{},
		Proof: &ProofRecord{ServerNonce: []byte{}, Vpk: []byte{}, Hops: []connect.VerifyProofHop{}, FinalSig: []byte{}, VerifierSig: []byte{}, FinalDigest: []byte{}, VpkSig: []byte{}, PathId: []byte{}},
	}
	emptyAssignment := empty
	emptyAssignment.Assignments = []AttemptAssignment{{Trail: []connect.Id{}, AssignMessage: []byte{}, AssignSignature: []byte{}}}
	cases := []AttemptRecord{{}, {Proof: &ProofRecord{}}, empty, emptyAssignment, attemptClonePopulatedRecord()}
	for _, value := range []string{"ascii<&>\u2028\u2029", "valid-\ufffd-\u00e9", string([]byte{0xff, 0xfe, 0xc0, 0xaf, 0xe2, 0x82})} {
		record := attemptClonePopulatedRecord()
		record.Schema, record.PreviousHash, record.Disposition, record.RecordHash = value, value, value, value
		record.Identity.DeploymentID, record.Identity.GenesisHash, record.Identity.ValidatorVPK = value, value, value
		record.Boundary.EVMBlockHash = value
		for index := range record.Assignments {
			record.Assignments[index].Binding.FleetID = value
			record.Assignments[index].Binding.Hotkey = value
		}
		cases = append(cases, record)
	}
	for index, record := range cases {
		expected := attemptCloneLegacyRecord(t, record)
		actual, err := cloneAttemptRecord(record)
		if err != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("legacy value parity failed in case %d: %v", index, err)
		}
		encoded, err := json.Marshal(actual)
		if err != nil {
			t.Fatal(err)
		}
		legacy, err := json.Marshal(expected)
		if err != nil || !bytes.Equal(encoded, legacy) {
			t.Fatalf("legacy wire parity failed in case %d: %v", index, err)
		}
	}
}

// Serialization adds many string/codec allocations to the same owned tree.
// The bound counts owners, not elapsed time, and runs outside parallel tests.
func TestAttemptRecordCloneAllocatesOnlyDetachedOwners(t *testing.T) {
	source := attemptClonePopulatedRecord()
	var cloned AttemptRecord
	var cloneErr error
	allocations := testing.AllocsPerRun(100, func() { cloned, cloneErr = cloneAttemptRecord(source) })
	// Nineteen populated owners plus five allocations of implementation slack.
	if cloneErr != nil || allocations <= 0 || allocations > 24 {
		t.Fatalf("clone allocated serialization work: allocations=%g maximum=24 error=%v", allocations, cloneErr)
	}
	if !reflect.DeepEqual(cloned, source) {
		t.Fatal("measured clone omitted its input")
	}
}

// A new mutable or string field requires an explicit ownership/codec decision
// and a populated fixture; it cannot silently bypass this typed copy review.
func TestAttemptRecordCloneFieldInventoryRequiresExplicitOwnership(t *testing.T) {
	t.Parallel()
	expected := map[string]reflect.Kind{
		"Schema": reflect.String, "Identity.DeploymentID": reflect.String,
		"Identity.GenesisHash": reflect.String, "Identity.ValidatorVPK": reflect.String,
		"PreviousHash": reflect.String, "Boundary.EVMBlockHash": reflect.String,
		"Disposition": reflect.String, "RecordHash": reflect.String,
		"ServerNonce": reflect.Slice, "VPK": reflect.Slice, "Signature": reflect.Slice,
		"Assignments": reflect.Slice, "Assignments[].Trail": reflect.Slice,
		"Assignments[].AssignMessage": reflect.Slice, "Assignments[].AssignSignature": reflect.Slice,
		"Assignments[].Binding.FleetID": reflect.String, "Assignments[].Binding.Hotkey": reflect.String,
		"Proof": reflect.Pointer, "Proof.ServerNonce": reflect.Slice, "Proof.Vpk": reflect.Slice,
		"Proof.Hops": reflect.Slice, "Proof.FinalSig": reflect.Slice, "Proof.VerifierSig": reflect.Slice,
		"Proof.FinalDigest": reflect.Slice, "Proof.VpkSig": reflect.Slice, "Proof.PathId": reflect.Slice,
	}
	actual := map[string]reflect.Kind{}
	var inspect func(reflect.Type, string)
	inspect = func(value reflect.Type, path string) {
		switch value.Kind() {
		case reflect.Struct:
			for index := range value.NumField() {
				field := value.Field(index)
				name := field.Name
				if path != "" {
					name = path + "." + name
				}
				inspect(field.Type, name)
			}
		case reflect.Pointer:
			actual[path] = value.Kind()
			inspect(value.Elem(), path)
		case reflect.Slice:
			actual[path] = value.Kind()
			inspect(value.Elem(), path+"[]")
		case reflect.Array:
			inspect(value.Elem(), path+"[]")
		case reflect.String, reflect.Map, reflect.Interface, reflect.Chan, reflect.Func, reflect.UnsafePointer:
			actual[path] = value.Kind()
		}
	}
	inspect(reflect.TypeFor[AttemptRecord](), "")
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("attempt record ownership inventory changed: actual=%v expected=%v", actual, expected)
	}
}

// Caller mutations after real trail publication cannot corrupt the journal,
// later cut, independent server-signature replay or measurement statistics.
func TestAttemptRecordClonePreservesRealLedgerCutAfterCallerMutation(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	server, validatorKey, clientId := newMockVerifyServer(t, 12)
	store, err := NewProofStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientId, 5, store)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	if _, err := engine.RunTrail(t.Context()); err != nil {
		t.Fatal(err)
	}
	originalBytes, err := os.ReadFile(ledger.path)
	if err != nil {
		t.Fatal(err)
	}
	records, err := ledger.RecordsAfter(0)
	if err != nil || len(records) != 5 || records[4].Proof == nil {
		t.Fatalf("real trail did not publish complete records: count=%d error=%v", len(records), err)
	}
	originalRecords, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	for index := range records {
		mutateAttemptCloneLeaves(reflect.ValueOf(&records[index]).Elem())
	}
	cut, err := ledger.BuildCut(attemptLedgerTestBoundary(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	vpk := validatorKey.Public().(ed25519.PublicKey)
	if err := VerifyAttemptLedgerCut(cut, vpk, server.serverPublicKeys()); err != nil {
		t.Fatalf("caller mutated a later authenticated cut: %v", err)
	}
	for index := range cut.Records {
		mutateAttemptCloneLeaves(reflect.ValueOf(&cut.Records[index]).Elem())
	}
	current, err := ledger.RecordsAfter(0)
	if err != nil {
		t.Fatal(err)
	}
	currentRecords, err := json.Marshal(current)
	if err != nil || !bytes.Equal(currentRecords, originalRecords) {
		t.Fatalf("public record or cut mutation reached retained records: %v", err)
	}
	currentBytes, err := os.ReadFile(ledger.path)
	if err != nil || !bytes.Equal(currentBytes, originalBytes) {
		t.Fatalf("caller mutation changed durable journal bytes: %v", err)
	}
	measurement, err := stats.detachReleaseStatsMeasurementWithAttemptCut(stateDir, attemptLedgerTestBoundary(), func(ReleaseStatsMeasurement, uint64) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAttemptLedgerCut(measurement.AttemptCut, vpk, server.serverPublicKeys()); err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyReleaseStatsMeasurement(measurement)
	if err != nil || len(verified.Providers) != 4 {
		t.Fatalf("caller mutation changed independent measurement replay: %v", err)
	}
}
