package miner

import (
	"os"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"
)

const testMib = connect.ByteCount(1024 * 1024)
const testGib = 1024 * testMib

// TestProviderMemoryPlanNoFlagDefault: without --max-memory the provider has
// no ceiling. Each provider's target is (4/5 x host) / (3 x count), the soft
// limit is 3 x count x target (four fifths of the host), and the process
// budget is the soft limit; never below the previous 20 MiB target; an unknown
// host keeps the previous 64 MiB. Pools are untouched.
func TestProviderMemoryPlanNoFlagDefault(t *testing.T) {
	opts := parseArgsForTest(t, []string{"provide"})
	if explicit := parseProviderMaxMemory(opts); explicit != 0 {
		t.Fatalf("absent flag parsed as %d", explicit)
	}
	// the 80% composition, written out: four fifths of the host, a third of
	// that per provider
	heap := func(host connect.ByteCount) connect.ByteCount { return host * 4 / 5 }
	for _, c := range []struct {
		host   connect.ByteCount
		count  int
		target connect.ByteCount
		soft   connect.ByteCount
	}{
		// unknown host: the previous default
		{0, 1, 64 * testMib, 192 * testMib},
		{0, 0, 64 * testMib, 192 * testMib},
		{0, 4, 64 * testMib, 768 * testMib},
		// known hosts: four fifths of the host over three per provider, no cap
		{4 * testGib, 1, heap(4*testGib) / 3, heap(4*testGib) / 3 * 3},
		{4 * testGib, 4, heap(4*testGib) / 12, heap(4*testGib) / 12 * 12},
		// 8 GiB x 4/5 = 6871947673, over 3
		{8 * testGib, 1, 2290649224, 3 * 2290649224},
		{8 * testGib, 4, heap(8*testGib) / 12, heap(8*testGib) / 12 * 12},
		{32 * testGib, 1, heap(32*testGib) / 3, heap(32*testGib) / 3 * 3},
		{32 * testGib, 4, heap(32*testGib) / 12, heap(32*testGib) / 12 * 12},
		{256 * testGib, 1, heap(256*testGib) / 3, heap(256*testGib) / 3 * 3},
		{256 * testGib, 4, heap(256*testGib) / 12, heap(256*testGib) / 12 * 12},
		// 2 GiB x 4/5 / (3 x 16) = 34.13 MiB
		{2 * testGib, 16, heap(2*testGib) / 48, heap(2*testGib) / 48 * 48},
		// the floor: the soft limit then exceeds the host, as before
		{8 * testGib, 200, 20 * testMib, 12000 * testMib},
		{128 * testMib, 1, heap(128*testMib) / 3, heap(128*testMib) / 3 * 3},
		{32 * testMib, 1, 20 * testMib, 60 * testMib},
	} {
		plan := newProviderMemoryPlan(0, c.host, c.count)
		want := providerMemoryPlan{
			DeviceMemoryTargetByteCount: c.target,
			SoftLimitByteCount:          c.soft,
			MemoryBudgetByteCount:       c.soft,
		}
		if plan != want {
			t.Errorf("host %d count %d: plan %+v, want %+v", c.host, c.count, plan, want)
		}
		assertProviderMemoryPlanConstraints(t, plan, c.host, c.count)
	}

	// no ceiling: the target keeps growing with the host. A cap at any
	// constant would flatten this sequence.
	previous := connect.ByteCount(0)
	for _, host := range []connect.ByteCount{4 * testGib, 8 * testGib, 32 * testGib, 256 * testGib} {
		target := newProviderMemoryPlan(0, host, 1).DeviceMemoryTargetByteCount
		if target <= previous {
			t.Errorf("host %d: target %d did not grow past %d", host, target, previous)
		}
		previous = target
	}
}

// TestProviderMemoryPlanLeavesHostHeadroom: the property asked for. At every
// host size and provider count where the 20 MiB floor does not bind, the
// composed soft limit is at most four fifths of the host, leaving a fifth to
// the operating system and co-resident processes. Where the floor binds (a
// host too small for 20 MiB x 3 per provider) the floor is what exceeds it,
// and that is stated rather than hidden.
func TestProviderMemoryPlanLeavesHostHeadroom(t *testing.T) {
	floorBound := 0
	for _, host := range []connect.ByteCount{
		256 * testMib, 512 * testMib, 1 * testGib, 2 * testGib, 4 * testGib,
		8 * testGib, 16 * testGib, 32 * testGib, 64 * testGib, 128 * testGib, 256 * testGib,
	} {
		for _, count := range []int{1, 2, 3, 4, 8, 16, 64} {
			plan := newProviderMemoryPlan(0, host, count)
			if plan.DeviceMemoryTargetByteCount == providerMinDeviceMemoryTargetByteCount &&
				host*4/5 < plan.SoftLimitByteCount {
				floorBound += 1
				continue
			}
			if host*4/5 < plan.SoftLimitByteCount {
				t.Errorf("host %d count %d: soft limit %d is over four fifths of the host (%d)",
					host, count, plan.SoftLimitByteCount, host*4/5)
			}
			if plan.SoftLimitByteCount < host*4/5-3*connect.ByteCount(count) {
				// integer division loses at most 3 x count bytes; anything
				// more is a formula that is not the 80% composition
				t.Errorf("host %d count %d: soft limit %d is not four fifths of the host (%d)",
					host, count, plan.SoftLimitByteCount, host*4/5)
			}
		}
	}
	// the one exception exists and is the tiny-host floor: 256 MiB across 64
	// providers is 3.2 MiB each, under the 20 MiB floor
	if floorBound == 0 {
		t.Error("no floor-bound row; the grid should include a host too small for its providers")
	}
	if plan := newProviderMemoryPlan(0, 256*testMib, 64); plan.DeviceMemoryTargetByteCount != providerMinDeviceMemoryTargetByteCount {
		t.Errorf("256 MiB / 64 providers: target %d, want the %d floor", plan.DeviceMemoryTargetByteCount, providerMinDeviceMemoryTargetByteCount)
	}
}

// assertProviderMemoryPlanConstraints checks the two constraints every
// derived plan keeps: the budget is at least three times one target (the
// runtime amplification), one target is at most 20/34 of the budget (the
// SDK's pool : target split of a process budget), and the soft limit does not
// exceed four fifths of a known host unless the 20 MiB floor forced it.
func assertProviderMemoryPlanConstraints(
	t *testing.T,
	plan providerMemoryPlan,
	host connect.ByteCount,
	count int,
) {
	t.Helper()
	target := plan.DeviceMemoryTargetByteCount
	budget := plan.MemoryBudgetByteCount
	if budget < providerRuntimeBytesPerLiveByte*target {
		t.Errorf("host %d count %d: budget %d is under 3 x target %d", host, count, budget, target)
	}
	if budget/34*20 < target {
		t.Errorf("host %d count %d: target %d is over 20/34 of budget %d", host, count, target, budget)
	}
	if budget != plan.SoftLimitByteCount {
		t.Errorf("host %d count %d: budget %d is not the soft limit %d", host, count, budget, plan.SoftLimitByteCount)
	}
	if 0 < host && providerMinDeviceMemoryTargetByteCount < target && host*4/5 < plan.SoftLimitByteCount {
		t.Errorf("host %d count %d: soft limit %d exceeds four fifths of the host", host, count, plan.SoftLimitByteCount)
	}
}

// TestProviderMemoryPlanExplicitFlagKeepsMeaning: --max-memory is still the
// soft limit and is divided into the per-provider targets, whatever the host;
// it is also the process budget.
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
		for _, host := range []connect.ByteCount{0, 512 * testMib, 64 * testGib} {
			plan := newProviderMemoryPlan(explicit, host, c.count)
			want := providerMemoryPlan{
				DeviceMemoryTargetByteCount: c.target,
				SoftLimitByteCount:          2 * testGib,
				MemoryBudgetByteCount:       2 * testGib,
			}
			if plan != want {
				t.Errorf("count %d host %d: plan %+v, want %+v", c.count, host, plan, want)
			}
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

// TestApplyProviderMemoryPlan installs the soft limit and the process budget,
// and resizes pools only when the plan says so; the auth plan sets no budget.
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
	if got := connect.MemoryBudget(); got != 384*testMib {
		t.Fatalf("process budget = %d, want %d", got, 384*testMib)
	}
	// the budget bought no flow caps: the provider NAT profile built under it
	// is the unlimited one the unbudgeted provider had
	if nat := connect.DefaultProviderLocalUserNatSettings(); nat.TcpBufferSettings.GlobalLimit != 0 ||
		nat.UdpBufferSettings.GlobalLimit != 0 ||
		nat.UdpBufferSettings.IdleTimeout != 300*time.Second {
		t.Fatalf("the process budget changed the provider NAT profile: tcp %d udp %d idle %s",
			nat.TcpBufferSettings.GlobalLimit, nat.UdpBufferSettings.GlobalLimit,
			nat.UdpBufferSettings.IdleTimeout)
	}
	connect.SetMemoryBudget(0)

	defer connect.ResizeMessagePools(
		connect.InitialMessagePoolByteCount/3,
		connect.InitialMessagePoolByteCount-connect.InitialMessagePoolByteCount/3,
	)
	applyProviderProcessMemory(newProviderAuthMemoryPlan(512 * testMib))
	if got := poolCapacity(); got == poolsBefore {
		t.Fatal("auth plan did not resize pools")
	}
	if got := connect.MemoryBudget(); got != 0 {
		t.Fatalf("auth plan set process budget %d", got)
	}
}

// TestProviderTargetIsAppliedToTheDevice: the plan's target is the device
// target; a zero target keeps the SDK default.
func TestProviderTargetIsAppliedToTheDevice(t *testing.T) {
	settings := sdk.DefaultDeviceLocalSettings()
	sdkDefault := settings.MemoryTargetByteCount
	applyProviderMemoryTarget(settings, 0)
	if settings.MemoryTargetByteCount != sdkDefault {
		t.Fatalf("zero target replaced the SDK default %d with %d", sdkDefault, settings.MemoryTargetByteCount)
	}
	plan := newProviderMemoryPlan(0, 8*testGib, 1)
	applyProviderMemoryTarget(settings, plan.DeviceMemoryTargetByteCount)
	if settings.MemoryTargetByteCount != sdk.ByteCount(plan.DeviceMemoryTargetByteCount) {
		t.Fatalf("device target = %d, want %d", settings.MemoryTargetByteCount, plan.DeviceMemoryTargetByteCount)
	}
}

// TestProviderTargetHasNoWindowCeiling: the premise that justified the old
// 64 MiB cap ("connect's window scale tops out at the 64 MiB reference") is
// false — the H3 windows are fractions of the target and keep growing above
// it — and the text is gone from the plan's source.
func TestProviderTargetHasNoWindowCeiling(t *testing.T) {
	at64 := connect.DefaultPlatformTransportSettingsWithMemoryTarget(64 * testMib)
	at256 := connect.DefaultPlatformTransportSettingsWithMemoryTarget(256 * testMib)
	if at256.H3MaxStreamReceiveWindowByteCount <= at64.H3MaxStreamReceiveWindowByteCount ||
		at256.H3MaxConnectionReceiveWindowByteCount <= at64.H3MaxConnectionReceiveWindowByteCount {
		t.Fatalf("H3 windows do not grow above 64 MiB: stream %d -> %d, conn %d -> %d",
			at64.H3MaxStreamReceiveWindowByteCount, at256.H3MaxStreamReceiveWindowByteCount,
			at64.H3MaxConnectionReceiveWindowByteCount, at256.H3MaxConnectionReceiveWindowByteCount)
	}

	source, err := os.ReadFile("provider_memory.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, stale := range []string{"tops out", "buys no larger"} {
		if strings.Contains(string(source), stale) {
			t.Errorf("provider_memory.go still carries the stale premise %q", stale)
		}
	}
	if !strings.Contains(string(source), "no memory ceiling") {
		t.Error("provider_memory.go does not state the no-ceiling default")
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
