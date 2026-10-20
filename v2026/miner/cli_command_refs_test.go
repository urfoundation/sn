package miner

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Docs and comments that tell a provider to run `provider <cmd>` or
// `snclaim <cmd>` must name a command the CLI has. A removed command left in
// the docs sends users to something that does not exist (support inbox 7934:
// head binding moved from bind-head to `provider fleet bind`).
func TestDocumentedCliCommandsExist(t *testing.T) {
	commands := map[string]map[string]bool{
		"provider": usageCommands(mainUsage(), "provider"),
	}
	onchainUsage, err := os.ReadFile(filepath.Join("onchain", "run.go"))
	if err != nil {
		t.Fatal(err)
	}
	commands["snclaim"] = usageCommands(string(onchainUsage), "snclaim")
	for binary, set := range commands {
		if len(set) == 0 {
			t.Fatalf("no %s commands found in its usage", binary)
		}
	}

	// `provider fleet <sub>` names a fleet subcommand, checked the same way
	// (support inbox 7934: the Top 200 copy names `provider fleet sign`).
	fleetCommands := usageCommands(mainUsage(), "provider fleet")
	if len(fleetCommands) == 0 {
		t.Fatal("no provider fleet commands found in its usage")
	}
	fleetReference := regexp.MustCompile("`provider fleet ([a-z][a-z0-9_-]*)")

	reference := regexp.MustCompile("`(provider|snclaim) ([a-z][a-z0-9_-]*)")
	root := ".."
	var missing []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if entry.IsDir() {
			switch rel {
			case ".git", "third_party", "node_modules", filepath.Join("docs", "parked"):
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range reference.FindAllStringSubmatch(string(b), -1) {
			if !commands[match[1]][match[2]] {
				missing = append(missing, rel+": "+match[1]+" "+match[2])
			}
		}
		for _, match := range fleetReference.FindAllStringSubmatch(string(b), -1) {
			if !fleetCommands[match[1]] {
				missing = append(missing, rel+": provider fleet "+match[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("documented commands missing from the CLI usage:\n  %s", strings.Join(missing, "\n  "))
	}
}

// usageCommands returns the first word after the binary name on each docopt
// usage line.
func usageCommands(usage string, binary string) map[string]bool {
	line := regexp.MustCompile(`(?m)^\s*` + binary + ` ([a-z][a-z0-9_-]*)`)
	out := map[string]bool{}
	for _, match := range line.FindAllStringSubmatch(usage, -1) {
		out[match[1]] = true
	}
	return out
}
