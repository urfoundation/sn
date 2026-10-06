//go:build linux

// Offline native restore preserves original JSONL/SCALE bytes and rebinds only
// physical checkpoint coordinates. No signer, RPC or mutable journal is opened.
package chain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

const nativeJournalRestoreSchema = "urnetwork-native-journal-restore-v1"

// The nomination selects one bounded proof candidate; it grants no signing or
// transaction authority. The old and next authenticated census must prove it.
// Fresh preparation deliberately has no such field and rejects its presence.
type NativeJournalRestoreScope struct {
	NativeJournalPreparationScope
	PendingRawMember string `json:"pending_raw_member,omitempty"`
}

// The reviewed plan retains the complete old checkpoint, including any exact
// append intent; target inode coordinates are derived only during apply.
type nativeJournalRestoreCensus struct {
	Schema          string                    `json:"schema"`
	Scope           NativeJournalRestoreScope `json:"scope"`
	OriginalCustody []byte                    `json:"original_custody"`
	AddedRawMember  string                    `json:"added_raw_member,omitempty"`
}

// A temporary has one exact canonical target and a fixed random-name suffix.
// No path, alternate case or second pending marker can nominate a raw member.
func nativeRestoreRawName(actual string) (string, bool, error) {
	parts := strings.Split(actual, ".pending-")
	if len(parts) > 2 || len(parts[0]) != 70 || !strings.HasSuffix(parts[0], ".scale") || strings.ToLower(actual) != actual {
		return "", false, errors.Join(ErrJournalUncertain, errors.New("native restore raw name is not canonical"))
	}
	if hash, err := hex.DecodeString(parts[0][:64]); err != nil || len(hash) != 32 {
		return "", false, errors.Join(ErrJournalUncertain, errors.New("native restore raw target hash is invalid"))
	}
	if len(parts) == 2 {
		if nonce, err := hex.DecodeString(parts[1]); err != nil || len(nonce) != 16 || len(parts[1]) != 32 {
			return "", false, errors.Join(ErrJournalUncertain, errors.New("native restore raw pending suffix is invalid"))
		}
	}
	return parts[0], len(parts) == 2, nil
}

// Strict public inputs cannot smuggle a signing key or unreviewed extension.
func decodeNativeRestore(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.Join(errors.New("native restore authority has trailing data"), err)
	}
	return nil
}

// A nomination is one explicit choice. JSON's usual last-key-wins behavior
// must not silently select between two different reviewed witness fields.
func decodeNativeRestoreScope(raw []byte, scope *NativeJournalRestoreScope) error {
	if err := decodeNativeRestore(raw, scope); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if _, err := decoder.Token(); err != nil {
		return err
	}
	nominated := false
	for decoder.More() {
		field, err := decoder.Token()
		if err != nil {
			return err
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if field == "pending_raw_member" {
			if nominated {
				return errors.Join(ErrJournalUncertain, errors.New("native restore has ambiguous repeated raw nomination"))
			}
			nominated = true
		}
	}
	return nil
}

// Layout validation consumes the exported original inode census. It accepts
// clean custody, exact pending log appends and a proven one-member raw delta.
// Unknown or incomplete original payloads remain retained and refused.
func PlanNativeJournalRestore(ctx context.Context, name string, owner durablevolume.PreparationOwner, report durablevolume.Inventory) (durablevolume.PreparationOwnerPlan, error) {
	if ctx == nil {
		return durablevolume.PreparationOwnerPlan{}, errors.New("native restore requires cancellation context")
	}
	if err := ctx.Err(); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if owner.Kind != NativeJournalPreparationKind || owner.Purpose != "restore" || owner.RelativePath != "." || report.Schema != durablevolume.PhysicalInventorySchema || report.RestartAuthorized {
		return durablevolume.PreparationOwnerPlan{}, errors.New("native restore requires exact physical retained authority")
	}
	var scope NativeJournalRestoreScope
	if err := decodeNativeRestoreScope(owner.Inputs, &scope); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if err := scope.validate(); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if scope.PendingRawMember != "" {
		if name, temporary, err := nativeRestoreRawName(scope.PendingRawMember); err != nil || temporary || name != scope.PendingRawMember {
			return durablevolume.PreparationOwnerPlan{}, errors.Join(ErrJournalUncertain, errors.New("native restore nomination must be one canonical final raw member"), err)
		}
	}
	if len(report.Entries) < 3 || uint64(len(report.Entries)) > scope.MaximumRawMembers+3 {
		return durablevolume.PreparationOwnerPlan{}, errors.New("native restore original namespace exceeds its fixed member profile")
	}
	var root, rawDirectory, journal *durablevolume.InventoryEntry
	var anchorRaw []byte
	files := make([]durablevolume.PreparationFile, 0, len(report.Entries))
	members := map[string]nativeRawMember{}
	names := map[string]bool{}
	inodes := map[uint64]bool{}
	pendingRaw := ""
	var rawBytes uint64
	for index := range report.Entries {
		if err := ctx.Err(); err != nil {
			return durablevolume.PreparationOwnerPlan{}, err
		}
		entry := &report.Entries[index]
		if entry.Physical == nil || entry.Physical.Inode == 0 || entry.Physical.Device != report.PhysicalRoot.Device || names[entry.Path] || inodes[entry.Physical.Inode] {
			return durablevolume.PreparationOwnerPlan{}, errors.New("native restore lacks original member generations")
		}
		names[entry.Path], inodes[entry.Physical.Inode] = true, true
		if entry.Path != "" {
			files = append(files, durablevolume.PreparationFile{Path: entry.Path, Kind: entry.Kind, Mode: entry.Mode, Bytes: entry.Size, Sha256: entry.Sha256})
		}
		for _, attribute := range entry.OwnerAttributes {
			if entry.Path != "" || attribute.Name != NativeJournalCustodyAttribute && attribute.Name != durablevolume.PreparationAttribute {
				return durablevolume.PreparationOwnerPlan{}, errors.New("native restore cannot omit another owner's checkpoint")
			}
			if attribute.Name == NativeJournalCustodyAttribute {
				if anchorRaw != nil || len(attribute.Value) == 0 || len(attribute.Value) > 4096 || "sha256:"+nativeDigest(attribute.Value) != attribute.Sha256 {
					return durablevolume.PreparationOwnerPlan{}, errors.New("native restore original checkpoint differs")
				}
				anchorRaw = append([]byte(nil), attribute.Value...)
			}
		}
		switch entry.Path {
		case "":
			root = entry
		case journalRawDir:
			rawDirectory = entry
		case journalFileName:
			journal = entry
		default:
			if filepath.Dir(entry.Path) != journalRawDir || entry.Kind != "file" || entry.Mode != 0600 || entry.Size < 1 || entry.Size > scope.MaximumRawRecordBytes || entry.Size > scope.MaximumRawBytes-rawBytes {
				return durablevolume.PreparationOwnerPlan{}, errors.New("native restore contains unknown or unbounded retained members")
			}
			memberName, temporary, err := nativeRestoreRawName(filepath.Base(entry.Path))
			if err != nil {
				return durablevolume.PreparationOwnerPlan{}, err
			}
			if temporary {
				if pendingRaw != "" {
					return durablevolume.PreparationOwnerPlan{}, errors.Join(ErrJournalUncertain, errors.New("native restore has ambiguous pending raw members"))
				}
				pendingRaw = memberName
			}
			if _, exists := members[memberName]; exists || uint64(len(members)) >= scope.MaximumRawMembers {
				return durablevolume.PreparationOwnerPlan{}, errors.New("native restore raw census is duplicated or exceeds its count")
			}
			rawBytes += entry.Size
			members[memberName] = nativeRawMember{Name: memberName, Inode: entry.Physical.Inode, Size: int64(entry.Size), Sha256: strings.TrimPrefix(entry.Sha256, "sha256:")}
		}
	}
	if root == nil || rawDirectory == nil || journal == nil || anchorRaw == nil || root.Kind != "directory" || root.Mode != 0700 || rawDirectory.Kind != "directory" || rawDirectory.Mode != 0700 || journal.Kind != "file" || journal.Mode != 0600 || journal.Size > scope.MaximumJournalBytes {
		return durablevolume.PreparationOwnerPlan{}, errors.New("native restore is missing original log, raw directory or anchor")
	}
	if *root.Physical != report.PhysicalRoot {
		return durablevolume.PreparationOwnerPlan{}, errors.New("native restore original physical root differs")
	}
	var anchor nativeJournalCustody
	if err := decodeNativeRestore(anchorRaw, &anchor); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if anchor.Schema != nativeJournalCustodySchema || anchor.DirectoryInode != root.Physical.Inode || anchor.RawDirectoryInode != rawDirectory.Physical.Inode {
		return durablevolume.PreparationOwnerPlan{}, errors.New("native restore original directory generations differ")
	}
	checkpoints := []nativeJournalCheckpoint{anchor.Committed}
	if anchor.Pending != nil {
		checkpoints = append(checkpoints, *anchor.Pending)
	}
	for _, checkpoint := range checkpoints {
		if checkpoint.JournalInode == 0 || checkpoint.JournalSize < 0 || uint64(checkpoint.JournalSize) > scope.MaximumJournalBytes || checkpoint.RawCount < 0 || uint64(checkpoint.RawCount) > scope.MaximumRawMembers {
			return durablevolume.PreparationOwnerPlan{}, errors.Join(durablevolume.ErrIdentity, errors.New("native restore original checkpoint bounds are invalid"))
		}
		for _, digest := range []string{checkpoint.JournalSha256, checkpoint.RawSha256} {
			decoded, err := hex.DecodeString(digest)
			if err != nil || len(decoded) != 32 || strings.ToLower(digest) != digest {
				return durablevolume.PreparationOwnerPlan{}, errors.Join(durablevolume.ErrIdentity, errors.New("native restore original checkpoint hash is malformed"))
			}
		}
	}
	actual := nativeJournalCheckpoint{JournalInode: journal.Physical.Inode, JournalSize: int64(journal.Size), JournalSha256: strings.TrimPrefix(journal.Sha256, "sha256:"), RawCount: len(members), RawSha256: nativeRawDigest(members)}
	addedRaw := ""
	if anchor.Pending == nil {
		if pendingRaw != "" || scope.PendingRawMember != "" {
			return durablevolume.PreparationOwnerPlan{}, errors.Join(ErrJournalUncertain, errors.New("native restore raw nomination has no original pending checkpoint"))
		}
		if actual != anchor.Committed {
			return durablevolume.PreparationOwnerPlan{}, errors.Join(durablevolume.ErrIdentity, errors.New("native restore completed original census differs"))
		}
	} else {
		old, next := anchor.Committed, *anchor.Pending
		rawDelta := old.RawCount >= 0 && old.RawCount < int(scope.MaximumRawMembers) && next.RawCount == old.RawCount+1 && old.JournalInode == next.JournalInode && old.JournalSize == next.JournalSize && old.JournalSha256 == next.JournalSha256
		if rawDelta {
			addedRaw = pendingRaw
			if scope.PendingRawMember != "" {
				if addedRaw != "" && scope.PendingRawMember != addedRaw {
					return durablevolume.PreparationOwnerPlan{}, errors.Join(ErrJournalUncertain, errors.New("native restore nomination contradicts original pending filename"))
				}
				addedRaw = scope.PendingRawMember
			}
			if addedRaw == "" {
				return durablevolume.PreparationOwnerPlan{}, errors.Join(ErrJournalUncertain, errors.New("native raw restore requires original predecessor member authority through an exact nominated witness"))
			}
			added, present := members[addedRaw]
			if !present || actual != next {
				return durablevolume.PreparationOwnerPlan{}, errors.Join(ErrJournalUncertain, errors.New("native restore nominated raw bytes do not match the exact intended checkpoint"))
			}
			delete(members, addedRaw)
			oldDigest := nativeRawDigest(members)
			members[addedRaw] = added
			if oldDigest != old.RawSha256 || old.RawCount != len(members)-1 {
				return durablevolume.PreparationOwnerPlan{}, errors.Join(ErrJournalUncertain, errors.New("native restore nominated member does not reproduce the original predecessor census"))
			}
		} else {
			if pendingRaw != "" || scope.PendingRawMember != "" || old.JournalInode != next.JournalInode || old.RawCount != next.RawCount || old.RawSha256 != next.RawSha256 || old.RawCount != actual.RawCount || old.RawSha256 != actual.RawSha256 {
				return durablevolume.PreparationOwnerPlan{}, errors.Join(ErrJournalUncertain, errors.New("native restore pending transition is not an exact log append or one-member raw delta"))
			}
			if old.JournalSize < 0 || next.JournalSize < old.JournalSize || uint64(next.JournalSize) > scope.MaximumJournalBytes || actual != old && actual != next {
				return durablevolume.PreparationOwnerPlan{}, errors.Join(ErrJournalUncertain, errors.New("native restore append bytes match neither original exact checkpoint"))
			}
		}
	}
	census, err := json.Marshal(nativeJournalRestoreCensus{Schema: nativeJournalRestoreSchema, Scope: scope, OriginalCustody: anchorRaw, AddedRawMember: addedRaw})
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, ExclusiveRoot: true, Files: files,
		Attributes: []durablevolume.PreparationAttributeSpec{{Path: ".", Name: NativeJournalCustodyAttribute}}, Census: census}, ctx.Err()
}

// Readback binds payload bytes and physical files through real descriptors.
// Failed post-read admission returns no bytes, including complete reads.
func nativeRestoreRead(ctx context.Context, parent *os.File, name string, expected durablevolume.PreparationFile) (result []byte, stat unix.Stat_t, resultErr error) {
	if ctx == nil || parent == nil || filepath.Base(name) != name || name == "." || name == ".." || expected.Kind != "file" || expected.Mode != 0600 || expected.Bytes > 16*1024*1024 {
		return nil, stat, errors.New("native restore read lacks bounded descriptor authority")
	}
	defer func() {
		if resultErr != nil {
			result = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, stat, err
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, stat, nativeObservation(err, true)
	}
	file := os.NewFile(uintptr(fd), name)
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, stat, nativeObservation(err, false)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&07777 != 0600 || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) || stat.Size < 0 || uint64(stat.Size) != expected.Bytes {
		return nil, stat, errors.Join(durablevolume.ErrIdentity, errors.New("native restore payload metadata differs"))
	}
	result = make([]byte, int(expected.Bytes))
	for offset := 0; offset < len(result); {
		if err := ctx.Err(); err != nil {
			return nil, stat, err
		}
		chunk := result[offset:min(offset+64*1024, len(result))]
		n, err := file.ReadAt(chunk, int64(offset))
		if err != nil || n != len(chunk) {
			return nil, stat, nativeObservation(errors.Join(err, io.ErrUnexpectedEOF), false)
		}
		offset += n
	}
	var after, named unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return nil, stat, nativeObservation(err, false)
	}
	if err := unix.Fstatat(int(parent.Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, stat, nativeObservation(err, true)
	}
	if !sameNativeRestoreStat(stat, after) || !sameNativeRestoreStat(after, named) || "sha256:"+nativeDigest(result) != expected.Sha256 {
		return nil, stat, errors.Join(durablevolume.ErrIdentity, errors.New("native restore payload changed or differs from original bytes"))
	}
	return result, stat, ctx.Err()
}

// Access time is irrelevant; content, protection and generation must remain.
func sameNativeRestoreStat(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

// Restore checkpoint construction is read-only. It preserves every original
// append boundary and signature, changing only target inode-bearing hashes.
// Pending appends remain pending for the original joined runtime reconciliation.
func InspectNativeJournalRestore(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, report durablevolume.Inventory) (result []durablevolume.PreparedAttribute, resultErr error) {
	defer func() {
		if resultErr != nil {
			result = nil
		}
	}()
	expected, err := PlanNativeJournalRestore(ctx, owner.StagingName, owner.Owner, report)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(expected, owner) {
		return nil, errors.New("native restore plan changed its exact original census")
	}
	var census nativeJournalRestoreCensus
	var anchor nativeJournalCustody
	if err := decodeNativeRestore(owner.Census, &census); err != nil {
		return nil, err
	}
	if err := decodeNativeRestore(census.OriginalCustody, &anchor); err != nil {
		return nil, err
	}
	rootStat, err := nativePreparationStat(root, true)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(root.Fd()), journalRawDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nativeObservation(err, true)
	}
	rawDirectory := os.NewFile(uintptr(fd), journalRawDir)
	defer func() { resultErr = errors.Join(resultErr, rawDirectory.Close()) }()
	rawStat, err := nativePreparationStat(rawDirectory, true)
	if err != nil {
		return nil, err
	}
	if rawStat.Dev != rootStat.Dev {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("native restore crossed a physical filesystem"))
	}
	members := map[string]nativeRawMember{}
	rawNames := map[string]bool{}
	var journalBytes []byte
	var journalStat unix.Stat_t
	for _, file := range owner.Files {
		if file.Kind == "directory" {
			continue
		}
		parent, name := root, file.Path
		if filepath.Dir(file.Path) == journalRawDir {
			parent, name = rawDirectory, filepath.Base(file.Path)
		}
		raw, stat, err := nativeRestoreRead(ctx, parent, name, file)
		if err != nil {
			return nil, err
		}
		if stat.Dev != rootStat.Dev {
			return nil, errors.Join(durablevolume.ErrIdentity, errors.New("native restored member device differs"))
		}
		if file.Path == journalFileName {
			journalBytes, journalStat = raw, stat
			if _, err := parseNativeJournalEntries(ctx, raw); err != nil {
				return nil, errors.Join(ErrJournalUncertain, err)
			}
		} else {
			canonical, _, err := nativeRestoreRawName(name)
			if err != nil {
				return nil, err
			}
			if strings.TrimPrefix(ExtrinsicHash(raw).Hex(), "0x") != canonical[:64] {
				return nil, errors.Join(durablevolume.ErrIdentity, errors.New("native restored SCALE bytes differ from their original extrinsic hash"))
			}
			if _, present := members[canonical]; present {
				return nil, errors.Join(durablevolume.ErrIdentity, errors.New("native restore contains duplicate canonical raw members"))
			}
			members[canonical] = nativeRawMember{Name: canonical, Inode: stat.Ino, Size: stat.Size, Sha256: nativeDigest(raw)}
			rawNames[name] = true
		}
	}
	if anchor.Pending != nil {
		if anchor.Committed.JournalSize < 0 || anchor.Committed.JournalSize > int64(len(journalBytes)) {
			return nil, errors.Join(durablevolume.ErrIdentity, errors.New("native pending restore original log prefix is outside retained bounds"))
		}
		if nativeDigest(journalBytes[:anchor.Committed.JournalSize]) != anchor.Committed.JournalSha256 {
			return nil, errors.Join(durablevolume.ErrIdentity, errors.New("native pending restore changed the acknowledged original log prefix"))
		}
	}
	names, readErr := rawDirectory.Readdirnames(int(census.Scope.MaximumRawMembers + 1))
	if readErr != nil && readErr != io.EOF {
		return nil, nativeObservation(readErr, false)
	}
	if len(names) != len(members) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("native restored raw namespace changed"))
	}
	for _, name := range names {
		if !rawNames[name] {
			return nil, errors.Join(durablevolume.ErrIdentity, errors.New("native restore contains an extra raw member"))
		}
	}
	if err := nativePreparationNamed(root, journalRawDir, rawDirectory, rawStat, true); err != nil {
		return nil, err
	}
	after, err := nativePreparationStat(root, true)
	if err != nil {
		return nil, err
	}
	if !sameNativeRestoreStat(rootStat, after) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("native restored root changed during semantic inspection"))
	}
	anchor.DirectoryInode, anchor.RawDirectoryInode = rootStat.Ino, rawStat.Ino
	nextRawDigest := nativeRawDigest(members)
	if census.AddedRawMember != "" {
		if _, present := members[census.AddedRawMember]; !present {
			return nil, errors.Join(ErrJournalUncertain, errors.New("native restore lost the proven raw delta member"))
		}
		delete(members, census.AddedRawMember)
	}
	anchor.Committed.JournalInode, anchor.Committed.RawSha256 = journalStat.Ino, nativeRawDigest(members)
	if anchor.Pending != nil {
		anchor.Pending.JournalInode, anchor.Pending.RawSha256 = journalStat.Ino, nextRawDigest
	}
	encoded, err := json.Marshal(anchor)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	return []durablevolume.PreparedAttribute{{Spec: owner.Attributes[0], Raw: encoded}}, nil
}
