// Mainnet observation starts read-only; bootstrap mutation is not admitted here.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const monitorSchema = "urnetwork-mainnet-monitor-event-v1"
const mainnetEvmChainId = 964

// monitorEvent is one JSON line suitable for the existing log/alert pipeline.
type monitorEvent struct {
	Schema     string            `json:"schema"`
	ObservedAt string            `json:"observed_at"`
	Status     string            `json:"status"`
	Detail     string            `json:"detail,omitempty"`
	Snapshot   *identityEnvelope `json:"snapshot,omitempty"`
}

// monitorState tracks finalized progress without treating a changing tip as finality.
type monitorState struct {
	lastHash       string
	lastNumber     uint64
	lastProgressAt time.Time
}

// observe recognizes frozen or contradictory finalized heads at one route.
func (self *monitorState) observe(now time.Time, identity chainIdentity, stallAfter time.Duration) (string, error) {
	if self.lastHash == "" {
		self.lastHash, self.lastNumber, self.lastProgressAt = identity.FinalizedHash, identity.FinalizedNumber, now
		return "ok", nil
	}
	if identity.FinalizedNumber < self.lastNumber || identity.FinalizedNumber == self.lastNumber && !strings.EqualFold(identity.FinalizedHash, self.lastHash) || identity.FinalizedNumber > self.lastNumber && strings.EqualFold(identity.FinalizedHash, self.lastHash) {
		return "finality-conflict", fmt.Errorf("finalized head changed incompatibly: %d/%s to %d/%s", self.lastNumber, self.lastHash, identity.FinalizedNumber, identity.FinalizedHash)
	}
	if identity.FinalizedNumber > self.lastNumber {
		self.lastHash, self.lastNumber, self.lastProgressAt = identity.FinalizedHash, identity.FinalizedNumber, now
		return "ok", nil
	}
	if now.Sub(self.lastProgressAt) >= stallAfter {
		return "finality-stalled", nil
	}
	return "ok", nil
}

// main wires cancellation once; no command in this executable loads a signer.
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(runMain(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// Dispatches signer-free observations and reference accounting with explicit exits.
func runMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 && (args[0] == "check-recycle-mode" || args[0] == "economic-reference") {
		return runEconomicCommand(ctx, args, stdout, stderr)
	}
	if len(args) == 0 || args[0] != "inspect" && args[0] != "monitor" {
		fmt.Fprintln(stderr, "usage: sn-mainnet inspect|monitor --rpc URL [identity flags]; check-recycle-mode --rpc URL --policy FILE; economic-reference --input FILE")
		return 2
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) RPC URL")
	expectedChain := flags.String("expected-chain", "", "approved native chain name")
	expectedGenesis := flags.String("expected-genesis", "", "approved native genesis hash")
	expectedEvmChainId := flags.Uint64("expected-evm-chain-id", 0, "approved EVM chain ID")
	retryWindow := flags.Duration("retry-window", 60*time.Second, "total transient retry window per read")
	interval := flags.Duration("interval", 30*time.Second, "monitor sampling interval")
	stallAfter := flags.Duration("stall-after", 5*time.Minute, "finality progress alert threshold")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *rpcUrl == "" {
		fmt.Fprintln(stderr, "command requires --rpc and no positional arguments")
		return 2
	}
	if command == "monitor" && (*interval <= 0 || *stallAfter <= 0) {
		fmt.Fprintln(stderr, "monitor interval and stall threshold must be positive")
		return 2
	}
	client, err := newRpcClient(*rpcUrl, *retryWindow)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	expected := identityExpectation{NativeChain: *expectedChain, GenesisHash: *expectedGenesis, EvmChainId: *expectedEvmChainId}
	checkExpected := command == "monitor" || expected.NativeChain != "" || expected.GenesisHash != "" || expected.EvmChainId != 0
	if checkExpected && (expected.NativeChain == "" || !validHash(expected.GenesisHash) || expected.EvmChainId == 0) {
		fmt.Fprintln(stderr, "approved chain, genesis and EVM chain ID must all be supplied")
		return 2
	}
	if command == "monitor" && expected.EvmChainId != mainnetEvmChainId {
		fmt.Fprintf(stderr, "mainnet monitor requires EVM chain ID %d\n", mainnetEvmChainId)
		return 2
	}
	encoder := json.NewEncoder(stdout)
	if command == "inspect" {
		identity, err := client.readIdentity(ctx)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		snapshot, err := sealIdentity(identity)
		if err != nil {
			fmt.Fprintln(stderr, "write identity snapshot:", err)
			return 1
		}
		if err := encoder.Encode(snapshot); err != nil {
			fmt.Fprintln(stderr, "write identity snapshot:", err)
			return 1
		}
		if checkExpected {
			if err := expected.match(identity); err != nil {
				fmt.Fprintln(stderr, err)
				return 3
			}
		}
		return 0
	}
	state := &monitorState{}
	for {
		if ctx.Err() != nil {
			return 0
		}
		event := monitorEvent{Schema: monitorSchema}
		identity, readErr := client.readIdentity(ctx)
		now := time.Now().UTC()
		event.ObservedAt = now.Format(time.RFC3339Nano)
		if readErr != nil {
			if ctx.Err() != nil {
				return 0
			}
			event.Status, event.Detail = "rpc-error", readErr.Error()
			if errors.Is(readErr, errRpcIntegrity) {
				event.Status = "rpc-integrity"
			}
		} else {
			snapshot, sealErr := sealIdentity(identity)
			if sealErr != nil {
				fmt.Fprintln(stderr, sealErr)
				return 1
			}
			event.Snapshot = &snapshot
			if identityErr := expected.match(identity); identityErr != nil {
				event.Status, event.Detail = "identity-mismatch", identityErr.Error()
				if encoder.Encode(event) != nil {
					return 1
				}
				return 3
			}
			continuous, continuityErr := client.priorFinalizedMatches(ctx, state, identity)
			if continuityErr != nil {
				event.Status, event.Detail = "rpc-error", continuityErr.Error()
				if errors.Is(continuityErr, errRpcIntegrity) {
					event.Status = "rpc-integrity"
				}
			} else if !continuous {
				event.Status, event.Detail = "finality-conflict", "previously finalized block hash changed at its original height"
			} else {
				event.Status, err = state.observe(now, identity, *stallAfter)
				if err != nil {
					event.Detail = err.Error()
				}
			}
		}
		if err := encoder.Encode(event); err != nil {
			fmt.Fprintln(stderr, "write monitor event:", err)
			return 1
		}
		if event.Status == "finality-conflict" || event.Status == "rpc-integrity" {
			return 3
		}
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(*interval):
		}
	}
}
