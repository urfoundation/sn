package miner

// The operator list commands (docs/OPERATOR-DISCOVERY.md section 5.1):
// "provide --all-operators" runs the supervisor of operators_supervisor.go,
// "auth --operator" authenticates to one listed operator, and "operators"
// prints the list with each operator's credential state. Every operator keeps
// its own state directory, operatorlist.DomainStateDir below the provider
// state directory, and the list is cached as operators.yml in the provider
// state directory, so these commands work while ur.xyz is unreachable.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/docopt/docopt-go"

	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/hotkeyauth"
	"github.com/urfoundation/sn/v2026/operatorlist"
)

const defaultOperatorsRefresh = time.Hour

// Single-operator provide flags that --all-operators replaces (the urls and
// the provider jwt come from each operator's directory) or that stay
// single-operator: the wallet set, captures, close reports and test egress.
// A new operator directory has no legacy key to adopt.
var allOperatorsExcludedOptions = []string{
	"--api_url",
	"--connect_url",
	"--provider-jwt",
	"--wallet",
	"--wallet-from-epoch",
	"--wallet-through-epoch",
	"--coldkey_seed_file",
	"--message",
	"--signature",
	"--test-egress-source-ip",
	"--close-report-domain",
	"--whole-work-capture",
	"--whole-work-capture-sha256",
	"--require-whole-work-capture",
	"--original-contract-capture",
	"--original-contract-capture-sha256",
	"--require-original-contract-capture",
	"--adopt-legacy-provider-key",
}

// provide and auth with every option of the main table, so a refusal can name
// the options that collide instead of printing the whole usage.
func operatorArgsUsage() string {
	usage := mainUsage()
	return "Usage:\n" +
		"    provider provide [options] [-v...]\n" +
		"    provider auth [<auth_code>] [options] [-v...]\n" +
		usage[strings.Index(usage, "\nOptions:"):]
}

// A refusal naming the operator list options used where the contract
// excludes them, or nil. Anything this grammar cannot parse is left to the
// real one.
func operatorArgsError(args []string) error {
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler, SkipHelpFlags: true}
	opts, err := parser.ParseArgs(operatorArgsUsage(), args, "")
	if err != nil {
		return nil
	}
	given := func(names ...string) []string {
		var present []string
		for _, name := range names {
			if value := opts[name]; value != nil && value != false {
				present = append(present, name)
			}
		}
		return present
	}
	if provide_, _ := opts.Bool("provide"); provide_ {
		if allOperators, _ := opts.Bool("--all-operators"); !allOperators {
			if present := given("--operators-url", "--operators-refresh", "--auto-register", "--hotkey_seed_file"); 0 < len(present) {
				return fmt.Errorf("provide: %s only applies with --all-operators", strings.Join(present, ", "))
			}
			return nil
		}
		if present := given(allOperatorsExcludedOptions...); 0 < len(present) {
			return fmt.Errorf(
				"provide --all-operators excludes %s: each operator's urls and jwt come from the operator list and its directory, and the wallet, capture, close-report and test egress settings stay single-operator",
				strings.Join(present, ", "),
			)
		}
		if 0 < len(given("--auto-register")) && len(given("--hotkey_seed_file")) == 0 {
			return errors.New("provide --all-operators --auto-register needs --hotkey_seed_file=<path>")
		}
		return nil
	}
	if len(given("--operator")) == 0 {
		if present := given("--operators-url", "--hotkey_seed_file"); 0 < len(present) {
			return fmt.Errorf("auth: %s only applies with --operator=<domain>", strings.Join(present, ", "))
		}
		return nil
	}
	if present := given("--api_url", "--max-memory"); 0 < len(present) {
		return fmt.Errorf("auth --operator excludes %s: the operator's api url comes from the operator list", strings.Join(present, ", "))
	}
	return nil
}

func operatorListCachePath(base string) string {
	return filepath.Join(base, "operators.yml")
}

// One fetch of the list, or the cached copy when the fetch fails.
func loadOperatorList(ctx context.Context, opts docopt.Opts, base string) (*operatorlist.Snapshot, error) {
	operatorsUrl, _ := opts.String("--operators-url")
	return operatorlist.Load(ctx, operatorlist.RefreshSettings{Url: operatorsUrl, CachePath: operatorListCachePath(base)})
}

func operatorJwtPath(base string, domain string) string {
	return filepath.Join(operatorlist.DomainStateDir(base, domain), "jwt")
}

// Never creates a hotkey: a missing or unsafe seed file is an error naming
// its path.
func loadOperatorHotkey(path string) (*crv4.Keypair, error) {
	seed, err := crv4.LoadSeedFile(path)
	if err != nil {
		return nil, fmt.Errorf("hotkey seed file %s: %w", path, err)
	}
	return crv4.KeypairFromSeed(seed)
}

// Reads the --all-operators options. The hotkey is loaded here, so a bad seed
// file stops startup before any child runs.
func newOperatorSupervisorSettings(opts docopt.Opts) (operatorSupervisorSettings, operatorlist.RefreshSettings, error) {
	base, err := providerStateDir()
	if err != nil {
		return operatorSupervisorSettings{}, operatorlist.RefreshSettings{}, err
	}
	refresh := defaultOperatorsRefresh
	if raw, err := opts.String("--operators-refresh"); err == nil {
		refresh, err = time.ParseDuration(raw)
		if err != nil || refresh < time.Minute {
			return operatorSupervisorSettings{}, operatorlist.RefreshSettings{}, fmt.Errorf("--operators-refresh=%s must be a Go duration of at least 1m", raw)
		}
	}
	operatorsUrl, _ := opts.String("--operators-url")
	settings := operatorSupervisorSettings{baseStateDir: base}
	// as for one operator, a nonpositive limit is no limit
	if raw, err := opts.String("--max-memory"); err == nil {
		settings.maxMemory, err = connect.ParseByteCount(raw)
		if err != nil {
			return operatorSupervisorSettings{}, operatorlist.RefreshSettings{}, fmt.Errorf("--max-memory=%s is not a byte count such as 2gib", raw)
		}
	}
	if settings.port, err = opts.Int("--port"); err != nil {
		return operatorSupervisorSettings{}, operatorlist.RefreshSettings{}, fmt.Errorf("--port: %w", err)
	}
	settings.verbosity, _ = opts["-v"].(int)
	settings.allowClientRegistration, _ = opts.Bool("--allow-client-registration")
	settings.autoRegister, _ = opts.Bool("--auto-register")
	if seedPath, err := opts.String("--hotkey_seed_file"); err == nil {
		if settings.hotkey, err = loadOperatorHotkey(seedPath); err != nil {
			return operatorSupervisorSettings{}, operatorlist.RefreshSettings{}, err
		}
	}
	if settings.autoRegister && settings.hotkey == nil {
		return operatorSupervisorSettings{}, operatorlist.RefreshSettings{}, errors.New("--auto-register needs --hotkey_seed_file=<path>")
	}
	if settings.executable, err = os.Executable(); err != nil {
		return operatorSupervisorSettings{}, operatorlist.RefreshSettings{}, err
	}
	listSettings := operatorlist.RefreshSettings{Url: operatorsUrl, Interval: refresh, CachePath: operatorListCachePath(base)}
	return settings, listSettings, nil
}

// "provider provide --all-operators".
func provideAllOperatorsCmd(opts docopt.Opts) {
	settings, listSettings, err := newOperatorSupervisorSettings(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "provide --all-operators: %v\n", err)
		os.Exit(1)
	}
	supervisor := newOperatorSupervisor(settings, defaultOperatorSupervisorHooks(), os.Stderr)
	status, err := supervisor.serveStatus(settings.port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "provide --all-operators: status server: %v\n", err)
		os.Exit(1)
	}
	// subscribed before any child exists, so every stop signal reaches the
	// children through the supervisor
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	ctx := context.Background()
	refresher, err := operatorlist.NewRefresher(ctx, listSettings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "provide --all-operators: %v\n", err)
		os.Exit(1)
	}
	supervisor.Run(ctx, refresher, signals)
	signal.Stop(signals)
	refresher.Close()
	if status != nil {
		_ = status.Close()
	}
}

// "provider auth --operator=<domain>".
func operatorAuthCmd(opts docopt.Opts) {
	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(event.Ctx())
	err := operatorAuth(ctx, opts, terminalLoginPrompts(), os.Stdout)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth --operator: %v\n", err)
		os.Exit(1)
	}
}

// Writes one listed operator's network jwt, from the logins auth uses against
// the operator's api url, or from a hotkey sign-in.
func operatorAuth(ctx context.Context, opts docopt.Opts, prompts loginPrompts, out io.Writer) error {
	domain, _ := opts.String("--operator")
	base, err := providerStateDir()
	if err != nil {
		return err
	}
	snapshot, err := loadOperatorList(ctx, opts, base)
	if err != nil {
		return err
	}
	operator, ok := snapshot.List.Operator(domain)
	if !ok {
		return fmt.Errorf("operator %q is not in the operator list (%s); provider operators prints the list", domain, snapshot.Sha256)
	}
	// the domain was validated by the list, so it is safe as a path
	jwtPath := operatorJwtPath(base, operator.Domain)
	if !confirmJwtOverwrite(opts, jwtPath) {
		return nil
	}
	var byJwt string
	if seedPath, err := opts.String("--hotkey_seed_file"); err == nil {
		hotkey, err := loadOperatorHotkey(seedPath)
		if err != nil {
			return err
		}
		network, err := hotkeyauth.SignIn(ctx, hotkeyauth.Settings{ApiUrl: operator.ApiUrl, Hotkey: hotkey})
		if err != nil {
			return err
		}
		if network.Created {
			fmt.Fprintf(out, "Created the hotkey's network on %s\n", operator.Domain)
		}
		byJwt = network.ByJwt
	} else if byJwt, err = networkLogin(ctx, opts, operator.ApiUrl, prompts); err != nil {
		return err
	}
	if byJwt == "" {
		return fmt.Errorf("operator %s returned no jwt", operator.Domain)
	}
	if err := clientauth.WriteToken(jwtPath, byJwt); err != nil {
		return err
	}
	fmt.Fprintf(out, "Jwt written to %s\n", jwtPath)
	if _, err := os.Stat(filepath.Join(filepath.Dir(jwtPath), ".provider.key")); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(out, "This operator has no provider client yet; its first run needs: provider provide --all-operators --allow-client-registration")
	}
	return nil
}

// "provider operators".
func operatorsCmd(opts docopt.Opts) {
	if err := printOperators(context.Background(), opts, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "operators: %v\n", err)
		os.Exit(1)
	}
}

// The list's source and digest, then each listed operator with its credential
// state and urls.
func printOperators(ctx context.Context, opts docopt.Opts, out io.Writer) error {
	base, err := providerStateDir()
	if err != nil {
		return err
	}
	snapshot, err := loadOperatorList(ctx, opts, base)
	if err != nil {
		return err
	}
	if snapshot.Cached {
		fmt.Fprintf(out, "source: cached %s, fetched %s\n", operatorListCachePath(base), snapshot.FetchedAt.UTC().Format(time.RFC3339))
	} else {
		operatorsUrl, _ := opts.String("--operators-url")
		if operatorsUrl == "" {
			operatorsUrl = operatorlist.DefaultUrl
		}
		fmt.Fprintf(out, "source: fetched %s\n", operatorsUrl)
	}
	fmt.Fprintf(out, "digest: %s\n", snapshot.Sha256)
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	awaiting := false
	for _, operator := range snapshot.List.Operators {
		state := "jwt present"
		if _, err := clientauth.ReadToken(operatorJwtPath(base, operator.Domain)); err != nil {
			state = "awaiting auth"
			awaiting = true
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", operator.Domain, state, operator.ApiUrl, operator.ConnectUrl)
	}
	if err := table.Flush(); err != nil {
		return err
	}
	if awaiting {
		fmt.Fprintln(out, "authenticate an operator awaiting auth with: provider auth --operator=<domain>")
	}
	return nil
}
