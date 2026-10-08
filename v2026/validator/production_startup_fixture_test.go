//go:build linux || darwin

// Full startup uses the same signed, zero-price continuation corpus, with real
// pre-seal activation anchors, private configuration and two physical API owners.
// It is continuation coverage, not paid capture or mainnet economic acceptance.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"github.com/urnetwork/sdk/v2026"
	"gopkg.in/yaml.v3"
)

// All signed values are fixed before serving. The native owner serializes raw
// fixture state; tests alter it only between joined service lifecycles.
type productionStartupTestFixture struct {
	storage            *durablefixture.Fixture
	continuation       *productionContinuationTestFixture
	native             *productionContinuationNativeTestClient
	activationKVs      map[uint64]protocol.ValidatorEvidenceActivation
	contextKVs         map[uint64]ReleaseEvidenceV2ActivationContext
	origins            [2]*productionStartupApiTestFixture
	evm                *productionStartupEvmTestFixture
	configPath         string
	stateLock          sync.Mutex
	latestReads        int
	writeRequests      int
	latestRead         chan struct{}
	latestOnce         sync.Once
	nativeBodyWaitHash string
	nativeBodyWait     <-chan struct{}
}

// Journal views are independently ABI encoded, rather than supplied as a
// successful activation verdict. The existing decision fixture handles all
// original policy, operator, binding, deposit and metagraph reads.
type productionStartupEvmTestFixture struct {
	operator  *recycleOperatorFixture
	contexts  map[uint64]ReleaseEvidenceV2ActivationContext
	code      []byte
	freshRead chan struct{}
	freshOnce sync.Once
	release   chan struct{}
}

// Blocking handlers consume and close the real bounded body before publishing
// their barrier. An unread POST body can delay net/http cancellation delivery.
func productionStartupRequestTestBytes(request *http.Request) ([]byte, error) {
	const maximum = 1024 * 1024
	raw, readErr := io.ReadAll(io.LimitReader(request.Body, maximum+1))
	err := errors.Join(readErr, request.Body.Close())
	if len(raw) > maximum {
		err = errors.Join(err, errors.New("startup fixture request exceeds its bound"))
	}
	return raw, err
}

// The exact current-only view has a physical HTTP outage. Historical reads
// remain available; the request barrier lets the test inspect durable progress
// before releasing/canceling the real request, without an elapsed-time guess.
func (self *productionStartupEvmTestFixture) allowHttp(writer http.ResponseWriter, request *http.Request) bool {
	raw, err := productionStartupRequestTestBytes(request)
	if err != nil {
		writer.WriteHeader(http.StatusBadRequest)
		return false
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	var call struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if json.Unmarshal(raw, &call) == nil && call.Method == "eth_call" && len(call.Params) == 2 {
		var selector gethrpc.BlockNumberOrHash
		if json.Unmarshal(call.Params[1], &selector) == nil && selector.BlockHash != nil && *selector.BlockHash == common.Hash(self.operator.blocks[110]) {
			self.freshOnce.Do(func() { close(self.freshRead) })
			writer.WriteHeader(http.StatusServiceUnavailable)
			writer.(http.Flusher).Flush()
			select {
			case <-request.Context().Done():
			case <-self.release:
			}
			return false
		}
	}
	return true
}

// Exactly pinned historical state includes the activation record and code.
func (self *productionStartupEvmTestFixture) codeAt(ctx context.Context, target common.Address, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	initial := self.contexts[9]
	if target != common.Address(initial.Journal) || selector.BlockHash == nil || *selector.BlockHash != common.Hash(initial.ObservedEVMHash) || selector.BlockNumber != nil || !selector.RequireCanonical {
		return nil, errors.New("startup journal code lost exact publication identity")
	}
	return bytes.Clone(self.code), ctx.Err()
}

// Method bytes and the exact target/hash select a response. Unhandled current
// decision calls continue through the original strict fixture.
func (self *productionStartupEvmTestFixture) view(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) ([]byte, bool, error) {
	if selector.BlockHash == nil || selector.BlockNumber != nil || !selector.RequireCanonical {
		return nil, false, errors.New("startup view lost canonical selector")
	}
	data := call["input"]
	if len(data) == 0 {
		data = call["data"]
	}
	initial := self.contexts[9]
	target := common.BytesToAddress(call["to"])
	coordinator, journal := stabi.NewSTCoordinator(), stabi.NewSTValidatorEvidence()
	coordinatorAbi, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, false, err
	}
	journalAbi, err := stabi.STValidatorEvidenceMetaData.ParseABI()
	if err != nil {
		return nil, false, err
	}
	if *selector.BlockHash == common.Hash(initial.Activation.EVMHash) {
		if target != common.Address(initial.Activation.Domain.Coordinator) {
			return nil, false, errors.New("startup activation historical target differs")
		}
		if bytes.Equal(data, coordinator.PackCurrentEpoch()) {
			raw, err := coordinatorAbi.Methods["currentEpoch"].Outputs.Pack(new(big.Int).SetUint64(initial.Activation.Domain.Epoch - 1))
			return raw, true, err
		}
		// Earlier activation approves the known upcoming policy/operator row;
		// those exact independent bytes are also served at the later observer.
		observer := gethrpc.BlockNumberOrHashWithHash(common.Hash(self.operator.blocks[self.operator.finalized]), true)
		raw, err := self.operator.releaseDecisionV2TestFixture.Call(ctx, call, observer)
		return raw, true, err
	}
	if *selector.BlockHash != common.Hash(initial.ObservedEVMHash) {
		return nil, false, nil
	}
	if target == common.Address(initial.Activation.Domain.Coordinator) && bytes.Equal(data, coordinator.PackValidatorEvidence()) {
		raw, err := coordinatorAbi.Methods["validatorEvidence"].Outputs.Pack(common.Address(initial.Journal))
		return raw, true, err
	}
	if target != common.Address(initial.Journal) {
		observer := gethrpc.BlockNumberOrHashWithHash(common.Hash(self.operator.blocks[self.operator.finalized]), true)
		raw, err := self.operator.releaseDecisionV2TestFixture.Call(ctx, call, observer)
		return raw, true, err
	}
	views := []releaseActivationV2TestView{
		{data: journal.PackCoordinator(), method: "coordinator", value: common.Address(initial.Activation.Domain.Coordinator)},
		{data: journal.PackSettlementVault(), method: "settlementVault", value: common.Address(initial.Activation.Domain.SettlementVault)},
		{data: journal.PackChainId(), method: "chainId", value: initial.Activation.Domain.ChainID},
		{data: journal.PackNetuid(), method: "netuid", value: initial.Activation.Domain.Netuid},
		{data: journal.PackGenesisHash(), method: "genesisHash", value: initial.Activation.Domain.GenesisHash},
		{data: journal.PackDeploymentIdHash(), method: "deploymentIdHash", value: initial.Activation.Domain.DeploymentIDHash},
	}
	for _, value := range self.contexts {
		digest, err := value.Activation.Digest()
		if err != nil {
			return nil, false, err
		}
		views = append(views, releaseActivationV2TestView{data: journal.PackActivation(digest), method: "activation", value: stabi.STValidatorEvidenceActivation{Record: stabi.ValidatorEvidenceActivationRecordFromProtocol(value.Activation), PublishedBlock: value.Activation.EVMBlock + 1}})
	}
	for _, view := range views {
		if bytes.Equal(data, view.data) {
			raw, err := journalAbi.Methods[view.method].Outputs.Pack(view.value)
			return raw, true, err
		}
	}
	return nil, false, errors.New("startup journal has no independent view for the request")
}

// Each origin owns real public bytes and enforces source signatures plus its
// distinct destination session. Both origins begin with the signed corpus.
type productionStartupApiTestFixture struct {
	owner                   *productionStartupTestFixture
	noId                    uint64
	credential              string
	keys                    map[byte]ed25519.PublicKey
	stateLock               sync.Mutex
	objects                 map[string][]byte
	posts                   int
	sessions                int
	seedRead                chan struct{}
	seedOnce                sync.Once
	release                 chan struct{}
	registrationUnavailable bool
	registrationRequest     []byte
	registrationPosts       int
	registrationRead        chan struct{}
	registrationOnce        sync.Once
	rejectRefreshAfter      int
	beforeRefreshReject     func(context.Context)
	beforeRefresh           func(context.Context)
	invalidRefreshAfter     int
	invalidRefresh          string
}

// This HTTP fixture retains one exact operation even when its reply is lost.
// The separate server suite exercises the actual database allocation owner.
func (self *productionStartupApiTestFixture) registerClient(writer http.ResponseWriter, request *http.Request) {
	raw, err := productionStartupRequestTestBytes(request)
	var args sdk.RegisterNetworkClientArgs
	if err != nil || request.Method != http.MethodPost || json.Unmarshal(raw, &args) != nil {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	self.stateLock.Lock()
	if self.registrationRequest == nil {
		self.registrationRequest = bytes.Clone(raw)
	}
	same := bytes.Equal(self.registrationRequest, raw)
	self.registrationPosts++
	unavailable := self.registrationUnavailable
	self.stateLock.Unlock()
	self.registrationOnce.Do(func() { close(self.registrationRead) })
	if !same {
		writer.WriteHeader(http.StatusConflict)
		return
	}
	if unavailable {
		writer.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	claims := gojwt.MapClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(self.credential, claims); err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	digest := sha256.Sum256(raw)
	_ = json.NewEncoder(writer).Encode(map[string]any{"schema": sdk.NetworkClientRegistrationSchema, "registration_id": args.RegistrationId, "request_sha256": hex.EncodeToString(digest[:]), "client_id": claims["client_id"], "device_id": claims["device_id"], "by_client_jwt": self.credential})
}

// Protocol traffic reaches actual HTTP handlers, including JWT refresh and
// immutable dual-replica publication. No source verification callback exists.
func (self *productionStartupApiTestFixture) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	switch request.URL.Path {
	case "/network/register-client-v1":
		self.registerClient(writer, request)
		return
	case "/hello":
		writer.WriteHeader(http.StatusOK)
		return
	case "/auth/refresh":
		if request.Header.Get("Authorization") != "Bearer "+self.credential {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		self.stateLock.Lock()
		self.sessions++
		invalid := self.invalidRefresh
		if self.sessions <= self.invalidRefreshAfter {
			invalid = ""
		}
		reject := self.rejectRefreshAfter > 0 && self.sessions > self.rejectRefreshAfter
		beforeReject := self.beforeRefreshReject
		beforeRefresh := self.beforeRefresh
		if reject {
			self.beforeRefreshReject = nil
		}
		self.stateLock.Unlock()
		if beforeRefresh != nil {
			if _, err := productionStartupRequestTestBytes(request); err != nil {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			beforeRefresh(request.Context())
		}
		if invalid != "" {
			if _, err := productionStartupRequestTestBytes(request); err != nil {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = writer.Write([]byte(invalid))
			return
		}
		if reject {
			if _, err := productionStartupRequestTestBytes(request); err != nil {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			if beforeReject != nil {
				beforeReject(request.Context())
			}
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]string{"by_jwt": self.credential})
		return
	case "/verify/keys":
		value := sdk.VerifyKeysResult{}
		for version, key := range self.keys {
			value.Keys = append(value.Keys, &sdk.VerifyServerKey{ServerKeyId: int32(version), PublicKey: key})
		}
		_ = json.NewEncoder(writer).Encode(value)
		return
	case "/network/find-providers2":
		if _, err := productionStartupRequestTestBytes(request); err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		self.seedOnce.Do(func() { close(self.seedRead) })
		select {
		case <-request.Context().Done():
		case <-self.release:
		}
		return
	case "/connect":
		upgrader := websocket.Upgrader{}
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	case "/sn/attempt-artifact":
	default:
		writer.WriteHeader(http.StatusNotFound)
		return
	}
	kind, hash := request.URL.Query().Get("kind"), request.URL.Query().Get("hash")
	if len(request.URL.Query()) != 2 {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	key := kind + "/" + hash
	if request.Method == http.MethodPost {
		raw, err := io.ReadAll(io.LimitReader(request.Body, 4*1024*1024+1))
		intent, signatureErr := protocol.VerifyValidatorAttemptUploadHeader(request.Header.Get(protocol.ValidatorAttemptUploadHeader))
		session, sessionErr := protocol.ValidatorAttemptUploadSessionHash(self.credential)
		validSource := false
		for _, activation := range self.owner.activationKVs {
			digest, digestErr := activation.Digest()
			validSource = validSource || digestErr == nil && intent.ActivationHash == digest && intent.VPK == activation.VPK
		}
		if err != nil || signatureErr != nil || sessionErr != nil || !validSource || len(raw) > 4*1024*1024 || intent.ReplicaNoID != self.noId || intent.SessionHash != session || request.Header.Get("Authorization") != "Bearer "+self.credential || intent.ContentHash != sha256.Sum256(raw) || intent.Size != uint64(len(raw)) || hash != attemptHex32(sha256.Sum256(raw)) {
			writer.WriteHeader(http.StatusForbidden)
			return
		}
		self.stateLock.Lock()
		prior := self.objects[key]
		if prior == nil || bytes.Equal(prior, raw) {
			self.objects[key] = bytes.Clone(raw)
			self.posts++
		}
		self.stateLock.Unlock()
		if prior != nil && !bytes.Equal(prior, raw) {
			writer.WriteHeader(http.StatusConflict)
			return
		}
		writer.Header().Set("ETag", "\""+hash+"\"")
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	if request.Method != http.MethodGet || request.Header.Get("Authorization") != "" {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	self.stateLock.Lock()
	raw := bytes.Clone(self.objects[key])
	self.stateLock.Unlock()
	if raw == nil {
		writer.WriteHeader(http.StatusNotFound)
		return
	}
	if kind != "metadata" {
		writer.Header().Set("Content-Type", "application/x-ndjson")
	}
	writer.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	_, _ = writer.Write(raw)
}

// Only raw RPC serialization is adapted. The original strict native fixture
// still serves exact metadata, canonical bodies, events, source and weight rows.
func (self *productionStartupTestFixture) serveNative(writer http.ResponseWriter, request *http.Request) {
	if websocket.IsWebSocketUpgrade(request) {
		upgrader := websocket.Upgrader{}
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		var writers sync.Mutex
		var joined sync.WaitGroup
		ctx, cancel := context.WithCancel(request.Context())
		defer func() { cancel(); joined.Wait() }()
		for {
			_, payload, err := connection.ReadMessage()
			if err != nil {
				return
			}
			joined.Add(1)
			go func() {
				defer joined.Done()
				response := self.nativeResponse(ctx, payload)
				writers.Lock()
				defer writers.Unlock()
				_ = connection.WriteMessage(websocket.TextMessage, response)
			}()
		}
	}
	if request.Method != http.MethodPost {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	payload, err := io.ReadAll(io.LimitReader(request.Body, 1024*1024))
	if err != nil {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	_, _ = writer.Write(self.nativeResponse(request.Context(), payload))
}

// WebSocket and read-only HTTP share genuine raw native state. Production
// configuration explicitly approves WebSocket; no constructor changes route.
func (self *productionStartupTestFixture) nativeResponse(ctx context.Context, payload []byte) []byte {
	var call struct {
		Id     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params []any           `json:"params"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	reply := func(result any, err error) []byte {
		value := map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result}
		if err != nil {
			delete(value, "result")
			value["error"] = map[string]any{"code": -32000, "message": err.Error()}
		}
		encoded, _ := json.Marshal(value)
		return encoded
	}
	if decoder.Decode(&call) != nil {
		return reply(nil, errors.New("synthetic native request is malformed"))
	}
	if call.Method == "chain_getBlock" && len(call.Params) == 1 && call.Params[0] == self.nativeBodyWaitHash && self.nativeBodyWait != nil {
		select {
		case <-ctx.Done():
			return reply(nil, ctx.Err())
		case <-self.nativeBodyWait:
		}
	}
	if strings.HasPrefix(call.Method, "author_") {
		self.stateLock.Lock()
		self.writeRequests++
		self.stateLock.Unlock()
	}
	if (call.Method == "state_getMetadata" || call.Method == "state_getRuntimeVersion") && len(call.Params) == 0 {
		self.stateLock.Lock()
		self.latestReads++
		self.stateLock.Unlock()
		self.latestOnce.Do(func() { close(self.latestRead) })
		return reply(nil, errors.New("synthetic latest metadata is unavailable; exact historical reads remain available"))
	}
	for index, param := range call.Params {
		if number, ok := param.(json.Number); ok {
			value, err := strconv.ParseUint(string(number), 10, 64)
			if err != nil {
				return reply(nil, err)
			}
			call.Params[index] = value
		}
	}
	var raw json.RawMessage
	err := func() error {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if self.native == nil {
			return errors.New("startup native fixture requested before source assembly")
		}
		return self.native.CallContext(ctx, &raw, call.Method, call.Params...)
	}()
	return reply(raw, err)
}

// The public loader receives exactly the independently signed configuration.
// This factory never changes it after the proposal signature is generated.
func newProductionStartupTestFixture(t *testing.T) *productionStartupTestFixture {
	return newProductionStartupTestFixtureWithRegistration(t, false)
}

// Opt-in belongs to the actual complete signed configuration, before any
// source or intent exists. Default fixtures preserve historical config bytes.
func newProductionStartupTestFixtureWithRegistration(t *testing.T, allowRegistration bool) *productionStartupTestFixture {
	return newProductionStartupTestFixtureWithConfig(t, allowRegistration, nil)
}

// configure edits the complete config before the independent approval signs
// it; nil keeps the default fixture's config bytes unchanged.
func newProductionStartupTestFixtureWithConfig(t *testing.T, allowRegistration bool, configure func(*ReleaseConfig)) *productionStartupTestFixture {
	t.Helper()
	self := &productionStartupTestFixture{activationKVs: map[uint64]protocol.ValidatorEvidenceActivation{}, contextKVs: map[uint64]ReleaseEvidenceV2ActivationContext{}, latestRead: make(chan struct{})}
	root := identityTestStateDir(t)
	nativeServer := httptest.NewServer(http.HandlerFunc(self.serveNative))
	t.Cleanup(nativeServer.Close)
	anchor := func(admission *recycleAdmissionFixture, source *attemptCutV2SealTestFixture) {
		domain := source.expected.Activation.Domain
		activation := protocol.ValidatorEvidenceActivation{
			Domain: protocol.ValidatorEvidenceActivationDomain{ChainID: domain.ChainID, GenesisHash: domain.GenesisHash, Netuid: domain.Netuid, Coordinator: domain.Coordinator, SettlementVault: domain.SettlementVault, DeploymentIDHash: domain.DeploymentIDHash, PolicyHash: domain.PolicyHash, Epoch: domain.ActivationEpoch},
			Hotkey: source.expected.Activation.Hotkey, NoID: source.expected.Identity.NoID, FirstSequence: 1,
			NativeBlock: 100, NativeHash: [32]byte(admission.finalized), EVMBlock: 80, EVMHash: [32]byte{0x78},
		}
		copy(activation.VPK[:], source.key.Public().(ed25519.PublicKey))
		var err error
		source.expected.Activation.Domain, err = activation.EvidenceDomain()
		if err != nil {
			t.Fatal(err)
		}
		self.activationKVs[activation.NoID] = activation
	}
	self.continuation = newProductionContinuationTestFixtureWithStartup(t, anchor, func(production *ownerRecycleProductionTestFixture, continuation *productionContinuationTestFixture) {
		cfg := production.cfg
		cfg.PollSeconds = 1
		cfg.HotkeySeedFile = filepath.Join(root, "hotkey.seed")
		seed := [32]byte{0x6a, 0x41}
		writeReleaseBootstrapV2TestFile(t, cfg.HotkeySeedFile, seed[:])
		nativeEndpoint := "ws" + strings.TrimPrefix(nativeServer.URL, "http")
		cfg.Substrate = []string{nativeEndpoint}
		production.operator.measurement.admission.chain.API.Client.(*recycleAdmissionRouteClient).route = nativeEndpoint
		original := cfg.EvidenceV2
		cfg.EvidenceV2 = releaseEvidenceV2TestConfig(root, cfg.Operators)
		cfg.EvidenceV2.UploadIntentSeconds = 300
		cfg.EvidenceV2.Bounds.Cut, cfg.EvidenceV2.Bounds.Replay = original.Bounds.Cut, original.Bounds.Replay
		cfg.EvidenceV2.Bounds.MaxHistoryBytes, cfg.EvidenceV2.Bounds.MaxEgressHashes = original.Bounds.MaxHistoryBytes, original.Bounds.MaxEgressHashes
		// Match the actual durable ledger census. Public metadata must admit
		// the separately configured signed header; replay must admit every
		// row allowed by that physical ledger, not only today's short rows.
		bounds := &cfg.EvidenceV2.Bounds
		bounds.Disk = attemptLedgerDiskTestLimits()
		bounds.Cut.Records.MaxPageBytes = max(bounds.Cut.Records.MaxPageBytes, bounds.Cut.MaxHeaderBytes)
		bounds.Cut.Proofs.MaxPageBytes = max(bounds.Cut.Proofs.MaxPageBytes, bounds.Cut.MaxHeaderBytes)
		bounds.Replay.MaxRecordBytes = max(bounds.Replay.MaxRecordBytes, bounds.Disk.MaxRecordBytes)
		bounds.Cut.Records.MaxChunkBytes = max(bounds.Cut.Records.MaxChunkBytes, bounds.Replay.MaxRecordBytes)
		self.evm = &productionStartupEvmTestFixture{operator: production.operator, contexts: self.contextKVs, code: []byte{0x60, 0x00, 0x00}, freshRead: make(chan struct{}), release: make(chan struct{})}
		t.Cleanup(func() { close(self.evm.release) })
		production.operator.startup = self.evm
		production.operator.blocks[80] = [32]byte{0x78}
		for index := range cfg.Operators {
			op := &cfg.Operators[index]
			op.AllowClientRegistration = allowRegistration
			physical := continuation.inputKVs[op.NoID]
			input := &cfg.EvidenceV2.Operators[index]
			// Scratch owns distinct provisioned roots outside every durable
			// state namespace, as required by the real public config loader.
			for _, path := range []string{input.ReplayScratchRoot, input.SealScratchRoot} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			activation := self.activationKVs[op.NoID]
			observed, err := parseReleaseHex32("startup publication hash", physical.source.expected.Boundary.EVMBlockHash, false)
			if err != nil {
				t.Fatal(err)
			}
			initial := ReleaseEvidenceV2ActivationContext{Schema: ReleaseEvidenceV2ActivationContextSchema, Activation: activation, InitialCut: physical.source.expected, ValidatorUID: 7, Journal: [20]byte{0x33}, RuntimeHash: [32]byte(crypto.Keccak256Hash(self.evm.code)), ObservedEVMBlock: 100, ObservedEVMHash: observed}
			self.contextKVs[op.NoID] = initial
			encoded, err := initial.CanonicalJSON(cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes)
			if err != nil {
				t.Fatal(err)
			}
			input.Context = writeReleaseBootstrapV2TestFile(t, input.Context.Path, encoded)
			payload, err := activation.Payload()
			if err != nil {
				t.Fatal(err)
			}
			input.Activation = writeReleaseBootstrapV2TestFile(t, input.Activation.Path, payload)
			vpk, err := activation.SignVPK(physical.source.key)
			if err != nil {
				t.Fatal(err)
			}
			digest, _ := activation.Digest()
			hotkey, err := production.hotkey.Sign(digest[:])
			if err != nil {
				t.Fatal(err)
			}
			input.VPKSignature = writeReleaseBootstrapV2TestFile(t, input.VPKSignature.Path, vpk)
			input.HotkeySignature = writeReleaseBootstrapV2TestFile(t, input.HotkeySignature.Path, hotkey)
			history, err := (ReleaseEvidenceV2ActivationHistory{Schema: ReleaseEvidenceV2ActivationHistorySchema, LegacyClosures: [][]byte{}}).CanonicalJSON(cfg.EvidenceV2.Bounds.MaxHistoryBytes)
			if err != nil {
				t.Fatal(err)
			}
			input.History = writeReleaseBootstrapV2TestFile(t, input.History.Path, history)
			op.ClientKeySeedFile = filepath.Join(root, fmt.Sprintf("no-%d", op.NoID), "client.seed")
			op.ClientJWTFile = filepath.Join(root, fmt.Sprintf("no-%d", op.NoID), "client.jwt")
			op.NetworkJWTFile = filepath.Join(root, fmt.Sprintf("no-%d", op.NoID), "network.jwt")
			op.ArtifactSigner = common.Address{byte(op.NoID)}.Hex()
			writeReleaseBootstrapV2TestFile(t, op.ClientKeySeedFile, physical.source.key.Seed())
			claims := gojwt.MapClaims{"client_id": physical.source.engine.clientId.String(), "device_id": releaseMeasurementTestID(op.NoID).String(), "exp": time.Now().Add(30 * 24 * time.Hour).Unix()}
			if allowRegistration {
				claims["network_id"], claims["user_id"], claims["roles"], claims["principal"] = releaseMeasurementTestID(201).String(), releaseMeasurementTestID(202).String(), []string{"admin"}, "synthetic-operator"
				bootstrap, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims{"network_id": claims["network_id"], "user_id": claims["user_id"], "roles": claims["roles"], "principal": claims["principal"], "exp": claims["exp"]}).SignedString(gojwt.UnsafeAllowNoneSignatureType)
				if err != nil {
					t.Fatal(err)
				}
				writeReleaseBootstrapV2TestFile(t, op.NetworkJWTFile, []byte(bootstrap))
			}
			credential, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, claims).SignedString(gojwt.UnsafeAllowNoneSignatureType)
			if err != nil {
				t.Fatal(err)
			}
			if clientId, err := clientauth.ClientIdFromJwt(credential); err != nil || clientId != physical.source.engine.clientId {
				t.Fatalf("fixture client token differs from its actual session parser: %v", err)
			}
			writeReleaseBootstrapV2TestFile(t, op.ClientJWTFile, []byte(credential))
			api := &productionStartupApiTestFixture{owner: self, noId: op.NoID, credential: credential, keys: physical.source.server.serverPublicKeys(), objects: map[string][]byte{}, seedRead: make(chan struct{}), release: make(chan struct{}), registrationRead: make(chan struct{})}
			for _, owner := range continuation.inputKVs {
				for hash, raw := range owner.objects.metadataKVs {
					api.objects["metadata/"+hash] = bytes.Clone(raw)
				}
				for kind, objects := range owner.objects.dataKVs {
					for hash, raw := range objects {
						api.objects[kind+"/"+hash] = bytes.Clone(raw)
					}
				}
			}
			server := httptest.NewServer(api)
			t.Cleanup(func() { close(api.release); server.Close() })
			op.APIURL, op.ConnectURL = server.URL, "ws"+strings.TrimPrefix(server.URL, "http")+"/connect"
			self.origins[index] = api
		}
		if configure != nil {
			configure(cfg)
		}
		normalizeProductionStartupTestConfig(t, cfg, root)
	})
	self.native = installProductionContinuationNative(t, self.continuation)
	self.configPath = writeReleaseConfig(t, *self.continuation.production.cfg)
	if _, err := LoadReleaseConfig(self.configPath); err != nil {
		t.Fatalf("complete independently approved startup configuration: %v", err)
	}
	roots := []string{self.continuation.production.cfg.StateDir}
	for _, operator := range self.continuation.production.cfg.Operators {
		roots = append(roots, operator.StateDir)
	}
	for _, operator := range self.continuation.production.cfg.EvidenceV2.Operators {
		roots = append(roots, operator.ReplayScratchRoot, operator.SealScratchRoot, filepath.Dir(operator.Activation.Path))
	}
	self.storage = durablefixture.New(t, t.Context(), roots...)
	return self
}

// Physical declaration is operational fixture input, independent of original
// signed config bytes. Public callers retain their own cancellation lifecycle.
func (self *productionStartupTestFixture) storageContext(ctx context.Context) context.Context {
	return durablepath.WithHost(durablevolume.WithReference(ctx, self.storage.Reference), self.storage.Host)
}

// The independent approver signs precisely the public loader's representation,
// including YAML's omitted/empty list grammar and normalized absolute paths.
// This runs before signing, never rewrites already approved authority bytes.
func normalizeProductionStartupTestConfig(t *testing.T, cfg *ReleaseConfig, root string) {
	t.Helper()
	if err := cfg.normalize(root); err != nil {
		t.Fatal(err)
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var normalized ReleaseConfig
	if err := yaml.Unmarshal(raw, &normalized); err != nil {
		t.Fatal(err)
	}
	if err := normalized.normalize(root); err != nil {
		t.Fatal(err)
	}
	*cfg = normalized
}

// All fixture preparation owners close before the public root opens the same
// physical stores. Canonical files, signatures and original timestamps remain.
func (self *productionStartupTestFixture) closePreparation(t *testing.T) {
	t.Helper()
	for _, input := range self.continuation.inputKVs {
		if err := input.ledger.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// A byte copy is used only for post-join assertions, never to restore an owner
// or impersonate the actual canonical intent reader.
func (self *productionStartupTestFixture) storedIntentBytes(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(self.continuation.steerer.intents.path)
	if err != nil {
		t.Fatal(err)
	}
	return slices.Clone(raw)
}

// A separately signed initial deployment uses genuinely empty private stores.
// The other fixture's retained M8/source/intent files are not copied or deleted;
// only its independently selected native state and public activation are reused.
func (self *productionStartupTestFixture) selectEmptyDeployment(t *testing.T) {
	t.Helper()
	self.closePreparation(t)
	production := self.continuation.production
	cfg := production.cfg
	root := identityTestStateDir(t)
	cfg.StateDir = filepath.Join(root, "coordinator")
	if err := os.Mkdir(cfg.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	for index := range cfg.Operators {
		op := &cfg.Operators[index]
		op.StateDir = filepath.Join(root, fmt.Sprintf("no-%d", op.NoID))
		if err := os.Mkdir(op.StateDir, 0700); err != nil {
			t.Fatal(err)
		}
		input := &cfg.EvidenceV2.Operators[index]
		input.ReplayScratchRoot, input.SealScratchRoot = filepath.Join(root, "scratch", fmt.Sprintf("no-%d", op.NoID), "replay"), filepath.Join(root, "scratch", fmt.Sprintf("no-%d", op.NoID), "seal")
		for _, path := range []string{input.ReplayScratchRoot, input.SealScratchRoot} {
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	admission := production.operator.measurement.admission
	normalizeProductionStartupTestConfig(t, cfg, root)
	var err error
	admission.approval.ConfigHash, err = OwnerRecycleConfigHash(cfg)
	if err != nil {
		t.Fatal(err)
	}
	admission.sign(t)
	if err := loadOwnerRecycleProductionConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionRuntimeHistory(cfg); err != nil {
		t.Fatal(err)
	}
	self.configPath = writeReleaseConfig(t, *cfg)
	if _, err := LoadReleaseConfig(self.configPath); err != nil {
		t.Fatal(err)
	}
	roots := []string{cfg.StateDir}
	for _, operator := range cfg.Operators {
		roots = append(roots, operator.StateDir)
	}
	for _, operator := range cfg.EvidenceV2.Operators {
		roots = append(roots, operator.ReplayScratchRoot, operator.SealScratchRoot, filepath.Dir(operator.Activation.Path))
	}
	self.storage = durablefixture.New(t, t.Context(), roots...)
	for _, operator := range cfg.Operators {
		source := self.continuation.inputKVs[operator.NoID].source
		prepareAttemptLedgerCustodyTest(t, self.storage.Context, operator.StateDir, source.expected.Identity, source.key)
	}
}
