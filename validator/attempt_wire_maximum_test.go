package validator

// Fixed-width wire accounting uses real signed trails, not zero-filled invalid
// proof shells. The empty deployment contribution is arithmetic only: every
// verified fixture carries a nonempty, round-trippable deployment identity.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"

	"github.com/urnetwork/connect"
)

// Counts JSON syntax independently of encoding/json and the runtime structs.
type attemptWireField struct {
	name  string
	width int
}

// Keys are the literal ASCII schema names; widths already include value syntax.
func attemptWireObjectWidth(fields ...attemptWireField) int {
	width := 2
	for index, field := range fields {
		if index > 0 {
			width++
		}
		width += len(field.name) + 3 + field.width
	}
	return width
}

// Accounts for brackets and commas, including the empty-array case.
func attemptWireArrayWidth(widths []int) int {
	width := 2
	for index, value := range widths {
		if index > 0 {
			width++
		}
		width += value
	}
	return width
}

// Positive fixture integers never exceed 20 decimal digits.
func attemptWireDecimalWidth(value uint64) int {
	width := 1
	for value >= 10 {
		value /= 10
		width++
	}
	return width
}

// The bounded binary sizes here cannot overflow the base64 rounding operation.
func attemptWireBase64Width(size int) int {
	return 2 + 4*((size+2)/3)
}

// Active bindings maximize compatible generation/UID widths; false inactive
// bindings must instead carry zero identities, generation and UID.
func attemptWireAssignmentWidth(prefix int, pending bool) int {
	trailWidths := make([]int, prefix)
	for index := range trailWidths {
		trailWidths[index] = 38
	}
	confirmationWidth, bucketWidth := 4, 2
	if pending {
		confirmationWidth, bucketWidth = 5, 1
	}
	bindingWidth := attemptWireObjectWidth(
		attemptWireField{name: "client_id", width: 38},
		attemptWireField{name: "active", width: 4},
		attemptWireField{name: "fleet_id", width: 68},
		attemptWireField{name: "hotkey", width: 68},
		attemptWireField{name: "generation", width: 20},
		attemptWireField{name: "uid_found", width: 4},
		attemptWireField{name: "uid", width: 5},
	)
	return attemptWireObjectWidth(
		attemptWireField{name: "trail", width: attemptWireArrayWidth(trailWidths)},
		attemptWireField{name: "next_hop", width: 38},
		attemptWireField{name: "server_key_id", width: 3},
		attemptWireField{name: "assign_message", width: attemptWireBase64Width(103 + 16*(prefix+1))},
		attemptWireField{name: "assign_signature", width: 90},
		attemptWireField{name: "confirmed", width: confirmationWidth},
		attemptWireField{name: "has_latency", width: confirmationWidth},
		attemptWireField{name: "latency_bucket", width: bucketWidth},
		attemptWireField{name: "binding", width: bindingWidth},
	)
}

// Hash arrays are 32 three-digit JSON numbers, not base64 byte slices. The
// FINAL contains all hops, while coverage remains the exact non-seed count.
func attemptWireProofWidth(depth int) int {
	hopWidths := make([]int, depth)
	for index := range hopWidths {
		hopWidths[index] = attemptWireObjectWidth(
			attemptWireField{name: "client_id", width: 38},
			attemptWireField{name: "time_ms", width: 20},
			attemptWireField{name: "egress_ip_hash", width: 2 + 32*3 + 31},
		)
	}
	return attemptWireObjectWidth(
		attemptWireField{name: "v", width: 1},
		attemptWireField{name: "epoch", width: 20},
		attemptWireField{name: "trail_id", width: 38},
		attemptWireField{name: "server_nonce", width: 46},
		attemptWireField{name: "vpk", width: 46},
		attemptWireField{name: "m", width: attemptWireDecimalWidth(uint64(depth))},
		attemptWireField{name: "hops", width: attemptWireArrayWidth(hopWidths)},
		attemptWireField{name: "server_key_id", width: 3},
		attemptWireField{name: "final_sig", width: 90},
		attemptWireField{name: "verifier_sig", width: 90},
		attemptWireField{name: "final_digest", width: 46},
		attemptWireField{name: "vpk_sig", width: 90},
		attemptWireField{name: "coverage", width: attemptWireDecimalWidth(uint64(depth - 1))},
		attemptWireField{name: "path_id", width: 46},
		attemptWireField{name: "complete_time_ms", width: 20},
	)
}

// Deployment content contributes exactly once to each record, including every
// pending checkpoint. Proof is omitted entirely until the completed terminal.
func attemptWireRecordWidth(depth, assignmentCount, deploymentContentWidth int, complete bool) int {
	identityWidth := attemptWireObjectWidth(
		attemptWireField{name: "deployment_id", width: 2 + deploymentContentWidth},
		attemptWireField{name: "chain_id", width: 20},
		attemptWireField{name: "genesis_hash", width: 68},
		attemptWireField{name: "netuid", width: 5},
		attemptWireField{name: "validator_id", width: 20},
		attemptWireField{name: "validator_uid", width: 5},
		attemptWireField{name: "no_id", width: 20},
		attemptWireField{name: "validator_vpk", width: 68},
	)
	boundaryWidth := attemptWireObjectWidth(
		attemptWireField{name: "settlement_epoch", width: 20},
		attemptWireField{name: "evm_block", width: 20},
		attemptWireField{name: "evm_block_hash", width: 68},
	)
	assignmentWidths := make([]int, assignmentCount)
	for index := range assignmentWidths {
		assignmentWidths[index] = attemptWireAssignmentWidth(index+1, !complete && index == assignmentCount-1)
	}
	dispositionWidth := 9
	if complete {
		dispositionWidth = 10
	}
	fields := []attemptWireField{
		{name: "schema", width: 39},
		{name: "identity", width: identityWidth},
		{name: "sequence", width: 20},
		{name: "previous_hash", width: 68},
		{name: "boundary", width: boundaryWidth},
		{name: "trail_id", width: 38},
		{name: "server_nonce", width: 46},
		{name: "vpk", width: 46},
		{name: "M", width: attemptWireDecimalWidth(uint64(depth))},
		{name: "assignments", width: attemptWireArrayWidth(assignmentWidths)},
		{name: "disposition", width: dispositionWidth},
	}
	if complete {
		fields = append(fields, attemptWireField{name: "proof", width: attemptWireProofWidth(depth)})
	}
	fields = append(fields, attemptWireField{name: "record_hash", width: 68}, attemptWireField{name: "signature", width: 90})
	return attemptWireObjectWidth(fields...)
}

// Synthetic keys sign actual canonical ASSIGN/EXTEND/FINAL bytes and every
// hash-linked record. Sequence leaves one representable successor cursor;
// all timestamps, epoch and other compatible uint64 fields retain 20 digits.
func attemptWireMaximumFixture(t *testing.T, depth int, deploymentID string) (*AttemptLedgerCut, ed25519.PublicKey, map[byte]ed25519.PublicKey) {
	t.Helper()
	if depth < 4 || depth > 16 || deploymentID == "" {
		t.Fatal("wire fixture requires a valid bounded depth and nonempty deployment")
	}
	validatorKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x11}, ed25519.SeedSize))
	serverKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x22}, ed25519.SeedSize))
	vpk := validatorKey.Public().(ed25519.PublicKey)
	serverKeys := map[byte]ed25519.PublicKey{255: serverKey.Public().(ed25519.PublicKey)}
	maximum := ^uint64(0)
	identity := AttemptLedgerIdentity{
		DeploymentID: deploymentID, ChainID: maximum, GenesisHash: "0x" + strings.Repeat("dd", 32),
		Netuid: ^uint16(0), ValidatorID: maximum, ValidatorUID: ^uint16(0), NoID: maximum,
		ValidatorVPK: attemptHex32(*(*[32]byte)(vpk)),
	}
	boundary := AttemptBoundary{SettlementEpoch: maximum, EVMBlock: maximum, EVMBlockHash: "0x" + strings.Repeat("ee", 32)}
	trailID := connect.Id{0: 0xa5, 15: 0xa5}
	nonce := bytes.Repeat([]byte{0x33}, 32)
	hops := make([]connect.VerifyProofHop, depth)
	trailIDs := make([]connect.Id, depth)
	for index := range hops {
		trailIDs[index] = connect.Id{0: 0xff, 15: byte(index + 1)}
		var egressHash [32]byte
		for byteIndex := range egressHash {
			egressHash[byteIndex] = byte(255 - index)
		}
		hops[index] = connect.VerifyProofHop{ClientId: trailIDs[index], TimeMs: maximum - uint64(depth-1-index), EgressIpHash: egressHash}
	}
	finalMessage, err := connect.BuildVerifyFinalMessage(255, trailID, nonce, vpk, byte(depth), hops)
	if err != nil || len(finalMessage) != 102+56*depth {
		t.Fatalf("canonical FINAL width: %d, %v", len(finalMessage), err)
	}
	extendMessage, err := connect.BuildVerifyExtendMessage(trailID, nonce, vpk, byte(depth), trailIDs)
	if err != nil || len(extendMessage) != 102+16*depth {
		t.Fatalf("canonical EXTEND width: %d, %v", len(extendMessage), err)
	}
	finalDigest := connect.VerifyFinalDigest(finalMessage)
	pathID := TrailPathId(trailID, vpk, 255)
	proof := &ProofRecord{
		Version: 1, Epoch: maximum, TrailId: trailID, ServerNonce: nonce, Vpk: vpk, M: depth, Hops: hops,
		ServerKeyId: 255, FinalSig: ed25519.Sign(serverKey, finalMessage), VerifierSig: ed25519.Sign(validatorKey, extendMessage),
		FinalDigest: finalDigest[:], VpkSig: ed25519.Sign(validatorKey, finalMessage), Coverage: uint64(depth - 1),
		PathId: pathID[:], CompleteTimeMs: maximum,
	}
	if err := VerifyProofRecord(proof, vpk, serverKeys, depth); err != nil {
		t.Fatalf("maximum-width signed proof: %v", err)
	}
	assignments := make([]AttemptAssignment, depth-1)
	for index := range assignments {
		message, err := connect.BuildVerifyAssignMessage(255, trailID, nonce, vpk, byte(depth), trailIDs[:index+2])
		if err != nil || len(message) != 103+16*(index+2) {
			t.Fatalf("canonical ASSIGN %d width: %d, %v", index, len(message), err)
		}
		assignments[index] = AttemptAssignment{
			Trail: append([]connect.Id(nil), trailIDs[:index+1]...), NextHop: trailIDs[index+1], ServerKeyID: 255,
			AssignMessage: message, AssignSignature: ed25519.Sign(serverKey, message), Confirmed: true, HasLatency: true, LatencyBucket: 30,
			Binding: AttemptBinding{ClientID: trailIDs[index+1], Active: true, FleetID: "0x" + strings.Repeat("fe", 32), Hotkey: "0x" + strings.Repeat("fd", 32), Generation: maximum, UIDFound: true, UID: ^uint16(0)},
		}
	}
	priorRoot := "0x" + strings.Repeat("ab", 32)
	previousHash := priorRoot
	records := make([]AttemptRecord, depth)
	for index := range records {
		complete := index == depth-1
		assignmentCount := index + 1
		if complete {
			assignmentCount = depth - 1
		}
		record := AttemptRecord{
			Schema: attemptLedgerRecordSchema, Identity: identity, Sequence: maximum - uint64(depth) + uint64(index), PreviousHash: previousHash,
			Boundary: boundary, TrailID: trailID, ServerNonce: nonce, VPK: vpk, M: depth,
			Assignments: append([]AttemptAssignment(nil), assignments[:assignmentCount]...), Disposition: AttemptDispositionPending,
		}
		if complete {
			record.Disposition, record.Proof = AttemptDispositionComplete, proof
		} else {
			last := &record.Assignments[assignmentCount-1]
			last.Confirmed, last.HasLatency, last.LatencyBucket = false, false, 0
		}
		digest, err := attemptRecordHash(&record)
		if err != nil {
			t.Fatal(err)
		}
		record.RecordHash = attemptHex32(digest)
		record.Signature = ed25519.Sign(validatorKey, attemptRecordSignatureMessage(digest))
		if err := VerifyAttemptRecord(&record, identity, vpk, serverKeys); err != nil {
			t.Fatalf("maximum-width signed checkpoint %d: %v", index, err)
		}
		records[index], previousHash = record, record.RecordHash
	}
	cut := &AttemptLedgerCut{
		Schema: attemptLedgerCutSchema, Identity: identity, Boundary: boundary,
		FirstSequence: records[0].Sequence, EgressFirstSequence: records[0].Sequence, LastSequence: records[depth-1].Sequence,
		RecordCount: uint64(depth), PriorRoot: priorRoot, Root: previousHash, Records: records,
	}
	message, err := attemptCutSignatureMessage(cut)
	if err != nil {
		t.Fatal(err)
	}
	cut.Signature = ed25519.Sign(validatorKey, message)
	if err := VerifyAttemptLedgerCut(cut, vpk, serverKeys); err != nil {
		t.Fatalf("maximum-width complete lifecycle/cursor control: %v", err)
	}
	return cut, vpk, serverKeys
}

// Every schema component and whole record must match the independent width
// model. Canonical round trips preserve deployment identity and signatures.
func attemptWireCheckMaximum(t *testing.T, depth int, deploymentID string, deploymentContentWidth, recordBase, proofJSONL, trailJSONLBase int) {
	t.Helper()
	cut, vpk, serverKeys := attemptWireMaximumFixture(t, depth, deploymentID)
	proof := cut.Records[depth-1].Proof
	proofBytes, err := json.Marshal(proof)
	if err != nil || len(proofBytes) != attemptWireProofWidth(depth) || len(proofBytes)+1 != proofJSONL {
		t.Fatalf("M%d proof JSON/JSONL width: %d, want %d/%d: %v", depth, len(proofBytes), attemptWireProofWidth(depth), proofJSONL, err)
	}
	var restoredProof ProofRecord
	if err := json.Unmarshal(proofBytes, &restoredProof); err != nil {
		t.Fatal(err)
	}
	if err := VerifyProofRecord(&restoredProof, vpk, serverKeys, depth); err != nil {
		t.Fatalf("M%d round-trip proof: %v", depth, err)
	}
	total, assignmentCopies := 0, 0
	pendingJSONL := make([]int, 0, depth-1)
	for index, record := range cut.Records {
		complete := index == depth-1
		encoded, err := json.Marshal(record)
		wantWidth := attemptWireRecordWidth(depth, len(record.Assignments), deploymentContentWidth, complete)
		if err != nil || len(encoded) != wantWidth {
			t.Fatalf("M%d checkpoint %d JSON width=%d want=%d: %v", depth, index, len(encoded), wantWidth, err)
		}
		if complete && len(encoded) != recordBase+deploymentContentWidth {
			t.Fatalf("M%d complete record base=%d want=%d", depth, len(encoded)-deploymentContentWidth, recordBase)
		}
		for assignmentIndex, assignment := range record.Assignments {
			assignmentBytes, err := json.Marshal(assignment)
			wantAssignment := attemptWireAssignmentWidth(assignmentIndex+1, !complete && assignmentIndex == len(record.Assignments)-1)
			if err != nil || len(assignmentBytes) != wantAssignment {
				t.Fatalf("M%d checkpoint %d assignment %d width=%d want=%d: %v", depth, index, assignmentIndex, len(assignmentBytes), wantAssignment, err)
			}
		}
		var restored AttemptRecord
		if err := json.Unmarshal(encoded, &restored); err != nil || restored.Identity.DeploymentID != deploymentID {
			t.Fatalf("M%d deployment identity did not round trip: %v", depth, err)
		}
		canonical, err := json.Marshal(restored)
		if err != nil || !bytes.Equal(encoded, canonical) {
			t.Fatalf("M%d checkpoint %d is not canonical after round trip: %v", depth, index, err)
		}
		if err := VerifyAttemptRecord(&restored, cut.Identity, vpk, serverKeys); err != nil {
			t.Fatalf("M%d round-trip signed checkpoint %d: %v", depth, index, err)
		}
		total += len(encoded) + 1
		assignmentCopies += len(record.Assignments)
		if !complete {
			pendingJSONL = append(pendingJSONL, len(encoded)+1)
		}
	}
	if total != trailJSONLBase+depth*deploymentContentWidth || assignmentCopies != (depth-1)*(depth+2)/2 {
		t.Fatalf("M%d whole trail JSONL=%d want=%d, assignment copies=%d", depth, total, trailJSONLBase+depth*deploymentContentWidth, assignmentCopies)
	}
	t.Logf("M=%d deployment_raw=%d deployment_json_content=%d proof_json=%d proof_jsonl=%d record_json=%d record_jsonl=%d pending_jsonl=%v trail_jsonl=%d records=%d assignment_copies=%d", depth, len(deploymentID), deploymentContentWidth, len(proofBytes), len(proofBytes)+1, recordBase+deploymentContentWidth, recordBase+deploymentContentWidth+1, pendingJSONL, total, len(cut.Records), assignmentCopies)
}

// Global minimum depth still retains all three pending checkpoints.
func TestAttemptWireMaximumM4(t *testing.T) {
	attemptWireCheckMaximum(t, 4, "maximum", 7, 5263, 1643, 13441)
}

// Default depth has seven pending records plus one completed proof record.
func TestAttemptWireMaximumM8(t *testing.T) {
	attemptWireCheckMaximum(t, 8, "maximum", 7, 10417, 2567, 43321)
}

// The globally supported upper depth exceeds an M8-sized record/proof budget.
func TestAttemptWireMaximumM16(t *testing.T) {
	attemptWireCheckMaximum(t, 16, "maximum", 7, 23628, 4417, 167879)
}

// Valid UTF-8 includes six-byte control/HTML/line-separator escapes. Each
// signed pending record repeats the escaped identity, not just the terminal.
func TestAttemptWireMaximumEscapedDeployment(t *testing.T) {
	cases := []struct {
		deploymentID string
		contentWidth int
	}{
		{deploymentID: "\x00", contentWidth: 6},
		{deploymentID: strings.Repeat("\x00", 64), contentWidth: 6 * 64},
		{deploymentID: "\"\\\n\r\t\b\f<>&\u2028\u2029é", contentWidth: 7*2 + 5*6 + 2},
	}
	for _, testCase := range cases {
		encoded, err := json.Marshal(testCase.deploymentID)
		if err != nil || len(encoded) != testCase.contentWidth+2 || bytes.ContainsAny(encoded, "\n\r\x00") {
			t.Fatalf("deployment escape width=%d want=%d: %v", len(encoded), testCase.contentWidth+2, err)
		}
		attemptWireCheckMaximum(t, 16, testCase.deploymentID, testCase.contentWidth, 23628, 4417, 167879)
	}
}

// Out-of-range JSON integers must not silently wrap to a small compatible
// value. Valid maximum-width integers are separately verified above.
func TestAttemptWireMaximumNumericRanges(t *testing.T) {
	cases := []struct {
		name    string
		encoded string
		value   any
	}{
		{name: "chain ID uint64", encoded: `{"chain_id":18446744073709551616}`, value: &AttemptLedgerIdentity{}},
		{name: "netuid uint16", encoded: `{"netuid":65536}`, value: &AttemptLedgerIdentity{}},
		{name: "validator UID uint16", encoded: `{"validator_uid":65536}`, value: &AttemptLedgerIdentity{}},
		{name: "sequence uint64", encoded: `{"sequence":18446744073709551616}`, value: &AttemptRecord{}},
		{name: "settlement epoch uint64", encoded: `{"settlement_epoch":18446744073709551616}`, value: &AttemptBoundary{}},
		{name: "generation uint64", encoded: `{"generation":18446744073709551616}`, value: &AttemptBinding{}},
		{name: "binding UID uint16", encoded: `{"uid":65536}`, value: &AttemptBinding{}},
		{name: "server key uint8", encoded: `{"server_key_id":256}`, value: &AttemptAssignment{}},
		{name: "latency bucket uint8", encoded: `{"latency_bucket":256}`, value: &AttemptAssignment{}},
		{name: "proof completion uint64", encoded: `{"complete_time_ms":18446744073709551616}`, value: &ProofRecord{}},
		{name: "hop time uint64", encoded: `{"time_ms":18446744073709551616}`, value: &connect.VerifyProofHop{}},
	}
	for _, testCase := range cases {
		if err := json.Unmarshal([]byte(testCase.encoded), testCase.value); err == nil {
			t.Errorf("%s overflow was accepted", testCase.name)
		}
	}
}
