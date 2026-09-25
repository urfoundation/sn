//go:build linux || darwin

// Real signed fleet approvals and activated generation receipts reproduce the
// independent round-six to round-seven renewal without any chain submission.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Advance the actual signed binding generation before preparing the next
// approval. Every new window follows its predecessor and carries all actions.
func nextPolicyRolloverRenewalTestV2(t *testing.T, fleet *fleetRenewalTestFixture, source *SetupPlan, stateDir string) *SetupPlan {
	t.Helper()
	observation := fleet.observed
	round := uint64(len(source.FleetRenewals) + 1)
	observation.Renewal.Round = round
	observation.Renewal.SourcePlanHash = source.PlanHash
	observation.Renewal.ValidFromEpoch = 318 + 32*(round-1)
	observation.Renewal.ValidToEpoch = observation.Renewal.ValidFromEpoch + 31
	observation.Renewal.ObservedEpoch = observation.Renewal.ValidFromEpoch - 2
	observation.Renewal.CampaignReserveBeforeWei = actionByID(t, source, "campaign.evm-gas-reserve").Spend.EVMGasWei
	observation.Renewal.NativeHead.Number += round
	observation.Renewal.EVMHead.Number += round
	if round > 1 {
		previous := source.FleetRenewals[len(source.FleetRenewals)-1]
		observation.Renewal.OracleNonce = previous.OracleNonce + uint64(len(previous.Fleets))
		observation.Renewal.KeeperNonce = previous.KeeperNonce
		for _, retained := range previous.Fleets {
			manifest, err := protocol.ParseFleetManifest(retained.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			account := observation.Accounts[manifest.Hotkey]
			account.Nonce++
			observation.Accounts[manifest.Hotkey] = account
			for index, member := range retained.Members {
				binding, err := fleetRenewalBinding(*manifest, manifest.Members[index], member.Binding)
				if err != nil {
					t.Fatal(err)
				}
				observation.Evidence[binding.ClientID] = []FleetBindingEvidence{member.Binding}
				observation.Records[binding.ClientID] = fleetBindingVersionRead{Count: new(big.Int).SetUint64(member.VersionCount + 1), Record: stabi.STCoordinatorBindingRecord{
					FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientKey: binding.ClientKey, CommitmentHash: binding.CommitmentHash,
					Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, Uid: retained.UID,
				}}
				observation.Renewal.KeeperNonce++
				if member.RevokeSignature != "" {
					observation.Renewal.KeeperNonce++
				}
			}
		}
	}
	renewal, err := prepareFleetRenewal(fleet.cfg, fleet.stateDir, source, fleet.roles, observation)
	if err != nil {
		t.Fatal(err)
	}
	successor, err := appendFleetRenewalPlan(source, renewal)
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range []*SetupPlan{source, successor} {
		if _, err := archiveReviewedSetupPlan(stateDir, plan); err != nil {
			t.Fatal(err)
		}
	}
	return successor
}

// The fixture keeps the original activation bytes while six ordinary signed
// renewal approvals accumulate before this independent generation is staged.
func policyRolloverRoundSixTestV2(t *testing.T, fleet **fleetRenewalTestFixture) func(*policyRolloverGenerationTestV2) {
	t.Helper()
	return func(g *policyRolloverGenerationTestV2) {
		f := g.fixture
		owned := newFleetRenewalConfiguredTestFixture(t, f.cfg, f.plan, cloneRoleSecrets(f.roles))
		*fleet = &owned
		for round := 1; round <= 6; round++ {
			f.plan = nextPolicyRolloverRenewalTestV2(t, &owned, f.plan, f.stateDir)
		}
		raw, err := json.Marshal(f.plan)
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(f.stateDir, "plan.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// The exact read, process selection and manifest inventory must continue, but
// the old generation's plan may not authorize a fresh activation under round 7.
func TestPolicyRolloverRetainedRenewalRoundSevenPreservesGeneration(t *testing.T) {
	var fleet *fleetRenewalTestFixture
	g, p, h, journal := newPolicyRolloverHandoffTestV2(t, policyRolloverRoundSixTestV2(t, &fleet))
	f := g.fixture
	activatePolicyRolloverHandoffTestV2(t, g, p, h, journal)
	original, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(policyRolloverRoot(f.stateDir), "handoff.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := map[string]os.FileMode{}
	if err := addPolicyRolloverGenerationRuntimeInputsV2(t.Context(), f.cfg, f.stateDir, f.plan, wantPaths, policyRolloverSourceRoleIO()); err != nil {
		t.Fatal(err)
	}
	renewed := nextPolicyRolloverRenewalTestV2(t, fleet, f.plan, f.stateDir)
	if len(f.plan.FleetRenewals) != 6 || len(renewed.FleetRenewals) != 7 || p.SourcePlanHash != f.plan.PlanHash || renewed.PlanHash == f.plan.PlanHash {
		t.Fatal("fixture did not cross the sixth to seventh approval")
	}
	for range 2 {
		selected, err := readPolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, renewed)
		if err != nil || !reflect.DeepEqual(selected, original) {
			t.Fatalf("round-seven retained generation changed or became unreadable: %v", err)
		}
	}
	specs := buildClientSpecs(f.cfg, f.stateDir, map[string]string{"sim-testnet": "synthetic-binary"}, f.roles)
	if err := attachRetainedProvisionalProcessHandoff(t.Context(), f.cfg, f.stateDir, renewed, f.roles, specs); err != nil {
		t.Fatal("retained process selection lost original generation", err)
	}
	for _, owner := range original.Validators {
		found := false
		for _, spec := range specs {
			if spec.Role == "validator" && slices.Contains(spec.Args, "--config="+owner.Config.Path) && spec.Env["URNETWORK_STATE_DIR"] == owner.ClientStateDir {
				found = true
			}
		}
		if !found {
			t.Fatal("renewal lost a retained validator namespace")
		}
	}
	gotPaths := map[string]os.FileMode{}
	if err := addPolicyRolloverGenerationRuntimeInputsV2(t.Context(), f.cfg, f.stateDir, renewed, gotPaths, policyRolloverSourceRoleIO()); err != nil || !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatal("renewal moved immutable generation inputs", err)
	}
	if _, err := readPolicyRolloverPlanV2(t.Context(), f.cfg, renewed, f.stateDir, policyRolloverPlanPathV2(f.stateDir, p.Generation, p.Epoch)); err == nil {
		t.Fatal("retained read authority authorized a fresh old-plan rollover")
	}
	after, err := os.ReadFile(filepath.Join(policyRolloverRoot(f.stateDir), "handoff.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("retained selection rewrote the original handoff", err)
	}
}

// Signed role-only overlays have their own original owner; selecting one on a
// successor must neither re-sign it nor bypass current mutation approval.
func TestPolicyRolloverRetainedRenewalSourceRoleKeepsOriginalApproval(t *testing.T) {
	var fleet *fleetRenewalTestFixture
	g, h, io := newPolicyRolloverSourceRoleTestV2(t, policyRolloverRoundSixTestV2(t, &fleet))
	f := g.fixture
	p, err := preparePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, h, io)
	if err != nil {
		t.Fatal(err)
	}
	original, err := activatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, h, p, io, func(context.Context, *validatorcomponent.ReleaseConfig) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(original.SourceRoleOverlay.Path)
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := map[string]os.FileMode{}
	if err := addPolicyRolloverGenerationRuntimeInputsV2(t.Context(), f.cfg, f.stateDir, f.plan, wantPaths, io); err != nil {
		t.Fatal(err)
	}
	renewed := nextPolicyRolloverRenewalTestV2(t, fleet, f.plan, f.stateDir)
	for range 2 {
		retained, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, renewed)
		if err != nil {
			t.Fatal(err)
		}
		selected, err := readPolicyRolloverSourceRoleOverlayWithV2(t.Context(), f.cfg, f.stateDir, renewed, retained, io)
		if err != nil || !reflect.DeepEqual(selected, original) {
			t.Fatalf("round-seven role overlay lost its original signed approval: %v", err)
		}
	}
	gotPaths := map[string]os.FileMode{}
	if err := addPolicyRolloverGenerationRuntimeInputsV2(t.Context(), f.cfg, f.stateDir, renewed, gotPaths, io); err != nil || !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatal("selected overlay moved original runtime manifest inputs", err)
	}
	verified := false
	if _, err := activatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, renewed, f.roles, h, p, io, func(context.Context, *validatorcomponent.ReleaseConfig) error { verified = true; return nil }); err == nil || verified {
		t.Fatal("historical read widened fresh source-role mutation approval", err)
	}
	after, err := os.ReadFile(original.SourceRoleOverlay.Path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("retained role selection replaced its owner signature", err)
	}
	if err := os.WriteFile(p.Validators[0].Predecessor.Path, []byte("changed synthetic descriptor"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPolicyRolloverSourceRoleOverlayWithV2(t.Context(), f.cfg, f.stateDir, renewed, h, io); err == nil {
		t.Fatal("ancestor resolution bypassed descriptor authentication")
	}
}

// An approved ancestor is still an exact immutable custody domain. Missing or
// modified archives, unrelated plans and changed current custody cannot select it.
func TestPolicyRolloverRetainedSourceRejectsUnapprovedOrChangedHistory(t *testing.T) {
	f := newRuntimeEvidenceProvisionV2TestFixture(t)
	if _, err := archiveReviewedSetupPlan(f.stateDir, f.plan); err != nil {
		t.Fatal(err)
	}
	current := *f.plan
	current.PriorPlanHashes = append(slices.Clone(f.plan.PriorPlanHashes), f.plan.PlanHash)
	var err error
	current.PlanHash, err = current.hash()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retainedPolicyRolloverSourceV2(t.Context(), f.cfg, f.stateDir, &current, f.plan.PlanHash); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"lineage", "owner", "journal", "policy", "route", "action", "configuration", "source configuration"} {
		changed, cfg := current, *f.cfg
		switch fault {
		case "lineage":
			changed.PriorPlanHashes = nil
		case "owner":
			changed.Owner = "synthetic-foreign-owner"
		case "journal":
			journal := *changed.ValidatorEvidence
			journal.Address = common.Address{0x91}
			changed.ValidatorEvidence = &journal
		case "policy":
			changed.PolicyHash = common.Hash{0x92}.Hex()
		case "route":
			changed.OwnedRPCAuthority = "192.0.2.14:9944"
		case "action":
			changed.Actions = slices.Clone(changed.Actions)
			for index := range changed.Actions {
				if changed.Actions[index].ID == validatorEvidenceAnchorActionID {
					changed.Actions[index].IntentHash = common.Hash{0x93}.Hex()
				}
			}
		case "configuration":
			cfg.ConfigHash = common.Hash{0x94}.Hex()
		case "source configuration":
			changed.ConfigHash = common.Hash{0x95}.Hex()
			cfg.ConfigHash = changed.ConfigHash
		}
		if _, err := retainedPolicyRolloverSourceV2(t.Context(), &cfg, f.stateDir, &changed, f.plan.PlanHash); err == nil {
			t.Fatal("retained source admitted changed " + fault)
		}
	}
	// Even a separately valid archived approval cannot replace the source
	// configuration with a hash-only projection onto current semantics.
	other, err := rebindFleetRenewalRuntimePlan(f.plan, common.Hash{0x96}.Hex(), f.plan.ResolvedInputsHash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archiveReviewedSetupPlan(f.stateDir, other); err != nil {
		t.Fatal(err)
	}
	current.PriorPlanHashes = append(current.PriorPlanHashes, other.PlanHash)
	current.PlanHash, err = current.hash()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retainedPolicyRolloverSourceV2(t.Context(), f.cfg, f.stateDir, &current, other.PlanHash); err == nil {
		t.Fatal("changed archived configuration was admitted through hash substitution")
	}
	path := filepath.Join(f.stateDir, "plans", strings.TrimPrefix(f.plan.PlanHash, "0x")+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, replace := range []bool{false, true} {
		if replace {
			err = os.WriteFile(path, []byte("{}\n"), 0o600)
		} else {
			err = os.Remove(path)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := retainedPolicyRolloverSourceV2(t.Context(), f.cfg, f.stateDir, &current, f.plan.PlanHash); err == nil {
			t.Fatal("missing or changed source archive was ignored")
		}
		if err := os.WriteFile(path, before, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := retainedPolicyRolloverSourceV2(ctx, f.cfg, f.stateDir, &current, f.plan.PlanHash); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled historical reader continued", err)
	}
}
