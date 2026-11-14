// Public fresh starts require a distinct independently accepted current policy.
// Installation, historical reports and the original process approval are separate.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"
)

func runValidatorActivationCommand(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time) int {
	return runValidatorActivationCommandWithHost(ctx, args, stdout, stderr, now, newValidatorActivationHost(), nil)
}

// Tests may supply a synthetic owner. Public callers can select only the fixed
// production adapter, with exact independently signed current-policy acceptance.
func runValidatorActivationCommandWithHost(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time, host *validatorActivationHost, authority validatorActivationAuthority) (resultCode int) {
	if ctx == nil || ctx.Err() != nil || now == nil || host == nil || host.host == nil || runtime.GOOS != "linux" || uint32(os.Geteuid()) != host.host.rootUid || len(args) == 0 {
		fmt.Fprintln(stderr, "validator activation requires joined Linux host custody and an explicit operation")
		return 2
	}
	operation := args[0]
	if operation != "claim" && operation != "install" && operation != "admit" && operation != "admit-evidence" && operation != "admit-stake" && operation != "admit-health" && operation != "admit-committed" && operation != "admit-current" && operation != "start" && operation != "resume" && operation != "status" {
		fmt.Fprintln(stderr, "validator activation requires claim|install|admit|admit-evidence|admit-stake|admit-health|admit-committed|admit-current|start|resume|status")
		return 2
	}
	flags := flag.NewFlagSet("activate-validators "+operation, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("approval", "", "exact independently signed two-validator activation envelope")
	digest := flags.String("accept-approval-hash", "", "sha256 of the complete approval file")
	key := flags.String("independent-public-key", "", "independently supplied Ed25519 approval key")
	execute := flags.Bool("execute-approved-starts", false, "acknowledge actual producer starts; still requires a qualified current authority")
	currentPath := flags.String("current-approval", "", "separate independently signed current admission and custody acceptance")
	currentHash := flags.String("accept-current-approval-hash", "", "sha256 of the exact current admission envelope; explicit policy opt-in")
	currentKey := flags.String("current-independent-public-key", "", "independently supplied distinct current-admission Ed25519 key")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || !repairValidatorPath(*path) || !planSha256(*digest) || !rootCanonicalHash(*key) || *execute != (operation == "start") {
		fmt.Fprintln(stderr, "validator activation needs exact --approval, --accept-approval-hash, --independent-public-key; start also requires --execute-approved-starts")
		return 2
	}
	hasCurrent := *currentPath != "" || *currentHash != "" || *currentKey != ""
	if hasCurrent && (authority != nil || operation != "start" && operation != "admit-current" || !repairValidatorPath(*currentPath) || !planSha256(*currentHash) || !rootCanonicalHash(*currentKey)) || operation == "admit-current" && !hasCurrent && authority == nil {
		fmt.Fprintln(stderr, "current admission requires start|admit-current and all of --current-approval, --accept-current-approval-hash, --current-independent-public-key; no imported readiness grants authority")
		return 2
	}
	raw, actual, err := readPlanFile(ctx, *path, 64*1024)
	var approval validatorActivationApproval
	if err == nil {
		err = decodePlanJson(raw, &approval)
	}
	if err == nil {
		err = approval.validate(*key)
	}
	if err != nil || actual != *digest {
		fmt.Fprintln(stderr, "validator activation approval refused:", err)
		return 2
	}
	if _, err := loadValidatorActivationPreparation(ctx, approval); err != nil {
		fmt.Fprintln(stderr, "validator activation original bootstrap refused:", err)
		return 3
	}
	if err := host.host.parents(approval.Plan.StatePath, host.host.rootUid); err != nil {
		fmt.Fprintln(stderr, err)
		return 3
	}
	var custody *bootstrapChainReadinessState
	defer func() {
		if err := custody.close(); err != nil {
			fmt.Fprintln(stderr, err)
			resultCode = 3
		}
	}()
	if operation == "claim" {
		if err := validatorActivationWindow(ctx, approval.Plan, approval.Plan.ValidFrom, now(), nil); err != nil {
			fmt.Fprintln(stderr, err)
			return 3
		}
		retained, err := loadValidatorActivationPreparation(ctx, approval)
		if err == nil {
			custody, err = openBootstrapChainReadinessState(ctx, retained)
		}
		if err != nil {
			fmt.Fprintln(stderr, "validator activation requires retained original custody:", err)
			return 3
		}
	}
	store, err := openValidatorActivationStore(ctx, approval, *key, operation == "claim", now())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 3
	}
	var current *validatorActivationCurrentAuthority
	if hasCurrent {
		current, err = newValidatorActivationCurrentAuthority(ctx, approval, *key, planFileReference{Path: *currentPath, Sha256: *currentHash}, *currentKey)
		if err == nil {
			err = current.retained.Approval.window(ctx, approval.Plan.ValidFrom, now())
		}
		if err != nil {
			if current != nil {
				err = errors.Join(err, current.close())
			}
			fmt.Fprintln(stderr, errors.Join(err, store.close()))
			return 3
		}
		authority = current
	}
	var result validatorActivationResult
	if operation == "claim" || operation == "status" {
		var record validatorActivationRecord
		record, err = store.load(ctx)
		result = record.result()
	} else {
		result, err = advanceValidatorActivation(ctx, store, host, authority, operation, now)
	}
	if current != nil {
		err = errors.Join(err, current.close())
	}
	err = errors.Join(err, store.close())
	if custody != nil {
		if custodyErr := custody.checkpoint(ctx); custodyErr != nil {
			fmt.Fprintln(stderr, errors.Join(err, custodyErr))
			return 3
		}
	}
	if result.Schema != "" {
		err = errors.Join(err, json.NewEncoder(stdout).Encode(result))
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 3
	}
	return 0
}
