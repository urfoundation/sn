// Commands reconstruct original custody and independently approve exact bytes.
// Online resume additionally requires retained canonical review authority.
// Public current-only submission additionally requires a v2 policy acceptance
// and the exact retained acceptance hash. No complete-history claim is inferred.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The request is signable only after actual original journals, completed local
// preparation, pinned Safe release and both signature files are reconstructed.
func loadBootstrapSuccessorExecution(ctx context.Context, configPath, directory, accepted, requestPath, safeRequestPath, executionRequestPath, approvalPath string, additionalPaths ...string) (_ bootstrapSuccessorExecutionPlan, _ *safeExecutionProfile, _ *bootstrapChainReadinessState, resultErr error) {
	return loadBootstrapSuccessorExecutionRebound(ctx, configPath, directory, accepted, requestPath, safeRequestPath, executionRequestPath, approvalPath, nil, additionalPaths...)
}

// Only this explicit inspected input can borrow restored preparation. A
// preview remains read-only; resume still verifies its independent signature.
type bootstrapSuccessorLocalLoad struct {
	original bootstrapSuccessorExecutionApproval
	restore  durablevolume.Reference
	approval *bootstrapSuccessorLocalRebindApproval
}

func loadBootstrapSuccessorExecutionRebound(ctx context.Context, configPath, directory, accepted, requestPath, safeRequestPath, executionRequestPath, approvalPath string, local *bootstrapSuccessorLocalLoad, additionalPaths ...string) (_ bootstrapSuccessorExecutionPlan, _ *safeExecutionProfile, _ *bootstrapChainReadinessState, resultErr error) {
	var result bootstrapSuccessorExecutionPlan
	var request bootstrapSuccessorExecutionRequest
	raw, requestHash, err := readBootstrapRootFile(ctx, executionRequestPath, 16*1024)
	if err == nil {
		err = decodePlanJson(raw, &request)
	}
	if err == nil {
		err = request.validate()
	}
	if err != nil {
		return result, nil, nil, err
	}
	var safeRequest bootstrapSuccessorSafeRequest
	raw, safeHash, err := readBootstrapRootFile(ctx, safeRequestPath, 16*1024)
	if err == nil {
		err = decodePlanJson(raw, &safeRequest)
	}
	if err == nil {
		err = safeRequest.validate()
	}
	if err != nil {
		return result, nil, nil, err
	}
	inputs := append([]string{safeRequestPath, executionRequestPath, safeRequest.Archive.Path, request.SafeSignatures.Path, request.RelayerTransaction.Path}, additionalPaths...)
	plan, retained, err := loadBootstrapSuccessorPreparation(ctx, configPath, directory, accepted, requestPath, approvalPath, inputs...)
	if err != nil {
		return result, nil, nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, retained.close())
		}
	}()
	for _, path := range append([]string{configPath, requestPath, approvalPath}, inputs...) {
		if path == request.RegistryDirectory || strings.HasPrefix(path, request.RegistryDirectory+"/") {
			return result, nil, nil, errors.New("successor execution input overlaps the nonce registry")
		}
	}
	rawArchive, archiveHash, err := readPlanFile(ctx, safeRequest.Archive.Path, maximumSafeReleaseArchiveBytes)
	if err != nil || archiveHash != safeRequest.Archive.Sha256 {
		return result, nil, nil, errors.Join(errors.New("successor execution archive pin differs"), err)
	}
	pin, err := loadSafeReleasePin(safeRequest.Version, safeRequest.Variant)
	if err != nil {
		return result, nil, nil, err
	}
	members, err := readSafeReleaseMembers(ctx, rawArchive, pin, safeRequest.Variant)
	if err != nil {
		return result, nil, nil, err
	}
	var profile *safeExecutionProfile
	for _, artifact := range pin.Artifacts {
		if artifact.Name == safeRequest.Variant {
			profile, err = newSafeExecutionProfile(safeRequest.Version, safeRequest.Variant, members[artifact.ArchivePath])
		}
	}
	if err != nil || profile == nil {
		return result, nil, nil, errors.Join(errors.New("successor execution static profile is absent"), err)
	}
	var inspection *bootstrapSuccessorLocalInspection
	if local != nil {
		if err := local.original.validate(local.original.Plan, profile); err != nil {
			return result, nil, nil, err
		}
		originalPreparation := local.original.Plan.Review.Preparation.Approval.Plan
		plan.Root = originalPreparation.Root
		if rootObjectHash(plan) != rootObjectHash(originalPreparation) {
			return result, nil, nil, errors.New("successor local rebind changed original preparation authority")
		}
		var rebound bootstrapSuccessorLocalRebindPlan
		var restoredView *bootstrapSuccessorRestoredMemberView
		if local.approval != nil {
			if err := local.approval.validateWithView(ctx, local.original, profile, &restoredView); err != nil {
				return result, nil, nil, err
			}
			rebound = local.approval.Plan
		} else {
			rebound, err = buildBootstrapSuccessorLocalRebindWithView(ctx, local.original, profile, local.restore, &restoredView)
			if err != nil {
				return result, nil, nil, err
			}
		}
		inspection = &bootstrapSuccessorLocalInspection{preparationHash: rootObjectHash(originalPreparation), physical: rebound.RestoredLocal, restoredView: restoredView}
	}
	reader, record, err := openBootstrapSuccessorPreparationReaderRebound(ctx, plan, false, nil, inspection)
	if err != nil {
		return result, nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, reader.close()) }()
	review, err := buildBootstrapSuccessorSafeReview(ctx, plan, record, safeRequest, planFileReference{Path: safeRequestPath, Sha256: safeHash}, rawArchive)
	if err != nil {
		return result, nil, nil, err
	}
	result, err = buildBootstrapSuccessorExecution(ctx, review, request, planFileReference{Path: executionRequestPath, Sha256: requestHash}, profile)
	if err != nil {
		return result, nil, nil, err
	}
	return result, profile, retained, errors.Join(reader.checkpoint("execution-preview-reconstructed"), retained.checkpoint(ctx))
}

// Public resume defaults to reconciliation. Explicit v2 current-only acceptance
// can enable the separate bounded native route, without claiming full history.
func runBootstrapSuccessorExecutionCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runBootstrapSuccessorExecutionCommandWithProvenance(ctx, args, stdout, stderr, nil)
}

// An explicit internal capability is a test seam, never a caller-supplied flag,
// report or global mutable hook. Only local fixtures currently supply one.
func runBootstrapSuccessorExecutionCommandWithProvenance(ctx context.Context, args []string, stdout, stderr io.Writer, provenance bootstrapSuccessorSafeProvenanceAuthenticator) (resultCode int) {
	return runBootstrapSuccessorExecutionCommandWithAuthorities(ctx, args, stdout, stderr, provenance, 0)
}

// Independent acceptance and caller opt-in are separate gates. Legacy acceptance
// and a review proposal alone cannot select the public current-only route.
func runBootstrapSuccessorExecutionCommandWithAuthorities(ctx context.Context, args []string, stdout, stderr io.Writer, provenance bootstrapSuccessorSafeProvenanceAuthenticator, route bootstrapSuccessorSafeCurrentRoute) (resultCode int) {
	if len(args) == 0 || args[0] != "contract-successor-execution-preview" && args[0] != "contract-successor-execution-claim" && args[0] != "contract-successor-execution-resume" && args[0] != "contract-successor-execution-readback" && args[0] != "contract-successor-execution-rebind-preview" && args[0] != "contract-successor-execution-local-rebind-preview" {
		fmt.Fprintln(stderr, "unknown successor execution custody command")
		return 2
	}
	preview := args[0] == "contract-successor-execution-preview"
	readback := args[0] == "contract-successor-execution-readback"
	rebindPreview := args[0] == "contract-successor-execution-rebind-preview"
	localPreview := args[0] == "contract-successor-execution-local-rebind-preview"
	flags := flag.NewFlagSet("bootstrap-chain "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	if readback {
		flags.Usage = func() {
			fmt.Fprintln(stderr, "Installation readback performs network reads only; it may recover the exact retained terminal journal locally. It never submits or authorizes service start.")
			flags.PrintDefaults()
		}
	}
	configPath := flags.String("config", "", "original private v3 preparation config")
	directory := flags.String("run-dir", "", "original signed physical custody directory")
	accepted := flags.String("accept-plan-hash", "", "original accepted v3 preparation hash")
	requestPath := flags.String("request", "", "original successor preparation request")
	safeRequestPath := flags.String("safe-request", "", "retained pinned Safe review request")
	executionRequestPath := flags.String("execution-request", "", "private exact execution custody request")
	approvalPath := flags.String("approval", "", "independent successor execution approval")
	approvalHash := flags.String("approval-sha256", "", "exact approval file digest")
	executionHash := flags.String("accept-execution-hash", "", "exact previewed execution plan hash")
	online := flags.Bool("online", false, "authenticate original and current state on the originally approved owned route")
	submit := flags.Bool("submit", false, "request one counted exact write with independently accepted authority")
	canonicalPath := flags.String("canonical-approval", "", "independent build, runtime, signer-cutover and Safe-provenance authorization")
	canonicalHash := flags.String("canonical-approval-sha256", "", "exact canonical authorization file digest")
	runtimePath := flags.String("runtime-revision", "", "one additive independently signed runtime revision for exact online resume")
	runtimeHash := flags.String("runtime-revision-sha256", "", "exact additive runtime revision file digest")
	currentPath := flags.String("safe-current-revision", "", "one independently signed current-policy custody revision; import alone does not enable submission")
	currentHash := flags.String("safe-current-revision-sha256", "", "exact signed current-policy revision file digest")
	acceptedCurrent := flags.String("accept-safe-current-policy", "", "exact retained v2 acceptance object hash; opts into bounded public current-only submission")
	restorePath := flags.String("registry-restore-plan", "", "exact completed registry restore plan for independent rebind preview")
	restoreHash := flags.String("registry-restore-plan-sha256", "", "exact reviewed registry restore plan digest")
	rebindPath := flags.String("registry-rebind-approval", "", "independent original-approver physical registry rebind; resume/readback only")
	rebindHash := flags.String("registry-rebind-approval-sha256", "", "exact registry rebind approval file digest")
	localRestorePath := flags.String("local-restore-plan", "", "exact completed shared local restore plan for independent review")
	localRestoreHash := flags.String("local-restore-plan-sha256", "", "exact reviewed local restore plan digest")
	localRebindPath := flags.String("local-rebind-approval", "", "independent original-approver local physical rebind; resume/readback only")
	localRebindHash := flags.String("local-rebind-approval-sha256", "", "exact local rebind approval file digest")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *configPath == "" || *directory == "" || !planSha256(*accepted) ||
		*requestPath == "" || *safeRequestPath == "" || *executionRequestPath == "" ||
		preview && (*approvalPath != "" || *approvalHash != "" || *executionHash != "") ||
		!preview && (*approvalPath == "" || !planSha256(*approvalHash) || !planSha256(*executionHash)) ||
		(*online || *submit) && args[0] != "contract-successor-execution-resume" && !readback || *submit && !*online || readback && (!*online || *submit) ||
		*online && (*canonicalPath == "" || !planSha256(*canonicalHash)) || !*online && (*canonicalPath != "" || *canonicalHash != "") ||
		(*runtimePath != "" || *runtimeHash != "") && (!*online || *runtimePath == "" || !planSha256(*runtimeHash)) ||
		(*currentPath != "" || *currentHash != "") && (!*online || *currentPath == "" || !planSha256(*currentHash)) ||
		*acceptedCurrent != "" && (!*submit && !readback || !planSha256(*acceptedCurrent) || provenance != nil || route != 0) ||
		rebindPreview && (*restorePath == "" || !planSha256(*restoreHash) || *rebindPath != "" || *rebindHash != "" || *online || *submit) ||
		!rebindPreview && (*restorePath != "" || *restoreHash != "") ||
		(*rebindPath != "" || *rebindHash != "") && (*rebindPath == "" || !planSha256(*rebindHash) || args[0] != "contract-successor-execution-resume" && !readback && !localPreview) ||
		localPreview && (*localRestorePath == "" || !planSha256(*localRestoreHash) || *localRebindPath != "" || *localRebindHash != "" || *online || *submit) ||
		!localPreview && (*localRestorePath != "" || *localRestoreHash != "") ||
		(*localRebindPath != "" || *localRebindHash != "") && (*localRebindPath == "" || !planSha256(*localRebindHash) || args[0] != "contract-successor-execution-resume" && !readback && !rebindPreview) {
		fmt.Fprintln(stderr, "successor execution requires original --config, --run-dir, --accept-plan-hash, --request, --safe-request and --execution-request; claim/resume/rebind-preview also require --approval, --approval-sha256 and --accept-execution-hash; rebind-preview requires --registry-restore-plan and --registry-restore-plan-sha256; local-rebind-preview requires --local-restore-plan and --local-restore-plan-sha256; resume/readback may supply --registry-rebind-approval or --local-rebind-approval with their SHA-256 pins; only resume accepts --online with --canonical-approval and --canonical-approval-sha256; optional runtime/current-policy revision files and their SHA-256 pins require --online; --submit requires --online and an installed capability; public current-only submission requires --accept-safe-current-policy with the exact retained v2 acceptance object hash")
		return 2
	}
	if *acceptedCurrent != "" {
		route = bootstrapSuccessorSafeCurrentPublicRoute
	}
	if *submit && provenance == nil && route != bootstrapSuccessorSafeCurrentNativeRoute && (route != bootstrapSuccessorSafeCurrentPublicRoute || *acceptedCurrent == "") {
		fmt.Fprintln(stderr, errBootstrapSuccessorSafeProvenanceUnavailable)
		return 2
	}
	var additionalPaths []string
	if *canonicalPath != "" {
		additionalPaths = append(additionalPaths, *canonicalPath)
	}
	if *runtimePath != "" {
		additionalPaths = append(additionalPaths, *runtimePath)
	}
	if *currentPath != "" {
		additionalPaths = append(additionalPaths, *currentPath)
	}
	if *restorePath != "" {
		additionalPaths = append(additionalPaths, *restorePath)
	}
	if *rebindPath != "" {
		additionalPaths = append(additionalPaths, *rebindPath)
	}
	var local *bootstrapSuccessorLocalLoad
	if localPreview || *localRebindPath != "" {
		local = &bootstrapSuccessorLocalLoad{restore: durablevolume.Reference{Path: *localRestorePath, Sha256: *localRestoreHash}}
		raw, hash, err := readBootstrapRootFile(ctx, *approvalPath, maximumBootstrapSuccessorExecutionBytes)
		if err == nil && hash == *approvalHash {
			err = decodePlanJson(raw, &local.original)
		} else {
			err = errors.Join(errors.New("successor original approval pin differs"), err)
		}
		if err == nil && *localRebindPath != "" {
			raw, hash, err = readBootstrapRootFile(ctx, *localRebindPath, maximumBootstrapSuccessorRegistryRebindBytes)
			if err == nil && hash == *localRebindHash {
				local.approval = &bootstrapSuccessorLocalRebindApproval{}
				err = decodePlanJson(raw, local.approval)
				local.restore = local.approval.Plan.RestorePlan
			} else {
				err = errors.Join(errors.New("successor local rebind file pin differs"), err)
			}
			additionalPaths = append(additionalPaths, *localRebindPath)
		}
		if err != nil {
			fmt.Fprintln(stderr, "successor local rebind input:", err)
			return 2
		}
		additionalPaths = append(additionalPaths, local.restore.Path)
	}
	plan, profile, retained, err := loadBootstrapSuccessorExecutionRebound(ctx, *configPath, *directory, *accepted, *requestPath, *safeRequestPath, *executionRequestPath, *approvalPath, local, additionalPaths...)
	if err != nil {
		fmt.Fprintln(stderr, "successor execution original custody or exact inputs unresolved:", err)
		return 1
	}
	var owner *bootstrapSuccessorExecutionStore
	var canonical *bootstrapSuccessorCanonicalChain
	defer func() {
		if err := errors.Join(canonical.close(), owner.close(), retained.close()); err != nil {
			fmt.Fprintln(stderr, "successor execution ownership close:", err)
			resultCode = 1
		}
	}()
	if preview {
		message, err := plan.signingBytes(profile)
		err = errors.Join(err, retained.checkpoint(ctx))
		if err == nil {
			err = json.NewEncoder(stdout).Encode(struct {
				Schema       string                          `json:"schema"`
				Plan         bootstrapSuccessorExecutionPlan `json:"plan"`
				PlanHash     string                          `json:"execution_plan_hash"`
				SigningBytes string                          `json:"execution_signing_bytes"`
			}{Schema: "urnetwork-mainnet-successor-execution-preview-v1", Plan: plan, PlanHash: plan.hash(), SigningBytes: "0x" + hex.EncodeToString(message)})
		}
		if err != nil {
			fmt.Fprintln(stderr, "successor execution preview output:", err)
			return 1
		}
		return 0
	}
	raw, digest, err := readBootstrapRootFile(ctx, *approvalPath, maximumBootstrapSuccessorExecutionBytes)
	var approval bootstrapSuccessorExecutionApproval
	if err == nil && digest == *approvalHash {
		err = decodePlanJson(raw, &approval)
	} else {
		err = errors.Join(errors.New("successor execution approval file digest differs"), err)
	}
	if err == nil && (rebindPreview || *rebindPath != "") {
		// Only the separately reviewed physical coordinate can differ. The old
		// signature domain and accepted execution hash remain byte-identical.
		plan.Registry = approval.Plan.Registry
	}
	if err == nil && plan.hash() != *executionHash {
		fmt.Fprintln(stderr, "successor execution accepted hash differs from reconstructed original custody")
		return 3
	}
	if err == nil {
		err = approval.validate(plan, profile)
	}
	if err != nil {
		fmt.Fprintln(stderr, "successor independent execution approval:", err)
		return 2
	}
	if rebindPreview {
		rebindPlan, err := buildBootstrapSuccessorRegistryRebind(ctx, approval, profile, durablevolume.Reference{Path: *restorePath, Sha256: *restoreHash})
		var result any
		if err == nil {
			result, err = rebindPlan.preview(approval, profile)
		}
		if err = errors.Join(err, retained.checkpoint(ctx)); err == nil {
			err = json.NewEncoder(stdout).Encode(result)
		}
		if err != nil {
			fmt.Fprintln(stderr, "successor registry rebind review:", err)
			return 1
		}
		return 0
	}
	if localPreview {
		rebindPlan, err := buildBootstrapSuccessorLocalRebind(ctx, approval, profile, local.restore)
		var result any
		if err == nil {
			result, err = rebindPlan.preview(approval, profile)
		}
		if err = errors.Join(err, retained.checkpoint(ctx)); err == nil {
			err = json.NewEncoder(stdout).Encode(result)
		}
		if err != nil {
			fmt.Fprintln(stderr, "successor local rebind review:", err)
			return 1
		}
		return 0
	}
	var rebind *bootstrapSuccessorRegistryRebindApproval
	if *rebindPath != "" {
		raw, hash, err := readBootstrapRootFile(ctx, *rebindPath, maximumBootstrapSuccessorRegistryRebindBytes)
		var decoded bootstrapSuccessorRegistryRebindApproval
		if err == nil && hash == *rebindHash {
			err = decodePlanJson(raw, &decoded)
		} else {
			err = errors.Join(errors.New("successor registry rebind file pin differs"), err)
		}
		if err != nil {
			fmt.Fprintln(stderr, "successor registry rebind approval:", err)
			return 2
		}
		rebind = &decoded
	}
	if err := retained.checkpoint(ctx); err != nil {
		fmt.Fprintln(stderr, "successor execution original custody changed:", err)
		return 1
	}
	var localApproval *bootstrapSuccessorLocalRebindApproval
	if local != nil {
		localApproval = local.approval
	}
	owner, err = openBootstrapSuccessorExecutionStoreWithPhysicalRebind(ctx, plan, approval, profile, args[0] == "contract-successor-execution-claim", nil, rebind, localApproval)
	if err != nil {
		fmt.Fprintln(stderr, "successor execution custody unresolved; retain original and nonce registry files:", err)
		return 1
	}
	result := owner.result()
	if *online {
		raw, hash, err := readBootstrapRootFile(ctx, *canonicalPath, 16*1024)
		var canonicalApproval bootstrapSuccessorCanonicalApproval
		if err == nil && hash == *canonicalHash {
			err = decodePlanJson(raw, &canonicalApproval)
		} else {
			err = errors.Join(errors.New("successor canonical authorization file pin differs"), err)
		}
		if err == nil {
			err = canonicalApproval.validate(ctx, plan)
		}
		if err != nil {
			fmt.Fprintln(stderr, "successor independent canonical authorization:", err)
			return 2
		}
		var revisions []bootstrapSuccessorRuntimeApproval
		if *runtimePath != "" {
			raw, hash, err := readBootstrapRootFile(ctx, *runtimePath, maximumBootstrapSuccessorRuntimeBytes)
			var revision bootstrapSuccessorRuntimeApproval
			if err == nil && hash == *runtimeHash {
				err = decodePlanJson(raw, &revision)
			} else {
				err = errors.Join(errors.New("successor runtime revision file pin differs"), err)
			}
			if err == nil {
				err = revision.validate(ctx, plan, canonicalApproval)
			}
			if err != nil {
				fmt.Fprintln(stderr, "successor independent runtime revision:", err)
				return 2
			}
			revisions = append(revisions, revision)
		}
		var current []bootstrapSuccessorSafeCurrentRevisionApproval
		if *currentPath != "" {
			raw, hash, err := readBootstrapRootFile(ctx, *currentPath, maximumBootstrapSuccessorSafeCurrentRevisionBytes)
			var revision bootstrapSuccessorSafeCurrentRevisionApproval
			if err == nil && hash == *currentHash {
				err = decodePlanJson(raw, &revision)
			} else {
				err = errors.Join(errors.New("successor current-policy revision file pin differs"), err)
			}
			if err != nil {
				fmt.Fprintln(stderr, "successor independent current-policy revision:", err)
				return 2
			}
			current = append(current, revision)
		}
		if err := retained.checkpoint(ctx); err != nil {
			fmt.Fprintln(stderr, "successor canonical original custody changed:", err)
			return 1
		}
		canonical, err = newBootstrapSuccessorCanonicalChainWithAuthorities(ctx, owner, canonicalApproval, provenance, route, current, revisions...)
		if err == nil && route == bootstrapSuccessorSafeCurrentPublicRoute && owner.safeCurrentHistory.hash() != *acceptedCurrent {
			err = errors.New("successor public current-policy opt-in differs from the exact retained acceptance")
		}
		if err == nil {
			if readback {
				var installation bootstrapContractInstallation
				installation, err = inspectBootstrapContractInstallation(ctx, owner, canonical)
				err = errors.Join(err, retained.checkpoint(ctx))
				if err == nil {
					if err := json.NewEncoder(stdout).Encode(installation); err != nil {
						fmt.Fprintln(stderr, "successor installation readback output:", err)
						return 1
					}
					return 0
				}
			} else {
				result, err = advanceBootstrapSuccessorExecution(ctx, owner, canonical, *submit)
			}
		}
		if err != nil {
			fmt.Fprintln(stderr, "successor canonical execution unresolved; retain signatures, authority and cumulative custody:", err)
			return 1
		}
	}
	if err := retained.checkpoint(ctx); err != nil {
		fmt.Fprintln(stderr, "successor execution original custody changed:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "successor execution output failed; resume exact custody:", err)
		return 1
	}
	return 0
}
