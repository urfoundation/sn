// Wrong-pin truncated SCALE must be rejected before vector decoding. This
// reproduces the prior decoder-before-hash panic without allocating a huge vector.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The incomplete registry length formerly reached a decoder that assumes its
// compact-length read succeeded. Independent artifact rejection must happen first.
func TestOwnerRecycleUnapprovedMetadataRejectedBeforeScale(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, false)
	for _, encoded := range []string{"0x6d6574610e", "0x6d6574610e01", "0x" + strings.Repeat("00", maxMetadataRpcReplyBytes+1)} {
		if _, _, err := nativePinnedMetadata(encoded, f.config.Action.Policy.RuntimeMetadataHash); !errors.Is(err, errNativeMetadataPin) {
			t.Fatal("unapproved bytes reached SCALE decoder", err)
		}
	}
	if _, err := prepareOwnerRecycleAction(f.config.Action, "0x6d6574610e"); !errors.Is(err, errNativeMetadataPin) {
		t.Fatal("recycle preparation decoded before pinning", err)
	}
	trim := newOwnerTrimActionTestFixture(t)
	if _, err := prepareOwnerTrimAction(trim.config.Action, "0x6d6574610e"); !errors.Is(err, errNativeMetadataPin) {
		t.Fatal("adjacent trim preparation decoded before pinning", err)
	}
	root, _, _ := rootActionFixture(t)
	if _, err := prepareRootAction(root, "0x6d6574610e"); !errors.Is(err, errNativeMetadataPin) {
		t.Fatal("adjacent historical root preparation decoded before pinning", err)
	}
	chain, fixture, request, signed := ownerRecycleTestChain(t, f, 1, true)
	fixture.fault = func(method string, _ []json.RawMessage, _ int) (any, bool) {
		if method == "state_getMetadata" {
			return "0x6d6574610e", true
		}
		return nil, false
	}
	if _, err := chain.reconcile(context.Background(), request, signed); !errors.Is(err, errNativeMetadataPin) {
		t.Fatal("native receipt decoded unapproved RPC metadata", err)
	}
}
