// Model tests isolate exact digest and budget boundaries; a separate public
// command fixture authenticates the actual v3 journals and original approvals.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Synthetic local preparation retains both completed and unexecuted envelope
// floors; its public-command counterpart reconstructs these from genuine files.
func bootstrapSuccessorSafeTestInputs(t *testing.T, version, variant string) (bootstrapSuccessorPreparationApproval, ed25519.PrivateKey, bootstrapSuccessorSafeRequest, planFileReference, []byte) {
	t.Helper()
	approval, key := newBootstrapSuccessorPreparationTestApproval(t)
	p := &approval.Plan.Proposal
	p.Request.IntendedOwnerSafe = common.BytesToAddress(crypto.Keccak256([]byte("synthetic Safe proxy")))
	p.Request.IntendedRelayer = common.BytesToAddress(crypto.Keccak256([]byte("synthetic review relayer")))
	p.Request.IntendedSafeNonce, p.Request.IntendedRelayerNonce = "17", 42
	p.Request.AdditionalMaximumWei = "200000"
	p.Anchor = bootstrapContractAnchorIntent{ExpectedOwner: p.Request.IntendedOwnerSafe,
		Coordinator: common.BytesToAddress(crypto.Keccak256([]byte("synthetic review coordinator"))),
		Evidence:    common.BytesToAddress(crypto.Keccak256([]byte("synthetic review evidence"))), ValueWei: "0"}
	p.Anchor.CallData = "0x" + hex.EncodeToString(stabi.NewSTCoordinator().PackFixValidatorEvidence(p.Anchor.Evidence))
	p.Budget.OriginalMaximumWei, p.Budget.AdditionalMaximumWei, p.Budget.ProposedMaximumLifetimeWei = "1000000", "200000", "1200000"
	p.Budget.CompletedEnvelopeReservationWei, p.Budget.UnexecutedEnvelopeReservationWei = "400000", "100000"
	for i := range p.AdoptedActions {
		p.AdoptedActions[i].Receipt.NativeNumber = uint64(101 + i)
		p.AdoptedActions[i].Receipt.NativeHash = crypto.Keccak256Hash([]byte{byte(i), 0x71}).Hex()
	}
	p.AdoptedActions[4].Receipt.ContractAddress = p.Anchor.Coordinator.Hex()
	p.AdoptedActions[7].Receipt.ContractAddress = p.Anchor.Evidence.Hex()
	approval = bootstrapSuccessorPreparationTestSign(t, approval.Plan, key)
	profile, archivePath, _ := safeReleaseTestInputs(t, version, variant)
	rawArchive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	record := bootstrapSuccessorSafeTestRecord(approval)
	request := bootstrapSuccessorSafeRequest{Schema: bootstrapSuccessorSafeRequestSchema, PreparationPlanHash: approval.Plan.hash(), PreparationRecordHash: record.ContentHash,
		Version: version, Variant: variant, Archive: planFileReference{Path: archivePath, Sha256: profile.ArchiveSha256},
		RelayerGas: 70000, RelayerFeeCapWei: "10", RelayerTipCapWei: "1", StartNativeNumber: 108,
		StartNativeHash: approval.Plan.Proposal.AdoptedActions[7].Receipt.NativeHash, ValidThroughNative: 208}
	reference := planFileReference{Path: filepath.Join(t.TempDir(), "synthetic-safe-review.json"), Sha256: rootObjectHash(request)}
	return approval, key, request, reference, rawArchive
}

// This record models the immutable bytes emitted by the local owner.
func bootstrapSuccessorSafeTestRecord(approval bootstrapSuccessorPreparationApproval) bootstrapSuccessorPreparationRecord {
	record := bootstrapSuccessorPreparationRecord{Schema: bootstrapSuccessorPreparationStateSchema, Approval: approval, Phase: "prepared-offline"}
	record.ContentHash = rootObjectHash(record)
	return record
}

// The maintained proxy bytecode independently computes the exact mainnet digest
// for every supported profile, using the retained anchor rather than a dummy call.
func TestBootstrapSuccessorSafeReviewMatchesPublishedAnchorDigest(t *testing.T) {
	for _, c := range safeExecutionTestProfiles {
		approval, _, request, reference, rawArchive := bootstrapSuccessorSafeTestInputs(t, c.version, c.variant)
		record := bootstrapSuccessorSafeTestRecord(approval)
		result, err := buildBootstrapSuccessorSafeReview(t.Context(), approval.Plan, record, request, reference, rawArchive)
		if err != nil {
			t.Fatal(c, err)
		}
		oracle := newSafeExecutionFixture(t, c.version, c.variant)
		anchor := approval.Plan.Proposal.Anchor
		transaction := safeExecutionTransaction{ChainId: big.NewInt(mainnetEvmChainId), Safe: anchor.ExpectedOwner, To: anchor.Coordinator,
			Value: new(big.Int), Data: stabi.NewSTCoordinator().PackFixValidatorEvidence(anchor.Evidence), SafeTxGas: new(big.Int), BaseGas: new(big.Int), GasPrice: new(big.Int), Nonce: big.NewInt(17)}
		if result.Transaction.Digest != oracle.oracleDigest(transaction) || result.Transaction.ChainId != 964 || result.Transaction.Safe != anchor.ExpectedOwner || result.Transaction.To != anchor.Coordinator ||
			result.Transaction.Data != anchor.CallData || result.Transaction.ValueWei != "0" || result.Transaction.Operation != 0 || result.Transaction.SafeTxGas != "0" || result.Transaction.BaseGas != "0" || result.Transaction.GasPrice != "0" ||
			result.Transaction.GasToken != (common.Address{}) || result.Transaction.RefundReceiver != (common.Address{}) || result.Transaction.Nonce != "17" {
			t.Fatalf("successor anchor digest differs from published %s/%s", c.version, c.variant)
		}
		preimage, err := hex.DecodeString(result.Transaction.Preimage[2:])
		if err != nil || len(preimage) != 66 || crypto.Keccak256Hash(preimage) != result.Transaction.Digest {
			t.Fatal("review lost exact Safe preimage", err)
		}
		if result.Release.Version != c.version || result.Release.Variant != c.variant || result.Release.ArchiveSha256 != request.Archive.Sha256 ||
			!result.Release.ArtifactIntegrityVerified || !result.Release.PublishedBuildInputsVerified || len(result.Release.Artifacts) != 2 ||
			result.Preparation.ContentHash != record.ContentHash || result.RequestReference != reference ||
			!result.PreparationApprovalVerified || !result.LocalPreparationComplete || !result.SafeDigestComputed {
			t.Fatal("review lost exact preparation or published provenance")
		}
		if result.ExecutionApprovalVerified || result.ApprovalSigningPayloadProvided || result.CurrentChainVerified || result.SafeAuthorityVerified || result.GlobalSigningCustodyVerified ||
			result.OriginalReservationsReconciled || result.SigningAuthorized || result.Executable || result.NetworkEffects || result.InstallationComplete || result.ActivationReady ||
			result.Relayer.CalldataComplete || result.Relayer.NonceReserved || result.Relayer.BudgetReserved || result.Release.CurrentChainVerified || result.Release.SafeAuthorityVerified {
			t.Fatal("offline digest or local preparation manufactured live authority")
		}
		seal := result.ContentHash
		result.ContentHash = ""
		if seal != rootObjectHash(result) {
			t.Fatal("Safe review seal excludes its joined inputs")
		}
	}
}

// Completed maximum envelopes and the old unexecuted reservation are separate
// conservative liabilities; the boundary fails when either one is omitted.
func TestBootstrapSuccessorSafeReviewConservesOriginalLiabilities(t *testing.T) {
	approval, _, request, reference, rawArchive := bootstrapSuccessorSafeTestInputs(t, "1.4.1", "Safe")
	record := bootstrapSuccessorSafeTestRecord(approval)
	result, err := buildBootstrapSuccessorSafeReview(t.Context(), approval.Plan, record, request, reference, rawArchive)
	if err != nil || result.Relayer.MaximumLiabilityWei != "700000" || result.Relayer.CumulativeLiabilityWei != "1200000" || result.Relayer.CumulativeCeilingWei != "1200000" || result.Relayer.UnreservedHeadroomWei != "0" ||
		result.Relayer.Sender != approval.Plan.Proposal.Request.IntendedRelayer || result.Relayer.Nonce != 42 || result.Relayer.To != approval.Plan.Proposal.Request.IntendedOwnerSafe ||
		result.Preparation.Approval.Plan.Proposal.Budget != approval.Plan.Proposal.Budget {
		t.Fatal("review changed additive attempt or lifetime floors", err)
	}
	request.RelayerGas++
	if _, err := buildBootstrapSuccessorSafeReview(t.Context(), approval.Plan, record, request, reference, rawArchive); err == nil || !strings.Contains(err.Error(), "cumulative lifetime liability") {
		t.Fatal("review spent the retained completed or unexecuted envelope reservation", err)
	}
	request.RelayerGas = 60000
	result, err = buildBootstrapSuccessorSafeReview(t.Context(), approval.Plan, record, request, reference, rawArchive)
	if err != nil || result.Relayer.UnreservedHeadroomWei != "100000" || result.Relayer.BudgetReserved || result.Preparation.Approval.Plan.Proposal.Budget.RetainedAttempts != 8 || result.Preparation.Approval.Plan.Proposal.Budget.ProposedRemainingAttempts != 2 {
		t.Fatal("review allocated allowance or reset original spend", err)
	}
}

// The Safe signs its own nonce, while a separately retained relayer nonce and
// native review window affect only the complete review seal, never that digest.
func TestBootstrapSuccessorSafeReviewSeparatesNoncesAndWindow(t *testing.T) {
	approval, key, request, reference, rawArchive := bootstrapSuccessorSafeTestInputs(t, "1.4.1", "Safe")
	baseline, err := buildBootstrapSuccessorSafeReview(t.Context(), approval.Plan, bootstrapSuccessorSafeTestRecord(approval), request, reference, rawArchive)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"safe nonce", "relayer nonce", "window"} {
		changed := copyBootstrapSuccessorPreparationTestApproval(t, approval)
		selected := request
		switch field {
		case "safe nonce":
			changed.Plan.Proposal.Request.IntendedSafeNonce = "18"
		case "relayer nonce":
			changed.Plan.Proposal.Request.IntendedRelayerNonce++
		case "window":
			selected.ValidThroughNative++
		}
		changed = bootstrapSuccessorPreparationTestSign(t, changed.Plan, key)
		record := bootstrapSuccessorSafeTestRecord(changed)
		selected.PreparationPlanHash, selected.PreparationRecordHash = changed.Plan.hash(), record.ContentHash
		result, err := buildBootstrapSuccessorSafeReview(t.Context(), changed.Plan, record, selected, reference, rawArchive)
		if err != nil || result.ContentHash == baseline.ContentHash ||
			(result.Transaction.Digest == baseline.Transaction.Digest) != (field != "safe nonce") ||
			(result.Relayer.Nonce == baseline.Relayer.Nonce) != (field != "relayer nonce") {
			t.Fatalf("review confused separate nonce or window authority for %s: %v", field, err)
		}
	}
}

// Independently signed preparation remains insufficient if its anchor, receipt
// identities or declared review window disagree with the one permitted call.
func TestBootstrapSuccessorSafeReviewRejectsReboundAnchor(t *testing.T) {
	approval, key, request, reference, rawArchive := bootstrapSuccessorSafeTestInputs(t, "1.5.0", "SafeL2")
	cases := []struct {
		name   string
		change func(*bootstrapContractSuccessorProposal)
	}{
		{name: "calldata", change: func(p *bootstrapContractSuccessorProposal) { p.Anchor.CallData = "0x01020304" }},
		{name: "value", change: func(p *bootstrapContractSuccessorProposal) { p.Anchor.ValueWei = "1" }},
		{name: "owner", change: func(p *bootstrapContractSuccessorProposal) { p.Anchor.ExpectedOwner[0] ^= 1 }},
		{name: "coordinator receipt", change: func(p *bootstrapContractSuccessorProposal) {
			p.AdoptedActions[4].Receipt.ContractAddress = p.Anchor.Evidence.Hex()
		}},
		{name: "evidence receipt", change: func(p *bootstrapContractSuccessorProposal) {
			p.AdoptedActions[7].Receipt.ContractAddress = p.Anchor.Coordinator.Hex()
		}},
		{name: "relayer aliases evidence", change: func(p *bootstrapContractSuccessorProposal) { p.Request.IntendedRelayer = p.Anchor.Evidence }},
		{name: "later adopted receipt", change: func(p *bootstrapContractSuccessorProposal) { p.AdoptedActions[7].Receipt.NativeNumber++ }},
		{name: "same height different hash", change: func(p *bootstrapContractSuccessorProposal) {
			p.AdoptedActions[7].Receipt.NativeHash = crypto.Keccak256Hash([]byte("synthetic other receipt")).Hex()
		}},
	}
	for _, c := range cases {
		changed := copyBootstrapSuccessorPreparationTestApproval(t, approval)
		c.change(&changed.Plan.Proposal)
		changed = bootstrapSuccessorPreparationTestSign(t, changed.Plan, key)
		record := bootstrapSuccessorSafeTestRecord(changed)
		selected := request
		selected.PreparationPlanHash, selected.PreparationRecordHash = changed.Plan.hash(), record.ContentHash
		if _, err := buildBootstrapSuccessorSafeReview(t.Context(), changed.Plan, record, selected, reference, rawArchive); err == nil {
			t.Fatal("review admitted rebound anchor", c.name)
		}
	}
	record := bootstrapSuccessorSafeTestRecord(approval)
	for _, field := range []string{"plan", "record"} {
		changed := request
		if field == "plan" {
			changed.PreparationPlanHash = rootObjectHash("synthetic different plan")
		} else {
			changed.PreparationRecordHash = rootObjectHash("synthetic different record")
		}
		if _, err := buildBootstrapSuccessorSafeReview(t.Context(), approval.Plan, record, changed, reference, rawArchive); err == nil {
			t.Fatal("review ignored preparation pin", field)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := buildBootstrapSuccessorSafeReview(ctx, approval.Plan, record, request, reference, rawArchive); err == nil {
		t.Fatal("canceled review completed")
	}
	rawArchive[len(rawArchive)/2] ^= 1
	if _, err := buildBootstrapSuccessorSafeReview(t.Context(), approval.Plan, record, request, reference, rawArchive); err == nil {
		t.Fatal("review trusted substituted release bytes")
	}
}

// Strict request grammar has no live chain assertion, signer, owner signature,
// arbitrary target or hidden reimbursement field, and all work is finite.
func TestBootstrapSuccessorSafeReviewRequestBounds(t *testing.T) {
	_, _, request, _, _ := bootstrapSuccessorSafeTestInputs(t, "1.4.1", "Safe")
	cases := []struct {
		name   string
		change func(*bootstrapSuccessorSafeRequest)
	}{
		{name: "schema", change: func(r *bootstrapSuccessorSafeRequest) { r.Schema = bootstrapSuccessorPreparationSchema }},
		{name: "profile", change: func(r *bootstrapSuccessorSafeRequest) { r.Version = "1.3.0" }},
		{name: "variant", change: func(r *bootstrapSuccessorSafeRequest) { r.Variant = "" }},
		{name: "archive pin", change: func(r *bootstrapSuccessorSafeRequest) { r.Archive.Sha256 = rootObjectHash("synthetic archive") }},
		{name: "relative archive", change: func(r *bootstrapSuccessorSafeRequest) { r.Archive.Path = "synthetic.tgz" }},
		{name: "gas low", change: func(r *bootstrapSuccessorSafeRequest) { r.RelayerGas = 20999 }},
		{name: "gas high", change: func(r *bootstrapSuccessorSafeRequest) { r.RelayerGas = 100000001 }},
		{name: "fee zero", change: func(r *bootstrapSuccessorSafeRequest) { r.RelayerFeeCapWei = "0" }},
		{name: "fee noncanonical", change: func(r *bootstrapSuccessorSafeRequest) { r.RelayerFeeCapWei = "010" }},
		{name: "tip exceeds fee", change: func(r *bootstrapSuccessorSafeRequest) { r.RelayerTipCapWei = "11" }},
		{name: "liability overflow", change: func(r *bootstrapSuccessorSafeRequest) {
			r.RelayerFeeCapWei = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)).String()
		}},
		{name: "empty window", change: func(r *bootstrapSuccessorSafeRequest) { r.ValidThroughNative = r.StartNativeNumber }},
		{name: "unbounded window", change: func(r *bootstrapSuccessorSafeRequest) { r.ValidThroughNative = r.StartNativeNumber + 7201 }},
		{name: "zero hash", change: func(r *bootstrapSuccessorSafeRequest) { r.StartNativeHash = (common.Hash{}).Hex() }},
	}
	for _, c := range cases {
		changed := request
		c.change(&changed)
		if err := changed.validate(); err == nil {
			t.Fatal("review admitted unbounded request", c.name)
		}
	}
}
