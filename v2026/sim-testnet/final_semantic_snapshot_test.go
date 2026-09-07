package main

import "testing"

// Ensures factory-owned evidence cannot alter the artifact-verified snapshot
// through nested slices, pointers, or the temporary-oracle trust projection.
func TestFinalSemanticHistoricalReaderFactorySnapshotCannotMutateVerifiedCopy(t *testing.T) {
	proxy := "0x1000000000000000000000000000000000000001"
	active := "0x2000000000000000000000000000000000000002"
	restored := "0x3000000000000000000000000000000000000003"
	source := &FinalSemanticEvidence{
		Deployment: FinalContractDeploymentEvidence{RuntimeRoots: []FinalReleaseRuntimeRoot{{Name: "coordinator_proxy", Address: proxy, RuntimeCodeHash: finalTestHex(0x11)}}},
		FleetRefreshOracleWindow: FinalFleetRefreshOracleWindowEvidence{Checkpoints: FinalFleetRefreshOracleWindowCheckpoints{
			CoordinatorProxy:         proxy,
			AwaitActiveOperational:   FinalFleetRefreshOracleCheckpointEvidence{Head: ChainHead{Number: 10, Hash: finalTestHex(0x12)}, Oracle: active},
			AwaitActiveIndependent:   FinalFleetRefreshOracleCheckpointEvidence{Head: ChainHead{Number: 10, Hash: finalTestHex(0x12)}, Oracle: active},
			AwaitRestoredOperational: FinalFleetRefreshOracleCheckpointEvidence{Head: ChainHead{Number: 20, Hash: finalTestHex(0x13)}, Oracle: restored},
			AwaitRestoredIndependent: FinalFleetRefreshOracleCheckpointEvidence{Head: ChainHead{Number: 20, Hash: finalTestHex(0x13)}, Oracle: restored},
		}},
		PublicVerification: &FinalPublicChainVerification{Exchanges: []FinalRPCExchange{{Chain: "evm", Method: "eth_call", Params: []byte("[]"), Result: []byte("\"0x\""), PinnedHead: ChainHead{Number: 10, Hash: finalTestHex(0x12)}}}},
	}
	verified, err := finalSemanticEvidenceDetachedCopy(source)
	if err != nil {
		t.Fatal(err)
	}
	factoryInput, err := finalSemanticEvidenceDetachedCopy(verified)
	if err != nil {
		t.Fatal(err)
	}
	factoryInput.Deployment.RuntimeRoots[0].Address = "0x4000000000000000000000000000000000000004"
	factoryInput.FleetRefreshOracleWindow.Checkpoints.CoordinatorProxy = "0x5000000000000000000000000000000000000005"
	factoryInput.PublicVerification.Exchanges[0].Params[0] = '{'
	if verified.Deployment.RuntimeRoots[0].Address != proxy || verified.FleetRefreshOracleWindow.Checkpoints.CoordinatorProxy != proxy || verified.PublicVerification.Exchanges[0].Params[0] != '[' {
		t.Fatal("reader factory mutation reached the artifact-verified snapshot")
	}
	if source.Deployment.RuntimeRoots[0].Address != proxy || source.FleetRefreshOracleWindow.Checkpoints.CoordinatorProxy != proxy || source.PublicVerification.Exchanges[0].Params[0] != '[' {
		t.Fatal("snapshot mutation reached caller-owned evidence")
	}
}
