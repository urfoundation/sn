// Successor review tests use original synthetic approvals and real local EVM
// receipts. No fixture supplies Safe code, signatures or production authority.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Public proposed identities are independent synthetic inputs. The original
// fixture initializer fixes owner 41; a declared Safe remains unverified.
func bootstrapSuccessorTestRequest(config evmPhaseConfig, bootstrapHash string) bootstrapContractSuccessorRequest {
	return bootstrapContractSuccessorRequest{Schema: bootstrapContractSuccessorRequestSchema, SuccessorId: "synthetic-anchor-successor",
		BootstrapPlanHash: bootstrapHash, OriginalContractPlanHash: config.Plan.hash(), AdditionalMaximumAttempts: 2,
		AdditionalMaximumWei: "1000000", IntendedOwnerSafe: common.Address{0: 41}, IntendedSafeNonce: "3",
		IntendedRelayer: common.Address{19: 199}, IntendedRelayerNonce: 21}
}

// Every adoption starts by loading the independently signed original graph;
// result booleans and user-supplied journal digests cannot replace that owner.
func bootstrapSuccessorTestInspect(t *testing.T, f *evmCreateFixture, request bootstrapContractSuccessorRequest) (bootstrapContractSuccessorProposal, error) {
	t.Helper()
	reserve, err := loadEvmCreatePlan(t.Context(), f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	return inspectBootstrapContractSuccessor(t.Context(), reserve, f.configPath, request, rootObjectHash(request))
}

// Execute the approved eight actions in the local EVM before any proposal can
// observe their durable journal and receipt lineage.
func bootstrapSuccessorTestComplete(t *testing.T, f *evmCreateFixture) {
	t.Helper()
	f.prepareEvidenceSigned()
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online"); code != 0 || result.Status != "evidence-created-unanchored" {
		t.Fatalf("canonical evidence prerequisite: %+v %d %s", result, code, diagnostic)
	}
}

// An increase is finite, additive and scoped to the original approval. Neither
// identical custody identity nor a reused original sender/nonce can be proposed.
func TestBootstrapContractSuccessorRequestKeepsIncrementalScope(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	request := bootstrapSuccessorTestRequest(f.config, "sha256:"+strings.Repeat("cd", 32))
	if err := request.validate(f.config); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*bootstrapContractSuccessorRequest)
	}{
		{name: "schema", change: func(r *bootstrapContractSuccessorRequest) { r.Schema = evmPhaseApprovalSchema }},
		{name: "same custody", change: func(r *bootstrapContractSuccessorRequest) { r.SuccessorId = f.config.Plan.CustodyId }},
		{name: "changed plan", change: func(r *bootstrapContractSuccessorRequest) {
			r.OriginalContractPlanHash = "sha256:" + strings.Repeat("ef", 32)
		}},
		{name: "missing bootstrap", change: func(r *bootstrapContractSuccessorRequest) { r.BootstrapPlanHash = "" }},
		{name: "zero attempts", change: func(r *bootstrapContractSuccessorRequest) { r.AdditionalMaximumAttempts = 0 }},
		{name: "unbounded attempts", change: func(r *bootstrapContractSuccessorRequest) { r.AdditionalMaximumAttempts = 17 }},
		{name: "zero increment", change: func(r *bootstrapContractSuccessorRequest) { r.AdditionalMaximumWei = "0" }},
		{name: "noncanonical increment", change: func(r *bootstrapContractSuccessorRequest) { r.AdditionalMaximumWei = "01" }},
		{name: "negative increment", change: func(r *bootstrapContractSuccessorRequest) { r.AdditionalMaximumWei = "-1" }},
		{name: "overflow increment", change: func(r *bootstrapContractSuccessorRequest) {
			r.AdditionalMaximumWei = new(big.Int).Lsh(big.NewInt(1), 256).String()
		}},
		{name: "alternate Safe nonce", change: func(r *bootstrapContractSuccessorRequest) { r.IntendedSafeNonce = "03" }},
		{name: "missing owner", change: func(r *bootstrapContractSuccessorRequest) { r.IntendedOwnerSafe = common.Address{} }},
		{name: "missing relayer", change: func(r *bootstrapContractSuccessorRequest) { r.IntendedRelayer = common.Address{} }},
		{name: "aliased Safe", change: func(r *bootstrapContractSuccessorRequest) { r.IntendedRelayer = r.IntendedOwnerSafe }},
		{name: "original nonce", change: func(r *bootstrapContractSuccessorRequest) {
			r.IntendedRelayer, r.IntendedRelayerNonce = f.config.Plan.Actions[7].Sender, f.config.Plan.Actions[7].Nonce
		}},
	}
	for _, c := range cases {
		changed := request
		c.change(&changed)
		if err := changed.validate(f.config); err == nil {
			t.Fatalf("successor admitted %s", c.name)
		}
	}
	config := copyEvmPhaseConfig(f.config)
	config.Plan.MaximumTotalWei = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)).String()
	request.OriginalContractPlanHash = config.Plan.hash()
	if err := request.validate(config); err == nil || !strings.Contains(err.Error(), "overflows") {
		t.Fatal("additive lifetime ceiling lost the original liability", err)
	}
	if len(bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) != 0 || len(f.counts) != 0 {
		t.Fatal("request validation opened execution custody or a route")
	}
}

// Repeated proposal revisions reuse eight authentic receipts unchanged. Only
// the remaining anchor plus explicit retry margin receives proposed attempts.
func TestBootstrapContractSuccessorAdoptsCompletedCustodyAndIncrementalBudgets(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	bootstrapSuccessorTestComplete(t, f)
	before := bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)
	f.stateLock.Lock()
	counts, writes := maps.Clone(f.counts), len(f.writes)
	f.stateLock.Unlock()
	request := bootstrapSuccessorTestRequest(f.config, "sha256:"+strings.Repeat("cd", 32))
	for _, additional := range []uint8{2, 3} {
		request.AdditionalMaximumAttempts = additional
		proposal, err := bootstrapSuccessorTestInspect(t, f, request)
		if err != nil {
			t.Fatal(err)
		}
		if proposal.Schema != bootstrapContractSuccessorProposalSchema || proposal.Status != "unsigned-proposal-prerequisites-unresolved" || proposal.Request != request || proposal.RequestSha256 != rootObjectHash(request) ||
			proposal.OriginalConfigHash != rootObjectHash(f.config) || proposal.OriginalCustodyId != f.config.Plan.CustodyId || proposal.OriginalRunDirectory != f.config.Plan.RunDirectory || len(proposal.AdoptedActions) != 8 ||
			len(proposal.OriginalUnexecutedActions) != 0 || !slices.Equal(proposal.UnfinishedActions, []string{"evidence-anchor"}) || proposal.ApprovalVerified || proposal.ApprovalSigningPayloadProvided || proposal.Executable || proposal.CurrentChainVerified || proposal.SafeAuthorityVerified || proposal.NetworkEffects || proposal.InstallationComplete || proposal.ActivationReady {
			t.Fatalf("unsigned adoption lost scope or gained authority: %+v", proposal)
		}
		budget := proposal.Budget
		if budget.OriginalMaximumAttempts != 8 || budget.RetainedAttempts != 8 || budget.UnfinishedActionCount != 1 || budget.AdditionalMaximumAttempts != additional || budget.ProposedMaximumCumulativeAttempts != 8+uint16(additional) || budget.ProposedRemainingAttempts != uint16(additional) || budget.RetryMarginAttempts != uint16(additional)-1 ||
			budget.OriginalMaximumWei != "2243000000" || budget.CompletedEnvelopeReservationWei != "2243000000" || budget.UnexecutedEnvelopeReservationWei != "0" || budget.AdditionalMaximumWei != "1000000" || budget.ProposedMaximumLifetimeWei != "2244000000" {
			t.Fatalf("successor reset original spend or charged eight fresh actions: %+v", budget)
		}
		for i, action := range proposal.AdoptedActions {
			var original evmActionRecord
			if err := json.Unmarshal([]byte(before[filepath.Join(f.config.Plan.RunDirectory, bootstrapContractStateFile(i))]), &original); err != nil {
				t.Fatal(err)
			}
			if action.CustodyStatus != "retained-complete" || action.ReceiptObservation != "retained" || action.JournalHash != original.ContentHash || action.CustodyHash != rootObjectHash(original) || action.TransactionHash != original.TransactionHash || action.Attempts != original.Attempts || action.Receipt == nil || *action.Receipt != *original.Receipt {
				t.Fatalf("successor lost original action %d signature or receipt seal: %+v", i, action)
			}
		}
		coordinator := crypto.CreateAddress(f.config.Plan.Actions[0].Sender, 4)
		evidence := crypto.CreateAddress(f.config.Plan.Actions[0].Sender, 7)
		call := append(crypto.Keccak256([]byte("fixValidatorEvidence(address)"))[:4], common.LeftPadBytes(evidence.Bytes(), 32)...)
		if proposal.Anchor.ExpectedOwner != request.IntendedOwnerSafe || proposal.Anchor.Coordinator != coordinator || proposal.Anchor.Evidence != evidence || proposal.Anchor.ValueWei != "0" || proposal.Anchor.CallData != "0x"+hex.EncodeToString(call) || !slices.Contains(proposal.RequiredPrerequisites, "EXACT_SAFE_DIGEST_SIGNATURES_AND_OUTER_RELAYER_ENVELOPE") {
			t.Fatalf("successor invented a Safe transaction or lost original anchor target: %+v", proposal.Anchor)
		}
	}
	f.stateLock.Lock()
	unchanged := maps.Equal(counts, f.counts) && len(f.writes) == writes
	f.stateLock.Unlock()
	if !unchanged || !maps.Equal(before, bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) {
		t.Fatal("successor replayed, reobserved or rewrote adopted custody")
	}
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create"); code != 0 {
		t.Fatal("original owner cannot resume after proposal", diagnostic)
	}
}

// A ninth original envelope stays a separate sealed liability. Its unreviewed
// bytes are neither released nor reinterpreted as exact Safe authority.
func TestBootstrapContractSuccessorRetainsUnexecutedOriginalReservation(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	target := common.Address{19: 77}
	reservation := evmPhaseAction{Id: "evidence-anchor", Sender: f.config.Plan.Actions[0].Sender, Nonce: 8, To: &target, Data: "0x01", ValueWei: "0", Gas: 21000, FeeCapWei: "10", TipCapWei: "1"}
	f.config.Plan.Actions = append(f.config.Plan.Actions, reservation)
	f.config.Plan.MaximumTotalWei = "2244000000"
	f.publishConfig()
	bootstrapSuccessorTestComplete(t, f)
	before := bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)
	request := bootstrapSuccessorTestRequest(f.config, "sha256:"+strings.Repeat("cd", 32))
	proposal, err := bootstrapSuccessorTestInspect(t, f, request)
	if err != nil || len(proposal.OriginalUnexecutedActions) != 1 || !reflect.DeepEqual(proposal.OriginalUnexecutedActions[0], reservation) ||
		proposal.Budget.UnexecutedEnvelopeReservationWei != "210000" || proposal.Budget.ProposedMaximumLifetimeWei != "2245000000" || proposal.Budget.RetainedAttempts != 8 ||
		!slices.Contains(proposal.RequiredPrerequisites, "ORIGINAL_UNEXECUTED_RESERVATION_DISPOSITION") || proposal.SafeAuthorityVerified || proposal.Executable || !maps.Equal(before, bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) {
		t.Fatalf("successor released or executed the ninth original reservation: %+v %v", proposal, err)
	}
	request.IntendedRelayer, request.IntendedRelayerNonce = reservation.Sender, reservation.Nonce
	if err := request.validate(f.config); err == nil || !strings.Contains(err.Error(), "reserved custody") {
		t.Fatal("successor reused an original reserved nonce", err)
	}
}

// Signed-but-unfinished work belongs to its original owner. Missing journals
// and partial markers remain unchanged and never manufacture adopted receipts.
func TestBootstrapContractSuccessorRequiresCompleteOriginalCustody(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	request := bootstrapSuccessorTestRequest(f.config, "sha256:"+strings.Repeat("cd", 32))
	before := bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)
	proposal, err := bootstrapSuccessorTestInspect(t, f, request)
	if err == nil || !strings.Contains(err.Error(), "eight retained successful") || proposal.Schema != "" || !maps.Equal(before, bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) {
		t.Fatal("successor adopted an unfinished signed original action", err)
	}
	path := filepath.Join(f.config.Plan.RunDirectory, evmEvidenceCreateStateFile)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, partial := range []bool{false, true} {
		if partial {
			if err := os.WriteFile(path+".lock", []byte("synthetic-partial-claim\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		missing := bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)
		if proposal, err := bootstrapSuccessorTestInspect(t, f, request); err == nil || proposal.Schema != "" || !maps.Equal(missing, bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) {
			t.Fatal("successor repaired missing or partial original custody", partial, err)
		}
	}
	for _, suffix := range []string{"", ".lock"} {
		if err := os.WriteFile(path+suffix, []byte(before[path+suffix]), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create"); code != 0 {
		t.Fatal("failed proposal leaked original ownership", diagnostic)
	}
}

// Naming an owner address cannot change the initializer, and a relayer cannot
// alias one of the deployed contract addresses even before custody is opened.
func TestBootstrapContractSuccessorRejectsChangedOwnerAndContractRelayer(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	request := bootstrapSuccessorTestRequest(f.config, "sha256:"+strings.Repeat("cd", 32))
	request.IntendedOwnerSafe = common.Address{19: 198}
	if _, err := bootstrapSuccessorTestInspect(t, f, request); err == nil || !strings.Contains(err.Error(), "exact configured owner") {
		t.Fatal("successor inferred a different owner authority", err)
	}
	request.IntendedOwnerSafe = common.Address{0: 41}
	request.IntendedRelayer = crypto.CreateAddress(f.config.Plan.Actions[0].Sender, 7)
	if _, err := bootstrapSuccessorTestInspect(t, f, request); err == nil || !strings.Contains(err.Error(), "contract address") {
		t.Fatal("successor proposed a contract as its relayer", err)
	}
	if len(bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) != 0 || len(f.counts) != 0 {
		t.Fatal("identity rejection opened original custody or a route")
	}
}

// The public command demands exact original v3 scope and all original owners.
// Failure, writer contention and cancellation leave preparation resumable.
func TestBootstrapContractSuccessorCommandRetainsScopeAndReleasesOwnership(t *testing.T) {
	f := newBootstrapChainFixture(t)
	f.result(t, "apply")
	request := bootstrapSuccessorTestRequest(f.contracts.config, f.preparation.Plan.ContentHash)
	path := filepath.Join(filepath.Dir(f.path), "successor-request.json")
	bootstrapRootTestWrite(t, path, request)
	before := f.journals(t)
	var stdout, stderr bytes.Buffer
	if code := f.command(t.Context(), "contract-successor-plan", &stdout, &stderr, "--request", path); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "eight-action graph") {
		t.Fatalf("successor command bypassed original incomplete scope: %d %s", code, stderr.String())
	}
	for _, extra := range [][]string{{"--online"}, {"--submit"}, {"--rpc", "https://rpc.example"}, {"--signed-transaction", "/synthetic.bin"}, {"--accept-plan-hash", "sha256:" + strings.Repeat("ef", 32)}} {
		stdout.Reset()
		stderr.Reset()
		args := append([]string{"--request", path}, extra...)
		if code := f.command(t.Context(), "contract-successor-plan", &stdout, &stderr, args...); code != 2 && code != 3 || stdout.Len() != 0 {
			t.Fatalf("successor command acquired execution scope: %v %d %s", extra, code, stderr.String())
		}
	}
	request.BootstrapPlanHash = "sha256:" + strings.Repeat("ef", 32)
	bootstrapRootTestWrite(t, path, request)
	if code := f.command(t.Context(), "contract-successor-plan", io.Discard, &stderr, "--request", path); code != 2 {
		t.Fatal("successor request replaced original preparation hash", code)
	}
	request.BootstrapPlanHash = f.preparation.Plan.ContentHash
	bootstrapRootTestWrite(t, path, request)
	store, err := openEvmActionStore(f.contracts.config, false, nil, f.storageContext(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.close() })
	stderr.Reset()
	if code := f.command(t.Context(), "contract-successor-plan", io.Discard, &stderr, "--request", path); code != 1 || !strings.Contains(stderr.String(), "active custody owner") {
		t.Fatalf("successor crossed original custody writer: %d %s", code, stderr.String())
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code := f.command(ctx, "contract-successor-plan", io.Discard, &stderr, "--request", path); code == 0 || !maps.Equal(before, f.journals(t)) || len(f.contracts.counts) != 0 {
		t.Fatal("successor cancellation changed original custody", code)
	}
	f.result(t, "resume")
}
