//go:build linux || darwin

// Unsealed inventory reads the service's real ledger bytes into separate
// custody scratch. It never opens a producer database or a private signing key.
// Signed records remain liability until their distinct tail boundaries are
// independently authenticated by the canonical historical chain reader.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"github.com/syndtr/goleveldb/leveldb/util"
)

const ProductionBootstrapUnsealedSchema = "urnetwork-production-bootstrap-unsealed-inventory-v1"
const productionBootstrapUnsealedMaximumBytes = 64 * 1024 * 1024

// The existing committed proof supplies the operator, generation and epoch.
// PendingHash commits the exact final checkpoints of every unfinished trail.
type ProductionBootstrapUnsealedLedger struct {
	NoId              uint64                                    `json:"no_id"`
	Head              AttemptLedgerHead                         `json:"head"`
	UnsealedRecords   uint64                                    `json:"unsealed_records"`
	PendingTrails     uint64                                    `json:"pending_trails"`
	PendingHash       string                                    `json:"pending_hash"`
	TailBoundaryProof *ProductionBootstrapUnsealedBoundaryProof `json:"tail_boundary_proof,omitempty"`
}

// Only existing, fully imported disk ledgers and an absent or canonically
// empty intent store are supported. A missing ledger never becomes empty
// history. Nonempty intents require the complete measurement/receipt graph.
// Neither this inventory nor an empty local intent file proves signer custody.
type ProductionBootstrapUnsealedObservation struct {
	Schema            string                              `json:"schema"`
	CensusHash        string                              `json:"source_census_hash"`
	SourceCount       uint64                              `json:"source_count"`
	SourceBytes       uint64                              `json:"source_bytes"`
	IntentFilePresent bool                                `json:"intent_file_present"`
	IntentFileHash    string                              `json:"intent_file_hash,omitempty"`
	Ledgers           []ProductionBootstrapUnsealedLedger `json:"ledgers"`
}

// Reopened custody checks the projection's scope independently of its outer
// hash. This is shape validation; only the actual source replay creates it.
func (self ProductionBootstrapUnsealedObservation) Validate(prefixes []ProductionBootstrapOperatorPrefix) error {
	if self.Schema != ProductionBootstrapUnsealedSchema || len(prefixes) != 2 || len(self.Ledgers) != 2 || self.SourceCount < 5 || self.SourceCount > productionBootstrapCommittedMaximumObjects || self.SourceBytes == 0 || self.SourceBytes > productionBootstrapUnsealedMaximumBytes {
		return errors.New("unsealed inventory lacks its bounded complete scope")
	}
	if (self.Ledgers[0].TailBoundaryProof == nil) != (self.Ledgers[1].TailBoundaryProof == nil) {
		return errors.New("unsealed inventory has only partial tail boundary authority")
	}
	if _, err := parseReleaseContentHash(self.CensusHash); err != nil {
		return err
	}
	if self.IntentFilePresent {
		if _, err := parseReleaseContentHash(self.IntentFileHash); err != nil {
			return err
		}
	} else if self.IntentFileHash != "" {
		return errors.New("unsealed inventory invents absent intent bytes")
	}
	for i, ledger := range self.Ledgers {
		prefix := prefixes[i]
		if ledger.NoId == 0 || ledger.NoId != prefix.NoId || ledger.Head.LastSequence < prefix.LastSequence || ledger.UnsealedRecords != ledger.Head.LastSequence-prefix.LastSequence || ledger.PendingTrails > ledger.UnsealedRecords || ledger.PendingTrails > ledger.Head.TrailCount || ledger.Head.TrailCount > ledger.Head.LastSequence ||
			ledger.Head.LastSequence == 0 && (ledger.Head.Root != zeroAttemptHash() || ledger.Head.RecordBytes != 0 || ledger.Head.TrailCount != 0) ||
			ledger.Head.LastSequence > 0 && (ledger.Head.RecordBytes == 0 || ledger.Head.TrailCount == 0) || ledger.UnsealedRecords == 0 && ledger.Head.Root != prefix.Root {
			return errors.New("unsealed inventory differs from its committed operator prefix")
		}
		if _, err := canonicalAttemptHex32("unsealed ledger root", ledger.Head.Root, ledger.Head.LastSequence == 0); err != nil {
			return err
		}
		if _, err := parseReleaseContentHash(ledger.PendingHash); err != nil {
			return err
		}
		if ledger.PendingTrails == 0 && ledger.PendingHash != ReleaseMeasurementContentHash(nil) {
			return errors.New("unsealed inventory changed its empty pending census")
		}
		if ledger.TailBoundaryProof != nil {
			if err := ledger.TailBoundaryProof.validate(prefix, ledger.UnsealedRecords); err != nil {
				return err
			}
		}
	}
	return nil
}

// All local sources stay held through remote history authentication and real
// closes. Local changes are integrity refusals, never transport retries.
type productionBootstrapUnsealedOwner struct {
	ctx           context.Context
	uid           uint32
	remaining     uint64
	sources       []ReleaseEvidenceV2ArchiveSource
	files         []*releaseMeasurementInputV2Owner
	directories   []*releaseEvidenceV2HistoryDirectory
	hooks         releaseMeasurementInputV2ReadHooks
	tails         []productionBootstrapUnsealedTail
	inventoryHash string
	closed        bool
	closeErr      error
}

// Fixed callers select every name. Both successful absence and occupied files
// retain their original parent and leaf identity until this owner closes.
func (self *productionBootstrapUnsealedOwner) read(path, name string, maximum uint64, optional bool) ([]byte, error) {
	if len(self.sources) >= productionBootstrapCommittedMaximumObjects || maximum == 0 {
		return nil, errors.New("unsealed source census exceeds its finite bound")
	}
	owner, err := acquireReleaseMeasurementInputV2Owner(self.ctx, path, max(uint64(1), min(maximum, self.remaining)), self.hooks, false)
	self.files = append(self.files, owner)
	if err != nil {
		return nil, err
	}
	if owner.directory.anchor.uid != self.uid || owner.directory.anchor.mode&0o077 != 0 {
		return nil, errors.New("unsealed source parent differs from the signed service owner")
	}
	raw, err := owner.read()
	if err != nil {
		if !optional || !owner.initialMissing || !releaseMeasurementInputV2OnlyMissing(err) {
			return nil, err
		}
	} else if owner.witness == nil || owner.witness.uid != self.uid || owner.witness.links != 1 || uint64(len(raw)) > self.remaining {
		return nil, errors.New("unsealed source ownership, link count or byte allowance differs")
	}
	if err := errors.Join(owner.check(), self.ctx.Err()); err != nil {
		return nil, err
	}
	self.remaining -= uint64(len(raw))
	kind := "private"
	if owner.initialMissing {
		kind = "absent"
	}
	self.sources = append(self.sources, ReleaseEvidenceV2ArchiveSource{Source: ReleaseEvidenceV2CaptureSource{Kind: kind, Name: name}, SizeBytes: uint64(len(raw)), ContentHash: ReleaseMeasurementContentHash(raw)})
	return raw, nil
}

// Check every acquired owner even when another has already failed.
func (self *productionBootstrapUnsealedOwner) check() error {
	if self == nil || self.closed {
		return errors.New("unsealed source owner is closed")
	}
	var err error
	for _, file := range self.files {
		err = errors.Join(err, file.check())
	}
	for _, directory := range self.directories {
		err = errors.Join(err, directory.check())
	}
	return errors.Join(err, self.ctx.Err())
}

// Actual close failures join cancellation; no partial inventory can escape.
func (self *productionBootstrapUnsealedOwner) close() error {
	if self.closed {
		return self.closeErr
	}
	self.closed = true
	var err error
	for _, file := range self.files {
		err = errors.Join(err, file.finish())
	}
	for _, directory := range self.directories {
		err = errors.Join(err, directory.close())
	}
	// A later owner's close observer may have changed an earlier leaf. The
	// original descriptors are now closed; repeat only unhooked final witnesses.
	for _, file := range self.files {
		if file == nil {
			continue
		}
		final := *file
		final.closed, final.closeErr, final.hooks = false, nil, releaseMeasurementInputV2ReadHooks{}
		err = errors.Join(err, final.finish())
	}
	for _, directory := range self.directories {
		final := *directory
		final.closed, final.closeErr, final.hooks = false, nil, releaseMeasurementInputV2ReadHooks{}
		final.ctx = context.WithoutCancel(self.ctx)
		err = errors.Join(err, final.close())
	}
	self.closeErr = errors.Join(err, self.ctx.Err())
	return self.closeErr
}

// The archive has already authenticated committed ancestry. Only its original
// identities/keys and replayed cursors may constrain the unsealed extension.
// The public caller also checks every protected ancestor before and after use.
func readProductionBootstrapUnsealed(ctx context.Context, cfg *ReleaseConfig, archive *ReleaseEvidenceV2Archive, current *ProductionBootstrapCommittedObservation, previous *ProductionBootstrapCommittedObservation, scratch string, uid uint32, hooks releaseMeasurementInputV2ReadHooks) (result *ProductionBootstrapUnsealedObservation, resultOwner *productionBootstrapUnsealedOwner, resultErr error) {
	owned := &productionBootstrapUnsealedOwner{ctx: ctx, uid: uid, remaining: productionBootstrapUnsealedMaximumBytes, hooks: hooks}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			resultErr = errors.Join(resultErr, owned.close())
			result, resultOwner = nil, nil
		}
	}()
	if cfg == nil || archive == nil || archive.closed || archive.history == nil || current == nil || len(current.Prefixes) != 2 || len(cfg.Operators) != 2 {
		return nil, nil, errors.New("unsealed inventory lacks its committed replay owner")
	}
	result = &ProductionBootstrapUnsealedObservation{Schema: ProductionBootstrapUnsealedSchema}
	intent, err := owned.read(filepath.Join(cfg.StateDir, "steering-intents.json"), "steering-intents.json", cfg.EvidenceV2.Bounds.IntentFileLimit(), true)
	if err != nil {
		return nil, nil, err
	}
	if intent != nil {
		var file steeringIntentFile
		if err := decodeAttemptStreamV2JSON(intent, cfg.EvidenceV2.Bounds.IntentFileLimit(), &file); err != nil {
			return nil, nil, err
		}
		canonical, err := marshalAttemptSettlementV2JSON(ctx, &file, cfg.EvidenceV2.Bounds.IntentFileLimit(), true, true)
		if err != nil || file.Schema != steeringIntentSchema || !bytes.Equal(intent, canonical) || file.Current != nil || len(file.History) != 0 {
			return nil, nil, errors.Join(errors.New("nonempty or noncanonical steering intent liability requires complete authenticated graph replay"), err)
		}
		result.IntentFilePresent, result.IntentFileHash = true, ReleaseMeasurementContentHash(intent)
	}
	if previous != nil && previous.Unsealed != nil {
		if err := previous.Unsealed.Validate(previous.Prefixes); err != nil {
			return nil, nil, err
		}
		if previous.Unsealed.IntentFilePresent && (!result.IntentFilePresent || previous.Unsealed.IntentFileHash != result.IntentFileHash) {
			return nil, nil, errors.New("unsealed inventory lost its retained empty intent boundary")
		}
	}
	for i, operator := range cfg.Operators {
		prefix := current.Prefixes[i]
		if operator.NoID != prefix.NoId || operator.StateDir == scratch || strings.HasPrefix(operator.StateDir, scratch+string(filepath.Separator)) || strings.HasPrefix(scratch, operator.StateDir+string(filepath.Separator)) {
			return nil, nil, errors.New("unsealed operator routing or scratch namespace differs")
		}
		base := fmt.Sprintf("operator/%d/", operator.NoID)
		initial := archive.history.initial[operator.NoID].InitialCut
		markerRaw, err := owned.read(filepath.Join(operator.StateDir, attemptLedgerImportName), base+attemptLedgerImportName, cfg.EvidenceV2.Bounds.Disk.MaxRecordBytes*6+4096, false)
		if err != nil {
			return nil, nil, err
		}
		readyRaw, err := owned.read(filepath.Join(operator.StateDir, attemptLedgerReadyName), base+attemptLedgerReadyName, 4096, false)
		if err != nil {
			return nil, nil, err
		}
		legacyRaw, err := owned.read(filepath.Join(operator.StateDir, attemptLedgerLegacyName), base+attemptLedgerLegacyName, cfg.EvidenceV2.Bounds.Disk.MaxLegacyBytes, true)
		if err != nil {
			return nil, nil, err
		}
		var marker attemptLedgerImport
		var ready attemptLedgerImportReady
		if err := errors.Join(attemptStoreDecode(markerRaw, &marker), attemptStoreDecode(readyRaw, &ready)); err != nil {
			return nil, nil, err
		}
		if marker.Schema != attemptLedgerImportSchema || marker.Identity != initial.Identity || marker.Coordinator != strings.ToLower(cfg.Coordinator) || marker.LegacyPresent || marker.LegacyBytes != 0 || marker.LegacySHA256 != attemptHex32(sha256.Sum256(nil)) || marker.LocalDevice != 0 || marker.LocalInode != 0 || legacyRaw != nil ||
			ready.Schema != attemptLedgerImportSchema || ready.ImportSHA256 != attemptHex32(sha256.Sum256(markerRaw)) || ready.LastSequence != 0 || ready.Root != zeroAttemptHash() {
			return nil, nil, errors.New("unsealed ledger requires a complete original nonlegacy import boundary")
		}
		directory, err := openReleaseEvidenceV2HistoryDirectoryForUid(ctx, operator.StateDir, []string{attemptLedgerStoreName}, min(cfg.EvidenceV2.Bounds.Disk.MaxStorageFiles, uint64(productionBootstrapCommittedMaximumObjects)), uid, true, hooks)
		if err != nil {
			return nil, nil, err
		}
		owned.directories = append(owned.directories, directory)
		if directory.absent != "" {
			return nil, nil, errors.New("unsealed ledger is absent, not an authenticated empty prefix")
		}
		copyRoot, err := os.MkdirTemp(scratch, fmt.Sprintf("unsealed-no-%d-", operator.NoID))
		if err != nil {
			return nil, nil, err
		}
		for _, name := range slices.Sorted(maps.Keys(directory.entries)) {
			raw, err := owned.read(filepath.Join(directory.root.path, name), base+attemptLedgerStoreName+"/"+name, cfg.EvidenceV2.Bounds.Disk.MaxStorageBytes, false)
			if err != nil {
				return nil, nil, err
			}
			if err := os.WriteFile(filepath.Join(copyRoot, name), raw, 0o600); err != nil {
				return nil, nil, err
			}
		}
		var prior *ProductionBootstrapUnsealedLedger
		if previous != nil && previous.Unsealed != nil {
			prior = &previous.Unsealed.Ledgers[i]
		}
		ledger, boundaries, err := replayProductionBootstrapUnsealed(ctx, copyRoot, cfg.EvidenceV2.Bounds.Disk, initial.Identity, strings.ToLower(cfg.Coordinator), archive.history.keys[operator.NoID], archive.history.current[operator.NoID], current, prefix, prior)
		if err != nil {
			return nil, nil, err
		}
		result.Ledgers = append(result.Ledgers, ledger)
		owned.tails = append(owned.tails, productionBootstrapUnsealedTail{prefix: prefix, ledger: ledger, boundaries: boundaries})
	}
	result.CensusHash, result.SourceCount, result.SourceBytes = productionBootstrapPrefixHash(owned.sources), uint64(len(owned.sources)), productionBootstrapUnsealedMaximumBytes-owned.remaining
	owned.inventoryHash = productionBootstrapPrefixHash(*result)
	return result, owned, errors.Join(result.Validate(current.Prefixes), owned.check())
}

// The strict storage reader never repairs CURRENT or initializes missing
// metadata. Its exclusive database is the detached scratch copy only.
func replayProductionBootstrapUnsealed(ctx context.Context, path string, limits AttemptLedgerDiskLimits, identity AttemptLedgerIdentity, coordinator string, keys map[byte]ed25519.PublicKey, cursor releaseEvidenceV2StartupCursor, current *ProductionBootstrapCommittedObservation, prefix ProductionBootstrapOperatorPrefix, previous *ProductionBootstrapUnsealedLedger) (result ProductionBootstrapUnsealedLedger, resultBoundaries []ProductionBootstrapUnsealedBoundary, resultErr error) {
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = ProductionBootstrapUnsealedLedger{}
			resultBoundaries = nil
		}
	}()
	vpk, err := canonicalAttemptHex32("unsealed original vpk", identity.ValidatorVPK, false)
	if err != nil {
		return result, nil, err
	}
	bounds := attemptRecordStoreBounds{MaxRecordBytes: limits.MaxRecordBytes, MaxRecordCount: limits.MaxRecordCount, MaxTrailCount: limits.MaxTrailCount, MaxRawRecordBytes: limits.MaxRawRecordBytes, MaxStorageBytes: min(limits.MaxStorageBytes, uint64(productionBootstrapUnsealedMaximumBytes)), MaxStorageFiles: min(limits.MaxStorageFiles, uint64(productionBootstrapCommittedMaximumObjects))}
	store := &attemptRecordStore{identity: attemptRecordStoreIdentity{Schema: attemptStoreSchema, Identity: identity, Coordinator: coordinator}, vpk: vpk[:], bounds: bounds}
	disk, err := openAttemptRecordStoreStorage(path, bounds, attemptRecordStoreHooks{}, store.latchFault)
	if err != nil {
		return result, nil, err
	}
	store.disk = disk
	defer func() { resultErr = errors.Join(resultErr, disk.Close()) }()
	if _, err := disk.List(storage.TypeAll); err != nil {
		return result, nil, err
	}
	db, err := leveldb.Open(disk, &opt.Options{ReadOnly: true, ErrorIfMissing: true, Strict: opt.StrictAll, BlockCacheCapacity: 8 * 1024 * 1024, OpenFilesCacheCapacity: 64})
	if err != nil {
		return result, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, db.Close()) }()
	store.db = db
	raw, err := db.Get([]byte("identity"), nil)
	var storedIdentity attemptRecordStoreIdentity
	if err != nil || attemptStoreDecode(raw, &storedIdentity) != nil || storedIdentity != store.identity {
		return result, nil, errors.Join(errors.New("unsealed ledger original identity is absent or changed"), err)
	}
	raw, err = db.Get([]byte("head"), nil)
	if err != nil || attemptStoreDecode(raw, &store.head) != nil {
		return result, nil, errors.Join(errors.New("unsealed ledger lifetime head is absent or changed"), err)
	}
	if err := store.verifyContents(ctx); err != nil {
		return result, nil, err
	}
	match := func(sequence uint64, root string) error {
		if sequence > store.head.LastSequence || sequence == 0 && root != zeroAttemptHash() {
			return errors.New("unsealed ledger lost an authenticated ancestor")
		}
		if sequence != 0 {
			record, err := store.readRecord(sequence)
			if err != nil || record.RecordHash != root {
				return errors.Join(errors.New("unsealed ledger substituted an authenticated ancestor"), err)
			}
		}
		return nil
	}
	if err := match(prefix.LastSequence, prefix.Root); err != nil {
		return result, nil, err
	}
	if previous != nil {
		if previous.NoId != prefix.NoId || previous.Head.RecordBytes > store.head.RecordBytes || previous.Head.TrailCount > store.head.TrailCount || previous.Head.LastSequence == store.head.LastSequence && previous.Head != store.head {
			return result, nil, errors.New("unsealed ledger retained lifetime counters regressed")
		}
		if err := match(previous.Head.LastSequence, previous.Head.Root); err != nil {
			return result, nil, err
		}
	}
	census := &productionBootstrapUnsealedBoundaryCensus{}
	for sequence := uint64(1); sequence <= store.head.LastSequence; sequence++ {
		if err := ctx.Err(); err != nil {
			return result, nil, err
		}
		record, err := store.readRecord(sequence)
		if err != nil {
			return result, nil, err
		}
		if err := verifyAttemptRecord(&record, identity, vpk[:], keys, true); err != nil {
			return result, nil, err
		}
		if !releaseBlockAtOrBefore(record.Boundary.EVMBlock, record.Boundary.EVMBlockHash, current.EvmBlock, current.EvmHash) || sequence > prefix.LastSequence && (record.Boundary.SettlementEpoch != cursor.epoch || !releaseBlockAtOrBefore(cursor.lastBoundary.EVMBlock, cursor.lastBoundary.EVMBlockHash, record.Boundary.EVMBlock, record.Boundary.EVMBlockHash)) {
			return result, nil, errors.New("unsealed signed record exceeds its replayed epoch or current boundary")
		}
		if sequence > prefix.LastSequence {
			if err := census.add(record.Boundary); err != nil {
				return result, nil, err
			}
		}
	}
	result = ProductionBootstrapUnsealedLedger{NoId: identity.NoID, Head: store.head, UnsealedRecords: store.head.LastSequence - prefix.LastSequence}
	digest := sha256.New()
	iterator := db.NewIterator(util.BytesPrefix([]byte(attemptStoreTrailPrefix)), &opt.ReadOptions{Strict: opt.StrictAll, DontFillCache: true})
	defer iterator.Release()
	for iterator.Next() {
		if err := ctx.Err(); err != nil {
			return result, nil, err
		}
		var trail attemptRecordStoreTrail
		if err := attemptStoreDecode(iterator.Value(), &trail); err != nil {
			return result, nil, err
		}
		if !trail.Terminal {
			if trail.LastSequence <= prefix.LastSequence {
				return result, nil, errors.New("unsealed unfinished trail precedes the complete committed cut")
			}
			result.PendingTrails++
			if err := json.NewEncoder(digest).Encode(trail); err != nil {
				return result, nil, err
			}
		}
	}
	result.PendingHash = fmt.Sprintf("sha256:%x", digest.Sum(nil))
	return result, census.sorted(), iterator.Error()
}
