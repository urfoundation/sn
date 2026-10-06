//go:build linux

// Linux host facts for validator and operator repair: unified cgroup v2
// identity, POSIX access ACLs and native change times.
package main

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

// The statfs magic of a genuine unified cgroup filesystem.
const repairValidatorCgroup2Magic = unix.CGROUP2_SUPER_MAGIC

// A missing path in v1, a hybrid layout or an unmounted filesystem proves
// nothing about a descendant process. Public selection requires genuine v2.
func repairValidatorCgroupType(path string) (int64, error) {
	var state unix.Statfs_t
	if err := unix.Statfs(path, &state); err != nil {
		return 0, err
	}
	return int64(state.Type), nil
}

// An access ACL can grant what mode bits deny. Absence is ENODATA, or ENOTSUP
// on a filesystem without POSIX ACLs; any other failure observes nothing.
func repairOperatorAccessAcl(path string) (bool, error) {
	_, err := unix.Getxattr(path, "system.posix_acl_access", nil)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) {
		return false, nil
	}
	return false, err
}

// Linux names the native change time Ctim.
func repairStatChangeTime(stat *syscall.Stat_t) syscall.Timespec { return stat.Ctim }
