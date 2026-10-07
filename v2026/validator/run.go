package validator

// validator — the UR subnet validator binary (PLAN.md §7.2).
//
// One process, two jobs (VALIDATOR.md §0.5): MEASURE — walk /verify trails
// through per-hop egress-pinned tunnels, aggregate per-provider stats,
// persist completed proofs — and STEER — per tempo, commit the D_n×Q_n pool
// weight vector under CRv4. The effort-bounty epoch chores (register /
// submit-trails / claim) are deferred to the bounty phase (WHITEPAPER §9.3,
// D23); implementation parked at docs/parked/.
//
// CLI/auth/shutdown conventions mirror connect/provider/main.go: docopt,
// ~/.urnetwork/jwt written by `auth`, glog to stderr, NewEventWithContext +
// SetOnSignals.

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/docopt/docopt-go"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"github.com/urnetwork/sdk/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/internal/durableinspect"
	"github.com/urfoundation/sn/v2026/operatorlist"
)

const DefaultApiUrl = "https://api.bringyour.com"
const DefaultConnectUrl = "wss://connect.bringyour.com"

// Version is set via the linker:
// -ldflags "-X main.Version=$WARP_VERSION-$WARP_VERSION_CODE"
var Version string

func init() {
	initGlog()
}

func initGlog() {
	flag.Set("logtostderr", "true")
	flag.Set("stderrthreshold", "INFO")
	flag.Set("v", "0")
}

func RequireVersion() string {
	if version := os.Getenv("WARP_VERSION"); version != "" {
		return version
	}
	if Version != "" {
		return Version
	}
	return "0.0.0-dev"
}

// mainUsage returns the docopt usage string. Package-level so tests can
// parse argv against the real usage.
func mainUsage() string {
	return fmt.Sprintf(
		`UR subnet validator.

The default URLs are:
    api_url: %[1]s
    connect_url: %[2]s
    operators_url: %[3]s

Usage:
    validator storage-inspect --durable-volumes=<path> --durable-volumes-sha256=<hash> --directory=<path>...
    validator auth ([<auth_code>] | --user_auth=<user_auth> [--password=<password>] | --hotkey_seed_file=<path>) [-f]
        [--api_url=<api_url> | --operator=<domain> [--operators-url=<url>] [--state_dir=<path>]]
        [-v...]
    validator init (--config=<path> | --state_dir=<path> [--hotkey_seed_file=<path>] [--no_id=<id>]...)
        [-v...]
    validator register --config=<path> --coldkey_seed_file=<path>
        [--durable-volumes=<path> --durable-volumes-sha256=<hash>]
        [--burn_limit_rao=<n>] [--fee_limit_rao=<n>] [--apply | --dry-run]
        [-v...]
    validator stake add --amount_rao=<n> --config=<path> --coldkey_seed_file=<path>
        [--durable-volumes=<path> --durable-volumes-sha256=<hash>]
        [--limit_price_rao=<n>] [--allow_partial] [--fee_limit_rao=<n>] [--apply | --dry-run]
        [-v...]
    validator take status --config=<path> [--netuid=<id>]
        [-v...]
    validator take set --take=<percent> --config=<path> --coldkey_seed_file=<path>
        [--durable-volumes=<path> --durable-volumes-sha256=<hash>]
        [--fee_limit_rao=<n>] [--apply | --dry-run]
        [-v...]
    validator take childkey --netuid=<id> --take=<percent> --config=<path> --coldkey_seed_file=<path>
        [--durable-volumes=<path> --durable-volumes-sha256=<hash>]
        [--fee_limit_rao=<n>] [--apply | --dry-run]
        [-v...]
    validator activate --config=<path> [--relayer_key_file=<path>] [--apply | --dry-run]
        [--durable-volumes=<path> --durable-volumes-sha256=<hash>]
        [-v...]
    validator run --config=<path> [--progress-file=<path>]
        [--durable-volumes=<path> --durable-volumes-sha256=<hash>]
        [(--operators-refresh=<duration> [--operators-url=<url>] [--observe-unpinned-operators])]
        [-v...]
    validator run --all-operators [--operators-url=<url>] [--operators-refresh=<duration>]
        [(--auto-register --hotkey_seed_file=<path>)]
        [--concurrency=<n>] [--m=<depth>]
        [--rpc=<rpc_url>]... [--contract=<addr>] [--state_dir=<path>]
        [--adopt-legacy-measurement-key] [-v...]
    validator run [--api_url=<api_url>] [--connect_url=<connect_url>]
        [--concurrency=<n>] [--m=<depth>]
        [--rpc=<rpc_url>]... [--contract=<addr>] [--state_dir=<path>]
        [--adopt-legacy-measurement-key] [-v...]
    validator status [--config=<path>] [--api_url=<api_url>]
        [--rpc=<rpc_url>]... [--contract=<addr>] [--netuid=<id>]
        [--evm_key_file=<path>] [--hotkey_seed_file=<path>] [--state_dir=<path>]
        [-v...]

Options:
    -h --help                    Show this help and exit.
    --version                    Show version.
    -v...                        Verbose level (repeatable).
    -f                           Force overwrite the JWT token store file, if exists.
    --api_url=<api_url>          Custom API URL.
	--config=<path>                Strict release-1.0 production configuration; the only weight-writing mode.
                                 init/register/stake/activate/status accept it before its evidence_v2
                                 inputs are rendered; run does not.
	--progress-file=<path>         Optional bounded operational JSON outside protocol state.
    --durable-volumes=<path>      Exact external durable-volume declaration; required for mainnet state.
    --durable-volumes-sha256=<hash>  SHA-256 of the complete declaration bytes; does not change signed config.
    --directory=<path>           Existing service directory for read-only physical inspection; repeatable.
    --coldkey_seed_file=<path>   sr25519 coldkey seed (64 hex chars or 32 raw bytes) that signs
                                 register_limit / add_stake. The release config carries no coldkey and
                                 the EVM key's mirror account cannot sign a native extrinsic, so the
                                 seed file is required; keep it off the validator host afterwards.
    --burn_limit_rao=<n>         Maximum registration burn in rao passed to register_limit; the runtime
                                 rejects a higher live burn. Omitted: the burn observed at the read.
    --fee_limit_rao=<n>          Maximum native transaction fee in rao (payment_queryInfo is checked
                                 before broadcast) [default: 10000000].
    --amount_rao=<n>             TAO to stake, in rao (1 TAO = 1e9 rao); the pool converts it to alpha.
    --limit_price_rao=<n>        Use add_stake_limit with this maximum pool price in TAO rao per alpha
                                 instead of add_stake at the pool price.
    --allow_partial              With --limit_price_rao, allow a partial fill instead of fill-or-kill.
    --take=<percent>             Take in percent, at most two decimals, rounded down to parts of 65535
                                 (18 is 11796/65535); refused above the chain maximum. take set moves
                                 the delegate take; take childkey sets the childkey take on --netuid.
                                 take status shows both, for the config netuid and --netuid.
    --relayer_key_file=<path>    Hex secp256k1 EVM key that pays gas to publish the activations through
                                 the evidence journal; it receives no authority.
    --apply                      Sign, journal and broadcast. Without it every mutating command is a
                                 dry run that reads the live economics and reports what it would do.
    --dry-run                    Explicit dry run (the default).
    --no_id=<id>                 With init --state_dir, create <state_dir>/no-<id>/client.key for each
                                 operator; without any, create the flag-mode <state_dir>/.validator.key.
    --connect_url=<connect_url>  Custom connect (platform transport) URL.
    --all-operators              Measure every operator in the operator list, each with its own state
                                 under <state_dir>/operators/<domain>. Never steers.
    --operators-url=<url>        Operator list location [default: %[3]s].
    --operators-refresh=<duration>  Operator list refresh interval, a Go duration of at least 1m. Flag
                                 mode refreshes hourly without it; production mode follows the list
                                 only with it. With it, list drift is logged and written to
                                 operators-report.json beside the list cache.
    --observe-unpinned-operators  Also measure listed operators the config does not pin, for observation
                                 only, under <config.state_dir>/observed-operators/<domain>.
    --auto-register              Sign in with the hotkey where an operator has no JWT, and provision the
                                 measurement identity of a pristine operator directory.
    --operator=<domain>          Authenticate against this listed operator and write its JWT to
                                 <state_dir>/operators/<domain>/jwt.
    --user_auth=<user_auth>      Login with a username.
    --password=<password>        Login with a password (prompted when omitted).
    --adopt-legacy-measurement-key  Assert the existing measurement seed is original when first adopting
                                 a legacy client JWT; never creates a key or a client.
    --concurrency=<n>            Concurrent trail walkers [default: 4].
    --m=<depth>                  Requested trail depth M (server clamps to [4,16]) [default: 8].
    --rpc=<rpc_url>              EVM json-rpc endpoint (repeatable; ordered failover).
    --contract=<addr>            STSubnet contract address (0x hex).
    --netuid=<id>                Subnet netuid.
    --evm_key_file=<path>        Hex secp256k1 key file (stctl format). Its mirror is the
                                 validator coldkey [default: <state_dir>/evm.key].
    --hotkey_seed_file=<path>    sr25519 hotkey seed file. init creates it when missing; auth and
                                 auto-register only load it [default: <state_dir>/hotkey.seed].
    --state_dir=<path>           Validator state (vpk seed, proofs, stats, operator list cache)
                                 [default: ~/.urnetwork/validator].
`,
		DefaultApiUrl,
		DefaultConnectUrl,
		operatorlist.DefaultUrl,
	)
}

// Run is the validator CLI entry point (the executable lives at cli/validator).
// It takes the argument slice (os.Args[1:]) so it can be driven from tests.
func Run(args []string) {
	if len(args) != 0 && args[0] == "storage-inspect" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		if code := durableinspect.Run(ctx, args[1:], os.Stdout, os.Stderr); code != 0 {
			stop()
			os.Exit(code)
		}
		return
	}
	opts, err := docopt.ParseArgs(mainUsage(), args, RequireVersion())
	if err != nil {
		panic(err)
	}

	if authCmd, _ := opts.Bool("auth"); authCmd {
		auth(opts)
	} else if initCmd, _ := opts.Bool("init"); initCmd {
		initCommand(opts)
	} else if registerCmd, _ := opts.Bool("register"); registerCmd {
		registerCommand(opts)
	} else if stakeCmd, _ := opts.Bool("stake"); stakeCmd {
		stakeAddCommand(opts)
	} else if takeCmd, _ := opts.Bool("take"); takeCmd {
		// Before status: take status sets both commands.
		takeCommand(opts)
	} else if activateCmd, _ := opts.Bool("activate"); activateCmd {
		activateCommand(opts)
	} else if runCmd, _ := opts.Bool("run"); runCmd {
		run(opts)
	} else if statusCmd, _ := opts.Bool("status"); statusCmd {
		status(opts)
	}
}

// --- shared option helpers ---

func optString(opts docopt.Opts, key string, defaultValue string) string {
	if value, err := opts.String(key); err == nil && value != "" {
		return value
	}
	return defaultValue
}

func optStringList(opts docopt.Opts, key string) []string {
	if valueAny, ok := opts[key]; ok && valueAny != nil {
		if values, ok := valueAny.([]string); ok {
			return values
		}
	}
	return nil
}

func optInt(opts docopt.Opts, key string, defaultValue int) int {
	if value, err := opts.Int(key); err == nil {
		return value
	}
	if valueStr, err := opts.String(key); err == nil && valueStr != "" {
		if value, err := strconv.Atoi(valueStr); err == nil {
			return value
		}
	}
	return defaultValue
}

func optFloat(opts docopt.Opts, key string, defaultValue float64) float64 {
	if valueStr, err := opts.String(key); err == nil && valueStr != "" {
		if value, err := strconv.ParseFloat(valueStr, 64); err == nil {
			return value
		}
	}
	return defaultValue
}

func optUint64(opts docopt.Opts, key string, defaultValue uint64) uint64 {
	if valueStr, err := opts.String(key); err == nil && valueStr != "" {
		if value, err := strconv.ParseUint(valueStr, 10, 64); err == nil {
			return value
		}
	}
	return defaultValue
}

// identityOptionsFromOpts builds IdentityOptions from the common flags.
// The docopt defaults ("<state_dir>/…") are placeholders — resolve them to
// empty so LoadIdentity applies the real state-dir-relative defaults.
func identityOptionsFromOpts(opts docopt.Opts) IdentityOptions {
	stateDir := optString(opts, "--state_dir", "")
	if stateDir == "~/.urnetwork/validator" {
		stateDir = ""
	}
	evmKeyFile := optString(opts, "--evm_key_file", "")
	if strings.HasPrefix(evmKeyFile, "<state_dir>") {
		evmKeyFile = ""
	}
	hotkeySeedFile := optString(opts, "--hotkey_seed_file", "")
	if strings.HasPrefix(hotkeySeedFile, "<state_dir>") {
		hotkeySeedFile = ""
	}
	return IdentityOptions{
		StateDir:       stateDir,
		EvmKeyFile:     evmKeyFile,
		HotkeySeedFile: hotkeySeedFile,
	}
}

// dialChainFromOpts dials the configured EVM endpoints; contract required.
func dialChainFromOpts(opts docopt.Opts) (*ChainClient, error) {
	rpcUrls := optStringList(opts, "--rpc")
	contractStr := optString(opts, "--contract", "")
	if contractStr == "" {
		return nil, fmt.Errorf("--contract is required")
	}
	if !common.IsHexAddress(contractStr) {
		return nil, fmt.Errorf("--contract %q is not a hex address", contractStr)
	}
	return DialChain(rpcUrls, common.HexToAddress(contractStr))
}

// --- auth (mirrors provider auth: writes ~/.urnetwork/jwt, or with
// --operator that operator's jwt; see operators_auth.go) ---

func auth(opts docopt.Opts) {
	// The overwrite question precedes signal handling, so an interrupt there
	// still ends the command at once.
	target, proceed, err := confirmAuthTarget(context.Background(), opts, os.Stdin, os.Stdout)
	if err != nil {
		panic(err)
	}
	if !proceed {
		return
	}
	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(event.Ctx())
	defer cancel()
	if err := writeAuthJwt(ctx, opts, target, os.Stdout); err != nil {
		panic(err)
	}
}

// The target, and whether to go on: an existing JWT is replaced only with -f
// or a confirmation.
func confirmAuthTarget(ctx context.Context, opts docopt.Opts, input io.Reader, output io.Writer) (authTarget, bool, error) {
	target, err := authTargetFromOpts(ctx, opts)
	if err != nil {
		return authTarget{}, false, err
	}
	if _, err := os.Stat(target.jwtPath); !errors.Is(err, os.ErrNotExist) {
		if force, _ := opts.Bool("-f"); !force {
			fmt.Fprintf(output, "%s exists. Overwrite? [yN]\n", target.jwtPath)
			reader := bufio.NewReader(input)
			confirm, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
				return target, false, nil
			}
		}
	}
	return target, true, nil
}

// Signs in with the hotkey or a login and writes the network JWT. Only the
// auth code and password prompts read the terminal.
func writeAuthJwt(ctx context.Context, opts docopt.Opts, target authTarget, output io.Writer) error {
	var byJwt string
	var err error
	if hotkeySeedFile := authHotkeySeedFile(opts); hotkeySeedFile != "" {
		byJwt, err = hotkeyAuthJwt(ctx, target.apiUrl, hotkeySeedFile)
	} else {
		byJwt, err = loginNetworkJwt(ctx, opts, target.apiUrl, output)
	}
	if err != nil {
		return err
	}
	if byJwt == "" {
		return errors.New("the operator's answer has no network JWT")
	}
	if err := clientauth.WriteToken(target.jwtPath, byJwt); err != nil {
		return err
	}
	fmt.Fprintf(output, "Jwt written to %s\n", target.jwtPath)
	return nil
}

// An auth code, or a user and password, prompting on the terminal for what
// the options omit. An answer without a network JWT is refused: a JSON null
// decodes to no answer at all.
func loginNetworkJwt(ctx context.Context, opts docopt.Opts, apiUrl string, output io.Writer) (string, error) {
	clientStrategy := connect.NewClientStrategyWithDefaults(ctx)
	defer clientStrategy.Close()
	api := sdk.NewApi(ctx, clientStrategy, apiUrl)
	defer func() {
		_ = api.CloseAndWait(context.Background())
	}()

	if userAuth, err := opts.String("--user_auth"); err == nil && userAuth != "" {
		var password string
		if password, err = opts.String("--password"); err != nil || password == "" {
			fmt.Fprint(output, "Enter password: ")
			passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
			if err != nil {
				return "", err
			}
			password = string(passwordBytes)
			fmt.Fprintf(output, "\n")
		}

		loginResult, err := api.AuthLoginWithPasswordSyncWithContext(ctx, &sdk.AuthLoginWithPasswordArgs{
			UserAuth: userAuth,
			Password: password,
		})
		if err != nil {
			return "", err
		}
		if loginResult == nil {
			return "", errors.New("the operator's password login has no answer")
		}
		if loginResult.Error != nil {
			return "", fmt.Errorf("%s", loginResult.Error.Message)
		}
		if loginResult.VerificationRequired != nil {
			return "", fmt.Errorf("verification required for %s. Use the app or web to complete account setup.", loginResult.VerificationRequired.UserAuth)
		}
		if loginResult.Network == nil || loginResult.Network.ByJwt == "" {
			return "", errors.New("the operator's password login has no network")
		}
		return loginResult.Network.ByJwt, nil
	}
	authCode, _ := opts.String("<auth_code>")
	if authCode == "" {
		fmt.Fprint(output, "Enter auth code: ")
		authCodeBytes, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			return "", err
		}
		authCode = strings.TrimSpace(string(authCodeBytes))
		fmt.Fprintf(output, "\n")
	}

	authCodeLoginResult, err := api.AuthCodeLoginSyncWithContext(ctx, &sdk.AuthCodeLoginArgs{
		AuthCode: authCode,
	})
	if err != nil {
		return "", err
	}
	if authCodeLoginResult == nil {
		return "", errors.New("the operator's auth code login has no answer")
	}
	if authCodeLoginResult.Error != nil {
		return "", fmt.Errorf("%s", authCodeLoginResult.Error.Message)
	}
	if authCodeLoginResult.Jwt == "" {
		return "", errors.New("the operator's auth code login has no network JWT")
	}
	return authCodeLoginResult.Jwt, nil
}

// --- run ---

func run(opts docopt.Opts) {
	if configPath := optString(opts, "--config", ""); configPath != "" {
		progressPath, _ := opts.String("--progress-file")
		reference := durablevolume.Reference{
			Path: optString(opts, "--durable-volumes", ""), Sha256: optString(opts, "--durable-volumes-sha256", ""),
		}
		// The operator list runs beside the release only when asked for; the
		// release itself is started the same way either way.
		if optString(opts, "--operators-refresh", "") != "" {
			settings, err := productionOperatorListSettingsFromOpts(opts)
			exitOnError("validator run", err)
			runReleaseWithOperatorList(configPath, progressPath, reference, settings)
			return
		}
		runReleaseConfig(configPath, progressPath, reference)
		return
	}
	if optBool(opts, "--all-operators") {
		runAllOperators(opts)
		return
	}
	if err := rejectLegacySteeringOptions(opts); err != nil {
		panic(err)
	}
	settings, err := measurementSettingsFromOpts(opts)
	if err != nil {
		panic(err)
	}
	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	defer stopSignals()
	if err := settings.run(ctx, os.Stdout); err != nil {
		panic(err)
	}
}

func rejectLegacySteeringOptions(opts docopt.Opts) error {
	if len(optStringList(opts, "--substrate")) != 0 || optInt(opts, "--netuid", -1) >= 0 || optUint64(opts, "--version_key", 0) != 0 || optUint64(opts, "--tempo_blocks", 0) != 0 {
		return errors.New("legacy flag-mode steering is disabled; use validator run --config=<release-1.0.yml>")
	}
	return nil
}

// The submit-trails / claim commands (the effort-bounty flow) are deferred to
// the bounty phase (WHITEPAPER §9.3, D23); implementation parked at
// docs/parked/. Registration, staking and activation live in
// native_commands.go.

// --- status ---

func status(opts docopt.Opts) {
	if configPath := optString(opts, "--config", ""); configPath != "" {
		statusRelease(configPath)
		return
	}
	identityOpts := identityOptionsFromOpts(opts)
	identityOpts.LoadHotkey = true
	identity, err := LoadIdentity(identityOpts)
	if err != nil {
		panic(err)
	}

	fmt.Printf("state_dir: %s\n", identity.StateDir)
	fmt.Printf("vpk: 0x%s\n", hex.EncodeToString(identity.Vpk))
	if _, err := readNetworkJwt(); err == nil {
		fmt.Printf("network jwt: present\n")
	} else {
		fmt.Printf("network jwt: MISSING — run `validator auth`\n")
	}
	if identity.EvmKey != nil {
		mirror, _ := identity.MirrorSs58()
		fmt.Printf("evm address: %s\n", identity.EvmAddress)
		fmt.Printf("coldkey (mirror ss58): %s\n", mirror)
	} else {
		fmt.Printf("evm key: not configured (optional in v1; its mirror is the validator coldkey)\n")
	}
	if identity.Hotkey != nil {
		fmt.Printf("hotkey ss58: %s\n", identity.Hotkey.Address())
		fmt.Printf("hotkey pubkey: 0x%x\n", identity.Hotkey.PublicKey())
	}

	// Local proof / stats summary.
	if store, err := NewProofStore(identity.StateDir); err == nil {
		if records, skipped, err := store.Load(); err == nil {
			byEpoch := map[uint64]int{}
			for _, record := range records {
				byEpoch[record.Epoch]++
			}
			fmt.Printf("proofs: %d completed trails (%d unparseable lines)\n", len(records), skipped)
			for epoch, n := range byEpoch {
				fmt.Printf("  epoch %d: %d\n", epoch, n)
			}
		}
	}
	stats := NewStatsEngine(StatsConfig{})
	if err := stats.Load(identity.StateDir); err == nil {
		quality := stats.SortedQuality()
		fmt.Printf("scored providers: %d\n", len(quality))
	}

	// Chain state.
	if len(optStringList(opts, "--rpc")) == 0 || optString(opts, "--contract", "") == "" {
		fmt.Printf("chain: pass --rpc and --contract for on-chain status\n")
		return
	}
	chain, err := dialChainFromOpts(opts)
	if err != nil {
		fmt.Printf("chain: %v\n", err)
		return
	}
	defer chain.Close()

	fmt.Printf("chain: %s (chain id %s)\n", chain.RpcUrl(), chain.ChainId())
	if netuid, err := chain.Netuid(); err == nil {
		fmt.Printf("contract netuid: %d\n", netuid)
	}
	epoch, err := chain.PendingEpoch()
	if err != nil {
		fmt.Printf("epoch: %v\n", err)
		return
	}
	fmt.Printf("epoch (pending): %s\n", epoch)
}
