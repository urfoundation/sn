package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/signature"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/nacl/secretbox"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/ss58"
)

const (
	releaseProfile         = "release-1.0"
	testnetChainID         = uint64(945)
	testnetGenesis         = "0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105"
	btwalletNACLPrefix     = "$NACL"
	btwalletArgonTime      = uint32(8)
	btwalletArgonMemoryKiB = uint32(512 * 1024)
	btwalletArgonThreads   = uint8(1)
)

var btwalletNACLSalt = [16]byte{0x13, 0x71, 0x83, 0xdf, 0xf1, 0x5a, 0x09, 0xbc, 0x9c, 0x90, 0xb5, 0x51, 0x87, 0x39, 0xe9, 0xb1}

type HarnessConfig struct {
	SchemaVersion int              `yaml:"schema_version" json:"schema_version"`
	Profile       string           `yaml:"profile" json:"profile"`
	Repositories  RepositoryConfig `yaml:"repositories" json:"repositories"`
	Manifests     ManifestConfig   `yaml:"manifests" json:"manifests"`
	Deployment    DeploymentConfig `yaml:"deployment" json:"deployment"`
	LaunchInputs  LaunchInputs     `yaml:"launch_inputs" json:"launch_inputs"`
	Topology      TopologyConfig   `yaml:"topology" json:"topology"`
	Contracts     ContractConfig   `yaml:"contracts" json:"contracts"`
	Dependencies  DependencyConfig `yaml:"dependencies" json:"dependencies"`
	Artifacts     ArtifactConfig   `yaml:"artifacts" json:"artifacts"`
	Processes     ProcessConfig    `yaml:"processes" json:"processes"`
	Scenarios     ScenarioConfig   `yaml:"scenarios" json:"scenarios"`
	Budgets       BudgetConfig     `yaml:"budgets" json:"budgets"`
	Secrets       SecretConfig     `yaml:"secrets" json:"secrets"`
	Analysis      AnalysisConfig   `yaml:"analysis" json:"analysis"`
}

type RepositoryConfig struct {
	Discovery      string `yaml:"discovery" json:"discovery"`
	SN             string `yaml:"sn" json:"sn"`
	Server         string `yaml:"server" json:"server"`
	Vault          string `yaml:"vault" json:"vault"`
	PlatformConfig string `yaml:"platform_config" json:"platform_config"`
}

func (r *RepositoryConfig) UnmarshalYAML(n *yaml.Node) error {
	type raw RepositoryConfig
	var v struct {
		Discovery      string `yaml:"discovery"`
		SN             string `yaml:"sn"`
		Server         string `yaml:"server"`
		Vault          string `yaml:"vault"`
		PlatformConfig string `yaml:"platform_config"`
	}
	if err := n.Decode(&v); err != nil {
		return err
	}
	*r = RepositoryConfig{Discovery: v.Discovery, SN: v.SN, Server: v.Server, Vault: v.Vault, PlatformConfig: v.PlatformConfig}
	return nil
}

type ManifestConfig struct {
	Public          string `yaml:"public" json:"public"`
	Policy          string `yaml:"policy" json:"policy"`
	ReleaseLock     string `yaml:"release_lock" json:"release_lock"`
	Hyperparameters string `yaml:"hyperparameters" json:"hyperparameters"`
}
type DeploymentConfig struct {
	DeploymentID      string `yaml:"deployment_id" json:"deployment_id"`
	Network           string `yaml:"network" json:"network"`
	Subnet            string `yaml:"subnet" json:"subnet"`
	NetuidFrom        string `yaml:"netuid_from" json:"netuid_from"`
	Persistent        bool   `yaml:"persistent" json:"persistent"`
	DetachAfterLaunch bool   `yaml:"detach_after_launch" json:"detach_after_launch"`
}
type LaunchInputs struct {
	Wallet              string `yaml:"wallet" json:"wallet"`
	WalletPassword      string `yaml:"wallet_password" json:"wallet_password"`
	ChainID             string `yaml:"chain_id" json:"chain_id"`
	Authority           string `yaml:"authority" json:"authority"`
	ObjectStoreHostname string `yaml:"object_store_hostname" json:"object_store_hostname"`
	TrustedProxyCIDRs   string `yaml:"trusted_proxy_cidrs" json:"trusted_proxy_cidrs"`
	OperatorAPIOrigins  string `yaml:"operator_api_origins" json:"operator_api_origins"`
}
type TopologyConfig struct {
	Operators           int    `yaml:"operators" json:"operators"`
	Miners              int    `yaml:"miners" json:"miners"`
	Validators          int    `yaml:"validators" json:"validators"`
	HeadFleets          int    `yaml:"head_fleets" json:"head_fleets"`
	ClientsPerHeadFleet int    `yaml:"clients_per_head_fleet" json:"clients_per_head_fleet"`
	OperatorAssignment  string `yaml:"operator_assignment" json:"operator_assignment"`
}
type ContractConfig struct {
	Install               bool   `yaml:"install" json:"install"`
	ArtifactSource        string `yaml:"artifact_source" json:"artifact_source"`
	GovernanceProfile     string `yaml:"governance_profile" json:"governance_profile"`
	VerifyRuntimeCodeHash bool   `yaml:"verify_runtime_code_hash" json:"verify_runtime_code_hash"`
}
type DependencyConfig struct {
	Mode                  string `yaml:"mode" json:"mode"`
	PostgresImageFromLock string `yaml:"postgres_image_from_lock" json:"postgres_image_from_lock"`
	RedisImageFromLock    string `yaml:"redis_image_from_lock" json:"redis_image_from_lock"`
	ObjectStore           string `yaml:"object_store" json:"object_store"`
}
type ArtifactConfig struct {
	Writer           string `yaml:"writer" json:"writer"`
	HistoryAPI       string `yaml:"history_api" json:"history_api"`
	ContentAddressed bool   `yaml:"content_addressed" json:"content_addressed"`
	MinioPrefix      string `yaml:"minio_prefix" json:"minio_prefix"`
}
type ProcessConfig struct {
	BuildFromReleaseLock bool   `yaml:"build_from_release_lock" json:"build_from_release_lock"`
	Logs                 string `yaml:"logs" json:"logs"`
	RestartPolicy        string `yaml:"restart_policy" json:"restart_policy"`
}
type ScenarioConfig struct {
	Launch                     string          `yaml:"launch" json:"launch"`
	Release                    string          `yaml:"release" json:"release"`
	ShortEpochs                int             `yaml:"short_epochs" json:"short_epochs"`
	ProductionEpochs           int             `yaml:"production_epochs" json:"production_epochs"`
	VoluntaryConvictionRao     uint64          `yaml:"voluntary_conviction_rao" json:"voluntary_conviction_rao"`
	QualityFaultOperator       int             `yaml:"quality_fault_operator" json:"quality_fault_operator"`
	QualityFaultStartBlocks    uint64          `yaml:"quality_fault_start_blocks" json:"quality_fault_start_blocks"`
	QualityFaultDurationBlocks uint64          `yaml:"quality_fault_duration_blocks" json:"quality_fault_duration_blocks"`
	Adversaries                AdversaryConfig `yaml:"adversaries" json:"adversaries"`
}

// AdversaryConfig bounds the continuous hostile traffic and local economic
// emulation which run alongside the release and production-soak happy paths.
// The shared testnet campaign is deliberately read-only outside our own
// operator APIs, identities and netuid; chain-wide exploit reproduction is
// reserved for the exact local-runtime gate described by the matrix.
type AdversaryConfig struct {
	Enabled                       bool   `yaml:"enabled" json:"enabled"`
	Matrix                        string `yaml:"matrix" json:"matrix"`
	Seed                          uint64 `yaml:"seed" json:"seed"`
	SampleIntervalMilliseconds    int    `yaml:"sample_interval_milliseconds" json:"sample_interval_milliseconds"`
	RequestTimeoutMilliseconds    int    `yaml:"request_timeout_milliseconds" json:"request_timeout_milliseconds"`
	MinimumSamplesPerActor        int    `yaml:"minimum_samples_per_actor" json:"minimum_samples_per_actor"`
	MaximumActorErrorRatePPM      uint32 `yaml:"maximum_actor_error_rate_ppm" json:"maximum_actor_error_rate_ppm"`
	MaximumP99LatencyMilliseconds int    `yaml:"maximum_p99_latency_milliseconds" json:"maximum_p99_latency_milliseconds"`
	MaximumAttackControlP95Ratio  uint64 `yaml:"maximum_attack_control_p95_ratio_ppm" json:"maximum_attack_control_p95_ratio_ppm"`
	MaximumOperatorRequestsPerSec int    `yaml:"maximum_operator_requests_per_second" json:"maximum_operator_requests_per_second"`
	MaximumRPCRequestsPerSec      int    `yaml:"maximum_rpc_requests_per_second" json:"maximum_rpc_requests_per_second"`
}
type BudgetConfig struct {
	MaximumSubnetCreations     int    `yaml:"maximum_subnet_creations" json:"maximum_subnet_creations"`
	MaximumTotalTAORaoFrom     string `yaml:"maximum_total_tao_rao_from" json:"maximum_total_tao_rao_from"`
	MaximumTotalAlphaRaoFrom   string `yaml:"maximum_total_alpha_rao_from" json:"maximum_total_alpha_rao_from"`
	MaximumEVMGasWeiFrom       string `yaml:"maximum_evm_gas_tao_wei_from" json:"maximum_evm_gas_tao_wei_from"`
	MaximumRegistrations       int    `yaml:"maximum_registrations" json:"maximum_registrations"`
	MaximumRegistrationBurnRao uint64 `yaml:"maximum_registration_burn_rao" json:"maximum_registration_burn_rao"`
}
type SecretConfig struct {
	GeneratedRoleStore string `yaml:"generated_role_store" json:"generated_role_store"`
}
type AnalysisConfig struct {
	PublishPublicManifest  bool `yaml:"publish_public_manifest" json:"publish_public_manifest"`
	WriteJSON              bool `yaml:"write_json" json:"write_json"`
	WriteHTML              bool `yaml:"write_html" json:"write_html"`
	ServeReadOnlyDashboard bool `yaml:"serve_read_only_dashboard" json:"serve_read_only_dashboard"`
}

type PublicManifest struct {
	SchemaVersion int    `yaml:"schema_version"`
	Profile       string `yaml:"profile"`
	Chain         struct {
		Name                              string `yaml:"name"`
		ChainID                           uint64 `yaml:"chain_id"`
		GenesisHash                       string `yaml:"genesis_hash"`
		SS58Format                        uint16 `yaml:"ss58_format"`
		TokenSymbol                       string `yaml:"token_symbol"`
		TokenDecimals                     uint8  `yaml:"token_decimals"`
		ExpectedRuntimeSpec               uint32 `yaml:"expected_runtime_spec"`
		ExpectedTransactionVersion        uint32 `yaml:"expected_transaction_version"`
		ExpectedStateVersion              uint8  `yaml:"expected_state_version"`
		ExpectedBlockSeconds              uint64 `yaml:"expected_block_seconds"`
		SubstratePublicReadEndpoint       string `yaml:"substrate_public_read_endpoint"`
		EVMPublicReadEndpoint             string `yaml:"evm_public_read_endpoint"`
		PrivateAuthorityFrom              string `yaml:"private_authority_from"`
		FinalityMethod                    string `yaml:"finality_method"`
		PublicFallbackAllowsEventIndexing bool   `yaml:"public_fallback_allows_event_indexing"`
	} `yaml:"chain"`
	Subnet struct {
		Mode       string `yaml:"mode"`
		NetuidFrom string `yaml:"netuid_from"`
	} `yaml:"subnet"`
	Governance struct {
		Testnet string `yaml:"testnet"`
		Mainnet string `yaml:"mainnet"`
	} `yaml:"governance"`
	Artifacts struct {
		Store            string `yaml:"store"`
		History          string `yaml:"history"`
		ContentAddressed bool   `yaml:"content_addressed"`
	} `yaml:"artifacts"`
}

type ReleaseLock struct {
	SchemaVersion int    `yaml:"schema_version" json:"schema_version"`
	Release       string `yaml:"release" json:"release"`
	Runtime       struct {
		SourceRepository, SourceTag, SourceCommit string
		SpecVersion, TransactionVersion           uint32
		Image                                     string
	} `yaml:"runtime" json:"runtime"`
	EVMBuild       map[string]any    `yaml:"evm_build" json:"evm_build"`
	Repositories   map[string]any    `yaml:"repositories" json:"repositories"`
	Dependencies   map[string]string `yaml:"dependencies" json:"dependencies"`
	Interfaces     map[string]any    `yaml:"interfaces" json:"interfaces"`
	Infrastructure map[string]any    `yaml:"infrastructure" json:"infrastructure"`
}

func (r *ReleaseLock) UnmarshalYAML(n *yaml.Node) error {
	type lockAlias ReleaseLock
	var aux struct {
		SchemaVersion int    `yaml:"schema_version"`
		Release       string `yaml:"release"`
		Runtime       struct {
			SourceRepository   string `yaml:"source_repository"`
			SourceTag          string `yaml:"source_tag"`
			SourceCommit       string `yaml:"source_commit"`
			SpecVersion        uint32 `yaml:"spec_version"`
			TransactionVersion uint32 `yaml:"transaction_version"`
			Image              string `yaml:"image"`
		} `yaml:"runtime"`
		EVMBuild       map[string]any    `yaml:"evm_build"`
		Repositories   map[string]any    `yaml:"repositories"`
		Dependencies   map[string]string `yaml:"dependencies"`
		Interfaces     map[string]any    `yaml:"interfaces"`
		Infrastructure map[string]any    `yaml:"infrastructure"`
	}
	if err := n.Decode(&aux); err != nil {
		return err
	}
	r.SchemaVersion = aux.SchemaVersion
	r.Release = aux.Release
	r.Runtime.SourceRepository = aux.Runtime.SourceRepository
	r.Runtime.SourceTag = aux.Runtime.SourceTag
	r.Runtime.SourceCommit = aux.Runtime.SourceCommit
	r.Runtime.SpecVersion = aux.Runtime.SpecVersion
	r.Runtime.TransactionVersion = aux.Runtime.TransactionVersion
	r.Runtime.Image = aux.Runtime.Image
	r.EVMBuild = aux.EVMBuild
	r.Repositories = aux.Repositories
	r.Dependencies = aux.Dependencies
	r.Interfaces = aux.Interfaces
	r.Infrastructure = aux.Infrastructure
	return nil
}

type Hyperparameters struct {
	SchemaVersion              int                          `yaml:"schema_version" json:"schema_version"`
	Profile                    string                       `yaml:"profile" json:"profile"`
	NetuidFrom                 string                       `yaml:"netuid_from" json:"netuid_from"`
	OwnerControlled            map[string]any               `yaml:"owner_controlled" json:"owner_controlled"`
	ProductionOwnerControlled  map[string]any               `yaml:"production_owner_controlled" json:"production_owner_controlled"`
	ObservedCompatibilityGates map[string]CompatibilityGate `yaml:"observed_compatibility_gates" json:"observed_compatibility_gates"`
	Receipts                   map[string]any               `yaml:"receipts" json:"receipts"`
}

type CompatibilityGate struct {
	Storage  string   `yaml:"storage" json:"storage"`
	Scope    string   `yaml:"scope" json:"scope"`
	Kind     string   `yaml:"kind" json:"kind"`
	Rule     string   `yaml:"rule" json:"rule"`
	Expected []uint64 `yaml:"expected,omitempty" json:"expected,omitempty"`
	Decision string   `yaml:"decision" json:"decision"`
}

type RepoPaths struct{ SN, Server, Vault, PlatformConfig string }
type ResolvedConfig struct {
	ConfigPath           string
	Config               *HarnessConfig
	Public               *PublicManifest
	Policy               *protocol.Policy
	Release              *ReleaseLock
	Hyperparameters      *Hyperparameters
	Repos                RepoPaths
	VaultPath            string
	Vault                map[string]any
	Netuid               uint16
	ChainID              uint64
	Authority            string
	ObjectStoreHost      string
	TrustedProxyCIDRs    string
	OperatorAPIOrigins   []string
	WalletSecret         string
	WalletMaterial       string
	WalletPasswordSecret string
	WalletPassword       string
	WalletPublic         string
	WalletHotkeyPublic   string
	MaximumTAORao        uint64
	MaximumAlphaRao      uint64
	MaximumEVMGasWei     uint64
	PolicyHash           string
	ConfigHash           string
}

type LoadOptions struct {
	ConfigPath, SNRepo, ServerRepo, VaultRepo, PlatformConfigRepo string
	RequireSecrets                                                bool
}

func strictYAML(path string, out any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return fmt.Errorf("%s: multiple YAML documents", path)
	}
	return nil
}

func LoadResolved(opts LoadOptions) (*ResolvedConfig, error) {
	if opts.ConfigPath == "" {
		opts.ConfigPath = "sim-testnet/testnet.yml"
	}
	abs, err := filepath.Abs(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	var cfg HarnessConfig
	if err := strictYAML(abs, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	repos, err := discoverRepos(abs, opts)
	if err != nil {
		return nil, err
	}
	base := filepath.Dir(abs)
	resolve := func(p string) string {
		if filepath.IsAbs(p) {
			return filepath.Clean(p)
		}
		return filepath.Clean(filepath.Join(base, p))
	}
	pub := new(PublicManifest)
	if err := strictYAML(resolve(cfg.Manifests.Public), pub); err != nil {
		return nil, err
	}
	policy, err := protocol.LoadPolicy(resolve(cfg.Manifests.Policy))
	if err != nil {
		return nil, err
	}
	lock := new(ReleaseLock)
	if err := strictYAML(resolve(cfg.Manifests.ReleaseLock), lock); err != nil {
		return nil, err
	}
	hyper := new(Hyperparameters)
	if err := strictYAML(resolve(cfg.Manifests.Hyperparameters), hyper); err != nil {
		return nil, err
	}
	vaultPath := filepath.Join(repos.Vault, "main", "st.yml")
	vault := map[string]any{}
	raw, err := os.ReadFile(vaultPath)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(raw, &vault); err != nil {
		return nil, fmt.Errorf("%s: %w", vaultPath, err)
	}
	r := &ResolvedConfig{ConfigPath: abs, Config: &cfg, Public: pub, Policy: policy, Release: lock, Hyperparameters: hyper, Repos: repos, VaultPath: vaultPath, Vault: vault}
	if r.PolicyHash, err = policy.HashHex(); err != nil {
		return nil, err
	}
	if r.ConfigHash, err = releaseConfigHash(&cfg, pub, hyper); err != nil {
		return nil, err
	}
	if err := r.resolveVaultInputs(opts.RequireSecrets); err != nil {
		return nil, err
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return r, nil
}

// Bind every non-secret launch manifest which can change setup behavior. The
// policy and source lock retain their own hashes for independent audit fields.
func releaseConfigHash(config *HarnessConfig, public *PublicManifest, hyperparameters *Hyperparameters) (string, error) {
	if config == nil || public == nil || hyperparameters == nil {
		return "", errors.New("release configuration manifests are incomplete")
	}
	return canonicalHashHex(struct {
		Config          *HarnessConfig   `json:"config"`
		Public          *PublicManifest  `json:"public"`
		Hyperparameters *Hyperparameters `json:"hyperparameters"`
	}{Config: config, Public: public, Hyperparameters: hyperparameters})
}

func (c *HarnessConfig) Validate() error {
	if c.SchemaVersion != 1 || c.Profile != releaseProfile {
		return fmt.Errorf("config must be schema 1 profile %s", releaseProfile)
	}
	if c.Deployment.Network != "bittensor-testnet" || c.Deployment.Subnet != "existing" || c.Budgets.MaximumSubnetCreations != 0 {
		return errors.New("release harness only accepts the existing Bittensor testnet subnet and forbids subnet creation")
	}
	headMembers := c.Topology.HeadFleets * c.Topology.ClientsPerHeadFleet
	if c.Topology.Operators != 2 || c.Topology.Miners != 8 || c.Topology.Validators != 2 || c.Topology.HeadFleets != 2 || c.Topology.ClientsPerHeadFleet != 3 || headMembers != 6 {
		return errors.New("release-1.0 testnet topology is fixed at two operators, eight miners, two validators, and two independently keyed three-client head fleets")
	}
	if c.Topology.OperatorAssignment != "balanced" || c.Dependencies.Mode != "managed_containers" || c.Processes.RestartPolicy != "on_failure_bounded" {
		return errors.New("release topology requires balanced assignment, managed containers, and bounded restart supervision")
	}
	if c.Contracts.GovernanceProfile != "testnet-single-owner" || !c.Contracts.Install || !c.Contracts.VerifyRuntimeCodeHash {
		return errors.New("invalid contract release settings")
	}
	if c.Scenarios.Launch != "smoke" || c.Scenarios.Release != "release-1.0" || c.Scenarios.ShortEpochs < 20 || c.Scenarios.ProductionEpochs < 2 {
		return errors.New("release scenarios require smoke launch, release-1.0, at least 20 accelerated epochs, and two production epochs")
	}
	if c.Scenarios.VoluntaryConvictionRao == 0 || c.Scenarios.QualityFaultOperator < 1 || c.Scenarios.QualityFaultOperator > c.Topology.Operators || c.Scenarios.QualityFaultStartBlocks == 0 || c.Scenarios.QualityFaultDurationBlocks == 0 {
		return errors.New("release scenario requires voluntary conviction and a bounded quality-cohort fault")
	}
	adversaries := c.Scenarios.Adversaries
	if !adversaries.Enabled || adversaries.Matrix != "docs/spec/adversarial-matrix-v1.json" || adversaries.Seed == 0 {
		return errors.New("release scenario requires the pinned continuous adversarial campaign")
	}
	if adversaries.SampleIntervalMilliseconds < 250 || adversaries.SampleIntervalMilliseconds > 60_000 || adversaries.RequestTimeoutMilliseconds < 250 || adversaries.RequestTimeoutMilliseconds > 60_000 {
		return errors.New("adversarial sample interval and request timeout must be in [250,60000] milliseconds")
	}
	if adversaries.MinimumSamplesPerActor < 100 || adversaries.MaximumActorErrorRatePPM != 0 || adversaries.MaximumP99LatencyMilliseconds < adversaries.RequestTimeoutMilliseconds {
		return errors.New("adversarial evidence thresholds are incomplete or unsafe")
	}
	if adversaries.MaximumAttackControlP95Ratio < 1_000_000 || adversaries.MaximumAttackControlP95Ratio > 50_000_000 {
		return errors.New("adversarial attack/control p95 ratio must be in [1000000,50000000] ppm")
	}
	if adversaries.MaximumOperatorRequestsPerSec < 1 || adversaries.MaximumOperatorRequestsPerSec > 20 || adversaries.MaximumRPCRequestsPerSec < 1 || adversaries.MaximumRPCRequestsPerSec > 5 {
		return errors.New("adversarial request ceilings exceed the shared-testnet safety envelope")
	}
	if c.Dependencies.ObjectStore != "server-blob" || c.Artifacts.Writer != "server-blob" || c.Artifacts.HistoryAPI != "server-api" || !c.Artifacts.ContentAddressed {
		return errors.New("artifacts must use content-addressed server/blob and server API history")
	}
	// Each NO consumes a deposit and pool UID; validators include the reserve
	// validator; every fleet consumes one UID; and the immutable claims escrow
	// needs its own valid hotkey for moveStake/transferStake.
	requiredRegistrations := 2*c.Topology.Operators + c.Topology.Validators + c.Topology.HeadFleets + 1
	if c.Budgets.MaximumRegistrations < requiredRegistrations {
		return errors.New("registration budget is below topology requirement")
	}
	if c.Budgets.MaximumRegistrationBurnRao == 0 {
		return errors.New("maximum registration burn must be nonzero")
	}
	for name, ref := range map[string]string{"wallet": c.LaunchInputs.Wallet, "wallet password": c.LaunchInputs.WalletPassword, "chain_id": c.LaunchInputs.ChainID, "authority": c.LaunchInputs.Authority, "object store hostname": c.LaunchInputs.ObjectStoreHostname, "trusted proxy CIDRs": c.LaunchInputs.TrustedProxyCIDRs, "operator API origins": c.LaunchInputs.OperatorAPIOrigins, "netuid": c.Deployment.NetuidFrom, "tao budget": c.Budgets.MaximumTotalTAORaoFrom, "alpha budget": c.Budgets.MaximumTotalAlphaRaoFrom, "gas budget": c.Budgets.MaximumEVMGasWeiFrom} {
		if !strings.HasPrefix(ref, "vault://main/st.yml#testnet-") {
			return fmt.Errorf("%s must reference a testnet-prefixed st.yml key", name)
		}
	}
	return nil
}

// Convert YAML's supported scalar forms without allowing negative wraparound.
func parseUnsignedVaultValue(reference string, value any) (uint64, error) {
	switch typed := value.(type) {
	case int:
		if typed < 0 {
			return 0, fmt.Errorf("vault value %q is negative", reference)
		}
		return uint64(typed), nil
	case uint64:
		return typed, nil
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("vault value %q is not an unsigned integer: %w", reference, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("vault value %q is not an unsigned integer", reference)
	}
}

func (r *ResolvedConfig) resolveVaultInputs(require bool) error {
	get := func(ref string) (any, error) {
		const p = "vault://main/st.yml#"
		if !strings.HasPrefix(ref, p) {
			return nil, fmt.Errorf("unsupported vault reference %q", ref)
		}
		key := strings.TrimPrefix(ref, p)
		if !strings.HasPrefix(key, "testnet-") {
			return nil, fmt.Errorf("refusing non-testnet vault key %q", key)
		}
		v, ok := r.Vault[key]
		if !ok {
			return nil, fmt.Errorf("vault key %q missing", key)
		}
		return v, nil
	}
	uintv := func(ref string) (uint64, error) {
		v, e := get(ref)
		if e != nil {
			return 0, e
		}
		return parseUnsignedVaultValue(ref, v)
	}
	v, err := get(r.Config.LaunchInputs.Wallet)
	if err != nil {
		return err
	}
	r.WalletSecret = strings.TrimSpace(fmt.Sprint(v))
	passwordValue, err := get(r.Config.LaunchInputs.WalletPassword)
	if err != nil {
		return err
	}
	r.WalletPasswordSecret = strings.TrimSpace(fmt.Sprint(passwordValue))
	if strings.HasPrefix(r.WalletSecret, "vault-wallet:") {
		r.WalletMaterial, r.WalletPublic, r.WalletHotkeyPublic, r.WalletPassword, err = resolveVaultWallet(r.Repos.Vault, r.WalletSecret, r.WalletPasswordSecret, require)
		if err != nil {
			return fmt.Errorf("testnet wallet: %w", err)
		}
	} else if r.WalletSecret != "" {
		r.WalletMaterial, err = resolveSecretValue(r.WalletSecret, require)
		if err != nil {
			return fmt.Errorf("testnet wallet secret reference: %w", err)
		}
	}
	n, err := uintv(r.Config.Deployment.NetuidFrom)
	if err != nil {
		return err
	}
	if n > 65535 {
		return errors.New("testnet netuid exceeds uint16")
	}
	r.Netuid = uint16(n)
	if r.ChainID, err = uintv(r.Config.LaunchInputs.ChainID); err != nil {
		return err
	}
	v, err = get(r.Config.LaunchInputs.Authority)
	if err != nil {
		return err
	}
	r.Authority = strings.TrimSpace(fmt.Sprint(v))
	if r.Authority, err = resolveEnvTemplates(r.Authority, require); err != nil {
		return fmt.Errorf("testnet authority: %w", err)
	}
	v, err = get(r.Config.LaunchInputs.ObjectStoreHostname)
	if err != nil {
		return err
	}
	r.ObjectStoreHost = strings.TrimSpace(fmt.Sprint(v))
	v, err = get(r.Config.LaunchInputs.TrustedProxyCIDRs)
	if err != nil {
		return err
	}
	r.TrustedProxyCIDRs = strings.TrimSpace(fmt.Sprint(v))
	v, err = get(r.Config.LaunchInputs.OperatorAPIOrigins)
	if err != nil {
		return err
	}
	r.OperatorAPIOrigins, err = stringListValue(v)
	if err != nil {
		return fmt.Errorf("testnet operator API origins: %w", err)
	}
	if r.MaximumTAORao, err = uintv(r.Config.Budgets.MaximumTotalTAORaoFrom); err != nil {
		return err
	}
	if r.MaximumAlphaRao, err = uintv(r.Config.Budgets.MaximumTotalAlphaRaoFrom); err != nil {
		return err
	}
	if r.MaximumEVMGasWei, err = uintv(r.Config.Budgets.MaximumEVMGasWeiFrom); err != nil {
		return err
	}
	if r.WalletMaterial != "" {
		ring, err := signature.KeyringPairFromSecret(r.WalletMaterial, 42)
		if err != nil {
			return fmt.Errorf("testnet wallet signer: %w", err)
		}
		r.WalletPublic = ring.Address
	}
	if require {
		if r.WalletMaterial == "" {
			return errors.New("testnet-wallet is empty")
		}
		if r.Netuid == 0 {
			return errors.New("testnet-netuid is zero")
		}
		if r.MaximumTAORao == 0 || r.MaximumAlphaRao == 0 || r.MaximumEVMGasWei == 0 {
			return errors.New("all three testnet spending limits must be nonzero")
		}
		if r.Authority == "" {
			return errors.New("testnet-authority is empty")
		}
		if r.ObjectStoreHost == "" || r.TrustedProxyCIDRs == "" {
			return errors.New("testnet object-store hostname and trusted-proxy CIDRs must be nonempty")
		}
		if len(r.OperatorAPIOrigins) != r.Config.Topology.Operators {
			return fmt.Errorf("testnet-operator-api-origins must contain %d public origins", r.Config.Topology.Operators)
		}
	}
	return nil
}

// Minimal Bittensor wallet keyfile envelope. Public files must contain only
// identity fields; the decrypted coldkey supplies exactly one signer source.
type btwalletKeyfile struct {
	AccountID    string `json:"accountId"`
	PublicKey    string `json:"publicKey"`
	PrivateKey   string `json:"privateKey"`
	SecretPhrase string `json:"secretPhrase"`
	SecretSeed   string `json:"secretSeed"`
	SS58Address  string `json:"ss58Address"`
	CryptoType   *uint8 `json:"cryptoType"`
}

// Report whether a resolved candidate remains confined to a repository root.
func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// resolveVaultRelativePath confines portable vault-file/vault-wallet references
// to the checked-out vault repository and rejects symlink escapes.
func resolveVaultRelativePath(vaultRepo, reference, prefix string, require bool) (string, error) {
	if !strings.HasPrefix(reference, prefix) {
		return "", fmt.Errorf("reference must start with %s", prefix)
	}
	relative := strings.TrimSpace(strings.TrimPrefix(reference, prefix))
	if relative == "" || filepath.IsAbs(relative) {
		return "", errors.New("vault-relative reference must be a nonempty relative path")
	}
	root, err := filepath.EvalSymlinks(filepath.Clean(vaultRepo))
	if err != nil {
		return "", fmt.Errorf("resolve vault repository: %w", err)
	}
	candidate := filepath.Clean(filepath.Join(root, relative))
	if !pathWithin(root, candidate) {
		return "", errors.New("vault-relative reference escapes the vault repository")
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if errors.Is(err, os.ErrNotExist) && !require {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve vault-relative reference: %w", err)
	}
	if !pathWithin(root, resolved) {
		return "", errors.New("vault-relative symlink escapes the vault repository")
	}
	return resolved, nil
}

// Read a secret only from a regular owner-private file.
func readPrivateVaultFile(path string, require bool) (string, error) {
	if path == "" && !require {
		return "", nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("vault secret must be a regular file")
	}
	permissions := info.Mode().Perm()
	if permissions&0o077 != 0 || permissions&0o400 == 0 {
		return "", fmt.Errorf("vault secret permissions must be owner-readable and deny group/other access (got %04o)", permissions)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(b))
	if value == "" && require {
		return "", errors.New("vault secret file is empty")
	}
	return value, nil
}

// Resolve a fixed wallet child without allowing the keyfile tree to escape
// through a nested symlink. Secret keyfiles must also remain owner-private.
func resolveVaultWalletFile(walletDir, relative string, private bool) (string, error) {
	root, err := filepath.EvalSymlinks(filepath.Clean(walletDir))
	if err != nil {
		return "", fmt.Errorf("resolve wallet directory: %w", err)
	}
	candidate := filepath.Clean(filepath.Join(root, relative))
	if !pathWithin(root, candidate) {
		return "", errors.New("wallet file escapes the wallet directory")
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve wallet file: %w", err)
	}
	if !pathWithin(root, resolved) {
		return "", errors.New("wallet file symlink escapes the wallet directory")
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("wallet keyfile must be a regular file")
	}
	if private && (info.Mode().Perm()&0o077 != 0 || info.Mode().Perm()&0o400 == 0) {
		return "", fmt.Errorf("wallet keyfile permissions must be owner-readable and deny group/other access (got %04o)", info.Mode().Perm())
	}
	return resolved, nil
}

// Decode the legacy $NACL envelope using Bittensor's fixed Argon2i profile.
// Parameters remain injectable so unit tests can use a small memory cost.
func decryptBTWALLETNACL(encrypted []byte, password string, time uint32, memoryKiB uint32, threads uint8) ([]byte, error) {
	if !bytes.HasPrefix(encrypted, []byte(btwalletNACLPrefix)) || len(encrypted) < len(btwalletNACLPrefix)+24+secretbox.Overhead {
		return nil, errors.New("coldkey is not a valid $NACL keyfile")
	}
	derived := argon2.Key([]byte(password), btwalletNACLSalt[:], time, memoryKiB, threads, 32)
	var key [32]byte
	copy(key[:], derived)
	for i := range derived {
		derived[i] = 0
	}
	offset := len(btwalletNACLPrefix)
	var nonce [24]byte
	copy(nonce[:], encrypted[offset:offset+len(nonce)])
	plain, ok := secretbox.Open(nil, encrypted[offset+len(nonce):], &nonce, &key)
	for i := range key {
		key[i] = 0
	}
	if !ok {
		return nil, errors.New("coldkey decryption failed")
	}
	return plain, nil
}

// Validate a public-only sr25519 identity without accepting secret fields.
func readBTWALLETPublic(path string) (btwalletKeyfile, error) {
	var public btwalletKeyfile
	b, err := os.ReadFile(path)
	if err != nil {
		return public, err
	}
	if err := json.Unmarshal(b, &public); err != nil {
		return public, fmt.Errorf("decode public keyfile: %w", err)
	}
	if public.SS58Address == "" || public.PublicKey == "" || public.PrivateKey != "" || public.SecretPhrase != "" || public.SecretSeed != "" {
		return public, errors.New("public keyfile is missing identity fields or contains secret material")
	}
	if public.CryptoType != nil && *public.CryptoType != 1 {
		return public, errors.New("wallet keyfile must use sr25519")
	}
	publicKey, err := hex.DecodeString(strings.TrimPrefix(public.PublicKey, "0x"))
	if err != nil || len(publicKey) != 32 {
		return public, errors.New("public keyfile has an invalid publicKey")
	}
	if public.AccountID != "" && !strings.EqualFold(public.AccountID, public.PublicKey) {
		return public, errors.New("public keyfile accountId does not match publicKey")
	}
	addressPublicKey, err := ss58.DecodeWithPrefix(public.SS58Address, ss58.BittensorPrefix)
	if err != nil || !bytes.Equal(addressPublicKey[:], publicKey) {
		return public, errors.New("public keyfile ss58Address does not match publicKey")
	}
	return public, nil
}

// Bind decrypted signer material to the separately stored public identity.
func materialFromBTWALLET(secret, public btwalletKeyfile) (string, error) {
	if secret.CryptoType != nil && *secret.CryptoType != 1 {
		return "", errors.New("wallet coldkey must use sr25519")
	}
	material := strings.TrimSpace(secret.SecretPhrase)
	if material == "" {
		material = strings.TrimSpace(secret.SecretSeed)
	}
	if material == "" {
		return "", errors.New("wallet coldkey has no mnemonic or seed")
	}
	ring, err := signature.KeyringPairFromSecret(material, 42)
	if err != nil {
		return "", errors.New("derive wallet coldkey signer")
	}
	publicBytes, err := hex.DecodeString(strings.TrimPrefix(public.PublicKey, "0x"))
	if err != nil || len(publicBytes) != 32 || !bytes.Equal(ring.PublicKey, publicBytes) {
		return "", errors.New("decrypted coldkey does not match coldkeypub")
	}
	if ring.Address != public.SS58Address || (secret.SS58Address != "" && secret.SS58Address != public.SS58Address) {
		return "", errors.New("decrypted coldkey does not match coldkeypub")
	}
	return material, nil
}

// Resolve a portable wallet directory and password reference. Read-only loads
// expose identities but never decrypt secret material.
func resolveVaultWallet(vaultRepo, walletReference, passwordReference string, require bool) (material, public, hotkeyPublic, password string, err error) {
	walletDir, err := resolveVaultRelativePath(vaultRepo, walletReference, "vault-wallet:", require)
	if err != nil || walletDir == "" {
		return "", "", "", "", err
	}
	info, err := os.Stat(walletDir)
	if err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("wallet reference is not a directory")
		}
		return "", "", "", "", err
	}
	coldPublicPath, err := resolveVaultWalletFile(walletDir, "coldkeypub.txt", false)
	if err != nil {
		return "", "", "", "", err
	}
	coldPublic, err := readBTWALLETPublic(coldPublicPath)
	if err != nil {
		return "", "", "", "", err
	}
	hotPublicPath, err := resolveVaultWalletFile(walletDir, filepath.Join("hotkeys", "defaultpub.txt"), false)
	if err != nil {
		return "", "", "", "", fmt.Errorf("default hotkey: %w", err)
	}
	hotPublic, err := readBTWALLETPublic(hotPublicPath)
	if err != nil {
		return "", "", "", "", fmt.Errorf("default hotkey: %w", err)
	}
	if !require {
		return "", coldPublic.SS58Address, hotPublic.SS58Address, "", nil
	}
	passwordPath, err := resolveVaultRelativePath(vaultRepo, passwordReference, "vault-file:", true)
	if err != nil {
		return "", "", "", "", fmt.Errorf("wallet password: %w", err)
	}
	password, err = readPrivateVaultFile(passwordPath, true)
	if err != nil {
		return "", "", "", "", fmt.Errorf("wallet password: %w", err)
	}
	coldkeyPath, err := resolveVaultWalletFile(walletDir, "coldkey", true)
	if err != nil {
		return "", "", "", "", err
	}
	encrypted, err := os.ReadFile(coldkeyPath)
	if err != nil {
		return "", "", "", "", err
	}
	plain, err := decryptBTWALLETNACL(encrypted, password, btwalletArgonTime, btwalletArgonMemoryKiB, btwalletArgonThreads)
	if err != nil {
		return "", "", "", "", err
	}
	defer func() {
		for i := range plain {
			plain[i] = 0
		}
	}()
	var secret btwalletKeyfile
	if err := json.Unmarshal(plain, &secret); err != nil {
		return "", "", "", "", errors.New("decrypted coldkey is not valid JSON")
	}
	material, err = materialFromBTWALLET(secret, coldPublic)
	if err != nil {
		return "", "", "", "", err
	}
	return material, coldPublic.SS58Address, hotPublic.SS58Address, password, nil
}

func stringListValue(value any) ([]string, error) {
	var values []string
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			values = append(values, strings.TrimSpace(fmt.Sprint(item)))
		}
	case []string:
		for _, item := range typed {
			values = append(values, strings.TrimSpace(item))
		}
	case string:
		for _, item := range strings.Split(typed, ",") {
			if strings.TrimSpace(item) != "" {
				values = append(values, strings.TrimSpace(item))
			}
		}
	default:
		return nil, fmt.Errorf("expected a YAML sequence or comma-separated string, got %T", value)
	}
	for _, item := range values {
		if item == "" {
			return nil, errors.New("origin cannot be empty")
		}
	}
	return values, nil
}

var envTemplatePattern = regexp.MustCompile(`\{\{\s*env:([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

func resolveEnvTemplates(value string, require bool) (string, error) {
	var missing string
	resolved := envTemplatePattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := envTemplatePattern.FindStringSubmatch(match)
		v, ok := os.LookupEnv(parts[1])
		if !ok || strings.TrimSpace(v) == "" {
			missing = parts[1]
			return match
		}
		return strings.TrimSpace(v)
	})
	if missing != "" && require {
		return "", fmt.Errorf("environment variable %s is empty", missing)
	}
	return resolved, nil
}

var secretEnvironmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// resolveSecretValue resolves the only two indirections accepted for the
// testnet wallet. Write-capable loads fail closed when the target is absent;
// read-only loads may remain secretless, but malformed or insecure references
// are rejected in every mode.
func resolveSecretValue(value string, require bool) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "env:") {
		name := strings.TrimSpace(strings.TrimPrefix(value, "env:"))
		if !secretEnvironmentNamePattern.MatchString(name) {
			return "", errors.New("env reference has an invalid variable name")
		}
		material := strings.TrimSpace(os.Getenv(name))
		if material == "" && require {
			return "", fmt.Errorf("environment variable %s is empty", name)
		}
		return material, nil
	}
	if strings.HasPrefix(value, "file:") {
		path := strings.TrimSpace(strings.TrimPrefix(value, "file:"))
		if !filepath.IsAbs(path) {
			return "", errors.New("file reference must use an absolute path")
		}
		info, err := os.Lstat(filepath.Clean(path))
		if errors.Is(err, os.ErrNotExist) && !require {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("secret file metadata: %w", err)
		}
		if !info.Mode().IsRegular() {
			return "", errors.New("secret file must be a regular file, not a symlink or device")
		}
		permissions := info.Mode().Perm()
		if permissions&0o077 != 0 || permissions&0o400 == 0 {
			return "", fmt.Errorf("secret file permissions must be owner-readable and deny group/other access (got %04o)", permissions)
		}
		b, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return "", fmt.Errorf("read secret file: %w", err)
		}
		material := strings.TrimSpace(string(b))
		if material == "" && require {
			return "", errors.New("secret file is empty")
		}
		return material, nil
	}
	return value, nil
}

func (r *ResolvedConfig) Validate() error {
	if r.Public.SchemaVersion != 1 || r.Public.Profile != releaseProfile || r.Public.Chain.ChainID != testnetChainID || strings.ToLower(r.Public.Chain.GenesisHash) != testnetGenesis {
		return errors.New("public manifest is not the pinned Bittensor testnet release profile")
	}
	if r.Public.Chain.ExpectedBlockSeconds == 0 {
		return errors.New("public manifest must declare a nonzero block cadence")
	}
	if r.ChainID != 0 && r.ChainID != testnetChainID {
		return fmt.Errorf("vault testnet chain id %d, want %d", r.ChainID, testnetChainID)
	}
	if r.Release.Release != "1.0" || r.Release.Runtime.SpecVersion != r.Public.Chain.ExpectedRuntimeSpec {
		return errors.New("release/runtime manifest mismatch")
	}
	if r.Hyperparameters.SchemaVersion != 1 || r.Hyperparameters.Profile != releaseProfile {
		return errors.New("invalid hyperparameter manifest")
	}
	if r.Policy.ProductionCadence.AfterAcceleratedEpochs != uint64(r.Config.Scenarios.ShortEpochs) {
		return fmt.Errorf("production cadence requires %d accelerated epochs, scenario config requests %d", r.Policy.ProductionCadence.AfterAcceleratedEpochs, r.Config.Scenarios.ShortEpochs)
	}
	if len(r.Hyperparameters.ProductionOwnerControlled) != 1 || hyperparameterUint64(r.Hyperparameters.ProductionOwnerControlled["immunity_period"]) != r.Policy.ProductionCadence.EpochBlocks {
		return errors.New("production hyperparameters must set immunity_period to the production epoch length")
	}
	if len(r.OperatorAPIOrigins) != 0 {
		origins, err := validateOperatorAPIOrigins(r.OperatorAPIOrigins, r.Config.Topology.Operators)
		if err != nil {
			return err
		}
		r.OperatorAPIOrigins = origins
	}
	if err := validateCompatibilityGates(r.Hyperparameters.ObservedCompatibilityGates); err != nil {
		return err
	}
	if len(r.Policy.Deposit.Tiers) < 2 || r.Policy.Deposit.Tiers[1].MinConvictionRao != r.Config.Scenarios.VoluntaryConvictionRao {
		return errors.New("voluntary conviction must equal the first nonzero tier boundary")
	}
	perOperator, ok := checkedMul(r.Policy.Deposit.EpochCapRaoPerOperator, uint64(r.Config.Scenarios.ShortEpochs))
	if !ok {
		return errors.New("accelerated campaign deposit requirement overflows uint64")
	}
	required, ok := checkedMul(perOperator, uint64(r.Config.Topology.Operators))
	if !ok {
		return errors.New("accelerated campaign deposit requirement overflows uint64")
	}
	required, ok = checkedAdd(required, r.Config.Scenarios.VoluntaryConvictionRao)
	if !ok || r.Policy.Deposit.TotalTestCampaignCapRao < required {
		return fmt.Errorf("campaign cap %d cannot fund %d accelerated epochs plus voluntary conviction; require at least %d", r.Policy.Deposit.TotalTestCampaignCapRao, r.Config.Scenarios.ShortEpochs, required)
	}
	return nil
}

func validateOperatorAPIOrigins(origins []string, operators int) ([]string, error) {
	if len(origins) != operators {
		return nil, fmt.Errorf("operator API origins has %d entries, want %d", len(origins), operators)
	}
	result := make([]string, len(origins))
	seen := map[string]bool{}
	for i, origin := range origins {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return nil, fmt.Errorf("operator API origin %d is not a bare http(s) origin", i+1)
		}
		canonical := strings.TrimSuffix(origin, "/")
		if seen[canonical] {
			return nil, fmt.Errorf("operator API origin %d duplicates %q", i+1, canonical)
		}
		seen[canonical] = true
		result[i] = canonical
	}
	return result, nil
}

func validateCompatibilityGates(gates map[string]CompatibilityGate) error {
	required := []string{"alpha_values", "bonds_penalty", "max_allowed_validators", "max_weight_limit", "min_delegate_take", "registration_burn", "subnet_owner_cut", "tao_weight"}
	for _, name := range required {
		gate, ok := gates[name]
		if !ok {
			return fmt.Errorf("compatibility gate %s is missing", name)
		}
		if gate.Storage == "" || (gate.Scope != "global" && gate.Scope != "netuid") || (gate.Kind != "u16" && gate.Kind != "u64" && gate.Kind != "u16_pair") || (gate.Rule != "exact" && gate.Rule != "nonzero") || strings.TrimSpace(gate.Decision) == "" {
			return fmt.Errorf("compatibility gate %s is incomplete", name)
		}
		want := 1
		if gate.Kind == "u16_pair" {
			want = 2
		}
		if gate.Rule == "exact" && len(gate.Expected) != want {
			return fmt.Errorf("compatibility gate %s requires %d expected values", name, want)
		}
		if gate.Rule == "nonzero" && len(gate.Expected) != 0 {
			return fmt.Errorf("compatibility gate %s nonzero rule cannot have expected values", name)
		}
	}
	return nil
}

func canonicalHashHex(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return "0x" + hex.EncodeToString(h[:]), nil
}

func discoverRepos(configPath string, opts LoadOptions) (RepoPaths, error) {
	sn := opts.SNRepo
	if sn == "" {
		sn = findModule(filepath.Dir(configPath), "github.com/urfoundation/sn")
	}
	if sn == "" {
		return RepoPaths{}, errors.New("cannot discover sn repository; use --sn-repo")
	}
	parent := filepath.Dir(sn)
	server := opts.ServerRepo
	if server == "" {
		server = findSiblingModule(parent, "github.com/urnetwork/server")
	}
	vault := opts.VaultRepo
	if vault == "" {
		candidate := filepath.Join(parent, "vault")
		if fileExists(filepath.Join(candidate, "main", "st.yml")) {
			vault = candidate
		}
	}
	platformConfig := opts.PlatformConfigRepo
	if platformConfig == "" {
		candidate := filepath.Join(parent, "config")
		if fileExists(filepath.Join(candidate, "local", "settings.yml")) {
			platformConfig = candidate
		}
	}
	if server == "" {
		return RepoPaths{}, errors.New("cannot discover server repository; use --server-repo")
	}
	if vault == "" {
		return RepoPaths{}, errors.New("cannot discover vault repository; use --vault-repo")
	}
	if platformConfig == "" {
		return RepoPaths{}, errors.New("cannot discover platform config repository; use --platform-config-repo")
	}
	return RepoPaths{SN: cleanAbs(sn), Server: cleanAbs(server), Vault: cleanAbs(vault), PlatformConfig: cleanAbs(platformConfig)}, nil
}
func findModule(start, module string) string {
	d := cleanAbs(start)
	for {
		b, e := os.ReadFile(filepath.Join(d, "go.mod"))
		if e == nil && strings.Contains(string(b), "module "+module) {
			return d
		}
		p := filepath.Dir(d)
		if p == d {
			return ""
		}
		d = p
	}
}
func findSiblingModule(parent, module string) string {
	ents, _ := os.ReadDir(parent)
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		d := filepath.Join(parent, e.Name())
		b, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil && strings.Contains(string(b), "module "+module) {
			return d
		}
	}
	return ""
}
func cleanAbs(p string) string { a, _ := filepath.Abs(p); return filepath.Clean(a) }
func fileExists(p string) bool { _, e := os.Stat(p); return e == nil }

// MarshalJSON prevents an accidental serialization of loaded wallet material.
func (r ResolvedConfig) MarshalJSON() ([]byte, error) {
	type public struct {
		ConfigPath        string         `json:"config_path"`
		Config            *HarnessConfig `json:"config"`
		Netuid            uint16         `json:"netuid"`
		ChainID           uint64         `json:"chain_id"`
		Authority         string         `json:"authority"`
		ObjectStore       string         `json:"object_store_hostname"`
		TrustedProxyCIDRs string         `json:"trusted_proxy_cidrs"`
		WalletPublic      string         `json:"wallet_public"`
		PolicyHash        string         `json:"policy_hash"`
		ConfigHash        string         `json:"config_hash"`
	}
	authority, _, err := authorityURLs(r.Authority)
	if err != nil {
		authority = "<redacted-url>"
	} else {
		authority = redactURL(authority)
	}
	return json.Marshal(public{
		ConfigPath:        r.ConfigPath,
		Config:            r.Config,
		Netuid:            r.Netuid,
		ChainID:           r.ChainID,
		Authority:         authority,
		ObjectStore:       r.ObjectStoreHost,
		TrustedProxyCIDRs: r.TrustedProxyCIDRs,
		WalletPublic:      r.WalletPublic,
		PolicyHash:        r.PolicyHash,
		ConfigHash:        r.ConfigHash,
	})
}
