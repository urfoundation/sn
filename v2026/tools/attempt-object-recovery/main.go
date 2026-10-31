//go:build linux || darwin

// Rebuilds existing public attempt objects into a fresh local staging directory.
// Every input is public and hash-pinned; publication and validator lifecycle
// remain separate operator actions. No private key or journal path is accepted.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/sdk/v2026"
	"gopkg.in/yaml.v3"
)

// The success receipt covers only fully closed, bounded, content-addressed files.
type stagedObject struct {
	Kind  string `json:"kind"`
	Path  string `json:"path"`
	Hash  string `json:"hash"`
	Bytes uint64 `json:"bytes"`
}

// Fresh staging owns its directory descriptor and every unique file allowance.
type objectStaging struct {
	root           *os.Root
	maximumBytes   uint64
	maximumObjects uint64
	usedBytes      uint64
	objectKVs      map[string]stagedObject
}

// Reads only a named regular public document whose complete bytes match its pin.
func readPinnedPublicFile(path, hash string, maximum uint64) (result []byte, resultErr error) {
	if len(hash) != 64 || hash != strings.ToLower(hash) || maximum == 0 || maximum >= uint64(1<<63-1) {
		return nil, errors.New("public input pin or bound is incomplete")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || uint64(info.Size()) > maximum {
		return nil, errors.Join(errors.New("public input is missing, aliased or outside its bound"), err)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
		if resultErr != nil {
			result = nil
		}
	}()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.Join(errors.New("public input changed during open"), err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil || uint64(len(raw)) > maximum || fmt.Sprintf("%x", sha256.Sum256(raw)) != hash {
		return nil, errors.Join(errors.New("public input differs from its exact content pin"), err)
	}
	return raw, nil
}

// Typed canonical names cannot traverse or alias another object namespace.
func stagedObjectPath(kind, hash string) (string, error) {
	if kind != "metadata" && kind != "records" && kind != "proofs" || len(hash) != 66 || !strings.HasPrefix(hash, "0x") || hash != strings.ToLower(hash) {
		return "", errors.New("staged object identity is invalid")
	}
	for _, digit := range hash[2:] {
		if !strings.ContainsRune("0123456789abcdef", digit) {
			return "", errors.New("staged object hash is invalid")
		}
	}
	extension := ".jsonl"
	if kind == "metadata" {
		extension = ".json"
	}
	return kind + "/" + hash[2:] + extension, nil
}

// An existing staged object is still re-read, hashed and length-checked.
func (self *objectStaging) write(ctx context.Context, kind, hash string, raw []byte) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	name, err := stagedObjectPath(kind, hash)
	if err != nil {
		return err
	}
	if len(raw) == 0 || fmt.Sprintf("0x%x", sha256.Sum256(raw)) != hash {
		return errors.New("staged bytes differ from their public identity")
	}
	if prior, exists := self.objectKVs[name]; exists {
		if prior.Bytes != uint64(len(raw)) {
			return errors.New("staged immutable object changed length")
		}
		_, err := self.read(ctx, kind, hash, uint64(len(raw)))
		return err
	}
	if uint64(len(self.objectKVs)) >= self.maximumObjects || uint64(len(raw)) > self.maximumBytes-self.usedBytes {
		return errors.New("public recovery staging exceeds its independently admitted graph")
	}
	file, err := self.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close(), ctx.Err()) }()
	if count, err := file.Write(raw); err != nil || count != len(raw) {
		return errors.Join(io.ErrShortWrite, err)
	}
	if err := file.Sync(); err != nil {
		return err
	}
	self.objectKVs[name] = stagedObject{Kind: kind, Path: name, Hash: hash, Bytes: uint64(len(raw))}
	self.usedBytes += uint64(len(raw))
	return nil
}

// A returned object reader has already passed exact regular-file/hash checks.
func (self *objectStaging) read(ctx context.Context, kind, hash string, size uint64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, err := stagedObjectPath(kind, hash)
	if err != nil {
		return nil, err
	}
	object, exists := self.objectKVs[name]
	if !exists || object.Bytes != size {
		return nil, errors.New("staged public object is absent or resized")
	}
	info, err := self.root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || uint64(info.Size()) != size {
		return nil, errors.Join(errors.New("staged public object is aliased or resized"), err)
	}
	file, err := self.root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	opened, statErr := file.Stat()
	raw, readErr := io.ReadAll(io.LimitReader(file, int64(size)+1))
	if err := errors.Join(statErr, readErr, file.Close(), ctx.Err()); err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) || uint64(len(raw)) != size || fmt.Sprintf("0x%x", sha256.Sum256(raw)) != hash {
		return nil, errors.New("staged object changed its exact identity")
	}
	return raw, nil
}

// Process options carry all task-specific limits; no daemon settings change.
func main() {
	configPath := flag.String("config", "", "reviewed public validator config")
	configHash := flag.String("config-sha256", "", "exact config SHA-256 without prefix")
	artifactPath := flag.String("artifact", "", "retained public measurement artifact")
	artifactHash := flag.String("artifact-sha256", "", "exact public artifact SHA-256 without prefix")
	keysPath := flag.String("server-keys", "", "captured public /verify/keys JSON")
	keysHash := flag.String("server-keys-sha256", "", "exact public key response SHA-256 without prefix")
	target := flag.String("records-manifest", "", "original signed records manifest hash")
	output := flag.String("output", "", "fresh local output directory")
	scratch := flag.String("scratch", "", "fresh local scratch directory")
	timeout := flag.Duration("timeout", 30*time.Minute, "complete stopped recovery deadline")
	flag.Parse()
	if err := run(*configPath, *configHash, *artifactPath, *artifactHash, *keysPath, *keysHash, *target, *output, *scratch, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Pinned documents supply only existing public authority and unchanged limits.
func run(configPath, configHash, artifactPath, artifactHash, keysPath, keysHash, target, output, scratch string, timeout time.Duration) (resultErr error) {
	if timeout <= 0 || !filepath.IsAbs(output) || filepath.Clean(output) != output || !filepath.IsAbs(scratch) || filepath.Clean(scratch) != scratch || output == scratch || strings.HasPrefix(output, scratch+string(filepath.Separator)) || strings.HasPrefix(scratch, output+string(filepath.Separator)) {
		return errors.New("fresh independent staging paths and a deadline are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	configRaw, err := readPinnedPublicFile(configPath, configHash, 2*1024*1024)
	if err != nil {
		return err
	}
	var cfg validator.ReleaseConfig
	decoder := yaml.NewDecoder(bytes.NewReader(configRaw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return err
	}
	artifactRaw, err := readPinnedPublicFile(artifactPath, artifactHash, cfg.EvidenceV2.Bounds.MaxArtifactBytes)
	if err != nil {
		return err
	}
	var artifact validator.ReleaseMeasurementArtifact
	if err := json.Unmarshal(artifactRaw, &artifact); err != nil {
		return err
	}
	if artifact.DeploymentID != cfg.DeploymentID || artifact.ChainID != cfg.ChainID || artifact.GenesisHash != cfg.GenesisHash || artifact.Coordinator != cfg.Coordinator || artifact.ValidatorID != cfg.ValidatorID || artifact.Netuid != cfg.Netuid {
		return errors.New("public artifact differs from the reviewed deployment")
	}
	var cuts []*validator.AttemptCutV2
	for _, input := range artifact.Inputs {
		if input.AttemptCutV2 != nil {
			cuts = append(cuts, input.AttemptCutV2)
		}
	}
	if artifact.SettlementClosureV2 != nil {
		for _, transition := range artifact.SettlementClosureV2.Transitions {
			if transition != nil {
				cuts = append(cuts, &transition.Cut)
			}
		}
	}
	var cut *validator.AttemptCutV2
	for _, candidate := range cuts {
		if candidate.Records.ManifestHash == target {
			if cut != nil {
				left, _ := json.Marshal(cut)
				right, _ := json.Marshal(candidate)
				if !bytes.Equal(left, right) {
					return errors.New("public artifact has conflicting original cuts")
				}
			}
			cut = candidate
		}
	}
	if cut == nil {
		return errors.New("pinned public artifact does not contain the requested original cut")
	}
	var operator *validator.OperatorConfig
	for index := range cfg.Operators {
		if cfg.Operators[index].NoID == cut.Context.Identity.NoID {
			if operator != nil {
				return errors.New("configured operator is duplicated")
			}
			operator = &cfg.Operators[index]
		}
	}
	if operator == nil || !filepath.IsAbs(operator.StateDir) || cut.Context.Identity.DeploymentID != cfg.DeploymentID || cut.Context.Identity.ValidatorID != cfg.ValidatorID || cut.Context.Identity.ChainID != cfg.ChainID || cut.Context.Identity.GenesisHash != cfg.GenesisHash || cut.Context.Identity.Netuid != cfg.Netuid {
		return errors.New("original cut differs from the configured ledger owner")
	}
	policy, err := validator.ReleasePolicyForHash(&cfg, fmt.Sprintf("0x%x", cut.Context.Activation.Domain.PolicyHash))
	if err != nil {
		return err
	}
	if err := cut.VerifyHeader(cut.Context, cfg.EvidenceV2.Bounds.Cut); err != nil {
		return err
	}
	keyRaw, err := readPinnedPublicFile(keysPath, keysHash, cfg.EvidenceV2.Bounds.MaxControlBytes)
	if err != nil {
		return err
	}
	var keyResult sdk.VerifyKeysResult
	if err := json.Unmarshal(keyRaw, &keyResult); err != nil {
		return err
	}
	if len(keyResult.Keys) == 0 || len(keyResult.Keys) > 256 {
		return errors.New("public verify key census is invalid")
	}
	keys := map[byte]ed25519.PublicKey{}
	for _, key := range keyResult.Keys {
		if key == nil || key.ServerKeyId < 0 || key.ServerKeyId > 255 || len(key.PublicKey) != ed25519.PublicKeySize || keys[byte(key.ServerKeyId)] != nil {
			return errors.New("public verify keys are duplicated or malformed")
		}
		keys[byte(key.ServerKeyId)] = bytes.Clone(key.PublicKey)
	}
	maximumBytes := uint64(0)
	add := func(value uint64) error {
		if value > ^uint64(0)-maximumBytes {
			return errors.New("recovery graph byte allowance overflows")
		}
		maximumBytes += value
		return nil
	}
	for _, value := range []uint64{cut.Records.DataBytes, cut.Proofs.DataBytes, cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes, cfg.EvidenceV2.Bounds.Cut.Records.MaxManifestBytes, cfg.EvidenceV2.Bounds.Cut.Proofs.MaxManifestBytes} {
		if err := add(value); err != nil {
			return err
		}
	}
	for _, stream := range []struct {
		pages   uint64
		maximum uint64
	}{{pages: cut.Records.PageCount, maximum: cfg.EvidenceV2.Bounds.Cut.Records.MaxPageBytes}, {pages: cut.Proofs.PageCount, maximum: cfg.EvidenceV2.Bounds.Cut.Proofs.MaxPageBytes}} {
		if stream.pages != 0 && stream.maximum > ^uint64(0)/stream.pages {
			return errors.New("recovery graph page allowance overflows")
		}
		if err := add(stream.pages * stream.maximum); err != nil {
			return err
		}
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		return err
	}
	root, err := os.OpenRoot(output)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	for _, kind := range []string{"metadata", "records", "proofs"} {
		if err := root.Mkdir(kind, 0o700); err != nil {
			return err
		}
	}
	staging := &objectStaging{root: root, maximumBytes: maximumBytes, maximumObjects: cut.Records.ChunkCount + cut.Proofs.ChunkCount + cut.Records.PageCount + cut.Proofs.PageCount + 3, objectKVs: map[string]stagedObject{}}
	write := func(kind string) validator.AttemptStreamV2ObjectWriter {
		return func(ctx context.Context, hash string, raw []byte) error { return staging.write(ctx, kind, hash, raw) }
	}
	result, err := validator.RecoverStoppedAttemptCutV2Objects(ctx, operator.StateDir, *cut, cut.Context, policy, cfg.EvidenceV2.Bounds.Cut, validator.AttemptCutV2RecoveryOptions{LedgerLimits: cfg.EvidenceV2.Bounds.Disk, ReplayBounds: cfg.EvidenceV2.Bounds.Replay, ScratchDirectory: scratch, ServerKeys: keys, WriteRecords: write("records"), WriteProofs: write("proofs"), WriteMetadata: write("metadata"), ReadMetadata: func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		return staging.read(ctx, "metadata", hash, size)
	}, OpenData: func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		raw, err := staging.read(ctx, kind, hash, size)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(raw)), nil
	}})
	if err != nil {
		return err
	}
	cutRaw, err := cut.CanonicalJSON(cfg.EvidenceV2.Bounds.Cut)
	if err != nil {
		return err
	}
	if err := staging.write(ctx, "metadata", fmt.Sprintf("0x%x", sha256.Sum256(cutRaw)), cutRaw); err != nil {
		return err
	}
	files := make([]stagedObject, 0, len(staging.objectKVs))
	for _, object := range staging.objectKVs {
		if _, err := staging.read(ctx, object.Kind, object.Hash, object.Bytes); err != nil {
			return err
		}
		files = append(files, object)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	receipt := map[string]any{"schema": "urnetwork-attempt-object-recovery-v1", "config_sha256": configHash, "public_artifact_sha256": artifactHash, "public_keys_sha256": keysHash, "records": cut.Records, "proofs": cut.Proofs, "original_cut_sha256": fmt.Sprintf("%x", sha256.Sum256(cutRaw)), "replay": result, "files": files, "objects": len(files), "bytes": staging.usedBytes, "object_store_mutations": false, "source_mutations": false}
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	file, err := root.OpenFile("complete.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	count, writeErr := file.Write(raw)
	syncErr := file.Sync()
	closeErr := file.Close()
	if count != len(raw) {
		writeErr = errors.Join(writeErr, io.ErrShortWrite)
	}
	if err := errors.Join(writeErr, syncErr, closeErr, ctx.Err()); err != nil {
		return err
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	if err := errors.Join(directory.Sync(), directory.Close(), ctx.Err()); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"complete": true, "output": output, "objects": len(files), "bytes": staging.usedBytes, "records": cut.Records, "proofs": cut.Proofs, "source_mutations": false, "object_store_mutations": false})
}
