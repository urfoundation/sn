// The public offline command invokes exact replay bytes with no RPC, signer or
// financial mutation. Successful execution does not imply admitted finality.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"
)

func runHistoricalReplayCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify-historical-execution", flag.ContinueOnError)
	flags.SetOutput(stderr)
	engine := flags.String("engine", "", "absolute independently reviewed replay executable")
	engineHash := flags.String("engine-sha256", "", "exact replay executable sha256:DIGEST")
	job := flags.String("job", "", "absolute private complete historical proof job")
	jobHash := flags.String("job-sha256", "", "exact job sha256:DIGEST")
	budget := flags.Duration("budget", 300*time.Second, "one total process/read budget,60s–15m")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || !bootstrapRootAbsolutePath(*engine) || !planSha256(*engineHash) || !bootstrapRootAbsolutePath(*job) || !planSha256(*jobHash) || *budget < time.Minute || *budget > 15*time.Minute {
		fmt.Fprintln(stderr, "verify-historical-execution requires exact --engine/--engine-sha256 and --job/--job-sha256 pins with a60s–15m budget")
		return 2
	}
	report, err := runHistoricalReplay(ctx, historicalReplayRequest{Engine: planFileReference{Path: *engine, Sha256: *engineHash}, Job: planFileReference{Path: *job, Sha256: *jobHash}, Budget: *budget}, historicalReplayHooks{})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
