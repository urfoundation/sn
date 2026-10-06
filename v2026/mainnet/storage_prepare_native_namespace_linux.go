//go:build linux

package main

import (
	"path/filepath"
	"strings"
)

// Containment precedes member admission. A native approval inside an original
// root cannot become an external live input because another owner's narrower
// snapshot grammar rejects its path. The complete inventory admits its bytes.
func storageNativeRestoreRelative(root, path string) (string, bool) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !bootstrapRootAbsolutePath(path) {
		return "", false
	}
	relative, err := filepath.Rel(root, path)
	return relative, err == nil && relative != ".." && !strings.HasPrefix(relative, "../") && !filepath.IsAbs(relative)
}

// Native files retain the durable volume's finite child namespace. Monitor
// snapshots keep their separate 1024-byte reference and 155-byte leaf limits.
func storageNativeRestorePath(path string) bool {
	if path == "" || path == "." || path == ".." || filepath.IsAbs(path) || filepath.Clean(path) != path || strings.HasPrefix(path, "../") || strings.ContainsAny(path, "\x00\n\r") || len(path) > 4096 || strings.Count(path, "/") >= 32 {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if len(part) > 255 {
			return false
		}
	}
	return true
}
