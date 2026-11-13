// Resolver access follows the actual service credentials at each candidate,
// while filesystem reads remain owned by the privileged recovery observer.
package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/server/v2026"
)

// This selected-source fixture declares a different service principal without
// changing the test process UID or requiring privileged filesystem operations.
// It tests source admission only; the public controller fixture separately
// joins writable-root credentials, journals and the real process lifecycle.
func TestRepairOperatorLookupUsesServiceCredentialsAtActualPrecedence(t *testing.T) {
	for _, kind := range []string{"missing-higher", "higher-literal-search", "higher-version-list", "lower-after-literal", "lower-version-after-success"} {
		f := newRepairOperatorFixture(t)
		p := &f.envelope.original.Plan
		p.Uid, p.Gid = p.Uid+1000, p.Gid+1000
		for _, path := range []string{f.base.directory, filepath.Join(f.base.directory, "warp")} {
			if err := os.Chmod(path, 0755); err != nil {
				t.Fatal(err)
			}
		}
		for _, tree := range p.Resources {
			if err := filepath.WalkDir(tree.Path, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				mode := os.FileMode(0644)
				if entry.IsDir() {
					mode = 0755
				}
				return os.Chmod(path, mode)
			}); err != nil {
				t.Fatal(err)
			}
		}
		root := p.env("WARP_VAULT_HOME")
		original := filepath.Join(root, "main", "1.0.0", "st.yml")
		expected, err := os.ReadFile(original)
		if err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "higher-literal-search", "lower-after-literal":
			if err := os.Mkdir(filepath.Join(root, "all"), 0700); err != nil {
				t.Fatal(err)
			}
			if kind == "lower-after-literal" {
				repairValidatorTestWrite(t, filepath.Join(root, "st.yml"), expected, 0644)
			}
		case "higher-version-list", "lower-version-after-success":
			path := filepath.Join(root, "main", "2.0.0")
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
			if kind == "higher-version-list" {
				// Search succeeds, but this principal cannot list the empty
				// higher version to prove absence before the older source.
				if err := os.Chmod(path, 0711); err != nil {
					t.Fatal(err)
				}
			} else {
				repairValidatorTestWrite(t, filepath.Join(path, "st.yml"), expected, 0644)
				if err := os.Chmod(filepath.Dir(original), 0700); err != nil {
					t.Fatal(err)
				}
			}
		}
		for index := range p.Resources {
			hash, err := inspectRepairOperatorTree(t.Context(), f.base.host, p.Resources[index], *p)
			if err != nil {
				t.Fatal(kind, err)
			}
			p.Resources[index].Sha256 = hash
		}
		f.signHost()
		f.sign()
		raw, err := readRepairOperatorResource(f.ctx(), f.base.host, *p, root, "st.yml")
		if kind == "higher-literal-search" || kind == "higher-version-list" {
			if raw != nil || !errors.Is(err, errRpcIntegrity) || !errors.Is(err, server.ErrResourceUnavailable) {
				t.Fatal("privileged resolver bypassed an earlier service-credential refusal", kind, err)
			}
			continue
		}
		if err != nil || !bytes.Equal(raw, expected) {
			t.Fatal("resource lookup changed actual precedence or treated a later refusal as authority", kind, err)
		}
		if _, err := inspectRepairOperatorSt(raw, *p); err != nil {
			t.Fatal("selected source did not retain its original parsed signing and monetary policy", kind, err)
		}
	}
}
