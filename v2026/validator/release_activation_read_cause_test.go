//go:build linux || darwin

// Activation preparation and retained dual signatures traverse real native
// readers, ABI decoding, bounded HTTP retries and private setup-file custody.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The only fault controls replace physical HTTP reads or canonical hash data;
// no activation, runtime or eligibility verdict can be injected.
type releaseActivationReadFixture struct {
	setup        *releaseActivationSetup
	fault        string
	failedReads  int
	badCanonical bool
	block        uint64
	hash         common.Hash
	journal      common.Address
}

// Real historical metadata supplies native registration/stake/schedule reads.
// The HTTP transcript independently supplies exact coordinator ABI outputs.
func newReleaseActivationReadFixture(t *testing.T) *releaseActivationReadFixture {
	t.Helper()
	source := newReleaseHistoricalSourceTestFixture(t)
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x77})
	if err != nil {
		t.Fatal(err)
	}
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
	cfg := source.config
	cfg.SchemaVersion, cfg.DeploymentID, cfg.ValidatorID = 1, "synthetic-activation-read", 1
	cfg.ChainID, cfg.GenesisHash, cfg.StateDir = 945, source.native.genesis.Hex(), t.TempDir()
	cfg.EvidenceV2.Operators = []ReleaseEvidenceV2OperatorConfig{{NoID: 2}}
	if err := os.Chmod(cfg.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	deployment := ReleaseActivationDeploymentV2{DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, GenesisHash: [32]byte(source.native.genesis), Netuid: cfg.Netuid,
		Coordinator: [20]byte{0x11}, SettlementVault: [20]byte{0x22}, PolicyHash: [32]byte{0x33}}
	cfg.Coordinator, cfg.SettlementVault, cfg.PolicyHash = common.Address(deployment.Coordinator).Hex(), common.Address(deployment.SettlementVault).Hex(), attemptHex32(deployment.PolicyHash)
	self := &releaseActivationReadFixture{block: 200, hash: common.Hash{0x45}, journal: common.Address{0x46}}
	coordinator := stabi.NewSTCoordinator()
	coordinatorAbi, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	client := chainHTTPTestRPC(t, "http://activation-read.example", chainHTTPResponseLimit, func(http.RoundTripper) http.RoundTripper {
		return chainHTTPTestRoundTripper(func(request *http.Request) (*http.Response, error) {
			var call chainBatchRPCRequest
			if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
				return nil, err
			}
			var result any
			switch call.Method {
			case "eth_getBlockByNumber":
				var number string
				if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &number) != nil || number != "finalized" && number != hexutil.EncodeUint64(self.block) {
					return nil, errors.New("activation header read changed its exact snapshot")
				}
				if self.fault == "finalized" && number == "finalized" || self.fault == "canonical" && number != "finalized" {
					self.failedReads++
					return nil, context.DeadlineExceeded
				}
				hash := self.hash
				if self.badCanonical && number != "finalized" {
					hash[0] ^= 1
				}
				result = map[string]any{"number": hexutil.EncodeUint64(self.block), "hash": hash}
			case "eth_call", "eth_getCode":
				var selector gethrpc.BlockNumberOrHash
				if len(call.Params) != 2 || json.Unmarshal(call.Params[1], &selector) != nil || selector.BlockHash == nil || *selector.BlockHash != self.hash || !selector.RequireCanonical || selector.BlockNumber != nil {
					return nil, errors.New("activation view lost its canonical block")
				}
				if call.Method == "eth_getCode" {
					var target common.Address
					if json.Unmarshal(call.Params[0], &target) != nil || target != self.journal {
						return nil, errors.New("activation code target changed")
					}
					result = "0x600000"
					break
				}
				var arguments struct {
					To    common.Address `json:"to"`
					Input hexutil.Bytes  `json:"input"`
				}
				if json.Unmarshal(call.Params[0], &arguments) != nil || arguments.To != common.Address(deployment.Coordinator) {
					return nil, errors.New("activation view target changed")
				}
				method, err := coordinatorAbi.MethodById(arguments.Input)
				if err != nil {
					return nil, err
				}
				var value any
				var expected []byte
				switch method.Name {
				case "currentEpoch":
					expected, value = coordinator.PackCurrentEpoch(), big.NewInt(6)
				case "policyAt":
					expected, value = coordinator.PackPolicyAt(big.NewInt(7)), stabi.STCoordinatorPolicySnapshot{PolicyHash: deployment.PolicyHash, EffectiveEpoch: 7, EffectiveBlock: 210, EpochBlocks: 100, EpochDepositCapRao: big.NewInt(1000), CampaignDepositCapRao: big.NewInt(5000)}
				case "validatorEvidence":
					expected, value = coordinator.PackValidatorEvidence(), self.journal
				case "operatorAt":
					expected, value = coordinator.PackOperatorAt(big.NewInt(2), big.NewInt(7)), stabi.STCoordinatorOperatorVersion{Active: true, EffectiveEpoch: 7}
				default:
					return nil, fmt.Errorf("unexpected activation view %s", method.Name)
				}
				if !bytes.Equal(arguments.Input, expected) {
					return nil, errors.New("activation view calldata changed")
				}
				encoded, err := method.Outputs.Pack(value)
				if err != nil {
					return nil, err
				}
				result = hexutil.Encode(encoded)
			default:
				return nil, fmt.Errorf("unexpected activation RPC %s", call.Method)
			}
			return chainReadRetryResponse(t, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result}), nil
		})
	})
	chain := &ChainClient{client: ethclient.NewClient(client), coordinator: coordinator, contractAddr: common.Address(deployment.Coordinator), release: true}
	chain.readRetryHooks.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	self.setup = &releaseActivationSetup{cfg: &cfg, hotkey: hotkey, clientKeys: map[uint64]ed25519.PrivateKey{2: private}, chain: chain, native: source.native.chain,
		deployment: deployment, limit: cfg.EvidenceV2.Bounds.MaxControlBytes}
	return self
}

// A late exact-header read happens after real native eligibility but before
// signing or persistence. Its exhausted bounded read budget remains retryable.
func TestReleaseActivationPreparationReadPreservesTransportCause(t *testing.T) {
	fixture := newReleaseActivationReadFixture(t)
	fixture.fault = "canonical"
	prepared, err := fixture.setup.prepare(t.Context())
	if prepared != nil || !errors.Is(err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(err) || fixture.failedReads != chainReadMaximumAttempts {
		t.Fatalf("activation preparation read exhaustion became snapshot contradiction: %+v %v reads=%d", prepared, err, fixture.failedReads)
	}
	path, _ := ReleaseActivationSetupPaths(fixture.setup.cfg)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed preparation published a setup file: %v", err)
	}
	fixture.fault = ""
	prepared, err = fixture.setup.prepare(t.Context())
	if err != nil || prepared == nil || len(prepared.Members) != 1 || len(prepared.Members[0].VPKSignature) != 0 || len(prepared.Members[0].HotkeySignature) != 0 {
		t.Fatalf("recovered real preparation failed or signed before its owner: %v", err)
	}
	fixture.badCanonical = true
	if result, err := fixture.setup.prepare(t.Context()); result != nil || err == nil || RetryableEvidenceTransportError(err) {
		t.Fatalf("successful contradictory activation header became retryable: %+v %v", result, err)
	}
}

// Retained dual signatures survive both physical read failures. Each retry
// reopens the real private file and authenticates every original member.
func TestReleaseActivationRetainedReadPreservesSignedWork(t *testing.T) {
	fixture := newReleaseActivationReadFixture(t)
	prepared, err := fixture.setup.prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.setup.sign(prepared); err != nil {
		t.Fatal(err)
	}
	path, _ := ReleaseActivationSetupPaths(fixture.setup.cfg)
	before, err := writeReleaseActivationSetupV2(t.Context(), path, prepared, fixture.setup.limit)
	if err != nil {
		t.Fatal(err)
	}
	for _, selected := range []string{"finalized", "canonical"} {
		fixture.fault, fixture.failedReads = selected, 0
		retained, _, missing, err := fixture.setup.readPrepared(t.Context())
		if retained != nil || missing || !errors.Is(err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(err) || fixture.failedReads != chainReadMaximumAttempts {
			t.Fatalf("retained %s activation read failure became finality/canonical contradiction: %+v %t %v reads=%d", selected, retained, missing, err, fixture.failedReads)
		}
		fixture.fault = ""
		reopened := *fixture.setup
		retained, raw, missing, err := reopened.readPrepared(t.Context())
		if err != nil || missing || !bytes.Equal(raw, before) || !reflect.DeepEqual(retained, prepared) {
			t.Fatalf("%s activation recovery replaced original signed custody: %v", selected, err)
		}
	}
}
