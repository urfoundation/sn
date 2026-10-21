// The subset qualifier accepts only a census policy and bounded window proposal.
// Exit zero records conditional predicates; it never enables a signer or apply.
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

// Inputs name an exact finalized anchor; a stale proposal is not refreshed or
// resigned automatically. Invalid files fail before any RPC connection is used.
func runOwnerTrimBoundedCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("owner-trim-qualify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) RPC URL")
	policyPath := flags.String("policy", "", "independently approved census policy JSON")
	windowPath := flags.String("window", "", "exact finalized anchor and mortal window proposal JSON")
	retryWindow := flags.Duration("retry-window", 300*time.Second, "complete qualification deadline, 60s through 15m")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *rpcUrl == "" || *policyPath == "" || *windowPath == "" ||
		*retryWindow < 60*time.Second || *retryWindow > 15*time.Minute {
		fmt.Fprintln(stderr, "owner-trim-qualify requires --rpc URL --policy FILE --window FILE and a 60s..15m retry-window")
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
	windowRaw, windowHash, err := readPlanFile(ctx, *windowPath, maxRpcReplyBytes)
	var window ownerTrimWindow
	if err == nil {
		err = decodePlanJson(windowRaw, &window)
	}
	if err == nil {
		err = window.validate(policyHash)
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
	result, err := client.readOwnerTrimBoundedQualification(ctx, policy, policyHash, window, windowHash)
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
	if !result.ConditionalSafeSet {
		return 3
	}
	return 0
}
