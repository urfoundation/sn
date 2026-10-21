package commitmentoracle

import (
	"strings"
	"testing"

	"github.com/docopt/docopt-go"
)

func parseTestArgs(t *testing.T, args []string) (runOptions, error) {
	parser := &docopt.Parser{HelpHandler: func(err error, usage string) {
		if err != nil {
			t.Fatalf("usage: %v", err)
		}
	}}
	opts, err := parser.ParseArgs(usage(), args, "test")
	if err != nil {
		t.Fatal(err)
	}
	return parseRunOptions(opts)
}

func baseArgs() []string {
	return []string{"run", "--netuid=25", "--chain_id=964", "--coordinator=0x00000000000000000000000000000000c0047d17", "--state_dir=/srv/example/state",
		"--substrate=ws://127.0.0.1:9944", "--substrate=wss://archive.example", "--rpc=http://127.0.0.1:9933", "--rpc=https://rpc.example"}
}

func TestParseRunOptions(t *testing.T) {
	options, err := parseTestArgs(t, append(baseArgs(), "--oracle_key_file=/srv/example/oracle.key", "--metrics_file=/var/lib/example/oracle.prom"))
	if err != nil {
		t.Fatal(err)
	}
	settings := options.settings
	if settings.Netuid != 25 || settings.ChainId != 964 || len(options.substrates) != 2 || len(options.rpcs) != 2 || settings.DryRun || settings.Once {
		t.Fatalf("settings %+v substrates %v rpcs %v", settings, options.substrates, options.rpcs)
	}
	if settings.MaxGasPrice.String() != "20000000000" || settings.MaxGasLimit != 250000 || settings.LowBalanceWei.String() != "10000000000000000" || settings.MaxSendsPerCycle != 10 || settings.PollInterval.String() != "30s" {
		t.Fatalf("defaults %+v", settings)
	}
	if options.hasAddressOnly || options.oracleKeyFile != "/srv/example/oracle.key" {
		t.Fatalf("key options %+v", options)
	}
}

func TestParseRunOptionsRefusesAddressWithoutDryRun(t *testing.T) {
	_, err := parseTestArgs(t, append(baseArgs(), "--oracle_address=0x00000000000000000000000000000000000000aa"))
	if err == nil || !strings.Contains(err.Error(), "--oracle_address is only for --dry-run") {
		t.Fatalf("parse: %v", err)
	}
	options, err := parseTestArgs(t, append(baseArgs(), "--oracle_address=0x00000000000000000000000000000000000000aa", "--dry-run", "--once"))
	if err != nil {
		t.Fatal(err)
	}
	if !options.hasAddressOnly || !options.settings.DryRun || !options.settings.Once {
		t.Fatalf("options %+v", options)
	}
}

func TestParseRunOptionsRefusesBadValues(t *testing.T) {
	cases := [][]string{
		{"--netuid=0"},
		{"--netuid=70000"},
		{"--state_dir=relative"},
		{"--coordinator=0x0000000000000000000000000000000000000000"},
		{"--poll_interval=0s"},
		{"--max_gas_price_gwei=0"},
		{"--low_balance_wei=-1"},
		{"--metrics_file=relative.prom"},
	}
	for _, c := range cases {
		args := []string{}
		for _, arg := range baseArgs() {
			name := strings.SplitN(c[0], "=", 2)[0]
			if !strings.HasPrefix(arg, name+"=") {
				args = append(args, arg)
			}
		}
		args = append(append(args, c...), "--oracle_key_file=/srv/example/oracle.key")
		if _, err := parseTestArgs(t, args); err == nil {
			t.Errorf("%v accepted", c)
		}
	}
}

func TestEndpointLabelDropsCredentials(t *testing.T) {
	if got := endpointLabel("https://user:secret@rpc.example/path"); got != "https://rpc.example/path" {
		t.Fatalf("label %q", got)
	}
	if got := endpointLabel("ws://127.0.0.1:9944"); got != "ws://127.0.0.1:9944" {
		t.Fatalf("label %q", got)
	}
}
