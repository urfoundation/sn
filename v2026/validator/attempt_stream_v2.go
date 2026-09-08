package validator

// A typed stream has a small manifest and a bounded linked sequence of
// descriptor pages. Data bytes are fetched by their SHA-256 identity, never
// by a mutable path. This schema does not change generic graph/object limits.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
)

const (
	AttemptStreamV2Schema     = "urnetwork-validator-attempt-stream-v2"
	AttemptStreamV2PageSchema = "urnetwork-validator-attempt-stream-page-v2"
	AttemptStreamV2Records    = "records"
	AttemptStreamV2Proofs     = "proofs"
)

// Data and control metadata have separate explicit budgets. Public readers
// obtain these limits from authenticated configuration, not from a manifest.
type AttemptStreamV2Bounds struct {
	MaxDataBytes          uint64
	MaxItems              uint64
	MaxChunkBytes         uint64
	MaxChunks             uint64
	MaxPages              uint64
	MaxPageBytes          uint64
	MaxDescriptorsPerPage uint64
	MaxManifestBytes      uint64
}

// An empty stream has one unique representation: zero counters and zero hash.
// There is no invented empty data object whose absence could mask missing data.
type AttemptStreamV2Reference struct {
	ManifestHash  string `json:"manifest_hash"`
	ManifestBytes uint64 `json:"manifest_bytes"`
	ItemCount     uint64 `json:"item_count"`
	DataBytes     uint64 `json:"data_bytes"`
	ChunkCount    uint64 `json:"chunk_count"`
	PageCount     uint64 `json:"page_count"`
}

// All dimensions must be finite and explicit. Byte/count limits never derive
// from the candidate stream; a large declared count cannot authorize a fetch.
func (self AttemptStreamV2Bounds) Validate() error {
	if self.MaxDataBytes == 0 || self.MaxItems == 0 || self.MaxChunkBytes == 0 || self.MaxChunks == 0 || self.MaxPages == 0 || self.MaxPageBytes == 0 || self.MaxDescriptorsPerPage == 0 || self.MaxManifestBytes == 0 || self.MaxChunkBytes > self.MaxDataBytes || self.MaxPages > self.MaxChunks {
		return errors.New("attempt stream bounds are incomplete or inconsistent")
	}
	return nil
}

// Checks only bounded metadata; every count must later match actual traversal.
func (self AttemptStreamV2Reference) Validate(bounds AttemptStreamV2Bounds) error {
	if err := bounds.Validate(); err != nil {
		return err
	}
	hash, err := canonicalAttemptHex32("attempt stream manifest hash", self.ManifestHash, true)
	if err != nil {
		return err
	}
	if self.ItemCount == 0 {
		if hash != ([32]byte{}) || self.ManifestBytes != 0 || self.DataBytes != 0 || self.ChunkCount != 0 || self.PageCount != 0 {
			return errors.New("empty attempt stream metadata is inconsistent")
		}
		return nil
	}
	if hash == ([32]byte{}) || self.ManifestBytes == 0 || self.ManifestBytes > bounds.MaxManifestBytes || self.ItemCount > bounds.MaxItems || self.DataBytes < self.ItemCount || self.DataBytes > bounds.MaxDataBytes || self.ChunkCount == 0 || self.ChunkCount > bounds.MaxChunks || self.ChunkCount > self.ItemCount || self.PageCount == 0 || self.PageCount > bounds.MaxPages || self.PageCount > self.ChunkCount {
		return errors.New("attempt stream metadata is incomplete or exceeds its bounds")
	}
	if (self.ChunkCount-1)/bounds.MaxDescriptorsPerPage+1 > self.PageCount || (self.DataBytes-1)/bounds.MaxChunkBytes+1 > self.ChunkCount {
		return errors.New("attempt stream metadata cannot fit its declared pages or chunks")
	}
	return nil
}

// The manifest includes totals but not its own hash/size, avoiding a hash cycle.
// Record and proof chunks use the same stream type with distinct signed kinds.
type AttemptStreamV2Manifest struct {
	Schema         string `json:"schema"`
	Kind           string `json:"kind"`
	ItemCount      uint64 `json:"item_count"`
	DataBytes      uint64 `json:"data_bytes"`
	ChunkCount     uint64 `json:"chunk_count"`
	PageCount      uint64 `json:"page_count"`
	FirstPageHash  string `json:"first_page_hash"`
	FirstPageBytes uint64 `json:"first_page_bytes"`
}

// Chunk indices are zero based. Sequence coordinates retain original ledger
// sequence numbers: proof sequences may have gaps, record sequences may not.
type AttemptStreamV2Chunk struct {
	Index         uint64 `json:"index"`
	FirstSequence uint64 `json:"first_sequence"`
	LastSequence  uint64 `json:"last_sequence"`
	ItemCount     uint64 `json:"item_count"`
	DataBytes     uint64 `json:"data_bytes"`
	ContentHash   string `json:"content_hash"`
}

// Nonterminal pages bind the next page's exact hash and size. Producers can
// stage bounded descriptor pages on disk and link them from last to first.
type AttemptStreamV2Page struct {
	Schema        string                 `json:"schema"`
	Kind          string                 `json:"kind"`
	Index         uint64                 `json:"index"`
	Chunks        []AttemptStreamV2Chunk `json:"chunks"`
	NextPageHash  string                 `json:"next_page_hash"`
	NextPageBytes uint64                 `json:"next_page_bytes"`
}

// Distinct typed streams prevent proof data from substituting for record data.
func attemptStreamV2Kind(kind string) bool {
	return kind == AttemptStreamV2Records || kind == AttemptStreamV2Proofs
}

// Encodes bounded manifest metadata and derives its exact signed reference.
func (self AttemptStreamV2Manifest) CanonicalJSON(bounds AttemptStreamV2Bounds) ([]byte, AttemptStreamV2Reference, error) {
	if err := bounds.Validate(); err != nil {
		return nil, AttemptStreamV2Reference{}, err
	}
	if self.Schema != AttemptStreamV2Schema || !attemptStreamV2Kind(self.Kind) {
		return nil, AttemptStreamV2Reference{}, errors.New("attempt stream manifest schema or kind is invalid")
	}
	if _, err := canonicalAttemptHex32("attempt stream first page", self.FirstPageHash, false); err != nil {
		return nil, AttemptStreamV2Reference{}, err
	}
	if self.FirstPageBytes == 0 || self.FirstPageBytes > bounds.MaxPageBytes {
		return nil, AttemptStreamV2Reference{}, errors.New("attempt stream first page exceeds its byte bound")
	}
	encoded, err := json.Marshal(self)
	if err != nil {
		return nil, AttemptStreamV2Reference{}, err
	}
	encoded = append(encoded, '\n')
	reference := AttemptStreamV2Reference{ManifestHash: attemptHex32(sha256.Sum256(encoded)), ManifestBytes: uint64(len(encoded)), ItemCount: self.ItemCount, DataBytes: self.DataBytes, ChunkCount: self.ChunkCount, PageCount: self.PageCount}
	if reference.ItemCount == 0 {
		return nil, AttemptStreamV2Reference{}, errors.New("an empty attempt stream must not have a manifest")
	}
	if err := reference.Validate(bounds); err != nil {
		return nil, AttemptStreamV2Reference{}, err
	}
	return encoded, reference, nil
}

// Validates one bounded page, including local order and a canonical terminal
// link. Full traversal must also verify page/chunk continuity across pages.
func (self AttemptStreamV2Page) CanonicalJSON(bounds AttemptStreamV2Bounds) ([]byte, error) {
	if err := bounds.Validate(); err != nil {
		return nil, err
	}
	if self.Schema != AttemptStreamV2PageSchema || !attemptStreamV2Kind(self.Kind) || self.Index >= bounds.MaxPages || len(self.Chunks) == 0 || uint64(len(self.Chunks)) > bounds.MaxDescriptorsPerPage {
		return nil, errors.New("attempt stream page identity or descriptor count is invalid")
	}
	next, err := canonicalAttemptHex32("attempt stream next page", self.NextPageHash, true)
	if err != nil {
		return nil, err
	}
	if (next == ([32]byte{})) != (self.NextPageBytes == 0) || self.NextPageBytes > bounds.MaxPageBytes {
		return nil, errors.New("attempt stream next page link is incomplete or oversize")
	}
	for index, chunk := range self.Chunks {
		if chunk.Index >= bounds.MaxChunks || chunk.FirstSequence == 0 || chunk.LastSequence < chunk.FirstSequence || chunk.LastSequence == ^uint64(0) || chunk.ItemCount == 0 || chunk.ItemCount > bounds.MaxItems || chunk.DataBytes < chunk.ItemCount || chunk.DataBytes > bounds.MaxChunkBytes {
			return nil, errors.New("attempt stream chunk identity, range or byte bound is invalid")
		}
		if _, err := canonicalAttemptHex32("attempt stream chunk hash", chunk.ContentHash, false); err != nil {
			return nil, err
		}
		span := chunk.LastSequence - chunk.FirstSequence + 1
		if self.Kind == AttemptStreamV2Records && chunk.ItemCount != span || self.Kind == AttemptStreamV2Proofs && chunk.ItemCount > span {
			return nil, errors.New("attempt stream chunk sequence count differs from its kind")
		}
		if index > 0 {
			prior := self.Chunks[index-1]
			if chunk.Index != prior.Index+1 || chunk.FirstSequence <= prior.LastSequence || self.Kind == AttemptStreamV2Records && chunk.FirstSequence != prior.LastSequence+1 {
				return nil, errors.New("attempt stream page has reordered or discontinuous chunks")
			}
		}
	}
	encoded, err := json.Marshal(self)
	if err != nil || uint64(len(encoded))+1 > bounds.MaxPageBytes {
		return nil, errors.Join(errors.New("attempt stream descriptor page exceeds its byte bound"), err)
	}
	return append(encoded, '\n'), nil
}

// Decodes only after byte bounds are known; canonical equality also rejects
// duplicate fields and reordered keys that ordinary JSON parsing would accept.
func decodeAttemptStreamV2JSON(data []byte, limit uint64, destination any) error {
	if limit == 0 || uint64(len(data)) > limit {
		return errors.New("attempt stream metadata exceeds its byte bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("attempt stream metadata contains trailing JSON")
	}
	return nil
}

// Authenticates the complete manifest against the separately signed reference.
// This alone neither fetches its pages nor verifies its declared data totals.
func DecodeAttemptStreamV2Manifest(data []byte, kind string, expected AttemptStreamV2Reference, bounds AttemptStreamV2Bounds) (*AttemptStreamV2Manifest, error) {
	if !attemptStreamV2Kind(kind) {
		return nil, errors.New("expected attempt stream kind is invalid")
	}
	if err := expected.Validate(bounds); err != nil {
		return nil, err
	}
	if expected.ItemCount == 0 || uint64(len(data)) != expected.ManifestBytes || attemptHex32(sha256.Sum256(data)) != expected.ManifestHash {
		return nil, errors.New("attempt stream manifest bytes differ from their signed reference")
	}
	var manifest AttemptStreamV2Manifest
	if err := decodeAttemptStreamV2JSON(data, bounds.MaxManifestBytes, &manifest); err != nil {
		return nil, err
	}
	canonical, reference, err := manifest.CanonicalJSON(bounds)
	if err != nil {
		return nil, err
	}
	if manifest.Kind != kind || reference != expected || !bytes.Equal(canonical, data) {
		return nil, errors.New("attempt stream manifest kind, canonical bytes or totals differ")
	}
	return &manifest, nil
}

// The expected page hash/size come from the authenticated manifest or preceding
// page. Index and kind are caller-derived traversal state, never guessed here.
func DecodeAttemptStreamV2Page(data []byte, kind string, index uint64, expectedHash string, expectedBytes uint64, bounds AttemptStreamV2Bounds) (*AttemptStreamV2Page, error) {
	if err := bounds.Validate(); err != nil {
		return nil, err
	}
	if !attemptStreamV2Kind(kind) || expectedBytes == 0 || expectedBytes > bounds.MaxPageBytes || uint64(len(data)) != expectedBytes || attemptHex32(sha256.Sum256(data)) != expectedHash {
		return nil, errors.New("attempt stream page differs from its authenticated link")
	}
	// Keep the byte-bounded array opaque until its descriptor limit can be
	// enforced before each typed body. A slice decoder allocates every entry
	// before a later count check, including malformed entries outside the cap.
	var encoded struct {
		Schema        string          `json:"schema"`
		Kind          string          `json:"kind"`
		Index         uint64          `json:"index"`
		Chunks        json.RawMessage `json:"chunks"`
		NextPageHash  string          `json:"next_page_hash"`
		NextPageBytes uint64          `json:"next_page_bytes"`
	}
	if err := decodeAttemptStreamV2JSON(data, bounds.MaxPageBytes, &encoded); err != nil {
		return nil, err
	}
	page := AttemptStreamV2Page{Schema: encoded.Schema, Kind: encoded.Kind, Index: encoded.Index, NextPageHash: encoded.NextPageHash, NextPageBytes: encoded.NextPageBytes}
	decoder := json.NewDecoder(bytes.NewReader(encoded.Chunks))
	decoder.DisallowUnknownFields()
	if token, err := decoder.Token(); err != nil || token != json.Delim('[') {
		return nil, errors.Join(errors.New("attempt stream page descriptors are not an array"), err)
	}
	for decoder.More() {
		if uint64(len(page.Chunks)) >= bounds.MaxDescriptorsPerPage {
			return nil, errors.New("attempt stream page descriptor count exceeds its bound")
		}
		var chunk AttemptStreamV2Chunk
		if err := decoder.Decode(&chunk); err != nil {
			return nil, err
		}
		page.Chunks = append(page.Chunks, chunk)
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
		return nil, errors.Join(errors.New("attempt stream page descriptor array is incomplete"), err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("attempt stream page descriptors contain trailing JSON")
	}
	canonical, err := page.CanonicalJSON(bounds)
	if err != nil {
		return nil, err
	}
	if page.Kind != kind || page.Index != index || !bytes.Equal(canonical, data) {
		return nil, errors.New("attempt stream page kind, index or canonical bytes differ")
	}
	return &page, nil
}

// A reader returns owned bytes of exactly the requested authenticated size.
// Implementations must enforce that limit during I/O, not read an unbounded
// response first. Traversal independently checks size, hash and canonical form.
type AttemptStreamV2MetadataReader func(context.Context, string, uint64) ([]byte, error)

// Describes the exact verified descriptor census, not verified record contents.
// A caller accepting data must also read/replay each chunk and its full bytes.
type AttemptStreamV2Census struct {
	FirstSequence uint64
	LastSequence  uint64
	ItemCount     uint64
	DataBytes     uint64
	ChunkCount    uint64
	PageCount     uint64
}

// Keeps only one bounded metadata page. Visitors receive authenticated chunk
// descriptors but must not commit an acceptance verdict before complete
// traversal and data replay succeed. Cancellation or a late census mismatch
// returns an error even after earlier visitor calls have completed.
func WalkAttemptStreamV2Descriptors(ctx context.Context, kind string, reference AttemptStreamV2Reference, bounds AttemptStreamV2Bounds, read AttemptStreamV2MetadataReader, visit func(AttemptStreamV2Chunk) error) (AttemptStreamV2Census, error) {
	return walkAttemptStreamV2DescriptorsWithPageHook(ctx, kind, reference, bounds, read, visit, nil)
}

// The operation-owned hook exposes the boundary between authenticated metadata
// decoding and downstream data work without replacing any validation.
func walkAttemptStreamV2DescriptorsWithPageHook(ctx context.Context, kind string, reference AttemptStreamV2Reference, bounds AttemptStreamV2Bounds, read AttemptStreamV2MetadataReader, visit func(AttemptStreamV2Chunk) error, pageDecoded func()) (AttemptStreamV2Census, error) {
	if ctx == nil || read == nil || visit == nil || !attemptStreamV2Kind(kind) {
		return AttemptStreamV2Census{}, errors.New("attempt stream traversal authority is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return AttemptStreamV2Census{}, err
	}
	if err := reference.Validate(bounds); err != nil {
		return AttemptStreamV2Census{}, err
	}
	if reference.ItemCount == 0 {
		return AttemptStreamV2Census{}, ctx.Err()
	}
	data, err := read(ctx, reference.ManifestHash, reference.ManifestBytes)
	if err != nil {
		return AttemptStreamV2Census{}, err
	}
	if err := ctx.Err(); err != nil {
		return AttemptStreamV2Census{}, err
	}
	manifest, err := DecodeAttemptStreamV2Manifest(data, kind, reference, bounds)
	if err != nil {
		return AttemptStreamV2Census{}, err
	}
	pageHash, pageBytes := manifest.FirstPageHash, manifest.FirstPageBytes
	var census AttemptStreamV2Census
	for index := uint64(0); index < reference.PageCount; index++ {
		if err := ctx.Err(); err != nil {
			return AttemptStreamV2Census{}, err
		}
		data, err := read(ctx, pageHash, pageBytes)
		if err != nil {
			return AttemptStreamV2Census{}, err
		}
		if err := ctx.Err(); err != nil {
			return AttemptStreamV2Census{}, err
		}
		page, err := DecodeAttemptStreamV2Page(data, kind, index, pageHash, pageBytes, bounds)
		if err != nil {
			return AttemptStreamV2Census{}, err
		}
		if pageDecoded != nil {
			pageDecoded()
		}
		if err := ctx.Err(); err != nil {
			return AttemptStreamV2Census{}, err
		}
		lastPage := index == reference.PageCount-1
		if lastPage != (page.NextPageHash == zeroAttemptHash()) {
			return AttemptStreamV2Census{}, errors.New("attempt stream page chain is truncated or has an extra suffix")
		}
		for _, chunk := range page.Chunks {
			if err := ctx.Err(); err != nil {
				return AttemptStreamV2Census{}, err
			}
			if chunk.Index != census.ChunkCount || census.ChunkCount >= reference.ChunkCount || chunk.ItemCount > reference.ItemCount-census.ItemCount || chunk.DataBytes > reference.DataBytes-census.DataBytes {
				return AttemptStreamV2Census{}, errors.New("attempt stream descriptor count or totals differ")
			}
			if census.ChunkCount == 0 {
				census.FirstSequence = chunk.FirstSequence
			} else if chunk.FirstSequence <= census.LastSequence || kind == AttemptStreamV2Records && chunk.FirstSequence != census.LastSequence+1 {
				return AttemptStreamV2Census{}, errors.New("attempt stream sequence is reordered or discontinuous across pages")
			}
			census.LastSequence = chunk.LastSequence
			census.ItemCount += chunk.ItemCount
			census.DataBytes += chunk.DataBytes
			census.ChunkCount++
			if err := ctx.Err(); err != nil {
				return AttemptStreamV2Census{}, err
			}
			if err := visit(chunk); err != nil {
				return AttemptStreamV2Census{}, err
			}
			if err := ctx.Err(); err != nil {
				return AttemptStreamV2Census{}, err
			}
		}
		census.PageCount++
		pageHash, pageBytes = page.NextPageHash, page.NextPageBytes
	}
	if census.ItemCount != reference.ItemCount || census.DataBytes != reference.DataBytes || census.ChunkCount != reference.ChunkCount || census.PageCount != reference.PageCount {
		return AttemptStreamV2Census{}, errors.New("attempt stream complete descriptor census differs from its signed reference")
	}
	if err := ctx.Err(); err != nil {
		return AttemptStreamV2Census{}, err
	}
	return census, nil
}
