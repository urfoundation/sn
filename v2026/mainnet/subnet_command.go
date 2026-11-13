// The subnet commands export a bounded census and an optional unsigned method
// preview without a signer, apply mode or endpoint-derived authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

// Exit zero means complete read-only evidence; reset_ready remains false.
func runSubnetCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "subnet-preview" && args[0] != "owner-trim-plan" {
		fmt.Fprintln(stderr, "expected subnet-preview or owner-trim-plan")
		return 2
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) RPC URL")
	policyPath := flags.String("policy", "", "independently approved SN25 census and identity-scope policy JSON")
	retryWindow := flags.Duration("retry-window", 300*time.Second, "one complete bounded census window, 60s through 15m")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *rpcUrl == "" || *policyPath == "" || *retryWindow < 60*time.Second || *retryWindow > 15*time.Minute {
		fmt.Fprintln(stderr, command+" requires --rpc URL --policy FILE and a 60s..15m retry-window")
		return 2
	}
	var policy subnetCensusPolicy
	policyHash, err := readEconomicInput(*policyPath, &policy)
	if err == nil {
		err = policy.validate()
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	client, err := newRpcClient(*rpcUrl, *retryWindow)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer client.httpClient.CloseIdleConnections()
	preview, err := client.readSubnetPreview(ctx, policy, policyHash)
	if err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, errRpcIntegrity) {
			return 3
		}
		return 1
	}
	if command == "owner-trim-plan" {
		plan, err := buildOwnerTrimPlan(ctx, policy, preview)
		if err == nil {
			err = json.NewEncoder(stdout).Encode(plan)
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if plan.Best == nil || !plan.Best.RuntimeReady {
			return 3
		}
		return 0
	}
	envelope, err := sealSubnetPreview(preview)
	if err == nil {
		err = json.NewEncoder(stdout).Encode(envelope)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !preview.CensusComplete || len(preview.Blockers) != 0 {
		return 3
	}
	return 0
}
