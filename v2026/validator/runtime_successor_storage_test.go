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
	"github.com/urfoundation/sn/v2026/crv4"
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
// production on 2026-11-06; runtime490 (2026-11-10) must pass it too, or the
// reviewed API tuple alone would still leave the validator refusing it.
func TestReviewedRuntimeSuccessorsKeepProductionStorage(t *testing.T) {
	for _, runtime := range []struct {
		name, sha256, hash string
		size               int
	}{
		{name: "runtime488-metadata.scale.gz.base64", size: 392151, sha256: "66804d7b7f326f3285f900a1533df30c17ffa1d2855a5c3f3559ae05471446b7", hash: "0x9ae276aa9166d8d4c72deb8d14e802df2171df3c3232198bbff65d5fe205e0ca"},
		{name: "runtime490-metadata.scale.gz.base64", size: 395984, sha256: "6c1f9314e97e88d9ec4ffbdc1e2f7335e86c2b41bf27e726bef5bd3fa5305401", hash: "0x58fd4d5d2a68ee7d6491c98979127b3ecedd54c7fbc0fd6a04794bade14458d3"},
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
