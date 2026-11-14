package crv4

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/registry/retriever"
	registryState "github.com/centrifuge/go-substrate-rpc-client/v4/registry/state"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// TestLiveFinalizedExtrinsicEvents is an opt-in incident-analysis tool for a
// finalized native transaction. It does not submit or mutate anything.
func TestLiveFinalizedExtrinsicEvents(t *testing.T) {
	endpoint := os.Getenv("SIM_TESTNET_EVENT_RPC")
	blockText := os.Getenv("SIM_TESTNET_EVENT_BLOCK_HASH")
	txText := os.Getenv("SIM_TESTNET_EVENT_TX_HASH")
	if endpoint == "" || blockText == "" || txText == "" {
		t.Skip("set SIM_TESTNET_EVENT_RPC, SIM_TESTNET_EVENT_BLOCK_HASH, and SIM_TESTNET_EVENT_TX_HASH")
	}
	blockHash, err := types.NewHashFromHexString(blockText)
	if err != nil {
		t.Fatal(err)
	}
	txHash, err := types.NewHashFromHexString(txText)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := DialChain(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	block, err := chain.API.RPC.Chain.GetBlock(blockHash)
	if err != nil {
		t.Fatal(err)
	}
	index, found, err := extrinsicIndex(block.Block.Extrinsics, txHash)
	if err != nil || !found {
		t.Fatalf("find extrinsic: found=%t error=%v", found, err)
	}
	events, err := retriever.NewDefaultEventRetriever(registryState.NewEventProvider(chain.API.RPC.State), chain.API.RPC.State)
	if err != nil {
		t.Fatal(err)
	}
	records, err := events.GetEvents(blockHash)
	if err != nil {
		t.Fatal(err)
	}
	matched := 0
	for _, event := range records {
		if event == nil || event.Phase == nil || !event.Phase.IsApplyExtrinsic || event.Phase.AsApplyExtrinsic != index {
			continue
		}
		matched++
		fields, marshalErr := json.Marshal(event.Fields)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		t.Logf("event=%s fields=%s", event.Name, fields)
	}
	if matched == 0 {
		t.Fatal("finalized extrinsic has no decoded events")
	}
	if expected := os.Getenv("SIM_TESTNET_EVENT_EXPECT_ERROR"); expected != "" {
		verifyErr := chain.VerifyFinalizedExtrinsic(blockHash, txHash)
		if verifyErr == nil || !strings.Contains(verifyErr.Error(), expected) {
			t.Fatalf("dispatch error = %v, want substring %q", verifyErr, expected)
		}
		t.Logf("resolved_dispatch_error=%s", verifyErr)
	}
	storage := os.Getenv("SIM_TESTNET_EVENT_U16_STORAGE")
	netuidText := os.Getenv("SIM_TESTNET_EVENT_NETUID")
	if storage == "" || netuidText == "" {
		return
	}
	netuid, err := strconv.ParseUint(netuidText, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	encodedNetuid := make([]byte, 2)
	binary.LittleEndian.PutUint16(encodedNetuid, uint16(netuid))
	key, err := types.CreateStorageKey(chain.Meta, PalletName, storage, encodedNetuid)
	if err != nil {
		t.Fatal(err)
	}
	var atBlock, atLatest types.U16
	presentAtBlock, err := chain.API.RPC.State.GetStorage(key, &atBlock, blockHash)
	if err != nil {
		t.Fatal(err)
	}
	presentAtLatest, err := chain.API.RPC.State.GetStorageLatest(key, &atLatest)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("storage=%s netuid=%d key=%s at_block=%d present=%t latest=%d present=%t", storage, netuid, key.Hex(), atBlock, presentAtBlock, atLatest, presentAtLatest)
}
