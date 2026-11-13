package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"
)

// Local capture reads retained raw nodes. Archive capture fills an exclusive
// declared evidence cache through the same exact-parent proof broker; neither
// mode opens a node's mutable database or enrolls a production authority.
func runHistoricalCaptureCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("capture-historical-execution", flag.ContinueOnError)
	flags.SetOutput(stderr)
	engine := flags.String("engine", "", "absolute independently reviewed capture executable")
	engineHash := flags.String("engine-sha256", "", "exact capture executable sha256:DIGEST")
	input := flags.String("request", "", "absolute exact parent/child/body/runtime capture request")
	inputHash := flags.String("request-sha256", "", "exact request sha256:DIGEST")
	nodes := flags.String("nodes", "", "absolute read-only raw parent trie node directory")
	rpc := flags.String("rpc", "", "optional read-only archive route for bounded missing-node proofs")
	cache := flags.String("cache-dir", "", "absolute existing private declared archive evidence directory")
	ownerLocal := flags.Bool("owner-local-cache", false, "archive mode: explicitly select separate owner-local durable evidence policy")
	chain := flags.String("expected-chain", "", "archive mode: independently selected native chain")
	genesis := flags.String("expected-genesis", "", "archive mode: independently selected genesis")
	evm := flags.Uint64("expected-evm-chain-id", 0, "archive mode: independently selected EVM chain ID")
	maximumBytes := flags.Uint64("cache-max-bytes", 2*nativeProducerBoundaryReserve, "archive cache hard byte bound")
	maximumEntries := flags.Uint64("cache-max-entries", 2*nativeProducerBoundaryEntries, "archive cache hard entry bound")
	budget := flags.Duration("budget", 300*time.Second, "one total process/read budget,60s–15m")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || !bootstrapRootAbsolutePath(*engine) || !planSha256(*engineHash) || !bootstrapRootAbsolutePath(*input) || !planSha256(*inputHash) || *budget < time.Minute || *budget > 15*time.Minute {
		fmt.Fprintln(stderr, "capture-historical-execution requires exact --engine/--engine-sha256, --request/--request-sha256 and a 60s–15m budget")
		return 2
	}
	archive := *rpc != ""
	cacheLimitsSupplied := false
	flags.Visit(func(value *flag.Flag) {
		cacheLimitsSupplied = cacheLimitsSupplied || value.Name == "cache-max-bytes" || value.Name == "cache-max-entries" || value.Name == "owner-local-cache"
	})
	if archive && (*nodes != "" || !bootstrapRootAbsolutePath(*cache) || *chain == "" || !rootCanonicalHash(*genesis) || *evm == 0 || *maximumBytes < 2*nativeProducerBoundaryReserve || *maximumBytes > 64*1024*1024*1024 || *maximumEntries < 2*nativeProducerBoundaryEntries || *maximumEntries > 1024*1024) || !archive && (!bootstrapRootAbsolutePath(*nodes) || *cache != "" || *chain != "" || *genesis != "" || *evm != 0 || cacheLimitsSupplied) {
		fmt.Fprintln(stderr, "choose --nodes, or --rpc with --cache-dir and all expected network fields; archive cache requires finite limits and declared durable custody")
		return 2
	}
	request := historicalCaptureRequest{Engine: planFileReference{Path: *engine, Sha256: *engineHash}, Input: planFileReference{Path: *input, Sha256: *inputHash}, Nodes: *nodes, Budget: *budget}
	var report *historicalCaptureReport
	var err error
	if archive {
		report, err = runHistoricalArchiveCapture(ctx, historicalArchiveCaptureRequest{Capture: request, Rpc: *rpc, Expected: identityExpectation{NativeChain: *chain, GenesisHash: *genesis, EvmChainId: *evm}, CacheDirectory: *cache, OwnerLocalCache: *ownerLocal, MaximumBytes: *maximumBytes, MaximumEntries: *maximumEntries}, historicalReplayHooks{})
	} else {
		report, err = runHistoricalCapture(ctx, request, historicalReplayHooks{})
	}
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
