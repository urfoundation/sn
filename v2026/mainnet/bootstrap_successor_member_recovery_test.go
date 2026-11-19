// Joined real owners and explicit publication barriers discriminate recoverable
// exact pending state from missing committed custody and unknown partial bytes.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// A publication that reached rename cannot be retried by the same live owner.
// The original counted bytes are reconciled only after that owner has joined.
func TestBootstrapSuccessorMemberUncertaintyRetainsOriginalCountedIntent(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	armed := false
	name := bootstrapSuccessorExecutionEventName(1) + ".intent"
	owner := f.open(true, func(stage string) error {
		if armed && stage == name+":published" {
			return io.ErrUnexpectedEOF
		}
		return nil
	})
	armed = true
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); !errors.Is(err, errMainnetDurablePublicationUncertain) || errors.Is(err, durablevolume.ErrIdentity) || len(f.writes) != 0 {
		_ = owner.close()
		t.Fatal("post-rename failure lost uncertainty or allowed send", err)
	}
	path := filepath.Join(owner.local.path, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		_ = owner.close()
		t.Fatal(err)
	}
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err == nil || !owner.closed || len(f.writes) != 0 {
		_ = owner.close()
		t.Fatal("uncertain live owner blindly retried mutation or send", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	owner = f.open(false, nil)
	defer owner.close()
	retained, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, retained) || owner.last.CumulativeAttempts != 9 || len(f.writes) != 0 {
		t.Fatal("joined recovery changed original counted intent or allowance", err, owner.last.CumulativeAttempts)
	}
}

// A durable reservation is already a claimant even before the first stage
// exists. Only exact resume may finish it; create cannot renew that authority.
func TestBootstrapSuccessorPreparationResumesReservedIntentBeforeStage(t *testing.T) {
	approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	owner, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, func(stage string) error {
		if stage == "claim-reserved" {
			return io.ErrUnexpectedEOF
		}
		return nil
	})
	if owner != nil || !errors.Is(err, errMainnetDurablePublicationUncertain) {
		t.Fatal("pre-stage reservation did not retain original authority", err)
	}
	root := approval.Plan.Proposal.OriginalRunDirectory
	before := bootstrapSuccessorPreparationTestFiles(t, root)
	if len(before) != 1 || before[bootstrapSuccessorMemberSpec(false).Name] == "" {
		t.Fatal("pre-stage control has unexpected physical members")
	}
	owner, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, nil)
	if owner != nil {
		_ = owner.close()
	}
	if err == nil || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, root)) {
		t.Fatal("fresh prepare reused a reserved original claim", err)
	}
	owner, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
	if err != nil {
		t.Fatal("exact pre-stage preparation could not resume", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
}

// The pending payload carries the counted intent through a crash before its
// stage is named. Reopening consumes its original count and performs no send.
func TestBootstrapSuccessorExecutionResumesReservedCountedIntentBeforeStage(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	armed := false
	name := bootstrapSuccessorExecutionEventName(1) + ".intent"
	owner := f.open(true, func(stage string) error {
		if armed && stage == name+":reserved" {
			return io.ErrUnexpectedEOF
		}
		return nil
	})
	armed = true
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); !errors.Is(err, errMainnetDurablePublicationUncertain) || !owner.closed || len(f.writes) != 0 {
		t.Fatal("pre-stage attempt reservation did not stop and join its owner", err)
	}
	root := f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory
	raw, err := os.ReadFile(filepath.Join(root, bootstrapSuccessorMemberSpec(false).Name))
	if err != nil {
		t.Fatal(err)
	}
	var census bootstrapSuccessorMemberCensus
	if err := json.Unmarshal(raw, &census); err != nil || census.Pending == nil || census.Pending.Name != name || census.Pending.StageInode != 0 {
		t.Fatal("pre-stage checkpoint lost the exact counted intent", err)
	}
	if _, err := os.Lstat(filepath.Join(root, census.Pending.Stage)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pre-stage control already created a physical member", err)
	}
	expected, err := base64.StdEncoding.Strict().DecodeString(census.Pending.Payload)
	if err != nil {
		t.Fatal(err)
	}
	owner = f.open(false, nil)
	defer owner.close()
	retained, err := os.ReadFile(filepath.Join(root, name))
	if err != nil || !bytes.Equal(expected, retained) || owner.last.CumulativeAttempts != 9 || len(f.writes) != 0 {
		t.Fatal("pre-stage recovery changed intent, renewed allowance or sent", err, owner.last.CumulativeAttempts)
	}
}

// Materializing a terminal reservation cannot make it final or permit newer
// runtime/policy authority before canonical reconciliation of the old outcome.
func TestBootstrapSuccessorReservedOutcomeRetainsApplicationOrdering(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	base := bootstrapSuccessorCanonicalTestApproval(t, f.approval.Plan, f.key, bootstrapSuccessorCanonicalTestRuntime())
	if err := owner.retainCanonicalAuthority(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	runtime := bootstrapSuccessorRuntimeTestNext(t, f, base, nil)
	if err := owner.retainRuntimeRevision(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	policy := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, nil)
	if err := owner.retainSafeCurrentRevision(t.Context(), policy); err != nil {
		t.Fatal(err)
	}
	if err := owner.append(owner.attemptEvent()); err != nil {
		t.Fatal(err)
	}
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: f.receipt()}
	name := bootstrapSuccessorExecutionEventName(2) + ".intent"
	owner.local.hook = func(stage string) error {
		if stage == name+":reserved" {
			return io.ErrUnexpectedEOF
		}
		return nil
	}
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, false); !errors.Is(err, errMainnetDurablePublicationUncertain) || !owner.closed || len(f.writes) != 0 {
		t.Fatal("pre-stage terminal reservation did not stop and join its owner", err)
	}
	root := f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory
	raw, err := os.ReadFile(filepath.Join(root, bootstrapSuccessorMemberSpec(false).Name))
	if err != nil {
		t.Fatal(err)
	}
	var census bootstrapSuccessorMemberCensus
	if err := json.Unmarshal(raw, &census); err != nil || census.Pending == nil || census.Pending.Name != name || census.Pending.StageInode != 0 {
		t.Fatal("pre-stage terminal reservation lost exact original bytes", err)
	}
	if _, err := os.Lstat(filepath.Join(root, census.Pending.Stage)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("terminal control already has a physical stage", err)
	}
	expected, err := base64.StdEncoding.Strict().DecodeString(census.Pending.Payload)
	if err != nil {
		t.Fatal(err)
	}
	owner = f.open(false, nil)
	defer owner.close()
	if owner.pending != "installed" || owner.last.CumulativeAttempts != 9 || owner.last.RuntimeRevisionHash != rootObjectHash(runtime) || owner.last.SafeCurrentRevisionHash != rootObjectHash(policy) || len(f.writes) != 0 {
		t.Fatal("pre-stage terminal recovery finalized or changed original authority")
	}
	staged, err := os.ReadFile(filepath.Join(root, census.Pending.Stage))
	if err != nil || !bytes.Equal(expected, staged) {
		t.Fatal("pre-stage recovery changed retained terminal bytes", err)
	}
	if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pre-stage recovery finalized before canonical reconciliation", err)
	}
	nextRuntime := bootstrapSuccessorRuntimeTestNext(t, f, base, []bootstrapSuccessorRuntimeApproval{runtime})
	nextPolicy := bootstrapSuccessorSafeCurrentTestNext(t, f, base, owner.runtimeHistory, []bootstrapSuccessorSafeCurrentRevisionApproval{policy})
	if err := owner.retainRuntimeRevision(t.Context(), nextRuntime); err == nil {
		t.Fatal("new runtime authority crossed the pending original outcome")
	}
	if err := owner.retainSafeCurrentRevision(t.Context(), nextPolicy); err == nil {
		t.Fatal("new policy authority crossed the pending original outcome")
	}
	result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, false)
	retained, readErr := os.ReadFile(filepath.Join(root, name))
	if err != nil || readErr != nil || !result.InstallationComplete || !bytes.Equal(expected, retained) || owner.last.CumulativeAttempts != 9 || len(f.writes) != 0 {
		t.Fatal("canonical reconciliation changed exact reserved outcome or allowance", err, readErr)
	}
}

// The child exits after a real payload fsync while its reservation and original
// staged inode remain retained. No cleanup callback simulates the crash.
func TestBootstrapSuccessorMemberCrashResumesExactPreparedPayload(t *testing.T) {
	const childVariable = "URNETWORK_TEST_SUCCESSOR_MEMBER_CRASH"
	if path := os.Getenv(childVariable); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var approval bootstrapSuccessorPreparationApproval
		if err := json.Unmarshal(raw, &approval); err != nil {
			t.Fatal(err)
		}
		storage := durablefixture.New(t, t.Context(), approval.Plan.Proposal.OriginalRunDirectory)
		_, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, func(stage string) error {
			if stage == "record-stage-synced" {
				os.Exit(73)
			}
			return nil
		})
		t.Fatal("child did not reach its real synced crash boundary", err)
	}
	approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	raw, err := json.Marshal(approval)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "synthetic-public-preparation.json")
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestBootstrapSuccessorMemberCrashResumesExactPreparedPayload$", "-test.count=1")
	child.Env = append(os.Environ(), childVariable+"="+input)
	output, err := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 73 {
		t.Fatalf("child did not crash at the exact publication barrier: %v %s", err, output)
	}
	owner, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
	if err != nil {
		t.Fatal("original crash custody did not resume", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	root := approval.Plan.Proposal.OriginalRunDirectory
	completed := bootstrapSuccessorPreparationTestFiles(t, root)
	owner, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil || !maps.Equal(completed, bootstrapSuccessorPreparationTestFiles(t, root)) {
		t.Fatal("completed crash recovery rewrote original bytes", err)
	}
}

// A retained reservation authorizes only the original exact payload prefix.
// Unknown staged bytes remain present and cannot be rewritten during recovery.
func TestBootstrapSuccessorMemberRecoveryRefusesUnknownPartialBytes(t *testing.T) {
	approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	owner, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, func(stage string) error {
		if stage == "record-name-synced" {
			return io.ErrUnexpectedEOF
		}
		return nil
	})
	if owner != nil || !errors.Is(err, errMainnetDurablePublicationUncertain) {
		t.Fatal("exact partial-stage barrier was not retained", err)
	}
	root := approval.Plan.Proposal.OriginalRunDirectory
	stage := bootstrapSuccessorStagePrefix + strings.TrimPrefix(rootObjectHash(approval), "sha256:") + ".record"
	if err := os.WriteFile(filepath.Join(root, stage), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	before := bootstrapSuccessorPreparationTestFiles(t, root)
	owner, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
	if owner != nil {
		_ = owner.close()
	}
	if !errors.Is(err, durablevolume.ErrIdentity) || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, root)) {
		t.Fatal("unknown partial member was silently repaired or discarded", err)
	}
}

// The original run directory can contain other role namespaces. Exhausting a
// finite scan allowance refuses admission without inventing an owned-file loss.
func TestBootstrapSuccessorUnrelatedDirectoryPressureDoesNotPoisonCustody(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	defer owner.close()
	before := bootstrapSuccessorPreparationTestFiles(t, owner.local.path)
	names := make([]string, 0, maximumBootstrapSuccessorMemberCount+8)
	for i := range maximumBootstrapSuccessorMemberCount + 8 {
		path := filepath.Join(owner.local.path, fmt.Sprintf("synthetic-other-role-%05d.json", i))
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		names = append(names, path)
	}
	if err := owner.checkpoint("synthetic-unrelated-directory-capacity"); !errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, durablevolume.ErrIdentity) || len(f.writes) != 0 {
		t.Fatal("incomplete bounded scan became confirmed custody loss", err)
	}
	for _, path := range names {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := owner.checkpoint("synthetic-unrelated-capacity-restored"); err != nil || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, owner.local.path)) || len(f.writes) != 0 {
		t.Fatal("same owner could not recover unchanged custody after scan pressure", err)
	}
}
