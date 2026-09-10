// The complete suite uses real generated resources and the checked-in manifest;
// mutations use only synthetic source trees and never an ambient vault.
package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Only the frozen source resource contract is shared with the real adapter.
func suiteFixtureServerSource(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	server, err := filepath.EvalSymlinks(filepath.Join(cwd, "..", "..", "..", "server"))
	if err != nil {
		t.Fatal(err)
	}
	return server
}

// An independently located real manifest combines with a synthetic tls contract
// for destructive input controls. No host or captured certificate is copied.
func suiteFixtureTestInputs(t *testing.T) (string, string) {
	t.Helper()
	parent, server := fixtureTestInputs(t)
	manifest, err := readSuiteFixtureSource(suiteFixtureServerSource(t), "local/suite-resource-manifest.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(server, "local", "suite-resource-manifest.txt"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	contract := "test_env_tls_tree_complete() {\n    for host_name in fixture.example connect.fixture.example; do\n        :\n    done\n}\n"
	if err := os.WriteFile(filepath.Join(server, "test-env.sh"), []byte(contract), 0600); err != nil {
		t.Fatal(err)
	}
	return parent, server
}

// This is the original failure boundary: every upstream required file/tree is
// materialized without linking the unavailable workspace vault or local data.
func TestServerFixtureSuiteSatisfiesActualResourceCensus(t *testing.T) {
	parent, _ := fixtureTestInputs(t)
	server := suiteFixtureServerSource(t)
	report, err := createSuiteFixture(parent, server, "127.0.0.1:35431", "127.0.0.1:36371")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := readSuiteFixtureSource(server, "local/suite-resource-manifest.txt")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(manifest)), "\n")[1:] {
		kind, name, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatal("actual manifest framing differs")
		}
		if kind == "vault_tree" {
			kind = "vault"
		}
		info, err := os.Lstat(filepath.Join(report.Workspace, kind, name))
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("required physical %s: %v", line, err)
		}
		count++
	}
	if count != 30 {
		t.Fatalf("independent suite census changed: %d", count)
	}
	err = filepath.Walk(report.Workspace, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == filepath.Join(report.Workspace, "server") {
			return nil
		}
		want := os.FileMode(0600)
		if info.IsDir() {
			want = 0700
		} else if !info.Mode().IsRegular() {
			t.Fatalf("unexpected fixture alias %s", path)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("nonprivate suite resource %s mode %o", path, info.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"vault/local", "config/local", "vault/nonservice.yml"} {
		if _, err := os.Lstat(filepath.Join(report.Workspace, absent)); !os.IsNotExist(err) {
			t.Fatalf("ambient fixture alias %s survived: %v", absent, err)
		}
	}
	for _, pair := range [][2]string{{"vault/pg.yml", "vault/pg_maintenance.yml"}, {"config/db.yml", "config/db_maintenance.yml"}} {
		first, err := os.ReadFile(filepath.Join(report.Workspace, pair[0]))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(report.Workspace, pair[1]))
		if err != nil || !bytes.Equal(first, second) {
			t.Fatalf("maintenance source differs: %v", err)
		}
	}
}

// Required tls aliases carry a valid generated key pair with a synthetic
// certificate identity, never the live names used by the legacy path contract.
func TestServerFixtureSuiteUsesSyntheticCertificatesForRequiredAliases(t *testing.T) {
	parent, server := suiteFixtureTestInputs(t)
	report, err := createSuiteFixture(parent, server, "127.0.0.1:35431", "127.0.0.1:36371")
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"fixture.example", "connect.fixture.example"} {
		base := filepath.Join(report.Workspace, "vault", "tls", alias, alias)
		pair, err := tls.LoadX509KeyPair(base+".crt", base+".key")
		if err != nil || len(pair.Certificate) != 1 {
			t.Fatalf("required pair is not genuine: %v", err)
		}
		certificate, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			t.Fatal(err)
		}
		if certificate.Subject.CommonName != "fixture.example" || !slices.Equal(certificate.DNSNames, []string{"fixture.example"}) || len(certificate.IPAddresses) != 0 {
			t.Fatal("synthetic certificate acquired a source alias identity")
		}
		if err := certificate.CheckSignatureFrom(certificate); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.ReadFile(filepath.Join(report.Workspace, "config", "apple_roots.pem"))
	if err != nil {
		t.Fatal(err)
	}
	block, rest := pem.Decode(root)
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		t.Fatal("synthetic local root is malformed")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		t.Fatal(err)
	}
}

// A missing implementation, duplicate or altered format refuses before output;
// silently creating placeholder files could not satisfy this contract.
func TestServerFixtureSuiteRejectsManifestDriftBeforeMutation(t *testing.T) {
	for _, fault := range []string{"missing", "duplicate", "unknown", "format"} {
		parent, server := suiteFixtureTestInputs(t)
		path := filepath.Join(server, "local", "suite-resource-manifest.txt")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "missing":
			data = bytes.Replace(data, []byte("vault=auth.yml\n"), nil, 1)
		case "duplicate":
			data = append(data, []byte("vault=auth.yml\n")...)
		case "unknown":
			data = append(data, []byte("vault=unknown.yml\n")...)
		case "format":
			data = bytes.Replace(data, []byte("resources-v1"), []byte("resources-v2"), 1)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := createSuiteFixture(parent, server, "127.0.0.1:35431", "127.0.0.1:36371"); err == nil {
			t.Fatalf("%s manifest admitted", fault)
		}
		requireEmptyFixtureParent(t, parent)
	}
}

// New source inputs keep the original physical-file and bounded-read contract.
func TestServerFixtureSuiteRejectsAliasedOrExcessiveSourceBeforeMutation(t *testing.T) {
	for _, name := range []string{"local/suite-resource-manifest.txt", "test-env.sh"} {
		for _, fault := range []string{"missing", "alias", "excessive"} {
			parent, server := suiteFixtureTestInputs(t)
			path := filepath.Join(server, filepath.FromSlash(name))
			if err := os.Rename(path, path+".retained"); err != nil {
				t.Fatal(err)
			}
			if fault == "alias" {
				if err := os.Symlink(path+".retained", path); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "excessive" {
				if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 1024*1024+1), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := createSuiteFixture(parent, server, "127.0.0.1:35431", "127.0.0.1:36371"); err == nil {
				t.Fatalf("%s %s source admitted", name, fault)
			}
			requireEmptyFixtureParent(t, parent)
		}
	}
}

// No supplied authority can escape the exact two private daemon endpoints.
func TestServerFixtureSuiteRequiresDistinctCanonicalLoopbackAuthorities(t *testing.T) {
	for _, pair := range [][2]string{{"", "127.0.0.1:36371"}, {"localhost:35431", "127.0.0.1:36371"}, {"192.0.2.1:35431", "127.0.0.1:36371"}, {"127.0.0.1:035431", "127.0.0.1:36371"}, {"127.0.0.1:0", "127.0.0.1:36371"}, {"127.0.0.1:65536", "127.0.0.1:36371"}, {"127.0.0.1:36371", "127.0.0.1:36371"}} {
		parent, server := suiteFixtureTestInputs(t)
		if _, err := createSuiteFixture(parent, server, pair[0], pair[1]); err == nil {
			t.Fatal("foreign or ambiguous service authority admitted")
		}
		requireEmptyFixtureParent(t, parent)
	}
}

// The alias parser admits only one finite literal contract, never executable
// shell expansion or a path traversal from the source resource inventory.
func TestServerFixtureSuiteRejectsChangedTlsAliasContract(t *testing.T) {
	for _, hosts := range []string{"", "fixture.example fixture.example", "../fixture.example", "$(command)", strings.Repeat("a", 254) + ".example", strings.Repeat("fixture.example ", 17)} {
		contract := []byte("test_env_tls_tree_complete() {\n    for host_name in " + hosts + "; do\n        :\n    done\n}\n")
		if _, err := suiteFixtureTlsAliases(contract); err == nil {
			t.Fatalf("invalid tls alias contract admitted: %q", hosts)
		}
	}
}

// Suite-only CLI fields cannot silently widen the authentication-only mode.
func TestServerFixtureSuiteCliRequiresExplicitModeAndCompleteAuthorities(t *testing.T) {
	parent, server := suiteFixtureTestInputs(t)
	base := []string{"--parent", parent, "--server", server}
	for _, extra := range [][]string{{"--path-only"}, {"--postgres-authority", "127.0.0.1:35431"}, {"--suite"}, {"--suite", "--postgres-authority", "127.0.0.1:35431"}} {
		var output bytes.Buffer
		if err := run(append(append([]string(nil), base...), extra...), &output); err == nil || output.Len() != 0 {
			t.Fatalf("incomplete suite emitted output: %v", err)
		}
		requireEmptyFixtureParent(t, parent)
	}
	var output bytes.Buffer
	if err := run(append(base, "--suite", "--postgres-authority", "127.0.0.1:35431", "--redis-authority", "127.0.0.1:36371", "--path-only"), &output); err != nil {
		t.Fatal(err)
	}
	root := strings.TrimSuffix(output.String(), "\n")
	if filepath.Dir(root) != parent || strings.Contains(root, "\n") {
		t.Fatal("path-only output lost its private owner")
	}
	if _, err := os.Stat(filepath.Join(root, "vault", "auth.yml")); err != nil {
		t.Fatal(err)
	}
}
