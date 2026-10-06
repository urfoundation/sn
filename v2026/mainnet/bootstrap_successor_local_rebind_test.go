//go:build linux

// Private owner controls complement the unchanged public causal test overlay.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"maps"
	"path/filepath"
	"strings"
	"testing"
)

// Loss of the real publication acknowledgement must resume exactly that
// retained receipt. It cannot renew the completed original execution counter.
func TestBootstrapSuccessorLocalRebindLostAcknowledgementResumesOriginalReceipt(t *testing.T) {
	f := newBootstrapSuccessorCanonicalFixture(t)
	path, hash := localRebindTestRestore(t, f)
	ctx := f.original.storageContext(t.Context())
	plan, err := buildBootstrapSuccessorLocalRebind(ctx, f.approval, f.profile, durablevolume.Reference{Path: path, Sha256: hash})
	if err != nil {
		t.Fatal(err)
	}
	message, err := plan.signingBytes(f.approval, f.profile)
	if err != nil {
		t.Fatal(err)
	}
	approval := bootstrapSuccessorLocalRebindApproval{Schema: bootstrapSuccessorLocalRebindEnvelopeSchema, Plan: plan, Signature: hex.EncodeToString(ed25519.Sign(f.key, message))}
	ref := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(path), "synthetic-local-lost-ack.json"), approval)
	nonces := bootstrapSuccessorPreparationTestFiles(t, f.approval.Plan.Request.RegistryDirectory)
	lost := errors.New("synthetic completed local receipt acknowledgement loss")
	reached := false
	owner, err := openBootstrapSuccessorExecutionStoreWithPhysicalRebind(ctx, f.approval.Plan, f.approval, f.profile, false, func(stage string) error {
		if stage == bootstrapSuccessorLocalRebindFile+":published-synced" {
			reached = true
			return lost
		}
		return nil
	}, nil, &approval)
	if !reached || !errors.Is(err, lost) || owner != nil && !owner.closed {
		t.Fatal("local acknowledgement fault missed completed custody", reached, err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	before := bootstrapSuccessorPreparationTestFiles(t, f.original.config.RunDirectory)
	var output, diagnostic bytes.Buffer
	args := append(append(append([]string{}, f.paths...), f.approvalArgs...), "--local-rebind-approval", ref.Path, "--local-rebind-approval-sha256", ref.Sha256)
	if code := f.original.command(t.Context(), "contract-successor-execution-resume", &output, &diagnostic, args...); code != 0 {
		t.Fatal("lost local receipt acknowledgement cannot resume publicly", code, diagnostic.String())
	}
	if !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.original.config.RunDirectory)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, f.approval.Plan.Request.RegistryDirectory)) {
		t.Fatal("local lost acknowledgement recovery rewrote completed custody")
	}
}

// A passive preview capability cannot be reused as an exclusive writer, even
// when its original plan and named physical directory happen to match.
func TestBootstrapSuccessorLocalInspectionCannotBorrowWriterAuthority(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	path := owner.local.path
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	before := bootstrapSuccessorPreparationTestFiles(t, path)
	plan := f.approval.Plan.Review.Preparation.Approval.Plan
	inspection := &bootstrapSuccessorLocalInspection{preparationHash: rootObjectHash(plan), physical: plan.Root}
	reader, _, err := openBootstrapSuccessorPreparationReaderRebound(f.storage.Context, plan, true, nil, inspection)
	if err == nil || reader != nil || !strings.Contains(err.Error(), "exact owner authority") {
		t.Fatal("unsigned local review borrowed exclusive custody", err)
	}
	if !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, path)) {
		t.Fatal("unsigned inspection changed retained custody")
	}
}

// The common receipt census rejects a correctly named local receipt unless
// this owner independently authenticated its exact original-approver approval.
func TestBootstrapSuccessorUnapprovedLocalReceiptCannotAcquireAuthority(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	name := bootstrapSuccessorExecutionPrefix + "-local-rebind.json"
	if err := owner.local.publish(name, "local-rebind", []byte(`{"schema":"synthetic-unapproved-local-rebind"}`)); err != nil {
		t.Fatal(err)
	}
	path, registry := owner.local.path, owner.registry.path
	before, nonces := bootstrapSuccessorPreparationTestFiles(t, path), bootstrapSuccessorPreparationTestFiles(t, registry)
	if err := owner.checkpoint("synthetic-unapproved-local-receipt"); err == nil || !strings.Contains(err.Error(), "unexpected custody file") {
		t.Fatal("local filename became independent rebind authority", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	opened, err := openBootstrapSuccessorExecutionStore(f.storage.Context, f.approval.Plan, f.approval, f.profile, false, nil)
	if err == nil || opened != nil && !opened.closed {
		t.Fatal("replay adopted unapproved local physical authority", err)
	}
	if !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, path)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, registry)) {
		t.Fatal("unapproved local receipt refusal changed original custody")
	}
}
