// Contract-role admission binds independently signed producer declarations to
// the approved installation graph. It supplies no current-state or send authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/ethereum/go-ethereum/common"
)

const bootstrapContractRolePlanSchema = "urnetwork-mainnet-bootstrap-contract-role-plan-v1"

// The deployment scan floor is only a declaration until canonical installation
// receipts establish that it cannot omit the required contract history.
type bootstrapContractValidatorBinding struct {
	Role                string            `json:"role"`
	ValidatorId         uint64            `json:"validator_id"`
	Config              planFileReference `json:"config"`
	ApprovalHash        string            `json:"approval_hash"`
	DeclaredDeployBlock uint64            `json:"declared_deploy_block"`
}

// These are reviewed declarations and predicted addresses, never a certificate
// of deployment, an accepted evidence anchor or permission to start a producer.
type bootstrapContractRolePlan struct {
	Schema                    string                              `json:"schema"`
	PreparationHash           string                              `json:"preparation_hash"`
	ContractPlanHash          string                              `json:"contract_plan_hash"`
	Artifacts                 planFileReference                   `json:"artifacts"`
	CoordinatorProxy          common.Address                      `json:"coordinator_proxy"`
	CoordinatorImplementation common.Address                      `json:"coordinator_implementation"`
	SettlementVault           common.Address                      `json:"settlement_vault"`
	EvidenceJournal           common.Address                      `json:"evidence_journal"`
	EvidenceDomain            contractEvidenceConstructor         `json:"evidence_domain"`
	InitialPolicyHash         common.Hash                         `json:"initial_policy_hash"`
	Validators                []bootstrapContractValidatorBinding `json:"validators"`
	DeclarationsVerified      bool                                `json:"declarations_verified"`
	CanonicalReceiptsVerified bool                                `json:"canonical_receipts_verified"`
	CurrentStateVerified      bool                                `json:"current_state_verified"`
	InstallationComplete      bool                                `json:"installation_complete"`
	ActivationReady           bool                                `json:"activation_ready"`
	NetworkEffects            bool                                `json:"network_effects"`
	PendingChainPhases        []string                            `json:"pending_chain_phases"`
	ContentHash               string                              `json:"content_hash"`
}

// Every invocation rereads all original signed inputs. Comparing two producers
// with each other cannot substitute for binding them to the installed protocol.
func loadBootstrapContractRolePlan(ctx context.Context, path string) (bootstrapContractRolePlan, error) {
	var result bootstrapContractRolePlan
	if ctx == nil {
		return result, errors.New("contract-role context is absent")
	}
	preparation, err := loadBootstrapChainPreparation(ctx, path)
	if err != nil {
		return result, err
	}
	if !bootstrapChainHasRootRole(preparation.Plan.Config.Schema) || len(preparation.Contracts.Config.Plan.Actions) < 8 {
		return result, errors.New("contract-role admission requires original v3 scope and the complete evidence CREATE graph")
	}
	evidence, err := selectEvmCreatePlan(ctx, preparation.Contracts, "evidence-create", preparation.Plan.Config.Contracts.Path)
	if err != nil {
		return result, err
	}
	if err := evidence.validateSelection(); err != nil {
		return result, err
	}
	proxy, vault, implementation := evidence.priorPlan(4), evidence.priorPlan(1), evidence.priorPlan(2)
	if evidence.EvidenceConstructor == nil || proxy.ProxyConstructor == nil {
		return result, errors.New("contract-role admission lacks the reconstructed evidence domain or proxy policy")
	}
	policy := common.HexToHash(proxy.ProxyConstructor.ApprovedPolicy.PolicyHash)
	bindings := []bootstrapContractValidatorBinding{}
	for i, inspection := range preparation.Plan.ValidatorInspections {
		if common.HexToAddress(inspection.Coordinator) != proxy.Address {
			return result, errors.New("contract-role validator coordinator differs from the approved proxy")
		}
		if common.HexToAddress(inspection.SettlementVault) != vault.Address {
			return result, errors.New("contract-role validator vault differs from the approved settlement vault")
		}
		if common.HexToHash(inspection.PolicyHash) != policy {
			return result, errors.New("contract-role validator policy differs from the approved proxy initializer")
		}
		role := preparation.Plan.Config.Validators[i]
		bindings = append(bindings, bootstrapContractValidatorBinding{Role: role.Role, ValidatorId: role.ValidatorId,
			Config: role.Config, ApprovalHash: rootObjectHash(inspection.Approval), DeclaredDeployBlock: inspection.DeployBlock})
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result = bootstrapContractRolePlan{Schema: bootstrapContractRolePlanSchema, PreparationHash: preparation.Plan.ContentHash,
		ContractPlanHash: preparation.Contracts.Config.Plan.hash(), Artifacts: preparation.Contracts.Config.Plan.Artifacts,
		CoordinatorProxy: proxy.Address, CoordinatorImplementation: implementation.Address, SettlementVault: vault.Address,
		EvidenceJournal: evidence.Address, EvidenceDomain: *evidence.EvidenceConstructor, InitialPolicyHash: policy,
		Validators: bindings, DeclarationsVerified: true, PendingChainPhases: bootstrapChainPendingPhases()}
	result.ContentHash = rootObjectHash(result)
	return result, nil
}

// This separate command leaves original preparation hashes and recovery scope
// unchanged. It has no custody, credential, online or service-start option.
func runBootstrapContractRoleCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("contract-role-plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "exact private original v3 chain configuration")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *path == "" {
		fmt.Fprintln(stderr, "contract-role-plan requires --config")
		return 2
	}
	result, err := loadBootstrapContractRolePlan(ctx, *path)
	if err != nil {
		fmt.Fprintln(stderr, "contract-role declaration admission:", err)
		return 2
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "contract-role output:", err)
		return 1
	}
	return 0
}
