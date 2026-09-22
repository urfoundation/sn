//go:build linux || darwin

// Signed predecessor fixtures exercise exact lifetime expansion and rendering.
package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	validatorcomponent "github.com/urfoundation/sn/validator"
	"gopkg.in/yaml.v3"
)

// Build an explicit source successor using the genuine signed relay fixture.
func evidenceRelaySourceExpansionRequestTest(t *testing.T, fixture *runtimeEvidenceProvisionV2TestFixture, executor *Executor) EvidenceRelayContinuation {
	t.Helper()
	current := evidenceRelayExpansionRequestTest(t, executor)
	var err error
	current.SourceBounds, err = captureEvidenceRelaySourceBounds(fixture.cfg, executor.plan, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	current.Schema = evidenceRelayContinuationSourceExpansionSchema
	return current
}

// Complete original activation history before resolving successor runtime inputs.
func retainEvidenceRelaySourceExpansionSetupTest(t *testing.T, fixture *runtimeEvidenceProvisionV2TestFixture, executor *Executor) {
	t.Helper()
	if err := executor.journal.Close(); err != nil {
		t.Fatal(err)
	}
	retainRuntimeEvidenceSetupCarryV2Test(t, fixture)
	journal, err := OpenJournal(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	executor.journal = journal
}

// The approved lifetime covers full work while preserving finite exact bounds.
func TestEvidenceRelaySourceExpansionFitsFullWorkAndReportsExactFailure(t *testing.T) {
	t.Parallel()
	cfg := runtimeEvidenceLaunchConfigTest(t)
	original := cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds
	approved, err := doubledEvidenceRelaySourceBounds(original)
	if err != nil {
		t.Fatal(err)
	}
	// The retained campaign has already consumed most of its original lifetime.
	// Full finalization must still reserve every remaining block and restart tail.
	observed := validatorcomponent.StoppedAttemptLedgerCapacity{Head: validatorcomponent.AttemptLedgerHead{LastSequence: 525000, TrailCount: 66000, RecordBytes: 2_750_000_000}, StorageBytes: 1_200_000_000, StorageFiles: 600}
	err = validateEvidenceRelayContinuationCapacity(cfg, original, 7570, observed)
	if err == nil || !strings.Contains(err.Error(), "records: retained=525000 forecast=487936 limit=655360") || !strings.Contains(err.Error(), "trails: retained=66000 forecast=60992 limit=81920") {
		t.Fatal("full retained source did not report its exact limiting operands", err)
	}
	if err := validateEvidenceRelayContinuationCapacity(cfg, approved, 8500, observed); err != nil {
		t.Fatal("approved doubled lifetime cannot fit full work and preparation", err)
	}
	if approved.Disk.MaxRecordCount != 1310720 || approved.Disk.MaxTrailCount != 163840 || approved.Disk.MaxRawRecordBytes != 20*1024*1024*1024 || approved.Disk.MaxStorageBytes != 48*1024*1024*1024 || approved.Disk.MaxStorageFiles != 32768 {
		t.Fatal("source lifetime approval changed the exact approved 2x limits", approved.Disk)
	}
	if approved.Disk.MaxRecordBytes != original.Disk.MaxRecordBytes || approved.Disk.MaxLegacyBytes != original.Disk.MaxLegacyBytes || approved.Cut.MaxHeaderBytes != original.Cut.MaxHeaderBytes || approved.Replay.MaxRecordBytes != original.Replay.MaxRecordBytes || approved.Replay.MaxProofBytes != original.Replay.MaxProofBytes || approved.MaxControlBytes != original.MaxControlBytes || approved.Persistence != original.Persistence || approved.HeadEMA != original.HeadEMA {
		t.Fatal("lifetime expansion changed record, legacy, metadata or persistence policy")
	}
	observed.Head.LastSequence = approved.Disk.MaxRecordCount
	if err := validateEvidenceRelayContinuationCapacity(cfg, approved, 1, observed); err == nil {
		t.Fatal("expanded source lost its finite lifetime limit")
	}
}

// Real lineage and renderer paths keep original files and apply approved bounds.
func TestEvidenceRelaySourceExpansionPreservesLineageAndRendersApprovedBounds(t *testing.T) {
	t.Parallel()
	fixture, executor := newEvidenceRelayExpansionTest(t)
	retainEvidenceRelaySourceExpansionSetupTest(t, fixture, executor)
	beforeConfig, err := json.Marshal(fixture.cfg.Config)
	if err != nil {
		t.Fatal(err)
	}
	beforePlan, err := json.Marshal(executor.plan)
	if err != nil {
		t.Fatal(err)
	}
	original, err := runtimeEvidenceV2ResolvedConfig(fixture.cfg, fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	current := evidenceRelaySourceExpansionRequestTest(t, fixture, executor)
	plan, err := appendEvidenceRelayContinuationPlan(executor.plan, current)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ConfigHash != executor.plan.ConfigHash || !reflect.DeepEqual(plan.EvidenceRelayContinuation.Sources, executor.plan.EvidenceRelayContinuation.Sources) || current.NewSlots != 2044 || current.HistoricalLiabilityWei != "100000000000000000" {
		t.Fatal("source expansion changed original authority, retained roots or higher-fee liabilities")
	}
	if err := writeRunInputs(fixture.cfg, fixture.stateDir, plan, fixture.roles); err != nil {
		t.Fatal(err)
	}
	if err := validateEvidenceRelayContinuationSource(fixture.stateDir, plan, executor.journal.Entries()); err != nil {
		t.Fatal("source expansion lost authenticated predecessor ancestry", err)
	}
	resolved, err := runtimeEvidenceV2ResolvedConfig(fixture.cfg, fixture.stateDir)
	if err != nil {
		t.Fatal("approved source capacity did not reach actual runtime resolution", err)
	}
	again, err := runtimeEvidenceV2ResolvedConfig(resolved, fixture.stateDir)
	if err != nil || !reflect.DeepEqual(again.Config.ValidatorEvidenceV2, resolved.Config.ValidatorEvidenceV2) {
		t.Fatal("repeated runtime resolution applied another doubling", err)
	}
	for index, value := range resolved.Config.ValidatorEvidenceV2 {
		if !reflect.DeepEqual(value.Evidence.Bounds, current.SourceBounds[index].Approved) || !reflect.DeepEqual(value.Evidence.Operators, original.Config.ValidatorEvidenceV2[index].Evidence.Operators) {
			t.Fatal("expanded runtime changed immutable activation references or ignored approved bounds")
		}
		wire, err := marshalRuntimeValidatorConfig(resolved, fixture.stateDir, fixture.roles, map[string]any{}, index+1)
		if err != nil {
			t.Fatal(err)
		}
		var rendered struct {
			Evidence validatorcomponent.ReleaseEvidenceV2Config `yaml:"evidence_v2"`
		}
		if err := yaml.Unmarshal(wire, &rendered); err != nil || !reflect.DeepEqual(rendered.Evidence, value.Evidence) {
			t.Fatal("runtime writer did not use the same approved lifetime and signed source references", err)
		}
	}
	originalIdentity, err := runtimeEvidenceV2Identity(original)
	if err != nil {
		t.Fatal(err)
	}
	nextIdentity, err := runtimeEvidenceV2Identity(resolved)
	if err != nil || nextIdentity == originalIdentity {
		t.Fatal("runtime manifest identity failed to bind the new source capacity", err)
	}
	if _, err := applyEvidenceRelaySourceBounds(original.Config.ValidatorEvidenceV2, plan.EvidenceRelayContinuation); err != nil {
		t.Fatal("public replay could not derive approved bounds from the original authority", err)
	}
	forecast, err := evidenceRelaySourceCapacityConfig(fixture.cfg, plan)
	if err != nil || !reflect.DeepEqual(forecast.Config.ValidatorEvidenceV2[0].Evidence.Bounds, resolved.Config.ValidatorEvidenceV2[0].Evidence.Bounds) {
		t.Fatal("campaign source forecast did not use the exact approved runtime bounds", err)
	}
	afterConfig, _ := json.Marshal(fixture.cfg.Config)
	afterPlan, _ := json.Marshal(executor.plan)
	if !bytes.Equal(beforeConfig, afterConfig) || !bytes.Equal(beforePlan, afterPlan) {
		t.Fatal("successor mutated its original config or predecessor plan")
	}
}

// Refresh and import retain the exact original approval without compounding it.
func TestEvidenceRelaySourceExpansionRefreshAndImportCannotChangeApproval(t *testing.T) {
	t.Parallel()
	fixture, executor := newEvidenceRelayExpansionTest(t)
	current := evidenceRelaySourceExpansionRequestTest(t, fixture, executor)
	for _, multiplier := range []uint64{0, 2} {
		var pin *EvidenceRelayContinuation
		if multiplier == 0 {
			pin = &current
		}
		captured, err := captureEvidenceRelaySourceBounds(fixture.cfg, executor.plan, multiplier, pin)
		if err != nil || !reflect.DeepEqual(captured, current.SourceBounds) {
			t.Fatal("exact capture and import differ", err)
		}
	}
	changed := current
	changed.SourceBounds = append([]evidenceRelaySourceBounds(nil), current.SourceBounds...)
	changed.SourceBounds[0].Original.Disk.MaxRecordCount++
	if _, err := captureEvidenceRelaySourceBounds(fixture.cfg, executor.plan, 0, &changed); err == nil {
		t.Fatal("import replaced the authenticated original capacity")
	}
	plan, err := appendEvidenceRelayContinuationPlan(executor.plan, current)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRunInputs(fixture.cfg, fixture.stateDir, plan, fixture.roles); err != nil {
		t.Fatal(err)
	}
	executor.plan = plan
	next := evidenceRelayRefreshRequestTest(t, executor)
	next.Schema = evidenceRelayContinuationSourceExpansionSchema
	next.NewSlots, next.HistoricalLiabilityWei, err = next.remainingSlots()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appendEvidenceRelayContinuationPlan(plan, next); err != nil {
		t.Fatal("ordinary refresh could not retain the source revision", err)
	}
	for _, multiplier := range []uint64{0, 2} {
		retained, err := captureEvidenceRelaySourceBounds(fixture.cfg, plan, multiplier, nil)
		if err != nil || !reflect.DeepEqual(retained, current.SourceBounds) {
			t.Fatal("repeat capture multiplied the already expanded source", err)
		}
	}
	for _, mutate := range []func(*EvidenceRelayContinuation){
		func(c *EvidenceRelayContinuation) { c.SourceBounds = nil },
		func(c *EvidenceRelayContinuation) { c.Schema = evidenceRelayContinuationExpansionSchema },
		func(c *EvidenceRelayContinuation) { c.SourceBounds[0].Approved.Disk.MaxRecordCount++ },
		func(c *EvidenceRelayContinuation) {
			c.SourceBounds[0].Original = c.SourceBounds[0].Approved
			c.SourceBounds[0].Approved, _ = doubledEvidenceRelaySourceBounds(c.SourceBounds[0].Original)
		},
	} {
		changed := next
		changed.SourceBounds = append([]evidenceRelaySourceBounds(nil), next.SourceBounds...)
		mutate(&changed)
		if _, err := appendEvidenceRelayContinuationPlan(plan, changed); err == nil {
			t.Fatal("refresh removed, altered or doubled source approval again")
		}
	}
	values := append([]validatorcomponent.ReleaseValidatorEvidenceV2Config(nil), fixture.cfg.Config.ValidatorEvidenceV2...)
	values[0].Evidence.Bounds.Disk.MaxRecordBytes++
	if _, err := applyEvidenceRelaySourceBounds(values, &current); err == nil {
		t.Fatal("runtime accepted unrelated source policy drift")
	}
}

// Only an explicit continuation request may introduce the source expansion.
func TestEvidenceRelaySourceExpansionRequiresExplicitCaptureOption(t *testing.T) {
	t.Parallel()
	for _, sample := range []struct {
		command string
		options cliOptions
		valid   bool
	}{
		{"relay-continuation", cliOptions{RelayEndBlock: 100, RelaySlots: 2048, RelaySourceLimitMultiplier: 2}, true},
		{"relay-continuation", cliOptions{RelayEndBlock: 100, RelaySourceLimitMultiplier: 3}, false},
		{"relay-continuation", cliOptions{RelayContinuationPlan: filepath.Join(t.TempDir(), "plan.json"), RelaySourceLimitMultiplier: 2}, false},
		{"resume", cliOptions{RelaySourceLimitMultiplier: 2}, false},
	} {
		if err := validateEvidenceRelayContinuationOptions(sample.command, sample.options); (err == nil) != sample.valid {
			t.Fatalf("source expansion option: valid=%v error=%v", sample.valid, err)
		}
	}
	cfg := runtimeEvidenceLaunchConfigTest(t)
	if _, err := captureEvidenceRelaySourceBounds(cfg, &SetupPlan{}, 2, nil); err == nil {
		t.Fatal("fresh setup acquired an unsigned source lifetime expansion")
	}
}
