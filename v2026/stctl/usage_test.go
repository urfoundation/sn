package main

import (
	"testing"

	"github.com/docopt/docopt-go"
)

// parseArgv parses argv against the real usage string without exiting on
// mismatch (NoHelpHandler returns the error instead).
func parseArgv(t *testing.T, argv []string) (docopt.Opts, error) {
	t.Helper()
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler}
	return parser.ParseArgs(mainUsage(), argv, "")
}

// TestUsageParses drives every subcommand's argv through the docopt usage
// string and checks flag binding.
func TestUsageParses(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		bool string
		str  map[string]string
	}{
		{
			name: "deploy-status",
			argv: []string{"deploy-status"},
			bool: "deploy-status",
		},
		{
			name: "deploy-status with config",
			argv: []string{"deploy-status", "--config=/tmp/x.yml"},
			bool: "deploy-status",
			str:  map[string]string{"--config": "/tmp/x.yml"},
		},
		{
			name: "register-operator",
			argv: []string{"register-operator", "--no_id=1", "--coldkey=5F...", "--miner_hotkey=5G..."},
			bool: "register-operator",
			str:  map[string]string{"--no_id": "1", "--coldkey": "5F...", "--miner_hotkey": "5G..."},
		},
		{
			name: "deposit with push",
			argv: []string{"deposit", "--no_id=1", "--alpha=1000000000", "--push"},
			bool: "--push",
			str:  map[string]string{"--alpha": "1000000000"},
		},
		{
			name: "commit-root",
			argv: []string{"commit-root", "--epoch=3", "--no_id=1", "--root=0xaa", "--off=0xbb"},
			bool: "commit-root",
			str:  map[string]string{"--epoch": "3", "--root": "0xaa", "--off": "0xbb"},
		},
		{
			name: "initialize minimal",
			argv: []string{"initialize", "--owner=0xabc", "--treasury_hotkey=5F...", "--reserve_hotkey=5G..."},
			bool: "initialize",
			str: map[string]string{
				"--owner":           "0xabc",
				"--treasury_hotkey": "5F...",
				"--reserve_hotkey":  "5G...",
			},
		},
		{
			name: "initialize full",
			argv: []string{
				"initialize", "--owner=0xabc", "--guardian=0xdef",
				"--treasury_hotkey=0x11", "--reserve_hotkey=0x22",
				"--t_epoch=300", "--commit_window=50", "--trails_window=100",
				"--finalize_offset=150", "--self_coldkey=0x33",
			},
			bool: "initialize",
			str: map[string]string{
				"--guardian":        "0xdef",
				"--t_epoch":         "300",
				"--trails_window":   "100",
				"--finalize_offset": "150",
				"--self_coldkey":    "0x33",
			},
		},
		{
			name: "finalize",
			argv: []string{"finalize", "--epoch=3"},
			bool: "finalize",
			str:  map[string]string{"--epoch": "3"},
		},
		{
			name: "claim-miner",
			argv: []string{"claim-miner", "--epoch=3", "--no_id=1", "--coldkey=5F...", "--share_bps=100", "--proof=aa,bb"},
			bool: "claim-miner",
			str:  map[string]string{"--share_bps": "100", "--proof": "aa,bb"},
		},
		{
			name: "epoch",
			argv: []string{"epoch"},
			bool: "epoch",
		},
		{
			name: "state with filters",
			argv: []string{"state", "--epoch=3", "--no_id=1"},
			bool: "state",
			str:  map[string]string{"--epoch": "3", "--no_id": "1"},
		},
		{
			name: "evm-address",
			argv: []string{"evm-address", "0x00112233445566778899aabbccddeeff00112233"},
			bool: "evm-address",
			str:  map[string]string{"<h160>": "0x00112233445566778899aabbccddeeff00112233"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseArgv(t, tc.argv)
			if err != nil {
				t.Fatalf("argv %v rejected: %v", tc.argv, err)
			}
			if on, _ := opts.Bool(tc.bool); !on {
				t.Errorf("argv %v: %q not set", tc.argv, tc.bool)
			}
			for name, want := range tc.str {
				if got := stringOpt(opts, name); got != want {
					t.Errorf("argv %v: %s = %q, want %q", tc.argv, name, got, want)
				}
			}
		})
	}
}

// TestUsageRejects checks that incomplete argvs do not match.
func TestUsageRejects(t *testing.T) {
	cases := [][]string{
		{},                           // no command
		{"deposit"},                  // missing --no_id/--alpha
		{"deposit", "--no_id=1"},     // missing --alpha
		{"finalize"},                 // missing --epoch
		{"evm-address"},              // missing <h160>
		{"claim-miner", "--epoch=1"}, // missing the rest
		{"initialize", "--owner=0xabc", "--treasury_hotkey=0x11"},         // missing --reserve_hotkey
		{"initialize", "--treasury_hotkey=0x11", "--reserve_hotkey=0x22"}, // missing --owner
		{"bogus-command", "--epoch=1"},                                    // unknown command
	}
	for _, argv := range cases {
		if _, err := parseArgv(t, argv); err == nil {
			t.Errorf("argv %v unexpectedly matched the usage", argv)
		}
	}
}

// TestVerbosityCounts checks the -v... counting convention.
func TestVerbosityCounts(t *testing.T) {
	opts, err := parseArgv(t, []string{"epoch", "-vv"})
	if err != nil {
		t.Fatalf("epoch -vv rejected: %v", err)
	}
	// docopt stores the count as an int (Opts.Int only handles strings)
	if n, ok := opts["-v"].(int); !ok || n != 2 {
		t.Errorf("-vv parsed as %v, want 2", opts["-v"])
	}
}
