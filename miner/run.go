package miner

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	// "net"
	mathrand "math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/proxy"
	"golang.org/x/term"

	"github.com/docopt/docopt-go"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"

	"github.com/urfoundation/sn/clientauth"
)

const DefaultApiUrl = "https://api.bringyour.com"
const DefaultConnectUrl = "wss://connect.bringyour.com"

// this value is set via the linker, e.g.
// -ldflags "-X main.Version=$WARP_VERSION-$WARP_VERSION_CODE"
var Version string

func init() {
	// debug.SetGCPercent(10)

	initGlog()

	// initPprof()
}

func initGlog() {
	flag.Set("logtostderr", "true")
	flag.Set("stderrthreshold", "INFO")
	flag.Set("v", "0")
	// unlike unix, the android/ios standard is for diagnostics to go to stdout
	os.Stderr = os.Stdout
}

// mainUsage returns the docopt usage string. Package-level (rather than
// inline in `main`) so tests can parse argv against the real usage.
func mainUsage() string {
	return fmt.Sprintf(
		`Connect provider.

The default URLs are:
    api_url: %s
    connect_url: %s

A network saved with "provider choose_network" replaces these defaults;
"provider choose_network --show" prints the network actually in effect.

Usage:
    provider auth ([<auth_code>] | --user_auth=<user_auth> [--password=<password>]) [-f]
    	[--api_url=<api_url>]
    	[--max-memory=<mem>]
    	[-v...]
    provider provide [--port=<port>]
        [--api_url=<api_url>]
        [--connect_url=<connect_url>]
        [--wallet=<coldkey_ss58>]
        [--max-memory=<mem>]
        [-v...]
    provider auth-provide ([<auth_code>] | --user_auth=<user_auth> [--password=<password>]) [-f]
    	[--port=<port>]
        [--api_url=<api_url>]
        [--connect_url=<connect_url>]
        [--wallet=<coldkey_ss58>]
        [--max-memory=<mem>]
        [-v...]
    provider wallet set <coldkey_ss58>
        [--api_url=<api_url>]
        [-v...]
    provider claim [--epoch=<epoch>] [--rpc=<rpc_url>]... [--key_file=<key_file>] [--dry-run]
        [--api_url=<api_url>]
        [-v...]
    provider bind-head --hotkey=<hex> --registrant=<registrant> --contract=<contract> [--rpc=<rpc_url>]... [--key_file=<key_file>] [--dry-run]
        [-v...]
    provider unbind-head --hotkey=<hex> [--contract=<contract>] [--rpc=<rpc_url>]... [--key_file=<key_file>] [--dry-run]
        [-v...]
    provider proxy auth add [<key>] <proxy_user> <proxy_password> [-f]
    provider proxy auth remove [<key>] [--all]
    provider proxy add [<key_address>...] [--proxy_file=<proxy_file>] [-f]
    provider proxy remove [<key_address>...] [--all]
    provider choose_network <api_url> <connect_url>
    provider choose_network --reset
    provider choose_network --show

Options:
    -h --help                        Show this help and exit.
    --version                        Show version.
    -v...                            Enable verbose mode. -v implies verbose level 1,
    				                 -vv implies level 2... etc.
    -f                               Force overwrite the JWT token store file or proxy value, if exists.
                                     By default, existing values will not be overwritten.
    --api_url=<api_url>              Specify a custom API URL to use.
    --connect_url=<connect_url>      Specify a custom connect URL to use.
    <api_url>                        API URL to save as the chosen network (http:// or https://).
    <connect_url>                    Connect URL to save as the chosen network (ws:// or wss://).
    --reset                          With choose_network, clear the saved network and revert to the main network.
    --show                           With choose_network, print the network currently in effect and exit.
    --user_auth=<user_auth>	         Login with a username.
    --password=<password>            Login with a password. If --user_auth is used, you will be prompted for your
    				                 password anyways, if you don't specify it using this option.
    -p --port=<port>                 Status server port [default: 0].
    --max-memory=<mem>               Set the maximum amount of memory in bytes, or the suffixes b, kib, mib, gib may be used [This is a soft limit].
    --wallet=<coldkey_ss58>          Also set the subnet claim wallet at startup, same as provider wallet set.
                                     A failure is logged and does not block providing.
    <coldkey_ss58>                   Subnet claim wallet: an ss58 coldkey address (prefix 42).
    --epoch=<epoch>                  Epoch to fetch the subnet pool claim for. Defaults to the last
                                     finalized epoch, which is the epoch before the current one.
    --rpc=<rpc_url>                  EVM json-rpc endpoint used to check the payout root on-chain.
                                     May be repeated; endpoints are tried in order until one answers.
    --key_file=<key_file>            Path to a hex-encoded 32-byte secp256k1 EVM private key. When given,
                                     claim / bind-head / unbind-head sign and submit the transaction (via
                                     the sn/miner/onchain path) instead of only printing the calldata.
    --dry-run                        With --key_file, stop at the eth_call preflight and send nothing.
                                     Without --key_file the command only verifies, so it has no effect.
    --hotkey=<hex>                   Head-tier miner hotkey as a 0x-optional 32-byte hex account id.
    --registrant=<registrant>        The EVM address that will submit bindHead via snclaim (0x, 20 bytes).
                                     The head-bind digest is bound to this address, so it MUST equal the
                                     snclaim sender, whose mirror must be the hotkey's on-chain coldkey.
    --contract=<contract>            STSubnet proxy contract address (0x, 20 bytes).
    <key>                            Authentication key
    <proxy_user>                     SOCKS5 user
    <proxy_password>                 SOCKS5 password
    <key_address>                    SOCKS5 server as host:port, host:port:user:pass, host:port::, or key@host:port
    --proxy_file=<proxy_file>        A path to a file where each line contains on entry as host:port, host:port:user:pass, host:port::, or key@host:port`,
		DefaultApiUrl,
		DefaultConnectUrl,
	)
}

// Run is the miner CLI entry point (the executable lives at cli/miner). It takes
// the argument slice (os.Args[1:]) so it can be driven from tests. The miner is
// the subnet's provider: it runs the provide/proxy/auth flows plus the on-chain
// wallet / claim / bind-head actions (formerly connect/provider).
func Run(args []string) {
	opts, err := docopt.ParseArgs(mainUsage(), args, RequireVersion())

	if err != nil {
		panic(err)
	}

	if proxy, _ := opts.Bool("proxy"); proxy {
		if auth, _ := opts.Bool("auth"); auth {
			if add, _ := opts.Bool("add"); add {
				proxyAuthAdd(opts)
			} else if remove, _ := opts.Bool("remove"); remove {
				proxyAuthRemove(opts)
			}
		} else if add, _ := opts.Bool("add"); add {
			proxyAdd(opts)
		} else if remove, _ := opts.Bool("remove"); remove {
			proxyRemove(opts)
		}
	} else if wallet, _ := opts.Bool("wallet"); wallet {
		if set, _ := opts.Bool("set"); set {
			walletSet(opts)
		}
	} else if claim_, _ := opts.Bool("claim"); claim_ {
		claim(opts)
	} else if bindHead_, _ := opts.Bool("bind-head"); bindHead_ {
		bindHead(opts)
	} else if unbindHead_, _ := opts.Bool("unbind-head"); unbindHead_ {
		unbindHead(opts)
	} else if auth_, _ := opts.Bool("auth"); auth_ {
		auth(opts)
	} else if provide_, _ := opts.Bool("provide"); provide_ {
		provide(opts)
	} else if authProvide, _ := opts.Bool("auth-provide"); authProvide {
		auth(opts)
		provide(opts)
	} else if chooseNetwork, _ := opts.Bool("choose_network"); chooseNetwork {
		if err := chooseNetworkCmd(opts); err != nil {
			fmt.Printf("%s\n", err)
			os.Exit(1)
		}
	}
}

func auth(opts docopt.Opts) {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	urNetworkDir := filepath.Join(home, ".urnetwork")
	jwtPath := filepath.Join(urNetworkDir, "jwt")

	if _, err := os.Stat(jwtPath); !errors.Is(err, os.ErrNotExist) {
		// jwt exists
		if force, _ := opts.Bool("-f"); !force {
			fmt.Printf("%s exists. Overwrite? [yN]\n", jwtPath)

			reader := bufio.NewReader(os.Stdin)
			confirm, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
				return
			}

		}
	}

	apiUrl, err := resolveApiUrl(opts)
	if err != nil {
		fmt.Printf("network config error: %s\n", err)
		os.Exit(1)
	}

	maxMemoryHumanReadable, err := opts.String("--max-memory")
	var maxMemory connect.ByteCount
	if err == nil {
		maxMemory, err = connect.ParseByteCount(maxMemoryHumanReadable)
		if err != nil {
			panic(fmt.Errorf("Bad mem argument: %s", maxMemoryHumanReadable))
		}
	}
	if 0 < maxMemory {
		connect.ResizeMessagePools(maxMemory / 8)
		debug.SetMemoryLimit(maxMemory)
	}

	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(event.Ctx())
	defer cancel()

	clientStrategy := connect.NewClientStrategyWithDefaults(ctx)
	api := sdk.NewApi(ctx, clientStrategy, apiUrl)
	defer api.Close()

	var byJwt string
	if userAuth, err := opts.String("--user_auth"); err == nil {
		// user_auth and password

		var password string
		if password, err = opts.String("--password"); err == nil && password == "" {
			fmt.Print("Enter password: ")
			passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
			if err != nil {
				panic(err)
			}
			password = string(passwordBytes)
			fmt.Printf("\n")
		}

		// fmt.Printf("userAuth='%s'; password='%s'\n", userAuth, password)

		loginArgs := &sdk.AuthLoginWithPasswordArgs{
			UserAuth: userAuth,
			Password: password,
		}
		loginResult, err := api.AuthLoginWithPasswordSyncWithContext(ctx, loginArgs)
		if err != nil {
			panic(err)
		}
		if loginResult.Error != nil {
			panic(fmt.Errorf("%s", loginResult.Error.Message))
		}
		if loginResult.VerificationRequired != nil {
			panic(fmt.Errorf("Verification required for %s. Use the app or web to complete account setup.", loginResult.VerificationRequired.UserAuth))
		}

		byJwt = loginResult.Network.ByJwt
	} else {
		// auth_code
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

		authCodeLogin := &sdk.AuthCodeLoginArgs{
			AuthCode: authCode,
		}
		authCodeLoginResult, err := api.AuthCodeLoginSyncWithContext(ctx, authCodeLogin)
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

func provide(opts docopt.Opts) {
	port, _ := opts.Int("--port")

	apiUrl, err := resolveApiUrl(opts)
	if err != nil {
		fmt.Printf("network config error: %s\n", err)
		os.Exit(1)
	}

	connectUrl, err := resolveConnectUrl(opts)
	if err != nil {
		fmt.Printf("network config error: %s\n", err)
		os.Exit(1)
	}

	maxMemoryHumanReadable, err := opts.String("--max-memory")
	var maxMemory connect.ByteCount
	if err == nil {
		maxMemory, err = connect.ParseByteCount(maxMemoryHumanReadable)
		if err != nil {
			panic(fmt.Errorf("Bad mem argument: %s", maxMemoryHumanReadable))
		}
	}
	if 0 < maxMemory {
		debug.SetMemoryLimit(maxMemory)
	}

	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(event.Ctx())
	defer cancel()

	// subnet claim wallet (sn/PLAN.md 7.3, decision D-2): validate the
	// ss58 coldkey locally and idempotently register it with the platform
	// before providing starts. A failure warns and does not block
	// providing — the wallet may already be set from a previous run, and
	// the call can be retried any time with `provider wallet set`.
	if coldkeySs58, walletErr := opts.String("--wallet"); walletErr == nil && coldkeySs58 != "" {
		walletClientStrategy := connect.NewClientStrategyWithDefaults(ctx)
		if err := snSetWallet(ctx, walletClientStrategy, apiUrl, coldkeySs58); err != nil {
			fmt.Printf("subnet wallet not set: %s\n", err)
			fmt.Printf("continuing to provide. Retry with: provider wallet set <coldkey_ss58>\n")
		}
	}

	allProxySettings := readProxySettings()
	providerCount := len(allProxySettings)
	if providerCount == 0 {
		providerCount = 1
	}
	providerMemoryTarget := maxMemory
	if 0 < providerMemoryTarget {
		providerMemoryTarget /= connect.ByteCount(providerCount)
	}

	provideWithProxy := func(proxySettings *connect.ProxySettings) {
		proxyCtx, proxyCancel := context.WithCancel(ctx)
		defer proxyCancel()

		clientStrategySettings := connect.DefaultClientStrategySettings()
		clientStrategySettings.ProxySettings = proxySettings
		networkSpace := sdk.NewNetworkSpaceWithUrls(proxyCtx, apiUrl, connectUrl, clientStrategySettings)
		api := networkSpace.GetApi()

		networkJwtPath, err := providerStatePath("jwt")
		if err != nil {
			panic(err)
		}
		clientJwtPath, err := providerClientJwtPath(proxySettings)
		if err != nil {
			panic(err)
		}

		byClientJwt, clientId, err := func() (string, connect.Id, error) {
			for {
				byClientJwt, clientId, err := clientauth.LoadOrCreateClientJwt(
					proxyCtx,
					api,
					networkJwtPath,
					clientJwtPath,
					fmt.Sprintf("provider %s %s", runtime.GOOS, RequireVersion()),
				)
				if err == nil {
					return byClientJwt, clientId, nil
				}
				retryDelay := time.Duration(500+mathrand.Intn(10000)) * time.Millisecond
				fmt.Printf("init proxy auth failed. Will retry in %.2fs\n", float64(retryDelay/time.Millisecond)/1000.0)
				select {
				case <-proxyCtx.Done():
					return "", connect.Id{}, proxyCtx.Err()
				case <-time.After(retryDelay):
				}
			}
		}()
		if err != nil {
			if proxyCtx.Err() != nil {
				return
			}
			panic(err)
		}

		refreshSub := api.AddJwtRefreshListener(clientauth.JwtRefreshListenerFunc(func(jwt string) {
			if err := clientauth.WriteToken(clientJwtPath, jwt); err != nil {
				fmt.Printf("provider client JWT save failed: %s\n", err)
				cancel()
			}
		}))
		defer refreshSub.Close()
		logoutSub := api.AddAuthLogoutListener(clientauth.AuthLogoutListenerFunc(func() {
			if err := clientauth.MarkRejected(clientJwtPath, networkJwtPath); err != nil {
				fmt.Printf("provider client JWT rejection save failed: %s\n", err)
			}
			fmt.Printf("provider authentication was rejected; run `provider auth` if the bootstrap credential is no longer valid\n")
			cancel()
		}))
		defer logoutSub.Close()

		seed, _ := readProviderClientKeySeed()
		certPem, keyPem, _ := readProviderTlsCertAndKey()
		settings := sdk.DefaultDeviceLocalSettings()
		settings.KeyMaterial = sdk.NewDeviceLocalKeyMaterial(seed, certPem, keyPem)
		settings.MemoryTargetByteCount = sdk.ByteCount(providerMemoryTarget)
		instanceId := sdk.NewId()
		device, err := sdk.NewDeviceLocal(
			networkSpace,
			byClientJwt,
			fmt.Sprintf("provider %s %s", runtime.GOOS, RequireVersion()),
			"",
			RequireVersion(),
			instanceId,
			settings,
		)
		if err != nil {
			panic(err)
		}
		defer device.Close()

		// Always-on public mode includes network and friends/family service,
		// matching the SDK's hierarchical provide contract.
		device.SetProvideControlMode(sdk.ProvideControlModeAlways)

		keyMaterial := device.GetKeyMaterial()
		if seed := keyMaterial.GetClientKeySeed(); 0 < len(seed) {
			if err := writeProviderClientKeySeed(seed); err != nil {
				fmt.Printf("provider client key save failed: %s\n", err)
			}
		}
		certPem = keyMaterial.GetProvideTlsCertificatePem()
		keyPem = keyMaterial.GetProvideTlsPrivateKeyPem()
		if 0 < len(certPem) && 0 < len(keyPem) {
			if err := writeProviderTlsCertAndKey(certPem, keyPem); err != nil {
				fmt.Printf("provider tls cert/key save failed: %s\n", err)
			}
		}

		fmt.Printf("client_id: %s\n", clientId)
		fmt.Printf("instance_id: %s\n", instanceId)

		select {
		case <-proxyCtx.Done():
		}
	}

	var wg sync.WaitGroup

	if 0 < len(allProxySettings) {
		fmt.Printf("Using %d proxy servers:\n", len(allProxySettings))

		for i, proxySettings := range allProxySettings {
			var user string
			var password string
			if proxySettings.Auth != nil {
				user = proxySettings.Auth.User
				password = proxySettings.Auth.Password
			}
			fmt.Printf("  proxy[%d] %s (%s/%s)\n",
				i,
				proxySettings.Address,
				obfuscateUser(user),
				obfuscatePassword(password),
			)
		}
		for i, proxySettings := range allProxySettings {
			wg.Add(1)
			go connect.HandleError(func() {
				defer wg.Done()

				initialDelay := time.Duration(i) * 100 * time.Millisecond
				select {
				case <-ctx.Done():
				case <-time.After(initialDelay):
				}

				provideWithProxy(proxySettings)
			})
		}
	} else {
		wg.Add(1)
		go connect.HandleError(func() {
			defer wg.Done()
			provideWithProxy(nil)
		})
	}

	if 0 < port {
		fmt.Printf(
			"Provider %s started. Status on *:%d\n",
			RequireVersion(),
			port,
		)
		statusServer := &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: &Status{},
		}
		defer statusServer.Shutdown(ctx)

		go connect.HandleError(func() {
			defer cancel()
			err := statusServer.ListenAndServe()
			if err != nil {
				fmt.Printf("status error: %s\n", err)
			}
		}, cancel)
	} else {
		fmt.Printf(
			"Provider %s started\n",
			RequireVersion(),
		)
	}

	wg.Wait()

	// exit
	os.Exit(0)
}

// providerStateDir returns the absolute path of the provider state
// directory, ~/.urnetwork — the one place `jwt`, `client_id`,
// `network.json`, `.provider.key` and `.provider.cert` live. Does not
// create it.
func providerStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".urnetwork"), nil
}

// providerStatePath returns the absolute filesystem path of a named
// provider state file under ~/.urnetwork (alongside `jwt`). Does not
// create the directory.
func providerStatePath(name string) (string, error) {
	dir, err := providerStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// providerClientJwtPath gives each independently connected proxy provider a
// stable renewable client identity without placing proxy credentials in a
// filename. The direct (non-proxy) provider keeps the simple legacy-adjacent
// name.
func providerClientJwtPath(proxySettings *connect.ProxySettings) (string, error) {
	if proxySettings == nil {
		return providerStatePath(".provider.jwt")
	}
	key := proxySettings.Network + "\x00" + proxySettings.Address
	sum := sha256.Sum256([]byte(key))
	return providerStatePath(fmt.Sprintf(".provider-%x.jwt", sum[:8]))
}

// readProviderClientKeySeed loads the Ed25519 seed for the provider
// client's long-lived identity key from `~/.urnetwork/.provider.key`.
// Returns (nil, nil) when the file does not exist — a fresh install.
// The file is the raw 32-byte seed; no encoding.
func readProviderClientKeySeed() ([]byte, error) {
	p, err := providerStatePath(".provider.key")
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// writeProviderClientKeySeed persists the Ed25519 seed to
// `~/.urnetwork/.provider.key` with 0600 permissions (sensitive
// material — anyone with this file can impersonate the provider
// against the platform identity layer).
func writeProviderClientKeySeed(seed []byte) error {
	p, err := providerStatePath(".provider.key")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	return os.WriteFile(p, seed, 0600)
}

// readProviderTlsCertAndKey loads the sequence-level TLS server cert
// chain and matching private key from `~/.urnetwork/.provider.cert`
// (PEM, leaf first, possibly chained) and the private key from the
// same file (the PEM blocks are concatenated: cert blocks first,
// then a single `PRIVATE KEY` block). Returns (nil, nil, nil) when
// the file does not exist.
func readProviderTlsCertAndKey() (certPem []byte, keyPem []byte, returnErr error) {
	p, err := providerStatePath(".provider.cert")
	if err != nil {
		return nil, nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	// Split into cert blocks and the private key block.
	rest := b
	for {
		block, next := pem.Decode(rest)
		if block == nil {
			break
		}
		// re-encode the block so the output is canonical PEM (one
		// trailing newline per block).
		blockPem := pem.EncodeToMemory(block)
		if block.Type == "CERTIFICATE" {
			certPem = append(certPem, blockPem...)
		} else {
			// First non-cert block (typically `PRIVATE KEY` or
			// `EC PRIVATE KEY`) is treated as the key. Stop after
			// the first key block.
			keyPem = blockPem
			break
		}
		rest = next
	}
	return certPem, keyPem, nil
}

// writeProviderTlsCertAndKey persists the sequence-level TLS server
// cert and private key to `~/.urnetwork/.provider.cert` with 0600
// permissions. The cert blocks are written first, then the private
// key block, so the on-disk file is a self-contained PEM bundle.
func writeProviderTlsCertAndKey(certPem, keyPem []byte) error {
	p, err := providerStatePath(".provider.cert")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	out := make([]byte, 0, len(certPem)+len(keyPem))
	out = append(out, certPem...)
	out = append(out, keyPem...)
	return os.WriteFile(p, out, 0600)
}

type Status struct {
}

func (self *Status) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	type WarpStatusResult struct {
		Version       string `json:"version,omitempty"`
		ConfigVersion string `json:"config_version,omitempty"`
		Status        string `json:"status"`
		ClientAddress string `json:"client_address,omitempty"`
		Host          string `json:"host"`
	}

	result := &WarpStatusResult{
		Version: RequireVersion(),
		// ConfigVersion: RequireConfigVersion(),
		Status: "ok",
		Host:   RequireHost(),
	}

	responseJson, err := json.Marshal(result)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJson)
}

func Host() (string, error) {
	host := os.Getenv("WARP_HOST")
	if host != "" {
		return host, nil
	}
	host, err := os.Hostname()
	if err == nil {
		return host, nil
	}
	return "", errors.New("WARP_HOST not set")
}

func RequireHost() string {
	host, err := Host()
	if err != nil {
		panic(err)
	}
	return host
}

func RequireVersion() string {
	if version := os.Getenv("WARP_VERSION"); version != "" {
		return version
	}
	return Version
}

func proxyAuthAdd(opts docopt.Opts) {
	proxyConfig := readProxyConfig()

	key, _ := opts.String("key")
	user, _ := opts.String("proxy_user")
	password, _ := opts.String("proxy_password")

	if proxyConfig.Auths == nil {
		proxyConfig.Auths = map[string]*ProxyAuth{}
	}

	if _, ok := proxyConfig.Auths[key]; ok {
		if force, _ := opts.Bool("-f"); !force {
			fmt.Printf("auth key \"%s\" exists. Overwrite? [yN]\n", key)

			reader := bufio.NewReader(os.Stdin)
			confirm, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
				return
			}
		}
	}

	proxyConfig.Auths[key] = &ProxyAuth{
		User:     user,
		Password: password,
	}

	writeProxyConfig(proxyConfig)
}

func proxyAuthRemove(opts docopt.Opts) {
	proxyConfig := readProxyConfig()

	if all, _ := opts.Bool("--all"); all {
		clear(proxyConfig.Auths)
	} else {

		key, _ := opts.String("key")

		if proxyConfig.Auths == nil {
			proxyConfig.Auths = map[string]*ProxyAuth{}
		}

		delete(proxyConfig.Auths, key)
	}

	writeProxyConfig(proxyConfig)
}

func proxyAdd(opts docopt.Opts) {
	proxyConfig := readProxyConfig()

	allKeyAddress := []string{}
	if allKeyAddressAny, ok := opts["<key_address>"]; ok {
		allKeyAddress = append(allKeyAddress, allKeyAddressAny.([]string)...)
	}
	if proxyPath, _ := opts.String("--proxy_file"); proxyPath != "" {
		b, err := os.ReadFile(proxyPath)
		if err != nil {
			panic(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && line[0] != '#' {
				allKeyAddress = append(allKeyAddress, line)
			}
		}
	}

	if proxyConfig.Servers == nil {
		proxyConfig.Servers = map[string]string{}
	}

	for _, keyAddress := range allKeyAddress {
		var key string
		var proxyAddress string
		i := strings.Index(keyAddress, "@")
		if 0 <= i {
			key = keyAddress[:i]
			proxyAddress = keyAddress[i+1:]
		} else {
			key = ""
			proxyAddress = keyAddress
		}

		address, user, password := parseProxyAddress(proxyAddress)
		if proxyConfig.Auths != nil {
			proxyAuth, ok := proxyConfig.Auths[key]
			if ok {
				user = proxyAuth.User
				password = proxyAuth.Password
			}
		}

		if currentKey, ok := proxyConfig.Servers[proxyAddress]; ok && currentKey != key {
			if force, _ := opts.Bool("-f"); !force {
				fmt.Printf(
					"server %s (%s/%s) exists with different key. Change key? [yN]\n",
					address,
					obfuscateUser(user),
					obfuscatePassword(password),
				)

				reader := bufio.NewReader(os.Stdin)
				confirm, _ := reader.ReadString('\n')
				if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
					return
				}
			}
		}

		fmt.Printf(
			"added server %s (%s/%s)\n",
			address,
			obfuscateUser(user),
			obfuscatePassword(password),
		)

		proxyConfig.Servers[proxyAddress] = key
	}

	writeProxyConfig(proxyConfig)
}

func proxyRemove(opts docopt.Opts) {
	proxyConfig := readProxyConfig()

	if all, _ := opts.Bool("--all"); all {
		clear(proxyConfig.Servers)
	} else {

		allKeyAddress := []string{}
		if allKeyAddressAny, ok := opts["<key_address>"]; ok {
			allKeyAddress = append(allKeyAddress, allKeyAddressAny.([]string)...)
		}

		if proxyConfig.Servers == nil {
			proxyConfig.Servers = map[string]string{}
		}

		for _, keyAddress := range allKeyAddress {
			var key string
			var address string
			i := strings.Index(keyAddress, "@")
			if 0 <= i {
				key = keyAddress[:i]
				address = keyAddress[i+1:]
			} else {
				key = ""
				address = keyAddress
			}

			if key == "" || proxyConfig.Servers[address] == key {
				delete(proxyConfig.Servers, address)
			}
		}
	}

	writeProxyConfig(proxyConfig)
}

type ProxyConfig struct {
	Auths map[string]*ProxyAuth `json:"auths"`
	// TODO is there a use case for multiple keys to the same address?
	// address -> key
	Servers map[string]string `json:"servers"`
}

type ProxyAuth struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

func readProxySettings() []*connect.ProxySettings {
	proxyConfig := readProxyConfig()

	if proxyConfig.Servers == nil {
		return nil
	}

	var allProxySettings []*connect.ProxySettings
	for proxyAddress, key := range proxyConfig.Servers {
		address, user, password := parseProxyAddress(proxyAddress)
		proxySettings := &connect.ProxySettings{
			Network: "tcp",
			Address: address,
		}
		if user != "" || password != "" {
			proxySettings.Auth = &proxy.Auth{
				User:     user,
				Password: password,
			}
		}
		if proxyConfig.Auths != nil {
			proxyAuth, ok := proxyConfig.Auths[key]
			if ok {
				proxySettings.Auth = &proxy.Auth{
					User:     proxyAuth.User,
					Password: proxyAuth.Password,
				}
			}
		}
		allProxySettings = append(allProxySettings, proxySettings)
	}

	return allProxySettings
}

func parseProxyAddress(proxyAddress string) (address string, user string, password string) {
	r := regexp.MustCompile("^(.*:\\d*):([^:]*):([^:]*)$")
	groups := r.FindStringSubmatch(proxyAddress)
	if groups != nil {
		address = groups[1]
		user = groups[2]
		password = groups[3]
		return
	}
	// assume host:port
	address = proxyAddress
	return
}

func obfuscateUser(user string) string {
	if user == "" {
		return "<no user>"
	} else if len(user) < 6 {
		return "***"
	} else {
		return fmt.Sprintf("%s***%s", user[:2], user[len(user)-2:])
	}
}

func obfuscatePassword(password string) string {
	if password == "" {
		return "<no password>"
	} else if len(password) < 6 {
		return "***"
	} else {
		return fmt.Sprintf("%s***%s", password[:2], password[len(password)-2:])
	}
}

func readProxyConfig() *ProxyConfig {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	urNetworkDir := filepath.Join(home, ".urnetwork")
	proxyPath := filepath.Join(urNetworkDir, "proxy")

	if _, err := os.Stat(proxyPath); errors.Is(err, os.ErrNotExist) {
		return &ProxyConfig{}
	}

	b, err := os.ReadFile(proxyPath)
	if err != nil {
		panic(err)
	}

	var proxyConfig ProxyConfig
	err = json.Unmarshal(b, &proxyConfig)
	if err != nil {
		panic(err)
	}
	return &proxyConfig
}

func writeProxyConfig(proxyConfig *ProxyConfig) {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	urNetworkDir := filepath.Join(home, ".urnetwork")
	proxyPath := filepath.Join(urNetworkDir, "proxy")

	b, err := json.Marshal(proxyConfig)
	if err != nil {
		panic(err)
	}

	err = os.WriteFile(proxyPath, b, 0700)
	if err != nil {
		panic(err)
	}
}
