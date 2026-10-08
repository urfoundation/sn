// Each live observation verifies only newly appended complete proofs. Durable
// authenticated prefix checkpoints preserve that work across driver restarts.
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
	prefixes         map[string]scenarioPathProofPrefix
	store            *scenarioPathProofStore
	checkpointProofs int
}

// Ordinary callers can retain only memory; live drivers attach a durable store.
func newScenarioPathProofCache() *scenarioPathProofCache {
	return &scenarioPathProofCache{prefixes: map[string]scenarioPathProofPrefix{}, checkpointProofs: 1024}
}

func configuredScenarioPathProofLimits(cfg *ResolvedConfig, validatorID int) (scenarioPathProofLimits, bool, error) {
	if cfg == nil || cfg.Config == nil {
		return scenarioPathProofLimits{}, false, errors.New("scenario path-proof limits are unavailable")
	}
	for _, configured := range cfg.Config.ValidatorEvidenceV2 {
		if configured.ValidatorID != uint64(validatorID) {
			continue
		}
		limits, err := scenarioPathProofLimitsV2(configured.Evidence.Bounds)
		return limits, true, err
	}
	return scenarioPathProofLimits{}, false, nil
}

func scenarioPathProofLimitsV2(bounds validatorpkg.ReleaseEvidenceV2Bounds) (scenarioPathProofLimits, error) {
	disk := bounds.Disk
	maximumLine := max(disk.MaxRecordBytes, bounds.Replay.MaxProofBytes)
	if disk.MaxProofBytes == 0 || disk.MaxTrailCount == 0 || maximumLine == 0 || maximumLine > disk.MaxProofBytes {
		return scenarioPathProofLimits{}, errors.New("scenario path-proof bounds are incomplete")
	}
	return scenarioPathProofLimits{maximumBytes: disk.MaxProofBytes, maximumProofs: disk.MaxTrailCount, maximumLine: maximumLine}, nil
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

// Successful complete records become checkpoints even when a later record or
// cancellation interrupts the sweep. Unverified or partial bytes never enter it.
func (self *scenarioPathProofCache) inspect(ctx context.Context, path, verifierHash string, limits scenarioPathProofLimits, verify func(*validatorpkg.ProofRecord, int) error) (int, error) {
	if ctx == nil || self == nil || self.prefixes == nil || !validCanonicalHashHex(verifierHash) || limits.maximumBytes == 0 || limits.maximumProofs == 0 || limits.maximumLine == 0 || verify == nil {
		return 0, errors.New("scenario path-proof cache input is incomplete")
	}
	prefix, reuse := self.prefixes[path]
	if !reuse {
		prefix, reuse = self.store.load(path, limits)
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		if reuse && prefix.bytes != 0 {
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

	if reuse && info.Size() < prefix.bytes {
		return 0, errors.New("verified path-proof prefix was truncated")
	}
	if !reuse || prefix.verifierHash != verifierHash {
		prefix = scenarioPathProofPrefix{verifierHash: verifierHash, trailIDs: map[connect.Id]bool{}}
	}
	if prefix.bytes < 0 || uint64(prefix.bytes) > limits.maximumBytes || prefix.proofs < 0 || uint64(prefix.proofs) > limits.maximumProofs || prefix.proofs != len(prefix.trailIDs) {
		return 0, errors.New("verified path-proof prefix exceeds its current bounds or identity census")
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
		var last [1]byte
		if _, err := file.ReadAt(last[:], prefix.bytes-1); err != nil || last[0] != '\n' {
			return 0, errors.New("verified path-proof prefix has no complete record boundary")
		}
	}

	trailIDs := cloneScenarioTrailIDs(prefix.trailIDs)
	proofs, completedBytes := prefix.proofs, prefix.bytes
	checkpointBytes, checkpointProofs := prefix.bytes, prefix.proofs
	checkpoint := func() {
		if completedBytes == checkpointBytes {
			return
		}
		var prefixHash [sha256.Size]byte
		copy(prefixHash[:], hasher.Sum(nil))
		verified := scenarioPathProofPrefix{verifierHash: verifierHash, bytes: completedBytes, hash: prefixHash, proofs: proofs, trailIDs: cloneScenarioTrailIDs(trailIDs)}
		self.prefixes[path] = verified
		if self.store != nil {
			persisted := self.store.save(path, verified, limits)
			source, _, _ := self.store.source(path)
			fmt.Fprintf(os.Stderr, "sim-testnet: path-proof prefix checkpoint; source=%s completed_proofs=%d completed_bytes=%d scan_cut_bytes=%d durable=%t\n", source, proofs, completedBytes, info.Size(), persisted)
		}
		checkpointBytes, checkpointProofs = completedBytes, proofs
	}
	// A later failure retains only records whose verification already succeeded.
	defer checkpoint()
	// A live validator can append faster than signatures are checked. Retain the
	// initial size cut so this snapshot finishes; later bytes belong to the next.
	reader := bufio.NewReaderSize(io.LimitReader(file, info.Size()-prefix.bytes), int(min(limits.maximumLine, 64*1024)))
	scannedBytes := prefix.bytes
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		line, readErr := readScenarioPathProofLine(ctx, reader, limits.maximumLine)
		scannedBytes += int64(len(line))
		if uint64(len(line)) > limits.maximumLine {
			return 0, fmt.Errorf("proof line %d exceeds %d bytes", proofs+1, limits.maximumLine)
		}
		if errors.Is(readErr, io.EOF) {
			if scannedBytes != info.Size() {
				return 0, errors.New("path-proof source was truncated during its fixed-cut sweep")
			}
			// The validator may be appending one record. It is excluded until its
			// newline makes the complete bytes part of the next verified prefix.
			break
		}
		if readErr != nil {
			return 0, readErr
		}
		if len(line) == 1 {
			completedBytes += int64(len(line))
			hasher.Write(line)
			continue
		}
		var record validatorpkg.ProofRecord
		if err := json.Unmarshal(line[:len(line)-1], &record); err != nil {
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
		completedBytes += int64(len(line))
		hasher.Write(line)
		if proofs-checkpointProofs >= max(1, self.checkpointProofs) || completedBytes-checkpointBytes >= 8*1024*1024 {
			checkpoint()
		}
	}
	var prefixHash [sha256.Size]byte
	copy(prefixHash[:], hasher.Sum(nil))
	if err := revalidateScenarioPathProofPrefix(ctx, path, info.Size(), completedBytes, prefixHash); err != nil {
		return 0, err
	}
	checkpoint()
	if _, ok := self.prefixes[path]; !ok {
		self.prefixes[path] = scenarioPathProofPrefix{verifierHash: verifierHash, bytes: completedBytes, hash: prefixHash, proofs: proofs, trailIDs: trailIDs}
	}
	return proofs, nil
}

// Bound allocation before appending a reader fragment. A malformed record with
// no newline must not allocate the entire, potentially much larger, proof file.
func readScenarioPathProofLine(ctx context.Context, reader *bufio.Reader, maximum uint64) ([]byte, error) {
	var line []byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fragment, err := reader.ReadSlice('\n')
		if uint64(len(line))+uint64(len(fragment)) > maximum {
			return nil, fmt.Errorf("proof line exceeds %d bytes", maximum)
		}
		line = append(line, fragment...)
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err
		}
	}
}

// Reopen the selected path after verification so replacement, truncation or
// mutation during a sweep cannot become the current observation's evidence.
func revalidateScenarioPathProofPrefix(ctx context.Context, path string, cut, length int64, expected [sha256.Size]byte) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("reopen verified path-proof prefix: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < cut {
		return errors.New("verified path-proof source was replaced or truncated")
	}
	hasher := sha256.New()
	buffer := make([]byte, 64*1024)
	for remaining := length; remaining > 0; {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := io.ReadFull(file, buffer[:min(int64(len(buffer)), remaining)])
		if err != nil {
			return fmt.Errorf("rehash completed path-proof prefix: %w", err)
		}
		hasher.Write(buffer[:n])
		remaining -= int64(n)
	}
	var actual [sha256.Size]byte
	copy(actual[:], hasher.Sum(nil))
	if actual != expected {
		return errors.New("verified path-proof prefix changed during verification")
	}
	return nil
}
