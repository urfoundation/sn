// A normal readonly module census must succeed without rewriting source inputs.
package sourcegraph

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A sibling graph can raise transitive requirements even when each individual
// source replacement is correct. Exercise the real complete Go graph instead
// of asserting a copied list of dependency version strings.
func TestSourceGraphModuleCensusIsReadonly(t *testing.T) {
	directory, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, name := range []string{"go.mod", "go.sum"} {
		before[name], err = os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
	}
	command := exec.CommandContext(t.Context(), "go", "list", "-mod=readonly", "-m", "all")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly", "GOPROXY=off", "GOSUMDB=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("complete module graph needs source changes despite readonly release selection: %v\n%s", err, output)
	}
	if len(bytes.TrimSpace(output)) == 0 {
		t.Fatal("readonly module census returned no identities")
	}
	for name, expected := range before {
		after, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(expected, after) {
			t.Fatalf("readonly module census changed %s", name)
		}
	}
}
