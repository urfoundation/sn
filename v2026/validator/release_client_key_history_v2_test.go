//go:build linux || darwin

// Real HTTP/RPC/descriptor owners consume genuine independent operator
// signatures; deterministic faults replace source bytes, never verifier verdicts.
package validator

import (
	"bytes"
	"context"
	"crypto/ecdsa"
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
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

// One current registration and observation fit this fixture-only wire bound.
// Production still admits the caller's complete independently bounded history.
const releaseClientKeyHistoryTestResponseBytes = 16 * 1024

// Distinct private test owners sign operator statements and publisher wrappers.
func releaseClientKeyTestSigner(t testing.TB, value string) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat(value, 32))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// This independently encodes the public server/startifact wire. The server's
// own integration test sends real production wrappers through the same reader.
func releaseClientKeyTestEnvelope(t testing.TB, domain protocol.ClientKeyHistoryDomain, deployment, kind string, payload []byte) []byte {
	t.Helper()
	key := releaseClientKeyTestSigner(t, "13")
	envelope := protocol.ClientKeyEvidenceEnvelope{Schema: "urnetwork-release-evidence-v1", DeploymentID: deployment, ChainID: domain.ChainID, GenesisHash: releaseHex32(domain.GenesisHash), Netuid: domain.Netuid, Kind: kind, CreatedAt: "2026-01-01T00:00:00Z", Payload: payload, Signer: crypto.PubkeyToAddress(key.PublicKey)}
	unsigned, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(unsigned)
	signature, err := crypto.Sign(hash[:], key)
	if err != nil {
		t.Fatal(err)
	}
	envelope.ContentHash, envelope.Signature = "sha256:"+hex.EncodeToString(hash[:]), "0x"+hex.EncodeToString(signature)
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// A complete contiguous history ends at the exact request-bound observation.
func releaseClientKeyTestResponse(t testing.TB, deployment string, domain protocol.ClientKeyHistoryDomain, request protocol.ClientKeyObservationRequest, keys [][32]byte) []byte {
	t.Helper()
	key := releaseClientKeyTestSigner(t, "12")
	response := protocol.ClientKeyHistoryResponse{}
	var prior *protocol.ClientKeyRegistration
	var registration protocol.ClientKeyRegistration
	for index, publicKey := range keys {
		next := protocol.ClientKeyRegistration{Domain: domain, ClientID: request.ClientID, NetworkID: [16]byte{0x77}, Generation: uint64(index + 1), Present: publicKey != ([32]byte{}), PublicKey: publicKey, EffectiveBoundary: request.DecisionBoundary}
		if prior != nil {
			next.PreviousHash, _ = prior.ContentHash()
		}
		if err := protocol.SignClientKeyRegistration(&next, key); err != nil {
			t.Fatal(err)
		}
		if err := next.Follows(prior); err != nil {
			t.Fatal(err)
		}
		encoded, err := next.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		response.History = append(response.History, releaseClientKeyTestEnvelope(t, domain, deployment, protocol.ClientKeyRegistrationEvidenceKind, encoded))
		registration = next
		prior = &registration
	}
	hash, err := registration.ContentHash()
	if err != nil {
		t.Fatal(err)
	}
	observation := protocol.ClientKeyObservation{Domain: domain, ClientID: request.ClientID, Generation: registration.Generation, RegistrationHash: hash, Request: request}
	if err := protocol.SignClientKeyObservation(&observation, key); err != nil {
		t.Fatal(err)
	}
	encoded, err := observation.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	response.Observation = releaseClientKeyTestEnvelope(t, domain, deployment, protocol.ClientKeyObservationEvidenceKind, encoded)
	encoded, err = json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// Existing head-quality oracles compare the same independent chain facts.
// Dedicated tests below separately require actual new capture byte authority.
func releaseClientKeyTestChainBindings(bindings []ReleaseBindingMeasurement) []ReleaseBindingMeasurement {
	owned := slices.Clone(bindings)
	for index := range owned {
		owned[index].ClientKeyObservationHash = ""
	}
	return owned
}

// Extend the existing raw head RPC fixture with independently encoded root
// and immutable companion values. Binding batches keep their original path.
func releaseHeadV2ClientKeyRPC(artifact *ReleaseMeasurementArtifact, call chainBatchRPCRequest) (any, bool, error) {
	if call.Method == "eth_chainId" {
		return hexutil.EncodeUint64(artifact.ChainID), true, nil
	}
	if call.Method == "chain_getBlockHash" {
		return artifact.GenesisHash, true, nil
	}
	if call.Method == "eth_getBlockByNumber" {
		var tag string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &tag) != nil {
			return nil, true, errors.New("invalid block request")
		}
		if tag == "0x0" {
			return nil, true, nil
		}
		if tag != "finalized" && tag != hexutil.EncodeUint64(artifact.EVMSnapshotBlock) {
			return nil, true, errors.New("unknown finalized block")
		}
		return map[string]any{"number": hexutil.EncodeUint64(artifact.EVMSnapshotBlock), "hash": artifact.EVMSnapshotHash}, true, nil
	}
	if call.Method != "eth_call" {
		return nil, false, nil
	}
	var payload struct {
		To    common.Address
		Data  hexutil.Bytes
		Input hexutil.Bytes
	}
	var selector gethrpc.BlockNumberOrHash
	if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &payload) != nil || json.Unmarshal(call.Params[1], &selector) != nil {
		return nil, true, errors.New("invalid authority call")
	}
	data := payload.Input
	if len(data) == 0 {
		data = payload.Data
	}
	coordinator, companion := stabi.NewSTCoordinator(), stabi.NewSTValidatorEvidence()
	binding := coordinator.PackBindingAt([16]byte{}, big.NewInt(0))
	if len(data) >= 4 && bytes.Equal(data[:4], binding[:4]) {
		return nil, false, nil
	}
	if selector.BlockHash == nil || !selector.RequireCanonical || *selector.BlockHash != common.HexToHash(artifact.EVMSnapshotHash) {
		return nil, true, errors.New("authority selector changed")
	}
	anchor := common.Address{0x77}
	if payload.To == anchor {
		fields := []struct {
			data  []byte
			value [32]byte
		}{
			{data: companion.PackCoordinator(), value: [32]byte(common.BytesToHash(common.HexToAddress(artifact.Coordinator).Bytes()))},
			{data: companion.PackSettlementVault(), value: [32]byte(common.BytesToHash(common.HexToAddress(artifact.SettlementVault).Bytes()))},
			{data: companion.PackChainId(), value: [32]byte(common.BigToHash(new(big.Int).SetUint64(artifact.ChainID)))},
			{data: companion.PackNetuid(), value: [32]byte(common.BigToHash(new(big.Int).SetUint64(uint64(artifact.Netuid))))},
			{data: companion.PackGenesisHash(), value: [32]byte(common.HexToHash(artifact.GenesisHash))},
			{data: companion.PackDeploymentIdHash(), value: sha256.Sum256([]byte(artifact.DeploymentID))},
		}
		for _, field := range fields {
			if bytes.Equal(data, field.data) {
				return hexutil.Encode(field.value[:]), true, nil
			}
		}
		return nil, true, errors.New("unknown companion field")
	}
	if payload.To != common.HexToAddress(artifact.Coordinator) {
		return nil, true, errors.New("wrong authority contract")
	}
	root, err := crypto.HexToECDSA(strings.Repeat("12", 32))
	if err != nil {
		return nil, true, err
	}
	epoch := new(big.Int).SetUint64(artifact.SettlementEpoch)
	methods := []struct {
		method string
		data   []byte
		value  any
	}{
		{method: "validatorEvidence", data: coordinator.PackValidatorEvidence(), value: anchor},
		{method: "currentEpoch", data: coordinator.PackCurrentEpoch(), value: epoch},
		{method: "netuid", data: coordinator.PackNetuid(), value: artifact.Netuid},
		{method: "settlementVault", data: coordinator.PackSettlementVault(), value: common.HexToAddress(artifact.SettlementVault)},
		{method: "policyAt", data: coordinator.PackPolicyAt(epoch), value: stabi.STCoordinatorPolicySnapshot{PolicyHash: common.HexToHash(artifact.PolicyHash), EpochDepositCapRao: big.NewInt(1), CampaignDepositCapRao: big.NewInt(1)}},
	}
	operator := coordinator.PackOperatorAt(big.NewInt(0), epoch)
	if len(data) == 68 && bytes.Equal(data[:4], operator[:4]) && bytes.Equal(data[36:], operator[36:]) {
		methods = append(methods, struct {
			method string
			data   []byte
			value  any
		}{method: "operatorAt", data: data, value: stabi.STCoordinatorOperatorVersion{RootSigner: crypto.PubkeyToAddress(root.PublicKey), Active: true}})
	}
	parsed, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, true, err
	}
	for _, method := range methods {
		if bytes.Equal(data, method.data) {
			encoded, err := parsed.Methods[method.method].Outputs.Pack(method.value)
			return hexutil.Encode(encoded), true, err
		}
	}
	return nil, true, errors.New("unknown authority method")
}

// Each operator serves its own immutable domain and cannot read another
// operator's session. A real reader supplies framing, credentials and Close.
func newReleaseHeadV2ClientKeyReader(t testing.TB, cfg *ReleaseConfig, noID uint64) *HTTPClientKeyHistoryReader {
	t.Helper()
	owned := *cfg
	domain := protocol.ClientKeyHistoryDomain{ChainID: cfg.ChainID, GenesisHash: common.HexToHash(cfg.GenesisHash), Netuid: cfg.Netuid, Coordinator: common.HexToAddress(cfg.Coordinator), SettlementVault: common.HexToAddress(cfg.SettlementVault), DeploymentIDHash: sha256.Sum256([]byte(cfg.DeploymentID)), PolicyHash: common.HexToHash(cfg.PolicyHash), NoID: noID}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method != http.MethodPost || r.URL.Path != "/sn/client-key/observation" && r.URL.Path != "/sn/client-key/observations" || r.Header.Get("Authorization") != "Bearer client-key-test" {
			http.Error(w, "invalid capture", 400)
			return
		}
		if r.URL.Path == "/sn/client-key/observations" {
			encoded, err := io.ReadAll(io.LimitReader(r.Body, protocol.MaxClientKeyObservationBatchRequestBytes+1))
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			batch, err := protocol.DecodeClientKeyObservationBatchRequest(encoded)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			response := protocol.ClientKeyObservationBatchResponse{Responses: make([]json.RawMessage, len(batch.Requests))}
			for index, request := range batch.Requests {
				body := releaseClientKeyTestResponse(t, owned.DeploymentID, domain, request, [][32]byte{{0x31}})
				if len(body) > releaseClientKeyHistoryTestResponseBytes {
					t.Errorf("actual batch member exceeds its admitted fixture bound: %d", len(body))
					http.Error(w, "excessive fixture body", 500)
					return
				}
				response.Responses[index] = body
			}
			body, err := json.Marshal(response)
			if err != nil || uint64(len(body)) > batch.MaximumResponseBytes {
				http.Error(w, "excessive fixture batch", 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
		var args struct {
			ClientID string `json:"client_id"`
			Request  []byte `json:"request"`
		}
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var request protocol.ClientKeyObservationRequest
		if err := json.Unmarshal(args.Request, &request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if connect.Id(request.ClientID).String() != args.ClientID {
			http.Error(w, "wrong client", 400)
			return
		}
		encoded := releaseClientKeyTestResponse(t, owned.DeploymentID, domain, request, [][32]byte{{0x31}})
		if len(encoded) > releaseClientKeyHistoryTestResponseBytes {
			t.Errorf("actual signed-response fixture exceeds its admitted bound: %d", len(encoded))
			http.Error(w, "signed-response fixture exceeds its admitted bound", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(encoded)
	}))
	t.Cleanup(endpoint.Close)
	reader, err := NewHTTPClientKeyHistoryReader(endpoint.URL, func() string { return "client-key-test" })
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

// The actual M8/live collector now returns a hash for every active key, and
// real historical RPC plus retained descriptor custody reproduces those keys.
func TestReleaseClientKeyHistoryLiveHeadRetainsActualCaptureForRecovery(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 15)
	result, err := fixture.gather(t.Context(), fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	custody := &releaseEvidenceV2StartupReferences{remaining: 32 * 1024 * 1024}
	defer custody.close()
	for _, binding := range result.Bindings {
		if !binding.Active {
			continue
		}
		if binding.ClientKeyObservationHash == "" {
			t.Fatal("actual live head omitted its signed source capture")
		}
		clientID, err := connect.ParseId(binding.ClientID)
		if err != nil {
			t.Fatal(err)
		}
		domain, request, err := releaseClientKeyDecisionV2(fixture.steerer.cfg, binding.NoID, fixture.steerer.hotkey.PublicKey(), fixture.measurement.artifact, clientID)
		if err != nil {
			t.Fatal(err)
		}
		// Removing the HTTP credential proves recovery performs no new POST.
		fixture.steerer.contexts[binding.NoID].ClientKeyHistory.byJwt = func() string { return "" }
		registration, err := readRetainedReleaseClientKeyV2(t.Context(), fixture.steerer.chain, custody, fixture.steerer.cfg.StateDir, domain, request, binding.ClientKeyObservationHash, 1024*1024)
		if err != nil || !registration.Present || releaseHex32(registration.PublicKey) != binding.LocalClientKey {
			t.Fatalf("retained real key recovery differs: %v", err)
		}
	}
	if err := errors.Join(custody.check(), custody.close()); err != nil {
		t.Fatal(err)
	}
}

// A new decision cannot accept yesterday's signed response, including a
// transport replay with a valid operator signature and unchanged public key.
func TestReleaseClientKeyHistoryFreshDecisionRejectsOldSignedResponse(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 1)
	binding := fixture.measurement.artifact.Bindings[0]
	clientID, _ := connect.ParseId(binding.ClientID)
	domain, request, err := releaseClientKeyDecisionV2(fixture.steerer.cfg, binding.NoID, fixture.steerer.hotkey.PublicKey(), fixture.measurement.artifact, clientID)
	if err != nil {
		t.Fatal(err)
	}
	old := request
	old.Nonce = [32]byte{3}
	encoded := releaseClientKeyTestResponse(t, fixture.steerer.cfg.DeploymentID, domain, old, [][32]byte{{0x31}})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(encoded)
	}))
	defer endpoint.Close()
	reader, err := NewHTTPClientKeyHistoryReader(endpoint.URL, func() string { return "client-key-test" })
	if err != nil {
		t.Fatal(err)
	}
	result, hash, _, err := captureReleaseClientKeyV2(t.Context(), fixture.steerer.chain, reader, fixture.steerer.cfg.StateDir, domain, request, 1024*1024, 32*1024*1024)
	if err == nil || result != (protocol.ClientKeyRegistration{}) || hash != "" {
		t.Fatal("fresh capture accepted an old request's valid operator signature")
	}
	path, _ := releaseClientKeyCaptureV2Path(fixture.steerer.cfg.StateDir, domain, request)
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replayed response reached durable custody: %v", err)
	}
}

// Canonical history omissions, wrappers from another domain and a changed
// root all fail at the actual retained-byte consumer, before key publication.
func TestReleaseClientKeyHistoryRejectsTamperOmissionAndUnapprovedRoot(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 1)
	binding := fixture.measurement.artifact.Bindings[0]
	clientID, _ := connect.ParseId(binding.ClientID)
	domain, request, err := releaseClientKeyDecisionV2(fixture.steerer.cfg, binding.NoID, fixture.steerer.hotkey.PublicKey(), fixture.measurement.artifact, clientID)
	if err != nil {
		t.Fatal(err)
	}
	request.Nonce = [32]byte{3}
	encoded := releaseClientKeyTestResponse(t, fixture.steerer.cfg.DeploymentID, domain, request, [][32]byte{{0x30}, {0x31}})
	for _, fault := range []string{"omit", "tamper", "domain", "root"} {
		response, err := protocol.DecodeClientKeyHistoryResponse(encoded, 1024*1024)
		if err != nil {
			t.Fatal(err)
		}
		expected := domain
		switch fault {
		case "omit":
			response.History = response.History[1:]
		case "tamper":
			response.History[0][len(response.History[0])/2] ^= 1
		case "domain":
			expected.NoID++
		case "root":
			wrapper, err := protocol.DecodeClientKeyEvidence(response.History[1], domain, protocol.ClientKeyRegistrationEvidenceKind)
			if err != nil {
				t.Fatal(err)
			}
			registration, err := protocol.DecodeClientKeyRegistration(wrapper.Payload)
			if err != nil {
				t.Fatal(err)
			}
			if err := protocol.SignClientKeyRegistration(&registration, releaseClientKeyTestSigner(t, "14")); err != nil {
				t.Fatal(err)
			}
			payload, err := registration.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			response.History[1] = releaseClientKeyTestEnvelope(t, domain, fixture.steerer.cfg.DeploymentID, protocol.ClientKeyRegistrationEvidenceKind, payload)
		}
		candidate, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := verifyReleaseClientKeyCaptureV2(t.Context(), fixture.steerer.chain, candidate, 1024*1024, expected, request, false); err == nil || result != (protocol.ClientKeyRegistration{}) {
			t.Fatalf("%s source fault reached a key consumer", fault)
		}
	}
}

// A refused or canceled actual body Close cannot leave apparently usable
// response bytes. The late hook runs deterministically, after full body read.
type clientKeyHistoryTestRoundTripper func(*http.Request) (*http.Response, error)

func (self clientKeyHistoryTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

type clientKeyHistoryTestBody struct {
	io.Reader
	close func() error
}

func (self *clientKeyHistoryTestBody) Close() error { return self.close() }

func TestReleaseClientKeyHistoryHTTPJoinsLateCloseCancellationAndBounds(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 1)
	binding := fixture.measurement.artifact.Bindings[0]
	clientID, _ := connect.ParseId(binding.ClientID)
	domain, request, err := releaseClientKeyDecisionV2(fixture.steerer.cfg, binding.NoID, fixture.steerer.hotkey.PublicKey(), fixture.measurement.artifact, clientID)
	if err != nil {
		t.Fatal(err)
	}
	request.Nonce = [32]byte{3}
	encoded := releaseClientKeyTestResponse(t, fixture.steerer.cfg.DeploymentID, domain, request, [][32]byte{{0x31}})
	for _, fault := range []string{"close", "cancel", "oversize", "redirect", "logout"} {
		ctx, cancel := context.WithCancel(t.Context())
		var closed atomic.Int32
		credential := "client-key-test"
		reader, err := NewHTTPClientKeyHistoryReader("http://example.test", func() string { return credential })
		if err != nil {
			t.Fatal(err)
		}
		failure := fmt.Errorf("test-owned %s", fault)
		reader.client.Transport = clientKeyHistoryTestRoundTripper(func(*http.Request) (*http.Response, error) {
			status := http.StatusOK
			if fault == "redirect" {
				status = http.StatusTemporaryRedirect
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, ContentLength: int64(len(encoded)), Body: &clientKeyHistoryTestBody{Reader: bytes.NewReader(encoded), close: func() error {
				closed.Add(1)
				if fault == "cancel" {
					cancel()
				}
				if fault == "logout" {
					credential = ""
				}
				if fault == "close" {
					return failure
				}
				return nil
			}}}, nil
		})
		maximum := uint64(1024 * 1024)
		if fault == "oversize" {
			maximum = 1
		}
		result, err := reader.Read(ctx, request, maximum)
		cancel()
		if err == nil || result != nil || closed.Load() != 1 {
			t.Fatalf("%s late HTTP failure published usable bytes: %v", fault, err)
		}
		if fault == "close" && !errors.Is(err, failure) || fault == "cancel" && !errors.Is(err, context.Canceled) {
			t.Fatalf("%s failure lost its cause: %v", fault, err)
		}
	}
}
