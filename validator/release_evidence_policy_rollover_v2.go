//go:build linux || darwin

package validator

import (
	"context"
	"errors"
	"fmt"

	"github.com/urfoundation/sn/protocol"
)

// DeriveValidatorEvidencePolicyRolloverV2 verifies the complete signed terminal
// closure before binding new activations to its exact per-operator ledger heads.
// The caller still owns public closure history, snapshot finality, current
// policy authority, signatures, publication and the process handoff.
func DeriveValidatorEvidencePolicyRolloverV2(ctx context.Context, closure *AttemptSettlementClosureV2, options AttemptSettlementV2Options, domain protocol.ValidatorEvidenceActivationDomain, nativeBlock uint64, nativeHash [32]byte, evmBlock uint64, evmHash [32]byte) ([]protocol.ValidatorEvidenceActivation, error) {
	if closure == nil || closure.Epoch == ^uint64(0) || domain.Epoch != closure.Epoch+1 || domain.PolicyHash == ([32]byte{}) || nativeBlock == 0 || nativeHash == ([32]byte{}) || evmBlock == 0 || evmHash == ([32]byte{}) {
		return nil, errors.New("policy rollover has no adjacent signed terminal and complete snapshots")
	}
	if _, err := VerifyAttemptSettlementClosureV2(ctx, closure, options); err != nil {
		return nil, fmt.Errorf("verify policy rollover terminal: %w", err)
	}
	result := make([]protocol.ValidatorEvidenceActivation, 0, len(closure.Transitions))
	for _, transition := range closure.Transitions {
		cut := transition.Cut
		prior := cut.Context.Activation.Domain
		if transition.ToEpoch != domain.Epoch || cut.Context.Boundary.SettlementEpoch != closure.Epoch || cut.Context.Boundary.EVMBlock > evmBlock ||
			prior.ChainID != domain.ChainID || prior.GenesisHash != domain.GenesisHash || prior.Netuid != domain.Netuid || prior.Coordinator != domain.Coordinator || prior.SettlementVault != domain.SettlementVault || prior.DeploymentIDHash != domain.DeploymentIDHash || prior.PolicyHash == domain.PolicyHash || cut.LastSequence == ^uint64(0) {
			return nil, errors.New("policy rollover changes the terminal deployment, keeps the old policy, or lacks an adjacent prefix")
		}
		vpk, err := canonicalAttemptHex32("policy rollover VPK", transition.Identity.ValidatorVPK, false)
		if err != nil {
			return nil, err
		}
		root, err := canonicalAttemptHex32("policy rollover prior root", cut.Root, true)
		if err != nil {
			return nil, err
		}
		activation := protocol.ValidatorEvidenceActivation{Domain: domain, Hotkey: cut.Context.Activation.Hotkey, NoID: transition.Identity.NoID, VPK: vpk,
			FirstSequence: cut.LastSequence + 1, PriorRoot: root, NativeBlock: nativeBlock, NativeHash: nativeHash, EVMBlock: evmBlock, EVMHash: evmHash}
		if err := activation.Validate(); err != nil {
			return nil, fmt.Errorf("policy rollover operator %d: %w", transition.Identity.NoID, err)
		}
		result = append(result, activation)
	}
	return result, ctx.Err()
}
