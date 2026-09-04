// generate_script_test.go verifies hermetic generator tool discovery and pinning.
package stabi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// generatorTestEnvironment supplies only explicit tool links, so generator
// discovery cannot accidentally consume an abigen installed on the test host.
type generatorTestEnvironment struct {
	sourceDir string
	pathDir   string
	goBin     string
	goPath    string
}

// newGeneratorTestEnvironment constructs a hermetic PATH plus a controllable
// fake implementation of the two go env queries used by generate.sh.
func newGeneratorTestEnvironment(t *testing.T) *generatorTestEnvironment {
	t.Helper()
	sourceDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	pathDir := t.TempDir()
	for _, name := range []string{"bash", "cmp", "dirname", "jq", "mktemp", "rm", "stat"} {
		target, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("locate test prerequisite %s: %v", name, err)
		}
		if err := os.Symlink(target, filepath.Join(pathDir, name)); err != nil {
			t.Fatalf("link test prerequisite %s: %v", name, err)
		}
	}
	writeGeneratorTestExecutable(t, filepath.Join(pathDir, "go"), `#!/bin/sh
case "$1:$2" in
env:GOBIN) printf '%s\n' "${FAKE_GO_BIN-}" ;;
env:GOPATH) printf '%s\n' "${FAKE_GO_PATH-}" ;;
*) printf 'unexpected fake go invocation: %s\n' "$*" >&2; exit 64 ;;
esac
`)
	return &generatorTestEnvironment{
		sourceDir: sourceDir,
		pathDir:   pathDir,
		goPath:    t.TempDir(),
	}
}

// writeGeneratorTestExecutable creates one deterministic fake tool.
func writeGeneratorTestExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// installGeneratorTestAbigen creates an abigen double that reports the chosen
// version and reproduces checked-in bindings without running a host abigen.
func installGeneratorTestAbigen(t *testing.T, path, version string) {
	t.Helper()
	body := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then
    printf 'abigen version %s\n'
    exit 0
fi
type_name=''
output=''
while [ "$#" -gt 0 ]; do
    case "$1" in
    --type) shift; type_name="$1" ;;
    --out) shift; output="$1" ;;
    esac
    shift
done
case "$type_name" in
STSubnet) source_name=stsubnet.go ;;
STCoordinator) source_name=stcoordinator.go ;;
STSettlementVault) source_name=stsettlementvault.go ;;
STReserveSink) source_name=streservesink.go ;;
*) printf 'unexpected binding type: %%s\n' "$type_name" >&2; exit 65 ;;
esac
/bin/cp "$STABI_SOURCE_DIR/$source_name" "$output"
`, version)
	writeGeneratorTestExecutable(t, path, body)
}

// run invokes the real generator with no inherited tool-discovery variables.
func (self *generatorTestEnvironment) run(t *testing.T, abigenOverride string) ([]byte, error) {
	return self.runMode(t, abigenOverride, "--check")
}

// runMode invokes one explicit generator operation in the hermetic environment.
func (self *generatorTestEnvironment) runMode(t *testing.T, abigenOverride, mode string) ([]byte, error) {
	t.Helper()
	command := exec.Command(filepath.Join(self.sourceDir, "generate.sh"), mode)
	command.Dir = self.sourceDir
	environment := make([]string, 0, len(os.Environ())+5)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "ABIGEN=") || strings.HasPrefix(value, "PATH=") ||
			strings.HasPrefix(value, "FAKE_GO_BIN=") || strings.HasPrefix(value, "FAKE_GO_PATH=") ||
			strings.HasPrefix(value, "STABI_SOURCE_DIR=") {
			continue
		}
		environment = append(environment, value)
	}
	environment = append(environment,
		"PATH="+self.pathDir,
		"FAKE_GO_BIN="+self.goBin,
		"FAKE_GO_PATH="+self.goPath,
		"STABI_SOURCE_DIR="+self.sourceDir,
	)
	if abigenOverride != "" {
		environment = append(environment, "ABIGEN="+abigenOverride)
	}
	command.Env = environment
	return command.CombinedOutput()
}

func TestGeneratePreflightStopsAfterVersionValidation(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	installGeneratorTestAbigen(t, filepath.Join(environment.pathDir, "abigen"), "1.17.0-stable")
	if err := os.Remove(filepath.Join(environment.pathDir, "jq")); err != nil {
		t.Fatal(err)
	}
	output, err := environment.runMode(t, "", "--preflight")
	if err != nil {
		t.Fatalf("generate --preflight: %v\n%s", err, output)
	}
	if got, want := string(output), "abigen version 1.17.0-stable\n"; got != want {
		t.Fatalf("preflight output = %q, want %q", got, want)
	}
}

// An explicit override wins even when PATH contains an incompatible tool.
func TestGenerateUsesExplicitAbigenBeforePath(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	installGeneratorTestAbigen(t, filepath.Join(environment.pathDir, "abigen"), "1.16.7-stable")
	explicitAbigen := filepath.Join(t.TempDir(), "abigen")
	installGeneratorTestAbigen(t, explicitAbigen, "1.17.0-stable")
	if output, err := environment.run(t, explicitAbigen); err != nil {
		t.Fatalf("generate --check with ABIGEN: %v\n%s", err, output)
	}
}

// The ordinary executable search remains the first implicit lookup path.
func TestGenerateDiscoversAbigenOnPath(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	installGeneratorTestAbigen(t, filepath.Join(environment.pathDir, "abigen"), "1.17.0-stable")
	if output, err := environment.run(t, ""); err != nil {
		t.Fatalf("generate --check with PATH abigen: %v\n%s", err, output)
	}
}

// A configured Go binary directory works when PATH has no generator.
func TestGenerateDiscoversAbigenInGoBin(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	environment.goBin = t.TempDir()
	installGeneratorTestAbigen(t, filepath.Join(environment.goBin, "abigen"), "1.17.0-stable")
	if output, err := environment.run(t, ""); err != nil {
		t.Fatalf("generate --check with GOBIN abigen: %v\n%s", err, output)
	}
}

// Only the first Go workspace supplies the conventional fallback directory.
func TestGenerateDiscoversAbigenInFirstGoPath(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	secondGoPath := t.TempDir()
	environment.goPath += string(os.PathListSeparator) + secondGoPath
	if err := os.Mkdir(filepath.Join(strings.Split(environment.goPath, string(os.PathListSeparator))[0], "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	firstGoPath := strings.Split(environment.goPath, string(os.PathListSeparator))[0]
	installGeneratorTestAbigen(t, filepath.Join(firstGoPath, "bin", "abigen"), "1.17.0-stable")
	if output, err := environment.run(t, ""); err != nil {
		t.Fatalf("generate --check with GOPATH abigen: %v\n%s", err, output)
	}
}

// A discoverable generator from the wrong go-ethereum release fails closed.
func TestGenerateRejectsWrongAbigenVersion(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	installGeneratorTestAbigen(t, filepath.Join(environment.pathDir, "abigen"), "1.16.7-stable")
	output, err := environment.run(t, "")
	if err == nil {
		t.Fatal("generate --check accepted the wrong abigen version")
	}
	message := string(output)
	if !strings.Contains(message, "got 'abigen version 1.16.7-stable', want 'abigen version 1.17.0-stable'") ||
		!strings.Contains(message, "go install github.com/ethereum/go-ethereum/cmd/abigen@v1.17.0") {
		t.Fatalf("wrong-version diagnostic is incomplete:\n%s", output)
	}
}

// Missing tools produce the exact reproducible installation instruction.
func TestGenerateRejectsMissingAbigen(t *testing.T) {
	environment := newGeneratorTestEnvironment(t)
	output, err := environment.run(t, "")
	if err == nil {
		t.Fatal("generate --check accepted a missing abigen")
	}
	message := string(output)
	if !strings.Contains(message, "was not found in ABIGEN, PATH, GOBIN, or the first GOPATH/bin") ||
		!strings.Contains(message, "go install github.com/ethereum/go-ethereum/cmd/abigen@v1.17.0") {
		t.Fatalf("missing-tool diagnostic is incomplete:\n%s", output)
	}
}
