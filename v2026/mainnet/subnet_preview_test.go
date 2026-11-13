// Synthetic chain state exercises authenticated census and identity-bound trim
// previews through the production reader. No signer or live account is present.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
)

// Small fixture vectors use canonical one-byte compact lengths.
func subnetTestVector(data []byte, width int) []byte {
	return append([]byte{byte((len(data) / width) * 4)}, data...)
}

// Two owner seats, two declared validators (one without permit), and two miners
// make runtime immunity distinct from the policy's mandatory preservation.
func newSubnetFixture(t *testing.T, ownerOverride ...[]byte) (*rpcClient, *rootRpcFixture, subnetCensusPolicy) {
	t.Helper()
	client, fixture := newRootFixture(t)
	netuidArg := []byte{25, 0}
	owner := bytes.Repeat([]byte{0x61}, 32)
	if len(ownerOverride) != 0 {
		owner = bytes.Clone(ownerOverride[0])
	}
	fixture.set(t, "SubnetworkN", []byte{6, 0}, netuidArg)
	fixture.set(t, "MaxAllowedUids", []byte{8, 0}, netuidArg)
	fixture.set(t, "MinAllowedUids", []byte{2, 0}, netuidArg)
	fixture.set(t, "ImmunityPeriod", []byte{10, 0}, netuidArg)
	fixture.set(t, "ImmuneOwnerUidsLimit", []byte{1, 0}, netuidArg)
	fixture.set(t, "SubnetOwner", owner, netuidArg)
	fixture.set(t, "SubnetOwnerHotkey", bytes.Repeat([]byte{0x41}, 32), netuidArg)
	fixture.set(t, "OwnedHotkeys", subnetTestVector(append(bytes.Repeat([]byte{0x42}, 32), bytes.Repeat([]byte{0x41}, 32)...), 32), owner)
	fixture.set(t, "NetworkRegisteredAt", binary.LittleEndian.AppendUint64(nil, 10), netuidArg)
	fixture.set(t, "RegisteredSubnetCounter", binary.LittleEndian.AppendUint64(nil, 3), netuidArg)
	fixture.set(t, "NetworkRegistrationAllowed", []byte{0}, netuidArg)
	fixture.set(t, "NetworkPowRegistrationAllowed", []byte{0}, netuidArg)
	fixture.set(t, "Active", subnetTestVector([]byte{1, 1, 1, 1, 1, 0}, 1), netuidArg)
	fixture.set(t, "ValidatorPermit", subnetTestVector([]byte{0, 0, 1, 0, 0, 0}, 1), netuidArg)
	emissions := []byte{}
	for _, value := range []uint64{100, 90, 80, 70, 1, 1} {
		emissions = binary.LittleEndian.AppendUint64(emissions, value)
	}
	fixture.set(t, "Emission", subnetTestVector(emissions, 8), netuidArg)
	fixture.set(t, "Tempo", []byte{100, 0}, netuidArg)
	fixture.set(t, "LastEpochBlock", binary.LittleEndian.AppendUint64(nil, 50), netuidArg)
	fixture.set(t, "PendingEpochAt", binary.LittleEndian.AppendUint64(nil, 0), netuidArg)
	fixture.set(t, "AdminFreezeWindow", []byte{5, 0})
	fixture.set(t, "TransactionKeyLastBlock", binary.LittleEndian.AppendUint64(nil, 0), owner, netuidArg, []byte{9, 0})
	for index := 0; index < 2; index++ {
		fixture.set(t, "IsNetworkMember", []byte{1}, bytes.Repeat([]byte{byte(0x11 + index)}, 32), []byte{0, 0})
	}
	born, generation, maximum := uint64(10), uint64(3), uint16(4)
	policy := subnetCensusPolicy{
		Schema: subnetPolicySchema, Netuid: 25, NativeChain: fixture.policy.NativeChain, GenesisHash: fixture.policy.GenesisHash, EvmChainId: fixture.policy.EvmChainId,
		StorageProfile: subnetStorageProfileName, RuntimeSourceCommit: rootProfileSource, RuntimeVersion: fixture.policy.RuntimeVersion,
		RuntimeCodeHash: fixture.policy.RuntimeCodeHash, RuntimeMetadataHash: fixture.policy.RuntimeMetadataHash, SubnetOwnerColdkey: "0x" + hex.EncodeToString(owner),
		SubnetRegistrationBlock: &born, SubnetGeneration: &generation, TrimMaximumUids: &maximum,
		Remove: []subnetIdentityExpectation{}, Preserve: []subnetProtectedIdentity{},
	}
	for index := 0; index < 6; index++ {
		uidArg := binary.LittleEndian.AppendUint16(nil, uint16(index))
		hotkey := bytes.Repeat([]byte{byte(0x41 + index)}, 32)
		coldkey := bytes.Repeat([]byte{byte(0x61 + index)}, 32)
		if index <= 1 {
			coldkey = owner
		}
		block := uint64(40 + index)
		fixture.set(t, "Keys", hotkey, netuidArg, uidArg)
		fixture.set(t, "Uids", uidArg, netuidArg, hotkey)
		fixture.set(t, "Owner", coldkey, hotkey)
		fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, block), netuidArg, uidArg)
		fixture.set(t, "IsNetworkMember", []byte{1}, hotkey, netuidArg)
		expected := subnetIdentityExpectation{Hotkey: "0x" + hex.EncodeToString(hotkey), Coldkey: "0x" + hex.EncodeToString(coldkey), RegistrationBlock: &block}
		if index >= 4 {
			policy.Remove = append(policy.Remove, expected)
		} else if index >= 2 {
			policy.Preserve = append(policy.Preserve, subnetProtectedIdentity{subnetIdentityExpectation: expected, Roles: []string{"ur-validator"}})
		}
	}
	return client, fixture, policy
}

// A complete favorable selection is still incapable of admitting any mutation.
func TestSubnetPreviewCompleteCensusNeverAuthorizesReset(t *testing.T) {
	client, fixture, policy := newSubnetFixture(t)
	preview, err := client.readSubnetPreview(t.Context(), policy, "sha256:synthetic-policy")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.CensusComplete || preview.ResetReady || len(preview.ResetBlockers) == 0 || len(preview.Blockers) != 0 || len(preview.Seats) != 6 || len(preview.RootRegistrations) != 2 || len(preview.Remove) != 2 || len(preview.Preserve) != 4 || len(preview.Unresolved) != 0 || !preview.Trim.CandidateComplete || !preview.Trim.MatchesRequestedRemovalSet || preview.Trim.Call == nil || len(preview.Trim.Blockers) != 0 {
		t.Fatalf("incorrect read-only preview: %+v", preview)
	}
	if preview.Seats[3].ValidatorPermit || preview.Seats[3].Disposition != "preserve" || !preview.Seats[0].OwnerImmune || preview.Seats[1].OwnerImmune || !preview.Seats[1].OwnerRecognized || preview.Seats[5].Active {
		t.Fatalf("roles, activity or owner immunity collapsed: %+v", preview.Seats)
	}
	if preview.Trim.Removed[0].Uid != 4 || preview.Trim.Removed[1].Uid != 5 || len(preview.Trim.Survivors) != 4 || fixture.count("state_getKeysPaged") != 8 || fixture.count("chain_getFinalizedHead") != 4 {
		t.Fatalf("incomplete mappings or terminal pages: %+v", preview.Trim)
	}
	sealed, err := sealSubnetPreview(preview)
	if err != nil {
		t.Fatal(err)
	}
	preview.Storage[0].ValueSource = "synthetic-tamper"
	changed, err := sealSubnetPreview(preview)
	if err != nil || changed.ContentHash == sealed.ContentHash {
		t.Fatal("raw evidence was not bound by the observation hash")
	}
}

// Missing independent authority or ambiguous scope is rejected before RPC work.
func TestSubnetPolicyRejectsMissingAuthorityAndAmbiguousScope(t *testing.T) {
	for _, change := range []string{"netuid", "chain", "genesis", "source", "version", "owner", "generation", "capacity", "duplicate", "conflict", "missing-registration", "role", "oversized"} {
		client, fixture, policy := newSubnetFixture(t)
		switch change {
		case "netuid":
			policy.Netuid = 0
		case "chain":
			policy.EvmChainId = 945
		case "genesis":
			policy.GenesisHash = "0x" + strings.Repeat("0", 64)
		case "source":
			policy.RuntimeSourceCommit = strings.Repeat("a", 40)
		case "version":
			policy.RuntimeVersion.StateVersion = 0
		case "owner":
			policy.SubnetOwnerColdkey = ""
		case "generation":
			policy.SubnetGeneration = nil
		case "capacity":
			policy.TrimMaximumUids = nil
		case "duplicate":
			policy.Remove = append(policy.Remove, policy.Remove[0])
		case "conflict":
			policy.Preserve = append(policy.Preserve, subnetProtectedIdentity{subnetIdentityExpectation: policy.Remove[0], Roles: []string{"escrow"}})
		case "missing-registration":
			policy.Remove[0].RegistrationBlock = nil
		case "role":
			policy.Preserve[0].Roles = []string{"ur-validator", "ur-validator"}
		case "oversized":
			policy.Remove = make([]subnetIdentityExpectation, rootCensusLimit+1)
		}
		preview, err := client.readSubnetPreview(t.Context(), policy, "policy")
		if err == nil || preview.CensusComplete || fixture.count("system_chain") != 0 {
			t.Fatalf("%s reached RPC or admitted policy: %+v %v", change, preview, err)
		}
	}
}

// Exact runtime artifacts and network identity gate every census storage read.
func TestSubnetPreviewRejectsIdentityAndRuntimeDriftBeforeStorage(t *testing.T) {
	for _, change := range []string{"testnet", "genesis", "runtime", "code", "metadata"} {
		client, fixture, policy := newSubnetFixture(t)
		switch change {
		case "testnet":
			fixture.evmChainHex = "0x3b1"
		case "genesis":
			policy.GenesisHash = "0x" + strings.Repeat("e", 64)
		case "runtime":
			policy.RuntimeVersion.TransactionVersion++
		case "code":
			policy.RuntimeCodeHash = "0x" + strings.Repeat("e", 64)
		case "metadata":
			policy.RuntimeMetadataHash = "0x" + strings.Repeat("e", 64)
		}
		preview, err := client.readSubnetPreview(t.Context(), policy, "policy")
		if !errors.Is(err, errRpcIntegrity) || preview.CensusComplete || fixture.count("state_getStorage") != 0 {
			t.Fatalf("%s reached census storage: %+v %v", change, preview, err)
		}
	}
}

// Removes a known fixture row without changing any unrelated declared state.
func subnetTestDelete(t *testing.T, fixture *rootRpcFixture, name string, args ...[]byte) {
	t.Helper()
	key, err := types.CreateStorageKey(fixture.metadata, "SubtensorModule", name, args...)
	if err != nil {
		t.Fatal(err)
	}
	delete(fixture.storageKVs, key.Hex())
}

// Missing, forged or malformed members cannot be published as partial success.
func TestSubnetPreviewRejectsIncompleteAndContradictoryCensus(t *testing.T) {
	for _, change := range []string{"missing-forward", "extra-reverse", "wrong-reverse", "missing-owner", "zero-hotkey", "missing-generation", "future-generation", "prior-subnet-generation", "membership", "short-permits", "bad-bool", "missing-emission", "duplicate-page", "missing-result", "fork", "root-reverse", "missing-owned", "duplicate-owned", "contradictory-owned"} {
		client, fixture, policy := newSubnetFixture(t)
		netuidArg, uidArg := []byte{25, 0}, []byte{4, 0}
		hotkey := bytes.Repeat([]byte{0x45}, 32)
		switch change {
		case "missing-forward":
			subnetTestDelete(t, fixture, "Keys", netuidArg, uidArg)
		case "extra-reverse":
			fixture.set(t, "Uids", uidArg, netuidArg, bytes.Repeat([]byte{0xee}, 32))
		case "wrong-reverse":
			fixture.set(t, "Uids", []byte{5, 0}, netuidArg, hotkey)
		case "missing-owner":
			subnetTestDelete(t, fixture, "Owner", hotkey)
		case "zero-hotkey":
			fixture.set(t, "Keys", make([]byte, 32), netuidArg, uidArg)
		case "missing-generation":
			subnetTestDelete(t, fixture, "BlockAtRegistration", netuidArg, uidArg)
		case "future-generation":
			fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 101), netuidArg, uidArg)
		case "prior-subnet-generation":
			fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 9), netuidArg, uidArg)
		case "membership":
			fixture.set(t, "IsNetworkMember", []byte{0}, hotkey, netuidArg)
		case "short-permits":
			fixture.set(t, "ValidatorPermit", subnetTestVector([]byte{0, 0, 1}, 1), netuidArg)
		case "bad-bool":
			fixture.set(t, "Active", subnetTestVector([]byte{1, 1, 1, 1, 1, 2}, 1), netuidArg)
		case "missing-emission":
			subnetTestDelete(t, fixture, "Emission", netuidArg)
		case "duplicate-page":
			fixture.duplicatePage = true
		case "missing-result":
			fixture.omitStorage = true
		case "fork":
			fixture.forkAfterStorage = true
		case "root-reverse":
			fixture.set(t, "Uids", []byte{0, 0}, []byte{0, 0}, bytes.Repeat([]byte{0xee}, 32))
		case "missing-owned":
			subnetTestDelete(t, fixture, "OwnedHotkeys", bytes.Repeat([]byte{0x61}, 32))
		case "duplicate-owned":
			fixture.set(t, "OwnedHotkeys", subnetTestVector(bytes.Repeat([]byte{0x41}, 64), 32), bytes.Repeat([]byte{0x61}, 32))
		case "contradictory-owned":
			fixture.set(t, "Owner", bytes.Repeat([]byte{0x77}, 32), bytes.Repeat([]byte{0x42}, 32))
		}
		preview, err := client.readSubnetPreview(t.Context(), policy, "policy")
		if !errors.Is(err, errRpcIntegrity) || preview.CensusComplete || len(preview.Seats) != 0 {
			t.Fatalf("%s published incomplete state: %+v %v", change, preview, err)
		}
	}
}

// Bounds are checked before per-seat enumeration or owned-key worker allocation.
func TestSubnetPreviewRejectsOversizedCensusAndOwnedHotkeys(t *testing.T) {
	for _, owned := range []bool{false, true} {
		client, fixture, policy := newSubnetFixture(t)
		if owned {
			data := binary.LittleEndian.AppendUint16(nil, uint16((rootCensusLimit+1)*4+1))
			data = append(data, make([]byte, (rootCensusLimit+1)*32)...)
			fixture.set(t, "OwnedHotkeys", data, bytes.Repeat([]byte{0x61}, 32))
		} else {
			fixture.set(t, "SubnetworkN", binary.LittleEndian.AppendUint16(nil, rootCensusLimit+1), []byte{25, 0})
			fixture.set(t, "MaxAllowedUids", binary.LittleEndian.AppendUint16(nil, rootCensusLimit+1), []byte{25, 0})
		}
		preview, err := client.readSubnetPreview(t.Context(), policy, "policy")
		if !errors.Is(err, errRpcIntegrity) || preview.CensusComplete || !owned && fixture.count("state_getKeysPaged") != 0 {
			t.Fatalf("oversized owned=%t census admitted: %+v %v", owned, preview, err)
		}
	}
}

// A preserved validator does not lose its identity merely because permit is off.
func TestSubnetPreviewPreservesDeclaredValidatorWithoutPermit(t *testing.T) {
	client, _, policy := newSubnetFixture(t)
	preview, err := client.readSubnetPreview(t.Context(), policy, "policy")
	if err != nil || preview.Seats[3].ValidatorPermit || preview.Seats[3].Disposition != "preserve" || len(preview.Unresolved) != 0 {
		t.Fatalf("permit bit replaced protected identity: %+v %v", preview, err)
	}
}

// Automatic owner/permit protection conflicts remain visible in requested scope.
func TestSubnetPreviewExposesOwnerAndValidatorRemovalConflicts(t *testing.T) {
	for _, owner := range []bool{false, true} {
		client, fixture, policy := newSubnetFixture(t)
		index := 4
		if owner {
			index = 1
			block := uint64(41)
			policy.Remove = append(policy.Remove, subnetIdentityExpectation{Hotkey: "0x" + strings.Repeat("42", 32), Coldkey: policy.SubnetOwnerColdkey, RegistrationBlock: &block})
		} else {
			fixture.set(t, "ValidatorPermit", subnetTestVector([]byte{0, 0, 1, 0, 1, 0}, 1), []byte{25, 0})
		}
		preview, err := client.readSubnetPreview(t.Context(), policy, "policy")
		if err != nil || !preview.CensusComplete || preview.Seats[index].Disposition != "unresolved" || !preview.Seats[index].RequestedRemoval || preview.Trim.MatchesRequestedRemovalSet || !strings.Contains(strings.Join(preview.Blockers, ","), "REMOVAL_CONFLICTS_WITH_PROTECTED_IDENTITY") {
			t.Fatalf("owner=%t conflict was silently omitted: %+v %v", owner, preview, err)
		}
	}
}

// Same UID and hotkey cannot satisfy an approval for another owner/generation.
func TestSubnetPreviewBlocksChangedIdentityAndSubnetGenerations(t *testing.T) {
	for _, change := range []string{"owner", "generation", "missing", "subnet-owner", "subnet-generation", "subnet-birth", "unclassified"} {
		client, _, policy := newSubnetFixture(t)
		switch change {
		case "owner":
			policy.Remove[0].Coldkey = "0x" + strings.Repeat("77", 32)
		case "generation":
			*policy.Remove[0].RegistrationBlock--
		case "missing":
			policy.Remove[0].Hotkey = "0x" + strings.Repeat("77", 32)
		case "subnet-owner":
			policy.SubnetOwnerColdkey = "0x" + strings.Repeat("77", 32)
		case "subnet-generation":
			*policy.SubnetGeneration++
		case "subnet-birth":
			*policy.SubnetRegistrationBlock++
		case "unclassified":
			policy.Remove = policy.Remove[1:]
		}
		preview, err := client.readSubnetPreview(t.Context(), policy, "policy")
		if err != nil || !preview.CensusComplete || len(preview.Blockers) == 0 || preview.Trim.MatchesRequestedRemovalSet || preview.ResetReady {
			t.Fatalf("%s stale scope admitted: %+v %v", change, preview, err)
		}
	}
}

// Explicit owner priority is applied after chronological/UID ordering and limit.
func TestSubnetOwnerImmunityUsesSourceOrderingAndLimit(t *testing.T) {
	seats := []subnetSeat{
		{subnetRegistration: subnetRegistration{Uid: 0, Hotkey: "owner-a", RegistrationBlock: 20}},
		{subnetRegistration: subnetRegistration{Uid: 1, Hotkey: "owner-b", RegistrationBlock: 10}},
		{subnetRegistration: subnetRegistration{Uid: 2, Hotkey: "owner-c", RegistrationBlock: 10}},
	}
	owned := map[string]bool{"owner-a": true, "owner-b": true, "owner-c": true}
	setSubnetOwnerImmunity(seats, owned, nil, 1)
	if seats[0].OwnerImmune || !seats[1].OwnerImmune || seats[2].OwnerImmune {
		t.Fatalf("oldest/lowest UID ordering changed: %+v", seats)
	}
	explicit := "owner-a"
	setSubnetOwnerImmunity(seats, owned, &explicit, 1)
	if !seats[0].OwnerImmune || seats[1].OwnerImmune || seats[2].OwnerImmune {
		t.Fatalf("explicit owner was not inserted before truncation: %+v", seats)
	}
}

// Equal emissions remove the higher UID; survivors retain immutable generations.
func TestSubnetTrimTieOrderingAndSurvivorCompression(t *testing.T) {
	for _, lowerEmission := range []bool{false, true} {
		client, fixture, policy := newSubnetFixture(t)
		*policy.TrimMaximumUids = 5
		removedIndex, preservedIndex := 1, 0
		if lowerEmission {
			removedIndex, preservedIndex = 0, 1
			values := []byte{}
			for _, value := range []uint64{100, 90, 80, 70, 0, 1} {
				values = binary.LittleEndian.AppendUint64(values, value)
			}
			fixture.set(t, "Emission", subnetTestVector(values, 8), []byte{25, 0})
		}
		preserved := policy.Remove[preservedIndex]
		policy.Preserve = append(policy.Preserve, subnetProtectedIdentity{subnetIdentityExpectation: preserved, Roles: []string{"custody"}})
		policy.Remove = []subnetIdentityExpectation{policy.Remove[removedIndex]}
		preview, err := client.readSubnetPreview(t.Context(), policy, "policy")
		if err != nil || !preview.Trim.MatchesRequestedRemovalSet || len(preview.Trim.Removed) != 1 || preview.Trim.Removed[0].Hotkey != policy.Remove[0].Hotkey || len(preview.Trim.Survivors) != 5 {
			t.Fatalf("trim order changed: %+v %v", preview, err)
		}
		last := preview.Trim.Survivors[4]
		if last.NewUid != 4 || last.Hotkey != preserved.Hotkey || last.RegistrationBlock != *preserved.RegistrationBlock {
			t.Fatalf("compression lost identity/generation: %+v", last)
		}
	}
}

// Capacity is the observed minimum/maximum, never a nominal 64-seat constant.
func TestSubnetTrimRespectsRuntimeCapacityAndImmunity(t *testing.T) {
	for _, change := range []string{"minimum", "maximum", "temporary"} {
		client, fixture, policy := newSubnetFixture(t)
		want := "RESET_TRIM_CAPACITY_OUTSIDE_RUNTIME_LIMITS"
		switch change {
		case "minimum":
			*policy.TrimMaximumUids = 1
		case "maximum":
			*policy.TrimMaximumUids = 9
		case "temporary":
			want = "RESET_TRIM_IMMUNITY_THRESHOLD_REACHED"
			for _, uid := range []uint16{3, 4, 5} {
				fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 99), []byte{25, 0}, binary.LittleEndian.AppendUint16(nil, uid))
			}
		}
		preview, err := client.readSubnetPreview(t.Context(), policy, "policy")
		if err != nil || preview.Trim.CandidateComplete || preview.Trim.MatchesRequestedRemovalSet || !strings.Contains(strings.Join(preview.Trim.Blockers, ","), want) {
			t.Fatalf("%s eligibility ignored: %+v %v", change, preview, err)
		}
	}
}

// Whole-percent flooring and the strict threshold match the pinned SDK source.
func TestSubnetTrimPercentFloorAndStrictThreshold(t *testing.T) {
	for _, threshold := range []uint8{33, 34} {
		maximum := uint16(3)
		policy := subnetCensusPolicy{TrimMaximumUids: &maximum}
		seats := []subnetSeat{
			{subnetRegistration: subnetRegistration{Uid: 0}, OwnerImmune: true, emission: 9},
			{subnetRegistration: subnetRegistration{Uid: 1}, emission: 8},
			{subnetRegistration: subnetRegistration{Uid: 2}, emission: 7},
			{subnetRegistration: subnetRegistration{Uid: 3}, emission: 6},
		}
		trim := previewSubnetTrim(seats, policy, 1, 4, true, subnetTrimPreview{MaximumUids: maximum, MaximumImmunePercentage: &threshold, Call: &subnetTrimCall{Pallet: "SyntheticAdmin", Call: "synthetic_trim"}})
		if trim.ImmunePercentage == nil || *trim.ImmunePercentage != 33 || trim.CandidateComplete != (threshold == 34) {
			t.Fatalf("threshold %d changed floor/strict comparison: %+v", threshold, trim)
		}
	}
}

// A valid census cannot hide owner cooldown uncertainty or the current window.
func TestSubnetPreviewReportsTrimTimingBlockers(t *testing.T) {
	for _, change := range []string{"prior-trim", "pending", "freeze", "tempo-zero"} {
		client, fixture, policy := newSubnetFixture(t)
		wantOpen := false
		switch change {
		case "prior-trim":
			fixture.set(t, "TransactionKeyLastBlock", binary.LittleEndian.AppendUint64(nil, 1), bytes.Repeat([]byte{0x61}, 32), []byte{25, 0}, []byte{9, 0})
			wantOpen = true
		case "pending":
			fixture.set(t, "PendingEpochAt", binary.LittleEndian.AppendUint64(nil, 101), []byte{25, 0})
		case "freeze":
			fixture.set(t, "LastEpochBlock", binary.LittleEndian.AppendUint64(nil, 3), []byte{25, 0})
		case "tempo-zero":
			fixture.set(t, "Tempo", []byte{0, 0}, []byte{25, 0})
			fixture.set(t, "PendingEpochAt", binary.LittleEndian.AppendUint64(nil, 101), []byte{25, 0})
			wantOpen = true
		}
		preview, err := client.readSubnetPreview(t.Context(), policy, "policy")
		if err != nil || !preview.CensusComplete || preview.Trim.AdminWindowOpen != wantOpen || preview.ResetReady {
			t.Fatalf("%s window changed: %+v %v", change, preview, err)
		}
		if change == "prior-trim" && !strings.Contains(strings.Join(preview.Trim.Blockers, ","), "RESET_OWNER_TRIM_RATE_LIMIT_BUILD_VALUE_REQUIRES_REVIEW") {
			t.Fatal("source build value was invented from runtime version or docs")
		}
	}
}

// Changed fixture metadata is explicitly approved only within this synthetic test.
func subnetTestApproveMetadata(t *testing.T, fixture *rootRpcFixture, policy *subnetCensusPolicy) {
	t.Helper()
	encoded, err := codec.EncodeToHex(fixture.metadata)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := hex.DecodeString(encoded[2:])
	if err != nil {
		t.Fatal(err)
	}
	digest := blake2b.Sum256(raw)
	fixture.metadataHex = encoded
	policy.RuntimeMetadataHash = "0x" + hex.EncodeToString(digest[:])
}

// A storage schema never supplies a missing runtime call or missing constant.
func TestSubnetPreviewBlocksUnverifiedTrimCapability(t *testing.T) {
	for _, call := range []bool{false, true} {
		client, fixture, policy := newSubnetFixture(t)
		for index := range fixture.metadata.AsMetadataV14.Pallets {
			pallet := &fixture.metadata.AsMetadataV14.Pallets[index]
			if call && pallet.Name == "AdminUtils" {
				pallet.HasCalls = false
			}
			if !call && pallet.Name == "SubtensorModule" {
				for constantIndex := range pallet.Constants {
					if pallet.Constants[constantIndex].Name == "MaxImmuneUidsPercentage" {
						pallet.Constants[constantIndex].Name = "SyntheticUnreviewedPercentage"
					}
				}
			}
		}
		subnetTestApproveMetadata(t, fixture, &policy)
		preview, err := client.readSubnetPreview(t.Context(), policy, "policy")
		if err != nil || !preview.CensusComplete || preview.ResetReady || preview.Trim.CandidateComplete || preview.Trim.MatchesRequestedRemovalSet || call && preview.Trim.Call != nil || !call && preview.Trim.MaximumImmunePercentage != nil || len(preview.Trim.Blockers) == 0 {
			t.Fatalf("missing call=%t acquired capability: %+v %v", call, preview, err)
		}
	}
}

// Tuple order, argument types and duplicate variants cannot inherit trim meaning.
func TestSubnetTrimCallRejectsChangedMetadataShape(t *testing.T) {
	for _, change := range []string{"field", "type", "count", "duplicate-index", "duplicate-pallet", "duplicate-pallet-index"} {
		metadata, _, _ := rootTestMetadata(t)
		for index := range metadata.AsMetadataV14.Pallets {
			pallet := &metadata.AsMetadataV14.Pallets[index]
			if pallet.Name != "AdminUtils" {
				continue
			}
			if change == "duplicate-pallet" {
				metadata.AsMetadataV14.Pallets = append(metadata.AsMetadataV14.Pallets, *pallet)
				break
			}
			if change == "duplicate-pallet-index" {
				metadata.AsMetadataV14.Pallets[0].Index = pallet.Index
				break
			}
			variants := &metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()].Def.Variant.Variants
			for variantIndex := range *variants {
				variant := &(*variants)[variantIndex]
				if variant.Name != "sudo_trim_to_max_allowed_uids" {
					continue
				}
				switch change {
				case "field":
					variant.Fields[0].Name = "synthetic-other-argument"
				case "type":
					variant.Fields[0].Type = types.NewSi1LookupTypeIDFromUInt(999999)
				case "count":
					variant.Fields = variant.Fields[:1]
				case "duplicate-index":
					*variants = append(*variants, *variant)
				}
				break
			}
			break
		}
		if call, err := subnetOwnerTrimCall(metadata); err == nil || call != nil {
			t.Fatalf("%s was recognized as a valid trim call", change)
		}
	}
}

// The shared reader refuses storage outside its selected profile before RPC.
func TestObservationStorageReaderRejectsUndeclaredShapeBeforeRpc(t *testing.T) {
	client, fixture := newRootFixture(t)
	entries, err := rootStorageProfile(fixture.metadata)
	if err != nil {
		t.Fatal(err)
	}
	reader := &rootStorageReader{client: client, metadata: fixture.metadata, entries: entries, block: testFinalizedHash, valueKVs: map[string]rootStorageValue{}}
	if _, err := reader.read(t.Context(), "SubnetOwner", []byte{25, 0}); !errors.Is(err, errRpcIntegrity) || fixture.count("state_getStorage") != 0 {
		t.Fatalf("undeclared profile storage reached RPC: %v", err)
	}
}

// Count limits alone cannot bound thousands of individually oversized page keys.
func TestObservationStorageKeysRejectsOverlongKeyBeforePaging(t *testing.T) {
	client, fixture := newRootFixture(t)
	probe, err := types.CreateStorageKey(fixture.metadata, "SubtensorModule", "Keys", []byte{0, 0}, []byte{0, 0})
	if err != nil {
		t.Fatal(err)
	}
	fixture.storageKVs["0x"+hex.EncodeToString(probe[:34])+strings.Repeat("00", 100)] = "0x00"
	reader := &rootStorageReader{client: client, metadata: fixture.metadata, block: testFinalizedHash}
	if _, err := reader.keys(t.Context(), probe[:34]); !errors.Is(err, errRpcIntegrity) || fixture.count("state_getKeysPaged") != 1 {
		t.Fatalf("oversized individual key escaped the page byte bound: %v", err)
	}
}

// A failed page retries exactly its immutable prefix/start/block; completed
// storage reads and the selected finalized head are not restarted.
func TestSubnetPreviewRetriesTransientPageAtSameFinalizedHash(t *testing.T) {
	client, fixture, policy := newSubnetFixture(t)
	var stateLock sync.Mutex
	var failedParams []json.RawMessage
	attempts := 0
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		var call struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			return nil, err
		}
		first := false
		if call.Method == "state_getKeysPaged" {
			stateLock.Lock()
			first = failedParams == nil
			if first {
				failedParams = call.Params
			}
			if reflect.DeepEqual(failedParams, call.Params) {
				attempts++
			}
			stateLock.Unlock()
		}
		if first {
			return &http.Response{StatusCode: http.StatusGatewayTimeout, Body: io.NopCloser(strings.NewReader("synthetic transient"))}, nil
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		return fixture.roundTrip(request)
	})
	preview, err := client.readSubnetPreview(t.Context(), policy, "policy")
	if err != nil || !preview.CensusComplete || attempts != 2 || fixture.count("chain_getFinalizedHead") != 4 {
		t.Fatalf("page retry changed identity or lost census: attempts=%d %+v %v", attempts, preview, err)
	}
}

// Explicit barriers cancel a worker during an actual seat read; all readers join.
func TestSubnetPreviewCancellationPublishesNoPartialCensus(t *testing.T) {
	client, fixture, policy := newSubnetFixture(t)
	key, err := types.CreateStorageKey(fixture.metadata, "SubtensorModule", "Keys", []byte{25, 0}, []byte{2, 0})
	if err != nil {
		t.Fatal(err)
	}
	entered, returned := make(chan struct{}), make(chan struct{})
	var once sync.Once
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		var call struct {
			Method string   `json:"method"`
			Params []string `json:"params"`
		}
		if json.Unmarshal(raw, &call) == nil && call.Method == "state_getStorage" && call.Params[0] == key.Hex() {
			once.Do(func() { close(entered) })
			<-request.Context().Done()
			close(returned)
			return nil, request.Context().Err()
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		return fixture.roundTrip(request)
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		preview, err := client.readSubnetPreview(ctx, policy, "policy")
		if preview.CensusComplete || len(preview.Seats) != 0 {
			finished <- errors.New("partial canceled census escaped")
			return
		}
		finished <- err
	}()
	select {
	case <-entered:
	case err := <-finished:
		t.Fatalf("census did not reach the controlled worker: %v", err)
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("worker cancellation lost: %v", err)
	}
	<-returned
}

// CLI output hashes the actual preview and explicitly refuses mutation flags.
func TestSubnetPreviewCommandExportsOnlyReadOnlyEvidence(t *testing.T) {
	_, fixture, policy := newSubnetFixture(t)
	server := rootFixtureServer(t, fixture)
	path := filepath.Join(t.TempDir(), "synthetic-subnet-policy.json")
	raw, err := json.Marshal(policy)
	if err != nil || os.WriteFile(path, raw, 0600) != nil {
		t.Fatal("write synthetic policy:", err)
	}
	var stdout, stderr bytes.Buffer
	code := runMain(t.Context(), []string{"subnet-preview", "--rpc", server.URL, "--policy", path}, &stdout, &stderr)
	var envelope subnetPreviewEnvelope
	if code != 0 || json.Unmarshal(stdout.Bytes(), &envelope) != nil || !envelope.Observation.CensusComplete || envelope.Observation.ResetReady || !strings.HasPrefix(envelope.ContentHash, "sha256:") {
		t.Fatalf("subnet CLI did not publish bounded evidence: %d %s %s", code, stdout.String(), stderr.String())
	}
	priorReads := fixture.count("system_chain")
	for _, badFlag := range [][]string{{"--apply"}, {"--retry-window", "1s"}, {"--retry-window", "16m"}} {
		stdout.Reset()
		stderr.Reset()
		args := append([]string{"subnet-preview", "--rpc", server.URL, "--policy", path}, badFlag...)
		if code := runMain(t.Context(), args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || fixture.count("system_chain") != priorReads {
			t.Fatalf("unsupported command reached RPC: %d %s", code, stderr.String())
		}
	}
}
