//go:build windows

package miner

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/urfoundation/sn/v2026/internal/wincustody"
)

// privateDirProblem says when the state folder's owner or permissions are
// ones the miner's key custody refuses (the Windows rule: owned by
// this user, SYSTEM or Administrators, and nobody else may change it).
func privateDirProblem(dir string, _ os.FileInfo) string {
	if err := wincustody.CheckPrivateDirectory(dir); err != nil {
		return displayStateDir() + " can be changed by other users or belongs to another user, so the miner won't keep keys there. Fix: " + privateDirFix(dir)
	}
	return ""
}

// privateDirFix is a PowerShell command that gives the folder (and what it
// holds) to this account and only this account, SYSTEM and Administrators:
// /reset drops every explicit entry, /setowner makes it this account's, then
// inheritance is cut and the three are granted. Accounts are named by SID, so
// it works in any Windows language and never picks a same-named account.
func privateDirFix(dir string) string {
	user := "${env:USERNAME}"
	if token, err := windows.GetCurrentProcessToken().GetTokenUser(); err == nil {
		user = "*" + token.User.Sid.String()
	}
	quoted := "'" + strings.ReplaceAll(dir, "'", "''") + "'"
	return "icacls " + quoted + " /reset /T; icacls " + quoted + " /setowner '" + user + "' /T; icacls " + quoted + ` /inheritance:r /grant:r "` + user + `:(OI)(CI)F" "*S-1-5-18:(OI)(CI)F" "*S-1-5-32-544:(OI)(CI)F" /T`
}

// A system service doesn't exist on Windows (provider service says so).
func rootOnlyProblem(string) string {
	return ""
}
