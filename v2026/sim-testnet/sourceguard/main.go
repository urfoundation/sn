package main

// This command parses simulator sources before release compilation so a broad
// rewrite receives one attributable failure instead of cascading diagnostics.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
)

// Rejects parser failures and identifiers that reveal a whole-file gofmt
// rewrite instead of an intentional receiver rename.
func validateGoSourceFile(path string) error {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if file.Name.Name != "main" {
		return fmt.Errorf("%s declares package %q, want %q", path, file.Name.Name, "main")
	}
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if declaration.Name.Name == "self" {
				return fmt.Errorf("%s declares rewritten top-level function %q", path, declaration.Name.Name)
			}
		case *ast.GenDecl:
			for _, specification := range declaration.Specs {
				switch specification := specification.(type) {
				case *ast.TypeSpec:
					if specification.Name.Name == "self" {
						return fmt.Errorf("%s declares rewritten top-level type %q", path, specification.Name.Name)
					}
				case *ast.ValueSpec:
					for _, name := range specification.Names {
						if name.Name == "self" {
							return fmt.Errorf("%s declares rewritten top-level value %q", path, name.Name)
						}
					}
				}
			}
		}
	}
	return nil
}

// Checks every Go source file without importing or compiling the package under
// inspection.
func validateGoSourceTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		return validateGoSourceFile(path)
	})
}

// Validates the requested source tree and reports the first canonical failure.
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: sourceguard SIM_TESTNET_ROOT")
		os.Exit(2)
	}
	if err := validateGoSourceTree(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
