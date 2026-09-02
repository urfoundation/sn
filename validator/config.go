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
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/protocol"
)

const ReleaseValidatorSchemaVersion = 1

type OperatorConfig struct {
	NoID              uint64 `yaml:"no_id" json:"no_id"`
	APIURL            string `yaml:"api_url" json:"api_url"`
	ConnectURL        string `yaml:"connect_url" json:"connect_url"`
	ArtifactSigner    string `yaml:"artifact_signer" json:"artifact_signer"`
	StateDir          string `yaml:"state_dir" json:"state_dir"`
	NetworkJWTFile    string `yaml:"network_jwt_file" json:"network_jwt_file"`
	ClientJWTFile     string `yaml:"client_jwt_file" json:"client_jwt_file"`
	ClientKeySeedFile string `yaml:"client_key_seed_file" json:"client_key_seed_file"`
	Concurrency       int    `yaml:"concurrency" json:"concurrency"`
}

type ReleaseConfig struct {
	SchemaVersion   int              `yaml:"schema_version" json:"schema_version"`
	Production      bool             `yaml:"production" json:"production"`
	Release         string           `yaml:"release" json:"release"`
	DeploymentID    string           `yaml:"deployment_id" json:"deployment_id"`
	ValidatorID     uint64           `yaml:"validator_id" json:"validator_id"`
	ChainID         uint64           `yaml:"chain_id" json:"chain_id"`
	GenesisHash     string           `yaml:"genesis_hash" json:"genesis_hash"`
	RuntimeSpec     uint32           `yaml:"runtime_spec" json:"runtime_spec"`
	Netuid          uint16           `yaml:"netuid" json:"netuid"`
	Coordinator     string           `yaml:"coordinator" json:"coordinator"`
	SettlementVault string           `yaml:"settlement_vault" json:"settlement_vault"`
	DeployBlock     uint64           `yaml:"deploy_block" json:"deploy_block"`
	PolicyHash      string           `yaml:"policy_hash" json:"policy_hash"`
	RPC             []string         `yaml:"rpc" json:"rpc"`
	Substrate       []string         `yaml:"substrate" json:"substrate"`
	StateDir        string           `yaml:"state_dir" json:"state_dir"`
	HotkeySeedFile  string           `yaml:"hotkey_seed_file" json:"hotkey_seed_file"`
	ControlledNOIDs []uint64         `yaml:"controlled_no_ids" json:"controlled_no_ids"`
	TrailDepth      int              `yaml:"trail_depth" json:"trail_depth"`
	PollSeconds     int              `yaml:"poll_seconds" json:"poll_seconds"`
	VersionKey      uint64           `yaml:"version_key" json:"version_key"`
	Policy          protocol.Policy  `yaml:"policy" json:"policy"`
	Operators       []OperatorConfig `yaml:"operators" json:"operators"`
}

func LoadReleaseConfig(path string) (*ReleaseConfig, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("validator config path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var cfg ReleaseConfig
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode validator config %s: %w", abs, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return nil, fmt.Errorf("decode validator config %s: multiple YAML documents", abs)
	}
	if err := cfg.normalize(filepath.Dir(abs)); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validator config %s: %w", abs, err)
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
	var err error
	if c.StateDir, err = configPath(base, c.StateDir); err != nil {
		return fmt.Errorf("state_dir: %w", err)
	}
	if c.HotkeySeedFile, err = configPath(base, c.HotkeySeedFile); err != nil {
		return fmt.Errorf("hotkey_seed_file: %w", err)
	}
	for i := range c.Operators {
		op := &c.Operators[i]
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
	if c.SchemaVersion != ReleaseValidatorSchemaVersion || c.Release != "1.0" {
		return errors.New("schema_version must be 1 and release must be 1.0")
	}
	if !c.Production {
		return errors.New("release config must explicitly set production: true")
	}
	if strings.TrimSpace(c.DeploymentID) == "" || strings.ContainsAny(c.DeploymentID, "/\\.") {
		return errors.New("deployment_id must be one nonempty safe segment")
	}
	if c.ValidatorID == 0 || c.ChainID == 0 || c.RuntimeSpec == 0 || c.Netuid == 0 || c.DeployBlock == 0 {
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
		if op.NoID == 0 || seenNO[op.NoID] {
			return fmt.Errorf("operators[%d] has zero or duplicate no_id", i)
		}
		seenNO[op.NoID] = true
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
	return nil
}
