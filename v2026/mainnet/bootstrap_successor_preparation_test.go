// Local-store regressions use synthetic retained claims only. The separate
// public-command fixture rebuilds and authenticates genuine original v3 custody.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// A bounded structurally valid model isolates local publication mechanics. It
// is not accepted by the public loader, which requires the original journals.
func newBootstrapSuccessorPreparationTestApproval(t *testing.T) (bootstrapSuccessorPreparationApproval, ed25519.PrivateKey) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	prepareBootstrapSuccessorMembersTest(t, directory, false)
	root, err := bootstrapSuccessorPhysicalRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	seed := sha256.Sum256([]byte("synthetic successor preparation approval"))
	key := ed25519.NewKeyFromSeed(seed[:])
	seal := rootObjectHash("synthetic retained custody")
	transaction := "0x" + strings.Repeat("19", 32)
	proposal := bootstrapContractSuccessorProposal{Schema: bootstrapContractSuccessorProposalSchema, Status: "unsigned-proposal-prerequisites-unresolved",
		RequestSha256: seal, OriginalConfigHash: seal, OriginalCustodyId: "synthetic-original-custody", OriginalRunDirectory: directory,
		Request: bootstrapContractSuccessorRequest{Schema: bootstrapContractSuccessorRequestSchema, SuccessorId: "synthetic-preparation",
			BootstrapPlanHash: seal, OriginalContractPlanHash: seal, AdditionalMaximumAttempts: 2, AdditionalMaximumWei: "200"},
		LocalPreparation:  &bootstrapChainReadinessState{PreparationHash: seal, ContractsHash: seal, RootProgressHash: seal, RootCustodyHash: seal, RootServiceHash: seal, ContractTransactionHash: transaction},
		UnfinishedActions: []string{"evidence-anchor"},
		Budget: bootstrapContractSuccessorBudget{OriginalMaximumAttempts: 8, RetainedAttempts: 8, UnfinishedActionCount: 1, AdditionalMaximumAttempts: 2,
			ProposedMaximumCumulativeAttempts: 10, ProposedRemainingAttempts: 2, RetryMarginAttempts: 1, OriginalMaximumWei: "1000",
			CompletedEnvelopeReservationWei: "800", UnexecutedEnvelopeReservationWei: "100", AdditionalMaximumWei: "200", ProposedMaximumLifetimeWei: "1200"}}
	for _, id := range []string{"reserve-create", "vault-create", "coordinator-create", "escrow-register", "proxy-create", "reserve-link", "vault-link", "evidence-create"} {
		proposal.AdoptedActions = append(proposal.AdoptedActions, bootstrapChainContractAction{Id: id, Approved: true, SemanticsVerified: true, ExecutorImplemented: true,
			CustodyStatus: "retained-complete", JournalHash: seal, CustodyHash: seal, TransactionHash: transaction, Attempts: 1, ReceiptObservation: "retained",
			Receipt: &evmCreateReceipt{Status: 1, TransactionHash: transaction}})
	}
	plan := bootstrapSuccessorPreparationPlan{Schema: bootstrapSuccessorPreparationSchema,
		OriginalConfig:    planFileReference{Path: filepath.Join(directory, "synthetic-config.json"), Sha256: seal},
		Request:           planFileReference{Path: filepath.Join(directory, "synthetic-request.json"), Sha256: seal},
		ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), Root: root, Proposal: proposal}
	return bootstrapSuccessorPreparationTestSign(t, plan, key), key
}

// Persistent fixtures declare the existing physical root before the first
// claim. Reopens keep this reference, including missing-member fault cases.
func newBootstrapSuccessorPreparationStorageTestApproval(t *testing.T) (bootstrapSuccessorPreparationApproval, ed25519.PrivateKey, *durablefixture.Fixture) {
	t.Helper()
	approval, key := newBootstrapSuccessorPreparationTestApproval(t)
	storage := durablefixture.New(t, t.Context(), approval.Plan.Proposal.OriginalRunDirectory)
	return approval, key, storage
}

// Only synthetic test keys sign the separate local preparation domain.
func bootstrapSuccessorPreparationTestSign(t *testing.T, plan bootstrapSuccessorPreparationPlan, key ed25519.PrivateKey) bootstrapSuccessorPreparationApproval {
	t.Helper()
	plan.Proposal.ContentHash = ""
	plan.Proposal.ContentHash = rootObjectHash(plan.Proposal)
	message, err := plan.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	return bootstrapSuccessorPreparationApproval{Schema: bootstrapSuccessorPreparationEnvelopeSchema, Plan: plan, Signature: hex.EncodeToString(ed25519.Sign(key, message))}
}

// Mutations own all nested pointers and slices rather than changing the oracle.
func copyBootstrapSuccessorPreparationTestApproval(t *testing.T, approval bootstrapSuccessorPreparationApproval) bootstrapSuccessorPreparationApproval {
	t.Helper()
	raw, err := json.Marshal(approval)
	if err != nil {
		t.Fatal(err)
	}
	var copied bootstrapSuccessorPreparationApproval
	if err := decodePlanJson(raw, &copied); err != nil {
		t.Fatal(err)
	}
	return copied
}

// Exact namespace snapshots detect both lost claims and unexpected new files.
func bootstrapSuccessorPreparationTestFiles(t *testing.T, directory string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = string(raw)
	}
	return files
}

// The original approval key is independently expected; domain, physical root,
// complete retained prefix and proposed cumulative floors all enter the bytes.
func TestBootstrapSuccessorPreparationBindsIndependentDomainAndScope(t *testing.T) {
	approval, key := newBootstrapSuccessorPreparationTestApproval(t)
	message, err := approval.Plan.signingBytes()
	if err != nil || !bytes.HasPrefix(message, []byte(bootstrapSuccessorPreparationApprovalSchema+"\x00")) {
		t.Fatal("preparation lacks its independent approval domain", err)
	}
	if err := approval.validate(approval.Plan); err != nil {
		t.Fatal(err)
	}
	otherSeed := sha256.Sum256([]byte("synthetic unrelated preparation approver"))
	otherKey := ed25519.NewKeyFromSeed(otherSeed[:])
	cases := []struct {
		name   string
		change func(*bootstrapSuccessorPreparationApproval)
	}{
		{name: "original domain", change: func(a *bootstrapSuccessorPreparationApproval) {
			a.Signature = hex.EncodeToString(ed25519.Sign(key, append([]byte(evmPhaseApprovalSchema+"\x00"), message[len(bootstrapSuccessorPreparationApprovalSchema)+1:]...)))
		}},
		{name: "unsigned", change: func(a *bootstrapSuccessorPreparationApproval) { a.Signature = "" }},
		{name: "different signer", change: func(a *bootstrapSuccessorPreparationApproval) {
			a.Signature = hex.EncodeToString(ed25519.Sign(otherKey, message))
		}},
		{name: "self supplied key", change: func(a *bootstrapSuccessorPreparationApproval) {
			a.Plan.ApprovalPublicKey = "0x" + hex.EncodeToString(otherKey.Public().(ed25519.PublicKey))
			*a = bootstrapSuccessorPreparationTestSign(t, a.Plan, otherKey)
		}},
		{name: "root", change: func(a *bootstrapSuccessorPreparationApproval) { a.Plan.Root.Inode++ }},
		{name: "original seal", change: func(a *bootstrapSuccessorPreparationApproval) {
			a.Plan.Proposal.LocalPreparation.RootCustodyHash = rootObjectHash("different custody")
		}},
		{name: "receipt", change: func(a *bootstrapSuccessorPreparationApproval) { a.Plan.Proposal.AdoptedActions[7].Receipt.GasUsed++ }},
		{name: "attempt floor", change: func(a *bootstrapSuccessorPreparationApproval) { a.Plan.Proposal.Budget.RetainedAttempts-- }},
		{name: "lifetime floor", change: func(a *bootstrapSuccessorPreparationApproval) {
			a.Plan.Proposal.Budget.ProposedMaximumLifetimeWei = "200"
		}},
		{name: "new path", change: func(a *bootstrapSuccessorPreparationApproval) { a.Plan.Proposal.OriginalRunDirectory += "-copy" }},
	}
	for _, c := range cases {
		changed := copyBootstrapSuccessorPreparationTestApproval(t, approval)
		c.change(&changed)
		if err := changed.validate(approval.Plan); err == nil {
			t.Fatalf("preparation accepted changed independent authority or scope: %s", c.name)
		}
	}
}

// Even a resealed structural proposal cannot reset original counted attempts,
// omit reservation floors or promote this local domain to execution authority.
func TestBootstrapSuccessorPreparationConservesOriginalFloors(t *testing.T) {
	approval, _ := newBootstrapSuccessorPreparationTestApproval(t)
	cases := []struct {
		name   string
		change func(*bootstrapSuccessorPreparationPlan)
	}{
		{name: "retained", change: func(p *bootstrapSuccessorPreparationPlan) {
			p.Proposal.Budget.RetainedAttempts = 0
			p.Proposal.Budget.ProposedRemainingAttempts = 10
			p.Proposal.Budget.RetryMarginAttempts = 9
		}},
		{name: "cumulative", change: func(p *bootstrapSuccessorPreparationPlan) {
			p.Proposal.Budget.ProposedMaximumCumulativeAttempts = 2
			p.Proposal.Budget.ProposedRemainingAttempts = 2
		}},
		{name: "lifetime", change: func(p *bootstrapSuccessorPreparationPlan) { p.Proposal.Budget.ProposedMaximumLifetimeWei = "200" }},
		{name: "reservation", change: func(p *bootstrapSuccessorPreparationPlan) { p.Proposal.Budget.CompletedEnvelopeReservationWei = "1001" }},
		{name: "unexecuted reservation", change: func(p *bootstrapSuccessorPreparationPlan) { p.Proposal.Budget.UnexecutedEnvelopeReservationWei = "201" }},
		{name: "missing attempt", change: func(p *bootstrapSuccessorPreparationPlan) { p.Proposal.AdoptedActions[7].Attempts = 0 }},
		{name: "action order", change: func(p *bootstrapSuccessorPreparationPlan) {
			p.Proposal.AdoptedActions[6], p.Proposal.AdoptedActions[7] = p.Proposal.AdoptedActions[7], p.Proposal.AdoptedActions[6]
		}},
		{name: "request cap", change: func(p *bootstrapSuccessorPreparationPlan) { p.Proposal.Request.AdditionalMaximumAttempts++ }},
		{name: "missing preparation", change: func(p *bootstrapSuccessorPreparationPlan) { p.Proposal.LocalPreparation.RootCustodyHash = "" }},
		{name: "execution", change: func(p *bootstrapSuccessorPreparationPlan) { p.Proposal.Executable = true }},
		{name: "safe authority", change: func(p *bootstrapSuccessorPreparationPlan) { p.Proposal.SafeAuthorityVerified = true }},
	}
	for _, c := range cases {
		changed := copyBootstrapSuccessorPreparationTestApproval(t, approval).Plan
		c.change(&changed)
		changed.Proposal.ContentHash = ""
		changed.Proposal.ContentHash = rootObjectHash(changed.Proposal)
		if err := changed.validate(); err == nil {
			t.Fatalf("preparation discarded original additive floor or gained authority: %s", c.name)
		}
	}
}

// The fixed claim is prepared once, survives process-owner close, and cannot
// silently be replaced by a second independently signed proposed cap increase.
func TestBootstrapSuccessorPreparationClaimsOnceAndResumesSameRoot(t *testing.T) {
	approval, key, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	directory := approval.Plan.Proposal.OriginalRunDirectory
	store, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	before := bootstrapSuccessorPreparationTestFiles(t, directory)
	if len(before) != 3 || before[bootstrapSuccessorPreparationFile+".lock"] != rootObjectHash(approval)+"\n"+bootstrapRootClaimComplete {
		t.Fatal("preparation did not publish exactly one complete fixed claim")
	}
	var record bootstrapSuccessorPreparationRecord
	if err := decodePlanJson([]byte(before[bootstrapSuccessorPreparationFile]), &record); err != nil {
		t.Fatal(err)
	}
	if err := record.validate(approval); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		store, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
		if err != nil {
			t.Fatal("same-root ordinary restart lost retained preparation", err)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
	}
	changed := copyBootstrapSuccessorPreparationTestApproval(t, approval).Plan
	changed.Proposal.Request.AdditionalMaximumAttempts++
	changed.Proposal.Budget.AdditionalMaximumAttempts++
	changed.Proposal.Budget.ProposedMaximumCumulativeAttempts++
	changed.Proposal.Budget.ProposedRemainingAttempts++
	changed.Proposal.Budget.RetryMarginAttempts++
	replacement := bootstrapSuccessorPreparationTestSign(t, changed, key)
	for _, candidate := range []bootstrapSuccessorPreparationApproval{approval, replacement} {
		for _, create := range []bool{true, false} {
			if !create && rootObjectHash(candidate) == rootObjectHash(approval) {
				continue
			}
			store, err := openBootstrapSuccessorPreparationStore(storage.Context, candidate.Plan, candidate, create, nil)
			if store != nil {
				store.close()
			}
			if err == nil {
				t.Fatal("fixed preparation admitted a duplicate or revised claimant")
			}
		}
	}
	if !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, directory)) {
		t.Fatal("resume or revised cap changed immutable preparation custody")
	}
}

// Each hook deterministically interrupts before/after file fsync and publication.
// The hash-named stage keeps a single claimant even when its contents are empty.
func TestBootstrapSuccessorPreparationRecoversEveryPublicationBoundary(t *testing.T) {
	stages := []string{"claim-name-created", "claim-name-synced", "claim-stage-written", "claim-stage-synced", "claim-published", "claim-published-synced",
		"record-name-created", "record-name-synced", "record-stage-written", "record-stage-synced", "record-published", "record-published-synced", "record-retained", "complete-written", "complete-synced"}
	interrupted := errors.New("synthetic preparation interruption")
	for _, stage := range stages {
		approval, key, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
		reached := false
		store, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, func(current string) error {
			if current == stage {
				reached = true
				return interrupted
			}
			return nil
		})
		if store != nil {
			store.close()
		}
		if !reached || !errors.Is(err, interrupted) {
			t.Fatalf("preparation hid interruption at %s: %v", stage, err)
		}
		directory := approval.Plan.Proposal.OriginalRunDirectory
		before := bootstrapSuccessorPreparationTestFiles(t, directory)
		if len(before) == 0 {
			t.Fatalf("interruption discarded claimant at %s", stage)
		}
		changed := copyBootstrapSuccessorPreparationTestApproval(t, approval).Plan
		changed.Proposal.Request.SuccessorId = "synthetic-competing-preparation"
		competitor := bootstrapSuccessorPreparationTestSign(t, changed, key)
		for _, attempt := range []struct {
			approval bootstrapSuccessorPreparationApproval
			create   bool
		}{
			{approval: competitor, create: false}, {approval: competitor, create: true}, {approval: approval, create: true},
		} {
			store, err := openBootstrapSuccessorPreparationStore(storage.Context, attempt.approval.Plan, attempt.approval, attempt.create, nil)
			if store != nil {
				store.close()
			}
			if err == nil || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, directory)) {
				t.Fatalf("interrupted claim admitted replacement at %s", stage)
			}
		}
		store, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
		if err != nil {
			t.Fatalf("exact interrupted preparation could not resume at %s: %v", stage, err)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		after := bootstrapSuccessorPreparationTestFiles(t, directory)
		if len(after) != 3 || after[bootstrapSuccessorPreparationFile+".lock"] != rootObjectHash(approval)+"\n"+bootstrapRootClaimComplete {
			t.Fatalf("resumed preparation did not retain one completed claim at %s", stage)
		}
		var record bootstrapSuccessorPreparationRecord
		if err := decodePlanJson([]byte(after[bootstrapSuccessorPreparationFile]), &record); err != nil {
			t.Fatal(err)
		}
		if err := record.validate(approval); err != nil {
			t.Fatalf("resumed approval changed at %s: %v", stage, err)
		}
	}
}

// Prefix recovery never repairs unknown corruption. A partially appended
// complete marker requires its already-published record rather than recreation.
func TestBootstrapSuccessorPreparationRecoversOnlyExactPartialBytes(t *testing.T) {
	for _, kind := range []string{"claim", "record"} {
		for _, corruption := range []bool{false, true} {
			approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
			store, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, func(stage string) error {
				if stage == kind+"-stage-written" {
					return errors.New("synthetic partial write")
				}
				return nil
			})
			if store != nil || err == nil {
				t.Fatal("partial fixture did not interrupt")
			}
			directory := approval.Plan.Proposal.OriginalRunDirectory
			stage := (&bootstrapSuccessorPreparationStore{approval: approval}).stageName(kind)
			path := filepath.Join(directory, stage)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			raw = raw[:len(raw)/2]
			if corruption {
				raw[0] ^= 1
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			before := bootstrapSuccessorPreparationTestFiles(t, directory)
			store, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
			if store != nil {
				store.close()
			}
			if corruption {
				if err == nil || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, directory)) {
					t.Fatal("preparation repaired an unknown partial stage")
				}
			} else if err != nil {
				t.Fatal("preparation lost its exact partial prefix", err)
			}
		}
	}
	for _, missingRecord := range []bool{false, true} {
		approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
		store, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, func(stage string) error {
			if stage == "complete-written" {
				return errors.New("synthetic partial completion")
			}
			return nil
		})
		if store != nil || err == nil {
			t.Fatal("completion fixture did not interrupt")
		}
		directory := approval.Plan.Proposal.OriginalRunDirectory
		if err := os.WriteFile(filepath.Join(directory, bootstrapSuccessorPreparationFile+".lock"), []byte(rootObjectHash(approval)+"\ncl"), 0600); err != nil {
			t.Fatal(err)
		}
		if missingRecord {
			if err := os.Remove(filepath.Join(directory, bootstrapSuccessorPreparationFile)); err != nil {
				t.Fatal(err)
			}
		}
		before := bootstrapSuccessorPreparationTestFiles(t, directory)
		store, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
		if store != nil {
			store.close()
		}
		if missingRecord {
			if err == nil || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, directory)) {
				t.Fatal("partial completion recreated its lost record")
			}
		} else if err != nil {
			t.Fatal("partial completion could not resume its exact retained record", err)
		}
	}
}

// Losing completed evidence cannot reset the fixed claim, even when its marker
// is still valid. Unknown marker or record bytes remain unchanged for review.
func TestBootstrapSuccessorPreparationRefusesMissingOrReboundCustody(t *testing.T) {
	for _, fault := range []string{"record-missing", "record-empty", "record-malformed", "record-rebound", "marker-missing", "marker-empty", "marker-short", "marker-extra", "unknown-stage"} {
		approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
		store, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		directory := approval.Plan.Proposal.OriginalRunDirectory
		recordPath, markerPath := filepath.Join(directory, bootstrapSuccessorPreparationFile), filepath.Join(directory, bootstrapSuccessorPreparationFile+".lock")
		switch fault {
		case "record-missing":
			err = os.Remove(recordPath)
		case "record-empty":
			err = os.WriteFile(recordPath, nil, 0600)
		case "record-malformed":
			err = os.WriteFile(recordPath, []byte("{"), 0600)
		case "record-rebound":
			var record bootstrapSuccessorPreparationRecord
			raw, readErr := os.ReadFile(recordPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if err := decodePlanJson(raw, &record); err != nil {
				t.Fatal(err)
			}
			record.Approval.Plan.Proposal.Budget.RetainedAttempts = 0
			record.ContentHash = ""
			record.ContentHash = rootObjectHash(record)
			raw, err = json.Marshal(record)
			if err == nil {
				err = os.WriteFile(recordPath, raw, 0600)
			}
		case "marker-missing":
			err = os.Remove(markerPath)
		case "marker-empty":
			err = os.WriteFile(markerPath, nil, 0600)
		case "marker-short":
			err = os.WriteFile(markerPath, []byte(rootObjectHash(approval)[:20]), 0600)
		case "marker-extra":
			err = os.WriteFile(markerPath, []byte(rootObjectHash(approval)+"\n"+bootstrapRootClaimComplete+"x"), 0600)
		case "unknown-stage":
			err = os.WriteFile(filepath.Join(directory, bootstrapSuccessorStagePrefix+strings.Repeat("0", 64)+".claim"), nil, 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		before := bootstrapSuccessorPreparationTestFiles(t, directory)
		for _, create := range []bool{false, true} {
			store, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, create, nil)
			if store != nil {
				store.close()
			}
			if err == nil || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, directory)) {
				t.Fatalf("preparation reset or repaired retained %s", fault)
			}
		}
	}
}

// An exact byte clone at the same pathname has a different signed inode. Moving
// the real directory to an alternate path also requires explicit new approval.
func TestBootstrapSuccessorPreparationRejectsCopiedAndMovedPhysicalRoot(t *testing.T) {
	approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	directory := approval.Plan.Proposal.OriginalRunDirectory
	store, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	before := bootstrapSuccessorPreparationTestFiles(t, directory)
	moved := directory + "-moved"
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(moved) })
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for name, raw := range before {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
	if store != nil {
		store.close()
	}
	if err == nil || !strings.Contains(err.Error(), "physical root") || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, directory)) {
		t.Fatal("copied directory satisfied the original signed physical identity", err)
	}
	changed := copyBootstrapSuccessorPreparationTestApproval(t, approval).Plan
	changed.Proposal.OriginalRunDirectory = moved
	changed.Proposal.ContentHash = ""
	changed.Proposal.ContentHash = rootObjectHash(changed.Proposal)
	store, err = openBootstrapSuccessorPreparationStore(storage.Context, changed, approval, false, nil)
	if store != nil {
		store.close()
	}
	if err == nil {
		t.Fatal("alternate path inherited the signed preparation")
	}
	if err := os.RemoveAll(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, directory); err != nil {
		t.Fatal(err)
	}
	store, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
	if err != nil {
		t.Fatal("restoring the same original inode lost ordinary recovery", err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
}

// Directory replacement at a publication hook cannot redirect writes to the
// new path. The original descriptor's staged evidence stays in the moved root.
func TestBootstrapSuccessorPreparationFencesRootReplacementDuringPublication(t *testing.T) {
	approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	directory := approval.Plan.Proposal.OriginalRunDirectory
	moved := directory + "-replaced"
	t.Cleanup(func() { os.RemoveAll(moved) })
	reached := false
	store, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, func(stage string) error {
		if stage != "claim-stage-synced" {
			return nil
		}
		reached = true
		if err := os.Rename(directory, moved); err != nil {
			return err
		}
		return os.Mkdir(directory, 0700)
	})
	if store != nil {
		store.close()
	}
	if !reached || !errors.Is(err, durablevolume.ErrIdentity) || len(bootstrapSuccessorPreparationTestFiles(t, directory)) != 0 {
		t.Fatal("publication followed a replaced physical root", err)
	}
	if len(bootstrapSuccessorPreparationTestFiles(t, moved)) != 2 {
		t.Fatal("root replacement lost the original staged claim")
	}
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, directory); err != nil {
		t.Fatal(err)
	}
	store, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
}

// The first owner holds an explicit barrier before publication; a contender
// cannot enter the same directory even before the fixed marker exists.
func TestBootstrapSuccessorPreparationSerializesBeforeClaimPublication(t *testing.T) {
	approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	acquired, release := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		store, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, func(stage string) error {
			if stage == "owner-acquired" {
				close(acquired)
				<-release
			}
			return nil
		})
		finished <- errors.Join(err, store.close())
	}()
	select {
	case <-acquired:
	case err := <-finished:
		t.Fatal("first preparation could not reach ownership barrier", err)
	}
	store, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, nil)
	if store != nil {
		store.close()
	}
	close(release)
	firstErr := <-finished
	if err == nil || !strings.Contains(err.Error(), "already has a local owner") || firstErr != nil {
		t.Fatal("competing preparation escaped prepublication ownership", err, firstErr)
	}
}

// A fixed name that appears after the census must not be overwritten by stage
// publication. The no-replace rename preserves both competing pieces of evidence.
func TestBootstrapSuccessorPreparationNeverOverwritesPublicationCollision(t *testing.T) {
	for _, kind := range []string{"claim", "record"} {
		approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
		directory := approval.Plan.Proposal.OriginalRunDirectory
		var retained map[string]string
		store, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, func(stage string) error {
			if stage != kind+"-stage-synced" {
				return nil
			}
			name := bootstrapSuccessorPreparationFile
			if kind == "claim" {
				name += ".lock"
			}
			if err := os.WriteFile(filepath.Join(directory, name), []byte("synthetic competing retained evidence"), 0600); err != nil {
				return err
			}
			retained = bootstrapSuccessorPreparationTestFiles(t, directory)
			return nil
		})
		if store != nil {
			store.close()
		}
		if retained == nil || err == nil || !strings.Contains(err.Error(), "publication refused an existing fixed name") || !maps.Equal(retained, bootstrapSuccessorPreparationTestFiles(t, directory)) {
			t.Fatalf("preparation overwrote a fixed-name publication collision: %s %v", kind, err)
		}
	}
}

// Files with shared permissions, links, unknown staging or excessive namespace
// size cannot turn an interrupted or completed claim into reusable allowance.
func TestBootstrapSuccessorPreparationRejectsUnsafeFilesAndBounds(t *testing.T) {
	for _, fault := range []string{"marker-mode", "record-mode", "marker-symlink", "record-hardlink", "stage-hardlink", "stage-symlink", "namespace-bound"} {
		approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
		directory := approval.Plan.Proposal.OriginalRunDirectory
		stageFault := strings.HasPrefix(fault, "stage-")
		store, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, func(stage string) error {
			if stageFault && stage == "claim-name-synced" {
				return errors.New("synthetic unsafe stage")
			}
			return nil
		})
		if stageFault && err == nil || !stageFault && err != nil {
			t.Fatal("unsafe fixture preparation", err)
		}
		if store != nil {
			store.close()
		}
		path := filepath.Join(directory, bootstrapSuccessorPreparationFile)
		if strings.HasPrefix(fault, "marker-") {
			path += ".lock"
		}
		if stageFault {
			path = filepath.Join(directory, (&bootstrapSuccessorPreparationStore{approval: approval}).stageName("claim"))
		}
		switch {
		case strings.HasSuffix(fault, "-mode"):
			err = os.Chmod(path, 0644)
		case strings.HasSuffix(fault, "-hardlink"):
			err = os.Link(path, filepath.Join(t.TempDir(), "synthetic-linked-custody"))
		case strings.HasSuffix(fault, "-symlink"):
			target := filepath.Join(t.TempDir(), "synthetic-marker")
			err = os.Rename(path, target)
			if err == nil {
				err = os.Symlink(target, path)
			}
		case fault == "namespace-bound":
			for i := range 513 {
				if err := os.WriteFile(filepath.Join(directory, hex.EncodeToString([]byte{byte(i / 256), byte(i % 256)})), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		before := bootstrapSuccessorPreparationTestFiles(t, directory)
		store, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
		if store != nil {
			store.close()
		}
		if err == nil || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, directory)) {
			t.Fatalf("preparation accepted unsafe retained %s", fault)
		}
	}
	approval, key, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	ctx, cancel := context.WithCancel(storage.Context)
	cancel()
	store, err := openBootstrapSuccessorPreparationStore(ctx, approval.Plan, approval, true, nil)
	if store != nil {
		store.close()
	}
	if !errors.Is(err, context.Canceled) || len(bootstrapSuccessorPreparationTestFiles(t, approval.Plan.Proposal.OriginalRunDirectory)) != 0 {
		t.Fatal("canceled preparation created custody", err)
	}
	store, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, false, nil)
	if store != nil {
		store.close()
	}
	if err == nil {
		t.Fatal("missing preparation resume created a fresh claimant")
	}
	// The envelope can fit while the journal's additional seal does not. Its
	// complete serialization must be admitted before publishing even a claim.
	plan := approval.Plan
	plan.Proposal.RequiredPrerequisites = []string{""}
	bounded := bootstrapSuccessorPreparationTestSign(t, plan, key)
	raw, err := json.Marshal(bounded)
	if err != nil {
		t.Fatal(err)
	}
	plan.Proposal.RequiredPrerequisites[0] = strings.Repeat("x", maximumBootstrapSuccessorPreparationBytes-1-len(raw))
	bounded = bootstrapSuccessorPreparationTestSign(t, plan, key)
	store, err = openBootstrapSuccessorPreparationStore(storage.Context, bounded.Plan, bounded, true, nil)
	if store != nil {
		store.close()
	}
	if err == nil || !strings.Contains(err.Error(), "record exceeds its byte bound") || len(bootstrapSuccessorPreparationTestFiles(t, approval.Plan.Proposal.OriginalRunDirectory)) != 0 {
		t.Fatal("oversized record left an unrecoverable new claim", err)
	}
}
