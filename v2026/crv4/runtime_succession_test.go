// Successor admission is exercised with the real public mainnet runtime 473
// and 475 metadata over scripted read-only transcripts. No key, approval or
// network is involved; no fixture asserts an admission verdict.
package crv4

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// Public mainnet artifacts, identified in crv4/testdata/runtime-successor-metadata.md.
var (
	runtimeSuccession473 = RuntimeArtifactIdentity{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 473, TransactionVersion: 1, StateVersion: 1},
		CodeHash: "0x7773f5c0a6d6e9ea9ff347edcc491246eec08a5cf441d964ee96f40d7fa65a08", MetadataHash: "0xa97219740ed3b034a06463c783cd5b794788652e0692c1edb03eed34c8968172"}
	runtimeSuccession475 = RuntimeArtifactIdentity{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 475, TransactionVersion: 1, StateVersion: 1},
		CodeHash: "0x557634c8c31bc639ea6552e297dcfb781cd7d9bdd491a5352c151305db33d3a0", MetadataHash: "0x983cfdabc62b0c6b08faafb47f24303999236e0022598d1b5a3ec70fbde895ff"}
)

// Decodes one gzip/base64 fixture and checks its exact raw-byte digest.
func runtimeSuccessionMetadataTest(t *testing.T, path, expectedHash string) (*types.Metadata, string) {
	t.Helper()
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
	raw, err := io.ReadAll(io.LimitReader(reader, 1024*1024))
	if err := errors.Join(err, reader.Close()); err != nil {
		t.Fatal(err)
	}
	wire := fmt.Sprintf("0x%x", raw)
	metadata, hash, err := DecodeRuntimeMetadata(wire)
	if err != nil || hash != expectedHash {
		t.Fatalf("metadata fixture %s hash %s: %v", path, hash, err)
	}
	return metadata, wire
}

// One scripted runtime artifact per block. Every read names its exact block.
type runtimeSuccessionTestBlock struct {
	version  map[string]any
	code     string
	metadata string
}

type runtimeSuccessionTestFixture struct {
	stateLock sync.Mutex
	chain     *Chain
	blocks    map[types.Hash]*runtimeSuccessionTestBlock
	callKVs   map[string]int
	admitted  []RuntimeSuccessor
	refuse    error
}

var (
	runtimeSuccessionTestGenesis = types.Hash{0x2f, 0x05}
	runtimeSuccessionOldBlock    = types.Hash{0x47, 0x03}
	runtimeSuccessionNewBlock    = types.Hash{0x47, 0x05}
	runtimeSuccessionLaterBlock  = types.Hash{0x47, 0x06}
)

func runtimeSuccessionTestVersion(identity RuntimeArtifactIdentity, apiVersion int) map[string]any {
	version := identity.Version
	return map[string]any{"specName": version.SpecName, "specVersion": version.SpecVersion, "transactionVersion": version.TransactionVersion,
		"stateVersion": version.StateVersion, "apis": []any{[]any{"0xdf6acb689907609b", 5}, []any{"0x8375104b299b74c5", apiVersion}}}
}

// Block 473 runs the approved artifact; the two later blocks run 475.
func newRuntimeSuccessionTestFixture(t *testing.T) *runtimeSuccessionTestFixture {
	t.Helper()
	_, wire473 := runtimeSuccessionMetadataTest(t, "testdata/runtime473-metadata.scale.gz.base64", runtimeSuccession473.MetadataHash)
	_, wire475 := runtimeSuccessionMetadataTest(t, "testdata/runtime475-metadata.scale.gz.base64", runtimeSuccession475.MetadataHash)
	self := &runtimeSuccessionTestFixture{callKVs: map[string]int{}, blocks: map[types.Hash]*runtimeSuccessionTestBlock{
		runtimeSuccessionOldBlock:   {version: runtimeSuccessionTestVersion(runtimeSuccession473, 2), code: runtimeSuccession473.CodeHash, metadata: wire473},
		runtimeSuccessionNewBlock:   {version: runtimeSuccessionTestVersion(runtimeSuccession475, 2), code: runtimeSuccession475.CodeHash, metadata: wire475},
		runtimeSuccessionLaterBlock: {version: runtimeSuccessionTestVersion(runtimeSuccession475, 2), code: runtimeSuccession475.CodeHash, metadata: wire475},
	}}
	self.chain = &Chain{GenesisHash: runtimeSuccessionTestGenesis}
	self.chain.API = &gsrpc.SubstrateAPI{Client: &runtimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.callKVs[method]++
		if len(args) == 0 {
			return fmt.Errorf("successor Rpc %s names no exact block", method)
		}
		hash, err := types.NewHashFromHexString(fmt.Sprint(args[len(args)-1]))
		if err != nil {
			return err
		}
		block := self.blocks[hash]
		if block == nil {
			return fmt.Errorf("successor Rpc %s escaped its scripted blocks", method)
		}
		switch method {
		case "state_getRuntimeVersion":
			return setRuntimeIdentityTestResult(result, block.version)
		case "state_getStorageHash":
			if len(args) != 2 || args[0] != "0x3a636f6465" {
				return fmt.Errorf("unexpected code-hash query %v", args)
			}
			return setRuntimeIdentityTestResult(result, block.code)
		case "state_getMetadata":
			return setRuntimeIdentityTestResult(result, block.metadata)
		default:
			return fmt.Errorf("unexpected or mutating successor Rpc %s", method)
		}
	}}}
	return self
}

func (self *runtimeSuccessionTestFixture) enable(t *testing.T, approved ...RuntimeArtifactIdentity) {
	t.Helper()
	if err := self.chain.EnableRuntimeSuccession(runtimeSuccessionTestGenesis, ValidatorProducerRuntimeProfile, approved, func(successor RuntimeSuccessor) error {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if self.refuse != nil {
			return self.refuse
		}
		self.admitted = append(self.admitted, successor)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func (self *runtimeSuccessionTestFixture) calls(method string) int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.callKVs[method]
}

// Re-encodes a deliberately changed copy of the real 475 metadata.
func runtimeSuccessionChangedMetadata(t *testing.T, change func(*types.Metadata)) (string, string) {
	t.Helper()
	metadata, _ := runtimeSuccessionMetadataTest(t, "testdata/runtime475-metadata.scale.gz.base64", runtimeSuccession475.MetadataHash)
	change(metadata)
	encoded, err := codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, hash, err := DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, hash
}

// The real 473-to-475 upgrade keeps every producer-consumed interface. Only an
// installed policy naming the approved anchor admits it, as its own exact
// strict artifact with a producer purpose proof that names that anchor.
func TestRuntimeSuccessionAdmitsRealMainnetSuccessorOnlyWithPolicy(t *testing.T) {
	fixture := newRuntimeSuccessionTestFixture(t)
	if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, runtimeSuccessionNewBlock, runtimeSuccession473); err == nil || !strings.Contains(err.Error(), "unreviewed identity") {
		t.Fatalf("exact pin admitted an upgrade without a successor policy: %v", err)
	}
	if fixture.chain.RuntimeSuccessionEnabled() {
		t.Fatal("successor policy appeared without installation")
	}
	fixture.enable(t, runtimeSuccession473)
	if !fixture.chain.RuntimeSuccessionEnabled() || fixture.chain.ProvisionalRuntimeCompatibilityEnabled() {
		t.Fatal("successor policy installation changed its scope")
	}
	exact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, runtimeSuccessionOldBlock, runtimeSuccession473)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := exact.RuntimeSuccessorOf(); ok || exact.CodeHash != runtimeSuccession473.CodeHash || len(fixture.admitted) != 0 {
		t.Fatal("approved exact artifact was relabeled as a successor")
	}
	metadataReads := fixture.calls("state_getMetadata")
	successor, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, runtimeSuccessionNewBlock, runtimeSuccession473)
	if err != nil {
		t.Fatal(err)
	}
	approved, ok := successor.RuntimeSuccessorOf()
	if !ok || approved != runtimeSuccession473 || successor.CompatibilityProfile != "" ||
		(RuntimeArtifactIdentity{Version: successor.Version, CodeHash: successor.CodeHash, MetadataHash: successor.MetadataHash}) != runtimeSuccession475 {
		t.Fatalf("successor lost its own exact identity or approved anchor: %+v", successor)
	}
	if len(fixture.admitted) != 1 || fixture.admitted[0].Artifact != runtimeSuccession475 || fixture.admitted[0].Approved != runtimeSuccession473 ||
		fixture.admitted[0].BlockHash != runtimeSuccessionNewBlock || fixture.admitted[0].Metadata == nil {
		t.Fatalf("caller consumed-storage check did not see the exact successor: %+v", fixture.admitted)
	}
	if reads := fixture.calls("state_getMetadata") - metadataReads; reads != 1 {
		t.Fatalf("first successor observation read metadata %d times", reads)
	}
	if err := ValidateValidatorProducerRuntimeArtifactContext(t.Context(), fixture.chain, successor); err != nil {
		t.Fatal(err)
	}
	view := *fixture.chain
	if err := view.BindValidatorProducerRuntimeArtifactContext(t.Context(), successor); err != nil {
		t.Fatal(err)
	}
	if uint32(view.Runtime.SpecVersion) != 475 || view.Meta != successor.Metadata {
		t.Fatal("signing view did not bind the real successor runtime")
	}
	if err := view.ValidateValidatorProducerRuntimeSuccessor(runtimeSuccession473); err != nil {
		t.Fatal(err)
	}
	if err := view.ValidateValidatorProducerRuntime(runtimeSuccession473); err == nil {
		t.Fatal("successor proof satisfied an exact approved pin")
	}
	if err := view.ValidateValidatorProducerRuntimeSuccessor(runtimeSuccession475); err == nil {
		t.Fatal("successor proof named itself as its approved anchor")
	}
	// Later blocks of the same artifact reuse the admission without another
	// metadata read or caller check; exact version and code reads repeat.
	versionReads, codeReads, metadataReads := fixture.calls("state_getRuntimeVersion"), fixture.calls("state_getStorageHash"), fixture.calls("state_getMetadata")
	later, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, runtimeSuccessionLaterBlock, runtimeSuccession473)
	if err != nil {
		t.Fatal(err)
	}
	if anchor, ok := later.RuntimeSuccessorOf(); !ok || anchor != runtimeSuccession473 || later.MetadataHash != runtimeSuccession475.MetadataHash || len(fixture.admitted) != 1 {
		t.Fatal("repeated successor observation lost or repeated its admission")
	}
	if fixture.calls("state_getMetadata") != metadataReads || fixture.calls("state_getRuntimeVersion") <= versionReads || fixture.calls("state_getStorageHash") <= codeReads {
		t.Fatal("repeated successor authentication skipped exact reads or refetched metadata")
	}
	// An allowlist without the approved anchor keeps its exact meaning.
	if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, runtimeSuccessionNewBlock, RuntimeArtifactIdentity{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 470, TransactionVersion: 1, StateVersion: 1},
		CodeHash: types.Hash{0x70}.Hex(), MetadataHash: types.Hash{0x71}.Hex()}); err == nil || !strings.Contains(err.Error(), "unreviewed identity") {
		t.Fatalf("an unrelated exact pin inherited successor admission: %v", err)
	}
}

// Version order, encoding versions, every producer-consumed interface, the
// selective-metagraph API and the caller's own storage checks each refuse.
func TestRuntimeSuccessionRefusesNonSuccessorsAndChangedInterfaces(t *testing.T) {
	for _, fault := range []string{"older", "transaction-version", "storage-shape", "call-index", "signed-extension", "runtime-api", "caller-storage"} {
		t.Run(fault, func(t *testing.T) {
			fixture := newRuntimeSuccessionTestFixture(t)
			anchor, block, want := runtimeSuccession473, runtimeSuccessionNewBlock, "not an admitted successor"
			switch fault {
			case "older":
				anchor, block, want = runtimeSuccession475, runtimeSuccessionOldBlock, "not a successor"
			case "transaction-version":
				version := runtimeSuccessionTestVersion(runtimeSuccession475, 2)
				version["transactionVersion"] = 2
				fixture.blocks[block].version, want = version, "not a successor"
			case "storage-shape", "call-index", "signed-extension":
				fixture.blocks[block].metadata, _ = runtimeSuccessionChangedMetadata(t, func(metadata *types.Metadata) {
					if fault == "signed-extension" {
						extensions := metadata.AsMetadataV14.Extrinsic.SignedExtensions
						extensions[0], extensions[1] = extensions[1], extensions[0]
						return
					}
					for index := range metadata.AsMetadataV14.Pallets {
						pallet := &metadata.AsMetadataV14.Pallets[index]
						if pallet.Name != PalletName {
							continue
						}
						if fault == "storage-shape" {
							for itemIndex := range pallet.Storage.Items {
								if item := &pallet.Storage.Items[itemIndex]; item.Name == "Weights" {
									item.Type.AsMap.Hashers[0] = types.StorageHasherV10{IsBlake2_128Concat: true}
								}
							}
							continue
						}
						pallet.Index++
					}
				})
			case "runtime-api":
				fixture.blocks[block].version = runtimeSuccessionTestVersion(runtimeSuccession475, 3)
			case "caller-storage":
				fixture.refuse = errors.New("synthetic consumed owner storage changed")
			}
			fixture.enable(t, anchor)
			_, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, block, anchor)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s successor was not refused with %q: %v", fault, want, err)
			}
			if fault == "storage-shape" && !strings.Contains(err.Error(), "consumed interface storage/SubtensorModule.Weights changed") {
				t.Fatalf("changed storage refusal does not name the consumed item: %v", err)
			}
			if fault == "caller-storage" && !strings.Contains(err.Error(), "synthetic consumed owner storage changed") {
				t.Fatalf("caller storage refusal lost its cause: %v", err)
			}
			if len(fixture.admitted) != 0 {
				t.Fatal("refused successor reached admission")
			}
			// The exact approved artifact keeps working beside a refusal.
			if fault != "older" {
				if _, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, runtimeSuccessionOldBlock, anchor); err != nil {
					t.Fatalf("refused successor revoked the exact approved artifact: %v", err)
				}
			}
		})
	}
}

// Installation is explicit, complete, immutable and exclusive with testnet
// provisional compatibility in both orders.
func TestRuntimeSuccessionPolicyIsExplicitAndExclusive(t *testing.T) {
	admit := func(RuntimeSuccessor) error { return nil }
	fixture := newRuntimeSuccessionTestFixture(t)
	changedTransaction := runtimeSuccession473
	changedTransaction.Version.TransactionVersion = 2
	for name, install := range map[string]func() error{
		"profile": func() error {
			return fixture.chain.EnableRuntimeSuccession(runtimeSuccessionTestGenesis, ProvisionalRuntimeCompatibilityProfile, []RuntimeArtifactIdentity{runtimeSuccession473}, admit)
		},
		"genesis": func() error {
			return fixture.chain.EnableRuntimeSuccession(types.Hash{0x99}, ValidatorProducerRuntimeProfile, []RuntimeArtifactIdentity{runtimeSuccession473}, admit)
		},
		"anchors": func() error {
			return fixture.chain.EnableRuntimeSuccession(runtimeSuccessionTestGenesis, ValidatorProducerRuntimeProfile, nil, admit)
		},
		"encoding": func() error {
			return fixture.chain.EnableRuntimeSuccession(runtimeSuccessionTestGenesis, ValidatorProducerRuntimeProfile, []RuntimeArtifactIdentity{changedTransaction}, admit)
		},
		"duplicate": func() error {
			return fixture.chain.EnableRuntimeSuccession(runtimeSuccessionTestGenesis, ValidatorProducerRuntimeProfile, []RuntimeArtifactIdentity{runtimeSuccession473, runtimeSuccession473}, admit)
		},
		"admit-check": func() error {
			return fixture.chain.EnableRuntimeSuccession(runtimeSuccessionTestGenesis, ValidatorProducerRuntimeProfile, []RuntimeArtifactIdentity{runtimeSuccession473}, nil)
		},
	} {
		if err := install(); err == nil || fixture.chain.RuntimeSuccessionEnabled() {
			t.Fatalf("incomplete %s authority installed successor admission", name)
		}
	}
	fixture.enable(t, runtimeSuccession473)
	fixture.enable(t, runtimeSuccession473)
	if err := fixture.chain.EnableRuntimeSuccession(runtimeSuccessionTestGenesis, ValidatorProducerRuntimeProfile, []RuntimeArtifactIdentity{runtimeSuccession475}, admit); err == nil {
		t.Fatal("installed successor anchors changed after publication")
	}
	if err := fixture.chain.EnableProvisionalRuntimeCompatibility(runtimeSuccessionTestGenesis, func(AuthenticatedRuntimeArtifact) error { return nil }); err == nil || fixture.chain.ProvisionalRuntimeCompatibilityEnabled() {
		t.Fatal("testnet provisional compatibility joined mainnet successor admission")
	}
	provisional := newRuntimeSuccessionTestFixture(t)
	if err := provisional.chain.EnableProvisionalRuntimeCompatibility(runtimeSuccessionTestGenesis, func(AuthenticatedRuntimeArtifact) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := provisional.chain.EnableRuntimeSuccession(runtimeSuccessionTestGenesis, ValidatorProducerRuntimeProfile, []RuntimeArtifactIdentity{runtimeSuccession473}, admit); err == nil || provisional.chain.RuntimeSuccessionEnabled() {
		t.Fatal("mainnet successor admission joined testnet provisional compatibility")
	}
}

// A view bound under another connection's policy cannot present its successor
// proof as this connection's approved anchor, and generic binding revokes it.
func TestRuntimeSuccessionProofBelongsToItsConnection(t *testing.T) {
	fixture := newRuntimeSuccessionTestFixture(t)
	fixture.enable(t, runtimeSuccession473)
	successor, err := AuthenticateRuntimeArtifactAtContext(t.Context(), fixture.chain, runtimeSuccessionNewBlock, runtimeSuccession473)
	if err != nil {
		t.Fatal(err)
	}
	foreign := newRuntimeSuccessionTestFixture(t)
	foreign.enable(t, runtimeSuccession473)
	if err := foreign.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), successor); err == nil {
		t.Fatal("foreign connection bound another connection's successor proof")
	}
	view := *fixture.chain
	if err := view.BindValidatorProducerRuntimeArtifactContext(t.Context(), successor); err != nil {
		t.Fatal(err)
	}
	if err := view.BindRuntimeArtifact(successor); err != nil {
		t.Fatal(err)
	}
	if err := view.ValidateValidatorProducerRuntimeSuccessor(runtimeSuccession473); err == nil {
		t.Fatal("generic binding retained successor signing authority")
	}
	fabricated := successor
	fabricated.authenticationProof = nil
	if err := view.BindValidatorProducerRuntimeArtifactContext(t.Context(), fabricated); err == nil {
		t.Fatal("exported successor fields acquired producer authority without a proof")
	}
}
