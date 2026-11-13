// A released HTTP response may lose its connection without losing read
// authority. The configured owner retries only pure physical failures.
package crv4

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

// Count actual response releases and discarded idle connections in the same
// configured transport; no fixture callback chooses the retry verdict.
type substrateReadHttpCloseFixture struct {
	t        *testing.T
	cause    error
	calls    int
	closes   int
	discards int
}

func (self *substrateReadHttpCloseFixture) RoundTrip(request *http.Request) (*http.Response, error) {
	var call chainContextRPCRequest
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		return nil, err
	}
	self.calls++
	if self.calls > 1 && (self.discards != 1 || self.closes != 1) {
		return nil, errors.New("next native read started before response release and idle discard")
	}
	var closeErr error
	if self.calls == 1 {
		closeErr = self.cause
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), ContentLength: -1,
		Body: &substrateReadHttpTestBody{reader: bytes.NewReader(substrateReadHttpReply(self.t, call)), closeErr: closeErr, closes: &self.closes}}, nil
}

func (self *substrateReadHttpCloseFixture) CloseIdleConnections() { self.discards++ }

// A physical close failure is no longer an artificial permanent error. The
// exact same allowlisted read receives a new body after the first is released.
func TestSubstrateReadHttpTransientCloseRecoversAfterDiscard(t *testing.T) {
	for _, cause := range []error{io.EOF, io.ErrUnexpectedEOF, &net.OpError{Op: "read", Net: "tcp", Err: context.DeadlineExceeded}} {
		transport := &substrateReadHttpCloseFixture{t: t, cause: cause}
		client := newSubstrateReadHttpDecoratedFixture(t, 1024, transport)
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		client.readRetry = hooks
		var result string
		err := client.CallContext(t.Context(), &result, "chain_getFinalizedHead")
		if err != nil || result != "0x2a00" || transport.calls != 2 || transport.closes != 2 || transport.discards != 1 || budget.elapsed != 75*time.Second {
			t.Fatalf("pure HTTP close failure stopped a recoverable read: cause=%v calls=%d closes=%d discards=%d elapsed=%s error=%v", cause, transport.calls, transport.closes, transport.discards, budget.elapsed, err)
		}
	}
}

// Custody, cancellation and mixed integrity remain hard even when a physical
// response owner exposes them alongside an otherwise transient network cause.
func TestSubstrateReadHttpCloseRejectsFileMixedAndCanceledCauses(t *testing.T) {
	for _, cause := range []error{
		&os.PathError{Op: "close", Path: "synthetic-custody", Err: io.EOF},
		&os.LinkError{Op: "rename", Old: "synthetic-custody-old", New: "synthetic-custody-new", Err: io.EOF},
		errors.Join(io.EOF, errors.New("synthetic connection ownership contradiction")),
		context.Canceled,
	} {
		transport := &substrateReadHttpCloseFixture{t: t, cause: cause}
		client := newSubstrateReadHttpDecoratedFixture(t, 1024, transport)
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		client.readRetry = hooks
		var result string
		err := client.CallContext(t.Context(), &result, "chain_getFinalizedHead")
		if err == nil || !errors.Is(err, cause) || RetryableSubstrateReadTransportError(err) || !HasSubstrateReadTransportCause(err) || transport.calls != 1 || transport.closes != 1 || transport.discards != 1 || budget.elapsed != 0 {
			t.Fatalf("HTTP close promoted a hard cause: cause=%v calls=%d closes=%d discards=%d elapsed=%s error=%v", cause, transport.calls, transport.closes, transport.discards, budget.elapsed, err)
		}
	}
}
