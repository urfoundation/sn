// The actual setup native reader authenticates synthetic public RPC responses,
// consumed runtime metadata and the drained boundary; no observation is prebuilt.
package validator

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/crv4"
)

// The existing exact-artifact fixture gains only the setup reader's storage.
// Missing registration/permit fields cannot accidentally become prerequisites.
func configureNativeTestInputs(t *testing.T) (*releaseMainnetProfile, *productionRuntimeTestFixture, map[string][]byte) {
	t.Helper()
	fixture := newProductionRuntimeTestFixture(t, false)
	metadata, encoded, digest := ownerRecycleProductionTestMetadata(t)
	fixture.rpc.metadata, fixture.rpc.head = encoded, 126
	profile := &releaseMainnetProfile{Config: *fixture.cfg, NativeChain: fixture.approval.NativeChain, Proposal: fixture.approval.Proposal}
	profile.Config.RuntimeMetadataHash = digest
	profile.Proposal.Runtime.MetadataHash, _ = parseHash32("synthetic metadata", digest)
	values := map[string][]byte{}
	keyNames := map[string]string{}
	for _, field := range []struct {
		name  string
		value []byte
	}{
		{name: "LastEpochBlock", value: binary.LittleEndian.AppendUint64(nil, 120)},
		{name: "PendingEpochAt", value: make([]byte, 8)},
		{name: "SubnetEpochIndex", value: binary.LittleEndian.AppendUint64(nil, 20)},
		{name: "BlocksSinceLastStep", value: binary.LittleEndian.AppendUint64(nil, 6)},
		{name: "Tempo", value: binary.LittleEndian.AppendUint16(nil, 12)},
		{name: "PendingServerEmission", value: make([]byte, 8)},
	} {
		key, err := types.CreateStorageKey(metadata, crv4.PalletName, field.name, binary.LittleEndian.AppendUint16(nil, profile.Config.Netuid))
		if err != nil {
			t.Fatal(err)
		}
		values[field.name], keyNames[key.Hex()] = field.value, field.name
	}
	call := fixture.rpc.client.callContext
	fixture.rpc.client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if method == "state_getStorage" {
			if len(args) != 2 {
				return fmt.Errorf("unexpected storage arguments: %v", args)
			}
			name, present := keyNames[fmt.Sprint(args[0])]
			if !present {
				return fmt.Errorf("setup asked for undeclared storage: %v", args[0])
			}
			return setReleaseHistoricalTestResult(result, codec.HexEncodeToString(values[name]))
		}
		return call(ctx, result, method, args...)
	}
	return profile, fixture, values
}

// Registration/permit is intentionally unavailable. Initial setup still reads
// the exact current network, runtime and drained native epoch before signing.
func TestConfigureNativeReadsActualDrainedBoundaryWithoutPermit(t *testing.T) {
	profile, fixture, _ := configureNativeTestInputs(t)
	got, err := readReleaseConfigureNativeBoundary(t.Context(), fixture.rpc.native, profile)
	if err != nil || got.Boundary.Epoch != 20 || got.Boundary.Block != 120 || got.Boundary.Hash != [32]byte(mainnetRuntimeTestBlock(120)) || got.Tempo != 12 {
		t.Fatalf("setup boundary %+v: %v", got, err)
	}
	if fixture.rpc.callKVs["system_chain"] == 0 || fixture.rpc.callKVs["state_getStorageHash"] < 2 || fixture.rpc.callKVs["chain_getFinalizedHead"] == 0 {
		t.Fatal("setup skipped actual network, artifact or finality reads", fixture.rpc.callKVs)
	}
}

// Each contradiction is supplied to the real read path, with the unchanged
// fixture first proving its healthy response rather than mocking a verdict.
func TestConfigureNativeRejectsUndrainedAndWrongNetworkOrArtifact(t *testing.T) {
	for _, fault := range []string{"pending emission", "missing epoch", "future boundary", "zero tempo", "wrong network", "wrong code", "noncanonical boundary"} {
		profile, fixture, values := configureNativeTestInputs(t)
		if _, err := readReleaseConfigureNativeBoundary(t.Context(), fixture.rpc.native, profile); err != nil {
			t.Fatal("healthy native control failed", fault, err)
		}
		switch fault {
		case "pending emission":
			values["PendingServerEmission"] = binary.LittleEndian.AppendUint64(nil, 1)
		case "missing epoch":
			values["SubnetEpochIndex"] = make([]byte, 8)
		case "future boundary":
			values["LastEpochBlock"] = binary.LittleEndian.AppendUint64(nil, 127)
		case "zero tempo":
			values["Tempo"] = make([]byte, 2)
		case "wrong network":
			fixture.rpc.nativeChain = "Other Synthetic Network"
		case "wrong code":
			fixture.rpc.codes[1] = (types.Hash{0xed}).Hex()
		case "noncanonical boundary":
			fixture.rpc.canonicalHashKVs[120] = types.Hash{0xee}
		}
		if got, err := readReleaseConfigureNativeBoundary(t.Context(), fixture.rpc.native, profile); err == nil || got != (releaseConfigureNativeSnapshot{}) {
			t.Errorf("%s supplied an initial authority boundary: %+v %v", fault, got, err)
		}
	}
}
