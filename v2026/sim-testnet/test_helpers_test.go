package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/ss58"
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
			OperatorAPIOrigins: "vault://main/st.yml#testnet-operator-api-origins", PublicEVMMaximumRequestsPerMinute: 40,
		},
		Topology: TopologyConfig{
			Operators: 2, Miners: 1_000, Validators: 2, HeadSlots: 200, HeadFleets: 200,
			ChallengerFleets: 2, ClientsPerHeadFleet: 4, ChurnFloorUIDs: 47, MinerSwarmProcesses: 20,
			OperatorAssignment: "balanced",
		},
		AlphaTransfers: AlphaTransferConfig{MinimumTAOEquivalentMarginBPS: 1_000},
		ValidatorBootstrap: ValidatorBootstrapConfig{
			ReserveTargetShareBPS: 6_500, ReserveMinimumShareBPS: 6_000,
			IndependentTargetAlphaRao: 1_000_000_000_000, MaximumReserveRepairAlphaRao: 3_000_000_000_000, MinimumSourceRemainingAlphaRao: 2_000_000_000_000,
		},
		Contracts:    ContractConfig{Install: true, GovernanceProfile: "testnet-single-owner", VerifyRuntimeCodeHash: true},
		Dependencies: DependencyConfig{Mode: "managed_containers", ObjectStore: "server-blob"},
		Artifacts:    ArtifactConfig{Writer: "server-blob", HistoryAPI: "server-api", ContentAddressed: true, MinioPrefix: "blob/sim-testnet/${deployment_id}"},
		Processes:    ProcessConfig{RestartPolicy: "on_failure_bounded"},
		Scenarios: ScenarioConfig{
			Launch: "smoke", Release: "release-1.0", ShortEpochs: 5, ProductionEpochs: 3,
			VoluntaryConvictionRao: 1_000_000_000, DishonestDepositRao: dishonestDepositRao,
			QualityFaultOperator:    2,
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
			MaximumSubnetCreations: 0, MaximumRegistrations: 260, MaximumRegistrationBurnRao: 1_000_000, MaximumNativeTransactionFeeRao: 3_000_000, MaximumEVMFeePerGasWei: 100_000_000_000,
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
	public.Chain.ExpectedRuntimeSpec = reviewedRuntimeSpecVersion
	public.Chain.ExpectedTransactionVersion = reviewedRuntimeTransactionVersion
	public.Chain.ExpectedStateVersion = reviewedRuntimeStateVersion
	public.Chain.ExpectedBlockSeconds = 12
	public.Chain.ExpectedDefaultMinTransferRao = 100_000
	release := &ReleaseLock{SchemaVersion: 1, Release: "1.0", Dependencies: map[string]string{
		"postgres": "postgres:18@sha256:" + strings.Repeat("1", 64),
		"redis":    "redis:8-alpine@sha256:" + strings.Repeat("2", 64),
	}}
	release.Runtime.CodeHash = reviewedRuntimeCodeHash
	release.Runtime.SpecVersion = reviewedRuntimeSpecVersion
	release.Runtime.TransactionVersion = reviewedRuntimeTransactionVersion
	release.Runtime.StateVersion = reviewedRuntimeStateVersion
	hyperparameters := &Hyperparameters{SchemaVersion: 1, Profile: releaseProfile, OwnerControlled: map[string]any{
		"tempo": 360, "max_allowed_uids": 256, "commit_reveal_weights_enabled": true, "commit_reveal_period": 1, "burn_half_life": 1, "immunity_period": testnetBootstrapImmunityPeriodBlocks,
	}, ProductionOwnerControlled: map[string]any{"burn_half_life": 360, "immunity_period": 360}}
	configHash, err := releaseConfigHash(cfg, public, hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	var testHotkey [32]byte
	for i := range testHotkey {
		testHotkey[i] = 0x42
	}
	var testColdkey [32]byte
	for i := range testColdkey {
		testColdkey[i] = 0x45
	}
	testHotkeyPublic, err := ss58.Encode(testHotkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	testColdkeyPublic, err := ss58.Encode(testColdkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	return &ResolvedConfig{
		Config: cfg, Public: public, Policy: policy, Release: release,
		Hyperparameters: hyperparameters,
		Repos: RepoPaths{
			SN: snRepo, Server: filepath.Join(filepath.Dir(snRepo), "server"), Vault: filepath.Join(filepath.Dir(snRepo), "vault"),
			OperatorProxy: filepath.Join(filepath.Dir(snRepo), "operator-proxy"), PlatformConfig: filepath.Join(filepath.Dir(snRepo), "config"),
		},
		Netuid: 7, ChainID: testnetChainID, Authority: "127.0.0.1:9944", OperationalRPCMode: rpcModePrivateAuthority,
		OperationalSubstrate: "ws://127.0.0.1:9944", OperationalEVM: "http://127.0.0.1:9944", ObjectStoreHost: "127.0.0.1",
		OperatorAPIOrigins: []string{"https://no1.example", "https://no2.example"}, WalletSecret: "unit test wallet reference", WalletMaterial: "unit test wallet secret", WalletPasswordSecret: "unit test password reference", WalletPassword: "unit test password secret", WalletPublic: testColdkeyPublic, WalletHotkeyPublic: testHotkeyPublic,
		MaximumTAORao: 200_000_000_000, MaximumAlphaRao: 20_000_000_000_000, MaximumEVMGasWei: DecimalUint("160000000000000000000"),
		PolicyHash: policyHash, ConfigHash: configHash,
	}
}

func testSetupFacts() *SetupFacts {
	return &SetupFacts{
		BurnRao: 500_000, MinBurnRao: 500_000, MaxBurnRao: 100_000_000_000,
		BurnHalfLifeBlocks: 360, BurnIncreaseMultQ64: "23058430092136939520", AlphaSourceHotkey: "0x" + strings.Repeat("42", 32),
		AlphaAvailableRao: 25_000_000_000_000, AlphaTransferableRao: 25_000_000_000_000,
		WalletNetuidAlphaRao: 25_000_000_000_000, ExistentialDepositRao: 500,
		InitialMinStakeRao: 2_000_000, DefaultMinTransferRao: 100_000, AlphaPriceQ9: 568_309, RegisteredAlphaRao: 26_000_000_000_000, AlphaSourceRegistered: true,
		NominatorMinimumRao: 1_000, ProbeTAORao: 1_000,
		ExistingUIDCount: 2, SubnetOwnerHotkey: "0x" + strings.Repeat("42", 32), UIDZeroHotkey: "0x" + strings.Repeat("42", 32),
		ExistingUIDs: []ExistingUIDFact{
			{UID: 0, Hotkey: "0x" + strings.Repeat("42", 32), Coldkey: "0x" + strings.Repeat("45", 32), RegistrationBlock: 50, SubnetOwner: true, TotalHotkeyAlphaRao: 25_000_000_000_000},
			{UID: 1, Hotkey: "0x" + strings.Repeat("43", 32), Coldkey: "0x" + strings.Repeat("44", 32), RegistrationBlock: 60, TotalHotkeyAlphaRao: 1_000_000_000_000},
		},
		WalletFreeTAORao: 200_000_000_000,
		FinalizedBlock:   100, FinalizedBlockHash: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
}
