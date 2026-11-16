// Public execution reconstructs and retains the independent local and global
// member heads before acquiring any new nonce or cumulative allowance.
package main

import (
	"bytes"
	"maps"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Removing only either preprovisioned head leaves all signed inputs and physical
// roots intact. The actual public claim must refuse without recreating it.
func TestBootstrapSuccessorPublicClaimRequiresBothMemberHeads(t *testing.T) {
	checked := 0
	f := newBootstrapSuccessorCanonicalFixtureWithClaimGate(t, func(f *bootstrapSuccessorCanonicalFixture) {
		root, registry := f.original.config.RunDirectory, f.approval.Plan.Request.RegistryDirectory
		before, nonces := bootstrapSuccessorPreparationTestFiles(t, root), bootstrapSuccessorPreparationTestFiles(t, registry)
		for _, scope := range []struct {
			path     string
			registry bool
		}{{path: root}, {path: registry, registry: true}} {
			spec := bootstrapSuccessorMemberSpec(scope.registry)
			attribute := durablehead.Attribute(spec.Kind, spec.Name)
			raw := make([]byte, 4096)
			n, err := unix.Getxattr(scope.path, attribute, raw)
			if err != nil {
				t.Fatal(err)
			}
			raw = raw[:n]
			if err := unix.Removexattr(scope.path, attribute); err != nil {
				t.Fatal(err)
			}
			var stdout bytes.Buffer
			code, diagnostic := f.invoke("contract-successor-execution-claim", &stdout, f.approvalArgs...)
			if code != 1 || stdout.Len() != 0 || !strings.Contains(diagnostic, durablevolume.ErrIdentity.Error()) ||
				!maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, root)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, registry)) {
				t.Fatal("public claim recreated missing member authority or changed original custody", scope.registry, code, diagnostic)
			}
			if err := unix.Setxattr(scope.path, attribute, raw, unix.XATTR_CREATE); err != nil {
				t.Fatal("refused public command enrolled a replacement head", err)
			}
			checked++
		}
	})
	if checked != 2 || len(f.original.contracts.writes) != 8 {
		t.Fatal("public member-head controls lost scope or caused a send", checked)
	}
}
