package onchain

import (
	"testing"

	docopt "github.com/docopt/docopt-go"
)

// parseArgv runs the real usage string through docopt without the default
// exit-on-error help handler.
func parseArgv(t *testing.T, argv []string) (docopt.Opts, error) {
	t.Helper()
	parser := &docopt.Parser{HelpHandler: func(err error, usage string) {}}
	return parser.ParseArgs(usage, argv, "snclaim test")
}

// TestUsageParses exercises the three documented command forms plus the
// repeatable --rpc failover list, and checks that mixing raw-calldata and
// structured submit arguments is rejected.
func TestUsageParses(t *testing.T) {
	// submit, raw-calldata mode.
	opts, err := parseArgv(t, []string{
		"submit",
		"--calldata=" + goldenClaimCalldata,
		"--contract=0x0102030405060708090a0b0c0d0e0f1011121314",
		"--rpc=https://a.example", "--rpc=https://b.example",
		"--key_file=/tmp/k",
		"--chain_id=945", "--gas_limit=200000", "--dry-run",
	})
	if err != nil {
		t.Fatalf("raw submit usage: %v", err)
	}
	if !boolOpt(opts, "submit") || boolOpt(opts, "status") {
		t.Fatalf("raw submit: command flags wrong: %v", opts)
	}
	if got := strsOpt(opts, "--rpc"); len(got) != 2 || got[0] != "https://a.example" || got[1] != "https://b.example" {
		t.Fatalf("raw submit: --rpc order/failover list = %v", got)
	}
	if !boolOpt(opts, "--dry-run") || strOpt(opts, "--chain_id") != "945" || strOpt(opts, "--gas_limit") != "200000" {
		t.Fatalf("raw submit: option values wrong: %v", opts)
	}
	if !hasOpt(opts, "--calldata") || hasOpt(opts, "--epoch") {
		t.Fatalf("raw submit: mode detection keys wrong: %v", opts)
	}

	// submit, structured mode (empty --proof = single-leaf tree).
	opts, err = parseArgv(t, []string{
		"submit",
		"--epoch=7", "--no_id=3", "--coldkey=" + aliceSS58, "--share_bps=1234", "--proof=",
		"--contract=0x0102030405060708090a0b0c0d0e0f1011121314",
		"--rpc=https://a.example",
		"--key_file=/tmp/k",
	})
	if err != nil {
		t.Fatalf("structured submit usage: %v", err)
	}
	if hasOpt(opts, "--calldata") || !hasOpt(opts, "--epoch") {
		t.Fatalf("structured submit: mode detection keys wrong: %v", opts)
	}
	if strOpt(opts, "--proof") != "" || !hasOpt(opts, "--proof") {
		t.Fatalf("structured submit: empty --proof handling wrong: %v", opts)
	}

	// status, with and without --coldkey.
	for _, argv := range [][]string{
		{"status", "--epoch=7", "--no_id=3", "--contract=0x0102030405060708090a0b0c0d0e0f1011121314", "--rpc=u"},
		{"status", "--epoch=7", "--no_id=3", "--coldkey=" + aliceHex, "--contract=0x0102030405060708090a0b0c0d0e0f1011121314", "--rpc=u"},
	} {
		opts, err = parseArgv(t, argv)
		if err != nil {
			t.Fatalf("status usage %v: %v", argv, err)
		}
		if !boolOpt(opts, "status") {
			t.Fatalf("status: command flag missing: %v", opts)
		}
	}

	// Mixing the two submit modes must not match any pattern.
	if _, err := parseArgv(t, []string{
		"submit", "--calldata=0x4c207962", "--epoch=7",
		"--contract=0x0102030405060708090a0b0c0d0e0f1011121314", "--rpc=u", "--key_file=/tmp/k",
	}); err == nil {
		t.Fatal("mixed raw+structured submit unexpectedly parsed")
	}

	// Missing required --key_file on submit must fail.
	if _, err := parseArgv(t, []string{
		"submit", "--calldata=0x4c207962",
		"--contract=0x0102030405060708090a0b0c0d0e0f1011121314", "--rpc=u",
	}); err == nil {
		t.Fatal("submit without --key_file unexpectedly parsed")
	}
}
