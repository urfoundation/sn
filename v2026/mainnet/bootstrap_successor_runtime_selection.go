// Historical selection is independent of the current node runtime. Version
// identities select signed candidates; CRv4 must still verify both full artifacts.
package main

import (
	"errors"

	"github.com/urfoundation/sn/v2026/crv4"
)

// At most two candidates enter a read regardless of retained history length.
// A duplicate version is an explicit unsupported-profile gate, never authority
// to choose whichever code or metadata happens to be returned by the node.
func bootstrapSuccessorRuntimeArtifactPair(profiles []rootReceiptProfile, inclusion, parent crv4.RuntimeVersionIdentity) ([]crv4.RuntimeArtifactIdentity, error) {
	versions := []crv4.RuntimeVersionIdentity{inclusion}
	if parent != inclusion {
		versions = append(versions, parent)
	}
	artifacts := make([]crv4.RuntimeArtifactIdentity, 0, len(versions))
	for _, version := range versions {
		matches := 0
		for _, profile := range profiles {
			if profile.RuntimeVersion == version {
				matches++
				artifacts = append(artifacts, crv4.RuntimeArtifactIdentity{Version: profile.RuntimeVersion, CodeHash: profile.RuntimeCodeHash, MetadataHash: profile.RuntimeMetadataHash})
			}
		}
		if matches != 1 {
			return nil, errors.New("successor historical runtime lacks one independently approved artifact")
		}
	}
	return artifacts, nil
}
