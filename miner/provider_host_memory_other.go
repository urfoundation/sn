//go:build !linux && !darwin

package miner

import (
	"github.com/urnetwork/connect"
)

// hostMemoryByteCount is unknown on this platform. Zero keeps the unknown-host
// 64 MiB per-provider target (providerUnknownHostDeviceMemoryTargetByteCount)
// since there is no host memory to derive from.
func hostMemoryByteCount() connect.ByteCount {
	return 0
}
