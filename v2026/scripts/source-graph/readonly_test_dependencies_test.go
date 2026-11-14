// Loading all test imports catches module requirements that drift when a local
// sibling adds or upgrades an imported dependency; no test binary is executed.
package sourcegraph

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Current sources must load their complete test dependency graph offline with
// the checked-in requirements, including versions selected by sibling modules.
func TestSourceGraphLoadsCompleteTestDependenciesReadonly(t *testing.T) {
	directory, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "list", "-mod=readonly", "-p=1", "-deps", "-test", "-json", "./...")
	command.Dir = directory
	// Keep an explicitly selected private modfile or overlay in GOFLAGS so the
	// child examines the same source inputs as its parent qualification command.
	command.Env = append(os.Environ(),
		"GOWORK=off", "GOENV=off", "GOTOOLCHAIN=local", "GOPROXY=off",
		"GONOPROXY=none", "GOPRIVATE=", "GOAUTH=off",
		"GOVCS=*:off", "GOTELEMETRY=off",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("load complete test dependency graph without changing source or downloading modules: %v\n%s", err, output)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	packages := 0
	for {
		var pkg struct {
			ImportPath string
			Error      *struct{ Err string }
			DepsErrors []struct{ Err string }
		}
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("decode complete test dependency graph: %v", err)
		}
		if pkg.ImportPath == "" || pkg.Error != nil || len(pkg.DepsErrors) != 0 {
			t.Fatalf("incomplete test dependency package: %+v", pkg)
		}
		packages++
	}
	if packages == 0 {
		t.Fatal("complete test dependency graph is empty")
	}
}
