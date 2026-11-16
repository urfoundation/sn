//go:build linux || darwin

package validator

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"path/filepath"
	"testing"
)

// A later cut's stream starts with the earlier cut's rows, so its chunks repeat.
// The complete stream still reads back, and only its new tail was written.
func TestReleaseAttemptUploadMemoWritesOnlyANewTail(t *testing.T) {
	t.Parallel()
	memo := newReleaseAttemptUploadMemo()
	stored := map[string][]byte{}
	cut := func(sequences ...uint64) int {
		t.Helper()
		fixture := newAttemptStreamV2WriteFixture(t)
		for hash, raw := range stored {
			fixture.dataKVs[hash] = raw
		}
		writes := 0
		write := fixture.options.WriteData
		fixture.options.WriteData = memo.writer(AttemptStreamV2Records, func(ctx context.Context, hash string, raw []byte) error {
			writes++
			return write(ctx, hash, raw)
		})
		reference, census, err := WriteAttemptStreamV2(t.Context(), AttemptStreamV2Records, attemptStreamV2WriteRows(sequences...), fixture.options)
		if err != nil {
			t.Fatal(err)
		}
		fixture.verify(t, AttemptStreamV2Records, reference, census, sequences)
		for hash, raw := range fixture.dataKVs {
			stored[hash] = raw
		}
		return writes
	}
	first := cut(1, 2, 3, 4, 5, 6)
	if first < 3 {
		t.Fatalf("the fixture needs several chunks: %d writes", first)
	}
	if again := cut(1, 2, 3, 4, 5, 6); again != 0 {
		t.Fatalf("the same stream wrote %d acknowledged chunks again", again)
	}
	if grown := cut(1, 2, 3, 4, 5, 6, 7); grown == 0 || grown >= first {
		t.Fatalf("a stream with one more row wrote %d chunks of %d", grown, first)
	}
}

// Only success is remembered: a refused write is tried again by the next seal,
// and nothing is ever written under another kind's memory.
func TestReleaseAttemptUploadMemoRemembersOnlyAcknowledgements(t *testing.T) {
	t.Parallel()
	memo := newReleaseAttemptUploadMemo()
	refusal := errors.New("attempt upload response status is 403")
	writes, fail := 0, true
	write := memo.writer(AttemptStreamV2Records, func(context.Context, string, []byte) error {
		writes++
		if fail {
			return refusal
		}
		return nil
	})
	if err := write(t.Context(), "hash", []byte("raw")); !errors.Is(err, refusal) {
		t.Fatalf("the refusal vanished: %v", err)
	}
	fail = false
	for range 3 {
		if err := write(t.Context(), "hash", []byte("raw")); err != nil {
			t.Fatal(err)
		}
	}
	if writes != 2 {
		t.Fatalf("%d writes, want the refused one and one acknowledged", writes)
	}
	other := 0
	proofs := memo.writer(AttemptStreamV2Proofs, func(context.Context, string, []byte) error {
		other++
		return nil
	})
	if err := proofs(t.Context(), "hash", []byte("raw")); err != nil || other != 1 {
		t.Fatalf("another kind's object was treated as acknowledged: %d writes: %v", other, err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := write(canceled, "hash", []byte("raw")); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled write reported success: %v", err)
	}
	memo.forget()
	if err := write(t.Context(), "hash", []byte("raw")); err != nil || writes != 3 {
		t.Fatalf("a forgotten object was not written again: %d writes: %v", writes, err)
	}
	var none *releaseAttemptUploadMemo
	direct := func(context.Context, string, []byte) error { return refusal }
	if err := none.writer(AttemptStreamV2Records, direct)(t.Context(), "hash", nil); !errors.Is(err, refusal) {
		t.Fatalf("no memo must mean the plain writer: %v", err)
	}
	for index := range releaseAttemptUploadMemoObjects + 2 {
		memo.remember("bound", string(rune(index)))
	}
	if len(memo.acknowledged) > releaseAttemptUploadMemoObjects {
		t.Fatalf("the memo outgrew its bound: %d", len(memo.acknowledged))
	}
}

// The whole seal, twice: the second cut of the same prefix writes nothing and
// still replays every object from the origin before it signs. An object the
// origin lost fails that seal and forgets the memory, so the next seal repairs it.
func TestReleaseAttemptUploadMemoSealStillReadsBackEverything(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 0)
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	memo := newReleaseAttemptUploadMemo()
	var missing string
	open := options.OpenData
	seal := func(ctx context.Context, attempt string) (*AttemptCutV2, error) {
		owned := options
		owned.ScratchDirectory = filepath.Join(t.TempDir(), attempt)
		owned.WriteRecords = memo.writer(AttemptStreamV2Records, options.WriteRecords)
		owned.WriteProofs = memo.writer(AttemptStreamV2Proofs, options.WriteProofs)
		owned.WriteMetadata = memo.writer("metadata", options.WriteMetadata)
		owned.ReadMetadata = memo.metadataReader(options.ReadMetadata)
		owned.OpenData = memo.dataOpener(func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			if hash == missing {
				return nil, &attemptStreamHttpStatusError{status: http.StatusNotFound}
			}
			return open(ctx, kind, hash, size)
		})
		cut, _, err := SealAttemptCutV2(ctx, fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, owned)
		return cut, err
	}
	first, err := seal(t.Context(), "first")
	if err != nil {
		t.Fatal(err)
	}
	writes, reads := objects.writes, objects.reads
	if writes == 0 || reads == 0 {
		t.Fatalf("the first seal wrote %d and read %d", writes, reads)
	}
	second, err := seal(t.Context(), "second")
	if err != nil {
		t.Fatal(err)
	}
	if objects.writes != writes {
		t.Fatalf("the second seal wrote %d acknowledged objects again", objects.writes-writes)
	}
	if objects.reads <= reads {
		t.Fatal("the second seal skipped its read-back, which is what proves the objects are there")
	}
	if second.Root != first.Root || second.Records.ManifestHash != first.Records.ManifestHash || string(second.Signature) != string(first.Signature) {
		t.Fatal("skipping acknowledged writes changed the signed cut")
	}
	for hash := range objects.dataKVs[AttemptStreamV2Records] {
		missing = hash
		break
	}
	gone := &attemptStreamHttpStatusError{}
	if _, err := seal(t.Context(), "lost"); !errors.As(err, &gone) || gone.status != http.StatusNotFound {
		t.Fatalf("a lost object did not fail the seal: %v", err)
	}
	if len(memo.acknowledged) != 0 {
		t.Fatalf("a lost object left %d objects remembered", len(memo.acknowledged))
	}
	missing = ""
	writes = objects.writes
	if _, err := seal(t.Context(), "repaired"); err != nil {
		t.Fatal(err)
	}
	if objects.writes == writes {
		t.Fatal("the seal after a lost object did not write again")
	}
}

// The defect itself: without the memory, each cut writes every object of the
// epoch again, so a seal's chance of meeting one refusing instance grows with it.
func TestReleaseAttemptSealWithoutTheMemoWritesEverythingAgain(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 0)
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	var writes []int
	for _, attempt := range []string{"first", "second"} {
		owned := options
		owned.ScratchDirectory = filepath.Join(t.TempDir(), attempt)
		if _, _, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, owned); err != nil {
			t.Fatal(err)
		}
		writes = append(writes, objects.writes)
	}
	if writes[0] == 0 || writes[1] != 2*writes[0] {
		t.Fatalf("plain seals should each write everything: %v", writes)
	}
}

// Both cut kinds reach the origin through the one seal owner, which holds the
// runtime's memory of what each operator's origin acknowledged.
func TestReleaseRuntimeSealOwnsTheUploadMemo(t *testing.T) {
	t.Parallel()
	file, err := parser.ParseFile(token.NewFileSet(), "release_runtime_v2.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		if function, ok := node.(*ast.FuncDecl); ok && function.Name.Name == "seal" {
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok {
					if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
						found[selector.Sel.Name] = true
					}
				}
				return true
			})
		}
		return true
	})
	for _, name := range []string{"uploadMemo", "writer", "metadataReader", "dataOpener"} {
		if !found[name] {
			t.Fatalf("the runtime's seal does not call %s", name)
		}
	}
}
