//go:build linux || darwin

// Offline ledger restore authenticates the complete original signed history
// and migration receipts. It changes only physical custody coordinates; the
// existing runtime remains responsible for one exact pending-record replay.
package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

const attemptLedgerRestoreSchema = "urnetwork-attempt-ledger-restore-v1"

// The external reviewed head is the original acknowledged checkpoint. A
// pending successor remains separately retained, not relabeled acknowledged.
type attemptLedgerRestoreCensus struct {
	Schema                 string                        `json:"schema"`
	Scope                  AttemptLedgerPreparationScope `json:"scope"`
	OriginalCustody        []byte                        `json:"original_custody"`
	OriginalRequestCustody []byte                        `json:"original_request_custody,omitempty"`
}

// Public inputs may use whitespace but may not introduce unknown authority.
func decodeAttemptLedgerRestore(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.Join(errors.New("ledger restore contains trailing data"), err)
	}
	return nil
}

// A pure original-inventory check precedes staging. Unknown or partial members
// are not silently omitted, and no complete pending record is re-created.
func PlanAttemptLedgerRestore(ctx context.Context, name string, owner durablevolume.PreparationOwner, report durablevolume.Inventory) (durablevolume.PreparationOwnerPlan, error) {
	if ctx == nil || owner.Kind != AttemptLedgerPreparationKind || owner.Purpose != "restore" || owner.RelativePath != "." || name == "" || filepath.Base(name) != name || name == "." || name == ".." || strings.ContainsAny(name, "\x00\n\r") || len(name) > 128 {
		return durablevolume.PreparationOwnerPlan{}, errors.New("ledger restore requires its fixed public owner profile")
	}
	if err := ctx.Err(); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	var scope AttemptLedgerPreparationScope
	if err := decodeAttemptLedgerRestore(owner.Inputs, &scope); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if _, err := scope.validate(); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	maximumEntries := scope.Limits.MaxStorageFiles + 6
	if scope.Requests != nil {
		maximumEntries += 2
	}
	if report.Schema != durablevolume.PhysicalInventorySchema || report.RestartAuthorized || len(report.Entries) < 5 || uint64(len(report.Entries)) > maximumEntries {
		return durablevolume.PreparationOwnerPlan{}, errors.New("ledger restore requires the complete bounded original physical inventory")
	}
	entries := map[string]durablevolume.InventoryEntry{}
	inodes := map[uint64]bool{}
	files := make([]durablevolume.PreparationFile, 0, len(report.Entries)-1)
	var original []byte
	var originalRequest []byte
	used, metadata, backendCount := uint64(attemptStoreMetadataReserve), uint64(0), uint64(0)
	for _, entry := range report.Entries {
		if err := ctx.Err(); err != nil {
			return durablevolume.PreparationOwnerPlan{}, err
		}
		if entry.Physical == nil || entry.Physical.Inode == 0 || entry.Physical.Device != report.PhysicalRoot.Device || inodes[entry.Physical.Inode] {
			return durablevolume.PreparationOwnerPlan{}, attemptLedgerCustodyLoss("ledger restore original generations are missing or aliased", nil)
		}
		if _, exists := entries[entry.Path]; exists {
			return durablevolume.PreparationOwnerPlan{}, errors.New("ledger restore repeats an original member")
		}
		entries[entry.Path], inodes[entry.Physical.Inode] = entry, true
		for _, attribute := range entry.OwnerAttributes {
			if entry.Path == "" && attribute.Name == durablevolume.PreparationAttribute {
				continue
			}
			if entry.Path == "" && attribute.Name == ProviderAttemptRequestAttribute && scope.Requests != nil {
				if originalRequest != nil || len(attribute.Value) == 0 || len(attribute.Value) > 4096 || attemptLedgerCustodyDigest(attribute.Value) != attribute.Sha256 {
					return durablevolume.PreparationOwnerPlan{}, attemptLedgerCustodyLoss("ledger restore changed original request checkpoint bytes", nil)
				}
				originalRequest = append([]byte(nil), attribute.Value...)
				continue
			}
			if entry.Path != "" || attribute.Name != attemptLedgerCustodyAttribute || original != nil || len(attribute.Value) == 0 || len(attribute.Value) > 4096 || attemptLedgerCustodyDigest(attribute.Value) != attribute.Sha256 {
				return durablevolume.PreparationOwnerPlan{}, errors.New("ledger restore cannot omit another owner or change its original checkpoint")
			}
			original = append([]byte(nil), attribute.Value...)
		}
		if entry.Path == "" || entry.Path == attemptLedgerStoreName {
			if entry.Kind != "directory" || entry.Mode != 0700 || entry.Size != 0 {
				return durablevolume.PreparationOwnerPlan{}, errors.New("ledger restore original directories differ from private custody")
			}
			if entry.Path == "" && *entry.Physical != report.PhysicalRoot {
				return durablevolume.PreparationOwnerPlan{}, errors.New("ledger restore root differs from original physical authority")
			}
		} else {
			if entry.Kind != "file" || entry.Mode != 0600 {
				return durablevolume.PreparationOwnerPlan{}, errors.New("ledger restore has nonprivate or nonregular original members")
			}
			maximum := uint64(0)
			switch entry.Path {
			case attemptLedgerImportName:
				maximum = scope.Limits.MaxRecordBytes*6 + 4096
			case attemptLedgerReadyName:
				maximum = 4096
			case attemptLedgerLegacyName:
				if scope.Legacy != nil {
					maximum = scope.Limits.MaxLegacyBytes
				}
			case attemptLedgerPendingName:
				maximum = scope.Limits.MaxRecordBytes
			case ProviderAttemptRequestJournalName:
				if scope.Requests != nil {
					maximum = scope.Requests.Preparation.Limits.MaxJournalBytes
				}
			case ProviderAttemptRequestPendingName:
				if scope.Requests != nil {
					maximum = scope.Requests.Preparation.Limits.MaxRecordBytes
				}
			default:
				leaf := filepath.Base(entry.Path)
				if filepath.Dir(entry.Path) != attemptLedgerStoreName {
					return durablevolume.PreparationOwnerPlan{}, errors.New("ledger restore contains an unknown original owner member")
				}
				if _, known := attemptPreparationDescriptor(leaf); !known && leaf != "CURRENT" && leaf != "CURRENT.bak" && leaf != "LOCK" && leaf != "LOG" && leaf != "LOG.old" {
					return durablevolume.PreparationOwnerPlan{}, errors.New("ledger restore contains an unknown or partial backend member")
				}
				backendCount++
				if backendCount > scope.Limits.MaxStorageFiles {
					return durablevolume.PreparationOwnerPlan{}, errAttemptRecordStoreLimit
				}
				maximum = scope.Limits.MaxStorageBytes
				if attemptStoreMetadataName(leaf) {
					if entry.Size > attemptStoreMetadataReserve-metadata {
						return durablevolume.PreparationOwnerPlan{}, errAttemptRecordStoreLimit
					}
					metadata += entry.Size
				} else {
					if entry.Size > scope.Limits.MaxStorageBytes-used {
						return durablevolume.PreparationOwnerPlan{}, errAttemptRecordStoreLimit
					}
					used += entry.Size
				}
			}
			if maximum == 0 || entry.Size > maximum {
				return durablevolume.PreparationOwnerPlan{}, errAttemptRecordStoreLimit
			}
		}
		if entry.Path != "" {
			files = append(files, durablevolume.PreparationFile{Path: entry.Path, Kind: entry.Kind, Mode: entry.Mode, Bytes: entry.Size, Sha256: entry.Sha256})
		}
	}
	for _, required := range []string{"", attemptLedgerStoreName, attemptLedgerImportName, attemptLedgerReadyName, attemptLedgerStoreName + "/LOCK", attemptLedgerStoreName + "/CURRENT"} {
		if _, present := entries[required]; !present {
			return durablevolume.PreparationOwnerPlan{}, attemptLedgerCustodyLoss("ledger restore is missing original database or migration custody", nil)
		}
	}
	var checkpoint attemptLedgerCustodyCheckpoint
	if original == nil {
		return durablevolume.PreparationOwnerPlan{}, attemptLedgerCustodyLoss("ledger restore original checkpoint is missing", nil)
	}
	if err := attemptStoreDecode(original, &checkpoint); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if checkpoint.Schema != attemptLedgerCustodySchema || checkpoint.Identity != scope.Identity || checkpoint.Coordinator != scope.Coordinator || checkpoint.DirectoryInode != report.PhysicalRoot.Inode || checkpoint.DatabaseInode != entries[attemptLedgerStoreName].Physical.Inode || checkpoint.Committed != scope.ExpectedHead {
		return durablevolume.PreparationOwnerPlan{}, attemptLedgerCustodyLoss("ledger restore original namespace or acknowledged head differs", nil)
	}
	matches := func(name string, member attemptLedgerCustodyMember) bool {
		entry, ok := entries[name]
		return ok && member.Inode != 0 && entry.Physical.Inode == member.Inode && entry.Size == member.Bytes && entry.Sha256 == member.Sha256
	}
	if !matches(attemptLedgerImportName, checkpoint.Import) || !matches(attemptLedgerReadyName, checkpoint.Ready) {
		return durablevolume.PreparationOwnerPlan{}, attemptLedgerCustodyLoss("ledger restore original migration receipt differs", nil)
	}
	if (scope.Legacy == nil) != (checkpoint.Legacy == nil) {
		return durablevolume.PreparationOwnerPlan{}, attemptLedgerCustodyLoss("ledger restore changed explicit legacy presence", nil)
	}
	if scope.Legacy != nil && (!matches(attemptLedgerLegacyName, *checkpoint.Legacy) || checkpoint.Legacy.Bytes != scope.Legacy.Bytes || checkpoint.Legacy.Sha256 != scope.Legacy.Sha256) {
		return durablevolume.PreparationOwnerPlan{}, attemptLedgerCustodyLoss("ledger restore original legacy bytes differ", nil)
	}
	if pending := checkpoint.Pending; pending != nil {
		nextScope := scope
		nextScope.ExpectedHead = pending.Head
		if _, err := nextScope.validate(); err != nil {
			return durablevolume.PreparationOwnerPlan{}, err
		}
		if checkpoint.Committed.LastSequence == ^uint64(0) || pending.Head.LastSequence != checkpoint.Committed.LastSequence+1 || pending.Record.Bytes == 0 || pending.Record.Bytes > scope.Limits.MaxRecordBytes || pending.Record.Bytes > scope.Limits.MaxRawRecordBytes-checkpoint.Committed.RecordBytes || pending.Head.RecordBytes != checkpoint.Committed.RecordBytes+pending.Record.Bytes || pending.Head.TrailCount < checkpoint.Committed.TrailCount || pending.Head.TrailCount > checkpoint.Committed.TrailCount+1 {
			return durablevolume.PreparationOwnerPlan{}, attemptLedgerCustodyLoss("ledger restore pending head does not extend original authority", nil)
		}
		entry, present := entries[attemptLedgerPendingName]
		if !present || entry.Size != pending.Record.Bytes || entry.Sha256 != pending.Record.Sha256 || pending.Record.Inode != 0 && entry.Physical.Inode != pending.Record.Inode {
			return durablevolume.PreparationOwnerPlan{}, errors.Join(ErrDurablePublicationUncertain, errors.New("ledger restore lacks complete original pending record bytes"))
		}
	}
	if err := validateAttemptLedgerRequestRestore(scope, report.PhysicalRoot.Inode, entries, originalRequest); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	census, err := json.Marshal(attemptLedgerRestoreCensus{Schema: attemptLedgerRestoreSchema, Scope: scope, OriginalCustody: original, OriginalRequestCustody: originalRequest})
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	attributes := []durablevolume.PreparationAttributeSpec{{Path: ".", Name: attemptLedgerCustodyAttribute}}
	if scope.Requests != nil {
		attributes = append(attributes, durablevolume.PreparationAttributeSpec{Path: ".", Name: ProviderAttemptRequestAttribute})
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, ExclusiveRoot: true, Files: files, Census: census, Attributes: attributes}, ctx.Err()
}

// The read-only engine shares exact signature/index validation with production.
// Its caller owns the retained descriptors and closes the database before them.
func (self *attemptPreparationView) inspectRestoreDatabase(scope AttemptLedgerPreparationScope, checkpoint attemptLedgerCustodyCheckpoint) (resultErr error) {
	vpk, err := scope.validate()
	if err != nil {
		return err
	}
	disk := &attemptPreparationReadOnlyStorage{view: self}
	defer func() { resultErr = errors.Join(resultErr, disk.Close()) }()
	db, err := leveldb.Open(disk, &opt.Options{ReadOnly: true, ErrorIfMissing: true, Strict: opt.StrictAll, BlockCacheCapacity: 8 * 1024 * 1024, OpenFilesCacheCapacity: 64})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, db.Close()) }()
	store := &attemptRecordStore{db: db, identity: attemptRecordStoreIdentity{Schema: attemptStoreSchema, Identity: scope.Identity, Coordinator: scope.Coordinator}, vpk: vpk,
		bounds: attemptRecordStoreBounds{MaxRecordBytes: scope.Limits.MaxRecordBytes, MaxRecordCount: scope.Limits.MaxRecordCount, MaxTrailCount: scope.Limits.MaxTrailCount, MaxRawRecordBytes: scope.Limits.MaxRawRecordBytes, MaxStorageBytes: scope.Limits.MaxStorageBytes, MaxStorageFiles: scope.Limits.MaxStorageFiles}}
	raw, err := db.Get([]byte("identity"), nil)
	var identity attemptRecordStoreIdentity
	if err != nil || attemptStoreDecode(raw, &identity) != nil || identity != store.identity {
		return errors.Join(errors.New("ledger restore database changed original identity"), err)
	}
	raw, err = db.Get([]byte("head"), nil)
	if err != nil || attemptStoreDecode(raw, &store.head) != nil || store.head != checkpoint.Committed && (checkpoint.Pending == nil || store.head != checkpoint.Pending.Head) {
		return errors.Join(errors.New("ledger restore database is neither original retained head"), err)
	}
	if err := store.verifyContents(self.ctx); err != nil {
		return fmt.Errorf("ledger restore original signed prefix: %w", err)
	}
	if err := self.verifyReceipts(scope, store); err != nil {
		return err
	}
	if err := self.verifyRestorePending(store, checkpoint); err != nil {
		return err
	}
	return self.checkCensus()
}

// No append is issued here. A complete pending record must be the exact next
// valid lifecycle, or the exact already committed tail awaiting cleanup.
func (self *attemptPreparationView) verifyRestorePending(store *attemptRecordStore, checkpoint attemptLedgerCustodyCheckpoint) error {
	_, present := self.members[attemptLedgerPendingName]
	if !present {
		if checkpoint.Pending != nil {
			return errors.Join(ErrDurablePublicationUncertain, errors.New("ledger restore pending record disappeared"))
		}
		return nil
	}
	raw, err := self.readRoot(attemptLedgerPendingName)
	if err != nil {
		return err
	}
	record, err := store.decodeRecord(raw)
	if err != nil {
		return fmt.Errorf("ledger restore original pending signature: %w", err)
	}
	pending := checkpoint.Pending
	if pending == nil || store.head == pending.Head {
		if record.Sequence != store.head.LastSequence || record.RecordHash != store.head.Root {
			return attemptLedgerCustodyLoss("ledger restore cleanup record is not original committed tail", nil)
		}
		retained, err := store.readRecord(record.Sequence)
		if err != nil {
			return err
		}
		retainedRaw, err := json.Marshal(retained)
		if err != nil || !bytes.Equal(raw, retainedRaw) {
			return attemptLedgerCustodyLoss("ledger restore cleanup bytes differ from signed database", err)
		}
		if pending == nil {
			return self.checkCensus()
		}
	}
	if record.Sequence != pending.Head.LastSequence || record.RecordHash != pending.Head.Root || record.PreviousHash != checkpoint.Committed.Root || uint64(len(raw)) != pending.Record.Bytes || attemptLedgerCustodyDigest(raw) != pending.Record.Sha256 {
		return attemptLedgerCustodyLoss("ledger restore pending record does not retain original successor", nil)
	}
	newTrail := false
	if store.head == pending.Head {
		prefix := append([]byte(attemptStoreTrailRecordPrefix), record.TrailID[:]...)
		iterator := store.db.NewIterator(util.BytesPrefix(prefix), &opt.ReadOptions{Strict: opt.StrictAll})
		valid := iterator.First() && len(iterator.Key()) == len(prefix)+8
		if valid {
			newTrail = binary.BigEndian.Uint64(iterator.Key()[len(prefix):]) == record.Sequence
		}
		iteratorErr := iterator.Error()
		iterator.Release()
		if !valid || iteratorErr != nil {
			return errors.Join(errors.New("ledger restore lost pending trail ancestry"), iteratorErr)
		}
	} else {
		pendingTrails := map[connect.Id]AttemptRecord{}
		terminalTrails := map[connect.Id]bool{}
		rawState, stateErr := store.db.Get(attemptStoreTrailKey(record.TrailID), nil)
		newTrail = errors.Is(stateErr, leveldb.ErrNotFound)
		if !newTrail {
			var state attemptRecordStoreTrail
			if stateErr != nil || attemptStoreDecode(rawState, &state) != nil {
				return errors.Join(errors.New("ledger restore pending predecessor is unreadable"), stateErr)
			}
			prior, err := store.readRecord(state.LastSequence)
			if err != nil {
				return err
			}
			if prior.TrailID != record.TrailID || prior.RecordHash != state.RecordHash || state.Terminal != (prior.Disposition != AttemptDispositionPending) {
				return errors.New("ledger restore pending predecessor differs")
			}
			if state.Terminal {
				terminalTrails[record.TrailID] = true
			} else {
				pendingTrails[record.TrailID] = prior
			}
		}
		if err := attemptStoreCheckLifecycle(pendingTrails, terminalTrails, record, int(checkpoint.Committed.LastSequence)); err != nil {
			return err
		}
	}
	trails := checkpoint.Committed.TrailCount
	if newTrail {
		trails++
	}
	if trails != pending.Head.TrailCount {
		return attemptLedgerCustodyLoss("ledger restore pending trail count differs from original lifecycle", nil)
	}
	return self.checkCensus()
}

// Borrows the target after exact file publication. Only attribute bytes are
// returned; no database, signature, migration receipt or pending record changes.
func InspectAttemptLedgerRestore(ctx context.Context, directory *os.File, owner durablevolume.PreparationOwnerPlan, report durablevolume.Inventory) (result []durablevolume.PreparedAttribute, resultErr error) {
	expected, err := PlanAttemptLedgerRestore(ctx, owner.StagingName, owner.Owner, report)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(expected, owner) {
		return nil, errors.New("ledger restore changed its exact reviewed owner census")
	}
	var census attemptLedgerRestoreCensus
	var checkpoint attemptLedgerCustodyCheckpoint
	if err := attemptStoreDecode(owner.Census, &census); err != nil {
		return nil, err
	}
	if census.Schema != attemptLedgerRestoreSchema {
		return nil, errors.New("ledger restore census schema differs")
	}
	if err := attemptStoreDecode(census.OriginalCustody, &checkpoint); err != nil {
		return nil, err
	}
	physical, err := openAttemptPreparationView(ctx, directory, census.Scope.Limits)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, physical.close())
		if resultErr != nil {
			result = nil
		}
	}()
	files, err := physical.censusMembers(census.Scope, true)
	if err != nil {
		return nil, err
	}
	if len(files) != len(owner.Files) {
		return nil, attemptLedgerCustodyLoss("ledger restore target member census differs", nil)
	}
	for index, file := range files {
		want := owner.Files[index]
		if file.Path != want.Path || file.Kind != want.Kind || file.Mode != want.Mode || file.Bytes != want.Bytes || file.Sha256 != want.Sha256 {
			return nil, attemptLedgerCustodyLoss("ledger restore target bytes differ from reviewed source", nil)
		}
	}
	if err := physical.inspectRestoreDatabase(census.Scope, checkpoint); err != nil {
		return nil, err
	}
	rebound := physical.checkpoint(census.Scope)
	if pending := checkpoint.Pending; pending != nil {
		member := pending.Record
		if member.Inode != 0 {
			member.Inode = physical.members[attemptLedgerPendingName].stat.Ino
		}
		rebound.Pending = &attemptLedgerCustodyPending{Head: pending.Head, Record: member}
	}
	raw, err := json.Marshal(rebound)
	if err != nil || len(raw) > 4096 {
		return nil, errors.Join(errors.New("ledger restore checkpoint exceeds fixed capacity"), err)
	}
	if !physical.anchorAbsent && !bytes.Equal(physical.anchor, raw) {
		return nil, attemptLedgerCustodyLoss("ledger restore target retains a different physical checkpoint", nil)
	}
	if err := physical.checkCensus(); err != nil {
		return nil, err
	}
	result = []durablevolume.PreparedAttribute{{Spec: owner.Attributes[0], Raw: raw}}
	if census.Scope.Requests != nil {
		requestRaw, err := physical.requestCheckpoint(census.Scope, census.OriginalRequestCustody, true)
		if err != nil {
			return nil, err
		}
		result = append(result, durablevolume.PreparedAttribute{Spec: owner.Attributes[1], Raw: requestRaw})
	}
	return result, ctx.Err()
}
