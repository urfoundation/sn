package miner

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/proxy"
	"golang.org/x/term"

	"github.com/docopt/docopt-go"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
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
}

// mainUsage returns the docopt usage string. Package-level (rather than
// inline in `main`) so tests can parse argv against the real usage.
// Docopt needs two spaces between an option declaration and its description.
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
		[--close-report-domain=<path>]
		[--whole-work-capture=<path> --whole-work-capture-sha256=<hash>]
		[--require-whole-work-capture]
		[--original-contract-capture=<path> --original-contract-capture-sha256=<hash>]
		[--require-original-contract-capture]
		[--allow-client-registration | --adopt-legacy-provider-key]
        [--api_url=<api_url>]
        [--connect_url=<connect_url>]
        [--wallet=<coldkey_ss58> [--provider-jwt=<path>] [--wallet-from-epoch=<epoch> --wallet-through-epoch=<epoch>] [--coldkey_seed_file=<path> | --message=<text> --signature=<hex>]]
		[--test-egress-source-ip=<source_ip>]
        [--max-memory=<mem>]
        [-v...]
    provider auth-provide ([<auth_code>] | --user_auth=<user_auth> [--password=<password>]) [-f]
		[--close-report-domain=<path>]
		[--whole-work-capture=<path> --whole-work-capture-sha256=<hash>]
		[--require-whole-work-capture]
		[--original-contract-capture=<path> --original-contract-capture-sha256=<hash>]
		[--require-original-contract-capture]
		[--allow-client-registration | --adopt-legacy-provider-key]
    	[--port=<port>]
        [--api_url=<api_url>]
        [--connect_url=<connect_url>]
        [--wallet=<coldkey_ss58> [--provider-jwt=<path>] [--wallet-from-epoch=<epoch> --wallet-through-epoch=<epoch>] [--coldkey_seed_file=<path> | --message=<text> --signature=<hex>]]
		[--test-egress-source-ip=<source_ip>]
        [--max-memory=<mem>]
        [-v...]
    provider wallet set <coldkey_ss58> [--provider-jwt=<path> | --legacy-network-wallet] [--wallet-from-epoch=<epoch> --wallet-through-epoch=<epoch>] [--coldkey_seed_file=<path> | --message=<text> --signature=<hex>]
        [--api_url=<api_url>]
        [-v...]
    provider wallet challenge <coldkey_ss58> [--provider-jwt=<path> | --legacy-network-wallet] [--wallet-from-epoch=<epoch> --wallet-through-epoch=<epoch>]
        [--api_url=<api_url>]
        [-v...]
    provider claim [--provider-jwt=<path> | --legacy-coldkey=<coldkey_ss58>] [--epoch=<epoch>] [--rpc=<rpc_url>]... [--key_file=<key_file>] [--dry-run]
        [--api_url=<api_url>]
        [-v...]
    provider claim-daemon --config=<path>
        [--durable-volumes=<path> --durable-volumes-sha256=<hash>]
        [-v...]
    provider fleet manifest --manifest=<path>
        [-v...]
    provider fleet register --manifest=<path> --hotkey_seed_file=<path> --coldkey_seed_file=<path> --substrate=<ws_url>...
        [--burn_limit_rao=<n>] [--fee_limit_rao=<n>] [--apply | --dry-run]
        [--provisional-runtime-compatibility=<profile> --runtime-observation-dir=<path>]
        [--mainnet-runtime-authority=<path> --mainnet-runtime-authority-sha256=<hex>]
        [--durable-volumes=<path> --durable-volumes-sha256=<hash>]
        [-v...]
    provider fleet publish --manifest=<path> --substrate=<ws_url>... --hotkey_seed_file=<path>
        [--provisional-runtime-compatibility=<profile> --runtime-observation-dir=<path>]
        [--mainnet-runtime-authority=<path> --mainnet-runtime-authority-sha256=<hex>]
        [--durable-volumes=<path> --durable-volumes-sha256=<hash>]
        [-v...]
    provider fleet bind --manifest=<path> --client_id=<hex> --client_seed_file=<path> --hotkey_seed_file=<path> --valid_from_epoch=<e> --valid_to_epoch=<e> --rpc=<rpc_url>... --relayer_key_file=<path> [--dry-run]
        [--mainnet-runtime-authority=<path> --mainnet-runtime-authority-sha256=<hex>]
        [--durable-volumes=<path> --durable-volumes-sha256=<hash>]
        [-v...]
    provider fleet status --manifest=<path> --client_id=<hex> --substrate=<ws_url>... --rpc=<rpc_url>...
        [--provisional-runtime-compatibility=<profile> --runtime-observation-dir=<path>]
        [--mainnet-runtime-authority=<path> --mainnet-runtime-authority-sha256=<hex>]
        [--durable-volumes=<path> --durable-volumes-sha256=<hash>]
        [-v...]
    provider fleet revoke --manifest=<path> --client_id=<hex> --client_seed_file=<path> --effective_epoch=<e> --rpc=<rpc_url>... --relayer_key_file=<path> [--dry-run]
        [--mainnet-runtime-authority=<path> --mainnet-runtime-authority-sha256=<hex>]
        [--durable-volumes=<path> --durable-volumes-sha256=<hash>]
        [-v...]
    provider proxy auth add [<key>] <proxy_user> <proxy_password> [-f]
    provider proxy auth remove [<key>] [--all]
    provider proxy add [<key_address>...] [--proxy_file=<proxy_file>] [-f]
    provider proxy remove [<key_address>...] [--all]
    provider choose_network <api_url> <connect_url>
    provider choose_network --reset
    provider choose_network --show

Options:
	--close-report-domain=<path>       Optional original client-key policy domain for signed close evidence; absence or refusal leaves evidence unknown without stopping providing.
	--whole-work-capture=<path>        Exact public launch profile for independently signed whole-work requests and retained private outboxes.
	--whole-work-capture-sha256=<hash>  Independently reviewed sha256: digest of that original launch profile.
	--require-whole-work-capture       Refuse startup without the complete original capture profile; never allocate replacement client keys.
	--original-contract-capture=<path>  Exact approved original request/admission source profile with prepared private custody.
	--original-contract-capture-sha256=<hash>  Independently reviewed sha256: digest of the original contract source profile.
	--require-original-contract-capture  Refuse startup without the complete original contract source profile and retained provider identity.
    --durable-volumes=<path>          Exact external storage declaration; fleet writes require the owner-local schema.
    --durable-volumes-sha256=<hash>   Reviewed sha256: digest; claim daemons require the daemon-volume schema.
    -h --help                        Show this help and exit.
    --version                        Show version.
    -v                               Enable verbose mode. Repeat for higher levels:
                                     one -v means level 1, -vv means level 2, and so on.
    -f                               Force overwrite the JWT token store file or proxy value, if exists.
                                     By default, existing values will not be overwritten.
    --api_url=<api_url>              Specify a custom API URL to use.
	--allow-client-registration       Explicitly permit a new durable provider client operation; never replaces retained identity.
	--adopt-legacy-provider-key       Assert the retained legacy provider key is original; refresh only, no new client allocation.
	--config=<path>                    Strict release-1.0 daemon/component configuration.
    --connect_url=<connect_url>      Specify a custom connect URL to use.
    <api_url>                        API URL to save (https://, or http:// only for an explicit loopback host).
    <connect_url>                    Connect URL to save (wss://, or ws:// only for an explicit loopback host).
    --reset                          With choose_network, clear the saved network and revert to the main network.
    --show                           With choose_network, print the network currently in effect and exit.
    --user_auth=<user_auth>	         Login with a username.
    --password=<password>            Login with a password. If --user_auth is used, you will be prompted for your
    				                 password anyways, if you don't specify it using this option.
    -p --port=<port>                 Status server port [default: 0].
    --max-memory=<mem>               Set the maximum amount of memory in bytes, or the suffixes b, kib, mib, gib may be used [This is a soft limit].
    --wallet-from-epoch=<epoch>     First prospective earning epoch in the signed provider mapping.
    --wallet-through-epoch=<epoch>  Last inclusive earning epoch; at most 65536 epochs.
                                     Omit both to use the next epoch through the finite 65536-epoch window.
    --provider-jwt=<path>           Select one retained provider JWT; default: .provider.jwt in the provider state directory.
    --legacy-network-wallet         Explicit network-wallet compatibility; never selects a provider wallet.
    --legacy-coldkey=<coldkey_ss58>   Read a legacy network-only proof for this original committed coldkey.
                                     Uses the network JWT; never substitutes the current network wallet.
    --wallet=<coldkey_ss58>          Also set the subnet claim wallet at startup, same as provider wallet set.
                                     A failure is logged and does not block providing.
    --coldkey_seed_file=<path>       With --wallet / wallet set: the coldkey's 32-byte sr25519 seed (raw, or 64 hex
                                     chars with an optional 0x prefix) in a private file that is never created here.
                                     The CLI fetches the wallet challenge and signs it, proving the coldkey; refused
                                     unless the seed derives <coldkey_ss58>. The seed never leaves the host.
    --message=<text>                 With --wallet / wallet set: the challenge printed by "provider wallet challenge",
                                     signed elsewhere (a literal \n stands for a newline). Needs --signature.
    --signature=<hex>                The coldkey's 64-byte sr25519 signature over --message, hex (0x optional), made
                                     in the "substrate" signing context over the exact UTF-8 text (LF line endings,
                                     no trailing newline) or over that text wrapped in <Bytes>...</Bytes> (what a
                                     Polkadot extension signRaw of type "bytes" signs). Without --coldkey_seed_file
                                     or --message/--signature the set is sent unsigned, which the main network refuses.
                                     With fleet register: the same seed grammar for the fleet coldkey that signs
                                     register_limit (the hotkey's owner); keep it off the mining hosts.
    --burn_limit_rao=<n>             fleet register: maximum registration burn in rao passed to register_limit;
                                     the runtime rejects a higher live burn. Omitted: the burn observed at the read.
    --fee_limit_rao=<n>              fleet register: maximum native transaction fee in rao, checked with
                                     payment_queryInfo before broadcast [default: 10000000].
    --apply                          fleet register: sign, journal and broadcast. Without it the command is a dry
                                     run that reads the live burn economics and quotes the signed extrinsic.
	--test-egress-source-ip=<source_ip>  Integration-harness-only IPv4 loopback source bound to both
	                                     platform control and provider exit sockets.
    <coldkey_ss58>                   Subnet claim wallet: an ss58 coldkey address (prefix 42).
    --epoch=<epoch>                  Epoch to fetch the subnet pool claim for. Defaults to the last
                                     finalized epoch, which is the epoch before the current one.
    --rpc=<rpc_url>                  EVM json-rpc endpoint used to check the payout root on-chain.
                                     May be repeated; endpoints are tried in order until one answers.
	--substrate=<ws_url>               Substrate websocket endpoint; repeatable ordered failover.
	--provisional-runtime-compatibility=<profile>  Explicit testnet consumed-runtime profile.
	--runtime-observation-dir=<path>   Absolute durable directory required with the provisional profile.
	--mainnet-runtime-authority=<path>  Independently reviewed exact mainnet fleet runtime authority document.
	--mainnet-runtime-authority-sha256=<hex>  Approved SHA-256 of the exact authority bytes (64 lowercase hex digits).
	--manifest=<path>                  Canonical urnetwork-fleet-manifest-v1 JSON file.
	--client_id=<hex>                  Stable 16-byte UR client identity from the fleet manifest.
	--client_seed_file=<path>          Raw or hex 32-byte Ed25519 client key seed.
	--hotkey_seed_file=<path>          Hex/raw 32-byte sr25519 fleet hotkey seed.
	--valid_from_epoch=<e>             First settlement epoch in which a binding is active.
	--valid_to_epoch=<e>               Last settlement epoch in which a binding is active.
	--effective_epoch=<e>              Future epoch at which a fleet revocation takes effect.
	--relayer_key_file=<path>          EVM transaction relayer key; it receives no binding ownership.
    --key_file=<key_file>            Path to a hex-encoded 32-byte secp256k1 EVM private key. When given,
                                     claim signs and submits the settlement-vault claim (via the
                                     sn/miner/onchain path) instead of only printing the calldata.
    --dry-run                        With --key_file / --relayer_key_file, stop at the eth_call preflight and
                                     send nothing; for fleet register the explicit dry run (the default).
                                     Without a key the claim command only verifies, so it has no effect.
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
// wallet / claim / fleet actions (formerly connect/provider).
func Run(args []string) {
	opts, err := docopt.ParseArgs(mainUsage(), args, RequireVersion())

	if err != nil {
		panic(err)
	}
	providing, _ := opts.Bool("provide")
	authProviding, _ := opts.Bool("auth-provide")
	if providing || authProviding {
		path, _ := opts.String("--whole-work-capture")
		digest, _ := opts.String("--whole-work-capture-sha256")
		required, _ := opts.Bool("--require-whole-work-capture")
		profile, err := ReadProviderWorkCaptureProfile(context.Background(), path, digest, required)
		allowRegistration, _ := opts.Bool("--allow-client-registration")
		if err != nil || profile != nil && allowRegistration {
			// Complete-profile refusal precedes auth-provide authentication and
			// the optional startup wallet write as well as provider allocation.
			fmt.Fprintln(os.Stderr, "whole-work launch requires its reviewed profile, original provider identity and private outbox; no provider started")
			os.Exit(1)
		}
		contractPath, _ := opts.String("--original-contract-capture")
		contractDigest, _ := opts.String("--original-contract-capture-sha256")
		contractRequired, _ := opts.Bool("--require-original-contract-capture")
		contractProfile, err := ReadProviderContractCaptureProfile(context.Background(), contractPath, contractDigest, contractRequired)
		if err != nil || contractProfile != nil && allowRegistration {
			fmt.Fprintln(os.Stderr, "original contract launch requires its approved source profile and retained provider identity; no provider started")
			os.Exit(1)
		}
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
		} else if challenge, _ := opts.Bool("challenge"); challenge {
			walletChallenge(opts)
		}
	} else if claim_, _ := opts.Bool("claim"); claim_ {
		claim(opts)
	} else if claimDaemon, _ := opts.Bool("claim-daemon"); claimDaemon {
		if err := runClaimDaemon(minerStorageContext(context.Background(), opts), fleetOpt(opts, "--config")); err != nil {
			panic(err)
		}
	} else if fleet_, _ := opts.Bool("fleet"); fleet_ {
		if err := fleetCommand(opts); err != nil {
			panic(err)
		}
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

// Authentication shares the selected state directory with provider startup and claims.
func auth(opts docopt.Opts) {
	jwtPath, err := providerStatePath("jwt")
	if err != nil {
		panic(err)
	}

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

	// auth is short lived: only an explicit --max-memory sizes it
	applyProviderProcessMemory(newProviderAuthMemoryPlan(parseProviderMaxMemory(opts)))

	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(event.Ctx())
	defer cancel()

	clientStrategy := connect.NewClientStrategyWithDefaults(ctx)
	defer clientStrategy.Close()
	api := sdk.NewApi(ctx, clientStrategy, apiUrl)
	defer func() {
		_ = api.CloseAndWait(context.Background())
	}()

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
			stdin := int(syscall.Stdin)
			var err error
			authCode, err = readAuthCode(os.Stdin, os.Stdout, term.IsTerminal(stdin), func() ([]byte, error) {
				return term.ReadPassword(stdin)
			})
			if err != nil {
				panic(err)
			}
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

// testEgressDialContext returns the narrow source-bind seam used by
// sim-testnet to run several independently attributable provider exits on one
// Linux host. Requiring an IPv4 loopback address makes the flag incapable of
// selecting a production interface or exporting traffic outside the host.
func testEgressDialContext(opts docopt.Opts) (*connect.DialContextSettings, error) {
	raw, err := opts.String("--test-egress-source-ip")
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	settings, err := testEgressDialContextForIP(raw)
	if err != nil {
		return nil, fmt.Errorf("--test-egress-source-ip must be an IPv4 loopback address")
	}
	return settings, nil
}

// applyProviderMemoryTarget installs one provider's device target (see
// newProviderMemoryPlan). A nonpositive target deliberately preserves the
// SDK's positive per-device default: replacing it with zero would make
// unrelated SOCKS-backed providers share Connect's process-wide transport
// budget.
func applyProviderMemoryTarget(
	settings *sdk.DeviceLocalSettings,
	deviceMemoryTargetByteCount connect.ByteCount,
) {
	if deviceMemoryTargetByteCount <= 0 {
		return
	}
	settings.MemoryTargetByteCount = sdk.ByteCount(deviceMemoryTargetByteCount)
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
	testEgressDialer, err := testEgressDialContext(opts)
	if err != nil {
		panic(err)
	}

	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	defer stopSignals()

	allProxySettings := readProxySettings()
	providerCount := len(allProxySettings)
	if providerCount == 0 {
		providerCount = 1
	}

	// sized per provider (provider_memory.go); an absent --max-memory has no
	// ceiling: the target is derived from the host memory
	memoryPlan := newProviderMemoryPlan(
		parseProviderMaxMemory(opts),
		hostMemoryByteCount(),
		providerCount,
	)
	applyProviderProcessMemory(memoryPlan)

	allowClientRegistration, _ := opts.Bool("--allow-client-registration")
	adoptLegacyProviderKey, _ := opts.Bool("--adopt-legacy-provider-key")
	domainPath, _ := opts.String("--close-report-domain")
	domainHash, domainErr := ReadProviderCloseReportDomain(domainPath)
	if domainErr != nil {
		fmt.Fprintf(os.Stderr, "signed close evidence unavailable: %v\n", domainErr)
	}
	settings := providerRunSettings{apiUrl: apiUrl, connectUrl: connectUrl, port: port, proxySettings: allProxySettings, memoryPlan: memoryPlan, testEgressDialer: testEgressDialer, allowClientRegistration: allowClientRegistration, adoptLegacyProviderKey: adoptLegacyProviderKey}
	settings.wallet, _ = opts.String("--wallet")
	if settings.wallet != "" {
		settings.walletProof, err = snWalletProofFromOpts(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "subnet wallet not set: %v; continuing to provide\n", err)
			settings.wallet = ""
		}
	} else if misuse := snProvideWalletMisuse(opts); misuse != "" {
		fmt.Fprintln(os.Stderr, misuse)
	}
	settings.closeReportDomainHash = domainHash
	settings.workCapturePath, _ = opts.String("--whole-work-capture")
	settings.workCaptureSha256, _ = opts.String("--whole-work-capture-sha256")
	settings.requireWorkCapture, _ = opts.Bool("--require-whole-work-capture")
	settings.contractCapturePath, _ = opts.String("--original-contract-capture")
	settings.contractCaptureSha256, _ = opts.String("--original-contract-capture-sha256")
	settings.requireContractCapture, _ = opts.Bool("--require-original-contract-capture")
	// A complete-profile launch must report refusal to its supervisor. The
	// owned run has joined every child before this process-level exit decision.
	if err := settings.run(ctx, os.Stdout); err != nil && ctx.Err() == nil && (settings.requireWorkCapture || settings.workCapturePath != "" || settings.workCaptureSha256 != "" || settings.requireContractCapture || settings.contractCapturePath != "" || settings.contractCaptureSha256 != "") {
		stopSignals()
		fmt.Fprintln(os.Stderr, "provider original capture launch refused; restore approved profiles, retained identity and prepared custody")
		os.Exit(1)
	}
}

// Each invocation owns its output, status server and provider children. The
// finite CLI setup above retains its existing output and validation behavior.
func (self providerRunSettings) run(parent context.Context, writer io.Writer) (returnErr error) {
	output, err := newProviderDiagnostics(parent, writer)
	if err != nil {
		return err
	}
	hooks, _ := parent.Value(providerDiagnosticHooksKey{}).(providerDiagnosticHooks)
	defer func() {
		closeErr := output.close()
		returnErr = errors.Join(returnErr, closeErr)
		if hooks.afterClose != nil {
			hooks.afterClose(output, closeErr)
		}
	}()
	if hooks.afterCreate != nil {
		hooks.afterCreate(output)
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if err := validateProviderRegistrationSlots(self.proxySettings); err != nil {
		output.observe(providerStartupRecoveryRequired, 0, false, err, 0, nil)
		return err
	}
	providers := self.proxySettings
	if len(providers) == 0 {
		providers = []*connect.ProxySettings{nil}
	}
	workProfile, err := ReadProviderWorkCaptureProfile(ctx, self.workCapturePath, self.workCaptureSha256, self.requireWorkCapture)
	if err != nil {
		output.observe(providerWorkCaptureRequired, 0, false, err, 0, nil)
		return err
	}
	contractProfile, err := ReadProviderContractCaptureProfile(ctx, self.contractCapturePath, self.contractCaptureSha256, self.requireContractCapture)
	if err != nil {
		output.observe(providerContractCaptureRequired, 0, false, err, 0, nil)
		return err
	}
	captureEvent := providerWorkCaptureRequired
	if contractProfile != nil {
		captureEvent = providerContractCaptureRequired
	}
	if workProfile != nil || contractProfile != nil {
		slots := make([]string, 0, len(providers))
		for _, proxy := range providers {
			slots = append(slots, providerRegistrationSlot(proxy))
		}
		if err := workProfile.validateRole(self.apiUrl, slots, self.closeReportDomainHash); err != nil {
			output.observe(providerWorkCaptureRequired, 0, false, err, 0, nil)
			return err
		}
		if err := contractProfile.validateRole(self.apiUrl, slots, self.closeReportDomainHash, workProfile); err != nil {
			output.observe(providerContractCaptureRequired, 0, false, err, 0, nil)
			return err
		}
		if self.allowClientRegistration {
			err := errors.New("original capture launch requires retained provider identity and cannot allocate registration")
			output.observe(captureEvent, 0, false, err, 0, nil)
			return err
		}
	}
	progressMembers := make([]providerProgressConfigMember, 0, len(providers))
	for _, proxy := range providers {
		progressMembers = append(progressMembers, providerProgressConfigMember{Slot: providerRegistrationSlot(proxy), ApiUrl: self.apiUrl, ConnectUrl: self.connectUrl})
	}
	progress, err := newProviderProgressOwner("standalone", progressMembers)
	if err != nil {
		return err
	}
	defer progress.close()
	status, err := newProviderStatusServer(self.port, output, cancel, progress)
	if err != nil {
		output.observe(providerStatusFailed, 0, false, err, 0, nil)
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, status.close()) }()
	keyPath, err := providerStatePath(".provider.key")
	if err != nil {
		output.observe(providerStartupRecoveryRequired, 0, false, err, 0, nil)
		return err
	}
	keyOwner, err := clientauth.OpenProviderClientKey(ctx, keyPath, clientauth.ProviderClientKeyOptions{AllowCreate: self.allowClientRegistration, AdoptLegacyKey: self.adoptLegacyProviderKey})
	if err != nil {
		if ctx.Err() == nil {
			output.observe(providerStartupRecoveryRequired, 0, false, err, 0, nil)
		}
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, keyOwner.Close()) }()

	provideWithProxy := func(index uint64, proxySettings *connect.ProxySettings) (returnErr error) {
		defer func() {
			// Registered first, this runs after every real cleanup below.
			// Keep cleanup causes even when an SDK/operation panics.
			if cause := recover(); cause != nil {
				if original, ok := cause.(error); ok {
					returnErr = errors.Join(returnErr, original)
				} else {
					returnErr = errors.Join(returnErr, errors.New("provider worker panicked"))
				}
			}
		}()
		proxyCtx, proxyCancel := context.WithCancel(ctx)
		defer proxyCancel()

		clientStrategySettings := connect.DefaultClientStrategySettings()
		clientStrategySettings.ProxySettings = proxySettings
		clientStrategySettings.DialContextSettings = self.testEgressDialer
		networkSpace := sdk.NewNetworkSpaceWithUrls(proxyCtx, self.apiUrl, self.connectUrl, clientStrategySettings)
		defer networkSpace.Close()
		api := networkSpace.GetApi()
		// Authentication refusal also joins the API before the shared key owner
		// is released; later callback cleanup may join this same owner again.
		defer func() {
			returnErr = errors.Join(returnErr, api.CloseAndWait(context.Background()))
			if hook, ok := ctx.Value(providerRegistrationHooksKey{}).(providerRegistrationHooks); ok && hook.afterApiJoined != nil {
				hook.afterApiJoined()
			}
		}()

		networkJwtPath, err := providerStatePath("jwt")
		if err != nil {
			panic(err)
		}
		clientJwtPath, err := providerClientJwtPath(proxySettings)
		if err != nil {
			panic(err)
		}

		seed := keyOwner.Seed()
		byClientJwt, clientId, err := authenticateProvider(proxyCtx, api, networkJwtPath, clientJwtPath, keyOwner, providerRegistrationSlot(proxySettings), self.allowClientRegistration, output, index)
		if err != nil {
			if proxyCtx.Err() == nil {
				output.observe(providerStartupRecoveryRequired, index, true, err, 0, nil)
			}
			return err
		}

		// Each authenticated slot owns its optional wallet set. A fresh or
		// proxy provider never borrows the direct provider's credential.
		if self.wallet != "" {
			proof := snWalletProof{}
			if self.walletProof != nil {
				proof = *self.walletProof
			}
			if proof.credentials.jwtFile == "" {
				proof.credentials.jwtFile = clientJwtPath
			}
			proof.credentials.original = byClientJwt
			walletStrategy := connect.NewClientStrategy(proxyCtx, clientStrategySettings)
			walletErr := snSetWallet(proxyCtx, walletStrategy, self.apiUrl, self.wallet, &proof)
			walletStrategy.Close()
			if hooks, ok := proxyCtx.Value(providerRegistrationHooksKey{}).(providerRegistrationHooks); ok && hooks.afterWallet != nil {
				if err := hooks.afterWallet(walletErr); err != nil {
					return errors.Join(walletErr, err)
				}
			}
			if walletErr != nil {
				fmt.Fprintf(os.Stderr, "provider %d subnet wallet not set: %v; continuing to provide\n", index, walletErr)
			}
		}

		callbacks := &providerAuthenticationCallbacks{diagnostics: output, provider: index, clientJwtPath: clientJwtPath, networkJwtPath: networkJwtPath, cancel: cancel, custody: keyOwner}
		boundRefresh := &providerBoundRefresh{original: byClientJwt, callbacks: callbacks}
		refreshSub := api.AddJwtRefreshListener(boundRefresh)
		integritySub := api.AddClientRefreshIntegrityListener(boundRefresh)
		logoutSub := api.AddAuthLogoutListener(callbacks)
		defer func() {
			// Callbacks can cancel, but only this enclosing owner joins them.
			returnErr = errors.Join(returnErr, api.CloseAndWait(context.Background()))
			refreshSub.Close()
			integritySub.Close()
			logoutSub.Close()
			returnErr = errors.Join(returnErr, callbacks.failure())
		}()

		certPem, keyPem, _ := readProviderTlsCertAndKey()
		extenderKeySeed, _ := readProviderExtenderKeySeed()
		settings := ProviderDeviceSettings(self.closeReportDomainHash)
		settings.KeyMaterial = sdk.NewDeviceLocalKeyMaterial(seed, certPem, keyPem)
		// the extender identity of this provider (connect/EXTENDER.md B1, G2).
		// The space keeps no local state, so the seed lives here: without it
		// the role would activate under a new key every launch and the
		// operator would revoke the old one as fast as it publishes it.
		settings.KeyMaterial.SetExtenderKeySeed(extenderKeySeed)
		applyProviderMemoryTarget(settings, self.memoryPlan.DeviceMemoryTargetByteCount)
		settings.ProviderDialContextSettings = self.testEgressDialer
		device, err := newProviderDeviceLocal(
			proxyCtx,
			networkSpace,
			clientStrategySettings,
			byClientJwt,
			fmt.Sprintf("provider %s %s", runtime.GOOS, RequireVersion()),
			settings,
			workProfile,
			providerRegistrationSlot(proxySettings),
			clientId,
			contractProfile,
		)
		if err != nil {
			if workProfile != nil || contractProfile != nil {
				output.observe(captureEvent, index, true, err, 0, nil)
			}
			return err
		}
		defer func() {
			returnErr = errors.Join(returnErr, device.CloseAndWait(context.Background()))
		}()

		// Always-on public mode includes network and friends/family service,
		// matching the SDK's hierarchical provide contract.
		device.SetProvideControlMode(sdk.ProvideControlModeAlways)
		progressGeneration, err := progress.attach(proxyCtx, providerRegistrationSlot(proxySettings), device)
		if err != nil {
			return err
		}
		defer progress.retire(providerRegistrationSlot(proxySettings), progressGeneration)

		keyMaterial := device.GetKeyMaterial()
		if !bytes.Equal(keyMaterial.GetClientKeySeed(), seed) {
			return errors.New("provider device changed its retained registration key")
		}
		persistProviderAuxiliaryKeyMaterial(output, index, keyMaterial)
		observeProviderExtenderIdentity(output, index, keyMaterial.GetExtenderKeySeed())
		// one line per change, so the activation prints once it settles and
		// nothing repeats while it holds (connect/EXTENDER.md F3, G3)
		extenderStatusSub := device.AddExtenderProvideStatusChangeListener(
			newProviderExtenderStatusListener(output, index))
		defer extenderStatusSub.Close()

		select {
		case <-proxyCtx.Done():
		}
		return nil
	}

	var wg sync.WaitGroup
	results := make(chan error, len(providers))
	for index, proxySettings := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var result error
			defer func() {
				if cause := recover(); cause != nil {
					if original, ok := cause.(error); ok {
						result = original
					} else {
						result = errors.New("provider worker panicked")
					}
				}
				if result != nil {
					if workProfile != nil || contractProfile != nil {
						// A complete launch cannot leave its other declared members
						// running after one actual owner failed admission.
						cancel()
					}
					output.observe(providerWorkerFailed, uint64(index), true, result, 0, nil)
				}
				results <- result
			}()
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(index) * 100 * time.Millisecond):
			}
			if ctx.Err() != nil {
				return
			}
			output.observe(providerStarted, uint64(index), true, nil, 0, nil)
			result = provideWithProxy(uint64(index), proxySettings)
		}()
	}
	wg.Wait()
	cancel()
	close(results)
	for result := range results {
		returnErr = errors.Join(returnErr, result)
	}
	return returnErr
}

// Resolves the shared auth, network, proxy and provider state directory.
// URNETWORK_STATE_DIR overrides ~/.urnetwork; resolution creates no files.
func providerStateDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("URNETWORK_STATE_DIR")); override != "" {
		absolute, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		return filepath.Clean(absolute), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".urnetwork"), nil
}

// Resolves one file in the selected state directory without creating it.
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

// readProviderExtenderKeySeed loads the ed25519 seed of this provider's
// extender identity from `~/.urnetwork/.provider.extender.key`
// (connect/EXTENDER.md B1). Returns (nil, nil) when the file does not exist --
// a provider that has not been an extender yet. The file is the raw 32-byte
// seed; no encoding, exactly as the client key seed beside it.
func readProviderExtenderKeySeed() ([]byte, error) {
	p, err := providerStatePath(".provider.extender.key")
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// writeProviderExtenderKeySeed persists the extender identity seed to
// `~/.urnetwork/.provider.extender.key` with 0600 permissions (sensitive
// material -- anyone with this file can present this host's extender identity
// to the mesh and to the operator).
func writeProviderExtenderKeySeed(extenderKeySeed []byte) error {
	p, err := providerStatePath(".provider.extender.key")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	return os.WriteFile(p, extenderKeySeed, 0600)
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
	diagnostics *providerDiagnostics
	progress    *providerProgressOwner
}

func (self *Status) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/provider-progress" {
		serveProviderProgress(w, r, self.progress)
		return
	}
	type WarpStatusResult struct {
		Version       string                    `json:"version,omitempty"`
		ConfigVersion string                    `json:"config_version,omitempty"`
		Status        string                    `json:"status"`
		ClientAddress string                    `json:"client_address,omitempty"`
		Host          string                    `json:"host"`
		Diagnostics   *providerDiagnosticStatus `json:"diagnostics,omitempty"`
	}

	result := &WarpStatusResult{
		Version: RequireVersion(),
		// ConfigVersion: RequireConfigVersion(),
		Status:      "ok",
		Host:        RequireHost(),
		Diagnostics: self.diagnostics.snapshot(),
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

// Proxy selection follows the same state directory as its provider credentials.
func readProxyConfig() *ProxyConfig {
	proxyPath, err := providerStatePath("proxy")
	if err != nil {
		panic(err)
	}

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

// Updates only the selected directory's proxy configuration.
func writeProxyConfig(proxyConfig *ProxyConfig) {
	proxyPath, err := providerStatePath("proxy")
	if err != nil {
		panic(err)
	}

	b, err := json.Marshal(proxyConfig)
	if err != nil {
		panic(err)
	}

	err = os.WriteFile(proxyPath, b, 0700)
	if err != nil {
		panic(err)
	}
}
