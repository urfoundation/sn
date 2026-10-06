//go:build linux

// Offline preparation uses public identity and exact reviewed bytes. It never
// opens a signing owner, mutates a retained ledger, or authorizes its restart.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"golang.org/x/sys/unix"
)

const AttemptLedgerPreparationKind = "validator-attempt-ledger"
const attemptLedgerPreparationSchema = "urnetwork-attempt-ledger-preparation-census-v1"

// Presence is explicit. An absent witness never authorizes reimport or removal
// of a legacy member which is actually present in the retained namespace.
type AttemptLedgerPreparationLegacy struct {
	Bytes  uint64 `json:"bytes"`
	Sha256 string `json:"sha256"`
}

// The independently reviewed head is exact, not merely a minimum prefix. No
// private key is accepted by any offline preparation entry point.
type AttemptLedgerPreparationScope struct {
	Identity     AttemptLedgerIdentity                 `json:"identity"`
	Coordinator  string                                `json:"coordinator"`
	Limits       AttemptLedgerDiskLimits               `json:"limits"`
	ExpectedHead AttemptLedgerHead                     `json:"expected_head"`
	Legacy       *AttemptLedgerPreparationLegacy       `json:"legacy"`
	Requests     *AttemptLedgerRequestPreparationScope `json:"requests,omitempty"`
}

// Portable bytes and modes are separate from the target's eventual inodes.
// The common plan-owned publisher admits only these exact bounded members.
type AttemptLedgerPreparationFile struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Mode   uint32 `json:"mode"`
	Bytes  uint64 `json:"bytes"`
	Sha256 string `json:"sha256,omitempty"`
}

// A read-only public ledger census binds its exact portable members and head.
type AttemptLedgerPreparationCensus struct {
	Schema            string                         `json:"schema"`
	Kind              string                         `json:"kind"`
	ScopeSha256       string                         `json:"scope_sha256"`
	Head              AttemptLedgerHead              `json:"head"`
	Files             []AttemptLedgerPreparationFile `json:"files"`
	RestartAuthorized bool                           `json:"restart_authorized"`
}

// Bounds cover filesystem work and the signed-prefix verifier independently.
func (self AttemptLedgerPreparationScope) validate() (ed25519.PublicKey, error) {
	if err := validateAttemptLedgerIdentity(self.Identity, nil); err != nil {
		return nil, err
	}
	if self.Requests != nil {
		if err := self.Requests.validate(self.Identity, self.Coordinator); err != nil {
			return nil, err
		}
		if err := ValidateProviderAttemptRequestRootCapacity(self.Limits, self.Requests.Preparation.Limits); err != nil {
			return nil, err
		}
	}
	public, err := canonicalAttemptHex32("preparation validator public key", self.Identity.ValidatorVPK, false)
	if err != nil {
		return nil, err
	}
	address, err := hex.DecodeString(strings.TrimPrefix(self.Coordinator, "0x"))
	if err != nil || len(address) != 20 || self.Coordinator != "0x"+hex.EncodeToString(address) || bytes.Equal(address, make([]byte, 20)) {
		return nil, errors.New("preparation coordinator is not canonical")
	}
	limits := self.Limits
	if limits.MaxRecordBytes == 0 || limits.MaxRecordBytes > 16*1024*1024 || limits.MaxRecordCount == 0 || limits.MaxRecordCount > 1000000000 ||
		limits.MaxTrailCount == 0 || limits.MaxTrailCount > limits.MaxRecordCount || limits.MaxRawRecordBytes < limits.MaxRecordBytes ||
		limits.MaxStorageBytes <= attemptStoreMetadataReserve || limits.MaxStorageBytes > 1024*1024*1024*1024 || limits.MaxStorageFiles < 8 || limits.MaxStorageFiles > 10000 ||
		limits.MaxLegacyBytes == 0 || limits.MaxLegacyBytes > 1024*1024*1024*1024 || limits.MaxProofBytes == 0 || uint64(len(self.Identity.DeploymentID)) > limits.MaxRecordBytes {
		return nil, errors.New("preparation ledger limits are absent or exceed the finite offline profile")
	}
	if _, err := canonicalAttemptHex32("preparation ledger head", self.ExpectedHead.Root, true); err != nil ||
		self.ExpectedHead.LastSequence > limits.MaxRecordCount || self.ExpectedHead.RecordBytes > limits.MaxRawRecordBytes || self.ExpectedHead.TrailCount > limits.MaxTrailCount ||
		self.ExpectedHead.LastSequence == 0 && self.ExpectedHead != (AttemptLedgerHead{Root: zeroAttemptHash()}) {
		return nil, errors.Join(errors.New("preparation head is invalid or unbounded"), err)
	}
	if self.Legacy != nil {
		if self.Legacy.Bytes > limits.MaxLegacyBytes || !strings.HasPrefix(self.Legacy.Sha256, "sha256:") || len(self.Legacy.Sha256) != 71 {
			return nil, errors.New("preparation legacy witness is absent or unbounded")
		}
		if _, err := hex.DecodeString(self.Legacy.Sha256[7:]); err != nil || strings.ToLower(self.Legacy.Sha256) != self.Legacy.Sha256 {
			return nil, errors.New("preparation legacy witness is not canonical")
		}
	}
	return append(ed25519.PublicKey(nil), public[:]...), nil
}

// Borrows the caller's exclusively retained directory. Every backend method
// is read-only; even LOCK and CURRENT must already exist. The caller retains
// the external former-writer fence and physical mount/path admission.
func InspectAttemptLedgerPreparation(ctx context.Context, directory *os.File, scope AttemptLedgerPreparationScope) (result AttemptLedgerPreparationCensus, resultErr error) {
	vpk, err := scope.validate()
	if err != nil {
		return result, err
	}
	physical, err := openAttemptPreparationView(ctx, directory, scope.Limits)
	if err != nil {
		return result, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, physical.close())
		if resultErr != nil {
			result = AttemptLedgerPreparationCensus{}
		}
	}()
	files, err := physical.census(scope)
	if err != nil {
		return result, err
	}
	disk := &attemptPreparationReadOnlyStorage{view: physical}
	defer func() { resultErr = errors.Join(resultErr, disk.Close()) }()
	db, err := leveldb.Open(disk, &opt.Options{ReadOnly: true, ErrorIfMissing: true, Strict: opt.StrictAll, BlockCacheCapacity: 8 * 1024 * 1024, OpenFilesCacheCapacity: 64})
	if err != nil {
		return result, err
	}
	closed := false
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, db.Close())
		}
	}()
	store := &attemptRecordStore{db: db, identity: attemptRecordStoreIdentity{Schema: attemptStoreSchema, Identity: scope.Identity, Coordinator: scope.Coordinator}, vpk: vpk,
		bounds: attemptRecordStoreBounds{MaxRecordBytes: scope.Limits.MaxRecordBytes, MaxRecordCount: scope.Limits.MaxRecordCount, MaxTrailCount: scope.Limits.MaxTrailCount,
			MaxRawRecordBytes: scope.Limits.MaxRawRecordBytes, MaxStorageBytes: scope.Limits.MaxStorageBytes, MaxStorageFiles: scope.Limits.MaxStorageFiles}}
	raw, err := db.Get([]byte("identity"), nil)
	var identity attemptRecordStoreIdentity
	if err != nil || attemptStoreDecode(raw, &identity) != nil || identity != store.identity {
		return result, errors.Join(errors.New("preparation ledger identity differs from the reviewed owner"), err)
	}
	raw, err = db.Get([]byte("head"), nil)
	if err != nil || attemptStoreDecode(raw, &store.head) != nil || store.head != scope.ExpectedHead {
		return result, errors.Join(errors.New("preparation ledger lost its exact reviewed head"), err)
	}
	if err := store.verifyContents(ctx); err != nil {
		return result, fmt.Errorf("preparation signed ledger prefix: %w", err)
	}
	if err := physical.verifyReceipts(scope, store); err != nil {
		return result, err
	}
	if err := db.Close(); err != nil {
		closed = true
		return result, err
	}
	closed = true
	if err := physical.checkCensus(); err != nil {
		return result, err
	}
	scopeRaw, err := json.Marshal(scope)
	if err != nil {
		return result, err
	}
	result = AttemptLedgerPreparationCensus{Schema: attemptLedgerPreparationSchema, Kind: AttemptLedgerPreparationKind,
		ScopeSha256: attemptLedgerCustodyDigest(scopeRaw), Head: store.head, Files: files, RestartAuthorized: false}
	return result, ctx.Err()
}

// Creates exactly one new private staging child. The empty database is built
// with bounded in-memory backend files, then published and synced under the
// staging FD before inspection. No live target or signed history is initialized.
func BuildFreshAttemptLedgerPreparation(ctx context.Context, stagingParent *os.File, childName string, scope AttemptLedgerPreparationScope) (result AttemptLedgerPreparationCensus, resultErr error) {
	if _, err := scope.validate(); err != nil {
		return result, err
	}
	if ctx == nil || stagingParent == nil || childName == "" || childName == "." || childName == ".." || strings.ContainsAny(childName, "/\x00") || len(childName) > 128 || scope.Legacy != nil || scope.ExpectedHead != (AttemptLedgerHead{Root: zeroAttemptHash()}) {
		return result, errors.New("fresh ledger preparation requires explicit staging and empty public authority")
	}
	if scope.Requests != nil && scope.Requests.ExpectedHead != (ProviderAttemptRequestHead{}) {
		return result, errors.New("fresh request preparation cannot replace a retained original head")
	}
	if err := errors.Join(ctx.Err(), attemptPreparationPrivate(stagingParent, true)); err != nil {
		return result, err
	}
	memory := storage.NewMemStorage()
	defer func() { resultErr = errors.Join(resultErr, memory.Close()) }()
	db, err := leveldb.Open(memory, &opt.Options{ErrorIfExist: true, Strict: opt.StrictAll, WriteBuffer: 128 * 1024, BlockCacheCapacity: 128 * 1024, OpenFilesCacheCapacity: 8})
	if err != nil {
		return result, err
	}
	identityRaw, err := json.Marshal(attemptRecordStoreIdentity{Schema: attemptStoreSchema, Identity: scope.Identity, Coordinator: scope.Coordinator})
	if err != nil {
		return result, errors.Join(err, db.Close())
	}
	headRaw, err := json.Marshal(scope.ExpectedHead)
	if err != nil {
		return result, errors.Join(err, db.Close())
	}
	batch := new(leveldb.Batch)
	batch.Put([]byte("identity"), identityRaw)
	batch.Put([]byte("head"), headRaw)
	writeErr := db.Write(batch, &opt.WriteOptions{Sync: true})
	if err := errors.Join(writeErr, db.Close(), ctx.Err()); err != nil {
		return result, err
	}
	descriptors, err := memory.List(storage.TypeAll)
	if err != nil || uint64(len(descriptors))+2 > scope.Limits.MaxStorageFiles {
		return result, errors.Join(errAttemptRecordStoreLimit, err)
	}
	manifest, err := memory.GetMeta()
	if err != nil {
		return result, err
	}
	prepared := map[string][]byte{"CURRENT": []byte(manifest.String() + "\n"), "LOCK": {}}
	total := uint64(len(prepared["CURRENT"]))
	for _, descriptor := range descriptors {
		reader, err := memory.Open(descriptor)
		if err != nil {
			return result, err
		}
		raw, readErr := io.ReadAll(io.LimitReader(reader, int64(scope.Limits.MaxStorageBytes-total)+1))
		if err := errors.Join(readErr, reader.Close(), ctx.Err()); err != nil {
			return result, err
		}
		if uint64(len(raw)) > scope.Limits.MaxStorageBytes-total {
			return result, errAttemptRecordStoreLimit
		}
		total += uint64(len(raw))
		prepared[descriptor.String()] = raw
	}
	parentFd := int(stagingParent.Fd())
	if err := unix.Mkdirat(parentFd, childName, 0700); err != nil {
		return result, err
	}
	child, err := attemptPreparationOpen(stagingParent, childName, true)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, child.Close()) }()
	if err := unix.Mkdirat(int(child.Fd()), attemptLedgerStoreName, 0700); err != nil {
		return result, err
	}
	backend, err := attemptPreparationOpen(child, attemptLedgerStoreName, true)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, backend.Close()) }()
	names := make([]string, 0, len(prepared))
	for name := range prepared {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := attemptPreparationCreate(ctx, backend, name, prepared[name]); err != nil {
			return result, err
		}
	}
	markerRaw, err := json.Marshal(attemptLedgerImport{Schema: attemptLedgerImportSchema, Identity: scope.Identity, Coordinator: scope.Coordinator, LegacySHA256: attemptHex32(sha256.Sum256(nil))})
	if err != nil {
		return result, err
	}
	readyRaw, err := json.Marshal(attemptLedgerImportReady{Schema: attemptLedgerImportSchema, ImportSHA256: attemptHex32(sha256.Sum256(markerRaw)), Root: zeroAttemptHash()})
	if err != nil {
		return result, err
	}
	if err := attemptPreparationCreate(ctx, child, attemptLedgerImportName, markerRaw); err != nil {
		return result, err
	}
	if err := attemptPreparationCreate(ctx, child, attemptLedgerReadyName, readyRaw); err != nil {
		return result, err
	}
	if scope.Requests != nil {
		if err := attemptPreparationCreate(ctx, child, ProviderAttemptRequestJournalName, nil); err != nil {
			return result, err
		}
	}
	if err := errors.Join(backend.Sync(), child.Sync(), stagingParent.Sync(), ctx.Err()); err != nil {
		return result, err
	}
	return InspectAttemptLedgerPreparation(ctx, child, scope)
}

// Produces only checkpoint bytes using the target's actual inodes. Publication
// and XATTR_CREATE belong to the independently held plan-owned stage. A copied
// or conflicting checkpoint is refused, never overwritten or rebound silently.
func BuildAttemptLedgerPreparationCheckpoint(ctx context.Context, directory *os.File, scope AttemptLedgerPreparationScope, reviewed AttemptLedgerPreparationCensus) (_ []byte, resultErr error) {
	observed, err := InspectAttemptLedgerPreparation(ctx, directory, scope)
	if err != nil {
		return nil, err
	}
	if reviewed.RestartAuthorized || !reflect.DeepEqual(reviewed, observed) {
		return nil, errors.New("prepared ledger differs from its exact reviewed census")
	}
	physical, err := openAttemptPreparationView(ctx, directory, scope.Limits)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, physical.close()) }()
	if _, err := physical.census(scope); err != nil {
		return nil, err
	}
	checkpoint := physical.checkpoint(scope)
	raw, err := json.Marshal(checkpoint)
	if err != nil || len(raw) > 4096 {
		return nil, errors.Join(errors.New("prepared ledger checkpoint exceeds its bound"), err)
	}
	retained, err := readAttemptLedgerCustodyAttribute(directory)
	if err != nil && !errors.Is(err, unix.ENODATA) {
		return nil, err
	}
	if err == nil && !bytes.Equal(raw, retained) {
		return nil, attemptLedgerCustodyLoss("prepared target retains another custody checkpoint", nil)
	}
	return raw, errors.Join(physical.checkCensus(), ctx.Err())
}
