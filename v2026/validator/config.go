package validator

// config.go contains the fail-closed release-1.0 validator configuration.
// The legacy flag surface remains measurement-only for development. Every
// weight-writing validator must be started with --config and all operator
// contexts are declared explicitly. In particular, credentials, state and
// statistics are never shared between network operators.

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/v2026/protocol"
)

const ReleaseValidatorSchemaVersion = 1
const ReleaseMainnetProductionSchemaVersion = 3
const maximumReleaseConfigBytes = 2 * 1024 * 1024

type OperatorConfig struct {
	// Original Server receipt domain is explicit; nil preserves unknown legacy coverage.
	RequestReceiptScope     *protocol.ProviderAttemptReceiptScope `yaml:"request_receipt_scope,omitempty" json:"request_receipt_scope,omitempty"`
	RequestPreparation      *ReleaseEvidenceV2File                `yaml:"request_preparation,omitempty" json:"request_preparation,omitempty"`
	NoID                    uint64                                `yaml:"no_id" json:"no_id"`
	APIURL                  string                                `yaml:"api_url" json:"api_url"`
	ConnectURL              string                                `yaml:"connect_url" json:"connect_url"`
	ArtifactSigner          string                                `yaml:"artifact_signer" json:"artifact_signer"`
	StateDir                string                                `yaml:"state_dir" json:"state_dir"`
	NetworkJWTFile          string                                `yaml:"network_jwt_file" json:"network_jwt_file"`
	ClientJWTFile           string                                `yaml:"client_jwt_file" json:"client_jwt_file"`
	AllowClientRegistration bool                                  `yaml:"allow_client_registration,omitempty" json:"allow_client_registration,omitempty"`
	ClientKeySeedFile       string                                `yaml:"client_key_seed_file" json:"client_key_seed_file"`
	Concurrency             int                                   `yaml:"concurrency" json:"concurrency"`
}

type ReleaseConfig struct {
	SchemaVersion       int                     `yaml:"schema_version" json:"schema_version"`
	Production          bool                    `yaml:"production" json:"production"`
	Release             string                  `yaml:"release" json:"release"`
	DeploymentID        string                  `yaml:"deployment_id" json:"deployment_id"`
	ValidatorID         uint64                  `yaml:"validator_id" json:"validator_id"`
	ChainID             uint64                  `yaml:"chain_id" json:"chain_id"`
	GenesisHash         string                  `yaml:"genesis_hash" json:"genesis_hash"`
	RuntimeSpec         uint32                  `yaml:"runtime_spec" json:"runtime_spec"`
	TransactionVersion  uint32                  `yaml:"transaction_version" json:"transaction_version"`
	StateVersion        uint8                   `yaml:"state_version" json:"state_version"`
	RuntimeCodeHash     string                  `yaml:"runtime_code_hash" json:"runtime_code_hash"`
	RuntimeMetadataHash string                  `yaml:"runtime_metadata_hash" json:"runtime_metadata_hash"`
	Netuid              uint16                  `yaml:"netuid" json:"netuid"`
	Coordinator         string                  `yaml:"coordinator" json:"coordinator"`
	SettlementVault     string                  `yaml:"settlement_vault" json:"settlement_vault"`
	DeployBlock         uint64                  `yaml:"deploy_block" json:"deploy_block"`
	PolicyHash          string                  `yaml:"policy_hash" json:"policy_hash"`
	RPC                 []string                `yaml:"rpc" json:"rpc"`
	Substrate           []string                `yaml:"substrate" json:"substrate"`
	StateDir            string                  `yaml:"state_dir" json:"state_dir"`
	HotkeySeedFile      string                  `yaml:"hotkey_seed_file" json:"hotkey_seed_file"`
	ControlledNOIDs     []uint64                `yaml:"controlled_no_ids" json:"controlled_no_ids"`
	TrailDepth          int                     `yaml:"trail_depth" json:"trail_depth"`
	PollSeconds         int                     `yaml:"poll_seconds" json:"poll_seconds"`
	VersionKey          uint64                  `yaml:"version_key" json:"version_key"`
	PreviousPolicy      *protocol.Policy        `yaml:"previous_policy,omitempty" json:"previous_policy,omitempty"`
	Policy              protocol.Policy         `yaml:"policy" json:"policy"`
	Operators           []OperatorConfig        `yaml:"operators" json:"operators"`
	EvidenceV2          ReleaseEvidenceV2Config `yaml:"evidence_v2" json:"evidence_v2"`

	SourceRolePredecessorV2    *ReleaseEvidenceV2File             `yaml:"source_role_predecessor_v2,omitempty" json:"source_role_predecessor_v2,omitempty"`
	OwnerRecycleApproval       *ReleaseOwnerRecycleApprovalConfig `yaml:"owner_recycle_approval,omitempty" json:"owner_recycle_approval,omitempty"`
	TreasuryApproval           *ReleaseTreasuryApprovalConfig     `yaml:"treasury_approval,omitempty" json:"treasury_approval,omitempty"`
	MainnetRuntimeApprovals    []ReleaseEvidenceV2File            `yaml:"mainnet_runtime_approvals,omitempty" json:"mainnet_runtime_approvals,omitempty"`
	ProductionRuntimeApprovals []ReleaseEvidenceV2File            `yaml:"production_runtime_approvals,omitempty" json:"production_runtime_approvals,omitempty"`
	ProductionAuthorityHistory []ReleaseEvidenceV2File            `yaml:"production_authority_history,omitempty" json:"production_authority_history,omitempty"`
	ProductionCapacityRevision *ProductionCapacityRevision        `yaml:"production_capacity_revision,omitempty" json:"production_capacity_revision,omitempty"`

	ProvisionalDeferClosedNativeInput bool   `yaml:"provisional_defer_closed_native_input,omitempty" json:"provisional_defer_closed_native_input,omitempty"`
	ProvisionalRuntimeCompatibility   string `yaml:"provisional_runtime_compatibility,omitempty" json:"provisional_runtime_compatibility,omitempty"`
	RuntimeSuccessorProfile           string `yaml:"runtime_successor_profile,omitempty" json:"runtime_successor_profile,omitempty"`
	historyAdoptionV2                 *ReleaseHistoryAdoptionV2
	mainnetRuntimeHistory             *releaseMainnetRuntimeHistory
	ownerRecycleProduction            *ownerRecycleProductionAuthority
	productionRuntimeHistory          *releaseProductionRuntimeHistory
	productionAuthorityHistory        *releaseProductionAuthorityHistory
	// Server staging's pinned config directory, a last retained-copy location.
	// Unexported, so neither the YAML/JSON schema nor any config hash sees it.
	stagingConfigDirectory string
}

func LoadReleaseConfig(path string) (*ReleaseConfig, error) {
	return loadReleaseConfig(path, releaseConfigLoadMode{})
}

// LoadProvisionalActivationObservationConfig admits a retained testnet config
// only for a hash-pinned, read-only activation observation. It accepts an
// exact reviewed predecessor runtime, but never grants producer or archive
// authority.
func LoadProvisionalActivationObservationConfig(path string) (*ReleaseConfig, error) {
	return loadReleaseConfig(path, releaseConfigLoadMode{provisionalActivationObservation: true})
}

// LoadReleaseConfigPreActivation admits a complete production configuration
// whose evidence_v2 operator entries are not rendered yet (each names only its
// no_id, optionally with the paths it wants). Every other rule is the strict
// one. It serves the bootstrap commands (init, register, stake, activate,
// status); RunRelease never uses it.
func LoadReleaseConfigPreActivation(path string) (*ReleaseConfig, error) {
	return loadReleaseConfig(path, releaseConfigLoadMode{preActivation: true})
}

// LoadReleaseProductionConfigPreActivation authenticates a signed schema-3
// configuration exactly as the producer loader does, except that its complete
// evidence_v2 census is activation-pending: every entry is unrendered and
// pre-declares all of its paths. Only `validator activate` uses it, to render
// those inputs into a separate successor that the approval key re-approves;
// RunRelease never does.
func LoadReleaseProductionConfigPreActivation(path string) (*ReleaseConfig, error) {
	return loadReleaseConfig(path, releaseConfigLoadMode{productionPreActivation: true})
}

// releaseConfigLoadMode selects which non-default admissions a loader grants.
type releaseConfigLoadMode struct {
	provisionalActivationObservation bool
	preActivation                    bool
	productionPreActivation          bool
	ownerRecycleAdmission            bool
	mainnetRuntimeObservation        bool
	// Only server staging sets this: it mounts no validator state_dir.
	stagingConfigDirectory string
}

func loadReleaseConfig(path string, mode releaseConfigLoadMode) (*ReleaseConfig, error) {
	abs, b, err := readReleaseConfigFile(path)
	if err != nil {
		return nil, err
	}
	return decodeReleaseConfigBytesMode(abs, b, mode)
}

// Reads the bounded regular file once; callers decode exactly these bytes.
func readReleaseConfigFile(path string) (string, []byte, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil, errors.New("validator config path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", nil, err
	}
	file, err := os.Open(abs)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maximumReleaseConfigBytes {
		return "", nil, errors.Join(errors.New("validator config is not a bounded regular file"), err)
	}
	b, err := io.ReadAll(io.LimitReader(file, maximumReleaseConfigBytes+1))
	if err != nil {
		return "", nil, err
	}
	return abs, b, nil
}

// The adoption caller must parse the same immutable bytes that its request
// pins, rather than pair a prior parse with a later pathname read.
func decodeReleaseConfigBytes(abs string, b []byte) (*ReleaseConfig, error) {
	return decodeReleaseConfigBytesMode(abs, b, releaseConfigLoadMode{})
}

// Parse the exact borrowed document once, preserving the regular loader's
// strict grammar and normalization without loading any approval or key.
func decodeReleaseConfigDocument(abs string, b []byte) (*ReleaseConfig, error) {
	if len(b) == 0 || len(b) > maximumReleaseConfigBytes {
		return nil, errors.New("validator config is empty or exceeds its byte bound")
	}
	var cfg ReleaseConfig
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode validator config %s: %w", abs, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("decode validator config %s: trailing YAML: %w", abs, err)
		}
		return nil, fmt.Errorf("decode validator config %s: multiple YAML documents", abs)
	}
	if err := ValidateReleaseEvidenceV2ConfigYAML(b); err != nil {
		return nil, fmt.Errorf("decode validator config %s: %w", abs, err)
	}
	if err := validateProductionCapacityDocument(b); err != nil {
		return nil, fmt.Errorf("decode validator capacity revision %s: %w", abs, err)
	}
	if err := cfg.normalize(filepath.Dir(abs)); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Purpose-specific authority is installed only after the common byte decoder.
func decodeReleaseConfigBytesMode(abs string, b []byte, mode releaseConfigLoadMode) (*ReleaseConfig, error) {
	decoded, err := decodeReleaseConfigDocument(abs, b)
	if err != nil {
		return nil, err
	}
	cfg := *decoded
	provisionalActivationObservation := mode.provisionalActivationObservation
	if cfg.SchemaVersion == ReleaseMainnetProductionSchemaVersion {
		if mode.mainnetRuntimeObservation || mode.ownerRecycleAdmission || mode.preActivation || mode.provisionalActivationObservation {
			return nil, errors.New("mainnet production authority is restricted to the producer loader")
		}
		cfg.Coordinator = strings.ToLower(cfg.Coordinator)
		cfg.SettlementVault = strings.ToLower(cfg.SettlementVault)
		cfg.stagingConfigDirectory = mode.stagingConfigDirectory
		if err := loadOwnerRecycleProductionConfig(&cfg); err != nil {
			return nil, fmt.Errorf("validator production config %s: %w", abs, err)
		}
		if err := loadReleaseProductionRuntimeHistory(&cfg); err != nil {
			return nil, fmt.Errorf("validator production runtime history %s: %w", abs, err)
		}
		if err := loadReleaseProductionAuthorityHistory(&cfg); err != nil {
			return nil, fmt.Errorf("validator production authority history %s: %w", abs, err)
		}
		if mode.productionPreActivation {
			if err := cfg.validateProductionPreActivation(); err != nil {
				return nil, fmt.Errorf("validator production pre-activation config %s: %w", abs, err)
			}
		} else if err := cfg.Validate(); errors.Is(err, ErrReleaseEvidenceV2ActivationPending) {
			// The signed approval binds these unrendered references; the
			// in-place completion that legacy configs allow cannot apply.
			return nil, fmt.Errorf("validator production config %s: %w", abs, ErrReleaseProductionActivationPending)
		} else if err != nil {
			return nil, fmt.Errorf("validator production config %s: %w", abs, err)
		}
	} else if mode.productionPreActivation {
		return nil, fmt.Errorf("validator config %s: production pre-activation requires a signed schema 3 config", abs)
	} else if mode.mainnetRuntimeObservation {
		if err := loadReleaseMainnetRuntimeHistory(&cfg); err != nil {
			return nil, fmt.Errorf("validator runtime observation config %s: %w", abs, err)
		}
		if err := cfg.validateWithMode(false, false, false, true); err != nil {
			return nil, fmt.Errorf("validator runtime observation config %s: %w", abs, err)
		}
	} else if mode.ownerRecycleAdmission {
		if err := validateOwnerRecycleApprovalScope(&cfg); err != nil {
			return nil, fmt.Errorf("validator admission config %s: %w", abs, err)
		}
		if productionEconomicSelection(&cfg).Approval != (ReleaseEvidenceV2File{}) {
			if err := productionEconomicSelection(&cfg).Approval.Validate(maximumOwnerRecycleApprovalBytes); err != nil {
				return nil, fmt.Errorf("validator admission config %s: %w", abs, err)
			}
		}
	} else if provisionalActivationObservation {
		if err := cfg.validateProvisionalActivationObservation(); err != nil {
			return nil, fmt.Errorf("validator config %s: %w", abs, err)
		}
	} else if mode.preActivation {
		if err := cfg.validateWithMode(false, false, true, false); err != nil {
			return nil, fmt.Errorf("validator config %s: %w", abs, err)
		}
	} else if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validator config %s: %w", abs, err)
	}
	// Config accepts checksum-case addresses, while signed measurement identities
	// require lowercase. Keep every producer and its expected decision consistent
	// without rewriting the configured file or retained signed inputs.
	cfg.Coordinator = strings.ToLower(cfg.Coordinator)
	cfg.SettlementVault = strings.ToLower(cfg.SettlementVault)
	if mode.mainnetRuntimeObservation {
		if err := sealReleaseMainnetRuntimeHistory(&cfg); err != nil {
			return nil, err
		}
	}
	return &cfg, nil
}

func configPath(base, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New("path is empty")
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(base, value)
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func (c *ReleaseConfig) normalize(base string) error {
	// New evidence authority cannot acquire legitimacy through legacy cleaning.
	if c.EvidenceV2.Schema != "" {
		for _, path := range []string{c.StateDir, c.HotkeySeedFile} {
			if err := ValidateReleaseEvidenceV2Path(path); err != nil {
				return err
			}
		}
		for _, operator := range c.Operators {
			if err := ValidateReleaseEvidenceV2Path(operator.StateDir); err != nil {
				return err
			}
			for _, path := range []string{operator.NetworkJWTFile, operator.ClientJWTFile, operator.ClientKeySeedFile} {
				if path != "" {
					if err := ValidateReleaseEvidenceV2Path(path); err != nil {
						return err
					}
				}
			}
		}
	}
	var err error
	if c.StateDir, err = configPath(base, c.StateDir); err != nil {
		return fmt.Errorf("state_dir: %w", err)
	}
	if c.HotkeySeedFile, err = configPath(base, c.HotkeySeedFile); err != nil {
		return fmt.Errorf("hotkey_seed_file: %w", err)
	}
	for i := range c.Operators {
		op := &c.Operators[i]
		if op.RequestPreparation != nil {
			if op.RequestPreparation.Path, err = configPath(base, op.RequestPreparation.Path); err != nil {
				return fmt.Errorf("operators[%d].request_preparation: %w", i, err)
			}
		}
		if op.StateDir, err = configPath(base, op.StateDir); err != nil {
			return fmt.Errorf("operators[%d].state_dir: %w", i, err)
		}
		if op.NetworkJWTFile == "" {
			op.NetworkJWTFile = filepath.Join(op.StateDir, "network.jwt")
		}
		if op.ClientJWTFile == "" {
			op.ClientJWTFile = filepath.Join(op.StateDir, "client.jwt")
		}
		if op.ClientKeySeedFile == "" {
			op.ClientKeySeedFile = filepath.Join(op.StateDir, "client.key")
		}
		if op.NetworkJWTFile, err = configPath(base, op.NetworkJWTFile); err != nil {
			return fmt.Errorf("operators[%d].network_jwt_file: %w", i, err)
		}
		if op.ClientJWTFile, err = configPath(base, op.ClientJWTFile); err != nil {
			return fmt.Errorf("operators[%d].client_jwt_file: %w", i, err)
		}
		if op.ClientKeySeedFile, err = configPath(base, op.ClientKeySeedFile); err != nil {
			return fmt.Errorf("operators[%d].client_key_seed_file: %w", i, err)
		}
		if op.Concurrency == 0 {
			op.Concurrency = 4
		}
	}
	if c.TrailDepth == 0 {
		c.TrailDepth = c.Policy.Verify.TrailDepth
	}
	if c.PollSeconds == 0 {
		c.PollSeconds = 3
	}
	return nil
}

func validateEndpoint(name, raw string, schemes ...string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("%s %q is not an absolute endpoint", name, raw)
	}
	for _, scheme := range schemes {
		if strings.EqualFold(u.Scheme, scheme) {
			return nil
		}
	}
	return fmt.Errorf("%s %q has unsupported scheme", name, raw)
}

func parseHash32(name, value string) ([32]byte, error) {
	var out [32]byte
	b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(value), "0x"))
	if err != nil || len(b) != 32 {
		return out, fmt.Errorf("%s must be a 32-byte hex value", name)
	}
	copy(out[:], b)
	if out == ([32]byte{}) {
		return out, fmt.Errorf("%s is zero", name)
	}
	return out, nil
}

func (c ReleaseConfig) Validate() error {
	return c.validate(false)
}

// ValidateHistorical admits an original config only after an archive caller
// authenticates its captured bytes. Fresh configuration uses Validate.
func (self ReleaseConfig) ValidateHistorical() error {
	return self.validate(true)
}

// A retained provisional activation config is validated as an exact reviewed
// predecessor. Its compatibility profile is permitted only for this read-only
// observer, whose caller has already verified the immutable handoff.
func (c ReleaseConfig) validateProvisionalActivationObservation() error {
	return c.validateWithMode(true, true, false, false)
}

// Public archive replay authenticates an original configuration without
// authorizing it as a current producer or changing its serialized identity.
func (c ReleaseConfig) validate(historical bool) error {
	return c.validateWithMode(historical, false, false, false)
}

// preActivation admits unrendered evidence_v2 operator entries only; it grants
// no runtime, history or producer authority.
func (c ReleaseConfig) validateWithMode(historical, provisionalActivationObservation, preActivation, mainnetRuntimeObservation bool) error {
	production := c.SchemaVersion == ReleaseMainnetProductionSchemaVersion
	if c.ProductionCapacityRevision != nil && (!production || len(c.ProductionAuthorityHistory) == 0) {
		return errors.New("capacity revision requires original independently approved production authority")
	}
	if production {
		// Pre-activation is admitted only for a complete activation-pending
		// census; the evidence check below then accepts those entries alone.
		if provisionalActivationObservation || mainnetRuntimeObservation || preActivation && requireReleaseEvidenceV2ProductionPending(c.EvidenceV2.Operators) != nil {
			return errors.New("mainnet production authority cannot authorize another config load purpose")
		}
		if err := validateOwnerRecycleProductionConfig(&c); err != nil {
			return err
		}
		if err := validateReleaseProductionAuthorityHistory(&c); err != nil {
			return err
		}
	} else if isOwnerRecycleProductionConfig(&c) {
		return errors.New("production runtime authority requires an authenticated schema 3 config")
	}
	if mainnetRuntimeObservation {
		if err := validateReleaseMainnetRuntimeHistoryScope(&c); err != nil {
			return err
		}
	} else if err := rejectMainnetRuntimeObservationWrites(&c); err != nil {
		return err
	}
	if (c.SchemaVersion != ReleaseValidatorSchemaVersion && !mainnetRuntimeObservation && !production) || c.Release != "1.0" {
		return errors.New("schema_version must be 1 or authenticated production schema 3 and release must be 1.0")
	}
	if !c.Production {
		return errors.New("release config must explicitly set production: true")
	}
	if productionEconomicSelection(&c) != nil {
		if err := validateOwnerRecycleApprovalSelection(&c); err != nil {
			return err
		}
	}
	if c.ProvisionalDeferClosedNativeInput && !provisionalClosedNativeInputEnabled(&c) {
		return errors.New("provisional closed native input deferral requires chain 945 and testnet policy")
	}
	if err := validateReleaseProvisionalRuntimeCompatibility(&c); err != nil {
		return err
	}
	if err := validateReleaseRuntimeSuccessorProfile(&c); err != nil {
		return err
	}
	if historical && c.ProvisionalRuntimeCompatibility != "" && !provisionalActivationObservation {
		return errors.New("provisional runtime compatibility cannot authorize a final historical archive")
	}
	if strings.TrimSpace(c.DeploymentID) == "" || strings.ContainsAny(c.DeploymentID, "/\\.") {
		return errors.New("deployment_id must be one nonempty safe segment")
	}
	if c.ValidatorID == 0 || c.ChainID == 0 || c.RuntimeSpec == 0 || c.TransactionVersion == 0 || c.StateVersion == 0 || c.Netuid == 0 || c.DeployBlock == 0 {
		return errors.New("validator, chain, runtime, netuid and deploy block must be nonzero")
	}
	if !common.IsHexAddress(c.Coordinator) || common.HexToAddress(c.Coordinator) == (common.Address{}) {
		return errors.New("coordinator is missing or zero")
	}
	if !common.IsHexAddress(c.SettlementVault) || common.HexToAddress(c.SettlementVault) == (common.Address{}) {
		return errors.New("settlement_vault is missing or zero")
	}
	genesis, err := parseHash32("genesis_hash", c.GenesisHash)
	if err != nil {
		return err
	}
	_ = genesis
	if _, err := parseHash32("runtime_code_hash", c.RuntimeCodeHash); err != nil {
		return err
	}
	if _, err := parseHash32("runtime_metadata_hash", c.RuntimeMetadataHash); err != nil {
		return err
	}
	validateRuntime := validateReleaseNativeRuntimeConfig
	if historical {
		validateRuntime = validateReleaseHistoricalNativeRuntimeConfig
	}
	if err := validateRuntime(&c); err != nil {
		if historical || c.ProvisionalRuntimeCompatibility == "" || validateReleaseProvisionalRuntimeCompatibility(&c) != nil {
			return err
		}
	}
	configuredPolicyHash, err := parseHash32("policy_hash", c.PolicyHash)
	if err != nil {
		return err
	}
	if err := c.Policy.Validate(); err != nil {
		return fmt.Errorf("policy: %w", err)
	}
	policyHash, err := c.Policy.Hash()
	if err != nil {
		return err
	}
	if policyHash != configuredPolicyHash {
		return fmt.Errorf("policy hash mismatch: config has 0x%x, embedded policy hashes to 0x%x", configuredPolicyHash, policyHash)
	}
	if c.PreviousPolicy != nil {
		if err := protocol.ValidateTestnetRateAmendment(c.PreviousPolicy, &c.Policy); err != nil {
			return fmt.Errorf("previous policy: %w", err)
		}
	}
	if len(c.RPC) == 0 || len(c.Substrate) == 0 {
		return errors.New("at least one EVM and Substrate endpoint is required")
	}
	seenEndpoint := map[string]bool{}
	for i, endpoint := range c.RPC {
		if err := validateEndpoint(fmt.Sprintf("rpc[%d]", i), endpoint, "http", "https"); err != nil {
			return err
		}
		if seenEndpoint[endpoint] {
			return fmt.Errorf("duplicate endpoint %q", endpoint)
		}
		seenEndpoint[endpoint] = true
	}
	for i, endpoint := range c.Substrate {
		if err := validateEndpoint(fmt.Sprintf("substrate[%d]", i), endpoint, "ws", "wss"); err != nil {
			return err
		}
		if seenEndpoint[endpoint] {
			return fmt.Errorf("duplicate endpoint %q", endpoint)
		}
		seenEndpoint[endpoint] = true
	}
	if !filepath.IsAbs(c.StateDir) || !filepath.IsAbs(c.HotkeySeedFile) {
		return errors.New("state and hotkey paths must be absolute after normalization")
	}
	if c.TrailDepth != c.Policy.Verify.TrailDepth || c.TrailDepth < 2 || c.PollSeconds < 1 || c.PollSeconds > 60 {
		return errors.New("trail depth must equal policy and poll_seconds must be in [1,60]")
	}
	if len(c.Operators) < c.Policy.Safety.MinimumHealthyNOCount {
		return fmt.Errorf("configured operators %d below policy minimum %d", len(c.Operators), c.Policy.Safety.MinimumHealthyNOCount)
	}
	seenNO := map[uint64]bool{}
	seenArtifactSigner := map[common.Address]uint64{}
	seenPath := map[string]string{}
	for i, op := range c.Operators {
		if op.RequestPreparation != nil {
			if err := op.RequestPreparation.Validate(4096); err != nil {
				return fmt.Errorf("operators[%d].request_preparation: %w", i, err)
			}
		}
		if op.NoID == 0 || seenNO[op.NoID] {
			return fmt.Errorf("operators[%d] has zero or duplicate no_id", i)
		}
		seenNO[op.NoID] = true
		if op.AllowClientRegistration && c.SchemaVersion != ReleaseMainnetProductionSchemaVersion {
			return fmt.Errorf("operators[%d].allow_client_registration requires independently approved production schema3", i)
		}
		if err := validateEndpoint(fmt.Sprintf("operators[%d].api_url", i), op.APIURL, "http", "https"); err != nil {
			return err
		}
		if err := validateEndpoint(fmt.Sprintf("operators[%d].connect_url", i), op.ConnectURL, "ws", "wss"); err != nil {
			return err
		}
		if !common.IsHexAddress(op.ArtifactSigner) || common.HexToAddress(op.ArtifactSigner) == (common.Address{}) {
			return fmt.Errorf("operators[%d].artifact_signer is missing or zero", i)
		}
		artifactSigner := common.HexToAddress(op.ArtifactSigner)
		if priorNO, exists := seenArtifactSigner[artifactSigner]; exists {
			return fmt.Errorf("operators[%d].artifact_signer aliases no_id %d", i, priorNO)
		}
		seenArtifactSigner[artifactSigner] = op.NoID
		if op.Concurrency < 1 || op.Concurrency > 128 {
			return fmt.Errorf("operators[%d].concurrency outside [1,128]", i)
		}
		if op.Concurrency > c.Policy.Verify.HardActiveTrailsPerSource {
			return fmt.Errorf("operators[%d].concurrency exceeds the verify active-trail hard limit", i)
		}
		seedInterval, err := releaseSeedAttemptInterval(c.Policy.Verify.HardSeedPerMinutePerSource)
		if err != nil {
			return fmt.Errorf("operators[%d] seed pacing: %w", i, err)
		}
		maximumInitialWait := time.Duration(op.Concurrency-1) * seedInterval
		if maximumInitialWait >= time.Duration(c.Policy.Verify.StepTimeoutSeconds)*time.Second {
			return fmt.Errorf("operators[%d].concurrency cannot enter the seed gate within step_timeout", i)
		}
		for label, p := range map[string]string{"state_dir": op.StateDir, "network_jwt_file": op.NetworkJWTFile, "client_jwt_file": op.ClientJWTFile, "client_key_seed_file": op.ClientKeySeedFile} {
			if !filepath.IsAbs(p) {
				return fmt.Errorf("operators[%d].%s is not absolute", i, label)
			}
			if prior, exists := seenPath[p]; exists {
				return fmt.Errorf("operator path %s aliases %s", p, prior)
			}
			seenPath[p] = fmt.Sprintf("no-%d/%s", op.NoID, label)
		}
		if op.StateDir == c.StateDir || strings.HasPrefix(c.StateDir+string(filepath.Separator), op.StateDir+string(filepath.Separator)) {
			return fmt.Errorf("operator %d state aliases validator state", op.NoID)
		}
	}
	controlled := append([]uint64(nil), c.ControlledNOIDs...)
	sort.Slice(controlled, func(i, j int) bool { return controlled[i] < controlled[j] })
	for i, id := range controlled {
		if id == 0 || (i > 0 && id == controlled[i-1]) {
			return errors.New("controlled_no_ids contains zero or a duplicate")
		}
		if !seenNO[id] {
			return fmt.Errorf("controlled no_id %d is not in the operator directory", id)
		}
	}
	// Controlled pools and their fleets are masked; masking every operator
	// leaves no provider weight in any epoch: a treasury validator could only
	// submit its reserve-only row and any other validator no row at all.
	if len(controlled) > 0 && len(controlled) == len(seenNO) {
		return errors.New("controlled_no_ids covers every configured operator, leaving no weight to submit")
	}
	if c.SourceRolePredecessorV2 != nil {
		if err := c.SourceRolePredecessorV2.Validate(ReleaseSourceRolePredecessorV2MaximumBytes); err != nil {
			return fmt.Errorf("source role predecessor: %w", err)
		}
	}
	if preActivation {
		return c.EvidenceV2.ValidatePreActivation(c.Operators, c.StateDir, c.HotkeySeedFile)
	}
	return c.EvidenceV2.Validate(c.Operators, c.StateDir, c.HotkeySeedFile)
}
