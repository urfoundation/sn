//go:build linux || darwin

package validator

// Replay fixtures use the real trail engine and production signature formats.
// Only content-addressed transport is in memory; the replay's lifecycle/proof
// index is the actual bounded private disk backend, not a mocked verdict.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/protocol"
)

// Test artifacts retain exact data and metadata hashes independently.
type attemptReplayV2TestStream struct {
	reference AttemptStreamV2Reference
	metadata  map[string][]byte
	data      map[string][]byte
	chunks    []AttemptStreamV2Chunk
}

// Small finite budgets exercise public data paths without changing defaults.
func attemptReplayV2TestBounds() (AttemptCutV2Bounds, AttemptCutV2ReplayBounds) {
	stream := AttemptStreamV2Bounds{MaxDataBytes: 8 * 1024 * 1024, MaxItems: 256, MaxChunkBytes: 64 * 1024, MaxChunks: 128, MaxPages: 64, MaxPageBytes: 4096, MaxDescriptorsPerPage: 2, MaxManifestBytes: 1024}
	return AttemptCutV2Bounds{MaxHeaderBytes: 16 * 1024, Records: stream, Proofs: stream}, AttemptCutV2ReplayBounds{MaxRecordBytes: 32 * 1024, MaxProofBytes: 8 * 1024, MaxTrails: 32, MaxScratchBytes: 64 * 1024 * 1024, MaxScratchFiles: 256}
}

// Groups complete rows into two-row chunks and two-descriptor pages. Mutation
// hooks precede every metadata hash and cut signature, so negative tests can
// reach the actual byte/lifecycle/projection checks with fresh outer hashes.
func attemptReplayV2TestStreamBuild(t *testing.T, kind string, rows [][]byte, sequences []uint64, bounds AttemptStreamV2Bounds, edit func(int, *AttemptStreamV2Chunk, *[]byte)) attemptReplayV2TestStream {
	t.Helper()
	stream := attemptReplayV2TestStream{reference: AttemptStreamV2Reference{ManifestHash: zeroAttemptHash()}, metadata: map[string][]byte{}, data: map[string][]byte{}}
	if len(rows) != len(sequences) {
		t.Fatal("test rows and sequence counts differ")
	}
	if len(rows) == 0 {
		return stream
	}
	var itemCount, dataBytes uint64
	for first := 0; first < len(rows); first += 2 {
		last := min(first+2, len(rows))
		raw := bytes.Join(rows[first:last], nil)
		chunk := AttemptStreamV2Chunk{Index: uint64(len(stream.chunks)), FirstSequence: sequences[first], LastSequence: sequences[last-1], ItemCount: uint64(last - first)}
		if edit != nil {
			edit(len(stream.chunks), &chunk, &raw)
		}
		chunk.DataBytes, chunk.ContentHash = uint64(len(raw)), attemptHex32(sha256.Sum256(raw))
		stream.data[chunk.ContentHash] = bytes.Clone(raw)
		stream.chunks = append(stream.chunks, chunk)
		itemCount += chunk.ItemCount
		dataBytes += chunk.DataBytes
	}
	pageCount := (len(stream.chunks) + 1) / 2
	nextHash, nextBytes := zeroAttemptHash(), uint64(0)
	for pageIndex := pageCount - 1; pageIndex >= 0; pageIndex-- {
		first := pageIndex * 2
		page := AttemptStreamV2Page{Schema: AttemptStreamV2PageSchema, Kind: kind, Index: uint64(pageIndex), Chunks: stream.chunks[first:min(first+2, len(stream.chunks))], NextPageHash: nextHash, NextPageBytes: nextBytes}
		raw, err := page.CanonicalJSON(bounds)
		if err != nil {
			t.Fatal(err)
		}
		nextHash, nextBytes = attemptHex32(sha256.Sum256(raw)), uint64(len(raw))
		stream.metadata[nextHash] = raw
	}
	manifest := AttemptStreamV2Manifest{Schema: AttemptStreamV2Schema, Kind: kind, ItemCount: itemCount, DataBytes: dataBytes, ChunkCount: uint64(len(stream.chunks)), PageCount: uint64(pageCount), FirstPageHash: nextHash, FirstPageBytes: nextBytes}
	raw, reference, err := manifest.CanonicalJSON(bounds)
	if err != nil {
		t.Fatal(err)
	}
	stream.reference = reference
	stream.metadata[reference.ManifestHash] = raw
	return stream
}

// Captures complete real M8 trails and the operator's actual signing key ring.
func attemptReplayV2TestRecords(t *testing.T, trails int) ([]AttemptRecord, ed25519.PrivateKey, map[byte]ed25519.PublicKey) {
	t.Helper()
	server, key, clientID := newMockVerifyServer(t, 16)
	engine, stats, _ := newTestEngine(t, server, key, clientID, 8, nil)
	generation := uint64(1)
	stateDir := filepath.Join(t.TempDir(), "private-state")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	for index := 0; index < trails; index++ {
		if _, err := engine.RunTrail(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	records, err := ledger.RecordsAfter(0)
	if err != nil || len(records) != trails*8 {
		t.Fatalf("real trail record count = %d: %v", len(records), err)
	}
	return records, key, server.serverPublicKeys()
}

// Builds public archives from exact signed rows; its proof data is the same
// canonical projection produced by the existing proof store.
func attemptReplayV2TestCut(t *testing.T, records []AttemptRecord, key ed25519.PrivateKey, editRecords, editProofs func(int, *AttemptStreamV2Chunk, *[]byte)) (AttemptCutV2, attemptReplayV2TestStream, attemptReplayV2TestStream) {
	t.Helper()
	if len(records) == 0 {
		t.Fatal("use an explicit empty-cut fixture")
	}
	bounds, _ := attemptReplayV2TestBounds()
	var recordRows, proofRows [][]byte
	var recordSequences, proofSequences []uint64
	var failed uint64
	for _, record := range records {
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		recordRows = append(recordRows, append(raw, '\n'))
		recordSequences = append(recordSequences, record.Sequence)
		if record.Disposition == AttemptDispositionComplete {
			raw, err := json.Marshal(record.Proof)
			if err != nil {
				t.Fatal(err)
			}
			proofRows = append(proofRows, append(raw, '\n'))
			proofSequences = append(proofSequences, record.Sequence)
		} else if record.Disposition != AttemptDispositionPending {
			failed++
		}
	}
	recordStream := attemptReplayV2TestStreamBuild(t, AttemptStreamV2Records, recordRows, recordSequences, bounds.Records, editRecords)
	proofStream := attemptReplayV2TestStreamBuild(t, AttemptStreamV2Proofs, proofRows, proofSequences, bounds.Proofs, editProofs)
	first, last := records[0], records[len(records)-1]
	genesis, err := canonicalAttemptHex32("test genesis", first.Identity.GenesisHash, false)
	if err != nil {
		t.Fatal(err)
	}
	domain := protocol.ValidatorEvidenceDomain{ChainID: first.Identity.ChainID, GenesisHash: genesis, Netuid: first.Identity.Netuid, Coordinator: [20]byte{0x11}, SettlementVault: [20]byte{0x12}, DeploymentIDHash: sha256.Sum256([]byte(first.Identity.DeploymentID)), PolicyHash: [32]byte{0x13}, ActivationEpoch: first.Boundary.SettlementEpoch, ActivationHash: [32]byte{0x14}}
	cut := AttemptCutV2{Schema: AttemptCutV2Schema, Context: AttemptCutV2Context{Identity: first.Identity, Activation: AttemptCutV2Activation{Domain: domain, Hotkey: [32]byte{0x15}, FirstSequence: first.Sequence, PriorRoot: first.PreviousHash}, Boundary: last.Boundary, FirstSequence: first.Sequence, EgressFirstSequence: first.Sequence, EgressGeneration: 1, PriorRoot: first.PreviousHash}, LastSequence: last.Sequence, RecordCount: uint64(len(records)), CompleteCount: uint64(len(proofRows)), FailedCount: failed, Root: last.RecordHash, Records: recordStream.reference, Proofs: proofStream.reference}
	cut.Signature, err = cut.Sign(key, bounds)
	if err != nil {
		t.Fatal(err)
	}
	return cut, recordStream, proofStream
}

// Each replay gets a new namespace and owned transport bytes. No private
// fixture writer changes the production verifier or the expected cut context.
func attemptReplayV2TestOptions(t *testing.T, records, proofs attemptReplayV2TestStream, keys map[byte]ed25519.PublicKey) AttemptCutV2ReplayOptions {
	t.Helper()
	_, limits := attemptReplayV2TestBounds()
	metadata := map[string][]byte{}
	for _, stream := range []attemptReplayV2TestStream{records, proofs} {
		for hash, raw := range stream.metadata {
			metadata[hash] = bytes.Clone(raw)
		}
	}
	return AttemptCutV2ReplayOptions{Bounds: limits, ScratchDirectory: filepath.Join(t.TempDir(), "replay-scratch"), ServerKeys: keys, ReadMetadata: attemptStreamV2TestReader(metadata), OpenData: func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		stream := records
		if kind == AttemptStreamV2Proofs {
			stream = proofs
		} else if kind != AttemptStreamV2Records {
			return nil, errors.New("unexpected data stream kind")
		}
		raw, found := stream.data[hash]
		if !found {
			return nil, errors.New("missing data chunk")
		}
		return io.NopCloser(bytes.NewReader(bytes.Clone(raw))), nil
	}}
}

// Re-signing all validator wrappers must not hide invalid lifecycle or server
// data. Original server ASSIGN/FINAL signatures remain unchanged by this helper.
func attemptReplayV2TestRechain(t *testing.T, records []AttemptRecord, key ed25519.PrivateKey) []AttemptRecord {
	t.Helper()
	result := make([]AttemptRecord, len(records))
	prior := records[0].PreviousHash
	first := records[0].Sequence
	for index, record := range records {
		cloned, err := cloneAttemptRecord(record)
		if err != nil {
			t.Fatal(err)
		}
		cloned.Sequence, cloned.PreviousHash = first+uint64(index), prior
		result[index] = resignAttemptRecordStoreTest(t, cloned, key)
		prior = result[index].RecordHash
	}
	return result
}

// Both complete trails traverse multiple chunks/pages and retain exact bytes.
func TestAttemptCutV2ReplayRealCompleteTrails(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 2)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	var visited int
	options.VisitRecord = func(record AttemptRecord) error {
		if !reflect.DeepEqual(record, records[visited]) {
			t.Fatalf("visited record %d differs", visited)
		}
		visited++
		return nil
	}
	bounds, _ := attemptReplayV2TestBounds()
	result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, options)
	if err != nil || result.CompleteCount != 2 || result.FailedCount != 0 || result.TrailCount != 2 || result.Records.ItemCount != 16 || result.Records.PageCount != 4 || result.Proofs.ItemCount != 2 || visited != 16 {
		t.Fatalf("complete real replay = %+v visits=%d: %v", result, visited, err)
	}
	proofBytes := proofStream.data[proofStream.chunks[0].ContentHash]
	if result.ProofProjectionHash != attemptHex32(sha256.Sum256(proofBytes)) {
		t.Fatal("proof projection digest differs from the complete public bytes")
	}
}

// Interleaved workers cannot overwrite one another's pending state.
func TestAttemptCutV2ReplayInterleavedTrails(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 2)
	interleaved := make([]AttemptRecord, 0, len(records))
	for index := 0; index < 8; index++ {
		interleaved = append(interleaved, records[index], records[index+8])
	}
	interleaved = attemptReplayV2TestRechain(t, interleaved, key)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, interleaved, key, nil, nil)
	bounds, _ := attemptReplayV2TestBounds()
	result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, attemptReplayV2TestOptions(t, recordStream, proofStream, keys))
	if err != nil || result.CompleteCount != 2 || result.TrailCount != 2 || result.Records.LastSequence != 16 {
		t.Fatalf("interleaved lifecycle = %+v: %v", result, err)
	}
}

// M4/M8/M16 retain real maximum-width signatures, not truncated proof shells;
// original sequence coordinates close to uint64 exhaustion do not wrap.
func TestAttemptCutV2ReplayMaximumWireDepths(t *testing.T) {
	for _, depth := range []int{4, 8, 16} {
		legacy, _, keys := attemptWireMaximumFixture(t, depth, "stream-maximum-wire")
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x11}, ed25519.SeedSize))
		cut, recordStream, proofStream := attemptReplayV2TestCut(t, legacy.Records, key, nil, nil)
		bounds, _ := attemptReplayV2TestBounds()
		result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, attemptReplayV2TestOptions(t, recordStream, proofStream, keys))
		if err != nil || result.CompleteCount != 1 || result.TrailCount != 1 || result.Records.ItemCount != uint64(depth) || result.Records.LastSequence != ^uint64(0)-1 {
			t.Fatalf("M%d maximum replay = %+v: %v", depth, result, err)
		}
	}
}

// Failed outcomes retain their full denominators without inventing proof rows.
func TestAttemptCutV2ReplayFailedTerminalOutcomes(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	for _, disposition := range []string{AttemptDispositionHopFailure, AttemptDispositionProtocol, AttemptDispositionUnknownFinal, AttemptDispositionValidatorError} {
		terminal := records[0]
		terminal.Disposition = disposition
		failed := attemptReplayV2TestRechain(t, []AttemptRecord{records[0], terminal}, key)
		cut, recordStream, proofStream := attemptReplayV2TestCut(t, failed, key, nil, nil)
		bounds, _ := attemptReplayV2TestBounds()
		result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, attemptReplayV2TestOptions(t, recordStream, proofStream, keys))
		if err != nil || result.CompleteCount != 0 || result.FailedCount != 1 || result.TrailCount != 1 || result.Proofs != (AttemptStreamV2Census{}) {
			t.Fatalf("%s replay = %+v: %v", disposition, result, err)
		}
	}
}

// No artificial empty object, ledger walk or scratch namespace is created.
func TestAttemptCutV2ReplayEmptyWindowDoesNoIO(t *testing.T) {
	cut, key, _ := attemptCutV2TestFixture(t)
	cut.LastSequence, cut.RecordCount, cut.CompleteCount, cut.FailedCount = 0, 0, 0, 0
	cut.Context.FirstSequence, cut.Context.EgressFirstSequence = 1, 1
	cut.Root = cut.Context.PriorRoot
	cut.Records, cut.Proofs = AttemptStreamV2Reference{ManifestHash: zeroAttemptHash()}, AttemptStreamV2Reference{ManifestHash: zeroAttemptHash()}
	bounds, limits := attemptReplayV2TestBounds()
	var err error
	cut.Signature, err = cut.Sign(key, bounds)
	if err != nil {
		t.Fatal(err)
	}
	options := AttemptCutV2ReplayOptions{Bounds: limits, ScratchDirectory: filepath.Join(t.TempDir(), "unused"), ReadMetadata: func(context.Context, string, uint64) ([]byte, error) { t.Fatal("empty metadata read"); return nil, nil }, OpenData: func(context.Context, string, string, uint64) (io.ReadCloser, error) {
		t.Fatal("empty data read")
		return nil, nil
	}}
	result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, options)
	if err != nil || result.Records != (AttemptStreamV2Census{}) || result.Proofs != (AttemptStreamV2Census{}) || result.ProofProjectionHash != attemptHex32(sha256.Sum256(nil)) {
		t.Fatalf("empty replay = %+v: %v", result, err)
	}
	if _, err := os.Lstat(options.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty replay created scratch state: %v", err)
	}
}

// Fresh validator signatures cannot authorize skipped or reused lifecycles.
func TestAttemptCutV2ReplayRejectsResignedLifecycleChanges(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 2)
	for _, candidate := range []struct {
		name    string
		records []AttemptRecord
	}{
		{name: "skipped first pending", records: append(append([]AttemptRecord(nil), records[:8]...), records[9:]...)},
		{name: "reused terminal trail", records: append(append([]AttemptRecord(nil), records[:8]...), records[:8]...)},
		{name: "repeated pending", records: append(append([]AttemptRecord{records[0]}, records[:8]...), records[8:]...)},
	} {
		changed := attemptReplayV2TestRechain(t, candidate.records, key)
		cut, recordStream, proofStream := attemptReplayV2TestCut(t, changed, key, nil, nil)
		bounds, _ := attemptReplayV2TestBounds()
		result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, attemptReplayV2TestOptions(t, recordStream, proofStream, keys))
		if err == nil || result != (AttemptCutV2ReplayResult{}) {
			t.Fatalf("%s accepted or returned partial success: %+v %v", candidate.name, result, err)
		}
	}
}

// A valid completed prefix cannot hide another worker's unfinished checkpoint.
func TestAttemptCutV2ReplayRejectsUnfinishedTail(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 2)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records[:15], key, nil, nil)
	bounds, _ := attemptReplayV2TestBounds()
	result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, attemptReplayV2TestOptions(t, recordStream, proofStream, keys))
	if err == nil || !strings.Contains(err.Error(), "pending lifecycle") || result != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("unfinished tail: %+v %v", result, err)
	}
}

// Both the enclosing records and the compact cut are authentically re-signed;
// rejection must come from the actual operator's server signature.
func TestAttemptCutV2ReplayRejectsResignedServerForgery(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	records[0].Assignments[0].AssignSignature[0] ^= 1
	records = attemptReplayV2TestRechain(t, records, key)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	bounds, _ := attemptReplayV2TestBounds()
	result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, attemptReplayV2TestOptions(t, recordStream, proofStream, keys))
	if err == nil || !strings.Contains(err.Error(), "server signature") || result != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("freshly re-signed server forgery: %+v %v", result, err)
	}
}

// Reordered and duplicated valid proofs cannot replace the exact projection.
func TestAttemptCutV2ReplayRejectsResignedProofProjectionChanges(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 2)
	for _, duplicate := range []bool{false, true} {
		cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, func(_ int, _ *AttemptStreamV2Chunk, raw *[]byte) {
			rows := bytes.SplitAfter(*raw, []byte{'\n'})
			if len(rows) != 3 {
				t.Fatal("expected two complete proof rows")
			}
			if duplicate {
				*raw = append(bytes.Clone(rows[0]), rows[0]...)
			} else {
				*raw = append(bytes.Clone(rows[1]), rows[0]...)
			}
		})
		bounds, _ := attemptReplayV2TestBounds()
		result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, attemptReplayV2TestOptions(t, recordStream, proofStream, keys))
		if err == nil || !strings.Contains(err.Error(), "signed record projection") || result != (AttemptCutV2ReplayResult{}) {
			t.Fatalf("projection duplicate=%t: %+v %v", duplicate, result, err)
		}
	}
}

// Proof JSON carries no sequence; coordinates must come from original rows.
func TestAttemptCutV2ReplayBindsProofSequenceCoordinates(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 2)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, func(_ int, chunk *AttemptStreamV2Chunk, _ *[]byte) { chunk.FirstSequence++; chunk.LastSequence++ })
	bounds, _ := attemptReplayV2TestBounds()
	result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, attemptReplayV2TestOptions(t, recordStream, proofStream, keys))
	if err == nil || !strings.Contains(err.Error(), "original record sequence") || result != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("proof sequence substitution: %+v %v", result, err)
	}
}

// Missing, truncated, changed and extra data never yields a partial verdict.
func TestAttemptCutV2ReplayRejectsIncompleteOrChangedChunkBytes(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	for _, kind := range []string{"missing", "truncated", "changed", "extra"} {
		cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
		hash := recordStream.chunks[len(recordStream.chunks)-1].ContentHash
		raw := bytes.Clone(recordStream.data[hash])
		switch kind {
		case "missing":
			delete(recordStream.data, hash)
		case "truncated":
			recordStream.data[hash] = raw[:len(raw)-1]
		case "changed":
			raw[0] = ' '
			recordStream.data[hash] = raw
		case "extra":
			recordStream.data[hash] = append(raw, ' ')
		}
		bounds, _ := attemptReplayV2TestBounds()
		result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, attemptReplayV2TestOptions(t, recordStream, proofStream, keys))
		if err == nil || result != (AttemptCutV2ReplayResult{}) {
			t.Fatalf("%s chunk: %+v %v", kind, result, err)
		}
	}
}

// Fresh content hashes and signatures cannot normalize alternate JSON bytes.
func TestAttemptCutV2ReplayRejectsRehashedNoncanonicalRows(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	for _, mutation := range []string{"space", "duplicate field", "CRLF"} {
		cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, func(index int, _ *AttemptStreamV2Chunk, raw *[]byte) {
			if index != 0 {
				return
			}
			switch mutation {
			case "space":
				*raw = append([]byte{' '}, *raw...)
			case "duplicate field":
				*raw = bytes.Replace(*raw, []byte(`"sequence":1,`), []byte(`"sequence":1,"sequence":1,`), 1)
			case "CRLF":
				*raw = bytes.Replace(*raw, []byte{'\n'}, []byte{'\r', '\n'}, 1)
			}
		}, nil)
		bounds, _ := attemptReplayV2TestBounds()
		result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, attemptReplayV2TestOptions(t, recordStream, proofStream, keys))
		if err == nil || !strings.Contains(err.Error(), "canonical") || result != (AttemptCutV2ReplayResult{}) {
			t.Fatalf("%s row: %+v %v", mutation, result, err)
		}
	}
}

// Limits remain independent: none is silently enlarged from declared totals.
func TestAttemptCutV2ReplayEnforcesRowTrailAndScratchBounds(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 2)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	for _, limit := range []string{"record", "proof", "trails", "scratch"} {
		options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
		switch limit {
		case "record":
			options.Bounds.MaxRecordBytes = 1
		case "proof":
			options.Bounds.MaxProofBytes = 1
		case "trails":
			options.Bounds.MaxTrails = 1
		case "scratch":
			options.Bounds.MaxScratchBytes = attemptStoreMetadataReserve + 1
		}
		bounds, _ := attemptReplayV2TestBounds()
		result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, options)
		if err == nil || result != (AttemptCutV2ReplayResult{}) {
			t.Fatalf("%s limit: %+v %v", limit, result, err)
		}
	}
}

// The verifier cannot reopen an old scratch index or touch unrelated files.
func TestAttemptCutV2ReplayPreservesExistingScratch(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	if err := os.Mkdir(options.ScratchDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(options.ScratchDirectory, "sentinel")
	want := []byte("existing unrelated bytes\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	bounds, _ := attemptReplayV2TestBounds()
	result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, options)
	if err == nil || !strings.Contains(err.Error(), "fresh directory") || result != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("existing scratch: %+v %v", result, err)
	}
	got, err := os.ReadFile(path)
	entries, entriesErr := os.ReadDir(options.ScratchDirectory)
	if err != nil || entriesErr != nil || len(entries) != 1 || !bytes.Equal(got, want) {
		t.Fatalf("existing scratch changed: %v %v", err, entriesErr)
	}
}

// A reader seam records exact ownership without scheduler timing assumptions.
type attemptReplayV2TestReader struct {
	io.Reader
	close func() error
}

// The callback models both ordinary and failing I/O resource teardown.
func (self *attemptReplayV2TestReader) Close() error { return self.close() }

// Cancellation is forced at synchronous ownership boundaries, not with sleeps.
func TestAttemptCutV2ReplayCancellationAndVisitorFailuresCloseReaders(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	for _, stage := range []string{"metadata", "open", "visit", "visitor error", "last close"} {
		ctx, cancel := context.WithCancel(context.Background())
		options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
		opened, closed := 0, 0
		read := options.ReadMetadata
		options.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
			data, err := read(ctx, hash, size)
			if stage == "metadata" {
				cancel()
			}
			return data, err
		}
		open := options.OpenData
		options.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			reader, err := open(ctx, kind, hash, size)
			if err != nil {
				return nil, err
			}
			opened++
			if stage == "open" {
				cancel()
			}
			return &attemptReplayV2TestReader{Reader: reader, close: func() error {
				closed++
				if stage == "last close" && kind == AttemptStreamV2Proofs {
					cancel()
				}
				return reader.Close()
			}}, nil
		}
		failure := errors.New("deterministic staged visitor failure")
		options.VisitRecord = func(AttemptRecord) error {
			if stage == "visit" {
				cancel()
			} else if stage == "visitor error" {
				return failure
			}
			return nil
		}
		bounds, _ := attemptReplayV2TestBounds()
		result, err := ReplayAttemptCutV2(ctx, cut, cut.Context, bounds, options)
		cancel()
		wantErr := error(context.Canceled)
		if stage == "visitor error" {
			wantErr = failure
		}
		if !errors.Is(err, wantErr) || result != (AttemptCutV2ReplayResult{}) || opened != closed {
			t.Fatalf("%s result=%+v readers=%d/%d: %v", stage, result, opened, closed, err)
		}
	}
}

// A final successful read does not hide a close/transport completion failure.
func TestAttemptCutV2ReplayCloseFailureReturnsNoResult(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	open := options.OpenData
	failure := errors.New("deterministic final close failure")
	options.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := open(ctx, kind, hash, size)
		if err != nil {
			return nil, err
		}
		return &attemptReplayV2TestReader{Reader: reader, close: func() error {
			err := reader.Close()
			if kind == AttemptStreamV2Proofs {
				return errors.Join(err, failure)
			}
			return err
		}}, nil
	}
	bounds, _ := attemptReplayV2TestBounds()
	result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, options)
	if !errors.Is(err, failure) || result != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("final reader close: %+v %v", result, err)
	}
}

// A transport may return an acquired body with an opening error. The caller
// still owns it; the pre-fix early error return leaked that exact reader.
func TestAttemptCutV2ReplayOpenErrorClosesReturnedReader(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	failure := errors.New("opening failed after acquiring body")
	closed := 0
	options.OpenData = func(context.Context, string, string, uint64) (io.ReadCloser, error) {
		return &attemptReplayV2TestReader{Reader: bytes.NewReader(nil), close: func() error { closed++; return nil }}, failure
	}
	bounds, _ := attemptReplayV2TestBounds()
	result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, options)
	if !errors.Is(err, failure) || result != (AttemptCutV2ReplayResult{}) || closed != 1 {
		t.Fatalf("open-error reader ownership: closes=%d, want 1; result=%+v error=%v", closed, result, err)
	}
}

// A visitor owns its decoded bytes; later proof comparison and scratch state
// cannot alias those buffers and inherit arbitrary downstream mutations.
func TestAttemptCutV2ReplayVisitorOwnsItsRecordBytes(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	visits := 0
	options.VisitRecord = func(record AttemptRecord) error {
		visits++
		record.Signature[0] ^= 1
		record.VPK[0] ^= 1
		record.Assignments[0].AssignSignature[0] ^= 1
		if record.Proof != nil {
			record.Proof.FinalSig[0] ^= 1
		}
		return nil
	}
	bounds, _ := attemptReplayV2TestBounds()
	result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, options)
	if err != nil || result.CompleteCount != 1 || visits != 8 {
		t.Fatalf("visitor aliasing: %+v visits=%d: %v", result, visits, err)
	}
}

// The same EVM block number cannot name two hashes at the pinned boundary.
func TestAttemptCutV2ReplayRejectsSameHeightBoundarySubstitution(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	cut.Context.Boundary.EVMBlockHash = attemptHex32([32]byte{0x51})
	bounds, _ := attemptReplayV2TestBounds()
	var err error
	cut.Signature, err = cut.Sign(key, bounds)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, attemptReplayV2TestOptions(t, recordStream, proofStream, keys))
	if err == nil || !strings.Contains(err.Error(), "boundary") || result != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("same-height hash substitution: %+v %v", result, err)
	}
}

// Creation and acquisition must refer to the same private directory inode;
// replacing the leaf or its parent must not redirect even the first DB write.
func TestAttemptCutV2ReplayRejectsFreshScratchReplacement(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	for _, replaceParent := range []bool{false, true} {
		options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
		base := t.TempDir()
		parent := filepath.Join(base, "parent")
		other := filepath.Join(base, "unrelated-private-directory")
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(other, 0o700); err != nil {
			t.Fatal(err)
		}
		options.ScratchDirectory = filepath.Join(parent, "scratch")
		hooks := attemptCutV2ReplayHooks{ScratchCreated: func(created string) error {
			if replaceParent {
				if err := os.Rename(parent, filepath.Join(base, "preserved-created-parent")); err != nil {
					return err
				}
				if err := os.Mkdir(parent, 0o700); err != nil {
					return err
				}
			} else if err := os.Rename(created, filepath.Join(parent, "preserved-created-scratch")); err != nil {
				return err
			}
			return os.Rename(other, options.ScratchDirectory)
		}}
		bounds, _ := attemptReplayV2TestBounds()
		result, err := replayAttemptCutV2WithHooks(context.Background(), cut, cut.Context, bounds, options, hooks)
		entries, entriesErr := os.ReadDir(options.ScratchDirectory)
		if err == nil || result != (AttemptCutV2ReplayResult{}) || entriesErr != nil || len(entries) != 0 {
			t.Fatalf("fresh scratch replacement parent=%t: result=%+v unrelated files=%d readErr=%v replayErr=%v", replaceParent, result, len(entries), entriesErr, err)
		}
	}
}

// Cancellation after real authentication must precede scratch/visitor work,
// just as cancellation after descriptor decoding precedes chunk visitors.
func TestAttemptCutV2ReplayPostDecodeCancellationPreventsVisitor(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	visits, decodes := 0, 0
	options.VisitRecord = func(AttemptRecord) error { visits++; return nil }
	hooks := attemptCutV2ReplayHooks{RecordDecoded: func() { decodes++; cancel() }}
	bounds, _ := attemptReplayV2TestBounds()
	result, err := replayAttemptCutV2WithHooks(ctx, cut, cut.Context, bounds, options, hooks)
	if !errors.Is(err, context.Canceled) || result != (AttemptCutV2ReplayResult{}) || decodes != 1 || visits != 0 {
		t.Fatalf("post-decode cancellation: decodes=%d visits=%d, want 1/0; result=%+v error=%v", decodes, visits, result, err)
	}
}
