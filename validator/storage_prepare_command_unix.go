//go:build linux || darwin

// The CLI creates a public fresh-storage request, then uses the existing
// explicit plan/apply engine. It never registers, activates or starts a service.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/urfoundation/sn/internal/durablepath"
	"github.com/urnetwork/connect/durablevolume"
)

const validatorStoragePrepareUsage = `Usage:
  validator storage-prepare request --config=FILE --root-dir=DIR --validator-uid=UID
    --mount-path=DIR --filesystem-uuid=UUID --filesystem-type=TYPE --fresh --out=FILE
    [--min-available-bytes=N] [--min-available-inodes=N]
  validator storage-prepare plan --request=FILE --request-sha256=HASH
  validator storage-prepare apply --plan=FILE --plan-sha256=HASH

request reads an authenticated initial configuration and the runner's own
measurement public key. All state and evidence directories must be beneath
the empty root-dir; keys and configuration must be outside it. validator-uid
is the UID of the hotkey on subnet25; activation verifies it independently.
fresh asserts that this root has no previous writer or retained validator state.
The storage engine checks the declared mount, UUID and filesystem against the
actual host. Supported daemon filesystems: Linux ext4/xfs/btrfs, or macOS APFS.
Reserve defaults:100GiB and100000inodes. Flags select reserve floors, not quotas.
plan stages original empty ledgers and returns the public accepted-plan input.
apply takes that exact plan and publishes the prepared daemon declaration.
`

// Return the usual CLI exit status while preserving cancellation and diagnostics.
func runValidatorStoragePrepare(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, validatorStoragePrepareUsage)
		return 0
	}
	var result any
	var err error
	switch args[0] {
	case "request":
		result, err = createValidatorStorageRequest(ctx, args[1:], stderr)
	case "plan", "apply":
		flags := flag.NewFlagSet("validator storage-prepare "+args[0], flag.ContinueOnError)
		flags.SetOutput(stderr)
		name := "request"
		if args[0] == "apply" {
			name = "plan"
		}
		path := flags.String(name, "", "explicit preparation input file")
		digest := flags.String(name+"-sha256", "", "sha256 of that complete input file")
		if err = flags.Parse(args[1:]); err == nil && (flags.NArg() != 0 || *path == "" || *digest == "") {
			err = errors.New("storage preparation requires its explicit file and complete sha256")
		}
		if err == nil {
			reference := durablevolume.Reference{Path: *path, Sha256: *digest}
			if args[0] == "plan" {
				result, err = durablepath.PlanPreparation(ctx, reference, validatorRuntimePreparationAdapter())
			} else {
				result, err = durablepath.ApplyPreparation(ctx, reference, validatorRuntimePreparationAdapter())
			}
		}
	default:
		err = errors.New("unknown storage preparation command; use request, plan or apply")
	}
	if err != nil {
		fmt.Fprintln(stderr, "validator storage-prepare:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "validator storage-prepare output:", err)
		return 1
	}
	return 0
}

// Observe the explicit fresh root and generate only public preparation inputs.
func createValidatorStorageRequest(ctx context.Context, args []string, stderr io.Writer) (durablevolume.Reference, error) {
	var none durablevolume.Reference
	flags := flag.NewFlagSet("validator storage-prepare request", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "complete initial configuration")
	root := flags.String("root-dir", "", "existing empty private parent of all runtime directories")
	uidText := flags.String("validator-uid", "", "registered native hotkey UID, including zero")
	mount := flags.String("mount-path", "", "actual persistent filesystem mount path")
	uuid := flags.String("filesystem-uuid", "", "actual persistent filesystem UUID")
	filesystem := flags.String("filesystem-type", "", "ext4, xfs, btrfs or apfs")
	fresh := flags.Bool("fresh", false, "assert that the root has no previous writer or retained state")
	out := flags.String("out", "", "new request file outside runtime state")
	minimumBytes := flags.Uint64("min-available-bytes", 100*1024*1024*1024, "minimum available bytes on the filesystem")
	minimumInodes := flags.Uint64("min-available-inodes", 100000, "minimum available filesystem inodes")
	if err := flags.Parse(args); err != nil {
		return none, err
	}
	if flags.NArg() != 0 || !*fresh || *configPath == "" || *root == "" || *uidText == "" || *mount == "" || *uuid == "" || *filesystem == "" || *out == "" || *minimumBytes == 0 || *minimumInodes == 0 {
		return none, errors.New("request requires config, root-dir, validator-uid, mount-path, filesystem-uuid, filesystem-type, fresh and out")
	}
	if *root == "/" {
		return none, errors.New("runtime root cannot be the filesystem root")
	}
	uid, err := strconv.ParseUint(*uidText, 10, 16)
	if err != nil {
		return none, errors.New("validator-uid must be an unsigned16-bit native UID")
	}
	if ctx == nil {
		return none, errors.New("storage preparation context is absent")
	}
	if err := ctx.Err(); err != nil {
		return none, err
	}
	for _, path := range []string{*root, *out, *configPath, *mount} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" && path != *mount {
			return none, errors.New("storage request paths must be canonical and absolute")
		}
	}
	cfg, err := LoadReleaseProductionConfigPreActivation(*configPath)
	if err != nil {
		return none, err
	}
	for _, path := range []string{*configPath, *out, cfg.HotkeySeedFile} {
		if validatorPreparationPathWithin(*root, path) {
			return none, errors.New("keys, configuration and preparation metadata must be outside the empty runtime root")
		}
	}
	for _, operator := range cfg.Operators {
		if validatorPreparationPathWithin(*root, operator.ClientKeySeedFile) {
			return none, errors.New("measurement keys must be provisioned outside the empty runtime root")
		}
	}
	scope, err := validatorRuntimePreparationForConfig(cfg, *root, uint16(uid))
	if err != nil {
		return none, err
	}
	if err := ValidateReleaseEvidenceV2Path(*root); err != nil {
		return none, err
	}
	directory, err := os.OpenFile(*root, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return none, err
	}
	defer directory.Close()
	if err := attemptPreparationPrivate(directory, true); err != nil {
		return none, err
	}
	names, err := directory.Readdirnames(1)
	if len(names) != 0 || err != io.EOF {
		return none, errors.Join(errors.New("fresh validator runtime root must be empty"), err)
	}
	stat, err := directory.Stat()
	if err != nil {
		return none, err
	}
	physical, ok := stat.Sys().(*syscall.Stat_t)
	if !ok {
		return none, errors.New("cannot observe fresh runtime root inode")
	}
	metadata := filepath.Dir(*out)
	fencePath := *out + ".fresh.json"
	fence := durablevolume.PreparationFence{Schema: "urnetwork-storage-preparation-fence-v1", RootPath: *root, RootInode: physical.Ino,
		Purpose: "fresh", FormerWritersStopped: true, NoPreviousOwnerState: true,
		Evidence: "runner explicitly supplied --fresh for this observed empty private runtime root"}
	fenceRaw, err := json.Marshal(fence)
	if err != nil {
		return none, err
	}
	input, err := json.Marshal(scope)
	if err != nil {
		return none, err
	}
	request := durablevolume.PreparationRequest{Schema: "urnetwork-storage-preparation-request-v1", Purpose: "fresh", Scope: "daemon",
		MountPath: *mount, FilesystemUuid: *uuid, FilesystemType: *filesystem, MinAvailableBytes: *minimumBytes, MinAvailableInodes: *minimumInodes,
		RootPath: *root, MarkerPath: *out + ".marker", LeasePath: *out + ".lease", DeclarationPath: *out + ".volumes.json",
		ControlPath: *out + ".journal", StagingDirectory: filepath.Join(metadata, filepath.Base(*out)+".staging"),
		FormerWriterFence: durablevolume.Reference{Path: fencePath, Sha256: validatorPreparationDigest(fenceRaw)},
		Limits: durablevolume.PreparationLimits{MaxEntries: 1000, MaxBytes: 256 * 1024 * 1024, MaxDepth: 16, MaxOwnerAttributes: 128,
			MaxOwnerAttributeBytes: 128 * 4096, MaxPlanBytes: 4 * 1024 * 1024},
		Owners: []durablevolume.PreparationOwner{{Kind: validatorRuntimePreparationKind, RelativePath: ".", Purpose: "fresh", Inputs: input}}}
	raw, err := json.Marshal(request)
	if err != nil {
		return none, err
	}
	if err := os.Mkdir(request.StagingDirectory, 0o700); err != nil {
		return none, err
	}
	// Neither input is replaced on retry. Retain the exact original files once
	// any plan has been created or applied, including after lost output.
	for _, file := range []struct {
		path string
		raw  []byte
	}{{path: fencePath, raw: fenceRaw}, {path: *out, raw: raw}} {
		output, err := os.OpenFile(file.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return none, err
		}
		n, writeErr := output.Write(file.raw)
		if n != len(file.raw) && writeErr == nil {
			writeErr = io.ErrShortWrite
		}
		if err := errors.Join(writeErr, output.Sync(), output.Close()); err != nil {
			return none, err
		}
	}
	if err := ctx.Err(); err != nil {
		return none, err
	}
	return durablevolume.Reference{Path: *out, Sha256: validatorPreparationDigest(raw)}, nil
}

// Segment-aware containment keeps sibling names distinct.
func validatorPreparationPathWithin(parent, path string) bool {
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
