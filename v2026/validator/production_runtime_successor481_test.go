// The captured 481 artifact and its actual API3 declaration cross the real
// production successor path only under the already signed successor opt-in.
package validator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
)

const productionSuccessorTest481Code = "0x5fb504c1c793a3ddb5456f189ddef98b5146853aea6ff998501f1b6c6eb720bc"
const productionSuccessorTest481Metadata = "0x40db5b7feac3a8e9ab2ab000234257428826d9827ab326034a3329fdd0d50088"

// Exact state_getRuntimeVersion object captured at finalized block 9,320,004.
const productionSuccessorTest481VersionReply = `{"specName":"node-subtensor","implName":"node-subtensor","authoringVersion":1,"specVersion":481,"implVersion":1,"apis":[["0xdf6acb689907609b",5],["0x37e397fc7c91f5e4",2],["0x40fe3ad401f8959a",6],["0xfbc577b9d747efd6",1],["0xd2bc9897eed08f15",3],["0xf78b278be53f454c",2],["0xdd718d5cc53262d4",1],["0xab3c0572291feb8b",1],["0xed99c5acb25eedf5",3],["0xbc9d89904f5b923f",1],["0x37c8bb1350a9a2a8",4],["0xf3ff14d5ab527059",3],["0x582211f65bb14b89",6],["0xe65b00e46cedd0aa",2],["0x68b66ba122c93fa7",2],["0x42e62be4a39e5b60",1],["0x806df4ccaa9ed485",1],["0x8375104b299b74c5",3],["0x5d1fbfbe852f2807",1],["0xc6886e2f8e598b0a",1],["0x43580abff6baab45",5],["0xc0de4984d112f3b4",1],["0xcbca25e39f142387",2],["0xa8b093e6508d9e9c",1],["0x1c4585bd5c707202",1]],"transactionVersion":1,"systemVersion":1,"stateVersion":1}`

func newProductionSuccessor481TestFixture(t *testing.T, optIn bool) *productionSuccessorTestFixture {
	t.Helper()
	fixture := newProductionSuccessorTestFixture(t, optIn)
	_, wire := provisionalValidatorMetadataTest(t, "../crv4/testdata/runtime481-metadata.scale.gz.base64", productionSuccessorTest481Metadata)
	fixture.successorWire, fixture.successorCode = wire, productionSuccessorTest481Code
	if err := json.Unmarshal([]byte(productionSuccessorTest481VersionReply), &fixture.successorVersion); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestProductionRuntimeSuccessorCrossesRuntime481(t *testing.T) {
	fixture := newProductionSuccessor481TestFixture(t, true)
	native, cfg := fixture.rpc.native, fixture.cfg
	approved := releaseNativeRuntimeIdentity(cfg)
	if err := enableReleaseRuntimeSuccession(native, cfg); err != nil {
		t.Fatal(err)
	}
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, cfg, mainnetRuntimeTestBlock(150)); err != nil {
		t.Fatal(err)
	}
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, cfg, mainnetRuntimeTestBlock(170)); err != nil {
		t.Fatalf("compatible runtime481 API3 successor halted production: %v", err)
	}
	if uint32(native.Runtime.SpecVersion) != 481 || validateReleaseNativeSigningRuntime(native, cfg) != nil {
		t.Fatal("signing view did not bind the exact runtime481 successor")
	}
	if native.ValidateValidatorProducerRuntime(approved) == nil {
		t.Fatal("runtime481 successor satisfied the unchanged approved anchor itself")
	}
	historical := *native
	if err := authenticateHistoricalNativeRuntimeAtContext(t.Context(), &historical, cfg, mainnetRuntimeTestBlock(150)); err != nil || uint32(historical.Runtime.SpecVersion) != 473 {
		t.Fatal("runtime481 admission changed the exact historical opening", err)
	}
	if validateReleaseNativeSigningRuntime(&historical, cfg) == nil {
		t.Fatal("historical predecessor view acquired current signing authority")
	}
	if err := validatePreparedNativeRuntimeContext(t.Context(), native, cfg, mainnetRuntimeTestBlock(150), mainnetRuntimeTestBlock(170)); err == nil {
		t.Fatal("runtime481 replayed bytes prepared under its predecessor")
	}
}

// A reviewed API addition does not replace signed opt-in, the exact runtime
// family, the consumed storage/call shapes or the caller's live context.
func TestProductionRuntime481RetainsAuthorityAndInterfaceRefusals(t *testing.T) {
	for _, fault := range []string{"no-opt-in", "future-api3-spec", "owner-storage", "producer-storage", "producer-call", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			fixture := newProductionSuccessor481TestFixture(t, fault != "no-opt-in")
			native, cfg := fixture.rpc.native, fixture.cfg
			if err := enableReleaseRuntimeSuccession(native, cfg); err != nil {
				t.Fatal(err)
			}
			if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, cfg, mainnetRuntimeTestBlock(150)); err != nil {
				t.Fatal(err)
			}
			oldMeta, oldRuntime := native.Meta, native.Runtime
			switch fault {
			case "future-api3-spec":
				fixture.successorVersion["specVersion"] = 487
			case "owner-storage", "producer-storage", "producer-call":
				metadata, _ := provisionalValidatorMetadataTest(t, "../crv4/testdata/runtime481-metadata.scale.gz.base64", productionSuccessorTest481Metadata)
				changed := false
				for index := range metadata.AsMetadataV14.Pallets {
					pallet := &metadata.AsMetadataV14.Pallets[index]
					if fault == "producer-call" && pallet.Name == "Commitments" {
						calls := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
						for variant := range calls.Def.Variant.Variants {
							if calls.Def.Variant.Variants[variant].Name == "set_commitment" {
								calls.Def.Variant.Variants[variant].Index++
								changed = true
							}
						}
					}
					if pallet.Name != crv4.PalletName {
						continue
					}
					name := "OwnedHotkeys"
					if fault == "producer-storage" {
						name = "SubnetEpochIndex"
					}
					if fault == "producer-call" {
						continue
					}
					for item := range pallet.Storage.Items {
						if string(pallet.Storage.Items[item].Name) == name {
							pallet.Storage.Items[item].Type.AsMap.Hashers = []types.StorageHasherV10{{IsTwox64Concat: true}}
							changed = true
						}
					}
				}
				if !changed {
					t.Fatal("changed-interface fixture did not locate its target", fault)
				}
				var err error
				fixture.successorWire, err = codec.EncodeToHex(metadata)
				if err != nil {
					t.Fatal(err)
				}
			}
			ctx := t.Context()
			if fault == "canceled" {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			err := authenticatePinnedNativeRuntimeAtContext(ctx, native, cfg, mainnetRuntimeTestBlock(170))
			if err == nil {
				t.Fatal("runtime481 boundary accepted", fault)
			}
			if fault == "owner-storage" && !strings.Contains(err.Error(), "owner-recycle storage OwnedHotkeys") {
				t.Fatal("owner-storage failure did not reach the intended boundary", err)
			}
			if fault == "producer-storage" && !strings.Contains(err.Error(), "storage/SubtensorModule.SubnetEpochIndex changed") {
				t.Fatal("producer-storage failure did not reach the intended boundary", err)
			}
			if fault == "producer-call" && !strings.Contains(err.Error(), "call/Commitments.set_commitment changed") {
				t.Fatal("producer-call failure did not reach the intended boundary", err)
			}
			if native.Meta != oldMeta || native.Runtime != oldRuntime || validateReleaseNativeSigningRuntime(native, cfg) != nil {
				t.Fatal("refused runtime481 boundary changed the retained approved signing view", fault)
			}
		})
	}
}
