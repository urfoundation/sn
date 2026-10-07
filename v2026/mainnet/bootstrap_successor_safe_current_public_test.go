// Public current-only acceptance is a separate signing decision. Synthetic
// independent keys exercise version separation and durable original custody.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Only synthetic test approvers issue these signatures. The production command
// consumes independently supplied approvals and never signs or upgrades v1.
func bootstrapSuccessorSafeCurrentTestPublicAcceptance(t *testing.T, revision bootstrapSuccessorSafeCurrentRevisionApproval, key ed25519.PrivateKey) bootstrapSuccessorSafeCurrentRevisionApproval {
	t.Helper()
	revision.Authorization.Schema = bootstrapSuccessorSafeCurrentPublicRevisionSchema
	revision.Authorization.Policy = bootstrapSuccessorSafeCurrentPublicRevisionPolicy
	raw, err := json.Marshal(revision.Authorization)
	if err != nil {
		t.Fatal(err)
	}
	revision.Signature = hex.EncodeToString(ed25519.Sign(key, append([]byte(bootstrapSuccessorSafeCurrentPublicRevisionDomain+"\x00"), raw...)))
	return revision
}

// A real v2 signature succeeds while schema-only upgrades, reused domains,
// different keys and weaker policy text cannot grant public write authority.
func TestBootstrapSuccessorSafeCurrentPublicAcceptanceSeparateDomain(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	runtime := bootstrapSuccessorRuntimeHistory{}
	legacy := bootstrapSuccessorSafeCurrentTestNext(t, f, base, runtime, nil)
	approval := bootstrapSuccessorSafeCurrentTestPublicAcceptance(t, legacy, f.key)
	if _, err := approval.validate(t.Context(), f.approval.Plan, base, runtime); err != nil || !approval.Authorization.permitsPublicSubmission() {
		t.Fatal("independent public current-only acceptance refused", err)
	}
	if _, err := legacy.validate(t.Context(), f.approval.Plan, base, runtime); err != nil || legacy.Authorization.permitsPublicSubmission() {
		t.Fatal("legacy signature changed its public meaning", err)
	}
	for _, fault := range []string{"legacy signature", "legacy domain", "proposal signature", "foreign key", "v1 schema", "v1 policy", "weaker policy", "unknown schema"} {
		changed := approval
		switch fault {
		case "legacy signature":
			changed.Signature = legacy.Signature
		case "legacy domain":
			changed = bootstrapSuccessorSafeCurrentTestSign(t, changed.Authorization, f.key)
		case "proposal signature":
			changed.Signature = legacy.Authorization.Proposal.Signature
		case "foreign key":
			changed = bootstrapSuccessorSafeCurrentTestPublicAcceptance(t, changed, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{213}, ed25519.SeedSize)))
		case "v1 schema":
			changed.Authorization.Schema = bootstrapSuccessorSafeCurrentRevisionSchema
		case "v1 policy":
			changed.Authorization.Policy = bootstrapSuccessorSafeCurrentRevisionPolicy
		case "weaker policy":
			changed.Authorization.Policy = "synthetic-accept-without-pending-risk"
		case "unknown schema":
			changed.Authorization.Schema = "synthetic-unreviewed-acceptance-version"
		}
		if fault == "v1 schema" || fault == "v1 policy" || fault == "weaker policy" || fault == "unknown schema" {
			raw, err := json.Marshal(changed.Authorization)
			if err != nil {
				t.Fatal(err)
			}
			changed.Signature = hex.EncodeToString(ed25519.Sign(f.key, append([]byte(bootstrapSuccessorSafeCurrentPublicRevisionDomain+"\x00"), raw...)))
		}
		if _, err := changed.validate(t.Context(), f.approval.Plan, base, runtime); err == nil {
			t.Fatal("public current-only acceptance ignored a signing boundary", fault)
		}
	}
}

// Importing v1 cannot unlock the public route. V2 extends, rather than rewrites,
// that exact history; partial publication blocks sends and later runtime imports.
func TestBootstrapSuccessorSafeCurrentPublicAcceptanceResumesExactCustody(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	root, registry := owner.local.path, owner.registry.path
	original := bootstrapSuccessorPreparationTestFiles(t, root)
	nonces := bootstrapSuccessorPreparationTestFiles(t, registry)
	base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	if err := owner.retainCanonicalAuthority(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	legacy := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, nil)
	if err := owner.retainSafeCurrentRevision(t.Context(), legacy); err != nil {
		t.Fatal(err)
	}
	adapter := &bootstrapSuccessorCanonicalChain{owner: owner, approval: base, currentRevisionHash: rootObjectHash(legacy)}
	if err := adapter.selectCurrentPolicy(t.Context(), bootstrapSuccessorSafeCurrentPublicRoute); err == nil || !strings.Contains(err.Error(), "separately signed v2 acceptance") || adapter.currentPolicy != nil {
		t.Fatal("legacy acceptance gained public submission authority", err)
	}
	if err := owner.append(owner.attemptEvent()); err != nil {
		t.Fatal(err)
	}
	counted := owner.last
	countedRaw, err := owner.local.read(bootstrapSuccessorExecutionEventName(counted.Sequence) + ".json")
	if err != nil {
		t.Fatal(err)
	}
	legacyRaw, err := owner.local.read(bootstrapSuccessorSafeCurrentName(1))
	if err != nil {
		t.Fatal(err)
	}
	approval := bootstrapSuccessorSafeCurrentTestPublicAcceptance(t, bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, owner.safeCurrentHistory.approvals), f.key)
	interrupted := errors.New("synthetic v2 acceptance publication interrupted")
	name := bootstrapSuccessorSafeCurrentName(2)
	owner.local.hook = func(stage string) error {
		if stage == name+":stage-written" {
			return interrupted
		}
		return nil
	}
	stagePath := filepath.Join(root, owner.local.stageName(name, bootstrapSuccessorSafeCurrentStageKind(rootObjectHash(approval))))
	if err := owner.retainSafeCurrentRevision(t.Context(), approval); !errors.Is(err, interrupted) || !owner.closed {
		t.Fatal("public acceptance did not retain its interrupted publication", err)
	}
	if err := os.Truncate(stagePath, 19); err != nil {
		t.Fatal(err)
	}
	owner = f.open(false, nil)
	adapter.owner = owner
	if owner.safeCurrentHistory.pendingHash != rootObjectHash(approval) || owner.last != counted {
		t.Fatal("partial public acceptance lost the exact hash or counted custody")
	}
	if err := adapter.selectCurrentPolicy(t.Context(), bootstrapSuccessorSafeCurrentPublicRoute); err == nil || adapter.currentPolicy != nil {
		t.Fatal("partial public acceptance selected a write route")
	}
	other := bootstrapSuccessorSafeCurrentTestPublicAcceptance(t, bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, owner.safeCurrentHistory.approvals), f.key)
	if err := owner.retainSafeCurrentRevision(t.Context(), other); err == nil {
		t.Fatal("partial public acceptance was replaced by another signed revision")
	}
	runtime := bootstrapSuccessorRuntimeTestNext(t, f, base, nil)
	if err := owner.retainRuntimeRevision(t.Context(), runtime); err == nil {
		t.Fatal("partial public acceptance allowed the runtime tip to advance")
	}
	if err := owner.retainSafeCurrentRevision(t.Context(), approval); err != nil || owner.last != counted {
		t.Fatal("exact public acceptance recovery changed counted custody", err)
	}
	adapter.currentRevisionHash = rootObjectHash(approval)
	if err := adapter.selectCurrentPolicy(t.Context(), bootstrapSuccessorSafeCurrentPublicRoute); err != nil || adapter.currentPolicy == nil || adapter.currentPolicy.route != bootstrapSuccessorSafeCurrentPublicRoute {
		t.Fatal("complete public acceptance did not select its distinct native route", err)
	}
	adapter.provenance = &bootstrapSuccessorCanonicalFixture{}
	if err := adapter.selectCurrentPolicy(t.Context(), bootstrapSuccessorSafeCurrentPublicRoute); err == nil || adapter.currentPolicy != nil {
		t.Fatal("public current-only acceptance was mixed with complete history")
	}
	adapter.provenance = nil
	if err := owner.retainRuntimeRevision(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	adapter.runtimeRevisionHash = owner.runtimeHistory.hash()
	if err := adapter.selectCurrentPolicy(t.Context(), bootstrapSuccessorSafeCurrentPublicRoute); err == nil || adapter.currentPolicy != nil {
		t.Fatal("public acceptance silently widened to later runtime authority")
	}
	owner.close()
	owner = f.open(false, nil)
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: f.receipt()}
	result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, false)
	if err != nil || !result.InstallationComplete || owner.last.SafeCurrentRevisionHash != rootObjectHash(legacy) || owner.last.ReservedLifetimeWei != counted.ReservedLifetimeWei || owner.last.CumulativeAttempts != counted.CumulativeAttempts || len(f.writes) != 0 {
		t.Fatal("v2 import/runtime drift changed historical counted policy or liability", result, err)
	}
	for name, raw := range original {
		retained, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(retained) != raw {
			t.Fatal("public acceptance changed original signed custody", name, err)
		}
	}
	retainedCounted, countedErr := owner.local.read(bootstrapSuccessorExecutionEventName(counted.Sequence) + ".json")
	retainedLegacy, legacyErr := owner.local.read(bootstrapSuccessorSafeCurrentName(1))
	if countedErr != nil || legacyErr != nil || !bytes.Equal(countedRaw, retainedCounted) || !bytes.Equal(legacyRaw, retainedLegacy) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, registry)) {
		t.Fatal("public acceptance recovery rewrote legacy/count/nonce custody", countedErr, legacyErr)
	}
}
