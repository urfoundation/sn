//go:build linux

// Native producer restore is a fixed complete-union owner. Original jobs,
// attempts and trie nodes remain byte-identical; no VM, RPC or signer runs.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"github.com/urnetwork/server/v2026/strecovery"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/sys/unix"
)

const storageNativeProducerKind = "mainnet-native-execution-artifacts"
const storageNativeProducerSchema = "urnetwork-native-execution-artifact-restore-v1"

// Approval bytes are carried exactly, including signatures and original paths.
// They are verified with the original policy key by the runtime's same loader.
// Checkpoint is an independent original-byte pin, not a newly inferred cursor.
type storageNativeProducerScope struct {
	FeePolicyHash     string                              `json:"original_fee_policy_hash,omitempty"`
	ApprovalSources   []storageNativeApprovalSource       `json:"original_signed_approval_sources,omitempty"`
	CheckpointStorage *economicConservationStorageProfile `json:"original_checkpoint_storage,omitempty"`
	Schema            string                              `json:"schema"`
	Checkpoint        monitorHistoryReference             `json:"original_checkpoint"`
	Policy            economicEmissionPolicy              `json:"original_observation_policy"`
	Cursor            economicEmissionBoundary            `json:"original_cursor"`
	State             *nativeExecutionProducerState       `json:"original_producer_state"`
	Approvals         [][]byte                            `json:"original_signed_approval_bytes"`
	SharedDirectories []string                            `json:"shared_original_ancestors,omitempty"`
}

// Exact protected copied files keep large signed approvals outside the fixed
// owner-input envelope. Original policy references still select every digest;
// these paths select only where the already authenticated bytes are retained.
type storageNativeApprovalSource struct {
	Reference planFileReference `json:"reference"`
	Bytes     uint64            `json:"bytes"`
}

// A single bounded inventory hash covers pending and completed custody. The
// target inspector reconstructs semantics from those exact bytes before apply.
type storageNativeProducerCensus struct {
	Schema        string `json:"schema"`
	ScopeHash     string `json:"scope_hash"`
	InventoryHash string `json:"inventory_hash"`
	Files         uint64 `json:"files"`
	Bytes         uint64 `json:"bytes"`
}

// A fully expanded policy reaches the unchanged semantic authority reader.
// Compact fee descriptors are expanded and authenticated by the outer loader.
func readStorageNativeProducerAuthorities(ctx context.Context, scope storageNativeProducerScope) ([]nativeProducerReviewedAuthority, error) {
	if ctx == nil || scope.Schema != storageNativeProducerSchema || scope.Policy.Execution == nil || scope.Policy.Execution.Producer == nil {
		return nil, errors.New("native artifact restore requires its original producer policy")
	}
	maximum := uint64(maxRpcReplyBytes)
	if scope.CheckpointStorage != nil {
		maximum = scope.CheckpointStorage.MaximumBytes
	}
	if err := errors.Join(ctx.Err(), scope.CheckpointStorage.validate(), scope.Checkpoint.validateLimit(maximum), scope.Policy.validate(), scope.State.validate(scope.Policy, scope.Cursor)); err != nil {
		return nil, err
	}
	references := append([]planFileReference{scope.Policy.Execution.Producer.Authority}, scope.Policy.Execution.Producer.Renewals...)
	if scope.ApprovalSources != nil && (scope.Approvals != nil || len(scope.ApprovalSources) != len(references)) || scope.ApprovalSources == nil && len(references) != len(scope.Approvals) {
		return nil, errors.New("native artifact restore omitted original approval lineage")
	}
	index := 0
	return readNativeProducerAuthorities(ctx, scope.Policy, func(ctx context.Context, reference planFileReference) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if index >= len(references) || references[index] != reference {
			return nil, errors.New("native artifact restore changed approval read order")
		}
		raw, err := readStorageNativeApproval(ctx, scope, index, reference)
		index++
		return raw, err
	})
}

// Both projection expansion and full signature validation read the same exact
// bounded source. A later read rechecks custody instead of caching stale bytes.
func readStorageNativeApproval(ctx context.Context, scope storageNativeProducerScope, index int, reference planFileReference) ([]byte, error) {
	var raw []byte
	if scope.ApprovalSources != nil {
		source := scope.ApprovalSources[index]
		if source.Bytes == 0 || source.Bytes > uint64(nativeProducerAuthorityMaximum(scope.Policy.Execution.FeeCensus)) || source.Reference.Sha256 != reference.Sha256 || !bootstrapRootAbsolutePath(source.Reference.Path) {
			return nil, errors.New("native restore approval source differs from its exact original reference")
		}
		var err error
		raw, err = nativeProducerReadApprovalFor(ctx, source.Reference, scope.Policy.Execution.FeeCensus)
		if err != nil {
			return nil, err
		}
		if uint64(len(raw)) != source.Bytes {
			return nil, errors.New("native restore approval source changed original byte count")
		}
	} else {
		raw = scope.Approvals[index]
	}
	if len(raw) == 0 || len(raw) > nativeProducerAuthorityMaximum(scope.Policy.Execution.FeeCensus) || monitorReadDigest(raw) != reference.Sha256 {
		return nil, errors.New("native artifact restore changed original signed approval bytes")
	}
	return raw, nil
}

// Names select only the producer's existing grammar. Pending files preserve
// partial writes as unknown bytes; they cannot stand in for completed records.
func storageNativeProducerMember(path string) (maximum int, pending bool, ok bool) {
	return storageNativeProducerMemberFor(path, nil)
}

// The approved original fee scope selects the same frame as live publication.
func storageNativeProducerMemberFor(path string, fees *nativeFeeCensusPolicy) (maximum int, pending bool, ok bool) {
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[0] == "nodes" {
		name := strings.TrimSuffix(parts[1], ".pending")
		return 16 * 1024 * 1024, name != parts[1], rootCanonicalHash("0x" + name)
	}
	if len(parts) == 2 && parts[0] == "intents" {
		name := strings.TrimSuffix(parts[1], ".pending")
		stem := strings.TrimSuffix(name, ".json")
		number, err := strconv.ParseUint(stem, 10, 32)
		return 16 * 1024, name != parts[1], err == nil && name == fmt.Sprintf("%010d.json", number)
	}
	if len(parts) < 2 || len(parts) > 3 || !storageNativeProducerBoundaryName(parts[0]) {
		return 0, false, false
	}
	if len(parts) == 2 {
		name := strings.TrimSuffix(parts[1], ".pending")
		switch name {
		case "input.json":
			return historicalCaptureRequestLimit, name != parts[1], true
		case "job.json":
			return historicalNativeJobLimit, name != parts[1], true
		case "complete.json":
			return nativeProducerCompletionMaximum(fees), name != parts[1], true
		}
		return 0, false, false
	}
	if parts[1] != "finality" {
		return 0, false, false
	}
	name := parts[2]
	if strings.HasPrefix(name, ".strecovery-") && strings.HasSuffix(name, ".tmp") {
		random := strings.TrimSuffix(strings.TrimPrefix(name, ".strecovery-"), ".tmp")
		raw, err := hex.DecodeString(random)
		return strecovery.MaximumReceiptFinalityBytes, true, err == nil && len(raw) == 16 && hex.EncodeToString(raw) == random
	}
	if name == "native-capture.json" || name == "native-proof.json" {
		return strecovery.MaximumReceiptFinalityBytes, false, true
	}
	for _, prefix := range []string{"request-", "read-"} {
		if strings.HasPrefix(name, prefix) {
			stem := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".json")
			number, err := strconv.ParseUint(stem, 10, 32)
			return 1024, false, err == nil && number > 0 && number <= 32768 && name == fmt.Sprintf("%s%05d.json", prefix, number)
		}
	}
	for _, prefix := range []string{"header-", "certificate-"} {
		if strings.HasPrefix(name, prefix) {
			hash := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".json")
			return 8*1024*1024 + 32, false, name == prefix+hash+".json" && rootCanonicalHash("0x"+hash)
		}
	}
	return 0, false, false
}

func storageNativeProducerBoundaryName(name string) bool {
	if len(name) != 76 || name[0] != 'b' || name[11] != '-' || !rootCanonicalHash("0x"+name[12:]) {
		return false
	}
	number, err := strconv.ParseUint(name[1:11], 10, 32)
	return err == nil && name[:12] == fmt.Sprintf("b%010d-", number)
}

func storageNativeProducerProfile(ctx context.Context, owner durablevolume.PreparationOwner, report durablevolume.Inventory, ownerLocal bool) (storageNativeProducerScope, []nativeProducerReviewedAuthority, string, error) {
	var scope storageNativeProducerScope
	if ownerLocal || owner.Kind != storageNativeProducerKind || owner.Purpose != "restore" || owner.RelativePath != "." || owner.RestoreCoverage != durablevolume.PreparationCompleteUnion {
		return scope, nil, "", errors.New("native artifacts require explicit daemon complete-union restoration")
	}
	if err := decodePlanJson(owner.Inputs, &scope); err != nil {
		return scope, nil, "", err
	}
	scope, authorities, err := loadStorageNativeProducerScope(ctx, scope)
	if err != nil {
		return scope, nil, "", err
	}
	prefix, found := storageNativeRestoreRelative(report.StateRoot.Path, scope.Policy.Execution.Directory)
	if !found || prefix != "." && !storageNativeRestorePath(prefix) {
		return scope, nil, "", errors.New("native artifacts moved outside their original declared root")
	}
	for index, path := range scope.SharedDirectories {
		if !storageNativeRestorePath(path) || !strings.HasPrefix(prefix, path+"/") || index > 0 && scope.SharedDirectories[index-1] >= path {
			return scope, nil, "", errors.New("native artifacts may share only exact sorted original ancestors")
		}
	}
	return scope, authorities, prefix, nil
}

func planStorageNativeProducerRestore(ctx context.Context, name string, owner durablevolume.PreparationOwner, report durablevolume.Inventory, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	var empty durablevolume.PreparationOwnerPlan
	scope, authorities, prefix, err := storageNativeProducerProfile(ctx, owner, report, ownerLocal)
	if err != nil {
		return empty, err
	}
	capacity := authorities[len(authorities)-1].value.capacity()
	result := durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name}
	census := storageNativeProducerCensus{Schema: storageNativeProducerSchema, ScopeHash: rootObjectHash(scope), InventoryHash: rootObjectHash(report)}
	seen, inodes := map[string]bool{}, map[uint64]bool{}
	approvalMembers := storageNativeApprovalMembers(scope, report.StateRoot.Path)
	approvalDirectories := map[string]bool{}
	for path := range approvalMembers {
		for path = filepath.Dir(path); path != "."; path = filepath.Dir(path) {
			approvalDirectories[path] = true
		}
	}
	foundRoot := false
	ancestors := map[string]bool{}
	for path := filepath.Dir(prefix); path != "."; path = filepath.Dir(path) {
		ancestors[path] = true
	}
	for _, path := range scope.SharedDirectories {
		delete(ancestors, path)
	}
	for _, entry := range report.Entries {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		original := entry.Path
		if ancestors[original] {
			if entry.Kind != "directory" || entry.Mode != 0700 || entry.Size != 0 || entry.Sha256 != "" || entry.Physical == nil || entry.Physical.Inode == 0 || entry.Physical.Device != report.PhysicalRoot.Device || len(entry.OwnerAttributes) != 0 {
				return empty, errors.New("native artifact original ancestor changed custody")
			}
			result.Files = append(result.Files, durablevolume.PreparationFile{Path: original, Kind: entry.Kind, Mode: entry.Mode})
			delete(ancestors, original)
			continue
		}
		if prefix != "." {
			if original == prefix {
				entry.Path = ""
			} else if strings.HasPrefix(original, prefix+"/") {
				entry.Path = strings.TrimPrefix(original, prefix+"/")
			} else {
				continue
			}
		}
		if seen[original] || entry.Physical == nil || entry.Physical.Inode == 0 || entry.Physical.Device != report.PhysicalRoot.Device || inodes[entry.Physical.Inode] {
			return empty, errors.New("native artifact source repeats a path or physical generation")
		}
		seen[original], inodes[entry.Physical.Inode] = true, true
		approval, approvalFile := approvalMembers[original]
		if entry.Path == "" {
			foundRoot = entry.Kind == "directory" && entry.Mode == 0700
			if !foundRoot {
				return empty, errors.New("native artifact root lost original private custody")
			}
		} else if entry.Kind == "directory" {
			parts := strings.Split(entry.Path, "/")
			if entry.Mode != 0700 || entry.Size != 0 || entry.Sha256 != "" || !(approvalDirectories[original] || entry.Path == "nodes" || entry.Path == "intents" || len(parts) == 1 && storageNativeProducerBoundaryName(parts[0]) || len(parts) == 2 && storageNativeProducerBoundaryName(parts[0]) && parts[1] == "finality") {
				return empty, errors.New("native artifact directory is outside the fixed original grammar")
			}
		} else if approvalFile {
			if entry.Kind != "file" || entry.Mode != 0600 && entry.Mode != 0400 || entry.Size != approval.Bytes || entry.Sha256 != approval.Sha256 {
				return empty, errors.New("native approval member changed original signed bytes")
			}
		} else {
			maximum, _, recognized := storageNativeProducerMemberFor(entry.Path, scope.Policy.Execution.FeeCensus)
			if !recognized || entry.Kind != "file" || entry.Mode != 0600 && entry.Mode != 0400 || entry.Size > uint64(maximum) || !planSha256(entry.Sha256) {
				return empty, errors.New("native artifact member is outside the fixed original grammar or bound")
			}
		}
		for _, attribute := range entry.OwnerAttributes {
			if original != "" || attribute.Name != durablevolume.PreparationAttribute {
				return empty, errors.New("native artifact restore cannot discard another owner's metadata")
			}
		}
		if original != "" && !approvalFile {
			result.Files = append(result.Files, durablevolume.PreparationFile{Path: original, Kind: entry.Kind, Mode: entry.Mode, Bytes: entry.Size, Sha256: entry.Sha256})
		}
		census.Files++
		if entry.Size > capacity.Bytes-census.Bytes || census.Files > capacity.Entries {
			return empty, errMonitorEconomicCapacity
		}
		census.Bytes += entry.Size
	}
	if !foundRoot || len(result.Files) == 0 || len(ancestors) != 0 {
		return empty, errors.New("native artifact restore requires its exact nonempty original namespace")
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })
	result.Census, err = json.Marshal(census)
	return result, err
}

// Read a copied member under its held no-follow ancestry. Both byte digest
// and named inode are checked; failed reads never become returned mismatches.
func readStorageNativeProducerMember(ctx context.Context, root *os.File, member durablevolume.PreparationFile) (_ []byte, resultErr error) {
	if !storageNativeRestorePath(member.Path) {
		return nil, errors.New("native copied member is outside its bounded original namespace")
	}
	parent, err := openStorageNativeRestoreDirectory(ctx, root, filepath.Dir(member.Path))
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.check(ctx), parent.close()) }()
	directory := parent.files[len(parent.files)-1]
	fd, err := unix.Openat(int(directory.Fd()), filepath.Base(member.Path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, storageMonitorTreeObservation(err)
	}
	file := os.NewFile(uintptr(fd), member.Path)
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return nil, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&07777 != member.Mode || before.Nlink != 1 || before.Uid != uint32(os.Geteuid()) || before.Dev != parent.stats[0].Dev || before.Size < 0 || uint64(before.Size) != member.Bytes {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("native copied artifact custody differs"))
	}
	raw := make([]byte, member.Bytes)
	for offset := 0; offset < len(raw); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		part := raw[offset:min(len(raw), offset+64*1024)]
		n, err := file.ReadAt(part, int64(offset))
		if err != nil || n != len(part) {
			return nil, errors.Join(err, io.ErrUnexpectedEOF)
		}
		offset += n
	}
	var after, named unix.Stat_t
	if err := errors.Join(unix.Fstat(fd, &after), unix.Fstatat(int(directory.Fd()), filepath.Base(member.Path), &named, unix.AT_SYMLINK_NOFOLLOW)); err != nil {
		return nil, err
	}
	if before.Dev != after.Dev || before.Ino != after.Ino || before.Mode != after.Mode || before.Uid != after.Uid || before.Gid != after.Gid || before.Nlink != after.Nlink || before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim || named.Dev != after.Dev || named.Ino != after.Ino || monitorReadDigest(raw) != member.Sha256 {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("native copied artifact bytes or named custody changed"))
	}
	return raw, nil
}

func inspectStorageNativeProducerRestore(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, report durablevolume.Inventory, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	expected, err := planStorageNativeProducerRestore(ctx, owner.StagingName, owner.Owner, report, ownerLocal)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(expected, owner) {
		return nil, errors.New("native artifact restore changed the exact original census")
	}
	scope, authorities, prefix, err := storageNativeProducerProfile(ctx, owner.Owner, report, ownerLocal)
	if err != nil {
		return nil, err
	}
	return nil, validateStorageNativeProducerHistory(ctx, scope, authorities, prefix, owner.Files, func(member durablevolume.PreparationFile) ([]byte, error) {
		return readStorageNativeProducerMember(ctx, root, member)
	})
}

// A completed node's address is its original Blake2 content digest. SHA256
// separately binds restored physical bytes to the reviewed exported inventory.
func storageNativeProducerNodeMatches(name string, raw []byte) bool {
	hash := blake2b.Sum256(raw)
	return len(raw) != 0 && hex.EncodeToString(hash[:]) == name
}

func storageNativeProducerRuntimeMatches(job historicalReplayJob, input historicalCaptureInput) bool {
	raw, err := historicalReplayHex(job.RuntimeCodeHex, 8*1024*1024)
	return err == nil && historicalReplayDigest(sha256.Sum256(raw)) == input.RuntimeCodeSha256 && historicalReplayDigest(blake2b.Sum256(raw)) == input.RuntimeCodeBlake2b256
}
