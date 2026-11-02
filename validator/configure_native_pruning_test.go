// A real local native WebSocket transport distinguishes a readable current
// runtime from pruned boundary state before any configuration is signed.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
)

// The healthy head keeps its actual synthetic metadata while the older drained
// boundary returns the node's typed pruning error. Neither a cached current
// artifact nor a new current-state read may replace that historical state.
func TestConfigureNativePrunedBoundaryCannotCreateSignedConfig(t *testing.T) {
	profile, options, _ := configureTestInputs(t)
	nativeProfile, fixture, _ := configureNativeTestInputs(t)
	profile.Config.RuntimeMetadataHash = nativeProfile.Config.RuntimeMetadataHash
	profile.Proposal.Runtime.MetadataHash = nativeProfile.Proposal.Runtime.MetadataHash
	boundaryHash, headHash := mainnetRuntimeTestBlock(120).Hex(), mainnetRuntimeTestBlock(126).Hex()
	var pruned atomic.Bool
	var denied, currentAfterDenial atomic.Uint64
	closed := make(chan struct{}, 3)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(output http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(output, request, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = connection.Close(); closed <- struct{}{} }()
		connection.SetReadLimit(64 * 1024)
		for {
			var call struct {
				Id     json.RawMessage   `json:"id"`
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			if err := connection.ReadJSON(&call); err != nil {
				return
			}
			args := make([]any, 0, len(call.Params))
			for index, raw := range call.Params {
				if call.Method == "chain_getBlockHash" && index == 0 {
					value, err := strconv.ParseUint(string(raw), 10, 64)
					if err != nil {
						t.Error(err)
						return
					}
					args = append(args, value)
				} else {
					var value any
					if err := json.Unmarshal(raw, &value); err != nil {
						t.Error(err)
						return
					}
					args = append(args, value)
				}
			}
			// Connection initialization asks for latest metadata/version; the
			// subsequent setup read always supplies its explicit selected hash.
			if len(args) == 0 && (call.Method == "state_getMetadata" || call.Method == "state_getRuntimeVersion") {
				args = append(args, headHash)
			}
			response := map[string]any{"jsonrpc": "2.0", "id": call.Id}
			if call.Method == "state_getStorage" && len(args) == 2 && args[1] == boundaryHash && pruned.Load() {
				denied.Add(1)
				response["error"] = map[string]any{"code": 4003, "message": "State already discarded"}
			} else {
				if call.Method == "state_getStorage" && len(args) == 2 && args[1] == headHash && denied.Load() != 0 {
					currentAfterDenial.Add(1)
				}
				var result json.RawMessage
				if err := fixture.rpc.client.CallContext(request.Context(), &result, call.Method, args...); err != nil {
					t.Errorf("unexpected synthetic native RPC %s: %v", call.Method, err)
					return
				}
				response["result"] = result
			}
			if err := connection.WriteJSON(response); err != nil {
				t.Error(err)
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	options.Substrate = "ws" + strings.TrimPrefix(server.URL, "http")
	if snapshot, err := readReleaseConfigureNativeSnapshot(t.Context(), profile, options.Substrate); err != nil || snapshot.Boundary.Block != 120 {
		t.Fatal("healthy historical native control failed", snapshot, err)
	}
	<-closed
	pruned.Store(true)
	var observed error
	readSnapshot := func(ctx context.Context, profile *releaseMainnetProfile, endpoint string) (releaseConfigureNativeSnapshot, error) {
		snapshot, err := readReleaseConfigureNativeSnapshot(ctx, profile, endpoint)
		observed = err
		return snapshot, err
	}
	var output, errorOutput bytes.Buffer
	if code := runConfigureCommandWithProfile(t.Context(), configureTestArgs(options), &output, &errorOutput, profile, readSnapshot); code != 1 {
		t.Fatalf("pruned native boundary exit=%d, expected refusal: %s", code, errorOutput.String())
	}
	<-closed
	var rpcError interface{ ErrorCode() int }
	if !errors.As(observed, &rpcError) || rpcError.ErrorCode() != 4003 || denied.Load() != 1 || currentAfterDenial.Load() != 0 {
		t.Fatalf("pruned state was substituted or lost its native cause: error=%v denied=%d current_fallback=%d", observed, denied.Load(), currentAfterDenial.Load())
	}
	for _, path := range []string{options.OutputPath, options.OutputPath + ".treasury-approval.json", options.StateDir, options.EvidenceDir} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("pruned boundary created signed configuration or mutable state", path, err)
		}
	}
	// Restoring historical service is sufficient; no identity, approval policy
	// or state is reset to recover from the refused setup attempt.
	pruned.Store(false)
	denied.Store(0)
	if code := runConfigureCommandWithProfile(t.Context(), configureTestArgs(options), &output, &errorOutput, profile, readSnapshot); code != 0 {
		t.Fatalf("restored historical native service did not configure: exit=%d %s", code, errorOutput.String())
	}
	<-closed
	if _, err := LoadReleaseProductionConfigPreActivation(options.OutputPath); err != nil {
		t.Fatal("restored history produced an invalid signed config", err)
	}
}
