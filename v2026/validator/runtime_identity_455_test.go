// Current455 admission is an exact artifact boundary, not a spec-only upgrade.
package validator

import (
	"context"
	"errors"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Expected values are independent literals from the reviewed finalized code
// and exact-commit artifact; changing production constants cannot bless drift.
func runtime455ValidatorTestConfig() ReleaseConfig {
	return ReleaseConfig{
		RuntimeSpec: 455, TransactionVersion: 1, StateVersion: 1,
		RuntimeCodeHash:     "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a",
		RuntimeMetadataHash: "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc",
	}
}

// The one reviewed455 pair is accepted; every adjacent version or artifact
// mismatch is refused before its configured bytes can become signing authority.
func TestReleaseRuntime455RequiresExactReviewedArtifact(t *testing.T) {
	cfg := runtime455ValidatorTestConfig()
	if err := validateReleaseNativeRuntimeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ReleaseConfig){
		func(value *ReleaseConfig) { value.RuntimeSpec = 454 },
		func(value *ReleaseConfig) { value.RuntimeSpec = 456 },
		func(value *ReleaseConfig) { value.TransactionVersion = 2 },
		func(value *ReleaseConfig) { value.StateVersion = 2 },
		func(value *ReleaseConfig) {
			value.RuntimeCodeHash = "0x725e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef"
		},
		func(value *ReleaseConfig) {
			value.RuntimeMetadataHash = "0x4d17516b694ef8d18f8a565dcb2df0117e7a0018a3ffa40812c91a1621225702"
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

// A formerly reviewed release lock is evidence, not authority for new455
// steering. Refusal happens before even a public read or chain-binding mutation.
func TestReleaseRuntime455RejectsHistoricalConfigBeforeRpc(t *testing.T) {
	calls := 0
	client := &validatorRuntimeIdentityTestClient{callContext: func(context.Context, any, string, ...any) error {
		calls++
		return errors.New("historical current config reached public rpc")
	}}
	metadata := types.NewMetadataV14()
	runtime := &types.RuntimeVersion{SpecName: "retained", SpecVersion: 454, TransactionVersion: 1}
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: metadata, Runtime: runtime}
	cfg := runtime455ValidatorTestConfig()
	cfg.RuntimeSpec = 454
	cfg.RuntimeCodeHash = "0x725e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef"
	cfg.RuntimeMetadataHash = "0x4d17516b694ef8d18f8a565dcb2df0117e7a0018a3ffa40812c91a1621225702"
	if err := authenticatePinnedNativeRuntimeAtContext(context.Background(), chain, &cfg, types.Hash{9}); err == nil {
		t.Fatal("historical454 config became current authority")
	}
	if calls != 0 || chain.Meta != metadata || chain.Runtime != runtime {
		t.Fatal("refused historical config changed chain state or made a public read")
	}
}
