// The public bridge only reads retained custody and pinned local release bytes.
// It has no RPC, signature import, approval, recovery or execution option.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
)

// Successful review exits blocked with a complete sealed result. An invalid or
// incomplete original/prepared claim emits no result and is never repaired.
func runBootstrapSuccessorSafeReviewCommand(ctx context.Context, args []string, stdout, stderr io.Writer) (resultCode int) {
	flags := flag.NewFlagSet("bootstrap-chain contract-successor-safe-review", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "original private v3 preparation config")
	runDirectory := flags.String("run-dir", "", "exact original physical custody directory")
	accepted := flags.String("accept-plan-hash", "", "original accepted v3 preparation hash")
	requestPath := flags.String("request", "", "original private successor preparation request")
	safeRequestPath := flags.String("safe-request", "", "private pinned Safe profile and relayer review request")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *configPath == "" || *runDirectory == "" || !planSha256(*accepted) || *requestPath == "" || *safeRequestPath == "" {
		fmt.Fprintln(stderr, "successor Safe review requires --config, --run-dir, --accept-plan-hash, --request and --safe-request")
		return 2
	}
	raw, requestHash, err := readBootstrapRootFile(ctx, *safeRequestPath, 16*1024)
	var request bootstrapSuccessorSafeRequest
	if err == nil {
		err = decodePlanJson(raw, &request)
	}
	if err == nil {
		err = request.validate()
	}
	if err != nil {
		fmt.Fprintln(stderr, "successor Safe review request:", err)
		return 2
	}
	plan, retained, err := loadBootstrapSuccessorPreparation(ctx, *configPath, *runDirectory, *accepted, *requestPath, *safeRequestPath, request.Archive.Path)
	if err != nil {
		fmt.Fprintln(stderr, "successor Safe review original scope unresolved; preserve original custody:", err)
		return 1
	}
	var reader *bootstrapSuccessorPreparationStore
	defer func() {
		if err := errors.Join(reader.close(), retained.close()); err != nil {
			fmt.Fprintln(stderr, "successor Safe review ownership close:", err)
			resultCode = 1
		}
	}()
	if request.PreparationPlanHash != plan.hash() {
		fmt.Fprintln(stderr, "successor Safe review preparation hash differs from the reconstructed original scope")
		return 3
	}
	var record bootstrapSuccessorPreparationRecord
	reader, record, err = openBootstrapSuccessorPreparationReader(ctx, plan, nil)
	if err != nil {
		fmt.Fprintln(stderr, "successor Safe review requires completed local preparation; preserve its original owner:", err)
		return 1
	}
	if request.PreparationRecordHash != record.ContentHash {
		fmt.Fprintln(stderr, "successor Safe review record hash differs from the completed preparation")
		return 3
	}
	rawArchive, archiveHash, err := readPlanFile(ctx, request.Archive.Path, maximumSafeReleaseArchiveBytes)
	if err != nil || archiveHash != request.Archive.Sha256 {
		fmt.Fprintln(stderr, "successor Safe review published archive pin differs:", err)
		return 1
	}
	result, err := buildBootstrapSuccessorSafeReview(ctx, plan, record, request, planFileReference{Path: *safeRequestPath, Sha256: requestHash}, rawArchive)
	if err == nil {
		err = errors.Join(reader.checkpoint("review-ready"), retained.checkpoint(ctx))
	}
	if err != nil {
		fmt.Fprintln(stderr, "successor Safe review remains unresolved:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "successor Safe review output:", err)
		return 1
	}
	return 3
}
