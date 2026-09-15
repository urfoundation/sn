// Current460 admission is an exact artifact boundary, not a spec-only upgrade.
package validator

import (
	"context"
	"errors"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

// Expected values are independent literals from the reviewed finalized code
// and exact-commit artifact; changing production constants cannot bless drift.
func runtime458ValidatorTestConfig() ReleaseConfig {
	return ReleaseConfig{
		RuntimeSpec: 458, TransactionVersion: 1, StateVersion: 1,
		RuntimeCodeHash:     "0x2fdb28e5c3fe4e79844b25dee09ed960e90004432ea2bd98079aba4c5530c51a",
		RuntimeMetadataHash: "0x040088e73e34ed5561372aa51b07b56e41cf7f390312837b074434f30452593d",
	}
}

// Independent literals pin the newly reviewed operational artifact.
func runtime459ValidatorTestConfig() ReleaseConfig {
	return ReleaseConfig{
		RuntimeSpec: 459, TransactionVersion: 1, StateVersion: 1,
		RuntimeCodeHash:     "0x558275958401c026fa4a4159466d49eabd08c761f0c801390593fcba91dee69b",
		RuntimeMetadataHash: "0xcf97fac54fee756137f42e53deeeca828959a74c6d87274898db2c36a33c4fef",
	}
}

// The one reviewed460 pair is accepted; every adjacent version or artifact
// mismatch is refused before its configured bytes can become signing authority.
func TestReleaseRuntime460RequiresExactReviewedArtifact(t *testing.T) {
	cfg := runtime460ValidatorTestConfig()
	if err := validateReleaseNativeRuntimeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ReleaseConfig){
		func(value *ReleaseConfig) { value.RuntimeSpec = 454 },
		func(value *ReleaseConfig) { value.RuntimeSpec = 455 },
		func(value *ReleaseConfig) { value.RuntimeSpec = 456 },
		func(value *ReleaseConfig) { value.RuntimeSpec = 457 },
		func(value *ReleaseConfig) { value.RuntimeSpec = 458 },
		func(value *ReleaseConfig) { value.RuntimeSpec = 459 },
		func(value *ReleaseConfig) { value.RuntimeSpec = 461 },
		func(value *ReleaseConfig) {
			value.RuntimeCodeHash = "0x3708442dc6aae2ea654d827d8b9985d36b6640b2447cfd48125a1a0205c8f1d3"
		},
		func(value *ReleaseConfig) { value.TransactionVersion = 2 },
		func(value *ReleaseConfig) { value.StateVersion = 2 },
		func(value *ReleaseConfig) {
			value.RuntimeCodeHash = "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a"
		},
		func(value *ReleaseConfig) {
			value.RuntimeMetadataHash = "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc"
		},
		func(value *ReleaseConfig) { value.RuntimeCodeHash = "" },
		func(value *ReleaseConfig) { value.RuntimeMetadataHash = "" },
	} {
		mutated := cfg
		mutate(&mutated)
		if err := validateReleaseNativeRuntimeConfig(&mutated); err == nil {
			t.Fatalf("unreviewed current runtime config was accepted: %+v", mutated)
		}
	}
}

// A formerly reviewed release lock is evidence, not authority for new460
// steering. Refusal happens before even a public read or chain-binding mutation.
func TestReleaseRuntime460RejectsHistoricalConfigBeforeRpc(t *testing.T) {
	calls := 0
	client := &validatorRuntimeIdentityTestClient{callContext: func(context.Context, any, string, ...any) error {
		calls++
		return errors.New("historical current config reached public rpc")
	}}
	metadata := types.NewMetadataV14()
	runtime := &types.RuntimeVersion{SpecName: "retained", SpecVersion: 455, TransactionVersion: 1}
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: metadata, Runtime: runtime}
	cfg := runtime460ValidatorTestConfig()
	cfg.RuntimeSpec = 455
	cfg.RuntimeCodeHash = "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a"
	cfg.RuntimeMetadataHash = "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc"
	if err := authenticatePinnedNativeRuntimeAtContext(context.Background(), chain, &cfg, types.Hash{9}); err == nil {
		t.Fatal("historical455 config became current authority")
	}
	if calls != 0 || chain.Meta != metadata || chain.Runtime != runtime {
		t.Fatal("refused historical config changed chain state or made a public read")
	}
}
