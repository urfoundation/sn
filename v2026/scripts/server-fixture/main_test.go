// Real key decoding/signing and physical input boundaries cover the missing
// private JWT fixture without importing any host vault or shared services.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A tiny physical source tree carries the adapter's actual input layout.
func fixtureTestInputs(t *testing.T) (string, string) {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// testing.TempDir's leaf mode is not a private-resource contract.
	for _, path := range []string{parent, server} {
		if err := os.Chmod(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"local/postgres/initdb", "local/testdata/config/local"} {
		if err := os.MkdirAll(filepath.Join(server, filepath.FromSlash(path)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"db.yml", "redis.yml"} {
		if err := os.WriteFile(filepath.Join(server, "local", "testdata", "config", "local", name), []byte("fixture: true\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return parent, server
}

// Admission failures cannot leave a partial output or silently provision elsewhere.
func requireEmptyFixtureParent(t *testing.T, parent string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected fixture output: entries%d err%v", len(entries), err)
	}
}

// The generated resource points to a genuine P-256 key usable by the real JWT loader.
func TestServerFixtureProducesPrivateSigningResources(t *testing.T) {
	parent, server := fixtureTestInputs(t)
	report, err := createFixture(parent, server)
	if err != nil {
		t.Fatal(err)
	}
	if report.Format != fixtureFormat || filepath.Dir(report.Workspace) != parent || report.Server != server {
		t.Fatal("fixture report lost explicit ownership")
	}
	for _, path := range []string{report.Workspace, filepath.Join(report.Workspace, "vault"), filepath.Join(report.Workspace, "config"), filepath.Join(report.Workspace, "site")} {
		if err := requirePhysicalDirectory(path, true); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"vault/" + fixtureKeyName, "vault/jwt.yml", "vault/password.yml", "fixture.json"} {
		info, err := os.Lstat(filepath.Join(report.Workspace, filepath.FromSlash(name)))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatalf("fixture resource %s is not private/physical: %v", name, err)
		}
	}
	config, err := os.ReadFile(filepath.Join(report.Workspace, "vault", "jwt.yml"))
	if err != nil || string(config) != fixtureJwtConfig {
		t.Fatalf("actual JWT resource differs: %v", err)
	}
	encoded, err := os.ReadFile(filepath.Join(report.Workspace, "vault", fixtureKeyName))
	if err != nil {
		t.Fatal(err)
	}
	block, rest := pem.Decode(encoded)
	if block == nil || block.Type != "EC PRIVATE KEY" || len(rest) != 0 {
		t.Fatal("invalid or ambiguous private-key encoding")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil || key.Curve.Params().Name != "P-256" {
		t.Fatalf("real signing key parse failed: %v", err)
	}
	digest := sha256.Sum256([]byte("private server authentication fixture"))
	signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil || !ecdsa.VerifyASN1(&key.PublicKey, digest[:], signature) {
		t.Fatalf("real signature verification failed: %v", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicHash := sha256.Sum256(publicDER)
	if report.PublicKeySHA256 != hex.EncodeToString(publicHash[:]) {
		t.Fatal("public fingerprint differs from actual signing key")
	}
	linked, err := os.Readlink(filepath.Join(report.Workspace, "server"))
	if err != nil || linked != server {
		t.Fatalf("service source alias lost exact input: %v", err)
	}
	for _, name := range []string{"config", "site"} {
		requireEmptyFixtureParent(t, filepath.Join(report.Workspace, name))
	}
}

// Independent qualification owners get separate files and signing authority.
func TestServerFixtureSeparatesRepeatedOwners(t *testing.T) {
	parent, server := fixtureTestInputs(t)
	first, err := createFixture(parent, server)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(first.Workspace, "vault", fixtureKeyName))
	if err != nil {
		t.Fatal(err)
	}
	second, err := createFixture(parent, server)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(first.Workspace, "vault", fixtureKeyName))
	if err != nil || !bytes.Equal(before, after) || first.Workspace == second.Workspace || first.PublicKeySHA256 == second.PublicKeySHA256 {
		t.Fatalf("new fixture overwrote or reused an existing owner: %v", err)
	}
}

// No default parent or symlink can turn test preparation into an external write.
func TestServerFixtureRejectsInvalidParentBeforeMutation(t *testing.T) {
	for _, fault := range []string{"missing", "relative", "noncanonical", "alias", "public", "file"} {
		parent, server := fixtureTestInputs(t)
		input := parent
		switch fault {
		case "missing":
			input = filepath.Join(parent, "absent")
		case "relative":
			input = "."
		case "noncanonical":
			input = parent + "/."
		case "alias":
			input = filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(parent, input); err != nil {
				t.Fatal(err)
			}
		case "public":
			if err := os.Chmod(parent, 0755); err != nil {
				t.Fatal(err)
			}
		case "file":
			input = filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(input, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := createFixture(input, server); err == nil {
			t.Fatalf("%s parent was admitted", fault)
		}
		requireEmptyFixtureParent(t, parent)
	}
}

// A source alias or absent resource is rejected before key generation/output.
func TestServerFixtureRejectsInvalidServerBeforeMutation(t *testing.T) {
	for _, fault := range []string{"missing", "relative", "noncanonical", "alias", "file"} {
		parent, server := fixtureTestInputs(t)
		input := server
		switch fault {
		case "missing":
			input = filepath.Join(server, "absent")
		case "relative":
			input = "."
		case "noncanonical":
			input = server + "/."
		case "alias":
			input = filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(server, input); err != nil {
				t.Fatal(err)
			}
		case "file":
			input = filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(input, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := createFixture(parent, input); err == nil {
			t.Fatalf("%s server was admitted", fault)
		}
		requireEmptyFixtureParent(t, parent)
	}
}

// Physical ownership alone does not permit output inside the frozen source tree.
func TestServerFixtureRejectsParentInsideServerBeforeMutation(t *testing.T) {
	for _, nested := range []bool{false, true} {
		_, server := fixtureTestInputs(t)
		parent := server
		if nested {
			parent = filepath.Join(server, "fixture-parent")
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal(err)
			}
		}
		snapshot := func() map[string]string {
			values := map[string]string{}
			err := filepath.Walk(server, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				values[path] = info.Mode().String()
				if info.Mode().IsRegular() {
					data, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					values[path] += string(data)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			return values
		}
		before := snapshot()
		if _, err := createFixture(parent, server); err == nil || !strings.Contains(err.Error(), "descendants") {
			t.Fatalf("nested%v fixture parent crossed frozen source boundary: %v", nested, err)
		}
		if !reflect.DeepEqual(before, snapshot()) {
			t.Fatalf("nested%v refusal changed source files or modes", nested)
		}
	}
}

// Fixed service input paths must remain physical, not credentials reached by aliases.
func TestServerFixtureRejectsIncompleteOrAliasedInputs(t *testing.T) {
	for _, relative := range []string{"local/postgres/initdb", "local/testdata/config/local/db.yml", "local/testdata/config/local/redis.yml"} {
		for _, fault := range []string{"missing", "alias", "wrong-type"} {
			parent, server := fixtureTestInputs(t)
			path := filepath.Join(server, filepath.FromSlash(relative))
			if err := os.Rename(path, path+".retained"); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "alias":
				if err := os.Symlink(path+".retained", path); err != nil {
					t.Fatal(err)
				}
			case "wrong-type":
				if strings.HasSuffix(relative, ".yml") {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.WriteFile(path, []byte("untouched"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := createFixture(parent, server); err == nil {
				t.Fatalf("%s %s was admitted", relative, fault)
			}
			requireEmptyFixtureParent(t, parent)
		}
	}
}

// All command arguments are validated before any output directory is created.
func TestServerFixtureCLIRequiresExplicitInputs(t *testing.T) {
	parent, server := fixtureTestInputs(t)
	for _, args := range [][]string{
		{},
		{"--parent", parent},
		{"--server", server},
		{"--parent", parent, "--server", server, "unexpected"},
		{"--parent", parent, "--server", server, "--unknown"},
	} {
		var output bytes.Buffer
		if err := run(args, &output); err == nil || output.Len() != 0 {
			t.Fatalf("invalid CLI emitted/admitted output: %v", err)
		}
		requireEmptyFixtureParent(t, parent)
	}
	if err := run([]string{"--parent", parent, "--server", server}, nil); err == nil {
		t.Fatal("nil output was admitted")
	}
	requireEmptyFixtureParent(t, parent)
}

// Only the public report crosses stdout; even inherited credential roots are ignored.
func TestServerFixtureCLIReportsPublicProvenanceOnly(t *testing.T) {
	parent, server := fixtureTestInputs(t)
	t.Setenv("WARP_VAULT_HOME", filepath.Join(parent, "must-not-read-vault"))
	t.Setenv("WARP_CONFIG_HOME", filepath.Join(parent, "must-not-read-config"))
	var output bytes.Buffer
	if err := run([]string{"--parent", parent, "--server", server}, &output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	decoder.DisallowUnknownFields()
	var report fixtureReport
	if err := decoder.Decode(&report); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("report has trailing output: %v", err)
	}
	stored, err := os.ReadFile(filepath.Join(report.Workspace, "fixture.json"))
	if err != nil || bytes.Contains(stored, []byte("PRIVATE KEY")) {
		t.Fatalf("public report included private signing material: %v", err)
	}
	var retained fixtureReport
	if err := json.Unmarshal(stored, &retained); err != nil || report != retained {
		t.Fatalf("persisted provenance differs: %v", err)
	}
}

// A failed output consumer does not erase already-created diagnostic evidence.
type fixtureFailWriter struct{ err error }

// This synchronous error deterministically exercises the publication failure.
func (self fixtureFailWriter) Write([]byte) (int, error) { return 0, self.err }

// Failed report publication retains the exact owned directory and no secret text.
func TestServerFixtureOutputFailureRetainsPrivateResources(t *testing.T) {
	parent, server := fixtureTestInputs(t)
	want := errors.New("fixture report rejected")
	err := run([]string{"--parent", parent, "--server", server}, fixtureFailWriter{err: want})
	if !errors.Is(err, want) || strings.Contains(err.Error(), "PRIVATE KEY") {
		t.Fatalf("report failure lost cause or exposed key: %v", err)
	}
	entries, readErr := os.ReadDir(parent)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("failed publication lost the owned fixture: %v", readErr)
	}
	path := filepath.Join(parent, entries[0].Name())
	if !strings.Contains(err.Error(), path) {
		t.Fatal("failed publication omitted recovery path")
	}
	if _, err := os.Stat(filepath.Join(path, "vault", fixtureKeyName)); err != nil {
		t.Fatal(err)
	}
}

// Existing files and symlinks are never overwritten by private resource writes.
func TestServerFixtureExclusiveWritesPreserveExistingTargets(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "retained")
	before := []byte("retained fixture")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{path, alias} {
		if err := writePrivateFile(target, []byte("replacement")); !errors.Is(err, os.ErrExist) {
			t.Fatalf("existing target write did not refuse: %v", err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("retained target changed: %v", err)
	}
}

// Real account creation uses password.pepper before it can exercise JWT upload.
func TestServerFixtureProvidesPrivateAccountPasswordPepper(t *testing.T) {
	parent, server := fixtureTestInputs(t)
	first, err := createFixture(parent, server)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(first.Workspace, "vault", "password.yml"))
	if err != nil {
		t.Fatalf("private account password pepper is absent: %v", err)
	}
	prefix, suffix := "password:\n  pepper: \"", "\"\n"
	if !bytes.HasPrefix(data, []byte(prefix)) || !bytes.HasSuffix(data, []byte(suffix)) {
		t.Fatal("private account password resource has the wrong exact schema")
	}
	encoded := string(data[len(prefix) : len(data)-len(suffix)])
	pepper, err := hex.DecodeString(encoded)
	if err != nil || len(pepper) != 32 {
		t.Fatal("private account password pepper is not a fresh 32-byte encoding")
	}
	info, err := os.Lstat(filepath.Join(first.Workspace, "vault", "password.yml"))
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatalf("private account password resource is not private: %v", err)
	}
	report, err := os.ReadFile(filepath.Join(first.Workspace, "fixture.json"))
	if err != nil || bytes.Contains(report, []byte(encoded)) {
		t.Fatalf("private password material reached the public report: %v", err)
	}
	second, err := createFixture(parent, server)
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.ReadFile(filepath.Join(second.Workspace, "vault", "password.yml"))
	if err != nil || bytes.Equal(data, other) {
		t.Fatalf("independent fixtures reused account password authority: %v", err)
	}
}

// The test helper owns explicit permissions instead of inheriting host umask.
func TestServerFixtureTestInputsHaveExplicitPrivateModes(t *testing.T) {
	parent, server := fixtureTestInputs(t)
	for _, path := range []string{parent, server} {
		if err := requirePhysicalDirectory(path, true); err != nil {
			t.Fatalf("test resource directory has no explicit private ownership: %v", err)
		}
	}
}
