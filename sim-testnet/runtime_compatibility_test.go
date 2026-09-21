package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcrpc "github.com/centrifuge/go-substrate-rpc-client/v4/rpc"
	gsrpcstate "github.com/centrifuge/go-substrate-rpc-client/v4/rpc/state"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	gsrpcblock "github.com/centrifuge/go-substrate-rpc-client/v4/types/block"
	"github.com/urfoundation/sn/crv4"
)

// A synthetic next runtime retains the reviewed consumed interface while
// changing its signing domain. It must remain newer after a release upgrade.
const provisionalRuntimeSuccessorTestSpec = reviewedRuntimeSpecVersion + 1

func provisionalRuntimeConfigTest(t *testing.T) *ResolvedConfig {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.provisionalResume = &provisionalResumeState{RecordPath: filepath.Join(t.TempDir(), "provenance.json"), Record: &provisionalResumeRecord{Schema: "urnetwork-sim-provisional-resume-v1", Provisional: true, FinalAcceptance: false, ConfigHash: cfg.ConfigHash, DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: "0x" + strings.Repeat("42", 32)}}
	return cfg
}

func provisionalRuntimeFixtureTest(t *testing.T) (string, *types.Metadata) {
	t.Helper()
	path := "../crv4/runtime-profile-v1.scale.gz.base64"
	encoded, err := os.ReadFile(path)
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
	raw, err := io.ReadAll(io.LimitReader(reader, 400000))
	if err != nil {
		t.Fatal(err)
	}
	hex := fmt.Sprintf("0x%x", raw)
	metadata, _, err := crv4.DecodeRuntimeMetadata(hex)
	if err != nil {
		t.Fatal(err)
	}
	return hex, metadata
}

func provisionalRuntimeChainTest(t *testing.T, cfg *ResolvedConfig) *crv4.Chain {
	t.Helper()
	current, _ := provisionalRuntimeFixtureTest(t)
	prior := current
	genesis := types.Hash{}
	if err := genesis.UnmarshalJSON([]byte(fmt.Sprintf("%q", testnetGenesis))); err != nil {
		t.Fatal(err)
	}
	client := &releaseRuntimeTestClient{callContext: func(ctx context.Context, target any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		version := uint32(provisionalRuntimeSuccessorTestSpec)
		code := types.Hash{9}.Hex()
		metadata := current
		if len(args) != 0 && args[len(args)-1] == (types.Hash{1}).Hex() {
			version = cfg.Public.Chain.ExpectedRuntimeSpec
			code = cfg.Release.Runtime.CodeHash
			metadata = prior
		}
		switch method {
		case "chain_getFinalizedHead":
			return setReleaseRuntimeTestResult(target, types.Hash{2}.Hex())
		case "chain_getHeader":
			result, ok := target.(*types.Header)
			if !ok {
				return fmt.Errorf("header type %T", target)
			}
			*result = types.Header{Number: 200}
			return nil
		case "chain_getBlockHash":
			result, ok := target.(*types.Hash)
			if !ok {
				return fmt.Errorf("genesis type %T", target)
			}
			*result = genesis
			return nil
		case "state_getRuntimeVersion":
			return setReleaseRuntimeTestResult(target, map[string]any{"specName": "node-subtensor", "specVersion": version, "transactionVersion": 1, "stateVersion": 1, "apis": []any{[]any{"0x8375104b299b74c5", 2}}})
		case "state_getStorageHash":
			return setReleaseRuntimeTestResult(target, code)
		case "state_getMetadata":
			return setReleaseRuntimeTestResult(target, metadata)
		case "state_getStorage":
			return setReleaseRuntimeTestResult(target, "0x0300")
		default:
			return fmt.Errorf("unexpected or mutating RPC %s", method)
		}
	}}
	dialMetadata := types.NewMetadataV14()
	dialMetadata.MagicNumber = types.MagicNumber
	return &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client, RPC: &gsrpcrpc.RPC{State: gsrpcstate.NewState(client)}}, GenesisHash: genesis, Meta: dialMetadata, Runtime: &types.RuntimeVersion{SpecName: "node-subtensor", SpecVersion: types.U32(cfg.Public.Chain.ExpectedRuntimeSpec), TransactionVersion: 1}}
}

func TestProvisionalRuntimeCompatibilityCurrentHistoricalAndDurableEvidence(t *testing.T) {
	cfg := provisionalRuntimeConfigTest(t)
	chain := provisionalRuntimeChainTest(t, cfg)
	originalMetadata, originalRuntime := chain.Meta, chain.Runtime
	for _, read := range []func(context.Context, *crv4.Chain, *ResolvedConfig, types.Hash) (authenticatedRuntimeMetadata, error){readAuthenticatedRuntimeMetadataAtContext, readReleaseHistoryRuntimeMetadataAtContext} {
		current, err := read(t.Context(), chain, cfg, types.Hash{2})
		if err != nil || current.Version.SpecVersion != provisionalRuntimeSuccessorTestSpec || current.CompatibilityProfile != crv4.ProvisionalRuntimeCompatibilityProfile {
			t.Fatalf("compatible successor runtime: %+v %v", current, err)
		}
		prior, err := read(t.Context(), chain, cfg, types.Hash{1})
		if err != nil || prior.Version.SpecVersion != cfg.Public.Chain.ExpectedRuntimeSpec || prior.CompatibilityProfile != "" || prior.CodeHash != cfg.Release.Runtime.CodeHash {
			t.Fatalf("historical reviewed runtime: %+v %v", prior, err)
		}
	}
	if chain.Meta != originalMetadata || chain.Runtime != originalRuntime {
		t.Fatal("read rewrote shared signing authority")
	}
	dir := filepath.Join(filepath.Dir(cfg.provisionalResume.RecordPath), "runtime-compatibility")
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("durable observation: %v count=%d", err, len(files))
	}
	raw, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	if record["genesis_hash"] != testnetGenesis || record["provisional"] != true || record["final_acceptance"] != false {
		t.Fatalf("observation lost domain: %s", raw)
	}
}

func TestProvisionalRuntimeCompatibilityRejectsStrictAndInvalidApproval(t *testing.T) {
	for _, fault := range []string{"strict", "schema", "plan", "config", "deployment", "final", "chain", "relative-path", "write-failure"} {
		cfg := provisionalRuntimeConfigTest(t)
		chain := provisionalRuntimeChainTest(t, cfg)
		switch fault {
		case "strict":
			cfg.provisionalResume = nil
		case "schema":
			cfg.provisionalResume.Record.Schema = "foreign"
		case "plan":
			cfg.provisionalResume.Record.PlanHash = "invalid"
		case "config":
			cfg.provisionalResume.Record.ConfigHash = "other"
		case "deployment":
			cfg.provisionalResume.Record.DeploymentID = "other"
		case "final":
			cfg.provisionalResume.Record.FinalAcceptance = true
		case "chain":
			cfg.ChainID = 1
		case "relative-path":
			cfg.provisionalResume.RecordPath = "relative/provenance.json"
		case "write-failure":
			if err := os.WriteFile(filepath.Join(filepath.Dir(cfg.provisionalResume.RecordPath), "runtime-compatibility"), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := readAuthenticatedRuntimeMetadataAtContext(t.Context(), chain, cfg, types.Hash{2}); err == nil {
			t.Fatalf("%s admitted the compatible successor", fault)
		}
	}
}

func TestProvisionalRuntimeCompatibilityFinalizedStorageUsesPrivateView(t *testing.T) {
	cfg := provisionalRuntimeConfigTest(t)
	chain := provisionalRuntimeChainTest(t, cfg)
	originalMetadata, originalRuntime := chain.Meta, chain.Runtime
	manager := &SubstrateManager{chain: chain, cfg: cfg}
	view, hash, number, err := manager.finalizedManagerContext(t.Context())
	if err != nil || view == manager || view.chain == chain || view.chain.Meta == originalMetadata || view.chain.Runtime.SpecVersion != types.U32(provisionalRuntimeSuccessorTestSpec) || hash != (types.Hash{2}) || number != 200 {
		t.Fatalf("private finalized view: %+v %s %d %v", view, hash.Hex(), number, err)
	}
	// The deliberately empty dial metadata has no SubnetworkN. The original
	// caller built a key before authenticating its head and failed here.
	count, err := manager.UIDCount()
	if err != nil || count != 3 {
		t.Fatalf("block-local storage decode: %d %v", count, err)
	}
	if chain.Meta != originalMetadata || chain.Runtime != originalRuntime {
		t.Fatal("storage read mutated shared runtime")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, _, err := manager.finalizedManagerContext(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

// Compatible decoding does not authorize a different native signature domain.
func TestProvisionalRuntimeCompatibilityNativeSigningArtifactFence(t *testing.T) {
	cfg := provisionalRuntimeConfigTest(t)
	chain := provisionalRuntimeChainTest(t, cfg)
	prepared, err := readAuthenticatedRuntimeMetadataAtContext(t.Context(), chain, cfg, types.Hash{2})
	if err != nil {
		t.Fatal(err)
	}
	current := prepared
	current.FinalizedHash = types.Hash{3}
	if err := requireSameNativeSigningRuntime(prepared, current); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"spec", "transaction", "state", "name", "code", "metadata"} {
		current = prepared
		switch fault {
		case "spec":
			current.Version.SpecVersion++
		case "transaction":
			current.Version.TransactionVersion++
		case "state":
			current.Version.StateVersion++
		case "name":
			current.Version.SpecName = "foreign"
		case "code":
			current.CodeHash = types.Hash{8}.Hex()
		case "metadata":
			current.MetadataHash = types.Hash{8}.Hex()
		}
		if err := requireSameNativeSigningRuntime(prepared, current); err == nil {
			t.Fatalf("%s allowed stale native bytes", fault)
		}
	}
}

// Exercise the real no-receipt recovery path across a compatible successor
// boundary. It must reject old bytes before fee quotation or transport submit.
func TestProvisionalRuntimeCompatibilityNativeRebroadcastRejectsUpgrade(t *testing.T) {
	cfg := provisionalRuntimeConfigTest(t)
	chain := provisionalRuntimeChainTest(t, cfg)
	client := chain.API.Client.(*releaseRuntimeTestClient)
	original := client.callContext
	priorHash, currentHash := types.Hash{1}.Hex(), types.Hash{2}.Hex()
	headerCalls := 0
	var blockNumbers []uint64
	var blockHashes []string
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		switch method {
		case "chain_getHeader":
			if len(args) != 1 || args[0] != currentHash {
				return fmt.Errorf("rebroadcast header arguments differ: %v", args)
			}
			result, ok := target.(*types.Header)
			if !ok {
				return fmt.Errorf("rebroadcast header result type %T", target)
			}
			headerCalls++
			*result = types.Header{Number: 201}
			return nil
		case "chain_getBlockHash":
			if len(args) != 1 {
				return fmt.Errorf("rebroadcast block-hash arguments differ: %v", args)
			}
			number, ok := args[0].(uint64)
			if !ok {
				return fmt.Errorf("rebroadcast block number differs: %v", args[0])
			}
			if number == 0 {
				return original(ctx, target, method, args...)
			}
			if number != 200 && number != 201 {
				return fmt.Errorf("rebroadcast block number differs: %v", args[0])
			}
			blockNumbers = append(blockNumbers, number)
			if number == 200 {
				return setReleaseRuntimeTestResult(target, priorHash)
			}
			return setReleaseRuntimeTestResult(target, currentHash)
		case "chain_getBlock":
			if len(args) != 1 || args[0] != priorHash && args[0] != currentHash {
				return fmt.Errorf("rebroadcast block arguments differ: %v", args)
			}
			result, ok := target.(*gsrpcblock.SignedBlock)
			if !ok {
				return fmt.Errorf("rebroadcast block result type %T", target)
			}
			blockHashes = append(blockHashes, args[0].(string))
			*result = gsrpcblock.SignedBlock{}
			return nil
		}
		return original(ctx, target, method, args...)
	}
	manager := &SubstrateManager{cfg: cfg, chain: chain}
	_, _, err := manager.watchRaw(t.Context(), "plan", Action{ID: "action"}, []byte{1}, types.Hash{7}, 200, priorHash, false)
	if headerCalls != 2 || !slices.Equal(blockNumbers, []uint64{200, 201, 200}) || !slices.Equal(blockHashes, []string{priorHash, currentHash}) {
		t.Fatalf("rebroadcast recovery flow headers=%d numbers=%v hashes=%v", headerCalls, blockNumbers, blockHashes)
	}
	if err == nil || !strings.Contains(err.Error(), "native runtime changed across signing/recovery") {
		t.Fatalf("old native bytes reached a later boundary: %v", err)
	}
}
