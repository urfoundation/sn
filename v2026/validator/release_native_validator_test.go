// Startup controls run the actual CRV4 identity/stake readers against exact
// metadata and block-pinned scripted RPCs, not a fabricated eligibility result.
package validator

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Each fixture owns its transcript, metadata, chain and mutable chain-state
// fields; cancellation tests join the read before examining the transcript.
type releaseNativeValidatorTestFixture struct {
	ctx        context.Context
	chain      *crv4.Chain
	expected   crv4.RuntimeArtifactIdentity
	genesis    types.Hash
	block      types.Hash
	hotkey     [32]byte
	threshold  uint64
	total      uint64
	permit     bool
	owner      bool
	calls      []string
	beforeCall func(context.Context, string) error
}

// The independent SDK supplies exact SCALE compacts for the runtime response.
func releaseNativeValidatorTestCompact(t *testing.T, value uint64) []byte {
	t.Helper()
	encoded, err := codec.Encode(types.NewUCompactFromUInt(value))
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// Uses the actual v454 storage hashers and API field indexes while authorizing
// only this fixture's metadata content hash in the private real-reader path.
func newReleaseNativeValidatorTestFixture(t *testing.T) *releaseNativeValidatorTestFixture {
	t.Helper()
	fixture := &releaseNativeValidatorTestFixture{
		ctx: context.Background(), genesis: types.Hash{1}, block: types.Hash{2},
		hotkey: [32]byte{11}, threshold: 100, total: 150, permit: true,
	}
	identityHasher := types.StorageHasherV10{IsIdentity: true}
	accountHasher := types.StorageHasherV10{IsBlake2_128Concat: true}
	metadata := types.NewMetadataV14()
	metadata.MagicNumber = types.MagicNumber
	metadata.AsMetadataV14.Pallets = []types.PalletMetadataV14{{Name: "SubtensorModule", HasStorage: true, Storage: types.StorageMetadataV14{Prefix: "SubtensorModule"}}}
	for _, field := range []struct {
		name     string
		optional bool
		hashers  []types.StorageHasherV10
	}{
		{name: "SubnetworkN", hashers: []types.StorageHasherV10{identityHasher}},
		{name: "Keys", hashers: []types.StorageHasherV10{identityHasher, identityHasher}},
		{name: "Uids", optional: true, hashers: []types.StorageHasherV10{identityHasher, accountHasher}},
		{name: "Owner", hashers: []types.StorageHasherV10{accountHasher}},
		{name: "TotalHotkeyAlpha", hashers: []types.StorageHasherV10{accountHasher, identityHasher}},
		{name: "ValidatorPermit", hashers: []types.StorageHasherV10{identityHasher}},
		{name: "SubnetOwnerHotkey", hashers: []types.StorageHasherV10{identityHasher}},
		{name: "StakeThreshold"},
	} {
		entry := types.StorageEntryMetadataV14{Name: types.Text(field.name), Modifier: types.StorageFunctionModifierV0{IsOptional: field.optional, IsDefault: !field.optional}}
		if len(field.hashers) == 0 {
			entry.Type = types.StorageEntryTypeV14{IsPlainType: true}
		} else {
			entry.Type = types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Hashers: field.hashers}}
		}
		metadata.AsMetadataV14.Pallets[0].Storage.Items = append(metadata.AsMetadataV14.Pallets[0].Storage.Items, entry)
	}
	metadataHex, err := codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, metadataHash, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	fixture.expected = crv4.RuntimeArtifactIdentity{
		Version:  crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 454, TransactionVersion: 1, StateVersion: 1},
		CodeHash: types.Hash{4}.Hex(), MetadataHash: metadataHash,
	}
	netuid, uid := binary.LittleEndian.AppendUint16(nil, 521), binary.LittleEndian.AppendUint16(nil, 1)
	keyNames := map[string]string{}
	for _, field := range []struct {
		name string
		args [][]byte
	}{
		{name: "SubnetworkN", args: [][]byte{netuid}},
		{name: "Keys", args: [][]byte{netuid, uid}},
		{name: "Uids", args: [][]byte{netuid, fixture.hotkey[:]}},
		{name: "Owner", args: [][]byte{fixture.hotkey[:]}},
		{name: "TotalHotkeyAlpha", args: [][]byte{fixture.hotkey[:], netuid}},
		{name: "ValidatorPermit", args: [][]byte{netuid}},
		{name: "SubnetOwnerHotkey", args: [][]byte{netuid}},
		{name: "StakeThreshold"},
	} {
		key, err := types.CreateStorageKey(metadata, "SubtensorModule", field.name, field.args...)
		if err != nil {
			t.Fatal(err)
		}
		keyNames[key.Hex()] = field.name
	}
	fixture.chain = &crv4.Chain{GenesisHash: fixture.genesis, Meta: types.NewMetadataV14(), Runtime: &types.RuntimeVersion{SpecName: "unrelated-dial-time"}}
	fixture.chain.API = &gsrpc.SubstrateAPI{Client: &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if ctx != fixture.ctx {
			return errors.New("native startup reader changed its caller context")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		fixture.calls = append(fixture.calls, method)
		if fixture.beforeCall != nil {
			if err := fixture.beforeCall(ctx, method); err != nil {
				return err
			}
		}
		check := func(want ...any) error {
			if !reflect.DeepEqual(args, want) {
				return fmt.Errorf("native startup %s args=%v, want=%v", method, args, want)
			}
			return nil
		}
		switch method {
		case "chain_getFinalizedHead":
			if err := check(); err != nil {
				return err
			}
			return setValidatorRuntimeIdentityTestResult(result, fixture.block.Hex())
		case "chain_getHeader":
			if err := check(fixture.block.Hex()); err != nil {
				return err
			}
			header, ok := result.(*types.Header)
			if !ok {
				return fmt.Errorf("unexpected header result %T", result)
			}
			*header = types.Header{Number: 100}
			return nil
		case "chain_getBlockHash":
			if reflect.DeepEqual(args, []any{uint64(0)}) {
				return setValidatorRuntimeIdentityTestResult(result, fixture.genesis.Hex())
			}
			if err := check(uint64(100)); err != nil {
				return err
			}
			return setValidatorRuntimeIdentityTestResult(result, fixture.block.Hex())
		case "state_getRuntimeVersion":
			if err := check(fixture.block.Hex()); err != nil {
				return err
			}
			return setValidatorRuntimeIdentityTestResult(result, fixture.expected.Version)
		case "state_getStorageHash":
			if err := check("0x3a636f6465", fixture.block.Hex()); err != nil {
				return err
			}
			return setValidatorRuntimeIdentityTestResult(result, fixture.expected.CodeHash)
		case "state_getMetadata":
			if err := check(fixture.block.Hex()); err != nil {
				return err
			}
			return setValidatorRuntimeIdentityTestResult(result, metadataHex)
		case "state_getStorage":
			if len(args) != 2 || args[1] != fixture.block.Hex() {
				return errors.New("native startup storage is not exact-block")
			}
			key, ok := args[0].(string)
			if !ok {
				return errors.New("native startup storage key is not a string")
			}
			var value []byte
			switch keyNames[key] {
			case "SubnetworkN":
				value = []byte{3, 0}
			case "Keys":
				value = fixture.hotkey[:]
			case "Uids":
				value = uid
			case "Owner":
				value = make([]byte, 32)
				value[0] = 12
			case "TotalHotkeyAlpha":
				value = binary.LittleEndian.AppendUint64(nil, 9_000_000_000_000_000)
			case "ValidatorPermit":
				value = []byte{12, 0, 0, 0}
				if fixture.permit {
					value[2] = 1
				}
			case "StakeThreshold":
				value = binary.LittleEndian.AppendUint64(nil, fixture.threshold)
			case "SubnetOwnerHotkey":
				if !fixture.owner {
					target, ok := result.(*json.RawMessage)
					if !ok {
						return errors.New("unexpected owner result")
					}
					*target = json.RawMessage("null")
					return nil
				}
				value = fixture.hotkey[:]
			default:
				return fmt.Errorf("unexpected native startup storage key %s", key)
			}
			return setValidatorRuntimeIdentityTestResult(result, "0x"+hex.EncodeToString(value))
		case "state_call":
			if err := check("SubnetInfoRuntimeApi_get_selective_metagraph", "0x09021400001e00340039004500", fixture.block.Hex()); err != nil {
				return err
			}
			data := append([]byte{1}, releaseNativeValidatorTestCompact(t, 521)...)
			for index := 1; index <= 76; index++ {
				if index != 30 && index != 52 && index != 57 && index != 69 {
					data = append(data, 0)
					continue
				}
				data = append(data, 1, 12)
				switch index {
				case 52:
					for _, hotkey := range [][32]byte{{31}, fixture.hotkey, {33}} {
						data = append(data, hotkey[:]...)
					}
				case 57:
					permit := byte(0)
					if fixture.permit {
						permit = 1
					}
					data = append(data, 0, permit, 0)
				case 69:
					data = append(data, 0)
					data = append(data, releaseNativeValidatorTestCompact(t, fixture.total)...)
					data = append(data, 0)
				}
			}
			return setValidatorRuntimeIdentityTestResult(result, "0x"+hex.EncodeToString(data))
		default:
			return fmt.Errorf("unexpected native startup RPC %s", method)
		}
	}}}
	return fixture
}

// The actual production helper receives independent expected inputs, not a
// callback that can approve or replace the native reader's observation.
func (self *releaseNativeValidatorTestFixture) read() (crv4.ValidatorStakeObservation, error) {
	return readReleaseNativeValidatorAtContext(self.ctx, self.chain, self.genesis, 521, self.hotkey, 1, self.expected)
}

// A complete exact-block observation permits the real startup prerequisite.
func TestAuthenticatePinnedNativeRuntimeValidatorStakeAccepts(t *testing.T) {
	t.Parallel()
	fixture := newReleaseNativeValidatorTestFixture(t)
	metadata, runtime := fixture.chain.Meta, fixture.chain.Runtime
	observed, err := fixture.read()
	if err != nil || observed.TotalStakeRao != 150 || observed.Identity.UID != 1 || observed.Identity.BlockHash != fixture.block || observed.Identity.Hotkey != fixture.hotkey {
		t.Fatalf("native startup observation differs: %+v, %v", observed, err)
	}
	if fixture.chain.Meta != metadata || fixture.chain.Runtime != runtime {
		t.Fatal("native startup changed signing metadata")
	}
}

// Eligibility decisions use the calculated stake, not the much larger raw
// alpha balance, and failed admission returns no partial observation.
func TestAuthenticatePinnedNativeRuntimeValidatorStakeRejectsInsufficient(t *testing.T) {
	t.Parallel()
	fixture := newReleaseNativeValidatorTestFixture(t)
	fixture.threshold = 151
	observed, err := fixture.read()
	if err == nil || observed != (crv4.ValidatorStakeObservation{}) {
		t.Fatalf("insufficient stake escaped startup: %+v, %v", observed, err)
	}
}

// The native integer threshold is inclusive, not the API list's strict >.
func TestAuthenticatePinnedNativeRuntimeValidatorStakeAcceptsEquality(t *testing.T) {
	t.Parallel()
	fixture := newReleaseNativeValidatorTestFixture(t)
	fixture.threshold = 150
	observed, err := fixture.read()
	if err != nil || !observed.MeetsNonSelfStakeAndPermit() {
		t.Fatalf("equal threshold refused: %+v, %v", observed, err)
	}
}

// A non-owner without a permit may not start a weight-writing validator.
func TestAuthenticatePinnedNativeRuntimeValidatorStakeRejectsNoPermit(t *testing.T) {
	t.Parallel()
	fixture := newReleaseNativeValidatorTestFixture(t)
	fixture.permit = false
	observed, err := fixture.read()
	if err == nil || observed != (crv4.ValidatorStakeObservation{}) {
		t.Fatalf("unpermitted startup escaped: %+v, %v", observed, err)
	}
}

// The real registered subnet-owner exception admits both absent prerequisites.
func TestAuthenticatePinnedNativeRuntimeValidatorStakeAcceptsOwner(t *testing.T) {
	t.Parallel()
	fixture := newReleaseNativeValidatorTestFixture(t)
	fixture.owner, fixture.permit, fixture.total, fixture.threshold = true, false, 0, math.MaxUint64
	observed, err := fixture.read()
	if err != nil || !observed.SubnetOwnerRegistered || !observed.MeetsNonSelfStakeAndPermit() {
		t.Fatalf("native owner refused: %+v, %v", observed, err)
	}
}

// Passing EVM registration alone cannot substitute another native hotkey at
// the same numeric UID; the real signer must match the finalized native read.
func TestAuthenticatePinnedNativeRuntimeValidatorStakeRejectsDifferentSigner(t *testing.T) {
	t.Parallel()
	fixture := newReleaseNativeValidatorTestFixture(t)
	observed, err := readReleaseNativeValidatorAtContext(fixture.ctx, fixture.chain, fixture.genesis, 521, [32]byte{99}, 1, fixture.expected)
	if err == nil || observed != (crv4.ValidatorStakeObservation{}) {
		t.Fatalf("wrong native signer escaped: %+v, %v", observed, err)
	}
}

// A real in-flight runtime RPC is interrupted and joined through the caller's
// context, using an explicit barrier instead of a sleep or negative timeout.
func TestAuthenticatePinnedNativeRuntimeValidatorStakeCancels(t *testing.T) {
	t.Parallel()
	fixture := newReleaseNativeValidatorTestFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.ctx = ctx
	entered := make(chan struct{})
	fixture.beforeCall = func(ctx context.Context, method string) error {
		if method == "state_call" {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	type result struct {
		observed crv4.ValidatorStakeObservation
		err      error
	}
	done := make(chan result, 1)
	go func() { observed, err := fixture.read(); done <- result{observed: observed, err: err} }()
	<-entered
	cancel()
	completed := <-done
	if !errors.Is(completed.err, context.Canceled) || completed.observed != (crv4.ValidatorStakeObservation{}) {
		t.Fatalf("canceled startup escaped: %+v, %v", completed.observed, completed.err)
	}
}

// Invalid independent inputs refuse before any external read, including the
// native census's unrepresentable final UID and a changed genesis pin.
func TestAuthenticatePinnedNativeRuntimeValidatorStakeRejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"nil context", "nil chain", "genesis", "netuid", "hotkey", "UID"} {
		fixture := newReleaseNativeValidatorTestFixture(t)
		ctx, chain, genesis, netuid, hotkey, uid := fixture.ctx, fixture.chain, fixture.genesis, uint16(521), fixture.hotkey, uint16(1)
		switch field {
		case "nil context":
			ctx = nil
		case "nil chain":
			chain = nil
		case "genesis":
			genesis = types.Hash{99}
		case "netuid":
			netuid = 0
		case "hotkey":
			hotkey = [32]byte{}
		case "UID":
			uid = math.MaxUint16
		}
		observed, err := readReleaseNativeValidatorAtContext(ctx, chain, genesis, netuid, hotkey, uid, fixture.expected)
		if err == nil || observed != (crv4.ValidatorStakeObservation{}) || len(fixture.calls) != 0 {
			t.Errorf("invalid %s reached native startup: %+v, %v", field, observed, err)
		}
	}
}

// The release wrapper never lets a fixture-authorized artifact or missing
// genesis replace its production release pin before opening native state.
func TestAuthenticatePinnedNativeRuntimeValidatorStakeRequiresReleasePins(t *testing.T) {
	t.Parallel()
	for _, wrongRuntime := range []bool{false, true} {
		fixture := newReleaseNativeValidatorTestFixture(t)
		cfg := &ReleaseConfig{RuntimeSpec: releaseRuntimeSpecVersion, TransactionVersion: releaseRuntimeTransactionVersion, StateVersion: releaseRuntimeStateVersion, RuntimeCodeHash: releaseRuntimeCodeHash, RuntimeMetadataHash: releaseRuntimeMetadataHash, Netuid: 521}
		if wrongRuntime {
			cfg.RuntimeSpec = 453
			cfg.GenesisHash = fixture.genesis.Hex()
		}
		observed, err := authenticateReleaseValidatorStakeContext(fixture.ctx, fixture.chain, cfg, fixture.hotkey, 1)
		if err == nil || observed != (crv4.ValidatorStakeObservation{}) || len(fixture.calls) != 0 {
			t.Errorf("incomplete release pin escaped, runtime=%t: %+v, %v", wrongRuntime, observed, err)
		}
	}
}
