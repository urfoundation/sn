//go:build darwin

package miner

import (
	"golang.org/x/sys/unix"

	"github.com/urnetwork/connect/v2026"
)

func hostMemoryByteCount() connect.ByteCount {
	byteCount, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	return effectiveHostMemory(connect.ByteCount(byteCount), 0)
}
