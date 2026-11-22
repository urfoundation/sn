// Separate writer and passive owners retain the same original preparation.
// Kernel-fact transitions occur at exact I/O barriers, never timed sleeps.
package main

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The actual public claim is blocked while a passive preparation owns a shared
// view. After it joins, the same command publishes under exclusive admission;
// no preparation bytes, signature or nonce are replaced to make that work.
func TestBootstrapSuccessorPublicExclusiveClaimRetainsWriteAdmission(t *testing.T) {
	blocked := false
	f := newBootstrapSuccessorCanonicalFixtureWithClaimGate(t, func(f *bootstrapSuccessorCanonicalFixture) {
		root, registry := f.original.config.RunDirectory, f.approval.Plan.Request.RegistryDirectory
		before := bootstrapSuccessorPreparationTestFiles(t, root)
		nonces := bootstrapSuccessorPreparationTestFiles(t, registry)
		reader, _, err := openBootstrapSuccessorPreparationReader(f.original.storageContext(t.Context()), f.approval.Plan.Review.Preparation.Approval.Plan, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.close()
		var stdout bytes.Buffer
		code, diagnostic := f.invoke("contract-successor-execution-claim", &stdout, f.approvalArgs...)
		if code != 1 || stdout.Len() != 0 || !strings.Contains(diagnostic, "active local owner") || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, root)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, registry)) {
			t.Fatal("public claim escaped passive preparation ownership", code, diagnostic)
		}
		blocked = true
		if err := reader.close(); err != nil {
			t.Fatal(err)
		}
	})
	if !blocked {
		t.Fatal("public exclusive admission control was not reached")
	}
	before := bootstrapSuccessorPreparationTestFiles(t, f.original.config.RunDirectory)
	nonces := bootstrapSuccessorPreparationTestFiles(t, f.approval.Plan.Request.RegistryDirectory)
	var stdout bytes.Buffer
	code := f.original.command(t.Context(), "contract-successor-execution-resume", &stdout, &bytes.Buffer{}, append(append([]string{}, f.paths...), f.approvalArgs...)...)
	var result bootstrapSuccessorExecutionResult
	if err := decodePlanJson(stdout.Bytes(), &result); code != 0 || err != nil || !result.LocalCustodyComplete || result.SubmissionAttempted || result.CumulativeAttempts != f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.Budget.RetainedAttempts {
		t.Fatal("public exclusive claim could not retain and resume original custody", code, err)
	}
	if !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.original.config.RunDirectory)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, f.approval.Plan.Request.RegistryDirectory)) || len(f.original.contracts.writes) != 8 {
		t.Fatal("public local resume rewrote or submitted retained authority")
	}
}

// An execution writer borrows immutable preparation but must retain its own
// write admission. Claim/reopen preserves signed bytes and consumes no send.
func TestBootstrapSuccessorExecutionDurableWriteAdmission(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	root := f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory
	original := bootstrapSuccessorPreparationTestFiles(t, root)
	delete(original, bootstrapSuccessorMemberSpec(false).Name)
	owner := f.open(true, nil)
	if owner.last.Phase != "adopted" || owner.last.CumulativeAttempts != f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.Budget.RetainedAttempts || len(f.writes) != 0 {
		t.Fatal("claim consumed or changed original authority")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	claimed := bootstrapSuccessorPreparationTestFiles(t, root)
	nonces := bootstrapSuccessorPreparationTestFiles(t, f.approval.Plan.Request.RegistryDirectory)
	owner = f.open(false, nil)
	if owner.planCopy().SignedRelayer != f.approval.Plan.SignedRelayer {
		t.Fatal("reopen changed original signed transaction")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(claimed, bootstrapSuccessorPreparationTestFiles(t, root)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, f.approval.Plan.Request.RegistryDirectory)) {
		t.Fatal("ordinary reopen changed immutable execution custody")
	}
	for name, raw := range original {
		if claimed[name] != raw {
			t.Fatal("claim rewrote original preparation", name)
		}
	}
}

// Passive inspection stays available on full/read-only media, cannot publish
// even a temporary, and cannot lend write admission to an exclusive execution.
func TestBootstrapSuccessorPassiveDurableAdmissionHasNoWriteEffects(t *testing.T) {
	approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	owner, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	before := bootstrapSuccessorPreparationTestFiles(t, approval.Plan.Proposal.OriginalRunDirectory)
	storage.Host.SetReserve(0, 0)
	storage.Host.SetReadOnly(true)
	first, record, err := openBootstrapSuccessorPreparationReader(storage.Context, approval.Plan, nil)
	if err != nil || rootObjectHash(record.Approval) != rootObjectHash(approval) {
		t.Fatal("passive full-volume read lost original authority", err)
	}
	defer first.close()
	second, _, err := openBootstrapSuccessorPreparationReader(storage.Context, approval.Plan, nil)
	if err != nil {
		t.Fatal("independent passive read was excluded", err)
	}
	if err := first.publish("claim", "synthetic-forbidden-publication.json", []byte("synthetic forbidden bytes")); err == nil {
		t.Fatal("passive reader published a new claim")
	}
	if err := errors.Join(first.close(), second.close()); err != nil {
		t.Fatal(err)
	}
	writer, _, err := openBootstrapSuccessorPreparationReaderMode(storage.Context, approval.Plan, true, nil)
	if writer != nil {
		_ = writer.close()
	}
	if !errors.Is(err, durablevolume.ErrUnavailable) || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, approval.Plan.Proposal.OriginalRunDirectory)) {
		t.Fatal("passive or unavailable exclusive admission changed custody", err)
	}
	storage.Host.SetReserve(1024*1024, 1024)
	storage.Host.SetReadOnly(false)
	writer, _, err = openBootstrapSuccessorPreparationReaderMode(storage.Context, approval.Plan, true, nil)
	if err != nil {
		t.Fatal("recovered volume did not admit the separately opened writer", err)
	}
	if err := writer.close(); err != nil {
		t.Fatal(err)
	}
}

// Pressure after initial admission but before the first claim is a refusal,
// not an uncertain temporary publication. The same approval remains fresh.
func TestBootstrapSuccessorPreparationPressureBeforeClaimHasNoEffects(t *testing.T) {
	approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	root := approval.Plan.Proposal.OriginalRunDirectory
	before := bootstrapSuccessorPreparationTestFiles(t, root)
	owner, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, func(stage string) error {
		if stage == "owner-acquired" {
			storage.Host.SetReserve(0, 0)
		}
		return nil
	})
	if owner != nil {
		_ = owner.close()
	}
	if !errors.Is(err, durablevolume.ErrUnavailable) || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, root)) {
		t.Fatal("pre-claim pressure mutated preparation custody", err)
	}
	storage.Host.SetReserve(1024*1024, 1024)
	owner, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, nil)
	if err != nil {
		t.Fatal("pre-admission refusal consumed the fresh claim", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
}

// The separate execution root and nonce registry remain untouched when write
// reserve disappears after both owners are acquired but before any publication.
func TestBootstrapSuccessorExecutionPressureBeforeClaimHasNoEffects(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	root := f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory
	before := bootstrapSuccessorPreparationTestFiles(t, root)
	nonces := bootstrapSuccessorPreparationTestFiles(t, f.approval.Plan.Request.RegistryDirectory)
	owner, err := openBootstrapSuccessorExecutionStore(f.storageContext(t.Context()), f.approval.Plan, f.approval, f.profile, true, func(stage string) error {
		if stage == "execution-owner-acquired" {
			f.storage.Host.SetReserve(0, 0)
		}
		return nil
	})
	if owner != nil {
		_ = owner.close()
	}
	if !errors.Is(err, durablevolume.ErrUnavailable) || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, root)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, f.approval.Plan.Request.RegistryDirectory)) || len(f.writes) != 0 {
		t.Fatal("pre-claim execution pressure changed custody or sent", err)
	}
	f.storage.Host.SetReserve(1024*1024, 1024)
	owner = f.open(true, nil)
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
}

// Once the exact claim name is durable, a later refusal retains that pending
// name without writing the payload. Recovery uses its original approval only.
func TestBootstrapSuccessorPreparationPressureRetainsEmptyClaimStage(t *testing.T) {
	approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	owner, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, func(stage string) error {
		if stage == "claim-name-synced" {
			storage.Host.SetReserve(0, 0)
		}
		return nil
	})
	if owner != nil {
		_ = owner.close()
	}
	name := (&bootstrapSuccessorPreparationStore{approval: approval}).stageName("claim")
	path := filepath.Join(approval.Plan.Proposal.OriginalRunDirectory, name)
	raw, readErr := os.ReadFile(path)
	if !errors.Is(err, durablevolume.ErrUnavailable) || readErr != nil || len(raw) != 0 {
		t.Fatal("post-name pressure wrote or lost the pending claim", err, readErr, len(raw))
	}
	storage.Host.SetReserve(1024*1024, 1024)
	owner, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
	if err != nil {
		t.Fatal("original pending preparation could not resume", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
}

// Execution has the same pre-payload boundary, retaining exact signed intent
// and both original nonce identities through close/reopen without a send.
func TestBootstrapSuccessorExecutionPressureRetainsEmptyClaimStage(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	root := f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory
	claimName := bootstrapSuccessorExecutionPrefix + ".claim"
	owner, err := openBootstrapSuccessorExecutionStore(f.storageContext(t.Context()), f.approval.Plan, f.approval, f.profile, true, func(stage string) error {
		if stage == claimName+":name-synced" {
			f.storage.Host.SetReserve(0, 0)
		}
		return nil
	})
	if owner != nil {
		_ = owner.close()
	}
	name := (&bootstrapSuccessorExecutionDirectory{claim: rootObjectHash(f.approval)}).stageName(claimName, "claim")
	raw, readErr := os.ReadFile(filepath.Join(root, name))
	if !errors.Is(err, durablevolume.ErrUnavailable) || readErr != nil || len(raw) != 0 || len(f.writes) != 0 {
		t.Fatal("post-name pressure wrote or lost original execution intent", err, readErr, len(raw))
	}
	f.storage.Host.SetReserve(1024*1024, 1024)
	owner = f.open(false, nil)
	if owner.planCopy().SignedRelayer != f.approval.Plan.SignedRelayer || len(f.writes) != 0 {
		t.Fatal("pending claim recovery replaced signed bytes or sent")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
}

// Completing a prepared record has a separate write boundary. Reserve loss
// there cannot claim completion; exact pending marker/record bytes survive.
func TestBootstrapSuccessorPreparationPressureBeforeCompletionRetainsPending(t *testing.T) {
	approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	root := approval.Plan.Proposal.OriginalRunDirectory
	owner, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, func(stage string) error {
		if stage == "record-retained" {
			storage.Host.SetReserve(0, 0)
		}
		return nil
	})
	if owner != nil {
		_ = owner.close()
	}
	marker, readErr := os.ReadFile(filepath.Join(root, bootstrapSuccessorPreparationFile+".lock"))
	if !errors.Is(err, durablevolume.ErrUnavailable) || readErr != nil || string(marker) != rootObjectHash(approval)+"\n" {
		t.Fatal("completion pressure wrote or lost the pending marker", err, readErr)
	}
	record, err := os.ReadFile(filepath.Join(root, bootstrapSuccessorPreparationFile))
	if err != nil {
		t.Fatal(err)
	}
	storage.Host.SetReserve(1024*1024, 1024)
	owner, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
	if err != nil {
		t.Fatal("original pending completion could not resume", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(filepath.Join(root, bootstrapSuccessorPreparationFile))
	if err != nil || !bytes.Equal(record, retained) {
		t.Fatal("completion recovery rewrote original prepared record", err)
	}
}
