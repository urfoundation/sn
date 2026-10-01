package main

// A source lock records exact clean Git inputs for a future qualified mainnet
// release. It has no signer, RPC client, deployment action or approval claim.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const sourceLockSchema = "urnetwork-mainnet-source-lock-v1"
const sourceLockMaximumModuleBytes = 8 * 1024 * 1024

type sourceLockRepository struct {
	Path   string `json:"path"`
	Commit string `json:"commit"`
}

type sourceLockReplacement struct {
	Module     string `json:"module"`
	LocalPath  string `json:"local_path"`
	Repository string `json:"repository"`
}

type sourceLock struct {
	Schema            string                  `json:"schema"`
	Scope             string                  `json:"scope"`
	Module            string                  `json:"module"`
	GoVersion         string                  `json:"go_version"`
	ToolSha256        string                  `json:"tool_sha256"`
	GoModSha256       string                  `json:"go_mod_sha256"`
	GoSumSha256       string                  `json:"go_sum_sha256"`
	Repositories      []sourceLockRepository  `json:"repositories"`
	LocalReplacements []sourceLockReplacement `json:"local_replacements"`
	ContentHash       string                  `json:"content_hash"`
}

type sourceLockModuleEdit struct {
	Module struct {
		Path string `json:"Path"`
	} `json:"Module"`
	Replace []struct {
		Old struct {
			Path string `json:"Path"`
		} `json:"Old"`
		New struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
		} `json:"New"`
	} `json:"Replace"`
}

func runSourceLockCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("source-lock", flag.ContinueOnError)
	flags.SetOutput(stderr)
	snDir := flags.String("sn-dir", "", "absolute path to the clean SN repository")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || !filepath.IsAbs(*snDir) {
		fmt.Fprintln(stderr, "source-lock requires --sn-dir with an absolute path and no positional arguments")
		return 2
	}
	lock, err := buildSourceLock(ctx, *snDir)
	if err != nil {
		fmt.Fprintln(stderr, "source-lock:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(lock); err != nil {
		fmt.Fprintln(stderr, "source-lock output:", err)
		return 1
	}
	return 0
}

func sourceLockCommand(ctx context.Context, dir, executable string, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(commandCtx, executable, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s failed: %w", executable, strings.Join(args, " "), errors.Join(err, commandCtx.Err()))
	}
	if len(output) > sourceLockMaximumModuleBytes {
		return nil, fmt.Errorf("%s reply exceeds source-lock resource bound", executable)
	}
	return output, nil
}

func sourceLockFileHash(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("source-lock input is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(hash.Sum(nil)), nil
}

func sourceLockGitRepository(ctx context.Context, snDir, path string) (sourceLockRepository, error) {
	rootBytes, err := sourceLockCommand(ctx, path, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return sourceLockRepository{}, err
	}
	root, err := filepath.EvalSymlinks(strings.TrimSpace(string(rootBytes)))
	if err != nil {
		return sourceLockRepository{}, err
	}
	commitBytes, err := sourceLockCommand(ctx, root, "git", "rev-parse", "--verify", "HEAD")
	if err != nil {
		return sourceLockRepository{}, err
	}
	commit := strings.TrimSpace(string(commitBytes))
	if len(commit) != 40 {
		return sourceLockRepository{}, errors.New("source repository HEAD is not a 40-character Git commit")
	}
	status, err := sourceLockCommand(ctx, root, "git", "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return sourceLockRepository{}, err
	}
	if len(status) != 0 {
		return sourceLockRepository{}, fmt.Errorf("source repository %s has uncommitted or untracked files", root)
	}
	relative, err := filepath.Rel(snDir, root)
	if err != nil {
		return sourceLockRepository{}, err
	}
	return sourceLockRepository{Path: filepath.ToSlash(relative), Commit: commit}, nil
}

func sourceLockRequireTracked(ctx context.Context, snDir string, repo sourceLockRepository, path string) error {
	repoDir := filepath.Join(snDir, filepath.FromSlash(repo.Path))
	relative, err := filepath.Rel(repoDir, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.Join(errors.New("module file is outside its source repository"), err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.Join(errors.New("module file is not a regular tracked file"), err)
	}
	if _, err := sourceLockCommand(ctx, repoDir, "git", "ls-files", "--error-unmatch", "--", relative); err != nil {
		return fmt.Errorf("module file %s is not tracked by its source repository: %w", relative, err)
	}
	return nil
}

func buildSourceLock(ctx context.Context, rawSnDir string) (sourceLock, error) {
	snDir, err := filepath.EvalSymlinks(rawSnDir)
	if err != nil {
		return sourceLock{}, err
	}
	if !filepath.IsAbs(snDir) {
		return sourceLock{}, errors.New("SN source directory is not absolute")
	}
	workspaceOutput, err := sourceLockCommand(ctx, snDir, "go", "env", "GOWORK")
	if err != nil {
		return sourceLock{}, err
	}
	workspace := strings.TrimSpace(string(workspaceOutput))
	if workspace != "" && workspace != "off" {
		return sourceLock{}, errors.New("active go.work can replace unlocked source inputs; qualify with GOWORK=off")
	}
	moduleBytes, err := os.ReadFile(filepath.Join(snDir, "go.mod"))
	if err != nil || len(moduleBytes) == 0 || len(moduleBytes) > sourceLockMaximumModuleBytes {
		return sourceLock{}, errors.Join(errors.New("SN go.mod is unavailable or oversized"), err)
	}
	moduleOutput, err := sourceLockCommand(ctx, snDir, "go", "mod", "edit", "-json")
	if err != nil {
		return sourceLock{}, err
	}
	var edit sourceLockModuleEdit
	if err := json.Unmarshal(moduleOutput, &edit); err != nil || edit.Module.Path != "github.com/urfoundation/sn/v2026" {
		return sourceLock{}, errors.Join(errors.New("source directory is not the SN module"), err)
	}
	repoByPath := map[string]sourceLockRepository{}
	rootRepo, err := sourceLockGitRepository(ctx, snDir, snDir)
	if err != nil || rootRepo.Path != "." {
		return sourceLock{}, errors.Join(errors.New("SN module root is not its clean Git root"), err)
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		if err := sourceLockRequireTracked(ctx, snDir, rootRepo, filepath.Join(snDir, name)); err != nil {
			return sourceLock{}, err
		}
	}
	repoByPath[rootRepo.Path] = rootRepo
	replacements := make([]sourceLockReplacement, 0, len(edit.Replace))
	for _, replacement := range edit.Replace {
		if replacement.New.Version != "" {
			continue
		}
		if replacement.Old.Path == "" || replacement.New.Path == "" {
			return sourceLock{}, errors.New("local replacement has no module or path")
		}
		localPath := replacement.New.Path
		if !filepath.IsAbs(localPath) {
			localPath = filepath.Join(snDir, localPath)
		}
		localPath, err = filepath.EvalSymlinks(localPath)
		if err != nil {
			return sourceLock{}, err
		}
		repo, err := sourceLockGitRepository(ctx, snDir, localPath)
		if err != nil {
			return sourceLock{}, fmt.Errorf("local replacement %s: %w", replacement.Old.Path, err)
		}
		if err := sourceLockRequireTracked(ctx, snDir, repo, filepath.Join(localPath, "go.mod")); err != nil {
			return sourceLock{}, fmt.Errorf("local replacement %s: %w", replacement.Old.Path, err)
		}
		repoByPath[repo.Path] = repo
		replacements = append(replacements, sourceLockReplacement{Module: replacement.Old.Path, LocalPath: replacement.New.Path, Repository: repo.Path})
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].Module < replacements[j].Module })
	repositories := make([]sourceLockRepository, 0, len(repoByPath))
	for _, repo := range repoByPath {
		repositories = append(repositories, repo)
	}
	sort.Slice(repositories, func(i, j int) bool { return repositories[i].Path < repositories[j].Path })
	goModHash, err := sourceLockFileHash(filepath.Join(snDir, "go.mod"))
	if err != nil {
		return sourceLock{}, err
	}
	parsedModHash := sha256.Sum256(moduleBytes)
	if goModHash != "0x"+hex.EncodeToString(parsedModHash[:]) {
		return sourceLock{}, errors.New("SN go.mod changed while source lock was being built")
	}
	goSumHash, err := sourceLockFileHash(filepath.Join(snDir, "go.sum"))
	if err != nil {
		return sourceLock{}, err
	}
	toolPath, err := os.Executable()
	if err != nil {
		return sourceLock{}, err
	}
	toolHash, err := sourceLockFileHash(toolPath)
	if err != nil {
		return sourceLock{}, err
	}
	for _, original := range repositories {
		current, err := sourceLockGitRepository(ctx, snDir, filepath.Join(snDir, filepath.FromSlash(original.Path)))
		if err != nil || current != original {
			return sourceLock{}, errors.Join(errors.New("source repository changed while source lock was being built"), err)
		}
	}
	lock := sourceLock{Schema: sourceLockSchema, Scope: "clean-git-sources-and-local-go-replacements-only", Module: edit.Module.Path, GoVersion: runtime.Version(), ToolSha256: toolHash, GoModSha256: goModHash, GoSumSha256: goSumHash, Repositories: repositories, LocalReplacements: replacements}
	encoded, err := json.Marshal(lock)
	if err != nil {
		return sourceLock{}, err
	}
	digest := sha256.Sum256(append([]byte(sourceLockSchema+"\x00"), encoded...))
	lock.ContentHash = "0x" + hex.EncodeToString(digest[:])
	return lock, nil
}
