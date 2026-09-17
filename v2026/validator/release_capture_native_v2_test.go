//go:build linux || darwin

// Real HTTP transport and real native storage facades exercise capture's
// result, cancellation and no-write boundary without replacing verdicts.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	gsrpcstate "github.com/centrifuge/go-substrate-rpc-client/v4/rpc/state"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The production adapter requires the actual transport endpoint identity.
type releaseNativeCaptureHttpTestClientV2 struct {
	*gsrpcgeth.Client
	endpoint string
}

// Implements only the transport's existing endpoint contract.
func (self *releaseNativeCaptureHttpTestClientV2) URL() string { return self.endpoint }

// The server returns an original JSON result; the real RPC client decodes it.
func newReleaseNativeCaptureHttpV2TestClient(t *testing.T, result string, calls *atomic.Uint64) *releaseNativeCaptureHttpTestClientV2 {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		var value struct {
			Id     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&value); err != nil {
			t.Error(err)
			http.Error(writer, "bad request", 400)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, "{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}", value.Id, result)
	}))
	t.Cleanup(server.Close)
	client, err := gsrpcgeth.DialContext(t.Context(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return &releaseNativeCaptureHttpTestClientV2{Client: client, endpoint: server.URL}
}

// JSON object spacing and absent storage remain original response bytes.
func TestReleaseNativeCaptureV2RetainsExactHttpResult(t *testing.T) {
	var calls atomic.Uint64
	original := `{ "number" : "0x01", "extra": [1, 2] }`
	transport := newReleaseNativeCaptureHttpV2TestClient(t, original, &calls)
	var retained []ReleaseEvidenceV2NativeRead
	client := &releaseNativeCaptureClientV2{Client: transport, ctx: t.Context(), maximum: 4096, retain: func(_ context.Context, read ReleaseEvidenceV2NativeRead) error {
		retained = append(retained, read)
		return nil
	}}
	var value map[string]any
	if err := client.CallContext(t.Context(), &value, "chain_getHeader", "0x0102"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len(retained) != 1 || string(retained[0].Result) != original || string(retained[0].Parameters) != `["0x0102"]` || value["number"] != "0x01" {
		t.Fatalf("actual RPC result was normalized or not retained: %+v / %+v", retained, value)
	}
}

// Archive failure is causal: the genuine successful HTTP response cannot
// escape into the requested destination after its durable retention failed.
func TestReleaseNativeCaptureV2ArchiveFailurePreventsResult(t *testing.T) {
	var calls atomic.Uint64
	transport := newReleaseNativeCaptureHttpV2TestClient(t, `"0xab"`, &calls)
	want := errors.New("actual archive refusal")
	client := &releaseNativeCaptureClientV2{Client: transport, ctx: t.Context(), maximum: 4096, retain: func(context.Context, ReleaseEvidenceV2NativeRead) error { return want }}
	value := "unchanged"
	if err := client.CallContext(t.Context(), &value, "state_getStorage", "0x00", "0x01"); !errors.Is(err, want) || value != "unchanged" || calls.Load() != 1 {
		t.Fatalf("uncaptured result escaped: value=%q calls=%d err=%v", value, calls.Load(), err)
	}
}

// A forbidden method never reaches even a real permissive RPC endpoint.
func TestReleaseNativeCaptureV2RefusesWritesBeforeHttp(t *testing.T) {
	var calls atomic.Uint64
	transport := newReleaseNativeCaptureHttpV2TestClient(t, `"0xab"`, &calls)
	client := &releaseNativeCaptureClientV2{Client: transport, ctx: t.Context(), maximum: 4096, retain: func(context.Context, ReleaseEvidenceV2NativeRead) error {
		t.Error("write result was retained")
		return nil
	}}
	var value string
	if err := client.CallContext(t.Context(), &value, "author_submitExtrinsic", "0x00"); err == nil || calls.Load() != 0 {
		t.Fatalf("native capture submitted a write: calls=%d err=%v", calls.Load(), err)
	}
}

// Allocation admission is inside the real RPC destination decoder.
func TestReleaseNativeCaptureV2RejectsOversizedHttpResult(t *testing.T) {
	var calls atomic.Uint64
	transport := newReleaseNativeCaptureHttpV2TestClient(t, `"`+strings.Repeat("a", 256)+`"`, &calls)
	client := &releaseNativeCaptureClientV2{Client: transport, ctx: t.Context(), maximum: 32, retain: func(context.Context, ReleaseEvidenceV2NativeRead) error {
		t.Error("oversized result was retained")
		return nil
	}}
	var value string
	if err := client.CallContext(t.Context(), &value, "state_getStorage"); err == nil || value != "" || calls.Load() != 1 {
		t.Fatalf("oversized result escaped: bytes=%d calls=%d err=%v", len(value), calls.Load(), err)
	}
}

// The actual convenience State facade must use capture, not the inherited
// native connection's original RPC facade pointer.
func TestReleaseNativeCaptureV2CapturesConvenienceStorageReader(t *testing.T) {
	var calls atomic.Uint64
	transport := newReleaseNativeCaptureHttpV2TestClient(t, `"0xab"`, &calls)
	var retained []ReleaseEvidenceV2NativeRead
	native, err := releaseNativeCaptureChainV2(t.Context(), &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: transport}}, 4096, func(_ context.Context, read ReleaseEvidenceV2NativeRead) error {
		retained = append(retained, read)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := native.API.RPC.State.GetStorageRaw(types.StorageKey{1, 2}, types.Hash{3})
	if err != nil || value == nil || !bytes.Equal(*value, []byte{0xab}) || len(retained) != 1 || retained[0].Method != "state_getStorage" || calls.Load() != 1 {
		t.Fatalf("real state facade bypassed capture: %v %+v err=%v", value, retained, err)
	}
}

// Cancellation is driven by the server's actual entered-request barrier and
// joins the real RPC call; no sleep or negative timeout chooses the ordering.
func TestReleaseNativeCaptureV2CancellationJoinsActualHttp(t *testing.T) {
	entered, serverDone := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		close(entered)
		<-request.Context().Done()
		close(serverDone)
	}))
	t.Cleanup(server.Close)
	transport, err := gsrpcgeth.DialContext(t.Context(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(transport.Close)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := &releaseNativeCaptureClientV2{Client: &releaseNativeCaptureHttpTestClientV2{Client: transport, endpoint: server.URL}, ctx: ctx, maximum: 4096, retain: func(context.Context, ReleaseEvidenceV2NativeRead) error {
		t.Error("canceled response was retained")
		return nil
	}}
	done := make(chan error, 1)
	go func() {
		var value string
		done <- client.CallContext(t.Context(), &value, "state_getStorage", "0x00", "0x01")
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("actual HTTP cancellation was lost: %v", err)
	}
	<-serverDone
}

// The existing native metadata/stake fixture still passes through its real
// reader; capture retains storage and the calculated stake runtime API bytes.
func TestReleaseNativeCaptureV2PreservesActualNativeStakeReads(t *testing.T) {
	fixture := newReleaseNativeValidatorTestFixture(t)
	transport := fixture.chain.API.Client
	// The existing fixture has two typed destinations. Serialize its actual
	// responses just as a transport does; no native verdict is substituted.
	fixture.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		fixture.ctx = ctx
		if method == "chain_getHeader" {
			var header types.Header
			if err := transport.CallContext(ctx, &header, method, args...); err != nil {
				return err
			}
			encoded, err := json.Marshal(header)
			if err != nil {
				return err
			}
			return json.Unmarshal(encoded, result)
		}
		var raw json.RawMessage
		if err := transport.CallContext(ctx, &raw, method, args...); err != nil {
			return err
		}
		return json.Unmarshal(raw, result)
	}}
	var retained []ReleaseEvidenceV2NativeRead
	owned, err := releaseNativeCaptureChainV2(t.Context(), fixture.chain, 1024*1024, func(_ context.Context, read ReleaseEvidenceV2NativeRead) error {
		retained = append(retained, read)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.chain = owned
	fixture.ctx = t.Context()
	observed, err := fixture.read()
	if err != nil || !observed.MeetsNonSelfStakeAndPermit() {
		t.Fatalf("actual native reader failed through capture: %+v %v", observed, err)
	}
	methods := map[string]bool{}
	for _, read := range retained {
		methods[read.Method] = true
		if !json.Valid(read.Result) || !json.Valid(read.Parameters) {
			t.Fatal("native original result/parameters are not retained")
		}
	}
	for _, method := range []string{"state_getMetadata", "state_getStorage", "state_getStorageHash", "state_call", "chain_getBlockHash"} {
		if !methods[method] {
			t.Errorf("actual native source omitted %s", method)
		}
	}
}

// Typed header readers use the transport's Json destination contract too.
// The original response is retained before the normal header decoder sees it.
func TestReleaseNativeCaptureV2DecodesActualHttpHeader(t *testing.T) {
	var calls atomic.Uint64
	parent, state, extrinsics := types.Hash{1}, types.Hash{2}, types.Hash{3}
	original := fmt.Sprintf(`{ "parentHash" : "%s", "number" : "0x7b", "stateRoot" : "%s", "extrinsicsRoot" : "%s", "digest" : { "logs" : [] } }`, parent.Hex(), state.Hex(), extrinsics.Hex())
	transport := newReleaseNativeCaptureHttpV2TestClient(t, original, &calls)
	var retained []ReleaseEvidenceV2NativeRead
	native, err := releaseNativeCaptureChainV2(t.Context(), &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: transport}}, 4096, func(_ context.Context, read ReleaseEvidenceV2NativeRead) error {
		retained = append(retained, read)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	header, err := native.API.RPC.Chain.GetHeader(types.Hash{9})
	if err != nil || header == nil || header.Number != 123 || header.ParentHash != parent || header.StateRoot != state || header.ExtrinsicsRoot != extrinsics {
		t.Fatalf("actual typed header decoder failed through capture: %+v %v", header, err)
	}
	if calls.Load() != 1 || len(retained) != 1 || retained[0].Method != "chain_getHeader" || string(retained[0].Result) != original {
		t.Fatalf("typed header capture lost the original Http bytes: calls=%d retained=%+v", calls.Load(), retained)
	}
}

// Absent storage remains the original null, not a fabricated empty hex value,
// even though the convenience facade returns an empty slice in either case.
func TestReleaseNativeCaptureV2RetainsActualHttpNullStorage(t *testing.T) {
	var calls atomic.Uint64
	transport := newReleaseNativeCaptureHttpV2TestClient(t, "null", &calls)
	baseline, err := gsrpcstate.NewState(transport).GetStorageRaw(types.StorageKey{1, 2}, types.Hash{3})
	if err != nil || baseline == nil || len(*baseline) != 0 || calls.Load() != 1 {
		t.Fatalf("unwrapped storage facade did not expose its actual empty-value contract: value=%v error=%v", baseline, err)
	}
	var retained []ReleaseEvidenceV2NativeRead
	native, err := releaseNativeCaptureChainV2(t.Context(), &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: transport}}, 4096, func(_ context.Context, read ReleaseEvidenceV2NativeRead) error {
		retained = append(retained, read)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := native.API.RPC.State.GetStorageRaw(types.StorageKey{1, 2}, types.Hash{3})
	if err != nil || value == nil || !bytes.Equal(*value, *baseline) || calls.Load() != 2 || len(retained) != 1 || string(retained[0].Result) != "null" {
		t.Fatalf("actual absent storage was changed by capture: value=%v calls=%d retained=%+v error=%v", value, calls.Load(), retained, err)
	}
}

// The upstream facade collapses null and empty storage to empty bytes, but
// retained original results still distinguish both from a present zero byte.
func TestReleaseNativeCaptureV2StorageFacadeKeepsDistinctOriginalResults(t *testing.T) {
	for _, input := range []struct {
		original string
		expected []byte
	}{
		{original: "null", expected: []byte{}},
		{original: `"0x"`, expected: []byte{}},
		{original: `"0x00"`, expected: []byte{0}},
	} {
		var calls atomic.Uint64
		transport := newReleaseNativeCaptureHttpV2TestClient(t, input.original, &calls)
		baseline, err := gsrpcstate.NewState(transport).GetStorageRaw(types.StorageKey{1}, types.Hash{2})
		if err != nil || baseline == nil || !bytes.Equal(*baseline, input.expected) {
			t.Fatalf("unwrapped %s storage differs: value=%v error=%v", input.original, baseline, err)
		}
		var retained []ReleaseEvidenceV2NativeRead
		native, err := releaseNativeCaptureChainV2(t.Context(), &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: transport}}, 4096, func(_ context.Context, read ReleaseEvidenceV2NativeRead) error {
			retained = append(retained, read)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		value, err := native.API.RPC.State.GetStorageRaw(types.StorageKey{1}, types.Hash{2})
		if err != nil || value == nil || !bytes.Equal(*value, *baseline) || calls.Load() != 2 || len(retained) != 1 || string(retained[0].Result) != input.original {
			t.Fatalf("captured %s changed its actual facade or original bytes: value=%v retained=%+v error=%v", input.original, value, retained, err)
		}
	}
}

// Explicit null consumes four response bytes, even when the transport clears
// a nullable destination without invoking its custom decoder.
func TestReleaseNativeCaptureV2NullResultHonorsExactByteBound(t *testing.T) {
	var calls atomic.Uint64
	transport := newReleaseNativeCaptureHttpV2TestClient(t, "null", &calls)
	var retained []ReleaseEvidenceV2NativeRead
	client := &releaseNativeCaptureClientV2{Client: transport, ctx: t.Context(), maximum: 4, retain: func(_ context.Context, read ReleaseEvidenceV2NativeRead) error {
		retained = append(retained, read)
		return nil
	}}
	value := new(string)
	*value = "unchanged"
	if err := client.CallContext(t.Context(), &value, "state_getStorage", []any{}...); err != nil || value != nil || calls.Load() != 1 {
		t.Fatalf("exact-bound null did not reach the real destination: value=%v calls=%d error=%v", value, calls.Load(), err)
	}
	if len(retained) != 1 || string(retained[0].Result) != "null" || string(retained[0].Parameters) != "[]" {
		t.Fatalf("exact-bound null lost its original control bytes: %+v", retained)
	}
}

// The two-byte parameters pass admission; the actual four-byte Http result
// must then fail its three-byte response bound before archive or destination.
func TestReleaseNativeCaptureV2RejectsNullResultAboveByteBound(t *testing.T) {
	var calls atomic.Uint64
	transport := newReleaseNativeCaptureHttpV2TestClient(t, "null", &calls)
	client := &releaseNativeCaptureClientV2{Client: transport, ctx: t.Context(), maximum: 3, retain: func(context.Context, ReleaseEvidenceV2NativeRead) error {
		t.Error("oversized null result was retained")
		return nil
	}}
	value := new(string)
	*value = "unchanged"
	if err := client.CallContext(t.Context(), &value, "state_getStorage", []any{}...); err == nil || value == nil || *value != "unchanged" || calls.Load() != 1 || !strings.Contains(err.Error(), "result exceeds its finite byte bound") {
		t.Fatalf("oversized null result escaped: value=%v calls=%d error=%v", value, calls.Load(), err)
	}
}

// A faulty transport which returns success without writing a result does not
// acquire the authority of an explicit, observed null response.
func TestReleaseNativeCaptureV2OmittedTransportResultIsNotNull(t *testing.T) {
	var calls atomic.Uint64
	transport := &validatorRuntimeIdentityTestClient{callContext: func(context.Context, any, string, ...any) error {
		calls.Add(1)
		return nil
	}}
	client := &releaseNativeCaptureClientV2{Client: transport, ctx: t.Context(), maximum: 4, retain: func(context.Context, ReleaseEvidenceV2NativeRead) error {
		t.Error("missing transport result was retained as a fact")
		return nil
	}}
	value := new(string)
	*value = "unchanged"
	if err := client.CallContext(t.Context(), &value, "state_getStorage", []any{}...); err == nil || !strings.Contains(err.Error(), "transport omitted its result") || value == nil || *value != "unchanged" || calls.Load() != 1 {
		t.Fatalf("missing transport result became an observed null: value=%v calls=%d error=%v", value, calls.Load(), err)
	}
}

// The actual upstream decoder also distinguishes an absent result field from
// a present null field; capture must preserve its transport error unchanged.
func TestReleaseNativeCaptureV2AbsentHttpResultIsNotNull(t *testing.T) {
	var calls atomic.Uint64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		var input struct {
			Id json.RawMessage `json:"id"`
		}
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Error(err)
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, "{\"jsonrpc\":\"2.0\",\"id\":%s}", input.Id)
	}))
	t.Cleanup(server.Close)
	transport, err := gsrpcgeth.DialContext(t.Context(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(transport.Close)
	client := &releaseNativeCaptureClientV2{Client: &releaseNativeCaptureHttpTestClientV2{Client: transport, endpoint: server.URL}, ctx: t.Context(), maximum: 4096, retain: func(context.Context, ReleaseEvidenceV2NativeRead) error {
		t.Error("absent Http result was retained as a fact")
		return nil
	}}
	value := new(string)
	*value = "unchanged"
	if err := client.CallContext(t.Context(), &value, "state_getStorage"); !errors.Is(err, gsrpcgeth.ErrNoResult) || value == nil || *value != "unchanged" || calls.Load() != 1 {
		t.Fatalf("absent Http result became an observed null: value=%v calls=%d error=%v", value, calls.Load(), err)
	}
}

// Null is still a fact requiring successful retention before it can clear the
// caller's destination; it does not get a special archive-failure bypass.
func TestReleaseNativeCaptureV2NullArchiveFailurePreventsResult(t *testing.T) {
	var calls atomic.Uint64
	transport := newReleaseNativeCaptureHttpV2TestClient(t, "null", &calls)
	want := errors.New("actual null archive refusal")
	var retained []ReleaseEvidenceV2NativeRead
	client := &releaseNativeCaptureClientV2{Client: transport, ctx: t.Context(), maximum: 4096, retain: func(_ context.Context, read ReleaseEvidenceV2NativeRead) error {
		retained = append(retained, read)
		return want
	}}
	value := new(string)
	*value = "unchanged"
	if err := client.CallContext(t.Context(), &value, "state_getStorage"); !errors.Is(err, want) || value == nil || *value != "unchanged" || calls.Load() != 1 {
		t.Fatalf("uncaptured null cleared the destination: value=%v calls=%d error=%v", value, calls.Load(), err)
	}
	if len(retained) != 1 || string(retained[0].Result) != "null" {
		t.Fatalf("archive did not receive the explicit original null: %+v", retained)
	}
}
