// A combined finalized observation selects one authenticated native hash once,
// then retains runtime artifacts and native/EVM mapping at that exact identity.
// It is an unapproved owned-RPC observation, never imported signing authority.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

const finalizedSnapshotSchema = "urnetwork-mainnet-finalized-snapshot-v1"

// The top-level hash names the sole native block shared by both complete
// component records. The old standalone envelope schemas remain unchanged.
type finalizedSnapshot struct {
	Schema          string           `json:"schema"`
	Admission       string           `json:"admission"`
	FinalizedHash   string           `json:"finalized_hash"`
	FinalizedNumber uint64           `json:"finalized_number"`
	Runtime         runtimeSnapshot  `json:"runtime"`
	Mapping         finalizedMapping `json:"mapping"`
}

// Embedding keeps the shared hash at the JSON top level. The digest seals the
// entire finalizedSnapshot value, excluding this added content-hash field.
type finalizedSnapshotEnvelope struct {
	finalizedSnapshot
	ContentHash string `json:"content_hash"`
}

// Only this client's complete in-memory identity can enter an at-identity
// reader. Deserialized v1 identity JSON lacks the private full runtime tuple.
func (self *rpcClient) validateSnapshotIdentity(identity chainIdentity) error {
	if self == nil || identity.Schema != identitySchema || identity.RpcUrl != self.url ||
		!rootCanonicalHash(identity.GenesisHash) || !rootCanonicalHash(identity.FinalizedHash) ||
		identity.ObservedAt == "" || identity.NativeChain == "" || identity.NodeVersion == "" || identity.EvmChainId == 0 ||
		identity.runtimeVersion.SpecName == "" || identity.RuntimeSpec == 0 ||
		identity.RuntimeSpec != uint64(identity.runtimeVersion.SpecVersion) || identity.RuntimeTx != uint64(identity.runtimeVersion.TransactionVersion) {
		return fmt.Errorf("%w: pinned identity is incomplete or belongs to another RPC route", errRpcIntegrity)
	}
	return nil
}

// One total deadline covers selection and both component reads. Runtime bytes
// are collected first; the mapping reader's final native/network/EVM canonical
// rechecks therefore occur after both components' evidence reads.
func (self *rpcClient) readFinalizedSnapshot(ctx context.Context, expected *identityExpectation) (result finalizedSnapshot, resultErr error) {
	if ctx == nil {
		return finalizedSnapshot{}, errors.New("finalized snapshot context is unavailable")
	}
	sampleCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, sampleCtx.Err())
		if resultErr != nil {
			result = finalizedSnapshot{}
		}
	}()
	identity, err := self.readIdentity(sampleCtx)
	if err != nil {
		return finalizedSnapshot{}, err
	}
	if expected != nil {
		if err := expected.match(identity); err != nil {
			return finalizedSnapshot{}, err
		}
	}
	runtime, err := self.readRuntimeSnapshotAtIdentity(sampleCtx, identity)
	if err != nil {
		return finalizedSnapshot{}, err
	}
	mapping, err := self.readFinalizedMappingAtIdentity(sampleCtx, identity)
	if err != nil {
		return finalizedSnapshot{}, err
	}
	result = finalizedSnapshot{
		Schema: finalizedSnapshotSchema, Admission: "unapproved_observation",
		FinalizedHash: identity.FinalizedHash, FinalizedNumber: identity.FinalizedNumber,
		Runtime: runtime, Mapping: mapping,
	}
	if err := result.validateScope(); err != nil {
		return finalizedSnapshot{}, err
	}
	return result, nil
}

// Equal spec numbers or genesis alone cannot join observations. Require exact
// identity, full runtime tuple and retained native header height/hash agreement.
func (self finalizedSnapshot) validateScope() error {
	if self.Schema != finalizedSnapshotSchema || self.Admission != "unapproved_observation" ||
		self.Runtime.Schema != runtimeSnapshotSchema || self.Mapping.Schema != finalizedMappingSchema ||
		self.Runtime.Admission != self.Admission || self.Mapping.Admission != self.Admission ||
		self.Mapping.RuntimeSourceProven || self.Mapping.FinalityAuthority != "owned-rpc-assertion" ||
		!rootCanonicalHash(self.FinalizedHash) || self.Runtime.Identity != self.Mapping.Identity ||
		self.Runtime.Identity.FinalizedHash != self.FinalizedHash || self.Runtime.Identity.FinalizedNumber != self.FinalizedNumber ||
		self.Runtime.Version != self.Mapping.RuntimeVersion {
		return fmt.Errorf("%w: combined finalized snapshot does not share one complete identity", errRpcIntegrity)
	}
	if number, err := self.Mapping.NativeHeader.authenticate(self.FinalizedHash); err != nil || number != self.FinalizedNumber {
		return fmt.Errorf("%w: combined finalized snapshot native header differs: %v", errRpcIntegrity, err)
	}
	return nil
}

// Validation prevents an accidental post-hoc join of the standalone outputs;
// the envelope still proves only integrity of one unapproved observation.
func sealFinalizedSnapshot(snapshot finalizedSnapshot) (finalizedSnapshotEnvelope, error) {
	if err := snapshot.validateScope(); err != nil {
		return finalizedSnapshotEnvelope{}, err
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return finalizedSnapshotEnvelope{}, err
	}
	digest := sha256.Sum256(append([]byte(finalizedSnapshotSchema+"\x00"), raw...))
	return finalizedSnapshotEnvelope{finalizedSnapshot: snapshot, ContentHash: "sha256:" + hex.EncodeToString(digest[:])}, nil
}

// Captures both evidence types together. No input-file merge or signer path is
// exposed; missing mapping capability keeps its distinct exit4 and no output.
func runFinalizedSnapshotCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("finalized-snapshot", flag.ContinueOnError)
	flags.SetOutput(stderr)
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) RPC URL")
	expectedChain := flags.String("expected-chain", "", "independently approved native chain name")
	expectedGenesis := flags.String("expected-genesis", "", "independently approved native genesis hash")
	expectedEvmChainId := flags.Uint64("expected-evm-chain-id", 0, "independently approved EVM chain ID")
	retryWindow := flags.Duration("retry-window", 300*time.Second, "total retry window for both finalized components")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *rpcUrl == "" || *retryWindow < time.Minute || *retryWindow > 15*time.Minute {
		fmt.Fprintln(stderr, "finalized-snapshot requires --rpc, no positional arguments, and a 60s to 15m retry window")
		return 2
	}
	var expected *identityExpectation
	if *expectedChain != "" || *expectedGenesis != "" || *expectedEvmChainId != 0 {
		expected = &identityExpectation{NativeChain: *expectedChain, GenesisHash: *expectedGenesis, EvmChainId: *expectedEvmChainId}
		if expected.NativeChain == "" || !validHash(expected.GenesisHash) || expected.EvmChainId == 0 {
			fmt.Fprintln(stderr, "approved chain, genesis and EVM chain ID must all be supplied")
			return 2
		}
	}
	client, err := newRpcClient(*rpcUrl, *retryWindow)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	snapshot, err := client.readFinalizedSnapshot(ctx, expected)
	if err != nil {
		fmt.Fprintln(stderr, "finalized snapshot:", err)
		if errors.Is(err, errRpcIdentityMismatch) {
			return 3
		}
		if errors.Is(err, errFinalizedMappingUnavailable) {
			return 4
		}
		return 1
	}
	envelope, err := sealFinalizedSnapshot(snapshot)
	if err != nil {
		fmt.Fprintln(stderr, "seal finalized snapshot:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(envelope); err != nil {
		fmt.Fprintln(stderr, "write finalized snapshot:", err)
		return 1
	}
	return 0
}
