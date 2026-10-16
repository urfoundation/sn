package commitmentoracle

// The commitment-oracle command line (the executable lives at
// cli/commitment-oracle). docopt, like the validator and miner commands.

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/ethereum/go-ethereum/common"
)

// Stamped by the build (cli/commitment-oracle main.Version).
var Version string

func requireVersion() string {
	if version := os.Getenv("WARP_VERSION"); version != "" {
		return version
	}
	if Version != "" {
		return Version
	}
	return "0.0.0-dev"
}

func usage() string {
	return `SN25 commitment oracle.

Mirrors finalized native fleet commitments (Commitments.CommitmentOf(netuid, *)
with exactly one Sha256 field) into STCoordinator.mirrorCommitment, signed by
the coordinator's active commitment oracle key. It never mirrors a hotkey that
holds a validator permit, is unregistered, or has no standalone fleet
set_commitment in its commitment block, and mirrors nothing while a role cannot
be read. It reads only finalized chain state and refuses to start unless the
key's address is the active oracle.

Usage:
    commitment-oracle run --netuid=<id> --chain_id=<id> --coordinator=<addr> --state_dir=<path>
        --substrate=<url>... --rpc=<url>... (--oracle_key_file=<path> | --oracle_address=<addr>)
        [--metrics_file=<path>] [--poll_interval=<duration>] [--max_backoff=<duration>]
        [--confirm_timeout=<duration>] [--max_gas_price_gwei=<n>] [--max_gas_limit=<n>]
        [--low_balance_wei=<n>] [--max_sends=<n>] [--once] [--dry-run]
    commitment-oracle -h | --help
    commitment-oracle --version

Options:
    -h --help                    Show this help and exit.
    --version                    Show version.
    --netuid=<id>                Subnet whose commitments are mirrored; the coordinator must serve it.
    --chain_id=<id>              EVM chain id the RPC must report (964 on mainnet).
    --coordinator=<addr>         STCoordinator proxy address.
    --state_dir=<path>           Absolute directory for the durable transaction records
                                 (<state_dir>/mirror-sends); created 0700 when missing.
    --substrate=<url>            Subtensor websocket endpoint, repeatable. The first is read; every
                                 other one must report the same commitment bytes and block hash
                                 before a mirror is sent.
    --rpc=<url>                  EVM JSON-RPC endpoint, repeatable; the first that answers is used.
    --oracle_key_file=<path>     Hex secp256k1 key of the commitment oracle, owner-only mode.
    --oracle_address=<addr>      With --dry-run only: the oracle address, for a dry run without the key.
    --metrics_file=<path>        Prometheus textfile written after every cycle, in an existing directory.
    --poll_interval=<duration>   Time between cycles [default: 30s].
    --max_backoff=<duration>     Longest wait after repeated failed cycles [default: 10m].
    --confirm_timeout=<duration>  How long a cycle waits for a sent mirror to become final before it
                                 resumes the wait in the next cycle [default: 3m].
    --max_gas_price_gwei=<n>     Refuse to send while the suggested gas price is higher [default: 20].
    --max_gas_limit=<n>          Refuse to send when the estimate plus 20% is higher [default: 250000].
    --low_balance_wei=<n>        oracle_balance_low is 1 below this balance [default: 10000000000000000].
    --max_sends=<n>              Mirrors sent (or logged in a dry run) per cycle [default: 10].
    --once                       Run one cycle and exit with its status.
    --dry-run                    Read, preflight and estimate every mirror and log the call it would
                                 send; never sign or broadcast.
`
}

// Entry point; takes os.Args[1:].
func Run(args []string) {
	opts, err := docopt.ParseArgs(usage(), args, requireVersion())
	if err != nil {
		panic(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := runCommand(ctx, opts, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "commitment-oracle: %v\n", err)
		stop()
		os.Exit(1)
	}
}

// The parsed command line, before any network access.
type runOptions struct {
	settings       Settings
	substrates     []string
	rpcs           []string
	oracleKeyFile  string
	oracleAddress  common.Address
	hasAddressOnly bool
}

func parseRunOptions(opts docopt.Opts) (runOptions, error) {
	text := func(name string) string {
		value, _ := opts.String(name)
		return value
	}
	list := func(name string) []string {
		values, _ := opts[name].([]string)
		return values
	}
	flag := func(name string) bool {
		value, _ := opts.Bool(name)
		return value
	}
	duration := func(name string) (time.Duration, error) {
		value, err := time.ParseDuration(text(name))
		if err != nil || value <= 0 {
			return 0, fmt.Errorf("%s must be a positive duration", name)
		}
		return value, nil
	}
	unsigned := func(name string, bits int) (uint64, error) {
		value, err := strconv.ParseUint(text(name), 10, bits)
		if err != nil || value == 0 {
			return 0, fmt.Errorf("%s must be a positive integer", name)
		}
		return value, nil
	}

	var options runOptions
	var errs []error
	netuid, err := unsigned("--netuid", 16)
	errs = append(errs, err)
	chainId, err := unsigned("--chain_id", 64)
	errs = append(errs, err)
	coordinator := text("--coordinator")
	if !common.IsHexAddress(coordinator) || common.HexToAddress(coordinator) == (common.Address{}) {
		errs = append(errs, errors.New("--coordinator must be a nonzero hex address"))
	}
	stateDir := text("--state_dir")
	if !filepath.IsAbs(stateDir) {
		errs = append(errs, errors.New("--state_dir must be an absolute path"))
	}
	options.substrates = list("--substrate")
	options.rpcs = list("--rpc")
	if len(options.substrates) == 0 || len(options.rpcs) == 0 {
		errs = append(errs, errors.New("at least one --substrate and one --rpc are required"))
	}
	dryRun := flag("--dry-run")
	options.oracleKeyFile = text("--oracle_key_file")
	if address := text("--oracle_address"); address != "" {
		if !dryRun {
			errs = append(errs, errors.New("--oracle_address is only for --dry-run; a live oracle needs --oracle_key_file"))
		}
		if !common.IsHexAddress(address) {
			errs = append(errs, errors.New("--oracle_address must be a hex address"))
		}
		options.oracleAddress = common.HexToAddress(address)
		options.hasAddressOnly = true
	}
	pollInterval, err := duration("--poll_interval")
	errs = append(errs, err)
	maxBackoff, err := duration("--max_backoff")
	errs = append(errs, err)
	confirmTimeout, err := duration("--confirm_timeout")
	errs = append(errs, err)
	maxGasPriceGwei, err := unsigned("--max_gas_price_gwei", 32)
	errs = append(errs, err)
	maxGasLimit, err := unsigned("--max_gas_limit", 64)
	errs = append(errs, err)
	maxSends, err := unsigned("--max_sends", 16)
	errs = append(errs, err)
	lowBalance, ok := new(big.Int).SetString(text("--low_balance_wei"), 10)
	if !ok || lowBalance.Sign() < 0 {
		errs = append(errs, errors.New("--low_balance_wei must be a non-negative integer"))
	}
	if err := errors.Join(errs...); err != nil {
		return runOptions{}, err
	}
	metricsFile := text("--metrics_file")
	if metricsFile != "" && !filepath.IsAbs(metricsFile) {
		return runOptions{}, errors.New("--metrics_file must be an absolute path")
	}
	options.settings = Settings{
		Netuid:           uint16(netuid),
		ChainId:          chainId,
		Coordinator:      common.HexToAddress(coordinator),
		StateDir:         stateDir,
		MetricsFile:      metricsFile,
		DryRun:           dryRun,
		Once:             flag("--once"),
		PollInterval:     pollInterval,
		MaxBackoff:       maxBackoff,
		ConfirmTimeout:   confirmTimeout,
		ConfirmPoll:      6 * time.Second,
		MaxGasPrice:      new(big.Int).Mul(new(big.Int).SetUint64(maxGasPriceGwei), big.NewInt(1000*1000*1000)),
		MaxGasLimit:      maxGasLimit,
		LowBalanceWei:    lowBalance,
		MaxSendsPerCycle: int(maxSends),
	}
	return options, nil
}

// Dials the first answering EVM RPC and every substrate endpoint.
func dialConnections(ctx context.Context, substrates []string, rpcs []string) (connections, error) {
	var result connections
	var dialErrs []error
	for _, endpoint := range rpcs {
		client, err := dialEthEvmClient(ctx, endpoint)
		if err == nil {
			if _, err = client.chainId(ctx); err == nil {
				result.evm = client
				break
			}
			client.close()
		}
		dialErrs = append(dialErrs, err)
	}
	if result.evm == nil {
		return connections{}, fmt.Errorf("no EVM RPC answered: %w", errors.Join(dialErrs...))
	}
	for index, endpoint := range substrates {
		reader, err := dialSubstrateReader(ctx, endpoint)
		if err != nil {
			result.close()
			return connections{}, err
		}
		if index == 0 {
			result.primary = reader
		} else {
			result.secondaries = append(result.secondaries, reader)
		}
	}
	return result, nil
}

func runCommand(ctx context.Context, opts docopt.Opts, logOutput io.Writer) error {
	options, err := parseRunOptions(opts)
	if err != nil {
		return err
	}
	logf := func(format string, args ...any) {
		fmt.Fprintf(logOutput, "%s commitment-oracle: %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
	}
	var key *ecdsa.PrivateKey
	address := options.oracleAddress
	if !options.hasAddressOnly {
		key, err = loadOracleKey(options.oracleKeyFile)
		if err != nil {
			return err
		}
		address = keyAddress(key)
	}
	directory := filepath.Join(options.settings.StateDir, "mirror-sends")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if info, err := os.Stat(directory); err != nil || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s must be a directory private to its owner", directory)
	}

	dial := func(ctx context.Context) (connections, error) {
		return dialConnections(ctx, options.substrates, options.rpcs)
	}
	initial, err := dial(ctx)
	if err != nil {
		return err
	}
	send := &sender{
		chainId:     new(big.Int).SetUint64(options.settings.ChainId),
		coordinator: options.settings.Coordinator,
		oracle:      address,
		key:         key,
		dryRun:      options.settings.DryRun,
		fees:        feeSettings{maxGasPrice: options.settings.MaxGasPrice, maxGasLimit: options.settings.MaxGasLimit},
		directory:   directory,
		now:         time.Now,
		logf:        logf,
	}
	oracle := newOracle(options.settings, address, initial, send, time.Now, logf)
	oracle.dial = dial
	// A redial replaces the connections, so close whichever are current.
	defer func() { oracle.connections.close() }()

	logf("starting %s: netuid %d, coordinator %s, chain %d, oracle %s, %d substrate endpoint(s), dry run %t",
		requireVersion(), options.settings.Netuid, options.settings.Coordinator.Hex(), options.settings.ChainId, address.Hex(), len(options.substrates), options.settings.DryRun)
	if err := oracle.admit(ctx); err != nil {
		oracle.metrics.failed(time.Now())
		oracle.writeMetrics()
		return err
	}
	return oracle.run(ctx)
}

// Endpoint URLs can carry credentials in their user info; logs name only the
// scheme and host.
func endpointLabel(endpoint string) string {
	if index := strings.Index(endpoint, "@"); index >= 0 {
		if scheme := strings.Index(endpoint, "://"); scheme >= 0 && scheme < index {
			return endpoint[:scheme+3] + endpoint[index+1:]
		}
	}
	return endpoint
}
