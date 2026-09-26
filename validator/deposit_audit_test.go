package validator

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/payoutartifact"
	"github.com/urfoundation/sn/protocol"
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
	required, _, err := protocol.RequiredDepositRao(artifact.TotalUsageBytes, artifact.TotalUsers, conviction, policy)
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
	required, _, err := protocol.RequiredDepositRao(artifact.TotalUsageBytes, artifact.TotalUsers, big.NewInt(0), policy)
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

// Under a zero-price policy the audit is chain state only: eligible whatever
// the operator deposited, with nothing required and no artifact evidence.
func TestZeroPriceDepositAuditIsEligibleWhateverWasDeposited(t *testing.T) {
	for _, observed := range []*big.Int{big.NewInt(0), big.NewInt(5_000_000_000)} {
		audit := ZeroPriceDepositAudit(5, 4, 1, observed, big.NewInt(20))
		if !audit.Compliant || audit.Status != DepositAuditZeroPrice || audit.Disposition != DepositDispositionZeroPrice || audit.RequiredDepositRao != "0" || audit.ObservedDepositRao != observed.String() || audit.ConvictionBeforeRao != "20" || audit.Error != "" || audit.ArtifactHash != "" || audit.UsageBytes != 0 || audit.Users != 0 || audit.SourceEpoch != 4 {
			t.Fatalf("zero-price audit for deposit %s = %+v", observed, audit)
		}
	}
	encoded, err := json.Marshal(ZeroPriceDepositAudit(5, 4, 1, big.NewInt(0), big.NewInt(0)))
	if err != nil || strings.Contains(string(encoded), `"users"`) || strings.Contains(string(encoded), "rate_numerator_rao_per_user") {
		t.Fatalf("optional audit fields serialized when zero: %s %v", encoded, err)
	}
	policy := exactPolicy(t).Deposit
	if depositAuditSourceEpoch(0, policy) != 0 || depositAuditSourceEpoch(1, policy) != 0 || depositAuditSourceEpoch(5, policy) != 4 {
		t.Fatal("zero-price source epoch does not follow the policy lag")
	}
}

// The per-user rate prices the artifact's attested user count alongside its
// bytes; a per-GiB-only policy prices the same artifact's users at zero.
func TestEvaluateDepositArtifactPricesAttestedUsers(t *testing.T) {
	artifact, err := payoutartifact.Build(payoutartifact.BuildInput{
		DeploymentID: "test-deployment", GenesisHash: "0x" + strings.Repeat("ab", 32),
		PolicyHash: "0x" + strings.Repeat("cd", 32), ChainID: 945, Netuid: 521,
		Coordinator: common.HexToAddress("0x100"), SettlementVault: common.HexToAddress("0x200"),
		Epoch: 4, NoID: 1,
		Start:                payoutartifact.Boundary{Number: 100, Hash: "0x" + strings.Repeat("01", 32)},
		End:                  payoutartifact.Boundary{Number: 200, Hash: "0x" + strings.Repeat("02", 32)},
		OperatorSnapshotHash: "sha256:" + strings.Repeat("10", 32),
		FleetSnapshotHash:    "sha256:" + strings.Repeat("20", 32),
		Providers:            []payoutartifact.ProviderInput{{ClientID: [16]byte{1}, Coldkey: [32]byte{1}, UsageBytes: 3 * 1024 * 1024 * 1024, Assignments: 8, Confirmations: 8, Eligible: true}},
		TotalUsers:           1_000,
		ReliabilityAMin:      8, CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(artifact, key); err != nil {
		t.Fatal(err)
	}
	expectation := depositAuditExpectation(t, artifact)
	priced := exactPolicy(t).Deposit
	priced.Unit = protocol.DepositUnitRaoPerGiBAndUser
	for index, perUser := range []uint64{10, 8, 6} {
		priced.Tiers[index].RateNumeratorRaoPerUser = perUser
	}
	conviction := big.NewInt(0)
	bytesOnly, _, err := protocol.RequiredDepositRao(artifact.TotalUsageBytes, 0, conviction, priced)
	if err != nil {
		t.Fatal(err)
	}
	required, _, err := protocol.RequiredDepositRao(artifact.TotalUsageBytes, artifact.TotalUsers, conviction, priced)
	if err != nil || new(big.Int).Sub(required, bytesOnly).Cmp(big.NewInt(10_000)) != 0 {
		t.Fatalf("users priced %s over %s, want 10000 more (%v)", required, bytesOnly, err)
	}
	audit := EvaluateDepositArtifact(artifact, expectation, 5, required, conviction, priced)
	if !audit.Compliant || audit.Status != DepositAuditCompliant || audit.Users != 1_000 || audit.RateNumeratorRaoPerUser != 10 || audit.RateNumeratorRaoPerGiB != 1_000_000 || audit.RequiredDepositRao != required.String() {
		t.Fatalf("compliant two-component audit = %+v", audit)
	}
	audit = EvaluateDepositArtifact(artifact, expectation, 5, bytesOnly, conviction, priced)
	if audit.Compliant || audit.Status != DepositAuditMismatch || audit.Disposition != "zero_pool_weight" {
		t.Fatalf("deposit that ignored the per-user rate was accepted: %+v", audit)
	}

	perGiBOnly := exactPolicy(t).Deposit
	audit = EvaluateDepositArtifact(artifact, expectation, 5, bytesOnly, conviction, perGiBOnly)
	if !audit.Compliant || audit.Users != 1_000 || audit.RateNumeratorRaoPerUser != 0 || audit.RequiredDepositRao != bytesOnly.String() {
		t.Fatalf("per-GiB-only policy did not price users at zero: %+v", audit)
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
