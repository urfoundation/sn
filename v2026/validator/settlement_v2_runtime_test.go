//go:build linux || darwin

package validator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// VA-3, VA-7: `validator run --config` takes both runtime flags, and
// --ledger_max_bytes defaults to 2 GiB (VERSION2-FINAL.md §6.6).
func TestSettlementV2RuntimeFlagsParse(t *testing.T) {
	t.Parallel()
	keyFile := filepath.Join(t.TempDir(), "auditor.key")
	if err := os.WriteFile(keyFile, []byte(strings.Repeat("2b", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	opts, err := parseValidatorArgsForTest(t, []string{"run", "--config=/tmp/release.yml", "--ledger_auditor_key_file=" + keyFile, "--ledger_max_bytes=4294967296"})
	if err != nil {
		t.Fatal(err)
	}
	options, err := settlementV2RuntimeOptionsFromOpts(opts)
	if err != nil || options != (SettlementV2RuntimeOptions{AuditorKeyFile: keyFile, LedgerMaxBytes: 4 << 30}) {
		t.Fatalf("options %+v: %v", options, err)
	}
	opts, err = parseValidatorArgsForTest(t, []string{"run", "--config=/tmp/release.yml"})
	if err != nil {
		t.Fatal(err)
	}
	options, err = settlementV2RuntimeOptionsFromOpts(opts)
	if err != nil || options != (SettlementV2RuntimeOptions{LedgerMaxBytes: DefaultLedgerMaxBytes}) || DefaultLedgerMaxBytes != 2147483648 {
		t.Fatalf("default options %+v: %v", options, err)
	}
}

// VA-3, VA-7: a ledger budget that is not a positive byte count, or an
// auditor key that does not load, stops `validator run` before it starts.
func TestSettlementV2RuntimeFlagsRefuseInvalidValues(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--ledger_max_bytes=0"},
		{"--ledger_max_bytes=2GiB"},
		{"--ledger_max_bytes=-1"},
		{"--ledger_auditor_key_file=" + filepath.Join(t.TempDir(), "absent.key")},
	} {
		opts, err := parseValidatorArgsForTest(t, append([]string{"run", "--config=/tmp/release.yml"}, args...))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := settlementV2RuntimeOptionsFromOpts(opts); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}
}

// VA-3, VA-7: the flags reach the release runtime through its context;
// without them the runtime sees the defaults.
func TestSettlementV2RuntimeOptionsTravelInContext(t *testing.T) {
	t.Parallel()
	options := SettlementV2RuntimeOptions{AuditorKeyFile: "/keys/auditor", LedgerMaxBytes: 7}
	if got := SettlementV2RuntimeOptionsFrom(WithSettlementV2RuntimeOptions(context.Background(), options)); got != options {
		t.Fatalf("carried options %+v", got)
	}
	if got := SettlementV2RuntimeOptionsFrom(context.Background()); got != (SettlementV2RuntimeOptions{LedgerMaxBytes: DefaultLedgerMaxBytes}) {
		t.Fatalf("default options %+v", got)
	}
}
