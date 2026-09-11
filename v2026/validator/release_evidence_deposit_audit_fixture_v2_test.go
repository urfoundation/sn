//go:build linux || darwin

// The actual native reader, hash-selected Evm views, payout HTTP parser and
// authenticated source-owned upload sessions form one producer fixture.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// The endpoint method completes the external Gsrpc client interface only.
type depositAuditNativeRpcTestClient struct {
	*gsrpcgeth.Client
	endpoint string
}

func (self *depositAuditNativeRpcTestClient) URL() string { return self.endpoint }

// Mutable controls change actual HTTP responses, never a source verdict.
type depositAuditPublicationV2TestFixture struct {
	base        *releaseRuntimeV2TestFixture
	chain       *releaseDecisionV2TestFixture
	artifact    *ReleaseMeasurementArtifact
	options     ValidatorEvidencePublicationV2ReadOptions
	mode        string
	sourceNoIds [2]uint64
	payout      *payoutartifact.Artifact
	payoutBytes []byte
	payoutReads atomic.Uint64
	publicReads atomic.Uint64
	posts       atomic.Uint64
	nativeReads atomic.Uint64
	outage      atomic.Bool
	refuseLast  atomic.Bool
}

// Extend the existing actual activation/runtime fixture with independently
// served later epoch views. The prior activation/keys remain unchanged.
func newDepositAuditPublicationV2TestFixture(t *testing.T, mode string) *depositAuditPublicationV2TestFixture {
	return newDepositAuditPublicationV2TestFixtureWithBounds(t, mode, nil, 1)
}

// Larger source observations choose their complete signed provider census and
// independent bounds before the real runtime and HTTP owners are constructed.
func newDepositAuditPublicationV2TestFixtureWithBounds(t *testing.T, mode string, bounds *ReleaseEvidenceV2Bounds, providerCount int) *depositAuditPublicationV2TestFixture {
	t.Helper()
	self := &depositAuditPublicationV2TestFixture{base: newReleaseRuntimeV2TestFixtureWithBounds(t, bounds), chain: newReleaseDecisionV2TestFixture(t), mode: mode}
	runtime, chain := self.base.runtime, self.chain
	cfg := runtime.cfg
	// Startup's private explicit-artifact fixture uses genuine tiny metadata.
	// The later producer must receive that same independent identity; current
	// reviewed production pins and the original startup configuration stay intact.
	nativeIdentity := self.base.startup.nativeFixture.expected
	cfg.RuntimeSpec, cfg.TransactionVersion, cfg.StateVersion = nativeIdentity.Version.SpecVersion, nativeIdentity.Version.TransactionVersion, nativeIdentity.Version.StateVersion
	cfg.RuntimeCodeHash, cfg.RuntimeMetadataHash = nativeIdentity.CodeHash, nativeIdentity.MetadataHash
	activation := runtime.sources[runtime.history.participants[0].NoID].activation
	for index, participant := range runtime.history.participants {
		self.sourceNoIds[index] = participant.NoID
	}
	domain, err := activation.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	epoch := activation.Domain.Epoch + 1
	start := uint64(10) + epoch*cfg.Policy.Settlement.EpochBlocks
	sourceStart, block := start-cfg.Policy.Settlement.EpochBlocks, start+5
	chain.query = releaseDecisionChainV2Query{domain: domain, boundary: AttemptBoundary{SettlementEpoch: epoch, EVMBlock: block, EVMBlockHash: attemptHex32([32]byte{0x73})}, policy: cfg.Policy,
		operators: []releaseDecisionChainV2OperatorQuery{{noID: self.sourceNoIds[0]}, {noID: self.sourceNoIds[1]}}, maxOperators: cfg.EvidenceV2.Bounds.MaxOperators, maxProviders: cfg.EvidenceV2.Bounds.MaxProviders, maxControlBytes: cfg.EvidenceV2.Bounds.MaxControlBytes}
	chain.blocks = map[uint64][32]byte{sourceStart: {0x71}, start: {0x72}, block: {0x73}, block - 1: {0x74}}
	chain.finalized = block
	chain.chain.contractAddr = common.Address(domain.Coordinator)
	coordinator := chain.chain.coordinator
	chain.set(t, "netuid", coordinator.PackNetuid(), cfg.Netuid)
	chain.set(t, "currentEpoch", coordinator.PackCurrentEpoch(), new(big.Int).SetUint64(epoch))
	policy := cfg.Policy
	snapshotPolicy := stabi.STCoordinatorPolicySnapshot{PolicyHash: domain.PolicyHash, EffectiveEpoch: 0, EffectiveBlock: 10, EpochBlocks: policy.Settlement.EpochBlocks, RootCommitWindowBlocks: policy.Settlement.RootCommitWindowBlocks, FinalizeOffsetBlocks: policy.Settlement.FinalizeOffsetBlocks, CloseGraceBlocks: policy.Settlement.CloseGraceBlocks, ClaimTTLEpochs: policy.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: policy.Settlement.ClaimGraceEpochs, MaximumBindingValidityEpochs: policy.Binding.MaximumValidityEpochs, CommitmentMaxAgeBlocks: 10, EpochDepositCapRao: new(big.Int).SetUint64(policy.Deposit.EpochCapRaoPerOperator), CampaignDepositCapRao: new(big.Int).SetUint64(policy.Deposit.TotalTestCampaignCapRao)}
	if mode == "production" {
		snapshotPolicy.EffectiveEpoch, snapshotPolicy.EffectiveBlock = epoch, start
		snapshotPolicy.EpochBlocks, snapshotPolicy.RootCommitWindowBlocks = policy.ProductionCadence.EpochBlocks, policy.ProductionCadence.RootCommitWindowBlocks
		snapshotPolicy.FinalizeOffsetBlocks, snapshotPolicy.CloseGraceBlocks = policy.ProductionCadence.FinalizeOffsetBlocks, policy.ProductionCadence.CloseGraceBlocks
	}
	chain.set(t, "policyAt", coordinator.PackPolicyAt(new(big.Int).SetUint64(epoch)), snapshotPolicy)
	chain.set(t, "epochStartBlock", coordinator.PackEpochStartBlock(new(big.Int).SetUint64(epoch)), new(big.Int).SetUint64(start))
	chain.set(t, "epochStartBlock", coordinator.PackEpochStartBlock(new(big.Int).SetUint64(epoch-1)), new(big.Int).SetUint64(sourceStart))
	chain.set(t, "epochEndBlock", coordinator.PackEpochEndBlock(new(big.Int).SetUint64(epoch-1)), new(big.Int).SetUint64(start))
	providers := []payoutartifact.ProviderInput{{ClientID: [16]byte{1}, Coldkey: [32]byte{1}, UsageBytes: 3 * 1024 * 1024 * 1024, Assignments: 8, Confirmations: 8, Eligible: true}}
	for index := 1; index < providerCount; index++ {
		var clientId [16]byte
		clientId[0], clientId[1], clientId[2] = 2, byte(index>>8), byte(index)
		var coldkey [32]byte
		copy(coldkey[:], clientId[:])
		providers = append(providers, payoutartifact.ProviderInput{ClientID: clientId, Coldkey: coldkey, UsageBytes: 1, Assignments: 8, Confirmations: 8, Eligible: true})
	}
	self.payout, err = payoutartifact.Build(payoutartifact.BuildInput{DeploymentID: cfg.DeploymentID, GenesisHash: cfg.GenesisHash, PolicyHash: cfg.PolicyHash, ChainID: cfg.ChainID, Netuid: cfg.Netuid, Coordinator: common.HexToAddress(cfg.Coordinator), SettlementVault: common.HexToAddress(cfg.SettlementVault), Epoch: epoch - 1, NoID: self.sourceNoIds[0],
		Start: payoutartifact.Boundary{Number: sourceStart, Hash: attemptHex32(chain.blocks[sourceStart])}, End: payoutartifact.Boundary{Number: start, Hash: attemptHex32(chain.blocks[start])}, OperatorSnapshotHash: "sha256:" + strings.Repeat("10", 32), FleetSnapshotHash: "sha256:" + strings.Repeat("20", 32),
		Providers: providers, ReliabilityAMin: cfg.Policy.Verify.ReliabilityAMin, CreatedAt: time.Unix(1700000000, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	payoutKey, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(self.payout, payoutKey); err != nil {
		t.Fatal(err)
	}
	self.payoutBytes, err = payoutartifact.Bytes(self.payout)
	if err != nil {
		t.Fatal(err)
	}
	payoutHash, err := parseReleaseContentHash(self.payout.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	required, _, err := protocol.RequiredDepositRao(self.payout.TotalUsageBytes, chain.bigConviction, cfg.Policy.Deposit)
	if err != nil || required.Sign() <= 0 {
		t.Fatalf("actual payout deposit: %v", err)
	}
	for index := range chain.versions {
		id := new(big.Int).SetUint64(self.sourceNoIds[index])
		chain.set(t, "operatorIdAt", coordinator.PackOperatorIdAt(big.NewInt(int64(index))), id)
		version := chain.versions[index]
		version.RootSigner = self.payout.Signer
		chain.versions[index] = version
		chain.set(t, "operatorAt", coordinator.PackOperatorAt(id, new(big.Int).SetUint64(epoch)), version)
		chain.set(t, "operatorAt", coordinator.PackOperatorAt(id, new(big.Int).SetUint64(epoch-1)), version)
		deposit := big.NewInt(15)
		if index == 0 && (mode == "positive" || mode == "production") {
			deposit = required
		}
		chain.set(t, "epochDeposits", coordinator.PackEpochDeposits(new(big.Int).SetUint64(epoch), id), deposit)
		chain.set(t, "epochConvictionAdded", coordinator.PackEpochConvictionAdded(new(big.Int).SetUint64(epoch), id), big.NewInt(7))
		chain.set(t, "cumulativeConviction", coordinator.PackCumulativeConviction(id), new(big.Int).Add(new(big.Int).Add(new(big.Int).Set(chain.bigConviction), deposit), big.NewInt(7)))
		commitment := stabi.RootCommitmentsOutput{}
		if index == 0 && mode != "no-payout" {
			commitment = stabi.RootCommitmentsOutput{PayoutRoot: self.payout.PayoutRoot, ArtifactHash: payoutHash, Committer: version.RootSigner, CommitBlock: start + 1}
		}
		chain.set(t, "rootCommitments", coordinator.PackRootCommitments(new(big.Int).SetUint64(epoch-1), id), commitment.PayoutRoot, commitment.ArtifactHash, commitment.Committer, commitment.CommitBlock)
	}
	selector, netuid, uid := evmSelector("getHotkey(uint16,uint16)"), evmUint16Word(cfg.Netuid), evmUint16Word(2)
	data := append(append(slices.Clone(selector[:]), netuid[:]...), uid[:]...)
	key := self.base.hotkey.PublicKey()
	chain.views[fmt.Sprintf("%x", data)] = releaseDecisionV2TestView{method: "getHotkey", data: bytes.Clone(key[:])}
	native := self.base.startup.nativeFixture
	native.blockNumber = block
	self.base.startup.nativeEpoch[native.block.Hex()] = block - 99
	self.wrapNativeHttp(t)
	for index := range runtime.runtimes {
		original, err := url.Parse(runtime.origins[index])
		if err != nil {
			t.Fatal(err)
		}
		proxy := httputil.NewSingleHostReverseProxy(original)
		endpoint := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/sn/artifacts" || request.URL.Path == "/sn/artifact" {
				self.payoutReads.Add(1)
				writer.Header().Set("Content-Type", "application/json")
				if index != 0 || mode == "no-payout" {
					writer.WriteHeader(http.StatusInternalServerError)
					return
				}
				if mode == "negative" || self.outage.Load() {
					writer.WriteHeader(http.StatusServiceUnavailable)
					_, _ = writer.Write([]byte(`{"actual":"payout unavailable"}`))
					return
				}
				if request.URL.Path == "/sn/artifacts" {
					if request.URL.Query().Get("no_id") != strconv.FormatUint(self.sourceNoIds[0], 10) || request.URL.Query().Get("epoch") != strconv.FormatUint(epoch-1, 10) {
						writer.WriteHeader(http.StatusBadRequest)
						return
					}
					value := artifactHistoryResponse{Schema: "urnetwork-payout-artifact-history-v1", Objects: []artifactHistoryObject{{Key: fmt.Sprintf("store/st/v1/history/%s/%d/%d/%d/%s.json", cfg.DeploymentID, cfg.Netuid, epoch-1, self.sourceNoIds[0], strings.TrimPrefix(self.payout.ContentHash, "sha256:")), Size: int64(len(self.payoutBytes)), ContentHash: self.payout.ContentHash}}}
					_ = json.NewEncoder(writer).Encode(value)
				} else {
					if request.URL.Query().Get("hash") != self.payout.ContentHash {
						writer.WriteHeader(http.StatusBadRequest)
						return
					}
					_, _ = writer.Write(self.payoutBytes)
				}
				return
			}
			if request.Method == http.MethodPost && request.URL.Path == "/sn/attempt-artifact" {
				self.posts.Add(1)
				raw, err := io.ReadAll(request.Body)
				if err != nil {
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				_ = request.Body.Close()
				request.Body = io.NopCloser(bytes.NewReader(raw))
				path, err := ValidatorEvidenceDepositAuditV2ManifestPath(runtime.cfg.StateDir, epoch-1, protocol.ValidatorEvidenceSubject{ObservationEpoch: epoch, NativeEpoch: block - 99})
				if err != nil {
					writer.WriteHeader(http.StatusInternalServerError)
					return
				}
				prepared, err := os.ReadFile(filepath.Join(runtime.cfg.StateDir, "evidence-deposit-audit-prepared", filepath.Base(path)))
				var bundle releaseDepositAuditPreparedV2
				if err != nil || json.Unmarshal(prepared, &bundle) != nil || bundle.Publication == nil || len(bundle.Publication.Members) != 2 {
					t.Error("audit public upload preceded durable complete signatures")
					writer.WriteHeader(http.StatusForbidden)
					return
				}
				for _, member := range bundle.Publication.Members {
					if len(member.Evidence.HotkeySignature) != 64 || len(member.Evidence.VPKSignature) != 64 {
						t.Error("audit public upload preceded final member consent")
						writer.WriteHeader(http.StatusForbidden)
						return
					}
				}
				var signed ValidatorEvidenceSignedV2
				if index == 1 && json.Unmarshal(raw, &signed) == nil && signed.Schema == ValidatorEvidenceSignedV2Schema && signed.Header.NoID == self.sourceNoIds[1] && self.refuseLast.CompareAndSwap(true, false) {
					writer.WriteHeader(http.StatusServiceUnavailable)
					return
				}
			}
			if request.Method == http.MethodGet && request.URL.Path == "/sn/attempt-artifact" {
				self.publicReads.Add(1)
			}
			proxy.ServeHTTP(writer, request)
		}))
		t.Cleanup(endpoint.Close)
		credential := "runtime-v2-destination-" + strconv.Itoa(index+1)
		strategy := connect.NewClientStrategyWithDefaults(t.Context())
		api := sdk.NewApi(t.Context(), strategy, endpoint.URL)
		api.SetByJwt(credential)
		t.Cleanup(func() {
			if err := api.CloseAndWait(context.Background()); err != nil {
				t.Error(err)
			}
			strategy.Close()
		})
		cfg.Operators[index].APIURL, cfg.Operators[index].ArtifactSigner = endpoint.URL, self.payout.Signer.Hex()
		upload, err := newReleaseAttemptUploadV2(t.Context(), cfg.Operators[index], cfg.EvidenceV2.Bounds, api.GetByJwt)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(upload.close)
		runtime.runtimes[index].attemptUpload = upload
		runtime.origins[index] = endpoint.URL
	}
	runtime.cfg, runtime.history.cfg, runtime.chain = cfg, cfg, chain.chain
	decision := ReleaseMeasurementV2Decision{DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.GenesisHash, Coordinator: cfg.Coordinator, SettlementVault: cfg.SettlementVault, ValidatorID: cfg.ValidatorID, Netuid: cfg.Netuid,
		SubnetEpoch: block - 99, NativeSnapshotBlock: block, NativeSnapshotHash: native.block.Hex(), EVMSnapshotBlock: block, EVMSnapshotHash: chain.query.boundary.EVMBlockHash, SettlementEpoch: epoch, PolicyHash: cfg.PolicyHash, SelfUID: 2}
	self.artifact = releaseDepositAuditV2Artifact(decision, nil, cfg.Policy)
	contexts := map[uint64]*ReleaseMeasurementContext{}
	operators := map[uint64]OperatorConfig{}
	for _, operator := range cfg.Operators {
		reader, err := NewHTTPArtifactReader(operator.APIURL, cfg.DeploymentID, cfg.Netuid)
		if err != nil {
			t.Fatal(err)
		}
		contexts[operator.NoID] = &ReleaseMeasurementContext{NoID: operator.NoID, Artifacts: reader}
		operators[operator.NoID] = operator
	}
	poolCfg := cfg
	poolCfg.ControlledNOIDs = slices.Clone(self.sourceNoIds[:])
	steerer := &ReleaseSteerer{cfg: &poolCfg, chain: chain.chain, hotkey: self.base.hotkey, contexts: contexts, operators: operators}
	observed, err := chain.chain.readReleaseDecisionChainV2Context(t.Context(), chain.query)
	if err != nil {
		t.Fatal(err)
	}
	self.artifact.Pools, self.artifact.DepositAudits, err = steerer.gatherPoolsV2(t.Context(), &ReleaseSnapshot{BlockNumber: block, BlockHash: chain.blocks[block], Epoch: new(big.Int).SetUint64(epoch), Policy: snapshotPolicy}, map[uint64]map[connect.Id]bool{}, observed.hotkeyUIDs, self.artifact)
	if err != nil {
		t.Fatal(err)
	}
	self.options = ValidatorEvidencePublicationV2ReadOptions{Origins: runtime.origins, Bounds: cfg.EvidenceV2.Bounds, Window: protocol.ValidatorEvidenceWindow{Epoch: epoch - 1, StartBlock: sourceStart, EndBlock: start, FinalizedBlock: block, Subject: protocol.ValidatorEvidenceSubject{ObservationEpoch: epoch, NativeEpoch: block - 99}}}
	for _, participant := range runtime.history.participants {
		self.options.Activations = append(self.options.Activations, runtime.sources[participant.NoID].activation)
	}
	return self
}

// The existing selective metadata/state producer is exposed as real JSON-Rpc;
// the production Gsrpc client still owns wire decoding and cancellation.
func (self *depositAuditPublicationV2TestFixture) wrapNativeHttp(t *testing.T) {
	t.Helper()
	native := self.base.startup.nativeFixture
	original := native.chain.API.Client
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		self.nativeReads.Add(1)
		var call struct {
			Id     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.NewDecoder(request.Body).Decode(&call) != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var args []any
		for _, parameter := range call.Params {
			var value any
			if call.Method == "chain_getBlockHash" {
				var number uint64
				if json.Unmarshal(parameter, &number) != nil {
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				value = number
			} else {
				var text string
				if json.Unmarshal(parameter, &text) != nil {
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				value = text
			}
			args = append(args, value)
		}
		var result any
		var raw json.RawMessage
		result = &raw
		if call.Method == "chain_getHeader" {
			result = new(types.Header)
		}
		err := original.CallContext(request.Context(), result, call.Method, args...)
		response := map[string]any{"jsonrpc": "2.0", "id": call.Id}
		if err != nil {
			response["error"] = map[string]any{"code": -32000, "message": err.Error()}
		} else {
			response["result"] = result
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(response)
	}))
	t.Cleanup(server.Close)
	client, err := gsrpcgeth.DialContext(t.Context(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	native.chain.API = &gsrpc.SubstrateAPI{Client: &depositAuditNativeRpcTestClient{Client: client, endpoint: server.URL}}
	self.base.runtime.native = native.chain
}

// Actual immutable descriptor reads select completed and original custody.
func (self *depositAuditPublicationV2TestFixture) retained(t *testing.T) (*ValidatorEvidenceDepositAuditV2Manifest, []byte) {
	t.Helper()
	path, err := ValidatorEvidenceDepositAuditV2ManifestPath(self.base.runtime.cfg.StateDir, self.options.Window.Epoch, self.options.Window.Subject)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ReadValidatorEvidenceDepositAuditV2Manifest(t.Context(), path, self.options.Bounds.MaxClosureBytes, self.options.Bounds.MaxParticipants)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := os.ReadFile(filepath.Join(self.base.runtime.cfg.StateDir, "evidence-deposit-audit-prepared", filepath.Base(path)))
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared) == 0 {
		t.Fatal("empty prepared custody")
	}
	return manifest, prepared
}

// New private operation ownership reopens all persisted public-source bytes;
// actual activation/source/destination identities remain the original ones.
func (self *depositAuditPublicationV2TestFixture) restart() {
	owned := *self.base.runtime
	owned.gate = make(chan struct{}, 1)
	self.base.runtime = &owned
}

// A control error is an actual source refusal, not a fabricated publication.
func requireDepositAuditPublicationV2Refused(t *testing.T, fixture *depositAuditPublicationV2TestFixture) {
	t.Helper()
	before := fixture.posts.Load()
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err == nil {
		t.Fatal("changed actual audit source was accepted")
	}
	if fixture.posts.Load() != before {
		t.Fatal("changed actual source reached protected upload")
	}
	manifests, err := DiscoverValidatorEvidenceDepositAuditV2Manifests(t.Context(), fixture.base.runtime.cfg.StateDir, fixture.options.Bounds)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(manifests) != 0 {
		t.Fatal("refused source created a completed audit locator")
	}
}
