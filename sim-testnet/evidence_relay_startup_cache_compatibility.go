//go:build linux || darwin

// Reuse already authenticated v1 relay checkpoints across unrelated rebuilds.
// A matching verifier contract, exact legacy context and every current file
// witness are required before a checkpoint is promoted to the stable v2 key.
package main

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"errors"
	"io"
	"strings"
)

const (
	evidenceRelayStartupCacheLegacySchema          = "urnetwork-sim-relay-startup-cache-v1"
	evidenceRelayStartupCacheLegacyDirectoryName   = "relay-startup-cache-v1"
	evidenceRelayStartupCacheLegacyVerifierVersion = "exact-plan-prefix-v1"
	evidenceRelayStartupCacheLegacyMaximumEntries  = 1024
	evidenceRelayStartupCacheLegacyMaximumBytes    = 64 * 1024 * 1024
)

// Failed, canceled, stale, mismatched or unsafe old checkpoints are ordinary
// misses. Authentication precedes context reconstruction and witness traversal.
func (self *evidenceRelayRuntime) readCompatibleEvidenceRelayStartupProof(ctx context.Context, entry *evidenceRelayStartupCacheEntry, key [32]byte,
	entries []JournalEntry, inventories map[uint64]evidenceRelayStartupSourceInventory) (evidenceRelayStartupCacheProof, bool) {
	var best evidenceRelayStartupCacheProof
	if ctx == nil || ctx.Err() != nil || entry.fixed.Schema != evidenceRelayStartupCacheSchema ||
		entry.fixed.VerifierVersion != evidenceRelayStartupCacheLegacyVerifierVersion || entry.fixed.ExecutableSha256 != "" {
		return best, false
	}
	directory, err := openHistoricalAuditNamedCacheDirectory(entry.stateDir, evidenceRelayStartupCacheLegacyDirectoryName, false)
	if err != nil {
		return best, false
	}
	defer directory.Close()
	contexts := map[string]string{}
	count, totalBytes := 0, int64(0)
	for {
		files, readErr := directory.ReadDir(64)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return best, false
		}
		for _, file := range files {
			count++
			if ctx.Err() != nil || count > evidenceRelayStartupCacheLegacyMaximumEntries {
				return evidenceRelayStartupCacheProof{}, false
			}
			if !strings.HasSuffix(file.Name(), ".json") {
				continue
			}
			if totalBytes >= evidenceRelayStartupCacheLegacyMaximumBytes {
				return evidenceRelayStartupCacheProof{}, false
			}
			envelope, readBytes, ok := readEvidenceRelayStartupCacheEnvelope(directory, file.Name(),
				min(evidenceRelayStartupCacheMaximumBytes, evidenceRelayStartupCacheLegacyMaximumBytes-totalBytes))
			totalBytes += readBytes
			proof := envelope.Proof
			if !ok || proof.Schema != evidenceRelayStartupCacheLegacySchema || proof.VerifierVersion != evidenceRelayStartupCacheLegacyVerifierVersion ||
				!validSHA256ContentHash("sha256:"+proof.ExecutableSha256) || file.Name() != strings.TrimPrefix(proof.ContextHash, "0x")+".json" {
				continue
			}
			legacy := evidenceRelayStartupCacheEntry{key: key}
			tag, err := hex.DecodeString(envelope.Mac)
			if err != nil || !hmac.Equal(tag, legacy.authenticationTag(proof)) {
				continue
			}
			contextHash, found := contexts[proof.ExecutableSha256]
			if !found {
				contextHash, err = self.evidenceRelayStartupContextHashFor(evidenceRelayStartupCacheLegacySchema,
					evidenceRelayStartupCacheLegacyVerifierVersion, proof.ExecutableSha256)
				if err != nil {
					return evidenceRelayStartupCacheProof{}, false
				}
				contexts[proof.ExecutableSha256] = contextHash
			}
			if proof.ContextHash != contextHash {
				continue
			}
			proof.Schema, proof.ExecutableSha256, proof.ContextHash = entry.fixed.Schema, "", entry.fixed.ContextHash
			if proof.JournalCount > best.JournalCount && self.validateEvidenceRelayStartupProof(proof, entries, inventories) {
				best = proof
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	return best, best.JournalCount != 0 && ctx.Err() == nil
}
