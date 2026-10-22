// Configuration and custody tests use synthetic identities and temporary files;
// no fixture contains an operational key, endpoint, or deployment identity.
package payoutroster

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/protocol"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

// All temporary paths are physical so this fixture does not depend on the
// host's temporary-directory alias policy.
func commandTestDirectory(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// Valid public configuration does not require opening a signing key or state.
func commandTestConfig(t *testing.T) Config {
	t.Helper()
	directory := commandTestDirectory(t)
	return Config{
		Schema: ConfigSchema,
		Domain: protocol.ClientKeyHistoryDomain{
			ChainID: 1234, GenesisHash: [32]byte{1}, Netuid: 17,
			Coordinator: common.Address{19: 1}, SettlementVault: common.Address{19: 2},
			DeploymentIDHash: [32]byte{2}, PolicyHash: [32]byte{3}, NoID: 7,
		},
		RequestPublicKey: [32]byte{4}, AuthoritySigner: common.Address{19: 3},
		ArtifactSigner: common.Address{19: 4}, ClientKeyRootSigner: common.Address{19: 5},
		ApiBase: "https://roster.example/operator", KeyFile: filepath.Join(directory, "vault", "roster.key"),
		StateDirectory: filepath.Join(directory, "state"), InboxDirectory: filepath.Join(directory, "inbox"),
	}
}

// The fixture emits operator-friendly hex strings, including both supported
// prefix spellings, instead of relying on the protocol's byte-array JSON form.
func commandTestConfigBytes(t *testing.T, config Config) []byte {
	t.Helper()
	raw, err := yaml.Marshal(configDocument{
		Schema: config.Schema,
		Domain: configDomain{
			ChainId: config.Domain.ChainID, GenesisHash: "0x" + hex.EncodeToString(config.Domain.GenesisHash[:]),
			Netuid: config.Domain.Netuid, Coordinator: config.Domain.Coordinator, SettlementVault: config.Domain.SettlementVault,
			DeploymentIdHash: hex.EncodeToString(config.Domain.DeploymentIDHash[:]), PolicyHash: "0x" + hex.EncodeToString(config.Domain.PolicyHash[:]),
			NoId: config.Domain.NoID,
		},
		RequestPublicKey: hex.EncodeToString(config.RequestPublicKey[:]), AuthoritySigner: config.AuthoritySigner,
		ArtifactSigner: config.ArtifactSigner, ClientKeyRootSigner: config.ClientKeyRootSigner,
		ApiBase: config.ApiBase, KeyFile: config.KeyFile, StateDirectory: config.StateDirectory, InboxDirectory: config.InboxDirectory,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Every test owns one newly created private file.
func commandTestWrite(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Hex fields and explicit nested protocol names round-trip into the expected
// public typed configuration while missing signing custody remains untouched.
func TestPayoutRosterConfigLoadsHexDomain(t *testing.T) {
	expected := commandTestConfig(t)
	path := filepath.Join(commandTestDirectory(t), "payout_roster.yml")
	commandTestWrite(t, path, commandTestConfigBytes(t, expected))
	actual, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("public configuration changed: got %#v, want %#v", actual, expected)
	}
	if _, err := os.Stat(expected.KeyFile); !os.IsNotExist(err) {
		t.Fatalf("loading public config touched the key: %v", err)
	}
}

// Strict decoding rejects ambiguous config shapes without returning rejected
// source values in diagnostics.
func TestPayoutRosterConfigRejectsUnknownDuplicateAndTrailingYaml(t *testing.T) {
	config := commandTestConfig(t)
	raw := commandTestConfigBytes(t, config)
	const rejectedValue = "synthetic-sensitive-value"
	cases := []struct {
		name string
		raw  []byte
	}{
		{name: "unknown root", raw: append(bytes.Clone(raw), []byte("unexpected: "+rejectedValue+"\n")...)},
		{name: "unknown domain", raw: bytes.Replace(raw, []byte("domain:\n"), []byte("domain:\n    unexpected: "+rejectedValue+"\n"), 1)},
		{name: "duplicate root", raw: append(bytes.Clone(raw), []byte("schema: "+ConfigSchema+"\n")...)},
		{name: "trailing document", raw: append(bytes.Clone(raw), []byte("---\nunexpected: "+rejectedValue+"\n")...)},
		{name: "null document", raw: append(bytes.Clone(raw), []byte("---\nnull\n")...)},
	}
	path := filepath.Join(commandTestDirectory(t), "config.yml")
	for _, c := range cases {
		commandTestWrite(t, path, c.raw)
		_, err := LoadConfig(path)
		if err == nil {
			t.Errorf("%s accepted", c.name)
		} else if strings.Contains(err.Error(), rejectedValue) {
			t.Errorf("%s leaked rejected source data", c.name)
		}
	}
}

// Invalid key lengths and scalar spellings cannot be silently padded or
// converted from YAML numeric arrays into an approved identity.
func TestPayoutRosterConfigRejectsMalformedHexFields(t *testing.T) {
	config := commandTestConfig(t)
	raw := commandTestConfigBytes(t, config)
	encoded := hex.EncodeToString(config.RequestPublicKey[:])
	path := filepath.Join(commandTestDirectory(t), "config.yml")
	for _, value := range []string{"04", strings.Repeat("z", 64), "0x" + strings.Repeat("0", 62), "[4, 0]", "\" " + encoded + "\""} {
		changed := bytes.Replace(raw, []byte("request_public_key: \""+encoded+"\""), []byte("request_public_key: "+value), 1)
		if bytes.Equal(changed, raw) {
			changed = bytes.Replace(raw, []byte("request_public_key: "+encoded), []byte("request_public_key: "+value), 1)
		}
		if bytes.Equal(changed, raw) {
			t.Fatal("fixture could not find request public key field")
		}
		commandTestWrite(t, path, changed)
		if _, err := LoadConfig(path); err == nil {
			t.Errorf("malformed public key accepted: %q", value)
		}
	}
}

// Every missing public role, domain component, and custody conflict fails
// before a file or transport can be used.
func TestPayoutRosterConfigRejectsIncompleteOrConfusedCustody(t *testing.T) {
	valid := commandTestConfig(t)
	cases := []struct {
		name   string
		change func(*Config)
	}{
		{name: "schema", change: func(config *Config) { config.Schema = "unsupported" }},
		{name: "domain", change: func(config *Config) { config.Domain.PolicyHash = [32]byte{} }},
		{name: "request key", change: func(config *Config) { config.RequestPublicKey = [32]byte{} }},
		{name: "authority signer", change: func(config *Config) { config.AuthoritySigner = common.Address{} }},
		{name: "artifact signer", change: func(config *Config) { config.ArtifactSigner = common.Address{} }},
		{name: "root signer", change: func(config *Config) { config.ClientKeyRootSigner = common.Address{} }},
		{name: "signer role confusion", change: func(config *Config) { config.ArtifactSigner = config.AuthoritySigner }},
		{name: "relative key", change: func(config *Config) { config.KeyFile = "vault/roster.key" }},
		{name: "noncanonical key", change: func(config *Config) { config.KeyFile = "/synthetic/../roster.key" }},
		{name: "root state", change: func(config *Config) { config.StateDirectory = "/" }},
		{name: "shared inbox", change: func(config *Config) { config.InboxDirectory = config.StateDirectory }},
		{name: "nested inbox", change: func(config *Config) { config.InboxDirectory = filepath.Join(config.StateDirectory, "inbox") }},
		{name: "nested state", change: func(config *Config) { config.StateDirectory = filepath.Join(config.InboxDirectory, "state") }},
		{name: "inbox key", change: func(config *Config) { config.KeyFile = filepath.Join(config.InboxDirectory, "key") }},
		{name: "state key", change: func(config *Config) { config.KeyFile = filepath.Join(config.StateDirectory, "key") }},
	}
	for _, c := range cases {
		config := valid
		c.change(&config)
		if err := config.Validate(); err == nil {
			t.Errorf("invalid %s accepted", c.name)
		}
	}
}

// HTTPS base selection excludes credentials, request selectors, aliases, and
// malformed URLs, and diagnostics never echo rejected endpoint contents.
func TestPayoutRosterConfigRejectsUnsafeApiBases(t *testing.T) {
	config := commandTestConfig(t)
	for _, endpoint := range []string{
		"http://roster.example", "https://", "https://secret:password@roster.example", "https://roster.example?secret=value",
		"https://roster.example?", "https://roster.example#secret", "https:opaque", "https://roster.example/a%2fb", "https://roster.example:invalid",
	} {
		config.ApiBase = endpoint
		if err := config.Validate(); err == nil {
			t.Errorf("unsafe API base accepted: %s", endpoint)
		} else if strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "value") {
			t.Errorf("endpoint contents escaped into diagnostic: %v", err)
		}
	}
}

// The key loader decodes only a private physical file and never needs the
// network or the configured API, state, or inbox.
func TestPayoutRosterSigningKeyLoadsDedicatedHexFile(t *testing.T) {
	keyBytes := bytes.Repeat([]byte{0x11}, 32)
	path := filepath.Join(commandTestDirectory(t), "roster.key")
	for _, prefix := range []string{"", "0x"} {
		commandTestWrite(t, path, []byte("\n"+prefix+hex.EncodeToString(keyBytes)+"\n"))
		key, err := LoadSigningKey(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(crypto.FromECDSA(key), keyBytes) {
			t.Fatal("loaded key differs from dedicated file")
		}
	}
}

// Both a leaf symlink and an ancestor symlink must fail even when their target
// contains a well-formed private synthetic signing key.
func TestPayoutRosterSigningKeyRejectsSymlinkComponents(t *testing.T) {
	directory := commandTestDirectory(t)
	physical := filepath.Join(directory, "physical")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(physical, "roster.key")
	commandTestWrite(t, keyPath, []byte(strings.Repeat("11", 32)))
	leaf := filepath.Join(directory, "leaf.key")
	ancestor := filepath.Join(directory, "alias")
	if err := os.Symlink(keyPath, leaf); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(physical, ancestor); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{leaf, filepath.Join(ancestor, "roster.key")} {
		if _, err := LoadSigningKey(path); err == nil {
			t.Errorf("symlink path accepted: %s", path)
		}
	}
}

// Shared permissions and a second hard link both defeat private key custody.
func TestPayoutRosterSigningKeyRejectsPermissionsAndHardlinks(t *testing.T) {
	directory := commandTestDirectory(t)
	path := filepath.Join(directory, "roster.key")
	commandTestWrite(t, path, []byte(strings.Repeat("11", 32)))
	for _, mode := range []os.FileMode{0o400, 0o640, 0o644, 0o666} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSigningKey(path); err == nil {
			t.Errorf("key mode %o accepted", mode)
		}
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, filepath.Join(directory, "other.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSigningKey(path); err == nil {
		t.Fatal("multiply linked key accepted")
	}
}

// Opening a FIFO must return without waiting for a writer, and oversized files
// are rejected before reading them into a dynamically growing buffer.
func TestPayoutRosterFileReadersRejectNonregularAndOversizedFiles(t *testing.T) {
	directory := commandTestDirectory(t)
	fifo := filepath.Join(directory, "queue.key")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSigningKey(fifo); err == nil {
		t.Fatal("FIFO accepted as a key")
	}
	if _, err := LoadSigningKey(directory); err == nil {
		t.Fatal("directory accepted as a key")
	}
	oversized := filepath.Join(directory, "oversized.yml")
	commandTestWrite(t, oversized, bytes.Repeat([]byte{'x'}, maxConfigBytes+1))
	if _, err := LoadConfig(oversized); err == nil {
		t.Fatal("oversized config accepted")
	}
	if _, err := LoadSigningKey(oversized); err == nil {
		t.Fatal("oversized key accepted")
	}
}

// Failure messages must remain independent of the rejected key contents.
func TestPayoutRosterSigningKeyErrorsDoNotExposeBytes(t *testing.T) {
	path := filepath.Join(commandTestDirectory(t), "roster.key")
	for _, raw := range []string{strings.Repeat("z", 64), strings.Repeat("00", 32), "synthetic-secret-not-a-key"} {
		commandTestWrite(t, path, []byte(raw))
		_, err := LoadSigningKey(path)
		if err == nil || strings.Contains(err.Error(), raw) {
			t.Fatalf("key error leaked or accepted rejected data: %v", err)
		}
	}
}
