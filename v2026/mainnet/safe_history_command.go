// The archive command emits reusable read-only census evidence. Exit zero
// means the bounded census passed; it never means Safe provenance was proven.
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

// Every identity and interval endpoint is supplied explicitly. This command
// loads no authority files, signing material, nonce registry, or submission port.
func runSafeHistoryCaptureCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("safe-history-capture", flag.ContinueOnError)
	flags.SetOutput(stderr)
	rpcUrl := flags.String("rpc", "", "explicit owned archive HTTP(S) RPC URL")
	output := flags.String("output", "", "new absolute witness file inside an existing private directory")
	chain := flags.String("expected-chain", "", "independently supplied native chain name")
	genesis := flags.String("expected-genesis", "", "independently supplied native genesis hash")
	chainId := flags.Uint64("expected-evm-chain-id", 0, "independently supplied mainnet EVM chain ID")
	safe := flags.String("safe", "", "exact lowercase Safe address")
	from := flags.Uint64("from-number", 0, "first native block number, inclusive")
	fromHash := flags.String("from-hash", "", "exact first native block hash")
	through := flags.Uint64("through-number", 0, "last native block number, inclusive")
	throughHash := flags.String("through-hash", "", "exact last native block hash")
	retry := flags.Duration("retry-window", 300*time.Second, "transient retry window for each read, under caller cancellation")
	nativeTrace := flags.Bool("native-storage-trace", false, "retain bounded keyed native replay traces and parent runtime proofs; does not prove complete history")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || ctx == nil || *rpcUrl == "" || !bootstrapRootAbsolutePath(*output) || *chain == "" ||
		!rootCanonicalHash(*genesis) || *chainId != mainnetEvmChainId || *retry < time.Minute || *retry > 15*time.Minute {
		fmt.Fprintln(stderr, "safe-history-capture requires an owned archive route, explicit mainnet identity, exact interval, and a 60s to 15m per-read retry window")
		return 2
	}
	scope := safeHistoryScope{Safe: *safe, From: safeHistoryBoundary{Number: *from, Hash: *fromHash}, Through: safeHistoryBoundary{Number: *through, Hash: *throughHash}}
	if err := scope.validate(); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	client, err := newRpcClient(*rpcUrl, *retry)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer client.httpClient.CloseIdleConnections()
	expected := identityExpectation{NativeChain: *chain, GenesisHash: *genesis, EvmChainId: *chainId}
	var capture safeHistoryCapture
	if *nativeTrace {
		capture, err = client.captureSafeHistoryNative(ctx, expected, scope)
	} else {
		capture, err = client.captureSafeHistory(ctx, expected, scope)
	}
	if err != nil {
		fmt.Fprintln(stderr, "Safe archive census:", err)
		if errors.Is(err, errRpcIdentityMismatch) {
			return 3
		}
		if errors.Is(err, errSafeHistoryArchiveUnavailable) || errors.Is(err, errFinalizedMappingUnavailable) {
			return 4
		}
		return 1
	}
	envelope, err := sealSafeHistoryCapture(capture)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := writeSafeHistoryCapture(ctx, *output, envelope); err != nil {
		fmt.Fprintln(stderr, "retain Safe archive census:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(envelope); err != nil {
		fmt.Fprintln(stderr, "write Safe archive census:", err)
		return 1
	}
	return 0
}
