//go:build linux || darwin

// A complete registry observation is not intent eligibility. These controls
// retain three real pinned operators, including two independently healthy
// peers, while the historical consumer admits only an all-active census.
package validator

import (
	"fmt"
	"math/big"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

// The caller owns all source responses and reference claims. Reads use the
// actual native schedule/stake and geth hash-pinned coordinator clients.
type releaseDecisionV2EligibilityTestFixture struct {
	decision *releaseDecisionV2TestFixture
	native   *releaseNativeValidatorTestFixture
	history  *releaseEvidenceV2StartupHistory
	intent   *SteeringIntent
	artifact *ReleaseMeasurementArtifact
}

// Give the third operator its own registry identity, pool and bounded input.
// Inactive bindings and real root absence avoid missing historical API facts;
// neither is supplied as an acceptance callback or a signed-claim verdict.
func newReleaseDecisionV2EligibilityTestFixture(t *testing.T, thirdProviderIDs []connect.Id) *releaseDecisionV2EligibilityTestFixture {
	t.Helper()
	decision := newReleaseDecisionV2TestFixture(t)
	decision.query.maxOperators = 3
	decision.query.operators = append(decision.query.operators, releaseDecisionChainV2OperatorQuery{noID: 3, providerIDs: slices.Clone(thirdProviderIDs)})
	coordinator := decision.chain.coordinator
	decision.set(t, "operatorCount", coordinator.PackOperatorCount(), big.NewInt(3))
	decision.set(t, "operatorIdAt", coordinator.PackOperatorIdAt(big.NewInt(2)), big.NewInt(3))
	version := stabi.STCoordinatorOperatorVersion{Coldkey: [32]byte{0x42}, PoolHotkey: chainBatchHotkey(2), DepositHotkey: [32]byte{0x52}, DepositSigner: common.Address{0x62}, RootSigner: common.Address{0x72}, Active: true}
	decision.versions = append(decision.versions, version)
	decision.commitments = append(decision.commitments, stabi.RootCommitmentsOutput{})
	decision.set(t, "operatorAt", coordinator.PackOperatorAt(big.NewInt(3), big.NewInt(1)), version)
	decision.set(t, "operatorAt", coordinator.PackOperatorAt(big.NewInt(3), big.NewInt(0)), version)
	decision.set(t, "epochDeposits", coordinator.PackEpochDeposits(big.NewInt(1), big.NewInt(3)), big.NewInt(15))
	decision.set(t, "epochConvictionAdded", coordinator.PackEpochConvictionAdded(big.NewInt(1), big.NewInt(3)), big.NewInt(7))
	decision.set(t, "cumulativeConviction", coordinator.PackCumulativeConviction(big.NewInt(3)), new(big.Int).Add(new(big.Int).Set(decision.bigConviction), big.NewInt(22)))
	for index, operator := range decision.query.operators {
		decision.commitments[index] = stabi.RootCommitmentsOutput{}
		decision.set(t, "rootCommitments", coordinator.PackRootCommitments(big.NewInt(0), new(big.Int).SetUint64(operator.noID)), [32]byte{}, [32]byte{}, common.Address{}, uint64(0))
		for _, provider := range operator.providerIDs {
			decision.set(t, "bindingAt", coordinator.PackBindingAt([16]byte(provider), big.NewInt(1)), false, stabi.STCoordinatorBindingRecord{})
		}
	}
	native := newReleaseDecisionV2NativeTestFixture(t, decision)
	history, intent, artifact := releaseDecisionV2HistoricalTestReference(t, decision, native)
	root := filepath.Dir(history.cfg.StateDir)
	history.cfg.Operators = append(history.cfg.Operators, OperatorConfig{NoID: 3, APIURL: "https://three.example", ConnectURL: "wss://three.example/connect", ArtifactSigner: "0x3333333333333333333333333333333333333333", StateDir: filepath.Join(root, "no-3"), Concurrency: 2})
	history.cfg.EvidenceV2 = releaseEvidenceV2TestConfig(root, history.cfg.Operators)
	if err := history.cfg.EvidenceV2.Bounds.Validate(uint64(len(history.cfg.Operators))); err != nil {
		t.Fatalf("three-operator bounded input prerequisite failed: %v", err)
	}
	if history.cfg.Policy.Safety.MinimumHealthyNOCount != 2 || len(history.participants) != 3 || len(artifact.Pools) != 3 || len(artifact.DepositAudits) != 3 {
		t.Fatal("historical eligibility prerequisite lost its three-operator census or two-peer minimum")
	}
	return &releaseDecisionV2EligibilityTestFixture{decision: decision, native: native, history: history, intent: intent, artifact: artifact}
}

// First admit the complete active reference through genuine readers, then
// alter one independently encoded operator version at the decision hash.
// The candidate follows the new pool/audit census; eligibility must not.
func assertReleaseDecisionV2InactiveReferenceRefused(t *testing.T, fixture *releaseDecisionV2EligibilityTestFixture, inactiveNoID uint64) {
	t.Helper()
	decision, native, history, intent, artifact := fixture.decision, fixture.native, fixture.history, fixture.intent, fixture.artifact
	if err := history.authenticateIntentChainReference(t.Context(), decision.chain, native.chain, native.expected, intent, artifact); err != nil {
		t.Fatalf("all-active historical eligibility prerequisite failed: %v", err)
	}
	version := decision.versions[inactiveNoID-1]
	version.Active = false
	decision.set(t, "operatorAt", decision.chain.coordinator.PackOperatorAt(new(big.Int).SetUint64(inactiveNoID), big.NewInt(1)), version)
	artifact.Pools = slices.DeleteFunc(artifact.Pools, func(pool ReleasePoolMeasurement) bool { return pool.NoID == inactiveNoID })
	artifact.DepositAudits = slices.DeleteFunc(artifact.DepositAudits, func(audit DepositAudit) bool { return audit.NoID == inactiveNoID })
	observed, err := decision.chain.readReleaseDecisionChainV2Context(t.Context(), decision.query)
	if err != nil || len(observed.operators) != 3 || len(observed.pools) != 2 || observed.operators[inactiveNoID-1].version.Active || observed.operators[inactiveNoID-1].noID != inactiveNoID || !slices.Equal(observed.pools, artifact.Pools) || !slices.Equal(observed.bindings, artifact.Bindings) {
		t.Fatalf("complete observation lost the inactive member or its two healthy peers: %v", err)
	}
	beforeEpoch, beforeOperator := decision.count("currentEpoch"), decision.count("operatorAt")
	err = history.authenticateIntentChainReference(t.Context(), decision.chain, native.chain, native.expected, intent, artifact)
	want := fmt.Sprintf("historical decision operator %d was inactive at the referenced boundary", inactiveNoID)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("historical intent admitted inactive participant with healthy quorum: no_id=%d err=%v", inactiveNoID, err)
	}
	if decision.count("currentEpoch")-beforeEpoch != 2 || decision.count("operatorAt")-beforeOperator != 6 {
		t.Fatal("historical eligibility did not independently reread the exact complete chain census")
	}
}

// The last member reproduces a third inactive participant with two healthy
// operators. First/middle variations prevent prefix or positional admission.
func TestReleaseEvidenceV2DecisionHistoricalIntentRequiresEveryActiveParticipant(t *testing.T) {
	t.Parallel()
	for _, inactiveNoID := range []uint64{3, 1, 2} {
		fixture := newReleaseDecisionV2EligibilityTestFixture(t, []connect.Id{releaseMeasurementTestID(4)})
		assertReleaseDecisionV2InactiveReferenceRefused(t, fixture, inactiveNoID)
	}
}

// An empty provider input still owns an operator lane; filtering providers
// or active pools cannot remove that participant's eligibility obligation.
func TestReleaseEvidenceV2DecisionHistoricalIntentRequiresActiveEmptyParticipant(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2EligibilityTestFixture(t, nil)
	if len(fixture.history.inputByEpoch[1][3].MeasurementInput.Stats.Providers) != 0 {
		t.Fatal("empty operator prerequisite unexpectedly acquired providers")
	}
	assertReleaseDecisionV2InactiveReferenceRefused(t, fixture, 3)
}
