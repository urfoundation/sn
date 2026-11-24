//go:build linux || darwin

// The api reaches its native node through a forward to another host, so each
// storage round trip of the permit census is a network round trip. These
// tests pin the census to two batched round trips, its results to those of
// one state_getStorage read per entry, and an incomplete batch to the same
// fallback as a failed read.
package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/crv4"
)

// The census as 62a6b72d read it: one state_getStorage per entry, in order.
// Batched reads must give exactly its results.
func validatorUploadPermitCensusSequentialReference(self *ValidatorUploadAdmission, ctx context.Context, nativeObserver ValidatorUploadNativeObserver, currentRuntime []crv4.RuntimeArtifactIdentity) (validatorUploadPermitCensus, error) {
	empty := validatorUploadPermitCensus{}
	deployment := self.config.Deployment
	artifact, err := crv4.AuthenticateRuntimeArtifactAtContext(ctx, self.native, nativeObserver.Hash, currentRuntime...)
	if err != nil {
		return empty, fmt.Errorf("validator staging permit census runtime: %w", err)
	}
	netuid := binary.LittleEndian.AppendUint16(nil, deployment.Netuid)
	read := func(name string, maximum int, args ...[]byte) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key, err := types.CreateStorageKey(artifact.Metadata, crv4.PalletName, name, args...)
		if err != nil {
			return nil, fmt.Errorf("validator staging permit census %s key: %w", name, err)
		}
		raw := json.RawMessage("null")
		if err := self.native.API.Client.CallContext(ctx, &raw, "state_getStorage", key.Hex(), nativeObserver.Hash.Hex()); err != nil {
			return nil, fmt.Errorf("validator staging permit census %s read: %w", name, err)
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, nil
		}
		var encoded string
		if err := json.Unmarshal(raw, &encoded); err != nil || len(encoded) > 2+2*maximum {
			return nil, fmt.Errorf("validator staging permit census %s is not bounded hex", name)
		}
		value, err := hex.DecodeString(strings.TrimPrefix(encoded, "0x"))
		if err != nil || !strings.HasPrefix(encoded, "0x") {
			return nil, fmt.Errorf("validator staging permit census %s is not hex", name)
		}
		return value, nil
	}
	census, err := read("SubnetworkN", 2, netuid)
	if err != nil {
		return empty, err
	}
	if len(census) != 2 {
		return empty, errors.New("validator staging permit census SubnetworkN is not an exact u16")
	}
	count := binary.LittleEndian.Uint16(census)
	if count == 0 || uint32(count) > deployment.MaximumSubnetUIDs {
		return empty, errors.New("validator staging permit census exceeds its subnet bound")
	}
	permits, err := read("ValidatorPermit", int(count)+4, netuid)
	if err != nil {
		return empty, err
	}
	prefix, err := codec.Encode(types.NewUCompactFromUInt(uint64(count)))
	if err != nil {
		return empty, err
	}
	if len(permits) != len(prefix)+int(count) || !bytes.Equal(permits[:len(prefix)], prefix) {
		return empty, errors.New("validator staging permit vector does not match the subnet census")
	}
	result := validatorUploadPermitCensus{stakes: make(map[[32]byte]uint64)}
	include := func(hotkey [32]byte) {
		if _, exists := result.stakes[hotkey]; !exists {
			result.stakes[hotkey] = 0
			result.hotkeys = append(result.hotkeys, hotkey)
		}
	}
	for uid, permit := range permits[len(prefix):] {
		if permit > 1 {
			return empty, errors.New("validator staging permit vector contains a non-boolean value")
		}
		if permit == 0 {
			continue
		}
		hotkey, err := read("Keys", 32, netuid, binary.LittleEndian.AppendUint16(nil, uint16(uid)))
		if err != nil {
			return empty, err
		}
		if len(hotkey) != 32 || [32]byte(hotkey) == ([32]byte{}) {
			return empty, fmt.Errorf("validator staging permit census UID %d has no hotkey", uid)
		}
		include([32]byte(hotkey))
	}
	owner, err := read("SubnetOwnerHotkey", 32, netuid)
	if err != nil {
		return empty, err
	}
	if owner != nil {
		if len(owner) != 32 {
			return empty, errors.New("validator staging permit census subnet owner is not an exact AccountId32")
		}
		include([32]byte(owner))
	}
	slices.SortFunc(result.hotkeys, func(a, b [32]byte) int { return bytes.Compare(a[:], b[:]) })
	return result, nil
}

// An admission with no refresh, and the census inputs a refresh would read.
func validatorUploadCensusTestInputs(t *testing.T, fixture *validatorUploadAuthorityTestFixture) (*ValidatorUploadAdmission, ValidatorUploadNativeObserver, []crv4.RuntimeArtifactIdentity) {
	t.Helper()
	owner, _ := validatorUploadSilentDropTestState(t, fixture)
	nativeObserver, err := ValidatorUploadNativeObserverContext(t.Context(), fixture.native.chain, owner.config.Deployment)
	if err != nil {
		t.Fatal(err)
	}
	currentRuntime, err := owner.config.Deployment.runtimeArtifactsAt(nativeObserver.Number, false)
	if err != nil {
		t.Fatal(err)
	}
	return owner, nativeObserver, currentRuntime
}

// Two round trips: the subnet's three entries, then every permitted UID's
// Keys. Before, each entry was its own read, four here and nineteen on
// mainnet SN25 today.
func TestValidatorUploadPermitCensusTakesTwoRoundTrips(t *testing.T) {
	t.Parallel()
	for _, permit := range []bool{true, false} {
		t.Run(fmt.Sprintf("permit=%t", permit), func(t *testing.T) {
			t.Parallel()
			fixture := newValidatorUploadAuthorityTestFixture(t)
			fixture.currentPermit = permit
			owner, nativeObserver, currentRuntime := validatorUploadCensusTestInputs(t, fixture)
			fixture.censusBatches.Store(0)
			fixture.censusReads.Store(0)
			census, err := owner.readValidatorUploadPermitCensus(t.Context(), nativeObserver, currentRuntime)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(census.hotkeys, fixture.authority.Expected.Hotkey) != permit {
				t.Fatalf("prerequisite: the census holds the validator %t", permit)
			}
			want := int64(1)
			if permit {
				want = 2
			}
			if trips := fixture.censusBatches.Load() + fixture.censusReads.Load(); trips != want || fixture.censusReads.Load() != 0 {
				t.Fatalf("the census took %d storage round trips (%d single reads), want %d batches", trips, fixture.censusReads.Load(), want)
			}
		})
	}
}

// For every answer the fixture node can give, the batched census equals the
// sequential one: the same hotkeys and stakes, or a failure for both.
func TestValidatorUploadPermitCensusMatchesSequentialReads(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		fault  string
		permit bool
		fails  bool
	}{
		{permit: true},
		{permit: false},
		{fault: "current-hotkey-unregistered", permit: true},
		{fault: "census-keys-absent", permit: true, fails: true},
		{fault: "current-schedule-unavailable", permit: true, fails: true},
	} {
		t.Run(fmt.Sprintf("%s/permit=%t", test.fault, test.permit), func(t *testing.T) {
			t.Parallel()
			fixture := newValidatorUploadAuthorityTestFixture(t)
			fixture.fault, fixture.currentPermit = test.fault, test.permit
			owner, nativeObserver, currentRuntime := validatorUploadCensusTestInputs(t, fixture)
			batched, batchedErr := owner.readValidatorUploadPermitCensus(t.Context(), nativeObserver, currentRuntime)
			sequential, sequentialErr := validatorUploadPermitCensusSequentialReference(owner, t.Context(), nativeObserver, currentRuntime)
			if (batchedErr != nil) != test.fails || (sequentialErr != nil) != test.fails {
				t.Fatalf("failure differs: batched %v, sequential %v, want failure %t", batchedErr, sequentialErr, test.fails)
			}
			if !slices.Equal(batched.hotkeys, sequential.hotkeys) || !maps.Equal(batched.stakes, sequential.stakes) {
				t.Fatalf("batched census %+v differs from sequential %+v", batched, sequential)
			}
		})
	}
}

// A batch that fails, leaves out an entry or answers for another block is a
// failed census. The refresh then follows the admitted owners' hotkeys, as
// it does when one read of the census fails, and keeps the admitted
// validator.
func TestValidatorUploadPermitCensusIncompleteBatchFallsBack(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"census-batch-unavailable", "census-batch-partial", "census-batch-foreign-block"} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()
			fixture := newValidatorUploadAuthorityTestFixture(t)
			owner, nativeObserver, currentRuntime := validatorUploadCensusTestInputs(t, fixture)
			if err := owner.refresh(t.Context(), fixture.now); err != nil {
				t.Fatal(err)
			}
			fixture.fault = fault
			if _, err := owner.readValidatorUploadPermitCensus(t.Context(), nativeObserver, currentRuntime); err == nil {
				t.Fatal("an incomplete census batch was a census")
			}
			if err := owner.refresh(t.Context(), fixture.now); err != nil {
				owner.invalidate(err)
				t.Fatalf("an incomplete census batch failed the refresh: %v", err)
			}
			if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, fixture.now); err != nil {
				t.Fatalf("the admitted validator lost its admission to an incomplete census batch: %v", err)
			}
		})
	}
}
