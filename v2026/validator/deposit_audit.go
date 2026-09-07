package validator

// deposit_audit.go contains the pure, deterministic trust decision between a
// signed prior-epoch payout artifact and the current on-chain operator deposit.
// Any identity, boundary, commitment, tier, or amount mismatch yields zero pool
// weight and remains visible in the immutable steering intent.

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
)

const (
	DepositAuditBootstrap          = "bootstrap"
	DepositAuditCompliant          = "compliant"
	DepositAuditMismatch           = "deposit_mismatch"
	DepositAuditUnavailablePending = "artifact_pending"
	DepositAuditUnavailable        = "artifact_unavailable"
	DepositAuditInvalid            = "artifact_invalid"
	DepositAuditEquivocation       = "artifact_equivocation"
)

// DepositAudit is the exact validator evidence for one operator's demand
// signal. Decimal strings preserve uint256 conviction/deposit values.
type DepositAudit struct {
	NoID                   uint64 `json:"no_id"`
	Epoch                  uint64 `json:"epoch"`
	SourceEpoch            uint64 `json:"source_epoch"`
	ArtifactHash           string `json:"artifact_hash,omitempty"`
	CommittedArtifactHash  string `json:"committed_artifact_hash,omitempty"`
	PayoutRoot             string `json:"payout_root,omitempty"`
	ArtifactSigner         string `json:"artifact_signer,omitempty"`
	RootCommitter          string `json:"root_committer,omitempty"`
	RootSigner             string `json:"root_signer,omitempty"`
	SourceStartBlock       uint64 `json:"source_start_block,omitempty"`
	SourceStartHash        string `json:"source_start_hash,omitempty"`
	SourceEndBlock         uint64 `json:"source_end_block,omitempty"`
	SourceEndHash          string `json:"source_end_hash,omitempty"`
	RootCommitBlock        uint64 `json:"root_commit_block,omitempty"`
	ObservedAtBlock        uint64 `json:"observed_at_block"`
	ArtifactDeadlineBlock  uint64 `json:"artifact_deadline_block"`
	UsageBytes             uint64 `json:"usage_bytes"`
	ConvictionBeforeRao    string `json:"conviction_before_rao"`
	RateNumeratorRaoPerGiB uint64 `json:"rate_numerator_rao_per_gib"`
	RateDenominator        uint64 `json:"rate_denominator"`
	RequiredDepositRao     string `json:"required_deposit_rao"`
	ObservedDepositRao     string `json:"observed_deposit_rao"`
	Status                 string `json:"status"`
	Compliant              bool   `json:"compliant"`
	Disposition            string `json:"disposition"`
	Error                  string `json:"error,omitempty"`
}

// DepositArtifactExpectation pins every artifact field that does not come from
// the artifact itself, including its finalized chain commitment.
type DepositArtifactExpectation struct {
	DeploymentID    string
	ChainID         uint64
	GenesisHash     string
	Netuid          uint16
	Coordinator     common.Address
	SettlementVault common.Address
	PolicyHash      string
	Epoch           uint64
	NoID            uint64
	Signer          common.Address
	Start           payoutartifact.Boundary
	End             payoutartifact.Boundary
	PayoutRoot      [32]byte
	ArtifactHash    [32]byte
	Committer       common.Address
	RootSigner      common.Address
	CommitBlock     uint64
}

func baseDepositAudit(epoch, sourceEpoch, noID uint64, observed, conviction *big.Int) DepositAudit {
	audit := DepositAudit{
		NoID: noID, Epoch: epoch, SourceEpoch: sourceEpoch,
		Status: DepositAuditInvalid, Disposition: "zero_pool_weight",
		RequiredDepositRao: "0", ObservedDepositRao: "0", ConvictionBeforeRao: "0",
	}
	if observed != nil {
		audit.ObservedDepositRao = observed.String()
	}
	if conviction != nil {
		audit.ConvictionBeforeRao = conviction.String()
	}
	return audit
}

// FailedDepositAudit records a transport/history failure without turning a
// dishonest or unavailable operator into a validator-wide outage.
func FailedDepositAudit(epoch, sourceEpoch, noID uint64, observed, conviction *big.Int, status string, failure error) DepositAudit {
	audit := baseDepositAudit(epoch, sourceEpoch, noID, observed, conviction)
	audit.Status = status
	if failure != nil {
		audit.Error = failure.Error()
	}
	return audit
}

// EvaluateDepositArtifact reconstructs and validates an operator statement,
// then applies the shared exact floor-and-cap formula. Overdepositing is also a
// mismatch: it cannot buy weight beyond the usage the operator signed.
func EvaluateDepositArtifact(
	artifact *payoutartifact.Artifact,
	expectation DepositArtifactExpectation,
	epoch uint64,
	observedDeposit *big.Int,
	convictionBefore *big.Int,
	depositPolicy protocol.DepositPolicy,
) DepositAudit {
	audit := baseDepositAudit(epoch, expectation.Epoch, expectation.NoID, observedDeposit, convictionBefore)
	if observedDeposit == nil || observedDeposit.Sign() < 0 || convictionBefore == nil || convictionBefore.Sign() < 0 {
		audit.Error = "deposit or conviction snapshot is nil or negative"
		return audit
	}
	if err := payoutartifact.Verify(artifact); err != nil {
		audit.Error = err.Error()
		return audit
	}
	audit.ArtifactHash = artifact.ContentHash
	audit.ArtifactSigner = strings.ToLower(artifact.Signer.Hex())
	audit.SourceStartBlock = artifact.Start.Number
	audit.SourceStartHash = strings.ToLower(artifact.Start.Hash)
	audit.SourceEndBlock = artifact.End.Number
	audit.SourceEndHash = strings.ToLower(artifact.End.Hash)
	audit.CommittedArtifactHash = "0x" + hex.EncodeToString(expectation.ArtifactHash[:])
	audit.PayoutRoot = "0x" + hex.EncodeToString(expectation.PayoutRoot[:])
	audit.RootCommitter = strings.ToLower(expectation.Committer.Hex())
	audit.RootSigner = strings.ToLower(expectation.RootSigner.Hex())
	audit.RootCommitBlock = expectation.CommitBlock
	if artifact.DeploymentID != expectation.DeploymentID || artifact.ChainID != expectation.ChainID || artifact.Netuid != expectation.Netuid || artifact.Coordinator != expectation.Coordinator || artifact.SettlementVault != expectation.SettlementVault || artifact.Epoch != expectation.Epoch || artifact.NoID != expectation.NoID || artifact.Signer != expectation.Signer || !strings.EqualFold(artifact.GenesisHash, expectation.GenesisHash) || !strings.EqualFold(artifact.PolicyHash, expectation.PolicyHash) {
		audit.Error = "payout artifact deployment identity mismatch"
		return audit
	}
	if artifact.Start.Number != expectation.Start.Number || artifact.End.Number != expectation.End.Number || !strings.EqualFold(artifact.Start.Hash, expectation.Start.Hash) || !strings.EqualFold(artifact.End.Hash, expectation.End.Hash) {
		audit.Error = "payout artifact finalized boundary mismatch"
		return audit
	}
	artifactHash, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(artifact.ContentHash), "sha256:"))
	if err != nil || len(artifactHash) != 32 {
		audit.Error = "payout artifact content hash is invalid"
		return audit
	}
	var artifactHash32 [32]byte
	copy(artifactHash32[:], artifactHash)
	if expectation.CommitBlock == 0 || expectation.PayoutRoot == ([32]byte{}) || expectation.ArtifactHash == ([32]byte{}) || expectation.Committer == (common.Address{}) || expectation.Committer != expectation.RootSigner || artifact.PayoutRoot != expectation.PayoutRoot || artifactHash32 != expectation.ArtifactHash {
		audit.Error = "payout artifact does not match its on-chain root commitment"
		return audit
	}
	required, tier, err := protocol.RequiredDepositRao(artifact.TotalUsageBytes, convictionBefore, depositPolicy)
	if err != nil {
		audit.Error = fmt.Sprintf("canonical deposit formula: %v", err)
		return audit
	}
	audit.UsageBytes = artifact.TotalUsageBytes
	audit.RateNumeratorRaoPerGiB = tier.RateNumeratorRaoPerGiB
	audit.RateDenominator = tier.RateDenominator
	audit.RequiredDepositRao = required.String()
	if observedDeposit.Cmp(required) != 0 {
		audit.Status = DepositAuditMismatch
		audit.Error = "observed deposit does not equal the signed-usage requirement"
		return audit
	}
	audit.Status = DepositAuditCompliant
	audit.Compliant = true
	audit.Disposition = "pool_weight_eligible"
	return audit
}
