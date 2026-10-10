package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"
)

func runHistoricalCaptureRequestCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("historical-capture-request", flag.ContinueOnError)
	flags.SetOutput(stderr)
	rpc := flags.String("rpc", "", "owned read-only HTTP(S) native RPC route")
	block := flags.String("block-hash", "", "exact child block to reproduce")
	chain := flags.String("expected-chain", "", "independently selected native chain")
	genesis := flags.String("expected-genesis", "", "independently selected genesis")
	evm := flags.Uint64("expected-evm-chain-id", 0, "independently selected EVM chain ID")
	profile := flags.String("profile", "", "optional original-code observation profile, still unapproved")
	profileHash := flags.String("profile-sha256", "", "exact optional profile sha256:DIGEST")
	budget := flags.Duration("budget", 300*time.Second, "one total read budget,60s–15m")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *rpc == "" || !rootCanonicalHash(*block) || *chain == "" || !rootCanonicalHash(*genesis) || *evm == 0 || *budget < time.Minute || *budget > 15*time.Minute || (*profile == "") != (*profileHash == "") || *profile != "" && (!bootstrapRootAbsolutePath(*profile) || !planSha256(*profileHash)) {
		fmt.Fprintln(stderr, "historical-capture-request requires --rpc, --block-hash, all expected network fields and a60s–15m budget; optional profile requires its exact hash")
		return 2
	}
	owner, cancel := context.WithTimeout(ctx, *budget)
	defer cancel()
	var observationProfile *historicalReplayObservationProfile
	if *profile != "" {
		raw, digest, err := readBootstrapRootFile(owner, *profile, 64*1024)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if digest != *profileHash {
			fmt.Fprintln(stderr, "historical capture profile differs from exact pin")
			return 1
		}
		observationProfile = &historicalReplayObservationProfile{}
		if err := decodePlanJson(raw, observationProfile); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	client, err := newRpcClient(*rpc, *budget)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer client.httpClient.CloseIdleConnections()
	observation, err := client.readHistoricalCaptureRequest(owner, identityExpectation{NativeChain: *chain, GenesisHash: *genesis, EvmChainId: *evm}, *block, observationProfile)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	result := historicalCaptureObservationEnvelope{Observation: observation, ContentHash: rootObjectHash(observation)}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
