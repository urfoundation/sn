// The read-only producer independently verifies fee semantics approval, signed
// receipt/native context and actual owned replay before publishing evidence.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"
)

func runEconomicNativeFeeEvidenceCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify-admitted-native-fees", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("request", "", "absolute private input-pin request")
	digest := flags.String("request-sha256", "", "exact request sha256:DIGEST")
	budget := flags.Duration("budget", 300*time.Second, "one complete verification budget, 60s–15m")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || !bootstrapRootAbsolutePath(*path) || !planSha256(*digest) || *budget < time.Minute || *budget > 15*time.Minute {
		fmt.Fprintln(stderr, "verify-admitted-native-fees requires --request/--request-sha256 and a 60s–15m total budget")
		return 2
	}
	owner, cancel := context.WithTimeout(ctx, *budget)
	defer cancel()
	raw, observed, err := readBootstrapRootFile(owner, *path, 64*1024)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if observed != *digest {
		fmt.Fprintln(stderr, "admitted native fee request differs from its exact pin")
		return 1
	}
	var request economicNativeFeeRequest
	if err := decodePlanJson(raw, &request); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	result, err := runEconomicNativeFeeEvidence(owner, request, *budget, historicalReplayHooks{})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
