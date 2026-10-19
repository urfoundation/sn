// Explicit independently signed active repair is a separate command/domain.
// Claim has no service effect; resume never obtains authority from monitoring.
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

// Production fixes the host implementation; test transport has no public flag.
func runRepairActiveValidatorCommand(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time) int {
	return runRepairActiveValidatorCommandWithHost(ctx, args, stdout, stderr, now, newRepairValidatorHost())
}

// Original evidence, finite limits and independent key are required even for
// status. No private approver key, signer loader or approval generator exists.
func runRepairActiveValidatorCommandWithHost(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time, host *repairValidatorHost) int {
	if ctx == nil || host == nil || now == nil || runtime.GOOS != "linux" || uint32(os.Geteuid()) != host.rootUid || len(args) == 0 || args[0] != "claim" && args[0] != "resume" && args[0] != "status" {
		fmt.Fprintln(stderr, "active validator repair requires Linux root custody and claim|resume|status")
		return 2
	}
	flags := flag.NewFlagSet("repair-active-validator "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	approvalPath := flags.String("approval", "", "exact independently signed active repair envelope")
	approvalHash := flags.String("accept-approval-hash", "", "sha256 of exact approval file")
	publicKey := flags.String("independent-public-key", "", "independently approved Ed25519 key, canonical 0x32-byte hex")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *approvalPath == "" || !validMonitorReadDigest(*approvalHash) || !rootCanonicalHash(*publicKey) {
		fmt.Fprintln(stderr, "active validator repair requires --approval FILE --accept-approval-hash sha256:DIGEST --independent-public-key 0xKEY")
		return 2
	}
	raw, digest, err := readPlanFile(ctx, *approvalPath, 64*1024)
	var approval repairActiveValidatorApproval
	if err == nil {
		err = decodePlanJson(raw, &approval)
	}
	if err == nil {
		err = approval.validate(*publicKey)
	}
	if err != nil || digest != *approvalHash {
		fmt.Fprintln(stderr, "active validator repair approval refused:", err)
		return 2
	}
	p := approval.Plan.Process
	if err := host.parents(p.StatePath, host.rootUid); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	// Claim owns the common unit lock until both permanent generation claim and
	// per-envelope journal are durable. Either incomplete artifact blocks effects.
	var control *repairValidatorControl
	if args[0] == "claim" {
		stamp := now()
		if stamp.Before(p.ValidFrom) || !stamp.Before(p.ExpiresAt) {
			fmt.Fprintln(stderr, "active repair claim window closed")
			return 3
		}
		control, err = host.control(ctx, p.Unit)
		if err == nil {
			var manager repairValidatorManager
			manager, err = host.inspectActive(ctx, approval.Plan)
			if err == nil && !repairActiveValidatorRunning(p, manager) {
				err = errors.New("active repair previous generation is not running")
			}
		}
		if err == nil {
			err = host.activeIncident(ctx, approval.Plan, now(), true)
		}
		if current := now(); err == nil && (current.Before(stamp) || current.Before(p.ValidFrom) || !current.Before(p.ExpiresAt) || ctx.Err() != nil) {
			err = errors.Join(errors.New("active repair claim window changed during admission"), ctx.Err())
		}
		if err == nil {
			err = host.activeClaim(ctx, approval, *publicKey, true)
		}
		if err != nil {
			fmt.Fprintln(stderr, errors.Join(err, control.close()))
			return 3
		}
	}
	store, err := openRepairActiveValidatorStore(ctx, approval, *publicKey, args[0] == "claim", now())
	if err != nil {
		fmt.Fprintln(stderr, errors.Join(err, control.close()))
		return 3
	}
	var result repairActiveValidatorResult
	if args[0] == "resume" {
		result, err = resumeRepairActiveValidator(ctx, store, host, now)
	} else {
		var record repairActiveValidatorRecord
		record, err = store.load(ctx)
		if err == nil {
			err = host.activeClaim(ctx, approval, *publicKey, false)
		}
		result = record.result()
	}
	err = errors.Join(err, store.close(), control.close())
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
