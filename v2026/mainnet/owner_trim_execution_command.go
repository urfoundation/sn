// The public trim command owns original custody and public signature recovery.
// Strict actions require enforced authority; a fresh best-effort domain requires
// explicit residual-risk approval and the original imported owner signature.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// The current binary can prepare/import/reconcile an approved action. Its
// executor never supplies a native signer. Best-effort submission is a separate
// opt-in mode and never manufactures the strict future-window capability.
func runBootstrapTrimCommand(ctx context.Context, args []string, stdout, stderr io.Writer) (result int) {
	mode := args[0]
	if mode != "trim-plan" && mode != "trim-apply" && mode != "trim-resume" && mode != "trim-import" && mode != "trim-reconcile" && mode != "trim-export" && mode != "trim-import-reply" && mode != "trim-submit-plan" && mode != "trim-submit" {
		fmt.Fprintln(stderr, "bootstrap-chain requires trim-plan, trim-apply, trim-resume, trim-export, trim-import, trim-import-reply, trim-reconcile, trim-submit-plan or trim-submit")
		return 2
	}
	flags := flag.NewFlagSet("bootstrap-chain "+mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "exact original v3 or passive-root v4 chain config")
	runDir := flags.String("run-dir", "", "original precreated private custody directory")
	accepted := flags.String("accept-plan-hash", "", "exact original accepted v3 preparation")
	trimPath := flags.String("trim-config", "", "private independently approved trim config; unsigned template for trim-plan")
	key := flags.String("trim-approval-key", "", "independently provisioned trim approval public key")
	var metadataPath, ledgerMetadataPath, signaturePath, requestPath, requestHash, replyPath, replyHash string
	var submissionPath, submissionHash, submissionKey string
	var pruningPolicy, registrationPolicy string
	if mode == "trim-plan" || mode == "trim-export" {
		flags.StringVar(&metadataPath, "metadata", "", "private file containing exact pinned runtime metadata hex")
		flags.StringVar(&ledgerMetadataPath, "ledger-metadata", "", "Ledger domains: independently approved unwrapped metadata15 hex")
	}
	if mode == "trim-import" {
		flags.StringVar(&signaturePath, "signature", "", "original public native signature file, never a private key")
	}
	if mode == "trim-import-reply" {
		flags.StringVar(&requestPath, "request", "", "original exported portable owner request")
		flags.StringVar(&requestHash, "accept-request-hash", "", "exact exported request content hash")
		flags.StringVar(&replyPath, "reply", "", "owner's original public signed reply")
		flags.StringVar(&replyHash, "reply-sha256", "", "sha256 pin of the original reply file")
	}
	if mode == "trim-submit" {
		flags.StringVar(&submissionPath, "submission-policy", "", "private independently signed best-effort submission policy")
		flags.StringVar(&submissionHash, "submission-policy-sha256", "", "sha256 pin of the exact signed submission policy file")
		flags.StringVar(&submissionKey, "submission-approval-key", "", "independently provisioned submission approval public key")
	}
	if mode == "trim-submit-plan" {
		flags.StringVar(&pruningPolicy, "public-pruning-policy", ownerTrimRequirePruningImmunity, "unsigned choice: require-public-pruning-immunity-through-original-expiry or accept-public-pruning-and-netuid-reuse-risk")
		flags.StringVar(&registrationPolicy, "registration-policy", ownerTrimRequireClosedRegistration, "unsigned choice: require-observed-closed-registration or accept-competing-registration-and-reentry-risk")
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *configPath == "" || *trimPath == "" || *runDir == "" ||
		!planSha256(*accepted) || !rootCanonicalHash(*key) || (mode == "trim-plan" || mode == "trim-export") && metadataPath == "" || mode == "trim-import" && signaturePath == "" ||
		mode == "trim-import-reply" && (requestPath == "" || replyPath == "" || !planSha256(requestHash) || !planSha256(replyHash)) ||
		mode == "trim-submit" && (submissionPath == "" || !planSha256(submissionHash) || !rootCanonicalHash(submissionKey)) {
		fmt.Fprintln(stderr, "trim phase requires original --config, --run-dir, --accept-plan-hash, --trim-config and independent --trim-approval-key")
		return 2
	}
	preparation, err := loadBootstrapChainPreparation(ctx, *configPath)
	if err != nil || !bootstrapChainHasRootRole(preparation.Plan.Config.Schema) || preparation.Plan.ContentHash != *accepted || preparation.Plan.Config.RunDirectory != *runDir {
		fmt.Fprintln(stderr, "trim phase original preparation differs:", err)
		return 3
	}
	raw, _, err := readBootstrapRootFile(ctx, *trimPath, maxRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "trim phase input:", err)
		return 2
	}
	var config ownerTrimExecutionConfig
	if err := decodePlanJson(raw, &config); err != nil {
		fmt.Fprintln(stderr, "trim phase config:", err)
		return 2
	}
	ledgerMetadata := ""
	if mode == "trim-plan" || mode == "trim-export" {
		if (config.Schema == ownerTrimLedgerExecutionSchema || config.Schema == ownerTrimBestEffortExecutionSchema) != (ledgerMetadataPath != "") {
			fmt.Fprintln(stderr, "trim Ledger domains require --ledger-metadata; v1 keeps its original metadata profile")
			return 2
		}
		if ledgerMetadataPath != "" {
			raw, _, err := readBootstrapRootFile(ctx, ledgerMetadataPath, 2*maxMetadataRpcReplyBytes+3)
			if err != nil {
				fmt.Fprintln(stderr, "trim Ledger metadata:", err)
				return 2
			}
			ledgerMetadata = strings.TrimSpace(string(raw))
			hash, err := ownerLedgerMetadataHash(ledgerMetadata)
			if err != nil || hash != config.Action.LedgerMetadataHash {
				fmt.Fprintln(stderr, "trim Ledger metadata differs from template/approval:", err)
				return 2
			}
		}
	}
	encoder := json.NewEncoder(stdout)
	if mode == "trim-plan" {
		retained, err := openBootstrapChainReadinessState(ctx, preparation)
		if err != nil {
			fmt.Fprintln(stderr, "trim plan original custody:", err)
			return 3
		}
		defer func() {
			if err := retained.close(); err != nil {
				fmt.Fprintln(stderr, "trim plan close:", err)
				result = 1
			}
		}()
		policyRaw, err := readBootstrapChainInput(ctx, preparation.Plan.Config.OwnerTrimPolicy, maxRpcReplyBytes)
		var policy subnetCensusPolicy
		if err != nil || decodePlanJson(policyRaw, &policy) != nil || policy.validate() != nil {
			fmt.Fprintln(stderr, "trim plan original policy is unavailable or invalid:", err)
			return 3
		}
		reviewRaw, err := readBootstrapChainInput(ctx, preparation.Plan.Config.OwnerTrimPlan, maximumOwnerTrimPlanBytes)
		var review ownerTrimPlan
		if err != nil || decodePlanJson(reviewRaw, &review) != nil || validateOwnerTrimGuardPlan(policy, preparation.Plan.Config.OwnerTrimPolicy.Sha256, review) != nil {
			fmt.Fprintln(stderr, "trim plan original review is unavailable or invalid:", err)
			return 3
		}
		if config.Action.Schema == "" {
			config.Action.Schema = ownerTrimActionSchema
			if config.Schema == ownerTrimLedgerExecutionSchema {
				config.Action.Schema = ownerTrimLedgerActionSchema
			}
			if config.Schema == ownerTrimBestEffortExecutionSchema {
				config.Action.Schema = ownerTrimBestEffortActionSchema
			}
		}
		if config.Signature != "" || !config.validSchemas() || config.Route.validate() != nil || config.Route.ReadRetrySeconds < 60 || config.Route.ReadRetrySeconds > 900 || config.Route.SendTimeoutSeconds == 0 || config.Route.SendTimeoutSeconds > 60 {
			fmt.Fprintln(stderr, "trim-plan requires an unsigned bounded action/route template")
			return 2
		}
		action := config.Action
		action.PreparationHash, action.PreparationStateHash = preparation.Plan.ContentHash, rootObjectHash(retained)
		action.PolicyHash, action.ReviewHash, action.Network = preparation.Plan.Config.OwnerTrimPolicy.Sha256, review.ContentHash, preparation.Plan.Config.Network
		action.Runtime = rootReceiptProfile{RuntimeSourceCommit: policy.RuntimeSourceCommit, RuntimeVersion: policy.RuntimeVersion, RuntimeCodeHash: policy.RuntimeCodeHash, RuntimeMetadataHash: policy.RuntimeMetadataHash}
		action.StatePath, action.Coldkey, action.Netuid = filepath.Join(*runDir, ownerTrimStateFile), policy.SubnetOwnerColdkey, policy.Netuid
		action.SubnetRegistrationBlock, action.SubnetGeneration, action.MaximumUids, action.SelectionRule = *policy.SubnetRegistrationBlock, *policy.SubnetGeneration, review.Best.MaximumUids, ownerTrimSubsetRule
		if config.Schema == ownerTrimBestEffortExecutionSchema {
			action.SelectionRule = ownerTrimBestEffortSelection
		}
		metadata, _, err := readBootstrapRootFile(ctx, metadataPath, 2*maxMetadataRpcReplyBytes+3)
		if err == nil {
			config.Action, err = prepareOwnerTrimAction(action, strings.TrimSpace(string(metadata)))
		}
		if err != nil {
			fmt.Fprintln(stderr, "trim plan metadata/action:", err)
			return 2
		}
		if err := retained.checkpoint(ctx); err != nil {
			fmt.Fprintln(stderr, "trim plan original custody changed:", err)
			return 3
		}
		if err := encoder.Encode(config); err != nil {
			fmt.Fprintln(stderr, "trim plan output:", err)
			return 1
		}
		return 0
	}
	// The new config, public signature and markers must never name each other.
	for _, input := range []string{*trimPath, signaturePath, metadataPath, ledgerMetadataPath, requestPath, replyPath, submissionPath} {
		if input != "" && (!bootstrapRootAbsolutePath(input) || input == config.Action.StatePath || input == config.Action.StatePath+".lock") {
			fmt.Fprintln(stderr, "trim input overlaps the fixed action journal or marker")
			return 2
		}
	}
	store, err := openOwnerTrimStore(ctx, preparation, config, *key, mode == "trim-apply")
	if err != nil {
		fmt.Fprintln(stderr, "trim original retained ownership:", err)
		return 3
	}
	defer func() {
		if err := store.close(); err != nil {
			fmt.Fprintln(stderr, "trim phase close:", err)
			result = 1
		}
	}()
	owner := ownerTrimExecutor{config: config, key: *key, store: store}
	record, err := store.load()
	if err != nil {
		fmt.Fprintln(stderr, "trim phase retained action:", err)
		return 3
	}
	step := ownerTrimRetainedResult(record)
	if mode == "trim-submit-plan" {
		approval, err := store.bestEffortApprovalTemplate()
		if err != nil {
			fmt.Fprintln(stderr, "trim submission planning:", err)
			return 3
		}
		if pruningPolicy != ownerTrimRequirePruningImmunity && pruningPolicy != ownerTrimAcceptPruningRisk || registrationPolicy != ownerTrimRequireClosedRegistration && registrationPolicy != ownerTrimAcceptRegistrationRisk {
			fmt.Fprintln(stderr, "trim submission planning requires explicit supported pruning and registration policy choices")
			return 2
		}
		approval.PublicPruningPolicy, approval.RegistrationPolicy = pruningPolicy, registrationPolicy
		approval.ResidualRisks = approval.residuals()
		if _, err := store.load(); err != nil {
			fmt.Fprintln(stderr, "trim submission planning custody changed:", err)
			return 3
		}
		if err := encoder.Encode(struct {
			Approval     ownerTrimBestEffortApproval `json:"approval_template"`
			SigningBytes string                      `json:"signing_bytes"`
		}{Approval: approval, SigningBytes: "0x" + hex.EncodeToString(approval.signingBytes())}); err != nil {
			fmt.Fprintln(stderr, "trim submission policy output:", err)
			return 1
		}
		return 0
	}
	if mode == "trim-export" {
		if record.Phase != "reserved" || record.Signature != "" {
			fmt.Fprintln(stderr, "trim export requires original reserved custody; recover any existing or uncertain signature")
			return 3
		}
		metadata, _, err := readBootstrapRootFile(ctx, metadataPath, 2*maxMetadataRpcReplyBytes+3)
		if err != nil {
			fmt.Fprintln(stderr, "trim export metadata:", err)
			return 2
		}
		request, err := newOwnerSigningRequest(config, *key, strings.TrimSpace(string(metadata)), ledgerMetadata)
		if err != nil {
			fmt.Fprintln(stderr, "trim export request:", err)
			return 2
		}
		if _, err := store.load(); err != nil {
			fmt.Fprintln(stderr, "trim export original custody changed:", err)
			return 3
		}
		if err := encoder.Encode(request); err != nil {
			fmt.Fprintln(stderr, "trim export output:", err)
			return 1
		}
		return 0
	}
	if mode == "trim-import" || mode == "trim-import-reply" {
		var signature []byte
		if mode == "trim-import" {
			encoded, _, err := readBootstrapRootFile(ctx, signaturePath, 129)
			var decodeErr error
			signature, decodeErr = rootOfflineSignatureBytes(strings.TrimSpace(string(encoded)))
			if err != nil || decodeErr != nil {
				fmt.Fprintln(stderr, "trim original public signature:", errors.Join(err, decodeErr))
				return 2
			}
		} else {
			request, err := readOwnerSigningRequest(ctx, requestPath, ownerSigningTrust{RequestHash: requestHash,
				ApprovalKey: *key, Owner: config.Action.Coldkey, Genesis: config.Action.Network.GenesisHash})
			if err != nil || rootObjectHash(request.Config) != rootObjectHash(config) {
				fmt.Fprintln(stderr, "trim reply request differs from original retained config:", err)
				return 3
			}
			reply, err := readOwnerSigningReply(ctx, replyPath, replyHash)
			if err == nil {
				signature, err = reply.validate(request)
			}
			if err != nil {
				fmt.Fprintln(stderr, "trim original owner reply:", err)
				return 2
			}
		}
		signed, err := config.Action.signed(signature)
		if err != nil || record.Signature != "" && record.Signature != hex.EncodeToString(signature) || record.Signature == "" && record.Phase != "reserved" {
			fmt.Fprintln(stderr, "trim signature cannot replace original custody or resolve unknown signing:", err)
			return 3
		}
		if record.Signature == "" {
			record.Phase, record.Signature = "signed", hex.EncodeToString(signature)
			record.RawExtrinsic, record.ExtrinsicHash = "0x"+hex.EncodeToString(signed), rootExtrinsicHash(signed)
			if err := owner.persist(record); err != nil {
				fmt.Fprintln(stderr, "trim public signature durability:", err)
				return 1
			}
		}
		step = ownerTrimRetainedResult(record)
	}
	if mode == "trim-reconcile" || mode == "trim-submit" {
		chain, err := newOwnerTrimCanonicalChain(config, *key, store.policy, nil)
		if err != nil {
			fmt.Fprintln(stderr, "trim owned observation route:", err)
			return 2
		}
		owner.chain = chain
		if mode == "trim-submit" {
			raw, digest, err := readBootstrapRootFile(ctx, submissionPath, maxRpcReplyBytes)
			var approval ownerTrimBestEffortApproval
			if err != nil || digest != submissionHash {
				fmt.Fprintln(stderr, "trim submission policy input differs:", err)
				return 2
			}
			if err := decodePlanJson(raw, &approval); err != nil {
				fmt.Fprintln(stderr, "trim submission policy:", err)
				return 2
			}
			bestEffort, err := newOwnerTrimBestEffortChain(store, approval, submissionKey)
			if err != nil {
				fmt.Fprintln(stderr, "trim separate best-effort approval:", err)
				return 3
			}
			owner.chain, owner.authority = bestEffort, bestEffort
		}
		step, err = owner.step(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "trim reconciliation unresolved or effects blocked; retain original custody:", err)
			result = 1
		}
	}
	record, err = store.load()
	if err != nil {
		fmt.Fprintln(stderr, "trim retained result:", err)
		return 1
	}
	if err := encoder.Encode(struct {
		Step   ownerTrimStepResult `json:"step"`
		Record ownerTrimRecord     `json:"retained_action"`
	}{Step: step, Record: record}); err != nil {
		fmt.Fprintln(stderr, "trim result output; resume original custody:", err)
		return 1
	}
	return result
}
