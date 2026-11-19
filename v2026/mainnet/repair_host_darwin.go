//go:build darwin

// Validator and operator repair manage systemd units and cgroup v2 on Linux
// service hosts. Darwin has neither, and its ACLs are not POSIX access xattrs,
// so these facts refuse rather than report an empty cgroup or an absent ACL.
package main

import (
	"errors"
	"syscall"
)

// No Darwin filesystem can claim the unified cgroup identity.
const repairValidatorCgroup2Magic = -1

func repairValidatorCgroupType(string) (int64, error) {
	return 0, errors.New("validator repair requires a Linux unified cgroup host")
}

func repairOperatorAccessAcl(string) (bool, error) {
	return false, errors.New("operator resource access policy requires Linux POSIX access ACLs")
}

// Darwin names the native change time Ctimespec.
func repairStatChangeTime(stat *syscall.Stat_t) syscall.Timespec { return stat.Ctimespec }
