package commitmentoracle

// Oracle key loading. The key file has the stctl/validator key_file format
// (64 hex characters, optional 0x, surrounding whitespace ignored) that
// onchain.LoadKeyFile reads for the validator's relayer and auditor keys, and
// is provisioned from vault/main/sn.yml commitment_oracle.private_key. Errors
// name the path and the problem, never any of the file's bytes.

import (
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

// Reads the key and refuses a file that is not a single regular file private
// to its owner.
func loadOracleKey(path string) (*ecdsa.PrivateKey, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("oracle key file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("oracle key file %s is not a regular file", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("oracle key file %s has mode %04o; group and others must have no access", path, info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("oracle key file: %w", err)
	}
	defer clear(raw)
	text := strings.TrimSpace(string(raw))
	text = strings.TrimPrefix(strings.TrimPrefix(text, "0x"), "0X")
	if len(text) != 64 {
		return nil, fmt.Errorf("oracle key file %s does not hold 64 hex characters", path)
	}
	decoded, err := hex.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("oracle key file %s does not hold 64 hex characters", path)
	}
	defer clear(decoded)
	key, err := crypto.ToECDSA(decoded)
	if err != nil {
		return nil, errors.New("oracle key file " + path + " does not hold a valid secp256k1 key")
	}
	return key, nil
}
