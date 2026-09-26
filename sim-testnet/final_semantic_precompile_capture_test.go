// Completed dual-signed probe repairs must remain admissible throughout the
// release census and its independently sealed historical replay.
package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// The fixture retains a real v1 refusal, its signed v2 replacement, both
// finalized repairs, and the dual-signed completion under the original plan.
func finalPrecompileCaptureFixture(t *testing.T) (*finalHistoricalCoordinatorSource, *PrecompileConformanceEvidence, *PrecompileRecoveryCompletion, *precompileRecoveryTestFixture) {
	t.Helper()
	fixture, evidence, entries, completion := completedPrecompileGasRevisionFixture(t)
	source := fixture.base.plan
	source.CoordinatorRepairCarry = nil
	// This synthetic recovery fixture does not configure a fleet batcher.
	for index, action := range source.Actions {
		if action.ID == "fleet.refresh.deploy-batcher" {
			source.Actions = append(source.Actions[:index], source.Actions[index+1:]...)
			break
		}
	}
	current := clonePrecompileProbeSuccessorPlan(t, source)
	current.PlanHash = finalTestHex(0xef)
	current.PriorPlanHashes = []string{source.PlanHash}
	deployment := current.Deployment
	deployment.DeployBlock = 300
	batcher := common.Address{0x91}
	addresses, err := finalReleaseContractAddressSet(&deployment, batcher)
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range []*SetupPlan{source, current} {
		if err := finalAddReleaseDeploymentAddresses(addresses, plan.Deployment); err != nil {
			t.Fatal(err)
		}
		for _, retired := range plan.SupersededDeployments {
			if err := finalAddReleaseDeploymentAddresses(addresses, retired); err != nil {
				t.Fatal(err)
			}
		}
	}
	releaseAddresses, err := finalCanonicalReleaseAddressStrings(addresses)
	if err != nil {
		t.Fatal(err)
	}
	currentAddresses, err := finalReleaseContractAddressSet(&deployment, batcher)
	if err != nil {
		t.Fatal(err)
	}
	canonicalCurrent, err := finalCanonicalReleaseAddressStrings(currentAddresses)
	if err != nil {
		t.Fatal(err)
	}
	minimum := deployment.DeployBlock
	for _, plan := range []*SetupPlan{source, current} {
		if plan.Deployment.DeployBlock != 0 && plan.Deployment.DeployBlock < minimum {
			minimum = plan.Deployment.DeployBlock
		}
		for _, retired := range plan.SupersededDeployments {
			if retired.DeployBlock != 0 && retired.DeployBlock < minimum {
				minimum = retired.DeployBlock
			}
		}
	}
	for _, entry := range entries {
		if entry.PlanHash == source.PlanHash && entry.Stage == StageFinalized && entry.BlockNumber > 0 && entry.BlockNumber < minimum {
			minimum = entry.BlockNumber
		}
	}
	files := make(map[string][]byte)
	for name, value := range map[string]any{"public/precompile-conformance.json": evidence, precompileRecoveryCompletionFilename: completion} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = raw
	}
	return &finalHistoricalCoordinatorSource{
		archive: &finalSemanticArchive{files: files}, current: current, deployment: &deployment,
		plans: map[string]*SetupPlan{source.PlanHash: source, current.PlanHash: current}, entries: entries,
		chain: &FinalCollectedChainSnapshot{FleetBatcher: strings.ToLower(batcher.Hex()), EVMFromBlock: minimum, CurrentReleaseFromBlock: deployment.DeployBlock, CurrentReleaseAddresses: canonicalCurrent, ReleaseContractAddresses: releaseAddresses},
	}, evidence, completion, fixture.base
}

// Before the fix this fails on repair.precompile-residual.v2.1 even though
// the sealed source includes the exact approval and signed closure.
func TestFinalPrecompileCaptureAcceptsCompletedSignedV2Recovery(t *testing.T) {
	source, _, _, _ := finalPrecompileCaptureFixture(t)
	if err := source.verifyReleaseCaptureCensus(); err != nil {
		t.Fatalf("completed signed precompile repair was refused: %v", err)
	}
}
