// Real local Rpc calls force replacement around production identity and
// closing reads. Runtime and wallet inputs remain original fixture authority.
package miner

import (
	"context"
	"sync/atomic"
	"testing"

	gsrpcclient "github.com/centrifuge/go-substrate-rpc-client/v4/client"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The adapter changes only the transport generation after an actual Rpc read.
// It cannot supply an artifact, decoded response, signing result or verdict.
type fleetRuntimeTransportTestClient struct {
	gsrpcclient.Client
	generation atomic.Uint64
	after      func(string, []any)
}

// Reads the same independent generation contract as the production transport.
func (self *fleetRuntimeTransportTestClient) TransportGeneration() uint64 {
	return self.generation.Load()
}

// The owned calling goroutine observes the exact completed-response boundary.
func (self *fleetRuntimeTransportTestClient) CallContext(ctx context.Context, target any, method string, args ...any) error {
	err := self.Client.CallContext(ctx, target, method, args...)
	if err == nil && self.after != nil {
		self.after(method, args)
	}
	return err
}

// A reconnect inside the artifact read repeats the enclosing network census;
// matching runtime code cannot authenticate a replacement's different genesis.
func TestFleetRuntimeReconnectCannotBorrowEarlierNetworkIdentity(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	native, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(native.API.Client.Close)
	client := &fleetRuntimeTransportTestClient{Client: native.API.Client}
	client.generation.Store(1)
	native.API.Client = client
	faulted, networks := false, 0
	client.after = func(method string, _ []any) {
		if method == "system_chain" {
			networks++
		}
		if method == "state_getStorageHash" && !faulted {
			faulted = true
			client.generation.Add(1)
			fixture.stateLock.Lock()
			fixture.genesis = types.Hash{0xf2}
			fixture.stateLock.Unlock()
		}
	}
	metadata, runtime := native.Meta, native.Runtime
	artifact, err := fixture.authority.authenticateAt(t.Context(), native, fixture.head)
	if err == nil || artifact.Metadata != nil || !faulted || networks != 2 || native.Meta != metadata || native.Runtime != runtime {
		t.Fatalf("fleet replacement borrowed the earlier network identity: networks=%d artifact=%+v err=%v", networks, artifact, err)
	}
}

// The late reconnect used to return an expired artifact or make binding fail
// without retrying the role's earlier identity reads. It now returns one view.
func TestFleetRuntimeClosingReconnectReturnsFreshOwnedView(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	native, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(native.API.Client.Close)
	client := &fleetRuntimeTransportTestClient{Client: native.API.Client}
	client.generation.Store(1)
	native.API.Client = client
	canonicalReads, networks := 0, 0
	client.after = func(method string, args []any) {
		if method == "system_chain" {
			networks++
		}
		if method == "chain_getBlockHash" && args[0] != uint64(0) {
			canonicalReads++
			if canonicalReads == 2 {
				client.generation.Add(1)
			}
		}
	}
	metadata, runtime := native.Meta, native.Runtime
	view, err := fixture.authority.viewAt(t.Context(), native, fixture.head)
	if err != nil || view == nil || view == native || networks != 2 || canonicalReads != 4 ||
		view.Runtime.SpecVersion != types.U32(fixture.authority.RuntimeVersion.SpecVersion) || native.Meta != metadata || native.Runtime != runtime {
		t.Fatalf("fleet closing reconnect failed the complete owned read: networks=%d canonical=%d view=%+v err=%v", networks, canonicalReads, view, err)
	}
}

// Commitment storage completes the runtime observation. A later finalized
// head does not retarget the block selected before transport replacement.
func TestFleetCommitmentReconnectRepeatsOriginalBlockRead(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	native, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(native.API.Client.Close)
	client := &fleetRuntimeTransportTestClient{Client: native.API.Client}
	client.generation.Store(1)
	native.API.Client = client
	networks, storageReads := 0, 0
	client.after = func(method string, args []any) {
		if method == "system_chain" {
			networks++
		}
		if method == "state_getStorage" {
			storageReads++
			if len(args) != 2 || args[1] != fixture.head.Hex() {
				t.Errorf("commitment read escaped its original block: %v", args)
			}
			if storageReads == 2 {
				client.generation.Add(1)
				fixture.stateLock.Lock()
				fixture.finalizedNumber = 101
				fixture.stateLock.Unlock()
			}
		}
	}
	metadata, runtime := native.Meta, native.Runtime
	observed, err := fixture.authority.commitmentFinalized(t.Context(), native, fixture.manifest.Netuid, fixture.manifest.Hotkey)
	if err != nil || observed == nil || observed.FinalizedAt != 100 || observed.FinalizedHash != fixture.head || storageReads != 4 || networks != 2 || native.Meta != metadata || native.Runtime != runtime {
		t.Fatalf("commitment replacement did not repeat the original complete read: storage=%d networks=%d observed=%+v err=%v", storageReads, networks, observed, err)
	}
}

// Finalized receipt readback is read-only. Replacement cannot borrow its
// earlier network census and must never submit the already included write.
func TestFleetCommitmentReceiptReconnectRechecksNetwork(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	native, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(native.API.Client.Close)
	client := &fleetRuntimeTransportTestClient{Client: native.API.Client}
	client.generation.Store(1)
	native.API.Client = client
	networks, storageReads := 0, 0
	client.after = func(method string, _ []any) {
		if method == "system_chain" {
			networks++
		}
		if method == "state_getStorage" {
			storageReads++
			if storageReads == 2 {
				client.generation.Add(1)
				fixture.stateLock.Lock()
				fixture.genesis = types.Hash{0xf4}
				fixture.stateLock.Unlock()
			}
		}
	}
	expected, err := fixture.manifest.CommitmentHash()
	if err != nil {
		t.Fatal(err)
	}
	receipt := &crv4.FinalizedExtrinsic{BlockHash: fixture.head, BlockNumber: 100, ExtrinsicHash: types.Hash{0x61}}
	observed, err := fixture.authority.commitmentWrite(t.Context(), native, fixture.manifest.Netuid, fixture.manifest.Hotkey, expected, receipt)
	if err == nil || observed != nil || storageReads != 2 || networks != 2 || fixture.count("author_submitExtrinsic") != 0 {
		t.Fatalf("receipt replacement borrowed the earlier network: storage=%d networks=%d observed=%+v err=%v", storageReads, networks, observed, err)
	}
}
