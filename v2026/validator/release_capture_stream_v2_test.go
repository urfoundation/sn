//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type captureStreamTestOrigin struct {
	stateLock sync.Mutex
	objects   map[string][]byte
	reads     map[string]int
	alter     func(string, []byte) []byte
	server    *httptest.Server
}

func newCaptureStreamTestOrigin(t *testing.T, objects map[string][]byte) *captureStreamTestOrigin {
	t.Helper()
	origin := &captureStreamTestOrigin{objects: objects, reads: map[string]int{}}
	origin.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kind, hash := r.URL.Query().Get("kind"), r.URL.Query().Get("hash")
		origin.stateLock.Lock()
		origin.reads[kind+"/"+hash]++
		raw := bytes.Clone(origin.objects[hash])
		size := len(raw)
		if origin.alter != nil {
			raw = origin.alter(kind, raw)
		}
		origin.stateLock.Unlock()
		if size == 0 {
			http.NotFound(w, r)
			return
		}
		contentType := "application/x-ndjson"
		if kind == "metadata" {
			contentType = "application/json"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", fmt.Sprint(size))
		_, _ = w.Write(raw)
	}))
	t.Cleanup(origin.server.Close)
	return origin
}

func (self *captureStreamTestOrigin) count(kind, hash string) int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.reads[kind+"/"+hash]
}

// The complete and prefix references share one authentic original chunk, while
// their metadata independently binds each exact census and sequence geometry.
func captureStreamTestReferences(t *testing.T, kind string, rows [][]byte, edit func(*AttemptStreamV2Page)) ([]AttemptStreamV2Reference, map[string][]byte) {
	t.Helper()
	objects := map[string][]byte{}
	var refs []AttemptStreamV2Reference
	var chunks []AttemptStreamV2Chunk
	var size uint64
	for index, raw := range rows {
		hash := attemptHex32(sha256.Sum256(raw))
		objects[hash] = bytes.Clone(raw)
		chunks = append(chunks, AttemptStreamV2Chunk{Index: uint64(index), FirstSequence: uint64(index + 1), LastSequence: uint64(index + 1), ItemCount: 1, DataBytes: uint64(len(raw)), ContentHash: hash})
		size += uint64(len(raw))
		pages := []AttemptStreamV2Page{{Schema: AttemptStreamV2PageSchema, Kind: kind, Index: 0, Chunks: append([]AttemptStreamV2Chunk(nil), chunks...)}}
		ref, metadata := attemptStreamV2TestArchive(t, kind, pages, func(_ int, page *AttemptStreamV2Page) {
			if edit != nil {
				edit(page)
			}
		}, func(manifest *AttemptStreamV2Manifest) {
			manifest.ItemCount, manifest.ChunkCount, manifest.PageCount, manifest.DataBytes = uint64(len(chunks)), uint64(len(chunks)), 1, size
			if edit != nil {
				manifest.DataBytes = 0
				for _, chunk := range pages[0].Chunks {
					manifest.DataBytes += chunk.DataBytes
				}
			}
		})
		for hash, raw := range metadata {
			objects[hash] = raw
		}
		refs = append(refs, ref)
	}
	return refs, objects
}

func captureStreamTestBounds() AttemptCutV2Bounds {
	return AttemptCutV2Bounds{Records: attemptStreamV2TestBounds(), Proofs: attemptStreamV2TestBounds()}
}

func TestReleaseCaptureStreamReuseRetainsEveryOriginAndSharedPrefix(t *testing.T) {
	rows := [][]byte{[]byte("synthetic-one\n"), []byte("synthetic-two\n")}
	refs, objects := captureStreamTestReferences(t, AttemptStreamV2Records, rows, nil)
	origins := []*captureStreamTestOrigin{newCaptureStreamTestOrigin(t, objects), newCaptureStreamTestOrigin(t, objects)}
	retained := map[ReleaseEvidenceV2CaptureSource][]byte{}
	emit := func(source ReleaseEvidenceV2CaptureSource, raw []byte) error {
		retained[source] = bytes.Clone(raw)
		return nil
	}
	options := ReleaseEvidenceV2CaptureOptions{MaximumObjects: 8, ReuseCapturedStreams: true}
	capture := newReleaseCaptureStreamsV2(captureStreamTestBounds(), options, emit)
	for _, origin := range origins {
		for _, ref := range []AttemptStreamV2Reference{refs[0], refs[1], refs[1]} {
			if err := capture.capture(t.Context(), origin.server.URL, AttemptStreamV2Records, ref, attemptStreamV2TestBounds()); err != nil {
				t.Fatal(err)
			}
		}
		for _, raw := range rows {
			hash := attemptHex32(sha256.Sum256(raw))
			if reads := origin.count(AttemptStreamV2Records, hash); reads != 1 {
				t.Fatalf("shared immutable chunk downloaded %d times instead of once per origin", reads)
			}
			if !bytes.Equal(retained[ReleaseEvidenceV2CaptureSource{Kind: AttemptStreamV2Records, Name: hash, Origin: origin.server.URL}], raw) {
				t.Fatal("exact original bytes or origin provenance were lost")
			}
		}
		if origin.count("metadata", refs[1].ManifestHash) != 2 {
			t.Fatal("reuse skipped independent reference validation")
		}
	}
	// Invocation success cannot become a persistent authority for a later read.
	next := newReleaseCaptureStreamsV2(captureStreamTestBounds(), options, emit)
	if err := next.capture(t.Context(), origins[0].server.URL, AttemptStreamV2Records, refs[0], attemptStreamV2TestBounds()); err != nil {
		t.Fatal(err)
	}
	if origins[0].count(AttemptStreamV2Records, attemptHex32(sha256.Sum256(rows[0]))) != 2 {
		t.Fatal("new invocation borrowed an old success witness")
	}
}

func TestReleaseCaptureStreamReuseDefaultStrictReadsAgain(t *testing.T) {
	raw := []byte("synthetic-original\n")
	refs, objects := captureStreamTestReferences(t, AttemptStreamV2Records, [][]byte{raw}, nil)
	origin := newCaptureStreamTestOrigin(t, objects)
	capture := newReleaseCaptureStreamsV2(captureStreamTestBounds(), ReleaseEvidenceV2CaptureOptions{MaximumObjects: 2}, func(ReleaseEvidenceV2CaptureSource, []byte) error { return nil })
	for range 2 {
		if err := capture.capture(t.Context(), origin.server.URL, AttemptStreamV2Records, refs[0], attemptStreamV2TestBounds()); err != nil {
			t.Fatal(err)
		}
	}
	if capture.retained != nil || origin.count(AttemptStreamV2Records, attemptHex32(sha256.Sum256(raw))) != 2 {
		t.Fatal("strict default inherited diagnostic reuse")
	}
}

func TestReleaseCaptureStreamReuseRequiresCompleteBodyAndDurableSink(t *testing.T) {
	for _, failure := range []string{"truncated", "changed", "sink"} {
		raw := []byte("synthetic-original\n")
		refs, objects := captureStreamTestReferences(t, AttemptStreamV2Records, [][]byte{raw}, nil)
		origin := newCaptureStreamTestOrigin(t, objects)
		origin.alter = func(kind string, body []byte) []byte {
			if kind == AttemptStreamV2Records && len(body) != 0 {
				if failure == "truncated" {
					return body[:len(body)-1]
				}
				if failure == "changed" {
					body[0] ^= 1
				}
			}
			return body
		}
		sinkFails := failure == "sink"
		capture := newReleaseCaptureStreamsV2(captureStreamTestBounds(), ReleaseEvidenceV2CaptureOptions{MaximumObjects: 2, ReuseCapturedStreams: true}, func(source ReleaseEvidenceV2CaptureSource, _ []byte) error {
			if source.Kind == AttemptStreamV2Records && sinkFails {
				return errors.New("synthetic durable sink failure")
			}
			return nil
		})
		err := capture.capture(t.Context(), origin.server.URL, AttemptStreamV2Records, refs[0], attemptStreamV2TestBounds())
		if err == nil || len(capture.retained) != 0 || !strings.Contains(err.Error(), "chunk 0 "+attemptHex32(sha256.Sum256(raw))) {
			t.Fatalf("%s became successful custody or lost its failing chunk: %v", failure, err)
		}
		origin.stateLock.Lock()
		origin.alter = nil
		origin.stateLock.Unlock()
		sinkFails = false
		if err := capture.capture(t.Context(), origin.server.URL, AttemptStreamV2Records, refs[0], attemptStreamV2TestBounds()); err != nil {
			t.Fatal(err)
		}
		if len(capture.retained) != 1 || origin.count(AttemptStreamV2Records, attemptHex32(sha256.Sum256(raw))) != 2 {
			t.Fatalf("%s prevented an independent complete retry", failure)
		}
	}
}

func TestReleaseCaptureStreamReuseKeepsTypeSizeAndMetadataAuthority(t *testing.T) {
	raw := []byte("synthetic-original\n")
	recordRefs, objects := captureStreamTestReferences(t, AttemptStreamV2Records, [][]byte{raw}, nil)
	proofRefs, proofs := captureStreamTestReferences(t, AttemptStreamV2Proofs, [][]byte{raw}, nil)
	sizeRefs, changedSize := captureStreamTestReferences(t, AttemptStreamV2Records, [][]byte{raw}, func(page *AttemptStreamV2Page) { page.Chunks[0].DataBytes++ })
	for _, additional := range []map[string][]byte{proofs, changedSize} {
		for hash, body := range additional {
			objects[hash] = body
		}
	}
	origin := newCaptureStreamTestOrigin(t, objects)
	capture := newReleaseCaptureStreamsV2(captureStreamTestBounds(), ReleaseEvidenceV2CaptureOptions{MaximumObjects: 4, ReuseCapturedStreams: true}, func(ReleaseEvidenceV2CaptureSource, []byte) error { return nil })
	for _, stream := range []struct {
		kind string
		ref  AttemptStreamV2Reference
	}{{kind: AttemptStreamV2Records, ref: recordRefs[0]}, {kind: AttemptStreamV2Proofs, ref: proofRefs[0]}} {
		if err := capture.capture(t.Context(), origin.server.URL, stream.kind, stream.ref, attemptStreamV2TestBounds()); err != nil {
			t.Fatal(err)
		}
	}
	hash := attemptHex32(sha256.Sum256(raw))
	if origin.count(AttemptStreamV2Records, hash) != 1 || origin.count(AttemptStreamV2Proofs, hash) != 1 || len(capture.retained) != 2 {
		t.Fatal("one typed route borrowed another route's custody")
	}
	if err := capture.capture(t.Context(), origin.server.URL, AttemptStreamV2Records, sizeRefs[0], attemptStreamV2TestBounds()); err == nil || !strings.Contains(err.Error(), "authenticated size") || origin.count(AttemptStreamV2Records, hash) != 2 {
		t.Fatalf("different authenticated size reused an old body: %v", err)
	}
	origin.stateLock.Lock()
	origin.alter = func(kind string, body []byte) []byte {
		if kind == "metadata" && len(body) != 0 {
			body[0] ^= 1
		}
		return body
	}
	origin.stateLock.Unlock()
	if err := capture.capture(t.Context(), origin.server.URL, AttemptStreamV2Records, recordRefs[0], attemptStreamV2TestBounds()); err == nil || !strings.Contains(err.Error(), "metadata ") {
		t.Fatalf("cached chunk hid invalid fresh metadata: %v", err)
	}
}

func TestReleaseCaptureStreamReuseHonorsWitnessBoundAndCancellation(t *testing.T) {
	rows := [][]byte{[]byte("synthetic-one\n"), []byte("synthetic-two\n")}
	refs, objects := captureStreamTestReferences(t, AttemptStreamV2Records, rows, nil)
	origin := newCaptureStreamTestOrigin(t, objects)
	capture := newReleaseCaptureStreamsV2(captureStreamTestBounds(), ReleaseEvidenceV2CaptureOptions{MaximumObjects: 1, ReuseCapturedStreams: true}, func(ReleaseEvidenceV2CaptureSource, []byte) error { return nil })
	if err := capture.capture(t.Context(), origin.server.URL, AttemptStreamV2Records, refs[1], attemptStreamV2TestBounds()); err == nil || !strings.Contains(err.Error(), "witness count") || len(capture.retained) != 1 || origin.count(AttemptStreamV2Records, attemptHex32(sha256.Sum256(rows[1]))) != 0 {
		t.Fatalf("one-over witness admitted a new network body: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := capture.capture(ctx, origin.server.URL, AttemptStreamV2Records, refs[0], attemptStreamV2TestBounds()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached body hid owner cancellation: %v", err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	capture = newReleaseCaptureStreamsV2(captureStreamTestBounds(), ReleaseEvidenceV2CaptureOptions{MaximumObjects: 1, ReuseCapturedStreams: true}, func(source ReleaseEvidenceV2CaptureSource, _ []byte) error {
		if source.Kind == AttemptStreamV2Records {
			cancel()
		}
		return nil
	})
	if err := capture.capture(ctx, origin.server.URL, AttemptStreamV2Records, refs[0], attemptStreamV2TestBounds()); !errors.Is(err, context.Canceled) || len(capture.retained) != 0 {
		t.Fatalf("cancellation during sink created a reusable success: %v", err)
	}
}
