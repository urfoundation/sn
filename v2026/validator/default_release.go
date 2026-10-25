package validator

import (
	_ "embed"
	"path/filepath"
)

// bundledReleaseConfig is kept beside the loader so operators can inspect the
// exact defaults shipped in the binary. Explicit files never merge with it.
//
//go:embed release.yml
var bundledReleaseConfig string

func readBundledReleaseConfig() (string, []byte, error) {
	state, err := defaultStateDir()
	if err != nil {
		return "", nil, err
	}
	// This is a normalization anchor, not a file that is created or read.
	// Relative state/credential paths must not depend on the caller's cwd.
	return filepath.Join(state, "release.yml"), []byte(bundledReleaseConfig), nil
}
