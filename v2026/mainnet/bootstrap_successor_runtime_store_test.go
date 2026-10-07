// Runtime journal recovery conserves the existing immutable execution journal,
// both global nonce claims, counted attempts and full retained liability.
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

// An independent synthetic approver adds a distinct version without changing
// any execution or financial field. Evidence files remain separately pinned.
func bootstrapSuccessorRuntimeTestNext(t *testing.T, f *bootstrapSuccessorExecutionFixture, base bootstrapSuccessorCanonicalApproval, previous []bootstrapSuccessorRuntimeApproval) bootstrapSuccessorRuntimeApproval {
	t.Helper()
	profile := base.Authorization.CurrentRuntime
	profile.RuntimeVersion.SpecVersion += uint32(len(previous) + 1)
	return bootstrapSuccessorRuntimeTestApproval(t, f.approval.Plan, base, f.key, previous, profile)
}

// Adding twelve artifacts neither rewrites v1 events nor limits retained
// history to CRv4's per-call allowlist. A later outcome can name a newer tip.
func TestBootstrapSuccessorRuntimeRevisionConservesCustody(t *testing.T) {
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
	var firstAttempt []byte
	for i := range 12 {
		revision := bootstrapSuccessorRuntimeTestNext(t, f, base, owner.runtimeHistory.approvals)
		if err := owner.retainRuntimeRevision(t.Context(), revision); err != nil {
			t.Fatal("runtime lifecycle was capped or lost its predecessor", i, err)
		}
		if i == 0 {
			if err := owner.append(owner.attemptEvent()); err != nil {
				t.Fatal(err)
			}
			var err error
			firstAttempt, err = owner.local.read(bootstrapSuccessorExecutionEventName(1) + ".json")
			if err != nil || owner.last.RuntimeRevisionHash != rootObjectHash(revision) {
				t.Fatal("new attempt omitted exact runtime revision", err)
			}
		}
	}
	if len(owner.runtimeProfiles()) != 13 {
		t.Fatal("runtime revision discarded independently approved predecessors")
	}
	if owner.last.CumulativeAttempts != 9 || owner.last.ReservedLifetimeWei != "1200000" || owner.last.CanonicalAuthorityHash != rootObjectHash(base) {
		t.Fatal("additive runtime authority changed original custody")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	owner = f.open(false, nil)
	retainedAttempt, err := owner.local.read(bootstrapSuccessorExecutionEventName(1) + ".json")
	if err != nil || !bytes.Equal(firstAttempt, retainedAttempt) || len(owner.runtimeHistory.approvals) != 12 {
		t.Fatal("reopening rewrote a counted attempt or discarded runtime history", err)
	}
	for name, raw := range original {
		retained, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(retained) != raw {
			t.Fatal("runtime revision rewrote original claim or adoption bytes", name, err)
		}
	}
	if !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, registry)) {
		t.Fatal("runtime revision rewrote a nonce claim")
	}
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: f.receipt()}
	result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, false)
	if err != nil || !result.InstallationComplete || owner.last.RuntimeRevisionHash != owner.runtimeHistory.hash() || owner.last.CumulativeAttempts != 9 || len(f.writes) != 0 {
		t.Fatal("later runtime authority lost historical outcome custody", result, err)
	}
	terminal := rootObjectHash(owner.last)
	if err := owner.retainRuntimeRevision(t.Context(), owner.runtimeHistory.approvals[0]); err != nil || rootObjectHash(owner.last) != terminal {
		t.Fatal("idempotent earlier revision rewrote terminal custody", err)
	}
}

// Retained event references are ordered even if an attacker recomputes the
// local event checksum. New runtime authority cannot roll a later count backward.
func TestBootstrapSuccessorRuntimeRevisionRejectsEventRollback(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	if err := owner.retainCanonicalAuthority(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		revision := bootstrapSuccessorRuntimeTestNext(t, f, base, owner.runtimeHistory.approvals)
		if err := owner.retainRuntimeRevision(t.Context(), revision); err != nil {
			t.Fatal(err)
		}
		if err := owner.append(owner.attemptEvent()); err != nil {
			t.Fatal(err)
		}
	}
	root := owner.local.path
	retained, err := json.Marshal(owner.last)
	if err != nil {
		t.Fatal(err)
	}
	changed := owner.last
	changed.RuntimeRevisionHash, changed.ContentHash = "", ""
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
	if err == nil || !strings.Contains(err.Error(), "rolled back runtime revision authority") {
		t.Fatal("counted event replay accepted a backward runtime revision", err)
	}
	for _, suffix := range []string{".intent", ".json"} {
		if err := os.WriteFile(filepath.Join(root, bootstrapSuccessorExecutionEventName(2)+suffix), retained, 0600); err != nil {
			t.Fatal(err)
		}
	}
	owner = f.open(false, nil)
	if owner.last.CumulativeAttempts != 10 || owner.last.RuntimeRevisionHash != owner.runtimeHistory.hash() || len(f.writes) != 0 {
		t.Fatal("event repair renewed consumed runtime custody")
	}
}

// Even empty stages reserve one exact signed envelope. Every durable boundary
// and a genuine partial byte prefix recovers without another nonce or attempt.
func TestBootstrapSuccessorRuntimeRevisionRecoversPublication(t *testing.T) {
	for _, boundary := range []string{"name-created", "name-synced", "stage-written", "stage-synced", "published", "published-synced", "partial-prefix"} {
		f := newBootstrapSuccessorExecutionFixture(t)
		owner := f.open(true, nil)
		base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
		if err := owner.retainCanonicalAuthority(t.Context(), base); err != nil {
			t.Fatal(err)
		}
		revision := bootstrapSuccessorRuntimeTestNext(t, f, base, nil)
		name := bootstrapSuccessorRuntimeName(1)
		interrupted := errors.New("synthetic interrupted runtime publication")
		reached := false
		owner.local.hook = func(stage string) error {
			if stage == name+":"+boundary || boundary == "partial-prefix" && stage == name+":stage-written" {
				reached = true
				return interrupted
			}
			return nil
		}
		stagePath := filepath.Join(owner.local.path, owner.local.stageName(name, bootstrapSuccessorRuntimeStageKind(rootObjectHash(revision))))
		if err := owner.retainRuntimeRevision(t.Context(), revision); !errors.Is(err, interrupted) || !reached || !owner.closed {
			t.Fatal("runtime publication interruption was not reached", boundary, err)
		}
		if boundary == "partial-prefix" {
			if err := os.Truncate(stagePath, 19); err != nil {
				t.Fatal(err)
			}
		}
		owner = f.open(false, nil)
		before := rootObjectHash(owner.last)
		if owner.runtimeHistory.pendingHash != "" {
			other := bootstrapSuccessorRuntimeTestNext(t, f, base, nil)
			if err := owner.retainRuntimeRevision(t.Context(), other); err == nil {
				t.Fatal("empty runtime stage accepted a different independently signed revision", boundary)
			}
			if err := owner.append(owner.attemptEvent()); err == nil || rootObjectHash(owner.last) != before {
				t.Fatal("partial runtime revision permitted a counted attempt", boundary, err)
			}
			if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err == nil || len(f.writes) != 0 || rootObjectHash(owner.last) != before {
				t.Fatal("partial runtime revision permitted a send", boundary, err)
			}
			owner = f.open(false, nil)
		}
		if err := owner.retainRuntimeRevision(t.Context(), revision); err != nil || owner.runtimeHistory.hash() != rootObjectHash(revision) || owner.runtimeHistory.pendingHash != "" || rootObjectHash(owner.last) != before {
			t.Fatal("runtime prefix recovery changed existing custody", boundary, err)
		}
		owner.close()
	}
}

// Missing authority referenced by a counted event cannot be recreated from
// another input. Canonical encoding, physical file safety and chain contiguity
// are checked independently of the event reference.
func TestBootstrapSuccessorRuntimeRevisionRejectsLostAndForkedHistory(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	if err := owner.retainCanonicalAuthority(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	first := bootstrapSuccessorRuntimeTestNext(t, f, base, nil)
	if err := owner.retainRuntimeRevision(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := owner.append(owner.attemptEvent()); err != nil {
		t.Fatal(err)
	}
	root := owner.local.path
	path := filepath.Join(root, bootstrapSuccessorRuntimeName(1))
	raw, err := owner.local.read(bootstrapSuccessorRuntimeName(1))
	if err != nil {
		t.Fatal(err)
	}
	stage := owner.local.stageName(bootstrapSuccessorRuntimeName(1), bootstrapSuccessorRuntimeStageKind(rootObjectHash(first)))
	owner.close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	reopened, err := openBootstrapSuccessorExecutionStore(f.storageContext(t.Context()), f.approval.Plan, f.approval, f.profile, false, nil)
	if reopened != nil {
		reopened.close()
	}
	if err == nil || !strings.Contains(err.Error(), "counted event lost its runtime revision authority") {
		t.Fatal("missing counted runtime revision reopened execution custody", err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	second := bootstrapSuccessorRuntimeTestNext(t, f, base, []bootstrapSuccessorRuntimeApproval{first})
	for _, fault := range []string{"changed bytes", "noncanonical bytes", "gap", "fork", "completed stage", "unsafe mode", "symlink"} {
		var extra string
		switch fault {
		case "changed bytes":
			changed := first
			changed.Signature = strings.Repeat("00", 64)
			encoded, _ := json.Marshal(changed)
			err = os.WriteFile(path, encoded, 0600)
		case "noncanonical bytes":
			err = os.WriteFile(path, append(bytes.Clone(raw), '\n'), 0600)
		case "gap":
			extra = filepath.Join(root, bootstrapSuccessorRuntimeName(3))
			encoded, _ := json.Marshal(second)
			err = os.WriteFile(extra, encoded, 0600)
		case "fork":
			extra = filepath.Join(root, bootstrapSuccessorRuntimeName(2))
			changed := second.Authorization
			changed.PreviousHash = rootObjectHash(base)
			encoded, _ := json.Marshal(bootstrapSuccessorRuntimeTestSign(t, changed, f.key))
			err = os.WriteFile(extra, encoded, 0600)
		case "completed stage":
			extra = filepath.Join(root, stage)
			err = os.WriteFile(extra, nil, 0600)
		case "unsafe mode":
			err = os.Chmod(path, 0644)
		case "symlink":
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			target := bootstrapSuccessorExecutionTestRaw(t, "synthetic-linked-runtime.json", raw)
			err = os.Symlink(target.Path, path)
		}
		if err != nil {
			t.Fatal(err)
		}
		reopened, err = openBootstrapSuccessorExecutionStore(f.storageContext(t.Context()), f.approval.Plan, f.approval, f.profile, false, nil)
		if reopened != nil {
			reopened.close()
		}
		if err == nil {
			t.Fatal("runtime history accepted missing, changed or conflicting custody", fault)
		}
		if extra != "" {
			if err := os.Remove(extra); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	owner = f.open(false, nil)
	if owner.last.CumulativeAttempts != 9 || owner.runtimeHistory.hash() != rootObjectHash(first) {
		t.Fatal("restoring original runtime custody renewed an attempt")
	}
	if err := owner.retainRuntimeRevision(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, bootstrapSuccessorRuntimeName(2))); err != nil {
		t.Fatal(err)
	}
	if err := owner.checkpoint("synthetic-runtime-removal"); err == nil {
		t.Fatal("runtime authority deletion was hidden by an owner snapshot")
	}
}

// A partial counted reservation is consumed before new authority may arrive.
// Conversely, a partial terminal intent must retain its original runtime seal
// until canonical readback completes exactly those bytes.
func TestBootstrapSuccessorRuntimeRevisionOrdersInterruptedEvents(t *testing.T) {
	for _, phase := range []string{"attempt-reserved", "installed"} {
		for _, boundary := range []string{"name-synced", "stage-written"} {
			f := newBootstrapSuccessorExecutionFixture(t)
			owner := f.open(true, nil)
			base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
			if err := owner.retainCanonicalAuthority(t.Context(), base); err != nil {
				t.Fatal(err)
			}
			first := bootstrapSuccessorRuntimeTestNext(t, f, base, nil)
			if err := owner.retainRuntimeRevision(t.Context(), first); err != nil {
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
			interrupted := errors.New("synthetic cross-journal interruption")
			owner.local.hook = func(stage string) error {
				if stage == bootstrapSuccessorExecutionEventName(sequence)+".intent:"+boundary {
					return interrupted
				}
				return nil
			}
			if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); !errors.Is(err, interrupted) || len(f.writes) != 0 {
				t.Fatal("runtime cross-journal interruption was not reached", phase, boundary, err)
			}
			owner = f.open(false, nil)
			second := bootstrapSuccessorRuntimeTestNext(t, f, base, []bootstrapSuccessorRuntimeApproval{first})
			if phase == "attempt-reserved" {
				if owner.last.CumulativeAttempts != 9 || owner.last.RuntimeRevisionHash != rootObjectHash(first) || owner.pending != "" {
					t.Fatal("partial counted attempt was not consumed before runtime revision")
				}
				if err := owner.retainRuntimeRevision(t.Context(), second); err != nil {
					t.Fatal(err)
				}
				if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err != nil || owner.last.CumulativeAttempts != 10 || owner.last.RuntimeRevisionHash != rootObjectHash(second) || len(f.writes) != 1 {
					t.Fatal("runtime revision renewed a consumed attempt or replaced signed bytes", err)
				}
			} else {
				if owner.pending != "installed" {
					t.Fatal("partial outcome was not retained")
				}
				if err := owner.retainRuntimeRevision(t.Context(), second); err == nil {
					t.Fatal("runtime revision changed a pending terminal reconstruction")
				}
				result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, false)
				if err != nil || !result.InstallationComplete || owner.last.RuntimeRevisionHash != rootObjectHash(first) || owner.last.CumulativeAttempts != 9 {
					t.Fatal("pending outcome lost its original runtime authority", result, err)
				}
				terminal := rootObjectHash(owner.last)
				if err := owner.retainRuntimeRevision(t.Context(), second); err != nil || rootObjectHash(owner.last) != terminal || len(f.writes) != 0 {
					t.Fatal("post-outcome runtime revision rewrote terminal custody", err)
				}
			}
			owner.close()
		}
	}
}
