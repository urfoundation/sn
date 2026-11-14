package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Go 1.26 does not install every legacy command in GOTOOLDIR. Preserve the
// actual installed census, including explicit absence, while requiring the
// compiler tools used by this runner. A newly appearing tool changes custody.
type goToolCensus struct {
	Directory    string               `json:"directory"`
	Files        map[string]fileProof `json:"files"`
	AbsentLegacy []string             `json:"absent_legacy_tools"`
}

func captureGoToolCensus(directory string) (*goToolCensus, error) {
	if err := physicalPath(directory, true); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 || len(entries) > 128 {
		return nil, errors.New("Go tool directory has an empty or unbounded census")
	}
	census := &goToolCensus{Directory: directory, Files: map[string]fileProof{}, AbsentLegacy: []string{}}
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		proof, err := regularProof(path)
		if err != nil {
			return nil, err
		}
		if proof.Mode&0111 == 0 {
			return nil, fmt.Errorf("Go tool is not executable: %s", path)
		}
		census.Files[path] = proof
	}
	for _, name := range []string{"asm", "compile", "link", "cgo"} {
		if _, present := census.Files[filepath.Join(directory, name)]; !present {
			return nil, fmt.Errorf("missing required Go tool: %s", name)
		}
	}
	for _, name := range []string{"pack", "test2json"} {
		if _, present := census.Files[filepath.Join(directory, name)]; !present {
			census.AbsentLegacy = append(census.AbsentLegacy, name)
		}
	}
	return census, nil
}

// Existing input proofs check every installed executable's bytes and mode.
// This check additionally keeps absent tools absent and rejects new entries.
func checkGoToolCensus(census *goToolCensus) error {
	if census == nil {
		return nil // Discovery itself precedes the admitted tool census.
	}
	if err := physicalPath(census.Directory, true); err != nil {
		return err
	}
	entries, err := os.ReadDir(census.Directory)
	if err != nil {
		return err
	}
	if len(entries) != len(census.Files) {
		return errors.New("installed Go tool census changed")
	}
	for _, entry := range entries {
		if _, present := census.Files[filepath.Join(census.Directory, entry.Name())]; !present {
			return fmt.Errorf("installed Go tool census changed: %s", entry.Name())
		}
	}
	return nil
}

type resolvedGoTool struct {
	SourcePath  string    `json:"source_path"`
	SourceProof fileProof `json:"source_proof"`
	Path        string    `json:"path"`
	Proof       fileProof `json:"proof"`
}

// `go tool -n test2json` returns the real installed or cache-built command
// without executing it. Keep a private executable copy so later cache eviction
// cannot change the converter. The tool's real build remains an owned stage.
func retainResolvedGoTool(stdout, destination string) (resolvedGoTool, error) {
	data, err := readBounded(stdout, 4096)
	if err != nil {
		return resolvedGoTool{}, err
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return resolvedGoTool{}, errors.New("Go tool resolution must be one complete path")
	}
	path := strings.TrimSuffix(string(data), "\n")
	if err := physicalPath(path, false); err != nil {
		return resolvedGoTool{}, err
	}
	original, err := regularProof(path)
	if err != nil {
		return resolvedGoTool{}, err
	}
	if original.Mode&0111 == 0 {
		return resolvedGoTool{}, errors.New("resolved Go tool is not executable")
	}
	if err := copyFile(path, destination, 0700); err != nil {
		return resolvedGoTool{}, err
	}
	retained, err := regularProof(destination)
	if err != nil {
		return resolvedGoTool{}, err
	}
	if retained.SHA256 != original.SHA256 || retained.Mode != 0700 {
		return resolvedGoTool{}, errors.New("resolved Go tool changed during owned copy")
	}
	return resolvedGoTool{SourcePath: path, SourceProof: original, Path: destination, Proof: retained}, nil
}
