// Explicit signed local repair capability. Claim is custody-only; resume can
// start exactly one approved stopped standard-validator generation.
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

// The production path fixes the system manager and requires its root custody
// owner. Test transport injection is private and never selected by a flag.
func runRepairValidatorCommand(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time) int {
	return runRepairValidatorCommandWithHost(ctx, args, stdout, stderr, now, newRepairValidatorHost())
}

// An independent key and exact signed approval bytes must both be supplied.
func runRepairValidatorCommandWithHost(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time, host *repairValidatorHost) int {
	if ctx == nil || host == nil || now == nil || runtime.GOOS != "linux" || uint32(os.Geteuid()) != host.rootUid || len(args) == 0 || args[0] != "claim" && args[0] != "resume" && args[0] != "status" {
		fmt.Fprintln(stderr, "validator repair requires Linux root custody and claim|resume|status")
		return 2
	}
	flags := flag.NewFlagSet("repair-validator "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	approvalPath := flags.String("approval", "", "exact independently signed repair envelope")
	approvalHash := flags.String("accept-approval-hash", "", "sha256 of the exact approval file")
	publicKey := flags.String("independent-public-key", "", "independently approved Ed25519 key, canonical 0x32-byte hex")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *approvalPath == "" || !validMonitorReadDigest(*approvalHash) || !rootCanonicalHash(*publicKey) {
		fmt.Fprintln(stderr, "validator repair requires --approval FILE --accept-approval-hash sha256:DIGEST --independent-public-key 0xKEY")
		return 2
	}
	raw, digest, err := readPlanFile(ctx, *approvalPath, 64*1024)
	var approval repairValidatorApproval
	if err == nil {
		err = decodePlanJson(raw, &approval)
	}
	if err == nil {
		err = approval.validate(*publicKey)
	}
	if err != nil || digest != *approvalHash {
		fmt.Fprintln(stderr, "validator repair approval refused:", err)
		return 2
	}
	plan := approval.Plan
	if err := host.parents(plan.StatePath, host.rootUid); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if args[0] == "claim" {
		stamp := now()
		if stamp.Before(plan.ValidFrom) || !stamp.Before(plan.ExpiresAt) {
			fmt.Fprintln(stderr, "validator repair claim window is closed")
			return 3
		}
		manager, err := host.inspect(ctx, plan)
		if err == nil {
			err = host.stopped(ctx, plan, manager)
		}
		if err == nil {
			_, err = host.incident(ctx, plan, now())
		}
		if err != nil {
			fmt.Fprintln(stderr, "validator repair claim refused:", err)
			return 3
		}
	}
	store, err := openRepairValidatorStore(ctx, approval, *publicKey, args[0] == "claim", now())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 3
	}
	var result repairValidatorResult
	if args[0] == "resume" {
		result, err = resumeRepairValidator(ctx, store, host, now)
	} else {
		var record repairValidatorRecord
		record, err = store.load(ctx)
		result = record.result()
	}
	err = errors.Join(err, store.close())
	if result.Schema != "" {
		err = errors.Join(err, json.NewEncoder(stdout).Encode(result))
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 3
	}
	if args[0] == "resume" && result.Completed == nil {
		return 3
	}
	return 0
}
