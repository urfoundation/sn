//go:build linux || darwin

// Real immutable configs, dual signatures and journal receipts force the
// generation transition without network calls or scheduling-dependent waits.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

// A fresh second-generation ledger keeps validator one's empty role and binds
// validator two's occupied native slot to its exact first-generation receipt.
func TestPolicyRolloverSuccessorSourceRoleSelectsCurrentGeneration(t *testing.T) {
	g, plan, handoff, journal, original := newPolicyRolloverSuccessorTestV2(t)
	f := g.fixture
	limit, _ := runtimeEvidenceProvisionLimit(f.cfg)
	if err := activatePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan, plan, handoff, journal, limit); err != nil {
		t.Fatal(err)
	}
	selected, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil {
		t.Fatal(err)
	}
	io := policyRolloverSourceRoleTestIoV2(t, g, selected)
	prepare := io.prepare
	io.prepare = func(ctx context.Context, config *validatorcomponent.ReleaseConfig, previous string) ([]byte, error) {
		if config.ValidatorID == 1 {
			return nil, nil
		}
		return prepare(ctx, config, previous)
	}
	if err := os.Remove(filepath.Join(selected.Validators[0].PreviousStateDir, "steering-intents.json")); err != nil {
		t.Fatal(err)
	}
	rolePlan, err := preparePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, selected, io)
	if err != nil {
		t.Fatal(err)
	}
	if rolePlan.Generation != 2 || rolePlan.Validators[0].Predecessor != nil || rolePlan.Validators[1].Predecessor == nil || rolePlan.Validators[1].PreviousStateDir != selected.predecessor.Validators[1].StateDir {
		t.Fatal("role approval selected a stale generation or changed the V1/V2 predecessor census")
	}
	wrongSlot := errors.New("current native slot differs from exact V2 predecessor")
	if _, err := activatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, selected, rolePlan, io, func(context.Context, *validatorcomponent.ReleaseConfig) error { return wrongSlot }); !errors.Is(err, wrongSlot) {
		t.Fatal("native slot refusal did not block selecting the overlay", err)
	}
	stillBase, err := readPolicyRolloverSourceRoleOverlayWithV2(t.Context(), f.cfg, f.stateDir, f.plan, selected, io)
	if err != nil || stillBase.SourceRoleOverlay != nil {
		t.Fatal("failed native verification selected the role overlay", err)
	}
	var verified []uint64
	active, err := activatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, selected, rolePlan, io, func(_ context.Context, config *validatorcomponent.ReleaseConfig) error {
		if config.StateDir != handoff.Validators[config.ValidatorID-1].StateDir || (config.SourceRolePredecessorV2 == nil) != (config.ValidatorID == 1) {
			t.Fatal("native verifier received a stale config or the wrong predecessor")
		}
		verified = append(verified, config.ValidatorID)
		return nil
	})
	if err != nil || active.SourceRoleOverlay == nil || !slices.Equal(verified, []uint64{1, 2}) || active.Generation != 2 || active.predecessor.Generation != 1 {
		t.Fatal("generation-two role overlay failed to authenticate both native owners", err)
	}
	baseAgain, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil {
		t.Fatal(err)
	}
	read, err := readPolicyRolloverSourceRoleOverlayWithV2(t.Context(), f.cfg, f.stateDir, f.plan, baseAgain, io)
	if err != nil || read.Validators[1].Config != rolePlan.Validators[1].Config || read.Validators[0].Config != handoff.Validators[0].Config || read.predecessor.Generation != 1 {
		t.Fatal("role reader changed the successor's retained chain or V1/V2 config selection", err)
	}
	retained, err := os.ReadFile(filepath.Join(policyRolloverRoot(f.stateDir), "handoff.json"))
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatal("successor role overlay changed the original generation", err)
	}
}

// Re-key the synthetic deployment while retaining both actual signing owners.
func rekeyPolicyRolloverTestV2(t *testing.T, generation *policyRolloverGenerationTestV2, id, epoch uint64) {
	t.Helper()
	roles, err := policyRolloverGenerationRolesV2(generation.fixture.cfg, generation.fixture.roles, id)
	if err != nil {
		t.Fatal(err)
	}
	generation.generation, generation.roles = id, roles
	for index := range generation.members {
		member := &generation.members[index]
		hotkey, key, err := runtimeEvidenceActivationKeysV2(roles, member.ValidatorId, member.NoId)
		if err != nil {
			t.Fatal(err)
		}
		member.Activation.VPK = [32]byte(key[32:])
		member.Activation.Domain.Epoch = epoch
		member.VpkSignature, err = member.Activation.SignVPK(key)
		if err != nil {
			t.Fatal(err)
		}
		digest, err := member.Activation.Digest()
		if err != nil {
			t.Fatal(err)
		}
		member.HotkeySignature, err = hotkey.Sign(digest[:])
		if err != nil {
			t.Fatal(err)
		}
	}
}

// The first generation is fully activated; the second has finalized four
// publications and staged independent inputs but has no activation commit.
func newPolicyRolloverSuccessorTestV2(t *testing.T) (*policyRolloverGenerationTestV2, *policyRolloverPlanV2, *policyRolloverHandoffV2, *Journal, []byte) {
	t.Helper()
	first, firstPlan, firstHandoff, journal := newPolicyRolloverHandoffTestV2(t, func(g *policyRolloverGenerationTestV2) {
		rekeyPolicyRolloverTestV2(t, g, 1, g.members[0].Activation.Domain.Epoch)
	})
	activatePolicyRolloverHandoffTestV2(t, first, firstPlan, firstHandoff, journal)
	f := first.fixture
	originalPath := filepath.Join(policyRolloverRoot(f.stateDir), "handoff.json")
	original, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil {
		t.Fatal(err)
	}
	next := &policyRolloverGenerationTestV2{fixture: f, members: slices.Clone(first.members), original: first.original}
	rekeyPolicyRolloverTestV2(t, next, 2, firstPlan.Epoch+1)
	next.provision(t)
	validators, err := next.stage(t)
	if err != nil {
		t.Fatal(err)
	}
	limit, err := runtimeEvidenceProvisionLimit(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := policyRolloverHandoffReferenceV2(t.Context(), prior, limit)
	if err != nil {
		t.Fatal(err)
	}
	plan := *firstPlan
	plan.Generation, plan.Epoch, plan.PreviousHandoff = 2, firstPlan.Epoch+1, &reference
	plan.SourceJournalHash = journal.Entries()[len(journal.Entries())-1].EntryHash
	plan.Members, plan.Validators = next.members, nil
	for index, previous := range prior.Validators {
		raw, err := validatorcomponent.ReadReleaseEvidenceV2File(t.Context(), previous.Config, limit)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(policyRolloverPlanRootV2(f.stateDir, plan.Generation, plan.Epoch), "source", fmt.Sprintf("validator-%d.yml", previous.ValidatorID))
		if _, err := validatorcomponent.WriteReleaseEvidenceV2File(t.Context(), path, raw, limit); err != nil {
			t.Fatal(err)
		}
		checkpoint := policyRolloverValidatorCheckpointV2{ValidatorID: previous.ValidatorID, Config: policyRolloverFile(path, raw), StateDir: previous.StateDir}
		for _, member := range prior.Members[index*2 : index*2+2] {
			checkpoint.PreviousActivations = append(checkpoint.PreviousActivations, member.Activation)
		}
		plan.Validators = append(plan.Validators, checkpoint)
		validators[index].PreviousStateDir = previous.StateDir
	}
	plan.Actions, err = policyRolloverActionsV2(&plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePolicyRolloverPlanV2(f.cfg, f.plan, f.stateDir, &plan); err != nil {
		t.Fatal(err)
	}
	if _, err := writeRuntimeEvidenceSetupV2(t.Context(), policyRolloverPlanPathV2(f.stateDir, plan.Generation, plan.Epoch), &plan, limit); err != nil {
		t.Fatal(err)
	}
	if _, err := publishPolicyRolloverV2(t.Context(), &plan, journal, limit, policyRolloverPublicationIOV2{
		Observe: func(context.Context, runtimeEvidenceActivationMemberV2) (uint64, error) {
			return f.completed.Boundary.Number, nil
		},
		Send: func(context.Context, Action, runtimeEvidenceActivationMemberV2) error {
			t.Fatal("finalized fixture broadcast")
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	handoff := &policyRolloverHandoffV2{Schema: policyRolloverHandoffV2Schema, PlanHash: plan.PlanHash, SourcePlanHash: plan.SourcePlanHash, PreviousHandoff: plan.PreviousHandoff, DeploymentID: plan.DeploymentID, Generation: plan.Generation, CutoffEpoch: plan.Epoch, FirstFullEpoch: plan.Epoch + 1, Native: plan.Native, EVM: plan.EVM, Boundary: f.completed.Boundary, Members: plan.Members, Validators: validators, Identities: validators[0].Identities}
	return next, &plan, handoff, journal, original
}

// The former fixed-path writer deterministically rejects this second commit.
func TestPolicyRolloverSuccessorFixedPathCollision(t *testing.T) {
	g, plan, handoff, journal, original := newPolicyRolloverSuccessorTestV2(t)
	f := g.fixture
	limit, _ := runtimeEvidenceProvisionLimit(f.cfg)
	beforeFiles := map[string]os.FileMode{}
	if err := addPolicyRolloverGenerationRuntimeInputsV2(t.Context(), f.cfg, f.stateDir, f.plan, beforeFiles, policyRolloverSourceRoleIO()); err != nil {
		t.Fatal(err)
	}
	sealedFiles := make([]RuntimeConfigFile, 0, len(beforeFiles))
	for relative, mode := range beforeFiles {
		digest, observedMode, err := runtimeConfigFileDigest(filepath.Join(f.stateDir, filepath.FromSlash(relative)))
		if err != nil || observedMode != mode {
			t.Fatal("original runtime input unavailable", err)
		}
		sealedFiles = append(sealedFiles, RuntimeConfigFile{Path: relative, SHA256: digest, Mode: fmt.Sprintf("%04o", mode)})
	}
	if err := activatePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan, plan, handoff, journal, limit); err != nil {
		t.Fatalf("second generation activation: %v", err)
	}
	selected, err := readPolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil || selected == nil || selected.Generation != 2 || selected.predecessor == nil || selected.predecessor.Generation != 1 {
		t.Fatalf("successor selection: %+v %v", selected, err)
	}
	retained, err := os.ReadFile(filepath.Join(policyRolloverRoot(f.stateDir), "handoff.json"))
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatal("original generation custody changed", err)
	}
	before := len(journal.Entries())
	if err := activatePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan, plan, handoff, journal, limit); err != nil || len(journal.Entries()) != before {
		t.Fatal("exact activation retry changed custody", err)
	}
	afterFiles := map[string]os.FileMode{}
	if err := addPolicyRolloverGenerationRuntimeInputsV2(t.Context(), f.cfg, f.stateDir, f.plan, afterFiles, policyRolloverSourceRoleIO()); err != nil || !reflect.DeepEqual(beforeFiles, afterFiles) {
		t.Fatal("successor changed the sealed runtime manifest inventory", err)
	}
	for _, file := range sealedFiles {
		if err := verifyRuntimeConfigManifestFile(f.cfg, f.stateDir, file, afterFiles[file.Path]); err != nil {
			t.Fatal("retained resume changed original manifest bytes", err)
		}
	}
	contexts, err := runtimeReservedAttemptUploadContexts(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil || len(contexts) != 4 {
		t.Fatal("successor API context census", err)
	}
	for index, got := range contexts {
		if got != handoff.Validators[index/2].Evidence.Operators[index%2].Context {
			t.Fatal("API retained stale source")
		}
	}
	specs := []ProcessSpec{{ID: "validator-1", Role: "validator", Args: []string{"__validator", "--config=prior"}}, {ID: "validator-2", Role: "validator", Args: []string{"__validator", "--config=prior"}}}
	if err := attachPolicyRolloverProcessConfigsV2(t.Context(), f.cfg, f.stateDir, f.plan, specs); err != nil {
		t.Fatal(err)
	}
	for index, spec := range specs {
		if !slices.Contains(spec.Args, "--config="+handoff.Validators[index].Config.Path) {
			t.Fatal("worker retained stale config")
		}
	}
}

// A prepared postcondition is not a commit; publishing the receipt is the
// durable selector and an exact crash retry may consume the same static bytes.
func TestPolicyRolloverSuccessorStagingDoesNotSelect(t *testing.T) {
	g, plan, handoff, journal, _ := newPolicyRolloverSuccessorTestV2(t)
	f := g.fixture
	limit, _ := runtimeEvidenceProvisionLimit(f.cfg)
	handoff.Activated = true
	path, err := postconditionRelativePath(plan.PlanHash, "evidence.policy-rollover-handoff")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeRuntimeEvidenceSetupV2(t.Context(), filepath.Join(f.stateDir, path), handoff, limit); err != nil {
		t.Fatal(err)
	}
	selected, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil || selected.Generation != 1 {
		t.Fatal("staged bytes changed selection", err)
	}
	if err := activatePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan, plan, handoff, journal, limit); err != nil {
		t.Fatal(err)
	}
	selected, err = readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil || selected.Generation != 2 {
		t.Fatal("committed successor unavailable", err)
	}
}

// Wrong predecessor and cutoff approvals fail before a durable activation row.
func TestPolicyRolloverSuccessorRejectsChangedApproval(t *testing.T) {
	g, plan, handoff, journal, _ := newPolicyRolloverSuccessorTestV2(t)
	f := g.fixture
	limit, _ := runtimeEvidenceProvisionLimit(f.cfg)
	before := len(journal.Entries())
	changed := *plan
	reference := *changed.PreviousHandoff
	reference.SHA256 = "0x" + string(bytes.Repeat([]byte{'a'}, 64))
	changed.PreviousHandoff = &reference
	if err := activatePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan, &changed, handoff, journal, limit); err == nil {
		t.Fatal("wrong predecessor accepted")
	}
	modified := *handoff
	modified.CutoffEpoch--
	if err := activatePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan, plan, &modified, journal, limit); err == nil {
		t.Fatal("changed cutoff accepted")
	}
	if len(journal.Entries()) != before {
		t.Fatal("rejected approval wrote activation")
	}
}

// A live validator record rejects activation even when every static input is ready.
func TestPolicyRolloverSuccessorRequiresStoppedTopology(t *testing.T) {
	g, plan, handoff, journal, _ := newPolicyRolloverSuccessorTestV2(t)
	f := g.fixture
	limit, _ := runtimeEvidenceProvisionLimit(f.cfg)
	raw, err := json.Marshal(SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", Processes: []ProcessState{{ID: "validator-1", PID: os.Getpid()}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.stateDir, "supervisor.state.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := activatePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan, plan, handoff, journal, limit); err == nil || !strings.Contains(err.Error(), "validator 1 legacy state cannot be quarantined") {
		t.Fatal("live validator record did not block generation activation", err)
	}
}

// A duplicate committed selector cannot silently choose or roll back a source.
func TestPolicyRolloverSuccessorRejectsDuplicateSelector(t *testing.T) {
	g, plan, handoff, journal, original := newPolicyRolloverSuccessorTestV2(t)
	f := g.fixture
	limit, _ := runtimeEvidenceProvisionLimit(f.cfg)
	if err := activatePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan, plan, handoff, journal, limit); err != nil {
		t.Fatal(err)
	}
	entries := journal.Entries()
	entries = append(entries, entries[len(entries)-1])
	if _, err := readPolicyRolloverSuccessorsV2(t.Context(), f.cfg, f.stateDir, f.plan, original, entries); err == nil {
		t.Fatal("duplicate selector accepted")
	}
}

// Malformed committed bytes fail closed instead of reverting to generation one.
func TestPolicyRolloverSuccessorRejectsTornPostcondition(t *testing.T) {
	g, plan, handoff, journal, _ := newPolicyRolloverSuccessorTestV2(t)
	f := g.fixture
	limit, _ := runtimeEvidenceProvisionLimit(f.cfg)
	if err := activatePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan, plan, handoff, journal, limit); err != nil {
		t.Fatal(err)
	}
	entry := journal.Entries()[len(journal.Entries())-1]
	if err := os.WriteFile(filepath.Join(f.stateDir, entry.PostconditionPath), []byte("{\"schema\":"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan); err == nil {
		t.Fatal("torn postcondition fell back")
	}
	if err := os.Remove(filepath.Join(policyRolloverRoot(f.stateDir), "handoff.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := readPolicyRolloverObservationV2(t.Context(), f.cfg, f.stateDir); err == nil {
		t.Fatal("missing committed selector fell back to original observations")
	}
	if _, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan); err == nil {
		t.Fatal("missing committed selector fell back to original runtime")
	}

}

// A partially appended commit cannot select either a guessed new generation
// or the older one; the journal reader owns recovery of its exact suffix.
func TestPolicyRolloverSuccessorRejectsTornJournalSelector(t *testing.T) {
	g, plan, handoff, journal, _ := newPolicyRolloverSuccessorTestV2(t)
	f := g.fixture
	limit, _ := runtimeEvidenceProvisionLimit(f.cfg)
	if err := activatePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan, plan, handoff, journal, limit); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(f.stateDir, "journal.jsonl"), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString("{\"schema\":")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal(writeErr, closeErr)
	}
	if _, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan); err == nil {
		t.Fatal("torn selector silently selected a generation")
	}
}

// Successor custody is checked independently of the unchanged original manifest.
func TestPolicyRolloverSuccessorRejectsChangedNativeCustody(t *testing.T) {
	g, plan, handoff, journal, _ := newPolicyRolloverSuccessorTestV2(t)
	f := g.fixture
	limit, _ := runtimeEvidenceProvisionLimit(f.cfg)
	if err := activatePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan, plan, handoff, journal, limit); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(handoff.Validators[0].Config.Path), "hotkey.seed")
	hexSeed, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(hexSeed, []byte("0x")) || len(hexSeed) != 66 {
		t.Fatal("native seed fixture must exercise its provisioned hex encoding", err)
	}
	if selected, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan); err != nil || selected.Generation != 2 {
		t.Fatal("authenticated reader rejected the provisioned native hex seed", err)
	}
	rawSeed, err := hex.DecodeString(string(hexSeed[2:]))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, rawSeed, 0o600); err != nil {
		t.Fatal(err)
	}
	if selected, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan); err != nil || selected.Generation != 2 {
		t.Fatal("authenticated reader rejected the same native raw seed", err)
	}
	clientPath := filepath.Join(handoff.Validators[0].ClientStateDir, "operators", "no-1", "client.key")
	clientSeed, err := os.ReadFile(clientPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clientPath, []byte(hex.EncodeToString(clientSeed)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan); err == nil {
		t.Fatal("authenticated reader widened the raw-only client seed grammar")
	}
	if err := os.WriteFile(clientPath, clientSeed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("0x"+string(bytes.Repeat([]byte{'a'}, 64))), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan); err == nil {
		t.Fatal("changed native custody accepted")
	}
}

// Legacy activation committed its receipt before the original locator. Only
// that exact plan may finish the write, without adding or changing a receipt.
func TestPolicyRolloverSuccessorPreservesOriginalActivationCrashRecovery(t *testing.T) {
	g, plan, handoff, journal := newPolicyRolloverHandoffTestV2(t)
	f := g.fixture
	activatePolicyRolloverHandoffTestV2(t, g, plan, handoff, journal)
	path := filepath.Join(policyRolloverRoot(f.stateDir), "handoff.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan); err == nil {
		t.Fatal("missing committed first locator was accepted by a reader")
	}
	before := len(journal.Entries())
	limit, _ := runtimeEvidenceProvisionLimit(f.cfg)
	if err := activatePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan, plan, handoff, journal, limit); err != nil {
		t.Fatal("exact legacy crash recovery failed", err)
	}
	restored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(restored, original) || len(journal.Entries()) != before {
		t.Fatal("legacy recovery changed immutable custody", err)
	}
}
