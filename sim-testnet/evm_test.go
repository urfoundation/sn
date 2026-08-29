package main

import (
	"bytes"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

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
	if len(a.Manifest.RuntimeHashes) != 6 || len(a.ExpectedRuntime) != 6 {
		t.Fatalf("deployment runtime evidence incomplete: %+v", a.Manifest.RuntimeHashes)
	}
	deployer := common.HexToAddress(roles.EVM["deployer"].Address)
	if a.Manifest.CoordinatorProxy != crypto.CreateAddress(deployer, 21) || a.Manifest.GovernanceDrillImplementation != crypto.CreateAddress(deployer, 22) || a.Manifest.PrecompileProbe != crypto.CreateAddress(deployer, 25) || len(a.RegisterEscrow) == 0 {
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

func TestGovernanceDrillImplementationIsStorageCompatibleAndTestnetOnly(t *testing.T) {
	if CoordinatorStorageLayoutHash == "" || CoordinatorAdversaryStorageLayoutHash == "" {
		t.Fatal("coordinator storage layout hashes are missing")
	}
	if CoordinatorStorageLayoutHash != CoordinatorAdversaryStorageLayoutHash {
		t.Fatalf("testnet adversary storage layout %s differs from coordinator %s", CoordinatorAdversaryStorageLayoutHash, CoordinatorStorageLayoutHash)
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
