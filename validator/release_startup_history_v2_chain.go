//go:build linux || darwin

// Current-cursor history uses actual historical native and EVM observations.
// A signed cut cannot invent its epoch/window, and a ledger's historical UID
// cannot replace the hotkey's real native registration at an ordinary cut.
package validator

import (
	"context"
	"errors"
	"math/big"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

// Reads canonical hash-pinned policy and complete operator eligibility. The
// terminal additionally needs the actual rolled exclusive epoch-end boundary,
// not merely an old snapshot whose block happened to be finalized.
func (self *ChainClient) authenticateReleaseStartupBoundaryV2Context(ctx context.Context, domain protocol.ValidatorEvidenceDomain, noID uint64, boundary AttemptBoundary, terminal bool) (resultErr error) {
	return self.authenticateReleaseStartupBoundaryV2ContextWithRetainedHistory(ctx, domain, noID, boundary, terminal, false)
}

// Only startup's complete validated provisional handoff waives repeated
// external history comparisons; all runtime callers retain the strict wrapper.
func (self *ChainClient) authenticateReleaseStartupBoundaryV2ContextWithRetainedHistory(ctx context.Context, domain protocol.ValidatorEvidenceDomain, noID uint64, boundary AttemptBoundary, terminal, retained bool) (resultErr error) {
	if ctx == nil || self == nil || self.client == nil || self.coordinator == nil || self.chainId == nil {
		return errors.New("startup history EVM owner is absent")
	}
	defer func() { resultErr = errors.Join(resultErr, ctx.Err()) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := domain.Validate(); err != nil {
		return err
	}
	if err := self.requireRelease(); err != nil {
		return err
	}
	if noID == 0 || boundary.EVMBlock == 0 || self.contractAddr != common.Address(domain.Coordinator) || self.chainId.Cmp(new(big.Int).SetUint64(domain.ChainID)) != 0 {
		return errors.New("startup history boundary differs from the configured chain owner")
	}
	hash, err := canonicalAttemptHex32("startup history EVM hash", boundary.EVMBlockHash, false)
	if err != nil {
		return err
	}
	if retained {
		return ctx.Err()
	}
	finalized, finalizedHash, err := self.FinalizedBlockContext(ctx)
	if err != nil {
		return err
	}
	if finalized < boundary.EVMBlock || finalized == boundary.EVMBlock && (terminal || finalizedHash != hash) {
		return errors.New("startup history boundary is not finalized in its required window")
	}
	epoch := new(big.Int).SetUint64(boundary.SettlementEpoch)
	outputs, err := self.batchCallsAtHashContext(ctx, boundary.EVMBlock, hash, []chainBatchCall{
		{address: self.contractAddr, calldata: self.coordinator.PackCurrentEpoch()},
		{address: self.contractAddr, calldata: self.coordinator.PackPolicyAt(epoch)},
		{address: self.contractAddr, calldata: self.coordinator.PackOperatorAt(new(big.Int).SetUint64(noID), epoch)},
	})
	if err != nil {
		return err
	}
	current, err := self.coordinator.UnpackCurrentEpoch(outputs[0])
	if err != nil || current == nil || !current.IsUint64() || current.Uint64() != boundary.SettlementEpoch {
		return errors.Join(errors.New("startup history boundary belongs to another on-chain epoch"), err)
	}
	if err := canonicalReleaseActivationV2View("currentEpoch", outputs[0], current); err != nil {
		return err
	}
	policy, err := self.coordinator.UnpackPolicyAt(outputs[1])
	if err != nil {
		return err
	}
	if err := canonicalReleaseActivationV2View("policyAt", outputs[1], policy); err != nil {
		return err
	}
	if policy.PolicyHash != domain.PolicyHash || policy.EffectiveEpoch > boundary.SettlementEpoch || policy.EffectiveBlock == 0 || policy.EpochBlocks == 0 {
		return errors.New("startup history policy differs from independent deployment authority")
	}
	start := new(big.Int).Mul(new(big.Int).SetUint64(boundary.SettlementEpoch-policy.EffectiveEpoch), new(big.Int).SetUint64(policy.EpochBlocks))
	start.Add(start, new(big.Int).SetUint64(policy.EffectiveBlock))
	end := new(big.Int).Add(new(big.Int).Set(start), new(big.Int).SetUint64(policy.EpochBlocks))
	block := new(big.Int).SetUint64(boundary.EVMBlock)
	if block.Cmp(start) < 0 || block.Cmp(end) >= 0 {
		return errors.New("startup history boundary falls outside its on-chain policy window")
	}
	operator, err := self.coordinator.UnpackOperatorAt(outputs[2])
	if err != nil {
		return err
	}
	if err := canonicalReleaseActivationV2View("operatorAt", outputs[2], operator); err != nil {
		return err
	}
	if !operator.Active || operator.EffectiveEpoch > boundary.SettlementEpoch {
		return errors.New("startup history operator was inactive at its boundary")
	}
	if terminal {
		if !end.IsUint64() || end.Uint64() == 0 || boundary.EVMBlock != end.Uint64()-1 || finalized < end.Uint64() {
			return errors.New("startup history terminal does not end its independently observed window")
		}
		actualEnd, err := self.ReleaseEpochEndBlockAtHashContext(ctx, finalized, finalizedHash, epoch)
		if err != nil || actualEnd != end.Uint64() {
			return errors.Join(errors.New("startup history terminal differs from the actual rolled epoch end"), err)
		}
	}
	return ctx.Err()
}

// The native block/hash and signing hotkey are explicit. A real selective
// metagraph response supplies calculated stake, while the exact historical
// metadata supplies SubnetEpochIndex without modifying current Chain.Meta.
func authenticateReleaseStartupNativeV2Context(ctx context.Context, native *crv4.Chain, initial ReleaseEvidenceV2ActivationContext, journal *releaseMeasurementInputJournal, runtime crv4.RuntimeArtifactIdentity, legacy bool) error {
	return authenticateReleaseStartupNativeV2ContextWithRetainedHistory(ctx, native, initial, journal, runtime, legacy, false)
}

func authenticateReleaseStartupNativeV2ContextWithRetainedHistory(ctx context.Context, native *crv4.Chain, initial ReleaseEvidenceV2ActivationContext, journal *releaseMeasurementInputJournal, runtime crv4.RuntimeArtifactIdentity, legacy, retained bool) error {
	if ctx == nil || journal == nil {
		return errors.New("startup ordinary native context is absent")
	}
	input := journal.MeasurementInput
	hash, err := canonicalAttemptHex32("startup ordinary native hash", input.CutNativeBlockHash, false)
	if err != nil {
		return err
	}
	if !legacy && !releaseBlockAtOrBefore(initial.Activation.NativeBlock, attemptHex32(initial.Activation.NativeHash), input.CutNativeBlock, input.CutNativeBlockHash) {
		return errors.New("startup ordinary native observation precedes authenticated activation")
	}
	if retained {
		return ctx.Err()
	}
	observed, err := crv4.ReadValidatorScheduleAtContext(ctx, native, crv4.ValidatorScheduleQuery{
		GenesisHash: types.Hash(initial.Activation.Domain.GenesisHash), BlockHash: types.Hash(hash), BlockNumber: input.CutNativeBlock,
		Netuid: initial.Activation.Domain.Netuid, Hotkey: initial.Activation.Hotkey, MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs,
	}, runtime)
	if err != nil {
		return err
	}
	if observed.SubnetEpochIndex != journal.SubnetEpoch || !observed.Stake.MeetsNonSelfStakeAndPermit() {
		return errors.New("startup ordinary native epoch or signing eligibility differs from real history")
	}
	return ctx.Err()
}
