//go:build linux || darwin

// A fresh runtime tree prepares the parent, operator ledgers and evidence
// directories together. The normal volume plan still owns every publication.
package validator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/urnetwork/connect/durablevolume"
	"golang.org/x/sys/unix"
)

const validatorRuntimePreparationKind = "validator-runtime-tree"
const validatorRuntimePreparationSchema = "urnetwork-validator-runtime-preparation-v1"

// Each ledger is scoped to the fresh hotkey's observed UID and own client key.
type validatorRuntimePreparationLedger struct {
	Directory string                        `json:"directory"`
	Ledger    AttemptLedgerPreparationScope `json:"ledger"`
}

// Relative directories are the complete configured tree, including scratch.
// Keys and configuration remain outside it; this owner cannot copy secrets.
type validatorRuntimePreparationScope struct {
	Schema      string                              `json:"schema"`
	Directories []string                            `json:"directories"`
	Ledgers     []validatorRuntimePreparationLedger `json:"ledgers"`
}

// A portable census contains the original empty database files. Checkpoints
// are derived after publication from the target's actual directory inodes.
type validatorRuntimePreparationCensus struct {
	Schema    string                           `json:"schema"`
	ScopeHash string                           `json:"scope_hash"`
	Ledgers   []AttemptLedgerPreparationCensus `json:"ledgers"`
}

// Validate all paths and ledger profiles before creating the staging tree.
func (self validatorRuntimePreparationScope) validate() error {
	if self.Schema != validatorRuntimePreparationSchema || len(self.Directories) == 0 || len(self.Directories) > 256 || len(self.Ledgers) == 0 || len(self.Ledgers) > 32 {
		return errors.New("validator storage preparation needs a bounded fresh directory and ledger census")
	}
	validPath := func(path string) bool {
		return path != "" && path != "." && !filepath.IsAbs(path) && filepath.Clean(path) == path && path != ".." &&
			!strings.HasPrefix(path, ".."+string(filepath.Separator)) && !strings.ContainsAny(path, "\x00\n\r")
	}
	for index, path := range self.Directories {
		if !validPath(path) || index > 0 && path <= self.Directories[index-1] {
			return errors.New("validator storage directories must be canonical, ordered and unique relative paths")
		}
		parent := filepath.Dir(path)
		if parent != "." && !slices.Contains(self.Directories, parent) {
			return errors.New("validator storage directory census omits a parent")
		}
	}
	for index, entry := range self.Ledgers {
		if !validPath(entry.Directory) || !slices.Contains(self.Directories, entry.Directory) || index > 0 && entry.Directory <= self.Ledgers[index-1].Directory ||
			entry.Ledger.Legacy != nil || entry.Ledger.Requests != nil || entry.Ledger.ExpectedHead != (AttemptLedgerHead{Root: zeroAttemptHash()}) {
			return errors.New("validator storage ledgers must be unique, empty and explicitly scoped")
		}
		if _, err := entry.Ledger.validate(); err != nil {
			return err
		}
		for _, path := range self.Directories {
			if strings.HasPrefix(path, entry.Directory+string(filepath.Separator)) {
				return errors.New("validator storage directory overlaps a ledger's private backend")
			}
		}
	}
	return nil
}

// A caller fixes this adapter before parsing any request. It admits only a
// fresh daemon tree, never restore, owner-wallet custody or arbitrary payloads.
func validatorRuntimePreparationAdapter() durablevolume.PreparationAdapter {
	return durablevolume.PreparationAdapter{
		Build:   buildValidatorRuntimePreparation,
		Inspect: inspectValidatorRuntimePreparation,
	}
}

// Decode the finite public profile without accepting trailing or unknown data.
func decodeValidatorRuntimePreparation(owner durablevolume.PreparationOwner) (validatorRuntimePreparationScope, error) {
	var scope validatorRuntimePreparationScope
	if owner.Kind != validatorRuntimePreparationKind || owner.Purpose != "fresh" || owner.RelativePath != "." || owner.RestoreCoverage != "" {
		return scope, errors.New("validator runtime preparation accepts only its explicit fresh tree owner")
	}
	if err := attemptStoreDecode(owner.Inputs, &scope); err != nil {
		return scope, err
	}
	return scope, scope.validate()
}

// Descriptor-relative traversal never follows a symlink or reopens a root path.
func openValidatorPreparationDirectory(root *os.File, relative string) (_ *os.File, resultErr error) {
	fd, err := unix.Dup(int(root.Fd()))
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "." {
			continue
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		closeErr := unix.Close(fd)
		if err != nil || closeErr != nil {
			if err == nil {
				unix.Close(next)
			}
			return nil, errors.Join(err, closeErr)
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), filepath.Join(root.Name(), relative)), nil
}

// Build original empty ledger files in staging, never at a runtime target.
func buildValidatorRuntimePreparation(ctx context.Context, staging *os.File, name string, owner durablevolume.PreparationOwner) (result durablevolume.PreparationOwnerPlan, resultErr error) {
	scope, err := decodeValidatorRuntimePreparation(owner)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if name == "" || name == "." || filepath.Base(name) != name {
		return result, errors.New("validator staging name is invalid")
	}
	if err := unix.Mkdirat(int(staging.Fd()), name, 0o700); err != nil {
		return result, err
	}
	root, err := openValidatorPreparationDirectory(staging, name)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	files := []durablevolume.PreparationFile{}
	attributes := []durablevolume.PreparationAttributeSpec{}
	census := validatorRuntimePreparationCensus{Schema: validatorRuntimePreparationSchema, ScopeHash: validatorPreparationDigest(owner.Inputs)}
	for _, path := range scope.Directories {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		parent, err := openValidatorPreparationDirectory(root, filepath.Dir(path))
		if err != nil {
			return result, err
		}
		ledgerIndex := slices.IndexFunc(scope.Ledgers, func(entry validatorRuntimePreparationLedger) bool { return entry.Directory == path })
		if ledgerIndex < 0 {
			err = unix.Mkdirat(int(parent.Fd()), filepath.Base(path), 0o700)
		} else {
			var ledger AttemptLedgerPreparationCensus
			ledger, err = BuildFreshAttemptLedgerPreparation(ctx, parent, filepath.Base(path), scope.Ledgers[ledgerIndex].Ledger)
			if err == nil {
				census.Ledgers = append(census.Ledgers, ledger)
				for _, file := range ledger.Files {
					files = append(files, durablevolume.PreparationFile{Path: filepath.Join(path, file.Path), Kind: file.Kind, Mode: file.Mode, Bytes: file.Bytes, Sha256: file.Sha256})
				}
				attributes = append(attributes, durablevolume.PreparationAttributeSpec{Path: path, Name: attemptLedgerCustodyAttribute})
			}
		}
		if err := errors.Join(err, parent.Sync(), parent.Close()); err != nil {
			return result, err
		}
		files = append(files, durablevolume.PreparationFile{Path: path, Kind: "directory", Mode: 0o700})
	}
	// The ledger directory walk and the canonical ledger order are identical.
	if len(census.Ledgers) != len(scope.Ledgers) {
		return result, errors.New("validator staging omitted a ledger")
	}
	raw, err := json.Marshal(census)
	if err := errors.Join(err, root.Sync(), staging.Sync(), ctx.Err()); err != nil {
		return result, err
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, Files: files, Attributes: attributes, Census: raw}, nil
}

// Reopen every declared ledger with the production inspector. The returned
// attributes bind its exact zero head to the target's published inodes.
func inspectValidatorRuntimePreparation(ctx context.Context, target *os.File, owner durablevolume.PreparationOwnerPlan) (result []durablevolume.PreparedAttribute, resultErr error) {
	scope, err := decodeValidatorRuntimePreparation(owner.Owner)
	if err != nil {
		return nil, err
	}
	var census validatorRuntimePreparationCensus
	if err := attemptStoreDecode(owner.Census, &census); err != nil {
		return nil, err
	}
	if census.Schema != validatorRuntimePreparationSchema || census.ScopeHash != validatorPreparationDigest(owner.Owner.Inputs) || len(census.Ledgers) != len(scope.Ledgers) ||
		len(owner.Attributes) != len(scope.Ledgers) || owner.ExclusiveRoot || owner.PhysicalMetadata != nil {
		return nil, errors.New("validator runtime preparation census changed")
	}
	for _, path := range scope.Directories {
		directory, err := openValidatorPreparationDirectory(target, path)
		if err != nil {
			return nil, err
		}
		if err := errors.Join(attemptPreparationPrivate(directory, true), directory.Close()); err != nil {
			return nil, err
		}
	}
	for index, entry := range scope.Ledgers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		spec := durablevolume.PreparationAttributeSpec{Path: entry.Directory, Name: attemptLedgerCustodyAttribute}
		if owner.Attributes[index] != spec {
			return nil, errors.New("validator ledger checkpoint destination changed")
		}
		directory, err := openValidatorPreparationDirectory(target, entry.Directory)
		if err != nil {
			return nil, err
		}
		raw, err := BuildAttemptLedgerPreparationCheckpoint(ctx, directory, entry.Ledger, census.Ledgers[index])
		if err := errors.Join(err, directory.Close()); err != nil {
			return nil, err
		}
		result = append(result, durablevolume.PreparedAttribute{Spec: spec, Raw: raw})
	}
	return result, ctx.Err()
}

// Content references use the existing durable-volume digest spelling.
func validatorPreparationDigest(raw []byte) string {
	hash := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(hash[:])
}

// Translate only complete configuration paths and public keys into a fresh
// preparation scope. Runtime later independently authenticates the given UID.
func validatorRuntimePreparationForConfig(cfg *ReleaseConfig, root string, uid uint16) (validatorRuntimePreparationScope, error) {
	scope := validatorRuntimePreparationScope{Schema: validatorRuntimePreparationSchema}
	pathsKVs := map[string]bool{}
	add := func(path string) error {
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("configured runtime path %s must be beneath the fresh runtime root", path)
		}
		for relative != "." {
			pathsKVs[relative] = true
			relative = filepath.Dir(relative)
		}
		return nil
	}
	for _, path := range append([]string{cfg.StateDir}, releaseConfiguredEvidenceDirectories(cfg)...) {
		if err := add(path); err != nil {
			return scope, err
		}
	}
	for _, operator := range cfg.Operators {
		if err := add(operator.StateDir); err != nil {
			return scope, err
		}
		seed, err := loadClientSeed(operator.ClientKeySeedFile)
		if err != nil {
			return scope, err
		}
		relative, _ := filepath.Rel(root, operator.StateDir)
		member := ReleaseActivationMemberV2{NoID: operator.NoID, ValidatorUID: uid}
		copy(member.Activation.VPK[:], ed25519PublicKey(seed))
		scope.Ledgers = append(scope.Ledgers, validatorRuntimePreparationLedger{Directory: relative,
			Ledger: AttemptLedgerPreparationScope{Identity: releaseActivationLedgerIdentity(cfg, member), Coordinator: strings.ToLower(cfg.Coordinator),
				Limits: cfg.EvidenceV2.Bounds.Disk, ExpectedHead: AttemptLedgerHead{Root: zeroAttemptHash()}}})
	}
	for path := range pathsKVs {
		scope.Directories = append(scope.Directories, path)
	}
	slices.Sort(scope.Directories)
	slices.SortFunc(scope.Ledgers, func(a, b validatorRuntimePreparationLedger) int { return strings.Compare(a.Directory, b.Directory) })
	return scope, scope.validate()
}

// Evidence inputs and both scratch directories must already exist at startup.
func releaseConfiguredEvidenceDirectories(cfg *ReleaseConfig) []string {
	paths := []string{}
	for _, operator := range cfg.EvidenceV2.Operators {
		paths = append(paths, operator.ReplayScratchRoot, operator.SealScratchRoot)
		for _, file := range operator.Files() {
			paths = append(paths, filepath.Dir(file.Path))
		}
	}
	return paths
}
