//go:build linux || darwin

// Real signatures, private files and HTTP/RPC transports exercise the bounded
// observer. Faults change source data or hold transport, never verifier verdicts.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

// Local HTTP sources combine the existing genuine activation and key-history
// oracles. Mutation flags are atomic; all other inputs freeze before reads.
type productionBootstrapObservationFixture struct {
	*releaseBootstrapV2TestFixture
	t        *testing.T
	inputs   []productionBootstrapPublicInput
	native   ProductionBootstrapNativePoint
	evm      ProductionBootstrapEvmPoint
	artifact ReleaseMeasurementArtifact
	apiCalls atomic.Int64
	reorg    atomic.Bool
	fault    string
	entered  chan struct{}
	release  chan struct{}
	left     chan struct{}
}

// Every new private directory is explicitly chmodded so umask cannot alter
// this fixture's evidence/credential ownership contract.
func newProductionBootstrapObservationFixture(t *testing.T, fault string) *productionBootstrapObservationFixture {
	t.Helper()
	base := newReleaseBootstrapV2TestFixture(t)
	activation := base.contexts[0].Activation
	f := &productionBootstrapObservationFixture{releaseBootstrapV2TestFixture: base, t: t, fault: fault, entered: make(chan struct{}), release: make(chan struct{}), left: make(chan struct{})}
	f.native = ProductionBootstrapNativePoint{Block: 101, Hash: [32]byte{0x51}, Epoch: 8, Hotkey: activation.Hotkey}
	f.evm = ProductionBootstrapEvmPoint{Block: base.providers[0].block, Hash: base.providers[0].blockHash}
	f.artifact = ReleaseMeasurementArtifact{DeploymentID: base.cfg.DeploymentID, ChainID: base.cfg.ChainID, GenesisHash: base.cfg.GenesisHash, Netuid: base.cfg.Netuid, Coordinator: base.cfg.Coordinator, SettlementVault: base.cfg.SettlementVault, PolicyHash: base.cfg.PolicyHash, SettlementEpoch: activation.Domain.Epoch, EVMSnapshotBlock: f.evm.Block, EVMSnapshotHash: attemptHex32(f.evm.Hash)}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	for index := range base.cfg.Operators {
		i := index
		clientId := connect.Id{byte(0x81 + i)}
		token, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims{"client_id": clientId.String(), "device_id": connect.Id{byte(0x91 + i)}.String()}).SignedString(gojwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, fmt.Sprintf("client-%d.jwt", i))
		if err := os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		base.cfg.Operators[i].ClientJWTFile = path
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer r.Body.Close()
			f.apiCalls.Add(1)
			if r.Method != http.MethodPost || r.URL.Path != "/sn/client-key/observation" || r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "wrong synthetic route or credential", 400)
				return
			}
			var body struct {
				ClientId string `json:"client_id"`
				Request  []byte `json:"request"`
			}
			var request protocol.ClientKeyObservationRequest
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&body) != nil || json.Unmarshal(body.Request, &request) != nil || body.ClientId != clientId.String() || request.ClientID != [16]byte(clientId) {
				http.Error(w, "wrong synthetic request", 400)
				return
			}
			if f.fault == "cancel" && i == 0 {
				close(f.entered)
				select {
				case <-r.Context().Done():
				case <-f.release:
				}
				close(f.left)
				return
			}
			if f.fault == "nonce" {
				request.Nonce[0] ^= 1
			}
			if f.fault == "native" {
				request.NativeHash[0] ^= 1
			}
			if f.fault == "evm" {
				request.DecisionBoundary.Hash[0] ^= 1
			}
			if f.fault == "hotkey" {
				request.ValidatorHotkey[0] ^= 1
			}
			domain := protocol.ClientKeyHistoryDomain{ChainID: base.cfg.ChainID, GenesisHash: common.HexToHash(base.cfg.GenesisHash), Netuid: base.cfg.Netuid, Coordinator: common.HexToAddress(base.cfg.Coordinator), SettlementVault: common.HexToAddress(base.cfg.SettlementVault), DeploymentIDHash: sha256.Sum256([]byte(base.cfg.DeploymentID)), PolicyHash: common.HexToHash(base.cfg.PolicyHash), NoID: base.cfg.Operators[i].NoID}
			key := base.contexts[i].Activation.VPK
			if f.fault == "key" && i == 1 {
				key[0] ^= 1
			}
			if f.fault == "absent-key" {
				key = [32]byte{}
			}
			response := releaseClientKeyTestResponse(t, base.cfg.DeploymentID, domain, request, [][32]byte{key})
			if f.fault == "oversize" {
				response = bytes.Repeat([]byte(" "), productionBootstrapMaximumResponseBytes+1)
			}
			if f.fault == "closing-reorg" && i == 1 {
				f.reorg.Store(true)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(response)
		}))
		t.Cleanup(api.Close)
		base.cfg.Operators[i].APIURL = api.URL
		// These files are never needed by the read-only proof/key observer.
		if err := os.Remove(base.cfg.Operators[i].ClientKeySeedFile); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(base.cfg.EvidenceV2.Operators[i].History.Path); err != nil {
			t.Fatal(err)
		}
	}
	inputs, err := readProductionBootstrapPublicInputs(t.Context(), &base.cfg, activation.Hotkey)
	if err != nil {
		t.Fatal(err)
	}
	f.inputs = inputs
	server := gethrpc.NewServer()
	if err := server.RegisterName("eth", f); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterName("chain", f); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(func() { httpServer.Close(); server.Stop() })
	f.chain, err = DialReleaseChainContext(t.Context(), []string{httpServer.URL}, common.HexToAddress(base.cfg.Coordinator))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.chain.Close)
	return f
}

// Native genesis is a separate concrete RPC fact, not the response's domain.
func (self *productionBootstrapObservationFixture) GetBlockHash(ctx context.Context, block uint64) (common.Hash, error) {
	if block != 0 {
		return common.Hash{}, errors.New("synthetic native lookup is not genesis")
	}
	return common.HexToHash(self.cfg.GenesisHash), ctx.Err()
}

// The numeric closing lookup can contradict earlier finalized reads exactly
// after the last API response, without replacing the real verifier.
func (self *productionBootstrapObservationFixture) GetBlockByNumber(ctx context.Context, block gethrpc.BlockNumber, full bool) (map[string]any, error) {
	if block == gethrpc.FinalizedBlockNumber {
		return self.releaseBootstrapV2TestFixture.GetBlockByNumber(ctx, block, full)
	}
	if full || block != gethrpc.BlockNumber(self.evm.Block) {
		return nil, errors.New("unexpected synthetic canonical height")
	}
	hash := self.evm.Hash
	if self.reorg.Load() {
		hash[0] ^= 1
	}
	return map[string]any{"number": hexutil.EncodeUint64(self.evm.Block), "hash": common.Hash(hash)}, ctx.Err()
}

// Actual ABI publication/companion views come from the activation oracle;
// policy/current operator root views use the independent key-history oracle.
func (self *productionBootstrapObservationFixture) Call(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	if encoded, err := self.releaseBootstrapV2TestFixture.Call(ctx, call, selector); err == nil {
		return encoded, nil
	}
	first, err := json.Marshal(map[string]any{"to": common.BytesToAddress(call["to"]), "input": call["input"]})
	if err != nil {
		return nil, err
	}
	second, err := json.Marshal(selector)
	if err != nil {
		return nil, err
	}
	value, handled, err := releaseHeadV2ClientKeyRPC(&self.artifact, chainBatchRPCRequest{Method: "eth_call", Params: []json.RawMessage{first, second}})
	if err != nil || !handled {
		return nil, errors.Join(errors.New("synthetic current authority view unavailable"), err)
	}
	encoded, ok := value.(string)
	if !ok {
		return nil, errors.New("synthetic current authority encoding differs")
	}
	raw, err := hexutil.Decode(encoded)
	if err != nil {
		return nil, err
	}
	parsed, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(call["input"], parsed.Methods["operatorAt"].ID) && (self.fault == "root" || self.fault == "inactive") {
		operator, err := stabi.NewSTCoordinator().UnpackOperatorAt(raw)
		if err != nil {
			return nil, err
		}
		if self.fault == "root" {
			operator.RootSigner = common.Address{0x88}
		} else {
			operator.Active = false
		}
		return parsed.Methods["operatorAt"].Outputs.Pack(operator)
	}
	return raw, nil
}

// The full real chain/API path observes both operators without seed/history
// files. Repeating the same point must create independently signed nonces.
func TestProductionBootstrapObservationAuthenticatesBothOperators(t *testing.T) {
	f := newProductionBootstrapObservationFixture(t, "")
	first, err := observeProductionBootstrapOperators(t.Context(), &f.cfg, f.inputs, f.native, f.evm, f.chain)
	if err != nil || first == nil || len(first.Operators) != 2 || f.apiCalls.Load() != 2 {
		t.Fatal("complete current observation refused", first, err)
	}
	second, err := observeProductionBootstrapOperators(t.Context(), &f.cfg, f.inputs, f.native, f.evm, f.chain)
	if err != nil || second == nil || f.apiCalls.Load() != 4 {
		t.Fatal("fresh repeated observation refused", err)
	}
	root := crypto.PubkeyToAddress(releaseClientKeyTestSigner(t, "12").PublicKey)
	for i, operator := range first.Operators {
		digest, _ := f.contexts[i].Activation.Digest()
		if operator.NoId != f.cfg.Operators[i].NoID || operator.ActivationHash != attemptHex32(digest) || operator.ClientKey != attemptHex32(f.contexts[i].Activation.VPK) || operator.RootSigner != strings.ToLower(root.Hex()) || operator.ClientKeyGeneration != 1 || operator.PublishedBlock == 0 || operator.ObservationNonce == second.Operators[i].ObservationNonce || operator.ClientKeyResponseHash == second.Operators[i].ClientKeyResponseHash {
			t.Fatalf("authenticated projection differs: %+v", operator)
		}
	}
}

// Signed responses still fail if any independently supplied clock, nonce,
// identity, presence or public key differs. The complete census is atomic.
func TestProductionBootstrapObservationRejectsSubstitutedResponses(t *testing.T) {
	for _, fault := range []string{"nonce", "native", "evm", "hotkey", "key", "absent-key", "oversize", "root", "inactive"} {
		f := newProductionBootstrapObservationFixture(t, fault)
		got, err := observeProductionBootstrapOperators(t.Context(), &f.cfg, f.inputs, f.native, f.evm, f.chain)
		if got != nil || err == nil {
			t.Fatal("substituted operator response escaped", fault, got, err)
		}
		if fault == "key" && f.apiCalls.Load() != 2 {
			t.Fatal("late second-member control did not reach both real APIs")
		}
	}
}

// Earlier canonical success cannot survive a contradictory final lookup.
func TestProductionBootstrapObservationRejectsClosingCanonicalChange(t *testing.T) {
	f := newProductionBootstrapObservationFixture(t, "closing-reorg")
	got, err := observeProductionBootstrapOperators(t.Context(), &f.cfg, f.inputs, f.native, f.evm, f.chain)
	if got != nil || err == nil || !strings.Contains(err.Error(), "anchor changed") || f.apiCalls.Load() != 2 {
		t.Fatal("closing canonical contradiction accepted", got, err)
	}
}

// A transport barrier proves cancellation at the actual authenticated API
// request. Cleanup releases the source even if an earlier assertion fails.
func TestProductionBootstrapObservationCancellationDiscardsAndJoins(t *testing.T) {
	f := newProductionBootstrapObservationFixture(t, "cancel")
	ctx, cancel := context.WithCancel(t.Context())
	type outcome struct {
		observation *ProductionBootstrapObservation
		err         error
	}
	done := make(chan outcome, 1)
	joined := false
	t.Cleanup(func() {
		cancel()
		close(f.release)
		if !joined {
			<-done
		}
	})
	go func() {
		value, err := observeProductionBootstrapOperators(ctx, &f.cfg, f.inputs, f.native, f.evm, f.chain)
		done <- outcome{observation: value, err: err}
	}()
	select {
	case <-f.entered:
	case early := <-done:
		joined = true
		t.Fatal("observer ended before API barrier", early.err)
	case <-t.Context().Done():
		t.Fatal("test cancelled before API barrier")
	}
	cancel()
	got := <-done
	joined = true
	<-f.left
	if got.observation != nil || !errors.Is(got.err, context.Canceled) || f.apiCalls.Load() != 1 {
		t.Fatal("cancelled observation escaped or reached next operator", got, f.apiCalls.Load())
	}
}

// Pinned public files authenticate both signers before any network operation.
// Removing the secret/history files above provides the positive no-read control.
func TestProductionBootstrapPublicInputsRejectSignatureAndCensusChanges(t *testing.T) {
	f := newProductionBootstrapObservationFixture(t, "")
	before := f.calls()
	files := f.cfg.EvidenceV2.Operators[1]
	raw, err := os.ReadFile(files.HotkeySignature.Path)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 1
	f.cfg.EvidenceV2.Operators[1].HotkeySignature = writeReleaseBootstrapV2TestFile(t, files.HotkeySignature.Path, raw)
	if got, err := readProductionBootstrapPublicInputs(t.Context(), &f.cfg, f.native.Hotkey); got != nil || err == nil {
		t.Fatal("changed hotkey signature accepted")
	}
	if f.calls() != before || f.apiCalls.Load() != 0 {
		t.Fatal("local signature refusal performed network work")
	}
	f.cfg.EvidenceV2.Operators[1].NoID = f.cfg.EvidenceV2.Operators[0].NoID
	if got, err := readProductionBootstrapPublicInputs(t.Context(), &f.cfg, f.native.Hotkey); got != nil || err == nil {
		t.Fatal("duplicate public evidence census accepted")
	}
}

// Public admission reauthenticates the exact signed config before it reads
// operator files or tries credentials. Neither missing context nor caller
// substitution can silently use a producer's retained authority fallback.
func TestProductionBootstrapObservationPublicScopeFailsBeforeNetwork(t *testing.T) {
	f := newProductionRuntimeTestFixture(t, false)
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	point := ProductionBootstrapNativePoint{Block: 101, Hash: [32]byte{1}, Epoch: 20, Hotkey: [32]byte{2}}
	got, err := ObserveProductionBootstrapOperators(t.Context(), f.path, raw, point, ProductionBootstrapEvmPoint{Block: 1, Hash: [32]byte{1}})
	if got != nil || err == nil || !strings.Contains(err.Error(), "approved native scope") {
		t.Fatal("unapproved native caller identity reached operator observation", got, err)
	}
}
