package crv4

// Provisioned keys use the same descriptor custody as generated identities,
// with explicit no-create format entry points. A caller's grammar is not
// widened merely because another seed format accepts more text.

import (
	"encoding/hex"
	"errors"
	"path/filepath"
)

// Loads only an existing private raw32 file, without creating parents or keys.
func LoadRawSeedFile(path string) ([32]byte, error) {
	return loadSeedFileParsed(path, parseRawSeedFile, seedFileHooks{})
}

// Loads an existing private file containing raw32 or bare64 hexadecimal with
// only ASCII space, tab, CR and LF at its edges. Raw32 always takes precedence.
// Optional 0x prefixes and Unicode whitespace are not part of this grammar.
// Acquisition is no-create and bounded to the shared 4096-byte file maximum.
func LoadRawOrBareHexSeedFile(path string) ([32]byte, error) {
	return loadSeedFileParsed(path, parseRawOrBareHexSeedFile, seedFileHooks{})
}

// A parse, read, sync or close failure never returns a usable provisioned key.
// This entry point cannot consume entropy or enter the publication path.
func loadSeedFileParsed(path string, parse func([]byte) ([32]byte, error), hooks seedFileHooks) (seed [32]byte, resultErr error) {
	if parse == nil {
		return seed, errors.New("seed parser is missing")
	}
	directory, err := openSeedFileDirectory(path, false, hooks)
	if err != nil {
		return seed, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, directory.close())
		if resultErr != nil {
			seed = [32]byte{}
		}
	}()
	seed, _, resultErr = directory.readWithParser(filepath.Base(path), parse)
	return seed, resultErr
}

// Raw-only callers never accept textual encodings of an otherwise valid key.
func parseRawSeedFile(raw []byte) ([32]byte, error) {
	var seed [32]byte
	if len(raw) != len(seed) {
		return seed, errors.New("seed must contain exactly 32 raw bytes")
	}
	copy(seed[:], raw)
	return seed, nil
}

// Preserve the release client's original byte grammar, not Unicode trimming.
func parseRawOrBareHexSeedFile(raw []byte) ([32]byte, error) {
	if len(raw) == 32 {
		return parseRawSeedFile(raw)
	}
	start, end := 0, len(raw)
	for start < end && (raw[start] == ' ' || raw[start] == '\n' || raw[start] == '\r' || raw[start] == '\t') {
		start++
	}
	for start < end && (raw[end-1] == ' ' || raw[end-1] == '\n' || raw[end-1] == '\r' || raw[end-1] == '\t') {
		end--
	}
	var seed [32]byte
	if end-start == hex.EncodedLen(len(seed)) {
		if _, err := hex.Decode(seed[:], raw[start:end]); err == nil {
			return seed, nil
		}
	}
	return [32]byte{}, errors.New("expected a raw or hex 32-byte Ed25519 seed")
}
