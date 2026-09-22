package main

// A continuation may move its operational transport to an explicitly
// approved owned node without rewriting the configuration which authenticated
// existing activation consents. The plan binds the selected route separately;
// new observations use only that node and do not assert backend independence.
import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

func validateOwnedRPCAuthority(authority string) error {
	host, port, err := net.SplitHostPort(authority)
	ip := net.ParseIP(host)
	n, portErr := strconv.Atoi(port)
	if err != nil || ip == nil || ip.To4() == nil || !ip.IsPrivate() || host != ip.String() || portErr != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
		return errors.New("owned RPC authority must be an explicit private IPv4 HOST:PORT")
	}
	return nil
}

func validateOwnedRPCOptions(command string, options cliOptions) error {
	if options.OwnedRPCAuthority == "" {
		return nil
	}
	if options.ProvisionalRPCAuthority != "" || options.Manifest != "" {
		return errors.New("--owned-rpc-authority cannot combine another route or public-manifest override")
	}
	if options.ProvisionalResume {
		if command != "doctor" && command != "setup" && command != "resume" && command != "scenario" {
			return errors.New("provisional owned RPC is restricted to approved doctor, setup, resume or scenario")
		}
		if err := validateProvisionalResumeOptions(command, options); err != nil {
			return err
		}
	}
	switch command {
	case "audit", "doctor", "plan", "setup", "launch", "resume", "fleet-renew", "history-adoption", "relay-continuation", "scenario", "status", "inspect", "analyze":
	default:
		return fmt.Errorf("--owned-rpc-authority is not supported by %s", command)
	}
	return validateOwnedRPCAuthority(options.OwnedRPCAuthority)
}

func ownedRPCCommandOption(cfg *ResolvedConfig) string {
	if cfg == nil || cfg.ownedRPCAuthority == "" {
		return ""
	}
	return " --owned-rpc-authority " + cfg.ownedRPCAuthority
}

// The canonical harness, public manifest, policy, vault authority and hashes
// stay untouched. Only this invocation's approved operational route changes.
func prepareOwnedRPCConfiguration(cfg *ResolvedConfig, authority string) (*ResolvedConfig, error) {
	if authority == "" {
		return cfg, nil
	}
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.ChainID != testnetChainID || !strings.EqualFold(cfg.Public.Chain.GenesisHash, testnetGenesis) || cfg.provisionalRPCAuthority != "" {
		return nil, errors.New("owned RPC routing requires an authenticated testnet configuration")
	}
	if err := validateOwnedRPCAuthority(authority); err != nil {
		return nil, err
	}
	if cfg.ownedRPCAuthority != "" {
		return nil, errors.New("owned RPC authority was already selected for this invocation")
	}
	if err := validateOperationalRPCRouting(cfg); err != nil {
		return nil, err
	}
	resolved := *cfg
	resolved.ownedRPCAuthority = authority
	resolved.OperationalRPCMode = rpcModeOwnedNode
	resolved.OperationalSubstrate = "ws://" + authority
	resolved.OperationalEVM = "http://" + authority
	if err := validateExecutionRPCConfiguration(&resolved); err != nil {
		return nil, err
	}
	return &resolved, nil
}

func validateOwnedRPCRouting(cfg *ResolvedConfig) error {
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.provisionalRPCAuthority != "" || cfg.ChainID != testnetChainID || !strings.EqualFold(cfg.Public.Chain.GenesisHash, testnetGenesis) {
		return errors.New("owned RPC routing lost its authenticated testnet authority")
	}
	if err := validateOwnedRPCAuthority(cfg.ownedRPCAuthority); err != nil {
		return err
	}
	if cfg.OperationalRPCMode != rpcModeOwnedNode || cfg.OperationalSubstrate != "ws://"+cfg.ownedRPCAuthority || cfg.OperationalEVM != "http://"+cfg.ownedRPCAuthority {
		return errors.New("owned operational RPC routing differs from its selected authority")
	}
	return nil
}

// Keep the canonical public configuration as historical provenance. Every
// current observation selects the approved owned endpoint instead.
func ownedRPCOnly(cfg *ResolvedConfig) bool {
	return cfg != nil && cfg.ownedRPCAuthority != "" && cfg.OperationalRPCMode == rpcModeOwnedNode
}

func verificationSubstrateEndpoint(cfg *ResolvedConfig) string {
	if ownedRPCOnly(cfg) {
		return "ws://" + cfg.ownedRPCAuthority
	}
	return cfg.Public.Chain.SubstratePublicReadEndpoint
}

func verificationEVMEndpoint(cfg *ResolvedConfig) string {
	if ownedRPCOnly(cfg) {
		return "http://" + cfg.ownedRPCAuthority
	}
	return cfg.Public.Chain.EVMPublicReadEndpoint
}

func effectiveAdversaryConfig(cfg *ResolvedConfig) AdversaryConfig {
	config := cfg.Config.Scenarios.Adversaries
	if ownedRPCOnly(cfg) {
		config.MaximumRPCRequestsPerSec = 0
	}
	return config
}

func validateOwnedRPCObservationEndpoints(substrate, evm string) error {
	authority := strings.TrimPrefix(substrate, "ws://")
	if substrate != "ws://"+authority || evm != "http://"+authority {
		return errors.New("owned-node observations must use one explicit LAN authority over WS/HTTP")
	}
	return validateOwnedRPCAuthority(authority)
}

func validateOwnedRPCDialEndpoint(cfg *ResolvedConfig, endpoint string) error {
	if !ownedRPCOnly(cfg) {
		return nil
	}
	if endpoint == "ws://"+cfg.ownedRPCAuthority || endpoint == "http://"+cfg.ownedRPCAuthority {
		return nil
	}
	if endpoint == "http://"+campaignEVMAuthority() && endpoint == cfg.OperationalEVM {
		return nil
	}
	return errors.New("owned-node RPC cannot dial an endpoint outside its approved LAN route")
}

func validateOwnedRPCPlan(cfg *ResolvedConfig, plan *SetupPlan) error {
	if cfg == nil || plan == nil {
		return errors.New("owned RPC plan context is incomplete")
	}
	if cfg.ownedRPCAuthority == "" && plan.OwnedRPCAuthority == "" {
		return nil
	}
	if cfg.ownedRPCAuthority != plan.OwnedRPCAuthority {
		return errors.New("owned RPC authority differs from the exact approved plan; retain the approved invocation route or review a plan revision")
	}
	if err := validateOwnedRPCRouting(cfg); err != nil {
		return err
	}
	resolvedHash, err := resolvedInputsHash(cfg)
	if err != nil || resolvedHash != plan.ResolvedInputsHash || cfg.ConfigHash != plan.ConfigHash || cfg.PolicyHash != plan.PolicyHash {
		return errors.Join(errors.New("owned RPC endpoint inputs differ from the approved plan"), err)
	}
	return nil
}
