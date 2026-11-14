// Current fleet admission authenticates complete native headers before any
// signing or durable preparation, including the block installing an upgrade.
package miner

import (
	"context"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	snchain "github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
)

// An approved current artifact remains usable at the runtime-update block;
// its complete digest must survive without granting another artifact authority.
func TestFleetMainnetRegisterAuthenticatesFinalizedUpgradeHeader(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.stateLock.Lock()
	fixture.nativeRuntimeUpdateAt = 100
	err := fixture.rebuildNativeBlocksWithLock()
	fixture.stateLock.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := fleetRegister(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil {
		t.Fatalf("approved upgrade header prevented registration preparation: %v", err)
	}
	if fixture.count("payment_queryInfo") != 1 || fixture.count("author_submitAndWatchExtrinsic") != 0 {
		t.Fatal("registration did not quote one synthetic dry run without broadcasting")
	}
}

// Publication retains the original complete prepared header and exact signed
// bytes; reopening the completed action cannot spend a second native nonce.
func TestFleetMainnetPublishRetainsUpgradePreparedHeaderAcrossRestart(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.stateLock.Lock()
	fixture.nativeRuntimeUpdateAt = 100
	err := fixture.rebuildNativeBlocksWithLock()
	fixture.stateLock.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	for range 2 {
		if err := fleetPublish(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil {
			t.Fatalf("approved upgrade publication or retained recovery failed: %v", err)
		}
	}
	if fixture.count("author_submitAndWatchExtrinsic") != 1 {
		t.Fatal("reopening the upgrade preparation repeated publication")
	}
}

// Both Evm fleet writes retain their native preparation at a complete upgrade
// header and recover their original outcome without another signed transaction.
func TestFleetMainnetEvmRetainsUpgradePreparedHeaderAcrossRestart(t *testing.T) {
	for _, action := range []string{"bind", "revoke"} {
		fixture := newFleetMainnetTestFixture(t)
		fixture.stateLock.Lock()
		fixture.nativeRuntimeUpdateAt = 100
		err := fixture.rebuildNativeBlocksWithLock()
		fixture.stateLock.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		fixture.opts["--dry-run"] = false
		for range 2 {
			if action == "bind" {
				err = fleetBind(fixture.durable.Context, fixture.opts, fixture.manifest)
			} else {
				err = fleetRevoke(fixture.durable.Context, fixture.opts, fixture.manifest)
			}
			if err != nil {
				t.Fatalf("%s approved upgrade preparation or retained recovery failed: %v", action, err)
			}
		}
		if fixture.count("eth_sendRawTransaction") != 1 {
			t.Fatalf("%s reopening the upgrade preparation repeated its send", action)
		}
	}
}

// A matching runtime tuple and Rpc height lookup cannot authenticate an
// incomplete or substituted header. The shared registration/stake reader must
// refuse it before selecting metadata or changing its caller's bound view.
func TestFleetFinalizedRuntimeRejectsUnauthenticatedHeader(t *testing.T) {
	for _, fault := range []string{"number", "parent", "state", "extrinsics", "missing", "digest"} {
		fixture := newFleetMainnetTestFixture(t)
		native, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(native.API.Client.Close)
		priorMeta, priorRuntime := native.Meta, native.Runtime
		reads := fixture.count("state_getStorageHash")
		fixture.stateLock.Lock()
		header := fixture.nativeHeaderWireWithLock(100).(map[string]any)
		switch fault {
		case "number":
			header["number"] = "0x65"
		case "parent":
			header["parentHash"] = (types.Hash{0xa1}).Hex()
		case "state":
			header["stateRoot"] = (types.Hash{0xa2}).Hex()
		case "extrinsics":
			header["extrinsicsRoot"] = (types.Hash{0xa3}).Hex()
		case "missing":
			delete(header, "stateRoot")
		case "digest":
			header["digest"] = map[string]any{"logs": []string{"0x0800"}}
		}
		fixture.nativeHeaderOverrides = map[uint64]any{100: header}
		fixture.stateLock.Unlock()
		view, _, err := snchain.AuthenticateFinalizedRuntimeContext(t.Context(), native, fixture.authority.artifactIdentity())
		if err == nil || view != nil {
			t.Fatalf("%s header acquired a finalized runtime view", fault)
		}
		if fixture.count("state_getStorageHash") != reads || native.Meta != priorMeta || native.Runtime != priorRuntime {
			t.Fatalf("%s header reached artifact selection or changed the caller view", fault)
		}
	}
}

// A canonical replacement during either cold or warm artifact authentication
// cannot publish a view. The explicit Rpc hook fixes the ordering deterministically.
func TestFleetFinalizedRuntimeRejectsClosingCanonicalChange(t *testing.T) {
	for _, warm := range []bool{false, true} {
		fixture := newFleetMainnetTestFixture(t)
		native, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(native.API.Client.Close)
		if warm {
			if _, _, err := snchain.AuthenticateFinalizedRuntimeContext(t.Context(), native, fixture.authority.artifactIdentity()); err != nil {
				t.Fatal(err)
			}
		}
		priorMeta, priorRuntime := native.Meta, native.Runtime
		fixture.stateLock.Lock()
		fixture.hook = func(method string) {
			if method == "state_getStorageHash" {
				fixture.nativeBlocks[100] = types.Hash{0xa4}
			}
		}
		fixture.stateLock.Unlock()
		view, _, err := snchain.AuthenticateFinalizedRuntimeContext(t.Context(), native, fixture.authority.artifactIdentity())
		if err == nil || view != nil || !strings.Contains(err.Error(), "canonical") {
			t.Fatalf("warm=%t closing canonical replacement acquired a view: %v", warm, err)
		}
		if native.Meta != priorMeta || native.Runtime != priorRuntime {
			t.Fatal("closing canonical failure changed the caller view")
		}
	}
}

// A canceled operation cannot return the immutable view after a successful
// complete-header read; it cannot continue into a caller's signing path.
func TestFleetFinalizedRuntimeCancellationPreservesPriorView(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	native, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer native.API.Client.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.stateLock.Lock()
	fixture.hook = func(method string) {
		if method == "chain_getHeader" {
			cancel()
		}
	}
	fixture.stateLock.Unlock()
	priorMeta, priorRuntime := native.Meta, native.Runtime
	view, _, err := snchain.AuthenticateFinalizedRuntimeContext(ctx, native, fixture.authority.artifactIdentity())
	if err == nil || view != nil || native.Meta != priorMeta || native.Runtime != priorRuntime {
		t.Fatalf("canceled header admission acquired or changed the bound view: %v", err)
	}
}

// All public fleet commands share the same current authority. None may use a
// changed root to prepare, sign, publish consent or report commitment success.
func TestFleetMainnetCommandsRejectUncommittedCurrentHeader(t *testing.T) {
	for _, action := range []string{"register", "publish", "bind", "revoke", "status"} {
		fixture := newFleetMainnetTestFixture(t)
		fixture.stateLock.Lock()
		header := fixture.nativeHeaderWireWithLock(100).(map[string]any)
		header["stateRoot"] = (types.Hash{0xa7}).Hex()
		fixture.nativeHeaderOverrides = map[uint64]any{100: header}
		fixture.stateLock.Unlock()
		var err error
		switch action {
		case "register":
			err = fleetRegister(fixture.durable.Context, fixture.opts, fixture.manifest)
		case "publish":
			err = fleetPublish(fixture.durable.Context, fixture.opts, fixture.manifest)
		case "bind":
			err = fleetBind(fixture.durable.Context, fixture.opts, fixture.manifest)
		case "revoke":
			err = fleetRevoke(fixture.durable.Context, fixture.opts, fixture.manifest)
		case "status":
			err = fleetStatus(fixture.durable.Context, fixture.opts, fixture.manifest)
		}
		if err == nil || !strings.Contains(err.Error(), "header SCALE hash") {
			t.Fatalf("%s admitted an uncommitted current header: %v", action, err)
		}
		for _, method := range []string{"payment_queryInfo", "author_submitAndWatchExtrinsic", "eth_sendRawTransaction", "eth_call", "state_getStorage"} {
			if fixture.count(method) != 0 {
				t.Fatalf("%s uncommitted header reached %s", action, method)
			}
		}
	}
}

// Native and Evm fleet owners use this same artifact gate, including warm
// metadata reuse. Canonicality must close after the exact runtime read.
func TestFleetMainnetAuthorityRejectsClosingCanonicalChange(t *testing.T) {
	for _, warm := range []bool{false, true} {
		fixture := newFleetMainnetTestFixture(t)
		native, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(native.API.Client.Close)
		if warm {
			if _, err := fixture.authority.viewAt(t.Context(), native, fixture.head); err != nil {
				t.Fatal(err)
			}
		}
		fixture.stateLock.Lock()
		fixture.hook = func(method string) {
			if method == "state_getStorageHash" {
				fixture.nativeBlocks[100] = types.Hash{0xa8}
			}
		}
		fixture.stateLock.Unlock()
		priorMeta, priorRuntime := native.Meta, native.Runtime
		view, err := fixture.authority.viewAt(t.Context(), native, fixture.head)
		if err == nil || view != nil || !strings.Contains(err.Error(), "canonical") {
			t.Fatalf("warm=%t canonical replacement acquired fleet authority: %v", warm, err)
		}
		if native.Meta != priorMeta || native.Runtime != priorRuntime {
			t.Fatal("failed fleet admission changed the original view")
		}
	}
}
