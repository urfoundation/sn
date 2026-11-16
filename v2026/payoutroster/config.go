// Configuration names approved public identities and separate local custody.
// Private key bytes are read only after the reviewed request is validated.
package payoutroster

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/protocol"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

const ConfigSchema = "urnetwork-payout-roster-config-v1"
const maxConfigBytes = 64 * 1024
const maxSigningKeyBytes = 256

// Public approval identities are independent of the payout artifact signer.
// Key, retained state, and reviewed inbox paths must be explicit and absolute.
type Config struct {
	Schema              string                          `json:"schema"`
	Domain              protocol.ClientKeyHistoryDomain `json:"domain"`
	RequestPublicKey    [32]byte                        `json:"request_public_key"`
	AuthoritySigner     common.Address                  `json:"authority_signer"`
	ArtifactSigner      common.Address                  `json:"artifact_signer"`
	ClientKeyRootSigner common.Address                  `json:"client_key_root_signer"`
	ApiBase             string                          `json:"api_base"`
	KeyFile             string                          `json:"key_file"`
	StateDirectory      string                          `json:"state_directory"`
	InboxDirectory      string                          `json:"inbox_directory"`
}

// Protocol domain fields carry JSON tags only; this explicit YAML projection
// retains the same field names and keeps unknown nested fields rejectable.
type configDomain struct {
	ChainId          uint64         `yaml:"chain_id"`
	GenesisHash      string         `yaml:"genesis_hash"`
	Netuid           uint16         `yaml:"netuid"`
	Coordinator      common.Address `yaml:"coordinator"`
	SettlementVault  common.Address `yaml:"settlement_vault"`
	DeploymentIdHash string         `yaml:"deployment_id_hash"`
	PolicyHash       string         `yaml:"policy_hash"`
	NoId             uint64         `yaml:"no_id"`
}

// This decoding-only shape prevents YAML's default lowercase field aliases
// from becoming a second configuration contract beside the protocol names.
type configDocument struct {
	Schema              string         `yaml:"schema"`
	Domain              configDomain   `yaml:"domain"`
	RequestPublicKey    string         `yaml:"request_public_key"`
	AuthoritySigner     common.Address `yaml:"authority_signer"`
	ArtifactSigner      common.Address `yaml:"artifact_signer"`
	ClientKeyRootSigner common.Address `yaml:"client_key_root_signer"`
	ApiBase             string         `yaml:"api_base"`
	KeyFile             string         `yaml:"key_file"`
	StateDirectory      string         `yaml:"state_directory"`
	InboxDirectory      string         `yaml:"inbox_directory"`
}

// Validation never opens a key or creates state. Deployment identity and all
// approved keys must be complete before any preparation or signing is admitted.
func (self Config) Validate() error {
	if self.Schema != ConfigSchema {
		return errors.New("payout roster config schema is unsupported")
	}
	if err := self.Domain.Validate(); err != nil {
		return fmt.Errorf("payout roster config domain: %w", err)
	}
	if self.RequestPublicKey == ([32]byte{}) || self.AuthoritySigner == (common.Address{}) || self.ArtifactSigner == (common.Address{}) || self.ClientKeyRootSigner == (common.Address{}) {
		return errors.New("payout roster config approved public keys are incomplete")
	}
	if self.AuthoritySigner == self.ArtifactSigner {
		return errors.New("payout roster authority signer must differ from the payout artifact signer")
	}
	for _, path := range []string{self.KeyFile, self.StateDirectory, self.InboxDirectory} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
			return errors.New("payout roster key, state, and inbox paths must be canonical absolute paths")
		}
	}
	// Separate the approved input queue from retained producer output. Neither
	// directory may contain the other or the private signing key.
	contains := func(directory, path string) bool {
		return directory == path || strings.HasPrefix(path, directory+string(filepath.Separator))
	}
	if contains(self.StateDirectory, self.InboxDirectory) || contains(self.InboxDirectory, self.StateDirectory) || contains(self.StateDirectory, self.KeyFile) || contains(self.InboxDirectory, self.KeyFile) {
		return errors.New("payout roster key, state, and inbox custody must be separate")
	}
	endpoint, err := url.Parse(self.ApiBase)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Opaque != "" || endpoint.ForceQuery || endpoint.RawPath != "" {
		return errors.New("payout roster API base must be an absolute HTTPS URL without credentials, query, or fragment")
	}
	return nil
}

// Load a bounded, physical file and reject unknown fields, duplicate mapping
// keys, and extra documents. Decoder errors omit source values by design.
func LoadConfig(path string) (Config, error) {
	raw, err := readRosterFile(path, maxConfigBytes, false)
	if err != nil {
		return Config{}, fmt.Errorf("read payout roster config: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	var document configDocument
	if err := decoder.Decode(&document); err != nil {
		return Config{}, errors.New("payout roster config is not valid strict YAML")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("payout roster config must contain exactly one YAML document")
	}
	config := Config{
		Schema: document.Schema,
		Domain: protocol.ClientKeyHistoryDomain{
			ChainID: document.Domain.ChainId,
			Netuid:  document.Domain.Netuid, Coordinator: document.Domain.Coordinator,
			SettlementVault: document.Domain.SettlementVault, NoID: document.Domain.NoId,
		},
		AuthoritySigner: document.AuthoritySigner,
		ArtifactSigner:  document.ArtifactSigner, ClientKeyRootSigner: document.ClientKeyRootSigner,
		ApiBase: document.ApiBase, KeyFile: document.KeyFile,
		StateDirectory: document.StateDirectory, InboxDirectory: document.InboxDirectory,
	}
	for _, field := range []struct {
		name    string
		encoded string
		target  *[32]byte
	}{
		{name: "domain.genesis_hash", encoded: document.Domain.GenesisHash, target: &config.Domain.GenesisHash},
		{name: "domain.deployment_id_hash", encoded: document.Domain.DeploymentIdHash, target: &config.Domain.DeploymentIDHash},
		{name: "domain.policy_hash", encoded: document.Domain.PolicyHash, target: &config.Domain.PolicyHash},
		{name: "request_public_key", encoded: document.RequestPublicKey, target: &config.RequestPublicKey},
	} {
		encoded := strings.TrimPrefix(field.encoded, "0x")
		if len(encoded) != 64 {
			return Config{}, fmt.Errorf("payout roster config %s must encode exactly 32 hexadecimal bytes", field.name)
		}
		if _, err := hex.Decode(field.target[:], []byte(encoded)); err != nil {
			return Config{}, fmt.Errorf("payout roster config %s is not valid hexadecimal", field.name)
		}
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// A dedicated Vault file contains one optional-0x hex-encoded secp256k1 key.
// Files must be current-user owned, single-link regular files with mode 0600;
// every path component is opened without following symlinks. Errors never
// expose key contents, and temporary decoded byte buffers are cleared.
func LoadSigningKey(path string) (*ecdsa.PrivateKey, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("payout roster signing key path must be canonical absolute")
	}
	raw, err := readRosterFile(path, maxSigningKeyBytes, true)
	if err != nil {
		return nil, fmt.Errorf("read payout roster signing key: %w", err)
	}
	defer clear(raw)
	encoded := bytes.TrimSpace(raw)
	if bytes.HasPrefix(encoded, []byte("0x")) {
		encoded = encoded[2:]
	}
	if len(encoded) != 64 {
		return nil, errors.New("payout roster signing key must encode exactly 32 bytes")
	}
	decoded := make([]byte, 32)
	defer clear(decoded)
	if _, err := hex.Decode(decoded, encoded); err != nil {
		return nil, errors.New("payout roster signing key is not valid hexadecimal")
	}
	key, err := crypto.ToECDSA(decoded)
	if err != nil {
		return nil, errors.New("payout roster signing key is not a valid secp256k1 scalar")
	}
	return key, nil
}

// Pin each physical directory before opening its child. This avoids both
// final-component links and substitution of a checked ancestor with a link.
func openRosterDirectory(path string, private bool) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("directory path must be canonical absolute")
	}
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	fd, err := unix.Open(string(filepath.Separator), flags, 0)
	if err != nil {
		return nil, fmt.Errorf("open physical directory: %w", err)
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		next, openErr := unix.Openat(fd, part, flags, 0)
		closeErr := unix.Close(fd)
		if openErr != nil {
			return nil, fmt.Errorf("open physical directory component: %w", openErr)
		}
		if closeErr != nil {
			unix.Close(next)
			return nil, errors.New("close physical directory component failed")
		}
		fd = next
	}
	file := os.NewFile(uintptr(fd), "payout-roster-directory")
	if private {
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0o7777 != 0o700 {
			file.Close()
			return nil, errors.New("payout roster directory must be owned by the current user with mode 0700")
		}
	}
	return file, nil
}

// Keep reads relative to the already pinned parent and reject devices, pipes,
// links, shared private files, and oversized input before allocating a body.
func openRosterFileAt(directory *os.File, name string) (*os.File, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return nil, errors.New("invalid bounded file reference")
	}
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open physical regular file: %w", err)
	}
	return os.NewFile(uintptr(fd), "payout-roster-file"), nil
}

// The caller owns and closes the descriptor. Keeping it open through approval
// retirement lets the queue authenticate the object captured by atomic rename.
func readRosterOpenedFile(file *os.File, limit int64, private bool) ([]byte, error) {
	if limit < 0 || limit > MaxRequestBytes {
		return nil, errors.New("invalid bounded file byte limit")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, errors.New("payout roster input must be a regular file")
	}
	if private && (stat.Uid != uint32(os.Geteuid()) || stat.Mode&0o7777 != 0o600 || stat.Nlink != 1) {
		return nil, errors.New("payout roster private file must be current-user owned, single-link, and mode 0600")
	}
	if stat.Size < 0 || stat.Size > limit {
		return nil, errors.New("payout roster file exceeds its byte limit")
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		clear(raw)
		return nil, errors.New("read payout roster file failed")
	}
	if int64(len(raw)) > limit {
		clear(raw)
		return nil, errors.New("payout roster file exceeds its byte limit")
	}
	return raw, nil
}

// Most readers need no retained descriptor after copying their bounded bytes.
func readRosterFileAt(directory *os.File, name string, limit int64, private bool) ([]byte, error) {
	file, err := openRosterFileAt(directory, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readRosterOpenedFile(file, limit, private)
}

// Relative command paths are resolved once; all actual reads use descriptors
// opened without following any symlink in the resulting absolute path.
func readRosterFile(path string, limit int64, private bool) ([]byte, error) {
	if path == "" {
		return nil, errors.New("payout roster file path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.New("resolve payout roster file path failed")
	}
	directory, err := openRosterDirectory(filepath.Dir(absolute), false)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	return readRosterFileAt(directory, filepath.Base(absolute), limit, private)
}
