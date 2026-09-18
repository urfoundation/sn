package crv4

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

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

func provisionalRuntimeMetadataTest(t *testing.T) (*types.Metadata, string, string) {
	t.Helper()
	encoded, err := os.ReadFile("testdata/runtime463-metadata.scale.gz.base64")
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
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, 338397))
	if err != nil || len(raw) != 338396 {
		t.Fatalf("fixture size: %d %v", len(raw), err)
	}
	hex := fmt.Sprintf("0x%x", raw)
	metadata, hash, err := DecodeRuntimeMetadata(hex)
	if err != nil || hash != "0xe9af0fcab804e08c0f6cc2c13715b1e366a916eda6a61aec6fb2601bc2a66b4c" {
		t.Fatalf("fixture hash: %s %v", hash, err)
	}
	return metadata, hash, hex
}

func TestProvisionalRuntimeCompatibilityActualConsumedProfile(t *testing.T) {
	metadata, _, _ := provisionalRuntimeMetadataTest(t)
	if err := ValidateProvisionalRuntimeMetadata(metadata); err != nil {
		t.Fatal(err)
	}
	baseline, err := runtimeProfileBaseline()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateProvisionalRuntimeMetadata(baseline); err != nil {
		t.Fatal(err)
	}
}

func TestProvisionalRuntimeCompatibilityRejectsConsumedChanges(t *testing.T) {
	for _, change := range []string{"storage-width", "storage-hasher", "call-index", "call-argument", "event-index", "signed-extension", "missing-storage", "metadata-version"} {
		metadata, _, _ := provisionalRuntimeMetadataTest(t)
		switch change {
		case "metadata-version":
			metadata.Version = 15
		case "signed-extension":
			metadata.AsMetadataV14.Extrinsic.SignedExtensions = metadata.AsMetadataV14.Extrinsic.SignedExtensions[1:]
		default:
			for i := range metadata.AsMetadataV14.Pallets {
				pallet := &metadata.AsMetadataV14.Pallets[i]
				if pallet.Name != "SubtensorModule" {
					continue
				}
				switch change {
				case "storage-width", "storage-hasher", "missing-storage":
					for j := range pallet.Storage.Items {
						item := &pallet.Storage.Items[j]
						if item.Name != "Uids" {
							continue
						}
						switch change {
						case "storage-width":
							item.Type.AsMap.Value = item.Type.AsMap.Key
						case "storage-hasher":
							item.Type.AsMap.Hashers = nil
						case "missing-storage":
							item.Name = "RemovedUids"
						}
						break
					}
				case "call-index", "call-argument":
					item := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
					for j := range item.Def.Variant.Variants {
						v := &item.Def.Variant.Variants[j]
						if v.Name != "commit_timelocked_weights" {
							continue
						}
						if change == "call-index" {
							v.Index++
						} else {
							v.Fields = v.Fields[1:]
						}
						break
					}
				case "event-index":
					item := metadata.AsMetadataV14.EfficientLookup[pallet.Events.Type.Int64()]
					for j := range item.Def.Variant.Variants {
						v := &item.Def.Variant.Variants[j]
						if v.Name == "TimelockedWeightsCommitted" {
							v.Index++
							break
						}
					}
				}
			}
		}
		if err := ValidateProvisionalRuntimeMetadata(metadata); err == nil {
			t.Fatalf("%s acquired compatibility", change)
		}
	}
}

func TestProvisionalRuntimeCompatibilityConsumedApiOnly(t *testing.T) {
	for _, unrelated := range []int{3, 4, 99} {
		raw := json.RawMessage(fmt.Sprintf(`{"apis":[["0x8375104b299b74c5",2],["0x43580abff6baab45",%d]]}`, unrelated))
		if err := validateProvisionalRuntimeApis(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`{}`, `{"apis":[]}`, `{"apis":[["0x8375104b299b74c5",3]]}`, `{"apis":[["0x8375104b299b74c5",2],["0x8375104b299b74c5",2]]}`, `{"apis":[["0x8375104b299b74c5","2"]]}`} {
		if err := validateProvisionalRuntimeApis(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestProvisionalRuntimeCompatibilityDurableObservationFailureAndReuse(t *testing.T) {
	metadata, hash, _ := provisionalRuntimeMetadataTest(t)
	artifact := AuthenticatedRuntimeArtifact{BlockHash: types.Hash{4}, GenesisHash: types.Hash{8}, Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 463, TransactionVersion: 1, StateVersion: 1}, CodeHash: types.Hash{7}.Hex(), MetadataHash: hash, Metadata: metadata, CompatibilityProfile: ProvisionalRuntimeCompatibilityProfile}
	dir := filepath.Join(t.TempDir(), "observations")
	if err := WriteProvisionalRuntimeObservation(dir, artifact); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatal("observation missing", err)
	}
	path := filepath.Join(dir, files[0].Name())
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	artifact.BlockHash = types.Hash{5}
	if err := WriteProvisionalRuntimeObservation(dir, artifact); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("observation was relabeled", err)
	}
	if err := os.WriteFile(path, []byte(`{"final_acceptance":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteProvisionalRuntimeObservation(dir, artifact); err == nil {
		t.Fatal("invalid existing observation accepted")
	}
	bad := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(bad, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteProvisionalRuntimeObservation(bad, artifact); err == nil {
		t.Fatal("evidence write failure accepted")
	}
}

func TestProvisionalRuntimeCompatibilityExactArtifactAndFailureRecovery(t *testing.T) {
	_, expectedHash, encoded := provisionalRuntimeMetadataTest(t)
	allowed, _ := ReviewedRuntimeArtifact(RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 461, TransactionVersion: 1, StateVersion: 1})
	for _, fault := range []string{"strict", "valid", "future-version", "wrong-genesis", "wrong-api", "wrong-name", "transaction", "state", "evidence-write", "catalog-hash", "foreign-pin", "cancelled"} {
		genesis := types.Hash{8}
		version := RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 463, TransactionVersion: 1, StateVersion: 1}
		api := 2
		code := types.Hash{9}.Hex()
		pin := allowed
		observations, metadataReads := 0, 0
		switch fault {
		case "future-version":
			version.SpecVersion = 500
		case "wrong-api":
			api = 3
		case "wrong-name":
			version.SpecName = "foreign"
		case "transaction":
			version.TransactionVersion = 2
		case "state":
			version.StateVersion = 2
		case "catalog-hash":
			version = allowed.Version
		case "foreign-pin":
			pin.CodeHash = types.Hash{99}.Hex()
		}
		client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, target any, method string, args ...any) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			switch method {
			case "chain_getBlockHash":
				value := genesis
				if fault == "wrong-genesis" {
					value = types.Hash{7}
				}
				dest, ok := target.(*types.Hash)
				if !ok {
					return fmt.Errorf("unexpected genesis type %T", target)
				}
				*dest = value
				return nil
			case "state_getRuntimeVersion":
				return setRuntimeIdentityTestResult(target, map[string]any{"specName": version.SpecName, "specVersion": version.SpecVersion, "transactionVersion": version.TransactionVersion, "stateVersion": version.StateVersion, "apis": []any{[]any{"0x8375104b299b74c5", api}}})
			case "state_getStorageHash":
				return setRuntimeIdentityTestResult(target, code)
			case "state_getMetadata":
				metadataReads++
				return setRuntimeIdentityTestResult(target, encoded)
			default:
				return fmt.Errorf("unexpected or mutating RPC %s", method)
			}
		}}
		chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}, GenesisHash: genesis}
		if fault != "strict" {
			if err := chain.EnableProvisionalRuntimeCompatibility(genesis, func(a AuthenticatedRuntimeArtifact) error {
				observations++
				if fault == "evidence-write" {
					return errors.New("forced evidence failure")
				}
				if a.Version != version || a.GenesisHash != genesis || a.MetadataHash != expectedHash || a.CodeHash != code {
					return errors.New("observation changed identity")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		ctx := context.Background()
		if fault == "cancelled" {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			ctx = cancelled
		}
		artifact, err := AuthenticateRuntimeArtifactAtContext(ctx, chain, types.Hash{4}, pin)
		if fault != "valid" && fault != "future-version" {
			if err == nil {
				t.Fatalf("%s admitted", fault)
			}
			if len(chain.runtimeMetadataArtifactCache().identityEntries) != 0 {
				t.Fatalf("%s poisoned strict cache", fault)
			}
			continue
		}
		if err != nil || artifact.Version != version || !chain.RuntimeArtifactCompatible(artifact) {
			t.Fatalf("%s: %+v %v", fault, artifact, err)
		}
		second, err := AuthenticateRuntimeArtifactAtContext(ctx, chain, types.Hash{5}, pin)
		if err != nil || second.BlockHash != (types.Hash{5}) || second.Metadata != artifact.Metadata || observations != 1 || metadataReads != 1 {
			t.Fatalf("compatible artifact not reused: %v observations=%d reads=%d", err, observations, metadataReads)
		}
		chain.Meta = artifact.Metadata
		chain.Runtime = &types.RuntimeVersion{SpecName: version.SpecName, SpecVersion: types.U32(version.SpecVersion), TransactionVersion: 1}
		if chain.CurrentRuntimeCompatibilityProfile() != ProvisionalRuntimeCompatibilityProfile {
			t.Fatal("actual signing domain lost profile")
		}
		chain.Runtime.SpecVersion = 461
		if chain.CurrentRuntimeCompatibilityProfile() != "" {
			t.Fatal("old signing domain inherited profile")
		}
	}
}

func TestProvisionalRuntimeCompatibilityActualSchedulePreservesEligibility(t *testing.T) {
	fixture, query := newValidatorScheduleTestFixture(t)
	identity := fixture.identity
	metadata, _, encoded := provisionalRuntimeMetadataTest(t)
	identity.metadata, identity.metadataHex = metadata, encoded
	identity.version.SpecVersion = 463
	allowed, _ := ReviewedRuntimeArtifact(RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 461, TransactionVersion: 1, StateVersion: 1})
	identity.allowed = []RuntimeArtifactIdentity{allowed}
	identity.keyNames = map[string]string{}
	netuid := binary.LittleEndian.AppendUint16(nil, query.Netuid)
	uid := binary.LittleEndian.AppendUint16(nil, identity.query.UID)
	for _, field := range []struct {
		name string
		args [][]byte
	}{
		{"SubnetworkN", [][]byte{netuid}}, {"Keys", [][]byte{netuid, uid}}, {"Uids", [][]byte{netuid, identity.hotkey[:]}}, {"Owner", [][]byte{identity.hotkey[:]}}, {"TotalHotkeyAlpha", [][]byte{identity.hotkey[:], netuid}}, {"ValidatorPermit", [][]byte{netuid}}, {"StakeThreshold", nil}, {"SubnetOwnerHotkey", [][]byte{netuid}}, {"SubnetEpochIndex", [][]byte{netuid}},
	} {
		key, err := types.CreateStorageKey(metadata, PalletName, field.name, field.args...)
		if err != nil {
			t.Fatal(err)
		}
		identity.keyNames[key.Hex()] = field.name
	}
	priorHook := identity.hook
	identity.hook = func(ctx context.Context, target any, method string, args ...any) (bool, error) {
		if method == "state_getRuntimeVersion" {
			return true, setRuntimeIdentityTestResult(target, map[string]any{"specName": "node-subtensor", "specVersion": 463, "transactionVersion": 1, "stateVersion": 1, "apis": []any{[]any{"0x8375104b299b74c5", 2}}})
		}
		if method == "chain_getBlockHash" {
			if result, ok := target.(*types.Hash); ok {
				*result = query.GenesisHash
				return true, nil
			}
		}
		return priorHook(ctx, target, method, args...)
	}
	if err := identity.chain.EnableProvisionalRuntimeCompatibility(query.GenesisHash, func(AuthenticatedRuntimeArtifact) error { return nil }); err != nil {
		t.Fatal(err)
	}
	beforeMetadata, beforeRuntime := identity.chain.Meta, identity.chain.Runtime
	result, err := ReadValidatorScheduleAtContext(identity.ctx, identity.chain, query, identity.allowed...)
	if err != nil || result.SubnetEpochIndex != 77 || result.Stake.TotalStakeRao != 150 || !result.Stake.MeetsNonSelfStakeAndPermit() || result.Stake.Identity.Runtime.Version.SpecVersion != 463 || fixture.runtimeCalls != 1 {
		t.Fatalf("actual463 schedule: %+v %v calls=%d", result, err, fixture.runtimeCalls)
	}
	if identity.chain.Meta != beforeMetadata || identity.chain.Runtime != beforeRuntime {
		t.Fatal("block-local schedule changed signing authority")
	}
}

func TestProvisionalRuntimeCompatibilitySourceSignsActualDomainAndRejectsRelabeling(t *testing.T) {
	metadata, hash, _ := provisionalRuntimeMetadataTest(t)
	version := RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 463, TransactionVersion: 1, StateVersion: 1}
	chain := &Chain{Meta: metadata, GenesisHash: types.Hash{8}, Runtime: &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: 463, TransactionVersion: 1}}
	if err := chain.EnableProvisionalRuntimeCompatibility(chain.GenesisHash, func(AuthenticatedRuntimeArtifact) error { return nil }); err != nil {
		t.Fatal(err)
	}
	identity := RuntimeArtifactIdentity{Version: version, CodeHash: types.Hash{9}.Hex(), MetadataHash: hash}
	chain.provisionalRuntime.artifacts[identity] = AuthenticatedRuntimeArtifact{BlockHash: types.Hash{4}, GenesisHash: chain.GenesisHash, Version: version, CodeHash: identity.CodeHash, MetadataHash: hash, Metadata: metadata, CompatibilityProfile: ProvisionalRuntimeCompatibilityProfile}
	for _, mecid := range []*uint8{nil, new(uint8)} {
		prepared, key := sourcePreparedTest(t)
		prepared.Mecid = mecid
		prepared.SourceCommitment.RuntimeSpec = 463
		prepared.SourceCommitment.CompatibilityProfile = ProvisionalRuntimeCompatibilityProfile
		signSourcePreparedTest(t, prepared, key, nil)
		if err := chain.ValidatePreparedSource(prepared); err != nil {
			t.Fatalf("actual metadata signed domain: %v", err)
		}
		strict := *chain
		strict.provisionalRuntime = nil
		if err := strict.ValidatePreparedSource(prepared); err == nil {
			t.Fatal("strict chain admitted provisional source")
		}
		prepared.SourceCommitment.RuntimeSpec = 464
		if _, err := prepared.Validate(); err == nil {
			t.Fatal("signature relabeled to successor runtime")
		}
		prepared.SourceCommitment.RuntimeSpec = 463
		prepared.SourceCommitment.CompatibilityProfile = ""
		if _, err := prepared.Validate(); err == nil {
			t.Fatal("provisional signature acquired strict authority")
		}
	}
}
