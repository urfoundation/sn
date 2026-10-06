//go:build linux

// Claim history may occupy several original roots. Planning authenticates its
// complete signed-path lineage, stages fixed owners and retains one reviewable
// cohort. Actual target mutations belong to storage-prepare cohort-apply.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

const monitorClaimRestoreCohortSchema = "urnetwork-claim-monitor-restore-cohort-v1"

// Each request retains its independent source fence and target generation.
// Sorted logical paths, all count/byte dimensions and unchanged roots are bound
// before the command can produce a usable cohort reference.
type monitorClaimRestoreCohortRequest struct {
	Schema       string                                `json:"schema"`
	Expected     identityExpectation                   `json:"expected"`
	Policy       monitorClaimPolicy                    `json:"policy"`
	Original     monitorHistoryReference               `json:"original"`
	Limits       durablevolume.PreparationCohortLimits `json:"limits"`
	Preparations []durablevolume.PreparationRequest    `json:"preparations"`
}

// Read the complete source union first. Later staging may create public plan
// artifacts, but no target control/root/head changes until separate apply.
func buildMonitorClaimRestoreCohort(ctx context.Context, request monitorClaimRestoreCohortRequest) (durablevolume.PreparationCohortResult, error) {
	var empty durablevolume.PreparationCohortResult
	if err := durablepath.Require(ctx); err != nil {
		return empty, err
	}
	if request.Schema != monitorClaimRestoreCohortSchema || len(request.Preparations) < 2 || len(request.Preparations) > 256 || uint64(len(request.Preparations)) > request.Limits.MaxRoots {
		return empty, errors.New("Claim cohort schema or complete root capacity is invalid")
	}
	if err := errors.Join(request.Policy.validate(request.Expected), request.Original.validate()); err != nil {
		return empty, err
	}
	reference, declared, err := monitorHistoryRestoreDeclaration(ctx)
	if err != nil {
		return empty, err
	}
	roots := make([]*monitorHistoryRestoreRootReview, 0, len(request.Preparations))
	for index, preparation := range request.Preparations {
		if index > 0 && request.Preparations[index-1].RootPath >= preparation.RootPath {
			return empty, errors.New("Claim cohort roots must be distinct and sorted")
		}
		root, err := newMonitorHistoryRestoreRootReview(ctx, preparation, declared)
		if err != nil {
			return empty, err
		}
		roots = append(roots, root)
	}
	read := func(ref monitorHistoryReference) ([]byte, error) {
		root, err := monitorHistoryRestoreRoot(roots, ref)
		if err != nil {
			return nil, err
		}
		return root.read(ctx, ref)
	}
	if err := validateMonitorClaimRestoreHistory(ctx, request.Policy, request.Original, read); err != nil {
		return empty, err
	}
	for _, root := range roots {
		if err := root.finish(ctx); err != nil {
			return empty, err
		}
	}
	cohort := durablevolume.PreparationCohort{Schema: durablevolume.PreparationCohortSchema, Scope: "daemon", RetainedDeclaration: &reference, Limits: request.Limits}
	adapter := storagePreparationAdapter(false)
	for _, root := range roots {
		requestReference, err := durablevolume.RetainPreparationRequest(ctx, root.request)
		if err != nil {
			return empty, err
		}
		plan, err := durablepath.PlanPreparation(ctx, requestReference, adapter)
		if err != nil {
			return empty, err
		}
		planReference, err := durablevolume.RetainPreparationPlan(ctx, plan)
		if err != nil {
			return empty, err
		}
		cohort.Plans = append(cohort.Plans, planReference)
	}
	cohortReference, err := durablevolume.RetainPreparationCohort(ctx, cohort, roots[0].request.StagingDirectory)
	if err != nil {
		return empty, err
	}
	return durablepath.CheckPreparationCohort(ctx, cohortReference, adapter)
}

// A complete reviewable file reference is the public output. Lost stdout leaves
// only bounded staging artifacts; it cannot accidentally activate a target.
func runMonitorClaimArchiveRestoreCohort(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("monitor-claim-archive restore-cohort-plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("request", "", "exact complete Claim-history cohort request")
	hash := flags.String("request-sha256", "", "reviewed request digest")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *path == "" || *hash == "" {
		fmt.Fprintln(stderr, "Claim cohort requires only its exact request and digest")
		return 2
	}
	raw, digest, err := readPlanFile(ctx, *path, 8*maxRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "Claim cohort input read:", err)
		return 2
	}
	if digest != *hash {
		fmt.Fprintln(stderr, "Claim cohort input differs from reviewed digest")
		return 2
	}
	var request monitorClaimRestoreCohortRequest
	var result durablevolume.PreparationCohortResult
	if err = decodeMonitorHistoryInput(raw, &request); err == nil {
		result, err = buildMonitorClaimRestoreCohort(ctx, request)
	}
	if err != nil {
		fmt.Fprintln(stderr, "Claim cohort:", err)
		return 2
	}
	raw, err = json.Marshal(result)
	if err != nil {
		fmt.Fprintln(stderr, "Claim cohort result:", err)
		return 2
	}
	raw = append(raw, '\n')
	n, err := stdout.Write(raw)
	if err != nil || n != len(raw) {
		fmt.Fprintln(stderr, "Claim cohort result was not delivered:", errors.Join(io.ErrShortWrite, err))
		return 2
	}
	return 0
}
