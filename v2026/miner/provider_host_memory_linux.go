//go:build linux

package miner

import (
	"os"

	"github.com/urnetwork/connect/v2026"
)

// hostMemoryByteCount is the memory this process can actually use: the
// smaller of physical memory and any cgroup limit, so a provider started in a
// memory-limited container sizes itself to the container, not the host.
func hostMemoryByteCount() connect.ByteCount {
	physical := parseMeminfoTotal(readSmallFile("/proc/meminfo"))
	limit := parseCgroupLimit(readSmallFile("/sys/fs/cgroup/memory.max"))
	if limit <= 0 {
		limit = parseCgroupLimit(readSmallFile("/sys/fs/cgroup/memory/memory.limit_in_bytes"))
	}
	return effectiveHostMemory(physical, limit)
}

func readSmallFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
