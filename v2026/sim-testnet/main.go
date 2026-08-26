package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

var version = "1.0"

type cliOptions struct {
	Config, SNRepo, ServerRepo, VaultRepo, PlatformConfigRepo, StateDir, PlanHash, Name, Manifest, Format string
	Apply, Detach                                                                                         bool
}

func usage() {
	fmt.Fprint(os.Stderr, `sim-testnet release 1.0

Usage: sim-testnet <command> [options]

Commands:
  doctor   read-only configuration, repository, tool, RPC, wallet, and subnet checks
  plan     print the canonical setup diff, costs, actions, and plan hash; never writes
  setup    converge the existing subnet and install contracts (dry-run unless approved)
  launch   setup, start topology, readiness, and smoke scenario (dry-run unless approved)
  resume   reconcile the journal and continue an interrupted approved action
  status   show process and finalized on-chain state
  inspect  emit the complete public live-state view
  analyze  reconstruct weights, roots, claims, reserve, and conservation evidence
  scenario run a named scenario (precompile-conformance, smoke, epoch, release-1.0, production-soak, or fault scenario)
  tail     multiplex structured process logs
  stop     stop local processes only; preserves keys, evidence, and chain state
  retire   plan future-effective operator retirement; dry-run by default

Common options:
  --config PATH       harness config (default sim-testnet/testnet.yml)
  --state-dir PATH    persistent state root (default <config-dir>/runs)
  --sn-repo PATH      repository discovery override
  --server-repo PATH  repository discovery override
  --vault-repo PATH   repository discovery override
  --platform-config-repo PATH  platform config repository override
  --format human|json
  --apply --plan-hash HASH  mandatory pair for any write
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
	valid := map[string]bool{"doctor": true, "plan": true, "setup": true, "launch": true, "resume": true, "status": true, "inspect": true, "analyze": true, "scenario": true, "tail": true, "stop": true, "retire": true}
	if !valid[cmd] {
		return "", cliOptions{}, fmt.Errorf("unknown command %q", cmd)
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var o cliOptions
	fs.StringVar(&o.Config, "config", "sim-testnet/testnet.yml", "")
	fs.StringVar(&o.StateDir, "state-dir", "", "")
	fs.StringVar(&o.SNRepo, "sn-repo", "", "")
	fs.StringVar(&o.ServerRepo, "server-repo", "", "")
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
	if err := runMain(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "sim-testnet: %v\n", err)
		os.Exit(1)
	}
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
	if len(args) > 0 && args[0] == "__rpc_proxy" {
		fs := flag.NewFlagSet("__rpc_proxy", flag.ContinueOnError)
		var config rpcProxyConfig
		fs.StringVar(&config.ListenAddress, "listen", "", "")
		fs.StringVar(&config.HealthAddress, "health", "", "")
		fs.StringVar(&config.Upstream, "upstream", "", "")
		fs.StringVar(&config.TLSServerName, "tls-server-name", "", "")
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
	cmd, o, err := parseCLI(args)
	if err != nil {
		usage()
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	requireSecrets := cmd == "doctor" || cmd == "plan" || cmd == "setup" || cmd == "launch" || cmd == "resume" || cmd == "scenario" || cmd == "retire"
	resolved, err := LoadResolved(LoadOptions{ConfigPath: o.Config, SNRepo: o.SNRepo, ServerRepo: o.ServerRepo, VaultRepo: o.VaultRepo, PlatformConfigRepo: o.PlatformConfigRepo, RequireSecrets: requireSecrets})
	if err != nil {
		return err
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
	switch cmd {
	case "doctor":
		report := RunDoctor(ctx, resolved)
		return printResult(o.Format, report, report.Error())
	case "plan":
		p, err := BuildPlan(ctx, resolved)
		if err != nil {
			return err
		}
		return printResult(o.Format, p, nil)
	case "setup", "launch", "resume", "scenario", "retire":
		return runMutation(ctx, cmd, resolved, stateDir, o)
	case "status":
		v, err := Status(ctx, resolved, stateDir)
		return printResult(o.Format, v, err)
	case "inspect":
		v, err := Inspect(ctx, resolved, stateDir, o.Manifest)
		return printResult(o.Format, v, err)
	case "analyze":
		v, err := Analyze(ctx, resolved, stateDir, o.Manifest)
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
