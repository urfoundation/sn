//go:build linux || darwin

package validator

// Real runtime metadata pins the take calls' wire layout and the storage
// layout behind the dry run. Every block, key and stored value is synthetic;
// the fixture derives storage keys by hand, so a wrong metadata-driven key
// reads a runtime default and changes the printed values.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/centrifuge/go-substrate-rpc-client/v4/xxhash"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"golang.org/x/crypto/blake2b"

	snchain "github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
)

const (
	takeTestRuntime461Metadata       = "../miner/testdata/runtime461-metadata.scale.gz.base64"
	takeTestRuntime461MetadataHash   = "0x98b2cfd0d6633488dfe5b3b70b869d5753aa3c42396533013df131e4e0e5ca68"
	takeTestRuntime470Projection     = "../mainnet/testdata/runtime470-subnet-codec.scale.gz.base64"
	takeTestRuntime470ProjectionHash = "0xcdb975f33cf23ba0df2279208feeacdcf0e629f4cd0d0b3e972ee63d1ebdc0a4"
	takeTestRuntime475Metadata       = "../crv4/testdata/runtime475-metadata.scale.gz.base64"
	takeTestRuntime475MetadataHash   = "0x983cfdabc62b0c6b08faafb47f24303999236e0022598d1b5a3ec70fbde895ff"
)

func TestTakePercentRoundsDownToPartsOf65535(t *testing.T) {
	for value, want := range map[string]uint16{
		"18": 11796, "18.0": 11796, "18.00": 11796, "018": 11796,
		"0": 0, "0.00": 0, "0.01": 6, "1": 655, "12.5": 8191, "17.99": 11789, "100": 65535, "100.00": 65535,
	} {
		if got, err := parseTakePercent(value); err != nil || got != want {
			t.Errorf("--take=%s = %d, %v; want %d", value, got, err, want)
		}
	}
	// The printed percent recovers every two-decimal input.
	for hundredths := uint64(0); hundredths <= 100*100; hundredths++ {
		percent := fmt.Sprintf("%d.%02d", hundredths/100, hundredths%100)
		parts, err := parseTakePercent(percent)
		if err != nil || parts != uint16(hundredths*65535/10000) || !strings.HasSuffix(formatTake(parts), "("+percent+"%)") {
			t.Fatalf("--take=%s = %s, %v", percent, formatTake(parts), err)
		}
	}
}

func TestTakePercentRefusesMalformedOrOutOfRangeValues(t *testing.T) {
	for _, value := range []string{"", "-1", "+1", "1e1", " 18", "18 ", "18%", "18.", ".5", "18.123", "100.01", "101", "1000", "0x10", "1,5", "eighteen"} {
		if parts, err := parseTakePercent(value); err == nil {
			t.Errorf("--take=%q was accepted as %d", value, parts)
		}
	}
}

func TestTakeNetuidNamesASubnet(t *testing.T) {
	if netuid, err := parseTakeNetuid("25"); err != nil || netuid != 25 {
		t.Fatalf("--netuid=25 = %d, %v", netuid, err)
	}
	for _, value := range []string{"", "0", "-1", "65536", "25x"} {
		if netuid, err := parseTakeNetuid(value); err == nil {
			t.Errorf("--netuid=%q was accepted as %d", value, netuid)
		}
	}
}

func TestTakeOwnerDecisionRequiresTheSigningColdkey(t *testing.T) {
	coldkey := [32]byte{1}
	if err := takeOwnerDecision(coldkey, coldkey); err != nil {
		t.Fatal(err)
	}
	if err := takeOwnerDecision([32]byte{}, coldkey); err == nil || !strings.Contains(err.Error(), "no owning coldkey") {
		t.Fatalf("unowned hotkey: %v", err)
	}
	if err := takeOwnerDecision([32]byte{2}, coldkey); err == nil || !strings.Contains(err.Error(), "not the signing coldkey") {
		t.Fatalf("foreign hotkey: %v", err)
	}
}

func TestDelegateTakeDecisionFollowsTheRuntimeRules(t *testing.T) {
	stored := func(take uint16, lastChange uint64) delegateTakeState {
		return delegateTakeState{Take: take, Stored: true, Minimum: 655, Maximum: 11796, RateLimit: 216000, LastChange: lastChange}
	}
	unstored := delegateTakeState{Take: 11796, Minimum: 655, Maximum: 11796, RateLimit: 216000}
	for _, testCase := range []struct {
		name      string
		state     delegateTakeState
		target    uint16
		inclusion uint64
		want      string
		wantErr   string
	}{
		{name: "stored at the target", state: stored(11796, 5), target: 11796, inclusion: 10},
		{name: "the runtime default is pinned by a decrease", state: unstored, target: 11796, inclusion: 10, want: "decrease_take"},
		{name: "a decrease ignores the rate limit", state: stored(11796, 9), target: 9000, inclusion: 10, want: "decrease_take"},
		{name: "a decrease to the minimum", state: stored(11796, 0), target: 655, inclusion: 10, want: "decrease_take"},
		{name: "a first increase", state: stored(9000, 0), target: 11796, inclusion: 10, want: "increase_take"},
		{name: "an increase at the limit waits", state: stored(9000, 1000), target: 11796, inclusion: 217000, wantErr: "the earliest is block 217001"},
		{name: "an increase after the limit", state: stored(9000, 1000), target: 11796, inclusion: 217001, want: "increase_take"},
		{name: "above the maximum", state: stored(9000, 0), target: 11797, inclusion: 10, wantErr: "above the chain maximum"},
		{name: "above the maximum when already stored there", state: stored(11797, 0), target: 11797, inclusion: 10, wantErr: "above the chain maximum"},
		{name: "below the minimum", state: stored(11796, 0), target: 654, inclusion: 10, wantErr: "below the chain minimum"},
	} {
		got, err := delegateTakeDecision(testCase.state, testCase.target, testCase.inclusion)
		if testCase.wantErr == "" {
			if err != nil || got != testCase.want {
				t.Errorf("%s: %q, %v; want %q", testCase.name, got, err, testCase.want)
			}
		} else if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
			t.Errorf("%s: %q, %v; want an error with %q", testCase.name, got, err, testCase.wantErr)
		}
	}
}

func TestChildkeyTakeDecisionFollowsTheRuntimeRules(t *testing.T) {
	state := func(take uint16, stored bool, subnetMinimum uint16, lastChange uint64) childkeyTakeState {
		return childkeyTakeState{Netuid: 25, SubnetExists: true, Take: take, Stored: stored, SubnetMinimum: subnetMinimum, Maximum: 11796, RateLimit: 216000, LastChange: lastChange}
	}
	for _, testCase := range []struct {
		name      string
		state     childkeyTakeState
		target    uint16
		inclusion uint64
		want      string
		wantErr   string
	}{
		{name: "stored at the target", state: state(11796, true, 0, 5), target: 11796, inclusion: 10},
		{name: "a first take", state: state(0, false, 0, 0), target: 11796, inclusion: 10, want: "set_childkey_take"},
		{name: "the effective minimum is pinned without the rate limit", state: state(0, false, 1000, 9), target: 1000, inclusion: 10, want: "set_childkey_take"},
		{name: "a decrease ignores the rate limit", state: state(11796, true, 0, 9), target: 9000, inclusion: 10, want: "set_childkey_take"},
		{name: "an increase inside the limit waits", state: state(0, true, 0, 500), target: 11796, inclusion: 216499, wantErr: "the earliest is block 216500"},
		{name: "an increase at the limit", state: state(0, true, 0, 500), target: 11796, inclusion: 216500, want: "set_childkey_take"},
		{name: "above the maximum", state: state(0, false, 0, 0), target: 11797, inclusion: 10, wantErr: "above the chain maximum"},
		{name: "below the subnet minimum", state: state(0, false, 1000, 0), target: 999, inclusion: 10, wantErr: "below the chain minimum"},
		{name: "a missing subnet", state: childkeyTakeState{Netuid: 25, Maximum: 11796}, target: 11796, inclusion: 10, wantErr: "netuid 25 does not exist"},
	} {
		got, err := childkeyTakeDecision(testCase.state, testCase.target, testCase.inclusion)
		if testCase.wantErr == "" {
			if err != nil || got != testCase.want {
				t.Errorf("%s: %q, %v; want %q", testCase.name, got, err, testCase.want)
			}
		} else if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
			t.Errorf("%s: %q, %v; want an error with %q", testCase.name, got, err, testCase.wantErr)
		}
	}
}

// Pallet 7 with calls 65, 66 and 75 is the reviewed layout from runtime 455
// through the runtime 470 projection and the exact runtime 475 metadata.
func TestTakeCallsMatchReviewedRuntimeLayouts(t *testing.T) {
	hotkey := [32]byte{7, 7, 7}
	for _, fixture := range []struct{ path, hash string }{
		{path: takeTestRuntime461Metadata, hash: takeTestRuntime461MetadataHash},
		{path: takeTestRuntime470Projection, hash: takeTestRuntime470ProjectionHash},
		{path: takeTestRuntime475Metadata, hash: takeTestRuntime475MetadataHash},
	} {
		metadata, _ := provisionalValidatorMetadataTest(t, fixture.path, fixture.hash)
		for _, testCase := range []struct {
			name   string
			netuid uint16
			want   []byte
		}{
			{name: "decrease_take", want: slices.Concat([]byte{7, 65}, hotkey[:], []byte{0x14, 0x2e})},
			{name: "increase_take", want: slices.Concat([]byte{7, 66}, hotkey[:], []byte{0x14, 0x2e})},
			{name: "set_childkey_take", netuid: 25, want: slices.Concat([]byte{7, 75}, hotkey[:], []byte{25, 0}, []byte{0x14, 0x2e})},
		} {
			call, err := takeCall(metadata, testCase.name, hotkey, testCase.netuid, 11796)
			if err != nil {
				t.Fatalf("%s %s: %v", fixture.path, testCase.name, err)
			}
			encoded, err := codec.Encode(call)
			if err != nil || !bytes.Equal(encoded, testCase.want) {
				t.Fatalf("%s %s encoding differs:\n got %x\nwant %x (%v)", fixture.path, testCase.name, encoded, testCase.want, err)
			}
		}
		for _, testCase := range []struct {
			name   string
			hotkey [32]byte
			netuid uint16
		}{
			{name: "decrease_take", hotkey: hotkey, netuid: 25},
			{name: "set_childkey_take", hotkey: hotkey},
			{name: "add_stake", hotkey: hotkey, netuid: 25},
			{name: "increase_take"},
		} {
			if _, err := takeCall(metadata, testCase.name, testCase.hotkey, testCase.netuid, 11796); err == nil {
				t.Errorf("%s accepted %s for netuid %d and hotkey %x", fixture.path, testCase.name, testCase.netuid, testCase.hotkey)
			}
		}
	}
}

// The delegate-take rate key keeps its variant when 475 appends hyperparameters.
func TestTakeDelegateRateKeyMatchesReviewedRuntimes(t *testing.T) {
	hotkey := [32]byte{7, 7, 7}
	want := append([]byte{5}, hotkey[:]...)
	for _, fixture := range []struct{ path, hash string }{
		{path: takeTestRuntime461Metadata, hash: takeTestRuntime461MetadataHash},
		{path: takeTestRuntime475Metadata, hash: takeTestRuntime475MetadataHash},
	} {
		metadata, _ := provisionalValidatorMetadataTest(t, fixture.path, fixture.hash)
		key, err := delegateTakeRateKey(metadata, hotkey)
		if err != nil || !bytes.Equal(key, want) {
			t.Fatalf("%s delegate take rate key %x: %v", fixture.path, key, err)
		}
	}
}

func TestTakeCallRefusesARuntimeWithChangedArguments(t *testing.T) {
	metadata, _ := provisionalValidatorMetadataTest(t, takeTestRuntime461Metadata, takeTestRuntime461MetadataHash)
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != crv4.PalletName {
			continue
		}
		calls := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
		for i, variant := range calls.Def.Variant.Variants {
			if variant.Name == "decrease_take" {
				calls.Def.Variant.Variants[i].Fields = variant.Fields[:1]
			}
		}
	}
	if _, err := takeCall(metadata, "decrease_take", [32]byte{1}, 0, 11796); err == nil || !strings.Contains(err.Error(), "not the reviewed (hotkey:[u8;32], take:u16)") {
		t.Fatalf("changed decrease_take was signed: %v", err)
	}
	if _, err := takeCall(metadata, "increase_take", [32]byte{1}, 0, 11796); err != nil {
		t.Fatalf("unchanged increase_take was refused: %v", err)
	}
}

// Answers state_getStorage at one authenticated block from hand-derived keys,
// plus the nonce and fee quote a dry run needs. Any other method or block fails
// the read; broadcasting is not served.
type takeTestFixture struct {
	block   types.Hash
	storage map[string]string
	methods []string
	chain   *crv4.Chain
	runtime snchain.FinalizedRuntime
	hotkey  [32]byte
	coldkey *crv4.Keypair
}

func newTakeTestFixture(t *testing.T) *takeTestFixture {
	t.Helper()
	metadata, _ := provisionalValidatorMetadataTest(t, takeTestRuntime461Metadata, takeTestRuntime461MetadataHash)
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x48, 0x01})
	if err != nil {
		t.Fatal(err)
	}
	coldkey, err := crv4.KeypairFromSeed([32]byte{0x43, 0x02})
	if err != nil {
		t.Fatal(err)
	}
	version := crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 461, TransactionVersion: 1, StateVersion: 1}
	fixture := &takeTestFixture{block: types.Hash{0xb1, 0x0c}, storage: map[string]string{}, hotkey: hotkey.PublicKey(), coldkey: coldkey}
	fixture.chain = &crv4.Chain{
		API:  &gsrpc.SubstrateAPI{Client: &validatorRuntimeIdentityTestClient{callContext: fixture.call}},
		Meta: metadata, GenesisHash: types.Hash{0x9e, 0x57},
		Runtime: &types.RuntimeVersion{SpecName: version.SpecName, SpecVersion: types.U32(version.SpecVersion), TransactionVersion: types.U32(version.TransactionVersion)},
	}
	fixture.runtime = snchain.FinalizedRuntime{Hash: fixture.block, Number: 300_000, Artifact: crv4.AuthenticatedRuntimeArtifact{Version: version}}
	coldkeyAccount := coldkey.PublicKey()
	fixture.set("Owner", coldkeyAccount[:], takeTestBlake2Concat(fixture.hotkey[:]))
	return fixture
}

func (self *takeTestFixture) call(_ context.Context, result any, method string, args ...any) error {
	self.methods = append(self.methods, method)
	switch method {
	case "state_getStorage":
		if len(args) != 2 || args[1] != self.block.Hex() {
			return fmt.Errorf("storage read outside the authenticated block: %v", args)
		}
		target := result.(**string)
		*target = nil
		if value, ok := self.storage[args[0].(string)]; ok {
			*target = &value
		}
		return nil
	case "system_accountNextIndex":
		*result.(*uint32) = 7
		return nil
	case "payment_queryInfo":
		result.(*snchain.NativeTransactionFeeResponse).PartialFee = json.RawMessage(`"2131733"`)
		return nil
	}
	return fmt.Errorf("unexpected take test RPC %s", method)
}

// Stores value under twox128(pallet) ++ twox128(storage) ++ the hashed key parts.
func (self *takeTestFixture) set(storage string, value []byte, keyParts ...[]byte) {
	key := slices.Concat(xxhash.New128([]byte(crv4.PalletName)).Sum(nil), xxhash.New128([]byte(storage)).Sum(nil))
	for _, part := range keyParts {
		key = append(key, part...)
	}
	self.storage[hexutil.Encode(key)] = hexutil.Encode(value)
}

func (self *takeTestFixture) signed() bool {
	return slices.Contains(self.methods, "system_accountNextIndex") || slices.Contains(self.methods, "payment_queryInfo")
}

func (self *takeTestFixture) request(netuid uint16, take uint16, output *bytes.Buffer) takeRequest {
	return takeRequest{Command: "validator take", Netuid: netuid, Hotkey: self.hotkey, Coldkey: self.coldkey, Take: take, FeeLimitRao: defaultNativeFeeLimitRao, Output: output}
}

func takeTestBlake2Concat(value []byte) []byte {
	hash, err := blake2b.New(16, nil)
	if err != nil {
		panic(err)
	}
	hash.Write(value)
	return append(hash.Sum(nil), value...)
}

func takeTestU16(value uint16) []byte { return binary.LittleEndian.AppendUint16(nil, value) }

func takeTestU64(value uint64) []byte { return binary.LittleEndian.AppendUint64(nil, value) }

func requireTakeOutput(t *testing.T, output string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%s", want, output)
		}
	}
}

func TestTakeSetDryRunReadsLiveValuesAndPrintsTheSignedCall(t *testing.T) {
	fixture := newTakeTestFixture(t)
	fixture.set("Delegates", takeTestU16(9000), takeTestBlake2Concat(fixture.hotkey[:]))
	fixture.set("MinDelegateTake", takeTestU16(655))
	fixture.set("MaxDelegateTake", takeTestU16(11796))
	fixture.set("TxDelegateTakeRateLimit", takeTestU64(216000))
	// RateLimitKey::LastTxBlockDelegateTake is variant 5 in the reviewed runtimes.
	fixture.set("LastRateLimitedBlock", takeTestU64(50_000), []byte{5}, fixture.hotkey[:])
	var output bytes.Buffer
	if err := changeTakeAt(context.Background(), fixture.chain, fixture.runtime, fixture.request(0, 11796, &output)); err != nil {
		t.Fatalf("%v\n%s", err, output.String())
	}
	requireTakeOutput(t, output.String(),
		"runtime: node-subtensor/461/1/1 at finalized block 300000 ("+fixture.block.Hex()+")",
		"hotkey: "+takeSs58(fixture.hotkey)+" ("+snchain.Hex32(fixture.hotkey)+")",
		"coldkey: "+fixture.coldkey.Address()+" (hotkey owner "+fixture.coldkey.Address()+")",
		"delegate take: 9000/65535 (13.73%) stored; chain minimum 655/65535 (1.00%), maximum 11796/65535 (18.00%)",
		"delegate take increases: one per 216000 blocks; last change at block 50000, next increase from block 266001",
		"target delegate take: 11796/65535 (18.00%)",
		"note: after this change, raising the delegate take waits 216000 blocks",
		"call: SubtensorModule.increase_take to 11796/65535 (18.00%), call data "+hexutil.Encode(slices.Concat([]byte{7, 66}, fixture.hotkey[:], []byte{0x14, 0x2e})),
		"extrinsic: 0x",
		"(signer "+fixture.coldkey.Address()+", nonce 7, ",
		"fee: estimated 0.002131733 TAO (2131733 rao), limit 10000000 rao",
		"dry run: nothing was broadcast",
	)
}

func TestTakeChildkeyDryRunPinsTheTakeOnTheSubnet(t *testing.T) {
	fixture := newTakeTestFixture(t)
	netuid := takeTestU16(25)
	fixture.set("NetworksAdded", []byte{1}, netuid)
	fixture.set("MinChildkeyTakePerSubnet", takeTestU16(655), netuid)
	fixture.set("MaxChildkeyTake", takeTestU16(11796))
	fixture.set("TransactionKeyLastBlock", takeTestU64(100), takeTestBlake2Concat(fixture.hotkey[:]), netuid, takeTestU16(1))
	var output bytes.Buffer
	if err := changeTakeAt(context.Background(), fixture.chain, fixture.runtime, fixture.request(25, 11796, &output)); err != nil {
		t.Fatalf("%v\n%s", err, output.String())
	}
	requireTakeOutput(t, output.String(),
		"childkey take (netuid 25): 0/65535 (0.00%) runtime default, none stored, effective 655/65535 (1.00%); chain minimum 655/65535 (1.00%) (global 0/65535 (0.00%), subnet 655/65535 (1.00%)), maximum 11796/65535 (18.00%)",
		"childkey take (netuid 25) increases: one per 216000 blocks; last change at block 100, next increase from block 216100",
		"target childkey take (netuid 25): 11796/65535 (18.00%)",
		"call: SubtensorModule.set_childkey_take to 11796/65535 (18.00%), call data "+hexutil.Encode(slices.Concat([]byte{7, 75}, fixture.hotkey[:], netuid, []byte{0x14, 0x2e})),
		"dry run: nothing was broadcast",
	)
}

func TestTakeChildkeyIncreaseInsideTheRateLimitStopsBeforeSigning(t *testing.T) {
	fixture := newTakeTestFixture(t)
	netuid := takeTestU16(25)
	fixture.set("NetworksAdded", []byte{1}, netuid)
	fixture.set("ChildkeyTake", takeTestU16(9000), takeTestBlake2Concat(fixture.hotkey[:]), netuid)
	fixture.set("TransactionKeyLastBlock", takeTestU64(200_000), takeTestBlake2Concat(fixture.hotkey[:]), netuid, takeTestU16(1))
	var output bytes.Buffer
	err := changeTakeAt(context.Background(), fixture.chain, fixture.runtime, fixture.request(25, 11796, &output))
	if err == nil || !strings.Contains(err.Error(), "wait 216000 blocks after the last change at block 200000; the earliest is block 416000") {
		t.Fatalf("rate-limited increase: %v\n%s", err, output.String())
	}
	if fixture.signed() {
		t.Fatalf("a refused take quoted or signed an extrinsic: %v", fixture.methods)
	}
}

func TestTakeSetAtTheStoredTargetSubmitsNothing(t *testing.T) {
	fixture := newTakeTestFixture(t)
	fixture.set("Delegates", takeTestU16(11796), takeTestBlake2Concat(fixture.hotkey[:]))
	var output bytes.Buffer
	if err := changeTakeAt(context.Background(), fixture.chain, fixture.runtime, fixture.request(0, 11796, &output)); err != nil {
		t.Fatal(err)
	}
	requireTakeOutput(t, output.String(), "delegate take: 11796/65535 (18.00%) stored;", "delegate take is already stored at the target; nothing to submit")
	if fixture.signed() {
		t.Fatalf("an unchanged take quoted or signed an extrinsic: %v", fixture.methods)
	}
}

func TestTakeSetRefusesAColdkeyThatDoesNotOwnTheHotkey(t *testing.T) {
	fixture := newTakeTestFixture(t)
	other, err := crv4.KeypairFromSeed([32]byte{0x43, 0x03})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	request := fixture.request(0, 11796, &output)
	request.Coldkey = other
	if err := changeTakeAt(context.Background(), fixture.chain, fixture.runtime, request); err == nil || !strings.Contains(err.Error(), "is owned by coldkey "+fixture.coldkey.Address()) {
		t.Fatalf("foreign coldkey: %v", err)
	}
	if fixture.signed() {
		t.Fatalf("a foreign coldkey quoted or signed an extrinsic: %v", fixture.methods)
	}
}

func TestTakeVerificationRequiresTheStoredTargetAtInclusion(t *testing.T) {
	fixture := newTakeTestFixture(t)
	fixture.set("Delegates", takeTestU16(11796), takeTestBlake2Concat(fixture.hotkey[:]))
	receipt := crv4.FinalizedExtrinsic{BlockHash: fixture.block, BlockNumber: 300_002}
	var output bytes.Buffer
	if err := verifyStoredTake(context.Background(), fixture.chain, receipt, fixture.request(0, 11796, &output), "delegate take"); err != nil {
		t.Fatal(err)
	}
	requireTakeOutput(t, output.String(), "delegate take: 11796/65535 (18.00%) at finalized block 300002")
	// The ChildkeyTake default is not a stored change.
	err := verifyStoredTake(context.Background(), fixture.chain, receipt, fixture.request(25, 0, &output), "childkey take (netuid 25)")
	if err == nil || !strings.Contains(err.Error(), "stored false") {
		t.Fatalf("unstored childkey take verified: %v", err)
	}
}

func TestTakeStatusPrintsDelegateAndChildkeyTakesPerNetuid(t *testing.T) {
	fixture := newTakeTestFixture(t)
	fixture.set("Uids", takeTestU16(12), takeTestU16(0), takeTestBlake2Concat(fixture.hotkey[:]))
	fixture.set("Uids", takeTestU16(3), takeTestU16(25), takeTestBlake2Concat(fixture.hotkey[:]))
	fixture.set("NetworksAdded", []byte{1}, takeTestU16(25))
	fixture.set("ChildkeyTake", takeTestU16(11796), takeTestBlake2Concat(fixture.hotkey[:]), takeTestU16(25))
	fixture.set("AutoParentDelegationEnabled", []byte{0}, takeTestBlake2Concat(fixture.hotkey[:]))
	child := [32]byte{0x4f, 1}
	fixture.set("ChildKeys", slices.Concat([]byte{1 << 2}, takeTestU64(math.MaxUint64), child[:]), takeTestBlake2Concat(fixture.hotkey[:]), takeTestU16(25))
	parents := []byte{2 << 2}
	for _, parent := range [][32]byte{{0x50, 1}, {0x50, 2}} {
		parents = slices.Concat(parents, takeTestU64(1<<63), parent[:])
	}
	fixture.set("ParentKeys", parents, takeTestBlake2Concat(fixture.hotkey[:]), takeTestU16(25))
	var output bytes.Buffer
	if err := printTakeStatus(context.Background(), fixture.chain, fixture.runtime, fixture.hotkey, []uint16{25, 7}, &output); err != nil {
		t.Fatalf("%v\n%s", err, output.String())
	}
	// Unset values are the reviewed runtime's declared defaults.
	requireTakeOutput(t, output.String(),
		"owner coldkey: "+fixture.coldkey.Address(),
		"registration (netuid 0): uid 12",
		"registration (netuid 25): uid 3",
		"registration (netuid 7): not registered",
		"delegate take: 11796/65535 (18.00%) runtime default, none stored; chain minimum 0/65535 (0.00%), maximum 11796/65535 (18.00%)",
		"delegate take increases: one per 216000 blocks; no change recorded",
		"auto parent delegation: false (stored)",
		"childkey take (netuid 25): 11796/65535 (18.00%) stored; chain minimum 0/65535 (0.00%) (global 0/65535 (0.00%), subnet 0/65535 (0.00%)), maximum 11796/65535 (18.00%)",
		"childkey take (netuid 25) increases: one per 216000 blocks; no change recorded",
		"children (netuid 25): "+takeSs58(child)+" 100.00%",
		"parents (netuid 25): 2 hotkeys name this hotkey as a child",
		"childkey take (netuid 7): the subnet does not exist",
	)
	if strings.Contains(output.String(), "(netuid 7): none") || strings.Contains(output.String(), "parents (netuid 7)") {
		t.Fatalf("relations were read for a missing subnet:\n%s", output.String())
	}
}

func TestTakeUsageForms(t *testing.T) {
	durable := []string{"--durable-volumes=/etc/ur-validator/volumes.yml", "--durable-volumes-sha256=" + strings.Repeat("ab", 32)}
	for _, testCase := range []struct {
		name    string
		args    []string
		command string
	}{
		{name: "status", args: []string{"take", "status", "--config=r.yml"}, command: "status"},
		{name: "status with a netuid", args: []string{"take", "status", "--config=r.yml", "--netuid=7"}, command: "status"},
		{name: "set dry run", args: []string{"take", "set", "--take=18", "--config=r.yml", "--coldkey_seed_file=cold.seed"}, command: "set"},
		{name: "set apply", args: slices.Concat([]string{"take", "set", "--take=18", "--config=r.yml", "--coldkey_seed_file=cold.seed", "--fee_limit_rao=5000000", "--apply"}, durable), command: "set"},
		{name: "childkey", args: slices.Concat([]string{"take", "childkey", "--netuid=25", "--take=18", "--config=r.yml", "--coldkey_seed_file=cold.seed", "--dry-run"}, durable), command: "childkey"},
	} {
		opts, err := parseValidatorArgsForTest(t, testCase.args)
		if err != nil {
			t.Fatalf("%s: %v", testCase.name, err)
		}
		// take status also sets status, so the dispatch tests take first.
		if !optBool(opts, "take") || !optBool(opts, testCase.command) {
			t.Fatalf("%s: take %s not selected: %v", testCase.name, testCase.command, opts)
		}
	}
	opts, err := parseValidatorArgsForTest(t, []string{"take", "set", "--take=18", "--config=r.yml", "--coldkey_seed_file=cold.seed"})
	if err != nil {
		t.Fatal(err)
	}
	if optString(opts, "--take", "") != "18" || optUint64(opts, "--fee_limit_rao", 0) != defaultNativeFeeLimitRao || optBool(opts, "--apply") || optBool(opts, "childkey") {
		t.Fatalf("take set defaults = %v", opts)
	}
	opts, err = parseValidatorArgsForTest(t, []string{"take", "childkey", "--netuid=25", "--take=18.5", "--config=r.yml", "--coldkey_seed_file=cold.seed"})
	if err != nil {
		t.Fatal(err)
	}
	if optString(opts, "--netuid", "") != "25" || optString(opts, "--take", "") != "18.5" {
		t.Fatalf("take childkey values = %v", opts)
	}
	opts, err = parseValidatorArgsForTest(t, []string{"status", "--config=r.yml"})
	if err != nil {
		t.Fatal(err)
	}
	if !optBool(opts, "status") || optBool(opts, "take") {
		t.Fatalf("validator status selected take: %v", opts)
	}
	for _, args := range [][]string{
		{"take", "--config=r.yml"},
		{"take", "status"},
		{"take", "status", "--config=r.yml", "--take=18"},
		{"take", "set", "--config=r.yml", "--coldkey_seed_file=cold.seed"},
		{"take", "set", "--take=18", "--config=r.yml"},
		{"take", "set", "--take=18", "--config=r.yml", "--coldkey_seed_file=cold.seed", "--apply", "--dry-run"},
		{"take", "childkey", "--take=18", "--config=r.yml", "--coldkey_seed_file=cold.seed"},
	} {
		if _, err := parseValidatorArgsForTest(t, args); err == nil {
			t.Errorf("%v parsed without its required options", args)
		}
	}
}
