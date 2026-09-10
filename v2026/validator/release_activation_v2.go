// Authenticate signed activation publication and both historical chain views.
// The independently supplied prefix still needs complete migration/EMA replay;
// this read-only result alone cannot activate a producer or authorize a cut.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Caller-owned authority comes from the approved deployment, signing identity,
// authenticated retained prefix and selected historical snapshots. Never fill
// Expected by copying an unverified candidate at a file or API boundary.
type ReleaseActivationV2Authority struct {
	Expected      protocol.ValidatorEvidenceActivation
	Journal       common.Address
	RuntimeHash   [32]byte
	ValidatorUID  uint16
	NativeRuntime crv4.RuntimeArtifactIdentity
}

// Value-only observations retain their exact, distinct clocks. Publication is
// not proof of complete migration, prior EMA, retained IDs or public replicas.
type VerifiedReleaseActivationV2 struct {
	Publication      ValidatorEvidenceActivationPublication
	Native           crv4.ValidatorStakeObservation
	ObservedEVMBlock uint64
	ObservedEVMHash  [32]byte
}

// Uses real ChainClient and CRV4 readers, without eligibility or inclusion
// callbacks. Independent native/EVM observations overlap; any failure cancels
// and joins the sibling, and no partial result escapes cancellation or error.
func (self *ChainClient) AuthenticateReleaseActivationV2Context(ctx context.Context, native *crv4.Chain, authority ReleaseActivationV2Authority, candidate protocol.ValidatorEvidenceActivation, vpkSignature, hotkeySignature []byte, block uint64, blockHash [32]byte) (result VerifiedReleaseActivationV2, resultErr error) {
	if ctx == nil {
		return result, errors.New("activation authentication context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = VerifiedReleaseActivationV2{}
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if self == nil || self.client == nil || self.chainId == nil || self.coordinator == nil ||
		native == nil || native.API == nil || native.API.Client == nil {
		return result, errors.New("activation authentication chain owners are unavailable")
	}
	if err := self.requireRelease(); err != nil {
		return result, err
	}
	expected := authority.Expected
	if err := candidate.Verify(expected, vpkSignature, hotkeySignature); err != nil {
		return result, err
	}
	if self.contractAddr != common.Address(expected.Domain.Coordinator) ||
		self.chainId.Cmp(new(big.Int).SetUint64(expected.Domain.ChainID)) != 0 ||
		native.GenesisHash != types.Hash(expected.Domain.GenesisHash) ||
		authority.Journal == (common.Address{}) || authority.Journal == self.contractAddr ||
		authority.Journal == common.Address(expected.Domain.SettlementVault) || authority.RuntimeHash == ([32]byte{}) ||
		uint32(authority.ValidatorUID) >= releaseNativeValidatorMaximumUIDs ||
		block <= expected.EVMBlock || blockHash == ([32]byte{}) {
		return result, errors.New("activation authentication domain, observer or native UID differs")
	}
	// Both readers already impose their per-call bounds. Clamp the complete
	// historical observation to the existing native startup window as well.
	operationCtx, cancel := context.WithTimeout(ctx, releaseNativeEndpointTimeout(nil))
	defer cancel()
	var joined sync.WaitGroup
	var evmErr, nativeErr error
	var publication ValidatorEvidenceActivationPublication
	var observation crv4.ValidatorStakeObservation
	joined.Add(2)
	go func() {
		defer joined.Done()
		publication, evmErr = self.readReleaseActivationV2EVMContext(operationCtx, authority, block, blockHash)
		if evmErr != nil {
			cancel()
		}
	}()
	go func() {
		defer joined.Done()
		observation, nativeErr = crv4.ReadValidatorStakeAtContext(operationCtx, native, crv4.ValidatorIdentityQuery{
			GenesisHash: types.Hash(expected.Domain.GenesisHash), BlockHash: types.Hash(expected.NativeHash),
			BlockNumber: expected.NativeBlock, Netuid: expected.Domain.Netuid,
			UID: authority.ValidatorUID, MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs,
		}, authority.NativeRuntime)
		if nativeErr == nil && (observation.Identity.Hotkey != expected.Hotkey || !observation.MeetsNonSelfStakeAndPermit()) {
			nativeErr = errors.New("activation historical hotkey lacks the exact native stake/permit authority")
		}
		if nativeErr != nil {
			cancel()
		}
	}()
	joined.Wait()
	if err := errors.Join(evmErr, nativeErr, operationCtx.Err(), ctx.Err()); err != nil {
		return result, err
	}
	return VerifiedReleaseActivationV2{
		Publication: publication, Native: observation, ObservedEVMBlock: block, ObservedEVMHash: blockHash,
	}, nil
}

// Exact tuples prevent high padding or trailing bytes from becoming accepted
// historical policy/operator values through permissive generated unpackers.
func canonicalReleaseActivationV2View(method string, encoded []byte, value any) error {
	contractABI, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return err
	}
	view, exists := contractABI.Methods[method]
	if !exists {
		return errors.New("activation coordinator view is unknown")
	}
	canonical, err := view.Outputs.Pack(value)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, encoded) {
		return fmt.Errorf("activation %s view is noncanonical", method)
	}
	return nil
}

// Each EVM view is EIP-1898 canonical-hash pinned. Future-epoch activations are
// allowed by the contract; the earlier snapshot must not be after that epoch.
// No native height is compared numerically with either EVM height.
func (self *ChainClient) readReleaseActivationV2EVMContext(ctx context.Context, authority ReleaseActivationV2Authority, block uint64, blockHash [32]byte) (ValidatorEvidenceActivationPublication, error) {
	zero := ValidatorEvidenceActivationPublication{}
	expected := authority.Expected
	finalizedBlock, finalizedHash, err := self.FinalizedBlockContext(ctx)
	if err != nil {
		return zero, err
	}
	if finalizedBlock < block || finalizedBlock == block && finalizedHash != blockHash {
		return zero, errors.New("activation observation is not finalized at its captured EVM hash")
	}
	epoch := new(big.Int).SetUint64(expected.Domain.Epoch)
	outputs, err := self.batchCallsAtHashContext(ctx, expected.EVMBlock, expected.EVMHash, []chainBatchCall{
		{address: self.contractAddr, calldata: self.coordinator.PackCurrentEpoch()},
		{address: self.contractAddr, calldata: self.coordinator.PackPolicyAt(epoch)},
		{address: self.contractAddr, calldata: self.coordinator.PackOperatorAt(new(big.Int).SetUint64(expected.NoID), epoch)},
	})
	if err != nil {
		return zero, err
	}
	currentEpoch, err := self.coordinator.UnpackCurrentEpoch(outputs[0])
	if err != nil {
		return zero, err
	}
	if currentEpoch == nil || !currentEpoch.IsUint64() || currentEpoch.Uint64() > expected.Domain.Epoch {
		return zero, errors.New("activation historical EVM snapshot is after its activation epoch")
	}
	if err := canonicalReleaseActivationV2View("currentEpoch", outputs[0], currentEpoch); err != nil {
		return zero, err
	}
	policy, err := self.coordinator.UnpackPolicyAt(outputs[1])
	if err != nil {
		return zero, err
	}
	if err := canonicalReleaseActivationV2View("policyAt", outputs[1], policy); err != nil {
		return zero, err
	}
	if policy.PolicyHash != expected.Domain.PolicyHash || policy.EffectiveEpoch > expected.Domain.Epoch {
		return zero, errors.New("activation historical policy differs from approved authority")
	}
	operator, err := self.coordinator.UnpackOperatorAt(outputs[2])
	if err != nil {
		return zero, err
	}
	if err := canonicalReleaseActivationV2View("operatorAt", outputs[2], operator); err != nil {
		return zero, err
	}
	if !operator.Active || operator.EffectiveEpoch > expected.Domain.Epoch {
		return zero, errors.New("activation operator was not active for its approved epoch")
	}
	publication, err := self.ValidatorEvidenceActivationAtHashContext(ctx, authority.Journal, authority.RuntimeHash, expected, block, blockHash)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return publication, nil
}
