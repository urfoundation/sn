package miner

import (
	"runtime/debug"
	"testing"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"
)

const testMib = connect.ByteCount(1024 * 1024)
const testGib = 1024 * testMib

// TestProviderMemoryPlanNoFlagDefault: without --max-memory each provider gets
// the 64 MiB target and a soft limit of 3 x count x target, bounded by a known
// host but never below the previous 20 MiB target. Pools are untouched.
func TestProviderMemoryPlanNoFlagDefault(t *testing.T) {
	opts := parseArgsForTest(t, []string{"provide"})
	if explicit := parseProviderMaxMemory(opts); explicit != 0 {
		t.Fatalf("absent flag parsed as %d", explicit)
	}
	for _, c := range []struct {
		host   connect.ByteCount
		count  int
		target connect.ByteCount
		soft   connect.ByteCount
	}{
		{0, 1, 64 * testMib, 192 * testMib},
		{0, 0, 64 * testMib, 192 * testMib},
		{0, 4, 64 * testMib, 768 * testMib},
		{8 * testGib, 1, 64 * testMib, 192 * testMib},
		{8 * testGib, 4, 64 * testMib, 768 * testMib},
		// 2 GiB / (3 x 16) = 42.66 MiB
		{2 * testGib, 16, 2 * testGib / 48, 2 * testGib / 48 * 48},
		{8 * testGib, 200, 20 * testMib, 12000 * testMib},
		{128 * testMib, 1, 42*testMib + 682*1024 + 682, 3 * (42*testMib + 682*1024 + 682)},
		{32 * testMib, 1, 20 * testMib, 60 * testMib},
	} {
		plan := newProviderMemoryPlan(0, c.host, c.count)
		want := providerMemoryPlan{DeviceMemoryTargetByteCount: c.target, SoftLimitByteCount: c.soft}
		if plan != want {
			t.Errorf("host %d count %d: plan %+v, want %+v", c.host, c.count, plan, want)
		}
	}
}

// TestProviderMemoryPlanExplicitFlagKeepsMeaning: --max-memory is still the
// soft limit and is divided into the per-provider targets, whatever the host.
func TestProviderMemoryPlanExplicitFlagKeepsMeaning(t *testing.T) {
	opts := parseArgsForTest(t, []string{"provide", "--max-memory=2gib"})
	explicit := parseProviderMaxMemory(opts)
	for _, c := range []struct {
		count  int
		target connect.ByteCount
	}{
		{1, 2 * testGib},
		{4, 512 * testMib},
		{0, 2 * testGib},
	} {
		plan := newProviderMemoryPlan(explicit, 64*testGib, c.count)
		want := providerMemoryPlan{DeviceMemoryTargetByteCount: c.target, SoftLimitByteCount: 2 * testGib}
		if plan != want {
			t.Errorf("count %d: plan %+v, want %+v", c.count, plan, want)
		}
	}
}

// TestProviderAuthMemoryPlanKeepsPoolSizing: auth keeps the /8 pool cap and
// the soft limit for an explicit flag, and does nothing without one.
func TestProviderAuthMemoryPlanKeepsPoolSizing(t *testing.T) {
	if plan := newProviderAuthMemoryPlan(0); plan != (providerMemoryPlan{}) {
		t.Fatalf("no-flag auth plan = %+v", plan)
	}
	want := providerMemoryPlan{SoftLimitByteCount: 512 * testMib, MessagePoolByteCount: 64 * testMib}
	if plan := newProviderAuthMemoryPlan(512 * testMib); plan != want {
		t.Fatalf("auth plan = %+v, want %+v", plan, want)
	}
}

// TestApplyProviderMemoryPlan installs the soft limit, resizes pools only when
// the plan says so, and leaves connect's process budget unset.
func TestApplyProviderMemoryPlan(t *testing.T) {
	previousSoftLimit := debug.SetMemoryLimit(-1)
	defer debug.SetMemoryLimit(previousSoftLimit)
	previousBudget := connect.MemoryBudget()
	defer connect.SetMemoryBudget(previousBudget)
	connect.SetMemoryBudget(0)

	poolCapacity := func() connect.ByteCount {
		return connect.GetMessagePoolAggregateStats().CapacityByteCount
	}
	poolsBefore := poolCapacity()
	applyProviderProcessMemory(newProviderMemoryPlan(0, 0, 2))
	if got := debug.SetMemoryLimit(-1); got != int64(384*testMib) {
		t.Fatalf("soft limit = %d, want %d", got, 384*testMib)
	}
	if got := poolCapacity(); got != poolsBefore {
		t.Fatalf("provide plan resized pools %d -> %d", poolsBefore, got)
	}
	if got := connect.MemoryBudget(); got != 0 {
		t.Fatalf("provide plan set process budget %d", got)
	}

	defer connect.ResizeMessagePools(
		connect.InitialMessagePoolByteCount/3,
		connect.InitialMessagePoolByteCount-connect.InitialMessagePoolByteCount/3,
	)
	applyProviderProcessMemory(newProviderAuthMemoryPlan(512 * testMib))
	if got := poolCapacity(); got == poolsBefore {
		t.Fatal("auth plan did not resize pools")
	}
}

// TestProviderDefaultTargetReachesUnscaledWindows: 64 MiB is the smallest
// target with the full H3 windows; 20 MiB (the old default) is scaled down.
func TestProviderDefaultTargetReachesUnscaledWindows(t *testing.T) {
	settings := sdk.DefaultDeviceLocalSettings()
	applyProviderMemoryTarget(settings, providerDefaultDeviceMemoryTargetByteCount)
	if settings.MemoryTargetByteCount != sdk.ByteCount(64*testMib) {
		t.Fatalf("device target = %d", settings.MemoryTargetByteCount)
	}
	full := connect.DefaultPlatformTransportSettingsWithMemoryTarget(64 * testMib)
	if full.H3MaxStreamReceiveWindowByteCount != 3*testMib ||
		full.H3MaxConnectionReceiveWindowByteCount != 4*testMib ||
		full.H3BudgetByteCount != 8*testMib {
		t.Fatalf("64 MiB H3 = stream %d conn %d budget %d",
			full.H3MaxStreamReceiveWindowByteCount,
			full.H3MaxConnectionReceiveWindowByteCount,
			full.H3BudgetByteCount)
	}
	above := connect.DefaultPlatformTransportSettingsWithMemoryTarget(1 * testGib)
	if above.H3MaxConnectionReceiveWindowByteCount != full.H3MaxConnectionReceiveWindowByteCount {
		t.Fatal("a target above 64 MiB changed the H3 window")
	}
	old := connect.DefaultPlatformTransportSettingsWithMemoryTarget(20 * testMib)
	if old.H3MaxConnectionReceiveWindowByteCount != 1310720 {
		t.Fatalf("20 MiB H3 conn window = %d, want 1310720", old.H3MaxConnectionReceiveWindowByteCount)
	}
}

func TestHostMemoryParsing(t *testing.T) {
	if got := parseMeminfoTotal("MemFree: 1 kB\nMemTotal:       8039936 kB\n"); got != 8039936*1024 {
		t.Fatalf("MemTotal = %d", got)
	}
	if got := parseMeminfoTotal("garbage"); got != 0 {
		t.Fatalf("garbage MemTotal = %d", got)
	}
	for content, want := range map[string]connect.ByteCount{
		"max\n":                 0,
		"":                      0,
		"536870912\n":           536870912,
		"9223372036854771712\n": 0,
	} {
		if got := parseCgroupLimit(content); got != want {
			t.Errorf("cgroup %q = %d, want %d", content, got, want)
		}
	}
	if got := effectiveHostMemory(8*testGib, 512*testMib); got != 512*testMib {
		t.Fatalf("container limit not applied: %d", got)
	}
	if got := effectiveHostMemory(0, 0); got != 0 {
		t.Fatalf("unknown = %d", got)
	}
}
