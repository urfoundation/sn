package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	serverapi "github.com/urnetwork/server/api"
	serverconnect "github.com/urnetwork/server/connect"
	servertaskworker "github.com/urnetwork/server/taskworker"

	minercomponent "github.com/urfoundation/sn/miner"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

var version = "1.0"
var defaultConfigPath = "sim-testnet/testnet.yml"

type cliOptions struct {
	Config, SNRepo, ServerRepo, OperatorProxyRepo, VaultRepo, PlatformConfigRepo, StateDir, PlanHash, Name, Manifest, Format string
	Apply, Detach                                                                                                            bool
}

func usage() {
	fmt.Fprint(os.Stderr, `sim-testnet release 1.0

Usage: sim-testnet <command> [options]

Commands:
  doctor   read-only configuration, repository, tool, RPC, wallet, and subnet checks
  release-lock  render or atomically refresh observed release-lock fields from clean repositories
  plan     print the canonical setup diff, costs, actions, and plan hash; never writes
  setup    converge the existing subnet and install contracts (dry-run unless approved)
  launch   setup, start topology, readiness, and smoke scenario (dry-run unless approved)
  resume   reconcile the journal and continue an interrupted approved action
  status   show process and finalized on-chain state
  inspect  emit the complete public live-state view
  analyze  reconstruct weights, roots, claims, reserve, and conservation evidence
  scenario run a named scenario (precompile-conformance, smoke, epoch, release-1.0, production-soak, release-candidate, or fault scenario)
  tail     multiplex structured process logs
  stop     stop local processes only; preserves keys, evidence, and chain state
  retire   plan future-effective operator retirement; dry-run by default

Common options:
  --config PATH       harness config (default sim-testnet/testnet.yml)
  --state-dir PATH    persistent state root (default <config-dir>/runs)
  --sn-repo PATH      repository discovery override
  --server-repo PATH  repository discovery override
  --operator-proxy-repo PATH  repository discovery override
  --vault-repo PATH   repository discovery override
  --platform-config-repo PATH  platform config repository override
  --format human|json
  --apply --plan-hash HASH  mandatory pair for chain/process writes; release-lock uses --apply alone
  --detach            persistent supervisor mode for launch
  --name NAME         scenario name
  --manifest PATH     public manifest for secretless inspect/analyze
`)
}

func parseCLI(args []string) (string, cliOptions, error) {
	if len(args) == 0 {
		return "", cliOptions{}, errors.New("missing command")
	}
	cmd := args[0]
	valid := map[string]bool{"doctor": true, "release-lock": true, "plan": true, "setup": true, "launch": true, "resume": true, "status": true, "inspect": true, "analyze": true, "scenario": true, "tail": true, "stop": true, "retire": true}
	if !valid[cmd] {
		return "", cliOptions{}, fmt.Errorf("unknown command %q", cmd)
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var o cliOptions
	fs.StringVar(&o.Config, "config", defaultConfigPath, "")
	fs.StringVar(&o.StateDir, "state-dir", "", "")
	fs.StringVar(&o.SNRepo, "sn-repo", "", "")
	fs.StringVar(&o.ServerRepo, "server-repo", "", "")
	fs.StringVar(&o.OperatorProxyRepo, "operator-proxy-repo", "", "")
	fs.StringVar(&o.VaultRepo, "vault-repo", "", "")
	fs.StringVar(&o.PlatformConfigRepo, "platform-config-repo", "", "")
	fs.StringVar(&o.PlanHash, "plan-hash", "", "")
	fs.StringVar(&o.Format, "format", "human", "")
	fs.StringVar(&o.Name, "name", "", "")
	fs.StringVar(&o.Manifest, "manifest", "", "")
	fs.BoolVar(&o.Apply, "apply", false, "")
	fs.BoolVar(&o.Detach, "detach", false, "")
	if err := fs.Parse(args[1:]); err != nil {
		return "", o, err
	}
	if fs.NArg() != 0 {
		return "", o, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if o.Format != "human" && o.Format != "json" {
		return "", o, errors.New("--format must be human or json")
	}
	return cmd, o, nil
}

func main() {
	defaultConfigPath = configPathForExecutable(os.Args[0])
	if err := runMain(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "sim-testnet: %v\n", err)
		os.Exit(1)
	}
}

// The light smoke-test binary is built from the exact release harness. Its
// executable name selects only the isolated lightnode transport profile; all
// release topology, source-lock, wallet, and scenario checks remain shared.
func configPathForExecutable(executable string) string {
	name := strings.TrimSuffix(filepath.Base(strings.ReplaceAll(executable, `\\`, "/")), ".exe")
	if name == "sim-testnet-light" {
		return "sim-testnet/testnet-light.yml"
	}
	return "sim-testnet/testnet.yml"
}

func runMain(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "help", "-h", "--help":
			usage()
			return nil
		case "version", "-version", "--version":
			fmt.Fprintln(os.Stdout, version)
			return nil
		}
	}
	if len(args) > 0 && args[0] == "__supervise" {
		fs := flag.NewFlagSet("__supervise", flag.ContinueOnError)
		var configPath, stateDir, manifest string
		fs.StringVar(&configPath, "config", "", "")
		fs.StringVar(&stateDir, "state-dir", "", "")
		fs.StringVar(&manifest, "manifest", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if configPath == "" || stateDir == "" || manifest == "" || fs.NArg() != 0 {
			return errors.New("invalid internal supervisor invocation")
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return supervise(ctx, stateDir, manifest)
	}
	if len(args) > 0 && args[0] == "__listener_probe" {
		fs := flag.NewFlagSet("__listener_probe", flag.ContinueOnError)
		var network string
		var addresses []string
		fs.StringVar(&network, "network", "", "")
		fs.Func("address", "", func(address string) error {
			addresses = append(addresses, address)
			return nil
		})
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if network != "udp" || len(addresses) == 0 || fs.NArg() != 0 {
			return errors.New("invalid internal listener probe invocation")
		}
		return validateAvailableNetworkListenAddresses(network, addresses)
	}
	if len(args) > 0 && args[0] == "__rpc_proxy" {
		fs := flag.NewFlagSet("__rpc_proxy", flag.ContinueOnError)
		var config rpcProxyConfig
		fs.StringVar(&config.ListenAddress, "listen", "", "")
		fs.StringVar(&config.HealthAddress, "health", "", "")
		fs.StringVar(&config.Upstream, "upstream", "", "")
		fs.StringVar(&config.TLSServerName, "tls-server-name", "", "")
		fs.BoolVar(&config.HTTP, "http", false, "")
		fs.IntVar(&config.MaximumRequestsPerMinute, "maximum-requests-per-minute", 0, "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("invalid internal RPC proxy invocation")
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return runRPCProxy(ctx, config)
	}
	if len(args) > 0 && (args[0] == "__miner_swarm" || args[0] == "__claim_swarm" || args[0] == "__validator") {
		component := args[0]
		fs := flag.NewFlagSet(component, flag.ContinueOnError)
		var configPath string
		fs.StringVar(&configPath, "config", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if configPath == "" || fs.NArg() != 0 {
			return fmt.Errorf("invalid internal %s invocation", component)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if component == "__miner_swarm" {
			return minercomponent.RunProviderSwarm(ctx, configPath)
		}
		if component == "__claim_swarm" {
			return minercomponent.RunClaimSwarm(ctx, configPath)
		}
		return validatorcomponent.RunRelease(ctx, configPath)
	}
	if len(args) > 0 && (args[0] == "__server_api" || args[0] == "__server_connect" || args[0] == "__server_taskworker") {
		component := args[0]
		fs := flag.NewFlagSet(component, flag.ContinueOnError)
		var port, count, batchSize int
		var tlsDefaultHost string
		var directH3Loopback bool
		fs.IntVar(&port, "port", 0, "")
		fs.IntVar(&count, "count", 8, "")
		fs.IntVar(&batchSize, "batch_size", 4, "")
		fs.StringVar(&tlsDefaultHost, "tls-default-host", "", "")
		fs.BoolVar(&directH3Loopback, "direct-h3-loopback", false, "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if port == 0 || fs.NArg() != 0 {
			return fmt.Errorf("invalid internal %s invocation", component)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if component == "__server_api" {
			if tlsDefaultHost != "" || directH3Loopback {
				return errors.New("API module cannot set Connect transport options")
			}
			return serverapi.Run(ctx, serverapi.RunOptions{Port: port})
		}
		if component == "__server_connect" {
			ip := net.ParseIP(tlsDefaultHost)
			if ip == nil || ip.To4() == nil || !ip.IsLoopback() {
				return errors.New("connect module requires an IPv4 loopback TLS default host")
			}
			if !directH3Loopback {
				return errors.New("simulator Connect module requires direct H3 loopback mode")
			}
			return serverconnect.Run(ctx, serverconnect.RunOptions{Port: port, TLSDefaultHostName: tlsDefaultHost, DirectH3LoopbackMode: true})
		}
		if tlsDefaultHost != "" || directH3Loopback {
			return errors.New("taskworker module cannot set Connect transport options")
		}
		return servertaskworker.Run(ctx, servertaskworker.RunOptions{Port: port, Count: count, BatchSize: batchSize})
	}
	if len(args) > 0 && args[0] == "__server_cleanup_contracts" {
		fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
		var cutoffUnixNano int64
		var resultPath string
		fs.Int64Var(&cutoffUnixNano, "cutoff-unix-nano", 0, "")
		fs.StringVar(&resultPath, "result", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if cutoffUnixNano <= 0 || !filepath.IsAbs(resultPath) || fs.NArg() != 0 {
			return errors.New("invalid internal server contract cleanup invocation")
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		result, err := closeStaleServerContracts(ctx, time.Unix(0, cutoffUnixNano).UTC())
		if err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		return atomicWrite(resultPath, append(encoded, '\n'), 0o600)
	}
	cmd, o, err := parseCLI(args)
	if err != nil {
		usage()
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	requireSecrets := cmd == "doctor" || cmd == "plan" || cmd == "setup" || cmd == "launch" || cmd == "resume" || cmd == "scenario" || cmd == "retire"
	resolved, err := LoadResolved(LoadOptions{ConfigPath: o.Config, SNRepo: o.SNRepo, ServerRepo: o.ServerRepo, OperatorProxyRepo: o.OperatorProxyRepo, VaultRepo: o.VaultRepo, PlatformConfigRepo: o.PlatformConfigRepo, RequireSecrets: requireSecrets})
	if err != nil {
		return err
	}
	if cmd == "release-lock" {
		return runReleaseLockCommand(resolved, o)
	}
	if (cmd == "inspect" || cmd == "analyze") && o.Manifest != "" {
		if err := adoptPublicManifest(ctx, resolved, o.Manifest); err != nil {
			return err
		}
	}
	stateDir, err := resolveStateDir(resolved, o.StateDir)
	if err != nil {
		return err
	}
	readResolved := resolved
	if commandUsesCampaignEgress(cmd) {
		active, err := supervisedCampaignEgressActive(ctx, stateDir)
		if err != nil {
			return err
		}
		readResolved, err = selectReadOnlyRPCConfig(resolved, active)
		if err != nil {
			return err
		}
	}
	switch cmd {
	case "doctor":
		report := RunDoctorForState(ctx, resolved, stateDir)
		return printResult(o.Format, report, report.Error())
	case "plan":
		p, err := BuildPlanForState(ctx, resolved, stateDir)
		if err != nil {
			return err
		}
		return printResult(o.Format, p, nil)
	case "setup", "launch", "resume", "scenario", "retire":
		return runMutation(ctx, cmd, resolved, stateDir, o)
	case "status":
		v, err := Status(ctx, readResolved, stateDir)
		return printResult(o.Format, v, err)
	case "inspect":
		v, err := Inspect(ctx, readResolved, stateDir, o.Manifest)
		return printResult(o.Format, v, err)
	case "analyze":
		v, err := Analyze(ctx, readResolved, stateDir, o.Manifest)
		return printResult(o.Format, v, err)
	case "tail":
		return Tail(ctx, stateDir, os.Stdout)
	case "stop":
		v, err := StopDeployment(ctx, stateDir)
		return printResult(o.Format, v, err)
	default:
		return errors.New("unreachable command")
	}
}

// Identifies the read-only commands which may run concurrently with a live
// campaign and therefore must share its single public-provider egress gate.
func commandUsesCampaignEgress(command string) bool {
	return command == "status" || command == "inspect" || command == "analyze"
}

// Selects the exact internally derived transport copy only while the
// supervised campaign egress is active. The canonical configuration remains
// the fallback before launch and after a deliberate stop.
func selectReadOnlyRPCConfig(cfg *ResolvedConfig, active bool) (*ResolvedConfig, error) {
	if !active {
		return cfg, nil
	}
	return campaignRPCConfig(cfg)
}

// Verifies that a live supervisor owns a healthy central egress listener. A
// live but incomplete topology fails closed instead of silently bypassing its
// provider quota; a stopped or never-launched topology uses canonical RPCs.
func supervisedCampaignEgressActive(ctx context.Context, stateDir string) (bool, error) {
	var state SupervisorState
	statePath := filepath.Join(stateDir, "supervisor.state.json")
	if err := readJSONFile(statePath, &state); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read campaign egress supervisor state: %w", err)
	}
	if state.Schema != "urnetwork-sim-supervisor-state-v1" {
		return false, errors.New("campaign egress supervisor state has an invalid schema")
	}
	if validateSupervisorGeneration(state) != nil {
		return false, nil
	}
	found := false
	for _, process := range state.Processes {
		if process.ID != publicEVMEgressProcessID {
			continue
		}
		if found {
			return false, errors.New("live supervisor contains duplicate campaign EVM egress processes")
		}
		found = true
		if process.Role != "dependency-rpc-proxy" || process.PID <= 1 || !process.Healthy || syscall.Kill(process.PID, syscall.Signal(0)) != nil {
			return false, errors.New("live supervisor campaign EVM egress is not healthy")
		}
	}
	if !found {
		return false, errors.New("live supervisor is missing the campaign EVM egress")
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", campaignEVMAuthority())
	if err != nil {
		return false, fmt.Errorf("connect supervised campaign EVM egress: %w", err)
	}
	_ = connection.Close()
	return true, nil
}

func printResult(format string, v any, resultErr error) error {
	if format == "json" {
		b, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			return e
		}
		fmt.Println(string(b))
	} else {
		fmt.Print(renderHuman(v))
	}
	return resultErr
}
