// Deterministic contract-state tests for stopped-topology policy migration.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/stabi"
)

// Serve a finalized block plus an explicit calldata-to-result table. An
// unplanned contract read fails at the RPC boundary.
type policyRevisionRPCFixture struct {
	t         *testing.T
	stateLock sync.Mutex
	outputs   map[string]string
}

// Accept only the finalized-header and read-only call shapes used by the
// migration verifier.
func (self *policyRevisionRPCFixture) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.t.Helper()
	defer request.Body.Close()
	var call struct {
		ID     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
	switch call.Method {
	case "eth_getBlockByNumber":
		response["result"] = map[string]any{"number": "0x64", "hash": "0x" + strings.Repeat("64", 32)}
	case "eth_call":
		if len(call.Params) != 2 || string(call.Params[1]) != `"0x64"` {
			response["error"] = map[string]any{"code": -32602, "message": "policy migration call is not block-pinned"}
			break
		}
		var envelope struct {
			Data  string `json:"data"`
			Input string `json:"input"`
		}
		if err := json.Unmarshal(call.Params[0], &envelope); err != nil {
			response["error"] = map[string]any{"code": -32602, "message": "malformed policy migration call"}
			break
		}
		data := envelope.Data
		if data == "" {
			data = envelope.Input
		}
		self.stateLock.Lock()
		result, ok := self.outputs[strings.ToLower(data)]
		self.stateLock.Unlock()
		if !ok {
			response["error"] = map[string]any{"code": -32601, "message": "unplanned policy migration call"}
			break
		}
		response["result"] = result
	default:
		response["error"] = map[string]any{"code": -32601, "message": "unexpected method"}
	}
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		self.t.Errorf("encode policy migration RPC response: %v", err)
	}
}

// Encode one exact call and its ABI return value into the fixture table.
func addPolicyRevisionRPCOutput(t *testing.T, outputs map[string]string, parsed abi.ABI, method string, args []any, values ...any) string {
	t.Helper()
	data, err := parsed.Pack(method, args...)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parsed.Methods[method].Outputs.Pack(values...)
	if err != nil {
		t.Fatal(err)
	}
	key := "0x" + hex.EncodeToString(data)
	outputs[strings.ToLower(key)] = "0x" + hex.EncodeToString(result)
	return strings.ToLower(key)
}

func TestPolicyRevisionOnChainAuthenticatesEveryPreCampaignBalance(t *testing.T) {
	cfg, stateDir, prior, _, entries, recovery := testVoluntaryConvictionDuplicateRecovery(t)
	previous, previousHash := previousAcceleratedPolicy(t, cfg)
	prior.PolicyHash = previousHash
	recovery.PolicyHash = previousHash
	recovery.OriginalPlanPolicyHash = previousHash
	recovery.DuplicatePlanPolicyHash = previousHash
	recovery.OriginalEvidence.PolicyHash = previousHash

	coordinatorAddress := common.HexToAddress("0x100")
	reserveAddress := common.HexToAddress("0x200")
	vaultAddress := common.HexToAddress("0x300")
	if err := saveContractDeployment(stateDir, ContractDeployment{
		Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		CoordinatorProxy: coordinatorAddress, ReserveSink: reserveAddress, SettlementVault: vaultAddress,
	}); err != nil {
		t.Fatal(err)
	}
	coordinator, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	reserve, err := abi.JSON(strings.NewReader(ReserveSinkABI))
	if err != nil {
		t.Fatal(err)
	}
	vault, err := abi.JSON(strings.NewReader(SettlementVaultABI))
	if err != nil {
		t.Fatal(err)
	}
	policyHash, err := decodeHash(previousHash)
	if err != nil {
		t.Fatal(err)
	}
	activePolicy := stabi.STCoordinatorPolicySnapshot{
		PolicyHash: policyHash, EffectiveEpoch: 0, EffectiveBlock: 1,
		EpochBlocks: previous.Settlement.EpochBlocks, RootCommitWindowBlocks: previous.Settlement.RootCommitWindowBlocks,
		FinalizeOffsetBlocks: previous.Settlement.FinalizeOffsetBlocks, CloseGraceBlocks: previous.Settlement.CloseGraceBlocks,
		ClaimTTLEpochs: previous.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: previous.Settlement.ClaimGraceEpochs,
		MaximumBindingValidityEpochs: previous.Binding.MaximumValidityEpochs, CommitmentMaxAgeBlocks: previous.Settlement.EpochBlocks * 2,
		EpochDepositCapRao:    new(big.Int).SetUint64(previous.Deposit.EpochCapRaoPerOperator),
		CampaignDepositCapRao: new(big.Int).SetUint64(previous.Deposit.TotalTestCampaignCapRao),
	}
	reserved := new(big.Int).SetUint64(recovery.CumulativeAfterRao)
	zero := new(big.Int)
	outputs := map[string]string{}
	addPolicyRevisionRPCOutput(t, outputs, coordinator, "currentEpoch", nil, big.NewInt(7))
	policyKey := addPolicyRevisionRPCOutput(t, outputs, coordinator, "policyAt", []any{big.NewInt(7)}, activePolicy)
	countKey := addPolicyRevisionRPCOutput(t, outputs, coordinator, "policyCount", nil, big.NewInt(1))
	operatorCountKey := addPolicyRevisionRPCOutput(t, outputs, coordinator, "operatorCount", nil, big.NewInt(int64(cfg.Config.Topology.Operators)))
	reservedKey := addPolicyRevisionRPCOutput(t, outputs, coordinator, "campaignReserved", nil, reserved)
	principalKey := addPolicyRevisionRPCOutput(t, outputs, reserve, "principal", nil, reserved)
	liveStakeKey := addPolicyRevisionRPCOutput(t, outputs, reserve, "liveStake", nil, new(big.Int).Add(reserved, big.NewInt(1)))
	vaultKeys := map[string]string{}
	for _, method := range []string{"totalCaptured", "totalPaid", "escrowAccounted", "pendingFunding", "outstandingLiability"} {
		vaultKeys[method] = addPolicyRevisionRPCOutput(t, outputs, vault, method, nil, zero)
	}
	operatorPrincipalKeys := map[int]string{}
	operatorIDKeys := map[int]string{}
	convictionKeys := map[int]string{}
	nonceKeys := map[int]string{}
	for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
		amount := zero
		nonce := zero
		if noID == 1 {
			amount = reserved
			nonce = big.NewInt(3)
		}
		argument := []any{big.NewInt(int64(noID))}
		operatorIDKeys[noID] = addPolicyRevisionRPCOutput(t, outputs, coordinator, "operatorIdAt", []any{big.NewInt(int64(noID - 1))}, big.NewInt(int64(noID)))
		operatorPrincipalKeys[noID] = addPolicyRevisionRPCOutput(t, outputs, reserve, "operatorPrincipal", argument, amount)
		convictionKeys[noID] = addPolicyRevisionRPCOutput(t, outputs, coordinator, "cumulativeConviction", argument, amount)
		nonceKeys[noID] = addPolicyRevisionRPCOutput(t, outputs, coordinator, "nextDepositNonce", argument, nonce)
	}
	fixture := &policyRevisionRPCFixture{t: t, outputs: outputs}
	server := httptest.NewServer(fixture)
	defer server.Close()
	cfg.OperationalRPCMode = rpcModePrivateAuthority
	cfg.OperationalEVM = server.URL
	decision := policyRevisionDecision{Class: policyRevisionFutureAcceleration, PreviousPolicy: previous}
	recoveries := planRevisionRecoveries{VoluntaryConvictions: []voluntaryConvictionDuplicateRecovery{recovery}}
	validate := func() error {
		return validatePolicyRevisionOnChain(context.Background(), cfg, stateDir, prior, entries, decision, recoveries)
	}
	if err := validate(); err != nil {
		t.Fatal(err)
	}
	base := make(map[string]string, len(outputs))
	for key, value := range outputs {
		base[key] = value
	}
	mutations := []struct {
		name  string
		key   string
		abi   abi.ABI
		value any
		want  string
	}{
		{name: "active policy", key: policyKey, abi: coordinator, value: func() stabi.STCoordinatorPolicySnapshot { value := activePolicy; value.EpochBlocks++; return value }(), want: "active accelerated policy"},
		{name: "policy count", key: countKey, abi: coordinator, value: big.NewInt(2), want: "policyCount"},
		{name: "operator count", key: operatorCountKey, abi: coordinator, value: big.NewInt(3), want: "operatorCount"},
		{name: "campaign reserve", key: reservedKey, abi: coordinator, value: new(big.Int).Add(reserved, big.NewInt(1)), want: "campaignReserved"},
		{name: "reserve principal", key: principalKey, abi: reserve, value: new(big.Int).Sub(reserved, big.NewInt(1)), want: "reserve principal"},
		{name: "live stake", key: liveStakeKey, abi: reserve, value: new(big.Int).Sub(reserved, big.NewInt(1)), want: "liveStake"},
		{name: "vault liability", key: vaultKeys["outstandingLiability"], abi: vault, value: big.NewInt(1), want: "outstandingLiability"},
		{name: "operator identity", key: operatorIDKeys[2], abi: coordinator, value: big.NewInt(3), want: "operatorIdAt(1)"},
		{name: "operator principal", key: operatorPrincipalKeys[2], abi: reserve, value: big.NewInt(1), want: "operator 2 principal"},
		{name: "operator conviction", key: convictionKeys[2], abi: coordinator, value: big.NewInt(1), want: "operator 2 cumulative conviction"},
		{name: "operator nonce", key: nonceKeys[1], abi: coordinator, value: big.NewInt(2), want: "operator 1 nonce"},
	}
	for _, mutation := range mutations {
		method := ""
		for name, candidate := range mutation.abi.Methods {
			if strings.HasPrefix(mutation.key, "0x"+hex.EncodeToString(candidate.ID)) {
				method = name
				break
			}
		}
		if method == "" {
			t.Fatalf("%s mutation has no ABI method", mutation.name)
		}
		result, err := mutation.abi.Methods[method].Outputs.Pack(mutation.value)
		if err != nil {
			t.Fatal(err)
		}
		fixture.stateLock.Lock()
		for key, value := range base {
			outputs[key] = value
		}
		outputs[mutation.key] = "0x" + hex.EncodeToString(result)
		fixture.stateLock.Unlock()
		if err := validate(); err == nil || !strings.Contains(err.Error(), mutation.want) {
			t.Errorf("%s mutation error=%v, want %q", mutation.name, err, mutation.want)
		}
	}
	fixture.stateLock.Lock()
	for key, value := range base {
		outputs[key] = value
	}
	fixture.stateLock.Unlock()
	approvedCap := cfg.Policy.Deposit.TotalTestCampaignCapRao
	cfg.Policy.Deposit.TotalTestCampaignCapRao = recovery.CumulativeAfterRao - 1
	if err := validate(); err == nil || !strings.Contains(err.Error(), "below authenticated reserve") {
		t.Errorf("underfunded future campaign cap error=%v", err)
	}
	cfg.Policy.Deposit.TotalTestCampaignCapRao = approvedCap
}
