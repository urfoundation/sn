package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func discoverGoTestReferences(t *testing.T, root, prefix string, references map[string]bool) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "build", "lib", "node_modules", "out", "runs", "vendor":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		dir, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		component := prefix
		if dir != "." {
			component = strings.Trim(strings.TrimSuffix(prefix, "/")+"/"+filepath.ToSlash(dir), "/")
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && strings.HasPrefix(function.Name.Name, "Test") {
				references[strings.Trim(component+"/"+function.Name.Name, "/")] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func discoverSolidityTestReferences(t *testing.T, root string, references map[string]bool) {
	t.Helper()
	contractPattern := regexp.MustCompile(`(?m)\bcontract\s+([A-Za-z_][A-Za-z0-9_]*)`)
	testPattern := regexp.MustCompile(`(?m)\bfunction\s+(test_[A-Za-z0-9_]*)\s*\(`)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".t.sol") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		contracts, tests := contractPattern.FindAllSubmatch(data, -1), testPattern.FindAllSubmatch(data, -1)
		for _, contract := range contracts {
			for _, test := range tests {
				references[string(contract[1])+"."+string(test[1])] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func discoverShellTestReferences(t *testing.T, root string, references map[string]bool) {
	t.Helper()
	scripts := filepath.Join(root, "scripts")
	err := filepath.WalkDir(scripts, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sh") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0o111 == 0 {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		references[filepath.ToSlash(relative)] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReleaseScenarioMatrixCoversAllTwentyNormativeRows(t *testing.T) {
	cfg := testResolvedConfig(t)
	matrix, err := loadScenarioMatrix(cfg.Repos.SN)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	if definition.MatrixHash == "" || definition.MatrixHash != matrix.Hash || len(matrix.Rows) != 20 {
		t.Fatalf("matrix hash/rows mismatch: definition=%s matrix=%s rows=%d", definition.MatrixHash, matrix.Hash, len(matrix.Rows))
	}
	if err := validateScenarioMatrixCoverage(matrix, definition.Checks, definition.Faults); err != nil {
		t.Fatal(err)
	}
	mutated := *matrix
	mutated.Rows = append([]ScenarioMatrixRow(nil), matrix.Rows...)
	mutated.Rows[0] = matrix.Rows[0]
	mutated.Rows[0].LiveAssertions = []string{"missing-live-proof"}
	if err := validateScenarioMatrixCoverage(&mutated, definition.Checks, definition.Faults); err == nil {
		t.Fatal("matrix row with a missing live assertion was accepted")
	}
}

func TestReleaseScenarioMatrixReferencesOnlyCheckedInTests(t *testing.T) {
	cfg := testResolvedConfig(t)
	matrix, err := loadScenarioMatrix(cfg.Repos.SN)
	if err != nil {
		t.Fatal(err)
	}
	references := map[string]bool{}
	discoverGoTestReferences(t, cfg.Repos.SN, "", references)
	discoverGoTestReferences(t, cfg.Repos.Server, "server", references)
	discoverSolidityTestReferences(t, filepath.Join(cfg.Repos.SN, "evm", "test"), references)
	discoverShellTestReferences(t, cfg.Repos.SN, references)
	for _, row := range matrix.Rows {
		for _, reference := range row.LocalTests {
			if !references[reference] {
				t.Errorf("scenario matrix row %s references missing checked-in test %s", row.ID, reference)
			}
		}
	}
}
