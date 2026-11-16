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
	"path/filepath"
	"strconv"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
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

// The observer borrows a committed physical head for its entire run. Shared
// admission never repairs a pending writer or consumes write reserve.
type rootPassivePreparation struct {
	reader *bootstrapContractReadinessMarker
}

// The sole synchronous command owner closes after its observation loop joins.
func (self *rootPassivePreparation) Close() error {
	if self == nil {
		return nil
	}
	return self.reader.close()
}

// Both the original marker and complete journal remain authenticated on each
// sample boundary, including after a delayed RPC read or output operation.
func (self *rootPassivePreparation) check() error {
	if self == nil || self.reader == nil {
		return errors.New("passive preparation owner is absent")
	}
	return self.reader.checkpoint()
}

// Shared preparation ownership validates the complete snapshot without
// completing interrupted claims. The monitor separately owns its checkpoint.
func openRootPassivePreparation(ctx context.Context, plan bootstrapRootPlan) (_ *rootPassivePreparation, resultErr error) {
	if err := plan.validate(); err != nil || plan.PassiveService == nil {
		return nil, errors.Join(errors.New("passive service preparation is invalid"), err)
	}
	path := filepath.Join(plan.RunDirectory, bootstrapRootProgressFile)
	marker := plan.ContentHash + "\n" + bootstrapRootClaimComplete
	reader, err := openBootstrapReadinessSnapshot(path, marker, "mainnet-bootstrap-root", 16*1024, ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, reader.close())
		}
	}()
	raw, _, err := reader.storage.readFile(ctx, path, 16*1024)
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
	return &rootPassivePreparation{reader: reader}, errors.Join(ctx.Err(), reader.checkpoint())
}

// Running requires exact accepted runtime bytes and prior local preparation.
// Finite samples/cadence come from the signed service, not command overrides.
func runRootPassiveServiceCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runRootPassiveServiceCommandWithMonitorHooks(ctx, args, stdout, stderr, time.Now, monitorServiceHooks{})
}

// Hooks observe the same joined sampling/wait boundaries as the direct root
// command; they do not supply preparation bytes or a custody decision.
func runRootPassiveServiceCommandWithMonitorHooks(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time, hooks monitorServiceHooks) (code int) {
	if ctx == nil || len(args) == 0 || args[0] != "plan" && args[0] != "run" {
		fmt.Fprintln(stderr, "root-passive-service requires plan|run")
		return 2
	}
	if args[0] == "run" {
		if err := durablepath.Require(ctx); err != nil {
			fmt.Fprintln(stderr, "passive root durable custody:", err)
			return 2
		}
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
	return runRootCommandWithPolicy(ctx, monitorArgs, stdout, stderr, now, hooks, &service.Policy, rootObjectHash(service.Policy), lock)
}
