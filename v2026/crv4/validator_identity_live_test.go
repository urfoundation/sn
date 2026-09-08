// The opt-in public testnet probe exercises the real native identity reader.
// It observes registration, ownership, stake and permit; it neither submits a
// transaction nor certifies eligibility, activation inclusion or consensus.
package crv4

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// The chain and runtime pins are independent of the RPC response. UID zero is
// a registered subnet observation, not an assumption that its current owner
// is our signing validator. No private wallet or configuration is loaded.
func TestLiveValidatorIdentityRuntime454Testnet521(t *testing.T) {
	if os.Getenv("CRV4_LIVE_VALIDATOR_IDENTITY") != "1" {
		t.Skip("set CRV4_LIVE_VALIDATOR_IDENTITY=1 for the read-only public testnet identity probe")
	}
	const endpoint = "wss://test.finney.opentensor.ai:443"
	genesis, err := types.NewHashFromHexString("0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105")
	if err != nil {
		t.Fatal(err)
	}
	allowed := RuntimeArtifactIdentity{
		Version: RuntimeVersionIdentity{
			SpecName: "node-subtensor", SpecVersion: 454,
			TransactionVersion: 1, StateVersion: 1,
		},
		CodeHash:     "0x725e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef",
		MetadataHash: "0x4d17516b694ef8d18f8a565dcb2df0117e7a0018a3ffa40812c91a1621225702",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	chain, err := DialChainContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	if chain.GenesisHash != genesis {
		t.Fatalf("endpoint returned genesis %s, want testnet %s", chain.GenesisHash.Hex(), genesis.Hex())
	}
	finalized, err := FinalizedHeadContext(ctx, chain)
	if err != nil {
		t.Fatal(err)
	}
	header, err := chain.HeaderAtContext(ctx, finalized)
	if err != nil {
		t.Fatal(err)
	}
	query := ValidatorIdentityQuery{
		GenesisHash: genesis, BlockHash: finalized, BlockNumber: uint64(header.Number),
		Netuid: 521, UID: 0, MaximumSubnetUIDs: 256,
	}
	dialMetadata, dialRuntime := chain.Meta, chain.Runtime
	observed, err := ReadValidatorIdentityAtContext(ctx, chain, query, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if observed.GenesisHash != genesis || observed.BlockHash != finalized || observed.BlockNumber != query.BlockNumber ||
		observed.Netuid != 521 || observed.UID != 0 || observed.SubnetUIDs == 0 || observed.SubnetUIDs > 256 ||
		observed.Hotkey == ([32]byte{}) || observed.Coldkey == ([32]byte{}) || observed.Runtime != allowed ||
		observed.FinalizedHash == (types.Hash{}) || observed.FinalizedNumber < observed.BlockNumber {
		t.Fatalf("public testnet identity observation is incomplete: %+v", observed)
	}
	if chain.Meta != dialMetadata || chain.Runtime != dialRuntime {
		t.Fatal("historical identity read rebound the dial-time metadata or runtime")
	}
	// A replay of the same immutable block may observe a newer finality
	// watermark, but every identity and storage field must remain identical.
	replayed, err := ReadValidatorIdentityAtContext(ctx, chain, query, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateValidatorIdentityReplay(observed, replayed); err != nil {
		t.Fatalf("exact-block identity replay: %v; first=%+v replay=%+v", err, observed, replayed)
	}
	if chain.Meta != dialMetadata || chain.Runtime != dialRuntime {
		t.Fatalf("exact-block identity replay changed: first=%+v replay=%+v", observed, replayed)
	}
	encoded, err := json.Marshal(struct {
		Endpoint string                       `json:"endpoint"`
		First    ValidatorIdentityObservation `json:"first"`
		Replay   ValidatorIdentityObservation `json:"replay"`
	}{Endpoint: endpoint, First: observed, Replay: replayed})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("read_only_identity_observation=%s", encoded)
}
