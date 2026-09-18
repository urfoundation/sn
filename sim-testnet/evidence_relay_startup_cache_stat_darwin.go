//go:build darwin

package main

import (
	"errors"
	"os"
	"syscall"
)

func evidenceRelayStartupFileIdentity(info os.FileInfo) (uint64, uint64, uint32, int64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, 0, 0, errors.New("relay startup file has no Darwin identity")
	}
	return uint64(stat.Dev), stat.Ino, stat.Uid, stat.Ctimespec.Sec*1_000_000_000 + stat.Ctimespec.Nsec, nil
}
