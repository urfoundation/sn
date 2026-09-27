package chain

// Call construction is checked against the reviewed runtime-461 metadata: the
// call index resolves to the named dispatchable and the argument bytes are the
// exact SCALE layout the harness's registrations used.

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/binary"
	"io"
	"os"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/urfoundation/sn/v2026/crv4"
)

func reviewedRuntime461Metadata(t *testing.T) *types.Metadata {
	t.Helper()
	encoded, err := os.ReadFile("../miner/testdata/runtime461-metadata.scale.gz.base64")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(io.LimitReader(reader, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	metadata, hash, err := crv4.DecodeRuntimeMetadata(hexutil.Encode(raw))
	if err != nil {
		t.Fatal(err)
	}
	identity, ok := crv4.ReviewedRuntimeArtifact(crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 461, TransactionVersion: 1, StateVersion: 1})
	if !ok || hash != identity.MetadataHash {
		t.Fatalf("fixture is not the reviewed 461 metadata: %s", hash)
	}
	return metadata
}

func callIndex(t *testing.T, chain *crv4.Chain, name string) uint8 {
	t.Helper()
	report, err := chain.DescribeCall(crv4.PalletName, name)
	if err != nil || !report.Found {
		t.Fatalf("%s: %+v %v", name, report, err)
	}
	return report.CallIndex
}

func TestRegisterLimitCallMatchesReviewedRuntimeLayout(t *testing.T) {
	metadata := reviewedRuntime461Metadata(t)
	chain := &crv4.Chain{Meta: metadata}
	hotkey := [32]byte{7, 7, 7}
	call, err := BurnRegisterLimitCall(metadata, 521, hotkey, 1_500_000_000)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := codec.Encode(call)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{call.CallIndex.SectionIndex, callIndex(t, chain, "register_limit")}
	want = append(want, NetuidArg(521)...)
	want = append(want, hotkey[:]...)
	want = binary.LittleEndian.AppendUint64(want, 1_500_000_000)
	if !bytes.Equal(encoded, want) {
		t.Fatalf("register_limit encoding differs:\n got %x\nwant %x", encoded, want)
	}
	for _, testCase := range []struct {
		name   string
		netuid uint16
		hotkey [32]byte
		limit  uint64
	}{
		{name: "zero netuid", netuid: 0, hotkey: hotkey, limit: 1},
		{name: "zero hotkey", netuid: 521, limit: 1},
		{name: "zero limit", netuid: 521, hotkey: hotkey},
	} {
		if _, err := BurnRegisterLimitCall(metadata, testCase.netuid, testCase.hotkey, testCase.limit); err == nil {
			t.Fatalf("%s: register_limit accepted an incomplete request", testCase.name)
		}
	}
}

func TestStakeCallsMatchReviewedRuntimeLayout(t *testing.T) {
	metadata := reviewedRuntime461Metadata(t)
	chain := &crv4.Chain{Meta: metadata}
	hotkey := [32]byte{9, 9, 9}
	plain, err := AddStakeCall(metadata, 25, hotkey, 250_000_000)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := codec.Encode(plain)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{plain.CallIndex.SectionIndex, callIndex(t, chain, "add_stake")}
	want = append(want, hotkey[:]...)
	want = append(want, NetuidArg(25)...)
	want = binary.LittleEndian.AppendUint64(want, 250_000_000)
	if !bytes.Equal(encoded, want) {
		t.Fatalf("add_stake encoding differs:\n got %x\nwant %x", encoded, want)
	}
	limit, err := AddStakeLimitCall(metadata, 25, hotkey, 250_000_000, 3_000_000_000, true)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = codec.Encode(limit)
	if err != nil {
		t.Fatal(err)
	}
	want = []byte{limit.CallIndex.SectionIndex, callIndex(t, chain, "add_stake_limit")}
	want = append(want, hotkey[:]...)
	want = append(want, NetuidArg(25)...)
	want = binary.LittleEndian.AppendUint64(want, 250_000_000)
	want = binary.LittleEndian.AppendUint64(want, 3_000_000_000)
	want = append(want, 1)
	if !bytes.Equal(encoded, want) {
		t.Fatalf("add_stake_limit encoding differs:\n got %x\nwant %x", encoded, want)
	}
	if _, err := AddStakeCall(metadata, 25, hotkey, 0); err == nil {
		t.Fatal("add_stake accepted a zero amount")
	}
	if _, err := AddStakeLimitCall(metadata, 25, hotkey, 1, 0, false); err == nil {
		t.Fatal("add_stake_limit accepted a zero limit price")
	}
}
