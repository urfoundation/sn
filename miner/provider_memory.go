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
// Two surfaces are sized here:
//
//   - the per-provider DeviceLocal target (DeviceLocalSettings.
//     MemoryTargetByteCount). Its carrier, NAT and transfer budgets derive
//     from it. connect's window scale tops out at the 64 MiB reference, so a
//     target above 64 MiB buys no larger H3 windows.
//   - the Go runtime soft limit (debug.SetMemoryLimit). The runtime holds
//     roughly three bytes per live byte, so a soft limit equal to the summed
//     targets makes GC run continuously once live memory reaches a third of
//     it. The default soft limit is therefore three times the summed targets.
//
// connect.SetMemoryBudget is deliberately not set. At 64 MiB it changes no
// scaled constant and no process carrier budget (both identical to an unset
// budget), and its only effect would be to switch on the per-process flow
// caps in connect's generic UDP/TCP/ICMP buffer settings.
//
// --max-memory keeps its existing meaning exactly: the process soft limit,
// divided evenly into the per-provider targets. Only its absence changes.

// providerDefaultDeviceMemoryTargetByteCount is connect's scaling reference:
// the smallest target that reaches the unscaled windows.
const providerDefaultDeviceMemoryTargetByteCount = connect.ByteCount(64 * 1024 * 1024)

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
// Explicit --max-memory: soft limit = max-memory, target = max-memory / count.
//
// Absent: target = 64 MiB per provider, bounded when the usable host memory is
// known so the default soft limit fits it (host / (3 x count)), but never
// below the previous 20 MiB default; soft limit = 3 x count x target.
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
		}
	}
	target := providerDefaultDeviceMemoryTargetByteCount
	if 0 < hostByteCount {
		target = min(target, hostByteCount/(providerRuntimeBytesPerLiveByte*count))
		target = max(target, providerMinDeviceMemoryTargetByteCount)
	}
	return providerMemoryPlan{
		DeviceMemoryTargetByteCount: target,
		SoftLimitByteCount:          providerRuntimeBytesPerLiveByte * count * target,
	}
}

// applyProviderProcessMemory installs the process-wide half of a plan.
func applyProviderProcessMemory(plan providerMemoryPlan) {
	if 0 < plan.MessagePoolByteCount {
		connect.ResizeMessagePools(plan.MessagePoolByteCount)
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
