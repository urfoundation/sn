// Opt-in public testnet observations exercise the complete exact-block stake
// reader. They neither submit transactions nor certify activation inclusion.
package crv4

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// UID0 is a live registered observation, not a claim that we own that validator
// or that an arbitrary weight transaction from it would be accepted.
func TestLiveValidatorStakeRuntime455Testnet521(t *testing.T) {
	if os.Getenv("CRV4_LIVE_VALIDATOR_STAKE") != "1" {
		t.Skip("set CRV4_LIVE_VALIDATOR_STAKE=1 for the read-only public testnet stake probe")
	}
	const endpoint = "wss://test.finney.opentensor.ai:443"
	genesis, err := types.NewHashFromHexString("0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105")
	if err != nil {
		t.Fatal(err)
	}
	allowed := RuntimeArtifactIdentity{
		Version:      RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 455, TransactionVersion: 1, StateVersion: 1},
		CodeHash:     "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a",
		MetadataHash: "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	chain, err := DialChainContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	if chain.GenesisHash != genesis {
		t.Fatalf("endpoint returned genesis%s, want%s", chain.GenesisHash.Hex(), genesis.Hex())
	}
	finalized, err := FinalizedHeadContext(ctx, chain)
	if err != nil {
		t.Fatal(err)
	}
	header, err := chain.HeaderAtContext(ctx, finalized)
	if err != nil {
		t.Fatal(err)
	}
	query := ValidatorIdentityQuery{GenesisHash: genesis, BlockHash: finalized, BlockNumber: uint64(header.Number), Netuid: 521, UID: 0, MaximumSubnetUIDs: 256}
	metadata, runtime := chain.Meta, chain.Runtime
	first, err := ReadValidatorStakeAtContext(ctx, chain, query, allowed)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := ReadValidatorStakeAtContext(ctx, chain, query, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateValidatorIdentityReplay(first.Identity, replayed.Identity); err != nil {
		t.Fatalf("public stake identity replay differs: %v", err)
	}
	// The identity helper checked every field before only the allowed finality
	// change is normalized; weighted stake and owner/threshold must remain exact.
	comparison := replayed
	comparison.Identity = first.Identity
	if comparison != first || chain.Meta != metadata || chain.Runtime != runtime {
		t.Fatalf("public exact-block stake replay differs: first=%+v replay=%+v", first, replayed)
	}
	encoded, err := json.Marshal(struct {
		Endpoint string                    `json:"endpoint"`
		First    ValidatorStakeObservation `json:"first"`
		Replay   ValidatorStakeObservation `json:"replay"`
	}{Endpoint: endpoint, First: first, Replay: replayed})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("read_only_stake_observation=%s", encoded)
}
