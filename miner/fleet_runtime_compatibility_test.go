package miner

// Actual public metadata exercises fleet runtime binding and commitment
// decoding. Accounts, code hashes, blocks and storage are synthetic.

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/docopt/docopt-go"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

// Preserve the future-runtime branch when the reviewed release advances.
const provisionalFleetSuccessorTestSpec = crv4.ReviewedRuntimeSpecVersion + 1

type provisionalFleetRuntimeFixture struct {
	chain        *crv4.Chain
	block        types.Hash
	metadata     *types.Metadata
	metadataHex  string
	code         string
	storageReads int
}

// RPC results use the real JSON assignment shapes, including native hashes.
func setProvisionalFleetRuntimeResult(result any, value any) error {
	wire, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(wire, result)
}

func newProvisionalFleetRuntimeFixture(t *testing.T) *provisionalFleetRuntimeFixture {
	t.Helper()
	encoded, err := os.ReadFile("../crv4/runtime-profile-v1.scale.gz.base64")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(io.LimitReader(reader, 347305))
	if err := errors.Join(err, reader.Close()); err != nil || len(raw) != 347304 {
		t.Fatalf("metadata bytes=%d: %v", len(raw), err)
	}
	self := &provisionalFleetRuntimeFixture{block: types.Hash{0xb1}, metadataHex: codec.HexEncodeToString(raw), code: types.Hash{0x75}.Hex()}
	var hash string
	self.metadata, hash, err = crv4.DecodeRuntimeMetadata(self.metadataHex)
	if err != nil || hash != "0xb0fae6d022b74faf948e3b98463b98b46c4738e87348e24340f91146ededa4bf" {
		t.Fatalf("metadata hash=%s: %v", hash, err)
	}
	genesis, err := types.NewHashFromHexString(fleetProvisionalTestnetGenesis)
	if err != nil {
		t.Fatal(err)
	}
	commitmentKey, err := types.CreateStorageKey(self.metadata, "Commitments", "CommitmentOf", binary.LittleEndian.AppendUint16(nil, 7), bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	lastKey, err := types.CreateStorageKey(self.metadata, "Commitments", "LastCommitment", binary.LittleEndian.AppendUint16(nil, 7), bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	info, err := crv4.EncodeFleetCommitmentInfo([32]byte{0x66})
	if err != nil {
		t.Fatal(err)
	}
	registration := binary.LittleEndian.AppendUint64(nil, 1)
	registration = binary.LittleEndian.AppendUint32(registration, 42)
	registration = append(registration, info...)
	client := &fleetRuntimeTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if method == "chain_getFinalizedHead" {
			return setProvisionalFleetRuntimeResult(result, self.block.Hex())
		}
		if method == "chain_getBlockHash" {
			if len(args) != 1 || args[0] != uint64(0) {
				return errors.New("unexpected genesis request")
			}
			return setProvisionalFleetRuntimeResult(result, genesis.Hex())
		}
		if len(args) == 0 || args[len(args)-1] != self.block.Hex() {
			return fmt.Errorf("unpinned fleet %s: %v", method, args)
		}
		switch method {
		case "state_getRuntimeVersion":
			return setProvisionalFleetRuntimeResult(result, map[string]any{"specName": "node-subtensor", "specVersion": provisionalFleetSuccessorTestSpec, "transactionVersion": 1, "stateVersion": 1, "apis": []any{[]any{"0x8375104b299b74c5", 2}}})
		case "state_getStorageHash":
			if len(args) != 2 || args[0] != "0x3a636f6465" {
				return errors.New("runtime code key changed")
			}
			return setProvisionalFleetRuntimeResult(result, self.code)
		case "state_getMetadata":
			return setProvisionalFleetRuntimeResult(result, self.metadataHex)
		case "state_getStorage":
			self.storageReads++
			if len(args) != 2 {
				return errors.New("invalid commitment parameters")
			}
			if args[0] == commitmentKey.Hex() {
				return setProvisionalFleetRuntimeResult(result, codec.HexEncodeToString(registration))
			}
			if args[0] == lastKey.Hex() {
				return setProvisionalFleetRuntimeResult(result, codec.HexEncodeToString(binary.LittleEndian.AppendUint32(nil, 42)))
			}
			return errors.New("unexpected commitment key")
		case "chain_getHeader":
			return setProvisionalFleetRuntimeResult(result, types.Header{Number: 100})
		default:
			return fmt.Errorf("unexpected provisional fleet RPC %s", method)
		}
	}}
	self.chain = fleetRuntimeTestChain(client)
	self.chain.GenesisHash = genesis
	self.chain.Runtime = &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: types.U32(crv4.ReviewedRuntimeSpecVersion), TransactionVersion: 1}
	return self
}

func TestFleetProvisionalRuntimeFlagsRequireExplicitTestnetAuthority(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "runtime")
	args := []string{"fleet", "publish", "--manifest=synthetic.json", "--substrate=wss://native.example", "--hotkey_seed_file=synthetic.seed", "--provisional-runtime-compatibility=" + crv4.ProvisionalRuntimeCompatibilityProfile, "--runtime-observation-dir=" + directory}
	opts, err := docopt.ParseArgs(mainUsage(), args, "test")
	if err != nil {
		t.Fatal(err)
	}
	if fleetOpt(opts, "--provisional-runtime-compatibility") != crv4.ProvisionalRuntimeCompatibilityProfile || fleetOpt(opts, "--runtime-observation-dir") != directory {
		t.Fatal("fleet CLI dropped explicit profile flags")
	}
	manifest := &protocol.FleetManifest{ChainID: 945}
	if err := validateFleetRuntimeCompatibility(manifest, crv4.ProvisionalRuntimeCompatibilityProfile, directory); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name               string
		chain              uint64
		profile, directory string
	}{
		{"profile", 945, "unknown", directory},
		{"missing profile", 945, "", directory},
		{"missing directory", 945, crv4.ProvisionalRuntimeCompatibilityProfile, ""},
		{"relative directory", 945, crv4.ProvisionalRuntimeCompatibilityProfile, "relative"},
		{"chain", 1, crv4.ProvisionalRuntimeCompatibilityProfile, directory},
	} {
		if err := validateFleetRuntimeCompatibility(&protocol.FleetManifest{ChainID: item.chain}, item.profile, item.directory); err == nil {
			t.Fatalf("%s admitted", item.name)
		}
	}
	fixture := newProvisionalFleetRuntimeFixture(t)
	fixture.chain.GenesisHash = types.Hash{9}
	if err := enableFleetProvisionalRuntimeCompatibility(fixture.chain, manifest, crv4.ProvisionalRuntimeCompatibilityProfile, directory); err == nil {
		t.Fatal("wrong native genesis admitted")
	}
}

func TestFleetProvisionalRuntimeAuthenticatesCurrentStatusAndSigningBinding(t *testing.T) {
	fixture := newProvisionalFleetRuntimeFixture(t)
	var hotkey [32]byte
	copy(hotkey[:], bytes.Repeat([]byte{0x22}, 32))
	if _, err := pinnedFleetCommitmentFinalizedContext(t.Context(), fixture.chain, 7, hotkey); err == nil || fixture.storageReads != 0 || fixture.chain.Runtime.SpecVersion != types.U32(crv4.ReviewedRuntimeSpecVersion) {
		t.Fatal("strict fleet admitted future runtime")
	}
	directory := filepath.Join(t.TempDir(), "observations")
	if err := enableFleetProvisionalRuntimeCompatibility(fixture.chain, &protocol.FleetManifest{ChainID: 945}, crv4.ProvisionalRuntimeCompatibilityProfile, directory); err != nil {
		t.Fatal(err)
	}
	observed, err := pinnedFleetCommitmentFinalizedContext(t.Context(), fixture.chain, 7, hotkey)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Hash != ([32]byte{0x66}) || observed.FinalizedHash != fixture.block || observed.CommitmentBlock != 42 || fixture.storageReads != 2 || fixture.chain.Runtime.SpecVersion != types.U32(provisionalFleetSuccessorTestSpec) {
		t.Fatal("fleet status did not bind exact successor state")
	}
	if fixture.chain.CurrentRuntimeCompatibilityProfile() != crv4.ProvisionalRuntimeCompatibilityProfile {
		t.Fatal("actual signing metadata lacks authenticated profile")
	}
	if _, err := fixture.chain.NewSetFleetCommitmentCall(7, [32]byte{0x66}); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("fleet observation count=%d: %v", len(files), err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{fixture.chain.GenesisHash.Hex(), fixture.block.Hex(), fixture.code, fmt.Sprintf(`"specVersion": %d`, provisionalFleetSuccessorTestSpec), `"provisional": true`, `"final_acceptance": false`} {
		if !bytes.Contains(raw, []byte(value)) {
			t.Fatalf("fleet observation omitted %s", value)
		}
	}
}

func TestFleetProvisionalRuntimeRefusesForgedBindingAndUndurableObservation(t *testing.T) {
	fixture := newProvisionalFleetRuntimeFixture(t)
	directory := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(directory, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := enableFleetProvisionalRuntimeCompatibility(fixture.chain, &protocol.FleetManifest{ChainID: 945}, crv4.ProvisionalRuntimeCompatibilityProfile, directory); err != nil {
		t.Fatal(err)
	}
	forged := crv4.AuthenticatedRuntimeArtifact{BlockHash: fixture.block, GenesisHash: fixture.chain.GenesisHash, Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: provisionalFleetSuccessorTestSpec, TransactionVersion: 1, StateVersion: 1}, CodeHash: fixture.code, MetadataHash: "0xb0fae6d022b74faf948e3b98463b98b46c4738e87348e24340f91146ededa4bf", Metadata: fixture.metadata, CompatibilityProfile: crv4.ProvisionalRuntimeCompatibilityProfile}
	if err := bindFleetRuntime(fixture.chain, forged); err == nil {
		t.Fatal("caller supplied compatibility marker authorized metadata")
	}
	if _, err := authenticateAndBindFleetRuntimeFinalizedContext(t.Context(), fixture.chain); err == nil || fixture.chain.Runtime.SpecVersion != types.U32(crv4.ReviewedRuntimeSpecVersion) || fixture.chain.Meta != nil {
		t.Fatal("failed durable observation changed fleet signing view")
	}
}
