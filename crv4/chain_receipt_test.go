package crv4

import (
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
)

func TestExtrinsicIndexUsesScaleBlake2Hash(t *testing.T) {
	raw := []byte{0x10, 0x84, 0x01, 0x02, 0x03}
	digest := blake2b.Sum256(raw)
	want := types.Hash(digest)
	index, found, err := extrinsicIndex([]string{"0x00", codec.HexEncodeToString(raw)}, want)
	if err != nil {
		t.Fatal(err)
	}
	if !found || index != 1 {
		t.Fatalf("index=%d found=%t", index, found)
	}
	if _, found, err := extrinsicIndex([]string{"0x00"}, want); err != nil || found {
		t.Fatalf("absent hash: found=%t err=%v", found, err)
	}
	if _, _, err := extrinsicIndex([]string{"not-hex"}, want); err == nil {
		t.Fatal("malformed block extrinsic was accepted")
	}
}
