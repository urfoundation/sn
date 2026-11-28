//go:build darwin

package validator

import "fmt"

// Only Linux mount facts are found automatically. On macOS give the APFS
// volume's mount point, UUID (diskutil info) and type.
func detectRuntimeFilesystem(path string) (string, string, string, error) {
	return "", "", "", fmt.Errorf("pass --mount-path, --filesystem-uuid and --filesystem-type for the volume holding %s (diskutil info shows them)", path)
}
