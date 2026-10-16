// Fleet publication refuses validator and subnet-owner hotkeys before any
// signature. A tagged fleet write from such a hotkey would be mirrored into
// STCoordinator and halt the SN25 validator for good: checkSourceRoleV2 then
// fails with "validator source native slot belongs to another role or
// unretained write" (validator/release_submit_v2.go). Refs support inbox
// 7906, 7916; SN25 validator mirror incident 2026-10-15.
package miner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	snchain "github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The validator message this guard exists to prevent. It is read from the
// validator source so the reference cannot silently drift.
const fleetPublishRoleValidatorHaltMessage = "validator source native slot belongs to another role or unretained write"

// putSubtensor writes one synthetic SubtensorModule value into the fixture.
func (self *fleetMainnetTestFixture) putSubtensor(t *testing.T, name string, value any, args ...[]byte) {
	t.Helper()
	metadata, _, err := crv4.DecodeRuntimeMetadata(self.metadata)
	if err != nil {
		t.Fatal(err)
	}
	key, err := types.CreateStorageKey(metadata, "SubtensorModule", name, args...)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := codec.EncodeToHex(value)
	if err != nil {
		t.Fatal(err)
	}
	self.stateLock.Lock()
	self.storage[key.Hex()] = encoded
	self.stateLock.Unlock()
}

// registerValidatorRole gives the manifest hotkey UID 3 on netuid 25 and,
// when permit is set, the validator permit for that UID.
func (self *fleetMainnetTestFixture) registerValidatorRole(t *testing.T, permit bool) {
	t.Helper()
	self.putSubtensor(t, "Uids", types.U16(3), snchain.NetuidArg(25), self.manifest.Hotkey[:])
	self.putSubtensor(t, "ValidatorPermit", []types.Bool{true, false, false, types.Bool(permit), false}, snchain.NetuidArg(25))
}

func TestFleetPublishRoleGuardReferencesValidatorHaltCheck(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "validator", "release_submit_v2.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(source, []byte(fleetPublishRoleValidatorHaltMessage)) {
		t.Fatalf("validator no longer reports %q; re-check why fleet publish refuses validator hotkeys", fleetPublishRoleValidatorHaltMessage)
	}
}

// A hotkey whose UID holds a validator permit is refused before signing.
func TestFleetMainnetPublishRefusesValidatorPermitHotkey(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.registerValidatorRole(t, true)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	err := fleetPublish(fixture.durable.Context, fixture.opts, fixture.manifest)
	if !errors.Is(err, ErrFleetPublishValidatorHotkey) || !strings.Contains(err.Error(), "validator permit") {
		t.Fatalf("validator permit hotkey published: %v", err)
	}
	if fixture.count("author_submitAndWatchExtrinsic") != 0 || fixture.count("system_accountNextIndex") != 0 {
		t.Fatal("refused publication signed or broadcast")
	}
	store, err := openFleetRecoveryStore(fixture.durable.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	if len(store.records) != 0 {
		t.Fatalf("refused publication left %d prepared records", len(store.records))
	}
}

// The subnet owner hotkey steers without a permit (UID 1 on SN25), so it is
// refused even with no permit and even when unregistered.
func TestFleetMainnetPublishRefusesSubnetOwnerHotkey(t *testing.T) {
	for _, registered := range []bool{false, true} {
		fixture := newFleetMainnetTestFixture(t)
		if registered {
			fixture.registerValidatorRole(t, false)
		}
		fixture.putSubtensor(t, "SubnetOwnerHotkey", types.AccountID(fixture.manifest.Hotkey), snchain.NetuidArg(25))
		fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
		err := fleetPublish(fixture.durable.Context, fixture.opts, fixture.manifest)
		if !errors.Is(err, ErrFleetPublishValidatorHotkey) || !strings.Contains(err.Error(), "subnet owner hotkey") {
			t.Fatalf("registered=%v: owner hotkey published: %v", registered, err)
		}
		if fixture.count("author_submitAndWatchExtrinsic") != 0 {
			t.Fatalf("registered=%v: refused publication broadcast", registered)
		}
	}
}

// A registered miner without a permit, under a different owner hotkey,
// still publishes: the guard does not refuse ordinary fleets.
func TestFleetMainnetPublishAllowsRegisteredNonValidatorHotkey(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.registerValidatorRole(t, false)
	fixture.putSubtensor(t, "SubnetOwnerHotkey", types.AccountID{0x77}, snchain.NetuidArg(25))
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	if err := fleetPublish(fixture.durable.Context, fixture.opts, fixture.manifest); err != nil {
		t.Fatal(err)
	}
	if fixture.count("author_submitAndWatchExtrinsic") != 1 {
		t.Fatal("non-validator fleet did not publish")
	}
}

// A retained prepared publish is a fresh send when replayed. If the hotkey
// has become a validator since, replay is refused and the record is kept.
func TestFleetRecoveryPublishReplayRefusesValidatorHotkey(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	record, signer := fleetRecoveryTestPrepared(t, fixture)
	store, err := openFleetRecoveryStore(fixture.durable.Context)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.put(record, signer); err != nil {
		t.Fatal(err)
	}
	store.close()
	fixture.registerValidatorRole(t, true)
	err = fleetPublish(fixture.durable.Context, fixture.opts, fixture.manifest)
	if !errors.Is(err, ErrFleetPublishValidatorHotkey) {
		t.Fatalf("validator replay was not refused: %v", err)
	}
	retained := fleetRecoveryTestRecord(t, fixture)
	if retained.Stage != "prepared" || retained.TxHash != record.TxHash || !bytes.Equal(retained.Raw, record.Raw) || fixture.count("author_submitAndWatchExtrinsic") != 0 {
		t.Fatal("refused replay advanced or sent the retained transaction")
	}
}

// Unreadable role state refuses the publish (fail closed).
func TestFleetMainnetPublishRoleReadFailureFailsClosed(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	metadata, _, err := crv4.DecodeRuntimeMetadata(fixture.metadata)
	if err != nil {
		t.Fatal(err)
	}
	ownerKey, err := types.CreateStorageKey(metadata, "SubtensorModule", "SubnetOwnerHotkey", snchain.NetuidArg(25))
	if err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	// A value that cannot decode as an AccountId32.
	fixture.storage[ownerKey.Hex()] = "0x01"
	fixture.stateLock.Unlock()
	err = fleetPublish(fixture.durable.Context, fixture.opts, fixture.manifest)
	if err == nil || !strings.Contains(err.Error(), "fleet publish refused") {
		t.Fatalf("unreadable owner hotkey did not refuse: %v", err)
	}
	if fixture.count("author_submitAndWatchExtrinsic") != 0 {
		t.Fatal("unreadable role broadcast")
	}
}

// The pinned (testnet) and authority paths share this function; it is
// exercised directly on a view bound to the fixture's authenticated runtime.
func TestRefuseFleetPublishFromValidatorRoleDirect(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*testing.T, *fleetMainnetTestFixture)
		refuse bool
	}{
		{"unregistered", func(*testing.T, *fleetMainnetTestFixture) {}, false},
		{"registered without permit", func(t *testing.T, f *fleetMainnetTestFixture) { f.registerValidatorRole(t, false) }, false},
		{"permit", func(t *testing.T, f *fleetMainnetTestFixture) { f.registerValidatorRole(t, true) }, true},
		{"uid beyond permit vector", func(t *testing.T, f *fleetMainnetTestFixture) {
			f.putSubtensor(t, "Uids", types.U16(9), snchain.NetuidArg(25), f.manifest.Hotkey[:])
			f.putSubtensor(t, "ValidatorPermit", []types.Bool{true}, snchain.NetuidArg(25))
		}, false},
		{"owner", func(t *testing.T, f *fleetMainnetTestFixture) {
			f.putSubtensor(t, "SubnetOwnerHotkey", types.AccountID(f.manifest.Hotkey), snchain.NetuidArg(25))
		}, true},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			fixture := newFleetMainnetTestFixture(t)
			item.setup(t, fixture)
			ctx := context.Background()
			chain, _, err := dialFleetNativeWithEndpointContext(ctx, []string{fixture.nativeWebsocket(t, false)}, fleetNativeEndpointTimeout, func(parent context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
				return context.WithTimeout(parent, budget)
			}, crv4.DialChainContext, func(context.Context, *crv4.Chain) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			defer closeFleetNative(chain)
			view, block, err := fixture.authority.finalizedFor(ctx, chain, crv4.FleetCommitmentWrite)
			if err != nil {
				t.Fatal(err)
			}
			err = refuseFleetPublishFromValidatorRole(ctx, view, 25, fixture.manifest.Hotkey, block)
			if item.refuse != errors.Is(err, ErrFleetPublishValidatorHotkey) || (!item.refuse && err != nil) {
				t.Fatalf("refuse=%v: %v", item.refuse, err)
			}
		})
	}
}
