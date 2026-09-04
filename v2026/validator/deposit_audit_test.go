package validator

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
)

func depositAuditExpectation(t *testing.T, artifact *payoutartifact.Artifact) DepositArtifactExpectation {
	t.Helper()
	value, err := hex.DecodeString(strings.TrimPrefix(artifact.ContentHash, "sha256:"))
	if err != nil || len(value) != 32 {
		t.Fatalf("artifact hash = %q, %v", artifact.ContentHash, err)
	}
	var artifactHash [32]byte
	copy(artifactHash[:], value)
	rootSigner := common.HexToAddress("0x3333333333333333333333333333333333333333")
	return DepositArtifactExpectation{
		DeploymentID: artifact.DeploymentID, ChainID: artifact.ChainID, GenesisHash: artifact.GenesisHash,
		Netuid: artifact.Netuid, Coordinator: artifact.Coordinator, SettlementVault: artifact.SettlementVault,
		PolicyHash: artifact.PolicyHash, Epoch: artifact.Epoch, NoID: artifact.NoID, Signer: artifact.Signer,
		Start: artifact.Start, End: artifact.End, PayoutRoot: artifact.PayoutRoot, ArtifactHash: artifactHash,
		Committer: rootSigner, RootSigner: rootSigner, CommitBlock: artifact.End.Number + 1,
	}
}

func TestEvaluateDepositArtifactAcceptsOnlyExactSharedFormulaAmount(t *testing.T) {
	artifact, _ := validatorTestArtifact(t)
	policy := exactPolicy(t).Deposit
	conviction := big.NewInt(0)
	required, _, err := protocol.RequiredDepositRao(artifact.TotalUsageBytes, conviction, policy)
	if err != nil || required.Sign() == 0 {
		t.Fatalf("required deposit = %v, %v", required, err)
	}
	expectation := depositAuditExpectation(t, artifact)
	audit := EvaluateDepositArtifact(artifact, expectation, 5, required, conviction, policy)
	if !audit.Compliant || audit.Status != DepositAuditCompliant || audit.RequiredDepositRao != required.String() || audit.ObservedDepositRao != required.String() || audit.UsageBytes != artifact.TotalUsageBytes {
		t.Fatalf("compliant audit = %+v", audit)
	}
	for _, observed := range []*big.Int{
		new(big.Int).Sub(new(big.Int).Set(required), big.NewInt(1)),
		new(big.Int).Add(new(big.Int).Set(required), big.NewInt(1)),
	} {
		audit = EvaluateDepositArtifact(artifact, expectation, 5, observed, conviction, policy)
		if audit.Compliant || audit.Status != DepositAuditMismatch || audit.Disposition != "zero_pool_weight" {
			t.Errorf("mismatched deposit %s was not zero-weighted: %+v", observed, audit)
		}
	}
}

func TestEvaluateDepositArtifactRejectsSignerBoundaryAndCommitmentDrift(t *testing.T) {
	artifact, _ := validatorTestArtifact(t)
	policy := exactPolicy(t).Deposit
	required, _, err := protocol.RequiredDepositRao(artifact.TotalUsageBytes, big.NewInt(0), policy)
	if err != nil {
		t.Fatal(err)
	}
	base := depositAuditExpectation(t, artifact)
	tests := []DepositArtifactExpectation{base, base, base, base}
	tests[0].Signer = common.HexToAddress("0x4444444444444444444444444444444444444444")
	tests[1].Start.Hash = "0x" + strings.Repeat("ff", 32)
	tests[2].ArtifactHash[0] ^= 0xff
	tests[3].Committer = common.HexToAddress("0x5555555555555555555555555555555555555555")
	for _, expectation := range tests {
		audit := EvaluateDepositArtifact(artifact, expectation, 5, required, big.NewInt(0), policy)
		if audit.Compliant || audit.Status != DepositAuditInvalid || audit.Disposition != "zero_pool_weight" {
			t.Errorf("drifted artifact identity was accepted: %+v", audit)
		}
	}
}

func TestFailedDepositAuditNeverMakesUnavailableEvidenceEligible(t *testing.T) {
	for _, status := range []string{DepositAuditUnavailablePending, DepositAuditUnavailable, DepositAuditEquivocation, DepositAuditInvalid} {
		audit := FailedDepositAudit(5, 4, 1, big.NewInt(10), big.NewInt(20), status, ErrArtifactUnavailable)
		if audit.Compliant || audit.Disposition != "zero_pool_weight" || audit.Status != status || audit.Error == "" {
			t.Errorf("failed audit %q = %+v", status, audit)
		}
	}
}
