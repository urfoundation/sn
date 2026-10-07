// Fixed SCALE/Blake2 vectors cover the native multisig boundary, including the
// compact-vector prefix transition. These public identities own no private key.
package crv4

import (
	"fmt"
	"testing"
)

// The source tuple encodes a fixed15-byte domain, AccountId vector and u16.
func TestNativeMultisigAccountPinnedSourceVectors(t *testing.T) {
	for _, item := range []struct {
		count int
		want  string
	}{
		{3, "28ce16de72e6ac66ecae7834b9fa7fe74bdf15352d5329ab4a1010c68902a759"},
		{64, "c103375b5470afb4a6fc24dcbed5102b96f4688099fb9dc919ef2ab49ab95c55"},
		{100, "9a51a6b69d7b5f010ef4489c5dfa2852edaadefe6649a6999867b5a95fde1c4c"},
	} {
		keys := make([][32]byte, item.count)
		for i := range keys {
			keys[i][0] = byte(i + 1)
		}
		got, err := DeriveNativeMultisigAccount(keys, 2)
		if err != nil || fmt.Sprintf("%x", got) != item.want {
			t.Fatalf("native source vector%d: %x %v", item.count, got, err)
		}
		other, err := DeriveNativeMultisigAccount(keys, 3)
		if err != nil || other == got {
			t.Fatal("threshold did not select a distinct account", err)
		}
	}
}

// Account membership is authority; malformed sets must never be normalized.
func TestNativeMultisigAccountRejectsUnreachableOrAmbiguousAuthority(t *testing.T) {
	for _, item := range []struct {
		keys      [][32]byte
		threshold uint16
	}{
		{nil, 2}, {[][32]byte{{1}}, 2}, {[][32]byte{{1}, {2}}, 1},
		{[][32]byte{{1}, {2}}, 3}, {[][32]byte{{2}, {1}}, 2},
		{[][32]byte{{1}, {1}}, 2}, {[][32]byte{{}, {2}}, 2},
		{make([][32]byte, 101), 2},
	} {
		if _, err := DeriveNativeMultisigAccount(item.keys, item.threshold); err == nil {
			t.Fatal("accepted ambiguous native multisig authority", item)
		}
	}
}
