// Exact prior signed carriers already exist in both retained public stores.
// Their typed census avoids wrapping original base64 data in a second bundle.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urnetwork/server/v2026"
	"github.com/urnetwork/server/v2026/startifact"
)

// Route hash and complete-wire identity remain different authorities. The
// suffix/hash preserve the producer's exact distinct local staged bytes.
type FinalCollectedPriorCarrierV2 struct {
	Scope        string `json:"scope"`
	Path         string `json:"path"`
	EnvelopeHash string `json:"envelope_hash"`
	WireHash     string `json:"wire_sha256"`
	WireBytes    uint64 `json:"wire_bytes"`
	LocalHash    string `json:"local_sha256"`
	LocalBytes   uint64 `json:"local_bytes"`
	LocalSuffix  []byte `json:"local_suffix"`
}

// The caller supplies independently configured origins and a bounded reader.
type finalPriorCarrierReaderV2 func(context.Context, string, string, uint64) ([]byte, error)

// Canonical local names are reconstructed, never supplied as exclusion paths.
func finalPriorCarrierLocalPathV2(runId, scope, path string) (string, error) {
	if runId == "" || filepath.Base(runId) != runId || strings.ContainsAny(runId, "/\\\r\n\x00") || scope != "run" && scope != "reference" {
		return "", errors.New("prior carrier routing is invalid")
	}
	if err := validateCampaignEvidencePath(path); err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(scope + "\x00" + path))
	directory := "files"
	if scope == "reference" {
		directory = "references"
	}
	return filepath.ToSlash(filepath.Join("public", campaignEvidenceLocalArchiveDirectory, runId, directory, hex.EncodeToString(digest[:])+".evidence.json")), nil
}

// No trim/normalization can turn different original bytes into a local match.
func finalPriorCarrierLocalHashV2(wire, suffix []byte) (string, error) {
	if !bytes.Equal(suffix, []byte{'\n'}) {
		return "", errors.New("prior carrier local suffix differs from the original producer")
	}
	hash := sha256.New()
	_, _ = hash.Write(wire)
	_, _ = hash.Write(suffix)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

// A retained signed manifest defines the complete file/reference census.
func finalPriorCarrierEntriesV2(manifest *campaignEvidenceManifestPayload, limits campaignEvidenceLimits) (map[string]campaignEvidenceFileEntry, []string, error) {
	if manifest == nil {
		return nil, nil, errors.New("prior carrier manifest is absent")
	}
	if _, err := campaignEvidenceManifestFilesWithLimits(manifest.Files, limits); err != nil {
		return nil, nil, err
	}
	if _, err := campaignEvidenceEntryFilesWithLimits(manifest.References, limits); err != nil {
		return nil, nil, err
	}
	if uint64(len(manifest.Files))+uint64(len(manifest.References)) > limits.maximumObjects {
		return nil, nil, errors.New("prior carrier census exceeds its configured object bound")
	}
	entries := map[string]campaignEvidenceFileEntry{}
	for _, group := range []struct {
		scope string
		files []campaignEvidenceFileEntry
	}{{scope: "run", files: manifest.Files}, {scope: "reference", files: manifest.References}} {
		for _, entry := range group.files {
			key := group.scope + "\x00" + entry.Path
			if _, exists := entries[key]; exists {
				return nil, nil, errors.New("prior carrier manifest repeats a source")
			}
			entries[key] = entry
		}
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return entries, keys, nil
}

// A correct body cannot excuse a swapped route's signed-envelope hash.
func verifyFinalPriorCarrierWireV2(cfg *ResolvedConfig, runId, scope string, entry campaignEvidenceFileEntry, owner common.Address, wire []byte) error {
	return verifyFinalPriorCarrierWireWithObserverV2(cfg, runId, scope, entry, owner, wire, nil)
}

// A per-call work observer reports the actual algorithm choice, not a
// supplied verification verdict; production always leaves it absent.
func verifyFinalPriorCarrierWireWithObserverV2(cfg *ResolvedConfig, runId, scope string, entry campaignEvidenceFileEntry, owner common.Address, wire []byte, observed func(bool)) error {
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || owner == (common.Address{}) {
		return errors.New("prior carrier owner is absent")
	}
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return err
	}
	limit, err := limits.fileEnvelopeBytes(entry.Path, entry.Size)
	if err != nil || len(wire) == 0 || uint64(len(wire)) > uint64(limit) {
		return errors.Join(errors.New("prior carrier exceeds its signed source-size bound"), err)
	}
	envelope, err := decodeFinalPriorCarrierEnvelopeV2(wire)
	if err != nil {
		return err
	}
	if err := validateReleaseEvidenceIdentity(&envelope); err != nil {
		return err
	}
	canonical, err := canonicalFinalPriorFilePayloadV2(limits, runId, scope, entry, envelope.Payload)
	if err != nil {
		return err
	}
	if observed != nil {
		observed(canonical != nil)
	}
	if canonical != nil {
		// This invocation proved the complete payload grammar and exact
		// original source. Still hash it afresh and recover its signature.
		digest, err := evidenceDigestFromCanonicalPayload(&envelope, canonical)
		if err != nil {
			return err
		}
		if err := verifyEvidenceSignature(&envelope, digest, nil); err != nil {
			return err
		}
	} else if err := verifyEvidence(&envelope, nil); err != nil {
		return err
	}
	if envelope.Signer != owner || envelope.ContentHash != entry.EnvelopeHash || envelope.Kind != campaignEvidenceFileKind || envelope.RunID != runId || envelope.DeploymentID != cfg.Config.Deployment.DeploymentID || envelope.ChainID != cfg.ChainID || envelope.Netuid != cfg.Netuid || !strings.EqualFold(envelope.GenesisHash, cfg.Public.Chain.GenesisHash) {
		return errors.New("prior carrier signed route/deployment/owner differs")
	}
	if canonical != nil {
		err = verifyFinalCanonicalPriorWireV2(&envelope, canonical, wire)
	} else {
		if err := verifyFinalPriorCarrierFilePayloadV2(limits, runId, scope, entry, envelope.Payload); err != nil {
			return err
		}
		err = verifyValidatedEvidenceWire(&envelope, wire)
	}
	if err != nil {
		return errors.Join(errors.New("prior carrier wire differs from the original canonical publisher"), err)
	}
	return nil
}

// Origins come from resolved launch inputs, never from a carrier or filename.
func finalPriorCarrierOriginsV2(cfg *ResolvedConfig) ([2]string, error) {
	var result [2]string
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || len(cfg.OperatorAPIOrigins) != 2 || cfg.Config.Topology.Operators != 2 {
		return result, errors.New("prior carrier requires the complete configured origin pair")
	}
	origins, err := validateOperatorAPIOrigins(cfg.OperatorAPIOrigins, 2, cfg.ChainID, cfg.Public.Chain.GenesisHash, cfg.Config.Deployment.Network)
	if err != nil {
		return result, err
	}
	for index := range result {
		if origins[index] != cfg.OperatorAPIOrigins[index] {
			return result, errors.New("prior carrier origin is not the exact canonical launch origin")
		}
		result[index] = origins[index]
	}
	return result, nil
}

// Shape checks bind every row to the independently authenticated manifest.
// Wire bytes are authenticated separately at both independently given origins.
func verifyFinalPriorCarrierCensusV2(cfg *ResolvedConfig, runId string, manifest *campaignEvidenceManifestPayload, origins []string, carriers []FinalCollectedPriorCarrierV2) error {
	expectedOrigins, err := finalPriorCarrierOriginsV2(cfg)
	if err != nil || len(origins) != 2 || origins[0] != expectedOrigins[0] || origins[1] != expectedOrigins[1] {
		return errors.Join(errors.New("prior carrier origins differ from the approved launch"), err)
	}
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return err
	}
	entries, keys, err := finalPriorCarrierEntriesV2(manifest, limits)
	if err != nil {
		return err
	}
	if manifest.RunID != runId || len(carriers) == 0 || len(carriers) != len(keys) {
		return errors.New("prior carrier census is not the complete signed manifest")
	}
	for index, carrier := range carriers {
		key := carrier.Scope + "\x00" + carrier.Path
		entry := entries[key]
		limit, err := limits.fileEnvelopeBytes(entry.Path, entry.Size)
		localBytes, ok := checkedAdd(carrier.WireBytes, 1)
		if err != nil || keys[index] != key || carrier.EnvelopeHash != entry.EnvelopeHash || !validSHA256ContentHash(carrier.WireHash) || !validSHA256ContentHash(carrier.LocalHash) || carrier.WireBytes == 0 || carrier.WireBytes > uint64(limit) || !ok || carrier.LocalBytes != localBytes || !bytes.Equal(carrier.LocalSuffix, []byte{'\n'}) {
			return errors.Join(errors.New("prior carrier identity, order, size or exact local suffix differs"), err)
		}
		if _, err := finalPriorCarrierLocalPathV2(runId, carrier.Scope, carrier.Path); err != nil {
			return err
		}
	}
	return nil
}

// Read one carrier at a time. Matching payload bytes cannot replace signed
// route identity, exact wire identity or the other retained public replica.
func verifyFinalPriorCarriersV2(ctx context.Context, cfg *ResolvedConfig, runId string, manifest *campaignEvidenceManifestPayload, origins []string, carriers []FinalCollectedPriorCarrierV2, owner common.Address, read finalPriorCarrierReaderV2) error {
	if ctx == nil || read == nil {
		return errors.New("prior carrier read owner is absent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyFinalPriorCarrierCensusV2(cfg, runId, manifest, origins, carriers); err != nil {
		return err
	}
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return err
	}
	entries, _, err := finalPriorCarrierEntriesV2(manifest, limits)
	if err != nil {
		return err
	}
	for _, carrier := range carriers {
		entry := entries[carrier.Scope+"\x00"+carrier.Path]
		for _, origin := range origins {
			if err := ctx.Err(); err != nil {
				return err
			}
			wire, err := read(ctx, origin, carrier.EnvelopeHash, carrier.WireBytes)
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if uint64(len(wire)) != carrier.WireBytes || bytesSHA256(wire) != carrier.WireHash {
				return errors.New("prior carrier published wire differs")
			}
			if err := verifyFinalPriorCarrierWireV2(cfg, runId, carrier.Scope, entry, owner, wire); err != nil {
				return err
			}
			localHash, err := finalPriorCarrierLocalHashV2(wire, carrier.LocalSuffix)
			if err != nil || localHash != carrier.LocalHash {
				return errors.Join(errors.New("prior carrier exact original local bytes differ"), err)
			}
		}
	}
	return nil
}

// The live capture reads both existing content/history objects through the
// same independently rendered isolated stores used by the actual publisher.
func finalPriorCarrierStoreReaderV2(cfg *ResolvedConfig, stateRoot, runId string, stores scenarioCompletionStoreFactory) (finalPriorCarrierReaderV2, error) {
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return nil, err
	}
	origins, err := finalPriorCarrierOriginsV2(cfg)
	if err != nil {
		return nil, err
	}
	if err := verifyRuntimeBlobConfigManifest(cfg, stateRoot); err != nil {
		return nil, err
	}
	if stores == nil {
		stores = func(operator int) (server.BlobStore, error) {
			return renderedOperatorEvidenceStore(cfg, stateRoot, operator)
		}
	}
	originStores := map[string]server.BlobStore{}
	for index, origin := range origins {
		store, err := stores(index + 1)
		if err != nil {
			return nil, err
		}
		prefix, err := operatorArtifactPrefix(cfg.Config, index+1)
		if err != nil || store == nil || store.Prefix() != prefix {
			return nil, errors.Join(errors.New("prior carrier store is outside its approved operator namespace"), err)
		}
		originStores[origin] = store
	}
	return func(ctx context.Context, origin, hash string, maximumBytes uint64) ([]byte, error) {
		if ctx == nil {
			return nil, errors.New("prior carrier store read has no context")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		store := originStores[origin]
		if store == nil || !validSHA256ContentHash(hash) || maximumBytes == 0 || maximumBytes > limits.maximumEnvelopeBytes() {
			return nil, errors.New("prior carrier store read is outside its approved identity or bound")
		}
		contentKey, err := startifact.EvidenceContentKey(store, hash)
		if err != nil {
			return nil, err
		}
		historyKey, err := startifact.EvidenceHistoryKey(store, cfg.Config.Deployment.DeploymentID, cfg.Netuid, campaignEvidenceFileKind, evidenceHistoryStorageRunID(runId), hash)
		if err != nil {
			return nil, err
		}
		var first []byte
		for _, key := range []string{contentKey, historyKey} {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			reader, err := store.Get(ctx, key)
			if err != nil {
				return nil, err
			}
			raw, readErr := io.ReadAll(io.LimitReader(reader, int64(maximumBytes)+1))
			err = errors.Join(readErr, reader.Close(), ctx.Err())
			if err != nil {
				return nil, err
			}
			if uint64(len(raw)) != maximumBytes {
				return nil, errors.New("prior carrier retained object differs from its exact declared wire length")
			}
			if first == nil {
				first = raw
			} else if !bytes.Equal(first, raw) {
				return nil, errors.New("prior carrier content and history bytes differ")
			}
		}
		return first, nil
	}, nil
}

// The only suffix admitted is proved against the actual original local file.
// Public originals stay where already retained; only their exact census grows.
func captureFinalPriorCarriersV2(ctx context.Context, cfg *ResolvedConfig, stateRoot, runId string, manifest *campaignEvidenceManifestPayload, owner common.Address, stores scenarioCompletionStoreFactory) ([2]string, []FinalCollectedPriorCarrierV2, error) {
	var none [2]string
	if ctx == nil {
		return none, nil, errors.New("prior carrier capture has no context")
	}
	if manifest == nil || manifest.RunID != runId {
		return none, nil, errors.New("prior carrier manifest does not name the requested original run")
	}
	origins, err := finalPriorCarrierOriginsV2(cfg)
	if err != nil {
		return none, nil, err
	}
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return none, nil, err
	}
	entries, keys, err := finalPriorCarrierEntriesV2(manifest, limits)
	if err != nil {
		return none, nil, err
	}
	read, err := finalPriorCarrierStoreReaderV2(cfg, stateRoot, runId, stores)
	if err != nil {
		return none, nil, err
	}
	carriers := make([]FinalCollectedPriorCarrierV2, 0, len(keys))
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return none, nil, err
		}
		scope, _, _ := strings.Cut(key, "\x00")
		entry := entries[key]
		relative, err := finalPriorCarrierLocalPathV2(runId, scope, entry.Path)
		if err != nil {
			return none, nil, err
		}
		limit, err := limits.fileEnvelopeBytes(entry.Path, entry.Size)
		if err != nil {
			return none, nil, err
		}
		file, err := openFinalCollectedFile(stateRoot, filepath.FromSlash(relative))
		if err != nil {
			return none, nil, err
		}
		before, statErr := file.Stat()
		if statErr != nil || !before.Mode().IsRegular() || before.Size() < 2 || before.Size() > limit+1 {
			_ = file.Close()
			return none, nil, errors.Join(errors.New("prior carrier original file exceeds its exact source-derived bound"), statErr)
		}
		local, readErr := io.ReadAll(io.LimitReader(file, limit+2))
		after, afterErr := file.Stat()
		closeErr := file.Close()
		if err := errors.Join(readErr, afterErr, closeErr, ctx.Err()); err != nil {
			return none, nil, err
		}
		if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || int64(len(local)) != before.Size() || local[len(local)-1] != '\n' {
			return none, nil, errors.New("prior carrier original file changed or has a different suffix")
		}
		wire := local[:len(local)-1]
		if err := verifyFinalPriorCarrierWireV2(cfg, runId, scope, entry, owner, wire); err != nil {
			return none, nil, fmt.Errorf("prior carrier %s: %w", entry.Path, err)
		}
		carrier := FinalCollectedPriorCarrierV2{Scope: scope, Path: entry.Path, EnvelopeHash: entry.EnvelopeHash, WireHash: bytesSHA256(wire), WireBytes: uint64(len(wire)), LocalHash: bytesSHA256(local), LocalBytes: uint64(len(local)), LocalSuffix: []byte{'\n'}}
		for _, origin := range origins {
			got, err := read(ctx, origin, entry.EnvelopeHash, carrier.WireBytes)
			if err != nil {
				return none, nil, err
			}
			if !bytes.Equal(got, wire) {
				return none, nil, errors.New("prior carrier public original differs from its exact locally retained signed bytes")
			}
		}
		carriers = append(carriers, carrier)
	}
	if err := verifyFinalPriorCarrierCensusV2(cfg, runId, manifest, origins[:], carriers); err != nil {
		return none, nil, err
	}
	return origins, carriers, nil
}

// These exact paths are derived only after complete prior-census authentication.
// An unlisted file under the same directory remains an ordinary captured file.
func finalPriorCarrierExcludedPathsV2(prior *FinalCollectedPriorPhaseInputs) (map[string]FinalCollectedPriorCarrierV2, error) {
	result := map[string]FinalCollectedPriorCarrierV2{}
	if prior == nil || prior.SemanticStatus != finalSemanticCapturePendingStatus {
		return result, nil
	}
	if len(prior.PublicCarriers) == 0 {
		return nil, errors.New("pending prior phase has no exact original public carrier census")
	}
	for _, carrier := range prior.PublicCarriers {
		path, err := finalPriorCarrierLocalPathV2(prior.RunID, carrier.Scope, carrier.Path)
		if err != nil {
			return nil, err
		}
		relative := strings.TrimPrefix(path, "public/")
		if _, exists := result[relative]; exists {
			return nil, errors.New("prior carrier census repeats an original local path")
		}
		result[relative] = carrier
	}
	return result, nil
}

// The capture's exact-path substitution refuses a changed original rather
// than letting a once-authenticated directory grant blanket exclusion.
func verifyFinalPriorCarrierLocalFileV2(ctx context.Context, stateRoot, relative string, carrier FinalCollectedPriorCarrierV2) error {
	return verifyFinalPriorCarrierLocalFileWithLimitsV2(ctx, stateRoot, relative, carrier, defaultCampaignEvidenceLimits())
}

// The same real descriptor/hash check handles the exact original larger
// metadata carrier, without admitting any unlisted sibling by prefix.
func verifyFinalPriorCarrierLocalFileWithLimitsV2(ctx context.Context, stateRoot, relative string, carrier FinalCollectedPriorCarrierV2, limits campaignEvidenceLimits) error {
	if err := limits.validate(); err != nil {
		return err
	}
	if ctx == nil || !validSHA256ContentHash(carrier.LocalHash) || carrier.LocalBytes == 0 || carrier.LocalBytes > limits.maximumEnvelopeBytes()+1 {
		return errors.New("prior carrier local read identity is invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateCampaignEvidencePath(relative); err != nil {
		return err
	}
	file, err := openFinalCollectedFile(stateRoot, filepath.FromSlash("public/"+relative))
	if err != nil {
		return err
	}
	before, statErr := file.Stat()
	if statErr != nil || !before.Mode().IsRegular() || before.Size() != int64(carrier.LocalBytes) {
		_ = file.Close()
		return errors.Join(errors.New("prior carrier original size or descriptor differs"), statErr)
	}
	hash := sha256.New()
	count, readErr := io.Copy(hash, io.LimitReader(file, int64(carrier.LocalBytes)+1))
	after, afterErr := file.Stat()
	closeErr := file.Close()
	if err := errors.Join(readErr, afterErr, closeErr, ctx.Err()); err != nil {
		return err
	}
	if count != int64(carrier.LocalBytes) || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != carrier.LocalHash {
		return errors.New("prior carrier original bytes changed before exact-path substitution")
	}
	return nil
}
