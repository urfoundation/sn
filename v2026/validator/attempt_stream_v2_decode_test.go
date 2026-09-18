package validator

// Bounds typed descriptor decoding independently of the raw page-byte budget.
// These metadata-only fixtures do not claim data-chunk or record verification.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

// Produces a canonical terminal page with contiguous opaque chunk descriptors.
func attemptStreamV2DescriptorPage(t *testing.T, count int) (AttemptStreamV2Page, AttemptStreamV2Bounds, []byte) {
	t.Helper()
	bounds := attemptStreamV2TestBounds()
	bounds.MaxDescriptorsPerPage, bounds.MaxPageBytes = uint64(count), 256*1024
	page := AttemptStreamV2Page{Schema: AttemptStreamV2PageSchema, Kind: AttemptStreamV2Records, NextPageHash: zeroAttemptHash()}
	for index := range count {
		page.Chunks = append(page.Chunks, AttemptStreamV2Chunk{Index: uint64(index), FirstSequence: uint64(index*4 + 1), LastSequence: uint64((index + 1) * 4), ItemCount: 4, DataBytes: 400, ContentHash: attemptHex32(sha256.Sum256([]byte("opaque descriptor test data")))})
	}
	raw, err := page.CanonicalJSON(bounds)
	if err != nil {
		t.Fatal(err)
	}
	return page, bounds, raw
}

// A long homogeneous tail of typed-invalid bodies must have the same count
// failure as one extra body; no tail member is authorized for typed decoding.
func TestAttemptStreamV2BoundsHugeDescriptorTails(t *testing.T) {
	for _, sample := range []struct{ limit, extras int }{
		{limit: 1, extras: 1},
		{limit: 1, extras: 4096},
		{limit: 4, extras: 4096},
	} {
		_, bounds, raw := attemptStreamV2DescriptorPage(t, sample.limit)
		ending := []byte(`],"next_page_hash"`)
		replacement := append(bytes.Repeat([]byte(`,"must-not-decode-descriptor"`), sample.extras), ending...)
		raw = bytes.Replace(raw, ending, replacement, 1)
		if uint64(len(raw)) > bounds.MaxPageBytes || !json.Valid(raw) {
			t.Fatal("large descriptor-tail fixture is not within its authenticated byte budget")
		}
		page, err := DecodeAttemptStreamV2Page(raw, AttemptStreamV2Records, 0, attemptHex32(sha256.Sum256(raw)), uint64(len(raw)), bounds)
		if page != nil || err == nil || !strings.Contains(err.Error(), "descriptor count") {
			t.Fatalf("limit=%d extras=%d: decoded a forbidden descriptor body: %v", sample.limit, sample.extras, err)
		}
	}
}

// Exact per-page limits retain every valid descriptor, including a larger
// page. The bound is not an off-by-one rejection or a hidden fixed limit.
func TestAttemptStreamV2DecodesExactDescriptorCounts(t *testing.T) {
	for _, count := range []int{1, 2, 64} {
		want, bounds, raw := attemptStreamV2DescriptorPage(t, count)
		page, err := DecodeAttemptStreamV2Page(raw, AttemptStreamV2Records, 0, attemptHex32(sha256.Sum256(raw)), uint64(len(raw)), bounds)
		if err != nil || page == nil || !slices.Equal(page.Chunks, want.Chunks) {
			t.Fatalf("exact descriptor count %d: page=%v err=%v", count, page, err)
		}
		encoded, err := page.CanonicalJSON(bounds)
		if err != nil || !bytes.Equal(encoded, raw) {
			t.Fatalf("exact descriptor count %d changed canonical bytes: %v", count, err)
		}
	}
}

// Every body within the count limit still reaches the strict typed decoder;
// count-based refusal cannot hide an invalid first or final permitted entry.
func TestAttemptStreamV2RejectsMalformedDescriptorWithinBound(t *testing.T) {
	_, bounds, raw := attemptStreamV2DescriptorPage(t, 2)
	for _, mutation := range []struct {
		name string
		old  string
		new  string
		want string
	}{
		{name: "first index type", old: `"index":0,"first_sequence"`, new: `"index":"first","first_sequence"`, want: "cannot unmarshal string"},
		{name: "last index type", old: `"index":1,"first_sequence"`, new: `"index":"last","first_sequence"`, want: "cannot unmarshal string"},
		{name: "unknown chunk field", old: `"index":1,"first_sequence"`, new: `"extra":0,"index":1,"first_sequence"`, want: "unknown field"},
	} {
		changed := bytes.Replace(raw, []byte(mutation.old), []byte(mutation.new), 1)
		if bytes.Equal(changed, raw) {
			t.Fatal("descriptor mutation did not change its target")
		}
		page, err := DecodeAttemptStreamV2Page(changed, AttemptStreamV2Records, 0, attemptHex32(sha256.Sum256(changed)), uint64(len(changed)), bounds)
		if page != nil || err == nil || !strings.Contains(err.Error(), mutation.want) || strings.Contains(err.Error(), "descriptor count") {
			t.Fatalf("%s: permitted malformed body did not reach strict decoding: %v", mutation.name, err)
		}
	}
}

// Separating the array decoder must preserve outer and nested canonical
// identity, strict fields, JSON framing, and the required array shape.
func TestAttemptStreamV2PageRetainsStrictCanonicalDecoding(t *testing.T) {
	page, bounds, raw := attemptStreamV2DescriptorPage(t, 2)
	chunks, err := json.Marshal(page.Chunks)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		name string
		data []byte
	}{
		{name: "leading whitespace", data: append([]byte(" "), raw...)},
		{name: "missing newline", data: bytes.Clone(raw[:len(raw)-1])},
		{name: "trailing JSON", data: append(bytes.Clone(raw), '{', '}')},
		{name: "outer duplicate key", data: bytes.Replace(raw, []byte(`{"schema":`), []byte(`{"kind":"records","schema":`), 1)},
		{name: "outer unknown key", data: bytes.Replace(raw, []byte(`{"schema":`), []byte(`{"extra":0,"schema":`), 1)},
		{name: "nested duplicate key", data: bytes.Replace(raw, []byte(`"index":1,"first_sequence"`), []byte(`"index":1,"index":1,"first_sequence"`), 1)},
		{name: "array whitespace", data: bytes.Replace(raw, []byte(`"chunks":[`), []byte(`"chunks":[ `), 1)},
		{name: "empty array", data: bytes.Replace(raw, chunks, []byte(`[]`), 1)},
		{name: "null array", data: bytes.Replace(raw, chunks, []byte(`null`), 1)},
		{name: "object array", data: bytes.Replace(raw, chunks, []byte(`{}`), 1)},
		{name: "null descriptor", data: bytes.Replace(raw, chunks, []byte(`[null]`), 1)},
	} {
		decoded, err := DecodeAttemptStreamV2Page(mutation.data, AttemptStreamV2Records, 0, attemptHex32(sha256.Sum256(mutation.data)), uint64(len(mutation.data)), bounds)
		if decoded != nil || err == nil {
			t.Errorf("%s was accepted with freshly authenticated bytes", mutation.name)
		}
	}
}

// Descriptor caps apply separately to each page, while global counts and
// original record sequences remain exact across the complete linked stream.
func TestAttemptStreamV2PerPageBoundKeepsGlobalCensus(t *testing.T) {
	whole, bounds, _ := attemptStreamV2DescriptorPage(t, 4)
	bounds.MaxDescriptorsPerPage = 2
	pages := []AttemptStreamV2Page{
		{Schema: AttemptStreamV2PageSchema, Kind: AttemptStreamV2Records, Index: 0, Chunks: whole.Chunks[:2]},
		{Schema: AttemptStreamV2PageSchema, Kind: AttemptStreamV2Records, Index: 1, Chunks: whole.Chunks[2:]},
	}
	reference, objects := attemptStreamV2TestArchive(t, AttemptStreamV2Records, pages, nil, func(manifest *AttemptStreamV2Manifest) {
		manifest.ItemCount, manifest.DataBytes, manifest.ChunkCount = 16, 1600, 4
	})
	visits := uint64(0)
	census, err := WalkAttemptStreamV2Descriptors(context.Background(), AttemptStreamV2Records, reference, bounds, attemptStreamV2TestReader(objects), func(chunk AttemptStreamV2Chunk) error {
		if chunk.Index != visits {
			t.Fatal("page boundary changed the global descriptor sequence")
		}
		visits++
		return nil
	})
	if err != nil || visits != 4 || census != (AttemptStreamV2Census{FirstSequence: 1, LastSequence: 16, ItemCount: 16, DataBytes: 1600, ChunkCount: 4, PageCount: 2}) {
		t.Fatalf("per-page descriptor bounds changed the complete census: visits=%d census=%+v err=%v", visits, census, err)
	}
}

// A later authenticated page cannot start data work after cancellation even
// when prior pages already delivered legitimate descriptors to the visitor.
func TestAttemptStreamV2CancellationAfterLaterPagePreventsVisitor(t *testing.T) {
	reference, objects := attemptStreamV2TestArchive(t, AttemptStreamV2Records, attemptStreamV2TestPages(AttemptStreamV2Records), nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	visits, decoded := 0, 0
	census, err := walkAttemptStreamV2DescriptorsWithPageHook(ctx, AttemptStreamV2Records, reference, attemptStreamV2TestBounds(), attemptStreamV2TestReader(objects), func(AttemptStreamV2Chunk) error {
		visits++
		return nil
	}, func() {
		decoded++
		if decoded == 2 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || visits != 1 || decoded != 2 || census != (AttemptStreamV2Census{}) {
		t.Fatalf("later-page cancellation: visits=%d decoded=%d census=%+v err=%v", visits, decoded, census, err)
	}
}

// Models cancellation immediately after an already sampled nil error. A
// synchronous owner arms this boundary; no scheduler or wall clock is involved.
type attemptStreamV2CancelAfterSample struct {
	context.Context
	cancel context.CancelFunc
	armed  bool
}

// Returns the sampled error, then publishes cancellation for the next check.
func (self *attemptStreamV2CancelAfterSample) Err() error {
	err := self.Context.Err()
	if self.armed && err == nil {
		self.armed = false
		self.cancel()
	}
	return err
}

// A nil post-visitor sample is not a final acceptance verdict: cancellation
// published immediately afterward must be rechecked before successful return.
func TestAttemptStreamV2CancellationAfterLastVisitorCheckFailsClosed(t *testing.T) {
	reference, objects := attemptStreamV2TestArchive(t, AttemptStreamV2Records, attemptStreamV2TestPages(AttemptStreamV2Records), nil, nil)
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &attemptStreamV2CancelAfterSample{Context: base, cancel: cancel}
	visits := uint64(0)
	census, err := WalkAttemptStreamV2Descriptors(ctx, AttemptStreamV2Records, reference, attemptStreamV2TestBounds(), attemptStreamV2TestReader(objects), func(AttemptStreamV2Chunk) error {
		visits++
		if visits == reference.ChunkCount {
			ctx.armed = true
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || visits != reference.ChunkCount || census != (AttemptStreamV2Census{}) {
		t.Fatalf("final cancellation sample: visits=%d census=%+v err=%v", visits, census, err)
	}
}

// The unique empty representation still needs a final cancellation check;
// the absence of metadata reads must not authorize a canceled operation.
func TestAttemptStreamV2EmptyStreamRechecksCancellationBeforeSuccess(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &attemptStreamV2CancelAfterSample{Context: base, cancel: cancel, armed: true}
	census, err := WalkAttemptStreamV2Descriptors(ctx, AttemptStreamV2Records, AttemptStreamV2Reference{ManifestHash: zeroAttemptHash()}, attemptStreamV2TestBounds(), func(context.Context, string, uint64) ([]byte, error) {
		t.Fatal("empty canceled stream fetched metadata")
		return nil, nil
	}, func(AttemptStreamV2Chunk) error {
		t.Fatal("empty canceled stream emitted a descriptor")
		return nil
	})
	if !errors.Is(err, context.Canceled) || census != (AttemptStreamV2Census{}) {
		t.Fatalf("empty cancellation sample: census=%+v err=%v", census, err)
	}
}
