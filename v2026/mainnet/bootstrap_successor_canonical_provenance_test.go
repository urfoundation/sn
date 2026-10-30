// Provenance review is independently bound, while real orphan storage entries
// demonstrate why current Safe getters cannot grant the missing send capability.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Re-signing the outer review cannot make another deployed Safe's provenance
// usable. The provenance signature, profile and underlying history stay distinct.
func TestBootstrapSuccessorCanonicalProvenanceBindsExactScope(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	plan := f.approval.Plan
	approval := bootstrapSuccessorCanonicalTestApproval(t, plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	provenance, err := approval.readProvenance(t.Context(), plan)
	if err != nil {
		t.Fatal("canonical provenance positive review", err)
	}
	signCanonical := func(value bootstrapSuccessorSafeProvenanceApproval) bootstrapSuccessorCanonicalApproval {
		changed := approval
		changed.Authorization.SafeProvenance = bootstrapRootTestWrite(t, approval.Authorization.SafeProvenance.Path, value)
		message, err := changed.Authorization.signingBytes()
		if err != nil {
			t.Fatal(err)
		}
		changed.Signature = hex.EncodeToString(ed25519.Sign(f.key, message))
		return changed
	}
	for _, c := range []struct {
		name   string
		change func(*bootstrapSuccessorSafeProvenance)
	}{
		{name: "other Safe", change: func(p *bootstrapSuccessorSafeProvenance) { p.Safe = common.Address{19: 211} }},
		{name: "other singleton", change: func(p *bootstrapSuccessorSafeProvenance) { p.Singleton = common.Address{19: 212} }},
		{name: "other release", change: func(p *bootstrapSuccessorSafeProvenance) { p.Version = "1.5.0" }},
		{name: "other runtime", change: func(p *bootstrapSuccessorSafeProvenance) { p.SafeProxyRuntimeHash = common.Hash{1} }},
		{name: "other execution", change: func(p *bootstrapSuccessorSafeProvenance) {
			p.ExecutionPlanHash = rootObjectHash("synthetic other execution")
		}},
	} {
		changed := provenance
		c.change(&changed.Provenance)
		message, err := changed.Provenance.signingBytes()
		if err != nil {
			t.Fatal(err)
		}
		changed.Signature = hex.EncodeToString(ed25519.Sign(f.key, message))
		if err := signCanonical(changed).validate(t.Context(), plan); err == nil {
			t.Fatal("canonical provenance accepted swapped signed scope", c.name)
		}
	}
	changed := provenance
	changed.Signature = f.approval.Signature
	if err := signCanonical(changed).validate(t.Context(), plan); err == nil {
		t.Fatal("canonical provenance reused execution signature domain")
	}
	approval = signCanonical(provenance)
	if err := os.Remove(approval.Authorization.SafeProvenance.Path); err != nil {
		t.Fatal(err)
	}
	if err := approval.validate(t.Context(), plan); err == nil {
		t.Fatal("canonical authorization accepted missing signed provenance")
	}
	approval = signCanonical(provenance)
	if err := os.WriteFile(provenance.Provenance.HistoryEvidence.Path, []byte("synthetic substituted Safe history"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := approval.validate(t.Context(), plan); err == nil {
		t.Fatal("canonical provenance accepted substituted history")
	}
}

// Published code authorizes nonzero owner/module entries independently of the
// sentinel lists. A real orphan in local EVM storage stays invisible to ordinary
// census getters, so a review file cannot unlock the public submission path.
func TestBootstrapSuccessorCanonicalOrphanAuthorityCannotEnableSubmission(t *testing.T) {
	adapter, model, chain := bootstrapSuccessorCanonicalSafeFixture(t)
	plan := model.approval.Plan
	orphanModule, orphanOwner := common.Address{19: 211}, common.Address{19: 212}
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		for _, entry := range []struct {
			address common.Address
			slot    int64
		}{{address: orphanModule, slot: 1}, {address: orphanOwner, slot: 2}} {
			key := crypto.Keccak256Hash(common.LeftPadBytes(entry.address[:], 32), common.LeftPadBytes(big.NewInt(entry.slot).Bytes(), 32))
			chain.state.SetState(plan.Review.Transaction.Safe, key, common.BigToHash(big.NewInt(1)))
		}
	}()
	if _, err := adapter.safeState(t.Context(), plan, "pending"); err != nil {
		t.Fatal("orphan fixture did not preserve ordinary Safe census", err)
	}
	for _, getter := range []struct {
		name    string
		address common.Address
	}{{name: "isModuleEnabled", address: orphanModule}, {name: "isOwner", address: orphanOwner}} {
		result, err := adapter.safeCall(t.Context(), plan, "pending", 32, getter.name, getter.address)
		if err != nil || len(result) != 1 || result[0] != true {
			t.Fatal("published Safe did not expose the enabled orphan mapping", getter.name, result, err)
		}
	}
	if err := adapter.submit(t.Context(), plan, common.FromHex(plan.SignedRelayer)); !errors.Is(err, errBootstrapSuccessorSafeProvenanceUnavailable) {
		t.Fatal("orphan Safe acquired submission without canonical provenance", err)
	}
	approval := bootstrapSuccessorCanonicalTestApproval(t, plan, model.key, bootstrapSuccessorCanonicalTestRuntime())
	adapter.approval = approval
	if err := adapter.authenticateProvenance(t.Context(), plan, chainIdentity{}); !errors.Is(err, errBootstrapSuccessorSafeProvenanceUnavailable) {
		t.Fatal("signed provenance review substituted for canonical history capability", err)
	}
	var stdout, stderr bytes.Buffer
	pin := rootObjectHash("synthetic public flag digest")
	args := []string{"contract-successor-execution-resume", "--config", "/synthetic/config", "--run-dir", "/synthetic/custody", "--accept-plan-hash", pin,
		"--request", "/synthetic/request", "--safe-request", "/synthetic/safe", "--execution-request", "/synthetic/execution",
		"--approval", "/synthetic/approval", "--approval-sha256", pin, "--accept-execution-hash", pin,
		"--online", "--submit", "--canonical-approval", approval.Authorization.SafeProvenance.Path, "--canonical-approval-sha256", approval.Authorization.SafeProvenance.Sha256}
	if code := runBootstrapSuccessorExecutionCommand(t.Context(), args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), errBootstrapSuccessorSafeProvenanceUnavailable.Error()) {
		t.Fatal("public submit bypassed the unavailable provenance capability", code, stderr.String())
	}
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		if len(chain.writes) != 0 {
			t.Fatal("orphan authority refusal performed a transaction write")
		}
	}()
}
