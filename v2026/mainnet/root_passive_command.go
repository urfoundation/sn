// A separately approved passive service runs the existing bounded observer.
// Neither config nor command accepts native signatures, signers or submissions.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const rootPassiveRuntimeSchema = "urnetwork-mainnet-root-passive-runtime-v1"

// The independent role signer binds the complete child plan and service bytes.
type rootPassiveRuntimeConfig struct {
	Schema string                      `json:"schema"`
	Root   planFileReference           `json:"root_config"`
	Role   bootstrapChainRootValidator `json:"root_validator"`
}

// Every invocation rereads exact private input bytes and verifies the full
// service signature. Retained progress never replaces missing authority.
func loadRootPassiveRuntime(ctx context.Context, path string) (plan bootstrapRootPlan, inputHash string, resultErr error) {
	raw, digest, err := readBootstrapRootFile(ctx, path, rootServiceStoreLimit)
	if err != nil {
		return plan, "", err
	}
	var config rootPassiveRuntimeConfig
	if err := decodePlanJson(raw, &config); err != nil {
		return plan, "", err
	}
	if config.Schema != rootPassiveRuntimeSchema || config.Role.Strategy != rootPassiveStrategy {
		return plan, "", errors.New("passive runtime cannot adopt legacy root action configuration")
	}
	rootRaw, err := readBootstrapChainInput(ctx, config.Root, rootServiceStoreLimit)
	if err != nil {
		return plan, "", err
	}
	plan, err = decodeBootstrapRootPlan(ctx, config.Root.Path, rootRaw, config.Root.Sha256)
	if err != nil {
		return plan, "", err
	}
	outer := bootstrapChainConfig{Schema: bootstrapChainConfigSchemaV4, DeploymentId: plan.DeploymentId, Network: plan.Network, RunDirectory: plan.RunDirectory, Root: config.Root, RootValidator: &config.Role}
	if _, err := loadBootstrapChainRootInspection(ctx, outer, plan); err != nil {
		return plan, "", err
	}
	seen := map[string]bool{}
	for _, input := range []string{path, config.Root.Path, plan.ServiceInput.Path, config.Role.Approval.Path, plan.PassiveService.CheckpointPath, filepath.Join(plan.RunDirectory, bootstrapRootProgressFile)} {
		if !bootstrapRootAbsolutePath(input) || seen[input] || seen[input+".lock"] {
			return plan, "", errors.New("passive runtime inputs and retained state overlap")
		}
		seen[input], seen[input+".lock"] = true, true
	}
	return plan, digest, ctx.Err()
}

// Shared preparation ownership validates a complete marker without completing
// interrupted claims. The monitor separately owns its finalized checkpoint.
func openRootPassivePreparation(ctx context.Context, plan bootstrapRootPlan) (_ *os.File, resultErr error) {
	if err := plan.validate(); err != nil || plan.PassiveService == nil {
		return nil, errors.Join(errors.New("passive service preparation is invalid"), err)
	}
	path := filepath.Join(plan.RunDirectory, bootstrapRootProgressFile)
	fd, err := syscall.Open(path+".lock", syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path+".lock")
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, file.Close())
		}
	}()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(errors.New("passive preparation marker is not private regular state"), err)
	}
	if err := syscall.Flock(fd, syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return nil, err
	}
	marker := plan.ContentHash + "\n" + bootstrapRootClaimComplete
	raw, err := io.ReadAll(io.LimitReader(file, int64(len(marker)+1)))
	if err != nil || string(raw) != marker {
		return nil, errors.Join(errors.New("passive preparation marker differs or is incomplete"), err)
	}
	raw, _, err = readBootstrapRootFile(ctx, path, 16*1024)
	if err != nil {
		return nil, err
	}
	var record bootstrapRootRecord
	if err := decodePlanJson(raw, &record); err != nil {
		return nil, err
	}
	if err := record.validate(plan); err != nil || record.Phase != "passive-service-retained" {
		return nil, errors.Join(errors.New("passive service has no completed local preparation"), err)
	}
	return file, ctx.Err()
}

// Running requires exact accepted runtime bytes and prior local preparation.
// Finite samples/cadence come from the signed service, not command overrides.
func runRootPassiveServiceCommand(ctx context.Context, args []string, stdout, stderr io.Writer) (code int) {
	if ctx == nil || len(args) == 0 || args[0] != "plan" && args[0] != "run" {
		fmt.Fprintln(stderr, "root-passive-service requires plan|run")
		return 2
	}
	flags := flag.NewFlagSet("root-passive-service "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "private exact passive runtime config")
	accepted := flags.String("accept-runtime-sha256", "", "sha256 of independently approved complete runtime config")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *path == "" || args[0] == "plan" && *accepted != "" || args[0] == "run" && !planSha256(*accepted) {
		return 2
	}
	plan, digest, err := loadRootPassiveRuntime(ctx, *path)
	if err != nil {
		fmt.Fprintln(stderr, "passive root inputs:", err)
		return 2
	}
	if args[0] == "plan" {
		result := struct {
			Schema         string            `json:"schema"`
			RuntimeSha256  string            `json:"runtime_sha256"`
			RootPlan       bootstrapRootPlan `json:"root_plan"`
			NativeSigning  bool              `json:"native_signing"`
			NetworkEffects bool              `json:"network_effects"`
		}{Schema: rootPassiveRuntimeSchema, RuntimeSha256: digest, RootPlan: plan}
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			return 1
		}
		return 0
	}
	if *accepted != digest {
		fmt.Fprintln(stderr, "passive runtime bytes differ; no checkpoint or route opened")
		return 3
	}
	lock, err := openRootPassivePreparation(ctx, plan)
	if err != nil {
		fmt.Fprintln(stderr, "passive root preparation:", err)
		return 3
	}
	defer func() {
		if lock.Close() != nil {
			code = 3
		}
	}()
	service := plan.PassiveService
	monitorArgs := []string{"root-monitor", "--rpc", service.RpcUrl, "--policy", plan.ServiceInput.Path,
		"--retry-window", strconv.FormatUint(uint64(service.ReadRetrySeconds), 10) + "s", "--checkpoint", service.CheckpointPath,
		"--samples", strconv.FormatUint(uint64(service.MaximumSamples), 10), "--interval", strconv.FormatUint(uint64(service.IntervalSeconds), 10) + "s",
		"--stall-after", strconv.FormatUint(uint64(service.StallAfterSeconds), 10) + "s"}
	return runRootCommandWithPolicy(ctx, monitorArgs, stdout, stderr, time.Now, monitorServiceHooks{}, &service.Policy, rootObjectHash(service.Policy))
}
