// Original consensus witnesses accompany a completed producer result without
// changing its historical amount/completion digest. The consumer verifies the
// certificate bytes against the independently approved initial authority.
package main

import (
	"context"
	"errors"

	"github.com/urnetwork/server/v2026/strecovery"
)

// One certificate window may authenticate several consecutive executions.
// Its anchor belongs to the certified tip of the preceding window, never an
// earlier selected economic child. The original proof is retained unchanged.
type nativeExecutionFinalityWindow struct {
	Anchor strecovery.NativeFinalityCheckpoint     `json:"anchor"`
	Proof  strecovery.NativeExecutionFinalityProof `json:"original_proof"`
}

// The selected result binds a reusable window to this actual replay outcome.
// It is a companion, not a caller-supplied assertion that finality succeeded.
type nativeExecutionFinalityProjection struct {
	Approval    []byte                        `json:"original_signed_approval"`
	Window      nativeExecutionFinalityWindow `json:"window"`
	Parent      economicEmissionBoundary      `json:"parent"`
	Child       economicEmissionBoundary      `json:"child"`
	OutcomeHash string                        `json:"original_outcome_hash"`
	ProofHash   string                        `json:"selected_proof_hash"`
}

// Read the exact immutable publication already verified by the producer. This
// runs before completion publication, so a failed read leaves the same pending
// job and original economic cursor available to the next bounded attempt.
func (self *nativeProducerSession) finalityProjection(ctx context.Context, reference planFileReference, outcome nativeExecutionOutcome, selected *strecovery.NativeExecutionFinality) (*nativeExecutionFinalityProjection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if self.finalityApproval == nil {
		raw, err := nativeProducerReadApprovalFor(ctx, self.originalPolicy.Execution.Producer.Authority, self.originalPolicy.Execution.FeeCensus)
		if err != nil {
			return nil, err
		}
		if monitorReadDigest(raw) != self.originalPolicy.Execution.Producer.Authority.Sha256 {
			return nil, errors.Join(errRpcIntegrity, errors.New("native original finality approval changed after admission"))
		}
		self.finalityApproval = raw
	}
	raw, err := self.files.readReference(reference, strecovery.MaximumReceiptFinalityBytes)
	if err != nil {
		return nil, err
	}
	var proof strecovery.NativeExecutionFinalityProof
	if err := decodePlanJson(raw, &proof); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	if selected == nil || selected.CheckpointHash != self.state.Anchor.Hash() || selected.ProofHash != outcome.FinalityProofHash || selected.Child.Hash != outcome.Boundary.Hash || selected.Child.Number != outcome.Boundary.Number {
		return nil, errors.Join(errRpcIntegrity, errors.New("native finality companion differs from completed original execution"))
	}
	selectedProof := proof
	selectedProof.Parent, selectedProof.Child = selected.Parent, selected.Child
	if rootObjectHash(selectedProof) != selected.ProofHash {
		return nil, errors.Join(errRpcIntegrity, errors.New("native finality companion changed original selected proof bytes"))
	}
	if err := errors.Join(ctx.Err(), self.files.check()); err != nil {
		return nil, err
	}
	return &nativeExecutionFinalityProjection{Approval: append([]byte(nil), self.finalityApproval...), Window: nativeExecutionFinalityWindow{Anchor: self.state.Anchor, Proof: proof}, Parent: economicEmissionBoundary{Number: selected.Parent.Number, Hash: selected.Parent.Hash}, Child: outcome.Boundary, OutcomeHash: outcome.ContentHash, ProofHash: selected.ProofHash}, ctx.Err()
}
