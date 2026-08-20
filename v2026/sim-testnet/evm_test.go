package main

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

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
