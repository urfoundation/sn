//go:build linux || darwin

package validator

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// ErrValidatorEvidencePolicyEraMismatch means these exact signed bytes cannot
// satisfy the coordinator policy for their epoch.
var ErrValidatorEvidencePolicyEraMismatch = errors.New("signed validator evidence belongs to another policy era")

// ValidatorEvidencePolicyEraMismatchV2 retains the exact finalized authority
// behind a refusal. It is evidence of a gap, never a publication receipt.
type ValidatorEvidencePolicyEraMismatchV2 struct {
	Header         protocol.ValidatorEvidenceHeader  `json:"header"`
	FinalizedBlock uint64                            `json:"finalized_block"`
	FinalizedHash  [32]byte                          `json:"finalized_hash"`
	Policy         stabi.STCoordinatorPolicySnapshot `json:"policy"`
}

func (self *ValidatorEvidencePolicyEraMismatchV2) Error() string {
	return fmt.Sprintf("validator evidence epoch %d names policy 0x%x, but policyAt(%d) at finalized block %d (0x%x) is 0x%x (effective epoch %d): %v",
		self.Header.Epoch, self.Header.Domain.PolicyHash, self.Header.Epoch, self.FinalizedBlock, self.FinalizedHash, self.Policy.PolicyHash, self.Policy.EffectiveEpoch, ErrValidatorEvidencePolicyEraMismatch)
}

func (self *ValidatorEvidencePolicyEraMismatchV2) Unwrap() error {
	return ErrValidatorEvidencePolicyEraMismatch
}

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
	return self.validateValidatorEvidencePolicyEraAtHashV2(ctx, header, block, hash)
}

func (self *ChainClient) validateValidatorEvidencePolicyEraAtHashV2(ctx context.Context, header protocol.ValidatorEvidenceHeader, block uint64, hash [32]byte) error {
	policy, err := chainViewAtHashContext(ctx, self, block, hash, self.coordinator.PackPolicyAt(new(big.Int).SetUint64(header.Epoch)), self.coordinator.UnpackPolicyAt)
	if err != nil {
		return fmt.Errorf("read validator evidence policyAt(%d) at finalized block %d: %w", header.Epoch, block, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if validateValidatorEvidencePolicyEraV2(header, policy) != nil {
		return &ValidatorEvidencePolicyEraMismatchV2{Header: header, FinalizedBlock: block, FinalizedHash: hash, Policy: policy}
	}
	return nil
}

// AuthenticateValidatorEvidencePolicyEraMismatchV2Context replays a retained
// refusal at its original canonical finalized block. Private record integrity
// does not replace either chain finality or an exact policyAt read.
func (self *ChainClient) AuthenticateValidatorEvidencePolicyEraMismatchV2Context(ctx context.Context, retained *ValidatorEvidencePolicyEraMismatchV2) error {
	if err := self.requireRelease(); err != nil {
		return err
	}
	if retained == nil || retained.FinalizedBlock == 0 || retained.FinalizedHash == ([32]byte{}) || validateValidatorEvidencePolicyEraV2(retained.Header, retained.Policy) == nil {
		return errors.New("retained validator evidence policy mismatch is incomplete")
	}
	block, _, err := self.FinalizedBlockContext(ctx)
	if err != nil {
		return err
	}
	if block < retained.FinalizedBlock {
		return errors.New("retained validator evidence policy mismatch is not finalized")
	}
	hash, err := self.BlockHashContext(ctx, retained.FinalizedBlock)
	if err != nil {
		return err
	}
	if hash != retained.FinalizedHash {
		return errors.New("retained validator evidence policy mismatch block is not canonical")
	}
	err = self.validateValidatorEvidencePolicyEraAtHashV2(ctx, retained.Header, retained.FinalizedBlock, retained.FinalizedHash)
	var actual *ValidatorEvidencePolicyEraMismatchV2
	if !errors.As(err, &actual) || !reflect.DeepEqual(actual, retained) {
		return errors.Join(errors.New("retained validator evidence policy mismatch differs from its finalized authority"), err)
	}
	return ctx.Err()
}
