package validator

// Real temporary descriptor files exercise streaming production and recovery
// boundaries. Small JSON rows test this codec only, never signature validity.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixture object maps capture the exact owned bytes accepted by each sink.
type attemptStreamV2WriteFixture struct {
	options  AttemptStreamV2WriteOptions
	spool    *os.File
	dataKVs  map[string][]byte
	metaKVs  map[string][]byte
	putKinds []string
}

// Creates one exclusive private spool; no existing path is reused or erased.
func newAttemptStreamV2WriteFixture(t *testing.T) *attemptStreamV2WriteFixture {
	t.Helper()
	spool, err := os.OpenFile(filepath.Join(t.TempDir(), "descriptors"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := spool.Close(); err != nil {
			t.Error(err)
		}
	})
	self := &attemptStreamV2WriteFixture{spool: spool, dataKVs: map[string][]byte{}, metaKVs: map[string][]byte{}}
	bounds := attemptStreamV2TestBounds()
	bounds.MaxChunkBytes = 30
	self.options = AttemptStreamV2WriteOptions{Bounds: bounds, MaxRowBytes: 30, Spool: spool}
	self.options.WriteData = func(ctx context.Context, hash string, data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if uint64(len(data)) > bounds.MaxChunkBytes || hash != attemptHex32(sha256.Sum256(data)) {
			t.Fatal("data sink received an invalid length or hash")
		}
		self.putKinds = append(self.putKinds, "data")
		self.dataKVs[hash] = data
		return nil
	}
	self.options.WriteMetadata = func(ctx context.Context, hash string, data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var object struct {
			Schema string `json:"schema"`
		}
		if err := json.Unmarshal(data, &object); err != nil || hash != attemptHex32(sha256.Sum256(data)) {
			t.Fatalf("metadata sink received invalid bytes: %v", err)
		}
		self.putKinds = append(self.putKinds, object.Schema)
		self.metaKVs[hash] = data
		return nil
	}
	return self
}

// Source rows have real original coordinates and are borrowed until return.
func attemptStreamV2WriteRows(sequences ...uint64) AttemptStreamV2RowSource {
	return func(ctx context.Context, visit func(uint64, []byte) error) error {
		for _, sequence := range sequences {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := visit(sequence, []byte(fmt.Sprintf("{\"sequence\":%d}\n", sequence))); err != nil {
				return err
			}
		}
		return nil
	}
}

// Fetches every descriptor and exact data byte using the public stream reader.
func (self *attemptStreamV2WriteFixture) verify(t *testing.T, kind string, reference AttemptStreamV2Reference, want AttemptStreamV2Census, sequences []uint64) {
	t.Helper()
	var rows [][]byte
	census, err := WalkAttemptStreamV2Descriptors(context.Background(), kind, reference, self.options.Bounds, attemptStreamV2TestReader(self.metaKVs), func(chunk AttemptStreamV2Chunk) error {
		data, found := self.dataKVs[chunk.ContentHash]
		if !found || uint64(len(data)) != chunk.DataBytes || attemptHex32(sha256.Sum256(data)) != chunk.ContentHash || data[len(data)-1] != '\n' {
			return errors.New("sealed data changed or is missing")
		}
		chunkRows := bytes.Split(data[:len(data)-1], []byte{'\n'})
		if uint64(len(chunkRows)) != chunk.ItemCount {
			return errors.New("sealed chunk row count differs")
		}
		rows = append(rows, chunkRows...)
		return nil
	})
	if err != nil || census != want || len(rows) != len(sequences) {
		t.Fatalf("sealed complete census: %+v want %+v rows=%d error=%v", census, want, len(rows), err)
	}
	for index, sequence := range sequences {
		if string(rows[index]) != fmt.Sprintf("{\"sequence\":%d}", sequence) {
			t.Fatalf("row %d changed: %s", index, rows[index])
		}
	}
	info, err := self.spool.Stat()
	if err != nil || uint64(info.Size()) != want.ChunkCount*attemptStreamV2DescriptorBytes {
		t.Fatalf("descriptor spool length differs: %v", err)
	}
	if self.putKinds[len(self.putKinds)-1] != AttemptStreamV2Schema {
		t.Fatal("manifest was not the final publication")
	}
}

// Complete records cross chunk and page boundaries without coordinate loss.
func TestAttemptStreamV2WriteCompleteRecordCensus(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	sequences := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9}
	reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, attemptStreamV2WriteRows(sequences...), fixture.options)
	if err != nil || census.ItemCount != 9 || census.ChunkCount != 5 || census.PageCount != 3 {
		t.Fatalf("complete writer: %+v %v", census, err)
	}
	fixture.verify(t, AttemptStreamV2Records, reference, census, sequences)
}

// Proof row coordinates keep gaps from nonterminal/failed source records.
func TestAttemptStreamV2WriteGappedProofCoordinates(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	sequences := []uint64{8, 16, 51, 92, ^uint64(0) - 1}
	fixture.options.Bounds.MaxChunkBytes = 60
	fixture.options.MaxRowBytes = 60
	fixture.options.WriteData = func(_ context.Context, hash string, data []byte) error {
		fixture.dataKVs[hash] = data
		fixture.putKinds = append(fixture.putKinds, "data")
		return nil
	}
	reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Proofs, attemptStreamV2WriteRows(sequences...), fixture.options)
	if err != nil || census.FirstSequence != 8 || census.LastSequence != ^uint64(0)-1 || census.ItemCount != 5 {
		t.Fatalf("proof writer: %+v %v", census, err)
	}
	fixture.verify(t, AttemptStreamV2Proofs, reference, census, sequences)
}

// Only bounded I/O boundaries are replaced, never row validation or hashing.
type attemptStreamV2SpoolObserver struct {
	io.ReadWriteSeeker
	read  func([]byte) (int, error)
	write func([]byte) (int, error)
	seek  func(int64, int) (int64, error)
}

// Preserves the real descriptor file when no explicit fault is selected.
func (self *attemptStreamV2SpoolObserver) Read(data []byte) (int, error) {
	if self.read != nil {
		return self.read(data)
	}
	return self.ReadWriteSeeker.Read(data)
}

// A synchronous hook forces short writes or cancellation without timing.
func (self *attemptStreamV2SpoolObserver) Write(data []byte) (int, error) {
	if self.write != nil {
		return self.write(data)
	}
	return self.ReadWriteSeeker.Write(data)
}

// A synchronous hook observes exact spool ownership and positioning.
func (self *attemptStreamV2SpoolObserver) Seek(offset int64, whence int) (int64, error) {
	if self.seek != nil {
		return self.seek(offset, whence)
	}
	return self.ReadWriteSeeker.Seek(offset, whence)
}

// Empty census is unique and must not create even a scratch descriptor.
func TestAttemptStreamV2WriteEmptyDoesNoStorageIO(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	fixture.options.Spool = &attemptStreamV2SpoolObserver{ReadWriteSeeker: fixture.spool, seek: func(int64, int) (int64, error) {
		t.Fatal("empty writer accessed its spool")
		return 0, errors.New("unreachable")
	}}
	reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, attemptStreamV2WriteRows(), fixture.options)
	if err != nil || reference != (AttemptStreamV2Reference{ManifestHash: zeroAttemptHash()}) || census != (AttemptStreamV2Census{}) || len(fixture.putKinds) != 0 {
		t.Fatalf("empty writer invented storage: %+v %+v %v", reference, census, err)
	}
}

// Explicit allocation limits are checked before asking a source for any row.
func TestAttemptStreamV2WriteRejectsInvalidBoundsBeforeSource(t *testing.T) {
	for _, mutate := range []func(*AttemptStreamV2WriteOptions){
		func(options *AttemptStreamV2WriteOptions) { options.MaxRowBytes = 0 },
		func(options *AttemptStreamV2WriteOptions) { options.Bounds.MaxChunkBytes = ^uint64(0) },
		func(options *AttemptStreamV2WriteOptions) { options.Bounds.MaxPageBytes = 1 },
		func(options *AttemptStreamV2WriteOptions) { options.Bounds.MaxChunks = ^uint64(0) },
		func(options *AttemptStreamV2WriteOptions) { options.Spool = nil },
	} {
		fixture := newAttemptStreamV2WriteFixture(t)
		mutate(&fixture.options)
		source := func(context.Context, func(uint64, []byte) error) error {
			t.Fatal("invalid bounds reached the source")
			return nil
		}
		reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, source, fixture.options)
		if err == nil || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) || len(fixture.putKinds) != 0 {
			t.Fatalf("invalid bounds accepted or published: %v", err)
		}
	}
}

// Invalid row framing and sequence coordinates are refused before publication.
func TestAttemptStreamV2WriteRejectsInvalidRows(t *testing.T) {
	for _, row := range []struct {
		sequence uint64
		data     string
	}{
		{sequence: 1, data: ""}, {sequence: 1, data: "{}"}, {sequence: 1, data: "{}\n{}\n"},
		{sequence: 1, data: "{\n}\n"}, {sequence: 1, data: "invalid\n"},
		{sequence: 1, data: "\"" + strings.Repeat("x", 40) + "\"\n"},
		{sequence: 0, data: "{}\n"}, {sequence: ^uint64(0), data: "{}\n"},
	} {
		fixture := newAttemptStreamV2WriteFixture(t)
		source := func(_ context.Context, visit func(uint64, []byte) error) error {
			return visit(row.sequence, []byte(row.data))
		}
		if reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, source, fixture.options); err == nil || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) || len(fixture.putKinds) != 0 {
			t.Fatalf("invalid row accepted: %d %q error=%v", row.sequence, row.data, err)
		}
	}
}

// Chunk/page boundaries cannot hide duplicates, regressions or record gaps.
func TestAttemptStreamV2WriteRejectsSequenceDiscontinuities(t *testing.T) {
	for _, kind := range []string{AttemptStreamV2Records, AttemptStreamV2Proofs} {
		for _, sequences := range [][]uint64{{1, 2, 3, 3}, {1, 2, 3, 2}, {1, 2, 3, 5}} {
			if kind == AttemptStreamV2Proofs && sequences[3] == 5 {
				continue
			}
			fixture := newAttemptStreamV2WriteFixture(t)
			if reference, census, err := WriteAttemptStreamV2(context.Background(), kind, attemptStreamV2WriteRows(sequences...), fixture.options); err == nil || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) || len(fixture.metaKVs) != 0 {
				t.Fatalf("discontinuous %s stream accepted: %v error=%v", kind, sequences, err)
			}
		}
	}
}

// Every configured dimension has an independent hard refusal boundary.
func TestAttemptStreamV2WriteEnforcesCompleteBounds(t *testing.T) {
	for _, mutate := range []func(*AttemptStreamV2WriteOptions){
		func(options *AttemptStreamV2WriteOptions) { options.Bounds.MaxItems = 2 },
		func(options *AttemptStreamV2WriteOptions) { options.Bounds.MaxDataBytes = 30 },
		func(options *AttemptStreamV2WriteOptions) { options.Bounds.MaxChunks, options.Bounds.MaxPages = 1, 1 },
		func(options *AttemptStreamV2WriteOptions) {
			options.Bounds.MaxPages, options.Bounds.MaxDescriptorsPerPage = 1, 1
		},
	} {
		fixture := newAttemptStreamV2WriteFixture(t)
		mutate(&fixture.options)
		if reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, attemptStreamV2WriteRows(1, 2, 3, 4, 5), fixture.options); err == nil || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) || len(fixture.metaKVs) != 0 {
			t.Fatalf("complete stream bound was ignored: %v", err)
		}
	}
}

// Scratch reuse cannot overwrite an earlier producer or failed evidence.
func TestAttemptStreamV2WritePreservesNonemptySpool(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	original := []byte("preserved failed descriptors")
	if _, err := fixture.spool.Write(original); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, attemptStreamV2WriteRows(1), fixture.options); err == nil || !strings.Contains(err.Error(), "empty") || len(fixture.putKinds) != 0 {
		t.Fatalf("nonempty spool reused: %v", err)
	}
	after, err := os.ReadFile(fixture.spool.Name())
	if err != nil || !bytes.Equal(after, original) {
		t.Fatalf("existing spool changed: %v", err)
	}
}

// A source may fail after several durable chunks; no manifest may escape.
func TestAttemptStreamV2WriteSourceFailureReturnsNoReference(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	failure := errors.New("source interrupted after persisted prefix")
	source := func(ctx context.Context, visit func(uint64, []byte) error) error {
		if err := attemptStreamV2WriteRows(1, 2, 3, 4, 5)(ctx, visit); err != nil {
			return err
		}
		return failure
	}
	reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, source, fixture.options)
	if !errors.Is(err, failure) || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) || len(fixture.dataKVs) == 0 || len(fixture.metaKVs) != 0 {
		t.Fatalf("source failure published a partial census: %v", err)
	}
}

// A faulty source cannot erase refusal by ignoring the visitor's return value.
func TestAttemptStreamV2WriteLatchesIgnoredVisitorFailure(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	source := func(_ context.Context, visit func(uint64, []byte) error) error {
		_ = visit(0, []byte("{}\n"))
		_ = visit(1, []byte("{}\n"))
		return nil
	}
	reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, source, fixture.options)
	if err == nil || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) || len(fixture.putKinds) != 0 {
		t.Fatalf("ignored visitor failure became success: %v", err)
	}
}

// Every publication layer preserves its actual error and returns no acceptance.
func TestAttemptStreamV2WriteSinkFailuresReturnNoReference(t *testing.T) {
	for _, failKind := range []string{"data", AttemptStreamV2PageSchema, AttemptStreamV2Schema} {
		fixture := newAttemptStreamV2WriteFixture(t)
		failure := errors.New("deterministic sink failure")
		if failKind == "data" {
			fixture.options.WriteData = func(context.Context, string, []byte) error { return failure }
		} else {
			write := fixture.options.WriteMetadata
			fixture.options.WriteMetadata = func(ctx context.Context, hash string, data []byte) error {
				if bytes.Contains(data, []byte(`"schema":"`+failKind+`"`)) {
					return failure
				}
				return write(ctx, hash, data)
			}
		}
		reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, attemptStreamV2WriteRows(1, 2, 3), fixture.options)
		if !errors.Is(err, failure) || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) {
			t.Fatalf("%s failure was accepted: %v", failKind, err)
		}
	}
}

// Short successful writes cannot be acknowledged as complete descriptors.
func TestAttemptStreamV2WriteRejectsShortSpoolWrite(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	fixture.options.Spool = &attemptStreamV2SpoolObserver{ReadWriteSeeker: fixture.spool, write: func(data []byte) (int, error) {
		return fixture.spool.Write(data[:len(data)-1])
	}}
	reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, attemptStreamV2WriteRows(1), fixture.options)
	if !errors.Is(err, io.ErrShortWrite) || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) || len(fixture.metaKVs) != 0 {
		t.Fatalf("short spool write became a descriptor: %v", err)
	}
}

// Exact mutation hooks cover byte corruption, reordering, truncation and suffix.
func TestAttemptStreamV2WriteRejectsChangedSpool(t *testing.T) {
	for _, mutation := range []string{"corrupt", "reorder", "truncate", "suffix"} {
		fixture := newAttemptStreamV2WriteFixture(t)
		changed := false
		fixture.options.Spool = &attemptStreamV2SpoolObserver{ReadWriteSeeker: fixture.spool, seek: func(offset int64, whence int) (int64, error) {
			if whence == io.SeekEnd && !changed {
				info, err := fixture.spool.Stat()
				if err != nil {
					return 0, err
				}
				if info.Size() >= 2*attemptStreamV2DescriptorBytes {
					changed = true
					data, err := os.ReadFile(fixture.spool.Name())
					if err != nil {
						return 0, err
					}
					switch mutation {
					case "corrupt":
						data[20] ^= 1
						_, err = fixture.spool.WriteAt(data, 0)
					case "reorder":
						first := bytes.Clone(data[:attemptStreamV2DescriptorBytes])
						copy(data[:attemptStreamV2DescriptorBytes], data[attemptStreamV2DescriptorBytes:2*attemptStreamV2DescriptorBytes])
						copy(data[attemptStreamV2DescriptorBytes:], first)
						_, err = fixture.spool.WriteAt(data, 0)
					case "truncate":
						err = fixture.spool.Truncate(info.Size() - 1)
					case "suffix":
						_, err = fixture.spool.WriteAt([]byte{0}, info.Size())
					}
					if err != nil {
						return 0, err
					}
				}
			}
			return fixture.spool.Seek(offset, whence)
		}}
		reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, attemptStreamV2WriteRows(1, 2, 3, 4, 5), fixture.options)
		if !changed || err == nil || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) {
			t.Fatalf("%s spool mutation accepted: changed=%t error=%v", mutation, changed, err)
		}
		for _, kind := range fixture.putKinds {
			if kind == AttemptStreamV2Schema {
				t.Fatalf("%s mutation published a manifest", mutation)
			}
		}
	}
}

// A zero-progress reader is a deterministic error, never a busy loop.
func TestAttemptStreamV2WriteRejectsSpoolNoProgress(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	reads := 0
	fixture.options.Spool = &attemptStreamV2SpoolObserver{ReadWriteSeeker: fixture.spool, read: func([]byte) (int, error) {
		reads++
		if reads > 1 {
			return 0, errors.New("reader was polled after no progress")
		}
		return 0, nil
	}}
	reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, attemptStreamV2WriteRows(1), fixture.options)
	if !errors.Is(err, io.ErrNoProgress) || reads != 1 || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) {
		t.Fatalf("zero-progress read was retried: %d %v", reads, err)
	}
}

// Cancellation is forced at each I/O callback, including after the final write.
func TestAttemptStreamV2WriteCancellationAtEveryStorageBoundary(t *testing.T) {
	for _, boundary := range []string{"seek", "write", "read", "data", "page", "manifest"} {
		fixture := newAttemptStreamV2WriteFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		canceled := false
		stop := func() { canceled = true; cancel() }
		observer := &attemptStreamV2SpoolObserver{ReadWriteSeeker: fixture.spool}
		observer.seek = func(offset int64, whence int) (int64, error) {
			if canceled {
				t.Fatalf("%s cancellation allowed another seek", boundary)
			}
			position, err := fixture.spool.Seek(offset, whence)
			if boundary == "seek" {
				stop()
			}
			return position, err
		}
		observer.write = func(data []byte) (int, error) {
			if canceled {
				t.Fatalf("%s cancellation allowed another write", boundary)
			}
			n, err := fixture.spool.Write(data)
			if boundary == "write" {
				stop()
			}
			return n, err
		}
		observer.read = func(data []byte) (int, error) {
			if canceled {
				t.Fatalf("%s cancellation allowed another read", boundary)
			}
			n, err := fixture.spool.Read(data)
			if boundary == "read" {
				stop()
			}
			return n, err
		}
		fixture.options.Spool = observer
		dataWrite, metadataWrite := fixture.options.WriteData, fixture.options.WriteMetadata
		fixture.options.WriteData = func(ctx context.Context, hash string, data []byte) error {
			if canceled {
				t.Fatal("cancellation allowed a data write")
			}
			err := dataWrite(ctx, hash, data)
			if boundary == "data" {
				stop()
			}
			return err
		}
		fixture.options.WriteMetadata = func(ctx context.Context, hash string, data []byte) error {
			if canceled {
				t.Fatal("cancellation allowed a metadata write")
			}
			err := metadataWrite(ctx, hash, data)
			if boundary == "page" && bytes.Contains(data, []byte(AttemptStreamV2PageSchema)) || boundary == "manifest" && bytes.Contains(data, []byte(`"schema":"`+AttemptStreamV2Schema+`"`)) {
				stop()
			}
			return err
		}
		reference, census, err := WriteAttemptStreamV2(ctx, AttemptStreamV2Records, attemptStreamV2WriteRows(1, 2, 3), fixture.options)
		if !canceled || !errors.Is(err, context.Canceled) || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) {
			t.Fatalf("%s cancellation accepted: %+v %+v %v", boundary, reference, census, err)
		}
	}
}

// Borrowed row reuse and retained sink ownership cannot corrupt prior chunks.
func TestAttemptStreamV2WriteOwnsRowsAndTransfersChunkBytes(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	var sourceRows [][]byte
	source := func(_ context.Context, visit func(uint64, []byte) error) error {
		for sequence := uint64(1); sequence <= 7; sequence++ {
			row := []byte(fmt.Sprintf("{\"sequence\":%d}\n", sequence))
			if err := visit(sequence, row); err != nil {
				return err
			}
			sourceRows = append(sourceRows, row)
			for index := range row {
				row[index] = 'x'
			}
		}
		return nil
	}
	reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, source, fixture.options)
	if err != nil || len(sourceRows) != 7 {
		t.Fatalf("row ownership failed: %v", err)
	}
	fixture.verify(t, AttemptStreamV2Records, reference, census, []uint64{1, 2, 3, 4, 5, 6, 7})
}

// Partially filled reads are supported without assuming one syscall per row.
func TestAttemptStreamV2WriteReadsSplitSpoolDescriptors(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	fixture.options.Spool = &attemptStreamV2SpoolObserver{ReadWriteSeeker: fixture.spool, read: func(data []byte) (int, error) {
		return fixture.spool.Read(data[:min(len(data), 7)])
	}}
	reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, attemptStreamV2WriteRows(1, 2, 3), fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	fixture.verify(t, AttemptStreamV2Records, reference, census, []uint64{1, 2, 3})
}

// Conservative serialized widths limit each page even when the configured
// descriptor count alone would allow a page larger than its byte budget.
func TestAttemptStreamV2WritePageCapacityRespectsByteBudget(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	fixture.options.Bounds.MaxDescriptorsPerPage = 100
	fixture.options.Bounds.MaxPageBytes = 512
	sequences := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9}
	reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, attemptStreamV2WriteRows(sequences...), fixture.options)
	if err != nil || census.PageCount != 5 || census.ChunkCount != 5 {
		t.Fatalf("byte-bounded descriptor pages: %+v %v", census, err)
	}
	fixture.verify(t, AttemptStreamV2Records, reference, census, sequences)
}

// A pre-canceled context never invokes the source or scratch owner.
func TestAttemptStreamV2WritePreCanceledDoesNoWork(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := func(context.Context, func(uint64, []byte) error) error {
		t.Fatal("canceled writer invoked its source")
		return nil
	}
	reference, census, err := WriteAttemptStreamV2(ctx, AttemptStreamV2Records, source, fixture.options)
	if !errors.Is(err, context.Canceled) || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) || len(fixture.putKinds) != 0 {
		t.Fatalf("pre-canceled writer accepted work: %v", err)
	}
}

// Cancellation delivered by the source prevents flushing its buffered prefix.
func TestAttemptStreamV2WriteSourceCancellationPreventsFlush(t *testing.T) {
	fixture := newAttemptStreamV2WriteFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := func(ctx context.Context, visit func(uint64, []byte) error) error {
		if err := attemptStreamV2WriteRows(1)(ctx, visit); err != nil {
			return err
		}
		cancel()
		return nil
	}
	reference, census, err := WriteAttemptStreamV2(ctx, AttemptStreamV2Records, source, fixture.options)
	if !errors.Is(err, context.Canceled) || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) || len(fixture.putKinds) != 0 {
		t.Fatalf("source cancellation flushed its pending chunk: %v", err)
	}
}

// A falsely acknowledged position cannot authorize a write at another offset.
func TestAttemptStreamV2WriteRejectsInvalidSpoolOffsets(t *testing.T) {
	for _, failWhence := range []int{io.SeekStart, io.SeekEnd} {
		fixture := newAttemptStreamV2WriteFixture(t)
		fixture.options.Spool = &attemptStreamV2SpoolObserver{ReadWriteSeeker: fixture.spool, seek: func(offset int64, whence int) (int64, error) {
			if whence == failWhence {
				if whence == io.SeekEnd {
					return -1, nil
				}
				return offset + 1, nil
			}
			return fixture.spool.Seek(offset, whence)
		}}
		reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, attemptStreamV2WriteRows(1), fixture.options)
		if err == nil || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) || len(fixture.putKinds) != 0 {
			t.Fatalf("invalid seek offset was accepted: whence=%d error=%v", failWhence, err)
		}
	}
}

// Reader errors accompanying valid bytes are not swallowed, except EOF at the
// exact final descriptor byte, which is a legal io.Reader completion boundary.
func TestAttemptStreamV2WriteSpoolReadErrorIsNotSwallowed(t *testing.T) {
	for _, full := range []bool{false, true} {
		fixture := newAttemptStreamV2WriteFixture(t)
		failure := errors.New("descriptor read failed with bytes")
		fixture.options.Spool = &attemptStreamV2SpoolObserver{ReadWriteSeeker: fixture.spool, read: func(data []byte) (int, error) {
			if !full {
				data = data[:1]
			}
			n, err := fixture.spool.Read(data)
			return n, errors.Join(err, failure)
		}}
		reference, census, err := WriteAttemptStreamV2(context.Background(), AttemptStreamV2Records, attemptStreamV2WriteRows(1), fixture.options)
		if !errors.Is(err, failure) || reference != (AttemptStreamV2Reference{}) || census != (AttemptStreamV2Census{}) || len(fixture.metaKVs) != 0 {
			t.Fatalf("read failure with full=%t became success: %v", full, err)
		}
	}
}
