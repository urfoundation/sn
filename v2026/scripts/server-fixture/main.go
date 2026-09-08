// Creates private authentication resources for isolated server qualification.
// It never reads host credentials, changes server inputs, or starts services.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const fixtureFormat = "urnetwork-server-private-auth-fixture-v1"
const fixtureKeyName = "fixture-jwt.key"
const fixtureJwtConfig = "tls_key_paths:\n  - fixture-jwt.key\n"

// Contains public provenance only, never the generated private key or a token.
type fixtureReport struct {
	Format          string `json:"format"`
	Workspace       string `json:"workspace"`
	Server          string `json:"server"`
	PublicKeySHA256 string `json:"public_key_sha256"`
}

// Rejects aliases before creating any private output or linking an input.
func requirePhysicalDirectory(path string, private bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("directory must be an absolute canonical path")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("directory must be physical")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if resolved != path {
		return errors.New("directory must not contain a path alias")
	}
	if private && info.Mode().Perm() != 0700 {
		return errors.New("private parent must have mode 0700")
	}
	return nil
}

// Every resource consumed by the existing service adapter has a fixed input.
func requireServerInputs(server string) error {
	if err := requirePhysicalDirectory(server, false); err != nil {
		return fmt.Errorf("server source: %w", err)
	}
	for _, input := range []struct {
		path      string
		directory bool
	}{
		{path: "local/postgres/initdb", directory: true},
		{path: "local/testdata/config/local/db.yml"},
		{path: "local/testdata/config/local/redis.yml"},
	} {
		path := filepath.Join(server, filepath.FromSlash(input.path))
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("server input %s: %w", input.path, err)
		}
		if (input.directory && !info.IsDir()) || (!input.directory && !info.Mode().IsRegular()) {
			return fmt.Errorf("server input %s has the wrong physical type", input.path)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != path {
			return fmt.Errorf("server input %s contains a path alias", input.path)
		}
	}
	return nil
}

// Exclusive writes cannot replace a prior fixture or follow an existing alias.
func writePrivateFile(path string, data []byte) (returnErr error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	written, err := file.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	return file.Sync()
}

// All output is owned by one newly created 0700 directory. On a later error,
// retain that private directory for diagnosis; never recursively remove input.
func createFixture(parent string, server string) (report fixtureReport, returnErr error) {
	if err := requirePhysicalDirectory(parent, true); err != nil {
		return report, fmt.Errorf("fixture parent: %w", err)
	}
	if err := requireServerInputs(server); err != nil {
		return report, err
	}
	relative, err := filepath.Rel(server, parent)
	if err != nil {
		return report, fmt.Errorf("fixture/input separation: %w", err)
	}
	if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return report, errors.New("fixture parent must not be the server source or one of its descendants")
	}
	workspace, err := os.MkdirTemp(parent, "server-fixture-")
	if err != nil {
		return report, err
	}
	defer func() {
		if returnErr != nil {
			returnErr = fmt.Errorf("private fixture retained at %s: %w", workspace, returnErr)
		}
	}()
	for _, name := range []string{"vault", "config", "site"} {
		if err := os.Mkdir(filepath.Join(workspace, name), 0700); err != nil {
			return report, err
		}
	}
	if err := os.Symlink(server, filepath.Join(workspace, "server")); err != nil {
		return report, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return report, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return report, err
	}
	if err := writePrivateFile(filepath.Join(workspace, "vault", fixtureKeyName), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})); err != nil {
		return report, err
	}
	if err := writePrivateFile(filepath.Join(workspace, "vault", "jwt.yml"), []byte(fixtureJwtConfig)); err != nil {
		return report, err
	}
	pepper := make([]byte, 32)
	if _, err := rand.Read(pepper); err != nil {
		return report, err
	}
	passwordConfig := []byte(fmt.Sprintf("password:\n  pepper: \"%x\"\n", pepper))
	if err := writePrivateFile(filepath.Join(workspace, "vault", "password.yml"), passwordConfig); err != nil {
		return report, err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return report, err
	}
	digest := sha256.Sum256(publicDER)
	report = fixtureReport{Format: fixtureFormat, Workspace: workspace, Server: server, PublicKeySHA256: hex.EncodeToString(digest[:])}
	encoded, err := json.Marshal(report)
	if err != nil {
		return report, err
	}
	if err := writePrivateFile(filepath.Join(workspace, "fixture.json"), append(encoded, '\n')); err != nil {
		return report, err
	}
	return report, nil
}

// The command has no ambient credential, configuration or output-directory defaults.
func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("server-fixture", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	parent := flags.String("parent", "", "existing physical 0700 parent for a fresh workspace")
	server := flags.String("server", "", "physical frozen server source root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *parent == "" || *server == "" || stdout == nil {
		return errors.New("explicit --parent and --server, with no positional arguments, are required")
	}
	report, err := createFixture(*parent, *server)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		return fmt.Errorf("private fixture retained at %s; report output: %w", report.Workspace, err)
	}
	return nil
}

// Only public fixture provenance is printed; private signing material stays on disk.
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
