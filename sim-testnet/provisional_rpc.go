package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func validateProvisionalRPCAuthority(authority string) error {
	host, port, err := net.SplitHostPort(authority)
	ip := net.ParseIP(host)
	n, portErr := strconv.Atoi(port)
	if err != nil || ip == nil || ip.To4() == nil || !ip.IsPrivate() || host != ip.String() || portErr != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
		return errors.New("provisional RPC authority must be an explicit private IPv4 HOST:PORT")
	}
	return nil
}

// Only the I/O copy changes. The original configuration, resolved hashes,
// retained runtime YAML, plan and receipts remain canonical and untouched.
func provisionalRPCTransportConfig(cfg *ResolvedConfig) (*ResolvedConfig, error) {
	if !provisionalResumeEnabled(cfg) || cfg.Config == nil || cfg.Public == nil || cfg.ChainID != testnetChainID || !strings.EqualFold(cfg.Public.Chain.GenesisHash, testnetGenesis) {
		return nil, errors.New("owned RPC routing requires authenticated provisional testnet continuation")
	}
	if err := validateProvisionalRPCAuthority(cfg.provisionalRPCAuthority); err != nil {
		return nil, err
	}
	resolved, harness, public := *cfg, *cfg.Config, *cfg.Public
	resolved.Config, resolved.Public = &harness, &public
	resolved.OperationalEVM = "http://" + campaignEVMAuthority()
	resolved.OperationalSubstrate = "ws://" + cfg.provisionalRPCAuthority
	resolved.Public.Chain.EVMPublicReadEndpoint = resolved.OperationalEVM
	resolved.Public.Chain.SubstratePublicReadEndpoint = resolved.OperationalSubstrate
	// Existing lower-assurance mode records no independent backend. The route
	// receipt explicitly identifies the owned LAN endpoint and this waiver.
	resolved.OperationalRPCMode = rpcModePublicOverride
	harness.LaunchInputs.PublicEVMMaximumRequestsPerMinute = 0
	return &resolved, nil
}

func prepareProvisionalRPCOverride(cfg *ResolvedConfig, stateDir, authority string) error {
	if authority == "" {
		return nil
	}
	if !provisionalResumeEnabled(cfg) {
		return errors.New("owned RPC routing requires the authenticated provisional plan")
	}
	if err := validateProvisionalRPCAuthority(authority); err != nil {
		return err
	}
	live, err := liveRecordedSupervisor(stateDir)
	if err != nil || live == nil {
		return errors.Join(err, errors.New("owned RPC routing requires its live retained supervisor"))
	}
	var manifest SupervisorFile
	if err := readJSONFile(filepath.Join(stateDir, "supervisor.json"), &manifest); err != nil {
		return err
	}
	hash, err := canonicalHashHex(manifest)
	if err != nil || hash != live.ManifestHash {
		return errors.Join(err, errors.New("owned RPC manifest differs from the live generation"))
	}
	want := map[string]string{publicEVMEgressProcessID: publicEVMEgressAddress, workloadSubstrateProcessID: workloadSubstrateProxyAddress}
	for _, spec := range manifest.Specs {
		listen, required := want[spec.ID]
		if !required {
			continue
		}
		if len(spec.Args) == 0 || spec.Args[0] != "__rpc_proxy" || spec.Role != "dependency-rpc-proxy" {
			return fmt.Errorf("owned RPC proxy %s has an unexpected command", spec.ID)
		}
		var proxy rpcProxyConfig
		fs := flag.NewFlagSet(spec.ID, flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		fs.StringVar(&proxy.ListenAddress, "listen", "", "")
		fs.StringVar(&proxy.HealthAddress, "health", "", "")
		fs.StringVar(&proxy.Upstream, "upstream", "", "")
		fs.StringVar(&proxy.TLSServerName, "tls-server-name", "", "")
		fs.BoolVar(&proxy.HTTP, "http", false, "")
		fs.IntVar(&proxy.MaximumRequestsPerMinute, "maximum-requests-per-minute", 0, "")
		if err := fs.Parse(spec.Args[1:]); err != nil || fs.NArg() != 0 || proxy.ListenAddress != listen || proxy.Upstream != authority || proxy.TLSServerName != "" || proxy.MaximumRequestsPerMinute != 0 || (spec.ID == publicEVMEgressProcessID && !proxy.HTTP) {
			return fmt.Errorf("owned RPC proxy %s must use exactly %s without TLS or a request ceiling", spec.ID, authority)
		}
		matched := false
		for _, process := range live.Processes {
			if process.ID != spec.ID {
				continue
			}
			if process.Role != spec.Role || process.Identity != spec.Identity || !process.Healthy || process.PID <= 1 || syscall.Kill(process.PID, syscall.Signal(0)) != nil {
				return fmt.Errorf("owned RPC proxy %s is not its healthy retained process", spec.ID)
			}
			raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", process.PID))
			argv := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
			if err != nil || len(argv) < 2 || strings.Join(argv[1:], "\x00") != strings.Join(spec.Args, "\x00") {
				return fmt.Errorf("owned RPC proxy %s live arguments differ from the retained manifest", spec.ID)
			}
			matched = true
		}
		if !matched {
			return fmt.Errorf("owned RPC proxy %s is absent from the live generation", spec.ID)
		}
		delete(want, spec.ID)
	}
	if len(want) != 0 {
		return errors.New("owned RPC route requires both EVM egress and native workload proxies")
	}
	cfg.provisionalRPCAuthority = authority
	runtimeCfg, err := provisionalRPCTransportConfig(cfg)
	if err != nil {
		return err
	}
	record := map[string]any{
		"schema": "urnetwork-sim-provisional-rpc-route-v1", "recorded_at": time.Now().UTC().Format(time.RFC3339Nano),
		"provisional": true, "final_acceptance": false, "independent_rpc_waived": true,
		"client_and_proxy_request_ceiling": 0, "owned_authority": authority,
		"operational_evm_rpc": runtimeCfg.OperationalEVM, "operational_substrate_rpc": runtimeCfg.OperationalSubstrate,
		"config_hash": cfg.ConfigHash, "plan_hash": cfg.provisionalResume.Record.PlanHash,
		"supervisor_manifest_hash": hash, "supervisor_pid": live.SupervisorPID,
		"supervisor_start_time_ticks": live.SupervisorStartTimeTicks,
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(cfg.provisionalResume.RecordPath), "rpc-route.json")
	if err := atomicWrite(path, append(raw, '\n'), 0600); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "sim-testnet: provisional owned RPC %s; request_ceiling=0; independent_rpc_waived=true; final_acceptance=false; route %s\n", authority, path)
	return nil
}
