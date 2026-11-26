// Captured mainnet metadata exercises the production successor storage gate;
// it grants no runtime authority by itself.
package validator

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

// One bounded gzip member of a crv4 fixture, matched by size and both digests.
func runtimeSuccessorStorageTestMetadata(t *testing.T, name string, size int, sha256Hex string, metadataHash string) *types.Metadata {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join("..", "crv4", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.NewReader(compressed)
	reader, err := gzip.NewReader(input)
	if err != nil {
		t.Fatal(err)
	}
	reader.Multistream(false)
	raw, readErr := io.ReadAll(io.LimitReader(reader, int64(size)+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(raw) != size || input.Len() != 0 {
		t.Fatalf("bounded %s member differs: read=%v close=%v bytes=%d trailing=%d", name, readErr, closeErr, len(raw), input.Len())
	}
	if digest := sha256.Sum256(raw); hex.EncodeToString(digest[:]) != sha256Hex {
		t.Fatalf("%s SHA256 differs", name)
	}
	metadata, hash, err := crv4.DecodeRuntimeMetadata(fmt.Sprintf("0x%x", raw))
	if err != nil || hash != metadataHash {
		t.Fatalf("%s normal metadata decoder/pin: %v %s", name, err, hash)
	}
	return metadata
}

// A successor is admitted only if the owner census, eligibility and treasury
// custody storage it decodes keep their shapes. Runtime488 passed this in
// production on 2026-11-06; runtime490 (2026-11-10), runtime491 (2026-11-16),
// runtime492 (2026-11-19), runtime493 (2026-11-20), runtime494 (2026-11-23, transaction version 2), runtime495
// (2026-11-24, transaction version 2) and runtime497 (2026-11-25, transaction version 2) must pass it too, or the
// reviewed API tuple alone would still leave the validator refusing them.
func TestReviewedRuntimeSuccessorsKeepProductionStorage(t *testing.T) {
	for _, runtime := range []struct {
		name, sha256, hash string
		size               int
	}{
		{name: "runtime488-metadata.scale.gz.base64", size: 392151, sha256: "66804d7b7f326f3285f900a1533df30c17ffa1d2855a5c3f3559ae05471446b7", hash: "0x9ae276aa9166d8d4c72deb8d14e802df2171df3c3232198bbff65d5fe205e0ca"},
		{name: "runtime490-metadata.scale.gz.base64", size: 395984, sha256: "6c1f9314e97e88d9ec4ffbdc1e2f7335e86c2b41bf27e726bef5bd3fa5305401", hash: "0x58fd4d5d2a68ee7d6491c98979127b3ecedd54c7fbc0fd6a04794bade14458d3"},
		{name: "runtime491-metadata.scale.gz.base64", size: 395751, sha256: "5e7d5b553f5fd70d2fa6e53a529f28c811e8c23686ccae45975bcfeb053df137", hash: "0xae0df84c73d0dd04ebcab7d184a228b9e49d9817fa127f2203c627afe29d24a0"},
		{name: "runtime492-metadata.scale.gz.base64", size: 395745, sha256: "ca6174da65c56320bc449b0c1c98d6202c65339c40955eebcc914538a27bb878", hash: "0xc132138eacebe032e0871d2a2cbd75abad88d19a2bf8640205e268206d6d0cd0"},
		{name: "runtime493-metadata.scale.gz.base64", size: 395608, sha256: "4ed0c6997e668a995c9dc4ea4c8d587d1625c193fc3361a7781e49156a8db9df", hash: "0x25137f5e9ee03a4fbf1d6eade4bafb8e65a72a1f94a68526d495e3c944c1ed04"},
		{name: "runtime494-metadata.scale.gz.base64", size: 406936, sha256: "a2f8737e3b8acdf7bffbd764c587c5696e7bec9b2453be8d69bcc990306bf093", hash: "0x5378d4cbeebda6a70798f94f6c7ba6a10551bb54ff3506d96902e8b0cac12d3a"},
		{name: "runtime495-metadata.scale.gz.base64", size: 409264, sha256: "cfebbb78baf01a47a9b0bbd97888b46d07a593594f65ca7df231fa9b1b097343", hash: "0x15e58ecd5d479b0aff4e811f8e108db7b97715c288fe67c7d6fe659708f40ed6"},
		{name: "runtime497-metadata.scale.gz.base64", size: 409264, sha256: "d54cab26911b8c2d840b5bc0534945e43d43cf7be23266d5100f9c0c64ecdd4e", hash: "0x9a042e5c8bb2864af87d97cfa6739ad4d3d1a852de901df51e6a8d843bc76451"},
	} {
		t.Run(runtime.name, func(t *testing.T) {
			metadata := runtimeSuccessorStorageTestMetadata(t, runtime.name, runtime.size, runtime.sha256, runtime.hash)
			for _, treasury := range []bool{false, true} {
				if err := validateReleaseRuntimeSuccessorStorage(metadata, treasury); err != nil {
					t.Fatalf("successor storage (treasury=%v) refused: %v", treasury, err)
				}
			}
		})
	}
}
