// Policy journal faults exercise original byte preservation, exact counted
// authority and cross-journal recovery without granting a production send route.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Importing later policy/runtime authority never rewrites the policy of an
// already counted attempt or its historical canonical outcome.
func TestBootstrapSuccessorSafeCurrentRevisionConservesCustody(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	root, registry := owner.local.path, owner.registry.path
	original := bootstrapSuccessorPreparationTestFiles(t, root)
	delete(original, bootstrapSuccessorMemberSpec(false).Name)
	nonces := bootstrapSuccessorPreparationTestFiles(t, registry)
	base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	if err := owner.retainCanonicalAuthority(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	first := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, nil)
	if err := owner.retainSafeCurrentRevision(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := owner.append(owner.attemptEvent()); err != nil {
		t.Fatal(err)
	}
	counted, err := owner.local.read(bootstrapSuccessorExecutionEventName(1) + ".json")
	if err != nil || owner.last.SafeCurrentRevisionHash != rootObjectHash(first) || owner.last.CumulativeAttempts != 9 {
		t.Fatal("counted attempt omitted exact current-policy authority", err)
	}
	runtime := bootstrapSuccessorRuntimeTestNext(t, f, base, nil)
	changedRuntime := runtime.Authorization
	changedRuntime.RuntimeEvidence = first.Authorization.Proposal.Authorization.ReviewEvidence
	if err := owner.retainRuntimeRevision(t.Context(), bootstrapSuccessorRuntimeTestSign(t, changedRuntime, f.key)); err == nil || owner.runtimeHistory.hash() != "" {
		t.Fatal("later runtime reused retained current-policy evidence")
	}
	if err := owner.retainRuntimeRevision(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	second := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, []bootstrapSuccessorSafeCurrentRevisionApproval{first})
	if err := owner.retainSafeCurrentRevision(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	owner.close()
	owner = f.open(false, nil)
	if len(owner.safeCurrentHistory.approvals) != 2 || owner.last.SafeCurrentRevisionHash != rootObjectHash(first) || owner.last.CumulativeAttempts != 9 || owner.last.ReservedLifetimeWei != "1200000" {
		t.Fatal("policy revision changed counted custody or lost a historical runtime prefix")
	}
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: f.receipt()}
	result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, false)
	if err != nil || !result.InstallationComplete || owner.last.SafeCurrentRevisionHash != rootObjectHash(first) || owner.last.RuntimeRevisionHash != rootObjectHash(runtime) || owner.last.CumulativeAttempts != 9 {
		t.Fatal("historical outcome lost the exact counted current policy", result, err)
	}
	terminal := rootObjectHash(owner.last)
	if err := owner.retainSafeCurrentRevision(t.Context(), first); err != nil || rootObjectHash(owner.last) != terminal {
		t.Fatal("idempotent policy recovery rewrote terminal custody", err)
	}
	for name, raw := range original {
		retained, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(retained) != raw {
			t.Fatal("current-policy revision rewrote original custody", name, err)
		}
	}
	retained, err := owner.local.read(bootstrapSuccessorExecutionEventName(1) + ".json")
	if err != nil || !bytes.Equal(counted, retained) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, registry)) || len(f.writes) != 0 {
		t.Fatal("current policy rewrote counted bytes or nonce claims", err)
	}
}

// The signed journal cannot stand in for the distinct production capability.
// Refusal precedes observation, another reservation and every possible write.
func TestBootstrapSuccessorSafeCurrentRevisionCannotEnableSubmission(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	if err := owner.retainCanonicalAuthority(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	revision := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, nil)
	if err := owner.retainSafeCurrentRevision(t.Context(), revision); err != nil {
		t.Fatal(err)
	}
	adapter := &bootstrapSuccessorCanonicalChain{owner: owner, provenance: &bootstrapSuccessorCanonicalFixture{}, admitted: true}
	if _, err := adapter.observe(t.Context(), f.approval.Plan); !errors.Is(err, errBootstrapSuccessorSafeCurrentCapabilityUnavailable) || adapter.admitted {
		t.Fatal("legacy adapter admitted current policy without its distinct capability", err)
	}
	if err := adapter.submit(t.Context(), f.approval.Plan, nil); !errors.Is(err, errBootstrapSuccessorSafeCurrentCapabilityUnavailable) {
		t.Fatal("legacy adapter submitted under current policy without its distinct capability", err)
	}
	before := rootObjectHash(owner.last)
	result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true)
	if !errors.Is(err, errBootstrapSuccessorSafeCurrentCapabilityUnavailable) || result.SubmissionAttempted || rootObjectHash(owner.last) != before || f.observations != 0 || len(f.writes) != 0 {
		t.Fatal("signed current-policy custody enabled submission without a capability", result, err)
	}
	owner = f.open(false, nil)
	if owner.last.CumulativeAttempts != 8 || owner.safeCurrentHistory.hash() != rootObjectHash(revision) {
		t.Fatal("unavailable current-policy submission changed retained custody")
	}
}

// Every publication boundary, including an empty stage and true byte prefix,
// retains exactly one acceptance and excludes attempts or cross-runtime imports.
func TestBootstrapSuccessorSafeCurrentRevisionRecoversPublication(t *testing.T) {
	for _, boundary := range []string{"name-created", "name-synced", "stage-written", "stage-synced", "published", "published-synced", "partial-prefix"} {
		f := newBootstrapSuccessorExecutionFixture(t)
		owner := f.open(true, nil)
		base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
		if err := owner.retainCanonicalAuthority(t.Context(), base); err != nil {
			t.Fatal(err)
		}
		revision := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, nil)
		name := bootstrapSuccessorSafeCurrentName(1)
		interrupted, reached := errors.New("synthetic current-policy publication interruption"), false
		owner.local.hook = func(stage string) error {
			if stage == name+":"+boundary || boundary == "partial-prefix" && stage == name+":stage-written" {
				reached = true
				return interrupted
			}
			return nil
		}
		stagePath := filepath.Join(owner.local.path, owner.local.stageName(name, bootstrapSuccessorSafeCurrentStageKind(rootObjectHash(revision))))
		if err := owner.retainSafeCurrentRevision(t.Context(), revision); !errors.Is(err, interrupted) || !reached || !owner.closed {
			t.Fatal("current-policy interruption was not reached", boundary, err)
		}
		if boundary == "partial-prefix" {
			if err := os.Truncate(stagePath, 19); err != nil {
				t.Fatal(err)
			}
		}
		owner = f.open(false, nil)
		before := rootObjectHash(owner.last)
		if owner.safeCurrentHistory.pendingHash != "" {
			other := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, nil)
			if err := owner.retainSafeCurrentRevision(t.Context(), other); err == nil {
				t.Fatal("partial current-policy stage accepted another signed revision", boundary)
			}
			if err := owner.append(owner.attemptEvent()); err == nil || rootObjectHash(owner.last) != before {
				t.Fatal("partial current policy permitted a counted attempt", boundary, err)
			}
			runtime := bootstrapSuccessorRuntimeTestNext(t, f, base, nil)
			if err := owner.retainRuntimeRevision(t.Context(), runtime); err == nil {
				t.Fatal("partial current policy permitted a changed runtime tip", boundary)
			}
			if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err == nil || len(f.writes) != 0 || rootObjectHash(owner.last) != before {
				t.Fatal("partial current policy permitted a send", boundary, err)
			}
			owner = f.open(false, nil)
		}
		if err := owner.retainSafeCurrentRevision(t.Context(), revision); err != nil || owner.safeCurrentHistory.hash() != rootObjectHash(revision) || owner.safeCurrentHistory.pendingHash != "" || rootObjectHash(owner.last) != before {
			t.Fatal("current-policy recovery changed original custody", boundary, err)
		}
		owner.close()
	}
}

// A recomputed local checksum cannot remove a counted authority reference or
// make a terminal outcome use a later policy than the attempt that produced it.
func TestBootstrapSuccessorSafeCurrentRevisionRejectsEventSubstitution(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	if err := owner.retainCanonicalAuthority(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	first := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, nil)
	if err := owner.retainSafeCurrentRevision(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := owner.append(owner.attemptEvent()); err != nil {
		t.Fatal(err)
	}
	second := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, []bootstrapSuccessorSafeCurrentRevisionApproval{first})
	if err := owner.retainSafeCurrentRevision(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	changed := owner.last
	changed.Phase, changed.SafeCurrentRevisionHash = "installed", rootObjectHash(second)
	if err := owner.safeCurrentHistory.validateEvent(changed, &owner.last, owner.runtimeHistory); err == nil {
		t.Fatal("terminal policy changed the exact counted authority")
	}
	if err := owner.append(owner.attemptEvent()); err != nil {
		t.Fatal(err)
	}
	root := owner.local.path
	retained, err := json.Marshal(owner.last)
	if err != nil {
		t.Fatal(err)
	}
	changed = owner.last
	changed.SafeCurrentRevisionHash, changed.ContentHash = "", ""
	changed.ContentHash = rootObjectHash(changed)
	malformed, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	owner.close()
	for _, suffix := range []string{".intent", ".json"} {
		if err := os.WriteFile(filepath.Join(root, bootstrapSuccessorExecutionEventName(2)+suffix), malformed, 0600); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := openBootstrapSuccessorExecutionStore(f.storageContext(t.Context()), f.approval.Plan, f.approval, f.profile, false, nil)
	if reopened != nil {
		reopened.close()
	}
	if err == nil || !strings.Contains(err.Error(), "changed counted current-policy authority") {
		t.Fatal("counted event replay accepted a backward current policy", err)
	}
	for _, suffix := range []string{".intent", ".json"} {
		if err := os.WriteFile(filepath.Join(root, bootstrapSuccessorExecutionEventName(2)+suffix), retained, 0600); err != nil {
			t.Fatal(err)
		}
	}
	owner = f.open(false, nil)
	if owner.last.CumulativeAttempts != 10 || owner.last.SafeCurrentRevisionHash != rootObjectHash(second) || len(f.writes) != 0 {
		t.Fatal("policy event repair renewed consumed custody")
	}
}

// Complete counted authority must remain present, canonical and contiguous.
// Live owner snapshots also detect removed unreferenced suffixes immediately.
func TestBootstrapSuccessorSafeCurrentRevisionRejectsLostAndForkedHistory(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	if err := owner.retainCanonicalAuthority(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	first := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, nil)
	if err := owner.retainSafeCurrentRevision(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := owner.append(owner.attemptEvent()); err != nil {
		t.Fatal(err)
	}
	root, path := owner.local.path, filepath.Join(owner.local.path, bootstrapSuccessorSafeCurrentName(1))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, []bootstrapSuccessorSafeCurrentRevisionApproval{first})
	stage := owner.local.stageName(bootstrapSuccessorSafeCurrentName(1), bootstrapSuccessorSafeCurrentStageKind(rootObjectHash(first)))
	owner.close()
	for _, fault := range []string{"missing", "signature", "noncanonical", "gap", "fork", "completed stage"} {
		var extra string
		switch fault {
		case "missing":
			err = os.Remove(path)
		case "signature":
			changed := first
			changed.Signature = strings.Repeat("00", 64)
			encoded, _ := json.Marshal(changed)
			err = os.WriteFile(path, encoded, 0600)
		case "noncanonical":
			err = os.WriteFile(path, append(bytes.Clone(raw), '\n'), 0600)
		case "gap":
			extra = filepath.Join(root, bootstrapSuccessorSafeCurrentName(3))
			encoded, _ := json.Marshal(second)
			err = os.WriteFile(extra, encoded, 0600)
		case "fork":
			extra = filepath.Join(root, bootstrapSuccessorSafeCurrentName(2))
			changed := second.Authorization
			changed.PreviousHash = rootObjectHash(base)
			encoded, _ := json.Marshal(bootstrapSuccessorSafeCurrentTestSign(t, changed, f.key))
			err = os.WriteFile(extra, encoded, 0600)
		case "completed stage":
			extra = filepath.Join(root, stage)
			err = os.WriteFile(extra, nil, 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		reopened, err := openBootstrapSuccessorExecutionStore(f.storageContext(t.Context()), f.approval.Plan, f.approval, f.profile, false, nil)
		if reopened != nil {
			reopened.close()
		}
		if err == nil {
			t.Fatal("current-policy history accepted missing or conflicting custody", fault)
		}
		if extra != "" {
			if err := os.Remove(extra); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	owner = f.open(false, nil)
	if err := owner.retainSafeCurrentRevision(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, bootstrapSuccessorSafeCurrentName(2))); err != nil {
		t.Fatal(err)
	}
	if err := owner.checkpoint("synthetic-current-policy-removal"); err == nil {
		t.Fatal("current-policy deletion was hidden by an owner snapshot")
	}
}

// A partial counted attempt is consumed before any new acceptance. A partial
// terminal outcome blocks policy/runtime imports until exact reconciliation.
func TestBootstrapSuccessorSafeCurrentRevisionOrdersInterruptedEvents(t *testing.T) {
	for _, phase := range []string{"attempt-reserved", "installed"} {
		for _, boundary := range []string{"name-synced", "stage-written"} {
			f := newBootstrapSuccessorExecutionFixture(t)
			owner := f.open(true, nil)
			base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
			if err := owner.retainCanonicalAuthority(t.Context(), base); err != nil {
				t.Fatal(err)
			}
			first := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, nil)
			if err := owner.retainSafeCurrentRevision(t.Context(), first); err != nil {
				t.Fatal(err)
			}
			sequence := uint16(1)
			if phase == "installed" {
				if err := owner.append(owner.attemptEvent()); err != nil {
					t.Fatal(err)
				}
				sequence = 2
				f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: f.receipt()}
			}
			interrupted := errors.New("synthetic current-policy event interruption")
			owner.local.hook = func(stage string) error {
				if stage == bootstrapSuccessorExecutionEventName(sequence)+".intent:"+boundary {
					return interrupted
				}
				return nil
			}
			var err error
			if phase == "attempt-reserved" {
				err = owner.append(owner.attemptEvent())
			} else {
				_, err = advanceBootstrapSuccessorExecution(t.Context(), owner, f, false)
			}
			if !errors.Is(err, interrupted) || len(f.writes) != 0 {
				t.Fatal("current-policy event interruption was not reached", phase, boundary, err)
			}
			owner = f.open(false, nil)
			second := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, []bootstrapSuccessorSafeCurrentRevisionApproval{first})
			if owner.last.CumulativeAttempts != 9 || owner.last.SafeCurrentRevisionHash != rootObjectHash(first) {
				t.Fatal("partial event lost its consumed attempt or exact current policy")
			}
			if phase == "installed" {
				if owner.pending != "installed" {
					t.Fatal("partial policy outcome was not retained")
				}
				if err := owner.retainSafeCurrentRevision(t.Context(), second); err == nil {
					t.Fatal("current-policy revision changed a pending terminal reconstruction")
				}
				runtime := bootstrapSuccessorRuntimeTestNext(t, f, base, nil)
				if err := owner.retainRuntimeRevision(t.Context(), runtime); err == nil {
					t.Fatal("runtime changed a pending current-policy outcome")
				}
				result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, false)
				if err != nil || !result.InstallationComplete || owner.last.SafeCurrentRevisionHash != rootObjectHash(first) || owner.last.CumulativeAttempts != 9 {
					t.Fatal("partial outcome did not retain exact counted policy", result, err)
				}
			}
			before := rootObjectHash(owner.last)
			if err := owner.retainSafeCurrentRevision(t.Context(), second); err != nil || rootObjectHash(owner.last) != before || len(f.writes) != 0 {
				t.Fatal("post-recovery policy import rewrote an event", phase, boundary, err)
			}
			owner.close()
		}
	}
}
