package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func qualificationToolDirectory(t *testing.T) string {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"asm", "compile", "link", "cgo", "vet", "preprofile"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("fixture executable "+name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func TestQualificationToolchainPinsInstalledAndAbsentTools(t *testing.T) {
	directory := qualificationToolDirectory(t)
	census, err := captureGoToolCensus(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(census.Files) != 6 || !reflect.DeepEqual(census.AbsentLegacy, []string{"pack", "test2json"}) {
		t.Fatalf("actual tool census differs: %+v", census)
	}
	if err := checkGoToolCensus(census); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pack", "test2json"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("legacy executable "+name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkGoToolCensus(census); err == nil {
		t.Fatal("new legacy executables did not change admitted tool custody")
	}
	withLegacy, err := captureGoToolCensus(directory)
	if err != nil || len(withLegacy.Files) != 8 || len(withLegacy.AbsentLegacy) != 0 {
		t.Fatalf("installed legacy executables were not pinned: %+v/%v", withLegacy, err)
	}
}

func TestQualificationToolchainRejectsMissingOrChangedExecutable(t *testing.T) {
	for _, name := range []string{"asm", "compile", "link", "cgo"} {
		t.Run("missing-"+name, func(t *testing.T) {
			directory := qualificationToolDirectory(t)
			if err := os.Remove(filepath.Join(directory, name)); err != nil {
				t.Fatal(err)
			}
			if _, err := captureGoToolCensus(directory); err == nil {
				t.Fatalf("missing %s was accepted", name)
			}
		})
	}
	for _, change := range []string{"bytes", "mode", "symlink", "new-tool"} {
		t.Run(change, func(t *testing.T) {
			directory := qualificationToolDirectory(t)
			census, err := captureGoToolCensus(directory)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "compile")
			switch change {
			case "bytes":
				testWrite(t, path, "changed compiler")
			case "mode":
				err = os.Chmod(path, 0600)
			case "symlink":
				if err = os.Remove(path); err == nil {
					err = os.Symlink(filepath.Join(directory, "asm"), path)
				}
			case "new-tool":
				err = os.WriteFile(filepath.Join(directory, "pack"), []byte("new executable"), 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			owner := &stageOwner{Capture: t.TempDir(), Inputs: census.Files, GoTools: census}
			if _, err := owner.command(t.Context(), "must-not-start", []string{"unused"}, directory, 1, ""); err == nil {
				t.Fatalf("changed %s tool custody admitted a command", change)
			}
			if _, err := os.Lstat(filepath.Join(owner.Capture, "must-not-start.request.json")); !os.IsNotExist(err) {
				t.Fatalf("changed tool custody created a request: %v", err)
			}
		})
	}
}

func TestQualificationToolchainRetainsResolvedConverterWithoutCacheLifetime(t *testing.T) {
	directory := qualificationToolDirectory(t)
	source := filepath.Join(directory, "cached test2json")
	if err := os.WriteFile(source, []byte("actual converter executable"), 0755); err != nil {
		t.Fatal(err)
	}
	stdout := filepath.Join(directory, "resolution.stdout")
	testWrite(t, stdout, source+"\n")
	retained, err := retainResolvedGoTool(stdout, filepath.Join(directory, "owned-converter"))
	if err != nil {
		t.Fatal(err)
	}
	if retained.SourcePath != source || retained.SourceProof.SHA256 != retained.Proof.SHA256 || retained.Proof.Mode != 0700 {
		t.Fatalf("converter provenance differs: %+v", retained)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	proofs := map[string]fileProof{retained.Path: retained.Proof}
	if err := checkProofs(proofs); err != nil {
		t.Fatalf("cache eviction changed the owned converter: %v", err)
	}
	testWrite(t, retained.Path, "substituted converter")
	if err := checkProofs(proofs); err == nil {
		t.Fatal("changed actual converter escaped its executable proof")
	}
}

func TestQualificationToolchainRejectsInvalidConverterResolution(t *testing.T) {
	for _, kind := range []string{"empty", "unterminated", "multiple", "relative", "symlink", "non-executable"} {
		t.Run(kind, func(t *testing.T) {
			directory := qualificationToolDirectory(t)
			source := filepath.Join(directory, "compile")
			value := source + "\n"
			switch kind {
			case "empty":
				value = ""
			case "unterminated":
				value = source
			case "multiple":
				value += source + "\n"
			case "relative":
				value = "compile\n"
			case "symlink":
				alias := filepath.Join(directory, "alias")
				if err := os.Symlink(source, alias); err != nil {
					t.Fatal(err)
				}
				value = alias + "\n"
			case "non-executable":
				if err := os.Chmod(source, 0600); err != nil {
					t.Fatal(err)
				}
			}
			stdout := filepath.Join(directory, "resolution.stdout")
			testWrite(t, stdout, value)
			destination := filepath.Join(directory, "owned-converter")
			if _, err := retainResolvedGoTool(stdout, destination); err == nil {
				t.Fatalf("accepted %s converter resolution", kind)
			}
			if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatalf("invalid converter resolution wrote a destination: %v", err)
			}
		})
	}
}
