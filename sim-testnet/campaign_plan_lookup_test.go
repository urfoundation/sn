// Recovery traversal fixtures retain real signed lineage and exact plan bytes.
// Explicit read hooks force changes at the authentication boundary.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Multiple recovery edges and their succession root share each archived plan,
// including a later approved configuration, without changing signed digests.
func TestScenarioCampaignPlanLookupSharesWholeRecoveryTraversal(t *testing.T) {
	t.Parallel()
	fixture, current, _, files := campaignRecoveryChainCacheFixture(t)
	bindFailedRecoveryGeneration(t, fixture, current, 10)
	advanceCampaignLineageFixture(t, fixture)
	if _, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(3*time.Hour), fixture.journal); err != nil {
		t.Fatal(err)
	}
	files, err := scenarioCampaignRecoveryFiles(fixture.stateDir)
	if err != nil || len(files) != 3 {
		t.Fatalf("recovery files: count=%d error=%v", len(files), err)
	}
	reader, err := newScenarioCampaignLineageReader(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	reads := map[string]int{}
	reader.plans.afterReadForTest = func(hash string) { reads[hash]++ }
	records, err := reader.readRecoveryChain(files, nil)
	if err != nil || len(records) != 3 || len(reads) != 3 {
		t.Fatalf("shared recovery traversal: records=%d reads=%v error=%v", len(records), reads, err)
	}
	for hash, count := range reads {
		if count != 1 {
			t.Fatalf("approval %s was decoded %d times", hash, count)
		}
	}
	for _, record := range records {
		raw := readCampaignSuccessionFixtureBytes(t, filepath.Join(fixture.stateDir, "plans", stringsTrim0x(record.attempt.payload.PlanHash)+".json"))
		if record.attempt.payload.Recovery.ApprovedPlanSha256 != bytesSHA256(raw) {
			t.Fatal("shared proof changed exact archived plan digest")
		}
	}
}

// A lookup belongs to one traversal. Reuse retains exact raw bytes, while the
// next traversal must authenticate independently rather than share pointers.
func TestScenarioCampaignPlanLookupReusesOnlyItsOwnProof(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	lookup := &scenarioCampaignPlanLookup{stateDir: fixture.stateDir}
	reads := 0
	lookup.afterReadForTest = func(string) { reads++ }
	first, digest, err := lookup.read(fixture.stateDir, fixture.current.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		plan, actualDigest, err := lookup.read(fixture.stateDir, fixture.current.PlanHash)
		if err != nil || plan != first || actualDigest != digest || reads != 1 {
			t.Fatalf("warm lookup: reads=%d error=%v", reads, err)
		}
	}
	other := &scenarioCampaignPlanLookup{stateDir: fixture.stateDir, afterReadForTest: func(string) { reads++ }}
	second, secondDigest, err := other.read(fixture.stateDir, fixture.current.PlanHash)
	if err != nil || second == first || secondDigest != digest || reads != 2 {
		t.Fatalf("fresh traversal inherited another proof: reads=%d error=%v", reads, err)
	}
	if _, _, err := lookup.read(t.TempDir(), fixture.current.PlanHash); err == nil {
		t.Fatal("lookup accepted another source owner")
	}
}

// Changed raw bytes, truncation and substitutions cannot reuse an authenticated
// plan even when its semantic hash or restored timestamp is unchanged.
func TestScenarioCampaignPlanLookupRejectsWarmSourceChanges(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"same-size-restored-mtime", "truncate", "replace", "symlink", "directory-replace"} {
		fixture := newCampaignSuccessionFixture(t)
		lookup := &scenarioCampaignPlanLookup{stateDir: fixture.stateDir}
		if _, _, err := lookup.read(fixture.stateDir, fixture.current.PlanHash); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(fixture.stateDir, "plans", stringsTrim0x(fixture.current.PlanHash)+".json")
		raw := readCampaignSuccessionFixtureBytes(t, path)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		switch mutation {
		case "same-size-restored-mtime":
			index := bytes.IndexByte(raw, '\n')
			if index < 0 {
				t.Fatal("fixture requires a whitespace byte")
			}
			raw[index] = ' '
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
		case "truncate":
			if err := os.Truncate(path, int64(len(raw)/2)); err != nil {
				t.Fatal(err)
			}
		case "replace":
			if err := atomicWrite(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
		case "symlink":
			if err := os.Rename(path, path+".retained"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path+".retained", path); err != nil {
				t.Fatal(err)
			}
		case "directory-replace":
			directory := filepath.Dir(path)
			if err := os.Rename(directory, directory+".retained"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(filepath.Join(directory+".retained", filepath.Base(path)), path); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := lookup.read(fixture.stateDir, fixture.current.PlanHash); err == nil {
			t.Fatalf("warm source accepted %s", mutation)
		}
		if err := lookup.check(); err == nil {
			t.Fatalf("final source fence accepted %s", mutation)
		}
	}
}

// A write forced after full decoding but before publication cannot create a
// trusted entry. The following invocation still performs full authentication.
func TestScenarioCampaignPlanLookupRejectsMutationDuringRead(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	path := filepath.Join(fixture.stateDir, "plans", stringsTrim0x(fixture.current.PlanHash)+".json")
	raw := readCampaignSuccessionFixtureBytes(t, path)
	lookup := &scenarioCampaignPlanLookup{stateDir: fixture.stateDir}
	lookup.afterReadForTest = func(string) {
		if err := atomicWrite(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := lookup.read(fixture.stateDir, fixture.current.PlanHash); err == nil || len(lookup.proofKVs) != 0 {
		t.Fatalf("interrupted authentication was retained: entries=%d error=%v", len(lookup.proofKVs), err)
	}
	lookup.afterReadForTest = nil
	if _, _, err := lookup.read(fixture.stateDir, fixture.current.PlanHash); err != nil {
		t.Fatal(err)
	}
}

// A later valid plan read must not hide mutation of an earlier plan, including
// the implicit succession source that no later recovery edge reads again.
func TestScenarioCampaignPlanLookupFencesEarlierRootAtCompletion(t *testing.T) {
	t.Parallel()
	fixture, current, _, _ := campaignRecoveryChainCacheFixture(t)
	bindFailedRecoveryGeneration(t, fixture, current, 10)
	advanceCampaignLineageFixture(t, fixture)
	if _, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(3*time.Hour), fixture.journal); err != nil {
		t.Fatal(err)
	}
	files, err := scenarioCampaignRecoveryFiles(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := newScenarioCampaignLineageReader(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.stateDir, "plans", stringsTrim0x(fixture.prior.PlanHash)+".json")
	raw := readCampaignSuccessionFixtureBytes(t, path)
	changed := false
	reader.plans.afterReadForTest = func(hash string) {
		if hash == fixture.current.PlanHash {
			if _, exists := reader.plans.proofKVs[fixture.prior.PlanHash]; !exists {
				t.Fatal("mutation did not follow the root's authentication")
			}
			if err := atomicWrite(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			changed = true
		}
	}
	if records, err := reader.readRecoveryChain(files, nil); err == nil || len(records) != 0 || !changed || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("traversal released changed earlier source: records=%d changed=%t error=%v", len(records), changed, err)
	}
}

// Memory exhaustion only disables reuse. Every miss still authenticates the
// complete persisted hash, and raw formatting stays part of signed recovery.
func TestScenarioCampaignPlanLookupBudgetFallbackAuthenticatesEveryRead(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	lookup := &scenarioCampaignPlanLookup{stateDir: fixture.stateDir, retainedBytes: maximumCampaignEvidenceAggregateBytes}
	reads := 0
	lookup.afterReadForTest = func(string) { reads++ }
	path := filepath.Join(fixture.stateDir, "plans", stringsTrim0x(fixture.current.PlanHash)+".json")
	raw := readCampaignSuccessionFixtureBytes(t, path)
	for range 2 {
		if _, digest, err := lookup.read(fixture.stateDir, fixture.current.PlanHash); err != nil || digest != bytesSHA256(raw) {
			t.Fatalf("cold fallback changed authentication: digest=%s error=%v", digest, err)
		}
	}
	if reads != 2 || len(lookup.proofKVs) != 0 {
		t.Fatalf("over-budget lookup retained a plan: reads=%d entries=%d", reads, len(lookup.proofKVs))
	}
	if err := atomicWrite(path, append(bytes.Clone(raw), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, digest, err := lookup.read(fixture.stateDir, fixture.current.PlanHash); err != nil || digest == bytesSHA256(raw) {
		t.Fatalf("fallback manufactured the original raw digest: digest=%s error=%v", digest, err)
	}
	priorRaw := readCampaignSuccessionFixtureBytes(t, filepath.Join(fixture.stateDir, "plans", stringsTrim0x(fixture.prior.PlanHash)+".json"))
	if err := atomicWrite(path, priorRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lookup.read(fixture.stateDir, fixture.current.PlanHash); err == nil {
		t.Fatal("cold fallback accepted substituted semantic plan hash")
	}
}
