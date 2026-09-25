//go:build linux || darwin

// Real activation signatures and durable handoff receipts cross the detached
// verifier boundary. The parent plan budget is tested by continuation tests;
// this fixture supplies its authenticated plan objects without any RPC or key
// access during replay.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	validatorpkg "github.com/urfoundation/sn/validator"
	"gopkg.in/yaml.v3"
)

type finalGenerationTestV2 struct {
	roles                                                  *RoleSecrets
	cfg                                                    *ResolvedConfig
	original, source, current                              *SetupPlan
	authority                                              finalValidatorAuthorityV2
	evidence                                               *FinalSemanticEvidence
	runtime, manifest, public, identities, policy, release []byte
	replay                                                 finalValidatorGenerationReplayV2
}

func newFinalGenerationTestV2(t *testing.T) *finalGenerationTestV2 {
	t.Helper()
	test := &finalGenerationTestV2{}
	g, p, h, j := newPolicyRolloverHandoffTestV2(t, func(g *policyRolloverGenerationTestV2) {
		f := g.fixture
		test.original = f.plan
		originalConfig, err := finalGenerationConfigV2(g.original[filepath.Join(f.stateDir, "runtime", "validator-1", "validator.yml")])
		if err != nil {
			t.Fatal(err)
		}
		previous := originalConfig.Policy
		f.cfg.Policy = rateAmendmentTestPolicy(t, &previous)
		f.cfg.PolicyHash, err = f.cfg.Policy.HashHex()
		if err != nil {
			t.Fatal(err)
		}
		f.cfg.previousPolicy = &previous
		source := *f.plan
		source.PolicyHash = f.cfg.PolicyHash
		source.PolicyRateAmendment = &PolicyRateAmendment{Schema: policyRateAmendmentSchema, PriorPlanHash: f.plan.PlanHash, Previous: previous, Next: *f.cfg.Policy}
		source.PriorPlanHashes = append(append([]string(nil), f.plan.PriorPlanHashes...), f.plan.PlanHash)
		source.PlanHash, err = source.hash()
		if err != nil {
			t.Fatal(err)
		}
		f.plan = &source
		for index := range g.members {
			member := &g.members[index]
			hotkey, key, err := runtimeEvidenceActivationKeysV2(g.roles, member.ValidatorId, member.NoId)
			if err != nil {
				t.Fatal(err)
			}
			member.Activation.Domain.PolicyHash = [32]byte(common.HexToHash(f.cfg.PolicyHash))
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
	})
	f := g.fixture
	activatePolicyRolloverHandoffTestV2(t, g, p, h, j)
	wire, err := os.ReadFile(filepath.Join(policyRolloverRoot(f.stateDir), "handoff.json"))
	if err != nil {
		t.Fatal(err)
	}
	h.sourceSHA256 = bytesSHA256(wire)
	test.roles = f.roles
	input, err := captureFinalValidatorGenerationInputsV2(t.Context(), f.cfg, f.stateDir, h)
	if err != nil {
		t.Fatal(err)
	}
	current := *f.plan
	current.PriorPlanHashes = append(append([]string(nil), f.plan.PriorPlanHashes...), f.plan.PlanHash)
	generation := &evidenceRelayActiveGeneration{Generation: h.Generation, RolloverPlanHash: h.PlanHash, SourcePlanHash: h.SourcePlanHash, HandoffSha256: bytesSHA256(input.Handoff), CutoffEpoch: h.CutoffEpoch, FirstFullEpoch: h.FirstFullEpoch}
	current.EvidenceRelayContinuation = &EvidenceRelayContinuation{Schema: evidenceRelayContinuationGenerationSchema, ActiveGeneration: generation}
	for _, value := range f.cfg.Config.ValidatorEvidenceV2 {
		approved, err := doubledEvidenceRelaySourceBounds(value.Evidence.Bounds)
		if err != nil {
			t.Fatal(err)
		}
		current.EvidenceRelayContinuation.SourceBounds = append(current.EvidenceRelayContinuation.SourceBounds, evidenceRelaySourceBounds{ValidatorId: value.ValidatorID, Original: value.Evidence.Bounds, Approved: approved})
	}
	for index, member := range h.Members {
		generation.Sources = append(generation.Sources, EvidenceRelayContinuationSource{ValidatorID: member.ValidatorId, NoID: member.NoId, CoordinatorStateDir: h.Validators[index/2].StateDir, Activation: member.Activation})
	}
	for _, owner := range h.Validators {
		overlay, err := captureEvidenceRelayGenerationRuntime(t.Context(), f.cfg, &current, owner)
		if err != nil {
			t.Fatal(err)
		}
		generation.Runtime = append(generation.Runtime, *overlay)
	}
	current.PlanHash, err = current.hash()
	if err != nil {
		t.Fatal(err)
	}
	test.cfg, test.source, test.current = f.cfg, f.plan, &current
	test.authority = finalValidatorAuthorityV2{Schema: finalValidatorAuthorityV2Schema, StateRoot: f.stateDir, Config: f.cfg.Config, Public: f.cfg.Public, Hyperparameters: f.cfg.Hyperparameters, Resolved: resolvedPlanInputs(f.cfg), Prepared: bytes.Clone(f.preparedBytes), ActiveGeneration: input}
	test.authority.Completed, err = json.Marshal(f.completed)
	if err != nil {
		t.Fatal(err)
	}
	test.runtime = []byte(generation.Runtime[0].Content)
	test.identities, err = json.Marshal(f.roles.Public())
	if err != nil {
		t.Fatal(err)
	}
	test.policy, err = f.cfg.Policy.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	test.release, err = yaml.Marshal(f.cfg.Release)
	if err != nil {
		t.Fatal(err)
	}
	deployment := f.plan.Deployment
	deployment.DeployBlock, deployment.CoordinatorEventStartBlock = 100, 100
	deployment.DeployBlockHash = common.Hash{8}.Hex()
	deployment.CoordinatorEventStartBlockHash = deployment.DeployBlockHash
	public := PublicDeploymentManifest{DeploymentID: f.plan.DeploymentID, PlanHash: current.PlanHash, ConfigHash: f.cfg.ConfigHash, PolicyHash: f.cfg.PolicyHash, ChainID: f.cfg.ChainID, Netuid: f.cfg.Netuid, Contracts: &deployment}
	for i, origin := range f.cfg.OperatorAPIOrigins {
		public.Operators = append(public.Operators, PublicOperator{NoID: i + 1, APIURL: origin})
	}
	test.public, err = json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	manifest := RuntimeConfigManifest{Schema: runtimeConfigManifestSchema, DeploymentID: f.plan.DeploymentID, ConfigHash: f.cfg.ConfigHash, PolicyHash: f.cfg.PolicyHash}
	for index, owner := range h.Validators {
		relative, err := filepath.Rel(f.stateDir, owner.Config.Path)
		if err != nil {
			t.Fatal(err)
		}
		manifest.Files = append(manifest.Files, RuntimeConfigFile{Path: filepath.ToSlash(relative), Mode: "0600", SHA256: bytesSHA256(input.Configs[index])}, RuntimeConfigFile{Path: fmt.Sprintf("runtime/validator-%d/validator.yml", index+1), Mode: "0600", SHA256: bytesSHA256(input.FrozenConfigs[index])})
	}
	manifest.ManifestHash, err = runtimeConfigManifestHash(manifest)
	if err != nil {
		t.Fatal(err)
	}
	test.manifest, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	test.evidence = &FinalSemanticEvidence{DeploymentID: current.DeploymentID, PlanHash: current.PlanHash, ConfigHash: f.cfg.ConfigHash, PolicyHash: f.cfg.PolicyHash, ChainID: f.cfg.ChainID, GenesisHash: f.cfg.Public.Chain.GenesisHash, Netuid: f.cfg.Netuid, ExpectedValidators: 2, ExpectedOperators: 2}
	test.evidence.Deployment.CoordinatorProxy = deployment.CoordinatorProxy.Hex()
	test.evidence.Deployment.SettlementVault = deployment.SettlementVault.Hex()
	test.evidence.Deployment.ReserveSink = deployment.ReserveSink.Hex()
	test.replay = finalValidatorGenerationReplayV2{source: test.source, entries: j.Entries()}
	// Removal is an explicit barrier: replay cannot silently load live configs,
	// private keys, journal or source-role files from the original namespace.
	retired := f.stateDir + "-detached"
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.stateDir, retired); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Rename(retired, f.stateDir); err != nil {
			t.Error(err)
		}
	})
	return test
}

func (self *finalGenerationTestV2) verify(t *testing.T) (*validatorpkg.ReleaseConfig, error) {
	t.Helper()
	raw, err := json.Marshal(self.authority)
	if err != nil {
		t.Fatal(err)
	}
	config, _, err := finalValidatorConfigAuthorityV2(t.Context(), self.evidence, self.current, self.original, raw, self.runtime, self.manifest, self.public, self.identities, self.policy, self.release, 1, self.replay)
	return config, err
}

func TestFinalActiveGenerationReplaysBothSignedOwnersWithoutLiveState(t *testing.T) {
	f := newFinalGenerationTestV2(t)
	config, err := f.verify(t)
	if err != nil {
		t.Fatal(err)
	}
	if config.StateDir == filepath.Join(f.authority.StateRoot, "runtime", "validator-1", "state") || config.PolicyHash != f.cfg.PolicyHash || !bytes.Contains(f.runtime, []byte("generation-")) {
		t.Fatal("active verifier selected original runtime")
	}
}

func TestFinalActiveGenerationRejectsChangedOriginalAndCurrentAuthority(t *testing.T) {
	f := newFinalGenerationTestV2(t)
	if _, err := f.verify(t); err != nil {
		t.Fatal(err)
	}
	authority, _ := json.Marshal(f.authority)
	original, _ := json.Marshal(f.original)
	source, _ := json.Marshal(f.source)
	runtime := bytes.Clone(f.runtime)
	for _, name := range []string{"original-fixed", "original-config", "original-policy-and-config", "active-config", "active-publication", "active-journal", "active-source", "strict-runtime", "missing-generation"} {
		if err := json.Unmarshal(authority, &f.authority); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(original, f.original); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(source, f.source); err != nil {
			t.Fatal(err)
		}
		f.runtime = bytes.Clone(runtime)
		entries := f.replay.entries
		switch name {
		case "original-fixed":
			f.authority.ActiveGeneration.FrozenInputs[0][0] ^= 1
		case "original-config":
			f.authority.ActiveGeneration.FrozenConfigs[0] = append(f.authority.ActiveGeneration.FrozenConfigs[0], '\n')
		case "original-policy-and-config":
			f.original.ConfigHash = common.Hash{0xc1}.Hex()
			f.original.PolicyHash = common.Hash{0xc2}.Hex()
		case "active-config":
			f.authority.ActiveGeneration.Configs[0] = append(f.authority.ActiveGeneration.Configs[0], '\n')
		case "active-publication":
			f.authority.ActiveGeneration.Publications[0] = append(f.authority.ActiveGeneration.Publications[0], '\n')
		case "active-journal":
			f.replay.entries = entries[:1]
		case "active-source":
			f.source.OwnedRPCAuthority = "unapproved.example:443"
		case "strict-runtime":
			f.runtime = bytes.Replace(f.runtime, []byte(f.cfg.OperatorAPIOrigins[0]), []byte("https://other.example"), 1)
		case "missing-generation":
			f.authority.ActiveGeneration = nil
		}
		if _, err := f.verify(t); err == nil {
			t.Fatalf("accepted changed %s", name)
		}
		f.replay.entries = entries
	}
}

// The original public file remains frozen. Both readback and the completed
// semantic owner route a new VPK using separately signed generation inputs.
func TestFinalActiveGenerationRoutesCapturedPathsWithoutReplacingOriginalIdentities(t *testing.T) {
	f := newFinalGenerationTestV2(t)
	if _, err := f.verify(t); err != nil {
		t.Fatal(err)
	}
	original, err := decodeFinalOperatorPathAuthority(f.identities, f.current.DeploymentID, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(f.authority)
	if err != nil {
		t.Fatal(err)
	}
	locator := FinalArtifactLocator{Kind: "validator-evidence-v2-source", URI: "final-inputs/synthetic-authority.bin", SizeBytes: uint64(len(raw)), ContentHash: bytesSHA256(raw)}
	collected := FinalCollectedValidatorInputs{ValidatorID: 1, EvidenceV2: &FinalCollectedValidatorEvidenceV2{Sources: []FinalCollectedValidatorSourceV2{{Source: validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: "simulator-authority"}, Artifact: locator}}}}
	active, err := finalCapturedGenerationPathAuthorityV2(t.Context(), collected, original, func(context.Context, FinalArtifactLocator) ([]byte, error) { return raw, nil })
	if err != nil {
		t.Fatal(err)
	}
	paths := active.pathsByValidator[1]
	if err := active.verify(1, paths[0].PathVPK, paths); err != nil {
		t.Fatal(err)
	}
	if err := original.verify(1, paths[0].PathVPK, paths); err == nil || !bytes.Equal(original.publicBytes, f.identities) {
		t.Fatal("active routing replaced original identity artifact")
	}
	// Changing both the public pin and signed payload cannot create a consent.
	var h policyRolloverHandoffV2
	if err := json.Unmarshal(f.authority.ActiveGeneration.Handoff, &h); err != nil {
		t.Fatal(err)
	}
	var identities finalPublicIdentities
	if err := json.Unmarshal(f.authority.ActiveGeneration.Identities, &identities); err != nil {
		t.Fatal(err)
	}
	h.Members[0].Activation.VPK[0] ^= 1
	client := identities.Clients["validator-1-no-1"]
	client.ClientKey = fmt.Sprintf("0x%x", h.Members[0].Activation.VPK)
	identities.Clients["validator-1-no-1"] = client
	f.authority.ActiveGeneration.Identities, _ = json.Marshal(identities)
	h.Identities = policyRolloverFile(h.Identities.Path, f.authority.ActiveGeneration.Identities)
	f.authority.ActiveGeneration.Handoff, _ = json.Marshal(h)
	if _, err := finalGenerationPathAuthorityV2(original, &f.authority); err == nil {
		t.Fatal("joint identity/pin rewrite created fresh dual consent")
	}
}

// Each validator captures its own predecessor payload. This tests the other
// validator's exact owner-signed role projection; genuine payload decoding is
// independently exercised by TestReleaseSourceRoleArchiveV2 in validator.
func TestFinalActiveGenerationSourceRoleRetainsOtherValidatorApproval(t *testing.T) {
	f := newFinalGenerationTestV2(t)
	input := f.authority.ActiveGeneration
	var base policyRolloverHandoffV2
	if err := json.Unmarshal(input.Handoff, &base); err != nil {
		t.Fatal(err)
	}
	base.sourceSHA256 = bytesSHA256(input.Handoff)
	plan := policyRolloverSourceRolePlanV2{Schema: policyRolloverSourceRoleSchemaV2, SourcePlanHash: f.source.PlanHash, RolloverPlanHash: base.PlanHash, OriginalHandoffSHA256: base.sourceSHA256, DeploymentID: f.current.DeploymentID, StateDir: f.authority.StateRoot, ConfigHash: f.cfg.ConfigHash, PolicyHash: f.cfg.PolicyHash, Generation: base.Generation, RoleOnly: true, Provisional: true}
	for index, owner := range base.Validators {
		member := policyRolloverSourceRoleValidatorV2{ValidatorID: owner.ValidatorID, PreviousStateDir: owner.PreviousStateDir, OriginalConfig: owner.Config, Config: owner.Config}
		if index == 1 {
			root := filepath.Join(policyRolloverSourceRoleRootV2(f.authority.StateRoot, base.Generation), "validator-2")
			proof := policyRolloverFile(filepath.Join(root, "source-role-predecessor.json"), []byte("separate validator payload"))
			intents := policyRolloverFile(filepath.Join(owner.PreviousStateDir, "steering-intents.json"), []byte("separate validator intents"))
			member.Predecessor, member.SourceIntents = &proof, &intents
			config, err := finalGenerationConfigV2(input.Configs[index])
			if err != nil {
				t.Fatal(err)
			}
			config.SourceRolePredecessorV2 = &proof
			input.SelectedConfigs[index], err = yaml.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			member.Config = policyRolloverFile(filepath.Join(root, "validator.yml"), input.SelectedConfigs[index])
		}
		plan.Validators = append(plan.Validators, member)
	}
	var err error
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signEvidence(f.cfg, policyRolloverSourceRoleKindV2, plan.PlanHash, &plan, f.roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	input.SourceRole, err = json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	selected := base
	selected.Validators = append([]policyRolloverValidatorHandoffV2(nil), base.Validators...)
	if err := verifyFinalGenerationSourceRoleV2(t.Context(), f.cfg, f.current, f.authority.StateRoot, &selected, input, 1, nil); err != nil {
		t.Fatal(err)
	}
	if selected.SourceRoleOverlay == nil || selected.Validators[1].Config != plan.Validators[1].Config || selected.Validators[0].Config != base.Validators[0].Config {
		t.Fatal("role projection changed another validator")
	}
	// The owner of the actual descriptor must supply and authenticate it; a
	// second validator's successful structural routing never waives that proof.
	if err := verifyFinalGenerationSourceRoleV2(t.Context(), f.cfg, f.current, f.authority.StateRoot, &base, input, 2, nil); err == nil || !strings.Contains(err.Error(), "archive inputs are absent") {
		t.Fatal("source owner accepted missing payload", err)
	}
	// Being another allowed ancestor is insufficient: this branch has proved
	// only the exact rollout owner. A freshly signed overlay cannot borrow it.
	plan.SourcePlanHash = f.original.PlanHash
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := signEvidence(f.cfg, policyRolloverSourceRoleKindV2, plan.PlanHash, &plan, f.roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	input.SourceRole, err = json.Marshal(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalGenerationSourceRoleV2(t.Context(), f.cfg, f.current, f.authority.StateRoot, &base, input, 1, nil); err == nil {
		t.Fatal("re-signed other ancestor borrowed rollout authority")
	}
	signed.Signature = "0x" + strings.Repeat("00", 65)
	input.SourceRole, _ = json.Marshal(signed)
	if err := verifyFinalGenerationSourceRoleV2(t.Context(), f.cfg, f.current, f.authority.StateRoot, &base, input, 1, nil); err == nil {
		t.Fatal("role projection accepted bad owner signature")
	}
}

// The strict request is separately bound to runtime bytes/current approval;
// the replay source join must select the activated ancestor, retaining the
// initial activation ancestor as an independent historical proof branch.
func TestFinalActiveGenerationAdoptionBindsItsExactArchivedSource(t *testing.T) {
	f := newFinalGenerationTestV2(t)
	config, err := f.verify(t)
	if err != nil {
		t.Fatal(err)
	}
	request := &validatorpkg.ReleaseHistoryAdoptionV2{DeploymentID: f.current.DeploymentID, ValidatorID: 1, ApprovedPlanHash: f.current.PlanHash, SourcePlanHash: f.source.PlanHash, ConfigSHA256: bytesSHA256(f.runtime), CoordinatorStateDir: config.StateDir}
	verify := func() error {
		if err := verifyFinalHistoryAdoptionV2(f.current, f.authority.StateRoot, config, f.runtime, request); err != nil {
			return err
		}
		return finalValidatorAdoptionSourceV2(f.original, []finalValidatorGenerationReplayV2{f.replay}, request)
	}
	if err := verify(); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{f.original.PlanHash, f.current.PlanHash, common.Hash{0x91}.Hex()} {
		request.SourcePlanHash = hash
		if err := verify(); err == nil {
			t.Fatal("another ancestor authorized active strict history")
		}
	}
	if err := finalValidatorAdoptionSourceV2(f.original, []finalValidatorGenerationReplayV2{f.replay}, nil); err == nil {
		t.Fatal("active source accepted no strict adoption")
	}
}
