package main

import (
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

func TestPrecompileBatteryTupleMatchesGeneratedABI(t *testing.T) {
	parsed, err := (&Executor{}).precompileABI()
	if err != nil {
		t.Fatal(err)
	}
	want := precompileBatteryTuple{BlakeOk: true, MirrorKat: [32]byte{1}, BlakeKatMatch: true, SelfColdkey: [32]byte{2}, EdOk: true, EdVerifyGood: true, EdVerifyBad: true, SrOk: true, SrVerifyGood: true, SrVerifyBad: true, MgOk: true, UidCount: 3, Uid0Hotkey: [32]byte{4}, Uid0Coldkey: [32]byte{5}, NeuronOk: true, SampleExists: true, SampleUid: 2, AbsentRejected: true, StakeViewOk: true, SampleSelfStake: big.NewInt(6), NominatorMinimum: big.NewInt(7)}
	raw, err := parsed.Methods["readBattery"].Outputs.Pack(want)
	if err != nil {
		t.Fatal(err)
	}
	values, err := parsed.Unpack("readBattery", raw)
	if err != nil || len(values) != 1 {
		t.Fatalf("unpack battery: %v", err)
	}
	got, ok := abi.ConvertType(values[0], new(precompileBatteryTuple)).(*precompileBatteryTuple)
	if !ok || got == nil || got.SampleUid != want.SampleUid || got.NominatorMinimum.Cmp(want.NominatorMinimum) != 0 || !got.SrVerifyBad {
		t.Fatalf("generated ABI does not convert to the Go battery tuple: %+v", got)
	}
}

func TestIndependentBatteryCompatibilityChecksEveryRuntimeFamily(t *testing.T) {
	mirrorKAT, probeColdkey := [32]byte{1}, [32]byte{2}
	evidence := &PrecompileConformanceEvidence{SampleUID: 2, ProbeColdkey: hexBytesValue(probeColdkey[:])}
	evidence.Battery.MirrorKAT = hexBytesValue(mirrorKAT[:])
	tuple := &precompileBatteryTuple{
		BlakeOk: true, MirrorKat: [32]byte{1}, BlakeKatMatch: true, SelfColdkey: [32]byte{2},
		EdOk: true, EdVerifyGood: true, EdVerifyBad: true, SrOk: true, SrVerifyGood: true, SrVerifyBad: true,
		MgOk: true, UidCount: 3, Uid0Hotkey: [32]byte{4}, Uid0Coldkey: [32]byte{5}, NeuronOk: true,
		SampleExists: true, SampleUid: 2, AbsentRejected: true, StakeViewOk: true,
		SampleSelfStake: big.NewInt(6), NominatorMinimum: big.NewInt(7),
	}
	if !batteryTupleCompatible(evidence, tuple, 7) {
		t.Fatal("compatible independent battery was rejected")
	}
	tuple.SrVerifyBad = false
	if batteryTupleCompatible(evidence, tuple, 7) {
		t.Fatal("failed independent sr25519 negative control was accepted")
	}
	tuple.SrVerifyBad = true
	if batteryTupleCompatible(evidence, tuple, 8) {
		t.Fatal("changed independent nominator minimum was accepted")
	}
}

func completePrecompileEvidence() *PrecompileConformanceEvidence {
	tx := "0x" + strings.Repeat("11", 32)
	block := "0x" + strings.Repeat("22", 32)
	return &PrecompileConformanceEvidence{
		Commitment: PrecompileCommitmentEvidence{ProbeHash: "0x" + strings.Repeat("33", 32), CanonicalHash: "0x" + strings.Repeat("44", 32), CanonicalGeneration: precompileCanonicalFleetGeneration, EncodedProbeBytes: 34, WriteTransactionHash: tx, WriteFinalizedHead: ChainHead{Number: 1, Hash: block}, WriteCommitmentBlock: 1, RestoreTransactionHash: tx, RestoreFinalizedHead: ChainHead{Number: 2, Hash: block}, RestoreCommitmentBlock: 2, Restored: true},
		Battery: PrecompileBatteryEvidence{
			Passed: true, FinalizedHead: ChainHead{Number: 1, Hash: block}, BlakeOK: true, MirrorKATMatch: true,
			Ed25519OK: true, Ed25519Good: true, Ed25519BadRejected: true, Sr25519OK: true, Sr25519Good: true,
			Sr25519BadRejected: true, MetagraphOK: true, UIDCount: 3, NeuronOK: true, SampleExists: true,
			AbsentRejected: true, StakeViewOK: true,
		},
		Seed:     PrecompileValueStep{TransactionHash: tx, BlockNumber: 1, BlockHash: block, TAORao: 1_000, ValueWei: "1000000000000", BeforeRao: 0, AfterRao: 100, DeltaRao: 100},
		Forward:  PrecompileMoveStep{TransactionHash: tx, BlockNumber: 2, BlockHash: block, AmountRao: 50, FromBeforeRao: 100, FromAfterRao: 50, ToBeforeRao: 0, ToAfterRao: 50},
		Back:     PrecompileMoveStep{TransactionHash: tx, BlockNumber: 3, BlockHash: block, AmountRao: 50, FromBeforeRao: 50, FromAfterRao: 0, ToBeforeRao: 50, ToAfterRao: 100},
		Snapshot: PrecompileSnapshotStep{TransactionHash: tx, BlockNumber: 4, BlockHash: block, BaselineRao: 100, SinceBlock: 4},
		Dividend: PrecompileDividendStep{FinalizedHead: ChainHead{Number: 10, Hash: block}, BaselineRao: 100, CurrentRao: 105, DeltaRao: 5, SinceBlock: 4},
		Transfer: PrecompileTransferStep{TransactionHash: tx, BlockNumber: 11, BlockHash: block, AmountRao: 105, ProbeBeforeRao: 105, ProbeAfterRao: 0, ProviderBeforeRao: 10, ProviderAfterRao: 115},
		Complete: true,
	}
}

func TestPrecompileEvidenceRequiresExactConservation(t *testing.T) {
	evidence := completePrecompileEvidence()
	if !precompileEvidenceComplete(evidence) {
		t.Fatal("exact complete evidence was rejected")
	}
	evidence.Forward.ToAfterRao++
	if precompileEvidenceComplete(evidence) {
		t.Fatal("inexact move evidence was accepted")
	}
	evidence = completePrecompileEvidence()
	evidence.Transfer.ProviderAfterRao--
	if precompileEvidenceComplete(evidence) {
		t.Fatal("underfunded provider transfer was accepted")
	}
	evidence = completePrecompileEvidence()
	evidence.Back.ToBeforeRao--
	evidence.Back.ToAfterRao--
	if precompileEvidenceComplete(evidence) {
		t.Fatal("disconnected round-trip evidence was accepted")
	}
	evidence = completePrecompileEvidence()
	evidence.Transfer.ProbeAfterRao++
	evidence.Transfer.AmountRao--
	if precompileEvidenceComplete(evidence) {
		t.Fatal("unrecovered probe balance was accepted")
	}
	evidence = completePrecompileEvidence()
	evidence.Battery.Sr25519BadRejected = false
	if precompileEvidenceComplete(evidence) {
		t.Fatal("failed precompile subcheck hidden by passed bit was accepted")
	}
	evidence = completePrecompileEvidence()
	evidence.Seed.ValueWei = "1000"
	if precompileEvidenceComplete(evidence) {
		t.Fatal("incorrect TAO-to-EVM conversion evidence was accepted")
	}
	evidence = completePrecompileEvidence()
	evidence.Commitment.CanonicalGeneration = 1
	if precompileEvidenceComplete(evidence) {
		t.Fatal("generation-1 restore was accepted after the generation-2 fleet refresh")
	}
}

func TestPrecompileEvidenceHashAndIdentityFailClosed(t *testing.T) {
	cfg := testResolvedConfig(t)
	probe := common.HexToAddress("0x0000000000000000000000000000000000001234")
	deployment := &ContractDeployment{PrecompileProbe: probe}
	evidence := completePrecompileEvidence()
	evidence.Schema = "urnetwork-precompile-conformance-v1"
	evidence.DeploymentID = cfg.Config.Deployment.DeploymentID
	evidence.ConfigHash = cfg.ConfigHash
	evidence.PolicyHash = cfg.PolicyHash
	evidence.ChainID = testnetChainID
	evidence.GenesisHash = testnetGenesis
	evidence.Netuid = cfg.Netuid
	evidence.ProbeAddress = probe.Hex()
	coldkey := ss58Mirror(probe)
	evidence.ProbeColdkey = hexBytesValue(coldkey[:])
	if err := validatePrecompileEvidenceIdentity(cfg, deployment.PrecompileProbe, evidence); err != nil {
		t.Fatal(err)
	}
	replacement := common.HexToAddress("0x0000000000000000000000000000000000005678")
	if err := validatePrecompileEvidenceIdentity(cfg, replacement, evidence); err == nil {
		t.Fatal("retired-probe evidence was accepted for its replacement generation")
	}
	replacementEvidence := *evidence
	replacementEvidence.ProbeAddress = replacement.Hex()
	replacementColdkey := ss58Mirror(replacement)
	replacementEvidence.ProbeColdkey = hexBytesValue(replacementColdkey[:])
	if err := validatePrecompileEvidenceIdentity(cfg, replacement, &replacementEvidence); err != nil {
		t.Fatalf("replacement-probe evidence was rejected: %v", err)
	}
	wrongGeneration := *evidence
	wrongGeneration.Commitment.CanonicalGeneration = 1
	if err := validatePrecompileEvidenceIdentity(cfg, deployment.PrecompileProbe, &wrongGeneration); err == nil {
		t.Fatal("foreign canonical fleet generation passed evidence identity validation")
	}
	dir := t.TempDir()
	if err := writePrecompileEvidence(dir, evidence); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadPrecompileEvidence(dir)
	if err != nil || !precompileEvidenceComplete(loaded) {
		t.Fatalf("valid evidence did not round-trip: %v", err)
	}
	path := precompileEvidencePath(dir)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `"delta_rao": 5`, `"delta_rao": 6`, 1))
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPrecompileEvidence(dir); err == nil {
		t.Fatal("tampered evidence hash was accepted")
	}
}
