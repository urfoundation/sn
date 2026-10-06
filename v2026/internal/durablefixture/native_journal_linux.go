//go:build linux

package durablefixture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// ProvisionNativeJournal is test-fixture enrollment, separate from application
// admission. It refuses every existing entry and never repairs retained bytes.
func ProvisionNativeJournal(t testing.TB, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	rawPath := filepath.Join(path, "native-transactions")
	if err := os.Mkdir(rawPath, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(path, "native-transactions.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var root, directory, journal syscall.Stat_t
	for target, stat := range map[string]*syscall.Stat_t{path: &root, rawPath: &directory, filepath.Join(path, "native-transactions.jsonl"): &journal} {
		if err := syscall.Stat(target, stat); err != nil {
			t.Fatal(err)
		}
	}
	empty := sha256.Sum256(nil)
	raw, err := json.Marshal(map[string]any{
		"schema": "urnetwork-native-journal-custody-v1", "directory_inode": root.Ino, "raw_directory_inode": directory.Ino,
		"committed": map[string]any{"journal_inode": journal.Ino, "journal_size": 0, "journal_sha256": hex.EncodeToString(empty[:]), "raw_count": 0, "raw_sha256": hex.EncodeToString(empty[:])},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Setxattr(path, "user.urnetwork.native-journal-custody", raw, 1); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{rawPath, path, filepath.Dir(path)} {
		directory, err := os.Open(target)
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
}
