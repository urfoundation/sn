// The actual setup native reader authenticates synthetic public RPC responses,
// consumed runtime metadata and the drained boundary; no observation is prebuilt.
package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
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

// The bundled profile opts generated configs into successor admission, so
// setup and production follow a later runtime whose consumed interfaces are
// unchanged instead of halting at the profile's own anchor.
func TestConfigureBundledMainnetProfileOptsIntoRuntimeSuccession(t *testing.T) {
	profile, err := readReleaseMainnetProfile()
	if err != nil {
		t.Fatal(err)
	}
	if profile.Config.RuntimeSuccessorProfile != crv4.ValidatorProducerRuntimeProfile {
		t.Fatalf("bundled profile does not opt into successor admission: %q", profile.Config.RuntimeSuccessorProfile)
	}
}

// The setup read on a chain that has moved past the profile's anchor, as
// mainnet has: an opted-in profile admits the authenticated successor and
// says so; without the opt-in the same read refuses the unreviewed runtime.
func TestConfigureNativeAdmitsSuccessorOnlyWithOptIn(t *testing.T) {
	for _, optIn := range []bool{true, false} {
		fixture := newProductionSuccessorTestFixture(t, false)
		profile := &releaseMainnetProfile{Config: *fixture.cfg, NativeChain: fixture.approval.NativeChain, Proposal: fixture.approval.Proposal}
		profile.Config.RuntimeSuccessorProfile = ""
		if optIn {
			profile.Config.RuntimeSuccessorProfile = crv4.ValidatorProducerRuntimeProfile
		}
		metadata, _ := provisionalValidatorMetadataTest(t, "../crv4/testdata/runtime475-metadata.scale.gz.base64", productionSuccessorTest475.MetadataHash)
		values, keyNames := map[string][]byte{}, map[string]string{}
		for _, field := range []struct {
			name  string
			value []byte
		}{
			{name: "LastEpochBlock", value: binary.LittleEndian.AppendUint64(nil, 170)},
			{name: "PendingEpochAt", value: make([]byte, 8)},
			{name: "SubnetEpochIndex", value: binary.LittleEndian.AppendUint64(nil, 20)},
			{name: "BlocksSinceLastStep", value: binary.LittleEndian.AppendUint64(nil, 10)},
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
			if method == "state_getStorage" && len(args) == 2 {
				if name, present := keyNames[fmt.Sprint(args[0])]; present {
					return setReleaseHistoricalTestResult(result, codec.HexEncodeToString(values[name]))
				}
			}
			return call(ctx, result, method, args...)
		}
		var log bytes.Buffer
		got, err := readReleaseConfigureNativeSnapshotOn(t.Context(), fixture.rpc.native, profile, &log)
		if !optIn {
			var unreviewed *crv4.UnreviewedRuntimeError
			if err == nil || !errors.As(err, &unreviewed) || log.Len() != 0 {
				t.Fatalf("setup without the opt-in read the successor runtime: %+v %v", got, err)
			}
			continue
		}
		if err != nil || got.Boundary.Epoch != 20 || got.Boundary.Block != 170 || got.Tempo != 12 {
			t.Fatalf("opted-in setup did not read the successor's drained boundary: %+v %v", got, err)
		}
		if !strings.Contains(log.String(), "admitted runtime node-subtensor/475") || !strings.Contains(log.String(), "successor of approved 473") {
			t.Fatalf("setup admitted a successor silently: %q", log.String())
		}
	}
}

// The bundled defaults are public terms only: public endpoints, and no
// private host, address, path or key material of any existing validator.
func TestBundledMainnetProfileNamesNothingPrivate(t *testing.T) {
	profile, err := readReleaseMainnetProfile()
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(releaseMainnetProfileBytes))
	for _, private := range []*regexp.Regexp{
		regexp.MustCompile(`\b127\.\d+\.\d+\.\d+\b|\blocalhost\b`),
		regexp.MustCompile(`\b10\.\d+\.\d+\.\d+\b|\b172\.(1[6-9]|2\d|3[01])\.\d+\.\d+\b|\b192\.168\.\d+\.\d+\b`),
		regexp.MustCompile(`"/(srv|etc|home|users|var|root)/`),
		regexp.MustCompile(`\.(seed|key|jwt)"`),
	} {
		if match := private.FindString(text); match != "" {
			t.Errorf("bundled profile contains %q", match)
		}
	}
	for _, endpoint := range append(append([]string{}, profile.Config.RPC...), profile.Config.Substrate...) {
		if !strings.HasPrefix(endpoint, "https://") && !strings.HasPrefix(endpoint, "wss://") {
			t.Errorf("bundled endpoint %q is not a public TLS endpoint", endpoint)
		}
	}
	if profile.Config.HotkeySeedFile != "" || profile.Config.StateDir != "" || profile.Config.ValidatorID != 0 || profile.Config.TreasuryApproval != nil {
		t.Fatal("bundled profile carries a validator's own identity or paths")
	}
}
