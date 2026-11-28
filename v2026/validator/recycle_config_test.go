// The observer loads explicit authority without relaxing producer admission.
package validator

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A separately approved artifact can be inspected but cannot start steering.
func TestOwnerRecycleAdmissionConfigLoadsOnlyForReadOnlyUse(t *testing.T) {
	fixture := newRecycleAdmissionFixture(t, nil)
	sourcePath := fixture.cfg.OwnerRecycleApproval.Approval.Path
	fixture.cfg.OwnerRecycleApproval.Approval = ReleaseEvidenceV2File{}
	raw, err := yaml.Marshal(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "synthetic-admission.yml")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadOwnerRecycleAdmissionConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RetainOwnerRecycleApproval(t.Context(), loaded); err == nil {
		t.Fatal("unapproved draft acquired custody")
	}
	if result, err := ObserveOwnerRecycleAdmission(t.Context(), loaded, fixture.chain); err == nil || result != nil || len(fixture.calls) != 0 {
		t.Fatalf("unapproved draft reached observation: %+v %v calls=%v", result, err, fixture.calls)
	}
	// The external signer hashes the loaded representation, including YAML's
	// nil/empty collection normalization, before publishing its envelope pin.
	fixture.cfg = loaded
	fixture.approval.ConfigHash, err = OwnerRecycleConfigHash(loaded)
	if err != nil {
		t.Fatal(err)
	}
	loaded.OwnerRecycleApproval.Approval.Path = sourcePath
	fixture.sign(t)
	raw, err = yaml.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadOwnerRecycleAdmissionConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if hash, err := OwnerRecycleConfigHash(loaded); err != nil || hash != fixture.approval.ConfigHash {
		t.Fatalf("read-only load changed signed configuration: %x %v", hash, err)
	}
	if _, err := LoadReleaseConfig(path); err == nil {
		t.Fatal("read-only approval relaxed the producer loader")
	}
	if _, err := RetainOwnerRecycleApproval(t.Context(), loaded); err != nil {
		t.Fatal(err)
	}
	observation, err := ObserveOwnerRecycleAdmission(t.Context(), loaded, fixture.chain)
	if err != nil || observation == nil || observation.ActivationReady {
		t.Fatalf("read-only public entry: %+v %v", observation, err)
	}
	for _, compact := range []bool{false, true} {
		var err error
		if compact {
			_, err = newReleaseSteererV2(loaded, nil, nil, nil, nil, nil)
		} else {
			_, err = NewReleaseSteerer(loaded, nil, nil, nil, nil)
		}
		if err == nil || !strings.Contains(err.Error(), "owner-recycle successor activation is blocked") {
			t.Fatalf("compact=%v constructor admitted mainnet config: %v", compact, err)
		}
	}
}

// The bounded loader keeps duplicate, unknown and trailing YAML refusal.
func TestOwnerRecycleAdmissionConfigRejectsAmbiguousAndOversizedInput(t *testing.T) {
	fixture := newRecycleAdmissionFixture(t, nil)
	raw, err := yaml.Marshal(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{
		append(append([]byte{}, raw...), []byte("\n---\nproduction: true\n")...),
		append(append([]byte{}, raw...), []byte("\nproduction: true\n")...),
		append(append([]byte{}, raw...), []byte("\nunknown_authority: true\n")...),
		bytes.Repeat([]byte{' '}, maximumOwnerRecycleConfigBytes+1),
	} {
		path := filepath.Join(t.TempDir(), "synthetic-admission.yml")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOwnerRecycleAdmissionConfig(path); err == nil {
			t.Fatal("read-only loader accepted ambiguous or oversized configuration")
		}
	}
}

// Even the correct chain identity on another route has no selected authority.
func TestOwnerRecycleAdmissionRejectsUnapprovedConnectionBeforeRpc(t *testing.T) {
	fixture := newRecycleAdmissionFixture(t, nil)
	fixture.retain(t)
	fixture.chain.API.Client.(*recycleAdmissionRouteClient).route = "wss://unapproved.example"
	observation, err := ObserveOwnerRecycleAdmission(t.Context(), fixture.cfg, fixture.chain)
	if observation != nil || err == nil || !strings.Contains(err.Error(), "explicitly approved native route") || len(fixture.calls) != 0 {
		t.Fatalf("unapproved route observation=%+v calls=%v error=%v", observation, fixture.calls, err)
	}
}
