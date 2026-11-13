// Exercise the release recipes with a synthetic compiler so an early failed
// platform cannot be hidden by a later successful build or packaged as a release.
package validator

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A failing first platform must stop the actual multi-platform recipe.
func TestValidatorMakeBuildStopsAtFirstFailure(t *testing.T) {
	root, env := validatorMakeFixture(t)
	env = append(env, "VALIDATOR_FIXTURE_FAIL=linux/amd64")
	cmd := exec.Command("make", "--no-print-directory", "build", "GOOS_ARCH=linux/amd64 linux/arm64", "VALIDATOR_VERSION=synthetic")
	cmd.Dir, cmd.Env = root, env
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("first compiler failure was hidden: %s", output)
	}
	called, err := os.ReadFile(filepath.Join(root, "compiler.calls"))
	if err != nil || string(called) != "linux/amd64\n" {
		t.Fatalf("recipe advanced after failure: %q, %v", called, err)
	}
}

// Real tar packaging must carry the binary, guide and inspectable defaults.
func TestValidatorMakePackageContainsRunnerInputs(t *testing.T) {
	root, env := validatorMakeFixture(t)
	for _, name := range []string{"README.md", "release.yml", "mainnet-profile.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("synthetic "+name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("make", "--no-print-directory", "package", "GOOS_ARCH=linux/amd64 linux/arm64", "VALIDATOR_VERSION=synthetic")
	cmd.Dir, cmd.Env = root, env
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("package: %v\n%s", err, output)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		file, err := os.Open(filepath.Join(root, "build", "releases", "validator-synthetic-linux-"+arch+".tar.gz"))
		if err != nil {
			t.Fatal(err)
		}
		compressed, err := gzip.NewReader(file)
		if err != nil {
			file.Close()
			t.Fatal(err)
		}
		archive := tar.NewReader(compressed)
		contentsKVs := map[string]string{}
		for {
			header, err := archive.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if header.Typeflag != tar.TypeReg {
				continue
			}
			body, err := io.ReadAll(archive)
			if err != nil {
				t.Fatal(err)
			}
			name := strings.TrimPrefix(header.Name, "./")
			contentsKVs[name] = string(body)
			if name == "validator" && header.Mode&0o111 == 0 {
				t.Fatal("packaged binary is not executable")
			}
		}
		compressed.Close()
		file.Close()
		if len(contentsKVs) != 4 || contentsKVs["validator"] != "synthetic linux/"+arch+"\n" ||
			contentsKVs["README.md"] != "synthetic README.md\n" || contentsKVs["release.yml"] != "synthetic release.yml\n" ||
			contentsKVs["mainnet-profile.json"] != "synthetic mainnet-profile.json\n" {
			t.Fatalf("archive lost runner inputs: %v", contentsKVs)
		}
	}
}

// Copy the original recipe into an isolated directory, replacing only go.
func validatorMakeFixture(t *testing.T) (string, []string) {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is unavailable")
	}
	root := t.TempDir()
	recipe, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Makefile"), recipe, 0o600); err != nil {
		t.Fatal(err)
	}
	compiler := `#!/bin/sh
set -eu
printf '%s/%s\n' "$GOOS" "$GOARCH" >> compiler.calls
if test "${VALIDATOR_FIXTURE_FAIL:-}" = "$GOOS/$GOARCH"; then exit 23; fi
while test "$#" -gt 0; do
    if test "$1" = -o; then
        shift
        mkdir -p "$(dirname "$1")"
        printf 'synthetic %s/%s\n' "$GOOS" "$GOARCH" > "$1"
        chmod 755 "$1"
        exit 0
    fi
    shift
done
exit 24
`
	if err := os.WriteFile(filepath.Join(root, "go"), []byte(compiler), 0o700); err != nil {
		t.Fatal(err)
	}
	env := []string{}
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "PATH=") && !strings.HasPrefix(variable, "VALIDATOR_FIXTURE_FAIL=") {
			env = append(env, variable)
		}
	}
	return root, append(env, "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"))
}
