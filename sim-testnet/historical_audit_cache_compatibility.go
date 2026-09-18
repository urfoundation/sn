// Import the known v1 verifier contract without repeating immutable RPC work.
// Directory indexes contain lookup hints only: each selected proof is reopened
// and authenticated against its exact context, input and original MAC.
package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
)

const (
	historicalAuditCacheLegacySchema          = "urnetwork-sim-historical-audit-cache-v1"
	historicalAuditCacheLegacyDirectoryName   = "historical-audit-cache-v1"
	historicalAuditCacheLegacyVerifierVersion = "immutable-history-v1"
	historicalAuditCacheLegacyMaximumEntries  = 1_000_000
)

// A context is indexed once per process, not once per receipt. No authentication
// key or trusted success is kept in the index, and canceled scans are retryable.
type historicalAuditLegacyIndex struct {
	stateLock sync.Mutex
	complete  bool
	proofKVs  map[string][]historicalAuditCacheProof
}

type historicalAuditLegacyIndexKey struct {
	stateDir    string
	contextHash string
}

var historicalAuditLegacyIndexes sync.Map

// A known unchanged verifier contract can import an old executable-bound proof.
// Changing the verifier version disables import as well as ordinary cache hits.
func (self *historicalAuditCacheEntry) readCompatibleSuccess(ctx context.Context, key [32]byte) bool {
	if ctx == nil || ctx.Err() != nil || self.proof.Schema != historicalAuditCacheSchema ||
		self.proof.VerifierVersion != historicalAuditCacheLegacyVerifierVersion || self.proof.ExecutableSHA256 != "" {
		return false
	}
	indexKey := historicalAuditLegacyIndexKey{stateDir: self.stateDir, contextHash: self.proof.ContextHash}
	value, _ := historicalAuditLegacyIndexes.LoadOrStore(indexKey, &historicalAuditLegacyIndex{})
	index := value.(*historicalAuditLegacyIndex)
	for _, proof := range index.candidates(ctx, self) {
		current := proof
		current.Schema, current.ExecutableSHA256 = historicalAuditCacheSchema, ""
		if current != self.proof {
			continue
		}
		nameHash, err := canonicalHashHex(proof)
		if err != nil {
			continue
		}
		legacy := &historicalAuditCacheEntry{stateDir: self.stateDir, directoryName: historicalAuditCacheLegacyDirectoryName,
			name: strings.TrimPrefix(nameHash, "0x") + ".json", key: key, proof: proof}
		if legacy.readSuccess() && ctx.Err() == nil {
			return true
		}
	}
	return false
}

// Scan bounded descriptor-relative entries once; a malformed hint can only
// cause a cache miss, never bypass verification or choose a filesystem path.
func (self *historicalAuditLegacyIndex) candidates(ctx context.Context, entry *historicalAuditCacheEntry) []historicalAuditCacheProof {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if !self.complete {
		directory, err := openHistoricalAuditNamedCacheDirectory(entry.stateDir, historicalAuditCacheLegacyDirectoryName, false)
		if err != nil {
			return nil
		}
		defer directory.Close()
		proofKVs := map[string][]historicalAuditCacheProof{}
		count := 0
		for {
			files, readErr := directory.ReadDir(256)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return nil
			}
			for _, file := range files {
				count++
				if ctx.Err() != nil || count > historicalAuditCacheLegacyMaximumEntries {
					return nil
				}
				if !strings.HasSuffix(file.Name(), ".json") {
					continue
				}
				envelope, ok := readHistoricalAuditCacheEnvelope(directory, file.Name())
				proof := envelope.Proof
				if !ok || proof.Schema != historicalAuditCacheLegacySchema || proof.VerifierVersion != historicalAuditCacheLegacyVerifierVersion ||
					proof.ContextHash != entry.proof.ContextHash || !proof.Success || !validSHA256ContentHash("sha256:"+proof.ExecutableSHA256) {
					continue
				}
				nameHash, err := canonicalHashHex(proof)
				if err != nil || strings.TrimPrefix(nameHash, "0x")+".json" != file.Name() {
					continue
				}
				current := proof
				current.Schema, current.ExecutableSHA256 = historicalAuditCacheSchema, ""
				currentHash, err := canonicalHashHex(current)
				if err == nil {
					name := strings.TrimPrefix(currentHash, "0x") + ".json"
					proofKVs[name] = append(proofKVs[name], proof)
				}
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
		}
		self.proofKVs, self.complete = proofKVs, true
	}
	return self.proofKVs[entry.name]
}
