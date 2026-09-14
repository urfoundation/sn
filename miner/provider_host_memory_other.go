//go:build !linux && !darwin

package miner

import (
	"github.com/urnetwork/connect"
)

// hostMemoryByteCount is unknown on this platform. Zero keeps the default
// 64 MiB per-provider target unbounded by host memory.
func hostMemoryByteCount() connect.ByteCount {
	return 0
}
