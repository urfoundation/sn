//go:build !linux && !darwin && !windows

package miner

import (
	"fmt"
	"os"
)

func privateDirProblem(dir string, info os.FileInfo) string {
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Sprintf("%s can be changed by other users, so the miner won't keep keys there. Fix: %s", displayStateDir(), privateDirFix(dir))
	}
	return ""
}

func privateDirFix(dir string) string {
	return "chmod 700 " + cliShellQuote(dir)
}

func rootOnlyProblem(string) string {
	return ""
}
