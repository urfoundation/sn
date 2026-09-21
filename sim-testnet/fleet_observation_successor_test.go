package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// Build two complete signed generations around a sealed takeover handoff. The
// second round extends every fleet, including the two lifecycle participants.
func fleetObservationSuccessorFixture(t *testing.T) (fleetRenewalTestFixture, *SetupPlan, *FleetLifecycleEvidence) {
	t.Helper()
	f := newFleetRenewalTestFixture(t)
	first, err := appendFleetRenewalPlan(f.base, f.renewal)
	if err != nil {
		t.Fatal(err)
	}
	observation := f.observed
	observation.Renewal = f.renewal
	observation.Renewal.Fleets = nil
	observation.Renewal.Round = 2
	observation.Renewal.SourcePlanHash = first.PlanHash
	observation.Renewal.ObservedEpoch = 347
	observation.Renewal.ValidFromEpoch, observation.Renewal.ValidToEpoch = 350, 381
	observation.Renewal.CampaignReserveBeforeWei = actionByID(t, first, "campaign.evm-gas-reserve").Spend.EVMGasWei
	for _, fleet := range f.renewal.Fleets {
		manifest, err := protocol.ParseFleetManifest(fleet.Manifest)
		if err != nil {
			t.Fatal(err)
		}
		for index, member := range fleet.Members {
			binding, err := fleetRenewalBinding(*manifest, manifest.Members[index], member.Binding)
			if err != nil {
				t.Fatal(err)
			}
			observation.Evidence[binding.ClientID] = []FleetBindingEvidence{member.Binding}
			observation.Records[binding.ClientID] = fleetBindingVersionRead{Count: new(big.Int).SetUint64(binding.Generation), Record: stabi.STCoordinatorBindingRecord{
				FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientKey: binding.ClientKey, CommitmentHash: binding.CommitmentHash,
				Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, Uid: fleet.UID,
			}}
		}
	}
	renewal, err := prepareFleetRenewal(f.cfg, f.stateDir, first, f.roles, observation)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := appendFleetRenewalPlan(first, renewal)
	if err != nil {
		t.Fatal(err)
	}
	writeFleetCensusTestPlan(t, f.stateDir, plan)
	journal, err := OpenJournal(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	for _, round := range plan.FleetRenewals {
		owner := plan.PlanHash
		if round.Round == 1 {
			owner = first.PlanHash
		}
		for _, fleet := range round.Fleets {
			stem := fleetRenewalStem(round.Round, fleet.Fleet)
			manifest, err := protocol.ParseFleetManifest(fleet.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			commitmentHash, err := manifest.CommitmentHash()
			if err != nil {
				t.Fatal(err)
			}
			if err := atomicWrite(filepath.Join(f.stateDir, "public", stem+".json"), fleet.Manifest, 0o644); err != nil {
				t.Fatal(err)
			}
			commitment := FleetCommitmentEvidence{Schema: fleetCommitmentEvidenceSchemaV2, DeploymentID: plan.DeploymentID, PlanHash: owner,
				ManifestURI: stem + ".json", CommitmentHash: fleetLifecycleHex(commitmentHash), Hotkey: fleetLifecycleHex(manifest.Hotkey),
				ExtrinsicHash: common.Hash{0x31}.Hex(), CommitmentBlock: 100, FinalizedBlock: 100, FinalizedBlockHash: common.Hash{0x32}.Hex()}
			if err := writePublicJSON(filepath.Join(f.stateDir, "public", stem+".commitment.json"), commitment); err != nil {
				t.Fatal(err)
			}
			for index, member := range fleet.Members {
				action := actionByID(t, plan, fleetRenewalActionID(round.Round, fleet.Fleet, "bind", index+1))
				binding := member.Binding
				binding.DeploymentID, binding.PlanHash, binding.ActionID, binding.IntentHash = plan.DeploymentID, owner, action.ID, action.IntentHash
				binding.TransactionHash, binding.BlockHash, binding.BlockNumber = common.Hash{0x33}.Hex(), common.Hash{0x34}.Hex(), 101
				if err := writePublicJSON(filepath.Join(f.stateDir, "public", fmt.Sprintf("%s-member-%d.binding.json", stem, index+1)), binding); err != nil {
					t.Fatal(err)
				}
				path, err := postconditionRelativePath(owner, action.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err := journal.Append(JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: owner, ActionID: action.ID, IntentHash: action.IntentHash,
					Stage: StageVerified, PostconditionHash: common.Hash{0x35}.Hex(), PostconditionPath: path}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	evidence := &FleetLifecycleEvidence{Schema: fleetLifecycleEvidenceSchema, DeploymentID: first.DeploymentID, PlanHash: first.PlanHash,
		RunID: "sealed-release", Stage: fleetLifecycleStageReleaseHandoff, TakeoverEffectiveEpoch: f.renewal.ValidFromEpoch, Renewal: cloneFleetLifecycleRenewal(first.FleetLifecycleRenewal)}
	if err := writePublicJSON(filepath.Join(f.stateDir, "public", "fleet-lifecycle.json"), evidence); err != nil {
		t.Fatal(err)
	}
	return f, plan, evidence
}

func TestFleetObservationSuccessorPreservesSealedLifecycleAndActivation(t *testing.T) {
	f, plan, evidence := fleetObservationSuccessorFixture(t)
	inspect := func(t *testing.T, epoch, round uint64, valid bool) {
		t.Helper()
		descriptors, err := fleetLifecycleEvidenceDescriptors(f.cfg, f.stateDir, epoch)
		if err != nil {
			t.Fatal(err)
		}
		for index, descriptor := range descriptors {
			if descriptor.ManifestName != fleetRenewalStem(round, index+1)+".json" {
				t.Fatalf("epoch %d fleet %d selected %s instead of renewal %d", epoch, index+1, descriptor.ManifestName, round)
			}
		}
		committed, count, bound, uids, hotkeys, groups := inspectFleetEvidence(f.cfg, f.stateDir, epoch)
		if !committed || count != 808 || bound != valid || len(uids) != 202 {
			t.Fatalf("epoch %d census: commitments=%t bindings=%d valid=%t uids=%d; want 202 fleets/808 bindings, valid=%t", epoch, committed, count, bound, len(uids), valid)
		}
		if valid && (len(hotkeys) != 202 || len(groups) != 202) {
			t.Fatal("successor observation lost candidate identities")
		}
		if !valid && (hotkeys != nil || groups != nil) {
			t.Fatal("expired successor retained current candidate identities")
		}
	}
	// Each call decodes plan and lifecycle again and creates a new census cache,
	// so no process-local state can make a stale or missing handoff look valid.
	inspect(t, 349, 1, true)
	inspect(t, 350, 2, true)
	inspect(t, 381, 2, true)
	inspect(t, 382, 2, false)
	t.Run("concurrent recovery", func(t *testing.T) {
		for _, epoch := range []uint64{350, 351, 380, 381} {
			t.Run(fmt.Sprint(epoch), func(t *testing.T) {
				t.Parallel()
				inspect(t, epoch, 2, true)
			})
		}
	})
	lifecyclePath := filepath.Join(f.stateDir, "public", "fleet-lifecycle.json")
	for _, fault := range []string{"foreign-plan", "foreign-deployment", "unsealed-predecessor", "changed-renewal", "current-foreign-deployment"} {
		t.Run(fault, func(t *testing.T) {
			changed := *evidence
			changed.Renewal = cloneFleetLifecycleRenewal(evidence.Renewal)
			switch fault {
			case "foreign-plan":
				changed.PlanHash = common.Hash{0x99}.Hex()
			case "foreign-deployment":
				changed.DeploymentID += "-foreign"
			case "unsealed-predecessor":
				changed.Stage = fleetLifecycleStageAwaitingDemotion
			case "changed-renewal":
				changed.Renewal.ProviderGeneration++
			case "current-foreign-deployment":
				changed.PlanHash, changed.DeploymentID = plan.PlanHash, plan.DeploymentID+"-foreign"
			}
			if err := writePublicJSON(lifecyclePath, changed); err != nil {
				t.Fatal(err)
			}
			if _, err := fleetLifecycleEvidenceDescriptors(f.cfg, f.stateDir, 350); err == nil {
				t.Fatal("successor admitted unauthenticated lifecycle selection")
			}
		})
	}
	if err := writePublicJSON(lifecyclePath, evidence); err != nil {
		t.Fatal(err)
	}
	// Warm census proofs must not admit a changed successor signature.
	bindingPath := filepath.Join(f.stateDir, "public", "fleet-5.renewal-2-member-1.binding.json")
	raw, err := os.ReadFile(bindingPath)
	if err != nil {
		t.Fatal(err)
	}
	var binding FleetBindingEvidence
	if err := json.Unmarshal(raw, &binding); err != nil {
		t.Fatal(err)
	}
	binding.ClientSignature = "0x" + strings.Repeat("00", 64)
	if err := writePublicJSON(bindingPath, binding); err != nil {
		t.Fatal(err)
	}
	if _, _, bound, _, _, _ := inspectFleetEvidence(f.cfg, f.stateDir, 350); bound {
		t.Fatal("successor reused a census proof after signature corruption")
	}
	if err := atomicWrite(bindingPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	// Remove the last authenticated completion without damaging the journal
	// hash chain. An effective but incomplete successor cannot fall back to R1.
	journalPath := filepath.Join(f.stateDir, "journal.jsonl")
	wire, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(wire), "\n"), "\n")
	if err := atomicWrite(journalPath, []byte(strings.Join(lines[:len(lines)-1], "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fleetLifecycleEvidenceDescriptors(f.cfg, f.stateDir, 350); err == nil || !strings.Contains(err.Error(), "not postcondition-verified") {
		t.Fatalf("incomplete successor escaped journal authentication: %v", err)
	}
	inspect(t, 349, 1, true)
}
