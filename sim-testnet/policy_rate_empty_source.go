// An empty signed usage epoch has no committable payout root. Its provisional
// startup proof binds the actual source instead of borrowing a settled payout.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// The signer comes from the reviewed public identity census. CanonicalHead is
// filled only after numbered RPC reads authenticate both signed boundaries.
type PolicyRateEmptySourceEvidence struct {
	Artifact       payoutArtifact `json:"artifact"`
	ExpectedSigner string         `json:"expected_signer"`
	CanonicalHead  ChainHead      `json:"canonical_head"`
}

// Only the genuinely empty source gets an alternate proof: nonempty usage,
// provider rows or leaves still require the original chain-matching predicate.
func emptyPolicyRateSourceArtifact(cfg *ResolvedConfig, contracts *ContractView, source PolicyRateSourceObservation, evidence *PolicyRateEmptySourceEvidence) bool {
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.Policy == nil || contracts == nil || contracts.Deployment == nil || evidence == nil {
		return false
	}
	a := &evidence.Artifact
	if !common.IsHexAddress(evidence.ExpectedSigner) || common.HexToAddress(evidence.ExpectedSigner) == (common.Address{}) || !strings.EqualFold(a.Signer.Hex(), evidence.ExpectedSigner) || verifyPayoutArtifact(a) != nil {
		return false
	}
	if a.DeploymentID != cfg.Config.Deployment.DeploymentID || a.ChainID != cfg.ChainID || a.Netuid != cfg.Netuid || !strings.EqualFold(a.GenesisHash, cfg.Public.Chain.GenesisHash) || a.Coordinator != contracts.Deployment.CoordinatorProxy || a.SettlementVault != contracts.Deployment.SettlementVault || a.PolicyHash != cfg.PolicyHash || a.ReliabilityAMin != cfg.Policy.Verify.ReliabilityAMin {
		return false
	}
	if a.NoID != source.NoId || a.Epoch != source.Epoch || a.PolicyHash != source.PolicyHash || a.ContentHash != source.ContentHash || a.TotalUsageBytes != source.TotalUsageBytes || a.TotalUsageBytes != 0 || len(a.Providers) != 0 || len(a.Leaves) != 0 || a.PayoutRoot != ([32]byte{}) {
		return false
	}
	if contracts.CurrentEpoch == 0 || a.Epoch != contracts.CurrentEpoch-1 || a.Epoch < contracts.Policy.EffectiveEpoch || contracts.Policy.EpochBlocks == 0 || contracts.CurrentEpochStart < contracts.Policy.EpochBlocks || a.Start.Number != contracts.CurrentEpochStart-contracts.Policy.EpochBlocks || a.End.Number != contracts.CurrentEpochStart || a.End.Number > contracts.FinalizedHead.Number {
		return false
	}
	// Empty artifacts cannot own a chain commitment. A conflicting nonempty
	// root or hash must not be concealed by a valid empty operator signature.
	found := false
	zero := common.Hash{}.Hex()
	for _, epoch := range contracts.Epochs {
		if epoch.Epoch != a.Epoch {
			continue
		}
		for _, operator := range epoch.Operators {
			if operator.NoID == a.NoID {
				if found || operator.PayoutRoot != zero || operator.ArtifactHash != zero || operator.CommitBlock != 0 {
					return false
				}
				found = true
			}
		}
	}
	return found
}

// Retain only an exact empty source authenticated by the ordinary artifact
// reader's reviewed signer. RPC boundary authentication happens after census.
func observeEmptyPolicyRateSource(cfg *ResolvedConfig, contracts *ContractView, source PolicyRateSourceObservation, artifact payoutArtifact, expectedSigner string) *PolicyRateEmptySourceEvidence {
	if !provisionalResumeEnabled(cfg) || cfg.readOnlyAudit {
		return nil
	}
	evidence := &PolicyRateEmptySourceEvidence{Artifact: artifact, ExpectedSigner: expectedSigner}
	if !emptyPolicyRateSourceArtifact(cfg, contracts, source, evidence) {
		return nil
	}
	return evidence
}

// Cache shared boundaries for this observation only, and recheck the pinned
// observation head last. A transient read never leaves an authenticated marker.
func authenticateEmptyPolicyRateSources(ctx context.Context, cfg *ResolvedConfig, contracts *ContractView, operators []OperatorObservation, reader evmBlockReader) error {
	heads := map[uint64]ChainHead{}
	var pending []*PolicyRateEmptySourceEvidence
	for _, operator := range operators {
		if operator.EmptyRateSource != nil {
			operator.EmptyRateSource.CanonicalHead = ChainHead{}
		}
	}
	for _, operator := range operators {
		evidence := operator.EmptyRateSource
		if evidence == nil {
			continue
		}
		if operator.RateSource == nil || uint64(operator.NoID) != operator.RateSource.NoId || !emptyPolicyRateSourceArtifact(cfg, contracts, *operator.RateSource, evidence) || reader == nil {
			return errors.New("empty rate source changed its exact signed authority")
		}
		for _, boundary := range []ChainHead{{Number: evidence.Artifact.Start.Number, Hash: evidence.Artifact.Start.Hash}, {Number: evidence.Artifact.End.Number, Hash: evidence.Artifact.End.Hash}} {
			head, ok := heads[boundary.Number]
			if !ok {
				var err error
				head, err = reader.EVMBlockByNumber(ctx, new(big.Int).SetUint64(boundary.Number))
				if err != nil {
					return fmt.Errorf("empty rate source canonical boundary: %w", err)
				}
				heads[boundary.Number] = head
			}
			if head != boundary {
				return errors.New("empty rate source boundary differs from canonical chain")
			}
		}
		pending = append(pending, evidence)
	}
	if len(pending) == 0 {
		return nil
	}
	head, err := reader.EVMBlockByNumber(ctx, new(big.Int).SetUint64(contracts.FinalizedHead.Number))
	if err != nil || head != contracts.FinalizedHead {
		return errors.Join(errors.New("empty rate source observation head changed"), err)
	}
	for _, evidence := range pending {
		evidence.CanonicalHead = head
	}
	return nil
}
