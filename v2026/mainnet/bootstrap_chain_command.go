// This command composes bounded local preparation and read-only readiness.
// The explicit root-registration precursor owns separate signing/send authority.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
)

// Exact accepted preparation and run-directory identities precede any mutation.
// Every resume reloads its independently pinned inputs before retained ownership.
func runBootstrapChainCommand(ctx context.Context, args []string, stdout, stderr io.Writer) (result int) {
	if len(args) > 1 && args[1] == "root-registration" {
		return runRootRegisterCommand(ctx, args[2:], stdout, stderr)
	}
	if len(args) > 1 && args[1] == "provider-role-config" {
		return runBootstrapProviderRoleCommand(ctx, args[2:], stdout, stderr)
	}
	if len(args) > 1 && args[1] == "validator-source-role-config" {
		return runBootstrapValidatorOriginalRoleCommand(ctx, args[2:], stdout, stderr)
	}
	if len(args) > 1 && strings.HasPrefix(args[1], "contract-") {
		return runBootstrapChainContractCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 1 && strings.HasPrefix(args[1], "trim-") {
		return runBootstrapTrimCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) < 2 || args[1] != "plan" && args[1] != "apply" && args[1] != "resume" && args[1] != "readiness" {
		fmt.Fprintln(stderr, "bootstrap-chain requires plan, apply, resume, read-only readiness, contract-plan, offline contract-readiness, unsigned contract-successor-plan or the separately approved root-registration precursor")
		return 2
	}
	command := args[1]
	flags := flag.NewFlagSet("bootstrap-chain "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "canonical private JSON chain preparation config")
	runDirectory := flags.String("run-dir", "", "exact precreated private custody directory")
	accepted := flags.String("accept-plan-hash", "", "accepted offline preparation plan hash")
	var rpcUrl string
	var retryWindow time.Duration
	if command == "readiness" {
		flags.StringVar(&rpcUrl, "rpc", "", "explicit owned HTTP(S) observation route")
		flags.DurationVar(&retryWindow, "retry-window", 300*time.Second, "one bounded read-only census window, 60s through 15m")
	}
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 || *configPath == "" ||
		command == "plan" && (*runDirectory != "" || *accepted != "") || command != "plan" && (*runDirectory == "" || !planSha256(*accepted)) {
		fmt.Fprintln(stderr, "bootstrap-chain plan needs --config; apply/resume/readiness also need --run-dir and --accept-plan-hash")
		return 2
	}
	if command == "readiness" && (rpcUrl == "" || retryWindow < 60*time.Second || retryWindow > 15*time.Minute) {
		fmt.Fprintln(stderr, "bootstrap-chain readiness requires --rpc and a 60s..15m retry-window")
		return 2
	}
	preparation, err := loadBootstrapChainPreparation(ctx, *configPath)
	if err != nil {
		fmt.Fprintln(stderr, "bootstrap chain preparation inputs:", err)
		return 2
	}
	encoder := json.NewEncoder(stdout)
	if command == "plan" {
		if err := encoder.Encode(preparation.Plan); err != nil {
			fmt.Fprintln(stderr, "bootstrap chain plan output:", err)
			return 1
		}
		return 0
	}
	if *accepted != preparation.Plan.ContentHash || *runDirectory != preparation.Plan.Config.RunDirectory {
		fmt.Fprintln(stderr, "bootstrap chain accepted plan or run directory differs; no journal opened")
		return 3
	}
	if command == "readiness" {
		return runBootstrapChainReadiness(ctx, preparation, rpcUrl, retryWindow, stdout, stderr)
	}
	if command == "apply" && !bootstrapChainHasRootRole(preparation.Plan.Config.Schema) {
		fmt.Fprintln(stderr, "bootstrap chain new preparation requires v3 UR and root config inspections; existing v1/v2 custody remains resumable at its original scope")
		return 3
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, "bootstrap chain canceled:", err)
		return 1
	}
	store, err := openBootstrapChainStore(preparation, command == "apply", nil, ctx)
	if err != nil {
		fmt.Fprintln(stderr, "bootstrap chain retained ownership:", err)
		return 3
	}
	defer func() {
		if err := store.close(); err != nil {
			fmt.Fprintln(stderr, "bootstrap chain close:", err)
			result = 1
		}
	}()
	prepared, err := advanceBootstrapChain(ctx, store, nil)
	if err != nil {
		fmt.Fprintln(stderr, "bootstrap chain local preparation stopped; retain all journals for resume:", err)
		return 1
	}
	if err := encoder.Encode(prepared); err != nil {
		fmt.Fprintln(stderr, "bootstrap chain output failed; resume the retained preparation:", err)
		return 1
	}
	return 0
}
