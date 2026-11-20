// Failed archive bytes retain private diagnostic custody without becoming
// acknowledged trie nodes or erasing the original proof refusal.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/server/v2026/strecovery"
)

// A complete concrete wrong identity stays hard even with a service status.
// The local handler verifies the immutable request before emitting the fault.
func nativeFeedRpcFailureTest(t *testing.T, child bool) (context.Context, *nativeExecutionPolicy, *nativeProducerFiles, *historicalNativeFeed, historicalNativeFeedRequest, []byte) {
	t.Helper()
	ctx, policy, files := nativeProducerTestFiles(t, nil)
	parent := "0x" + strings.Repeat("12", 32)
	raw := []byte(`{"jsonrpc":"2.0","id":2,"result":{"at":"` + parent + `","proof":["0x01"]}}`)
	method := "state_getReadProof"
	if child {
		method = "state_getChildReadProof"
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			Id     int               `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		count := 2
		if child {
			count = 3
		}
		if call.Id != 1 || call.Method != method || len(call.Params) != count || string(call.Params[count-1]) != `"`+parent+`"` {
			t.Error("diagnostic fixture lost its original proof route", call)
			return
		}
		writer.WriteHeader(http.StatusServiceUnavailable)
		if _, err := writer.Write(raw); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	client, err := newRpcClient(server.URL, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.httpClient.CloseIdleConnections)
	feed := &historicalNativeFeed{files: files, client: client, requestSha256: monitorReadDigest([]byte("input")), parentHash: parent, parentStateRoot: "0x" + strings.Repeat("34", 32)}
	request := historicalNativeFeedRequest{Schema: historicalNativeFeedSchema, Id: 1, RequestSha256: feed.requestSha256, ParentHash: parent, ParentStateRoot: feed.parentStateRoot, Operation: "storage", ChildStorageKey: "0x", PrefixHex: "0x", MissingHash: "0x" + strings.Repeat("56", 32)}
	if child {
		request.Operation, request.ChildStorageKey = "child-storage", nativeExecutionTestHex([]byte(":child_storage:default:original"))
	}
	return ctx, policy, files, feed, request, raw
}

func TestNativeProducerFeedRetainsFailedRpcEnvelopeWithoutAcknowledging(t *testing.T) {
	for _, child := range []bool{false, true} {
		ctx, _, files, feed, request, reply := nativeFeedRpcFailureTest(t, child)
		var output bytes.Buffer
		err := feed.serve(ctx, bytes.NewReader(nativeFeedTestFrame(t, request)), &output)
		var failure *strecovery.NativeExecutionProofReadFailure
		if !errors.Is(err, errRpcIntegrity) || !errors.Is(err, strecovery.ErrNativeExecutionProofConflict) || !errors.As(err, &failure) || output.Len() != 0 {
			t.Fatal("failed identity became acknowledged proof or lost attribution", child, err, output.Len())
		}
		if failure.RequestId != 1 || failure.Status != 503 || !failure.BodyReadComplete || failure.ReplyTruncated || !bytes.Equal(failure.ReplyPrefix, reply) {
			t.Fatal("retained failure differs from original observed reply", failure)
		}
		raw, err := json.Marshal(failure)
		if err != nil {
			t.Fatal(err)
		}
		relative := "proof-rpc/" + strings.TrimPrefix(monitorReadDigest(raw), "sha256:") + ".json"
		retained, err := files.read(relative, 128*1024)
		if err != nil || !bytes.Equal(retained, raw) {
			t.Fatal("failed read did not retain exact diagnostic bytes", err)
		}
		info, err := os.Stat(filepath.Join(files.path, relative))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatal("RPC failure evidence is not a private original file", err)
		}
		if _, err := os.Stat(filepath.Join(files.path, "nodes", strings.TrimPrefix(request.MissingHash, "0x"))); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("diagnostic candidate was published as a requested trie node", err)
		}
		if err := files.admitMargin(1); err != nil {
			t.Fatal("diagnostic escaped counted private evidence admission", err)
		}
	}
}

func TestNativeProducerFeedDiagnosticCapacityKeepsOriginalRefusal(t *testing.T) {
	ctx, _, files, feed, request, _ := nativeFeedRpcFailureTest(t, false)
	files.policy.MaximumBytes = nativeProducerBoundaryReserve - 1
	var output bytes.Buffer
	err := feed.serve(ctx, bytes.NewReader(nativeFeedTestFrame(t, request)), &output)
	var failure *strecovery.NativeExecutionProofReadFailure
	if !errors.Is(err, errRpcIntegrity) || !errors.Is(err, strecovery.ErrNativeExecutionProofConflict) || !errors.Is(err, errMonitorEconomicCapacity) || !errors.As(err, &failure) || output.Len() != 0 {
		t.Fatal("diagnostic capacity failure hid original evidence or acknowledged proof", err, output.Len())
	}
	if _, err := os.Stat(filepath.Join(files.path, "proof-rpc")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("diagnostic wrote bytes before shared capacity admission", err)
	}
	if _, err := os.Stat(filepath.Join(files.path, "nodes", strings.TrimPrefix(request.MissingHash, "0x"))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused diagnostic became a proof node", err)
	}
}
