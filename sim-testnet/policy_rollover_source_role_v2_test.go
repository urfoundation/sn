//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	validatorcomponent "github.com/urfoundation/sn/validator"
	"gopkg.in/yaml.v3"
)

// These tests exercise deployment approval, immutable references, one-field
// config projection, and publication ordering. Validator tests separately
// authenticate genuine signed envelopes, atomic extrinsics, and native slots.
func newPolicyRolloverSourceRoleTestV2(t *testing.T, configure ...func(*policyRolloverGenerationTestV2)) (*policyRolloverGenerationTestV2, *policyRolloverHandoffV2, policyRolloverSourceRoleIOV2) {
	t.Helper()
	g, p, h, j := newPolicyRolloverHandoffTestV2(t, configure...)
	activatePolicyRolloverHandoffTestV2(t, g, p, h, j)
	f := g.fixture
	h, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil {
		t.Fatal(err)
	}
	descriptors := map[uint64][]byte{}
	for _, owner := range h.Validators {
		measurement, envelope := []byte("retained-measurement"), []byte("retained-signed-envelope")
		measurementHash, envelopeHash := bytesSHA256(measurement), bytesSHA256(envelope)
		measurementRelative := filepath.Join("measurements", strings.TrimPrefix(measurementHash, "sha256:")+".json")
		envelopeRelative := filepath.Join("measurements", "envelopes", strings.TrimPrefix(envelopeHash, "sha256:")+".json")
		proof := validatorcomponent.ReleaseSourceRolePredecessorV2{Schema: validatorcomponent.ReleaseSourceRolePredecessorV2Schema,
			Intent:      validatorcomponent.SteeringIntent{ValidatorID: owner.ValidatorID, MeasurementArtifactHash: measurementHash, MeasurementArtifactPath: filepath.ToSlash(measurementRelative), MeasurementEnvelopeHash: envelopeHash, MeasurementEnvelopePath: filepath.ToSlash(envelopeRelative)},
			Measurement: policyRolloverFile(filepath.Join(owner.PreviousStateDir, measurementRelative), measurement), Envelope: policyRolloverFile(filepath.Join(owner.PreviousStateDir, envelopeRelative), envelope)}
		for _, file := range []struct {
			path string
			raw  []byte
		}{{proof.Measurement.Path, measurement}, {proof.Envelope.Path, envelope}, {filepath.Join(owner.PreviousStateDir, "steering-intents.json"), []byte("retained-intent-store")}} {
			if _, err := validatorcomponent.WriteReleaseEvidenceV2File(t.Context(), file.path, file.raw, 1<<20); err != nil {
				t.Fatal(err)
			}
			g.original[file.path] = file.raw
		}
		raw, err := json.MarshalIndent(proof, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		descriptors[owner.ValidatorID] = append(raw, '\n')
	}
	io := policyRolloverSourceRoleIOV2{
		prepare: func(_ context.Context, config *validatorcomponent.ReleaseConfig, previous string) ([]byte, error) {
			if previous != h.Validators[config.ValidatorID-1].PreviousStateDir {
				t.Fatal("preparation changed previous state owner")
			}
			return bytes.Clone(descriptors[config.ValidatorID]), nil
		},
		decode: func(ctx context.Context, config *validatorcomponent.ReleaseConfig, raw []byte) (*validatorcomponent.ReleaseSourceRolePredecessorV2, error) {
			if !bytes.Equal(raw, descriptors[config.ValidatorID]) {
				return nil, errors.New("descriptor changed")
			}
			var proof validatorcomponent.ReleaseSourceRolePredecessorV2
			if err := json.Unmarshal(raw, &proof); err != nil {
				return nil, err
			}
			for _, reference := range []validatorcomponent.ReleaseEvidenceV2File{proof.Measurement, proof.Envelope} {
				if _, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, reference, 1<<20); err != nil {
					return nil, err
				}
			}
			return &proof, nil
		},
	}
	return g, h, io
}

func TestPolicyRolloverSourceRoleOptionsRequireExplicitActiveGeneration(t *testing.T) {
	base := cliOptions{RolloverSourceRole: true, ProvisionalResume: true, PlanHash: "0x" + strings.Repeat("1", 64)}
	if err := validatePolicyRolloverOptionsV2("policy-rollover", base); err != nil {
		t.Fatal(err)
	}
	for _, alter := range []func(*cliOptions){
		func(o *cliOptions) { o.ProvisionalResume = false },
		func(o *cliOptions) { o.RolloverGeneration = 1 },
		func(o *cliOptions) { o.RolloverEpoch = 10 },
		func(o *cliOptions) { o.Apply = true },
		func(o *cliOptions) { o.RolloverPlan = "relative.json" },
		func(o *cliOptions) { o.Detach = true },
	} {
		options := base
		alter(&options)
		if err := validatePolicyRolloverOptionsV2("policy-rollover", options); err == nil {
			t.Fatalf("unsafe role continuation flags accepted: %+v", options)
		}
	}
	if err := validatePolicyRolloverOptionsV2("scenario", base); err == nil {
		t.Fatal("role-only flag accepted outside its command")
	}
	base.Apply, base.RolloverPlan, base.RolloverPlanHash = true, "/reviewed/plan.json", base.PlanHash
	if err := validatePolicyRolloverOptionsV2("policy-rollover", base); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyRolloverSourceRolePlansPreserveOriginalGeneration(t *testing.T) {
	g, h, io := newPolicyRolloverSourceRoleTestV2(t)
	f := g.fixture
	for _, owner := range h.Validators {
		raw, err := os.ReadFile(owner.Config.Path)
		if err != nil {
			t.Fatal(err)
		}
		g.original[owner.Config.Path] = raw
	}
	path := filepath.Join(policyRolloverRoot(f.stateDir), "handoff.json")
	g.original[path], _ = os.ReadFile(path)
	p, err := preparePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, h, io)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := preparePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, h, io)
	if err != nil || !reflect.DeepEqual(retry, p) {
		t.Fatalf("exact plan retry changed: %v", err)
	}
	if !p.RoleOnly || p.StateImported || p.LedgerContinuityClaimed || p.OriginalHandoffSHA256 != h.sourceSHA256 {
		t.Fatal("role proof claimed generation continuity or state import")
	}
	selected, err := readPolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil || selected.SourceRoleOverlay != nil || !reflect.DeepEqual(selected, h) {
		t.Fatalf("unapproved plan selected the new config: %v", err)
	}
	for path, want := range g.original {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("original state or config changed: %s: %v", path, err)
		}
	}
	if err := validatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, h, p, policyRolloverSourceRoleIO()); err == nil {
		t.Fatal("real descriptor decoder accepted the synthetic harness-only fixture")
	}
}

func TestPolicyRolloverSourceRoleApprovalBindsOneFieldAndRetainedFiles(t *testing.T) {
	g, h, io := newPolicyRolloverSourceRoleTestV2(t)
	f := g.fixture
	p, err := preparePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, h, io)
	if err != nil {
		t.Fatal(err)
	}
	limit, _ := runtimeEvidenceProvisionLimit(f.cfg)
	for _, tc := range []struct {
		name   string
		change func(*policyRolloverSourceRolePlanV2)
	}{
		{"generation", func(p *policyRolloverSourceRolePlanV2) { p.Generation++ }},
		{"handoff", func(p *policyRolloverSourceRolePlanV2) { p.OriginalHandoffSHA256 = bytesSHA256([]byte("other")) }},
		{"plan", func(p *policyRolloverSourceRolePlanV2) { p.SourcePlanHash = "0x" + strings.Repeat("1", 64) }},
		{"policy", func(p *policyRolloverSourceRolePlanV2) { p.PolicyHash = "0x" + strings.Repeat("1", 64) }},
		{"state import", func(p *policyRolloverSourceRolePlanV2) { p.StateImported = true }},
		{"strict acceptance", func(p *policyRolloverSourceRolePlanV2) { p.FinalAcceptance = true }},
		{"provisional marker", func(p *policyRolloverSourceRolePlanV2) { p.Provisional = false }},
		{"ledger continuity", func(p *policyRolloverSourceRolePlanV2) { p.LedgerContinuityClaimed = true }},
		{"previous namespace", func(p *policyRolloverSourceRolePlanV2) { p.Validators[0].PreviousStateDir += "-other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutated := *p
			mutated.Validators = append([]policyRolloverSourceRoleValidatorV2(nil), p.Validators...)
			tc.change(&mutated)
			mutated.PlanHash, _ = mutated.hash()
			if err := validatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, h, &mutated, io); err == nil {
				t.Fatal("rehashing admitted a changed approval domain")
			}
		})
	}
	for _, reference := range []validatorcomponent.ReleaseEvidenceV2File{*p.Validators[0].Predecessor, *p.Validators[0].SourceIntents} {
		before, _ := os.ReadFile(reference.Path)
		if err := os.WriteFile(reference.Path, []byte("changed"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := validatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, h, p, io); err == nil {
			t.Fatal("changed descriptor or source intent store accepted")
		}
		if err := os.WriteFile(reference.Path, before, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	originalWire, _ := os.ReadFile(p.Validators[0].Config.Path)
	config, err := policyRolloverSourceRoleConfigV2(t.Context(), p.Validators[0].Config, limit)
	if err != nil {
		t.Fatal(err)
	}
	config.StateDir += "-imported-state"
	wire, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Validators[0].Config.Path, wire, 0o600); err != nil {
		t.Fatal(err)
	}
	mutated := *p
	mutated.Validators = append([]policyRolloverSourceRoleValidatorV2(nil), p.Validators...)
	mutated.Validators[0].Config = policyRolloverFile(p.Validators[0].Config.Path, wire)
	mutated.PlanHash, _ = mutated.hash()
	if err := validatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, h, &mutated, io); err == nil {
		t.Fatal("role overlay changed fresh state namespace after rehashing")
	}
	if err := os.WriteFile(p.Validators[0].Config.Path, originalWire, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyRolloverSourceRoleActivationRequiresStoppedAndCurrentProof(t *testing.T) {
	g, h, io := newPolicyRolloverSourceRoleTestV2(t)
	f := g.fixture
	p, err := preparePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, h, io)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(policyRolloverSourceRoleRootV2(f.stateDir, h.Generation), "handoff.evidence.json")
	lock, err := os.OpenFile(filepath.Join(f.stateDir, "supervisor.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	called := 0
	verify := func(context.Context, *validatorcomponent.ReleaseConfig) error { called++; return nil }
	if _, err := activatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, h, p, io, verify); err == nil || called != 0 {
		t.Fatalf("active supervisor admitted role handoff: calls=%d err=%v", called, err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	stale := errors.New("current finalized source slot changed")
	if _, err := activatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, h, p, io, func(context.Context, *validatorcomponent.ReleaseConfig) error { return stale }); !errors.Is(err, stale) {
		t.Fatalf("native refusal was lost: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed native proof published an activation receipt")
	}
	selected, err := activatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, h, p, io, verify)
	if err != nil || selected.SourceRoleOverlay == nil || called != 2 {
		t.Fatalf("verified role continuation failed: calls=%d err=%v", called, err)
	}
	for index, owner := range selected.Validators {
		if owner.Config != p.Validators[index].Config || !reflect.DeepEqual(owner.SourceRolePredecessorV2, p.Validators[index].Predecessor) || owner.StateDir != h.Validators[index].StateDir || owner.Evidence.Bounds != h.Validators[index].Evidence.Bounds {
			t.Fatal("selected role config changed source authority")
		}
	}
	receipt, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := activatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, h, p, io, func(context.Context, *validatorcomponent.ReleaseConfig) error {
		t.Fatal("exact retry rechecked a later overwritten slot")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	retry, _ := os.ReadFile(path)
	if !bytes.Equal(receipt, retry) {
		t.Fatal("retry replaced owner signature or original activation time")
	}
	if _, err := readPolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan); err == nil {
		t.Fatal("real selector bypassed production descriptor authentication")
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPolicyRolloverSourceRoleOverlayWithV2(t.Context(), f.cfg, f.stateDir, f.plan, h, io); err == nil {
		t.Fatal("malformed selected role receipt fell back to original config")
	}
}

func TestPolicyRolloverSourceRoleSelectedReceiptRejectsOtherOwner(t *testing.T) {
	g, h, io := newPolicyRolloverSourceRoleTestV2(t)
	f := g.fixture
	p, err := preparePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, h, io)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signEvidence(f.cfg, policyRolloverSourceRoleKindV2, p.PlanHash, p, f.roles.EVM["keeper"])
	if err != nil {
		t.Fatal(err)
	}
	limit, _ := runtimeEvidenceProvisionLimit(f.cfg)
	path := filepath.Join(policyRolloverSourceRoleRootV2(f.stateDir, h.Generation), "handoff.evidence.json")
	if _, err := writeRuntimeEvidenceSetupV2(t.Context(), path, signed, limit); err != nil {
		t.Fatal(err)
	}
	if _, err := readPolicyRolloverSourceRoleOverlayWithV2(t.Context(), f.cfg, f.stateDir, f.plan, h, io); err == nil || !strings.Contains(err.Error(), "deployment owner") {
		t.Fatalf("different signed role admitted: %v", err)
	}
}

func TestPolicyRolloverSourceRoleAbsentPredecessorStillChecksNativeSlot(t *testing.T) {
	g, h, io := newPolicyRolloverSourceRoleTestV2(t)
	f := g.fixture
	prepare := io.prepare
	io.prepare = func(ctx context.Context, config *validatorcomponent.ReleaseConfig, previous string) ([]byte, error) {
		if config.ValidatorID == 2 {
			return nil, nil
		}
		return prepare(ctx, config, previous)
	}
	p, err := preparePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, h, io)
	if err != nil {
		t.Fatal(err)
	}
	if p.Validators[1].Predecessor != nil || p.Validators[1].Config != h.Validators[1].Config {
		t.Fatal("absent predecessor changed its config")
	}
	called := []uint64{}
	occupied := errors.New("unexplained occupied native slot")
	_, err = activatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, h, p, io, func(_ context.Context, config *validatorcomponent.ReleaseConfig) error {
		called = append(called, config.ValidatorID)
		if config.ValidatorID == 2 && config.SourceRolePredecessorV2 == nil {
			return occupied
		}
		return nil
	})
	if !errors.Is(err, occupied) || !reflect.DeepEqual(called, []uint64{1, 2}) {
		t.Fatalf("absent descriptor bypassed current slot verification: calls=%v err=%v", called, err)
	}
	path := filepath.Join(policyRolloverSourceRoleRootV2(f.stateDir, h.Generation), "handoff.evidence.json")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unexplained native slot published role selection")
	}
}
