// Only fresh synthetic fixtures enroll member authority, before any claimant
// opens. Reopen tests retain the original declaration and snapshot attributes.
package main

import (
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
)

// The fixed owner kind and capacity match the production namespace; this test
// helper never runs in a runtime constructor or adopts missing retained files.
func prepareBootstrapSuccessorMembersTest(t *testing.T, path string, registry bool) {
	t.Helper()
	spec := bootstrapSuccessorMemberSpec(registry)
	durablefixture.ProvisionSnapshot(t, path, spec.Kind, spec.Name, spec.MaximumBytes, "", nil)
}
