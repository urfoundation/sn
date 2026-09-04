package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Supplies deterministic finalized and numbered EVM blocks without a
// network or timing dependency.
type evmFinalityFixture struct {
	finalized       ChainHead
	canonical       map[uint64]ChainHead
	receipt         *types.Receipt
	receiptError    error
	headerRequests  []int64
	receiptRequests []common.Hash
}

func (self *evmFinalityFixture) EVMBlockByNumber(_ context.Context, number *big.Int) (ChainHead, error) {
	if number == nil || !number.IsInt64() {
		return ChainHead{}, ethereum.NotFound
	}
	self.headerRequests = append(self.headerRequests, number.Int64())
	if number.Sign() < 0 {
		if self.finalized.Number == 0 || self.finalized.Hash == "" {
			return ChainHead{}, ethereum.NotFound
		}
		return self.finalized, nil
	}
	head, ok := self.canonical[number.Uint64()]
	if !ok {
		return ChainHead{}, ethereum.NotFound
	}
	return head, nil
}

func (self *evmFinalityFixture) TransactionReceipt(_ context.Context, hash common.Hash) (*types.Receipt, error) {
	self.receiptRequests = append(self.receiptRequests, hash)
	return self.receipt, self.receiptError
}

// Construct distinct explicit RPC heads without implying that their hashes can
// be recomputed from a standard Ethereum header.
func testEVMHead(number uint64, marker byte) ChainHead {
	return ChainHead{Number: number, Hash: common.BytesToHash([]byte{marker}).Hex()}
}

func TestEVMFundingDeltaAccountsForRuntimeExistentialDeposit(t *testing.T) {
	wei := func(rao uint64) *big.Int {
		return new(big.Int).Mul(new(big.Int).SetUint64(rao), new(big.Int).SetUint64(evmWeiPerRao))
	}
	for name, test := range map[string]struct {
		usable, deposit, free uint64
		balance               *big.Int
		wantDelta             uint64
	}{
		"fresh mirror":              {usable: 1_000, deposit: 500, free: 0, balance: big.NewInt(0), wantDelta: 1_500},
		"deposit-only mirror":       {usable: 1_000, deposit: 500, free: 500, balance: big.NewInt(0), wantDelta: 1_000},
		"failed-run incident state": {usable: 1_000, deposit: 500, free: 1_000, balance: wei(500), wantDelta: 500},
		"partially funded mirror":   {usable: 1_000, deposit: 500, free: 750, balance: wei(250), wantDelta: 750},
		"already usable":            {usable: 1_000, deposit: 500, free: 1_500, balance: wei(1_000), wantDelta: 0},
		"sub-rao EVM remainder":     {usable: 1_000, deposit: 500, free: 500, balance: big.NewInt(1), wantDelta: 1_000},
	} {
		t.Run(name, func(t *testing.T) {
			delta, target, err := evmFundingDelta(test.usable, test.deposit, test.free, test.balance)
			if err != nil {
				t.Fatal(err)
			}
			if delta != test.wantDelta || target.Cmp(wei(test.usable)) != 0 {
				t.Fatalf("delta/target = %d/%s, want %d/%s", delta, target, test.wantDelta, wei(test.usable))
			}
		})
	}
}

func TestEVMFundingDeltaRejectsImpossibleAndOverflowingState(t *testing.T) {
	for name, test := range map[string]struct {
		usable, deposit, free uint64
		balance               *big.Int
	}{
		"below deposit":      {usable: 1_000, deposit: 500, free: 499, balance: big.NewInt(0)},
		"EVM exceeds native": {usable: 1_000, deposit: 500, free: 0, balance: big.NewInt(1)},
		"missing balance":    {usable: 1_000, deposit: 500, free: 0},
		"zero usable":        {deposit: 500, free: 0, balance: big.NewInt(0)},
		"zero deposit":       {usable: 1_000, free: 0, balance: big.NewInt(0)},
		"maximum overflow":   {usable: math.MaxUint64, deposit: 1, free: 0, balance: big.NewInt(0)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := evmFundingDelta(test.usable, test.deposit, test.free, test.balance); err == nil {
				t.Fatalf("%s funding state was accepted", name)
			}
		})
	}
}

func TestReserveDeploymentEnvelopeReproducesAndFixesLiveGasIncident(t *testing.T) {
	const (
		estimatedGas = uint64(418_811)
		liveFeeCap   = uint64(40_268_567_174)
	)
	incident := Action{
		ID: "evm.reserve-sink", Kind: "evm-transaction",
		Parameters: map[string]string{evmMaximumGasUnitsParameter: "20901", evmMaximumFeePerGasParameter: "100000000000"},
		Spend:      Spend{EVMGasWei: DecimalUint("2090195084874588")},
	}
	balance := new(big.Int).SetUint64(1_000_000_000_000_000_000)
	if _, _, err := validateEVMTransactionEnvelope(incident, estimatedGas, new(big.Int).SetUint64(liveFeeCap), balance, new(big.Int)); err == nil || !strings.Contains(err.Error(), "gas-unit ceiling") {
		t.Fatalf("live under-allocation incident was not reproduced: %v", err)
	}
	corrected := incident
	corrected.Parameters = map[string]string{evmMaximumGasUnitsParameter: "600000", evmMaximumFeePerGasParameter: "100000000000"}
	corrected.Spend.EVMGasWei = DecimalUint("60000000000000000")
	gas, maximumCost, err := validateEVMTransactionEnvelope(corrected, estimatedGas, new(big.Int).SetUint64(liveFeeCap), balance, new(big.Int))
	if err != nil || gas != 527_573 || maximumCost == nil || maximumCost.String() != "21244608789688702" {
		t.Fatalf("corrected live envelope = gas %d cost %v, %v", gas, maximumCost, err)
	}
}

func TestSettlementVaultDeploymentEnvelopeReproducesRelease10GasIncident(t *testing.T) {
	const estimatedGas = uint64(2_253_684)
	fee := new(big.Int).SetUint64(100_000_000_000)
	balance := new(big.Int).SetUint64(math.MaxUint64)
	incident := Action{
		ID: "evm.settlement-vault", Kind: "evm-transaction",
		Parameters: map[string]string{evmMaximumGasUnitsParameter: "2200000", evmMaximumFeePerGasParameter: fee.String()},
		Spend:      Spend{EVMGasWei: DecimalUint("220000000000000000")},
	}
	if _, _, err := validateEVMTransactionEnvelope(incident, estimatedGas, fee, balance, new(big.Int)); err == nil || !strings.Contains(err.Error(), "padded gas 2729420 exceeds approved gas-unit ceiling 2200000") {
		t.Fatalf("release-1.0 settlement deployment incident was not reproduced: %v", err)
	}
	corrected := incident
	corrected.Parameters = map[string]string{evmMaximumGasUnitsParameter: "3000000", evmMaximumFeePerGasParameter: fee.String()}
	corrected.Spend.EVMGasWei = DecimalUint("300000000000000000")
	gas, maximumCost, err := validateEVMTransactionEnvelope(corrected, estimatedGas, fee, balance, new(big.Int))
	if err != nil || gas != 2_729_420 || maximumCost == nil || maximumCost.String() != "272942000000000000" {
		t.Fatalf("corrected settlement deployment envelope = gas %d cost %v, %v", gas, maximumCost, err)
	}
	below := corrected
	below.Parameters = map[string]string{evmMaximumGasUnitsParameter: "2729419", evmMaximumFeePerGasParameter: fee.String()}
	below.Spend.EVMGasWei = DecimalUint("272941900000000000")
	if _, _, err := validateEVMTransactionEnvelope(below, estimatedGas, fee, balance, new(big.Int)); err == nil || !strings.Contains(err.Error(), "gas-unit ceiling") {
		t.Fatalf("one-unit-short settlement deployment envelope was accepted: %v", err)
	}
	exact := corrected
	exact.Parameters = map[string]string{evmMaximumGasUnitsParameter: "2729420", evmMaximumFeePerGasParameter: fee.String()}
	exact.Spend.EVMGasWei = DecimalUint("272942000000000000")
	if gas, _, err := validateEVMTransactionEnvelope(exact, estimatedGas, fee, balance, new(big.Int)); err != nil || gas != 2_729_420 {
		t.Fatalf("exact settlement deployment boundary = gas %d, %v", gas, err)
	}
}

func TestHistoricalRuntime452OperatorRegistrationGasIncident(t *testing.T) {
	const estimatedGas = uint64(515_196)
	fee := new(big.Int).SetUint64(100_000_000_000)
	balance := new(big.Int).SetUint64(1_000_000_000_000_000_000)
	incident := Action{
		ID: "operator.register.1", Kind: "evm-transaction",
		Parameters: map[string]string{evmMaximumGasUnitsParameter: "600000", evmMaximumFeePerGasParameter: "100000000000"},
		Spend:      Spend{EVMGasWei: DecimalUint("60000000000000000")},
	}
	if _, _, err := validateEVMTransactionEnvelope(incident, estimatedGas, fee, balance, new(big.Int)); err == nil || !strings.Contains(err.Error(), "padded gas 643235 exceeds approved gas-unit ceiling 600000") {
		t.Fatalf("historical runtime 452 operator-registration incident was not reproduced: %v", err)
	}
	corrected := incident
	corrected.Parameters = map[string]string{evmMaximumGasUnitsParameter: "750000", evmMaximumFeePerGasParameter: "100000000000"}
	corrected.Spend.EVMGasWei = DecimalUint("75000000000000000")
	gas, maximumCost, err := validateEVMTransactionEnvelope(corrected, estimatedGas, fee, balance, new(big.Int))
	if err != nil || gas != 643_235 || maximumCost == nil || maximumCost.String() != "64323500000000000" {
		t.Fatalf("corrected runtime 453 operator envelope = gas %d cost %v, %v", gas, maximumCost, err)
	}
}

func TestRuntime453OperatorRegistrationEnvelopeRetainsIncidentBoundary(t *testing.T) {
	const estimatedGas = uint64(515_196)
	fee := new(big.Int).SetUint64(100_000_000_000)
	balance := new(big.Int).SetUint64(1_000_000_000_000_000_000)
	below := Action{
		ID: "operator.register.1", Kind: "evm-transaction",
		Parameters: map[string]string{evmMaximumGasUnitsParameter: "643234", evmMaximumFeePerGasParameter: "100000000000"},
		Spend:      Spend{EVMGasWei: DecimalUint("64323400000000000")},
	}
	if _, _, err := validateEVMTransactionEnvelope(below, estimatedGas, fee, balance, new(big.Int)); err == nil || !strings.Contains(err.Error(), "gas-unit ceiling") {
		t.Fatalf("one-unit-short runtime 453 envelope was accepted: %v", err)
	}
	exact := below
	exact.Parameters = map[string]string{evmMaximumGasUnitsParameter: "643235", evmMaximumFeePerGasParameter: "100000000000"}
	exact.Spend.EVMGasWei = DecimalUint("64323500000000000")
	gas, maximumCost, err := validateEVMTransactionEnvelope(exact, estimatedGas, fee, balance, new(big.Int))
	if err != nil || gas != 643_235 || maximumCost == nil || maximumCost.String() != "64323500000000000" {
		t.Fatalf("exact runtime 453 envelope boundary = gas %d cost %v, %v", gas, maximumCost, err)
	}
}

func TestHistoricalRuntime452CoordinatorDeploymentEstimatesRemainCovered(t *testing.T) {
	limits := setupEVMGasUnitLimits(testResolvedConfig(t))
	incidents := []struct {
		id       string
		estimate uint64
	}{
		{id: "evm.coordinator-implementation", estimate: 5_308_989},
		{id: "evm.coordinator-upgrade-implementation", estimate: 5_308_989},
		{id: "evm.governance-drill-implementation", estimate: 5_502_232},
	}
	fee := new(big.Int).SetUint64(100_000_000_000)
	balance := new(big.Int).SetUint64(math.MaxUint64)
	for _, incident := range incidents {
		padded, err := paddedEVMGas(incident.estimate)
		if err != nil {
			t.Fatal(err)
		}
		action := Action{
			ID: incident.id, Kind: "evm-transaction",
			Parameters: map[string]string{evmMaximumGasUnitsParameter: strconv.FormatUint(limits[incident.id], 10), evmMaximumFeePerGasParameter: fee.String()},
			Spend:      Spend{EVMGasWei: multiplyUint64Decimal(limits[incident.id], fee.Uint64())},
		}
		if gas, _, err := validateEVMTransactionEnvelope(action, incident.estimate, fee, balance, new(big.Int)); err != nil || gas != padded {
			t.Errorf("%s live deployment estimate=%d padded=%d cap=%d: gas=%d err=%v", incident.id, incident.estimate, padded, limits[incident.id], gas, err)
		}
		below := action
		below.Parameters = cloneStrings(action.Parameters)
		below.Parameters[evmMaximumGasUnitsParameter] = strconv.FormatUint(padded-1, 10)
		below.Spend.EVMGasWei = multiplyUint64Decimal(padded-1, fee.Uint64())
		if _, _, err := validateEVMTransactionEnvelope(below, incident.estimate, fee, balance, new(big.Int)); err == nil || !strings.Contains(err.Error(), "gas-unit ceiling") {
			t.Errorf("%s one-unit-short deployment cap was accepted: %v", incident.id, err)
		}
	}
}

func TestEVMTransactionEnvelopeRejectsAdjacentGasFeeBalanceAndEncodingDrift(t *testing.T) {
	baseline := Action{
		ID: "evm.reserve-sink", Kind: "evm-transaction",
		Parameters: map[string]string{evmMaximumGasUnitsParameter: "600000", evmMaximumFeePerGasParameter: "100000000000"},
		Spend:      Spend{EVMGasWei: DecimalUint("60000000000000000")},
	}
	fee := new(big.Int).SetUint64(40_268_567_174)
	balance := new(big.Int).SetUint64(1_000_000_000_000_000_000)
	belowProduct := baseline
	belowProduct.Spend.EVMGasWei = DecimalUint("59999999999999999")
	excessRemainder := baseline
	excessRemainder.Spend.EVMGasWei = DecimalUint("60000100000000000")
	tests := []struct {
		name      string
		action    Action
		estimate  uint64
		fee       *big.Int
		balance   *big.Int
		value     *big.Int
		wantError string
	}{
		{name: "fee above approval", action: baseline, estimate: 418_811, fee: new(big.Int).SetUint64(100_000_000_001), balance: balance, value: new(big.Int), wantError: "fee-per-gas ceiling"},
		{name: "negative fee", action: baseline, estimate: 418_811, fee: big.NewInt(-1), balance: balance, value: new(big.Int), wantError: "fee-per-gas ceiling"},
		{name: "fee above uint64", action: baseline, estimate: 418_811, fee: new(big.Int).Lsh(big.NewInt(1), 65), balance: balance, value: new(big.Int), wantError: "fee-per-gas ceiling"},
		{name: "gas above approval", action: baseline, estimate: 480_001, fee: fee, balance: balance, value: new(big.Int), wantError: "gas-unit ceiling"},
		{name: "estimate overflow", action: baseline, estimate: math.MaxUint64, fee: fee, balance: balance, value: new(big.Int), wantError: "overflow"},
		{name: "missing balance", action: baseline, estimate: 418_811, fee: fee, value: new(big.Int), wantError: "invalid signer balance"},
		{name: "missing value", action: baseline, estimate: 418_811, fee: fee, balance: balance, wantError: "invalid signer balance"},
		{name: "value plus gas underfunded", action: baseline, estimate: 418_811, fee: fee, balance: new(big.Int).SetUint64(21_244_608_789_688_702), value: big.NewInt(1), wantError: "value-plus-maximum-gas"},
		{name: "aggregate below product", action: belowProduct, estimate: 418_811, fee: fee, balance: balance, value: new(big.Int), wantError: "do not match"},
		{name: "aggregate has hidden gas unit", action: excessRemainder, estimate: 418_811, fee: fee, balance: balance, value: new(big.Int), wantError: "do not match"},
	}
	for _, test := range tests {
		if _, _, err := validateEVMTransactionEnvelope(test.action, test.estimate, test.fee, test.balance, test.value); err == nil || !strings.Contains(err.Error(), test.wantError) {
			t.Errorf("%s error = %v, want %q", test.name, err, test.wantError)
		}
	}
}

func TestNeuronRegistrationTransactionUsesMirrorBalanceAndBindsLimit(t *testing.T) {
	parsed, err := abi.JSON(strings.NewReader(neuronSetupABI))
	if err != nil {
		t.Fatal(err)
	}
	hotkey := [32]byte{1, 2, 3}
	data, value, err := buildNeuronRegistrationTransaction(parsed, 521, hotkey, 100_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if value.Sign() != 0 {
		t.Fatalf("neuron precompile call value = %s, want zero", value)
	}
	if funding := registrationFundingWei(100_000_000); funding.Cmp(big.NewInt(100_000_000_000_000_000)) != 0 {
		t.Fatalf("contract registration funding = %s", funding)
	}
	method := parsed.Methods["registerLimit"]
	decoded, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 3 || decoded[0].(uint16) != 521 || decoded[1].([32]byte) != hotkey || decoded[2].(uint64) != 100_000_000 {
		t.Fatalf("registration calldata = %#v", decoded)
	}
}

func TestDeploymentPayloadsAreStableAndMatchPlanRoles(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	a, err := buildDeploymentPayloads(cfg, roles, 17)
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildDeploymentPayloads(cfg, roles, 17)
	if err != nil {
		t.Fatal(err)
	}
	if a.Manifest.ReserveSink != b.Manifest.ReserveSink || a.Manifest.CoordinatorProxy != b.Manifest.CoordinatorProxy || !bytes.Equal(a.Proxy, b.Proxy) {
		t.Fatal("deployment payloads are not deterministic")
	}
	if len(a.Manifest.RuntimeHashes) != 6 || len(a.ExpectedRuntime) != 7 {
		t.Fatalf("deployment runtime evidence incomplete: %+v", a.Manifest.RuntimeHashes)
	}
	deployer := common.HexToAddress(roles.EVM["deployer"].Address)
	if a.Manifest.CoordinatorProxy != crypto.CreateAddress(deployer, 21) || a.Manifest.GovernanceDrillImplementation != crypto.CreateAddress(deployer, 22) || a.Manifest.PrecompileProbe != crypto.CreateAddress(deployer, 25) || a.CoordinatorUpgrade.Implementation != crypto.CreateAddress(deployer, 26) || len(a.RegisterEscrow) == 0 {
		t.Fatalf("deployment nonce sequence does not reserve the vault escrow call: %+v", a.Manifest)
	}
	if a.Manifest.ReserveSink == a.Manifest.SettlementVault || a.Manifest.CoordinatorImplementation == a.Manifest.CoordinatorProxy || a.Manifest.GovernanceDrillImplementation == a.Manifest.CoordinatorImplementation || a.Manifest.PrecompileProbe == a.Manifest.GovernanceDrillImplementation || len(a.GovernanceDrill) == 0 || len(a.PrecompileProbe) == 0 {
		t.Fatal("predicted release contract addresses collide")
	}
	changed, err := buildDeploymentPayloads(cfg, roles, 18)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Manifest.ReserveSink == a.Manifest.ReserveSink {
		t.Fatal("initial nonce did not affect predicted addresses")
	}
}

func replacementPrecompileProbeFixtureAt(t *testing.T, replacementNonce uint64) (*ResolvedConfig, *DeploymentPayloads, ContractDeployment, CoordinatorUpgradeBaseline, string) {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := buildDeploymentPayloads(cfg, roles, 17)
	if err != nil {
		t.Fatal(err)
	}
	active := payloads.CoordinatorUpgrade
	releaseHash, err := contractDeploymentIdentityHash(payloads.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	reserveExecutable, err := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[payloads.Manifest.ReserveSink], artifactByName("ReserveSink"))
	if err != nil {
		t.Fatal(err)
	}
	vaultExecutable, err := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[payloads.Manifest.SettlementVault], artifactByName("SettlementVault"))
	if err != nil {
		t.Fatal(err)
	}
	proxyExecutable, err := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[payloads.Manifest.CoordinatorProxy], artifactByName("ERC1967Proxy"))
	if err != nil {
		t.Fatal(err)
	}
	probeExecutable, err := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[payloads.PrecompileProbeAddress], TestnetPrecompileProbeArtifact)
	if err != nil {
		t.Fatal(err)
	}
	retained := payloads.Manifest
	retained.RuntimeHashes = cloneStrings(payloads.Manifest.RuntimeHashes)
	retained.RuntimeHashes[retained.PrecompileProbe.Hex()] = crypto.Keccak256Hash([]byte{1}).Hex()
	retainedHash, err := contractDeploymentIdentityHash(retained)
	if err != nil {
		t.Fatal(err)
	}
	if err := configureCoordinatorUpgradeNonce(payloads, replacementNonce+1); err != nil {
		t.Fatal(err)
	}
	if err := configurePrecompileProbeNonce(payloads, replacementNonce); err != nil {
		t.Fatal(err)
	}
	baseline := CoordinatorUpgradeBaseline{
		Schema: "urnetwork-coordinator-upgrade-baseline-v4", PriorDeploymentHash: retainedHash,
		ReleaseDeploymentHash: releaseHash, ReboundDeploymentHash: retainedHash,
		ReserveSinkExecutableHash: reserveExecutable, SettlementVaultExecutableHash: vaultExecutable,
		GovernanceDrillVersion: crypto.Keccak256Hash([]byte("urnetwork/coordinator-adversary/v1")).Hex(), GovernanceProxiableUUID: erc1967ImplementationSlot,
		DeployerNonce: payloads.CoordinatorUpgrade.DeployerNonce, ProbeAddressEmpty: true,
		ActiveImplementation: active.Implementation.Hex(), ActiveImplementationHash: active.RuntimeCodeHash,
		PrecompileProbeExecutableHash: probeExecutable, CoordinatorProxyExecutableHash: proxyExecutable,
		ReplacementPrecompileProbe: payloads.PrecompileProbeAddress.Hex(), ReplacementPrecompileProbeNonce: payloads.PrecompileProbeNonce,
		ReplacementPrecompileProbeHash: crypto.Keccak256Hash(payloads.ExpectedRuntime[payloads.PrecompileProbeAddress]).Hex(),
		RetiredPrecompileProbe:         retained.PrecompileProbe.Hex(), RetiredPrecompileProbeHash: retained.RuntimeHashes[retained.PrecompileProbe.Hex()],
		FinalizedBlock: 123, FinalizedBlockHash: "0x" + strings.Repeat("99", 32),
	}
	return cfg, payloads, retained, baseline, releaseHash
}

func replacementPrecompileProbeFixture(t *testing.T) (*ResolvedConfig, *DeploymentPayloads, ContractDeployment, CoordinatorUpgradeBaseline, string) {
	t.Helper()
	return replacementPrecompileProbeFixtureAt(t, 29)
}

func TestHistoricalPrecompileProbeGenerationRequiresExactReplacement(t *testing.T) {
	_, payloads, _, baseline, _ := replacementPrecompileProbeFixture(t)
	current := payloads.ExpectedRuntime[payloads.PrecompileProbeAddress]
	if len(current) != 7_265 {
		t.Fatalf("current SubnetProbe runtime length=%d, want 7265", len(current))
	}
	historical := make([]byte, 7_224)
	if _, err := matchingNormalizedSolidityExecutableHash("precompile probe", historical, current, TestnetPrecompileProbeArtifact); err == nil || !strings.Contains(err.Error(), "SubnetProbe runtime length=7224 want=7265") {
		t.Fatalf("historical normalization mismatch was not reproduced: %v", err)
	}
	const historicalExecutable = "0x987d1bdfc26675cbb0e6c8f45c910c877536f307264f7c047a2b1bbcf637c7bc"
	activeHash, releaseHash, replace, err := comparePrecompileProbeRelease(historical, historicalExecutable, current)
	if err != nil || activeHash != historicalExecutable || releaseHash != baseline.PrecompileProbeExecutableHash || !replace {
		t.Fatalf("historical probe decision=%s/%s/%t error=%v", activeHash, releaseHash, replace, err)
	}
	if _, _, replace, err := comparePrecompileProbeRelease(current, releaseHash, current); err != nil || replace {
		t.Fatalf("current probe generation was not reusable: replace=%t error=%v", replace, err)
	}
}

func TestReplacementPrecompileProbeKeepsCoreIdentityAndContiguousCreates(t *testing.T) {
	cfg, payloads, retained, baseline, _ := replacementPrecompileProbeFixture(t)
	gotHash, err := contractDeploymentIdentityHash(retained)
	if err != nil || gotHash != baseline.ReboundDeploymentHash {
		t.Fatalf("replacement changed core deployment identity: got=%s want=%s error=%v", gotHash, baseline.ReboundDeploymentHash, err)
	}
	if len(retained.RuntimeHashes) != 6 || retained.RuntimeHashes[retained.PrecompileProbe.Hex()] != baseline.RetiredPrecompileProbeHash {
		t.Fatalf("replacement changed immutable runtime census: %+v", retained.RuntimeHashes)
	}
	if payloads.PrecompileProbeNonce != 29 || payloads.CoordinatorUpgrade.DeployerNonce != 30 || payloads.FleetBatcherNonce != 31 || payloads.PrecompileProbeAddress != crypto.CreateAddress(payloads.Deployer, 29) || payloads.CoordinatorUpgrade.Implementation != crypto.CreateAddress(payloads.Deployer, 30) || payloads.FleetBatcherAddress != crypto.CreateAddress(payloads.Deployer, 31) {
		t.Fatalf("replacement CREATE sequence is not contiguous: probe=%d/%s coordinator=%d/%s batcher=%d/%s", payloads.PrecompileProbeNonce, payloads.PrecompileProbeAddress, payloads.CoordinatorUpgrade.DeployerNonce, payloads.CoordinatorUpgrade.Implementation, payloads.FleetBatcherNonce, payloads.FleetBatcherAddress)
	}
	if err := validateCoordinatorUpgradeBaselineRelease(baseline, retained, payloads.Manifest, payloads.CoordinatorUpgrade); err != nil {
		t.Fatal(err)
	}
	if err := validateCoordinatorUpgradePayloadBaseline(baseline, retained, payloads); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		name   string
		change func(*CoordinatorUpgradeBaseline)
	}{
		{"replacement gap", func(value *CoordinatorUpgradeBaseline) { value.ReplacementPrecompileProbeNonce-- }},
		{"false empty checkpoint", func(value *CoordinatorUpgradeBaseline) { value.ProbeAddressEmpty = false }},
		{"foreign replacement", func(value *CoordinatorUpgradeBaseline) {
			value.ReplacementPrecompileProbe = common.HexToAddress("0x1234").Hex()
		}},
		{"wrong replacement runtime", func(value *CoordinatorUpgradeBaseline) {
			value.ReplacementPrecompileProbeHash = "0x" + strings.Repeat("11", 32)
		}},
		{"wrong retired runtime", func(value *CoordinatorUpgradeBaseline) {
			value.RetiredPrecompileProbeHash = "0x" + strings.Repeat("22", 32)
		}},
	} {
		changed := baseline
		mutation.change(&changed)
		err := validateCoordinatorUpgradeBaseline(changed, retained, payloads.CoordinatorUpgrade)
		if err == nil {
			err = validatePrecompileProbeReplacement(changed, payloads.Deployer, retained, payloads.CoordinatorUpgrade)
		}
		if err == nil {
			err = validateCoordinatorUpgradePayloadBaseline(changed, retained, payloads)
		}
		if err == nil {
			t.Fatalf("%s replacement baseline was accepted", mutation.name)
		}
	}
	badBatcher := *payloads
	badBatcher.FleetBatcherNonce++
	if err := validateCoordinatorUpgradePayloadBaseline(baseline, retained, &badBatcher); err == nil {
		t.Fatal("non-contiguous fleet batcher was accepted")
	}
	nextUpgrade := payloads.CoordinatorUpgrade
	nextUpgrade.DeployerNonce = payloads.FleetBatcherNonce + 1
	nextUpgrade.Implementation = crypto.CreateAddress(payloads.Deployer, nextUpgrade.DeployerNonce)
	if err := validateCoordinatorUpgradeBaseline(baseline, retained, nextUpgrade); err == nil {
		t.Fatalf("terminal v4 replacement checkpoint was rebound to a later coordinator CREATE: %v", err)
	}
	consumedNonce := baseline
	consumedNonce.ReplacementPrecompileProbeNonce = payloads.Manifest.InitialNonce + 9
	consumedNonce.ReplacementPrecompileProbe = crypto.CreateAddress(payloads.Deployer, consumedNonce.ReplacementPrecompileProbeNonce).Hex()
	consumedNonce.DeployerNonce = consumedNonce.ReplacementPrecompileProbeNonce + 1
	consumedUpgrade := payloads.CoordinatorUpgrade
	consumedUpgrade.DeployerNonce = consumedNonce.DeployerNonce
	consumedUpgrade.Implementation = crypto.CreateAddress(payloads.Deployer, consumedUpgrade.DeployerNonce)
	if err := validateCoordinatorUpgradeBaseline(consumedNonce, retained, consumedUpgrade); err == nil {
		t.Fatal("v4 replacement reused the already-consumed first coordinator upgrade nonce")
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := buildDeploymentPayloads(cfg, roles, 17)
	if err != nil {
		t.Fatal(err)
	}
	if err := configureCoordinatorUpgradeNonce(ordered, 29); err != nil {
		t.Fatal(err)
	}
	if err := configurePrecompileProbeNonce(ordered, 29); err == nil || !strings.Contains(err.Error(), "active release CREATE") {
		t.Fatalf("replacement probe collided with the current coordinator candidate: %v", err)
	}
	if err := configureCoordinatorUpgradeNonce(ordered, 30); err != nil {
		t.Fatal(err)
	}
	if err := configurePrecompileProbeNonce(ordered, 29); err != nil {
		t.Fatal(err)
	}
	if len(ordered.ExpectedRuntime[ordered.PrecompileProbeAddress]) == 0 || len(ordered.ExpectedRuntime[ordered.CoordinatorUpgrade.Implementation]) == 0 || ordered.PrecompileProbeAddress == ordered.CoordinatorUpgrade.Implementation {
		t.Fatalf("ordered replacement lost a CREATE runtime: probe=%d coordinator=%d", len(ordered.ExpectedRuntime[ordered.PrecompileProbeAddress]), len(ordered.ExpectedRuntime[ordered.CoordinatorUpgrade.Implementation]))
	}
}

func TestPublicReplacementPrecompileProbeBindsAuthenticatedDeployer(t *testing.T) {
	_, payloads, retained, baseline, _ := replacementPrecompileProbeFixture(t)
	identities, err := json.Marshal(finalPublicIdentities{
		Schema: "urnetwork-sim-public-identities-v1", DeploymentID: payloads.Manifest.DeploymentID,
		EVM: map[string]string{"deployer": payloads.Deployer.Hex()},
	})
	if err != nil {
		t.Fatal(err)
	}
	public := &PublicDeploymentManifest{
		DeploymentID: payloads.Manifest.DeploymentID, Contracts: &retained,
		CoordinatorUpgrade: payloads.CoordinatorUpgrade, CoordinatorUpgradeBaseline: &baseline,
		Identities: identities,
	}
	if err := validatePublicPrecompileProbeGeneration(public); err != nil {
		t.Fatal(err)
	}
	wrongAddress := *public
	wrongBaseline := baseline
	wrongBaseline.ReplacementPrecompileProbe = common.HexToAddress("0x1234").Hex()
	wrongAddress.CoordinatorUpgradeBaseline = &wrongBaseline
	if err := validatePublicPrecompileProbeGeneration(&wrongAddress); err == nil || !strings.Contains(err.Error(), "deterministic CREATE") {
		t.Fatalf("forged public replacement address was accepted: %v", err)
	}
	wrongDeployer := *public
	identities, err = json.Marshal(finalPublicIdentities{
		Schema: "urnetwork-sim-public-identities-v1", DeploymentID: payloads.Manifest.DeploymentID,
		EVM: map[string]string{"deployer": common.HexToAddress("0x5678").Hex()},
	})
	if err != nil {
		t.Fatal(err)
	}
	wrongDeployer.Identities = identities
	if err := validatePublicPrecompileProbeGeneration(&wrongDeployer); err == nil || !strings.Contains(err.Error(), "deterministic CREATE") {
		t.Fatalf("forged public deployer was accepted: %v", err)
	}
}

func TestReplacementPrecompileProbePayloadResumesWithImmutableDeployment(t *testing.T) {
	cfg, payloads, legacy, baseline, releaseHash := replacementPrecompileProbeFixture(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	legacyHash, err := contractDeploymentIdentityHash(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if legacyHash == releaseHash || strings.EqualFold(legacy.RuntimeHashes[legacy.PrecompileProbe.Hex()], payloads.Manifest.RuntimeHashes[payloads.Manifest.PrecompileProbe.Hex()]) {
		t.Fatal("historical and release probe generations unexpectedly have the same deployment identity")
	}
	if err := validateCoordinatorUpgradePayloadBaseline(baseline, legacy, payloads); err != nil {
		t.Fatalf("genuinely different retired and release probe runtimes were rejected: %v", err)
	}
	if err := validateCoordinatorUpgradePayloadBaseline(baseline, payloads.Manifest, payloads); err == nil || !strings.Contains(err.Error(), "retired precompile probe") {
		t.Fatalf("release manifest was accepted as the retained historical deployment: %v", err)
	}
	plan := &SetupPlan{Schema: currentSetupPlanSchema, Deployment: legacy, CoordinatorUpgrade: payloads.CoordinatorUpgrade, CoordinatorUpgradeBaseline: baseline}
	stateDir := t.TempDir()
	if err := saveContractDeployment(stateDir, legacy); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		executor := &Executor{cfg: cfg, stateDir: stateDir, plan: plan, roles: roles}
		if err := executor.ensurePayloads(context.Background()); err != nil {
			t.Fatalf("resume %d: %v", attempt, err)
		}
		if executor.payloads.PrecompileProbeAddress != payloads.PrecompileProbeAddress || executor.payloads.PrecompileProbeNonce != payloads.PrecompileProbeNonce || executor.payloads.CoordinatorUpgrade != payloads.CoordinatorUpgrade {
			t.Fatalf("resume %d changed replacement payload: %+v", attempt, executor.payloads)
		}
		stored, err := loadContractDeployment(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		storedHash, err := contractDeploymentIdentityHash(*stored)
		if err != nil || storedHash != legacyHash || len(stored.RuntimeHashes) != 6 || stored.RuntimeHashes[stored.PrecompileProbe.Hex()] != baseline.RetiredPrecompileProbeHash {
			t.Fatalf("resume %d changed persisted core deployment: hash=%s manifest=%+v error=%v", attempt, storedHash, stored, err)
		}
	}
}

func TestEnsurePayloadsAuthenticatesRepeatedBaselineWhenCoreDeploymentIsEqual(t *testing.T) {
	cfg, payloads, _, replacement, manifestHash := replacementPrecompileProbeFixture(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := buildDeploymentPayloads(cfg, roles, payloads.Manifest.InitialNonce)
	if err != nil {
		t.Fatal(err)
	}
	reserveExecutable, _ := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[payloads.Manifest.ReserveSink], artifactByName("ReserveSink"))
	vaultExecutable, _ := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[payloads.Manifest.SettlementVault], artifactByName("SettlementVault"))
	probeExecutable, _ := normalizedSolidityExecutableHash(initial.ExpectedRuntime[initial.Manifest.PrecompileProbe], TestnetPrecompileProbeArtifact)
	proxyExecutable, _ := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[payloads.Manifest.CoordinatorProxy], artifactByName("ERC1967Proxy"))
	baseline := CoordinatorUpgradeBaseline{
		Schema: "urnetwork-coordinator-upgrade-baseline-v3", PriorDeploymentHash: manifestHash,
		ReleaseDeploymentHash: manifestHash, ReboundDeploymentHash: manifestHash,
		ReserveSinkExecutableHash: reserveExecutable, SettlementVaultExecutableHash: vaultExecutable,
		GovernanceDrillVersion: crypto.Keccak256Hash([]byte("urnetwork/coordinator-adversary/v1")).Hex(), GovernanceProxiableUUID: erc1967ImplementationSlot,
		DeployerNonce: payloads.CoordinatorUpgrade.DeployerNonce, ActiveImplementation: initial.CoordinatorUpgrade.Implementation.Hex(), ActiveImplementationHash: initial.CoordinatorUpgrade.RuntimeCodeHash,
		PrecompileProbeExecutableHash: probeExecutable, CoordinatorProxyExecutableHash: proxyExecutable,
		FinalizedBlock: 123, FinalizedBlockHash: "0x" + strings.Repeat("99", 32),
	}
	base := SetupPlan{
		Schema: currentSetupPlanSchema, Deployment: payloads.Manifest,
		CoordinatorUpgrade: payloads.CoordinatorUpgrade, CoordinatorUpgradeBaseline: baseline,
	}
	validDir := t.TempDir()
	if err := saveContractDeployment(validDir, payloads.Manifest); err != nil {
		t.Fatal(err)
	}
	valid := &Executor{cfg: cfg, stateDir: validDir, plan: &base, roles: roles}
	if err := valid.ensurePayloads(context.Background()); err != nil {
		t.Fatalf("valid equal-core repeated baseline was rejected: %v", err)
	}
	for _, mutation := range []struct {
		name   string
		change func(*CoordinatorUpgradeBaseline)
	}{
		{name: "release manifest", change: func(value *CoordinatorUpgradeBaseline) { value.ReleaseDeploymentHash = "0x" + strings.Repeat("81", 32) }},
		{name: "replacement runtime", change: func(value *CoordinatorUpgradeBaseline) {
			value.ReplacementPrecompileProbeHash = "0x" + strings.Repeat("82", 32)
		}},
		{name: "replacement executable", change: func(value *CoordinatorUpgradeBaseline) {
			value.PrecompileProbeExecutableHash = "0x" + strings.Repeat("83", 32)
		}},
	} {
		plan := base
		plan.CoordinatorUpgradeBaseline = replacement
		plan.CoordinatorUpgradeBaseline.PriorDeploymentHash = manifestHash
		plan.CoordinatorUpgradeBaseline.ReboundDeploymentHash = manifestHash
		plan.CoordinatorUpgradeBaseline.RetiredPrecompileProbeHash = payloads.Manifest.RuntimeHashes[payloads.Manifest.PrecompileProbe.Hex()]
		mutation.change(&plan.CoordinatorUpgradeBaseline)
		stateDir := t.TempDir()
		if err := saveContractDeployment(stateDir, payloads.Manifest); err != nil {
			t.Fatal(err)
		}
		executor := &Executor{cfg: cfg, stateDir: stateDir, plan: &plan, roles: roles}
		if err := executor.ensurePayloads(context.Background()); err == nil {
			t.Errorf("%s mutation was accepted", mutation.name)
		}
		stored, err := loadContractDeployment(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		storedHash, err := contractDeploymentIdentityHash(*stored)
		if err != nil || storedHash != manifestHash {
			t.Errorf("%s rejection mutated core deployment: hash=%s want=%s error=%v", mutation.name, storedHash, manifestHash, err)
		}
		if _, err := os.Stat(filepath.Join(stateDir, "public", "deployments")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s rejection created a deployment archive: %v", mutation.name, err)
		}
	}
}

func TestLegacyCoordinatorBaselinesRejectReplacementProbeFields(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := buildDeploymentPayloads(cfg, roles, 17)
	if err != nil {
		t.Fatal(err)
	}
	manifestHash, err := contractDeploymentIdentityHash(initial.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	reserveExecutable, _ := normalizedSolidityExecutableHash(initial.ExpectedRuntime[initial.Manifest.ReserveSink], artifactByName("ReserveSink"))
	vaultExecutable, _ := normalizedSolidityExecutableHash(initial.ExpectedRuntime[initial.Manifest.SettlementVault], artifactByName("SettlementVault"))
	probeExecutable, _ := normalizedSolidityExecutableHash(initial.ExpectedRuntime[initial.Manifest.PrecompileProbe], TestnetPrecompileProbeArtifact)
	proxyExecutable, _ := normalizedSolidityExecutableHash(initial.ExpectedRuntime[initial.Manifest.CoordinatorProxy], artifactByName("ERC1967Proxy"))
	governanceVersion := crypto.Keccak256Hash([]byte("urnetwork/coordinator-adversary/v1")).Hex()
	base := CoordinatorUpgradeBaseline{
		PriorDeploymentHash: manifestHash, ReleaseDeploymentHash: manifestHash, ReboundDeploymentHash: manifestHash,
		ReserveSinkExecutableHash: reserveExecutable, SettlementVaultExecutableHash: vaultExecutable,
		GovernanceDrillVersion: governanceVersion, GovernanceProxiableUUID: erc1967ImplementationSlot,
		FinalizedBlock: 123, FinalizedBlockHash: "0x" + strings.Repeat("99", 32),
	}
	v1 := base
	v1.Schema, v1.DeployerNonce, v1.ProbeAddressEmpty = "urnetwork-coordinator-upgrade-baseline-v1", initial.Manifest.InitialNonce+8, true
	if err := validateCoordinatorUpgradeBaseline(v1, initial.Manifest, initial.CoordinatorUpgrade); err != nil {
		t.Fatalf("valid v1 fixture: %v", err)
	}
	active := initial.CoordinatorUpgrade
	if err := configureCoordinatorUpgradeNonce(initial, active.DeployerNonce+1); err != nil {
		t.Fatal(err)
	}
	v2 := base
	v2.Schema, v2.DeployerNonce = "urnetwork-coordinator-upgrade-baseline-v2", initial.CoordinatorUpgrade.DeployerNonce
	v2.ActiveImplementation, v2.ActiveImplementationHash = active.Implementation.Hex(), active.RuntimeCodeHash
	v2.PrecompileProbeExecutableHash = probeExecutable
	if err := validateCoordinatorUpgradeBaseline(v2, initial.Manifest, initial.CoordinatorUpgrade); err != nil {
		t.Fatalf("valid v2 fixture: %v", err)
	}
	v3 := v2
	v3.Schema, v3.CoordinatorProxyExecutableHash = "urnetwork-coordinator-upgrade-baseline-v3", proxyExecutable
	if err := validateCoordinatorUpgradeBaseline(v3, initial.Manifest, initial.CoordinatorUpgrade); err != nil {
		t.Fatalf("valid v3 fixture: %v", err)
	}
	for _, fixture := range []struct {
		name     string
		baseline CoordinatorUpgradeBaseline
		upgrade  CoordinatorUpgrade
	}{
		{"v1", v1, active},
		{"v2", v2, initial.CoordinatorUpgrade},
		{"v3", v3, initial.CoordinatorUpgrade},
	} {
		for _, mutation := range []struct {
			name   string
			change func(*CoordinatorUpgradeBaseline)
		}{
			{"address", func(value *CoordinatorUpgradeBaseline) {
				value.ReplacementPrecompileProbe = common.HexToAddress("0x1234").Hex()
			}},
			{"nonce", func(value *CoordinatorUpgradeBaseline) { value.ReplacementPrecompileProbeNonce = 29 }},
			{"runtime", func(value *CoordinatorUpgradeBaseline) {
				value.ReplacementPrecompileProbeHash = "0x" + strings.Repeat("11", 32)
			}},
			{"retired address", func(value *CoordinatorUpgradeBaseline) {
				value.RetiredPrecompileProbe = initial.Manifest.PrecompileProbe.Hex()
			}},
			{"retired runtime", func(value *CoordinatorUpgradeBaseline) {
				value.RetiredPrecompileProbeHash = "0x" + strings.Repeat("22", 32)
			}},
		} {
			changed := fixture.baseline
			mutation.change(&changed)
			if err := validateCoordinatorUpgradeBaseline(changed, initial.Manifest, fixture.upgrade); err == nil {
				t.Fatalf("%s baseline accepted v4-only %s", fixture.name, mutation.name)
			}
		}
	}
}

func TestDeploymentGenerationChangesOnlyGenerationBoundVaultIdentityAtOneNonce(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := buildDeploymentPayloadsWithRegistrationGeneration(cfg, roles, 17, 0)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := buildDeploymentPayloadsWithRegistrationGeneration(cfg, roles, 17, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !contractDeploymentAddressesEqual(legacy.Manifest, replacement.Manifest) || legacy.Manifest.RegistrationRoleGeneration != 0 || replacement.Manifest.RegistrationRoleGeneration != 1 {
		t.Fatalf("generation changed deterministic CREATE identities: legacy=%+v replacement=%+v", legacy.Manifest, replacement.Manifest)
	}
	if !bytes.Equal(legacy.Reserve, replacement.Reserve) || bytes.Equal(legacy.Vault, replacement.Vault) || bytes.Equal(legacy.ExpectedRuntime[legacy.Manifest.SettlementVault], replacement.ExpectedRuntime[replacement.Manifest.SettlementVault]) {
		t.Fatal("generation did not isolate its change to the settlement-vault escrow immutable")
	}
	if legacy.Manifest.RuntimeHashes[legacy.Manifest.ReserveSink.Hex()] != replacement.Manifest.RuntimeHashes[replacement.Manifest.ReserveSink.Hex()] || legacy.Manifest.RuntimeHashes[legacy.Manifest.SettlementVault.Hex()] == replacement.Manifest.RuntimeHashes[replacement.Manifest.SettlementVault.Hex()] {
		t.Fatal("generation runtime hashes do not preserve reserve and rotate vault identity")
	}
	legacyHash, _ := contractDeploymentIdentityHash(legacy.Manifest)
	replacementHash, _ := contractDeploymentIdentityHash(replacement.Manifest)
	if legacyHash == replacementHash {
		t.Fatal("deployment identity hash does not bind registration generation")
	}
}

func TestSettlementVaultDeploymentBindsRuntimeDefaultMinTransfer(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	base, err := buildDeploymentPayloads(cfg, roles, 17)
	if err != nil {
		t.Fatal(err)
	}
	changedConfig := *cfg
	changedPublic := *cfg.Public
	changedPublic.Chain.ExpectedDefaultMinTransferRao++
	changedConfig.Public = &changedPublic
	changed, err := buildDeploymentPayloads(&changedConfig, roles, 17)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(base.Vault, changed.Vault) || bytes.Equal(base.ExpectedRuntime[base.Manifest.SettlementVault], changed.ExpectedRuntime[changed.Manifest.SettlementVault]) {
		t.Fatal("runtime DefaultMinTransfer did not alter settlement-vault creation and runtime identity")
	}
	changedPublic.Chain.ExpectedDefaultMinTransferRao = 0
	if _, err := buildDeploymentPayloads(&changedConfig, roles, 17); err == nil || !strings.Contains(err.Error(), "DefaultMinTransfer") {
		t.Fatalf("zero runtime DefaultMinTransfer was accepted: %v", err)
	}
}

func TestCoordinatorUpgradeBaselineAllowsOnlyOriginalImplementationDrift(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	built, err := buildDeploymentPayloads(cfg, roles, 17)
	if err != nil {
		t.Fatal(err)
	}
	baseline := built.Manifest
	baseline.RuntimeHashes = cloneStrings(built.Manifest.RuntimeHashes)
	baseline.RuntimeHashes[baseline.CoordinatorImplementation.Hex()] = "0x" + strings.Repeat("12", 32)
	if !contractDeploymentUpgradeBaselineCompatible(baseline, built.Manifest) {
		t.Fatal("one reviewed coordinator implementation drift was rejected")
	}
	baseline.RuntimeHashes[baseline.SettlementVault.Hex()] = "0x" + strings.Repeat("34", 32)
	if contractDeploymentUpgradeBaselineCompatible(baseline, built.Manifest) {
		t.Fatal("immutable settlement-vault drift was accepted")
	}
}

func TestRepeatedCoordinatorUpgradeBindsNextNonceAndImmutableExecutableBaseline(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := buildDeploymentPayloads(cfg, roles, 17)
	if err != nil {
		t.Fatal(err)
	}
	activeUpgrade := payloads.CoordinatorUpgrade
	if err := configureCoordinatorUpgradeNonce(payloads, activeUpgrade.DeployerNonce+1); err != nil {
		t.Fatal(err)
	}
	if payloads.CoordinatorUpgrade.Schema != "urnetwork-coordinator-upgrade-v2" || payloads.CoordinatorUpgrade.DeployerNonce != 27 || payloads.CoordinatorUpgrade.Implementation != crypto.CreateAddress(payloads.Deployer, 27) {
		t.Fatalf("repeated upgrade identity=%+v", payloads.CoordinatorUpgrade)
	}
	if err := validateCoordinatorUpgradeIdentity(payloads.CoordinatorUpgrade, payloads.Deployer, payloads.Manifest); err != nil {
		t.Fatal(err)
	}
	envelope, ok := deploymentActionEnvelope(payloads, "evm.coordinator-upgrade-implementation", cfg.Config.Budgets.MaximumRegistrationBurnRao)
	if !ok || envelope["expected_nonce"] != "27" || !strings.EqualFold(envelope["expected_created_address"], payloads.CoordinatorUpgrade.Implementation.Hex()) {
		t.Fatalf("repeated upgrade transaction envelope=%+v", envelope)
	}
	if err := configureCoordinatorUpgradeNonce(payloads, payloads.Manifest.InitialNonce+8); err == nil {
		t.Fatal("upgrade nonce below the initial approved boundary was accepted")
	}

	manifestHash, err := contractDeploymentIdentityHash(payloads.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	reserveExecutable, err := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[payloads.Manifest.ReserveSink], artifactByName("ReserveSink"))
	if err != nil {
		t.Fatal(err)
	}
	vaultExecutable, err := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[payloads.Manifest.SettlementVault], artifactByName("SettlementVault"))
	if err != nil {
		t.Fatal(err)
	}
	probeExecutable, err := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[payloads.Manifest.PrecompileProbe], TestnetPrecompileProbeArtifact)
	if err != nil {
		t.Fatal(err)
	}
	proxyArtifact := artifactByName("ERC1967Proxy")
	proxyExecutable, err := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[payloads.Manifest.CoordinatorProxy], proxyArtifact)
	if err != nil {
		t.Fatal(err)
	}
	baseline := CoordinatorUpgradeBaseline{
		Schema: "urnetwork-coordinator-upgrade-baseline-v3", PriorDeploymentHash: manifestHash,
		ReleaseDeploymentHash: manifestHash, ReboundDeploymentHash: manifestHash,
		ReserveSinkExecutableHash: reserveExecutable, SettlementVaultExecutableHash: vaultExecutable,
		GovernanceDrillVersion: crypto.Keccak256Hash([]byte("urnetwork/coordinator-adversary/v1")).Hex(), GovernanceProxiableUUID: erc1967ImplementationSlot,
		DeployerNonce: payloads.CoordinatorUpgrade.DeployerNonce, ProbeAddressEmpty: false,
		ActiveImplementation: activeUpgrade.Implementation.Hex(), ActiveImplementationHash: activeUpgrade.RuntimeCodeHash,
		PrecompileProbeExecutableHash:  probeExecutable,
		CoordinatorProxyExecutableHash: proxyExecutable,
		FinalizedBlock:                 123, FinalizedBlockHash: "0x" + strings.Repeat("99", 32),
	}
	if err := validateCoordinatorUpgradeBaselineRelease(baseline, payloads.Manifest, payloads.Manifest, payloads.CoordinatorUpgrade); err != nil {
		t.Fatal(err)
	}
	if err := validateCoordinatorUpgradePayloadBaseline(baseline, payloads.Manifest, payloads); err != nil {
		t.Fatal(err)
	}
	wrongExecutable := baseline
	wrongExecutable.PrecompileProbeExecutableHash = "0x" + strings.Repeat("77", 32)
	if err := validateCoordinatorUpgradePayloadBaseline(wrongExecutable, payloads.Manifest, payloads); err == nil || !strings.Contains(err.Error(), "precompile probe") {
		t.Fatalf("probe executable drift was accepted: %v", err)
	}
	metadataOnly := *payloads
	metadataOnly.ExpectedRuntime = maps.Clone(payloads.ExpectedRuntime)
	metadataOnly.ExpectedRuntime[payloads.Manifest.CoordinatorProxy] = append([]byte(nil), payloads.ExpectedRuntime[payloads.Manifest.CoordinatorProxy]...)
	executable, err := normalizedSolidityExecutable(metadataOnly.ExpectedRuntime[payloads.Manifest.CoordinatorProxy], proxyArtifact)
	if err != nil {
		t.Fatal(err)
	}
	metadataOnly.ExpectedRuntime[payloads.Manifest.CoordinatorProxy][len(executable)+1] ^= 0x01
	if err := validateCoordinatorUpgradePayloadBaseline(baseline, payloads.Manifest, &metadataOnly); err != nil {
		t.Fatalf("proxy metadata-only drift was rejected: %v", err)
	}
	metadataOnly.ExpectedRuntime[payloads.Manifest.CoordinatorProxy][40] ^= 0x01
	if err := validateCoordinatorUpgradePayloadBaseline(baseline, payloads.Manifest, &metadataOnly); err == nil || !strings.Contains(err.Error(), "proxy executable") {
		t.Fatalf("proxy executable drift was accepted: %v", err)
	}
	wrongActive := baseline
	wrongActive.ActiveImplementation = payloads.CoordinatorUpgrade.Implementation.Hex()
	if err := validateCoordinatorUpgradeBaselineRelease(wrongActive, payloads.Manifest, payloads.Manifest, payloads.CoordinatorUpgrade); err == nil || !strings.Contains(err.Error(), "distinct active") {
		t.Fatalf("self-referential repeated upgrade was accepted: %v", err)
	}
}

func TestExactActiveCoordinatorUpgradeIsReusedOnlyForIdenticalRelease(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := buildDeploymentPayloads(cfg, roles, 17)
	if err != nil {
		t.Fatal(err)
	}
	previousActive := payloads.CoordinatorUpgrade
	if err := configureCoordinatorUpgradeNonce(payloads, previousActive.DeployerNonce+1); err != nil {
		t.Fatal(err)
	}
	active := payloads.CoordinatorUpgrade
	manifestHash, err := contractDeploymentIdentityHash(payloads.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	reserveExecutable, err := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[payloads.Manifest.ReserveSink], artifactByName("ReserveSink"))
	if err != nil {
		t.Fatal(err)
	}
	vaultExecutable, err := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[payloads.Manifest.SettlementVault], artifactByName("SettlementVault"))
	if err != nil {
		t.Fatal(err)
	}
	probeExecutable, err := normalizedSolidityExecutableHash(payloads.ExpectedRuntime[payloads.Manifest.PrecompileProbe], TestnetPrecompileProbeArtifact)
	if err != nil {
		t.Fatal(err)
	}
	priorDeployment := payloads.Manifest
	priorDeployment.RuntimeHashes = cloneStrings(payloads.Manifest.RuntimeHashes)
	prior := &SetupPlan{Deployment: priorDeployment, CoordinatorUpgrade: active, CoordinatorUpgradeBaseline: CoordinatorUpgradeBaseline{
		Schema: "urnetwork-coordinator-upgrade-baseline-v2", PriorDeploymentHash: manifestHash,
		ReleaseDeploymentHash: manifestHash, ReboundDeploymentHash: manifestHash,
		ReserveSinkExecutableHash: reserveExecutable, SettlementVaultExecutableHash: vaultExecutable,
		GovernanceDrillVersion: crypto.Keccak256Hash([]byte("urnetwork/coordinator-adversary/v1")).Hex(), GovernanceProxiableUUID: erc1967ImplementationSlot,
		DeployerNonce: active.DeployerNonce, ProbeAddressEmpty: false,
		ActiveImplementation: previousActive.Implementation.Hex(), ActiveImplementationHash: previousActive.RuntimeCodeHash,
		PrecompileProbeExecutableHash: probeExecutable,
		FinalizedBlock:                123, FinalizedBlockHash: "0x" + strings.Repeat("99", 32),
	}}
	batcherEnvelope, ok := deploymentActionEnvelope(payloads, "fleet.refresh.deploy-batcher", cfg.Config.Budgets.MaximumRegistrationBurnRao)
	if !ok {
		t.Fatal("fleet batcher deployment envelope is unavailable")
	}
	batcherEnvelope["runtime_code_hash"] = crypto.Keccak256Hash(payloads.FleetBatcherRuntime).Hex()
	prior.Actions = []Action{{ID: "fleet.refresh.deploy-batcher", Kind: "evm-transaction", Target: payloads.FleetBatcherAddress.Hex(), Parameters: batcherEnvelope}}
	if err := configureCoordinatorUpgradeNonce(payloads, active.DeployerNonce+1); err != nil {
		t.Fatal(err)
	}
	candidate := payloads.CoordinatorUpgrade
	reused, err := reuseExactActiveCoordinatorUpgrade(prior, payloads)
	if err != nil || !reused || payloads.CoordinatorUpgrade != active {
		t.Fatalf("identical active upgrade reuse=%t upgrade=%+v want=%+v: %v", reused, payloads.CoordinatorUpgrade, active, err)
	}

	if err := configureCoordinatorUpgradeNonce(payloads, candidate.DeployerNonce); err != nil {
		t.Fatal(err)
	}
	driftedBatcher := *prior
	driftedBatcher.Actions = append([]Action(nil), prior.Actions...)
	driftedBatcher.Actions[0].Parameters = cloneStrings(driftedBatcher.Actions[0].Parameters)
	driftedBatcher.Actions[0].Parameters["runtime_code_hash"] = "0x" + strings.Repeat("56", 32)
	if reused, err := reuseExactActiveCoordinatorUpgrade(&driftedBatcher, payloads); err != nil || reused || payloads.CoordinatorUpgrade != candidate {
		t.Fatalf("batcher-drifted release reuse=%t candidate=%+v: %v", reused, payloads.CoordinatorUpgrade, err)
	}
	driftedPrior := *prior
	driftedPrior.CoordinatorUpgrade.RuntimeCodeHash = "0x" + strings.Repeat("12", 32)
	if reused, err := reuseExactActiveCoordinatorUpgrade(&driftedPrior, payloads); err != nil || reused || payloads.CoordinatorUpgrade != candidate {
		t.Fatalf("runtime-drifted release reuse=%t candidate=%+v: %v", reused, payloads.CoordinatorUpgrade, err)
	}

	originalVaultHash := payloads.Manifest.RuntimeHashes[payloads.Manifest.SettlementVault.Hex()]
	payloads.Manifest.RuntimeHashes[payloads.Manifest.SettlementVault.Hex()] = "0x" + strings.Repeat("34", 32)
	if reused, err := reuseExactActiveCoordinatorUpgrade(prior, payloads); err != nil || reused || payloads.CoordinatorUpgrade != candidate {
		t.Fatalf("manifest-drifted release reuse=%t candidate=%+v: %v", reused, payloads.CoordinatorUpgrade, err)
	}
	payloads.Manifest.RuntimeHashes[payloads.Manifest.SettlementVault.Hex()] = originalVaultHash

	withoutBaseline := *prior
	withoutBaseline.CoordinatorUpgradeBaseline = CoordinatorUpgradeBaseline{}
	if reused, err := reuseExactActiveCoordinatorUpgrade(&withoutBaseline, payloads); err != nil || reused || payloads.CoordinatorUpgrade != candidate {
		t.Fatalf("unproved release reuse=%t candidate=%+v: %v", reused, payloads.CoordinatorUpgrade, err)
	}
}

func TestRepeatedCoordinatorActivationRequiresExactPriorSlotAndRuntime(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := buildDeploymentPayloads(cfg, roles, 17)
	if err != nil {
		t.Fatal(err)
	}
	activeUpgrade := payloads.CoordinatorUpgrade
	activeRuntime := append([]byte(nil), payloads.ExpectedRuntime[activeUpgrade.Implementation]...)
	if err := configureCoordinatorUpgradeNonce(payloads, activeUpgrade.DeployerNonce+1); err != nil {
		t.Fatal(err)
	}
	plan := &SetupPlan{CoordinatorUpgradeBaseline: CoordinatorUpgradeBaseline{
		Schema: "urnetwork-coordinator-upgrade-baseline-v3", ActiveImplementation: activeUpgrade.Implementation.Hex(), ActiveImplementationHash: activeUpgrade.RuntimeCodeHash,
	}}
	baselineAddress, baselineHash, err := coordinatorUpgradeActivationBaseline(plan, payloads)
	if err != nil || baselineAddress != activeUpgrade.Implementation || baselineHash != activeUpgrade.RuntimeCodeHash {
		t.Fatalf("repeated activation baseline=%s/%s: %v", baselineAddress, baselineHash, err)
	}
	already, err := validateCoordinatorUpgradeActivationPrestate(activeUpgrade.Implementation, activeRuntime, payloads.CoordinatorUpgrade, baselineAddress, baselineHash)
	if err != nil || already {
		t.Fatalf("exact prior implementation prestate=%t/%v", already, err)
	}
	already, err = validateCoordinatorUpgradeActivationPrestate(payloads.CoordinatorUpgrade.Implementation, nil, payloads.CoordinatorUpgrade, baselineAddress, baselineHash)
	if err != nil || !already {
		t.Fatalf("idempotent activated prestate=%t/%v", already, err)
	}
	if _, err := validateCoordinatorUpgradeActivationPrestate(common.HexToAddress("0x1234"), activeRuntime, payloads.CoordinatorUpgrade, baselineAddress, baselineHash); err == nil || !strings.Contains(err.Error(), "neither baseline") {
		t.Fatalf("foreign proxy slot was accepted: %v", err)
	}
	if _, err := validateCoordinatorUpgradeActivationPrestate(activeUpgrade.Implementation, append(activeRuntime, 0x00), payloads.CoordinatorUpgrade, baselineAddress, baselineHash); err == nil || !strings.Contains(err.Error(), "baseline runtime") {
		t.Fatalf("wrong active implementation runtime was accepted: %v", err)
	}
	plan.CoordinatorUpgradeBaseline.ActiveImplementation = payloads.CoordinatorUpgrade.Implementation.Hex()
	if _, _, err := coordinatorUpgradeActivationBaseline(plan, payloads); err == nil || !strings.Contains(err.Error(), "self-referential") {
		t.Fatalf("self-referential activation baseline was accepted: %v", err)
	}
}

func TestDeploymentRuntimeHashesNormalizeCaseAndRejectAddressAliases(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := buildDeploymentPayloads(cfg, roles, 17)
	if err != nil {
		t.Fatal(err)
	}
	planned := payloads.Manifest
	observed := planned
	alias := strings.ToLower(planned.ReserveSink.Hex())
	if alias == planned.ReserveSink.Hex() {
		alias = "0x" + strings.ToUpper(planned.ReserveSink.Hex()[2:])
	}
	observed.RuntimeHashes = map[string]string{
		alias: planned.RuntimeHashes[planned.ReserveSink.Hex()],
	}
	if !contractDeploymentRuntimeHashesCompatible(observed, planned) {
		t.Fatal("a case-normalized exact observed runtime subset was rejected")
	}
	observed.RuntimeHashes[planned.ReserveSink.Hex()] = planned.RuntimeHashes[planned.ReserveSink.Hex()]
	if contractDeploymentRuntimeHashesCompatible(observed, planned) {
		t.Fatal("duplicate address aliases were accepted as runtime evidence")
	}
	observed.RuntimeHashes = map[string]string{"not-an-address": planned.RuntimeHashes[planned.ReserveSink.Hex()]}
	if contractDeploymentRuntimeHashesCompatible(observed, planned) {
		t.Fatal("invalid runtime-hash address was accepted")
	}
}

func TestEnsurePayloadsArchivesOnlyTheExactlyApprovedSupersededDeployment(t *testing.T) {
	cfg := testResolvedConfig(t)
	facts := testSetupFacts()
	facts.DeployerNonce = 3
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, facts, publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	obsolete, err := buildDeploymentPayloads(cfg, roles, 0)
	if err != nil {
		t.Fatal(err)
	}
	obsolete.Manifest.DeployBlock = 120
	obsolete.Manifest.DeployBlockHash = "0x" + strings.Repeat("12", 32)
	plan.SupersededDeployments = []ContractDeployment{obsolete.Manifest}
	stateDir := t.TempDir()
	if err := saveContractDeployment(stateDir, obsolete.Manifest); err != nil {
		t.Fatal(err)
	}
	executor := &Executor{cfg: cfg, stateDir: stateDir, plan: plan, roles: roles}
	if err := executor.ensurePayloads(context.Background()); err != nil {
		t.Fatal(err)
	}
	active, err := loadContractDeployment(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if !contractDeploymentAddressesEqual(*active, plan.Deployment) {
		t.Fatalf("active deployment was not replaced by the approved identity: %+v", active)
	}
	obsoleteHash, err := canonicalHashHex(obsolete.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(stateDir, "public", "deployments", stringsTrim0x(obsoleteHash)+".json")
	archived, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	var archivedManifest ContractDeployment
	if err := json.Unmarshal(archived, &archivedManifest); err != nil {
		t.Fatal(err)
	}
	archivedHash, err := canonicalHashHex(archivedManifest)
	if err != nil || archivedHash != obsoleteHash {
		t.Fatalf("archived deployment hash = %s, want %s: %v", archivedHash, obsoleteHash, err)
	}
}

func TestEnsurePayloadsRejectsAnUnapprovedExistingDeploymentWithoutMutation(t *testing.T) {
	cfg := testResolvedConfig(t)
	facts := testSetupFacts()
	facts.DeployerNonce = 3
	publicRoles, _ := derivePublicRoles(cfg)
	plan, err := buildPlan(cfg, facts, publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := buildDeploymentPayloads(cfg, roles, 0)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	if err := saveContractDeployment(stateDir, foreign.Manifest); err != nil {
		t.Fatal(err)
	}
	executor := &Executor{cfg: cfg, stateDir: stateDir, plan: plan, roles: roles}
	if err := executor.ensurePayloads(context.Background()); err == nil || !strings.Contains(err.Error(), "neither active nor approved") {
		t.Fatalf("unapproved deployment was accepted: %v", err)
	}
	unchanged, err := loadContractDeployment(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	beforeHash, _ := canonicalHashHex(foreign.Manifest)
	afterHash, _ := canonicalHashHex(*unchanged)
	if beforeHash != afterHash {
		t.Fatal("rejected deployment changed the active manifest")
	}
}

func TestApprovedDeploymentTransactionBindsNonceTargetValueAndPayload(t *testing.T) {
	cfg := testResolvedConfig(t)
	facts := testSetupFacts()
	facts.DeployerNonce = 17
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, facts, publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := buildDeploymentPayloads(cfg, roles, facts.DeployerNonce)
	if err != nil {
		t.Fatal(err)
	}
	action := actionByID(t, plan, "evm.reserve-sink")
	signer := common.HexToAddress(publicRoles.Deployer)
	if err := validateApprovedEVMTransactionFields(action, signer, facts.DeployerNonce, nil, new(big.Int), payloads.Reserve); err != nil {
		t.Fatal(err)
	}
	if action.Parameters["expected_created_address"] != plan.Deployment.ReserveSink.Hex() || action.Parameters[deploymentManifestHashParameter] == "" {
		t.Fatalf("deployment action does not expose its approved CREATE identity: %+v", action.Parameters)
	}
}

func TestApprovedDeploymentTransactionRejectsEveryAdjacentFieldDrift(t *testing.T) {
	cfg := testResolvedConfig(t)
	facts := testSetupFacts()
	facts.DeployerNonce = 17
	publicRoles, _ := derivePublicRoles(cfg)
	plan, err := buildPlan(cfg, facts, publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	roles, _ := BuildRoleSecrets(cfg)
	payloads, err := buildDeploymentPayloads(cfg, roles, facts.DeployerNonce)
	if err != nil {
		t.Fatal(err)
	}
	action := actionByID(t, plan, "evm.reserve-sink")
	signer := common.HexToAddress(publicRoles.Deployer)
	wrongTarget := common.HexToAddress("0x0000000000000000000000000000000000000521")
	tests := []struct {
		name   string
		action Action
		signer common.Address
		nonce  uint64
		to     *common.Address
		value  *big.Int
		data   []byte
	}{
		{name: "signer", action: action, signer: wrongTarget, nonce: facts.DeployerNonce, value: new(big.Int), data: payloads.Reserve},
		{name: "nonce", action: action, signer: signer, nonce: facts.DeployerNonce + 1, value: new(big.Int), data: payloads.Reserve},
		{name: "target", action: action, signer: signer, nonce: facts.DeployerNonce, to: &wrongTarget, value: new(big.Int), data: payloads.Reserve},
		{name: "value", action: action, signer: signer, nonce: facts.DeployerNonce, value: big.NewInt(1), data: payloads.Reserve},
		{name: "payload", action: action, signer: signer, nonce: facts.DeployerNonce, value: new(big.Int), data: append(append([]byte(nil), payloads.Reserve...), 0)},
	}
	incomplete := action
	incomplete.Parameters = maps.Clone(action.Parameters)
	delete(incomplete.Parameters, "expected_nonce")
	tests = append(tests, struct {
		name   string
		action Action
		signer common.Address
		nonce  uint64
		to     *common.Address
		value  *big.Int
		data   []byte
	}{name: "incomplete envelope", action: incomplete, signer: signer, nonce: facts.DeployerNonce, value: new(big.Int), data: payloads.Reserve})
	for _, test := range tests {
		if err := validateApprovedEVMTransactionFields(test.action, test.signer, test.nonce, test.to, test.value, test.data); err == nil {
			t.Errorf("%s drift was accepted", test.name)
		}
	}
}

func TestPrecompileProbeArtifactIsLockedAndTestnetOnly(t *testing.T) {
	if TestnetPrecompileProbeArtifact.Name != "SubnetProbe" || TestnetPrecompileProbeArtifact.CreationBytecode == "" || TestnetPrecompileProbeArtifact.RuntimeBytecodeHash == "" {
		t.Fatal("testnet precompile probe artifact is missing")
	}
	for _, artifact := range ReleaseContractArtifacts {
		if artifact.Name == TestnetPrecompileProbeArtifact.Name {
			t.Fatal("disposable precompile probe leaked into production contract artifacts")
		}
	}
}

func TestGovernanceDrillImplementationIsStorageIsolatedAndTestnetOnly(t *testing.T) {
	if CoordinatorStorageLayoutHash == "" || CoordinatorAdversaryStorageLayoutHash == "" {
		t.Fatal("coordinator storage layout hashes are missing")
	}
	if CoordinatorStorageLayoutHash == CoordinatorAdversaryStorageLayoutHash {
		t.Fatal("minimal testnet adversary unexpectedly embeds the coordinator storage layout")
	}
	if TestnetGovernanceDrillArtifact.Name == "" || TestnetGovernanceDrillArtifact.CreationBytecode == "" {
		t.Fatal("testnet governance drill artifact is missing")
	}
	for _, artifact := range ReleaseContractArtifacts {
		if artifact.Name == TestnetGovernanceDrillArtifact.Name {
			t.Fatal("testnet governance drill implementation leaked into production release artifacts")
		}
	}
}

func TestEVMFinalityUsesCanonicalEVMRPCHashInsteadOfSubstrateHash(t *testing.T) {
	transactionHash := common.HexToHash("0xf27c1c57baf867427594f53bdf8ec33e8ebae8dd4352336a2b49a51657fce758")
	canonical := testEVMHead(7_888_433, 0x52)
	finalized := testEVMHead(7_888_441, 0x53)
	receipt := &types.Receipt{
		Status: types.ReceiptStatusSuccessful, TxHash: transactionHash,
		BlockNumber: new(big.Int).SetUint64(canonical.Number), BlockHash: common.HexToHash(canonical.Hash),
	}
	substrateHash := common.HexToHash("0x49ad70213ee4795f1c8ecdef0f32717ccce04dedac3eb63b523d14b82a823935")
	if receiptIsCanonicalAndFinalized(finalized.Number, receipt, substrateHash.Hex()) {
		t.Fatal("live cross-domain Substrate hash unexpectedly matched the EVM receipt")
	}
	fixture := &evmFinalityFixture{
		finalized: finalized, canonical: map[uint64]ChainHead{canonical.Number: canonical}, receipt: receipt,
	}
	observed, ready, err := observeEVMReceiptFinality(context.Background(), fixture, transactionHash)
	if err != nil || !ready || observed != receipt {
		t.Fatalf("canonical EVM receipt = %p, ready=%t, err=%v", observed, ready, err)
	}
	if len(fixture.headerRequests) != 2 || fixture.headerRequests[0] != -3 || fixture.headerRequests[1] != int64(canonical.Number) {
		t.Fatalf("header requests = %v, want finalized then inclusion block", fixture.headerRequests)
	}
	if len(fixture.receiptRequests) != 1 || fixture.receiptRequests[0] != transactionHash {
		t.Fatalf("receipt requests = %v", fixture.receiptRequests)
	}
}

func TestEthEVMBlockReaderPreservesSyntheticRPCBlockHash(t *testing.T) {
	explicitHash := "0xbfe848f36613b6dcbf9a12075a3ea98cb93ef049a9dbd55f492bdbcf1091a559"
	var selectors []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
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
		if call.Method != "eth_getBlockByNumber" || len(call.Params) != 2 {
			_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "error": map[string]any{"code": -32601, "message": "unexpected method"}})
			return
		}
		var selector string
		if err := json.Unmarshal(call.Params[0], &selector); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		selectors = append(selectors, selector)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"jsonrpc": "2.0", "id": call.ID,
			"result": map[string]any{"number": "0x785e31", "hash": explicitHash},
		})
	}))
	defer server.Close()
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	reader := ethEVMBlockReader{client: client}
	head, err := reader.EVMBlockByNumber(context.Background(), big.NewInt(int64(-3)))
	if err != nil || head.Number != 7_888_433 || head.Hash != explicitHash {
		t.Fatalf("explicit finalized EVM block = %+v, %v", head, err)
	}
	head, err = reader.EVMBlockByNumber(context.Background(), new(big.Int).SetUint64(7_888_433))
	if err != nil || head.Hash != explicitHash {
		t.Fatalf("explicit numbered EVM block = %+v, %v", head, err)
	}
	if len(selectors) != 2 || selectors[0] != "finalized" || selectors[1] != "0x785e31" {
		t.Fatalf("EVM block selectors = %v", selectors)
	}
}

func TestEVMFinalityRejectsAdjacentUnfinalizedOrphanedAndMalformedEvidence(t *testing.T) {
	transactionHash := common.HexToHash("0x1234")
	canonical := testEVMHead(20, 0x20)
	baseline := &types.Receipt{
		Status: types.ReceiptStatusSuccessful, TxHash: transactionHash,
		BlockNumber: big.NewInt(20), BlockHash: common.HexToHash(canonical.Hash),
	}
	orphan := *baseline
	orphan.BlockHash = common.HexToHash(testEVMHead(20, 0x21).Hash)
	missingBlock := *baseline
	missingBlock.BlockNumber = nil
	wrongNumber := testEVMHead(21, 0x20)
	tests := []struct {
		name      string
		fixture   *evmFinalityFixture
		wantReady bool
		wantError bool
	}{
		{name: "unfinalized", fixture: &evmFinalityFixture{finalized: testEVMHead(19, 0x19), canonical: map[uint64]ChainHead{20: canonical}, receipt: baseline}},
		{name: "orphaned", fixture: &evmFinalityFixture{finalized: testEVMHead(21, 0x22), canonical: map[uint64]ChainHead{20: canonical}, receipt: &orphan}},
		{name: "missing receipt", fixture: &evmFinalityFixture{finalized: testEVMHead(21, 0x22), canonical: map[uint64]ChainHead{20: canonical}, receiptError: ethereum.NotFound}},
		{name: "missing inclusion block", fixture: &evmFinalityFixture{finalized: testEVMHead(21, 0x22), canonical: map[uint64]ChainHead{20: canonical}, receipt: &missingBlock}, wantError: true},
		{name: "wrong numbered header", fixture: &evmFinalityFixture{finalized: testEVMHead(21, 0x22), canonical: map[uint64]ChainHead{20: wrongNumber}, receipt: baseline}, wantError: true},
		{name: "canonical", fixture: &evmFinalityFixture{finalized: testEVMHead(21, 0x22), canonical: map[uint64]ChainHead{20: canonical}, receipt: baseline}, wantReady: true},
	}
	for _, test := range tests {
		_, ready, err := observeEVMReceiptFinality(context.Background(), test.fixture, transactionHash)
		if ready != test.wantReady || (err != nil) != test.wantError {
			t.Errorf("%s: ready=%t err=%v, want ready=%t error=%t", test.name, ready, err, test.wantReady, test.wantError)
		}
	}
}

func TestFinalizedEVMHeadBindsFinalizedTagAndEthereumHeaderHash(t *testing.T) {
	head := testEVMHead(42, 0x42)
	fixture := &evmFinalityFixture{finalized: head}
	head, err := finalizedEVMHeadFromReader(context.Background(), fixture)
	if err != nil || head.Number != 42 || head.Hash != fixture.finalized.Hash {
		t.Fatalf("finalized EVM head = %+v, %v", head, err)
	}
	if len(fixture.headerRequests) != 1 || fixture.headerRequests[0] != -3 {
		t.Fatalf("finalized header requests = %v", fixture.headerRequests)
	}
}

func TestBoundFinalizedEVMHeadPreventsNestedCheckpointResolution(t *testing.T) {
	want := testEVMHead(42, 0x42)
	ctx, err := withFinalizedEVMHead(context.Background(), want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := finalizedEVMHead(ctx, nil)
	if err != nil {
		t.Fatalf("bound checkpoint unexpectedly required an RPC client: %v", err)
	}
	if got != want {
		t.Fatalf("bound checkpoint = %+v, want %+v", got, want)
	}
	if _, err := withFinalizedEVMHead(context.Background(), ChainHead{Number: 42}); err == nil {
		t.Fatal("incomplete bound checkpoint was accepted")
	}
	if _, err := finalizedEVMHead(context.Background(), nil); err == nil {
		t.Fatal("an unbound checkpoint accepted a missing RPC client")
	}
}

func TestReceiptRequiresCanonicalHashAndFinalizedHeight(t *testing.T) {
	canonical := common.HexToHash("0x1234")
	tx := common.HexToHash("0x9876")
	receipt := &types.Receipt{Status: types.ReceiptStatusSuccessful, TxHash: tx, BlockNumber: big.NewInt(20), BlockHash: canonical}
	if !receiptIsCanonicalAndFinalized(20, receipt, canonical.Hex()) {
		t.Fatal("canonical finalized receipt was rejected")
	}
	if receiptIsCanonicalAndFinalized(19, receipt, canonical.Hex()) {
		t.Fatal("unfinalized receipt was accepted")
	}
	if receiptIsCanonicalAndFinalized(20, receipt, common.HexToHash("0x5678").Hex()) {
		t.Fatal("orphaned receipt block hash was accepted")
	}
	if receiptIsCanonicalAndFinalized(20, receipt, "") {
		t.Fatal("missing canonical block hash was accepted")
	}
	if !receiptMatchesEvidence(ChainHead{Number: 20, Hash: canonical.Hex()}, receipt, tx.Hex(), 20, canonical.Hex()) {
		t.Fatal("exact receipt evidence was rejected")
	}
	failed := *receipt
	failed.Status = types.ReceiptStatusFailed
	if receiptMatchesEvidence(ChainHead{Number: 20, Hash: canonical.Hex()}, &failed, tx.Hex(), 20, canonical.Hex()) ||
		receiptMatchesEvidence(ChainHead{Number: 19, Hash: canonical.Hex()}, receipt, tx.Hex(), 20, canonical.Hex()) ||
		receiptMatchesEvidence(ChainHead{Number: 20, Hash: canonical.Hex()}, receipt, common.HexToHash("0x1111").Hex(), 20, canonical.Hex()) {
		t.Fatal("noncanonical, failed, or wrong transaction evidence was accepted")
	}
}

func TestRuntimeImmutablePatchingFailsClosed(t *testing.T) {
	artifact := ContractArtifact{Name: "bad", RuntimeBytecode: "00", ImmutableReferences: map[string][]int{"value": {0}}}
	if _, err := runtimeWithImmutables(artifact, nil); err == nil {
		t.Fatal("missing immutable value accepted")
	}
	if _, err := runtimeWithImmutables(artifact, map[string][]byte{"value": make([]byte, 31)}); err == nil {
		t.Fatal("short immutable value accepted")
	}
	if _, err := runtimeWithImmutables(artifact, map[string][]byte{"value": make([]byte, 32)}); err == nil {
		t.Fatal("out-of-range immutable reference accepted")
	}
	if _, err := runtimeWithImmutables(artifact, map[string][]byte{"other": make([]byte, 32)}); err == nil {
		t.Fatal("unknown immutable name accepted")
	}
}

func TestNormalizedSolidityExecutableIgnoresOnlyImmutablesAndMetadata(t *testing.T) {
	artifact := artifactByName("ReserveSink")
	left := hexBytes(artifact.RuntimeBytecode)
	right := append([]byte(nil), left...)
	for _, offsets := range artifact.ImmutableReferences {
		for _, offset := range offsets {
			for index := 0; index < 32; index++ {
				left[offset+index] = 0x11
				right[offset+index] = 0x22
			}
		}
	}
	executable, err := normalizedSolidityExecutable(left, artifact)
	if err != nil {
		t.Fatal(err)
	}
	// The CBOR payload may differ while retaining its canonical length suffix.
	left[len(executable)+1] ^= 0x01
	right[len(executable)+2] ^= 0x02
	leftHash, err := normalizedSolidityExecutableHash(left, artifact)
	if err != nil {
		t.Fatal(err)
	}
	rightHash, err := normalizedSolidityExecutableHash(right, artifact)
	if err != nil || leftHash != rightHash {
		t.Fatalf("constructor/metadata-only drift changed executable hash: %s %s %v", leftHash, rightHash, err)
	}
	right[40] ^= 0x01
	changedHash, err := normalizedSolidityExecutableHash(right, artifact)
	if err != nil || changedHash == leftHash {
		t.Fatalf("executable opcode drift was ignored: %s %s %v", leftHash, changedHash, err)
	}
	right[len(right)-2], right[len(right)-1] = 0, 0
	if _, err := normalizedSolidityExecutableHash(right, artifact); err == nil {
		t.Fatal("runtime without canonical Solidity metadata was accepted")
	}
}

func TestCoordinatorUpgradeBaselineRetainsLegacyCustodyAndRequiresCurrentProbe(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	built, err := buildDeploymentPayloads(cfg, roles, 17)
	if err != nil {
		t.Fatal(err)
	}
	legacy := built.Manifest
	legacy.RuntimeHashes = cloneStrings(built.Manifest.RuntimeHashes)
	for index, address := range []common.Address{legacy.ReserveSink, legacy.SettlementVault, legacy.CoordinatorImplementation, legacy.GovernanceDrillImplementation, legacy.PrecompileProbe} {
		legacy.RuntimeHashes[address.Hex()] = fmt.Sprintf("0x%064x", index+1)
	}
	rebound := legacy
	rebound.RuntimeHashes = cloneStrings(legacy.RuntimeHashes)
	rebound.RuntimeHashes[rebound.PrecompileProbe.Hex()] = built.Manifest.RuntimeHashes[built.Manifest.PrecompileProbe.Hex()]
	priorHash, _ := contractDeploymentIdentityHash(legacy)
	releaseHash, _ := contractDeploymentIdentityHash(built.Manifest)
	reboundHash, _ := contractDeploymentIdentityHash(rebound)
	baseline := CoordinatorUpgradeBaseline{
		Schema: "urnetwork-coordinator-upgrade-baseline-v1", PriorDeploymentHash: priorHash,
		ReleaseDeploymentHash: releaseHash, ReboundDeploymentHash: reboundHash,
		ReserveSinkExecutableHash: "0x" + strings.Repeat("11", 32), SettlementVaultExecutableHash: "0x" + strings.Repeat("22", 32),
		GovernanceDrillVersion: crypto.Keccak256Hash([]byte("urnetwork/coordinator-adversary/v1")).Hex(), GovernanceProxiableUUID: erc1967ImplementationSlot,
		DeployerNonce: 25, ProbeAddressEmpty: true, FinalizedBlock: 100, FinalizedBlockHash: "0x" + strings.Repeat("33", 32),
	}
	if err := validateCoordinatorUpgradeBaselineRelease(baseline, rebound, built.Manifest, built.CoordinatorUpgrade); err != nil {
		t.Fatalf("valid retained baseline was rejected: %v", err)
	}
	wrongProbe := rebound
	wrongProbe.RuntimeHashes = cloneStrings(rebound.RuntimeHashes)
	wrongProbe.RuntimeHashes[wrongProbe.PrecompileProbe.Hex()] = legacy.RuntimeHashes[legacy.PrecompileProbe.Hex()]
	baseline.ReboundDeploymentHash, _ = contractDeploymentIdentityHash(wrongProbe)
	if err := validateCoordinatorUpgradeBaselineRelease(baseline, wrongProbe, built.Manifest, built.CoordinatorUpgrade); err == nil || !strings.Contains(err.Error(), "release-executed runtime") {
		t.Fatalf("legacy probe artifact was accepted for a new deployment: %v", err)
	}
	wrongProxy := rebound
	wrongProxy.RuntimeHashes = cloneStrings(rebound.RuntimeHashes)
	wrongProxy.RuntimeHashes[wrongProxy.CoordinatorProxy.Hex()] = "0x" + strings.Repeat("44", 32)
	baseline.ReboundDeploymentHash, _ = contractDeploymentIdentityHash(wrongProxy)
	if err := validateCoordinatorUpgradeBaselineRelease(baseline, wrongProxy, built.Manifest, built.CoordinatorUpgrade); err == nil || !strings.Contains(err.Error(), "release-executed runtime") {
		t.Fatalf("proxy bytecode drift was accepted: %v", err)
	}
}

func TestHyperparameterNormalizationIsStrict(t *testing.T) {
	if got, err := normalizeYAMLValue(7, "u16"); err != nil || got.(uint64) != 7 {
		t.Fatalf("normalized integer = %v, %v", got, err)
	}
	for _, value := range []any{-1, "7", true} {
		if _, err := normalizeYAMLValue(value, "u16"); err == nil {
			t.Fatalf("invalid numeric value %v (%T) accepted", value, value)
		}
	}
}

func TestOperatorDepositAlphaUsesCoordinatorCustodyNotSignerMirror(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := buildDeploymentPayloads(cfg, roles, 7)
	if err != nil {
		t.Fatal(err)
	}
	coldkey, hotkey, err := alphaTransferDestination(roles, &payloads.Manifest, "operator-deposit", 1)
	if err != nil {
		t.Fatal(err)
	}
	wantHotkey, err := roleBytes32(roles, "operator-1-deposit-hotkey")
	if err != nil {
		t.Fatal(err)
	}
	if coldkey != ss58Mirror(payloads.Manifest.CoordinatorProxy) || hotkey != wantHotkey {
		t.Fatalf("deposit destination = (%x,%x)", coldkey, hotkey)
	}
	signer, err := roles.EVMAddress("operator-1-deposit")
	if err != nil {
		t.Fatal(err)
	}
	if coldkey == ss58Mirror(signer) {
		t.Fatal("deposit authorization signer was incorrectly selected as stake custodian")
	}
}
