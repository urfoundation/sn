package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type doctorRPCFixture struct {
	parsed       abi.ABI
	acceptBadSig bool
	wrongUID     bool
}

func (f *doctorRPCFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var request struct {
		JSONRPC string            `json:"jsonrpc"`
		ID      json.RawMessage   `json:"id"`
		Method  string            `json:"method"`
		Params  []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	response := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(request.ID)}
	if request.Method != "eth_call" || len(request.Params) < 1 {
		response["error"] = map[string]any{"code": -32601, "message": "unsupported"}
		_ = json.NewEncoder(w).Encode(response)
		return
	}
	var call struct {
		To    string `json:"to"`
		Data  string `json:"data"`
		Input string `json:"input"`
	}
	if err := json.Unmarshal(request.Params[0], &call); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	input := call.Input
	if input == "" {
		input = call.Data
	}
	data, err := hex.DecodeString(strings.TrimPrefix(input, "0x"))
	if err != nil || len(data) < 4 {
		response["error"] = map[string]any{"code": -32602, "message": "bad calldata"}
		_ = json.NewEncoder(w).Encode(response)
		return
	}
	method, err := f.parsed.MethodById(data[:4])
	if err != nil {
		response["error"] = map[string]any{"code": -32602, "message": err.Error()}
		_ = json.NewEncoder(w).Encode(response)
		return
	}
	var output []byte
	switch method.Name {
	case "verify":
		args, unpackErr := method.Inputs.Unpack(data[4:])
		if unpackErr != nil {
			err = unpackErr
			break
		}
		s := args[3].([32]byte)
		address := common.HexToAddress(call.To)
		want := doctorEdKAT[3]
		if address == common.HexToAddress("0x403") {
			want = doctorSrKAT[3]
		}
		accepted := s == want || f.acceptBadSig
		output, err = method.Outputs.Pack(accepted)
	case "getUidCount":
		output, err = method.Outputs.Pack(uint16(3))
	case "getHotkey":
		output, err = method.Outputs.Pack([32]byte{1})
	case "getColdkey":
		output, err = method.Outputs.Pack([32]byte{2})
	case "getUid":
		args, unpackErr := method.Inputs.Unpack(data[4:])
		if unpackErr != nil {
			err = unpackErr
			break
		}
		hotkey := args[1].([32]byte)
		exists := hotkey == ([32]byte{1})
		uid := uint16(0)
		if f.wrongUID && exists {
			uid = 1
		}
		output, err = method.Outputs.Pack(exists, uid)
	case "getStake":
		output, err = method.Outputs.Pack(big.NewInt(9))
	case "getNominatorMinRequiredStake":
		output, err = method.Outputs.Pack(big.NewInt(7))
	default:
		err = errors.New("unsupported fixture method")
	}
	if err != nil {
		response["error"] = map[string]any{"code": -32000, "message": err.Error()}
	} else {
		response["result"] = "0x" + hex.EncodeToString(output)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func newDoctorRPCFixture(t *testing.T) (*doctorRPCFixture, *ethclient.Client, func()) {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(doctorPrecompileABI))
	if err != nil {
		t.Fatal(err)
	}
	fixture := &doctorRPCFixture{parsed: parsed}
	server := httptest.NewServer(fixture)
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return fixture, client, func() {
		client.Close()
		server.Close()
	}
}

func TestDoctorReadOnlyPrecompileBattery(t *testing.T) {
	fixture, client, closeFixture := newDoctorRPCFixture(t)
	defer closeFixture()
	cfg := testResolvedConfig(t)
	ctx := context.Background()
	if err := checkDoctorSignaturePrecompiles(ctx, client, fixture.parsed, 10); err != nil {
		t.Fatal(err)
	}
	if detail, err := checkDoctorIdentityPrecompiles(ctx, client, fixture.parsed, 10, cfg); err != nil || !strings.Contains(detail, "uid_count=3") {
		t.Fatalf("identity battery: detail=%q err=%v", detail, err)
	}
	if detail, err := checkDoctorStakingPrecompile(ctx, client, fixture.parsed, 10, cfg); err != nil || !strings.Contains(detail, "nominator_minimum_rao=7") {
		t.Fatalf("staking battery: detail=%q err=%v", detail, err)
	}
}

func TestDoctorPrecompileBatteryRejectsBadControls(t *testing.T) {
	fixture, client, closeFixture := newDoctorRPCFixture(t)
	defer closeFixture()
	fixture.acceptBadSig = true
	if err := checkDoctorSignaturePrecompiles(context.Background(), client, fixture.parsed, 10); err == nil {
		t.Fatal("signature battery accepted a precompile that verifies tampered signatures")
	}
	fixture.acceptBadSig = false
	fixture.wrongUID = true
	if _, err := checkDoctorIdentityPrecompiles(context.Background(), client, fixture.parsed, 10, testResolvedConfig(t)); err == nil {
		t.Fatal("identity battery accepted a mismatched neuron UID")
	}
}

func TestIndependentRPCEndpointsMustBeDistinct(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Public.Chain.SubstratePublicReadEndpoint = "wss://public-substrate.example:443"
	cfg.Public.Chain.EVMPublicReadEndpoint = "https://public-evm.example"
	if err := validateIndependentRPCEndpoints(cfg); err != nil {
		t.Fatal(err)
	}

	cfg.Public.Chain.SubstratePublicReadEndpoint = "ws://127.0.0.1:9944"
	if err := validateIndependentRPCEndpoints(cfg); err == nil || !strings.Contains(err.Error(), "must not resolve") {
		t.Fatalf("private Substrate endpoint was accepted as independent: %v", err)
	}

	cfg.Public.Chain.SubstratePublicReadEndpoint = "wss://public-substrate.example:443"
	cfg.Public.Chain.EVMPublicReadEndpoint = "http://127.0.0.1:9944"
	if err := validateIndependentRPCEndpoints(cfg); err == nil || !strings.Contains(err.Error(), "must not resolve") {
		t.Fatalf("private EVM endpoint was accepted as independent: %v", err)
	}
}
