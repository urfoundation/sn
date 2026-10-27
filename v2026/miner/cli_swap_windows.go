//go:build windows

package miner

import "github.com/urfoundation/sn/v2026/internal/winservice"

// replaceMinerBinary renames the running binary aside, as executable.previous,
// and the new one into its place: Windows can rename a running program but
// not replace it. Between the two renames, for an instant, no binary is at
// executable.
func replaceMinerBinary(newPath string, executable string) error {
	return winservice.ReplaceExecutable(newPath, executable)
}

// rollBackMiner puts executable.previous back, setting the new one aside.
func rollBackMiner(executable string) error {
	return winservice.RestorePrevious(executable)
}
