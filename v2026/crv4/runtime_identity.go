package crv4

// Runtime identity helpers deliberately use raw JSON for fields which the
// pinned GSRPC RuntimeVersion type does not expose. Callers supply the expected
// release values; this package preserves authoritative presence and bytes.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/scale"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"golang.org/x/crypto/blake2b"
)

// RuntimeVersionIdentity is the complete release-bound portion of an
// authoritative state_getRuntimeVersion response.
type RuntimeVersionIdentity struct {
	SpecName           string `json:"specName"`
	SpecVersion        uint32 `json:"specVersion"`
	TransactionVersion uint32 `json:"transactionVersion"`
	StateVersion       uint8  `json:"stateVersion"`
}

// Decode one required unsigned JSON integer without accepting strings,
// fractions, exponents, signs, null, leading zeroes, or width truncation.
func decodeRuntimeVersionUint(name string, raw json.RawMessage, bits int) (uint64, error) {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 {
		return 0, fmt.Errorf("runtime version field %s is empty", name)
	}
	for index, character := range value {
		if character < '0' || character > '9' || (index == 0 && character == '0' && len(value) != 1) {
			return 0, fmt.Errorf("runtime version field %s has invalid unsigned integer encoding %q", name, value)
		}
	}
	decoded, err := strconv.ParseUint(string(value), 10, bits)
	if err != nil {
		return 0, fmt.Errorf("runtime version field %s is outside uint%d: %w", name, bits, err)
	}
	return decoded, nil
}

// DecodeRuntimeVersionIdentity retains required-field presence. Unknown
// forward-compatible fields are ignored, while duplicate release-bound fields
// and trailing JSON are rejected.
func DecodeRuntimeVersionIdentity(raw json.RawMessage) (RuntimeVersionIdentity, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil {
		return RuntimeVersionIdentity{}, fmt.Errorf("decode runtime version object: %w", err)
	}
	delimiter, ok := opening.(json.Delim)
	if !ok || delimiter != '{' {
		return RuntimeVersionIdentity{}, errors.New("runtime version result is not an object")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		keyToken, keyErr := decoder.Token()
		if keyErr != nil {
			return RuntimeVersionIdentity{}, fmt.Errorf("decode runtime version field name: %w", keyErr)
		}
		key, ok := keyToken.(string)
		if !ok {
			return RuntimeVersionIdentity{}, errors.New("runtime version field name is not a string")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return RuntimeVersionIdentity{}, fmt.Errorf("decode runtime version field %s: %w", key, err)
		}
		switch key {
		case "specName", "specVersion", "transactionVersion", "stateVersion":
			if _, exists := fields[key]; exists {
				return RuntimeVersionIdentity{}, fmt.Errorf("runtime version field %s is duplicated", key)
			}
			fields[key] = value
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return RuntimeVersionIdentity{}, fmt.Errorf("close runtime version object: %w", err)
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != '}' {
		return RuntimeVersionIdentity{}, errors.New("runtime version object is not closed")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		if err == nil {
			return RuntimeVersionIdentity{}, errors.New("runtime version result has trailing JSON")
		}
		return RuntimeVersionIdentity{}, fmt.Errorf("decode runtime version trailing JSON: %w", err)
	}
	for _, name := range []string{"specName", "specVersion", "transactionVersion", "stateVersion"} {
		if _, exists := fields[name]; !exists {
			return RuntimeVersionIdentity{}, fmt.Errorf("runtime version field %s is missing", name)
		}
	}

	var version RuntimeVersionIdentity
	if err := json.Unmarshal(fields["specName"], &version.SpecName); err != nil || version.SpecName == "" {
		return RuntimeVersionIdentity{}, errors.New("runtime version field specName is not a nonempty string")
	}
	specVersion, err := decodeRuntimeVersionUint("specVersion", fields["specVersion"], 32)
	if err != nil {
		return RuntimeVersionIdentity{}, err
	}
	transactionVersion, err := decodeRuntimeVersionUint("transactionVersion", fields["transactionVersion"], 32)
	if err != nil {
		return RuntimeVersionIdentity{}, err
	}
	stateVersion, err := decodeRuntimeVersionUint("stateVersion", fields["stateVersion"], 8)
	if err != nil {
		return RuntimeVersionIdentity{}, err
	}
	version.SpecVersion = uint32(specVersion)
	version.TransactionVersion = uint32(transactionVersion)
	version.StateVersion = uint8(stateVersion)
	return version, nil
}

// RuntimeVersionAt reads the complete runtime version at one caller-selected
// block rather than silently dropping stateVersion through the GSRPC type.
func RuntimeVersionAt(chain *Chain, blockHash types.Hash) (RuntimeVersionIdentity, error) {
	if chain == nil || chain.API == nil || chain.API.Client == nil || blockHash == (types.Hash{}) {
		return RuntimeVersionIdentity{}, errors.New("runtime version read dependencies are unavailable")
	}
	var raw json.RawMessage
	if err := chain.API.Client.Call(&raw, "state_getRuntimeVersion", blockHash.Hex()); err != nil {
		return RuntimeVersionIdentity{}, fmt.Errorf("read state_getRuntimeVersion at %s: %w", blockHash.Hex(), err)
	}
	version, err := DecodeRuntimeVersionIdentity(raw)
	if err != nil {
		return RuntimeVersionIdentity{}, fmt.Errorf("decode state_getRuntimeVersion at %s: %w", blockHash.Hex(), err)
	}
	return version, nil
}

// RuntimeCodeHashAt reads the authoritative System.Code storage hash at one
// explicit block. Shape validation belongs to the release caller.
func RuntimeCodeHashAt(chain *Chain, blockHash types.Hash) (string, error) {
	if chain == nil || chain.API == nil || chain.API.Client == nil || blockHash == (types.Hash{}) {
		return "", errors.New("runtime code-hash read dependencies are unavailable")
	}
	var codeHash string
	if err := chain.API.Client.Call(&codeHash, "state_getStorageHash", "0x3a636f6465", blockHash.Hex()); err != nil {
		return "", err
	}
	return strings.ToLower(codeHash), nil
}

// DecodeRuntimeMetadata returns decoded metadata plus BLAKE2b-256 of the exact
// SCALE bytes in one authoritative state_getMetadata result. It never hashes a
// re-encoding.
func DecodeRuntimeMetadata(encoded string) (*types.Metadata, string, error) {
	if len(encoded) <= 2 || !strings.HasPrefix(encoded, "0x") || len(encoded)%2 != 0 {
		return nil, "", errors.New("runtime metadata is not canonical even-length 0x hex")
	}
	raw, err := hex.DecodeString(encoded[2:])
	if err != nil {
		return nil, "", fmt.Errorf("decode runtime metadata hex: %w", err)
	}
	metadata := new(types.Metadata)
	reader := bytes.NewReader(raw)
	if err := scale.NewDecoder(reader).Decode(metadata); err != nil {
		return nil, "", fmt.Errorf("decode runtime metadata SCALE: %w", err)
	}
	if reader.Len() != 0 {
		return nil, "", fmt.Errorf("runtime metadata has %d trailing SCALE bytes", reader.Len())
	}
	digest := blake2b.Sum256(raw)
	return metadata, "0x" + hex.EncodeToString(digest[:]), nil
}

// RuntimeMetadataAt reads and authenticates metadata at one explicit block.
func RuntimeMetadataAt(chain *Chain, blockHash types.Hash) (*types.Metadata, string, error) {
	if chain == nil || chain.API == nil || chain.API.Client == nil || blockHash == (types.Hash{}) {
		return nil, "", errors.New("runtime metadata read dependencies are unavailable")
	}
	var encoded string
	if err := chain.API.Client.Call(&encoded, "state_getMetadata", blockHash.Hex()); err != nil {
		return nil, "", err
	}
	return DecodeRuntimeMetadata(encoded)
}
