package miner

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/term"

	"github.com/docopt/docopt-go"
)

// The friendly front of the CLI. mainUsage stays the one grammar and the
// complete reference; this layer answers help and version itself, gives
// everyday commands friendlier names, and turns a parse error or a panic
// into a short message instead of the whole usage text or a goroutine trace.
// Every existing invocation still parses and runs exactly as before.

// cliAliases maps a friendly command to the argv it stands for. Only the
// leading words are rewritten; the rest of argv follows unchanged.
// An alias marked alone applies only when it is the whole argv.
var cliAliases = []struct {
	words   []string
	replace []string
	alone   bool
}{
	{[]string{"start"}, []string{"provide"}, false},
	{[]string{"login"}, []string{"auth"}, false},
	{[]string{"network", "show"}, []string{"choose_network", "--show"}, false},
	{[]string{"network", "reset"}, []string{"choose_network", "--reset"}, false},
	{[]string{"network", "use"}, []string{"choose_network"}, false},
	{[]string{"network"}, []string{"choose_network", "--show"}, true},
	{[]string{"network"}, []string{"choose_network"}, false},
}

// cliAliasCommands returns each alias's first word, so docs may name it.
func cliAliasCommands() map[string]bool {
	out := map[string]bool{}
	for _, alias := range cliAliases {
		out[alias.words[0]] = true
	}
	return out
}

// cliCommandHelp is the plain-language part of `provider <command> --help`.
// The usage forms and option text come from mainUsage, so they never drift.
var cliCommandHelp = map[string]struct {
	summary  string
	alias    string
	examples []string
}{
	"setup": {
		summary: "Set up this device in four steps: sign in, register it as a miner, set the payout\naddress, and start the background service. Safe to run again; finished steps are kept.",
		examples: []string{
			"provider setup                                       # asks as it goes",
			"provider setup --yes --auth-code-file=/run/secrets/ur_code   # no prompts, for fleets",
			"sudo provider setup --system --yes --auth-code-file=code.txt # a system service",
		},
	},
	"status": {
		summary: "Show whether the miner on this device is set up, running and connected, one line\neach, and the next command when something isn't. Exit code: 0 all good, 3 needs\nsetup, 4 not running or not connected.",
		examples: []string{
			"provider status",
			"provider status --json     # for scripts and monitoring",
		},
	},
	"doctor": {
		summary: "Look for why the miner can't sign in or connect: the state folder, DNS, the\nconnection, a TLS-inspecting proxy, the clock, and whether updates can install.",
	},
	"service": {
		summary: "Run the miner in the background and on every boot (systemd on Linux, launchd on\nmacOS), with automatic updates checked every 6 hours. If the URnetwork installer's\nurnetwork.service is already there, start, stop, status and logs work on it.",
		examples: []string{
			"provider service install     # start now and on every boot",
			"provider service logs --follow",
			"provider service uninstall   # your sign-in and identity are kept",
		},
	},
	"update": {
		summary: "Update the miner to the newest release. With the background service installed,\nupdates install automatically within a day of a release, and a version that\ndoesn't come back connected is rolled back. Turn that off with provider update\n--off, or set URNETWORK_AUTO_UPDATE=0. A miner the URnetwork installer set up\nkeeps updating through its urnet-tools, and these commands run that.",
		examples: []string{
			"provider update            # update now",
			"provider update --status   # automatic update settings and last check",
			"provider update --off      # no automatic updates (provider update --on undoes it)",
		},
	},
	"auth": {
		summary: "Sign this device in to the network. Get an auth code from your URnetwork app or\naccount page; with no code on the command line you are asked to paste it.",
		alias:   "login",
		examples: []string{
			"provider login                       # paste the code when asked",
			"provider login --user_auth=you@example.com",
			"provider auth --operator=<domain>    # sign in to one operator from the list",
		},
	},
	"provide": {
		summary: "Run the miner in this terminal until it is stopped (Ctrl-C). On a new device,\nprovider start asks before registering it; provide and scripts need\n--allow-client-registration for that first start.",
		alias:   "start",
		examples: []string{
			"provider start                               # in a terminal: plain progress, logs in miner.log",
			"provider start --allow-client-registration   # first start without a prompt",
			"provider start --all-operators               # mine every listed operator",
		},
	},
	"auth-provide": {
		summary: "Sign in, then start mining: auth followed by provide in one command.",
	},
	"operators": {
		summary: "List the operators in the published operator list and whether this device\nis signed in to each.",
		examples: []string{
			"provider operators",
		},
	},
	"wallet": {
		summary: "Set the wallet your rewards are paid to: a Bittensor coldkey address (ss58,\nstarts with 5). You prove you own it by signing a challenge, either here with\nthe seed file or on another device.",
		examples: []string{
			"provider wallet challenge 5Grw...utQY      # print the text to sign elsewhere",
			"provider wallet set 5Grw...utQY --message=\"$(cat challenge.txt)\" --signature=0x...",
			"provider wallet set 5Grw...utQY --coldkey_seed_file=$HOME/.keys/cold.seed",
		},
	},
	"claim": {
		summary: "Fetch and verify the pool claim for an epoch. With --key_file it also submits it.",
		examples: []string{
			"provider claim --rpc=<rpc_url>          # verify the last finished epoch's claim",
		},
	},
	"wallet hotkey": {
		summary: "Set one coldkey-to-hotkey payout chain for every operator in the list,\nor show its status. Advanced: most miners use provider wallet set.",
	},
	"proxy auth": {
		summary: "Add or remove a SOCKS5 user and password, for every proxy or for one proxy key.",
	},
	"claim-daemon": {
		summary: "Submit claims automatically from a durable queue (release-1.0 configuration).",
	},
	"claim-daemon-v2": {
		summary: "Claim each account automatically once its balance passes a threshold.",
	},
	"fleet": {
		summary: "Head-tier fleet management: manifests, registration, bindings and revocation.\nRegistration is a dry run unless --apply is given.",
	},
	"proxy": {
		summary: "Add or remove SOCKS5 exits. Each exit gets its own provider slot.",
		examples: []string{
			"provider proxy add 203.0.113.7:1080:user:pass",
			"provider proxy add --proxy_file=proxies.txt",
		},
	},
	"choose_network": {
		summary: "Show or switch the network this device connects to. The default is the main network.",
		alias:   "network",
		examples: []string{
			"provider network                     # show the network in effect",
			"provider network use <api_url> <connect_url>",
			"provider network reset               # back to the main network",
		},
	},
}

func cliOverview() string {
	version := RequireVersion()
	if version == "" {
		version = "development build"
	}
	return fmt.Sprintf(`provider: the URnetwork miner                     %s

Usage: provider <command> [options]

Get started
  setup         Set up this device, step by step
  login         Sign this device in with an auth code     (same as auth)
  start         Start mining in this terminal             (same as provide)
  wallet        Set the wallet your rewards are paid to

Check on it
  status        Is the miner set up, running and connected?
  doctor        Find out why it can't sign in or connect
  service       Run it in the background and on every boot
  update        Update to the newest release

Manage
  operators     List operators and whether you are signed in to each
  network       Show or switch the network          (same as choose_network)
  proxy         Add or remove SOCKS5 exits

Advanced
  claim, claim-daemon, claim-daemon-v2, fleet, auth-provide

Help
  provider <command> --help    Usage, options and examples for one command
  provider help all            The complete reference: every command and option
  provider --version           Print the version

Examples
  provider setup                 # the easy way: sign in, register, run in the background
  provider login                 # or by hand: paste the auth code when asked,
  provider start                 # then start mining in this terminal

State lives in %s. Docs: https://ur.xyz/docs/miner
`, version, displayStateDir())
}

func cliBareVersion() string {
	if version := RequireVersion(); version != "" {
		return version
	}
	return "(development build)"
}

func cliVersion() string {
	return fmt.Sprintf("provider %s %s/%s", cliBareVersion(), runtime.GOOS, runtime.GOARCH)
}

// cliFront handles what the friendly front answers on its own. It returns the
// argv to parse, or done with the exit code when it already answered.
func cliFront(args []string, stdout io.Writer, stderr io.Writer) (rest []string, done bool, code int) {
	if len(args) == 0 {
		fmt.Fprint(stdout, cliOverview())
		return nil, true, 0
	}
	switch args[0] {
	case "--version":
		if len(args) == 1 {
			// the bare version, as docopt printed it
			fmt.Fprintln(stdout, cliBareVersion())
			return nil, true, 0
		}
	case "version":
		fmt.Fprintln(stdout, cliVersion())
		return nil, true, 0
	case "help":
		switch {
		case len(args) == 1 || args[1] == "help":
			fmt.Fprint(stdout, cliOverview())
		case args[1] == "all" || args[1] == "advanced":
			fmt.Fprintln(stdout, mainUsage())
		case args[1] == "version":
			fmt.Fprintln(stdout, cliVersion())
		default:
			target := rewriteCliAlias(args[1:])
			if commands := usageCommands(mainUsage(), "provider"); !commands[target[0]] {
				writeCliError(stderr, unknownCliCommand(args[1], commands))
				return nil, true, 1
			}
			fmt.Fprint(stdout, cliHelpFor(target))
		}
		return nil, true, 0
	}
	if strings.HasPrefix(args[0], "-") {
		// options may come before the command, as docopt has always allowed;
		// -h and --help are answered by the parser's help handler
		return args, false, 0
	}

	args = rewriteCliAlias(args)
	commands := usageCommands(mainUsage(), "provider")
	if !commands[args[0]] {
		writeCliError(stderr, unknownCliCommand(args[0], commands))
		return nil, true, 1
	}
	return args, false, 0
}

// cliHelpFor is the help for the command args names, ignoring options, or
// the overview when it names none.
func cliHelpFor(args []string) string {
	var words []string
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "-") {
			words = append(words, arg)
		}
	}
	words = rewriteCliAlias(words)
	if len(words) == 0 || !usageCommands(mainUsage(), "provider")[words[0]] {
		return cliOverview()
	}
	return cliCommandUsage(words)
}

func rewriteCliAlias(args []string) []string {
	if i := cliAliasIndex(args); 0 <= i {
		out := append([]string{}, cliAliases[i].replace...)
		return append(out, args[len(cliAliases[i].words):]...)
	}
	return args
}

// matchedCliAlias returns the alias words that start args, or nil.
func matchedCliAlias(args []string) []string {
	if i := cliAliasIndex(args); 0 <= i {
		return cliAliases[i].words
	}
	return nil
}

func cliAliasIndex(args []string) int {
	for i, alias := range cliAliases {
		if len(alias.words) <= len(args) && equalWords(alias.words, args[:len(alias.words)]) && (!alias.alone || len(args) == len(alias.words)) {
			return i
		}
	}
	return -1
}

func equalWords(a []string, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func unknownCliCommand(name string, commands map[string]bool) string {
	if strings.HasPrefix(name, "-") {
		return fmt.Sprintf("%q needs a command before it.\n  All commands: provider --help", name)
	}
	candidates := []string{}
	for command := range commands {
		candidates = append(candidates, command)
	}
	for command := range cliAliasCommands() {
		candidates = append(candidates, command)
	}
	if suggestion := closestWord(name, candidates); suggestion != "" {
		return fmt.Sprintf("Unknown command %q. Did you mean %q?\n  All commands: provider --help", name, suggestion)
	}
	return fmt.Sprintf("Unknown command %q.\n  All commands: provider --help", name)
}

// cliParseError explains why argv did not match the usage of its command, in
// place of docopt printing every usage line.
func cliParseError(args []string, typed []string) string {
	message := cliParseErrorFor(args)
	if alias := matchedCliAlias(typed); alias != nil {
		// name the command as it was typed, and what it stands for
		message = strings.ReplaceAll(message, "\"provider "+args[0], "\"provider "+strings.Join(alias, " "))
		message = strings.Replace(message, "  Help: provider "+args[0], "  Help: provider "+strings.Join(alias, " "), 1)
		message += fmt.Sprintf("\n  (provider %s is short for provider %s)", strings.Join(alias, " "), strings.Join(cliAliases[cliAliasIndex(typed)].replace, " "))
	}
	return message
}

func cliParseErrorFor(all []string) string {
	// options may come before the command; name the command itself
	args := commandArgs(all)
	if len(args) == 0 {
		return "Options need a command after them.\n  All commands: provider --help"
	}
	known := usageOptionNames(mainUsage())
	for _, arg := range all {
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}
		name, _, _ := strings.Cut(arg, "=")
		if isVerbosityArg(name) || known[name] {
			continue
		}
		names := []string{}
		for option := range known {
			names = append(names, option)
		}
		message := fmt.Sprintf("Unknown option %s for %q.", name, "provider "+args[0])
		if suggestion := closestWord(name, names); suggestion != "" {
			message = fmt.Sprintf("Unknown option %s for %q. Did you mean %s?", name, "provider "+args[0], suggestion)
		}
		return message + "\n  Help: provider " + args[0] + " --help"
	}
	words := commandWords(args)
	command := "provider " + strings.Join(words, " ")
	forms := commandUsageForms(mainUsage(), words)
	if len(words) < len(args) && !strings.HasPrefix(args[len(words)], "-") {
		if subcommands := commandSubcommands(forms, words); 0 < len(subcommands) {
			message := fmt.Sprintf("Unknown command %q for %q.", args[len(words)], command)
			if suggestion := closestWord(args[len(words)], subcommands); suggestion != "" {
				message = fmt.Sprintf("Unknown command %q for %q. Did you mean %q?", args[len(words)], command, suggestion)
			}
			return message + "\n  Help: " + command + " --help"
		}
	}
	var b strings.Builder
	if len(words) == len(args) {
		fmt.Fprintf(&b, "%q needs more. It can be run as:\n", command)
	} else {
		fmt.Fprintf(&b, "Those arguments don't match %q. It can be run as:\n", command)
	}
	for _, form := range forms {
		for _, line := range wrapCliLine("    "+firstLine(form), "            ") {
			fmt.Fprintf(&b, "%s\n", line)
		}
	}
	fmt.Fprintf(&b, "  Help: %s --help", command)
	return b.String()
}

// commandArgs is argv from its first known command on.
func commandArgs(args []string) []string {
	commands := usageCommands(mainUsage(), "provider")
	for i, arg := range args {
		if arg == "--" {
			break
		}
		if commands[arg] {
			return args[i:]
		}
	}
	return nil
}

// commandSubcommands returns the literal words that may follow words in forms.
func commandSubcommands(forms []string, words []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, form := range forms {
		fields := strings.Fields(firstLine(form))
		if len(words)+1 < len(fields) {
			next := fields[len(words)+1]
			if regexp.MustCompile(`^[a-z][a-z0-9_-]*$`).MatchString(next) && !seen[next] {
				seen[next] = true
				out = append(out, next)
			}
		}
	}
	return out
}

// cliParser parses argv against mainUsage, answering a mismatch with
// cliParseError rather than the whole usage text.
// A help flag docopt really parsed as one (not an option value) gets that
// command's help; --version is printed as before.
func cliParser(args []string, typed []string, stdout io.Writer, stderr io.Writer) *docopt.Parser {
	return &docopt.Parser{
		HelpHandler: func(err error, output string) {
			if err != nil {
				writeCliError(stderr, cliParseError(args, typed))
				os.Exit(1)
			}
			if output == strings.Trim(mainUsage(), "\n") {
				fmt.Fprint(stdout, cliHelpFor(args))
				os.Exit(0)
			}
			docopt.PrintHelpAndExit(err, output)
		},
	}
}

// cliCommandUsage is `provider <command> --help`: a summary, the command's
// usage forms, the options those forms use and a few examples.
func cliCommandUsage(args []string) string {
	usage := mainUsage()
	words := commandWords(args)
	help := cliCommandHelp[strings.Join(words, " ")]
	if help.summary == "" && len(words) == 1 {
		help = cliCommandHelp[words[0]]
	}
	var b strings.Builder
	title := "provider " + strings.Join(words, " ")
	if help.alias != "" && len(words) == 1 {
		title += "  (also: provider " + help.alias + ")"
	}
	fmt.Fprintf(&b, "%s\n\n", title)
	if help.summary != "" {
		fmt.Fprintf(&b, "%s\n\n", help.summary)
	}
	forms := commandUsageForms(usage, words)
	fmt.Fprintf(&b, "Usage:\n")
	for _, form := range forms {
		for _, line := range strings.Split(form, "\n") {
			indent := "            "
			if strings.HasPrefix(strings.TrimSpace(line), "[") {
				indent = "            " + "    "
			}
			for _, wrapped := range wrapCliLine(line, indent) {
				fmt.Fprintf(&b, "%s\n", wrapped)
			}
		}
	}
	if options := commandOptionText(usage, forms); options != "" {
		fmt.Fprintf(&b, "\nOptions:\n%s", options)
	}
	if 0 < len(help.examples) && len(words) == 1 {
		fmt.Fprintf(&b, "\nExamples:\n")
		for _, example := range help.examples {
			fmt.Fprintf(&b, "    %s\n", example)
		}
	}
	fmt.Fprintf(&b, "\nEvery command and option: provider help all\n")
	return b.String()
}

// commandWords is the command and the subcommands of argv that the usage
// knows, e.g. [wallet hotkey set] from "wallet hotkey set 5F... --x".
func commandWords(args []string) []string {
	usage := mainUsage()
	words := []string{args[0]}
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "-") {
			break
		}
		next := append(append([]string{}, words...), arg)
		if len(commandUsageForms(usage, next)) == 0 {
			break
		}
		words = next
	}
	return words
}

// commandUsageForms returns the usage entries (a `provider ...` line and its
// continuation lines) whose words start with words.
func commandUsageForms(usage string, words []string) []string {
	section := usageSection(usage, "Usage:")
	prefix := "provider " + strings.Join(words, " ")
	var forms []string
	var current []string
	flush := func() {
		if 0 < len(current) {
			forms = append(forms, strings.Join(current, "\n"))
		}
		current = nil
	}
	matching := false
	for _, line := range section {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "provider ") {
			flush()
			matching = trimmed == prefix || strings.HasPrefix(trimmed, prefix+" ")
			if matching {
				current = []string{"    " + trimmed}
			}
			continue
		}
		if matching && trimmed != "" {
			current = append(current, "        "+trimmed)
		}
	}
	flush()
	return forms
}

// commandOptionText returns the Options entries for the options and
// arguments that appear in forms, in the order the reference lists them.
func commandOptionText(usage string, forms []string) string {
	// "--x=<value>" names the option --x; only a bare <arg> names an argument
	used := map[string]bool{}
	for _, name := range optionPattern.FindAllString(optionValuePattern.ReplaceAllString(strings.Join(forms, " "), ""), -1) {
		used[name] = true
	}
	var b strings.Builder
	var declaration string
	var description []string
	flush := func() {
		if declaration != "" {
			writeOptionEntry(&b, declaration, strings.Join(description, " "))
		}
		declaration, description = "", nil
	}
	for _, line := range usageSection(usage, "Options:") {
		expanded := strings.ReplaceAll(line, "\t", "    ")
		trimmed := strings.TrimSpace(expanded)
		if trimmed == "" {
			continue
		}
		// a declaration starts near the margin; deeper lines continue the
		// entry above, even when they start with an option name
		indent := len(expanded) - len(strings.TrimLeft(expanded, " "))
		if indent <= 8 && (strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "<")) {
			flush()
			names, text := splitOptionLine(trimmed)
			for _, name := range optionPattern.FindAllString(optionValuePattern.ReplaceAllString(names, ""), -1) {
				if used[name] {
					declaration, description = names, []string{text}
				}
			}
			continue
		}
		if declaration != "" {
			description = append(description, trimmed)
		}
	}
	flush()
	return b.String()
}

// writeOptionEntry lays out one option: the declaration, then its description
// from optionDescriptionColumn (on the next line when the declaration is
// wider), wrapped at cliHelpWidth.
func writeOptionEntry(b *strings.Builder, declaration string, description string) {
	pad := strings.Repeat(" ", optionDescriptionColumn)
	first := "    " + declaration
	if len(first)+2 <= optionDescriptionColumn {
		first += strings.Repeat(" ", optionDescriptionColumn-len(first)) + description
	} else {
		fmt.Fprintf(b, "%s\n", first)
		first = pad + description
	}
	for _, line := range wrapCliLine(strings.TrimRight(first, " "), pad) {
		fmt.Fprintf(b, "%s\n", line)
	}
}

// optionDescriptionColumn is where option descriptions start in help.
const optionDescriptionColumn = 37

// cliHelpWidth is the widest line help prints before wrapping.
const cliHelpWidth = 100

// splitOptionLine splits a reference Options line into its declaration and
// its description (separated by two or more spaces, or a tab).
func splitOptionLine(trimmed string) (string, string) {
	declaration, description, _ := strings.Cut(trimmed, "  ")
	return strings.TrimSpace(declaration), strings.TrimSpace(description)
}

// wrapCliLine breaks line at spaces so no piece is wider than cliHelpWidth,
// starting each continuation with indent.
func wrapCliLine(line string, indent string) []string {
	var out []string
	for cliHelpWidth < len(line) {
		cut := strings.LastIndex(line[:cliHelpWidth], " ")
		if cut <= len(indent) {
			break
		}
		out = append(out, strings.TrimRight(line[:cut], " "))
		line = indent + strings.TrimLeft(line[cut:], " ")
	}
	return append(out, line)
}

var optionPattern = regexp.MustCompile(`--?[A-Za-z][A-Za-z0-9_-]*|<[a-z0-9_]+>`)

var optionValuePattern = regexp.MustCompile(`=<[^>]*>`)

func usageSection(usage string, header string) []string {
	_, body, found := strings.Cut(usage, "\n"+header+"\n")
	if !found {
		return nil
	}
	if header == "Usage:" {
		body, _, _ = strings.Cut(body, "\nOptions:\n")
	}
	return strings.Split(body, "\n")
}

func usageOptionNames(usage string) map[string]bool {
	out := map[string]bool{"-h": true, "--help": true}
	for _, name := range optionPattern.FindAllString(usage, -1) {
		if strings.HasPrefix(name, "-") {
			out[name] = true
		}
	}
	return out
}

func isVerbosityArg(arg string) bool {
	return regexp.MustCompile(`^-v+$`).MatchString(arg)
}

func cliVerbosity(args []string) int {
	level := 0
	for _, arg := range args {
		if isVerbosityArg(arg) {
			level += len(arg) - 1
		}
	}
	return level
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// closestWord returns the candidate within two edits of word (or that word
// abbreviates), or "" when none is that close.
func closestWord(word string, candidates []string) string {
	sort.Strings(candidates)
	best, bestDistance := "", 3
	for _, candidate := range candidates {
		distance := editDistance(word, candidate)
		if 3 <= len(word) && strings.HasPrefix(candidate, word) && distance > 1 {
			distance = 1
		}
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return best
}

func editDistance(a string, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

// writeCliError prints "✗ message" to w, the mark in red on a color terminal.
func writeCliError(w io.Writer, message string) {
	mark := "✗"
	if cliColor(w) {
		mark = "\x1b[31m✗\x1b[0m"
	}
	fmt.Fprintf(w, "%s %s\n", mark, message)
}

func cliColor(w io.Writer) bool {
	return cliIsTerminal(w) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}

func cliIsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

// cliPanicMessage is what a panic in a command becomes, unless -vv or
// PROVIDER_DEBUG asks for the trace.
func cliPanicMessage(args []string, recovered any) string {
	err, _ := recovered.(error)
	command := ""
	if 0 < len(args) {
		command = args[0]
	}
	if errors.Is(err, errPromptInterrupted) {
		return "Cancelled."
	}
	if (command == "auth" || command == "login") && (errors.Is(err, io.EOF) || errors.Is(err, errEmptyAuthCode)) && os.Getenv(cliSetupStepEnv) != "" {
		return "No auth code was entered. Get one from your URnetwork app or account page,\n  then run provider setup again."
	}
	if (command == "auth" || command == "login" || command == "auth-provide") && (errors.Is(err, io.EOF) || errors.Is(err, errEmptyAuthCode)) {
		return "No auth code was entered.\n" +
			"  Get one from your URnetwork app or account page, then run:\n" +
			"  provider login <auth_code>"
	}
	text := fmt.Sprint(recovered)
	if (command == "auth" || command == "login" || command == "auth-provide") && strings.Contains(text, "Invalid auth code") {
		return "That auth code wasn't accepted. It may have expired or been mistyped.\n" +
			"  Get a fresh one from your URnetwork app or account page, then run:\n" +
			"  provider login <auth_code>"
	}
	if strings.Contains(text, "http request attempts exhausted") {
		return "Couldn't reach the URnetwork API. Check this device's internet connection.\n" +
			"  Behind a proxy that inspects TLS, set URNETWORK_CONNECT_EXTRA_ROOT_CA_FILE\n" +
			"  to a file of that proxy's root CA certificates.\n" +
			"  For technical details run the command again with -vv."
	}
	if strings.Contains(text, "registration parent must be owned and not writable by other users") {
		dir, dirErr := providerStateDir()
		if dirErr != nil {
			dir = "~/.urnetwork"
		}
		return fmt.Sprintf("The state folder %s can be changed by other users, so the miner won't keep its keys there.\n"+
			"  Make it private, then run the command again:\n"+
			"  %s", dir, privateDirFix(dir))
	}
	return fmt.Sprintf("%v\n  For technical details run the command again with -vv.", recovered)
}

// cliShellQuote quotes a path for a command the user copies, only when needed.
func cliShellQuote(path string) string {
	if regexp.MustCompile(`^[A-Za-z0-9_./~-]+$`).MatchString(path) {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

func recoverCliPanic(args []string, stderr io.Writer) {
	recovered := recover()
	if recovered == nil {
		return
	}
	if stderr == io.Writer(os.Stderr) && cliTerminal != nil {
		// provider start sent stderr to its log; answer on the terminal
		stderr = cliTerminal
	}
	if err, ok := recovered.(error); ok && errors.Is(err, errPromptInterrupted) {
		// Ctrl-C at a prompt is no crash
		fmt.Fprintln(stderr, cliPanicMessage(args, recovered))
		os.Exit(130)
	}
	// logs (systemd, docker) keep the full panic, as before; a step of
	// provider setup answers in plain words, since setup shows it to a person
	if 2 <= cliVerbosity(args) || os.Getenv("PROVIDER_DEBUG") != "" || !cliIsTerminal(stderr) && os.Getenv(cliSetupStepEnv) == "" {
		panic(recovered)
	}
	writeCliError(stderr, cliPanicMessage(args, recovered))
	// a panic exited 2 before, and supervisors may rely on it
	os.Exit(2)
}

// usageCommands returns the first word after the binary name on each docopt
// usage line.
func usageCommands(usage string, binary string) map[string]bool {
	line := regexp.MustCompile(`(?m)^\s*` + binary + ` ([a-z][a-z0-9_-]*)`)
	out := map[string]bool{}
	for _, match := range line.FindAllStringSubmatch(usage, -1) {
		out[match[1]] = true
	}
	return out
}
