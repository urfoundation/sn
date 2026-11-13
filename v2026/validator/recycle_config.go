// Read-only admission may inspect a separately approved mainnet artifact.
// It does not change the production loader's reviewed runtime requirement.
package validator

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maximumOwnerRecycleConfigBytes = 2 * 1024 * 1024

// Loads bounded configuration for approval retention and observation only.
// Full producer validation and every mainnet steering entry remain closed;
// the separately pinned envelope is authenticated by the custody/read owners.
// An empty envelope reference is allowed only to prepare the resolved config
// hash for external approval. Retention and observation reject that draft.
func LoadOwnerRecycleAdmissionConfig(path string) (*ReleaseConfig, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("owner-recycle admission configuration path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maximumOwnerRecycleConfigBytes {
		return nil, errors.Join(errors.New("owner-recycle admission configuration is not a bounded regular file"), err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumOwnerRecycleConfigBytes+1))
	if err != nil || len(raw) > maximumOwnerRecycleConfigBytes {
		return nil, errors.Join(errors.New("owner-recycle admission configuration exceeds its byte bound"), err)
	}
	return decodeReleaseConfigBytesMode(abs, raw, releaseConfigLoadMode{ownerRecycleAdmission: true})
}
