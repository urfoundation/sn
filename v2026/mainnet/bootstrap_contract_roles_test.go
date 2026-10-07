// Real signed producer configs and the approved contract artifacts establish
// offline cross-component binding without executing the deployment graph.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Reapproval happens before local custody exists. The two configs deliberately
// start from the old fixture's mutually matching but unrelated declarations.
func newBootstrapContractRoleFixture(t *testing.T) (*bootstrapChainFixture, evmCreatePlan) {
	t.Helper()
	f := newBootstrapSuccessorCommandFixture(t)
	evidence, err := selectEvmCreatePlan(t.Context(), f.preparation.Contracts, "evidence-create", f.config.Contracts.Path)
	if err != nil {
		t.Fatal(err)
	}
	proxy, vault := evidence.priorPlan(4), evidence.priorPlan(1)
	// The generic EVM fixture's placeholder hash has no policy preimage.
	// Reapprove its initializer with the real producer parent before custody.
	policyHash, err := f.validators[0].config.Policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	constructor := proxy.ProxyConstructor
	policy := constructor.ApprovedPolicy.binding()
	policy.PolicyHash = policyHash
	initializer := stabi.NewSTCoordinator().PackInitialize(f.config.Netuid, constructor.Owner, constructor.Guardian,
		common.HexToHash(constructor.SelfColdkey), vault.Address, evidence.priorPlan(0).Address, constructor.CommitmentOracle, policy)
	for _, artifact := range evmTestRelease(t).Artifacts {
		if artifact.Name == "ERC1967Proxy" {
			parsed, err := abi.JSON(strings.NewReader(artifact.Abi))
			if err != nil {
				t.Fatal(err)
			}
			arguments, err := parsed.Pack("", evidence.priorPlan(2).Address, initializer)
			if err != nil {
				t.Fatal(err)
			}
			f.contracts.config.Plan.Actions[4].Data = "0x" + artifact.Creation + hex.EncodeToString(arguments)
		}
	}
	f.contracts.publishConfig()
	f.config.Contracts = bootstrapRootTestWrite(t, f.config.Contracts.Path, f.contracts.config)
	for _, validator := range f.validators {
		validator.config.Coordinator = strings.ToLower(proxy.Address.Hex())
		validator.config.SettlementVault = strings.ToLower(vault.Address.Hex())
	}
	bootstrapContractRolePublish(t, f)
	evidence, err = selectEvmCreatePlan(t.Context(), f.preparation.Contracts, "evidence-create", f.config.Contracts.Path)
	if err != nil {
		t.Fatal(err)
	}
	return f, evidence
}

// Matching foreign declarations remain individually signed and pass original
// preparation admission. Reapproval derives both parent references from actual
// policy contents before signing; production never repairs a supplied mismatch.
func bootstrapContractRolePublish(t *testing.T, f *bootstrapChainFixture) {
	t.Helper()
	for i, validator := range f.validators {
		policyHash, err := validator.config.Policy.Hash()
		if err != nil {
			t.Fatal(err)
		}
		validator.config.PolicyHash = common.Hash(policyHash).Hex()
		validator.approval.Proposal.ParentPolicyHash = policyHash
		f.config.Validators[i].Config = validator.publish(t)
	}
	bootstrapRootTestWrite(t, f.path, f.config)
	var err error
	f.preparation, err = loadBootstrapChainPreparation(t.Context(), f.path)
	if err != nil {
		t.Fatal("independently signed original preparation refused", err)
	}
}

// Public output binds exact original pins and predicted protocol addresses,
// while every executed-state and activation claim remains explicitly false.
func TestBootstrapContractRolePlanBindsApprovedGraph(t *testing.T) {
	f, evidence := newBootstrapContractRoleFixture(t)
	before, reads := f.journals(t), maps.Clone(f.contracts.counts)
	var stdout, stderr bytes.Buffer
	args := []string{"bootstrap-chain", "contract-role-plan", "--config", f.path}
	if code := runMain(f.storageContext(t.Context()), args, &stdout, &stderr); code != 0 {
		t.Fatal("approved contract-role plan refused", code, stderr.String())
	}
	var result bootstrapContractRolePlan
	if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	seal := result.ContentHash
	result.ContentHash = ""
	proxy, vault, implementation := evidence.priorPlan(4), evidence.priorPlan(1), evidence.priorPlan(2)
	if seal != rootObjectHash(result) || result.Schema != bootstrapContractRolePlanSchema || result.PreparationHash != f.preparation.Plan.ContentHash ||
		result.ContractPlanHash != f.preparation.Plan.ContractPlanHash || result.Artifacts != f.contracts.config.Plan.Artifacts ||
		result.CoordinatorProxy != proxy.Address || result.CoordinatorImplementation != implementation.Address || result.SettlementVault != vault.Address ||
		result.EvidenceJournal != evidence.Address || result.EvidenceDomain != *evidence.EvidenceConstructor ||
		result.InitialPolicyHash != common.HexToHash(proxy.ProxyConstructor.ApprovedPolicy.PolicyHash) || !result.DeclarationsVerified ||
		result.CanonicalReceiptsVerified || result.CurrentStateVerified || result.InstallationComplete || result.ActivationReady || result.NetworkEffects ||
		!reflect.DeepEqual(result.PendingChainPhases, bootstrapChainPendingPhases()) || len(result.Validators) != 2 {
		t.Fatal("contract-role output lost exact declarations or claimed live authority", result)
	}
	for i, binding := range result.Validators {
		inspection := f.preparation.Plan.ValidatorInspections[i]
		if binding.Role != f.config.Validators[i].Role || binding.ValidatorId != inspection.ValidatorId || binding.Config != f.config.Validators[i].Config ||
			binding.ApprovalHash != rootObjectHash(inspection.Approval) || binding.DeclaredDeployBlock != inspection.DeployBlock {
			t.Fatal("contract-role output lost a signed producer declaration", i)
		}
	}
	if !reflect.DeepEqual(before, f.journals(t)) || !reflect.DeepEqual(reads, f.contracts.counts) {
		t.Fatal("contract-role planning changed custody or contacted the chain")
	}
	prepared := f.result(t, "apply")
	retained := f.journals(t)
	var repeated bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), args, &repeated, &stderr); code != 0 || !bytes.Equal(stdout.Bytes(), repeated.Bytes()) ||
		!reflect.DeepEqual(retained, f.journals(t)) || !reflect.DeepEqual(prepared, f.result(t, "resume")) {
		t.Fatal("contract-role planning changed original preparation or recovery", code, stderr.String())
	}
}

// A successful old pairwise comparison cannot admit two approvals targeting
// the implementation directly or another deployment's coordinator.
func TestBootstrapContractRolePlanRejectsMutualProxySubstitution(t *testing.T) {
	f, evidence := newBootstrapContractRoleFixture(t)
	for _, validator := range f.validators {
		validator.config.Coordinator = strings.ToLower(evidence.priorPlan(2).Address.Hex())
	}
	bootstrapContractRolePublish(t, f)
	bootstrapContractRoleReject(t, f, "coordinator differs from the approved proxy", "mutually signed implementation substituted for coordinator proxy")
}

// Both producer signatures can agree on a foreign vault without proving its
// relation to the independently approved deployment graph.
func TestBootstrapContractRolePlanRejectsMutualVaultSubstitution(t *testing.T) {
	f, _ := newBootstrapContractRoleFixture(t)
	for _, validator := range f.validators {
		validator.config.SettlementVault = "0x" + strings.Repeat("78", 20)
	}
	bootstrapContractRolePublish(t, f)
	bootstrapContractRoleReject(t, f, "vault differs from the approved settlement vault", "mutually signed foreign vault bypassed contract-role binding")
}

// Matching deployment addresses cannot silently select a different approved
// policy identifier from the proxy's exact atomic initializer.
func TestBootstrapContractRolePlanRejectsMutualPolicySubstitution(t *testing.T) {
	f, _ := newBootstrapContractRoleFixture(t)
	for _, validator := range f.validators {
		validator.config.Policy.Settlement.ClaimTTLEpochs++
	}
	bootstrapContractRolePublish(t, f)
	bootstrapContractRoleReject(t, f, "policy differs from the approved proxy initializer", "mutually signed foreign policy bypassed contract-role binding")
}

// A signature over a changed hash is not a policy preimage. The normal producer
// loader must reject it before declaration comparison, without repairing it.
func TestBootstrapContractRolePlanRejectsSignedPolicyHashMismatch(t *testing.T) {
	f, _ := newBootstrapContractRoleFixture(t)
	for i, validator := range f.validators {
		validator.config.PolicyHash = "0x" + strings.Repeat("78", 32)
		f.config.Validators[i].Config = validator.publish(t)
	}
	bootstrapRootTestWrite(t, f.path, f.config)
	bootstrapContractRoleReject(t, f, "configured parent policy hash differs", "signed policy hash without its preimage was admitted")
}

// Re-signing a changed complete configuration cannot rewrite the independently
// named owner-recycle parent retained inside its successor proposal.
func TestBootstrapContractRolePlanRejectsSignedRecycleParentMismatch(t *testing.T) {
	f, _ := newBootstrapContractRoleFixture(t)
	for i, validator := range f.validators {
		validator.config.Policy.Settlement.ClaimTTLEpochs++
		var err error
		validator.config.PolicyHash, err = validator.config.Policy.HashHex()
		if err != nil {
			t.Fatal(err)
		}
		f.config.Validators[i].Config = validator.publish(t)
	}
	bootstrapRootTestWrite(t, f.path, f.config)
	bootstrapContractRoleReject(t, f, "unchanged mainnet parent policy hash", "signed successor with a stale parent policy was admitted")
}

// Refusal produces no partially usable output and cannot prepare child custody.
func bootstrapContractRoleReject(t *testing.T, f *bootstrapChainFixture, diagnostic, assertion string) {
	t.Helper()
	before, reads := f.journals(t), maps.Clone(f.contracts.counts)
	var stdout, stderr bytes.Buffer
	code := runMain(f.storageContext(t.Context()), []string{"bootstrap-chain", "contract-role-plan", "--config", f.path}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), diagnostic) ||
		!reflect.DeepEqual(before, f.journals(t)) || !reflect.DeepEqual(reads, f.contracts.counts) {
		t.Fatal(assertion, code, stdout.String(), stderr.String())
	}
}

// A reserve-only signed graph cannot synthesize the missing evidence domain or
// infer future proxy/vault addresses from a pair of producer declarations.
func TestBootstrapContractRolePlanRequiresEvidenceGraph(t *testing.T) {
	f := newBootstrapChainFixture(t)
	bootstrapContractRoleReject(t, f, "complete evidence CREATE graph", "incomplete graph supplied contract-role authority")
}

// Neither cancellation, output loss nor invented online flags may open a
// writer or weaken retained preparation after the declaration-only check.
func TestBootstrapContractRolePlanKeepsOfflineBoundary(t *testing.T) {
	f, _ := newBootstrapContractRoleFixture(t)
	before, reads := f.journals(t), maps.Clone(f.contracts.counts)
	args := []string{"bootstrap-chain", "contract-role-plan", "--config", f.path}
	for _, suffix := range []string{"--online", "--submit", "--run-dir"} {
		var stdout, stderr bytes.Buffer
		if code := runMain(f.storageContext(t.Context()), append(append([]string{}, args...), suffix), &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Fatal("contract-role plan acquired an execution option", suffix, code)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code := runMain(f.storageContext(ctx), args, io.Discard, io.Discard); code == 0 {
		t.Fatal("canceled contract-role plan reported success")
	}
	if code := runMain(f.storageContext(t.Context()), args, bootstrapContractRoleFailedOutput{}, io.Discard); code != 1 {
		t.Fatal("contract-role output loss reported success", code)
	}
	if !reflect.DeepEqual(before, f.journals(t)) || !reflect.DeepEqual(reads, f.contracts.counts) {
		t.Fatal("contract-role failure changed custody or contacted the chain")
	}
}

// Output failure exercises the command's real encoder boundary without I/O.
type bootstrapContractRoleFailedOutput struct{}

func (bootstrapContractRoleFailedOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
