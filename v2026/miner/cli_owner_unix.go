//go:build linux || darwin

package miner

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// privateDirProblem says when other users could change the state folder or
// it isn't this user's.
func privateDirProblem(dir string, info os.FileInfo) string {
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Sprintf("%s can be changed by other users, so the miner won't keep keys there. Fix: %s", displayStateDir(), privateDirFix(dir))
	}
	return ownerProblem(dir, info)
}

// privateDirFix is the command that makes the state folder private.
func privateDirFix(dir string) string {
	return "chmod 700 " + cliShellQuote(dir)
}

// ownerProblem says when a folder isn't owned by this user.
func ownerProblem(dir string, info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if ok && int(stat.Uid) != os.Geteuid() {
		return displayStateDir() + " belongs to another user. Fix: sudo chown -R $(id -un) " + cliShellQuote(dir)
	}
	return ""
}

// rootOnlyProblem says when someone other than root could change the file
// or the folders that hold it.
func rootOnlyProblem(path string) string {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err != nil {
			return err.Error()
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if ok && stat.Uid != 0 || info.Mode().Perm()&0022 != 0 && !(info.IsDir() && info.Mode()&os.ModeSticky != 0) {
			return current + " can be changed by a user other than root."
		}
		if parent := filepath.Dir(current); parent == current {
			return ""
		}
	}
}
