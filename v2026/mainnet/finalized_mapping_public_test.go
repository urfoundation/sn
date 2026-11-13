// Synthetic public-RPC regressions bind every RLP15 field to independently
// encoded header bytes. No live chain identity or production capture is used.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// The external Ethereum JSON encoder supplies field spellings and quantities;
// only the documented Frontier projection differences are applied afterward.
func mappingTestPublicBlock(t *testing.T, header *types.Header, transactionHashes []string) map[string]any {
	t.Helper()
	raw, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["timestamp"] = fmt.Sprintf("0x%x", header.Time/1000)
	fields["baseFeePerGas"] = "0x7"
	fields["author"] = header.Coinbase.Hex()
	fields["transactions"] = transactionHashes
	fields["uncles"] = []string{}
	fields["size"] = "0x123"
	fields["totalDifficulty"] = "0x0"
	delete(fields, "mixHash")
	for _, field := range []string{"withdrawalsRoot", "blobGasUsed", "excessBlobGas", "parentBeaconBlockRoot", "requestsHash"} {
		delete(fields, field)
	}
	return fields
}

// All serialized values vary from their zero/defaults except the omitted mix
// digest. The native and EVM heights deliberately retain their unequal values.
func newPublicFinalizedMappingFixture(t *testing.T, variant byte, remainder uint64) (*rpcClient, *finalizedMappingFixture) {
	t.Helper()
	client, fixture := newFinalizedMappingFixture(t, variant, 2)
	header := fixture.evmHeader
	header.Coinbase = common.HexToAddress("0x" + strings.Repeat("ac", 20))
	header.TxHash = common.HexToHash("0x" + strings.Repeat("bc", 32))
	header.ReceiptHash = common.HexToHash("0x" + strings.Repeat("cd", 32))
	header.Bloom[0], header.Bloom[127], header.Bloom[255] = 1, 2, 4
	header.Difficulty = new(big.Int).Lsh(big.NewInt(1), 180)
	header.GasUsed = 54321
	header.Time = 1700000000000 + remainder
	header.Extra = []byte{0, 1, 127, 128, 255}
	header.Nonce = types.EncodeNonce(19)
	raw, err := rlp.EncodeToBytes(header)
	if err != nil {
		t.Fatal(err)
	}
	fixture.rawEvmHeader = "0x" + hex.EncodeToString(raw)
	fixture.evmHash = header.Hash().Hex()
	fixture.replaceNativeLogs(t, []string{mappingTestDigest(t, variant, fixture.evmHash, fixture.transactionHashes)})
	fixture.renderedEvmBlock = mappingTestPublicBlock(t, header, fixture.transactionHashes)
	fixture.fault = func(method string, _ []any, _ int) (any, bool) {
		return mappingFixtureRpcError{code: -32601}, method == "debug_getRawHeader"
	}
	return client, fixture
}

// Both unsupported method and unsupported object-selector responses can use
// the public view; each exact RLP remains independently replayable afterward.
func TestFinalizedMappingPublicHeaderRecoversExactRlp(t *testing.T) {
	for _, variant := range []byte{1, 3} {
		for _, remainder := range []uint64{0, 1, 127, 128, 255, 256, 999} {
			client, fixture := newPublicFinalizedMappingFixture(t, variant, remainder)
			if remainder%2 == 0 {
				fixture.fault = func(method string, _ []any, _ int) (any, bool) {
					return mappingFixtureRpcError{code: -32602}, method == "debug_getRawHeader"
				}
			}
			mapping, err := client.readFinalizedMapping(t.Context(), nil)
			if err != nil || mapping.EvmHeader.HeaderRlp != fixture.rawEvmHeader || mapping.EvmHeader.Hash != fixture.evmHash || mapping.EvmHeader.Number != 37 || mapping.Identity.FinalizedNumber != 100 ||
				mapping.Admission != "unapproved_observation" || mapping.RuntimeSourceProven || mapping.FinalityAuthority != "owned-rpc-assertion" || fixture.counts["debug_getRawHeader"] != 1 || fixture.counts["eth_getBlockByHash"] != 1 || fixture.counts["eth_getBlockByNumber"] != 2 {
				t.Fatalf("variant=%d remainder=%d lost exact scope: %+v calls=%v err=%v", variant, remainder, mapping, fixture.counts, err)
			}
			raw, _ := hex.DecodeString(mapping.EvmHeader.HeaderRlp[2:])
			var decoded types.Header
			if err := rlp.DecodeBytes(raw, &decoded); err != nil || !reflect.DeepEqual(&decoded, fixture.evmHeader) {
				t.Fatalf("public projection lost a committed field: %+v err=%v", decoded, err)
			}
		}
	}
}

// A correct hash echo cannot hide even one changed serialized field. Removing
// the optional author alias makes the miner case reach the actual hash gate.
func TestRenderedFrontierHeaderChecksEveryCommittedField(t *testing.T) {
	changes := []struct {
		field string
		value any
	}{
		{field: "parentHash", value: "0x" + strings.Repeat("11", 32)},
		{field: "sha3Uncles", value: "0x" + strings.Repeat("22", 32)},
		{field: "miner", value: "0x" + strings.Repeat("33", 20)},
		{field: "stateRoot", value: "0x" + strings.Repeat("44", 32)},
		{field: "transactionsRoot", value: "0x" + strings.Repeat("55", 32)},
		{field: "receiptsRoot", value: "0x" + strings.Repeat("66", 32)},
		{field: "logsBloom", value: "0x" + strings.Repeat("77", 256)},
		{field: "difficulty", value: "0x8"},
		{field: "number", value: "0x26"},
		{field: "gasLimit", value: "0x9"},
		{field: "gasUsed", value: "0xa"},
		{field: "timestamp", value: "0x6553f101"},
		{field: "extraData", value: "0xff"},
		{field: "mixHash", value: "0x" + strings.Repeat("88", 32)},
		{field: "nonce", value: "0x0000000000000014"},
	}
	for _, change := range changes {
		_, fixture := newPublicFinalizedMappingFixture(t, 1, 321)
		fields := fixture.renderedEvmBlock.(map[string]any)
		delete(fields, "author")
		fields[change.field] = change.value
		raw, _ := json.Marshal(fields)
		header, err := authenticateRenderedFrontierHeader(t.Context(), raw, fixture.evmHash)
		if !errors.Is(err, errRpcIntegrity) || header != (mappedEvmHeader{}) || !strings.Contains(err.Error(), "cannot reproduce the native commitment") {
			t.Errorf("field %s bypassed the exact hash gate: %+v err=%v", change.field, header, err)
		}
	}
}

// Defaults for omitted miner/nonce (including zero-valued originals) are not
// evidence. Only the source-documented omitted mix digest has a bounded value.
func TestRenderedFrontierHeaderRequiresCompleteProjection(t *testing.T) {
	for _, field := range []string{"hash", "parentHash", "sha3Uncles", "miner", "stateRoot", "transactionsRoot", "receiptsRoot", "logsBloom", "difficulty", "number", "gasLimit", "gasUsed", "timestamp", "extraData", "nonce"} {
		for _, absent := range []bool{false, true} {
			_, fixture := newFinalizedMappingFixture(t, 3, 0)
			fields := mappingTestPublicBlock(t, fixture.evmHeader, fixture.transactionHashes)
			fields[field] = nil
			if absent {
				delete(fields, field)
			}
			raw, _ := json.Marshal(fields)
			header, err := authenticateRenderedFrontierHeader(t.Context(), raw, fixture.evmHash)
			if !errors.Is(err, errFinalizedMappingUnavailable) || header != (mappedEvmHeader{}) || !strings.Contains(err.Error(), field) {
				t.Errorf("field=%s absent=%v was defaulted: %+v err=%v", field, absent, header, err)
			}
		}
	}
}

// An explicit mix digest is serialized verbatim. Its omission can recover only
// a zero digest; the committed hash prevents guessing a different value.
func TestRenderedFrontierHeaderBindsExplicitAndOmittedMixHash(t *testing.T) {
	_, fixture := newPublicFinalizedMappingFixture(t, 3, 999)
	header := fixture.evmHeader
	header.MixDigest = common.HexToHash("0x" + strings.Repeat("de", 32))
	fields := mappingTestPublicBlock(t, header, fixture.transactionHashes)
	fields["mixHash"] = header.MixDigest.Hex()
	raw, _ := json.Marshal(fields)
	mapped, err := authenticateRenderedFrontierHeader(t.Context(), raw, header.Hash().Hex())
	expected, _ := rlp.EncodeToBytes(header)
	if err != nil || mapped.HeaderRlp != "0x"+hex.EncodeToString(expected) {
		t.Fatalf("explicit mix digest did not survive: %+v err=%v", mapped, err)
	}
	delete(fields, "mixHash")
	raw, _ = json.Marshal(fields)
	if mapped, err := authenticateRenderedFrontierHeader(t.Context(), raw, header.Hash().Hex()); !errors.Is(err, errRpcIntegrity) || mapped != (mappedEvmHeader{}) {
		t.Fatalf("omitted nonzero mix digest was invented: %+v err=%v", mapped, err)
	}
}

// Runtime base fee is a documented projection annotation, never an optional
// header-layout guess. A genuine sixteen-field hash cannot pass the profile.
func TestRenderedFrontierHeaderKeepsOneLegacyProfile(t *testing.T) {
	_, fixture := newPublicFinalizedMappingFixture(t, 1, 123)
	fields := fixture.renderedEvmBlock.(map[string]any)
	for _, baseFee := range []any{nil, "0x0", "0xffff"} {
		fields["baseFeePerGas"] = baseFee
		raw, _ := json.Marshal(fields)
		mapped, err := authenticateRenderedFrontierHeader(t.Context(), raw, fixture.evmHash)
		if err != nil || mapped.HeaderRlp != fixture.rawEvmHeader {
			t.Fatalf("runtime base fee changed stored header: %+v err=%v", mapped, err)
		}
	}
	fixture.evmHeader.BaseFee = big.NewInt(7)
	fields["baseFeePerGas"] = "0x7"
	fields["hash"] = fixture.evmHeader.Hash().Hex()
	raw, _ := json.Marshal(fields)
	if mapped, err := authenticateRenderedFrontierHeader(t.Context(), raw, fixture.evmHeader.Hash().Hex()); !errors.Is(err, errRpcIntegrity) || mapped != (mappedEvmHeader{}) {
		t.Fatalf("unreviewed sixteen-field header admitted: %+v err=%v", mapped, err)
	}
}

// Future serialized fields cannot be silently discarded to manufacture a
// legacy candidate, even when the legacy hash would otherwise match exactly.
func TestRenderedFrontierHeaderRefusesUnknownHeaderProfiles(t *testing.T) {
	for _, field := range []string{"withdrawalsRoot", "blobGasUsed", "excessBlobGas", "parentBeaconBlockRoot", "requestsHash", "syntheticFutureRoot"} {
		_, fixture := newPublicFinalizedMappingFixture(t, 3, 0)
		fields := fixture.renderedEvmBlock.(map[string]any)
		fields[field] = "0x1"
		raw, _ := json.Marshal(fields)
		mapped, err := authenticateRenderedFrontierHeader(t.Context(), raw, fixture.evmHash)
		if !errors.Is(err, errFinalizedMappingUnavailable) || mapped != (mappedEvmHeader{}) {
			t.Errorf("profile extension %s silently discarded: %+v err=%v", field, mapped, err)
		}
	}
}

// Zero and the last representable millisecond exercise RLP integer boundaries
// and the partial final second without overflowing multiplication or addition.
func TestRenderedFrontierHeaderBoundsTimestampSearch(t *testing.T) {
	for _, timestamp := range []uint64{0, 1, 999, 1000, ^uint64(0) - 1, ^uint64(0)} {
		_, fixture := newPublicFinalizedMappingFixture(t, 3, 0)
		header := fixture.evmHeader
		header.Time = timestamp
		header.Number = new(big.Int).SetUint64(^uint64(0))
		fields := mappingTestPublicBlock(t, header, fixture.transactionHashes)
		raw, _ := json.Marshal(fields)
		mapped, err := authenticateRenderedFrontierHeader(t.Context(), raw, header.Hash().Hex())
		expected, _ := rlp.EncodeToBytes(header)
		if err != nil || mapped.HeaderRlp != "0x"+hex.EncodeToString(expected) || mapped.Number != ^uint64(0) {
			t.Fatalf("timestamp %d failed exact bounded recovery: %+v err=%v", timestamp, mapped, err)
		}
	}
}

// Strict lengths, integer widths and quantities fail before any successful
// header can be published; an RPC echo never supplies missing encoding bytes.
func TestRenderedFrontierHeaderRejectsMalformedAndOversizedFields(t *testing.T) {
	cases := []struct {
		field string
		value any
	}{
		{field: "hash", value: testGenesisHash},
		{field: "parentHash", value: "0x00"},
		{field: "miner", value: "0x00"},
		{field: "logsBloom", value: "0x00"},
		{field: "nonce", value: "0x0"},
		{field: "mixHash", value: "0x00"},
		{field: "author", value: "0x" + strings.Repeat("ab", 20)},
		{field: "number", value: "0x10000000000000000"},
		{field: "number", value: "0x025"},
		{field: "difficulty", value: "0x1" + strings.Repeat("0", 64)},
		{field: "gasLimit", value: "0x10000000000000000"},
		{field: "gasUsed", value: "0x10000000000000000"},
		{field: "timestamp", value: fmt.Sprintf("0x%x", ^uint64(0)/1000+1)},
		{field: "extraData", value: "0x0"},
		{field: "extraData", value: "0x" + strings.Repeat("ff", maximumMappingHeaderBytes)},
		{field: "extraData", value: "0x" + strings.Repeat("ff", maximumMappingHeaderBytes+1)},
		{field: "baseFeePerGas", value: "0x01"},
	}
	for _, testCase := range cases {
		_, fixture := newPublicFinalizedMappingFixture(t, 1, 0)
		fields := fixture.renderedEvmBlock.(map[string]any)
		fields[testCase.field] = testCase.value
		raw, _ := json.Marshal(fields)
		mapped, err := authenticateRenderedFrontierHeader(t.Context(), raw, fixture.evmHash)
		if !errors.Is(err, errRpcIntegrity) || mapped != (mappedEvmHeader{}) {
			t.Errorf("malformed %s admitted: %+v err=%v", testCase.field, mapped, err)
		}
	}
	_, fixture := newPublicFinalizedMappingFixture(t, 1, 0)
	raw, _ := json.Marshal(fixture.renderedEvmBlock)
	duplicate := append([]byte(`{"number":"0x25",`), raw[1:]...)
	for _, invalid := range [][]byte{[]byte(`null`), []byte(`[]`), []byte(`"header"`), duplicate, bytes.Repeat([]byte{' '}, maxRpcReplyBytes+1)} {
		mapped, err := authenticateRenderedFrontierHeader(t.Context(), invalid, fixture.evmHash)
		if !errors.Is(err, errRpcIntegrity) || mapped != (mappedEvmHeader{}) {
			t.Errorf("invalid JSON or reply bound admitted: %+v err=%v", mapped, err)
		}
	}
}

// A working raw method stays primary. Null, malformed, mismatching, oversized
// and unrelated RPC failures cannot be hidden by a healthy public projection.
func TestFinalizedMappingRawEvidenceDoesNotFallThrough(t *testing.T) {
	for _, reply := range []any{nil, "0x01", 17, mappingFixtureRpcError{code: -32004}, "0x" + strings.Repeat("ab", maxRpcReplyBytes)} {
		client, fixture := newPublicFinalizedMappingFixture(t, 1, 1)
		fixture.fault = func(method string, _ []any, _ int) (any, bool) { return reply, method == "debug_getRawHeader" }
		mapping, err := client.readFinalizedMapping(t.Context(), nil)
		if err == nil || mapping.Schema != "" || fixture.counts["eth_getBlockByHash"] != 0 || fixture.counts["eth_getBlockByNumber"] != 0 {
			t.Fatalf("raw refusal hidden by fallback: schema=%s calls=%v err=%v", mapping.Schema, fixture.counts, err)
		}
	}
	client, fixture := newPublicFinalizedMappingFixture(t, 1, 1)
	fixture.fault = nil
	mapping, err := client.readFinalizedMapping(t.Context(), nil)
	if err != nil || mapping.EvmHeader.HeaderRlp != fixture.rawEvmHeader || fixture.counts["eth_getBlockByHash"] != 0 {
		t.Fatalf("working raw method did not remain primary: schema=%s calls=%v err=%v", mapping.Schema, fixture.counts, err)
	}
}

// The public candidate is still subject to native/route checks, the complete
// transaction vector and the final canonical EVM read after artifact capture.
func TestFinalizedSnapshotPublicHeaderKeepsCanonicalRechecks(t *testing.T) {
	for _, changed := range []string{"native", "evm", "transactions"} {
		client, fixture := newPublicFinalizedMappingFixture(t, 1, 789)
		fixture.fault = func(method string, params []any, count int) (any, bool) {
			if method == "debug_getRawHeader" {
				return mappingFixtureRpcError{code: -32601}, true
			}
			if changed == "native" && method == "chain_getBlockHash" && fixture.counts["eth_getBlockByHash"] == 1 && params[0] == float64(100) {
				return testGenesisHash, true
			}
			if method == "eth_getBlockByNumber" && count == 2 {
				if changed == "evm" {
					return map[string]any{"hash": testGenesisHash, "number": "0x25", "transactions": fixture.transactionHashes}, true
				}
				if changed == "transactions" {
					return map[string]any{"hash": fixture.evmHash, "number": "0x25", "transactions": []string{fixture.transactionHashes[1], fixture.transactionHashes[0]}}, true
				}
			}
			return nil, false
		}
		snapshot, err := client.readFinalizedSnapshot(t.Context(), nil)
		if !errors.Is(err, errRpcIntegrity) || snapshot.Schema != "" || fixture.counts["eth_getBlockByHash"] != 1 || fixture.counts["state_getMetadata"] != 1 {
			t.Fatalf("changed %s published partial snapshot: %+v calls=%v err=%v", changed, snapshot, fixture.counts, err)
		}
	}
}

// Unsupported/null public methods are explicit capability failures; the
// specialized profile cannot leak the new method into ordinary RPC readers.
func TestFinalizedMappingPublicHeaderAvailabilityAndAdmission(t *testing.T) {
	for _, reply := range []any{nil, mappingFixtureRpcError{code: -32601}, mappingFixtureRpcError{code: -32602}} {
		client, fixture := newPublicFinalizedMappingFixture(t, 3, 1)
		fixture.fault = func(method string, _ []any, _ int) (any, bool) {
			if method == "debug_getRawHeader" {
				return mappingFixtureRpcError{code: -32601}, true
			}
			return reply, method == "eth_getBlockByHash"
		}
		mapping, err := client.readFinalizedMapping(t.Context(), nil)
		if !errors.Is(err, errFinalizedMappingUnavailable) || mapping.Schema != "" || fixture.counts["eth_getBlockByHash"] != 1 || fixture.counts["eth_getBlockByNumber"] != 0 {
			t.Fatalf("public capability refusal changed: schema=%s calls=%v err=%v", mapping.Schema, fixture.counts, err)
		}
	}
	client, fixture := newPublicFinalizedMappingFixture(t, 3, 1)
	var result json.RawMessage
	if err := client.call(t.Context(), "eth_getBlockByHash", []any{}, &result); err == nil || len(fixture.counts) != 0 {
		t.Fatal("public block method leaked into the ordinary profile")
	}
}

// An HTTP rejection is distinct from an unsupported JSON-RPC capability. A
// public fallback must preserve that refusal and emit no partial mapping.
func TestFinalizedMappingPublicHeaderHttpFailureStaysDistinct(t *testing.T) {
	client, fixture := newPublicFinalizedMappingFixture(t, 1, 1)
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response, err := fixture.roundTrip(request)
		if err == nil && fixture.counts["eth_getBlockByHash"] != 0 {
			response.Body.Close()
			response.StatusCode = http.StatusBadRequest
			response.Body = io.NopCloser(strings.NewReader("synthetic invalid request"))
		}
		return response, err
	})
	mapping, err := client.readFinalizedMapping(t.Context(), nil)
	if err == nil || errors.Is(err, errFinalizedMappingUnavailable) || !strings.Contains(err.Error(), "eth_getBlockByHash: HTTP 400") || mapping.Schema != "" || fixture.counts["debug_getRawHeader"] != 1 || fixture.counts["eth_getBlockByHash"] != 1 || fixture.counts["eth_getBlockByNumber"] != 0 {
		t.Fatalf("public HTTP failure became capability absence or partial evidence: schema=%s calls=%v err=%v", mapping.Schema, fixture.counts, err)
	}
}

// Cancellation at the public read boundary returns neither a reconstructed
// header nor a partial combined observation, irrespective of valid reply bytes.
func TestFinalizedSnapshotPublicHeaderCancellation(t *testing.T) {
	client, fixture := newPublicFinalizedMappingFixture(t, 3, 999)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.fault = func(method string, _ []any, _ int) (any, bool) {
		if method == "debug_getRawHeader" {
			return mappingFixtureRpcError{code: -32601}, true
		}
		if method == "eth_getBlockByHash" {
			cancel()
		}
		return nil, false
	}
	snapshot, err := client.readFinalizedSnapshot(ctx, nil)
	if !errors.Is(err, context.Canceled) || snapshot.Schema != "" || fixture.counts["eth_getBlockByHash"] != 1 || fixture.counts["eth_getBlockByNumber"] != 0 {
		t.Fatalf("canceled public read published evidence: %+v calls=%v err=%v", snapshot, fixture.counts, err)
	}
}

// Both public commands retain unchanged replayable schemas. A malformed hash
// returns exit1, missing projection fields return exit4, and neither emits JSON.
func TestFinalizedCommandsPublicHeaderRetainProofOrRefuse(t *testing.T) {
	for _, command := range []string{"finalized-mapping", "finalized-snapshot"} {
		for _, wantExit := range []int{0, 1, 4} {
			_, fixture := newPublicFinalizedMappingFixture(t, 1, 999)
			fields := fixture.renderedEvmBlock.(map[string]any)
			if wantExit == 1 {
				fields["stateRoot"] = testGenesisHash
			} else if wantExit == 4 {
				delete(fields, "nonce")
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				response, err := fixture.roundTrip(request)
				if err != nil {
					t.Error(err)
					http.Error(writer, "synthetic request failed", http.StatusBadRequest)
					return
				}
				defer response.Body.Close()
				if _, err := io.Copy(writer, response.Body); err != nil {
					t.Error(err)
				}
			}))
			var stdout, stderr bytes.Buffer
			exit := runMain(t.Context(), []string{command, "--rpc", server.URL}, &stdout, &stderr)
			server.Close()
			if exit != wantExit || wantExit != 0 && stdout.Len() != 0 {
				t.Fatalf("%s expected exit%d: exit=%d stderr=%s stdout=%s", command, wantExit, exit, stderr.String(), stdout.String())
			}
			if wantExit != 0 {
				continue
			}
			var envelope finalizedMappingEnvelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope.Mapping.EvmHeader.HeaderRlp != fixture.rawEvmHeader || envelope.Mapping.RuntimeSourceProven {
				t.Fatalf("%s lost exact recovered bytes: %+v err=%v", command, envelope, err)
			}
			if command == "finalized-snapshot" {
				var snapshot finalizedSnapshotEnvelope
				if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil || snapshot.FinalizedHash != fixture.nativeHash || snapshot.Runtime.Identity.FinalizedHash != fixture.nativeHash || snapshot.Mapping.Identity != snapshot.Runtime.Identity {
					t.Fatalf("combined public path changed native identity: %+v err=%v", snapshot, err)
				}
				sealed, err := sealFinalizedSnapshot(snapshot.finalizedSnapshot)
				if err != nil || sealed.ContentHash != snapshot.ContentHash {
					t.Fatalf("combined public evidence seal does not reproduce: %v", err)
				}
			} else if sealed, err := sealFinalizedMapping(envelope.Mapping); err != nil || sealed.ContentHash != envelope.ContentHash {
				t.Fatalf("public evidence seal does not reproduce: %v", err)
			}
		}
	}
}
