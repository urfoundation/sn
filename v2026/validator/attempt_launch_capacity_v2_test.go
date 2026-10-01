// Canonical M8 wire sizing uses genuine engine-produced checkpoints and full
// signature verification. These are profile bounds, not a global M16 limit.
package validator

import (
	"crypto/ed25519"
	"encoding/json"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

// The ordinary engine emits seven increasing checkpoints and one terminal
// record; the derived proof projection duplicates only the terminal proof.
func attemptLaunchCapacityV2RecordsTest(t *testing.T) ([]AttemptRecord, ed25519.PrivateKey, ed25519.PrivateKey) {
	t.Helper()
	server, validatorKey, clientId := newMockVerifyServer(t, 12)
	stateDir := t.TempDir()
	store, err := NewProofStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientId, 8, store)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	t.Cleanup(func() {
		if err := ledger.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := engine.RunTrail(t.Context()); err != nil {
		t.Fatal(err)
	}
	records, err := ledger.RecordsAfter(0)
	if err != nil || len(records) != 8 || records[7].Disposition != AttemptDispositionComplete {
		t.Fatalf("actual M8 engine did not produce its exact durable census: %v", err)
	}
	return records, validatorKey, server.serverKey
}

// A real engine sample is reported separately from the all-width upper bound;
// random fixed-width signatures never determine admission or test scheduling.
func TestAttemptLaunchV2CanonicalM8EngineRowsFitProfile(t *testing.T) {
	records, validatorKey, serverKey := attemptLaunchCapacityV2RecordsTest(t)
	var recordBytes int
	for index := range records {
		record := &records[index]
		if err := VerifyAttemptRecord(record, record.Identity, validatorKey.Public().(ed25519.PublicKey), map[byte]ed25519.PublicKey{7: serverKey.Public().(ed25519.PublicKey)}); err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(record)
		if err != nil || len(wire)+1 > 16*1024 {
			t.Fatalf("actual engine record %d exceeds profile row allowance: %v", index, err)
		}
		recordBytes += len(wire) + 1
	}
	proof, err := json.Marshal(records[7].Proof)
	if err != nil || len(proof)+1 > 4*1024 {
		t.Fatalf("actual engine proof exceeds profile row allowance: %v", err)
	}
	t.Logf("actual signed M8 sample: eight record rows=%d bytes, projected proof row=%d bytes", recordBytes, len(proof)+1)
}

// Every unbounded numeric field is widened to uint64/uint16 maximum and every
// numeric egress-hash octet to 255. Genuine re-signing verifies the exact byte
// shape, not the truth of these deliberately impossible live-chain heights.
func TestAttemptLaunchV2CanonicalM8MaximumWidthRowsFitProfile(t *testing.T) {
	records, validatorKey, serverKey := attemptLaunchCapacityV2RecordsTest(t)
	var totalBytes int
	wantRows := []int{1821, 2669, 3576, 4542, 5571, 6659, 7806, 10381}
	for index := range records {
		record := &records[index]
		record.Identity.DeploymentID = "ur-subnet-testnet-v1"
		record.Identity.ChainID, record.Identity.Netuid = 945, 521
		record.Identity.ValidatorID, record.Identity.NoID, record.Identity.ValidatorUID = 2, 2, ^uint16(0)
		record.Sequence = ^uint64(0)
		record.Boundary.SettlementEpoch, record.Boundary.EVMBlock = ^uint64(0), ^uint64(0)
		for assignmentIndex := range record.Assignments {
			assignment := &record.Assignments[assignmentIndex]
			assignment.ServerKeyID = 255
			assignment.Binding.Generation, assignment.Binding.UID = ^uint64(0), ^uint16(0)
			if assignment.HasLatency {
				assignment.LatencyBucket = statsLatencyBuckets - 1
			}
			walked := append(append([]connect.Id(nil), assignment.Trail...), assignment.NextHop)
			message, err := connect.BuildVerifyAssignMessage(assignment.ServerKeyID, record.TrailID, record.ServerNonce, record.VPK, byte(record.M), walked)
			if err != nil {
				t.Fatal(err)
			}
			if len(message) != len(connect.VerifyCtx)+84+16*len(walked) {
				t.Fatal("canonical assignment acquired an unaccounted variable field")
			}
			assignment.AssignMessage, assignment.AssignSignature = message, ed25519.Sign(serverKey, message)
		}
		if proof := record.Proof; proof != nil {
			proof.Epoch, proof.CompleteTimeMs, proof.ServerKeyId = ^uint64(0), ^uint64(0), 255
			for hopIndex := range proof.Hops {
				proof.Hops[hopIndex].TimeMs = ^uint64(0)
				for octet := range proof.Hops[hopIndex].EgressIpHash {
					proof.Hops[hopIndex].EgressIpHash[octet] = 255
				}
			}
			path := TrailPathId(proof.TrailId, proof.Vpk, proof.ServerKeyId)
			proof.PathId = append([]byte(nil), path[:]...)
			resignAttemptRecordProof(t, record, validatorKey, serverKey)
		} else {
			hash, err := attemptRecordHash(record)
			if err != nil {
				t.Fatal(err)
			}
			record.RecordHash, record.Signature = attemptHex32(hash), ed25519.Sign(validatorKey, attemptRecordSignatureMessage(hash))
		}
		if err := VerifyAttemptRecord(record, record.Identity, validatorKey.Public().(ed25519.PublicKey), map[byte]ed25519.PublicKey{255: serverKey.Public().(ed25519.PublicKey)}); err != nil {
			t.Fatalf("maximum-width record %d is not cryptographically valid: %v", index, err)
		}
		wire, err := json.Marshal(record)
		if err != nil || len(wire)+1 != wantRows[index] || len(wire)+1 > 16*1024 {
			t.Fatalf("canonical record %d row=%d, want %d: %v", index, len(wire)+1, wantRows[index], err)
		}
		totalBytes += len(wire) + 1
	}
	proof, err := json.Marshal(records[7].Proof)
	if err != nil || len(proof)+1 != 2567 || totalBytes != 43025 {
		t.Fatalf("canonical complete-trail bound changed: records=%d proof=%d: %v", totalBytes, len(proof)+1, err)
	}
}
