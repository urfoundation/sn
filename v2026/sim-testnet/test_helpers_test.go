package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

func testResolvedConfig(t *testing.T) *ResolvedConfig {
	t.Helper()
	policy, err := protocol.LoadPolicy(filepath.Join("..", "deploy", "testnet", "policy-v1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &HarnessConfig{
		SchemaVersion: 1,
		Profile:       releaseProfile,
		Deployment: DeploymentConfig{
			DeploymentID: "unit-test-deployment", Network: "bittensor-testnet", Subnet: "existing",
			NetuidFrom: "vault://main/st.yml#testnet-netuid", Persistent: true,
		},
		LaunchInputs: LaunchInputs{
			Wallet: "vault://main/st.yml#testnet-wallet", WalletPassword: "vault://main/st.yml#testnet-wallet-password", ChainID: "vault://main/st.yml#testnet-chain-id",
			Authority: "vault://main/st.yml#testnet-authority", ObjectStoreHostname: "vault://main/st.yml#testnet-minio-hostname",
			OperatorAPIOrigins: "vault://main/st.yml#testnet-operator-api-origins",
		},
		Topology:     TopologyConfig{Operators: 2, Miners: 8, Validators: 2, HeadFleets: 2, ClientsPerHeadFleet: 3, OperatorAssignment: "balanced"},
		Contracts:    ContractConfig{Install: true, GovernanceProfile: "testnet-single-owner", VerifyRuntimeCodeHash: true},
		Dependencies: DependencyConfig{Mode: "managed_containers", ObjectStore: "server-blob"},
		Artifacts:    ArtifactConfig{Writer: "server-blob", HistoryAPI: "server-api", ContentAddressed: true},
		Processes:    ProcessConfig{RestartPolicy: "on_failure_bounded"},
		Scenarios: ScenarioConfig{
			Launch: "smoke", Release: "release-1.0", ShortEpochs: 20, ProductionEpochs: 2,
			VoluntaryConvictionRao: 1_000_000_000, QualityFaultOperator: 2,
			QualityFaultStartBlocks: 5, QualityFaultDurationBlocks: 20,
			Adversaries: AdversaryConfig{
				Enabled: true, Matrix: "docs/spec/adversarial-matrix-v1.json", Seed: 52120260820,
				SampleIntervalMilliseconds: 5000, RequestTimeoutMilliseconds: 10000,
				MinimumSamplesPerActor: 100, MaximumActorErrorRatePPM: 0,
				MaximumP99LatencyMilliseconds: 15000, MaximumAttackControlP95Ratio: 20_000_000, MaximumOperatorRequestsPerSec: 8,
				MaximumRPCRequestsPerSec: 2,
			},
		},
		Budgets: BudgetConfig{
			MaximumSubnetCreations: 0, MaximumRegistrations: 32, MaximumRegistrationBurnRao: 250_000_000,
			MaximumTotalTAORaoFrom:   "vault://main/st.yml#testnet-spending-limit-tao-rao",
			MaximumTotalAlphaRaoFrom: "vault://main/st.yml#testnet-spending-limit-alpha-rao",
			MaximumEVMGasWeiFrom:     "vault://main/st.yml#testnet-spending-limit-evm-gas-wei",
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	snRepo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	policyHash, err := policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	public := &PublicManifest{SchemaVersion: 1, Profile: releaseProfile}
	public.Chain.ChainID = testnetChainID
	public.Chain.GenesisHash = testnetGenesis
	public.Chain.ExpectedRuntimeSpec = 447
	public.Chain.ExpectedTransactionVersion = 1
	public.Chain.ExpectedBlockSeconds = 12
	release := &ReleaseLock{SchemaVersion: 1, Release: "1.0", Dependencies: map[string]string{
		"postgres": "postgres:18@sha256:" + strings.Repeat("1", 64),
		"redis":    "redis:8-alpine@sha256:" + strings.Repeat("2", 64),
	}}
	hyperparameters := &Hyperparameters{SchemaVersion: 1, Profile: releaseProfile, OwnerControlled: map[string]any{
		"tempo": 360, "max_allowed_uids": 256, "commit_reveal_weights_enabled": true,
	}, ProductionOwnerControlled: map[string]any{"immunity_period": 50400}}
	configHash, err := releaseConfigHash(cfg, public, hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	return &ResolvedConfig{
		Config: cfg, Public: public, Policy: policy, Release: release,
		Hyperparameters: hyperparameters,
		Repos: RepoPaths{
			SN: snRepo, Server: filepath.Join(filepath.Dir(snRepo), "server"), Vault: filepath.Join(filepath.Dir(snRepo), "vault"),
			PlatformConfig: filepath.Join(filepath.Dir(snRepo), "config"),
		},
		Netuid: 7, ChainID: testnetChainID, Authority: "127.0.0.1:9944", ObjectStoreHost: "127.0.0.1",
		OperatorAPIOrigins: []string{"https://no1.example", "https://no2.example"}, WalletSecret: "unit test wallet reference", WalletMaterial: "unit test wallet secret", WalletPasswordSecret: "unit test password reference", WalletPassword: "unit test password secret", WalletPublic: "5UnitTestWallet", WalletHotkeyPublic: "5UnitTestHotkey",
		MaximumTAORao: 10_000_000_000, MaximumAlphaRao: 6_000_000_000, MaximumEVMGasWei: 1_000_000_000_000_000_000,
		PolicyHash: policyHash, ConfigHash: configHash,
	}
}

func testSetupFacts() *SetupFacts {
	return &SetupFacts{
		BurnRao: 100_000_000, AlphaSourceHotkey: "0x" + strings.Repeat("11", 32),
		AlphaAvailableRao: 6_000_000_000, NominatorMinimumRao: 1_000, ProbeTAORao: 1_000,
		WalletFreeTAORao: 20_000_000_000,
		FinalizedBlock:   100, FinalizedBlockHash: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
}
