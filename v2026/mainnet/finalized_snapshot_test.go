// Combined snapshot tests force changes between component reads while retaining
// exact old block evidence. They never contact a chain or import signing state.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Advancing the latest finalized selection after artifacts are read must not
// replace the old hash passed to mapping or discard still-canonical old bytes.
func TestFinalizedSnapshotPinsHeadAcrossAdvancement(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	f.head = 100
	client, fixture := f.client, f.mapping
	advanced := false
	f.after = func(method string, _ []any) {
		if method == "state_getMetadata" {
			advanced = true
			f.head = 150
		}
	}
	snapshot, err := client.readFinalizedSnapshot(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !advanced || f.counts["chain_getFinalizedHead"] != 5 || snapshot.FinalizedHash != fixture.nativeHash || snapshot.FinalizedNumber != 100 ||
		snapshot.Runtime.Identity != snapshot.Mapping.Identity || snapshot.Mapping.Identity.FinalizedHash != fixture.nativeHash ||
		snapshot.Runtime.CodeHex != "0x"+hex.EncodeToString(fixture.runtimeCode) || snapshot.Runtime.MetadataHex != "0x"+hex.EncodeToString(fixture.runtimeMetadata) ||
		snapshot.Mapping.EvmHeader.HeaderRlp != fixture.rawEvmHeader || snapshot.Mapping.EvmHeader.Number != 37 {
		t.Fatalf("advancement replaced retained exact-hash evidence: %+v calls=%v", snapshot, fixture.counts)
	}
}

// A proxy can retain the same genesis/name while changing its canonical fork.
// Force that change only after runtime and raw EVM bytes have been read.
func TestFinalizedSnapshotRejectsSameGenesisProxyRetarget(t *testing.T) {
	client, fixture := newFinalizedMappingFixture(t, 1, 1)
	retargeted := false
	fixture.fault = func(method string, params []any, _ int) (any, bool) {
		if method == "debug_getRawHeader" {
			retargeted = true
		}
		if retargeted && method == "chain_getBlockHash" && params[0] == float64(100) {
			return testFinalizedHash, true
		}
		return nil, false
	}
	snapshot, err := client.readFinalizedSnapshot(context.Background(), nil)
	if !errors.Is(err, errRpcIntegrity) || snapshot.Schema != "" || !retargeted || fixture.counts["state_getStorage"] != 1 || fixture.counts["state_getMetadata"] != 1 {
		t.Fatalf("same-genesis retarget published combined evidence: %+v calls=%v err=%v", snapshot, fixture.counts, err)
	}
}

// The last canonical EVM check must still occur after both artifact and mapping
// collection; a late same-network index change cannot be sealed as completion.
func TestFinalizedSnapshotRejectsFinalCanonicalEvmChange(t *testing.T) {
	client, fixture := newFinalizedMappingFixture(t, 1, 0)
	fixture.fault = func(method string, _ []any, count int) (any, bool) {
		if method == "eth_getBlockByNumber" && count == 2 {
			return map[string]any{"hash": testFinalizedHash, "number": "0x25", "transactions": []string{}}, true
		}
		return nil, false
	}
	snapshot, err := client.readFinalizedSnapshot(context.Background(), nil)
	if !errors.Is(err, errRpcIntegrity) || snapshot.Schema != "" || fixture.counts["state_getMetadata"] != 1 || fixture.counts["eth_getBlockByNumber"] != 2 {
		t.Fatalf("late canonical EVM change accepted: schema=%q calls=%v err=%v", snapshot.Schema, fixture.counts, err)
	}
}

// The helpers accept one live authenticated identity but do not turn imported
// identity-v1 JSON, missing fields or a different RPC route into authority.
func TestFinalizedSnapshotPinnedReadersRejectForeignOrImportedIdentity(t *testing.T) {
	client, fixture := newFinalizedMappingFixture(t, 1, 0)
	identity, err := client.readIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	var imported chainIdentity
	if err := json.Unmarshal(raw, &imported); err != nil {
		t.Fatal(err)
	}
	foreign := identity
	foreign.RpcUrl = "http://other-rpc.example"
	for _, invalid := range []chainIdentity{imported, foreign, {}} {
		if _, err := client.readRuntimeSnapshotAtIdentity(context.Background(), invalid); !errors.Is(err, errRpcIntegrity) {
			t.Errorf("invalid runtime identity admitted: %v", err)
		}
		if _, err := client.readFinalizedMappingAtIdentity(context.Background(), invalid); !errors.Is(err, errRpcIntegrity) {
			t.Errorf("invalid mapping identity admitted: %v", err)
		}
	}
	if fixture.counts["chain_getFinalizedHead"] != 2 || fixture.counts["state_getStorage"] != 0 || fixture.counts["debug_getRawHeader"] != 0 {
		t.Fatalf("invalid pinned identity caused RPC reads: %v", fixture.counts)
	}
}

// Sealing explicitly refuses a post-hoc join when only genesis, route or spec
// numbers happen to agree. Top-level scope and complete runtime tuples matter.
func TestFinalizedSnapshotRefusesMismatchedComponentSeal(t *testing.T) {
	client, _ := newFinalizedMappingFixture(t, 1, 0)
	snapshot, err := client.readFinalizedSnapshot(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*finalizedSnapshot){
		func(value *finalizedSnapshot) { value.Mapping.Identity.FinalizedHash = testFinalizedHash },
		func(value *finalizedSnapshot) { value.Mapping.RuntimeVersion.StateVersion = 0 },
		func(value *finalizedSnapshot) { value.FinalizedNumber++ },
		func(value *finalizedSnapshot) { value.Runtime.Identity.ObservedAt = "different-observation" },
	}
	for index, mutate := range mutations {
		changed := snapshot
		mutate(&changed)
		if _, err := sealFinalizedSnapshot(changed); !errors.Is(err, errRpcIntegrity) {
			t.Errorf("mismatched component %d sealed: %v", index, err)
		}
	}
}

// The full operation owns one total deadline; nested helpers cannot restart
// its budget as each new evidence component begins.
func TestFinalizedSnapshotSharesOneDeadline(t *testing.T) {
	client, _ := newFinalizedMappingFixture(t, 1, 0)
	transport := client.httpClient.Transport
	var deadlines []time.Time
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		if !ok {
			t.Fatal("combined RPC read has no deadline")
		}
		deadlines = append(deadlines, deadline)
		return transport.RoundTrip(request)
	})
	if _, err := client.readFinalizedSnapshot(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	for _, deadline := range deadlines[1:] {
		if !deadline.Equal(deadlines[0]) {
			t.Fatalf("component restarted the total budget: %v", deadlines)
		}
	}
}

// Both raw evidence types and the shared hash survive JSON roundtrip. Existing
// standalone command schemas remain covered by their unchanged command tests.
func TestFinalizedSnapshotCommandKeepsSharedHashAndCompleteEvidence(t *testing.T) {
	_, fixture := newFinalizedMappingFixture(t, 1, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		response, err := fixture.roundTrip(request)
		if err != nil {
			t.Error(err)
			http.Error(writer, "synthetic request failed", http.StatusBadRequest)
			return
		}
		defer response.Body.Close()
		writer.WriteHeader(response.StatusCode)
		if _, err := io.Copy(writer, response.Body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	var stdout, stderr bytes.Buffer
	code := runMain(context.Background(), []string{"finalized-snapshot", "--rpc", server.URL}, &stdout, &stderr)
	var envelope finalizedSnapshotEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || code != 0 || envelope.FinalizedHash != fixture.nativeHash || envelope.Mapping.EvmHeader.HeaderRlp != fixture.rawEvmHeader || envelope.Runtime.CodeHex != "0x"+hex.EncodeToString(fixture.runtimeCode) {
		t.Fatalf("combined command did not retain both components: exit=%d err=%v stderr=%s", code, err, stderr.String())
	}
	resealed, err := sealFinalizedSnapshot(envelope.finalizedSnapshot)
	if err != nil || resealed.ContentHash != envelope.ContentHash {
		t.Fatalf("combined content digest did not reproduce: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &top); err != nil || len(top["finalized_hash"]) == 0 || len(top["runtime"]) == 0 || len(top["mapping"]) == 0 {
		t.Fatal("combined scope is missing from the JSON top level")
	}
}
