// Offline inspection tests use the real producer fixtures and deliberately
// absent keys. They do not grant a live runtime or invoke a network reader.
package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The projection agrees with normal producer admission but cannot return its
// private authority capsule. Declared evidence and signer files stay absent.
func TestProductionBootstrapInspectionMatchesProducerConfig(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	raw, err := os.ReadFile(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := InspectProductionBootstrapConfig(t.Context(), fixture.path, raw)
	if err != nil || got == nil {
		t.Fatal("valid signed production config refused", err)
	}
	if !reflect.DeepEqual(got.Approval, fixture.approval) || got.ValidatorId != fixture.cfg.ValidatorID || got.DeploymentId != fixture.cfg.DeploymentID ||
		got.ApprovalSigner != fixture.cfg.OwnerRecycleApproval.Signer || got.ApprovalReference != fixture.cfg.OwnerRecycleApproval.Approval ||
		got.DeclaredPaths[0] != fixture.cfg.StateDir || got.DeclaredPaths[1] != fixture.cfg.HotkeySeedFile {
		t.Fatalf("public projection differs from producer admission: %+v", got)
	}
	for _, path := range []string{fixture.cfg.HotkeySeedFile, fixture.cfg.StateDir, fixture.cfg.Operators[0].ClientKeySeedFile, fixture.cfg.EvidenceV2.Operators[0].Activation.Path} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("offline config inspection opened or created producer input", path, err)
		}
	}
	got.Approval.Production.ValidatorHotkeys[0][0] ^= 1
	again, err := InspectProductionBootstrapConfig(t.Context(), fixture.path, raw)
	if err != nil || !reflect.DeepEqual(again.Approval, fixture.approval) {
		t.Fatal("caller-edited facts contaminated another inspection", err)
	}
}

// The pathname changes deterministically after the caller captures exact bytes.
// Reopening here would silently bind an inspected plan to different input.
func TestProductionBootstrapInspectionConsumesPinnedBytes(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	raw, err := os.ReadFile(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.path, []byte("schema_version: 3\nunknown_substitution: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := InspectProductionBootstrapConfig(t.Context(), fixture.path, raw)
	if err != nil || got == nil || got.Approval.ConfigHash != fixture.approval.ConfigHash {
		t.Fatal("inspection reopened a substituted config pathname", err)
	}
	if cfg, err := LoadReleaseConfig(fixture.path); cfg != nil || err == nil {
		t.Fatal("substitution control did not reach the real strict decoder", err)
	}
}

// A producer's retained-source recovery is intentional but must not hide a
// changed provisioning input during independent initial-bootstrap review.
func TestProductionBootstrapInspectionRefusesRetainedApprovalFallback(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	raw, err := os.ReadFile(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	approvalRaw, err := os.ReadFile(fixture.cfg.OwnerRecycleApproval.Approval.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fixture.cfg.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	retained := retainedProductionApprovalPath(fixture.cfg)
	if err := os.WriteFile(retained, approvalRaw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.cfg.OwnerRecycleApproval.Approval.Path); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadReleaseConfig(fixture.path); err != nil || loaded == nil {
		t.Fatal("existing producer recovery was lost", err)
	}
	if got, err := InspectProductionBootstrapConfig(t.Context(), fixture.path, raw); got != nil || err == nil {
		t.Fatal("offline inspection substituted retained approval for absent declared source", err)
	}
	after, err := os.ReadFile(retained)
	if err != nil || !bytes.Equal(approvalRaw, after) {
		t.Fatal("inspection changed original retained authority", err)
	}
}

// The alias is rejected before any attempted approval read. All of these
// credential/evidence paths are absent, making an accidental read observable
// as an unrelated not-exist error instead of the required ownership refusal.
func TestProductionBootstrapInspectionRejectsApprovalCredentialAlias(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	for _, path := range []string{fixture.cfg.HotkeySeedFile, filepath.Join(fixture.cfg.StateDir, "approval.json"), fixture.cfg.Operators[0].ClientJWTFile, fixture.cfg.EvidenceV2.Operators[0].Context.Path} {
		cfg := *fixture.cfg
		selection := *cfg.OwnerRecycleApproval
		selection.Approval.Path = path
		cfg.OwnerRecycleApproval = &selection
		raw, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := InspectProductionBootstrapConfig(t.Context(), fixture.path, raw); got != nil || err == nil || !strings.Contains(err.Error(), "approval overlaps declared") {
			t.Fatal("credential alias reached an approval read", path, err)
		}
	}
}

// Initial preparation cannot inherit either observation authority or a writer's
// historical interval. Rejection occurs before the unavailable references open.
func TestProductionBootstrapInspectionRejectsHistoryAndOtherSchemas(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	for _, mutate := range []func(*ReleaseConfig){
		func(cfg *ReleaseConfig) { cfg.SchemaVersion = 1 },
		func(cfg *ReleaseConfig) { cfg.SchemaVersion = 2 },
		func(cfg *ReleaseConfig) {
			cfg.MainnetRuntimeApprovals = []ReleaseEvidenceV2File{{Path: "/synthetic-unread-observation"}}
		},
		func(cfg *ReleaseConfig) {
			cfg.ProductionRuntimeApprovals = []ReleaseEvidenceV2File{{Path: "/synthetic-unread-runtime"}}
		},
		func(cfg *ReleaseConfig) {
			cfg.ProductionAuthorityHistory = []ReleaseEvidenceV2File{{Path: "/synthetic-unread-authority"}}
		},
	} {
		cfg := *fixture.cfg
		mutate(&cfg)
		raw, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := InspectProductionBootstrapConfig(t.Context(), fixture.path, raw); got != nil || err == nil || !strings.Contains(err.Error(), "initial schema 3") {
			t.Fatal("inherited or observation authority reached initial bootstrap", err)
		}
	}
}

// Refusal never returns partially populated public facts, and a canceled caller
// cannot be revived by internal context.Background reads or retained fallbacks.
func TestProductionBootstrapInspectionBoundsCancellationAndStrictDecode(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	raw, err := os.ReadFile(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{nil, make([]byte, maximumReleaseConfigBytes+1), append(bytes.Clone(raw), []byte("\n---\n{}\n")...), append(bytes.Clone(raw), []byte("\nunknown_bootstrap_field: true\n")...)} {
		if got, err := InspectProductionBootstrapConfig(t.Context(), fixture.path, bad); got != nil || err == nil {
			t.Fatal("invalid document returned bootstrap facts", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := InspectProductionBootstrapConfig(ctx, fixture.path, raw); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled inspection returned facts", err)
	}
	for _, path := range []string{"relative.yml", filepath.Dir(fixture.path) + "/../foreign.yml"} {
		if got, err := InspectProductionBootstrapConfig(t.Context(), path, raw); got != nil || err == nil {
			t.Fatal("noncanonical config context admitted", err)
		}
	}
	if got, err := InspectProductionBootstrapConfig(nil, fixture.path, raw); got != nil || err == nil {
		t.Fatal("nil context admitted", err)
	}
}
