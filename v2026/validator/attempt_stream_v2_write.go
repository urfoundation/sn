package validator

// Stream production holds one bounded data chunk and descriptor page in RAM.
// A fixed-width, hash-chained descriptor spool lets pages be linked backwards
// without retaining a history-sized index. Only the final manifest is a result.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const attemptStreamV2DescriptorBytes = 104

// The source synchronously visits rows in increasing original ledger sequence.
// Rows are borrowed only during the call. Records must be contiguous; completed
// proofs retain their original, possibly gapped, terminal record coordinates.
// Sources must stop on a visitor error and must not invoke it concurrently or
// retain it after returning. The writer also latches errors against omission.
type AttemptStreamV2RowSource func(context.Context, func(uint64, []byte) error) error

// Successful writes mean the exact content-addressed bytes are durable. The
// callback owns its byte slice and must honor cancellation. Referenced objects
// are staged, not accepted: no caller may publish a signed cut before full
// replay and fetch-back. Metadata remains inside the ordinary control budget.
type AttemptStreamV2ObjectWriter func(context.Context, string, []byte) error

// Spool is caller-owned, empty, private scratch with no concurrent access; the
// caller closes/retains it on every outcome. Its maximum written length is
// MaxChunks*104. It must honor context cancellation between bounded operations.
// Data writers are separately typed by the enclosing records/proofs invocation.
// This codec checks framing, coordinates and complete counts, not signatures or
// typed canonical record semantics; the cut sealer must perform full replay.
type AttemptStreamV2WriteOptions struct {
	Bounds        AttemptStreamV2Bounds
	MaxRowBytes   uint64
	Spool         io.ReadWriteSeeker
	WriteData     AttemptStreamV2ObjectWriter
	WriteMetadata AttemptStreamV2ObjectWriter
}

// All state belongs to one synchronous write; no lock spans a source or sink.
type attemptStreamV2Writer struct {
	ctx                context.Context
	kind               string
	options            AttemptStreamV2WriteOptions
	descriptorsPerPage uint64
	census             AttemptStreamV2Census
	chunk              AttemptStreamV2Chunk
	data               []byte
	descriptorHash     [32]byte
	fault              error
	spoolStarted       bool
}

// Derives a conservative page capacity from the actual JSON representation.
// The calculation allocates one descriptor, never a candidate-sized slice.
func attemptStreamV2WritePageCapacity(kind string, options AttemptStreamV2WriteOptions) (uint64, error) {
	bounds := options.Bounds
	if err := bounds.Validate(); err != nil {
		return 0, err
	}
	maximumInt := uint64(^uint(0) >> 1)
	if options.MaxRowBytes == 0 || options.MaxRowBytes > bounds.MaxChunkBytes || bounds.MaxChunkBytes > maximumInt/2 || bounds.MaxPageBytes > maximumInt/2 || bounds.MaxChunks > uint64(1<<63-1)/attemptStreamV2DescriptorBytes {
		return 0, errors.New("attempt stream writer allocation or spool bounds are invalid")
	}
	chunk, err := json.Marshal(AttemptStreamV2Chunk{
		Index: bounds.MaxChunks - 1, FirstSequence: ^uint64(0) - 1, LastSequence: ^uint64(0) - 1,
		ItemCount: bounds.MaxItems, DataBytes: bounds.MaxChunkBytes, ContentHash: zeroAttemptHash(),
	})
	if err != nil {
		return 0, err
	}
	page, err := json.Marshal(AttemptStreamV2Page{
		Schema: AttemptStreamV2PageSchema, Kind: kind, Index: bounds.MaxPages - 1,
		Chunks: []AttemptStreamV2Chunk{}, NextPageHash: zeroAttemptHash(), NextPageBytes: bounds.MaxPageBytes,
	})
	if err != nil {
		return 0, err
	}
	// The final newline offsets the absent comma after the last descriptor.
	if bounds.MaxPageBytes <= uint64(len(page)) {
		return 0, errors.New("attempt stream writer page cannot fit one descriptor")
	}
	capacity := (bounds.MaxPageBytes - uint64(len(page))) / uint64(len(chunk)+1)
	if capacity == 0 {
		return 0, errors.New("attempt stream writer page cannot fit one descriptor")
	}
	return min(capacity, bounds.MaxDescriptorsPerPage), nil
}

// Encodes a complete typed stream. Cancellation, any source/sink/spool failure
// or a late integrity mismatch returns zero results, even after staged writes.
// Empty sources return the unique empty reference and perform no storage I/O.
func WriteAttemptStreamV2(ctx context.Context, kind string, source AttemptStreamV2RowSource, options AttemptStreamV2WriteOptions) (AttemptStreamV2Reference, AttemptStreamV2Census, error) {
	if ctx == nil || source == nil || options.Spool == nil || options.WriteData == nil || options.WriteMetadata == nil || !attemptStreamV2Kind(kind) {
		return AttemptStreamV2Reference{}, AttemptStreamV2Census{}, errors.New("attempt stream writer authority is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return AttemptStreamV2Reference{}, AttemptStreamV2Census{}, err
	}
	capacity, err := attemptStreamV2WritePageCapacity(kind, options)
	if err != nil {
		return AttemptStreamV2Reference{}, AttemptStreamV2Census{}, err
	}
	writer := &attemptStreamV2Writer{ctx: ctx, kind: kind, options: options, descriptorsPerPage: capacity}
	err = source(ctx, func(sequence uint64, row []byte) error {
		if writer.fault == nil {
			writer.fault = writer.append(sequence, row)
		}
		return writer.fault
	})
	if err := errors.Join(err, writer.fault, ctx.Err()); err != nil {
		return AttemptStreamV2Reference{}, AttemptStreamV2Census{}, err
	}
	if writer.census.ItemCount == 0 {
		return AttemptStreamV2Reference{ManifestHash: zeroAttemptHash()}, AttemptStreamV2Census{}, nil
	}
	if err := writer.flush(); err != nil {
		return AttemptStreamV2Reference{}, AttemptStreamV2Census{}, err
	}
	reference, err := writer.seal()
	if err != nil {
		return AttemptStreamV2Reference{}, AttemptStreamV2Census{}, err
	}
	return reference, writer.census, nil
}

// Checks returned offsets as well as errors. A faulty scratch implementation
// cannot redirect writes into a prefix or silently substitute a different row.
func (self *attemptStreamV2Writer) seek(offset int64, whence int) (int64, error) {
	if err := self.ctx.Err(); err != nil {
		return 0, err
	}
	position, err := self.options.Spool.Seek(offset, whence)
	if err := errors.Join(err, self.ctx.Err()); err != nil {
		return 0, err
	}
	if position < 0 || whence == io.SeekStart && position != offset {
		return 0, errors.New("attempt stream descriptor spool returned an invalid offset")
	}
	return position, nil
}

// Validates before flushing or accepting a row, and copies its borrowed bytes.
// Syntactic JSONL framing is deliberately separate from full cut authentication.
func (self *attemptStreamV2Writer) append(sequence uint64, row []byte) error {
	if err := self.ctx.Err(); err != nil {
		return err
	}
	bounds := self.options.Bounds
	if len(row) == 0 || uint64(len(row)) > self.options.MaxRowBytes || row[len(row)-1] != '\n' || bytes.IndexByte(row[:len(row)-1], '\n') >= 0 || !json.Valid(row[:len(row)-1]) {
		return errors.New("attempt stream writer row is oversized or not one complete JSONL value")
	}
	if sequence == 0 || sequence == ^uint64(0) || self.census.ItemCount > 0 && (sequence <= self.census.LastSequence || self.kind == AttemptStreamV2Records && sequence != self.census.LastSequence+1) {
		return errors.New("attempt stream writer row sequence is invalid or discontinuous")
	}
	if self.census.ItemCount >= bounds.MaxItems || uint64(len(row)) > bounds.MaxDataBytes-self.census.DataBytes {
		return errors.New("attempt stream writer exceeds its complete item or data bound")
	}
	if !self.spoolStarted {
		size, err := self.seek(0, io.SeekEnd)
		if err != nil {
			return err
		}
		if size != 0 {
			return errors.New("attempt stream descriptor spool must be empty")
		}
		if _, err := self.seek(0, io.SeekStart); err != nil {
			return err
		}
		self.spoolStarted = true
	}
	if uint64(len(row)) > bounds.MaxChunkBytes-uint64(len(self.data)) {
		if err := self.flush(); err != nil {
			return err
		}
	}
	if len(self.data) == 0 {
		if self.census.ChunkCount >= bounds.MaxChunks || self.census.ChunkCount/self.descriptorsPerPage >= bounds.MaxPages {
			return errors.New("attempt stream writer exceeds its chunk or page count bound")
		}
		self.chunk = AttemptStreamV2Chunk{Index: self.census.ChunkCount, FirstSequence: sequence}
	}
	if err := self.ctx.Err(); err != nil {
		return err
	}
	self.data = append(self.data, row...)
	self.chunk.LastSequence = sequence
	self.chunk.ItemCount++
	self.chunk.DataBytes += uint64(len(row))
	if self.census.ItemCount == 0 {
		self.census.FirstSequence = sequence
	}
	self.census.LastSequence = sequence
	self.census.ItemCount++
	self.census.DataBytes += uint64(len(row))
	return nil
}

// Durably stages one complete chunk, then records its authenticated descriptor.
// The data callback receives ownership; later chunks never reuse that buffer.
func (self *attemptStreamV2Writer) flush() error {
	if err := self.ctx.Err(); err != nil {
		return err
	}
	if len(self.data) == 0 {
		return nil
	}
	self.chunk.ContentHash = attemptHex32(sha256.Sum256(self.data))
	data := self.data
	self.data = nil
	if err := self.options.WriteData(self.ctx, self.chunk.ContentHash, data); err != nil {
		return err
	}
	if err := self.ctx.Err(); err != nil {
		return err
	}
	var descriptor [attemptStreamV2DescriptorBytes]byte
	for index, value := range []uint64{self.chunk.Index, self.chunk.FirstSequence, self.chunk.LastSequence, self.chunk.ItemCount, self.chunk.DataBytes} {
		binary.BigEndian.PutUint64(descriptor[index*8:], value)
	}
	digest, err := canonicalAttemptHex32("attempt stream chunk hash", self.chunk.ContentHash, false)
	if err != nil {
		return err
	}
	copy(descriptor[40:72], digest[:])
	copy(descriptor[72:], self.descriptorHash[:])
	if _, err := self.seek(int64(self.census.ChunkCount*attemptStreamV2DescriptorBytes), io.SeekStart); err != nil {
		return err
	}
	nextDescriptorHash := sha256.Sum256(descriptor[:])
	n, err := self.options.Spool.Write(descriptor[:])
	if n != len(descriptor) {
		err = errors.Join(err, io.ErrShortWrite)
	}
	if err := errors.Join(err, self.ctx.Err()); err != nil {
		return err
	}
	self.descriptorHash = nextDescriptorHash
	self.census.ChunkCount++
	return nil
}

// Reads one descriptor against the rolling anchor before interpreting fields.
// Original sequence order is authenticated even while traversing in reverse.
func (self *attemptStreamV2Writer) readDescriptor(index uint64, expectedHash [32]byte) (AttemptStreamV2Chunk, [32]byte, error) {
	if _, err := self.seek(int64(index*attemptStreamV2DescriptorBytes), io.SeekStart); err != nil {
		return AttemptStreamV2Chunk{}, [32]byte{}, err
	}
	var encoded [attemptStreamV2DescriptorBytes]byte
	for offset := 0; offset < len(encoded); {
		if err := self.ctx.Err(); err != nil {
			return AttemptStreamV2Chunk{}, [32]byte{}, err
		}
		n, err := self.options.Spool.Read(encoded[offset:])
		if n < 0 || n > len(encoded)-offset {
			return AttemptStreamV2Chunk{}, [32]byte{}, errors.New("attempt stream descriptor spool returned an invalid read count")
		}
		offset += n
		if err := self.ctx.Err(); err != nil {
			return AttemptStreamV2Chunk{}, [32]byte{}, err
		}
		if offset == len(encoded) && err == io.EOF {
			err = nil
		}
		if err != nil {
			return AttemptStreamV2Chunk{}, [32]byte{}, err
		}
		if n == 0 {
			return AttemptStreamV2Chunk{}, [32]byte{}, io.ErrNoProgress
		}
	}
	if sha256.Sum256(encoded[:]) != expectedHash {
		return AttemptStreamV2Chunk{}, [32]byte{}, errors.New("attempt stream descriptor spool hash differs")
	}
	chunk := AttemptStreamV2Chunk{
		Index: binary.BigEndian.Uint64(encoded[:8]), FirstSequence: binary.BigEndian.Uint64(encoded[8:16]),
		LastSequence: binary.BigEndian.Uint64(encoded[16:24]), ItemCount: binary.BigEndian.Uint64(encoded[24:32]),
		DataBytes: binary.BigEndian.Uint64(encoded[32:40]), ContentHash: attemptHex32([32]byte(encoded[40:72])),
	}
	if chunk.Index != index {
		return AttemptStreamV2Chunk{}, [32]byte{}, errors.New("attempt stream descriptor spool index differs")
	}
	return chunk, [32]byte(encoded[72:]), nil
}

// Emits pages last-to-first, then the manifest. The spool chain and complete
// reverse census must match all accepted rows before a reference is returned.
func (self *attemptStreamV2Writer) seal() (AttemptStreamV2Reference, error) {
	size, err := self.seek(0, io.SeekEnd)
	if err != nil {
		return AttemptStreamV2Reference{}, err
	}
	if uint64(size) != self.census.ChunkCount*attemptStreamV2DescriptorBytes {
		return AttemptStreamV2Reference{}, errors.New("attempt stream descriptor spool has a missing or extra suffix")
	}
	self.census.PageCount = (self.census.ChunkCount-1)/self.descriptorsPerPage + 1
	nextHash, nextBytes := zeroAttemptHash(), uint64(0)
	expectedHash := self.descriptorHash
	var reverseItems, reverseBytes, reverseChunks uint64
	nextFirst := self.census.LastSequence + 1
	for index := self.census.PageCount; index > 0; {
		index--
		if err := self.ctx.Err(); err != nil {
			return AttemptStreamV2Reference{}, err
		}
		firstChunk := index * self.descriptorsPerPage
		count := min(self.descriptorsPerPage, self.census.ChunkCount-firstChunk)
		page := AttemptStreamV2Page{Schema: AttemptStreamV2PageSchema, Kind: self.kind, Index: index, Chunks: make([]AttemptStreamV2Chunk, int(count)), NextPageHash: nextHash, NextPageBytes: nextBytes}
		for offset := count; offset > 0; {
			offset--
			chunk, priorHash, err := self.readDescriptor(firstChunk+offset, expectedHash)
			if err != nil {
				return AttemptStreamV2Reference{}, err
			}
			if chunk.ItemCount > self.census.ItemCount-reverseItems || chunk.DataBytes > self.census.DataBytes-reverseBytes || chunk.LastSequence >= nextFirst || self.kind == AttemptStreamV2Records && chunk.LastSequence != nextFirst-1 {
				return AttemptStreamV2Reference{}, errors.New("attempt stream descriptor spool census or sequence differs")
			}
			page.Chunks[offset] = chunk
			expectedHash = priorHash
			nextFirst = chunk.FirstSequence
			reverseItems += chunk.ItemCount
			reverseBytes += chunk.DataBytes
			reverseChunks++
		}
		encoded, err := page.CanonicalJSON(self.options.Bounds)
		if err != nil {
			return AttemptStreamV2Reference{}, err
		}
		if err := self.ctx.Err(); err != nil {
			return AttemptStreamV2Reference{}, err
		}
		nextHash, nextBytes = attemptHex32(sha256.Sum256(encoded)), uint64(len(encoded))
		if err := self.options.WriteMetadata(self.ctx, nextHash, encoded); err != nil {
			return AttemptStreamV2Reference{}, err
		}
	}
	if err := self.ctx.Err(); err != nil {
		return AttemptStreamV2Reference{}, err
	}
	if expectedHash != ([32]byte{}) || nextFirst != self.census.FirstSequence || reverseItems != self.census.ItemCount || reverseBytes != self.census.DataBytes || reverseChunks != self.census.ChunkCount {
		return AttemptStreamV2Reference{}, errors.New("attempt stream descriptor spool complete census differs")
	}
	manifest := AttemptStreamV2Manifest{
		Schema: AttemptStreamV2Schema, Kind: self.kind, ItemCount: self.census.ItemCount,
		DataBytes: self.census.DataBytes, ChunkCount: self.census.ChunkCount, PageCount: self.census.PageCount,
		FirstPageHash: nextHash, FirstPageBytes: nextBytes,
	}
	encoded, reference, err := manifest.CanonicalJSON(self.options.Bounds)
	if err != nil {
		return AttemptStreamV2Reference{}, err
	}
	if err := self.options.WriteMetadata(self.ctx, reference.ManifestHash, encoded); err != nil {
		return AttemptStreamV2Reference{}, fmt.Errorf("attempt stream manifest persistence: %w", err)
	}
	if err := self.ctx.Err(); err != nil {
		return AttemptStreamV2Reference{}, err
	}
	return reference, nil
}
