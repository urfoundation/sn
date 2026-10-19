// The actual coordinator HTTP fixture must encode every selected generated ABI
// tuple. An absent big integer is not an encoded zero and must not turn fixture
// construction into a production retry-budget or artifact scheduling failure.
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/v2026/stabi"
)

// A single actual HTTP exchange exposes the original handler panic directly;
// the one-minute bound is test liveness, not a changed production read budget.
func economicEntitlementTupleRead(t *testing.T, f *economicEntitlementFixture, name string, args ...any) []any {
	t.Helper()
	data, err := f.coordinator.Pack(name, args...)
	if err != nil {
		t.Fatal("original coordinator calldata", name, err)
	}
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "eth_call", "params": []any{map[string]any{"to": f.address.Hex(), "data": hexutil.Encode(data)}, map[string]any{"blockHash": f.source.vault.blocks[11].header.Hash().Hex(), "requireCanonical": true}}})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.source.vault.url, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: time.Minute}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("complete ABI coordinator fixture refused bounded original HTTP", name, err)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || len(body) > 4096 {
		t.Fatal("complete ABI coordinator fixture returned invalid HTTP", name, response.StatusCode, readErr, closeErr)
	}
	var reply struct {
		Id     int             `json:"id"`
		Result string          `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &reply); err != nil || reply.Id != 1 || len(reply.Error) != 0 {
		t.Fatal("complete ABI coordinator response envelope", name, err, string(body))
	}
	encoded, err := hexutil.Decode(reply.Result)
	if err != nil {
		t.Fatal("complete ABI coordinator return bytes", name, err)
	}
	values, err := f.coordinator.Methods[name].Outputs.Unpack(encoded)
	if err != nil {
		t.Fatal("complete ABI coordinator tuple decode", name, err)
	}
	return values
}

// The policy tuple is decoded through the pinned generated binding. Adjacent
// getters still retain their actual commitment, signer and epoch identities.
func TestEconomicEntitlementCoordinatorFixtureEncodesCompleteAbiTuples(t *testing.T) {
	f := newEconomicEntitlementFixture(t)
	values := economicEntitlementTupleRead(t, f, "policyAt", big.NewInt(3))
	if len(values) != 1 {
		t.Fatal("coordinator policy did not return one complete tuple", values)
	}
	policy, ok := abi.ConvertType(values[0], new(stabi.STCoordinatorPolicySnapshot)).(*stabi.STCoordinatorPolicySnapshot)
	if !ok || policy == nil || policy.PolicyHash != [32]byte(common.HexToHash(f.artifact.PolicyHash)) || policy.RootCommitWindowBlocks != 10 || policy.EpochDepositCapRao == nil || policy.CampaignDepositCapRao == nil || policy.EpochDepositCapRao.Sign() != 0 || policy.CampaignDepositCapRao.Sign() != 0 {
		t.Fatal("original policy tuple lost explicit zero caps or selected identity", policy)
	}
	values = economicEntitlementTupleRead(t, f, "operatorAt", big.NewInt(1), big.NewInt(3))
	if len(values) != 1 {
		t.Fatal("coordinator operator did not return one complete tuple", values)
	}
	operator, ok := abi.ConvertType(values[0], new(stabi.STCoordinatorOperatorVersion)).(*stabi.STCoordinatorOperatorVersion)
	if !ok || operator == nil || operator.RootSigner != f.rootSigner || operator.EffectiveEpoch != 1 || !operator.Active || operator.RootSigner == f.artifact.Signer {
		t.Fatal("original operator tuple lost independent commitment identity", operator)
	}
	values = economicEntitlementTupleRead(t, f, "rootCommitments", big.NewInt(3), big.NewInt(1))
	if !reflect.DeepEqual(values, []any{f.committedRoot, f.committedHash, f.rootSigner, uint64(11)}) {
		t.Fatal("original commitment tuple changed", values)
	}
	values = economicEntitlementTupleRead(t, f, "settlementVault")
	if !reflect.DeepEqual(values, []any{common.HexToAddress(f.source.policy.Vault.Address)}) {
		t.Fatal("original selected vault changed", values)
	}
	for _, boundary := range []struct {
		method string
		value  int64
	}{{method: "epochStartBlock", value: 0}, {method: "epochEndBlock", value: 10}} {
		values = economicEntitlementTupleRead(t, f, boundary.method, big.NewInt(3))
		if len(values) != 1 {
			t.Fatal("original epoch boundary count changed", boundary.method, values)
		}
		value, ok := values[0].(*big.Int)
		if !ok || value == nil || value.Cmp(big.NewInt(boundary.value)) != 0 {
			t.Fatal("original epoch boundary changed", boundary.method, values)
		}
	}
	if f.artifactReads.Load() != 0 {
		t.Fatal("coordinator tuple control unexpectedly acquired an artifact")
	}
}
