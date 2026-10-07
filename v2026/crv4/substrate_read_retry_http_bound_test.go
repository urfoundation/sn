// The configured transport ceiling includes every currently admitted field
// plus finite JSON framing. This checks wire admission, not event semantics.
package crv4

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Sixteen MiB is the pre-existing independent System.Events SCALE limit;
// encoding its maximum hex field must still fit the configured HTTP reader.
func TestSubstrateReadHttpAdmitsMaximumEventFieldWithEnvelope(t *testing.T) {
	value := "0x" + strings.Repeat("ab", 16*1024*1024)
	fixture := newSubstrateReadHttpFixture(t, func(writer http.ResponseWriter, request *http.Request, call chainContextRPCRequest, count int) {
		if call.Method != "state_getStorage" || len(call.Params) != 2 {
			t.Error("maximum-field control used the wrong read boundary")
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": value})
	})
	var observed string
	err := fixture.client.CallContext(t.Context(), &observed, "state_getStorage", "0x0102", types.Hash{7}.Hex())
	if err != nil || observed != value || len(fixture.snapshot()) != 1 {
		t.Fatalf("native HTTP rejected an admitted maximum event field plus JSON envelope: bytes=%d calls=%d error=%v", len(observed), len(fixture.snapshot()), err)
	}
}

// The larger ceiling stays finite; a declared 34 MiB response cannot consume
// unbounded bytes or be mislabeled as a transient interrupted body.
func TestSubstrateReadHttpEnvelopeAllowanceStaysFinite(t *testing.T) {
	fixture := newSubstrateReadHttpFixture(t, func(writer http.ResponseWriter, request *http.Request, call chainContextRPCRequest, count int) {
		writer.Header().Set("Content-Length", "35651584")
		_, _ = writer.Write(substrateReadHttpReply(t, call))
	})
	var observed string
	err := fixture.client.CallContext(t.Context(), &observed, "state_getStorage", "0x0102", types.Hash{7}.Hex())
	if err == nil || RetryableSubstrateReadTransportError(err) || len(fixture.snapshot()) != 1 || observed != "" {
		t.Fatalf("native HTTP accepted or retried an oversized envelope: calls=%d error=%v", len(fixture.snapshot()), err)
	}
}

// Origin detection must survive an outer URL and mixed error tree without
// promoting a mixed body-close EOF and integrity cause into retry authority.
func TestSubstrateReadHttpOriginPresenceKeepsHardVerdict(t *testing.T) {
	hard := &url.Error{Op: "Post", URL: "http://synthetic.invalid", Err: errors.Join(&substrateReadHttpTransportError{cause: io.ErrUnexpectedEOF}, &substrateReadHttpCloseError{cause: errors.Join(io.EOF, errors.New("physical close ownership defect"))})}
	if !HasSubstrateReadTransportCause(hard) || RetryableSubstrateReadTransportError(hard) {
		t.Fatal("native physical origin was lost or its hard close was promoted")
	}
	if HasSubstrateReadTransportCause(io.EOF) || HasSubstrateReadTransportCause(errors.New("native HTTP read interrupted")) {
		t.Fatal("unowned diagnostic acquired native origin")
	}
}
