//go:build linux || darwin

// `provider fleet storage` prepares, on the operator's own machine, the private
// journal directories mainnet fleet writes open and the one owner-local
// declaration that names them. Each directory goes through the same offline plan
// and apply as `sn-mainnet storage-owner-prepare`: an exact pinned request, an
// exact pinned plan, its control journal and its physical checkpoints. request
// only writes those inputs from what the operator states; it enrolls nothing.
// No RPC, signing key, manifest or runtime owner is reachable from here.
package miner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/docopt/docopt-go"
	"golang.org/x/sys/unix"

	snchain "github.com/urfoundation/sn/chain"
	"github.com/urfoundation/sn/internal/durablepath"
	"github.com/urnetwork/connect/durablesys"
	"github.com/urnetwork/connect/durablevolume"
)

const (
	fleetStorageRequestSchema = "urnetwork-fleet-storage-request-v1"
	fleetStoragePlanSchema    = "urnetwork-fleet-storage-plan-v1"
	fleetStorageResultSchema  = "urnetwork-fleet-storage-result-v1"

	// The private metadata beside the journals: never inside one.
	fleetStorageCustodyDirectory = "fleet-custody"
	fleetStorageRequestFile      = "preparation-request.json"
	fleetStoragePlanFile         = "preparation-plan.json"
	fleetStorageDeclarationFile  = "durable-volumes.json"

	// Admission floors, room for the largest journals both formats can hold.
	fleetStorageMinAvailableBytes  = 128 * 1024 * 1024
	fleetStorageMinAvailableInodes = 16 * 1024

	// The former-writer fence's own bound on its evidence.
	fleetStorageMaxEvidenceBytes = 8192
	fleetStorageMaxFileBytes     = 1024 * 1024
)

// What preparing one journal directory may cost. These bound preparation only;
// each journal's capacity is its fixed format.
var fleetStorageLimits = durablevolume.PreparationLimits{MaxEntries: 16, MaxBytes: 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 4, MaxOwnerAttributeBytes: 16 * 1024, MaxPlanBytes: fleetStorageMaxFileBytes}

// One directory a fleet write opens, and the only owner that may prepare it:
// register opens both, and publish, bind and revoke the recovery journal alone.
type fleetStorageMember struct {
	kind   string
	root   string
	files  string
	inputs any
	pinned func(*fleetStorageCover) *durablevolume.Reference
}

func (self fleetStorageMember) fileNamed(name string) string {
	return filepath.Join(self.files, name)
}

func fleetStorageMembers(stateDir string) []fleetStorageMember {
	custody := filepath.Join(stateDir, fleetStorageCustodyDirectory)
	return []fleetStorageMember{
		{
			kind: "fleet-recovery", root: filepath.Join(stateDir, "fleet-mainnet-recovery"), files: filepath.Join(custody, "recovery"),
			inputs: minerStoragePreparationScope{Schema: "urnetwork-miner-snapshot-preparation-v1", Name: "journal.json", MaximumBytes: fleetRecoveryMaxBytes, MaximumRecords: fleetRecoveryMaxRecords, MaximumRawRecordBytes: fleetRecoveryMaxRaw, MaximumManifestBytes: 256 * 1024},
			pinned: func(cover *fleetStorageCover) *durablevolume.Reference { return &cover.Recovery },
		},
		{
			kind: snchain.NativeJournalPreparationKind, root: filepath.Join(stateDir, "fleet-native"), files: filepath.Join(custody, "native"),
			inputs: snchain.FreshNativeJournalPreparationScope(),
			pinned: func(cover *fleetStorageCover) *durablevolume.Reference { return &cover.Native },
		},
	}
}

// The one file an operator pins at each step. It names each directory's request
// or plan by exact digest, and each of those is still checked on its own.
type fleetStorageCover struct {
	Schema string `json:"schema"`
	// On a plan, the request it was made from.
	Request  *durablevolume.Reference `json:"request,omitempty"`
	Recovery durablevolume.Reference  `json:"recovery"`
	Native   durablevolume.Reference  `json:"native"`
}

// What apply returns: the flags every fleet write takes.
type fleetStorageResult struct {
	Schema      string                  `json:"schema"`
	Plan        durablevolume.Reference `json:"plan"`
	Declaration durablevolume.Reference `json:"declaration"`
	Roots       []string                `json:"roots"`
}

// The fixed fresh adapters `sn-mainnet storage-owner-prepare` dispatches to, with
// its owner-local scope. No restore adapter: retained or unknown history is never
// prepared here, and the shared planner refuses a restore request without one.
func fleetStorageAdapter() durablevolume.PreparationAdapter {
	return durablevolume.PreparationAdapter{
		Build: func(ctx context.Context, staging *os.File, name string, owner durablevolume.PreparationOwner) (durablevolume.PreparationOwnerPlan, error) {
			if owner.Kind == snchain.NativeJournalPreparationKind {
				return snchain.BuildFreshNativeJournalPreparation(ctx, staging, name, owner)
			}
			return BuildFreshStoragePreparation(ctx, staging, name, owner, true)
		},
		Inspect: func(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan) ([]durablevolume.PreparedAttribute, error) {
			if owner.Owner.Kind == snchain.NativeJournalPreparationKind {
				return snchain.InspectFreshNativeJournalPreparation(ctx, root, owner)
			}
			return InspectFreshStoragePreparation(ctx, root, owner, true)
		},
	}
}

// fleetStorage implements `provider fleet storage request|plan|apply`.
func fleetStorage(ctx context.Context, opts docopt.Opts, stdout, stderr io.Writer) error {
	stateDir, err := providerStateDir()
	if err != nil {
		return err
	}
	switch {
	case mustBoolOpt(opts, "request"):
		return fleetStorageRequest(opts, stateDir, stdout, stderr)
	case mustBoolOpt(opts, "plan"):
		return fleetStoragePlan(ctx, opts, stateDir, stdout, stderr)
	case mustBoolOpt(opts, "apply"):
		return fleetStorageApply(ctx, opts, stateDir, stdout, stderr)
	}
	return errors.New("unknown fleet storage command")
}

// A separator boundary keeps one sibling prefix from admitting another.
func fleetStorageBeneath(parent, path string) bool {
	return parent == "/" && filepath.IsAbs(path) || path == parent || strings.HasPrefix(path, parent+"/")
}

func fleetStorageDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// Reads exact bytes the operator pinned. The digest is what authenticates them;
// the shared preparation stage still applies its own protected read to each file
// it is handed.
func fleetStorageRead(reference durablevolume.Reference) ([]byte, error) {
	if !filepath.IsAbs(reference.Path) || filepath.Clean(reference.Path) != reference.Path {
		return nil, fmt.Errorf("%q: give the absolute path", reference.Path)
	}
	fd, err := unix.Open(reference.Path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: reference.Path, Err: err}
	}
	file := os.NewFile(uintptr(fd), reference.Path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || fleetStorageMaxFileBytes < info.Size() {
		return nil, fmt.Errorf("%s is not a regular file of at most %d bytes", reference.Path, fleetStorageMaxFileBytes)
	}
	raw, err := io.ReadAll(io.LimitReader(file, fleetStorageMaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if fleetStorageDigest(raw) != reference.Sha256 {
		return nil, fmt.Errorf("%s: bytes differ from the accepted sha256: digest", reference.Path)
	}
	return raw, nil
}

// Reads one pinned cover of the expected schema.
func fleetStorageReadCover(reference durablevolume.Reference, schema string) (*fleetStorageCover, error) {
	raw, err := fleetStorageRead(reference)
	if err != nil {
		return nil, err
	}
	var cover fleetStorageCover
	if err := fleetRecoveryDecode(raw, &cover); err != nil {
		return nil, fmt.Errorf("%s: %w", reference.Path, err)
	}
	if cover.Schema != schema {
		return nil, fmt.Errorf("%s is not a %s file", reference.Path, schema)
	}
	return &cover, nil
}

// A published name holds complete bytes and never changes. Repeating the same
// bytes is a lost acknowledgement, and anything else is refused.
func fleetStoragePublish(path string, raw []byte) error {
	published := func() (bool, error) {
		existing, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !bytes.Equal(existing, raw) {
			return false, fmt.Errorf("%s already exists with other contents", path)
		}
		return true, nil
	}
	if done, err := published(); done || err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".next-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(raw)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	err = durablesys.RenameNoReplace(int(directory.Fd()), filepath.Base(file.Name()), int(directory.Fd()), filepath.Base(path))
	runtime.KeepAlive(directory)
	if errors.Is(err, unix.EEXIST) {
		if done, err := published(); done || err != nil {
			return err
		}
	}
	if err != nil {
		return &os.PathError{Op: "publish", Path: path, Err: err}
	}
	return directory.Sync()
}

func fleetStorageReport(stdout io.Writer, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if n, err := stdout.Write(raw); err != nil || n != len(raw) {
		return fmt.Errorf("fleet storage report was not fully delivered: %w", errors.Join(io.ErrShortWrite, err))
	}
	return nil
}

// A directory is created private or must already be. An operator's mode is never
// changed from here, and an alias of another directory is never followed.
func fleetStoragePrivateDirectory(path string) error {
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return fmt.Errorf("%s must be a private directory: chmod 700, or use another URNETWORK_STATE_DIR", path)
	}
	return nil
}

// An existing journal directory may only be empty, so nothing is written for
// history that can't be prepared as new. The planner checks this again.
func fleetStorageFreshRoot(path string) error {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("%s already holds files, so it can't be prepared as new. A mainnet journal must continue where it is, and testnet records can stay: for those, use another URNETWORK_STATE_DIR", path)
	}
	return nil
}

// The request step writes what the operator states about this machine. Nothing
// here proves it: the plan checks the facts, and only the operator can know that
// no other machine holds this fleet's signing state.
func fleetStorageRequest(opts docopt.Opts, stateDir string, stdout, stderr io.Writer) error {
	if !mustBoolOpt(opts, "--former-writers-stopped") || !mustBoolOpt(opts, "--no-previous-owner-state") {
		return errors.New("fleet storage request needs both --former-writers-stopped and --no-previous-owner-state: only you can state them")
	}
	evidence := fleetOpt(opts, "--evidence")
	if evidence == "" || fleetStorageMaxEvidenceBytes < len(evidence) {
		return fmt.Errorf("--evidence must say what you checked, in at most %d bytes", fleetStorageMaxEvidenceBytes)
	}
	mount := fleetOpt(opts, "--mount_path")
	if !filepath.IsAbs(mount) || filepath.Clean(mount) != mount {
		return fmt.Errorf("--mount_path %q must be the absolute mount point", mount)
	}
	if !fleetStorageBeneath(mount, stateDir) {
		return fmt.Errorf("the state directory %s is not below --mount_path %s. Run this with URNETWORK_STATE_DIR set to its physical path below that mount, and use the same value on every fleet write. On macOS the data volume is /System/Volumes/Data, so that is /System/Volumes/Data%s", stateDir, mount, stateDir)
	}
	if info, err := os.Lstat(stateDir); err != nil || !info.IsDir() {
		return errors.Join(fmt.Errorf("the state directory %s must already exist as a real directory: mkdir -m 700 it, or set URNETWORK_STATE_DIR", stateDir), err)
	}
	custody := filepath.Join(stateDir, fleetStorageCustodyDirectory)
	coverPath := filepath.Join(custody, fleetStorageRequestFile)
	if raw, err := os.ReadFile(coverPath); err == nil {
		return fmt.Errorf("a request already exists. To use it: provider fleet storage plan --request=%s --request-sha256=%s. To start over, before any apply has run, remove %s", cliShellQuote(coverPath), fleetStorageDigest(raw), cliShellQuote(custody))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	members := fleetStorageMembers(stateDir)
	for _, member := range members {
		if err := fleetStorageFreshRoot(member.root); err != nil {
			return err
		}
	}
	if err := fleetStoragePrivateDirectory(custody); err != nil {
		return err
	}
	cover := fleetStorageCover{Schema: fleetStorageRequestSchema}
	for _, member := range members {
		for _, path := range []string{member.root, member.files, member.fileNamed("staging")} {
			if err := fleetStoragePrivateDirectory(path); err != nil {
				return err
			}
		}
		var root unix.Stat_t
		if err := unix.Lstat(member.root, &root); err != nil {
			return &os.PathError{Op: "lstat", Path: member.root, Err: err}
		}
		fence, err := json.MarshalIndent(durablevolume.PreparationFence{Schema: durablevolume.PreparationFenceSchema, RootPath: member.root, RootInode: root.Ino, Purpose: "fresh", FormerWritersStopped: true, NoPreviousOwnerState: true, Evidence: evidence}, "", "  ")
		if err != nil {
			return err
		}
		fence = append(fence, '\n')
		fencePath := member.fileNamed("former-writer.json")
		inputs, err := json.Marshal(member.inputs)
		if err != nil {
			return err
		}
		raw, err := json.MarshalIndent(durablevolume.PreparationRequest{
			Schema: durablevolume.PreparationRequestSchema, Purpose: "fresh", Scope: "owner-local",
			MountPath: mount, FilesystemUuid: fleetOpt(opts, "--filesystem_uuid"), FilesystemType: fleetOpt(opts, "--filesystem_type"),
			MinAvailableBytes: fleetStorageMinAvailableBytes, MinAvailableInodes: fleetStorageMinAvailableInodes,
			RootPath: member.root, MarkerPath: member.fileNamed("identity"), LeasePath: member.fileNamed("root.lease"),
			DeclarationPath: member.fileNamed(fleetStorageDeclarationFile), ControlPath: member.fileNamed("preparation.jsonl"), StagingDirectory: member.fileNamed("staging"),
			FormerWriterFence: durablevolume.Reference{Path: fencePath, Sha256: fleetStorageDigest(fence)},
			Limits:            fleetStorageLimits,
			Owners:            []durablevolume.PreparationOwner{{Kind: member.kind, RelativePath: ".", Purpose: "fresh", Inputs: inputs}},
		}, "", "  ")
		if err != nil {
			return err
		}
		raw = append(raw, '\n')
		requestPath := member.fileNamed(fleetStorageRequestFile)
		for _, file := range []struct {
			path string
			raw  []byte
		}{{fencePath, fence}, {requestPath, raw}} {
			if err := fleetStoragePublish(file.path, file.raw); err != nil {
				return fmt.Errorf("%w. To start over, before any apply has run, remove %s", err, cliShellQuote(custody))
			}
		}
		*member.pinned(&cover) = durablevolume.Reference{Path: requestPath, Sha256: fleetStorageDigest(raw)}
	}
	raw, err := json.MarshalIndent(cover, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := fleetStoragePublish(coverPath, raw); err != nil {
		return err
	}
	reference := durablevolume.Reference{Path: coverPath, Sha256: fleetStorageDigest(raw)}
	if err := fleetStorageReport(stdout, reference); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "Wrote a request for %s and %s. Read it, then:\n  provider fleet storage plan --request=%s --request-sha256=%s\n", members[0].root, members[1].root, cliShellQuote(reference.Path), reference.Sha256)
	return nil
}

// A fleet write opens one fixed path per format, so a request for anything else
// could never admit it. The state directory must be the one the request names.
func fleetStorageCheckMember(member fleetStorageMember, requestBytes []byte) error {
	var request durablevolume.PreparationRequest
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		return err
	}
	if request.RootPath != member.root {
		return fmt.Errorf("this prepares %s, but fleet writes here open %s: use the URNETWORK_STATE_DIR the request was made with", request.RootPath, member.root)
	}
	if len(request.Owners) != 1 || request.Owners[0].Kind != member.kind {
		return fmt.Errorf("%s holds only a %s journal, as its one owner", member.root, member.kind)
	}
	return nil
}

// The plan step reports nothing until every directory has its plan. Each plan is
// retained as exact bytes under its own name and never replaced, so repeating
// this after a lost reply returns the same one.
func fleetStoragePlan(ctx context.Context, opts docopt.Opts, stateDir string, stdout, stderr io.Writer) error {
	source := durablevolume.Reference{Path: fleetOpt(opts, "--request"), Sha256: fleetOpt(opts, "--request-sha256")}
	requests, err := fleetStorageReadCover(source, fleetStorageRequestSchema)
	if err != nil {
		return err
	}
	members := fleetStorageMembers(stateDir)
	cover := fleetStorageCover{Schema: fleetStoragePlanSchema, Request: &source}
	for _, member := range members {
		reference := *member.pinned(requests)
		requestBytes, err := fleetStorageRead(reference)
		if err != nil {
			return err
		}
		if err := fleetStorageCheckMember(member, requestBytes); err != nil {
			return err
		}
		path := member.fileNamed(fleetStoragePlanFile)
		raw, err := os.ReadFile(path)
		if err == nil {
			var retained durablevolume.PreparationPlan
			if err := json.Unmarshal(raw, &retained); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if retained.Request != reference {
				return fmt.Errorf("%s was planned from another request. Apply that one or, before any apply has run, remove %s and start over", path, cliShellQuote(filepath.Join(stateDir, fleetStorageCustodyDirectory)))
			}
		} else if errors.Is(err, os.ErrNotExist) {
			plan, err := durablepath.PlanOwnerLocalPreparation(ctx, reference, fleetStorageAdapter())
			if err != nil {
				return fmt.Errorf("fleet storage plan for %s: %w", member.root, err)
			}
			if raw, err = json.Marshal(plan); err != nil {
				return err
			}
			raw = append(raw, '\n')
			if err := fleetStoragePublish(path, raw); err != nil {
				return err
			}
		} else {
			return err
		}
		*member.pinned(&cover) = durablevolume.Reference{Path: path, Sha256: fleetStorageDigest(raw)}
	}
	raw, err := json.MarshalIndent(cover, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	path := filepath.Join(stateDir, fleetStorageCustodyDirectory, fleetStoragePlanFile)
	if err := fleetStoragePublish(path, raw); err != nil {
		return fmt.Errorf("%w. Apply the plan already there or, before any apply has run, remove %s and start over", err, cliShellQuote(filepath.Dir(path)))
	}
	reference := durablevolume.Reference{Path: path, Sha256: fleetStorageDigest(raw)}
	if err := fleetStorageReport(stdout, reference); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "Planned without changing the journal directories. Read %s, then:\n  provider fleet storage apply --plan=%s --plan-sha256=%s\n", cliShellQuote(filepath.Dir(path)), cliShellQuote(reference.Path), reference.Sha256)
	return nil
}

// The apply step runs each directory's accepted plan through its own journal, and
// exposes a usable declaration only once every directory is complete. An
// interrupted run continues from the same plan with the same command.
func fleetStorageApply(ctx context.Context, opts docopt.Opts, stateDir string, stdout, stderr io.Writer) error {
	source := durablevolume.Reference{Path: fleetOpt(opts, "--plan"), Sha256: fleetOpt(opts, "--plan-sha256")}
	plans, err := fleetStorageReadCover(source, fleetStoragePlanSchema)
	if err != nil {
		return err
	}
	members := fleetStorageMembers(stateDir)
	var declarations []durablevolume.Reference
	for _, member := range members {
		reference := *member.pinned(plans)
		raw, err := fleetStorageRead(reference)
		if err != nil {
			return err
		}
		var plan durablevolume.PreparationPlan
		if err := json.Unmarshal(raw, &plan); err != nil {
			return fmt.Errorf("%s: %w", reference.Path, err)
		}
		if err := fleetStorageCheckMember(member, plan.RequestBytes); err != nil {
			return err
		}
		result, err := durablepath.ApplyOwnerLocalPreparation(ctx, reference, fleetStorageAdapter())
		if err != nil {
			return fmt.Errorf("fleet storage apply for %s: %w", member.root, err)
		}
		declarations = append(declarations, result.Declaration)
	}
	declaration, err := fleetStorageCombine(filepath.Join(stateDir, fleetStorageCustodyDirectory, fleetStorageDeclarationFile), declarations)
	if err != nil {
		return err
	}
	// The same physical admission a fleet write starts with, read-only, before
	// the operator is told this declaration is ready.
	check := durablevolume.WithReference(ctx, declaration)
	result := fleetStorageResult{Schema: fleetStorageResultSchema, Plan: source, Declaration: declaration}
	for _, member := range members {
		owner, err := durablepath.OpenOwnerLocalVolume(check, member.root, durablevolume.ReadOnly)
		if err != nil {
			return fmt.Errorf("the declaration does not admit %s: %w", member.root, err)
		}
		if err := owner.Close(); err != nil {
			return err
		}
		result.Roots = append(result.Roots, member.root)
	}
	if err := fleetStorageReport(stdout, result); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "Prepared. On every fleet register, publish, bind and revoke, pass:\n  --durable-volumes=%s --durable-volumes-sha256=%s\nand keep URNETWORK_STATE_DIR=%s. Keep everything in %s, and never restore these directories from a backup.\n", cliShellQuote(declaration.Path), declaration.Sha256, cliShellQuote(stateDir), cliShellQuote(filepath.Dir(declaration.Path)))
	return nil
}

// A write takes one declaration, and the shared preparation produces one for each
// root it prepares, so this lists every root of one volume in a single document:
// the shape connect's daemon cohorts publish, with the first marker as the volume's.
// It only names what each apply already proved. Every runtime open checks all of
// it again, and the owner-local loader refuses a document that doesn't validate.
func fleetStorageCombine(path string, declarations []durablevolume.Reference) (durablevolume.Reference, error) {
	combined := durablevolume.VolumeSpec{}
	for index, reference := range declarations {
		config, err := durablevolume.LoadOwnerLocal(reference)
		if err != nil {
			return durablevolume.Reference{}, err
		}
		if len(config.Volumes) != 1 || len(config.Volumes[0].StateRoots) != 1 {
			return durablevolume.Reference{}, fmt.Errorf("%s must name exactly its one prepared root", reference.Path)
		}
		volume := config.Volumes[0]
		if index == 0 {
			combined = volume
			continue
		}
		if volume.MountPath != combined.MountPath || volume.FilesystemUuid != combined.FilesystemUuid || volume.FilesystemType != combined.FilesystemType {
			return durablevolume.Reference{}, fmt.Errorf("%s names another filesystem than %s", reference.Path, declarations[0].Path)
		}
		combined.MinAvailableBytes = max(combined.MinAvailableBytes, volume.MinAvailableBytes)
		combined.MinAvailableInodes = max(combined.MinAvailableInodes, volume.MinAvailableInodes)
		combined.StateRoots = append(combined.StateRoots, volume.StateRoots...)
	}
	sort.Slice(combined.StateRoots, func(i, j int) bool { return combined.StateRoots[i].Path < combined.StateRoots[j].Path })
	raw, err := json.MarshalIndent(durablevolume.Config{Schema: durablevolume.OwnerLocalSchema, Volumes: []durablevolume.VolumeSpec{combined}}, "", "  ")
	if err != nil {
		return durablevolume.Reference{}, err
	}
	raw = append(raw, '\n')
	if err := fleetStoragePublish(path, raw); err != nil {
		return durablevolume.Reference{}, err
	}
	reference := durablevolume.Reference{Path: path, Sha256: fleetStorageDigest(raw)}
	if _, err := durablevolume.LoadOwnerLocal(reference); err != nil {
		return durablevolume.Reference{}, err
	}
	return reference, nil
}
