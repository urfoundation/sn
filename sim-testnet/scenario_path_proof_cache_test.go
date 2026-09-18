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
