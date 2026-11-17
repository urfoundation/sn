// Preparation commands only approve and retain local adoption review material.
// They expose no network route, transaction import, signer or execution option.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
)

// A preview is independently signable only within the new preparation domain.
// No signature, current chain observation or Safe authority is manufactured.
type bootstrapSuccessorPreparationPreview struct {
	Schema       string                            `json:"schema"`
	Plan         bootstrapSuccessorPreparationPlan `json:"plan"`
	PlanHash     string                            `json:"preparation_plan_hash"`
	SigningBytes string                            `json:"preparation_signing_bytes"`
}

// Positive fields describe local preparation alone. Proposed ceilings are not
// an executable allowance; all live and cross-host authority remains absent.
type bootstrapSuccessorPreparationResult struct {
	Schema                       string                           `json:"schema"`
	Status                       string                           `json:"status"`
	PlanHash                     string                           `json:"preparation_plan_hash"`
	ApprovalHash                 string                           `json:"preparation_approval_hash"`
	RecordHash                   string                           `json:"preparation_record_hash"`
	PhysicalRoot                 bootstrapSuccessorRootIdentity   `json:"physical_root"`
	ProposedBudget               bootstrapContractSuccessorBudget `json:"proposed_budget"`
	RetainedOriginalActions      uint8                            `json:"retained_original_actions"`
	PreparationApprovalVerified  bool                             `json:"preparation_approval_verified"`
	LocalPreparationComplete     bool                             `json:"local_preparation_complete"`
	ExecutionApprovalVerified    bool                             `json:"execution_approval_verified"`
	CurrentChainVerified         bool                             `json:"current_chain_verified"`
	SafeAuthorityVerified        bool                             `json:"safe_authority_verified"`
	GlobalSigningCustodyVerified bool                             `json:"global_signing_custody_verified"`
	Executable                   bool                             `json:"executable"`
	Signing                      bool                             `json:"signing"`
	NetworkEffects               bool                             `json:"network_effects"`
	InstallationComplete         bool                             `json:"installation_complete"`
	ActivationReady              bool                             `json:"activation_ready"`
}

// Every invocation rebuilds the original retained graph before using the new
// approval. Original locks remain held until fixed local custody is released.
func runBootstrapSuccessorPreparationCommand(ctx context.Context, args []string, stdout, stderr io.Writer) (resultCode int) {
	if len(args) == 0 || args[0] != "contract-successor-preview" && args[0] != "contract-successor-prepare" && args[0] != "contract-successor-resume" {
		fmt.Fprintln(stderr, "unknown local successor preparation command")
		return 2
	}
	command := args[0]
	preview := command == "contract-successor-preview"
	flags := flag.NewFlagSet("bootstrap-chain "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "original private v3 preparation config")
	runDirectory := flags.String("run-dir", "", "exact original physical custody directory")
	accepted := flags.String("accept-plan-hash", "", "original accepted v3 preparation hash")
	requestPath := flags.String("request", "", "private public-identity and incremental-budget request")
	approvalPath := flags.String("approval", "", "private independently signed local preparation envelope")
	approvalHash := flags.String("approval-sha256", "", "sha256 pin of the exact local preparation envelope file")
	successorHash := flags.String("accept-successor-hash", "", "exact previewed preparation plan hash")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *configPath == "" || *runDirectory == "" || !planSha256(*accepted) || *requestPath == "" ||
		preview && (*approvalPath != "" || *approvalHash != "" || *successorHash != "") ||
		!preview && (*approvalPath == "" || !planSha256(*approvalHash) || !planSha256(*successorHash)) {
		fmt.Fprintln(stderr, "successor preparation requires original --config, --run-dir, --accept-plan-hash and --request; prepare/resume also require --approval, --approval-sha256 and --accept-successor-hash")
		return 2
	}
	plan, retained, err := loadBootstrapSuccessorPreparation(ctx, *configPath, *runDirectory, *accepted, *requestPath, *approvalPath)
	if err != nil {
		fmt.Fprintln(stderr, "successor preparation original scope unresolved; preserve original custody:", err)
		return 1
	}
	var store *bootstrapSuccessorPreparationStore
	defer func() {
		if err := errors.Join(store.close(), retained.close()); err != nil {
			fmt.Fprintln(stderr, "successor preparation ownership close:", err)
			resultCode = 1
		}
	}()
	if preview {
		signingBytes, err := plan.signingBytes()
		err = errors.Join(err, retained.checkpoint(ctx))
		if err == nil {
			err = json.NewEncoder(stdout).Encode(bootstrapSuccessorPreparationPreview{Schema: "urnetwork-mainnet-successor-preparation-preview-v1",
				Plan: plan, PlanHash: plan.hash(), SigningBytes: "0x" + hex.EncodeToString(signingBytes)})
		}
		if err != nil {
			fmt.Fprintln(stderr, "successor preparation preview output:", err)
			return 1
		}
		return 0
	}
	if *successorHash != plan.hash() {
		fmt.Fprintln(stderr, "successor preparation hash differs from the exact retained original scope")
		return 3
	}
	raw, digest, err := readBootstrapRootFile(ctx, *approvalPath, maximumBootstrapSuccessorPreparationBytes)
	var approval bootstrapSuccessorPreparationApproval
	if err == nil && digest == *approvalHash {
		err = decodePlanJson(raw, &approval)
	} else {
		err = errors.Join(errors.New("successor preparation approval input pin differs"), err)
	}
	if err == nil {
		err = approval.validate(plan)
	}
	if err != nil {
		fmt.Fprintln(stderr, "successor preparation independent approval:", err)
		return 2
	}
	if err := retained.checkpoint(ctx); err != nil {
		fmt.Fprintln(stderr, "successor preparation original custody changed:", err)
		return 1
	}
	store, err = openBootstrapSuccessorPreparationStore(ctx, plan, approval, command == "contract-successor-prepare", nil)
	if err != nil {
		fmt.Fprintln(stderr, "successor preparation local custody unresolved; preserve its fixed files:", err)
		return 1
	}
	record := bootstrapSuccessorPreparationRecord{Schema: bootstrapSuccessorPreparationStateSchema, Approval: approval, Phase: "prepared-offline"}
	record.ContentHash = rootObjectHash(record)
	result := bootstrapSuccessorPreparationResult{Schema: "urnetwork-mainnet-successor-preparation-result-v1", Status: "prepared-offline-authority-unresolved",
		PlanHash: plan.hash(), ApprovalHash: rootObjectHash(approval), RecordHash: record.ContentHash, PhysicalRoot: plan.Root,
		ProposedBudget: plan.Proposal.Budget, RetainedOriginalActions: uint8(len(plan.Proposal.AdoptedActions)),
		PreparationApprovalVerified: true, LocalPreparationComplete: true}
	if err := retained.checkpoint(ctx); err != nil {
		fmt.Fprintln(stderr, "successor preparation original custody changed:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "successor preparation output failed; resume retained local custody:", err)
		return 1
	}
	return 0
}
