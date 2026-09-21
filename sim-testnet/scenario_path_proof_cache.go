package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026"
)

type scenarioPathProofLimits struct {
	maximumBytes  uint64
	maximumProofs uint64
	maximumLine   uint64
}

type scenarioPathProofPrefix struct {
	verifierHash string
	bytes        int64
	hash         [sha256.Size]byte
	proofs       int
	trailIDs     map[connect.Id]bool
}

// A cached prefix still gets hashed from disk on every observation. Only the
// expensive JSON and signature verification is retained, so a changed prefix
// fails closed while an append verifies only its new complete records.
type scenarioPathProofCache struct {
	prefixes map[string]scenarioPathProofPrefix
}

func newScenarioPathProofCache() *scenarioPathProofCache {
	return &scenarioPathProofCache{prefixes: map[string]scenarioPathProofPrefix{}}
}

func configuredScenarioPathProofLimits(cfg *ResolvedConfig, validatorID int) (scenarioPathProofLimits, bool, error) {
	if cfg == nil || cfg.Config == nil {
		return scenarioPathProofLimits{}, false, errors.New("scenario path-proof limits are unavailable")
	}
	for _, configured := range cfg.Config.ValidatorEvidenceV2 {
		if configured.ValidatorID != uint64(validatorID) {
			continue
		}
		disk := configured.Evidence.Bounds.Disk
		maximumLine := disk.MaxRecordBytes
		if configured.Evidence.Bounds.Replay.MaxProofBytes > maximumLine {
			maximumLine = configured.Evidence.Bounds.Replay.MaxProofBytes
		}
		if disk.MaxProofBytes == 0 || disk.MaxTrailCount == 0 || maximumLine == 0 || maximumLine > disk.MaxProofBytes {
			return scenarioPathProofLimits{}, true, errors.New("scenario path-proof bounds are incomplete")
		}
		return scenarioPathProofLimits{maximumBytes: disk.MaxProofBytes, maximumProofs: disk.MaxTrailCount, maximumLine: maximumLine}, true, nil
	}
	return scenarioPathProofLimits{}, false, nil
}

type scenarioPathProofServerKey struct {
	ID  byte   `json:"id"`
	Key string `json:"key"`
}

func scenarioPathProofVerifierHash(vpk ed25519.PublicKey, serverKeys map[byte]ed25519.PublicKey, trailDepth int) (string, error) {
	ids := make([]int, 0, len(serverKeys))
	for id := range serverKeys {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	keys := make([]scenarioPathProofServerKey, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, scenarioPathProofServerKey{ID: byte(id), Key: hex.EncodeToString(serverKeys[byte(id)])})
	}
	return canonicalHashHex(struct {
		VPK        string                       `json:"vpk"`
		ServerKeys []scenarioPathProofServerKey `json:"server_keys"`
		TrailDepth int                          `json:"trail_depth"`
	}{VPK: hex.EncodeToString(vpk), ServerKeys: keys, TrailDepth: trailDepth})
}

func cloneScenarioTrailIDs(source map[connect.Id]bool) map[connect.Id]bool {
	result := make(map[connect.Id]bool, len(source))
	for id := range source {
		result[id] = true
	}
	return result
}

func (cache *scenarioPathProofCache) inspect(ctx context.Context, path, verifierHash string, limits scenarioPathProofLimits, verify func(*validatorpkg.ProofRecord, int) error) (int, error) {
	if ctx == nil || cache == nil || cache.prefixes == nil || !validCanonicalHashHex(verifierHash) || limits.maximumBytes == 0 || limits.maximumProofs == 0 || limits.maximumLine == 0 || verify == nil {
		return 0, errors.New("scenario path-proof cache input is incomplete")
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		if prefix, found := cache.prefixes[path]; found && prefix.bytes != 0 {
			return 0, errors.New("verified path-proof prefix disappeared")
		}
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || uint64(info.Size()) > limits.maximumBytes {
		return 0, fmt.Errorf("path-proof store size %d exceeds its bounded regular-file owner", info.Size())
	}

	prefix, reuse := cache.prefixes[path]
	if !reuse || prefix.verifierHash != verifierHash {
		prefix = scenarioPathProofPrefix{verifierHash: verifierHash, trailIDs: map[connect.Id]bool{}}
	} else if info.Size() < prefix.bytes {
		return 0, errors.New("verified path-proof prefix was truncated")
	}
	hasher := sha256.New()
	if prefix.bytes != 0 {
		if _, err := io.CopyN(hasher, file, prefix.bytes); err != nil {
			return 0, fmt.Errorf("rehash verified path-proof prefix: %w", err)
		}
		var got [sha256.Size]byte
		copy(got[:], hasher.Sum(nil))
		if got != prefix.hash {
			return 0, errors.New("verified path-proof prefix changed before append")
		}
	}

	trailIDs := cloneScenarioTrailIDs(prefix.trailIDs)
	proofs, completedBytes := prefix.proofs, prefix.bytes
	reader := bufio.NewReaderSize(file, int(min(limits.maximumLine, 64*1024)))
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		line, readErr := reader.ReadBytes('\n')
		if uint64(len(line)) > limits.maximumLine {
			return 0, fmt.Errorf("proof line %d exceeds %d bytes", proofs+1, limits.maximumLine)
		}
		if errors.Is(readErr, io.EOF) {
			// The validator may be appending one record. It is excluded until its
			// newline makes the complete bytes part of the next verified prefix.
			break
		}
		if readErr != nil {
			return 0, readErr
		}
		completedBytes += int64(len(line))
		hasher.Write(line)
		line = line[:len(line)-1]
		if len(line) == 0 {
			continue
		}
		var record validatorpkg.ProofRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return 0, fmt.Errorf("proof line %d is malformed: %w", proofs+1, err)
		}
		if record.Version != 1 || record.TrailId == (connect.Id{}) || record.Coverage == 0 || record.CompleteTimeMs == 0 {
			return 0, fmt.Errorf("proof line %d has an incomplete release identity", proofs+1)
		}
		if trailIDs[record.TrailId] {
			return 0, fmt.Errorf("proof line %d duplicates trail_id %s", proofs+1, record.TrailId)
		}
		if uint64(proofs) >= limits.maximumProofs {
			return 0, fmt.Errorf("path-proof store exceeds %d records", limits.maximumProofs)
		}
		if err := verify(&record, proofs); err != nil {
			return 0, err
		}
		trailIDs[record.TrailId] = true
		proofs++
	}
	var prefixHash [sha256.Size]byte
	copy(prefixHash[:], hasher.Sum(nil))
	cache.prefixes[path] = scenarioPathProofPrefix{verifierHash: verifierHash, bytes: completedBytes, hash: prefixHash, proofs: proofs, trailIDs: trailIDs}
	return proofs, nil
}
