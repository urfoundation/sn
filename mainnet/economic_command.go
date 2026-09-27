// Economic commands read explicit policy/reference files and never load keys.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/urfoundation/sn/protocol"
)

// Bounds local inputs, rejects ambiguous JSON and retains the exact input hash.
func readEconomicInput(path string, value any) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxRpcReplyBytes+1))
	if err != nil || len(raw) > maxRpcReplyBytes {
		return "", errors.Join(errors.New("economic input exceeds 1 MiB or cannot be read"), err)
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return "", err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", errors.New("economic input has trailing JSON")
	}
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// Exit zero means only the named calculation or mode precondition succeeded.
// Economic activation stays blocked until the separate runtime outcome proof.
func runEconomicCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	if command == "economic-reference" {
		inputPath := flags.String("input", "", "explicit reference input JSON file")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *inputPath == "" {
			fmt.Fprintln(stderr, "economic-reference requires --input FILE and no positional arguments")
			return 2
		}
		var input economicReferenceInput
		inputHash, err := readEconomicInput(*inputPath, &input)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		reference, err := calculateEconomicReference(input, inputHash)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if err := json.NewEncoder(stdout).Encode(reference); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	policyPath := flags.String("policy", "", "explicit independently reviewed network/runtime policy JSON file")
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) RPC URL")
	retryWindow := flags.Duration("retry-window", 60*time.Second, "total finalized mode observation deadline")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *policyPath == "" || *rpcUrl == "" {
		fmt.Fprintln(stderr, "check-recycle-mode requires --rpc URL --policy FILE and no positional arguments")
		return 2
	}
	var policy recyclePolicy
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
	observation, err := client.readRecycleMode(ctx, policy, policyHash)
	if err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, errRpcIntegrity) {
			return 3
		}
		return 1
	}
	snapshot, err := sealRecycleMode(observation)
	if err == nil {
		err = json.NewEncoder(stdout).Encode(snapshot)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !observation.ModeGatePassed {
		fmt.Fprintln(stderr, "owner-recycle mode precondition failed: finalized mode is Burn")
		return 3
	}
	return 0
}
