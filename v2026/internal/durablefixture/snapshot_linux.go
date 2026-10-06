//go:build linux

// Explicit test enrollment is separate from every runtime storage constructor.
package durablefixture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"testing"
)

// Only an explicitly fresh absent snapshot is provisioned here. Each named
// marker is created once with its original application byte grammar.
func ProvisionSnapshot(t testing.TB, path, kind, name string, maximum int64, lockName string, auxiliaries map[string][]byte) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(path, name)); !os.IsNotExist(err) {
		t.Fatalf("fresh snapshot exists or cannot be observed: %v", err)
	}
	type auxiliary struct {
		Name  string `json:"name"`
		Inode uint64 `json:"inode"`
	}
	type member struct {
		Present bool   `json:"present"`
		Inode   uint64 `json:"inode"`
		Size    int64  `json:"size"`
		Sha256  string `json:"sha256"`
	}
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		t.Fatal(err)
	}
	checkpoint := struct {
		Schema         string      `json:"schema"`
		Kind           string      `json:"kind"`
		Name           string      `json:"name"`
		MaximumBytes   int64       `json:"maximum_bytes"`
		DirectoryInode uint64      `json:"directory_inode"`
		LockName       string      `json:"lock_name"`
		Auxiliaries    []auxiliary `json:"auxiliaries"`
		Committed      member      `json:"committed"`
	}{Schema: "urnetwork-durable-snapshot-head-v2", Kind: kind, Name: name, MaximumBytes: maximum, DirectoryInode: stat.Ino, LockName: lockName}
	names := make([]string, 0, len(auxiliaries))
	for name := range auxiliaries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		file, err := os.OpenFile(filepath.Join(path, name), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(auxiliaries[name]); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Sync(); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := syscall.Fstat(int(file.Fd()), &stat); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		checkpoint.Auxiliaries = append(checkpoint.Auxiliaries, auxiliary{Name: name, Inode: stat.Ino})
	}
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(kind + "\x00" + name))
	attribute := "user.urnetwork.snapshot." + hex.EncodeToString(digest[:])
	attributePath := path
	if lockName != "" {
		attributePath = filepath.Join(path, lockName)
	}
	if err := syscall.Setxattr(attributePath, attribute, raw, 1); err != nil {
		t.Fatal(err)
	}
	attributeFile, err := os.Open(attributePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := attributeFile.Sync(); err != nil {
		attributeFile.Close()
		t.Fatal(err)
	}
	if err := attributeFile.Close(); err != nil {
		t.Fatal(err)
	}
	directory, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := directory.Sync(); err != nil {
		directory.Close()
		t.Fatal(err)
	}
	if err := directory.Close(); err != nil {
		t.Fatal(err)
	}
}
