//go:build linux

package validator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The mount, filesystem UUID and type holding path, as findmnt reports them:
// the longest mount point in /proc/self/mountinfo that contains path, and the
// /dev/disk/by-uuid entry naming its device. Storage preparation checks the
// facts against the host itself; this only finds them.
func detectRuntimeFilesystem(path string) (string, string, string, error) {
	fallback := fmt.Errorf("could not find the filesystem holding %s; pass --mount-path, --filesystem-uuid and --filesystem-type (findmnt -no TARGET,UUID,FSTYPE --target %s)", path, path)
	raw, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", "", "", errors.Join(fallback, err)
	}
	mount, filesystem, source := "", "", ""
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		separator := -1
		for index, field := range fields {
			if field == "-" {
				separator = index
				break
			}
		}
		if len(fields) < 5 || separator < 0 || separator+2 >= len(fields) {
			continue
		}
		point := unescapeMountInfo(fields[4])
		if point != path && point != "/" && !strings.HasPrefix(path, point+"/") {
			continue
		}
		if len(point) >= len(mount) {
			mount, filesystem, source = point, fields[separator+1], fields[separator+2]
		}
	}
	if mount == "" || !strings.HasPrefix(source, "/") {
		return "", "", "", fallback
	}
	device, err := filepath.EvalSymlinks(source)
	if err != nil {
		return "", "", "", errors.Join(fallback, err)
	}
	entries, err := os.ReadDir("/dev/disk/by-uuid")
	if err != nil {
		return "", "", "", errors.Join(fallback, err)
	}
	for _, entry := range entries {
		target, err := filepath.EvalSymlinks(filepath.Join("/dev/disk/by-uuid", entry.Name()))
		if err == nil && target == device {
			return mount, entry.Name(), filesystem, nil
		}
	}
	return "", "", "", fallback
}

// mountinfo escapes space, tab, newline and backslash as octal.
func unescapeMountInfo(value string) string {
	var out strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] == '\\' && index+3 < len(value) {
			if code, err := strconv.ParseUint(value[index+1:index+4], 8, 8); err == nil {
				out.WriteByte(byte(code))
				index += 3
				continue
			}
		}
		out.WriteByte(value[index])
	}
	return out.String()
}
