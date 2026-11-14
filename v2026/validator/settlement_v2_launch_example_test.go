//go:build linux || darwin

package validator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// VA-3, VERSION2-LAUNCH.md step 10: the auditor drop-in's ExecStart parses
// with `validator run`'s real usage in release config mode, and its
// --ledger_auditor_key_file loads the key that sends approvals, challenges
// and demands. The same command line without the flag, as the repository's
// unit runs it, sends none.
func TestSettlementV2LaunchAuditorDropInParses(t *testing.T) {
	raw, err := os.ReadFile("../mainnet/validator-ledger-auditor.example.conf")
	if err != nil {
		t.Fatal(err)
	}
	start := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if value, found := strings.CutPrefix(line, "ExecStart="); found && value != "" {
			start = value
		}
	}
	keyFile := filepath.Join(t.TempDir(), "auditor.key")
	if err := os.WriteFile(keyFile, []byte(strings.Repeat("2b", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	argv := strings.Fields(strings.ReplaceAll(start, "<O10-auditor-key-file>", keyFile))
	opts, err := parseValidatorArgsForTest(t, argv[1:])
	if err != nil || argv[0] != "<validator-binary>" || argv[len(argv)-1] != "--ledger_auditor_key_file="+keyFile || !optBool(opts, "run") || optString(opts, "--config", "") != "<adopted-config>" || optString(opts, "--progress-file", "") != "<progress-file>" ||
		optString(opts, "--durable-volumes", "") != "<validator-declaration>" || optString(opts, "--durable-volumes-sha256", "") != "<validator-declaration-sha256>" {
		t.Fatalf("drop-in ExecStart %q = %v, %v", argv, opts, err)
	}
	options, err := settlementV2RuntimeOptionsFromOpts(opts)
	if err != nil || options != (SettlementV2RuntimeOptions{AuditorKeyFile: keyFile, LedgerMaxBytes: DefaultLedgerMaxBytes}) {
		t.Fatalf("drop-in auditor options %+v: %v", options, err)
	}
	opts, err = parseValidatorArgsForTest(t, argv[1:len(argv)-1])
	if err != nil {
		t.Fatal(err)
	}
	if options, err := settlementV2RuntimeOptionsFromOpts(opts); err != nil || options.AuditorKeyFile != "" {
		t.Fatalf("rendered unit options %+v: %v", options, err)
	}
}
