//go:build linux || darwin

// The exact source window and committed payout identity are on-chain facts.
// They do not establish that an HTTP origin was unavailable in the past.
package validator

import (
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The effective snapshot may use either committed cadence, but cannot claim
// the production cadence before the canonical policy permits that transition.
func validateReleaseDecisionChainV2Policy(query releaseDecisionChainV2Query, actual stabi.STCoordinatorPolicySnapshot) error {
	policy := query.policy
	if actual.PolicyHash != query.domain.PolicyHash || actual.EffectiveEpoch > query.boundary.SettlementEpoch || actual.EffectiveBlock == 0 || actual.EpochBlocks == 0 || actual.EpochDepositCapRao == nil || actual.CampaignDepositCapRao == nil {
		return errors.New("decision policy identity or effective boundary differs")
	}
	initial := actual.EpochBlocks == policy.Settlement.EpochBlocks && actual.RootCommitWindowBlocks == policy.Settlement.RootCommitWindowBlocks && actual.FinalizeOffsetBlocks == policy.Settlement.FinalizeOffsetBlocks && actual.CloseGraceBlocks == policy.Settlement.CloseGraceBlocks
	production := actual.EpochBlocks == policy.ProductionCadence.EpochBlocks && actual.RootCommitWindowBlocks == policy.ProductionCadence.RootCommitWindowBlocks && actual.FinalizeOffsetBlocks == policy.ProductionCadence.FinalizeOffsetBlocks && actual.CloseGraceBlocks == policy.ProductionCadence.CloseGraceBlocks && actual.EffectiveEpoch >= policy.ProductionCadence.AfterAcceleratedEpochs
	if (!initial && !production) || actual.ClaimTTLEpochs != policy.Settlement.ClaimTTLEpochs || actual.ClaimGraceEpochs != policy.Settlement.ClaimGraceEpochs || actual.MaximumBindingValidityEpochs != policy.Binding.MaximumValidityEpochs || actual.EpochDepositCapRao.Cmp(new(big.Int).SetUint64(policy.Deposit.EpochCapRaoPerOperator)) != 0 || actual.CampaignDepositCapRao.Cmp(new(big.Int).SetUint64(policy.Deposit.TotalTestCampaignCapRao)) != 0 {
		return errors.New("decision on-chain policy fields differ from canonical configuration")
	}
	return nil
}

// The closures are private decoding operations created by the actual hash-
// pinned reader; callers cannot inject an observation or acceptance verdict.
// Every root, version and epoch-boundary call uses that same decision hash.
func readReleaseDecisionChainV2Source(ctx context.Context, chain *ChainClient, query releaseDecisionChainV2Query, observed *releaseDecisionChainV2Observation, read func(string, []byte) ([]byte, error), readUint func(string, []byte) (*big.Int, error)) error {
	if query.boundary.SettlementEpoch < query.policy.Deposit.UsageLagEpochs {
		return ctx.Err()
	}
	observed.sourceEpoch = query.boundary.SettlementEpoch - query.policy.Deposit.UsageLagEpochs
	epoch := new(big.Int).SetUint64(observed.sourceEpoch)
	start, err := readUint("epochStartBlock", chain.coordinator.PackEpochStartBlock(epoch))
	if err != nil || !start.IsUint64() || start.Sign() == 0 {
		return errors.Join(errors.New("decision source epoch start is invalid"), err)
	}
	end, err := readUint("epochEndBlock", chain.coordinator.PackEpochEndBlock(epoch))
	if err != nil || !end.IsUint64() || end.Cmp(start) <= 0 || end.Uint64() > query.boundary.EVMBlock || end.Uint64() != observed.epochStart {
		return errors.Join(errors.New("decision source epoch is not the complete preceding rolled window"), err)
	}
	observed.sourceStart, observed.sourceEnd = start.Uint64(), end.Uint64()
	observed.sourceStartHash, err = chain.BlockHashContext(ctx, observed.sourceStart)
	if err != nil {
		return err
	}
	observed.sourceEndHash, err = chain.BlockHashContext(ctx, observed.sourceEnd)
	if err != nil {
		return err
	}
	for index, operator := range observed.operators {
		id := new(big.Int).SetUint64(operator.noID)
		encoded, err := read("operatorAt", chain.coordinator.PackOperatorAt(id, epoch))
		if err != nil {
			return err
		}
		version, err := chain.coordinator.UnpackOperatorAt(encoded)
		if err != nil || version.EffectiveEpoch > observed.sourceEpoch {
			return errors.Join(errors.New("decision source operator version is from a later epoch"), err)
		}
		encoded, err = read("rootCommitments", chain.coordinator.PackRootCommitments(epoch, id))
		if err != nil {
			return err
		}
		commitment, err := chain.coordinator.UnpackRootCommitments(encoded)
		if err != nil {
			return err
		}
		if commitment.CommitBlock == 0 {
			if commitment != (stabi.RootCommitmentsOutput{}) {
				return errors.New("decision absent source commitment contains an occupied identity")
			}
		} else if commitment.CommitBlock > query.boundary.EVMBlock || commitment.PayoutRoot == ([32]byte{}) || commitment.ArtifactHash == ([32]byte{}) || commitment.Committer == (common.Address{}) || commitment.Committer != version.RootSigner || !version.Active {
			return errors.New("decision source root differs from its historical operator authority")
		}
		observed.operators[index].sourceVersion, observed.operators[index].commitment = version, commitment
	}
	for _, boundary := range []struct {
		block uint64
		hash  [32]byte
	}{{block: observed.sourceStart, hash: observed.sourceStartHash}, {block: observed.sourceEnd, hash: observed.sourceEndHash}} {
		hash, err := chain.BlockHashContext(ctx, boundary.block)
		if err != nil || hash != boundary.hash {
			return errors.Join(errors.New("decision source boundary hash changed during observation"), err)
		}
	}
	return ctx.Err()
}
