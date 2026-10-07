// A signed current-state proposal remains a proposal, while actual pending
// mutations demonstrate why its final recheck cannot replace complete history.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/crypto"
)

// The proposal has a separate domain/evidence file under the original approver;
// neither its signature nor its field names claim the existing history policy.
func bootstrapSuccessorSafeCurrentTestProposal(t *testing.T, model *bootstrapSuccessorExecutionFixture, base bootstrapSuccessorCanonicalApproval) bootstrapSuccessorSafeCurrentPolicyApproval {
	t.Helper()
	plan := model.approval.Plan
	provenance, err := base.readProvenance(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	evidence := bootstrapRootTestWrite(t, filepath.Join(bootstrapSuccessorExecutionTestDirectory(t), "synthetic-current-policy-review.json"), map[string]string{"scope": "synthetic review of current-only policy and pending limits"})
	p := provenance.Provenance
	approval := bootstrapSuccessorSafeCurrentPolicyApproval{Authorization: bootstrapSuccessorSafeCurrentPolicyAuthorization{
		Schema: bootstrapSuccessorSafeCurrentPolicySchema, ExecutionPlanHash: plan.hash(), CanonicalAuthorityHash: rootObjectHash(base),
		Safe: p.Safe, Singleton: p.Singleton, Version: p.Version, Variant: p.Variant, SafeProxyRuntimeHash: p.SafeProxyRuntimeHash, SingletonRuntimeHash: p.SingletonRuntimeHash,
		Runtime: base.Authorization.CurrentRuntime, ReviewEvidence: evidence, Policy: bootstrapSuccessorSafeCurrentPolicy}}
	message, err := approval.Authorization.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	approval.Signature = hex.EncodeToString(ed25519.Sign(model.key, message))
	return approval
}

// Signature, predecessor, exact execution, runtime and distinct evidence remain
// mandatory even though successful validation only authorizes observation scope.
func TestBootstrapSuccessorSafeCurrentPolicyRequiresDistinctAuthority(t *testing.T) {
	model := newBootstrapSuccessorExecutionFixture(t)
	plan := model.approval.Plan
	base := bootstrapSuccessorCanonicalTestApproval(t, plan, model.key, bootstrapSuccessorCanonicalTestRuntime())
	proposal := bootstrapSuccessorSafeCurrentTestProposal(t, model, base)
	before := rootObjectHash([]any{plan, base})
	scope, err := proposal.validate(t.Context(), plan, base, bootstrapSuccessorRuntimeHistory{})
	if err != nil || scope.Safe != plan.Review.Transaction.Safe || scope.Nonce != plan.Review.Transaction.Nonce {
		t.Fatal("separate signed current policy proposal failed", err)
	}
	for _, changed := range []string{"foreign signature", "old domain", "plan", "predecessor", "runtime tip", "runtime artifact", "Safe", "singleton", "release", "history policy", "reused evidence"} {
		value := proposal
		switch changed {
		case "foreign signature":
			value.Signature = hex.EncodeToString(make([]byte, ed25519.SignatureSize))
		case "old domain":
			value.Signature = base.Signature
		case "plan":
			value.Authorization.ExecutionPlanHash = rootObjectHash("synthetic changed execution")
		case "predecessor":
			value.Authorization.CanonicalAuthorityHash = rootObjectHash("synthetic changed authority")
		case "runtime tip":
			value.Authorization.RuntimeRevisionHash = rootObjectHash("synthetic unseen runtime tip")
		case "runtime artifact":
			value.Authorization.Runtime.RuntimeCodeHash = common.Hash{31: 77}.Hex()
		case "Safe":
			value.Authorization.Safe = common.Address{19: 77}
		case "singleton":
			value.Authorization.Singleton = common.Address{19: 78}
		case "release":
			value.Authorization.Version = "1.5.0"
		case "history policy":
			value.Authorization.Policy = bootstrapSuccessorSafeProvenancePolicy
		case "reused evidence":
			value.Authorization.ReviewEvidence = base.Authorization.CutoverEvidence
		}
		if changed != "foreign signature" && changed != "old domain" && changed != "history policy" {
			message, err := value.Authorization.signingBytes()
			if err != nil {
				t.Fatal(err)
			}
			value.Signature = hex.EncodeToString(ed25519.Sign(model.key, message))
		}
		if _, err := value.validate(t.Context(), plan, base, bootstrapSuccessorRuntimeHistory{}); err == nil {
			t.Fatal("current Safe policy accepted changed independent authority", changed)
		}
	}
	if _, err := proposal.validate(t.Context(), plan, base, bootstrapSuccessorRuntimeHistory{pendingHash: rootObjectHash("synthetic pending revision")}); err == nil {
		t.Fatal("current Safe policy accepted partial runtime authority")
	}
	if rootObjectHash([]any{plan, base}) != before {
		t.Fatal("current Safe proposal changed immutable original authority")
	}
	if err := os.WriteFile(proposal.Authorization.ReviewEvidence.Path, []byte("synthetic swapped review"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := proposal.validate(t.Context(), plan, base, bootstrapSuccessorRuntimeHistory{}); err == nil {
		t.Fatal("current Safe policy accepted swapped independently pinned evidence")
	}
}

// Proof completion precedes pending code/slot rechecks. A changed known word
// refuses; an unknown pending orphan remains an explicitly unproved condition.
func TestSafeCurrentPendingRecheckCannotClaimCompleteAuthority(t *testing.T) {
	adapter, model, chain := bootstrapSuccessorCanonicalSafeFixture(t)
	plan := model.approval.Plan
	f := safeCurrentProofFixtureFromOracle(t, model.oracle, plan.Review.Request.Version, plan.Review.Request.Variant)
	witness := safeCurrentTestWitness(t, f.entries)
	finalized, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness)
	if err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		result, err := adapter.readSafeCurrentPending(t.Context(), f.scope, finalized)
		if err != nil || result == nil || !result.ScopedWordsMatched || result.CompletePendingVerified || result.SendAuthorized || result.FinalizedObservationHash != rootObjectHash(finalized) {
			t.Fatal("pending scope failed or overclaimed complete authority", err)
		}
	}
	check()
	for _, change := range []string{"nonce", "guard", "proxy code", "singleton code"} {
		chain.stateLock.Lock()
		slot := common.BigToHash(big.NewInt(5))
		if change == "guard" {
			slot = common.HexToHash("0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8")
		}
		previous := chain.state.GetState(f.scope.Safe, slot)
		address := f.scope.Safe
		if change == "singleton code" {
			address = f.scope.Singleton
		}
		code := chain.state.GetCode(address)
		if change == "proxy code" || change == "singleton code" {
			chain.state.SetCode(address, []byte{0}, tracing.CodeChangeUnspecified)
		} else {
			chain.state.SetState(f.scope.Safe, slot, common.Hash{31: 88})
		}
		chain.stateLock.Unlock()
		if result, err := adapter.readSafeCurrentPending(t.Context(), f.scope, finalized); err == nil || result != nil {
			t.Fatal("final pending recheck accepted a changed scoped authority", change)
		}
		chain.stateLock.Lock()
		chain.state.SetCode(address, code, tracing.CodeChangeUnspecified)
		chain.state.SetState(f.scope.Safe, slot, previous)
		chain.stateLock.Unlock()
	}
	orphan := common.Address{19: 231}
	slot := crypto.Keccak256Hash(common.LeftPadBytes(orphan[:], 32), common.LeftPadBytes([]byte{1}, 32))
	chain.stateLock.Lock()
	chain.state.SetState(f.scope.Safe, slot, common.Hash{31: 1})
	chain.stateLock.Unlock()
	check()
	if err := adapter.submit(t.Context(), plan, common.FromHex(plan.SignedRelayer)); !errors.Is(err, errBootstrapSuccessorSafeProvenanceUnavailable) {
		t.Fatal("current-only proof or pending checks enabled public submission", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := adapter.readSafeCurrentPending(ctx, f.scope, finalized); !errors.Is(err, context.Canceled) || result != nil {
		t.Fatal("canceled scoped pending recheck produced authority", err)
	}
	chain.stateLock.Lock()
	defer chain.stateLock.Unlock()
	if len(chain.writes) != 0 {
		t.Fatal("current Safe proof work submitted a transaction")
	}
}
