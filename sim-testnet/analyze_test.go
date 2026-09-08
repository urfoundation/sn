package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

func TestAnalysisReconstructsExactRationalDepositAndConservation(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Policy.Deposit.Tiers = []protocol.DepositTier{{MinConvictionRao: 0, RateNumeratorRaoPerGiB: 3, RateDenominator: 2}}
	observation := testScenarioObservation(cfg, 1)
	observation.Status.Contracts.Operators = []OperatorView{{NoID: 1, Active: true, PoolLive: true, ConvictionRao: "7"}}
	observation.Status.Contracts.Epochs = []EpochView{{Epoch: 0, Operators: []EpochOperatorView{{NoID: 1, DepositRao: "3", ConvictionAddedRao: "4", Status: 2}}}}
	report := analyzeScenarioObservation(cfg, observation)
	if !report.Conservation.Holds || report.Conservation.DeltaRao != "0" || report.Conservation.EscrowDeltaRao != "0" || report.Conservation.EscrowBackingDeltaRao != "0" {
		t.Fatalf("conservation = %+v", report.Conservation)
	}
	if len(report.EpochOperators) != 1 {
		t.Fatalf("epoch rows = %+v", report.EpochOperators)
	}
	row := report.EpochOperators[0]
	if row.RateNumeratorRaoPerGiB != "3" || row.RateDenominator != "2" || row.ImpliedUsageBytesFloor != "2147483648" || row.PreEpochConvictionRao != "0" || !report.VoluntaryConvictionObserved || !report.TierReconstructionComplete {
		t.Fatalf("rational reconstruction = %+v", row)
	}
}

func TestAnalysisConservationRejectsMissingPendingFunding(t *testing.T) {
	cfg := testResolvedConfig(t)
	observation := testScenarioObservation(cfg, 1)
	observation.Status.Contracts.PendingFunding = "0"
	report := analyzeScenarioObservation(cfg, observation)
	if report.Conservation.Holds || report.Conservation.EscrowDeltaRao != "2" {
		t.Fatalf("omitted pending funding was accepted: %+v", report.Conservation)
	}
}

func TestAnalysisConservationRejectsUnderbackedEscrow(t *testing.T) {
	cfg := testResolvedConfig(t)
	observation := testScenarioObservation(cfg, 1)
	observation.Status.Contracts.LiveEscrowStake = "6"
	report := analyzeScenarioObservation(cfg, observation)
	if report.Conservation.Holds || report.Conservation.EscrowBackingDeltaRao != "-1" {
		t.Fatalf("underbacked escrow was accepted: %+v", report.Conservation)
	}
}

func TestAnalysisReconstructsTierExactlyWhenHistoryIsPruned(t *testing.T) {
	cfg := testResolvedConfig(t)
	observation := testScenarioObservation(cfg, 10)
	observation.Status.Contracts.Operators = []OperatorView{{NoID: 1, ConvictionRao: "1003"}}
	observation.Status.Contracts.Epochs = []EpochView{{Epoch: 8, Operators: []EpochOperatorView{{NoID: 1, DepositRao: "3", ConvictionAddedRao: "0"}}}}
	report := analyzeScenarioObservation(cfg, observation)
	if report.EpochOperators[0].PreEpochConvictionRao != "1000" || !report.TierReconstructionComplete {
		t.Fatalf("pruned report = %+v", report)
	}
}

func TestAnalysisRejectsImpossibleConvictionHistory(t *testing.T) {
	cfg := testResolvedConfig(t)
	observation := testScenarioObservation(cfg, 10)
	observation.Status.Contracts.Operators = []OperatorView{{NoID: 1, ConvictionRao: "2"}}
	observation.Status.Contracts.Epochs = []EpochView{{Epoch: 8, Operators: []EpochOperatorView{{NoID: 1, DepositRao: "3"}}}}
	report := analyzeScenarioObservation(cfg, observation)
	if report.TierReconstructionComplete || len(report.Discrepancies) == 0 || report.EpochOperators[0].PreEpochConvictionRao != "" {
		t.Fatalf("impossible history accepted: %+v", report)
	}
}

func TestAnalysisRetainsVoluntaryTierEvidenceAfterEpochPruning(t *testing.T) {
	cfg := testResolvedConfig(t)
	observation := testScenarioObservation(cfg, 20)
	observation.VoluntaryConvictionValid = true
	observation.VoluntaryConviction = &VoluntaryConvictionEvidence{BeforeConvictionRao: "0", AfterConvictionRao: "1000000000", AmountRao: "1000000000"}
	observation.Status.Contracts.Operators = []OperatorView{{NoID: 1, ConvictionRao: "1000000001"}}
	observation.Status.Contracts.Epochs = []EpochView{{Epoch: 19, Operators: []EpochOperatorView{{NoID: 1, DepositRao: "1"}}}}
	report := analyzeScenarioObservation(cfg, observation)
	if !report.VoluntaryConvictionObserved || !report.ConvictionTierCrossed || !report.TierReconstructionComplete {
		t.Fatalf("pruned voluntary evidence was not reconstructed: %+v", report)
	}
}

func TestAnalysisHTMLIsSelfContainedAndEscaped(t *testing.T) {
	report := &AnalysisReport{Schema: "urnetwork-sim-analysis-v1", DeploymentID: `<script>alert(1)</script>`, Netuid: 7, Conservation: AnalysisConservation{Holds: true}}
	path := filepath.Join(t.TempDir(), "analysis.html")
	if err := writeAnalysisHTML(path, report); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if containsLiteralScript(text) || len(text) < 100 {
		t.Fatalf("unsafe/incomplete HTML: %s", text)
	}
}

func containsLiteralScript(value string) bool {
	for i := 0; i+8 <= len(value); i++ {
		if value[i:i+8] == "<script>" {
			return true
		}
	}
	return false
}
