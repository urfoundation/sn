package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	releaseLayoutTypeASTID = regexp.MustCompile(`(t_(?:struct|contract|enum|userDefinedValueType)\([^)]*\))\d+`)
	releaseGitCommit       = regexp.MustCompile(`^[0-9a-f]{40}$`)
	releaseSHA256          = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	releaseHex256          = regexp.MustCompile(`^0x[0-9a-f]{64}$`)
	releaseImageDigest     = regexp.MustCompile(`^[^@[:space:]]+@sha256:[0-9a-f]{64}$`)
	releaseAbigenVersion   = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

const (
	reviewedRuntimeSourceRepository         = "https://github.com/RaoFoundation/subtensor"
	reviewedRuntimeSourceTag                = ""
	reviewedRuntimeSourceRefKind            = "commit"
	reviewedRuntimeSourceRefName            = "67dcf7f791dc495064c293f080a0702cb433e51e"
	reviewedRuntimeSourceCommit             = "67dcf7f791dc495064c293f080a0702cb433e51e"
	reviewedRuntimeCodeHash                 = "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a"
	reviewedRuntimeMetadataHash             = "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc"
	reviewedRuntimeCompressedWasmSHA256     = "0x232bfc0d65ec2dbe4280b152e23f13879df9692d2286dd08c6ba14483deee00f"
	reviewedRuntimeUpstreamReleaseCallHash  = ""
	reviewedRuntimeUpstreamReleaseTimepoint = ""
	reviewedRuntimeSpecVersion              = uint32(455)
	reviewedRuntimeTransactionVersion       = uint32(1)
	reviewedRuntimeStateVersion             = uint8(1)
)

// Remove compiler IDs without collapsing two declarations that share a short
// name. Collision groups use their complete compiler type labels, and every
// nested reference follows the same substitution. Unresolvable aliases fail.
func normalizeReleaseStorageLayout(value any) (any, error) {
	layout, _ := value.(map[string]any)
	typeKVs, _ := layout["types"].(map[string]any)
	typeGroups := map[string]map[string]string{}
	for key, entry := range typeKVs {
		match := releaseLayoutTypeASTID.FindStringSubmatchIndex(key)
		if match == nil || match[0] != 0 {
			continue
		}
		raw, base := key[match[0]:match[1]], key[match[2]:match[3]]
		fields, _ := entry.(map[string]any)
		label, _ := fields["label"].(string)
		if typeGroups[base] == nil {
			typeGroups[base] = map[string]string{}
		}
		if prior, exists := typeGroups[base][raw]; exists && prior != label {
			return nil, fmt.Errorf("storage type %q has conflicting declaration labels", raw)
		}
		typeGroups[base][raw] = label
	}
	typeNames := map[string]string{}
	canonicalOwners := map[string]string{}
	for base, declarations := range typeGroups {
		for raw, label := range declarations {
			canonical := base
			if len(declarations) > 1 {
				open := strings.IndexByte(base, '(')
				kind := strings.TrimPrefix(base[:open], "t_")
				prefix := kind + " "
				if kind == "userDefinedValueType" {
					prefix = ""
				}
				qualified := strings.TrimPrefix(label, prefix)
				if label == "" || prefix != "" && qualified == label || strings.TrimSpace(qualified) != qualified || qualified == "" || strings.ContainsAny(qualified, "()\x00") {
					return nil, fmt.Errorf("ambiguous storage type %q has no complete declaration label", raw)
				}
				canonical = base[:open+1] + qualified + ")"
			}
			if prior, exists := canonicalOwners[canonical]; exists && prior != raw {
				return nil, fmt.Errorf("ambiguous storage types %q and %q share one canonical identity", prior, raw)
			}
			canonicalOwners[canonical] = raw
			typeNames[raw] = canonical
		}
	}
	normalizeName := func(text string) (string, error) {
		var nameErr error
		normalized := releaseLayoutTypeASTID.ReplaceAllStringFunc(text, func(raw string) string {
			if canonical, exists := typeNames[raw]; exists {
				return canonical
			}
			nameErr = fmt.Errorf("storage type reference %q has no declaration", raw)
			return raw
		})
		return normalized, nameErr
	}
	var visit func(any) (any, error)
	visit = func(current any) (any, error) {
		switch typed := current.(type) {
		case map[string]any:
			out := make(map[string]any, len(typed))
			for key, child := range typed {
				if key == "astId" || key == "contract" {
					continue
				}
				normalizedKey, err := normalizeName(key)
				if err != nil {
					return nil, err
				}
				if _, exists := out[normalizedKey]; exists {
					return nil, fmt.Errorf("storage layout keys collide at %q after normalization", normalizedKey)
				}
				out[normalizedKey], err = visit(child)
				if err != nil {
					return nil, err
				}
			}
			return out, nil
		case []any:
			out := make([]any, len(typed))
			for index, child := range typed {
				var err error
				out[index], err = visit(child)
				if err != nil {
					return nil, err
				}
			}
			return out, nil
		case string:
			return normalizeName(typed)
		default:
			return current, nil
		}
	}
	normalized, err := visit(value)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

type releaseLockObservation struct {
	EVMBuild       map[string]string
	Repositories   map[string]string
	Interfaces     map[string]string
	Infrastructure map[string]string
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestNamedFiles(root string, names []string) (string, error) {
	paths := make([]string, 0, len(names))
	seenPaths := map[string]struct{}{}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve release-lock root %s: %w", root, err)
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return "", err
	}
	for _, name := range names {
		clean := filepath.Clean(name)
		if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("unsafe release-lock path %q", name)
		}
		portable := filepath.ToSlash(clean)
		if _, ok := seenPaths[portable]; ok {
			return "", fmt.Errorf("duplicate release-lock path %q", portable)
		}
		seenPaths[portable] = struct{}{}
		paths = append(paths, portable)
	}
	sort.Strings(paths)
	h := sha256.New()
	var size [8]byte
	for _, name := range paths {
		path := filepath.Join(root, filepath.FromSlash(name))
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("release-lock path %q is not a regular file", name)
		}
		resolvedPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
		resolvedPath, err = filepath.Abs(resolvedPath)
		if err != nil {
			return "", err
		}
		expectedPath := filepath.Join(resolvedRoot, filepath.FromSlash(name))
		if resolvedPath != expectedPath {
			return "", fmt.Errorf("release-lock path %q traverses a symlink", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		binary.BigEndian.PutUint64(size[:], uint64(len(name)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(name))
		binary.BigEndian.PutUint64(size[:], uint64(len(data)))
		_, _ = h.Write(size[:])
		_, _ = h.Write(data)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// Fixed-path release inputs must also be reviewed Git files. A clean worktree
// does not report ignored files, so accepting a merely present path would let
// local ignored bytes enter a freshly rendered lock.
func digestTrackedNamedFiles(root string, names []string) (string, error) {
	output, err := exec.Command("git", "-C", root, "ls-files", "-z", "--cached").Output()
	if err != nil {
		return "", fmt.Errorf("enumerate reviewed release-lock files in %s: %w", root, err)
	}
	tracked := map[string]struct{}{}
	for _, raw := range bytes.Split(output, []byte{0}) {
		if len(raw) != 0 {
			tracked[filepath.ToSlash(filepath.Clean(filepath.FromSlash(string(raw))))] = struct{}{}
		}
	}
	for _, name := range names {
		clean := filepath.Clean(name)
		if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("unsafe reviewed release-lock path %q", name)
		}
		if _, ok := tracked[filepath.ToSlash(clean)]; !ok {
			return "", fmt.Errorf("release-lock path %q is not a reviewed Git file", name)
		}
	}
	return digestNamedFiles(root, names)
}

// cleanGitSubtreeHash binds large, shared runtime assets without reading
// gigabytes into every doctor process. The exact filesystem/index comparison
// also rejects ignored or nested-worktree bytes that Git status omits, while
// ls-tree supplies reviewed modes, object IDs, and paths for a stable digest.
func cleanGitSubtreeHash(root, subtree string) (string, error) {
	clean := filepath.Clean(subtree)
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe git subtree %q", subtree)
	}
	if _, err := trackedFilesUnder(root, []string{clean}, classifyCompleteReleasePath); err != nil {
		return "", fmt.Errorf("validate reviewed git subtree %s: %w", clean, err)
	}
	status := exec.Command("git", "-C", root, "status", "--porcelain=v1", "--untracked-files=all", "--", clean)
	statusOutput, err := status.Output()
	if err != nil {
		return "", fmt.Errorf("inspect platform config subtree %s: %w", clean, err)
	}
	if len(bytes.TrimSpace(statusOutput)) != 0 {
		return "", fmt.Errorf("platform config subtree %s differs from reviewed HEAD: %s", clean, strings.TrimSpace(string(statusOutput)))
	}
	tree := exec.Command("git", "-C", root, "ls-tree", "-r", "--full-tree", "HEAD", "--", clean)
	treeOutput, err := tree.Output()
	if err != nil {
		return "", fmt.Errorf("read platform config subtree %s: %w", clean, err)
	}
	if len(bytes.TrimSpace(treeOutput)) == 0 {
		return "", fmt.Errorf("platform config subtree %s has no reviewed files", clean)
	}
	return digestBytes(treeOutput), nil
}

// releasePathSelection is the single traversal and inclusion decision for a
// portable repository-relative path.
type releasePathSelection uint8

const (
	releasePathExcluded releasePathSelection = iota
	releasePathTraversed
	releasePathIncluded
)

// releasePathClassifier applies identical path semantics to the filesystem
// walk and the reviewed Git index.
type releasePathClassifier func(name string, directory bool) releasePathSelection

// Include every regular file and descend into every directory. This is used
// for configuration trees whose complete contents are release inputs.
func classifyCompleteReleasePath(_ string, directory bool) releasePathSelection {
	if directory {
		return releasePathTraversed
	}
	return releasePathIncluded
}

// Include Solidity sources without treating a nested directory named lib as
// a dependency root. External dependencies live outside the explicit roots.
func classifySolidityReleasePath(name string, directory bool) releasePathSelection {
	if directory {
		return releasePathTraversed
	}
	if strings.HasSuffix(name, ".sol") {
		return releasePathIncluded
	}
	return releasePathExcluded
}

// Include protocol sources and specifications while retaining the existing
// exclusion of Go regression files from the production digest.
func classifyProtocolReleasePath(name string, directory bool) releasePathSelection {
	if directory {
		return releasePathTraversed
	}
	if !strings.HasSuffix(name, "_test.go") {
		return releasePathIncluded
	}
	return releasePathExcluded
}

// Apply one exact path policy to both directory traversal and indexed files.
// Ambiguous nested lib/build names remain first-party; only known dependency
// and cache roots are excluded, with case-sensitive portable path semantics.
func classifyGoReleasePath(name string, directory bool) releasePathSelection {
	parts := strings.Split(name, "/")
	directoryParts := parts
	if !directory {
		directoryParts = parts[:len(parts)-1]
	}
	for index, part := range directoryParts {
		switch part {
		case ".git", "node_modules", "out", "runs", "vendor":
			return releasePathExcluded
		case "build":
			if index == 0 {
				return releasePathExcluded
			}
		case "lib":
			if index == 1 && parts[0] == "evm" {
				return releasePathExcluded
			}
		}
	}
	if directory {
		return releasePathTraversed
	}
	base := parts[len(parts)-1]
	if (strings.HasSuffix(base, ".go") && !strings.HasSuffix(base, "_test.go")) || base == "go.mod" || base == "go.sum" {
		return releasePathIncluded
	}
	return releasePathExcluded
}

// The SDK's mobile release toolchain is a nested Go module under build/. The
// general Go digest omits that top-level directory, so hash it separately with
// the complete reviewed-tree policy.
func sdkMobileBuildTreeHash(sdkRoot string) (string, error) {
	return cleanGitSubtreeHash(sdkRoot, "build")
}

// Enumerate only reviewed Git paths beneath explicit source roots. Comparing
// the filesystem selection with the index rejects ignored source/config bytes
// that ordinary Git cleanliness does not report. Working-tree bytes are hashed
// later, while the observer brackets the read with clean revision snapshots.
func trackedFilesUnder(root string, roots []string, classify releasePathClassifier) ([]string, error) {
	if len(roots) == 0 || classify == nil {
		return nil, errors.New("release-lock source roots are empty")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve release-lock repository root %s: %w", root, err)
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return nil, err
	}
	arguments := []string{"-C", root, "ls-files", "-z", "--cached", "--"}
	cleanRoots := make([]string, 0, len(roots))
	filesystemNames := map[string]struct{}{}
	for _, relativeRoot := range roots {
		if relativeRoot == "" {
			return nil, errors.New("release-lock source root is empty")
		}
		clean := filepath.Clean(relativeRoot)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("unsafe release-lock source root %q", relativeRoot)
		}
		path := filepath.Join(root, clean)
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("release-lock source root %q is not a directory", relativeRoot)
		}
		resolvedPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, err
		}
		resolvedPath, err = filepath.Abs(resolvedPath)
		if err != nil {
			return nil, err
		}
		if resolvedPath != filepath.Join(resolvedRoot, clean) {
			return nil, fmt.Errorf("release-lock source root %q traverses a symlink", relativeRoot)
		}
		cleanRoots = append(cleanRoots, filepath.ToSlash(clean))
		if err := filepath.WalkDir(path, func(candidate string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if candidate == path {
				return nil
			}
			relative, err := filepath.Rel(root, candidate)
			if err != nil {
				return err
			}
			portable := filepath.ToSlash(relative)
			if entry.IsDir() {
				switch classify(portable, true) {
				case releasePathExcluded:
					return filepath.SkipDir
				case releasePathTraversed:
					return nil
				default:
					return fmt.Errorf("release-lock path classifier included directory %q as a file", portable)
				}
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return fmt.Errorf("release-lock source path %q is not a regular file", portable)
			}
			switch classify(portable, false) {
			case releasePathIncluded:
				if _, exists := filesystemNames[portable]; exists {
					return fmt.Errorf("duplicate release-lock source path %q", portable)
				}
				filesystemNames[portable] = struct{}{}
			case releasePathExcluded:
				return nil
			default:
				return fmt.Errorf("release-lock path classifier traversed file %q as a directory", portable)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	arguments = append(arguments, cleanRoots...)
	output, err := exec.Command("git", arguments...).Output()
	if err != nil {
		return nil, fmt.Errorf("enumerate tracked release-lock files in %s: %w", root, err)
	}
	trackedNames := map[string]struct{}{}
	for _, raw := range bytes.Split(output, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		name := string(raw)
		clean := filepath.Clean(filepath.FromSlash(name))
		if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("Git returned unsafe release-lock path %q", name)
		}
		portable := filepath.ToSlash(clean)
		switch classify(portable, false) {
		case releasePathIncluded:
			trackedNames[portable] = struct{}{}
		case releasePathExcluded:
			continue
		default:
			return nil, fmt.Errorf("release-lock path classifier traversed indexed file %q as a directory", portable)
		}
	}
	filesystemPaths := make([]string, 0, len(filesystemNames))
	for name := range filesystemNames {
		filesystemPaths = append(filesystemPaths, name)
	}
	sort.Strings(filesystemPaths)
	for _, name := range filesystemPaths {
		if _, ok := trackedNames[name]; !ok {
			return nil, fmt.Errorf("release-lock source path %q is not a reviewed Git file", name)
		}
	}
	trackedPaths := make([]string, 0, len(trackedNames))
	for name := range trackedNames {
		trackedPaths = append(trackedPaths, name)
	}
	sort.Strings(trackedPaths)
	for _, name := range trackedPaths {
		if _, ok := filesystemNames[name]; !ok {
			return nil, fmt.Errorf("reviewed release-lock source path %q is missing from the worktree", name)
		}
	}
	if len(trackedNames) == 0 {
		return nil, fmt.Errorf("release-lock source roots %v contain no reviewed files", cleanRoots)
	}
	return trackedPaths, nil
}

func goReleaseSourceHash(root string) (string, error) {
	names, err := trackedFilesUnder(root, []string{"."}, classifyGoReleasePath)
	if err != nil {
		return "", err
	}
	return digestNamedFiles(root, names)
}

// Bind a Go module's source digest to one clean commit observed on both sides
// of the read. This closes the gap where a sibling checkout changes while a
// release-lock observation is walking its files.
func observeCleanGoReleaseSource(root string) (string, string, error) {
	revision, err := cleanGitRevision(root)
	if err != nil {
		return "", "", err
	}
	hash, err := goReleaseSourceHash(root)
	if err != nil {
		return "", "", err
	}
	confirmedRevision, err := cleanGitRevision(root)
	if err != nil {
		return "", "", err
	}
	if confirmedRevision != revision {
		return "", "", errors.New("dependency checkout changed during source observation")
	}
	return hash, revision, nil
}

// Name the two independently checked fields exactly as they appear in the
// release lock so observation and schema validation cannot drift apart.
func observeOperatorProxyReleaseSource(root string) (map[string]string, error) {
	hash, commit, err := observeCleanGoReleaseSource(root)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"operator_proxy_go_source_hash": hash,
		"operator_proxy_commit":         commit,
	}, nil
}

func soliditySourceHash(snRoot string) (string, error) {
	names, err := trackedFilesUnder(snRoot, []string{"evm/src", "evm/script"}, classifySolidityReleasePath)
	if err != nil {
		return "", err
	}
	return digestNamedFiles(snRoot, names)
}

func solidityCompilerSettingsHash(snRoot string) (string, error) {
	return digestTrackedNamedFiles(snRoot, []string{"evm/foundry.toml", "evm/remappings.txt"})
}

func cleanGitRevision(root string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve dependency checkout %s: %w", root, err)
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return "", err
	}
	topLevel := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel")
	topLevelOutput, err := topLevel.Output()
	if err != nil {
		return "", fmt.Errorf("locate dependency checkout %s: %w", root, err)
	}
	resolvedTopLevel, err := filepath.EvalSymlinks(strings.TrimSpace(string(topLevelOutput)))
	if err != nil {
		return "", fmt.Errorf("resolve dependency checkout root %s: %w", root, err)
	}
	resolvedTopLevel, err = filepath.Abs(resolvedTopLevel)
	if err != nil {
		return "", err
	}
	if resolvedRoot != resolvedTopLevel {
		return "", fmt.Errorf("dependency checkout %s is not its Git worktree root", root)
	}
	status := exec.Command("git", "-C", root, "status", "--porcelain", "--untracked-files=all")
	statusOutput, err := status.Output()
	if err != nil {
		return "", fmt.Errorf("inspect dependency checkout %s: %w", root, err)
	}
	if len(bytes.TrimSpace(statusOutput)) != 0 {
		return "", fmt.Errorf("dependency checkout %s has uncommitted or untracked files", root)
	}
	revision := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	revisionOutput, err := revision.Output()
	if err != nil {
		return "", fmt.Errorf("read dependency revision %s: %w", root, err)
	}
	value := strings.TrimSpace(string(revisionOutput))
	if !releaseGitCommit.MatchString(value) {
		return "", fmt.Errorf("dependency checkout %s returned an invalid revision", root)
	}
	return value, nil
}

func parseFoundryVersion(output []byte) (string, string, error) {
	var version string
	var commit string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "forge Version:"):
			if version != "" {
				return "", "", errors.New("forge version output contains duplicate version fields")
			}
			version = strings.TrimSpace(strings.TrimPrefix(line, "forge Version:"))
		case strings.HasPrefix(line, "Commit SHA:"):
			if commit != "" {
				return "", "", errors.New("forge version output contains duplicate commit fields")
			}
			commit = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "Commit SHA:")))
		}
	}
	if version == "" || !releaseGitCommit.MatchString(commit) {
		return "", "", errors.New("forge version output is incomplete or malformed")
	}
	return version, commit, nil
}

// The sole success line emitted by the shared binding-generator preflight is
// the complete identity contract for release observation and generation.
func parseAbigenVersion(output []byte) (string, error) {
	const prefix = "abigen version "
	const suffix = "-stable"
	raw := string(output)
	if !strings.HasSuffix(raw, "\n") || strings.Count(raw, "\n") != 1 || strings.Contains(raw, "\r") {
		return "", errors.New("abigen version output is malformed")
	}
	value := strings.TrimSuffix(raw, "\n")
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return "", errors.New("abigen version output is malformed")
	}
	version := strings.TrimSuffix(strings.TrimPrefix(value, prefix), suffix)
	if !releaseAbigenVersion.MatchString(version) {
		return "", errors.New("abigen version output is malformed")
	}
	return version, nil
}

// Use the generator's own resolver and version policy so the release lock
// cannot attest a different executable than binding generation will run.
func observeAbigenVersion(snRoot string) (string, error) {
	executable := filepath.Join(snRoot, "stabi", "generate.sh")
	command := exec.Command(executable, "--preflight")
	command.Dir = snRoot
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("preflight abigen: %w: %s", err, bytes.TrimSpace(output))
	}
	return parseAbigenVersion(output)
}

func observeFoundryVersion() (string, string, error) {
	executable, err := exec.LookPath("forge")
	if err != nil {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", "", fmt.Errorf("locate forge: %w", err)
		}
		executable = filepath.Join(home, ".foundry", "bin", "forge")
	}
	output, err := exec.Command(executable, "--version").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("read forge version: %w: %s", err, bytes.TrimSpace(output))
	}
	return parseFoundryVersion(output)
}

func protocolSourceHash(snRoot string) (string, error) {
	names, err := trackedFilesUnder(snRoot, []string{"protocol", "docs/spec"}, classifyProtocolReleasePath)
	if err != nil {
		return "", err
	}
	// The runtime source manifest and its enforcement entry points are part of
	// the protocol claim, not incidental CI plumbing. Bind their exact bytes so
	// a release-lock update cannot preserve docs while weakening the checker.
	names = append(names,
		"WHITEPAPER.md",
		"VALIDATOR.md",
		"stabi/generate.sh",
		"scripts/check-runtime-metadata-artifacts.sh",
		"scripts/check-runtime-v454-source.sh",
		"scripts/test-release-1.0-local.sh",
		"scripts/test-release-1.0-producer-gate.sh",
		"tools/runtime-metadata-probe/Cargo.lock",
		"tools/runtime-metadata-probe/Cargo.toml",
		"tools/runtime-metadata-probe/rust-toolchain.toml",
		"tools/runtime-metadata-probe/src/lib.rs",
		"tools/runtime-metadata-probe/src/main.rs",
	)
	return digestTrackedNamedFiles(snRoot, names)
}

// serverLocalDependencyConfigHash covers the server/local compose contract
// mirrored by sim-testnet and every PostgreSQL init hook mounted into the
// release containers. Enumerating the directory also makes newly added hooks
// fail the lock instead of executing as unreviewed container input.
func serverLocalDependencyConfigHash(serverRoot string) (string, error) {
	names, err := trackedFilesUnder(serverRoot, []string{"local/postgres/initdb"}, classifyCompleteReleasePath)
	if err != nil {
		return "", err
	}
	names = append(names, "local/docker-compose.yml")
	return digestTrackedNamedFiles(serverRoot, names)
}

// releaseABI names one generated interface included in the aggregate ABI lock.
type releaseABI struct {
	name string
	abi  string
}

var generatedReleaseABIs = []releaseABI{
	{"Coordinator", CoordinatorABI},
	{"CoordinatorAdversary", CoordinatorAdversaryABI},
	{"ERC1967Proxy", ERC1967ProxyABI},
	{"FleetBatcher", FleetBatcherABI},
	{"ReserveSink", ReserveSinkABI},
	{"SettlementVault", SettlementVaultABI},
	{"SubnetProbe", SubnetProbeABI},
	{"ValidatorEvidence", ValidatorEvidenceABI},
}

// digestReleaseABIs hashes the ordered contract names and exact generated ABIs.
func digestReleaseABIs(entries []releaseABI) string {
	h := sha256.New()
	var size [8]byte
	for _, entry := range entries {
		binary.BigEndian.PutUint64(size[:], uint64(len(entry.name)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(entry.name))
		binary.BigEndian.PutUint64(size[:], uint64(len(entry.abi)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(entry.abi))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// generatedABIHash returns the aggregate ABI hash for every deployed artifact.
func generatedABIHash() string {
	return digestReleaseABIs(generatedReleaseABIs)
}

// foundryStorageLayoutHash pins the upgradeable coordinator's complete
// compiler-emitted storage layout. Decoding and re-encoding through encoding/json
// makes the fingerprint independent of whitespace and object-key order while
// retaining slot, offset, type, label, and nested member information.
func foundryStorageLayoutHash(snRoot, artifact string) (string, error) {
	b, err := os.ReadFile(filepath.Join(snRoot, artifact))
	if err != nil {
		return "", err
	}
	var value struct {
		StorageLayout any `json:"storageLayout"`
	}
	if err := json.Unmarshal(b, &value); err != nil {
		return "", fmt.Errorf("decode Foundry artifact storage layout: %w", err)
	}
	if value.StorageLayout == nil {
		return "", errors.New("Foundry artifact has no storageLayout")
	}
	layout, err := normalizeReleaseStorageLayout(value.StorageLayout)
	if err != nil {
		return "", fmt.Errorf("normalize Foundry storage layout: %w", err)
	}
	canonical, err := json.Marshal(layout)
	if err != nil {
		return "", fmt.Errorf("canonicalize Foundry storage layout: %w", err)
	}
	return digestBytes(canonical), nil
}

func moduleRoot(parent, name string) (string, error) {
	root := filepath.Join(parent, name)
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("release module %s: %w", name, err)
	}
	want := "github.com/urnetwork/" + name
	found := ""
	for _, line := range strings.Split(string(module), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			if found != "" {
				return "", fmt.Errorf("release module %s declares multiple module identities", name)
			}
			found = fields[1]
		}
	}
	if found != want {
		return "", fmt.Errorf("release module %s declares %q, want %q", name, found, want)
	}
	return root, nil
}

// Lock the rendered RPC behavior, not only the playbook that happens to install
// it. A template or included-task change can otherwise alter the endpoint while
// leaving the release lock green.
var subtensorGatewayReleaseFiles = []string{
	"main/ansible/playbook-subtensor.yml",
	"main/ansible/tasks/subtensor-gateway.yml",
	"main/ansible/host_files/snow/subtensor/nginx.conf.j2",
	"main/ansible/host_files/snow/subtensor/nginx-overlay.conf.j2",
}

// The node lock binds both side-by-side service definitions and the complete
// independent lightnode rollout/readiness path used before sim-testnet-light
// is allowed to run.
var subtensorNodeReleaseFiles = []string{
	"main/ansible/host_files/snow/subtensor/vars.yml",
	"main/ansible/host_files/snow/subtensor/docker-compose.yml.j2",
	"main/ansible/host_files/snow/subtensor/subtensor.service",
	"main/ansible/playbook-subtensor-lightnode.yml",
	"main/ansible/run-playbook.sh",
	"main/ansible/resolve-controller-virtualenv.sh",
	"main/ansible/run-subtensor.sh",
	"main/ansible/tasks/subtensor-lightnode-preflight.yml",
	"main/ansible/tasks/subtensor-monitor-helper.yml",
	"main/ansible/host_files/snow/subtensor/subtensor-monitor.py",
	"main/ansible/host_files/snow/subtensor/subtensor-monitor.sudoers",
}

func observeReleaseLockUnchecked(cfg *ResolvedConfig) (*releaseLockObservation, error) {
	if cfg == nil || cfg.Repos.SN == "" || cfg.Repos.Server == "" || cfg.Repos.OperatorProxy == "" {
		return nil, errors.New("release repository paths are incomplete")
	}
	observation := &releaseLockObservation{EVMBuild: map[string]string{}, Repositories: map[string]string{}, Interfaces: map[string]string{}, Infrastructure: map[string]string{}}
	var err error
	observation.EVMBuild["source_hash"], err = soliditySourceHash(cfg.Repos.SN)
	if err != nil {
		return nil, err
	}
	observation.EVMBuild["abigen"], err = observeAbigenVersion(cfg.Repos.SN)
	if err != nil {
		return nil, err
	}
	observation.EVMBuild["foundry"], observation.EVMBuild["foundry_commit"], err = observeFoundryVersion()
	if err != nil {
		return nil, err
	}
	for key, relative := range map[string]string{
		"forge_std_commit":                          "forge-std",
		"openzeppelin_contracts_commit":             "openzeppelin-contracts",
		"openzeppelin_contracts_upgradeable_commit": "openzeppelin-contracts-upgradeable",
	} {
		observation.EVMBuild[key], err = cleanGitRevision(filepath.Join(cfg.Repos.SN, "evm", "lib", relative))
		if err != nil {
			return nil, err
		}
	}
	observation.EVMBuild["compiler_settings_hash"], err = solidityCompilerSettingsHash(cfg.Repos.SN)
	if err != nil {
		return nil, err
	}
	observation.EVMBuild["reserve_sink_runtime_hash"] = ReserveSinkRuntimeBytecodeHash
	observation.EVMBuild["settlement_vault_runtime_hash"] = SettlementVaultRuntimeBytecodeHash
	observation.EVMBuild["coordinator_implementation_runtime_hash"] = CoordinatorRuntimeBytecodeHash
	observation.EVMBuild["coordinator_proxy_runtime_hash"] = ERC1967ProxyRuntimeBytecodeHash
	observation.EVMBuild["governance_drill_implementation_runtime_hash"] = CoordinatorAdversaryRuntimeBytecodeHash
	observation.EVMBuild["precompile_probe_runtime_hash"] = SubnetProbeRuntimeBytecodeHash
	observation.EVMBuild["fleet_batcher_runtime_hash"] = FleetBatcherRuntimeBytecodeHash
	observation.EVMBuild["reserve_sink_artifact_hash"] = ReserveSinkFoundryArtifactHash
	observation.EVMBuild["settlement_vault_artifact_hash"] = SettlementVaultFoundryArtifactHash
	observation.EVMBuild["coordinator_implementation_artifact_hash"] = CoordinatorFoundryArtifactHash
	observation.EVMBuild["coordinator_proxy_artifact_hash"] = ERC1967ProxyFoundryArtifactHash
	observation.EVMBuild["governance_drill_implementation_artifact_hash"] = CoordinatorAdversaryFoundryArtifactHash
	observation.EVMBuild["precompile_probe_artifact_hash"] = SubnetProbeFoundryArtifactHash
	observation.EVMBuild["fleet_batcher_artifact_hash"] = FleetBatcherFoundryArtifactHash
	observation.EVMBuild["governance_drill_storage_layout_hash"] = CoordinatorAdversaryStorageLayoutHash
	observation.EVMBuild["fleet_batcher_storage_layout_hash"] = FleetBatcherStorageLayoutHash
	observation.EVMBuild["validator_evidence_runtime_hash"] = ValidatorEvidenceRuntimeBytecodeHash
	observation.EVMBuild["validator_evidence_artifact_hash"] = ValidatorEvidenceFoundryArtifactHash
	observation.EVMBuild["validator_evidence_storage_layout_hash"] = ValidatorEvidenceStorageLayoutHash
	observation.EVMBuild["abi_hash"] = generatedABIHash()
	observation.EVMBuild["coordinator_storage_layout_hash"] = CoordinatorStorageLayoutHash

	parent := filepath.Dir(cfg.Repos.SN)
	modules := map[string]string{"sn": cfg.Repos.SN, "server": cfg.Repos.Server}
	for _, name := range []string{"connect", "sdk", "glog", "goidenticons", "proxy", "userwireguard"} {
		root, rootErr := moduleRoot(parent, name)
		if rootErr != nil {
			return nil, rootErr
		}
		modules[name] = root
	}
	for name, root := range modules {
		hash, hashErr := goReleaseSourceHash(root)
		if hashErr != nil {
			return nil, fmt.Errorf("hash %s module: %w", name, hashErr)
		}
		observation.Repositories[name+"_go_source_hash"] = hash
	}
	operatorProxyObservation, err := observeOperatorProxyReleaseSource(cfg.Repos.OperatorProxy)
	if err != nil {
		return nil, fmt.Errorf("observe operator-proxy module: %w", err)
	}
	for key, value := range operatorProxyObservation {
		observation.Repositories[key] = value
	}
	observation.Repositories["sdk_mobile_build_tree_hash"], err = sdkMobileBuildTreeHash(modules["sdk"])
	if err != nil {
		return nil, fmt.Errorf("hash SDK mobile build module: %w", err)
	}
	observation.Repositories["protocol_source_hash"], err = protocolSourceHash(cfg.Repos.SN)
	if err != nil {
		return nil, err
	}
	configNames, err := trackedFilesUnder(cfg.Repos.PlatformConfig, []string{"local"}, classifyCompleteReleasePath)
	if err != nil {
		return nil, fmt.Errorf("enumerate platform config: %w", err)
	}
	observation.Repositories["platform_config_source_hash"], err = digestNamedFiles(cfg.Repos.PlatformConfig, configNames)
	if err != nil {
		return nil, fmt.Errorf("hash platform config: %w", err)
	}
	observation.Repositories["platform_config_shared_tree_hash"], err = cleanGitSubtreeHash(cfg.Repos.PlatformConfig, "all")
	if err != nil {
		return nil, fmt.Errorf("hash shared platform config: %w", err)
	}

	interfaces, err := trackedFilesUnder(cfg.Repos.SN, []string{"evm/src/interfaces"}, classifySolidityReleasePath)
	if err != nil {
		return nil, err
	}
	observation.Interfaces["precompile_interfaces_source_hash"], err = digestNamedFiles(cfg.Repos.SN, interfaces)
	if err != nil {
		return nil, err
	}

	xops := filepath.Join(parent, "xops")
	observation.Infrastructure["gateway_config_hash"], err = digestTrackedNamedFiles(xops, subtensorGatewayReleaseFiles)
	if err != nil {
		return nil, fmt.Errorf("hash RPC gateway config: %w", err)
	}
	observation.Infrastructure["node_config_hash"], err = digestTrackedNamedFiles(xops, subtensorNodeReleaseFiles)
	if err != nil {
		return nil, fmt.Errorf("hash Subtensor node config: %w", err)
	}
	observation.Infrastructure["server_local_config_hash"], err = serverLocalDependencyConfigHash(cfg.Repos.Server)
	if err != nil {
		return nil, fmt.Errorf("hash server/local dependency config: %w", err)
	}
	if err := validateReleaseLockObservation(observation); err != nil {
		return nil, err
	}
	return observation, nil
}

func lockString(section map[string]any, key string) (string, error) {
	value, ok := section[key]
	if !ok || value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
		return "", fmt.Errorf("release lock field %s is missing", key)
	}
	return strings.TrimSpace(fmt.Sprint(value)), nil
}

func compareLockSection(name string, locked map[string]any, observed map[string]string, allowedAnnotations map[string]struct{}) error {
	for key, got := range observed {
		want, err := lockString(locked, key)
		if err != nil {
			return err
		}
		if !strings.EqualFold(want, got) {
			return fmt.Errorf("release lock %s.%s mismatch: locked=%s observed=%s", name, key, want, got)
		}
	}
	for key := range locked {
		if _, ok := observed[key]; ok {
			continue
		}
		if _, ok := allowedAnnotations[key]; !ok {
			return fmt.Errorf("release lock %s.%s is not an observed field or an allowed annotation", name, key)
		}
	}
	return nil
}

// Require the independently versioned operator-proxy module to carry both its
// exact clean commit and its production-source digest in the release schema.
func validateReleaseRepositorySchema(repositories map[string]any) error {
	sourceHash, err := lockString(repositories, "operator_proxy_go_source_hash")
	if err != nil {
		return err
	}
	if !releaseSHA256.MatchString(sourceHash) {
		return errors.New("release lock repositories.operator_proxy_go_source_hash is not a canonical SHA-256 digest")
	}
	commit, err := lockString(repositories, "operator_proxy_commit")
	if err != nil {
		return err
	}
	if !releaseGitCommit.MatchString(commit) {
		return errors.New("release lock repositories.operator_proxy_commit is not a canonical Git commit")
	}
	return nil
}

// Bind the operational testnet profile to the source and finalized Wasm
// independently reviewed for runtime 455. Exact-commit testnet provenance is
// distinct from a tagged mainnet proposal; no such proposal is asserted here.
// The node image is pinned separately:
// an older compatible binary may execute this on-chain Wasm while it syncs.
func validateReviewedRuntimeIdentity(lock *ReleaseLock) error {
	if lock == nil || lock.Runtime.SourceRepository != reviewedRuntimeSourceRepository || lock.Runtime.SourceTag != reviewedRuntimeSourceTag || lock.Runtime.SourceRefKind != reviewedRuntimeSourceRefKind || lock.Runtime.SourceRefName != reviewedRuntimeSourceRefName || lock.Runtime.SourceCommit != reviewedRuntimeSourceCommit || lock.Runtime.SpecVersion != reviewedRuntimeSpecVersion || lock.Runtime.TransactionVersion != reviewedRuntimeTransactionVersion || lock.Runtime.StateVersion != reviewedRuntimeStateVersion || !strings.EqualFold(lock.Runtime.CodeHash, reviewedRuntimeCodeHash) || !strings.EqualFold(lock.Runtime.MetadataHash, reviewedRuntimeMetadataHash) || !strings.EqualFold(lock.Runtime.CompressedWasmSHA256, reviewedRuntimeCompressedWasmSHA256) || lock.Runtime.UpstreamReleaseCallHash != reviewedRuntimeUpstreamReleaseCallHash || lock.Runtime.UpstreamReleaseTimepoint != reviewedRuntimeUpstreamReleaseTimepoint {
		return errors.New("release lock runtime identity is not the reviewed testnet runtime 455 release")
	}
	return nil
}

func validateReleaseLock(cfg *ResolvedConfig) error {
	if cfg == nil {
		return errors.New("resolved release configuration is missing")
	}
	if err := validateReleaseLockStatic(cfg.Release); err != nil {
		return err
	}
	observed, err := observeReleaseLock(cfg)
	if err != nil {
		return err
	}
	if err := compareLockSection("evm_build", cfg.Release.EVMBuild, observed.EVMBuild, releaseKeySet(releaseEVMAnnotationKeys)); err != nil {
		return err
	}
	if err := compareLockSection("repositories", cfg.Release.Repositories, observed.Repositories, releaseKeySet(releaseRepositoryAnnotationKeys)); err != nil {
		return err
	}
	if err := compareLockSection("interfaces", cfg.Release.Interfaces, observed.Interfaces, nil); err != nil {
		return err
	}
	if err := compareLockSection("infrastructure", cfg.Release.Infrastructure, observed.Infrastructure, nil); err != nil {
		return err
	}
	return nil
}
