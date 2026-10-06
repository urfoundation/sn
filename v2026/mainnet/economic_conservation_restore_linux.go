//go:build linux

// Complete combined-owner restore keeps original logical paths and financial
// evidence. Physical source review selects every checkpoint/archive owner;
// target publication remains a separate storage-prepare operation.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

const economicConservationRestoreSchema = "urnetwork-economic-conservation-restore-v1"

// Each original root retains its own stopped-writer fence and exact target
// generation. Additional co-owners require explicit complete-union coverage.
type economicConservationRestoreRequest struct {
	Schema       string                                `json:"schema"`
	Policy       economicConservationPolicy            `json:"original_policy"`
	Original     monitorHistoryReference               `json:"original_checkpoint"`
	Limits       durablevolume.PreparationCohortLimits `json:"limits"`
	Preparations []durablevolume.PreparationRequest    `json:"preparations"`
}

// The same semantic archive reader reconstructs Claim windows, fee revisions,
// immutable receipts, carry and native source provenance. Its temporary index
// cannot be used by a live worker: all target owners must later reopen normally.
func validateEconomicConservationRestoreHistory(ctx context.Context, policy economicConservationPolicy, original monitorHistoryReference, read func(monitorHistoryReference) ([]byte, error), reread func(context.Context, monitorHistoryReference) ([]byte, error), hooks monitorServiceHooks) error {
	if err := errors.Join(ctx.Err(), policy.validate(), policy.validateReference(original)); err != nil {
		return err
	}
	if read == nil {
		return errors.New("economic restore requires an original source reader")
	}
	readExact := func(reference monitorHistoryReference) ([]byte, error) {
		if err := policy.validateReference(reference); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := read(reference)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if uint64(len(raw)) != reference.Bytes || monitorReadDigest(raw) != reference.Sha256 {
			return nil, errors.New("economic restore returned bytes differ from original reference")
		}
		return raw, nil
	}
	raw, err := readExact(original)
	if err != nil {
		return err
	}
	state, err := decodeEconomicConservation(ctx, raw, policy)
	if err != nil {
		return err
	}
	canonical, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if !bytes.Equal(append(canonical, '\n'), raw) {
		return errors.New("economic restore requires exact original checkpoint framing")
	}
	if state.Renewal != nil && state.Renewal.Original.Path != original.Path || state.FeeRevision != nil && state.FeeRevision.Original.Path != original.Path {
		return errors.New("economic restore moved an original signed operational revision")
	}
	if state.NativeRenewal != nil && state.NativeRenewal.Original.Path != original.Path {
		return errors.New("economic restore moved an original signed native adoption")
	}
	if err := state.validateClaimCheckpointPath(original.Path); err != nil {
		return err
	}
	view, err := readEconomicConservationArchive(ctx, policy, state, hooks, func(_ context.Context, reference monitorHistoryReference) (*monitorHistorySnapshot, []byte, error) {
		if monitorHistoryPathsAlias(reference.Path, original.Path) {
			return nil, nil, errors.New("economic restore archive aliases original checkpoint custody")
		}
		raw, err := readExact(reference)
		return nil, raw, err
	}, func(readContext context.Context, reference monitorHistoryReference) ([]byte, error) {
		if err := errors.Join(ctx.Err(), readContext.Err(), policy.validateReference(reference)); err != nil {
			return nil, err
		}
		if reread == nil {
			return nil, errors.New("economic copied principal history requires an admitted original rereader")
		}
		return reread(readContext, reference)
	})
	if err != nil {
		return err
	}
	state.archiveView = view
	return errors.Join(ctx.Err(), state.validateClaimCheckpointPath(original.Path), view.close())
}

// Read all copied original bytes before staging any plan. A missing later
// root, read outage or contradictory history cannot create partial authority.
func reviewEconomicConservationRestore(ctx context.Context, request economicConservationRestoreRequest, hooks monitorServiceHooks) (durablevolume.Reference, []*monitorHistoryRestoreRootReview, error) {
	var empty durablevolume.Reference
	if err := durablepath.Require(ctx); err != nil {
		return empty, nil, err
	}
	if request.Schema != economicConservationRestoreSchema || len(request.Preparations) == 0 || len(request.Preparations) > 256 || uint64(len(request.Preparations)) > request.Limits.MaxRoots {
		return empty, nil, errors.New("economic restore requires the complete bounded original root census")
	}
	if err := errors.Join(request.Policy.validate(), request.Policy.validateReference(request.Original)); err != nil {
		return empty, nil, err
	}
	declaration, declared, err := monitorHistoryRestoreDeclaration(ctx)
	if err != nil {
		return empty, nil, err
	}
	roots := make([]*monitorHistoryRestoreRootReview, 0, len(request.Preparations))
	for index, preparation := range request.Preparations {
		if index > 0 && request.Preparations[index-1].RootPath >= preparation.RootPath {
			return empty, nil, errors.New("economic restore roots must be unique and sorted")
		}
		root, err := newMonitorHistoryRestoreRootReview(ctx, preparation, declared)
		if err != nil {
			return empty, nil, err
		}
		roots = append(roots, root)
	}
	ctx = context.WithValue(ctx, nativeProducerRuntimeReadKey{}, func(ctx context.Context, reference planFileReference, maximum int) ([]byte, error) {
		raw, _, err := readNativeProducerRestoreOriginal(ctx, reference, maximum, roots, declared)
		return raw, err
	})
	var originalRaw []byte
	if err := validateEconomicConservationRestoreHistory(ctx, request.Policy, request.Original, func(reference monitorHistoryReference) ([]byte, error) {
		root, err := monitorHistoryRestoreRootLimit(roots, reference, request.Policy.storageMaximum())
		if err != nil {
			return nil, err
		}
		raw, err := root.readProfile(ctx, reference, request.Policy.storageKind(), int(request.Policy.storageMaximum()))
		if err == nil && reference == request.Original {
			originalRaw = raw
		}
		return raw, err
	}, func(readContext context.Context, reference monitorHistoryReference) ([]byte, error) {
		root, err := monitorHistoryRestoreRootLimit(roots, reference, request.Policy.storageMaximum())
		if err != nil {
			return nil, err
		}
		return root.rereadProfile(readContext, reference, request.Policy.storageKind(), int(request.Policy.storageMaximum()))
	}, hooks); err != nil {
		return empty, nil, err
	}
	if execution := request.Policy.Native.Observation.Execution; execution != nil && execution.Producer != nil {
		state, err := decodeEconomicConservation(ctx, originalRaw, request.Policy)
		if err != nil {
			return empty, nil, err
		}
		nativePolicy, err := state.nativeOperatingPolicy(request.Policy)
		if err != nil {
			return empty, nil, err
		}
		if err := reviewEconomicNativeProducerRestore(ctx, nativePolicy.Observation, request.Original, request.Policy.StorageProfile, state.Native, roots, declared); err != nil {
			return empty, nil, err
		}
	}
	for _, root := range roots {
		if err := root.finish(ctx); err != nil {
			return empty, nil, err
		}
	}
	return declaration, roots, ctx.Err()
}

// Cohort staging uses the existing complete-union adapter and keeps untouched
// roots under their original reserve and marker authority. No service restarts.
func buildEconomicConservationRestoreCohort(ctx context.Context, request economicConservationRestoreRequest, hooks monitorServiceHooks) (durablevolume.PreparationCohortResult, error) {
	var result durablevolume.PreparationCohortResult
	if len(request.Preparations) < 2 {
		return result, errors.New("economic restore cohort requires at least two original roots")
	}
	declaration, roots, err := reviewEconomicConservationRestore(ctx, request, hooks)
	if err != nil {
		return result, err
	}
	cohort := durablevolume.PreparationCohort{Schema: durablevolume.PreparationCohortSchema, Scope: "daemon", RetainedDeclaration: &declaration, Limits: request.Limits}
	adapter := storagePreparationAdapter(false)
	for _, root := range roots {
		requestReference, err := durablevolume.RetainPreparationRequest(ctx, root.request)
		if err != nil {
			return result, err
		}
		plan, err := durablepath.PlanPreparation(ctx, requestReference, adapter)
		if err != nil {
			return result, err
		}
		planReference, err := durablevolume.RetainPreparationPlan(ctx, plan)
		if err != nil {
			return result, err
		}
		cohort.Plans = append(cohort.Plans, planReference)
	}
	reference, err := durablevolume.RetainPreparationCohort(ctx, cohort, roots[0].request.StagingDirectory)
	if err != nil {
		return result, err
	}
	return durablepath.CheckPreparationCohort(ctx, reference, adapter)
}

func runEconomicConservationRestore(ctx context.Context, args []string, stdout, stderr io.Writer, hooks monitorServiceHooks) int {
	if len(args) == 0 || args[0] != "restore-request" && args[0] != "restore-cohort-plan" {
		fmt.Fprintln(stderr, "economic-conservation-archive restore-request|restore-cohort-plan --request FILE --request-sha256 HASH")
		return 2
	}
	mode := args[0]
	flags := flag.NewFlagSet("economic-conservation-archive "+mode, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("request", "", "exact copied-source restore request")
	pin := flags.String("request-sha256", "", "sha256:DIGEST of request bytes")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *path == "" || !planSha256(*pin) {
		return 2
	}
	raw, digest, err := readPlanFile(ctx, *path, 8*maxRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "economic restore input read:", err)
		return 2
	}
	if digest != *pin {
		fmt.Fprintln(stderr, "economic restore input differs from exact reviewed bytes")
		return 2
	}
	var request economicConservationRestoreRequest
	if err = decodeMonitorHistoryInput(raw, &request); err == nil {
		if mode == "restore-request" {
			if len(request.Preparations) != 1 {
				err = errors.New("economic single-root request cannot omit a cohort")
			} else {
				var roots []*monitorHistoryRestoreRootReview
				_, roots, err = reviewEconomicConservationRestore(ctx, request, hooks)
				if err == nil {
					raw, err = json.Marshal(roots[0].request)
				}
			}
		} else {
			var result durablevolume.PreparationCohortResult
			result, err = buildEconomicConservationRestoreCohort(ctx, request, hooks)
			if err == nil {
				raw, err = json.Marshal(result)
			}
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "economic restore:", err)
		return 2
	}
	raw = append(raw, '\n')
	written, err := stdout.Write(raw)
	if err != nil || written != len(raw) {
		fmt.Fprintln(stderr, "economic restore request not delivered:", errors.Join(io.ErrShortWrite, err))
		return 2
	}
	return 0
}
