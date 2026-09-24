//go:build linux || darwin

package validator

import (
	"strings"
	"testing"

	"github.com/urfoundation/sn/protocol"
)

func TestValidatorEvidencePolicyRolloverV2BindsVerifiedTerminalPrefix(t *testing.T) {
	first := newAttemptSettlementV2TestOperator(t, 9, 1, 0)
	second := newAttemptSettlementV2TestOperator(t, 10, 0, 0)
	closure := sealAttemptSettlementV2Test(t, first, second)
	old := closure.Transitions[0].Cut.Context.Activation.Domain
	domain := protocol.ValidatorEvidenceActivationDomain{ChainID: old.ChainID, GenesisHash: old.GenesisHash, Netuid: old.Netuid, Coordinator: old.Coordinator,
		SettlementVault: old.SettlementVault, DeploymentIDHash: old.DeploymentIDHash, PolicyHash: old.PolicyHash, Epoch: closure.Epoch + 1}
	domain.PolicyHash[0] ^= 0xff
	options := attemptSettlementV2TestOptions(t, first, second)
	evmBlock := closure.Transitions[0].FromBoundary.EVMBlock + 1
	activations, err := DeriveValidatorEvidencePolicyRolloverV2(t.Context(), closure, options, domain, 100, [32]byte{0xa1}, evmBlock, [32]byte{0xb1})
	if err != nil || len(activations) != 2 {
		t.Fatalf("derive from complete signed closure: %v", err)
	}
	for index, transition := range closure.Transitions {
		activation := activations[index]
		if activation.NoID != transition.Identity.NoID || activation.FirstSequence != transition.Cut.LastSequence+1 ||
			activation.PriorRoot != mustPolicyRolloverTestRoot(t, transition.Cut.Root) || activation.Domain != domain || activation.Hotkey != transition.Cut.Context.Activation.Hotkey {
			t.Fatalf("operator %d lost signed prefix", transition.Identity.NoID)
		}
	}
	closure.Transitions[0].Signature[0] ^= 1
	if _, err := DeriveValidatorEvidencePolicyRolloverV2(t.Context(), closure, options, domain, 100, [32]byte{0xa1}, evmBlock, [32]byte{0xb1}); err == nil {
		t.Fatal("modified terminal signature supplied an activation prefix")
	}
}

func TestValidatorEvidencePolicyRolloverV2RejectsNonAdjacentOrUnchangedPolicy(t *testing.T) {
	fixture := newAttemptSettlementV2TestOperator(t, 9, 0, 0)
	closure := sealAttemptSettlementV2Test(t, fixture)
	old := closure.Transitions[0].Cut.Context.Activation.Domain
	domain := protocol.ValidatorEvidenceActivationDomain{ChainID: old.ChainID, GenesisHash: old.GenesisHash, Netuid: old.Netuid, Coordinator: old.Coordinator,
		SettlementVault: old.SettlementVault, DeploymentIDHash: old.DeploymentIDHash, PolicyHash: old.PolicyHash, Epoch: closure.Epoch + 1}
	options := attemptSettlementV2TestOptions(t, fixture)
	block := closure.Transitions[0].FromBoundary.EVMBlock + 1
	if _, err := DeriveValidatorEvidencePolicyRolloverV2(t.Context(), closure, options, domain, 100, [32]byte{0xa1}, block, [32]byte{0xb1}); err == nil || !strings.Contains(err.Error(), "keeps the old policy") {
		t.Fatalf("unchanged policy was accepted as rollover: %v", err)
	}
	domain.PolicyHash[0] ^= 0xff
	domain.Epoch++
	if _, err := DeriveValidatorEvidencePolicyRolloverV2(t.Context(), closure, options, domain, 100, [32]byte{0xa1}, block, [32]byte{0xb1}); err == nil {
		t.Fatal("nonadjacent activation skipped the signed terminal successor")
	}
}

func mustPolicyRolloverTestRoot(t *testing.T, value string) [32]byte {
	t.Helper()
	root, err := canonicalAttemptHex32("test rollover root", value, true)
	if err != nil {
		t.Fatal(err)
	}
	return root
}
