package crv4

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
)

// Only the standard library's exact private values qualify: net's dial and
// lookup timeout, net/http's client timeout and net's cancellation. The
// context errors themselves, other deadline values and look-alikes do not.
func TestStandardContextErrorNamesOnlyExactStandardValues(t *testing.T) {
	expired, cancelExpired := context.WithDeadline(t.Context(), time.Unix(1, 0))
	defer cancelExpired()
	_, dialTimeout := (&net.Dialer{}).DialContext(expired, "tcp", "192.0.2.1:443")
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, dialCanceled := (&net.Dialer{}).DialContext(canceled, "tcp", "192.0.2.1:443")
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	defer transport.CloseIdleConnections()
	response, clientTimeout := (&http.Client{Timeout: time.Nanosecond, Transport: transport}).Get("https://api.example/")
	if response != nil {
		response.Body.Close()
	}
	var dialTimeoutOp, dialCanceledOp *net.OpError
	var clientTimeoutUrl *url.Error
	if !errors.As(dialTimeout, &dialTimeoutOp) || !errors.As(dialCanceled, &dialCanceledOp) || !errors.As(clientTimeout, &clientTimeoutUrl) {
		t.Fatalf("standard values not produced: %v; %v; %v", dialTimeout, dialCanceled, clientTimeout)
	}
	for _, item := range []struct {
		err  error
		want error
	}{
		{err: dialTimeoutOp.Err, want: context.DeadlineExceeded},
		{err: clientTimeoutUrl.Err, want: context.DeadlineExceeded},
		{err: dialCanceledOp.Err, want: context.Canceled},
	} {
		if standard, ok := StandardContextError(item.err); !ok || standard != item.want {
			t.Errorf("%T: got %v %t, want %v", item.err, standard, ok, item.want)
		}
	}
	type timeoutError struct{ error }
	var nilTimeout *timeoutError
	for _, err := range []error{
		nil, context.DeadlineExceeded, context.Canceled, os.ErrDeadlineExceeded, dialTimeoutOp,
		&timeoutError{error: errors.New("i/o timeout")}, nilTimeout, errors.New("i/o timeout"),
	} {
		if standard, ok := StandardContextError(err); ok || standard != nil {
			t.Errorf("%T %v claimed a standard context verdict", err, err)
		}
	}
}
