// The native observation CLI accepts only an explicit owned read route and
// reviewed file. It emits bounded partial evidence even when collection fails.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"
)

// Exit zero means the requested range was read. Economic outcome and launch
// authorization remain unresolved in the artifact regardless of exit status.
func runEconomicEmissionCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("observe-native-miner-emission", flag.ContinueOnError)
	flags.SetOutput(stderr)
	policyPath := flags.String("policy", "", "independently reviewed runtime, subnet generation and exact block range JSON")
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) archive RPC URL")
	retryWindow := flags.Duration("retry-window", 60*time.Second, "one total observation deadline, 60s through 15m")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *policyPath == "" || *rpcUrl == "" || *retryWindow < 60*time.Second || *retryWindow > 15*time.Minute {
		fmt.Fprintln(stderr, "observe-native-miner-emission requires --rpc URL --policy FILE and a 60s–15m total deadline")
		return 2
	}
	var policy economicEmissionPolicy
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
	observation, readErr := observeEconomicEmission(ctx, client, policy, policyHash)
	if err := json.NewEncoder(stdout).Encode(observation); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if readErr != nil {
		fmt.Fprintln(stderr, readErr)
		return 3
	}
	return 0
}
