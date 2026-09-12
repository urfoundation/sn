package main

// A strict continuation may move its operational transport to an explicitly
// approved owned node without rewriting the configuration which authenticated
// existing activation consents. The plan binds the selected route separately;
// public comparison readers remain independent and retain their own policy.
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
	if options.ProvisionalResume || options.ProvisionalRPCAuthority != "" || options.Manifest != "" {
		return errors.New("--owned-rpc-authority requires strict configured operation, without provisional or public-manifest overrides")
	}
	switch command {
	case "doctor", "plan", "setup", "launch", "resume", "fleet-renew", "history-adoption", "relay-continuation", "scenario", "status", "inspect", "analyze":
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
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.ChainID != testnetChainID || !strings.EqualFold(cfg.Public.Chain.GenesisHash, testnetGenesis) || provisionalResumeEnabled(cfg) || cfg.provisionalRPCAuthority != "" {
		return nil, errors.New("owned RPC routing requires a strict authenticated testnet configuration")
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
	resolved.OperationalRPCMode = rpcModePrivateAuthority
	resolved.OperationalSubstrate = "ws://" + authority
	resolved.OperationalEVM = "http://" + authority
	if err := validateExecutionRPCConfiguration(&resolved); err != nil {
		return nil, err
	}
	return &resolved, nil
}

func validateOwnedRPCRouting(cfg *ResolvedConfig) error {
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || provisionalResumeEnabled(cfg) || cfg.provisionalRPCAuthority != "" || cfg.ChainID != testnetChainID || !strings.EqualFold(cfg.Public.Chain.GenesisHash, testnetGenesis) {
		return errors.New("owned RPC routing lost its strict testnet authority")
	}
	if err := validateOwnedRPCAuthority(cfg.ownedRPCAuthority); err != nil {
		return err
	}
	if cfg.OperationalRPCMode != rpcModePrivateAuthority || cfg.OperationalSubstrate != "ws://"+cfg.ownedRPCAuthority || cfg.OperationalEVM != "http://"+cfg.ownedRPCAuthority {
		return errors.New("owned operational RPC routing differs from its selected authority")
	}
	return nil
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
