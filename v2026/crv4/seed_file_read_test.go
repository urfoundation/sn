//go:build linux || darwin

package crv4

// Provisioned-key regressions keep format policy separate from descriptor
// acquisition. Every byte, alias, timestamp and directory is test-owned.

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// All three grammars retain raw32 precedence. Only the historical hotkey
// parser permits 0x/Unicode; raw-only consumers never accept hex encodings.
func TestSeedCustodyProvisionedParsersKeepSeparateGrammars(t *testing.T) {
	raw := bytes.Repeat([]byte{0xab}, 32)
	encoded := hex.EncodeToString(raw)
	for _, input := range []struct {
		name string
		wire []byte
		want []byte
		bare bool
		hot  bool
	}{
		{name: "raw", wire: raw, want: raw, bare: true, hot: true},
		{name: "raw-space", wire: bytes.Repeat([]byte{' '}, 32), want: bytes.Repeat([]byte{' '}, 32), bare: true, hot: true},
		{name: "lower", wire: []byte(encoded), want: raw, bare: true, hot: true},
		{name: "upper", wire: []byte(strings.ToUpper(encoded)), want: raw, bare: true, hot: true},
		{name: "ascii", wire: []byte(" \t\r\n" + encoded + "\n\r\t "), want: raw, bare: true, hot: true},
		{name: "prefix", wire: []byte("0x" + encoded), want: raw, hot: true},
		{name: "upper-prefix", wire: []byte("0X" + encoded)},
		{name: "unicode", wire: []byte("\u00a0" + encoded + "\u00a0"), want: raw, hot: true},
		{name: "vertical-tab", wire: []byte("\v" + encoded + "\v"), want: raw, hot: true},
		{name: "interior", wire: []byte(encoded[:32] + " " + encoded[32:])},
		{name: "partial-decode", wire: []byte(encoded[:62] + "zz")},
		{name: "empty"},
		{name: "short", wire: bytes.Repeat([]byte{'x'}, 31)},
		{name: "long", wire: bytes.Repeat([]byte{'x'}, 33)},
	} {
		original := append([]byte(nil), input.wire...)
		for _, format := range []struct {
			name  string
			parse func([]byte) ([32]byte, error)
			valid bool
		}{
			{name: "raw", parse: parseRawSeedFile, valid: len(input.wire) == 32},
			{name: "bare", parse: parseRawOrBareHexSeedFile, valid: input.bare},
			{name: "hotkey", parse: parseSeedFile, valid: input.hot},
		} {
			seed, err := format.parse(input.wire)
			if format.valid {
				if err != nil || !bytes.Equal(seed[:], input.want) {
					t.Fatalf("%s/%s changed accepted seed format: %v", input.name, format.name, err)
				}
			} else if err == nil || seed != ([32]byte{}) {
				t.Fatalf("%s/%s returned rejected format authority: %v", input.name, format.name, err)
			}
			if !bytes.Equal(input.wire, original) {
				t.Fatalf("%s/%s changed caller-owned bytes", input.name, format.name)
			}
		}
	}
}

// Neither provisioned reader may create absent ancestry or a missing leaf.
// Nil parser refusal also precedes any attempt to acquire a namespace.
func TestSeedCustodyProvisionedReadersNeverCreate(t *testing.T) {
	for _, load := range []func(string) ([32]byte, error){LoadRawSeedFile, LoadRawOrBareHexSeedFile} {
		path := seedCustodyTestPath(t)
		for _, missing := range []string{path, filepath.Join(filepath.Dir(path), "missing", "nested", "seed")} {
			seed, err := load(missing)
			if !errors.Is(err, os.ErrNotExist) || seed != ([32]byte{}) {
				t.Fatalf("missing provisioned key returned authority: %v", err)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 0 {
				t.Fatalf("provisioned read created missing state: %v", err)
			}
		}
	}
	if seed, err := loadSeedFileParsed("", nil, seedFileHooks{}); err == nil || seed != ([32]byte{}) || !strings.Contains(err.Error(), "parser is missing") {
		t.Fatalf("missing parser acquired a namespace: %v", err)
	}
}

// The exact maximum remains usable for the old ASCII grammar. Oversize files
// are rejected before leaf acquisition or parsing, not after unbounded reads.
func TestSeedCustodyProvisionedReaderBoundsBeforeParsing(t *testing.T) {
	for _, size := range []int{maximumSeedFileBytes, maximumSeedFileBytes + 1} {
		path := seedCustodyTestPath(t)
		wire := append(bytes.Repeat([]byte{' '}, size-64), bytes.Repeat([]byte{'a'}, 64)...)
		if err := os.WriteFile(path, wire, 0o600); err != nil {
			t.Fatal(err)
		}
		observed, parses := 0, 0
		seed, err := loadSeedFileParsed(path, func(raw []byte) ([32]byte, error) {
			parses++
			if len(raw) > maximumSeedFileBytes {
				t.Fatal("provisioned parser received bytes beyond the hard bound")
			}
			return parseRawOrBareHexSeedFile(raw)
		}, seedFileHooks{step: func(operation, _ string) error {
			if operation == "leaf-observed" {
				observed++
			}
			return nil
		}})
		if size == maximumSeedFileBytes {
			if err != nil || parses != 1 || observed != 1 || !bytes.Equal(seed[:], bytes.Repeat([]byte{0xaa}, 32)) {
				t.Fatalf("maximum valid provisioned seed rejected: parses=%d observed=%d error=%v", parses, observed, err)
			}
		} else if err == nil || parses != 0 || observed != 0 || seed != ([32]byte{}) {
			t.Fatalf("oversized seed passed bounded acquisition: parses=%d observed=%d error=%v", parses, observed, err)
		}
		after, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(wire, after) {
			t.Fatalf("bounded refusal changed provisioned bytes: %v", readErr)
		}
	}
}

// Every acquired file closes on parse, read or post-close failure; errors
// from either the leaf or a retained parent must revoke returned authority.
func TestSeedCustodyProvisionedReaderClosesDescriptorsOnEveryReturn(t *testing.T) {
	for _, failureAt := range []string{"none", "parse", "seed-read", "leaf-close", "parent-close", "parent-observed"} {
		path := seedCustodyTestPath(t)
		raw := bytes.Repeat([]byte{0x71}, 32)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		failure := errors.New("test-owned provisioned read failure")
		closed, parses := 0, 0
		seed, err := loadSeedFileParsed(path, func(wire []byte) ([32]byte, error) {
			parses++
			if failureAt == "parse" {
				return [32]byte{1}, failure
			}
			return parseRawOrBareHexSeedFile(wire)
		}, seedFileHooks{
			step: func(operation, _ string) error {
				if operation == failureAt {
					return failure
				}
				return nil
			},
			afterClose: func(file *os.File) error {
				closed++
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Errorf("%s observed an unclosed seed descriptor: %v", failureAt, err)
				}
				if failureAt == "leaf-close" && closed == 1 || failureAt == "parent-close" && closed == 2 {
					return failure
				}
				return nil
			},
		})
		components := len(strings.Split(strings.TrimPrefix(filepath.Dir(path), string(filepath.Separator)), string(filepath.Separator)))
		wantClosed, wantParses := components+2, 1
		if failureAt == "parent-observed" {
			wantClosed, wantParses = components, 0
		} else if failureAt == "seed-read" {
			wantParses = 0
		}
		if closed != wantClosed || parses != wantParses {
			t.Fatalf("%s ownership counts closed=%d/%d parses=%d/%d", failureAt, closed, wantClosed, parses, wantParses)
		}
		if failureAt == "none" {
			if err != nil || !bytes.Equal(seed[:], raw) {
				t.Fatalf("complete descriptor lifecycle refused the seed: %v", err)
			}
		} else if !errors.Is(err, failure) || seed != ([32]byte{}) {
			t.Fatalf("%s failed to revoke returned seed: %v", failureAt, err)
		}
		after, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(raw, after) {
			t.Fatalf("%s changed provisioned bytes: %v", failureAt, readErr)
		}
	}
	directory, err := openSeedFileDirectory(seedCustodyTestPath(t), false, seedFileHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if err := directory.close(); err != nil {
		t.Fatal(err)
	}
	for _, root := range directory.roots {
		if _, err := root.Stat("."); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("retained directory root survived its owner: %v", err)
		}
	}
}

// Descriptor equality alone does not authorize a same-size rewrite after the
// read. Explicit old timestamps make the forced mutation clock-independent.
func TestSeedCustodyProvisionedReaderRejectsLateMutations(t *testing.T) {
	for _, stage := range []string{"seed-read", "load-sync"} {
		for _, mutation := range []string{"rewrite", "replacement", "shared-mode"} {
			path := seedCustodyTestPath(t)
			original := bytes.Repeat([]byte{0x72}, 32)
			replacement := bytes.Repeat([]byte{0x73}, 32)
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			fired := false
			seed, err := loadSeedFileParsed(path, parseRawOrBareHexSeedFile, seedFileHooks{step: func(operation, _ string) error {
				if operation != stage {
					return nil
				}
				fired = true
				if mutation == "shared-mode" {
					return os.Chmod(path, 0o644)
				}
				if mutation == "replacement" {
					if err := os.Rename(path, path+"-preserved"); err != nil {
						return err
					}
				}
				if err := os.WriteFile(path, replacement, 0o600); err != nil {
					return err
				}
				return os.Chtimes(path, time.Unix(1, 0), time.Unix(1, 0))
			}})
			if !fired || err == nil || seed != ([32]byte{}) {
				t.Fatalf("%s/%s returned late-mutated authority: %v", stage, mutation, err)
			}
			want := replacement
			if mutation == "shared-mode" {
				want = original
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(after, want) {
				t.Fatalf("%s/%s reader rewrote the fixture mutation: %v", stage, mutation, readErr)
			}
			if mutation == "replacement" {
				preserved, readErr := os.ReadFile(path + "-preserved")
				if readErr != nil || !bytes.Equal(preserved, original) {
					t.Fatalf("%s lost the original key: %v", stage, readErr)
				}
			}
		}
	}
}

// The new no-create wrapper must reach the same anchored acquisition checks,
// including parent and leaf changes forced before actual descriptor opening.
func TestSeedCustodyProvisionedReaderRejectsAcquisitionChanges(t *testing.T) {
	for _, stage := range []string{"parent-observed", "parent-opened", "leaf-observed"} {
		path := seedCustodyTestPath(t)
		original := bytes.Repeat([]byte{0x74}, 32)
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		fired := false
		preserved := path + "-preserved"
		seed, err := loadSeedFileParsed(path, parseRawOrBareHexSeedFile, seedFileHooks{step: func(operation, _ string) error {
			if operation != stage {
				return nil
			}
			fired = true
			if stage == "leaf-observed" {
				if err := os.Rename(path, preserved); err != nil {
					return err
				}
				return os.WriteFile(path, bytes.Repeat([]byte{0x75}, 32), 0o600)
			}
			parent := filepath.Dir(path)
			preserved = filepath.Join(parent+"-preserved", filepath.Base(path))
			if err := os.Rename(parent, parent+"-preserved"); err != nil {
				return err
			}
			return os.Mkdir(parent, 0o700)
		}})
		if !fired || err == nil || seed != ([32]byte{}) {
			t.Fatalf("%s returned changed acquisition authority: %v", stage, err)
		}
		retained, readErr := os.ReadFile(preserved)
		if readErr != nil || !bytes.Equal(retained, original) {
			t.Fatalf("%s changed the preserved original: %v", stage, readErr)
		}
	}
}

// Existing read-only/private leaves and non-shared listed parents stay valid;
// reading never repairs their modes or changes raw-only format policy.
func TestSeedCustodyProvisionedReadersPreserveReadonlyFiles(t *testing.T) {
	for _, parentMode := range []os.FileMode{0o700, 0o755} {
		for _, fileMode := range []os.FileMode{0o400, 0o600} {
			path := seedCustodyTestPath(t)
			raw := bytes.Repeat([]byte{0x76}, 32)
			if err := os.WriteFile(path, raw, fileMode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Dir(path), parentMode); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, load := range []func(string) ([32]byte, error){LoadRawSeedFile, LoadRawOrBareHexSeedFile} {
				seed, err := load(path)
				if err != nil || !bytes.Equal(seed[:], raw) {
					t.Fatalf("compatible provisioned file refused: %v", err)
				}
			}
			after, err := os.Lstat(path)
			parent, parentErr := os.Lstat(filepath.Dir(path))
			if err != nil || parentErr != nil || !os.SameFile(before, after) || after.Mode() != fileMode || parent.Mode().Perm() != parentMode {
				t.Fatalf("read changed private file or parent: %v/%v", err, parentErr)
			}
		}
	}
}
