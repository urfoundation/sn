// Process authority is accepted independently of the passive runtime approval.
// Status is historical; only resume performs a current manager readback.
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

func runRootPassiveHostCommand(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time) int {
	return runRootPassiveHostCommandWithHost(ctx, args, stdout, stderr, now, newRootPassiveHost())
}

func runRootPassiveHostCommandWithHost(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time, host *rootPassiveHost) (code int) {
	if ctx == nil || ctx.Err() != nil || now == nil || host == nil || host.files == nil || host.files.host == nil || runtime.GOOS != "linux" || uint32(os.Geteuid()) != host.files.host.rootUid || len(args) == 0 {
		fmt.Fprintln(stderr, "passive host requires joined Linux host custody and an explicit operation")
		return 2
	}
	operation := args[0]
	if operation != "claim" && operation != "install" && operation != "admit" && operation != "start" && operation != "resume" && operation != "status" {
		fmt.Fprintln(stderr, "activate-root-passive requires claim|install|admit|start|resume|status")
		return 2
	}
	flags := flag.NewFlagSet("activate-root-passive "+operation, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("approval", "", "exact independently signed passive host envelope")
	accepted := flags.String("accept-approval-hash", "", "sha256 of complete independent host envelope")
	key := flags.String("independent-public-key", "", "independently supplied Ed25519 host approval key")
	execute := flags.Bool("execute-approved-start", false, "acknowledge the one actual approved passive process start")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || !repairValidatorPath(*path) || !planSha256(*accepted) || !rootCanonicalHash(*key) || *execute != (operation == "start") {
		fmt.Fprintln(stderr, "passive host needs exact approval, hash and independent public key; start requires --execute-approved-start")
		return 2
	}
	raw, digest, err := readPlanFile(ctx, *path, 64*1024)
	var approval rootPassiveHostApproval
	if err == nil {
		err = decodePlanJson(raw, &approval)
	}
	if err == nil {
		err = approval.validate(*key)
	}
	if err != nil || digest != *accepted {
		fmt.Fprintln(stderr, "passive host approval refused:", err)
		return 2
	}
	if rootPassiveHostPathContains(approval.Plan.CheckpointDirectory, *path) {
		fmt.Fprintln(stderr, "passive host approval is in writable process state")
		return 2
	}
	preparation, err := loadRootPassiveHostPreparation(ctx, approval)
	if err == nil {
		err = host.authority(ctx, approval.Plan, preparation)
	}
	if err != nil {
		fmt.Fprintln(stderr, "passive host original authority refused:", err)
		return 3
	}
	custody, err := openBootstrapChainReadinessState(ctx, preparation)
	if err != nil {
		fmt.Fprintln(stderr, "passive host requires complete original preparation:", err)
		return 3
	}
	defer func() {
		if err := custody.close(); err != nil {
			fmt.Fprintln(stderr, err)
			code = 3
		}
	}()
	if operation == "claim" {
		if err := rootPassiveHostWindow(ctx, approval.Plan, approval.Plan.ValidFrom, now(), nil); err != nil {
			fmt.Fprintln(stderr, err)
			return 3
		}
	}
	store, err := openRootPassiveHostStore(ctx, approval, *key, operation == "claim", now(), custody, rootObjectHash(preparation.Root.PassiveService.Policy))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 3
	}
	var result rootPassiveHostResult
	if operation == "claim" || operation == "status" {
		var record rootPassiveHostRecord
		record, err = store.load(ctx)
		result = record.result()
	} else {
		result, err = advanceRootPassiveHost(ctx, store, host, preparation, operation, now)
	}
	err = errors.Join(err, store.validateOwner(), store.close())
	result.refuseCustody(err)
	if err != nil {
		result.CurrentProcessRunning = false
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
