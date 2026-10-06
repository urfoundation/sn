// Durable proof-prefix checkpoints retain completed signature verification
// across driver replacements. Their authentication never replaces rehashing the
// original source prefix or verifying every newly appended complete record.
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/urnetwork/connect/v2026"
	"golang.org/x/sys/unix"
)

const (
	scenarioPathProofStoreSchema       = "urnetwork-sim-path-proof-prefix-v1"
	scenarioPathProofStoreDirectory    = "path-proof-prefix-cache-v1"
	scenarioPathProofStoreMaximumBytes = 64 * 1024 * 1024
	// Bump when proof decoding, identity or VerifyProofRecord rules change.
	scenarioPathProofVerifierVersion = "validator-proof-record-v1"
)

// This is a proof of an exact byte prefix, not authority for a whole live file.
// Sorted unique trail identities preserve duplicate detection after a restart.
type scenarioPathProofCheckpoint struct {
	Schema          string       `json:"schema"`
	VerifierVersion string       `json:"verifier_version"`
	ContextHash     string       `json:"context_hash"`
	Source          string       `json:"source"`
	VerifierHash    string       `json:"verifier_hash"`
	Bytes           int64        `json:"bytes"`
	Hash            string       `json:"sha256"`
	Proofs          int          `json:"proofs"`
	TrailIds        []connect.Id `json:"trail_ids"`
}

// Only private, bounded, authenticated envelopes can supply a reusable prefix.
type scenarioPathProofCheckpointEnvelope struct {
	Proof scenarioPathProofCheckpoint `json:"proof"`
	Mac   string                      `json:"hmac_sha256"`
}

// Each observation owner has its own in-memory cache; atomic publication and a
// short per-source file lock prevent concurrent owners from regressing progress.
type scenarioPathProofStore struct {
	stateDir    string
	contextHash string
	key         [32]byte
	readOnly    bool
}

// Executable hashes, runtime revisions and unrelated allowance changes do not
// change proof verification. Keys and trail depth remain in the verifier hash.
func newDurableScenarioPathProofCache(cfg *ResolvedConfig, stateDir string) *scenarioPathProofCache {
	cache := newScenarioPathProofCache()
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.WalletMaterial == "" || stateDir == "" {
		return cache
	}
	contextHash, err := canonicalHashHex(struct {
		DeploymentId string
		GenesisHash  string
		ChainId      uint64
		Netuid       uint16
	}{cfg.Config.Deployment.DeploymentID, cfg.Public.Chain.GenesisHash, cfg.ChainID, cfg.Netuid})
	if err != nil {
		return cache
	}
	cache.store = &scenarioPathProofStore{stateDir: stateDir, contextHash: contextHash,
		key: derive32(cfg, "scenario-path-proof-prefix/v1"), readOnly: cfg.readOnlyAudit}
	return cache
}

// Cache names bind a canonical source inside the deployment, so moving the
// whole state directory preserves progress without allowing foreign sources.
func (self *scenarioPathProofStore) source(path string) (string, string, bool) {
	relative, err := filepath.Rel(self.stateDir, path)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", false
	}
	relative = filepath.ToSlash(relative)
	hash := sha256.Sum256([]byte(self.contextHash + "\x00" + relative))
	return relative, hex.EncodeToString(hash[:]) + ".json", true
}

// The entire decoded proof, including its complete trail census, is covered.
func (self *scenarioPathProofStore) authenticationTag(proof scenarioPathProofCheckpoint) []byte {
	wire, _ := json.Marshal(proof)
	mac := hmac.New(sha256.New, self.key[:])
	mac.Write([]byte(scenarioPathProofStoreSchema + "\x00"))
	mac.Write(wire)
	return mac.Sum(nil)
}

// Unavailable or corrupt cache data is a miss. Source truncation or substitution
// is checked separately by the caller and remains an integrity error.
func (self *scenarioPathProofStore) load(path string, limits scenarioPathProofLimits) (scenarioPathProofPrefix, bool) {
	if self == nil {
		return scenarioPathProofPrefix{}, false
	}
	source, name, ok := self.source(path)
	if !ok {
		return scenarioPathProofPrefix{}, false
	}
	directory, err := openHistoricalAuditNamedCacheDirectory(self.stateDir, scenarioPathProofStoreDirectory, false)
	if err != nil {
		return scenarioPathProofPrefix{}, false
	}
	defer directory.Close()
	return self.read(directory, source, name, limits)
}

// Validate both authentication and the internal count/identity census before a
// single signature can be skipped. The original source hash is checked later.
func (self *scenarioPathProofStore) read(directory *os.File, source, name string, limits scenarioPathProofLimits) (scenarioPathProofPrefix, bool) {
	var prefix scenarioPathProofPrefix
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return prefix, false
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	if requireHistoricalAuditPrivateMode(fd, unix.S_IFREG, 0o600) != nil {
		return prefix, false
	}
	wire, err := io.ReadAll(io.LimitReader(file, scenarioPathProofStoreMaximumBytes+1))
	if err != nil || len(wire) > scenarioPathProofStoreMaximumBytes || rejectDuplicatePostconditionJSONFields(wire) != nil {
		return prefix, false
	}
	var envelope scenarioPathProofCheckpointEnvelope
	if decodeStrictJSONBytes(wire, &envelope) != nil {
		return prefix, false
	}
	proof := envelope.Proof
	tag, tagErr := hex.DecodeString(envelope.Mac)
	hash, hashErr := hex.DecodeString(proof.Hash)
	if proof.Schema != scenarioPathProofStoreSchema || proof.VerifierVersion != scenarioPathProofVerifierVersion || proof.ContextHash != self.contextHash || proof.Source != source ||
		!validCanonicalHashHex(proof.VerifierHash) || proof.Bytes < 0 || uint64(proof.Bytes) > limits.maximumBytes || proof.Proofs < 0 || uint64(proof.Proofs) > limits.maximumProofs || proof.Proofs != len(proof.TrailIds) ||
		hashErr != nil || len(hash) != sha256.Size || hex.EncodeToString(hash) != proof.Hash || tagErr != nil || !hmac.Equal(tag, self.authenticationTag(proof)) {
		return prefix, false
	}
	prefix = scenarioPathProofPrefix{verifierHash: proof.VerifierHash, bytes: proof.Bytes, proofs: proof.Proofs, trailIDs: make(map[connect.Id]bool, len(proof.TrailIds))}
	copy(prefix.hash[:], hash)
	for index, id := range proof.TrailIds {
		if id == (connect.Id{}) || index > 0 && bytes.Compare(proof.TrailIds[index-1][:], id[:]) >= 0 {
			return scenarioPathProofPrefix{}, false
		}
		prefix.trailIDs[id] = true
	}
	if prefix.bytes == 0 && (prefix.proofs != 0 || prefix.hash != sha256.Sum256(nil)) {
		return scenarioPathProofPrefix{}, false
	}
	return prefix, true
}

// Persist only completed verified records. A stale concurrent writer cannot
// replace a longer prefix under the same verifier; other key sets revalidate.
// Failure loses an optimization and never stops the active observation.
func (self *scenarioPathProofStore) save(path string, prefix scenarioPathProofPrefix, limits scenarioPathProofLimits) bool {
	if self == nil || self.readOnly || prefix.bytes == 0 {
		return false
	}
	source, name, ok := self.source(path)
	if !ok {
		return false
	}
	ids := make([]connect.Id, 0, len(prefix.trailIDs))
	for id := range prefix.trailIDs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
	proof := scenarioPathProofCheckpoint{Schema: scenarioPathProofStoreSchema, VerifierVersion: scenarioPathProofVerifierVersion,
		ContextHash: self.contextHash, Source: source, VerifierHash: prefix.verifierHash, Bytes: prefix.bytes,
		Hash: hex.EncodeToString(prefix.hash[:]), Proofs: prefix.proofs, TrailIds: ids}
	wire, err := json.Marshal(scenarioPathProofCheckpointEnvelope{Proof: proof, Mac: hex.EncodeToString(self.authenticationTag(proof))})
	if err != nil || len(wire) >= scenarioPathProofStoreMaximumBytes {
		return false
	}
	directory, err := openHistoricalAuditNamedCacheDirectory(self.stateDir, scenarioPathProofStoreDirectory, true)
	if err != nil {
		return false
	}
	defer directory.Close()
	directoryFd := int(directory.Fd())
	lockFd, err := unix.Openat(directoryFd, name+".lock", unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return false
	}
	defer unix.Close(lockFd)
	if requireHistoricalAuditPrivateMode(lockFd, unix.S_IFREG, 0o600) != nil || unix.Flock(lockFd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return false
	}
	defer unix.Flock(lockFd, unix.LOCK_UN)
	if retained, ok := self.read(directory, source, name, limits); ok && retained.verifierHash == prefix.verifierHash && retained.bytes >= prefix.bytes {
		return retained.bytes > prefix.bytes || retained.hash == prefix.hash
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return false
	}
	temporary := ".tmp-" + hex.EncodeToString(random[:])
	fd, err := unix.Openat(directoryFd, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return false
	}
	defer unix.Unlinkat(directoryFd, temporary, 0)
	file := os.NewFile(uintptr(fd), temporary)
	defer file.Close()
	if file.Chmod(0o600) != nil {
		return false
	}
	if _, err := file.Write(append(wire, '\n')); err != nil || file.Sync() != nil || file.Close() != nil {
		return false
	}
	return unix.Renameat(directoryFd, temporary, directoryFd, name) == nil && directory.Sync() == nil
}
