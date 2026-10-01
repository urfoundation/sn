//go:build linux || darwin

// Production history controls use signed config files, actual SCALE storage,
// the selective metagraph decoder, dual-key consent and local EVM HTTP reads.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Mutation is owned by the test between completed reader invocations. A reader
// receives storage/API bytes, never a precomputed eligibility observation.
type productionRuntimeStakeTestState struct {
	permit bool
	epoch  uint64
	reads  uint64
}

func installProductionRuntimeStakeTest(t *testing.T, fixture *productionRuntimeTestFixture, hotkey [32]byte) *productionRuntimeStakeTestState {
	t.Helper()
	metadata, _, err := crv4.DecodeRuntimeMetadata(fixture.rpc.metadata)
	if err != nil {
		t.Fatal(err)
	}
	state := &productionRuntimeStakeTestState{permit: true, epoch: 19}
	netuid := binary.LittleEndian.AppendUint16(nil, fixture.cfg.Netuid)
	uid := binary.LittleEndian.AppendUint16(nil, 1)
	keys := map[string]string{}
	put := func(name string, args ...[]byte) {
		key, err := types.CreateStorageKey(metadata, crv4.PalletName, name, args...)
		if err != nil {
			t.Fatal(err)
		}
		keys[key.Hex()] = name
	}
	put("SubnetworkN", netuid)
	put("Keys", netuid, uid)
	put("Uids", netuid, hotkey[:])
	put("Owner", hotkey[:])
	put("TotalHotkeyAlpha", hotkey[:], netuid)
	put("ValidatorPermit", netuid)
	put("StakeThreshold")
	put("SubnetOwnerHotkey", netuid)
	put("SubnetEpochIndex", netuid)
	original := fixture.rpc.native.API.Client.(*validatorRuntimeIdentityTestClient).callContext
	fixture.rpc.native.API.Client.(*validatorRuntimeIdentityTestClient).callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if method == "state_getStorage" || method == "state_call" {
			state.reads++
			hash, err := types.NewHashFromHexString(fmt.Sprint(args[len(args)-1]))
			if err != nil || hash != mainnetRuntimeTestBlock(100) {
				return errors.New("production historical stake escaped the original block")
			}
			var value []byte
			if method == "state_getStorage" {
				if len(args) != 2 {
					return errors.New("production historical storage is not exact-block")
				}
				switch keys[fmt.Sprint(args[0])] {
				case "SubnetworkN":
					value = []byte{3, 0}
				case "Keys":
					value = hotkey[:]
				case "Uids":
					value = uid
				case "Owner":
					value = bytes.Repeat([]byte{0x71}, 32)
				case "TotalHotkeyAlpha", "StakeThreshold":
					value = binary.LittleEndian.AppendUint64(nil, 100)
				case "ValidatorPermit":
					value = []byte{12, 0, 0, 0}
					if state.permit {
						value[2] = 1
					}
				case "SubnetOwnerHotkey":
					return setReleaseHistoricalTestResult(result, nil)
				case "SubnetEpochIndex":
					value = binary.LittleEndian.AppendUint64(nil, state.epoch)
				default:
					return errors.New("production historical storage key is unknown")
				}
			} else {
				request := append(bytes.Clone(netuid), []byte{20, 0, 0, 30, 0, 52, 0, 57, 0, 69, 0}...)
				if len(args) != 3 || args[0] != "SubnetInfoRuntimeApi_get_selective_metagraph" || args[1] != hexutil.Encode(request) {
					return errors.New("production historical metagraph changed its exact query")
				}
				value = append([]byte{1}, releaseNativeValidatorTestCompact(t, uint64(fixture.cfg.Netuid))...)
				for index := 1; index <= 76; index++ {
					if index != 30 && index != 52 && index != 57 && index != 69 {
						value = append(value, 0)
						continue
					}
					value = append(value, 1, 12)
					switch index {
					case 52:
						for _, key := range [][32]byte{{31}, hotkey, {33}} {
							value = append(value, key[:]...)
						}
					case 57:
						permit := byte(0)
						if state.permit {
							permit = 1
						}
						value = append(value, 0, permit, 0)
					case 69:
						for _, stake := range []uint64{0, 150, 0} {
							value = append(value, releaseNativeValidatorTestCompact(t, stake)...)
						}
					}
				}
			}
			return setReleaseHistoricalTestResult(result, hexutil.Encode(value))
		}
		// Match a real JSON transport, including the capture reader's bounded
		// custom destination; the original fixture owns typed header/hash reads.
		if method == "chain_getHeader" {
			var header types.Header
			if err := original(ctx, &header, method, args...); err != nil {
				return err
			}
			return setReleaseHistoricalTestResult(result, header)
		}
		if method == "chain_getBlockHash" {
			var hash types.Hash
			if err := original(ctx, &hash, method, args...); err != nil {
				return err
			}
			return setReleaseHistoricalTestResult(result, hash)
		}
		var wire json.RawMessage
		if err := original(ctx, &wire, method, args...); err != nil {
			return err
		}
		return json.Unmarshal(wire, result)
	}
	return state
}

// Restart replays the old approved artifact at its original block, while the
// current producer view stays untouched. Dropping the private config fails.
func TestProductionRuntimeStartupPreservesApprovedOriginalSchedule(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, true)
	hotkey := fixture.approval.ValidatorHotkey
	state := installProductionRuntimeStakeTest(t, fixture, hotkey)
	initial := ReleaseEvidenceV2ActivationContext{Activation: protocol.ValidatorEvidenceActivation{
		Domain: protocol.ValidatorEvidenceActivationDomain{ChainID: fixture.cfg.ChainID, GenesisHash: [32]byte(fixture.rpc.genesis), Netuid: fixture.cfg.Netuid},
		Hotkey: hotkey, NativeBlock: 100, NativeHash: [32]byte(mainnetRuntimeTestBlock(100))}}
	journal := &releaseMeasurementInputJournal{SubnetEpoch: state.epoch, MeasurementInput: ReleaseMeasurementInput{CutNativeBlock: 100, CutNativeBlockHash: mainnetRuntimeTestBlock(100).Hex()}}
	metadata, runtime := fixture.rpc.native.Meta, fixture.rpc.native.Runtime
	read := func() error {
		return authenticateReleaseStartupNativeV2ContextWithConfig(t.Context(), fixture.rpc.native, initial, journal, releaseNativeRuntimeIdentity(fixture.cfg), false, false, fixture.cfg)
	}
	if err := read(); err != nil {
		t.Fatal(err)
	}
	if state.reads == 0 || fixture.rpc.native.Meta != metadata || fixture.rpc.native.Runtime != runtime {
		t.Fatal("startup omitted real historical stake reads or changed current signing view")
	}
	before := state.reads
	if err := authenticateReleaseStartupNativeV2Context(t.Context(), fixture.rpc.native, initial, journal, releaseNativeRuntimeIdentity(fixture.cfg), false); err == nil || state.reads != before {
		t.Fatal("identity-only startup inferred production history")
	}
	state.permit = false
	if err := read(); err == nil {
		t.Fatal("production startup accepted old consent without actual historical permit")
	}
	state.permit = true
	state.epoch++
	if err := read(); err == nil {
		t.Fatal("production startup accepted a changed original epoch")
	}
}

// Actual activation authentication joins the original native stake reader with
// independently signed consent and an actual hash-pinned EVM HTTP transcript.
func TestProductionRuntimeActivationCarriesSignedHistoryAuthority(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, true)
	activation := newReleaseActivationV2TestFixture(t, "")
	key, err := crv4.KeypairFromSeed([32]byte{0x31})
	if err != nil {
		t.Fatal(err)
	}
	state := installProductionRuntimeStakeTest(t, fixture, key.PublicKey())
	expected := &activation.authority.Expected
	expected.Domain.ChainID, expected.Domain.GenesisHash, expected.Domain.Netuid = fixture.cfg.ChainID, [32]byte(fixture.rpc.genesis), fixture.cfg.Netuid
	expected.Domain.Coordinator = [20]byte(common.HexToAddress(fixture.cfg.Coordinator))
	expected.Domain.SettlementVault = [20]byte(common.HexToAddress(fixture.cfg.SettlementVault))
	expected.Domain.DeploymentIDHash = sha256.Sum256([]byte(fixture.cfg.DeploymentID))
	expected.Domain.PolicyHash, _ = parseHash32("production policy", fixture.cfg.PolicyHash)
	expected.NativeHash = [32]byte(mainnetRuntimeTestBlock(100))
	activation.chain.chainId = new(big.Int).SetUint64(fixture.cfg.ChainID)
	activation.chain.contractAddr = common.Address(expected.Domain.Coordinator)
	activation.native.chain = fixture.rpc.native
	activation.authority.NativeRuntime = releaseNativeRuntimeIdentity(fixture.cfg)
	activation.authority.productionRuntimeConfig = fixture.cfg
	seed := [32]byte{0x41}
	activation.vpkSignature, err = expected.SignVPK(ed25519.NewKeyFromSeed(seed[:]))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := expected.Digest()
	if err != nil {
		t.Fatal(err)
	}
	activation.hotkeySignature, err = key.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := activation.read(t.Context()); err != nil {
		t.Fatal(err)
	}
	if state.reads == 0 {
		t.Fatal("activation bypassed its original native stake")
	}
	before := activation.calls.Load()
	activation.authority.productionRuntimeConfig = nil
	if _, err := activation.read(t.Context()); err == nil || activation.calls.Load() != before {
		t.Fatal("activation inferred mainnet runtime history from its tuple")
	}
}
