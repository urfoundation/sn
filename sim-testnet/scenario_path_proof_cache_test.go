// The live observation cache keeps verified append-only proof prefixes while
// hashing every retained byte again before it authenticates an appended suffix.
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/connect"
)

func appendScenarioPathProofCacheRecord(t *testing.T, path string, trailId connect.Id) {
	t.Helper()
	record := validatorpkg.ProofRecord{Version: 1, TrailId: trailId, Coverage: 1, CompleteTimeMs: 1}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func scenarioPathProofCacheTestInspect(cache *scenarioPathProofCache, path, verifierHash string, verified *int) (int, error) {
	limits := scenarioPathProofLimits{maximumBytes: 1024 * 1024, maximumProofs: 16, maximumLine: 64 * 1024}
	return cache.inspect(context.Background(), path, verifierHash, limits, func(*validatorpkg.ProofRecord, int) error {
		*verified++
		return nil
	})
}

func TestScenarioPathProofCacheVerifiesOnlyAuthenticatedAppend(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "proofs.jsonl")
	first, second, third := connect.NewId(), connect.NewId(), connect.NewId()
	appendScenarioPathProofCacheRecord(t, path, first)
	appendScenarioPathProofCacheRecord(t, path, second)
	cache, verified := newScenarioPathProofCache(), 0
	verifierHash := "0x" + strings.Repeat("31", 32)
	if count, err := scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil || count != 2 || verified != 2 {
		t.Fatalf("initial prefix was not verified exactly once: count=%d verified=%d err=%v", count, verified, err)
	}
	if count, err := scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil || count != 2 || verified != 2 {
		t.Fatalf("unchanged prefix was cryptographically replayed: count=%d verified=%d err=%v", count, verified, err)
	}
	appendScenarioPathProofCacheRecord(t, path, third)
	if count, err := scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil || count != 3 || verified != 3 {
		t.Fatalf("append was not the sole newly verified record: count=%d verified=%d err=%v", count, verified, err)
	}
}

func TestScenarioPathProofCacheRejectsPrefixSubstitutionAndTruncation(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"substitute", "truncate"} {
		path := filepath.Join(t.TempDir(), mutation+".jsonl")
		appendScenarioPathProofCacheRecord(t, path, connect.NewId())
		cache, verified := newScenarioPathProofCache(), 0
		verifierHash := "0x" + strings.Repeat("42", 32)
		if _, err := scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if mutation == "substitute" {
			data[0] ^= 1
		} else {
			data = data[:len(data)-1]
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err = scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified)
		if err == nil || !strings.Contains(err.Error(), "prefix") || verified != 1 {
			t.Fatalf("%s did not fail closed without signature replay: verified=%d err=%v", mutation, verified, err)
		}
	}
}

func TestScenarioPathProofCacheRevalidatesChangedVerifierAndRejectsDuplicateAppend(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "proofs.jsonl")
	trailId := connect.NewId()
	appendScenarioPathProofCacheRecord(t, path, trailId)
	cache, verified := newScenarioPathProofCache(), 0
	if _, err := scenarioPathProofCacheTestInspect(cache, path, "0x"+strings.Repeat("53", 32), &verified); err != nil {
		t.Fatal(err)
	}
	if _, err := scenarioPathProofCacheTestInspect(cache, path, "0x"+strings.Repeat("64", 32), &verified); err != nil || verified != 2 {
		t.Fatalf("changed verifier did not force full authentication: verified=%d err=%v", verified, err)
	}
	appendScenarioPathProofCacheRecord(t, path, trailId)
	if _, err := scenarioPathProofCacheTestInspect(cache, path, "0x"+strings.Repeat("64", 32), &verified); err == nil || !strings.Contains(err.Error(), "duplicates trail_id") || verified != 2 {
		t.Fatalf("duplicate append changed the verified prefix: verified=%d err=%v", verified, err)
	}
	if prefix := cache.prefixes[path]; prefix.proofs != 1 {
		t.Fatalf("failed append entered the cache: %+v", prefix)
	}
}

// Appending from verification forces the writer to advance after the reader's
// size cut, without depending on a goroutine's scheduling or a wall-clock delay.
func TestScenarioPathProofCacheRetainsFixedCutDuringConcurrentAppend(t *testing.T) {
	t.Parallel()
	for _, retained := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "proofs.jsonl")
		cache, verified := newScenarioPathProofCache(), 0
		verifierHash := "0x" + strings.Repeat("75", 32)
		baseline := 0
		if retained {
			appendScenarioPathProofCacheRecord(t, path, connect.NewId())
			if _, err := scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil {
				t.Fatal(err)
			}
			baseline = 1
		}
		appendScenarioPathProofCacheRecord(t, path, connect.NewId())
		cut, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		appended := false
		limits := scenarioPathProofLimits{maximumBytes: 1024 * 1024, maximumProofs: 16, maximumLine: 64 * 1024}
		count, err := cache.inspect(context.Background(), path, verifierHash, limits, func(*validatorpkg.ProofRecord, int) error {
			verified++
			if !appended {
				appendScenarioPathProofCacheRecord(t, path, connect.NewId())
				appended = true
			}
			return nil
		})
		if err != nil || count != baseline+1 || verified != baseline+1 || cache.prefixes[path].bytes != cut.Size() {
			t.Fatalf("retained=%v: snapshot followed appended bytes: count=%d verified=%d prefix=%d cut=%d err=%v", retained, count, verified, cache.prefixes[path].bytes, cut.Size(), err)
		}
		if count, err = scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil || count != baseline+2 || verified != baseline+2 {
			t.Fatalf("retained=%v: later snapshot lost or replayed the deferred append: count=%d verified=%d err=%v", retained, count, verified, err)
		}
	}
}

// A newline written after the size cut must not complete a partial proof in the
// current observation; the next observation authenticates that complete record.
func TestScenarioPathProofCacheDefersPartialTailCompletedAfterCut(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "proofs.jsonl")
	appendScenarioPathProofCacheRecord(t, path, connect.NewId())
	completeCut, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	appendScenarioPathProofCacheRecord(t, path, connect.NewId())
	withTail, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, withTail.Size()-1); err != nil {
		t.Fatal(err)
	}
	cache, verified := newScenarioPathProofCache(), 0
	verifierHash := "0x" + strings.Repeat("86", 32)
	limits := scenarioPathProofLimits{maximumBytes: 1024 * 1024, maximumProofs: 16, maximumLine: 64 * 1024}
	count, err := cache.inspect(context.Background(), path, verifierHash, limits, func(*validatorpkg.ProofRecord, int) error {
		verified++
		if verified == 1 {
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			_, writeErr := file.Write([]byte{'\n'})
			closeErr := file.Close()
			if writeErr != nil {
				return writeErr
			}
			return closeErr
		}
		return nil
	})
	if err != nil || count != 1 || verified != 1 || cache.prefixes[path].bytes != completeCut.Size() {
		t.Fatalf("snapshot included a tail completed after its cut: count=%d verified=%d prefix=%d cut=%d err=%v", count, verified, cache.prefixes[path].bytes, completeCut.Size(), err)
	}
	if count, err = scenarioPathProofCacheTestInspect(cache, path, verifierHash, &verified); err != nil || count != 2 || verified != 2 {
		t.Fatalf("later snapshot lost or replayed the completed tail: count=%d verified=%d err=%v", count, verified, err)
	}
}
