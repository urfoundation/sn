//go:build linux || darwin

// Closed-census discovery is complete bounded private metadata, not public
// authority. Genuine two-origin publication remains the next mandatory step.
package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The real mixed-trail publisher supplies exact original metadata. An exact
// history ceiling succeeds; one byte less or one member less yields no prefix.
func TestValidatorEvidencePublicationV2DiscoveryKeepsBothActualReplicas(t *testing.T) {
	fixture := newReleasePublicationV2TestFixture(t, true)
	path, err := ValidatorEvidencePublicationV2ManifestPath(fixture.startup.cfg.StateDir, fixture.manifest.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bounds := fixture.readOptions.Bounds
	bounds.MaxHistoryBytes = uint64(len(original))
	observed, err := DiscoverValidatorEvidencePublicationV2Manifests(t.Context(), fixture.startup.cfg.StateDir, bounds)
	if err != nil || len(observed) != 1 || !reflect.DeepEqual(observed[0], *fixture.manifest) {
		t.Fatal("exact private history ceiling lost its genuine census", err)
	}
	var reads [2]int
	for index, store := range fixture.startup.stores {
		_, _, reads[index] = store.snapshot()
	}
	publication, err := ReadValidatorEvidencePublicationV2(t.Context(), &observed[0], fixture.readOptions)
	if err != nil || publication == nil || !bytes.Equal(publication.Census, fixture.publication.Census) || len(publication.Members) != 2 {
		t.Fatal("discovered locator replaced complete public authority", err)
	}
	for index, member := range publication.Members {
		original := fixture.publication.Members[index]
		if !bytes.Equal(member.SignedArtifact, original.SignedArtifact) || !bytes.Equal(member.Payload, original.Payload) || !bytes.Equal(member.Calldata, original.Calldata) {
			t.Fatal("discovery changed original signed source or payload bytes")
		}
	}
	for index, store := range fixture.startup.stores {
		_, _, after := store.snapshot()
		if after <= reads[index] {
			t.Fatal("discovery skipped one independently retained public replica")
		}
		reads[index] = after
	}
	for _, fault := range []string{"history", "members"} {
		limited := bounds
		if fault == "history" {
			limited.MaxHistoryBytes--
		} else {
			limited.MaxParticipants = 1
		}
		if retained, err := DiscoverValidatorEvidencePublicationV2Manifests(t.Context(), fixture.startup.cfg.StateDir, limited); err == nil || retained != nil {
			t.Fatalf("%s one-over returned a partial discovery census: %v", fault, err)
		}
	}
	for index, store := range fixture.startup.stores {
		_, _, after := store.snapshot()
		if after != reads[index] {
			t.Fatal("private capacity refusal initiated public reads")
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("discovery or capacity refusal rewrote the original locator", err)
	}
}

// A numeric probe would miss an arbitrarily delayed/future filename. Retain
// it for independent rejection, never silently exclude it or authorize it.
func TestValidatorEvidencePublicationV2DiscoveryIncludesFutureAndRefusesUnknownFiles(t *testing.T) {
	fixture := newReleasePublicationV2TestFixture(t, false)
	stateDir := fixture.startup.cfg.StateDir
	future := *fixture.manifest
	future.Epoch = ^uint64(0)
	encoded, err := marshalAttemptSettlementV2JSON(t.Context(), &future, fixture.readOptions.Bounds.MaxClosureBytes, true, true)
	if err != nil {
		t.Fatal(err)
	}
	path, err := ValidatorEvidencePublicationV2ManifestPath(stateDir, future.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteReleaseEvidenceV2File(t.Context(), path, encoded, fixture.readOptions.Bounds.MaxClosureBytes); err != nil {
		t.Fatal(err)
	}
	observed, err := DiscoverValidatorEvidencePublicationV2Manifests(t.Context(), stateDir, fixture.readOptions.Bounds)
	if err != nil || len(observed) != 2 || !reflect.DeepEqual(observed[0], *fixture.manifest) || !reflect.DeepEqual(observed[1], future) {
		t.Fatal("discovery omitted an unexpected retained epoch", err)
	}
	if publication, err := ReadValidatorEvidencePublicationV2(t.Context(), &observed[1], fixture.readOptions); err == nil || publication != nil {
		t.Fatal("discovered future locator became independent epoch authority", err)
	}
	if publication, err := ReadValidatorEvidencePublicationV2(t.Context(), &observed[0], fixture.readOptions); err != nil || publication == nil {
		t.Fatal("delayed invalid discovery poisoned original public bytes", err)
	}
	unknown := filepath.Join(filepath.Dir(path), "unrecognized-original.json")
	if err := os.WriteFile(unknown, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if observed, err := DiscoverValidatorEvidencePublicationV2Manifests(t.Context(), stateDir, fixture.readOptions.Bounds); err == nil || observed != nil {
		t.Fatal("unknown original filename was silently skipped or returned a prefix", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(encoded, after) {
		t.Fatal("failed discovery discarded unexpected original bytes", err)
	}
}

// Empty discovery and cancelled owners do not create state. Descriptor
// aliases cannot manufacture empty success or borrow another private owner.
func TestValidatorEvidencePublicationV2DiscoveryMissingCancelledAndAliasOwners(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	bounds := ReleaseEvidenceV2Bounds{MaxHistoryBytes: 4096, MaxParticipants: 2, MaxClosureBytes: 2048}
	if observed, err := DiscoverValidatorEvidencePublicationV2Manifests(t.Context(), stateDir, bounds); err != nil || observed != nil {
		t.Fatal("actual absent publication namespace was not empty", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if observed, err := DiscoverValidatorEvidencePublicationV2Manifests(ctx, stateDir, bounds); !errors.Is(err, context.Canceled) || observed != nil {
		t.Fatal("cancelled discovery was acknowledged", err)
	}
	if _, err := os.Lstat(stateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only discovery created validator state", err)
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "retained")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(stateDir, "evidence-publications")); err != nil {
		t.Fatal(err)
	}
	if observed, err := DiscoverValidatorEvidencePublicationV2Manifests(t.Context(), stateDir, bounds); err == nil || observed != nil {
		t.Fatal("alias namespace became empty funded discovery", err)
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatal("refused alias mutated its unrelated private target", err)
	}
}
