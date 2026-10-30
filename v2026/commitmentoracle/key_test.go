package commitmentoracle

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestLoadOracleKey(t *testing.T) {
	dir := t.TempDir()
	key := testOracleKey(t)
	encoded := hex.EncodeToString(crypto.FromECDSA(key))
	path := filepath.Join(dir, "oracle.key")
	if err := os.WriteFile(path, []byte("0x"+encoded+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadOracleKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if keyAddress(loaded) != keyAddress(key) {
		t.Fatal("loaded another key")
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOracleKey(path); err == nil || !strings.Contains(err.Error(), "group and others") {
		t.Fatalf("group-readable key: %v", err)
	}
}

// Errors never repeat any of the file's bytes.
func TestLoadOracleKeyErrorsCarryNoKeyMaterial(t *testing.T) {
	dir := t.TempDir()
	secret := strings.Repeat("5e", 31) + "zz"
	cases := []string{secret, strings.Repeat("5e", 30), "0x" + strings.Repeat("00", 32)}
	for index, content := range cases {
		path := filepath.Join(dir, "bad.key")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := loadOracleKey(path)
		if err == nil {
			t.Fatalf("case %d accepted", index)
		}
		if strings.Contains(err.Error(), "5e5e") || strings.Contains(err.Error(), "zz") {
			t.Fatalf("case %d error carries key bytes: %v", index, err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "bad.key"), filepath.Join(dir, "link.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOracleKey(filepath.Join(dir, "link.key")); err == nil {
		t.Fatal("followed a symlink")
	}
}
