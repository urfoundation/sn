//go:build linux || darwin

package validator

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// ErrValidatorEvidencePolicyEraMismatch means these exact signed bytes cannot
// satisfy the coordinator policy for their epoch.
var ErrValidatorEvidencePolicyEraMismatch = errors.New("signed validator evidence belongs to another policy era")

// A signed header cannot be repaired after its policy era changes. Check the
// finalized coordinator authority before admitting a new relay transaction.
func validateValidatorEvidencePolicyEraV2(header protocol.ValidatorEvidenceHeader, policy stabi.STCoordinatorPolicySnapshot) error {
	if policy.EffectiveEpoch > header.Epoch || policy.PolicyHash != header.Domain.PolicyHash {
		return fmt.Errorf("validator evidence epoch %d names policy 0x%x, but finalized policyAt(%d) is %s (effective epoch %d): %w",
			header.Epoch, header.Domain.PolicyHash, header.Epoch, policy.PolicyHash, policy.EffectiveEpoch,
			ErrValidatorEvidencePolicyEraMismatch)
	}
	return nil
}

// ValidateValidatorEvidencePolicyEraV2Context reads the policy at one finalized
// EVM block. A transport failure stays retryable to the caller; a verified
// mismatch is terminal for these exact signed bytes.
func (self *ChainClient) ValidateValidatorEvidencePolicyEraV2Context(ctx context.Context, header protocol.ValidatorEvidenceHeader) error {
	if err := self.requireRelease(); err != nil {
		return err
	}
	block, hash, err := self.FinalizedBlockContext(ctx)
	if err != nil {
		return err
	}
	epoch := new(big.Int).SetUint64(header.Epoch)
	policy, err := chainViewAtHashContext(ctx, self, block, hash, self.coordinator.PackPolicyAt(epoch), self.coordinator.UnpackPolicyAt)
	if err != nil {
		return fmt.Errorf("read validator evidence policyAt(%d) at finalized block %d: %w", header.Epoch, block, err)
	}
	return errors.Join(validateValidatorEvidencePolicyEraV2(header, policy), ctx.Err())
}
