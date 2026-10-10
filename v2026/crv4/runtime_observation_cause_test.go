// Failed reads do not establish contradictory chain facts. Actual returned
// values still traverse the unchanged exact identity and finality checks.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

func TestEvmCheckpointObservationErrorsDoNotInventIdentity(t *testing.T) {
	for _, stage := range []string{"genesis", "canonical", "header", "parent", "parent-header", "finalized"} {
		fixture, query, _ := newEVMCheckpointTestFixture(t)
		prior := fixture.hook
		parent := fixture.headers[query.NativeHash.Hex()].ParentHash
		faulted := false
		fixture.hook = func(ctx context.Context, out any, method string, args ...any) (bool, error) {
			selected := false
			if len(args) == 1 {
				switch stage {
				case "genesis":
					selected = method == "chain_getBlockHash" && args[0] == uint64(0)
				case "canonical":
					selected = method == "chain_getBlockHash" && args[0] == query.NativeNumber
				case "header":
					selected = method == "chain_getHeader" && args[0] == query.NativeHash.Hex()
				case "parent":
					selected = method == "chain_getBlockHash" && args[0] == query.NativeNumber-1
				case "parent-header":
					selected = method == "chain_getHeader" && args[0] == parent.Hex()
				case "finalized":
					selected = method == "chain_getHeader" && args[0] == fixture.finalized.Hex()
				}
			}
			if selected {
				faulted = true
				return true, context.DeadlineExceeded
			}
			return prior(ctx, out, method, args...)
		}
		beforeMetadata, beforeRuntime := fixture.chain.Meta, fixture.chain.Runtime
		observed, err := ReadEVMCheckpointAtContext(fixture.ctx, fixture.chain, query, fixture.allowed...)
		if !faulted || observed != (EVMCheckpointObservation{}) || !errors.Is(err, context.DeadlineExceeded) || !retryableSubstrateRpcReadTransport(err, false, false) {
			t.Fatalf("%s checkpoint unavailable read became contradictory evidence: observed=%+v err=%v", stage, observed, err)
		}
		fixture.hook = prior
		observed, err = ReadEVMCheckpointAtContext(fixture.ctx, fixture.chain, query, fixture.allowed...)
		if err != nil || observed.Query != query || fixture.chain.Meta != beforeMetadata || fixture.chain.Runtime != beforeRuntime {
			t.Fatalf("%s same selected checkpoint did not recover intact: %+v %v", stage, observed, err)
		}
	}
}

func TestEvmCheckpointReturnedIdentityConflictsRemainHard(t *testing.T) {
	for _, stage := range []string{"genesis", "canonical", "parent", "finalized"} {
		fixture, query, _ := newEVMCheckpointTestFixture(t)
		switch stage {
		case "genesis":
			fixture.blockHashes[0] = types.Hash{0x71}
		case "canonical":
			fixture.blockHashes[query.NativeNumber] = types.Hash{0x72}
		case "parent":
			fixture.blockHashes[query.NativeNumber-1] = types.Hash{0x73}
		case "finalized":
			fixture.finalized = fixture.blockHashes[query.NativeNumber-1]
		}
		observed, err := ReadEVMCheckpointAtContext(fixture.ctx, fixture.chain, query, fixture.allowed...)
		if err == nil || observed != (EVMCheckpointObservation{}) || retryableSubstrateRpcReadTransport(err, false, false) {
			t.Fatalf("%s returned checkpoint contradiction became retryable: %+v %v", stage, observed, err)
		}
	}
}

func TestProvisionalRuntimeUnavailableGenesisDoesNotBecomeDifferentChain(t *testing.T) {
	fixture := newRuntimeProofFixture(t)
	client := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	read := client.callContext
	faulted := false
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if method == "chain_getBlockHash" {
			faulted = true
			return context.DeadlineExceeded
		}
		return read(ctx, target, method, args...)
	}
	result, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, types.Hash{1}, fixture.allowed)
	if !faulted || !errors.Is(err, context.DeadlineExceeded) || !retryableSubstrateRpcReadTransport(err, false, false) || result.compatibilityProof != nil || fixture.observations.Load() != 0 {
		t.Fatalf("unread provisional genesis acquired contradictory or admitted authority: %+v %v", result, err)
	}
	client.callContext = read
	result, err = AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, types.Hash{1}, fixture.allowed)
	if err != nil || !fixture.chain.RuntimeArtifactCompatible(result) || fixture.observations.Load() != 1 {
		t.Fatalf("original provisional owner could not recover exact genesis: %v", err)
	}
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if method == "chain_getBlockHash" {
			*(target.(*types.Hash)) = types.Hash{0x51}
			return nil
		}
		return read(ctx, target, method, args...)
	}
	if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, types.Hash{1}, fixture.allowed); err == nil || retryableSubstrateRpcReadTransport(err, false, false) || !strings.Contains(err.Error(), "genesis differs") {
		t.Fatalf("returned foreign genesis did not remain hard: %v", err)
	}
}

// A malformed but completely returned API tuple is still a hard decoding
// failure, and a well-formed changed tuple remains an explicit contradiction.
func TestRuntimePurposeMalformedAndDifferentTuplesRemainDistinct(t *testing.T) {
	for _, raw := range []string{`{"specName":`, `{"specName":"different","specVersion":7,"transactionVersion":1,"stateVersion":1}`} {
		fixture := validatorProducerRuntimeFixture(t)
		artifact := fixture.bind(t)
		client := fixture.chain.API.Client.(*runtimeIdentityTestClient)
		read := client.callContext
		client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
			if method == "state_getRuntimeVersion" {
				*(target.(*json.RawMessage)) = json.RawMessage(raw)
				return nil
			}
			return read(ctx, target, method, args...)
		}
		err := ValidateValidatorProducerRuntimeArtifactContext(t.Context(), fixture.chain, artifact)
		if err == nil || retryableSubstrateRpcReadTransport(err, false, false) {
			t.Fatalf("complete invalid runtime API became transport availability: %v", err)
		}
		changed := strings.Contains(err.Error(), "runtime changed during capability authentication")
		if changed != (strings.Contains(raw, "different")) {
			t.Fatalf("runtime decode absence and returned changed tuple were conflated: %v", err)
		}
	}
}

func TestRuntimeStakeMalformedAndDifferentTuplesRemainDistinct(t *testing.T) {
	for _, raw := range []string{`{"specName":`, `{"specName":"different","specVersion":7,"transactionVersion":1,"stateVersion":1}`} {
		fixture := newValidatorStakeCapabilityTestFixture(t)
		client := fixture.stake.identity.chain.API.Client.(*runtimeIdentityTestClient)
		read, versions := client.callContext, 0
		client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
			if method == "state_getRuntimeVersion" {
				versions++
				if versions == 2 {
					*(target.(*json.RawMessage)) = json.RawMessage(raw)
					return nil
				}
			}
			return read(ctx, target, method, args...)
		}
		observed, err := fixture.stake.read()
		if versions != 2 || observed != (ValidatorStakeObservation{}) || err == nil || retryableSubstrateRpcReadTransport(err, false, false) ||
			strings.Contains(err.Error(), "runtime changed during capability observation") != strings.Contains(raw, "different") {
			t.Fatalf("public stake malformed tuple and observed runtime change were conflated: versions=%d err=%v", versions, err)
		}
	}
}
