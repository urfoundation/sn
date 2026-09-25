//go:build linux || darwin

// Signed synthetic subjects exercise pure appendix and reserve admission.
// Runtime-file and stopped signed-ledger authentication have independent gates.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Keep the genuine funded predecessor and cumulative journal. New activation
// headers use generated fixture keys; file placeholders test shape, not custody.
func newEvidenceRelayGenerationTest(t *testing.T) (*SetupPlan, EvidenceRelayContinuation) {
	t.Helper()
	f, executor := newEvidenceRelayExpansionTest(t)
	prior, err := appendEvidenceRelayContinuationPlan(executor.plan, evidenceRelaySourceExpansionRequestTest(t, f, executor))
	if err != nil {
		t.Fatal(err)
	}
	executor.plan = prior
	current := evidenceRelayRefreshRequestTest(t, executor)
	current.Schema = evidenceRelayContinuationGenerationSchema
	current.NewSlots, current.HistoricalLiabilityWei, err = current.remainingSlots()
	if err != nil {
		t.Fatal(err)
	}
	generation := &evidenceRelayActiveGeneration{Generation: 1, RolloverPlanHash: common.Hash{0xca}.Hex(), SourcePlanHash: prior.PlanHash, HandoffSha256: bytesSHA256([]byte("synthetic-handoff")), CutoffEpoch: current.EndSettlementEpoch - 1, FirstFullEpoch: current.EndSettlementEpoch, Frozen: append([]EvidenceRelayContinuationSource(nil), current.Sources...)}
	current.ActiveGeneration = generation
	for index, original := range current.Sources {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(0xb1 + original.ValidatorID)}, ed25519.SeedSize))
		source := original
		source.Activation.VPK = [32]byte(key[ed25519.SeedSize:])
		source.Activation.Domain.Epoch = generation.CutoffEpoch
		root := filepath.Join(filepath.Dir(original.CoordinatorStateDir), "evidence-generations", "synthetic-generation")
		source.CoordinatorStateDir = filepath.Join(root, "coordinator-state-v2")
		source.Capacity.Identity.ValidatorVPK = fmt.Sprintf("0x%x", source.Activation.VPK)
		generation.Sources = append(generation.Sources, source)
		if index%2 == 0 {
			content := fmt.Sprintf("synthetic runtime %d\n", original.ValidatorID)
			generation.Runtime = append(generation.Runtime, evidenceRelayGenerationRuntime{ValidatorId: original.ValidatorID, Config: policyRolloverFile(filepath.Join(root, "validator-strict.yml"), []byte(content)), Content: content})
		}
		if index != 0 {
			continue
		}
		request := evidenceRelayLaunchRequestTest(t, f, f.prepared.Members[index], generation.CutoffEpoch, false, 0)
		request.Activation = source.Activation
		request.Evidence.Header.VPK = source.Activation.VPK
		request.Evidence.Header.Domain, err = source.Activation.EvidenceDomain()
		if err != nil {
			t.Fatal(err)
		}
		request.Evidence.VPKSignature, err = request.Evidence.Header.SignVPK(key)
		if err != nil {
			t.Fatal(err)
		}
		hotkey, _, err := runtimeEvidenceActivationKeysV2(f.roles, source.ValidatorID, source.NoID)
		if err != nil {
			t.Fatal(err)
		}
		digest, err := request.Evidence.Header.Digest()
		if err != nil {
			t.Fatal(err)
		}
		request.Evidence.HotkeySignature, err = hotkey.Sign(digest[:])
		if err != nil {
			t.Fatal(err)
		}
		current.Retained = append(current.Retained, request)
	}
	current.Retained, err = canonicalEvidenceRelayContinuationRequests(current.Retained)
	if err != nil {
		t.Fatal(err)
	}
	return prior, current
}

// Both generations debit the same finite approval; original source entries,
// higher-fee liabilities and the one approved source doubling remain unchanged.
func TestEvidenceRelayGenerationAppendPreservesFrozenFunding(t *testing.T) {
	prior, current := newEvidenceRelayGenerationTest(t)
	before, _ := json.Marshal(prior)
	plan, err := appendEvidenceRelayContinuationPlan(prior, current)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateEvidenceRelayContinuationPlan(plan); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.EvidenceRelayContinuation.Sources, prior.EvidenceRelayContinuation.Sources) || !reflect.DeepEqual(plan.EvidenceRelayContinuation.SourceBounds, prior.EvidenceRelayContinuation.SourceBounds) || plan.MaximumSpend != prior.MaximumSpend || plan.Limits != prior.Limits || plan.EvidenceRelayContinuation.NewSlots != prior.EvidenceRelayContinuation.NewSlots {
		t.Fatal("generation changed original sources, limits or funding")
	}
	after, _ := json.Marshal(prior)
	if !bytes.Equal(before, after) {
		t.Fatal("append rewrote its immutable predecessor")
	}
	for _, source := range current.ActiveGeneration.Sources {
		if err := validateEvidenceRelayContinuationNamespace(plan, source.ValidatorID, source.CoordinatorStateDir); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateEvidenceRelayContinuationNamespace(plan, 1, filepath.Join(t.TempDir(), "coordinator-state-v2")); err == nil {
		t.Fatal("accepted an unrelated namespace")
	}
}

// Joint changes to metadata cannot replace frozen custody, increase funding,
// remove a debit, move the cutoff or introduce an unapproved third generation.
func TestEvidenceRelayGenerationRejectsAuthorityAndPrefixChanges(t *testing.T) {
	prior, original := newEvidenceRelayGenerationTest(t)
	raw, _ := json.Marshal(original)
	for _, test := range []struct {
		name   string
		change func(*EvidenceRelayContinuation)
	}{
		{name: "original root", change: func(value *EvidenceRelayContinuation) {
			value.Sources[0].IntentPrefixSHA256 = bytesSHA256([]byte("other"))
		}},
		{name: "frozen root", change: func(value *EvidenceRelayContinuation) {
			value.ActiveGeneration.Frozen[0].IntentPrefixSHA256 = bytesSHA256([]byte("other"))
		}},
		{name: "missing debit", change: func(value *EvidenceRelayContinuation) {
			value.Debits = nil
			value.NewSlots, value.HistoricalLiabilityWei, _ = value.remainingSlots()
		}},
		{name: "third generation", change: func(value *EvidenceRelayContinuation) {
			value.ActiveGeneration.Sources = append(value.ActiveGeneration.Sources, value.ActiveGeneration.Sources...)
		}},
		{name: "cutoff", change: func(value *EvidenceRelayContinuation) {
			value.ActiveGeneration.CutoffEpoch++
			value.ActiveGeneration.FirstFullEpoch++
		}},
		{name: "active key", change: func(value *EvidenceRelayContinuation) { value.ActiveGeneration.Sources[0].Activation.VPK = [32]byte{1} }},
		{name: "incomplete census", change: func(value *EvidenceRelayContinuation) {
			value.ActiveGeneration.Sources = value.ActiveGeneration.Sources[:3]
		}},
		{name: "source multiplier", change: func(value *EvidenceRelayContinuation) { value.SourceBounds[0].Approved.Disk.MaxRecordCount++ }},
	} {
		var changed EvidenceRelayContinuation
		if err := json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		test.change(&changed)
		plan, err := appendEvidenceRelayContinuationPlan(prior, changed)
		if err == nil {
			err = validateEvidenceRelayContinuationPlan(plan)
		}
		if err == nil {
			t.Fatalf("accepted %s", test.name)
		}
	}
}

// The future native bucket belongs to the active VPK. A late audit from the
// frozen VPK must consume an additional slot even at the same native epoch.
func TestEvidenceRelayGenerationCountsFrozenAuditSeparately(t *testing.T) {
	_, current := newEvidenceRelayGenerationTest(t)
	base, err := current.requiredSubjects(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	old := current.Sources[0].Activation
	active := current.ActiveGeneration.Sources[0].Activation
	header := protocol.ValidatorEvidenceHeader{Hotkey: old.Hotkey, NoID: old.NoID, VPK: old.VPK, Kind: protocol.ValidatorEvidenceDepositAudit, Subject: protocol.ValidatorEvidenceSubject{NativeEpoch: current.NativeEpoch}}
	headers := map[[32]byte]protocol.ValidatorEvidenceHeader{{1}: header}
	oldRequired, err := current.requiredSubjects(headers, nil)
	if err != nil || oldRequired != base+1 {
		t.Fatal("frozen audit borrowed the active native reserve", oldRequired, base, err)
	}
	header.VPK = active.VPK
	headers[[32]byte{2}] = header
	required, err := current.requiredSubjects(headers, nil)
	if err != nil || required != base+1 {
		t.Fatal("first active audit was charged twice", required, base, err)
	}
	header.Kind, header.Epoch = protocol.ValidatorEvidenceClosedCensus, current.ActiveGeneration.CutoffEpoch
	if current.acceptsActivation(old, header) || !current.acceptsActivation(active, header) {
		t.Fatal("cutoff selected the wrong generation")
	}
}

// Independent final replay must carry the active writer's history request;
// using the frozen source namespace cannot stand in for that permission.
func TestEvidenceRelayGenerationFinalHistoryKeepsActiveAuthority(t *testing.T) {
	prior, current := newEvidenceRelayGenerationTest(t)
	plan, err := appendEvidenceRelayContinuationPlan(prior, current)
	if err != nil {
		t.Fatal(err)
	}
	generation := current.ActiveGeneration
	content := []byte(generation.Runtime[0].Content)
	release := &validatorcomponent.ReleaseConfig{DeploymentID: plan.DeploymentID, ValidatorID: 1, StateDir: generation.Sources[0].CoordinatorStateDir}
	request := &validatorcomponent.ReleaseHistoryAdoptionV2{DeploymentID: plan.DeploymentID, ValidatorID: 1, ApprovedPlanHash: plan.PlanHash, SourcePlanHash: generation.SourcePlanHash, CoordinatorStateDir: release.StateDir, ConfigSHA256: bytesSHA256(content)}
	if err := verifyFinalHistoryAdoptionV2(plan, t.TempDir(), release, content, request); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*validatorcomponent.ReleaseHistoryAdoptionV2)
	}{
		{name: "frozen namespace", change: func(value *validatorcomponent.ReleaseHistoryAdoptionV2) {
			value.CoordinatorStateDir = current.Sources[0].CoordinatorStateDir
		}},
		{name: "original activation", change: func(value *validatorcomponent.ReleaseHistoryAdoptionV2) {
			value.SourcePlanHash = current.ActivationPlanHash
		}},
		{name: "other runtime", change: func(value *validatorcomponent.ReleaseHistoryAdoptionV2) {
			value.ConfigSHA256 = bytesSHA256([]byte("other"))
		}},
		{name: "other validator", change: func(value *validatorcomponent.ReleaseHistoryAdoptionV2) { value.ValidatorID = 2 }},
	} {
		changed := *request
		test.change(&changed)
		if err := verifyFinalHistoryAdoptionV2(plan, t.TempDir(), release, content, &changed); err == nil {
			t.Fatalf("final replay accepted %s", test.name)
		}
	}
}
