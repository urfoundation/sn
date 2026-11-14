// Guard commands accept exact retained plans and independently supplied census
// policies. They publish read-only comparisons and expose no signing interface.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
)

// Exit zero means only matching observations. Execution-time selection safety
// and the independent authority gates remain unproved by this comparison.
func runOwnerTrimGuardCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "owner-trim-recheck" && args[0] != "owner-trim-reconcile" {
		fmt.Fprintln(stderr, "expected owner-trim-recheck or owner-trim-reconcile")
		return 2
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) RPC URL")
	policyPath := flags.String("policy", "", "exact original independently approved census policy JSON")
	planPath := flags.String("plan", "", "retained owner-trim-plan JSON")
	planHash := flags.String("plan-hash", "", "expected retained plan content hash; not an approval")
	retryWindow := flags.Duration("retry-window", 300*time.Second, "one complete recheck window, 60s through 15m")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *rpcUrl == "" || *policyPath == "" || *planPath == "" ||
		!planSha256(*planHash) || *retryWindow < 60*time.Second || *retryWindow > 15*time.Minute {
		fmt.Fprintln(stderr, args[0]+" requires --rpc URL --policy FILE --plan FILE --plan-hash sha256:DIGEST and a 60s..15m retry-window")
		return 2
	}
	policyRaw, policyHash, err := readPlanFile(ctx, *policyPath, maxRpcReplyBytes)
	var policy subnetCensusPolicy
	if err == nil {
		err = decodePlanJson(policyRaw, &policy)
	}
	if err == nil {
		err = policy.validate()
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	planRaw, _, err := readPlanFile(ctx, *planPath, maximumOwnerTrimPlanBytes)
	var plan ownerTrimPlan
	if err == nil {
		err = decodePlanJson(planRaw, &plan)
	}
	if err == nil {
		err = validateOwnerTrimGuardPlan(policy, policyHash, plan)
	}
	if err != nil || plan.ContentHash != *planHash {
		fmt.Fprintln(stderr, "owner trim retained plan or expected content hash differs:", err)
		return 2
	}
	client, err := newRpcClient(*rpcUrl, *retryWindow)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer client.httpClient.CloseIdleConnections()
	result, err := client.readOwnerTrimGuard(ctx, policy, policyHash, plan, strings.TrimPrefix(args[0], "owner-trim-"))
	if err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, errRpcIntegrity) {
			return 3
		}
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !result.ObservationMatches {
		return 3
	}
	return 0
}
