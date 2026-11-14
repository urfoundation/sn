//go:build linux || darwin

package validator

// The full disk sealer and independent policy replay use actual local HTTP.
// This fixture is not the production operator API or a MinIO replication test.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Fixture-only immutable object storage serializes publication with HTTP reads.
// Payload copies and hashing occur outside the map lock.
type attemptStreamV2HTTPReplayArchive struct {
	stateLock    sync.Mutex
	objects      map[string]map[string][]byte
	reads        map[string]int
	corruptProof bool
}

// The actual stream writer transfers owned bounded data to this test transport.
func (self *attemptStreamV2HTTPReplayArchive) write(kind string) AttemptStreamV2ObjectWriter {
	return func(ctx context.Context, contentHash string, raw []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if contentHash != attemptHex32(sha256.Sum256(raw)) {
			return errors.New("HTTP fixture publication hash differs")
		}
		owned := bytes.Clone(raw)
		return func() error {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			if old, exists := self.objects[kind][contentHash]; exists && !bytes.Equal(old, owned) {
				return errors.New("HTTP fixture publication conflicts")
			}
			self.objects[kind][contentHash] = owned
			return nil
		}()
	}
}

// Each request owns its returned bytes; no staged output becomes a fake verdict.
func (self *attemptStreamV2HTTPReplayArchive) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	kind, contentHash := request.URL.Query().Get("kind"), request.URL.Query().Get("hash")
	if request.Method != http.MethodGet || request.URL.Path != "/sn/attempt-artifact" || len(request.URL.Query()) != 2 {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	raw, corrupt := func() ([]byte, bool) {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.reads[kind]++
		return self.objects[kind][contentHash], self.corruptProof && kind == AttemptStreamV2Proofs
	}()
	if raw == nil {
		response.WriteHeader(http.StatusNotFound)
		return
	}
	if corrupt {
		raw = bytes.Clone(raw)
		raw[len(raw)-1] ^= 1
	}
	contentType := "application/x-ndjson"
	if kind == "metadata" {
		contentType = "application/json"
	}
	response.Header().Set("Content-Type", contentType)
	_, _ = response.Write(raw)
}

// Uses the same real M8 source and finite bounds as existing sealer regressions.
func newAttemptStreamV2HTTPReplayFixture(t *testing.T, corrupt bool) (*attemptCutV2SealTestFixture, AttemptCutV2SealOptions, *attemptStreamV2HTTPReplayArchive) {
	t.Helper()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 1)
	options, _ := newAttemptCutV2SealTestOptions(t, fixture)
	archive := &attemptStreamV2HTTPReplayArchive{objects: map[string]map[string][]byte{"metadata": {}, AttemptStreamV2Records: {}, AttemptStreamV2Proofs: {}}, reads: map[string]int{}, corruptProof: corrupt}
	server := httptest.NewServer(archive)
	t.Cleanup(server.Close)
	reader, err := NewHTTPAttemptStreamV2Reader(server.URL, fixture.bounds)
	if err != nil {
		t.Fatal(err)
	}
	options.WriteRecords, options.WriteProofs, options.WriteMetadata = archive.write(AttemptStreamV2Records), archive.write(AttemptStreamV2Proofs), archive.write("metadata")
	options.ReadMetadata, options.OpenData = reader.ReadMetadata, reader.OpenData
	return fixture, options, archive
}

// Successful fetch-back and a second full replay authenticate all actual records,
// complete/failed terminals and exact proof projection through HTTP independently.
func TestAttemptStreamV2HTTPRealM8DiskSealAndPublicReplay(t *testing.T) {
	fixture, options, archive := newAttemptStreamV2HTTPReplayFixture(t, false)
	cut, verified, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	assertAttemptCutV2SealTestSuccess(t, fixture, options, cut, verified, fixture.recordTs, 2, 1)
	reads, counts := func() (map[string]int, map[string]int) {
		archive.stateLock.Lock()
		defer archive.stateLock.Unlock()
		reads, counts := map[string]int{}, map[string]int{}
		for _, kind := range []string{"metadata", AttemptStreamV2Records, AttemptStreamV2Proofs} {
			reads[kind], counts[kind] = archive.reads[kind], len(archive.objects[kind])
		}
		return reads, counts
	}()
	for _, kind := range []string{"metadata", AttemptStreamV2Records, AttemptStreamV2Proofs} {
		if reads[kind] < 2 || counts[kind] == 0 {
			t.Errorf("full HTTP replay omitted %s: reads=%d objects=%d", kind, reads[kind], counts[kind])
		}
	}
}

// Changed served proof bytes under an unchanged hash cannot produce a signed
// cut or partial success, even after all record objects were fetched correctly.
func TestAttemptStreamV2HTTPChangedPublicProofPreventsCutSignature(t *testing.T) {
	fixture, options, archive := newAttemptStreamV2HTTPReplayFixture(t, true)
	cut, verified, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err == nil || cut != nil || verified != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("changed public proof became signed cut: cut=%v verified=%+v error=%v", cut, verified, err)
	}
	recordReads, proofReads := func() (int, int) {
		archive.stateLock.Lock()
		defer archive.stateLock.Unlock()
		return archive.reads[AttemptStreamV2Records], archive.reads[AttemptStreamV2Proofs]
	}()
	if recordReads == 0 || proofReads == 0 {
		t.Error("rejection did not reach both actual typed HTTP bodies")
	}
}
