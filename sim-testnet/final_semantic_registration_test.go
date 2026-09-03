package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/ss58"
)

// Builds only the identity evidence needed by structural UID tests.
func finalSemanticIdentityFixture(t *testing.T) FinalSemanticEvidence {
	t.Helper()
	artifact := func(kind, name string) FinalArtifactLocator {
		return FinalArtifactLocator{Kind: kind, URI: "artifacts/" + name + ".json", ContentHash: bytesSHA256([]byte(name)), SizeBytes: uint64(len(name))}
	}
	nativeReceipt := func(name string, block uint64) FinalNativeReceipt {
		return FinalNativeReceipt{ExtrinsicHash: finalTestHex(byte(block)), Block: ChainHead{Number: block, Hash: finalTestHex(byte(block + 1))}, Proof: artifact("native-receipt", name)}
	}
	evmReceipt := func(name string, block uint64) FinalEVMReceipt {
		return FinalEVMReceipt{TransactionHash: finalTestHex(byte(block)), Block: ChainHead{Number: block, Hash: finalTestHex(byte(block + 1))}, Status: "success", LogsHash: finalTestHex(byte(block + 2)), Proof: artifact("evm-receipt", name)}
	}
	nativeTerminal := ChainHead{Number: 100, Hash: finalTestHex(100)}
	settlementVault := common.HexToAddress("0x3333333333333333333333333333333333333333")
	vaultColdkey, err := ss58.Encode(ss58.EvmMirrorPubkey(settlementVault), ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	minerManifest := artifact("miner-process-manifest", "miners")
	bindingManifest := artifact("fleet-binding-manifest", "bindings")
	source := FinalSemanticEvidence{
		Window: ScenarioAcceptanceWindow{FirstEpoch: 10, EpochCount: 3, TerminalBlock: 122}, NativeTerminalHead: nativeTerminal, EVMTerminalHead: ChainHead{Number: 122, Hash: finalTestHex(122)},
		Deployment:        FinalContractDeploymentEvidence{SettlementVault: settlementVault.Hex()},
		ExpectedOperators: 2, ExpectedValidators: 2, ExpectedMiners: 1000, ExpectedCandidates: finalHeadCandidateCount, ExpectedHeadSlots: finalHeadSlotCount,
		Topology: FinalTopologyEvidence{
			MinerSDKInstances: 1000, MinerSwarmProcesses: finalMinerSwarmProcessCount, HeadCandidateFleets: finalHeadCandidateCount,
			HeadSlots: finalHeadSlotCount, ValidatorProcesses: 2, OperatorPools: 2,
			MinerManifestHash: minerManifest.ContentHash, MinerManifest: minerManifest,
			BindingManifestHash: bindingManifest.ContentHash, BindingManifest: bindingManifest,
			ProcessRestarts: []FinalProcessRestartEvidence{{ProcessID: "miner-swarm-1", ExpectedRestarts: 1, ObservedRestarts: 1, FaultIDs: []string{"rolling-restart"}}},
		},
		ValidatorView: FinalValidatorViewTransition{
			FaultEpoch: 10, RestoredEpoch: 11, AffectedValidatorID: 1, ControlValidatorID: 2,
			WithheldFleetID: 1, ReplacementFleetID: 2, Artifact: artifact("validator-view-transition", "validator-view"),
		},
	}
	for i := 0; i < 2; i++ {
		serverKey := bytes.Repeat([]byte{byte(i + 1)}, 32)
		source.Pools = append(source.Pools, FinalPoolUIDEvidence{
			NoID: uint64(i + 1), UID: uint16(11 + 2*i), Hotkey: fmt.Sprintf("5PoolHotkey%d", i+1), Coldkey: vaultColdkey, OperatorColdkey: fmt.Sprintf("5PoolOperatorColdkey%d", i+1), Registered: true,
			Registration: evmReceipt(fmt.Sprintf("pool-registration-%d", i+1), uint64(5+2*i)), Snapshot: nativeTerminal, FinalCarryRao: "0",
			DepositHotkey: fmt.Sprintf("5DepositHotkey%d", i+1), DepositSigner: fmt.Sprintf("0x%040x", 0x60+i), PayoutRootSigner: fmt.Sprintf("0x%040x", 0x70+i),
			ConvictionReceipt: evmReceipt(fmt.Sprintf("conviction-%d", i+1), uint64(91+2*i)), EffectiveEpoch: 10, VersionCount: 1, Active: true,
			ServerKeyHistory: []FinalServerKey{{KeyID: 1, PublicKey: "0x" + hex.EncodeToString(serverKey)}}, OwnershipArtifact: artifact("native-ownership", fmt.Sprintf("pool-ownership-%d", i+1)),
		})
	}
	for i := 0; i < finalHeadCandidateCount; i++ {
		fleetID := uint64(i + 1)
		source.HeadFleets = append(source.HeadFleets, FinalHeadFleetEvidence{
			FleetID: fleetID, UID: uint16(1000 + i), Hotkey: fmt.Sprintf("5HeadHotkey%d", fleetID), Coldkey: fmt.Sprintf("5HeadColdkey%d", fleetID),
			Generation: 1, MemberCount: 4, Registered: true, Registration: nativeReceipt(fmt.Sprintf("head-registration-%d", fleetID), uint64(1+i%40)),
			Snapshot: nativeTerminal, BindingArtifact: artifact("head-fleet-binding", fmt.Sprintf("head-fleet-%d", fleetID)),
		})
	}
	for i := 0; i < 2; i++ {
		fleet := source.HeadFleets[finalHeadSlotCount+i]
		nativeSnapshot := ChainHead{Number: uint64(90 + i), Hash: finalTestHex(byte(90 + i))}
		evmSnapshot := ChainHead{Number: uint64(91 + i), Hash: finalTestHex(byte(91 + i))}
		source.HeadTransitions = append(source.HeadTransitions, FinalHeadTournamentTransition{
			ChallengerFleetID: fleet.FleetID, PromotedUID: fleet.UID, PromotedHotkey: fleet.Hotkey,
			PrunedUID: fleet.UID, PrunedChurn: uint64(i + 1), PrunedHotkey: fmt.Sprintf("5PrunedHotkey%d", fleet.FleetID),
			OperationalRPCMode: rpcModePrivateAuthority, IndependentRPC: true, Registration: fleet.Registration,
			Snapshot: nativeSnapshot, IndependentSnapshot: nativeSnapshot, EVMSnapshot: evmSnapshot, IndependentEVMSnapshot: evmSnapshot,
			Artifact: artifact("head-tournament-transition", fmt.Sprintf("head-transition-%d", fleet.FleetID)),
		})
	}
	return source
}

// Proves that UID zero remains a valid pool identity while duplicate UIDs fail.
func TestFinalSemanticPoolUIDZeroIsValidAndStillUnique(t *testing.T) {
	source := finalSemanticIdentityFixture(t)
	source.Pools[0].UID = 0
	if _, err := verifyFinalPools(&source); err != nil {
		t.Fatalf("pool UID zero was rejected: %v", err)
	}
	source.Pools[1].UID = 0
	if _, err := verifyFinalPools(&source); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate pool UID zero was not rejected: %v", err)
	}
}

// Proves that UID zero remains a valid head identity while duplicate UIDs fail.
func TestFinalSemanticHeadUIDZeroIsValidAndStillUnique(t *testing.T) {
	source := finalSemanticIdentityFixture(t)
	source.HeadFleets[0].UID = 0
	if err := verifyFinalTopology(&source); err != nil {
		t.Fatalf("head UID zero was rejected: %v", err)
	}
	source.HeadFleets[1].UID = 0
	if err := verifyFinalTopology(&source); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate head UID zero was not rejected: %v", err)
	}
}

// Proves that UID zero remains a valid validator identity while reuse fails.
func TestFinalSemanticValidatorUIDZeroIsValidAndStillUnique(t *testing.T) {
	source := finalSemanticIdentityFixture(t)
	validator := FinalValidatorIdentityEvidence{
		ValidatorID: 1, UID: 0, Hotkey: "5ValidatorHotkey1", Coldkey: "5ValidatorColdkey1", Registered: true,
		Registration: FinalNativeReceipt{ExtrinsicHash: finalTestHex(6), Block: ChainHead{Number: 6, Hash: finalTestHex(7)}, Proof: FinalArtifactLocator{Kind: "native-receipt", URI: "artifacts/validator-registration-1.json", ContentHash: bytesSHA256([]byte("registration")), SizeBytes: 12}},
		StakeRao:     "1000000", ValidatorPermit: true, ValidatorTrustU16: 42, PathVPK: "0x" + hex.EncodeToString(bytes.Repeat([]byte{3}, 32)), Snapshot: source.NativeTerminalHead,
		SnapshotArtifact: FinalArtifactLocator{Kind: "native-validator-state", URI: "artifacts/validator-1.json", ContentHash: bytesSHA256([]byte("validator")), SizeBytes: 9},
	}
	seenUIDs := map[uint16]bool{}
	seenVPKs := map[string]bool{}
	if err := verifyFinalValidatorIdentity(&source, &validator, seenUIDs, seenVPKs); err != nil {
		t.Fatalf("validator UID zero was rejected: %v", err)
	}
	duplicate := validator
	duplicate.ValidatorID = 2
	duplicate.PathVPK = "0x" + hex.EncodeToString(bytes.Repeat([]byte{4}, 32))
	if err := verifyFinalValidatorIdentity(&source, &duplicate, seenUIDs, seenVPKs); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("duplicate validator UID zero was not rejected: %v", err)
	}
}

// Proves that pool registration is sealed and replayed as an EVM receipt while
// terminal native ownership is independently queried at the native snapshot.
func TestFinalSemanticPoolRegistrationUsesEVMReceiptAndNativeSnapshot(t *testing.T) {
	requireFinalSemanticReleaseScaleFixture(t)
	source, _ := finalSemanticFixture(t)
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := SealFinalSemanticEvidenceOnChain(context.Background(), draft, &finalTestChainReader{evidence: draft})
	if err != nil {
		t.Fatal(err)
	}
	registration := sealed.Pools[0].Registration
	foundReceipt := false
	foundOwnership := false
	for _, exchange := range sealed.PublicVerification.Exchanges {
		if exchange.Chain == "evm" && exchange.Method == "eth_getTransactionReceipt" && exchange.PinnedHead == registration.Block {
			foundReceipt = true
		}
		if exchange.Chain == "substrate" && exchange.Method == "state_getStorage" && exchange.PinnedHead == sealed.Pools[0].Snapshot {
			foundOwnership = true
		}
	}
	if !foundReceipt || !foundOwnership {
		t.Fatalf("registration receipt/native ownership transcript coverage = %t/%t", foundReceipt, foundOwnership)
	}
	markdown, err := RenderFinalSemanticEvidenceMarkdown(sealed)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{[]byte("bind the EVM registration transaction"), []byte(registration.TransactionHash), []byte(sealed.Pools[0].Snapshot.Hash)} {
		if !bytes.Contains(markdown, want) {
			t.Fatalf("FINAL.md does not expose pool registration/ownership evidence %q", want)
		}
	}
	if err := VerifyFinalSemanticEvidenceOnChain(context.Background(), sealed, &finalTestChainReader{evidence: sealed, corruptPoolOwnership: true}); err == nil || !strings.Contains(err.Error(), "ownership evidence") {
		t.Fatalf("corrupt terminal pool ownership was not rejected: %v", err)
	}
	if err := VerifyFinalSemanticEvidenceOnChain(context.Background(), sealed, &finalTestChainReader{evidence: sealed, corruptRegistration: true}); err == nil || !strings.Contains(err.Error(), "registration receipt") {
		t.Fatalf("corrupt public pool registration receipt was not rejected: %v", err)
	}
}

// Proves that a block alone cannot masquerade as the pool registration proof.
func TestFinalSemanticPoolRegistrationRequiresSuccessfulEVMReceipt(t *testing.T) {
	source := finalSemanticIdentityFixture(t)
	source.Pools[0].Registration.TransactionHash = ""
	if _, err := verifyFinalPools(&source); err == nil || !strings.Contains(err.Error(), "pool registration transaction") {
		t.Fatalf("pool registration without an EVM transaction was not rejected: %v", err)
	}
	source = finalSemanticIdentityFixture(t)
	source.Pools[0].Registration.Status = "failed"
	if _, err := verifyFinalPools(&source); err == nil || !strings.Contains(err.Error(), "pool registration is failed") {
		t.Fatalf("failed pool registration was not rejected: %v", err)
	}
}
