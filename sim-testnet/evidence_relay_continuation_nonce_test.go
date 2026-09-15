//go:build linux || darwin

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Each actual reader accepts only nonce reads and records the exact selector.
// Response maps are fixed before any synchronous observation begins.
type evidenceRelayNonceRpcFixture struct {
	stateLock sync.Mutex
	reads     []string
	nonces    map[string]uint64
	failure   string
}

func (self *evidenceRelayNonceRpcFixture) client(t *testing.T) *ethclient.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var call struct {
			Id     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		var address, selector string
		if json.NewDecoder(request.Body).Decode(&call) != nil || call.Method != "eth_getTransactionCount" || len(call.Params) != 2 || json.Unmarshal(call.Params[0], &address) != nil || json.Unmarshal(call.Params[1], &selector) != nil {
			t.Errorf("nonce census sent a non-read or malformed request: %s", call.Method)
			http.Error(writer, "unexpected nonce request", http.StatusBadRequest)
			return
		}
		key := common.HexToAddress(address).Hex() + "/" + selector
		func() {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			self.reads = append(self.reads, key)
		}()
		response := map[string]any{"jsonrpc": "2.0", "id": call.Id}
		if key == self.failure {
			response["error"] = map[string]any{"code": -32000, "message": "nonce fixture unavailable"}
		} else if nonce, ok := self.nonces[key]; ok {
			response["result"] = fmt.Sprintf("0x%x", nonce)
		} else {
			t.Errorf("nonce census read an unapproved address or block: %s", key)
			response["error"] = map[string]any{"code": -32000, "message": "unexpected nonce checkpoint"}
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	client, err := ethclient.DialContext(t.Context(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func (self *evidenceRelayNonceRpcFixture) observedReads() []string {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return slices.Clone(self.reads)
}

func evidenceRelayNonceTest(t *testing.T, mode string) (*Executor, fleetRenewalExposure, *evidenceRelayNonceRpcFixture) {
	t.Helper()
	roles := &RoleSecrets{EVM: map[string]EVMRoleSecret{}}
	exposure := fleetRenewalExposure{Nonces: map[common.Address]map[uint64]bool{}}
	reader := &evidenceRelayNonceRpcFixture{nonces: map[string]uint64{}}
	for index, label := range []string{"keeper", "owner"} {
		address := common.Address{19: byte(index + 1)}
		roles.EVM[label] = EVMRoleSecret{Label: label, Address: address.Hex()}
		exposure.Nonces[address] = map[uint64]bool{0: true}
		for _, selector := range []string{"0x28", "latest", "pending"} {
			reader.nonces[address.Hex()+"/"+selector] = 1
		}
	}
	return &Executor{cfg: &ResolvedConfig{OperationalRPCMode: mode}, roles: roles, keeper: &EvmTxManager{client: reader.client(t)}}, exposure, reader
}

func TestEvidenceRelayContinuationNonceOwnedRouteUsesOneObserver(t *testing.T) {
	t.Parallel()
	executor, exposure, operational := evidenceRelayNonceTest(t, rpcModeOwnedNode)
	// This synthetic private authority exercises real owned-route validation;
	// actual reads use the local Http fixture and never dial that authority.
	cfg, err := prepareOwnedRPCConfiguration(ownedRPCSourceConfigTest(t), "10.91.92.93:9944")
	if err != nil {
		t.Fatal(err)
	}
	executor.cfg = cfg
	points, err := executor.observeEvidenceRelayContinuationNonces(t.Context(), exposure, 40)
	if err != nil {
		t.Fatalf("owned nonce census incorrectly required another reader: %v", err)
	}
	if independentRPCRequired(cfg) || executor.independentEVM != nil || cfg.OperationalRPCMode != rpcModeOwnedNode {
		t.Fatal("owned observation acquired an independent-reader claim")
	}
	want := []FleetRenewalNonce{
		{Role: "keeper", Address: common.Address{19: 1}, Finalized: 1, Latest: 1, Pending: 1},
		{Role: "owner", Address: common.Address{19: 2}, Finalized: 1, Latest: 1, Pending: 1},
	}
	if !reflect.DeepEqual(points, want) {
		t.Fatalf("owned nonce census lost complete role observations: %+v", points)
	}
	var reads []string
	for _, point := range want {
		for _, selector := range []string{"0x28", "latest", "pending"} {
			reads = append(reads, point.Address.Hex()+"/"+selector)
		}
	}
	if !slices.Equal(operational.observedReads(), reads) {
		t.Fatal("owned nonce census changed its finalized, latest, or pending reads")
	}
}

func TestEvidenceRelayContinuationNonceSharedModesDoNotUseAnExtraReader(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{rpcModeOwnedNode, rpcModePublicOverride} {
		executor, exposure, _ := evidenceRelayNonceTest(t, mode)
		extra := &evidenceRelayNonceRpcFixture{nonces: map[string]uint64{}}
		executor.independentEVM = extra.client(t)
		points, err := executor.observeEvidenceRelayContinuationNonces(t.Context(), exposure, 40)
		if err != nil || len(points) != 2 || len(extra.observedReads()) != 0 || independentRPCRequired(executor.cfg) {
			t.Fatalf("%s used an extra observer or lost the operational census: %v", mode, err)
		}
	}
}

func TestEvidenceRelayContinuationNoncePrivateAuthorityRequiresIndependentReader(t *testing.T) {
	t.Parallel()
	executor, exposure, operational := evidenceRelayNonceTest(t, rpcModePrivateAuthority)
	points, err := executor.observeEvidenceRelayContinuationNonces(t.Context(), exposure, 40)
	if err == nil || !strings.Contains(err.Error(), "requires the independent reader") || points != nil || len(operational.observedReads()) != 0 {
		t.Fatalf("private-authority nonce census accepted a missing reader: %v", err)
	}
}

func TestEvidenceRelayContinuationNoncePrivateAuthorityComparesPinnedState(t *testing.T) {
	t.Parallel()
	for _, mismatch := range []string{"", "nonce", "unavailable"} {
		executor, exposure, _ := evidenceRelayNonceTest(t, rpcModePrivateAuthority)
		first := common.Address{19: 1}.Hex() + "/0x28"
		second := common.Address{19: 2}.Hex() + "/0x28"
		independent := &evidenceRelayNonceRpcFixture{nonces: map[string]uint64{first: 1, second: 1}}
		if mismatch == "nonce" {
			independent.nonces[first] = 2
		} else if mismatch == "unavailable" {
			independent.failure = first
		}
		executor.independentEVM = independent.client(t)
		points, err := executor.observeEvidenceRelayContinuationNonces(t.Context(), exposure, 40)
		if mismatch == "" {
			if err != nil || len(points) != 2 || !slices.Equal(independent.observedReads(), []string{first, second}) {
				t.Fatalf("independent nonce census did not read the exact finalized checkpoint: %v", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "independent finalized nonce differs") || points != nil || !slices.Equal(independent.observedReads(), []string{first}) {
			t.Fatalf("%s independent nonce result was accepted: %v", mismatch, err)
		}
	}
}

func TestEvidenceRelayContinuationNonceOwnedRouteRetainsLiabilityChecks(t *testing.T) {
	t.Parallel()
	for _, mutation := range []struct {
		name string
		want string
	}{
		{name: "missing signed history", want: "has no retained signed transaction"},
		{name: "unfinalized latest", want: "still has unfinalized nonce liability"},
		{name: "unfinalized pending", want: "still has unfinalized nonce liability"},
		{name: "unused retained signature", want: "already owns retained signed nonce"},
		{name: "foreign signer", want: "non-deployment signer"},
		{name: "nonce bound", want: "exceeds its bound"},
		{name: "read failure", want: "nonce fixture unavailable"},
	} {
		executor, exposure, operational := evidenceRelayNonceTest(t, rpcModeOwnedNode)
		address := common.Address{19: 1}
		switch mutation.name {
		case "missing signed history":
			delete(exposure.Nonces[address], 0)
		case "unfinalized latest":
			operational.nonces[address.Hex()+"/latest"] = 2
			operational.nonces[address.Hex()+"/pending"] = 2
			exposure.Nonces[address][1] = true
		case "unfinalized pending":
			operational.nonces[address.Hex()+"/pending"] = 2
			exposure.Nonces[address][1] = true
		case "unused retained signature":
			exposure.Nonces[address][1] = true
		case "foreign signer":
			exposure.Nonces[common.Address{19: 3}] = map[uint64]bool{0: true}
		case "nonce bound":
			for _, selector := range []string{"0x28", "latest", "pending"} {
				operational.nonces[address.Hex()+"/"+selector] = 20001
			}
		case "read failure":
			operational.failure = address.Hex() + "/0x28"
		}
		points, err := executor.observeEvidenceRelayContinuationNonces(t.Context(), exposure, 40)
		if err == nil || !strings.Contains(err.Error(), mutation.want) || points != nil {
			t.Fatalf("%s bypassed owned nonce accounting: %v", mutation.name, err)
		}
	}
}
