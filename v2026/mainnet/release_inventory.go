// Release inventory binds actual local file bytes to one clean source closure.
// Presence is never provenance, semantic qualification or deployment authority.
package main

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
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const releaseInventoryConfigSchema = "urnetwork-mainnet-release-inventory-config-v1"
const releaseInventorySchema = "urnetwork-mainnet-release-inventory-v1"
const maximumReleaseInventoryArtifacts = 256
const maximumReleaseInventoryArtifactBytes int64 = 512 * 1024 * 1024
const maximumReleaseInventoryTotalBytes int64 = 2 * 1024 * 1024 * 1024

// The source context must be declared again for each artifact; an old artifact
// declaration cannot silently inherit a successor source lock. This binding
// does not assert that those sources produced the bytes.
type releaseArtifactInput struct {
	Id                    string `json:"id"`
	Category              string `json:"category"`
	Path                  string `json:"path"`
	SourceLockContentHash string `json:"source_lock_content_hash"`
	ExpectedSha256        string `json:"expected_sha256,omitempty"`
	ImageReference        string `json:"image_reference,omitempty"`
}

// Explicit paths cover detached SN builds, sibling server/Connect repositories
// and external generated artifacts without guessing conventional build output.
type releaseInventoryConfig struct {
	Schema      string                 `json:"schema"`
	CandidateId string                 `json:"candidate_id"`
	SourceSnDir string                 `json:"source_sn_dir"`
	SourceLock  planFileReference      `json:"source_lock"`
	Artifacts   []releaseArtifactInput `json:"artifacts"`
}

// Exact raw bytes and executable mode are captured, not artifact interpretation.
// Paths are retained for review; no raw config/policy contents enter output.
type releaseArtifact struct {
	Id                    string `json:"id"`
	Category              string `json:"category"`
	Path                  string `json:"path"`
	SourceLockContentHash string `json:"source_lock_content_hash"`
	Sha256                string `json:"sha256"`
	Bytes                 int64  `json:"bytes"`
	Mode                  uint32 `json:"mode"`
	ImageReference        string `json:"image_reference,omitempty"`
	Status                string `json:"status"`
}

// Category coverage describes only selected file presence. One migration file
// or one executable can never establish that a whole release is complete.
type releaseInventoryCategory struct {
	Category string `json:"category"`
	Files    int    `json:"files"`
	Status   string `json:"status"`
}

// This unsigned candidate is a review input to plan, not a release approval.
// Missing categories are explicit even when the inventory operation succeeds.
type releaseInventory struct {
	Schema                string                     `json:"schema"`
	CandidateId           string                     `json:"candidate_id"`
	Status                string                     `json:"status"`
	ReleaseComplete       bool                       `json:"release_complete"`
	DeploymentApproved    bool                       `json:"deployment_approved"`
	ProvenanceProven      bool                       `json:"provenance_proven"`
	ConfigSha256          string                     `json:"config_sha256"`
	SourceLockSha256      string                     `json:"source_lock_sha256"`
	SourceLockContentHash string                     `json:"source_lock_content_hash"`
	SourceSnDir           string                     `json:"source_sn_dir"`
	SourceState           string                     `json:"source_state"`
	Artifacts             []releaseArtifact          `json:"artifacts"`
	Categories            []releaseInventoryCategory `json:"categories"`
	MissingCategories     []string                   `json:"missing_categories"`
	TotalBytes            int64                      `json:"total_bytes"`
	Limitations           []string                   `json:"limitations"`
	ContentHash           string                     `json:"content_hash"`
}

// Stable ordering keeps empty and partial inventories reproducible.
func releaseInventoryCategories() []string {
	return []string{"executable", "contract", "config", "policy", "migration", "image-identity", "dependency", "toolchain"}
}

// Reject mutable tags and credential-bearing/ambiguous image labels. The
// declared immutable reference still does not prove a local or deployed image.
func releaseInventoryImageReference(value string) bool {
	parts := strings.Split(value, "@sha256:")
	if len(parts) != 2 || parts[0] == "" || len(parts[0]) > 512 || !rootCanonicalHash("0x"+parts[1]) {
		return false
	}
	for _, character := range parts[0] {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || strings.ContainsRune("/._-:", character)) {
			return false
		}
	}
	return !strings.Contains(parts[0], "://")
}

// Syntax admission is completed before touching artifact files. Missing whole
// categories are allowed; unknown or duplicate declarations are not.
func (self releaseInventoryConfig) validate(lock sourceLock) error {
	if self.Schema != releaseInventoryConfigSchema || !planLabel(self.CandidateId) || !filepath.IsAbs(self.SourceSnDir) ||
		strings.ContainsAny(self.SourceSnDir, "$\x00") || len(self.Artifacts) > maximumReleaseInventoryArtifacts {
		return errors.New("release inventory requires its schema, candidate ID, absolute source_sn_dir and at most 256 explicit artifacts")
	}
	categoryKVs := map[string]bool{}
	for _, category := range releaseInventoryCategories() {
		categoryKVs[category] = true
	}
	seenIds := map[string]bool{}
	for _, artifact := range self.Artifacts {
		if !planLabel(artifact.Id) || seenIds[artifact.Id] || !categoryKVs[artifact.Category] || artifact.Path == "" ||
			strings.ContainsAny(artifact.Path, "$\x00") || strings.Contains(artifact.Path, "://") || artifact.SourceLockContentHash != lock.ContentHash ||
			artifact.ExpectedSha256 != "" && !planSha256(artifact.ExpectedSha256) {
			return fmt.Errorf("artifact %q has malformed identity/path/hash/category or a stale source-lock binding", artifact.Id)
		}
		if artifact.Category == "image-identity" {
			if !releaseInventoryImageReference(artifact.ImageReference) {
				return fmt.Errorf("artifact %q needs an immutable declared image reference", artifact.Id)
			}
		} else if artifact.ImageReference != "" {
			return errors.New("only image-identity entries accept image_reference")
		}
		seenIds[artifact.Id] = true
	}
	return nil
}

// Reuse the source-lock closure audit, including tracked module files and all
// local replacement repositories. Tool/Go-version differences are not source
// drift; their exact build provenance belongs to separate toolchain evidence.
func verifyReleaseInventorySources(ctx context.Context, snDir string, expected sourceLock) error {
	current, err := buildSourceLock(ctx, snDir)
	if err != nil {
		return err
	}
	if current.Schema != expected.Schema || current.Scope != expected.Scope || current.Module != expected.Module ||
		current.GoModSha256 != expected.GoModSha256 || current.GoSumSha256 != expected.GoSumSha256 ||
		!reflect.DeepEqual(current.Repositories, expected.Repositories) || !reflect.DeepEqual(current.LocalReplacements, expected.LocalReplacements) {
		return errors.New("current clean source/module/replacement closure differs from the supplied source lock")
	}
	return nil
}

// Cancellation is checked on every bounded buffer read, not only before a
// potentially large executable is hashed. No artifact-sized allocation occurs.
type releaseInventoryReader struct {
	ctx    context.Context
	reader io.Reader
}

// A canceled reader cannot continue consuming the remaining byte allowance.
func (self releaseInventoryReader) Read(buffer []byte) (int, error) {
	if err := self.ctx.Err(); err != nil {
		return 0, err
	}
	return self.reader.Read(buffer)
}

// Only regular, nonempty files are admitted. Hold one descriptor through the
// hash and compare identity, size, permissions and mtime before/after reading.
func captureReleaseArtifact(ctx context.Context, input releaseArtifactInput, parentPath string, maximumBytes int64) (releaseArtifact, error) {
	if ctx == nil || ctx.Err() != nil || maximumBytes <= 0 {
		return releaseArtifact{}, errors.New("artifact capture context or byte bound is unavailable")
	}
	path := input.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(parentPath), path)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return releaseArtifact{}, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return releaseArtifact{}, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maximumBytes {
		return releaseArtifact{}, errors.New("artifact is empty, oversized or not a regular file")
	}
	if input.Category == "executable" && before.Mode().Perm()&0111 == 0 {
		return releaseArtifact{}, errors.New("executable artifact has no execute permission")
	}
	digest := sha256.New()
	reader := releaseInventoryReader{ctx: ctx, reader: io.LimitReader(file, maximumBytes+1)}
	written, err := io.CopyBuffer(digest, reader, make([]byte, 128*1024))
	if err != nil || written > maximumBytes || written != before.Size() || ctx.Err() != nil {
		return releaseArtifact{}, errors.Join(errors.New("artifact read failed, changed size or exceeded its bound"), err, ctx.Err())
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		return releaseArtifact{}, errors.New("artifact changed while hashing")
	}
	hash := "sha256:" + hex.EncodeToString(digest.Sum(nil))
	if input.ExpectedSha256 != "" && input.ExpectedSha256 != hash {
		return releaseArtifact{}, errors.New("artifact exact bytes differ from expected_sha256")
	}
	return releaseArtifact{Id: input.Id, Category: input.Category, Path: path, SourceLockContentHash: input.SourceLockContentHash, Sha256: hash,
		Bytes: written, Mode: uint32(before.Mode().Perm()), ImageReference: input.ImageReference, Status: "present_unvalidated"}, nil
}

// A second complete hash catches in-place edits even if mtime is restored,
// replacements at the path, or generated output changed during source checks.
func verifyReleaseArtifact(ctx context.Context, input releaseArtifactInput, parentPath string, expected releaseArtifact) error {
	current, err := captureReleaseArtifact(ctx, input, parentPath, expected.Bytes)
	if err != nil || current != expected {
		return errors.Join(errors.New("artifact changed before inventory publication"), err)
	}
	return nil
}

// A path alias or hardlink is still one file. It cannot fill multiple category
// counters by being declared under different artifact IDs.
func rejectReleaseArtifactAliases(parentPath string, inputs []releaseArtifactInput) error {
	resolvedPaths := map[string]bool{}
	infos := make([]os.FileInfo, 0, len(inputs))
	for _, input := range inputs {
		path := input.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(parentPath), path)
		}
		path, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("artifact %q path: %w", input.Id, err)
		}
		if resolvedPaths[resolved] {
			return errors.New("one resolved artifact path cannot populate multiple inventory entries")
		}
		info, err := os.Lstat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("artifact resolved path is not a regular file")
		}
		for _, prior := range infos {
			if os.SameFile(prior, info) {
				return errors.New("hardlinked artifact aliases cannot populate multiple inventory entries")
			}
		}
		resolvedPaths[resolved] = true
		infos = append(infos, info)
	}
	return nil
}

// Collect explicitly selected files. Both source checks and final file rehashes
// must succeed before any inventory is published; there is no inherited cache.
func buildReleaseInventory(ctx context.Context, configPath string) (releaseInventory, error) {
	configRaw, configHash, err := readPlanFile(ctx, configPath, maximumPlanManifestBytes)
	if err != nil {
		return releaseInventory{}, err
	}
	var config releaseInventoryConfig
	if err := decodePlanJson(configRaw, &config); err != nil {
		return releaseInventory{}, err
	}
	lockRaw, err := readPlanReference(ctx, configPath, config.SourceLock, maximumPlanManifestBytes)
	if err != nil {
		return releaseInventory{}, err
	}
	var lock sourceLock
	if err := decodePlanJson(lockRaw, &lock); err != nil {
		return releaseInventory{}, err
	}
	if err := validatePlanSourceLock(lock); err != nil {
		return releaseInventory{}, err
	}
	if err := config.validate(lock); err != nil {
		return releaseInventory{}, err
	}
	snDir, err := filepath.EvalSymlinks(config.SourceSnDir)
	if err != nil {
		return releaseInventory{}, err
	}
	if err := verifyReleaseInventorySources(ctx, snDir, lock); err != nil {
		return releaseInventory{}, err
	}
	inputs := append([]releaseArtifactInput(nil), config.Artifacts...)
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Id < inputs[j].Id })
	if err := rejectReleaseArtifactAliases(configPath, inputs); err != nil {
		return releaseInventory{}, err
	}
	artifacts := make([]releaseArtifact, 0, len(inputs))
	totalBytes := int64(0)
	for _, input := range inputs {
		remaining := maximumReleaseInventoryTotalBytes - totalBytes
		artifact, err := captureReleaseArtifact(ctx, input, configPath, min(remaining, maximumReleaseInventoryArtifactBytes))
		if err != nil {
			return releaseInventory{}, fmt.Errorf("artifact %q: %w", input.Id, err)
		}
		totalBytes += artifact.Bytes
		artifacts = append(artifacts, artifact)
	}
	if err := verifyReleaseInventorySources(ctx, snDir, lock); err != nil {
		return releaseInventory{}, err
	}
	for index, input := range inputs {
		if err := verifyReleaseArtifact(ctx, input, configPath, artifacts[index]); err != nil {
			return releaseInventory{}, fmt.Errorf("artifact %q: %w", input.Id, err)
		}
	}
	if err := rejectReleaseArtifactAliases(configPath, inputs); err != nil {
		return releaseInventory{}, err
	}
	if err := ctx.Err(); err != nil {
		return releaseInventory{}, err
	}
	counts := map[string]int{}
	for _, artifact := range artifacts {
		counts[artifact.Category]++
	}
	categories := make([]releaseInventoryCategory, 0, len(releaseInventoryCategories()))
	missing := []string{}
	for _, category := range releaseInventoryCategories() {
		entry := releaseInventoryCategory{Category: category, Files: counts[category], Status: "present_unvalidated"}
		if entry.Files == 0 {
			entry.Status = "missing"
			missing = append(missing, category)
		}
		categories = append(categories, entry)
	}
	inventory := releaseInventory{
		Schema: releaseInventorySchema, CandidateId: config.CandidateId, Status: "unapproved_candidate",
		ConfigSha256: configHash, SourceLockSha256: config.SourceLock.Sha256, SourceLockContentHash: lock.ContentHash,
		SourceSnDir: snDir, SourceState: "clean-git-and-local-go-replacement-closure-rechecked",
		Artifacts: artifacts, Categories: categories, MissingCategories: missing, TotalBytes: totalBytes,
		Limitations: []string{
			"Selected file presence is not a complete release; each category still needs role-by-role coverage and semantic qualification",
			"Source binding is review context, not proof these sources/toolchains produced these executable or contract bytes",
			"Solidity library closure, compiler settings, reproducible builds and source-to-Wasm/source-to-bytecode attestations require separate proof",
			"Migration files do not establish a deployed catalog/version or safe mixed-writer cutover; image references do not establish image availability or deployment",
			"No mainnet identity, runtime capability, custody, plan authorization, production qualification or deployment approval is granted",
			"Sequential rechecks detect ordinary drift but are not an atomic filesystem snapshot; later consumers must verify exact bytes again",
		},
	}
	raw, err := json.Marshal(inventory)
	if err != nil {
		return releaseInventory{}, err
	}
	digest := sha256.Sum256(append([]byte(releaseInventorySchema+"\x00"), raw...))
	inventory.ContentHash = "sha256:" + hex.EncodeToString(digest[:])
	return inventory, nil
}

// Inventory only reads local files and invokes bounded local source-lock
// checks. Exit zero means a candidate was emitted, even with missing categories.
func runReleaseInventoryCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("release-inventory", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "strict JSON inventory config with source lock and explicit artifacts")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *configPath == "" || ctx == nil {
		fmt.Fprintln(stderr, "release-inventory requires --config FILE and no positional arguments")
		return 2
	}
	captureCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	inventory, err := buildReleaseInventory(captureCtx, *configPath)
	if err != nil {
		fmt.Fprintln(stderr, "release inventory:", err)
		return 2
	}
	if err := json.NewEncoder(stdout).Encode(inventory); err != nil {
		fmt.Fprintln(stderr, "release inventory output:", err)
		return 1
	}
	return 0
}
