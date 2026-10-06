//go:build linux || darwin

// Synthetic proposal inputs retain the exact independently reviewed runtime and
// public policy. Proposal validation does not grant production send authority.
package validator

import (
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// The treasury proposal keeps its allocation and cap on a reviewed source.
func TestTreasuryRuntimeSuccessorProposalPreservesIndependentPolicy(t *testing.T) {
	input := recycleTestInput(t)
	policy := treasuryPolicyTestValue(t)
	proposal := input.Proposal
	proposal.Schema, proposal.Treasury = TreasuryProposalSchema, &policy
	proposal.Remainder, proposal.OwnerAllocation = "ordinary_treasury_credit", "equal_exact_registered"
	proposal.Runtime.SourceCommit = crv4.NativeOwnerSource473
	proposal.Runtime.Version.SpecVersion = 473
	if err := proposal.Validate(input.ParentPolicy); err != nil {
		t.Fatal("reviewed successor proposal refused", err)
	}
	current, err := proposal.Hash(input.ParentPolicy)
	if err != nil {
		t.Fatal(err)
	}
	historical := proposal
	historical.Runtime.SourceCommit = crv4.NativeOwnerSource470
	historical.Runtime.Version.SpecVersion = 470
	previous, err := historical.Hash(input.ParentPolicy)
	if err != nil || previous == current {
		t.Fatal("successor borrowed historical policy identity", err)
	}
	for _, fault := range []string{"source", "code", "metadata", "genesis", "share", "allocation"} {
		changed := proposal
		switch fault {
		case "source":
			changed.Runtime.SourceCommit = ownerRecycleSourceCommit
		case "code":
			changed.Runtime.CodeHash = [32]byte{}
		case "metadata":
			changed.Runtime.MetadataHash = [32]byte{}
		case "genesis":
			changed.Runtime.GenesisHash = [32]byte{}
		case "share":
			changed.ProviderShare.Numerator++
		case "allocation":
			changed.OwnerAllocation = "equal_unmasked_registered"
		}
		if err := changed.Validate(input.ParentPolicy); err == nil {
			t.Fatal("successor runtime bypassed original policy requirement", fault)
		}
	}
}

// The legacy owner preview can use the reviewed call source without acquiring
// signing, durable intent or treasury authority from the newer spec number.
func TestOwnerRecycleRuntimeSuccessorPreviewRemainsUnapproved(t *testing.T) {
	input := recycleTestInput(t)
	input.Proposal.Runtime.SourceCommit = crv4.NativeOwnerSource473
	input.Proposal.Runtime.Version.SpecVersion = 473
	input.Snapshot.Runtime = input.Proposal.Runtime
	preview, err := PreviewOwnerRecycle(input)
	if err != nil || preview == nil || preview.AdmissionError() == nil {
		t.Fatal("successor preview lost its review-only boundary", err)
	}
	input.Proposal.Runtime.SourceCommit = "synthetic-unreviewed-source"
	input.Snapshot.Runtime = input.Proposal.Runtime
	if preview, err := PreviewOwnerRecycle(input); err == nil || preview != nil {
		t.Fatal("spec number alone admitted an unreviewed owner source", err)
	}
}
