// Durable prefix tests use explicit callback boundaries to interrupt, append or
// replace sources without timing assumptions or real RPC dependencies.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/connect"
)

// Use the live constructor and private directory layout; only cryptographic
// verification is counted independently at the cache's existing callback seam.
func scenarioPathProofStoreFixture(t *testing.T, count int) (*ResolvedConfig, string, string, *scenarioPathProofCache) {
	t.Helper()
	cfg, root := testResolvedConfig(t), t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "runtime", "validator-1", "state", "operators", "no-1", "proofs.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	for range count {
		appendScenarioPathProofCacheRecord(t, path, connect.NewId())
	}
	cache := newDurableScenarioPathProofCache(cfg, root)
	if cache.store == nil {
		t.Fatal("live constructor did not enable durable proofs")
	}
	return cfg, root, path, cache
}

// Keep limits independent of checkpoint cadence so tests can force an exact
// durable cut without changing the reader's configured trust boundaries.
func scenarioPathProofStoreTestLimits() scenarioPathProofLimits {
	return scenarioPathProofLimits{maximumBytes: 1024 * 1024, maximumProofs: 16, maximumLine: 64 * 1024}
}

// Resolve the actual private checkpoint rather than assuming a cache key.
func scenarioPathProofStoreTestPath(t *testing.T, cache *scenarioPathProofCache, path string) string {
	t.Helper()
	_, name, ok := cache.store.source(path)
	if !ok {
		t.Fatal("fixture source escaped its deployment")
	}
	return filepath.Join(cache.store.stateDir, scenarioPathProofStoreDirectory, name)
}

// A fresh driver rehashes existing bytes but verifies only the appended suffix.
// Budget/configuration revisions do not invalidate immutable proof semantics.
func TestScenarioPathProofStoreResumesExactPrefixAcrossDrivers(t *testing.T) {
	t.Parallel()
	cfg, root, path, cache := scenarioPathProofStoreFixture(t, 2)
	verifierHash, verified := "0x"+strings.Repeat("91", 32), 0
	if count, err := scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil || count != 2 || verified != 2 {
		t.Fatalf("initial verification count=%d verified=%d error=%v", count, verified, err)
	}
	prefix, present := cache.store.load(path, scenarioPathProofStoreTestLimits())
	if !present || prefix.proofs != 2 || len(prefix.trailIDs) != 2 {
		t.Fatalf("checkpoint census=%+v present=%v", prefix, present)
	}
	appendScenarioPathProofCacheRecord(t, path, connect.NewId())
	changed := *cfg
	changed.ConfigHash = "0x" + strings.Repeat("72", 32)
	resumed := newDurableScenarioPathProofCache(&changed, root)
	if count, err := scenarioPathProofCacheTestInspect(resumed, path, verifierHash, &verified); err != nil || count != 3 || verified != 3 {
		t.Fatalf("replacement replayed signatures: count=%d verified=%d error=%v", count, verified, err)
	}
	info, err := os.Stat(scenarioPathProofStoreTestPath(t, cache, path))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("checkpoint permissions: %v %v", info, err)
	}
}

// Cancellation and later verifier failure preserve only already successful
// complete records; the failed record must be retried by the next owner.
func TestScenarioPathProofStoreRetainsProgressBeforeInterruption(t *testing.T) {
	for _, cancelRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "verify-failure", true: "cancellation"}[cancelRun], func(t *testing.T) {
			t.Parallel()
			cfg, root, path, cache := scenarioPathProofStoreFixture(t, 3)
			cache.checkpointProofs = 1
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := errors.New("second signature did not authenticate")
			verifierHash := "0x" + strings.Repeat("92", 32)
			_, err := cache.inspect(ctx, path, verifierHash, scenarioPathProofStoreTestLimits(), func(_ *validatorpkg.ProofRecord, index int) error {
				if cancelRun && index == 0 {
					cancel()
				} else if !cancelRun && index == 1 {
					return failure
				}
				return nil
			})
			if cancelRun && !errors.Is(err, context.Canceled) || !cancelRun && !errors.Is(err, failure) {
				t.Fatalf("interruption error=%v", err)
			}
			prefix, present := cache.store.load(path, scenarioPathProofStoreTestLimits())
			if !present || prefix.proofs != 1 || len(prefix.trailIDs) != 1 {
				t.Fatalf("unverified record entered checkpoint: %+v present=%v", prefix, present)
			}
			verified := 0
			if count, err := scenarioPathProofCacheTestInspect(newDurableScenarioPathProofCache(cfg, root), path, verifierHash, &verified); err != nil || count != 3 || verified != 2 {
				t.Fatalf("resume count=%d verified=%d error=%v", count, verified, err)
			}
		})
	}
}

// Once authenticated, a checkpoint detects source loss even in a new process.
func TestScenarioPathProofStoreRejectsChangedSourceAndVerifierReuse(t *testing.T) {
	for _, mutation := range []string{"substitute", "truncate", "missing", "verifier"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			cfg, root, path, cache := scenarioPathProofStoreFixture(t, 2)
			verifierHash, verified := "0x"+strings.Repeat("93", 32), 0
			if _, err := scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "substitute":
				raw[0] ^= 1
				err = os.WriteFile(path, raw, 0o600)
			case "truncate":
				err = os.Truncate(path, int64(len(raw)-1))
			case "missing":
				err = os.Remove(path)
			case "verifier":
				verifierHash = "0x" + strings.Repeat("94", 32)
			}
			if err != nil {
				t.Fatal(err)
			}
			count, err := scenarioPathProofCacheTestInspect(newDurableScenarioPathProofCache(cfg, root), path, verifierHash, &verified)
			if mutation == "verifier" {
				if err != nil || count != 2 || verified != 4 {
					t.Fatalf("changed verifier reused old proof: count=%d verified=%d error=%v", count, verified, err)
				}
			} else if err == nil || verified != 2 {
				t.Fatalf("changed source escaped its prefix: verified=%d error=%v", verified, err)
			}
		})
	}
}

// Malformed, empty or wrongly authenticated cache files force full validation.
// Even a signed envelope must carry a consistent unique trail census.
func TestScenarioPathProofStoreRejectsCorruptCheckpointAuthority(t *testing.T) {
	t.Parallel()
	cfg, root, path, cache := scenarioPathProofStoreFixture(t, 2)
	verifierHash, verified := "0x"+strings.Repeat("95", 32), 0
	if _, err := scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil {
		t.Fatal(err)
	}
	checkpointPath := scenarioPathProofStoreTestPath(t, cache, path)
	original, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"empty", "malformed", "mac", "count", "duplicate", "zero", "domain", "version", "bytes"} {
		var envelope scenarioPathProofCheckpointEnvelope
		if err := json.Unmarshal(original, &envelope); err != nil {
			t.Fatal(err)
		}
		switch mutation {
		case "count":
			envelope.Proof.Proofs++
		case "duplicate":
			envelope.Proof.TrailIds[1] = envelope.Proof.TrailIds[0]
		case "zero":
			envelope.Proof.TrailIds[0] = connect.Id{}
		case "domain":
			envelope.Proof.Source = "another/proofs.jsonl"
		case "version":
			envelope.Proof.VerifierVersion = "unsupported"
		case "bytes":
			envelope.Proof.Bytes = -1
		}
		envelope.Mac = hex.EncodeToString(cache.store.authenticationTag(envelope.Proof))
		if mutation == "mac" {
			envelope.Mac = strings.Repeat("00", 32)
		}
		wire, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if mutation == "empty" {
			wire = nil
		}
		if mutation == "malformed" {
			wire = []byte("{")
		}
		if err := os.WriteFile(checkpointPath, wire, 0o600); err != nil {
			t.Fatal(err)
		}
		verified = 0
		if count, err := scenarioPathProofCacheTestInspect(newDurableScenarioPathProofCache(cfg, root), path, verifierHash, &verified); err != nil || count != 2 || verified != 2 {
			t.Fatalf("%s bypassed full verification: count=%d verified=%d error=%v", mutation, count, verified, err)
		}
	}
}

// Appending after the initial cut is deferred; replacing or shrinking that cut
// cannot be accepted even if the buffered reader retained the original bytes.
func TestScenarioPathProofStoreChecksConcurrentSourceCut(t *testing.T) {
	for _, mutation := range []string{"append", "replace", "truncate"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			cfg, root, path, cache := scenarioPathProofStoreFixture(t, 2)
			verifierHash := "0x" + strings.Repeat("96", 32)
			count, err := cache.inspect(t.Context(), path, verifierHash, scenarioPathProofStoreTestLimits(), func(_ *validatorpkg.ProofRecord, index int) error {
				if index != 0 {
					return nil
				}
				if mutation == "append" {
					appendScenarioPathProofCacheRecord(t, path, connect.NewId())
					return nil
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if mutation == "truncate" {
					return os.Truncate(path, 1)
				}
				raw[0] ^= 1
				return atomicWrite(path, raw, 0o600)
			})
			if mutation != "append" {
				if err == nil {
					t.Fatal("changed cut became a successful observation")
				}
				return
			}
			if err != nil || count != 2 {
				t.Fatalf("append followed beyond cut: count=%d error=%v", count, err)
			}
			verified := 0
			if count, err := scenarioPathProofCacheTestInspect(newDurableScenarioPathProofCache(cfg, root), path, verifierHash, &verified); err != nil || count != 3 || verified != 1 {
				t.Fatalf("deferred append: count=%d verified=%d error=%v", count, verified, err)
			}
		})
	}
}

// A forged mid-record cut cannot pass merely because it has a valid MAC and
// hash. Safe cache storage remains optional for observers without write access.
func TestScenarioPathProofStoreChecksRecordBoundaryAndReadOnlyOwner(t *testing.T) {
	t.Parallel()
	cfg, root, path, cache := scenarioPathProofStoreFixture(t, 1)
	verifierHash, verified := "0x"+strings.Repeat("97", 32), 0
	if _, err := scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil {
		t.Fatal(err)
	}
	checkpointPath := scenarioPathProofStoreTestPath(t, cache, path)
	original, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatal(err)
	}
	appendScenarioPathProofCacheRecord(t, path, connect.NewId())
	readOnly := *cfg
	readOnly.readOnlyAudit = true
	if count, err := scenarioPathProofCacheTestInspect(newDurableScenarioPathProofCache(&readOnly, root), path, verifierHash, &verified); err != nil || count != 2 || verified != 2 {
		t.Fatalf("read-only observation failed: count=%d verified=%d error=%v", count, verified, err)
	}
	after, err := os.ReadFile(checkpointPath)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("read-only owner changed durable progress", err)
	}
	var envelope scenarioPathProofCheckpointEnvelope
	if err := json.Unmarshal(original, &envelope); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	envelope.Proof.Bytes--
	hash := sha256.Sum256(raw[:envelope.Proof.Bytes])
	envelope.Proof.Hash = hex.EncodeToString(hash[:])
	envelope.Mac = hex.EncodeToString(cache.store.authenticationTag(envelope.Proof))
	if err := writePublicJSON(checkpointPath, envelope); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(checkpointPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scenarioPathProofCacheTestInspect(newDurableScenarioPathProofCache(cfg, root), path, verifierHash, &verified); err == nil || !strings.Contains(err.Error(), "complete record boundary") {
		t.Fatalf("mid-record checkpoint accepted: %v", err)
	}
}

// Atomic writes always leave one complete authenticated prefix; a late stale
// writer cannot lower an already-published cut under the same verifier.
func TestScenarioPathProofStoreConcurrentPublicationPreservesLongestPrefix(t *testing.T) {
	t.Parallel()
	_, _, path, cache := scenarioPathProofStoreFixture(t, 1)
	verifierHash, verified := "0x"+strings.Repeat("98", 32), 0
	if _, err := scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil {
		t.Fatal(err)
	}
	short := cache.prefixes[path]
	appendScenarioPathProofCacheRecord(t, path, connect.NewId())
	if _, err := scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil {
		t.Fatal(err)
	}
	long := cache.prefixes[path]
	var workers sync.WaitGroup
	for index := range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			prefix := short
			if index%2 == 0 {
				prefix = long
			}
			cache.store.save(path, prefix, scenarioPathProofStoreTestLimits())
		}()
	}
	workers.Wait()
	loaded, ok := cache.store.load(path, scenarioPathProofStoreTestLimits())
	if !ok || loaded.bytes != long.bytes || loaded.hash != long.hash || loaded.proofs != 2 {
		t.Fatalf("stale publication regressed checkpoint: %+v present=%v", loaded, ok)
	}
}

// Duplicate trails remain rejected across process boundaries and a different
// operator path cannot borrow the first operator's authenticated checkpoint.
func TestScenarioPathProofStoreKeepsTrailAndSourceIdentity(t *testing.T) {
	t.Parallel()
	cfg, root, path, cache := scenarioPathProofStoreFixture(t, 1)
	verifierHash, verified := "0x"+strings.Repeat("99", 32), 0
	if _, err := scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil {
		t.Fatal(err)
	}
	var duplicate connect.Id
	for id := range cache.prefixes[path].trailIDs {
		duplicate = id
	}
	appendScenarioPathProofCacheRecord(t, path, duplicate)
	verified = 0
	if _, err := scenarioPathProofCacheTestInspect(newDurableScenarioPathProofCache(cfg, root), path, verifierHash, &verified); err == nil || !strings.Contains(err.Error(), "duplicates trail_id") || verified != 0 {
		t.Fatalf("persisted duplicate census was lost: verified=%d error=%v", verified, err)
	}
	other := filepath.Join(root, "other", "proofs.jsonl")
	if err := os.MkdirAll(filepath.Dir(other), 0o700); err != nil {
		t.Fatal(err)
	}
	appendScenarioPathProofCacheRecord(t, other, connect.NewId())
	_, otherName, _ := cache.store.source(other)
	wire, err := os.ReadFile(scenarioPathProofStoreTestPath(t, cache, path))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, scenarioPathProofStoreDirectory, otherName), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	if count, err := scenarioPathProofCacheTestInspect(newDurableScenarioPathProofCache(cfg, root), other, verifierHash, &verified); err != nil || count != 1 || verified != 1 {
		t.Fatalf("another source borrowed a checkpoint: count=%d verified=%d error=%v", count, verified, err)
	}
}

// An unterminated record never enters a durable cut, and the bounded reader
// rejects an oversized line before allocating the rest of the source file.
func TestScenarioPathProofStoreExcludesPartialAndOversizedRecords(t *testing.T) {
	t.Parallel()
	cfg, root, path, cache := scenarioPathProofStoreFixture(t, 2)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, info.Size()-1); err != nil {
		t.Fatal(err)
	}
	verifierHash, verified := "0x"+strings.Repeat("9a", 32), 0
	if count, err := scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil || count != 1 || verified != 1 {
		t.Fatalf("partial record entered checkpoint: count=%d verified=%d error=%v", count, verified, err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write([]byte{'\n'})
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	if count, err := scenarioPathProofCacheTestInspect(newDurableScenarioPathProofCache(cfg, root), path, verifierHash, &verified); err != nil || count != 2 || verified != 2 {
		t.Fatalf("completed tail was not authenticated: count=%d verified=%d error=%v", count, verified, err)
	}
	file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr = file.Write([]byte(strings.Repeat("x", 64*1024+1)))
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err := scenarioPathProofCacheTestInspect(newDurableScenarioPathProofCache(cfg, root), path, verifierHash, &verified); err == nil || !strings.Contains(err.Error(), "exceeds") || verified != 2 {
		t.Fatalf("oversized record was not bounded: verified=%d error=%v", verified, err)
	}
}
