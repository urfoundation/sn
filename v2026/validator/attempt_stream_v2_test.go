package validator

// These fixtures authenticate descriptor metadata only. Their opaque chunk
// hashes are not presented as real signed records or a data-replay pass.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Small explicit test budgets exercise the same caller-bounded metadata path.
func attemptStreamV2TestBounds() AttemptStreamV2Bounds {
	return AttemptStreamV2Bounds{MaxDataBytes: 1024 * 1024, MaxItems: 1024, MaxChunkBytes: 8 * 1024, MaxChunks: 128, MaxPages: 64, MaxPageBytes: 4 * 1024, MaxDescriptorsPerPage: 2, MaxManifestBytes: 1024}
}

// Two pages let tests distinguish local page validation from global continuity.
func attemptStreamV2TestPages(kind string) []AttemptStreamV2Page {
	pages := []AttemptStreamV2Page{
		{Schema: AttemptStreamV2PageSchema, Kind: kind, Index: 0, Chunks: []AttemptStreamV2Chunk{{Index: 0, FirstSequence: 1, LastSequence: 4, ItemCount: 4, DataBytes: 400, ContentHash: attemptHex32(sha256.Sum256([]byte("opaque first chunk")))}}},
		{Schema: AttemptStreamV2PageSchema, Kind: kind, Index: 1, Chunks: []AttemptStreamV2Chunk{{Index: 1, FirstSequence: 5, LastSequence: 8, ItemCount: 4, DataBytes: 400, ContentHash: attemptHex32(sha256.Sum256([]byte("opaque second chunk")))}}},
	}
	if kind == AttemptStreamV2Proofs {
		pages[0].Chunks[0].FirstSequence, pages[0].Chunks[0].ItemCount = 2, 2
		pages[1].Chunks[0].FirstSequence, pages[1].Chunks[0].ItemCount = 6, 2
	}
	return pages
}

// Hashes even deliberately malformed candidate metadata, so structural tests
// reach replay rather than failing only an outer stale content hash.
func attemptStreamV2TestArchive(t *testing.T, kind string, pages []AttemptStreamV2Page, editPage func(int, *AttemptStreamV2Page), editManifest func(*AttemptStreamV2Manifest)) (AttemptStreamV2Reference, map[string][]byte) {
	t.Helper()
	objects := map[string][]byte{}
	nextHash, nextBytes := zeroAttemptHash(), uint64(0)
	for index := len(pages) - 1; index >= 0; index-- {
		page := pages[index]
		page.NextPageHash, page.NextPageBytes = nextHash, nextBytes
		if editPage != nil {
			editPage(index, &page)
		}
		raw, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, '\n')
		nextHash, nextBytes = attemptHex32(sha256.Sum256(raw)), uint64(len(raw))
		objects[nextHash] = raw
	}
	manifest := AttemptStreamV2Manifest{Schema: AttemptStreamV2Schema, Kind: kind, ItemCount: 8, DataBytes: 800, ChunkCount: 2, PageCount: 2, FirstPageHash: nextHash, FirstPageBytes: nextBytes}
	if kind == AttemptStreamV2Proofs {
		manifest.ItemCount = 4
	}
	if editManifest != nil {
		editManifest(&manifest)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	reference := AttemptStreamV2Reference{ManifestHash: attemptHex32(sha256.Sum256(raw)), ManifestBytes: uint64(len(raw)), ItemCount: manifest.ItemCount, DataBytes: manifest.DataBytes, ChunkCount: manifest.ChunkCount, PageCount: manifest.PageCount}
	objects[reference.ManifestHash] = raw
	return reference, objects
}

// The test reader returns owned bytes; missing data never becomes an empty page.
func attemptStreamV2TestReader(objects map[string][]byte) AttemptStreamV2MetadataReader {
	return func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, found := objects[hash]
		if !found {
			return nil, errors.New("missing metadata object")
		}
		return bytes.Clone(data), nil
	}
}

// Exact totals and original sequence coordinates survive every page boundary.
func TestAttemptStreamV2WalksCompleteDescriptorCensus(t *testing.T) {
	for _, kind := range []string{AttemptStreamV2Records, AttemptStreamV2Proofs} {
		reference, objects := attemptStreamV2TestArchive(t, kind, attemptStreamV2TestPages(kind), nil, nil)
		reads, visits := 0, uint64(0)
		reader := attemptStreamV2TestReader(objects)
		census, err := WalkAttemptStreamV2Descriptors(context.Background(), kind, reference, attemptStreamV2TestBounds(), func(ctx context.Context, hash string, size uint64) ([]byte, error) {
			reads++
			if size > 4*1024 {
				t.Fatal("reader received a request outside metadata limits")
			}
			return reader(ctx, hash, size)
		}, func(chunk AttemptStreamV2Chunk) error {
			if chunk.Index != visits {
				t.Fatal("visitor received reordered chunks")
			}
			visits++
			return nil
		})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		first := uint64(1)
		if kind == AttemptStreamV2Proofs {
			first = 2
		}
		if reads != 3 || visits != 2 || census != (AttemptStreamV2Census{FirstSequence: first, LastSequence: 8, ItemCount: reference.ItemCount, DataBytes: 800, ChunkCount: 2, PageCount: 2}) {
			t.Fatalf("%s: unexpected traversal counts: reads=%d visits=%d census=%+v", kind, reads, visits, census)
		}
	}
}

// Freshly hashed malformed pages cannot launder an incomplete or mixed stream.
func TestAttemptStreamV2RejectsRehashedPageViolations(t *testing.T) {
	for _, mutation := range []struct {
		name string
		edit func(int, *AttemptStreamV2Page)
	}{
		{name: "cross-page gap", edit: func(index int, page *AttemptStreamV2Page) {
			if index == 1 {
				page.Chunks[0].FirstSequence++
				page.Chunks[0].LastSequence++
			}
		}},
		{name: "cross-page overlap", edit: func(index int, page *AttemptStreamV2Page) {
			if index == 1 {
				page.Chunks[0].FirstSequence--
				page.Chunks[0].LastSequence--
			}
		}},
		{name: "duplicate chunk index", edit: func(index int, page *AttemptStreamV2Page) {
			if index == 1 {
				page.Chunks[0].Index = 0
			}
		}},
		{name: "wrong page index", edit: func(index int, page *AttemptStreamV2Page) {
			if index == 1 {
				page.Index = 0
			}
		}},
		{name: "cross-kind page", edit: func(index int, page *AttemptStreamV2Page) {
			if index == 1 {
				page.Kind = AttemptStreamV2Proofs
			}
		}},
		{name: "early terminal", edit: func(index int, page *AttemptStreamV2Page) {
			if index == 0 {
				page.NextPageHash = zeroAttemptHash()
				page.NextPageBytes = 0
			}
		}},
		{name: "extra suffix", edit: func(index int, page *AttemptStreamV2Page) {
			if index == 1 {
				page.NextPageHash = attemptHex32([32]byte{3})
				page.NextPageBytes = 100
			}
		}},
		{name: "empty page", edit: func(index int, page *AttemptStreamV2Page) {
			if index == 1 {
				page.Chunks = nil
			}
		}},
		{name: "zero chunk hash", edit: func(index int, page *AttemptStreamV2Page) {
			if index == 1 {
				page.Chunks[0].ContentHash = zeroAttemptHash()
			}
		}},
		{name: "range overflow", edit: func(index int, page *AttemptStreamV2Page) {
			if index == 1 {
				page.Chunks[0].LastSequence = ^uint64(0)
			}
		}},
	} {
		reference, objects := attemptStreamV2TestArchive(t, AttemptStreamV2Records, attemptStreamV2TestPages(AttemptStreamV2Records), mutation.edit, nil)
		if _, err := WalkAttemptStreamV2Descriptors(context.Background(), AttemptStreamV2Records, reference, attemptStreamV2TestBounds(), attemptStreamV2TestReader(objects), func(AttemptStreamV2Chunk) error { return nil }); err == nil {
			t.Errorf("%s was accepted", mutation.name)
		}
	}
}

// The final exact totals remain mandatory after earlier authenticated visits.
func TestAttemptStreamV2RejectsLateCensusMismatch(t *testing.T) {
	for _, mutation := range []struct {
		name string
		edit func(*AttemptStreamV2Manifest)
	}{
		{name: "items", edit: func(manifest *AttemptStreamV2Manifest) { manifest.ItemCount++ }},
		{name: "bytes", edit: func(manifest *AttemptStreamV2Manifest) { manifest.DataBytes++ }},
		{name: "chunks", edit: func(manifest *AttemptStreamV2Manifest) { manifest.ChunkCount++ }},
	} {
		reference, objects := attemptStreamV2TestArchive(t, AttemptStreamV2Records, attemptStreamV2TestPages(AttemptStreamV2Records), nil, mutation.edit)
		visits := 0
		census, err := WalkAttemptStreamV2Descriptors(context.Background(), AttemptStreamV2Records, reference, attemptStreamV2TestBounds(), attemptStreamV2TestReader(objects), func(AttemptStreamV2Chunk) error { visits++; return nil })
		if err == nil || visits != 2 || census != (AttemptStreamV2Census{}) {
			t.Fatalf("%s: late mismatch must fail after both visits without exposing a success census: visits=%d census=%+v err=%v", mutation.name, visits, census, err)
		}
	}
}

// Neither hash-authenticated metadata nor a cache permits noncanonical JSON.
func TestAttemptStreamV2RejectsChangedAndNoncanonicalBytes(t *testing.T) {
	reference, objects := attemptStreamV2TestArchive(t, AttemptStreamV2Records, attemptStreamV2TestPages(AttemptStreamV2Records), nil, nil)
	data := objects[reference.ManifestHash]
	for _, mutation := range []struct {
		name string
		data []byte
	}{
		{name: "leading whitespace", data: append([]byte(" "), data...)},
		{name: "missing newline", data: bytes.Clone(data[:len(data)-1])},
		{name: "trailing JSON", data: append(bytes.Clone(data), '{', '}')},
		{name: "duplicate key", data: bytes.Replace(data, []byte("{\"schema\":"), []byte("{\"kind\":\"records\",\"schema\":"), 1)},
		{name: "unknown key", data: bytes.Replace(data, []byte("{\"schema\":"), []byte("{\"extra\":0,\"schema\":"), 1)},
	} {
		changed := reference
		changed.ManifestHash, changed.ManifestBytes = attemptHex32(sha256.Sum256(mutation.data)), uint64(len(mutation.data))
		if _, err := DecodeAttemptStreamV2Manifest(mutation.data, AttemptStreamV2Records, changed, attemptStreamV2TestBounds()); err == nil {
			t.Errorf("%s was accepted with a freshly calculated hash", mutation.name)
		}
	}
	corrupt := bytes.Clone(data)
	corrupt[0] ^= 1
	if _, err := DecodeAttemptStreamV2Manifest(corrupt, AttemptStreamV2Records, reference, attemptStreamV2TestBounds()); err == nil {
		t.Fatal("changed manifest bytes were accepted under an old hash")
	}
	if _, err := DecodeAttemptStreamV2Manifest(data, AttemptStreamV2Proofs, reference, attemptStreamV2TestBounds()); err == nil {
		t.Fatal("record manifest accepted as proof manifest")
	}
}

// Bounds are checked before reads; a reader cannot authorize a larger reply.
func TestAttemptStreamV2EnforcesPreReadAndResponseBounds(t *testing.T) {
	reference, objects := attemptStreamV2TestArchive(t, AttemptStreamV2Records, attemptStreamV2TestPages(AttemptStreamV2Records), nil, nil)
	for _, mutation := range []struct {
		name string
		edit func(*AttemptStreamV2Reference)
	}{
		{name: "manifest bytes", edit: func(ref *AttemptStreamV2Reference) { ref.ManifestBytes = 1025 }},
		{name: "data bytes", edit: func(ref *AttemptStreamV2Reference) { ref.DataBytes = 1024*1024 + 1 }},
		{name: "items", edit: func(ref *AttemptStreamV2Reference) { ref.ItemCount = 1025 }},
		{name: "pages", edit: func(ref *AttemptStreamV2Reference) { ref.PageCount = 65 }},
		{name: "chunks", edit: func(ref *AttemptStreamV2Reference) { ref.ChunkCount = 129 }},
		{name: "impossible page capacity", edit: func(ref *AttemptStreamV2Reference) { ref.ChunkCount = 5; ref.PageCount = 2 }},
		{name: "impossible chunk capacity", edit: func(ref *AttemptStreamV2Reference) { ref.DataBytes = 2*8*1024 + 1 }},
	} {
		changed := reference
		mutation.edit(&changed)
		reads := 0
		if _, err := WalkAttemptStreamV2Descriptors(context.Background(), AttemptStreamV2Records, changed, attemptStreamV2TestBounds(), func(context.Context, string, uint64) ([]byte, error) { reads++; return nil, nil }, func(AttemptStreamV2Chunk) error { return nil }); err == nil || reads != 0 {
			t.Errorf("%s read before validating its bounds: reads=%d err=%v", mutation.name, reads, err)
		}
	}
	reader := attemptStreamV2TestReader(objects)
	if _, err := WalkAttemptStreamV2Descriptors(context.Background(), AttemptStreamV2Records, reference, attemptStreamV2TestBounds(), func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		data, err := reader(ctx, hash, size)
		return append(data, 0), err
	}, func(AttemptStreamV2Chunk) error { return nil }); err == nil {
		t.Fatal("oversize reader response was accepted")
	}
}

// Empty streams need no network read, but cannot hide nonzero claimed data.
func TestAttemptStreamV2EmptyCensusIsUnique(t *testing.T) {
	empty := AttemptStreamV2Reference{ManifestHash: zeroAttemptHash()}
	reader := func(context.Context, string, uint64) ([]byte, error) {
		t.Fatal("empty stream fetched metadata")
		return nil, nil
	}
	census, err := WalkAttemptStreamV2Descriptors(context.Background(), AttemptStreamV2Records, empty, attemptStreamV2TestBounds(), reader, func(AttemptStreamV2Chunk) error { t.Fatal("empty stream emitted a chunk"); return nil })
	if err != nil || census != (AttemptStreamV2Census{}) {
		t.Fatalf("empty stream: %+v, %v", census, err)
	}
	for _, edit := range []func(*AttemptStreamV2Reference){
		func(ref *AttemptStreamV2Reference) { ref.ManifestHash = attemptHex32([32]byte{1}) },
		func(ref *AttemptStreamV2Reference) { ref.ManifestBytes = 1 },
		func(ref *AttemptStreamV2Reference) { ref.DataBytes = 1 },
		func(ref *AttemptStreamV2Reference) { ref.ChunkCount = 1 },
		func(ref *AttemptStreamV2Reference) { ref.PageCount = 1 },
	} {
		changed := empty
		edit(&changed)
		if err := changed.Validate(attemptStreamV2TestBounds()); err == nil {
			t.Errorf("nonempty metadata accepted as an empty stream: %+v", changed)
		}
	}
}

// Cancellation is forced at entry, after a read and at the final visitor;
// none may return a success census or silently ignore a visitor's own error.
func TestAttemptStreamV2CancellationAndVisitorError(t *testing.T) {
	reference, objects := attemptStreamV2TestArchive(t, AttemptStreamV2Records, attemptStreamV2TestPages(AttemptStreamV2Records), nil, nil)
	for _, stage := range []string{"entry", "read", "final visitor", "visitor error"} {
		ctx, cancel := context.WithCancel(context.Background())
		reader := attemptStreamV2TestReader(objects)
		reads, visits := 0, 0
		sentinel := errors.New("visitor stopped")
		if stage == "entry" {
			cancel()
		}
		census, err := WalkAttemptStreamV2Descriptors(ctx, AttemptStreamV2Records, reference, attemptStreamV2TestBounds(), func(ctx context.Context, hash string, size uint64) ([]byte, error) {
			reads++
			data, err := reader(ctx, hash, size)
			if stage == "read" {
				cancel()
			}
			return data, err
		}, func(AttemptStreamV2Chunk) error {
			visits++
			if stage == "visitor error" {
				return sentinel
			}
			if stage == "final visitor" && visits == 2 {
				cancel()
			}
			return nil
		})
		cancel()
		want := error(context.Canceled)
		if stage == "visitor error" {
			want = sentinel
		}
		if !errors.Is(err, want) || census != (AttemptStreamV2Census{}) {
			t.Errorf("%s: census=%+v err=%v", stage, census, err)
		}
		if stage == "entry" && reads != 0 || stage == "read" && (reads != 1 || visits != 0) || stage == "final visitor" && visits != 2 || stage == "visitor error" && visits != 1 {
			t.Errorf("%s: reads=%d visits=%d", stage, reads, visits)
		}
	}
}

// A forbidden extra descriptor must be refused at the array boundary, before
// its typed body is decoded. A malformed extra body makes the ordering visible
// without an allocation benchmark or scheduler-dependent memory observation.
func TestAttemptStreamV2BoundsDescriptorsBeforeDecodingExtra(t *testing.T) {
	page := attemptStreamV2TestPages(AttemptStreamV2Records)[0]
	bound := attemptStreamV2TestBounds()
	bound.MaxDescriptorsPerPage = 1
	raw, err := json.Marshal(map[string]any{
		"schema": AttemptStreamV2PageSchema, "kind": AttemptStreamV2Records,
		"index": uint64(0), "chunks": []any{page.Chunks[0], "must-not-decode-this-descriptor"},
		"next_page_hash": zeroAttemptHash(), "next_page_bytes": uint64(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	_, err = DecodeAttemptStreamV2Page(raw, AttemptStreamV2Records, 0, attemptHex32(sha256.Sum256(raw)), uint64(len(raw)), bound)
	if err == nil || !strings.Contains(err.Error(), "descriptor count") {
		t.Fatalf("extra descriptor body was decoded before its count bound: %v", err)
	}
}

// Cancellation after a genuine page decode must prevent the first downstream
// chunk fetch, rather than being noticed only after the visitor has run.
func TestAttemptStreamV2CancellationAfterDecodePreventsVisitor(t *testing.T) {
	reference, objects := attemptStreamV2TestArchive(t, AttemptStreamV2Records, attemptStreamV2TestPages(AttemptStreamV2Records), nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	visits, decoded := 0, 0
	census, err := walkAttemptStreamV2DescriptorsWithPageHook(ctx, AttemptStreamV2Records, reference, attemptStreamV2TestBounds(), attemptStreamV2TestReader(objects), func(AttemptStreamV2Chunk) error { visits++; return nil }, func() { decoded++; cancel() })
	if !errors.Is(err, context.Canceled) || visits != 0 || decoded != 1 || census != (AttemptStreamV2Census{}) {
		t.Fatalf("post-decode cancellation: visits=%d decoded=%d census=%+v err=%v", visits, decoded, census, err)
	}
}
