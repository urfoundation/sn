//go:build linux || darwin

// Real canonical locator replacements exercise the gap between discovery's
// charged outer witness and the independently owned inner descriptor read.
package validator

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Both grammars use their actual strict decoder and discovery function. The
// second observed entry is replaced, independent of filesystem entry order,
// so any refusal must discard an already decoded first entry.
func verifyPublicationDiscoveryV2Replacement[T any](t *testing.T, paths []string, bounds *ReleaseEvidenceV2Bounds, failure string, discover func(func(operation, path string) error) ([]T, error), canonical func(path string, grow bool) ([]byte, error)) {
	t.Helper()
	originalsKVs := make(map[string][]byte, len(paths))
	var exactBytes uint64
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		originalsKVs[path] = raw
		exactBytes += uint64(len(raw))
	}
	if len(originalsKVs) != 2 {
		t.Fatal("replacement control requires two distinct original locators")
	}
	bounds.MaxHistoryBytes = exactBytes
	if observed, err := discover(nil); err != nil || len(observed) != 2 {
		t.Fatal("exact initial private census failed", err)
	}

	for _, test := range []struct {
		stage string
		grow  bool
	}{
		{stage: "before-read", grow: false},
		{stage: "before-read", grow: true},
		{stage: "after-read", grow: false},
		{stage: "after-read", grow: true},
	} {
		backupDir := newAttemptSettlementRuntimeV2TestStateDir(t)
		savedPath := filepath.Join(backupDir, "original.json")
		replacedPath := ""
		var replacement []byte
		started, completed := 0, 0
		observed, err := discover(func(operation, path string) error {
			if operation == "before-read" {
				started++
			}
			if operation == "after-read" {
				completed++
			}
			if started != 2 || operation != test.stage {
				return nil
			}
			if replacedPath != "" {
				t.Fatal("replacement boundary ran more than once")
			}
			original, exists := originalsKVs[path]
			if !exists {
				t.Fatal("discovery selected an unowned replacement path")
			}
			var err error
			replacement, err = canonical(path, test.grow)
			if err != nil {
				return err
			}
			if test.grow {
				if len(replacement) <= len(original) || uint64(len(replacement)) > bounds.MaxClosureBytes {
					t.Fatal("grown canonical locator did not cross only the aggregate byte ceiling")
				}
			} else if !bytes.Equal(replacement, original) {
				t.Fatal("equal-byte inode replacement changed canonical metadata")
			}
			before, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if err := os.Rename(path, savedPath); err != nil {
				return err
			}
			if err := os.WriteFile(path, replacement, 0o600); err != nil {
				return err
			}
			after, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if os.SameFile(before, after) {
				t.Fatal("real replacement reused the original inode")
			}
			replacedPath = path
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), failure) || observed != nil || started != 2 || completed != 2 || replacedPath == "" {
			t.Fatalf("%s grow=%t did not refuse the whole census at the exact poststate: result=%d starts=%d reads=%d error=%v", test.stage, test.grow, len(observed), started, completed, err)
		}
		decoded, err := canonical(replacedPath, false)
		if err != nil || !bytes.Equal(decoded, replacement) {
			t.Fatal("replacement was not accepted by the genuine canonical descriptor reader", err)
		}
		saved, err := os.ReadFile(savedPath)
		if err != nil || !bytes.Equal(saved, originalsKVs[replacedPath]) {
			t.Fatal("replacement lost original private bytes", err)
		}

		// Stable replacement controls distinguish outer custody from decoder
		// failure: exact bytes pass, growth fails only the old aggregate cap.
		stable, err := discover(nil)
		if test.grow {
			if err == nil || stable != nil {
				t.Fatal("stable enlarged files escaped the original aggregate ceiling")
			}
			bounds.MaxHistoryBytes = exactBytes + uint64(len(replacement)-len(saved))
			if stable, err := discover(nil); err != nil || len(stable) != 2 {
				t.Fatal("exact enlarged census failed after real replacement stabilized", err)
			}
		} else if err != nil || len(stable) != 2 {
			t.Fatal("stable byte-identical replacement was not valid outside the race", err)
		}
		if err := os.Rename(replacedPath, filepath.Join(backupDir, "replacement.json")); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(savedPath, replacedPath); err != nil {
			t.Fatal(err)
		}
		bounds.MaxHistoryBytes = exactBytes
		if stable, err := discover(nil); err != nil || len(stable) != 2 {
			t.Fatal("restored originals did not recover the exact census", err)
		}
	}
	for path, original := range originalsKVs {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, original) {
			t.Fatal("discovery changed retained original custody", err)
		}
	}
}

// A real mixed M8/failed/idle publication remains independently readable from
// both origins after every real-file race. The extra numeric locator is only
// private census input, and cannot authorize a different public epoch.
func TestValidatorEvidencePublicationV2DiscoveryRejectsRealFileReplacement(t *testing.T) {
	t.Parallel()
	fixture := newReleasePublicationV2TestFixture(t, true)
	var replicaObjectsKVs [2]map[string][]byte
	var writes [2]int
	for index, store := range fixture.startup.stores {
		replicaObjectsKVs[index], writes[index], _ = store.snapshot()
	}
	stateDir := fixture.startup.cfg.StateDir
	bounds := fixture.readOptions.Bounds
	originalPath, err := ValidatorEvidencePublicationV2ManifestPath(stateDir, fixture.manifest.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	other := *fixture.manifest
	other.Epoch++
	otherPath, err := ValidatorEvidencePublicationV2ManifestPath(stateDir, other.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := marshalAttemptSettlementV2JSON(t.Context(), &other, bounds.MaxClosureBytes, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteReleaseEvidenceV2File(t.Context(), otherPath, encoded, bounds.MaxClosureBytes); err != nil {
		t.Fatal(err)
	}
	verifyPublicationDiscoveryV2Replacement(t, []string{originalPath, otherPath}, &bounds, "closed publication changed during bounded discovery",
		func(step func(operation, path string) error) ([]ValidatorEvidencePublicationV2Manifest, error) {
			return discoverValidatorEvidencePublicationV2Manifests(t.Context(), stateDir, bounds, step)
		},
		func(path string, grow bool) ([]byte, error) {
			value, err := ReadValidatorEvidencePublicationV2Manifest(t.Context(), path, bounds.MaxClosureBytes, bounds.MaxParticipants)
			if err != nil {
				return nil, err
			}
			if grow {
				value.CensusBytes = bounds.MaxClosureBytes
			}
			return marshalAttemptSettlementV2JSON(t.Context(), value, bounds.MaxClosureBytes, true, true)
		})
	retained, err := DiscoverValidatorEvidencePublicationV2Manifests(t.Context(), stateDir, bounds)
	if err != nil || len(retained) != 2 || !reflect.DeepEqual(retained[0], *fixture.manifest) {
		t.Fatal("exported discovery lost original complete locator", err)
	}
	if publication, err := ReadValidatorEvidencePublicationV2(t.Context(), &other, fixture.readOptions); err == nil || publication != nil {
		t.Fatal("untrusted extra locator became public epoch authority")
	}
	var reads [2]int
	for index, store := range fixture.startup.stores {
		_, _, reads[index] = store.snapshot()
	}
	publication, err := ReadValidatorEvidencePublicationV2(t.Context(), &retained[0], fixture.readOptions)
	if err != nil || publication == nil || !bytes.Equal(publication.Census, fixture.publication.Census) || len(publication.Members) != 2 {
		t.Fatal("restored locator lost original public census", err)
	}
	for index, member := range publication.Members {
		original := fixture.publication.Members[index]
		if !bytes.Equal(member.SignedArtifact, original.SignedArtifact) || !bytes.Equal(member.Payload, original.Payload) || !bytes.Equal(member.Calldata, original.Calldata) {
			t.Fatal("replacement changed original consent, source or calldata")
		}
	}
	var census ValidatorEvidenceCensusV2
	if err := json.Unmarshal(publication.Census, &census); err != nil {
		t.Fatal(err)
	}
	if len(census.Members) != 2 || census.Members[0].CompleteCount != 1 || census.Members[0].FailedCount != 1 || census.Members[1].RecordCount != 0 {
		t.Fatal("replacement dropped mixed terminal or idle source membership")
	}
	for index, store := range fixture.startup.stores {
		objectsKVs, afterWrites, after := store.snapshot()
		if after <= reads[index] {
			t.Fatal("restored publication skipped an actual public replica")
		}
		if afterWrites != writes[index] || !reflect.DeepEqual(objectsKVs, replicaObjectsKVs[index]) {
			t.Fatal("replacement changed original records, proofs or metadata at a public replica")
		}
	}
}

// Actual payout observation and no-root members use a separate locator
// grammar, but must retain the same outer/inner file-state equality.
func TestValidatorEvidenceDepositAuditV2DiscoveryRejectsRealFileReplacement(t *testing.T) {
	t.Parallel()
	fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	manifest, prepared := fixture.retained(t)
	var original releaseDepositAuditPreparedV2
	if err := json.Unmarshal(prepared, &original); err != nil {
		t.Fatal(err)
	}
	if original.Publication == nil || len(original.Publication.Members) != 2 {
		t.Fatal("actual producer did not retain both original source members")
	}
	stateDir := fixture.base.runtime.cfg.StateDir
	bounds := fixture.options.Bounds
	originalPath, err := ValidatorEvidenceDepositAuditV2ManifestPath(stateDir, manifest.Epoch, manifest.Subject)
	if err != nil {
		t.Fatal(err)
	}
	other := *manifest
	other.Subject.NativeEpoch++
	other.Decision.SubnetEpoch++
	otherPath, err := ValidatorEvidenceDepositAuditV2ManifestPath(stateDir, other.Epoch, other.Subject)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := marshalAttemptSettlementV2JSON(t.Context(), &other, bounds.MaxClosureBytes, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteReleaseEvidenceV2File(t.Context(), otherPath, encoded, bounds.MaxClosureBytes); err != nil {
		t.Fatal(err)
	}
	verifyPublicationDiscoveryV2Replacement(t, []string{originalPath, otherPath}, &bounds, "deposit audit publication changed during bounded discovery",
		func(step func(operation, path string) error) ([]ValidatorEvidenceDepositAuditV2Manifest, error) {
			return discoverValidatorEvidenceDepositAuditV2Manifests(t.Context(), stateDir, bounds, step)
		},
		func(path string, grow bool) ([]byte, error) {
			value, err := ReadValidatorEvidenceDepositAuditV2Manifest(t.Context(), path, bounds.MaxClosureBytes, bounds.MaxParticipants)
			if err != nil {
				return nil, err
			}
			if grow {
				value.CensusBytes = bounds.MaxClosureBytes
			}
			return marshalAttemptSettlementV2JSON(t.Context(), value, bounds.MaxClosureBytes, true, true)
		})
	retained, err := DiscoverValidatorEvidenceDepositAuditV2Manifests(t.Context(), stateDir, bounds)
	if err != nil || len(retained) != 2 || !reflect.DeepEqual(retained[0], *manifest) {
		t.Fatal("exported audit discovery lost original complete locator", err)
	}
	if publication, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), &other, fixture.options); err == nil || publication != nil {
		t.Fatal("untrusted extra locator became public audit authority")
	}
	reads, posts := fixture.publicReads.Load(), fixture.posts.Load()
	publication, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), &retained[0], fixture.options)
	if err != nil || publication == nil || !bytes.Equal(publication.Census, original.Publication.Census) || len(publication.Members) != 2 {
		t.Fatal("restored audit lost original public census", err)
	}
	for index, member := range publication.Members {
		prior := original.Publication.Members[index]
		if !bytes.Equal(member.SignedArtifact, prior.SignedArtifact) || !bytes.Equal(member.Payload, prior.Payload) {
			t.Fatal("replacement changed original dual consent or actual signed observation")
		}
	}
	// Each of two actual origins reads one census plus two consent/payload
	// pairs. No re-sign, protected post or past payout fetch is authorized.
	if fixture.publicReads.Load()-reads != 10 || fixture.posts.Load() != posts || fixture.payoutReads.Load() != 2 {
		t.Fatal("restored audit skipped complete replica reads or republished original custody")
	}
	_, after := fixture.retained(t)
	if !bytes.Equal(after, prepared) {
		t.Fatal("replacement modified original prepared signatures or request/response bytes")
	}
}
