package main

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
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
		EVMFromBlock: deployment.DeployBlock, EVMHead: ChainHead{Number: 40, Hash: blockHash},
		EVMLogs: []finalCanonicalEVMLog{{
			Address: strings.ToLower(deployment.CoordinatorProxy.Hex()), Topics: []string{"0x" + strings.Repeat("66", 32)}, Data: "0x01",
			BlockNumber: 20, BlockHash: blockHash, TransactionHash: txHash, TransactionIndex: 1, LogIndex: 0,
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
		"wrong deployment": func(value *FinalCollectedChainSnapshot) { value.DeploymentID = "other" },
		"outside range":    func(value *FinalCollectedChainSnapshot) { value.EVMLogs[0].BlockNumber = 9 },
		"foreign emitter":  func(value *FinalCollectedChainSnapshot) { value.EVMLogs[0].Address = "0x" + strings.Repeat("aa", 20) },
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
