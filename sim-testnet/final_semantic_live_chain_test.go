package main

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func finalCollectedChainFixture() (*FinalCollectedChainSnapshot, *ContractDeployment) {
	deployment := &ContractDeployment{
		DeploymentID: "deployment", DeployBlock: 10,
		CoordinatorProxy: common.HexToAddress("0x" + strings.Repeat("11", 20)),
		SettlementVault:  common.HexToAddress("0x" + strings.Repeat("22", 20)),
		ReserveSink:      common.HexToAddress("0x" + strings.Repeat("33", 20)),
	}
	blockHash := "0x" + strings.Repeat("44", 32)
	txHash := "0x" + strings.Repeat("55", 32)
	snapshot := &FinalCollectedChainSnapshot{
		Schema: finalCollectedChainSnapshotSchema, Phase: "release-1.0", RunID: "run", DeploymentID: deployment.DeploymentID,
		FleetBatcher: "0x" + strings.Repeat("34", 20),
		EVMFromBlock: deployment.DeployBlock, CurrentReleaseFromBlock: deployment.DeployBlock, EVMHead: ChainHead{Number: 40, Hash: blockHash},
		CurrentReleaseAddresses: []string{
			strings.ToLower(deployment.CoordinatorProxy.Hex()),
			strings.ToLower(deployment.SettlementVault.Hex()),
			strings.ToLower(deployment.ReserveSink.Hex()),
			"0x" + strings.Repeat("34", 20),
		},
		ReleaseContractAddresses: []string{
			strings.ToLower(deployment.CoordinatorProxy.Hex()),
			strings.ToLower(deployment.SettlementVault.Hex()),
			strings.ToLower(deployment.ReserveSink.Hex()),
			"0x" + strings.Repeat("34", 20),
		},
		EVMLogs: []finalCanonicalEVMLog{{
			Address: strings.ToLower(deployment.CoordinatorProxy.Hex()), Topics: []string{"0x" + strings.Repeat("66", 32)}, Data: "0x01",
			BlockNumber: 20, BlockHash: blockHash, TransactionHash: txHash, TransactionIndex: 1, LogIndex: 0,
		}},
		EVMTransactions: []FinalCollectedEVMTransaction{{
			TransactionHash: txHash, Block: ChainHead{Number: 20, Hash: blockHash},
			From: "0x" + strings.Repeat("aa", 20), To: strings.ToLower(deployment.CoordinatorProxy.Hex()), Input: "0x01020304", ValueWei: "0",
		}},
		NativeHead:  ChainHead{Number: 30, Hash: "0x" + strings.Repeat("77", 32)},
		NativeHeads: []ChainHead{{Number: 28, Hash: "0x" + strings.Repeat("76", 32)}, {Number: 30, Hash: "0x" + strings.Repeat("77", 32)}},
		NativeUIDs: []FinalCollectedNativeUIDState{{
			UID: 0, HotkeyPublicKey: "0x" + strings.Repeat("88", 32), ColdkeyPublicKey: "0x" + strings.Repeat("99", 32),
			RegistrationBlock: 1, StakeRao: "0", ValidatorPermit: false,
		}},
		PublicIdentitiesHash: "sha256:" + strings.Repeat("aa", 32),
		RewardStakeSnapshots: []FinalCollectedRewardStakeSnapshot{
			{NativeHead: ChainHead{Number: 28, Hash: "0x" + strings.Repeat("76", 32)}, EVMHead: ChainHead{Number: 28, Hash: "0x" + strings.Repeat("86", 32)}, Positions: []FinalCollectedRewardStakePosition{{Identity: "fleet-1", HotkeyPublicKey: "0x" + strings.Repeat("88", 32), ColdkeyPublicKey: "0x" + strings.Repeat("99", 32), StakeRao: "10"}}},
			{NativeHead: ChainHead{Number: 30, Hash: "0x" + strings.Repeat("77", 32)}, EVMHead: ChainHead{Number: 30, Hash: "0x" + strings.Repeat("87", 32)}, Positions: []FinalCollectedRewardStakePosition{{Identity: "fleet-1", HotkeyPublicKey: "0x" + strings.Repeat("88", 32), ColdkeyPublicKey: "0x" + strings.Repeat("99", 32), StakeRao: "11"}}},
		},
	}
	return snapshot, deployment
}

func TestFinalCollectedChainSnapshotAcceptsUIDZeroAndCanonicalLogs(t *testing.T) {
	snapshot, deployment := finalCollectedChainFixture()
	if err := verifyFinalCollectedChainSnapshot(snapshot, deployment); err != nil {
		t.Fatal(err)
	}
}

func TestFinalCollectedChainSnapshotFailsClosed(t *testing.T) {
	for name, mutate := range map[string]func(*FinalCollectedChainSnapshot){
		"wrong deployment":      func(value *FinalCollectedChainSnapshot) { value.DeploymentID = "other" },
		"missing fleet batcher": func(value *FinalCollectedChainSnapshot) { value.FleetBatcher = "" },
		"missing active emitter": func(value *FinalCollectedChainSnapshot) {
			value.ReleaseContractAddresses = value.ReleaseContractAddresses[1:]
		},
		"current historical collision": func(value *FinalCollectedChainSnapshot) {
			value.CurrentReleaseAddresses[0] = value.ReleaseContractAddresses[1]
		},
		"late range boundary":    func(value *FinalCollectedChainSnapshot) { value.EVMFromBlock = 11 },
		"wrong current boundary": func(value *FinalCollectedChainSnapshot) { value.CurrentReleaseFromBlock = 9 },
		"outside range":          func(value *FinalCollectedChainSnapshot) { value.EVMLogs[0].BlockNumber = 9 },
		"foreign emitter":        func(value *FinalCollectedChainSnapshot) { value.EVMLogs[0].Address = "0x" + strings.Repeat("aa", 20) },
		"removed ordering": func(value *FinalCollectedChainSnapshot) {
			copy := value.EVMLogs[0]
			copy.LogIndex = 1
			value.EVMLogs = append([]finalCanonicalEVMLog{copy}, value.EVMLogs...)
		},
		"UID gap":        func(value *FinalCollectedChainSnapshot) { value.NativeUIDs[0].UID = 1 },
		"negative stake": func(value *FinalCollectedChainSnapshot) { value.NativeUIDs[0].StakeRao = "-1" },
		"missing terminal native head": func(value *FinalCollectedChainSnapshot) {
			value.NativeHeads[0] = ChainHead{Number: 29, Hash: "0x" + strings.Repeat("77", 32)}
		},
		"missing owner stake": func(value *FinalCollectedChainSnapshot) { value.RewardStakeSnapshots[0].Positions = nil },
		"adjacent owner substitution": func(value *FinalCollectedChainSnapshot) {
			value.RewardStakeSnapshots[1].Positions[0].ColdkeyPublicKey = "0x" + strings.Repeat("98", 32)
		},
		"wrong EVM height": func(value *FinalCollectedChainSnapshot) {
			value.RewardStakeSnapshots[0].EVMHead.Number++
		},
		"malformed EVM hash": func(value *FinalCollectedChainSnapshot) {
			value.RewardStakeSnapshots[0].EVMHead.Hash = "0x01"
		},
		"duplicate owner pair": func(value *FinalCollectedChainSnapshot) {
			for index := range value.RewardStakeSnapshots {
				copy := value.RewardStakeSnapshots[index].Positions[0]
				copy.Identity = "fleet-2"
				value.RewardStakeSnapshots[index].Positions = append(value.RewardStakeSnapshots[index].Positions, copy)
			}
		},
		"malformed owner stake": func(value *FinalCollectedChainSnapshot) { value.RewardStakeSnapshots[0].Positions[0].StakeRao = "01" },
	} {
		t.Run(name, func(t *testing.T) {
			snapshot, deployment := finalCollectedChainFixture()
			mutate(snapshot)
			if err := verifyFinalCollectedChainSnapshot(snapshot, deployment); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

// Verifies the shared log splitter retains both endpoints at the provider's
// inclusive limit and begins a second adjacent request exactly afterward.
func TestFinalEVMLogQueryRangesRespectOfficialInclusiveLimit(t *testing.T) {
	exact, err := finalEVMLogQueryRanges(10, 1009)
	if err != nil || len(exact) != 1 || exact[0] != (finalEVMLogQueryRange{From: 10, To: 1009}) {
		t.Fatalf("exact 1000-block range=%+v err=%v, want one inclusive request", exact, err)
	}
	split, err := finalEVMLogQueryRanges(10, 1010)
	if err != nil || len(split) != 2 || split[0] != (finalEVMLogQueryRange{From: 10, To: 1009}) || split[1] != (finalEVMLogQueryRange{From: 1010, To: 1010}) {
		t.Fatalf("1001-block range=%+v err=%v, want adjacent 1000+1 requests", split, err)
	}
	for index := range split {
		if split[index].To-split[index].From+1 > finalEVMLogQueryMaximumBlocks || index > 0 && split[index].From != split[index-1].To+1 {
			t.Fatalf("range %d is oversized or noncontiguous: %+v", index, split)
		}
	}
}

// Requires every captured proxy baseline to anchor to one canonical initial
// Upgraded event, preventing a plan field from masquerading as observation.
func TestFinalCollectedCoordinatorBaselinesRequireInitializerLog(t *testing.T) {
	proxy := strings.ToLower(common.HexToAddress("0x1000000000000000000000000000000000000001").Hex())
	implementation := strings.ToLower(common.HexToAddress("0x2000000000000000000000000000000000000002").Hex())
	head := ChainHead{Number: 10, Hash: finalTestHex(0x21)}
	log := finalCanonicalEVMLog{
		Address: proxy, Topics: []string{strings.ToLower(crypto.Keccak256Hash([]byte("Upgraded(address)")).Hex()), common.BytesToHash(common.HexToAddress(implementation).Bytes()).Hex()}, Data: "0x",
		BlockNumber: head.Number, BlockHash: head.Hash, TransactionHash: finalTestHex(0x22), TransactionIndex: 0, LogIndex: 0,
	}
	baseline := FinalCollectedCoordinatorBaseline{Proxy: proxy, Head: head, Implementation: implementation, ImplementationRuntimeHash: finalTestHex(0x23), ProxyRuntimeHash: finalTestHex(0x24)}
	if err := verifyFinalCollectedCoordinatorBaselines([]FinalCollectedCoordinatorBaseline{baseline}, []finalCanonicalEVMLog{log}); err != nil {
		t.Fatalf("initializer-bound baseline rejected: %v", err)
	}
	baseline.Implementation = strings.ToLower(common.HexToAddress("0x3000000000000000000000000000000000000003").Hex())
	if err := verifyFinalCollectedCoordinatorBaselines([]FinalCollectedCoordinatorBaseline{baseline}, []finalCanonicalEVMLog{log}); err == nil {
		t.Fatal("accepted a baseline that is not anchored to its initializer log")
	}
}
