package miner

// The share limit flags of provide (provide_share.go): what they read through
// the real usage on every provide form, how a run's slots and an
// --all-operators run's children share them, and the slot folders that keep
// them.

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/docopt/docopt-go"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

func parseProvideShareFlagsForTest(t *testing.T, args ...string) (provideShareFlags, error) {
	t.Helper()
	opts, err := docopt.ParseArgs(mainUsage(), args, "synthetic")
	if err != nil {
		t.Fatalf("usage refused %v: %v", args, err)
	}
	return parseProvideShareFlags(opts)
}

// Every provide form takes both flags: 20 Mbps is 2,500,000 bytes a second,
// and the month reads a byte count with its suffix.
func TestProvideShareFlagsReadOnEveryProvideForm(t *testing.T) {
	for _, args := range [][]string{
		{"provide", "--max-share-speed=20", "--monthly-share-limit=500gib"},
		{"auth-provide", "code", "--max-share-speed=20", "--monthly-share-limit=500gib"},
		{"provide", "--all-operators", "--max-share-speed=20", "--monthly-share-limit=500gib"},
	} {
		flags, err := parseProvideShareFlagsForTest(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		want := sdk.ProvideShareLimits{MaxBytesPerSecond: 2500000, MonthlyByteCount: 500 * 1024 * 1024 * 1024}
		if !flags.set() || !flags.speedSet || !flags.monthlySet || flags.limits != want {
			t.Fatalf("%v read %+v, want %+v", args, flags, want)
		}
	}
	flags, err := parseProvideShareFlagsForTest(t, "provide", "--max-share-speed=0.5")
	if err != nil || flags.limits.MaxBytesPerSecond != 62500 || flags.monthlySet {
		t.Fatalf("half a megabit read %+v, %v", flags, err)
	}
	flags, err = parseProvideShareFlagsForTest(t, "provide")
	if err != nil || flags.set() {
		t.Fatalf("no flags read %+v, %v", flags, err)
	}
}

func TestProvideShareFlagsRefuseWhatIsNotACount(t *testing.T) {
	for _, args := range [][]string{
		{"provide", "--max-share-speed=fast"},
		{"provide", "--max-share-speed=-1"},
		{"provide", "--max-share-speed=NaN"},
		{"provide", "--max-share-speed=20000000"},
		{"provide", "--monthly-share-limit=lots"},
		{"provide", "--monthly-share-limit=-5gib"},
	} {
		if flags, err := parseProvideShareFlagsForTest(t, args...); err == nil {
			t.Errorf("%v read %+v, want a refusal", args, flags)
		}
	}
}

// The host's limits split evenly over its slots; a flag not given keeps the
// slot's stored limit, and 0 removes one.
func TestProvideShareFlagsSplitOverTheSlots(t *testing.T) {
	stored := &sdk.ProvideShareLimits{MaxBytesPerSecond: 777, MonthlyByteCount: 999}
	both := provideShareFlags{
		limits:     sdk.ProvideShareLimits{MaxBytesPerSecond: 3000000, MonthlyByteCount: 3 * 1024 * 1024 * 1024},
		speedSet:   true,
		monthlySet: true,
	}
	if limits := both.slotLimits(stored, 3); *limits != (sdk.ProvideShareLimits{MaxBytesPerSecond: 1000000, MonthlyByteCount: 1024 * 1024 * 1024}) {
		t.Fatalf("three slots = %+v", limits)
	}
	if limits := both.slotLimits(nil, 1); *limits != both.limits {
		t.Fatalf("one slot = %+v", limits)
	}
	speedOnly := provideShareFlags{limits: sdk.ProvideShareLimits{MaxBytesPerSecond: 2000000}, speedSet: true}
	if limits := speedOnly.slotLimits(stored, 2); *limits != (sdk.ProvideShareLimits{MaxBytesPerSecond: 1000000, MonthlyByteCount: 999}) {
		t.Fatalf("speed only = %+v, want the stored month kept", limits)
	}
	removed := provideShareFlags{speedSet: true, monthlySet: true}
	if limits := removed.slotLimits(stored, 4); *limits != (sdk.ProvideShareLimits{}) {
		t.Fatalf("0 for both = %+v, want none", limits)
	}
	// a limit never splits into no limit
	tiny := provideShareFlags{limits: sdk.ProvideShareLimits{MonthlyByteCount: 2}, monthlySet: true}
	if limits := tiny.slotLimits(nil, 5); limits.MonthlyByteCount != 1 {
		t.Fatalf("2 bytes over 5 slots = %d, want 1", limits.MonthlyByteCount)
	}
}

// A child's flags read back as exactly its share.
func TestProvideShareFlagsChildArgsReadBackExactly(t *testing.T) {
	flags := provideShareFlags{
		limits:     sdk.ProvideShareLimits{MaxBytesPerSecond: 2500000, MonthlyByteCount: 500 * 1024 * 1024 * 1024},
		speedSet:   true,
		monthlySet: true,
	}
	for _, childCount := range []int{1, 2, 3, 7} {
		args := append([]string{"provide"}, flags.childArgs(childCount)...)
		read, err := parseProvideShareFlagsForTest(t, args...)
		if err != nil {
			t.Fatalf("%d children: %v", childCount, err)
		}
		want := sdk.ProvideShareLimits{
			MaxBytesPerSecond: provideShareSplit(flags.limits.MaxBytesPerSecond, childCount),
			MonthlyByteCount:  provideShareSplit(flags.limits.MonthlyByteCount, childCount),
		}
		if read.limits != want {
			t.Fatalf("%d children read back %+v from %v, want %+v", childCount, read.limits, args, want)
		}
	}
	if args := (provideShareFlags{}).childArgs(3); len(args) != 0 {
		t.Fatalf("no flags passed %v", args)
	}
}

// Each slot keeps its own folder in the state directory, named as its
// credential file is.
func TestProviderShareStorageDirIsTheSlotsOwn(t *testing.T) {
	state := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", state)
	direct, err := providerShareStorageDir(nil)
	if err != nil || direct != filepath.Join(state, ".provide_share", "direct") {
		t.Fatalf("direct folder = %q, %v", direct, err)
	}
	proxy, err := providerShareStorageDir(&connect.ProxySettings{Network: "tcp", Address: "proxy.example:1080"})
	if err != nil || filepath.Dir(proxy) != filepath.Join(state, ".provide_share") || proxy == direct {
		t.Fatalf("proxy folder = %q, %v", proxy, err)
	}
	for _, dir := range []string{direct, proxy} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("folder %s not created: %v", dir, err)
		}
	}
}

// An --all-operators run passes each child its share of the limits, and the
// children restart into new shares when an operator leaves.
func TestOperatorSupervisorSharesTheShareLimitsOverItsProviders(t *testing.T) {
	fixture := newTestOperatorFixture(t)
	fixture.settings.shareFlags = provideShareFlags{
		limits:     sdk.ProvideShareLimits{MaxBytesPerSecond: 3000000, MonthlyByteCount: 3 * 1024 * 1024 * 1024},
		speedSet:   true,
		monthlySet: true,
	}
	domains := []string{"alpha.example", "beta.example", "gamma.example"}
	for _, domain := range domains {
		fixture.authenticate(domain)
	}
	fixture.publish(testListedOperator("alpha.example"), testListedOperator("beta.example"), testListedOperator("gamma.example"))
	fixture.run()
	var children []*testOperatorChild
	for range domains {
		child := fixture.nextStart()
		if child.option("--max-share-speed") != "8" || child.option("--monthly-share-limit") != strconv.Itoa(1024*1024*1024)+"b" {
			t.Fatalf("%s share = %v", child.domain(), child.command.args)
		}
		children = append(children, child)
	}

	fixture.publish(testListedOperator("alpha.example"), testListedOperator("beta.example"))
	for _, child := range children {
		if received := testOperatorReceive(t, child.signals, child.domain()+" stop"); received != syscall.SIGTERM {
			t.Fatalf("%s got %v", child.domain(), received)
		}
		child.exit <- nil
	}
	alpha := fixture.nextStart()
	beta := fixture.nextStart()
	for _, child := range []*testOperatorChild{alpha, beta} {
		if child.option("--max-share-speed") != "12" || child.option("--monthly-share-limit") != strconv.Itoa(1536*1024*1024)+"b" {
			t.Fatalf("%s share after delisting = %v", child.domain(), child.command.args)
		}
	}
	fixture.stop(syscall.SIGTERM, alpha, beta)
}
