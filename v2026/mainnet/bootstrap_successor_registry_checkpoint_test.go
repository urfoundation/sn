//go:build linux

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

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The acknowledgement is lost after the actual immutable receipt and member
// checkpoint are synced. Public resume must keep those bytes and original
// nonces, without treating its authenticated receipt as an unknown event.
func TestBootstrapSuccessorRegistryRebindLostAcknowledgementKeepsPublicResume(t *testing.T) {
	f := newBootstrapSuccessorCanonicalFixture(t)
	path, hash := registryRebindTestRestore(t, f)
	ctx := f.original.storageContext(t.Context())
	plan, err := buildBootstrapSuccessorRegistryRebind(ctx, f.approval, f.profile, durablevolume.Reference{Path: path, Sha256: hash})
	if err != nil {
		t.Fatal(err)
	}
	message, err := plan.signingBytes(f.approval, f.profile)
	if err != nil {
		t.Fatal(err)
	}
	approval := bootstrapSuccessorRegistryRebindApproval{Schema: bootstrapSuccessorRegistryRebindEnvelopeSchema, Plan: plan,
		Signature: hex.EncodeToString(ed25519.Sign(f.key, message))}
	ref := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(path), "synthetic-retained-rebind.json"), approval)
	local, registry := f.original.config.RunDirectory, f.approval.Plan.Request.RegistryDirectory
	nonces := bootstrapSuccessorPreparationTestFiles(t, registry)
	retained, err := openBootstrapChainReadinessState(ctx, f.original.preparation)
	if err != nil {
		t.Fatal(err)
	}
	lostAck := errors.New("synthetic lost completed rebind acknowledgement")
	reached := false
	owner, openErr := openBootstrapSuccessorExecutionStoreWithRegistryRebind(ctx, f.approval.Plan, f.approval, f.profile, false, func(stage string) error {
		if stage == bootstrapSuccessorRegistryRebindFile+":published-synced" {
			reached = true
			return lostAck
		}
		return nil
	}, &approval)
	if !reached || !errors.Is(openErr, lostAck) || owner != nil && !owner.closed {
		t.Fatal("receipt acknowledgement fault missed its real completed boundary", reached, openErr)
	}
	if err := errors.Join(owner.close(), retained.close()); err != nil {
		t.Fatal(err)
	}
	completed := bootstrapSuccessorPreparationTestFiles(t, local)
	expected, err := json.Marshal(approval)
	if err != nil || completed[bootstrapSuccessorRegistryRebindFile] != string(expected) {
		t.Fatal("lost acknowledgement did not retain exact independently approved receipt", err)
	}
	extra := append(append(append([]string{}, f.paths...), f.approvalArgs...), "--registry-rebind-approval", ref.Path, "--registry-rebind-approval-sha256", ref.Sha256)
	var output, diagnostic bytes.Buffer
	if code := f.original.command(t.Context(), "contract-successor-execution-resume", &output, &diagnostic, extra...); code != 0 {
		t.Fatal("public retained rebind cannot reopen after lost acknowledgement", code, diagnostic.String())
	}
	var result bootstrapSuccessorExecutionResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.CumulativeAttempts != 8 || result.SubmissionAttempted || result.ActivationReady {
		t.Fatal("rebind recovery changed original allowance or execution authority", err)
	}
	if !maps.Equal(completed, bootstrapSuccessorPreparationTestFiles(t, local)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, registry)) {
		t.Fatal("lost acknowledgement recovery rewrote completed custody")
	}
	// Even another fully retained file with this prefix is not in the exact
	// authenticated receipt inventory. Neither live checkpoint nor reopening
	// may turn its name into approval authority.
	retained, err = openBootstrapChainReadinessState(ctx, f.original.preparation)
	if err != nil {
		t.Fatal(err)
	}
	owner, err = openBootstrapSuccessorExecutionStoreWithRegistryRebind(ctx, f.approval.Plan, f.approval, f.profile, false, nil, &approval)
	if err != nil {
		t.Fatal(err)
	}
	foreignName := bootstrapSuccessorExecutionPrefix + "-registry-rebind-foreign.json"
	if err := owner.local.publish(foreignName, "registry-rebind", expected); err != nil {
		t.Fatal(err)
	}
	foreign := bootstrapSuccessorPreparationTestFiles(t, local)
	if err := owner.checkpoint("synthetic-foreign-retained-receipt"); err == nil || !strings.Contains(err.Error(), "unexpected custody file") {
		t.Fatal("live checkpoint treated a foreign receipt prefix as authority", err)
	}
	if err := errors.Join(owner.close(), retained.close()); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	diagnostic.Reset()
	if code := f.original.command(t.Context(), "contract-successor-execution-resume", &output, &diagnostic, extra...); code != 1 || output.Len() != 0 {
		t.Fatal("public reopen admitted a foreign retained receipt", code, diagnostic.String())
	}
	if !maps.Equal(foreign, bootstrapSuccessorPreparationTestFiles(t, local)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, registry)) {
		t.Fatal("foreign receipt refusal repaired custody or released a nonce")
	}
}

// A correct fixed filename without separately authenticated rebind approval
// grants no exception. This adjacent control uses actual retained publication
// so the byte/inode guard alone cannot be the reason it refuses.
func TestBootstrapSuccessorUnapprovedRebindReceiptCannotAcquireAuthority(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	name := bootstrapSuccessorExecutionPrefix + "-registry-rebind.json"
	forged := []byte(`{"schema":"synthetic-unapproved-registry-rebind","signature_ed25519":"forged"}`)
	if err := owner.local.publish(name, "registry-rebind", forged); err != nil {
		t.Fatal(err)
	}
	local, registry := owner.local.path, owner.registry.path
	before, nonces := bootstrapSuccessorPreparationTestFiles(t, local), bootstrapSuccessorPreparationTestFiles(t, registry)
	if err := owner.checkpoint("synthetic-unapproved-rebind-receipt"); err == nil || !strings.Contains(err.Error(), "unexpected custody file") {
		t.Fatal("fixed receipt name bypassed independent authority", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	ctx := f.storage.Context
	reopened, err := openBootstrapSuccessorExecutionStore(ctx, f.approval.Plan, f.approval, f.profile, false, nil)
	if err == nil || reopened != nil && !reopened.closed {
		t.Fatal("replay adopted unapproved physical authority", err)
	}
	if err := reopened.close(); err != nil {
		t.Fatal(err)
	}
	raw, readErr := os.ReadFile(filepath.Join(local, name))
	if readErr != nil || !bytes.Equal(raw, forged) || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, local)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, registry)) || len(f.writes) != 0 {
		t.Fatal("unapproved receipt refusal changed retained history or sent", readErr)
	}
}
