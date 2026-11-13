package commitmentoracle

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gaugeLine(rendered string, name string) string {
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, metricsPrefix+name+" ") {
			return strings.TrimPrefix(line, metricsPrefix+name+" ")
		}
	}
	return ""
}

func TestLowBalanceMetric(t *testing.T) {
	threshold := big.NewInt(10 * 1000 * 1000 * 1000 * 1000 * 1000)
	cases := []struct {
		name    string
		balance *big.Int
		want    string
	}{
		{name: "unknown", balance: nil, want: "1"},
		{name: "empty", balance: new(big.Int), want: "1"},
		{name: "just below", balance: new(big.Int).Sub(threshold, big.NewInt(1)), want: "1"},
		{name: "at threshold", balance: new(big.Int).Set(threshold), want: "0"},
		{name: "funded", balance: new(big.Int).Mul(threshold, big.NewInt(5)), want: "0"},
	}
	for _, c := range cases {
		metrics := newMetrics(Settings{LowBalanceWei: threshold}, time.Unix(100, 0))
		if c.balance != nil {
			metrics.setBalance(c.balance)
		}
		rendered := metrics.render(time.Unix(200, 0))
		if got := gaugeLine(rendered, "oracle_balance_low"); got != c.want {
			t.Errorf("%s: oracle_balance_low %q, want %q", c.name, got, c.want)
		}
		if got := gaugeLine(rendered, "low_balance_threshold_wei"); got != "10000000000000000" {
			t.Errorf("%s: threshold %q", c.name, got)
		}
	}
}

// After a cycle the textfile carries lag, pending, balance and success gauges.
func TestMetricsFileAfterCycle(t *testing.T) {
	dir := t.TempDir()
	settings := testSettings(filepath.Join(dir, "state"))
	if err := os.MkdirAll(settings.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	settings.MetricsFile = filepath.Join(dir, "sn-commitment-oracle.prom")
	harness := newGoldenHarness(t, settings, true)
	harness.evm.balances[harness.oracle.address] = big.NewInt(1000)
	harness.oracle.processedNative = goldenFinalized - 4
	harness.oracle.metrics.lagBlocks = 0
	if err := harness.oracle.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(settings.MetricsFile)
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(encoded)
	want := map[string]string{
		"dry_run":                        "1",
		"oracle_active":                  "1",
		"last_cycle_ok":                  "1",
		"native_finalized_block":         "9286066",
		"native_lag_blocks":              "0",
		"fleet_commitments":              "1",
		"pending_unmirrored":             "1",
		"pending_min_blocks_to_expiry":   "74607",
		"oracle_balance_wei":             "1000",
		"oracle_balance_low":             "1",
		"in_flight":                      "0",
		"last_success_timestamp_seconds": "1760000000",
	}
	for name, value := range want {
		if got := gaugeLine(rendered, name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	info, _ := os.Stat(settings.MetricsFile)
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("metrics file mode %o", info.Mode().Perm())
	}
}

// A failed cycle keeps the lag growing and clears last_cycle_ok.
func TestMetricsLagGrowsWhileCyclesFail(t *testing.T) {
	harness := newGoldenHarness(t, testSettings(t.TempDir()), true)
	if err := harness.oracle.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	harness.oracle.metrics.succeeded()
	harness.evm.paused = true
	harness.native.finalized = nativeHead{number: goldenFinalized + 30, hash: goldenHeadHash()}
	if err := harness.oracle.cycle(context.Background()); err == nil {
		t.Fatal("paused cycle succeeded")
	}
	harness.oracle.metrics.failed(harness.clock.now)
	rendered := harness.oracle.metrics.render(harness.clock.now)
	if gaugeLine(rendered, "native_lag_blocks") != "30" || gaugeLine(rendered, "last_cycle_ok") != "0" || gaugeLine(rendered, "cycle_failures") != "1" {
		t.Fatalf("rendered:\n%s", rendered)
	}
}
