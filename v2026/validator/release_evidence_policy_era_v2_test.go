//go:build linux || darwin

package validator

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

func TestValidatorEvidencePolicyEraV2RequiresFinalizedEpochPolicy(t *testing.T) {
	oldHash := common.HexToHash("0x1526b242cf4908cc31f7e58006664bce6064003c69fd8452eab2d49122fef277")
	newHash := common.HexToHash("0x41f0c7efe7e1b23b2fd22dac9352ca18be48d2e4d1fb5b41ce89660bc899b0dd")
	header := protocol.ValidatorEvidenceHeader{Epoch: 594}
	header.Domain.PolicyHash = [32]byte(oldHash)
	policy := stabi.STCoordinatorPolicySnapshot{PolicyHash: newHash, EffectiveEpoch: 594}
	err := validateValidatorEvidencePolicyEraV2(header, policy)
	if err == nil || !strings.Contains(err.Error(), "signed validator evidence belongs to another policy era") || !strings.Contains(err.Error(), "594") {
		t.Fatalf("stale signed evidence was admitted across the policy boundary: %v", err)
	}
	policy.PolicyHash = oldHash
	policy.EffectiveEpoch = 593
	if err := validateValidatorEvidencePolicyEraV2(header, policy); err != nil {
		t.Fatalf("matching historical policy was rejected: %v", err)
	}
	policy.EffectiveEpoch = 595
	if err := validateValidatorEvidencePolicyEraV2(header, policy); err == nil {
		t.Fatal("a future policy was admitted for an earlier evidence epoch")
	}
}
