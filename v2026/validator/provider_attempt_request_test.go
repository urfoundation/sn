//go:build linux || darwin

// Actual signed M8 trails and protected files expose the pre-send boundary.
// Fault hooks force interrupted publication without sleeps or scheduler races.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// A fixture transport observes the real original owner before delegating to
// the existing signed Server simulator; it does not replace wire admission.
type providerRequestTestTransport struct {
	base   TrailTransport
	before func(context.Context, connect.Id, []byte) error
}

func (self providerRequestTestTransport) PostVerify(ctx context.Context, hop connect.Id, raw []byte) ([]byte, error) {
	if self.before != nil {
		if err := self.before(ctx, hop, raw); err != nil {
			return nil, err
		}
	}
	return self.base.PostVerify(ctx, hop, raw)
}

// Explicit offline staging uses the same pure physical checkpoint generator
// as the preparation adapter; runtime Open never performs this operation.
func prepareProviderRequestTestOwner(t *testing.T, directory string, expected ProviderAttemptRequestPreparation) {
	t.Helper()
	root, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := os.OpenFile(filepath.Join(directory, ProviderAttemptRequestJournalName), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rootInfo, err := root.Stat()
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := FreshProviderAttemptRequestCheckpoint(expected, rootInfo, fileInfo)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := writeProviderAttemptRequestAttribute(root, raw, true); err != nil {
		t.Fatal(err)
	}
	if err := root.Sync(); err != nil {
		t.Fatal(err)
	}
}

// Every test gets its own original key, actual ledger, protected directory and
// independently retained preparation. No runtime birth is inferred from zero.
func providerRequestTestOwner(t *testing.T, limit uint64) (*TrailEngine, *ProviderAttemptRequestJournal, ProviderAttemptRequestPreparation, string) {
	t.Helper()
	server, key, clientId := newMockVerifyServer(t, 12)
	engine, stats, _ := newTestEngine(t, server, key, clientId, 8, nil)
	engine.cfg.StepTimeout = time.Minute
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, directory, &generation)
	t.Cleanup(func() {
		if err := ledger.Close(); err != nil {
			t.Error(err)
		}
	})
	expected := ProviderAttemptRequestPreparation{Identity: ProviderAttemptRequestIdentity{Ledger: ledger.identity, Coordinator: "0x1111111111111111111111111111111111111111", ClientId: clientId, PolicyHash: [32]byte{9}}, Limits: ProviderAttemptRequestLimits{MaxRecords: limit, MaxRecordBytes: 8192, MaxJournalBytes: 1024 * 1024}, Birth: attemptLedgerTestBoundary()}
	prepareProviderRequestTestOwner(t, directory, expected)
	journal, err := OpenProviderAttemptRequestJournal(context.Background(), directory, expected, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	engine.cfg.RequestJournal = journal
	return engine, journal, expected, directory
}

// Observe a complete signed prefix before every actual send, including the
// first SEED that the old returned-assignment ledger could not reconstruct.
func TestProviderRequestActualTrailPersistsBeforeEverySend(t *testing.T) {
	engine, journal, _, _ := providerRequestTestOwner(t, 32)
	sends := uint64(0)
	engine.transport = providerRequestTestTransport{base: engine.transport, before: func(ctx context.Context, _ connect.Id, raw []byte) error {
		sends++
		head, _, err := journal.Head(ctx)
		if err != nil {
			return err
		}
		if head.Sequence != sends {
			t.Fatalf("transport preceded original request publication: head=%d send=%d", head.Sequence, sends)
		}
		var last ProviderAttemptRequestRecord
		if err := journal.Walk(ctx, func(record ProviderAttemptRequestRecord) error { last = record; return nil }); err != nil {
			return err
		}
		if !bytes.Equal(last.Body, raw) {
			t.Fatal("transport body differs from durable original")
		}
		return nil
	}}
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sends != 8 {
		t.Fatalf("actual complete M8 request census=%d", sends)
	}
}

// A failed first transport attempt retries the identical preexisting record;
// a second send cannot create a second owned request or a new signature.
func TestProviderRequestSeedRetryRetainsOneOriginal(t *testing.T) {
	engine, journal, _, _ := providerRequestTestOwner(t, 32)
	calls := 0
	var original []byte
	engine.transport = providerRequestTestTransport{base: engine.transport, before: func(ctx context.Context, _ connect.Id, raw []byte) error {
		calls++
		if calls <= 2 {
			head, _, err := journal.Head(ctx)
			if err != nil {
				return err
			}
			if head.Sequence != 1 {
				t.Fatal("retry counted another original request")
			}
		}
		if calls == 1 {
			original = bytes.Clone(raw)
			return errors.New("synthetic lost request transport reply")
		}
		if calls == 2 && !bytes.Equal(original, raw) {
			t.Fatal("retry changed original signed bytes")
		}
		return nil
	}}
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	head, _, err := journal.Head(context.Background())
	if err != nil || head.Sequence != 8 || calls != 9 {
		t.Fatalf("retry census head=%+v calls=%d err=%v", head, calls, err)
	}
}

// Cancellation after file fsync leaves the original pending request for exact
// restart recovery, while no transport has yet observed it.
func TestProviderRequestCanceledPublicationRecoversOriginalWithoutSend(t *testing.T) {
	engine, journal, expected, directory := providerRequestTestOwner(t, 32)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	journal.step = func(stage string) error {
		if stage == "after-request-file-sync" {
			cancel()
			return ctx.Err()
		}
		return nil
	}
	calls := 0
	engine.transport = providerRequestTestTransport{base: engine.transport, before: func(context.Context, connect.Id, []byte) error { calls++; return nil }}
	if _, err := engine.RunTrail(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("publication cancellation lost: %v", err)
	}
	if calls != 0 {
		t.Fatal("canceled unpublished request reached transport")
	}
	pending, err := os.ReadFile(filepath.Join(directory, ProviderAttemptRequestPendingName))
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenProviderAttemptRequestJournal(context.Background(), directory, expected, engine.vsk)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	head, _, err := reopened.Head(context.Background())
	if err != nil || head.Sequence != 1 {
		t.Fatalf("pending original was not recovered exactly: %+v %v", head, err)
	}
	var record ProviderAttemptRequestRecord
	if err := reopened.Walk(context.Background(), func(value ProviderAttemptRequestRecord) error { record = value; return nil }); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(record)
	if err != nil || !bytes.Equal(raw, pending) {
		t.Fatalf("restart regenerated original request: %v", err)
	}
}

// Total loss is not an empty owner: even an otherwise healthy legacy ledger
// cannot grant replacement sequence zero or reset a pending pre-SEED request.
func TestProviderRequestAbsentAnchorNeverCreatesBirth(t *testing.T) {
	engine, journal, expected, directory := providerRequestTestOwner(t, 32)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unix.Removexattr(directory, ProviderAttemptRequestAttribute); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, ProviderAttemptRequestJournalName)); err != nil {
		t.Fatal(err)
	}
	owner, err := OpenProviderAttemptRequestJournal(context.Background(), directory, expected, engine.vsk)
	if owner != nil || !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatalf("missing original custody created a birth: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(directory, ProviderAttemptRequestJournalName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime created a replacement journal: %v", err)
	}
}

// A missing committed file remains physical loss, even with an empty head.
func TestProviderRequestMissingPreparedFileIsNotRecreated(t *testing.T) {
	engine, journal, expected, directory := providerRequestTestOwner(t, 32)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, ProviderAttemptRequestJournalName)); err != nil {
		t.Fatal(err)
	}
	owner, err := OpenProviderAttemptRequestJournal(context.Background(), directory, expected, engine.vsk)
	if owner != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lost file was recreated: %v", err)
	}
}

// Capacity is a source-local held result; it cannot change the first original
// or convert the still-unobserved request census to an authenticated zero.
func TestProviderRequestCapacityPreservesOriginalPrefix(t *testing.T) {
	engine, journal, _, _ := providerRequestTestOwner(t, 1)
	calls := 0
	engine.transport = providerRequestTestTransport{base: engine.transport, before: func(context.Context, connect.Id, []byte) error { calls++; return nil }}
	if _, err := engine.RunTrail(context.Background()); !errors.Is(err, protocol.ErrProviderAttemptsCapacity) {
		t.Fatalf("request capacity did not remain typed: %v", err)
	}
	head, _, err := journal.Head(context.Background())
	if err != nil || head.Sequence != 1 || calls != 1 {
		t.Fatalf("capacity altered original prefix: %+v %d %v", head, calls, err)
	}
}

// An authentic owner signature cannot admit a request from before the pinned
// birth or from another hash at that exact birth height.
func TestProviderRequestOriginalBirthRejectsEarlierAndForeignFork(t *testing.T) {
	engine, journal, expected, _ := providerRequestTestOwner(t, 32)
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	var first ProviderAttemptRequestRecord
	if err := journal.Walk(context.Background(), func(record ProviderAttemptRequestRecord) error {
		if record.Sequence == 1 {
			first = record
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, boundary := range []AttemptBoundary{{SettlementEpoch: expected.Birth.SettlementEpoch, EVMBlock: expected.Birth.EVMBlock - 1, EVMBlockHash: expected.Birth.EVMBlockHash}, {SettlementEpoch: expected.Birth.SettlementEpoch, EVMBlock: expected.Birth.EVMBlock, EVMBlockHash: attemptHex32([32]byte{77})}} {
		changed := first
		changed.Boundary = boundary
		unsigned, err := changed.signingBytes()
		if err != nil {
			t.Fatal(err)
		}
		changed.Signature = ed25519.Sign(engine.vsk, unsigned)
		raw, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := changed.Hash()
		if err != nil {
			t.Fatal(err)
		}
		checkpoint := ProviderAttemptRequestCheckpoint{Schema: ProviderAttemptRequestCheckpointSchema, Identity: expected.Identity, Limits: expected.Limits, Birth: expected.Birth, Committed: ProviderAttemptRequestHead{Sequence: 1, Hash: hash, Bytes: uint64(len(raw)) + 1, LastBoundary: boundary}}
		if _, err := VerifyProviderAttemptRequestPrefix(context.Background(), checkpoint, expected, bytes.NewReader(append(raw, '\n')), nil); err == nil {
			t.Fatal("original birth or canonical fork was replaced")
		}
	}
}

// The pure verifier proves every committed byte and permits only the exact
// partial next suffix. Restore cannot discard pending original knowledge.
func TestProviderRequestPureRestoreRetainsInterruptedOriginal(t *testing.T) {
	engine, journal, expected, directory := providerRequestTestOwner(t, 32)
	fault := errors.New("synthetic retained crash boundary")
	journal.step = func(string) error { return fault }
	if _, err := engine.RunTrail(context.Background()); !errors.Is(err, fault) {
		t.Fatalf("original fault was lost: %v", err)
	}
	checkpoint := *journal.checkpoint
	pending, err := os.ReadFile(filepath.Join(directory, ProviderAttemptRequestPendingName))
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []int{0, 7, len(pending) + 1} {
		line := append(bytes.Clone(pending), '\n')
		verified, err := VerifyProviderAttemptRequestPrefix(context.Background(), checkpoint, expected, bytes.NewReader(line[:suffix]), pending)
		if err != nil || verified.PendingRecord() == nil {
			t.Fatalf("exact interrupted original refused: %v", err)
		}
		if verified.Checkpoint().Birth != expected.Birth || verified.Checkpoint().Limits != expected.Limits {
			t.Fatal("restore changed original birth or capacity")
		}
	}
	if _, err := VerifyProviderAttemptRequestPrefix(context.Background(), checkpoint, expected, bytes.NewReader([]byte("foreign")), pending); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatalf("foreign pending suffix admitted: %v", err)
	}
	if _, err := VerifyProviderAttemptRequestPrefix(context.Background(), checkpoint, expected, bytes.NewReader(nil), nil); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatalf("missing pending request became empty: %v", err)
	}
}

// Rebound inode publication changes no original logical field and cannot be
// manufactured from a cached counter without a full prefix verification.
func TestProviderRequestPureRestoreRebindsOnlyPhysicalIdentity(t *testing.T) {
	engine, journal, expected, directory := providerRequestTestOwner(t, 32)
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, ProviderAttemptRequestJournalName))
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyProviderAttemptRequestPrefix(context.Background(), *journal.checkpoint, expected, bytes.NewReader(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Chmod(target, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(target, ProviderAttemptRequestJournalName)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	directoryInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	attribute, err := verified.ReboundCheckpoint(directoryInfo, fileInfo)
	if err != nil {
		t.Fatal(err)
	}
	var rebound ProviderAttemptRequestCheckpoint
	if err := json.Unmarshal(attribute, &rebound); err != nil {
		t.Fatal(err)
	}
	before := verified.Checkpoint()
	before.DirectoryInode, before.FileInode = rebound.DirectoryInode, rebound.FileInode
	want, err := json.Marshal(before)
	if err != nil || !bytes.Equal(want, attribute) {
		t.Fatalf("restore changed logical request custody: %v", err)
	}
}

// A returned original must remain tied to its complete wire body and owner;
// changing a client, message byte or hop cannot reuse the first signature.
func TestProviderRequestForeignWireCannotReuseOriginalSignature(t *testing.T) {
	engine, journal, expected, _ := providerRequestTestOwner(t, 32)
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	var first ProviderAttemptRequestRecord
	if err := journal.Walk(context.Background(), func(record ProviderAttemptRequestRecord) error {
		if record.Sequence == 1 {
			first = record
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ProviderAttemptRequestRecord){func(r *ProviderAttemptRequestRecord) { r.Identity.ClientId = connect.NewId() }, func(r *ProviderAttemptRequestRecord) {
		r.Message = bytes.Clone(r.Message)
		r.Message[len(r.Message)-1] ^= 1
	}, func(r *ProviderAttemptRequestRecord) { r.Hop = connect.NewId() }} {
		changed := first
		change(&changed)
		if err := VerifyProviderAttemptRequest(context.Background(), changed, expected.Identity, ProviderAttemptRequestHead{}, expected.Limits); err == nil {
			t.Fatal("foreign request reused original signature")
		}
	}
}

// A lost committed inode and a partial staging name cannot be silently reset
// or promoted. They remain owned recovery work before another request starts.
func TestProviderRequestIncompleteStagingRemainsUnknown(t *testing.T) {
	engine, journal, expected, directory := providerRequestTestOwner(t, 32)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, ProviderAttemptRequestPendingName+".tmp"), []byte("{\"schema\":"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner, err := OpenProviderAttemptRequestJournal(context.Background(), directory, expected, engine.vsk)
	if owner != nil || !errors.Is(err, protocol.ErrProviderAttemptsUnavailable) {
		t.Fatalf("partial original staging was hidden: %v", err)
	}
}

// Live custody detects a changed original before transport; cold admission
// also authenticates the complete bytes instead of trusting the old counters.
func TestProviderRequestChangedOriginalRefusesLiveAndReopen(t *testing.T) {
	engine, journal, expected, directory := providerRequestTestOwner(t, 32)
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, ProviderAttemptRequestJournalName)
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("!"), 0); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := journal.Head(context.Background()); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatalf("changed original kept live custody: %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if owner, err := OpenProviderAttemptRequestJournal(context.Background(), directory, expected, engine.vsk); err == nil || owner != nil {
		t.Fatal("changed original reopened from retained counters")
	}
}
