// The complete protected WARP tree closes literal, all/env/host and versioned
// resolver fallbacks. Reads never populate a vault, enroll a root or reset quota.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

// Only public identities and content digests enter this local observation.
type repairOperatorResourceEntry struct {
	Path   string `json:"path"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Mode   uint32 `json:"mode"`
	Uid    uint32 `json:"uid"`
	Gid    uint32 `json:"gid"`
	Size   int64  `json:"size"`
	Sha256 string `json:"sha256,omitempty"`
}

var repairOperatorInterpolation = regexp.MustCompile(`\{\{\s*env:([^}\s]*)\s*\}\}`)

// Open descriptors and the named entry must still identify the same original
// regular file; disappearance is different from a returned byte contradiction.
func readRepairOperatorFile(ctx context.Context, host *repairValidatorHost, path string, uid uint32, maximum int64) (raw []byte, info os.FileInfo, resultErr error) {
	if ctx == nil || host == nil {
		return nil, nil, errors.New("operator retained resource owner is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := host.parents(path, uid); err != nil {
		return nil, nil, err
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, repairValidatorObservationError("cannot open retained operator resource", err, true)
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() {
		closeErr := repairValidatorObservation(ctx, "operator-resource-close", file.Close())
		resultErr = errors.Join(resultErr, repairValidatorObservationError("cannot close retained operator resource", closeErr, false))
	}()
	info, err = file.Stat()
	observationErr := repairValidatorObservationError("cannot inspect retained operator resource", repairValidatorObservation(ctx, "operator-resource-open-stat", err), false)
	if err != nil {
		return nil, nil, observationErr
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uid || stat.Nlink != 1 || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 || info.Size() < 0 || info.Size() > maximum {
		return nil, nil, errors.Join(errRpcIntegrity, errors.New("operator retained resource is not protected original file custody"), observationErr)
	}
	if observationErr != nil {
		return nil, nil, observationErr
	}
	raw = make([]byte, 0, min(info.Size(), 64*1024))
	buffer := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		count, readErr := file.Read(buffer)
		if int64(len(raw))+int64(count) > maximum {
			return nil, nil, errors.Join(errRpcIntegrity, errors.New("operator retained resource exceeds its byte bound"))
		}
		raw = append(raw, buffer[:count]...)
		end := errors.Is(readErr, io.EOF)
		if end {
			readErr = nil
		}
		if err := repairValidatorObservation(ctx, "operator-resource-read", readErr); err != nil {
			return nil, nil, repairValidatorObservationError("cannot read retained operator resource", err, false)
		}
		if end {
			break
		}
	}
	after, statErr := file.Stat()
	named, namedErr := os.Lstat(path)
	if err := errors.Join(statErr, namedErr, ctx.Err()); err != nil {
		return nil, nil, repairValidatorObservationError("cannot observe retained operator resource", err, true)
	}
	afterState, afterOk := after.Sys().(*syscall.Stat_t)
	namedState, namedOk := named.Sys().(*syscall.Stat_t)
	if !afterOk || !namedOk || !os.SameFile(info, after) || !os.SameFile(info, named) || stat.Uid != afterState.Uid || stat.Gid != afterState.Gid || stat.Nlink != afterState.Nlink || stat.Uid != namedState.Uid || stat.Gid != namedState.Gid || stat.Nlink != namedState.Nlink || stat.Ctim != afterState.Ctim || info.Mode() != after.Mode() || info.Mode() != named.Mode() || info.Size() != after.Size() || info.Size() != int64(len(raw)) || !info.ModTime().Equal(after.ModTime()) {
		return nil, nil, errors.Join(errRpcIntegrity, errors.New("operator retained resource changed during observation"))
	}
	return raw, info, nil
}

// Root's ability to parse a vault is not evidence that the service can consume
// it. Require selected resource and directory access under the signed uid/gid;
// undeclared ACLs cannot override the checked ordinary permission class.
func inspectRepairOperatorResourceAccess(ctx context.Context, host *repairValidatorHost, plan repairOperatorHostPlan, path string) error {
	for selected, first := path, true; ; selected, first = filepath.Dir(selected), false {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Lstat(selected)
		if err != nil {
			return repairValidatorObservationError("cannot observe operator service resource access", err, true)
		}
		state, ok := info.Sys().(*syscall.Stat_t)
		if !ok || info.Mode()&os.ModeSymlink != 0 || !first && !info.IsDir() {
			return errors.Join(errRpcIntegrity, errors.New("operator selected resource access leaves original file custody"))
		}
		bits := uint32(info.Mode().Perm())
		if state.Uid == plan.Uid {
			bits >>= 6
		} else if state.Gid == plan.Gid {
			bits >>= 3
		}
		required := uint32(1)
		if first {
			required = 4
		} else {
			for _, tree := range plan.Resources {
				if rootPassiveHostPathContains(tree.Path, selected) {
					required = 5
				}
			}
		}
		if bits&required != required {
			return errors.Join(errRpcIntegrity, errors.New("operator taskworker credentials cannot consume an original selected resource"))
		}
		_, aclErr := unix.Getxattr(selected, "system.posix_acl_access", nil)
		if aclErr == nil {
			return errors.Join(errRpcIntegrity, errors.New("operator selected source has an undeclared access ACL"))
		}
		if !errors.Is(aclErr, unix.ENODATA) && !errors.Is(aclErr, unix.ENOTSUP) {
			return repairValidatorObservationError("cannot observe operator selected resource access policy", aclErr, false)
		}
		if selected == host.trustRoot || selected == filepath.Dir(selected) {
			return nil
		}
	}
}

// Root's successful path lookup cannot hide a service-credential refusal at an
// earlier literal or version candidate. The production resolver supplies each
// exact operation and retains its own precedence and error-handling rules.
func inspectRepairOperatorLookupAccess(ctx context.Context, host *repairValidatorHost, plan repairOperatorHostPlan, operation, path string) error {
	if ctx == nil || host == nil {
		return errors.New("operator resource lookup owner is absent")
	}
	if operation != "stat" && operation != "read-dir" || !repairValidatorPath(path) {
		return errors.Join(errRpcIntegrity, errors.New("operator resource lookup operation differs from its original profile"))
	}
	directory := filepath.Dir(path)
	if operation == "read-dir" {
		directory = path
	}
	parents := []string{}
	for selected := directory; ; selected = filepath.Dir(selected) {
		parents = append(parents, selected)
		if selected == host.trustRoot || selected == filepath.Dir(selected) {
			break
		}
	}
	slices.Reverse(parents)
	for _, selected := range parents {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Lstat(selected)
		if errors.Is(err, os.ErrNotExist) {
			// A genuinely absent ancestor belongs to the resolver's ordinary
			// fallback logic after all earlier search permissions were checked.
			return ctx.Err()
		}
		if err != nil {
			return repairValidatorObservationError("cannot observe operator resource lookup access", err, true)
		}
		state, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.Join(errRpcIntegrity, errors.New("operator resource lookup leaves original directory custody"))
		}
		bits := uint32(info.Mode().Perm())
		if state.Uid == plan.Uid {
			bits >>= 6
		} else if state.Gid == plan.Gid {
			bits >>= 3
		}
		required := uint32(1)
		if operation == "read-dir" && selected == directory {
			required = 5
		}
		if bits&required != required {
			return errors.Join(errRpcIntegrity, errors.New("operator taskworker credentials cannot traverse an original resource lookup"))
		}
		_, aclErr := unix.Getxattr(selected, "system.posix_acl_access", nil)
		if aclErr == nil {
			return errors.Join(errRpcIntegrity, errors.New("operator resource lookup has an undeclared access ACL"))
		}
		if !errors.Is(aclErr, unix.ENODATA) && !errors.Is(aclErr, unix.ENOTSUP) {
			return repairValidatorObservationError("cannot observe operator resource lookup access policy", aclErr, false)
		}
	}
	return ctx.Err()
}

// Every environment interpolation has a signed input. Even an unselected
// settings fallback cannot introduce an unreviewed environment override.
func validateRepairOperatorResource(raw []byte, name string, plan repairOperatorHostPlan) error {
	for _, match := range repairOperatorInterpolation.FindAllSubmatch(raw, -1) {
		if plan.env(string(match[1])) == "" {
			return errors.Join(errRpcIntegrity, errors.New("operator resource references an undeclared environment input"))
		}
	}
	if filepath.Base(name) != "settings.yml" {
		return nil
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&document); err != nil {
		return errors.Join(errRpcIntegrity, errors.New("operator settings resource is invalid"))
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.Join(errRpcIntegrity, errors.New("operator settings has multiple documents"))
	}
	var inspect func(*yaml.Node, int) error
	inspect = func(node *yaml.Node, depth int) error {
		if depth > 64 || node.Kind == yaml.AliasNode {
			return errors.New("operator settings aliases or depth are not admitted")
		}
		if node.Kind == yaml.MappingNode {
			keys := map[string]bool{}
			for index := 0; index < len(node.Content); index += 2 {
				key, value := node.Content[index], node.Content[index+1]
				if key.Kind != yaml.ScalarNode || keys[key.Value] || key.Value == "<<" {
					return errors.New("operator settings keys are ambiguous")
				}
				keys[key.Value] = true
				if key.Value == "env_vars" {
					if value.Kind != yaml.MappingNode {
						return errors.New("operator settings environment is not a literal map")
					}
					for offset := 0; offset < len(value.Content); offset += 2 {
						name, selected := value.Content[offset], value.Content[offset+1]
						if name.Kind != yaml.ScalarNode || selected.Kind != yaml.ScalarNode || selected.Tag != "!!str" || plan.env(name.Value) == "" || plan.env(name.Value) != selected.Value {
							return errors.New("operator settings would change the original signed environment")
						}
					}
				}
			}
		}
		for _, child := range node.Content {
			if err := inspect(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := inspect(&document, 0); err != nil {
		return errors.Join(errRpcIntegrity, err)
	}
	return nil
}

// A recursive descriptor census rejects symlinks, hardlinks, special files,
// mutable owners and incomplete reads. The caller compares the signed digest.
func inspectRepairOperatorTree(ctx context.Context, host *repairValidatorHost, tree repairOperatorTree, plan repairOperatorHostPlan) (string, error) {
	if err := host.parents(filepath.Join(tree.Path, "owned"), host.rootUid); err != nil {
		return "", err
	}
	entries := []repairOperatorResourceEntry{}
	var total uint64
	var walk func(string, int) error
	walk = func(path string, depth int) (resultErr error) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 64 || len(entries) >= int(tree.MaximumFiles) {
			return errors.Join(errRpcIntegrity, errors.New("operator complete resource census exceeds its count or depth bound"))
		}
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
		if err != nil {
			return repairValidatorObservationError("cannot open operator resource directory", err, true)
		}
		directory := os.NewFile(uintptr(fd), path)
		defer func() {
			closeErr := repairValidatorObservation(ctx, "operator-resource-directory-close", directory.Close())
			resultErr = errors.Join(resultErr, repairValidatorObservationError("cannot close original operator resource directory", closeErr, false))
		}()
		before, err := directory.Stat()
		observationErr := repairValidatorObservationError("cannot inspect original operator resource directory", repairValidatorObservation(ctx, "operator-resource-directory-stat", err), false)
		if err != nil {
			return observationErr
		}
		stat, ok := before.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != host.rootUid || !before.IsDir() || before.Mode().Perm()&0022 != 0 {
			return errors.Join(errRpcIntegrity, errors.New("operator resource directory is not protected"), observationErr)
		}
		if observationErr != nil {
			return observationErr
		}
		relative, err := filepath.Rel(tree.Path, path)
		if err != nil {
			return err
		}
		entries = append(entries, repairOperatorResourceEntry{Path: relative, Device: uint64(stat.Dev), Inode: stat.Ino, Mode: stat.Mode, Uid: stat.Uid, Gid: stat.Gid})
		children, err := directory.ReadDir(int(tree.MaximumFiles) + 1)
		if err != nil && !errors.Is(err, io.EOF) {
			return repairValidatorObservationError("cannot list complete operator resources", err, false)
		}
		if len(children)+len(entries) > int(tree.MaximumFiles) {
			return errors.Join(errRpcIntegrity, errors.New("operator resource directory exceeds complete census capacity"))
		}
		slices.SortFunc(children, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
		for _, child := range children {
			name := filepath.Join(path, child.Name())
			if !repairValidatorPath(name) {
				return errors.Join(errRpcIntegrity, errors.New("operator resource path is not canonical"))
			}
			if child.IsDir() {
				if err := walk(name, depth+1); err != nil {
					return err
				}
				continue
			}
			if child.Type() != 0 || len(entries) >= int(tree.MaximumFiles) {
				return errors.Join(errRpcIntegrity, errors.New("operator resource entry is not a bounded regular file"))
			}
			raw, info, err := readRepairOperatorFile(ctx, host, name, host.rootUid, int64(tree.MaximumBytes-total))
			if err != nil {
				return err
			}
			total += uint64(len(raw))
			if err := validateRepairOperatorResource(raw, name, plan); err != nil {
				return err
			}
			state := info.Sys().(*syscall.Stat_t)
			relative, _ := filepath.Rel(tree.Path, name)
			entries = append(entries, repairOperatorResourceEntry{Path: relative, Device: uint64(state.Dev), Inode: state.Ino, Mode: state.Mode, Uid: state.Uid, Gid: state.Gid, Size: info.Size(), Sha256: monitorReadDigest(raw)})
		}
		after, statErr := directory.Stat()
		named, namedErr := os.Lstat(path)
		if err := errors.Join(statErr, namedErr, ctx.Err()); err != nil {
			return repairValidatorObservationError("cannot observe original operator resource directory", err, true)
		}
		if !os.SameFile(before, after) || !os.SameFile(before, named) || before.Mode() != after.Mode() || before.Mode() != named.Mode() || !before.ModTime().Equal(after.ModTime()) {
			return errors.Join(errRpcIntegrity, errors.New("operator resource directory changed during complete census"))
		}
		return nil
	}
	if err := walk(tree.Path, 0); err != nil {
		return "", err
	}
	return rootObjectHash(entries), nil
}

// This rechecks enrolled roots without acquiring another mutable quota owner.
func inspectRepairOperatorRoots(ctx context.Context, host *repairValidatorHost, plan repairOperatorHostPlan) error {
	for _, root := range plan.WritableRoots {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := host.parents(filepath.Join(root.Path, "owned"), plan.Uid); err != nil {
			return err
		}
		info, err := os.Lstat(root.Path)
		if err != nil {
			return repairValidatorObservationError("cannot observe original operator writable root", err, true)
		}
		state, ok := info.Sys().(*syscall.Stat_t)
		if !ok || uint64(state.Dev) != root.Device || state.Ino != root.Inode || state.Uid != plan.Uid || state.Gid != plan.Gid || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return errors.Join(errRpcIntegrity, errors.New("operator writable root no longer has its original physical custody"))
		}
	}
	return nil
}

// An empty quota lock is still original physical custody, not an absent file
// that the restarted application may silently recreate.
func inspectRepairOperatorRetained(ctx context.Context, host *repairValidatorHost, plan repairOperatorHostPlan, retained []repairOperatorRetainedFile, original bool) error {
	for _, value := range retained {
		raw, info, err := readRepairOperatorFile(ctx, host, value.File.Path, plan.Uid, 16*1024*1024)
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		if (original || value.Quota) && (uint64(stat.Dev) != value.Device || stat.Ino != value.Inode || monitorReadDigest(raw) != value.File.Sha256) || value.Quota && (len(raw) != 0 || info.Mode().Perm()&0077 != 0) {
			return errors.Join(errRpcIntegrity, errors.New("operator original journal or quota custody differs"))
		}
		if value.Quota && original {
			fd, err := syscall.Open(value.File.Path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
			if err != nil {
				return repairValidatorObservationError("cannot open original operator quota owner", err, true)
			}
			file := os.NewFile(uintptr(fd), value.File.Path)
			locked, statErr := file.Stat()
			observationErr := repairValidatorObservationError("cannot inspect original operator quota owner", repairValidatorObservation(ctx, "operator-quota-open-stat", statErr), false)
			if statErr != nil {
				return errors.Join(observationErr, file.Close())
			}
			if !os.SameFile(info, locked) {
				return errors.Join(errRpcIntegrity, errors.New("operator original quota identity changed before lock observation"), observationErr, file.Close())
			}
			if observationErr != nil {
				return errors.Join(observationErr, file.Close())
			}
			lockErr := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
			if lockErr == nil {
				lockErr = unix.Flock(fd, unix.LOCK_UN)
			}
			if err := errors.Join(lockErr, file.Close(), ctx.Err()); err != nil {
				return errors.Join(errRepairProcessPending, errors.New("original operator quota owner is still busy or unavailable"), err)
			}
		}
	}
	return nil
}

// The declaration's physical roots and all raw source pins are retained in the
// process postcondition without serializing secret resources or signed bytes.
func repairOperatorResourceHash(plan repairOperatorHostPlan) string {
	raw, _ := json.Marshal(struct {
		Environment []repairOperatorEnvironment `json:"environment"`
		Resources   []repairOperatorTree        `json:"resources"`
		Roots       []repairOperatorRoot        `json:"roots"`
	}{plan.Environment, plan.Resources, plan.WritableRoots})
	return monitorReadDigest(raw)
}
