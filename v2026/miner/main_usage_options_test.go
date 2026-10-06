// Parse the production command grammar at its public dependency boundary.
// Option descriptions must not turn boolean launch policy into a value or
// prevent unrelated claim and wallet commands from reaching their owners.
package miner

import (
	"reflect"
	"strings"
	"testing"

	"github.com/docopt/docopt-go"
)

// Both startup forms retain exact profile values and independent boolean gates.
func TestMainUsageCaptureFlagsRemainBoolean(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, command := range [][]string{{"provide"}, {"auth-provide", "synthetic-code"}} {
		for _, required := range []bool{false, true} {
			arguments := append([]string(nil), command...)
			arguments = append(arguments,
				"--whole-work-capture=/synthetic/work.json", "--whole-work-capture-sha256="+digest,
				"--original-contract-capture=/synthetic/contracts.json", "--original-contract-capture-sha256="+digest,
				"--api_url=https://operator.example", "--port=1234")
			if required {
				arguments = append(arguments, "--require-original-contract-capture", "--require-whole-work-capture")
			}
			opts := parseArgsForTest(t, arguments)
			for _, flag := range []string{"--require-whole-work-capture", "--require-original-contract-capture"} {
				value, err := opts.Bool(flag)
				if err != nil || value != required {
					t.Fatalf("parse %v: %s lost its boolean policy: %v, %v", arguments, flag, value, err)
				}
			}
			for option, want := range map[string]string{
				"--whole-work-capture":               "/synthetic/work.json",
				"--whole-work-capture-sha256":        digest,
				"--original-contract-capture":        "/synthetic/contracts.json",
				"--original-contract-capture-sha256": digest,
				"--api_url":                          "https://operator.example",
				"--port":                             "1234",
			} {
				value, err := opts.String(option)
				if err != nil || value != want {
					t.Fatalf("parse %v: %s = %q, %v; want %q", arguments, option, value, err, want)
				}
			}
		}
	}
}

// A capture option's declaration must not corrupt the independent financial
// command grammar or change the explicitly selected credential and proof.
func TestMainUsageFinancialOptionsPreserveSelectedIdentity(t *testing.T) {
	for _, credential := range []string{"--provider-jwt=/synthetic/provider.jwt", "--legacy-coldkey=synthetic-original-coldkey"} {
		arguments := []string{"claim", credential, "--epoch=17", "--rpc=https://rpc-one.example", "--rpc=https://rpc-two.example", "--key_file=/synthetic/relayer.key", "--dry-run"}
		opts := parseArgsForTest(t, arguments)
		if selected, err := opts.Bool("claim"); err != nil || !selected {
			t.Fatalf("claim command was lost: %v, %v", selected, err)
		}
		if values, ok := opts["--rpc"].([]string); !ok || !reflect.DeepEqual(values, []string{"https://rpc-one.example", "https://rpc-two.example"}) {
			t.Fatalf("claim changed ordered rpc inputs: %#v", opts["--rpc"])
		}
		if epoch, err := opts.String("--epoch"); err != nil || epoch != "17" {
			t.Fatalf("claim changed selected epoch: %q, %v", epoch, err)
		}
		if dryRun, err := opts.Bool("--dry-run"); err != nil || !dryRun {
			t.Fatalf("claim lost dry-run: %v, %v", dryRun, err)
		}
		name, want, _ := strings.Cut(credential, "=")
		if actual, err := opts.String(name); err != nil || actual != want {
			t.Fatalf("claim changed explicit identity: %q, %v; want %q", actual, err, want)
		}
	}
	for _, command := range []string{"set", "challenge"} {
		arguments := []string{"wallet", command, "synthetic-coldkey", "--provider-jwt=/synthetic/provider.jwt", "--wallet-from-epoch=18", "--wallet-through-epoch=19"}
		if command == "set" {
			arguments = append(arguments, "--message=synthetic consent", "--signature=synthetic-signature")
		}
		opts := parseArgsForTest(t, arguments)
		for option, want := range map[string]string{
			"<coldkey_ss58>":         "synthetic-coldkey",
			"--provider-jwt":         "/synthetic/provider.jwt",
			"--wallet-from-epoch":    "18",
			"--wallet-through-epoch": "19",
		} {
			if actual, err := opts.String(option); err != nil || actual != want {
				t.Fatalf("wallet %s changed %s: %q, %v; want %q", command, option, actual, err, want)
			}
		}
		if command == "set" {
			if message, err := opts.String("--message"); err != nil || message != "synthetic consent" {
				t.Fatalf("wallet set changed original message: %q, %v", message, err)
			}
			if signature, err := opts.String("--signature"); err != nil || signature != "synthetic-signature" {
				t.Fatalf("wallet set changed original signature: %q, %v", signature, err)
			}
		}
	}
}

// Invalid input must fail as user syntax, not as a broken document grammar.
func TestMainUsageRejectsConflictingAuthorityOptions(t *testing.T) {
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler}
	for _, arguments := range [][]string{
		{"provide", "--require-original-contract-capture=true"},
		{"provide", "--require-whole-work-capture=true"},
		{"provide", "--allow-client-registration", "--adopt-legacy-provider-key"},
		{"claim", "--provider-jwt=/synthetic/provider.jwt", "--legacy-coldkey=synthetic-original-coldkey"},
		{"wallet", "set", "synthetic-coldkey", "--provider-jwt=/synthetic/provider.jwt", "--legacy-network-wallet"},
		{"wallet", "set", "synthetic-coldkey", "--message=synthetic consent"},
		{"wallet", "set", "synthetic-coldkey", "--signature=synthetic-signature"},
		{"wallet", "set", "synthetic-coldkey", "--coldkey_seed_file=/synthetic/coldkey.seed", "--message=synthetic consent", "--signature=synthetic-signature"},
	} {
		_, err := parser.ParseArgs(mainUsage(), arguments, "synthetic")
		if _, ok := err.(*docopt.UserError); !ok {
			t.Fatalf("parse %v: want user syntax refusal, got %T: %v", arguments, err, err)
		}
	}
}

// Sibling command families share the whole document and repeatable verbosity.
func TestMainUsageSiblingCommandsAndVerbosity(t *testing.T) {
	for _, arguments := range [][]string{
		{"auth", "--user_auth=synthetic-user", "--password=synthetic-password"},
		{"auth", "--operator=alpha.example", "synthetic-code"},
		{"provide", "--all-operators"},
		{"operators"},
		{"claim-daemon", "--config=/synthetic/daemon.yml"},
		{"fleet", "manifest", "--manifest=/synthetic/fleet.json"},
	} {
		for count := 0; count <= 3; count++ {
			withVerbosity := append([]string(nil), arguments...)
			if count > 0 {
				withVerbosity = append(withVerbosity, "-"+strings.Repeat("v", count))
			}
			opts := parseArgsForTest(t, withVerbosity)
			if actual, ok := opts["-v"].(int); !ok || actual != count {
				t.Fatalf("parse %v: verbosity = %#v; want %d", withVerbosity, opts["-v"], count)
			}
		}
	}
	for _, arguments := range [][]string{
		{"choose_network", "--show"},
		{"proxy", "auth", "add", "synthetic-key", "synthetic-user", "synthetic-password"},
		{"proxy", "auth", "remove", "--all"},
		{"proxy", "add", "proxy.example:1080"},
		{"proxy", "remove", "--all"},
	} {
		parseArgsForTest(t, arguments)
	}
}
