//go:build linux || darwin

package validator

// Staged bytes are evidence, not an accepted cut. Full fetch-back must reject
// every missing/changed byte, bad attestation, open lifecycle and wrong proof
// projection without returning a partial census or signed header.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Failed results are uniformly empty even after real durable staging.
func assertAttemptCutV2SealTestFailure(t *testing.T, cut *AttemptCutV2, result AttemptCutV2ReplayResult, err error, contains string) {
	t.Helper()
	if err == nil || cut != nil || result != (AttemptCutV2ReplayResult{}) || contains != "" && !strings.Contains(err.Error(), contains) {
		t.Fatalf("failed sealing returned cut=%+v result=%+v error=%v, want %q", cut, result, err, contains)
	}
}

// A callback may durably store an object and still report failure. The
// orphan bytes remain inspectable, but no header or acceptance is returned.
func TestAttemptCutV2SealStageErrorsPreserveEvidence(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	for _, kind := range []string{AttemptStreamV2Records, AttemptStreamV2Proofs, "metadata"} {
		options, objects := newAttemptCutV2SealTestOptions(t, fixture)
		failure := errors.New("staged object acknowledgment failed")
		wrap := func(write AttemptStreamV2ObjectWriter) AttemptStreamV2ObjectWriter {
			return func(ctx context.Context, hash string, raw []byte) error {
				return errors.Join(write(ctx, hash, raw), failure)
			}
		}
		switch kind {
		case AttemptStreamV2Records:
			options.WriteRecords = wrap(options.WriteRecords)
		case AttemptStreamV2Proofs:
			options.WriteProofs = wrap(options.WriteProofs)
		case "metadata":
			options.WriteMetadata = wrap(options.WriteMetadata)
		}
		cut, result, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
		assertAttemptCutV2SealTestFailure(t, cut, result, err, failure.Error())
		if !errors.Is(err, failure) || objects.writes == 0 || objects.reads != 0 {
			t.Fatalf("%s stage failure lost evidence or reached acceptance: writes=%d reads=%d %v", kind, objects.writes, objects.reads, err)
		}
		if info, err := os.Lstat(options.ScratchDirectory); err != nil || !attemptStorePrivateDirectory(info) {
			t.Fatalf("%s failure erased owned scratch: %v", kind, err)
		}
	}
}

// Fetching by the requested hash is not proof that the returned data still
// matches it. Exact length, order, rows and terminal EOF all remain checked.
func TestAttemptCutV2SealRejectsChangedFetchedRecordBytes(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	for _, variation := range []string{"missing", "truncated", "extra", "reordered", "changed"} {
		options, objects := newAttemptCutV2SealTestOptions(t, fixture)
		open, changed := options.OpenData, false
		options.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			if changed || kind != AttemptStreamV2Records {
				return open(ctx, kind, hash, size)
			}
			changed = true
			if variation == "missing" {
				return nil, errors.New("missing staged data object")
			}
			raw := bytes.Clone(objects.dataKVs[kind][hash])
			switch variation {
			case "truncated":
				raw = raw[:len(raw)-1]
			case "extra":
				raw = append(raw, '\n')
			case "reordered":
				rows := bytes.SplitAfter(raw, []byte{'\n'})
				if len(rows) < 3 || len(rows[1]) == 0 {
					t.Fatal("record chunk did not contain two complete rows")
				}
				rows[0], rows[1] = rows[1], rows[0]
				raw = bytes.Join(rows, nil)
			case "changed":
				raw[0] = '['
			}
			return io.NopCloser(bytes.NewReader(raw)), nil
		}
		cut, result, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
		assertAttemptCutV2SealTestFailure(t, cut, result, err, "")
		if !changed || objects.writes == 0 || len(objects.dataKVs[AttemptStreamV2Proofs]) == 0 {
			t.Fatalf("%s did not reach full staged fetch-back", variation)
		}
	}
}

// The separate proof stream is fetched and authenticated even though its
// canonical expected projection was already observed in terminal records.
func TestAttemptCutV2SealRejectsChangedFetchedProofBytes(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	for _, variation := range []string{"missing", "truncated", "extra", "changed"} {
		options, objects := newAttemptCutV2SealTestOptions(t, fixture)
		open, changed := options.OpenData, false
		options.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			if kind != AttemptStreamV2Proofs {
				return open(ctx, kind, hash, size)
			}
			changed = true
			if variation == "missing" {
				return nil, errors.New("missing staged proof object")
			}
			raw := bytes.Clone(objects.dataKVs[kind][hash])
			switch variation {
			case "truncated":
				raw = raw[:len(raw)-1]
			case "extra":
				raw = append(raw, '\n')
			case "changed":
				raw[0] = '['
			}
			return io.NopCloser(bytes.NewReader(raw)), nil
		}
		cut, result, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
		assertAttemptCutV2SealTestFailure(t, cut, result, err, "")
		if !changed || objects.closes != len(objects.dataKVs[AttemptStreamV2Records]) {
			t.Fatalf("%s failure preceded complete record fetch-back", variation)
		}
	}
}

// Authenticated references do not excuse a missing, resized or wrong-hash
// manifest/page response from the public immutable object transport.
func TestAttemptCutV2SealRejectsChangedFetchedMetadata(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	for _, variation := range []string{"missing", "truncated", "suffix", "changed"} {
		options, objects := newAttemptCutV2SealTestOptions(t, fixture)
		read, reached := options.ReadMetadata, false
		options.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
			reached = true
			raw, err := read(ctx, hash, size)
			if err != nil {
				return nil, err
			}
			switch variation {
			case "missing":
				return nil, errors.New("metadata object missing")
			case "truncated":
				return raw[:len(raw)-1], nil
			case "suffix":
				return append(raw, '\n'), nil
			default:
				raw[0] = '['
				return raw, nil
			}
		}
		cut, result, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
		assertAttemptCutV2SealTestFailure(t, cut, result, err, "")
		if !reached || objects.writes == 0 || objects.closes != 0 {
			t.Fatalf("%s metadata control did not precede data work", variation)
		}
	}
}

// Even freshly hashed, signed outer metadata cannot substitute a different
// valid proof: proof order is exactly the completed record projection.
func TestAttemptCutV2PolicyReplayRejectsRehashedProofSubstitution(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 0)
	cut, records, proofs := attemptReplayV2TestCut(t, fixture.recordTs, fixture.key, nil, func(_ int, _ *AttemptStreamV2Chunk, raw *[]byte) {
		rows := bytes.SplitAfter(*raw, []byte{'\n'})
		if len(rows) != 3 || len(rows[2]) != 0 {
			t.Fatal("expected two genuine complete proof rows")
		}
		rows[0], rows[1] = rows[1], rows[0]
		*raw = bytes.Join(rows, nil)
	})
	cut.Context = fixture.expected
	bounds, _ := attemptReplayV2TestBounds()
	var err error
	cut.Signature, err = cut.Sign(fixture.key, bounds)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ReplayAttemptCutV2WithPolicy(context.Background(), cut, cut.Context, fixture.policy, bounds, attemptReplayV2TestOptions(t, records, proofs, fixture.server.serverPublicKeys()))
	if err == nil || !strings.Contains(err.Error(), "signed record projection") || result != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("freshly rehashed proof substitution result=%+v: %v", result, err)
	}
}

// Server-key authority is resolved by the full fetched-byte verifier, not
// assumed from the local disk ledger's validator-only canonical checks.
func TestAttemptCutV2SealRejectsUntrustedServerKeys(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 1)
	for _, variation := range []string{"missing", "wrong", "malformed"} {
		options, _ := newAttemptCutV2SealTestOptions(t, fixture)
		switch variation {
		case "missing":
			delete(options.ServerKeys, fixture.server.serverKeyId)
		case "wrong":
			options.ServerKeys[fixture.server.serverKeyId] = bytes.Repeat([]byte{0x71}, 32)
		case "malformed":
			options.ServerKeys[fixture.server.serverKeyId] = []byte{0x71}
		}
		cut, result, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
		assertAttemptCutV2SealTestFailure(t, cut, result, err, "")
	}
}

// Validator-resigned wrappers do not hide a forged server signature on a
// failed terminal. Its genuine failed-trail control is independently valid.
func TestAttemptCutV2SealRejectsForgedServerAssignment(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 1)
	ledger, err := NewDiskAttemptLedger(context.Background(), newAttemptLedgerDiskTestStateDir(t), fixture.expected.Identity, attemptLedgerDiskTestCoordinator, fixture.key, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	for _, record := range fixture.recordTs {
		owned, err := cloneAttemptRecord(record)
		if err != nil {
			t.Fatal(err)
		}
		owned.Assignments[0].AssignSignature[0] ^= 1
		if _, err := ledger.AppendContext(context.Background(), owned); err != nil {
			t.Fatalf("validator-only durable source unexpectedly rejected the intended server-signature fixture: %v", err)
		}
	}
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	cut, result, err := SealAttemptCutV2(context.Background(), ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	assertAttemptCutV2SealTestFailure(t, cut, result, err, "signature")
	if objects.reads == 0 || objects.writes == 0 {
		t.Fatal("forged source did not reach full independent fetch-back")
	}
}

// A valid complete trail cannot conceal an unfinished neighboring trail at
// the captured Head. The full record census must close every observed trail.
func TestAttemptCutV2SealRejectsOpenLifecycleTail(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 0)
	ledger, err := NewDiskAttemptLedger(context.Background(), newAttemptLedgerDiskTestStateDir(t), fixture.expected.Identity, attemptLedgerDiskTestCoordinator, fixture.key, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	for _, record := range fixture.recordTs[:9] {
		if _, err := ledger.AppendContext(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	cut, result, err := SealAttemptCutV2(context.Background(), ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	assertAttemptCutV2SealTestFailure(t, cut, result, err, "pending lifecycle")
	if objects.reads == 0 || objects.writes == 0 {
		t.Fatal("open lifecycle was not checked against staged data")
	}
}

// Independent count, row, total-data, chunk/page and scratch dimensions are
// enforced without truncating the full signed input or returning partial cuts.
func TestAttemptCutV2SealEnforcesExplicitResourceBounds(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 0)
	for _, variation := range []string{"record-count", "record-row", "proof-row", "data-bytes", "chunks", "pages", "trails", "scratch-files", "scratch-bytes", "header"} {
		bounds := fixture.bounds
		options, _ := newAttemptCutV2SealTestOptions(t, fixture)
		switch variation {
		case "record-count":
			bounds.Records.MaxItems = 15
			options.ReplayBounds.MaxTrails = 15
		case "record-row":
			options.ReplayBounds.MaxRecordBytes = 512
		case "proof-row":
			options.ReplayBounds.MaxProofBytes = 512
		case "data-bytes":
			bounds.Records.MaxDataBytes = bounds.Records.MaxChunkBytes
		case "chunks":
			bounds.Records.MaxChunks, bounds.Records.MaxPages = 1, 1
		case "pages":
			bounds.Records.MaxPages = 1
		case "trails":
			options.ReplayBounds.MaxTrails = 1
		case "scratch-files":
			options.ReplayBounds.MaxScratchFiles = 7
		case "scratch-bytes":
			options.ReplayBounds.MaxScratchBytes = attemptStoreMetadataReserve
		case "header":
			bounds.MaxHeaderBytes = 1024
		}
		cut, result, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, bounds, options)
		assertAttemptCutV2SealTestFailure(t, cut, result, err, "")
		if err == nil {
			t.Fatalf("%s bound was not reached", variation)
		}
	}
}

// Every data object is typed and canonical; the object sinks never receive
// an ordinary signed header that could be mistaken for completed acceptance.
func TestAttemptCutV2SealStagesNoSignedHeader(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	cut, _, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil || cut == nil {
		t.Fatal(err)
	}
	for _, raw := range objects.metadataKVs {
		var envelope struct {
			Schema string `json:"schema"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Schema == AttemptCutV2Schema || envelope.Schema == "" {
			t.Fatalf("metadata sink received a header/nonobject: %v", err)
		}
	}
	if _, err := os.Lstat(filepath.Join(options.ScratchDirectory, "replay")); err != nil {
		t.Fatalf("successful replay evidence was erased: %v", err)
	}
}
