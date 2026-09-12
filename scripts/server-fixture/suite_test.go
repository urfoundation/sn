// The complete suite uses real generated resources and the checked-in manifest;
// mutations use only synthetic source trees and never an ambient vault.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	serverpkg "github.com/urnetwork/server"
	servermodel "github.com/urnetwork/server/model"
	"gopkg.in/yaml.v3"
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

// Portable connection fixtures resolve reserved addresses without requiring
// the production-sized location database or masking real-database test inputs.
func TestServerFixtureSuiteUsesDocumentationIpOverrideWithoutMmdb(t *testing.T) {
	parent, server := suiteFixtureTestInputs(t)
	report, err := createSuiteFixture(parent, server, "127.0.0.1:35431", "127.0.0.1:36371")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(filepath.Join(report.Workspace, "config", "settings.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		All struct {
			IpOverrides []struct {
				Subnet      string `yaml:"subnet"`
				CountryCode string `yaml:"country_code"`
				Country     string `yaml:"country"`
				Region      string `yaml:"region"`
				City        string `yaml:"city"`
			} `yaml:"ip_overrides"`
		} `yaml:"all"`
	}
	if err := yaml.Unmarshal(encoded, &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings.All.IpOverrides) != 4 {
		t.Fatalf("ip override count = %d, want 4", len(settings.All.IpOverrides))
	}
	for index, expected := range []struct {
		Prefix  string
		Inside  string
		Outside string
	}{
		{Prefix: "192.0.2.0/24", Inside: "192.0.2.1", Outside: "198.51.100.1"},
		{Prefix: "2001:db8::/32", Inside: "2001:db8::1", Outside: "::1"},
		{Prefix: "127.0.0.0/8", Inside: "127.0.0.1", Outside: "128.0.0.1"},
		{Prefix: "::1/128", Inside: "::1", Outside: "::2"},
	} {
		override := settings.All.IpOverrides[index]
		prefix, err := netip.ParsePrefix(override.Subnet)
		if err != nil {
			t.Fatal(err)
		}
		if prefix.String() != expected.Prefix || !prefix.Contains(netip.MustParseAddr(expected.Inside)) || prefix.Contains(netip.MustParseAddr(expected.Outside)) {
			t.Fatalf("fixture override %d does not isolate the intended test subnet", index)
		}
		if override.CountryCode != "zz" || override.Country != "Fixture Country" || override.Region != "Fixture Region" || override.City != "Fixture City" {
			t.Fatalf("fixture override %d lost its synthetic location fields", index)
		}
	}
	if _, err := os.Lstat(filepath.Join(report.Workspace, "config", "mmdb", "ip-ipinfo.mmdb")); !os.IsNotExist(err) {
		t.Fatalf("portable fixture unexpectedly materialized the location database: %v", err)
	}
}

// Real proxy providers connect over loopback. Exercise the same location and
// ownership lookups in a fresh process so their once caches cannot borrow a
// database or settings from a different fixture. No database service is needed.
func TestServerFixtureSuiteLoopbackResolvesWithoutDatabases(t *testing.T) {
	const childVariable = "RELEASE_GATE_FIXTURE_LOOPBACK_TEST_CHILD"
	if os.Getenv(childVariable) == "1" {
		for _, raw := range []string{"127.0.0.1", "127.255.255.254", "::1", "192.0.2.1", "2001:db8::1"} {
			address := netip.MustParseAddr(raw)
			location, err := serverpkg.GetIpInfo(address)
			if err != nil || location == nil || location.CountryCode != "zz" || location.Country != "Fixture Country" || location.Region != "Fixture Region" || location.City != "Fixture City" {
				t.Fatalf("portable transport address %s location: %v %+v", raw, err, location)
			}
			owner, err := serverpkg.GetArinInfo(address)
			if err != nil || owner == nil || !slices.Equal(owner.OrgCountryCodes, []string{"zz"}) {
				t.Fatalf("portable transport address %s ownership: %v %+v", raw, err, owner)
			}
		}
		return
	}
	parent, source := suiteFixtureTestInputs(t)
	report, err := createSuiteFixture(parent, source, "127.0.0.1:35431", "127.0.0.1:36371")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mmdb/ip-ipinfo.mmdb", "arindb/arin.mmdb"} {
		if _, err := os.Lstat(filepath.Join(report.Workspace, "config", name)); !os.IsNotExist(err) {
			t.Fatalf("portable fixture unexpectedly has database %s: %v", name, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestServerFixtureSuiteLoopbackResolvesWithoutDatabases$", "-test.count=1", "-test.v")
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if !strings.HasPrefix(name, "WARP_") && !strings.HasPrefix(name, "BRINGYOUR_") && name != childVariable {
			command.Env = append(command.Env, value)
		}
	}
	command.Env = append(command.Env, childVariable+"=1", "WARP_HOME="+report.Workspace,
		"WARP_CONFIG_HOME="+filepath.Join(report.Workspace, "config"), "WARP_VAULT_HOME="+filepath.Join(report.Workspace, "vault"),
		"WARP_SITE_HOME="+filepath.Join(report.Workspace, "site"), "WARP_ENV=local", "WARP_SERVICE=test")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("portable loopback location consumer: %v\n%s", err, output)
	}
}

// The required physical pro.yml must be valid under the owning server parser,
// while its synthetic values remain inert rather than copying a real product
// plan into the portable fixture.
func TestServerFixtureSuiteProConfigIsSyntheticAndParseable(t *testing.T) {
	parent, server := suiteFixtureTestInputs(t)
	report, err := createSuiteFixture(parent, server, "127.0.0.1:35431", "127.0.0.1:36371")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WARP_CONFIG_HOME", filepath.Join(report.Workspace, "config"))

	config := servermodel.Pro()
	if config.EnforceConcurrentClients || config.EnforceFeatures {
		t.Fatal("portable Pro fixture unexpectedly enables enforcement")
	}
	if config.MaxConcurrentClients(false) != 0 || config.MaxConcurrentClients(true) != 0 {
		t.Fatal("portable Pro fixture unexpectedly configures client limits")
	}
	if config.DataAmount(false) != 0 || config.DataAmount(true) != 0 || config.ReferralBonus != 0 || config.ReferredBonus != 0 {
		t.Fatal("portable Pro fixture unexpectedly configures a data grant")
	}
	if config.PriceMonthlyUsd() != 0 || config.PriceYearlyUsd() != 0 || config.DataCodeDuration != 0 || len(config.DataCodeSkus) != 0 {
		t.Fatal("portable Pro fixture unexpectedly configures a product for sale")
	}
	if config.MaxReferrals != 0 || config.ReferralsCapped(0) || config.SeekerDataMultiplier() != 1 || config.ReferralGrantPeriod() <= 0 {
		t.Fatal("portable Pro fixture lost the inert referral/task contract")
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
