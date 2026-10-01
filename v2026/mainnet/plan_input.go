// Plan input loading is bounded local-file I/O only. Exact file hashes and
// observation seals are verified without calling an RPC, Git or signer.
package main

import (
	"bytes"
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

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/sys/unix"
)

const maximumPlanSnapshotBytes = 2*(maximumRuntimeSnapshotCodeBytes+maximumRuntimeSnapshotMetadataBytes) + 2*maxRpcReplyBytes
const maximumPlanManifestBytes = maxRpcReplyBytes
const maximumPlanReviewInputs = 32

// Nonblocking/no-follow open refuses special files and final-component links
// before reading. A replacement regular file still has to match the exact hash.
func readPlanFile(ctx context.Context, path string, maximumBytes int) ([]byte, string, error) {
	if ctx == nil || ctx.Err() != nil || path == "" || strings.ContainsAny(path, "$\x00") {
		return nil, "", errors.New("plan input context or literal file path is unavailable")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, "", err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > int64(maximumBytes) {
		return nil, "", errors.New("plan input is empty, oversized or not a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(maximumBytes)+1))
	if err != nil || len(raw) == 0 || len(raw) > maximumBytes || ctx.Err() != nil {
		return nil, "", errors.Join(errors.New("plan input read failed, was canceled or exceeded its bound"), err, ctx.Err())
	}
	digest := sha256.Sum256(raw)
	return raw, "sha256:" + hex.EncodeToString(digest[:]), nil
}

// Unknown fields, duplicate names and trailing values cannot silently change
// a reviewed identity. Shared runtime decoders separately require full tuples.
func decodePlanJson(raw []byte, result any) error {
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(result); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("plan input has trailing JSON")
	}
	return nil
}

// References are resolved relative to their containing manifest. Neither URLs
// nor environment substitutions are fetched or expanded.
func readPlanReference(ctx context.Context, parentPath string, reference planFileReference, maximumBytes int) ([]byte, error) {
	if !planSha256(reference.Sha256) || reference.Path == "" || strings.Contains(reference.Path, "://") {
		return nil, errors.New("plan reference requires a literal local path and canonical sha256 digest")
	}
	path := reference.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(parentPath), path)
	}
	raw, digest, err := readPlanFile(ctx, path, maximumBytes)
	if err != nil {
		return nil, err
	}
	if digest != reference.Sha256 {
		return nil, errors.New("plan reference exact file hash differs")
	}
	return raw, nil
}

// Hash strings have one representation so input spellings cannot introduce
// alternate identities for the same file.
func planSha256(value string) bool {
	return strings.HasPrefix(value, "sha256:") && rootCanonicalHash("0x"+strings.TrimPrefix(value, "sha256:"))
}

// Recompute all locally provable commitments, including artifact bytes and
// digest-to-raw-RLP linkage. This does not prove RPC finality or storage roots.
func validatePlanSnapshot(snapshot finalizedSnapshotEnvelope) error {
	sealed, err := sealFinalizedSnapshot(snapshot.finalizedSnapshot)
	if err != nil || sealed.ContentHash != snapshot.ContentHash {
		return errors.Join(errors.New("finalized snapshot seal or exact shared scope differs"), err)
	}
	identity := snapshot.Runtime.Identity
	version := snapshot.Runtime.Version
	if identity.Schema != identitySchema || identity.NativeChain == "" || identity.ObservedAt == "" || identity.RpcUrl == "" ||
		!rootCanonicalHash(identity.GenesisHash) || version.SpecName == "" || version.SpecVersion == 0 ||
		identity.RuntimeSpec != uint64(version.SpecVersion) || identity.RuntimeTx != uint64(version.TransactionVersion) {
		return errors.New("snapshot identity or full runtime tuple is incomplete or contradictory")
	}
	for _, artifact := range []struct {
		label string
		hex   string
		hash  string
		limit int
	}{
		{label: "runtime code", hex: snapshot.Runtime.CodeHex, hash: snapshot.Runtime.CodeHash, limit: maximumRuntimeSnapshotCodeBytes},
		{label: "runtime metadata", hex: snapshot.Runtime.MetadataHex, hash: snapshot.Runtime.MetadataHash, limit: maximumRuntimeSnapshotMetadataBytes},
	} {
		raw, canonical, err := decodeRuntimeSnapshotHex(artifact.label, artifact.hex, artifact.limit)
		if err != nil || canonical != artifact.hex {
			return errors.New("snapshot artifact bytes are malformed or noncanonical")
		}
		digest := blake2b.Sum256(raw)
		if "0x"+hex.EncodeToString(digest[:]) != artifact.hash {
			return errors.New("snapshot artifact bytes do not reproduce their retained hash")
		}
	}
	if snapshot.Mapping.SourceCommit != frontierMappingSourceCommit {
		return errors.New("snapshot mapping codec source is outside the reviewed profile")
	}
	postLog, err := finalizedFrontierPostLog(snapshot.Mapping.NativeHeader)
	if err != nil || !reflect.DeepEqual(postLog, snapshot.Mapping.PostLog) {
		return errors.Join(errors.New("snapshot retained Frontier digest differs"), err)
	}
	header, err := authenticateMappedEvmHeader(snapshot.Mapping.EvmHeader.HeaderRlp, postLog.BlockHash)
	if err != nil || header != snapshot.Mapping.EvmHeader || header.Hash != snapshot.Mapping.EvmCanonicalHash {
		return errors.Join(errors.New("snapshot raw EVM commitment differs"), err)
	}
	return nil
}

// Source seals bind clean-Git claims, not rebuilt artifacts or current source
// trees. Validate structure and internal references without running Git.
func validatePlanSourceLock(lock sourceLock) error {
	if lock.Schema != sourceLockSchema || lock.Scope != "clean-git-sources-and-local-go-replacements-only" ||
		lock.Module != "github.com/urfoundation/sn/v2026" || lock.GoVersion == "" || len(lock.Repositories) == 0 ||
		len(lock.Repositories) > 128 || len(lock.LocalReplacements) > 128 || !rootCanonicalHash(lock.ToolSha256) ||
		!rootCanonicalHash(lock.GoModSha256) || !rootCanonicalHash(lock.GoSumSha256) {
		return errors.New("source lock is incomplete or unsupported")
	}
	want := lock.ContentHash
	lock.ContentHash = ""
	raw, err := json.Marshal(lock)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(append([]byte(sourceLockSchema+"\x00"), raw...))
	if "0x"+hex.EncodeToString(digest[:]) != want {
		return errors.New("source lock content hash differs")
	}
	repositoryKVs := map[string]bool{}
	for _, repository := range lock.Repositories {
		commit, err := hex.DecodeString(repository.Commit)
		if repository.Path == "" || repositoryKVs[repository.Path] || err != nil || len(commit) != 20 ||
			repository.Commit != strings.ToLower(repository.Commit) || repository.Commit == strings.Repeat("0", 40) {
			return errors.New("source lock repository identity is malformed or duplicated")
		}
		repositoryKVs[repository.Path] = true
	}
	if !repositoryKVs["."] {
		return errors.New("source lock has no SN repository identity")
	}
	moduleKVs := map[string]bool{}
	for _, replacement := range lock.LocalReplacements {
		if replacement.Module == "" || replacement.LocalPath == "" || moduleKVs[replacement.Module] || !repositoryKVs[replacement.Repository] {
			return errors.New("source lock replacement is incomplete, duplicated or outside its repositories")
		}
		moduleKVs[replacement.Module] = true
	}
	return nil
}

// Required fields inside the shared runtime tuple cannot disappear merely
// because Go's zero value can represent a valid stateVersion.
func validatePlanRuntimeJson(raw []byte, path ...string) error {
	for _, key := range path {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil || len(object[key]) == 0 {
			return errors.New("plan runtime identity is absent")
		}
		raw = object[key]
	}
	_, err := crv4.DecodeRuntimeVersionIdentity(raw)
	return err
}

// The only I/O occurs before pure graph construction; supplemental inputs
// remain unvalidated even when their exact file bytes have been retained.
func loadBootstrapPlan(ctx context.Context, configPath string) (bootstrapPlan, error) {
	configRaw, configHash, err := readPlanFile(ctx, configPath, maximumPlanManifestBytes)
	if err != nil {
		return bootstrapPlan{}, err
	}
	var config bootstrapPlanConfig
	if err := decodePlanJson(configRaw, &config); err != nil {
		return bootstrapPlan{}, err
	}
	var snapshot finalizedSnapshotEnvelope
	snapshotRaw, err := readPlanReference(ctx, configPath, config.Snapshot, maximumPlanSnapshotBytes)
	if err != nil {
		return bootstrapPlan{}, fmt.Errorf("snapshot input: %w", err)
	}
	if err := decodePlanJson(snapshotRaw, &snapshot); err != nil {
		return bootstrapPlan{}, err
	}
	for _, key := range []string{"runtime", "mapping"} {
		if err := validatePlanRuntimeJson(snapshotRaw, key, "runtime_version"); err != nil {
			return bootstrapPlan{}, err
		}
	}
	var lock sourceLock
	lockRaw, err := readPlanReference(ctx, configPath, config.SourceLock, maximumPlanManifestBytes)
	if err != nil {
		return bootstrapPlan{}, fmt.Errorf("source-lock input: %w", err)
	}
	if err := decodePlanJson(lockRaw, &lock); err != nil {
		return bootstrapPlan{}, err
	}
	var release bootstrapReleaseInput
	releaseRaw, err := readPlanReference(ctx, configPath, config.Release, maximumPlanManifestBytes)
	if err != nil {
		return bootstrapPlan{}, fmt.Errorf("release input: %w", err)
	}
	if err := decodePlanJson(releaseRaw, &release); err != nil {
		return bootstrapPlan{}, err
	}
	if err := validatePlanRuntimeJson(releaseRaw, "runtime_version"); err != nil {
		return bootstrapPlan{}, err
	}
	if len(release.ReviewInputs) > maximumPlanReviewInputs {
		return bootstrapPlan{}, errors.New("release review input count exceeds 32")
	}
	inputs := []planInputBinding{
		{Kind: "config", Sha256: configHash},
		{Kind: "finalized-snapshot", Sha256: config.Snapshot.Sha256, ContentHash: snapshot.ContentHash},
		{Kind: "source-lock", Sha256: config.SourceLock.Sha256, ContentHash: lock.ContentHash},
		{Kind: "release-input", Sha256: config.Release.Sha256},
	}
	// Sort copied references, preserving release bytes and their exact input hash.
	reviewInputs := append([]planReviewInput(nil), release.ReviewInputs...)
	sort.Slice(reviewInputs, func(i, j int) bool { return reviewInputs[i].Requirement < reviewInputs[j].Requirement })
	releasePath := config.Release.Path
	if !filepath.IsAbs(releasePath) {
		releasePath = filepath.Join(filepath.Dir(configPath), releasePath)
	}
	for _, input := range reviewInputs {
		if _, err := readPlanReference(ctx, releasePath, input.planFileReference, maximumPlanManifestBytes); err != nil {
			return bootstrapPlan{}, fmt.Errorf("release review %q: %w", input.Requirement, err)
		}
		inputs = append(inputs, planInputBinding{Kind: "review:" + input.Requirement, Sha256: input.Sha256})
	}
	if err := ctx.Err(); err != nil {
		return bootstrapPlan{}, err
	}
	return buildBootstrapPlan(config, snapshot, lock, release, inputs)
}

// An outline is explicitly unbound: it helps collect missing inputs before
// Snow mainnet identity exists, without relabeling testnet evidence as mainnet.
func bootstrapPlanOutline() bootstrapPlan {
	requirements := bootstrapRequirements()
	for index := range requirements {
		requirements[index].Status = "missing"
	}
	actions := bootstrapActions()
	for index := range actions {
		actions[index].Status = "blocked"
	}
	return bootstrapPlan{Schema: bootstrapPlanSchema, Status: "unbound_outline", Netuid: 25, Requirements: requirements, Actions: actions,
		Economics:         planEconomics{Denominator: "native_miner_allocation_before_withholding", ProviderNumerator: 1, FractionDenominator: 10, RemainderNumerator: 9, Remainder: "owner-recycle", Assurance: "observed-native-target"},
		ExecutionBlockers: []string{"Supply independently approved mainnet chain/genesis/EVM964 and exact snapshot/source-lock/release inputs before building a bound review", "This review hash cannot authorize the separate executable local-custody phase; chain actions still need their semantic adapters and independent bounded authority"}}
}

// Exit zero means review output was produced, never that launch is ready.
// Identity mismatch returns 3; invalid input returns 2 without partial output.
func runPlanCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "strict JSON plan config with exact local input hashes")
	outline := flags.Bool("outline", false, "emit an unbound blocked graph without reading inputs")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || (*outline == (*configPath != "")) {
		fmt.Fprintln(stderr, "plan requires exactly one of --outline or --config FILE; output is a blocked review, never apply authority")
		return 2
	}
	if ctx == nil || ctx.Err() != nil {
		fmt.Fprintln(stderr, "plan context is unavailable or canceled")
		return 2
	}
	plan := bootstrapPlanOutline()
	if !*outline {
		var err error
		plan, err = loadBootstrapPlan(ctx, *configPath)
		if err != nil {
			fmt.Fprintln(stderr, "plan:", err)
			if errors.Is(err, errRpcIdentityMismatch) {
				return 3
			}
			return 2
		}
	}
	if err := json.NewEncoder(stdout).Encode(plan); err != nil {
		fmt.Fprintln(stderr, "plan output:", err)
		return 1
	}
	return 0
}
