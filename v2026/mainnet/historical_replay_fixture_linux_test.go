//go:build linux

// Linux-only observations: sealed memfd engines, inotify read census and
// native change times reported through FileInfo.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Exporting through a retained protected image keeps its exact path and bytes.
func TestHistoricalReplayFixturePreservesProtectedEngine(t *testing.T) {
	for _, mode := range []os.FileMode{0500, 0755} {
		path := filepath.Join(t.TempDir(), "selected-engine")
		raw := []byte("\x7fELFprotected fixture")
		if err := os.WriteFile(path, raw, mode); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		directory := t.TempDir()
		reference, err := historicalReplayFixtureEngine(path, directory)
		if err != nil || reference.Path != path || reference.Sha256 != monitorReadDigest(raw) {
			t.Fatal("protected selected exporter engine was replaced", reference, err)
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() {
			t.Fatal("protected selected engine changed", err)
		}
		initial, final := before.Sys().(*syscall.Stat_t), after.Sys().(*syscall.Stat_t)
		if initial.Nlink != final.Nlink || initial.Mtim != final.Mtim || initial.Ctim != final.Ctim {
			t.Fatal("protected selected engine custody metadata changed")
		}
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 0 {
			t.Fatal("protected selected engine created an unnecessary copy", err)
		}
	}
}

// Every inadmissible permission/link field copies without altering the source.
func TestHistoricalReplayFixtureOwnsTemporaryEngine(t *testing.T) {
	for _, c := range []struct {
		name string
		mode os.FileMode
		link bool
	}{
		{name: "not-executable", mode: 0600},
		{name: "group-writable", mode: 0770},
		{name: "world-writable", mode: 0702},
		{name: "setuid", mode: 0700 | os.ModeSetuid},
		{name: "setgid", mode: 0700 | os.ModeSetgid},
		{name: "cache-hardlink", mode: 0500, link: true},
	} {
		root := t.TempDir()
		path := filepath.Join(root, "temporary-engine")
		raw := []byte("\x7fELFtemporary fixture")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, c.mode); err != nil {
			t.Fatal(err)
		}
		if c.link {
			if err := os.Link(path, filepath.Join(root, "cache-link")); err != nil {
				t.Fatal(err)
			}
		}
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		reference, err := historicalReplayFixtureEngine(path, t.TempDir())
		if err != nil {
			t.Fatal(c.name, err)
		}
		owned, err := os.Stat(reference.Path)
		if err != nil {
			t.Fatal(err)
		}
		stat, ok := owned.Sys().(*syscall.Stat_t)
		if !ok || reference.Path == path || os.SameFile(before, owned) || owned.Mode().Perm() != 0500 || stat.Nlink != 1 || reference.Sha256 != monitorReadDigest(raw) {
			t.Fatal("temporary engine did not acquire private custody", c.name, reference)
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() {
			t.Fatal("fixture changed its original temporary engine", c.name, err)
		}
		initial, final := before.Sys().(*syscall.Stat_t), after.Sys().(*syscall.Stat_t)
		if initial.Nlink != final.Nlink || initial.Mtim != final.Mtim || initial.Ctim != final.Ctim {
			t.Fatal("fixture changed its original custody metadata", c.name)
		}
		retained, err := os.ReadFile(reference.Path)
		if err != nil || !bytes.Equal(raw, retained) {
			t.Fatal("private engine copy changed executable bytes", c.name, err)
		}
	}
}
