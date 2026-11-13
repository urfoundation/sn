//go:build linux

// Linux fixture volumes claim ext4 and report the full 64-bit dev_t.
package durablefixture

import "golang.org/x/sys/unix"

// The synthetic volume claims the platform's qualified filesystem.
const filesystemType = "ext4"

// The statfs magic the host reports for the claimed filesystem.
const filesystemMagic = 0xef53

// Linux reports the full 64-bit dev_t.
func rawDevice(stat *unix.Stat_t) uint64 { return stat.Dev }
