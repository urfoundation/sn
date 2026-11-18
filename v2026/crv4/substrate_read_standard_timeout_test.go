// Native reads meet the same standard library deadline values as the API's
// readers. A dial timeout to a native endpoint is transport, not a matcher.
package crv4

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"
)

// net's own dial deadline value; the expired context ends the dial before
// any socket is opened.
func substrateReadTestDialTimeout(t *testing.T) error {
	t.Helper()
	expired, cancel := context.WithDeadline(t.Context(), time.Unix(1, 0))
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(expired, "tcp", "192.0.2.1:9944")
	if conn != nil {
		conn.Close()
	}
	var dial *net.OpError
	if !errors.As(err, &dial) || dial.Err == context.DeadlineExceeded || dial.Err.Error() != "i/o timeout" {
		t.Fatalf("expired dial did not return net's own deadline value: %v", err)
	}
	return &url.Error{Op: "Post", URL: "http://192.0.2.1:9944", Err: err}
}

// The value implements Is, so the classifiers refused it as a custom matcher
// and a native read's dial timeout ended its retry budget at once.
func TestSubstrateReadDialTimeoutIsTransport(t *testing.T) {
	timeout := substrateReadTestDialTimeout(t)
	if !retryableSubstrateRpcReadTransport(timeout, false, false) {
		t.Error("a native read's dial timeout was not transport")
	}
	physical := &substrateReadHttpTransportError{cause: timeout}
	if !RetryableSubstrateReadTransportError(physical) {
		t.Error("a physical native read's dial timeout was not retryable")
	}
	if !receiptScanUnavailable(physical) {
		t.Error("a receipt scan's dial timeout was not unavailable evidence")
	}
	if retryableSubstrateRpcReadTransport(errors.Join(timeout, errors.New("synthetic canonical hash differs")), false, false) {
		t.Error("a contradiction joined to a timeout became transport")
	}
}
