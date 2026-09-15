package miner

import (
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/docopt/docopt-go"
	"github.com/urnetwork/connect"
)

// Provider process memory.
//
// The default provider has no memory ceiling. Three surfaces are sized here:
//
//   - the per-provider DeviceLocal target (DeviceLocalSettings.
//     MemoryTargetByteCount). Its carrier, NAT and transfer budgets derive
//     from it, and they keep scaling with it: the H3 stream window is 3T/32
//     of the target (6 MiB at 64 MiB, 24 MiB at 256 MiB, 96 MiB at 1 GiB),
//     the connection window T/8, and the provider client's transfer pair is a
//     fraction of the provider share. There is no reference above which a
//     larger target buys nothing.
//   - the connect process budget (connect.SetMemoryBudget). It sizes the
//     transfer share (an eighth of the budget) of every send sequence the
//     device wiring does not give its own pool, and the process-scaled
//     defaults. It used to be left unset because a positive budget switched on
//     the phone's flow caps in connect's provider NAT profile; those caps are
//     now the memory target's alone, so the budget costs the provider nothing.
//   - the Go runtime soft limit (debug.SetMemoryLimit). The runtime holds
//     roughly three bytes per live byte, so a soft limit equal to the summed
//     targets makes GC run continuously once live memory reaches a third of
//     it. The default soft limit is therefore three times the summed targets.
//
// The budget is the soft limit. That keeps the two constraints the default
// plan encodes: the budget is at least three times one target (the runtime
// amplification above), and one target is at most 20/34 of the budget (the
// SDK's split of a process budget into 12 packet pool : 2 large object pool :
// 20 device target parts, see sdk.SetMemoryLimit).
//
// --max-memory keeps its existing meaning: the process soft limit, divided
// evenly into the per-provider targets, whatever the host; the deployment
// that passes it owns that split. It is now also the process budget.

// providerUnknownHostDeviceMemoryTargetByteCount is the per-provider target
// when the usable host memory cannot be read (no /proc/meminfo, no sysctl):
// there is nothing to derive from, so the plan keeps the previous default.
const providerUnknownHostDeviceMemoryTargetByteCount = connect.ByteCount(64 * 1024 * 1024)

// providerMinDeviceMemoryTargetByteCount is the SDK's desktop/server default,
// which every provider got before. A small host never goes below it.
const providerMinDeviceMemoryTargetByteCount = connect.ByteCount(20 * 1024 * 1024)

// providerRuntimeBytesPerLiveByte is the runtime overhead factor applied to
// the summed targets for the default soft limit.
const providerRuntimeBytesPerLiveByte = 3

type providerMemoryPlan struct {
	// per provider device; 0 keeps the SDK default
	DeviceMemoryTargetByteCount connect.ByteCount
	// Go soft limit; 0 leaves it unset
	SoftLimitByteCount connect.ByteCount
	// connect process budget; 0 leaves it unset
	MemoryBudgetByteCount connect.ByteCount
	// one-argument ResizeMessagePools cap; 0 leaves the pools unchanged
	MessagePoolByteCount connect.ByteCount
}

// newProviderAuthMemoryPlan: the auth command is unchanged. Only an explicit
// --max-memory sizes it, with the historical /8 message pool cap.
func newProviderAuthMemoryPlan(explicitMaxMemory connect.ByteCount) providerMemoryPlan {
	if explicitMaxMemory <= 0 {
		return providerMemoryPlan{}
	}
	return providerMemoryPlan{
		SoftLimitByteCount:   explicitMaxMemory,
		MessagePoolByteCount: explicitMaxMemory / 8,
	}
}

// newProviderMemoryPlan sizes the provide command. The provide path never
// resized the message pools and still does not.
//
// Explicit --max-memory: soft limit = budget = max-memory, target =
// max-memory / count.
//
// Absent: no ceiling. target = host / (3 x count) when the usable host memory
// is known, so the default soft limit (3 x count x target) is the host memory
// itself and never exceeds it; never below the previous 20 MiB default. An
// unknown host keeps the previous 64 MiB target. budget = soft limit.
func newProviderMemoryPlan(
	explicitMaxMemory connect.ByteCount,
	hostByteCount connect.ByteCount,
	providerCount int,
) providerMemoryPlan {
	providerCount = max(1, providerCount)
	count := connect.ByteCount(providerCount)
	if 0 < explicitMaxMemory {
		return providerMemoryPlan{
			DeviceMemoryTargetByteCount: explicitMaxMemory / count,
			SoftLimitByteCount:          explicitMaxMemory,
			MemoryBudgetByteCount:       explicitMaxMemory,
		}
	}
	target := providerUnknownHostDeviceMemoryTargetByteCount
	if 0 < hostByteCount {
		target = max(
			hostByteCount/(providerRuntimeBytesPerLiveByte*count),
			providerMinDeviceMemoryTargetByteCount,
		)
	}
	softLimit := providerRuntimeBytesPerLiveByte * count * target
	return providerMemoryPlan{
		DeviceMemoryTargetByteCount: target,
		SoftLimitByteCount:          softLimit,
		MemoryBudgetByteCount:       softLimit,
	}
}

// applyProviderProcessMemory installs the process-wide half of a plan.
func applyProviderProcessMemory(plan providerMemoryPlan) {
	if 0 < plan.MessagePoolByteCount {
		connect.ResizeMessagePools(plan.MessagePoolByteCount)
	}
	if 0 < plan.MemoryBudgetByteCount {
		// before any device exists: the budget is sampled when connect's
		// default settings are constructed
		connect.SetMemoryBudget(plan.MemoryBudgetByteCount)
	}
	if 0 < plan.SoftLimitByteCount {
		debug.SetMemoryLimit(plan.SoftLimitByteCount)
	}
}

// parseProviderMaxMemory returns the explicit --max-memory, or 0 when absent.
func parseProviderMaxMemory(opts docopt.Opts) connect.ByteCount {
	humanReadable, err := opts.String("--max-memory")
	if err != nil {
		return 0
	}
	maxMemory, err := connect.ParseByteCount(humanReadable)
	if err != nil {
		panic(fmt.Errorf("Bad mem argument: %s", humanReadable))
	}
	return maxMemory
}

// effectiveHostMemory is the smaller positive of physical memory and a
// container limit. Either may be unknown (<= 0).
func effectiveHostMemory(physical connect.ByteCount, limit connect.ByteCount) connect.ByteCount {
	if physical <= 0 {
		return max(0, limit)
	}
	if 0 < limit && limit < physical {
		return limit
	}
	return physical
}

// parseMeminfoTotal reads MemTotal (KiB) from /proc/meminfo content.
func parseMeminfoTotal(meminfo string) connect.ByteCount {
	for _, line := range strings.Split(meminfo, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "MemTotal:" {
			continue
		}
		kib, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kib <= 0 {
			return 0
		}
		return connect.ByteCount(kib) * 1024
	}
	return 0
}

// parseCgroupLimit reads a cgroup v2 memory.max or v1 memory.limit_in_bytes.
// "max" and the v1 unlimited sentinel (a page-rounded int64 max) are no limit.
func parseCgroupLimit(content string) connect.ByteCount {
	value := strings.TrimSpace(content)
	if value == "" || value == "max" {
		return 0
	}
	byteCount, err := strconv.ParseInt(value, 10, 64)
	if err != nil || byteCount <= 0 || (1<<62) <= byteCount {
		return 0
	}
	return connect.ByteCount(byteCount)
}
