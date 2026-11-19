// Synthetic independent signatures distinguish policy acceptance from a review
// proposal. All artifacts and evidence stay private to one deterministic fixture.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Literal signing also covers intentionally malformed payloads, so structural
// negative cases cannot accidentally pass only because a signature went stale.
func bootstrapSuccessorSafeCurrentTestSign(t *testing.T, authorization bootstrapSuccessorSafeCurrentRevisionAuthorization, key ed25519.PrivateKey) bootstrapSuccessorSafeCurrentRevisionApproval {
	t.Helper()
	raw, err := json.Marshal(authorization)
	if err != nil {
		t.Fatal(err)
	}
	return bootstrapSuccessorSafeCurrentRevisionApproval{Authorization: authorization,
		Signature: hex.EncodeToString(ed25519.Sign(key, append([]byte(bootstrapSuccessorSafeCurrentRevisionDomain+"\x00"), raw...)))}
}

// Every revision has separate signed proposal evidence and binds the complete
// runtime prefix present when its original independent approver reviewed it.
func bootstrapSuccessorSafeCurrentTestNext(t *testing.T, f *bootstrapSuccessorExecutionFixture, base bootstrapSuccessorCanonicalApproval, runtime bootstrapSuccessorRuntimeHistory, previous []bootstrapSuccessorSafeCurrentRevisionApproval) bootstrapSuccessorSafeCurrentRevisionApproval {
	t.Helper()
	proposal := bootstrapSuccessorSafeCurrentTestProposal(t, f, base)
	proposal.Authorization.RuntimeRevisionHash = runtime.hash()
	if len(runtime.approvals) != 0 {
		proposal.Authorization.Runtime = runtime.approvals[len(runtime.approvals)-1].Authorization.Runtime
	}
	message, err := proposal.Authorization.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	proposal.Signature = hex.EncodeToString(ed25519.Sign(f.key, message))
	predecessor := rootObjectHash(base)
	if len(previous) != 0 {
		predecessor = rootObjectHash(previous[len(previous)-1])
	}
	return bootstrapSuccessorSafeCurrentTestSign(t, bootstrapSuccessorSafeCurrentRevisionAuthorization{
		Schema: bootstrapSuccessorSafeCurrentRevisionSchema, Sequence: uint16(len(previous) + 1), PreviousHash: predecessor,
		Proposal: proposal, Policy: bootstrapSuccessorSafeCurrentRevisionPolicy}, f.key)
}

// Neither an existing review signature nor a foreign acceptance can grant this
// policy. Scope, evidence, runtime prefix and predecessor remain independently bound.
func TestBootstrapSuccessorSafeCurrentRevisionIndependentAcceptance(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	runtime := bootstrapSuccessorRuntimeHistory{}
	approval := bootstrapSuccessorSafeCurrentTestNext(t, f, base, runtime, nil)
	if _, err := approval.validate(t.Context(), f.approval.Plan, base, runtime); err != nil {
		t.Fatal("independent current-policy acceptance failed", err)
	}
	if err := approval.extends(base, runtime, nil); err != nil {
		t.Fatal(err)
	}
	for _, signature := range []string{approval.Authorization.Proposal.Signature, base.Signature, strings.Repeat("00", 64)} {
		changed := approval
		changed.Signature = signature
		if _, err := changed.validate(t.Context(), f.approval.Plan, base, runtime); err == nil {
			t.Fatal("current-policy revision accepted another signing domain or key")
		}
	}
	for _, fault := range []string{"inner signature", "execution", "canonical base", "runtime prefix", "accepted policy", "review policy", "sequence", "predecessor"} {
		changed := approval.Authorization
		switch fault {
		case "inner signature":
			changed.Proposal.Signature = base.Signature
		case "execution":
			changed.Proposal.Authorization.ExecutionPlanHash = rootObjectHash("synthetic other execution")
		case "canonical base":
			changed.Proposal.Authorization.CanonicalAuthorityHash = rootObjectHash("synthetic other base")
		case "runtime prefix":
			changed.Proposal.Authorization.RuntimeRevisionHash = rootObjectHash("synthetic missing runtime")
		case "accepted policy":
			changed.Policy = bootstrapSuccessorSafeCurrentPolicy
		case "review policy":
			changed.Proposal.Authorization.Policy = bootstrapSuccessorSafeProvenancePolicy
		case "sequence":
			changed.Sequence++
		case "predecessor":
			changed.PreviousHash = rootObjectHash("synthetic other predecessor")
		}
		if fault == "execution" || fault == "canonical base" || fault == "runtime prefix" {
			message, err := changed.Proposal.Authorization.signingBytes()
			if err != nil {
				t.Fatal(err)
			}
			changed.Proposal.Signature = hex.EncodeToString(ed25519.Sign(f.key, message))
		}
		value := bootstrapSuccessorSafeCurrentTestSign(t, changed, f.key)
		_, err := value.validate(t.Context(), f.approval.Plan, base, runtime)
		if err == nil {
			err = value.extends(base, runtime, nil)
		}
		if err == nil {
			t.Fatal("current-policy revision accepted changed signed authority", fault)
		}
	}
	if err := os.WriteFile(approval.Authorization.Proposal.Authorization.ReviewEvidence.Path, []byte("synthetic swapped review"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := approval.validate(t.Context(), f.approval.Plan, base, runtime); err == nil {
		t.Fatal("current-policy revision accepted changed proposal evidence")
	}
}

// Optional current-policy references must not change any historical event byte
// or its existing content seal when the previous history policy remains in use.
func TestBootstrapSuccessorSafeCurrentRevisionPreservesLegacyEncoding(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	for _, event := range []bootstrapSuccessorExecutionEvent{owner.last, owner.attemptEvent()} {
		raw, err := json.Marshal(event)
		if err != nil || bytes.Contains(raw, []byte("safe_current_revision_hash")) {
			t.Fatal("current-policy field changed legacy event encoding", err)
		}
		var legacy map[string]json.RawMessage
		if err := json.Unmarshal(raw, &legacy); err != nil {
			t.Fatal(err)
		}
		before := event.ContentHash
		event.ContentHash = ""
		if rootObjectHash(event) != before {
			t.Fatal("current-policy field changed a historical content seal")
		}
	}
}
