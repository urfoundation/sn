package validator

// Complete real M8 cuts observe actual comparison/JSON work. The independent
// legacy comparator retains all pre-repair normalization semantics as an oracle.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/urnetwork/connect"
)

// Copies every mutable slice without collapsing nil and nonnil empty values.
func cloneAttemptComparisonAssignments(source []AttemptAssignment) []AttemptAssignment {
	if source == nil {
		return nil
	}
	cloned := make([]AttemptAssignment, len(source))
	copy(cloned, source)
	for index := range cloned {
		if source[index].Trail != nil {
			cloned[index].Trail = make([]connect.Id, len(source[index].Trail))
			copy(cloned[index].Trail, source[index].Trail)
		}
		cloned[index].AssignMessage = bytes.Clone(source[index].AssignMessage)
		cloned[index].AssignSignature = bytes.Clone(source[index].AssignSignature)
	}
	return cloned
}

// This is the unchanged old implementation, independent of production helpers.
func oldAttemptComparisonJSON(left, right []AttemptAssignment) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

// Every assertion retains all seven checkpoints and the genuine terminal proof.
func attemptComparisonRealCut(t *testing.T) (*AttemptLedgerCut, ed25519.PrivateKey, map[byte]ed25519.PublicKey) {
	t.Helper()
	cut, key, keys := attemptBoundaryLifecycleFixture(t, 8)
	if len(cut.Records) != 8 || cut.RecordCount != 8 {
		t.Fatal("comparison fixture lost the real eight-record M8 census")
	}
	for index, record := range cut.Records {
		if record.M != 8 || record.Sequence != uint64(index+1) {
			t.Fatal("comparison fixture changed signed depth or sequence")
		}
		if index < 7 && (record.Disposition != AttemptDispositionPending || len(record.Assignments) != index+1) {
			t.Fatal("comparison fixture lost an intermediate pending checkpoint")
		}
	}
	last := cut.Records[7]
	if last.Disposition != AttemptDispositionComplete || last.Proof == nil || last.Proof.M != 8 || len(last.Proof.Hops) != 8 {
		t.Fatal("comparison fixture omitted its actual complete signed M8 proof")
	}
	return cut, key, keys
}

// Counts only the equality helper's real JSON calls, inside full authentication.
func TestAttemptComparisonAvoidsJSONForRealM8Cut(t *testing.T) {
	t.Parallel()
	cut, key, keys := attemptComparisonRealCut(t)
	serializations := 0
	work := attemptAssignmentComparisonWork{serialized: func() { serializations++ }}
	if err := verifyAttemptLedgerCutWithComparison(cut, key.Public().(ed25519.PublicKey), keys, true, connect.VerifyVerifyMessageSignature, work); err != nil {
		t.Fatal(err)
	}
	if serializations != 0 {
		t.Fatalf("real M8 lifecycle serialized assignment prefixes: got %d, want exactly 0", serializations)
	}
}

// Six extensions and one applicable terminal comparison are sufficient. The
// discarded unadjusted terminal result must not execute as hidden extra work.
func TestAttemptComparisonCompletesTerminalOnce(t *testing.T) {
	t.Parallel()
	cut, key, keys := attemptComparisonRealCut(t)
	comparisons := 0
	work := attemptAssignmentComparisonWork{compared: func() { comparisons++ }}
	if err := verifyAttemptLedgerCutWithComparison(cut, key.Public().(ed25519.PublicKey), keys, true, connect.VerifyVerifyMessageSignature, work); err != nil {
		t.Fatal(err)
	}
	if comparisons != 7 {
		t.Fatalf("real M8 lifecycle repeated completed-terminal comparison: got %d, want exactly 7", comparisons)
	}
}

// All scalar/nested/slice fields are independently changed from genuine bytes.
func TestAttemptComparisonJSONParityEveryField(t *testing.T) {
	t.Parallel()
	cut, _, _ := attemptComparisonRealCut(t)
	original := cut.Records[7].Assignments
	changes := []struct {
		name string
		edit func(*AttemptAssignment)
	}{
		{name: "trail", edit: func(a *AttemptAssignment) { a.Trail[0][0] ^= 1 }},
		{name: "next_hop", edit: func(a *AttemptAssignment) { a.NextHop[0] ^= 1 }},
		{name: "server_key_id", edit: func(a *AttemptAssignment) { a.ServerKeyID ^= 1 }},
		{name: "assign_message", edit: func(a *AttemptAssignment) { a.AssignMessage[0] ^= 1 }},
		{name: "assign_signature", edit: func(a *AttemptAssignment) { a.AssignSignature[0] ^= 1 }},
		{name: "confirmed", edit: func(a *AttemptAssignment) { a.Confirmed = !a.Confirmed }},
		{name: "has_latency", edit: func(a *AttemptAssignment) { a.HasLatency = !a.HasLatency }},
		{name: "latency_bucket", edit: func(a *AttemptAssignment) { a.LatencyBucket ^= 1 }},
		{name: "binding.client_id", edit: func(a *AttemptAssignment) { a.Binding.ClientID[0] ^= 1 }},
		{name: "binding.active", edit: func(a *AttemptAssignment) { a.Binding.Active = !a.Binding.Active }},
		{name: "binding.fleet_id", edit: func(a *AttemptAssignment) { a.Binding.FleetID += "x" }},
		{name: "binding.hotkey", edit: func(a *AttemptAssignment) { a.Binding.Hotkey += "x" }},
		{name: "binding.generation", edit: func(a *AttemptAssignment) { a.Binding.Generation++ }},
		{name: "binding.uid_found", edit: func(a *AttemptAssignment) { a.Binding.UIDFound = !a.Binding.UIDFound }},
		{name: "binding.uid", edit: func(a *AttemptAssignment) { a.Binding.UID++ }},
	}
	if !attemptAssignmentsEqual(original, cloneAttemptComparisonAssignments(original)) {
		t.Fatal("byte-identical owned assignment copy differs")
	}
	for _, change := range changes {
		changed := cloneAttemptComparisonAssignments(original)
		change.edit(&changed[0])
		if oldAttemptComparisonJSON(original, changed) {
			t.Fatalf("%s mutation did not change original JSON", change.name)
		}
		if attemptAssignmentsEqual(original, changed) || attemptAssignmentsEqual(changed, original) {
			t.Errorf("%s field was omitted from assignment comparison", change.name)
		}
	}
	reordered := cloneAttemptComparisonAssignments(original)
	reordered[0], reordered[1] = reordered[1], reordered[0]
	if oldAttemptComparisonJSON(original, reordered) || attemptAssignmentsEqual(original, reordered) {
		t.Fatal("assignment ordering was normalized away")
	}
	reordered = cloneAttemptComparisonAssignments(original)
	reordered[6].Trail[0], reordered[6].Trail[1] = reordered[6].Trail[1], reordered[6].Trail[0]
	if oldAttemptComparisonJSON(original, reordered) || attemptAssignmentsEqual(original, reordered) {
		t.Fatal("trail ordering was normalized away")
	}
}

// JSON null and empty arrays/byte strings remain distinct at every slice level.
func TestAttemptComparisonNilAndEmptyParity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		left, right []AttemptAssignment
		want        bool
	}{
		{name: "both nil", want: true},
		{name: "outer nil empty", right: []AttemptAssignment{}, want: false},
		{name: "outer empty copies", left: []AttemptAssignment{}, right: []AttemptAssignment{}, want: true},
		{name: "trail nil empty", left: []AttemptAssignment{{}}, right: []AttemptAssignment{{Trail: []connect.Id{}}}, want: false},
		{name: "message nil empty", left: []AttemptAssignment{{}}, right: []AttemptAssignment{{AssignMessage: []byte{}}}, want: false},
		{name: "signature nil empty", left: []AttemptAssignment{{}}, right: []AttemptAssignment{{AssignSignature: []byte{}}}, want: false},
		{name: "all nil members", left: []AttemptAssignment{{}}, right: []AttemptAssignment{{}}, want: true},
		{name: "all empty members", left: []AttemptAssignment{{Trail: []connect.Id{}, AssignMessage: []byte{}, AssignSignature: []byte{}}}, right: []AttemptAssignment{{Trail: []connect.Id{}, AssignMessage: []byte{}, AssignSignature: []byte{}}}, want: true},
	}
	for _, row := range cases {
		if old := oldAttemptComparisonJSON(row.left, row.right); old != row.want {
			t.Fatalf("%s independent JSON expectation=%t want=%t", row.name, old, row.want)
		}
		if actual := attemptAssignmentsEqual(row.left, row.right); actual != row.want {
			t.Errorf("%s equality=%t want=%t", row.name, actual, row.want)
		}
	}
}

// Invalid-byte replacement escapes and literal valid U+FFFD have different
// JSON bytes, even when decoding yields the same string. Pin both encodings
// independently; decoded equality must never replace the legacy byte oracle.
func TestAttemptComparisonInvalidUTF8Parity(t *testing.T) {
	t.Parallel()
	const replacementWire = "\"\\ufffd\""
	const twoReplacementWire = "\"\\ufffd\\ufffd\""
	const threeReplacementWire = "\"\\ufffd\\ufffd\\ufffd\""
	const literalReplacementWire = "\"\ufffd\""
	const literalTwoReplacementWire = "\"\ufffd\ufffd\""
	const literalThreeReplacementWire = "\"\ufffd\ufffd\ufffd\""
	const escapedWire = "\"" + "\\u003c" + "\\u003e" + "\\u0026" + "\\u2028" + "\\u2029" + "\\\\" + "\\\"" + "\""
	cases := []struct {
		name                string
		left, right         string
		leftWire, rightWire string
		want, decodedEqual  bool
	}{
		{name: "single invalid collision", left: string([]byte{0xff}), right: string([]byte{0xfe}), leftWire: replacementWire, rightWire: replacementWire, want: true, decodedEqual: true},
		{name: "invalid versus literal replacement", left: string([]byte{0xff}), right: "\ufffd", leftWire: replacementWire, rightWire: literalReplacementWire, want: false, decodedEqual: true},
		{name: "two invalid versus literal replacements", left: string([]byte{0xff, 0xfe}), right: "\ufffd\ufffd", leftWire: twoReplacementWire, rightWire: literalTwoReplacementWire, want: false, decodedEqual: true},
		{name: "two invalid versus one literal replacement", left: string([]byte{0xff, 0xfe}), right: "\ufffd", leftWire: twoReplacementWire, rightWire: literalReplacementWire, want: false, decodedEqual: false},
		{name: "overlong versus literal replacements", left: "x" + string([]byte{0xc0, 0xaf}), right: "x\ufffd\ufffd", leftWire: "\"x\\ufffd\\ufffd\"", rightWire: "\"x\ufffd\ufffd\"", want: false, decodedEqual: true},
		{name: "surrogate versus literal replacements", left: string([]byte{0xed, 0xa0, 0x80}), right: "\ufffd\ufffd\ufffd", leftWire: threeReplacementWire, rightWire: literalThreeReplacementWire, want: false, decodedEqual: true},
		{name: "ASCII versus invalid", left: "ascii", right: string([]byte{0xff}), leftWire: "\"ascii\"", rightWire: replacementWire, want: false, decodedEqual: false},
		{name: "valid JSON escapes", left: "<>&\u2028\u2029\\\"", right: "<>&\u2028\u2029\\\"", leftWire: escapedWire, rightWire: escapedWire, want: true, decodedEqual: true},
		{name: "two invalid collision", left: string([]byte{0xff, 0xfe}), right: string([]byte{0xfe, 0xfd}), leftWire: twoReplacementWire, rightWire: twoReplacementWire, want: true, decodedEqual: true},
		{name: "overlong invalid collision", left: "x" + string([]byte{0xc0, 0xaf}), right: "x" + string([]byte{0xff, 0xfe}), leftWire: "\"x\\ufffd\\ufffd\"", rightWire: "\"x\\ufffd\\ufffd\"", want: true, decodedEqual: true},
		{name: "surrogate invalid collision", left: string([]byte{0xed, 0xa0, 0x80}), right: string([]byte{0xff, 0xfe, 0xfd}), leftWire: threeReplacementWire, rightWire: threeReplacementWire, want: true, decodedEqual: true},
		{name: "truncated invalid collision", left: string([]byte{0xe2, 0x82}), right: string([]byte{0xc0, 0xaf}), leftWire: twoReplacementWire, rightWire: twoReplacementWire, want: true, decodedEqual: true},
		{name: "invalid versus escape text", left: string([]byte{0xff}), right: "\\ufffd", leftWire: replacementWire, rightWire: "\"\\\\ufffd\"", want: false, decodedEqual: false},
		{name: "mixed literal and invalid collision", left: "pre" + string([]byte{0xff}) + "\ufffdpost", right: "pre" + string([]byte{0xfe}) + "\ufffdpost", leftWire: "\"pre\\ufffd\ufffdpost\"", rightWire: "\"pre\\ufffd\ufffdpost\"", want: true, decodedEqual: true},
		{name: "invalid position is retained", left: "a" + string([]byte{0xff}) + "b", right: "ab" + string([]byte{0xfe}), leftWire: "\"a\\ufffdb\"", rightWire: "\"ab\\ufffd\"", want: false, decodedEqual: false},
		{name: "valid replacement collision", left: "\ufffd", right: "\ufffd", leftWire: literalReplacementWire, rightWire: literalReplacementWire, want: true, decodedEqual: true},
		{name: "valid Unicode is not folded", left: "\u00e9", right: "e\u0301", leftWire: "\"\u00e9\"", rightWire: "\"e\u0301\"", want: false, decodedEqual: false},
	}
	for _, row := range cases {
		leftWire, leftErr := json.Marshal(row.left)
		rightWire, rightErr := json.Marshal(row.right)
		if leftErr != nil || rightErr != nil || !bytes.Equal(leftWire, []byte(row.leftWire)) || !bytes.Equal(rightWire, []byte(row.rightWire)) {
			t.Fatalf("%s exact JSON bytes differ: left=%x want=%x right=%x want=%x errors=%v/%v", row.name, leftWire, row.leftWire, rightWire, row.rightWire, leftErr, rightErr)
		}
		if bytes.Equal(leftWire, rightWire) != row.want {
			t.Fatalf("%s independent wire equality expectation is inconsistent", row.name)
		}
		var leftDecoded, rightDecoded string
		if err := json.Unmarshal([]byte(row.leftWire), &leftDecoded); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(row.rightWire), &rightDecoded); err != nil {
			t.Fatal(err)
		}
		if (leftDecoded == rightDecoded) != row.decodedEqual {
			t.Fatalf("%s independent decoded-string expectation differs", row.name)
		}
		for field, fieldName := range []string{"fleet_id", "hotkey"} {
			left, right := []AttemptAssignment{{}}, []AttemptAssignment{{}}
			if field == 0 {
				left[0].Binding.FleetID, right[0].Binding.FleetID = row.left, row.right
			} else {
				left[0].Binding.Hotkey, right[0].Binding.Hotkey = row.left, row.right
			}
			leftJSON, leftErr := json.Marshal(left)
			rightJSON, rightErr := json.Marshal(right)
			prefix := "\"" + fieldName + "\":"
			if leftErr != nil || rightErr != nil || !bytes.Contains(leftJSON, []byte(prefix+row.leftWire)) || !bytes.Contains(rightJSON, []byte(prefix+row.rightWire)) {
				t.Fatalf("%s field%d did not preserve the independent string wire bytes", row.name, field)
			}
			for direction := 0; direction < 2; direction++ {
				if direction == 1 {
					left, right = right, left
				}
				if oldAttemptComparisonJSON(left, right) != row.want {
					t.Fatalf("%s independent old JSON oracle differs for field%d direction%d", row.name, field, direction)
				}
				if actual := attemptAssignmentsEqual(left, right); actual != row.want {
					t.Errorf("%s comparison changed for field%d direction%d: got=%t want=%t", row.name, field, direction, actual, row.want)
				}
				if !utf8.ValidString(row.left) || !utf8.ValidString(row.right) {
					serializations := 0
					work := attemptAssignmentComparisonWork{serialized: func() { serializations++ }}
					if actual := work.equal(left, right); actual != row.want || serializations != 2 {
						t.Errorf("%s fallback changed for field%d direction%d: result=%t want=%t serializations=%d want2", row.name, field, direction, actual, row.want, serializations)
					}
				}
			}
		}
	}
}

// Future fields/tags must be reviewed before the handwritten comparator can
// claim equivalence. This also pins the custom ID marshaler's underlying width.
func TestAttemptComparisonFieldCensus(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		typeOf reflect.Type
		fields []string
	}{
		{typeOf: reflect.TypeOf(AttemptAssignment{}), fields: []string{
			"Trail:[]connect.Id:trail", "NextHop:connect.Id:next_hop", "ServerKeyID:uint8:server_key_id",
			"AssignMessage:[]uint8:assign_message", "AssignSignature:[]uint8:assign_signature",
			"Confirmed:bool:confirmed", "HasLatency:bool:has_latency", "LatencyBucket:uint8:latency_bucket",
			"Binding:validator.AttemptBinding:binding",
		}},
		{typeOf: reflect.TypeOf(AttemptBinding{}), fields: []string{
			"ClientID:connect.Id:client_id", "Active:bool:active", "FleetID:string:fleet_id", "Hotkey:string:hotkey",
			"Generation:uint64:generation", "UIDFound:bool:uid_found", "UID:uint16:uid",
		}},
	} {
		actual := []string{}
		for index := 0; index < row.typeOf.NumField(); index++ {
			field := row.typeOf.Field(index)
			actual = append(actual, field.Name+":"+field.Type.String()+":"+field.Tag.Get("json"))
		}
		if !reflect.DeepEqual(actual, row.fields) {
			t.Fatalf("%s fields changed without comparator review: %v", row.typeOf, actual)
		}
	}
	if actual := reflect.TypeOf(connect.Id{}); actual.Kind() != reflect.Array || actual.Len() != 16 || actual.Elem().Kind() != reflect.Uint8 {
		t.Fatal("custom JSON ID representation needs new equivalence review")
	}
}

// Comparing valid and fallback inputs cannot transfer or modify any slice.
func TestAttemptComparisonLeavesBothInputsOwned(t *testing.T) {
	t.Parallel()
	cut, _, _ := attemptComparisonRealCut(t)
	for _, malformed := range []bool{false, true} {
		left := cloneAttemptComparisonAssignments(cut.Records[7].Assignments)
		right := cloneAttemptComparisonAssignments(left)
		if malformed {
			left[1].Binding.Hotkey = string([]byte{0xff})
			right[1].Binding.Hotkey = string([]byte{0xfe})
		}
		leftBefore, rightBefore := cloneAttemptComparisonAssignments(left), cloneAttemptComparisonAssignments(right)
		if !attemptAssignmentsEqual(left, right) {
			t.Fatal("equivalent owned inputs compare unequal")
		}
		if !reflect.DeepEqual(left, leftBefore) || !reflect.DeepEqual(right, rightBefore) {
			t.Fatal("assignment comparison mutated its owner inputs")
		}
		right[0].AssignMessage[0] ^= 1
		if !reflect.DeepEqual(left, leftBefore) || attemptAssignmentsEqual(left, right) {
			t.Fatal("comparison retained authority across a detached caller mutation")
		}
	}
}

// Genuine failed I/O follows a signed first assignment and records a terminal
// without manufacturing a proof or lowering the configured M8 depth.
func TestAttemptComparisonRetainsRealFailedTerminal(t *testing.T) {
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
		t.Fatalf("real M8 transport failure did not reach its terminal: proof=%v error=%v", proof, err)
	}
	cut, err := ledger.BuildCut(attemptLedgerTestBoundary(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(cut.Records) != 2 || cut.Records[0].Disposition != AttemptDispositionPending ||
		cut.Records[1].Disposition != AttemptDispositionHopFailure || cut.Records[1].Proof != nil ||
		cut.Records[0].M != 8 || cut.Records[1].M != 8 {
		t.Fatal("failed trail census, depth or genuine terminal changed")
	}
	comparisons := 0
	work := attemptAssignmentComparisonWork{compared: func() { comparisons++ }}
	if err := verifyAttemptLedgerCutWithComparison(cut, key.Public().(ed25519.PublicKey), server.serverPublicKeys(), true, connect.VerifyVerifyMessageSignature, work); err != nil {
		t.Fatal(err)
	}
	if comparisons != 1 {
		t.Fatalf("failed terminal applicable comparisons=%d want1", comparisons)
	}
	if err := VerifyAttemptLedgerCut(cut, key.Public().(ed25519.PublicKey), server.serverPublicKeys()); err != nil {
		t.Fatal(err)
	}
}

// Incomplete prefixes remain visible as pending and cannot become public cuts.
func TestAttemptComparisonRetainsPendingAndContextChecks(t *testing.T) {
	t.Parallel()
	cut, key, keys := attemptComparisonRealCut(t)
	pending, terminal, err := attemptLifecycleWithComparison(cut.Records[:7], attemptAssignmentComparisonWork{})
	if err != nil || len(pending) != 1 || len(terminal) != 0 || !reflect.DeepEqual(pending[cut.Records[0].TrailID], cut.Records[6]) {
		t.Fatalf("pending lifecycle state changed: pending=%d terminal=%d error=%v", len(pending), len(terminal), err)
	}
	for _, change := range []struct {
		name string
		edit func(*AttemptRecord)
	}{
		{name: "nonce", edit: func(r *AttemptRecord) { r.ServerNonce[0] ^= 1 }},
		{name: "depth", edit: func(r *AttemptRecord) { r.M++ }},
		{name: "boundary", edit: func(r *AttemptRecord) { r.Boundary.EVMBlock++ }},
	} {
		changed := cloneAttemptLedgerCut(t, cut).Records[7]
		change.edit(&changed)
		if err := validateAttemptLifecycleRecord(pending, terminal, changed, 7); err == nil {
			t.Errorf("terminal %s drift bypassed pending context", change.name)
		}
	}
	partial := cloneAttemptLedgerCut(t, cut)
	partial.Records, partial.RecordCount, partial.LastSequence = partial.Records[:7], 7, 7
	partial.Root = partial.Records[6].RecordHash
	message, err := attemptCutSignatureMessage(partial)
	if err != nil {
		t.Fatal(err)
	}
	partial.Signature = ed25519.Sign(key, message)
	if err := VerifyAttemptLedgerCut(partial, key.Public().(ed25519.PublicKey), keys); err == nil || !strings.Contains(err.Error(), "unfinished trail") {
		t.Fatalf("genuinely signed unfinished cut was admitted: %v", err)
	}
}

// Every individual signature/hash is repaired so only lifecycle prefix equality
// can reject a changed binding in an intermediate pending record.
func TestAttemptComparisonRejectsResignedChangedPrefix(t *testing.T) {
	t.Parallel()
	original, key, keys := attemptComparisonRealCut(t)
	cut := cloneAttemptLedgerCut(t, original)
	cut.Records[3].Assignments[0].Binding.Generation++
	previous := cut.PriorRoot
	for index := range cut.Records {
		record := &cut.Records[index]
		record.PreviousHash = previous
		digest, err := attemptRecordHash(record)
		if err != nil {
			t.Fatal(err)
		}
		record.RecordHash = attemptHex32(digest)
		record.Signature = ed25519.Sign(key, attemptRecordSignatureMessage(digest))
		if err := VerifyAttemptRecord(record, cut.Identity, key.Public().(ed25519.PublicKey), keys); err != nil {
			t.Fatalf("mutation broke an earlier individual authentication boundary: %v", err)
		}
		previous = record.RecordHash
	}
	cut.Root = previous
	message, err := attemptCutSignatureMessage(cut)
	if err != nil {
		t.Fatal(err)
	}
	cut.Signature = ed25519.Sign(key, message)
	if err := VerifyAttemptLedgerCut(cut, key.Public().(ed25519.PublicKey), keys); err == nil || !strings.Contains(err.Error(), "does not extend its pending checkpoint") {
		t.Fatalf("re-signed changed prefix bypassed actual public lifecycle: %v", err)
	}
	if err := VerifyAttemptLedgerCut(original, key.Public().(ed25519.PublicKey), keys); err != nil {
		t.Fatal(err)
	}
}

// Public verifier calls retain their own cryptographic obligations after a
// successful comparison; there is no cross-call validation token.
func TestAttemptComparisonPublicCutRechecksMutation(t *testing.T) {
	t.Parallel()
	cut, key, keys := attemptComparisonRealCut(t)
	if err := VerifyAttemptLedgerCut(cut, key.Public().(ed25519.PublicKey), keys); err != nil {
		t.Fatal(err)
	}
	cut.Records[7].Signature[0] ^= 1
	if err := VerifyAttemptLedgerCut(cut, key.Public().(ed25519.PublicKey), keys); err == nil || !strings.Contains(err.Error(), "validator signature is invalid") {
		t.Fatalf("mutated post-success signature was reused: %v", err)
	}
}

// Distinct observers are invocation-owned and detached from previous results.
func TestAttemptComparisonObserverIsInvocationLocal(t *testing.T) {
	t.Parallel()
	first, second := 0, 0
	a := attemptAssignmentComparisonWork{compared: func() { first++ }}
	b := attemptAssignmentComparisonWork{compared: func() { second++ }}
	input := []AttemptAssignment{{Binding: AttemptBinding{Hotkey: string([]byte{0xff})}}}
	if !a.equal(input, input) || !b.equal(input, input) || !a.equal(input, input) {
		t.Fatal("standalone comparison changed its result")
	}
	if first != 2 || second != 1 {
		t.Fatalf("comparison observers crossed calls: %d/%d want2/1", first, second)
	}
}

// A rare fallback must execute both original serializations, not just report
// them or silently impose a stricter invalid-UTF8 equality rule.
func TestAttemptComparisonInvalidUTF8ReachesJSONBoundary(t *testing.T) {
	t.Parallel()
	serializations := 0
	work := attemptAssignmentComparisonWork{serialized: func() { serializations++ }}
	left := []AttemptAssignment{{Binding: AttemptBinding{FleetID: string([]byte{0xff})}}}
	right := []AttemptAssignment{{Binding: AttemptBinding{FleetID: string([]byte{0xfe})}}}
	if !work.equal(left, right) || serializations != 2 {
		t.Fatalf("normalization fallback skipped original JSON: equal=%t encodes=%d", oldAttemptComparisonJSON(left, right), serializations)
	}
}

// The full public, replay and append/store paths must reach the observed body.
// A detached alternate test-only verifier cannot satisfy these source edges.
func TestAttemptComparisonWrappersReachActualLifecycle(t *testing.T) {
	t.Parallel()
	for _, edge := range []struct{ file, function, callee string }{
		{file: "attempt_ledger.go", function: "VerifyAttemptLedgerCut", callee: "verifyAttemptLedgerCut"},
		{file: "attempt_ledger.go", function: "verifyAttemptLedgerCut", callee: "verifyAttemptLedgerCutWithAssignVerifier"},
		{file: "attempt_ledger.go", function: "verifyAttemptLedgerCutWithAssignVerifier", callee: "verifyAttemptLedgerCutWithComparison"},
		{file: "attempt_ledger.go", function: "verifyAttemptLedgerCutWithComparison", callee: "attemptLifecycleWithComparison"},
		{file: "attempt_ledger.go", function: "attemptLifecycleWithComparison", callee: "validateAttemptLifecycleRecordWithComparison"},
		{file: "attempt_ledger.go", function: "validateAttemptLifecycleRecord", callee: "validateAttemptLifecycleRecordWithComparison"},
		{file: "attempt_ledger.go", function: "validateAttemptLifecycleRecordWithComparison", callee: "equal"},
		{file: "attempt_ledger.go", function: "attemptAssignmentsEqual", callee: "equal"},
		{file: "attempt_comparison.go", function: "equal", callee: "marshal"},
		{file: "attempt_comparison.go", function: "marshal", callee: "Marshal"},
	} {
		parsed, err := parser.ParseFile(token.NewFileSet(), edge.file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name.Name != edge.function {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch callee := call.Fun.(type) {
				case *ast.Ident:
					found = found || callee.Name == edge.callee
				case *ast.SelectorExpr:
					found = found || callee.Sel.Name == edge.callee
				}
				return true
			})
		}
		if !found {
			t.Errorf("actual production edge missing: %s %s -> %s", edge.file, edge.function, edge.callee)
		}
	}
}
