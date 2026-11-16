package miner

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/docopt/docopt-go"
)

func runCliFront(t *testing.T, args ...string) (stdout string, stderr string, rest []string, done bool, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	rest, done, code = cliFront(args, &out, &errOut)
	return out.String(), errOut.String(), rest, done, code
}

// The overview a bare `provider` or `provider --help` prints fits one screen,
// unlike the complete reference.
func TestCliOverviewFitsOneScreen(t *testing.T) {
	for _, args := range [][]string{{}, {"help"}, {"help", "help"}} {
		stdout, _, _, done, code := runCliFront(t, args...)
		if !done || code != 0 {
			t.Fatalf("%v: done=%v code=%d", args, done, code)
		}
		if stdout != cliOverview() {
			t.Fatalf("%v: not the overview", args)
		}
	}
	// -h and --help reach the parser, whose help handler answers with this
	for _, args := range [][]string{{"--help"}, {"-h"}, {"-v", "--help"}} {
		stdout := cliHelpFor(args)
		if lines := strings.Count(stdout, "\n"); 40 < lines {
			t.Fatalf("%v: overview is %d lines", args, lines)
		}
		if !strings.Contains(stdout, "provider help all") {
			t.Fatalf("%v: overview does not point to the complete reference:\n%s", args, stdout)
		}
	}
	stdout, _, _, _, _ := runCliFront(t, "help", "all")
	if !strings.Contains(stdout, mainUsage()) {
		t.Fatal("help all does not print the complete reference")
	}
}

func TestCliVersion(t *testing.T) {
	t.Setenv("WARP_VERSION", "2026.10.21-1")
	// --version prints the bare version, as docopt did, so scripts keep working
	stdout, _, _, done, code := runCliFront(t, "--version")
	if !done || code != 0 || stdout != "2026.10.21-1\n" {
		t.Fatalf("--version: done=%v code=%d %q", done, code, stdout)
	}
	for _, args := range [][]string{{"version"}, {"help", "version"}} {
		stdout, _, _, done, code := runCliFront(t, args...)
		if !done || code != 0 || !strings.HasPrefix(stdout, "provider 2026.10.21-1 ") {
			t.Fatalf("%v: done=%v code=%d %q", args, done, code, stdout)
		}
	}
}

// Each alias parses to exactly the options of the command it stands for.
func TestCliAliasesParseAsTheirCommand(t *testing.T) {
	cases := []struct {
		alias  []string
		legacy []string
	}{
		{[]string{"start"}, []string{"provide"}},
		{[]string{"start", "--allow-client-registration", "-v"}, []string{"provide", "--allow-client-registration", "-v"}},
		{[]string{"start", "--all-operators"}, []string{"provide", "--all-operators"}},
		{[]string{"login", "CODE"}, []string{"auth", "CODE"}},
		{[]string{"network"}, []string{"choose_network", "--show"}},
		{[]string{"network", "show"}, []string{"choose_network", "--show"}},
		{[]string{"network", "reset"}, []string{"choose_network", "--reset"}},
		{[]string{"network", "use", "https://api.example", "wss://connect.example"}, []string{"choose_network", "https://api.example", "wss://connect.example"}},
		{[]string{"network", "--show"}, []string{"choose_network", "--show"}},
		{[]string{"network", "--reset"}, []string{"choose_network", "--reset"}},
		{[]string{"network", "https://api.example", "wss://connect.example"}, []string{"choose_network", "https://api.example", "wss://connect.example"}},
	}
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler, SkipHelpFlags: true}
	for _, c := range cases {
		_, _, rest, done, _ := runCliFront(t, c.alias...)
		if done {
			t.Fatalf("%v: answered by the front", c.alias)
		}
		aliasOpts, err := parser.ParseArgs(mainUsage(), rest, "")
		if err != nil {
			t.Fatalf("%v: %v", c.alias, err)
		}
		legacyOpts, err := parser.ParseArgs(mainUsage(), c.legacy, "")
		if err != nil {
			t.Fatalf("%v: %v", c.legacy, err)
		}
		for key, value := range legacyOpts {
			if !reflect.DeepEqual(aliasOpts[key], value) {
				t.Fatalf("%v: %s = %v, want %v", c.alias, key, aliasOpts[key], value)
			}
		}
	}
}

// Existing invocations reach docopt unchanged.
func TestCliFrontPassesLegacyArgvThrough(t *testing.T) {
	for _, args := range [][]string{
		{"provide"},
		{"provide", "--allow-client-registration"},
		{"auth", "CODE", "-f"},
		{"wallet", "set", "5F3sa2TJAWMqDhXG6jhV4N8ko9SxwGy8TpaNS1repo5EYjQX"},
		{"choose_network", "--show"},
		{"fleet", "manifest", "--manifest=m.json"},
		// options before the command, which docopt has always accepted
		{"-v", "operators"},
		{"--port=0", "provide"},
		{"-f", "auth", "abc"},
		// help flags are left to the parser, which knows a value from a flag
		{"auth", "--user_auth=u", "--password", "-h"},
		{"provide", "--help"},
	} {
		_, _, rest, done, _ := runCliFront(t, args...)
		if done || strings.Join(rest, " ") != strings.Join(args, " ") {
			t.Fatalf("%v: done=%v rest=%v", args, done, rest)
		}
	}
}

func TestCliUnknownCommandSuggests(t *testing.T) {
	_, stderr, _, done, code := runCliFront(t, "provde")
	if !done || code != 1 || !strings.Contains(stderr, `Did you mean "provide"?`) {
		t.Fatalf("done=%v code=%d %q", done, code, stderr)
	}
	_, stderr, _, _, _ = runCliFront(t, "strat")
	if !strings.Contains(stderr, `Did you mean "start"?`) {
		t.Fatalf("%q", stderr)
	}
	if lines := strings.Count(stderr, "\n"); 3 < lines {
		t.Fatalf("unknown command message is %d lines", lines)
	}
}

func TestCliParseErrorIsShort(t *testing.T) {
	message := cliParseError([]string{"provide", "--bogus"}, []string{"provide", "--bogus"})
	if !strings.Contains(message, "Unknown option --bogus") {
		t.Fatal(message)
	}
	message = cliParseError([]string{"fleet", "regster"}, []string{"fleet", "regster"})
	if !strings.Contains(message, `Did you mean "register"?`) {
		t.Fatal(message)
	}
	message = cliParseError([]string{"auth", "--", "--version", "x"}, []string{"auth", "--", "--version", "x"})
	if strings.Contains(message, "Unknown option") {
		t.Fatal(message)
	}
	message = cliParseError([]string{"choose_network", "https://a"}, []string{"network", "use", "https://a"})
	if !strings.Contains(message, `"provider network use"`) || !strings.Contains(message, "short for provider choose_network") {
		t.Fatal(message)
	}
	message = cliParseError([]string{"-v", "provide", "--bogus"}, []string{"-v", "provide", "--bogus"})
	if !strings.Contains(message, `Unknown option --bogus for "provider provide"`) {
		t.Fatal(message)
	}
	message = cliParseError([]string{"--api_url=x", "wallet", "set"}, []string{"--api_url=x", "wallet", "set"})
	if !strings.Contains(message, `"provider wallet set"`) || !strings.Contains(message, "provider wallet set <coldkey_ss58>") {
		t.Fatal(message)
	}
	message = cliParseError([]string{"choose_network", "a"}, []string{"network", "a"})
	if !strings.Contains(message, "short for provider choose_network)") {
		t.Fatal(message)
	}
	if _, stderr, _, done, code := runCliFront(t, "help", "bogus"); !done || code != 1 || !strings.Contains(stderr, `Unknown command "bogus"`) {
		t.Fatalf("help bogus: done=%v code=%d %q", done, code, stderr)
	}
	message = cliParseError([]string{"wallet"}, []string{"wallet"})
	if !strings.Contains(message, "provider wallet set <coldkey_ss58>") || 10 < strings.Count(message, "\n") {
		t.Fatal(message)
	}
}

// Every command in the usage has a summary, so a new command cannot ship
// with an empty `--help`.
func TestCliEveryCommandHasHelp(t *testing.T) {
	for command := range usageCommands(mainUsage(), "provider") {
		if cliCommandHelp[command].summary == "" {
			t.Errorf("%s has no summary in cliCommandHelp", command)
		}
		stdout := cliHelpFor([]string{command, "--help"})
		if !strings.HasPrefix(stdout, "provider "+command) {
			t.Errorf("%s --help is not its own help", command)
		}
	}
}

func TestCliCommandHelpShowsItsOwnOptions(t *testing.T) {
	stdout := cliHelpFor([]string{"wallet", "--help"})
	for _, want := range []string{"provider wallet challenge", "--coldkey_seed_file=<path>", "--signature=<hex>"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("missing %q:\n%s", want, stdout)
		}
	}
	for _, unwanted := range []string{"--whole-work-capture", "--burn_limit_rao", "--config="} {
		if strings.Contains(stdout, unwanted) {
			t.Fatalf("has %q:\n%s", unwanted, stdout)
		}
	}
	stdout, _, _, _, _ = runCliFront(t, "help", "start")
	if !strings.Contains(stdout, "provider provide  (also: provider start)") {
		t.Fatal(stdout)
	}
}

func TestCliPanicMessage(t *testing.T) {
	for _, err := range []error{io.EOF, errEmptyAuthCode} {
		message := cliPanicMessage([]string{"login"}, err)
		if !strings.Contains(message, "No auth code was entered") || strings.Contains(message, "goroutine") {
			t.Fatal(message)
		}
	}
	message := cliPanicMessage([]string{"auth", "BADCODE"}, "Invalid auth code.")
	if !strings.Contains(message, "wasn't accepted") {
		t.Fatal(message)
	}
	message = cliPanicMessage([]string{"login", "CODE"}, errors.New("http request attempts exhausted"))
	if !strings.Contains(message, "Couldn't reach the URnetwork API") || !strings.Contains(message, "URNETWORK_CONNECT_EXTRA_ROOT_CA_FILE") {
		t.Fatal(message)
	}
	t.Setenv("URNETWORK_STATE_DIR", "/home/me/my state")
	message = cliPanicMessage([]string{"auth", "CODE"}, errors.New("registration parent must be owned and not writable by other users"))
	if !strings.Contains(message, "/home/me/my state can be changed by other users") || !strings.HasSuffix(message, "chmod 700 '/home/me/my state'") {
		t.Fatal(message)
	}
	message = cliPanicMessage([]string{"provide"}, errors.New("disk full"))
	if !strings.HasPrefix(message, "disk full\n") || !strings.Contains(message, "-vv") {
		t.Fatal(message)
	}
}

// Help lines stay readable in an 80-100 column terminal, and a continuation
// line that starts with an option name is not read as a new option.
func TestCliCommandHelpLayout(t *testing.T) {
	for _, args := range [][]string{{"auth"}, {"provide"}, {"wallet"}, {"wallet", "hotkey"}, {"fleet"}, {"fleet", "register"}, {"claim"}, {"operators"}, {"proxy"}} {
		stdout := cliHelpFor(args)
		for _, line := range strings.Split(stdout, "\n") {
			if cliHelpWidth < len(line) {
				t.Errorf("%v: %d-character line %q", args, len(line), line)
			}
			if strings.HasPrefix(line, "    --hotkey_seed_file;") {
				t.Errorf("%v: stray continuation line %q", args, line)
			}
		}
	}
	if stdout := cliHelpFor([]string{"wallet", "hotkey"}); strings.Contains(stdout, "Set the wallet your rewards are paid to") {
		t.Fatalf("wallet hotkey reuses the wallet summary:\n%s", stdout)
	}
	if stdout := cliHelpFor([]string{"fleet", "register"}); !strings.Contains(stdout, "register_limit") {
		t.Fatalf("fleet register does not explain its --coldkey_seed_file:\n%s", stdout)
	}
}
