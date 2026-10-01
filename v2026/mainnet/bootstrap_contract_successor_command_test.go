// The public successor command observes a complete v3 preparation and genuine
// eight-action local EVM history under one synthetic approved runtime domain.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Reapprove common synthetic facts before any custody is claimed. The native
// fixture's full metadata is also served by the local EVM receipt adapter.
func newBootstrapSuccessorCommandFixture(t *testing.T) *bootstrapChainFixture {
	t.Helper()
	f := newBootstrapChainFixture(t)
	contracts := newEvmEvidenceFixture(t)
	raw, _, err := readPlanFile(t.Context(), f.config.OwnerTrimPolicy.Path, maxRpcReplyBytes)
	if err != nil {
		t.Fatal(err)
	}
	var policy subnetCensusPolicy
	if err := decodePlanJson(raw, &policy); err != nil {
		t.Fatal(err)
	}
	policy.RuntimeCodeHash = contracts.config.Plan.Runtime.RuntimeCodeHash
	f.census.policy.RuntimeCodeHash = policy.RuntimeCodeHash
	f.config.OwnerTrimPolicy = bootstrapRootTestWrite(t, f.config.OwnerTrimPolicy.Path, policy)
	preview, err := f.client.readSubnetPreview(t.Context(), policy, f.config.OwnerTrimPolicy.Sha256)
	if err != nil {
		t.Fatal(err)
	}
	trim, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil {
		t.Fatal(err)
	}
	f.config.OwnerTrimPlan = bootstrapRootTestWrite(t, f.config.OwnerTrimPlan.Path, trim)
	contracts.config.Plan.Network, contracts.config.Plan.DeploymentId = f.config.Network, f.config.DeploymentId
	contracts.config.Plan.RunDirectory = f.config.RunDirectory
	// Genuine inclusion decodes full native metadata under race instrumentation.
	// This success-path fixture approves the existing finite send bound up front.
	contracts.config.Plan.Route.SendTimeoutSeconds = 60
	contracts.config.Plan.Runtime = rootReceiptProfile{RuntimeSourceCommit: policy.RuntimeSourceCommit, RuntimeVersion: policy.RuntimeVersion,
		RuntimeCodeHash: policy.RuntimeCodeHash, RuntimeMetadataHash: policy.RuntimeMetadataHash}
	contracts.metadata, err = hex.DecodeString(f.census.metadataHex[2:])
	if err != nil {
		t.Fatal(err)
	}
	contracts.override = func(method string, _ []any, result any) any {
		if method == "system_chain" {
			return f.config.Network.NativeChain
		}
		return result
	}
	deployment := sha256.Sum256([]byte(f.config.DeploymentId))
	arguments := stabi.NewSTValidatorEvidence().PackConstructor(crypto.CreateAddress(contracts.config.Plan.Actions[0].Sender, 4), common.HexToHash(f.config.Network.GenesisHash), deployment)
	for _, artifact := range evmTestRelease(t).Artifacts {
		if artifact.Name == "ValidatorEvidence" {
			contracts.config.Plan.Actions[7].Data = "0x" + artifact.Creation + hex.EncodeToString(arguments)
		}
	}
	contracts.publishConfig()
	f.contracts = contracts
	f.config.Contracts = bootstrapRootTestWrite(t, contracts.configPath, contracts.config)
	action := copyRootAction(f.root.offline.packet.Action)
	action.Scope.RuntimeCodeHash = policy.RuntimeCodeHash
	action, err = prepareRootAction(action, f.census.metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	f.root.offline.packet = rootOfflineApprove(t, f.root.offline.trust, action, f.root.offline.approvalKey)
	service := copyRootServiceConfig(f.root.plan.Service)
	service.Packet = f.root.offline.packet
	f.rootRole.service(t, service)
	f.rootRole.approve(t)
	for i, validator := range f.validators {
		validator.config.RuntimeCodeHash = policy.RuntimeCodeHash
		validator.approval.Proposal.Runtime.CodeHash = bootstrapChainTestAccount(t, policy.RuntimeCodeHash)
		f.config.Validators[i].Config = validator.publish(t)
	}
	bootstrapRootTestWrite(t, f.path, f.config)
	f.preparation, err = loadBootstrapChainPreparation(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Complete the genuine original preparation and eight actions once per public
// command fixture; each success fixture has its finite send budget approved up front.
func bootstrapSuccessorCommandTestComplete(t *testing.T, f *bootstrapChainFixture) bootstrapChainResult {
	t.Helper()
	prepared := f.result(t, "apply")
	if !prepared.LocalPreparationComplete || !prepared.UrValidatorConfigsVerified || !prepared.RootValidatorConfigVerified || len(f.journals(t)) != 10 {
		t.Fatal("full-v3 fixture did not claim five original preparation owners")
	}
	reserve := f.contracts.plan
	for i, action := range f.contracts.config.Plan.Actions {
		plan, err := selectEvmCreatePlan(t.Context(), reserve, action.Id, f.contracts.configPath)
		if err != nil {
			t.Fatal(err)
		}
		f.contracts.plan, f.contracts.receipt = plan, nil
		f.contracts.signSelectedAction()
		if i != 0 {
			if _, code, diagnostic := f.contracts.command("apply", "--action", action.Id); code != 0 {
				t.Fatalf("prepare genuine action %s: %d %s", action.Id, code, diagnostic)
			}
		}
		for _, args := range [][]string{
			{"--signed-transaction", f.contracts.signedPath, "--signed-transaction-hash", f.contracts.signedHash},
			{"--online", "--submit"}, {"--online"},
		} {
			if _, code, diagnostic := f.contracts.command("resume", append([]string{"--action", action.Id}, args...)...); code != 0 {
				t.Fatalf("execute genuine action %s %v: %d %s", action.Id, args, code, diagnostic)
			}
		}
	}
	retained := f.result(t, "resume")
	if retained.Contracts.Status != "reserve-created" || retained.Contracts.TransactionHash == "" || retained.NetworkEffects || retained.ActivationReady {
		t.Fatalf("prepared v3 scope did not retain genuine reserve completion: %+v", retained)
	}
	return retained
}

// One full graph covers the public output boundary, exact preparation and
// receipt adoption, unchanged custody, output failure and released ownership.
func TestBootstrapContractSuccessorCommandAdoptsCompleteV3Custody(t *testing.T) {
	f := newBootstrapSuccessorCommandFixture(t)
	retained := bootstrapSuccessorCommandTestComplete(t, f)
	before := bootstrapContractTestJournals(t, f.config.RunDirectory)
	maps.Copy(before, f.journals(t))
	custodyNames := func() []string {
		entries, err := os.ReadDir(f.config.RunDirectory)
		if err != nil {
			t.Fatal(err)
		}
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return names
	}
	originalNames := custodyNames()
	f.contracts.stateLock.Lock()
	counts, writes := maps.Clone(f.contracts.counts), len(f.contracts.writes)
	f.contracts.stateLock.Unlock()
	f.census.stateLock.Lock()
	censusCounts := maps.Clone(f.census.methodCounts)
	f.census.stateLock.Unlock()
	request := bootstrapSuccessorTestRequest(f.contracts.config, f.preparation.Plan.ContentHash)
	path := filepath.Join(filepath.Dir(f.path), "complete-successor-request.json")
	requestReference := bootstrapRootTestWrite(t, path, request)
	var first []byte
	for iteration := range 2 {
		var stdout, stderr bytes.Buffer
		if code := f.command(t.Context(), "contract-successor-plan", &stdout, &stderr, "--request", path); code != 3 || stderr.Len() != 0 {
			t.Fatalf("full-v3 successor command failed completed custody: %d %s", code, stderr.String())
		}
		var proposal bootstrapContractSuccessorProposal
		if err := decodePlanJson(stdout.Bytes(), &proposal); err != nil {
			t.Fatal(err)
		}
		if proposal.LocalPreparation == nil || proposal.LocalPreparation.ContractTransactionHash != retained.Contracts.TransactionHash || proposal.RequestSha256 != requestReference.Sha256 ||
			proposal.Request.BootstrapPlanHash != retained.PlanHash || proposal.OriginalConfigHash != rootObjectHash(f.contracts.config) ||
			len(proposal.AdoptedActions) != 8 || len(proposal.UnfinishedActions) != 1 || proposal.UnfinishedActions[0] != "evidence-anchor" ||
			proposal.Budget.RetainedAttempts != 8 || proposal.Budget.ProposedMaximumCumulativeAttempts != 10 || proposal.Budget.ProposedRemainingAttempts != 2 ||
			proposal.Budget.RetryMarginAttempts != 1 || proposal.Budget.ProposedMaximumLifetimeWei != "2244000000" ||
			proposal.ApprovalVerified || proposal.ApprovalSigningPayloadProvided || proposal.Executable || proposal.CurrentChainVerified || proposal.SafeAuthorityVerified || proposal.NetworkEffects || proposal.InstallationComplete || proposal.ActivationReady {
			t.Fatalf("public successor lost complete original v3 scope or gained authority: %+v", proposal)
		}
		seal := proposal.ContentHash
		proposal.ContentHash = ""
		if seal != rootObjectHash(proposal) {
			t.Fatal("public successor output seal omitted retained v3 preparation")
		}
		for i, adopted := range proposal.AdoptedActions {
			var original evmActionRecord
			if err := json.Unmarshal([]byte(before[filepath.Join(f.config.RunDirectory, bootstrapContractStateFile(i))]), &original); err != nil {
				t.Fatal(err)
			}
			if original.Receipt == nil || adopted.Receipt == nil || adopted.JournalHash != original.ContentHash || adopted.CustodyHash != rootObjectHash(original) ||
				adopted.TransactionHash != original.TransactionHash || *adopted.Receipt != *original.Receipt || adopted.Attempts != original.Attempts {
				t.Fatalf("public successor replaced original receipt or signed attempt %d", i)
			}
		}
		if iteration == 0 {
			first = append([]byte(nil), stdout.Bytes()...)
		} else if !bytes.Equal(first, stdout.Bytes()) {
			t.Fatal("public successor repeated observation changed its exact proposal")
		}
		if resumed := f.result(t, "resume"); !reflect.DeepEqual(retained, resumed) {
			t.Fatal("public successor prevented unchanged original preparation resume")
		}
	}
	var stderr bytes.Buffer
	if code := f.command(t.Context(), "contract-successor-plan", ioFailureWriter{}, &stderr, "--request", path); code != 1 {
		t.Fatal("public successor lost its output failure", code)
	}
	if resumed := f.result(t, "resume"); !reflect.DeepEqual(retained, resumed) {
		t.Fatal("output failure retained original preparation locks")
	}
	for path, original := range before {
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != original {
			t.Fatalf("public successor changed original journal %s: %v", filepath.Base(path), err)
		}
	}
	if !reflect.DeepEqual(originalNames, custodyNames()) {
		t.Fatal("public successor created additional custody")
	}
	f.contracts.stateLock.Lock()
	unchanged := maps.Equal(counts, f.contracts.counts) && len(f.contracts.writes) == writes
	f.contracts.stateLock.Unlock()
	f.census.stateLock.Lock()
	unchanged = unchanged && maps.Equal(censusCounts, f.census.methodCounts)
	f.census.stateLock.Unlock()
	if !unchanged || writes != 8 {
		t.Fatal("public successor reobserved or replayed the original eight actions")
	}
}
