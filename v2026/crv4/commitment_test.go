package crv4

import (
	"bytes"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

func TestFleetCommitmentInfoExactRuntime447Scale(t *testing.T) {
	var hash [32]byte
	for i := range hash {
		hash[i] = byte(i + 1)
	}
	got, err := EncodeFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0x04, 0x83}, hash[:]...)
	if !bytes.Equal(got, want) {
		t.Fatalf("SCALE = %x, want %x", got, want)
	}

	// Pin the type's direct encoder as well as the public helper so a future
	// struct-tag/reflection change cannot silently alter the dispatch bytes.
	info, err := NewFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := codec.Encode(info)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(direct, want) {
		t.Fatalf("direct SCALE = %x, want %x", direct, want)
	}
}

func TestFleetCommitmentInfoRejectsZero(t *testing.T) {
	if _, err := NewFleetCommitmentInfo([32]byte{}); err == nil {
		t.Fatal("expected zero hash rejection")
	}
	if _, err := EncodeFleetCommitmentInfo([32]byte{}); err == nil {
		t.Fatal("expected zero hash rejection")
	}
}
