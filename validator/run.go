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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/docopt/docopt-go"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"

	"github.com/urfoundation/sn/clientauth"
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
	os.Stderr = os.Stdout
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
    api_url: %s
    connect_url: %s

Usage:
    validator auth ([<auth_code>] | --user_auth=<user_auth> [--password=<password>]) [-f]
        [--api_url=<api_url>]
        [-v...]
    validator run --config=<path>
        [-v...]
    validator run [--api_url=<api_url>] [--connect_url=<connect_url>]
        [--concurrency=<n>] [--m=<depth>]
        [--rpc=<rpc_url>]... [--contract=<addr>] [--state_dir=<path>]
        [-v...]
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
    --connect_url=<connect_url>  Custom connect (platform transport) URL.
    --user_auth=<user_auth>      Login with a username.
    --password=<password>        Login with a password (prompted when omitted).
    --concurrency=<n>            Concurrent trail walkers [default: 4].
    --m=<depth>                  Requested trail depth M (server clamps to [4,16]) [default: 8].
    --rpc=<rpc_url>              EVM json-rpc endpoint (repeatable; ordered failover).
    --contract=<addr>            STSubnet contract address (0x hex).
    --netuid=<id>                Subnet netuid.
    --evm_key_file=<path>        Hex secp256k1 key file (stctl format). Its mirror is the
                                 validator coldkey [default: <state_dir>/evm.key].
    --hotkey_seed_file=<path>    sr25519 hotkey seed file (created if missing)
                                 [default: <state_dir>/hotkey.seed].
    --state_dir=<path>           Validator state (vpk seed, proofs, stats)
                                 [default: ~/.urnetwork/validator].
`,
		DefaultApiUrl,
		DefaultConnectUrl,
	)
}

// Run is the validator CLI entry point (the executable lives at cli/validator).
// It takes the argument slice (os.Args[1:]) so it can be driven from tests.
func Run(args []string) {
	opts, err := docopt.ParseArgs(mainUsage(), args, RequireVersion())
	if err != nil {
		panic(err)
	}

	if authCmd, _ := opts.Bool("auth"); authCmd {
		auth(opts)
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

// --- auth (mirrors provider auth: writes ~/.urnetwork/jwt) ---

func auth(opts docopt.Opts) {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	urNetworkDir := filepath.Join(home, ".urnetwork")
	jwtPath := filepath.Join(urNetworkDir, "jwt")

	if _, err := os.Stat(jwtPath); !errors.Is(err, os.ErrNotExist) {
		if force, _ := opts.Bool("-f"); !force {
			fmt.Printf("%s exists. Overwrite? [yN]\n", jwtPath)
			reader := bufio.NewReader(os.Stdin)
			confirm, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
				return
			}
		}
	}

	apiUrl := optString(opts, "--api_url", DefaultApiUrl)

	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(event.Ctx())
	defer cancel()

	clientStrategy := connect.NewClientStrategyWithDefaults(ctx)
	api := sdk.NewApi(ctx, clientStrategy, apiUrl)
	defer api.Close()

	var byJwt string
	if userAuth, err := opts.String("--user_auth"); err == nil && userAuth != "" {
		var password string
		if password, err = opts.String("--password"); err != nil || password == "" {
			fmt.Print("Enter password: ")
			passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
			if err != nil {
				panic(err)
			}
			password = string(passwordBytes)
			fmt.Printf("\n")
		}

		loginResult, err := api.AuthLoginWithPasswordSyncWithContext(ctx, &sdk.AuthLoginWithPasswordArgs{
			UserAuth: userAuth,
			Password: password,
		})
		if err != nil {
			panic(err)
		}
		if loginResult.Error != nil {
			panic(fmt.Errorf("%s", loginResult.Error.Message))
		}
		if loginResult.VerificationRequired != nil {
			panic(fmt.Errorf("verification required for %s. Use the app or web to complete account setup.", loginResult.VerificationRequired.UserAuth))
		}
		byJwt = loginResult.Network.ByJwt
	} else {
		authCode, _ := opts.String("<auth_code>")
		if authCode == "" {
			fmt.Print("Enter auth code: ")
			authCodeBytes, err := term.ReadPassword(int(syscall.Stdin))
			if err != nil {
				panic(err)
			}
			authCode = strings.TrimSpace(string(authCodeBytes))
			fmt.Printf("\n")
		}

		authCodeLoginResult, err := api.AuthCodeLoginSyncWithContext(ctx, &sdk.AuthCodeLoginArgs{
			AuthCode: authCode,
		})
		if err != nil {
			panic(err)
		}
		if authCodeLoginResult.Error != nil {
			panic(fmt.Errorf("%s", authCodeLoginResult.Error.Message))
		}
		byJwt = authCodeLoginResult.Jwt
	}

	if byJwt != "" {
		if err := clientauth.WriteToken(jwtPath, byJwt); err != nil {
			panic(err)
		}
		fmt.Printf("Jwt written to %s\n", jwtPath)
	}
}

// --- run ---

func run(opts docopt.Opts) {
	if configPath := optString(opts, "--config", ""); configPath != "" {
		runReleaseConfig(configPath)
		return
	}
	if err := rejectLegacySteeringOptions(opts); err != nil {
		panic(err)
	}
	apiUrl := optString(opts, "--api_url", DefaultApiUrl)
	connectUrl := optString(opts, "--connect_url", DefaultConnectUrl)
	concurrency := optInt(opts, "--concurrency", 4)
	m := optInt(opts, "--m", connect.VerifyMDefault)

	identityOpts := identityOptionsFromOpts(opts)
	identity, err := LoadIdentity(identityOpts)
	if err != nil {
		panic(err)
	}

	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(event.Ctx())
	defer cancel()

	clientStrategy := connect.NewClientStrategyWithDefaults(ctx)
	api := sdk.NewApi(ctx, clientStrategy, apiUrl)
	defer api.Close()

	networkTokenPath, err := networkJwtPath()
	if err != nil {
		panic(err)
	}
	clientTokenPath := filepath.Join(identity.StateDir, ".validator.jwt")
	byClientJwt, clientId, err := clientauth.LoadOrCreateClientJwt(
		ctx,
		api,
		networkTokenPath,
		clientTokenPath,
		fmt.Sprintf("validator %s", RequireVersion()),
	)
	if err != nil {
		panic(err)
	}
	refreshPersistSub := api.AddJwtRefreshListener(clientauth.JwtRefreshListenerFunc(func(jwt string) {
		if err := clientauth.WriteToken(clientTokenPath, jwt); err != nil {
			fmt.Printf("validator client JWT save failed: %s\n", err)
			cancel()
		}
	}))
	defer refreshPersistSub.Close()
	logoutSub := api.AddAuthLogoutListener(clientauth.AuthLogoutListenerFunc(func() {
		if err := clientauth.MarkRejected(clientTokenPath, networkTokenPath); err != nil {
			fmt.Printf("validator client JWT rejection save failed: %s\n", err)
		}
		fmt.Printf("validator authentication was rejected; run `validator auth` if the bootstrap credential is no longer valid\n")
		cancel()
	}))
	defer logoutSub.Close()

	// Client identity: authenticate a client id under the network, then
	// run a connect client whose ClientKeySeed is the persisted vpk seed —
	// the ClientKeyManager publishes the vpk to the platform
	// (ckey_<clientId>), which is what the /verify server checks SEED
	// bodies against (VALIDATOR.md §2).
	fmt.Printf("client_id: %s\n", clientId)
	fmt.Printf("vpk: %s\n", hex.EncodeToString(identity.Vpk))

	clientSettings := connect.DefaultClientSettings()
	clientSettings.ClientKeySeed = identity.VpkSeed
	clientOob := connect.NewApiOutOfBandControl(ctx, clientStrategy, byClientJwt, apiUrl)
	identityClient := connect.NewClient(ctx, clientId, clientOob, clientSettings)
	defer identityClient.Close()
	instanceId := connect.NewId()
	platformTransport := connect.NewPlatformTransportWithDefaults(ctx, clientStrategy, identityClient.RouteManager(), connectUrl, &connect.ClientAuth{
		ByJwt:      byClientJwt,
		InstanceId: instanceId,
		AppVersion: RequireVersion(),
	})
	refreshTransportSub := api.AddJwtRefreshListener(clientauth.JwtRefreshListenerFunc(func(jwt string) {
		clientOob.SetByJwt(jwt)
		platformTransport.SetAuth(&connect.ClientAuth{
			ByJwt:      jwt,
			InstanceId: instanceId,
			AppVersion: RequireVersion(),
		})
	}))
	defer refreshTransportSub.Close()
	api.StartJwtRefresh()

	// Optional chain access: epoch stamping for proofs + steering reads.
	var chain *ChainClient
	if len(optStringList(opts, "--rpc")) > 0 && optString(opts, "--contract", "") != "" {
		chain, err = dialChainFromOpts(opts)
		if err != nil {
			panic(err)
		}
		defer chain.Close()
		fmt.Printf("chain: %s (chain id %s)\n", chain.RpcUrl(), chain.ChainId())
	} else {
		fmt.Printf("chain: not configured (proofs will carry epoch 0; steering disabled)\n")
	}

	// Cached epoch for proof stamping.
	var cachedEpoch atomic.Uint64
	epochFn := func() uint64 { return cachedEpoch.Load() }
	if chain != nil {
		refreshEpoch := func() {
			if epoch, err := chain.Epoch(); err == nil {
				cachedEpoch.Store(epoch.Uint64())
			}
		}
		refreshEpoch()
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					refreshEpoch()
				}
			}
		}()
	}

	stats := NewStatsEngine(StatsConfig{})
	if err := stats.Load(identity.StateDir); err != nil {
		fmt.Printf("stats load: %v (starting fresh)\n", err)
	}
	store, err := NewProofStore(identity.StateDir)
	if err != nil {
		panic(err)
	}

	transport := NewTunnelTransport(ctx, clientStrategy, TunnelTransportConfig{
		ApiUrl:         apiUrl,
		ConnectUrl:     connectUrl,
		ByClientJwt:    api.GetByJwt,
		SourceClientId: clientId,
	})
	keyRing := NewApiServerKeyRing(api)
	seedPicker := NewFindProvidersSeedPicker(api, clientId)

	engine := NewTrailEngine(
		clientId, identity.Vsk, transport, keyRing, seedPicker, stats, store, epochFn,
		TrailEngineConfig{M: m},
	)

	go engine.Run(ctx, concurrency)

	// The flag-mode runner is measurement-only. Release weight writes require
	// the strict multi-NO config path, which uses per-NO quality and exact CRv4
	// intents. There is no CLI route to the legacy global-quality aggregator.
	fmt.Printf("steering: disabled in legacy flag mode; use --config for release-1.0 steering\n")

	// Periodic stats snapshots + final save on shutdown.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := stats.Save(identity.StateDir); err != nil {
					fmt.Printf("stats save: %v\n", err)
				}
			}
		}
	}()

	fmt.Printf("validator %s running (concurrency %d, M %d)\n", RequireVersion(), concurrency, m)
	<-ctx.Done()
	if err := stats.Save(identity.StateDir); err != nil {
		fmt.Printf("stats save: %v\n", err)
	}
	os.Exit(0)
}

func rejectLegacySteeringOptions(opts docopt.Opts) error {
	if len(optStringList(opts, "--substrate")) != 0 || optInt(opts, "--netuid", -1) >= 0 || optUint64(opts, "--version_key", 0) != 0 || optUint64(opts, "--tempo_blocks", 0) != 0 {
		return errors.New("legacy flag-mode steering is disabled; use validator run --config=<release-1.0.yml>")
	}
	return nil
}

// The register / submit-trails / claim commands (the effort-bounty flow) are
// deferred to the bounty phase (WHITEPAPER §9.3, D23); implementation parked
// at docs/parked/.

// --- status ---

func status(opts docopt.Opts) {
	if configPath := optString(opts, "--config", ""); configPath != "" {
		cfg, err := LoadReleaseConfig(configPath)
		if err != nil {
			panic(err)
		}
		fmt.Printf("release: %s production=%t validator=%d netuid=%d operators=%d\n", cfg.Release, cfg.Production, cfg.ValidatorID, cfg.Netuid, len(cfg.Operators))
		fmt.Printf("coordinator: %s\npolicy: %s\nstate_dir: %s\n", cfg.Coordinator, cfg.PolicyHash, cfg.StateDir)
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
