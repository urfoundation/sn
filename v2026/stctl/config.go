package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docopt/docopt-go"
	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"
)

// Config is the stctl YAML config schema (default ~/.urnetwork/stctl.yml).
type Config struct {
	// RpcUrls are EVM json-rpc endpoints, tried in order until one answers.
	RpcUrls []string `yaml:"rpc_urls"`
	// ChainId is asserted against eth_chainId of the endpoint that answers
	// (945 = subtensor testnet, 964 = mainnet).
	ChainId uint64 `yaml:"chain_id"`
	// ContractAddress is the STSubnet proxy address (0x-hex H160).
	ContractAddress string `yaml:"contract_address"`
	// Netuid is the subnet id; cross-checked against the contract's netuid().
	Netuid uint16 `yaml:"netuid"`
	// KeyFile is a path to a hex-encoded 32-byte EVM private key
	// (0x-optional, surrounding whitespace ignored). Only needed for writes.
	KeyFile string `yaml:"key_file"`
}

// exampleConfig is printed by `stctl deploy-status` when the config file is
// missing, and documented in stctl/README.md. Keep the two in sync.
const exampleConfig = `# stctl config (default path: ~/.urnetwork/stctl.yml)
#
# EVM json-rpc endpoints, tried in order until one answers.
# Testnet: https://test.chain.opentensor.ai (chain 945)
# Mainnet: https://lite.chain.opentensor.ai (chain 964)
rpc_urls:
  - https://test.chain.opentensor.ai

# Asserted against eth_chainId before any call.
chain_id: 945

# STSubnet proxy address (from the forge script deploy).
contract_address: "0x0000000000000000000000000000000000000000"

# Subnet netuid; cross-checked against the contract's netuid().
netuid: 1

# Path to a hex-encoded 32-byte EVM private key (0x-optional).
# Only required for state-changing commands. Fund the key's H160 through its
# ss58 mirror ("stctl evm-address <h160>", then btcli wallet transfer).
key_file: ~/.urnetwork/stctl.key
`

// defaultConfigPath returns ~/.urnetwork/stctl.yml (or the literal ~ form if
// the home directory cannot be resolved; expandHome retries at use time).
func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.urnetwork/stctl.yml"
	}
	return filepath.Join(home, ".urnetwork", "stctl.yml")
}

// expandHome expands a leading "~/" to the user's home directory.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
	}
	return path
}

// configPathFromOpts resolves --config (docopt supplies the default).
func configPathFromOpts(opts docopt.Opts) string {
	path, err := opts.String("--config")
	if err != nil || path == "" {
		path = defaultConfigPath()
	}
	return expandHome(path)
}

// parseConfig strictly decodes YAML config bytes (unknown fields rejected).
func parseConfig(data []byte) (*Config, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &config, nil
}

// loadConfig reads and validates the config file at path.
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf(
				"config file %s does not exist (run `stctl deploy-status` to print a commented example)",
				path,
			)
		}
		return nil, err
	}
	config, err := parseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return config, nil
}

func (c *Config) validate() error {
	if len(c.RpcUrls) == 0 {
		return fmt.Errorf("config: rpc_urls must list at least one endpoint")
	}
	if c.ChainId == 0 {
		return fmt.Errorf("config: chain_id must be set (945 testnet, 964 mainnet)")
	}
	if c.ContractAddress != "" && !common.IsHexAddress(c.ContractAddress) {
		return fmt.Errorf("config: contract_address %q is not a valid 0x-hex H160", c.ContractAddress)
	}
	return nil
}

// contractAddr returns the configured contract address, or an error when the
// config predates deployment (contract_address unset / zero).
func (c *Config) contractAddr() (common.Address, error) {
	if c.ContractAddress == "" {
		return common.Address{}, fmt.Errorf("config: contract_address is not set (deploy first: evm/script/Deploy.s.sol)")
	}
	addr := common.HexToAddress(c.ContractAddress)
	if addr == (common.Address{}) {
		return common.Address{}, fmt.Errorf("config: contract_address is the zero address (deploy first: evm/script/Deploy.s.sol)")
	}
	return addr, nil
}
